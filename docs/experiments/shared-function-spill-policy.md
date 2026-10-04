# Shared function compilation: spill-policy experiment

**Keep this mitigation on the isolated branch; continue revising the overall pilot.** Changing spill-victim selection improves the pressure workload by about 4% relative to the previous pilot in two separate timing cohorts. Its execution median is now within 0.5% of the frozen baseline. A longer run supports a pointwise upper bound of +1.53% for this workload's median regression under the measured conditions. This is stronger evidence than a nonsignificant comparison, but it is not a claim of equivalence across architectures or workloads.

The tradeoff is more compilation work when registers are full and 59 additional pressure-function code bytes. No measured allocation, scratch, frame, spill-slot, or native-mapping increase accompanies the new policy. Existing leaf frames, allocation-event regressions, and normal-process RSS differences remain. This report supplements [the initial experiment](shared-function-compilation.md), [the first mitigation](shared-function-execution-mitigation.md), and [the preceding investigation](shared-function-regression-investigation.md); it does not replace their evidence or limitations.

## Frozen sources and boundary

| Point | Commit | Role |
|---|---|---|
| A | `31547a885b6770cc0f1718f69bce495b5f3e38ce` | Original baseline |
| B | `9ff39cf3a2aabb9fb2204cdb5d426a6bb59f207a` | Original helper extraction; not retimed here |
| C | `c33e728ce92f8317469226d3ece0e603dac7ba09` | Original shared pilot; not retimed here |
| F | `9ce1333cb013c94102dacdf14465cecdbb208e44` | Previous production candidate |
| G | `3e595c0c3dcee066dd27d85be9a471f4d22eeb5c` | Spill policy and focused correctness test |

The branch is `experiment/shared-function-compilation`, at `/home/jtenner/.codex/worktrees/shared-function-pilot/wago`. No rebase, merge, push, admission expansion, or benchmark-fixture change occurred. A and F production/diagnostic binaries were copied from the preceding evidence and their hashes verified. The five shared measurement harness files match all three measured revisions exactly; the manifest preserves those hashes and each binary hash.

Only `ScalarState.alloc` changes production behavior. Free-register selection keeps the target's allocation order. When no register is free, one scan of the target register bank chooses the oldest eligible stable value ID, preferring single-reference values without a clean local home. Multiple references and clean homes are reuse heuristics, not future-use knowledge. IDs reflect record creation order; reconstructed control/local records are recent IDs even if their values originated earlier. Active operands, reserved registers and instruction clobber exclusions retain their existing contracts. Eviction still uses the authoritative common spill transition.

The shared layer remains the sole owner of the logical stack, stable IDs, deferred relationships, locations, register owners, locals, spill accounting, control agreements and return preparation. No value fields, auxiliary age array, local scan, next-use pass, IR, second target state, or per-operation allocation was introduced. Both target lowerers use the policy through the existing common services.

Admission remains the same bounded integer subset: i32/i64 constants, local.get/set/tee, basic nontrapping integer arithmetic/bitwise/shifts/comparisons, drop/nop, plain blocks, if/else with agreement, and one-result implicit or tail returns. Calls, memory/global effects, traps such as div/rem, arbitrary branches/loops, FP/vector/reference and metadata-sensitive features continue through explicit function-level fallback before emission.

Diagnostics prove **1,333/1,380 functions and 23,451/42,481 body bytes** use the shared path in F and G. Synthetic coverage is 1,031/1,032 functions and 21,406/21,412 bytes; existing-module coverage is 302/348 and 2,045/21,069 bytes. Only pressure has a different generated-code hash between F and G across the fixed 14 modules. Per-function admission counters remain in the diagnostic logs, and per-module admission nanoseconds are in `corpus-code-storage.tsv`. Admission logic is unchanged; these single instrumented observations are not production latency estimates. The outdated standalone admission microbenchmark remains excluded.

## Correctness and review

