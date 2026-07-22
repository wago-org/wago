//go:build arm64

package arm64

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime"
)

// regABIEnabled turns on the register-based internal-call ABI (default on;
// WAGO_ARM64_NOREGABI=1 forces the wrapper ABI everywhere, for A/B measurement).
var regABIEnabled = os.Getenv("WAGO_ARM64_NOREGABI") != "1"

// noStackFence skips the per-entry stack-overflow fence check (A/B measurement).
var noStackFence = os.Getenv("WAGO_ARM64_NOFENCE") == "1"

// immutableLocalPolyFastPath gates the polymorphic immutable-local call_indirect
// fast path (see callIndirect). It is OFF: that path does not preserve the stack
// fence for deeply self-recursive targets and faults instead of trapping. Set
// WAGO_ARM64_IMMUTABLE_POLY_FASTPATH=1 only for A/B measurement of the lost
// specialization; it reintroduces the spec call_indirect runaway crash.
var immutableLocalPolyFastPath = os.Getenv("WAGO_ARM64_IMMUTABLE_POLY_FASTPATH") == "1"

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
	at       int  // byte offset of the BL instruction within this function's code
	target   int  // target local-function index (into m.Code)
	internal bool // target the callee's register-ABI internal entry (else offset 0)
}

// intArgRegs is the integer argument/result register order for the internal
// register-call ABI (our own convention, not the C ABI). X0/X1 carry args/linMem;
// X19-X23 hold pinned locals; linMemReg holds linMem. The single result returns in X0
// (AAPCS64 return register, also arg 0).
var intArgRegs = []Reg{X0, X1, X2, X3, X4, X5, X6, X7}
var fpArgRegs = []Reg{0, 1, 2, 3, 4, 5, 6, 7} // V0..V7; single float result returns in V0.

