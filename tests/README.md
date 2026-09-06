# Tests

Use this directory to find shared test tools, fixtures, and corpora. Most Go
test files stay beside the package they test. This directory contains the data
and helpers that several packages share.

## Start Here

From the repository root, use these commands:

```sh
make test         # normal repository test surface
make test-corpus  # semantic-program corpus
make spec2        # strict WebAssembly 2.0 conformance gate
```

`make test` includes Wago-owned behavior tests and the regression corpus. It
does not have a separate compatibility suite. The Core 2.0 wrappers skip when
WABT or `tests/spec-v2` is missing. `make spec2` runs the same wrappers as a
strict, non-skippable gate over 147 WAST files.

## Directory Guide

`tests` contains repository-wide test infrastructure and data:

- `fixtures/wasm`: small checked-in modules used by examples, benchmarks, and
  CLI smoke tests;
- `fixtures/bench`: benchmark-specific modules and their source text;
- `fixtures/spec-card`: parser fixtures for the CI specification report;
- `regressions`: pinned runtime, ABI, validation, and unsupported-proposal
  regression corpora, including provenance and maintenance ledgers;
- `regressioncorpus` and `regressiontest`: shared corpus validation and isolated
  execution helpers;
- `scripts`: shell-level command and release integration tests;
- `spectest`: shared helpers for discovering and accounting official spec suites;
- `wasmtest`: shared builders for compact programmatic WebAssembly fixtures;
- `tools`: corpus import and verification commands;
- `spec` and `spec-v2`: pinned official specification submodules.

Go `_test.go` files remain beside the package they exercise. Compiler and
runtime tests often need unexported implementation details. In Go, files in a
different directory are a different package, even when they have the same
package clause.

## Pinned Regression Corpora

The base regression tree contains 939 artifacts pinned by SHA-256 digest
`910700035d51ffc50d380261168120f8d97ef4f0fb42e9c6dfe0824a79b8037a`.
Unsupported proposals are manifest-checked fail-closed cases, not skips. Source
provenance and the copied license are recorded in `regressions/README.md`.
The runtime subcorpus adds 104 source cases replayed across 364 checked-in
artifacts; see `regressions/runtime/README.md`.
