# Instantiation allocation follow-up — measured checkpoint 3a84fa628

The full-suite comparison, longer warning repeats, and final timing diagnostics
and compiler memory checks are complete. Release qualification has open issues.
Three small timing costs remain versus main. This is not a zero-regression or
shipping claim.

Before is freshly measured main `a07de0973191efab1d32677eff527952c7f9cdd2`.
After is production code `3a84fa628ac16ac5502a7de986f2fe732007583d`.
The prior candidate `d006d3d105dccd8a9f2ae6e0d2442a99201c9ba0` is not the baseline.

## Change and safety checks

The integer-ABI classifier now scans parameter and result slices directly.
It keeps the same eight-parameter, two-result limit and accepts only i32/i64.
The local register-ABI result is reused within the same function descriptor.
No feature, native-entry, or staged-tail-call gate was removed.

Reference type-key storage starts from the smaller of the function count and
the nonzero declared-type count. This is a capacity hint, not a limit. The
slice still grows for distinct legacy keys. Exact structural collision checks,
owner registration, and release rules are unchanged. Required linear-memory
initialization and copying are also unchanged.

Tests cover every byte-valued type in every parameter/result position around
the ABI limits, zero classifier allocations, repeated-type capacity, distinct
legacy-key growth, and final-owner release. The two allocation tests fail
against the prior source for the intended reasons. Those failures are retained
as evidence, not suppressed.

Fresh post-reboot checks passed: the full native and guard `./src/...` suites,
focused runtime race tests, the bench module tests, focused Go 1.22.12 checks,
and focused ARM64 tests under QEMU in both builds. This does not clear the
earlier root-suite Wine failures or the ARM64 host-reentry emulation limit.
Native ARM64 timing is not available on this AMD64 host.

All 298 wago generated-code size rows in the full comparison match main.
Separate byte hashes also match for CoreMark, bulk memory, wasm3, and Ruby
in both builds. All 108 pairs in the wider executable-corpus byte audit match.
Neither equal size nor equal code bytes replaces semantic and metadata tests.

The prior-candidate Ruby allocation profile identified the ABI classifier and
type-key storage as useful targets. It covers the whole benchmark process,
including setup and compilation, so its percentages are not an isolated
instantiation breakdown. A focused eight-parameter ABI helper check falls from
31.89 to 5.2835 ns/op and from 24 bytes / two allocations to zero. This helper
comparison is prior-candidate versus fixed source, not the main baseline used
by every full-suite and application table.

## Full numbers

- [All 3,910 timing rows](all-timings-main-vs-after.csv).
- [All 16,144 metric rows](all-metrics-main-vs-after.csv).
- Readable [explicit-build timings](timings-explicit-main-vs-after.md) and
  [guard-build timings](timings-signals-main-vs-after.md).
- [The 23 user-listed cases, first pass](listed-full.md).
- [The 23 user-listed cases, twelve-pair repeats](listed-focused.md).
- [All longer-repeat metrics](focused-main-vs-after.csv) and the
  [six repeat timing warnings](confirmed-timing-regressions.md).
- [Application times, largest main time first](application-timings-by-main-time.md).
- [Final timing diagnostics](timing-diagnostics.md) and [remaining warnings](remaining-timing-warnings.md).
- [All memory diagnostics](resource-diagnostics-main-vs-after.csv), including
  fixed-work runs, warm runs, and diagnostic prior-candidate columns.
- [Whole-process RSS](full-process-rss-summary.csv) and
  [executable byte audit](code-audit/audit-all-summary.json).

All 1,020 full-suite processes passed. Every paired metric has six samples per
revision. Linux AMD64, Ryzen 7 8845HS, Go 1.27.1, GOMAXPROCS=8, GOGC=100.
The run alternates fresh processes with 100 ms requested time and ISA fixtures
enabled. It ran from 2026-09-08 21:37 to 23:56 UTC. Other user work remained
active; the host was not isolated.

Values are medians; negative changes mean faster or smaller. P values are
unadjusted two-sided rank tests, not proof of equivalence. Longer repeats are
kept separate from the six-sample screen. Positive changes are not removed.
Opt-in ablation and the missing SQL fixture remain visible skips.

