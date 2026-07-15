//go:build amd64

package amd64

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime"
)

// regABIEnabled turns on the register-based internal-call ABI (default on;
// WAGO_Amd64_NOREGABI=1 forces the wrapper ABI everywhere, for A/B measurement).
var regABIEnabled = os.Getenv("WAGO_Amd64_NOREGABI") != "1"

// noStackFence skips the per-entry stack-overflow fence check (A/B measurement).
var noStackFence = os.Getenv("WAGO_Amd64_NOFENCE") == "1"

// noStackReg disables the WARP STACK_REG lazy local model (reverts to spill-all/
// reload-all around calls, no branch reconcile) — A/B measurement.
var noStackReg = os.Getenv("WAGO_Amd64_NOSTACKREG") == "1"

// Function calls. Internal (wasm→wasm) calls use wago's WasmWrapper ABI: the
// arguments and result slots live in a native-stack buffer at RSP; the callee is
// entered with RDI=args, RSI=linMem, RDX=trap, RCX=results — exactly what the
// prologue expects. Ported from WARP's call lowering but retargeted to wago's
// ABI/runtime (host imports adapt to wago's re-entry model, not WARP's
// synchronous native calls — the no-cgo constraint).

// callReloc records a CallRel32 site whose rel32 must be patched to point at the
// target local function's entry once the module is laid out.
type callReloc struct {
	at       int  // byte offset of the rel32 field within this function's code
	target   int  // target local-function index (into m.Code)
	internal bool // target the callee's register-ABI internal entry (else offset 0)
}

// intArgRegs is the integer argument/result register order for the internal
// register-call ABI (our own convention, not the C ABI). RDI/RSI carry linMem/
// trap; R12-R15 hold pinned locals; RBX holds linMem. The single result returns
// in RAX.
var intArgRegs = []Reg{RAX, RCX, RDX, R8, R9, R10, R11}
var fpArgRegs = []Reg{0, 1, 2, 3, 4, 5, 6, 7} // XMM0..XMM7; single float result returns in XMM0.

func isIntValType(t wasm.ValType) bool {
	return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64)
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
// and float params are assigned to separate GP/XMM banks; one result returns in
// RAX or XMM0, and the deliberately limited two-result form uses RAX/RDX for
// integers (mirrors arm64's X0/X1 pair return).
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

