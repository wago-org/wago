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

## Exact SIMD source pairs

`simd_rule_pairs_test.go` tests the S14/S15/S16 supplement of #809. It compares
widened unsigned rounded byte averages, fixed high-byte extraction, and signed
constant dot/sub expressions with their shorter core-SIMD forms. An additional
dot/sub form extracts the two signed halves with arithmetic shifts and subtracts
them. This avoids the fused-dot form's SSE2 regression in the measured fixture. Each original
and simplified module must match an independent full-vector lane oracle. No
production rewrite is added. Signed i16 dot coefficients are exactly 1/0/-1;
these tests do not generalize to relaxed dot, saturation, or arbitrary constants.

The byte-average lane model exhausts all 65,536 pairs. The high-byte model
enumerates every high byte and selected low-bit patterns; the shift identity
follows because the low 24 bits cannot contribute to a shift by 30. The dot
identity is the exact linear equation `a*1+b*0-(a*0+b*1)=a-b`; its signed-i16
range fits i32. The test sweeps every first lane with five second-lane controls.
Seven near misses reject wrapping/rounding changes, swapped halves, wrong byte
mapping, sign extension, changed shift counts, and different vector producers.

Native execution uses 64 distinct records from seed 809, with signed extremes
and byte sentinels. Each record contains two inputs and one complete 128-bit
output. Tests check unchanged input bytes, all output bytes, zero-count behavior,
and completed work. The same observer rejects skipped work and changed output.
AMD64 runs SSE2 and the enabled AVX2 profile. A diagnostic build reports the
established compiler, spills, and literal bytes. GNU objdump checks the selected
`pavgb`, immediate-30 `psrld`, `pmaddwd`, and the two immediate-16 `psrad`
instructions; substituting each valid
original artifact must fail that selection gate. Loaded and Wasm hashes are logged.

`BenchmarkSIMDSourcePairs` calls the low-level native engine on 256 records per
iteration, with correctness checks before and after timing. Its result is a
source-form comparison, not a public invocation or compiler-fix speedup.
`BenchmarkSIMDSourcePairCompile` includes decode, validation, and code generation
and excludes WAT assembly, executable mapping, and execution. Both report
allocations; execution also reports native bytes. The fixtures need the existing
WABT helper. Native ARM64 runs are supported but were not executed locally.
Rule-directed versus generic-generator yield, other control-flow/effect contexts,
and the rest of #809 remain outside this bounded slice.
