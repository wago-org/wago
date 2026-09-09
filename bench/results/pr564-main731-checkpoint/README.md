# PR564 integration checkpoint: main versus 1d04b458f

This frozen checkpoint measures production head
`1d04b458fa512265e2b93711f5c355435a29f33e` against freshly measured main
`731e95ff2cda7309eaf6d956f1417066bf7f1b69`. Earlier reports keep their own
baseline identities. Do not combine their samples with these samples.

## Results

All 1,020 full-suite processes passed. There are 3,910 timing rows and 16,144
total metrics; every metric has six samples per version. The first screen has
40 positive timing warnings with p < 0.05. This checkpoint ended before longer
timing confirmation. Those warnings are carried into the next head's repeat
selection, not dismissed. Fixed-work checks are complete.

Cold eight-worker JSON compilation uses 555 allocations on main and 611.5 here
(+10.18%, p = 0.015). This prompted the next [worker allocation fix](../pr564-worker-closures/README.md).
Ruby's equal-work explicit-bounds instantiation takes 1,812,448.5 ns on main
and 1,589,730 ns here (-12.29%). Bytes fall from 302,067.5 to 86,626.5 per
operation, and allocations from 9,405 to 779. These are separate fixed-work
samples, not replacements for the full screen.

Full-suite geometric means of per-case median ratios, for the default non-ISA
corpus (negative is lower time):

| Stage | Cases per mode | Explicit | Guard pages |
| --- | ---: | ---: | ---: |
| Full compile | 42 | -22.77% | -23.59% |
| Compact compile | 42 | -11.53% | -12.34% |
| Instantiate | 36 | -2.53% | -0.17% |
| Serial execution | 46 | -0.11% | +0.93% |
| Parallel execution throughput | 72 | -22.99% | -23.49% |
| Plugin instantiate | 5 | -7.31% | -8.97% |

These are unweighted summaries, not a combined application time or a claim
that every case improves. In particular, the positive guard-page serial result
is not hidden by the compile gains. See the full rows for individual costs.

Full compilation of JSON, Lua, SQLite, esbuild, and Ruby is 28.67–36.77% faster
in these samples. Ruby full compile allocates 1,185,632 fewer bytes per
operation in each mode. Other compiler cases still have small summary-storage
and scratch costs; the memory tables include these increases.

- [All timings, with main and after numbers](all-timings-main-vs-after.csv).
- [All metrics, including bytes and allocation counts](all-metrics-main-vs-after.csv).
- [Full explicit-bounds timings](timings-explicit-main-vs-after.md).
- [Full guard-page timings](timings-signals-main-vs-after.md).
- [The original 23 cases, full-suite samples](listed-full.md).
- [Application timings, largest main time first](application-timings-by-main-time.md).
- [Fixed-work memory and Ruby measurements](fixed-work-memory-main-vs-after.csv).
- [Equal-work peak process memory](equal-work-rss-main-vs-after.csv).

## Method and full numbers

Linux AMD64, Go 1.27.1, `GOMAXPROCS=8`, `GOGC=100`. Both explicit bounds and
guard pages are measured. Each full-suite case has six fresh alternating
main/after pairs, with 100 ms requested benchmark time. ISA cases are enabled;
the optional optimization-ablation matrix is not enabled. Wazero control rows
are included. Binary hashes and build commands are in `metadata.json`.
The optional `BenchmarkSqliBenign` cannot run because its external `sqli.wasm`
fixture is absent. `skipped-benchmarks.csv` lists observed skips from the first
sample of each group and version; the raw logs preserve every sample.

The next head's repeat set is saved before sampling. It includes all 23 user-listed cases,
every positive Wago timing screen with p < 0.05, and increases over 3% where
main takes at least 1 ms. It also includes significant allocation increases
above 0.5% for cases using at least 64 KiB/op, or at least eight extra
allocations per operation. Repeats use 12 fresh alternating pairs and 500 ms
requested time. Allocation selection excludes execution counters, whose batch
normalization requires separate treatment.
Large absolute increases of at least 128 KiB/op or 128 allocations/op are
repeated even without significance in the first screen. No longer timing repeat
is claimed for this checkpoint.

All reported values are medians. P-values are unadjusted, two-sided exact
rank-permutation tests with ties. They are screening evidence, not proof of a
cause or of equivalence. Selecting a case from an earlier warning does not
erase that warning; the original and repeated samples are both retained.

`ExecParallel` measures throughput time, not one-call latency. Batched execution
allocation counters are preserved as reported, with extra per-call metrics
derived from each sample's batch size before taking medians. `PluginExec`
stops Go's allocation timer; its printed zeros are not allocation measurements.
Its workload has a fixed size, so a longer requested benchtime does not make
each reported workload longer.

Fixed-work compiler checks use one operation and are memory checks, not timing
claims. Ruby uses 128 timed instantiations per final sample, plus Go's initial
one-operation calibration on each version. Process peak RSS
includes setup, decoded corpus, Go heap, and native mappings; it is not retained
compiler memory. Full-suite adaptive-iteration RSS is kept as raw evidence,
but equal-work RSS is used for comparisons.

No agent-run builds, tests, or profiles run alongside the timed comparisons.
This is a shared host, not an isolated performance lab. The low-cost host log
records load and memory during the checks.

## Safety and release checks

The allocation fixes preserve full-width indexes, checked total sizes,
deterministic error order, independent worker storage, and fallback growth.
They do not remove validation, feature, bounds, reference, or trap checks.
The new shared ASCII name checks preserve the prior exact regular-expression
grammars and caller length limits. Differential, boundary, and zero-allocation
tests cover these checks.

The main integration keeps a machine-value proof rule on ARM64. An `i32` type
alone does not prove zero upper carrier bits. Dirty serialized parameters keep
address canonicalization, including through blocks. Proven values retain the
optimization. Parallel loop facts preserve serial ordering and capacity.

All 112 checked AMD64 native-code pairs match main byte for byte, in both bounds
modes. This is corpus evidence, not a proof for all modules. ARM64 correctness
was checked with the full backend tests under QEMU and Go 1.22, in both modes;
native ARM64 performance was not measured here.

Native and guard-page source suites, targeted parallel race tests, and bench
tests pass. Exact-head CI passed all required platform, race, GC, conformance,
TinyGo, and build-size gates. Darwin AMD64 checks build portability; that target
does not implement the native Wasm entry ABI. Human approval remains required.

Local full-root test failures remain visible in `checks/`: Wine bootstrap fails
on both the candidate and fresh main. A Wine installer test also reports
`readdir: Invalid function` locally on both. Native Windows CI passes. Earlier local
runs also used the wrong TinyGo path or stale user settings; pinned-tool and
isolated-settings checks pass. The self test passes with its expected home
environment. These focused reruns do not turn a failed full-root run into a
passing full-root run.

Release sizes use Go 1.22.12 and pinned TinyGo 0.41.1. The optimized minimal
TinyGo runtime and the optimized standard TinyGo runtime each passed 80
fresh-process smoke checks. The removed startup regex
path avoids the observed optimized TinyGo startup crash; this does not claim
to fix a general TinyGo compiler or collector defect. CI now checks optimized
minimal-runtime startup as well as its debug tests.

## Evidence

Commands, identities, raw samples, process status, checks, and hashes are saved
with this report. Scripts contain the original local paths; adjust paths when
reproducing. Restore code overlays from `.go.txt` only at their overlay target,
not inside this evidence directory. `SHA256SUMS` covers every exported file
except itself.