func sameValTypes(a, b []wasm.ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !wasm.EqualValType(a[i], b[i]) {
			return false
		}
	}
	return true
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
	// Auto-inlining (WAGO_INLINE): splice a straight-line leaf callee's body here
	// instead of emitting a call. The frame reserved the callee's locals past
	// f.nLocals in this caller; the splice binds params, zeroes declared locals, and
	// runs the body with localBase set. Straight-line callees touch no control frame,
	// so this is a pure operand-stack/local transform.
	if f.inlineTargets != nil {
		if t := f.inlineTargets[int(idx)]; t != nil {
			if _, ok := f.inlineBase[int(idx)]; ok {
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
	// int result feeds a pinned local moves RAX straight into the local's
	// register — no intermediate result register, no separate set lowering.
	hint := -1
	if regABIEnabled && sigFitsRegABI(ft) && sigIsIntOnly(ft) && len(ft.Results) == 1 {
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

// returnCall lowers the bounded direct-tail-call milestone. A validated local
// target whose caller and callee fit the internal register ABI reuses the current
// activation: arguments are staged in the callee's GP/XMM banks, the current
// frame is released, and control jumps (rather than calls) to the callee's
// internal entry. Imported targets and wrapper-only signatures remain explicit
// backend rejections; the public tail-call feature gate therefore stays disabled.
func (f *fn) returnCall(r *wasm.Reader) error {
	idx, err := r.U32()
	if err != nil {
		return err
	}
	ft, ok := f.m.FuncSignature(idx)
	if !ok {
		return fmt.Errorf("return_call: unknown function %d", idx)
	}
	if !sameValTypes(f.ft.Results, ft.Results) {
		return fmt.Errorf("return_call: target %d result shape differs from caller", idx)
	}
	imported := f.m.ImportedFuncCount()
	if int(idx) < imported {
		return fmt.Errorf("return_call: imported target %d requires unsupported host/cross-instance tail ABI", idx)
	}
	if !sigFitsRegABI(f.ft) || !sigFitsRegABI(ft) {
		return fmt.Errorf("return_call: caller or target %d requires unsupported wrapper tail ABI", idx)
	}
	f.stats.call("tail-direct")
	f.emitTailRegisterJump(ft, func() {
		site := f.a.JmpPlaceholder()
		f.relocs = append(f.relocs, callReloc{at: site, target: int(idx) - imported, internal: true})
	})
	f.unreachable = true
	return nil
}

type tailDeferredArg struct {
	target Reg
	root   *elem
	float  bool
}

// emitTailRegisterJump stages a register-ABI callee's arguments without
// preserving any caller locals or operand values: a tail call has no continuation.
// It then releases the current frame and emits the supplied direct/indirect jump.
func (f *fn) emitTailRegisterJump(ft *wasm.CompType, emitJump func()) {
	p := len(ft.Params)
	f.storePinnedGlobals(false)

	var roots [15]*elem // sigFitsRegABI caps params at 7 GP + 8 FP
	cur := f.s.back()
	for i := p - 1; i >= 0; i-- {
		roots[i] = cur
		if i > 0 {
			cur = baseOfValentBlock(cur).prev
		}
	}

	var gpMoves [7]regMove
	var fpMoves [8]regMove
	var deferred [15]tailDeferredArg
	gpN, fpN, deferredN := 0, 0, 0
	gp, fp := 0, 0
	for i, typ := range ft.Params {
		mt := mtOf(typ)
		root := roots[i]
		resident := root.isDeferred() || (root.kind == ekValue && (root.st.kind == stReg || root.st.kind == stLocalReg || root.st.kind == stGlobReg || root.st.kind == stMemRef))
		if mt.isFloat() {
			target := fpArgRegs[fp]
			if resident {
				src := f.materializeF(root)
				f.fpinned = f.fpinned.add(src)
				fpMoves[fpN] = regMove{dst: target, src: src}
				fpN++
			} else {
				deferred[deferredN] = tailDeferredArg{target: target, root: root, float: true}
				deferredN++
			}
			fp++
			continue
		}
		target := intArgRegs[gp]
		if resident {
			src := f.materialize(root)
			f.pinned = f.pinned.add(src)
			gpMoves[gpN] = regMove{dst: target, src: src}
			gpN++
		} else {
			deferred[deferredN] = tailDeferredArg{target: target, root: root}
			deferredN++
		}
		gp++
	}

	for _, move := range gpMoves[:gpN] {
		f.pinned = f.pinned.remove(move.src)
	}
	resolveRegMoves(gpMoves[:gpN], func(dst, src Reg) { f.a.MovReg64(dst, src) }, func(x, y Reg) { f.a.Xchg64(x, y) })
	for _, move := range fpMoves[:fpN] {
		f.fpinned = f.fpinned.remove(move.src)
	}
	fpSwapSlot := -1
	resolveRegMoves(fpMoves[:fpN],
		func(dst, src Reg) { f.a.FMov(dst, src, true) },
		func(x, y Reg) {
			if fpSwapSlot < 0 {
				fpSwapSlot = f.allocSpillSlot()
			}
			off := f.spillOff(fpSwapSlot)
			f.a.FStoreDisp(RSP, off, x, true)
			f.a.FMov(x, y, true)
			f.a.FLoadDisp(y, RSP, off, true)
		})
	for _, arg := range deferred[:deferredN] {
		if arg.float {
			switch arg.root.st.kind {
			case stConst:
				f.loadFConst(arg.target, arg.root.st)
			case stSlot:
				f.a.FLoadDisp(arg.target, RSP, f.spillOff(arg.root.st.slot), arg.root.st.typ == mtF64)
			case stLocalRef:
				f.a.FLoadDisp(arg.target, RSP, f.localOff(arg.root.st.idx), arg.root.st.typ == mtF64)
			}
			continue
		}
		switch arg.root.st.kind {
		case stConst:
			f.loadConst(arg.target, arg.root.st)
		case stSlot:
			f.a.Load64(arg.target, RSP, f.spillOff(arg.root.st.slot))
		case stLocalRef:
			f.a.Load64(arg.target, RSP, f.localOff(arg.root.st.idx))
		}
	}

	frameSite := f.a.Len() + 3
	f.a.AddRsp(0)
	f.sc.tailFrameSites = append(f.sc.tailFrameSites, frameSite)
	emitJump()
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
	f.flush()
	d := f.depth()
	if p > 0 {
		f.a.Load32(RAX, RSP, f.spillOff(d-p)) // first param
	} else {
		f.a.XorSelf32(RAX)
	}
	// Scratch entirely in RAX/RCX/RDX/R8: a host call clobbers no wasm register
	// state, so pinned locals (which may live in RDI/RSI) stay untouched.
	f.a.Load64(R8, RBX, -offCustomCtx) // R8 = host-call log
	f.a.Load32(RCX, R8, 0)             // count
	f.a.LeaScaled(RDX, R8, RCX, 3, 8)  // entry = log + count*8 + 8
	f.a.StoreImm32Mem(RDX, 0, int32(importIdx))
	f.a.Store32(RDX, 4, RAX)
	f.a.AluRI(0, RCX, 1, false) // count++ (digit 0 = add)
	f.a.Store32(R8, 0, RCX)
	f.setDepth(d - p)
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

// callHostSync lowers a call to a RETURNING imported (host) function via the
// synchronous re-entry protocol (see src/core/runtime/hostcall_amd64.go). The p
// params are marshaled into the off-heap control frame (at [linMem-offCustomCtx]);
// `call [ctrl+hcTrampoline]` runs the shared hostCallStub, which saves the wasm
// register state and unwinds to Go; Go runs the host function, writes the
// results, and resumes here; the rN results are read out of the control frame
// onto the operand stack.
//
// hostCallStub saves and resumeNative restores the callee-saved registers
// (RBX/RBP/R12..R15), so pinned locals and linMem survive the round trip and need
// no spilling — unlike a wasm→wasm call, whose callee reuses those registers.
// Value-pinned and module-pinned globals ARE synced around the call: the host may
// read or write the instance's globals through their cells.
func (f *fn) callHostSync(importIdx int, ft *wasm.CompType) error {
	f.stats.call(callKindHostSync)
	p, rN := len(ft.Params), len(ft.Results)
	paramSlots := funcTypeSlots(ft.Params)
	resultSlots := funcTypeSlots(ft.Results)
	if paramSlots > maxSyncHostSlots || resultSlots > maxSyncHostSlots {
		return fmt.Errorf("host import %d uses %d param slot(s), %d result slot(s); synchronous host imports support at most %d slots in each direction", importIdx, paramSlots, resultSlots, maxSyncHostSlots)
	}

	roots := f.rootsBottomToTop()
	d := len(roots)
	types := f.tmpTypes[:0]
	slotOf := f.tmpSlots[:0]
	slotTop := 0
	for _, root := range roots {
		typ := root.st.typ
		if root.kind == ekDeferred && root.typ != mtNone {
			typ = root.typ
		}
		types = append(types, typ)
		slotOf = append(slotOf, slotTop)
		slotTop += typ.stackSlots()
	}
	f.tmpTypes = types
	f.tmpSlots = slotOf
	belowTypes := f.tmpTypes2[:0]
	if cap(belowTypes) < d-p {
		belowTypes = make([]machineType, 0, d-p)
	}
	belowTypes = append(belowTypes, types[:d-p]...)
	f.tmpTypes2 = belowTypes

	f.flush()                   // operands to canonical slot-width slots
	f.storePinnedGlobals(false) // coherence: the host may read the current values
	f.storeModuleGlobals(RAX)

	// Marshal params into the control frame as wrapper-ABI slots. A v128 occupies
	// two adjacent little-endian uint64 slots, exactly like Invoke and cross-
	// instance wrapper calls.
	f.a.Load64(R8, RBX, -offCustomCtx) // R8 = control frame
	argSlot, ctrlSlot := 0, 0
	if p > 0 {
		argSlot = slotOf[d-p]
	}
	for i := 0; i < p; i++ {
		mt := mtOf(ft.Params[i])
		if mt.isV128() {
			x := f.allocFReg(0)
			f.a.VMovdquLoadDisp(x, RSP, f.spillOff(argSlot))
			f.a.VMovdquStoreDisp(R8, hcArgs+int32(ctrlSlot)*8, x)
			f.releaseF(x)
		} else if mt.is64() {
			f.a.Load64(RAX, RSP, f.spillOff(argSlot))
			f.a.Store64(R8, hcArgs+int32(ctrlSlot)*8, RAX)
		} else {
			f.a.Load32(RAX, RSP, f.spillOff(argSlot)) // zero-extends into RAX
			f.a.Store64(R8, hcArgs+int32(ctrlSlot)*8, RAX)
		}
		argSlot += mt.stackSlots()
		ctrlSlot += mt.stackSlots()
	}
	f.a.StoreImm32Mem(R8, hcImportIdx, int32(importIdx))
	// hcNArgs packs param slots (low 16) and result slots (high 16) so the Go
	// re-entry loop copies back only the real result count. Both are <= 16.
	f.a.StoreImm32Mem(R8, hcNArgs, int32(paramSlots|resultSlots<<16))

	// Park at the host call. Like the wrapper path, no post-call trap check: a
	// trap unwinds the whole native tree in one jump (it never returns here).
	f.a.CallMem(R8, hcTrampoline)

	f.deriveModuleGlobals() // the host may have written global cells
	f.derivePinnedGlobals()
	f.setDepthTypes(belowTypes)

	// Read results out of the control frame onto the operand stack, honoring
	// slot-width result layout for v128 and mixed scalar/vector signatures.
	f.a.Load64(R8, RBX, -offCustomCtx) // reload ctrl (clobbered by the round trip)
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
			f.a.VMovdquLoadDisp(res[j], R8, hcResults+int32(ctrlSlot)*8)
			f.fpinned = f.fpinned.add(res[j]) // keep across the remaining loads
		case rt.isFloat():
			tmp := f.allocReg(0)
			f.a.Load64(tmp, R8, hcResults+int32(ctrlSlot)*8)
			res[j] = f.allocFReg(0)
			f.a.MovGprToXmm(res[j], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[j])
		default:
			res[j] = f.allocReg(0)
			f.a.Load64(res[j], R8, hcResults+int32(ctrlSlot)*8)
			f.pinned = f.pinned.add(res[j]) // keep across the remaining loads
		}
		ctrlSlot += rt.stackSlots()
	}
	for j := 0; j < rN; j++ {
		switch rt := resTypes[j]; {
		case rt.isV128():
			f.fpinned = f.fpinned.remove(res[j])
			f.pushVReg(res[j])
		case rt.isFloat():
			f.fpinned = f.fpinned.remove(res[j])
			f.pushFReg(res[j], rt)
		default:
			f.pinned = f.pinned.remove(res[j])
			f.pushReg(res[j], rt)
		}
	}
	return nil
}

// HostIndirectThunk returns standalone machine code that logs a host call for
// importIdx and returns — for a legacy HostFunc reached through call_indirect
// (placed in a table as a funcref). It is entered with the wrapper ABI (RSI =
// linMem, RDI = args buffer), appends (importIdx, first-arg-i32) to the host-call
// log at [linMem-offCustomCtx] exactly like callHost, and returns void, so the
// normal post-invoke replay runs the host function. Emitted per host funcref into
// a per-instance mapping; the same code is instance-independent (it reads the log
// pointer from RSI at run time).
func HostIndirectThunk(importIdx uint32) []byte {
	a := &amd64.Asm{}
	a.Load32(RAX, RDI, 0)            // RAX = first arg (i32; a harmless slot read for 0-param funcs)
	a.Load64(R8, RSI, -offCustomCtx) // R8 = host-call log (RSI = linMem in the wrapper ABI)
	a.Load32(RCX, R8, 0)             // count
	a.LeaScaled(RDX, R8, RCX, 3, 8)  // entry = log + count*8 + 8
	a.StoreImm32Mem(RDX, 0, int32(importIdx))
	a.Store32(RDX, 4, RAX)    // arg
	a.AluRI(0, RCX, 1, false) // count++
	a.Store32(R8, 0, RCX)
	a.Ret()
	return a.B
}

// HostIndirectSyncThunk returns standalone machine code for a sync-mode host
// import reached through call_indirect. It is entered with the wrapper ABI
// (RDI=args, RCX=results, RSI=home linMem). Unlike HostIndirectThunk, it must not
// touch the async host log at offCustomCtx; sync-mode instances store the
// host-call control frame there. The thunk marshals raw uint64 wrapper slots into
// the control frame, parks via hostCallStub, then copies result slots back into
// the wrapper results buffer before returning to the wasm caller.
func HostIndirectSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return hostIndirectSyncThunk(importIdx, paramSlots, resultSlots, true)
}

// HostIndirectOwnedSyncThunk emits an explicitly owned host-funcref thunk. It
// uses the active caller's control frame rather than the descriptor's home frame,
// allowing the retained thunk to park through whichever same-store instance
// invoked the public token. The Runtime/store dispatcher resolves importIdx to
// the exact HostFuncRef owner.
func HostIndirectOwnedSyncThunk(importIdx uint32, paramSlots, resultSlots int) []byte {
	return hostIndirectSyncThunk(importIdx, paramSlots, resultSlots, false)
}

func hostIndirectSyncThunk(importIdx uint32, paramSlots, resultSlots int, useHome bool) []byte {
	a := &amd64.Asm{}
	// The host-call round trip preserves only callee-saved registers recorded by
	// hostCallStub. Save the caller's RBX (active linMem) and the wrapper result
	// pointer across the park/resume. Ordinary per-instance imports switch RBX to
	// their home context; explicitly owned imports keep the active caller context.
	a.Push(RBX)
	a.Push(RCX)
	if useHome {
		a.MovReg64(RBX, RSI)
	}
	a.Load64(R8, RBX, -offCustomCtx) // R8 = sync host-call control frame
	for i := 0; i < paramSlots; i++ {
		a.Load64(RAX, RDI, int32(i*8))
		a.Store64(R8, hcArgs+int32(i*8), RAX)
	}
	a.StoreImm32Mem(R8, hcImportIdx, int32(importIdx))
	a.StoreImm32Mem(R8, hcNArgs, int32(paramSlots|resultSlots<<16)) // low16 params, high16 results
	a.CallMem(R8, hcTrampoline)

	// resumeNative returns here with RSP pointing at the saved RCX, and with RBX
	// restored to the context saved by hostCallStub. Reload the control frame
	// (caller-saved registers were clobbered), restore the result pointer, copy
	// result slots, then restore the caller's original RBX and return.
	a.Load64(R8, RBX, -offCustomCtx)
	a.Pop(RCX)
	for i := 0; i < resultSlots; i++ {
		a.Load64(RAX, R8, hcResults+int32(i*8))
		a.Store64(RCX, int32(i*8), RAX)
	}
	a.Pop(RBX)
	a.Ret()
	return a.B
}

// Basedata scratch offsets (negative from the linMem base), matching the runtime
// and backend/railshot/amd64: a scratch cell to carry the indirect code pointer across the
// flush, and the indirect-call table descriptor pointer.
const (
	offTrapReentry = 24 // handler-jump re-entry SP (set per native entry)
	offCustomCtx   = 40 // host-call log pointer / sync host-call control frame
	offSpillRegion = 48 // 8B scratch
	offStackFence  = 72 // low stack bound for the fence check
	offTablePtr    = 80 // table descriptor pointer
	// offTrapCellPtr (== abi.TrapCellPtrOffset) is defined in memory.go.
)

// Control-frame field offsets for the synchronous host-call protocol. A
// returning host import needs no async log, so it reuses the customCtx slot
// (offCustomCtx) for its control frame. These MUST match
// src/core/runtime/hostcall_amd64.go (hcSavedRSP..hcResults, maxHostArity=16).
const (
	hcTrampoline     = 56  // u64: hostCallStub address (published per-instance by CallWithHost)
	hcImportIdx      = 64  // u32: native -> Go
	hcNArgs          = 68  // u32: low 16 bits = param slots, high 16 bits = result slots
	hcArgs           = 72  // [16]u64: native -> Go
	hcResults        = 200 // [16]u64: Go -> native (== hcArgs + 16*8)
	maxSyncHostSlots = 16  // must match runtime.MaxHostArity / maxHostArity
)

var instanceContextOffsets = [...]int32{
	offCustomCtx,
	offTablePtr,
	offFuncRefDescPtr,
	offPassiveElemPtr,
	offGlobalsPtr,
	offPassiveDataPtr,
	offTableDirPtr,
	offImportDispatchPtr,
}

func (f *fn) copyInstanceContext(dst, src Reg) {
	for i, off := range instanceContextOffsets {
		f.a.Load64(RAX, src, int32(i*8))
		f.a.Store64(dst, -off, RAX)
	}
}

// emitCrossInstanceCall lowers a call to an imported function that is bound to
// another instance's function (cross-instance linking). Unlike a host import
// (which logs and returns void), this is a real native call into the callee
// instance, staying on the same foreign stack. The callee's offset-0 entry
// re-establishes ITS module context from RSI=linMem (RBX, memSize R15, module
// globals R12-R14), so the caller's whole-module-invariant registers are
// preserved across the call by push/pop; the three per-execution control words
// (trap re-entry, stack fence, trap cell) are copied caller→callee so a trap in
// the callee unwinds to this execution's enterNative. Production code loads the
// callee entry, home memory, and target/caller contexts from the import dispatch
// cell; the immediate form remains only for focused backend callers.
func (f *fn) emitCrossInstanceCall(b ImportBinding, ft *wasm.CompType) error {
	kind := callKindCrossInstance
	if b.Dynamic {
		kind = callKindImportDispatch
	}
	f.stats.call(kind)
	p, rN := len(ft.Params), len(ft.Results)
	roots := f.rootsBottomToTop()
	d := len(roots)
	types := f.tmpTypes[:0]
	slotOf := f.tmpSlots[:0]
	slotTop := 0
	for _, root := range roots {
		typ := root.st.typ
		if root.kind == ekDeferred && root.typ != mtNone {
			typ = root.typ
		}
		types = append(types, typ)
		slotOf = append(slotOf, slotTop)
		slotTop += typ.stackSlots()
	}
	f.tmpTypes = types
	f.tmpSlots = slotOf
	belowTypes := f.tmpTypes2[:0]
	if cap(belowTypes) < d-p {
		belowTypes = make([]machineType, 0, d-p)
	}
	belowTypes = append(belowTypes, types[:d-p]...)
	f.tmpTypes2 = belowTypes
	resultSlot := slotTop
	resultSlots := funcTypeSlots(ft.Results)

	f.flush()
	f.storePinnedGlobals(false) // value-pinned globals → cells (reloaded after; callee can't touch B's cells)

	if need := resultSlot + resultSlots; need > f.maxSpill {
		f.maxSpill = need
	}
	argOff := f.spillOff(resultSlot) // p==0: unused, but a valid in-frame address
	if p > 0 {
		argOff = f.spillOff(slotOf[d-p])
	}
	f.spillLocalsForCall()
	f.storeModuleGlobals(RAX) // cross-instance boundary: shared globals must be cell-coherent

	// Args/results buffers as absolute pointers (survive the pushes below).
	f.a.LeaRsp(RDI, argOff)                 // args = &first arg slot
	f.a.LeaRsp(RCX, f.spillOff(resultSlot)) // results = &slot-width top

	// Preserve the caller's module-invariant registers (RBX=linMem, R15=memSize,
	// R12-R14=module globals) plus one alignment pad (6 pushes = 16-aligned → the
	// callee's offset-0 entry sees RSP ≡ 8 mod 16 after the CALL, as it expects).
	f.a.Push(RBX)
	f.a.Push(R12)
	f.a.Push(R13)
	f.a.Push(R14)
	f.a.Push(R15)
	f.a.Push(RAX) // alignment pad

	if b.Dynamic {
		if b.ImportIndex > uint32((1<<31-1-runtime.ImportDispatchCallerContextOffset)/runtime.ImportDispatchEntryBytes) {
			return fmt.Errorf("import dispatch index %d overflows displacement", b.ImportIndex)
		}
		disp := int32(b.ImportIndex * runtime.ImportDispatchEntryBytes)
		f.a.Load64(RAX, RBX, -offImportDispatchPtr)
		f.a.Load64(RSI, RAX, disp+runtime.ImportDispatchHomeLinMemOffset)    // wrapper-ABI arg 1
		f.a.Load64(R10, RAX, disp+runtime.ImportDispatchTargetContextOffset) // target context
		f.a.Load64(R9, RAX, disp+runtime.ImportDispatchCallerContextOffset)  // caller context
		f.a.Load64(R11, RAX, disp+runtime.ImportDispatchCodePtrOffset)       // wrapper entry
		f.a.Push(R9)
		f.a.Push(R10) // alignment pad + preserves the caller-context pair
		f.copyInstanceContext(RSI, R10)
	} else {
		f.a.MovImm64(RSI, b.CalleeLinMem) // callee linMem base (wrapper-ABI arg 1)
	}
	// Copy the per-execution control words caller(RBX)→callee(RSI).
	f.a.Load64(RAX, RBX, -offTrapReentry)
	f.a.Store64(RSI, -offTrapReentry, RAX)
	f.a.Load64(RAX, RBX, -offStackFence)
	f.a.Store64(RSI, -offStackFence, RAX)
	f.a.Load64(RAX, RBX, -offTrapCellPtr)
	f.a.Store64(RSI, -offTrapCellPtr, RAX)

	if b.Dynamic {
		f.a.CallReg(R11)
	} else {
		f.a.MovImm64(RAX, b.CalleeEntry)
		f.a.CallReg(RAX)
	}

	if b.Dynamic {
		f.a.Pop(R10) // alignment pad
		f.a.Pop(R9)  // caller context
	}
	f.a.Pop(RAX) // alignment pad
	f.a.Pop(R15)
	f.a.Pop(R14)
	f.a.Pop(R13)
	f.a.Pop(R12)
	f.a.Pop(RBX)
	if b.Dynamic {
		f.copyInstanceContext(RBX, R9)
	}

	f.reloadLocalsForCall() // non-STACK_REG model only
	f.deriveModuleGlobals() // cross-instance callee may have written shared global cells
	f.derivePinnedGlobals() // reload value-pinned globals from B's cells

	// Pop the args; load results out of their slot-width ABI area.
	f.setDepthTypes(belowTypes)
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
	resSlot := resultSlot
	for i := 0; i < rN; i++ {
		rt := mtOf(ft.Results[i])
		resTypes[i] = rt
		switch {
		case rt.isV128():
			res[i] = f.allocFReg(0)
			f.a.VMovdquLoadDisp(res[i], RSP, f.spillOff(resSlot))
			f.fpinned = f.fpinned.add(res[i])
		case rt.isFloat():
			tmp := f.allocReg(0)
			f.a.Load64(tmp, RSP, f.spillOff(resSlot))
			res[i] = f.allocFReg(0)
			f.a.MovGprToXmm(res[i], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[i])
		default:
			res[i] = f.allocReg(0)
			f.a.Load64(res[i], RSP, f.spillOff(resSlot))
			f.pinned = f.pinned.add(res[i])
		}
		resSlot += rt.stackSlots()
	}
	for i := 0; i < rN; i++ {
		switch rt := resTypes[i]; {
		case rt.isV128():
			f.fpinned = f.fpinned.remove(res[i])
			f.pushVReg(res[i])
		case rt.isFloat():
			f.fpinned = f.fpinned.remove(res[i])
			f.pushFReg(res[i], rt)
		default:
			f.pinned = f.pinned.remove(res[i])
			f.pushReg(res[i], rt)
		}
	}
	return nil
}

// callInternal lowers a direct call to another local function. Integer-only
// callees use the fast register ABI (args/result in registers); others go
// through the wrapper (rsp-buffer) ABI.
func (f *fn) callInternal(localIdx int, ft *wasm.CompType, resHint int) error {
	if regABIEnabled && sigFitsRegABI(ft) {
		if sigIsIntOnly(ft) {
			f.stats.call(callKindRegisterABI)
			f.emitRegisterCall(localIdx, ft, resHint)
		} else {
			f.stats.call(callKindMixed)
			f.emitMixedRegisterCall(localIdx, ft)
		}
		return nil
	}
	f.stats.call(callKindWrapper)
	f.emitWrapperCall(ft, func() {
		site := f.a.CallRel32()
		f.relocs = append(f.relocs, callReloc{at: site, target: localIdx})
	})
	return nil
}

// emitRegisterCall lowers an internal call to a register-ABI function: the top p
// operands become the argument registers (via a parallel move), the callee is
// entered at its internal entry, and the single result is taken from RAX.
// resHint >= 0 fuses a following `local.set resHint`: RAX moves straight into
// the pinned local's register instead of an allocated result register.
func (f *fn) emitRegisterCall(localIdx int, ft *wasm.CompType, resHint int) {
	f.emitRegisterCallVia(ft, resHint, localIdx, regNone)
}

// emitRegisterCallVia emits either a direct internal rel32 call (localIdx >= 0)
// or an indirect register call. Explicit operands avoid a closure per wasm call.
func (f *fn) emitRegisterCallVia(ft *wasm.CompType, resHint int, localIdx int, indirect Reg) {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()
	f.storePinnedGlobals(false) // spill value-pinned globals to their cells before the call (scratch is free here)

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
			cur = baseOfValentBlock(cur).prev
		}
	}

	// Register-resident args (deferred/reg/pinned-local) are materialized into
	// owned, pinned registers now (protected from the flush below); const/memory
	// args are loaded straight into their target register afterward.
	moves := f.tmpMoves[:0]
	deferred := f.tmpDeferred[:0]
	for i := 0; i < p; i++ {
		root := argRoots[i]
		if root.isDeferred() || (root.kind == ekValue && (root.st.kind == stReg || root.st.kind == stLocalReg || root.st.kind == stGlobReg || root.st.kind == stMemRef)) {
			reg := f.materialize(root) // stMemRef → emits the deferred load into its addr reg
			f.pinned = f.pinned.add(reg)
			moves = append(moves, regMove{dst: intArgRegs[i], src: reg})
		} else {
			deferred = append(deferred, deferredArg{target: intArgRegs[i], root: root})
		}
	}
	if p > 0 {
		f.flushBelow(argRoots[0]) // operands below the args → canonical slots
	} else {
		f.flush()
	}
	// Store dirty pinned locals BEFORE the argument staging: a pinned local may
	// live in an argument register (R9-R11 for 5+-arg calls) or in RDI/RSI
	// (clobbered by the linMem/trap setup below), not just in a callee-clobbered
	// register. Their values were already copied out above where an argument reads
	// them. Lazy reload on the next read — WARP's STACK_REG model.
	f.spillLocalsForCall()

	// Unpin the owned source registers, then resolve the parallel move into targets.
	for _, m := range moves {
		f.pinned = f.pinned.remove(m.src)
	}
	resolveRegMoves(moves, func(dst, src Reg) { f.a.MovReg64(dst, src) }, func(x, y Reg) { f.a.Xchg64(x, y) })
	f.tmpMoves = moves[:0]
	for _, da := range deferred {
		switch da.root.st.kind {
		case stConst:
			f.loadConst(da.target, da.root.st)
		case stSlot:
			f.a.Load64(da.target, RSP, f.spillOff(da.root.st.slot))
		case stLocalRef:
			f.a.Load64(da.target, RSP, f.localOff(da.root.st.idx))
		}
	}
	f.tmpDeferred = deferred[:0]

	// Consume the args; the operand model is now the k below-operands in slots.
	f.setDepth(d - p)

	// No environment passing: RBX (linMem) is a whole-module invariant and the
	// trap cell pointer lives in basedata — the callee inherits both (WARP model).
	if localIdx >= 0 {
		site := f.a.CallRel32()
		f.relocs = append(f.relocs, callReloc{at: site, target: localIdx, internal: true})
	} else {
		f.a.CallReg(indirect)
	}

	// Capture the result(s) out of the return registers before the reload
	// sequence below reuses RAX/RDX as scratch. Single int → RAX; two ints →
	// RAX/RDX (mirrors arm64's X0/X1 pair return).
	resReg := regNone
	if rN == 1 && resHint < 0 {
		resReg = f.allocReg(maskOf(RAX))
		f.a.MovReg64(resReg, RAX)
		f.pinned = f.pinned.add(resReg)
	}
	var pairRes [2]Reg
	if rN == 2 {
		pairRes[0] = f.allocReg(maskOf(RAX, RDX))
		f.pinned = f.pinned.add(pairRes[0])
		f.a.MovReg64(pairRes[0], RAX)
		pairRes[1] = f.allocReg(maskOf(RAX, RDX))
		f.a.MovReg64(pairRes[1], RDX)
		f.pinned = f.pinned.add(pairRes[1])
	}
	f.reloadLocalsForCall() // non-STACK_REG model only
	f.derivePinnedGlobals() // reload value-pinned globals: the callee may have changed the shared cell
	// No post-call trap check: a callee trap jumps straight back to enterNative
	// via emitTrap's handler-jump, so control never returns here with *trap set.

	if rN == 1 && resHint >= 0 {
		// Fused `local.set`: the result lands directly in the pinned local's
		// register — after any eager post-call reload, which would otherwise
		// overwrite it with the stale slot value.
		pr, _, _ := f.pinReg(resHint)
		f.a.MovReg64(pr, RAX)
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
}

// emitMixedRegisterCall uses the register ABI for signatures containing floats.
// GP and FP arguments are staged independently as parallel moves, so values that
// are already resident in registers do not round-trip through canonical slots
// (mirrors arm64's mixed staging). Only const/slot args are loaded from memory.
func (f *fn) emitMixedRegisterCall(localIdx int, ft *wasm.CompType) {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()

	f.storePinnedGlobals(false) // spill value-pinned globals to their cells before the call

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
			cur = baseOfValentBlock(cur).prev
		}
	}

	// Register-resident args are materialized into owned, pinned registers now
	// (per bank), so the flush below cannot spill them; const/slot/local-ref args
	// are deferred and loaded straight into their target register afterward.
	var gpBuf, fpBuf [8]regMove
	var deferredBuf [16]deferredMixedArg
	gpMoves, fpMoves := gpBuf[:0], fpBuf[:0]
	deferred := deferredBuf[:0]
	gp, fp := 0, 0
	for i, t := range ft.Params {
		mt := mtOf(t)
		root := argRoots[i]
		regResident := root.isDeferred() || (root.kind == ekValue && (root.st.kind == stReg || root.st.kind == stLocalReg || root.st.kind == stGlobReg || root.st.kind == stMemRef))
		if mt.isFloat() {
			target := fpArgRegs[fp]
			if regResident {
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
			if regResident {
				reg := f.materialize(root) // stMemRef → emits the deferred load into its addr reg
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
		f.flushBelow(argRoots[0]) // operands below the args → canonical slots
	} else {
		f.flush()
	}
	// Store dirty pinned locals AFTER their values were copied out above (an arg may
	// read a pinned local); a mixed callee may clobber every caller pin.
	f.spillLocalsForCall()

	// Resolve each bank's parallel move independently. GP swaps use XCHG; XMM has no
	// swap, so a cyclic FP move goes through one reused spill slot (like arm64).
	for _, m := range gpMoves {
		f.pinned = f.pinned.remove(m.src)
	}
	resolveRegMoves(gpMoves, func(dst, src Reg) { f.a.MovReg64(dst, src) }, func(x, y Reg) { f.a.Xchg64(x, y) })
	for _, m := range fpMoves {
		f.fpinned = f.fpinned.remove(m.src)
	}
	fpSwapSlot := -1
	resolveRegMoves(fpMoves,
		func(dst, src Reg) { f.a.FMov(dst, src, true) },
		func(x, y Reg) {
			if fpSwapSlot < 0 {
				fpSwapSlot = f.allocSpillSlot()
			}
			off := f.spillOff(fpSwapSlot)
			f.a.FStoreDisp(RSP, off, x, true)
			f.a.FMov(x, y, true)
			f.a.FLoadDisp(y, RSP, off, true)
		})
	for _, da := range deferred {
		if da.float {
			switch da.root.st.kind {
			case stConst:
				f.loadFConst(da.target, da.root.st)
			case stSlot:
				f.a.FLoadDisp(da.target, RSP, f.spillOff(da.root.st.slot), da.root.st.typ == mtF64)
			case stLocalRef:
				f.a.FLoadDisp(da.target, RSP, f.localOff(da.root.st.idx), da.root.st.typ == mtF64)
			}
			continue
		}
		switch da.root.st.kind {
		case stConst:
			f.loadConst(da.target, da.root.st)
		case stSlot:
			f.a.Load64(da.target, RSP, f.spillOff(da.root.st.slot))
		case stLocalRef:
			f.a.Load64(da.target, RSP, f.localOff(da.root.st.idx))
		}
	}
	f.setDepth(d - p)

	site := f.a.CallRel32()
	f.relocs = append(f.relocs, callReloc{at: site, target: localIdx, internal: true})

	// Capture integer results out of RAX/RDX before the reload below reuses them as
	// scratch. A float result stays in XMM0 (never a pin target, so reload-safe).
	resReg := regNone
	if rN == 1 && !mtOf(ft.Results[0]).isFloat() {
		resReg = f.allocReg(maskOf(RAX))
		f.a.MovReg64(resReg, RAX)
		f.pinned = f.pinned.add(resReg)
	}
	var pairRes [2]Reg
	if rN == 2 {
		pairRes[0] = f.allocReg(maskOf(RAX, RDX))
		f.pinned = f.pinned.add(pairRes[0])
		f.a.MovReg64(pairRes[0], RAX)
		pairRes[1] = f.allocReg(maskOf(RAX, RDX))
		f.a.MovReg64(pairRes[1], RDX)
		f.pinned = f.pinned.add(pairRes[1])
	}
	f.reloadLocalsForCall() // non-STACK_REG model only
	f.derivePinnedGlobals() // reload value-pinned globals: the callee may have changed the shared cell

	if rN == 1 {
		rt := mtOf(ft.Results[0])
		if rt.isFloat() {
			f.pushFReg(0, rt) // XMM0
		} else {
			f.pinned = f.pinned.remove(resReg)
			f.pushReg(resReg, rt)
		}
	}
	if rN == 2 {
		for i, reg := range pairRes {
			f.pinned = f.pinned.remove(reg)
			f.pushReg(reg, mtOf(ft.Results[i]))
		}
	}
}

// callRef invokes the descriptor-pointer funcref on top of the operand stack.
// The immediate type supplies the expected canonical signature. Local int-only
// descriptors may carry a tagged internal register-ABI entry; every other
// descriptor uses the existing wrapper/cross-instance context path shared with
// call_indirect.
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
	canon := int32(f.m.StructuralTypeID(typeIdx))

	ref := f.materialize(f.popValue())
	f.pinned = f.pinned.add(ref)
	f.a.TestSelf(ref, true)
	f.trapIf(condE, trapIndirectOOB)

	code := f.allocReg(0)
	f.a.Load64(code, ref, runtime.TableEntryCodePtrOffset)
	f.a.TestSelf(code, true)
	f.trapIf(condE, trapIndirectOOB)
	tid := f.allocReg(maskOf(code))
	f.a.Load32(tid, ref, runtime.TableEntrySigIDOffset)
	f.a.AluRI(cmpDigit, tid, canon, false)
	f.release(tid)
	f.trapIf(condNE, trapIndirectSig)
	home := f.allocReg(maskOf(ref, code))
	f.a.Load64(home, ref, runtime.TableEntryHomeLinMemOffset)
	f.pinned = f.pinned.remove(ref)
	f.release(ref)

	if sigFitsRegABI(ft) && sigIsIntOnly(ft) {
		// A tagged descriptor points at a same-instance internal entry. Untagged
		// descriptors name wrapper entries and retain their real home linMem.
		roots := f.rootsBottomToTop()
		types := make([]machineType, len(roots))
		for i, root := range roots {
			types[i] = root.st.typ
			if root.kind == ekDeferred && root.typ != mtNone {
				types[i] = root.typ
			}
		}
		f.pinned = f.pinned.add(code).add(home)
		f.flush()
		savedLocals := append([]localDef(nil), f.locals...)
		tag := f.allocReg(maskOf(code, home))
		f.a.MovReg64(tag, home)
		f.a.ShiftImm(5, tag, 63, true)
		f.a.TestSelf(tag, true)
		f.release(tag)
		wrapper := f.a.JccPlaceholder(condE)
		f.pinned = f.pinned.remove(home)
		f.emitRegisterCallVia(ft, -1, func() { f.a.CallReg(code) })
		f.pinned = f.pinned.remove(code)
		f.release(code)
		done := f.a.JmpPlaceholder()

		f.a.PatchRel32(wrapper, f.a.Len())
		f.locals = savedLocals
		f.setDepthTypes(types)
		f.a.Store64(RBX, -int32(offSpillRegion), code)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		f.a.ShiftImm(4, home, 1, true)
		f.a.ShiftImm(5, home, 1, true)
		f.emitIndirectCallHomeAware(ft, home)
		f.a.PatchRel32(done, f.a.Len())
		return nil
	}

	f.a.Store64(RBX, -int32(offSpillRegion), code)
	f.release(code)
	f.emitIndirectCallHomeAware(ft, home)
	return nil
}

// returnCallIndirect lowers the bounded indirect-tail milestone for a private,
// immutable local funcref table. That proof guarantees that every same-signature
// non-null entry is a same-module internal register-ABI target, so after the same
// bounds/null/canonical-signature checks as call_indirect the current frame can
// be released and the entry jumped to directly. Mutable/imported/exported tables
// and wrapper-only signatures remain explicit backend rejections.
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
	if !sameValTypes(f.ft.Results, ft.Results) {
		return fmt.Errorf("return_call_indirect: type %d result shape differs from caller", typeIdx)
	}
	if !sigFitsRegABI(f.ft) || !sigFitsRegABI(ft) {
		return fmt.Errorf("return_call_indirect: caller or type %d requires unsupported indirect tail ABI", typeIdx)
	}
	if tableIdx != 0 || !f.immutableLocalTable {
		return fmt.Errorf("return_call_indirect: table %d is not a private immutable local funcref table", tableIdx)
	}
	f.stats.call("tail-indirect")
	canon := int32(f.m.StructuralTypeID(typeIdx))

	idxReg := f.materialize(f.popValue())
	f.pinned = f.pinned.add(idxReg)
	tbl := f.allocReg(0)
	f.loadTableDescriptor(tbl, tableIdx)
	f.pinned = f.pinned.add(tbl)
	ln := f.allocReg(0)
	f.a.Load32(ln, tbl, 0)
	f.a.AluRR(0x39, idxReg, ln, false)
	f.release(ln)
	f.trapIf(condAE, trapIndirectOOB)

	f.a.ShiftImm(4, idxReg, 5, true)
	f.a.AluRR(0x01, idxReg, tbl, true)
	f.pinned = f.pinned.remove(tbl)
	f.release(tbl)
	code := f.allocReg(0)
	f.a.Load64(code, idxReg, 8)
	f.a.TestSelf(code, true)
	f.trapIf(condE, trapIndirectOOB)
	if f.immutableTableTyped && f.immutableTableType == uint32(canon) {
		f.stats.peep("immutable-table-type-check-elide")
	} else {
		tid := f.allocReg(maskOf(code))
		f.a.Load32(tid, idxReg, 16)
		f.a.AluRI(cmpDigit, tid, canon, false)
		f.release(tid)
		f.trapIf(condNE, trapIndirectSig)
	}
	f.pinned = f.pinned.remove(idxReg)
	f.release(idxReg)

	// The basedata scratch survives frame teardown and is not an argument bank.
	// Reloading into RSI after staging avoids reserving a GP argument register for
	// the indirect code pointer.
	f.a.Store64(RBX, -int32(offSpillRegion), code)
	f.release(code)
	f.emitTailRegisterJump(ft, func() {
		f.a.Load64(RSI, RBX, -int32(offSpillRegion))
		f.a.JmpReg(RSI)
	})
	f.unreachable = true
	return nil
}

