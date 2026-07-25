# Wasmtime core regression fixtures

This directory contains portable WebAssembly core tests adapted from Wasmtime's
`tests/misc_testsuite` corpus.

- upstream: `github.com/bytecodealliance/wasmtime`
- revision: `a5720e50d5ec9eab34eed690eee952abfdd0e3ba`
- revision date: 2026-07-24
- license: Apache License 2.0 with LLVM exceptions; the upstream license is
  preserved in `testdata/wasmtime/LICENSE`

`MANIFEST.tsv` is the exact applicability and port-mode ledger. The current port
contains 104 upstream tests:

- 54 feature-focused WebAssembly 2.0 regressions covering sign extension,
  non-trapping conversions, multi-value, reference types, bulk memory, and SIMD;
- 41 general compiler/runtime regressions covering control flow, traps, calls,
  wide signatures, numeric lowering, stack exhaustion, Winch regressions, and
  historical issues;
- 4 Emscripten/Embenchen compile-and-link workloads;
- 1 malformed-binary validation regression;
- 2 concurrent instance-lifecycle and memory-reuse regressions adapted from
  Wasmtime's pooling tests to Wago's runtime model; and
- 2 post-WebAssembly-2 branch-hinting regressions, including Wago's intentional
  strict rejection of malformed structured metadata.

The 97 `wast-json` entries preserve the upstream WAST source and checked-in WABT
1.0.41 JSON/wasm conversion. The three `direct-go` entries use exact Go
assertions where WABT cannot serialize the upstream oracle or annotation. The
two `direct-invalid` entries preserve malformed binaries, and
`direct-concurrency` translates WAST thread commands into Go goroutines while
retaining the original source and exact Wasm modules.

The Embenchen sources carry a prominent modification notice: Wago's standard
WAST replay requires the named `$env` provider to be explicitly registered.
No workload module was otherwise changed. Direct Go execution tests additionally
run all four `_main` exports through a bounded legacy Emscripten host shim and
verify exact output digests.

The fixture tree contains 364 upstream or mechanically generated artifacts:
104 WAST sources, 97 JSON command files, and 163 wasm modules. The replay-order
path-and-content SHA-256 digest is
`b3bbb1072672801a3ce89a12deb953b10a2fe4a2b690f67b9de54489f7c6b243`.
The Go replay and direct assertions live in
`src/wago/wasmtime_core_port_test.go`. Portable Rust API, compiler, lifecycle,
trap, and workload adaptations live in the other
`src/wago/wasmtime_*_port_test.go` files.

See `testdata/wasmtime/EXCLUSIONS.md` for the small set of Wasmtime-specific
files that cannot preserve their upstream oracle in Wago.
