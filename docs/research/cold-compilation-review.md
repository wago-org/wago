# Cold-compilation review reproduction

This revision keeps decode/validation fusion, compact compiler scratch, batched
encoder writes and redundant-scan elimination. It retains the base ARM64
execution policies and lightweight entry. Source-order experiments and file-backed
code/data staging are outside this PR. Requested deferred compilation uses heap
staging, including compile-only Darwin callers.

Use Go 1.27.1 on native Linux AMD64/ARM64 or Darwin ARM64. From the repository root:

```sh
python3 bench/suite/compare-cold.py
```

The script builds exact main `dc13730184e8` and the current revision before
measurement, copying the same measurement tests into the base worktree. It uses
one worker, GOMAXPROCS=1, Linux CPU affinity (Darwin uses its scheduler),
and one alternating fresh-process pair for each
of 20 pinned wasm.fyi inputs per phase. `bench/suite/testdata/cold-compilation/manifest.json`
contains input hashes and result contracts; command cases use the pinned corpus
catalog's input/output checks. No code export occurs before the first result.

`.validation/cold-comparison/` contains source/toolchain/host identity, raw samples, per-input
ratios, native-code hashes and a 10,000-iteration tiny compile/close probe. The
manual **Cold compilation review** workflow runs native regression tests first
and uploads the same artifacts.

The compile phase reports compile wall time, Go allocation traffic, process peak
RSS before instantiation, and warm execution. The separate first-result phase
measures compile through first validated result, without intervening GC,
allocation profiling, or code export. It includes benchmark import/command setup
and oracle checks; command cases include their normal command lifecycle.
Pipeline allocations and RSS are labeled separately from compile-only metrics.
A fresh small shell forks each child to exclude inherited Python launcher RSS.

Single pairs provide ballpark evidence on the named runner, not universal timing
or per-input speed guarantees. No historical file-backed measurements describe
this revised default configuration. Historical records remain at commit
`8053b247ebd9b2be7e1f4b861e6a1ea76cfa3526` and workflow runs
[40073017826](https://github.com/wago-org/wago/actions/runs/40073017826) and
[40073536273](https://github.com/wago-org/wago/actions/runs/40073536273).

## Native ARM64 review rerun, 2026-10-24

Apple M4 Max, Darwin 25.6.0, Go 1.27.1, defaults, one worker and GOMAXPROCS=1.
Main `dc13730184e8` versus `da6e10a36ee2`; one alternating pair per input/phase,
under the exclusive Apple device-schedule lock, without Darwin CPU affinity.
All 80 oracles passed; all 20 native hashes matched in both phases.
[Recorded pairs, identity and totals](../../bench/suite/testdata/cold-compilation/arm64-review-2026-10-24.json).

| Metric | Main | PR | Delta |
|---|---:|---:|---:|
| Compile wall, total | 2.464 s | 1.834 s | -25.6% |
| Compile peak RSS, summed | 633.69 MiB | 629.52 MiB | -0.7% |
| Compile Go allocation traffic, total | 415.83 MiB | 363.77 MiB | -12.5% |
| Compile through first result, total | 4.460 s | 3.866 s | -13.3% |
| Warm execution, geometric mean | 1.102 ms | 1.100 ms | -0.2% |
| Tiny compile plus close | 8.604 us | 8.153 us | -5.2% |

RSS and warm execution are effectively flat at this sampling depth. Summed RSS
is a sum of process peaks, not one process's peak or total machine memory.
The raytrace compile sample regressed 12.5%; first-result samples regressed 0.8%
for fannkuch and 2.7% for coremark. These are single pairs on a shared host;
no universal speedup is claimed. Tiny compilation uses one 10,000-operation batch
per revision (19,456 to 19,344 allocated bytes/op; 73 to 72 allocations/op).
AMD64 remeasurement is deferred.
