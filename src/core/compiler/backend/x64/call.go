package x64

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// regABIEnabled turns on the register-based internal-call ABI (default on;
// WAGO_X64_NOREGABI=1 forces the wrapper ABI everywhere, for A/B measurement).
var regABIEnabled = os.Getenv("WAGO_X64_NOREGABI") != "1"

// noStackFence skips the per-entry stack-overflow fence check (A/B measurement).
var noStackFence = os.Getenv("WAGO_X64_NOFENCE") == "1"

// noStackReg disables the WARP STACK_REG lazy local model (reverts to spill-all/
// reload-all around calls, no branch reconcile) — A/B measurement.
var noStackReg = os.Getenv("WAGO_X64_NOSTACKREG") == "1"

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

func isIntValType(t wasm.ValType) bool {
	return wasm.EqualValType(t, wasm.I32) || wasm.EqualValType(t, wasm.I64)
}

// sigFitsRegABI reports whether a signature can use the register ABI: integer-
// only, at most len(intArgRegs) params and one result.
func sigFitsRegABI(ft *wasm.CompType) bool {
	if len(ft.Params) > len(intArgRegs) || len(ft.Results) > 1 {
		return false
	}
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
	return f.callInternal(int(idx)-imported, ft)
}

// callHost lowers a call to an imported (host) function. Since native wasm code
// cannot call back into Go without cgo, the call is LOGGED to an in-memory buffer
// (at [linMem-offCustomCtx]) and replayed on the Go stack after the wasm function
// returns. This matches the runtime's log format (backend/amd64). Fire-and-forget:
// a single i32 argument, no result.
func (f *fn) callHost(importIdx int, ft *wasm.CompType) error {
	if len(ft.Results) != 0 {
		return fmt.Errorf("x64: host import with results not supported (func %d)", importIdx)
	}
	p := len(ft.Params)
	f.flush()
	d := f.depth()
	if p > 0 {
		f.a.Load32(RAX, RBP, f.spillOff(d-p)) // first param
	} else {
		f.a.XorSelf32(RAX)
	}
	f.a.Load64(RDI, RBX, -offCustomCtx) // RDI = host-call log
	f.a.Load32(RCX, RDI, 0)             // count
	f.a.LeaScaled(RDX, RDI, RCX, 3, 8)  // entry = log + count*8 + 8
	f.a.StoreImm32Mem(RDX, 0, int32(importIdx))
	f.a.Store32(RDX, 4, RAX)
	f.a.AluRI(0, RCX, 1, false) // count++ (digit 0 = add)
	f.a.Store32(RDI, 0, RCX)
	f.setDepth(d - p)
	return nil
}

// Basedata scratch offsets (negative from the linMem base), matching the runtime
// and backend/amd64: a scratch cell to carry the indirect code pointer across the
// flush, and the indirect-call table descriptor pointer.
const (
	offCustomCtx   = 40 // host-call log pointer
	offSpillRegion = 48 // 8B scratch
	offTablePtr    = 80 // table descriptor pointer
)

