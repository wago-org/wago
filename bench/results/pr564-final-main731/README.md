# PR564: final allocation and performance qualification

Before is freshly measured main `731e95ff2cda7309eaf6d956f1417066bf7f1b69`.
After is production code `b4f2360f517641803249c7ed998f37d8b0ec82a2`.
This report adds evidence only. Earlier reports retain their own source
and baseline identities; their samples are not mixed into this comparison.

## Results

All 1,020 full-suite processes passed. The 16,144 metrics include 3,910 timing
rows, each with six samples per version. The full screen has 37 positive Wago
timing warnings with p < 0.05. The largest significant time increases are in
UTF conversion/validation throughput, not the long-running application lanes.

Default non-ISA geometric means of per-case median time ratios:

| Stage | Cases per mode | Explicit | Guard pages |
| --- | ---: | ---: | ---: |
| Full compile | 42 | -22.18% | -23.09% |
| Compact compile | 42 | -11.61% | -11.83% |
| Instantiate | 36 | -1.25% | -0.90% |
| Serial execution | 46 | +0.79% | -0.39% |
| Parallel execution throughput | 72 | -22.59% | -22.92% |
| Plugin instantiate | 5 | -7.67% | -7.70% |

These summaries do not mean every case improves. The positive explicit-bounds
serial result remains visible. Long-running listed cases such as Coremark and
wasm3 have lower medians in the full screen, but those small differences are not
significant in six samples. All 23 user-listed cases are repeated regardless.

The longer repeat covers 170 cases from both full screens and the user list.
All 4,080 repeat processes and all 504 fixed-work processes passed. Of the 170
repeated timing rows, 55 have higher branch medians and 11 have unadjusted
p < 0.05. No further optimization or diagnostic benchmark round was run, at
the user's request. This is not a zero-regression result.

The largest significant repeat timing increase is guard-page `isa_f32.min`:
33,202 -> 33,933 ns/op (+2.20%). Guard-page `isa_simd_i16x8.extend_high_s`
is 1,986.5 -> 2,069 ns/op (+4.15%). Four explicit-bounds tiny-entry cases
remain 3.37–5.95% slower, with absolute increases of 0.277–0.4805 ns/op.
All eleven are listed in the linked repeat-warning table; their causes are
not established by these measurements.

## Complete loss inventory

Before is main, not an earlier PR checkpoint. Every positive measured cost is
included, even when p >= 0.05. Bounds modes and repeat stages are separate rows;
these counts are not counts of unique workload names. A lower repeat median
does not erase a full-screen increase. The complete Wago inventory has 1,604
rows. Wazero controls and raw per-batch counter increases are separate.

| Samples | Metric | Increased rows | Unadjusted p < 0.05 |
| --- | --- | ---: | ---: |
| Full screen | Timing | 950 / 3,000 Wago timing rows | 37 |
| Full screen | Memory | 443 | 333 |
| Longer repeat | Timing | 55 / 170 repeated cases | 11 |
| Longer repeat | Memory | 86 | 64 |
| Fixed-work compiler and corpus checks | Memory | 55 | 30 |
| Equal-work process checks | Peak RSS | 15 | 4 |

The largest full-screen absolute timing increase is guard-page Ruby execution:
18.632097 -> 19.1429405 ms (+2.74%, p = 0.589). This is execution, not
instantiation. In the longer repeats, wasm3 execution is 23.1678295 ->
23.5019305 ms (+1.44%, p = 0.799); Coremark is essentially flat. Guard-page
bulk copy is 350.25 -> 352.1 ns/op (+0.53%, p = 0.755). These uncertain
positive medians remain in the inventory.

Memory costs remain material in some lanes:

| Check | Bounds | Benchmark | Metric | Main | Branch | Increase | p |
| --- | --- | --- | --- | ---: | ---: | ---: | ---: |
| Fixed 1x | Explicit | Full compile esbuild p4 | B/op | 186325204 | 188970520 | 2645316 (+1.42%) | 0.026 |
| Fixed 1x | Explicit | Full compile esbuild p2 | allocs/op | 22453.5 | 23979 | 1525.5 (+6.79%) | 0.041 |
| Repeat | Guard | Compile esbuild p4 | B/op | 120640542.5 | 121705945 | 1065402.5 (+0.88%) | 0.002 |
| Fixed 1x | Guard | Full compile SQLite p2 | B/op | 14091412 | 14475448 | 384036 (+2.73%) | 0.015 |
| Fixed 1x | Guard | Compile JSON p8 | Peak RSS KiB | 90210 | 94006 | 3796 (+4.21%) | 0.026 |

