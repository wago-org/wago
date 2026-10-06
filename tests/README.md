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

## AMD64 straight-line bounds proofs

The backend's `bounds_cert_model_test.go` checks source identity, proof extent,
changed sources, empty slots, replacement, and replacement-cursor wrap. Its
independent map records valid proofs without copying the cache's placement
rules. A deliberately stale proof must fail this gate before native execution.
`bounds_research_test.go` checks actual bounds-check counts at the table's
capacity, the established compiler path, explicit/guard modes, disabled facts,
and trap/memory agreement after an address changes.

Run the diagnostic tests from the repository root:

```sh
go test -tags=wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run 'TestBounds(Certificates|Facts)'
```

The ordinary-build `BenchmarkBoundsCertificates`, `BenchmarkBoundsFactsCompile`,
and `BenchmarkBoundsFactsExecute` separate table operations, complete backend
compilation, and native execution. Compile inputs include 1, 2, 8, 9, and 32
independent addresses and the cjson, coremark, zstd, and wren corpus modules.
Each generated execution body reads the addresses 32 times. Guard and explicit
checks are separate rows. These tests do not qualify memory64, imported-memory
aliasing, or native ARM64.

Research issue #818 compared three alternatives against main `640548d8` on
Linux AMD64 (Ryzen 7 8845HS, Go 1.27.1, CPU 4, GOMAXPROCS=1). Seven samples used
rotating order and 100 ms per row, followed by seven 150 ms samples for the last
two alternatives. Production changes were rejected:

- A single-pass update slowed table operations by 3.7–12.9% without a clear
  full-compile gain.
- An unrolled lookup improved larger table operations but slowed the one-source
  case by 17.4%. Its initial wren compile gain did not repeat.
- A 16-entry table improved the nine-address explicit-mode fixture by 83.5% in
  compilation and 47.9% in execution. It added 96 bytes to the proof table and
  128 allocated bytes per compile in the measured fixtures. Two guard-mode
  compile controls slowed by 4.6% and 6.8%; the four corpus compile comparisons
  showed no significant explicit-mode gain. Across 121 corpus modules, it
  removed 225 of 5,653,784 explicit checks and 3,232 code bytes. All guard-mode
  code and 108 explicit-mode modules were unchanged.

These results use unadjusted pairwise comparisons on one host. They support
keeping the tests and benchmarks, not a general workload speed claim. The
production table remains eight entries; these additions use no production
memory or instrumentation.
