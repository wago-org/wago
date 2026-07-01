package x64

import (
	"encoding/binary"
	"fmt"
	"sort"

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

	// Register-pinned locals (the WARP recoverLocalToReg win): localReg[i] is the
	// dedicated GP register caching integer local i (or regNone); localFReg[i] the
	// dedicated XMM register caching float local i. Pinned registers are excluded
	// from their allocation pools.
	localReg         []Reg
	pinnedLocalMask  regMask
	localFReg        []Reg
	fpinnedLocalMask regMask

	// WARP STACK_REG lazy-spill model for pinned locals in CALL-MAKING functions
	// (usesCalls). localState[i] tracks whether the live value of pinned local i is
	// in its register (dirty), in both register+slot (clean), or only in its slot.
	// Call-free functions keep locals permanently in registers (localState unused).
	usesCalls  bool
	localState []locState

	// Register occupancy: regUser[r] is the value elem currently resident in
	// physical register r, or nil if r is free. Only allocatable GPRs are tracked.
	regUser [16]*elem
	// pinned[r] marks a register temporarily protected from spilling/allocation
	// (e.g. an operand being consumed by the current op).
	pinned regMask

	// Parallel XMM-register occupancy for float values (Phase 5).
	fregUser [16]*elem
	fpinned  regMask

	maxSpill  int  // high-water number of operand spill slots used
	subRspAt  int  // byte offset of the prologue's SubRsp imm32 (patched with frameSize)
	guardMode bool // elide inline bounds checks; rely on guard-page + SIGSEGV trap

	// Control-flow state (Phase 3).
	ctrl        []ctrlFrame // open block/loop/if frames; ctrl[0] is the function frame
	unreachable bool        // in dead code after an unconditional branch/trap
	retSites    []int       // forward jmp sites that target the epilogue

	// Call state (Phase 4).
	relocs []callReloc // CallRel32 sites to patch at module layout
}

func align16(n int) int { return (n + 15) &^ 15 }

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
// CompileModule compiles with inline bounds checks (the safe default).
func CompileModule(m *wasm.Module) (*amd64.CompiledModule, error) {
	return CompileModuleWith(m, false)
}

// CompileModuleWith compiles every local function. guardMode elides the inline
// linear-memory bounds check, relying on a guard-page mapping + SIGSEGV handler
// (the caller must back memory with runtime guard pages).
func CompileModuleWith(m *wasm.Module, guardMode bool) (*amd64.CompiledModule, error) {
	n := len(m.Code)
	relocs := make([][]callReloc, n)
	entry := make([]int, n)
	internalEntry := make([]int, n)
	var code []byte
	for i := range m.Code {
		fnCode, rl, internalOff, err := compileFunc(m, i, guardMode)
		if err != nil {
			return nil, fmt.Errorf("x64: function %d: %w", i, err)
		}
		// 16-byte align each function.
		if pad := (16 - len(code)%16) % 16; pad != 0 {
			code = append(code, make([]byte, pad)...)
		}
		entry[i] = len(code)
		internalEntry[i] = len(code) + internalOff
		relocs[i] = rl
		code = append(code, fnCode...)
	}
	// Patch call sites now that every function's entry offsets are known.
	for i := 0; i < n; i++ {
		for _, rl := range relocs[i] {
			site := entry[i] + rl.at
			target := entry[rl.target]
			if rl.internal {
				target = internalEntry[rl.target]
			}
			binary.LittleEndian.PutUint32(code[site:], uint32(int32(target-(site+4))))
		}
	}
	return &amd64.CompiledModule{Code: code, Entry: entry}, nil
}

