# AMD64 SSE2 repeated performance measurements

Measured on 2026-09-26 after fetching and rebasing on `origin/main`.
The branch was already up to date; no rebase changes were necessary.

## Method

- Reference: `origin/main`, `1f137e8e6c6bac09e7b4b8876d505b5dc5e1d9f9`.
- Candidate: `aabccddf14ca887ed97e4110a3c9f958d99d0d81`.
- AMD Ryzen 7 8845HS, Linux AMD64, Go 1.27.1, performance CPU governor.
- Precompiled test binaries; `GOMAXPROCS=1`, process affinity fixed to CPU 14.
- Eight independent samples per workload, 250 ms per benchmark sample.
- Each round measures reference modern, candidate modern and candidate SSE2.
  The order reverses every second round. No benchmark processes run concurrently.
- The same `BenchmarkAMD64Tiers` workload source is used in both revisions.
  The reference copy omits the new capability fields and its SSE2 tier because
  those facilities do not exist on main. Both modern profiles enable bit counts.
- Compilation excludes Wasm decoding and executable memory mapping. Execution
  includes the engine call boundary; memory copy moves 1,024 bytes per call.
- Other user programs remained active. CPU affinity does not provide exclusive
  access to the core or eliminate shared power and thermal effects.

This repeat supersedes the short timing samples in the
[implementation report](amd64-sse2-validation.md). It covers only the 16 focused
workloads; the full benchmark suite was not run.

## Conclusions

`benchstat` found no statistically significant modern-path difference in any
individual compilation or execution workload (eight samples, alpha 0.05).
This does not prove equivalence or exclude a small regression.

The geometric mean of the per-workload median ratios is **+1.83% for compilation**
and **−0.99% for execution**. Modern compilation medians range from −0.4% to
+5.3%; execution medians range from −3.0% to +0.9%. These observed differences
are not evidence of a performance improvement.

All 16 modern native code sizes and CRC32 values match main. Compilation B/op
and allocs/op also match main for every workload. Every execution measurement,
including the SSE2 profile, reports **0 B/op and 0 allocs/op**.

Baseline SIMD sequences cost more code and execution time for several operations.
No performance code changes were made in response to this timing run.

## Compilation medians

All times are ns/op. Allocation columns give B/op followed by allocs/op.
The modern allocation column applies to both main and the candidate.

| Workload | Main modern | PR modern | Change | PR SSE2 | Modern B/op; allocs/op | SSE2 B/op; allocs/op |
|---|---:|---:|---:|---:|---|---|
| f32_add | 2664.5 | 2790.5 | +4.7% | 2719.0 | 8136; 22 | 8136; 22 |
| f64_add | 2738.0 | 2773.5 | +1.3% | 2748.0 | 8136; 22 | 8136; 22 |
| f32_nearest | 2544.5 | 2677.5 | +5.2% | 2974.5 | 8088; 21 | 8920; 23 |
| f64_nearest | 2569.5 | 2597.5 | +1.1% | 3033.0 | 8088; 21 | 8920; 23 |
| f64_convert_i64 | 2632.5 | 2650.0 | +0.7% | 2683.5 | 8088; 21 | 8088; 21 |
| i64_clz | 2534.5 | 2577.5 | +1.7% | 2611.0 | 8088; 21 | 8088; 21 |
| i64_ctz | 2540.0 | 2561.5 | +0.8% | 2608.5 | 8088; 21 | 8088; 21 |
| i64_popcnt | 2551.0 | 2582.0 | +1.2% | 2703.0 | 8088; 21 | 8088; 21 |
| memory_copy | 4712.0 | 4692.5 | -0.4% | 4270.5 | 10680; 31 | 9528; 30 |
| swizzle | 2982.5 | 3037.5 | +1.8% | 4181.0 | 8200; 24 | 10472; 27 |
| i32_mul | 2936.0 | 2929.5 | -0.2% | 3149.0 | 8176; 23 | 8176; 23 |
| i64_gt | 2950.5 | 2949.0 | -0.1% | 3117.0 | 8176; 23 | 8176; 23 |
| i32_min | 2888.0 | 2955.0 | +2.3% | 3374.5 | 8176; 23 | 9136; 25 |
| f64x2_nearest | 2679.5 | 2822.0 | +5.3% | 3713.5 | 8112; 22 | 9904; 25 |
| relaxed_q15 | 2859.5 | 2926.0 | +2.3% | 3645.0 | 8176; 23 | 9136; 25 |
| lane_extract | 2721.0 | 2767.0 | +1.7% | 2804.5 | 8112; 22 | 8112; 22 |

## Execution medians and emitted code

All times are ns/op. Modern code bytes apply to both main and the candidate.

| Workload | Main modern | PR modern | Change | PR SSE2 | Modern code bytes | SSE2 code bytes |
|---|---:|---:|---:|---:|---:|---:|
| f32_add | 20.41 | 20.15 | -1.3% | 19.88 | 54 | 59 |
| f64_add | 20.30 | 20.09 | -1.0% | 19.90 | 54 | 59 |
| f32_nearest | 20.26 | 19.97 | -1.4% | 19.68 | 46 | 231 |
| f64_nearest | 20.27 | 19.98 | -1.4% | 19.73 | 46 | 256 |
| f64_convert_i64 | 20.04 | 19.69 | -1.7% | 19.56 | 49 | 48 |
| i64_clz | 19.99 | 19.87 | -0.6% | 19.56 | 40 | 58 |
| i64_ctz | 19.98 | 19.77 | -1.1% | 19.79 | 40 | 50 |
| i64_popcnt | 20.38 | 19.77 | -3.0% | 19.67 | 40 | 127 |
| memory_copy | 31.98 | 32.27 | +0.9% | 32.09 | 673 | 567 |
| swizzle | 19.95 | 19.93 | -0.1% | 33.22 | 115 | 873 |
| i32_mul | 19.88 | 19.82 | -0.3% | 25.34 | 63 | 165 |
| i64_gt | 20.02 | 20.02 | -0.0% | 25.09 | 68 | 163 |
| i32_min | 20.06 | 19.86 | -1.0% | 27.52 | 63 | 245 |
| f64x2_nearest | 20.06 | 19.77 | -1.5% | 28.65 | 63 | 522 |
| relaxed_q15 | 20.14 | 19.87 | -1.4% | 31.97 | 63 | 425 |
| lane_extract | 20.00 | 19.81 | -0.9% | 20.05 | 57 | 72 |

## Reproduction

Build each revision's AMD64 backend test binary with `go test -c`. Copy the
focused benchmark into the reference checkout and adapt only its options as
specified above. Run each binary with the following arguments, substituting
`false` for the candidate SSE2 profile:

```sh
GOMAXPROCS=1 taskset -c 14 ./amd64.test \
  -test.run='^$' \
  -test.bench='^BenchmarkAMD64Tiers$/./modern=true/' \
  -test.benchmem -test.benchtime=250ms -test.count=1
```

Repeat eight rounds in alternating profile order. Append each profile's output
to its own file. Compare the modern result files with `benchstat`.
CPU 14 must exist and be permitted by the test machine's affinity mask.

Raw output and the comparison from this run are retained in the working
environment under `/tmp/wago-sse2-perf-repeat/`: `main-modern.txt`,
`pr-modern.txt`, `pr-sse2.txt` and `modern-benchstat.txt`.
