# Shared function compilation: PR #802 review fixes

**Keep the PR in draft.** This follow-up implements the confirmed correctness and measurement fixes and the three bounded mitigations from the external review. It retains the streaming shared/target boundary and unchanged admission subset. Parameter ingress, leaf-frame removal, broader ARM64 instruction selection, and a transition-level common checker remain separate design work; admission is not expanded here.

## Sources and reproducibility

| Revision | Exact commit | Purpose |
|---|---|---|
| A | `31547a885b6770cc0f1718f69bce495b5f3e38ce` | Frozen experiment baseline |
| G | `4229269fd0e69c4c9d04d618641ff43aac0e3c64` | Reviewed PR head; production equals the preceding G report |
| H | `107c5fdbc4db6cbacf68967221c34c3bd80b39c4` | Combined review fixes and final measurement harness |

The original helper-only B and initial pilot C remain recorded in [the initial experiment](shared-function-compilation.md). A/H measures the complete experiment; G/H isolates this review pass. No rebase or merge changed the measured branch. The exact validation merge is `5333988fc1f2439558d277032f0a46c06788b801`, tree `937557597d0553c0907f84647d335cd939de69bf`, combining H with main `0ef007c70581bf56155a4daf6fce8bda3f2c5ff1`. Its performance is not inferred from H's timings. Later report-only commits do not change executable source.

The attached review text was available; its linked ZIP was not attached. Findings were implemented and reproduced directly. The work is isolated at `/home/jtenner/.codex/visualizations/2026/10/03/01a103ee-af5e-7f11-a74d-8c68fd8e53a4/wago-pr802-recovery/repo` on `experiment/shared-function-compilation`. Baseline and reviewed-head checkouts are `/home/jtenner/.codex/visualizations/2026/10/03/01a103ee-af5e-7f11-a74d-8c68fd8e53a4/wago-pr802-recovery/baseline` and `/home/jtenner/.codex/visualizations/2026/10/03/01a103ee-af5e-7f11-a74d-8c68fd8e53a4/wago-pr802-recovery/pr-head`. Existing worktrees and historical evidence were not modified.

Full logs, binaries, profiles and runner scripts are outside tracked source at `/home/jtenner/.codex/visualizations/2026/10/03/01a103ee-af5e-7f11-a74d-8c68fd8e53a4/wago-pr802-recovery/evidence`. This recovery directory persists outside `/tmp`. A host restart cleared the original unpushed checkout and raw measurements. The session history restored all nine commits through H with their original hashes. Measurements in this report are fresh post-restart runs; pre-restart timing summaries are not substituted for missing raw samples. Small raw measurement tables, hashes and commands are retained alongside this report so reviewers can audit the new samples without access to the host.

## Applied changes

1. `d13ae1fb76bb4ec0e2bee3fa53648951381747f3` isolates Linux memory and GC diagnostics, restoring portable benchmark builds.
2. `dcc8d9b0286037c20a393be879147eeecde90464` converts shared `wide=true` to ARM64 encoder `is32=false`. The instruction count is unchanged. Regression tests cover both widths, shifts 0–3, both ABIs, high bits and wraparound; a target test checks the encoded width independently.
3. `0fe9b3a4e3df96b3ef2a56ca515cdac9dce430fd` reports actual owned mapping capacity separately from page-rounded payload. A test uses a three-page owner with a 17-byte payload.
4. `f6495f9291c39df251de6a65c1fbad0fa3cd0200` replaces the ambiguous address-occupancy ownership predicate with unique anonymous VMA identity where supported, plus unconditional Close/view/Take checks. Strict qualification is described below.
5. `d232f4c43a632ef2f67af37dac4683d24df0fe03` records maximum admitted control depth in existing summary padding, reserving bounded control capacity with geometric reuse.
6. `5e0dfee85d7c7fadd76269ec150922ff38cf9cf1` initializes both fallback stacks in place, preserving the already-installed `fn.s` alias and removing one duplicate stack object.
7. `bd1a399ba6a2f273e460baa0c040f0b62b859dee` omits redundant AMD64 declared-local zero stores for admitted scalar functions. Parameters still have established homes; declared locals start as zero values in common state, and control agreements store them before borrowing their homes. GC-lane clearing remains before the guard. GC/EH/effects and unsupported controls remain excluded.
8. `98905bd695d3952388d952a3da3b54a546962955` adds fixed zero-local probes, and H adds the optional worker setting for identical fixed-work memory diagnostics.

