//go:build amd64

package amd64

import (
	"cmp"
	"os"
	"slices"
	"strconv"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Loop bounds-check hoisting via loop versioning (P6.2, "hybrid loop precheck").
//
// A memory access `mem[$b + off]` inside a loop, where $b is a LOOP-INVARIANT
// local (never set in the loop body), re-checks `$b + off + size <= memBytes`
// every iteration even though $b and (since memBytes only ever grows) the bound
// are loop-invariant. This emits, before the loop, a one-time PRECHECK of the max
// extent on each such base, and compiles the loop body TWICE:
//
//   - the FAST body, run when every precheck passes, with those bases' inline
//     checks elided (they are provably in bounds — memBytes is monotone);
//   - the SLOW body, run otherwise, with the normal per-access checks, preserving
//     exact trap timing (0-iteration / early-exit / a genuinely-OOB access all
//     behave as the spec requires).
//
// The precheck is a BRANCH, not a trap, so it never introduces a spurious trap —
// that is what makes hoisting sound here (a plain hoisted check would move the
// trap earlier, which is observable because a wasm trap leaves partial memory
// writes visible). Explicit-bounds mode only (guard mode has no inline check to
// elide). V1 is memory32-only and excludes functions with candidate native GC root
// plans because their call/allocation liveness streams are linear in original Wasm
// order. Defaults on; set WAGO_LOOP_PRECHECK=0/off/false to disable it for A/B runs.

var loopPrecheckEnabled = envDefaultOn(os.Getenv("WAGO_LOOP_PRECHECK"))

// memAccessSize returns the byte width a memarg load/store opcode accesses, or 0
// if op is not a plain (non-SIMD) linear-memory load/store.
func memAccessSize(op byte) int {
	switch op {
	case 0x2c, 0x2d, 0x30, 0x31, 0x3a, 0x3c: // i32/i64.load8_*, i32/i64.store8
		return 1
	case 0x2e, 0x2f, 0x32, 0x33, 0x3b, 0x3d: // load16_* / store16
		return 2
	case 0x28, 0x2a, 0x34, 0x35, 0x36, 0x38, 0x3e: // i32/f32.load, i64.load32_*, i32/f32.store, i64.store32
		return 4
	case 0x29, 0x2b, 0x37, 0x39: // i64/f64.load, i64/f64.store
		return 8
	}
	return 0
}

// hoistCand is one loop-invariant base local and the max access extent (off+size)
// seen on a DIRECT `local.get $base; <memop>` in the loop body.
type hoistCand struct {
	base   uint32
	extent int32
}

// walkLoopBody consumes validated instructions until the matching loop end and
// always restores r. The shared Wasm classifier is the only immediate decoder:
// try_table catch vectors, br_table labels, SIMD/atomic forms, and memory64
// offsets therefore cannot desynchronize this scan. No partial findings escape
// on failure.
func walkLoopBody(r *wasm.Reader, memory64 bool, visit func(op byte, imm wasm.InstructionImmediate)) bool {
	start := r.Offset()
	defer func() { _ = r.JumpTo(start) }()
	depth := 0
	var imm wasm.InstructionImmediate
	for {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		if err := wasm.ClassifyInstructionImmediateIntoWithMemarg64(r, op, &imm, memory64); err != nil {
			return false
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f: // block, loop, if, try_table
			depth++
		case 0x0b: // end
			if depth == 0 {
				return true
			}
			depth--
		}
		visit(op, imm)
	}
}

// scanLoopBody records locals assigned anywhere in one loop and whether the loop
// grows memory. valid is false on any classifier failure; callers must then
// discard all findings and conservatively clear loop-sensitive facts.
func scanLoopBody(r *wasm.Reader, memory64 bool) (setLocals map[uint32]bool, hasGrow, valid bool) {
	setLocals = map[uint32]bool{}
	valid = walkLoopBody(r, memory64, func(_ byte, imm wasm.InstructionImmediate) {
		switch imm.Kind {
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			setLocals[imm.Index] = true
		case wasm.InstrMemoryGrow:
			hasGrow = true
		}
	})
	if !valid {
		return nil, false, false
	}
	return setLocals, hasGrow, true
}

// scanLoopHoistable scans the loop body (reader at the body start, restored on
// return) for hoistable bases: locals accessed as a direct memory base and never
// set in the loop. Returns them with each one's max extent, the total number of
// per-iteration accesses that would be elided (the check-density benefit signal),
// whether the loop grows memory, and an explicit validity result. A classifier
// failure returns no candidates or local findings. Memory64 still returns mutation
// and grow facts but no candidates until memAddr64 consumes a carry-safe certificate.
func scanLoopHoistable(r *wasm.Reader, memory64 bool) (cands []hoistCand, elidable int, hasGrow bool, setLocals map[uint32]bool, valid bool) {
	setLocals = map[uint32]bool{}
	maxExt := map[uint32]int32{}
	acc := map[uint32]int{}     // direct-access count per base
	poison := map[uint32]bool{} // bases with a direct access this scan can't size
	prevGet := int64(-1)        // local index of an immediately-preceding local.get, else -1
	valid = walkLoopBody(r, memory64, func(op byte, imm wasm.InstructionImmediate) {
		curGet := int64(-1)
		switch imm.Kind {
		case wasm.InstrLocalGet:
			curGet = int64(imm.Index)
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			setLocals[imm.Index] = true
		case wasm.InstrMemoryGrow:
			hasGrow = true
		}
		if prevGet >= 0 && imm.TouchesMemory {
			base := uint32(prevGet)
			if imm.HasMemIndex && imm.MemIndex != 0 {
				// The precheck compares against memory 0's cached byte length.
				poison[base] = true
			} else if size := memAccessSize(op); size != 0 {
				acc[base]++
				// The precheck's LEA displacement is int32. Memory64 and large
				// memory32 offsets that exceed it are never candidates.
				if imm.MemOffset > 0x7fffffff || imm.MemOffset+uint64(size) > 0x7fffffff {
					poison[base] = true
				} else if ext := int32(imm.MemOffset + uint64(size)); ext > maxExt[base] {
					maxExt[base] = ext
				}
			} else {
				// SIMD/atomic/bulk memory forms are decoded correctly, but this v1
				// precheck does not model their complete access width.
				poison[base] = true
			}
		}
		prevGet = curGet
	})
	if !valid {
		return nil, 0, false, nil, false
	}
	if memory64 {
		// memAddr64 does not consume elideBases and must retain both carry checks.
		// Keep the shared scan's mutation/grow facts, but do not version the loop.
		return nil, 0, hasGrow, setLocals, true
	}
	for b, ext := range maxExt {
		if !setLocals[b] && !poison[b] { // invariant, never set in the loop, all accesses sized
			cands = append(cands, hoistCand{base: b, extent: ext})
			elidable += acc[b]
		}
	}
	// Map iteration order must not choose precheck/register order: parallel
	// function compilation creates these maps on different goroutines and exposed
	// the latent nondeterminism as byte-different code for the same function.
	slices.SortFunc(cands, func(a, b hoistCand) int { return cmp.Compare(a.base, b.base) })
	return cands, elidable, hasGrow, setLocals, true
}

// loopPrecheckMinChecks is the minimum per-iteration elided-check count for a loop
// to be worth versioning: the fast/slow bodies double the loop code, so a loop
// that would elide only a check or two is not worth 2× the size. Tunable via
// WAGO_LP_MINCHECKS. (The gate mainly filters out the many 1–2 check loops; the
// check-dense loops that carry the exec win are kept.)
var loopPrecheckMinChecks = func() int {
	if v := os.Getenv("WAGO_LP_MINCHECKS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return 4
}()

// compileVersionedLoop lowers a versionable void loop: precheck → fast body
// (invariant-base checks elided) → jump past → slow body (checked). Both bodies
// are compiled from the same bytecode via bodyLoop; the reader ends past the
// loop's `end`. Returns false if the loop shape is not versioned here (caller
// falls back to the normal loop lowering).
func (f *fn) compileVersionedLoop(r *wasm.Reader, paramTypes, resultTypes []machineType, res0 machineType, cands []hoistCand, setLocals map[uint32]bool) bool {
	// v1 scope: void loops only (no params/results to stage and merge across the
	// two entries), and never nest a versioned loop inside another (bounds code
	// growth remains at 2×).
	if len(paramTypes) != 0 || len(resultTypes) != 0 || f.inVersionedLoop ||
		(f.gcFrameRoots != nil && f.gcFrameRoots.Candidate) {
		// The validated root-liveness streams are linear in original Wasm call and
		// allocation order. Duplicating a loop body without duplicating/remapping
		// both streams would make the native frame-root plan inexact.
		return false
	}
	bodyStart := r.Offset()
	preLoopCtrl := len(f.ctrl)

	// Canonicalize once, then preserve one semantic entry snapshot for both
	// generated versions. Compiling the fast body must not seed the slow body.
	f.reconcileLocals()
	f.flush()
	entryTypes := append([]machineType(nil), f.currentLogicalTypes()...)
	entryRoots := f.rootsBottomToTop()
	entryGCRoots := gcRootFlags(entryRoots)
	var entryStackFacts []shared.GCRefFact
	if exactGCRefFactsEnabled {
		entryStackFacts = make([]shared.GCRefFact, len(entryRoots))
		for i, root := range entryRoots {
			entryStackFacts[i] = gcRefFact(root)
		}
	}
	entryLocalFacts := f.snapshotGCRefFacts()
	var entryLocals []locState
	if f.usesCalls {
		entryLocals = make([]locState, len(f.pinnedLocals))
		for i, x := range f.pinnedLocals {
			entryLocals[i] = f.locals[x].state
		}
	}
	installEntry := func() {
		f.setDepthTypesWithGCInfo(entryTypes, entryGCRoots, entryStackFacts)
		f.setLocalsState(entryLocals)
		f.installGCRefFacts(entryLocalFacts)
		f.invalidateLoopModifiedGCRefFacts(setLocals)
		f.unreachable = false
	}
	installEntry()

	// Precheck: for each invariant base, trap-free compare of base+extent to
	// memBytes; any failure branches to the slow body. Scratch only (post-flush).
	failSites := make([]int, 0, len(cands))
	for _, c := range cands {
		base := f.allocReg(0)
		f.loadLocalValue(base, c.base)
		// Memory32 addresses are i32 values even when a returning host import left
		// arbitrary high bits in the 64-bit ABI word. Match memAddr's consuming-side
		// canonicalization before native-width precheck arithmetic.
		f.a.MovRegReg32(base, base)
		t := f.allocReg(maskOf(base))
		f.a.LeaDisp(t, base, c.extent) // t = base + off + size
		f.a.Cmp64(t, f.memSizeReg)
		failSites = append(failSites, f.a.JccPlaceholder(condA)) // base+ext > memBytes → slow
		f.release(t)
		f.release(base)
	}
	f.stats.peep("loop-precheck")

	elide := make(map[uint32]bool, len(cands))
	for _, c := range cands {
		elide[c.base] = true
	}

	// FAST body: invariant-base checks elided.
	f.inVersionedLoop = true
	f.elideBases = elide
	f.enterLoopFrame(resultTypes, res0, setLocals)
	if err := f.bodyLoop(r, preLoopCtrl); err != nil {
		panic(err) // decode/lowering error inside the fast body
	}
	fastExitFacts := f.snapshotGCRefFacts()
	f.elideBases = nil
	doneSite := f.a.JmpPlaceholder()

	// SLOW body: normal per-access checks. Re-read the body from the start and
	// reinstall the same conservative entry state, including loop-local fact
	// invalidation and straight-line cache/resolver invalidation.
	for _, s := range failSites {
		f.a.PatchRel32(s, f.a.Len())
	}
	if err := r.JumpTo(bodyStart); err != nil {
		panic(err)
	}
	installEntry()
	f.enterLoopFrame(resultTypes, res0, setLocals)
	if err := f.bodyLoop(r, preLoopCtrl); err != nil {
		panic(err)
	}
	f.inVersionedLoop = false
	f.a.PatchRel32(doneSite, f.a.Len())

	// Code after the versions is reached by either exit. Retain only facts both
	// compiled bodies guarantee rather than allowing the slow compile to win.
	f.mergeGCRefFactsInto(&fastExitFacts)
	f.installGCRefFacts(fastExitFacts)
	f.freeGCRefFactBuf(fastExitFacts)
	f.freeGCRefFactBuf(entryLocalFacts)
	return true
}

// enterLoopFrame replicates opBlock's cfLoop header for a versioned body: fix the
// frame's base/height from the (already-flushed) entry, converge locals eagerly,
// align the loop top, and push the frame.
func (f *fn) enterLoopFrame(resultTypes []machineType, res0 machineType, setLocals map[uint32]bool) {
	rN := len(resultTypes)
	fr := ctrlFrame{kind: cfLoop, resultN: rN, branchN: 0, elseSite: -1, res0: res0, resultTypes: resultTypes, loopSetLocals: setLocals}
	fr.height = f.depth()
	fr.baseTypes = append([]machineType(nil), f.currentLogicalTypes()...)
	f.captureGCFrameShape(&fr)
	fr.branchGCFacts = f.snapshotGCRefFacts()
	f.reconcileLocals()
	f.convergeEdgeTo(&fr.branchState)
	f.flush()
	f.a.Align16()
	fr.loopStart = f.a.Len()
	f.ctrl = append(f.ctrl, fr)
}

// loadLocalValue loads local x's current value into reg (from its pinned register
// or its frame slot). Used by the precheck, which runs right after the entry
// flush (so a pinned local is clean in both its register and slot).
func (f *fn) loadLocalValue(reg Reg, x uint32) {
	if pr, isFloat, ok := f.pinReg(int(x)); ok && !isFloat {
		f.a.MovReg64(reg, pr)
		return
	}
	f.loadFrameInt(reg, f.localAddr(int(x)), f.localType[x])
}
