# Regression fixtures

These WebAssembly binaries are Wago's pinned runtime and validation regression
corpus. They run through the ordinary package tests.

## Provenance

- upstream: `github.com/tetratelabs/wazero`
- revision: `236c2458ed22010150de76c5397eca2c89af3b4f`
- source directories:
  - `internal/integration_test/fuzzcases/testdata`
  - `internal/integration_test/engine/testdata`
  - `internal/integration_test/spectest/extended-const/testdata`
  - `internal/integration_test/spectest/{exception-handling,tail-call,threads,typed-function-references}/testdata`
- license: Apache License 2.0; the upstream license is preserved in
  `tests/regressions/LICENSE`

The corpus includes 71 fuzz binaries, 23 engine binaries, all 63 generated
extended-constant-expression artifacts, and all 782 generated artifacts from
the four unsupported proposal suites above. The 939 upstream artifacts (excluding
this README and the copied license) are pinned by SHA-256 digest
`910700035d51ffc50d380261168120f8d97ef4f0fb42e9c6dfe0824a79b8037a`.
Corresponding assertions live in `src/wago` and `src/core/compiler/wasm`.
See `tests/README.md` for the test surface. All 29 proposal JSON command streams
are replayed in order: applicable typed-function-reference actions run against
live instances, while commands targeting explicit proposal boundaries are
counted and required to remain fail-closed rather than skipped. One proposal-era
`type-equivalence` invalid assertion is separately pinned as superseded: its
compact singleton type is recursive under the standardized rectype grammar and
therefore validates under Wago's current-spec frontend.

`runtime/` contains a second pinned source corpus with 104 runtime/compiler
regressions and its own provenance, artifact digest, import tool, and license.
