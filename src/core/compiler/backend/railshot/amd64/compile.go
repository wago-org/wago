//go:build amd64

package amd64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"slices"
	"sort"
	"sync"
	"sync/atomic"

	plugincodegen "github.com/wago-org/wago/codegen/amd64"
	railcore "github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/optimization"
	"github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/encoder/amd64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// regMergeEnabled turns on WARP-style register reconciliation of single-int-result
// block/if merges (docs/operand-stack-registers-plan.md) instead of the
// flush-to-slot + reload. Default ON (fib_rec −13.7%, json-as serialize −1.5%, no
// regressions; validated against the spec suite + full corpus differential).
// WAGO_REG_MERGE=0 restores the slot path — kept as the reference oracle for A/B.
var regMergeEnabled = os.Getenv("WAGO_REG_MERGE") != "0"

// deadGCNewEnabled removes bounded GC constructor trees whose result is dropped.
// Struct and fixed-array trees disappear directly; dynamic/default/data/element
// arrays retain a nonallocating preflight helper so size, segment, and initializer
// traps remain ordered. WAGO_AMD64_NO_DEAD_GC_NEW=1 keeps every allocation helper
// for differential A/B testing.
var deadGCNewEnabled = os.Getenv("WAGO_AMD64_NO_DEAD_GC_NEW") != "1"

// exactGCRefFactsEnabled propagates exact non-null reference facts through
// locals inside conservative straight-line structured regions. It removes only
// casts already proved by a successful prior cast or exact constructor. It is
// default-off after broad compile-resource measurement; WAGO_AMD64_GC_REF_FACTS=1
// opts in, while the legacy NO variables remain rollback overrides.
var exactGCRefFactsEnabled = envDefaultOff(os.Getenv("WAGO_AMD64_GC_REF_FACTS")) &&
	os.Getenv("WAGO_AMD64_NO_GC_REF_FACTS") != "1" &&
	os.Getenv("WAGO_AMD64_NO_EXACT_GC_REF_FACTS") != "1"

var frameElideVoid = os.Getenv("WAGO_AMD64_NO_FRAME_ELIDE_VOID") != "1"

// compactRegABIFrameHeader removes the wrapper-only spare/results-pointer
// header from ordinary register-ABI internal frames. Tail-call lowering still
// has wrapper-transfer paths that consume the header, so it remains excluded.
// Keep the switch for corpus A/B and immediate rollback.
var compactRegABIFrameHeader = os.Getenv("WAGO_AMD64_NO_COMPACT_REGABI_FRAME") != "1"

// gcLoadForwardingEnabled keeps the bounded result-local array.len and immutable
// struct.get cache independently A/B-testable from the semantic fact engine.
var gcLoadForwardingEnabled = os.Getenv("WAGO_AMD64_NO_GC_LOAD_FORWARDING") != "1"

// gcKnownArrayBoundsEnabled lets a constructor-known length plus constant index
// remove the redundant logical Aux comparison from direct array.get/set. The
// physical object-extent hardening check remains. Keep an independent A/B switch.
var gcKnownArrayBoundsEnabled = os.Getenv("WAGO_AMD64_NO_GC_KNOWN_BOUNDS") != "1"

// nativeGCStructAllocEnabled consumes collector-reserved handle runs and nursery
// chunks for admitted struct and array constructors. Rooted Go helpers remain the
// collection/refill path. Keep one differential kill switch for qualification.
var nativeGCStructAllocEnabled = os.Getenv("WAGO_AMD64_NO_GC_NATIVE_ALLOC") != "1"

// gcSharedStubsEnabled moves the common noncollecting compact-handle resolver
// into one module-owned leaf stub. The local call edge retains exact trap/source
// attribution. WAGO_AMD64_NO_GC_SHARED_STUBS=1 restores fully inline resolution
// for differential qualification and low-site-count crossover measurement.
var gcSharedStubsEnabled = os.Getenv("WAGO_AMD64_NO_GC_SHARED_STUBS") != "1"

// sharedAdaptersEnabled lets compact code replace byte-identical register-ABI
// host adapters with compact function-local target thunks plus one cold
// module copy. WAGO_AMD64_NO_SHARED_ADAPTERS=1 retains adapter-tail sharing.
var sharedAdaptersEnabled = os.Getenv("WAGO_AMD64_NO_SHARED_ADAPTERS") != "1"

// stackDeltaAdapterThunkEnabled pushes an internal-entry delta before jumping
// to the shared adapter. Its fixed prefix consumes that delta and restores the
// original host stack before running the existing adapter body.
// The kill switch restores the former LEA/JMP thunk byte for byte.
var stackDeltaAdapterThunkEnabled = os.Getenv("WAGO_AMD64_NO_STACK_DELTA_ADAPTER_THUNK") != "1"

// gcResolveReuseEnabled retains one resolved object address only across a
// compiler-proven straight-line, safepoint-free region. The compact local remains
// the root and stable identity. WAGO_AMD64_NO_GC_RESOLVE_REUSE=1 disables reuse.
var gcResolveReuseEnabled = os.Getenv("WAGO_AMD64_NO_GC_RESOLVE_REUSE") != "1"

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

// extendedFPPinsEnabled lets a function use the WARP-sized float-local pin pool
// beyond baseFPPins. Default ON; WAGO_AMD64_NO_EXTFPPINS=1 caps at baseFPPins.
var extendedFPPinsEnabled = os.Getenv("WAGO_AMD64_NO_EXTFPPINS") != "1"

// vexFloatMemEnabled folds a scalar float memory operand into a non-destructive
// AVX operation, avoiding the legacy source-to-scratch copy for borrowed locals.
// Default ON; WAGO_AMD64_NO_VEX_FLOAT_MEM=1 restores the two-operand sequence.
var vexFloatMemEnabled = os.Getenv("WAGO_AMD64_NO_VEX_FLOAT_MEM") != "1"

// multiBoundsCertEnabled keeps independent straight-line bounds proofs for a
// small set of address sources. Default ON; WAGO_AMD64_SINGLE_BOUNDS_CERT=1
// restores the original one-entry certificate for A/B.
var multiBoundsCertEnabled = os.Getenv("WAGO_AMD64_SINGLE_BOUNDS_CERT") != "1"

// v128LocalPinsEnabled keeps hot v128 locals in an XMM register for the whole
// function instead of reloading them from the frame slot on every SIMD op — the
// amd64 analog of arm64's v128 local pins. Confined to call-free functions: every
// XMM is caller-saved on System V, so a wasm->wasm call would clobber the pin.
// Default ON; WAGO_AMD64_NO_V128_PINS=1 restores the spill-per-op path for A/B.
var v128LocalPinsEnabled = os.Getenv("WAGO_AMD64_NO_V128_PINS") != "1"

// v128LocalSinkEnabled peeps `local.set/tee $x (v128bin (local.get $x) …)` into a
// pinned v128 local and computes the op straight into x's XMM register (one
// 3-operand VEX instruction, no accumulator copy and no result-to-pin move) — the
// amd64 analog of arm64's v128 local sink. It is default-off after the broad
// ARM64/AMD64 toggle matrix found no execution benefit. WAGO_V128_SINK=1 opts in.
var v128LocalSinkEnabled = envDefaultOff(os.Getenv("WAGO_V128_SINK"))

// v128ConstCacheEnabled reserves an XMM register for each repeated v128.const
// value in a call-free function and materializes it once at entry, so a loop over
// a constant operand (the isa_simd reductions, bitselect masks, …) copies it from
// a register instead of rebuilding the 128-bit immediate every iteration. The
// amd64 analog of arm64 preloadV128Consts. Default ON; WAGO_AMD64_NO_V128_CONST_CACHE=1
// disables it for A/B.
var v128ConstCacheEnabled = os.Getenv("WAGO_AMD64_NO_V128_CONST_CACHE") != "1"

// callNextUseEnabled skips stores of dirty pinned locals whose next bounded
// post-call access overwrites the local before reading it. It is default-off;
// WAGO_AMD64_CALL_NEXT_USE=1 opts in for focused measurement.
var callNextUseEnabled = envDefaultOff(os.Getenv("WAGO_AMD64_CALL_NEXT_USE"))

// affineLeaEnabled extends scaled-index LEA selection across one-level affine
// base/index subtrees, folding their constants into the LEA displacement.
// It is default-off; WAGO_AMD64_AFFINE_LEA=1 opts in for focused measurement.
var affineLeaEnabled = envDefaultOff(os.Getenv("WAGO_AMD64_AFFINE_LEA"))

// treeOrderEnabled lets commutative, non-trapping Valent trees choose which
// child is evaluated first from a bounded register-need estimate. It adds no
// per-node state: maxDeferDepth bounds the recursive inspection.
var treeOrderEnabled = os.Getenv("WAGO_AMD64_NO_TREE_ORDER") != "1"

// associativeTreeEnabled covers small, trap-free associative trees as one
// accumulator instead of materializing their internal binary nodes.
var associativeTreeEnabled = os.Getenv("WAGO_AMD64_NO_ASSOC_TREE") != "1"

// intervalRegionPinsEnabled reuses GP registers across integer local lifetimes
// in bounded, call-free straight-line functions. Unlike whole-function hot-local
// pins, the regional cache is pressure-spillable and returns a register at the
// local's final get. WAGO_AMD64_INTERVAL_REGIONS=0 keeps the old whole-function
// pin allocator as an A/B and correctness oracle.
var intervalRegionPinsEnabled = os.Getenv("WAGO_AMD64_INTERVAL_REGIONS") != "0"

// memory32AddrZExtElimEnabled avoids a redundant self-move when the storage
// form feeding a memory32 access already guarantees a zero upper half. Default
// ON; WAGO_AMD64_NO_ADDR_ZEXT_ELIM=1 disables it for A/B.
var memory32AddrZExtElimEnabled = os.Getenv("WAGO_AMD64_NO_ADDR_ZEXT_ELIM") != "1"

// valueFactsEnabled carries bounded upper-zero and boolean provenance on Valent
// nodes. WAGO_AMD64_NOPROVENANCE=1 retains the pre-facts path for A/B checks.
var valueFactsEnabled = os.Getenv("WAGO_AMD64_NOPROVENANCE") != "1"

