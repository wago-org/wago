# Wago shared-function experiment: regression mitigation

Additional native target evidence: [macOS ARM64 qualification](shared-function-macos-qualification.md). The Linux measurements below are preserved; the Mac report adds measured results and identifies its remaining compile/code-size regressions.

**Recommendation: keep the revised bounded pilot for review.** The former join and two-argument leaf execution spikes are reduced to roughly 1–1.5% relative median differences from main, with uncertainty spanning zero. The 256-local compile probe is faster and retains its allocation improvement. Default-policy benchmark-process RSS remains higher: do not claim a uniform RSS improvement. A separately measured per-process huge-page policy removes the large memory spikes, but its execution tradeoff is inconclusive.

This supersedes the performance verdict in [the previous qualification](shared-function-final-qualification.md), while preserving those measurements. PR #802 remains ready for review; no merge is part of this experiment.

## Frozen revisions and changes

| Point | Exact source commit | Role |
|---|---|---|
| A / M | `0ef007c70581bf56155a4daf6fce8bda3f2c5ff1` | Frozen origin/main |
| B | `e026bfc92918b9576007e2ca1e659e62f42e4a13` | Shared helper extraction |
| P | `95732e7454186fccd788b9c4e883354e6772a31a` | Previously measured PR executable; report-only head was c6d0372 |
| Q | `318170b6159a23cb23aaba03037720d430291d79` | Join agreement and direct local-run admission |
| C / F | `1e96da7c6b2042d89b2cde78185a83e771136c20` | Final pilot with simple multi-argument leaf fallback |

Origin/main was fetched and remained A. The prior rebase, baseline correctness, overlapping-PR review, helper/code-generation comparison and reference-commit reconciliation are preserved in the previous qualification. No rebase occurred during this follow-up. Unrelated changes in the original checkout remain outside the isolated experiment branch.

Three changes address concrete costs:

1. Both if edges can prove that the single result and a particular local have the same value identity. The join preserves one such relation, avoiding the later local reload. This uses existing control-frame padding; no new state map or allocation is added. Canonicalization still captures old values before writing homes. Both edges must prove the relation independently; numerical or register equality is insufficient.
2. Admission consumes validated run-length local declarations directly. It no longer expands a 256-element ValType array in either target. AMD64 admission's Go stack frame shrinks from 4,216 to 64 bytes. Valid zero-count noninteger runs remain accepted when they introduce no values. This reduces repeated work without expanding the feature subset.
3. Functions with multiple parameters, no declared locals or control, and at most one new summarized value prefer the established register-homed compiler. The shared pilot currently homes incoming parameters in memory; its overhead is not repaid by these leaves. The rule uses semantic summary counts, so nop padding does not bypass it. `tiny.add` now has exactly main's native bytes and zero frame bytes on both targets.

The common streaming driver continues to own operand stack, stable identities, deferred relationships, authoritative locations, owned/borrowed GP registers, local versions/home validity, spill accounting, control agreements and return preparation. Targets supply physical banks/reservations/clobbers, operand forms, instruction selection, ABI details, loads/stores/moves/branches/returns and encoding/relocations. No second logical target state, whole-function instruction array or mandatory SSA graph is introduced.

The migrated subset remains bounded i32/i64 constants, local.get/set/tee, supported nontrapping integer arithmetic/bit operations/shifts/comparisons, drop/nop, plain blocks, structured if/else joins and single-result/tail returns. Loops, arbitrary branches, calls, div/rem, memory/global effects, FP/vector/ref operations, indexed block types, custom instructions, GC/EH conditions and source/unwind recording use explicit admission-before-emission function fallback. The new leaf policy is an additional performance boundary, not a mid-function compiler switch.

## Correctness and coverage

F passes `go test -p 1 ./...`, the complete `wago_regalloccheck` root suite, and the checked bench module suite. Focused public/shared/ARM64-backend tests execute under QEMU in ordinary and checked builds. Darwin/Windows AMD64/ARM64 suite and backend cross-builds pass in both modes; those are build checks, not execution testing.

