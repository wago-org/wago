//go:build arm64

package arm64

import (
	"fmt"
	"os"
	"sort"

	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// refreshCachedMemoryBoundAfterExternalCall reestablishes the memory-zero
// bounds cache after a continuation resumes from code outside this compilation
// context. The external code may have grown an aliased memory, and a
// cross-instance continuation must read from the restored caller basedata.
func (f *fn) refreshCachedMemoryBoundAfterExternalCall() {
	if f.memSizeReg != regNone {
		f.ld64(f.memSizeReg, linMemReg, -int32(bdCurBytes))
	}
}

// regABIEnabled turns on the register-based internal-call ABI (default on;
// WAGO_ARM64_NOREGABI=1 forces the wrapper ABI everywhere, for A/B measurement).
var regABIEnabled = os.Getenv("WAGO_ARM64_NOREGABI") != "1"

// noStackFence skips the per-entry stack-overflow fence check (A/B measurement).
var noStackFence = os.Getenv("WAGO_ARM64_NOFENCE") == "1"

// noStackReg disables the WARP STACK_REG lazy local model (reverts to spill-all/
// reload-all around calls, no branch reconcile) — A/B measurement.
var noStackReg = os.Getenv("WAGO_ARM64_NOSTACKREG") == "1"

// Function calls. Internal (wasm→wasm) calls use wago's WasmWrapper ABI: the
// arguments and result slots live in a native-stack buffer at SP; the callee is
// entered with X0=args, X1=linMem, X2=trap, X3=results — exactly what the
// prologue expects. Ported from WARP's call lowering but retargeted to wago's
// ABI/runtime (host imports adapt to wago's re-entry model, not WARP's
// synchronous native calls — the no-cgo constraint).

// callReloc records a Bl (BL) site whose imm26 must be patched to point at the
// target local function's entry once the module is laid out.
type callReloc struct {
	at       uint32 // byte offset of the BL instruction within this function's code
	target   uint32 // target local-function index (into m.Code)
	internal bool   // target the callee's register-ABI internal entry (else offset 0)
}

// callRelocTable stores every module relocation in source-function order. One
// pointer-free offset per function replaces a retained slice header per
// function; function slices are reconstructed only while finalizing the module.
type callRelocTable struct {
	offsets []uint32
	data    []callReloc
	next    uint32
	results []funcResult
	states  []workerState
}

func parallelCallRelocTable(results []funcResult, states []workerState) callRelocTable {
	return callRelocTable{results: results, states: states}
}

func newCallRelocTable(functions, capacity int) callRelocTable {
	if functions == 0 {
		return callRelocTable{}
	}
	return callRelocTable{
		offsets: make([]uint32, functions+1),
		data:    make([]callReloc, 0, capacity),
	}
}

func (t *callRelocTable) appendFunction(index int, relocs []callReloc) bool {
	if index < 0 || uint64(index) != uint64(t.next) || index+1 >= len(t.offsets) || uint64(len(t.data))+uint64(len(relocs)) > uint64(^uint32(0)) {
		return false
	}
	t.data = append(t.data, relocs...)
	t.offsets[index+1] = uint32(len(t.data))
	t.next++
	return true
}

// Hot finalization loops intentionally hoist the mode test and call these
// accessors directly. A combined accessor did not inline and cost 2.55% on the
// native many-functions compile benchmark.
func (t callRelocTable) serialFunction(index int) []callReloc {
	if index < 0 || index+1 >= len(t.offsets) {
		return nil
	}
	return t.data[t.offsets[index]:t.offsets[index+1]]
}

func (t callRelocTable) parallelFunction(index int) []callReloc {
	if index < 0 || index >= len(t.results) {
		return nil
	}
	r := t.results[index]
	if r.layoutFlags&layoutOmitted != 0 || int(r.worker) >= len(t.states) {
		return nil
	}
	return t.states[int(r.worker)].relocs[int(r.relocStart):int(r.relocEnd)]
}

func (t callRelocTable) functions() int {
	if t.results != nil {
		return len(t.results)
	}
	if len(t.offsets) == 0 {
		return 0
	}
	return len(t.offsets) - 1
}

const invalidCallRelocField = ^uint32(0)

func (f *fn) compactCallRelocField(value int) uint32 {
	if value < 0 || uint64(value) >= uint64(invalidCallRelocField) {
		f.setRepresentationLimit(functionRepresentationCallReloc)
		return 0
	}
	return uint32(value)
}

func (f *fn) newCallReloc(at, target int, internal bool) callReloc {
	return callReloc{
		at:       f.compactCallRelocField(at),
		target:   f.compactCallRelocField(target),
		internal: internal,
	}
}

// intArgRegs is the integer argument/result register order for the internal
// register-call ABI (our own convention, not the C ABI). X0/X1 carry args/linMem;
// X19-X23 hold pinned locals; linMemReg holds linMem. The single result returns in X0
// (AAPCS64 return register, also arg 0).
var intArgRegs = []Reg{X0, X1, X2, X3, X4, X5, X6, X7}
var fpArgRegs = []Reg{0, 1, 2, 3, 4, 5, 6, 7} // V0..V7; single float result returns in V0.

func isIntValType(t wasm.ValType) bool {
	return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64)
}

func preparedDirectIntSig(ft *wasm.CompType) bool {
	if len(ft.Params) > 4 || len(ft.Results) > 1 {
		return false
	}
	for _, typ := range ft.Params {
		if !isIntValType(typ) {
			return false
		}
	}
	for _, typ := range ft.Results {
		if !isIntValType(typ) {
			return false
		}
	}
	return true
}

func isFloatValType(t wasm.ValType) bool {
	return wasm.EqualValType(t, wasm.F32) || wasm.EqualValType(t, wasm.F64)
}

func sigIsIntOnly(ft *wasm.CompType) bool {
	for _, t := range ft.Params {
		if !isIntValType(t) {
			return false
		}
	}
	for _, t := range ft.Results {
		if !isIntValType(t) {
			return false
		}
	}
	return true
}

// sigFitsRegABI reports whether a signature can use the register ABI: integer-
// and float params are assigned to separate GP/V banks; one result returns in
// X0/V0, and the deliberately limited two-result form uses X0/X1 for integers.
func sigFitsRegABI(ft *wasm.CompType) bool {
	if len(ft.Results) > 2 {
		return false
	}
	if len(ft.Results) == 2 && (!isIntValType(ft.Results[0]) || !isIntValType(ft.Results[1])) {
		return false
	}
	gp, fp := 0, 0
	for _, t := range ft.Params {
		switch {
		case isIntValType(t):
			gp++
		case isFloatValType(t):
			fp++
		default:
			return false
		}
	}
	if gp > len(intArgRegs) || fp > len(fpArgRegs) {
		return false
	}
	for _, t := range ft.Results {
		if !isIntValType(t) && !isFloatValType(t) {
			return false
		}
	}
	return true
}

// sigFitsReferenceResultRegABI is the staged typed-tail extension of the native
// register ABI. It admits one funcref result in X0 with numeric parameters only.
// The descriptor pointer remains owned by the instance's bounded descriptor arena;
// no GC-managed reference or foreign wrapper result is admitted by this shape.
func sigFitsReferenceResultRegABI(ft *wasm.CompType) bool {
	if ft == nil || len(ft.Results) != 1 || !wasm.EqualValType(ft.Results[0], wasm.FuncRef) || len(ft.Params) > len(intArgRegs)+len(fpArgRegs) {
		return false
	}
	gp, fp := 0, 0
	for _, typ := range ft.Params {
		switch {
		case isIntValType(typ):
			gp++
		case isFloatValType(typ):
			fp++
		default:
			return false
		}
	}
	// Descriptor publication uses the cross-architecture staged classifier,
	// whose GP bank is capped at seven. Keep ARM64 aligned so eight-GP shapes
	// retain the wrapper fallback instead of requiring an unpublished internal tag.
	return gp < len(intArgRegs) && fp <= len(fpArgRegs)
}

func stagedTailRegisterABI(ft *wasm.CompType, staged bool) bool {
	return sigFitsRegABI(ft) || staged && sigFitsReferenceResultRegABI(ft)
}

func (f *fn) tailCallerUsesRegisterABI() bool {
	return f.opt(optRegABI) && stagedTailRegisterABI(f.ft, f.stagedTailDescriptors)
}

func tailResultABICompatible(a, b []wasm.ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		// Validation has already proved result subtyping. Native tail transfer only
		// needs the same physical register/slot class; all references are one i64
		// descriptor word even when their heap type or nullability differs.
		if mtOf(a[i]) != mtOf(b[i]) {
			return false
		}
	}
	return true
}

func sigFitsDirectCrossTailABI(ft *wasm.CompType) bool {
	return sigFitsRegABI(ft) && len(ft.Results) <= 2
}

func (f *fn) callOp(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	ft, ok := f.m.FuncSignature(idx)
	if !ok {
		return fmt.Errorf("call: unknown function %d", idx)
	}
	imported := f.m.ImportedFuncCount()
	if int(idx) < imported && f.customInstructions != nil {
		if custom, ok := f.customInstructions[idx]; ok && pluginARM64Lowering(custom) != nil {
			return f.emitCustomInstruction(custom, ft)
		}
	}
	// Auto-inlining (WAGO_INLINE): splice a straight-line leaf callee's body here
	// instead of emitting a call. The frame reserved the callee's locals past
	// f.nLocals in this caller; the splice binds params, zeroes declared locals, and
	// runs the body with localBase set. Straight-line callees touch no control frame,
	// so this is a pure operand-stack/local transform.
	if !f.inlineTargets.empty() {
		if t := f.inlineTargets.target(int(idx)); t != nil {
			if _, ok := f.inlineBase[int(idx)]; ok && !(t.inlineInLoopIsRegressive() && f.inCallSiteLoop()) {
				f.consumeGCFrameCallsite()
				return f.inlineCall(t)
			}
		}
	}
	if int(idx) < imported {
		if f.importBindings != nil && int(idx) < len(f.importBindings) && (f.importBindings[idx].Dynamic || f.importBindings[idx].CrossInstance) {
			return f.emitCrossInstanceCall(f.importBindings[idx], ft)
		}
		// A module with any returning host import uses the synchronous control
		// frame for ALL its host calls, so the async log and the control frame
		// never both occupy offCustomCtx. Otherwise void imports keep the cheaper
		// async log-and-replay path.
		if f.syncHostCalls || len(ft.Results) != 0 {
			return f.callHostSync(int(idx), ft) // synchronous re-entry
		}
		return f.callHost(int(idx), ft) // void: async log-and-replay
	}
	// `call f; local.set x` fusion: an int-only register-ABI call whose single
	// int result feeds a pinned local moves X0 straight into the local's
	// register — no intermediate result register, no separate set lowering.
	hint := -1
	if f.opt(optRegABI) && sigFitsRegABI(ft) && sigIsIntOnly(ft) && len(ft.Results) == 1 {
		r2 := *r // peek past the call without committing
		if b, err := r2.Byte(); err == nil && b == 0x21 {
			if x, err := r2.U32(); err == nil {
				if pr, isFloat, ok := f.pinReg(int(x)); ok && !isFloat && pr != regNone {
					// All operand-stack refs to x are flushed to slots by the call
					// sequence itself, so skipping setLocal's realizeLocalRefs is safe.
					f.stats.peep("call-localset-fuse")
					hint = int(x)
					if err := r.JumpTo(r2.Offset()); err != nil {
						return err
					}
				}
			}
		}
	}
	return f.callInternal(int(idx)-imported, ft, hint)
}

// returnCall lowers local proper-tail calls. Register-ABI targets receive their
// arguments directly in X/V registers; wider signatures use the fixed basedata
// tail bank and jump to the target wrapper. Both paths release the current native
// frame before branching, so recursive tail chains remain stack bounded.
func (f *fn) returnCall(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	ft, ok := f.m.FuncSignature(idx)
	if !ok {
		return fmt.Errorf("return_call: unknown function %d", idx)
	}
	if !tailResultABICompatible(f.ft.Results, ft.Results) {
		return fmt.Errorf("return_call: target %d result shape differs from caller", idx)
	}
	imported := f.m.ImportedFuncCount()
	if int(idx) < imported {
		if f.importBindings != nil && int(idx) < len(f.importBindings) {
			binding := f.importBindings[idx]
			if binding.Dynamic || binding.CrossInstance {
				if !sigFitsDirectCrossTailABI(f.ft) || !sigFitsDirectCrossTailABI(ft) {
					return fmt.Errorf("return_call: imported target %d requires unsupported cross-instance tail ABI", idx)
				}
				return f.emitTailDynamicImportJump(ft, binding)
			}
		}
		var callErr error
		if f.syncHostCalls || len(ft.Results) != 0 {
			callErr = f.callHostSync(int(idx), ft)
		} else {
			callErr = f.callHost(int(idx), ft)
		}
		if callErr != nil {
			return callErr
		}
		return f.opReturn()
	}
	f.stats.call("tail-direct")
	target := int(idx) - imported
	callerRegisterABI := stagedTailRegisterABI(f.ft, f.stagedTailDescriptors)
	targetRegisterABI := stagedTailRegisterABI(ft, f.stagedTailDescriptors)
	if f.opt(optRegABI) && targetRegisterABI {
		jump := func() {
			site := f.a.Branch()
			f.relocs = append(f.relocs, f.newCallReloc(site, target, true))
		}
		if callerRegisterABI {
			f.emitTailRegisterJump(ft, jump)
		} else {
			f.emitTailWrapperToRegisterJump(ft, jump)
		}
	} else {
		if slots := funcTypeSlots(ft.Params); slots > abi.TailArgsSlots {
			return fmt.Errorf("return_call: target %d requires %d wrapper argument slots, limit %d", idx, slots, abi.TailArgsSlots)
		}
		f.emitTailWrapperJump(ft, func() {
			site := f.a.Branch()
			f.relocs = append(f.relocs, f.newCallReloc(site, target, false))
		})
	}
	f.unreachable = true
	return nil
}

func (f *fn) discardEHHandlersForTail() {
	if f.ehTryDepth != 0 {
		f.ld64(ehReg, SP, f.ehRecordOff(0)+ehPrevOff)
	}
}

func (f *fn) emitTailFrameRelease() {
	f.discardEHHandlersForTail()
	at := f.a.Len()
	f.tailFrameSites = append(f.tailFrameSites, at)
	f.a.Movz64(X16, 0, 0)
	f.a.Movk64(X16, 0, 1)
	f.a.AddSPReg(X16)
	if f.usesCalls {
		f.a.LdpPost(FP, LR, SP, 16)
	}
}

