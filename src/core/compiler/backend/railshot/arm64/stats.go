//go:build arm64

package arm64

// CodegenStats is the railshot "explain" dashboard: per-function counters that
// make every later optimization prove itself. Collection
// is opt-in â a *CodegenStats is threaded through the fn only when the caller asks
// (CompileOptions.Stats) or WAGO_EXPLAIN=1 is set. When off, the field is nil and
// every counter method is a no-op (nil-receiver methods), so the hot compile path
// pays nothing.
//
// The counters are the sinks the plan's phases target: MemRefsForcedByStore is
// what P2's alias-aware loads shrink, BoundsChecks is what P6's bounds facts
// elide, Calls[...] by kind is what P5's call work moves between buckets, and the
// Peephole map records which instruction-selection rewrites actually fired.

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// Explain/debug knobs, parsed once. Kept here next to the stats they drive.
var (
	// explainEnabled prints a per-module CodegenStats dump to stderr after every
	// compile. "size" retains the full report and highlights its native byte
	// ledger; "1" remains the backward-compatible spelling.
	explainMode    = os.Getenv("WAGO_EXPLAIN")
	explainEnabled = explainMode == "1" || explainMode == "size"
	// debugModGlobals prints the module-pinned-global choices (the #90-era temp
	// print, now first-class).
	debugModGlobals = os.Getenv("WAGO_DEBUG_MODGLOBALS") == "1"
	// pinGlobalK overrides the adaptive module-global pin count K: -1 = auto (the
	// pickModuleGlobals heuristic), 0..len(moduleGlobalRegs) = force that many.
	pinGlobalK = parsePinGlobalK(os.Getenv("WAGO_PIN_GLOBAL_K"))
	// boundsFactsEnabled gates P6.1 straight-line bounds-check elision (explicit
	// mode). WAGO_NO_BOUNDS_FACTS=1 forces every check â the A/B oracle + kill switch.
	boundsFactsEnabled = os.Getenv("WAGO_NO_BOUNDS_FACTS") != "1"
	// stFlagsEnabled gates the stFlags tee-forward window (R1): a compare stored by
	// `local.tee $c` and consumed by the next if/br_if/select fuses into the branch,
	// storing $c with a flag-neutral Cset after the CMP. WAGO_NO_STFLAGS=1 is the
	// A/B oracle + kill switch for this flag-desync-sensitive path.
	stFlagsEnabled = os.Getenv("WAGO_NO_STFLAGS") != "1"
	// swarMaskTestEnabled gates direct packed-word mask-test fusion.
	// WAGO_NO_SWAR_MASK_TEST=1 is the A/B oracle.
	swarMaskTestEnabled = os.Getenv("WAGO_NO_SWAR_MASK_TEST") != "1"
	// simdSuperoptEnabled gates exact bounded selection of multi-op Wasm SIMD
	// sequences. WAGO_NO_SIMD_SUPEROPT=1 is the A/B oracle.
	simdSuperoptEnabled = os.Getenv("WAGO_NO_SIMD_SUPEROPT") != "1"
	// v128DirectResultEnabled lets NEON's non-destructive destination forms read
	// pinned locals directly instead of copying a source into an accumulator.
	v128DirectResultEnabled = os.Getenv("WAGO_ARM64_NO_V128_DIRECT_RESULTS") != "1"
	// intervalRegionPinsEnabled reuses GP registers across integer-local
	// lifetimes in bounded call-free straight-line functions. The cache is
	// pressure-spillable and releases a register at the local's final get.
	intervalRegionPinsEnabled = os.Getenv("WAGO_ARM64_INTERVAL_REGIONS") != "0"
	// entryInitElisionEnabled skips zero-initialization for declared locals whose
	// first straight-line access is a set/tee. The kill switch is the A/B oracle.
	entryInitElisionEnabled = os.Getenv("WAGO_ARM64_NO_ENTRY_INIT_ELISION") != "1"

	// zeroBranchEnabled selects CBZ/CBNZ for flag-dead i32 control tests instead
	// of materializing NZCV with CMP before B.cond. The kill switch is the A/B
	// oracle for the one-word lowering.
	zeroBranchEnabled = os.Getenv("WAGO_ARM64_NO_ZERO_BRANCH") != "1"
	// logicalMoveImmediateEnabled lets constant materialization use the one-word
	// ORR-from-zero-register alias when the value is a logical immediate.
	logicalMoveImmediateEnabled = os.Getenv("WAGO_ARM64_NO_LOGICAL_MOVE_IMMEDIATE") != "1"
	// compactMoveImmediate32Enabled selects true W-register MOVZ/MOVN/MOVK
	// sequences instead of constructing every i32 as a zero-extended i64.
	compactMoveImmediate32Enabled = os.Getenv("WAGO_ARM64_NO_COMPACT_MOVE_IMMEDIATE32") != "1"
	// shiftedAddSubImmediateEnabled selects the legal imm12 LSL #12 form for
	// compact add, sub, compare, and address displacement operations.
	shiftedAddSubImmediateEnabled = os.Getenv("WAGO_ARM64_NO_SHIFTED_ADD_SUB_IMMEDIATE") != "1"
	// sharedTrapBodyEnabled lets compact trap groups share the complete
	// terminal trap record/writeback/unwind body within one function.
	sharedTrapBodyEnabled = os.Getenv("WAGO_ARM64_NO_SHARED_TRAP_BODY") != "1"
	// loadPairEnabled combines exact adjacent full-width scalar loads into LDP.
	// WAGO_ARM64_NO_LOAD_PAIR=1 is the A/B oracle and rollback switch.
	loadPairEnabled = os.Getenv("WAGO_ARM64_NO_LOAD_PAIR") != "1"

	// mulAddFuseEnabled gates MADD/MSUB fusion of add(c, a*b)/sub(c, a*b) into a
	// single multiply-add/-subtract. WAGO_NO_MULADD=1 is the A/B oracle.
	mulAddFuseEnabled = os.Getenv("WAGO_NO_MULADD") != "1"
)

