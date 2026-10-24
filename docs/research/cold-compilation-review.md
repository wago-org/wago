# Cold-compilation review reproduction

This revision keeps decode/validation fusion, compact compiler scratch, batched
encoder writes and redundant-scan elimination. It retains the base ARM64
execution policies and lightweight entry. Source-order experiments and file-backed
code/data staging are outside this PR. Requested deferred compilation uses heap
staging, including compile-only Darwin callers.

Use Go 1.27.1 on native Linux AMD64. From the repository root:

```sh
python3 bench/suite/compare-cold.py
```

The script builds exact main `dc13730184e8` and the current revision before
measurement, copying the same measurement tests into the base worktree. It uses
one worker, GOMAXPROCS=1, one CPU, and one alternating fresh-process pair for each
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