func (f *fn) emitTailRegisterJump(ft *wasm.CompType, emitJump func()) {
	p := len(ft.Params)
	roots := f.rootsBottomToTop()
	if len(roots) < p {
		panic("arm64 tail call operand underflow")
	}
	types := make([]machineType, len(roots))
	for i, root := range roots {
		types[i] = rootMachineType(root)
	}
	f.flush()
	f.storePinnedGlobals(false)
	f.storeModuleGlobals(X16)
	argBase := len(types) - p
	gp, fp := 0, 0
	for i, typ := range ft.Params {
		slot := slotOfLogicalTypes(types, argBase+i)
		mt := mtOf(typ)
		if mt.isFloat() {
			f.fld(fpArgRegs[fp], SP, f.spillOff(slot), mt == mtF64)
			fp++
		} else {
			f.ld64(intArgRegs[gp], SP, f.spillOff(slot))
			gp++
		}
	}
	f.emitTailFrameRelease()
	emitJump()
}

func (f *fn) emitTailWrapperToRegisterJump(ft *wasm.CompType, emitJump func()) {
	p := len(ft.Params)
	roots := f.rootsBottomToTop()
	if len(roots) < p {
		panic("arm64 tail wrapper-to-register operand underflow")
	}
	types := make([]machineType, len(roots))
	for i, root := range roots {
		types[i] = rootMachineType(root)
	}
	f.flush()
	f.storePinnedGlobals(false)
	f.storeModuleGlobals(X16)
	argBase := len(types) - p
	gp, fp := 0, 0
	for i, typ := range ft.Params {
		slot := slotOfLogicalTypes(types, argBase+i)
		mt := mtOf(typ)
		if mt.isFloat() {
			f.fld(fpArgRegs[fp], SP, f.spillOff(slot), mt == mtF64)
			fp++
		} else {
			f.ld64(intArgRegs[gp], SP, f.spillOff(slot))
			gp++
		}
	}
	f.ld64(X12, SP, frResultsOff)
	f.emitTailFrameRelease()
	f.a.SubSP64(32)
	f.st64(SP, 0, LR)
	f.st64(SP, 8, X12)
	trampolineADR := f.a.Adr(LR)
	f.recordPCRelative(trampolineADR)
	emitJump()

	trampoline := f.a.Len()
	f.a.PatchAdr(trampolineADR, trampoline)
	f.ld64(X3, SP, 8)
	if len(ft.Results) > 0 {
		if isFloatValType(ft.Results[0]) {
			f.a.FStoreDisp(X3, 0, 0, wasm.EqualValType(ft.Results[0], wasm.F64))
		} else {
			f.st64(X3, 0, X0)
		}
	}
	if len(ft.Results) > 1 {
		f.st64(X3, 8, X1)
	}
	f.ld64(LR, SP, 0)
	f.a.AddSP64(32)
	f.a.Ret()
}

func (f *fn) emitTailWrapperJump(ft *wasm.CompType, emitJump func()) {
	p := len(ft.Params)
	roots := f.rootsBottomToTop()
	if len(roots) < p {
		panic("arm64 tail wrapper operand underflow")
	}
	types := make([]machineType, len(roots))
	for i, root := range roots {
		types[i] = rootMachineType(root)
	}
	f.flush()
	f.storePinnedGlobals(false)
	f.storeModuleGlobals(X16)
	f.a.MovReg64(X0, linMemReg)
	f.leaDisp(X0, X0, -int32(abi.TailArgsOffset), true)
	argBase := len(types) - p
	dstSlot := 0
	for i, typ := range ft.Params {
		srcSlot := slotOfLogicalTypes(types, argBase+i)
		n := mtOf(typ).stackSlots()
		for j := 0; j < n; j++ {
			f.ld64(X16, SP, f.spillOff(srcSlot+j))
			f.st64(X0, int32((dstSlot+j)*8), X16)
		}
		dstSlot += n
	}
	f.ld64(X2, linMemReg, -int32(abi.TrapCellPtrOffset))
	f.a.MovReg64(X1, linMemReg)
	if !f.tailCallerUsesRegisterABI() {
		f.ld64(X3, SP, frResultsOff)
		f.emitTailFrameRelease()
		emitJump()
		return
	}

	f.emitTailFrameRelease()
	adapterPC := f.a.Adr(X16)
	f.recordPCRelative(adapterPC)
	f.a.PatchAdr(adapterPC, f.adapterReturnOff)
	if f.policy.CompactNative {
		f.adapterReturnReferenced = true
	}
	f.cmpRR(LR, X16, true)
	nested := f.a.Bcond(condNE)
	f.a.LdpPost(LR, X3, SP, 16)
	emitJump()

	f.a.PatchBranch19(nested, f.a.Len())
	f.a.SubSP64(32)
	f.st64(SP, 0, LR)
	f.a.LeaSP(X3, 16)
	trampolineADR := f.a.Adr(LR)
	f.recordPCRelative(trampolineADR)
	emitJump()

	trampoline := f.a.Len()
	f.a.PatchAdr(trampolineADR, trampoline)
	f.ld64(LR, SP, 0)
	if len(ft.Results) > 0 {
		if isFloatValType(ft.Results[0]) {
			f.fld(0, SP, 16, wasm.EqualValType(ft.Results[0], wasm.F64))
		} else {
			f.ld64(X0, SP, 16)
		}
	}
	if len(ft.Results) > 1 {
		f.ld64(X1, SP, 24)
	}
	f.a.AddSP64(32)
	f.a.Ret()
}

// emitTailDynamicImportJump transfers a register-ABI tail call through the
// runtime import-dispatch tuple. A target wrapper entered from another wrapper
// reuses that wrapper's LR/result record; a direct internal caller receives one
// fixed trampoline record that restores its instance context and result
// registers. Repeated cross-instance tails therefore do not accumulate frames.
func (f *fn) emitTailDynamicImportJump(ft *wasm.CompType, b ImportBinding) error {
	if b.Dynamic && b.ImportIndex > uint32((1<<31-1-runtime.ImportDispatchCallerContextOffset)/runtime.ImportDispatchEntryBytes) {
		return fmt.Errorf("return_call: import dispatch index %d overflows displacement", b.ImportIndex)
	}
	p := len(ft.Params)
	roots := f.rootsBottomToTop()
	if len(roots) < p {
		panic("arm64 cross-tail operand underflow")
	}
	types := make([]machineType, len(roots))
	for i, root := range roots {
		types[i] = rootMachineType(root)
	}
	f.flush()
	f.storePinnedGlobals(false)
	f.storeModuleGlobals(X16)

	if b.Dynamic {
		disp := int32(b.ImportIndex * runtime.ImportDispatchEntryBytes)
		f.ld64(X16, linMemReg, -int32(offImportDispatchPtr))
		f.ld64(X10, X16, disp+runtime.ImportDispatchHomeLinMemOffset)
		f.ld64(X11, X16, disp+runtime.ImportDispatchTargetContextOffset)
		f.ld64(X17, X16, disp+runtime.ImportDispatchCodePtrOffset)
		f.ld64(X12, X16, disp+runtime.ImportDispatchCallerContextOffset)
	} else {
		f.a.MovImm64(X10, b.CalleeLinMem)
		f.a.MovImm64(X17, b.CalleeEntry)
		f.ld64(X12, linMemReg, -int32(offFuncRefDescPtr))
		f.ld64(X12, X12, runtime.FuncRefContextOffset)
		f.ld64(X11, X10, -int32(offFuncRefDescPtr))
		f.ld64(X11, X11, runtime.FuncRefContextOffset)
	}
	for _, reg := range []Reg{X10, X11, X12, X17} {
		f.pinned = f.pinned.add(reg)
	}
	f.a.MovReg64(X0, X10)
	f.leaDisp(X0, X0, -int32(abi.TailArgsOffset), true)
	argBase := len(types) - p
	dstSlot := 0
	for i, typ := range ft.Params {
		srcSlot := slotOfLogicalTypes(types, argBase+i)
		n := mtOf(typ).stackSlots()
		for j := 0; j < n; j++ {
			f.ld64(X16, SP, f.spillOff(srcSlot+j))
			f.st64(X0, int32((dstSlot+j)*8), X16)
		}
		dstSlot += n
	}
	for _, reg := range []Reg{X10, X11, X12, X17} {
		f.pinned = f.pinned.remove(reg)
	}
	f.emitTailFrameRelease()

	transfer := func() {
		f.copyInstanceContext(X10, X11)
		f.ld64(X9, linMemReg, -int32(offTrapHandlerPtr))
		f.st64(X10, -int32(offTrapHandlerPtr), X9)
		f.ld64(X9, linMemReg, -int32(offTrapStackReentry))
		f.st64(X10, -int32(offTrapStackReentry), X9)
		f.ld64(X9, linMemReg, -int32(offStackFence))
		f.st64(X10, -int32(offStackFence), X9)
		f.ld64(X9, linMemReg, -int32(offTrapCellPtr))
		f.st64(X10, -int32(offTrapCellPtr), X9)
		f.a.MovReg64(X0, X10)
		f.leaDisp(X0, X0, -int32(abi.TailArgsOffset), true)
		f.a.MovReg64(X1, X10)
		f.ld64(X2, X10, -int32(offTrapCellPtr))
		f.a.Br(X17)
	}

	// A register-ABI function entered through its own wrapper can discard the
	// wrapper record as well, preserving the outer LR/results destination.
	adapterPC := f.a.Adr(X16)
	f.recordPCRelative(adapterPC)
	f.a.PatchAdr(adapterPC, f.adapterReturnOff)
	if f.policy.CompactNative {
		f.adapterReturnReferenced = true
	}
	f.cmpRR(LR, X16, true)
	nested := f.a.Bcond(condNE)
	f.a.LdpPost(LR, X3, SP, 16)
	transfer()

	f.a.PatchBranch19(nested, f.a.Len())
	// [LR, caller linMem, caller context, pad, result0, result1, pad, pad].
	f.a.SubSP64(64)
	f.st64(SP, 0, LR)
	f.st64(SP, 8, linMemReg)
	f.st64(SP, 16, X12)
	f.a.LeaSP(X3, 32)
	trampolineADR := f.a.Adr(LR)
	f.recordPCRelative(trampolineADR)
	transfer()

	trampoline := f.a.Len()
	f.a.PatchAdr(trampolineADR, trampoline)
	f.ld64(LR, SP, 0)
	f.ld64(X10, SP, 8)
	f.ld64(X11, SP, 16)
	f.copyInstanceContext(X10, X11)
	f.a.MovReg64(linMemReg, X10)
	f.refreshCachedMemoryBoundAfterExternalCall()
	f.deriveModuleGlobals()
	f.derivePinnedGlobals()
	if len(ft.Results) > 0 {
		if isFloatValType(ft.Results[0]) {
			f.fld(0, SP, 32, wasm.EqualValType(ft.Results[0], wasm.F64))
		} else {
			f.ld64(X0, SP, 32)
		}
	}
	if len(ft.Results) > 1 {
		f.ld64(X1, SP, 40)
	}
	f.a.AddSP64(64)
	f.a.Ret()
	f.unreachable = true
	return nil
}

func arm64FuncTypeCarriesGCRefs(m *wasm.Module, ft *wasm.CompType) bool {
	if ft == nil {
		return false
	}
	for _, typ := range ft.Params {
		if arm64GCFrameRefType(m, typ) {
			return true
		}
	}
	for _, typ := range ft.Results {
		if arm64GCFrameRefType(m, typ) {
			return true
		}
	}
	return false
}

func (f *fn) returnCallRef(r *wasm.Reader) error {
	typeIdx, err := r.U32()
	if err != nil {
		return err
	}
	return f.returnCallRefType(typeIdx)
}

func (f *fn) returnCallRefType(typeIdx uint32) error {
	ft, ok := f.m.TypeFunc(typeIdx)
	if !ok {
		return fmt.Errorf("return_call_ref: bad type %d", typeIdx)
	}
	if !tailResultABICompatible(f.ft.Results, ft.Results) {
		return fmt.Errorf("return_call_ref: type %d result shape differs from caller", typeIdx)
	}
	callerRegABI := stagedTailRegisterABI(f.ft, f.stagedTailDescriptors)
	targetRegABI := stagedTailRegisterABI(ft, f.stagedTailDescriptors)
	canon, ok := f.m.StructuralTypeKeyChecked(typeIdx)
	if !ok {
		return fmt.Errorf("return_call_ref: type %d exceeds bounded native identity", typeIdx)
	}
	refValue := f.popValue()
	if refValue.elemKind() == ekValue && refValue.st.kind == stFuncRef && refValue.st.idx < uint32(f.m.ImportedFuncCount()) {
		importIndex := refValue.st.index()
		if f.importBindings != nil && importIndex < len(f.importBindings) {
			binding := f.importBindings[importIndex]
			if binding.Dynamic || binding.CrossInstance {
				if slots := funcTypeSlots(ft.Params); slots > abi.TailArgsSlots {
					return fmt.Errorf("return_call_ref: type %d requires %d wrapper argument slots, limit %d", typeIdx, slots, abi.TailArgsSlots)
				}
				f.stats.call("tail-ref-foreign")
				return f.emitTailDynamicImportJump(ft, binding)
			}
		}
		var callErr error
		if f.syncHostCalls || len(ft.Results) != 0 {
			callErr = f.callHostSync(importIndex, ft)
		} else {
			callErr = f.callHost(importIndex, ft)
		}
		if callErr != nil {
			return callErr
		}
		return f.opReturn()
	}

	if slots := funcTypeSlots(ft.Params); slots > abi.TailArgsSlots {
		return fmt.Errorf("return_call_ref: type %d requires %d wrapper argument slots, limit %d", typeIdx, slots, abi.TailArgsSlots)
	}
	ref := f.materialize(refValue)
	f.pinned = f.pinned.add(ref)
	f.trapIfZero(ref, true, true, trapIndirectOOB)
	code := f.allocReg(0)
	f.ld64(code, ref, runtime.TableEntryCodePtrOffset)
	f.trapIfZero(code, true, true, trapIndirectOOB)
	f.checkCallType(ref, runtime.TableEntrySigKeyOffset, canon, maskOf(ref, code))
	home := f.allocReg(maskOf(ref, code))
	f.ld64(home, ref, runtime.TableEntryHomeLinMemOffset)
	targetContext := f.allocReg(maskOf(ref, code, home))
	f.ld64(targetContext, ref, runtime.FuncRefContextOffset)
	f.trapIfZero(targetContext, true, true, trapTailUnsupported)
	if arm64FuncTypeCarriesGCRefs(f.m, ft) {
		targetDomain := f.allocReg(maskOf(ref, code, home, targetContext))
		f.ld64(targetDomain, targetContext, runtime.InstanceContextGCDomainOffset)
		f.trapIfZero(targetDomain, true, true, trapTailUnsupported)
		callerDesc := f.allocReg(maskOf(ref, code, home, targetContext, targetDomain))
		f.ld64(callerDesc, linMemReg, -int32(offFuncRefDescPtr))
		f.trapIfZero(callerDesc, true, true, trapTailUnsupported)
		f.ld64(callerDesc, callerDesc, runtime.FuncRefContextOffset)
		f.ld64(callerDesc, callerDesc, runtime.InstanceContextGCDomainOffset)
		f.cmpRR(targetDomain, callerDesc, true)
		f.release(callerDesc)
		f.release(targetDomain)
		f.trapIf(condNE, trapTailUnsupported)
	}
	f.pinned = f.pinned.remove(ref)
	f.release(ref)

	roots := f.rootsBottomToTop()
	types := make([]machineType, len(roots))
	gcRoots := gcRootFlags(roots)
	for i, root := range roots {
		types[i] = rootMachineType(root)
	}
	f.pinned = f.pinned.add(code).add(home).add(targetContext)
	f.flush()
	savedLocals := append([]localDef(nil), f.locals...)
	kind := f.descriptorEntryKind(home, maskOf(code, home, targetContext))
	f.stripDescriptorHomeTags(home)
	f.a.MovReg64(X17, code)
	f.a.MovReg64(X10, home)
	f.a.MovReg64(X11, targetContext)
	f.a.MovReg64(X13, kind)
	f.pinned = f.pinned.remove(code).remove(home).remove(targetContext)
	f.release(code)
	f.release(home)
	f.release(targetContext)
	f.release(kind)

	if f.opt(optRegABI) && targetRegABI {
		f.cmpImm(X13, uint32(abi.FuncRefInternalTagValue), true)
		wrapper := f.a.Bcond(condNE)
		f.cmpRR(X10, linMemReg, true)
		f.trapIf(condNE, trapTailUnsupported)
		if callerRegABI {
			f.emitTailRegisterJump(ft, func() { f.a.Br(X17) })
		} else {
			f.emitTailWrapperToRegisterJump(ft, func() { f.a.Br(X17) })
		}

		f.a.PatchBranch19(wrapper, f.a.Len())
		f.locals = savedLocals
		f.setDepthTypesWithGCRoots(types, gcRoots)
	}
	f.validateWrapperDescriptor(X13, X10)
	if arm64FuncTypeCarriesGCRefs(f.m, ft) {
		// Runtime-owned GC host thunks use the active caller context and can
		// reuse the ordinary wrapper-tail result pointer without a restoration
		// record. Ordinary host descriptors fail the domain check above.
		f.cmpImm(X13, uint32(abi.FuncRefHostThunkTagValue), true)
		notHost := f.a.Bcond(condNE)
		f.emitTailWrapperJump(ft, func() { f.a.Br(X17) })

		f.a.PatchBranch19(notHost, f.a.Len())
		f.locals = savedLocals
		f.setDepthTypesWithGCRoots(types, gcRoots)
	}
	f.emitTailDescriptorWrapperJump(ft)
	f.unreachable = true
	return nil
}

