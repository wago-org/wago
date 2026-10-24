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

## Current source: native Linux AMD64 comparison

Production `d8c878ada3a834af9b305997fa7e301479763591` versus unchanged main
`dc13730184e8ba6a1a2b3e3887c53a2b880db88c`, Go 1.27.1, identical test-only
measurement harness and pinned 20-workload manifest. Hub is an AMD Ryzen 7
7800X3D running Linux x86_64. GOMAXPROCS=1, one compiler worker, CPU6 affinity.
The shared-host benchmark lock serialized the run and was verified released.
All artifact and command-input hashes were checked before execution; the current
catalog was copied into an isolated temporary directory. Shared inputs and
checkouts were not modified.

One alternating fresh-process pair per input gives these ballpark estimates:

| Metric | Current/main ratio | Change |
| --- | ---: | ---: |
| Total compile wall time | 0.623 | -37.7% |
| Geometric mean compile time | 0.654 | -34.6% |
| Sum of per-process compile peak RSS | 0.803 | -19.7% |
| Geometric mean process peak RSS | 0.896 | -10.4% |
| Compile allocation traffic | 0.191 | -80.9% |
| Native code bytes | 1.000 | unchanged |
| Geometric mean execution time | 0.999 | roughly flat |

All40 exact execution oracles pass. This is separate from the ARM64 comparison;
ratios across different hosts are not combined. Allocation traffic is not
physical memory. The total process-memory target remains open on both profiles.
These samples measure compile wall time, not compile CPU time.

[Paired samples](cold-compilation-2026-10-23/current-amd64-samples.jsonl)
and [source/binary identity](cold-compilation-2026-10-23/current-amd64-identity.json)
preserve the current-source attribution.

The native AMD64 on/off sweep also passed on the original20 corpus:
**1,702 complete valid execution-entry pairs across74 switches**, covering
23 entries. All75 advertised switches were exercised; stack-fence-off is
intentionally unsafe, so its23 on-only entries are excluded from pair counts.
One iteration per selection keeps this an oracle/attribution screen, not a
precision timing study. The sweep used CPU6 and one compiler worker under the
same required host lock, which was verified released afterward. The one missing
Grain fixture was copied with its exact hash into private scratch and redirected
in the private catalog only; shared fixtures were unchanged.

[Matrix summary](cold-compilation-2026-10-23/current-amd64-ablation-summary.json)
and [compressed complete log](cold-compilation-2026-10-23/current-amd64-ablation.log.gz)
retain selection coverage. Single-iteration timing differences alone do not
justify retiring an optimization without checking generated-code effects and
its applicable workload coverage.

## Follow-up: reuse validation's negative inline proof

Source `92c909d0a715456c46cdf39be9ff1d304445332d` records one bit per
function when validation proves that no direct callee fits any inline body-size
limit. Those callers skip the later inline-callee bytecode scan. Possible
candidates retain the existing full scan and inlining decisions; unavailable or
mismatched streamed hints retain the scanner fallback. Compact limits, configured
inline limits and the bounded recursive candidate are included in the proof.
There is no module/workload identity check.

A compile-only sample attributed about 7% of Swift compilation to that scan.
One exploratory pair against parent `e76b9fa2f` measured about 4% less Swift
compile time and 1% less esbuild compile time, with essentially unchanged RSS
and native-code sizes. These two rows are an attribution screen, not acceptance
of the full performance goal. Backend/public tests, register-allocation checks,
and exact corpus checks across 33 workloads pass. Tests compare every available
negative proof with the complete inline-target scan and cover oversized-only
callers, real candidates, compact policy and missing-proof fallback.

The fresh pinned-20 comparison against current main on the same Linux ARM64
profile measured total compile wall ratio **0.710** (about 29% faster), summed
per-process compile peak-RSS ratio **0.843** (about 16% lower), allocation ratio
0.228 and geometric execution-time ratio 0.995. All 40 exact oracles passed.
One pair per input, alternating order, GOMAXPROCS=1 and one compiler worker;
unrelated host load was uncontrolled. Several individual execution rows still
regress in this rough timing screen; aggregate neutrality does not establish
per-workload same-or-better timing. The full goal remains open.

[Raw paired samples](cold-compilation-2026-10-23/inline-proof-arm64-samples.jsonl)
and [production/binary identity](cold-compilation-2026-10-23/inline-proof-arm64-identity.json)
identify this source separately from the earlier integrated measurements below.

## Follow-up: restore independently proven float maps

The expanded corpus exposed a real gap in the earlier retirement screen:
polybench-gemm uses the independent floating-point-map component of the former
`dot-loop-vector` policy. A focused reversed-order comparison reproduced about
208 microseconds per call on integrated source versus 112 on main. Main with
`dot-loop-vector` explicitly off measured about 205; compiler diagnostics showed
`float-map-loop-vector` and `float-map-loop-unroll` admissions in main.

Restore that bounded proof/emitter alone as the default-on `float-map-loop`
selection. Integer dot/min-plus recognizers remain retired. Bounds/alias guards,
expression ordering, NaN behavior and the original scalar tail are preserved;
there is no workload identity check. The existing exhaustive memory oracle now
covers the restored path, and a separate admission regression ensures scalar
fallback cannot hide accidental removal of vectorization.

The restored path measured about 114 microseconds per call in one focused sample;
its explicit off switch restored the slowdown. This diagnoses the large gemm
regression in the historical integrated table below. Backend/public/settings and
project tests, codegen diagnostics and allocation-check float-map tests pass.
The new policy's off/on check passes across both corpus sets: 36 execution-entry
pairs across 33 workloads, plus default corpus checks. These timing screens are
ballpark and do not establish the overall acceptance gates.

## Integrated source: rough Linux ARM64 comparison

Tested source `1c3a98cb23ec4e7154221319b61ac77de3d1ba78` against unchanged
main `dc13730184e8ba6a1a2b3e3887c53a2b880db88c`, using the identical test-only
measurement harness and frozen 20-workload manifest. Native ARM64 Linux ran in
Docker Desktop on Apple M4 Max, Go 1.27.1, GOMAXPROCS=1, one compiler worker.
One fresh-process pair per input, alternating order; no repeated precision runs.
Builds/tests finished before measurement; unrelated host load was uncontrolled.

| Metric | Integrated/main ratio | Change |
| --- | ---: | ---: |
| Total compile wall time | 0.750 | -25.0% |
| Geometric mean compile time | 0.606 | -39.4% |
| Sum of per-process compile peak RSS | 0.835 | -16.5% |
| Geometric mean process peak RSS | 0.865 | -13.5% |
| Total compile allocation traffic | 0.228 | -77.2% |
| Total native code bytes | 0.916 | -8.4% |
| Geometric mean execution time | 1.006 | +0.6% |

All 40 samples pass exact execution oracles. Compile peak RSS is captured before
guest execution and includes process startup/input residency. Summing independent
process peaks is an aggregate comparison, not one simultaneous process peak.
Compilation CPU time was not measured by this harness.

Execution remains unqualified against the same-or-better gate: polybench-gemm
was about 86% slower in this single sample, while fastfloat was about 29% faster.
The rough aggregate cannot establish per-workload neutrality. The 30% total
compile-time and physical-memory goals remain open.

[Raw paired samples](cold-compilation-2026-10-23/integrated-arm64-samples.jsonl)
and [source/binary identity](cold-compilation-2026-10-23/integrated-arm64-identity.json)
preserve attribution. This compares current main with the combined PR; it does
not isolate the incremental contribution of the ARM64 merge.

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
