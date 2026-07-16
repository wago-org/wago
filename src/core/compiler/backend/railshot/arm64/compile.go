//go:build arm64

package arm64

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// regMergeEnabled turns on WARP-style register reconciliation of single-int-result
// block/if merges (docs/operand-stack-registers-plan.md) instead of the
// flush-to-slot + reload. Default ON (fib_rec −13.7%, json-as serialize −1.5%, no
// regressions; validated against the spec suite + full corpus differential).
// WAGO_REG_MERGE=0 restores the slot path — kept as the reference oracle for A/B.
var regMergeEnabled = os.Getenv("WAGO_REG_MERGE") != "0"

// loopRegionPinsEnabled gates the one-pass, call-free-loop local promotion
// experiment. It remains opt-in until candidate scoring proves it improves the
// corpus: WAGO_ARM64_LOOP_PINS=1 enables it.
var loopRegionPinsEnabled = os.Getenv("WAGO_ARM64_LOOP_PINS") == "1"

// uxtwAddEnabled gates folding i64.add(x, i64.extend_i32_u(y)) into a single
// UXTW extended-register add. On by default; WAGO_ARM64_NOUXTW=1 disables it for
// A/B measurement.
var uxtwAddEnabled = os.Getenv("WAGO_ARM64_NOUXTW") != "1"

// smallFrameAdjustEnabled replaces the fixed MOVZ+MOVK+SUB/ADD frame sequences
// with one immediate SP adjustment for the overwhelmingly common <=4095-byte
// frames. The reserved trailing words become NOPs so code offsets stay stable.
// WAGO_ARM64_NOSMALLFRAME=1 restores the wide uniform sequence for A/B checks.
var smallFrameAdjustEnabled = os.Getenv("WAGO_ARM64_NOSMALLFRAME") != "1"

// frameElideRegHomed extends frame elision to call-free leaves that keep extra
// locals (beyond params) permanently in registers — the reserved local slots are
// never touched, so the SUB/ADD SP pair is dead. Off restores the old
// preserveCallerPins-only gate for A/B and rollback checks.
var frameElideRegHomed = os.Getenv("WAGO_ARM64_NO_FRAME_ELIDE_REGHOMED") != "1"

// inlineCallFreeHintsEnabled lets frame/register planning use the post-inline
// fact that no native call remains. Disable only for A/B and rollback checks.
var inlineCallFreeHintsEnabled = os.Getenv("WAGO_ARM64_NO_INLINE_CALLFREE") != "1"

// immutableLocalTableEnabled specializes call_indirect when the one-pass module
// scan proves table 0 cannot change and every non-null entry is a same-module
// function. WAGO_ARM64_NO_IMMUTABLE_TABLE=1 restores the general home-tag fork.
var immutableLocalTableEnabled = os.Getenv("WAGO_ARM64_NO_IMMUTABLE_TABLE") != "1"

// immutableTableTypeEnabled removes call_indirect's dynamic type check only
// when every possible non-null entry in the immutable local table has one
// proven structural function type.
var immutableTableTypeEnabled = os.Getenv("WAGO_ARM64_NO_IMMUTABLE_TABLE_TYPE") != "1"

// linearStoreForwardEnabled keeps an owned full-width store value across a very
// short, side-effect-free local.get window and forwards an exact same-address
// load. WAGO_ARM64_NOMEMFWD=1 restores the load for A/B and rollback checks.
var linearStoreForwardEnabled = os.Getenv("WAGO_ARM64_NOMEMFWD") != "1"

// The switches below isolate register-lifetime changes while the ARM64 WIP is
// validated against high-pressure corpus functions. Each disabled path restores
// the immediately preceding code shape; keep them package-init constants so a
// parent/child corpus run can A/B one compiler mechanism per fresh process.
var (
	legacyGPPinsEnabled     = os.Getenv("WAGO_ARM64_LEGACY_GPPINS") == "1"
	legacyFPPinsEnabled     = os.Getenv("WAGO_ARM64_LEGACY_FPPINS") == "1"
	extendedFPPinsEnabled   = os.Getenv("WAGO_ARM64_NO_EXTFPPINS") != "1"
	deepFPPinsEnabled       = os.Getenv("WAGO_ARM64_NO_DEEP_FPPINS") != "1"
	threeOperandSinkEnabled = os.Getenv("WAGO_ARM64_NO3OPSINK") != "1"
	oldDestRHSSinkEnabled   = os.Getenv("WAGO_ARM64_NO_OLDDEST_RHS") != "1"
	callFreeX8PinEnabled    = os.Getenv("WAGO_ARM64_NO_X8PIN") != "1"
	leafScratchPinsEnabled  = os.Getenv("WAGO_ARM64_NO_LEAF_SCRATCH_PINS") != "1"
	entryArgPinsEnabled     = os.Getenv("WAGO_ARM64_NO_ENTRY_ARG_PINS") != "1"
	unaryLocalSinkEnabled   = os.Getenv("WAGO_ARM64_NOUNARYSINK") != "1"
	teeLocalSinkEnabled     = os.Getenv("WAGO_ARM64_NOTEESINK") != "1"
	// v128LocalSinkEnabled peeps `local.set/tee $x (v128op … (local.get $x) …)` for a
	// register-pinned v128 local $x and emits the single NEON op straight into $x's
	// pinned V register — the SIMD twin of tryFbinLocalSet. It removes both the
	// materializeV128 pre-copy of the accumulator and the setLocal result-to-pin
	// copy, leaving one in-place vector instruction. Inert unless v128 pins are on
	// (pinReg returns unpinned for v128 when WAGO_ARM64_NO_V128_PINS=1).
	v128LocalSinkEnabled = os.Getenv("WAGO_ARM64_NO_V128_SINK") != "1"
	// v128LocalPinsEnabled caches hot v128 locals in NEON V registers for the whole
	// function, exactly like the scalar-float pin pool. Restricted to CALL-FREE
	// functions: a wasm→wasm call only preserves the low 64 bits of the AAPCS64
	// callee-saved V range (and the STACK_REG spill helpers store 64-bit S/D), so a
	// 128-bit pin cannot survive a call. In a call-free function nothing clobbers the
	// register between the prologue init and the epilogue, so the full 128 bits stay
	// live and every local.get/set becomes a register op instead of LdrQ/StrQ.
	v128LocalPinsEnabled = os.Getenv("WAGO_ARM64_NO_V128_PINS") != "1"
	// v128ConstCacheEnabled reserves a V register for each repeated v128.const value
	// so its later uses copy (MOV.16b) instead of rebuilding the 128-bit immediate.
	// A pure codegen optimization independent of the pin/sink machinery; the kill
	// switch exists for A/B measurement and defensive fallback.
	v128ConstCacheEnabled = os.Getenv("WAGO_ARM64_NO_V128_CONST_CACHE") != "1"
)

// mergeReg is the canonical register a single-int-result block's value is
// reconciled into at every edge (fall-through, br, br_if, br_table) so the merge
// needs no slot round trip. X15 is a plain allocatable GPR (frameless backend),
// not a pinned-local (X19-X23) or fixed-role scratch — the arm64 analog of amd64's
// RBP merge register.
const mergeReg = X15

// mergeFReg is mergeReg's float counterpart: the canonical V register a single-
// float-result block/if is reconciled into. V15 is a freely-allocatable float
// temp, not a pinned-float-local (V8-V14).
const mergeFReg Reg = 15

