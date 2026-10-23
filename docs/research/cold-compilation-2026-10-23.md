# Cold compilation study, 2026-10-23

Cold source compilation repeats type queries, bytecode scans, temporary operand
allocation and native-image copies. This change fuses compiler decoding with
mandatory validation, reuses bounded hint/control/operand scratch, batches
encoder writes and transfers mapped code/data staging into compiled ownership.
Standalone decoding, malformed-input diagnostics, typed validation and runtime
guards remain checked. Adjacent compiler-local declarations are compacted while
preserving zero-count type checks and original source offsets.

## Measurements

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
