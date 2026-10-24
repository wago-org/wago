# Cold compilation study, 2026-10-23

Cold source compilation repeats type queries, bytecode scans, temporary operand
allocation and native-image copies. This change fuses compiler decoding with
mandatory validation, reuses bounded hint/control/operand scratch, batches
encoder writes and transfers mapped code/data staging into compiled ownership.
Standalone decoding, malformed-input diagnostics, typed validation and runtime
guards remain checked. Adjacent compiler-local declarations are compacted while
preserving zero-count type checks and original source offsets.

## ARM64 campaign integration

The ARM64 optimization campaign is now integrated into this existing PR. It adds
streamed validation hints, reusable register-move graphs, compact scratch and
metadata, and compiler declaration locality. Weak ARM64 selections are retired;
existing CLI/project configuration keys remain accepted as ignored compatibility
settings. The integration preserves the PR's shared frontend and AMD64 changes.

Merged-source validation passed both frozen 20-workload corpus sets (33 unique
workloads), 2,116 complete valid optimization off/on pairs across 93 selections,
Wasm/shared/ARM64/public tests, register-allocation checks, targeted race checks,
and native Linux ARM64 public tests. Linux AMD64 test binaries cross-compile;
AMD64 execution has not been rerun locally for this integration.

The root suite passed except for a CLI subprocess-start timeout (isolated rerun
passed) and `cli/runtime/commands/build` rejecting extended attributes while
publishing through a relative symlink. The latter reproduces on untouched current
main; the full root suite is therefore not reported as passing.

The full public profiling suite encounters a pre-existing mutex failure in
`TestHostFuncRefNumericRetainedReference`, independently reproduced on current
main and both parent sources. Backend profiling tests pass.

All performance figures below describe the explicitly identified earlier source.
They do not measure the combined integration. A fresh paired comparison is still
required to establish its compile, total process peak-RSS and execution effects.
The 30% physical-memory reduction goal remains open.

## Earlier PR branch validation against current main

Tested production source: 0a7de4d9c45149ac83325b629d5cdd56bfa7bea0 (after merging main).
Baseline: dc13730184e8ba6a1a2b3e3887c53a2b880db88c. The baseline uses identical measurement instrumentation;
its production compiler/runtime sources are unchanged.
[Identity](cold-compilation-2026-10-23/postmerge-identity.json) binds both revisions
and all four measurement binaries. Subsequent changes only add this report/evidence.

A single fresh-process pair per input measured **40.4% less total
compile CPU time** (ratio 0.596293; geometric ratio 0.67393).
The independent compile/execution harness measured about 40.0% less total compile
wall time. Execution was roughly flat (weighted ratio 1.006980, geometric
ratio 1.01415), with identical native-code hashes on **20/20** inputs and
**40/40** numeric/memory oracle checks passing. Only **7/20** workloads met
40% less compile CPU time in this rough sample. No universal per-workload or
same/better execution timing claim is established.

Full Wasm/frontend/shared/AMD64 encoder/backend/runtime tests, full
register-allocation checks, targeted race checks and focused profiling checks
passed on the merged source with a fresh task-local Go cache.
[Validation log](cold-compilation-2026-10-23/postmerge-validation.log),
[CPU rows](cold-compilation-2026-10-23/postmerge-cpu.jsonl), and
[execution rows](cold-compilation-2026-10-23/postmerge-execution.jsonl) are included.

| Workload | Current-main CPU ms | PR CPU ms | Time reduction |
| --- | ---: | ---: | ---: |
| fannkuch | 0.414 | 0.388 | +6.3% |
| raytrace | 0.611 | 0.406 | +33.6% |
| sha256 | 0.313 | 0.378 | -20.8% |
| json-as-simd | 2.682 | 1.691 | +37.0% |
| utf-as-simd | 1.132 | 0.827 | +26.9% |
| yyjson | 12.630 | 8.179 | +35.2% |
| pcre2 | 20.308 | 11.641 | +42.7% |
| monocypher | 1.761 | 1.121 | +36.3% |
| kissfft | 1.924 | 1.579 | +17.9% |
| fastfloat | 2.393 | 1.570 | +34.4% |
| polybench-gemm | 0.508 | 0.368 | +27.6% |
| polybench-nussinov | 0.406 | 0.372 | +8.4% |
| coremark | 1.293 | 0.806 | +37.7% |
| quickjs-script | 72.189 | 41.182 | +43.0% |
| esbuild-minify | 1263.549 | 722.234 | +42.8% |
| sqlite3-query | 149.530 | 88.454 | +40.8% |
| jq-json-transform | 64.486 | 36.283 | +43.7% |
| brotli-compress | 45.294 | 26.488 | +41.5% |
| lua-cli-buckets | 37.768 | 19.734 | +47.7% |
| swift-format-source | 1766.641 | 1091.025 | +38.2% |