// callInternal lowers a direct call to another local function. Integer-only
// callees use the fast register ABI (args/result in registers); others go
// through the wrapper (rsp-buffer) ABI.
func (f *fn) callInternal(localIdx int, ft *wasm.CompType) error {
	if regABIEnabled && sigFitsRegABI(ft) {
		f.emitRegisterCall(localIdx, ft)
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
func (f *fn) emitRegisterCall(localIdx int, ft *wasm.CompType) {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()

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
			f.a.Load64(da.target, RBP, f.spillOff(da.root.st.slot))
		case stLocalRef:
			f.a.Load64(da.target, RBP, f.localOff(da.root.st.idx))
		}
	}

	// Consume the args; the operand model is now the k below-operands in slots.
	f.setDepth(d - p)

	// Store dirty pinned locals; the callee clobbers their registers (lazy reload
	// on the next read — WARP's STACK_REG model).
	f.spillLocalsForCall()
	f.a.MovReg64(RDI, RBX)    // linMem
	f.a.Load64(RSI, RBP, -24) // trap
	site := f.a.CallRel32()
	f.relocs = append(f.relocs, callReloc{at: site, target: localIdx, internal: true})

	// Capture the result out of RAX before RAX is reused as scratch.
	resReg := regNone
	if rN == 1 {
		resReg = f.allocReg(maskOf(RAX))
		f.a.MovReg64(resReg, RAX)
		f.pinned = f.pinned.add(resReg)
	}
	f.reloadLocalsForCall() // non-STACK_REG model only
	// No post-call trap check: a callee trap jumps straight back to enterNative
	// via emitTrap's handler-jump, so control never returns here with *trap set.

	if rN == 1 {
		f.pinned = f.pinned.remove(resReg)
		f.pushReg(resReg, mtOf(ft.Results[0]))
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
	inb := f.a.JccPlaceholder(condB)
	f.emitTrap(trapIndirectOOB)
	f.a.PatchRel32(inb, f.a.Len())

	// 64-bit pointer arithmetic: slot address = tbl + idx*16.
	f.a.ShiftImm(4, idxReg, 4, true)   // idx *= 16
	f.a.AluRR(0x01, idxReg, tbl, true) // idx += tbl
	f.pinned = f.pinned.remove(tbl)
	f.release(tbl)

	tid := f.allocReg(0)
	f.a.Load32(tid, idxReg, 16) // entry type id
	f.a.AluRI(cmpDigit, tid, canon, false)
	f.release(tid)
	okSig := f.a.JccPlaceholder(condE)
	f.emitTrap(trapIndirectSig)
	f.a.PatchRel32(okSig, f.a.Len())

	code := f.allocReg(0)
	f.a.Load64(code, idxReg, 8) // entry code ptr
	f.pinned = f.pinned.remove(idxReg)
	f.release(idxReg)
	f.a.TestSelf(code, true)
	okNull := f.a.JccPlaceholder(condNE)
	f.emitTrap(trapIndirectOOB)
	f.a.PatchRel32(okNull, f.a.Len())

	// Stash the code ptr in linMem scratch so it survives the wrapper-call flush.
	f.a.Store64(RBX, -int32(offSpillRegion), code)
	f.release(code)

	f.emitWrapperCall(ft, func() {
		f.a.Load64(RAX, RSI, -int32(offSpillRegion)) // RSI = linMem in the call setup
		f.a.CallReg(RAX)
	})
	return nil
}

// emitWrapperCall marshals arguments into a native-stack buffer, sets up the
// wrapper ABI registers (RDI=args, RCX=results, RSI=linMem, RDX=trap), runs
// emitCall, propagates a callee trap, and loads the results back onto the stack.
func (f *fn) emitWrapperCall(ft *wasm.CompType, emitCall func()) {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()
	f.flush() // all operands to canonical slots; args are slots [d-p, d)

	buf := align16((p + rN) * 8)
	if buf > 0 {
		f.a.SubRsp(int32(buf))
	}
	for i := 0; i < p; i++ {
		f.a.Load64(RAX, RBP, f.spillOff(d-p+i))
		f.a.StoreRsp64(int32(i*8), RAX)
	}
	f.a.MovFromRsp(RDI)         // args = rsp
	f.a.LeaRsp(RCX, int32(p*8)) // results = rsp + p*8
	f.a.MovReg64(RSI, RBX)      // linMem (kept in RBX)
	f.a.Load64(RDX, RBP, -24)   // trap ptr
	// Store dirty pinned locals; the callee clobbers their registers (lazy reload
	// on the next read — WARP's STACK_REG model).
	f.spillLocalsForCall()
	emitCall()

	// No post-call trap check: a callee trap unwinds the whole native call tree
	// in one jump (emitTrap's handler-jump back to enterNative), so control never
	// returns here with *trap set.
	f.reloadLocalsForCall() // non-STACK_REG model only

	// Pop the args, load results out of the buffer into fresh registers, restore rsp.
	f.setDepth(d - p)
	res := make([]Reg, rN)
	isFP := make([]bool, rN)
	for i := 0; i < rN; i++ {
		rt := mtOf(ft.Results[i])
		if rt.isFloat() {
			// Load the 8-byte result word into a GP scratch, then into an XMM reg.
			tmp := f.allocReg(0)
			f.a.LoadRsp64(tmp, int32(p*8+i*8))
			res[i] = f.allocFReg(0)
			f.a.MovGprToXmm(res[i], tmp, true)
			f.release(tmp)
			f.fpinned = f.fpinned.add(res[i])
			isFP[i] = true
		} else {
			res[i] = f.allocReg(0)
			f.a.LoadRsp64(res[i], int32(p*8+i*8))
			f.pinned = f.pinned.add(res[i]) // keep across the remaining loads
		}
	}
	if buf > 0 {
		f.a.AddRsp(int32(buf))
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
