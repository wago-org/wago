package amd64

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
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

	// WARP-style per-local storage metadata. localType remains as the compact
	// type table used by existing lowering; locals holds the assigned register and
	// call-spill state for each local.
	locals           []localDef
	pinnedLocalMask  regMask
	fpinnedLocalMask regMask

	// WARP STACK_REG lazy-spill model for pinned locals in CALL-MAKING functions
	// (usesCalls). locals[i].state tracks whether the live value of pinned local i is
	// in its register (dirty), in both register+slot (clean), or only in its slot.
	// Call-free functions keep locals permanently in registers (locals[].state unused).
	usesCalls bool

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
	addRspAt  int  // byte offset of the epilogue's AddRsp imm32 (patched with frameSize)
	guardMode bool // elide inline bounds checks; rely on guard-page + SIGSEGV trap
	lazyZero  bool // defer declared-local zeroing for small call+memory functions

	// Control-flow state (Phase 3).
	ctrl        []ctrlFrame // open block/loop/if frames; ctrl[0] is the function frame
	unreachable bool        // in dead code after an unconditional branch/trap
	retSites    []int       // forward jmp sites that target the epilogue

	// Call state (Phase 4).
	relocs []callReloc // CallRel32 sites to patch at module layout

	// Occurrence tracking (WARP ModuleInfo referencesToLastOccurrenceOnStack):
	// maps local refs, owned scratch regs, and spill slots to the topmost stack
	// element currently representing that storage. This is infrastructure for the
	// fuller WARP local/storage model; current codegen behavior stays unchanged.
	refs map[refKey]*elem
}

func align16(n int) int { return (n + 15) &^ 15 }

// Frameless layout (WARP-style, RSP-relative). RBP is NOT a frame pointer — it is
// a general allocatable register — so the frame is a single `sub rsp,frameSize`
// with everything addressed at non-negative offsets from RSP, which stays put for
// the whole body (wrapper-call arg/result buffers reuse spill slots, so no
// transient SubRsp/AddRsp). Layout, low→high address from RSP:
//
//	[rsp+0] trap ptr · [rsp+8] results ptr · locals · spill slots
const (
	frameHdrBytes = 16 // trap ptr + results ptr
	frTrapOff     = 0  // *TrapCode pointer
	frResultsOff  = 8  // results buffer pointer
)

func (f *fn) localOff(i int) int32 { return int32(frameHdrBytes + 8*i) }
func (f *fn) spillOff(k int) int32 { return int32(frameHdrBytes + 8*f.nLocals + 8*k) }

// frameSize is biased to ≡ 8 (mod 16): the function is entered with RSP ≡ 8
// (mod 16) after the trampoline's CALL and there is no frame-pointer push to
// re-align, so `sub rsp,frameSize` must land the body on a 16-aligned RSP to keep
// our own call sites correctly aligned.
func (f *fn) frameSize() int {
	return align16(frameHdrBytes+8*f.nLocals+8*f.maxSpill) + 8
}

// CompileOptions configures direct wasm-to-amd64 compilation.
type CompileOptions struct {
	// ElideBoundsChecks omits inline linear-memory bounds checks, relying on
	// a guard-page mapping + SIGSEGV handler (see runtime/sigtrap_linux_amd64.go).
	// EXPERIMENTAL: only sound when the memory is backed by runtime guard pages.
	ElideBoundsChecks bool

	// Codegen carries injectable runtime/heap dependencies for future WasmGC
	// lowering. The current direct backend does not lower WasmGC opcodes yet, but
	// threading the option here lets that work use the same HeapABI as the IR
	// backend instead of hard-coding allocator or collector choices.
	Codegen codegen.Options
}

// DirectBackend adapts the direct wasm-to-amd64 compiler to the shared
// backend-neutral codegen.Backend shape used by heap/GC lowering work.
type DirectBackend struct{}

var _ codegen.Backend[*wasm.Module] = DirectBackend{}

func (DirectBackend) Name() string { return "amd64-direct" }

func (DirectBackend) CompileModule(m *wasm.Module, opts codegen.Options) (*codegen.Object, error) {
	cm, err := CompileModuleWith(m, CompileOptions{Codegen: opts})
	if err != nil {
		return nil, err
	}
	return &codegen.Object{Code: cm.Code, Entry: cm.Entry}, nil
}

