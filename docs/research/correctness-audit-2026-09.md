# Correctness audit — 2026-09-03

## Scope

This audit checked the decoder, validator, compiler admission, compiled-artifact
codec, runtime boundaries, command-line invocation, version management, and
semantic-version comparison. The final patch is based on `origin/main` commit
`779e5e65842359c1c7b169f1af299097853a71ad`.

The audit favored strict rejection of malformed modules. It did not widen the
general Core 3 product. The only feature-boundary correction is for the existing
experimental threads product: atomic instructions remain valid with one
unshared memory, as required by the threads validation rules, while atomic wait
still traps at runtime because it requires shared memory.

## Reproduced defects

The audit found and fixed these independent defects:

- Validation accepted numeric operands for `ref.is_null`.
- GC `array.new_data` and `array.init_data` did not always require a DataCount
  section in decoded, byte-backed, and AST validation paths.
- Packed struct and array field access accepted invalid signedness variants,
  including atomic forms.
- `ref.cast` admitted provably disjoint reference types.
- `br_on_cast` and `br_on_cast_fail` did not preserve the target label prefix
  correctly on fallthrough.
- Atomic operations were rejected for unshared memory even when their exact
  alignment and other validation rules were valid.
- Bottom reference types, MVP `funcref` tables and elements, compact block and
  select types, and exact element expressions could report the wrong required
  feature set.
- Deferred i31 element execution and exact null/i31 element initializers could
  lose required metadata across an artifact round trip.
- Integer add, subtract, and multiply were incomplete in GC constant-expression
  validation and evaluation.
- Nil instance invocation, nil host re-entry targets, and typed-nil memory and
  global imports could panic or cross a runtime boundary incorrectly.
- Memory close could race with instantiation. Negative, over-limit, and
  non-page-aligned guarded memory sizes were not rejected at the narrowest
  constructor boundary.
- Disabling prepared calls could retain stale trap state for the next invoke.
- CLI invocation could reach scalar formatting with a `v128` or reference
  signature and panic instead of returning a clear unsupported-signature error.
- Version names could escape the managed directory or use invalid Windows path
  forms. Cache-prune day conversion could overflow.
- Very large numeric prerelease identifiers could compare incorrectly after
  integer overflow, both in the core semver package and manager ordering.

Each defect has a focused regression test. Unshared atomic wait/notify coverage
includes local, exported, and imported memory.

## Independent oracles and suites

The validator audit used the pinned official corpora and independent tools:

- Core 2: 1,600 modules and 2,880 validation assertions passed.
- Core 3 explicit and signal-backed products: 2,226 modules and 58,038
  assertions passed with zero gaps.
- Exhaustive opcode-shape comparison with `wasm-tools`: zero mismatches.
- Differential execution: 1.6 million executions without a semantic mismatch.
- The regression, semantic, WAST, Wasmtime Core 3, and generated smith cases
  passed.

The final upstream-based tree also passed:

- normal tests for the changed compiler, core runtime, semver, public runtime,
  CLI, and version-manager packages;
- guard-page tests for `src/core/runtime` and `src/wago`;
- race tests for the changed compiler/core packages and focused public-runtime
  lifecycle, import, feature, artifact, prepared-call, and atomic boundaries;
- `go vet`, generated-file synchronization, formatting, documentation links,
  and cross-builds for Linux, macOS, and Windows on amd64 and arm64.

The broad local `go test ./...` run also exposed environment-only failures:
Wine could not create its lock under the restricted runtime directory, and the
installed TinyGo requires Go 1.25 or newer while this audit used Go 1.22.12.
These failures reproduce on the unchanged base and are outside this patch.

## Performance and footprint

Measurements used Go 1.22.12, `GOMAXPROCS=1`, repeated samples, and the current
`origin/main` tree as the baseline. Direction was reversed where ordering noise
was visible. Values are median deltas:

| Benchmark | Time delta | Allocation result |
|---|---:|---:|
| Decode and validate | +0.94% | unchanged: 46,942 B, 358 allocs |
| Invoke one scalar function | -0.05% | unchanged: 0 B, 0 allocs |
| Instantiate a small module | +0.73% | unchanged: 1,488 B, 10 allocs |
| Instantiate shared memory | +1.20% | unchanged: 2,368 B, 15 allocs |
| Analyze module requirements | +1.28% | unchanged: 2,296 B, 3 allocs |
| Scan a scalar function body | +0.18% | unchanged: 0 B, 0 allocs |

No measured path has a meaningful regression. No measured path added an
allocation.
