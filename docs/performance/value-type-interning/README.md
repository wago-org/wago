# Recent value-type interning

This change targets clustered repeats during compilation. It does not change
runtime execution or the persistent artifact format. Unique-heavy insertion
still uses the original linear search and remains quadratic over a growing pool.

The baseline is main at `82dbe15cea7fd75e1e4d66219e679d19b0976593`. The reviewed
PR head was `235bfb1f0ea33aa9dbd4c36b4ffd4f84f5cd7845`. Main was merged into the
published PR branch to permit a normal fast-forward push without rewriting its
history. No changes from the other optimization PRs were added separately.

## Correctness and memory

All six insertion sites share one compiler-local `uint32`: imported globals,
imported tables, defined globals, primary table metadata, extra tables, and typed
elements. Main has no equivalent recent-index cache. The pool only grows during
this scope. Cache hits use the same complete Go value equality as the original
linear lookup. Misses preserve the first index and insertion order.

Tests compare indexes and pool contents at every step, including the 31/32/33
boundary, growth, runs, alternating values, hits and misses, and differences in
every descriptor field. Recursive reference identity is carried by the flattened
defined type index. The fuzz input is capped at 512 bytes. The fixture that mixes
imports, globals, tables and elements exercises all six production sites.

The cache pointer does not escape. Existing descriptor-pool growth can allocate;
complete compilation is not allocation-free. A preallocated-pool test verifies
zero allocations from insertion and reuse. No persistent structure changed.
On Linux/amd64, both builds have these sizes: `Compiled=784`, `Module=136`,
`Instance=896`, `ValueTypeDescriptor=16` bytes. The local cache is four bytes.

Baseline and final serialized artifact hashes match for all seven workload
fixtures. Tests also compare the reloaded descriptor pool and indexes, imported
globals and extra tables. A test-only reconstruction from decoded Wasm compares
the compiled pool with the original linear insertion order.

## Method

Linux/amd64, AMD Ryzen 7 8845HS, Go 1.27.1, `GOMAXPROCS=4`. Two separate worktrees
use the same main base and identical benchmark source. The baseline has only a
test shim that routes metadata benchmark calls to `internValueType`; its
production compiler is unchanged. Six serial pairs alternate build order, with
150 ms per benchmark sample. No build, test, or other benchmark runs alongside
these measurements. `benchstat` compares the six samples.

`BenchmarkValueTypeMetadata` and `BenchmarkValueTypeMetadataPatterns` are isolated
microbenchmarks. Their pool storage is allocated before timing. Complete compile
benchmarks call `Compile` on Wasm bytes, including decoding, validation, native
code generation and final artifact creation. Workload and clustered benchmarks
also close each artifact. The existing small-scalar controls retain their original
benchmark definition. Allocation counts and bytes are reported separately.

The selected checked-in Wasm modules do not reach the cache threshold. Synthetic
Core 3 modules therefore exercise large indexed-reference pools: clustered
globals, all-unique globals, and mixed imported/defined globals, tables and typed
elements. These are complete valid modules, but metadata stress cases, not proof
of an application-level speedup.

## Small-pool helper experiment

An explicit early return to `internValueType` below 32 entries increased the Go
inline cost from 77 to 105, above the budget of 80. It increased the disassembly
from 142 to 224 lines and slowed the four-descriptor microbenchmarks by 37–55%
across six paired 200 ms samples. The experiment was rejected. The final helper
retains the single miss call and performs no descriptor comparison below the
threshold. `internValueType` has inline cost 30. Both original helpers are
eligible for inlining, but the large compilation function does not inline either
call arrangement. No forced inlining or duplicated call-site scan was added.

## Validation environment

The pinned Core 3 submodule was initialized in both worktrees. WABT 1.0.41 must
precede the installed 1.0.42 in `PATH`; setting only `WAGO_WAST2JSON` does not
configure every test. Missing-corpus and wrong-WABT errors were reproduced on the
baseline before correcting this setup.

The installed optional TinyGo tool first failed worktree VCS stamping, then failed
with duplicate `tinygo_task_exit` symbols after `GOFLAGS=-buildvcs=false`. The
symbol failure was reproduced on the clean baseline. The full `just test` gate
was also run with this optional tool and its mise shim absent from `PATH`; those
TinyGo-specific tests skip in that configuration. This does not claim local
TinyGo validation. Native ARM64 hardware was unavailable; local ARM64 validation
is a cross-build only.

## Results

The isolated metadata improvement is not a whole-compiler percentage. Complete
synthetic clustered compilation improves by 7–17%; the three checked-in workloads
show no statistically significant improvement. Full median data, including every
allocation result, is in [results.csv](results.csv).

