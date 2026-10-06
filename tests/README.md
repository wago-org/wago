# Tests

Package-local Go tests stay beside the implementation they exercise. This
directory contains repository-level conformance, integration, fuzz, corpus,
fixture, and support code:

```text
tests/
  conformance/   pinned WebAssembly spec suites and Wasmtime Core 3 ports
  corpus/        regression artifacts with provenance ledgers
  fuzz/          reusable fuzz workers and oracles
  integration/   cross-package and CI-policy tests
  fixtures/      small purpose-built test modules
  support/       shared Go test helpers
  scripts/       shell-level integration checks
  tools/         corpus and documentation maintenance commands
```

Use `just` as the public entry point. Run `just --list test` or
`just --list test spec` to explore:

```sh
just test                         # unit/integration tests + every benchmark corpus workload
CORPUS=quick just test            # faster representative local gate
just test corpus algorithms
just test corpus tag:polybench
just test corpus all              # every curated executable workload
just test spec v1                 # one pinned spec version
just test spec                    # all pinned spec versions
just test fuzz 30s                # bounded fuzzing gates
just test all                     # all of the above
```

The runtime benchmark corpus is repository-level data in `corpus/`; it is not
duplicated under tests. Regression artifacts remain under `tests/corpus`
because they are narrow bug reproductions, not performance workloads.

## AMD64 loop-boundary regression checks

`src/wago/loop_boundary_*test.go` keeps the #817 experiment's useful
checks: arithmetic and trap oracles, live stack/register pressure, native
producer placement, and compile/prepared-call benchmarks. It tests zero, one,
and many iterations, conditional entry, aliases, constants, subnormal values,
signed zeros, infinities and NaNs. No additional eager-flush optimization was
retained: the measured candidate emitted identical native code in all nine
fixtures and had no significant timing improvement on the tested AMD CPU.

The native check reads instruction addresses and direct backedge targets in
GNU objdump output, for both SSE2 and modern encodings. Its deliberately
inside-loop producer has the same multiply count as the pre-loop producer;
the observer must distinguish their placement. This is a bounded single-loop
check, not a general native control-flow analyzer. With `wago_codegenstats`, it
also verifies the established compiler path and actual allocator spills in the
register-pressure fixture.

```sh
go test -tags=wago_regalloccheck,wago_codegenstats ./src/wago -run '^TestLoopBoundary'
go test ./src/wago -run '^$' -bench '^BenchmarkLoopBoundary' -benchmem
```

These tests use the existing WABT helper and skip when `wat2wasm` is unavailable;
native placement additionally requires GNU `objdump` or `gobjdump` and skips
when only LLVM objdump is available. WAT assembly and guest
execution are excluded from the compile benchmark; setup and compilation are
excluded from the prepared-call benchmark. No wall-clock threshold is asserted.
Native ARM64 and admitted memory-region/shared-compiler loops remain outside
this coverage.


## Semantic result and execution identity controls

`TestSemanticProfilesAndExecutionIdentity` uses the existing spec command runner
and isolated regression process. Twelve deterministic cases define two bounded
profiles: `core-exact-and-arithmetic-nan-v1` and `relaxed-swizzle-v1`. They cover
integer widths, signed zero, exact signaling-NaN copies, full-vector lane order,
operation-specific arithmetic/canonical NaN classes, an unreachable trap, and
the pinned Core 3 relaxed-swizzle case with indices 16–31. The latter accepts
the permitted TBL/PSHUFB alternatives and separately checks Wago's target choice.
This does not generalize relaxed allowances to ordinary SIMD or NaN copies.

The optional observer hashes the Wasm bytes passed to compilation and the actual
executable mapping used by the instance. It records the test-binary SHA-256,
Go/OS/architecture, selected/required CPU mask, explicit bounds mode, public
invocation API, and result-profile versions. Diagnostic builds recompile with
the same options, require exact loaded-code agreement, and report each actual
shared/established function path. Ordinary builds leave path evidence absent;
an absent report is inconclusive, not proof of shared admission.

A schedule prepared before execution requires one terminal record per case.
Counts distinguish requested, executed, excluded, and missing cases. Return and
trap records retain raw slots before comparison and before the next invocation
expires them. Rejection, unsupported feature, limit, timeout, host failure, and
mismatch statuses cannot count as successful semantic coverage. Unit controls
check status classification; a compile-rejection wrapper checks real rejection
accounting. The supported positive lane must execute every scheduled case.

Two independent wrapper controls silently omit a supported action or load a
valid module with an extra custom section. The substitute returns the same
values and can have identical native code. The scheduled-case/source-identity
gate must still reject both controls for the intended reason. Wrong signed-zero,
width, lane-order, NaN-copy, NaN-class, and relaxed-result controls exercise the
existing comparison predicates. Deliberately faulty native code is never run.

The observer is optional and confined to `_test.go` files. Retained records are
bounded to 257, with at most two raw slots per record and bounded error text.
`BenchmarkSemanticResultObservation` compares the existing action path with and
without raw-result retention; compile, load, hashing, and gate evaluation are
outside that timing. There is no production performance or allocation change.
Native AMD64 and emulated ARM64 passed locally; native ARM64 is required in CI.
The profiles cover these named operations, not every relaxed-SIMD operation or
all corpus engine adapters. Wider adoption remains part of #819.
