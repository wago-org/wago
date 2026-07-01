package x64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Function calls. Internal (wasm→wasm) calls use wago's WasmWrapper ABI: the
// arguments and result slots live in a native-stack buffer at RSP; the callee is
// entered with RDI=args, RSI=linMem, RDX=trap, RCX=results — exactly what the
// prologue expects. Ported from WARP's call lowering but retargeted to wago's
// ABI/runtime (host imports adapt to wago's re-entry model, not WARP's
// synchronous native calls — the no-cgo constraint).

// callReloc records a CallRel32 site whose rel32 must be patched to point at the
// target local function's entry once the module is laid out.
type callReloc struct {
	at     int // byte offset of the rel32 field within this function's code
	target int // target local-function index (into m.Code)
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
		return fmt.Errorf("x64: host import calls not yet supported (func %d)", idx)
	}
	return f.callInternal(int(idx)-imported, ft)
}

// Basedata scratch offsets (negative from the linMem base), matching the runtime
// and backend/amd64: a scratch cell to carry the indirect code pointer across the
// flush, and the indirect-call table descriptor pointer.
const (
	offSpillRegion = 48 // 8B scratch
	offTablePtr    = 80 // table descriptor pointer
)

// callInternal lowers a direct call to another local function via the wrapper ABI.
func (f *fn) callInternal(localIdx int, ft *wasm.CompType) error {
	f.emitWrapperCall(ft, func() {
		site := f.a.CallRel32()
		f.relocs = append(f.relocs, callReloc{at: site, target: localIdx})
	})
	return nil
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
	emitCall()

	// Propagate a callee trap: if *trap != 0, unwind immediately.
	f.a.Load64(RAX, RBP, -24)
	f.a.Load32(RAX, RAX, 0)
	f.a.TestSelf(RAX, false)
	ok := f.a.JccPlaceholder(condE)
	if buf > 0 {
		f.a.AddRsp(int32(buf))
	}
	f.a.Leave()
	f.a.Ret()
	f.a.PatchRel32(ok, f.a.Len())

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
