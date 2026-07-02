package amd64

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// regMergeEnabled turns on WARP-style register reconciliation of single-int-result
// block/if merges (docs/operand-stack-registers-plan.md) instead of the
// flush-to-slot + reload. Default ON (fib_rec −13.7%, json-as serialize −1.5%, no
// regressions; validated against the spec suite + full corpus differential).
// WAGO_REG_MERGE=0 restores the slot path — kept as the reference oracle for A/B.
var regMergeEnabled = os.Getenv("WAGO_REG_MERGE") != "0"

// mergeReg is the canonical register a single-int-result block's value is
// reconciled into at every edge (fall-through, br, br_if, br_table) so the merge
// needs no slot round trip. RBP is a plain allocatable GPR (frameless backend),
// not a pinned-local (R12-R15) or fixed-role scratch.
const mergeReg = RBP

// mergeFReg is mergeReg's float counterpart: the canonical XMM a single-float-
// result block/if is reconciled into. XMM11 is in the operand pool (0-11), not a
// pinned-float-local (12-15).
const mergeFReg Reg = 11

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
	// singleRegResult: this function uses the register-return ABI with exactly one
	// result. Its exits produce that result directly in the return register — RAX
	// (int) or XMM0 (float) — via the WARP-style target hint, skipping the
	// flush-to-slot-0 + epilogue-reload round trip. resultFloat/resultF64 cache the
	// result's type for that placement.
	singleRegResult bool
	resultFloat     bool
	resultF64       bool
	regMerge        bool // reconcile single-int-result blocks in mergeReg (phase 2)

	// globalCellReg caches the cell pointer (&global[globalCellIdx]) of the most
	// recently accessed global in a register across a straight-line run, so repeated
	// accesses skip re-deriving that loop-invariant pointer. regNone when not cached;
	// invalidated at every flush (calls + control-flow boundaries). See globals.go.
	globalCellReg Reg
	globalCellIdx uint32

	// globalReg[g] pins hot global g's cell pointer in a register for the whole
	// function (call-free functions only), sharing the GP pin pool with hot locals.
	// The pointer is derived once in the prologue and every access reads/writes
	// through it — no per-access (or per-iteration) re-derivation or single-entry
	// cache thrashing. regNone when g is not pinned. See globals.go / assignPinnedLocals.
	globalReg []Reg

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

	f := &fn{a: &amd64.Asm{}, s: newStack(), m: m, ft: ft, nParams: len(ft.Params), nLocals: nLocals, guardMode: guardMode, regMerge: regMergeEnabled, globalCellReg: regNone}
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
	regABI := regABIEnabled && sigFitsRegABI(ft)
	gpPool := gpPinPool(regABI, !hasCall, bodyUsesBulkMem(c.Body), f.nParams)
	// Hot globals share the GP pin pool with locals, but only in call-free reg-ABI
	// functions: their cell pointer stays register-resident across the whole body
	// (no call to clobber it), derived once in the prologue. Elsewhere globals use
	// the per-run cell-pointer cache (globalCellPtr).
	var globalScores []int64
	if regABI && !hasCall {
		globalScores = globalHotness(c.Body, f.m.GlobalCount())
	}
	f.assignPinnedLocals(localHotness(c.Body, nLocals), globalScores, gpPool)
	if f.pinnedLocalMask.has(RBP) {
		f.regMerge = false // RBP now holds a pinned local/global, so it can't be the merge register
	}
	// STACK_REG (lazy pinned-local spill) is disabled for memory-touching
	// functions (#68): the explicit-bounds path's per-access scratch allocation
	// (memAddr) adds register pressure that desyncs the lazy local state,
	// corrupting a pinned pointer local. Eager save/reload is correct in both modes
	// for any call+memory function; compute-only call functions keep the lazy model.
	f.usesCalls = hasCall && !touchesMemory && !noStackReg
	// The return-in-register hint helps compute/call-heavy code (recursion,
	// dispatch) but adds register pressure in the deep, memory-bound call graphs
	// (json-as's TLSF/GC) where it measured as a small regression. Gate it on
	// !touchesMemory so it only fires where it's a win.
	f.singleRegResult = regABIEnabled && sigFitsRegABI(ft) && !touchesMemory && len(ft.Results) == 1
	if f.singleRegResult {
		rt := mtOf(ft.Results[0])
		f.resultFloat = rt.isFloat()
		f.resultF64 = rt == mtF64
	}
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
// gpPinPool returns the registers available to hold pinned integer locals. The
// base is R12-R15 (callee-saved and spill-managed around calls). A call-free
// reg-ABI function has no call to clobber caller-saved registers, so it fills more
// of the file: the non-argument registers RDI/RSI (free once the prologue consumes
// linMem/trap — unless bulk-memory `rep movs` would clobber them), the argument
// registers R9/R10/R11 when they carry no incoming parameter (nParams<=4, so the
// prologue's arg→pinned moves can't clobber a live arg), and RBP (which then can't
// serve as the block-merge register — the caller drops regMerge). RAX/RCX/RDX/R8
// stay free for operand evaluation and the x86 fixed-role ops (div/shift/return).
func gpPinPool(regABI, callFree, usesBulkMem bool, nParams int) []Reg {
	pool := append([]Reg{}, pinnedLocalRegs...) // R12-R15
	if !regABI || !callFree {
		return pool
	}
	if !usesBulkMem {
		pool = append(pool, RDI, RSI)
	}
	if nParams <= 4 {
		pool = append(pool, R9, R10, R11)
	}
	return append(pool, RBP)
}

