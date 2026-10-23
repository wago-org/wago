//go:build arm64

package arm64

import (
	"fmt"
	"os"

	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

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
	at     uint32 // byte offset of the BL instruction within this function's code
	target uint32 // low 31 bits: local-function index; high bit: internal entry
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

const (
	callRelocInternal      = uint32(1 << 31)
	invalidCallRelocField  = ^uint32(0)
	maxCallRelocTargetPlus = uint64(callRelocInternal)
)

// intArgRegs is the integer argument/result register order for the internal
// register-call ABI (our own convention, not the C ABI). X0/X1 carry args/linMem;
// X19-X23 hold pinned locals; linMemReg holds linMem. The single result returns in X0
// (AAPCS64 return register, also arg 0).
var intArgRegs = []Reg{X0, X1, X2, X3, X4, X5, X6, X7}
var fpArgRegs = []Reg{0, 1, 2, 3, 4, 5, 6, 7} // V0..V7; single float result returns in V0.

func sigIsFloatOnly(ft *wasm.CompType) bool {
	for _, typ := range ft.Params {
		if !isFloatValType(typ) {
			return false
		}
	}
	for _, typ := range ft.Results {
		if !isFloatValType(typ) {
			return false
		}
	}
	return true
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

// returnCall lowers local proper-tail calls. Register-ABI targets receive their
// arguments directly in X/V registers; wider signatures use the fixed basedata
// tail bank and jump to the target wrapper. Both paths release the current native
// frame before branching, so recursive tail chains remain stack bounded.
func (f *fn) returnCall(r *wasm.Reader) error {
	// Tail transfer discards the operand prefix; preserve its traps first.
	f.materializePendingTraps()
	idx, err := r.U32()
	if err != nil {
		return err
	}
	ft, ok := f.functionSignature(idx)
	if !ok {
		return fmt.Errorf("return_call: unknown function %d", idx)
	}
	if !tailResultABICompatible(f.ft.Results, ft.Results) {
		return fmt.Errorf("return_call: target %d result shape differs from caller", idx)
	}
	imported := f.importedFunctionCount()
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
			// Every return_call target has an offset-zero adapter, so a wrapper
			// caller enters it with the wrapper ABI and its own results buffer.
			// Unlike a trampoline record, the adapter record is discarded when
			// the target tails back to a wrapper, keeping tail cycles bounded.
			if slots := funcTypeSlots(ft.Params); slots > abi.TailArgsSlots {
				return fmt.Errorf("return_call: target %d requires %d wrapper argument slots, limit %d", idx, slots, abi.TailArgsSlots)
			}
			f.emitTailWrapperJump(ft, func() {
				site := f.a.Branch()
				f.relocs = append(f.relocs, f.newCallReloc(site, target, false))
			})
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
		// A tail transfer discards every record in this frame. Use the shared
		// large-frame-safe restore instead of addressing record zero from SP.
		f.restorePreviousEHHandler(f.ehRecordOff(0))
	}
}

func (f *fn) emitTailFrameRelease() {
	f.discardEHHandlersForTail()
	at := f.a.Len()
	f.tailFrameSites = append(f.tailFrameSites, at)
	f.a.Movz64(X16, 0, 0)
	f.a.Movk64(X16, 0, 1)
	f.a.AddSPReg(X16)
	if f.makesCalls {
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
	// The register-ABI target returns module pins in registers, but this
	// trampoline returns in place of the wrapper-ABI caller's epilogue, whose
	// callers expect coherent cells.
	f.storeModuleGlobals(X16)
	f.ld64(LR, SP, 0)
	f.a.AddSP64(32)
	f.a.Ret()
}

// emitTailWrapperToAdapterJump tail-enters the internal descriptor target in
// X17 from a wrapper-ABI caller. Its register-ABI internal entry would return in
// registers to a caller that expects a results buffer, so the transfer enters
// the target's offset-0 adapter instead, found through the backlink word below
// the internal entry, exactly like a direct wrapper-to-register return_call.
// The target's own later tails discard the adapter's record, so mixed
// register/wrapper tail loops stay stack-bounded.
func (f *fn) emitTailWrapperToAdapterJump(ft *wasm.CompType) {
	f.emitTailWrapperJump(ft, func() {
		f.ld32(X16, X17, -4)
		f.a.Sub64(X17, X17, X16)
		f.a.Br(X17)
	})
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
	f.patchAdr(adapterPC, f.adapterReturnOff)
	if f.policy.CompactNative {
		f.adapterReturnReferenced = true
	}
	f.cmpRR(LR, X16, true)
	nested := f.a.Bcond(condNE)
	f.a.LdpPost(LR, X3, SP, 16)
	emitJump()

	f.patchBranch19(nested, f.a.Len())
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
		f.a.AndImm64(X12, X12, ^runtime.ImportDispatchCallerTagMask)
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
	// Publish the callee context once before staging and the root/nested split.
	// Its tag-directory cell is immediately outside the wrapper argument bank.
	f.copyInstanceContext(X10, X11)
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
	f.patchAdr(adapterPC, f.adapterReturnOff)
	if f.policy.CompactNative {
		f.adapterReturnReferenced = true
	}
	f.cmpRR(LR, X16, true)
	nested := f.a.Bcond(condNE)
	f.a.LdpPost(LR, X3, SP, 16)
	transfer()

	f.patchBranch19(nested, f.a.Len())
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
	// The target null/type checks are later observable traps.
	f.materializePendingTraps()
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
	if refValue.elemKind() == ekValue && refValue.st.kind == stFuncRef && refValue.st.idx < uint32(f.importedFunctionCount()) {
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
	f.checkCallType(ref, runtime.TableEntrySigKeyOffset, canon, typeIdx, maskOf(ref, code))
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
			f.emitTailWrapperToAdapterJump(ft)
		}

		f.patchBranch19(wrapper, f.a.Len())
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

		f.patchBranch19(notHost, f.a.Len())
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
	// Publish the callee context once before staging and the root/nested split.
	// Its tag-directory cell is immediately outside the wrapper argument bank.
	f.copyInstanceContext(X10, X11)
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
		f.cmpRR(X10, linMemReg, true)
		foreign := f.a.Bcond(condNE)
		transfer()

		// A foreign target would return to our caller with its instance state
		// installed, and our caller may be same-instance code that relies on its
		// own. One fixed record [LR, caller linmem, caller context, pad] returns
		// through a trampoline that restores it, like a non-tail cross-instance
		// call; the target still writes our caller's results buffer.
		f.patchBranch19(foreign, f.a.Len())
		f.a.SubSP64(32)
		f.st64(SP, 0, LR)
		f.st64(SP, 8, linMemReg)
		f.st64(SP, 16, X12)
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
		f.a.AddSP64(32)
		f.a.Ret()
		return
	}

	f.emitTailFrameRelease()
	adapterPC := f.a.Adr(X16)
	f.recordPCRelative(adapterPC)
	f.patchAdr(adapterPC, f.adapterReturnOff)
	if f.policy.CompactNative {
		f.adapterReturnReferenced = true
	}
	f.cmpRR(LR, X16, true)
	nested := f.a.Bcond(condNE)
	f.a.LdpPost(LR, X3, SP, 16)
	transfer()

	f.patchBranch19(nested, f.a.Len())
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
	// Earlier operand traps precede table checks as well as callee effects.
	f.materializePendingTraps()
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
		f.a.LslImm64(idx, idx, 5)
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
	f.a.LslImm64(idx, idx, 5)
	f.a.Add64(idx, idx, tbl)
	f.release(tbl)
	code := f.allocReg(maskOf(idx))
	f.ld64(code, idx, 8)
	f.trapIfZero(code, true, true, trapIndirectOOB)
	if !f.immutableTableTyped || f.immutableTableType != canon {
		f.checkCallType(idx, 8+runtime.TableEntrySigKeyOffset, canon, typeIdx, maskOf(idx, code))
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
	if pluginARM64Lowering(custom) == nil {
		return f.emitInstructionRecipe(custom, ft)
	}
	start := f.a.Len()
	err := f.emitPluginARM64(pluginARM64Lowering(custom), custom.InputWidths, custom.ResultWidth, len(ft.Results), custom.CustomInputs, custom.CustomOutput)
	if err == nil {
		f.recordOpaquePlugin(start, f.a.Len())
	}
	return err
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

func (f *fn) syncHostStoreV128(base Reg, disp int32, src Reg) {
	addr, off := f.syncHostAddress(base, disp, 16)
	f.a.VMovdquStoreDisp(addr, off, src)
}

func (f *fn) syncHostLoadV128(dst, base Reg, disp int32) {
	addr, off := f.syncHostAddress(base, disp, 16)
	f.a.VMovdquLoadDisp(dst, addr, off)
}

// HostIndirectThunk returns standalone machine code that logs a deferred host
// event for importIdx and returns. It is entered with the wrapper ABI (X1 =
// linMem, X0 = args buffer), appends (importIdx, first-arg-i32) to the host-call
// log at [linMem-offCustomCtx] exactly like callHost, and returns void, so the
// normal post-invoke replay delivers the event. Emitted per host funcref into
// a per-instance mapping; the same code is instance-independent (it reads the log
// pointer from X1 at run time).
func HostIndirectThunk(importIdx uint32) []byte {
	// Preserve the ordinary encoding; module functions opt into logical MOVs
	// through their policy.
	a := &a64.Asm{DisableLogicalMoveImmediate: true}
	a.Load32(X9, X0, 0)               // X9 = first arg (i32; a harmless slot read for 0-param funcs)
	a.SubImm64(X10, X1, offCustomCtx) // X10 = &host-call log (X1 = linMem in the wrapper ABI)
	a.Load64(X10, X10, 0)
	a.Load32(X11, X10, 0) // count
	a.CmpImm32LSL12(X11, runtime.HostCallLogEntries)
	full := a.Bcond(condAE)
	a.AddShifted(X12, X10, X11, 3, false) // entry = log + count*8
	a.AddImm64(X12, X12, 8)               // + 8 header
	a.MovImm64(X16, uint64(uint32(importIdx)))
	a.Store32(X16, X12, 0)
	a.Store32(X9, X12, 4)   // arg
	a.AddImm32(X11, X11, 1) // count++
	a.Store32(X11, X10, 0)
	a.Ret()
	if !a.PatchBranch19(full, a.Len()) {
		panic("arm64: fixed host thunk branch exceeds instruction range")
	}
	a.SubImm64(X10, X1, offTrapCellPtr)
	a.Load64(X10, X10, 0)
	a.MovImm64(X16, uint64(runtime.TrapHostEventOverflow))
	a.Store32(X16, X10, 0)
	a.SubImm64(X10, X1, offTrapStackReentry)
	a.Load64(X10, X10, 0)
	a.SubImm64(X11, X1, offTrapHandlerPtr)
	a.Load64(LR, X11, 0)
	a.AddImm64(SP, X10, 0)
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

// emitHostThunkTransfers rebases each cursor at the 32 KiB scaled-offset
// boundary. This keeps every LDR/STR encodable without materializing a large
// address for each remaining slot.
func emitHostThunkTransfers(a *a64.Asm, value, loadBase, storeBase Reg, loadOffset, storeOffset uint32, slots int) {
	const offsetWindow = uint32(0x1000 * 8)
	for range slots {
		if loadOffset >= offsetWindow {
			a.AddImm64LSL12(loadBase, loadBase, offsetWindow)
			loadOffset -= offsetWindow
		}
		if storeOffset >= offsetWindow {
			a.AddImm64LSL12(storeBase, storeBase, offsetWindow)
			storeOffset -= offsetWindow
		}
		if !a.Load64(value, loadBase, loadOffset) {
			panic("arm64: host thunk load offset out of range")
		}
		if !a.Store64(value, storeBase, storeOffset) {
			panic("arm64: host thunk store offset out of range")
		}
		loadOffset += 8
		storeOffset += 8
	}
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
	emitHostThunkTransfers(a, X9, X0, argBase, 0, argOffset, paramSlots)
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
	emitHostThunkTransfers(a, X9, resultBase, X3, resultOffset, 0, resultSlots)
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

// When every overlapping source is a complete prefix root at or below its own
// destination, copy backwards directly to canonical homes. Each write then lies
// above all earlier sources; no temporary spill range or second copy is needed.
func (f *fn) canonicalizeFlushSpills(slot int, roots []*elem) {
	limit := slot
	for i := len(roots) - 1; i >= 0; i-- {
		root := roots[i]
		width := rootMachineType(root).stackSlots()
		slot -= width
		if root.elemKind() != ekValue || root.st.kind != stSlot {
			continue
		}
		from := root.st.slotIndex()
		if from >= limit || from == slot {
			continue
		}
		// Copy both halves backwards when a vector overlaps its new home.
		for j := width - 1; j >= 0; j-- {
			f.ld64(X16, SP, f.spillOff(from+j))
			f.st64(SP, f.spillOff(slot+j), X16)
		}
		root.st.slot = uint32(slot)
	}
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

	f.materializePendingTraps()
	ref := f.materialize(f.popValue())
	rootOffsets, recordRoots := f.prepareGCFrameCallsite(len(ft.Params))
	f.pinned = f.pinned.add(ref)
	f.trapIfZero(ref, true, true, trapIndirectOOB)
	code := f.allocReg(0)
	f.ld64(code, ref, runtime.TableEntryCodePtrOffset)
	f.trapIfZero(code, true, true, trapIndirectOOB)
	f.checkCallType(ref, runtime.TableEntrySigKeyOffset, canon, typeIdx, maskOf(ref, code))
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

		f.patchBranch19(wrapper, f.a.Len())
		f.locals = savedLocals
		f.setDepthTypesWithGCRoots(types, gcRoots)
		f.stripDescriptorHomeTags(home)
		f.validateWrapperDescriptor(kind, home)
		f.release(kind)
		f.st64(linMemReg, -int32(offSpillRegion), code)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		f.emitIndirectCallHomeAware(ft, home, targetContext, rootOffsets, recordRoots)
		f.patchBranch26(done, f.a.Len())
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

// finishWrapperResults removes consumed arguments and publishes wrapper-ABI
// results on the operand stack. Common narrow signatures retain the established
// register-resident path. Very wide signatures stay in canonical slots so legal
// multi-value calls are not limited by the physical register file.
func (f *fn) finishWrapperResults(belowTypes []machineType, resultSlot int, results []wasm.ValType) {
	f.finishWrapperResultsWithRoots(belowTypes, nil, resultSlot, results)
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

// nativeI32ImportAliases admits identical scalar imports or one scalar import in a shared-memory
// module, whose body must also pass nativeImportVariantEligible. A
// runtime native-leaf tag still guards entry to the specialized body.
func nativeI32ImportAliases(m *wasm.Module) bool {
	if len(m.Imports) < 2 && !(len(m.Imports) == 1 && len(m.Memories) == 1 && m.Memories[0].Shared) {
		return false
	}
	first := m.Imports[0]
	if first.Type.Kind != wasm.ExternFunc {
		return false
	}
	ft, ok := m.ImportFuncType(0)
	if !ok || len(ft.Params) < 1 || len(ft.Params) > 2 || len(ft.Results) != 1 || ft.Results[0] != wasm.I32 {
		return false
	}
	for _, param := range ft.Params {
		if param != wasm.I32 {
			return false
		}
	}
	for _, imp := range m.Imports[1:] {
		if imp.Module != first.Module || imp.Name != first.Name || imp.Type != first.Type {
			return false
		}
	}
	return true
}

func (f *fn) nativeImportVariantEligible(body []byte) bool {
	if len(f.m.Memories) != 0 {
		mt := f.m.Memories[0]
		if f.guardMode || len(f.m.Memories) != 1 || !mt.Shared || mt.Limits.Addr64 || !mt.Limits.HasMax || mt.Limits.Min != mt.Limits.Max {
			return false
		}
		if bounded, _ := shared.BoundedFixedSharedHostSegments(body, uint32(f.m.ImportedFuncCount()), 0); !bounded {
			return false
		}
	}
	if profileEnabled || len(body) > 384 || len(f.m.Tables) != 0 || len(f.m.Tags) != 0 || f.gcFrameRoots != nil && f.gcFrameRoots.Candidate || len(f.ft.Params) != 1 || f.ft.Params[0] != wasm.I32 || len(f.ft.Results) != 1 || f.ft.Results[0] != wasm.I32 || len(f.importBindings) != f.m.ImportedFuncCount() {
		return false
	}
	for i, binding := range f.importBindings {
		if !binding.Dynamic || binding.ImportIndex != uint32(i) {
			return false
		}
	}
	return nativeImportIntegerBody(f, body)
}

func nativeImportIntegerBody(f *fn, body []byte) bool {
	for _, typ := range f.localType {
		if typ != mtI32 && typ != mtI64 {
			return false
		}
	}
	r := wasm.ReaderFrom(body)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return false
		}
		var imm wasm.InstructionImmediate
		if wasm.ClassifyInstructionImmediateInto(&r, op, &imm) != nil {
			return false
		}
		switch {
		case op == 0 || op == 1 || op >= 2 && op <= 5 || op >= 0x0b && op <= 0x10 || op == 0x1a || op == 0x1b || op >= 0x20 && op <= 0x22:
		case op == 0x28 || op == 0x29 || op >= 0x2c && op <= 0x37 || op >= 0x3a && op <= 0x3e:
			if len(f.m.Memories) != 1 || !f.threadedMemory0 {
				return false
			}
		case op == 0x41 || op == 0x42 || op >= 0x45 && op <= 0x5a || op >= 0x67 && op <= 0x8a || op == 0xa7 || op == 0xac || op == 0xad || op >= 0xc0 && op <= 0xc4:
		default:
			return false
		}
	}
	return true
}

// Keep source trap/load order and reject overlapping noncanonical homes.
func nativeSharedArgPrefixSafe(roots []*elem) bool {
	const params = 1
	if params < 1 || params > 2 || len(roots) < params || len(roots) > len(intArgRegs) {
		return false
	}
	for i, root := range roots[:len(roots)-params] {
		plain := root.st.kind == stLocalReg || root.st.kind == stConst || root.st.kind == stSlot && root.st.slotIndex() == i
		if root.elemKind() != ekValue || !plain || root.st.typ != mtI32 && root.st.typ != mtI64 {
			return false
		}
	}
	for i, root := range roots[len(roots)-params:] {
		if root.elemKind() == ekValue && root.st.kind == stSlot && root.st.slotIndex() != len(roots)-params+i {
			return false
		}
	}
	return true
}