const (
	callKindInline         = shared.CallInline
	callKindHost           = shared.CallHost
	callKindHostSync       = shared.CallHostSync
	callKindCrossInstance  = shared.CallCrossInstance
	callKindImportDispatch = shared.CallImportDispatch
	callKindRegisterABI    = shared.CallRegisterABI
	callKindMixed          = shared.CallMixed
	callKindWrapper        = shared.CallWrapper
	callKindIndirect       = shared.CallIndirect
)

func parsePinGlobalK(s string) int {
	switch s {
	case "0":
		return 0
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	default: // "", "auto", or anything unrecognized
		return -1
	}
}

// CodegenStats holds one function's codegen counters. All fields are zero when a
// phenomenon did not occur; maps are nil until first use.
type CodegenStats struct {
	FuncIdx int    // local function index (0-based over m.Code)
	Name    string // name-section / export name, or "" if anonymous

	// Size.
	CodeBytes       int                      // emitted machine-code length
	FrameBytes      int                      // stack frame size (sub sp, N)
	MaxSpillSlots   int                      // high-water operand spill slots
	MaxPendingNodes int                      // high-water deferred nodes in the bounded operand packet
	GCCodeBytes     shared.GCNativeCodeBytes // diagnostic WasmGC byte attribution
	NativeSize      shared.NativeFunctionSizeReport
	// FinalizerFallback is the fail-closed reason a compact function kept
	// its maximal-safe encoding instead of applying an available compaction plan.
	FinalizerFallback string `json:"finalizer_fallback,omitempty"`
	// InlineSiteBytes is the exact pre-finalization byte span emitted directly by
	// inline sites. Caller frame growth and shared cold tails are outside it.
	InlineSiteBytes int

	// Register allocator / condense engine traffic.
	Flushes              int // full operand-stack flushes (control boundaries + calls)
	FlushBelows          int // partial flushes below a fused node
	FlushRoots           int // logical roots examined by full flushes
	FlushDeferredRoots   int // deferred roots forced to condense by full flushes
	FlushBelowRoots      int // logical roots examined by partial flushes
	FlushBelowDeferred   int // deferred roots forced to condense by partial flushes
	CallFlushes          int // full/partial operand flushes emitted for a register-ABI call
	LocalSetDeferred     int // local.set/tee whose source was a deferred Valent block
	Condenses            int // deferred-tree condensations to a register
	Spills               int // registerâslot evictions under pressure
	Reloads              int // slotâregister reloads of a spilled value
	MemRefsForcedByStore int // deferred loads forced out by a store (P2.1 target)

	// Bounds / traps.
	BoundsChecks          int // inline memory-OOB checks emitted (P6 elides these)
	BoundsChecksElidable  int // subset of BoundsChecks a straight-line certificate covers (P6.1 sizing; count-only)
	BoundsChecksInLoop    int // subset emitted inside a loop on a keyable base; count-only
	BoundsChecksHoistable int // subset on a loop-INVARIANT local base (not set in the loop) â the P6.2 hoistable target; count-only
	TrapStubs             int // shared cold trap stubs emitted (one per trap code used)
	TrapGroups            int // distinct source-function groups across trap stubs

	// Calls, by lowering kind: regabi / mixed / wrapper / host / indirect /
	// crossinstance / importdispatch.
	Calls map[string]int

	// Pins.
	PinnedLocals       int // integer/float locals given a dedicated register
	PinnedGlobalsValue int // hot mutable-int globals value-pinned in this function

	CompileNanos     uint64
	FunctionAttempts uint64

	// Peephole/instruction-selection rewrites that fired, by stable name.
	Peephole map[string]int
}