| Isolated metadata case | Baseline | Final | Change |
| --- | ---: | ---: | ---: |
| 128 distinct, cluster 16 | 209.93 µs | 23.77 µs | -88.68%, p=.002 |
| 1024 distinct, cluster 16 | 12.8872 ms | 0.8722 ms | -93.23%, p=.002 |
| 4 distinct, cluster 1 | 28.87 ns | 29.71 ns | +2.91%, p=.002 |
| 4 distinct, cluster 16 | 316.8 ns | 320.8 ns | +1.25%, p=.017 |
| 4 distinct, cluster 64 | 1.237 µs | 1.288 µs | +4.16%, p=.002 |
| 32 distinct, cluster 1 | 902.8 ns | 934.3 ns | +3.50%, p=.002 |
| 128 distinct, cluster 1 | 13.48 µs | 13.69 µs | +1.51%, p=.041 |
| 1024 all-unique | 402.9 µs | 401.6 µs | no significant change |
| 1024 then alternating final two | 1.194 ms | 1.200 ms | no significant change |
| 1024 then adjacent final value | 1.2194 ms | 0.4069 ms | -66.63%, p=.002 |

Every isolated case is **0 B/op, 0 allocs/op** in both builds. The small-pool
regressions remain: for four descriptors, about 0.03–0.10 ns per insertion in the
reported regressing cases. The early-return experiment made them much worse.
There is no significant small-scalar complete-compilation regression.

| Complete workload | Wasm bytes | Insertion calls | Distinct | Adjacent repeats | Baseline | Final | Time comparison |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| linked_list | 144 | 0 | 0 | 0 | 18.82 µs | 18.64 µs | p=.937 |
| json_as | 23,410 | 27 | 1 | 26 | 1.567 ms | 1.587 ms | p=.132 |
| nanosvg reduced | 82,360 | 2 | 2 | 0 | 5.941 ms | 6.119 ms | p=.310 |
| synthetic mixed 128×4 | 22,065 | 3,072 | 128 | 2,432 | 4.869 ms | 4.719 ms | p=.093 |
| synthetic globals 128×16 | 29,075 | 4,096 | 128 | 3,840 | 3.247 ms | 3.015 ms | -7.14%, p=.002 |
| synthetic globals 1024×16 | 261,140 | 32,768 | 1,024 | 30,720 | 71.31 ms | 58.85 ms | -17.48%, p=.002 |
| synthetic unique 1024 | 11,154 | 1,024 | 1,024 | 0 | 2.638 ms | 2.607 ms | p=.589 |

Insertion counts follow compile order, including the second insertion of imported
table metadata. They do not count function signature types, which do not enter
this pool. The global cluster fixtures include two passes over the type sequence.

| Complete workload | Baseline B/op | Final B/op | Baseline allocs/op | Final allocs/op |
| --- | ---: | ---: | ---: | ---: |
| linked_list | 21,237 | 21,237 | 102 | 102 |
| json_as | 309,077.5 | 309,076 | 408 | 408 |
| nanosvg | 1,040,294 | 1,040,268.5 | 658.5 | 658 |
| synthetic_mixed_128x4 | 1,895,534.5 | 1,895,542.5 | 10,233 | 10,233 |
| synthetic_globals_128x16 | 3,615,237 | 3,615,305.5 | 12,266.5 | 12,267 |
| synthetic_globals_1024x16 | 33,415,656 | 33,415,830.5 | 99,980 | 99,981.5 |
| synthetic_unique_1024 | 1,355,395 | 1,355,396 | 4,704 | 4,704 |
| small scalar | 15,861 | 15,861 | 81 | 81 |
| small scalar Core 3 | 15,717 | 15,717 | 80 | 80 |

These are medians; a fractional allocation count denotes the median of integer
sample reports, not a fractional allocation. Complete compilation has small
runtime allocation variation. The 128×16 bytes result is +68.5 B/op (about
0.0019%, p=.004), while its allocation count is not significantly different.
The cache itself introduces no allocation, as checked separately below.

## CPU profiles

Ten-second benchmark profiles (plus calibration) show linear interning at 18.33%
of sampled CPU time on the baseline 1024×16 synthetic workload. Final linear
interning is 1.44% cumulative; the whole cache helper is 1.54%. Type resolution
(`subtypeByTypeIdxWithRecGroup`, 22.79% flat), constant-expression evaluation
(22.54% flat), and frontend GC-heap support checks (8.96% flat) dominate the final
profile. Descriptor conversion itself is 1.44% cumulative. The helper call does
not hide the measured gain in this workload.

For json_as, interning is below sampling resolution in both builds. Its one
shape never activates the cache; instruction decoding, validation and code
emission dominate. No application-level compile improvement is established.
Percentages are shares of sampled process CPU, not wall-clock stage timers.

