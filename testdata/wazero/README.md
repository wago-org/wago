# wazero regression fixtures

This directory contains WebAssembly binaries ported from the wazero test suite:

- upstream: `github.com/tetratelabs/wazero`
- revision: `236c2458ed22010150de76c5397eca2c89af3b4f`
- source directories:
  - `internal/integration_test/fuzzcases/testdata`
  - `internal/integration_test/engine/testdata`
  - `internal/integration_test/spectest/extended-const/testdata`
  - `internal/integration_test/spectest/{exception-handling,tail-call,threads,typed-function-references}/testdata`
- license: Apache License 2.0; the upstream license is preserved in
  `testdata/wazero/LICENSE`

The corpus includes 71 fuzz binaries, 23 engine binaries, all 63 generated
extended-constant-expression artifacts, and all 782 generated artifacts from
the four unsupported proposal suites above. The 939 upstream artifacts (excluding
this README and the copied license) are pinned by SHA-256 digest
`910700035d51ffc50d380261168120f8d97ef4f0fb42e9c6dfe0824a79b8037a`.
Corresponding assertions are
adapted in `src/wago/wazero_*_port_test.go` and
`src/core/compiler/wasm/wazero_*_port_test.go`. Unsupported features remain
complete, manifest-checked fail-closed corpora rather than skips.