func (ms *ModuleStats) finalizeCompileResourceStats() {
	if ms == nil {
		return
	}
	c := &ms.Compile
	c.StageNanos[shared.CompileStageFunctions] = 0
	c.FunctionAttempts = 0
	for _, s := range ms.Funcs {
		if s == nil {
			continue
		}
		c.StageNanos[shared.CompileStageFunctions] += s.CompileNanos
		c.FunctionAttempts += s.FunctionAttempts
	}
}

func (ms *ModuleStats) setNodeScratchStats(sc *scratch) {
	if ms == nil {
		return
	}
	ms.Compile.NodeScratchReserved = 0
	ms.Compile.NodeScratchPeak = 0
	ms.Compile.NodeScratchRetained = 0
	ms.Compile.NodeScratchDiscarded = 0
	ms.Compile.ControlScratchReserved = 0
	ms.Compile.ControlScratchPeak = 0
	ms.Compile.ControlScratchRetained = 0
	ms.Compile.ControlScratchDiscarded = 0
	ms.addNodeScratchStats(sc)
}

func (ms *ModuleStats) addNodeScratchStats(sc *scratch) {
	if ms == nil || sc == nil {
		return
	}
	ms.Compile.AddWorkerScratch(workerScratchStats(sc))
}

func workerScratchStats(sc *scratch) shared.WorkerScratchStats {
	if sc == nil {
		return shared.WorkerScratchStats{}
	}
	_, retained := sc.stack.nodeMemory()
	frameBytes := uint64(unsafe.Sizeof(ctrlFrame{}))
	mergeBytes := uint64(unsafe.Sizeof(ctrlFrameMerge{}))
	rootBytes := uint64(unsafe.Sizeof(ctrlFrameRoots{}))
	return shared.WorkerScratchStats{
		NodeReserved: sc.nodeScratchReserved, NodePeak: sc.nodeScratchPeak,
		NodeRetained: retained, NodeDiscarded: sc.nodeScratchDiscarded,
		ControlReserved:  uint64(sc.controlScratchReserved) * frameBytes,
		ControlPeak:      uint64(sc.controlScratchPeak)*frameBytes + uint64(sc.controlMergePeak)*mergeBytes + uint64(sc.controlRootsPeak)*rootBytes,
		ControlRetained:  uint64(cap(sc.ctrl))*frameBytes + uint64(cap(sc.ctrlMerges))*mergeBytes + uint64(cap(sc.ctrlRoots))*rootBytes,
		ControlDiscarded: uint64(sc.controlScratchDiscarded)*frameBytes + uint64(sc.controlMergeDiscarded)*mergeBytes + uint64(sc.controlRootsDiscarded)*rootBytes,
	}
}