## Fixed-iteration allocation control

With `GOMAXPROCS=1 GOGC=off`, six fresh-process samples of ten iterations each
have identical baseline and final allocation results below. These runs control
GC-related noise; they are not the timing comparison. The preallocated interner
allocation test also passes in both builds.

| Workload | B/op in both builds | allocs/op in both builds |
| --- | ---: | ---: |
| CompileSmallScalar | 15,869 | 81 |
| CompileSmallScalarCore3 | 15,725 | 80 |
| CompileValueTypeWorkloads/synthetic_unique_1024 | 1,355,037 | 4,702 |
| CompileValueTypeWorkloads/synthetic_globals_128x16 | 3,614,365 | 12,260 |
| CompileValueTypeWorkloads/synthetic_globals_1024x16 | 33,414,429 | 99,971 |

## Commands and checks

The substantive commands used for this review are listed below. Paths to the
local binaries and logs use `/tmp/wago-pr640-evidence`. The benchmark source is
checked in; the raw six-pair logs, profiles and diagnostics remain in that local
evidence directory. `results.csv` records all timing-run medians.

- `git fetch origin main quality/p10-value-type-interning`; record both SHAs;
  create separate PR and detached-main worktrees.
- `git merge --no-commit origin/main`, review the production diff, `just lint`,
  then commit the merge. No conflict required an older file version.
- `rg` for `internValueType`, `valueTypeInterner`, `ValueTypes`, and pool writes
  across production and tests; all six insertion sites use the shared cache.
- `git submodule update --init --depth=1 tests/conformance/spec-v3` in both builds.
- `go test ./src/wago -run 'TestValueType|TestInternedGlobal' -count=1 -v`.
- `go test ./src/wago -run '^$' -fuzz '^FuzzValueTypeInterner$' -fuzztime=5s -parallel=2`:
  passed, 12,710 executions in the recorded run.
- `go test ./src/wago -run '^TestValueTypeCompileArtifacts$|^TestValueTypeInternerLayout$' -v`
  on baseline and final; compare all seven artifact hashes and layouts.
- `go test -c -o <build>.test ./src/wago` for each build; identical benchmark code.
- Six alternating pairs of `<build>.test -test.run '^$' -test.bench '<pattern>'
  -test.benchmem -test.benchtime=150ms`, with `GOMAXPROCS=4`.
  Pattern includes `BenchmarkValueTypeMetadata`, `BenchmarkValueTypeMetadataPatterns`,
  `BenchmarkCompileValueTypeInterning`, `BenchmarkCompileValueTypeClusters`,
  `BenchmarkCompileSmallScalar`, `BenchmarkCompileSmallScalarCore3`, and
  `BenchmarkCompileValueTypeWorkloads`.
- `benchstat base.bench final.bench`; separate six-pair 200 ms small-pool shape test.
- Ten-second workload benchmark CPU profiles for baseline/final synthetic 1024×16
  and json_as; `go tool pprof -top` and `-list`. The final json_as lookup listing
  has no matches because no lookup sample was recorded.
- Six allocation-control samples per build with `GOMAXPROCS=1 GOGC=off`,
  `-test.benchtime=10x -test.benchmem`; `benchstat base.alloc final.alloc`.
- `go test -gcflags='-m=2' ./src/wago -run '^$'`; inspect escape and inline diagnostics.
- `go tool objdump -s 'valueTypeInterner.*intern' <build>.test` for both helper shapes.
- `go test ./src/wago -count=1`.
- `GORACE=atexit_sleep_ms=0 go test -race ./src/wago -count=1`; race detection remains
  enabled. Removing the exit delay avoids a one-second wait per corpus child.
  An earlier unconfigured race run was stopped and replaced by this complete run.
- `go test -tags wago_guardpage ./src/wago -count=1` on native AMD64.
- `GOOS=linux GOARCH=arm64 go test -c -o final-arm64.test ./src/wago`: build only.
- `GOFLAGS=-buildvcs=false just test`, with pinned WABT and without the broken
  optional TinyGo installation or shim. Includes ordinary tests, release checks,
  and corpus tests. Earlier runs with local TinyGo failed as described above.
- `just lint`, `just docs`, and `git diff --check` before the final commit.
- Normal `git push origin quality/p10-value-type-interning`; inspect PR checks
  for that exact commit. The PR is not merged.

Final local results: native AMD64 unit tests, race tests, guard-page tests,
allocation tests, bounded fuzzing, artifact checks, ARM64 cross-build,
`just test` with the supported-tool setup, `just docs`, and `git diff --check`
pass. `just lint` exits successfully. Its standard staticcheck step reports
baseline findings that the repository recipe tolerates; comparison with the
baseline found no new finding. Runtime-tagged staticcheck passes.
