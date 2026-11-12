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

The script requires a clean working tree, then builds exact main `dc13730184e8`
and the current revision before measurement, copying the same measurement tests
into the base worktree. It uses
one worker, GOMAXPROCS=1, Linux CPU affinity (Darwin uses its scheduler),
and one alternating fresh-process pair for each
of 20 pinned wasm.fyi inputs per phase. `bench/suite/testdata/cold-compilation/manifest.json`
contains input hashes and result contracts; command cases use the pinned corpus
catalog's input/output checks. No code export occurs before the first result.

Each invocation prints a new `.validation/cold-comparison/run-*/` directory containing
source/toolchain/host identity, raw samples, per-input
ratios, native-code hashes and a 10,000-iteration tiny compile/close probe. The
manual **Cold compilation review** workflow runs native regression tests first
and uploads the same artifacts.
Failed or timed-out samples retain captured output and their phase, revision and
input label in `raw.log`. Each run owns its files; a failed rerun cannot inherit
an earlier successful summary. Evidence-handling regressions can be checked with
`python3 -B -m unittest discover -s bench/suite -p test_compare_cold.py -v`.

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


## Native ARM64 follow-up, 2026-10-24

Apple M4 Max, Darwin 25.6.0, Go 1.27.1, defaults, one worker and GOMAXPROCS=1.
Main `dc13730184e8` versus compiler source `3f34c3b36868`; one alternating pair per
input/phase, under the exclusive Apple device-schedule lock, without Darwin CPU
affinity. All 80 oracles passed; all 20 native hashes matched in both phases.
[Recorded pairs, identity and totals](../../bench/suite/testdata/cold-compilation/arm64-review-2026-10-24.json).

| Metric | Main | PR | Delta |
|---|---:|---:|---:|
| Compile wall, total | 2.409 s | 1.815 s | -24.7% |
| Compile peak RSS, summed | 635.77 MiB | 626.08 MiB | -1.5% |
| Compile Go allocation traffic, total | 415.83 MiB | 363.80 MiB | -12.5% |
| Compile through first result, total | 4.456 s | 3.797 s | -14.8% |
| Warm execution, geometric mean | 1.061 ms | 1.077 ms | +1.6% |
| Tiny compile plus close | 7.838 us | 8.030 us | +2.4% |

An independent same-run control against previous head `c6ef64211772`, before the
final small-move shortcut, measured 1.901 to 1.858 s (-2.3%) across the same 20
inputs. Its exact results are retained separately in the measurement record.
The prior main comparison at `da6e10a36ee2` measured -25.6%; runner variation means
the new main percentage does not establish a larger headline win. The direct
previous-head control supports only a modest additional gain.

The retained changes reuse interleaved repeated adapter shapes in a bounded
worker-local cache, reuse existing call roots, bypass unused omission proof
walks, reuse the operand sentinel, reject impossible GC recognizers early, and
avoid generic load/store and small register-move planning work. One-use types do
not promote the adapter cache. Heap staging, execution options and lightweight
prepared entry remain intact. Source ordering, encoder outlining, larger image
reservation, and ordinary-layout trap sharing were screened and rejected.

Single pairs are ballpark evidence on a shared host. RSS and warm execution are
effectively flat. Summed RSS is not one process's peak or total machine memory.
The fannkuch cold-compile sample regressed 30.4%; first-result samples regressed
1.0% for sha256 and 1.3% for polybench-gemm. Tiny compilation uses one 10,000-op
batch per revision (19,456 to 19,344 bytes/op; 73 to 72 allocations/op).
No universal speedup is claimed. AMD64 remeasurement remains deferred.

Full native ARM64 backend, encoder and public tests passed. The full checked
backend passed, including a final GOMAXPROCS=1, WAGO_SHARED_SCALAR=0 native-spike
run. One unrestricted checked run hit a foreign-stack split-stack crash;
alternating pre/post-shortcut verbose controls subsequently passed twice each.
The crash's cause was not established, and it is retained as a validation limit.

Review follow-up, 2026-10-30: the original crash trace and exact failing command
were not found in the retained local evidence. With unchanged compiler/runtime
source at `2ff70e21164a`, native Darwin ARM64 / Go 1.27.1 passed these commands
with no GOMAXPROCS or WAGO overrides:

```sh
go test ./src/core/compiler/wasm ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 -count=1 -timeout=5m
go test -tags=wago_regalloccheck ./src/core/compiler/wasm ./src/core/compiler/backend/railshot/arm64 ./src/core/encoder/arm64 -count=1 -timeout=5m
go test -tags=wago_regalloccheck ./src/core/runtime/arm64spike -count=1 -v -timeout=5m
```

These are fresh successful checks, not a reproduction or explanation of the
historical crash. No runtime fix is attributed to that unresolved report.
