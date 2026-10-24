//go:build arm64

package arm64

import (
	"fmt"

	"slices"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Control flow: block / loop / if / else / end / br / br_if / br_table / return /
// unreachable. Ported from WARP's control-flow lowering, but using the canonical-
// slots reconciliation model (the same one backend/railshot/arm64 uses against this
// runtime): at every control boundary the operand stack is flushed to position-
// indexed frame slots, so all edges into a join agree on where each value lives.
// This trades WARP's RegisterCopyResolver register-shuffling for a simpler,
// proven scheme; register residency of locals is layered on separately.
//
// This file is a mechanical arm64 twin of backend/railshot/amd64/control.go: the
// operand-stack canonicalization, control-frame bookkeeping and merge logic are
// architecture-neutral and port verbatim; only the leaf instruction lowering
// changes. Per the port contract, x86 EFLAGS cmp+Jcc fusion becomes CMP + B.cond
// (§4b), the br_table RIP-relative jump table becomes an ADR-based table + BR
// through the backend scratch registers (§4e), and every frame-slot load/store
// goes through the encodability-checked f.ld64/f.st64/f.fld/f.fst helpers off the
// SP base (§6.1/§6.7). Forward branch sites are patched with PatchBranch19 for the
// conditional (imm19, ±1 MiB) sites and PatchBranch26 for the unconditional (imm26,
// ±128 MiB) sites — the two-range split of §6.2 — chosen statically at each site.

var errBadLabel = fmt.Errorf("arm64: br label out of range")

type ctrlKind uint8
type ctrlFlags uint16

const (
	cfFunc ctrlKind = iota
	cfBlock
	cfLoop
	cfIf
	cfTry
)

const (
	ctrlHasElse ctrlFlags = 1 << iota
	ctrlEntryUnreachable
	ctrlEndReachable
	ctrlRegMerge1
	ctrlBaseTypesSet
	ctrlColdBaseTypes
	ctrlHasBaseGCRoots
	ctrlHasParamGCRoots
	ctrlHasResultGCRoots
	ctrlLoopCallFree
	ctrlCallFreeRegion
	ctrlLoopConstScope
)

// ctrlFrame is one open control construct (or the implicit function frame).
type ctrlFrame struct {
	kind       ctrlKind
	res0       machineType // first result's machine type (valid when resultN >= 1)
	flags      ctrlFlags
	mergeIndex uint32 // index+1 into scratch.ctrlMerges; zero has no cold merge state
	branchN    uint32 // values transferred on a branch to this label
	baseTypes  uint16 // fixed-arena start in low byte, count in high byte
	ehDepth    uint16 // installed handler count at this label (uses existing padding)

	height          int // operand depth at the frame's result base
	paramN, resultN int
	controlSite     int           // cfLoop: backward target; cfIf: false-edge branch, -1 once patched
	types           []machineType // parameters followed by results; split by paramN/resultN
}

// ctrlFrameMerge contains feature-specific state needed only by frames that
// merge pinned locals, track GC roots, patch branches, hoist/pin loops, or defer cold edges.
// Keeping it in compact scratch removes pointer-rich fields from ordinary frames.
type ctrlFrameMerge struct {
	ends         []uint32 // overflow after two inline packed forward-end sites
	branchState  packedLocStates
	entryState   packedLocStates
	loopSetStart uint32 // loop: modified-local range; other frames: first packed end site
	loopSetMeta  uint32 // loop: count/known/counted-counter; other frames: second packed end site
	extra        *ctrlFrameMergeExtra
}

// ctrlFrameMergeExtra keeps exception and deferred-code state out of ordinary
// scalar merge records. Both are uncommon and already require cold-path work.
type ctrlFrameMergeExtra struct {
	coldEdges []coldEdge
	eh        *ctrlFrameEH
}

// ctrlFrameRoots is allocated as a depth-parallel sidecar only when exact GC
// root tracking reaches structured control. Scalar merges do not retain three
// scanned slice headers per reserved merge slot.
type ctrlFrameRoots struct {
	flags []bool // base, parameters, then results
}

const initialCtrlMergeCapacity = 16

const (
	loopSetKnownBit        = uint32(1 << 16)
	countedLoopShift       = 17
	countedLoopCounterMask = uint32(0x7fff << countedLoopShift)
)

func (m *ctrlFrameMerge) hasLoopSet() bool  { return m.loopSetMeta&loopSetKnownBit != 0 }
func (m *ctrlFrameMerge) loopSetCount() int { return int(uint16(m.loopSetMeta)) }

func (m *ctrlFrameMerge) setCountedLoop(counter int) bool {
	if counter < 0 || counter >= 1<<15-1 {
		return false
	}
	m.loopSetMeta = m.loopSetMeta&^countedLoopCounterMask | uint32(counter+1)<<countedLoopShift
	return true
}

// inspectLinearSumLoop recognizes the exact scalar reduction
//
//	acc += i64.load(addr); addr += 8; counter--; br 0
//
// following a top-tested counter==0 exit. It is deliberately exact: admitting
// arbitrary loop bodies would require a general induction/range proof and could
// move a memory trap across an observable effect.
func (f *fn) inspectLinearSumLoop(r *wasm.Reader, counter int) (addr, acc int, loadPC uint32, ok bool) {
	r2 := *r
	readLocal := func(op byte) (int, bool) {
		got, err := r2.Byte()
		if err != nil || got != op {
			return 0, false
		}
		x, err := r2.U32()
		return int(x) + f.localBase, err == nil
	}
	readOp := func(want byte) bool {
		got, err := r2.Byte()
		return err == nil && got == want
	}

	acc, yes := readLocal(0x20) // local.get acc
	if !yes {
		return 0, 0, 0, false
	}
	addr, yes = readLocal(0x20) // local.get addr
	if !yes {
		return 0, 0, 0, false
	}
	loadPC = f.tracePCBase + uint32(r2.Offset())
	if !readOp(0x29) { // i64.load
		return 0, 0, 0, false
	}
	memoryIndex, off, err := f.readMemArg(&r2)
	if err != nil || memoryIndex != 0 || off != 0 || !readOp(0x7c) { // i64.add
		return 0, 0, 0, false
	}
	setAcc, yes := readLocal(0x21)
	if !yes || setAcc != acc {
		return 0, 0, 0, false
	}
	getAddr, yes := readLocal(0x20)
	if !yes || getAddr != addr || !readOp(0x41) {
		return 0, 0, 0, false
	}
	stride, err := r2.I32()
	if err != nil || stride != 8 || !readOp(0x6a) { // i32.add
		return 0, 0, 0, false
	}
	setAddr, yes := readLocal(0x21)
	if !yes || setAddr != addr {
		return 0, 0, 0, false
	}
	getCounter, yes := readLocal(0x20)
	if !yes || getCounter != counter || !readOp(0x41) {
		return 0, 0, 0, false
	}
	one, err := r2.I32()
	if err != nil || one != 1 || !readOp(0x6b) { // i32.sub
		return 0, 0, 0, false
	}
	setCounter, yes := readLocal(0x21)
	if !yes || setCounter != counter || !readOp(0x0c) {
		return 0, 0, 0, false
	}
	label, err := r2.U32()
	if err != nil || label != 0 || !readOp(0x0b) || !readOp(0x0b) {
		return 0, 0, 0, false
	}
	if acc < 0 || acc >= len(f.localType) || f.localType[acc] != mtI64 ||
		addr < 0 || addr >= len(f.localType) || f.localType[addr] != mtI32 || addr == counter ||
		addr >= 1<<16-1 || acc >= 1<<16-1 {
		return 0, 0, 0, false
	}
	return addr, acc, loadPC, true
}

func (f *fn) tryHoistLinearSumBounds(r *wasm.Reader, counter int) {
	if !f.opt(optLinearSumLoop) || f.guardMode || f.threadedMemory0 || f.memoryAddr64(0) || f.memSizeReg == regNone {
		return
	}
	addr, acc, loadPC, ok := f.inspectLinearSumLoop(r, counter)
	if !ok {
		return
	}
	counterReg, counterFloat, counterPinned := f.pinReg(counter)
	addrReg, addrFloat, addrPinned := f.pinReg(addr)
	if !counterPinned || counterFloat || !addrPinned || addrFloat {
		return
	}
	// Both operands are i32 values. Zero-extend before native-width arithmetic;
	// counter*8 cannot overflow uint64, and a bounded end proves every load.
	t := f.allocReg(0)
	f.a.MovReg32(t, counterReg)
	f.a.LslImm(t, t, 3, false)
	f.a.MovReg32(addrReg, addrReg)
	f.a.Add64(t, t, addrReg)
	f.cmpRR(t, f.memSizeReg, true)
	savedPC := f.wasmPC
	f.wasmPC = loadPC
	mt, _ := f.m.MemoryType(0)
	if mt.Limits.HasMax && mt.Limits.Max < 65536 {
		f.trapIf(condA, trapMemOOB)
	} else {
		// A full 4 GiB memory32 admits aligned 8-byte accesses across modular
		// i32 address wraparound. For every smaller memory, exceeding the widened
		// end necessarily encounters an out-of-bounds scalar iteration first.
		inBounds := f.a.Bcond(condBE)
		f.a.MovImm64(t, 1<<32)
		f.cmpRR(f.memSizeReg, t, true)
		f.trapIf(condNE, trapMemOOB)
		if !f.a.TstImm32(addrReg, 7) {
			panic("arm64: i32 alignment mask is not encodable")
		}
		f.trapIf(condNE, trapMemOOB)
		f.patchBranch19(inBounds, f.a.Len())
	}
	f.wasmPC = savedPC
	f.release(t)
	f.linearSumLoop = uint32(addr+1) | uint32(acc+1)<<16
	f.linearSumLoopDepth = uint16(len(f.ctrl))
	f.stats.peep("counted-loop-bounds-hoist")
}

// tryUnrolledLinearSumLatch splits an exact i64 reduction across four native
// accumulators. Addition is associative modulo 2^64, so regrouping preserves
// Wasm semantics while exposing independent load/add chains to the CPU.
func (f *fn) tryUnrolledLinearSumLatch(counter int) bool {
	if f.linearSumLoop == 0 || f.linearSumLoopDepth != uint16(len(f.ctrl)) {
		return false
	}
	addr := int(uint16(f.linearSumLoop) - 1)
	acc := int(uint16(f.linearSumLoop>>16) - 1)
	counterReg, counterFloat, counterPinned := f.pinReg(counter)
	addrReg, addrFloat, addrPinned := f.pinReg(addr)
	accReg, accFloat, accPinned := f.pinReg(acc)
	if !counterPinned || counterFloat || !addrPinned || addrFloat || !accPinned || accFloat {
		return false
	}

	p1 := f.allocReg(0)
	f.pinned = f.pinned.add(p1)
	p2 := f.allocReg(0)
	f.pinned = f.pinned.add(p2)
	p3 := f.allocReg(0)
	f.pinned = f.pinned.add(p3)
	f.a.MovImm64(p1, 0)
	f.a.MovImm64(p2, 0)
	f.a.MovImm64(p3, 0)

	// The scalar body already consumed the first element and advanced addr.
	f.a.SubsImm32(counterReg, counterReg, 1)
	firstDone := f.a.Bcond(condE)
	f.a.CmpImm32(counterReg, 4)
	toRemainder := f.a.Bcond(condB)

	toWrapping := -1
	mt, _ := f.m.MemoryType(0)
	if !mt.Limits.HasMax || mt.Limits.Max >= 65536 {
		// Native group offsets do not wrap at 2^32. Select the scalar tail
		// once for wrapping ranges, keeping ordinary groups unchanged.
		f.a.MovReg32(X16, counterReg)
		f.a.LslImm(X16, X16, 3, false)
		f.a.Add64(X16, X16, addrReg)
		f.a.LsrImm(X16, X16, 32, false)
		toWrapping = f.a.Cbnz64(X16)
	}

	group := f.a.Len()
	f.a.LoadIdx(X16, linMemReg, addrReg, 0, 8, false, true)
	f.a.Add64(accReg, accReg, X16)
	f.a.LoadIdx(X16, linMemReg, addrReg, 8, 8, false, true)
	f.a.Add64(p1, p1, X16)
	f.a.LoadIdx(X16, linMemReg, addrReg, 16, 8, false, true)
	f.a.Add64(p2, p2, X16)
	f.a.LoadIdx(X16, linMemReg, addrReg, 24, 8, false, true)
	f.a.Add64(p3, p3, X16)
	f.a.AddImm32(addrReg, addrReg, 32)
	f.a.SubImm32(counterReg, counterReg, 4)
	f.a.CmpImm32(counterReg, 4)
	moreGroups := f.a.Bcond(condAE)
	_ = f.a.PatchBranch19(moreGroups, group)

	_ = f.a.PatchBranch19(toRemainder, f.a.Len())
	if toWrapping >= 0 {
		f.patchBranch19(toWrapping, f.a.Len())
	}
	noRemainder := f.a.Cbz32(counterReg)
	remainder := f.a.Len()
	f.a.LoadIdx(X16, linMemReg, addrReg, 0, 8, false, true)
	f.a.Add64(accReg, accReg, X16)
	f.a.AddImm32(addrReg, addrReg, 8)
	f.a.SubsImm32(counterReg, counterReg, 1)
	moreRemainder := f.a.Bcond(condNE)
	_ = f.a.PatchBranch19(moreRemainder, remainder)

	combine := f.a.Len()
	_ = f.a.PatchBranch19(firstDone, combine)
	_ = f.a.PatchBranch19(noRemainder, combine)
	f.a.Add64(accReg, accReg, p1)
	f.a.Add64(accReg, accReg, p2)
	f.a.Add64(accReg, accReg, p3)
	f.pinned = f.pinned.remove(p3).remove(p2).remove(p1)
	f.release(p3)
	f.release(p2)
	f.release(p1)
	f.markLocalDirty(counter)
	f.markLocalDirty(addr)
	f.markLocalDirty(acc)
	f.setFactsForLocal(counter, 0)
	f.linearSumLoop = 0
	f.linearSumLoopDepth = 0
	f.stats.peep("linear-sum-unroll4")
	return true
}

type ctrlFrameEH struct {
	// cfTry only: one fixed native-stack handler record plus an ordered catch
	// dispatch table. Scalar exceptions carry at most two payload words; reference
	// catches copy those words into a fixed rooted exception slot before exposing
	// its stable frame-relative address.
	catches     []ehCatchClause
	targetSite  uint32
	recordIndex uint16
	refResults  uint16 // bit i: branch-result position i carries a rooted exception identity
}

func (f *fn) ensureFrameEH(fr *ctrlFrame) *ctrlFrameEH {
	extra := f.ensureCtrlMergeExtra(fr)
	if extra.eh == nil {
		extra.eh = new(ctrlFrameEH)
	}
	return extra.eh
}

func (f *fn) ctrlRoots(fr *ctrlFrame) *ctrlFrameRoots {
	if fr.mergeIndex == 0 || int(fr.mergeIndex) > len(f.scratchState().ctrlRoots) {
		return nil
	}
	return &f.scratchState().ctrlRoots[fr.mergeIndex-1]
}

func (f *fn) ensureCtrlRoots(fr *ctrlFrame) *ctrlFrameRoots {
	f.ensureCtrlMerge(fr)
	sc := f.scratchState()
	if sc.ctrlRoots == nil {
		sc.ctrlRoots = make([]ctrlFrameRoots, 0, initialCtrlMergeCapacity)
	}
	for len(sc.ctrlRoots) < int(fr.mergeIndex) {
		sc.ctrlRoots = append(sc.ctrlRoots, ctrlFrameRoots{})
	}
	return &sc.ctrlRoots[fr.mergeIndex-1]
}

func (f *fn) ensureFrameGCRootFlags(fr *ctrlFrame) []bool {
	roots := f.ensureCtrlRoots(fr)
	n := fr.height + fr.paramN + fr.resultN
	if len(roots.flags) < n {
		flags := make([]bool, n)
		copy(flags, roots.flags)
		roots.flags = flags
	}
	return roots.flags[:n]
}

func (f *fn) setFrameResultGCRoot(fr *ctrlFrame, index int) {
	flags := f.ensureFrameGCRootFlags(fr)
	flags[fr.height+fr.paramN+index] = true
	fr.set(ctrlHasResultGCRoots, true)
}

func (f *fn) frameLoopSetLocals(fr *ctrlFrame) []uint16 {
	if cold := f.ctrlMerge(fr); cold != nil && cold.hasLoopSet() {
		start := int(cold.loopSetStart)
		return f.loopSetLocals[start : start+cold.loopSetCount()]
	}
	return nil
}

func loopSetsLocal(locals []uint16, index uint32) bool {
	if index > uint32(^uint16(0)) {
		return false
	}
	_, ok := slices.BinarySearch(locals, uint16(index))
	return ok
}

type ehCatchClause struct {
	tag       uint32
	frame     uint32
	matchSite uint32
	kind      wasm.CatchKind
	scalarN   uint8
	payloadN  uint8
	rootIndex uint16
	// firstType is the machine type of payload 0, the only one delivered in a
	// register (single-result merge); wider payloads always go through slots.
	firstType machineType
	// gcParams marks tag parameters that are collector references and so
	// live in the GC lanes of the shared payload layout.
	gcParams uint8
}

// lane returns the payload lane of the clause tag's parameter i.
func (c *ehCatchClause) lane(i int) int {
	if c.gcParams&(1<<i) != 0 {
		return shared.EHGCLaneBase + i
	}
	return i
}

type coldEdge struct {
	site int
	code []byte
}

// --- operand-stack canonicalization ---

func typesOfVals(vals []wasm.ValType) []machineType {
	types := make([]machineType, len(vals))
	for i, val := range vals {
		types[i] = mtOf(val)
	}
	return types
}

// flushSuffix canonicalizes the stack and returns its logical types plus the
// physical slot where the last n logical operands begin. Logical depth and slot
// depth differ whenever values below the suffix include v128.
func (f *fn) flushSuffix(n int) ([]machineType, int) {
	f.flush()
	types := f.currentLogicalTypes()
	return types, slotOfLogicalTypes(types, len(types)-n)
}

func (f *fn) dropFlushedSuffix(types []machineType, n int) {
	f.setDepthTypes(types[:len(types)-n])
}

func (f *fn) frameDepthTypes(base, suffix []machineType) []machineType {
	out := f.tmpTypes[:0]
	out = append(out, base...)
	out = append(out, suffix...)
	f.tmpTypes = out
	return out
}

func (f *fn) setDepthTypes(types []machineType) {
	f.setDepthTypesWithGCRoots(types, nil)
}

// --- block types ---

const pollFreeLoopPhaseMaxLocals = 16

// --- control opcodes ---

const (
	// A record is [prev, saved SP, handler target, context anchor, tag, lanes...].
	// Root slots mirror [tag, lanes...] using the shared payload layout.
	// EH bodies keep the ordinary header; its spare word holds CustomCtx.
	frEHCustomCtxOff   = 0
	ehContextAnchorOff = 24 // descriptor zero, replacing the saved linear-memory word
	ehMaxPayloadWords  = shared.MaxEHTagPayloadWords
	ehRecordSlots      = 5 + shared.EHPayloadLanes
	ehRootSlots        = 1 + shared.EHPayloadLanes
	// Historical fixed reservation, kept for modules built without body bytes.
	legacyEHTryRecords  = 4
	legacyEHRootRecords = 4
	// Catch clauses are a growable list dispatched with ordinary branches;
	// the bound only keeps one try_table's dispatch within branch range.
	maxEHCatches         = 1024
	ehPrevOff            = 0
	ehSavedSPOff         = 8
	ehTargetOff          = 16
	ehTagOff             = 32
	offEHTagDirPtr       = abi.EHTagDirPtrOffset
	maxScaled64FrameDisp = 0xfff * 8
	ehReg                = X22
)

var ehStableInstanceContextOffsets = [...]int32{
	offTablePtr,
	offFuncRefDescPtr,
	offPassiveElemPtr,
	offGlobalsPtr,
	offPassiveDataPtr,
	offTableDirPtr,
	offMemoryDirPtr,
	offImportDispatchPtr,
}

// CustomCtx is the first ordinary context word and is activation-dynamic. The
// remaining ordinary prefix has the same order as this stable-field list. Make
// additions fail the build until this cold restore path is updated deliberately.
var _ [int(coreruntime.InstanceContextGCDomainOffset/8) - 1 - len(ehStableInstanceContextOffsets)]struct{}
var _ [len(ehStableInstanceContextOffsets) - (int(coreruntime.InstanceContextGCDomainOffset/8) - 1)]struct{}

// ehFrameRangeBase keeps the common small-frame path on a direct SP-relative
// LDR/STR while rebasing ranges which do not fit AArch64's scaled imm12 field.
func (f *fn) ehFrameRangeBase(scratch Reg, off, last int32) (base Reg, baseOff int32) {
	if off >= 0 && int64(off)+int64(last) <= maxScaled64FrameDisp {
		return SP, off
	}
	f.leaDisp(scratch, SP, off, true)
	return scratch, 0
}

func (f *fn) captureEHInstanceContext() {
	// X16 is the new record base; X17 is reserved scratch, never a live pin.
	// EH bodies retain the ordinary header and its otherwise unused first word.
	f.ld64(X17, linMemReg, -offCustomCtx)
	f.st64(SP, frEHCustomCtxOff, X17)
	// Descriptor zero owns both the stable home and native-context pointers.
	// Reuse the record's saved-linear-memory word so spills and roots never move.
	f.ld64(X17, linMemReg, -offFuncRefDescPtr)
	f.st64(X16, ehContextAnchorOff, X17)
}

func (f *fn) restoreEHInstanceContext(recordOff int32) {
	// Keep the record address encodable even when locals extend beyond 32 KiB.
	f.leaDisp(ehReg, SP, recordOff, true)
	f.ld64(X16, ehReg, ehContextAnchorOff)
	f.ld64(linMemReg, X16, coreruntime.TableEntryHomeLinMemOffset)
	f.ld64(X16, X16, coreruntime.FuncRefContextOffset)
	for i, basedataOff := range ehStableInstanceContextOffsets {
		f.ld64(X17, X16, int32((i+1)*8))
		f.st64(linMemReg, -basedataOff, X17)
	}
	f.ld64(X17, X16, coreruntime.InstanceContextEHTagDirOffset)
	f.st64(linMemReg, -int32(abi.EHTagDirPtrOffset), X17)
	f.ld64(X17, X16, coreruntime.InstanceContextGCNativeViewOffset)
	// The GC view is beyond STUR's signed displacement; only reserved scratch
	// registers are free here, because call-free local pins may remain live.
	f.leaDisp(X16, linMemReg, -int32(abi.GCNativeViewPtrOffset), true)
	f.st64(X16, 0, X17)
	// The activation-specific value wins over the mutable native image.
	f.ld64(X17, SP, frEHCustomCtxOff)
	f.st64(linMemReg, -offCustomCtx, X17)
}

func exceptionPayloadMachineType(typ wasm.ValType) (machineType, bool) {
	if wasm.EqualValType(typ, wasm.I32) || wasm.EqualValType(typ, wasm.I64) || wasm.EqualValType(typ, wasm.F32) || wasm.EqualValType(typ, wasm.F64) {
		return mtOf(typ), true
	}
	if typ.Kind() == wasm.ValRef {
		// One word; exception root maps decide which reference words the
		// collector scans.
		return mtI64, true
	}
	return mtNone, false
}

func moduleTagType(m *wasm.Module, index uint32) (wasm.TagType, bool) {
	for i := range m.Imports {
		im := &m.Imports[i]
		if im.Type.Kind != wasm.ExternTag {
			continue
		}
		if index == 0 {
			return im.Type.TagType(), true
		}
		index--
	}
	if int(index) >= len(m.Tags) {
		return wasm.TagType{}, false
	}
	return m.Tags[index], true
}

func (f *fn) opTryTable(r *wasm.Reader) error {
	paramTypes, resultTypes, frameTypes, res0, err := f.blockType(r)
	if err != nil {
		return err
	}
	n, err := r.U32()
	if err != nil {
		return err
	}
	if n > maxEHCatches {
		return fmt.Errorf("bounded exception handling supports at most %d catches per try_table", maxEHCatches)
	}
	pN, rN := len(paramTypes), len(resultTypes)
	if frameTypes == nil && res0 != mtNone {
		rN = 1
	}
	fr := ctrlFrame{kind: cfTry, paramN: pN, resultN: rN, branchN: uint32(rN), controlSite: -1, res0: res0, types: frameTypes}
	fr.set(ctrlEntryUnreachable, f.unreachable)
	eh := f.ensureFrameEH(&fr)
	for i := uint32(0); i < n; i++ {
		kindByte, err := r.Byte()
		if err != nil {
			return err
		}
		kind := wasm.CatchKind(kindByte)
		clause := ehCatchClause{kind: kind}
		switch kind {
		case wasm.CatchTag, wasm.CatchRef:
			clause.tag, err = r.U32()
			if err != nil {
				return err
			}
			tagType, ok := moduleTagType(f.m, clause.tag)
			if !ok {
				return fmt.Errorf("bounded exception handling catch tag %d is unavailable", clause.tag)
			}
			var ft wasm.CompType
			if !f.m.ResolveTypeFunc(tagType.Type.Index, &ft) || len(ft.Params) > ehMaxPayloadWords {
				return fmt.Errorf("bounded exception handling catch tag %d signature unavailable", clause.tag)
			}
			clause.scalarN = uint8(len(ft.Params))
			clause.payloadN = clause.scalarN
			for j, typ := range ft.Params {
				mt, ok := exceptionPayloadMachineType(typ)
				if !ok {
					return fmt.Errorf("bounded exception handling requires scalar or reference tag payloads")
				}
				if j == 0 {
					clause.firstType = mt
				}
				if shared.EHPayloadLane(f.m, typ, j) != j {
					clause.gcParams |= 1 << j
				}
			}
			if kind == wasm.CatchRef {
				if f.ehRootCount >= f.ehRootCap {
					return fmt.Errorf("exception root %d exceeds the %d reserved for this function", f.ehRootCount, f.ehRootCap)
				}
				clause.rootIndex = uint16(f.ehRootCount)
				f.ehRootCount++
				if clause.payloadN == 0 {
					clause.firstType = mtI64
				}
				clause.payloadN++
			}
		case wasm.CatchAll:
		case wasm.CatchAllRef:
			if f.ehRootCount >= f.ehRootCap {
				return fmt.Errorf("exception root %d exceeds the %d reserved for this function", f.ehRootCount, f.ehRootCap)
			}
			clause.rootIndex = uint16(f.ehRootCount)
			f.ehRootCount++
			clause.firstType = mtI64
			clause.payloadN = 1
		default:
			return fmt.Errorf("bounded exception handling rejects unknown catch kind %d", kind)
		}
		label, err := r.U32()
		if err != nil {
			return err
		}
		frame := len(f.ctrl) - 1 - int(label)
		if frame < 0 {
			return errBadLabel
		}
		clause.frame = uint32(frame)
		if f.ctrl[frame].branchArity() != int(clause.payloadN) {
			return fmt.Errorf("bounded exception handler payload arity mismatch")
		}
		if kind == wasm.CatchRef || kind == wasm.CatchAllRef {
			f.ensureFrameEH(&f.ctrl[frame]).refResults |= 1 << (clause.payloadN - 1)
		}
		f.ctrl[frame].set(ctrlRegMerge1, false)
		eh.catches = append(eh.catches, clause)
	}
	// An unreachable stack is polymorphic, so its depth may be below paramN.
	// Like block/loop/if, open the frame without a base-type prefix.
	if f.unreachable {
		f.pushCtrl(&fr)
		return nil
	}
	fr.height = f.depth() - fr.paramN
	f.setFrameBaseTypes(&fr, f.currentLogicalTypes()[:fr.height])
	if f.ehTryDepth >= f.ehTryCap {
		return fmt.Errorf("try_table record %d exceeds the %d reserved for this function", f.ehTryDepth, f.ehTryCap)
	}
	eh.recordIndex = uint16(f.ehTryDepth)
	f.ehTryDepth++
	f.reconcileLocals()
	f.flush()
	for i := range eh.catches {
		clause := &eh.catches[i]
		if clause.kind != wasm.CatchRef && clause.kind != wasm.CatchAllRef {
			continue
		}
		rootOff := f.ehRootOff(int(clause.rootIndex))
		// Root records follow the locals and may lie beyond LDR/STR's scaled
		// displacement. Small frames retain their original direct stores.
		base, baseOff := f.ehFrameRangeBase(X16, rootOff, (ehRootSlots-1)*8)
		for word := int32(0); word < ehRootSlots*8; word += 8 {
			f.st64(base, baseOff+word, ZR)
		}
		f.stats.peep("eh-root-init")
	}
	recordOff := f.ehRecordOff(int(eh.recordIndex))
	f.leaDisp(X16, SP, recordOff, true)
	f.st64(X16, ehPrevOff, ehReg)
	f.a.AddImm64(X17, SP, 0)
	f.st64(X16, ehSavedSPOff, X17)
	eh.targetSite = uint32(f.a.Adr(X17))
	f.recordPCRelative(int(eh.targetSite))
	f.st64(X16, ehTargetOff, X17)
	// An exceptional cross-instance return jumps directly to this frame and skips
	// the normal call continuation. Preserve the activation-dynamic values plus a
	// stable context anchor once per frame so any bounded handler can restore
	// shared-memory basedata without expanding every nested handler record.
	f.captureEHInstanceContext()
	f.a.MovReg64(ehReg, X16)
	f.pushCtrl(&fr)
	return nil
}

func (f *fn) opThrow(r *wasm.Reader) error {
	tag, err := r.U32()
	if err != nil {
		return err
	}
	tagType, ok := moduleTagType(f.m, tag)
	if !ok {
		return fmt.Errorf("bounded exception handling throw tag %d is unavailable", tag)
	}
	var ft wasm.CompType
	if !f.m.ResolveTypeFunc(tagType.Type.Index, &ft) || len(ft.Params) > ehMaxPayloadWords {
		return fmt.Errorf("bounded exception handling tag signature unavailable")
	}
	types := f.currentLogicalTypes()
	if len(types) < len(ft.Params) {
		return fmt.Errorf("bounded exception handling payload stack underflow")
	}
	f.reconcileLocals()
	if f.makesCalls && !f.usesCalls {
		// Catch routes reload eager pins, including throws within this frame.
		f.spillLocalsForCall()
	}
	f.flush()
	noHandler := f.zeroBranch(ehReg, true, true)
	// The handler restores its owner context and rederives cached globals from
	// cells. Publish deferred global.set values before that exceptional edge;
	// unhandled throws take the ordinary trap path, which already does this.
	f.storeDirtyGlobalPins(X16)
	f.ld64(X16, linMemReg, -int32(offEHTagDirPtr))
	f.ld64(X16, X16, int32(tag*8))
	f.st64(ehReg, ehTagOff, X16)
	base := len(types) - len(ft.Params)
	var gcLanes uint32
	for i, typ := range ft.Params {
		slot := slotOfLogicalTypes(types, base+i)
		lane := shared.EHPayloadLane(f.m, typ, i)
		gcLanes |= 1 << lane
		f.ld64(X16, SP, f.spillOff(slot))
		f.st64(ehReg, ehPayloadOff(lane), X16)
	}
	// GC lanes this tag leaves unused may hold an earlier exception's
	// references; clear them so a catch_all_ref root never scans a stale one.
	for lane := shared.EHGCLaneBase; lane < shared.EHPayloadLanes; lane++ {
		if gcLanes&(1<<lane) == 0 {
			f.st64(ehReg, ehPayloadOff(lane), ZR)
		}
	}
	f.ld64(X17, ehReg, ehSavedSPOff)
	f.ld64(X16, ehReg, ehTargetOff)
	f.a.AddImm64(SP, X17, 0)
	f.a.Br(X16)
	f.patchBranch19(noHandler, f.a.Len())
	f.trapAlways(trapUnhandledException)
	f.unreachable = true
	return nil
}

func (f *fn) opThrowRef() error {
	types := f.currentLogicalTypes()
	if len(types) == 0 || types[len(types)-1] != mtI64 {
		return fmt.Errorf("bounded exception handling throw_ref requires an exception reference")
	}
	refSlot := slotOfLogicalTypes(types, len(types)-1)
	f.reconcileLocals()
	if f.makesCalls && !f.usesCalls {
		// Catch routes reload eager pins, including throws within this frame.
		f.spillLocalsForCall()
	}
	f.flush()
	f.ld64(X16, SP, f.spillOff(refSlot))
	f.trapIfZero(X16, true, true, trapNullReference)
	noHandler := f.zeroBranch(ehReg, true, true)
	// X16 carries the exception object, so use the other fixed scratch while
	// making dirty pinned globals coherent for the handler's register reload.
	f.storeDirtyGlobalPins(X17)
	for off := int32(0); off < ehRootSlots*8; off += 8 {
		f.ld64(X17, X16, off)
		f.st64(ehReg, ehTagOff+off, X17)
	}
	f.ld64(X17, ehReg, ehSavedSPOff)
	f.ld64(X16, ehReg, ehTargetOff)
	f.a.AddImm64(SP, X17, 0)
	f.a.Br(X16)
	f.patchBranch19(noHandler, f.a.Len())
	f.trapAlways(trapUnhandledException)
	f.unreachable = true
	return nil
}

func (f *fn) emitEHCatchRoute(fr *ctrlFrame, clause *ehCatchClause) {
	target := &f.ctrl[int(clause.frame)]
	// emitEHHandler keeps the current record base in ehReg. Keep it there until
	// every payload is consumed; loading the previous handler earlier would force
	// large SP-relative accesses or require a third reserved scratch register.

	rootOff := int32(0)
	if clause.kind == wasm.CatchRef || clause.kind == wasm.CatchAllRef {
		rootOff = f.ehRootOff(int(clause.rootIndex))
		f.leaDisp(X17, SP, rootOff, true)
		for off := int32(ehTagOff); off < ehTagOff+ehRootSlots*8; off += 8 {
			f.ld64(X16, ehReg, off)
			f.st64(X17, off-ehTagOff, X16)
		}
	}

	loadPayload := func(reg Reg, i int) {
		if i == int(clause.scalarN) {
			f.a.MovReg64(reg, X17)
			return
		}
		f.ld64(reg, ehReg, ehPayloadOff(clause.lane(i)))
	}
	if target.has(ctrlRegMerge1) && clause.payloadN == 1 {
		if clause.scalarN == 0 {
			f.a.MovReg64(mergeReg, X17)
		} else if clause.firstType.isFloat() {
			f.fld(mergeFReg, ehReg, ehPayloadOff(0), clause.firstType == mtF64)
		} else {
			loadPayload(mergeReg, 0)
		}
	} else {
		toSlot := slotsOfTypes(f.frameBaseTypes(target))
		for i := 0; i < int(clause.payloadN); i++ {
			loadPayload(X16, i)
			f.st64(SP, f.spillOff(toSlot+i), X16)
		}
	}
	// The catch continuation owns the previous handler. Delay this load until the
	// record base is no longer needed for payload and rooted-reference copies.
	f.ld64(ehReg, ehReg, ehPrevOff)

	// A throw skips the ordinary post-call eager reload. Direct throws in this
	// frame publish the same slots before unwinding; call-free pins stay live.
	if f.makesCalls && !f.usesCalls {
		f.reloadLocalsForCall()
	}
	// Use the same local-indexed snapshot as control-frame merges. Whole-function
	// pins are only assigned within the range covered by packedLocStates; the
	// combined GP/FP pin count is not bounded by sixteen.
	var savedState packedLocStates
	if f.usesCalls {
		for x := range f.locals {
			if _, _, ok := f.pinReg(x); !ok {
				continue
			}
			savedState.set(x, f.locals[x].state)
			f.locals[x].state = lsMem
		}
	}
	f.convergeBranchLocals(target)
	f.restoreBranchHandlers(int(clause.frame))
	f.branchJump(target)
	if f.usesCalls {
		for x := range f.locals {
			if _, _, ok := f.pinReg(x); ok {
				f.locals[x].state = savedState.get(x)
			}
		}
	}
}

func (f *fn) emitEHHandler(fr *ctrlFrame) {
	eh := f.ensureFrameEH(fr)
	recordOff := f.ehRecordOff(int(eh.recordIndex))
	handlerPos := f.a.Len()
	if !f.a.PatchAdr(int(eh.targetSite), handlerPos) {
		panic("arm64: exception handler ADR out of range")
	}
	// A handler jump bypasses the normal cross-instance call continuation. Restore
	// every per-instance basedata pointer before dispatch so tag matching and the
	// catch continuation observe the owner, even when caller and callee share Memory.
	f.restoreEHInstanceContext(recordOff)
	if f.memSizeReg != regNone {
		f.loadMemoryCache()
	}
	f.deriveModuleGlobals()
	f.derivePinnedGlobals()
	// Keep one rebased record pointer through dispatch and matched catch routes.
	// EH records follow locals, so direct SP-relative loads would fail above 32 KiB.
	f.leaDisp(ehReg, SP, recordOff, true)

	dispatchN := len(eh.catches)
	for i := range eh.catches {
		clause := &eh.catches[i]
		if clause.kind == wasm.CatchAll || clause.kind == wasm.CatchAllRef {
			clause.matchSite = uint32(f.a.Branch())
			dispatchN = i + 1
			break
		}
		f.ld64(X16, ehReg, ehTagOff)
		f.ld64(X17, linMemReg, -int32(offEHTagDirPtr))
		f.ld64(X17, X17, int32(clause.tag*8))
		f.cmpRR(X16, X17, true)
		clause.matchSite = uint32(f.a.Bcond(condE))
	}

	// Forward unmatched exceptions with the two reserved backend scratches. X9
	// may hold a call-free pin that the outer catch legitimately keeps live. Keep
	// the record base in X16 so frames beyond LDR's 32 KiB scaled-offset range also
	// work, and use the soon-to-be-replaced EH register as the copy temporary.
	f.a.MovReg64(X16, ehReg)
	f.ld64(X17, X16, ehPrevOff)
	noPrevious := f.zeroBranch(X17, true, true)
	for off := int32(ehTagOff); off < ehTagOff+ehRootSlots*8; off += 8 {
		f.ld64(ehReg, X16, off)
		f.st64(X17, off, ehReg)
	}
	f.a.MovReg64(ehReg, X17)
	f.ld64(X16, X17, ehTargetOff)
	f.ld64(X17, X17, ehSavedSPOff)
	f.a.AddImm64(SP, X17, 0)
	f.a.Br(X16)
	f.patchBranch19(noPrevious, f.a.Len())
	f.trapAlways(trapUnhandledException)

	for i := 0; i < dispatchN; i++ {
		clause := &eh.catches[i]
		if clause.kind == wasm.CatchAll || clause.kind == wasm.CatchAllRef {
			f.patchBranch26(int(clause.matchSite), f.a.Len())
		} else {
			f.patchBranch19(int(clause.matchSite), f.a.Len())
		}
		f.emitEHCatchRoute(fr, clause)
	}
}

// restorePreviousEHHandler leaves the innermost try record without relying on a
// direct SP displacement. Large local frames can place every EH record beyond
// AArch64 LDR's 32 KiB scaled-immediate range.
func (f *fn) restorePreviousEHHandler(recordOff int32) {
	base, baseOff := f.ehFrameRangeBase(X16, recordOff, ehPrevOff)
	f.ld64(ehReg, base, baseOff+ehPrevOff)
}

func (f *fn) brOnNull(r *wasm.Reader) error {
	value := f.popValue()
	gcRoot := value.st.hasGCRoot()
	ref := f.materialize(value)
	idx, err := r.U32()
	if err != nil {
		return err
	}
	fi := len(f.ctrl) - 1 - int(idx)
	if fi < 0 {
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	d := f.depth()
	f.flush()
	refSlot := f.allocSpillSlot()
	f.st64(SP, f.spillOff(refSlot), ref)
	f.release(ref)
	over := f.zeroBranch(ref, true, false)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, fr.branchArity())
	}
	f.restoreBranchHandlers(fi)
	f.branchJump(fr)
	f.patchBranch19(over, f.a.Len())
	fallthroughRef := f.allocReg(0)
	f.ld64(fallthroughRef, SP, f.spillOff(refSlot))
	f.pushReg(fallthroughRef, mtI64).st.setGCRoot(gcRoot)
	return nil
}

