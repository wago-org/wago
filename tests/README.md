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

## Shared versus established compiler paths

`TestSharedEstablishedPathPairs` uses the same 22 fixture modules on AMD64 and
ARM64. A test-only adapter selects established compilation or ordinary shared
admission with the same options. Tests execute all 71 defined functions in both
modes, including the void fixture and every function in the many-function row.
They compare exact semantic result bits, the unreachable trap, ordered deferred
host events, and visible memory with fixed oracles. An optional independent Node/V8 run checks the same module bytes and
all exported functions; its absence is a separate skip, not successful coverage.

The matrix includes small and empty functions, 256/257 locals, 32/33 nested
controls, and both sides of the 16,384-instruction admission budget. Explicit
fallbacks cover indexed block/loop parameters, compatible branch-table labels,
unreachable typed blocks, discarded multi-results, FP, SIMD, and memory effects. A mixed module compares the ordered event log
from an established caller that invokes a shared-eligible callee. An i64 leaf
returns an exact value above binary64 integer precision.
Seven typed-control/result variants each violate one type premise and must stop
at authoritative validation. They are never sent to the native compiler.
The catch-free typed and unreachable `try_table` shapes reuse the #739
regression, with actual fallback evidence added here. A GC i31 construction
and extraction row checks reference fallback without collector allocation.
Existing EH/GC suites retain broader combinations; this matrix does not claim
an independent transport or collector-root proof.

Diagnostic builds require actual per-function shared/established evidence,
record frame sizes, and reject a valid established artifact labeled as shared.
The log's source-premise text explains the fixture; it is not a new production
fallback-reason field. A control omits the zero-returning function while its
untouched result buffer still looks correct; the completed-call gate rejects it.
Profile builds separately require established fallback and real source ranges,
even when shared compilation is requested. Ordinary builds leave path evidence
absent. Locally, all 44 ordinary/diagnostic source and native hash pairs matched.

`BenchmarkSharedEstablishedCompile` includes decode, validation, and codegen
with one worker and no executable mapping. `BenchmarkSharedEstablishedExecute`
times the prepared low-level call to function 0, with checks outside timing;
trap execution is excluded. Both compare matched ordinary builds and report
allocations. Native bytes, target features, explicit bounds, API, source hashes,
and loaded-code hashes are recorded. Compile/execution numbers are current-path
comparisons, not gains from a new compiler change. Native AMD64 and emulated
ARM64 passed locally; native ARM64 CI is still required. All new state is in
shared test support or test files; production admission and codegen are unchanged.