// bmi2RorxEnabled uses BMI2's non-destructive immediate rotate. It is off in
// the low-level backend default and selected by the public runtime only after
// host CPUID confirms BMI2.
var bmi2RorxEnabled bool

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
	a             *amd64.Asm // the (reused) x86-64 encoder
	s             *stack     // the valent-block operand stack
	m             *wasm.Module
	ft            *wasm.CompType // this function's signature
	gcTypeLayouts []codegen.GCTypeLayout
	transient
	globalIdx          int
	traceFuncIdx       uint32
	tracePCBase        uint32
	wasmPC             uint32
	customInstructions map[uint32]CustomInstruction

	nParams             int
	nLocals             int                 // params + declared locals
	localType           []machineType       // per-local machine type
	localSlot           []int               // per-local byte offset within the local-frame area
	localGCRefFacts     []codegen.GCRefFact // semantic facts for compact refs; no raw addresses
	nextGCRefIdentity   uint32              // bounded constructor identity, zero means unavailable
	gcOpcodeBarrier     bool                // current 0xfb opcode emits real barrier work
	gcLastArrayLen      gcArrayLenFact
	gcLastField         gcStructFieldFact
	gcResolved          gcResolvedObject
	gcSharedResolver    bool
	gcHandleResolutions int
	nLocalSlots         int // total local frame slots in 8-byte units

	// WARP-style per-local storage metadata. localType remains as the compact
	// type table used by existing lowering; locals holds the assigned register and
	// call-spill state for each local.
	locals           []localDef
	pinnedLocals     []int // indices of register-pinned locals (fixed after assignPinnedLocals)
	pinnedLocalMask  regMask
	fpinnedLocalMask regMask

	// WARP STACK_REG lazy-spill model for pinned locals in CALL-MAKING functions
	// (usesCalls). locals[i].state tracks whether the live value of pinned local i is
	// in its register (dirty), in both register+slot (clean), or only in its slot.
	// Call-free functions keep locals permanently in registers (locals[].state unused).
	usesCalls  bool
	usesWide   bool
	classifier wasm.ModuleInstructionClassifier
	moduleEH   bool
	// Bounded post-call next-use state. Pinned registers are numbered 0..15,
	// so two uint16 masks fit in the bool cluster's existing alignment padding.
	callDeadGP uint16
	callDeadFP uint16
	// Number of commutative self-update spill opportunities seen in this function.
	// The first keeps the conservative form; repeated pressure selects the denser
	// register form without perturbing one-off sites in otherwise cold functions.
	commuteSelfUpdates uint16

	// Bounded straight-line local intervals. A non-regNone intervalReg entry marks
	// an eligible local; locals[x].reg is populated only while its cached value is
	// live. Physical registers are selected and reclaimed dynamically.
	intervalReg   []Reg
	intervalLast  []uint32
	intervalScore []uint32
	intervalOwner [16]int

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
	vconsts  []v128ConstReg // repeated v128.const values cached in reserved XMM regs

	maxSpill int // high-water number of operand spill slots used
	// spillFloor temporarily reserves a low spill-slot range while wide-stack
	// canonicalization stages values above both their old homes and destinations.
	spillFloor              int
	subRspAt                int    // byte offset of the prologue's SubRsp imm32 (patched with frameSize)
	addRspAt                int    // byte offset of the epilogue's AddRsp imm32 (patched with frameSize)
	adapterReturnOff        int    // offset immediately after this function's root adapter CALL
	adapterEndOff           int    // end of the wrapper before internal-entry alignment
	adapterReturnReferenced bool   // cross-tail reuse embeds the local return PC; keep that tail local
	trapBodyOff             int    // complete shared trap body start; zero when not emitted
	trapBodyEnd             int    // complete shared trap body end before any literal pool
	guardMode               bool   // elide inline bounds checks; rely on guard-page + SIGSEGV trap
	boundsFacts             bool   // P6.1 straight-line bounds-check elision enabled (explicit mode)
	interruptible           bool   // emit context-cancellation polls at entries and loop headers
	functionCounters        bool   // atomically count internal function entries
	lazyZero                bool   // defer declared-local zeroing for small call+memory functions
	entryInitialized        uint64 // locals proven assigned before first entry-prefix read
	skipFence               bool   // call-free leaf with a provably small frame: no stack-fence check
	frameElided             bool   // register-homed call-free reg-ABI leaf: frameSize is 0 (see elideRegisterOnlyFrame)
	threadedMemory0         bool   // route shared memory zero through the private instance directory
	hasLoop                 bool   // retain loop alignment until it becomes a relaxable fragment
	hasJumpTableData        bool   // typed embedded data is remapped, but branches retain fixed widths

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
	regMerge        bool // reconcile single-result blocks in mergeReg/mergeFReg

	// call_indirect immutable-local-table specialization (see computeModuleHints).
	// Each admitted table has a finite proof that every non-null entry targets this
	// module, so no home/tag fork is needed; uniform type and monomorphic target
	// facts remain table-specific.
	immutableTables       []immutableTableHint
	stagedTailDescriptors bool

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

	// Straight-line bounds-check certificates (P6.1). Each entry records a stable
	// local/global address source and the largest checked extent. Keeping a small
	// set instead of one entry lets interleaved accesses to a few arrays reuse
	// their independent proofs without growing state with the function's locals.
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
	ctrl        []ctrlFrame // open block/loop/if/try frames; ctrl[0] is the function frame
	unreachable bool        // in dead code after an unconditional branch/trap
	ehTryDepth  int         // live reachable try_table records; bounded by maxEHTryRecords
	ehRootCount int         // compile-time assigned fixed exception roots; bounded by maxEHRootRecords

	// sc holds per-function scratch whose backing is reused across the module:
	// retSites, brFoldSites and trapSites live there so each function rewinds their
	// lengths (sc.reset) rather than reallocating. See scratch.
	sc *scratch

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
	importBindings []ImportBinding

	// syncHostCalls is set when the module has any returning host import, so every
	// host call in the module uses the synchronous control frame (callHostSync)
	// rather than the async log — the two share offCustomCtx and must not both be
	// live. Computed once per module in compileFunc.
	syncHostCalls          bool
	gcTypeSubtypingRefTest bool // typed function tests/casts resolve exact declared type identity after dynamic loads
	gcStructHelpers        bool // exact staged numeric struct ops use the same parked Go re-entry frame
	gcArrayHelpers         bool // exact staged numeric array ops use the same parked Go re-entry frame
	gcFrameRoots           *shared.GCFrameRootPlan
	gcCallsiteIndex        int
	compactFrameHeader     bool   // register ABI: no wrapper results-pointer header
	nativeStructAllocType  uint32 // type index + 1 for the next gcStructAllocOne call
	nativeArrayAlloc       gcArrayAllocStubSite

	// stats collects per-function codegen counters (docs/no-ir-plan.md P1). nil
	// unless the caller requested collection, in which case every counter method
	// is a no-op — the hot compile path is unaffected. See stats.go.
	stats  *CodegenStats
	policy CodegenPolicy
}

func (f *fn) opt(option optimization.Option) bool {
	if f.policy.Valid() {
		return f.policy.EnabledOption(option)
	}
	return currentCodegenPolicy().EnabledOption(option)
}

type transient struct {
	lsPool         [][]locState
	gcFactPool     [][]codegen.GCRefFact
	endsPool       [][]int
	tmpRoots       []*elem
	tmpTypes       []machineType
	tmpTypes2      []machineType
	tmpGCRoots     []bool
	tmpGCRoots2    []bool
	tmpGCFacts     []codegen.GCRefFact
	tmpGCFacts2    []codegen.GCRefFact
	tmpFlushTypes  []machineType
	tmpRegs        []Reg
	tmpSlots       []int
	tmpMoves       []regMove
	tmpLabels      []uint32
	tmpDeferred    []deferredArg
	tmpBelow       []*elem
	tmpGpCand      []gpCand
	tmpInts        []int
	tmpIntervalReg []Reg
	v128Pool       []poolConst // reusable 4/8/16-byte trailing rip-relative constants
	poolSites      []poolSite  // flat intrusive site lists; no per-constant allocation
	literalWords   []uint64    // packed compaction island plan; reusable per worker
}

// gpCand is a hot int local or global competing for a GP pin register, ranked by
// loop-weighted access score. See assignPinnedLocals.
type gpCand struct {
	global bool
	idx    int
	score  uint32
}

// deferredArg is a call argument (const/slot/localRef) staged into its target
// register after the parallel register-move resolves. See emitRegisterCallVia.
type deferredArg struct {
	target Reg
	root   *elem
}

type deferredMixedArg struct {
	target Reg
	root   *elem
	float  bool
}

// alignPad is a shared zero source for inter-function 16-byte alignment padding,
// appended via alignPad[:pad] to avoid allocating a temporary per function.
var alignPad [16]byte

// preloadScanGatesEnabled skips constant-cache body walks when the existing
// one-pass hints prove that the relevant instruction family is absent.
var preloadScanGatesEnabled = true

func align16(n int) int { return (n + 15) &^ 15 }

const (
	layoutHostAdapter uint8 = 1 << iota
	layoutHasLoop
	layoutHasCall
	layoutCallsSelf
)

func boolFlag(on bool, flag uint8) uint8 {
	if on {
		return flag
	}
	return 0
}

func functionStartPadding(off, bodyBytes int, hostAdapter bool, hints funcHints, policy CodegenPolicy) int {
	flags := boolFlag(hostAdapter, layoutHostAdapter) | boolFlag(hints.hasLoop, layoutHasLoop) |
		boolFlag(hints.hasCall, layoutHasCall) | boolFlag(hints.callsSelf, layoutCallsSelf)
	return functionStartPaddingFlags(off, bodyBytes, flags, policy)
}

func functionStartPaddingFlags(off, bodyBytes int, flags uint8, policy CodegenPolicy) int {
	optional := (-off) & 15
	if optional == 0 || policy.FunctionAlignLog2 < 4 {
		return 0
	}
	if flags&layoutHostAdapter != 0 {
		return optional
	}
	if flags&(layoutHasLoop|layoutHasCall|layoutCallsSelf) == 0 && bodyBytes < 256 {
		return 0
	}
	if optional <= min(15, bodyBytes/16) {
		return optional
	}
	return 0
}

