package amd64

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
// and float params are assigned to separate GP/XMM banks; a single result returns
// in RAX or XMM0. Multi-result register returns come in a later stage.
func sigFitsRegABI(ft *wasm.CompType) bool {
	if len(ft.Results) > 1 {
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
	if int(idx) < imported {
		return f.callHost(int(idx), ft)
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

// callHost lowers a call to an imported (host) function. Since native wasm code
// cannot call back into Go without cgo, the call is LOGGED to an in-memory buffer
// (at [linMem-offCustomCtx]) and replayed on the Go stack after the wasm function
// returns. This matches the runtime's log format (backend/railshot/amd64). Fire-and-forget:
// a single i32 argument, no result.
func (f *fn) callHost(importIdx int, ft *wasm.CompType) error {
	if len(ft.Results) != 0 {
		return fmt.Errorf("amd64: host import with results not supported (func %d)", importIdx)
	}
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

// Basedata scratch offsets (negative from the linMem base), matching the runtime
// and backend/railshot/amd64: a scratch cell to carry the indirect code pointer across the
// flush, and the indirect-call table descriptor pointer.
const (
	offCustomCtx   = 40 // host-call log pointer
	offSpillRegion = 48 // 8B scratch
	offTablePtr    = 80 // table descriptor pointer
)

// callInternal lowers a direct call to another local function. Integer-only
// callees use the fast register ABI (args/result in registers); others go
// through the wrapper (rsp-buffer) ABI.
func (f *fn) callInternal(localIdx int, ft *wasm.CompType, resHint int) error {
	if regABIEnabled && sigFitsRegABI(ft) {
		if sigIsIntOnly(ft) {
			f.emitRegisterCall(localIdx, ft, resHint)
		} else {
			f.emitMixedRegisterCall(localIdx, ft)
		}
		return nil
	}
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
	f.emitRegisterCallVia(ft, resHint, func() {
		site := f.a.CallRel32()
		f.relocs = append(f.relocs, callReloc{at: site, target: localIdx, internal: true})
	})
}

// emitRegisterCallVia is emitRegisterCall with a pluggable call emitter
// (direct rel32 or an indirect `call [mem]` for call_indirect).
func (f *fn) emitRegisterCallVia(ft *wasm.CompType, resHint int, emitCall func()) {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()
	f.storePinnedGlobals(false) // spill value-pinned globals to their cells before the call (scratch is free here)

	// Identify the p argument roots (top of stack), deepest first.
	argRoots := make([]*elem, p)
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
	var moves []regMove
	type deferredArg struct {
		target Reg
		root   *elem
	}
	var deferred []deferredArg
	for i := 0; i < p; i++ {
		root := argRoots[i]
		if root.isDeferred() || (root.kind == ekValue && (root.st.kind == stReg || root.st.kind == stLocalReg || root.st.kind == stMemRef)) {
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

	// Consume the args; the operand model is now the k below-operands in slots.
	f.setDepth(d - p)

	// No environment passing: RBX (linMem) is a whole-module invariant and the
	// trap cell pointer lives in basedata — the callee inherits both (WARP model).
	emitCall()

	// Capture the result out of RAX before RAX is reused as scratch.
	resReg := regNone
	if rN == 1 && resHint < 0 {
		resReg = f.allocReg(maskOf(RAX))
		f.a.MovReg64(resReg, RAX)
		f.pinned = f.pinned.add(resReg)
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
}

// emitMixedRegisterCall uses the register ABI for signatures containing floats.
// It deliberately keeps the current canonical-slot argument staging instead of a
// full mixed-bank copy resolver; integer-only calls stay on emitRegisterCall's
// hotter parallel-move path.
func (f *fn) emitMixedRegisterCall(localIdx int, ft *wasm.CompType) {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()

	f.flush()
	f.storePinnedGlobals(false) // spill value-pinned globals to their cells before the call
	// Store dirty pinned locals BEFORE the argument loads: a pinned local may live
	// in an argument register (R9-R11) or in RDI/RSI (clobbered by the setup below).
	f.spillLocalsForCall()
	gp, fp := 0, 0
	for i, t := range ft.Params {
		slot := d - p + i
		mt := mtOf(t)
		if mt.isFloat() {
			f.a.FLoadDisp(fpArgRegs[fp], RSP, f.spillOff(slot), mt == mtF64)
			fp++
		} else {
			f.a.Load64(intArgRegs[gp], RSP, f.spillOff(slot))
			gp++
		}
	}
	f.setDepth(d - p)

	site := f.a.CallRel32()
	f.relocs = append(f.relocs, callReloc{at: site, target: localIdx, internal: true})
	f.reloadLocalsForCall() // non-STACK_REG model only
	f.derivePinnedGlobals() // reload value-pinned globals: the callee may have changed the shared cell

	if rN == 1 {
		rt := mtOf(ft.Results[0])
		if rt.isFloat() {
			f.pushFReg(0, rt) // XMM0
		} else {
			resReg := f.allocReg(maskOf(RAX))
			f.a.MovReg64(resReg, RAX)
			f.pushReg(resReg, rt)
		}
	}
}

// callIndirect lowers call_indirect: bounds-check the table index, verify the
// entry's canonical type id, reject a null entry, then call the entry's code
// pointer via the wrapper ABI. Table layout matches the runtime (16-byte slots;
// +8 code ptr, +16 type id) with the descriptor pointer at [linMem-offTablePtr].
func (f *fn) callIndirect(r *wasm.Reader) error {
	typeIdx, err := r.U32()
	if err != nil {
		return err
	}
	tableIdx, err := r.U32()
	if err != nil {
		return err
	}
	if tableIdx != 0 {
		return fmt.Errorf("call_indirect: multi-table unsupported: table %d", tableIdx)
	}
	ft, ok := f.m.TypeFunc(typeIdx)
	if !ok {
		return fmt.Errorf("call_indirect: bad type %d", typeIdx)
	}
	canon := int32(f.m.CanonicalTypeID(typeIdx))

	idxReg := f.materialize(f.popValue()) // table index (i32)
	f.pinned = f.pinned.add(idxReg)
	tbl := f.allocReg(0)
	f.a.Load64(tbl, RBX, -int32(offTablePtr)) // table descriptor
	f.pinned = f.pinned.add(tbl)

	ln := f.allocReg(0)
	f.a.Load32(ln, tbl, 0) // table length
	f.a.AluRR(0x39, idxReg, ln, false)
	f.release(ln)
	f.trapIf(condAE, trapIndirectOOB) // idx >= length → cold stub

	// 64-bit pointer arithmetic: slot address = tbl + idx*16.
	f.a.ShiftImm(4, idxReg, 4, true)   // idx *= 16
	f.a.AluRR(0x01, idxReg, tbl, true) // idx += tbl
	f.pinned = f.pinned.remove(tbl)
	f.release(tbl)

	tid := f.allocReg(0)
	f.a.Load32(tid, idxReg, 16) // entry type id
	f.a.AluRI(cmpDigit, tid, canon, false)
	f.release(tid)
	f.trapIf(condNE, trapIndirectSig)

	code := f.allocReg(0)
	f.a.Load64(code, idxReg, 8) // entry code ptr
	f.a.TestSelf(code, true)
	f.trapIf(condE, trapIndirectOOB) // null entry

	// Register-ABI fast path: the type-id check proved the callee's signature
	// exactly, so a register-ABI-compatible signature guarantees the callee has
	// an internal entry. Its offset delta sits in the table entry's pad word.
	fast := regABIEnabled && sigFitsRegABI(ft) && sigIsIntOnly(ft)
	if fast {
		d := f.allocReg(maskOf(idxReg, code))
		f.a.Load32(d, idxReg, 20)      // internal-entry delta
		f.a.AluRR(0x01, code, d, true) // code += delta → internal entry
		f.release(d)
	}
	f.pinned = f.pinned.remove(idxReg)
	f.release(idxReg)

	// Stash the code ptr in linMem scratch so it survives the call staging.
	f.a.Store64(RBX, -int32(offSpillRegion), code)
	f.release(code)

	if fast {
		f.emitRegisterCallVia(ft, -1, func() {
			f.a.CallMem(RBX, -int32(offSpillRegion)) // RBX = linMem throughout staging
		})
		return nil
	}
	f.emitWrapperCall(ft, func() {
		f.a.Load64(RAX, RSI, -int32(offSpillRegion)) // RSI = linMem in the call setup
		f.a.CallReg(RAX)
	})
	return nil
}

// emitWrapperCall sets up the wrapper ABI registers (RDI=args, RCX=results,
// RSI=linMem, RDX=trap), runs emitCall, and loads the results back onto the
// operand stack. Frameless: the wrapper argument and result buffers are the
// operand SPILL SLOTS themselves — after the flush, the p arguments already sit
// contiguously and in order at spill slots [d-p, d) (exactly the [RDI+8*i] layout
// the callee's prologue reads), and the rN results land in the free slots
// [d, d+rN) just above them. So there is no separate native-stack buffer and no
// transient SubRsp/AddRsp — RSP stays put for the whole call.
func (f *fn) emitWrapperCall(ft *wasm.CompType, emitCall func()) {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()
	f.flush()                   // all operands to canonical slots; args are slots [d-p, d)
	f.storePinnedGlobals(false) // spill value-pinned globals to their cells before the call
	f.storeModuleGlobals(RAX)   // wrapper callee's offset-0 prologue reloads from the cells

	// Reserve the result slots [d, d+rN) in the frame.
	if need := d + rN; need > f.maxSpill {
		f.maxSpill = need
	}
	argOff := f.spillOff(d) // p==0: unused, but a valid in-frame address
	if p > 0 {
		argOff = f.spillOff(d - p)
	}
	// Store dirty pinned locals BEFORE the call-setup writes below: a pinned
	// local may live in RDI/RSI (clobbered by the setup itself), not just in a
	// callee-clobbered register. Lazy reload on the next read — WARP's STACK_REG.
	f.spillLocalsForCall()
	f.a.LeaRsp(RDI, argOff)        // args = &slot[d-p]
	f.a.LeaRsp(RCX, f.spillOff(d)) // results = &slot[d]
	f.a.MovReg64(RSI, RBX)         // linMem (kept in RBX); trap ptr lives in basedata
	emitCall()

	// No post-call trap check: a callee trap unwinds the whole native call tree
	// in one jump (emitTrap's handler-jump back to enterNative), so control never
	// returns here with *trap set.
	f.reloadLocalsForCall() // non-STACK_REG model only
	f.derivePinnedGlobals() // reload value-pinned globals: the callee may have changed the shared cell

	// Pop the args; load results out of their slots [d, d+rN) into fresh registers.
	f.setDepth(d - p)
	res := make([]Reg, rN)
	isFP := make([]bool, rN)
	for i := 0; i < rN; i++ {
		rt := mtOf(ft.Results[i])
		if rt.isFloat() {
			// Load the 8-byte result word into a GP scratch, then into an XMM reg.
			tmp := f.allocReg(0)
			f.a.Load64(tmp, RSP, f.spillOff(d+i))
			res[i] = f.allocFReg(0)
			f.a.MovGprToXmm(res[i], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[i])
			isFP[i] = true
		} else {
			res[i] = f.allocReg(0)
			f.a.Load64(res[i], RSP, f.spillOff(d+i))
			f.pinned = f.pinned.add(res[i]) // keep across the remaining loads
		}
	}
	for i := 0; i < rN; i++ {
		if isFP[i] {
			f.fpinned = f.fpinned.remove(res[i])
			f.pushFReg(res[i], mtOf(ft.Results[i]))
		} else {
			f.pinned = f.pinned.remove(res[i])
			f.pushReg(res[i], mtOf(ft.Results[i]))
		}
	}
}
