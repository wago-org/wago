//go:build amd64

package amd64

import (
	"encoding/binary"
	"errors"
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

// immutableLocalTableEnabled specializes call_indirect when the one-pass module
// scan proves table 0 is a private, never-mutated table of same-module functions
// (no home/tag fork, and a monomorphic table becomes a direct call). Default ON;
// WAGO_AMD64_NO_IMMUTABLE_TABLE=1 restores the general indirect path for A/B.
var immutableLocalTableEnabled = os.Getenv("WAGO_AMD64_NO_IMMUTABLE_TABLE") != "1"

// immutableTableTypeEnabled removes call_indirect's dynamic type check only when
// the immutable table is uniformly typed. Default ON; WAGO_AMD64_NO_IMMUTABLE_TABLE_TYPE=1
// keeps the type check for A/B.
var immutableTableTypeEnabled = os.Getenv("WAGO_AMD64_NO_IMMUTABLE_TABLE_TYPE") != "1"

// linearStoreForwardEnabled keeps an owned full-width store value in a register
// across a very short window (local.get leaves + the exact matching load) so an
// immediately re-read linear-memory slot forwards the value instead of reloading
// it. Default ON; WAGO_AMD64_NOMEMFWD=1 disables it for A/B.
var linearStoreForwardEnabled = os.Getenv("WAGO_AMD64_NOMEMFWD") != "1"

// unaryLocalSinkEnabled / teeLocalSinkEnabled extend in-place local-result
// sinking (`local.set $x (op (local.get $x) …)` computed straight into x's
// register, no pre-copy) beyond the plain binary-ALU case: unary/convert result
// producers, and the `local.tee` form. Default ON; WAGO_AMD64_NOUNARYSINK /
// WAGO_AMD64_NOTEESINK disable them for A/B. Mirrors arm64.
var (
	unaryLocalSinkEnabled = os.Getenv("WAGO_AMD64_NOUNARYSINK") != "1"
	teeLocalSinkEnabled   = os.Getenv("WAGO_AMD64_NOTEESINK") != "1"
)

// entryArgPinsEnabled lets a call-free reg-ABI leaf pin hot locals in the free
// incoming-arg registers (R9-R11 past the param count). Default ON;
// WAGO_AMD64_NO_ENTRY_ARG_PINS=1 disables it for A/B.
var entryArgPinsEnabled = os.Getenv("WAGO_AMD64_NO_ENTRY_ARG_PINS") != "1"

// inlineCallFreeHintsEnabled lets a function whose every direct call is inlined be
// planned as call-free (aggressive pins, no STACK_REG spill model), since inline
// targets are call-free leaves. Default ON; WAGO_AMD64_NO_INLINE_CALLFREE=1
// disables it for A/B.
var inlineCallFreeHintsEnabled = os.Getenv("WAGO_AMD64_NO_INLINE_CALLFREE") != "1"

// extendedFPPinsEnabled lets a call-free function pin more than baseFPPins float
// locals (into XMM8-10). Default ON; WAGO_AMD64_NO_EXTFPPINS=1 caps at baseFPPins.
var extendedFPPinsEnabled = os.Getenv("WAGO_AMD64_NO_EXTFPPINS") != "1"

// smallFrameElideEnabled drops the frame entirely (frameSize 0, so `sub/add rsp`
// adjust nothing) for a register-homed call-free reg-ABI leaf whose frame slots
// are never touched. Default ON; WAGO_AMD64_NO_FRAME_ELIDE=1 disables it for A/B.
var smallFrameElideEnabled = os.Getenv("WAGO_AMD64_NO_FRAME_ELIDE") != "1"

// storeForward is the one-entry linear store→load forwarding window: a store's
// value register kept live for an immediately-following load of the same local
// address, offset, and full width.
type storeForward struct {
	valid  bool
	reg    Reg
	typ    machineType
	local  int
	offset uint32
	size   int
}

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

	nParams     int
	nLocals     int           // params + declared locals
	localType   []machineType // per-local machine type
	localSlot   []int         // per-local frame slot in 8-byte units; v128 occupies two
	nLocalSlots int           // total local frame slots in 8-byte units

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
	fconsts  []floatConstReg

	maxSpill      int  // high-water number of operand spill slots used
	subRspAt      int  // byte offset of the prologue's SubRsp imm32 (patched with frameSize)
	addRspAt      int  // byte offset of the epilogue's AddRsp imm32 (patched with frameSize)
	guardMode     bool // elide inline bounds checks; rely on guard-page + SIGSEGV trap
	boundsFacts   bool // P6.1 straight-line bounds-check elision enabled (explicit mode)
	interruptible bool // emit context-cancellation polls at entries and loop headers
	lazyZero      bool // defer declared-local zeroing for small call+memory functions
	skipFence     bool // call-free leaf with a provably small frame: no stack-fence check
	frameElided   bool // register-homed call-free reg-ABI leaf: frameSize is 0 (see elideRegisterOnlyFrame)

	// memSizeReg caches the linear-memory size in bytes ([RBX-bdCurBytes]) in a
	// dedicated register for the whole module (WARP's REGS::memSize, which reserves
	// RSI when bounds checks are on). regNone in guard mode or when the module has
	// no memory. wago's ABI keeps RSI busy at every call boundary (trap/linMem), so
	// R15 is used instead: it has no fixed role, so it is preserved by construction
	// across wasm→wasm calls (reserved out of every pool module-wide), refreshed by
	// memory.grow, and established once at every offset-0 entry (wrapper prologue /
	// reg-ABI adapter — the only ways an activation enters from Go).
	memSizeReg Reg
	// reserved is the module-wide never-allocatable register set: memSizeReg and
	// the module-pinned global registers.
	reserved regMask
	// singleRegResult: this function uses the register-return ABI with exactly one
	// result. Its exits produce that result directly in the return register — RAX
	// (int) or XMM0 (float) — via the WARP-style target hint, skipping the
	// flush-to-slot-0 + epilogue-reload round trip. resultFloat/resultF64 cache the
	// result's type for that placement.
	singleRegResult bool
	resultFloat     bool
	resultF64       bool
	regMerge        bool // reconcile single-int-result blocks in mergeReg (phase 2)

	// call_indirect immutable-local-table specialization (see computeModuleHints).
	// immutableLocalTable proves every non-null table-0 entry targets this module,
	// so no home/tag fork is needed; immutableTableTyped+immutableTableType elide
	// the type check; monomorphicTarget is the sole target (or -1) for a direct call.
	immutableLocalTable bool
	immutableTableType  uint32
	immutableTableTyped bool
	monomorphicTarget   int

	// One-entry linear-memory store forwarding window. The value register is
	// protected in f.pinned until an exact load consumes it or any non-local.get
	// opcode invalidates it; address identity is deliberately limited to a local.
	storeFwd storeForward
	// Keep the extra protected register out of large/high-pressure functions.
	storeForwardOK bool

	// globalCellReg caches the cell pointer (&global[globalCellIdx]) of the most
	// recently accessed global in a register across a straight-line run, so repeated
	// accesses skip re-deriving that loop-invariant pointer. regNone when not cached;
	// invalidated at every flush (calls + control-flow boundaries). See globals.go.
	globalCellReg Reg
	globalCellIdx uint32

	// Straight-line bounds-check certificate (P6.1). After a check proves
	// source+bcExtent <= memBytes, a later access on the SAME address source with
	// off+size <= bcExtent is in-bounds and needs no check. Keyed on the address
	// SOURCE (a local/global index — a stable value), not a physical register.
	// Invalidated at any flush (call/control boundary), memory.grow, and a set of
	// the source. Currently count-only via stats (measurement; no codegen change).
	bcKind   uint8  // 0 none, 1 local, 2 global
	bcIdx    uint32 // address source index
	bcExtent int32  // max off+size proven in-bounds on that source

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

	// moduleGlobal[g] marks g as MODULE-pinned (WARP's model): every function in
	// the module holds g's live value in the SAME reserved register, making it a
	// whole-module invariant like RBX/linMem — register-ABI calls and returns
	// carry no spill/reload for it at all. The cell is synced only at the
	// wasm↔native boundary (offset-0 prologues/epilogues, adapter exit, trap
	// stubs) and around wrapper-ABI calls (whose callee's offset-0 prologue
	// reloads). This is what makes the AssemblyScript shadow-stack pointer
	// (touched in every function) free at call boundaries.
	moduleGlobal []bool

	// Control-flow state (Phase 3).
	ctrl        []ctrlFrame // open block/loop/if frames; ctrl[0] is the function frame
	unreachable bool        // in dead code after an unconditional branch/trap
	retSites    []int       // forward jmp sites that target the epilogue

	// brFoldSites are the Jcc rel32 offsets of empty-edge `Jcc over; JMP target`
	// br_if idioms, folded post-assembly into a single inverted Jcc (peephole.go).
	brFoldSites []int

	// Loop bounds-check hoisting (WAGO_LOOP_PRECHECK, boundshoist.go). elideBases
	// holds the loop-invariant address-source locals whose inline bounds check is
	// elided while the FAST version of a versioned loop body is being compiled
	// (nil otherwise). inVersionedLoop guards against nesting a versioned loop
	// inside another (v1 caps code growth at 2×).
	elideBases      map[uint32]bool
	inVersionedLoop bool

	// Call state (Phase 4).
	relocs []callReloc // CallRel32 sites to patch at module layout

	// Inlining (Phase 2 of auto-inlining, WAGO_INLINE). inlineTargets maps a
	// callee's GLOBAL function index to its splice info; when a `call` targets one,
	// callOp splices the callee's straight-line body instead of emitting a call.
	// inlineBase maps a spliced callee's global index to the base local index its
	// params+locals occupy in this caller's frame (reserved past f.nLocals, so the
	// prologue's zeroDeclaredLocals never touches them). localBase is added to every
	// local index while a splice runs, remapping the callee's locals onto that base;
	// it is 0 outside a splice.
	inlineTargets map[int]*inlineTarget
	inlineBase    map[int]int
	localBase     int
	// inlineRetFrame is the f.ctrl index of the synthetic block frame standing in
	// for an inlined control-flow callee's function boundary: `return` inside the
	// callee branches to it (not the real function frame). 0 when not inside such a
	// splice (the synthetic frame is always at index >= 1, above ctrl[0]=cfFunc).
	inlineRetFrame int

	// importBindings, when non-nil, resolves imported-function calls to host
	// (log-and-replay) or cross-instance (native context-swap) lowering. Set only
	// on the link-time recompile of a module with cross-instance imports.
	importBindings []ImportBinding

	// syncHostCalls is set when the module has any returning host import, so every
	// host call in the module uses the synchronous control frame (callHostSync)
	// rather than the async log — the two share offCustomCtx and must not both be
	// live. Computed once per module in compileFunc.
	syncHostCalls bool

	// trapSites[code] lists the branch sites (Jcc/Jmp rel32 placeholders) that
	// target this function's shared trap stub for `code`; emitTrapStubs emits the
	// stubs after the epilogue and patches them. See trapIf.
	trapSites map[uint32][]int

	// stats collects per-function codegen counters (docs/no-ir-plan.md P1). nil
	// unless the caller requested collection, in which case every counter method
	// is a no-op — the hot compile path is unaffected. See stats.go.
	stats *CodegenStats

	// Reused compile-time scratch for short-lived stack/type/register/label lists.
	// These slices must not be stored in ctrlFrame or other persistent metadata.
	tmpRoots  []*elem
	tmpTypes  []machineType
	tmpTypes2 []machineType
	tmpRegs   []Reg
	tmpSlots  []int
	tmpMoves  []regMove
	tmpLabels []uint32
}