func (s *CodegenStats) setFinalizerFallback(reason string) {
	if s != nil {
		s.FinalizerFallback = reason
	}
}

// --- nil-safe counter methods (no-op when collection is off) ---

func (s *CodegenStats) addFlush() {
	if s != nil {
		s.Flushes++
	}
}
func (s *CodegenStats) addFlushBelow() {
	if s != nil {
		s.FlushBelows++
	}
}
func (s *CodegenStats) addFlushRoot(deferred bool) {
	if s != nil {
		s.FlushRoots++
		if deferred {
			s.FlushDeferredRoots++
		}
	}
}
func (s *CodegenStats) addFlushBelowRoot(deferred bool) {
	if s != nil {
		s.FlushBelowRoots++
		if deferred {
			s.FlushBelowDeferred++
		}
	}
}
func (s *CodegenStats) addCallFlush() {
	if s != nil {
		s.CallFlushes++
	}
}
func (s *CodegenStats) addLocalSetDeferred() {
	if s != nil {
		s.LocalSetDeferred++
	}
}
func (s *CodegenStats) addCondense() {
	if s != nil {
		s.Condenses++
	}
}
func (s *CodegenStats) addSpill() {
	if s != nil {
		s.Spills++
	}
}
func (s *CodegenStats) addReload() {
	if s != nil {
		s.Reloads++
	}
}
func (s *CodegenStats) addForcedLoad() {
	if s != nil {
		s.MemRefsForcedByStore++
	}
}
func (s *CodegenStats) addTrapStub() {
	if s != nil {
		s.TrapStubs++
	}
}
func (s *CodegenStats) addTrapGroup() {
	if s != nil {
		s.TrapGroups++
	}
}
func (s *CodegenStats) addBoundsCheck() {
	if s != nil {
		s.BoundsChecks++
	}
}
func (s *CodegenStats) addBoundsElidable() {
	if s != nil {
		s.BoundsChecksElidable++
	}
}
func (s *CodegenStats) addBoundsInLoop() {
	if s != nil {
		s.BoundsChecksInLoop++
	}
}
func (s *CodegenStats) addBoundsHoistable() {
	if s != nil {
		s.BoundsChecksHoistable++
	}
}
func (s *CodegenStats) addPinnedLocal() {
	if s != nil {
		s.PinnedLocals++
	}
}
func (s *CodegenStats) addPinnedGlobalValue() {
	if s != nil {
		s.PinnedGlobalsValue++
	}
}
func (s *CodegenStats) addGCAllocationBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.Allocation += n
	}
}
func (s *CodegenStats) addGCHandleResolutionBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.HandleResolution += n
	}
}
func (s *CodegenStats) addGCTypeCastBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.TypeCast += n
	}
}
func (s *CodegenStats) addGCNullCheckBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.NullCheck += n
	}
}
func (s *CodegenStats) addGCBoundsCheckBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.BoundsCheck += n
	}
}
func (s *CodegenStats) addGCBarrierBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.Barrier += n
	}
}
func (s *CodegenStats) addGCHelperCallBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.HelperCall += n
	}
}
func (s *CodegenStats) addGCSharedStubBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.SharedStub += n
	}
}
func (s *CodegenStats) addGCSpillReloadBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.SpillReload += n
	}
}
func (s *CodegenStats) addGCTrapStubBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.TrapStub += n
	}
}
func (s *CodegenStats) addGCRootMapBytes(n int) {
	if s != nil && n > 0 {
		s.GCCodeBytes.RootMap += n
	}
}

// call records one call lowering of the given kind.
func (s *CodegenStats) call(kind string) {
	if s == nil {
		return
	}
	if s.Calls == nil {
		s.Calls = make(map[string]int)
	}
	s.Calls[kind]++
}

func (s *CodegenStats) addInlineSiteBytes(n int) {
	if s != nil {
		s.InlineSiteBytes += n
	}
}

// peep records one peephole/instruction-selection rewrite by stable name.
func (s *CodegenStats) peep(name string) {
	if s == nil {
		return
	}
	if s.Peephole == nil {
		s.Peephole = make(map[string]int)
	}
	s.Peephole[name]++
}