// emitTailDescriptorWrapperJump transfers the current activation through the
// descriptor target held in X17(code), X10(home linmem), and X11(context). It
// uses one fixed nested return record and therefore remains stack-bounded across
// arbitrary mutable-table and imported-reference tail chains.
func (f *fn) emitTailDescriptorWrapperJump(ft *wasm.CompType) {
	p := len(ft.Params)
	roots := f.rootsBottomToTop()
	types := make([]machineType, len(roots))
	for i, root := range roots {
		types[i] = rootMachineType(root)
	}
	f.flush()
	f.storePinnedGlobals(false)
	f.storeModuleGlobals(X16)

	f.ld64(X12, linMemReg, -int32(offFuncRefDescPtr))
	f.ld64(X12, X12, runtime.FuncRefContextOffset)
	f.a.MovReg64(X0, X10)
	f.leaDisp(X0, X0, -int32(abi.TailArgsOffset), true)
	argBase := len(types) - p
	dstSlot := 0
	for i, typ := range ft.Params {
		srcSlot := slotOfLogicalTypes(types, argBase+i)
		n := mtOf(typ).stackSlots()
		for j := 0; j < n; j++ {
			f.ld64(X16, SP, f.spillOff(srcSlot+j))
			f.st64(X0, int32((dstSlot+j)*8), X16)
		}
		dstSlot += n
	}

	transfer := func() {
		f.copyInstanceContext(X10, X11)
		f.ld64(X9, linMemReg, -int32(offTrapHandlerPtr))
		f.st64(X10, -int32(offTrapHandlerPtr), X9)
		f.ld64(X9, linMemReg, -int32(offTrapStackReentry))
		f.st64(X10, -int32(offTrapStackReentry), X9)
		f.ld64(X9, linMemReg, -int32(offStackFence))
		f.st64(X10, -int32(offStackFence), X9)
		f.ld64(X9, linMemReg, -int32(offTrapCellPtr))
		f.st64(X10, -int32(offTrapCellPtr), X9)
		f.a.MovReg64(X0, X10)
		f.leaDisp(X0, X0, -int32(abi.TailArgsOffset), true)
		f.a.MovReg64(X1, X10)
		f.ld64(X2, X10, -int32(offTrapCellPtr))
		f.a.Br(X17)
	}

	// A wrapper-ABI caller has no adapter record below its frame. Preserve its
	// original result destination from the frame header and tail-enter the target
	// wrapper directly. Register-ABI callers still need the run-time adapter/direct
	// distinction below because only adapter entry carries an outer X3 record.
	if !f.tailCallerUsesRegisterABI() {
		f.ld64(X3, SP, frResultsOff)
		f.emitTailFrameRelease()
		transfer()
		return
	}

	f.emitTailFrameRelease()
	adapterPC := f.a.Adr(X16)
	f.recordPCRelative(adapterPC)
	f.a.PatchAdr(adapterPC, f.adapterReturnOff)
	if f.policy.CompactNative {
		f.adapterReturnReferenced = true
	}
	f.cmpRR(LR, X16, true)
	nested := f.a.Bcond(condNE)
	f.a.LdpPost(LR, X3, SP, 16)
	transfer()

	f.a.PatchBranch19(nested, f.a.Len())
	f.a.SubSP64(64)
	f.st64(SP, 0, LR)
	f.st64(SP, 8, linMemReg)
	f.st64(SP, 16, X12)
	f.a.LeaSP(X3, 32)
	trampolineADR := f.a.Adr(LR)
	f.recordPCRelative(trampolineADR)
	transfer()

	trampoline := f.a.Len()
	f.a.PatchAdr(trampolineADR, trampoline)
	f.ld64(LR, SP, 0)
	f.ld64(X10, SP, 8)
	f.ld64(X11, SP, 16)
	f.copyInstanceContext(X10, X11)
	f.a.MovReg64(linMemReg, X10)
	f.refreshCachedMemoryBoundAfterExternalCall()
	f.deriveModuleGlobals()
	f.derivePinnedGlobals()
	if len(ft.Results) > 0 {
		if isFloatValType(ft.Results[0]) {
			f.fld(0, SP, 32, wasm.EqualValType(ft.Results[0], wasm.F64))
		} else {
			f.ld64(X0, SP, 32)
		}
	}
	if len(ft.Results) > 1 {
		f.ld64(X1, SP, 40)
	}
	f.a.AddSP64(64)
	f.a.Ret()
}

func (f *fn) returnCallIndirect(r *wasm.Reader) error {
	typeIdx, err := r.U32()
	if err != nil {
		return err
	}
	tableIdx, err := r.U32()
	if err != nil {
		return err
	}
	ft, ok := f.m.TypeFunc(typeIdx)
	if !ok {
		return fmt.Errorf("return_call_indirect: bad type %d", typeIdx)
	}
	if !tailResultABICompatible(f.ft.Results, ft.Results) {
		return fmt.Errorf("return_call_indirect: type %d result shape differs from caller", typeIdx)
	}
	callerRegisterTail := f.opt(optRegABI) && stagedTailRegisterABI(f.ft, f.stagedTailDescriptors)
	targetRegisterTail := f.opt(optRegABI) && stagedTailRegisterABI(ft, f.stagedTailDescriptors)
	if !targetRegisterTail && funcTypeSlots(ft.Params) > abi.TailArgsSlots {
		return fmt.Errorf("return_call_indirect: type %d requires %d wrapper argument slots, limit %d", typeIdx, funcTypeSlots(ft.Params), abi.TailArgsSlots)
	}
	if f.stagedTailDescriptors && f.importBindings != nil {
		idx := f.materialize(f.popValue())
		f.canonicalizeTableOperand(idx, tableIdx)
		f.pinned = f.pinned.add(idx)
		tbl := f.allocReg(0)
		f.loadTableDescriptor(tbl, tableIdx)
		ln := f.allocReg(maskOf(tbl))
		f.ld32(ln, tbl, 0)
		f.cmpRR(idx, ln, f.tableAddr64(tableIdx))
		f.release(ln)
		f.trapIf(condAE, trapIndirectOOB)
		f.a.LslImm(idx, idx, 5, true)
		f.a.Add64(idx, idx, tbl)
		f.ld64(idx, idx, 8+runtime.TableEntryRefSlotOffset)
		f.release(tbl)
		f.pinned = f.pinned.remove(idx)
		f.pushReg(idx, mtI64)
		return f.returnCallRefType(typeIdx)
	}
	canon, ok := f.m.StructuralTypeKeyChecked(typeIdx)
	if !ok {
		return fmt.Errorf("return_call_indirect: type %d exceeds bounded native identity", typeIdx)
	}
	idx := f.materialize(f.popValue())
	f.canonicalizeTableOperand(idx, tableIdx)
	f.pinned = f.pinned.add(idx)
	tbl := f.allocReg(0)
	f.loadTableDescriptor(tbl, tableIdx)
	ln := f.allocReg(maskOf(tbl))
	f.ld32(ln, tbl, 0)
	f.cmpRR(idx, ln, f.tableAddr64(tableIdx))
	f.release(ln)
	f.trapIf(condAE, trapIndirectOOB)
	f.a.LslImm(idx, idx, 5, true)
	f.a.Add64(idx, idx, tbl)
	f.release(tbl)
	code := f.allocReg(maskOf(idx))
	f.ld64(code, idx, 8)
	f.trapIfZero(code, true, true, trapIndirectOOB)
	if !f.immutableTableTyped || f.immutableTableType != canon {
		got := f.allocReg(maskOf(idx, code))
		f.ld64(got, idx, 16)
		want := f.allocReg(maskOf(idx, code, got))
		f.a.MovImm64(want, canon)
		f.cmpRR(got, want, true)
		f.release(want)
		f.release(got)
		f.trapIf(condNE, trapIndirectSig)
	}
	home := f.allocReg(maskOf(idx, code))
	f.ld64(home, idx, 8+runtime.TableEntryHomeLinMemOffset)
	kind := f.descriptorEntryKind(home, maskOf(idx, code, home))
	f.stripDescriptorHomeTags(home)
	f.cmpRR(home, linMemReg, true)
	f.trapIf(condNE, trapTailUnsupported)
	wantKind := uint32(abi.FuncRefLocalWrapperTagValue)
	if targetRegisterTail {
		wantKind = uint32(abi.FuncRefInternalTagValue)
	}
	f.cmpImm(kind, wantKind, true)
	f.trapIf(condNE, trapTailUnsupported)
	f.release(kind)
	f.release(home)
	f.pinned = f.pinned.remove(idx)
	f.release(idx)
	f.st64(linMemReg, -int32(offSpillRegion), code)
	f.release(code)
	jump := func() {
		f.ld64(X16, linMemReg, -int32(offSpillRegion))
		f.a.Br(X16)
	}
	if targetRegisterTail {
		if callerRegisterTail {
			f.emitTailRegisterJump(ft, jump)
		} else {
			f.emitTailWrapperToRegisterJump(ft, jump)
		}
	} else {
		if slots := funcTypeSlots(ft.Params); slots > abi.TailArgsSlots {
			return fmt.Errorf("return_call_indirect: type %d requires %d wrapper argument slots, limit %d", typeIdx, slots, abi.TailArgsSlots)
		}
		f.emitTailWrapperJump(ft, jump)
	}
	f.unreachable = true
	return nil
}

func (f *fn) emitCustomInstruction(custom railcore.CustomInstruction, ft *wasm.CompType) error {
	start := f.a.Len()
	err := f.emitPluginARM64(pluginARM64Lowering(custom), custom.InputWidths, custom.ResultWidth, len(ft.Results), custom.CustomInputs, custom.CustomOutput)
	if err == nil {
		f.recordOpaquePlugin(start, f.a.Len())
	}
	return err
}

// inCallSiteLoop reports whether the current call site is nested in a Wasm loop.
func (f *fn) inCallSiteLoop() bool {
	for i := len(f.ctrl) - 1; i >= 0; i-- {
		if f.ctrl[i].kind == cfLoop {
			return true
		}
	}
	return false
}

// callHost lowers a call to a VOID imported (host) function. Native wasm code
// cannot call back into Go without cgo, so the call is LOGGED to an in-memory
// buffer (at [linMem-offCustomCtx]) and replayed on the Go stack after the wasm
// function returns. Fire-and-forget: no result. Returning imports take the
// synchronous re-entry path instead (callHostSync). The caller (emitCall) routes
// by result arity, so ft here always has zero results.
func (f *fn) callHost(importIdx int, ft *wasm.CompType) error {
	f.stats.call(callKindHost)
	p := len(ft.Params)
	types, argSlot := f.flushSuffix(p)
	// The logger does not leave native execution; only its fixed X9-X11 scratch
	// bank needs preservation. Locals and value-pinned globals share that extended
	// pin bank, so home both before reusing it without evicting unrelated GP/FP pins.
	hostLogScratch := maskOf(X9, X10, X11)
	f.spillLocalsForClobbers(hostLogScratch, 0)
	f.storePinnedGlobalsIn(hostLogScratch, false)
	if p > 0 {
		f.ld32(X0, SP, f.spillOff(argSlot)) // first param
	} else {
		f.a.MovImm64(X0, 0) // zero (no flag side effect on arm64)
	}
	// The extended pin bank has already been homed, so X0/X9/X10/X11 are free
	// for the async host-log sequence below.
	f.ld64(X11, linMemReg, -int32(offCustomCtx)) // X11 = host-call log
	f.ld32(X9, X11, 0)                           // count
	f.a.AddShifted(X10, X11, X9, 3, false)       // entry = log + count*8
	f.leaDisp(X10, X10, 8, true)                 // + 8 header
	f.a.MovImm64(X16, uint64(uint32(importIdx)))
	f.st32(X10, 0, X16)
	f.st32(X10, 4, X0)
	f.a.AddImm32(X9, X9, 1) // count++
	f.st32(X11, 0, X9)
	f.dropFlushedSuffix(types, p)
	f.derivePinnedGlobalsIn(hostLogScratch)
	// The legacy non-STACK_REG model restores the selected local pins eagerly.
	// STACK_REG leaves them memory-resident and recovers each on its next read.
	f.reloadLocalsForClobbers(hostLogScratch, 0)
	return nil
}