## Earlier frozen-baseline study

Frozen baseline: fc6af651c15e286409d9cc117c94181e9ee50ff3.
Candidate146 source and binary hashes:
[identity](cold-compilation-2026-10-23/qualified-source-and-binaries.json).
These measurements PRECEDE synchronization with newer main; they are not fresh
performance evidence for that later merge.

Linux amd64, Ryzen 7 7800X3D, Go 1.27.1, GOMAXPROCS=1, one compiler worker,
CPU 6. A benchmark lock serialized measurements on a shared host.
One fresh-process sample per revision/input, alternating order. These are
ballpark estimates; sub-millisecond rows are particularly noisy.

Total cold compile CPU time was **40.2% lower** (ratio 0.59845), with geometric
mean ratio 0.68046. Only **8/20** workloads reached 40% less compile time.
The per-workload 40% target was **not met**; some small workloads regress.

| Workload | Baseline CPU ms | Candidate CPU ms | Time reduction |
| --- | ---: | ---: | ---: |
| fannkuch | 0.462 | 0.396 | +14.3% |
| raytrace | 0.623 | 0.418 | +32.9% |
| sha256 | 0.312 | 0.333 | -6.7% |
| json-as-simd | 3.154 | 1.765 | +44.0% |
| utf-as-simd | 1.137 | 1.129 | +0.7% |
| yyjson | 13.557 | 8.293 | +38.8% |
| pcre2 | 24.331 | 12.756 | +47.6% |
| monocypher | 2.270 | 1.140 | +49.8% |
| kissfft | 2.523 | 1.401 | +44.5% |
| fastfloat | 2.538 | 1.827 | +28.0% |
| polybench-gemm | 0.402 | 0.514 | -27.9% |
| polybench-nussinov | 0.448 | 0.425 | +5.1% |
| coremark | 1.138 | 0.853 | +25.0% |
| quickjs-script | 76.429 | 47.440 | +37.9% |
| esbuild-minify | 1432.221 | 792.012 | +44.7% |
| sqlite3-query | 157.553 | 106.417 | +32.5% |
| jq-json-transform | 68.755 | 38.342 | +44.2% |
| brotli-compress | 54.493 | 27.511 | +49.5% |
| lua-cli-buckets | 38.678 | 21.302 | +44.9% |
| swift-format-source | 1882.435 | 1187.984 | +36.9% |

Generated native-code hashes matched frozen main on all 20 inputs.
The final incremental execution pair compares candidate146 to previous qualified
candidate135, not to frozen main: all 40 numeric/memory oracles passed, compile
time was about 2.1% lower and execution was roughly flat (weighted ratio 1.0061,
geometric mean 1.0092). This does not establish same/better timing on every input.

Raw [cold CPU rows](cold-compilation-2026-10-23/cold-cpu-frozen-baseline.jsonl),
[paired execution rows](cold-compilation-2026-10-23/paired-compile-execution.jsonl)
and [input contracts/hashes](cold-compilation-2026-10-23/manifest.json) are included.

## Validation

Candidate146 passed full Wasm, src/wago, AMD64 encoder/backend tests, targeted
race checks, full AMD64 register-allocation checks, focused profiling checks,
20 native-code comparisons and 40 execution oracles. See the
[qualification log](cold-compilation-2026-10-23/qualification.log).

Coverage includes fused decoder errors, compact validation values, local types,
branch/control scratch, caller hints, operand arenas, mapping ownership and
encoder bytes/metadata. Linux amd64 qualification does not establish native
ARM64/RV64 performance. Rejected experiments are excluded. The last unqualified
experiment was reverted after a Go linker panic, also affecting an unchanged
encoder test package.

## Reproduction

Use separate baseline/candidate checkouts, identical Go versions, inputs and
CPU settings. Build scripts/compilemem in each checkout and invoke each binary
once with GOMAXPROCS=1, taskset -c 6, and the selected corpus artifact path.

For execution contracts, build the opt-in Linux test binary using
"go test -c -o /tmp/study.test ./bench/suite". Run from bench/suite with
WAGO_STUDY_MANIFEST set to the absolute path of this report's manifest,
GOMAXPROCS=1, and arguments:
-test.run=^TestOptimizationStudySample$ -test.v -wago.corpus=fannkuch.

Do not substitute allocation traffic or retained heap for kernel peak RSS.
