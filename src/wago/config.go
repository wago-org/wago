package wago

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago/src/core/compiler/backend/amd64"
	"github.com/wago-org/wago/src/core/compiler/frontend"
)

// CoreFeatures is a bit set of WebAssembly Core specification features, modeled
// on wazero's api.CoreFeatures. A RuntimeConfig carries the set it will accept;
// modules using a disabled feature are rejected at compile time.
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
	// CoreFeatureReferenceTypes: funcref/externref, table.* and ref ops.
	CoreFeatureReferenceTypes
	// CoreFeatureSignExtensionOps: i32/i64.extend{8,16,32}_s.
	CoreFeatureSignExtensionOps
	// CoreFeatureSIMD: the v128 vector instructions.
	CoreFeatureSIMD
	// CoreFeatureTailCall: return_call / return_call_indirect.
	CoreFeatureTailCall
)

// Feature groups mirroring wazero's CoreFeaturesV1 / CoreFeaturesV2.
const (
	// CoreFeaturesV1 is the WebAssembly 1.0 (MVP) feature set.
	CoreFeaturesV1 = CoreFeatureMutableGlobal
	// CoreFeaturesV2 is the WebAssembly 2.0 feature set.
	CoreFeaturesV2 = CoreFeaturesV1 |
		CoreFeatureBulkMemoryOperations |
		CoreFeatureMultiValue |
		CoreFeatureNonTrappingFloatToIntConversion |
		CoreFeatureReferenceTypes |
		CoreFeatureSignExtensionOps

	// coreFeaturesWago is the optional set wago's single-pass backend lowers
	// today; it is the default and the ceiling WithCoreFeatures is validated
	// against. Bulk-memory here means the supported subset (memory.copy/fill).
	// Multi-value, reference-types, SIMD, tail-call, and the non-trapping
	// float-to-int conversions are not yet wired, so enabling them is rejected up
	// front rather than silently mis-running.
	coreFeaturesWago = CoreFeatureMutableGlobal |
		CoreFeatureSignExtensionOps |
		CoreFeatureBulkMemoryOperations
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
	for _, e := range featureNames {
		if f.IsEnabled(e.bit) {
			names = append(names, e.name)
		}
	}
	return strings.Join(names, "|")
}

var featureNames = []struct {
	bit  CoreFeatures
	name string
}{
	{CoreFeatureBulkMemoryOperations, "bulk-memory-operations"},
	{CoreFeatureMultiValue, "multi-value"},
	{CoreFeatureMutableGlobal, "mutable-global"},
	{CoreFeatureNonTrappingFloatToIntConversion, "nontrapping-float-to-int-conversion"},
	{CoreFeatureReferenceTypes, "reference-types"},
	{CoreFeatureSignExtensionOps, "sign-extension-ops"},
	{CoreFeatureSIMD, "simd"},
	{CoreFeatureTailCall, "tail-call"},
}

// BoundsCheckMode selects how out-of-bounds linear-memory accesses are caught.
// This is a wago-specific extension (wazero only does explicit checks).
type BoundsCheckMode int

const (
	// BoundsChecksExplicit emits an inline bounds check on every access. The
	// default; needs no signal handler.
	BoundsChecksExplicit BoundsCheckMode = iota
	// BoundsChecksSignalsBased elides the inline check and relies on a guard-page
	// mapping plus a SIGSEGV/SIGBUS handler (see docs/guardpage-spike.md). Faster
	// on memory-heavy code, but installs process-wide signal handlers and requires
	// a binary built with the `wago_guardpage` tag.
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

// RuntimeConfig configures compilation and execution. Modeled on wazero's
// RuntimeConfig: it is immutable — every WithXxx returns a copy, so a base
// config can be shared and specialised safely. wago-specific knobs (e.g.
// WithBoundsChecks) extend the wazero-style surface.
type RuntimeConfig struct {
	features       CoreFeatures
	maxMemoryPages uint32
	boundsChecks   BoundsCheckMode
}

const defaultMaxMemoryPages = 1 << 16 // 4 GiB worth of 64 KiB wasm pages

// NewRuntimeConfig returns the default configuration: wago's supported feature
// set and explicit bounds checks.
func NewRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		features:       coreFeaturesWago,
		maxMemoryPages: defaultMaxMemoryPages,
		boundsChecks:   BoundsChecksExplicit,
	}
}

// WithCoreFeatures sets the accepted WebAssembly feature set. Validated on use.
func (c *RuntimeConfig) WithCoreFeatures(features CoreFeatures) *RuntimeConfig {
	n := *c
	n.features = features
	return &n
}

// WithMemoryLimitPages caps the maximum linear-memory size in 64 KiB pages.
func (c *RuntimeConfig) WithMemoryLimitPages(pages uint32) *RuntimeConfig {
	n := *c
	n.maxMemoryPages = pages
	return &n
}

// WithBoundsChecks selects the linear-memory bounds-check strategy (wago
// extension). BoundsChecksSignalsBased requires the wago_guardpage build tag.
func (c *RuntimeConfig) WithBoundsChecks(mode BoundsCheckMode) *RuntimeConfig {
	n := *c
	n.boundsChecks = mode
	return &n
}

// CoreFeatures reports the configured feature set.
func (c *RuntimeConfig) CoreFeatures() CoreFeatures { return c.features }

// BoundsChecks reports the configured bounds-check mode.
func (c *RuntimeConfig) BoundsChecks() BoundsCheckMode { return c.boundsChecks }

// frontendFeatures maps the config's feature set onto the frontend support
// pass's gate.
func (c *RuntimeConfig) frontendFeatures() frontend.Features {
	return frontend.Features{
		SignExtension: c.features.IsEnabled(CoreFeatureSignExtensionOps),
		BulkMemory:    c.features.IsEnabled(CoreFeatureBulkMemoryOperations),
	}
}

// compileOptions maps the config onto backend code-generation options.
func (c *RuntimeConfig) compileOptions() amd64.CompileOptions {
	return amd64.CompileOptions{ElideBoundsChecks: c.boundsChecks == BoundsChecksSignalsBased}
}

// validate rejects configurations this build cannot honor, so a feature flag is
// never a silent no-op.
func (c *RuntimeConfig) validate() error {
	if unsupported := c.features &^ coreFeaturesWago; unsupported != 0 {
		return fmt.Errorf("config: feature(s) %q not supported by this wago build", unsupported)
	}
	if c.boundsChecks == BoundsChecksSignalsBased && !guardPageBuilt {
		return fmt.Errorf("config: signals-based bounds checks require a binary built with -tags wago_guardpage")
	}
	return nil
}