func compileFunc(m *wasm.Module, funcIdx int, guardMode bool) (code []byte, relocs []callReloc, internalOff int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("x64: %v", r)
		}
	}()

	ft, ok := m.LocalFuncType(funcIdx)
	if !ok {
		return nil, nil, 0, fmt.Errorf("unknown function type")
	}
	c := &m.Code[funcIdx]
	nLocals, err := countLocals(ft.Params, c.Locals)
	if err != nil {
		return nil, nil, 0, err
	}

	f := &fn{a: &amd64.Asm{}, s: newStack(), m: m, ft: ft, nParams: len(ft.Params), nLocals: nLocals, guardMode: guardMode}
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
	f.assignPinnedLocals(localHotness(c.Body, nLocals))
	f.usesCalls = bodyHasCall(c.Body) && !noStackReg
	f.localState = make([]locState, nLocals) // all lsReg (0): params loaded / locals zeroed into regs

	if regABIEnabled && sigFitsRegABI(ft) {
		internalOff, err := f.emitRegABI(c)
		if err != nil {
			return nil, nil, 0, err
		}
		return f.a.B, f.relocs, internalOff, nil
	}

	f.prologue()
	if err := f.runBody(c); err != nil {
		return nil, nil, 0, err
	}
	f.epilogue()
	f.a.PatchU32(f.subRspAt, uint32(f.frameSize()))
	return f.a.B, f.relocs, 0, nil
}

// runBody opens the function control frame, lowers the body, and patches every
// return/br-to-function site to the (current) epilogue position.
func (f *fn) runBody(c *wasm.Func) error {
	f.ctrl = []ctrlFrame{{kind: cfFunc, resultN: len(f.ft.Results), branchN: len(f.ft.Results)}}
	if err := f.body(c.BodyBytes); err != nil {
		return err
	}
	for _, s := range f.retSites {
		f.a.PatchRel32(s, f.a.Len())
	}
	return nil
}

// assignPinnedLocals dedicates registers to the hottest integer locals (by the
// hotness scores). Locals with a zero score (no AST / unused) are ordered by
// index, so a body carrying only BodyBytes falls back to first-N pinning.
func (f *fn) assignPinnedLocals(scores []int64) {
	f.localReg = make([]Reg, f.nLocals)
	f.localFReg = make([]Reg, f.nLocals)
	for i := range f.localReg {
		f.localReg[i] = regNone
		f.localFReg[i] = regNone
	}
	// Rank integer and float locals separately by hotness (desc), then index (asc),
	// and pin the hottest of each to their dedicated register file.
	rank := func(want func(machineType) bool) []int {
		var c []int
		for i := 0; i < f.nLocals; i++ {
			if want(f.localType[i]) {
				c = append(c, i)
			}
		}
		sort.SliceStable(c, func(a, b int) bool {
			if scores[c[a]] != scores[c[b]] {
				return scores[c[a]] > scores[c[b]]
			}
			return c[a] < c[b]
		})
		return c
	}
	for k, i := range rank(func(t machineType) bool { return !t.isFloat() }) {
		if k >= len(pinnedLocalRegs) {
			break
		}
		f.localReg[i] = pinnedLocalRegs[k]
		f.pinnedLocalMask = f.pinnedLocalMask.add(pinnedLocalRegs[k])
	}
	for k, i := range rank(func(t machineType) bool { return t.isFloat() }) {
		if k >= len(pinnedFLocalRegs) {
			break
		}
		f.localFReg[i] = pinnedFLocalRegs[k]
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(pinnedFLocalRegs[k])
	}
}

// prologue: standard frame, pin linMem in RBX (moved from RSI per WARP's
// convention), stash trap/results, load params into their register or slot, zero
// declared locals.
func (f *fn) prologue() {
	a := f.a
	a.Prologue()              // push rbp; mov rbp,rsp
	f.subRspAt = len(a.B) + 3 // SubRsp opcode is 3 bytes (48 81 EC), then imm32
	a.SubRsp(0)               // frame; imm32 patched after body
	a.MovReg64(RBX, RSI)      // linMem → RBX (pinned for the whole function)
	a.Store64(RBP, -16, RSI)
	a.Store64(RBP, -24, RDX) // trap ptr
	a.Store64(RBP, -32, RCX) // results ptr
	f.emitStackFenceCheck(RBX, RAX)
	for i := 0; i < f.nParams; i++ {
		if pr := f.localReg[i]; pr != regNone {
			a.Load64(pr, RDI, int32(8*i)) // pinned int param → its GP register
		} else if pr := f.localFReg[i]; pr != regNone {
			a.FLoadDisp(pr, RDI, int32(8*i), f.localType[i] == mtF64) // pinned float param → XMM
		} else {
			a.Load64(RAX, RDI, int32(8*i))
			a.Store64(RBP, f.localOff(i), RAX)
		}
	}
	f.zeroDeclaredLocals()
}