## Full-suite stage summary

These are unweighted geometric means across non-ISA rows. Worker settings and
entry points repeat some modules, so row counts are not independent
applications. All individual values, including ISA and positive changes, are
in the linked tables. Throughput rows are not single-request latency.

| Stage | Rows per build | Explicit change | Guard change |
|---|---:|---:|---:|
| Decode | 42 | -10.19% | -10.74% |
| Validate | 42 | -12.91% | -12.82% |
| ValidateWorkers | 28 | -10.09% | -10.09% |
| Compile | 42 | -16.46% | -16.83% |
| CompileCompact | 42 | -12.41% | -10.54% |
| CompileWorkers | 36 | -25.57% | -25.57% |
| CompileFull | 42 | -24.29% | -24.93% |
| CompileFullWorkers | 45 | -34.47% | -35.24% |
| CompileMultiModuleThroughput | 6 | -28.77% | -30.08% |
| Instantiate | 36 | -1.14% | -0.90% |
| Exec | 46 | +1.33% | +0.18% |
| ExecParallel | 72 | -21.25% | +0.39% |
| PluginInstantiate | 5 | -11.58% | -11.48% |
| PluginExec | 5 | -1.53% | -1.17% |

Unchanged-engine Wazero controls also move. Their non-ISA geometric means are
-1.04% / -3.93% for compilation, -0.57% / -0.31% for instantiation, and
-2.14% / -0.11% for execution (explicit / guard builds). These are evidence of
measurement variation, not correction factors for wago samples taken at other
times. Small timing changes need the separate longer checks.

## Plugin instantiation repeats

These are twelve new alternating pairs, 500 ms requested time. They are not
pooled with the full-suite samples. All ten instantiation time changes have
p < 0.05. Each build uses its own freshly measured main samples.

### Explicit bounds

| Instantiation | Main ns/op | After ns/op | Time change | Main B/op | After B/op | Main allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|---:|
| Ruby | 1,806,119 | 1,605,583 | -11.10% | 294,923 | 79,067 | 9,168.5 | 529 |
| esbuild | 1,233,077 | 1,166,892.5 | -5.37% | 87,628.5 | 13,571 | 4,324 | 177 |
| SQLite | 237,107 | 209,414.5 | -11.68% | 67,893.5 | 36,599 | 1,184 | 250 |
| wasm3 | 157,603 | 144,112 | -8.56% | 34,199 | 22,628.5 | 823 | 213 |
| Lua | 138,072 | 128,898.5 | -6.64% | 30,186.5 | 21,696.5 | 545 | 196 |

### Guard bounds

| Instantiation | Main ns/op | After ns/op | Time change | Main B/op | After B/op | Main allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|---:|
| Ruby | 1,906,399.5 | 1,695,779 | -11.05% | 295,235 | 79,223.5 | 9,179 | 534.5 |
| esbuild | 1,394,256 | 1,285,845 | -7.78% | 87,668.5 | 13,632.5 | 4,326.5 | 180.5 |
| SQLite | 231,527.5 | 210,254 | -9.19% | 67,888 | 36,598.5 | 1,184 | 250 |
| wasm3 | 89,339 | 80,167 | -10.27% | 34,186 | 22,616 | 823 | 213 |
| Lua | 75,933 | 70,824 | -6.73% | 30,178 | 21,689 | 545 | 196 |

The timed allocation counters include first-instance preparation amortized
over the calibrated operation count. Separate six-triple, 128-operation checks
confirm the heap savings: guard Ruby is 302,069 -> 86,626 B/op (-71.32%) and
9,405 -> 779 allocs/op. The prior candidate uses 302,060.5 B/op and 9,404
allocs/op in that check. B/op is Go heap allocation, not total resident memory
or Wasm linear memory.

Explicit CoreMark is 32,827,641 -> 32,740,149 ns/op (-0.27%, p=0.347) in
the longer repeat. Its full-suite increase did not repeat. None of the five
explicit plugin execution cases shows a positive change with p < 0.05 in
these twelve pairs. This is not proof of exact timing equivalence.