No production `CodeBuffer` ownership behavior changed. The original iteration-116 failure remains unattributed. This host returns `EINVAL` for anonymous VMA naming. Owner-state and predicate tests pass, but actual OS identity/reuse assertions skip by default. `WAGO_REQUIRE_MAPPING_IDENTITY=1` deliberately makes that unavailable qualification fail. A green default suite must not be presented as a completed identity gate.

## Correctness and independent review

Before the width fix, the new ARM64 public test failed every i64 shift/ABI combination under QEMU: `1<<40` was lost. Corrected ordinary and checked public/backend tests pass under QEMU. These are execution tests under emulation, not native ARM64 qualification. Windows and Darwin benchmark/backend cross-builds pass for AMD64 and ARM64, ordinary and checked. Cross-builds are not execution tests.

Cold-control and fallback tests fail before their mitigations (8 versus a budget of 3 allocations, and 3 versus a budget of 2 respectively), and pass afterwards. The zero-local code-size test fails before the optimization and passes afterwards. Focused checked shared, AMD64 and public execution checks pass, including alternating fallback/shared function order and zero values across local overwrites and joins.

Independent read-only review found no concrete defect in the control reservation, fallback alias preservation, width adapter, or zero-local omission. It traced every admitted restore/borrow path to an established local home. Nonblocking test opportunities remain for the precise nested resultless-if/no-else and declared-zero tail-return combinations; the agreement fixtures also lack a diagnostic admission assertion, though the leaf fixtures and measured corpus have one. Review did not execute tests independently. Existing target transfer checks still do not observe every common-state transition. Add common transition instrumentation before expanding admission.

On the recorded validation merge, the full ordinary root suite, full checked root suite, and full checked benchmark-module suite pass. Enabled automatic-worker memory/GC diagnostics pass separately. Commands and statuses are in `final-gates.json` and logs are retained outside Git. The initial validation attempt hit TinyGo VCS stamping errors; disabling stamping exposed a read-only TinyGo cache. The targeted tests pass with `GOFLAGS=-buildvcs=false`, writable `GOCACHE` and `XDG_CACHE_HOME` directories. Those pre-restart failures remain recorded in session history; their full temporary logs were lost. Fresh full-gate logs use the writable-cache/stamping environment.

## Measurement protocol

The identical final harness is applied to A/G/H; its hashes and all binary hashes are in the manifest. Fixed fixtures were selected before the changes and retain their hashes. The new 1/8/64/256 zero-local probes were selected before that optimization and applied identically. No workload or unfavorable sample is discarded.

Native host: AMD Ryzen 7 8845HS, Debian 13, Linux 6.12.111+, Go 1.27.1, GOAMD64=v1. Production options use no instrumentation tags, default optimizer and explicit bounds checks. `WAGO_SHARED_SCALAR` is unset. Primary measurements use CPU4, GOMAXPROCS=1, one worker, GOGC=100, GOMEMLIMIT=off. Additional worker measurements use CPUs2–5 and GOMAXPROCS=4. Public default policy remains one worker; automatic workers are an additional scenario. Power governor is performance, boost enabled, frequency unlocked; ordinary desktop/system activity is not controlled. Agents run no builds, tests or profiles during measurement.

Each cohort uses three complete balanced A/G/H/H/G/A blocks: six independent fresh processes per revision. Primary timing uses 200 ms per row; the separate execution cohort uses one second. The worker cohort compares p1 and automatic compilation. Memory runs use fixed work, separately for p1 and automatic policies. Iterations inside one process are not independent samples. Benchstat reports process medians and uncertainty; aggregate rows are equal-weight geometric means, not a production traffic mix.

