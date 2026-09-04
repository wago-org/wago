//go:build arm64

package arm64

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/optimization"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// regMergeEnabled turns on WARP-style register reconciliation of single-int-result
// block/if merges (docs/operand-stack-registers-plan.md) instead of the
// flush-to-slot + reload. Default ON (fib_rec −13.7%, json-as serialize −1.5%, no
// regressions; validated against the spec suite + full corpus differential).
// WAGO_REG_MERGE=0 restores the slot path — kept as the reference oracle for A/B.
var regMergeEnabled = os.Getenv("WAGO_REG_MERGE") != "0"

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
)

// fn holds the per-function code-generation state — the port's equivalent of
// WARP's Compiler/backend working set. One is created per compiled function.
type fn struct {
	a             *a64.Asm // the (reused) AArch64 encoder
	s             *stack   // the valent-block operand stack
	sc            *scratch // module-wide reusable compile scratch
	m             *wasm.Module
	ft            *wasm.CompType // this function's signature
	gcTypeLayouts []codegen.GCTypeLayout
	classifier    wasm.ModuleInstructionClassifier
	transient
	traceFuncIdx        uint32
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
	intervalLast  []uint32
	intervalScore []uint32
	intervalOwner [32]int

	// WARP STACK_REG lazy-spill model for pinned locals in CALL-MAKING functions
	// (usesCalls). locals[i].state tracks whether the live value of pinned local i is
	// in its register (dirty), in both register+slot (clean), or only in its slot.
	// Call-free functions keep locals permanently in registers (locals[].state unused).
	usesCalls bool
	// controlBaseTypeN partitions the fixed function-result scratch: function
	// results occupy its prefix and open control-frame bases use the remaining tail.
	controlBaseTypeN uint8
	// localFactsEnabled admits assignment-version facts only in straight-line
	// bodies. Facts reuse an otherwise-unused byte in each localDef.
	localFactsEnabled bool
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

	maxSpill int // high-water number of operand spill slots used
	// spillFloor temporarily reserves a low spill-slot range while wide-stack
	// canonicalization stages values above both their old homes and destinations.
	spillFloor              int
	subRspAt                int   // byte offset of the prologue's frame-alloc MOVZ (patched with frameSize)
	addRspAt                int   // byte offset of the epilogue's frame-free MOVZ (patched with frameSize)
	tailFrameSites          []int // tail-transfer frame-free MOVZ sites (patched with frameSize)
	frameElided             bool  // simple register-only internal entry leaves SP unchanged
	adapterReturnOff        int   // register-ABI wrapper continuation used by cross-tail reuse
	adapterEndOff           int   // end of the wrapper before internal-entry alignment
	adapterReturnReferenced bool  // cross-tail reuse embeds the local return PC; keep that tail local
	trapBodyOff             int   // complete shared trap body start; zero when not emitted
	trapBodyEnd             int   // complete shared trap body end
	guardMode               bool  // elide inline bounds checks; rely on guard-page + SIGSEGV trap
	boundsFacts             bool  // P6.1 straight-line bounds-check elision enabled (explicit mode)
	interruptible           bool  // emit context-cancellation polls at entries and loop headers
	hasLoop                 bool  // finalizer preserves emission-time loop layout until alignment fragments are relaxable
	opaqueFragments         bool  // jump-table or plugin bytes require explicit fragment-aware scans
	lazyZero                bool  // defer declared-local zeroing for small call+memory functions
	skipFence               bool  // call-free leaf with a provably small frame: no stack-fence check

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
	ehTryDepth          int         // live reachable try_table records; bounded by maxEHTryRecords
	ehRootCount         int         // fixed exception-root records assigned in this function
	branchHints         []wasm.BranchHint
	branchHintLocalDecl uint32
	branchHintUnlikely  bool
	unreachable         bool // in dead code after an unconditional branch/trap

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

	// syncHostCalls is set when the module has any returning host import, so every
	// host call in the module uses the synchronous control frame (callHostSync)
	// rather than the async log — the two share offCustomCtx and must not both be
	// live. Computed once per module in compileFunc.
	syncHostCalls          bool
	syncHostSlots          int
	gcTypeSubtypingRefTest bool
	gcStructHelpers        bool
	gcArrayHelpers         bool
	gcFrameRoots           *shared.GCFrameRootPlan
	gcCallsiteIndex        int
	moduleEH               bool // reserve the handler register and fixed EH frame area
	compactFrameHeader     bool // register ABI: no wrapper results-pointer header

	// stats collects per-function codegen counters (docs/no-ir-plan.md P1). nil
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
	storeForwardOK bool
	// threadedMemory0 routes shared memory zero through the instance-owned memory
	// directory, leaving linMemReg's negative basedata private to the instance.
	threadedMemory0 bool
}

func (f *fn) opt(option optimization.Option) bool {
	if f.policy.Valid() {
		return f.policy.EnabledOption(option)
	}
	return currentCodegenPolicy().EnabledOption(option)
}

// transient is the per-function workspace handed back to module scratch after
// each compile. Embedding it keeps hot call sites terse while making ownership
// and lifetime a single assignment instead of a list of parallel fields.
type transient struct {
	lsPool         []packedLocStates
	lsPoolBytes    int
	inlineBasePool map[int]int
	endsPool       [][]uint32
	tmpRoots       []*elem
	tmpTypes       []machineType
	tmpTypes2      []machineType
	tmpGCRoots     []bool
	tmpGCRoots2    []bool
	tmpGCOffsets   []uint32
	tmpFlushTypes  []machineType
	tmpRegs        []Reg
	tmpStackSlots  []uint32 // operand slot prefixes; successful native frames fit uint32 exactly
	tmpMoves       []regMove
	tmpLabels      []uint32
	tmpDeferred    []deferredArg
	loopSetLocals  []uint16
	edgeScratch    []byte
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

func gpCandBefore(a, b gpCand) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if a.global != b.global {
		return !a.global // tie: prefer a local value over a global pointer
	}
	return a.idx < b.idx
}

func insertGPCandidate(top []gpCand, candidate gpCand, limit int) []gpCand {
	position := len(top)
	for i := range top {
		if gpCandBefore(candidate, top[i]) {
			position = i
			break
		}
	}
	if position >= limit {
		return top
	}
	if len(top) < limit {
		top = append(top, gpCand{})
	}
	copy(top[position+1:], top[position:len(top)-1])
	top[position] = candidate
	return top
}

func insertLocalCandidate(top []uint16, candidate uint16, scores []uint32, limit int) []uint16 {
	position := len(top)
	candidateScore := localHotness(scores[candidate])
	for i, local := range top {
		score := localHotness(scores[local])
		if candidateScore > score || candidateScore == score && candidate < local {
			position = i
			break
		}
	}
	if position >= limit {
		return top
	}
	if len(top) < limit {
		top = append(top, 0)
	}
	copy(top[position+1:], top[position:len(top)-1])
	top[position] = candidate
	return top
}

type deferredArg struct {
	target Reg
	root   *elem
}

var alignPad [16]byte

func align16(n int) int { return (n + 15) &^ 15 }

func alignmentPadding(off int, log2 uint8) int {
	if log2 < 2 {
		log2 = 2
	}
	align := 1 << log2
	return (-off) & (align - 1)
}

// functionStartPadding makes optional entry alignment a policy-owned, costed
// choice. Ordinary code keeps addressable entries aligned and admits statically hot
// or large bodies only when their Wasm size can amortize the exact padding.
func functionStartPadding(off, bodyBytes int, hostAdapter bool, hints funcHints, policy CodegenPolicy) int {
	flags := boolFlag(hostAdapter, layoutHostAdapter) | boolFlag(hints.flags.has(hintHasLoop), layoutHasLoop) | boolFlag(hints.flags.has(hintHasCall), layoutHasCall) | boolFlag(hints.flags.has(hintCallsSelf), layoutCallsSelf)
	return functionStartPaddingFlags(off, bodyBytes, flags, policy)
}

func boolFlag(on bool, flag uint8) uint8 {
	if on {
		return flag
	}
	return 0
}

func functionStartPaddingFlags(off, bodyBytes int, flags uint8, policy CodegenPolicy) int {
	mandatory := alignmentPadding(off, 2)
	optional := alignmentPadding(off, policy.FunctionAlignLog2)
	if optional == mandatory || policy.FunctionAlignLog2 <= 2 {
		return mandatory
	}
	if flags&layoutHostAdapter != 0 {
		return optional
	}
	if flags&(layoutHasLoop|layoutHasCall|layoutCallsSelf) == 0 && bodyBytes < 256 {
		return mandatory
	}
	budget := min(12, bodyBytes/16)
	if optional <= budget {
		return optional
	}
	return mandatory
}

func (f *fn) alignCode(log2 uint8) int {
	if log2 == 0 {
		// Isolated backend primitive tests construct fn directly. Production
		// compilation always supplies a resolved policy.
		log2 = 4
	}
	pad := alignmentPadding(f.a.Len(), log2)
	for range pad / 4 {
		f.a.Nop()
	}
	return pad
}

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
	stack          *stack   // the valent-block operand stack
	asm            *a64.Asm // the AArch64 encoder byte buffer
	fnState        fn       // per-function compiler state, reused across the module
	classifier     wasm.ModuleInstructionClassifier
	directPrepared bool
	relocs         []callReloc

	retSiteHead             uint32 // word index+1; links live in unresolved B immediates
	ctrl                    []ctrlFrame
	ctrlMerges              []ctrlFrameMerge
	ctrlRoots               []ctrlFrameRoots
	functionResultTypeArena [maxScratchFunctionResults]machineType
	trapSites               [trapAtomicUnaligned + 1][]trapSite
	branchTargets           []uint64
	branchTargetInline      [64]uint64   // one bit per instruction; covers 16 KiB of native code
	finalizerMarkers        map[int]bool // validation-only inventory
	brTableStubAt           []int        // duplicate-heavy jump-table target positions by control depth
	finalFragments          []finalizerFragment
	deadHoleSites           [maxFinalizerDeletions]int
	branchNextSites         [maxFinalizerDeletions]int
	singleBitTests          [maxFinalizerDeletions]singleBitTestSite
	deadHoleN               uint8
	branchNextN             uint8
	singleBitTestN          uint8
	deadHoleOverflow        bool
	fragmentOverflow        bool
	hasBranchTargets        bool
	hasPCRelative           bool
	offsetMap               shared.OffsetMap
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
	transient
}

type trapSite struct {
	branch   uint32
	function uint32
	pc       uint32
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
	return newScratchWithStackCap(defaultStackArenaCap)
}

func newScratchWithStackCap(stackCap int) *scratch {
	stack := newStackWithCap(stackCap)
	_, reserved := stack.nodeMemory()
	return &scratch{stack: stack, asm: &a64.Asm{}, nodeScratchReserved: reserved, nodeScratchPeak: reserved}
}

func (sc *scratch) reserveControlFrames(capacity int) {
	if capacity <= 0 {
		return
	}
	sc.ctrl = make([]ctrlFrame, 0, capacity)
	sc.controlScratchReserved = capacity
	sc.controlScratchPeak = capacity
}