// fn holds the per-function code-generation state — the port's equivalent of
// WARP's Compiler/backend working set. One is created per compiled function.
type fn struct {
	a  *a64.Asm // the (reused) AArch64 encoder
	s  *stack   // the valent-block operand stack
	sc *scratch // module-wide reusable compile scratch
	m  *wasm.Module
	ft *wasm.CompType // this function's signature
	transient

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
	// immutableLocalTable proves every non-null table-0 entry targets this module,
	// so call_indirect can enter it directly through the internal register ABI.
	immutableLocalTable bool
	immutableTableType  uint64
	immutableTableTyped bool
	monomorphicTarget   int
	// preserveCallerPins marks a simple register-ABI leaf whose internal entry
	// promises not to clobber the caller's pinned-local registers.  Direct callers
	// can then keep their hot locals live across the call.
	preserveCallerPins bool

	// Register occupancy: regUser[r] is the value elem currently resident in
	// physical register r, or nil if r is free. Only allocatable GPRs are tracked.
	// AArch64 has 31 GPRs (X0-X30), so the array is sized [32] (versus amd64's [16]).
	regUser [32]*elem
	// pinned[r] marks a register temporarily protected from spilling/allocation
	// (e.g. an operand being consumed by the current op).
	pinned regMask

	// Parallel V-register occupancy for float values (Phase 5).
	fregUser [32]*elem
	fpinned  regMask
	fconsts  []floatConstReg
	// vconsts caches repeated 128-bit v128.const values in reserved V registers
	// (like fconsts for scalar floats). A const that appears more than once in a
	// straight-line-reachable body — e.g. the loop-invariant constant reduced 16×
	// in the isa_simd_reduce corpus — is materialized once at entry, then each use
	// copies it with a single MOV.16b instead of rebuilding it (MovImm×2/FMOV/INS).
	vconsts []v128ConstReg

	maxSpill      int  // high-water number of operand spill slots used
	subRspAt      int  // byte offset of the prologue's frame-alloc MOVZ (patched with frameSize)
	addRspAt      int  // byte offset of the epilogue's frame-free MOVZ (patched with frameSize)
	frameElided   bool // simple register-only internal entry leaves SP unchanged
	guardMode     bool // elide inline bounds checks; rely on guard-page + SIGSEGV trap
	boundsFacts   bool // P6.1 straight-line bounds-check elision enabled (explicit mode)
	interruptible bool // emit context-cancellation polls at entries and loop headers
	lazyZero      bool // defer declared-local zeroing for small call+memory functions
	skipFence     bool // call-free leaf with a provably small frame: no stack-fence check

	// memSizeReg caches the linear-memory size in bytes ([linMemReg-bdCurBytes]) in a
	// dedicated register for the whole module (WARP's REGS::memSize=R27, which
	// reserves a register when bounds checks are on). regNone in guard mode or when
	// the module has no memory. wago's ABI keeps the wrapper-arg registers (X0-X3)
	// busy at every call boundary (trap/linMem/results), so X27 is used: it has no
	// fixed role, is AAPCS64 callee-saved, so it is preserved by construction across
	// wasm→wasm calls (reserved out of every pool module-wide), refreshed by
	// memory.grow, and established once at every offset-0 entry (wrapper prologue /
	// reg-ABI adapter — the only ways an activation enters from Go).
	memSizeReg Reg
	// reserved is the module-wide never-allocatable register set: memSizeReg and
	// the module-pinned global registers.
	reserved regMask
	// singleRegResult: this function uses the register-return ABI with exactly one
	// result. Its exits produce that result directly in the return register — X0
	// (int) or V0 (float) — via the WARP-style target hint, skipping the
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
	// whole-module invariant like linMemReg/linMem — register-ABI calls and returns
	// carry no spill/reload for it at all. The cell is synced only at the
	// wasm↔native boundary (offset-0 prologues/epilogues, adapter exit, trap
	// stubs) and around wrapper-ABI calls (whose callee's offset-0 prologue
	// reloads). This is what makes the AssemblyScript shadow-stack pointer
	// (touched in every function) free at call boundaries.
	moduleGlobal []bool

	// Control-flow state (Phase 3).
	ctrl                []ctrlFrame // open block/loop/if frames; ctrl[0] is the function frame
	branchHints         []wasm.BranchHint
	branchHintLocalDecl uint32
	branchHintUnlikely  bool
	// activeLoopPins is an O(1) index for pinReg: the loopPins of the one frame
	// that currently has any. Only a simple (call-free, non-nested) innermost loop
	// pins locals, so at most one frame's loopPins are live at a time — scanning
	// every ctrl frame per local access was O(depth) and dominated compilation of
	// deeply-nested functions (60% of esbuild's compile). Set on activateLoopPins,
	// cleared when that frame is popped in opEnd.
	activeLoopPins []loopPin
	unreachable    bool // in dead code after an unconditional branch/trap

	// Loop bounds-check hoisting (WAGO_LOOP_PRECHECK, boundshoist.go). elideBases
	// holds the loop-invariant address-source locals whose inline bounds check is
	// elided while the FAST version of a versioned loop body is being compiled
	// (nil otherwise). inVersionedLoop guards against nesting a versioned loop
	// inside another (v1 caps code growth at 2×).
	elideBases      map[uint32]bool
	inVersionedLoop bool

	// Call state (Phase 4).
	relocs []callReloc // direct-call (BL) sites to patch at module layout

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

	// importBindings selects imported-call lowering. Production compilation uses
	// Dynamic bindings so each call loads its wrapper target and contexts from the
	// per-instance dispatch table; immediate bindings remain for low-level tests.
	importBindings []ImportBinding

	// syncHostCalls is set when the module has any returning host import, so every
	// host call in the module uses the synchronous control frame (callHostSync)
	// rather than the async log — the two share offCustomCtx and must not both be
	// live. Computed once per module in compileFunc.
	syncHostCalls bool

	// stats collects per-function codegen counters (docs/no-ir-plan.md P1). nil
	// unless the caller requested collection, in which case every counter method
	// is a no-op — the hot compile path is unaffected. See stats.go.
	stats *CodegenStats

	// calleePreservesPins is computed once from module hints. Direct calls consult
	// it instead of rescanning and reallocating the callee's hint state per site.
	calleePreservesPins []bool

	// One-entry linear-memory store forwarding window. The value register is
	// protected in f.pinned until an exact load consumes it or any non-local.get
	// opcode invalidates it; address identity is deliberately limited to a local.
	storeFwd storeForward
	// Keep the extra protected register out of large/high-pressure functions.
	storeForwardOK bool
}

// transient is the per-function workspace handed back to module scratch after
// each compile. Embedding it keeps hot call sites terse while making ownership
// and lifetime a single assignment instead of a list of parallel fields.
type transient struct {
	lsPool      [][]locState
	endsPool    [][]int
	tmpRoots    []*elem
	tmpTypes    []machineType
	tmpTypes2   []machineType
	tmpRegs     []Reg
	tmpSlots    []int
	tmpMoves    []regMove
	tmpLabels   []uint32
	tmpDeferred []deferredArg
	tmpGpCand   []gpCand
	tmpInts     []int
	edgeScratch []byte
}

type storeForward struct {
	valid  bool
	reg    Reg
	typ    machineType
	local  int
	offset uint32
	size   int
}

type gpCand struct {
	global bool
	idx    int
	score  uint32
}

type deferredArg struct {
	target Reg
	root   *elem
}

var alignPad [16]byte

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
	stack *stack   // the valent-block operand stack
	asm   *a64.Asm // the AArch64 encoder byte buffer

	retSites      []int
	ctrl          []ctrlFrame
	trapSites     [trapStackFence + 1][]int
	branchTargets map[int]bool
	transient
}

// scratchState keeps low-level backend tests able to exercise an isolated fn.
// Production compilation always installs the module-owned scratch explicitly.
func (f *fn) scratchState() *scratch {
	if f.sc == nil {
		f.sc = &scratch{}
	}
	return f.sc
}

func newScratch() *scratch {
	return &scratch{stack: newStackWithCap(defaultStackArenaCap), asm: &a64.Asm{}}
}

func (sc *scratch) reset() {
	sc.stack.reset()
	sc.asm.B = sc.asm.B[:0]
	sc.retSites = sc.retSites[:0]
	sc.ctrl = sc.ctrl[:0]
	for i := range sc.trapSites {
		sc.trapSites[i] = sc.trapSites[i][:0]
	}
	clear(sc.branchTargets)
}

// workerState owns every mutable buffer used by one parallel compiler worker.
// arena is append-only until all workers join. Results retain offsets into it,
// never slices, because a later append may reallocate the arena.
type workerState struct {
	scratch *scratch
	arena   []byte
}

// funcResult is one independently compiled local function. worker/start/end
// identify its owned bytes after the worker pool joins; relocs is independently
// owned by the fn compiler state (it is not backed by scratch).
type funcResult struct {
	worker      int
	start       int
	end         int
	internalOff int
	relocs      []callReloc
	err         error
}

// Frameless layout (WARP-style, SP-relative). X29/FP is only a frame-record anchor
// in call-making functions (backtraces) — locals/spills are addressed at
// non-negative offsets from SP, which stays put for the whole body (wrapper-call
// arg/result buffers reuse spill slots, so no transient SUB/ADD SP). Layout,
// low→high address from SP:
//
//	[sp+0] (spare) · [sp+8] results ptr · locals · spill slots
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