func align16(n int) int { return (n + 15) &^ 15 }

func asmCapForBody(bodyLen int) int {
	// A direct lowering usually emits several native bytes per wasm byte. Reserve
	// enough for small/medium functions to avoid repeated encoder slice growth,
	// but clamp so a huge wasm body cannot force a huge speculative allocation.
	const (
		minAsmCap = 128
		maxAsmCap = 64 << 10
	)
	capHint := 64 + bodyLen*4
	if capHint < minAsmCap {
		return minAsmCap
	}
	if capHint > maxAsmCap {
		return maxAsmCap
	}
	return capHint
}

// scratch bundles the per-function compile buffers reused across all functions in
// one module compile. Every field is pure scratch that never outlives a
// function's compile — the emitted code is copied into the module buffer before
// the next function runs — so reset-and-reuse replaces per-function allocation.
// Compile is sequential, so a single scratch is shared safely.
type scratch struct {
	stack *stack     // the valent-block operand stack
	asm   *amd64.Asm // the x86-64 encoder byte buffer
}

func newScratch() *scratch {
	return &scratch{stack: newStackWithCap(defaultStackArenaCap), asm: &amd64.Asm{}}
}

func (sc *scratch) reset() {
	sc.stack.reset()
	sc.asm.B = sc.asm.B[:0]
}

// Frameless layout (WARP-style, RSP-relative). RBP is NOT a frame pointer — it is
// a general allocatable register — so the frame is a single `sub rsp,frameSize`
// with everything addressed at non-negative offsets from RSP, which stays put for
// the whole body (wrapper-call arg/result buffers reuse spill slots, so no
// transient SubRsp/AddRsp). Layout, low→high address from RSP:
//
//	[rsp+0] (spare) · [rsp+8] results ptr · locals · spill slots
//
// The trap cell pointer is NOT frame state: it lives in basedata
// ([linMem-offTrapCellPtr], installed once per entry by the runtime) since only
// the cold trap path reads it.
const (
	frameHdrBytes = 16 // spare + results ptr (keeps locals 16-aligned)
	frResultsOff  = 8  // results buffer pointer
)

func (f *fn) localOff(i int) int32 { return int32(frameHdrBytes + 8*f.localSlot[i]) }
func (f *fn) spillOff(k int) int32 { return int32(frameHdrBytes + 8*f.nLocalSlots + 8*k) }

