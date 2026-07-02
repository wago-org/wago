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

	// memSizeReg caches the linear-memory size in bytes ([RBX-bdCurBytes]) in a
	// dedicated register for the whole module (WARP's REGS::memSize, which reserves
	// RSI when bounds checks are on). regNone in guard mode or when the module has
	// no memory. wago's ABI keeps RSI busy at every call boundary (trap/linMem), so
	// R15 is used instead: it has no fixed role, so it is preserved by construction
	// across wasm→wasm calls (reserved out of every pool module-wide), refreshed by
	// memory.grow, and established once at every offset-0 entry (wrapper prologue /
	// reg-ABI adapter — the only ways an activation enters from Go).
	memSizeReg Reg
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

	// globalReg[g] value-pins hot mutable-int global g in a register for the whole
	// function, sharing the GP pin pool with hot locals (WARP's model). The value is
	// loaded once in the prologue and every access reads/writes the register directly
	// (no per-access memory traffic); dirty values are written back to the cell at the
	// epilogue. In call-making functions the value is spilled to / reloaded from the
	// cell around each internal call for coherence, so only globals accessed in a
	// CALL-FREE loop are pinned there (the spill/reload lands on out-of-loop calls).
	// regNone when g is not pinned. See globals.go / assignPinnedLocals.
	globalReg   []Reg
	globalDirty []bool // value-pinned global g was written → needs epilogue write-back

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
			if os.Getenv("WAGO_DEBUG_PANIC") == "1" {
				panic(r)
			}
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

	f := &fn{a: &amd64.Asm{}, s: newStack(), m: m, ft: ft, nParams: len(ft.Params), nLocals: nLocals, guardMode: guardMode, regMerge: regMergeEnabled, globalCellReg: regNone, memSizeReg: regNone}
	if !guardMode && len(m.Memories) > 0 {
		f.memSizeReg = R15 // explicit bounds: R15 = memBytes for the whole module
	}
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
	if f.memSizeReg != regNone {
		gpPool = withoutReg(gpPool, f.memSizeReg) // R15 is the module-wide memBytes cache
	}
	// Cap pins so at least numScratchGP+1 GPRs stay allocatable: the reserved
	// scratch four plus one spare for allocations that must avoid RAX/RDX/RCX and a
	// target (condenseBinary's RHS-relocation). Pinning deeper than this exhausts
	// the allocator on high-pressure expression trees.
	maxPins := len(gpAlloc) - numScratchGP - 1
	if f.memSizeReg != regNone {
		maxPins-- // R15 is reserved out of the allocatable file too
	}
	if len(gpPool) > maxPins {
		gpPool = gpPool[:maxPins]
	}
	// Hot mutable-int globals share the GP pin pool with locals, holding their VALUE
	// in the register (WARP's model). In call-free functions any loop-accessed global
	// qualifies; in call-making functions only globals accessed inside a CALL-FREE
	// loop do — the spill/reload keeping the cell coherent then lands on the sparse
	// out-of-loop calls, not per iteration. Non-eligible globals use the per-run
	// cell-pointer cache (globalCellPtr).
	var globalScores []int64
	var globalElig []bool
	if regABI {
		globalScores = globalHotness(c.Body, f.m.GlobalCount())
		if hasCall {
			globalElig = globalCallFreeLoopAccess(c.Body, f.m.GlobalCount())
		}
	}
	f.assignPinnedLocals(localHotness(c.Body, nLocals), globalScores, globalElig, gpPool)
	if f.pinnedLocalMask.has(RBP) {
		f.regMerge = false // RBP now holds a pinned local/global, so it can't be the merge register
	}
	// STACK_REG (lazy pinned-local spill) for every call-making function,
	// including memory-touching ones: dirty-only stores before a call, lazy reload
	// on the next read (WARP's model). #68 disabled this for memory functions as a
	// workaround; the actual root cause was the opElse merge edge skipping
	// reconcileLocals (fixed in control.go, TestExecIfElseLocalMerge).
	f.usesCalls = hasCall && !noStackReg
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

