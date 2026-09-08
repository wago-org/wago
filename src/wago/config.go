package wago

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/optimization"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// CoreFeatures is a bit set of WebAssembly Core specification features. A
// RuntimeConfig carries the set it will accept; modules using a disabled feature
// are rejected at compile time.
type CoreFeatures uint64

const (
	// CoreFeatureBulkMemoryOperations: memory.copy/fill (and the segment ops).
	CoreFeatureBulkMemoryOperations CoreFeatures = 1 << iota
	// CoreFeatureMultiValue: blocks and functions returning multiple values.
	CoreFeatureMultiValue
	// CoreFeatureMutableGlobal: importing/exporting mutable globals.
	CoreFeatureMutableGlobal
	// CoreFeatureNonTrappingFloatToIntConversion: the trunc_sat conversions.
	CoreFeatureNonTrappingFloatToIntConversion
	// CoreFeatureReferenceTypes: executable funcref tables plus reference
	// signatures/locals/control flow, ref.null, ref.func, ref.is_null, and
	// descriptor-identity ref.eq. Local and imported/shared reference globals,
	// typed externref tables/elements, every Release 2 table operation, exact
	// same-store sharing, opaque host funcref call boundaries, explicitly owned
	// HostFuncRef descriptor egress, and store-bound host-created funcref globals
	// execute. Unowned host descriptors remain fail-closed.
	CoreFeatureReferenceTypes
	// CoreFeatureSignExtensionOps: i32/i64.extend{8,16,32}_s.
	CoreFeatureSignExtensionOps
	// CoreFeatureSIMD: core and relaxed v128 vector instructions. This remains
	// one admission bit for compatibility with the existing executable SIMD
	// surface; relaxed SIMD is therefore represented by this bit in CoreFeaturesV3.
	CoreFeatureSIMD
	// CoreFeatureExtendedConst: integer add/sub/mul in constant expressions.
	CoreFeatureExtendedConst
	// CoreFeatureTailCall: return_call / return_call_indirect / return_call_ref.
	CoreFeatureTailCall
	// CoreFeatureExtendedConstExpressions: integer add/sub/mul and references to
	// previously declared immutable globals in constant expressions.
	CoreFeatureExtendedConstExpressions
	// CoreFeatureTypedFunctionReferences: typed refs, call_ref, and related casts.
	CoreFeatureTypedFunctionReferences
	// CoreFeatureGC: struct, array, i31, and GC-managed reference instructions.
	CoreFeatureGC
	// CoreFeatureExceptionHandling: tags, throw/throw_ref, and try_table.
	CoreFeatureExceptionHandling
	// CoreFeatureMultiMemory: multiple memories and indexed memory operations.
	CoreFeatureMultiMemory
	// CoreFeatureMemory64: 64-bit linear-memory limits and addresses.
	CoreFeatureMemory64
	// CoreFeatureTable64: 64-bit table limits and indexes.
	CoreFeatureTable64
	// CoreFeatureThreads: shared memory and core atomic memory instructions.
	CoreFeatureThreads
)

// Feature groups for WebAssembly Core releases.
const (
	// CoreFeaturesV1 is the WebAssembly 1.0 (MVP) feature set.
	CoreFeaturesV1 = CoreFeatureMutableGlobal
	// CoreFeaturesV2 is the WebAssembly 2.0 feature set.
	CoreFeaturesV2 = CoreFeaturesV1 |
		CoreFeatureBulkMemoryOperations |
		CoreFeatureMultiValue |
		CoreFeatureNonTrappingFloatToIntConversion |
		CoreFeatureReferenceTypes |
		CoreFeatureSignExtensionOps |
		CoreFeatureSIMD

	// CoreFeaturesV3 is the mandatory WebAssembly Core 3.0 release feature set.
	// CoreFeatureSIMD represents both core and relaxed SIMD in wago's existing
	// admission model. The set describes release scope, not current executability;
	// intersect it with SupportedFeatures before configuring a runtime.
	CoreFeaturesV3 = CoreFeaturesV2 |
		CoreFeatureTailCall |
		CoreFeatureExtendedConstExpressions |
		CoreFeatureTypedFunctionReferences |
		CoreFeatureGC |
		CoreFeatureExceptionHandling |
		CoreFeatureMultiMemory |
		CoreFeatureMemory64 |
		CoreFeatureTable64

	// coreFeaturesWago is the optional set wago's backend lowers and the ceiling
	// validated by WithCoreFeatures.
	coreFeaturesWago = CoreFeaturesV3 | CoreFeatureExtendedConst | CoreFeatureThreads

	// coreFeaturesWithoutSidecar is the set whose compiled representation needs
	// no feature-specific structural sidecar. Keep this separate from the runtime
	// default: Core 3 products still validate their persisted metadata even where
	// they are admitted by default.
	coreFeaturesWithoutSidecar = CoreFeatureMutableGlobal |
		CoreFeatureSignExtensionOps |
		CoreFeatureMultiValue |
		CoreFeatureBulkMemoryOperations |
		CoreFeatureNonTrappingFloatToIntConversion |
		CoreFeatureReferenceTypes |
		CoreFeatureSIMD |
		CoreFeatureExtendedConst |
		CoreFeatureExtendedConstExpressions

	// defaultCore3Features contains the finalized Core 3 families that extend
	// validation and execution without making managed-object lifetime or native
	// exception unwinding part of every runtime's default contract.
	defaultCore3Features = CoreFeatureTailCall |
		CoreFeatureTypedFunctionReferences |
		CoreFeatureMultiMemory |
		CoreFeatureMemory64 |
		CoreFeatureTable64
)

