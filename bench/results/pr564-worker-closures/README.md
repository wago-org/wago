# PR564: reuse parallel worker captures and small retained spans

The compiler keeps the same worker count and the same validation, hint scans,
error order, index checks, and output order. Each parallel phase now creates its
captured worker function once. Each hint worker has private inline space for
eight temporary retained global hints. Larger spans still grow. Final sidecars
are detached copies with the same serial/parallel capacity contract.

Main is `731e95ff2cda7309eaf6d956f1417066bf7f1b69`. The prior PR control is
`1d04b458fa512265e2b93711f5c355435a29f33e`; it is not the main baseline.
The candidate is that PR head plus `hint-scratch-source.patch`. Each run's
`identity.json` records all three frozen binary hashes.

## Findings

Warm allocation counts fall below main in the JSON, Lua, and SQLite checks.
Compared with the prior PR, the selected warm timings range from -1.93% to
+1.15%; none of these differences has p < 0.05. This is not proof of equivalent
speed, but the large slowdown from the rejected experiment does not recur.

Examples from the explicit-bounds warm checks:

| Benchmark | Main ns/op | After ns/op | Main allocs/op | After allocs/op |
| --- | ---: | ---: | ---: | ---: |
| CompileWorkers/json-as/p4 | 500,973.5 | 331,116 | 402.5 | 379 |
| CompileWorkers/json-as/p8 | 468,357 | 299,814.5 | 629 | 595.5 |
| CompileFullWorkers/json-as/p8 | 1,216,191 | 607,873.5 | 965.5 | 937 |
| CompileWorkers/lua/p8 | 6,431,737.5 | 3,166,931.5 | 2,212 | 2,131 |
| CompileWorkers/sqlite3/p8 | 22,343,903.5 | 10,008,074 | 4,501.5 | 4,471 |

Cold eight-worker JSON counts are 573 on main, 626 on the prior PR, and 581
after this change. The remaining +1.40% versus main is not significant in this
six-sample check (p = 0.621). Cold and warm counts must stay separate.
Some byte-count increases and serial validation-summary costs remain; this is
not a claim that every allocation metric is below main.

A guard-page esbuild allocation warning appeared in the warm screen. A separate
12-triple check used exactly three compiles per process. Its guard-page counts
were 27,862 on main, 27,991 on the prior PR, and 28,091.5 after the change
(+0.82% versus main, p = 0.319). Allocated bytes were +0.27% versus main
(p = 0.068), and time was +0.47% versus the prior PR (p = 0.266). The larger
warning did not recur at equal work, but the positive medians remain recorded.

The one-operation allocation profile attributes 16 allocations to each old
eight-worker launch site, versus nine after sharing its capture. In the sampled
hint phase, total allocations fell from 43 to 28 and allocated bytes fell from
16.48 to 15.67 KiB. Profiles are diagnostic and are not timing measurements.

## Rejected experiment

Requiring 16 KiB of body bytes per hint worker sent small JSON modules through
the serial hint scan. It saved allocations, but the explicit-bounds warm JSON
checks became about 5–17% slower than the prior PR. That policy is **not** in
this change. Its code patch and every measured result are retained, with the
`hintwork` label, in the same data file.

## Method and evidence

- [All 232 candidate and rejected-experiment metrics](all-measurements.csv).
- [Equal-work esbuild peak process memory](equal-work-esbuild-rss.csv).
- `hintscratch-*`: the accepted scratch/capture candidate.
- `hintwork-*`: the rejected per-worker work gate.
- Six fresh alternating main/prior/candidate triples per cold or warm case.
  Cold uses one operation and is not a timing claim. Warm requests 500 ms.
- Esbuild confirmation uses 12 triples and exactly three operations per process.
- Linux AMD64, Go 1.27.1, `GOMAXPROCS=8`, `GOGC=100`, both bounds modes.
  No agent-run builds, tests, or profiles overlapped the timed samples.
- All values are medians. P-values are unadjusted exact two-sided rank tests
  with ties. This is a shared host, not an isolated performance lab.

The new test first failed on missing inline capacity, then passed with the
change. It checks independent storage, growth beyond eight records, preservation
of prior records, and zero-allocation small-span reuse. Serial/parallel hint,
metadata, and error tests pass. Native and guard-page source tests, targeted race
tests, bench tests, and full Go 1.22 ARM64 backend suites under QEMU pass.
Native ARM64 speed is not measured here.

The first native source command used WABT 1.0.36 instead of pinned 1.0.41;
the next lacked the pinned interpreter setting. Both failed logs are retained.
The correctly configured source suite passes. No conformance expectations were
updated to make these checks pass.

Raw per-process logs are in `raw-process-logs.tar.gz`. Scripts contain original
local paths; adjust them to reproduce. `SHA256SUMS` covers all exported files
except itself. Full final-head qualification is separate from this focused
allocation checkpoint.