New tests cover both-edge identity, mismatched local indices, overwrites before/after a join, independently exercised nested edges, both integer widths and ABI modes, run/expanded declaration parity, zero-count runs and leaf admission exemptions/padding. Existing pressure, old-live-local, spill/join, fixed-register, move-cycle, trap/effect and mixed-bank fallback coverage remains. Independent source review found a zero-count-run admission mismatch and a nested-test gap; both were fixed before final measurements. No unresolved correctness finding remains in these changes. Review notes are in the data directory.

Fresh diagnostic builds on both targets prove **1,332/1,380 functions and 23,445/42,481 body bytes** migrate. Synthetic coverage remains 1,031/1,032 functions and 21,406/21,412 bytes; existing corpus is 301/348 functions and 2,039/21,069 bytes. Only one measured function leaves the shared path. Pressure and join execution benchmarks still directly exercise it. Pressure has 27 scalar spills and one explicit reload on AMD64; memory operands add traffic beyond explicit reload counters.

Q changes only join native bytes relative to P in the fixed 14-module corpus; F changes only tiny relative to Q, restoring main's tiny hash. Helper-only B retains A's native hashes on both targets. Hashes, frame/spill data, all scratch/hint counters and function-attempt accounting are in [diagnostic-summary.csv](shared-function-regression-data/diagnostic-summary.csv). Many-function frames still sum 12,288 bytes versus zero on main; this is per-function storage summed across the module, not simultaneous call-stack use.

## Timing results

Primary configuration: Linux AMD64, Ryzen 7 8845HS, Go 1.27.1, GOAMD64=v1, default optimization/explicit bounds, no production tags, GOMAXPROCS=1, one compiler worker, CPU 4, GOGC=100 and GOMEMLIMIT=off. Normal worker policy uses GOMAXPROCS=4 and CPUs 2–5. Performance governor/energy preference and CPU features are recorded. Host THP policy stayed `always`; it was not changed globally. Builds, fixture preparation, other agents' builds/tests/profiles and this experiment's profiling were excluded from timing.

Each primary cohort has three balanced M/P/F/F/P/M blocks, six fresh processes per revision. Primary rows run 200 ms; long execution rows run 1 s. The focused join/tiny repeat adds six processes per revision at the same 1 s boundary, giving n=12 for those two rows. No unfavorable sample was discarded, including a 21.57 ns F tiny sample. Helper-only comparison has separate B/F/F/B blocks, n=6. Benchstat results and raw samples are preserved; iterations within a process are not independent samples.

Native timing excludes decode/validation/setup and includes analysis, lowering, encoding and supported Close. Full timing includes decode, validation, analysis, native compilation and Close. Every iteration compiles; direct backend and PreparedCompile.Compile do not return cached compiled outputs. Immutable decoded inputs are reused. Execution compiles/instantiates outside timing, verifies fixed outputs and uses the identical existing harness. Synthetic Invoke/verification and amortized deferred cleanup are included; existing execution excludes cleanup and batches calls. JSON-AS retains the existing guest-allocator-state caveat, so its execution is not a strict fresh-guest-state result.

The migrated geomeans are the eight fixed synthetic shared-path workload groups; existing corpus aggregates are reported separately. Values below are medians; ± is benchstat's reported uncertainty. Percentages compare medians, including inconclusive rows.