// frameSize is biased to ≡ 8 (mod 16): the function is entered with RSP ≡ 8
// (mod 16) after the trampoline's CALL and there is no frame-pointer push to
// re-align, so `sub rsp,frameSize` must land the body on a 16-aligned RSP to keep
// our own call sites correctly aligned.
func (f *fn) frameSize() int {
	if f.frameElided {
		return 0 // frame never touched (all locals register-homed, no spills, no calls)
	}
	return align16(frameHdrBytes+8*f.nLocalSlots+8*f.maxSpill) + 8
}

// elideRegisterOnlyFrame drops the whole frame for a register-homed call-free
// reg-ABI leaf. Its frame reserves a header (unused by the register-returning
// internal entry) plus a slot per local and operand spill; when the function
// never spills (maxSpill==0) and every scalar local lives permanently in a
// register, none of those slots is ever addressed, so `sub/add rsp` adjust dead
// space. Being call-free, the 16-byte-alignment the frame provided for call sites
// is moot, so frameSize can go to 0 and the pair becomes `sub/add rsp,0`. Called
// after the body (maxSpill final); returns whether it elided.
func (f *fn) elideRegisterOnlyFrame() bool {
	if !smallFrameElideEnabled || !f.singleRegResult || f.usesCalls || f.maxSpill != 0 || len(f.localType) != f.nLocals {
		return false
	}
	if !f.allLocalsRegisterHomed() {
		return false
	}
	f.frameElided = true
	f.stats.peep("frame-adjust-elide")
	return true
}

// allLocalsRegisterHomed reports whether every local lives in a register for the
// whole activation (never uses its reserved frame slot). Only meaningful for
// call-free functions, where locals never leave their registers. A v128 local is
// copied through its frame slot in the prologue, so it disqualifies elision.
func (f *fn) allLocalsRegisterHomed() bool {
	if len(f.locals) < f.nLocals {
		return false
	}
	for i := 0; i < f.nLocals; i++ {
		if f.localType[i] == mtV128 || f.locals[i].reg == regNone {
			return false
		}
	}
	return true
}

// ImportBinding tells the compiler how an imported function is bound at link
// time, so a cross-instance call can be lowered to a native context-swap into
// the callee instance. The zero value (CrossInstance false) selects the default
// host-import log-and-replay lowering. When ImportBindings is supplied it is
// indexed by imported-function index.
type ImportBinding struct {
	CrossInstance bool
	CalleeLinMem  uint64 // callee instance's linear-memory base pointer
	CalleeEntry   uint64 // callee function's offset-0 (wrapper-ABI) entry pointer
}