// callIndirect lowers call_indirect: bounds-check the table index, verify the
// entry's canonical type id, reject a null entry, then call the entry's code
// pointer via the wrapper ABI. Table layout matches the runtime (32-byte entries;
// +8 code ptr, +16 type id) with the descriptor pointer at [linMem-offTablePtr].
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
	canon := int32(f.m.StructuralTypeID(typeIdx))

	idxReg := f.materialize(f.popValue()) // table index (i32)
	f.pinned = f.pinned.add(idxReg)
	tbl := f.allocReg(0)
	f.loadTableDescriptor(tbl, tableIdx)
	f.pinned = f.pinned.add(tbl)

	ln := f.allocReg(0)
	f.a.Load32(ln, tbl, 0) // table length
	f.a.AluRR(0x39, idxReg, ln, false)
	f.release(ln)
	f.trapIf(condAE, trapIndirectOOB) // idx >= length → cold stub

	// 64-bit pointer arithmetic: entry address = tbl + idx*32 (TableEntryBytes).
	f.a.ShiftImm(4, idxReg, 5, true)   // idx *= 32
	f.a.AluRR(0x01, idxReg, tbl, true) // idx += tbl
	f.pinned = f.pinned.remove(tbl)
	f.release(tbl)

	// Entry fields (folding the 8-byte descriptor header): +8 code, +16 sig id,
	// +24 home linMem. Check null (uninitialized element) BEFORE the signature so a
	// zero-initialized entry traps as an empty slot, not a type mismatch.
	code := f.allocReg(0)
	f.a.Load64(code, idxReg, 8) // entry code ptr (offset-0 entry)
	f.a.TestSelf(code, true)
	f.trapIf(condE, trapIndirectOOB) // null entry

	if tableIdx == 0 && f.immutableTableTyped && f.immutableTableType == uint32(canon) {
		// A uniformly-typed immutable table cannot hold a mismatched signature.
		f.stats.peep("immutable-table-type-check-elide")
	} else {
		tid := f.allocReg(maskOf(code))
		f.a.Load32(tid, idxReg, 16) // entry type id
		f.a.AluRI(cmpDigit, tid, canon, false)
		f.release(tid)
		f.trapIf(condNE, trapIndirectSig)
	}

	// With one private local immutable table and no function imports, every non-null
	// entry is necessarily a same-module internal entry. Skip loading its home
	// pointer, testing the internal-entry tag, and emitting the wrapper/cross-instance
	// fork; the OOB/null/type checks above are still required and remain on the hot
	// path. A monomorphic table (single target) collapses to a direct call.
	if tableIdx == 0 && f.immutableLocalTable && f.monomorphicTarget >= 0 && sigFitsRegABI(ft) && sigIsIntOnly(ft) {
		f.pinned = f.pinned.remove(idxReg)
		f.release(idxReg)
		f.release(code)
		f.stats.peep("monomorphic-call-indirect")
		f.emitRegisterCall(f.monomorphicTarget, ft, -1)
		return nil
	}
	if tableIdx == 0 && f.immutableLocalTable && sigFitsRegABI(ft) && sigIsIntOnly(ft) {
		f.pinned = f.pinned.remove(idxReg)
		f.release(idxReg)
		f.pinned = f.pinned.add(code)
		f.stats.peep("immutable-local-call-indirect")
		f.emitRegisterCallVia(ft, -1, -1, code)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		return nil
	}

	home := f.allocReg(maskOf(idxReg, code))
	f.a.Load64(home, idxReg, 24) // entry home linMem base
	canonical := f.allocReg(maskOf(idxReg, code, home))
	f.a.Load64(canonical, idxReg, 32) // canonical descriptor pointer
	f.a.TestSelf(canonical, true)
	f.trapIf(condE, trapIndirectOOB)
	targetContext := f.allocReg(maskOf(idxReg, code, home, canonical))
	f.a.Load64(targetContext, canonical, runtime.FuncRefContextOffset)
	f.a.TestSelf(targetContext, true)
	f.trapIf(condE, trapIndirectOOB)
	f.release(canonical)
	f.pinned = f.pinned.remove(idxReg)
	f.release(idxReg)
	if sigFitsRegABI(ft) && sigIsIntOnly(ft) {
		// Local function descriptors may point directly at the internal register-
		// ABI entry and tag bit 63 of homeLinMem. Split that fast path before the
		// wrapper/cross-instance lowering; treating the tagged value as a real
		// linMem pointer faults as soon as the cross-instance path dereferences it.
		roots := f.rootsBottomToTop()
		types := make([]machineType, len(roots))
		for i, root := range roots {
			types[i] = root.st.typ
			if root.kind == ekDeferred && root.typ != mtNone {
				types[i] = root.typ
			}
		}
		f.pinned = f.pinned.add(code).add(home).add(targetContext)
		f.flush()
		savedLocals := append([]localDef(nil), f.locals...)
		tag := f.allocReg(maskOf(code, home))
		f.a.MovReg64(tag, home)
		f.a.ShiftImm(5, tag, 63, true) // logical high-bit extract
		f.a.TestSelf(tag, true)
		f.release(tag)
		wrapper := f.a.JccPlaceholder(condE)
		f.pinned = f.pinned.remove(home).remove(targetContext)
		f.release(targetContext)
		f.emitRegisterCallVia(ft, -1, -1, code)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		done := f.a.JmpPlaceholder()

		f.a.PatchRel32(wrapper, f.a.Len())
		f.locals = savedLocals
		f.setDepthTypes(types)
		f.a.Store64(RBX, -int32(offSpillRegion), code)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		// Clear only the descriptor tag while retaining the full canonical
		// pointer without spending an immediate-mask register.
		f.a.ShiftImm(4, home, 1, true)
		f.a.ShiftImm(5, home, 1, true)
		f.emitIndirectCallHomeAware(ft, home, targetContext)
		f.a.PatchRel32(done, f.a.Len())
		return nil
	}

	// Stash the code ptr in linMem scratch so it survives the call staging.
	f.a.Store64(RBX, -int32(offSpillRegion), code)
	f.release(code)

	f.emitIndirectCallHomeAware(ft, home, targetContext)
	return nil
}

