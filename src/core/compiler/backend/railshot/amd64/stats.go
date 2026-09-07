//go:build amd64

package amd64

// CodegenStats is the railshot "explain" dashboard: per-function counters that
// make every later optimization prove itself. Collection
// is opt-in — a *CodegenStats is threaded through the fn only when the caller asks
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
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

// Explain/debug knobs, parsed once. Kept here next to the stats they drive.
var (
	// explainEnabled prints a per-module CodegenStats dump to stderr after every
	// compile. "size" highlights the native-byte ledger; "1" remains compatible.
	explainMode    = os.Getenv("WAGO_EXPLAIN")
	explainEnabled = explainMode == "1" || explainMode == "size"
	// debugModGlobals prints the module-pinned-global choices (the #90-era temp
	// print, now first-class).
	debugModGlobals = os.Getenv("WAGO_DEBUG_MODGLOBALS") == "1"
	// pinGlobalK overrides the adaptive module-global pin count K: -1 = auto (the
	// pickModuleGlobals heuristic), 0..len(moduleGlobalRegs) = force that many.
	pinGlobalK = parsePinGlobalK(os.Getenv("WAGO_PIN_GLOBAL_K"))
	// boundsFactsEnabled gates P6.1 straight-line bounds-check elision (explicit
	// mode). WAGO_NO_BOUNDS_FACTS=1 forces every check — the A/B oracle + kill switch.
	boundsFactsEnabled = os.Getenv("WAGO_NO_BOUNDS_FACTS") != "1"
	// compactI32FrameEnabled packs i32 locals in admitted kernels.
	compactI32FrameEnabled = os.Getenv("WAGO_NO_COMPACT_I32_FRAME") != "1"
	// accumulatorImmediateEnabled admits ModRM-free RAX/EAX imm32 encodings on
	// the explicit native-compaction path.
	accumulatorImmediateEnabled = os.Getenv("WAGO_AMD64_NO_ACCUMULATOR_IMMEDIATE") != "1"
	// sharedTrapBodyEnabled lets compact trap groups share the invariant
	// trap-cell stores and native-stack unwind within one compiled function.
	sharedTrapBodyEnabled = os.Getenv("WAGO_AMD64_NO_SHARED_TRAP_BODY") != "1"
	// compactLowPinEnabled makes RBP the first integer-local pin only for
	// call-free, straight-line compact functions. The register set and pin
	// count stay unchanged; this is an encoded-size tie-break experiment.
	compactLowPinEnabled = os.Getenv("WAGO_AMD64_NO_COMPACT_LOW_PIN") != "1"
	// localSlotOrderEnabled records exact emitted local-home references and lets
	// compact compilation swap referenced disp32 homes with equal-type zero-reference
	// low homes during finalization. WAGO_LOCAL_SLOT_ORDER=0 is the rollback.
	localSlotOrderEnabled = os.Getenv("WAGO_LOCAL_SLOT_ORDER") != "0"
	// commuteSelfUpdateEnabled makes a non-fixed destination the accumulator for
	// commutative x=f(y) op x expressions instead of spilling x first.
	// WAGO_AMD64_NO_COMMUTE_SELF_UPDATE=1 is the A/B oracle.
	commuteSelfUpdateEnabled = os.Getenv("WAGO_AMD64_NO_COMMUTE_SELF_UPDATE") != "1"
	// i64Mask32Enabled lowers i64.and with any low-32-bit mask to a 32-bit AND whose
	// destination write implicitly zero-extends. WAGO_AMD64_NO_I64_MASK32=1 is the
	// A/B oracle.
	i64Mask32Enabled = os.Getenv("WAGO_AMD64_NO_I64_MASK32") != "1"
	// stFlagsEnabled gates the stFlags tee-forward window (R1): a compare stored by
	// `local.tee $c` and consumed by the next if/br_if/select fuses into the branch,
	// storing $c with a flag-neutral SETcc after the CMP. WAGO_NO_STFLAGS=1 is the
	// A/B oracle + kill switch for this flag-desync-sensitive path.
	stFlagsEnabled = os.Getenv("WAGO_NO_STFLAGS") != "1"
	// store8FlagsEnabled gates direct low-byte comparison results consumed by an
	// i32.store8. WAGO_NO_STORE8_FLAGS=1 is the A/B oracle.
	store8FlagsEnabled = os.Getenv("WAGO_NO_STORE8_FLAGS") != "1"
	// swarMaskTestEnabled gates direct packed-word mask-test fusion.
	// WAGO_NO_SWAR_MASK_TEST=1 is the A/B oracle.
	swarMaskTestEnabled = os.Getenv("WAGO_NO_SWAR_MASK_TEST") != "1"
	// singleBitMaskTestEnabled selects BT for one-bit mask predicates in
	// native compaction. WAGO_AMD64_NO_SINGLE_BIT_MASK_TEST=1 is the A/B oracle.
	singleBitMaskTestEnabled = os.Getenv("WAGO_AMD64_NO_SINGLE_BIT_MASK_TEST") != "1"
	// simdSuperoptEnabled gates exact bounded selection of multi-op Wasm SIMD
	// sequences. WAGO_NO_SIMD_SUPEROPT=1 is the A/B oracle.
	simdSuperoptEnabled = os.Getenv("WAGO_NO_SIMD_SUPEROPT") != "1"

	// mul3opEnabled gates three-operand IMUL (dest = src*imm) that folds a borrowed
	// register source into a constant multiply. WAGO_NO_MUL3=1 is the A/B oracle.
	mul3opEnabled = os.Getenv("WAGO_NO_MUL3") != "1"

	// commuteMemLeftEnabled gates swapping a commutative op's memory left operand
	// with an owned-register right, to fold the memory as an r/m operand and
	// accumulate in the register. WAGO_NO_COMMUTE_MEM=1 is the A/B oracle.
	commuteMemLeftEnabled = os.Getenv("WAGO_NO_COMMUTE_MEM") != "1"

	// commuteFMemEnabled gates the float analogue: swapping a commutative float
	// op's (add/mul) memRef left operand with a non-memRef right so the load folds
	// as the SSE memory source instead of being materialized. IEEE add/mul are
	// exactly commutative (incl. NaN/±0), so output is bit-identical.
	// WAGO_NO_COMMUTE_FMEM=1 is the A/B oracle.
	commuteFMemEnabled = os.Getenv("WAGO_NO_COMMUTE_FMEM") != "1"
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
	CodeBytes     int                      // emitted machine-code length
	FrameBytes    int                      // stack frame size (sub rsp, N)
	MaxSpillSlots int                      // high-water operand spill slots
	GCCodeBytes   shared.GCNativeCodeBytes // diagnostic WasmGC byte attribution
	NativeSize    shared.NativeFunctionSizeReport
	Encoding      encoderamd64.EncodingStats
	// FinalizerFallback is the fail-closed reason a compact function kept
	// its maximal-safe encoding instead of applying an available compaction plan.
	FinalizerFallback string       `json:"finalizer_fallback,omitempty"`
	literalKeys       []literalKey // stats-only keys for module-level duplicate accounting
	Rel32Sites        uint32       `json:"rel32_sites,omitempty"`
	Rel32Recorded     int          `json:"rel32_recorded,omitempty"`
	Rel32Overflow     bool         `json:"rel32_overflow,omitempty"`
	// InlineSiteBytes is the exact pre-finalization byte span emitted directly by
	// inline sites. Caller frame growth and shared cold tails are outside it.
	InlineSiteBytes int

	// Register allocator / condense engine traffic.
	Flushes              int // full operand-stack flushes (control boundaries + calls)
	FlushBelows          int // partial flushes below a fused node
	Condenses            int // deferred-tree condensations to a register
	Spills               int // register→slot evictions under pressure
	Reloads              int // slot→register reloads of a spilled value
	MemRefsForcedByStore int // deferred loads forced out by a store (P2.1 target)

	// Bounds / traps.
	BoundsChecks            int // inline memory-OOB checks emitted (P6 elides these)
	BoundsChecksElidable    int // subset of BoundsChecks a straight-line certificate covers (P6.1 sizing; count-only)
	BoundsChecksInLoop      int // subset emitted inside a loop on a keyable base; count-only
	BoundsChecksHoistable   int // reserved for cross-target reporting; AMD64 no longer prewalks loops solely to populate it
	TrapStubs               int // shared cold trap stubs emitted (one per trap code used)
	TrapGroups              int // distinct source-function groups across trap stubs
	GCHandleResolutions     int // dynamic compact-handle resolutions emitted
	GCHandleResolutionReuse int // resolutions elided by bounded raw-address reuse

	// Calls, by lowering kind: regabi / mixed / wrapper / host / indirect /
	// crossinstance / importdispatch.
	Calls map[string]int

	// Pins.
	PinnedLocals       int // integer/float locals given a dedicated register
	PinnedGlobalsValue int // hot mutable-int globals value-pinned in this function
	PinRelinquishments int // pinned locals temporarily homed at exact exhaustion points

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
		ControlPeak:      uint64(sc.controlScratchPeak)*frameBytes + uint64(sc.controlMergePeak)*mergeBytes + uint64(sc.controlRootPeak)*rootBytes,
		ControlRetained:  uint64(cap(sc.ctrl))*frameBytes + uint64(cap(sc.ctrlMerges))*mergeBytes + uint64(cap(sc.ctrlRoots))*rootBytes,
		ControlDiscarded: uint64(sc.controlScratchDiscarded)*frameBytes + uint64(sc.controlMergeDiscarded)*mergeBytes + uint64(sc.controlRootDiscarded)*rootBytes,
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
func (s *CodegenStats) addGCHandleResolution() {
	if s != nil {
		s.GCHandleResolutions++
	}
}
func (s *CodegenStats) addGCHandleResolutionReuse() {
	if s != nil {
		s.GCHandleResolutionReuse++
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

// reclassifyPeep moves one already-recorded selection event to a more specific
// sink. It is used only on the opt-in explain path; ordinary compilation has a
// nil stats receiver and returns immediately.
func (s *CodegenStats) reclassifyPeep(from, to string) {
	if s == nil {
		return
	}
	s.Peephole[from]--
	if s.Peephole[from] == 0 {
		delete(s.Peephole, from)
	}
	s.Peephole[to]++
}

// ModuleGlobalPinInfo describes one module-wide global→register reservation.
type ModuleGlobalPinInfo = shared.ModuleGlobalPinInfo

// ModuleStats aggregates one module's per-function stats plus the module-wide
// decisions. The zero value is ready to collect into.
type ModuleStats struct {
	Funcs                 []*CodegenStats
	ModuleGlobalPins      []ModuleGlobalPinInfo
	Inline                *InlineReport // inline-candidate detection (nil if not analyzed)
	GCSharedStubBytes     int
	GCSharedStubs         int
	GCSharedStubCallSites int
	NativeSize            shared.NativeSizeReport
	Encoding              encoderamd64.EncodingStats
	Compile               shared.CompileResourceStats
}

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
		ms.NativeSize.HostAdapterBytes, ms.NativeSize.AdapterToInternalPaddingBytes, ms.NativeSize.InternalFunctionBytes)
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
	rel32Sites, rel32Recorded, rel32OverflowFuncs, rel32OverflowSites, maxRel32Sites := uint64(0), 0, 0, uint64(0), uint32(0)
	for _, s := range ms.Funcs {
		if s == nil {
			continue
		}
		rel32Sites += uint64(s.Rel32Sites)
		rel32Recorded += s.Rel32Recorded
		if s.Rel32Sites > maxRel32Sites {
			maxRel32Sites = s.Rel32Sites
		}
		if overflow := int64(s.Rel32Sites) - int64(s.Rel32Recorded); s.Rel32Overflow && overflow > 0 {
			rel32OverflowFuncs++
			rel32OverflowSites += uint64(overflow)
		}
	}
	fmt.Fprintf(&b, "amd64-rel32: sites=%d recorded=%d overflow-functions=%d overflow-sites=%d max-function-sites=%d\n",
		rel32Sites, rel32Recorded, rel32OverflowFuncs, rel32OverflowSites, maxRel32Sites)
	fmt.Fprintf(&b, "amd64-encoding: memory-disp0=%d memory-disp8=%d memory-disp32=%d memory-disp-bytes=%d frame-disp0=%d frame-disp8=%d frame-disp32=%d frame-disp-bytes=%d\n",
		ms.Encoding.MemoryDisp0, ms.Encoding.MemoryDisp8, ms.Encoding.MemoryDisp32,
		ms.Encoding.MemoryDisplacementBytes(), ms.Encoding.FrameDisp0, ms.Encoding.FrameDisp8,
		ms.Encoding.FrameDisp32, ms.Encoding.FrameDisplacementBytes())
	fmt.Fprintf(&b, "amd64-local-encoding: disp0=%d disp8=%d disp32=%d displacement-bytes=%d disp32-shortening-ceiling=%d\n",
		ms.Encoding.LocalDisp0, ms.Encoding.LocalDisp8, ms.Encoding.LocalDisp32,
		ms.Encoding.LocalFrameDisplacementBytes(), 3*ms.Encoding.LocalDisp32)
	fmt.Fprintf(&b, "amd64-prefixes: rex=%d rex-w=%d rex-bare=%d rex-nonw-extension=%d\n",
		ms.Encoding.RexPrefixes, ms.Encoding.RexWPrefixes, ms.Encoding.RexBare,
		ms.Encoding.RexNonWExtensionPrefixes())
	fmt.Fprintf(&b, "amd64-immediates: mov-imm32=%d mov-imm32-sext=%d mov-imm64=%d mov-imm64-narrowed=%d bytes-saved=%d\n",
		ms.Encoding.MovImm32, ms.Encoding.MovImm32Sext, ms.Encoding.MovImm64,
		ms.Encoding.MovImmNarrow, ms.Encoding.MovImmSaved)
	fmt.Fprintf(&b, "amd64-shifts: zero-elided=%d count-one=%d imm8=%d bytes-saved=%d\n",
		ms.Encoding.ShiftImmZero, ms.Encoding.ShiftImmOne, ms.Encoding.ShiftImm8,
		ms.Encoding.ShiftSaved)
	fmt.Fprintf(&b, "amd64-accumulator-immediates: alu=%d test=%d bytes-saved=%d\n",
		ms.Encoding.AluImm32Acc, ms.Encoding.TestImm32Acc,
		ms.Encoding.AluImm32Acc+ms.Encoding.TestImm32Acc)
	if ms.GCSharedStubs != 0 || ms.GCSharedStubCallSites != 0 {
		fmt.Fprintf(&b, "module GC leaf stubs: bodies=%d calls=%d bytes=%d\n", ms.GCSharedStubs, ms.GCSharedStubCallSites, ms.GCSharedStubBytes)
	}
	if len(ms.ModuleGlobalPins) == 0 {
		fmt.Fprintf(&b, "module-pinned globals: none (K=0)\n")
	} else {
		fmt.Fprintf(&b, "module-pinned globals (K=%d):", len(ms.ModuleGlobalPins))
		for _, p := range ms.ModuleGlobalPins {
			fmt.Fprintf(&b, " g%d→%s", p.Global, p.Reg)
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
	fmt.Fprintf(&b, "    rel32: sites=%d recorded=%d overflow=%t\n", s.Rel32Sites, s.Rel32Recorded, s.Rel32Overflow)
	fmt.Fprintf(&b, "    encoding: memory-disp0=%d memory-disp8=%d memory-disp32=%d memory-disp-bytes=%d frame-disp0=%d frame-disp8=%d frame-disp32=%d frame-disp-bytes=%d\n",
		s.Encoding.MemoryDisp0, s.Encoding.MemoryDisp8, s.Encoding.MemoryDisp32,
		s.Encoding.MemoryDisplacementBytes(), s.Encoding.FrameDisp0, s.Encoding.FrameDisp8,
		s.Encoding.FrameDisp32, s.Encoding.FrameDisplacementBytes())
	fmt.Fprintf(&b, "    local-encoding: disp0=%d disp8=%d disp32=%d displacement-bytes=%d disp32-shortening-ceiling=%d\n",
		s.Encoding.LocalDisp0, s.Encoding.LocalDisp8, s.Encoding.LocalDisp32,
		s.Encoding.LocalFrameDisplacementBytes(), 3*s.Encoding.LocalDisp32)
	fmt.Fprintf(&b, "    prefixes: rex=%d rex-w=%d rex-bare=%d rex-nonw-extension=%d\n",
		s.Encoding.RexPrefixes, s.Encoding.RexWPrefixes, s.Encoding.RexBare,
		s.Encoding.RexNonWExtensionPrefixes())
	fmt.Fprintf(&b, "    immediates: mov-imm32=%d mov-imm32-sext=%d mov-imm64=%d mov-imm64-narrowed=%d bytes-saved=%d\n",
		s.Encoding.MovImm32, s.Encoding.MovImm32Sext, s.Encoding.MovImm64,
		s.Encoding.MovImmNarrow, s.Encoding.MovImmSaved)
	fmt.Fprintf(&b, "    shifts: zero-elided=%d count-one=%d imm8=%d bytes-saved=%d\n",
		s.Encoding.ShiftImmZero, s.Encoding.ShiftImmOne, s.Encoding.ShiftImm8,
		s.Encoding.ShiftSaved)
	fmt.Fprintf(&b, "    accumulator-immediates: alu=%d test=%d bytes-saved=%d\n",
		s.Encoding.AluImm32Acc, s.Encoding.TestImm32Acc,
		s.Encoding.AluImm32Acc+s.Encoding.TestImm32Acc)
	fmt.Fprintf(&b, "    alloc: flushes=%d flushBelow=%d condenses=%d spills=%d reloads=%d forcedLoads=%d\n",
		s.Flushes, s.FlushBelows, s.Condenses, s.Spills, s.Reloads, s.MemRefsForcedByStore)
	fmt.Fprintf(&b, "    mem:   bounds=%d elidable=%d inloop=%d hoistable=%d trapStubs=%d trapGroups=%d   pins: local=%d gval=%d relinquish=%d\n",
		s.BoundsChecks, s.BoundsChecksElidable, s.BoundsChecksInLoop, s.BoundsChecksHoistable, s.TrapStubs, s.TrapGroups, s.PinnedLocals, s.PinnedGlobalsValue, s.PinRelinquishments)
	if s.InlineSiteBytes != 0 {
		fmt.Fprintf(&b, "    inline-site-bytes: %d\n", s.InlineSiteBytes)
	}
	if s.GCHandleResolutions != 0 || s.GCHandleResolutionReuse != 0 {
		fmt.Fprintf(&b, "    gcresolve: emitted=%d reused=%d\n", s.GCHandleResolutions, s.GCHandleResolutionReuse)
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
// regName maps a Reg to its lowercase x86-64 mnemonic for the explain dump.
func regName(r Reg) string {
	switch r {
	case RAX:
		return "rax"
	case RCX:
		return "rcx"
	case RDX:
		return "rdx"
	case RBX:
		return "rbx"
	case RSP:
		return "rsp"
	case RBP:
		return "rbp"
	case RSI:
		return "rsi"
	case RDI:
		return "rdi"
	case R8:
		return "r8"
	case R9:
		return "r9"
	case R10:
		return "r10"
	case R11:
		return "r11"
	case R12:
		return "r12"
	case R13:
		return "r13"
	case R14:
		return "r14"
	case R15:
		return "r15"
	default:
		return fmt.Sprintf("r?%d", r)
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