Native compilation includes analysis, native compilation and Close; reusable decode/validation/setup are outside timing. Full compilation includes decode, validation, analysis, native compilation and Close. Every iteration calls the compiler without a compiled-code cache and releases its result. Execution compiles/instantiates before timing, invokes fixed inputs and verifies outputs. Synthetic invocation/verification and amortized deferred cleanup remain included by the unchanged harness; existing corpus execution excludes cleanup. The previously documented JSON-AS per-instance guest-state caveat remains.

Memory scenarios run 270 compile/close operations, then three fixed batches of 36 live modules (4,128 functions each), Close all outputs, drop references and observe GC while keeping Runtime alive, then close Runtime and observe again. Native mapped output is a separate scenario. Input retention is identical. All phases record allocation totals/counts, HeapAlloc/Inuse/Objects/Released, current RSS and process high-water RSS. Neither forced page release nor prompt finalizer reclamation is assumed. Mapping capacity is `len(owner.Mapping())`; rounded payload is a separate metric and neither is physical RSS.

## Deterministic allocation and code results

The control-only and fallback-only stages have identical native hashes to G across all 14 modules. Admission decisions, every function's frame/spill-slot accounting and explicit spill/reload counts remain unchanged through H. Diagnostics prove 1,333/1,380 functions and 23,451/42,481 body bytes use the shared path; existing modules contribute only 2,045/21,069 migrated bytes. These results do not establish a production-traffic speedup.

| Observation | A | G | H | G→H |
|---|---:|---:|---:|---:|
| Deep native B/op | 11,504 | 10,000 | 8,496 | −1,504 (−15.04%) |
| Deep allocs/op | 22 | 26 | 21 | −5 (−19.23%) |
| Fallback native B/op | 8,248 | 8,584 | 8,504 | −80 (−0.93%) |
| Fallback allocs/op | 21 | 22 | 21 | −1 (−4.55%) |
| All 14 modules' code bytes | 111,363 | 110,968 | 103,887 | −7,081 (−6.38%) |
| 256-local i64 probe code bytes | 2,038 | 2,038 | 37 | −2,001 (−98.18%) |
| Deep common scratch accounting | 0 | 1,396 | 1,076 | −320 (−22.92%) |

The single-stage allocation captures isolate each change and are not latency significance tests. Control allocation is bounded by the unchanged maximum depth of 32 and geometric growth; summary and state sizes do not grow. The remaining fallback increase versus A is 256 bytes (+3.10%), the worker allocation class. Pressure and many-local allocation-event costs remain. Compiler payload envelopes do not equal retained Go heap or simultaneous process peaks. Full NodeScratch/ControlScratch/ScalarScratch, hints, attempts, discarded and retained counters are preserved in the diagnostic table.

The optional zero-local change reduces fixed synthetic code: small 49→43, pressure 569→567, join 90→84, large 3,109→3,103, deep 46→40, locals 1,008→103, many 16,401→13,329, large-then-small 19,493→16,415 bytes. Fallback and the five existing modules' native hashes are unchanged. Frames remain conservative: small/deep have 24 bytes versus baseline zero, and many has 12,288 summed frame bytes versus zero. The 256-local probe still has its frame; the 37-byte code result is not frame elision.

Identically stripped standard runtime binaries are A 9,601,184 bytes, G/H 9,654,432: H remains +53,248 bytes (+0.55%) versus A. Build flags and hashes are preserved. This is executable-size evidence, not deployed-runtime RSS qualification; the fixed-batch memory runner is a test executable.

## Fresh timing results

All 90 planned processes passed. Cells show process medians and benchstat 95% confidence envelopes, with six fresh processes per revision per cohort. These are pointwise, exploratory comparisons without multiplicity correction. No sample was discarded. A/H is the complete-design comparison; G/H isolates this review pass. Equal-weight aggregates do not describe production traffic.

