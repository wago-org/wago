// Package wago compiles and runs WebAssembly modules with a pure-Go,
// no-cgo single-pass JIT.
//
// # Quickstart
//
// Compile a module, instantiate it, and invoke an export:
//
//	mod, err := wago.Compile(wasmBytes)
//	inst, err := wago.Instantiate(mod, nil)
//	defer inst.Close()
//	out, err := inst.Invoke("add", wago.I32(2), wago.I32(3)) // args are uint64
//	fmt.Println(wago.AsI32(out[0]))
//
// # Configuration
//
// RuntimeConfig tunes compilation, modeled on wazero's config: it is immutable,
// so every WithXxx returns a copy and a base config can be shared safely. Compile
// under a config with the fluent Compile method or CompileWithConfig:
//
//	cfg := wago.NewRuntimeConfig().
//		WithFeature(wago.CoreFeatureBulkMemoryOperations, false) // reject memory.copy/fill
//	mod, err := cfg.Compile(wasmBytes)
//
// CoreFeatures gates which WebAssembly proposals are accepted; enabling one this
// build cannot lower fails fast with an *UnsupportedFeatureError rather than
// mis-running. SupportedFeatures reports the build's capabilities for portable
// programs.
//
// # Guard-page bounds checks
//
// WithBoundsChecks selects how out-of-bounds memory accesses are caught. The
// default, BoundsChecksExplicit, emits an inline check per access.
// BoundsChecksSignalsBased instead elides the checks and relies on a guard-page
// mapping plus a signal handler (≈25% faster on memory-heavy code); it requires a
// binary built with -tags wago_guardpage and is reported by GuardPageSupported:
//
//	cfg := wago.NewRuntimeConfig()
//	if wago.GuardPageSupported() {
//		cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
//	}
//	mod, err := cfg.Compile(wasmBytes) // GuardPageUnavailableError if unsupported
//
// See docs/guardpage-spike.md for the mechanism and its limitations.
package wago