// IsEnabled returns true if all bits in feature are set.
func (f CoreFeatures) IsEnabled(feature CoreFeatures) bool { return f&feature == feature }

// SetEnabled returns a copy with feature turned on or off.
func (f CoreFeatures) SetEnabled(feature CoreFeatures, enabled bool) CoreFeatures {
	if enabled {
		return f | feature
	}
	return f &^ feature
}

func (f CoreFeatures) String() string {
	var names []string
	for _, feature := range featureRegistry {
		if f.IsEnabled(feature.Feature) {
			names = append(names, feature.Name)
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, "|")
}

// FeatureInfo describes one configurable WebAssembly feature. FeatureInfos is
// the source consumed by the CLI; registering a feature here automatically adds
// config discovery, validation, JSON output, and the experimental preview.
type FeatureInfo struct {
	Feature      CoreFeatures `json:"-"`
	Name         string       `json:"name"`
	Label        string       `json:"label"`
	Description  string       `json:"description"`
	Default      bool         `json:"default"`
	Experimental bool         `json:"experimental"`
	Available    bool         `json:"available"`
}

var featureRegistry = []FeatureInfo{
	{Feature: CoreFeatureBulkMemoryOperations, Name: "bulk-memory-operations", Label: "Bulk memory", Description: "memory.copy, memory.fill, and segment operations"},
	{Feature: CoreFeatureMultiValue, Name: "multi-value", Label: "Multi-value", Description: "multiple block and function results"},
	{Feature: CoreFeatureMutableGlobal, Name: "mutable-global", Label: "Mutable globals", Description: "import and export mutable globals"},
	{Feature: CoreFeatureNonTrappingFloatToIntConversion, Name: "nontrapping-float-to-int-conversion", Label: "Non-trapping conversions", Description: "saturating float-to-integer conversions"},
	{Feature: CoreFeatureReferenceTypes, Name: "reference-types", Label: "Reference types", Description: "funcref, externref, tables, and reference instructions"},
	{Feature: CoreFeatureSignExtensionOps, Name: "sign-extension-ops", Label: "Sign extension", Description: "integer sign-extension instructions"},
	{Feature: CoreFeatureSIMD, Name: "simd", Label: "SIMD", Description: "128-bit vector instructions"},
	{Feature: CoreFeatureExtendedConst, Name: "extended-constant-expressions", Label: "Extended constants", Description: "integer arithmetic in constant expressions"},
	{Feature: CoreFeatureExtendedConstExpressions, Name: "extended-const-expressions", Label: "Extended constant expressions", Description: "imported globals in constant expressions"},
	{Feature: CoreFeatureTailCall, Name: "tail-call", Label: "Tail calls", Description: "return_call, return_call_indirect, and return_call_ref"},
	{Feature: CoreFeatureTypedFunctionReferences, Name: "typed-function-references", Label: "Typed function references", Description: "typed references, call_ref, and related casts"},
	{Feature: CoreFeatureGC, Name: "gc", Label: "Garbage collection", Description: "struct, array, i31, and managed reference instructions", Experimental: true},
	{Feature: CoreFeatureExceptionHandling, Name: "exception-handling", Label: "Exception handling", Description: "tags, throw, and try_table", Experimental: true},
	{Feature: CoreFeatureMultiMemory, Name: "multi-memory", Label: "Multiple memories", Description: "multiple memories and indexed memory instructions"},
	{Feature: CoreFeatureMemory64, Name: "memory64", Label: "64-bit memory", Description: "64-bit linear-memory limits and addresses"},
	{Feature: CoreFeatureTable64, Name: "table64", Label: "64-bit tables", Description: "64-bit table limits and indexes"},
	{Feature: CoreFeatureThreads, Name: "threads", Label: "Threads and atomics", Description: "shared memory and atomic memory instructions", Experimental: true},
}

// FeatureInfos returns every registered feature in stable display order. Its
// default and availability fields describe the current build.
func FeatureInfos() []FeatureInfo {
	// Configuration describes the build's compiler surface, not the current
	// machine's optional CPU instructions. SIMD remains configurable on a host
	// without SIMD just as RuntimeConfig.Validate permits it; compilation still
	// fails closed when a module actually requires unavailable instructions.
	supported := platformCoreFeatures()
	result := make([]FeatureInfo, len(featureRegistry))
	for index, feature := range featureRegistry {
		feature.Default = defaultCoreFeatures().IsEnabled(feature.Feature)
		feature.Available = supported.IsEnabled(feature.Feature)
		result[index] = feature
	}
	return result
}

// FeatureInfoByName resolves a registered feature name.
func FeatureInfoByName(name string) (FeatureInfo, bool) {
	for _, feature := range FeatureInfos() {
		if feature.Name == name {
			return feature, true
		}
	}
	return FeatureInfo{}, false
}

// BoundsCheckMode selects how out-of-bounds linear-memory accesses are caught.
type BoundsCheckMode int

const (
	// BoundsChecksExplicit emits an inline bounds check on every access. The
	// default; needs no signal handler.
	BoundsChecksExplicit BoundsCheckMode = iota
	// BoundsChecksSignalsBased elides eligible memory-0 memory32 checks and relies
	// on a guard-page mapping plus a SIGSEGV/SIGBUS handler. Indexed nonzero
	// memories and memory64 retain
	// explicit checks. The mode is faster on memory-heavy code, but installs
	// process-wide signal handlers and requires a `wago_guardpage` build.
	BoundsChecksSignalsBased
)

func (m BoundsCheckMode) String() string {
	switch m {
	case BoundsChecksExplicit:
		return "explicit"
	case BoundsChecksSignalsBased:
		return "signals-based"
	default:
		return fmt.Sprintf("BoundsCheckMode(%d)", int(m))
	}
}

// RuntimeConfig configures compilation and execution. It is immutable — every
// WithXxx returns a copy, so a base config can be shared and specialised safely.
type RuntimeConfig struct {
	features                 CoreFeatures
	optimizations            map[string]bool
	optimizationSnapshot     railshotOptimizationSnapshot
	optimizationDeltas       map[string]bool
	trustedOptimizations     bool
	maxMemoryPages           uint32
	maxFunctionLocals        uint32 // total function parameters plus declared locals
	maxMemoriesPerModule     uint32
	maxInstanceMetadataBytes uint64
	maxCompiledMetadataBytes uint64
	maxModuleBytes           uint64
	maxNativeCodeBytes       uint64
	boundsChecks             BoundsCheckMode
	noDeferBounds            bool   // disable skipping of provably-redundant bounds checks (default: enabled)
	functionWorkers          int    // function validation/codegen: 0 adaptive; 1 serial; >1 forced maximum
	nativeStackBytes         uint64 // per-Engine foreign execution stack capacity
	gcCodeTelemetry          bool   // collect code-neutral per-family WasmGC native byte attribution
	independentInstances     bool   // allow unrelated instances to execute native code concurrently
	instanceLimits           *runtimeInstanceLimits
}

type runtimeInstanceLimits struct {
	maxInstances            uint32
	maxMemoryBytes          uint64
	maxNativeMemoryMappings uint32
}

// A zero memory-page limit means no additional RuntimeConfig quota. Declared
// Wasm limits and platform representation checks still apply.
const defaultMaxMemoryPages = 0

// Native execution stack capacities are bounded so one instance cannot retain
// unbounded off-heap virtual address space. The minimum preserves the fixed
// 256 KiB fence plus usable frame space.
const (
	DefaultNativeStackBytes = coreruntime.DefaultNativeStackBytes
	MinNativeStackBytes     = coreruntime.MinNativeStackBytes
	MaxNativeStackBytes     = coreruntime.MaxNativeStackBytes
)

// DefaultMaxFunctionLocals is the default ceiling for one function's combined
// parameter and declared-local count. MaxFunctionLocalsLimit is the largest
// configurable ceiling; native frame-size safety remains independently checked.
const (
	DefaultMaxFunctionLocals    = wasm.DefaultMaxFunctionLocals
	MaxFunctionLocalsLimit      = wasm.MaximumFunctionLocals
	DefaultMaxMemoriesPerModule = wasm.DefaultMaxMemoriesPerModule
	MaxMemoriesPerModuleLimit   = wasm.MaximumMemoriesPerModule
)

var defaultOptimizationCache struct {
	sync.Mutex
	snapshot   railshotOptimizationSnapshot
	values     map[string]bool
	bmi2Values map[string]bool
	bmi2Delta  map[string]bool
}

// defaultOptimizationSnapshot returns an immutable process-default selection.
// The cache owns the maps: callers only read them, and WithOptimization clones
// before mutation. A backend revision change rebuilds the snapshot from values
// captured under the same lock as SetOptKnob.
func defaultOptimizationSnapshot(forceBMI2 bool) (values map[string]bool, snapshot railshotOptimizationSnapshot, deltas map[string]bool) {
	defaultOptimizationCache.Lock()
	defer defaultOptimizationCache.Unlock()

	snapshot = railshotCurrentOptKnobSnapshot()
	if defaultOptimizationCache.values == nil || defaultOptimizationCache.snapshot != snapshot {
		infos, capturedSnapshot := railshotOptKnobSnapshot()
		values = make(map[string]bool, len(infos))
		for _, info := range infos {
			values[info.Name] = info.On
		}
		defaultOptimizationCache.snapshot = capturedSnapshot
		defaultOptimizationCache.values = values
		defaultOptimizationCache.bmi2Values = nil
		defaultOptimizationCache.bmi2Delta = nil
		snapshot = capturedSnapshot
	}
	values = defaultOptimizationCache.values
	snapshot = defaultOptimizationCache.snapshot
	if !forceBMI2 || values["bmi2-rorx"] {
		return values, snapshot, nil
	}
	if defaultOptimizationCache.bmi2Values == nil {
		bmi2Values := make(map[string]bool, len(values))
		for name, on := range values {
			bmi2Values[name] = on
		}
		bmi2Values["bmi2-rorx"] = true
		defaultOptimizationCache.bmi2Values = bmi2Values
		defaultOptimizationCache.bmi2Delta = map[string]bool{"bmi2-rorx": true}
	}
	return defaultOptimizationCache.bmi2Values, snapshot, defaultOptimizationCache.bmi2Delta
}

// NewRuntimeConfig returns the default configuration: wago's selected feature
// set for this build, serial function validation/codegen, independent-instance execution, and
// the fastest available bounds-check mode — signals-based (guard-page) when
// built with -tags wago_guardpage, explicit otherwise. WAGO_BOUNDS overrides
// either way ("explicit" / "signals").
func NewRuntimeConfig() *RuntimeConfig {
	bounds := BoundsChecksExplicit
	if guardPageBuilt {
		bounds = BoundsChecksSignalsBased
	}
	switch strings.ToLower(os.Getenv("WAGO_BOUNDS")) {
	case "signals", "signal", "guard", "guardpage", "guard-page":
		bounds = BoundsChecksSignalsBased
	case "explicit", "inline":
		bounds = BoundsChecksExplicit
	}
	forceBMI2 := runtime.GOARCH == "amd64" && hostSupportsBMI2() && os.Getenv("WAGO_AMD64_NO_BMI2_RORX") != "1"
	optimizations, optimizationSnapshot, optimizationDeltas := defaultOptimizationSnapshot(forceBMI2)
	return &RuntimeConfig{
		features:             defaultCoreFeatures(),
		optimizations:        optimizations,
		optimizationSnapshot: optimizationSnapshot,
		optimizationDeltas:   optimizationDeltas,
		trustedOptimizations: true,
		maxMemoryPages:       defaultMaxMemoryPages,
		maxModuleBytes:       64 << 20,
		maxFunctionLocals:    DefaultMaxFunctionLocals,
		maxMemoriesPerModule: DefaultMaxMemoriesPerModule,
		boundsChecks:         bounds,
		functionWorkers:      1,
		nativeStackBytes:     DefaultNativeStackBytes,
		independentInstances: true,
	}
}

// WithCoreFeatures sets the accepted WebAssembly feature set. Validated on use.
func (c *RuntimeConfig) WithCoreFeatures(features CoreFeatures) *RuntimeConfig {
	n := *c
	n.features = features
	return &n
}

// WithInstanceLimits caps the number and total declared maximum linear-memory
// reservation of concurrently live direct Runtime instances. Zero leaves the
// corresponding aggregate unbounded.
func (c *RuntimeConfig) WithInstanceLimits(maxInstances uint32, maxMemoryBytes uint64) *RuntimeConfig {
	n := *c
	limits := runtimeInstanceLimits{maxInstances: maxInstances, maxMemoryBytes: maxMemoryBytes}
	if c.instanceLimits != nil {
		limits.maxNativeMemoryMappings = c.instanceLimits.maxNativeMemoryMappings
	}
	n.instanceLimits = &limits
	return &n
}

// WithNativeMemoryMappingLimit caps mappings that live instances in this
// Runtime own. Zero removes this Runtime limit. The fixed Linux process limit
// remains 4,096 mappings.
func (c *RuntimeConfig) WithNativeMemoryMappingLimit(maxMappings uint32) *RuntimeConfig {
	n := *c
	limits := runtimeInstanceLimits{}
	if c.instanceLimits != nil {
		limits = *c.instanceLimits
	}
	limits.maxNativeMemoryMappings = maxMappings
	n.instanceLimits = &limits
	return &n
}

// WithFeatures sets the accepted feature set to the union of the listed features
// — a readable, typo-proof alternative to OR-ing the bit set by hand. It
// replaces the set (like WithCoreFeatures); use WithFeature to toggle one on top.
//
//	cfg := wago.NewRuntimeConfig().WithFeatures(
//		wago.CoreFeatureMutableGlobal,
//		wago.CoreFeatureSignExtensionOps,
//	)
func (c *RuntimeConfig) WithFeatures(features ...CoreFeatures) *RuntimeConfig {
	var set CoreFeatures
	for _, f := range features {
		set |= f
	}
	return c.WithCoreFeatures(set)
}

// WithFeature toggles a single feature (or any OR-combined subset) on or off,
// without rebuilding the whole set:
//
//	cfg := wago.NewRuntimeConfig().WithFeature(wago.CoreFeatureBulkMemoryOperations, false)
func (c *RuntimeConfig) WithFeature(feature CoreFeatures, enabled bool) *RuntimeConfig {
	n := *c
	n.features = n.features.SetEnabled(feature, enabled)
	// The legacy arithmetic bit predates the Core 3 umbrella bit. Explicitly
	// disabling it keeps the historical promise that extended-constant arithmetic
	// is off, even when the default also enables prior immutable globals.
	if !enabled && feature.IsEnabled(CoreFeatureExtendedConst) {
		n.features &^= CoreFeatureExtendedConstExpressions
	}
	return &n
}

// WithGCCodeTelemetry enables code-neutral WasmGC native-byte attribution on
// freshly compiled modules. It does not change emitted code and is not persisted
// in .wago artifacts.
func (c *RuntimeConfig) WithGCCodeTelemetry(enabled bool) *RuntimeConfig {
	n := *c
	n.gcCodeTelemetry = enabled
	return &n
}

// WithOptimization returns a configuration with one compiler optimization
// selected on or off. Unknown names are rejected by Validate and Compile.
func (c *RuntimeConfig) WithOptimization(name string, enabled bool) *RuntimeConfig {
	n := *c
	n.optimizations = c.optimizationValues()
	n.optimizations[name] = enabled
	n.optimizationDeltas = c.optimizationDeltaValues()
	n.optimizationDeltas[name] = enabled
	n.trustedOptimizations = false
	return &n
}

// WithOptimizations returns a configuration with all supplied optimization
// selections applied to the current selection.
func (c *RuntimeConfig) WithOptimizations(values map[string]bool) *RuntimeConfig {
	n := *c
	n.optimizations = c.optimizationValues()
	for name, enabled := range values {
		n.optimizations[name] = enabled
	}
	n.optimizationDeltas = c.optimizationDeltaValues()
	for name, enabled := range values {
		n.optimizationDeltas[name] = enabled
	}
	n.trustedOptimizations = false
	return &n
}

// WithMemoryLimitPages caps each linear memory's live size in 64 KiB pages.
// Zero removes this additional runtime quota. The quota applies at instance
// creation and to memory.grow, including imported and indexed memories.
func (c *RuntimeConfig) WithMemoryLimitPages(pages uint32) *RuntimeConfig {
	n := *c
	n.maxMemoryPages = pages
	return &n
}

// WithMaxInstanceMetadataBytes caps the validated off-heap metadata allocated
// for one instance. Zero leaves this resource unbounded.
func (c *RuntimeConfig) WithMaxInstanceMetadataBytes(bytes uint64) *RuntimeConfig {
	n := *c
	n.maxInstanceMetadataBytes = bytes
	return &n
}

// WithMaxCompiledMetadataBytes bounds owned execution-snapshot metadata before
// cloning. Zero selects the 256 MiB default. This is separate from native
// instance metadata and applies to compilation and precompiled module admission.
func (c *RuntimeConfig) WithMaxCompiledMetadataBytes(bytes uint64) *RuntimeConfig {
	n := *c
	n.maxCompiledMetadataBytes = bytes
	return &n
}

// MaxCompiledMetadataBytes returns the configured snapshot quota; zero selects
// the default decoded metadata quota.
func (c *RuntimeConfig) MaxCompiledMetadataBytes() uint64 { return c.maxCompiledMetadataBytes }

// WithMaxModuleBytes caps input Wasm bytes accepted by compilation. Zero is
// unbounded. The default is 64 MiB. Decode-time type and metadata limits still
// apply independently. This is a cheap front-door compile resource quota.
func (c *RuntimeConfig) WithMaxModuleBytes(bytes uint64) *RuntimeConfig {
	n := *c
	n.maxModuleBytes = bytes
	return &n
}

// WithMaxNativeCodeBytes caps generated native code bytes for one module. Zero
// is unbounded. Runtime.Module rechecks decoded artifacts against this quota.
func (c *RuntimeConfig) WithMaxNativeCodeBytes(bytes uint64) *RuntimeConfig {
	n := *c
	n.maxNativeCodeBytes = bytes
	return &n
}

// WithMaxFunctionLocals sets the maximum combined parameter and declared-local
// count for one function. Valid values are 1 through 65,535. This bounds
// validation/compiler bookkeeping; native frame-size checks may reject a lower
// count when its slots and spills exceed the stack fence.
func (c *RuntimeConfig) WithMaxFunctionLocals(locals uint32) *RuntimeConfig {
	n := *c
	n.maxFunctionLocals = locals
	return &n
}

// WithMaxMemoriesPerModule sets the maximum count of imported and local
// memories in one module. Valid values are 1 through 4,096. The default is 100.
func (c *RuntimeConfig) WithMaxMemoriesPerModule(memories uint32) *RuntimeConfig {
	n := *c
	n.maxMemoriesPerModule = memories
	return &n
}

// WithBoundsChecks selects the linear-memory bounds-check strategy (wago
// extension). BoundsChecksSignalsBased requires the wago_guardpage build tag.
func (c *RuntimeConfig) WithBoundsChecks(mode BoundsCheckMode) *RuntimeConfig {
	n := *c
	n.boundsChecks = mode
	return &n
}

// WithDeferBoundsChecks controls whether the compiler skips a bounds check that a
// prior check in the same straight-line region already proved safe (explicit mode
// only; signal mode uses its fixed hybrid policy). On by default — pass false to
// bounds-check every eligible explicit-mode memory access, e.g. for A/B testing or
// maximal defensiveness. The
// WAGO_NO_BOUNDS_FACTS=1 env var disables it globally.
func (c *RuntimeConfig) WithDeferBoundsChecks(enabled bool) *RuntimeConfig {
	n := *c
	n.noDeferBounds = !enabled
	return &n
}

// WithFunctionWorkers sets the per-module function validation/codegen policy.
// Zero selects the measured adaptive policy, one forces the serial fast path,
// and N > 1 forces at most N workers (still capped by GOMAXPROCS and local-
// function count). Negative values are rejected by Validate.
func (c *RuntimeConfig) WithFunctionWorkers(workers int) *RuntimeConfig {
	n := *c
	n.functionWorkers = workers
	return &n
}

// WithNativeStackBytes sets the foreign execution stack capacity for each
// instance and synchronous host re-entry Engine. Valid values are 16-byte
// aligned capacities from 512 KiB through 1 GiB. The default remains 4 MiB.
func (c *RuntimeConfig) WithNativeStackBytes(stackBytes uint64) *RuntimeConfig {
	n := *c
	n.nativeStackBytes = stackBytes
	return &n
}

// WithIndependentInstanceExecution controls whether separately instantiated
// modules may execute native code concurrently. It is enabled by default;
// instances with cross-instance Wasm imports automatically use the process-wide
// execution lease instead. Disable it to force that conservative lease, for
// example when an extension maintains shared native state Wago cannot detect.
func (c *RuntimeConfig) WithIndependentInstanceExecution(enabled bool) *RuntimeConfig {
	n := *c
	n.independentInstances = enabled

	return &n
}

// WithCompileWorkers is retained for source compatibility.
// Deprecated: use WithFunctionWorkers.
func (c *RuntimeConfig) WithCompileWorkers(workers int) *RuntimeConfig {
	return c.WithFunctionWorkers(workers)
}

// CoreFeatures reports the configured feature set.
func (c *RuntimeConfig) CoreFeatures() CoreFeatures { return c.features }

// OptimizationInfos reports this configuration's immutable selection in stable
// backend order.
func (c *RuntimeConfig) OptimizationInfos() []OptKnobInfo {
	infos := OptimizationInfosForArch(runtime.GOARCH)
	for index := range infos {
		if enabled, ok := c.optimizations[infos[index].Name]; ok {
			infos[index].On = enabled
		}
	}
	return infos
}

func (c *RuntimeConfig) optimizationValues() map[string]bool {
	values := make(map[string]bool, len(c.optimizations))
	for name, enabled := range c.optimizations {
		values[name] = enabled
	}
	return values
}

func (c *RuntimeConfig) optimizationDeltaValues() map[string]bool {
	values := make(map[string]bool, len(c.optimizationDeltas)+1)
	for name, enabled := range c.optimizationDeltas {
		values[name] = enabled
	}
	return values
}

func (c *RuntimeConfig) clone() *RuntimeConfig {
	if c == nil {
		return NewRuntimeConfig()
	}
	clone := *c
	clone.optimizations = c.optimizationValues()
	return &clone
}

// BoundsChecks reports the configured bounds-check mode.
func (c *RuntimeConfig) BoundsChecks() BoundsCheckMode { return c.boundsChecks }

// DeferBoundsChecks reports whether skipping of provably-redundant bounds checks
// is enabled.
func (c *RuntimeConfig) DeferBoundsChecks() bool { return !c.noDeferBounds }

// MemoryLimitPages reports the per-memory live-page quota. Zero is unbounded.
func (c *RuntimeConfig) MemoryLimitPages() uint32 { return c.maxMemoryPages }

// MaxInstanceMetadataBytes reports the per-instance metadata-byte quota. Zero
// is unbounded.
func (c *RuntimeConfig) MaxInstanceMetadataBytes() uint64 { return c.maxInstanceMetadataBytes }

// MaxModuleBytes reports the compile input-byte quota. Zero is unbounded.
func (c *RuntimeConfig) MaxModuleBytes() uint64 { return c.maxModuleBytes }

// MaxNativeCodeBytes reports the generated native-code quota. Zero is unbounded.
func (c *RuntimeConfig) MaxNativeCodeBytes() uint64 { return c.maxNativeCodeBytes }

// MaxFunctionLocals reports the configured combined parameter and declared-
// local ceiling for one function.
func (c *RuntimeConfig) MaxFunctionLocals() uint32 { return c.maxFunctionLocals }

// MaxMemoriesPerModule reports the configured memory declaration ceiling.
func (c *RuntimeConfig) MaxMemoriesPerModule() uint32 { return c.maxMemoriesPerModule }

// GCCodeTelemetry reports whether fresh compilation should retain code-neutral
// WasmGC native-byte attribution. Serialized artifacts do not contain it.
func (c *RuntimeConfig) GCCodeTelemetry() bool { return c.gcCodeTelemetry }

// FunctionWorkers reports the configured function-pipeline worker policy: zero
// adaptive, one serial, or a positive forced maximum.
func (c *RuntimeConfig) FunctionWorkers() int { return c.functionWorkers }

// NativeStackBytes reports the configured foreign execution stack capacity.
func (c *RuntimeConfig) NativeStackBytes() uint64 { return c.nativeStackBytes }

// IndependentInstanceExecution reports whether native calls use instance-local
// execution leases instead of the process-wide cross-instance lease.
func (c *RuntimeConfig) IndependentInstanceExecution() bool { return c.independentInstances }

// CompileWorkers is retained for source compatibility.
// Deprecated: use FunctionWorkers.
func (c *RuntimeConfig) CompileWorkers() int { return c.FunctionWorkers() }

// Compile decodes, validates, and compiles wasmBytes under this config. On
// success the returned Compiled owns the byte slice and the caller must not
// mutate or reuse its backing array. It is the fluent form of Compile(c,
// wasmBytes):
//
//	mod, err := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksSignalsBased).Compile(b)
func (c *RuntimeConfig) Compile(wasmBytes []byte) (*Compiled, error) {
	return Compile(c, wasmBytes)
}

// MustCompile is like Compile but panics on error.
func (c *RuntimeConfig) MustCompile(wasmBytes []byte) *Compiled {
	m, err := Compile(c, wasmBytes)
	if err != nil {
		panic("wago: MustCompile: " + err.Error())
	}
	return m
}

func (c *RuntimeConfig) String() string {
	return fmt.Sprintf("RuntimeConfig{features: %s, optimizations: %d, bounds: %s, maxMemoryPages: %d, maxFunctionLocals: %d, maxMemoriesPerModule: %d, maxInstanceMetadataBytes: %d, maxCompiledMetadataBytes: %d, maxModuleBytes: %d, maxNativeCodeBytes: %d, functionWorkers: %d, nativeStackBytes: %d, independentInstances: %t}",
		c.features, len(c.optimizations), c.boundsChecks, c.maxMemoryPages, c.maxFunctionLocals, c.maxMemoriesPerModule, c.maxInstanceMetadataBytes, c.maxCompiledMetadataBytes, c.maxModuleBytes, c.maxNativeCodeBytes, c.functionWorkers, c.nativeStackBytes, c.independentInstances)
}

// SupportedFeatures reports the WebAssembly feature set this wago build can
// compile. Intersect a desired set with it to stay portable:
//
//	feats := want & wago.SupportedFeatures()
func supportsCompleteCore3Backend(goos, goarch string) bool {
	return (goos == "linux" && goarch == "amd64") ||
		(goarch == "arm64" && (goos == "linux" || goos == "darwin"))
}

func supportsThreadsBackend(goos, goarch string) bool {
	return (goarch == "amd64" || goarch == "arm64") && (goos == "linux" || goos == "darwin")
}

func platformCoreFeatures() CoreFeatures {
	supported := coreFeaturesWago
	if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
		// Targets outside linux/amd64 and Linux/Darwin arm64 retain the portable
		// Release 2 surface plus extended constant expressions and reject the
		// incomplete Core 3 families at configuration time.
		unsupported := CoreFeatureTailCall |
			CoreFeatureTypedFunctionReferences |
			CoreFeatureGC |
			CoreFeatureExceptionHandling |
			CoreFeatureMultiMemory |
			CoreFeatureMemory64 |
			CoreFeatureTable64
		supported &^= unsupported
	}
	if !supportsThreadsBackend(runtime.GOOS, runtime.GOARCH) {
		supported &^= CoreFeatureThreads
	}
	return supported
}

