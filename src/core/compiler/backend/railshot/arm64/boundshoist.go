//go:build arm64

package arm64

import (
	"cmp"
	"os"
	"slices"
	"strconv"

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
// elide). Defaults off because duplicating loop bodies costs compile memory; set
// WAGO_LOOP_PRECHECK=1/on/true to opt in.

var loopPrecheckEnabled = envDefaultOff(os.Getenv("WAGO_LOOP_PRECHECK"))

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

// scanLoopHoistable scans the loop body (reader at the body start, restored on
// return) for hoistable memory-0 bases. The shared module-aware classifier keeps
// scalar, SIMD, atomic, memory64, and indexed-memory immediates synchronized.
func scanLoopHoistable(r *wasm.Reader, m *wasm.Module) (cands []hoistCand, elidable int, hasGrow bool) {
	return scanLoopHoistableWithClassifier(r, m, wasm.NewModuleInstructionClassifier(m, true))
}

func scanLoopHoistableWithClassifier(r *wasm.Reader, m *wasm.Module, classifier wasm.ModuleInstructionClassifier) (cands []hoistCand, elidable int, hasGrow bool) {
	start := r.Offset()
	defer func() { _ = r.JumpTo(start) }()
	set := map[uint32]bool{}
	maxExt := map[uint32]int32{}
	acc := map[uint32]int{}
	poison := map[uint32]bool{}
	prevGet := int64(-1)
	depth := 0
	var imm wasm.InstructionImmediate
	for {
		op, err := r.Byte()
		if err != nil {
			return nil, 0, false
		}
		if err := classifier.ClassifyInto(r, op, &imm); err != nil {
			return nil, 0, false
		}
		curGet := int64(-1)
		switch imm.Kind {
		case wasm.InstrLocalGet:
			curGet = int64(imm.Index)
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			set[imm.Index] = true
		case wasm.InstrMemoryGrow:
			hasGrow = true
		}
		if prevGet >= 0 && imm.TouchesMemory {
			base := uint32(prevGet)
			if imm.HasMemIndex && imm.MemIndex != 0 {
				poison[base] = true
			} else if size := memAccessSize(op); size != 0 {
				acc[base]++
				if imm.MemOffset > 0x7fffffff || imm.MemOffset+uint64(size) > 0x7fffffff {
					poison[base] = true
				} else if ext := int32(imm.MemOffset + uint64(size)); ext > maxExt[base] {
					maxExt[base] = ext
				}
			} else {
				poison[base] = true
			}
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f:
			depth++
		case 0x0b:
			if depth == 0 {
				for b, ext := range maxExt {
					if !set[b] && !poison[b] {
						cands = append(cands, hoistCand{base: b, extent: ext})
						elidable += acc[b]
					}
				}
				slices.SortFunc(cands, func(a, b hoistCand) int { return cmp.Compare(a.base, b.base) })
				return cands, elidable, hasGrow
			}
			depth--
		}
		prevGet = curGet
	}
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
func (f *fn) compileVersionedLoop(r *wasm.Reader, paramN, resultN int, frameTypes []machineType, res0 machineType, cands []hoistCand) bool {
	// v1 scope: void loops only (no params to stage across the two entries), and
	// never nest a versioned loop inside another (bounds code growth to 2×).
	if paramN != 0 || f.inVersionedLoop {
		return false
	}
	bodyStart := r.Offset()
	preLoopCtrl := len(f.ctrl)

	// Canonicalize the loop-entry state so both versions start identically, and
	// snapshot it for restoring before the slow body.
	f.reconcileLocals()
	f.flush()
	entryTypes := append([]machineType(nil), f.currentLogicalTypes()...)
	var entryLocals packedLocStates
	if f.usesCalls {
		entryLocals = make(packedLocStates, packedLocStateBytes(f.nLocals))
		for x := 0; x < f.nLocals; x++ {
			entryLocals.set(x, f.locals[x].state)
		}
	}

	// Precheck: for each invariant base, trap-free compare of base+extent to
	// memBytes; any failure branches to the slow body. Scratch only (post-flush).
	//
	// arm64: there is no LEA and no memory-operand CMP. Compute t = base + extent
	// with an add-immediate (extent is a nonnegative int32; fall back to a
	// materialized constant + reg-reg add for the rare >12-bit extent), then a
	// register CMP (SUBS XZR,...) and a conditional branch on unsigned-greater.
	failSites := make([]int, 0, len(cands))
	for _, c := range cands {
		base := f.allocReg(0)
		f.loadLocalValue(base, c.base)
		t := f.allocReg(maskOf(base))
		if c.extent >= 0 && c.extent <= 0xFFF {
			f.a.AddImm64(t, base, uint32(c.extent)) // t = base + off + size
		} else {
			f.a.MovImm64(t, uint64(uint32(c.extent)))
			f.a.Add64(t, base, t)
		}
		f.a.CmpReg64(t, f.memSizeReg)
		failSites = append(failSites, f.a.Bcond(condA)) // base+ext > memBytes → slow
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
	f.enterLoopFrame(resultN, frameTypes, res0)
	if err := f.bodyLoop(r, preLoopCtrl); err != nil {
		panic(err) // decode/lowering error inside the fast body
	}
	f.elideBases = nil
	doneSite := f.a.Branch()

	// SLOW body: normal per-access checks. Re-read the body from the start and
	// restore the canonical loop-entry state first. The precheck fail sites are
	// conditional branches (imm19 range); the done jump is unconditional (imm26).
	for _, s := range failSites {
		f.a.PatchBranch19(s, f.a.Len())
	}
	if err := r.JumpTo(bodyStart); err != nil {
		panic(err)
	}
	f.setDepthTypes(entryTypes)
	f.setLocalsState(entryLocals)
	f.unreachable = false
	f.enterLoopFrame(resultN, frameTypes, res0)
	if err := f.bodyLoop(r, preLoopCtrl); err != nil {
		panic(err)
	}
	f.inVersionedLoop = false
	f.a.PatchBranch26(doneSite, f.a.Len())
	return true
}

// enterLoopFrame replicates opBlock's cfLoop header for a versioned body: fix the
// frame's base/height from the (already-flushed) entry, converge locals eagerly,
// align the loop top, and push the frame.
func (f *fn) enterLoopFrame(resultN int, frameTypes []machineType, res0 machineType) {
	fr := ctrlFrame{kind: cfLoop, resultN: resultN, branchN: 0, elseSite: -1, res0: res0, types: frameTypes}
	fr.height = f.depth()
	fr.baseTypes = append([]machineType(nil), f.currentLogicalTypes()...)
	f.reconcileLocals()
	f.convergeFrameBranchState(&fr)
	f.flush()
	f.alignCode(f.policy.LoopAlignLog2)
	fr.loopStart = f.a.Len()
	f.pushCtrl(&fr)
}

// loadLocalValue loads local x's current value into reg (from its pinned register
// or its frame slot). Used by the precheck, which runs right after the entry
// flush (so a pinned local is clean in both its register and slot).
func (f *fn) loadLocalValue(reg Reg, x uint32) {
	if pr, isFloat, ok := f.pinReg(int(x)); ok && !isFloat {
		f.a.MovReg64(reg, pr)
		return
	}
	// arm64: SP (reg 31) is the frame base; ld64 hides the scaled-offset
	// encodability fallback (Load64 returns ok=false for offsets that don't fit
	// the 12-bit scaled immediate).
	f.ld64(reg, SP, f.localOff(int(x)))
}