const internalEntryHomeTag uint64 = 1 << 63

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
			if _, ok := f.inlineBase[int(idx)]; ok && !(t.inlineInLoopIsRegressive() && f.inCallSiteLoop()) {
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
	f.flush()
	d := f.depth()
	if p > 0 {
		f.ld32(X0, SP, f.spillOff(d-p)) // first param
	} else {
		f.a.MovImm64(X0, 0) // zero (no flag side effect on arm64)
	}
	// Scratch entirely in X0/X9/X10/X11: a host call clobbers no wasm register
	// state, so pinned locals (which live in X19-X23) stay untouched.
	f.ld64(X11, linMemReg, -int32(offCustomCtx)) // X11 = host-call log
	f.ld32(X9, X11, 0)                           // count
	f.a.AddShifted(X10, X11, X9, 3, false)       // entry = log + count*8
	f.leaDisp(X10, X10, 8, true)                 // + 8 header
	f.a.MovImm64(X16, uint64(uint32(importIdx)))
	f.st32(X10, 0, X16)
	f.st32(X10, 4, X0)
	f.a.AddImm32(X9, X9, 1) // count++
	f.st32(X11, 0, X9)
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
// synchronous re-entry protocol (see src/core/runtime/hostcall_arm64.go). The p
// params are marshaled into the off-heap control frame (at [linMem-offCustomCtx]);
// `blr [ctrl+hcTrampoline]` runs the shared hostCallStub, which saves the wasm
// register state and unwinds to Go; Go runs the host function, writes the
// results, and resumes here; the rN results are read out of the control frame
// onto the operand stack.
//
// hostCallStub saves and resumeNative restores the callee-saved registers
// (X19..linMemReg, low 64 bits of V8..V15), so pinned locals and linMem survive the
// round trip and need no spilling — unlike a wasm→wasm call, whose callee reuses
// those registers. Value-pinned and module-pinned globals ARE synced around the
// call: the host may read or write the instance's globals through their cells.
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
	f.storeModuleGlobals(X9)

	// Marshal params into the control frame as wrapper-ABI slots. A v128 occupies
	// two adjacent little-endian uint64 slots, exactly like Invoke and cross-
	// instance wrapper calls.
	f.ld64(X11, linMemReg, -int32(offCustomCtx)) // X11 = control frame
	argSlot, ctrlSlot := 0, 0
	if p > 0 {
		argSlot = slotOf[d-p]
	}
	for i := 0; i < p; i++ {
		mt := mtOf(ft.Params[i])
		if mt.isV128() {
			x := f.allocFReg(0)
			f.a.VMovdquLoadDisp(x, SP, f.spillOff(argSlot))
			f.a.VMovdquStoreDisp(X11, hcArgs+int32(ctrlSlot)*8, x)
			f.releaseF(x)
		} else if mt.is64() {
			f.ld64(X9, SP, f.spillOff(argSlot))
			f.st64(X11, hcArgs+int32(ctrlSlot)*8, X9)
		} else {
			f.ld32(X9, SP, f.spillOff(argSlot)) // zero-extends into X9
			f.st64(X11, hcArgs+int32(ctrlSlot)*8, X9)
		}
		argSlot += mt.stackSlots()
		ctrlSlot += mt.stackSlots()
	}
	f.a.MovImm64(X16, uint64(uint32(importIdx)))
	f.st32(X11, hcImportIdx, X16)
	// hcNArgs packs param slots (low 16) and result slots (high 16) so the Go
	// re-entry loop copies back only the real result count. Both are <= 16.
	f.a.MovImm64(X16, uint64(uint32(paramSlots)|uint32(resultSlots)<<16))
	f.st32(X11, hcNArgs, X16)

	// Park at the host call. Like the wrapper path, no post-call trap check: a
	// trap unwinds the whole native tree in one jump (it never returns here).
	f.ld64(X16, X11, hcTrampoline)
	f.a.Blr(X16)

	f.deriveModuleGlobals() // the host may have written global cells
	f.derivePinnedGlobals()
	f.setDepthTypes(belowTypes)

	// Read results out of the control frame onto the operand stack, honoring
	// slot-width result layout for v128 and mixed scalar/vector signatures.
	f.ld64(X11, linMemReg, -int32(offCustomCtx)) // reload ctrl (clobbered by the round trip)
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
			f.a.VMovdquLoadDisp(res[j], X11, hcResults+int32(ctrlSlot)*8)
			f.fpinned = f.fpinned.add(res[j]) // keep across the remaining loads
		case rt.isFloat():
			tmp := f.allocReg(0)
			f.ld64(tmp, X11, hcResults+int32(ctrlSlot)*8)
			res[j] = f.allocFReg(0)
			f.a.FmovFromGpr(res[j], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[j])
		default:
			res[j] = f.allocReg(0)
			f.ld64(res[j], X11, hcResults+int32(ctrlSlot)*8)
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
// (placed in a table as a funcref). It is entered with the wrapper ABI (X1 =
// linMem, X0 = args buffer), appends (importIdx, first-arg-i32) to the host-call
// log at [linMem-offCustomCtx] exactly like callHost, and returns void, so the
// normal post-invoke replay runs the host function. Emitted per host funcref into
// a per-instance mapping; the same code is instance-independent (it reads the log
// pointer from X1 at run time).
func HostIndirectThunk(importIdx uint32) []byte {
	a := &a64.Asm{}
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
	a := &a64.Asm{}
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
	for i := 0; i < paramSlots; i++ {
		a.Load64(X9, X0, uint32(i*8))
		a.Store64(X9, X10, uint32(hcArgs+i*8))
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
	for i := 0; i < resultSlots; i++ {
		a.Load64(X9, X10, uint32(hcResults+i*8))
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
	offCustomCtx   = 40 // host-call log pointer / sync host-call control frame
	offSpillRegion = 48 // 8B scratch
	offStackFence  = 72 // low stack bound for the fence check
	offTablePtr    = 80 // table descriptor pointer
	// offTrapHandlerPtr (32), offTrapStackReentry (24), and offTrapCellPtr
	// (== abi.TrapCellPtrOffset) are defined in memory.go.
)

// Control-frame field offsets for the synchronous host-call protocol. A
// returning host import needs no async log, so it reuses the customCtx slot
// (offCustomCtx) for its control frame. These MUST match
// src/core/runtime/hostcall_arm64.go (hcSavedSP..hcResults, maxHostArity=16).
const (
	hcTrampoline     = 176 // u64: hostCallStub address (published per-instance by CallWithHost)
	hcImportIdx      = 184 // u32: native -> Go
	hcNArgs          = 188 // u32: low 16 bits = param slots, high 16 bits = result slots
	hcArgs           = 192 // [16]u64: native -> Go
	hcResults        = 320 // [16]u64: Go -> native (== hcArgs + 16*8)
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
		f.ld64(X9, src, int32(i*8))
		f.st64(dst, -off, X9)
	}
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
	f.storeModuleGlobals(X9) // cross-instance boundary: shared globals must be cell-coherent

	// Args/results buffers as absolute pointers (survive the STP pushes below —
	// they hold absolute addresses, unaffected by the SP adjustment).
	f.a.LeaSP(X0, argOff)                 // args = &first arg slot
	f.a.LeaSP(X3, f.spillOff(resultSlot)) // results = &slot-width top

	// Preserve the caller's module-invariant registers (linMemReg=linMem, X27=memSize,
	// X23-X25=module globals) plus one pad (3 STP pairs = 48 bytes → SP stays
	// 16-aligned for the callee's offset-0 entry, which STP-pushes its own frame
	// record). BL writes LR (no stack push), so no return-address bias is needed.
	f.a.StpPre(linMemReg, X24, SP, -16)
	f.a.StpPre(X25, X23, SP, -16)
	f.a.StpPre(X27, X9, SP, -16) // X9 = alignment pad

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

	if b.Dynamic {
		f.a.LdpPost(X16, X17, SP, 16) // caller context + saved target
	}
	f.a.LdpPost(X27, X9, SP, 16) // X9 = alignment pad
	f.a.LdpPost(X25, X23, SP, 16)
	f.a.LdpPost(linMemReg, X24, SP, 16)
	if b.Dynamic {
		f.copyInstanceContext(linMemReg, X16)
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
			f.a.VMovdquLoadDisp(res[i], SP, f.spillOff(resSlot))
			f.fpinned = f.fpinned.add(res[i])
		case rt.isFloat():
			tmp := f.allocReg(0)
			f.ld64(tmp, SP, f.spillOff(resSlot))
			res[i] = f.allocFReg(0)
			f.a.FmovFromGpr(res[i], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[i])
		default:
			res[i] = f.allocReg(0)
			f.ld64(res[i], SP, f.spillOff(resSlot))
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
// through the wrapper (sp-buffer) ABI.
func (f *fn) callInternal(localIdx int, ft *wasm.CompType, resHint int) error {
	if regABIEnabled && sigFitsRegABI(ft) && sigIsIntOnly(ft) {
		f.stats.call(callKindRegisterABI)
		f.emitRegisterCall(localIdx, ft, resHint, f.directCalleePreservesPins(localIdx))
		return nil
	}
	f.stats.call(callKindWrapper)
	f.emitWrapperCall(ft, func() {
		site := f.a.Bl()
		f.relocs = append(f.relocs, callReloc{at: site, target: localIdx})
	})
	return nil
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
func (f *fn) emitRegisterCallVia(ft *wasm.CompType, resHint int, preservesPins bool, localIdx int, indirect Reg) {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()
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
	resolveRegMoves(moves,
		func(dst, src Reg) { f.a.MovReg64(dst, src) },
		func(x, y Reg) {
			f.a.MovReg64(X16, x)
			f.a.MovReg64(x, y)
			f.a.MovReg64(y, X16)
		})
	f.tmpMoves = moves[:0]
	for _, da := range deferred {
		switch da.root.st.kind {
		case stConst:
			f.loadConst(da.target, da.root.st)
		case stSlot:
			f.ld64(da.target, SP, f.spillOff(da.root.st.slot))
		case stLocalRef:
			f.ld64(da.target, SP, f.localOff(da.root.st.idx))
		}
	}
	f.tmpDeferred = deferred[:0]

	// Consume the args; the operand model is now the k below-operands in slots.
	f.setDepth(d - p)

	// No environment passing: linMemReg (linMem) is a whole-module invariant and the
	// trap cell pointer lives in basedata — the callee inherits both (WARP model).
	if localIdx >= 0 {
		site := f.a.Bl()
		f.relocs = append(f.relocs, callReloc{at: site, target: localIdx, internal: true})
	} else {
		f.a.Blr(indirect)
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
}

// directCalleePreservesPins returns the module-precomputed leaf classification
// for one direct target. This is compile-time only; execution stays a plain BL.
func (f *fn) directCalleePreservesPins(localIdx int) bool {
	if localIdx < 0 || localIdx >= len(f.calleePreservesPins) {
		return false
	}
	return f.calleePreservesPins[localIdx]
}

// emitMixedRegisterCall uses the register ABI for signatures containing floats.
// GP and FP arguments are staged independently as parallel moves, so values that
// are already resident in registers do not round-trip through canonical slots.
func (f *fn) emitMixedRegisterCall(localIdx int, ft *wasm.CompType) {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()

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
			cur = baseOfValentBlock(cur).prev
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
			if root.isDeferred() || (root.kind == ekValue && (root.st.kind == stReg || root.st.kind == stLocalReg || root.st.kind == stGlobReg || root.st.kind == stMemRef)) {
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
			if root.isDeferred() || (root.kind == ekValue && (root.st.kind == stReg || root.st.kind == stLocalReg || root.st.kind == stGlobReg || root.st.kind == stMemRef)) {
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
	resolveRegMoves(gpMoves,
		func(dst, src Reg) { f.a.MovReg64(dst, src) },
		func(x, y Reg) {
			f.a.MovReg64(X16, x)
			f.a.MovReg64(x, y)
			f.a.MovReg64(y, X16)
		})
	for _, m := range fpMoves {
		f.fpinned = f.fpinned.remove(m.src)
	}
	fpSwapSlot := -1
	resolveRegMoves(fpMoves,
		func(dst, src Reg) { f.a.FmovReg(dst, src, true) },
		func(x, y Reg) {
			if fpSwapSlot < 0 {
				fpSwapSlot = f.allocSpillSlot()
			}
			off := f.spillOff(fpSwapSlot)
			f.fst(SP, off, x, true)
			f.a.FmovReg(x, y, true)
			f.fld(y, SP, off, true)
		})
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
				f.fld(da.target, SP, f.spillOff(da.root.st.slot), da.root.st.typ == mtF64)
			case stLocalRef:
				f.fld(da.target, SP, f.localOff(da.root.st.idx), da.root.st.typ == mtF64)
			}
			continue
		}
		switch da.root.st.kind {
		case stConst:
			f.loadConst(da.target, da.root.st)
		case stSlot:
			f.ld64(da.target, SP, f.spillOff(da.root.st.slot))
		case stLocalRef:
			f.ld64(da.target, SP, f.localOff(da.root.st.idx))
		}
	}
	f.setDepth(d - p)

	site := f.a.Bl()
	f.relocs = append(f.relocs, callReloc{at: site, target: localIdx, internal: true})
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
}

// callIndirect lowers call_indirect: bounds-check the table index, verify the
// entry's canonical type id, reject a null entry, then call the entry's code
// pointer via the wrapper ABI. Table layout matches the runtime (16-byte slots;
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
	f.ld32(ln, tbl, 0) // table length
	f.cmpRR(idxReg, ln, false)
	f.release(ln)
	f.trapIf(condAE, trapIndirectOOB) // idx >= length → cold stub

	// 64-bit pointer arithmetic: entry address = tbl + idx*32 (TableEntryBytes).
	f.a.LslImm(idxReg, idxReg, 5, true) // idx *= 32
	f.a.Add64(idxReg, idxReg, tbl)      // idx += tbl
	f.pinned = f.pinned.remove(tbl)
	f.release(tbl)

	// Entry fields (folding the 8-byte descriptor header): +8 code, +16 sig id,
	// +24 home linMem. Check null (uninitialized element) BEFORE the signature so a
	// zero-initialized entry traps as an empty slot, not a type mismatch.
	code := f.allocReg(0)
	f.ld64(code, idxReg, 8) // entry code ptr (offset-0 entry)
	f.cmpImm(code, 0, true)
	f.trapIf(condE, trapIndirectOOB) // null entry

	if tableIdx == 0 && f.immutableTableTyped && f.immutableTableType == uint32(canon) {
		f.stats.peep("immutable-table-type-check-elide")
	} else {
		tid := f.allocReg(maskOf(code))
		f.ld32(tid, idxReg, 16) // entry type id
		if fitsAddSubImm12(int64(canon)) {
			f.cmpImmS(tid, int64(canon), false)
		} else {
			want := f.allocReg(maskOf(code).add(tid))
			f.a.MovImm32(want, canon)
			f.cmpRR(tid, want, false)
			f.release(want)
		}
		f.release(tid)
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
		f.emitRegisterCall(f.monomorphicTarget, ft, -1, f.directCalleePreservesPins(f.monomorphicTarget))
		return nil
	}
	// NOTE: the polymorphic immutable-local fast path (register-ABI Blr of the
	// entry's runtime code pointer) is disabled — it is incorrect for the stack
	// fence. Under the register pressure of a large module it can enter a deeply
	// self-recursive target (spec call_indirect.wast $runaway / $mutual-runaway)
	// without the per-frame fence check tripping, so unbounded recursion faults
	// the foreign stack (Go "split stack overflow") instead of trapping "call
	// stack exhausted". The monomorphic fast path above (a direct call to the
	// proven internal entry, which carries the fence) is unaffected; polymorphic
	// entries fall through to emitIndirectCallHomeAware below, which enters the
	// callee's offset-0 entry with the correct per-execution control words
	// (including the stack fence). Verified: spec1 passes and spec2/3 no longer
	// crash. Re-enable only with an entry pointer that preserves the fence.
	if immutableLocalPolyFastPath && tableIdx == 0 && f.immutableLocalTable && sigFitsRegABI(ft) && sigIsIntOnly(ft) {
		f.pinned = f.pinned.remove(idxReg).add(code)
		f.release(idxReg)
		f.stats.peep("immutable-local-call-indirect")
		f.emitRegisterCallVia(ft, -1, false, -1, code)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		return nil
	}

	home := f.allocReg(maskOf(idxReg, code))
	f.ld64(home, idxReg, 24) // entry home linMem base
	canonical := f.allocReg(maskOf(idxReg, code, home))
	f.ld64(canonical, idxReg, 32) // canonical descriptor pointer
	f.cmpImm(canonical, 0, true)
	f.trapIf(condE, trapIndirectOOB)
	targetContext := f.allocReg(maskOf(idxReg, code, home, canonical))
	f.ld64(targetContext, canonical, runtime.FuncRefContextOffset)
	f.cmpImm(targetContext, 0, true)
	f.trapIf(condE, trapIndirectOOB)
	f.release(canonical)
	f.pinned = f.pinned.remove(idxReg)
	f.release(idxReg)
	if sigFitsRegABI(ft) && sigIsIntOnly(ft) {
		// Flush once, then emit both guarded paths from the same canonical stack
		// state. The compiler state for locals is restored before producing the
		// wrapper path; at run time only one branch executes.
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
		f.a.AndImm64(tag, home, internalEntryHomeTag)
		f.cmpImm(tag, 0, true)
		f.release(tag)
		wrapper := f.a.Bcond(condE)
		f.pinned = f.pinned.remove(home).remove(targetContext)
		f.release(targetContext)
		f.emitRegisterCallVia(ft, -1, false, -1, code)
		done := f.a.Branch()
		f.a.PatchBranch19(wrapper, f.a.Len())
		f.locals = savedLocals
		f.setDepthTypes(types)
		f.st64(linMemReg, -int32(offSpillRegion), code)
		f.pinned = f.pinned.remove(code)
		f.release(code)
		f.a.AndImm64(home, home, ^internalEntryHomeTag)
		f.emitIndirectCallHomeAware(ft, home, targetContext)
		f.a.PatchBranch26(done, f.a.Len())
		return nil
	}

	// Stash the code ptr in linMem scratch so it survives the call staging.
	f.st64(linMemReg, -int32(offSpillRegion), code)
	f.release(code)

	f.emitIndirectCallHomeAware(ft, home, targetContext)
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
	// and the cross-instance path's SP changes.
	homeSlot := resultSlot + resultSlots
	targetContextSlot := homeSlot + 1
	if need := targetContextSlot + 1; need > f.maxSpill {
		f.maxSpill = need
	}
	f.st64(SP, f.spillOff(homeSlot), homeReg)
	f.st64(SP, f.spillOff(targetContextSlot), targetContextReg)
	f.release(homeReg)
	f.release(targetContextReg)

	f.flush()                        // args → canonical slot-width slots
	f.storePinnedGlobals(false)      // value-pinned globals → cells
	f.storeModuleGlobals(X9)         // same-instance callee's offset-0 prologue reloads from cells
	argOff := f.spillOff(resultSlot) // p==0: unused, but a valid in-frame address
	if p > 0 {
		argOff = f.spillOff(slotOf[d-p])
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
	// Same instance: X1 = caller linMem, call the entry directly.
	f.a.MovReg64(X1, linMemReg)
	f.ld64(X16, linMemReg, -int32(offSpillRegion))
	f.a.Blr(X16)
	jdone := f.a.Branch()
	// Cross-instance: preserve the caller's invariants (+ one alignment pad), copy
	// the control words caller→callee, enter with X1 = callee linMem, then restore.
	f.a.PatchBranch19(jne, f.a.Len()) // the false edge is a B.cond (imm19)
	f.a.StpPre(linMemReg, X24, SP, -16)
	f.a.StpPre(X25, X23, SP, -16)
	f.a.StpPre(X27, X9, SP, -16) // X9 = alignment pad
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
	f.ld64(X16, linMemReg, -int32(offSpillRegion)) // linMemReg unchanged by the pushes
	f.a.Blr(X16)
	f.a.LdpPost(X13, X12, SP, 16)
	f.a.LdpPost(X27, X9, SP, 16)
	f.a.LdpPost(X25, X23, SP, 16)
	f.a.LdpPost(linMemReg, X24, SP, 16)
	f.copyInstanceContext(linMemReg, X13)
	f.deriveModuleGlobals()             // cross-instance callee may have written shared global cells
	f.a.PatchBranch26(jdone, f.a.Len()) // fr.jdone is an unconditional B (imm26)

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
			f.a.VMovdquLoadDisp(res[i], SP, f.spillOff(resSlot))
			f.fpinned = f.fpinned.add(res[i])
		case rt.isFloat():
			tmp := f.allocReg(0)
			f.ld64(tmp, SP, f.spillOff(resSlot))
			res[i] = f.allocFReg(0)
			f.a.FmovFromGpr(res[i], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[i])
		default:
			res[i] = f.allocReg(0)
			f.ld64(res[i], SP, f.spillOff(resSlot))
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

// emitWrapperCall sets up the wrapper ABI registers (X0=args, X3=results,
// X1=linMem, X2=trap), runs emitCall, and loads the results back onto the
// operand stack. Frameless: the wrapper argument and result buffers are the
// operand SPILL SLOTS themselves — after the flush, the p arguments already sit
// contiguously and in order at their canonical spill slots (exactly the wrapper
// ABI layout the callee's prologue reads), and the results land in free slots
// just above the current operand slot top. So there is no separate native-stack
// buffer and no transient SubSP/AddSP — SP stays put for the whole call.
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
	f.storeModuleGlobals(X9)    // wrapper callee's offset-0 prologue reloads from the cells

	// Reserve result slots above the full slot-width operand area, including v128 args.
	if need := resultSlot + resultSlots; need > f.maxSpill {
		f.maxSpill = need
	}
	argOff := f.spillOff(resultSlot) // p==0: unused, but a valid in-frame address
	if p > 0 {
		argOff = f.spillOff(slotOf[d-p])
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
			f.a.VMovdquLoadDisp(res[i], SP, f.spillOff(resSlot))
			f.fpinned = f.fpinned.add(res[i]) // keep across the remaining loads
		case rt.isFloat():
			// Load the 8-byte result word into a GP scratch, then into a V reg.
			tmp := f.allocReg(0)
			f.ld64(tmp, SP, f.spillOff(resSlot))
			res[i] = f.allocFReg(0)
			f.a.FmovFromGpr(res[i], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[i]) // keep across the remaining loads
		default:
			res[i] = f.allocReg(0)
			f.ld64(res[i], SP, f.spillOff(resSlot))
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