| Measurement | A / main | P / prior PR | C / F | A→F | Samples / inference |
|---|---:|---:|---:|---:|---|
| All native geomean | 66.78 µs ±13% | 59.78 µs ±8% | 57.43 µs ±6% | −14.00% | 6; p=.002 |
| Migrated native geomean | 60.14 µs ±5% | 47.64 µs ±9% | 47.92 µs ±4% | −20.31% | 6; p=.002 |
| All full geomean | 86.23 µs ±18% | 77.00 µs ±22% | 76.15 µs ±11% | −11.69% | 6; p=.041 |
| All execution geomean | 286.3 ns ±10% | 274.0 ns ±14% | 275.5 ns ±3% | −3.78% | 6; p=.015 |
| Join execution, combined repeats | 16.35 ns ±4% | 17.89 ns ±3% | 16.59 ns ±2% | +1.44% | 12; p=.629, inconclusive |
| tiny.add execution, combined repeats | 15.54 ns ±5% | 16.75 ns ±2% | 15.72 ns ±3% | +1.13% | 12; p=.843, inconclusive |
| 256-local native compile | 17.25 µs ±27% | 16.43 µs | 14.51 µs ±7% | −15.86% | 6; p=.002 |
| 256-local B/op | 14,569 | 11,976 | 11,976 | −2,593 (−17.80%) | 6; allocs/op 16→17 |
| many_funcs native | 416.1 µs ±19% | 413.7 µs | 381.4 µs ±6% | −8.34% | 6; p=.009 |
| Fallback full, focused 1 s repeat | 10.52 µs ±6% | 10.73 µs ±5% | 10.71 µs ±8% | +1.83% | 6; p=.485, inconclusive |

P→F join is −7.29% and tiny is −6.15%, both p<.001, n=12. A seeded 50,000-resample independent-process bootstrap gives relative median 95% percentile intervals of **−2.33% to +3.50%** for join and **−3.68% to +3.88%** for tiny. These bound the two investigated spikes below the initial 5% important-workload threshold in this host/corpus experiment; they are not universal noninferiority guarantees.

The helper-only B→F aggregate comparison is 70.17→58.74 µs native (−16.30%, p=.002) and 88.34→76.08 µs full (−13.88%, p=.002), n=6. P→F primary aggregates are inconclusive. Existing-corpus aggregates and most automatic-worker rows are inconclusive; one-worker many_funcs full improves 7.60%. JSON-AS full's prior repeatable increase does not reproduce significantly here. The primary fallback median was +8.61% with broad intervals; its mandated focused repeat is +1.83% and inconclusive. Both cohorts remain available.

## Memory and remaining costs

Identical fixed-work runners perform 270 compile/releases, three retain/release cycles of 36 modules/4,128 functions, GC observations with Runtime alive, and final Runtime close. Standalone runtime additionally keeps 36 verified instances alive. Inputs stay equivalent, outputs explicitly close through supported APIs, references are removed, and GC is outside timing. No forced page release or finalizer-timeliness assumption is used.

One-worker standalone allocated bytes fall 37,785,888→23,674,664 (−14,111,224, −37.35%); allocation counts 55,060→54,629 (−431, −0.78%). Backend allocated bytes fall 19,246,160→5,173,496 (−14,072,664, −73.12%). Retained native code falls 167,268→134,972 bytes (−32,296, −19.31%), while owned backend mapping capacity remains 606,208 bytes. After release it is zero. Public native byte capacity is not exposed; its runner reports actual registration counts rather than inventing mapped bytes.

| Standalone lifecycle metric, bytes unless stated | A median | F median | Change |
|---|---:|---:|---:|
| Retained HeapAlloc | 1,170,736 | 1,170,496 | −240 (−0.02%) |
| Retained HeapInuse | 1,798,144 | 1,839,104 | +40,960 (+2.28%) |
| After outputs close HeapAlloc | 1,079,816 | 1,079,704 | −112 (−0.01%) |
| After outputs close HeapInuse | 1,785,856 | 1,859,584 | +73,728 (+4.13%) |
| After Runtime close HeapAlloc | 150,488 | 150,504 | +16 (+0.01%) |
| After Runtime close current RSS | 13,363,200 | 13,590,528 | +227,328 (+1.70%) |

F released-cycle HeapAlloc medians plateau near 1,079,704 bytes; no continuing growth is observed over three cycles. HeapInuse includes span slack and is not allocated-object bytes. Profiles attribute sampled retained space to compiled-metadata clones and export/JSON caches, not the scalar backing. Additional GC observations are separately recorded; they do not replace primary lifecycle phases. All HeapObjects/HeapReleased/allocation deltas/current RSS/spreads are in the raw memory tables.

