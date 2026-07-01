package x64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/backend/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// fn holds the per-function code-generation state — the port's equivalent of
// WARP's Compiler/backend working set. One is created per compiled function.
type fn struct {
	a  *amd64.Asm // the (reused) x86-64 encoder
	s  *stack     // the valent-block operand stack
	m  *wasm.Module
	ft *wasm.CompType // this function's signature

	nParams   int
	nLocals   int           // params + declared locals
	localType []machineType // per-local machine type

	// Register occupancy: regUser[r] is the value elem currently resident in
	// physical register r, or nil if r is free. Only allocatable GPRs are tracked.
	regUser [16]*elem
	// pinned[r] marks a register temporarily protected from spilling/allocation
	// (e.g. an operand being consumed by the current op).
	pinned regMask

	maxSpill int // high-water number of operand spill slots used
	subRspAt int // byte offset of the prologue's SubRsp imm32 (patched with frameSize)
}

// Frame layout (RBP-relative), matching wago's runtime ABI so the trampoline and
// param/result marshaling are unchanged:
//
//	[-8] spare · [-16] saved linMem · [-24] trap ptr · [-32] results ptr
//	locals at localOff(i) = -(40 + 8*i) · spill slots after locals.
func (f *fn) localOff(i int) int32 { return -int32(40 + 8*i) }
func (f *fn) spillOff(k int) int32 { return -int32(40 + 8*f.nLocals + 8*k) }
func (f *fn) frameSize() int {
	sz := 40 + 8*f.nLocals + 8*f.maxSpill
	return (sz + 15) &^ 15
}

// CompileModule compiles every local function into one executable blob with
// per-function entry offsets — the same shape backend/amd64 produces, so
// src/wago consumes it unchanged. Phase 0: straight-line integer functions.
func CompileModule(m *wasm.Module) (*amd64.CompiledModule, error) {
	var code []byte
	entry := make([]int, len(m.Code))
	for i := range m.Code {
		fnCode, err := compileFunc(m, i)
		if err != nil {
			return nil, fmt.Errorf("x64: function %d: %w", i, err)
		}
		// 16-byte align each function.
		if pad := (16 - len(code)%16) % 16; pad != 0 {
			code = append(code, make([]byte, pad)...)
		}
		entry[i] = len(code)
		code = append(code, fnCode...)
	}
	return &amd64.CompiledModule{Code: code, Entry: entry}, nil
}

func compileFunc(m *wasm.Module, funcIdx int) (code []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("x64: %v", r)
		}
	}()

	ft, ok := m.LocalFuncType(funcIdx)
	if !ok {
		return nil, fmt.Errorf("unknown function type")
	}
	c := &m.Code[funcIdx]
	nLocals, err := countLocals(ft.Params, c.Locals)
	if err != nil {
		return nil, err
	}

	f := &fn{a: &amd64.Asm{}, s: newStack(), m: m, ft: ft, nParams: len(ft.Params), nLocals: nLocals}
	f.localType = make([]machineType, nLocals)
	i := 0
	for _, p := range ft.Params {
		f.localType[i] = mtOf(p)
		i++
	}
	for _, run := range c.Locals.Runs {
		for k := 0; k < int(run.Count); k++ {
			f.localType[i] = mtOf(run.Type)
			i++
		}
	}

	f.prologue()
	if err := f.body(c.BodyBytes); err != nil {
		return nil, err
	}
	f.epilogue()
	f.a.PatchU32(f.subRspAt, uint32(f.frameSize()))
	return f.a.B, nil
}

// prologue: standard frame, pin linMem in RBX (moved from RSI per WARP's
// convention), stash trap/results, copy register-passed params to their slots,
// zero declared locals.
func (f *fn) prologue() {
	a := f.a
	a.Prologue()              // push rbp; mov rbp,rsp
	f.subRspAt = len(a.B) + 3 // SubRsp opcode is 3 bytes (48 81 EC), then imm32
	a.SubRsp(0)               // frame; imm32 patched after body
	a.MovReg64(RBX, RSI)      // linMem → RBX (pinned for the whole function)
	a.Store64(RBP, -16, RSI)
	a.Store64(RBP, -24, RDX) // trap ptr
	a.Store64(RBP, -32, RCX) // results ptr
	for i := 0; i < f.nParams; i++ {
		a.Load64(RAX, RDI, int32(8*i))
		a.Store64(RBP, f.localOff(i), RAX)
	}
	if f.nLocals > f.nParams {
		a.XorSelf32(RAX)
		for i := f.nParams; i < f.nLocals; i++ {
			a.Store64(RBP, f.localOff(i), RAX)
		}
	}
}

// epilogue: write results to the results buffer, clear the trap slot, return.
func (f *fn) epilogue() {
	a := f.a
	// The function's results are the top len(results) valent blocks (a deferred
	// op keeps its operands physically below it, so results are logical blocks,
	// not physical positions). Collect their roots top-down, materialize each into
	// a register (pinned so the results-pointer load can't clobber it), then write
	// them to the results buffer in order.
	res := f.ft.Results
	n := len(res)
	roots := make([]*elem, n)
	cur := f.s.back()
	for i := n - 1; i >= 0; i-- {
		roots[i] = cur
		if i > 0 {
			cur = baseOfValentBlock(cur).prev
		}
	}
	regs := make([]Reg, n)
	for i := 0; i < n; i++ {
		regs[i] = f.materialize(roots[i])
		f.pinned = f.pinned.add(regs[i])
	}
	p := f.allocReg(0) // results pointer (result regs are pinned, so avoided)
	a.Load64(p, RBP, -32)
	for i := range res {
		a.Store64(p, int32(8*i), regs[i])
	}
	a.Load64(RSI, RBP, -24) // trap ptr (results already stored; safe to clobber)
	a.StoreImm32Mem(RSI, 0, 0)
	a.Leave()
	a.Ret()
}

func mtOf(t wasm.ValType) machineType {
	switch {
	case wasm.EqualValType(t, wasm.I32):
		return mtI32
	case wasm.EqualValType(t, wasm.I64):
		return mtI64
	case wasm.EqualValType(t, wasm.F32):
		return mtF32
	case wasm.EqualValType(t, wasm.F64):
		return mtF64
	}
	return mtNone
}

func countLocals(params []wasm.ValType, locals wasm.Locals) (int, error) {
	n := len(params)
	for _, run := range locals.Runs {
		n += int(run.Count)
	}
	return n, nil
}