| Primary, 200 ms/row | A | G | H | A→H | G→H |
|---|---:|---:|---:|---|---|
| Migrated native compile | 61.32 µs ±16% | 47.99 µs ±19% | 48.10 µs ±5% | −21.56%, p=.002 | +0.23%, inconclusive p=.937 |
| Migrated full compile | 77.00 µs ±17% | 66.89 µs ±15% | 66.90 µs ±8% | −13.11%, p=.002 | +0.01%, inconclusive p=.937 |
| Migrated execution | 44.16 ns ±4% | 43.46 ns ±4% | 40.99 ns ±3% | −7.17%, p=.002 | −5.68%, p=.009 |
| Full-sample native compile | 67.19 µs ±12% | 58.11 µs ±15% | 58.58 µs ±5% | −12.81%, p=.002 | +0.81%, inconclusive p=.937 |
| Full-sample full compile | 86.72 µs ±9% | 76.89 µs ±13% | 76.34 µs ±8% | −11.97%, p=.015 | −0.71%, inconclusive p=.937 |

The preplanned longer execution cohort corroborates the many-locals benefit:

| Execution, 1 s/row | A | G | H | Evidence |
|---|---:|---:|---:|---|
| Migrated aggregate | 44.77 ns ±3% | 42.70 ns ±1% | 41.26 ns ±3% | A/H −7.84%, p=.002; G/H −3.35%, p=.004 |
| Many locals | 84.59 ns ±1% | 56.64 ns ±4% | 40.93 ns ±5% | A/H −51.62%; G/H −27.75%; both p=.002 |
| Small | 15.43 ns ±6% | 15.65 ns ±3% | 15.64 ns ±5% | A/H p=.853; G/H p=.732; inconclusive |
| Pressure | 48.48 ns ±6% | 48.39 ns ±3% | 49.62 ns ±4% | A/H p=.699; G/H p=.100; inconclusive |
| Join | 15.84 ns ±5% | 15.88 ns ±2% | 15.95 ns ±6% | A/H p=.394; G/H p=.331; inconclusive |

Most aggregate execution benefit comes from many-locals. No other individual synthetic execution comparison is significant in the longer cohort. Nonsignificance does not prove equivalence: pressure's median is +2.56% versus G in that cohort, versus +0.29% in the primary cohort. The earlier report's tight pointwise bound does not apply to these samples.

The 256-zero-local probe's native compilation improves G/H 21.41→18.46 µs (−13.77%, p=.002); 1/8/64-local changes are inconclusive. Compared with A, the same 256-local probe remains 17.41→18.46 µs, a +6.01% median with p=.310. Its allocation regression is investigated below. G/H pressure native compilation has an unfavorable +5.86% median (19.40→20.54 µs, p=.485), with broad envelopes; unchanged allocation/spill accounting and only a two-byte code reduction give no evidence of additional compiler work. No repeatable aggregate compilation regression is established by this run.

Existing full `many_funcs` compilation is G/H 533.2→536.3 µs, +0.60%, p=.818. The pre-restart marginal signal is not reproduced in the new evidence and its lost samples are not treated as verifiable results. All six p1/automatic worker timing comparisons are inconclusive versus both A and G. Their wide envelopes, including 128% on A's tiny/p1 row, limit conclusions. No new timing follow-up was selected after seeing favorable rows. The primary `xxhash` G/H execution result is −2.11%, p=.041, but native code is identical and the function falls back: this cannot establish a shared-path benefit.

Admission costs are separately instrumented, not charged as counter collection in production timing. The single H diagnostic pass records 218,240 ns across 1,380 functions, including unsupported functions (G: 185,904 ns). H's one-function values range from 431 ns to 14,117 ns, while 512 small functions total 79,317 ns. These cold, single-pass counters include clock overhead and are not a statistical latency comparison; admission itself is included in every production compilation measurement. Full counter values are retained.

## Corrected fixed-work memory