func (f *fn) assignPinnedLocals(scores, globalScores []int64, gpPool []Reg) {
	f.locals = make([]localDef, f.nLocals)
	for i := range f.locals {
		f.locals[i] = localDef{reg: regNone, typ: f.localType[i], state: lsReg}
	}
	f.globalReg = make([]Reg, len(globalScores))
	for i := range f.globalReg {
		f.globalReg[i] = regNone
	}
	// The GP pin pool is shared by hot INT locals (value-in-register) and hot globals
	// (cell-pointer-in-register — even a float global's cell pointer is a GP value).
	// A global is a candidate only when accessed inside a loop (score >= one loop
	// level): pinning costs a one-time prologue derivation, worth it only when the
	// per-iteration re-derivation it replaces would otherwise repeat.
	type gpCand struct {
		global bool
		idx    int
		score  int64
	}
	var gp []gpCand
	for i := 0; i < f.nLocals; i++ {
		if !f.localType[i].isFloat() {
			gp = append(gp, gpCand{idx: i, score: scores[i]})
		}
	}
	loopMin := loopWeight(1)
	for g := 0; g < len(globalScores); g++ {
		if globalScores[g] >= loopMin {
			gp = append(gp, gpCand{global: true, idx: g, score: globalScores[g]})
		}
	}
	sort.SliceStable(gp, func(a, b int) bool {
		if gp[a].score != gp[b].score {
			return gp[a].score > gp[b].score
		}
		if gp[a].global != gp[b].global {
			return !gp[a].global // tie: prefer a local (value) over a global (pointer)
		}
		return gp[a].idx < gp[b].idx
	})
	for k, c := range gp {
		if k >= len(gpPool) {
			break
		}
		if c.global {
			f.globalReg[c.idx] = gpPool[k]
		} else {
			f.locals[c.idx].reg = gpPool[k]
		}
		f.pinnedLocalMask = f.pinnedLocalMask.add(gpPool[k])
	}
	// Float locals use the separate XMM pin pool.
	var fc []int
	for i := 0; i < f.nLocals; i++ {
		if f.localType[i].isFloat() {
			fc = append(fc, i)
		}
	}
	sort.SliceStable(fc, func(a, b int) bool {
		if scores[fc[a]] != scores[fc[b]] {
			return scores[fc[a]] > scores[fc[b]]
		}
		return fc[a] < fc[b]
	})
	for k, i := range fc {
		if k >= len(pinnedFLocalRegs) {
			break
		}
		f.locals[i].reg = pinnedFLocalRegs[k]
		f.locals[i].isFloat = true
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(pinnedFLocalRegs[k])
	}
}

// derivePinnedGlobals loads each pinned global's cell pointer into its dedicated
// register, once, in the prologue (RBX = linMem must already be set). A no-op when
// no globals are pinned. Every later access reads/writes through the register.
func (f *fn) derivePinnedGlobals() {
	for g, reg := range f.globalReg {
		if reg == regNone {
			continue
		}
		f.a.Load64(reg, RBX, -int32(abi.GlobalsPtrOffset))
		f.a.Load64(reg, reg, int32(g*8))
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
	f.derivePinnedGlobals()
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
	f.derivePinnedGlobals()
	if err := f.runBody(c); err != nil {
		return 0, err
	}
	if rN == 1 && !f.singleRegResult {
		rt := mtOf(f.ft.Results[0])
		if rt.isFloat() {
			a.FLoadDisp(0, RSP, f.spillOff(0), rt == mtF64) // result -> XMM0
		} else {
			a.Load64(RAX, RSP, f.spillOff(0)) // result -> RAX
		}
	}
	// singleRegResult: every exit already produced the result in RAX/XMM0.
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