Fresh-process high-water RSS below is KiB, median [min,max], n=6. Test-runner peak includes both public compile-only and backend mapping scenarios in one process. Standalone peak includes actual retained instances. RSS is not scratch payload or code mapping capacity.

| Default process policy | A | P | F | A→F |
|---|---:|---:|---:|---:|
| Test runner, one worker | 21,026 [20,728, 22,712] | 22,834 [22,312, 23,020] | 23,010 [22,992, 24,788] | +1,984 (+9.44%) |
| Test runner, auto | 22,298 [20,432, 22,624] | 24,280 [24,152, 24,792] | 24,382 [23,960, 25,312] | +2,084 (+9.35%) |
| Standalone, one worker | 89,106 [88,844, 91,232] | 88,786 [88,508, 90,392] | 88,974 [88,148, 90,984] | -132 (-0.15%) |
| Standalone, auto | 89,742 [88,428, 90,740] | 90,714 [88,276, 90,852] | 90,692 [88,716, 91,520] | +950 (+1.06%) |

Default test-runner RSS is still about 2 MiB higher than main and slightly higher than P. This cost is not erased by scratch/latency improvements. Disabling the shared compiler in the same P/F binaries gives similar or higher peaks; the difference also appears substantially before compilation. That weakens attribution to shared value storage alone, without proving a unique cause for every page.

A separate Linux diagnostic sets inherited per-process `PR_SET_THP_DISABLE` before exec, equally for A and F. It does not change host policy, source binaries, guest inputs, GC settings or lifecycle work, and it does not force-release memory. Its results are not substituted for primary production results.

| Per-process huge pages disabled, diagnostic | A | F | A→F |
|---|---:|---:|---:|
| Test runner, one worker | 19,560 [19,492, 20,348] | 19,220 [18,992, 19,308] | -340 (-1.74%) |
| Test runner, auto | 19,378 [19,080, 20,084] | 19,146 [18,864, 19,592] | -232 (-1.20%) |
| Standalone, one worker | 14,168 [14,024, 14,444] | 13,776 [13,704, 13,932] | -392 (-2.77%) |
| Standalone, auto | 14,560 [14,472, 14,808] | 14,606 [14,200, 14,796] | +46 (+0.32%) |

This demonstrates a practical memory-policy tradeoff and strong sensitivity of these spikes to huge-page allocation. It does not establish a compiler-only RSS fix. A separate balanced F-normal/no-THP execution comparison on the same preselected corpus is inconclusive, with very broad intervals; dense-guest performance and strict reset state remain unqualified. Treat the launcher as an opt-in experiment for deployments with RSS spikes, not a new default or a general zero-cost recommendation.

Ranked remaining concerns:

1. **Default-policy test-runner peak RSS:** +1,984 KiB (+9.44%) with one worker and +2,084 KiB (+9.35%) with auto. Standalone medians are near main in this final cohort, but prior cohorts had much wider spread. Physical RSS causality/default mitigation remains open; the per-process policy diagnostic is a measured alternative.
2. **Guest frames:** many/large-then-small retain conservative 24-byte frames for each shared scalar function versus zero for corresponding main leaves. These sums are native function storage, not runtime heap or simultaneous stack use. Tiny's frame regression is removed.
3. **Bounded allocation overhead:** fallback native still allocates +256 B/op (+3.10%) relative to main, with the same 21 allocations/op. The 256-local case has one additional allocation despite 2,593 fewer bytes. No new per-operation execution allocations appear.
4. **Retained allocator slack:** +40,960 bytes retained HeapInuse and +73,728 after output release, while HeapAlloc is essentially equal and does not grow across cycles. Current RSS after Runtime close is +227,328 bytes. These are actual costs to retain in qualification, not evidence of live scalar-scratch growth.
5. **Small residual execution differences:** join/tiny medians +1.44%/+1.13%, now inconclusive with the above bootstrap bounds. Existing aggregate execution and worker-policy effects remain noisy.

