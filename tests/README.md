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

## AMD64 worker reset

`TestWorkerScratchMatchesFresh` compares each function with a fresh worker after
14 fixed predecessor sequences. Its generated Go fixtures need no WABT or
external files. The matrix covers shared scalar and fallback lowering, large
stacks and locals, branch tables, calls, traps, memory32/memory64, and both
native code-size policies. Exact code and metadata checks run before native
execution. Modern-CPU code runs only when the host supports its features.

Run the focused checks with:

```sh
go test -tags=wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run 'TestWorkerScratch|TestWorkerModuleStatsComparison'
go test -tags=wago_profile,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run TestWorkerScratch
python3 tests/scripts/check-worker-reset-control.py
```

The control script uses a temporary Go overlay to omit the CPU-feature reset.
It requires the comparison to reject stale metadata before the first native
call. It changes no checkout file. The profile build checks source and unwind
metadata on fallback lowering; it does not qualify the shared scalar path.
GC/EH roots and native ARM64 are outside this matrix.

`TestWorkerScratchMatchesFreshAfterError` checks reuse after a controlled backend
error. Failed code is never executed. `BenchmarkWorkerScratchReuse` compares
existing reuse with a fresh worker per function and reports allocations and
tracked retained scalar/node/control scratch. These counters do not include
all worker heap storage. They measure existing behavior, not a new speed gain.

The serial/parallel statistics comparison excludes scalar admission timing and
worker-lifetime scratch counters, just as it excludes node/control counters.
Code, source metadata, frame data, and compiler path remain part of the check.