// moduleUsesSyncHostCalls reports whether the module has any returning host
// import (a function import with results, not bound cross-instance). Such a
// module routes ALL host calls through the synchronous control frame, so the
// async host-call log and the control frame never both occupy offCustomCtx.
func moduleUsesSyncHostCalls(m *wasm.Module, bindings []ImportBinding) bool {
	imported := m.ImportedFuncCount()
	for i := 0; i < imported; i++ {
		if bindings != nil && i < len(bindings) && bindings[i].CrossInstance {
			continue
		}
		if ft, ok := m.FuncSignature(uint32(i)); ok && (len(ft.Results) != 0 || funcTypeUsesV128(ft)) {
			return true
		}
	}
	return false
}

func funcTypeUsesV128(ft *wasm.CompType) bool {
	for _, t := range ft.Params {
		if wasm.EqualValType(t, wasm.V128) {
			return true
		}
	}
	for _, t := range ft.Results {
		if wasm.EqualValType(t, wasm.V128) {
			return true
		}
	}
	return false
}

func funcTypeSlots(ts []wasm.ValType) int {
	n := 0
	for _, t := range ts {
		n += mtOf(t).stackSlots()
	}
	return n
}

func (f *fn) syncHostAddress(base Reg, disp int32, size int) (Reg, int32) {
	if disp >= 0 && disp%int32(size) == 0 && disp/int32(size) <= 0xfff {
		return base, disp
	}
	f.a.MovImm64(X16, uint64(uint32(disp)))
	f.a.Add64(X16, base, X16)
	return X16, 0
}

func (f *fn) syncHostStore64(base Reg, disp int32, src Reg) {
	addr, off := f.syncHostAddress(base, disp, 8)
	f.st64(addr, off, src)
}

func (f *fn) syncHostLoad64(dst, base Reg, disp int32) {
	addr, off := f.syncHostAddress(base, disp, 8)
	f.ld64(dst, addr, off)
}

func (f *fn) syncHostStoreV128(base Reg, disp int32, src Reg) {
	addr, off := f.syncHostAddress(base, disp, 16)
	f.a.VMovdquStoreDisp(addr, off, src)
}

func (f *fn) syncHostLoadV128(dst, base Reg, disp int32) {
	addr, off := f.syncHostAddress(base, disp, 16)
	f.a.VMovdquLoadDisp(dst, addr, off)
}

func (f *fn) gcFramePrefixRoots(roots []*elem, n int) []bool {
	if !f.tracksGCFrameRoots() || n <= 0 {
		return nil
	}
	flags := f.tmpGCRoots2[:0]
	for _, root := range roots[:n] {
		flags = append(flags, root.elemKind() == ekValue && root.st.hasGCRoot())
	}
	f.tmpGCRoots2 = flags
	return flags
}

// callHostSync lowers a call to a RETURNING imported (host) function via the
// synchronous re-entry protocol (see src/core/runtime/hostcall_arm64.go). The p
// params are marshaled into the off-heap control frame (at [linMem-offCustomCtx]);
// `blr [ctrl+hcTrampoline]` runs the shared hostCallStub, which saves the wasm
// register state and unwinds to Go; Go runs the host function, writes the
// results, and resumes here; the rN results are read out of the control frame
// onto the operand stack.
//
// hostCallStub saves and resumeNative restores the callee-saved registers
// (X19..linMemReg, low 64 bits of V8..V15), but the extended local-pin pool also
// uses caller-saved X8..X11 and vector values may occupy the full 128 bits.
// Pinned locals are therefore homed before the transition and restored under the
// old non-STACK_REG model. Value-pinned and module-pinned globals are also synced
// around the call: the host may read or write the instance's globals through
// their cells.
func (f *fn) callHostSync(importIdx int, ft *wasm.CompType) error {
	f.stats.call(callKindHostSync)
	internalGC := uint32(importIdx)&(gcStructDispatchBit|shared.AtomicWaitDispatchBit) != 0
	p, rN := len(ft.Params), len(ft.Results)
	var rootOffsets []uint32
	recordRoots := false
	if uint32(importIdx)&(gcStructDispatchBit|shared.AtomicWaitDispatchBit) == 0 {
		rootOffsets, recordRoots = f.prepareGCFrameCallsite(p)
	}
	paramSlots := funcTypeSlots(ft.Params)
	resultSlots := funcTypeSlots(ft.Results)
	internalGCHelper := uint32(importIdx)&gcStructDispatchBit != 0
	slotLimit := maxSyncHostSlots
	if internalGCHelper {
		slotLimit = f.syncHostSlots
	}
	wide := internalGCHelper && (paramSlots > maxSyncHostSlots || resultSlots > maxSyncHostSlots)
	if paramSlots > slotLimit || resultSlots > slotLimit {
		return fmt.Errorf("host import %d uses %d param slot(s), %d result slot(s); synchronous host frame supports at most %d slots in each direction", importIdx, paramSlots, resultSlots, slotLimit)
	}

	roots := f.rootsBottomToTop()
	d := len(roots)
	types := f.tmpTypes[:0]
	slotOf := f.tmpStackSlots[:0]
	slotTop := 0
	for _, root := range roots {
		typ := root.st.typ
		if root.elemKind() == ekDeferred && root.st.typ != mtNone {
			typ = root.st.typ
		}
		types = append(types, typ)
		slotOf = append(slotOf, uint32(slotTop))
		slotTop += typ.stackSlots()
	}
	f.tmpTypes = types
	f.tmpStackSlots = slotOf
	belowTypes := f.tmpTypes2[:0]
	if cap(belowTypes) < d-p {
		belowTypes = make([]machineType, 0, d-p)
	}
	belowTypes = append(belowTypes, types[:d-p]...)
	f.tmpTypes2 = belowTypes
	belowGCRoots := f.gcFramePrefixRoots(roots, d-p)

	f.flush() // operands to canonical slot-width slots
	// X9-X11 are part of the extended local-pin bank. Home every dirty pin before
	// globals, control-frame setup, or argument marshalling reuses those registers.
	f.spillLocalsForCall()
	f.storePinnedGlobals(false) // coherence/preservation for value-pinned caller-saved globals
	if !internalGC {
		// Internal GC helpers cannot observe or mutate numeric module-global cells,
		// and their module-pinned registers are preserved by the parked transition.
		f.storeModuleGlobals(X9)
	}

	// Marshal params into the control frame as wrapper-ABI slots. A v128 occupies
	// two adjacent little-endian uint64 slots, exactly like Invoke and cross-
	// instance wrapper calls.
	f.ld64(X11, linMemReg, -int32(offCustomCtx)) // X11 = control frame
	argsOffset, resultsOffset := int32(hcArgs), int32(hcResults)
	if wide {
		f.a.AddImm64(X11, X11, hcWideBase)
		argsOffset = hcWideArgs
		resultsOffset = hcWideArgs + int32(f.syncHostSlots)*8
	}
	argSlot, ctrlSlot := 0, 0
	if p > 0 {
		argSlot = int(slotOf[d-p])
	}
	for i := 0; i < p; i++ {
		mt := mtOf(ft.Params[i])
		if mt.isV128() {
			x := f.allocFReg(0)
			f.a.VMovdquLoadDisp(x, SP, f.spillOff(argSlot))
			f.syncHostStoreV128(X11, argsOffset+int32(ctrlSlot)*8, x)
			f.releaseF(x)
		} else if mt.is64() {
			f.ld64(X9, SP, f.spillOff(argSlot))
			f.syncHostStore64(X11, argsOffset+int32(ctrlSlot)*8, X9)
		} else {
			f.ld32(X9, SP, f.spillOff(argSlot)) // zero-extends into X9
			f.syncHostStore64(X11, argsOffset+int32(ctrlSlot)*8, X9)
		}
		argSlot += mt.stackSlots()
		ctrlSlot += mt.stackSlots()
	}
	if wide {
		f.ld64(X11, linMemReg, -int32(offCustomCtx))
	}
	f.a.MovImm64(X16, uint64(uint32(importIdx)))
	f.st32(X11, hcImportIdx, X16)
	// hcNArgs packs param slots (low 16) and result slots (high 16) so the Go
	// re-entry loop copies back only the real result count. Both fit uint16.
	f.a.MovImm64(X16, uint64(uint32(paramSlots)|uint32(resultSlots)<<16))
	f.st32(X11, hcNArgs, X16)

	// Park at the host call. Like the wrapper path, no post-call trap check: a
	// trap unwinds the whole native tree in one jump (it never returns here).
	f.ld64(X16, X11, hcTrampoline)
	f.a.Blr(X16)
	if recordRoots {
		f.gcFrameRoots.RecordCallsite(uint32(f.a.Len()), 0, rootOffsets)
	}
	f.reloadLocalsForCall()

	if !internalGC {
		f.deriveModuleGlobals() // arbitrary host code may have written global cells
	}
	f.derivePinnedGlobals()
	f.setDepthTypesWithGCRoots(belowTypes, belowGCRoots)

	// Read results out of the control frame onto the operand stack, honoring
	// slot-width result layout for v128 and mixed scalar/vector signatures.
	f.ld64(X11, linMemReg, -int32(offCustomCtx)) // reload ctrl (clobbered by the round trip)
	if wide {
		f.a.AddImm64(X11, X11, hcWideBase)
	}
	res := f.tmpRegs[:0]
	if cap(res) < rN {
		res = make([]Reg, 0, rN)
	}
	res = res[:rN]
	f.tmpRegs = res
	resTypes := f.tmpTypes[:0]
	if cap(resTypes) < rN {
		resTypes = make([]machineType, 0, rN)
	}
	resTypes = resTypes[:rN]
	f.tmpTypes = resTypes
	ctrlSlot = 0
	for j := 0; j < rN; j++ {
		rt := mtOf(ft.Results[j])
		resTypes[j] = rt
		switch {
		case rt.isV128():
			res[j] = f.allocFReg(0)
			f.syncHostLoadV128(res[j], X11, resultsOffset+int32(ctrlSlot)*8)
			f.fpinned = f.fpinned.add(res[j]) // keep across the remaining loads
		case rt.isFloat():
			tmp := f.allocReg(0)
			f.syncHostLoad64(tmp, X11, resultsOffset+int32(ctrlSlot)*8)
			res[j] = f.allocFReg(0)
			f.a.FmovFromGpr(res[j], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[j])
		default:
			res[j] = f.allocReg(0)
			f.syncHostLoad64(res[j], X11, resultsOffset+int32(ctrlSlot)*8)
			f.pinned = f.pinned.add(res[j]) // keep across the remaining loads
		}
		ctrlSlot += rt.stackSlots()
	}
	for j := 0; j < rN; j++ {
		var value *elem
		switch rt := resTypes[j]; {
		case rt.isV128():
			f.fpinned = f.fpinned.remove(res[j])
			value = f.pushVReg(res[j])
		case rt.isFloat():
			f.fpinned = f.fpinned.remove(res[j])
			value = f.pushFReg(res[j], rt)
		default:
			f.pinned = f.pinned.remove(res[j])
			value = f.pushReg(res[j], rt)
		}
		value.st.setGCRoot(f.tracksGCFrameRoots() && arm64GCFrameRefType(f.m, ft.Results[j]))
	}
	// Arbitrary host code can synchronously re-enter this instance and grow its
	// memory. Reload after reconstructing the operand stack so the continuation
	// cannot retain the parked activation's pre-call size.
	f.refreshCachedMemoryBoundAfterExternalCall()
	return nil
}

// HostIndirectThunk returns standalone machine code that logs a host call for
// importIdx and returns — for a legacy HostFunc reached through call_indirect
// (placed in a table as a funcref). It is entered with the wrapper ABI (X1 =
// linMem, X0 = args buffer), appends (importIdx, first-arg-i32) to the host-call
// log at [linMem-offCustomCtx] exactly like callHost, and returns void, so the
// normal post-invoke replay runs the host function. Emitted per host funcref into
// a per-instance mapping; the same code is instance-independent (it reads the log
// pointer from X1 at run time).
func HostIndirectThunk(importIdx uint32) []byte {
	// Preserve the ordinary encoding; module functions opt into logical MOVs
	// through their policy.
	a := &a64.Asm{DisableLogicalMoveImmediate: true}
	a.Load32(X9, X0, 0)               // X9 = first arg (i32; a harmless slot read for 0-param funcs)
	a.SubImm64(X10, X1, offCustomCtx) // X10 = &host-call log (X1 = linMem in the wrapper ABI)
	a.Load64(X10, X10, 0)
	a.Load32(X11, X10, 0)                 // count
	a.AddShifted(X12, X10, X11, 3, false) // entry = log + count*8
	a.AddImm64(X12, X12, 8)               // + 8 header
	a.MovImm64(X16, uint64(uint32(importIdx)))
	a.Store32(X16, X12, 0)
	a.Store32(X9, X12, 4)   // arg
	a.AddImm32(X11, X11, 1) // count++
	a.Store32(X11, X10, 0)
	a.Ret()
	return a.B
}

// HostIndirectSyncThunk returns standalone machine code for a sync-mode host
// import reached through call_indirect. It is entered with the wrapper ABI
// (X0=args, X3=results, X1=home linMem). Unlike HostIndirectThunk, it must not
// touch the async host log at offCustomCtx; sync-mode instances store the
// host-call control frame there. The thunk marshals raw uint64 wrapper slots into
// the control frame, parks via hostCallStub, then copies result slots back into
// the wrapper results buffer before returning to the wasm caller.
func HostIndirectSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return hostIndirectSyncThunk(importIdx, paramSlots, resultSlots, true)
}

// HostIndirectOwnedSyncThunk uses the active caller's control frame so an
// explicitly owned host funcref can be invoked from another same-store instance.
func HostIndirectOwnedSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return hostIndirectSyncThunk(importIdx, paramSlots, resultSlots, false)
}