Values below are medians from six fresh processes. Complete phase samples, allocation deltas, min/max ranges and both worker policies accompany this report. Allocation deltas include the same runner/snapshot overhead and are not timing results. Peak RSS comes from fresh-process `RUSAGE_CHILDREN` after the full two-scenario runner exits.

| Single worker | A | G | H | A→H |
|---|---:|---:|---:|---|
| Public compile/release allocated bytes | 37,770,632 | 27,770,720 | 27,723,440 | −10,047,192 (−26.60%) |
| Public allocation events | 55,066 | 54,818.5 | 54,640 | −426 (−0.77%) |
| Native compile/release allocated bytes | 19,246,176 | 9,284,704 | 9,237,168 | −10,009,008 (−52.01%) |
| Native allocation events | 6,303 | 6,063 | 5,883 | −420 (−6.66%) |
| Retained native code bytes | 167,268 | 163,296 | 134,972 | −32,296 (−19.31%) |
| Actual owned mapped bytes | 606,208 | 606,208 | 606,208 | 0 |
| Page-rounded payload bytes | 278,528 | 278,528 | 262,144 | −16,384 (−5.88%) |
| HeapAlloc after Runtime release | 206,744 | 206,680 | 206,600 | −144 (−0.07%); overlapping ranges |
| Native HeapAlloc after third release | 366,048 | 366,000 | 365,920 | −128 (−0.03%); overlapping ranges |
| Peak RSS, KiB | 20,904 | 22,654 | 24,618 | **+3,714 (+17.77%)** |
| Peak RSS min–max, KiB | 20,460–22,428 | 22,428–22,852 | 24,532–24,856 | H range does not overlap A/G |

G/H native allocation falls 47,536 bytes (−0.51%) and 180 events (−2.97%); 47,520 bytes of the difference match 30 repetitions of the deep/fallback savings, with 16 bytes of runner variation. Public totals fall 47,280 bytes (−0.17%) and 178.5 events (−0.33%). Retained code falls 28,324 bytes (−17.35%) G/H, but reserved mapping capacity does not fall. Payload reductions must not be reported as equivalent memory savings.

Single-worker H release HeapAlloc medians plateau at 1,547,584 bytes through the three public cycles and 365,920 through the native cycles. After Runtime release, A/G/H ranges are 206,504–212,240 / 206,360–211,936 / 206,200–207,000 bytes. The extra-GC epoch and delayed output reclamation caveat from earlier reports remains: the later drop is not attributable solely to engine teardown. No retained shared-scratch growth is established by these observations.

Normal current native RSS after the third release is A/G/H 19,087,360 / 20,711,424 / 22,806,528 bytes: H is +3,719,168 (+19.48%) versus A and +2,095,104 (+10.12%) versus G. Native HeapInuse is 901,120 bytes for all three, while public HeapInuse is 2,154,496 / 2,220,032 / 2,228,224; H is +73,728 (+3.42%) versus A. HeapReleased and object counts are preserved in the phase tables. Similar live Go heap does not clear the RSS increase.

Automatic-worker public compile/release allocation is A/G/H 45,819,452 / 34,521,276 / 34,241,308 bytes. Peak RSS is 21,602 [20,956–22,244] / 22,228 [19,480–24,304] / 22,414 [19,344–24,484] KiB: H is +812 KiB (+3.76%) versus A and +186 KiB (+0.84%) versus G, with overlapping ranges. Mapping capacity remains 606,208 bytes. Automatic-worker public release HeapAlloc medians drift by A +6,720, G +8,756, H +1,904 bytes from cycle 0 to 2; the short observation does not prove a universal plateau. H's Runtime-released HeapAlloc is 307,964 [305,336–317,664] bytes versus A 318,536 [308,496–324,792], −10,572 (−3.32%), with overlapping ranges. Native H release medians are 467,268 / 467,268 / 467,276 bytes. This drift is not attributed to common scratch.

### RSS investigation (separate diagnostics)

The larger normal RSS cost triggered investigation. No binaries, fixture populations or source were changed. All primary samples remain above.