func (sc *scratch) reserveLocalScratch(capacity int) {
	if capacity <= 0 {
		return
	}
	sc.fnState.localType = make([]machineType, 0, capacity)
	sc.fnState.localSlot = make([]uint32, 0, capacity)
	sc.fnState.locals = make([]localDef, 0, capacity)
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

// maxInitialStackArenaCap bounds speculative operand-node storage retained by
// one serial compiler scratch. Larger functions still grow through stable
// chunks. This removes geometric growth for ordinary large functions without
// letting one pathological hint reserve an unbounded first chunk.
const maxInitialStackArenaCap = shared.MaxInitialStackArenaCapacity

// moduleStackArenaCap chooses the first operand-stack chunk reused across the
// serial module compile. The one-pass function pre-scan already counts
// arena-producing nodes, so use its largest bounded estimate instead of forcing
// large functions through the legacy 256-element geometric growth path.
func moduleStackArenaCap(m *wasm.Module, hints []funcHints) int {
	if !shared.StackArenaHintsEnabled || len(hints) != len(m.Code) || moduleHasMultiValueResults(m) {
		return defaultStackArenaCap
	}
	capHint := minStackArenaCap
	legacyRetained := defaultStackArenaCap
	for i := range hints {
		if hints[i].flags.has(hintHasStackSinkFusion) {
			return defaultStackArenaCap
		}
		nodes := int(hints[i].stackArenaNodes)
		fnCap := stackArenaCapForHints(len(m.Code[i].BodyBytes), int(hints[i].localCount), nodes)
		if fnCap > maxInitialStackArenaCap || fnCap < nodes {
			return defaultStackArenaCap
		}
		if fnCap > capHint {
			capHint = fnCap
		}
		effectiveNodes := nodes - int(hints[i].stackArenaDiscount)
		if effectiveNodes < 1 {
			effectiveNodes = 1
		}
		if retained := legacyStackArenaRetained(effectiveNodes); retained > legacyRetained {
			legacyRetained = retained
		}
	}
	if capHint >= legacyRetained {
		return defaultStackArenaCap
	}
	return capHint
}

// legacyStackArenaRetained returns the total node capacity held by the legacy
// 256/512/... chunk sequence for a raw node hint. It stops once the total is
// already above any admissible direct first chunk.
func legacyStackArenaRetained(nodes int) int {
	total, next := defaultStackArenaCap, defaultStackArenaCap*2
	for total < nodes && total <= maxInitialStackArenaCap {
		total += next
		if next < maxStackChunkCap {
			next *= 2
			if next > maxStackChunkCap {
				next = maxStackChunkCap
			}
		}
	}
	return total
}

func moduleHasMultiValueResults(m *wasm.Module) bool {
	for i := range m.Types {
		for j := range m.Types[i].SubTypes {
			ct := &m.Types[i].SubTypes[j].Comp
			if ct.Kind == wasm.CompFunc && len(ct.Results) > 1 {
				return true
			}
		}
	}
	return false
}

// serialStackArenaCap keeps the legacy growth path when lowering may expand one
// scanned instruction into uncounted nodes. This applies to spliced inline
// bodies, extension-owned custom recipes, and GC helper argument construction.
func serialStackArenaCap(m *wasm.Module, hints []funcHints, inlineTargets inlineTargetTable, expandedLowering bool) int {
	if !inlineTargets.empty() || expandedLowering {
		return defaultStackArenaCap
	}
	return moduleStackArenaCap(m, hints)
}

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

func expandedStackLowering(opts CompileOptions) bool {
	return len(opts.CustomInstructions) != 0 || opts.GCTypeSubtypingRefTest || opts.GCStructHelpers || opts.GCArrayHelpers
}

const maxHintedControlFrames = 64

// maxWorkerInitialControlFrames bounds speculative pointer-rich control backing
// retained by every parallel worker. Deeper functions grow only the worker that
// receives them; append remains the correctness fallback.
const maxWorkerInitialControlFrames = 8

// A live control frame can own entry and branch snapshots. Branch tables can
// need thousands of tiny snapshots, so bound their pointer-rich headers and
// payload independently instead of forcing repeated allocation behind a small
// entry-count limit. Explicit slice growth keeps the backing at or below the
// header ceiling, so the two limits retain at most 224 KiB per worker.
const (
	maxRetainedLocStateBufs  = 4096
	maxRetainedLocStateBytes = 128 << 10
)

// Forward-edge overflow starts only after the inline sites. Bound both the
// number and size of recycled buffers so a deeply branched function cannot set
// persistent worker high-water for the rest of module compilation.
const (
	maxRetainedEndsBufs     = maxWorkerInitialControlFrames
	maxRetainedEndsBufSites = 256
)

// moduleControlFrameCap sizes the serial compiler's reusable control stack from
// the same one-pass bytecode hints. Zero preserves lazy allocation for
// straight-line, incomplete, AST-only, or unusually deep modules.
func moduleControlFrameCap(m *wasm.Module, hints []funcHints) int {
	if len(hints) != len(m.Code) {
		return 0
	}
	maxDepth := 0
	for i := range hints {
		if depth := int(hints[i].maxControlDepth); depth > maxDepth {
			maxDepth = depth
		}
	}
	if maxDepth == 0 || maxDepth >= maxHintedControlFrames {
		return 0
	}
	return maxDepth + 1 // implicit function frame
}

func workerControlFrameCap(m *wasm.Module, hints []funcHints) int {
	capHint := moduleControlFrameCap(m, hints)
	if capHint > maxWorkerInitialControlFrames {
		return maxWorkerInitialControlFrames
	}
	return capHint
}

func (sc *scratch) reset() {
	sc.stack.reset()
	sc.asm.B = sc.asm.B[:0]
	sc.asm.LogicalMoveImmediates = 0
	sc.asm.CompactMoveImmediates32 = 0
	sc.directPrepared = false
	sc.retSiteHead = 0
	sc.ctrl = sc.ctrl[:0]
	sc.transient.loopSetLocals = sc.transient.loopSetLocals[:0]
	clear(sc.ctrlMerges[:cap(sc.ctrlMerges)])
	clear(sc.ctrlRoots[:cap(sc.ctrlRoots)])
	sc.ctrlRoots = sc.ctrlRoots[:0]
	for i := range sc.trapSites {
		sc.trapSites[i] = sc.trapSites[i][:0]
	}
	sc.branchTargets = nil
	clear(sc.finalizerMarkers)
	sc.finalFragments = sc.finalFragments[:0]
	sc.fragmentOverflow = false
	sc.deadHoleN = 0
	sc.branchNextN = 0
	sc.singleBitTestN = 0
	sc.deadHoleOverflow = false
	sc.hasBranchTargets = false
	sc.hasPCRelative = false
}

// clearNodeReferences severs scratch-owned links into operand-arena chunks before
// finishFunction drops over-budget chunk headers. Pointer-bearing slice backings are
// scanned to capacity by Go, so clearing only their current length would retain
// nodes from an earlier giant function.
func (sc *scratch) clearNodeReferences() {
	clear(sc.fnState.regUser[:])
	clear(sc.fnState.fregUser[:])
	clear(sc.transient.tmpRoots[:cap(sc.transient.tmpRoots)])
	clear(sc.transient.tmpDeferred[:cap(sc.transient.tmpDeferred)])
}

func (sc *scratch) finishStackFunction() {
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
	scratch      *scratch
	scratchStats shared.WorkerScratchStats
	arena        []byte
	relocs       []callReloc
}

// funcResult is one independently compiled local function. worker/start/end
// identify its owned bytes after the worker pool joins. relocStart/relocEnd name
// the function's records in its worker's append-only relocation arena.
type funcResult struct {
	worker      uint32
	start       uint32
	end         uint32
	relocStart  uint32
	relocEnd    uint32
	bodyBytes   uint32
	internalOff uint32
	adapterOff  uint32 // shared-adapter call or adapter-tail return offset
	adapterEnd  uint32
	trapBody    sharedTrapBodyInfo
	layoutFlags uint8
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
	layoutOmitted
)

func markDirectPrepared(bits []uint64, n, bit int) []uint64 {
	if bits == nil {
		bits = make([]uint64, (n+63)/64)
	}
	bits[bit>>6] |= uint64(1) << uint(bit&63)
	return bits
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

func (f *fn) frameHeaderBytes() int {
	if f.compactFrameHeader {
		return 0
	}
	return frameHdrBytes
}

// prepareCompactGCFrameHeader makes the frontend's collector-local offsets
// consume this function's final local layout. The plan retains local identities
// specifically so this rewrite is allocation-free. Fixed roots belong to the EH
// layout and keep the stable header until those records become layout-relative.
func (f *fn) prepareCompactGCFrameHeader(plan *shared.GCFrameRootPlan) bool {
	if plan == nil {
		return true
	}
	if !plan.Candidate || plan.HasFixedOffsets() {
		return false
	}
	for _, local := range plan.Locals {
		if int(local.Index) >= f.nLocals {
			return false
		}
	}
	for i := range plan.Locals {
		plan.Locals[i].Offset = uint32(f.localOff(int(plan.Locals[i].Index)))
	}
	return true
}

func (f *fn) localOff(i int) int32 { return int32(f.frameHeaderBytes() + 8*int(f.localSlot[i])) }
func (f *fn) ehFrameBytes() int {
	if f.moduleEH {
		return (maxEHTryRecords*ehRecordSlots + maxEHRootRecords*ehRootSlots) * 8
	}
	return 0
}
func (f *fn) ehRecordOff(index int) int32 {
	return int32(f.frameHeaderBytes() + 8*f.nLocalSlots + index*ehRecordSlots*8)
}
func (f *fn) ehRootOff(index int) int32 {
	return int32(f.frameHeaderBytes() + 8*f.nLocalSlots + maxEHTryRecords*ehRecordSlots*8 + index*ehRootSlots*8)
}
func (f *fn) spillOff(k int) int32 {
	return int32(f.frameHeaderBytes() + 8*f.nLocalSlots + f.ehFrameBytes() + 8*k)
}

// frameSize is a multiple of 16: AArch64 requires SP 16-byte aligned at all times.
// Unlike amd64 there is no "+8 bias" — a BL writes the return address into LR
// (nothing is pushed), so a call-making function's `STP X29,X30,[SP,#-16]!` keeps
// 16-alignment and `SUB SP,SP,#frameSize` must stay a multiple of 16 to preserve
// it for our own call sites.
func (f *fn) frameSize() int {
	if f.frameElided {
		return 0
	}
	return align16(f.frameHeaderBytes() + 8*f.nLocalSlots + f.ehFrameBytes() + 8*f.maxSpill)
}

func (f *fn) elideRegisterOnlyFrame() bool {
	voidResult := len(f.ft.Results) == 0
	registerResult := f.singleRegResult || voidResult
	if f.moduleEH || !registerResult || f.usesCalls || f.maxSpill != 0 || len(f.localType) != f.nLocals {
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
	if !f.preserveCallerPins && !(f.opt(optFrameElideRegHomed) && f.allLocalsRegisterHomed()) {
		return false
	}
	f.frameElided = true
	f.stats.peep("frame-adjust-elide")
	if voidResult {
		f.stats.peep("frame-adjust-elide-void")
	}
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

func (f *fn) patchFrameAdjusts() error {
	size := f.frameSize()
	if err := f.validateFrameSize(size); err != nil {
		return err
	}
	addSites := append(f.tailFrameSites, f.addRspAt)
	if f.stats != nil {
		sites := len(addSites) + 1
		f.stats.NativeSize.FrameAdjustmentBytes += 12 * sites
		if f.opt(optSmallFrame) && size <= 4095 {
			deadPerSite := 8
			if size == 0 {
				deadPerSite = 12
			}
			f.stats.NativeSize.DeadFrameReservationBytes += deadPerSite * sites
		}
	}
	if f.opt(optSmallFrame) && size <= 4095 {
		const nop = 0xD503201F
		if size == 0 {
			f.a.PatchU32(f.subRspAt, nop)
			for _, at := range addSites {
				f.a.PatchU32(at, nop)
			}
		} else {
			f.a.PatchU32(f.subRspAt, 0xD10003FF|uint32(size)<<10) // SUB SP,SP,#size
			for _, at := range addSites {
				f.a.PatchU32(at, 0x910003FF|uint32(size)<<10) // ADD SP,SP,#size
			}
			f.stats.peep("small-frame-adjust")
		}
		for _, at := range append(addSites, f.subRspAt) {
			f.a.PatchU32(at+4, nop)
			f.a.PatchU32(at+8, nop)
		}
		return nil
	}
	f.a.PatchMovImm(f.subRspAt, uint32(size))
	for _, at := range addSites {
		f.a.PatchMovImm(at, uint32(size))
	}
	return nil
}

func (f *fn) validateFrameSize(size int) error {
	headroom := nativeFrameStackFenceHeadroom(f.usesCalls)
	if size < 0 || size > headroom {
		return fmt.Errorf("arm64: native frame %d bytes exceeds stack-fence headroom %d", size, headroom)
	}
	return nil
}

func nativeFrameStackFenceHeadroom(usesCalls bool) int {
	overhead := shared.MaxNativeInboundCallBytes
	if usesCalls {
		overhead += 16 // FP/LR record is stored before the body-frame fence check.
	}
	return shared.MaxNativeFrameBytes - overhead
}

// ImportBinding is shared by both Railshot architectures.
type ImportBinding = shared.ImportBinding

// CompileOptions configures direct wasm-to-arm64 compilation.
type CompileOptions struct {
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
	// codegen dashboard). Independent of WAGO_EXPLAIN, which prints the same dump
	// to stderr. nil = no collection, zero overhead.
	Stats *ModuleStats
	// CollectInlineReport enables the additional whole-module analysis used by
	// the human-facing inline report. It is intentionally separate from Stats so
	// ordinary timing and resource collection does not add another body walk.
	CollectInlineReport bool

	// CustomInstructions are currently handled by portable host fallbacks on
	// arm64; the field keeps the backend-neutral compile contract identical.
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

// CompileModuleWith compiles every local function. ElideBoundsChecks elides the
// inline linear-memory bounds check, relying on a guard-page mapping + SIGSEGV
// handler (the caller must back memory with runtime guard pages).
func CompileModuleWith(m *wasm.Module, opts CompileOptions) (*a64.CompiledModule, error) {
	compiled, err := compileModuleWith(m, opts)
	runtime.KeepAlive(m)
	runtime.KeepAlive(opts)
	return compiled, err
}

func compileModuleWith(m *wasm.Module, opts CompileOptions) (*a64.CompiledModule, error) {
	if opts.SyncHostSlots == 0 {
		opts.SyncHostSlots = coreruntime.MaxHostArity
	}
	if opts.SyncHostSlots < coreruntime.MaxHostArity || opts.SyncHostSlots > coreruntime.MaxSyncHostSlots {
		return nil, fmt.Errorf("arm64: synchronous host slot capacity %d is outside %d..%d", opts.SyncHostSlots, coreruntime.MaxHostArity, coreruntime.MaxSyncHostSlots)
	}
	// This is a module invariant. Resolve it once rather than rescanning imports
	// for every function.
	opts.SyncHostCalls = opts.SyncHostCalls || opts.GCStructHelpers || opts.GCArrayHelpers || moduleUsesSyncHostCalls(m, opts.ImportBindings)
	selection, err := optimizationBindings.ResolveSnapshot(opts.Optimizations, opts.OptimizationSnapshot, opts.OptimizationDeltas)
	if err != nil {
		return nil, fmt.Errorf("arm64: %w", err)
	}
	policy := shared.DefaultCodegenPolicy(selection)
	if !nativeCompactionDisabled && opts.CompactNative {
		policy = shared.CompactCodegenPolicy(selection)
	}
	if policy.FunctionAlignLog2 < 2 {
		policy.FunctionAlignLog2 = 2
	}
	if policy.InternalAlignLog2 < 2 {
		policy.InternalAlignLog2 = 2
	}
	if policy.LoopAlignLog2 < 2 {
		policy.LoopAlignLog2 = 2
	}
	guardMode := opts.ElideBoundsChecks
	// P6.1 elision is on unless disabled per-compile (opts) or globally (env).
	boundsFacts := policy.EnabledOption(optBoundsFacts) && !opts.NoBoundsFacts
	n := len(m.Code)
	workers := shared.ResolveWorkers(opts.Workers, n, runtime.GOMAXPROCS(0))
	entry, internalEntry := shared.ModuleEntries(n)
	importedFuncs := m.ImportedFuncCount()
	nGlobals := m.GlobalCount()
	var hintStart time.Time
	if opts.Stats != nil || explainEnabled {
		hintStart = time.Now()
	}
	allHints, hintSidecar, globalScores, err := computeModuleHintsWithWorkersPolicy(m, nGlobals, importedFuncs, workers, policy)
	if err != nil {
		return nil, fmt.Errorf("arm64: %w", err)
	}
	relocCap := 0
	for i := range allHints {
		relocCap += int(allHints[i].callRelocSites)
	}
	if relocCap < minPreallocatedCallRelocs {
		relocCap = 0
	}
	var hintNanos uint64
	if !hintStart.IsZero() {
		hintNanos = uint64(time.Since(hintStart))
	}
	immutableTable := computeImmutableTableHint(m, allHints, policy)
	modGlobals := pickModuleGlobals(m, nGlobals, globalScores)
	hostAdapters, err := shared.HostAdapterSet(m)
	if err != nil {
		return nil, fmt.Errorf("arm64: host adapter analysis: %w", err)
	}
	if policy.EnabledOption(optRegABI) {
		// A wrapper-ABI caller can tail-enter any exact-funcref result target.
		// Retain offset-zero adapters for this staged register-ABI class so mixed
		// wide-caller/narrow-target tails still write through the caller's X3
		// destination instead of returning an unconsumed X0 value.
		for i := range m.Code {
			ft, ok := m.FuncSignature(uint32(importedFuncs + i))
			if ok && sigFitsReferenceResultRegABI(ft) {
				hostAdapters[i] = true
			}
		}
	}
	if len(opts.CustomInstructions) != 0 {
		for i := range hostAdapters {
			hostAdapters[i] = true
		}
	}
	// Stats collection is opt-in: an explicit sink (opts.Stats) or WAGO_EXPLAIN=1.
	// nil ms => st stays nil in the loop => zero-overhead counter no-ops.
	var ms *ModuleStats
	if opts.Stats != nil {
		ms = opts.Stats
	} else if explainEnabled {
		ms = &ModuleStats{}
	}
	if ms != nil {
		hintHeaderBytes, hintSidecarBytes := funcHintStorageBytes(allHints, hintSidecar)
		*ms = ModuleStats{
			Funcs:            make([]*CodegenStats, n),
			ModuleGlobalPins: moduleGlobalPinInfos(modGlobals),
			Compile: shared.CompileResourceStats{
				HintHeaderBytes:  hintHeaderBytes,
				HintSidecarBytes: hintSidecarBytes,
			},
		}
		ms.Compile.StageNanos[shared.CompileStageHints] = hintNanos
		if opts.CollectInlineReport || explainEnabled {
			// Inline-candidate detection is report-only. Failure to analyze is
			// non-fatal because it never changes code generation.
			if rep, ierr := analyzeInlineCandidates(m, policy); ierr == nil {
				ms.Inline = rep
			}
		}
	}
	// Compile scratch reused across every function in the module. The operand
	// stack arena and the occurrence-tracking refs map are per-function scratch
	// that never outlives a function's compile, so resetting them (rather than
	// allocating fresh per function) removes the largest compile allocations.
	// Compile is sequential, so sharing one scratch is safe.
	// Auto-inlining (WAGO_INLINE): the straight-line leaf callees to splice at their
	// call sites, keyed by global func index. Empty when inlining is disabled.
	inlineTargets := buildInlineTargets(m, allHints, policy)
	for i := range allHints {
		ft, ok := m.LocalFuncType(i)
		if ok {
			allHints[i].flags.assign(hintPreservesCallerPins, preservesCallerPins(ft, int(allHints[i].localCount), allHints[i]))
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
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	if workers <= 1 {
		relocs := newCallRelocTable(n, relocCap)
		// Keep the serial compiler as a distinct fast path: one reusable scratch,
		// no goroutines, channels, atomics, worker metadata, or intermediate arena.
		expandedLowering := expandedStackLowering(opts)
		sc := newScratchWithStackCap(serialStackArenaCap(m, allHints, inlineTargets, expandedLowering))
		sc.classifier = classifier
		sc.reserveLocalScratch(serialLocalScratchCapacity(allHints, inlineTargets, hostAdapters))
		if ctrlCap := moduleControlFrameCap(m, allHints); ctrlCap != 0 {
			sc.reserveControlFrames(ctrlCap)
		}
		codeBuffer, err := coreruntime.NewCodeBuffer(codeCap)
		if err != nil {
			return nil, fmt.Errorf("arm64: allocate code image: %w", err)
		}
		keepCodeBuffer := false
		defer func() {
			if !keepCodeBuffer {
				_ = codeBuffer.Close()
			}
		}()
		pressureDone := false
		var directPrepared []uint64
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
		pressureAt := shared.PressureThreshold(opts.MemoryPressureAt, codeCap)
		for i := range m.Code {
			var st *CodegenStats
			if ms != nil {
				st = &CodegenStats{FuncIdx: i, Name: funcDisplayName(m, i, importedFuncs)}
				ms.Funcs[i] = st
			}
			if inlineTargets.omitStandaloneBody(i, hostAdapters[i]) {
				st.peep("inline-dead-body")
				if !relocs.appendFunction(i, nil) {
					return nil, fmt.Errorf("arm64: module call relocations exceed 32-bit range")
				}
				continue
			}
			// Align and reserve before lowering so the assembler can emit straight
			// into the module-owned image. If an unusually large function outgrows
			// the mapping tail, CommitTail rejects the detached slice and Append
			// preserves the old capacity-underestimate fallback.
			if pad := functionStartPadding(len(codeBuffer.Bytes()), len(m.Code[i].BodyBytes), hostAdapters[i], allHints[i], policy); pad != 0 {
				if err := codeBuffer.AppendZeros(pad); err != nil {
					return nil, fmt.Errorf("arm64: grow code image: %w", err)
				}
			}
			code := codeBuffer.Bytes()
			entry[i] = len(code)
			tail, err := codeBuffer.AppendTail(asmCapForBody(len(m.Code[i].BodyBytes)))
			if err != nil {
				return nil, fmt.Errorf("arm64: grow code image: %w", err)
			}
			sc.asm.B = tail
			hints := hintSidecar.view(allHints[i])
			fnCode, rl, internalOff, err := compileFunc(m, opts.Codegen.Module.GCTypeLayouts, i, hostAdapters[i], guardMode, boundsFacts, opts.Interruptible, modGlobals, &hints, immutableTable, opts.ImportBindings, opts.SyncHostCalls, opts.SyncHostSlots, opts.GCTypeSubtypingRefTest, opts.GCStructHelpers, opts.GCArrayHelpers, opts.GCFrameRoots.Function(i), opts.CustomInstructions, st, inlineTargets, allHints, policy, sc)
			if err != nil {
				return nil, fmt.Errorf("arm64: function %d: %w", i, err)
			}
			internalEntry[i] = len(code) + internalOff
			if adapterTails != nil && sc.fnState.adapterReturnOff != 0 {
				if info := sc.fnState.adapterTailInfo(); info.returnOff != 0 {
					info.function = uint32(i)
					adapterTails = append(adapterTails, info)
				}
			}
			if adapters != nil {
				if info := sc.fnState.sharedAdapterInfo(); info.endOff != 0 {
					info.function = uint32(i)
					adapters = append(adapters, info)
				}
			}
			if hostAdapters[i] {
				trapBodyCluster.reset()
			}
			if policy.EnabledOption(optSharedTrapBody) && policy.CompactNative {
				fnCode = trapBodyCluster.share(codeBuffer.Bytes(), fnCode, entry[i], sc.fnState.sharedTrapBodyInfo(), st)
			}
			if sc.directPrepared {
				directPrepared = markDirectPrepared(directPrepared, n, i)
			}
			if !relocs.appendFunction(i, rl) {
				return nil, fmt.Errorf("arm64: module call relocations exceed 32-bit range")
			}
			if !codeBuffer.CommitTail(fnCode) {
				if err := codeBuffer.Append(fnCode); err != nil {
					return nil, fmt.Errorf("arm64: grow code image: %w", err)
				}
			}
			if !pressureDone && opts.MemoryPressure != nil && len(codeBuffer.Bytes()) >= pressureAt {
				pressureDone = true
				opts.MemoryPressure()
			}
		}
		moduleOther := 0
		if adapters != nil {
			var sharedBytes int
			sharedBytes, err = shareAdaptersCodeBuffer(codeBuffer, entry, internalEntry, &relocs, adapters, opts.GCFrameRoots, ms)
			if err != nil {
				return nil, err
			}
			moduleOther += sharedBytes
		} else if adapterTails != nil {
			var sharedBytes int
			sharedBytes, err = shareAdapterTailsCodeBuffer(codeBuffer, entry, internalEntry, &relocs, adapterTails, opts.GCFrameRoots, ms)
			if err != nil {
				return nil, err
			}
			moduleOther += sharedBytes
		}
		code := codeBuffer.Bytes()
		if err := finalizeOmittedInlineEntries(entry, internalEntry, &relocs, hostAdapters, inlineTargets); err != nil {
			return nil, err
		}
		finalizeModuleNativeSize(ms, len(code), moduleOther, len(codeBuffer.Mapping()))
		if err := patchCallRelocs(code, entry, internalEntry, &relocs); err != nil {
			return nil, err
		}
		ms.setNodeScratchStats(sc)
		ms.finalizeCompileResourceStats()
		if explainEnabled && ms != nil {
			fmt.Fprint(os.Stderr, ms.String())
		}
		keepCodeBuffer = true
		return &a64.CompiledModule{Code: code, CodeImage: codeBuffer, Entry: entry, InternalEntry: internalEntry, DirectPrepared: directPrepared}, nil
	}

	return compileModuleParallel(m, opts, workers, codeCap, entry, internalEntry, allHints, hintSidecar, immutableTable, modGlobals, hostAdapters, inlineTargets, policy, ms, guardMode, boundsFacts, importedFuncs)
}

func serialLocalScratchCapacity(allHints []funcHints, inlineTargets inlineTargetTable, hostAdapters []bool) int {
	maxLocals := 0
	for i := range allHints {
		if inlineTargets.omitStandaloneBody(i, hostAdapters[i]) {
			continue
		}
		// localCount is the complete parameter-plus-declared-local population.
		maxLocals = max(maxLocals, int(allHints[i].localCount))
	}
	return maxLocals
}

// compileModuleParallel is split from CompileModuleWith so the goroutine closure
// and its captured state cannot escape into or add allocations to the serial path.
func compileModuleParallel(m *wasm.Module, opts CompileOptions, workers, codeCap int, entry, internalEntry []int, allHints []funcHints, hintSidecar funcHintSidecar, immutableTable immutableTableHint, modGlobals []moduleGlobalPin, hostAdapters []bool, inlineTargets inlineTargetTable, policy CodegenPolicy, ms *ModuleStats, guardMode, boundsFacts bool, importedFuncs int) (*a64.CompiledModule, error) {
	n := len(m.Code)
	if ms != nil {
		for i := range m.Code {
			ms.Funcs[i] = &CodegenStats{FuncIdx: i, Name: funcDisplayName(m, i, importedFuncs)}
		}
	}
	states := make([]workerState, workers)
	arenaCap := (codeCap + workers - 1) / workers
	expandedLowering := expandedStackLowering(opts)
	stackCap := workerStackArenaCap(m, allHints, inlineTargets, expandedLowering)
	ctrlCap := workerControlFrameCap(m, allHints)
	pressureAt := shared.PressureThreshold(opts.MemoryPressureAt, codeCap)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var pressureBytes atomic.Int64
	var pressureOnce sync.Once
	for i := range states {
		states[i] = workerState{scratch: newScratchWithStackCap(stackCap), arena: make([]byte, 0, arenaCap)}
		states[i].scratch.classifier = classifier
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
	for workerID := range states {
		go func(workerID int) {
			defer wg.Done()
			ws := &states[workerID]
			defer func() {
				ws.scratch.finishControlWorker()
				ws.scratch.finishStackWorker()
				ws.scratchStats = workerScratchStats(ws.scratch)
				ws.scratch = nil
			}()
			for {
				i := int(work.next.Add(1) - 1)
				if i >= n {
					return
				}
				var st *CodegenStats
				if ms != nil {
					st = ms.Funcs[i]
				}
				if inlineTargets.omitStandaloneBody(i, hostAdapters[i]) {
					st.peep("inline-dead-body")
					results[i] = funcResult{layoutFlags: layoutOmitted}
					continue
				}
				layoutFlags := boolFlag(hostAdapters[i], layoutHostAdapter) | boolFlag(allHints[i].flags.has(hintHasLoop), layoutHasLoop) | boolFlag(allHints[i].flags.has(hintHasCall), layoutHasCall) | boolFlag(allHints[i].flags.has(hintCallsSelf), layoutCallsSelf)
				hints := hintSidecar.view(allHints[i])
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
				ws.arena = append(ws.arena, fnCode...)
				result := funcResult{worker: uint32(workerID), start: compactStart, end: compactEnd, relocStart: compactRelocStart, relocEnd: compactRelocEnd, bodyBytes: bodyBytes, layoutFlags: layoutFlags, internalOff: compactInternalOff}
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
		}(workerID)
	}
	wg.Wait()
	if i, err := work.failures.Result(); err != nil {
		return nil, fmt.Errorf("arm64: function %d: %w", i, err)
	}
	relocs := parallelCallRelocTable(results, states)

	code := make([]byte, 0, codeCap)
	var directPrepared []uint64
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
		if r.layoutFlags&layoutOmitted != 0 {
			continue
		}
		if pad := functionStartPaddingFlags(len(code), int(r.bodyBytes), r.layoutFlags, policy); pad != 0 {
			code = append(code, alignPad[:pad]...)
		}
		entry[i] = len(code)
		internalEntry[i] = len(code) + int(r.internalOff)
		if r.layoutFlags&layoutDirectPrepared != 0 {
			directPrepared = markDirectPrepared(directPrepared, n, i)
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
			if ms != nil {
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
	if err := finalizeOmittedInlineEntries(entry, internalEntry, &relocs, hostAdapters, inlineTargets); err != nil {
		return nil, err
	}
	if err := patchCallRelocs(code, entry, internalEntry, &relocs); err != nil {
		return nil, err
	}
	finalizeModuleNativeSize(ms, len(code), moduleOther, 0)
	if ms != nil {
		for i := range states {
			ms.Compile.AddWorkerScratch(states[i].scratchStats)
		}
	}
	ms.finalizeCompileResourceStats()
	if explainEnabled && ms != nil {
		fmt.Fprint(os.Stderr, ms.String())
	}
	return &a64.CompiledModule{Code: code, Entry: entry, InternalEntry: internalEntry, DirectPrepared: directPrepared}, nil
}

// finalizeOmittedInlineEntries closes the module-layout seam for standalone
// bodies proved unreachable by compact inlining. Any surviving relocation fails
// closed. Entry metadata remains structurally valid by aliasing omitted logical
// functions to one retained internal entry; the proof guarantees it is never
// observed by Wasm or the host.
func finalizeOmittedInlineEntries(entry, internalEntry []int, relocs *callRelocTable, hostAdapters []bool, targets inlineTargetTable) error {
	if len(entry) == 0 {
		return nil
	}
	anchor := -1
	for i := range entry {
		if !targets.omitStandaloneBody(i, hostAdapters[i]) {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		return fmt.Errorf("arm64: every local function was marked as an omitted inline body")
	}
	for caller := 0; caller < relocs.functions(); caller++ {
		functionRelocs := relocs.serialFunction(caller)
		if relocs.results != nil {
			functionRelocs = relocs.parallelFunction(caller)
		}
		for _, rl := range functionRelocs {
			target := int(rl.target)
			if rl.target != invalidCallRelocField && target < len(entry) && targets.omitStandaloneBody(target, hostAdapters[target]) {
				return fmt.Errorf("arm64: function %d retains relocation to omitted inline body %d", caller, rl.target)
			}
		}
	}
	alias := internalEntry[anchor]
	for i := range entry {
		if targets.omitStandaloneBody(i, hostAdapters[i]) {
			entry[i], internalEntry[i] = alias, alias
		}
	}
	return nil
}

func patchCallRelocs(code []byte, entry, internalEntry []int, relocs *callRelocTable) error {
	if len(entry) < relocs.functions() {
		return fmt.Errorf("arm64: relocation entry table has %d functions, want at least %d", len(entry), relocs.functions())
	}
	asm := &a64.Asm{B: code}
	for i := 0; i < relocs.functions(); i++ {
		base := entry[i]
		functionRelocs := relocs.serialFunction(i)
		if relocs.results != nil {
			functionRelocs = relocs.parallelFunction(i)
		}
		for _, rl := range functionRelocs {
			if rl.at == invalidCallRelocField || base < 0 || base > len(code)-4 || uint64(rl.at) > uint64(len(code)-base-4) {
				return fmt.Errorf("arm64: invalid relocation site %#x in function %d for %d-byte code image", rl.at, i, len(code))
			}
			targetIndex := int(rl.target)
			if rl.target == invalidCallRelocField || targetIndex >= len(entry) {
				return fmt.Errorf("arm64: invalid call relocation target %d for function %d", rl.target, i)
			}
			site := base + int(rl.at)
			target := entry[targetIndex]
			if rl.internal {
				if targetIndex >= len(internalEntry) {
					return fmt.Errorf("arm64: missing internal entry for call relocation target %d in function %d", rl.target, i)
				}
				target = internalEntry[targetIndex]
			}
			if !asm.PatchBranch26(site, target) {
				return fmt.Errorf("arm64 direct call relocation from function %d at %#x to function %d at %#x exceeds BL range", i, site, rl.target, target)
			}
		}
	}
	return nil
}

func finalizeModuleNativeSize(ms *ModuleStats, codeLen, moduleOther, mappedBytes int) {
	if ms == nil {
		return
	}
	var native shared.NativeSizeReport
	native.TotalBytes = codeLen
	for _, fn := range ms.Funcs {
		if fn != nil {
			native.AddFunction(fn.NativeSize)
		}
	}
	type adapterShape struct {
		hash  uint64
		bytes int
	}
	shapes := make(map[adapterShape]struct{}) // stats-only
	for _, fn := range ms.Funcs {
		if fn == nil || fn.NativeSize.HostAdapterBytes == 0 {
			continue
		}
		native.HostAdapterCount++
		shape := adapterShape{fn.NativeSize.HostAdapterShapeHash, fn.NativeSize.HostAdapterBytes}
		if _, ok := shapes[shape]; ok {
			continue
		}
		shapes[shape] = struct{}{}
		native.HostAdapterUniqueBytes += shape.bytes
	}
	native.HostAdapterShapeCount = len(shapes)
	native.HostAdapterDuplicateBytes = native.HostAdapterBytes - native.HostAdapterUniqueBytes
	tailShapes := make(map[adapterShape]struct{}) // stats-only
	tailBytes := 0
	for _, fn := range ms.Funcs {
		if fn == nil || fn.NativeSize.HostAdapterTailBytes == 0 {
			continue
		}
		tailBytes += fn.NativeSize.HostAdapterTailBytes
		shape := adapterShape{fn.NativeSize.HostAdapterTailShapeHash, fn.NativeSize.HostAdapterTailBytes}
		if _, ok := tailShapes[shape]; ok {
			continue
		}
		tailShapes[shape] = struct{}{}
		native.HostAdapterTailUniqueBytes += shape.bytes
	}
	native.HostAdapterTailShapeCount = len(tailShapes)
	native.HostAdapterTailDuplicateBytes = tailBytes - native.HostAdapterTailUniqueBytes
	native.ModuleOtherBytes = moduleOther
	native.FunctionAlignmentBytes = codeLen - native.FunctionBytes - moduleOther
	if native.FunctionAlignmentBytes < 0 {
		native.ModuleOtherBytes = native.FunctionAlignmentBytes
		native.FunctionAlignmentBytes = 0
	}
	ms.NativeSize = native
	ms.NativeSize.SetExecutableMapping(codeLen, mappedBytes)
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
	return computeModuleHintsWithPolicy(m, nGlobals, importedFuncs, currentCodegenPolicy())
}

func computeModuleHintsWithPolicy(m *wasm.Module, nGlobals, importedFuncs int, policy CodegenPolicy) ([]funcHints, funcHintSidecar, []int64, error) {
	return computeModuleHintsWithWorkersPolicy(m, nGlobals, importedFuncs, 1, policy)
}

func computeModuleHintsWithWorkersPolicy(m *wasm.Module, nGlobals, importedFuncs, workers int, policy CodegenPolicy) ([]funcHints, funcHintSidecar, []int64, error) {
	n := len(m.Code)
	allHints := make([]funcHints, n)
	totalScores := 0
	intervalLocals := 0
	intervalFunctions := 0
	moduleEH := m.TagCount() != 0
	storageModuleEH := moduleEH
	for i := range m.Code {
		ft, ok := m.LocalFuncType(i)
		if !ok {
			return nil, funcHintSidecar{}, nil, fmt.Errorf("function %d hints: unknown function type", i)
		}
		count, err := countLocals(ft.Params, m.Code[i].Locals)
		if err != nil {
			return nil, funcHintSidecar{}, nil, fmt.Errorf("function %d hints: %w", i, err)
		}
		intervalStorage := intervalRegionHintStorageEligible(policy.EnabledOption(optIntervalRegionPins), len(m.Code[i].BodyBytes), count, storageModuleEH)
		scoreCount := count
		if count > 64 && !intervalStorage {
			scoreCount = 64
		}
		if scoreCount > int(^uint(0)>>1)-totalScores {
			return nil, funcHintSidecar{}, nil, fmt.Errorf("function hint locals overflow")
		}
		allHints[i].localCount = uint16(count)
		allHints[i].flags.assign(hintIntervalRegionStorage, intervalStorage)
		totalScores += scoreCount
		if intervalStorage {
			if count > int(^uint(0)>>1)-intervalLocals {
				return nil, funcHintSidecar{}, nil, fmt.Errorf("function hint interval locals overflow")
			}
			intervalLocals += count
			intervalFunctions++
		}
	}
	if uint64(totalScores) > uint64(^uint32(0)) || uint64(intervalLocals) > uint64(^uint32(0)) {
		return nil, funcHintSidecar{}, nil, fmt.Errorf("function hint local sidecar exceeds 32-bit index capacity")
	}
	localScores := make([]uint32, totalScores)
	// Dense storage needs no per-function index. Use sparse ranges only when
	// their index plus eligible-local payload is strictly smaller, so this
	// representation cannot increase retained last-get bytes on modules where
	// most functions qualify for interval regions.
	denseLastGets := uint64(totalScores) * 4
	sparseLastGets := uint64(intervalLocals)*4 + uint64(intervalFunctions)*8
	compactLastGets := sparseLastGets < denseLastGets
	lastGetCount := totalScores
	if compactLastGets {
		lastGetCount = intervalLocals + intervalFunctions*2
	}
	localLastGets := make([]uint32, lastGetCount)
	var sparseGlobals []shared.GlobalHint
	var agg []int64
	if nGlobals > 0 && n > 0 {
		agg = make([]int64, nGlobals)
	}
	intervalRangeAt := 0
	if parallelHintScanEligible(m, nGlobals, workers) {
		var parallelEH bool
		var err error
		sparseGlobals, intervalRangeAt, parallelEH, err = scanModuleHintsParallel(m, nGlobals, importedFuncs, workers, allHints, localScores, localLastGets, compactLastGets, intervalFunctions)
		if err != nil {
			return nil, funcHintSidecar{}, nil, err
		}
		moduleEH = moduleEH || parallelEH
		for _, gh := range sparseGlobals {
			agg[gh.Index] += int64(gh.Score)
		}
	} else {
		var sparseAccum shared.GlobalHintAccumulator
		eligibilityTracker := newGlobalEligibilityTracker(nGlobals)
		scoreAt := 0
		intervalAt := intervalFunctions * 2
		classifier := wasm.NewModuleInstructionClassifier(m, true)
		for i := range m.Code {
			nLocals := int(allHints[i].localCount)
			intervalStorage := allHints[i].flags.has(hintIntervalRegionStorage)
			scoreCount := retainedLocalScoreCount(allHints[i])
			sparseAccum.Reset(nGlobals)
			h := funcHintsWithStorage(localScores[scoreAt : scoreAt+scoreCount])
			if compactLastGets {
				if intervalStorage {
					h.localLastGet = localLastGets[intervalAt : intervalAt+nLocals]
					localLastGets[intervalRangeAt] = uint32(scoreAt)
					localLastGets[intervalRangeAt+1] = uint32(intervalAt)
					intervalRangeAt += 2
					intervalAt += nLocals
				}
			} else {
				if intervalStorage {
					h.localLastGet = localLastGets[scoreAt : scoreAt+nLocals]
				}
			}
			h.nLocals = nLocals
			h.localCount = uint16(nLocals)
			h.localStart = uint32(scoreAt)
			h.inlineCallSites = allHints[i].inlineCallSites
			h.directCallRefs = allHints[i].directCallRefs
			h.flags.assign(hintHasInlineLoopCall, allHints[i].flags.has(hintHasInlineLoopCall))
			var err error
			h, err = scanFuncBodyIntoModule(m.Code[i], nLocals, nGlobals, uint32(importedFuncs+i), m.BranchHintsForFunc(uint32(importedFuncs+i)), h, &eligibilityTracker, m, &classifier, allHints, importedFuncs, &sparseAccum)
			if err != nil {
				return nil, funcHintSidecar{}, nil, fmt.Errorf("function %d hints: %w", i, err)
			}
			h.inlineCallSites = allHints[i].inlineCallSites
			h.directCallRefs = allHints[i].directCallRefs
			h.flags.assign(hintHasInlineLoopCall, allHints[i].flags.has(hintHasInlineLoopCall))
			h.flags.assign(hintIntervalRegionStorage, intervalStorage)
			scoreAt += scoreCount
			moduleEH = moduleEH || h.flags.has(hintModuleEH)
			allHints[i] = h.funcHints
			start := len(sparseGlobals)
			sparseGlobals = sparseAccum.AppendTo(sparseGlobals)
			if uint64(len(sparseGlobals)) > uint64(^uint32(0)) {
				return nil, funcHintSidecar{}, nil, fmt.Errorf("function hint global sidecar exceeds 32-bit index capacity")
			}
			allHints[i].globalStart = uint32(start)
			allHints[i].globalCount = uint32(len(sparseGlobals) - start)
			for _, gh := range sparseGlobals[start:] {
				agg[gh.Index] += int64(gh.Score)
			}
		}
	}
	if moduleEH {
		for i := range allHints {
			allHints[i].flags.set(hintModuleEH)
		}
	}
	if moduleEH && !storageModuleEH {
		localScores = compactEHLocalScores(allHints, localScores)
		localLastGets = nil
		intervalRangeAt = 0
	}
	return allHints, funcHintSidecar{
		localScore:             localScores,
		localLastGet:           localLastGets,
		sparseGlobals:          sparseGlobals,
		localLastGetRangeCount: uint32(intervalRangeAt / 2),
	}, agg, nil
}

const (
	minParallelHintBodyBytes     = 16 << 10
	maxParallelHintGlobalEntries = 1 << 18
)

// parallelHintScanEligible bounds the extra dense global scratch retained by
// workers and leaves tiny or programmatically constructed modules on the
// allocation-minimal serial path.
func parallelHintScanEligible(m *wasm.Module, nGlobals, workers int) bool {
	if workers <= 1 || len(m.Code) < workers*2 || uint64(nGlobals)*uint64(workers) > maxParallelHintGlobalEntries {
		return false
	}
	bodyBytes := 0
	for i := range m.Code {
		if len(m.Code[i].BodyBytes) == 0 {
			return false
		}
		bodyBytes += len(m.Code[i].BodyBytes)
	}
	return bodyBytes >= minParallelHintBodyBytes
}

type parallelCalleeHints struct {
	direct atomic.Uint32
	inline atomic.Uint32
	loop   atomic.Bool
}

type parallelHintRange struct {
	worker uint32
	start  uint32
	end    uint32
}

type parallelHintWorker struct {
	elig       globalEligibilityTracker
	globals    shared.GlobalHintAccumulator
	retained   []shared.GlobalHint
	classifier wasm.ModuleInstructionClassifier
}

func scanModuleHintsParallel(m *wasm.Module, nGlobals, importedFuncs, workers int, allHints []funcHints, localScores, localLastGets []uint32, compactLastGets bool, intervalFunctions int) (sparseGlobals []shared.GlobalHint, intervalRangeAt int, moduleEH bool, err error) {
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

	states := make([]parallelHintWorker, workers)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range states {
		states[i].elig = newGlobalEligibilityTracker(nGlobals)
		states[i].globals.Reset(nGlobals)
		states[i].classifier = classifier
	}
	calleeHints := make([]parallelCalleeHints, len(allHints))
	ranges := make([]parallelHintRange, len(allHints))
	var next atomic.Int64
	var failures shared.LowestIndexError
	failures.Reset(len(allHints))
	var wg sync.WaitGroup
	wg.Add(workers)
	for workerID := range states {
		go func(workerID int) {
			defer wg.Done()
			state := &states[workerID]
			for {
				i := int(next.Add(1) - 1)
				if i >= len(allHints) {
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
				state.globals.Reset(nGlobals)
				h, scanErr := scanBodyBytesIntoModule(m.Code[i].BodyBytes, m.Code[i].LocalDeclBytes, nLocals, nGlobals, uint32(importedFuncs+i), m.BranchHintsForFunc(uint32(importedFuncs+i)), h, &state.elig, m, &state.classifier, nil, calleeHints, importedFuncs, &state.globals)
				if scanErr != nil {
					failures.Record(i, fmt.Errorf("function %d hints: %w", i, scanErr))
					continue
				}
				h.flags.assign(hintIntervalRegionStorage, base.flags.has(hintIntervalRegionStorage))
				allHints[i] = h.funcHints
				start := len(state.retained)
				state.retained = state.globals.AppendTo(state.retained)
				if uint64(len(state.retained)) > uint64(^uint32(0)) {
					failures.Record(i, fmt.Errorf("function %d hints: worker global sidecar exceeds 32-bit index capacity", i))
					continue
				}
				ranges[i] = parallelHintRange{worker: uint32(workerID), start: uint32(start), end: uint32(len(state.retained))}
			}
		}(workerID)
	}
	wg.Wait()
	if _, err := failures.Result(); err != nil {
		return nil, 0, false, err
	}

	totalGlobals := 0
	for i := range ranges {
		totalGlobals += int(ranges[i].end - ranges[i].start)
		if uint64(totalGlobals) > uint64(^uint32(0)) {
			return nil, 0, false, fmt.Errorf("function hint global sidecar exceeds 32-bit index capacity")
		}
	}
	// Append function ranges in source order from a nil slice so retained
	// capacity matches the serial accumulator's deterministic growth policy.
	// Compile resource accounting reports backing capacity, not only live bytes.
	sparseGlobals = nil
	for i := range allHints {
		r := ranges[i]
		stateGlobals := states[r.worker].retained[r.start:r.end]
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
	return sparseGlobals, intervalRangeAt, moduleEH, nil
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

// computeImmutableTableHint derives the module-owned table proof after the
// function scans have established whether any body mutates table zero.
func computeImmutableTableHint(m *wasm.Module, allHints []funcHints, policy CodegenPolicy) immutableTableHint {
	immutableLocalTable := policy.EnabledOption(optImmutableTable) &&
		m.ImportedTableCount() == 0 && len(m.Tables) == 1 && !moduleExportsTable(m)
	if immutableLocalTable {
		for i := range allHints {
			if allHints[i].flags.has(hintMutatesTable) {
				immutableLocalTable = false
				break
			}
		}
	}
	if immutableLocalTable {
		tableType, tableTyped := immutableLocalTableTypeWithPolicy(m, policy)
		return immutableTableHint{
			local:             true,
			typeKey:           tableType,
			typed:             tableTyped,
			monomorphicTarget: immutableLocalTableTarget(m),
		}
	}
	return immutableTableHint{monomorphicTarget: -1}
}

// immutableLocalTableTarget returns the sole local function stored in table 0,
// or -1 when entries may name different functions (or use expression forms the
// narrow specialization does not prove). The immutable-table preconditions are
// checked by computeImmutableTableHint before this helper is used.
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
	return immutableLocalTableTypeWithPolicy(m, currentCodegenPolicy())
}

func immutableLocalTableTypeWithPolicy(m *wasm.Module, policy CodegenPolicy) (uint64, bool) {
	if !policy.EnabledOption(optImmutableTableType) || len(m.Tables) != 1 || m.Tables[0].Init != nil {
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

// regExhausted is the internal sentinel raised when a lowering step blocks the
// whole register file after every spillable value has been homed.
type regExhausted struct{ class string }

// Below this count, an optionally inlined call can erase the only relocation
// and ordinary append growth crosses too few size classes to repay a reserve.
const minPreallocatedCallRelocs = 8

// compileFunc compiles one function exactly once. Its target-derived transient
// register floor prevents optional whole-function pins from forcing a retry.
func compileFunc(m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, funcIdx int, hostAdapter, guardMode, boundsFacts, interruptible bool, modGlobals []moduleGlobalPin, hints *funcHintView, immutableTable immutableTableHint, importBindings []ImportBinding, syncHostCalls bool, syncHostSlots int, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers bool, gcFrameRoots *shared.GCFrameRootPlan, customInstructions map[uint32]railcore.CustomInstruction, stats *CodegenStats, inlineTargets inlineTargetTable, calleeHints []funcHints, policy CodegenPolicy, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
	var compileStart time.Time
	if stats != nil {
		stats.FunctionAttempts++
		compileStart = time.Now()
		defer func() { stats.CompileNanos += uint64(time.Since(compileStart)) }()
	}
	if gcFrameRoots != nil && gcFrameRoots.Candidate {
		gcFrameRoots.Exact = true
		gcFrameRoots.ResetSafepoints()
		gcFrameRoots.ResetCallsites()
		gcFrameRoots.FrameBytes = 0
		gcFrameRoots.AdapterReturnOffset = 0
	}
	code, relocs, internalOff, err = compileFuncAttempt(m, gcTypeLayouts, funcIdx, hostAdapter, guardMode, boundsFacts, interruptible, modGlobals, hints, immutableTable, importBindings, syncHostCalls, syncHostSlots, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers, gcFrameRoots, customInstructions, stats, true, inlineTargets, calleeHints, policy, sc)
	if len(sc.stack.chunks) > 1 {
		sc.finishStackFunction()
	}
	if stats != nil {
		sc.noteControlScratch()
	}
	return
}

func compileFuncAttempt(m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, funcIdx int, hostAdapter, guardMode, boundsFacts, interruptible bool, modGlobals []moduleGlobalPin, hints *funcHintView, immutableTable immutableTableHint, importBindings []ImportBinding, syncHostCalls bool, syncHostSlots int, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers bool, gcFrameRoots *shared.GCFrameRootPlan, customInstructions map[uint32]railcore.CustomInstruction, stats *CodegenStats, pinLocals bool, inlineTargets inlineTargetTable, calleeHints []funcHints, policy CodegenPolicy, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
	defer func() {
		if r := recover(); r != nil {
			if exhausted, ok := r.(regExhausted); ok {
				err = fmt.Errorf("arm64: no %s register available after applying the transient register floor", exhausted.class)
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
	// Module hint construction already validated and counted the parameters and
	// local runs. Reuse that result.
	nLocals := hints.nLocals

	sc.reset()
	sc.asm.DenseIdxDisp = hints.memOps >= 8
	sc.asm.DisableLogicalMoveImmediate = !logicalMoveImmediateEnabled ||
		!policy.CompactNative
	sc.asm.DisableCompactMoveImmediate32 = !compactMoveImmediate32Enabled ||
		!policy.CompactNative
	sc.asm.Grow(asmCapForBody(len(c.BodyBytes)))
	globalIdx := m.ImportedFuncCount() + funcIdx
	f := &sc.fnState
	localType, localSlot, locals, globalReg := f.localType, f.localSlot, f.locals, f.globalReg
	mt0, _ := m.MemoryType(0)
	*f = fn{a: sc.asm, s: sc.stack, sc: sc, m: m, ft: ft, gcTypeLayouts: gcTypeLayouts, classifier: sc.classifier, transient: sc.transient, traceFuncIdx: uint32(globalIdx), tracePCBase: c.LocalDeclBytes, customInstructions: customInstructions, nParams: len(ft.Params), nLocals: nLocals, localType: localType, localSlot: localSlot, locals: locals, globalReg: globalReg[:0], guardMode: guardMode, boundsFacts: boundsFacts, interruptible: interruptible, hasLoop: hints.flags.has(hintHasLoop), gcStructHelpers: gcStructHelpers, gcArrayHelpers: gcArrayHelpers, gcFrameRoots: gcFrameRoots, moduleEH: hints.flags.has(hintModuleEH), regMerge: policy.EnabledOption(optRegMerge), globalCellReg: regNone, memSizeReg: regNone, immutableLocalTable: immutableTable.local, immutableTableType: immutableTable.typeKey, immutableTableTyped: immutableTable.typed, monomorphicTarget: immutableTable.monomorphicTarget, importBindings: importBindings, stagedTailDescriptors: true, stats: stats, policy: policy, branchHints: m.BranchHintsForFunc(uint32(globalIdx)), branchHintLocalDecl: c.LocalDeclBytes, calleeHints: calleeHints, threadedMemory0: mt0.Shared, localFactsEnabled: policy.EnabledOption(optValueFacts) && !hints.flags.has(hintHasControlFlow)}
	// Relocations are transient until the module owner copies them into its flat
	// arena. Reuse one function buffer instead of allocating one backing per
	// caller; larger decoded call counts can still reserve the exact target-cost
	// threshold without retaining one buffer per function.
	f.relocs = sc.relocs[:0]
	if hints.callRelocSites >= minPreallocatedCallRelocs && cap(f.relocs) < int(hints.callRelocSites) {
		f.relocs = make([]callReloc, 0, hints.callRelocSites)
	}
	defer func() {
		sc.ctrl = f.ctrl
		sc.transient = f.transient
		sc.relocs = f.relocs
	}()
	f.storeForwardOK = policy.EnabledOption(optStoreForward) && len(c.BodyBytes) <= 256 && nLocals <= 8
	f.syncHostCalls = syncHostCalls
	f.syncHostSlots = syncHostSlots
	f.gcTypeSubtypingRefTest = gcTypeSubtypingRefTest
	if !guardMode && len(m.Memories) > 0 {
		f.memSizeReg = X27 // explicit bounds: X27 = memBytes for the whole module
	}
	if cap(f.localType) < nLocals {
		f.localType = make([]machineType, nLocals)
	} else {
		f.localType = f.localType[:nLocals]
	}
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
	if cap(f.localSlot) < nLocals {
		f.localSlot = make([]uint32, nLocals)
	} else {
		f.localSlot = f.localSlot[:nLocals]
	}
	for i, mt := range f.localType {
		// Validation caps a function at 65,535 locals, so even an all-v128
		// local area fits exactly in uint32 slots. Inlined scratch is checked
		// against the much smaller native-frame limit below before any home is
		// consumed.
		f.localSlot[i] = uint32(f.nLocalSlots)
		f.nLocalSlots += mt.stackSlots()
	}
	hasCall := hints.flags.has(hintHasCall)
	touchesMemory := hints.flags.has(hintTouchesMemory)
	// A private prepared entry establishes X26 and preserves the full Go
	// callee-saved set, so small integer functions need not be leaves. Keep host
	// imports, memory-touching functions, module-pinned globals, and EH state on
	// the adapter. A module-level memory alone is harmless when this function's
	// bounded scan proves that its body never reads, writes, or grows memory.
	directPrepared := policy.EnabledOption(optRegABI) && preparedDirectIntSig(ft) && !touchesMemory && len(modGlobals) == 0 && !hints.flags.has(hintModuleEH) &&
		m.ImportedFuncCount() == 0 && (m.MemCount() == 0 || !hasCall) && len(c.BodyBytes) <= 96 && nLocals <= 8
	// Auto-inlining: collect the callees this caller will splice (before the pin
	// setup below, which the plan can influence). A spliced memory-touching callee
	// runs its linear-memory ops in THIS caller's frame, so fold it into
	// touchesMemory — otherwise the guard-page pin exclusion (which drops X9/X10/X11
	// from the pool for a memory-touching call-making function) would be skipped for
	// a caller whose own body never touched memory.
	inlinedCallees := collectInlinedCallees(c, inlineTargets)
	if policy.EnabledOption(optInlineCallFree) && hasCall && allCallsWillInline(c, inlineTargets, policy) {
		hasCall = false
		f.stats.peep("all-calls-inlined")
	}
	if inlinePlanTouchesMemory(inlinedCallees) {
		touchesMemory = true
	}
	effectiveHints := *hints
	effectiveHints.flags.assign(hintHasCall, hasCall)
	// The module bit records the original body classification for callers. This
	// function may additionally become a leaf after all of its calls inline, so
	// classify its effective post-inline hints here as before.
	f.preserveCallerPins = preservesCallerPins(ft, nLocals, effectiveHints.funcHints)
	if f.preserveCallerPins {
		// Keep this leaf out of every register a direct caller may use for a
		// pinned local or merge value. Its parameters stay in X0..X7 below; all
		// temporary work uses the ordinary caller-clobbered allocation set.
		for _, r := range pinnedLocalRegs {
			f.reserved = f.reserved.add(r)
		}
		for _, r := range [...]Reg{X9, X10, X11, mergeReg} {
			f.reserved = f.reserved.add(r)
		}
	}
	regABI := policy.EnabledOption(optRegABI) && (sigFitsRegABI(ft) || (f.stagedTailDescriptors && sigFitsReferenceResultRegABI(ft)))
	// Only wrapper-ABI code reads frResultsOff. Register-ABI adapters preserve X3
	// below the internal frame, while direct internal and tail paths return in
	// registers. EH and GC frame plans retain the established fixed layout until
	// their independently generated offset tables become header-relative.
	f.compactFrameHeader = regABI && !f.moduleEH
	if f.compactFrameHeader && !f.prepareCompactGCFrameHeader(gcFrameRoots) {
		f.compactFrameHeader = false
	}
	if f.compactFrameHeader {
		f.stats.peep("frame-header-elide")
	}
	var gpPoolStorage [24]Reg
	gpPool := gpPinPoolWithPolicy(gpPoolStorage[:0], regABI, f.nParams, !hasCall, policy)
	if f.moduleEH {
		// X22 carries the active handler across every local call in an
		// exception-enabled module. Cross-instance call paths save it explicitly.
		gpPool = withoutReg(gpPool, ehReg)
		f.reserved = f.reserved.add(ehReg)
	}
	if policy.EnabledOption(optLeafScratchPins) && !hasCall {
		// X12/X13 are fixed only by loop-region promotion, and X14 only by
		// bulk/table helpers. A straight-line scalar leaf can spend them on three
		// additional hot locals while the normal allocator still retains seven
		// ordinary transient GPRs plus its two scratch-floor registers in the
		// largest current scalar leaf.
		if !hints.flags.has(hintHasLoop) {
			gpPool = append(gpPool, X12, X13)
		}
		if !hints.flags.has(hintUsesBulkMem) && len(m.Tables) == 0 {
			gpPool = append(gpPool, X14)
		}
	}
	if policy.EnabledOption(optEntryArgPins) && regABI && !hasCall {
		gpPool = append(gpPool, X2, X3, X4, X5, X6, X7)
	}
	// The inline bulk-memory helpers use X9/X10/X11 as fixed dst/src/count
	// registers after canonicalizing the operand stack. They do not participate in
	// the general allocator, so assigning a local to one of those registers would
	// let memory.copy/fill silently overwrite live local state (fannkuch's dynamic
	// memory.copy turned its permutation loop into an infinite loop). The pre-scan
	// already records this exact class; reserve only the colliding helper registers
	// and retain the rest of the call-free pin pool.
	if hints.flags.has(hintUsesBulkMem) {
		gpPool = withoutReg(withoutReg(withoutReg(gpPool, X9), X10), X11)
	}
	// Memory-touching call-makers with imports or tables retain the conservative
	// unpinned path: host/cross-instance/indirect setup has substantially wider
	// clobber and merge surfaces (the SQLite pressure regressions). A
	// table-free, import-free recursive function only crosses the same-module
	// register ABI, whose STACK_REG path explicitly spills dirty pins and lazily
	// recovers them. Keeping pins for that auditable class removes the dominant
	// local-slot traffic in recursive memory kernels such as memory_tree.
	safeMemoryCallPins := hints.flags.has(hintCallsSelf) && m.ImportedFuncCount() == 0 && len(m.Tables) == 0
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
	// Leave enough unreserved registers for the widest ordinary lowering step:
	// three protected inputs/temporaries plus one result. This is a target-derived
	// bound over the allocatable file after module roles are removed, not a
	// workload heuristic. The allocator may still use these registers normally;
	// whole-function pins alone cannot consume them.
	maxPins := gpPinLimit(f.reserved)
	if len(gpPool) > maxPins {
		gpPool = gpPool[:maxPins]
	}
	// Wide local tables begin canonical so their many borrowed values cannot consume
	// the target-derived transient floor. There is no failed-attempt retry.
	if !pinLocals || nLocals > 64 {
		gpPool = nil
	}
	// Hot mutable-int globals share the GP pin pool with locals, holding their VALUE
	// in the register (WARP's model). In call-free functions any loop-accessed global
	// qualifies; in call-making functions only globals accessed inside a CALL-FREE
	// loop do — the spill/reload keeping the cell coherent then lands on the sparse
	// out-of-loop calls, not per iteration. Non-eligible globals use the per-run
	// cell-pointer cache (globalCellPtr).
	var globalHints []shared.GlobalHint
	if regABI {
		sc.directPrepared = directPrepared
		globalHints = hints.sparseGlobals
	}
	f.installModuleGlobals(modGlobals)
	intervalRegion := pinLocals && regABI && !hasCall && !hints.flags.has(hintHasControlFlow) &&
		!hints.flags.has(hintUsesBulkMem) && len(inlinedCallees) == 0 && f.prepareIntervalRegion(c.BodyBytes, hints)
	if intervalRegion {
		gpPool = nil // regional assignments supersede whole-function GP pins
	}
	f.assignPinnedLocals(hints.localScore, globalHints, gpPool, hasCall, pinLocals)
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
	f.usesCalls = hasCall && policy.EnabledOption(optStackReg)
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
	f.lazyZero = hints.flags.has(hintCallsSelf) && touchesMemory && len(c.BodyBytes) <= 192 && nLocals-len(ft.Params) <= 8

	// Auto-inlining: reserve each spliced callee's locals past f.nLocals (after all
	// nLocals-dependent setup above, so zeroDeclaredLocals/skipFence/lazyZero see the
	// caller's own locals only). Extends the frame's local arrays with unpinned
	// scratch; the splice at each call site binds/zeroes them.
	f.reserveInlineLocals(inlinedCallees, inlineTargets)
	// Reject an already-oversized locals/header frame before emitting any
	// SP-relative homes whose architecture encoding has a smaller displacement.
	// Operand spills can only grow this frame and remain checked after lowering.
	if err := f.validateFrameSize(f.frameSize()); err != nil {
		return nil, nil, 0, err
	}

	if regABI {
		internalOff, err := f.emitRegABI(c, hostAdapter, hints.localScore, hints.flags.has(hintHasFloatConst))
		if err != nil {
			return nil, nil, 0, err
		}
		if f.gcFrameRoots != nil {
			f.gcFrameRoots.FrameBytes = uint32(f.frameSize())
			if f.gcCallsiteIndex != f.gcFrameRoots.CallMaskCount() {
				f.gcFrameRoots.Exact = false
			}
		}
		f.finalizePeepholes()
		internalOff, err = f.finalizeNativeCode(internalOff)
		if err != nil {
			return nil, nil, 0, err
		}
		f.finalizeStats(len(f.a.B))
		return f.a.B, f.relocs, internalOff, nil
	}

	f.prologue(hints.localScore)
	if hints.flags.has(hintHasFloatConst) {
		f.preloadFloatConsts(c.BodyBytes)
	}
	if err := f.runBody(c); err != nil {
		return nil, nil, 0, err
	}
	f.epilogue()
	f.emitTrapStubs()
	if err := f.patchFrameAdjusts(); err != nil {
		return nil, nil, 0, err
	}
	if f.gcFrameRoots != nil {
		f.gcFrameRoots.FrameBytes = uint32(f.frameSize())
		if f.gcCallsiteIndex != f.gcFrameRoots.CallMaskCount() {
			f.gcFrameRoots.Exact = false
		}
	}
	f.finalizePeepholes()
	if _, err := f.finalizeNativeCode(0); err != nil {
		return nil, nil, 0, err
	}
	f.finalizeStats(len(f.a.B))
	return f.a.B, f.relocs, 0, nil
}

// preservesCallerPins identifies the deliberately narrow internal-call ABI
// variant used for hot, simple leaves. Such a function has no declared locals,
// calls, memory access, or global access; its integer parameters can stay in the
// incoming argument registers while every caller-pinned register is reserved.
// Consequently it cannot observe or modify caller state outside X0..X7/X16/X17.
func preservesCallerPins(ft *wasm.CompType, nLocals int, h funcHints) bool {
	if !sigFitsRegABI(ft) || !sigIsIntOnly(ft) || nLocals != len(ft.Params) || h.flags.has(hintHasCall) || h.flags.has(hintTouchesMemory) {
		return false
	}
	if h.globalCount != 0 {
		return false
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
	s.NativeSize.TotalBytes = codeLen
	s.NativeSize.InternalFunctionBytes = codeLen - s.NativeSize.HostAdapterBytes - s.NativeSize.AdapterToInternalPaddingBytes
	s.GCCodeBytes.Total = codeLen
	s.peepN("logical-move-immediate", f.a.LogicalMoveImmediates)
	s.peepN("compact-move-immediate32", f.a.CompactMoveImmediates32)
	s.FrameBytes = f.frameSize()
	s.MaxSpillSlots = f.maxSpill
	s.MaxPendingNodes = int(f.s.maxPending)
}

// runBody opens the function control frame, lowers the body, and patches every
// return/br-to-function site to the current epilogue position.
func (f *fn) runBody(c *wasm.Func) error {
	sc := f.scratchState()
	resultTypes := lowerFunctionResultTypes(sc, f.ft.Results)
	if len(resultTypes) <= len(sc.functionResultTypeArena) {
		f.controlBaseTypeN = uint8(len(resultTypes))
	}
	f.ctrl = append(sc.ctrl[:0], ctrlFrame{kind: cfFunc, resultN: len(resultTypes), branchN: uint32(len(resultTypes)), types: resultTypes})
	if err := f.body(c.BodyBytes); err != nil {
		return err
	}
	f.patchReturnSites()
	return nil
}

func (f *fn) appendReturnSite(site int) {
	if site < 0 || site&3 != 0 || site/4 >= 1<<26-1 {
		f.setRepresentationLimit(functionRepresentationReturnSite)
		return
	}
	sc := f.scratchState()
	word := rdWord(f.a.B, site)
	wrWord(f.a.B, site, word&0xfc000000|sc.retSiteHead)
	sc.retSiteHead = uint32(site/4 + 1)
}

func (f *fn) patchReturnSites() {
	sc := f.scratchState()
	for head := sc.retSiteHead; head != 0; {
		site := int(head-1) * 4
		word := rdWord(f.a.B, site)
		head = word & 0x03ffffff
		wrWord(f.a.B, site, word&0xfc000000)
		f.a.PatchBranch26(site, f.a.Len())
	}
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

func gpPinPoolWithPolicy(pool []Reg, regABI bool, nParams int, callFree bool, policy CodegenPolicy) []Reg {
	pool = append(pool, pinnedLocalRegs...) // X19-X23
	if callFree {
		pool = append(pool, X24, X25)
		// X8 is neither an internal integer argument (X0-X7) nor a fixed-role
		// backend scratch. A leaf can dedicate it to one more hot local without
		// any call-boundary save traffic.
		if policy.EnabledOption(optX8Pin) {
			pool = append(pool, X8)
		}
	}
	if !regABI || nParams <= 4 {
		pool = append(pool, X9, X10, X11)
	}
	return append(pool, X15)
}

// gpPinLimit leaves enough of the target's unreserved allocatable file for the
// widest ordinary lowering step: three protected inputs/temporaries plus one
// result. Whole-function pins are optional; transient lowering must not retry a
// function merely because module roles made the physical file smaller.
func gpPinLimit(reserved regMask) int {
	const minTransientGP = 4
	available := 0
	for _, r := range gpAlloc {
		if !reserved.has(r) {
			available++
		}
	}
	if available <= minTransientGP {
		return 0
	}
	return available - minTransientGP
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

func (f *fn) assignPinnedLocals(scores []uint32, globalHints []shared.GlobalHint, gpPool []Reg, hasCall, pinLocals bool) {
	if cap(f.locals) < f.nLocals {
		f.locals = make([]localDef, f.nLocals)
	} else {
		f.locals = f.locals[:f.nLocals]
	}
	for i := range f.locals {
		facts := valueFacts(0)
		if f.localFactsEnabled && i >= f.nParams && f.localType[i] == mtI32 {
			facts = factUpper32Zero | factBoolean // declared locals start at zero
		}
		f.locals[i] = localDef{reg: regNone, facts: facts, state: lsReg}
	}
	// Module-pinned globals (installModuleGlobals) already occupy globalReg
	// entries; keep them and size for whichever view is larger.
	if len(f.globalReg) < f.m.GlobalCount() {
		f.initGlobalRegs(f.m.GlobalCount())
	}
	// The GP pin pool is shared by hot INT locals and hot globals, both holding their
	// VALUE in the register (WARP's model). A global is a candidate only when it is a
	// mutable int accessed inside a loop (score >= one loop level): WARP pins only int
	// globals as values, and the loop gate ensures the per-iteration memory traffic it
	// removes outweighs the one-time prologue load + epilogue write-back.
	var gpStorage [32]gpCand // no target can dedicate more than its physical register file
	if len(gpPool) > len(gpStorage) {
		panic("arm64: GP pin pool exceeds architectural register file")
	}
	gp := gpStorage[:0]
	if len(gpPool) != 0 {
		for i := 0; i < f.nLocals; i++ {
			if f.localType[i] == mtI32 || f.localType[i] == mtI64 {
				gp = insertGPCandidate(gp, gpCand{idx: uint32(i), score: localHotness(scores[i])}, len(gpPool))
			}
		}
	}
	loopMin := uint32(loopWeight(1))
	for _, gh := range globalHints {
		g := int(gh.Index)
		if gh.Score < loopMin || f.isModuleGlobal(g) || hasCall && !gh.Eligible {
			continue
		}
		gt, ok := f.m.GlobalTypeByIndex(gh.Index)
		if !ok || !gt.Mutable || !isIntValType(wasm.GlobalValueType(gt)) {
			continue
		}
		gp = insertGPCandidate(gp, gpCand{global: true, idx: uint32(g), score: gh.Score}, len(gpPool))
	}
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
		idx := int(c.idx)
		if c.global {
			f.globalReg[idx] = gpPool[k]
			f.stats.addPinnedGlobalValue()
		} else {
			f.locals[idx].reg = gpPool[k]
			f.stats.addPinnedLocal()
		}
		f.pinnedLocalMask = f.pinnedLocalMask.add(gpPool[k])
	}
	// Float locals use the separate V pin pool. Call-free functions also pin hot
	// v128 locals here (same V registers, full 128-bit): a wasm→wasm call would only
	// preserve the low 64 bits, so a v128 pin is confined to the call-free class.
	pinV128 := f.opt(optV128Pins) && !hasCall
	hotV128Candidates := 0
	if pinLocals && f.nLocals <= 64 {
		for i := 0; i < f.nLocals; i++ {
			if pinV128 && f.localType[i] == mtV128 && localHotness(scores[i]) > 0 {
				hotV128Candidates++
			}
		}
	}
	fpPinLimit := len(pinnedFLocalRegs)
	deepV128Pins := false
	if !f.opt(optExtendedFPPins) && fpPinLimit > basePinnedFLocalRegs {
		fpPinLimit = basePinnedFLocalRegs
	} else if !hasCall && hotV128Candidates >= 24 {
		// Wide vector kernels have short expression trees but large named v128
		// state. Keeping that state resident is worth the smaller transient pool;
		// scalar-float kernels retain the measured 15-pin cap below.
		fpPinLimit = len(pinnedFLocalRegs)
		deepV128Pins = true
		f.stats.peep("deep-v128-local-pins")
	} else if !hasCall && fpPinLimit > callFreePinnedFLocalRegs {
		// Call-free numeric loops still need room for wide expression trees. Past
		// this point nbody loses more to transient-register pressure than it gains
		// from another local pin. Call-making raytrace has sparse call sites and a
		// much larger live-local set, so its existing STACK_REG path profitably uses
		// the full pool.
		fpPinLimit = callFreePinnedFLocalRegs
	} else if hasCall && fpPinLimit > 23 {
		fpPinLimit = 23
	}
	if !pinLocals || f.nLocals > 64 {
		// Keep very wide signatures canonical so optional V-register pins cannot
		// consume the transient register floor.
		fpPinLimit = 0
	}
	var fcStorage [32]uint16 // bounded by the 32-register architectural V file
	if fpPinLimit > len(fcStorage) {
		panic("arm64: FP pin limit exceeds architectural register file")
	}
	fc := fcStorage[:0]
	if fpPinLimit != 0 {
		for i := 0; i < f.nLocals; i++ {
			if f.localType[i].isFloat() || (pinV128 && f.localType[i] == mtV128) {
				fc = insertLocalCandidate(fc, uint16(i), scores, fpPinLimit)
			}
		}
	}
	for k, local := range fc {
		if k >= fpPinLimit {
			break
		}
		i := int(local)
		if deepV128Pins && localHotness(scores[i]) == 0 {
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

const globalRegDirty Reg = 1 << 7

func globalRegValue(state Reg) Reg {
	if state == regNone {
		return regNone
	}
	return state &^ globalRegDirty
}

func globalRegIsDirty(state Reg) bool {
	return state != regNone && state&globalRegDirty != 0
}

func (f *fn) initGlobalRegs(n int) {
	if cap(f.globalReg) < n {
		f.globalReg = make([]Reg, n)
	} else {
		f.globalReg = f.globalReg[:n]
	}
	for i := range f.globalReg {
		f.globalReg[i] = regNone
	}
}

// installModuleGlobals records the module-wide global→register pins on this
// function (every function in the module shares the same assignment).
func (f *fn) installModuleGlobals(pins []moduleGlobalPin) {
	if len(pins) == 0 {
		return
	}
	nG := f.m.GlobalCount()
	if len(f.globalReg) < nG {
		f.initGlobalRegs(nG)
	}
	f.moduleGlobals = pins
	for _, p := range pins {
		f.globalReg[p.global] = p.reg
	}
}

func (f *fn) isModuleGlobal(g int) bool {
	for _, p := range f.moduleGlobals {
		if int(p.global) == g {
			return true
		}
	}
	return false
}

// deriveModuleGlobals / storeModuleGlobals sync the module-pinned globals with
// their cells at wasm↔native boundaries (offset-0 prologues and epilogues, the
// adapter's Go exit, trap stubs) and before wrapper-ABI calls (whose callee's
// offset-0 prologue reloads). Register-ABI calls and returns carry nothing.
// scratch must be a register safe to clobber at the call site.
func (f *fn) deriveModuleGlobals() {
	for g, state := range f.globalReg {
		reg := globalRegValue(state)
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
	for g, state := range f.globalReg {
		reg := globalRegValue(state)
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
	f.derivePinnedGlobalsIn(^regMask(0))
}

// derivePinnedGlobalsIn reloads only globals assigned to registers in regs.
// Fixed-register sequences use it to restore their narrow scratch bank without
// touching unrelated value pins.
func (f *fn) derivePinnedGlobalsIn(regs regMask) {
	for g, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg == regNone || f.isModuleGlobal(g) || !regs.has(reg) {
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
	f.storePinnedGlobalsIn(^regMask(0), dirtyOnly)
}

// storePinnedGlobalsIn writes only globals assigned to registers in regs.
func (f *fn) storePinnedGlobalsIn(regs regMask, dirtyOnly bool) {
	for g, state := range f.globalReg {
		reg := globalRegValue(state)
		if reg == regNone || f.isModuleGlobal(g) || !regs.has(reg) || (dirtyOnly && !globalRegIsDirty(state)) {
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
func (f *fn) prologue(localScores []uint32) {
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
		f.ld64(f.memSizeReg, linMemReg, -bdCurBytes)
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
	pairParams := f.opt(optEntryParamPairs)
	pendingParam := pendingWrapperParamHome{}
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
				if pairParams {
					pendingParam = f.queueWrapperParamHome(pendingParam, paramOff, f.localOff(i))
				} else {
					// X16 (backend scratch) is the copy temp: X0 is the serArgs base and
					// must stay live for the remaining param loads (amd64 used RAX here,
					// but on arm64 that role register aliases the args base).
					f.ld64(X16, X0, paramOff)
					f.st64(SP, f.localOff(i), X16)
				}
			}
		}
		paramOff += abiValSize(pt)
	}
	if pairParams {
		f.flushWrapperParamHome(pendingParam)
	}
	if x0ParamOff >= 0 {
		f.ld64(X0, X0, x0ParamOff)
	}
	f.zeroDeclaredLocals(localScores)
	f.derivePinnedGlobals()
	f.deriveModuleGlobals() // offset-0 entry: cells → module-pinned registers
}

type pendingWrapperParamHome struct {
	argOff   int32
	localOff int32
	valid    bool
}

func (f *fn) queueWrapperParamHome(p pendingWrapperParamHome, argOff, localOff int32) pendingWrapperParamHome {
	if p.valid && argOff == p.argOff+8 && localOff == p.localOff+8 && p.argOff <= 504 && p.localOff <= 504 {
		f.a.LdpOffset(X16, X17, X0, p.argOff)
		f.a.StpOffset(X16, X17, SP, p.localOff)
		f.stats.peep("entry-param-pair-wrapper")
		return pendingWrapperParamHome{}
	}
	if p.valid {
		f.ld64(X16, X0, p.argOff)
		f.st64(SP, p.localOff, X16)
	}
	return pendingWrapperParamHome{argOff: argOff, localOff: localOff, valid: true}
}

func (f *fn) flushWrapperParamHome(p pendingWrapperParamHome) {
	if p.valid {
		f.ld64(X16, X0, p.argOff)
		f.st64(SP, p.localOff, X16)
	}
}

// zeroDeclaredLocals initializes non-parameter locals. Most functions keep the
// old eager zeroing path; small call+memory functions use WARP-style lazy zero,
// where reads materialize zero on demand and control-flow reconciliation stores it
// to the frame before paths diverge when required.
func (f *fn) zeroDeclaredLocals(localScores []uint32) {
	if f.nLocals <= f.nParams {
		return
	}
	if !f.lazyZero {
		a := f.a
		pairZeros := f.opt(optEntryZeroPairs)
		entryInitElision := f.opt(optEntryInitElision) && (f.gcFrameRoots == nil || !f.gcFrameRoots.Candidate)
		pendingZero := int32(-1)
		// AArch64 has a zero register (XZR): store it directly, no scratch to clear.
		for i := f.nParams; i < f.nLocals; i++ {
			if entryInitElision && i < 64 && localScores[i]&localScoreEntryInitialized != 0 {
				if pairZeros {
					pendingZero = f.flushDeclaredZeroSlot(pendingZero)
				}
				f.stats.peep("entry-init-elide")
				continue
			}
			if pr, _, ok := f.pinReg(i); ok && f.localType[i] == mtV128 {
				if pairZeros {
					pendingZero = f.flushDeclaredZeroSlot(pendingZero)
				}
				a.NeonEor16b(pr, pr, pr) // zero the whole 128-bit pin register
			} else if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
				if pairZeros {
					pendingZero = f.flushDeclaredZeroSlot(pendingZero)
				}
				a.MovImm64(pr, 0)
			} else if ok && isFloat {
				if pairZeros {
					pendingZero = f.flushDeclaredZeroSlot(pendingZero)
				}
				a.FmovFromGpr(pr, ZR, false) // fmov d,xzr → 0.0
			} else if f.localType[i] == mtV128 {
				if pairZeros {
					pendingZero = f.queueDeclaredZeroSlot(pendingZero, f.localOff(i))
					pendingZero = f.queueDeclaredZeroSlot(pendingZero, f.localOff(i)+8)
				} else {
					f.st64(SP, f.localOff(i), ZR)
					f.st64(SP, f.localOff(i)+8, ZR)
				}
			} else {
				if pairZeros {
					pendingZero = f.queueDeclaredZeroSlot(pendingZero, f.localOff(i))
				} else {
					f.st64(SP, f.localOff(i), ZR)
				}
			}
		}
		if pairZeros {
			f.flushDeclaredZeroSlot(pendingZero)
		}
		return
	}
	for i := f.nParams; i < f.nLocals; i++ {
		f.markDeclaredLocalZero(i)
	}
}

func (f *fn) queueDeclaredZeroSlot(pending, off int32) int32 {
	if pending >= 0 && off == pending+8 && pending <= 504 {
		f.a.StpOffset(ZR, ZR, SP, pending)
		f.stats.peep("entry-zero-pair")
		return -1
	}
	if pending >= 0 {
		f.st64(SP, pending, ZR)
	}
	return off
}

func (f *fn) flushDeclaredZeroSlot(pending int32) int32 {
	if pending >= 0 {
		f.st64(SP, pending, ZR)
	}
	return -1
}

// emitStackFenceCheck traps (StackFence → "call stack exhausted") when SP has
// dropped below the fence stored at [linMem-72], turning unbounded recursion into
// a clean trap instead of a fault. A zero fence disables the check (SP > 0).
func (f *fn) emitStackFenceCheck(linMemReg, scratch Reg) {
	if !f.opt(optStackFence) || f.skipFence {
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
func (f *fn) emitRegABI(c *wasm.Func, hostAdapter bool, localScores []uint32, hasFloatConst bool) (int, error) {
	a := f.a
	np, rN := f.nParams, len(f.ft.Results)

	// Host→internal adapter (offset 0): in X0=serArgs, X1=linMem, X2=trap,
	// X3=results; loads args into registers, calls the internal entry, stores the
	// register results.
	gp, fp := 0, 0
	var adapterCall int
	if hostAdapter {
		a.MovReg64(linMemReg, X1) // linMem → linMemReg: the module-wide invariant the internal entry inherits
		if f.memSizeReg != regNone {
			// Offset-0 entry (from Go, or an indirect call): establish the module-wide
			// memBytes cache before the internal entry runs (which relies on it).
			f.ld64(f.memSizeReg, linMemReg, -bdCurBytes)
		}
		f.deriveModuleGlobals()   // offset-0 entry: cells → module-pinned registers
		a.StpPre(LR, X3, SP, -16) // save LR (BL clobbers it) + results ptr; keeps SP 16-aligned
		gp, fp = 0, 0
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
		adapterCall = a.Bl() // BL internal entry; patched below
		f.adapterReturnOff = adapterCall + 4
		if f.gcFrameRoots != nil {
			f.gcFrameRoots.AdapterReturnOffset = uint32(adapterCall + 4)
		}
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
		f.adapterEndOff = a.Len()
		if f.stats != nil {
			f.stats.NativeSize.HostAdapterBytes = a.Len()
		}
	}

	// Internal entry (frameless): linMemReg (linMem) is inherited from the caller —
	// every wasm function keeps it pinned, and the adapter establishes it at the
	// Go boundary — and the trap cell pointer lives in basedata, so the entry
	// carries no environment setup at all (WARP's model). Args in GP/V regs.
	if hostAdapter {
		beforeAlign := a.Len()
		f.alignCode(f.policy.InternalAlignLog2)
		if f.stats != nil {
			f.stats.NativeSize.AdapterToInternalPaddingBytes = a.Len() - beforeAlign
		}
	}
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
	swapChains := resolveRegMovesWindow(moves,
		func(dst, src Reg) { a.MovReg64(dst, src) },
		func(x, y Reg) {
			a.MovReg64(X16, x)
			a.MovReg64(x, y)
			a.MovReg64(y, X16)
		},
		func(a, b, c Reg) {
			f.a.MovReg64(X16, a)
			f.a.MovReg64(a, b)
			f.a.MovReg64(b, c)
			f.a.MovReg64(c, X16)
		})
	f.stats.peepN("machine-swap-chain", swapChains)
	f.tmpMoves = moves[:0]
	f.zeroDeclaredLocals(localScores)
	if hasFloatConst {
		f.preloadFloatConsts(c.BodyBytes)
	}
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
	if err := f.patchFrameAdjusts(); err != nil {
		return 0, err
	}
	if hostAdapter {
		f.a.PatchBranch26(adapterCall, internalOff)
		if f.stats != nil {
			f.stats.NativeSize.HostAdapterShapeHash = shared.AdapterShapeHash(f.a.B[:f.stats.NativeSize.HostAdapterBytes], adapterCall, 4)
			f.stats.NativeSize.HostAdapterTailBytes = f.stats.NativeSize.HostAdapterBytes - f.adapterReturnOff
			f.stats.NativeSize.HostAdapterTailShapeHash = shared.AdapterShapeHash(f.a.B[f.adapterReturnOff:f.stats.NativeSize.HostAdapterBytes], -1, 0)
		}
	}
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
	case t.Kind() == wasm.ValRef:
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
