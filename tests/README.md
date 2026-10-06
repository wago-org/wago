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


## ARM64 direct synchronous host results

`TestSyncHostResultPressureCompile` and `TestSyncHostResultPressureExecution`
cover integer, floating-point, mixed, and vector host results up to the direct
ABI's 64-slot limit, with lazy local reload enabled and disabled. Each result
is stored separately with its exact Wasm width. The test checks all bits,
scalar padding, memory guards, live integer/vector values below the results,
callback counts, and repeated calls with changed
values, including signed zero and NaN payloads. Diagnostic builds also require
one actual direct synchronous host call. Public dynamic import wrappers use a
separate lowering; these tests do not claim to qualify that path.

`TestSyncHostRestoresX11LocalAfterResults`,
`TestSyncHostRestoresX11GlobalAfterResults`, and
`TestSyncHostResultSpillsPreserveRootMetadata` inspect the emission seam. They
check explicit X11 pin restoration and reference-root metadata after spills.
They never execute partial native code. The allocator tests also prevent
floating-point or vector values from being selected as general-purpose spill
victims. This is a structural root check, not a
collector-liveness proof.

`BenchmarkSyncHostResults` measures compilation and prepared execution
separately for valid one-result calls. These signatures work on the compiler
before and after the fix. Wider signatures are correctness cases; known-bad
native output is not executed as a timing baseline. No timing threshold is
asserted. ARM64 emulation establishes only emulated behavior and timing;
native ARM64 CI and hardware measurements must be labelled separately.