func hostIndirectSyncThunk(importIdx uint32, paramSlots, resultSlots int, useHome bool) []byte {
	// Preserve the ordinary encoding; module functions opt into logical MOVs
	// through their policy.
	a := &a64.Asm{DisableLogicalMoveImmediate: true}
	// The host-call round trip preserves only callee-saved registers recorded by
	// hostCallStub. Save the caller's linMemReg (active linMem), the wrapper result
	// pointer, and this thunk's incoming LR across the park/resume; set linMemReg to the
	// funcref's home linMem so the shared hostCallStub reads the correct basedata
	// control cells.
	a.StpPre(linMemReg, X3, SP, -32) // [SP]=linMemReg, [SP+8]=X3, [SP+16]=LR
	a.Store64(LR, SP, 16)
	if useHome {
		a.MovReg64(linMemReg, X1)
	}
	a.SubImm64(X10, linMemReg, offCustomCtx)
	a.Load64(X10, X10, 0) // X10 = sync host-call control frame
	wide := paramSlots > maxSyncHostSlots || resultSlots > maxSyncHostSlots
	argBase := X10
	argOffset := uint32(hcArgs)
	if wide {
		argBase = X11
		argOffset = 0
		a.AddImm64(X11, X10, uint32(hcWideBase+hcWideArgs))
	}
	for i := 0; i < paramSlots; i++ {
		a.Load64(X9, X0, uint32(i*8))
		a.Store64(X9, argBase, argOffset+uint32(i*8))
	}
	a.MovImm64(X16, uint64(uint32(importIdx)))
	a.Store32(X16, X10, hcImportIdx)
	a.MovImm64(X16, uint64(uint32(paramSlots)|uint32(resultSlots)<<16)) // low16 params, high16 results
	a.Store32(X16, X10, hcNArgs)
	a.Load64(X16, X10, hcTrampoline)
	a.Blr(X16)

	// resumeNative returns here with linMemReg restored to the home linMem saved by
	// hostCallStub. Reload the control frame (caller-saved registers were
	// clobbered), restore the result pointer from the saved slot, copy result
	// slots, then restore the caller's original linMemReg (and balance SP) and return.
	a.SubImm64(X10, linMemReg, offCustomCtx)
	a.Load64(X10, X10, 0)
	a.Load64(X3, SP, 8) // reload the wrapper results pointer from the saved slot
	resultBase := X10
	resultOffset := uint32(hcResults)
	if wide {
		resultBase = X11
		resultOffset = 0
		a.Load32(X11, X10, uint32(hcWideBase+4))
		a.AddShifted(X11, X10, X11, 3, false)
		a.AddImm64(X11, X11, uint32(hcWideBase+hcWideArgs))
	}
	for i := 0; i < resultSlots; i++ {
		a.Load64(X9, resultBase, resultOffset+uint32(i*8))
		a.Store64(X9, X3, uint32(i*8))
	}
	a.Load64(LR, SP, 16)
	a.LdpPost(linMemReg, X3, SP, 32) // restore caller linMemReg (X3 reload is harmless), SP += 32
	a.Ret()
	return a.B
}

// Basedata scratch offsets (negative from the linMem base), matching the runtime
// and backend/railshot/arm64: a scratch cell to carry the indirect code pointer
// across the flush, and the indirect-call table descriptor pointer.
const (
	offCustomCtx    = 40 // host-call log pointer / sync host-call control frame
	offSpillRegion  = 48 // 8B scratch
	offStackFence   = 72 // low stack bound for the fence check
	offTablePtr     = 80 // table descriptor pointer
	offMemoryDirPtr = abi.MemoryDirPtrOffset
	// offTrapHandlerPtr (32), offTrapStackReentry (24), and offTrapCellPtr
	// (== abi.TrapCellPtrOffset) are defined in memory.go.
)

// Control-frame field offsets for the synchronous host-call protocol. A
// returning host import needs no async log, so it reuses the customCtx slot
// (offCustomCtx) for its control frame. These MUST match
// src/core/runtime/hostcall_arm64.go (hcSavedSP..hcResults, maxHostArity=64).
const (
	hcTrampoline           = 176 // u64: hostCallStub address (published per-instance by CallWithHost)
	hcImportIdx            = 184 // u32: native -> Go
	hcNArgs                = 188 // u32: low 16 bits = param slots, high 16 bits = result slots
	hcArgs                 = 192 // [64]u64: native -> Go
	hcResults              = 704 // [64]u64: Go -> native (== hcArgs + 64*8)
	hcWideBase             = hcResults + maxSyncHostSlots*8
	hcWideArgs       int32 = 8
	maxSyncHostSlots       = 64 // must match runtime.MaxHostArity / maxHostArity
)

// copyInstanceContext runs only at flushed cross-instance transfer boundaries;
// X8 and X9 are therefore free call-clobbered scratch registers. The GC native
// view lives 280 bytes below linMem, outside STUR's signed 9-bit range, so form
// that destination address explicitly without clobbering the live dispatch
// pointer in X16 or target entry in X17.
func (f *fn) copyInstanceContext(dst, src Reg) {
	// The source descriptor is contiguous. Pair its loads, and pair the two
	// destination runs whose basedata cells are contiguous as well. The register
	// order in the first STP is reversed because basedata grows toward lower
	// addresses from dst.
	f.ld64(X9, src, 0)
	f.st64(dst, -offCustomCtx, X9)
	f.a.LdpOffset(X8, X9, src, 8)
	f.a.StpOffset(X9, X8, dst, -int32(offFuncRefDescPtr))
	f.a.LdpOffset(X8, X9, src, 24)
	f.a.StpOffset(X8, X9, dst, -int32(offPassiveElemPtr))
	f.a.LdpOffset(X8, X9, src, 40)
	f.st64(dst, -int32(offPassiveDataPtr), X8)
	f.st64(dst, -int32(offTableDirPtr), X9)
	f.a.LdpOffset(X8, X9, src, 56)
	f.st64(dst, -int32(offMemoryDirPtr), X8)
	f.st64(dst, -int32(offImportDispatchPtr), X9)
	f.ld64(X9, src, runtime.InstanceContextGCNativeViewOffset)
	f.a.SubImm64(X8, dst, uint32(abi.GCNativeViewPtrOffset))
	f.a.Store64(X9, X8, 0)
}

// emitCrossInstanceCall lowers a call to an imported function that is bound to
// another instance's function (cross-instance linking). Unlike a host import
// (which logs and returns void), this is a real native call into the callee
// instance, staying on the same foreign stack. The callee's offset-0 entry
// re-establishes ITS module context from X1=linMem (linMemReg, memSize X27, module
// globals X23-X25), so the caller's whole-module-invariant registers are
// preserved across the call by STP/LDP; the three per-execution control words
// (trap re-entry, stack fence, trap cell) are copied caller→callee so a trap in
// the callee unwinds to this execution's enterNative. Production code loads the
// callee entry, home memory, and target/caller contexts from the import dispatch
// cell; the immediate form remains only for focused backend callers.
func (f *fn) emitCrossInstanceCall(b ImportBinding, ft *wasm.CompType) error {
	rootOffsets, recordRoots := f.prepareGCFrameCallsite(len(ft.Params))
	kind := callKindCrossInstance
	if b.Dynamic {
		kind = callKindImportDispatch
	}
	f.stats.call(kind)
	p := len(ft.Params)
	roots := f.rootsBottomToTop()
	d := len(roots)
	types := f.tmpTypes[:0]
	slotOf := f.tmpStackSlots[:0]
	slotTop := 0
	for _, root := range roots {
		typ := root.st.typ
		if root.elemKind() == ekDeferred && root.st.typ != mtNone {
			typ = root.st.typ
		}
		types = append(types, typ)
		slotOf = append(slotOf, uint32(slotTop))
		slotTop += typ.stackSlots()
	}
	f.tmpTypes = types
	f.tmpStackSlots = slotOf
	belowTypes := f.tmpTypes2[:0]
	if cap(belowTypes) < d-p {
		belowTypes = make([]machineType, 0, d-p)
	}
	belowTypes = append(belowTypes, types[:d-p]...)
	f.tmpTypes2 = belowTypes
	belowGCRoots := f.gcFramePrefixRoots(roots, d-p)
	resultSlot := slotTop
	resultSlots := funcTypeSlots(ft.Results)

	f.flush()
	f.storePinnedGlobals(false) // value-pinned globals → cells (reloaded after; callee can't touch B's cells)

	if need := resultSlot + resultSlots; need > f.maxSpill {
		f.maxSpill = need
	}
	argOff := f.spillOff(resultSlot) // p==0: unused, but a valid in-frame address
	if p > 0 {
		argOff = f.spillOff(int(slotOf[d-p]))
	}
	f.spillLocalsForCall()
	f.storeModuleGlobals(X9) // cross-instance boundary: shared globals must be cell-coherent

	// Args/results buffers as absolute pointers (survive the STP pushes below —
	// they hold absolute addresses, unaffected by the SP adjustment).
	f.a.LeaSP(X0, argOff)                 // args = &first arg slot
	f.a.LeaSP(X3, f.spillOff(resultSlot)) // results = &slot-width top

	// Preserve the caller's module-invariant registers (linMemReg=linMem, X27=memSize,
	// X23-X25=module globals), and X22=the active EH record (3 STP pairs =
	// 48 bytes, so SP stays 16-aligned). An exception-enabled callee inherits X22;
	// a non-EH callee may use it as a local pin, so restore it on normal return.
	f.a.StpPre(linMemReg, X24, SP, -16)
	f.a.StpPre(X25, X23, SP, -16)
	f.a.StpPre(X27, ehReg, SP, -16)

	if b.Dynamic {
		if b.ImportIndex > uint32((1<<31-1-runtime.ImportDispatchCallerContextOffset)/runtime.ImportDispatchEntryBytes) {
			return fmt.Errorf("import dispatch index %d overflows displacement", b.ImportIndex)
		}
		disp := int32(b.ImportIndex * runtime.ImportDispatchEntryBytes)
		f.ld64(X16, linMemReg, -int32(offImportDispatchPtr))
		f.ld64(X1, X16, disp+runtime.ImportDispatchHomeLinMemOffset)     // wrapper-ABI arg 1
		f.ld64(X10, X16, disp+runtime.ImportDispatchTargetContextOffset) // target context
		f.ld64(X17, X16, disp+runtime.ImportDispatchCodePtrOffset)       // wrapper entry
		f.copyInstanceContext(X1, X10)
		f.ld64(X16, X16, disp+runtime.ImportDispatchCallerContextOffset) // caller context
		f.a.StpPre(X16, X17, SP, -16)
	} else {
		f.a.MovImm64(X1, b.CalleeLinMem) // callee linMem base (wrapper-ABI arg 1)
	}
	// Copy the per-execution control words caller(linMemReg)→callee(X1).
	f.ld64(X9, linMemReg, -int32(offTrapHandlerPtr))
	f.st64(X1, -int32(offTrapHandlerPtr), X9)
	f.ld64(X9, linMemReg, -int32(offTrapStackReentry))
	f.st64(X1, -int32(offTrapStackReentry), X9)
	f.ld64(X9, linMemReg, -int32(offStackFence))
	f.st64(X1, -int32(offStackFence), X9)
	f.ld64(X9, linMemReg, -int32(offTrapCellPtr))
	f.st64(X1, -int32(offTrapCellPtr), X9)

	if b.Dynamic {
		f.a.Blr(X17)
	} else {
		f.a.MovImm64(X9, b.CalleeEntry)
		f.a.Blr(X9)
	}
	if recordRoots {
		stackAdjust := uint32(48)
		if b.Dynamic {
			stackAdjust = 64
		}
		f.gcFrameRoots.RecordCallsite(uint32(f.a.Len()), stackAdjust, rootOffsets)
	}

	if b.Dynamic {
		f.a.LdpPost(X16, X17, SP, 16) // caller context + saved target
	}
	f.a.LdpPost(X27, ehReg, SP, 16)
	f.a.LdpPost(X25, X23, SP, 16)
	f.a.LdpPost(linMemReg, X24, SP, 16)
	if b.Dynamic {
		f.copyInstanceContext(linMemReg, X16)
	}
	// A dynamic target may be arbitrary host code that synchronously re-enters
	// this caller and grows its memory. Reload from the restored caller context
	// before its continuation resumes.
	f.refreshCachedMemoryBoundAfterExternalCall()

	f.reloadLocalsForCall() // non-STACK_REG model only
	f.deriveModuleGlobals() // cross-instance callee may have written shared global cells
	f.derivePinnedGlobals() // reload value-pinned globals from B's cells

	// Pop the arguments and publish the wrapper results without imposing a
	// physical-register arity limit.
	f.finishWrapperResultsWithRoots(belowTypes, belowGCRoots, resultSlot, ft.Results)
	return nil
}

// callInternal lowers a direct call to another local function. Integer-only
// callees use the fast register ABI (args/result in registers); others go
// through the wrapper (sp-buffer) ABI.
func (f *fn) callInternal(localIdx int, ft *wasm.CompType, resHint int) error {
	rootOffsets, recordRoots := f.prepareGCFrameCallsite(len(ft.Params))
	relocBase := len(f.relocs)
	finishRoots := func() {
		if !recordRoots {
			return
		}
		if len(f.relocs) != relocBase+1 {
			f.gcFrameRoots.Exact = false
			return
		}
		f.gcFrameRoots.RecordCallsite(uint32(f.relocs[relocBase].at+4), 0, rootOffsets)
	}
	if f.opt(optRegABI) && stagedTailRegisterABI(ft, f.stagedTailDescriptors) {
		if sigIsIntOnly(ft) {
			f.stats.call(callKindRegisterABI)
			preservesPins := f.directCalleePreservesPins(localIdx)
			if recordRoots {
				// Exact caller maps name frame slots. Force the ordinary spill-managed
				// call path even for a leaf that could otherwise preserve caller pins.
				preservesPins = false
			}
			f.emitRegisterCall(localIdx, ft, resHint, preservesPins)
		} else {
			f.stats.call(callKindMixed)
			f.emitMixedRegisterCall(localIdx, ft)
		}
		finishRoots()
		return nil
	}
	f.stats.call(callKindWrapper)
	f.emitWrapperCall(ft, func() {
		site := f.a.Bl()
		f.relocs = append(f.relocs, f.newCallReloc(site, localIdx, false))
	})
	finishRoots()
	return nil
}

// consumeGCFrameCallsite advances the source-level call-liveness stream for a
// local call removed by inlining. The splice is a leaf and cannot suspend, so it
// needs no native return-PC map, but later real calls must still use their own
// source call masks.
func (f *fn) consumeGCFrameCallsite() {
	plan := f.gcFrameRoots
	if plan == nil || !plan.Candidate {
		return
	}
	if f.gcCallsiteIndex >= plan.CallMaskCount() {
		plan.Exact = false
		return
	}
	f.gcCallsiteIndex++
}

