# Regression fixtures

This directory contains Wago's pinned WebAssembly runtime and validation
regression corpus. Ordinary package tests replay it. For the overall test
layout, see [tests/README.md](../README.md).

Do not treat these binaries as loose examples. Their source, license, and digest
are part of the regression contract.

## Base Corpus Provenance

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
extended-constant-expression artifacts, and all 782 generated artifacts from the
four unsupported proposal suites above. The 939 upstream artifacts, excluding
this README and the copied license, are pinned by SHA-256 digest
`910700035d51ffc50d380261168120f8d97ef4f0fb42e9c6dfe0824a79b8037a`.

The corresponding assertions are in `src/wago` and `src/core/compiler/wasm`.
All 29 proposal JSON command streams replay in order. Applicable
typed-function-reference actions run against live instances. Commands at
explicit proposal boundaries are counted and must remain fail-closed; they are
not skips.

One proposal-era `type-equivalence` invalid assertion is pinned as superseded.
Its compact singleton type is recursive under the standardized rectype grammar,
so Wago's current-spec frontend correctly validates it.

## Runtime Corpus

`runtime/` is a second pinned source corpus. It has 104 WebAssembly 2/general
runtime/compiler regressions plus its own provenance, artifact digest, import
tool, and license. See [runtime/README.md](runtime/README.md) for its test and
maintenance rules.

## WebAssembly Core 3 Corpus

`wasmtime-core3/` contains 103 exact Core 3 fixtures pinned to Wasmtime revision
`e8ac8c27f19939bfb1d26d920368d8b6028a67a9`. It replays 215 module instances and
690 execution assertions with explicit `CoreFeaturesV3` admission. Five more
upstream files are preserved and provenance-mapped to equivalent Wago product
tests.

`RUNTIME_REUSE.tsv` proves the 104 reused general-runtime rows path by path:
103 sources are byte-identical; one has a non-normative diagnostic-text change
with unchanged malformed module bytes. No applicable Core 3 inventory entry is
pending.
