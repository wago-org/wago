package wago

import (
	"errors"
	"fmt"
	"os"
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
	// Multi-value, reference-types, SIMD, and tail-call are not yet wired, so
	// enabling them is rejected up front rather than silently mis-running.
	coreFeaturesWago = CoreFeatureMutableGlobal |
		CoreFeatureSignExtensionOps |
		CoreFeatureBulkMemoryOperations |
		CoreFeatureNonTrappingFloatToIntConversion
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
	registerCallABI bool
	useX64          bool // route codegen through the experimental backend/x64 (WARP port)
}

const defaultMaxMemoryPages = 1 << 16 // 4 GiB worth of 64 KiB wasm pages

// NewRuntimeConfig returns the default configuration: wago's supported feature
// set and explicit bounds checks.
func NewRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		features:        coreFeaturesWago,
		maxMemoryPages:  defaultMaxMemoryPages,
		boundsChecks:    BoundsChecksExplicit,
		registerCallABI: os.Getenv("WAGO_REG_ABI") != "0", // on by default; WAGO_REG_ABI=0 disables
		useX64:          os.Getenv("WAGO_X64") == "1",     // opt-in experimental WARP-port backend
	}
}

// WithX64 selects the experimental WARP-port backend (backend/x64). Returns a
// copy; the receiver is unchanged.
func (c *RuntimeConfig) WithX64(on bool) *RuntimeConfig {
	n := *c
	n.useX64 = on
	return &n
}

// WithRegisterCallABI toggles the register-based internal-call ABI (default on;
// integer-only signatures use it, others fall back to the wrapper path). Returns
// a copy; the receiver is unchanged.
func (c *RuntimeConfig) WithRegisterCallABI(on bool) *RuntimeConfig {
	n := *c
	n.registerCallABI = on
	return &n
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

// CoreFeatures reports the configured feature set.
func (c *RuntimeConfig) CoreFeatures() CoreFeatures { return c.features }

// BoundsChecks reports the configured bounds-check mode.
func (c *RuntimeConfig) BoundsChecks() BoundsCheckMode { return c.boundsChecks }

// MemoryLimitPages reports the configured maximum linear-memory size in pages.
func (c *RuntimeConfig) MemoryLimitPages() uint32 { return c.maxMemoryPages }

// Compile decodes, validates, and compiles wasmBytes under this config. It is the
// fluent form of CompileWithConfig(c, wasmBytes):
//
//	mod, err := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksSignalsBased).Compile(b)
func (c *RuntimeConfig) Compile(wasmBytes []byte) (*Compiled, error) {
	return CompileWithConfig(c, wasmBytes)
}

// MustCompile is like Compile but panics on error.
func (c *RuntimeConfig) MustCompile(wasmBytes []byte) *Compiled {
	m, err := CompileWithConfig(c, wasmBytes)
	if err != nil {
		panic("wago: MustCompile: " + err.Error())
	}
	return m
}

func (c *RuntimeConfig) String() string {
	return fmt.Sprintf("RuntimeConfig{features: %s, bounds: %s, maxMemoryPages: %d}",
		c.features, c.boundsChecks, c.maxMemoryPages)
}

// SupportedFeatures reports the WebAssembly feature set this wago build can
// compile. Intersect a desired set with it to stay portable:
//
//	feats := want & wago.SupportedFeatures()
func SupportedFeatures() CoreFeatures { return coreFeaturesWago }

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
}

func (e *UnsupportedFeatureError) Error() string {
	return fmt.Sprintf("wago: unsupported feature(s) %s; this build supports %s", e.Requested, e.Supported)
}

// frontendFeatures maps the config's feature set onto the frontend support
// pass's gate.
func (c *RuntimeConfig) frontendFeatures() frontend.Features {
	return frontend.Features{
		SignExtension:   c.features.IsEnabled(CoreFeatureSignExtensionOps),
		BulkMemory:      c.features.IsEnabled(CoreFeatureBulkMemoryOperations),
		SaturatingTrunc: c.features.IsEnabled(CoreFeatureNonTrappingFloatToIntConversion),
	}
}

// compileOptions maps the config onto backend code-generation options.
func (c *RuntimeConfig) compileOptions() amd64.CompileOptions {
	return amd64.CompileOptions{
		ElideBoundsChecks: c.boundsChecks == BoundsChecksSignalsBased,
		RegisterCallABI:   c.registerCallABI,
	}
}

// Validate reports whether this build can honor the configuration, returning a
// *UnsupportedFeatureError or ErrGuardPageUnavailable otherwise. Compile and
// CompileWithConfig call it, so calling it yourself is optional — useful for
// surfacing a bad config early (e.g. at startup). A feature flag is never a
// silent no-op.
func (c *RuntimeConfig) Validate() error {
	if unsupported := c.features &^ coreFeaturesWago; unsupported != 0 {
		return &UnsupportedFeatureError{Requested: unsupported, Supported: coreFeaturesWago}
	}
	if c.boundsChecks == BoundsChecksSignalsBased && !guardPageBuilt {
		return &GuardPageUnavailableError{}
	}
	return nil
}