// defaultCoreFeatures admits selected finalized Core 3 families on backends that
// implement the complete product. Other targets retain the portable Release 2
// plus extended-constant surface instead of silently accepting partial support.
// GC, exception handling, and the separate threads proposal remain opt-in.
func defaultCoreFeatures() CoreFeatures {
	return coreFeaturesWithoutSidecar | (defaultCore3Features & platformCoreFeatures())
}

func SupportedFeatures() CoreFeatures {
	supported := platformCoreFeatures()
	if !hostSupportsSIMD() {
		supported &^= CoreFeatureSIMD
	}
	return supported
}

// GuardPageSupported reports whether this binary was built with guard-page
// (signals-based) bounds checks — i.e. with -tags wago_guardpage. Use it to
// pick a bounds-check mode at runtime without a hard failure.
func GuardPageSupported() bool { return guardPageBuilt }

// GuardPageUnavailableError is returned (via Validate / Compile) when
// BoundsChecksSignalsBased is configured but the binary was not built with
// -tags wago_guardpage. Test for it with IsGuardPageUnavailable or errors.As.
type GuardPageUnavailableError struct{}

func (*GuardPageUnavailableError) Error() string {
	return "wago: signals-based bounds checks require a binary built with -tags wago_guardpage"
}

// IsGuardPageUnavailable reports whether err is a *GuardPageUnavailableError —
// the ergonomic check for "this build can't do signals-based bounds checks".
func IsGuardPageUnavailable(err error) bool {
	return errors.As(err, new(*GuardPageUnavailableError))
}