// CompileOptions configures direct wasm-to-amd64 compilation.
type CompileOptions struct {
	// ElideBoundsChecks omits inline linear-memory bounds checks, relying on
	// a guard-page mapping + SIGSEGV handler (see runtime/sigtrap_linux_amd64.go).
	// EXPERIMENTAL: only sound when the memory is backed by runtime guard pages.
	ElideBoundsChecks bool

	// NoBoundsFacts disables P6.1 straight-line bounds-check elision (explicit
	// mode only; guard mode elides everything anyway). The WAGO_NO_BOUNDS_FACTS=1
	// env var forces the same globally; this is the per-compile override.
	NoBoundsFacts bool

	// ImportBindings, when non-nil, resolves imported functions to host or
	// cross-instance lowering (indexed by imported-function index). Used by the
	// link-time recompile that wires cross-instance calls; nil means every import
	// is a host import (the default single-pass compile).
	ImportBindings []ImportBinding

	// SyncHostCalls forces host imports through the synchronous host-call control
	// frame even if their wasm signatures are void/scalar. This is required for
	// non-legacy host bindings (HostFunc and reflected Go functions), which the
	// async log replay path cannot dispatch.
	SyncHostCalls bool

	// Interruptible emits context-cancellation polls at native function entries
	// and loop headers. A watcher writes TrapInterrupted to the invocation trap
	// cell; the poll observes it and takes the cold trap path, unwinding the whole
	// native call tree so a running wasm loop is cancelled within one iteration.
	Interruptible bool

	// Codegen carries injectable runtime/heap dependencies for future WasmGC
	// lowering. The current direct backend does not lower WasmGC opcodes yet, but
	// threading the option here lets that work use the same HeapABI as the IR
	// backend instead of hard-coding allocator or collector choices.
	Codegen codegen.Options

	// Stats, when non-nil, collects per-function codegen counters into it (the
	// "explain" dashboard, docs/no-ir-plan.md P1). Independent of WAGO_EXPLAIN,
	// which prints the same dump to stderr. nil = no collection, zero overhead.
	Stats *ModuleStats
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
	// P6.1 elision is on unless disabled per-compile (opts) or globally (env).
	boundsFacts := boundsFactsEnabled && !opts.NoBoundsFacts
	n := len(m.Code)
	relocs := make([][]callReloc, n)
	entry := make([]int, n)
	internalEntry := make([]int, n)
	importedFuncs := m.ImportedFuncCount()
	nGlobals := m.GlobalCount()
	allHints, globalScores, err := computeModuleHints(m, nGlobals, importedFuncs)
	if err != nil {
		return nil, fmt.Errorf("amd64: %w", err)
	}
	modGlobals := pickModuleGlobals(m, nGlobals, globalScores)
	// Stats collection is opt-in: an explicit sink (opts.Stats) or WAGO_EXPLAIN=1.
	// nil ms => st stays nil in the loop => zero-overhead counter no-ops.
	var ms *ModuleStats
	if opts.Stats != nil {
		ms = opts.Stats
	} else if explainEnabled {
		ms = &ModuleStats{}
	}
	if ms != nil {
		ms.Funcs = make([]*CodegenStats, n)
		ms.ModuleGlobalPins = moduleGlobalPinInfos(modGlobals)
		// Inline-candidate detection (report only; no codegen change yet). Failure
		// to analyze is non-fatal — it never blocks a compile.
		if rep, ierr := AnalyzeInlineCandidates(m); ierr == nil {
			ms.Inline = rep
		}
	}
	// Compile scratch reused across every function in the module. The operand
	// stack arena and the occurrence-tracking refs map are per-function scratch
	// that never outlives a function's compile, so resetting them (rather than
	// allocating fresh per function) removes the largest compile allocations.
	// Compile is sequential, so sharing one scratch is safe.
	// Auto-inlining (WAGO_INLINE): the straight-line leaf callees to splice at their
	// call sites, keyed by global func index. nil when inlining is disabled.
	inlineTargets := buildInlineTargets(m)
	sc := newScratch()
	var code []byte
	for i := range m.Code {
		hints := allHints[i]
		var st *CodegenStats
		if ms != nil {
			st = &CodegenStats{FuncIdx: i, Name: funcDisplayName(m, i, importedFuncs)}
			ms.Funcs[i] = st
		}
		fnCode, rl, internalOff, err := compileFunc(m, i, guardMode, boundsFacts, opts.Interruptible, modGlobals, hints, opts.ImportBindings, opts.SyncHostCalls, st, inlineTargets, sc)
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
	if explainEnabled && ms != nil {
		fmt.Fprint(os.Stderr, ms.String())
	}
	return &amd64.CompiledModule{Code: code, Entry: entry, InternalEntry: internalEntry}, nil
}

// moduleGlobalPinInfos converts the internal module-global pin assignments to the
// display form used by ModuleStats (register names instead of Reg values).
func moduleGlobalPinInfos(pins []moduleGlobalPin) []ModuleGlobalPinInfo {
	if len(pins) == 0 {
		return nil
	}
	out := make([]ModuleGlobalPinInfo, len(pins))
	for i, p := range pins {
		out[i] = ModuleGlobalPinInfo{Global: p.global, Reg: regName(p.reg)}
	}
	return out
}

// moduleGlobalPin is a module-wide global→register assignment (WARP's model).
type moduleGlobalPin struct {
	global uint32
	reg    Reg
}

// moduleGlobalRegs are the registers reserved for module-pinned globals, in
// assignment order. They are carved out of every function's pin pool and the
// allocator, like RBX (linMem) and R15 (memSize). Up to K of these are spent per
// module, chosen adaptively by pickModuleGlobals: the first is cheap, each extra
// one demands a much hotter global (it steals a pinned-local register from every
// function module-wide).
var moduleGlobalRegs = []Reg{R14, R13, R12}

// pickModuleGlobals aggregates loop-weighted global hotness across the whole
// module and assigns the top mutable int globals a module-wide register. The
// bar (an aggregate score of one loop-level use in several functions) keeps the
// reservation from costing pin-pool registers on modules that barely touch
// globals.
func computeFuncHints(m *wasm.Module, funcIdx int, nGlobals int, importedFuncs int) (funcHints, error) {
	ft, ok := m.LocalFuncType(funcIdx)
	if !ok {
		return funcHints{}, fmt.Errorf("unknown function type")
	}
	nLocals, err := countLocals(ft.Params, m.Code[funcIdx].Locals)
	if err != nil {
		return funcHints{}, err
	}
	return scanFuncBody(m.Code[funcIdx], nLocals, nGlobals, uint32(importedFuncs+funcIdx))
}

// computeModuleHints scans every function body ONCE, returning per-function hints
// plus the module-wide aggregated global scores. scanFuncBody already computes a
// per-function globalScore, and the module score for a global is just the sum of
// those across functions — so summing here removes a second full-body
// immediate-decoding pass per function (the standalone global-scores scan). The
// standalone computeModuleGlobalScores is retained as the parity oracle in tests.
func computeModuleHints(m *wasm.Module, nGlobals, importedFuncs int) ([]funcHints, []int64, error) {
	allHints := make([]funcHints, len(m.Code))
	var agg []int64
	if nGlobals > 0 && len(m.Code) > 0 {
		agg = make([]int64, nGlobals)
	}
	for i := range m.Code {
		h, err := computeFuncHints(m, i, nGlobals, importedFuncs)
		if err != nil {
			return nil, nil, fmt.Errorf("function %d hints: %w", i, err)
		}
		allHints[i] = h
		for g := 0; g < nGlobals && g < len(h.globalScore); g++ {
			agg[g] += h.globalScore[g]
		}
	}
	// Immutable local-table specialization for call_indirect (mirrors arm64):
	// with no function imports, a single private (non-exported, non-imported)
	// table, and no table-mutating op anywhere in the module, every non-null
	// table-0 entry is necessarily a same-module internal entry — so call_indirect
	// can skip the run-time home/tag fork, and (when the table is uniformly typed
	// or holds a single target) elide the type check or direct-call the target.
	immutableLocalTable := immutableLocalTableEnabled && importedFuncs == 0 &&
		m.ImportedTableCount() == 0 && len(m.Tables) == 1 && !moduleExportsTable(m)
	if immutableLocalTable {
		for i := range allHints {
			if allHints[i].mutatesTable {
				immutableLocalTable = false
				break
			}
		}
	}
	if immutableLocalTable {
		tableType, tableTyped := immutableLocalTableType(m)
		mono := immutableLocalTableTarget(m)
		for i := range allHints {
			allHints[i].immutableLocalTable = true
			allHints[i].immutableTableType = tableType
			allHints[i].immutableTableTyped = tableTyped
			allHints[i].monomorphicTarget = mono
		}
	}
	return allHints, agg, nil
}

// immutableLocalTableTarget returns the sole local function stored in table 0,
// or -1 when entries may name different functions (or use expression forms the
// narrow specialization does not prove). The immutable-table preconditions are
// checked by computeModuleHints before this helper is used.
func immutableLocalTableTarget(m *wasm.Module) int {
	target := -1
	// A table initializer prefills every slot with its default element, so that
	// target is also a possible non-null entry (active elements below override
	// individual slots). Fold it into the monomorphic set; a non-ref.func/-ref.null
	// initializer we cannot prove disqualifies the direct-call specialization.
	if len(m.Tables) == 1 && m.Tables[0].Init != nil {
		ee, err := wasm.ParseElementExpr(*m.Tables[0].Init)
		if err != nil {
			return -1
		}
		if !ee.Null {
			local := int(ee.FuncIndex) - m.ImportedFuncCount()
			if local < 0 || local >= len(m.Code) {
				return -1
			}
			target = local
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive {
			continue
		}
		if e.Mode.Table != 0 || e.Kind.Kind != wasm.ElemFuncs {
			return -1
		}
		for _, idx := range e.Kind.Funcs {
			local := int(idx) - m.ImportedFuncCount()
			if local < 0 || local >= len(m.Code) {
				return -1
			}
			if target < 0 {
				target = local
			} else if target != local {
				return -1
			}
		}
	}
	return target
}

func moduleExportsTable(m *wasm.Module) bool {
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternTable {
			return true
		}
	}
	return false
}

// immutableLocalTableType returns the shared structural type id of every table-0
// entry (and true) when the whole immutable table is uniformly typed, so the
// call_indirect type check can be elided. Returns (0, false) otherwise.
func immutableLocalTableType(m *wasm.Module) (uint32, bool) {
	if !immutableTableTypeEnabled || len(m.Tables) != 1 || m.Tables[0].Init != nil {
		return 0, false
	}
	var want uint32
	found := false
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive {
			continue // cannot reach the table without table.init, already excluded
		}
		if e.Mode.Table != 0 || e.Kind.Kind != wasm.ElemFuncs {
			return 0, false
		}
		for _, idx := range e.Kind.Funcs {
			ft, ok := m.FuncSignature(uint32(idx))
			if !ok {
				return 0, false
			}
			id := wasm.StructuralFuncTypeID(ft)
			if !found {
				want, found = id, true
			} else if id != want {
				return 0, false
			}
		}
	}
	return want, found
}

func computeModuleGlobalScores(m *wasm.Module, nGlobals int) ([]int64, error) {
	if nGlobals == 0 || len(m.Code) == 0 {
		return nil, nil
	}
	agg := make([]int64, nGlobals)
	for i := range m.Code {
		if err := scanFuncGlobalScores(m.Code[i], nGlobals, func(g uint32, score int64) {
			agg[g] += score
		}); err != nil {
			return nil, fmt.Errorf("function %d global scores: %w", i, err)
		}
	}
	return agg, nil
}

