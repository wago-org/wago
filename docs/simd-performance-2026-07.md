# SIMD performance report — July 2026

Date: 2026-07-05

Host:

- OS/arch: linux/amd64 (`Linux cc4611ad4cd7 6.12.90+deb13.1-amd64`)
- CPU: AMD Ryzen 7 8845HS w/ Radeon 780M Graphics
- Go: `go version go1.24.4 linux/amd64`
- Branch: `finish-simd-recursive`

All benchmark rows below are from one local run with Go's default benchmark
harness. SIMD microbenchmarks are intended to catch slow scalarized paths in the
current linux/amd64 baseline (SSSE3/SSE4.1 + AVX/VEX.128, no AVX2/FMA/VNNI).

## SIMD microbenchmarks

Command:

```sh
go test ./src/core/compiler/backend/railshot -bench='BenchmarkSIMD' -benchtime=200ms -count=1 -run '^$'
```

Result: PASS, all listed SIMD benchmarks reported `0 B/op` and `0 allocs/op`.

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| BenchmarkSIMDF32x4MinOrdinary-16 | 8.965 | 0 | 0 |
| BenchmarkSIMDF32x4MinEdges-16 | 12.30 | 0 | 0 |
| BenchmarkSIMDF32x4MaxOrdinary-16 | 8.445 | 0 | 0 |
| BenchmarkSIMDF32x4MaxEdges-16 | 8.705 | 0 | 0 |
| BenchmarkSIMDF64x2MinOrdinary-16 | 12.59 | 0 | 0 |
| BenchmarkSIMDF64x2MinEdges-16 | 9.428 | 0 | 0 |
| BenchmarkSIMDF64x2MaxOrdinary-16 | 9.198 | 0 | 0 |
| BenchmarkSIMDF64x2MaxEdges-16 | 8.916 | 0 | 0 |
| BenchmarkSIMDI32x4TruncSatF32x4SOrdinary-16 | 11.72 | 0 | 0 |
| BenchmarkSIMDI32x4TruncSatF32x4SEdges-16 | 9.866 | 0 | 0 |
| BenchmarkSIMDI32x4TruncSatF32x4UOrdinary-16 | 18.04 | 0 | 0 |
| BenchmarkSIMDI32x4TruncSatF32x4UEdges-16 | 10.15 | 0 | 0 |
| BenchmarkSIMDI32x4TruncSatF64x2SZeroOrdinary-16 | 9.635 | 0 | 0 |
| BenchmarkSIMDI32x4TruncSatF64x2SZeroEdges-16 | 12.46 | 0 | 0 |
| BenchmarkSIMDI32x4TruncSatF64x2UZeroOrdinary-16 | 9.157 | 0 | 0 |
| BenchmarkSIMDI32x4TruncSatF64x2UZeroEdges-16 | 8.452 | 0 | 0 |
| BenchmarkSIMDI32x4ConvertF32x4SOrdinary-16 | 9.131 | 0 | 0 |
| BenchmarkSIMDI32x4ConvertF32x4UOrdinary-16 | 8.941 | 0 | 0 |
| BenchmarkSIMDF64x2ConvertLowI32x4SOrdinary-16 | 8.439 | 0 | 0 |
| BenchmarkSIMDF64x2ConvertLowI32x4UOrdinary-16 | 8.767 | 0 | 0 |
| BenchmarkSIMDDemoteF64x2ZeroOrdinary-16 | 8.967 | 0 | 0 |
| BenchmarkSIMDDemoteF64x2ZeroEdges-16 | 8.490 | 0 | 0 |
| BenchmarkSIMDPromoteLowF32x4Ordinary-16 | 14.01 | 0 | 0 |
| BenchmarkSIMDPromoteLowF32x4Edges-16 | 8.658 | 0 | 0 |
| BenchmarkSIMDI64x2Mul-16 | 9.243 | 0 | 0 |
| BenchmarkSIMDI64x2ShrS-16 | 8.510 | 0 | 0 |
| BenchmarkSIMDI64x2SignedCmpLtS-16 | 15.03 | 0 | 0 |
| BenchmarkSIMDI64x2SignedCmpGeS-16 | 14.12 | 0 | 0 |
| BenchmarkSIMDRelaxedDotI16x8I8x16I7x16S-16 | 19.27 | 0 | 0 |
| BenchmarkSIMDRelaxedDotI32x4I8x16I7x16AddS-16 | 25.43 | 0 | 0 |

Observations:

- Hot SIMD operations in this benchmark set are allocation-free.
- The slowest measured baseline SIMD paths are the deterministic scalarized
  relaxed dot-product add path (25.43 ns/op), relaxed dot product (19.27 ns/op),
  unsigned f32x4 truncation ordinary case (18.04 ns/op), and scalarized signed
  i64x2 comparisons (14–15 ns/op). These are acceptable for the current
  auditable baseline, but are the first candidates for future AVX2/VNNI or
  specialized fast paths behind explicit CPU gates.