func internalEntryShouldAlign(currentOff, bodyBytes int, policy CodegenPolicy) bool {
	pad := (-currentOff) & 15
	if pad == 0 || policy.InternalAlignLog2 < 4 {
		return false
	}
	return pad <= min(15, bodyBytes/16)
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

func moduleCodeCapacityAMD64(bodyBytes, functions int, policy CodegenPolicy) int {
	largeExpansionEighths := 37
	if policy.CompactNative {
		largeExpansionEighths = 25
	}
	return shared.TaperedModuleCodeCapacity(bodyBytes, functions, 40, largeExpansionEighths, 1<<20)
}

// scratch bundles the per-function compile buffers reused across all functions in
// one module compile. Every field is pure scratch that never outlives a
// function's compile — the emitted code is copied into the module buffer before
// the next function runs — so reset-and-reuse replaces per-function allocation.
// Compile is sequential, so a single scratch is shared safely.
type gcStructAllocStubSite struct {
	typeIndex uint32
	site      int
}

type gcArrayAllocMode uint8

const (
	gcArrayNativeNone gcArrayAllocMode = iota
	gcArrayNativeDefault
	gcArrayNativeUniform
	gcArrayNativeFixed
)

type gcArrayAllocStubSite struct {
	typeIndex uint32
	count     uint32
	site      int
	mode      gcArrayAllocMode
}

type jumpTableFragmentKind uint8

const (
	jumpTableFragmentIDs jumpTableFragmentKind = iota + 1
	jumpTableFragmentDeltas
)

// jumpTableFragment classifies the data bytes embedded between dispatch code
// and case stubs. The finalizer never decodes them as instructions; delta
// tables are explicitly remapped when surrounding code shrinks.
type jumpTableFragment struct {
	start int
	end   int
	kind  jumpTableFragmentKind
}

type scratch struct {
	stack             *stack     // the valent-block operand stack
	asm               *amd64.Asm // the x86-64 encoder byte buffer
	directPrepared    bool
	rel32TailBound    bool // Rel32Sites uses the current function buffer's uncommitted tail
	localRefTailBound bool // localRefs uses caller-owned compiler-arena tail scratch
	policy            CodegenPolicy
	classifier        wasm.ModuleInstructionClassifier
	fnState           fn // per-function compiler state, reused across the module

	// Per-function jump-site accumulators. Held here (not on fn) so their backing
	// arrays are retained and reused across every function in the module instead of
	// being reallocated per function; reset() only rewinds their lengths. trapSites
	// is indexed by trap code (a small dense enum), replacing a per-function map.
	retSites                []int
	tailFrameSites          []int // AddRsp imm32 sites emitted before tail jumps
	brFoldSites             []int
	gcArrayLenStubSites     []int
	gcFinalCastStubSites    []int
	gcArrayRefGetSites      []int
	gcStructRefGetStubSites []int
	gcStructRefSetStubSites []int
	gcArrayRefSetStubSites  []int
	gcStructAllocStubSites  []gcStructAllocStubSite
	gcArrayAllocStubSites   []gcArrayAllocStubSite
	trapSites               [trapMax + 1][]trapSite
	ctrl                    []ctrlFrame // control-frame stack backing; reused across functions
	pinnedLocals            []int       // pinned-local index backing; reused across functions
	brTableStubAt           []int       // duplicate-heavy jump-table target positions by control depth
	jumpTableFragments      []jumpTableFragment
	localRefs               amd64.LocalRefRecorder
	offsetMap               shared.WideOffsetMap
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

type trapSite struct {
	branch   int
	function uint32
	pc       uint32
}

func newScratch() *scratch {
	return newScratchWithStackCap(defaultStackArenaCap)
}

func newScratchWithStackCap(stackCap int) *scratch {
	return &scratch{stack: newStackWithCap(stackCap), asm: &amd64.Asm{}}
}

// moduleStackArenaCap chooses the first operand-stack chunk reused across the
// module. The one-pass function pre-scan already counts arena-producing nodes,
// so small modules need not reserve the legacy 256-element chunk. Chunk growth
// remains the conservative overflow path; incomplete hints retain the legacy
// capacity to avoid predictable growth churn.
func moduleStackArenaCap(m *wasm.Module, hints []funcHints) int {
	if len(hints) != len(m.Code) {
		return defaultStackArenaCap
	}
	capHint := minStackArenaCap
	for i := range hints {
		fnCap := stackArenaCapForHints(len(m.Code[i].BodyBytes), hints[i].nLocals, hints[i].stackArenaNodes)
		if fnCap >= defaultStackArenaCap {
			return defaultStackArenaCap
		}
		if fnCap > capHint {
			capHint = fnCap
		}
	}
	return capHint
}

const maxHintedControlFrames = 64

// moduleControlFrameCap sizes the reusable control stack from the same one-pass
// bytecode hints. Zero preserves lazy allocation for straight-line, incomplete,
// AST-only, or unusually deep modules; append remains the correctness fallback.
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

func (sc *scratch) reset() {
	sc.stack.reset()
	sc.asm.B = sc.asm.B[:0]
	sc.asm.UsesBMI2 = false
	sc.asm.LocalRefs = nil
	if compactNativePolicy(sc.policy) {
		sc.asm.ResetRel32Recorder(finalizerRel32Limit(sc.policy))
	} else {
		sc.asm.ResetRel32Recorder(0)
	}
	sc.directPrepared = false
	sc.retSites = sc.retSites[:0]
	sc.tailFrameSites = sc.tailFrameSites[:0]
	sc.brFoldSites = sc.brFoldSites[:0]
	sc.jumpTableFragments = sc.jumpTableFragments[:0]
	sc.gcArrayLenStubSites = sc.gcArrayLenStubSites[:0]
	sc.gcFinalCastStubSites = sc.gcFinalCastStubSites[:0]
	sc.gcArrayRefGetSites = sc.gcArrayRefGetSites[:0]
	sc.gcStructRefGetStubSites = sc.gcStructRefGetStubSites[:0]
	sc.gcStructRefSetStubSites = sc.gcStructRefSetStubSites[:0]
	sc.gcArrayRefSetStubSites = sc.gcArrayRefSetStubSites[:0]
	sc.gcStructAllocStubSites = sc.gcStructAllocStubSites[:0]
	sc.gcArrayAllocStubSites = sc.gcArrayAllocStubSites[:0]
	sc.ctrl = sc.ctrl[:0]
	for i := range sc.trapSites {
		sc.trapSites[i] = sc.trapSites[i][:0]
	}
}

// workerState owns every mutable buffer used by one parallel compiler worker.
// arena is append-only until all workers join. Results retain offsets into it,
// never slices, because a later append may reallocate the arena.
type workerState struct {
	scratch  *scratch
	arena    []byte
	literals []uint64
	usesBMI2 bool
}

// funcResult is one independently compiled local function. worker/start/end
// identify its owned bytes after the worker pool joins; relocs is independently
// owned by the fn compiler state (it is not backed by scratch).
type funcResult struct {
	worker         int
	start          int
	end            int
	internalOff    int
	bodyBytes      int
	layoutFlags    uint8
	directPrepared bool
	adapterTail    adapterTailInfo
	adapter        sharedAdapterInfo
	trapBody       sharedTrapBodyInfoAMD64
	relocs         []callReloc
	literalStart   int
	literalEnd     int
	omitted        bool
	err            error
}

func markDirectPrepared(bits []uint64, n, bit int) []uint64 {
	if bits == nil {
		bits = make([]uint64, (n+63)/64)
	}
	bits[bit>>6] |= uint64(1) << uint(bit&63)
	return bits
}

func countHostAdaptersAMD64(adapters []bool) int {
	n := 0
	for _, adapter := range adapters {
		if adapter {
			n++
		}
	}
	return n
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
	frameHdrBytes = shared.AMD64FrameHeaderBytes // spare + results ptr (keeps locals 16-aligned)
	frResultsOff  = 8                            // results buffer pointer
)

func (f *fn) frameHeaderBytes() int {
	if f.compactFrameHeader {
		return 0
	}
	return frameHdrBytes
}

// prepareCompactGCFrameHeader remaps the frontend's collector-local identities
// through the final local-slot layout without allocating another table. EH fixed
// roots and malformed plans retain the established wrapper header.
func (f *fn) prepareCompactGCFrameHeader(plan *shared.GCFrameRootPlan) bool {
	if plan == nil {
		return true
	}
	if !plan.Candidate || len(plan.FixedOffsets) != 0 || len(plan.LocalIndexes) != len(plan.LocalOffsets) {
		return false
	}
	for _, index := range plan.LocalIndexes {
		if int(index) >= f.nLocals {
			return false
		}
	}
	for i, index := range plan.LocalIndexes {
		plan.LocalOffsets[i] = uint32(f.localOff(int(index)))
	}
	return true
}

func (f *fn) localOff(i int) int32 {
	return int32(f.frameHeaderBytes() + int(uint32(f.localSlot[i])))
}
func (f *fn) localAddr(i int) int32 {
	off := f.localOff(i)
	if f.a.LocalRefs != nil {
		slot := uint64(f.localSlot[i])
		count := uint32(slot >> 32)
		if count != ^uint32(0) {
			count++
			f.localSlot[i] = int(uint64(count)<<32 | uint64(uint32(slot)))
		}
		f.a.LocalRefs.Mark(uint32(i))
	}
	if f.stats != nil {
		f.stats.Encoding.RecordLocalFrameAddress(off)
	}
	return off
}
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

func (f *fn) immutableTable(tableIdx uint32) (immutableTableHint, bool) {
	if int(tableIdx) >= len(f.immutableTables) || !f.immutableTables[tableIdx].local {
		return immutableTableHint{}, false
	}
	return f.immutableTables[tableIdx], true
}

// frameSize is biased to ≡ 8 (mod 16): the function is entered with RSP ≡ 8
// (mod 16) after the trampoline's CALL and there is no frame-pointer push to
// re-align, so `sub rsp,frameSize` must land the body on a 16-aligned RSP to keep
// our own call sites correctly aligned.
func (f *fn) frameSize() int {
	if f.frameElided {
		return 0 // frame never touched (all locals register-homed, no spills, no calls)
	}
	return align16(f.frameHeaderBytes()+8*f.nLocalSlots+f.ehFrameBytes()+8*f.maxSpill) + 8
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
	voidResult := len(f.ft.Results) == 0
	registerResult := f.singleRegResult || frameElideVoid && voidResult
	if !f.opt(optFrameElide) || !registerResult || f.moduleEH || f.usesCalls || f.maxSpill != 0 || len(f.localType) != f.nLocals {
		return false
	}
	if !f.allLocalsRegisterHomed() {
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

// ImportBinding is shared by both Railshot architectures.
type ImportBinding = shared.ImportBinding

// CompileOptions configures direct wasm-to-amd64 compilation.
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
	// a guard-page mapping + SIGSEGV handler (see runtime/sigtrap_linux_amd64.go).
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
	// and loop headers. A watcher writes TrapInterrupted to the invocation trap
	// cell; the poll observes it and takes the cold trap path, unwinding the whole
	// native call tree so a running wasm loop is cancelled within one iteration.
	Interruptible bool

	// FunctionCounters emits one atomic increment at each local function's
	// internal entry. The instance must install a uint64 counter slab in basedata.
	// Disabled compilation has no counter code or runtime allocation.
	FunctionCounters bool

	// MemoryPressure is called once after retained native output reaches
	// MemoryPressureAt bytes. With a zero threshold it runs at seven-eighths of
	// the reserved output capacity. Public compilation uses that late checkpoint
	// for large modules to reclaim dead per-function state without changing global
	// GC configuration. A nil callback disables it.
	MemoryPressureAt int
	MemoryPressure   func()

	// GCTypeSubtypingRefTest admits typed function ref.test/ref.cast lowering.
	// Direct ref.func values fold statically; dynamically loaded descriptors use
	// exact per-function type IDs with a cold full-metadata fallback. It is compile-only.
	GCTypeSubtypingRefTest bool

	// GCStructHelpers admits only the exact staged collector-backed struct helper
	// products selected by src/wago. It is compile-only and never serialized.
	GCStructHelpers bool

	// GCArrayHelpers independently admits exact staged collector-backed array
	// helper products selected by src/wago. It is compile-only and never serialized.
	GCArrayHelpers bool

	// GCFrameRoots is the optional exact single-frame GC root-publication plan.
	// The backend verifies allocating helper stack shape and records final frame
	// size. It is absent for ordinary modules and unsupported products.
	GCFrameRoots *shared.GCModuleFrameRootPlan

	// Codegen carries injectable runtime/heap dependencies for broader WasmGC
	// lowering. The exact staged numeric-struct helper path is selected separately
	// by GCStructHelpers; future general lowering must consume this HeapABI rather
	// than hard-coding a collector policy into the backend.
	Codegen codegen.Options

	// Stats, when non-nil, collects per-function codegen counters into it (the
	// "explain" dashboard, docs/no-ir-plan.md P1). Independent of WAGO_EXPLAIN,
	// which prints the same dump to stderr. nil = no collection, zero overhead.
	Stats *ModuleStats

	// CustomInstructions contains validated recipes keyed by imported function
	// index. Unsupported recipes remain ordinary host calls.
	CustomInstructions map[uint32]CustomInstruction
}

type CustomInstructionOp = railcore.CustomInstructionOp
type CustomInstructionNode = railcore.CustomInstructionNode
type CustomInstruction = railcore.CustomInstruction

const (
	CustomInstructionInput  = railcore.CustomInstructionInput
	CustomInstructionConst  = railcore.CustomInstructionConst
	CustomInstructionAdd    = railcore.CustomInstructionAdd
	CustomInstructionSub    = railcore.CustomInstructionSub
	CustomInstructionMul    = railcore.CustomInstructionMul
	CustomInstructionAnd    = railcore.CustomInstructionAnd
	CustomInstructionOr     = railcore.CustomInstructionOr
	CustomInstructionXor    = railcore.CustomInstructionXor
	CustomInstructionShl    = railcore.CustomInstructionShl
	CustomInstructionShrU   = railcore.CustomInstructionShrU
	CustomInstructionShrS   = railcore.CustomInstructionShrS
	CustomInstructionEq     = railcore.CustomInstructionEq
	CustomInstructionNe     = railcore.CustomInstructionNe
	CustomInstructionLtU    = railcore.CustomInstructionLtU
	CustomInstructionLtS    = railcore.CustomInstructionLtS
	CustomInstructionLeU    = railcore.CustomInstructionLeU
	CustomInstructionLeS    = railcore.CustomInstructionLeS
	CustomInstructionGtU    = railcore.CustomInstructionGtU
	CustomInstructionGtS    = railcore.CustomInstructionGtS
	CustomInstructionGeU    = railcore.CustomInstructionGeU
	CustomInstructionGeS    = railcore.CustomInstructionGeS
	CustomInstructionNot    = railcore.CustomInstructionNot
	CustomInstructionIsZero = railcore.CustomInstructionIsZero
	CustomInstructionSelect = railcore.CustomInstructionSelect
)

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
	compiled, err := compileModuleWith(m, opts)
	runtime.KeepAlive(m)
	runtime.KeepAlive(opts)
	return compiled, err
}

func compileModuleWith(m *wasm.Module, opts CompileOptions) (*amd64.CompiledModule, error) {
	if opts.FunctionCounters && m.ImportedFuncCount()+len(m.Code) > profile.MaxRailshotFunctionCounters {
		return nil, fmt.Errorf("amd64: %d function counters exceed limit %d", m.ImportedFuncCount()+len(m.Code), profile.MaxRailshotFunctionCounters)
	}
	selection, err := optimizationBindings.ResolveSnapshot(opts.Optimizations, opts.OptimizationSnapshot, opts.OptimizationDeltas)
	if err != nil {
		return nil, fmt.Errorf("amd64: %w", err)
	}
	policy := shared.DefaultCodegenPolicy(selection)
	if !nativeCompactionDisabled && opts.CompactNative {
		policy = shared.CompactCodegenPolicy(selection)
	}
	guardMode := opts.ElideBoundsChecks
	// P6.1 elision is on unless disabled per-compile (opts) or globally (env).
	boundsFacts := policy.EnabledOption(optBoundsFacts) && !opts.NoBoundsFacts
	n := len(m.Code)
	relocs := make([][]callReloc, n)
	var literalWords []uint64
	var literalOffsets []uint32
	if moduleLiteralIsland(policy) {
		literalOffsets = make([]uint32, n+1)
	}
	entry, internalEntry := shared.ModuleEntries(n)
	importedFuncs := m.ImportedFuncCount()
	nGlobals := m.GlobalCount()
	allHints, globalScores, err := computeModuleHintsWithPolicy(m, nGlobals, importedFuncs, opts.Codegen.Module.GCTypeLayouts, opts.GCStructHelpers, policy)
	if err != nil {
		return nil, fmt.Errorf("amd64: %w", err)
	}
	resolverSites := 0
	for i := range allHints {
		resolverSites += allHints[i].gcResolverSites
	}
	// A one-entry address certificate can collapse a single function's repeated
	// candidate sites to one real resolution. Compile that narrow shape inline
	// first and select the module island only when lowering proves at least two
	// resolutions remain. Multi-function modules retain the measured static
	// crossover because caches cannot cross function boundaries.
	deferSingleFuncGCResolverDecision := gcSharedStubsEnabled && gcResolveReuseEnabled && n == 1 && resolverSites >= 2
	useSharedGCResolver := gcSharedStubsEnabled && resolverSites >= 2 && !deferSingleFuncGCResolverDecision
	for i := range allHints {
		allHints[i].gcSharedResolver = useSharedGCResolver
	}
	modGlobals := pickModuleGlobals(m, nGlobals, globalScores)
	hostAdapters, err := shared.HostAdapterSet(m)
	if err != nil {
		return nil, fmt.Errorf("amd64: host adapter analysis: %w", err)
	}
	if len(opts.CustomInstructions) != 0 {
		// Custom lowerings may publish function identities through extension-owned
		// state. Until that interface reports exact addressability, fail closed.
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
		// A stats sink is reusable across compiles. Reset the complete module-level
		// attribution, including optional analyses that may be unavailable for the
		// next module, before installing this compile's deterministic destinations.
		*ms = ModuleStats{
			Funcs:            make([]*CodegenStats, n),
			ModuleGlobalPins: moduleGlobalPinInfos(modGlobals),
		}
		// Inline-candidate detection (report only; no codegen change yet). Failure
		// to analyze is non-fatal — it never blocks a compile.
		if rep, ierr := analyzeInlineCandidates(m, policy); ierr == nil {
			ms.Inline = rep
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
	if opts.GCFrameRoots != nil || opts.FunctionCounters {
		// Exact frame-root callsite masks are derived from the validated Wasm call
		// stream. Keep that stream one-to-one with native callsites rather than
		// silently invalidating collection metadata by splicing a local callee.
		// Function counters likewise need one real entry per logical invocation.
		inlineTargets = inlineTargetTable{}
	}
	requiresAVX2 := false
	requiresAVX512 := false
	requiresBMI2 := false
	for _, definition := range opts.CustomInstructions {
		if lowering := pluginAMD64Lowering(definition); lowering != nil {
			if lowering.Features&plugincodegen.FeatureAVX2 != 0 {
				requiresAVX2 = true
			}
			if lowering.Features&plugincodegen.FeatureAVX512 != 0 {
				requiresAVX512 = true
			}
		}
	}
	// Pre-size the module code buffer to roughly the final machine-code size
	// (lowering emits ~4-5 native bytes per wasm body byte, matching asmCapForBody).
	// Without this the buffer grows geometrically, and each reallocation copies the
	// whole accumulated code — on a 16 MB module that churns hundreds of MB of
	// garbage. Under-estimating only costs a tail append; it is never incorrect.
	totalBody := 0
	for i := range m.Code {
		totalBody += len(m.Code[i].BodyBytes)
	}
	codeCap := moduleCodeCapacityAMD64(totalBody, n, policy)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	workers := shared.ResolveWorkers(opts.Workers, n, runtime.GOMAXPROCS(0))
	if workers <= 1 {
		// Keep the serial compiler as a distinct fast path: one reusable scratch,
		// no goroutines, channels, atomics, worker metadata, or intermediate arena.
		sc := newScratchWithStackCap(moduleStackArenaCap(m, allHints))
		sc.policy = policy
		sc.classifier = classifier
		if ctrlCap := moduleControlFrameCap(m, allHints); ctrlCap != 0 {
			sc.ctrl = make([]ctrlFrame, 0, ctrlCap)
		}
		codeBuffer, err := coreruntime.NewCodeBuffer(codeCap)
		if err != nil {
			return nil, fmt.Errorf("amd64: allocate code image: %w", err)
		}
		keepCodeBuffer := false
		defer func() {
			if !keepCodeBuffer {
				_ = codeBuffer.Close()
			}
		}()
		pressureDone := false
		pressureAt := shared.PressureThreshold(opts.MemoryPressureAt, codeCap)
		var directPrepared []uint64
		var adapterTails []adapterTailInfo
		var adapters []sharedAdapterInfo
		if policy.CompactNative {
			if policy.EnabledOption(optSharedAdapters) {
				adapters = make([]sharedAdapterInfo, 0, countHostAdaptersAMD64(hostAdapters))
			} else {
				adapterTails = make([]adapterTailInfo, 0, countHostAdaptersAMD64(hostAdapters))
			}
		}
		var trapBodyCluster sharedTrapBodyClusterAMD64
		for i := range m.Code {
			var st *CodegenStats
			if ms != nil {
				st = &CodegenStats{FuncIdx: i, Name: funcDisplayName(m, i, importedFuncs)}
				ms.Funcs[i] = st
			}
			if inlineTargets.omitStandaloneBody(i, hostAdapters[i]) {
				st.peep("inline-dead-body")
				if literalOffsets != nil {
					literalOffsets[i+1] = uint32(len(literalWords))
				}
				allHints[i] = funcHints{}
				continue
			}
			// Align and reserve before lowering so the assembler can emit straight
			// into the module-owned image. If an unusually large function outgrows
			// the mapping tail, CommitTail rejects the detached slice and Append
			// preserves the old capacity-underestimate fallback.
			if pad := functionStartPadding(len(codeBuffer.Bytes()), len(m.Code[i].BodyBytes), hostAdapters[i], allHints[i], policy); pad != 0 {
				if err := codeBuffer.AppendZeros(pad); err != nil {
					return nil, fmt.Errorf("amd64: grow code image: %w", err)
				}
			}
			code := codeBuffer.Bytes()
			entry[i] = len(code)
			tailCap := asmCapForBody(len(m.Code[i].BodyBytes))
			if compactNativePolicy(policy) {
				tailCap += amd64.Rel32ScratchSize(finalizerRel32Limit(policy))
			}
			if symbolicLocalSlotPackingPolicy(policy) {
				tailCap += amd64.LocalRefScratchSize(maxAMD64LocalRefSites)
			}
			tail, err := codeBuffer.AppendTail(tailCap)
			if err != nil {
				return nil, fmt.Errorf("amd64: grow code image: %w", err)
			}
			sc.asm.B = tail
			sc.asm.Rel32Sites = nil
			sc.rel32TailBound = false
			sc.localRefs.Sites = nil
			sc.localRefTailBound = false
			hints := allHints[i]
			fnCode, rl, internalOff, err := compileFunc(m, opts.Codegen.Module.GCTypeLayouts, i, hostAdapters[i], guardMode, boundsFacts, opts.Interruptible, opts.FunctionCounters, modGlobals, hints, opts.ImportBindings, opts.SyncHostCalls, opts.GCTypeSubtypingRefTest, opts.GCStructHelpers, opts.GCArrayHelpers, opts.CustomInstructions, opts.GCFrameRoots.Function(i), st, inlineTargets, sc)
			if err == nil && deferSingleFuncGCResolverDecision && sc.fnState.gcHandleResolutions >= 2 {
				hints.gcSharedResolver = true
				resetFuncStats(st)
				fnCode, rl, internalOff, err = compileFunc(m, opts.Codegen.Module.GCTypeLayouts, i, hostAdapters[i], guardMode, boundsFacts, opts.Interruptible, opts.FunctionCounters, modGlobals, hints, opts.ImportBindings, opts.SyncHostCalls, opts.GCTypeSubtypingRefTest, opts.GCStructHelpers, opts.GCArrayHelpers, opts.CustomInstructions, opts.GCFrameRoots.Function(i), st, inlineTargets, sc)
			}
			allHints[i] = funcHints{}
			if err != nil {
				return nil, fmt.Errorf("amd64: function %d: %w", i, err)
			}
			requiresBMI2 = requiresBMI2 || sc.asm.UsesBMI2
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
			if sc.directPrepared {
				directPrepared = markDirectPrepared(directPrepared, n, i)
			}
			relocs[i] = rl
			if literalOffsets != nil {
				literalWords = append(literalWords, sc.fnState.literalWords...)
				if uint64(len(literalWords)) > math.MaxUint32 {
					return nil, fmt.Errorf("amd64: module literal metadata exceeds uint32")
				}
				literalOffsets[i+1] = uint32(len(literalWords))
			}
			if policy.EnabledOption(optSharedTrapBody) && moduleSharedTrapBodyEnabled && policy.CompactNative {
				fnCode = trapBodyCluster.shareFunction(hostAdapters[i], codeBuffer.Bytes(), fnCode, entry[i], sc.fnState.sharedTrapBodyInfoAMD64(), st)
			}
			if !codeBuffer.CommitTail(fnCode) {
				if err := codeBuffer.Append(fnCode); err != nil {
					return nil, fmt.Errorf("amd64: grow code image: %w", err)
				}
			}
			if !pressureDone && opts.MemoryPressure != nil && len(codeBuffer.Bytes()) >= pressureAt {
				pressureDone = true
				opts.MemoryPressure()
			}
		}
		sharedAdapterBytes := 0
		if adapters != nil {
			sharedAdapterBytes, err = shareAdaptersCodeBufferAMD64(codeBuffer, entry, internalEntry, relocs, literalWords, literalOffsets, adapters, opts.GCFrameRoots, ms)
			if err != nil {
				return nil, err
			}
		} else if adapterTails != nil {
			sharedAdapterBytes, err = shareAdapterTailsCodeBufferAMD64(codeBuffer, entry, internalEntry, relocs, literalWords, literalOffsets, adapterTails, opts.GCFrameRoots, ms)
			if err != nil {
				return nil, err
			}
		}
		if err := finalizeOmittedInlineEntriesAMD64(entry, internalEntry, relocs, hostAdapters, inlineTargets); err != nil {
			return nil, err
		}
		functionsEnd := len(codeBuffer.Bytes()) - sharedAdapterBytes
		literalCode, moduleLiterals := buildModuleLiteralIsland(literalWords, literalOffsets)
		literalBase := -1
		if len(literalCode) != 0 {
			literalBase = len(codeBuffer.Bytes())
			if err := codeBuffer.Append(literalCode); err != nil {
				return nil, fmt.Errorf("amd64: append literal island: %w", err)
			}
		}
		// Append one module-owned cold leaf island after every function, then patch
		// ordinary calls and shared-stub calls in one deterministic pass.
		stubCode, stubOffsets := buildModuleGCSharedStubs(relocs)
		stubBase := -1
		if len(stubCode) != 0 {
			if pad := (16 - len(codeBuffer.Bytes())%16) % 16; pad != 0 {
				if err := codeBuffer.AppendZeros(pad); err != nil {
					return nil, fmt.Errorf("amd64: grow GC stub island: %w", err)
				}
			}
			stubBase = len(codeBuffer.Bytes())
			if err := codeBuffer.Append(stubCode); err != nil {
				return nil, fmt.Errorf("amd64: append GC stub island: %w", err)
			}
			if ms != nil {
				ms.GCSharedStubBytes = len(stubCode)
				ms.GCSharedStubs = 1
				ms.GCSharedStubCallSites = countGCSharedStubRelocs(relocs)
			}
		}
		code := codeBuffer.Bytes()
		finalizeModuleNativeSizeAMD64(ms, len(code), functionsEnd, len(literalCode), len(codeBuffer.Mapping()))
		if err := patchModuleRelocs(code, entry, internalEntry, relocs, stubBase, stubOffsets); err != nil {
			return nil, err
		}
		if err := patchModuleLiteralRelocs(code, entry, literalWords, literalOffsets, literalBase, moduleLiterals); err != nil {
			return nil, err
		}
		if explainEnabled && ms != nil {
			fmt.Fprint(os.Stderr, ms.String())
		}
		keepCodeBuffer = true
		return &amd64.CompiledModule{Code: code, CodeImage: codeBuffer, Entry: entry, InternalEntry: internalEntry, DirectPrepared: directPrepared, RequiresBMI2: requiresBMI2, RequiresAVX2: requiresAVX2, RequiresAVX512: requiresAVX512}, nil
	}

	return compileModuleParallel(m, opts, workers, codeCap, entry, internalEntry, relocs, literalOffsets, allHints, modGlobals, hostAdapters, inlineTargets, policy, ms, guardMode, boundsFacts, importedFuncs)
}

// compileModuleParallel is split from CompileModuleWith so the goroutine closure
// and its captured state cannot escape into or add allocations to the serial path.
func compileModuleParallel(m *wasm.Module, opts CompileOptions, workers, codeCap int, entry, internalEntry []int, relocs [][]callReloc, literalOffsets []uint32, allHints []funcHints, modGlobals []moduleGlobalPin, hostAdapters []bool, inlineTargets inlineTargetTable, policy CodegenPolicy, ms *ModuleStats, guardMode, boundsFacts bool, importedFuncs int) (*amd64.CompiledModule, error) {
	n := len(m.Code)
	// Parallel codegen starts only after every module-wide decision is complete.
	// Each function has a deterministic stats destination, and each worker owns all
	// mutable compiler state plus an append-only arena for completed function code.
	if ms != nil {
		for i := range m.Code {
			ms.Funcs[i] = &CodegenStats{FuncIdx: i, Name: funcDisplayName(m, i, importedFuncs)}
		}
	}
	states := make([]workerState, workers)
	arenaCap := (codeCap + workers - 1) / workers
	if symbolicLocalSlotPackingPolicy(policy) {
		arenaCap += amd64.LocalRefScratchSize(maxAMD64LocalRefSites)
	}
	stackCap := moduleStackArenaCap(m, allHints)
	ctrlCap := moduleControlFrameCap(m, allHints)
	pressureAt := shared.PressureThreshold(opts.MemoryPressureAt, codeCap)
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	var pressureBytes atomic.Int64
	var pressureOnce sync.Once
	for i := range states {
		states[i] = workerState{scratch: newScratchWithStackCap(stackCap), arena: make([]byte, 0, arenaCap)}
		states[i].scratch.policy = policy
		states[i].scratch.classifier = classifier
		if ctrlCap != 0 {
			states[i].scratch.ctrl = make([]ctrlFrame, 0, ctrlCap)
		}
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
				if inlineTargets.omitStandaloneBody(i, hostAdapters[i]) {
					st.peep("inline-dead-body")
					allHints[i] = funcHints{}
					results[i] = funcResult{omitted: true}
					continue
				}
				arenaTail := ws.arena[len(ws.arena):cap(ws.arena)]
				ws.scratch.rel32TailBound = false
				ws.scratch.localRefTailBound = false
				if compactNativePolicy(policy) {
					ws.scratch.asm.Rel32Sites = nil
					n := amd64.Rel32ScratchSize(finalizerRel32Limit(policy))
					if len(arenaTail) >= n && ws.scratch.asm.BindRel32Storage(arenaTail[:n], finalizerRel32Limit(policy)) {
						ws.scratch.rel32TailBound = true
						arenaTail = arenaTail[n:]
					}
				}
				if symbolicLocalSlotPackingPolicy(policy) {
					ws.scratch.localRefs.Sites = nil
					n := amd64.LocalRefScratchSize(maxAMD64LocalRefSites)
					if len(arenaTail) >= n && ws.scratch.localRefs.BindStorage(arenaTail[:n], maxAMD64LocalRefSites) {
						ws.scratch.localRefTailBound = true
					}
				}
				hints := allHints[i]
				fnCode, rl, internalOff, err := compileFunc(m, opts.Codegen.Module.GCTypeLayouts, i, hostAdapters[i], guardMode, boundsFacts, opts.Interruptible, opts.FunctionCounters, modGlobals, hints, opts.ImportBindings, opts.SyncHostCalls, opts.GCTypeSubtypingRefTest, opts.GCStructHelpers, opts.GCArrayHelpers, opts.CustomInstructions, opts.GCFrameRoots.Function(i), st, inlineTargets, ws.scratch)
				allHints[i] = funcHints{}
				if err != nil {
					results[i].err = err
					continue
				}
				ws.usesBMI2 = ws.usesBMI2 || ws.scratch.asm.UsesBMI2
				start := len(ws.arena)
				ws.arena = append(ws.arena, fnCode...)
				flags := boolFlag(hostAdapters[i], layoutHostAdapter) | boolFlag(hints.hasLoop, layoutHasLoop) |
					boolFlag(hints.hasCall, layoutHasCall) | boolFlag(hints.callsSelf, layoutCallsSelf)
				literalStart := len(ws.literals)
				ws.literals = append(ws.literals, ws.scratch.fnState.literalWords...)
				result := funcResult{worker: workerID, start: start, end: len(ws.arena), internalOff: internalOff, bodyBytes: len(m.Code[i].BodyBytes), layoutFlags: flags, directPrepared: ws.scratch.directPrepared, relocs: rl, literalStart: literalStart, literalEnd: len(ws.literals)}
				if policy.CompactNative {
					if policy.EnabledOption(optSharedAdapters) {
						result.adapter = ws.scratch.fnState.sharedAdapterInfo()
					} else {
						result.adapterTail = ws.scratch.fnState.adapterTailInfo()
					}
					if policy.EnabledOption(optSharedTrapBody) && moduleSharedTrapBodyEnabled {
						result.trapBody = ws.scratch.fnState.sharedTrapBodyInfoAMD64()
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

	// Error selection is by function index, never by wall-clock completion order.
	if i, err := firstFuncError(results); err != nil {
		return nil, fmt.Errorf("amd64: function %d: %w", i, err)
	}

	// Join in original function order so layout, alignment, entry metadata, and
	// relocation patching are byte-for-byte identical to the serial compiler.
	code := make([]byte, 0, codeCap)
	var literalWords []uint64
	var directPrepared []uint64
	var adapterTails []adapterTailInfo
	var adapters []sharedAdapterInfo
	if policy.CompactNative {
		if policy.EnabledOption(optSharedAdapters) {
			adapters = make([]sharedAdapterInfo, 0, countHostAdaptersAMD64(hostAdapters))
		} else {
			adapterTails = make([]adapterTailInfo, 0, countHostAdaptersAMD64(hostAdapters))
		}
	}
	var trapBodyCluster sharedTrapBodyClusterAMD64
	for i := range results {
		r := &results[i]
		if r.omitted {
			if literalOffsets != nil {
				literalOffsets[i+1] = uint32(len(literalWords))
			}
			continue
		}
		if pad := functionStartPaddingFlags(len(code), r.bodyBytes, r.layoutFlags, policy); pad != 0 {
			code = append(code, alignPad[:pad]...)
		}
		entry[i] = len(code)
		internalEntry[i] = len(code) + r.internalOff
		if r.directPrepared {
			directPrepared = markDirectPrepared(directPrepared, n, i)
		}
		relocs[i] = r.relocs
		if adapterTails != nil && r.adapterTail.returnOff != 0 {
			r.adapterTail.function = uint32(i)
			adapterTails = append(adapterTails, r.adapterTail)
		}
		if adapters != nil && r.adapter.endOff != 0 {
			r.adapter.function = uint32(i)
			adapters = append(adapters, r.adapter)
		}
		if literalOffsets != nil {
			literalWords = append(literalWords, states[r.worker].literals[r.literalStart:r.literalEnd]...)
			if uint64(len(literalWords)) > math.MaxUint32 {
				return nil, fmt.Errorf("amd64: module literal metadata exceeds uint32")
			}
			literalOffsets[i+1] = uint32(len(literalWords))
		}
		fnCode := states[r.worker].arena[r.start:r.end]
		if policy.EnabledOption(optSharedTrapBody) && moduleSharedTrapBodyEnabled && policy.CompactNative {
			var st *CodegenStats
			if ms != nil {
				st = ms.Funcs[i]
			}
			fnCode = trapBodyCluster.shareFunction(r.layoutFlags&layoutHostAdapter != 0, code, fnCode, entry[i], r.trapBody, st)
		}
		code = append(code, fnCode...)
	}
	sharedAdapterBytes := 0
	if adapters != nil {
		var err error
		code, sharedAdapterBytes, err = shareAdaptersAMD64(code, entry, internalEntry, relocs, literalWords, literalOffsets, adapters, opts.GCFrameRoots, ms)
		if err != nil {
			return nil, err
		}
	} else if adapterTails != nil {
		var err error
		code, sharedAdapterBytes, err = shareAdapterTailsAMD64(code, entry, internalEntry, relocs, literalWords, literalOffsets, adapterTails, opts.GCFrameRoots, ms)
		if err != nil {
			return nil, err
		}
	}
	if err := finalizeOmittedInlineEntriesAMD64(entry, internalEntry, relocs, hostAdapters, inlineTargets); err != nil {
		return nil, err
	}
	functionsEnd := len(code) - sharedAdapterBytes
	literalCode, moduleLiterals := buildModuleLiteralIsland(literalWords, literalOffsets)
	literalBase := -1
	if len(literalCode) != 0 {
		literalBase = len(code)
		code = append(code, literalCode...)
	}
	stubCode, stubOffsets := buildModuleGCSharedStubs(relocs)
	stubBase := -1
	if len(stubCode) != 0 {
		if pad := (16 - len(code)%16) % 16; pad != 0 {
			code = append(code, alignPad[:pad]...)
		}
		stubBase = len(code)
		code = append(code, stubCode...)
		if ms != nil {
			ms.GCSharedStubBytes = len(stubCode)
			ms.GCSharedStubs = 1
			ms.GCSharedStubCallSites = countGCSharedStubRelocs(relocs)
		}
	}
	if err := patchModuleRelocs(code, entry, internalEntry, relocs, stubBase, stubOffsets); err != nil {
		return nil, err
	}
	if err := patchModuleLiteralRelocs(code, entry, literalWords, literalOffsets, literalBase, moduleLiterals); err != nil {
		return nil, err
	}
	finalizeModuleNativeSizeAMD64(ms, len(code), functionsEnd, len(literalCode), 0)
	if explainEnabled && ms != nil {
		fmt.Fprint(os.Stderr, ms.String())
	}
	requiresAVX2 := false
	requiresAVX512 := false
	requiresBMI2 := false
	for i := range states {
		requiresBMI2 = requiresBMI2 || states[i].usesBMI2
	}
	for _, definition := range opts.CustomInstructions {
		if lowering := pluginAMD64Lowering(definition); lowering != nil {
			requiresAVX2 = requiresAVX2 || lowering.Features&plugincodegen.FeatureAVX2 != 0
			requiresAVX512 = requiresAVX512 || lowering.Features&plugincodegen.FeatureAVX512 != 0
		}
	}
	return &amd64.CompiledModule{Code: code, Entry: entry, InternalEntry: internalEntry, DirectPrepared: directPrepared, RequiresBMI2: requiresBMI2, RequiresAVX2: requiresAVX2, RequiresAVX512: requiresAVX512}, nil
}

func countGCSharedStubRelocs(relocs [][]callReloc) int {
	count := 0
	for i := range relocs {
		for _, reloc := range relocs[i] {
			if reloc.gcStub != gcSharedStubNone {
				count++
			}
		}
	}
	return count
}

func finalizeOmittedInlineEntriesAMD64(entry, internalEntry []int, relocs [][]callReloc, hostAdapters []bool, targets inlineTargetTable) error {
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
		return fmt.Errorf("amd64: every local function was marked as an omitted inline body")
	}
	for caller := range relocs {
		for _, rl := range relocs[caller] {
			if rl.target >= 0 && rl.target < len(entry) && targets.omitStandaloneBody(rl.target, hostAdapters[rl.target]) {
				return fmt.Errorf("amd64: function %d retains relocation to omitted inline body %d", caller, rl.target)
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

func patchModuleRelocs(code []byte, entry, internalEntry []int, relocs [][]callReloc, stubBase int, stubOffsets [gcSharedStubMax]int) error {
	if len(entry) < len(relocs) {
		return fmt.Errorf("amd64: relocation entry table has %d functions, want at least %d", len(entry), len(relocs))
	}
	for i := range relocs {
		base := entry[i]
		if base < 0 || base > len(code) {
			return fmt.Errorf("amd64: invalid function %d entry %#x for %d-byte code image", i, base, len(code))
		}
		for _, rl := range relocs[i] {
			if rl.at < 0 || base > len(code)-4 || rl.at > len(code)-base-4 {
				return fmt.Errorf("amd64: invalid relocation site %#x in function %d for %d-byte code image", rl.at, i, len(code))
			}
			site := base + rl.at
			target := 0
			if rl.gcStub != gcSharedStubNone {
				if stubBase < 0 || stubBase > len(code) || rl.gcStub >= gcSharedStubMax || stubOffsets[rl.gcStub] < 0 || stubOffsets[rl.gcStub] > len(code)-stubBase {
					return fmt.Errorf("amd64: missing shared GC stub %d for function %d", rl.gcStub, i)
				}
				target = stubBase + stubOffsets[rl.gcStub]
			} else {
				if rl.target < 0 || rl.target >= len(entry) {
					return fmt.Errorf("amd64: invalid call relocation target %d for function %d", rl.target, i)
				}
				target = entry[rl.target]
				if rl.internal {
					if rl.target >= len(internalEntry) {
						return fmt.Errorf("amd64: missing internal entry for call relocation target %d in function %d", rl.target, i)
					}
					target = internalEntry[rl.target]
				}
				if target < 0 || target > len(code) {
					return fmt.Errorf("amd64: invalid call relocation target entry %#x for function %d", target, i)
				}
			}
			delta := int64(target) - int64(site+4)
			if delta < math.MinInt32 || delta > math.MaxInt32 {
				return fmt.Errorf("amd64: relocation from %#x to %#x exceeds rel32 range", site, target)
			}
			binary.LittleEndian.PutUint32(code[site:], uint32(int32(delta)))
		}
	}
	return nil
}

func firstFuncError(results []funcResult) (int, error) {
	return shared.FirstErrorIndex(len(results), func(i int) error { return results[i].err })
}

func finalizeModuleNativeSizeAMD64(ms *ModuleStats, codeLen, functionsEnd, moduleLiteralBytes, mappedBytes int) {
	if ms == nil {
		return
	}
	var native shared.NativeSizeReport
	var encoding amd64.EncodingStats
	native.TotalBytes = codeLen
	for _, fn := range ms.Funcs {
		if fn != nil {
			native.AddFunction(fn.NativeSize)
			encoding.Add(fn.Encoding)
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
	if native.LiteralPoolBytes != 0 {
		keys := make(map[literalKey]struct{}) // stats-only; ordinary compilation has nil ms
		for _, fn := range ms.Funcs {
			if fn == nil {
				continue
			}
			for _, key := range fn.literalKeys {
				keys[key] = struct{}{}
			}
		}
		for key := range keys {
			native.LiteralPoolUniqueBytes += int(key.size)
		}
		native.LiteralPoolDuplicateBytes = native.LiteralPoolBytes - native.LiteralPoolUniqueBytes
	}
	native.FunctionAlignmentBytes = functionsEnd - native.FunctionBytes
	if native.FunctionAlignmentBytes < 0 {
		native.FunctionAlignmentBytes = 0
	}
	native.ModuleOtherBytes = codeLen - functionsEnd
	if moduleLiteralBytes != 0 {
		native.LiteralPoolBytes = moduleLiteralBytes
		native.LiteralPoolUniqueBytes = moduleLiteralBytes
		native.LiteralPoolDuplicateBytes = 0
	}
	ms.NativeSize = native
	ms.Encoding = encoding
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
	memory64 := false
	if mt, ok := m.MemoryType(0); ok {
		memory64 = mt.Limits.Addr64
	}
	if len(m.Code[funcIdx].BodyBytes) != 0 {
		return scanBodyBytesMemory64(m.Code[funcIdx].BodyBytes, nLocals, nGlobals, uint32(importedFuncs+funcIdx), memory64)
	}
	return scanFuncBody(m.Code[funcIdx], nLocals, nGlobals, uint32(importedFuncs+funcIdx))
}

// computeModuleHints scans every function body ONCE, returning per-function hints
// plus the module-wide aggregated global scores. scanFuncBody already computes a
// per-function globalScore, and the module score for a global is just the sum of
// those across functions — so summing here removes a second full-body
// immediate-decoding pass per function (the standalone global-scores scan). The
// standalone computeModuleGlobalScores is retained as the parity oracle in tests.
func computeModuleHints(m *wasm.Module, nGlobals, importedFuncs int, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool) ([]funcHints, []int64, error) {
	return computeModuleHintsWithPolicy(m, nGlobals, importedFuncs, gcTypeLayouts, gcStructHelpers, currentCodegenPolicy())
}

func computeModuleHintsWithPolicy(m *wasm.Module, nGlobals, importedFuncs int, gcTypeLayouts []codegen.GCTypeLayout, gcStructHelpers bool, policy CodegenPolicy) ([]funcHints, []int64, error) {
	n := len(m.Code)
	allHints := make([]funcHints, n)
	totalLocals := 0
	intervalLocals := 0
	moduleHasTailCall := false
	moduleEH := m.TagCount() != 0
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
		allHints[i].nLocals = count
		totalLocals += count
		if intervalRegionHintStorageEligible(policy.EnabledOption(optIntervalRegionPins), len(m.Code[i].BodyBytes), count, moduleEH) {
			if count > int(^uint(0)>>1)-intervalLocals {
				return nil, nil, fmt.Errorf("function hint interval locals overflow")
			}
			intervalLocals += count
		}
	}
	if nGlobals > 0 && n > int(^uint(0)>>1)/nGlobals {
		return nil, nil, fmt.Errorf("function hint globals overflow")
	}
	localScores := make([]uint32, totalLocals)
	localLastGets := make([]uint32, intervalLocals)
	denseGlobals := uint64(n)*uint64(nGlobals) <= 1<<20
	var globalScores []uint32
	var globalEligibility []bool
	if denseGlobals {
		globalScores = make([]uint32, n*nGlobals)
		globalEligibility = make([]bool, n*nGlobals)
	}
	type hintRange struct{ start, end int }
	var sparseRanges []hintRange
	var sparseGlobals []shared.GlobalHint
	var sparseAccum shared.GlobalHintAccumulator
	if !denseGlobals {
		sparseRanges = make([]hintRange, n)
	}
	eligibilityTracker := newGlobalEligibilityTracker(nGlobals)
	var agg []int64
	if nGlobals > 0 && n > 0 {
		agg = make([]int64, nGlobals)
	}
	memory64 := false
	if mt, ok := m.MemoryType(0); ok {
		memory64 = mt.Limits.Addr64
	}
	localAt := 0
	intervalAt := 0
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for i := range m.Code {
		nLocals := allHints[i].nLocals
		var h funcHints
		if denseGlobals {
			globalAt := i * nGlobals
			h = funcHintsWithStorage(localScores[localAt:localAt+nLocals], globalScores[globalAt:globalAt+nGlobals], globalEligibility[globalAt:globalAt+nGlobals])
		} else {
			sparseAccum.Reset(nGlobals)
			h = funcHintsWithStorage(localScores[localAt:localAt+nLocals], nil, nil)
			h.globalAccum = &sparseAccum
		}
		if intervalRegionHintStorageEligible(policy.EnabledOption(optIntervalRegionPins), len(m.Code[i].BodyBytes), nLocals, moduleEH) {
			h.localLastGet = localLastGets[intervalAt : intervalAt+nLocals]
			intervalAt += nLocals
		}
		h.nLocals = nLocals
		h.inlineCallSites = allHints[i].inlineCallSites
		var err error
		h, err = scanFuncBodyIntoMemory64WithModuleCalls(m.Code[i], nLocals, nGlobals, uint32(importedFuncs+i), h, &eligibilityTracker, memory64, m, &classifier, gcTypeLayouts, gcStructHelpers, allHints, importedFuncs)
		if err != nil {
			return nil, nil, fmt.Errorf("function %d hints: %w", i, err)
		}
		h.inlineCallSites = allHints[i].inlineCallSites
		localAt += nLocals
		h.globalAccum = nil
		allHints[i] = h
		moduleHasTailCall = moduleHasTailCall || h.hasTailCall
		moduleEH = moduleEH || h.moduleEH
		if denseGlobals {
			for g := 0; g < nGlobals; g++ {
				agg[g] += int64(h.globalScore[g])
			}
		} else {
			start := len(sparseGlobals)
			sparseGlobals = sparseAccum.AppendTo(sparseGlobals)
			sparseRanges[i] = hintRange{start: start, end: len(sparseGlobals)}
			for _, gh := range sparseGlobals[start:] {
				agg[gh.Index] += int64(gh.Score)
			}
		}
	}
	if !denseGlobals {
		for i, r := range sparseRanges {
			allHints[i].sparseGlobals = sparseGlobals[r.start:r.end]
		}
	}
	// Immutable local-table specialization for call_indirect and indirect tails.
	// The proof is per table: imports are allowed elsewhere in the module, but an
	// admitted table itself must be local, unexported, never mutated, and contain
	// only local function descriptors. This is finite and keeps host/cross-instance
	// descriptors out of the internal-entry path.
	var immutableTables []immutableTableHint
	if m.TableCount() != 0 {
		immutableTables = make([]immutableTableHint, m.TableCount())
	}
	immutableCandidates := policy.EnabledOption(optImmutableTable) && m.ImportedTableCount() == 0
	if immutableCandidates {
		for i := range allHints {
			if allHints[i].mutatesTable {
				immutableCandidates = false
				break
			}
		}
	}
	if immutableCandidates {
		for tableIdx := range m.Tables {
			idx := uint32(tableIdx)
			if moduleExportsTable(m, idx) || !immutableLocalTableEntries(m, idx) {
				continue
			}
			tableType, tableTyped := immutableLocalTableTypeWithPolicy(m, idx, policy)
			immutableTables[tableIdx] = immutableTableHint{
				local:             true,
				typeKey:           tableType,
				typed:             tableTyped,
				monomorphicTarget: immutableLocalTableTarget(m, idx),
			}
		}
	}
	for i := range allHints {
		allHints[i].immutableTables = immutableTables
		allHints[i].hasTailCall = moduleHasTailCall
		allHints[i].moduleEH = moduleEH
	}
	return allHints, agg, nil
}

// immutableLocalTableTarget returns the sole local function stored in tableIdx,
// or -1 when entries may name different functions (or use expression forms the
// narrow specialization does not prove). The immutable-table preconditions are
// checked by computeModuleHints before this helper is used.
func immutableLocalTableTarget(m *wasm.Module, tableIdx uint32) int {
	target := -1
	// A table initializer prefills every slot with its default element, so that
	// target is also a possible non-null entry (active elements below override
	// individual slots). Fold it into the monomorphic set; a non-ref.func/-ref.null
	// initializer we cannot prove disqualifies the direct-call specialization.
	if int(tableIdx) < len(m.Tables) && m.Tables[tableIdx].Init != nil {
		ee, err := wasm.ParseElementExpr(*m.Tables[tableIdx].Init)
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
		if uint32(e.Mode.Table) != tableIdx {
			continue
		}
		if e.Kind.Kind != wasm.ElemFuncs {
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

func moduleExportsTable(m *wasm.Module, tableIdx uint32) bool {
	for i := range m.Exports {
		if m.Exports[i].Index.Kind == wasm.ExternTable && uint32(m.Exports[i].Index.Index) == tableIdx {
			return true
		}
	}
	return false
}

// immutableLocalTableEntries proves that every statically installed non-null
// entry in tableIdx names a local function. With no table mutation/import/export,
// no host or cross-instance descriptor can subsequently enter the table.
func immutableLocalTableEntries(m *wasm.Module, tableIdx uint32) bool {
	if int(tableIdx) >= len(m.Tables) {
		return false
	}
	if init := m.Tables[tableIdx].Init; init != nil {
		ee, err := wasm.ParseElementExpr(*init)
		if err != nil || ee.HasGlobal || (!ee.Null && int(ee.FuncIndex) < m.ImportedFuncCount()) {
			return false
		}
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive || uint32(e.Mode.Table) != tableIdx {
			continue
		}
		switch e.Kind.Kind {
		case wasm.ElemFuncs:
			for _, idx := range e.Kind.Funcs {
				if int(idx) < m.ImportedFuncCount() || int(idx)-m.ImportedFuncCount() >= len(m.Code) {
					return false
				}
			}
		default:
			for _, expr := range e.Kind.Exprs {
				ee, err := wasm.ParseElementExpr(expr)
				if err != nil || ee.HasGlobal || (!ee.Null && (int(ee.FuncIndex) < m.ImportedFuncCount() || int(ee.FuncIndex)-m.ImportedFuncCount() >= len(m.Code))) {
					return false
				}
			}
		}
	}
	return true
}

// immutableLocalTableType returns the shared structural type key of every entry
// in tableIdx (and true) when the whole immutable table is uniformly typed, so
// the indirect-call type check can be elided. Returns (0, false) otherwise.
func immutableLocalTableType(m *wasm.Module, tableIdx uint32) (uint64, bool) {
	return immutableLocalTableTypeWithPolicy(m, tableIdx, currentCodegenPolicy())
}

func immutableLocalTableTypeWithPolicy(m *wasm.Module, tableIdx uint32, policy CodegenPolicy) (uint64, bool) {
	if !policy.EnabledOption(optImmutableTableType) || int(tableIdx) >= len(m.Tables) || m.Tables[tableIdx].Init != nil {
		return 0, false
	}
	var want uint64
	found := false
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Mode.Kind != wasm.ElemActive {
			continue // cannot reach the table without table.init, already excluded
		}
		if uint32(e.Mode.Table) != tableIdx {
			continue
		}
		if e.Kind.Kind != wasm.ElemFuncs {
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
func compileFunc(m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, funcIdx int, hostAdapter, guardMode, boundsFacts, interruptible, functionCounters bool, modGlobals []moduleGlobalPin, hints funcHints, importBindings []ImportBinding, syncHostCalls, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers bool, custom map[uint32]CustomInstruction, gcFrameRoots *shared.GCFrameRootPlan, stats *CodegenStats, inlineTargets inlineTargetTable, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
	if gcFrameRoots != nil && gcFrameRoots.Candidate {
		gcFrameRoots.Exact = true
		gcFrameRoots.Safepoints = gcFrameRoots.Safepoints[:0]
		gcFrameRoots.Callsites = gcFrameRoots.Callsites[:0]
		gcFrameRoots.FrameBytes = 0
		gcFrameRoots.AdapterReturnOffset = 0
	}
	moduleEH := hints.moduleEH
	pinLocals := !moduleEH
	if !pinLocals {
		// The bounded EH handler restores an older native frame directly. Keep
		// locals/globals canonical in memory until handler-state convergence is
		// generalized to pinned registers.
		modGlobals = nil
	}
	code, relocs, internalOff, err = compileFuncAttempt(m, gcTypeLayouts, funcIdx, hostAdapter, guardMode, boundsFacts, interruptible, functionCounters, modGlobals, hints, importBindings, syncHostCalls, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers, moduleEH, custom, gcFrameRoots, stats, pinLocals, inlineTargets, sc)
	if errors.Is(err, errRegExhausted) {
		resetFuncStats(stats)
		code, relocs, internalOff, err = compileFuncAttempt(m, gcTypeLayouts, funcIdx, hostAdapter, guardMode, boundsFacts, interruptible, functionCounters, modGlobals, hints, importBindings, syncHostCalls, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers, moduleEH, custom, gcFrameRoots, stats, false, inlineTargets, sc)
		if err == nil {
			stats.setUnpinnedRetry()
		}
	}
	return
}

func compileFuncAttempt(m *wasm.Module, gcTypeLayouts []codegen.GCTypeLayout, funcIdx int, hostAdapter, guardMode, boundsFacts, interruptible, functionCounters bool, modGlobals []moduleGlobalPin, hints funcHints, importBindings []ImportBinding, syncHostCalls, gcTypeSubtypingRefTest, gcStructHelpers, gcArrayHelpers, moduleEH bool, custom map[uint32]CustomInstruction, gcFrameRoots *shared.GCFrameRootPlan, stats *CodegenStats, pinLocals bool, inlineTargets inlineTargetTable, sc *scratch) (code []byte, relocs []callReloc, internalOff int, err error) {
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
	if stats != nil {
		sc.asm.EncodingStats = &stats.Encoding
	} else {
		sc.asm.EncodingStats = nil
	}
	sc.asm.Grow(asmCapForBody(len(c.BodyBytes)))
	if compactNativePolicy(sc.policy) {
		if hints.hasLoop && !loopCompactionEnabled || hints.hasJumpTableData && !jumpTableCompactionEnabled || len(custom) != 0 {
			// These are finalizer exclusions known before emission. Avoid
			// recording sites only to take identity.
			sc.asm.ResetRel32Recorder(0)
		} else if !sc.rel32TailBound && sc.asm.BindRel32Tail(finalizerRel32Limit(sc.policy)) {
			sc.rel32TailBound = true
		}
	}
	if symbolicLocalSlotPackingPolicy(sc.policy) && !sc.localRefTailBound && sc.asm.BindLocalRefTail(&sc.localRefs, maxAMD64LocalRefSites) {
		sc.localRefTailBound = true
	}
	globalIdx := m.ImportedFuncCount() + funcIdx
	entryInitialized := hints.entryInitialized
	if gcFrameRoots != nil && gcFrameRoots.Candidate {
		// Conservative root maps may publish a reference local before its first
		// Wasm assignment. Keep every declared slot zero-initialized so a reused
		// foreign stack cannot expose a stale compact handle at that safepoint.
		entryInitialized = 0
	}
	f := &sc.fnState
	policy := sc.policy
	if !policy.Valid() {
		policy = currentCodegenPolicy()
	}
	sc.asm.CompactAccumulatorImmediates = compactAccumulatorImmediatePolicy(policy)
	localType, localSlot, localGCRefFacts, locals := f.localType, f.localSlot, f.localGCRefFacts, f.locals
	mt0, _ := m.MemoryType(0)
	*f = fn{a: sc.asm, s: sc.stack, sc: sc, m: m, ft: ft, gcTypeLayouts: gcTypeLayouts, transient: sc.transient, globalIdx: globalIdx, traceFuncIdx: uint32(globalIdx), tracePCBase: c.LocalDeclBytes, customInstructions: custom, nParams: len(ft.Params), nLocals: nLocals, localType: localType, localSlot: localSlot, localGCRefFacts: localGCRefFacts, locals: locals, guardMode: guardMode, boundsFacts: boundsFacts, interruptible: interruptible, functionCounters: functionCounters, regMerge: policy.EnabledOption(optRegMerge) && !moduleEH, globalCellReg: regNone, memSizeReg: regNone, immutableTables: hints.immutableTables, stagedTailDescriptors: hints.hasTailCall, importBindings: importBindings, stats: stats, policy: policy, entryInitialized: entryInitialized, gcFrameRoots: gcFrameRoots, moduleEH: moduleEH, threadedMemory0: mt0.Shared, hasLoop: hints.hasLoop, gcSharedResolver: hints.gcSharedResolver, classifier: sc.classifier}
	f.v128Pool = f.v128Pool[:0]
	f.poolSites = f.poolSites[:0]
	f.literalWords = f.literalWords[:0]
	// Retain the (possibly grown) control-frame backing for the next function.
	defer func() {
		sc.ctrl = f.ctrl
		sc.transient = f.transient
	}()
	f.syncHostCalls = syncHostCalls || gcStructHelpers || gcArrayHelpers || moduleUsesSyncHostCalls(m, importBindings)
	f.gcTypeSubtypingRefTest = gcTypeSubtypingRefTest
	f.gcStructHelpers = gcStructHelpers
	f.gcArrayHelpers = gcArrayHelpers
	if !guardMode && len(m.Memories) > 0 {
		f.memSizeReg = R15 // explicit bounds: R15 = memBytes for the whole module
	}
	if cap(f.localType) < nLocals {
		f.localType = make([]machineType, nLocals)
	} else {
		f.localType = f.localType[:nLocals]
	}
	if policy.EnabledOption(optGCRefFacts) {
		if cap(f.localGCRefFacts) < nLocals {
			f.localGCRefFacts = make([]codegen.GCRefFact, nLocals)
		} else {
			f.localGCRefFacts = f.localGCRefFacts[:nLocals]
			clear(f.localGCRefFacts)
		}
	} else {
		// The facts-off oracle must remove the optimizer's local table and all
		// control snapshots, not merely suppress consumers of retained storage.
		f.localGCRefFacts = nil
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
	recBase, recLength, _ := nativeGCRecGroup(m, m.FuncTypes[funcIdx].Index)
	f.seedFinalGCParameterTypes(ft.Params, recBase, recLength)
	if cap(f.localSlot) < nLocals {
		f.localSlot = make([]int, nLocals)
	} else {
		f.localSlot = f.localSlot[:nLocals]
	}
	// Call-free integer kernels benefit from a denser frame: two i32
	// locals share one 8-byte slot. Besides halving their footprint, this keeps
	// more RSP-relative accesses in x86's compact disp8 encoding. Structured
	// control flow uses the same typed local load/store helpers and is safe to
	// admit. Call argument staging selects local load width from the value type;
	// inline-only scratch locals remain naturally word-sized beyond the packed
	// caller region. EH/GC layouts remain deliberately boring. Tiny functions stay
	// on the old layout because their locals are normally register-homed and do not
	// repay a second layout mode.
	i32Locals := 0
	for _, mt := range f.localType {
		if mt == mtI32 {
			i32Locals++
		}
	}
	compactI32Frame := f.opt(optCompactI32Frame) && (!hints.hasCall || compactI32CallsEnabled) && (!hints.hasControlFlow || compactI32ControlFlowEnabled) && !moduleEH && gcFrameRoots == nil && i32Locals >= 2
	if compactI32Frame {
		f.stats.peep("compact-i32-frame")
	}
	localBytes := 0
	for i, mt := range f.localType {
		if compactI32Frame && mt == mtI32 {
			f.localSlot[i] = localBytes
			localBytes += 4
			continue
		}
		localBytes = (localBytes + 7) &^ 7
		f.localSlot[i] = localBytes
		localBytes += 8 * mt.stackSlots()
	}
	f.nLocalSlots = (localBytes + 7) / 8
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
	if f.opt(optInlineCallFree) && hasCall && allCallsWillInline(c, inlineTargets, f.policy) {
		hasCall = false
		f.stats.peep("all-calls-inlined")
	}
	regABI := f.opt(optRegABI) && (sigFitsRegABI(ft) || (f.stagedTailDescriptors && sigFitsReferenceResultRegABI(ft)))
	// Ordinary register-ABI bodies never consume frResultsOff: adapters preserve
	// RCX below the internal frame and direct calls return in registers. Retain the
	// established header for tail transfer, EH, and GC-frame paths whose auxiliary
	// offset protocols still refer to that fixed layout.
	f.compactFrameHeader = compactRegABIFrameHeader && regABI && !hints.hasTailCall && !moduleEH
	if f.compactFrameHeader && !f.prepareCompactGCFrameHeader(gcFrameRoots) {
		f.compactFrameHeader = false
	}
	if f.compactFrameHeader {
		f.stats.peep("frame-header-elide")
	}
	var gpPoolStorage [16]Reg
	gpPool := gpPinPool(gpPoolStorage[:0], regABI, f.nParams, !hasCall, f.opt(optEntryArgPins))
	if compactLowPinEnabled && f.policy.CompactNative && !hasCall && !hints.hasControlFlow {
		preferPinReg(gpPool, RBP)
	}
	// Tiny prepared integer leaves can use a slimmer host trampoline when their
	// generated code is constrained to caller-saved GPRs. Reserve every Go
	// callee-saved allocatable register up front; RBX remains the explicit linMem
	// input. The body/local bounds keep any spill tradeoff away from larger code.
	volatilePrepared := regABI && preparedDirectIntSig(ft) && !hasCall && !touchesMemory && len(modGlobals) == 0 && !moduleEH &&
		len(c.BodyBytes) <= 96 && nLocals <= 8
	if volatilePrepared {
		for _, r := range [...]Reg{RBP, R12, R13, R14, R15} {
			gpPool = withoutReg(gpPool, r)
			f.reserved = f.reserved.add(r)
		}
	}
	if moduleEH {
		// Staged EH carries the active handler in RBP. Keep it a module-wide
		// invariant across local and exact cross-instance calls rather than sharing
		// a mutable basedata slot between concurrent executions.
		gpPool = withoutReg(gpPool, RBP)
		f.reserved = f.reserved.add(RBP)
	}
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
	var globalScores []uint32
	var globalElig []bool
	if regABI {
		globalScores = hints.globalScore
		if hasCall {
			globalElig = hints.globalElig
		}
	}
	f.installModuleGlobals(modGlobals)
	// Deeper FP local pinning extends the float pin pool past XMM12-15 into the
	// WARP-sized pool (see pinnedFLocalRegs). Call-making functions use the existing
	// pinned-local spill/reload state machine to preserve these caller-saved regs.
	fpPinLimit := baseFPPins
	if f.opt(optExtendedFPPins) {
		fpPinLimit = len(pinnedFLocalRegs)
	}
	if !pinLocals {
		fpPinLimit = 0
	}
	intervalRegion := regABI && !hasCall && !hints.hasControlFlow && !hints.usesBulkMem && len(inlinedCallees) == 0 && f.prepareIntervalRegion(c.BodyBytes, hints)
	if intervalRegion {
		gpPool = nil // regional GP assignments supersede whole-function GP pins
	}
	f.assignPinnedLocals(hints.localScore, globalScores, globalElig, hints.sparseGlobals, gpPool, fpPinLimit, hasCall, pinLocals && f.opt(optV128Pins) && !hasCall)
	if regABI && !hasCall && f.nParams > 4 {
		for i := range f.locals {
			if r := f.locals[i].reg; r == R9 || r == R10 || r == R11 {
				f.stats.peep("entry-arg-local-pin") // hot local kept in a free incoming-arg register
			}
		}
	}
	if f.pinnedLocalMask.has(RBP) {
		f.regMerge = false // RBP now holds a pinned local/global
	}
	// STACK_REG (lazy pinned-local spill) for every call-making function,
	// including memory-touching ones: dirty-only stores before a call, lazy reload
	// on the next read (WARP's model). #68 disabled this for memory functions as a
	// workaround; the actual root cause was the opElse merge edge skipping
	// reconcileLocals (fixed in control.go, TestExecIfElseLocalMerge).
	f.usesCalls = hasCall && f.opt(optStackReg)
	// A call-free leaf extends the deepest checked stack by exactly one frame; the
	// fence's 256 KiB margin (runtime stackFenceMargin) absorbs that when the frame
	// is provably small. frameSize isn't known until after the body, so bound it:
	// spill slots never exceed the body's operand pushes (< one per body byte).
	f.skipFence = shouldSkipStackFence(hasCall, f.nLocalSlots, len(c.BodyBytes))
	// The return-in-register hint helps compute/call-heavy code (recursion,
	// dispatch) but adds register pressure in the deep, memory-bound call graphs
	// (json-as's TLSF/GC) where it measured as a small regression. Gate it on
	// !touchesMemory so it only fires where it's a win.
	f.singleRegResult = regABI && !touchesMemory && len(ft.Results) == 1 && !moduleEH
	if f.singleRegResult {
		rt := mtOf(ft.Results[0])
		f.resultFloat = rt.isFloat()
		f.resultF64 = rt == mtF64
	}
	f.lazyZero = hints.callsSelf && touchesMemory && len(c.BodyBytes) <= 192 && nLocals-len(ft.Params) <= 8
	f.storeForwardOK = f.opt(optStoreForward) && len(c.BodyBytes) <= 256 && nLocals <= 8

	// Auto-inlining: reserve each spliced callee's locals past f.nLocals (after all
	// nLocals-dependent setup above, so zeroDeclaredLocals/skipFence/lazyZero see the
	// caller's own locals only). Extends the frame's local arrays with unpinned
	// scratch; the splice at each call site binds/zeroes them.
	f.reserveInlineLocals(inlinedCallees, inlineTargets)
	if symbolicLocalSlotPackingPolicy(policy) && sc.localRefTailBound && !moduleEH && gcFrameRoots == nil &&
		len(inlinedCallees) == 0 && sc.localRefs.Reset(f.nLocals, maxAMD64LocalRefSites) {
		sc.asm.LocalRefs = &sc.localRefs
	}

	if regABI {
		// A host trampoline may enter this internal ABI directly when no adapter
		// state beyond RBX is required. Keep this deliberately leaf-only: a local
		// callee could itself expect the module memory-size cache to be live.
		sc.directPrepared = volatilePrepared
		internalOff, err := f.emitRegABI(c, hostAdapter, hints.hasFloatConst, hints.hasSIMD)
		if err != nil {
			return nil, nil, 0, err
		}
		f.emitV128ConstPool()
		internalOff, err = f.finalizeNativeCode(internalOff)
		if err != nil {
			return nil, nil, 0, err
		}
		f.finalizeStats(len(f.a.B))
		if gcFrameRoots != nil && gcFrameRoots.Candidate {
			gcFrameRoots.FrameBytes = uint32(f.frameSize())
			gcFrameRoots.AdapterReturnOffset = uint32(f.adapterReturnOff)
			if f.gcCallsiteIndex != len(gcFrameRoots.LiveCallLocalMasks) {
				gcFrameRoots.Exact = false
			}
		}
		return f.a.B, f.relocs, internalOff, nil
	}

	f.prologue()
	if !preloadScanGatesEnabled || hints.hasFloatConst {
		f.preloadFloatConsts(c.BodyBytes)
	}
	if !preloadScanGatesEnabled || hints.hasSIMD {
		f.preloadV128Consts(c.BodyBytes)
	}
	if err := f.runBody(c); err != nil {
		return nil, nil, 0, err
	}
	f.epilogue()
	f.emitNativeGCStubs()
	f.emitTrapStubs()
	f.finalizeBranchFolds()
	f.patchFrameSize()
	f.emitV128ConstPool() // trailing rip-relative pool for v128 constants (after all code)
	if _, err := f.finalizeNativeCode(0); err != nil {
		return nil, nil, 0, err
	}
	f.finalizeStats(len(f.a.B))
	if gcFrameRoots != nil && gcFrameRoots.Candidate {
		gcFrameRoots.FrameBytes = uint32(f.frameSize())
		gcFrameRoots.AdapterReturnOffset = uint32(f.adapterReturnOff)
		if f.gcCallsiteIndex != len(gcFrameRoots.LiveCallLocalMasks) {
			gcFrameRoots.Exact = false
		}
	}
	return f.a.B, f.relocs, 0, nil
}

func compactAccumulatorImmediatePolicy(policy CodegenPolicy) bool {
	return policy.CompactNative && policy.EnabledOption(optAccumulatorImmediate)
}

func symbolicLocalSlotPackingPolicy(policy CodegenPolicy) bool {
	return compactNativePolicy(policy) && policy.EnabledOption(optLocalSlotOrder)
}

// packLocalSlots uses exact emitted local-home references after the forward
// lowering. It only swaps equal-type homes: a low home with zero references and
// a referenced disp32 home. The moved-out local has no machine references to
// expand, so every changed site is a monotonic disp32 -> disp8/disp0 shrink.
func (f *fn) packLocalSlots(siteBudget int) int {
	r := f.a.LocalRefs
	if r == nil || r.Overflow || r.Pending || siteBudget <= 0 || int(r.Locals) != f.nLocals {
		return 0
	}

	swaps := 0
	for donor := 0; donor < f.nLocals && siteBudget != 0; donor++ {
		// A v128 home can emit multiple machine references from one localAddr
		// call (for example, two scalar stores during zeroing). Until the
		// recorder attaches identity to each actual memory operand, keep this
		// one-call/one-site proof limited to single-slot values.
		if f.localType[donor].stackSlots() != 1 || f.localRefCount(donor) != 0 || f.localOff(donor) > 127 {
			continue
		}
		best, bestCount := -1, uint32(0)
		for local := range f.nLocals {
			count := f.localRefCount(local)
			if int(count) > siteBudget || count <= bestCount || f.localType[local].stackSlots() != 1 ||
				f.localType[local] != f.localType[donor] || f.localOff(local) <= 127 {
				continue
			}
			recorded := uint32(0)
			for _, site := range r.Sites {
				if int(site.Local) == local {
					recorded++
				}
			}
			if recorded != count {
				continue
			}
			best, bestCount = local, count
		}
		if best < 0 {
			continue
		}
		donorSlot, bestSlot := uint64(f.localSlot[donor]), uint64(f.localSlot[best])
		f.localSlot[donor] = int(donorSlot&0xffffffff00000000 | bestSlot&0xffffffff)
		f.localSlot[best] = int(bestSlot&0xffffffff00000000 | donorSlot&0xffffffff)
		siteBudget -= int(bestCount)
		swaps++
	}
	if swaps != 0 {
		f.stats.peep("local-slot-order")
	}
	return swaps
}

func (f *fn) localRefCount(local int) uint32 { return uint32(uint64(f.localSlot[local]) >> 32) }

// finalizeStats fills the per-function size counters from final compiler state
// (no-op when collection is off). Per-event counters are incremented at their
// emission sites during the body.
func (f *fn) finalizeStats(codeLen int) {
	s := f.stats
	if s == nil {
		return
	}
	s.Rel32Sites = f.a.Rel32Count
	s.Rel32Recorded = len(f.a.Rel32Sites)
	s.Rel32Overflow = f.a.Rel32Overflow
	s.CodeBytes = codeLen
	s.NativeSize.TotalBytes = codeLen
	s.NativeSize.InternalFunctionBytes = codeLen - s.NativeSize.HostAdapterBytes - s.NativeSize.AdapterToInternalPaddingBytes
	s.GCCodeBytes.Total = codeLen
	s.FrameBytes = f.frameSize()
	s.MaxSpillSlots = f.maxSpill
}

// runBody opens the function control frame, lowers the body, and patches every
// return/br-to-function site to the (current) epilogue position.
func (f *fn) runBody(c *wasm.Func) error {
	resultTypes := typesOfVals(f.ft.Results)
	// Seed the control-frame stack from scratch's retained backing so its
	// (large-struct) array is reused across functions rather than regrown to peak
	// nesting depth for every one; sc.ctrl is written back below to keep the cap.
	f.ctrl = append(f.sc.ctrl[:0], ctrlFrame{kind: cfFunc, resultN: len(resultTypes), branchN: len(resultTypes), resultTypes: resultTypes})
	if err := f.body(c.BodyBytes); err != nil {
		return err
	}
	for _, s := range f.sc.retSites {
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
// misreads as an identifier ("near \"SELECT\": syntax error"). Excluding RDI/RSI
// removes the hazard; R9/R10/R11 pins
// (which the STACK_REG spill/reload does handle) stay. See TestSyncSQLiteQuery.
//
// R9/R10/R11 are still excluded in reg-ABI functions with >4 params (the internal
// entry's incoming args would collide with the prologue's arg→pinned moves). RBP
// costs the block-merge register (the caller drops regMerge). RAX/RCX/RDX/R8 always
// stay free for operand evaluation and the x86 fixed-role ops (div/shift/return);
// callHost's scratch also lives there.
func gpPinPool(pool []Reg, regABI bool, nParams int, callFree, entryArgPins bool) []Reg {
	pool = append(pool, pinnedLocalRegs...) // R12-R15
	if !regABI || nParams <= 4 {
		pool = append(pool, R9, R10, R11)
	} else if callFree && entryArgPins {
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

func preferPinReg(pool []Reg, preferred Reg) {
	for i, r := range pool {
		if r != preferred || i == 0 {
			continue
		}
		copy(pool[1:i+1], pool[:i])
		pool[0] = preferred
		return
	}
}

func (f *fn) assignPinnedLocals(scores, globalScores []uint32, globalElig []bool, sparseGlobals []shared.GlobalHint, gpPool []Reg, fpPinLimit int, hasCall, pinV128 bool) {
	if cap(f.locals) < f.nLocals {
		f.locals = make([]localDef, f.nLocals)
	} else {
		f.locals = f.locals[:f.nLocals]
	}
	for i := range f.locals {
		f.locals[i] = localDef{reg: regNone, typ: f.localType[i], state: lsReg}
	}
	// Module-pinned globals (installModuleGlobals) already occupy globalReg
	// entries; keep them and size for whichever view is larger.
	if len(f.globalReg) < f.m.GlobalCount() {
		gr := make([]Reg, f.m.GlobalCount())
		for i := range gr {
			gr[i] = regNone
		}
		copy(gr, f.globalReg)
		f.globalReg = gr
		gd := make([]bool, f.m.GlobalCount())
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
	for _, gh := range sparseGlobals {
		g := int(gh.Index)
		if gh.Score < loopMin || f.isModuleGlobal(g) || hasCall && !gh.Eligible {
			continue
		}
		gt, ok := f.m.GlobalTypeByIndex(gh.Index)
		if !ok || !gt.Mutable || !isIntValType(wasm.GlobalValueType(gt)) {
			continue
		}
		gp = append(gp, gpCand{global: true, idx: g, score: gh.Score})
	}
	f.tmpGpCand = gp
	slices.SortStableFunc(gp, func(a, b gpCand) int {
		if a.score != b.score {
			if a.score > b.score { // descending score
				return -1
			}
			return 1
		}
		if a.global != b.global {
			if a.global {
				return 1 // tie: prefer a local (value) over a global (pointer)
			}
			return -1
		}
		return a.idx - b.idx
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
			if gpPool[k] == RBP && k == 0 && compactLowPinEnabled && f.policy.CompactNative {
				f.stats.peep("compact-low-local-pin")
			}
		}
		f.pinnedLocalMask = f.pinnedLocalMask.add(gpPool[k])
	}
	// Float locals use the separate XMM pin pool. Call-free functions also pin hot
	// v128 locals here (same pool, full 128-bit): every XMM is caller-saved, so a
	// v128 pin is confined to the call-free class (pinV128).
	fc := f.tmpInts[:0]
	for i := 0; i < f.nLocals; i++ {
		if f.localType[i].isFloat() || (pinV128 && f.localType[i] == mtV128) {
			fc = append(fc, i)
		}
	}
	slices.SortFunc(fc, func(a, b int) int {
		if scores[a] != scores[b] {
			if scores[a] > scores[b] {
				return -1
			}
			return 1
		}
		return a - b
	})
	f.tmpInts = fc
	if fpPinLimit > len(pinnedFLocalRegs) {
		fpPinLimit = len(pinnedFLocalRegs)
	}
	for k, i := range fc {
		if k >= fpPinLimit {
			break
		}
		if pinnedFLocalRegs[k] < 12 {
			f.stats.peep("deep-fp-local-pin") // extended XMM8-10/XMM4-7 pin
		}
		f.locals[i].reg = pinnedFLocalRegs[k]
		f.locals[i].isFloat = true
		f.fpinnedLocalMask = f.fpinnedLocalMask.add(pinnedFLocalRegs[k])
		f.stats.addPinnedLocal()
	}
	// Record the pinned-local index list so the per-edge/per-call state loops
	// iterate only pinned locals rather than scanning every local (the pin set is
	// fixed for the rest of the function). Backed by scratch so it is not
	// reallocated per function.
	f.pinnedLocals = f.sc.pinnedLocals[:0]
	for x := range f.locals {
		if f.locals[x].reg != regNone {
			f.pinnedLocals = append(f.pinnedLocals, x)
		}
	}
	f.sc.pinnedLocals = f.pinnedLocals
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
		a.Load64(f.memSizeReg, RBX, -bdCurBytes)
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
			if pr, _, ok := f.pinReg(i); ok {
				a.VMovdquLoadDisp(pr, RDI, paramOff) // pinned v128 param → its XMM register
			} else {
				a.VMovdquLoadDisp(0, RDI, paramOff)
				a.VMovdquStoreDisp(RSP, f.localAddr(i), 0)
			}
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
				f.storeFrameInt(f.localAddr(i), RAX, f.localType[i])
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
			if i < 64 && f.entryInitialized&(uint64(1)<<uint(i)) != 0 {
				continue
			}
			if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
				a.XorSelf32(pr)
			} else if ok && isFloat {
				a.SseRR(0x66, 0x57, pr, pr, false) // xorpd pr,pr -> 0.0
			} else if f.localType[i] == mtV128 {
				a.Store64(RSP, f.localAddr(i), RAX)
				a.Store64(RSP, f.localAddr(i)+8, RAX)
			} else {
				f.storeFrameInt(f.localAddr(i), RAX, f.localType[i])
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
	if !f.opt(optStackFence) || f.skipFence {
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
func (f *fn) emitRegABI(c *wasm.Func, hostAdapter, hasFloatConst, hasSIMD bool) (int, error) {
	a := f.a
	np, rN := f.nParams, len(f.ft.Results)

	// Host→internal adapter (offset 0): in RDI=serArgs, RSI=linMem, RDX=trap,
	// RCX=results; loads args into registers, calls the internal entry, stores the
	// single register result.
	var adapterCall int
	gp, fp := 0, 0
	if hostAdapter {
		a.MovReg64(RBX, RSI) // linMem → RBX: the module-wide invariant the internal entry inherits
		if f.memSizeReg != regNone {
			// Offset-0 entry (from Go, or an indirect call): establish the module-wide
			// memBytes cache before the internal entry runs (which relies on it).
			a.Load64(f.memSizeReg, RBX, -bdCurBytes)
		}
		f.deriveModuleGlobals() // offset-0 entry: cells → module-pinned registers
		a.Push(RCX)             // results ptr (also keeps RSP 16-aligned at the internal call)
		gp, fp = 0, 0
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
		adapterCall = a.CallRel32()
		f.adapterReturnOff = adapterCall + 4
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
		f.adapterEndOff = a.Len()
		if f.stats != nil {
			f.stats.NativeSize.HostAdapterBytes = a.Len()
		}
	}

	// Internal entry (frameless): RBX (linMem) is inherited from the caller —
	// every wasm function keeps it pinned, and the adapter establishes it at the
	// Go boundary — and the trap cell pointer lives in basedata, so the entry
	// carries no environment setup at all (WARP's model). Args in GP/XMM regs.
	if hostAdapter {
		beforeAlign := a.Len()
		if internalEntryShouldAlign(a.Len(), len(c.BodyBytes), f.policy) {
			a.Align16()
		}
		if f.stats != nil {
			f.stats.NativeSize.AdapterToInternalPaddingBytes = a.Len() - beforeAlign
		}
	}
	internalOff := a.Len()
	if f.functionCounters {
		a.Load64(RSI, RBX, -int32(abi.ProfileCountersPtrOffset))
		a.LockInc64(RSI, int32(f.globalIdx*8))
	}
	f.subRspAt = a.Len() + 3
	a.SubRsp(0)
	f.emitStackFenceCheck(RBX, RSI)
	f.emitInterruptCheck(RSI) // RSI is not an int-arg reg: free before args are homed
	gp, fp = 0, 0
	if len(f.intervalReg) != 0 {
		// Home all incoming integer parameters so the regional cache can claim and
		// release any parameter lazily without preserving argument-register cycles.
		for i := 0; i < np; i++ {
			mt := f.localType[i]
			if mt.isFloat() {
				fp++
				continue
			}
			f.storeFrameInt(f.localAddr(i), intArgRegs[gp], mt)
			gp++
		}
		gp, fp = 0, 0
	}
	for i := 0; i < np; i++ {
		mt := f.localType[i]
		if mt.isFloat() {
			src := fpArgRegs[fp]
			if pr, isFloat, ok := f.pinReg(i); ok && isFloat {
				a.FMov(pr, src, mt == mtF64)
			} else {
				a.FStoreDisp(RSP, f.localAddr(i), src, mt == mtF64)
			}
			fp++
		} else if len(f.intervalReg) != 0 {
			// Already homed; the regional cache loads it on first use.
		} else if pr, isFloat, ok := f.pinReg(i); ok && !isFloat {
			f.moveInt(pr, intArgRegs[gp], mt)
		} else {
			f.storeFrameInt(f.localAddr(i), intArgRegs[gp], mt)
		}
		if !mt.isFloat() {
			gp++
		}
	}
	f.zeroDeclaredLocals()
	if !preloadScanGatesEnabled || hasFloatConst {
		f.preloadFloatConsts(c.BodyBytes)
	}
	if !preloadScanGatesEnabled || hasSIMD {
		f.preloadV128Consts(c.BodyBytes)
	}
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
	f.emitNativeGCStubs()
	f.emitTrapStubs()
	f.finalizeBranchFolds()

	f.elideRegisterOnlyFrame() // register-homed call-free leaf → frameSize 0
	f.patchFrameSize()
	if hostAdapter {
		a.PatchRel32(adapterCall, internalOff)
		if f.stats != nil {
			f.stats.NativeSize.HostAdapterShapeHash = shared.AdapterShapeHash(f.a.B[:f.stats.NativeSize.HostAdapterBytes], adapterCall, 4)
			f.stats.NativeSize.HostAdapterTailBytes = f.stats.NativeSize.HostAdapterBytes - f.adapterReturnOff
			f.stats.NativeSize.HostAdapterTailShapeHash = shared.AdapterShapeHash(f.a.B[f.adapterReturnOff:f.stats.NativeSize.HostAdapterBytes], -1, 0)
		}
	}
	return internalOff, nil
}

func (f *fn) patchFrameSize() {
	size := uint32(f.frameSize())
	if f.stats != nil {
		sites := len(f.sc.tailFrameSites) + 2
		f.stats.NativeSize.FrameAdjustmentBytes += 7 * sites
		deadPerSite := 0
		if size == 0 {
			deadPerSite = 7
		} else if size <= 127 {
			deadPerSite = 3
		}
		f.stats.NativeSize.DeadFrameReservationBytes += deadPerSite * sites
	}
	f.a.PatchU32(f.subRspAt, size)
	f.a.PatchU32(f.addRspAt, size)
	for _, site := range f.sc.tailFrameSites {
		f.a.PatchU32(site, size)
	}
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
