# PR #564 regression fixes

This is the follow-up to [the initial qualification](../pr564/README.md).
The initial data is unchanged. Two identified causes are fixed and the full
comparison is complete. This is not proof of zero regressions: one small
guard-page timing observation is inconsistent across repeats. Resource costs
and platform limits remain visible below.

## Source and method

- Pinned main: `a07de0973191efab1d32677eff527952c7f9cdd2`.
- Prior candidate production code: `16124d7639983fca0243f77486e32dc5ac74ab57`.
- Small-result fix: `6d378eefd`.
- Compiler allocation fix: `d006d3d10`.
- Linux AMD64, Ryzen 7 8845HS, Go 1.27.1, `GOMAXPROCS=8`, `GOGC=100`.
- No source changes to Wasm validation, feature admission, bounds, native entry,
  reference-token checks, or trap handling in these follow-up fixes.

The six previously reported timing cases use 12 alternating fresh-process
triples (main, prior candidate, fixed candidate), with 500 ms requested time.
The full suite uses six fresh-process pairs per top-level benchmark and bounds
build, with 100 ms requested time and ISA fixtures enabled. Builds and tests do
not overlap timed samples. Other user workloads remain active; host load is
recorded. Statistical tests are unadjusted screens, not proof of equivalence.

## Changes

Small result buffers now use two Go-owned slots inside their Instance. Larger
results retain an exact-sized heap buffer. This removes a tiny allocation and
prevents different instances' small result writes from sharing a cache line.
No result aliases native mapped memory. A retained inline result slice retains
the Instance; callers must copy results that outlive the next call or instance.
Host re-entry retains its separate save/restore buffers.

Small compiler arenas use body size plus a saturated instruction-density hint.
The hint uses padding; both function headers remain 28 bytes. Small overflow
fills to power-of-two totals instead of tripling storage after an underestimate.
Large growth, pointer/ID stability, and retention limits remain unchanged.
Sparse global hints start at one record, with the same serial/parallel capacity
contract and merge size checks.

## Focused results

The final 12-triple repeat has no statistically confirmed timing slowdown among
the six original cases. The last two rows are not proof of zero regression.

| Case | Main | Fixed | Change | Benchstat p |
|---|---:|---:|---:|---:|
| explicit parallel process / parse4 | 10.310 ns | 3.595 ns | -65.13% | <0.001 |
| explicit parallel independent / fib_iter | 20.870 ns | 6.066 ns | -70.94% | <0.001 |
| explicit parallel process / mulhi | 16.965 ns | 3.689 ns | -78.26% | <0.001 |
| explicit GlobalGet | 94.13 ns | 85.81 ns | -8.84% | <0.001 |
| explicit parallel independent / i64x2.shl | 177.6 ns | 183.3 ns | +3.21% | 0.225 |
| guard instantiate / zstd | 12.07 us | 12.41 us | +2.82% | 0.590 |

Parallel time is elapsed time divided by total operations, not call latency.
The same-binary diagnostic models adjacent small heap results with a forced
packed buffer. Six alternating pairs give these medians. This isolates the
layout effect in this small-call test, not every corpus runtime difference.

| Build / execution mode | Packed | Inline | Change | Exact rank-test p |
|---|---:|---:|---:|---:|
| explicit / process | 14.200 ns | 2.938 ns | -79.31% | 0.002 |
| explicit / independent | 14.165 ns | 2.934 ns | -79.29% | 0.002 |
| guard / process | 130.500 ns | 130.550 ns | +0.04% | 0.909 |
| guard / independent | 90.195 ns | 88.295 ns | -2.11% | 0.004 |

Six-triple compact-compile repeats reduce heap bytes versus main by 2.91% for
isa_call, 3.00% for isa_var, and 2.84% for isa_bulk_mem, with unchanged allocation
counts in those cases. Full compile of json-as retains a small resource cost:
about +0.43% heap and 407 allocations versus main's 405 (prior candidate: 406).
The one-iteration compact census finds no >1% heap increase across the corpus;
that census is a footprint check, not a timing measurement.

The full repeat confirms no >1% compact-compile heap increase across 60 cases
in either build. Full compile of many_funcs still uses about 1.57% more heap
than main and 85 allocations versus 83. This cost was also present in the prior
candidate; it is not removed by these fixes.

## Tests and limits

Full `go test ./src/...` passes in native and guard builds. Selected compiler
and runtime race tests pass. Benchmark-module tests pass. Focused runtime tests
also pass with Go 1.22.12. The full ARM64 backend suite passes under QEMU, as do
the new result-storage checks. An expanded ARM64 host-reentry selection fails on QEMU's native deadline
timer; the same test fails with the prior binary. The failure is retained and
the timer check was not weakened. Native ARM64 speed is not measured here.

The first Go 1.22 test launch inherited a Go 1.27 GOROOT and failed to build;
clearing that override fixes it. The first CLI smoke used the user's settings
and rejected an unknown setting; a scoped test settings directory fixes it.

The standard Go release binary sizes are unchanged from the prior candidate.
TinyGo grows by 96 bytes to 2,246,016 bytes; all profiles remain below their
checked-in size budgets. The original manager/TinyGo increases versus main are
not removed. Runtime profile smoke tests return the expected fib result.
The root Wine installer failures from the initial qualification are outside the
changed source packages and have not been hidden or declared fixed.

## Full-suite results

All 1,020 benchmark processes passed. All 16,144 paired metric rows have six
samples per revision; there are no missing rows or sample-count gaps. The
SQL fixture and opt-in ablation remain visible skips, not passing measurements.

