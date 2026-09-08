# PR #564 qualification data

See the repository's [REPORT.md](../../../REPORT.md) for conclusions and limits.
These files preserve the numbers for later comparisons; historical PR claims
and the new native measurements are separate data sets.

## Revisions and host

- Baseline: `a07de0973191efab1d32677eff527952c7f9cdd2` (`origin/main` when pinned).
- Candidate production code: `16124d7639983fca0243f77486e32dc5ac74ab57`.
  Later test and report commits do not change production code.
- Reviewed PR: `ab29bf3a9ad215833b5138220de6cd7190461a78` against
  `447f057115ee04d9e58580061dbee696becec21f`.
- Native timing host: Linux AMD64, Ryzen 7 8845HS, Go 1.27.1.
- Environment: `GOMAXPROCS=8`, `GOGC=100`; no `GOMEMLIMIT` override.

`provenance.json` contains binary hashes, dependency/build details, commands,
and interpretation notes. `environment.txt` contains CPU/platform details.
The benchmark binaries were built from the pinned source before timing, not
rebuilt between samples. The default benchmark paths are unchanged between
revisions; the candidate adds a disabled opt-in ablation benchmark.

Display-only trailing spaces are removed from benchstat tables, and trailing
empty lines are normalized in the git-log capture. Benchmark streams, the PR
description, and the original report retain their captured bytes. The TSV
comparison uses `paired` for a normal matched metric's status.

## New measurements

| File | Contents |
|---|---|
| `base-explicit.txt.gz`, `candidate-explicit.txt.gz` | Full explicit-build raw benchmark streams |
| `base-signals.txt.gz`, `candidate-signals.txt.gz` | Full guard-build raw benchmark streams |
| `comparison.tsv`, `comparison.json` | Every paired metric, sample counts, medians, percent changes, and exact rank-test p-values |
| `summary.json` | Geomeans by stage, unit, bounds/build mode, and corpus scope |
| `benchstat-explicit.txt`, `benchstat-signals.txt` | Standard benchstat analysis of the raw streams |
| `regression-screen.json` | Positive first-pass timing changes with p < 0.05 |
| `confirmation-*.txt.gz`, `confirmation-summary.json` | Longer alternating-order repeats and their statistics |
| `regressions.tsv`, `regressions.json` | All observed increases, including unconfirmed and small changes; no percent cutoff |
| `confirmed-timing.md` | Timing rows still slower at p < 0.05 in the repeat |
| `process-captures.tar.gz` | Individual process logs, exit status, and GNU time peak-RSS records |
| `run-summary.json` | Process totals, failures, largest RSS, and skip messages |
| `host-load.jsonl.gz` | Host load/memory-pressure samples, starting during the explicit run |
| `test-*.txt.gz`, `test-*.status.json.gz` | Tests and focused type-cache/type-resolution microbenchmarks, including failed attempts |
| `initial-base-*.gz` | Failed/stopped original unsplit baseline runs, excluded from paired statistics |
| `initial-candidate-*.gz` | Duplicate first aggregate before the guard-only supplement; these are not failed runs |
| `arm64-code-size.tsv` | ARM64 code-size census, if present; no emulated timing claim |
| `build-size.tsv`, `build-size.json` | Go 1.22.12/TinyGo 0.41.1 binary sizes, budgets, tools, and commands |
| `build-size-initial.json`, `build-size-*.txt.gz` | Initial VCS-stamping failure and successful build logs |
| `resource-diagnostics.json` | Instrumented structural counters for four heap-increase cases; not normal timing results |
| `confirmation-pause.json` | Drained timing pause for correctness/tool checks and its one wrapper-duration caveat |
| `single-worker-*.txt.gz`, `single-worker-summary.json`, `benchstat-single-worker.txt` | Separate one-worker diagnosis of four repeated timing regressions |
| `test-native-census-*.gz` | Native-code hashes, exact entry values, and selected prepared-call flags for four modules in two configurations |
| `test-summary.md` | Retained test-command outcomes, including failed attempts |

Six samples per revision use one top-level benchmark and sample per process,
100 ms requested benchmark time, and alternate baseline/candidate order.
The full default suite includes `-wago.bench.isa`. The guard-only memory benchmark
is discovered from the guard build, not from the explicit benchmark list.

Timing candidates are repeated in 12 fresh pairs with 300 ms requested time:
all positive changes with first-pass p < 0.05, plus all slowdowns above 5%.
The exact rank permutation test handles tied values. This is a broad screen,
not a multiple-comparison-adjusted guarantee or a dedicated-host experiment.

The mode label is the build and runtime default. Low-level backend and
standalone JSON benchmarks keep their explicit-bounds setup in both builds;
decode/validation and wazero are bounds-independent controls. Public full
compile and normal execution paths use the selected runtime bounds mode.

The execution harness reports time per call but Go allocation counters per
batch. Derived `B/call` and `allocs/call` divide each raw sample by its reported
`calls/batch` before taking medians. PluginExec reports a manually timed whole
workload with Go's timer stopped; its zero allocation counters are unmeasured,
not proof of allocation-free execution.

ExecParallel's B/op and allocs/op also include one-time RunParallel worker and
closure setup divided by the calibrated iteration count. Small changes in those
counters are not isolated steady-state invocation-allocation measurements.

RSS is a whole-process high-water mark, not an isolated compiler structure or
per-operation footprint. Sample iteration counts can differ between revisions.
Do not infer hint-scan peak-memory savings from those RSS values alone.

## Reuse and checksums

To inspect the standard comparison, decompress the two matching captures into
a temporary directory and pass them to `benchstat`, baseline first. The recorded
benchstat version is `golang.org/x/perf` at
`v0.0.0-20260825160852-19be9d8e6c70`.

`scripts/` preserves the exact collection and analysis programs, including their
original local paths. To repeat collection, build both pinned revisions in
separate worktrees, with and without `-tags wago_guardpage`, then update the
script's binary/output/corpus paths. Keep its process boundaries and environment
the same. Do not run other tests, builds, or benchmarks alongside timing.
The saved `native-census.go.txt` is source for the diagnostic program; restore
its `.go` extension outside the result tree before running its build script.

`SHA256SUMS` covers the evidence files and scripts. From this directory, run
`sha256sum -c SHA256SUMS` to verify them. The original test binaries are not
checked in; their SHA-256 values and build details are recorded.

## Historical inventory and coverage limits

`inventory-summary.md`, `metric-inventory.tsv`, and `metric-inventory.json`
index reported claims from the PR description, report, commit messages, and
attached CI build-size artifact. Full source snapshots and CI logs are retained.
The CI size artifact used Go 1.22.12/TinyGo 0.41.1 and the PR's CI merge commit;
the historical Apple ARM64 and Rosetta timings used different checkpoints and
hardware. Do not combine them with these native Linux samples.

The external Impart SQL-rule fixture was unavailable. Its benchmark skip is
retained. The candidate-only, opt-in optimization-ablation matrix was not enabled.
Native ARM64 speed is not qualified by this AMD64 host; QEMU results qualify only
the selected correctness checks and code sizes. The full root test run's Wine
and TinyGo failures remain visible in its logs; see the report for rerun results.