- Wasm-correct packed float min/max, conversion, demote/promote, i64x2 multiply,
  and i64x2 signed shift paths all remain in the single-digit to low-teens ns/op
  range in this run.

## Corpus and application benchmarks

Command:

```sh
cd bench && go test . -bench='Benchmark(CompileFull|Exec|JsonAs.*_wago|SqliteQueryWago)' -benchtime=100ms -count=1 -run '^$'
```

Result: PASS. The command includes broad corpus compile/exec coverage plus the
SQLite application benchmark. Rows below are the wago corpus/application rows
from that run; all `BenchmarkExec/*` rows reported `0 B/op` and `0 allocs/op`.
The command also matched wazero comparison rows for the small `BenchmarkExec...`
helpers; those are intentionally omitted from the summary table because this
report is focused on wago SIMD-readiness and regression tracking.

### Selected application/helper rows

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| BenchmarkExecFibLoop_wago-16 | 26.96 | 0 | 0 |
| BenchmarkExecFibRec_wago-16 | 411828 | 79 | 0 |
| BenchmarkExecCallOverhead_wago-16 | 12.53 | 0 | 0 |
| BenchmarkExecHostRoundtrip_wago-16 | 165.4 | 128 | 2 |
| BenchmarkExecGlobalGet_wago-16 | 27.05 | 0 | 0 |
| BenchmarkExecGlobalSet_wago-16 | 17.34 | 0 | 0 |
| BenchmarkExecLocalGet_wago-16 | 18.63 | 0 | 0 |
| BenchmarkExecMemoryLoad_wago-16 | 26.62 | 0 | 0 |
| BenchmarkSqliteQueryWago-16 | 1058243 | 964 | 16 |

### Corpus compile-full rows

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| BenchmarkCompileFull/tiny-16 | 8879 | 10520 | 80 |
| BenchmarkCompileFull/fib_iter-16 | 14475 | 11856 | 74 |
| BenchmarkCompileFull/fib_rec-16 | 15057 | 10240 | 80 |
| BenchmarkCompileFull/arith-16 | 15723 | 12952 | 76 |
| BenchmarkCompileFull/float-16 | 13181 | 11696 | 72 |
| BenchmarkCompileFull/memory-16 | 28092 | 20153 | 120 |
| BenchmarkCompileFull/memory_tree-16 | 43977 | 28050 | 145 |
| BenchmarkCompileFull/globals-16 | 18761 | 15769 | 115 |
| BenchmarkCompileFull/dispatch-16 | 24599 | 24394 | 194 |
| BenchmarkCompileFull/branches-16 | 11904 | 13096 | 74 |
| BenchmarkCompileFull/many_funcs-16 | 742109 | 956105 | 6008 |
| BenchmarkCompileFull/linked_list-16 | 26936 | 19537 | 100 |
| BenchmarkCompileFull/mandelbrot-16 | 40763 | 31232 | 109 |
| BenchmarkCompileFull/sieve-16 | 36569 | 25946 | 107 |
| BenchmarkCompileFull/nbody-16 | 283962 | 87827 | 444 |
| BenchmarkCompileFull/spectralnorm-16 | 260198 | 69794 | 360 |
| BenchmarkCompileFull/fannkuch-16 | 323281 | 107791 | 609 |
| BenchmarkCompileFull/matmul-16 | 171197 | 60238 | 242 |
| BenchmarkCompileFull/quicksort-16 | 235814 | 91849 | 306 |
| BenchmarkCompileFull/crc32-16 | 131293 | 45421 | 155 |
| BenchmarkCompileFull/sha256-16 | 309276 | 88507 | 478 |
| BenchmarkCompileFull/raytrace-16 | 790663 | 265154 | 1604 |
| BenchmarkCompileFull/json-as-16 | 4688135 | 1801920 | 7249 |
| BenchmarkCompileFull/blake-as-16 | 985538 | 318570 | 1974 |
| BenchmarkCompileFull/utf-as-16 | 531569 | 221519 | 1764 |
| BenchmarkCompileFull/wasm3-16 | 11742661 | 619992 | 4992 |
| BenchmarkCompileFull/lua-16 | 20441319 | 709692 | 4409 |
| BenchmarkCompileFull/sqlite3-16 | 60417181 | 2871220 | 15240 |
| BenchmarkCompileFull/ruby-16 | 1132791349 | 28183056 | 124623 |
| BenchmarkCompileFull/esbuild-16 | 1794891426 | 778800256 | 3524284 |
| BenchmarkCompileFull/markdown-16 | 18224721 | 576252 | 1893 |
| BenchmarkCompileFull/crcsum-16 | 2460958 | 166056 | 777 |
| BenchmarkCompileFull/blake3sum-16 | 2753663 | 177246 | 809 |
| BenchmarkCompileFull/base64x-16 | 3870220 | 182019 | 876 |
| BenchmarkCompileFull/jsonproc-16 | 4625890 | 259835 | 1120 |
| BenchmarkCompileFull/script-16 | 151041627 | 4747848 | 14854 |