The complete longer repeats passed 177 explicit and 171 guard cases: 8,352
fresh processes. None of the 23 user-listed cases has a positive timing change
with p < 0.05 in these repeats. Six other warnings were selected for final
diagnostics, along with the large wasm3 execution case.

The guard wasm3 execution repeat is 23,017,639.5 -> 23,370,330.5 ns/op (+1.53%,
p=0.932). The separate final diagnostic is 23,402,883.5 -> 22,854,796.5 ns/op
(-2.34%, p=0.136). This change of direction is not claimed as a speedup or fix.

## Remaining timing costs

Twenty-four fresh alternating main/prior/after triples, one second requested
time. Prior is `d006d3d10`, not the baseline. These three positive changes have
unadjusted p < 0.05 versus main. None has p < 0.05 for an increase versus prior.
They remain visible; the instantiation fix does not remove every earlier cost.

| Bounds | Benchmark | Main ns/op | Prior ns/op | After ns/op | Main-to-after | p vs main |
|---|---|---:|---:|---:|---:|---:|
| explicit | ExecParallel/independent/isa_simd_i32x4.trunc_sat_f32x4_s | 1,156 | 1,168 | 1,165.5 | +0.82% | 0.010 |
| signals | ExecParallel/independent/isa_simd_i32x4.extadd_pairwise_u | 707 | 725.4 | 719.15 | +1.72% | <0.001 |
| signals | Validate/tiny | 682.3 | 692.55 | 693.8 | +1.69% | 0.018 |

The final Blake SIMD and f64.min diagnostics change direction and have no clear
timing difference. SHA-256 instantiation remains +0.95%, p=0.238; it is not
proven equivalent, but its earlier statistical warning does not persist.

The bounds label describes the build and runtime default. Direct backend and
standalone JSON helpers retain their explicit-bounds configuration in both
builds. Parallel execution reports throughput time, not call latency. Only rows
with calls/batch receive derived per-call allocation counters. PluginExec stops
the allocation timer, so its printed zero counters are not measurements; each
reported time covers one fixed workload, not b.N repetitions.

The host rebooted before this run. Incomplete pre-reboot data in volatile /tmp
storage was lost and is excluded. This comparison uses new post-reboot samples
for both main and the fixed revision. Raw archives, scripts, identities, and
checksums are retained with this report. Later commits need their own qualification;
these numbers must not be relabeled as measurements of a later main or PR head.

## Remaining memory costs

The twelve-pair warm checks confirm JSON parallel compile allocation increases:
explicit p8 is 628.5 -> 692 allocs/op (+10.10%); guard p8 is 629 -> 691
(+9.86%). Warm allocated bytes increase about 1.1–1.6% in the selected JSON
and many-function compiler cases. These costs are already present in the prior
candidate; the instantiation fix does not remove them.

Six-pair, single-operation memory checks also show guard JSON p8 at
559.5 -> 700.5 allocs/op (+25.20%), guard Lua p8 at
5,188,508 -> 5,416,028 B/op (+4.39%), and guard esbuild p2 at
111,797,372 -> 113,502,204 B/op (+1.52%). These are separate fixed-work
measurements, not warm timing claims. Worker scheduling affects scratch growth;
the causes and differences between warm and fixed-work checks need diagnosis.

The full time-limited explicit run has maximum process RSS of
1,738,476 -> 2,069,600 KiB (+19.04%). Do not discard that observation.
Its calibration can perform different amounts of work. In six equal-work
whole-corpus compact-compilation checks (one operation per module), median
peak RSS is 160,478 -> 156,938 KiB (-2.21%) for explicit and
161,544 -> 159,024 KiB (-1.56%) for guard builds. This does not establish
a repeatable 19% equal-work peak-memory regression, nor prove every workload
uses less memory. Per-operation Go allocation is not process RSS.

## Release checks

Release-size builds and smoke results are retained in [size.json](size.json)
and the `size/` directory. A pinned TinyGo minimal-runtime Fibonacci smoke
crashes on the measured main baseline and the fixed checkpoint; the prior
candidate passes this run. All twelve builds fit their size budgets. Build success and budget compliance
must not be described as runtime smoke success. This issue, the earlier Wine
limits, and the remaining allocation increases prevent a clean release claim.