// emitIndirectCallHomeAware makes the indirect call to the code ptr stashed at
// [linMem-offSpillRegion], running the funcref in its home instance's context.
// homeReg holds the entry's home linMem base and targetContextReg identifies its
// owning instance. Matching caller/target contexts take the plain frameless
// wrapper path, even when memory aliases are possible. Otherwise preserve the caller's
// whole-module-invariant registers (RBX, R12-R15), copy the per-execution control
// words caller→callee, and enter the callee's offset-0 entry with RSI = its linMem
// (the same context-swap as emitCrossInstanceCall, selected at run time).
func (f *fn) emitIndirectCallHomeAware(ft *wasm.CompType, homeReg, targetContextReg Reg) {
	p, rN := len(ft.Params), len(ft.Results)
	roots := f.rootsBottomToTop()
	d := len(roots)
	types := f.tmpTypes[:0]
	slotOf := f.tmpSlots[:0]
	slotTop := 0
	for _, root := range roots {
		typ := root.st.typ
		if root.kind == ekDeferred && root.typ != mtNone {
			typ = root.typ
		}
		types = append(types, typ)
		slotOf = append(slotOf, slotTop)
		slotTop += typ.stackSlots()
	}
	f.tmpTypes = types
	f.tmpSlots = slotOf
	belowTypes := f.tmpTypes2[:0]
	if cap(belowTypes) < d-p {
		belowTypes = make([]machineType, 0, d-p)
	}
	belowTypes = append(belowTypes, types[:d-p]...)
	f.tmpTypes2 = belowTypes
	resultSlot := slotTop
	resultSlots := 0
	for _, rt := range ft.Results {
		resultSlots += mtOf(rt).stackSlots()
	}

	// Stash the home linear-memory and target-context pointers above the results.
	// The frame is stable during the frameless call, so both survive arg staging
	// and the cross-instance path's RSP changes.
	homeSlot := resultSlot + resultSlots
	targetContextSlot := homeSlot + 1
	if need := targetContextSlot + 1; need > f.maxSpill {
		f.maxSpill = need
	}
	f.a.Store64(RSP, f.spillOff(homeSlot), homeReg)
	f.a.Store64(RSP, f.spillOff(targetContextSlot), targetContextReg)
	f.release(homeReg)
	f.release(targetContextReg)

	f.flush()                        // args → canonical slot-width slots
	f.storePinnedGlobals(false)      // value-pinned globals → cells
	f.storeModuleGlobals(RAX)        // same-instance callee's offset-0 prologue reloads from cells
	argOff := f.spillOff(resultSlot) // p==0: unused, but a valid in-frame address
	if p > 0 {
		argOff = f.spillOff(slotOf[d-p])
	}
	f.spillLocalsForCall()
	f.a.LeaRsp(RDI, argOff)                 // args = &first arg slot
	f.a.LeaRsp(RCX, f.spillOff(resultSlot)) // results = &slot top

	f.a.Load64(R11, RSP, f.spillOff(homeSlot))          // target home linMem
	f.a.Load64(R10, RSP, f.spillOff(targetContextSlot)) // target instance context
	f.a.Load64(R9, RBX, -offFuncRefDescPtr)
	f.a.Load64(R9, R9, runtime.FuncRefContextOffset) // caller instance context
	f.a.Cmp64(R10, R9)
	jne := f.a.JccPlaceholder(condNE)
	// Same instance: RSI = caller linMem, call the entry directly.
	f.a.MovReg64(RSI, RBX)
	f.a.CallMem(RBX, -int32(offSpillRegion))
	jdone := f.a.JmpPlaceholder()
	// Cross-instance: preserve the caller's invariants (+ one alignment pad), copy
	// the control words caller→callee, enter with RSI = callee linMem, then restore.
	f.a.PatchRel32(jne, f.a.Len())
	f.a.Push(RBX)
	f.a.Push(R12)
	f.a.Push(R13)
	f.a.Push(R14)
	f.a.Push(R15)
	f.a.Push(RAX) // alignment pad
	f.a.Push(R9)
	f.a.Push(R10) // alignment pad + preserves the caller-context pair
	f.copyInstanceContext(R11, R10)
	f.a.Load64(RAX, RBX, -offTrapReentry)
	f.a.Store64(R11, -offTrapReentry, RAX)
	f.a.Load64(RAX, RBX, -offStackFence)
	f.a.Store64(R11, -offStackFence, RAX)
	f.a.Load64(RAX, RBX, -offTrapCellPtr)
	f.a.Store64(R11, -offTrapCellPtr, RAX)
	f.a.MovReg64(RSI, R11)
	f.a.CallMem(RBX, -int32(offSpillRegion)) // RBX unchanged by the pushes
	f.a.Pop(R10)
	f.a.Pop(R9)
	f.a.Pop(RAX)
	f.a.Pop(R15)
	f.a.Pop(R14)
	f.a.Pop(R13)
	f.a.Pop(R12)
	f.a.Pop(RBX)
	f.copyInstanceContext(RBX, R9)
	f.deriveModuleGlobals() // cross-instance callee may have written shared global cells
	f.a.PatchRel32(jdone, f.a.Len())

	f.reloadLocalsForCall()
	f.derivePinnedGlobals()

	// Pop the args; load results out of their slot-width ABI area into fresh registers.
	f.setDepthTypes(belowTypes)
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
	resSlot := resultSlot
	for i := 0; i < rN; i++ {
		rt := mtOf(ft.Results[i])
		resTypes[i] = rt
		switch {
		case rt.isV128():
			res[i] = f.allocFReg(0)
			f.a.VMovdquLoadDisp(res[i], RSP, f.spillOff(resSlot))
			f.fpinned = f.fpinned.add(res[i])
		case rt.isFloat():
			tmp := f.allocReg(0)
			f.a.Load64(tmp, RSP, f.spillOff(resSlot))
			res[i] = f.allocFReg(0)
			f.a.MovGprToXmm(res[i], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[i])
		default:
			res[i] = f.allocReg(0)
			f.a.Load64(res[i], RSP, f.spillOff(resSlot))
			f.pinned = f.pinned.add(res[i])
		}
		resSlot += rt.stackSlots()
	}
	for i := 0; i < rN; i++ {
		rt := resTypes[i]
		switch {
		case rt.isV128():
			f.fpinned = f.fpinned.remove(res[i])
			f.pushVReg(res[i])
		case rt.isFloat():
			f.fpinned = f.fpinned.remove(res[i])
			f.pushFReg(res[i], rt)
		default:
			f.pinned = f.pinned.remove(res[i])
			f.pushReg(res[i], rt)
		}
	}
}