func (s *CodegenStats) peepN(name string, n int) {
	for ; n > 0; n-- {
		s.peep(name)
	}
}

// ModuleGlobalPinInfo describes one module-wide global-to-register reservation.
type ModuleGlobalPinInfo = shared.ModuleGlobalPinInfo

// ModuleStats aggregates one module's per-function stats plus the module-wide
// decisions. The zero value is ready to collect into.
type ModuleStats struct {
	Funcs            []*CodegenStats
	ModuleGlobalPins []ModuleGlobalPinInfo
	Inline           *InlineReport // inline-candidate detection (nil if not analyzed)
	NativeSize       shared.NativeSizeReport
	Compile          shared.CompileResourceStats
}

// NativeFunctionSizeReport and NativeSizeReport are shared by both Railshot
// targets. The aliases keep this architecture package's structured stats API
// self-contained for callers such as bench/cmd/explain.
type NativeFunctionSizeReport = shared.NativeFunctionSizeReport
type NativeSizeReport = shared.NativeSizeReport

// String renders the explain dump: a module summary line, the module-pinned
// globals, then one block per function.
func (ms *ModuleStats) String() string {
	if ms == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "=== codegen explain: %d function(s) ===\n", len(ms.Funcs))
	fmt.Fprintf(&b, "compile: hints=%dns functions=%dns finalize=%dns hint-headers=%dB hint-sidecars=%dB attempts=%d\n",
		ms.Compile.StageNanos[shared.CompileStageHints], ms.Compile.StageNanos[shared.CompileStageFunctions],
		ms.Compile.StageNanos[shared.CompileStageFinalize], ms.Compile.HintHeaderBytes,
		ms.Compile.HintSidecarBytes, ms.Compile.FunctionAttempts)
	fmt.Fprintf(&b, "compile-node-scratch: reserved=%dB peak-envelope=%dB retained=%dB discarded=%dB\n",
		ms.Compile.NodeScratchReserved, ms.Compile.NodeScratchPeak,
		ms.Compile.NodeScratchRetained, ms.Compile.NodeScratchDiscarded)
	fmt.Fprintf(&b, "compile-control-scratch: reserved=%dB peak-envelope=%dB retained=%dB discarded=%dB\n",
		ms.Compile.ControlScratchReserved, ms.Compile.ControlScratchPeak,
		ms.Compile.ControlScratchRetained, ms.Compile.ControlScratchDiscarded)
	fmt.Fprintf(&b, "native: total=%d functions=%d function-align=%d module-other=%d dead-reserved=%d\n",
		ms.NativeSize.TotalBytes, ms.NativeSize.FunctionBytes, ms.NativeSize.FunctionAlignmentBytes,
		ms.NativeSize.ModuleOtherBytes, ms.NativeSize.DeadReservationBytes())
	arenaSlack := 0
	if ms.NativeSize.CompilerCodeArenaBytes != 0 {
		arenaSlack = ms.NativeSize.CompilerCodeArenaBytes - ms.NativeSize.TotalBytes
	}
	fmt.Fprintf(&b, "native-mapping: required=%d pages=%d compiler-arena=%d arena-slack=%d\n",
		ms.NativeSize.ExecutableMappingBytes, ms.NativeSize.ExecutableMappingPages,
		ms.NativeSize.CompilerCodeArenaBytes, arenaSlack)
	fmt.Fprintf(&b, "native-regions: adapters=%d internal-pad=%d internal=%d\n",
		ms.NativeSize.HostAdapterBytes, ms.NativeSize.AdapterToInternalPaddingBytes,
		ms.NativeSize.InternalFunctionBytes)
	fmt.Fprintf(&b, "native-adapters: count=%d shapes=%d unique=%d duplicates=%d\n",
		ms.NativeSize.HostAdapterCount, ms.NativeSize.HostAdapterShapeCount, ms.NativeSize.HostAdapterUniqueBytes,
		ms.NativeSize.HostAdapterDuplicateBytes)
	fmt.Fprintf(&b, "native-adapter-tails: shapes=%d unique=%d duplicates=%d\n",
		ms.NativeSize.HostAdapterTailShapeCount, ms.NativeSize.HostAdapterTailUniqueBytes,
		ms.NativeSize.HostAdapterTailDuplicateBytes)
	fmt.Fprintf(&b, "native-reservations: frame-physical=%d frame-dead=%d branch-holes=%d store-load-nops=%d\n",
		ms.NativeSize.FrameAdjustmentBytes, ms.NativeSize.DeadFrameReservationBytes,
		ms.NativeSize.BranchFoldHoleBytes, ms.NativeSize.StoreLoadNopBytes)
	fmt.Fprintf(&b, "native-data: literals=%d module-unique-literals=%d cross-function-duplicates=%d\n",
		ms.NativeSize.LiteralPoolBytes, ms.NativeSize.LiteralPoolUniqueBytes,
		ms.NativeSize.LiteralPoolDuplicateBytes)
	type fallbackTotal struct{ count, bytes int }
	fallbacks := make(map[string]fallbackTotal)
	for _, s := range ms.Funcs {
		if s == nil || s.FinalizerFallback == "" {
			continue
		}
		total := fallbacks[s.FinalizerFallback]
		total.count++
		total.bytes += s.NativeSize.DeadReservationBytes()
		fallbacks[s.FinalizerFallback] = total
	}
	if len(fallbacks) != 0 {
		keys := make([]string, 0, len(fallbacks))
		for reason := range fallbacks {
			keys = append(keys, reason)
		}
		sort.Strings(keys)
		b.WriteString("native-finalizer-fallbacks:")
		for _, reason := range keys {
			total := fallbacks[reason]
			fmt.Fprintf(&b, " %s=%d/%dB", reason, total.count, total.bytes)
		}
		b.WriteByte('\n')
	}
	if len(ms.ModuleGlobalPins) == 0 {
		fmt.Fprintf(&b, "module-pinned globals: none (K=0)\n")
	} else {
		fmt.Fprintf(&b, "module-pinned globals (K=%d):", len(ms.ModuleGlobalPins))
		for _, p := range ms.ModuleGlobalPins {
			fmt.Fprintf(&b, " g%dâ%s", p.Global, p.Reg)
		}
		b.WriteByte('\n')
	}
	if ms.Inline != nil {
		b.WriteString(ms.Inline.String())
	}
	for _, s := range ms.Funcs {
		if s == nil {
			continue
		}
		b.WriteString(s.report())
	}
	return b.String()
}