func pickModuleGlobals(m *wasm.Module, nGlobals int, agg []int64) []moduleGlobalPin {
	if nGlobals == 0 || len(m.Code) == 0 {
		return nil
	}
	type cand struct {
		g     int
		score int64
	}
	var cs []cand
	minScore := 3 * loopWeight(1)
	// A global must clear extraBar (much higher than minScore) to justify a
	// SECOND or THIRD module-wide register: each extra reservation removes a
	// pinned-local register from every function, so it only pays off for a global
	// accessed dramatically more than a typical hot local. Empirically this pins
	// json-as's burst globals (g2/g4/g25 = 4603/1350/737 → K=3) while keeping
	// blake-as at K=1 (its 2nd/3rd globals score only ~125/98).
	extraBar := 50 * loopWeight(1)
	for g := 0; g < nGlobals && g < len(agg); g++ {
		if agg[g] < minScore {
			continue
		}
		gt, ok := m.GlobalTypeByIndex(uint32(g))
		if !ok || !gt.Mutable || !isIntValType(wasm.GlobalValueType(gt)) {
			continue
		}
		cs = append(cs, cand{g, agg[g]})
	}
	sort.SliceStable(cs, func(a, b int) bool { return cs[a].score > cs[b].score })
	// K = number of module-wide registers to spend. auto (pinGlobalK<0) applies the
	// extraBar gate for the 2nd/3rd; WAGO_PIN_GLOBAL_K forces a fixed cap (0..3),
	// bypassing the gate — for A/B measuring the adaptive choice.
	limit := len(moduleGlobalRegs)
	if pinGlobalK >= 0 && pinGlobalK < limit {
		limit = pinGlobalK
	}
	var pins []moduleGlobalPin
	for k, c := range cs {
		if k >= limit {
			break
		}
		if pinGlobalK < 0 && k >= 1 && c.score < extraBar {
			break // auto: cs is score-descending, so no later candidate clears the bar
		}
		pins = append(pins, moduleGlobalPin{global: uint32(c.g), reg: moduleGlobalRegs[k]})
	}
	if debugModGlobals {
		fmt.Fprintf(os.Stderr, "wago: module-pinned globals (K=%d):", len(pins))
		for _, p := range pins {
			fmt.Fprintf(os.Stderr, " g%d→%s", p.global, regName(p.reg))
		}
		fmt.Fprintln(os.Stderr)
	}
	return pins
}

// regExhausted is the sentinel panic allocReg raises when the register file is
// fully blocked. compileFunc catches it and recompiles the function without local
// pinning (see compileFuncAttempt).
type regExhausted struct{}

// errRegExhausted is regExhausted surfaced as an error from a compile attempt, so
// compileFunc can distinguish a recoverable register-pressure failure (retry with
// pinning off) from a genuine compile error (propagate).
var errRegExhausted = errors.New("amd64: no register available to spill")

// compileFunc compiles one function, retrying with local pinning disabled if the
// first (pinned) attempt exhausts the register file. Pinning is a pure speed
// optimization, so the unpinned recompile is always correct.
func compileFunc(m *wasm.Module, funcIdx int, guardMode, boundsFacts, interruptible bool, modGlobals []moduleGlobalPin, hints funcHints, importBindings []ImportBinding, syncHostCalls bool, stats *CodegenStats, inlineTargets map[int]*inlineTarget, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
	code, relocs, internalOff, err = compileFuncAttempt(m, funcIdx, guardMode, boundsFacts, interruptible, modGlobals, hints, importBindings, syncHostCalls, stats, true, inlineTargets, sc)
	if errors.Is(err, errRegExhausted) {
		resetFuncStats(stats)
		code, relocs, internalOff, err = compileFuncAttempt(m, funcIdx, guardMode, boundsFacts, interruptible, modGlobals, hints, importBindings, syncHostCalls, stats, false, inlineTargets, sc)
		if err == nil {
			stats.setUnpinnedRetry()
		}
	}
	return
}