// UnsupportedFeatureError reports that a config requested WebAssembly features
// this wago build cannot compile. Inspect it with errors.As.
type UnsupportedFeatureError struct {
	Requested CoreFeatures // the specific unsupported features
	Supported CoreFeatures // what this build does support
	Platform  string       // GOOS/GOARCH admission target
}

func (e *UnsupportedFeatureError) Error() string {
	platform := e.Platform
	if platform == "" {
		platform = "unknown platform"
	}
	return fmt.Sprintf("wago: unsupported feature(s) %s; this %s build supports %s", e.Requested, platform, e.Supported)
}

// frontendFeatures maps the config's feature set onto the frontend support
// pass's gate.
func (c *RuntimeConfig) frontendFeatures() frontend.Features {
	simd := c.features.IsEnabled(CoreFeatureSIMD)
	if simd && !hostSupportsSIMD() {
		// Do not admit SIMD modules on hosts that cannot execute the backend's AVX
		// and SSSE3/SSE4.1/SSE4.2 instruction sequences: reject at compile time
		// instead of risking SIGILL at runtime. Non-SIMD modules still compile with
		// the default
		// feature set on such hosts.
		simd = false
	}
	return frontend.Features{
		SignExtension:           c.features.IsEnabled(CoreFeatureSignExtensionOps),
		BulkMemory:              c.features.IsEnabled(CoreFeatureBulkMemoryOperations),
		SaturatingTrunc:         c.features.IsEnabled(CoreFeatureNonTrappingFloatToIntConversion),
		ReferenceTypes:          c.features.IsEnabled(CoreFeatureReferenceTypes),
		TypedFunctionReferences: c.features.IsEnabled(CoreFeatureTypedFunctionReferences),
		TailCalls:               c.features.IsEnabled(CoreFeatureTailCall),
		TypedTailCalls:          c.features.IsEnabled(CoreFeatureTailCall),
		MultiMemory:             c.features.IsEnabled(CoreFeatureMultiMemory),
		Memory64:                c.features.IsEnabled(CoreFeatureMemory64),
		Table64:                 c.features.IsEnabled(CoreFeatureTable64),
		Threads:                 c.features.IsEnabled(CoreFeatureThreads),
		ExceptionHandling:       c.features.IsEnabled(CoreFeatureExceptionHandling),
		ExceptionReferences:     c.features.IsEnabled(CoreFeatureExceptionHandling),
		NullReferenceProducts:   c.features.IsEnabled(CoreFeatureGC),
		StructuralTypeProducts:  c.features.IsEnabled(CoreFeatureGC),
		GCTypeSubtypingProducts: c.features.IsEnabled(CoreFeatureGC),
		GCStructProducts:        c.features.IsEnabled(CoreFeatureGC),
		GCArrayProducts:         c.features.IsEnabled(CoreFeatureGC),
		GCI31Products:           c.features.IsEnabled(CoreFeatureGC),
		SIMD:                    simd,
		ExtendedConst:           c.features.IsEnabled(CoreFeatureExtendedConst) || c.features.IsEnabled(CoreFeatureExtendedConstExpressions),
		ExtendedConstGlobals:    c.features.IsEnabled(CoreFeatureExtendedConstExpressions),
	}
}