func (f *fn) prepareGCFrameCallsite(paramCount int) ([]uint32, bool) {
	plan := f.gcFrameRoots
	if plan == nil || !plan.Candidate {
		return nil, false
	}
	siteIndex := f.gcCallsiteIndex
	f.gcCallsiteIndex++
	if siteIndex >= plan.CallMaskCount() {
		plan.Exact = false
		return nil, false
	}
	roots := f.rootsBottomToTop()
	if paramCount < 0 || paramCount > len(roots) {
		plan.Exact = false
		return nil, false
	}
	f.materializeGCFrameLocalsAt(siteIndex, true)
	offsets := f.tmpGCOffsets[:0]
	defer func() {
		if uint64(cap(offsets))*4 <= shared.MaxRetainedGCCallsiteOffsetBytes {
			f.tmpGCOffsets = offsets[:0]
		} else {
			f.tmpGCOffsets = nil
		}
	}()
	if !plan.VisitLiveLocals(siteIndex, true, func(root int) {
		offsets = append(offsets, plan.Locals[root].Offset)
	}) {
		plan.Exact = false
		return nil, false
	}
	hidden := len(roots) - paramCount
	slot := 0
	for i, root := range roots {
		if i < hidden && root.elemKind() == ekValue && root.st.hasGCRoot() {
			off := f.spillOff(slot)
			if off < 0 {
				plan.Exact = false
				return nil, false
			}
			offsets = append(offsets, uint32(off))
		}
		slot += rootMachineType(root).stackSlots()
	}
	offsets = append(offsets, plan.FixedOffsets()...)
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	return offsets, true
}

// emitRegisterCall lowers an internal call to a register-ABI function: the top p
// operands become the argument registers (via a parallel move), the callee is
// entered at its internal entry, and the single result is taken from X0.
// resHint >= 0 fuses a following `local.set resHint`: X0 moves straight into
// the pinned local's register instead of an allocated result register.
func (f *fn) emitRegisterCall(localIdx int, ft *wasm.CompType, resHint int, preservesPins bool) {
	f.emitRegisterCallVia(ft, resHint, preservesPins, localIdx, regNone)
}

// emitRegisterCallVia emits either a direct internal BL (localIdx >= 0) or an
// indirect BLR. Explicit operands avoid allocating a closure at every wasm call.
func (f *fn) emitRegisterCallVia(ft *wasm.CompType, resHint int, preservesPins bool, localIdx int, indirect Reg) uint32 {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()
	allRoots := f.rootsBottomToTop()
	allTypes := f.logicalTypes(allRoots)
	belowTypes := append(f.tmpTypes2[:0], allTypes[:d-p]...)
	f.tmpTypes2 = belowTypes
	belowGCRoots := f.gcFramePrefixRoots(allRoots, d-p)
	if !preservesPins {
		f.storePinnedGlobals(false) // spill value-pinned globals to their cells before the call (scratch is free here)
	}

	// Identify the p argument roots (top of stack), deepest first.
	argRoots := f.tmpRoots[:0]
	if cap(argRoots) < p {
		argRoots = make([]*elem, 0, p)
	}
	argRoots = argRoots[:p]
	f.tmpRoots = argRoots
	cur := f.s.back()
	for i := p - 1; i >= 0; i-- {
		argRoots[i] = cur
		if i > 0 {
			cur = f.s.prev(f.s.baseOfValentBlock(cur))
		}
	}

	// Register-resident args (deferred/reg/pinned-local) are materialized into
	// owned, pinned registers now (protected from the flush below); const/memory
	// args are loaded straight into their target register afterward.
	moves := f.tmpMoves[:0]
	deferred := f.tmpDeferred[:0]
	for i := 0; i < p; i++ {
		root := argRoots[i]
		if root.isDeferred() || (root.elemKind() == ekValue && (root.st.kind == stReg || root.st.kind == stLocalReg || root.st.kind == stGlobReg || root.st.kind == stMemRef)) {
			reg := f.materialize(root) // stMemRef → emits the deferred load into its addr reg
			f.pinned = f.pinned.add(reg)
			moves = append(moves, regMove{dst: intArgRegs[i], src: reg})
		} else {
			deferred = append(deferred, deferredArg{target: intArgRegs[i], root: root})
		}
	}
	if p > 0 {
		f.stats.addCallFlush()
		f.flushBelow(argRoots[0]) // operands below the args → canonical slots
	} else {
		f.stats.addCallFlush()
		f.flush()
	}
	// Store dirty pinned locals BEFORE the argument staging: a pinned local may
	// live in an argument register (X5-X7 for 6+-arg calls) or be clobbered by the
	// staging below. Their values were already copied out above where an argument
	// reads them. Lazy reload on the next read — WARP's STACK_REG model.
	if !preservesPins {
		f.spillLocalsForCall()
	}

	// Unpin the owned source registers, then resolve the parallel move into targets.
	for _, m := range moves {
		f.pinned = f.pinned.remove(m.src)
	}
	// AArch64 has no XCHG: a register swap goes through the backend scratch X16.
	swapChains := resolveRegMovesWindow(moves,
		func(dst, src Reg) { f.a.MovReg64(dst, src) },
		func(x, y Reg) {
			f.a.MovReg64(X16, x)
			f.a.MovReg64(x, y)
			f.a.MovReg64(y, X16)
		},
		func(a, b, c Reg) {
			f.a.MovReg64(X16, a)
			f.a.MovReg64(a, b)
			f.a.MovReg64(b, c)
			f.a.MovReg64(c, X16)
		})
	f.stats.peepN("machine-swap-chain", swapChains)
	f.tmpMoves = moves[:0]
	for _, da := range deferred {
		switch da.root.st.kind {
		case stConst:
			f.loadConst(da.target, da.root.st)
		case stSlot:
			f.ld64(da.target, SP, f.spillOff(da.root.st.slotIndex()))
		case stLocalRef:
			f.ld64(da.target, SP, f.localOff(da.root.st.index()))
		}
	}
	f.tmpDeferred = deferred[:0]

	// Consume the args while preserving v128 slot widths and collector identity.
	f.setDepthTypesWithGCRoots(belowTypes, belowGCRoots)

	// No environment passing: linMemReg (linMem) is a whole-module invariant and the
	// trap cell pointer lives in basedata — the callee inherits both (WARP model).
	var returnOffset uint32
	if localIdx >= 0 {
		site := f.a.Bl()
		f.relocs = append(f.relocs, f.newCallReloc(site, localIdx, true))
		returnOffset = uint32(site + 4)
	} else {
		f.a.Blr(indirect)
		returnOffset = uint32(f.a.Len())
	}

	// A pin-preserving callee leaves the caller state untouched, so its result can
	// remain allocator-owned in X0. Calls that reload state still copy it out first
	// because those reload sequences may use X0 as scratch.
	resReg := regNone
	if rN == 1 && resHint < 0 {
		if preservesPins {
			resReg = X0
			f.stats.peep("call-result-x0")
		} else {
			resReg = f.allocReg(maskOf(X0))
			f.a.MovReg64(resReg, X0)
		}
		f.pinned = f.pinned.add(resReg)
	}
	var pairRes [2]Reg
	if rN == 2 {
		if preservesPins {
			pairRes = [2]Reg{X0, X1}
		} else {
			pairRes[0] = f.allocReg(maskOf(X0, X1))
			f.pinned = f.pinned.add(pairRes[0])
			f.a.MovReg64(pairRes[0], X0)
			pairRes[1] = f.allocReg(maskOf(X0, X1))
			f.a.MovReg64(pairRes[1], X1)
		}
		f.pinned = f.pinned.add(pairRes[0]).add(pairRes[1])
	}
	if !preservesPins {
		f.reloadLocalsForCall() // non-STACK_REG model only
		f.derivePinnedGlobals() // reload value-pinned globals: the callee may have changed the shared cell
	}
	// No post-call trap check: a callee trap jumps straight back to enterNative
	// via emitTrap's handler-jump, so control never returns here with *trap set.

	if rN == 1 && resHint >= 0 {
		// Fused `local.set`: the result lands directly in the pinned local's
		// register — after any eager post-call reload, which would otherwise
		// overwrite it with the stale slot value.
		pr, _, _ := f.pinReg(resHint)
		f.a.MovReg64(pr, X0)
		f.markLocalDirty(resHint)
	}

	if rN == 1 && resHint < 0 {
		f.pinned = f.pinned.remove(resReg)
		f.pushReg(resReg, mtOf(ft.Results[0]))
	}
	if rN == 2 {
		for i, reg := range pairRes {
			f.pinned = f.pinned.remove(reg)
			f.pushReg(reg, mtOf(ft.Results[i]))
		}
	}
	return returnOffset
}

// directCalleePreservesPins returns the module-precomputed leaf classification
// for one direct target. This is compile-time only; execution stays a plain BL.
func (f *fn) directCalleePreservesPins(localIdx int) bool {
	if localIdx < 0 || localIdx >= len(f.calleeHints) {
		return false
	}
	return f.calleeHints[localIdx].flags.has(hintPreservesCallerPins)
}

// emitMixedRegisterCall uses the register ABI for signatures containing floats.
// GP and FP arguments are staged independently as parallel moves, so values that
// are already resident in registers do not round-trip through canonical slots.
func (f *fn) emitMixedRegisterCall(localIdx int, ft *wasm.CompType) {
	f.emitMixedRegisterCallVia(localIdx, regNone, ft)
}

func (f *fn) emitMixedRegisterCallVia(localIdx int, indirect Reg, ft *wasm.CompType) uint32 {
	if indirect != regNone {
		// GP argument staging owns X0-X7. Preserve a descriptor target selected in
		// that bank before parallel moves and deferred loads overwrite it.
		f.a.MovReg64(X17, indirect)
		indirect = X17
	}
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()
	allRoots := f.rootsBottomToTop()
	allTypes := f.logicalTypes(allRoots)
	belowTypes := append(f.tmpTypes2[:0], allTypes[:d-p]...)
	f.tmpTypes2 = belowTypes
	belowGCRoots := f.gcFramePrefixRoots(allRoots, d-p)

	f.storePinnedGlobals(false) // spill value-pinned globals to their cells before the call
	argRoots := f.tmpRoots[:0]
	if cap(argRoots) < p {
		argRoots = make([]*elem, p)
	} else {
		argRoots = argRoots[:p]
	}
	f.tmpRoots = argRoots
	cur := f.s.back()
	for i := p - 1; i >= 0; i-- {
		argRoots[i] = cur
		if i > 0 {
			cur = f.s.prev(f.s.baseOfValentBlock(cur))
		}
	}
	type deferredMixedArg struct {
		target Reg
		root   *elem
		float  bool
	}
	var gpBuf, fpBuf [8]regMove
	var deferredBuf [16]deferredMixedArg
	gpMoves, fpMoves := gpBuf[:0], fpBuf[:0]
	deferred := deferredBuf[:0]
	gp, fp := 0, 0
	for i, t := range ft.Params {
		mt := mtOf(t)
		root := argRoots[i]
		if mt.isFloat() {
			target := fpArgRegs[fp]
			if root.isDeferred() || (root.elemKind() == ekValue && (root.st.kind == stReg || root.st.kind == stLocalReg || root.st.kind == stGlobReg || root.st.kind == stMemRef)) {
				reg := f.materializeF(root)
				f.fpinned = f.fpinned.add(reg)
				fpMoves = append(fpMoves, regMove{dst: target, src: reg})
				f.stats.peep("mixed-call-reg-arg")
			} else {
				deferred = append(deferred, deferredMixedArg{target: target, root: root, float: true})
			}
			fp++
		} else {
			target := intArgRegs[gp]
			if root.isDeferred() || (root.elemKind() == ekValue && (root.st.kind == stReg || root.st.kind == stLocalReg || root.st.kind == stGlobReg || root.st.kind == stMemRef)) {
				reg := f.materialize(root)
				f.pinned = f.pinned.add(reg)
				gpMoves = append(gpMoves, regMove{dst: target, src: reg})
				f.stats.peep("mixed-call-reg-arg")
			} else {
				deferred = append(deferred, deferredMixedArg{target: target, root: root})
			}
			gp++
		}
	}
	if p > 0 {
		f.stats.addCallFlush()
		f.flushBelow(argRoots[0])
	} else {
		f.stats.addCallFlush()
		f.flush()
	}
	// Dirty locals are saved after argument values have been copied into owned
	// registers; the mixed callee may clobber every caller pin.
	f.spillLocalsForCall()
	for _, m := range gpMoves {
		f.pinned = f.pinned.remove(m.src)
	}
	gpSwapChains := resolveRegMovesWindow(gpMoves,
		func(dst, src Reg) { f.a.MovReg64(dst, src) },
		func(x, y Reg) {
			f.a.MovReg64(X16, x)
			f.a.MovReg64(x, y)
			f.a.MovReg64(y, X16)
		},
		func(a, b, c Reg) {
			f.a.MovReg64(X16, a)
			f.a.MovReg64(a, b)
			f.a.MovReg64(b, c)
			f.a.MovReg64(c, X16)
		})
	f.stats.peepN("machine-swap-chain", gpSwapChains)
	for _, m := range fpMoves {
		f.fpinned = f.fpinned.remove(m.src)
	}
	fpSwapSlot := -1
	fpSwapChains := resolveRegMovesWindow(fpMoves,
		func(dst, src Reg) { f.a.FmovReg(dst, src, true) },
		func(x, y Reg) {
			if fpSwapSlot < 0 {
				fpSwapSlot = f.allocSpillSlot()
			}
			off := f.spillOff(fpSwapSlot)
			f.fst(SP, off, x, true)
			f.a.FmovReg(x, y, true)
			f.fld(y, SP, off, true)
		},
		func(a, b, c Reg) {
			if fpSwapSlot < 0 {
				fpSwapSlot = f.allocSpillSlot()
			}
			off := f.spillOff(fpSwapSlot)
			f.fst(SP, off, a, true)
			f.a.FmovReg(a, b, true)
			f.a.FmovReg(b, c, true)
			f.fld(c, SP, off, true)
		})
	f.stats.peepN("machine-swap-chain", fpSwapChains)
	for _, da := range deferred {
		if da.float {
			switch da.root.st.kind {
			case stConst:
				if da.root.st.typ == mtF64 {
					f.a.MovImm64(X16, uint64(da.root.st.cval))
					f.a.FmovFromGpr(da.target, X16, true)
				} else {
					f.a.MovImm32(X16, int32(uint32(da.root.st.cval)))
					f.a.FmovFromGpr(da.target, X16, false)
				}
			case stSlot:
				f.fld(da.target, SP, f.spillOff(da.root.st.slotIndex()), da.root.st.typ == mtF64)
			case stLocalRef:
				f.fld(da.target, SP, f.localOff(da.root.st.index()), da.root.st.typ == mtF64)
			}
			continue
		}
		switch da.root.st.kind {
		case stConst:
			f.loadConst(da.target, da.root.st)
		case stSlot:
			f.ld64(da.target, SP, f.spillOff(da.root.st.slotIndex()))
		case stLocalRef:
			f.ld64(da.target, SP, f.localOff(da.root.st.index()))
		}
	}
	f.setDepthTypesWithGCRoots(belowTypes, belowGCRoots)

	var returnOffset uint32
	if localIdx >= 0 {
		site := f.a.Bl()
		f.relocs = append(f.relocs, f.newCallReloc(site, localIdx, true))
		returnOffset = uint32(site + 4)
	} else {
		f.a.Blr(indirect)
		returnOffset = uint32(f.a.Len())
	}
	f.reloadLocalsForCall() // non-STACK_REG model only
	f.derivePinnedGlobals() // reload value-pinned globals: the callee may have changed the shared cell

	if rN == 1 {
		rt := mtOf(ft.Results[0])
		if rt.isFloat() {
			f.pushFReg(0, rt) // V0
		} else {
			resReg := f.allocReg(maskOf(X0))
			f.a.MovReg64(resReg, X0)
			f.pushReg(resReg, rt)
		}
	}
	if rN == 2 {
		// Two-int register return (X0/X1): a mixed sig has float params but may
		// still return two integers, e.g. (f64,i64,i64)->(i64,i64).
		var pairRes [2]Reg
		pairRes[0] = f.allocReg(maskOf(X0, X1))
		f.pinned = f.pinned.add(pairRes[0])
		f.a.MovReg64(pairRes[0], X0)
		pairRes[1] = f.allocReg(maskOf(X0, X1))
		f.a.MovReg64(pairRes[1], X1)
		f.pinned = f.pinned.add(pairRes[1])
		for i, reg := range pairRes {
			f.pinned = f.pinned.remove(reg)
			f.pushReg(reg, mtOf(ft.Results[i]))
		}
	}
	return returnOffset
}