func compileFuncAttempt(m *wasm.Module, funcIdx int, guardMode, boundsFacts, interruptible bool, modGlobals []moduleGlobalPin, hints funcHints, importBindings []ImportBinding, syncHostCalls bool, stats *CodegenStats, pinLocals bool, inlineTargets map[int]*inlineTarget, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(regExhausted); ok {
				err = errRegExhausted // recoverable: caller retries with pinning off
				return
			}
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

	sc.reset()
	sc.asm.Grow(asmCapForBody(len(c.BodyBytes)))
	f := &fn{a: sc.asm, s: sc.stack, m: m, ft: ft, nParams: len(ft.Params), nLocals: nLocals, guardMode: guardMode, boundsFacts: boundsFacts, interruptible: interruptible, regMerge: regMergeEnabled, globalCellReg: regNone, memSizeReg: regNone, immutableLocalTable: hints.immutableLocalTable, immutableTableType: hints.immutableTableType, immutableTableTyped: hints.immutableTableTyped, monomorphicTarget: hints.monomorphicTarget, importBindings: importBindings, stats: stats}
	f.syncHostCalls = syncHostCalls || moduleUsesSyncHostCalls(m, importBindings)
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
	f.localSlot = make([]int, nLocals)
	for i, mt := range f.localType {
		f.localSlot[i] = f.nLocalSlots
		f.nLocalSlots += mt.stackSlots()
	}
	hasCall := hints.hasCall
	touchesMemory := hints.touchesMemory
	// Auto-inlining: collect the callees this caller will splice (before the pin
	// setup below, which the plan can influence). A spliced memory-touching callee
	// runs its linear-memory ops in THIS caller's frame, so fold it into
	// touchesMemory — otherwise the guard-page pin exclusion (which drops R9/R10/R11
	// from the pool for a memory-touching call-making function) would be skipped for
	// a caller whose own body never touched memory.
	inlinedCallees := collectInlinedCallees(c, inlineTargets)
	if inlinePlanTouchesMemory(inlinedCallees) {
		touchesMemory = true
	}
	// Call-free hint propagation through inlining: when every direct call gets
	// spliced away (and inline targets are call-free leaves, so they add no call of
	// their own), the caller makes no native call after inlining. Plan its pins and
	// frame as a call-free function — aggressive pins, STACK_REG spill model off.
	if inlineCallFreeHintsEnabled && hasCall && allCallsWillInline(c, inlineTargets) {
		hasCall = false
		f.stats.peep("all-calls-inlined")
	}
	regABI := regABIEnabled && sigFitsRegABI(ft)
	gpPool := gpPinPool(regABI, f.nParams, !hasCall)
	if f.memSizeReg != regNone {
		gpPool = withoutReg(gpPool, f.memSizeReg) // R15 is the module-wide memBytes cache
		f.reserved = f.reserved.add(f.memSizeReg)
	} else if guardMode && hasCall && touchesMemory {
		// Don't pin locals to the argument-staging registers R9/R10/R11 in a
		// memory-touching, call-making function under guard-page bounds. Guard mode
		// elides the inline bounds-check code, which shifts the register liveness
		// around a call's argument staging + linMem/trap setup; a pinned local in an
		// arg register is meant to be spill-managed by the STACK_REG model, but in
		// that guard-page window the staging runs out of free scratch and silently
		// corrupts the pinned value (the #144/sqlite-tokenizer register-pressure
		// class — the same one that motivated excluding RDI/RSI). Explicit bounds
		// keep the check code that preserves the arg registers here, so this is
		// guard-page-specific. Pinning is a pure speed optimization, so excluding
		// these registers only for this class is always correct. Observable repro:
		// num-bigint's to_str_radix panics ("assertion failed: digit_2 < big_base")
		// only under guard-page. Excluding R15 instead is NOT a fix: it pushes a pin
		// onto R9/R10/R11 for other modules (e.g. sqlite) and reintroduces the bug.
		gpPool = withoutReg(withoutReg(withoutReg(gpPool, R9), R10), R11)
	}
	for _, mg := range modGlobals {
		gpPool = withoutReg(gpPool, mg.reg) // module-pinned global registers
		f.reserved = f.reserved.add(mg.reg)
	}
	// Cap pins so the reserved scratch four (RAX/RDX/RCX/R8) always stay
	// allocatable — WARP's resScratchRegsGPR floor. Deeper pressure (nested
	// RHS-relocation hazards) degrades gracefully to spill slots via
	// allocRegOrNone's fallback in condenseBinary.
	maxPins := len(gpAlloc) - numScratchGP
	if f.memSizeReg != regNone {
		maxPins-- // R15 is reserved out of the allocatable file too
	}
	if len(gpPool) > maxPins {
		gpPool = gpPool[:maxPins]
	}
	// A pathologically register-heavy expression tree can pin its whole spine and
	// exhaust the file even under the scratch floor (condenseShift/condenseBinary
	// pin one register per nesting level). When that happens the first attempt
	// panics with errRegExhausted and compileFunc recompiles with pinLocals=false:
	// dropping every local/global VALUE pin frees the entire neutral file for
	// scratch. Pinning is a pure speed optimization, so the unpinned compile is
	// always correct.
	if !pinLocals {
		gpPool = nil
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
		globalScores = hints.globalScore
		if hasCall {
			globalElig = hints.globalElig
		}
	}
	f.installModuleGlobals(modGlobals)
	// Deeper FP local pinning: a call-free function extends the float pin pool past
	// the base XMM12-15 into XMM8-10 (see pinnedFLocalRegs) — with no calls those
	// operand registers are never clobbered.
	fpPinLimit := baseFPPins
	if extendedFPPinsEnabled && !hasCall {
		fpPinLimit = len(pinnedFLocalRegs)
	}
	f.assignPinnedLocals(hints.localScore, globalScores, globalElig, gpPool, fpPinLimit)
	if regABI && !hasCall && f.nParams > 4 {
		for i := range f.locals {
			if r := f.locals[i].reg; r == R9 || r == R10 || r == R11 {
				f.stats.peep("entry-arg-local-pin") // hot local kept in a free incoming-arg register
			}
		}
	}
	if f.pinnedLocalMask.has(RBP) {
		f.regMerge = false // RBP now holds a pinned local/global, so it can't be the merge register
	}
	// STACK_REG (lazy pinned-local spill) for every call-making function,
	// including memory-touching ones: dirty-only stores before a call, lazy reload
	// on the next read (WARP's model). #68 disabled this for memory functions as a
	// workaround; the actual root cause was the opElse merge edge skipping
	// reconcileLocals (fixed in control.go, TestExecIfElseLocalMerge).
	f.usesCalls = hasCall && !noStackReg
	// A call-free leaf extends the deepest checked stack by exactly one frame; the
	// fence's 256 KiB margin (runtime stackFenceMargin) absorbs that when the frame
	// is provably small. frameSize isn't known until after the body, so bound it:
	// spill slots never exceed the body's operand pushes (< one per body byte).
	f.skipFence = shouldSkipStackFence(hasCall, f.nLocalSlots, len(c.BodyBytes))
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
	f.lazyZero = hints.callsSelf && touchesMemory && len(c.BodyBytes) <= 192 && nLocals-len(ft.Params) <= 8
	f.storeForwardOK = linearStoreForwardEnabled && len(c.BodyBytes) <= 256 && nLocals <= 8

	// Auto-inlining: reserve each spliced callee's locals past f.nLocals (after all
	// nLocals-dependent setup above, so zeroDeclaredLocals/skipFence/lazyZero see the
	// caller's own locals only). Extends the frame's local arrays with unpinned
	// scratch; the splice at each call site binds/zeroes them.
	f.reserveInlineLocals(inlinedCallees, inlineTargets)

	if regABIEnabled && sigFitsRegABI(ft) {
		internalOff, err := f.emitRegABI(c)
		if err != nil {
			return nil, nil, 0, err
		}
		f.finalizeStats(len(f.a.B))
		return f.a.B, f.relocs, internalOff, nil
	}

	f.prologue()
	f.preloadFloatConsts(c.BodyBytes)
	if err := f.runBody(c); err != nil {
		return nil, nil, 0, err
	}
	f.epilogue()
	f.emitTrapStubs()
	f.finalizeBranchFolds()
	f.a.PatchU32(f.subRspAt, uint32(f.frameSize()))
	f.a.PatchU32(f.addRspAt, uint32(f.frameSize()))
	f.finalizeStats(len(f.a.B))
	return f.a.B, f.relocs, 0, nil
}

// finalizeStats fills the per-function size counters from final compiler state
// (no-op when collection is off). Per-event counters are incremented at their
// emission sites during the body.
func (f *fn) finalizeStats(codeLen int) {
	s := f.stats
	if s == nil {
		return
	}
	s.CodeBytes = codeLen
	s.FrameBytes = f.frameSize()
	s.MaxSpillSlots = f.maxSpill
}

// runBody opens the function control frame, lowers the body, and patches every
// return/br-to-function site to the (current) epilogue position.
func (f *fn) runBody(c *wasm.Func) error {
	resultTypes := typesOfVals(f.ft.Results)
	f.ctrl = []ctrlFrame{{kind: cfFunc, resultN: len(resultTypes), branchN: len(resultTypes), resultTypes: resultTypes}}
	if err := f.body(c.BodyBytes); err != nil {
		return err
	}
	for _, s := range f.retSites {
		f.a.PatchRel32(s, f.a.Len())
	}
	return nil
}