Scalar large/large-then-small scratch remains 2,072 bytes versus the earlier 64,120-byte representation, a 96.77% payload reduction. This follow-up does not claim another scratch reduction. Reported scratch peak sums worker high-water values: an accounting envelope, not a simultaneous process peak. NodeScratch*, ControlScratch*, scalar peak/retained/discarded, hint storage and function attempts are exported separately from production timing. A lower source-line count is not physical-memory evidence.

## Reproduction and preserved evidence

[Raw timings](shared-function-regression-data/raw-timing.csv), [raw memory phases](shared-function-regression-data/raw-memory.csv), [process resources](shared-function-regression-data/process-resources.csv), [corpus hashes](shared-function-regression-data/corpus-hashes.csv), [source/binary hashes](shared-function-regression-data/source-binary-manifest.json), build/check/run manifests, benchstat tables, bootstrap samples/method and independent reviews are in [the data directory](shared-function-regression-data/sha256.json). Measurement harness hashes match across revisions. The original fixed corpus was not reselected after results; the extra repeats investigate already measured regressions.

Large original logs, binaries, heaps, disassembly, build caches and earlier Q measurements remain outside tracked production source at:

`/home/jtenner/.codex/worktrees/1791067335-346105/wago/.worktrees/pr802-final-evidence/spike-final`

and its sibling `spike-mitigation`. Older complete baseline/helper/pilot evidence remains in the parent evidence directory. The report-only commit changes no measured compiler source.

Reproduce using the recorded sibling layout: `pr802-main` at A, `pr802-helpers` at B, `pr802-final` at F, and `pr802-final-evidence/spike-final` for output. Apply the same tracked `bench/suite/sharing_*_test.go` harness to A/B; its hashes are recorded. Build the prior P binaries from their recorded source if repeating P comparisons. Prepare corpus/tools before timing; pinned WABT/spec-interpreter paths and environment are in the check script/manifests.

```bash
# In each frozen source tree, before timing:
export GOCACHE=/path/to/pr802-final-evidence/go-cache GOFLAGS=-buildvcs=false
go test -c -o /path/to/evidence/revision-suite.test ./bench/suite
go test -c -tags=wago_codegenstats -o /path/to/evidence/revision-diagnostic.test ./bench/suite
# Copy runtime_memory.go.txt to an external .go path, then:
go build -trimpath -ldflags='-s -w' -o /path/to/evidence/revision-runtime-memory /path/to/evidence/runtime_memory.go

go test -p 1 ./...
go test -p 1 -tags=wago_regalloccheck ./...
(cd bench && go test -p 1 -tags=wago_regalloccheck ./...)
```

Exact balanced timing/worker/memory/admission commands are in `measurement-runs.json`, helper and repeat manifests. With the retained evidence layout:

```bash
python3 /path/to/pr802-final-evidence/spike-final/build_and_check.py
python3 /path/to/pr802-final-evidence/spike-final/measure.py
python3 /path/to/pr802-final-evidence/spike-final/measure_helpers.py
python3 /path/to/pr802-final-evidence/spike-final/repeat_spikes.py
python3 /path/to/pr802-final-evidence/spike-final/measure_thp_diagnostic.py
python3 /path/to/pr802-final-evidence/spike-final/measure_thp_execution.py
python3 /path/to/pr802-final-evidence/spike-final/profile_memory.py
python3 /path/to/pr802-final-evidence/spike-final/final_cross.py
```

The optional no-THP launcher is Linux-specific and applies only to its process descendants. It is kept out of production compiler/runtime code.

Incomplete: native ARM64 correctness/performance/RSS (not measured), broad ARM64 execution beyond focused emulation, Darwin/Windows execution, alternate toolchains, strict host mapping identity (the existing host naming facility rejects it), complete common transfer-transition checker coverage, uniform default-policy RSS improvement, definitive per-page causality, strict reset guest state for JSON-AS and dense-memory execution cost of the huge-page tradeoff. Exact cross/QEMU commands are recorded; native ARM64 measurements require building/running the same commands on an ARM64 host rather than substituting emulation timing.