// report renders one function's counters as an indented block.
func (s *CodegenStats) report() string {
	if s == nil {
		return ""
	}
	name := s.Name
	if name == "" {
		name = "(anon)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "fn#%d %q: code=%dB frame=%dB spill_hi=%d\n",
		s.FuncIdx, name, s.CodeBytes, s.FrameBytes, s.MaxSpillSlots)
	fmt.Fprintf(&b, "    native: adapter=%d internal-pad=%d internal=%d frame-adjust=%d dead-reserved=%d literals=%d\n",
		s.NativeSize.HostAdapterBytes, s.NativeSize.AdapterToInternalPaddingBytes,
		s.NativeSize.InternalFunctionBytes, s.NativeSize.FrameAdjustmentBytes,
		s.NativeSize.DeadReservationBytes(), s.NativeSize.LiteralPoolBytes)
	if s.FinalizerFallback != "" {
		fmt.Fprintf(&b, "    finalizer-fallback: %s\n", s.FinalizerFallback)
	}
	fmt.Fprintf(&b, "    alloc: flushes=%d roots=%d deferred=%d flushBelow=%d roots=%d deferred=%d callFlush=%d localSetDeferred=%d condenses=%d spills=%d reloads=%d forcedLoads=%d\n",
		s.Flushes, s.FlushRoots, s.FlushDeferredRoots, s.FlushBelows, s.FlushBelowRoots, s.FlushBelowDeferred, s.CallFlushes, s.LocalSetDeferred, s.Condenses, s.Spills, s.Reloads, s.MemRefsForcedByStore)
	fmt.Fprintf(&b, "    mem:   bounds=%d elidable=%d inloop=%d hoistable=%d trapStubs=%d trapGroups=%d   pins: local=%d gval=%d\n",
		s.BoundsChecks, s.BoundsChecksElidable, s.BoundsChecksInLoop, s.BoundsChecksHoistable, s.TrapStubs, s.TrapGroups, s.PinnedLocals, s.PinnedGlobalsValue)
	if s.InlineSiteBytes != 0 {
		fmt.Fprintf(&b, "    inline-site-bytes: %d\n", s.InlineSiteBytes)
	}
	gcBytes := s.GCCodeBytes
	if gcBytes.Allocation|gcBytes.HandleResolution|gcBytes.TypeCast|gcBytes.NullCheck|gcBytes.BoundsCheck|gcBytes.Barrier|gcBytes.SpillReload|gcBytes.HelperCall|gcBytes.SharedStub|gcBytes.TrapStub|gcBytes.RootMap != 0 {
		fmt.Fprintf(&b, "    gcbytes: total=%d alloc=%d resolve=%d cast=%d null=%d bounds=%d barrier=%d spill=%d helper=%d shared=%d trap=%d rootmap=%d\n",
			gcBytes.Total, gcBytes.Allocation, gcBytes.HandleResolution, gcBytes.TypeCast, gcBytes.NullCheck, gcBytes.BoundsCheck, gcBytes.Barrier, gcBytes.SpillReload, gcBytes.HelperCall, gcBytes.SharedStub, gcBytes.TrapStub, gcBytes.RootMap)
	}
	if len(s.Calls) > 0 {
		fmt.Fprintf(&b, "    calls: %s\n", fmtCountMap(s.Calls))
	}
	if len(s.Peephole) > 0 {
		fmt.Fprintf(&b, "    peep:  %s\n", fmtCountMap(s.Peephole))
	}
	return b.String()
}