- Native AMD64 checked backend/public tests pass, including golden, execution and control tests. The broader `src/core`, `src/wago`, and `tests` checks pass with pinned WABT 1.0.41 and spec interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`.
- The final shared-scalar cases pass in an ordinary production build and in `wago_regalloccheck` builds. Fixed fixture and selected semantic-corpus checks pass.
- ARM64 checked shared-scalar tests were cross-built and **executed under QEMU successfully**. Native ARM64 was not available and is **not measured**.
- The new test combines more live values than either register bank, nested variable shifts, an older local read surviving overwrite, multiply referenced values, and a result join, for both widths and register/memory result ABIs.
- Independent `contract_review` found no production defect. It identified that the first test version did not actually retain the original local ID across overwrite; the test now pushes that value before creating pressure, performs one additional reduction, and accounts for it in the oracle. The reviewer confirmed the fix. No unresolved concrete finding remains. Review was read-only and ran no builds or profiling.

Earlier limitations remain: the existing transfer checker does not instrument every common allocator transition; full ARM64 emulation had a baseline-reproducing crash; unrelated root CLI/installer checks were not repaired by this experiment. This pass does not claim those are resolved.

## Measurement protocol

Host: AMD Ryzen 7 8845HS, Debian 13, Linux 6.12.111+, Go 1.27.1, AMD64 with GOAMD64=v1. Default optimizer, explicit bounds checks, no production build tags; `wago_codegenstats` only for separate diagnostics. `GOMAXPROCS=1`, one compiler worker, `GOGC=100`, `GOMEMLIMIT=off`. Governor is performance, boost enabled; frequency was not locked. Full CPU features/toolchain information is in `environment.txt`.

All primary and follow-up processes were pinned to CPU4. CPU4/5 had 2.24%/0.40% utilization in the five-second pre-run snapshot. No agent builds, tests or profiling overlapped timing. Desktop processes were not stopped. Host activity and frequency variability remain possible confounds; no sample was discarded, including the longer baseline pressure outlier of 53.08 ns.

| Cohort | Balanced order, three complete blocks | Duration per row | Fresh processes per revision |
|---|---|---:|---:|
| Primary, all 43 rows | A/F/G/G/F/A | 200 ms | 6 |
| Longer execution, nine fixed synthetic rows | A/F/G/G/F/A | 1 s | 6 |
| Normal fixed-work memory | A/F/G/G/F/A | Fixed work | 6 |
| Compilation follow-up, nine selected rows | F/G/G/F | 1 s | 6 |
| Separate whole-process THP-disabled memory | A/F/G/G/F/A | Fixed work | 6 |

The follow-up was selected after primary compilation regressions appeared: native/full pressure, locals, small and fallback, plus full fib_rec. It is an investigation cohort, not a replacement for primary samples. Cohorts are analyzed separately with benchstat; iterations within a process are not independent samples. Aggregate timings are equal-weight per-process geometric means, not a production traffic mix.

Native timing includes analysis/codegen and Close; decode/validation and reusable input preparation are outside timing. Full timing includes decode, validation, analysis, native compilation and Close. These call the compiler each time without a compiled-code cache; outputs do not accumulate. Execution compiles/instantiates outside timing and checks fixed outputs. Synthetic execution includes invocation and verification each iteration, plus once-per-benchmark deferred Close after the loop (amortized in that timer, because this unchanged harness does not call StopTimer before deferred cleanup). Existing corpus execution stops timing before cleanup. Synthetic guests have equivalent state across iterations; the earlier JSON-AS same-instance GC-state caveat remains.

## Timing results

Each cell is benchstat's median and 95% envelope; n=6 independent processes per revision. A/G is primary. F/G isolates this mitigation from the preceding candidate.

| Primary boundary | A | F | G | A→G |
|---|---:|---:|---:|---|
| Migrated native compile aggregate | 57.49 µs ±5% | 44.64 µs ±5% | 44.89 µs ±4% | −21.91%, p=.002 |
| Migrated full compile aggregate | 71.95 µs ±4% | 60.21 µs ±3% | 61.65 µs ±6% | −14.31%, p=.002 |
| Migrated execution aggregate | 42.47 ns ±2% | 41.23 ns ±3% | 40.61 ns ±2% | −4.37%, p=.002 |
| Full-sample native aggregate | 62.81 µs ±4% | 53.30 µs ±2% | 54.66 µs ±3% | −12.97%, p=.002 |
| Full-sample full aggregate | 77.24 µs ±3% | 69.86 µs ±4% | 70.53 µs ±5% | −8.68%, p=.002 |
| Pressure execution | 46.19 ns ±2% | 48.26 ns ±3% | 46.38 ns ±3% | Median +0.42%, p=.485 |
| Join execution | 14.97 ns ±2% | 15.22 ns ±2% | 15.29 ns ±9% | +2.14%, p=.017 |

Primary pressure F→G is **−3.88%, p=.009**. F→G aggregate comparisons are inconclusive (p=.240–.394); migrated native/full median changes are +0.56%/+2.39%. Primary pressure native compilation is 26.62/17.75/18.14 µs for A/F/G: G is 31.87% faster than A, but its median is 2.18% slower than F. Full pressure is 32.76/24.03/25.41 µs: −22.45% versus A, median +5.71% versus F. The F/G differences are inconclusive in this cohort (p=.180/.240).

The longer execution cohort confirms the targeted benefit:

| Boundary | A | F | G | Evidence |
|---|---:|---:|---:|---|
| Pressure | 46.02 ns ±15% | 48.00 ns ±0% | 46.09 ns ±1% | F→G −3.97%, p=.002; A→G median +0.17%, p=.818 |
| Join | 15.01 ns ±2% | 15.03 ns ±1% | 15.11 ns ±2% | A/G inconclusive, p=.219; F→G +0.53%, p=.019 |
| Small | 14.62 ns ±0% | 14.78 ns ±1% | 14.80 ns ±1% | A→G +1.20%, p=.002 |
| Migrated execution aggregate | 42.32 ns ±4% | — | 40.42 ns ±1% | A→G −4.50%, p=.002 |

A post hoc, pointwise nonparametric upper bound can be more useful here than interpreting p>.05 as equivalence. With six independent identically distributed process samples per revision, each sample maximum is a 98.4375% one-sided upper bound for its population median, and each minimum is a corresponding lower bound. By the union bound, `max(G)/min(A)` is at least a 96.875% one-sided upper bound on the median ratio. Longer pressure samples give `46.47/45.77 − 1 = +1.53%`; the primary cohort gives +4.04%. Longer F/G gives an upper bound of −2.92%. These are pointwise, assumption-dependent bounds on process medians, not simultaneous corpus bounds, tails, all CPUs, or a post hoc universal acceptance margin. All raw values are preserved in `summary.json` and the cohort JSON files.

The targeted compilation follow-up finds pressure native **17.26→17.98 µs, +4.15%, p=.026**. Pressure full is 23.88→24.55 µs, inconclusive p=.240. Native small is +3.66%, p=.039. The primary full fib_rec +7.41% becomes +2.08%, p=.041; native locals +3.79% and full small +4.59% do not repeat significantly. Fallback and non-pressure generated bytes are identical. The changed scan is never reached by functions entirely on fallback, so it cannot directly explain their timing differences; host variation and Go executable layout remain possible causes, not established attributions. Multiple comparisons also weaken isolated marginal p values. Do not silently drop these unfavorable samples.

A separate fixed-work CPU profile uses 300,000 pressure compilations per binary, after all latency measurements. Approximate flat samples in `ScalarState.alloc` rise from 60 ms of 5.30 s in F to 250 ms of 5.45 s in G, consistent with added victim-scan work. Both versions are already non-inlineable (compiler costs 118/188 against budget 80), so losing inlining is not the explanation. Profiles are diagnostic samples, not replacement timing estimates.

## Allocation, code and scratch

Every primary B/op and allocs/op median is identical between F and G. Examples for native compilation:

| Workload | A B/op | G B/op | Absolute / percentage change | A/G allocs/op |
|---|---:|---:|---:|---:|
| Small | 8,248 | 7,536 | −712 / −8.63% | 21/20 |
| Pressure | 19,488 | 12,120 | −7,368 / −37.81% | 18/23 |
| Join | 10,152 | 7,936 | −2,216 / −21.83% | 26/21 |
| Deep | 11,504 | 10,000 | −1,504 / −13.07% | 22/26 |
| Many locals | 29,192 | 20,592 | −8,600 / −29.46% | 21/22 |
| Large then small | 275,320 | 119,680 | −155,640 / −56.53% | 30/24 |
| Fallback | 8,248 | 8,584 | +336 / +4.07% | 21/22 |

Pressure native code is A/F/G **825/510/569 bytes**. G adds 59 bytes (+11.57%) versus F and remains 256 bytes smaller (−31.03%) than A. REX-prefix counts grow from 24 to 85 as more extended registers are used. The reduction begins with register operands, delaying reads of recently stored values; a store-forwarding/dependency benefit is plausible, but no hardware-counter attribution is claimed. Frames stay 232 bytes, spill-slot high water 27; common spill and explicit reload counters stay 27 and 1. Memory operands are not counted as explicit reload instructions.

All other measured F/G code hashes, function frames, spill-slot counts and scratch accounting are identical. Shared-only synthetic modules still reserve no target Node/Control backing. Common scratch for pressure/locals/deep remains 4,340/7,192/1,396 bytes. `scratch-accounting.json` preserves every module's NodeScratch*, ScalarScratch*, ControlScratch*, hints, discarded/retained accounting and attempts. Worker high-water sums are accounting envelopes, not simultaneous process peaks. No new backing or per-value field was added. Large-then-small still exercises reuse inside one module worker.

## Fixed-work memory

Each fresh process performs 270 compile/close operations, then three batches of 36 retained modules (4,128 functions), closing outputs, removing references and observing GC with Runtime alive; then it closes Runtime. A separate native-output scenario measures executable mappings. Inputs remain retained equally. No forced page release or finalizer assumption is used. Complete HeapAlloc/Inuse/Objects/Released, RSS, peaks, allocation deltas, code and mapped bytes at every phase, with min/max spread, are in `memory-tables.md` and `results/memory-summary.json`.

| Normal-process measurement, n=6 | A median | F median | G median | A→G |
|---|---:|---:|---:|---:|
| Public compile/release allocated bytes | 37,770,080 | 27,770,336 | 27,770,496 | −9,999,584 / −26.48% |
| Public allocation count | 55,063 | 54,815.5 | 54,817 | −246 / −0.45% |
| Native compile/release allocated bytes | 19,245,144 | 9,283,664 | 9,283,664 | −9,961,480 / −51.76% |
| Native allocation count | 6,295 | 6,055 | 6,055 | −240 / −3.81% |
| Retained native code, 36 modules | 167,268 | 163,060 | 163,296 | −3,972 / −2.37% |
| Retained mapped bytes | 278,528 | 278,528 | 278,528 | 0 |
| HeapAlloc after Runtime release | 211,248 | 205,848 | 206,248 | −5,000 / −2.37%; overlapping ranges |
| Native HeapAlloc after third release | 370,488 | 365,088 | 365,488 | −5,000 / −1.35%; overlapping ranges |
| Peak RSS, KiB | 21,978 | 22,942 | 23,234 | +1,256 / +5.72% |
| Peak RSS min–max, KiB | 20,620–23,140 | 22,652–25,188 | 22,460–24,900 | Overlapping ranges |

F→G retained code grows 236 bytes (+0.145%) and mappings do not grow. Public allocation delta grows 160 bytes (+0.00058%), within run spread; native allocation medians are identical. Final HeapAlloc differs by 400 bytes F→G (about +0.19% public, +0.11% native), with overlapping ranges of several KiB. G released-phase medians plateau at 1,547,232 bytes in all three public cycles and 365,488 bytes in all three native cycles. That establishes no continued growth over this fixed observation, not a general lifetime proof. The earlier extra-GC/reclamation caveat remains: the later public heap drop cannot be assigned solely to engine release. No new heap profile was needed to explain an additional retained-growth trend; prior retained-metadata profiles remain preserved.

Normal peak RSS grows 292 KiB (+1.27%) F→G with overlapping ranges. As in the preceding investigation, startup RSS differs before any compilation on this host with THP=`always`. A separate child-local `PR_SET_THP_DISABLE` run uses the same unmodified binaries and fixed work, without changing global OS policy or forcing release. Peak RSS is A **19,614 [19,272–20,488]**, F **19,514 [19,236–19,888]**, G **19,476 [19,316–19,656] KiB**, n=6. These ranges overlap. This is consistent with the previously observed BSS/huge-page layout contribution; it does not replace normal results, establish deployment-binary RSS parity, or recommend disabling THP in production. Go allocation totals omit native mappings, and RSS includes reusable resident pages.

## Ranked remaining costs and recommendation

1. **Compilation/execution tradeoff:** pressure execution improves about 4%, at +4.15% native compile time versus F in the longer targeted follow-up. The register-bank scan is the supported cause of extra allocator work. G still beats A on pressure native/full compilation by 31.87%/22.45% in the primary cohort. Consider simplifying victim ranking only if preserving this execution gain.
2. **Remaining execution qualification:** longer small execution is +1.20% versus A; primary join +2.14% does not repeat significantly in the longer A/G comparison. G/F join differs by +0.53% in that longer cohort despite identical guest bytes. The pressure bound is much tighter now, but it is not a whole-corpus or native ARM64 equivalence claim.
3. **Code and leaf storage:** pressure adds 59 bytes over F. Small/deep still have 24-byte frames versus A's zero, and many-functions still has 12,288 summed frame bytes versus zero. Many-functions code remains 16,401 versus 14,865 (+1,536 / +10.33%); join remains 90 versus 85 (+5 / +5.88%). Leaf frame/parameter ingress is a distinct next experiment, not included here.
4. **RSS and allocation-event costs:** normal G peak RSS remains +1,256 KiB versus A in this cohort. Test-binary layout/THP sensitivity does not qualify deployment binaries. Pressure still has five more allocation events, deep four, locals one, and fallback one plus 336 B/op, despite lower aggregate allocated bytes.

**Recommendation: keep G as a targeted mitigation; revise before accepting the complete shared compiler design.** This pass establishes a repeatable pressure-execution improvement and a materially smaller bounded regression for that workload on this native host. It does not erase the listed costs or declare the investigation thresholds to be acceptance criteria.

Incomplete: native ARM64 correctness/performance; additional worker-policy qualification; complete ARM64 emulation beyond focused tests; unrelated root CLI/installer failures; strict JSON-AS guest-state reset; complete common transfer instrumentation; deployed-runtime RSS qualification; tight simultaneous regression bounds across the corpus. No results are invented for missing targets.

## Reproduction and preserved evidence

All raw samples, binaries, corpus/native hashes, manifests, lifecycle tables, CPU profiles, disassembly and large logs are outside tracked production source:

`/home/jtenner/.codex/experiments/wago-sharing-spill-policy-20261003`

`manifest.json`, `SHA256SUMS`, `corpus-code-storage.tsv`, `diagnostics.json`, `independent-review.txt`, and the three measurement-run JSON manifests preserve provenance. `results/`, `long-results/`, `compile-followup-results/`, and `nothp-results/` are separate cohorts. All 84 scheduled primary/follow-up timing and memory processes exited successfully. Fixed-work CPU profiles are separate. The original A/B/C comparisons remain in the earlier reports and evidence directories.

Build/test from each frozen source, using the identical harness identified by `manifest.json`:

```bash
go test -c -o /tmp/G-suite.test ./bench/suite
go test -c -tags=wago_codegenstats -o /tmp/G-diagnostic.test ./bench/suite
go test -tags=wago_regalloccheck ./src/core/compiler/backend/railshot/shared ./src/core/compiler/backend/railshot/amd64 ./src/wago -run 'TestShared|TestGolden|TestExec|TestControl|TestModuleControl' -count=1
PATH=/home/jtenner/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:$PATH WAGO_SPEC_INTERPRETER=/home/jtenner/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa go test -p 1 ./src/core/... ./src/wago/... ./tests/...
GOOS=linux GOARCH=arm64 go test -c -tags=wago_regalloccheck -o /tmp/G-arm64-checked.test ./src/wago
/tmp/qemu-aarch64-static /tmp/G-arm64-checked.test -test.run '^TestSharedScalar' -test.v
```

From `bench/suite`, one primary timing process and one fixed-work memory process:

```bash
env -u WAGO_SHARED_SCALAR GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off taskset -c 4 /tmp/G-suite.test -test.run='^$' -test.bench='^(BenchmarkSharing(Native|Full|Exec)|BenchmarkCompile|BenchmarkCompileFull|BenchmarkExec)$' -test.benchtime=200ms -test.count=1 -wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as
env -u WAGO_SHARED_SCALAR GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off WAGO_SHARING_MEMORY=1 taskset -c 4 /tmp/G-suite.test -test.run='^TestSharing(Memory|MappedMemory)$' -test.v -test.count=1
GOMAXPROCS=1 /tmp/G-diagnostic.test -test.run='^TestSharingDiagnostic$' -test.v -wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as
```

Use `run_measurements.py`, `run_compile_followup.py` and `run_nothp.py` in the evidence directory to reproduce the exact balanced cohorts; they name binaries relative to themselves and record every child command/exit. Run `analyze.py`, `analyze_long.py`, `analyze_compile_followup.py` and `analyze_nothp.py` with benchstat installed for the separate analyses. `memory-tables.md` and `summary.json` retain spreads and absolute values.

For missing native ARM64 results, check out A/F/G with this identical harness on native ARM64, build each using `go test -c -o /tmp/REV-suite.test ./bench/suite`, run `go test -tags=wago_regalloccheck ./src/core/compiler/backend/railshot/shared ./src/core/compiler/backend/railshot/arm64 ./src/wago -run 'TestShared|TestGolden|TestExec|TestControl|TestModuleControl' -count=1`, and execute the same balanced commands above with one fixed available CPU substituted for CPU4. QEMU timings must not be substituted for that native comparison.