// frameSize is a multiple of 16: AArch64 requires SP 16-byte aligned at all times.
// Unlike amd64 there is no "+8 bias" — a BL writes the return address into LR
// (nothing is pushed), so a call-making function's `STP X29,X30,[SP,#-16]!` keeps
// 16-alignment and `SUB SP,SP,#frameSize` must stay a multiple of 16 to preserve
// it for our own call sites.
func (f *fn) frameSize() int {
	if f.frameElided {
		return 0
	}
	return align16(frameHdrBytes + 8*f.nLocalSlots + 8*f.maxSpill)
}

func (f *fn) elideRegisterOnlyFrame() bool {
	if !f.singleRegResult || f.usesCalls || f.maxSpill != 0 || len(f.localType) != f.nLocals {
		return false
	}
	// The frame reserves slots for locals and operand spills. A call-free leaf with
	// no operand spills (maxSpill==0) keeps its locals permanently in registers, so
	// none of those slots is ever touched — the SUB/ADD SP pair is dead. Two ways to
	// prove the frame is untouched:
	//   1. preserveCallerPins: no locals beyond params, so no local slots at all.
	//   2. every local is register-homed (reg != regNone) and scalar: the register
	//      allocator never spills a call-free local to its frame slot, so the
	//      reserved slots stay dead even though nLocalSlots > 0.
	// A v128 local is copied through its frame slot in the prologue, so exclude it.
	if !f.preserveCallerPins && !(frameElideRegHomed && f.allLocalsRegisterHomed()) {
		return false
	}
	f.frameElided = true
	f.stats.peep("frame-adjust-elide")
	return true
}

// allLocalsRegisterHomed reports whether every local lives in a register for the
// whole activation (never uses its reserved frame slot). Only meaningful for
// call-free functions, where locals never leave their registers.
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

func (f *fn) patchFrameAdjusts() {
	size := f.frameSize()
	if smallFrameAdjustEnabled && size <= 4095 {
		const nop = 0xD503201F
		if size == 0 {
			f.a.PatchU32(f.subRspAt, nop)
			f.a.PatchU32(f.addRspAt, nop)
		} else {
			f.a.PatchU32(f.subRspAt, 0xD10003FF|uint32(size)<<10) // SUB SP,SP,#size
			f.a.PatchU32(f.addRspAt, 0x910003FF|uint32(size)<<10) // ADD SP,SP,#size
			f.stats.peep("small-frame-adjust")
		}
		for _, at := range []int{f.subRspAt, f.addRspAt} {
			f.a.PatchU32(at+4, nop)
			f.a.PatchU32(at+8, nop)
		}
		return
	}
	f.a.PatchMovImm(f.subRspAt, uint32(size))
	f.a.PatchMovImm(f.addRspAt, uint32(size))
}

// ImportBinding is shared by both Railshot architectures.
type ImportBinding = shared.ImportBinding

// CompileOptions configures direct wasm-to-arm64 compilation.
type CompileOptions struct {
	// Workers forces the maximum number of per-function compiler workers.
	// Values <= 1 retain the exact serial fast path. Values > 1 are capped by
	// runtime.GOMAXPROCS(0) and the module's local-function count.
	Workers int

	// ElideBoundsChecks omits inline linear-memory bounds checks, relying on
	// a guard-page mapping + SIGSEGV handler (see runtime/sigtrap_linux_arm64.go).
	// EXPERIMENTAL: only sound when the memory is backed by runtime guard pages.
	ElideBoundsChecks bool

	// NoBoundsFacts disables P6.1 straight-line bounds-check elision (explicit
	// mode only; guard mode elides everything anyway). The WAGO_NO_BOUNDS_FACTS=1
	// env var forces the same globally; this is the per-compile override.
	NoBoundsFacts bool

	// ImportBindings selects imported-function lowering by Wasm import index.
	// Dynamic bindings produce binding-independent code backed by the instance
	// dispatch table. nil retains the low-level legacy host-import path.
	ImportBindings []ImportBinding

	// SyncHostCalls forces host imports through the synchronous host-call control
	// frame even if their wasm signatures are void/scalar. This is required for
	// non-legacy host bindings (HostFunc and reflected Go functions), which the
	// async log replay path cannot dispatch.
	SyncHostCalls bool

	// Interruptible emits context-cancellation polls at native function entries
	// and loop headers. Public wago compilation enables it; low-level backend
	// callers may leave it off for the smallest standalone code.
	Interruptible bool

	// MemoryPressure is called once after retained native output reaches
	// MemoryPressureAt bytes. With a zero threshold it runs at seven-eighths of
	// the reserved output capacity. Public compilation uses that late checkpoint
	// for large modules to reclaim dead per-function state without changing global
	// GC configuration. A nil callback disables it.
	MemoryPressureAt int
	MemoryPressure   func()

	// GCStructHelpers is present for cross-target option parity. Staged admission
	// rejects it before arm64 code generation.
	GCStructHelpers bool

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

// DirectBackend adapts the direct wasm-to-arm64 compiler to the shared
// backend-neutral codegen.Backend shape used by heap/GC lowering work.
type DirectBackend struct{}

var _ codegen.Backend[*wasm.Module] = DirectBackend{}

func (DirectBackend) Name() string { return "arm64-direct" }

func (DirectBackend) CompileModule(m *wasm.Module, opts codegen.Options) (*codegen.Object, error) {
	cm, err := CompileModuleWith(m, CompileOptions{Codegen: opts})
	if err != nil {
		return nil, err
	}
	return &codegen.Object{Code: cm.Code, Entry: cm.Entry}, nil
}

// CompileModule compiles every local function into one executable blob with
// per-function entry offsets — the same shape src/core/encoder/arm64 produces, so
// src/wago consumes it unchanged. Phase 0: straight-line integer functions.
// CompileModule compiles with inline bounds checks (the safe default).
func CompileModule(m *wasm.Module) (*a64.CompiledModule, error) {
	return CompileModuleWith(m, CompileOptions{})
}

// CompileModuleWith compiles every local function. ElideBoundsChecks elides the
// inline linear-memory bounds check, relying on a guard-page mapping + SIGSEGV
// handler (the caller must back memory with runtime guard pages).
func CompileModuleWith(m *wasm.Module, opts CompileOptions) (*a64.CompiledModule, error) {
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
		return nil, fmt.Errorf("arm64: %w", err)
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
	inlineTargets := buildInlineTargets(m, allHints)
	calleePreservesPins := make([]bool, n)
	for i := range allHints {
		ft, ok := m.LocalFuncType(i)
		if ok {
			calleePreservesPins[i] = preservesCallerPins(ft, allHints[i].nLocals, allHints[i])
		}
	}
	// AArch64 lowering is close to four native bytes per Wasm opcode plus
	// adapters/alignment. Reserve once so large modules do not repeatedly copy the
	// accumulated native image as it grows.
	totalBody := 0
	for i := range m.Code {
		totalBody += len(m.Code[i].BodyBytes)
	}
	codeCap := shared.TaperedModuleCodeCapacity(totalBody, n, 32, 28, 768<<10)
	workers := shared.ResolveWorkers(opts.Workers, n, runtime.GOMAXPROCS(0))
	if workers <= 1 {
		// Keep the serial compiler as a distinct fast path: one reusable scratch,
		// no goroutines, channels, atomics, worker metadata, or intermediate arena.
		sc := newScratch()
		code := make([]byte, 0, codeCap)
		pressureDone := false
		pressureAt := shared.PressureThreshold(opts.MemoryPressureAt, cap(code))
		for i := range m.Code {
			hints := allHints[i]
			var st *CodegenStats
			if ms != nil {
				st = &CodegenStats{FuncIdx: i, Name: funcDisplayName(m, i, importedFuncs)}
				ms.Funcs[i] = st
			}
			fnCode, rl, internalOff, err := compileFunc(m, i, guardMode, boundsFacts, opts.Interruptible, modGlobals, hints, opts.ImportBindings, opts.SyncHostCalls, st, inlineTargets, calleePreservesPins, sc)
			allHints[i] = funcHints{}
			if err != nil {
				return nil, fmt.Errorf("arm64: function %d: %w", i, err)
			}
			if pad := (16 - len(code)%16) % 16; pad != 0 {
				code = append(code, alignPad[:pad]...)
			}
			entry[i] = len(code)
			internalEntry[i] = len(code) + internalOff
			relocs[i] = rl
			code = append(code, fnCode...)
			if !pressureDone && opts.MemoryPressure != nil && len(code) >= pressureAt {
				pressureDone = true
				opts.MemoryPressure()
			}
		}
		asm := &a64.Asm{B: code}
		for i := 0; i < n; i++ {
			for _, rl := range relocs[i] {
				site := entry[i] + rl.at
				target := entry[rl.target]
				if rl.internal {
					target = internalEntry[rl.target]
				}
				asm.PatchBranch26(site, target)
			}
		}
		code = asm.B
		if explainEnabled && ms != nil {
			fmt.Fprint(os.Stderr, ms.String())
		}
		return &a64.CompiledModule{Code: code, Entry: entry, InternalEntry: internalEntry}, nil
	}

	return compileModuleParallel(m, opts, workers, codeCap, entry, internalEntry, relocs, allHints, modGlobals, inlineTargets, calleePreservesPins, ms, guardMode, boundsFacts, importedFuncs)
}

// compileModuleParallel is split from CompileModuleWith so the goroutine closure
// and its captured state cannot escape into or add allocations to the serial path.
func compileModuleParallel(m *wasm.Module, opts CompileOptions, workers, codeCap int, entry, internalEntry []int, relocs [][]callReloc, allHints []funcHints, modGlobals []moduleGlobalPin, inlineTargets map[int]*inlineTarget, calleePreservesPins []bool, ms *ModuleStats, guardMode, boundsFacts bool, importedFuncs int) (*a64.CompiledModule, error) {
	n := len(m.Code)
	if ms != nil {
		for i := range m.Code {
			ms.Funcs[i] = &CodegenStats{FuncIdx: i, Name: funcDisplayName(m, i, importedFuncs)}
		}
	}
	states := make([]workerState, workers)
	arenaCap := (codeCap + workers - 1) / workers
	pressureAt := shared.PressureThreshold(opts.MemoryPressureAt, codeCap)
	var pressureBytes atomic.Int64
	var pressureOnce sync.Once
	for i := range states {
		states[i] = workerState{scratch: newScratch(), arena: make([]byte, 0, arenaCap)}
	}
	results := make([]funcResult, n)
	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for workerID := range states {
		go func(workerID int) {
			defer wg.Done()
			ws := &states[workerID]
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				var st *CodegenStats
				if ms != nil {
					st = ms.Funcs[i]
				}
				fnCode, rl, internalOff, err := compileFunc(m, i, guardMode, boundsFacts, opts.Interruptible, modGlobals, allHints[i], opts.ImportBindings, opts.SyncHostCalls, st, inlineTargets, calleePreservesPins, ws.scratch)
				allHints[i] = funcHints{}
				if err != nil {
					results[i].err = err
					continue
				}
				start := len(ws.arena)
				ws.arena = append(ws.arena, fnCode...)
				results[i] = funcResult{worker: workerID, start: start, end: len(ws.arena), internalOff: internalOff, relocs: rl}
				if opts.MemoryPressure != nil && pressureBytes.Add(int64(len(fnCode))) >= int64(pressureAt) {
					pressureOnce.Do(opts.MemoryPressure)
				}
			}
		}(workerID)
	}
	wg.Wait()
	if i, err := firstFuncError(results); err != nil {
		return nil, fmt.Errorf("arm64: function %d: %w", i, err)
	}

	code := make([]byte, 0, codeCap)
	for i := range results {
		r := &results[i]
		if pad := (16 - len(code)%16) % 16; pad != 0 {
			code = append(code, alignPad[:pad]...)
		}
		entry[i] = len(code)
		internalEntry[i] = len(code) + r.internalOff
		relocs[i] = r.relocs
		code = append(code, states[r.worker].arena[r.start:r.end]...)
	}
	asm := &a64.Asm{B: code}
	for i := 0; i < n; i++ {
		for _, rl := range relocs[i] {
			site := entry[i] + rl.at
			target := entry[rl.target]
			if rl.internal {
				target = internalEntry[rl.target]
			}
			asm.PatchBranch26(site, target)
		}
	}
	code = asm.B
	if explainEnabled && ms != nil {
		fmt.Fprint(os.Stderr, ms.String())
	}
	return &a64.CompiledModule{Code: code, Entry: entry, InternalEntry: internalEntry}, nil
}