Fixed-work JSON p8 allocation counts are 573 -> 589 (+2.79%, p = 0.394)
with explicit bounds and 598.5 -> 590 (-1.42%, p = 0.974) with guard pages.
The twelve-pair explicit repeat instead has 629 -> 596 allocations, but
749,236 -> 752,627.5 B/op (+0.45%). These are separate cold and adaptive
measurements; neither is substituted for the other.

Ruby's fixed-work instantiation checks use 9,405 -> 779 allocations (-91.72%)
and about 302,069 -> 86,629 B/op (-71.32%) in both modes. Timing is
1.851169 -> 1.685132 ms explicit (-8.97%, p = 0.065), and
1.8720925 -> 1.634117 ms guard (-12.71%, p = 0.002). Guard peak process
RSS still increases 203,814 -> 204,948 KiB (+0.56%, p = 0.394). Less allocated
Go memory is not a claim that every process-memory measurement decreases.

- [Every Wago loss, readable full table](all-wago-losses-main-vs-branch.md).
- [Every Wago loss, complete CSV](all-wago-losses-main-vs-branch.csv).
- [Every slower Wago timing row, CSV](all-slower-wago-benchmarks-main-vs-branch.csv).
- [Wazero control increases](wazero-control-increases-main-vs-branch.csv).
- [Raw batch increases, not per-call loss claims](raw-batch-counter-increases-not-per-call-losses.csv).

Full compilation of JSON, Lua, SQLite, Ruby, and esbuild is 27.56–36.76% faster
in the full screen. For example, explicit-bounds Ruby full compile falls from
1.071 s to 0.699 s and from 41,519,488 to 40,333,856 allocated Go-heap bytes per
operation. Serial summary costs remain: full JSON compile uses about 954 extra
bytes and two extra allocations per operation in that mode. The complete tables
include both these costs and the gains.

- [All timings, with main and after numbers](all-timings-main-vs-after.csv).
- [All metrics, including bytes and allocation counts](all-metrics-main-vs-after.csv).
- [Full explicit-bounds timings](timings-explicit-main-vs-after.md).
- [Full guard-page timings](timings-signals-main-vs-after.md).
- [The original 23 cases, full screen](listed-full.md).
- [The original 23 cases, longer repeat](listed-focused.md).
- [All longer-repeat metrics](focused-main-vs-after.csv).
- [Positive timing warnings in the longer repeat](confirmed-timing-regressions.md).
- [Application timings, largest main time first](application-timings-by-main-time.md).
- [Fixed-work memory and Ruby measurements](fixed-work-memory-main-vs-after.csv).
- [Equal-work peak process memory](equal-work-rss-main-vs-after.csv).
- [Every positive measured compiler or instantiation memory cost](all-positive-memory-costs.csv).

## What changed

The last allocation fix shares each parallel phase's worker capture. Hint workers
also reuse eight private retained hint records; larger spans still grow. Worker
count, instruction scans, deterministic error order, full-width indexes, checked
total sizes, and serial/parallel resource accounting remain unchanged. Final
sidecars are detached copies. No validation, feature, bounds, reference, or trap
check was removed. [The focused report](../pr564-worker-closures/README.md)
records the red/green tests and compares main, the prior PR, and this candidate.

A serial hint gate saved allocations but slowed JSON compilation by about
5–17% versus the prior PR. It was rejected. Its measurements and patch remain
in the focused report. This is not a claim that every memory metric is below
main. Small validation-summary and scratch costs remain visible in the tables.

The [1d04 checkpoint](../pr564-main731-checkpoint/README.md) contains the full
run that found the cold eight-worker JSON allocation increase. All of that
checkpoint's timing and material memory warnings are carried into this head's
repeat selection. They are not silently dropped because a short screen changed.

## Method

Linux AMD64, Go 1.27.1, `GOMAXPROCS=8`, `GOGC=100`. Both explicit bounds and
guard pages are measured. The full suite uses six fresh alternating main/after
pairs, with 100 ms requested benchmark time. ISA cases are enabled; the optional
optimization-ablation matrix is not. Wazero controls are included. Build commands
and frozen binary hashes are in `metadata.json`.