| Fixed-work single-worker diagnostic, n=6 | A peak KiB [min–max] | G | H |
|---|---:|---:|---:|
| Normal-policy replication | 22,562 [20,528–22,796] | 22,520 [21,964–22,928] | 23,496 [22,416–24,588] |
| Child-local transparent-huge-page disable | 19,540 [19,196–19,608] | 19,232 [19,108–19,660] | 19,274 [19,076–19,500] |

The normal replication still has H +934 KiB (+4.14%) versus A and +976 KiB (+4.33%) versus G, but ranges overlap. The diagnostic uses `PR_SET_THP_DISABLE` only in child processes, verifies the setting, and neither changes the global kernel policy nor forces memory release. Its overlapping ranges support sensitivity to huge-page policy and process layout; they do not replace normal results or qualify deployment behavior.

An additional external `/proc/PID/smaps` sampler runs the same fixed workload separately; its overhead disqualifies it as a primary measurement. In that single normal observation A has 2,048 KiB of anonymous huge pages and peak sampled RSS 22,628 KiB, while G/H have none and 19,248/19,328 KiB. All three child-disabled observations have none. This demonstrates run-to-run variation in huge-page residency and even ordering; it does not identify the exact mapping responsible in the earlier primary processes or explain the entire primary RSS gap. Raw phase data and commands for both diagnostic cohorts are preserved.

Peak RSS is a process high-water mark, current RSS includes reusable pages, Go allocation totals omit native mappings, and scratch high-water sums are accounting envelopes. Deployable-runtime retained/released-population RSS remains unqualified. The measured executable-size increase is distinct from these test-runner memory effects.

## Ranked remaining costs and recommendation

1. **Normal process memory:** primary H peak RSS remains +3,714 KiB (+17.77%) versus A and +1,964 KiB (+8.67%) versus G. The replication and huge-page diagnostics show sensitivity, not acceptance. Deployment binaries need fixed-population qualification under both worker policies. The stripped executable remains +53,248 bytes (+0.55%).
2. **Conservative frames and existing-module code:** small/deep retain 24-byte frames versus A's zero; many-small-functions retains 12,288 summed frame bytes versus zero. Existing `many_funcs` still emits 8,363 versus 7,771 bytes, +592 (+7.62%). Parameter ingress and proved leaf-frame elision remain separate next experiments.
3. **Untouched-local compiler storage:** 64/256-local probes retain the existing G/H allocation cost versus A. At 64 locals it is 9,064→10,760 B/op, +1,696 (+18.71%); at 256, 14,569→21,384, +6,815 (+46.78%). Equal 10,000-iteration allocation-profile diagnostics locate H's main cost in the common node arena (9,472 bytes per compilation), local-ID storage (1,024), widths (256), and small stack storage. A's fallback storage differs. This is bounded initial-value storage, not newly introduced per-operation allocation; zero-store omission improves code without shrinking these arrays. Profile timings are excluded from latency results.
4. **Other allocation events:** pressure remains 23 versus A's 18, many-locals 22 versus 21, and existing `many_funcs` 35 versus 31. Deep and fallback event regressions are removed, but fallback retains +256 B/op (+3.10%) versus A. Existing `many_funcs` retains +408 B/op (+0.59%) versus A.
5. **Timing uncertainty:** pressure/join/small execution is not proved equivalent; primary compilation and worker comparisons versus G remain inconclusive. The unfavorable pressure compilation and zero256-versus-A medians are retained. No statistically established aggregate latency regression from these fixes appears, but that is not an acceptance proof.

**Recommendation: keep the confirmed fixes, corrected harness and bounded mitigations; keep the overall PR draft.** The native AMD64 runs corroborate many-locals execution improvement and deterministic allocation savings. Native ARM64, mapping identity and deployment-memory qualification remain incomplete, alongside the workload and checker limits described below.

## Reviewable raw evidence

