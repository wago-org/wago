package wago

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/optimization"
)

// IsEnabled returns true if all bits in feature are set.
func (f CoreFeatures) IsEnabled(feature CoreFeatures) bool { return f&feature == feature }

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

// SupportedFeatures reports the WebAssembly feature set this wago build can
// compile. Intersect a desired set with it to stay portable:
//
//	feats := want & wago.SupportedFeatures()
func supportsCompleteCore3Backend(goos, goarch string) bool {
	return (goos == "linux" && (goarch == "amd64" || goarch == "riscv64")) ||
		(goarch == "arm64" && (goos == "linux" || goos == "darwin"))
}

func supportsThreadsBackend(goos, goarch string) bool {
	return (goarch == "amd64" || goarch == "arm64") && (goos == "linux" || goos == "darwin") || goos == "linux" && goarch == "riscv64"
}

func platformCoreFeatures() CoreFeatures {
	supported := coreFeaturesWago
	if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
		// Other targets retain the portable
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

// defaultCoreFeatures admits the complete Core 3 release on backends that
// implement it. Other targets retain the portable Release 2 plus
// extended-constant surface instead of silently accepting partial support.
// The separate threads proposal remains opt-in.
func defaultCoreFeatures() CoreFeatures {
	return coreFeaturesWithoutSidecar | (CoreFeaturesV3 & platformCoreFeatures())
}

// frontendFeatures maps the config's feature set onto the frontend support
// pass's gate.
func (c *RuntimeConfig) frontendFeatures() frontend.Features {
	simd := c.features.IsEnabled(CoreFeatureSIMD)
	if simd && !hostSupportsSIMD() {
		// Do not admit SIMD modules on hosts that lack the required CPU features.
		simd = false
	}
	return frontend.Features{
		SignExtension:           c.features.IsEnabled(CoreFeatureSignExtensionOps),
		BulkMemory:              c.features.IsEnabled(CoreFeatureBulkMemoryOperations),
		SaturatingTrunc:         c.features.IsEnabled(CoreFeatureNonTrappingFloatToIntConversion),
		ReferenceTypes:          c.features.IsEnabled(CoreFeatureReferenceTypes),
		TypedFunctionReferences: c.features.IsEnabled(CoreFeatureTypedFunctionReferences) || railshotI31Frontend(c),
		TailCalls:               c.features.IsEnabled(CoreFeatureTailCall),
		TypedTailCalls:          c.features.IsEnabled(CoreFeatureTailCall),
		MultiMemory:             c.features.IsEnabled(CoreFeatureMultiMemory),
		CompactImports:          c.features.IsEnabled(CoreFeatureCompactImports),
		WideArithmetic:          c.features.IsEnabled(CoreFeatureWideArithmetic),
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
	if c.gcCodeTelemetry && !compilerTelemetryEnabled {
		return fmt.Errorf("wago: compiler telemetry requires -tags=wago_codegenstats or wago_profile")
	}
	if c.codeProfile != nil && !codeProfileEnabled {
		return fmt.Errorf("wago: profiling requires a build with -tags=wago_profile")
	}
	// Unknown modes must not silently inherit explicit-check behavior.
	if c.boundsChecks != BoundsChecksExplicit && c.boundsChecks != BoundsChecksSignalsBased {
		return fmt.Errorf("wago: invalid bounds-check mode %d", c.boundsChecks)
	}
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
	if runtime.GOARCH == "amd64" && !hostSupportsSIMD() {
		return errNativeCPUFeatures
	}
	supported := platformCoreFeatures() | railshotStagedTailFeatures(c) | railshotStagedGCFeatures(c)
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