func (f *fn) descriptorEntryKind(home Reg, avoid regMask) Reg {
	kind := f.allocReg(avoid)
	f.a.LsrImm(kind, home, abi.FuncRefEntryTagShift, false)
	return kind
}

func (f *fn) stripDescriptorHomeTags(home Reg) {
	f.a.AndImm64(home, home, ^uint64(abi.FuncRefHomeTagMask))
}

func (f *fn) validateWrapperDescriptor(kind, home Reg) {
	// Valid wrapper tags are host=0, local=1, and cross=2. A retained local
	// wrapper may appear in another instance; home-aware dispatch decides whether
	// to switch contexts.
	f.cmpImm(kind, uint32(abi.FuncRefCrossInstanceTagValue), true)
	f.trapIf(condA, trapIndirectSig)
}

func (f *fn) checkCallType(entry Reg, offset int32, key uint64, avoid regMask) {
	got := f.allocReg(avoid)
	f.ld64(got, entry, offset)
	want := f.allocReg(avoid.add(got))
	f.a.MovImm64(want, key)
	f.cmpRR(got, want, true)
	f.release(want)
	f.release(got)
	f.trapIf(condNE, trapIndirectSig)
}

func (f *fn) callRef(r *wasm.Reader) error {
	f.stats.call("ref")
	typeIdx, err := r.U32()
	if err != nil {
		return err
	}
	ft, ok := f.m.TypeFunc(typeIdx)
	if !ok {
		return fmt.Errorf("call_ref: bad type %d", typeIdx)
	}
	canon, ok := f.m.StructuralTypeKeyChecked(typeIdx)
	if !ok {
		return fmt.Errorf("call_ref: type %d exceeds bounded native identity", typeIdx)
	}

	ref := f.materialize(f.popValue())
	rootOffsets, recordRoots := f.prepareGCFrameCallsite(len(ft.Params))
	f.pinned = f.pinned.add(ref)
	f.trapIfZero(ref, true, true, trapIndirectOOB)
	code := f.allocReg(0)
	f.ld64(code, ref, runtime.TableEntryCodePtrOffset)
	f.trapIfZero(code, true, true, trapIndirectOOB)
	f.checkCallType(ref, runtime.TableEntrySigKeyOffset, canon, maskOf(ref, code))
	home := f.allocReg(maskOf(ref, code))
	f.ld64(home, ref, runtime.TableEntryHomeLinMemOffset)
	targetContext := f.allocReg(maskOf(ref, code, home))
	f.ld64(targetContext, ref, runtime.FuncRefContextOffset)
	f.trapIfZero(targetContext, true, true, trapIndirectOOB)
	f.pinned = f.pinned.remove(ref)
	f.release(ref)

	if sigFitsRegABI(ft) && sigIsIntOnly(ft) || sigFitsReferenceResultRegABI(ft) {
		roots := f.rootsBottomToTop()
		types := make([]machineType, len(roots))
		var gcRoots []bool
		if f.tracksGCFrameRoots() {
			gcRoots = gcRootFlags(roots)
		}
		for i, root := range roots {
			types[i] = rootMachineType(root)
		}
		f.pinned = f.pinned.add(code).add(home).add(targetContext)
		f.flush()
		savedLocals := append([]localDef(nil), f.locals...)
		kind := f.descriptorEntryKind(home, maskOf(code, home, targetContext))
		f.cmpImm(kind, uint32(abi.FuncRefInternalTagValue), true)
		wrapper := f.a.Bcond(condNE)
		f.stripDescriptorHomeTags(home)
		f.pinned = f.pinned.remove(home)
		var returnOffset uint32
		if sigFitsReferenceResultRegABI(ft) {
			returnOffset = f.emitMixedRegisterCallVia(-1, code, ft)
		} else {
			returnOffset = f.emitRegisterCallVia(ft, -1, false, -1, code)
		}
		if recordRoots {
			f.gcFrameRoots.RecordCallsite(returnOffset, 0, rootOffsets)
		}
		f.pinned = f.pinned.remove(code)
		f.release(code)
		done := f.a.Branch()

		f.a.PatchBranch19(wrapper, f.a.Len())
		f.locals = savedLocals
		f.setDepthTypesWithGCRoots(types, gcRoots)
		f.stripDescriptorHomeTags(home)
		f.validateWrapperDescriptor(kind, home)
		f.release(kind)
		f.st64(linMemReg, -int32(offSpillRegion), code)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		f.emitIndirectCallHomeAware(ft, home, targetContext, rootOffsets, recordRoots)
		f.a.PatchBranch26(done, f.a.Len())
		return nil
	}

	kind := f.descriptorEntryKind(home, maskOf(code, home, targetContext))
	f.stripDescriptorHomeTags(home)
	f.validateWrapperDescriptor(kind, home)
	f.release(kind)
	f.st64(linMemReg, -int32(offSpillRegion), code)
	f.release(code)
	f.emitIndirectCallHomeAware(ft, home, targetContext, rootOffsets, recordRoots)
	return nil
}

// callIndirect lowers call_indirect: bounds-check the table index, verify the
// entry's canonical type key, reject a null entry, then call the entry's code
// pointer via the wrapper ABI. Table layout matches the runtime (32-byte entries;
// +8 code ptr, +16 type key) with the descriptor pointer at [linMem-offTablePtr].
func (f *fn) callIndirect(r *wasm.Reader) error {
	f.stats.call(callKindIndirect)
	typeIdx, err := r.U32()
	if err != nil {
		return err
	}
	tableIdx, err := r.U32()
	if err != nil {
		return err
	}
	ft, ok := f.m.TypeFunc(typeIdx)
	if !ok {
		return fmt.Errorf("call_indirect: bad type %d", typeIdx)
	}
	canon, ok := f.m.StructuralTypeKeyChecked(typeIdx)
	if !ok {
		return fmt.Errorf("call_indirect: type %d exceeds bounded native identity", typeIdx)
	}

	idxReg := f.materialize(f.popValue())
	rootOffsets, recordRoots := f.prepareGCFrameCallsite(len(ft.Params))
	relocBase := len(f.relocs)
	f.canonicalizeTableOperand(idxReg, tableIdx)
	f.pinned = f.pinned.add(idxReg)
	tbl := f.allocReg(0)
	f.loadTableDescriptor(tbl, tableIdx)
	f.pinned = f.pinned.add(tbl)

	ln := f.allocReg(0)
	f.ld32(ln, tbl, 0) // table length
	f.cmpRR(idxReg, ln, f.tableAddr64(tableIdx))
	f.release(ln)
	f.trapIf(condAE, trapIndirectOOB) // idx >= length → cold stub

	// 64-bit pointer arithmetic: entry address = tbl + idx*32 (TableEntryBytes).
	f.a.LslImm(idxReg, idxReg, 5, true) // idx *= 32
	f.a.Add64(idxReg, idxReg, tbl)      // idx += tbl
	f.pinned = f.pinned.remove(tbl)
	f.release(tbl)

	// Entry fields (folding the 8-byte descriptor header): +8 code, +16 type key,
	// +24 home linMem. Check null (uninitialized element) BEFORE the signature so a
	// zero-initialized entry traps as an empty slot, not a type mismatch.
	code := f.allocReg(0)
	f.ld64(code, idxReg, 8)                         // entry code ptr (offset-0 entry)
	f.trapIfZero(code, true, true, trapIndirectOOB) // null entry

	if f.gcTypeSubtypingRefTest {
		f.pinned = f.pinned.add(code)
		identity := f.allocReg(maskOf(idxReg, code))
		f.ld64(identity, idxReg, 8+runtime.TableEntryRefSlotOffset)
		f.emitLocalFunctionSubtypeIdentityCheck(identity, typeIdx, false, false, trapIndirectSig)
		f.release(identity)
		f.pinned = f.pinned.remove(code)
	} else if tableIdx == 0 && f.immutableTableTyped && f.immutableTableType == canon {
		f.stats.peep("immutable-table-type-check-elide")
	} else {
		got := f.allocReg(maskOf(code))
		f.ld64(got, idxReg, 16) // entry structural type key
		want := f.allocReg(maskOf(code).add(got))
		f.a.MovImm64(want, canon)
		f.cmpRR(got, want, true)
		f.release(want)
		f.release(got)
		f.trapIf(condNE, trapIndirectSig)
	}

	// With one private local immutable table and no function imports, every non-null
	// entry is necessarily a same-module internal entry. Avoid loading its home pointer,
	// testing the internal-entry tag, emitting the wrapper/cross-instance path, and
	// reconciling two compiler states. The ordinary OOB/null/type checks above are
	// still required and deliberately remain on this hot path.
	if tableIdx == 0 && f.immutableLocalTable && f.monomorphicTarget >= 0 && sigFitsRegABI(ft) && sigIsIntOnly(ft) {
		f.pinned = f.pinned.remove(idxReg)
		f.release(idxReg)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		f.stats.peep("monomorphic-call-indirect")
		preservesPins := f.directCalleePreservesPins(f.monomorphicTarget)
		if recordRoots {
			preservesPins = false
		}
		f.emitRegisterCall(f.monomorphicTarget, ft, -1, preservesPins)
		if recordRoots {
			if len(f.relocs) != relocBase+1 {
				f.gcFrameRoots.Exact = false
			} else {
				f.gcFrameRoots.RecordCallsite(uint32(f.relocs[relocBase].at+4), 0, rootOffsets)
			}
		}
		return nil
	}
	home := f.allocReg(maskOf(idxReg, code))
	f.ld64(home, idxReg, 24) // entry home linMem base
	canonical := f.allocReg(maskOf(idxReg, code, home))
	f.ld64(canonical, idxReg, 32) // canonical descriptor pointer
	f.trapIfZero(canonical, true, true, trapIndirectOOB)
	targetContext := f.allocReg(maskOf(idxReg, code, home, canonical))
	f.ld64(targetContext, canonical, runtime.FuncRefContextOffset)
	f.trapIfZero(targetContext, true, true, trapIndirectOOB)
	f.release(canonical)
	f.pinned = f.pinned.remove(idxReg)
	f.release(idxReg)
	if sigFitsRegABI(ft) && sigIsIntOnly(ft) || sigFitsReferenceResultRegABI(ft) {
		// Flush once, then emit both guarded paths from the same canonical stack
		// state. The compiler state for locals is restored before producing the
		// wrapper path; at run time only one branch executes.
		roots := f.rootsBottomToTop()
		types := make([]machineType, len(roots))
		gcRoots := gcRootFlags(roots)
		for i, root := range roots {
			types[i] = root.st.typ
			if root.elemKind() == ekDeferred && root.st.typ != mtNone {
				types[i] = root.st.typ
			}
		}
		f.pinned = f.pinned.add(code).add(home).add(targetContext)
		f.flush()
		savedLocals := append([]localDef(nil), f.locals...)
		kind := f.descriptorEntryKind(home, maskOf(code, home, targetContext))
		f.cmpImm(kind, uint32(abi.FuncRefInternalTagValue), true)
		wrapper := f.a.Bcond(condNE)
		f.stripDescriptorHomeTags(home)
		f.pinned = f.pinned.remove(home)
		var returnOffset uint32
		if sigFitsReferenceResultRegABI(ft) {
			returnOffset = f.emitMixedRegisterCallVia(-1, code, ft)
		} else {
			returnOffset = f.emitRegisterCallVia(ft, -1, false, -1, code)
		}
		if recordRoots {
			f.gcFrameRoots.RecordCallsite(returnOffset, 0, rootOffsets)
		}
		done := f.a.Branch()
		f.a.PatchBranch19(wrapper, f.a.Len())
		f.locals = savedLocals
		f.setDepthTypesWithGCRoots(types, gcRoots)
		f.st64(linMemReg, -int32(offSpillRegion), code)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		f.stripDescriptorHomeTags(home)
		f.validateWrapperDescriptor(kind, home)
		f.release(kind)
		f.emitIndirectCallHomeAware(ft, home, targetContext, rootOffsets, recordRoots)
		f.a.PatchBranch26(done, f.a.Len())
		return nil
	}

	// Stash the code ptr in linMem scratch so it survives the call staging.
	kind := f.descriptorEntryKind(home, maskOf(code, home, targetContext))
	f.stripDescriptorHomeTags(home)
	f.validateWrapperDescriptor(kind, home)
	f.release(kind)
	f.st64(linMemReg, -int32(offSpillRegion), code)
	f.release(code)

	f.emitIndirectCallHomeAware(ft, home, targetContext, rootOffsets, recordRoots)
	return nil
}