The [data directory](shared-function-review-data) contains the [timing samples](shared-function-review-data/raw-timing.csv), [all primary memory phases](shared-function-review-data/raw-memory.csv), [phase summaries](shared-function-review-data/memory-phase-summary.csv), [diagnostic and corpus hashes](shared-function-review-data/diagnostic-summary.csv), [zero-local hashes](shared-function-review-data/zero-probes.csv), [binary/harness manifest](shared-function-review-data/manifest.json), [run commands](shared-function-review-data/review-measurement-runs.json), [RSS investigations](shared-function-review-data/rss-investigation-summary.json), and [independent review](shared-function-review-data/independent-review.txt). Benchstat A/H and G/H outputs include every workload and allocations, not only significant rows. `SHA256SUMS` hashes these compact review artifacts. Full process logs, binaries and profiles remain at the durable evidence path above.

## Reproduce and remaining qualification

The evidence directory contains `rebuild_measurement.py`, `run_measurements.py`, `resource_wrapper.py`, `analyze.py`, `analyze_long.py`, `analyze_diagnostics.py`, `run_final_gates.py`, manifests, and all per-process logs. Copy identical `bench/suite/sharing_*_test.go` files from H into frozen A/G before building. Preserve existing output directories before rerunning scripts.

```bash
# From each frozen source, using the identical harness:
GOCACHE=/tmp/wago-pr802-go-cache go test -c -o /tmp/REV-suite.test ./bench/suite
GOCACHE=/tmp/wago-pr802-go-cache go test -c -tags=wago_codegenstats -o /tmp/REV-diagnostic.test ./bench/suite
# From bench/suite, repeat balanced A/G/H/H/G/A blocks three times:
env -u WAGO_SHARED_SCALAR GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off taskset -c 4 /tmp/REV-suite.test -test.run='^$' -test.bench='^(BenchmarkSharing(Native|Full|Exec|ZeroLocals)|BenchmarkCompile|BenchmarkCompileFull|BenchmarkExec)$' -test.benchtime=200ms -test.count=1 -wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as
env -u WAGO_SHARED_SCALAR GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off WAGO_SHARING_MEMORY=1 taskset -c 4 /tmp/REV-suite.test -test.run='^TestSharing(Memory|MappedMemory)$' -test.v -test.count=1
# Automatic-worker memory: same binary/work, WAGO_SHARING_WORKERS=0,
# GOMAXPROCS=4, taskset -c 2-5. No adaptive population sizes.
GOMAXPROCS=1 /tmp/REV-diagnostic.test -test.run='^TestSharingDiagnostic$' -test.v -wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as
# Strict mapping identity qualification on a supporting kernel:
WAGO_REQUIRE_MAPPING_IDENTITY=1 go test -tags=wago_regalloccheck ./bench/suite -count=1
```

Full native gates use the pinned WABT 1.0.41 directory on PATH and `WAGO_SPEC_INTERPRETER` pointing to revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`, with matching `WAGO_SPEC_INTERPRETER_REVISION`. Initialize all three pinned spec submodules. With the writable-cache/stamping environment above, run `go test -p 1 ./...`, `go test -p 1 -tags=wago_regalloccheck ./...`, and `(cd bench && go test -p 1 -tags=wago_regalloccheck ./...)` on the recorded validation merge.

For native ARM64, repeat those gates on an ARM64 host, build A/G/H suite/diagnostic binaries there, and execute the exact balanced cohorts with an available CPU substituted for CPU4. Current cross-build/emulation commands are recorded in `cross-checks.json`; they do not replace native measurements.

Outstanding acceptance gates: qualified OS mapping-identity/reuse tests on a supporting host; original ownership failure attribution; native ARM64 correctness/performance/memory; complete ARM64 emulation beyond focused tests; deployment-runtime fixed retained/released-population RSS under both worker policies; complete common-state transition instrumentation; broader production workload/state qualification. Parameter ingress/leaf frames and ARM64 form selection are separate future experiments, not claimed complete. Keep this PR draft until these limits are addressed and the remaining costs are accepted on evidence.
