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

// callInternal lowers a direct call to another local function via the wrapper ABI.
func (f *fn) callInternal(localIdx int, ft *wasm.CompType) error {
	p, rN := len(ft.Params), len(ft.Results)
	d := f.depth()
	f.flush() // all operands to canonical slots; args are slots [d-p, d)

	buf := align16((p + rN) * 8)
	if buf > 0 {
		f.a.SubRsp(int32(buf))
	}
	// Marshal args into the RSP buffer from their canonical slots.
	for i := 0; i < p; i++ {
		f.a.Load64(RAX, RBP, f.spillOff(d-p+i))
		f.a.StoreRsp64(int32(i*8), RAX)
	}
	f.a.MovFromRsp(RDI)         // args = rsp
	f.a.LeaRsp(RCX, int32(p*8)) // results = rsp + p*8
	f.a.MovReg64(RSI, RBX)      // linMem (kept in RBX)
	f.a.Load64(RDX, RBP, -24)   // trap ptr
	site := f.a.CallRel32()
	f.relocs = append(f.relocs, callReloc{at: site, target: localIdx})

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
	for i := 0; i < rN; i++ {
		res[i] = f.allocReg(0)
		f.a.LoadRsp64(res[i], int32(p*8+i*8))
		f.pinned = f.pinned.add(res[i]) // keep across the remaining loads
	}
	if buf > 0 {
		f.a.AddRsp(int32(buf))
	}
	for i := 0; i < rN; i++ {
		f.pinned = f.pinned.remove(res[i])
		f.pushReg(res[i], mtOf(ft.Results[i]))
	}
	return nil
}