### Corpus exec rows

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| BenchmarkExec/tiny.add-16 | 18.07 | 0 | 0 |
| BenchmarkExec/fib_iter.fib-16 | 31.76 | 0 | 0 |
| BenchmarkExec/fib_rec.fib-16 | 2011855 | 0 | 0 |
| BenchmarkExec/arith.run-16 | 1930 | 0 | 0 |
| BenchmarkExec/float.run-16 | 12004 | 0 | 0 |
| BenchmarkExec/memory.sum-16 | 366.3 | 0 | 0 |
| BenchmarkExec/memory_tree.run-16 | 13723 | 0 | 0 |
| BenchmarkExec/globals.accumulate-16 | 998.0 | 0 | 0 |
| BenchmarkExec/dispatch.apply-16 | 23.72 | 0 | 0 |
| BenchmarkExec/branches.classify-16 | 17.16 | 0 | 0 |
| BenchmarkExec/many_funcs.run-16 | 19.77 | 0 | 0 |
| BenchmarkExec/linked_list.sum-16 | 10828 | 0 | 0 |
| BenchmarkExec/mandelbrot.render-16 | 330697 | 0 | 0 |
| BenchmarkExec/sieve.count-16 | 157959 | 0 | 0 |
| BenchmarkExec/nbody.step-16 | 403000 | 0 | 0 |
| BenchmarkExec/spectralnorm.run-16 | 809615 | 0 | 0 |
| BenchmarkExec/fannkuch.run-16 | 1932140 | 0 | 0 |
| BenchmarkExec/matmul.run-16 | 317510 | 0 | 0 |
| BenchmarkExec/quicksort.sortN-16 | 84044 | 0 | 0 |
| BenchmarkExec/crc32.hashN-16 | 23293 | 0 | 0 |
| BenchmarkExec/sha256.hashN-16 | 57547 | 0 | 0 |
| BenchmarkExec/raytrace.render-16 | 616963 | 0 | 0 |
| BenchmarkExec/json-as.serializeN-16 | 25912 | 0 | 0 |
| BenchmarkExec/json-as.deserializeN-16 | 46899 | 0 | 0 |
| BenchmarkExec/blake-as.hashN-16 | 1036993 | 0 | 0 |
| BenchmarkExec/utf-as.convertN-16 | 318092 | 0 | 0 |

## Host-call ABI refresh

This slice changed the synchronous host-call slot marshaling so `v128` host
params/results use the public two-slot little-endian ABI and widened the fixed
control-frame arg/result arrays to 16 `uint64` slots (enough for existing WASI
imports plus vector slots). No SIMD instruction hot path changed. The
control-frame round-trip benchmark was refreshed to keep a host-call baseline
next to the SIMD plumbing change.

Command:

```sh
go test ./src/core/runtime -bench='BenchmarkHostCall' -benchtime=200ms -count=1 -run '^$'
```

Result: PASS.

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| BenchmarkHostCall-16 | 136.3 | 256 | 2 |

## Coverage and follow-up notes

- Admission/backend parity is tested by:
  - `TestDecodedSIMDOpcodeCoverage` in `src/core/compiler/frontend`, which locks
    the decoded core+relaxed SIMD table to 256 admitted opcodes and 20 reserved
    holes through opcode 275.
  - `TestSupportedSIMDInstructionsMatchValidator`, which checks frontend support
    against the SIMD validator-admission map.
  - `TestSIMDFrontendAdmittedShapesCompile` in `railshot`, which compiles a valid
    stack shape for every validator-admitted SIMD opcode and catches missing
    backend lowering cases.
- `v128` host/cross-instance value plumbing is covered by focused tests in
  `src/wago`: `TestSyncHostImportV128SlotForm`,
  `TestSyncHostImportVoidV128UsesSyncPath`,
  `TestSyncHostImportReflectedV128`, and `TestCrossInstanceCallV128`.
- The spec-suite execution harness in `src/wago/spectest_exec_test.go` now parses
  WABT structured `v128` lane-array arguments, expected results (including
  per-lane NaN classes for `f32x4`/`f64x2`), and `global.get` actions, flattening
  each vector through the public two-slot little-endian ABI. The focused helper
  test is `go test ./src/wago -run TestSpecValueV128StructuredJSON`.
- Official SIMD spec tests were not run in this report because `wast2json`
  (WABT) is not installed on `PATH` in this checkout. The `tests/spec` submodule
  was initialized at `a8bcbafe6d2fb191ce0188de0e18fdc107fa2598`. When WABT is
  available, run:

  ```sh
  WAGO_SPECTEST_DIR=tests/spec WAGO_SPEC_VERSION=simd go test ./src/wago -run TestSpecSuiteExec -count=1
  ```

  Focused execution tests, opcode parity tests, and the v128-aware harness unit
  test above are the current local proof until that end-to-end command can run.
- Follow-up optimization candidates, not correctness blockers: relaxed dot-product
  scalar sequences, unsigned truncation fast paths, and scalarized signed i64x2
  comparisons. Do not introduce AVX2/FMA/VNNI implementations without explicit
  CPU feature gates and retaining the baseline fallback.
