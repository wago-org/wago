package wago

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

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
	// validated by WithCoreFeatures. Core 3 features are opt-in so existing users
	// retain the Release 2-compatible default behavior.
	coreFeaturesWago = CoreFeaturesV3

	defaultCoreFeatures = CoreFeatureMutableGlobal |
		CoreFeatureSignExtensionOps |
		CoreFeatureMultiValue |
		CoreFeatureBulkMemoryOperations |
		CoreFeatureNonTrappingFloatToIntConversion |
		CoreFeatureReferenceTypes |
		CoreFeatureSIMD |
		CoreFeatureExtendedConstExpressions
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
	if len(names) == 0 {
		return "none"
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
	{CoreFeatureExtendedConstExpressions, "extended-const-expressions"},
	{CoreFeatureTypedFunctionReferences, "typed-function-references"},
	{CoreFeatureGC, "gc"},
	{CoreFeatureExceptionHandling, "exception-handling"},
	{CoreFeatureMultiMemory, "multi-memory"},
	{CoreFeatureMemory64, "memory64"},
	{CoreFeatureTable64, "table64"},
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
	features        CoreFeatures
	maxMemoryPages  uint32
	boundsChecks    BoundsCheckMode
	noDeferBounds   bool // disable skipping of provably-redundant bounds checks (default: enabled)
	functionWorkers int  // function validation/codegen: 0 adaptive; 1 serial; >1 forced maximum
}

const defaultMaxMemoryPages = 1 << 16 // 4 GiB worth of 64 KiB wasm pages

// NewRuntimeConfig returns the default configuration: wago's supported feature
// set, serial function validation/codegen, and the fastest available bounds-check
// mode — signals-based (guard-page) when built with -tags wago_guardpage,
// explicit otherwise. WAGO_BOUNDS overrides either way ("explicit" / "signals").
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
	return &RuntimeConfig{
		features:        defaultCoreFeatures,
		maxMemoryPages:  defaultMaxMemoryPages,
		boundsChecks:    bounds,
		functionWorkers: 1,
	}
}

// WithCoreFeatures sets the accepted WebAssembly feature set. Validated on use.
func (c *RuntimeConfig) WithCoreFeatures(features CoreFeatures) *RuntimeConfig {
	n := *c
	n.features = features
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

// WithDeferBoundsChecks controls whether the compiler skips a bounds check that a
// prior check in the same straight-line region already proved safe (explicit mode
// only; guard-page mode has no inline checks). On by default — pass false to bounds-
// check every memory access, e.g. for A/B testing or maximal defensiveness. The
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

// WithCompileWorkers is retained for source compatibility.
// Deprecated: use WithFunctionWorkers.
func (c *RuntimeConfig) WithCompileWorkers(workers int) *RuntimeConfig {
	return c.WithFunctionWorkers(workers)
}

// CoreFeatures reports the configured feature set.
func (c *RuntimeConfig) CoreFeatures() CoreFeatures { return c.features }

// BoundsChecks reports the configured bounds-check mode.
func (c *RuntimeConfig) BoundsChecks() BoundsCheckMode { return c.boundsChecks }

// DeferBoundsChecks reports whether skipping of provably-redundant bounds checks
// is enabled.
func (c *RuntimeConfig) DeferBoundsChecks() bool { return !c.noDeferBounds }

// MemoryLimitPages reports the configured maximum linear-memory size in pages.
func (c *RuntimeConfig) MemoryLimitPages() uint32 { return c.maxMemoryPages }

// FunctionWorkers reports the configured function-pipeline worker policy: zero
// adaptive, one serial, or a positive forced maximum.
func (c *RuntimeConfig) FunctionWorkers() int { return c.functionWorkers }

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
	return fmt.Sprintf("RuntimeConfig{features: %s, bounds: %s, maxMemoryPages: %d, functionWorkers: %d}",
		c.features, c.boundsChecks, c.maxMemoryPages, c.functionWorkers)
}

// SupportedFeatures reports the WebAssembly feature set this wago build can
// compile. Intersect a desired set with it to stay portable:
//
//	feats := want & wago.SupportedFeatures()
func SupportedFeatures() CoreFeatures {
	if !hostSupportsSIMD() {
		return coreFeaturesWago &^ CoreFeatureSIMD
	}
	return coreFeaturesWago
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
		// and SSSE3/SSE4.1 instruction sequences: reject at compile time instead of
		// risking SIGILL at runtime. Non-SIMD modules still compile with the default
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
		ExceptionHandling:       c.features.IsEnabled(CoreFeatureExceptionHandling),
		ExceptionReferences:     c.features.IsEnabled(CoreFeatureExceptionHandling),
		NullReferenceProducts:   c.features.IsEnabled(CoreFeatureGC),
		StructuralTypeProducts:  c.features.IsEnabled(CoreFeatureGC),
		GCTypeSubtypingProducts: c.features.IsEnabled(CoreFeatureGC),
		GCStructProducts:        c.features.IsEnabled(CoreFeatureGC),
		GCArrayProducts:         c.features.IsEnabled(CoreFeatureGC),
		GCI31Products:           c.features.IsEnabled(CoreFeatureGC),
		SIMD:                    simd,
		ExtendedConst:           c.features.IsEnabled(CoreFeatureExtendedConstExpressions),
	}
}

// Validate reports whether this build can honor the configuration, returning a
// *UnsupportedFeatureError or ErrGuardPageUnavailable otherwise. Compile and
// CompileWithConfig call it, so calling it yourself is optional — useful for
// surfacing a bad config early (e.g. at startup). A feature flag is never a
// silent no-op.
func (c *RuntimeConfig) Validate() error {
	if c.functionWorkers < 0 {
		return fmt.Errorf("wago: function workers must be non-negative, got %d", c.functionWorkers)
	}
	if unsupported := c.features &^ coreFeaturesWago; unsupported != 0 {
		return &UnsupportedFeatureError{
			Requested: unsupported,
			Supported: coreFeaturesWago,
			Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		}
	}
	if c.boundsChecks == BoundsChecksSignalsBased && !guardPageBuilt {
		return &GuardPageUnavailableError{}
	}
	return nil
}