// zeroDeclaredLocals zeroes the non-parameter locals (their register, if pinned,
// else their frame slot). Uses RAX; callers must have consumed any live RAX.
func (f *fn) zeroDeclaredLocals() {
	if f.nLocals <= f.nParams {
		return
	}
	a := f.a
	a.XorSelf32(RAX)
	for i := f.nParams; i < f.nLocals; i++ {
		if pr := f.localReg[i]; pr != regNone {
			a.XorSelf32(pr)
		} else if pr := f.localFReg[i]; pr != regNone {
			a.SseRR(0x66, 0x57, pr, pr, false) // xorpd pr,pr → 0.0
		} else {
			a.Store64(RBP, f.localOff(i), RAX)
		}
	}
}

// emitStackFenceCheck traps (StackFence → "call stack exhausted") when RSP has
// dropped below the fence stored at [linMem-72], turning unbounded recursion into
// a clean trap instead of a fault. A zero fence disables the check (RSP > 0).
func (f *fn) emitStackFenceCheck(linMemReg, scratch Reg) {
	if noStackFence {
		return
	}
	f.a.Load64(scratch, linMemReg, -72)
	f.a.Cmp64(RSP, scratch)
	ok := f.a.JccPlaceholder(condAE)
	f.emitTrap(trapStackFence)
	f.a.PatchRel32(ok, f.a.Len())
}

// emitRegABI emits a register-ABI function as [host adapter | internal entry].
// The adapter at offset 0 keeps the wrapper ABI working for exports/host calls;
// the internal entry takes args in registers and returns its result in RAX.
// Returns the internal entry's offset within the function's code.
func (f *fn) emitRegABI(c *wasm.Func) (int, error) {
	a := f.a
	np, rN := f.nParams, len(f.ft.Results)

	// Host→internal adapter (offset 0): in RDI=serArgs, RSI=linMem, RDX=trap,
	// RCX=results; loads args into registers, calls the internal entry, stores RAX.
	a.Push(RCX)
	a.Push(RDX)
	a.Push(RSI)
	for i := 0; i < np; i++ {
		a.Load64(intArgRegs[i], RDI, int32(8*i))
	}
	a.Pop(RDI) // linMem
	a.Pop(RSI) // trap
	adapterCall := a.CallRel32()
	a.Pop(RCX) // results
	if rN == 1 {
		a.Store64(RCX, 0, RAX)
	}
	a.Ret()

	// Internal entry: in RDI=linMem, RSI=trap, args in intArgRegs; result → RAX.
	internalOff := a.Len()
	a.Prologue()
	f.subRspAt = a.Len() + 3
	a.SubRsp(0)
	a.MovReg64(RBX, RDI) // linMem → RBX
	a.Store64(RBP, -16, RDI)
	a.Store64(RBP, -24, RSI) // trap
	f.emitStackFenceCheck(RBX, RSI)
	for i := 0; i < np; i++ {
		if pr := f.localReg[i]; pr != regNone {
			a.MovReg64(pr, intArgRegs[i])
		} else {
			a.Store64(RBP, f.localOff(i), intArgRegs[i])
		}
	}
	f.zeroDeclaredLocals()
	if err := f.runBody(c); err != nil {
		return 0, err
	}
	if rN == 1 {
		a.Load64(RAX, RBP, f.spillOff(0)) // result → RAX
	}
	a.Load64(RCX, RBP, -24) // clear trap (RCX is never the result register)
	a.StoreImm32Mem(RCX, 0, 0)
	a.Leave()
	a.Ret()

	a.PatchU32(f.subRspAt, uint32(f.frameSize()))
	a.PatchRel32(adapterCall, internalOff)
	return internalOff, nil
}

// epilogue: copy results from their canonical slots to the results buffer, clear
// the trap slot, and return. Every reaching path (fallthrough end, return, br to
// the function label) has already placed the results in slots [0, resultN).
func (f *fn) epilogue() {
	a := f.a
	a.Load64(RDI, RBP, -32) // results ptr
	for i := range f.ft.Results {
		a.Load64(RAX, RBP, f.spillOff(i))
		a.Store64(RDI, int32(8*i), RAX)
	}
	a.Load64(RSI, RBP, -24) // trap ptr
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