// withoutReg returns pool with r removed (order preserved).
func withoutReg(pool []Reg, r Reg) []Reg {
	out := pool[:0]
	for _, p := range pool {
		if p != r {
			out = append(out, p)
		}
	}
	return out
}

func (f *fn) assignPinnedLocals(scores, globalScores []int64, globalElig []bool, gpPool []Reg) {
	f.locals = make([]localDef, f.nLocals)
	for i := range f.locals {
		f.locals[i] = localDef{reg: regNone, typ: f.localType[i], state: lsReg}
	}
	f.globalReg = make([]Reg, len(globalScores))
	f.globalDirty = make([]bool, len(globalScores))
	for i := range f.globalReg {
		f.globalReg[i] = regNone
	}
	// The GP pin pool is shared by hot INT locals and hot globals, both holding their
	// VALUE in the register (WARP's model). A global is a candidate only when it is a
	// mutable int accessed inside a loop (score >= one loop level): WARP pins only int
	// globals as values, and the loop gate ensures the per-iteration memory traffic it
	// removes outweighs the one-time prologue load + epilogue write-back.
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
		if globalScores[g] < loopMin {
			continue
		}
		// In a call-making function (globalElig non-nil) only globals accessed in a
		// call-free loop qualify — otherwise the per-call spill/reload would land in
		// the hot loop and regress. In a call-free function every loop-accessed global
		// qualifies (globalElig nil).
		if globalElig != nil && !globalElig[g] {
			continue
		}
		gt, ok := f.m.GlobalTypeByIndex(uint32(g))
		if !ok || !gt.Mutable || !isIntValType(wasm.GlobalValueType(gt)) {
			continue
		}
		gp = append(gp, gpCand{global: true, idx: g, score: globalScores[g]})
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
func (f *fn) globalIs64(g int) bool {
	gt, _ := f.m.GlobalTypeByIndex(uint32(g))
	return wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I64)
}

// derivePinnedGlobals loads each value-pinned global's current value into its
// register from memory (base → &cell → value, reusing the register for the chain).
// Used in the prologue and to reload after a call (the callee may have changed the
// shared global). A no-op when no globals are pinned.
func (f *fn) derivePinnedGlobals() {
	for g, reg := range f.globalReg {
		if reg == regNone {
			continue
		}
		f.a.Load64(reg, RBX, -int32(abi.GlobalsPtrOffset)) // globals array base
		f.a.Load64(reg, reg, int32(g*8))                   // &cell[g]
		if f.globalIs64(g) {
			f.a.Load64(reg, reg, 0)
		} else {
			f.a.Load32(reg, reg, 0) // i32: low half, zero-extended
		}
	}
}

// storePinnedGlobals writes value-pinned globals' registers back to their memory
// cells. dirtyOnly (epilogue) writes only the globals this function actually set;
// the call path (dirtyOnly=false) writes all of them before a call so the callee
// observes the current value. Avoids RAX (the int result register) for the
// cell-address scratch.
func (f *fn) storePinnedGlobals(dirtyOnly bool) {
	for g, reg := range f.globalReg {
		if reg == regNone || (dirtyOnly && !f.globalDirty[g]) {
			continue
		}
		t := f.allocReg(maskOf(reg, RAX))
		f.a.Load64(t, RBX, -int32(abi.GlobalsPtrOffset))
		f.a.Load64(t, t, int32(g*8))
		if f.globalIs64(g) {
			f.a.Store64(t, 0, reg)
		} else {
			f.a.Store32(t, 0, reg)
		}
		f.release(t)
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
	if f.memSizeReg != regNone {
		// Offset-0 entry: establish the module-wide memBytes cache. Direct wasm→wasm
		// register-ABI calls skip this (the caller's value is valid by construction).
		a.Load32(f.memSizeReg, RBX, -bdCurBytes)
	}
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
	if f.memSizeReg != regNone {
		// Offset-0 entry (from Go, or an indirect call): establish the module-wide
		// memBytes cache before the internal entry runs (which relies on it).
		a.Load32(f.memSizeReg, RSI, -bdCurBytes)
	}
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
	f.storePinnedGlobals(true) // write dirty value-pinned globals back to their cells (all returns land here)
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