// Validate reports whether this build can honor the configuration, returning a
// *UnsupportedFeatureError or ErrGuardPageUnavailable otherwise. Compile and
// CompileWithConfig call it, so calling it yourself is optional — useful for
// surfacing a bad config early (e.g. at startup). A feature flag is never a
// silent no-op.
func (c *RuntimeConfig) Validate() error {
	if c.maxFunctionLocals == 0 || c.maxFunctionLocals > MaxFunctionLocalsLimit {
		return fmt.Errorf("wago: max function locals must be between 1 and %d, got %d", MaxFunctionLocalsLimit, c.maxFunctionLocals)
	}
	if c.maxMemoriesPerModule == 0 || c.maxMemoriesPerModule > MaxMemoriesPerModuleLimit {
		return fmt.Errorf("wago: max memories per module must be between 1 and %d, got %d", MaxMemoriesPerModuleLimit, c.maxMemoriesPerModule)
	}
	if c.functionWorkers < 0 {
		return fmt.Errorf("wago: function workers must be non-negative, got %d", c.functionWorkers)
	}
	if c.nativeStackBytes < MinNativeStackBytes || c.nativeStackBytes > MaxNativeStackBytes {
		return fmt.Errorf("wago: native stack bytes must be between %d and %d, got %d", MinNativeStackBytes, MaxNativeStackBytes, c.nativeStackBytes)
	}
	if c.nativeStackBytes&15 != 0 {
		return fmt.Errorf("wago: native stack bytes must be 16-byte aligned, got %d", c.nativeStackBytes)
	}
	if enabled, present := c.optimizations["stack-fence"]; present && !enabled {
		return fmt.Errorf("wago: stack-fence is required for bounded native execution")
	}
	if !c.trustedOptimizations {
		for name := range c.optimizations {
			if !optimization.Exists(runtime.GOARCH, name) {
				return fmt.Errorf("wago: unknown %s optimization %q", runtime.GOARCH, name)
			}
		}
	}
	if c.optimizations["bmi2-rorx"] && !hostSupportsBMI2() {
		return fmt.Errorf("wago: bmi2-rorx optimization requires BMI2 CPU support")
	}
	// SIMD remains configurable on builds whose host CPU cannot execute it so
	// scalar modules still compile under the default config; the frontend clears
	// SIMD admission for those modules. Architecture-incomplete Core 3 families,
	// in contrast, fail here before decoding or lowering.
	supported := platformCoreFeatures()
	if unsupported := c.features &^ supported; unsupported != 0 {
		return &UnsupportedFeatureError{
			Requested: unsupported,
			Supported: supported,
			Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		}
	}
	if c.boundsChecks == BoundsChecksSignalsBased && !guardPageBuilt {
		return &GuardPageUnavailableError{}
	}
	return nil
}
