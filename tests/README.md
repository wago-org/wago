# Tests

`tests` contains repository-wide test infrastructure and data:

- `fixtures/wasm`: small checked-in modules used by examples, benchmarks, and
  CLI smoke tests;
- `fixtures/bench`: benchmark-specific modules and their source text;
- `fixtures/spec-card`: parser fixtures for the CI specification report;
- `regressions`: pinned runtime, ABI, validation, and unsupported-proposal
  regression binaries;
- `scripts`: shell-level command and release integration tests;
- `spectest`: shared helpers for discovering and accounting official spec suites;
- `wasmtest`: shared builders for compact programmatic WebAssembly fixtures;
- `spec` and `spec-v2`: pinned official specification submodules.

Go `_test.go` files remain beside the package they exercise. Many compiler and
runtime tests intentionally use unexported implementation details, and Go treats
files in a different directory as a different package even when their package
clause has the same name.

## Running tests

```sh
make test
make test-corpus
make spec2
```

`make test` is the unified Go surface. It includes Wago-owned behavior tests and
the regression corpus; there is no separate compatibility suite. The Core v2
wrappers skip when WABT or `tests/spec-v2` is unavailable, while `make spec2`
runs them as a strict, non-skippable CI gate over 147 WAST files.

The regression tree contains 939 artifacts pinned by SHA-256 digest
`910700035d51ffc50d380261168120f8d97ef4f0fb42e9c6dfe0824a79b8037a`.
Unsupported proposals are manifest-checked fail-closed cases, not skips. Source
provenance and the copied license are recorded in `regressions/README.md`.