// CompileModule compiles every local function into one executable blob with
// per-function entry offsets — the same shape src/core/encoder/amd64 produces, so
// src/wago consumes it unchanged. Phase 0: straight-line integer functions.
// CompileModule compiles with inline bounds checks (the safe default).
func CompileModule(m *wasm.Module) (*amd64.CompiledModule, error) {
	return CompileModuleWith(m, CompileOptions{})
}

// CompileModuleWith compiles every local function. ElideBoundsChecks elides the
// inline linear-memory bounds check, relying on a guard-page mapping + SIGSEGV
// handler (the caller must back memory with runtime guard pages).
func CompileModuleWith(m *wasm.Module, opts CompileOptions) (*amd64.CompiledModule, error) {
	guardMode := opts.ElideBoundsChecks
	n := len(m.Code)
	relocs := make([][]callReloc, n)
	entry := make([]int, n)
	internalEntry := make([]int, n)
	var code []byte
	for i := range m.Code {
		fnCode, rl, internalOff, err := compileFunc(m, i, guardMode)
		if err != nil {
			return nil, fmt.Errorf("amd64: function %d: %w", i, err)
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
			err = fmt.Errorf("amd64: %v", r)
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
	hasCall := bodyHasCall(c.Body)
	touchesMemory := bodyTouchesMemory(c.Body)
	f.assignPinnedLocals(localHotness(c.Body, nLocals))
	f.usesCalls = hasCall && !(guardMode && touchesMemory) && !noStackReg
	selfIdx := uint32(m.ImportedFuncCount() + funcIdx)
	f.lazyZero = bodyCalls(c.Body, selfIdx) && touchesMemory && len(c.BodyBytes) <= 192 && nLocals-len(ft.Params) <= 8

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
	f.a.PatchU32(f.addRspAt, uint32(f.frameSize()))
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
	f.locals = make([]localDef, f.nLocals)
	for i := range f.locals {
		f.locals[i] = localDef{reg: regNone, typ: f.localType[i], state: lsReg}
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
		f.locals[i].reg = pinnedLocalRegs[k]
		f.pinnedLocalMask = f.pinnedLocalMask.add(pinnedLocalRegs[k])
	}
	for k, i := range rank(func(t machineType) bool { return t.isFloat() }) {
		if k >= len(pinnedFLocalRegs) {
			break
		}
		f.locals[i].reg = pinnedFLocalRegs[k]
		f.locals[i].isFloat = true
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(pinnedFLocalRegs[k])
	}
}

// prologue: frameless — one `sub rsp,frameSize` (no frame-pointer push), pin
// linMem in RBX (moved from RSI per WARP's convention), stash trap/results in the
// RSP-relative header, load params into their register or slot, zero declared
// locals.
func (f *fn) prologue() {
	a := f.a
	f.subRspAt = len(a.B) + 3         // SubRsp opcode is 3 bytes (48 81 EC), then imm32
	a.SubRsp(0)                       // frame; imm32 patched after body
	a.MovReg64(RBX, RSI)              // linMem → RBX (pinned for the whole function)
	a.Store64(RSP, frTrapOff, RDX)    // trap ptr
	a.Store64(RSP, frResultsOff, RCX) // results ptr
	f.emitStackFenceCheck(RBX, RAX)
	for i := 0; i < f.nParams; i++ {
		if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
			a.Load64(pr, RDI, int32(8*i)) // pinned int param → its GP register
		} else if ok && isFloat {
			a.FLoadDisp(pr, RDI, int32(8*i), f.localType[i] == mtF64) // pinned float param → XMM
		} else {
			a.Load64(RAX, RDI, int32(8*i))
			a.Store64(RSP, f.localOff(i), RAX)
		}
	}
	f.zeroDeclaredLocals()
}

// zeroDeclaredLocals initializes non-parameter locals. Most functions keep the
// old eager zeroing path; small call+memory functions use WARP-style lazy zero,
// where reads materialize zero on demand and control-flow reconciliation stores it
// to the frame before paths diverge when required.
func (f *fn) zeroDeclaredLocals() {
	if f.nLocals <= f.nParams {
		return
	}
	if !f.lazyZero {
		a := f.a
		a.XorSelf32(RAX)
		for i := f.nParams; i < f.nLocals; i++ {
			if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
				a.XorSelf32(pr)
			} else if ok && isFloat {
				a.SseRR(0x66, 0x57, pr, pr, false) // xorpd pr,pr -> 0.0
			} else {
				a.Store64(RSP, f.localOff(i), RAX)
			}
		}
		return
	}
	for i := f.nParams; i < f.nLocals; i++ {
		f.markDeclaredLocalZero(i)
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
// the internal entry takes args in GP/XMM registers and returns its single result
// in RAX or XMM0.
// Returns the internal entry's offset within the function's code.
func (f *fn) emitRegABI(c *wasm.Func) (int, error) {
	a := f.a
	np, rN := f.nParams, len(f.ft.Results)

	// Host→internal adapter (offset 0): in RDI=serArgs, RSI=linMem, RDX=trap,
	// RCX=results; loads args into registers, calls the internal entry, stores the
	// single register result.
	a.Push(RCX)
	a.Push(RDX)
	a.Push(RSI)
	gp, fp := 0, 0
	for i := 0; i < np; i++ {
		mt := f.localType[i]
		if mt.isFloat() {
			a.FLoadDisp(fpArgRegs[fp], RDI, int32(8*i), mt == mtF64)
			fp++
		} else {
			a.Load64(intArgRegs[gp], RDI, int32(8*i))
			gp++
		}
	}
	a.Pop(RDI) // linMem
	a.Pop(RSI) // trap
	adapterCall := a.CallRel32()
	a.Pop(RCX) // results
	if rN == 1 {
		rt := mtOf(f.ft.Results[0])
		if rt.isFloat() {
			a.FStoreDisp(RCX, 0, 0, rt == mtF64) // XMM0
		} else {
			a.Store64(RCX, 0, RAX)
		}
	}
	a.Ret()

	// Internal entry (frameless): in RDI=linMem, RSI=trap, args in GP/XMM regs.
	internalOff := a.Len()
	f.subRspAt = a.Len() + 3
	a.SubRsp(0)
	a.MovReg64(RBX, RDI)           // linMem → RBX
	a.Store64(RSP, frTrapOff, RSI) // trap ptr
	f.emitStackFenceCheck(RBX, RSI)
	gp, fp = 0, 0
	for i := 0; i < np; i++ {
		mt := f.localType[i]
		if mt.isFloat() {
			src := fpArgRegs[fp]
			if pr, isFloat, ok := f.pinReg(i); ok && isFloat {
				a.FMov(pr, src, mt == mtF64)
			} else {
				a.FStoreDisp(RSP, f.localOff(i), src, mt == mtF64)
			}
			fp++
		} else if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
			a.MovReg64(pr, intArgRegs[gp])
		} else {
			a.Store64(RSP, f.localOff(i), intArgRegs[gp])
		}
		if !mt.isFloat() {
			gp++
		}
	}
	f.zeroDeclaredLocals()
	if err := f.runBody(c); err != nil {
		return 0, err
	}
	if rN == 1 {
		rt := mtOf(f.ft.Results[0])
		if rt.isFloat() {
			a.FLoadDisp(0, RSP, f.spillOff(0), rt == mtF64) // result -> XMM0
		} else {
			a.Load64(RAX, RSP, f.spillOff(0)) // result -> RAX
		}
	}
	a.Load64(RCX, RSP, frTrapOff) // clear trap (RCX is never the result register)
	a.StoreImm32Mem(RCX, 0, 0)
	f.addRspAt = a.Len() + 3
	a.AddRsp(0) // undo the frame; imm32 patched after body
	a.Ret()

	a.PatchU32(f.subRspAt, uint32(f.frameSize()))
	a.PatchU32(f.addRspAt, uint32(f.frameSize()))
	a.PatchRel32(adapterCall, internalOff)
	return internalOff, nil
}

// epilogue: copy results from their canonical slots to the results buffer, clear
// the trap slot, and return. Every reaching path (fallthrough end, return, br to
// the function label) has already placed the results in slots [0, resultN).
func (f *fn) epilogue() {
	a := f.a
	a.Load64(RDI, RSP, frResultsOff) // results ptr
	for i := range f.ft.Results {
		a.Load64(RAX, RSP, f.spillOff(i))
		a.Store64(RDI, int32(8*i), RAX)
	}
	a.Load64(RSI, RSP, frTrapOff) // trap ptr
	a.StoreImm32Mem(RSI, 0, 0)
	f.addRspAt = a.Len() + 3
	a.AddRsp(0) // undo the frame; imm32 patched after body
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