The optional `BenchmarkSqliBenign` cannot run because its external `sqli.wasm`
fixture is absent. `skipped-benchmarks.csv` records observed skips from the first
sample of each group and version. Raw logs preserve every sample.

The repeat selection is saved before sampling. It includes all 23 user-listed
cases, positive Wago timing warnings with p < 0.05, and increases over 3% where
main takes at least 1 ms. Material memory screens include significant increases
above 0.5% for cases using at least 64 KiB/op, or at least eight extra allocations
per operation. Increases of at least 128 KiB/op or 128 allocations/op are repeated
even without significance, including new costs with a zero baseline. The
selection takes the union of this full screen and the prior 1d04 screen.
Repeats use 12 fresh alternating pairs and 500 ms
requested time. Execution allocation counters are excluded from this selection.

All values are medians. P-values are unadjusted, two-sided exact rank tests with
ties. They are warnings, not proof of a cause or of equivalence. Geometric means
are unweighted summaries of per-case ratios, not a combined application time.
Good compilation results do not cancel an execution slowdown.

`ExecParallel` measures throughput time, not one-call latency. Batched execution
allocation counters are retained as reported, with per-call metrics derived
from each sample's batch size before taking medians. `PluginExec` stops Go's
allocation timer; its printed zeros are not allocation measurements. Its timed
workload has a fixed size; a longer requested benchtime does not lengthen it.

Fixed-work compiler checks use one operation and support memory comparisons,
not timing claims. They include the full screen's largest esbuild byte/count
increases and the two-worker SQLite byte increase. Ruby uses 128 timed
instantiations per final sample, plus
Go's initial one-operation calibration on each version. Process
peak RSS includes setup, decoded corpus, Go heap, and native mappings; it is not
retained compiler memory. Adaptive-iteration RSS from the full suite remains
raw evidence. Equal-work RSS is used for comparisons.

No agent-run builds, tests, or profiles overlapped the timed comparisons. This
is a shared host, not an isolated performance lab. The host log records load
and memory. All samples remain saved, including positive changes.

## Correctness, portability, and release checks

Native and guard-page source suites, targeted parallel race tests, bench tests,
and both full Go 1.22 ARM64 backend suites under QEMU pass for the final code.
The initial native source commands used the wrong WABT version and then lacked
the pinned interpreter setting. Those failed setup logs remain beside the
passing correctly configured run; conformance expectations were not weakened.

All 112 checked AMD64 native-code pairs match main byte for byte in both bounds
modes. This is corpus evidence, not proof for all modules. Native ARM64 speed
was not measured. ARM64 retains machine-value proof for zero high carrier bits;
an `i32` declaration alone is not sufficient. Dirty serialized parameters keep
canonicalization, including across blocks. The branch therefore does not claim
unchanged ARM64 output relative to main's new optimization.

All enabled exact-head CI gates pass, including platform, race, GC, conformance,
TinyGo, and build-size checks. Conditional coverage and extended stress jobs are
skipped by the workflow; their status remains in the saved snapshot. Darwin
AMD64 checks build portability, not a supported native Wasm entry ABI. The PR
has no merge conflict but still needs human approval.

The local Wine installer test fails with `readdir: Invalid function` on both
main and the PR. Earlier full-root runs also had a Wine bootstrap failure on
both versions, plus tool-path and user-settings setup errors. The pinned-tool,
isolated-settings, and expected-home checks passed. Those focused checks do not
make a failed full-root run a pass. Earlier checks are explicitly kept under
`checks/prior/`; final-code checks are directly under `checks/`.

All eight release-size builds remain within budget. Sizes use Go 1.22.12 and
pinned TinyGo 0.41.1. Optimized minimal and standard TinyGo runtimes each passed
80 fresh-process startup/execution checks. Avoiding the old startup regex path
fixes the observed startup crash; this is not a claim to fix a general TinyGo
compiler or collector defect. Optimized minimal startup is also a CI gate.

## Evidence

Commands, identities, all samples, process status, checks, and hashes are saved
with this report. Scripts contain original local paths; adjust them to reproduce.
The repeat script also reads the prior checkpoint's full comparison. Restore
code overlays from `.go.txt` only at the overlay target, not in this evidence
directory. `SHA256SUMS` covers every exported file except itself.

