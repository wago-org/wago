//go:build arm64

package arm64

import (
	"fmt"
	"os"

	"sync"
	"sync/atomic"

	"unsafe"

	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

// regMergeEnabled turns on WARP-style register reconciliation of single-int-result
// block/if merges instead of the
// flush-to-slot + reload. Default ON (fib_rec −13.7%, json-as serialize −1.5%, no
// regressions; validated against the spec suite + full corpus differential).
// WAGO_REG_MERGE=0 restores the slot path — kept as the reference oracle for A/B.
// regMergeEnabled carries a single scalar block/if result in the target merge
// register across converged edges. WAGO_REG_MERGE=0 is the legacy rollback;
// the architecture-specific name is the public A/B oracle going forward.
var regMergeEnabled = os.Getenv("WAGO_ARM64_NO_REG_MERGE") != "1" && os.Getenv("WAGO_REG_MERGE") != "0"

// directGoImportEnabled bypasses cross-instance marshaling for bound Go imports
// in bounded numeric functions. The dispatch tag remains an instance decision.
var directGoImportEnabled = os.Getenv("WAGO_ARM64_NO_DIRECT_GO_IMPORT") != "1"

// uxtwAddEnabled gates folding i64.add(x, i64.extend_i32_u(y)) into a single
// UXTW extended-register add. On by default; WAGO_ARM64_NOUXTW=1 disables it for
// A/B measurement.
var uxtwAddEnabled = os.Getenv("WAGO_ARM64_NOUXTW") != "1"

// valueFactsEnabled carries bounded upper-zero and boolean provenance on Valent
// nodes. WAGO_ARM64_NOPROVENANCE=1 retains the pre-facts path for A/B checks.
var valueFactsEnabled = os.Getenv("WAGO_ARM64_NOPROVENANCE") != "1"

// mergeNextUseEnabled avoids forward-edge local reloads when bounded lookahead
// proves the local is overwritten or dead before its next read. Loop and EH
// targets stay conservative. WAGO_ARM64_NO_MERGE_NEXT_USE=1 restores eager loads.
var mergeNextUseEnabled = os.Getenv("WAGO_ARM64_NO_MERGE_NEXT_USE") != "1"

// countedLoopLatchEnabled folds exact non-interruptible top-tested countdown
// loops onto their decrement flags. WAGO_ARM64_NO_COUNTED_LOOP_LATCH=1 keeps
// the ordinary header-test backedge.
var countedLoopLatchEnabled = os.Getenv("WAGO_ARM64_NO_COUNTED_LOOP_LATCH") != "1"

// callFreeLoopColdExitEnabled moves local-slot reconciliation from a conditional
// exit's hot fall-through into its taken edge when the exited loop cannot call.
// RuntimeConfig optimization selection restores eager edge reconciliation.
var callFreeLoopColdExitEnabled = os.Getenv("WAGO_ARM64_NO_CALLFREE_LOOP_COLD_EXIT") != "1"

// These two switches isolate the loop-header and nested-region halves of the
// call-free state experiment. They remain separate from cold-edge placement so
// each mechanism can be measured against the same generated control flow.
var callFreeLoopEntryEnabled = os.Getenv("WAGO_ARM64_NO_CALLFREE_LOOP_ENTRY") != "1"
var callFreeLoopRegionEnabled = os.Getenv("WAGO_ARM64_NO_CALLFREE_LOOP_REGION") != "1"

// weightedScalarMergeEnabled reserves the canonical merge register in
// call-free functions with loop-hot scalar result joins. It changes only the
// whole-function pin choice; structured-control convergence remains unchanged.
var weightedScalarMergeEnabled = os.Getenv("WAGO_ARM64_NO_WEIGHTED_SCALAR_MERGE") != "1"

// memcopyQPairsEnabled halves the load/store instruction count in the 32- and
// 64-byte dynamic memory.copy loops. WAGO_ARM64_NO_MEMCOPY_QPAIRS=1 restores
// independent Q-register loads/stores for differential testing.
var memcopyQPairsEnabled = os.Getenv("WAGO_ARM64_NO_MEMCOPY_QPAIRS") != "1"

// sharedAdaptersEnabled lets compact code replace byte-identical register-ABI
// host adapters with eight-byte function-local target thunks plus one cold
// module copy. WAGO_ARM64_NO_SHARED_ADAPTERS=1 retains adapter-tail sharing.
var sharedAdaptersEnabled = os.Getenv("WAGO_ARM64_NO_SHARED_ADAPTERS") != "1"

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

// entryParamPairsEnabled packs adjacent wrapper-ABI scalar parameter homes into
// pair loads/stores. Register-ABI entries retain scalar stores: their immediate
// scalar-reload workloads measured slower with STP on current Apple cores.
// The environment switch retains exact scalar lowering for A/B.
var entryParamPairsEnabled = os.Getenv("WAGO_ARM64_NO_ENTRY_PARAM_PAIRS") != "1"

// entryZeroPairsEnabled packs adjacent declared-local zero stores into one
// offset STP. The environment switch retains exact single-store lowering for A/B.
var entryZeroPairsEnabled = os.Getenv("WAGO_ARM64_NO_ENTRY_ZERO_PAIRS") != "1"

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
	extendedFPPinsEnabled   = os.Getenv("WAGO_ARM64_NO_EXTFPPINS") != "1"
	threeOperandSinkEnabled = os.Getenv("WAGO_ARM64_NO3OPSINK") != "1"
	oldDestRHSSinkEnabled   = os.Getenv("WAGO_ARM64_NO_OLDDEST_RHS") != "1"
	callFreeX8PinEnabled    = os.Getenv("WAGO_ARM64_NO_X8PIN") != "1"
	leafScratchPinsEnabled  = os.Getenv("WAGO_ARM64_NO_LEAF_SCRATCH_PINS") != "1"
	entryArgPinsEnabled     = os.Getenv("WAGO_ARM64_NO_ENTRY_ARG_PINS") != "1"
	unaryLocalSinkEnabled   = os.Getenv("WAGO_ARM64_NOUNARYSINK") != "1"
	teeLocalSinkEnabled     = os.Getenv("WAGO_ARM64_NOTEESINK") != "1"
	// v128LocalPinsEnabled caches hot v128 locals in NEON V registers for the whole
	// function, exactly like the scalar-float pin pool. Restricted to CALL-FREE
	// functions: a wasm→wasm call only preserves the low 64 bits of the AAPCS64
	// callee-saved V range (and the STACK_REG spill helpers store 64-bit S/D), so a
	// 128-bit pin cannot survive a call. In a call-free function nothing clobbers the
	// register between the prologue init and the epilogue, so the full 128 bits stay
	// live and every local.get/set becomes a register op instead of LdrQ/StrQ.
	v128LocalPinsEnabled = os.Getenv("WAGO_ARM64_NO_V128_PINS") != "1"
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

type functionRepresentationLimit uint8

const (
	functionRepresentationOK functionRepresentationLimit = iota
	functionRepresentationReturnSite
	functionRepresentationFrameEnd
	functionRepresentationCallReloc
	functionRepresentationBranchRange
	functionRepresentationADRRange
)

// fn holds the per-function code-generation state — the port's equivalent of
// WARP's Compiler/backend working set. One is created per compiled function.
type fn struct {
	cacheImportDispatch     bool
	sharedNativePins        bool
	cacheNativeImportTarget bool
	nativeImportRouter      int
	requiresLSE             bool // selected optional instructions in this function
	scalarSummary           shared.ScalarSummary

	// Physical frame access emitted by the shared lowerer, not logical state.
	scalarFrameUsed bool

	//lint:ignore U1000 debug-only fields; the ordinary placeholder is empty
	regallocFnState
	profileFnState
	a             *a64.Asm // the (reused) AArch64 encoder
	s             *stack   // the valent-block operand stack
	sc            *scratch // module-wide reusable compile scratch
	m             *wasm.Module
	ft            *wasm.CompType // this function's signature
	gcTypeLayouts []codegen.GCTypeLayout
	classifier    wasm.ModuleInstructionClassifier
	transient
	traceFuncIdx        uint32
	rootTraceFuncIdx    uint32
	tracePCBase         uint32
	wasmPC              uint32
	customInstructions  map[uint32]railcore.CustomInstruction
	representationLimit functionRepresentationLimit

	nParams     int
	nLocals     int           // params + declared locals
	localType   []machineType // per-local machine type
	localSlot   []uint32      // per-local frame slot in 8-byte units; v128 occupies two
	nLocalSlots int           // total local frame slots in 8-byte units

	// WARP-style per-local storage metadata. localType remains as the compact
	// type table used by existing lowering; locals holds the assigned register and
	// call-spill state for each local.
	locals           []localDef
	pinnedLocalMask  regMask
	fpinnedLocalMask regMask

	// Bounded straight-line local intervals. A nonzero last-get offset plus the
	// score marks eligibility; locals[x].reg exists only while the cache is live.
	intervalLast    []uint32
	intervalScore   []uint32
	intervalEvents  []intervalLocalEvent
	intervalHead    []uint32
	intervalOwner   [32]int
	intervalActive  int
	intervalCalls   bool
	intervalControl bool
	intervalNext    bool
	wideReuseScore  []uint32

	mixedTrapSources bool
	linkMemoryTraps  bool
	memoryTrapHead   uint32
	memoryTrapCount  uint32
	memoryTrapPC     uint32
	selectGroupEnd   int
	selectGroupSite  int
	selectGroupLocal int
	localWritten     uint64 // conservative lexical write history for the first 64 locals

	// WARP STACK_REG lazy-spill model for pinned locals in CALL-MAKING functions
	// (usesCalls). locals[i].state tracks whether the live value of pinned local i is
	// in its register (dirty), in both register+slot (clean), or only in its slot.
	// Call-free functions keep locals permanently in registers (locals[].state unused).
	makesCalls    bool // physical calls, independent of the local spill policy
	usesCalls     bool
	reuseOperands bool
	// controlBaseTypeN partitions the fixed function-result scratch: function
	// results occupy its prefix and open control-frame bases use the remaining tail.
	controlBaseTypeN uint8
	// localFactsEnabled admits assignment-version facts only in straight-line
	// bodies. Facts reuse an otherwise-unused byte in each localDef.
	localFactsEnabled bool
	// loadDefinedLocals marks the first 64 locals updated by loading through their
	// own prior value. Their explicit W-register address rename is a useful
	// dependency break on Apple ARM cores and is retained by memory lowering.
	loadDefinedLocals uint64
	// canonicalI32Params marks hot i32 parameters whose 64-bit local
	// representation is canonicalized once at entry (and on every assignment).
	// This lets repeated address uses retain an upper-zero fact across control.
	canonicalI32Params uint64
	// memcopyTail4 bounds per-function native expansion for the four-byte
	// dynamic memory-copy tail. The lowering itself remains general within this cap.
	memcopyTail4 bool
	// memcopyQPairs selects paired Q-register loads/stores for the existing
	// dynamic memory-copy loops. Full-range bounds checks precede both forms.
	memcopyQPairs bool
	// floatLiteralPool selects one-instruction PC-relative loads for scalar float
	// constants that cannot use FMOV's immediate encoding. It is bounded to
	// ordinary-sized functions so every LDR literal remains within imm19 range.
	floatLiteralPool bool
	// suppressFloatLiteral is a one-operand lowering guard used for FDIV's
	// denominator, where an extra code-memory dependency is slower than overlapped
	// integer-pipeline materialization on Apple silicon.
	suppressFloatLiteral bool
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
	// intervalRegLimit is the target/mode-specific regional lease ceiling.
	intervalRegLimit int
	// pinned[r] marks a register temporarily protected from spilling/allocation
	// (e.g. an operand being consumed by the current op).
	pinned regMask

	// Parallel V-register occupancy for float values (Phase 5).
	fregUser         [32]*elem
	fpinned          regMask
	fconsts          []floatConstReg
	vconsts          []v128ConstReg
	iconsts          [4]intConstReg
	iconstN          uint8
	scopedConstHints scopedLoopConstHints

	maxSpill int // high-water number of operand spill slots used
	// spillFloor temporarily reserves a low spill-slot range while wide-stack
	// canonicalization stages values above both their old homes and destinations.
	spillFloor              int
	subRspAt                int   // byte offset of the prologue's frame-alloc MOVZ (patched with frameSize)
	addRspAt                int   // byte offset of the epilogue's frame-free MOVZ (patched with frameSize)
	tailFrameSites          []int // tail-transfer frame-free MOVZ sites (patched with frameSize)
	phasePadWords           int   // cold tail padding that preserves following function entry phases
	frameElided             bool  // simple register-only internal entry leaves SP unchanged
	adapterReturnOff        int   // register-ABI wrapper continuation used by cross-tail reuse
	adapterEndOff           int   // end of the wrapper before internal-entry alignment
	adapterReturnReferenced bool  // cross-tail reuse embeds the local return PC; keep that tail local
	trapBodyOff             int   // complete shared trap body start; zero when not emitted
	trapBodyEnd             int   // complete shared trap body end
	entryTrapEnd            int   // checks before value-pinned globals are initialized
	guardMode               bool  // elide inline bounds checks; rely on guard-page + SIGSEGV trap
	boundsFacts             bool  // P6.1 straight-line bounds-check elision enabled (explicit mode)
	interruptible           bool  // emit context-cancellation polls at entries and loop headers
	hasLoop                 bool  // finalizer preserves emission-time loop layout until alignment fragments are relaxable

	opaqueFragments bool // jump-table or plugin bytes require explicit fragment-aware scans
	lazyZero        bool // defer declared-local zeroing for small call+memory functions
	skipFence       bool // call-free leaf with a provably small frame: no stack-fence check

	// memSizeReg caches the ordinary linear-memory size in bytes, or the stable
	// backing base for shared memory when shared-memory-base is enabled, in a
	// dedicated register for the whole module (WARP's REGS::memSize=R27, which
	// reserves a register when bounds checks are on). regNone in guard mode or when
	// the module has no memory. wago's ABI keeps the wrapper-arg registers (X0-X3)
	// busy at every call boundary (trap/linMem/results), so X27 is used: it has no
	// fixed role, is AAPCS64 callee-saved, so it is preserved by construction across
	// wasm→wasm calls (reserved out of every pool module-wide), refreshed by
	// memory.grow, and established once at every offset-0 entry (wrapper prologue /
	// reg-ABI adapter — the only ways an activation enters from Go).
	memSizeReg Reg
	// memLimitReg caches memBytes-memLimitExtent in call-free explicit-bounds
	// functions. Exact matching accesses compare their canonical memory32 address
	// directly with this inclusive limit, removing the per-access end-address ADD.
	memLimitReg    Reg
	memLimitExtent int32
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

	// Straight-line bounds-check certificates (P6.1). Each entry records a stable
	// local/global address source and its largest checked extent. The fixed set
	// retains independent proofs for interleaved arrays without per-local state.
	boundsCerts    [8]boundsCert
	nextBoundsCert uint8

	// globalReg[g] value-pins hot mutable-int global g in a register for the whole
	// function, sharing the GP pin pool with hot locals (WARP's model). The value is
	// loaded once in the prologue and every access reads/writes the register directly
	// (no per-access memory traffic); dirty values are written back to the cell at the
	// epilogue. In call-making functions the value is spilled to / reloaded from the
	// cell around each internal call for coherence, so only globals accessed in a
	// CALL-FREE loop are pinned there (the spill/reload lands on out-of-loop calls).
	// regNone when g is not pinned. See globals.go / assignPinnedLocals.
	globalReg []Reg // high bit records a dirty value pin; physical registers are below 32

	// moduleGlobals is the bounded module-owned set of MODULE-pinned globals. Every
	// function holds each global's live value in the SAME reserved register, making it a
	// whole-module invariant like linMemReg/linMem — register-ABI calls and returns
	// carry no spill/reload for it at all. The cell is synced only at the
	// wasm↔native boundary (offset-0 prologues/epilogues, adapter exit, trap
	// stubs) and around wrapper-ABI calls (whose callee's offset-0 prologue
	// reloads). This is what makes the AssemblyScript shadow-stack pointer
	// (touched in every function) free at call boundaries.
	moduleGlobals []moduleGlobalPin
	// Control-flow state (Phase 3).
	ctrl                []ctrlFrame // open block/loop/if/try frames; ctrl[0] is the function frame
	ehTryDepth          int         // live reachable try_table records; bounded by ehTryCap
	ehRootCount         int         // fixed exception-root records assigned in this function
	ehTryCap            int         // this function's reserved try_table records (max nesting depth)
	ehRootCap           int         // this function's reserved exception roots (catch_ref clauses)
	branchHints         []wasm.BranchHint
	branchHintLocalDecl uint32
	branchHintUnlikely  bool
	innermostLoop       uint32 // control depth of the nearest loop; zero outside loops
	unreachable         bool   // in dead code after an unconditional branch/trap

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
	inlineTargets inlineTargetTable
	inlineBase    map[int]int
	localBase     int
	inlineDepth   int
	// inlineRetFrame is the f.ctrl index of the synthetic block frame standing in
	// for an inlined control-flow callee's function boundary: `return` inside the
	// callee branches to it (not the real function frame). 0 when not inside such a
	// splice (the synthetic frame is always at index >= 1, above ctrl[0]=cfFunc).
	inlineRetFrame int

	// importBindings selects imported-call lowering. Production compilation uses
	// Dynamic bindings so each call loads its wrapper target and contexts from the
	// per-instance dispatch table; immediate bindings remain for low-level tests.
	importBindings        []ImportBinding
	stagedTailDescriptors bool
	// adapterBacklink places the adapter backlink below the internal entry; see
	// emitRegABI. Set for modules that may tail-enter descriptors.
	adapterBacklink bool

	// syncHostCalls is set when the module has any returning host import, so every
	// host call in the module uses the synchronous control frame (callHostSync)
	// rather than the async log — the two share offCustomCtx and must not both be
	// live. Computed once per module in compileFunc.
	syncHostCalls             bool
	dynamicHostFast           bool
	syncHostSlots             int
	gcTypeSubtypingRefTest    bool
	nativeDeadArrayBytes      uint32
	nativeArrayHelper         uint32
	nativeArrayType           uint32
	nativeDeadStructType      uint32
	nativeDeadStructBatchType uint32
	nativeDeadArrayBatchBytes uint32
	gcReducedBounded          bool
	gcDeadBatchLayout         uint64
	gcI31Induction            bool
	gcStructHelpers           bool
	gcArrayHelpers            bool
	gcFrameRoots              *shared.GCFrameRootPlan
	gcCallsiteIndex           int
	moduleEH                  bool // reserve the handler register and fixed EH frame area
	compactFrameHeader        bool // register ABI: no wrapper results-pointer header

	// stats collects per-function codegen counters. nil
	// unless the caller requested collection, in which case every counter method
	// is a no-op — the hot compile path is unaffected. See stats.go.
	stats  *CodegenStats
	policy CodegenPolicy

	// calleeHints is the module's already-retained summary table. Direct calls
	// consult its preserves-caller-pins bit instead of retaining a second table.
	calleeHints []funcHints

	// One-entry linear-memory store forwarding window. The value register is
	// protected in f.pinned until an exact load consumes it or any non-local.get
	// opcode invalidates it; address identity is deliberately limited to a local.
	storeFwd storeForward
	// Keep the extra protected register out of large/high-pressure functions.
	storeForwardOK     bool
	sharedStoreForward bool
	// threadedMemory0 routes shared memory zero through the instance-owned memory
	// directory, leaving linMemReg's negative basedata private to the instance.
	threadedMemory0 bool
	// linearSumLoop encodes the exact reduction's address and accumulator locals;
	// its depth limits the state to the loop whose top test established it.
	linearSumLoop      uint32
	linearSumLoopDepth uint16
}

// transient is the per-function workspace retained in the worker's fnState.
// Embedding it keeps hot call sites terse; resetting fn preserves these headers
// so buffers can be reused without a duplicate set in module scratch.
type transient struct {
	inlineBasePool    map[int]int
	endsPool          [][]uint32
	tmpRoots          []*elem
	tmpTypes          []machineType
	tmpTypes2         []machineType
	tmpGCRoots        []bool
	tmpGCRoots2       []bool
	tmpGCOffsets      []uint32
	tmpFlushTypes     []machineType
	tmpRegs           []Reg
	tmpStackSlots     []uint32 // operand slot prefixes; successful native frames fit uint32 exactly
	tmpMoves          []regMove
	tmpLabels         []uint32
	tmpDeferred       []deferredArg
	loopSetLocals     []uint16
	edgeScratch       []byte
	tmpIntervalEvents []intervalLocalEvent
	tmpIntervalIndex  []uint32
	tmpWideTraffic    []uint32
	floatPool         []floatPoolConst
	floatPoolSites    []floatPoolSite
}

type storeForward struct {
	valid  bool
	reg    Reg
	typ    machineType
	local  int
	offset uint64
	size   int
}

type gpCand struct {
	score  uint32
	idx    uint32
	global bool
}

type deferredArg struct {
	target Reg
	root   *elem
}

var alignPad [32]byte

// scratch bundles the per-function compile buffers reused across all functions in
// one module compile. Every field is pure scratch that never outlives a
// function's compile — the emitted code is copied into the module buffer before
// the next function runs — so reset-and-reuse replaces per-function allocation.
// Compile is sequential, so a single scratch is shared safely.
type scratch struct {
	nativeImportFast bool // isolated guarded variant compilation only
	scalar           shared.ScalarState
	regMoves         regMoveScratch

	stack          *stack   // the valent-block operand stack
	stackCap       int      // deferred initial reservation for function-level fallback
	controlCap     int      // target control frames, reserved with the first fallback
	asm            *a64.Asm // the AArch64 encoder byte buffer
	fnState        fn       // per-function compiler state, reused across the module
	classifier     wasm.ModuleInstructionClassifier
	moduleTypes    moduleTypeCache
	directPrepared bool

	directPreparedBounded bool
	relocs                []callReloc

	retSiteHead             uint32 // word index+1; links live in unresolved B immediates
	ctrl                    []ctrlFrame
	ctrlMerges              []ctrlFrameMerge
	ctrlRoots               []ctrlFrameRoots
	functionResultTypeArena [maxScratchFunctionResults]machineType
	trapSites               [trapAtomicUnaligned + 1][]trapSite
	wideTrapSources         []trapSource
	branchTargets           []uint64
	branchTargetInline      [64]uint64   // one bit per instruction; covers 16 KiB of native code
	finalizerMarkers        map[int]bool // validation-only inventory
	brTableStubAt           []int        // duplicate-heavy jump-table target positions by control depth
	finalFragments          []finalizerFragment
	compact                 *compactScratch
	deadHoleN               uint8
	branchNextN             uint8
	singleBitTestN          uint8
	deadHoleOverflow        bool
	fragmentOverflow        bool
	hasBranchTargets        bool
	hasPCRelative           bool
	nodeScratchReserved     uint64
	nodeScratchPeak         uint64
	nodeScratchDiscarded    uint64
	controlScratchReserved  int
	controlScratchPeak      int
	controlScratchDiscarded int
	controlMergePeak        int
	controlMergeDiscarded   int
	controlRootsPeak        int
	controlRootsDiscarded   int
	adapterTemplate         adapterTemplateCache
	// adapterBacklink caches, for this scratch's single module, whether any
	// function has a dynamic call (call_ref/return_call_ref and friends).
	adapterBacklinkKnown bool
	adapterBacklink      bool
}

// Compact-only inventories are large fixed arrays. Ordinary compilation does
// not create deletion plans, so keep this storage out of every worker header.
type compactScratch struct {
	deadHoleSites   [maxFinalizerDeletions]int
	branchNextSites [maxFinalizerDeletions]int
	singleBitTests  [maxFinalizerDeletions]singleBitTestSite
	offsetMap       shared.OffsetMap
}

func (sc *scratch) ensureCompactScratch() *compactScratch {
	if sc.compact == nil {
		sc.compact = new(compactScratch)
	}
	return sc.compact
}

const maxCachedAdapterBytes = 256

// adapterTemplateCache retains one repeated host-adapter shape per worker.
// Function types alias immutable module storage, so pointer identity is an exact
// signature key within the scratch's single-module lifetime. The memory-size
// register is also part of the shape: bounded leaves can use X17 instead of X27.
// The first sighting only records a candidate; the second emits normally and
// promotes the complete
// patched adapter. This avoids copying unique shapes while turning longer runs
// into one bounded byte copy per function.
type adapterTemplateCache struct {
	candidate        *wasm.CompType
	typ              *wasm.CompType
	n                uint16
	returnOff        uint16
	endOff           uint16
	candidateMemSize Reg
	memSize          Reg
	code             [maxCachedAdapterBytes]byte
}

type trapSite struct {
	branch uint32
	source uint32
}

type trapSource struct {
	function uint32
	pc       uint32
}

func newScratch() *scratch {
	return newScratchWithStackCap(defaultStackArenaCap)
}

func newScratchWithStackCap(stackCap int) *scratch {
	stack := newStackWithCap(stackCap)
	_, reserved := stack.nodeMemory()
	return &scratch{stack: stack, asm: &a64.Asm{}, nodeScratchReserved: reserved, nodeScratchPeak: reserved}
}

func (sc *scratch) noteControlScratch() {
	if capacity := cap(sc.ctrl); capacity > sc.controlScratchPeak {
		sc.controlScratchPeak = capacity
	}
	if capacity := cap(sc.ctrlMerges); capacity > sc.controlMergePeak {
		sc.controlMergePeak = capacity
	}
	if capacity := cap(sc.ctrlRoots); capacity > sc.controlRootsPeak {
		sc.controlRootsPeak = capacity
	}
}

// finishControlWorker releases pointer-rich control frames before the parallel
// join allocates the final module image. No later phase reuses worker scratch.
func (sc *scratch) finishControlWorker() {
	if capacity := cap(sc.ctrl); capacity != 0 {
		clear(sc.ctrl[:capacity])
		sc.ctrl = nil
		sc.controlScratchDiscarded += capacity
	}
	if capacity := cap(sc.ctrlMerges); capacity != 0 {
		clear(sc.ctrlMerges[:capacity])
		sc.ctrlMerges = nil
		sc.controlMergeDiscarded += capacity
	}
	if capacity := cap(sc.ctrlRoots); capacity != 0 {
		clear(sc.ctrlRoots[:capacity])
		sc.ctrlRoots = nil
		sc.controlRootsDiscarded += capacity
	}
}

// maxScratchFunctionResults bounds owner-local signature lowering storage.
// Standard Go keeps 64 entries; TinyGo keeps none to protect release size.
// Signatures above the active bound allocate for that function only.
const maxScratchFunctionResults = shared.FunctionResultScratchCapacity

// workerStackArenaCap avoids multiplying one large function's initial arena by
// every parallel worker. Each worker keeps the established bounded growth path
// and allocates larger chunks only when it actually receives such a function.
func workerStackArenaCap(m *wasm.Module, hints []funcHints, inlineTargets inlineTargetTable, expandedLowering bool) int {
	capHint := serialStackArenaCap(m, hints, inlineTargets, expandedLowering)
	if capHint > defaultStackArenaCap {
		return defaultStackArenaCap
	}
	return capHint
}

const maxHintedControlFrames = 64

// maxWorkerInitialControlFrames bounds speculative pointer-rich control backing
// retained by every parallel worker. Deeper functions grow only the worker that
// receives them; append remains the correctness fallback.
const maxWorkerInitialControlFrames = 8

// Forward-edge overflow starts only after the inline sites. Bound both the
// number and size of recycled buffers so a deeply branched function cannot set
// persistent worker high-water for the rest of module compilation.
const (
	maxRetainedEndsBufs     = maxWorkerInitialControlFrames
	maxRetainedEndsBufSites = 256
)

func workerControlFrameCap(m *wasm.Module, hints []funcHints) int {
	capHint := moduleControlFrameCap(m, hints)
	if capHint > maxWorkerInitialControlFrames {
		return maxWorkerInitialControlFrames
	}
	return capHint
}

// clearNodeReferences severs scratch-owned links into operand-arena chunks before
// finishFunction drops over-budget chunk headers. Pointer-bearing slice backings are
// scanned to capacity by Go, so clearing only their current length would retain
// nodes from an earlier giant function.
func (sc *scratch) clearNodeReferences() {
	clear(sc.fnState.regUser[:])
	clear(sc.fnState.fregUser[:])
	clear(sc.fnState.tmpRoots[:cap(sc.fnState.tmpRoots)])
	clear(sc.fnState.tmpDeferred[:cap(sc.fnState.tmpDeferred)])
}

func (sc *scratch) finishStackFunction() {
	// Root retirement can grow independently of the node arena (for example a
	// wide control-flow rebuild). Bound that vector even for a small arena.
	if cap(sc.stack.detached) > 256 {
		sc.stack.detached = nil
	}
	_, reserved := sc.stack.nodeMemory()
	if reserved > sc.nodeScratchPeak {
		sc.nodeScratchPeak = reserved
	}
	if reserved <= shared.MaxRetainedStackArenaBytes {
		return
	}
	sc.clearNodeReferences()
	sc.nodeScratchDiscarded += sc.stack.finishFunction()
}

// finishStackWorker releases every pointer-rich node chunk after a parallel
// worker's final function. The join needs only worker code arenas and scalar
// metadata; operand nodes cannot be reused again.
func (sc *scratch) finishStackWorker() {
	sc.scalar.FinishWorker()

	sc.clearNodeReferences()
	_, retained := sc.stack.nodeMemory()
	sc.nodeScratchDiscarded += retained
	clear(sc.stack.cold[:cap(sc.stack.cold)])
	sc.stack.cold = nil
	sc.stack.chunks = nil
	sc.stack.head = nil
	sc.stack.cur = 0
}

// workerState owns every mutable buffer used by one parallel compiler worker.
// arena is append-only until all workers join. Results retain offsets into it,
// never slices, because a later append may reallocate the arena.
type workerState struct {
	// Sparse capabilities avoid enlarging every parallel function result.
	gcDeadBatchLayouts map[int]uint64
	gcI31Inductions    map[int]bool
	scratch            *scratch
	scratchStats       shared.WorkerScratchStats
	arena              []byte
	relocs             []callReloc
}

// funcResult is one independently compiled local function. worker/start/end
// identify its owned bytes after the worker pool joins. relocStart/relocEnd name
// the function's records in its worker's append-only relocation arena.
type funcResult struct {
	worker           uint32
	start            uint32
	end              uint32
	relocStart       uint32
	relocEnd         uint32
	bodyBytes        uint32
	internalOff      uint32
	adapterOff       uint32 // shared-adapter call or adapter-tail return offset
	adapterEnd       uint32
	trapBody         sharedTrapBodyInfo
	requiresLSE      bool
	layoutFlags      uint8
	scopedFloat      bool
	gcReducedBounded bool
}

func compactFuncResultRange(start, size int) (uint32, uint32, bool) {
	if start < 0 || size < 0 {
		return 0, 0, false
	}
	end := uint64(start) + uint64(size)
	if end > uint64(^uint32(0)) {
		return 0, 0, false
	}
	return uint32(start), uint32(end), true
}

func compactFuncResultValue(value int) (uint32, bool) {
	if value < 0 || uint64(value) > uint64(^uint32(0)) {
		return 0, false
	}
	return uint32(value), true
}

const (
	layoutHostAdapter uint8 = 1 << iota
	layoutHasLoop
	layoutHasCall
	layoutCallsSelf
	layoutDirectPrepared

	layoutDirectPreparedBounded
	layoutOmitted
)

func markDirectPrepared(bits []uint64, n, bit int) []uint64 {
	if bits == nil {
		bits = make([]uint64, (n+63)/64)
	}
	bits[bit>>6] |= uint64(1) << uint(bit&63)
	return bits
}

func directPreparedMarked(bits []uint64, bit int) bool {
	return bit >= 0 && bit>>6 < len(bits) && bits[bit>>6]&(uint64(1)<<uint(bit&63)) != 0
}

const (
	maxBoundedPreparedCallDepth = 32
	maxBoundedPreparedWorkBytes = 4 << 10
	// Register-entry candidates retain the tighter 96-byte compile-time cap.
	maxBoundedPreparedBodyBytes = 384
)

func immutablePreparedTableTargets(m *wasm.Module, table immutableTableHint) ([]uint64, bool) {
	if !table.local || len(m.Tables) != 1 {
		return nil, false
	}
	targets := make([]uint64, (len(m.Code)+63)/64)
	addExpr := func(expr wasm.Expr) bool {
		e, err := wasm.ParseElementExpr(expr)
		if err != nil {
			return false
		}
		if e.Null {
			return true
		}
		local := int(e.FuncIndex) - m.ImportedFuncCount()
		if local < 0 || local >= len(m.Code) {
			return false
		}
		targets[local>>6] |= uint64(1) << uint(local&63)
		return true
	}
	if m.Tables[0].Init != nil && !addExpr(*m.Tables[0].Init) {
		return nil, false
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive {
			continue
		}
		if e.Mode.Table != 0 {
			return nil, false
		}
		switch e.Kind.Kind {
		case wasm.ElemFuncs:
			for _, idx := range e.Kind.Funcs {
				local := int(idx) - m.ImportedFuncCount()
				if local < 0 || local >= len(m.Code) {
					return nil, false
				}
				targets[local>>6] |= uint64(1) << uint(local&63)
			}
		case wasm.ElemFuncExprs, wasm.ElemTypedExprs:
			for _, expr := range e.Kind.Exprs {
				if !addExpr(expr) {
					return nil, false
				}
			}
		default:
			return nil, false
		}
	}
	return targets, true
}

func countHostAdapters(adapters []bool) int {
	n := 0
	for _, adapter := range adapters {
		if adapter {
			n++
		}
	}
	return n
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

func (f *fn) ehRecordOff(index int) int32 {
	return int32(f.frameHeaderBytes() + 8*f.nLocalSlots + index*ehRecordSlots*8)
}
func (f *fn) ehRootOff(index int) int32 {
	return int32(f.frameHeaderBytes() + 8*f.nLocalSlots + f.ehTryCap*ehRecordSlots*8 + index*ehRootSlots*8)
}

// ImportBinding is shared by both Railshot architectures.
type ImportBinding = shared.ImportBinding

// CompileOptions configures direct wasm-to-arm64 compilation.
type CompileOptions struct {
	// ValidationAnalysis carries an optional completed serial hint stream. It is
	// transient, identity/policy checked, and consumed at most once.
	ValidationAnalysis *wasm.ValidatedModuleAnalysis
	SourceMaps         bool
	UnwindMaps         bool
	// Profile records finalized code regions without changing emitted bytes. Requires Stats.
	Profile          bool
	BitCountFeatures uint8 // unused on arm64; keeps the public compile contract uniform
	// Optimizations is the complete selection for this compilation. nil uses the
	// backend's environment-derived process defaults.
	Optimizations map[string]bool
	// OptimizationSnapshot identifies Optimizations as a snapshot of the backend
	// process defaults. OptimizationDeltas contains only public-runtime overrides
	// layered on that snapshot. A matching revision avoids reinstalling the full
	// selection while retaining the same compile lock and snapshot semantics.
	OptimizationSnapshot OptimizationSnapshot
	OptimizationDeltas   map[string]bool
	// CompactNative enables the internal bounded native-compaction path. It is
	// intended for measurements and rollout validation, not as a profile.
	CompactNative bool

	// Workers forces the maximum number of per-function compiler workers.
	// Values <= 1 retain the exact serial fast path. Values > 1 are capped by
	// runtime.GOMAXPROCS(0) and the module's local-function count.
	Workers int
	// DeferCodeMapping retains native bytes on the Go heap until the first
	// instance needs executable code. Public compilation uses this to avoid an
	// executable mapping for modules that are cached, serialized, or discarded.
	DeferCodeMapping bool

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
	// SyncHostSlots selects a checked symmetric control-frame capacity. Zero keeps
	// the 64-slot inline layout. Production derives wider values from GC helpers.
	SyncHostSlots int

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

	// GCTypeSubtypingRefTest admits typed function subtype tests and casts.
	// Direct ref.func values fold statically; dynamically loaded descriptors use
	// exact per-function type IDs with a cold full-metadata fallback.
	GCTypeSubtypingRefTest bool

	// GCStructHelpers admits collector-backed struct helper lowering through the
	// synchronous parked-host ABI.
	GCStructHelpers bool

	// GCArrayHelpers independently admits collector-backed array helper lowering.
	GCArrayHelpers bool

	// GCFrameRoots carries exact arm64 local/hidden-spill safepoints and caller
	// maps for direct, host, foreign, monomorphic-indirect, and local call_ref
	// boundaries. Tail/EH and polymorphic/foreign reference calls remain fail-closed.
	GCFrameRoots *shared.GCModuleFrameRootPlan

	// Codegen carries injectable runtime/heap dependencies for future WasmGC
	// lowering. The current direct backend does not lower WasmGC opcodes yet, but
	// threading the option here lets that work use the same HeapABI as the IR
	// backend instead of hard-coding allocator or collector choices.
	Codegen codegen.Options

	// Stats, when non-nil, collects per-function codegen counters into it (the
	// codegen dashboard). Requires wago_codegenstats or wago_profile.
	// Independent of WAGO_EXPLAIN, which prints the same dump
	// to stderr. nil = no collection, zero overhead.
	Stats *ModuleStats
	// CollectInlineReport enables the additional whole-module analysis used by
	// the human-facing inline report. It is intentionally separate from Stats so
	// ordinary timing and resource collection does not add another body walk.
	CollectInlineReport bool

	// CustomInstructions contains validated scalar recipes and native plugin
	// lowerings keyed by the exact imported function index.
	CustomInstructions map[uint32]railcore.CustomInstruction
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

// Parallel workers reserve only the small common local range. A rare large
// function must not multiply its full scratch size by the worker count.
// All local arrays retain their existing growth path beyond this capacity.
func parallelLocalScratchCapacity(allHints []funcHints, inlineTargets inlineTargetTable, hostAdapters []bool) int {
	return min(64, serialLocalScratchCapacity(allHints, inlineTargets, hostAdapters))
}

// compileModuleParallel is split from CompileModuleWith so the goroutine closure
// and its captured state cannot escape into or add allocations to the serial path.
func compileModuleParallel(m *wasm.Module, opts CompileOptions, workers, codeCap int, entry, internalEntry []int, allHints []funcHints, hintSidecar funcHintSidecar, immutableTable immutableTableHint, modGlobals []moduleGlobalPin, hostAdapters []bool, inlineTargets inlineTargetTable, moduleTypes moduleTypeCache, policy CodegenPolicy, ms *ModuleStats, guardMode, boundsFacts bool, importedFuncs int) (*a64.CompiledModule, error) {
	requiresLSE := false
	n := len(m.Code)
	if diagnosticsEnabled && ms != nil {
		for i := range m.Code {
			ms.Funcs[i] = &CodegenStats{RecordSources: profileEnabled && opts.Profile && opts.SourceMaps, FuncIdx: i, Name: funcDisplayName(m, i, importedFuncs)}
		}
	}
	states := make([]workerState, workers)
	arenaCap := (codeCap + workers - 1) / workers
	expandedLowering := expandedStackLowering(opts)
	stackCap := workerStackArenaCap(m, allHints, inlineTargets, expandedLowering)
	ctrlCap := workerControlFrameCap(m, allHints)
	localCap := parallelLocalScratchCapacity(allHints, inlineTargets, hostAdapters)
	pressureAt := shared.PressureThreshold(opts.MemoryPressureAt, codeCap)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var pressureBytes atomic.Int64
	var pressureOnce sync.Once
	for i := range states {
		states[i] = workerState{scratch: newCompileScratch(stackCap), arena: make([]byte, 0, arenaCap)}
		if regallocCheckEnabled {
			shared.PrepareSourceWorker(&states[i].scratch.scalar, i, workers)
		}
		states[i].scratch.classifier = classifier
		states[i].scratch.moduleTypes = moduleTypes
		states[i].scratch.reserveLocalScratch(localCap)
		if ctrlCap != 0 {
			states[i].scratch.reserveControlFrames(ctrlCap)
		}
	}
	results := make([]funcResult, n)
	var work struct {
		next     atomic.Int64
		failures shared.LowestIndexError
	}
	work.failures.Reset(n)
	var wg sync.WaitGroup
	wg.Add(workers)
	// Capture the shared context once; each worker still owns its scratch.
	runWorker := func(workerID int) {
		defer wg.Done()
		ws := &states[workerID]
		defer func() {
			ws.scratch.finishControlWorker()
			ws.scratch.finishStackWorker()
			ws.scratchStats = workerScratchStats(ws.scratch)
			ws.scratch = nil
		}()
		if regallocCheckEnabled {
			shared.SetSourceContext(&ws.scratch.scalar, codegen.SourceContextFor(opts.Codegen, m))
		}
		for {
			i := int(work.next.Add(1) - 1)
			if i >= n {
				return
			}
			var st *CodegenStats
			if diagnosticsEnabled && ms != nil {
				st = ms.Funcs[i]
			}
			if inlineTargets.omitStandaloneBody(i, hostAdapters[i]) {
				st.peep("inline-dead-body")
				results[i] = funcResult{layoutFlags: layoutOmitted}
				continue
			}
			layoutFlags := boolFlag(hostAdapters[i], layoutHostAdapter) | boolFlag(allHints[i].flags.has(hintHasLoop), layoutHasLoop) | boolFlag(allHints[i].flags.has(hintHasCall), layoutHasCall) | boolFlag(allHints[i].flags.has(hintCallsSelf), layoutCallsSelf)
			if workers == 1 {
				// Leave enough source slack for every possible entry-alignment
				// pad. The join can then compact toward the start in place:
				// its write cursor never overtakes the next unread function.
				ws.arena = append(ws.arena, alignPad[:]...)
			}
			// Emit into this worker's unused arena tail. Completed functions
			// are retained by offsets, so the encoder can use all remaining
			// capacity without retaining a second function-sized byte buffer.
			// If it outgrows the tail, ordinary append below still copies the
			// detached result into the arena safely.
			ws.scratch.asm.B = ws.arena[len(ws.arena):len(ws.arena)]
			hints := hintSidecar.viewAt(allHints[i], i)
			fnCode, rl, internalOff, err := compileFunc(m, opts.Codegen.Module.GCTypeLayouts, i, hostAdapters[i], guardMode, boundsFacts, opts.Interruptible, modGlobals, &hints, immutableTable, opts.ImportBindings, opts.SyncHostCalls, opts.SyncHostSlots, opts.GCTypeSubtypingRefTest, opts.GCStructHelpers, opts.GCArrayHelpers, opts.GCFrameRoots.Function(i), opts.CustomInstructions, st, inlineTargets, allHints, policy, ws.scratch)
			if err != nil {
				work.failures.Record(i, err)
				continue
			}
			start := len(ws.arena)
			compactStart, compactEnd, ok := compactFuncResultRange(start, len(fnCode))
			if !ok {
				work.failures.Record(i, fmt.Errorf("arm64: parallel worker code exceeds 4 GiB"))
				continue
			}
			bodyBytes, bodyOK := compactFuncResultValue(len(m.Code[i].BodyBytes))
			compactInternalOff, internalOK := compactFuncResultValue(internalOff)
			if !bodyOK || !internalOK {
				work.failures.Record(i, fmt.Errorf("arm64: parallel function metadata exceeds 32-bit range"))
				continue
			}
			relocStart := len(ws.relocs)
			compactRelocStart, compactRelocEnd, relocOK := compactFuncResultRange(relocStart, len(rl))
			if !relocOK {
				work.failures.Record(i, fmt.Errorf("arm64: parallel worker relocations exceed 32-bit range"))
				continue
			}
			ws.relocs = append(ws.relocs, rl...)
			layoutFlags |= boolFlag(ws.scratch.directPrepared, layoutDirectPrepared)

			layoutFlags |= boolFlag(ws.scratch.directPreparedBounded, layoutDirectPreparedBounded)
			ws.arena = append(ws.arena, fnCode...)
			if ws.scratch.fnState.gcI31Induction {
				if ws.gcI31Inductions == nil {
					ws.gcI31Inductions = make(map[int]bool)
				}
				ws.gcI31Inductions[i] = true
			}
			if layout := ws.scratch.fnState.gcDeadBatchLayout; layout != 0 {
				if ws.gcDeadBatchLayouts == nil {
					ws.gcDeadBatchLayouts = make(map[int]uint64)
				}
				ws.gcDeadBatchLayouts[i] = layout
			}
			result := funcResult{gcReducedBounded: ws.scratch.fnState.gcReducedBounded, requiresLSE: ws.scratch.fnState.requiresLSE, worker: uint32(workerID), start: compactStart, end: compactEnd, relocStart: compactRelocStart, relocEnd: compactRelocEnd, bodyBytes: bodyBytes, layoutFlags: layoutFlags, internalOff: compactInternalOff, scopedFloat: scopedFloatFunctionLayout(allHints[i], policy)}
			if policy.CompactNative {
				if policy.EnabledOption(optSharedAdapters) {
					info := ws.scratch.fnState.sharedAdapterInfo()
					result.adapterOff, result.adapterEnd = info.callOff, info.endOff
				} else {
					info := ws.scratch.fnState.adapterTailInfo()
					result.adapterOff, result.adapterEnd = info.returnOff, info.endOff
				}
				if policy.EnabledOption(optSharedTrapBody) {
					result.trapBody = ws.scratch.fnState.sharedTrapBodyInfo()
				}
			}
			results[i] = result
			if opts.MemoryPressure != nil && pressureBytes.Add(int64(len(fnCode))) >= int64(pressureAt) {
				pressureOnce.Do(opts.MemoryPressure)
			}
		}
	}
	for workerID := range states {
		go runWorker(workerID)
	}
	wg.Wait()
	if i, err := work.failures.Result(); err != nil {
		return nil, fmt.Errorf("arm64: function %d: %w", i, err)
	}
	relocs := parallelCallRelocTable(results, states)

	if !policy.CompactNative {
		// Worker bodies are complete. Compact adapter sharing keeps its old
		// estimate because it temporarily appends an island before compaction.
		joinedBytes := 0
		for i := range states {
			if len(states[i].arena) > int(^uint(0)>>1)-joinedBytes {
				joinedBytes = int(^uint(0) >> 1)
				break
			}
			joinedBytes += len(states[i].arena)
		}
		codeCap = shared.JoinedModuleCodeCapacity(codeCap, joinedBytes, n)
	}
	var code []byte
	if workers == 1 {
		code = states[0].arena[:0]
	} else {
		code = make([]byte, 0, codeCap)
	}
	var directPrepared, directPreparedBounded, gcReducedBounded, gcI31Inductions []uint64
	var gcDeadBatchLayouts []uint64
	var adapterTails []adapterTailInfo
	var adapters []sharedAdapterInfo
	if policy.CompactNative {
		if policy.EnabledOption(optSharedAdapters) {
			adapters = make([]sharedAdapterInfo, 0, countHostAdapters(hostAdapters))
		} else {
			adapterTails = make([]adapterTailInfo, 0, countHostAdapters(hostAdapters))
		}
	}
	var trapBodyCluster sharedTrapBodyCluster
	for i := range results {
		r := &results[i]
		requiresLSE = requiresLSE || r.requiresLSE
		if r.layoutFlags&layoutOmitted != 0 {
			continue
		}
		if pad := functionStartPaddingFlags(len(code), int(r.bodyBytes), r.layoutFlags, r.scopedFloat, policy); pad != 0 {
			code = append(code, alignPad[:pad]...)
		}
		entry[i] = len(code)
		internalEntry[i] = len(code) + int(r.internalOff)
		if r.layoutFlags&layoutDirectPrepared != 0 {
			directPrepared = markDirectPrepared(directPrepared, n, i)
		}

		if layout := states[r.worker].gcDeadBatchLayouts[i]; layout != 0 {
			if gcDeadBatchLayouts == nil {
				gcDeadBatchLayouts = make([]uint64, n)
			}
			gcDeadBatchLayouts[i] = layout
		}
		if states[r.worker].gcI31Inductions[i] {
			gcI31Inductions = markDirectPrepared(gcI31Inductions, n, i)
		}
		if r.gcReducedBounded {
			gcReducedBounded = markDirectPrepared(gcReducedBounded, n, i)
		}
		if r.layoutFlags&layoutDirectPreparedBounded != 0 {
			directPreparedBounded = markDirectPrepared(directPreparedBounded, n, i)
		}
		if adapterTails != nil && r.adapterOff != 0 {
			adapterTails = append(adapterTails, adapterTailInfo{function: uint32(i), returnOff: r.adapterOff, endOff: r.adapterEnd})
		}
		if adapters != nil && r.adapterEnd != 0 {
			adapters = append(adapters, sharedAdapterInfo{function: uint32(i), callOff: r.adapterOff, endOff: r.adapterEnd})
		}
		fnCode := states[int(r.worker)].arena[int(r.start):int(r.end)]
		if r.layoutFlags&layoutHostAdapter != 0 {
			trapBodyCluster.reset()
		}
		if policy.EnabledOption(optSharedTrapBody) && policy.CompactNative {
			var st *CodegenStats
			if diagnosticsEnabled && ms != nil {
				st = ms.Funcs[i]
			}
			fnCode = trapBodyCluster.share(code, fnCode, entry[i], r.trapBody, st)
		}
		code = append(code, fnCode...)
	}
	moduleOther := 0
	if adapters != nil {
		var err error
		var sharedBytes int
		code, sharedBytes, err = shareAdapters(code, entry, internalEntry, &relocs, adapters, opts.GCFrameRoots, ms)
		if err != nil {
			return nil, err
		}
		moduleOther += sharedBytes
	} else if adapterTails != nil {
		var err error
		var sharedBytes int
		code, sharedBytes, err = shareAdapterTails(code, entry, internalEntry, &relocs, adapterTails, opts.GCFrameRoots, ms)
		if err != nil {
			return nil, err
		}
		moduleOther += sharedBytes
	}
	if needsGCObjectStub(&relocs) {
		relocs.gcObjectEntry = len(code)
		code = emitGCObjectStub(code)
		moduleOther += len(code) - relocs.gcObjectEntry
	}
	if err := finalizeOmittedInlineEntries(entry, internalEntry, &relocs, hostAdapters, inlineTargets); err != nil {
		return nil, err
	}
	if err := patchCallRelocs(code, entry, internalEntry, &relocs); err != nil {
		return nil, err
	}
	directPreparedBounded = mergeGCReducedBounds(resolveBoundedPreparedEntries(m, directPreparedBounded, allHints, relocs, immutableTable), gcReducedBounded)
	finalizeModuleNativeSize(ms, len(code), moduleOther, 0)
	if profileEnabled && opts.Profile {
		recordProfileRegions(ms, entry, importedFuncs, len(code), len(code)-moduleOther, moduleOther)
	}
	if diagnosticsEnabled && ms != nil {
		for i := range states {
			ms.Compile.AddWorkerScratch(states[i].scratchStats)
		}
	}
	ms.finalizeCompileResourceStats()
	if diagnosticsEnabled && explainEnabled && ms != nil {
		fmt.Fprint(os.Stderr, ms.String())
	}
	return &a64.CompiledModule{GCDeadInductionLayouts: gcDeadBatchLayouts, GCI31Inductions: gcI31Inductions, RequiresARM64LSE: requiresLSE, Code: code, Entry: entry, InternalEntry: internalEntry, DirectPrepared: directPrepared, DirectPreparedBounded: directPreparedBounded, PreparedIsolatedTables: immutableTable.local}, nil
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
// computeModuleHints scans every function body ONCE, returning per-function hints
// plus the module-wide aggregated global scores. The scan uses one reusable dense
// accumulator and retains only the globals each function actually touches. This
// avoids both a second body pass and a functions-by-globals retained matrix. The
// standalone computeModuleGlobalScores is retained as the parity oracle in tests.
func computeModuleHints(m *wasm.Module, nGlobals, importedFuncs int) ([]funcHints, funcHintSidecar, []int64, error) {
	return computeModuleHintsWithPolicy(m, nGlobals, importedFuncs, currentCodegenPolicy(), false)
}

func computeModuleHintsWithPolicy(m *wasm.Module, nGlobals, importedFuncs int, policy CodegenPolicy, detailedResidency bool) ([]funcHints, funcHintSidecar, []int64, error) {
	return computeModuleHintsWithWorkersResidencyPolicy(m, nGlobals, importedFuncs, 1, policy, detailedResidency)
}

func computeModuleHintsWithWorkersPolicy(m *wasm.Module, nGlobals, importedFuncs, workers int, policy CodegenPolicy) ([]funcHints, funcHintSidecar, []int64, error) {
	return computeModuleHintsWithWorkersResidencyPolicy(m, nGlobals, importedFuncs, workers, policy, false)
}

// summarizeHintResidency uses the same bounded event tape and planner for
// serial and parallel scans. Each worker owns its tape; output slots are
// assigned in function order before workers start.
func summarizeHintResidency(tape *shared.LocalEventTape, nLocals int) shared.ResidencyShadowSummary {
	return shared.PlanResidencyTransitionShadow(tape.Events, nLocals, maxIntervalRegionRegs, tape.Overflow)
}

const (
	minParallelHintBodyBytes     = 16 << 10
	maxParallelHintGlobalEntries = 1 << 18
)

type parallelCalleeHints struct {
	direct atomic.Uint32
	inline atomic.Uint32
	loop   atomic.Bool
}

// Global offsets use each function's existing hint fields until flattening.
// Only the worker and event-sidecar owner need separate temporary storage.
type parallelHintOwner struct {
	worker uint32
	event  uint32
}

type parallelHintWorker struct {
	elig            globalEligibilityTracker
	globals         shared.GlobalHintAccumulator
	retained        []shared.GlobalHint
	classifier      wasm.ModuleInstructionClassifier
	localEvents     shared.LocalEventTape
	frameScratch    [4]globalEligibilityFrame
	globalScratch   [8]uint32
	retainedScratch [8]shared.GlobalHint
	loopIntConsts   []loopIntConstHintEntry
	loopIntConstAt  int
}

// The caller has already bounded workers * nGlobals with
// maxParallelHintGlobalEntries. Dense storage is one module-owned allocation;
// each worker gets exclusive, capacity-bounded slices and small scope scratch.
func newParallelHintWorkers(m *wasm.Module, nGlobals, workers int) []parallelHintWorker {
	states := make([]parallelHintWorker, workers)
	words := make([]uint32, 3*nGlobals*workers)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range states {
		state := &states[i]
		start := i * 3 * nGlobals
		state.elig.marks = words[start : start+nGlobals : start+nGlobals]
		state.elig.frames = state.frameScratch[:0]
		state.retained = state.retainedScratch[:0]
		state.elig.globals = state.globalScratch[:0]
		state.globals.ResetWithScratch(nGlobals, words[start+nGlobals:start+3*nGlobals:start+3*nGlobals])
		state.classifier = classifier
	}
	return states
}

func scanModuleHintsParallel(m *wasm.Module, nGlobals, importedFuncs, workers int, allHints []funcHints, localScores, localLastGets []uint32, compactLastGets bool, intervalFunctions int, localEventMeta []uint32, residencyShadow []shared.ResidencyShadowEntry, eventReserve int, detailedResidency, collectLoopIntConsts bool) (sparseGlobals []shared.GlobalHint, loopIntConsts []loopIntConstHintEntry, intervalRangeAt int, moduleEH bool, err error) {
	// Assign every worker-owned local range before starting goroutines. globalStart
	// temporarily carries the last-get start and is replaced during flattening.
	scoreAt, intervalAt := 0, intervalFunctions*2
	for i := range allHints {
		h := &allHints[i]
		nLocals := int(h.localCount)
		h.localStart = uint32(scoreAt)
		if h.flags.has(hintIntervalRegionStorage) {
			lastGetAt := scoreAt
			if compactLastGets {
				lastGetAt = intervalAt
				localLastGets[intervalRangeAt] = uint32(scoreAt)
				localLastGets[intervalRangeAt+1] = uint32(intervalAt)
				intervalRangeAt += 2
				intervalAt += nLocals
			}
			h.globalStart = uint32(lastGetAt)
		}
		scoreAt += retainedLocalScoreCount(*h)
	}

	states := newParallelHintWorkers(m, nGlobals, workers)
	calleeHints := make([]parallelCalleeHints, len(allHints))
	owners := make([]parallelHintOwner, len(allHints))
	eventAt := uint32(0)
	for i := range allHints {
		if allHints[i].flags.has(hintIntervalRegionStorage) {
			owners[i].event = eventAt
			eventAt++
		}
	}
	var next atomic.Int64
	var failures shared.LowestIndexError
	failures.Reset(len(allHints))
	var wg sync.WaitGroup
	wg.Add(workers)
	// Capture the shared context once; each worker still owns its scratch.
	runWorker := func(workerID int) {
		defer wg.Done()
		state := &states[workerID]
		for {
			i := int(next.Add(1) - 1)
			if i >= len(allHints) || !failures.ShouldStart(i) {
				return
			}
			base := allHints[i]
			nLocals := int(base.localCount)
			localStart := int(base.localStart)
			h := funcHintsWithStorage(localScores[localStart : localStart+retainedLocalScoreCount(base)])
			h.nLocals = nLocals
			h.localCount = base.localCount
			h.localStart = base.localStart
			h.flags.assign(hintIntervalRegionStorage, base.flags.has(hintIntervalRegionStorage))
			if base.flags.has(hintIntervalRegionStorage) {
				lastGetStart := int(base.globalStart)
				h.localLastGet = localLastGets[lastGetStart : lastGetStart+nLocals]
			}
			if base.flags.has(hintIntervalRegionStorage) {
				if detailedResidency {
					if state.localEvents.Events == nil {
						state.localEvents.Events = make([]shared.LocalEvent, 0, eventReserve)
					}
					state.localEvents.Reset(shared.LocalEventLimit)
					h.localEvents = &state.localEvents
				} else {
					h.countLocalEvents = true
				}
			}
			state.globals.Reset(nGlobals)
			h, scanErr := scanBodyBytesIntoModule(m.Code[i].BodyBytes, m.Code[i].LocalDeclBytes, nLocals, nGlobals, uint32(importedFuncs+i), m.BranchHintsForFunc(uint32(importedFuncs+i)), h, &state.elig, m, &state.classifier, nil, calleeHints, importedFuncs, &state.globals, collectLoopIntConsts)
			if scanErr != nil {
				failures.Record(i, fmt.Errorf("function %d hints: %w", i, scanErr))
				continue
			}
			h.flags.assign(hintIntervalRegionStorage, base.flags.has(hintIntervalRegionStorage))
			if base.flags.has(hintIntervalRegionStorage) {
				event := int(owners[i].event)
				count, overflow := h.localEventCountAt, h.localEventFull
				if detailedResidency {
					count, overflow = len(state.localEvents.Events), state.localEvents.Overflow
				}
				h.setLocalEventSummary(count, overflow)
				localEventMeta[event*2] = h.localStart
				localEventMeta[event*2+1] = h.localEventMeta
				if detailedResidency {
					residencyShadow[event] = shared.ResidencyShadowEntry{LocalStart: h.localStart, Summary: summarizeHintResidency(&state.localEvents, nLocals)}
				}
			}
			allHints[i] = h.funcHints
			if h.loopIntConstCount != 0 {
				state.loopIntConsts = append(state.loopIntConsts, loopIntConstHintEntry{function: uint32(i), bits: h.loopIntConst, types: h.loopIntConstTypes, count: h.loopIntConstCount})
			}
			start := len(state.retained)
			state.retained = state.globals.AppendTo(state.retained)
			if uint64(len(state.retained)) > uint64(^uint32(0)) {
				failures.Record(i, fmt.Errorf("function %d hints: worker global sidecar exceeds 32-bit index capacity", i))
				continue
			}
			allHints[i].globalStart = uint32(start)
			allHints[i].globalCount = uint32(len(state.retained) - start)
			owners[i].worker = uint32(workerID)
		}
	}
	for workerID := range states {
		go runWorker(workerID)
	}
	wg.Wait()
	if _, err := failures.Result(); err != nil {
		return nil, nil, 0, false, err
	}

	var totalGlobals uint64
	for i := range owners {
		totalGlobals += uint64(allHints[i].globalCount)
		if totalGlobals > uint64(^uint32(0)) || totalGlobals > uint64(^uint(0)>>1)/uint64(unsafe.Sizeof(shared.GlobalHint{})) {
			return nil, nil, 0, false, fmt.Errorf("function hint global sidecar exceeds 32-bit index capacity")
		}
	}
	// One destination allocation; use the serial accumulator's capacity contract.
	globalCapacity := shared.GlobalHintCapacity(int(totalGlobals))
	if uint64(globalCapacity) > uint64(^uint(0)>>1)/uint64(unsafe.Sizeof(shared.GlobalHint{})) {
		return nil, nil, 0, false, fmt.Errorf("function hint global sidecar exceeds addressable byte capacity")
	}
	if totalGlobals != 0 {
		sparseGlobals = make([]shared.GlobalHint, 0, globalCapacity)
	}
	for i := range allHints {
		r := owners[i]
		// A worker claims function indexes in increasing order. Merge its sparse
		// constants in module order, with the same append capacity as serial.
		state := &states[r.worker]
		if allHints[i].hasLoopIntConsts() {
			loopIntConsts = append(loopIntConsts, state.loopIntConsts[state.loopIntConstAt])
			state.loopIntConstAt++
		}
		start := allHints[i].globalStart
		count := allHints[i].globalCount
		stateGlobals := states[r.worker].retained[start : start+count]
		allHints[i].globalStart = uint32(len(sparseGlobals))
		allHints[i].globalCount = uint32(len(stateGlobals))
		sparseGlobals = append(sparseGlobals, stateGlobals...)
		moduleEH = moduleEH || allHints[i].flags.has(hintModuleEH)
	}
	for i := range allHints {
		calls := &calleeHints[i]
		allHints[i].directCallRefs = uint8(min(calls.direct.Load(), uint32(^uint8(0))))
		allHints[i].inlineCallSites = uint16(min(calls.inline.Load(), uint32(^uint16(0))))
		allHints[i].flags.assign(hintHasInlineLoopCall, calls.loop.Load())
	}
	return sparseGlobals, loopIntConsts, intervalRangeAt, moduleEH, nil
}

// compactEHLocalScores drops interval-only storage when a tagless try_table or
// throw makes the module-wide EH requirement visible during the fused body
// scan. This rare-path copy avoids adding a second body pass to ordinary
// modules while allowing the oversized transient backing to be collected.
func compactEHLocalScores(allHints []funcHints, scores []uint32) []uint32 {
	total := 0
	for i := range allHints {
		total += min(int(allHints[i].localCount), 64)
		allHints[i].flags.assign(hintIntervalRegionStorage, false)
	}
	if total == len(scores) {
		return scores
	}
	compact := make([]uint32, total)
	at := 0
	for i := range allHints {
		count := min(int(allHints[i].localCount), 64)
		start := int(allHints[i].localStart)
		copy(compact[at:at+count], scores[start:start+count])
		allHints[i].localStart = uint32(at)
		at += count
	}
	return compact
}

func immutableLocalTableType(m *wasm.Module) (uint64, bool) {
	return immutableLocalTableTypeWithPolicy(m, currentCodegenPolicy())
}

func computeModuleGlobalScores(m *wasm.Module, nGlobals int) ([]int64, error) {
	if nGlobals == 0 || len(m.Code) == 0 {
		return nil, nil
	}
	agg := make([]int64, nGlobals)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		if err := scanFuncGlobalScores(m, &classifier, m.Code[i], nGlobals, func(g uint32, score int64) {
			agg[g] += score
		}); err != nil {
			return nil, fmt.Errorf("function %d global scores: %w", i, err)
		}
	}
	return agg, nil
}

// regExhausted is the internal sentinel raised when a lowering step blocks the
// whole register file after every spillable value has been homed.
type regExhausted struct{ class string }

func (e regExhausted) Error() string {
	return fmt.Sprintf("no %s register available after applying the transient register floor", e.class)
}

// Below this count, an optionally inlined call can erase the only relocation
// and ordinary append growth crosses too few size classes to repay a reserve.
const minPreallocatedCallRelocs = 8

// compilerPanicError reads diagnostic context only after an invariant failed.
// A nil state means setup failed before the current function replaced scratch.
func (f *fn) compilerPanicError(m *wasm.Module, funcIdx int, recovered any) *railcore.InternalCompilerError {
	index, offset := funcIdx, -1
	if m != nil {
		index += m.ImportedFuncCount()
	}
	if f != nil {
		index = int(f.traceFuncIdx)
		// Valid bodies begin with the local-declaration vector; zero means no
		// instruction location has yet been recorded by the driver.
		if f.wasmPC != 0 {
			offset = int(f.wasmPC)
		}
	}
	return railcore.NewInternalCompilerError("arm64", index, offset, recovered)
}

// assignPinnedLocals dedicates registers to the hottest integer locals (by the
// hotness scores). Locals with a zero score (the DecodeModule BodyBytes path or
// unused) are ordered by index, so byte-backed bodies fall back to first-N
// pinning.
// gpPinPool returns the registers available to hold pinned integer locals, in
// priority order (hottest local gets the first). The base is X19-X25; these are
// callee-saved across the native entry boundary, module-global pins are removed
// before assignment, and call-making functions use the STACK_REG spill/lazy-load
// model when a Wasm callee may reuse them. Call-free functions may additionally
// use X8.
//
// The wrapper-arg registers (X0-X3) are deliberately NOT pinned. A call's
// linMem/trap/results setup clobbers them (they are not the reg-ABI internal-entry
// arg registers — intArgRegs is X0-X7, but pins never land there anyway), and in a
// register-heavy function that both touches memory (which reserves X27, pushing
// pins onto the extended pool) and makes multi-arg calls, having a pinned local
// live in a call-clobbered register on top of the arg-register pins over-subscribed
// the file: the call's arg-staging + setup ran out of free scratch and silently
// corrupted a pinned local's value. The observable repro is sqlite's tokenizer —
// every SQL keyword misreads as an identifier ("near \"SELECT\": syntax error").
// Restricting pins to the callee-saved block plus the STACK_REG-managed
// X9/X10/X11 removes the hazard. See TestSyncSQLiteQuery.
//
// X9/X10/X11 are still excluded in reg-ABI functions with >4 params (the internal
// entry's incoming args would collide with the prologue's arg→pinned moves). X15
// costs the block-merge register (the caller drops regMerge). X1/X0 always stay
// free for operand evaluation and the return register; callHost's scratch also
// lives in the caller-saved temporaries.
func gpPinPool(pool []Reg, regABI bool, nParams int, callFree bool) []Reg {
	return gpPinPoolWithPolicy(pool, regABI, nParams, callFree, currentCodegenPolicy())
}

const (
	globalRegDirty Reg = 1 << 7
	globalRegFloat Reg = 1 << 6
)

// storeDirtyGlobalPins publishes only values changed by this function. An EH
// transfer may rederive every pin in the handler after restoring instance
// context, so direct throws must make deferred global.set values cell-coherent
// first. Filtering on the compile-time dirty bit keeps the cold edge compact.
func (f *fn) storeDirtyGlobalPins(scratch Reg) {
	for g, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg == regNone || !globalRegIsDirty(state) {
			continue
		}
		f.ld64(scratch, linMemReg, -int32(abi.GlobalsPtrOffset))
		f.ld64(scratch, scratch, int32(g*8))
		f.storePinnedGlobalValue(g, state, scratch)
	}
}

type pendingWrapperParamHome struct {
	argOff   int32
	localOff int32
	valid    bool
}

func abiValOff(ts []wasm.ValType, idx int) int32 {
	off := int32(0)
	for i := 0; i < idx; i++ {
		off += abiValSize(ts[i])
	}
	return off
}

// ehFrameShape sizes one function's exception records from its body. Modules
// built without body bytes keep the historical fixed reservation.
func ehFrameShape(sc *scratch, body []byte) (shared.EHFrameShape, error) {
	if len(body) == 0 {
		return shared.EHFrameShape{TryRecords: legacyEHTryRecords, RootRecords: legacyEHRootRecords}, nil
	}
	return shared.ScanEHFrameShape(&sc.classifier, body)
}
