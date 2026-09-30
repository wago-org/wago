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
// RuntimeConfig tunes compilation and is immutable, so every WithXxx returns a
// copy and a base config can be shared safely. Compile
// under a config with the fluent Compile method, CompileWithConfig, or
// Compile(cfg, wasmBytes):
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
// default is the fastest mode available in the current binary:
// BoundsChecksSignalsBased when built with -tags wago_guardpage, otherwise
// BoundsChecksExplicit. Signals-based mode elides eligible memory-0 memory32
// checks and relies on a guard-page mapping plus a signal handler; indexed
// nonzero memories and memory64 retain explicit checks. GuardPageSupported reports whether
// the current binary can use it:
//
//	cfg := wago.NewRuntimeConfig()
//	mod, err := cfg.Compile(wasmBytes)
//
// Benchmarks and other default-config entry points can be pinned with WAGO_BOUNDS=explicit
// or WAGO_BOUNDS=signals.
package wago