// assignPinnedLocals dedicates registers to the hottest integer locals (by the
// hotness scores). Locals with a zero score (the DecodeModule BodyBytes path or
// unused) are ordered by index, so byte-backed bodies fall back to first-N
// pinning.
// gpPinPool returns the registers available to hold pinned integer locals, in
// priority order (hottest local gets the first). The base is R12-R15; call-making
// functions also pin the arg-staging registers R9/R10/R11 and the block-merge
// register RBP, all spill-managed around calls by the STACK_REG model.
//
// RDI/RSI are deliberately NOT pinned. A call's linMem/trap setup clobbers them
// (they are not arg registers here — intArgRegs is RAX/RCX/RDX/R8/R9/R10/R11), and
// in a register-heavy function that both touches memory (which reserves R15,
// pushing pins onto RDI/RSI) and makes multi-arg calls, having a pinned local live
// in RDI/RSI on top of the arg-register pins over-subscribed the file: the call's
// arg-staging + setup ran out of free scratch and silently corrupted a pinned
// local's value. The observable repro is sqlite's tokenizer — every SQL keyword
// misreads as an identifier ("near \"SELECT\": syntax error") — while wazero runs
// the same module correctly. Excluding RDI/RSI removes the hazard; R9/R10/R11 pins
// (which the STACK_REG spill/reload does handle) stay. See TestSyncSQLiteQuery.
//
// R9/R10/R11 are still excluded in reg-ABI functions with >4 params (the internal
// entry's incoming args would collide with the prologue's arg→pinned moves). RBP
// costs the block-merge register (the caller drops regMerge). RAX/RCX/RDX/R8 always
// stay free for operand evaluation and the x86 fixed-role ops (div/shift/return);
// callHost's scratch also lives there.
func gpPinPool(regABI bool, nParams int, callFree bool) []Reg {
	pool := append([]Reg{}, pinnedLocalRegs...) // R12-R15
	if !regABI || nParams <= 4 {
		pool = append(pool, R9, R10, R11)
	} else if callFree && entryArgPinsEnabled {
		// Entry-argument pinning (ledger ARM64→AMD64): a call-free reg-ABI leaf
		// never clobbers its caller-saved argument registers (no calls), so the
		// incoming-arg registers past the parameter count are free to hold hot pins.
		// Only R9/R10/R11 qualify — RAX/RCX/RDX carry fixed x86 roles (mul/div/shift)
		// and R8 doubles as bulk-memory scratch. Using total nParams as the index is
		// conservative for mixed GP/FP signatures (it may add fewer than are actually
		// free, never a register that carries a parameter — no arg-homing cycle).
		for i := nParams; i < len(intArgRegs); i++ {
			if r := intArgRegs[i]; r == R9 || r == R10 || r == R11 {
				pool = append(pool, r)
			}
		}
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

func (f *fn) assignPinnedLocals(scores, globalScores []int64, globalElig []bool, gpPool []Reg, fpPinLimit int) {
	f.locals = make([]localDef, f.nLocals)
	for i := range f.locals {
		f.locals[i] = localDef{reg: regNone, typ: f.localType[i], state: lsReg}
	}
	// Module-pinned globals (installModuleGlobals) already occupy globalReg
	// entries; keep them and size for whichever view is larger.
	if len(f.globalReg) < len(globalScores) {
		gr := make([]Reg, len(globalScores))
		for i := range gr {
			gr[i] = regNone
		}
		copy(gr, f.globalReg)
		f.globalReg = gr
		gd := make([]bool, len(globalScores))
		copy(gd, f.globalDirty)
		f.globalDirty = gd
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
		if f.localType[i] == mtI32 || f.localType[i] == mtI64 {
			gp = append(gp, gpCand{idx: i, score: scores[i]})
		}
	}
	loopMin := loopWeight(1)
	for g := 0; g < len(globalScores); g++ {
		if globalScores[g] < loopMin || f.isModuleGlobal(g) {
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
		// The extended pool slots (beyond the R12-R15 base) only take locals that
		// are actually used (score > 0): pinning a cold local there costs prologue
		// and call-spill traffic for nothing. Zero-score candidates still fill the
		// base slots so byte-backed decoded bodies keep the first-N fallback.
		if k >= len(pinnedLocalRegs) && c.score == 0 {
			break
		}
		if c.global {
			f.globalReg[c.idx] = gpPool[k]
			f.stats.addPinnedGlobalValue()
		} else {
			f.locals[c.idx].reg = gpPool[k]
			f.stats.addPinnedLocal()
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
	if fpPinLimit > len(pinnedFLocalRegs) {
		fpPinLimit = len(pinnedFLocalRegs)
	}
	for k, i := range fc {
		if k >= fpPinLimit {
			break
		}
		if pinnedFLocalRegs[k] < 12 {
			f.stats.peep("deep-fp-local-pin") // extended (call-free) XMM8-10 pin
		}
		f.locals[i].reg = pinnedFLocalRegs[k]
		f.locals[i].isFloat = true
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(pinnedFLocalRegs[k])
		f.stats.addPinnedLocal()
	}
}

// derivePinnedGlobals loads each pinned global's cell pointer into its dedicated
// register, once, in the prologue (RBX = linMem must already be set). A no-op when
// no globals are pinned. Every later access reads/writes through the register.
func (f *fn) globalIs64(g int) bool {
	gt, _ := f.m.GlobalTypeByIndex(uint32(g))
	return wasm.EqualValType(wasm.GlobalValueType(gt), wasm.I64)
}

// installModuleGlobals records the module-wide global→register pins on this
// function (every function in the module shares the same assignment).
func (f *fn) installModuleGlobals(pins []moduleGlobalPin) {
	if len(pins) == 0 {
		return
	}
	nG := f.m.GlobalCount()
	if len(f.globalReg) < nG {
		gr := make([]Reg, nG)
		for i := range gr {
			gr[i] = regNone
		}
		copy(gr, f.globalReg)
		f.globalReg = gr
		gd := make([]bool, nG)
		copy(gd, f.globalDirty)
		f.globalDirty = gd
	}
	f.moduleGlobal = make([]bool, nG)
	for _, p := range pins {
		f.globalReg[p.global] = p.reg
		f.moduleGlobal[p.global] = true
	}
}

func (f *fn) isModuleGlobal(g int) bool {
	return f.moduleGlobal != nil && g < len(f.moduleGlobal) && f.moduleGlobal[g]
}

// deriveModuleGlobals / storeModuleGlobals sync the module-pinned globals with
// their cells at wasm↔native boundaries (offset-0 prologues and epilogues, the
// adapter's Go exit, trap stubs) and before wrapper-ABI calls (whose callee's
// offset-0 prologue reloads). Register-ABI calls and returns carry nothing.
// scratch must be a register safe to clobber at the call site.
func (f *fn) deriveModuleGlobals() {
	for g, reg := range f.globalReg {
		if reg == regNone || !f.isModuleGlobal(g) {
			continue
		}
		f.a.Load64(reg, RBX, -int32(abi.GlobalsPtrOffset))
		f.a.Load64(reg, reg, int32(g*8))
		if f.globalIs64(g) {
			f.a.Load64(reg, reg, 0)
		} else {
			f.a.Load32(reg, reg, 0)
		}
	}
}

func (f *fn) storeModuleGlobals(scratch Reg) {
	for g, reg := range f.globalReg {
		if reg == regNone || !f.isModuleGlobal(g) {
			continue
		}
		f.a.Load64(scratch, RBX, -int32(abi.GlobalsPtrOffset))
		f.a.Load64(scratch, scratch, int32(g*8))
		if f.globalIs64(g) {
			f.a.Store64(scratch, 0, reg)
		} else {
			f.a.Store32(scratch, 0, reg)
		}
	}
}

// derivePinnedGlobals loads each value-pinned global's current value into its
// register from memory (base → &cell → value, reusing the register for the chain).
// Used in the prologue and to reload after a call (the callee may have changed the
// shared global). A no-op when no globals are pinned.
func (f *fn) derivePinnedGlobals() {
	for g, reg := range f.globalReg {
		if reg == regNone || f.isModuleGlobal(g) {
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
		if reg == regNone || f.isModuleGlobal(g) || (dirtyOnly && !f.globalDirty[g]) {
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
	a.Store64(RSP, frResultsOff, RCX) // results ptr (trap cell ptr lives in basedata)
	if f.memSizeReg != regNone {
		// Offset-0 entry: establish the module-wide memBytes cache. Direct wasm→wasm
		// register-ABI calls skip this (the caller's value is valid by construction).
		a.Load32(f.memSizeReg, RBX, -bdCurBytes)
	}
	f.emitStackFenceCheck(RBX, RAX)
	f.emitInterruptCheck(RAX) // RAX still free: params load below
	// Copy v128 params through XMM0 before loading any pinned scalar float params.
	// XMM0 is only a prologue scratch here; keeping these copies first prevents a
	// future pin-pool change from letting a later v128 copy clobber an already-live
	// scalar param register.
	paramOff := int32(0)
	for i, pt := range f.ft.Params {
		if f.localType[i] == mtV128 {
			a.VMovdquLoadDisp(0, RDI, paramOff)
			a.VMovdquStoreDisp(RSP, f.localOff(i), 0)
		}
		paramOff += abiValSize(pt)
	}
	rdiParamOff := int32(-1) // a param pinned in RDI must load LAST: RDI is the args base
	paramOff = 0
	for i, pt := range f.ft.Params {
		if f.localType[i] != mtV128 {
			if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
				if pr == RDI {
					rdiParamOff = paramOff
				} else {
					a.Load64(pr, RDI, paramOff) // pinned int param → its GP register
				}
			} else if ok && isFloat {
				a.FLoadDisp(pr, RDI, paramOff, f.localType[i] == mtF64) // pinned float param → XMM
			} else {
				a.Load64(RAX, RDI, paramOff)
				a.Store64(RSP, f.localOff(i), RAX)
			}
		}
		paramOff += abiValSize(pt)
	}
	if rdiParamOff >= 0 {
		a.Load64(RDI, RDI, rdiParamOff)
	}
	f.zeroDeclaredLocals()
	f.derivePinnedGlobals()
	f.deriveModuleGlobals() // offset-0 entry: cells → module-pinned registers
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
			} else if f.localType[i] == mtV128 {
				a.Store64(RSP, f.localOff(i), RAX)
				a.Store64(RSP, f.localOff(i)+8, RAX)
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
	if noStackFence || f.skipFence {
		return
	}
	f.a.Load64(scratch, linMemReg, -72)
	f.a.Cmp64(RSP, scratch)
	f.trapIf(condB, trapStackFence) // RSP below the fence → cold stub
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
	a.MovReg64(RBX, RSI) // linMem → RBX: the module-wide invariant the internal entry inherits
	if f.memSizeReg != regNone {
		// Offset-0 entry (from Go, or an indirect call): establish the module-wide
		// memBytes cache before the internal entry runs (which relies on it).
		a.Load32(f.memSizeReg, RBX, -bdCurBytes)
	}
	f.deriveModuleGlobals() // offset-0 entry: cells → module-pinned registers
	a.Push(RCX)             // results ptr (also keeps RSP 16-aligned at the internal call)
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
	adapterCall := a.CallRel32()
	a.Pop(RCX) // results
	if rN == 2 {
		// Two-int register return in RAX/RDX. Store both to the results buffer
		// BEFORE storeModuleGlobals, which uses RDX as scratch.
		a.Store64(RCX, 0, RAX)
		a.Store64(RCX, 8, RDX)
	}
	f.storeModuleGlobals(RDX) // Go exit: module-pinned registers → cells (RAX/RDX hold the result)
	if rN == 1 {
		rt := mtOf(f.ft.Results[0])
		if rt.isFloat() {
			a.FStoreDisp(RCX, 0, 0, rt == mtF64) // XMM0
		} else {
			a.Store64(RCX, 0, RAX)
		}
	}
	a.Ret()

	// Internal entry (frameless): RBX (linMem) is inherited from the caller —
	// every wasm function keeps it pinned, and the adapter establishes it at the
	// Go boundary — and the trap cell pointer lives in basedata, so the entry
	// carries no environment setup at all (WARP's model). Args in GP/XMM regs.
	a.Align16() // internal entries are hot call targets; align like function starts
	internalOff := a.Len()
	f.subRspAt = a.Len() + 3
	a.SubRsp(0)
	f.emitStackFenceCheck(RBX, RSI)
	f.emitInterruptCheck(RSI) // RSI is not an int-arg reg: free before args are homed
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
	f.preloadFloatConsts(c.BodyBytes)
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
	if rN == 2 {
		// Two-int register return: both results converged to slots 0,1. (Never
		// singleRegResult, which is one-result only.)
		a.Load64(RAX, RSP, f.spillOff(0)) // result 0 -> RAX
		a.Load64(RDX, RSP, f.spillOff(1)) // result 1 -> RDX
	}
	// singleRegResult: every exit already produced the result in RAX/XMM0.
	// No trap-slot protocol on return: the runtime zeroes the trap cell before
	// entry, and a trap never returns through here (handler-jump).
	f.addRspAt = a.Len() + 3
	a.AddRsp(0) // undo the frame; imm32 patched after body
	a.Ret()
	f.emitTrapStubs()
	f.finalizeBranchFolds()

	f.elideRegisterOnlyFrame() // register-homed call-free leaf → frameSize 0
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
	f.storeModuleGlobals(RDX)        // Go exit: module-pinned registers → cells
	a.Load64(RDI, RSP, frResultsOff) // results ptr
	resSlot := 0
	out := int32(0)
	for _, rt := range f.ft.Results {
		if mtOf(rt) == mtV128 {
			a.VMovdquLoadDisp(0, RSP, f.spillOff(resSlot))
			a.VMovdquStoreDisp(RDI, out, 0)
			resSlot += 2
		} else {
			a.Load64(RAX, RSP, f.spillOff(resSlot))
			a.Store64(RDI, out, RAX)
			resSlot++
		}
		out += abiValSize(rt)
	}
	f.addRspAt = a.Len() + 3
	a.AddRsp(0) // undo the frame; imm32 patched after body
	a.Ret()
}

func abiValOff(ts []wasm.ValType, idx int) int32 {
	off := int32(0)
	for i := 0; i < idx; i++ {
		off += abiValSize(ts[i])
	}
	return off
}

func abiValSize(t wasm.ValType) int32 {
	if wasm.EqualValType(t, wasm.V128) {
		return 16
	}
	return 8
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
	case wasm.EqualValType(t, wasm.V128):
		return mtV128
	case t.Kind == wasm.ValRef:
		return mtI64
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