The table uses unweighted geometric means of per-case median ratios. It omits
ISA cases so they do not dominate the application/compiler summary. Negative
changes are faster. Aggregates do not replace checks of individual warnings.

| Stage | Non-ISA rows | Explicit | Guard |
|---|---:|---:|---:|
| Decode | 42 | -10.17% | -11.33% |
| Validate | 42 | -13.76% | -13.42% |
| ValidateWorkers | 28 | -10.40% | -10.98% |
| Compile | 42 | -17.27% | -16.10% |
| CompileCompact | 42 | -11.91% | -12.57% |
| CompileFull | 42 | -24.85% | -23.90% |
| CompileWorkers | 36 | -26.16% | -25.34% |
| CompileFullWorkers | 45 | -34.00% | -36.08% |
| CompileMultiModuleThroughput | 6 | -27.90% | -30.68% |
| Instantiate | 36 | -1.77% | -0.44% |
| Exec | 46 | +0.99% | -0.93% |
| ExecParallel | 72 | -23.09% | -0.18% |
| PluginInstantiate | 5 | -0.53% | -2.64% |
| PluginExec | 5 | +1.14% | -1.15% |

The screen found 37 Wago timing warnings: a positive change with unadjusted
p < 0.05, or an observed increase above 5%. Fifteen meet the first condition.
All 37 receive 12 fresh alternating pairs with 500 ms requested time. Their
repeats are complete. No explicit warning confirms. One guard case has an
unadjusted significant increase: process-mode float.run, +1.22%, p=0.011.

A further pre-selected check compares main, the prior candidate, and the fixed
candidate with 12 fresh alternating triples and one-second samples. It does
not reproduce a clear slowdown. No source changed between these checks; the
first result is retained and is not described as a fixed defect.

| Guard process-mode float.run check | Main | Fixed | Change | Benchstat p |
|---|---:|---:|---:|---:|
| 12 pairs, 500 ms, 8 workers | 4,475.5 ns | 4,530 ns | +1.22% | 0.011 |
| 12 triples, 1 s, 8 workers | 4,518 ns | 4,527 ns | +0.20% | 0.183 |
| 12 triples, 1 s, 1 worker | 4,108.5 ns | 4,105 ns | -0.09% | 0.899 |

The noisy guard process bulk-copy case is +7.02% in the first repeat
(p=0.203), then -5.18% in the longer triples (p=0.887). Its variance does not
establish either a speedup or a regression. The one-worker check is a separate
diagnostic; it is not pooled with eight-worker data. These tests are unadjusted
and run on a shared host. A small guard-mode regression cannot be ruled out.

All 298 Wago native-code-size rows are equal to main. Size equality does not
prove byte equality or semantic equivalence. The earlier ARM64 size census
remains separate; this follow-up does not measure native ARM64 speed.

Build labels describe the selected runtime default. Direct backend helpers
still force explicit bounds in both builds. Batched execution allocation
counters remain per batch; the comparison also derives per-call values.
PluginExec stops the allocation timer, so its printed zeros are not allocation
measurements. Whole-process RSS is not isolated compiler temporary memory.

The largest timed-run RSS is 1,802,088 -> 2,073,592 KiB in explicit builds and
1,882,512 -> 2,086,084 KiB in guard builds. All four peaks occur in compact
compilation. These are maxima from different calibrated processes, not equal
amounts of compiler work. A separate equal-work census uses one iteration per
case and six alternating explicit-build pairs: median maximum RSS is
151,534 -> 149,962 KiB, about -1.04% (p=0.065), with no >1% per-case heap increase.
The raw timed peaks remain recorded; the census does not erase them or measure
an isolated hint-merge peak. Census times are excluded from speed claims.

## Saved evidence

- [Full metric comparison](full/comparison.tsv.gz),
  [stage summaries](full/summary.json), and
  [coverage and repeat checks](full/qualification.json).
- [All resource increases](full/resource-increases.tsv), including noisy
  amortized counters, and [equal-work RSS](fixed-work/rss-summary.json).
- Full process output and resource logs in `full/raw.tar.gz`; raw Go benchmark
  streams and benchstat tables are compressed alongside them.
- `final/` holds the six original timing-case repeats; `compiler-final/` holds
  focused compiler repeats. `confirm-explicit/` and `confirm-signals/` hold all
  37 new warning repeats. `remaining-triples/` and `remaining-one/` hold the
  further checks. `result-layout*/` hold the same-binary diagnostics.
- `recheck/`, `inline/`, `half/`, and the arena/density/bounded censuses retain
  the investigation. Experimental labels are not the final implementation.
- `tests/` retains passing and failing captures. `size/` retains the commands,
  logs, and sizes for four release builds, plus the three runtime smoke checks.
  Built executables are not stored in this directory. `scripts/`, `manifest.json`, and
  `host.jsonl.gz` record commands, source/binary identity, and host conditions.

The first full benchstat export exceeded the Node wrapper's output buffer.
Increasing that analysis-only buffer completed the export; no benchmark sample
was changed. SHA256SUMS covers this evidence. The original PR #564 evidence is
kept in the sibling directory and verified separately.

## Release limit

The large runtime and small-arena regressions have targeted fixes and passing
native checks. Do not use this report to claim every timing or footprint
regression is eliminated. The small guard-mode timing uncertainty needs a quiet
native-runner check for a zero-regression release gate. Native ARM64 timing,
the QEMU timer limitation, and the earlier Wine installer failures are not
qualified away by these changes.