func (f *fn) brOnNonNull(r *wasm.Reader) error {
	value := f.popValue()
	gcRoot := value.st.hasGCRoot()
	ref := f.materialize(value)
	f.pushReg(ref, mtI64).st.setGCRoot(gcRoot)
	idx, err := r.U32()
	if err != nil {
		return err
	}
	fi := len(f.ctrl) - 1 - int(idx)
	if fi < 0 {
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	allTypes := append([]machineType(nil), f.currentLogicalTypes()...)
	d := len(allTypes)
	refSlot := slotsOfTypes(allTypes) - 1
	f.flush()
	condition := f.allocReg(0)
	f.ld64(condition, SP, f.spillOff(refSlot))
	f.release(condition)
	over := f.zeroBranch(condition, true, true)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, fr.branchArity())
	}
	f.restoreBranchHandlers(fi)
	f.branchJump(fr)
	f.patchBranch19(over, f.a.Len())
	_ = f.popValue()
	return nil
}

// brOnCastResult consumes the helper's i32 match result while leaving the
// original reference at the top of the logical stack. Both edges retain the
// same compact reference identity; validation supplies only the refined type.
func (f *fn) brOnCastResult(idx uint32, branchOnMatch bool) error {
	matched, owned := f.materializeRead(f.popValue())
	fi := len(f.ctrl) - 1 - int(idx)
	if fi < 0 {
		if owned {
			f.release(matched)
		}
		return errBadLabel
	}
	fr := &f.ctrl[fi]
	f.convergeBranchLocals(fr)
	d := f.depth()
	f.flush()
	if owned {
		f.release(matched)
	}
	skipCond := condE
	if !branchOnMatch {
		skipCond = condNE
	}
	over := f.zeroBranch(matched, false, skipCond == condE)
	if fr.has(ctrlRegMerge1) {
		f.branchEdgeToMerge1(fr, d)
	} else {
		f.moveBranchValues(fr, d, fr.branchArity())
	}
	f.restoreBranchHandlers(fi)
	f.branchJump(fr)
	f.patchBranch19(over, f.a.Len())
	return nil
}

func skipImmediatesWithMemory64(r *wasm.Reader, op byte, memory64 bool) error {
	if memory64 {
		return wasm.SkipInstructionImmediateWithMemarg64(r, op, true)
	}
	return skipImmediates(r, op)
}

// brTableJumpMin is the label count at which br_table switches from a linear
// cmp/jne chain to an indirect jump table.
const brTableJumpMin = 5

func align4(n int) int { return (n + 3) &^ 3 }

// ehPayloadOff is the record offset of exception payload word i.
func ehPayloadOff(i int) int32 { return ehTagOff + 8 + int32(i)*8 }