// fmtCountMap renders a map[string]int as "k1=v1 k2=v2" in stable key order.
func fmtCountMap(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, m[k])
	}
	return strings.Join(parts, " ")
}

// funcDisplayName resolves a friendly name for local function index localIdx: the
// name-section function name if present, else a matching function export, else "".
// regName maps a Reg to its lowercase AArch64 (arm64) mnemonic for the explain
// dump. Register 31 is the stack pointer in the load/store base position this
// backend uses it in, so it prints as "sp".
func regName(r Reg) string {
	switch r {
	case a64.X0:
		return "x0"
	case a64.X1:
		return "x1"
	case a64.X2:
		return "x2"
	case a64.X3:
		return "x3"
	case a64.X4:
		return "x4"
	case a64.X5:
		return "x5"
	case a64.X6:
		return "x6"
	case a64.X7:
		return "x7"
	case a64.X8:
		return "x8"
	case a64.X9:
		return "x9"
	case a64.X10:
		return "x10"
	case a64.X11:
		return "x11"
	case a64.X12:
		return "x12"
	case a64.X13:
		return "x13"
	case a64.X14:
		return "x14"
	case a64.X15:
		return "x15"
	case a64.X16:
		return "x16"
	case a64.X17:
		return "x17"
	case a64.X18:
		return "x18"
	case a64.X19:
		return "x19"
	case a64.X20:
		return "x20"
	case a64.X21:
		return "x21"
	case a64.X22:
		return "x22"
	case a64.X23:
		return "x23"
	case a64.X24:
		return "x24"
	case a64.X25:
		return "x25"
	case a64.X26:
		return "x26"
	case a64.X27:
		return "x27"
	case a64.X28:
		return "x28"
	case a64.X29: // frame pointer (FP)
		return "x29"
	case a64.X30: // link register (LR)
		return "x30"
	case a64.XZR: // reg 31: stack pointer / zero register
		return "sp"
	default:
		return fmt.Sprintf("x?%d", r)
	}
}

func funcDisplayName(m *wasm.Module, localIdx, importedFuncs int) string {
	global := uint32(importedFuncs + localIdx)
	if n, ok := m.NameSec.FuncName(global); ok && n != "" {
		return n
	}
	for i := range m.Exports {
		ex := &m.Exports[i]
		if ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == global {
			return ex.Name
		}
	}
	return ""
}