func firstFuncError(results []funcResult) (int, error) {
	return shared.FirstErrorIndex(len(results), func(i int) error { return results[i].err })
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
// allocator, like linMemReg (linMem) and X27 (memSize). Up to K of these are spent
// per module, chosen adaptively by pickModuleGlobals: the first is cheap, each
// extra one demands a much hotter global (it steals a pinned-local register from
// every function module-wide). X26 is unavailable because arm64 keeps linMem there
// to avoid clobbering Go's X28/g register while native code is running.
var moduleGlobalRegs = []Reg{X25, X24, X23}

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
	return scanFuncBody(m.Code[funcIdx], nLocals, nGlobals, uint32(importedFuncs+funcIdx), m.BranchHintsForFunc(uint32(importedFuncs+funcIdx)))
}

// computeModuleHints scans every function body ONCE, returning per-function hints
// plus the module-wide aggregated global scores. scanFuncBody already computes a
// per-function globalScore, and the module score for a global is just the sum of
// those across functions — so summing here removes a second full-body
// immediate-decoding pass per function (the standalone global-scores scan). The
// standalone computeModuleGlobalScores is retained as the parity oracle in tests.
func computeModuleHints(m *wasm.Module, nGlobals, importedFuncs int) ([]funcHints, []int64, error) {
	n := len(m.Code)
	allHints := make([]funcHints, n)
	localCounts := make([]int, n)
	totalLocals := 0
	for i := range m.Code {
		ft, ok := m.LocalFuncType(i)
		if !ok {
			return nil, nil, fmt.Errorf("function %d hints: unknown function type", i)
		}
		count, err := countLocals(ft.Params, m.Code[i].Locals)
		if err != nil {
			return nil, nil, fmt.Errorf("function %d hints: %w", i, err)
		}
		if count > int(^uint(0)>>1)-totalLocals {
			return nil, nil, fmt.Errorf("function hint locals overflow")
		}
		localCounts[i] = count
		totalLocals += count
	}
	if nGlobals > 0 && n > int(^uint(0)>>1)/nGlobals {
		return nil, nil, fmt.Errorf("function hint globals overflow")
	}
	localScores := make([]uint32, totalLocals)
	globalScores := make([]uint32, n*nGlobals)
	globalEligibility := make([]bool, n*nGlobals)
	eligibilityTracker := newGlobalEligibilityTracker(nGlobals)
	var agg []int64
	if nGlobals > 0 && n > 0 {
		agg = make([]int64, nGlobals)
	}
	localAt := 0
	for i := range m.Code {
		nLocals := localCounts[i]
		globalAt := i * nGlobals
		h := funcHintsWithStorage(localScores[localAt:localAt+nLocals], globalScores[globalAt:globalAt+nGlobals], globalEligibility[globalAt:globalAt+nGlobals])
		h.nLocals = nLocals
		var err error
		h, err = scanFuncBodyInto(m.Code[i], nLocals, nGlobals, uint32(importedFuncs+i), m.BranchHintsForFunc(uint32(importedFuncs+i)), h, &eligibilityTracker)
		if err != nil {
			return nil, nil, fmt.Errorf("function %d hints: %w", i, err)
		}
		localAt += nLocals
		allHints[i] = h
		for g := 0; g < nGlobals; g++ {
			agg[g] += int64(h.globalScore[g])
		}
	}
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

func immutableLocalTableType(m *wasm.Module) (uint64, bool) {
	if !immutableTableTypeEnabled || len(m.Tables) != 1 || m.Tables[0].Init != nil {
		return 0, false
	}
	var want uint64
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
			typeIdx, ok := m.FuncTypeIndex(uint32(idx))
			if !ok {
				return 0, false
			}
			key, ok := m.StructuralTypeKeyChecked(typeIdx.Index)
			if !ok {
				return 0, false
			}
			if !found {
				want, found = key, true
			} else if key != want {
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
	// A module pin is an ABI-wide reservation, not a function-local choice: every
	// function must preserve the register even when it never reads that global.
	// Demand enough aggregate reuse to amortize that opportunity cost for the
	// FIRST pin as well as later pins. Empirically this retains json-as's burst
	// globals (g2/g4/g25 = 4603/1350/737 -> K=3), while rejecting blake-as's
	// modest g11/g10/g8 candidates (133/125/98), where K=1 displaced a hot local
	// and made the compression loop about 5% slower.
	extraBar := 50 * loopWeight(1)
	minScore := extraBar
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
	if debugModGlobals {
		fmt.Fprint(os.Stderr, "wago: module-global candidates:")
		for _, c := range cs {
			fmt.Fprintf(os.Stderr, " g%d=%d", c.g, c.score)
		}
		fmt.Fprintln(os.Stderr)
	}
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
var errRegExhausted = errors.New("arm64: no register available to spill")

// compileFunc compiles one function, retrying with local pinning disabled if the
// first (pinned) attempt exhausts the register file. Pinning is a pure speed
// optimization, so the unpinned recompile is always correct.
func compileFunc(m *wasm.Module, funcIdx int, guardMode, boundsFacts, interruptible bool, modGlobals []moduleGlobalPin, hints funcHints, importBindings []ImportBinding, syncHostCalls bool, stats *CodegenStats, inlineTargets map[int]*inlineTarget, calleePreservesPins []bool, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
	code, relocs, internalOff, err = compileFuncAttempt(m, funcIdx, guardMode, boundsFacts, interruptible, modGlobals, hints, importBindings, syncHostCalls, stats, true, inlineTargets, calleePreservesPins, sc)
	if errors.Is(err, errRegExhausted) {
		resetFuncStats(stats)
		code, relocs, internalOff, err = compileFuncAttempt(m, funcIdx, guardMode, boundsFacts, interruptible, modGlobals, hints, importBindings, syncHostCalls, stats, false, inlineTargets, calleePreservesPins, sc)
		if err == nil {
			stats.setUnpinnedRetry()
		}
	}
	return
}

func compileFuncAttempt(m *wasm.Module, funcIdx int, guardMode, boundsFacts, interruptible bool, modGlobals []moduleGlobalPin, hints funcHints, importBindings []ImportBinding, syncHostCalls bool, stats *CodegenStats, pinLocals bool, inlineTargets map[int]*inlineTarget, calleePreservesPins []bool, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(regExhausted); ok {
				err = errRegExhausted // recoverable: caller retries with pinning off
				return
			}
			if os.Getenv("WAGO_DEBUG_PANIC") == "1" {
				panic(r)
			}
			err = fmt.Errorf("arm64: %v", r)
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
	sc.asm.DenseIdxDisp = hints.memOps >= 8
	sc.asm.Grow(asmCapForBody(len(c.BodyBytes)))
	f := &fn{a: sc.asm, s: sc.stack, sc: sc, m: m, ft: ft, transient: sc.transient, nParams: len(ft.Params), nLocals: nLocals, guardMode: guardMode, boundsFacts: boundsFacts, interruptible: interruptible, regMerge: regMergeEnabled, globalCellReg: regNone, memSizeReg: regNone, immutableLocalTable: hints.immutableLocalTable, immutableTableType: hints.immutableTableType, immutableTableTyped: hints.immutableTableTyped, monomorphicTarget: hints.monomorphicTarget, importBindings: importBindings, stats: stats, branchHints: m.BranchHintsForFunc(uint32(m.ImportedFuncCount() + funcIdx)), branchHintLocalDecl: c.LocalDeclBytes, calleePreservesPins: calleePreservesPins}
	defer func() {
		sc.ctrl = f.ctrl
		sc.transient = f.transient
	}()
	f.storeForwardOK = linearStoreForwardEnabled && len(c.BodyBytes) <= 256 && nLocals <= 8
	f.syncHostCalls = syncHostCalls || moduleUsesSyncHostCalls(m, importBindings)
	if !guardMode && len(m.Memories) > 0 {
		f.memSizeReg = X27 // explicit bounds: X27 = memBytes for the whole module
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
	// touchesMemory — otherwise the guard-page pin exclusion (which drops X9/X10/X11
	// from the pool for a memory-touching call-making function) would be skipped for
	// a caller whose own body never touched memory.
	inlinedCallees := collectInlinedCallees(c, inlineTargets)
	if inlineCallFreeHintsEnabled && hasCall && allCallsWillInline(c, inlineTargets) {
		hasCall = false
		f.stats.peep("all-calls-inlined")
	}
	if inlinePlanTouchesMemory(inlinedCallees) {
		touchesMemory = true
	}
	effectiveHints := hints
	effectiveHints.hasCall = hasCall
	f.preserveCallerPins = preservesCallerPins(ft, nLocals, effectiveHints)
	if f.preserveCallerPins {
		// Keep this leaf out of every register a direct caller may use for a
		// pinned local or merge value. Its parameters stay in X0..X7 below; all
		// temporary work uses the ordinary caller-clobbered allocation set.
		for _, r := range append(append([]Reg{}, pinnedLocalRegs...), X9, X10, X11, mergeReg) {
			f.reserved = f.reserved.add(r)
		}
	}
	regABI := regABIEnabled && sigFitsRegABI(ft)
	gpPool := gpPinPool(regABI, f.nParams, !hasCall)
	if leafScratchPinsEnabled && !hasCall {
		// X12/X13 are fixed only by loop-region promotion, and X14 only by
		// bulk/table helpers. A straight-line scalar leaf can spend them on three
		// additional hot locals while the normal allocator still retains seven
		// ordinary transient GPRs plus its two scratch-floor registers in the
		// largest current scalar leaf.
		if !hints.hasLoop {
			gpPool = append(gpPool, X12, X13)
		}
		if !hints.usesBulkMem && len(m.Tables) == 0 {
			gpPool = append(gpPool, X14)
		}
	}
	if entryArgPinsEnabled && regABI && !hasCall {
		gpPool = append(gpPool, X2, X3, X4, X5, X6, X7)
	}
	// The inline bulk-memory helpers use X9/X10/X11 as fixed dst/src/count
	// registers after canonicalizing the operand stack. They do not participate in
	// the general allocator, so assigning a local to one of those registers would
	// let memory.copy/fill silently overwrite live local state (fannkuch's dynamic
	// memory.copy turned its permutation loop into an infinite loop). The pre-scan
	// already records this exact class; reserve only the colliding helper registers
	// and retain the rest of the call-free pin pool.
	if hints.usesBulkMem {
		gpPool = withoutReg(withoutReg(withoutReg(gpPool, X9), X10), X11)
	}
	// Memory-touching call-makers with imports or tables retain the conservative
	// unpinned path: host/cross-instance/indirect setup has substantially wider
	// clobber and merge surfaces (the SQLite pressure regressions). A
	// table-free, import-free recursive function only crosses the same-module
	// register ABI, whose STACK_REG path explicitly spills dirty pins and lazily
	// recovers them. Keeping pins for that auditable class removes the dominant
	// local-slot traffic in recursive memory kernels such as memory_tree.
	safeMemoryCallPins := hints.callsSelf && m.ImportedFuncCount() == 0 && len(m.Tables) == 0
	if touchesMemory && hasCall && !safeMemoryCallPins {
		gpPool = nil
	}
	if f.memSizeReg != regNone {
		gpPool = withoutReg(gpPool, f.memSizeReg) // X27 is the module-wide memBytes cache
		f.reserved = f.reserved.add(f.memSizeReg)
	} else if guardMode && hasCall && touchesMemory {
		// Don't pin locals to the call-scratch registers X9/X10/X11 in a
		// memory-touching, call-making function under guard-page bounds. Guard mode
		// elides the inline bounds-check code, which shifts the register liveness
		// around a call's argument staging + linMem/trap setup; a pinned local in a
		// call-scratch register is meant to be spill-managed by the STACK_REG model,
		// but in that guard-page window the staging runs out of free scratch and
		// silently corrupts the pinned value (the #144/sqlite-tokenizer register-
		// pressure class — the same one that motivated excluding the wrapper-arg
		// registers). Explicit bounds keep the check code that preserves these
		// registers here, so this is guard-page-specific. Pinning is a pure speed
		// optimization, so excluding these registers only for this class is always
		// correct. Excluding X27 instead is NOT a fix: it pushes a pin onto
		// X9/X10/X11 for other modules and reintroduces the bug.
		gpPool = withoutReg(withoutReg(withoutReg(gpPool, X9), X10), X11)
	}
	for _, mg := range modGlobals {
		gpPool = withoutReg(gpPool, mg.reg) // module-pinned global registers
		f.reserved = f.reserved.add(mg.reg)
	}
	// Cap pins so the reserved scratch (X1/X0) always stay allocatable — WARP's
	// resScratchRegsGPR floor. Deeper pressure (nested RHS-relocation hazards)
	// degrades gracefully to spill slots via allocRegOrNone's fallback in
	// condenseBinary.
	maxPins := len(gpAlloc) - numScratchGP
	if f.memSizeReg != regNone {
		maxPins-- // X27 is reserved out of the allocatable file too
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
	var globalScores []uint32
	var globalElig []bool
	if regABI {
		globalScores = hints.globalScore
		if hasCall {
			globalElig = hints.globalElig
		}
	}
	f.installModuleGlobals(modGlobals)
	f.assignPinnedLocals(hints.localScore, globalScores, globalElig, gpPool, hasCall)
	for i := range f.locals {
		if r := f.locals[i].reg; r >= X2 && r <= X7 {
			f.stats.peep("entry-arg-local-pin")
		}
	}
	// A call-free register-ABI leaf can keep its integer parameters in the
	// incoming argument registers.  Unlike the normal X19..X23 local pins, those
	// registers are caller-clobbered, so this leaves the caller's pinned locals
	// intact across a hot direct call.  It also removes the otherwise redundant
	// internal-entry arg-to-local moves.  This is deliberately leaf-only: a
	// callee that itself makes a call must retain the normal callee-saved local
	// model for its own call boundaries.
	if f.preserveCallerPins {
		f.pinLeafRegABIIntParams()
	}
	if f.pinnedLocalMask.has(mergeReg) {
		f.regMerge = false // X15 now holds a pinned local/global, so it can't be the merge register
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
	f.singleRegResult = regABI && !touchesMemory && len(ft.Results) == 1
	if f.singleRegResult {
		rt := mtOf(ft.Results[0])
		f.resultFloat = rt.isFloat()
		f.resultF64 = rt == mtF64
	}
	f.lazyZero = hints.callsSelf && touchesMemory && len(c.BodyBytes) <= 192 && nLocals-len(ft.Params) <= 8

	// Auto-inlining: reserve each spliced callee's locals past f.nLocals (after all
	// nLocals-dependent setup above, so zeroDeclaredLocals/skipFence/lazyZero see the
	// caller's own locals only). Extends the frame's local arrays with unpinned
	// scratch; the splice at each call site binds/zeroes them.
	f.reserveInlineLocals(inlinedCallees, inlineTargets)

	if regABI {
		internalOff, err := f.emitRegABI(c)
		if err != nil {
			return nil, nil, 0, err
		}
		f.finalizePeepholes()
		f.finalizeStats(len(f.a.B))
		return f.a.B, f.relocs, internalOff, nil
	}

	f.prologue()
	f.preloadFloatConsts(c.BodyBytes)
	f.preloadV128Consts(c.BodyBytes)
	if err := f.runBody(c); err != nil {
		return nil, nil, 0, err
	}
	f.epilogue()
	f.emitTrapStubs()
	f.patchFrameAdjusts()
	f.finalizePeepholes()
	f.finalizeStats(len(f.a.B))
	return f.a.B, f.relocs, 0, nil
}

// preservesCallerPins identifies the deliberately narrow internal-call ABI
// variant used for hot, simple leaves. Such a function has no declared locals,
// calls, memory access, or global access; its integer parameters can stay in the
// incoming argument registers while every caller-pinned register is reserved.
// Consequently it cannot observe or modify caller state outside X0..X7/X16/X17.
func preservesCallerPins(ft *wasm.CompType, nLocals int, h funcHints) bool {
	if !sigFitsRegABI(ft) || !sigIsIntOnly(ft) || nLocals != len(ft.Params) || h.hasCall || h.touchesMemory {
		return false
	}
	for _, score := range h.globalScore {
		if score != 0 {
			return false
		}
	}
	return true
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
// return/br-to-function site to the current epilogue position.
func (f *fn) runBody(c *wasm.Func) error {
	resultTypes := typesOfVals(f.ft.Results)
	sc := f.scratchState()
	f.ctrl = append(sc.ctrl[:0], ctrlFrame{kind: cfFunc, resultN: len(resultTypes), branchN: len(resultTypes), resultTypes: resultTypes})
	if err := f.body(c.BodyBytes); err != nil {
		return err
	}
	for _, s := range sc.retSites {
		// return/br-to-function sites are unconditional B placeholders (imm26).
		f.a.PatchBranch26(s, f.a.Len())
	}
	return nil
}

// assignPinnedLocals dedicates registers to the hottest integer locals (by the
// hotness scores). Locals with a zero score (the DecodeModule BodyBytes path or
// unused) are ordered by index, so byte-backed bodies fall back to first-N
// pinning.
// gpPinPool returns the registers available to hold pinned integer locals, in
// priority order (hottest local gets the first). The base is X19-X23. Call-free
// functions may also use X24/X25: they are callee-saved across the native entry
// boundary and module-global pins are removed from this pool before assignment.
// Call-making functions deliberately exclude them from local pinning so their
// ABI and the existing STACK_REG convergence model stay unchanged.
//
// The wrapper-arg registers (X0-X3) are deliberately NOT pinned. A call's
// linMem/trap/results setup clobbers them (they are not the reg-ABI internal-entry
// arg registers — intArgRegs is X0-X7, but pins never land there anyway), and in a
// register-heavy function that both touches memory (which reserves X27, pushing
// pins onto the extended pool) and makes multi-arg calls, having a pinned local
// live in a call-clobbered register on top of the arg-register pins over-subscribed
// the file: the call's arg-staging + setup ran out of free scratch and silently
// corrupted a pinned local's value. The observable repro is sqlite's tokenizer —
// every SQL keyword misreads as an identifier ("near \"SELECT\": syntax error") —
// while wazero runs the same module correctly. Restricting pins to the callee-saved
// block + the STACK_REG-managed X9/X10/X11 removes the hazard. See TestSyncSQLiteQuery.
//
// X9/X10/X11 are still excluded in reg-ABI functions with >4 params (the internal
// entry's incoming args would collide with the prologue's arg→pinned moves). X15
// costs the block-merge register (the caller drops regMerge). X1/X0 always stay
// free for operand evaluation and the return register; callHost's scratch also
// lives in the caller-saved temporaries.
func gpPinPool(regABI bool, nParams int, callFree bool) []Reg {
	pool := append([]Reg{}, pinnedLocalRegs...) // X19-X23
	if callFree && !legacyGPPinsEnabled {
		pool = append(pool, X24, X25)
		// X8 is neither an internal integer argument (X0-X7) nor a fixed-role
		// backend scratch. A leaf can dedicate it to one more hot local without
		// any call-boundary save traffic.
		if callFreeX8PinEnabled {
			pool = append(pool, X8)
		}
	}
	if !regABI || nParams <= 4 {
		pool = append(pool, X9, X10, X11)
	}
	return append(pool, X15)
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

func (f *fn) assignPinnedLocals(scores, globalScores []uint32, globalElig []bool, gpPool []Reg, hasCall bool) {
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
	gp := f.tmpGpCand[:0]
	for i := 0; i < f.nLocals; i++ {
		if f.localType[i] == mtI32 || f.localType[i] == mtI64 {
			gp = append(gp, gpCand{idx: i, score: scores[i]})
		}
	}
	loopMin := uint32(loopWeight(1))
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
	slices.SortFunc(gp, func(a, b gpCand) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		if a.global != b.global {
			if !a.global {
				return -1 // tie: prefer a local (value) over a global (pointer)
			}
			return 1
		}
		return a.idx - b.idx
	})
	f.tmpGpCand = gp
	for k, c := range gp {
		if k >= len(gpPool) {
			break
		}
		// The extended pool slots (beyond the X19-X23 base) only take locals that
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
	// Float locals use the separate V pin pool. Call-free functions also pin hot
	// v128 locals here (same V registers, full 128-bit): a wasm→wasm call would only
	// preserve the low 64 bits, so a v128 pin is confined to the call-free class.
	pinV128 := v128LocalPinsEnabled && !hasCall
	fc := f.tmpInts[:0]
	for i := 0; i < f.nLocals; i++ {
		if f.localType[i].isFloat() || (pinV128 && f.localType[i] == mtV128) {
			fc = append(fc, i)
		}
	}
	slices.SortFunc(fc, func(a, b int) int {
		if scores[a] != scores[b] {
			return cmp.Compare(scores[b], scores[a])
		}
		return a - b
	})
	f.tmpInts = fc
	fpPinLimit := len(pinnedFLocalRegs)
	if legacyFPPinsEnabled && fpPinLimit > 4 {
		fpPinLimit = 4
	} else if !extendedFPPinsEnabled && fpPinLimit > basePinnedFLocalRegs {
		fpPinLimit = basePinnedFLocalRegs
	} else if !hasCall && fpPinLimit > callFreePinnedFLocalRegs {
		// Call-free numeric loops still need room for wide expression trees. Past
		// this point nbody loses more to transient-register pressure than it gains
		// from another local pin. Call-making raytrace has sparse call sites and a
		// much larger live-local set, so its existing STACK_REG path profitably uses
		// the full pool.
		fpPinLimit = callFreePinnedFLocalRegs
	} else if hasCall && fpPinLimit > 23 {
		floatParams := 0
		for _, pt := range f.ft.Params {
			if mtOf(pt).isFloat() {
				floatParams++
			}
		}
		// V4-V7 overlap incoming FP arguments 5-8. Until the FP prologue uses a
		// parallel mover, retain them as temporaries for that signature class.
		if !deepFPPinsEnabled || floatParams > 4 {
			fpPinLimit = 23
		}
	}
	for k, i := range fc {
		if k >= fpPinLimit {
			break
		}
		f.locals[i].reg = pinnedFLocalRegs[k]
		f.locals[i].isFloat = true
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(pinnedFLocalRegs[k])
		f.stats.addPinnedLocal()
	}
}

// pinLeafRegABIIntParams maps integer parameters of a call-free register-ABI
// function onto X0..X7, their incoming locations at the internal entry.  The
// normal local allocator may already have selected a callee-saved pin for the
// parameter; release that pin before installing the argument register.
func (f *fn) pinLeafRegABIIntParams() {
	gp := 0
	for i := 0; i < f.nParams; i++ {
		if f.localType[i].isFloat() {
			continue
		}
		if gp >= len(intArgRegs) {
			return
		}
		if old := f.locals[i].reg; old != regNone {
			f.pinnedLocalMask = f.pinnedLocalMask.remove(old)
		}
		f.locals[i].reg = intArgRegs[gp]
		f.pinnedLocalMask = f.pinnedLocalMask.add(intArgRegs[gp])
		gp++
	}
}

// derivePinnedGlobals loads each pinned global's cell pointer into its dedicated
// register, once, in the prologue (linMemReg = linMem must already be set). A no-op when
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
		f.ld64(reg, linMemReg, -int32(abi.GlobalsPtrOffset))
		f.ld64(reg, reg, int32(g*8))
		if f.globalIs64(g) {
			f.ld64(reg, reg, 0)
		} else {
			f.ld32(reg, reg, 0)
		}
	}
}

func (f *fn) storeModuleGlobals(scratch Reg) {
	for g, reg := range f.globalReg {
		if reg == regNone || !f.isModuleGlobal(g) {
			continue
		}
		f.ld64(scratch, linMemReg, -int32(abi.GlobalsPtrOffset))
		f.ld64(scratch, scratch, int32(g*8))
		if f.globalIs64(g) {
			f.st64(scratch, 0, reg)
		} else {
			f.st32(scratch, 0, reg)
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
		f.ld64(reg, linMemReg, -int32(abi.GlobalsPtrOffset)) // globals array base
		f.ld64(reg, reg, int32(g*8))                         // &cell[g]
		if f.globalIs64(g) {
			f.ld64(reg, reg, 0)
		} else {
			f.ld32(reg, reg, 0) // i32: low half, zero-extended
		}
	}
}

// storePinnedGlobals writes value-pinned globals' registers back to their memory
// cells. dirtyOnly (epilogue) writes only the globals this function actually set;
// the call path (dirtyOnly=false) writes all of them before a call so the callee
// observes the current value. Avoids X0 (the int result register) for the
// cell-address scratch.
func (f *fn) storePinnedGlobals(dirtyOnly bool) {
	for g, reg := range f.globalReg {
		if reg == regNone || f.isModuleGlobal(g) || (dirtyOnly && !f.globalDirty[g]) {
			continue
		}
		t := f.allocReg(maskOf(reg, X0))
		f.ld64(t, linMemReg, -int32(abi.GlobalsPtrOffset))
		f.ld64(t, t, int32(g*8))
		if f.globalIs64(g) {
			f.st64(t, 0, reg)
		} else {
			f.st32(t, 0, reg)
		}
		f.release(t)
	}
}

// prologue: frameless body — pin linMem in linMemReg (moved from X1 per WARP's
// convention), save FP/LR in call-making functions, reserve the frame with one
// `SUB SP,SP,#frameSize`, stash the results ptr in the SP-relative header, load
// params into their register or slot, zero declared locals.
func (f *fn) prologue() {
	a := f.a
	if f.usesCalls {
		a.StpPre(FP, LR, SP, -16) // save FP/LR frame record (BL clobbers LR)
		a.AddImm64(FP, SP, 0)     // MOV X29, SP — frame pointer for backtraces
	}
	// Frame reserve: a fixed MOVZ/MOVK X16 + `SUB SP,SP,X16` sequence whose two mov
	// immediates are patched with frameSize after the body (the SUB-imm form is only
	// 12 bits, so we materialize the size in the backend scratch X16 — uniform for
	// any frame size). See CONTRACT §4h option 1.
	f.subRspAt = a.Len()
	a.Movz64(X16, 0, 0)          // frame size lo 16 bits; patched after body
	a.Movk64(X16, 0, 1)          // frame size hi 16 bits
	a.SubSPReg(X16)              // SUB SP, SP, X16
	a.MovReg64(linMemReg, X1)    // linMem → linMemReg (pinned for the whole function)
	f.st64(SP, frResultsOff, X3) // results ptr (trap cell ptr lives in basedata)
	if f.memSizeReg != regNone {
		// Offset-0 entry: establish the module-wide memBytes cache. Direct wasm→wasm
		// register-ABI calls skip this (the caller's value is valid by construction).
		f.ld32(f.memSizeReg, linMemReg, -bdCurBytes)
	}
	f.emitStackFenceCheck(linMemReg, X16)
	f.emitInterruptCheck()
	// Copy v128 params through V0 before loading any pinned scalar float params.
	// V0 is only a prologue scratch here; keeping these copies first prevents a
	// future pin-pool change from letting a later v128 copy clobber an already-live
	// scalar param register. X0 is the serArgs base (wrapper-ABI arg 0).
	paramOff := int32(0)
	for i, pt := range f.ft.Params {
		if f.localType[i] == mtV128 {
			if pr, _, ok := f.pinReg(i); ok {
				a.VMovdquLoadDisp(pr, X0, paramOff) // pinned v128 param → its V register
			} else {
				a.VMovdquLoadDisp(0, X0, paramOff)
				a.VMovdquStoreDisp(SP, f.localOff(i), 0)
			}
		}
		paramOff += abiValSize(pt)
	}
	x0ParamOff := int32(-1) // a param pinned in X0 must load LAST: X0 is the args base
	paramOff = 0
	for i, pt := range f.ft.Params {
		if f.localType[i] != mtV128 {
			if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
				if pr == X0 {
					x0ParamOff = paramOff
				} else {
					f.ld64(pr, X0, paramOff) // pinned int param → its GP register
				}
			} else if ok && isFloat {
				a.FLoadDisp(pr, X0, paramOff, f.localType[i] == mtF64) // pinned float param → V reg
			} else {
				// X16 (backend scratch) is the copy temp: X0 is the serArgs base and
				// must stay live for the remaining param loads (amd64 used RAX here,
				// but on arm64 that role register aliases the args base).
				f.ld64(X16, X0, paramOff)
				f.st64(SP, f.localOff(i), X16)
			}
		}
		paramOff += abiValSize(pt)
	}
	if x0ParamOff >= 0 {
		f.ld64(X0, X0, x0ParamOff)
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
		// AArch64 has a zero register (XZR): store it directly, no scratch to clear.
		for i := f.nParams; i < f.nLocals; i++ {
			if pr, _, ok := f.pinReg(i); ok && f.localType[i] == mtV128 {
				a.NeonEor16b(pr, pr, pr) // zero the whole 128-bit pin register
			} else if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
				a.MovImm64(pr, 0)
			} else if ok && isFloat {
				a.FmovFromGpr(pr, ZR, false) // fmov d,xzr → 0.0
			} else if f.localType[i] == mtV128 {
				f.st64(SP, f.localOff(i), ZR)
				f.st64(SP, f.localOff(i)+8, ZR)
			} else {
				f.st64(SP, f.localOff(i), ZR)
			}
		}
		return
	}
	for i := f.nParams; i < f.nLocals; i++ {
		f.markDeclaredLocalZero(i)
	}
}

// emitStackFenceCheck traps (StackFence → "call stack exhausted") when SP has
// dropped below the fence stored at [linMem-72], turning unbounded recursion into
// a clean trap instead of a fault. A zero fence disables the check (SP > 0).
func (f *fn) emitStackFenceCheck(linMemReg, scratch Reg) {
	if noStackFence || f.skipFence {
		return
	}
	f.ld64(scratch, linMemReg, -72)
	f.a.CmpSP64(scratch)            // CMP SP, scratch (SP-in-Rn extended-register form)
	f.trapIf(condB, trapStackFence) // SP below the fence → cold stub
}

// emitRegABI emits a register-ABI function as [host adapter | internal entry].
// The adapter at offset 0 keeps the wrapper ABI working for exports/host calls;
// the internal entry takes args in GP/V registers and returns its single result
// in X0/V0, or two integer results in X0/X1.
// Returns the internal entry's offset within the function's code.
func (f *fn) emitRegABI(c *wasm.Func) (int, error) {
	a := f.a
	np, rN := f.nParams, len(f.ft.Results)

	// Host→internal adapter (offset 0): in X0=serArgs, X1=linMem, X2=trap,
	// X3=results; loads args into registers, calls the internal entry, stores the
	// register results.
	a.MovReg64(linMemReg, X1) // linMem → linMemReg: the module-wide invariant the internal entry inherits
	if f.memSizeReg != regNone {
		// Offset-0 entry (from Go, or an indirect call): establish the module-wide
		// memBytes cache before the internal entry runs (which relies on it).
		f.ld32(f.memSizeReg, linMemReg, -bdCurBytes)
	}
	f.deriveModuleGlobals()   // offset-0 entry: cells → module-pinned registers
	a.StpPre(LR, X3, SP, -16) // save LR (BL clobbers it) + results ptr; keeps SP 16-aligned
	gp, fp := 0, 0
	x0ArgOff := int32(-1) // the arg targeting X0 aliases the serArgs base: load it LAST
	for i := 0; i < np; i++ {
		mt := f.localType[i]
		if mt.isFloat() {
			a.FLoadDisp(fpArgRegs[fp], X0, int32(8*i), mt == mtF64)
			fp++
		} else {
			if intArgRegs[gp] == X0 {
				x0ArgOff = int32(8 * i)
			} else {
				f.ld64(intArgRegs[gp], X0, int32(8*i))
			}
			gp++
		}
	}
	if x0ArgOff >= 0 {
		f.ld64(X0, X0, x0ArgOff)
	}
	adapterCall := a.Bl()     // BL internal entry; patched below
	a.LdpPost(LR, X3, SP, 16) // restore LR + results ptr
	f.storeModuleGlobals(X2)  // Go exit: module-pinned registers → cells (X0 holds the result)
	if rN == 1 {
		rt := mtOf(f.ft.Results[0])
		if rt.isFloat() {
			a.FStoreDisp(X3, 0, 0, rt == mtF64) // V0
		} else {
			f.st64(X3, 0, X0)
		}
	} else if rN == 2 {
		f.st64(X3, 0, X0)
		f.st64(X3, 8, X1)
	}
	a.Ret()

	// Internal entry (frameless): linMemReg (linMem) is inherited from the caller —
	// every wasm function keeps it pinned, and the adapter establishes it at the
	// Go boundary — and the trap cell pointer lives in basedata, so the entry
	// carries no environment setup at all (WARP's model). Args in GP/V regs.
	a.Align16() // internal entries are hot call targets; align like function starts
	internalOff := a.Len()
	if f.usesCalls {
		a.StpPre(FP, LR, SP, -16) // save FP/LR frame record (BL clobbers LR)
		a.AddImm64(FP, SP, 0)     // MOV X29, SP
	}
	f.subRspAt = a.Len()
	a.Movz64(X16, 0, 0)
	a.Movk64(X16, 0, 1)
	a.SubSPReg(X16)
	// X16 (backend scratch) is the fence scratch: the reg-ABI args occupy X0-X7 at
	// entry, so an arg register cannot double as scratch here (amd64 used RSI, which
	// is not one of its arg registers).
	f.emitStackFenceCheck(linMemReg, X16)
	f.emitInterruptCheck()
	gp, fp = 0, 0
	moves := f.tmpMoves[:0]
	for i := 0; i < np; i++ {
		mt := f.localType[i]
		if mt.isFloat() {
			src := fpArgRegs[fp]
			if pr, isFloat, ok := f.pinReg(i); ok && isFloat {
				a.FMov(pr, src, mt == mtF64)
			} else {
				a.FStoreDisp(SP, f.localOff(i), src, mt == mtF64)
			}
			fp++
		} else if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
			if pr != intArgRegs[gp] {
				moves = append(moves, regMove{dst: pr, src: intArgRegs[gp]})
			}
		} else {
			f.st64(SP, f.localOff(i), intArgRegs[gp])
		}
		if !mt.isFloat() {
			gp++
		}
	}
	resolveRegMoves(moves,
		func(dst, src Reg) { a.MovReg64(dst, src) },
		func(x, y Reg) {
			a.MovReg64(X16, x)
			a.MovReg64(x, y)
			a.MovReg64(y, X16)
		})
	f.tmpMoves = moves[:0]
	f.zeroDeclaredLocals()
	f.preloadFloatConsts(c.BodyBytes)
	f.preloadV128Consts(c.BodyBytes)
	f.derivePinnedGlobals()
	if err := f.runBody(c); err != nil {
		return 0, err
	}
	f.storePinnedGlobals(true) // write dirty value-pinned globals back to their cells (all returns land here)
	if rN == 1 && !f.singleRegResult {
		rt := mtOf(f.ft.Results[0])
		if rt.isFloat() {
			a.FLoadDisp(0, SP, f.spillOff(0), rt == mtF64) // result -> V0
		} else {
			f.ld64(X0, SP, f.spillOff(0)) // result -> X0
		}
	} else if rN == 2 {
		f.ld64(X0, SP, f.spillOff(0))
		f.ld64(X1, SP, f.spillOff(1))
	}
	// singleRegResult: every exit already produced the result in X0/V0.
	// No trap-slot protocol on return: the runtime zeroes the trap cell before
	// entry, and a trap never returns through here (handler-jump).
	f.addRspAt = a.Len()
	a.Movz64(X16, 0, 0) // undo the frame; imm patched after body
	a.Movk64(X16, 0, 1)
	a.AddSPReg(X16)
	if f.usesCalls {
		a.LdpPost(FP, LR, SP, 16) // restore FP/LR
	}
	a.Ret()
	f.emitTrapStubs()

	f.elideRegisterOnlyFrame()
	f.patchFrameAdjusts()
	f.a.PatchBranch26(adapterCall, internalOff)
	return internalOff, nil
}

// epilogue: copy results from their canonical slots to the results buffer, restore
// FP/LR (call-making functions), and return. Every reaching path (fallthrough end,
// return, br to the function label) has already placed the results in slots
// [0, resultN).
func (f *fn) epilogue() {
	a := f.a
	f.storeModuleGlobals(X2)     // Go exit: module-pinned registers → cells
	f.ld64(X1, SP, frResultsOff) // results ptr (X1 is free at the epilogue)
	resSlot := 0
	out := int32(0)
	for _, rt := range f.ft.Results {
		if mtOf(rt) == mtV128 {
			a.VMovdquLoadDisp(0, SP, f.spillOff(resSlot))
			a.VMovdquStoreDisp(X1, out, 0)
			resSlot += 2
		} else {
			f.ld64(X0, SP, f.spillOff(resSlot))
			f.st64(X1, out, X0)
			resSlot++
		}
		out += abiValSize(rt)
	}
	f.addRspAt = a.Len()
	a.Movz64(X16, 0, 0) // undo the frame; imm patched after body
	a.Movk64(X16, 0, 1)
	a.AddSPReg(X16)
	if f.usesCalls {
		a.LdpPost(FP, LR, SP, 16) // restore FP/LR
	}
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