// emitWrapperCall sets up the wrapper ABI registers (RDI=args, RCX=results,
// RSI=linMem, RDX=trap), runs emitCall, and loads the results back onto the
// operand stack. Frameless: the wrapper argument and result buffers are the
// operand SPILL SLOTS themselves — after the flush, the p arguments already sit
// contiguously and in order at their canonical spill slots (exactly the wrapper
// ABI layout the callee's prologue reads), and the results land in free slots
// just above the current operand slot top. So there is no separate native-stack
// buffer and no transient SubRsp/AddRsp — RSP stays put for the whole call.
func (f *fn) emitWrapperCall(ft *wasm.CompType, emitCall func()) {
	p, rN := len(ft.Params), len(ft.Results)
	roots := f.rootsBottomToTop()
	d := len(roots)
	types := f.tmpTypes[:0]
	slotOf := f.tmpSlots[:0]
	slotTop := 0
	for _, root := range roots {
		typ := root.st.typ
		if root.kind == ekDeferred && root.typ != mtNone {
			typ = root.typ
		}
		types = append(types, typ)
		slotOf = append(slotOf, slotTop)
		slotTop += typ.stackSlots()
	}
	f.tmpTypes = types
	f.tmpSlots = slotOf
	belowTypes := f.tmpTypes2[:0]
	if cap(belowTypes) < d-p {
		belowTypes = make([]machineType, 0, d-p)
	}
	belowTypes = append(belowTypes, types[:d-p]...)
	f.tmpTypes2 = belowTypes
	resultSlot := slotTop
	resultSlots := 0
	for _, rt := range ft.Results {
		resultSlots += mtOf(rt).stackSlots()
	}

	f.flush()                   // all operands to canonical slots; args start at slotOf[d-p]
	f.storePinnedGlobals(false) // spill value-pinned globals to their cells before the call
	f.storeModuleGlobals(RAX)   // wrapper callee's offset-0 prologue reloads from the cells

	// Reserve result slots above the full slot-width operand area, including v128 args.
	if need := resultSlot + resultSlots; need > f.maxSpill {
		f.maxSpill = need
	}
	argOff := f.spillOff(resultSlot) // p==0: unused, but a valid in-frame address
	if p > 0 {
		argOff = f.spillOff(slotOf[d-p])
	}
	// Store dirty pinned locals BEFORE the call-setup writes below: a pinned
	// local may live in RDI/RSI (clobbered by the setup itself), not just in a
	// callee-clobbered register. Lazy reload on the next read — WARP's STACK_REG.
	f.spillLocalsForCall()
	f.a.LeaRsp(RDI, argOff)                 // args = &first arg slot
	f.a.LeaRsp(RCX, f.spillOff(resultSlot)) // results = &slot top
	f.a.MovReg64(RSI, RBX)                  // linMem (kept in RBX); trap ptr lives in basedata
	emitCall()

	// No post-call trap check: a callee trap unwinds the whole native call tree
	// in one jump (emitTrap's handler-jump back to enterNative), so control never
	// returns here with *trap set.
	f.reloadLocalsForCall() // non-STACK_REG model only
	f.derivePinnedGlobals() // reload value-pinned globals: the callee may have changed the shared cell

	// Pop the args; load results out of their slot-width ABI area into fresh registers.
	f.setDepthTypes(belowTypes)
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
	resSlot := resultSlot
	for i := 0; i < rN; i++ {
		rt := mtOf(ft.Results[i])
		resTypes[i] = rt
		switch {
		case rt.isV128():
			res[i] = f.allocFReg(0)
			f.a.VMovdquLoadDisp(res[i], RSP, f.spillOff(resSlot))
			f.fpinned = f.fpinned.add(res[i]) // keep across the remaining loads
		case rt.isFloat():
			// Load the 8-byte result word into a GP scratch, then into an XMM reg.
			tmp := f.allocReg(0)
			f.a.Load64(tmp, RSP, f.spillOff(resSlot))
			res[i] = f.allocFReg(0)
			f.a.MovGprToXmm(res[i], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[i]) // keep across the remaining loads
		default:
			res[i] = f.allocReg(0)
			f.a.Load64(res[i], RSP, f.spillOff(resSlot))
			f.pinned = f.pinned.add(res[i]) // keep across the remaining loads
		}
		resSlot += rt.stackSlots()
	}
	for i := 0; i < rN; i++ {
		rt := resTypes[i]
		switch {
		case rt.isV128():
			f.fpinned = f.fpinned.remove(res[i])
			f.pushVReg(res[i])
		case rt.isFloat():
			f.fpinned = f.fpinned.remove(res[i])
			f.pushFReg(res[i], rt)
		default:
			f.pinned = f.pinned.remove(res[i])
			f.pushReg(res[i], rt)
		}
	}
}