// emitIndirectCallHomeAware makes the indirect call to the code ptr stashed at
// [linMem-offSpillRegion], running the funcref in its home instance's context.
// homeReg holds the entry's home linMem base and targetContextReg identifies its
// owning instance. Matching caller/target contexts take the plain frameless
// wrapper path, even when memory aliases are possible. Otherwise preserve the caller's
// whole-module-invariant registers (linMemReg, X23-X25, X27), copy the per-execution control
// words caller→callee, and enter the callee's offset-0 entry with X1 = its linMem
// (the same context-swap as emitCrossInstanceCall, selected at run time).
func (f *fn) emitIndirectCallHomeAware(ft *wasm.CompType, homeReg, targetContextReg Reg, rootOffsets []uint32, recordRoots bool) {
	p := len(ft.Params)
	roots := f.rootsBottomToTop()
	d := len(roots)
	types := f.tmpTypes[:0]
	slotOf := f.tmpStackSlots[:0]
	slotTop := 0
	for _, root := range roots {
		typ := root.st.typ
		if root.elemKind() == ekDeferred && root.st.typ != mtNone {
			typ = root.st.typ
		}
		types = append(types, typ)
		slotOf = append(slotOf, uint32(slotTop))
		slotTop += typ.stackSlots()
	}
	f.tmpTypes = types
	f.tmpStackSlots = slotOf
	belowTypes := f.tmpTypes2[:0]
	if cap(belowTypes) < d-p {
		belowTypes = make([]machineType, 0, d-p)
	}
	belowTypes = append(belowTypes, types[:d-p]...)
	f.tmpTypes2 = belowTypes
	belowGCRoots := f.gcFramePrefixRoots(roots, d-p)
	resultSlot := slotTop
	resultSlots := 0
	for _, rt := range ft.Results {
		resultSlots += mtOf(rt).stackSlots()
	}

	// Stash the home linear-memory and target-context pointers above the results.
	// The frame is stable during the frameless call, so both survive arg staging
	// and the cross-instance path's SP changes. Reserve the whole scratch range
	// through spillFloor while flushing: maxSpill sizes the frame, but curSpillSlot
	// deliberately does not treat that high-water mark as live storage. In
	// particular, flushWideStack stages above the current operand extent and would
	// otherwise overwrite these manually written slots.
	homeSlot := resultSlot + resultSlots
	targetContextSlot := homeSlot + 1
	scratchEnd := targetContextSlot + 1
	if scratchEnd > f.maxSpill {
		f.maxSpill = scratchEnd
	}
	f.st64(SP, f.spillOff(homeSlot), homeReg)
	f.st64(SP, f.spillOff(targetContextSlot), targetContextReg)
	f.release(homeReg)
	f.release(targetContextReg)

	oldSpillFloor := f.spillFloor
	if scratchEnd > f.spillFloor {
		f.spillFloor = scratchEnd
	}
	f.flush() // args → canonical slot-width slots
	f.spillFloor = oldSpillFloor
	f.storePinnedGlobals(false)      // value-pinned globals → cells
	f.storeModuleGlobals(X9)         // same-instance callee's offset-0 prologue reloads from cells
	argOff := f.spillOff(resultSlot) // p==0: unused, but a valid in-frame address
	if p > 0 {
		argOff = f.spillOff(int(slotOf[d-p]))
	}
	f.spillLocalsForCall()
	f.a.LeaSP(X0, argOff)                 // args = &first arg slot
	f.a.LeaSP(X3, f.spillOff(resultSlot)) // results = &slot top

	f.ld64(X11, SP, f.spillOff(homeSlot))          // target home linMem
	f.ld64(X12, SP, f.spillOff(targetContextSlot)) // target instance context
	f.ld64(X13, linMemReg, -int32(offFuncRefDescPtr))
	f.ld64(X13, X13, runtime.FuncRefContextOffset) // caller instance context
	f.cmpRR(X12, X13, true)
	jne := f.a.Bcond(condNE)
	// Same instance: establish the complete wrapper ABI and call directly.
	f.a.MovReg64(X1, linMemReg)
	f.ld64(X2, linMemReg, -int32(offTrapCellPtr))
	f.ld64(X16, linMemReg, -int32(offSpillRegion))
	f.a.Blr(X16)
	if recordRoots {
		f.gcFrameRoots.RecordCallsite(uint32(f.a.Len()), 0, rootOffsets)
	}
	jdone := f.a.Branch()
	// Cross-instance: preserve the caller's invariants (+ one alignment pad), copy
	// the control words caller→callee, enter with X1 = callee linMem, then restore.
	f.a.PatchBranch19(jne, f.a.Len()) // the false edge is a B.cond (imm19)
	f.a.StpPre(linMemReg, X24, SP, -16)
	f.a.StpPre(X25, X23, SP, -16)
	f.a.StpPre(X27, ehReg, SP, -16)
	f.a.StpPre(X13, X12, SP, -16)
	f.copyInstanceContext(X11, X12)
	f.ld64(X9, linMemReg, -int32(offTrapHandlerPtr))
	f.st64(X11, -int32(offTrapHandlerPtr), X9)
	f.ld64(X9, linMemReg, -int32(offTrapStackReentry))
	f.st64(X11, -int32(offTrapStackReentry), X9)
	f.ld64(X9, linMemReg, -int32(offStackFence))
	f.st64(X11, -int32(offStackFence), X9)
	f.ld64(X9, linMemReg, -int32(offTrapCellPtr))
	f.st64(X11, -int32(offTrapCellPtr), X9)
	f.a.MovReg64(X1, X11)
	f.ld64(X2, X11, -int32(offTrapCellPtr))
	f.ld64(X16, linMemReg, -int32(offSpillRegion)) // linMemReg unchanged by the pushes
	f.a.Blr(X16)
	if recordRoots {
		// Four 16-byte records preserve caller invariants while the foreign
		// wrapper runs. Frame walking adds this adjustment to recover the
		// caller's stable post-prologue SP.
		f.gcFrameRoots.RecordCallsite(uint32(f.a.Len()), 64, rootOffsets)
	}
	f.a.LdpPost(X13, X12, SP, 16)
	f.a.LdpPost(X27, ehReg, SP, 16)
	f.a.LdpPost(X25, X23, SP, 16)
	f.a.LdpPost(linMemReg, X24, SP, 16)
	f.copyInstanceContext(linMemReg, X13)
	f.deriveModuleGlobals()             // cross-instance callee may have written shared global cells
	f.a.PatchBranch26(jdone, f.a.Len()) // fr.jdone is an unconditional B (imm26)

	f.reloadLocalsForCall()
	f.derivePinnedGlobals()

	// Publish the wrapper results without imposing a physical-register arity limit.
	f.finishWrapperResultsWithRoots(belowTypes, belowGCRoots, resultSlot, ft.Results)
}

// emitWrapperCall sets up the wrapper ABI registers (X0=args, X3=results,
// X1=linMem, X2=trap), runs emitCall, and loads the results back onto the
// operand stack. Frameless: the wrapper argument and result buffers are the
// operand SPILL SLOTS themselves — after the flush, the p arguments already sit
// contiguously and in order at their canonical spill slots (exactly the wrapper
// ABI layout the callee's prologue reads), and the results land in free slots
// just above the current operand slot top. So there is no separate native-stack
// buffer and no transient SubSP/AddSP — SP stays put for the whole call.
func (f *fn) emitWrapperCall(ft *wasm.CompType, emitCall func()) {
	p := len(ft.Params)
	roots := f.rootsBottomToTop()
	d := len(roots)
	types := f.tmpTypes[:0]
	slotOf := f.tmpStackSlots[:0]
	slotTop := 0
	for _, root := range roots {
		typ := root.st.typ
		if root.elemKind() == ekDeferred && root.st.typ != mtNone {
			typ = root.st.typ
		}
		types = append(types, typ)
		slotOf = append(slotOf, uint32(slotTop))
		slotTop += typ.stackSlots()
	}
	f.tmpTypes = types
	f.tmpStackSlots = slotOf
	belowTypes := f.tmpTypes2[:0]
	if cap(belowTypes) < d-p {
		belowTypes = make([]machineType, 0, d-p)
	}
	belowTypes = append(belowTypes, types[:d-p]...)
	f.tmpTypes2 = belowTypes
	belowGCRoots := f.gcFramePrefixRoots(roots, d-p)
	resultSlot := slotTop
	resultSlots := 0
	for _, rt := range ft.Results {
		resultSlots += mtOf(rt).stackSlots()
	}

	f.flush()                   // all operands to canonical slots; args start at slotOf[d-p]
	f.storePinnedGlobals(false) // spill value-pinned globals to their cells before the call
	f.storeModuleGlobals(X9)    // wrapper callee's offset-0 prologue reloads from the cells

	// Reserve result slots above the full slot-width operand area, including v128 args.
	if need := resultSlot + resultSlots; need > f.maxSpill {
		f.maxSpill = need
	}
	argOff := f.spillOff(resultSlot) // p==0: unused, but a valid in-frame address
	if p > 0 {
		argOff = f.spillOff(int(slotOf[d-p]))
	}
	// Store dirty pinned locals BEFORE the call-setup writes below: a pinned
	// local may be clobbered by the setup itself. Lazy reload on the next read —
	// WARP's STACK_REG.
	f.spillLocalsForCall()
	f.a.LeaSP(X0, argOff)                 // args = &first arg slot
	f.a.LeaSP(X3, f.spillOff(resultSlot)) // results = &slot top
	f.a.MovReg64(X1, linMemReg)           // linMem (kept in linMemReg); trap ptr lives in basedata
	emitCall()

	// No post-call trap check: a callee trap unwinds the whole native call tree
	// in one jump (emitTrap's handler-jump back to enterNative), so control never
	// returns here with *trap set.
	f.reloadLocalsForCall() // non-STACK_REG model only
	f.derivePinnedGlobals() // reload value-pinned globals: the callee may have changed the shared cell

	// Publish the wrapper results without imposing a physical-register arity limit.
	f.finishWrapperResultsWithRoots(belowTypes, belowGCRoots, resultSlot, ft.Results)
}

// finishWrapperResults removes consumed arguments and publishes wrapper-ABI
// results on the operand stack. Common narrow signatures retain the established
// register-resident path. Very wide signatures stay in canonical slots so legal
// multi-value calls are not limited by the physical register file.
func (f *fn) finishWrapperResults(belowTypes []machineType, resultSlot int, results []wasm.ValType) {
	f.finishWrapperResultsWithRoots(belowTypes, nil, resultSlot, results)
}

func (f *fn) finishWrapperResultsWithRoots(belowTypes []machineType, belowGCRoots []bool, resultSlot int, results []wasm.ValType) {
	const maxRegisterResults = 12
	if len(results) > maxRegisterResults || !f.wrapperResultsFitRegisters(results) {
		f.adoptWideWrapperResults(belowTypes, belowGCRoots, resultSlot, results)
		return
	}

	f.setDepthTypesWithGCRoots(belowTypes, belowGCRoots)
	resultN := len(results)
	regs := f.tmpRegs[:0]
	if cap(regs) < resultN {
		regs = make([]Reg, 0, resultN)
	}
	regs = regs[:resultN]
	f.tmpRegs = regs
	types := f.tmpTypes[:0]
	if cap(types) < resultN {
		types = make([]machineType, 0, resultN)
	}
	types = types[:resultN]
	f.tmpTypes = types
	resultSlotCursor := resultSlot
	for i, result := range results {
		typ := mtOf(result)
		types[i] = typ
		switch {
		case typ.isV128():
			regs[i] = f.allocFReg(0)
			f.a.VMovdquLoadDisp(regs[i], SP, f.spillOff(resultSlotCursor))
			f.fpinned = f.fpinned.add(regs[i])
		case typ.isFloat():
			tmp := f.allocReg(0)
			f.ld64(tmp, SP, f.spillOff(resultSlotCursor))
			regs[i] = f.allocFReg(0)
			f.a.FmovFromGpr(regs[i], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(regs[i])
		default:
			regs[i] = f.allocReg(0)
			f.ld64(regs[i], SP, f.spillOff(resultSlotCursor))
			f.pinned = f.pinned.add(regs[i])
		}
		resultSlotCursor += typ.stackSlots()
	}
	for i, typ := range types {
		var value *elem
		switch {
		case typ.isV128():
			f.fpinned = f.fpinned.remove(regs[i])
			value = f.pushVReg(regs[i])
		case typ.isFloat():
			f.fpinned = f.fpinned.remove(regs[i])
			value = f.pushFReg(regs[i], typ)
		default:
			f.pinned = f.pinned.remove(regs[i])
			value = f.pushReg(regs[i], typ)
		}
		value.st.setGCRoot(f.tracksGCFrameRoots() && arm64GCFrameRefType(f.m, results[i]))
	}
}

// wrapperResultsFitRegisters mirrors the amd64 pressure gate. Wrapper results
// are pinned until all slot loads finish, while local/global reservations and FP
// constant registers remain unavailable after setDepthTypes. Scalar floats also
// need one temporary GPR for the slot-to-V-register move.
func (f *fn) wrapperResultsFitRegisters(results []wasm.ValType) bool {
	gpNeed, fpNeed := 0, 0
	needsFloatTmp := false
	for _, result := range results {
		typ := mtOf(result)
		switch {
		case typ.isV128():
			fpNeed++
		case typ.isFloat():
			fpNeed++
			needsFloatTmp = true
		default:
			gpNeed++
		}
	}
	if needsFloatTmp {
		gpNeed++
	}
	gpBlock := f.pinnedLocalMask.union(f.reserved)
	gpAvail := 0
	for _, r := range gpAlloc {
		if !gpBlock.has(r) {
			gpAvail++
		}
	}
	fpBlock := f.fpinnedLocalMask.union(f.fconstMask())
	fpAvail := 0
	for _, r := range fpAllocRegs {
		if !fpBlock.has(r) {
			fpAvail++
		}
	}
	return gpNeed <= gpAvail && fpNeed <= fpAvail
}

func (f *fn) adoptWideWrapperResults(belowTypes []machineType, belowGCRoots []bool, resultSlot int, results []wasm.ValType) {
	dstSlot := 0
	for _, typ := range belowTypes {
		dstSlot += typ.stackSlots()
	}
	resultSlots := funcTypeSlots(results)
	f.moveSlots(resultSlot, dstSlot, resultSlots)

	types := f.tmpTypes[:0]
	if need := len(belowTypes) + len(results); cap(types) < need {
		types = make([]machineType, 0, need)
	}
	types = append(types, belowTypes...)
	for _, result := range results {
		types = append(types, mtOf(result))
	}
	f.tmpTypes = types
	if !f.tracksGCFrameRoots() {
		f.setDepthTypes(types)
		return
	}
	gcRoots := f.tmpGCRoots[:0]
	gcRoots = append(gcRoots, belowGCRoots...)
	for _, result := range results {
		gcRoots = append(gcRoots, arm64GCFrameRefType(f.m, result))
	}
	f.tmpGCRoots = gcRoots
	f.setDepthTypesWithGCRoots(types, gcRoots)
}
