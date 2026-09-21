# Setup and cleanup investigation

For new runs, use the [corrected reproduction commands](REPRODUCING.md). Historical command blocks below describe the saved runs; they are not safe append/resume instructions.

Publication note (2026-09-21): the provider change is now submitted as [wasi#21](https://github.com/wago-org/wasi/pull/21). It is still not a released dependency or enabled by Wago's dependency pin. The historical measurement records below retain their original source identities and unpublished status. See [publication checks and branch identities](PUBLICATION.md).

A two-line import setup fix removes 46 allocations per command. Ten interleaved samples show lower cjson and tinyxml2 command latency. Both compatible full suites passed all 984 measurements; all 72 execution cases retained zero B/op and zero allocs/op. Ten fixed follow-up samples did not establish a Wago regression in the full run’s timing flags. The affected correctness checks passed; unrelated TinyGo linker failures remain documented below.

## Source and measurement controls

Both input files were read. The old capture contains 984 measurements at revision `95be283fa511db7b01d86ef72b6c58fbe1ab607a`. The working tree started at that same revision. Its only tracked local change was an unrelated `.gitignore` edit. Existing untracked files, including the input captures and `go.work.sum`, were preserved. No reset, branch switch, push, merge, or commit was performed.

The candidate changes only `src/wago/imports.go` in production. The new tests and diagnostic benchmarks are separate files. The baseline uses the original `imports.go` through Go's build overlay; it uses the same diagnostic source as the candidate. See [source and binary hashes](hashes-final.txt), [initial binary hashes](hashes-initial.txt), and [environment](environment.txt).

Dependencies are unchanged:

- `github.com/wago-org/wasi v0.3.1-0.20260916034125-6a6684d2ecd2`, revision `6a6684d2ecd2`, module sum `h1:9PqUA0cgQ4vJiZn30NVQZx90DnWXTFArJcphATzrkSg=`.
- `github.com/tetratelabs/wazero v1.9.0`, module sum `h1:IcZ56OuxrtaEz8UYNRHBrUa9bYeX9oVY93KspZZBf/I=`.
- `golang.org/x/sys v0.30.0`. The workspace and bench replacements point Wago to this repository. No global module-cache file was edited.

New performance runs use Go 1.27.1, Linux amd64, GOAMD64=v1, CGO_ENABLED=1, `wago_guardpage`, `WAGO_BOUNDS=signals`, affinity CPUs 0–15, GOMAXPROCS=16, GOGC=100, GOMEMLIMIT=off, and empty GODEBUG. The CPU is an AMD Ryzen 7 8845HS. The recorded governor and energy preference are `performance`; boost is enabled. The old report did not record affinity, GC settings, governor, or background load. Its single samples are context, not the controlled regression baseline.

Ten samples per selected case were fixed before the comparison. Baseline/candidate order alternates AB, BA for ten rounds. End-to-end cases use 200ms; isolated command phases use 1000 iterations to bound excluded setup. Profiling runs are separate. The worker matrix uses ten 100ms samples at each requested count, on all 16 CPUs. No sample count was increased to obtain a favorable result. Benchstat is `golang.org/x/perf v0.0.0-20260908200009-22c9c6c9d4da`.

[PLAN.md](PLAN.md) records timer boundaries. Existing end-to-end benchmarks are unchanged. Diagnostics require `-wago.bench.lifecycle`; they are disabled in the full suite. CPU/allocation profiles cover the whole process, including untimed setup. Phase times are diagnostic and must not be added together as if timer and cache effects were absent.

## Confirmed cause and implemented fix

`Imports.snapshot` rebuilt a string key and searched the binding map for every declaration, only to recover the callback that was already stored in the declaration. The 46-declaration diagnostic used 2,208 B and 46 allocations per snapshot. The cjson allocation profile attributed 460,138 objects to this lookup over 10,003 complete command calls. This is about 11.3% of profiled allocated objects. Small-allocation packing means profile object counts need not equal benchmark allocs/op.

The fix stores the I32 event callback in the existing `registeredImport.fn` field, as ordinary host callbacks already do. Snapshot validation reads that field directly. It still checks every declaration, holds the same mutex, seals the same collection, rejects signature errors and nil events, and returns the same binding map. There is no new field, cache, pool, buffer, dependency change, or execution-path work.

Relevant source: `src/wago/imports.go:106` and `src/wago/imports.go:144`. The production patch removes repeated name construction and map lookup; it does not move work outside a timer.

| Focused case | Before | After | Change |
| --- | ---: | ---: | ---: |
| cjson full command | 40.02 us; 38,472 B; 467 allocations | 38.17 us; 36,328 B; 421 allocations | time −4.62%, p=0.009; 46 fewer allocations |
| tinyxml2 full command | 48.78 us; 39,112 B; 478 allocations | 47.32 us; 36,968 B; 432 allocations | time −3.00%, p=0.004; 46 fewer allocations |
| minimal WASI command (1,000-iteration diagnostic) | 29.70 us; 443 allocations | 27.93 us; 397 allocations | time −5.99%, p=0.002; 46 fewer allocations |
| 46-declaration snapshot | 1,953 ns; 2,208 B; 46 allocations | 43.87 ns; 0 B; 0 allocations | time −97.75% |
| tiny instantiation control | 3.388 us; 1,408 B; 4 allocations | 3.433 us; 1,408 B; 4 allocations | +1.34%, p=0.019 |

The small control increase is reported rather than hidden. Its cause is not established. The modified loop does no work for an empty declaration list; code layout and measurement variation remain possible explanations. No extra samples were taken to remove this result. utf8proc and pcre2 instantiation showed no significant focused change. All three selected execution controls retained zero B/op and zero allocs/op.

See [focused benchstat](focused-benchstat.txt), [snapshot benchstat](snapshot-benchstat.txt), and [phase benchstat](phases-benchstat.txt). Both full captures confirm all 72 corpus execution cases at zero B/op and zero allocs/op.

## Remaining command setup cost

The minimal valid WASI command, cjson, and tinyxml2 all required 374 allocations and 32,648 B for raw import construction. Their median diagnostic construction times were 16.37, 16.16, and 16.47 us. This confirms a large fixed setup cost.

The exact pinned WASI implementation calls `core.Imports`, clones configuration, creates fresh filesystem state, builds 46 binding definitions, and registers adapter closures. `binding.callback` captures a binding; `Plugin.bindings` creates state-bound handlers. `Imports.HostFunc` creates declaration/builder storage, and Params/Results copy declared signatures. These are measured remaining costs, not all proven removable duplicates. [Allocated objects](command-objects.txt) and [allocated bytes](command-bytes.txt) distinguish their contributions. `HostFunc` accounts for about 30% of allocated bytes; WASI binding construction and callback adapters account for about 17% and 15%.

The raw `p1.Imports` API explicitly supplies state for one instance. Reusing the entire bundle would share command state and violate that contract. Wazero registers reusable host definitions before its timer and supplies fresh state with each guest module. This is an API lifecycle difference; the comparison was not changed. Immutable WASI definitions may be a later dependency-owned optimization. No dependency fork was needed for the selected Wago fix.

New sequential and concurrent tests use different argument counts, inputs, output buffers, environment configurations, mounts, and read/write permissions. They test normal exit, a guest trap, calls after Close, missing imports, and a trapping Wasm start function. Active native linear-memory registrations return to their previous count. Raw WASI file descriptors, including preopens, are explicitly closed by these tests; automatic descriptor cleanup after traps belongs to the provider lifecycle, not the raw bundle API. This investigation does not claim to add that capability.

## Memory reuse diagnosis

| Fixture | Initial / effective maximum pages | Active segments / bytes | Tables | Imports | Start |
| --- | --- | --- | --- | --- | --- |
| tiny | 0 / 0; no guest memory | 0 / 0 | none | 0 | absent |
| utf8proc | 7 / 65,536 | 281 / 316,533 | none | 0 | absent |
| pcre2 | 4 / 65,536 | 128 / 131,484 | one, min=max=3 | 0 | absent |
| xxhash | 4 / 65,536 | 1 / 59 | none | 0 | absent |

A Wasm page is 64 KiB. All these runs select guarded storage, including the basedata-only tiny case. The large virtual reservation is not resident physical memory. See [fixture metadata and hashes](fixture-metadata.txt).

The existing guarded one-slot cache avoids fresh reservation and registry work. Close applies PROT_NONE and MADV_DONTNEED. The next acquisition restores access; data copying then faults in physical pages. The non-guarded reuse path has a 384 KiB threshold, measured over basedata plus current memory, and clears smaller regions. That threshold does not govern the guarded fixtures.

In utf8proc's whole-process CPU profile, about 71% of samples land in `runtime.memmove` and 22% in system calls. Samples at copying include the effect of page faults; they do not prove that the copy instruction sequence itself is inefficient. The source profile locates the data initialization loop and release path. The synthetic matrix independently varies initial pages (1, 5, 6, 7, 16) and data bytes (0, 4,096, 65,536), with fresh guarded, reused guarded, and reused explicit storage.

At one initial page, reused guarded memory with no data takes about 3.53 us and 0 minor faults/op. Copying 4 KiB raises this to 5.69 us and one fault/op. Copying 64 KiB takes 24.17 us and 16 faults/op. At six initial pages the same data sizes have the same guarded fault counts. Explicit reuse below its threshold avoids refaults by keeping cleared pages resident; above the threshold it also refaults. This supports reclamation followed by page faults as a cause of data-heavy instantiation cost.

The isolated cached engine acquire/release median was 7.13 ns/op. Arena acquire/release medians were arena=4096: 7.3685 ns/op, arena=65536: 7.401 ns/op. All three reported zero Go allocations. These small costs do not explain the data-heavy fixture gaps. pcre2 also placed 61% of whole-process CPU samples in memmove and 29% in system calls.

The all-thread syscall trace over 1,000 utf8proc cycles reports 2,025 mprotect, 1,040 madvise, 86 mmap, and 9 munmap calls, including process/Go startup. The original trace without `-f` missed worker-thread calls and is retained only as an incomplete diagnostic. No timing conclusions use strace.

First use of a newly compiled module differs from warm reuse. The compiler already freezes metadata before publication; instantiation does not copy the whole metadata graph on every call. Diagnostic first-compiled Instantiate+Close medians were 180.6 us for utf8proc and 278.3 us for pcre2. Warm lifecycle medians were 112.3 us and 48.7 us. The isolated Close medians were 22.8 us and 12.6 us. The first-use measurements exclude compilation and final Compiled.Close.

The matrix also includes grow-then-reuse and checks that both initial and grown bytes are zero before writing the next instance. Existing runtime guard reuse tests cover dirty grown pages. No memory policy was changed: retaining more resident memory, omitting zeroing, or adding another pool is not justified under this task's constraints. Data order, overlaps, bounds, imported ownership, and traps remain unchanged.

### Separate phase counters

Test-only Go overlays add timers around internal phases; no instrumented production source is installed in the working tree. Ten alternating baseline/candidate pairs use 1,000 iterations. The probe calls the internal package API with no options argument; the existing public benchmark passes InstantiateOptions. Instrumentation can also alter code layout and escape behavior. Its totals and allocation counts are not substitutes for the unchanged public end-to-end benchmarks. Empty blocks show timer overhead near 20 ns.

| Warm candidate phase (median ns/op) | utf8proc | pcre2 |
| --- | ---: | ---: |
| Public preparation, including metadata/import checks | 210 | 103 |
| Builder preparation | 299 | 517 |
| Engine acquisition | 26 | 24 |
| Linear memory acquisition | 1,750 | 1,188 |
| Arena acquisition | 27 | 27 |
| Globals | 161 | 101 |
| Tables | 21 (empty) | 129 |
| Active data checks and copying | 81,744 | 32,439 |
| Close | 20,199 | 10,830 |

Neither fixture has a start section. A separate synthetic start function stores a checked byte and takes about 1,474 ns in its start block. This confirms that the probe includes start execution. See [raw phase summary](phase-probe-summary.json), [phase benchstat](phase-probe-benchstat.txt), and [overlay generator](prepare-phase-probe.py). Use `bash docs/performance/setup-cleanup/measure-phase-probe.sh` to reproduce this separate diagnostic.

## Memory results for the import fix

Ten paired fixed-operation processes run 1,000 cycles per fixture. Native linear-memory registrations end at Active=0, Cached=1 for both versions. The one cached reservation is existing behavior. Focused process peak RSS medians were 32,458 KiB before and 31,844 KiB after; maxima were 33,312 and 33,268 KiB. These are process measurements, not per-instance allocation sizes.

After explicit GC and OS reclamation, retained process RSS medians were 21,994 → 21,470 KiB for cjson and 21,932 → 21,938 KiB for tinyxml2. Heap retention remains about 1.33 MB. The small RSS variation does not show a material increase. No native memory retention was added to obtain the latency gain. Raw snapshots include RSS, peak RSS, heap bytes, page faults, and native registration counts: [baseline](resources-baseline.txt), [candidate](resources-candidate.txt), [process observations](focused-resources.jsonl). Virtual size and resident size are recorded separately.

## Worker diagnosis; policy unchanged

Automatic mode already uses a 16 KiB score threshold (`body bytes + 64 × functions`), a four-worker cap, GOMAXPROCS, and function-count limits in `internal/functionworkers/policy.go`. The default remains one worker.

The matrix covers requested counts 1, 2, 4, 8, and 0 (automatic), validation, backend code generation, and the full pipeline, with one caller and 16 independent callers. Concurrent callers own separate decoded modules. Requested/effective counts and body-size statistics are reported in every row, but the low-level requested=0 rows have a labeling error described below. Tiny has only two local functions, so requests 4 and 8 resolve to two; public automatic mode resolves to one. Other selected modules can use four automatic workers in the full public compile pipeline. Lua has 359 functions, 176,047 body bytes, and a largest body of 20,934 bytes; json-as has 43 functions and a largest body of 2,445 bytes.

For one caller, automatic full compilation reduced median time from 1.543 ms to 1.022 ms for json-as and from 14.91 ms to 8.74 ms for Lua. With concurrent callers, many_funcs rose from 69.47 us/op to 79.45 us/op, and json-as from 200.04 to 233.74 us/op. Allocated bytes rose from 179,189 to 305,039 and from 306,233 to 614,996 respectively. [Worker benchstat](workers-benchstat.txt) and [complete matrix](diagnostic-workers.txt) contain all counts and stages.

**Correction found during the detailed-summary review:** the diagnostic passes requested=0 directly to the low-level validation and backend APIs. Those APIs treat zero as serial; they do not apply the public automatic policy. The diagnostic nevertheless reports the public policy’s resolved count (four for many_funcs, json-as, and Lua). Thus those low-level automatic rows are serial measurements with incorrect effective-worker labels, and must not be used as automatic phase results. Forced-count phase rows remain usable. Full-pipeline automatic rows call the public configured API and remain valid, including the serial-versus-auto table and worker-default conclusion above. The raw capture is preserved. Corrected automatic phase measurements have not been run, and the diagnostic source still needs this reporting correction.

Validation launches bounded workers with private validator state and joins them before selecting the lowest function error. Backend compilation allocates worker scratch/arenas and per-function results, then merges/finalizes serially. Its hint scan also has bounded parallel work. Allocation profiling identifies `stack.alloc`, `compileModuleParallel`, and `newStackWithCap` as leading byte costs. CPU profiling shows both useful code generation and GC/write-barrier work. This supports temporary-storage and nested-parallelism costs; it does not isolate an exact launch/join or imbalance penalty. No trace was needed to decide to keep the serial default. No worker pool, global scratch cache, extra cap, or machine-code optimization change was made.

## Rejected hypotheses and limits

- Snapshot map copying: snapshot returns the sealed map; temporary key strings were the measured duplicate work.
- A syscall for every data segment: HostBytesChecked only extends a host view after growth; ordinary active segments do not each call mprotect.
- Full metadata freezing on every instance: compiler publication freezes it once; later instantiation reads the cached view.
- Missing memory reuse or an uncapped automatic policy: both already exist.
- Sharing live WASI bundles: forbidden by the raw API's state ownership.
- Switching bounds mode or keeping more physical pages to show a gain: not used as a candidate optimization.

Precise launch/join and per-function imbalance costs remain unisolated. Cross-architecture behavior, hardware-counter attribution, and automatic raw-WASI descriptor cleanup are not claimed. perf_event_paranoid is 3; page faults use getrusage and mapping activity uses all-thread strace instead. No full spec-matrix, TinyGo success, or other architecture is claimed.

## Correctness and command record

Passed: Wago unit tests in normal and guarded builds; affected import race tests; all pinned WASI package tests; WASI core and p1 race tests; new command isolation/race tests; guarded core runtime tests; all corpus correctness/oracle checks in normal and guarded builds; bench vet; `just lint` exit status 0. The lint recipe reports existing standard-staticcheck findings and allows them after runtime-tagged checks pass; this is not a claim that standard staticcheck is clean.

The broad `go test -count=1 ./...` run passed all packages except the standalone TinyGo build package. The initial WABT mismatch (installed default 1.0.42 versus required 1.0.41) was corrected by using the already-installed pinned binary. TinyGo initially failed VCS stamping. With `GOFLAGS=-buildvcs=false`, two tests fail with a duplicate `tinygo_task_exit` linker symbol. The same linker failure was reproduced with the original imports.go overlay. Logs retain both failures; no unrelated production workaround was added.

Exact scripts are checked in with the measurements:

```sh
bash docs/performance/setup-cleanup/build-binaries.sh
python3 docs/performance/setup-cleanup/run-focused.py
python3 docs/performance/setup-cleanup/run-resources.py
python3 docs/performance/setup-cleanup/run-full.py
bash docs/performance/setup-cleanup/diagnose-command.sh
bash docs/performance/setup-cleanup/diagnose-memory-workers.sh
bash docs/performance/setup-cleanup/validate-imports.sh
bash docs/performance/setup-cleanup/validate-go.sh
```

Before running validate-imports.sh, prepend `/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41/bin:/home/jtenner/.local/share/mise/installs/github-web-assembly-wabt/1.0.41` to PATH. The other validation script sets this path itself. The run scripts append logs: use a fresh output directory when reproducing results, rather than mixing new and saved samples.

These use the prebuilt matching test binaries in `.tmp/setup-cleanup`. The baseline build is `go test -c -overlay .tmp/setup-cleanup/baseline-overlay.json -tags wago_guardpage -o .tmp/setup-cleanup/baseline-diagnostic.test ./bench/suite`; the candidate build omits `-overlay` and writes `candidate-diagnostic.test`. The overlay replaces only imports.go with `git show 95be283fa511db7b01d86ef72b6c58fbe1ab607a:src/wago/imports.go`. The resource log records every executed benchmark command. The full command is the compiled equivalent of `just bench run all all 1 1s`, with the fixed environment and affinity listed above.

## Full-suite screen

Both full processes passed 984 matching cases. Baseline duration was 1,797.6 seconds; candidate duration was 1,886.1 seconds. Wall time includes untimed setup and benchmark calibration, so this is not a 4.9% aggregate latency regression. In particular, MemoryGrowSuccess stops its timer around instance setup and cleanup.

The single full samples show cjson at 36.527 → 34.544 us and tinyxml2 at 43.589 → 41.357 us, with the same 46-allocation reductions as the focused run. All 72 corpus execution cases remain at zero B/op and zero allocs/op. Tiny instantiation is 3.381 → 3.330 us; this single result does not erase the small opposite result in the ten-sample focused comparison.

Full-process peak RSS was 29,633,848 → 29,117,760 KiB (about 28.3 → 27.8 GiB), and peak virtual size was 185,455,028 → 184,316,240 KiB. These much larger process peaks include all engines, all workloads, and calibration. They must not be described as an individual Wago instance’s memory requirement. Major page faults were 0 → 115, which is another limit on interpreting single timing differences. The fixed-operation resource comparison above is the better measure of retained memory for this fix.

Eight timing rows changed by at least 10%: seven increases and one decrease. They include Wago commands, execution, decoding, compilation, and an unchanged wazero command. The new concern triggered ten preselected alternating follow-up samples at 200ms for these cases, with two command controls. This is a separate screen follow-up, not additional samples mixed into the original focused comparison. All Wago timing flags were insignificant in this follow-up (p=0.247 to 0.928). The unchanged wazero wren command measured +2.00% (p=0.023). The large single-sample Wago increases did not repeat, but this does not prove that every small timing difference is zero. Both additional Wago command cases removed exactly 46 allocations, and the execution controls remained at zero allocations. See [follow-up benchstat](full-flags-benchstat.txt).

See [full benchstat](full-benchstat.txt), [complete timing flags and allocation deltas](full-summary.json), and [follow-up runner](run-full-flags.py). Allocation counts in WazeroExec are per calibrated batch; changes in calls/batch explain many raw allocation-count differences. Three worker rows show small allocation-count increases of one to four allocations in the single full capture. No worker source was changed; this run does not establish their cause. These are not execution allocations introduced by the import patch.


## Completion status

The import snapshot fix has measured end-to-end benefit, passing affected unit/race/corpus checks, and a complete full-suite screen. There is no demonstrated material increase in retained memory and no new execution allocations. The original ten-sample tiny-instantiation control increase of 1.34% remains a disclosed limit. No independent memory or worker optimization was retained.

At the end of the original snapshot investigation, the remaining work included a provider-owned design and fresh-state tests for immutable WASI definition reuse; memory reclamation trades latency against resident pages; precise worker launch/imbalance costs remain unisolated. Two TinyGo standalone build tests remain blocked by a duplicate linker symbol reproduced on the original source. Full spec-matrix coverage, another architecture, successful TinyGo output, and hardware-counter attribution were not completed or claimed.

## Continuation baseline and diagnostic correction

The continuation baseline includes the completed import-snapshot fix. Its source/environment records are in [continuation/environment.json](continuation/environment.json), [initial source hashes](continuation/initial-source-hashes.json), and [the continuation plan](continuation/PLAN.md). The prior 46-allocation reduction is not counted again.

The worker diagnostic source now distinguishes raw low-level calls, production-policy resolution followed by a low-level phase call, and the public full pipeline. It reports requested and passed counts, validation/backend body-worker limits, GOMAXPROCS, and caller count. Limits are not observations of simultaneous activity. Raw validation only caps by local-function count; backend uses its production resolver, including the CPU limit. Module checks, merge/finalization, and some hint scans remain serial.

Configuration and race tests passed. Ten alternating diagnostic samples reran only the affected zero/automatic phase rows and serial controls. The original raw capture remains unchanged and its old automatic phase labels remain invalid. Corrected results are in [worker correction benchstat](continuation/workers-correction-benchstat.txt). Some same-worker-count tiny controls differ, so their timing differences are not attributed to automatic policy. No production worker policy was changed.

Resource ownership, including the need to wait for asynchronous Runtime shutdown, is recorded in [OWNERSHIP.md](continuation/OWNERSHIP.md). Current construction allocation sites are in [ALLOCATION-OWNERSHIP.md](continuation/ALLOCATION-OWNERSHIP.md).

The provider construction candidate has completed twenty preselected alternating baseline/candidate process pairs. Both builds include the previous snapshot fix; the figures below are additional gains. The provider base is `6a6684d2ecd2be2d17e5792733d1d0e03b2f2c0e`. This is a local, unpublished patch, reproduced with [reproduce-provider.sh](continuation/reproduce-provider.sh); the main workspace and dependency versions are unchanged.

| Operation | New baseline | Provider candidate | Result |
| --- | ---: | ---: | --- |
| Minimal WASI import construction | 16.04 us; 374 allocs; 32,648 B | 13.00 us; 304 allocs; 21,256 B | -18.93% time; -70 allocations; -11,392 B |
| Original complete cjson command | 37.01 us; 421 allocs; 36,328 B | 33.21 us; 351 allocs; 24,944 B | -10.28% time |
| Original complete tinyxml2 command | 45.85 us; 432 allocs; 36,968 B | 41.45 us; 362 allocs; 25,584 B | -9.61% time |
| Minimal complete raw command | 26.87 us | 23.61 us | -12.12% time |
| One direct WASI call including public Wasm entry | 560.0 ns; 2 allocs; 288 B | 558.6 ns; 2 allocs; 288 B | No significant latency change |
| 1,024 WASI calls in one guest invocation | 269.4 us; 2,048 allocs | 268.7 us; 2,048 allocs | No significant latency change |
| Provider runtime setup and completed shutdown | 124.8 us; 1,769 allocs | 120.1 us; 1,699 allocs | -3.71% time |

Time decreases above have p<0.001, n=20, except the two direct-call controls, which have p=0.825 and p=0.529. Command median uncertainty is approximately 1–2%; import-construction uncertainty is 3%. See [command benchstat](continuation/wasi-focused-benchstat.txt), [phase benchstat](continuation/wasi-phases-benchstat.txt), and [host/owned-lifecycle benchstat](continuation/wasi-host-owned-benchstat.txt). Isolated phase timings are not added to estimate end-to-end time. Lower construction allocation can change GC work charged to a later phase.

The supported owned-lifecycle benchmark creates a runtime, loads the provider, and compiles once outside the per-command timer. Each timed command instantiates, runs, and closes its instance, including the provider observer. Required runtime cleanup uses CloseContext after the benchmark. ProviderSetupClose separately measures runtime creation, plugin loading and completed shutdown; fixed provider definition/set preparation is outside that timer. Per-command owned-lifecycle allocations remain 39/67/81 for minimal/cjson/tinyxml2. The original raw command benchmark is unchanged and does not gain a new descriptor-cleanup contract.

The allocation profiles attribute the reduction to a private fixed definition table and removal of an intermediate callback layer. The old table, signature literals and inner callbacks were reconstructed for every bundle. The old outer callback captured a 112-byte binding; the replacement captures only the provider pointer and static handler. Final Params/Results copies, import keys, registration records, filesystem state and locking remain. The candidate still allocates 46 owner-specific callbacks, now about 1,104 B per bundle instead of 5,888 B. No lookup, lock, reflection or allocation was added to host calls. [Ownership and safety review](continuation/SECURITY.md) describes the boundaries.

Twenty bounded 1,000-command resource runs found no descriptor growth or major faults. Normal retained RSS medians were 24,774→24,614 KiB (minimal), 24,778→25,276 KiB (cjson), and 25,032→25,636 KiB (tinyxml2); none was statistically significant. Post-forced-GC tinyxml2 RSS was 21,210→21,966 KiB (+3.56%, p=0.004). This remains a disclosed limit, not a claimed memory improvement. The fixed table retains about 5 KiB of Go data; normal heap snapshots also depend on the current GC cycle. Whole focused-process peaks were 34,780→38,216 KiB and include calibration and multiple cases. See [retained memory comparison](continuation/wasi-retained-benchstat.txt), [all resource samples](continuation/wasi-resources.jsonl), and [resource summary](continuation/wasi-retained-summary.json).

The tiny/utf8proc/pcre2 instantiation controls were unchanged within uncertainty. Execution allocations remain zero in those focused controls. The utf8proc execution time changed by +0.39% (p=0.015); it is a small adverse control result and is not hidden by the command gains. The final full screen below verifies all 72 execution cases.

Provider package tests, Wago normal and guarded unit tests, affected race tests, and 20 seconds of argument-isolation fuzzing (1,040,031 executions) passed. The first final provider test command incorrectly combined signal bounds checks with an unguarded build; its failure is preserved and the corrected command passed. Earlier test-fixture errors and the correction from asynchronous Close to completed CloseContext are also preserved in their logs.

A second worker diagnostic issue was found during source review: the original backend benchmark helper drops the serial code-image owner, which has no finalizer. The corrected diagnostic directly calls the backend and closes its CodeImage. Its backend rows therefore include resource release. Earlier corrected-label backend rows remain preserved but are superseded for memory analysis. The original suite benchmark is unchanged. This is a benchmark ownership defect; it is not evidence that the public compile API leaks.

The final corrected worker run includes ten samples for raw 0/1/2/4, resolved automatic mode, and public 0/1/2/4 at 1, 4 and 16 callers. [Worker benchstat](continuation/workers-owned-benchstat.txt) and [effective-count records](continuation/workers-effective-counts.json) preserve the distinction between API input and phase limit. Forced four on the tiny two-function control can use at most two body workers; automatic mode remains one. The original default remains one.

| Public full compile | One worker | Automatic | Allocation bytes per call, one → auto |
| --- | ---: | ---: | ---: |
| many_funcs, 1 caller | 442.8 us | 370.6 us (-16.30%) | 179,386 → 288,024 |
| many_funcs, 16 callers, aggregate | 118.6 us | 136.0 us (+14.65%) | 179,582 → 289,943 |
| json-as, 1 caller | 1.541 ms | 1.199 ms (-22.17%) | 306,601 → 613,687 |
| json-as, 4 callers, aggregate | 549.6 us | 591.9 us (+7.69%) | 307,464 → 614,219 |
| json-as, 16 callers, aggregate | 343.3 us | 412.4 us (+20.13%) | 306,919 → 621,965 |
| Lua, 1 caller | 15.282 ms | 9.420 ms (-38.36%) | 2,271,595 → 3,923,397 |
| Lua, 4 callers, aggregate | 4.508 ms | 3.676 ms (-18.45%) | 2,270,634 → 3,923,588 |
| Lua, 16 callers, aggregate | 2.426 ms | 2.531 ms (not significant, p=0.247) | 2,270,062 → 3,906,257 |

These are comparisons of existing modes, not gains from a production patch. Except for the stated Lua saturated case, displayed timing changes have p<0.001, n=10. Most median uncertainty is 1–5%; the saturated Lua serial sample is ±10%. Same-count tiny validation controls differ in separate raw/policy processes, so those differences are not assigned to a policy effect.

A separate bounded test reports individual latency and process memory. For Lua with one caller, individual median latency is 14.98→9.39 ms at requested one→four and median peak RSS is 22,942→25,160 KiB. With 16 callers, individual median latency is 34.72→36.22 ms, median maximum latency is 43.83→75.53 ms, and process peak RSS is 80,682→73,934 KiB. The non-monotonic peak is not a memory-saving claim: GC timing and worker lifetimes differ, while minor faults increase from about 13,882 to 29,327 per bounded process. json-as with four callers increases median peak RSS from 22,210 to 26,418 KiB. [Resource summary](continuation/worker-memory-summary.json) retains per-configuration peaks, mapping counts, faults and separate latency measures.

[Worker storage diagnosis](continuation/WORKERS.md) attributes the major costs to private operand-node growth, completed function code buffers, per-function result metadata and the ordered final copy. Existing code already caps speculative per-worker local/control/stack storage, shares immutable module metadata, and avoids unused coordinator scratch. Reducing those buffers further would need evidence against repeated growth/copying or a changed lifetime contract. No compiler temporary-storage or scheduling production change is accepted.

The separate bounded memory experiment compares complete warm Instantiate-plus-Close under three stated policies: Wago guarded private memory with its existing release-time reclaim; wazero default initial capacity; and wazero maximum capacity. Maximum memory is 32 pages (2 MiB), so the capacity experiment does not reserve multi-GiB guest buffers. Initial memory is 6 or 16 pages; active data is empty, 4 KiB, 64 KiB or dense. Each comparison has ten alternating engine-order samples at 200ms. [Memory-policy benchstat](continuation/memory-policy-benchstat.txt) contains all timings, bytes and allocations.

At 16 pages with no data, Wago takes 4.906 us versus wazero default 111.777 us. With a dense 1 MiB active segment, Wago takes 394.7 us versus wazero default 167.1 us. Wazero maximum-capacity mode takes 277.0 us for the empty case and 213.7 us for dense data; the latter has ±59% median uncertainty and must not be treated as a precise ranking. Wago's empty/sparse and dense cases therefore have different tradeoffs. Go allocations alone are misleading: Wago reports 1,168 B and 3 allocations per lifecycle while guest storage is native memory.

Separate resource processes record first use, 128 warm cycles, normal retained memory and post-forced-GC memory. The original resource test accidentally retained a closed Wago compiled object through a deferred bound Close method. Its old raw files are preserved; released-owner conclusions use the corrected [resource summary](continuation/memory-policy-released-summary.json). The correction explicitly drops both closures after completed cleanup. Timing samples were not rerun or selected again.

No MADV_POPULATE_WRITE production experiment was implemented. No release-time reclamation, guard protection, cache bound, zeroing, imported-memory ownership or data-copy order changed. This policy comparison supports a measured tradeoff; it does not demonstrate a safe memory-latency fix. Writable-page preparation, resource-limit fallback, syscall attribution and concurrent-instantiation tests for such a patch remain not completed. No unsupported memory optimization is retained.

Final focused correctness checks passed for worker configuration and race tests; compiler/validator tests, including existing mixed-size, deterministic-output and invalid-function tests; normal and guarded corpus/oracle/catalog suites; semantic corpus; guarded runtime; guarded WASI core/p1 race tests; command isolation and mutation tests; and `just lint`. The broad root-package run passed all packages except standalone tests whose generated temporary modules rejected an explicitly forced GOWORK. Retesting that package with normal workspace discovery removed those workspace errors. Two TinyGo tests still fail with duplicate `tinygo_task_exit` linker symbols. The symbol failure was reproduced on the new baseline, with the completed snapshot fix, in TestBuildTinyGoEmbedsArtifactWithoutCompiler. This is not an all-tests-pass claim. See [test status](continuation/final-check-status.txt), [corrected baseline TinyGo log](continuation/test-standalone-new-baseline-workspace-corrected.txt), and [corrected standalone log](continuation/test-standalone-final-workspace-corrected.txt).

Exact commands are retained in [focused correctness](continuation/check-wasi.sh), [final correctness](continuation/check-final.sh), [paired WASI runs](continuation/run-wasi-pairs.py), [worker runs](continuation/run-workers-owned.py), [memory-policy timing runs](continuation/run-memory-policy-timing.py), [corrected memory resource runs](continuation/run-memory-released.py), [provider profiles](continuation/profile-wasi.sh), and [worker profiles](continuation/profile-workers.py). The two standalone correction commands use `env -u GOWORK GOFLAGS=-buildvcs=false go test -count=1 ./cli/manager/internal/standalone`; the baseline reproduction also adds `-overlay=$PWD/.tmp/setup-cleanup-next/baseline-overlay.json` to GOFLAGS and selects TestBuildTinyGoEmbedsArtifactWithoutCompiler. Original failed commands and raw files are preserved.

[Final source identities](continuation/final-identities.json) verify zero root production changes since the new baseline. All production changes in this continuation are in the reviewable local provider patch. The previous imports.go snapshot fix, unrelated .gitignore change, original benchmarks and existing untracked files are preserved. No commit, push, merge, reset, stash or branch switch was made.

## Continuation full screen and final status

Both new full screens passed **984 measurements**: baseline in **1,795.6 seconds**, candidate in **1,798.1 seconds**, about 30 minutes each. Both have **72/72 existing corpus execution cases at zero B/op and zero allocs/op**. Wago remains faster than wazero in **all 80 paired full-compilation cases** in each screen. See [full comparison](continuation/full-benchstat.txt), [screen summary](continuation/full-summary.json), [full runner](continuation/run-full.py), and [final benchmark/source hashes](continuation/full-source-hashes.json).

The single full command samples are cjson 32.853→32.271 us and tinyxml2 40.284→39.473 us. They retain the full 70-allocation reduction, but their latency gains are smaller than the focused gains. These are different process contexts and one sample per case; they do not replace the paired focused comparison. Complete-command bytes fall by 11,384 B/op; isolated construction falls by 11,392 B/op. The reported absolute values preserve this small allocation-accounting difference.

Six timing rows changed by at least 10%: three unchanged wazero instantiation controls, one Wago parallel-execution increase, and two Wago execution decreases. Twenty preselected alternating follow-up pairs measured all six, plus command, tiny-instantiation and utf8proc execution controls. **None of the six flags repeated significantly** (p=0.108–0.815). The Wago parallel jacobi-2d case is 719.9→719.5 us, p=0.607. The command controls again improve: cjson **-9.74%** and tinyxml2 **-8.68%**, p<0.001. The initial small utf8proc execution increase did not repeat significantly (p=0.512); the original +0.39% observation remains disclosed. See [follow-up benchstat](continuation/full-followup-benchstat.txt), [selection and fixed sample count](continuation/full-followup-selection.json), and [follow-up runner](continuation/run-full-followup.py). No follow-up samples were merged into the original focused data.

Three worker rows increase by one allocation in the single full screen. No compiler source changed; dispatch-dependent scratch growth and single-sample counts limit attribution. Allocation-count changes in wazero execution rows also reflect different calls/batch. There are no new allocations in the 72 Wago execution cases.

Full-process peak RSS is **29,925,260→30,695,812 KiB** (about 28.5→29.3 GiB); peak virtual size is **185,614,624→186,597,788 KiB**. Major faults are **0→123**. These totals include all engines, calibration, and the original low-level backend benchmark's missing code-image release. They are not individual-instance memory results. The bounded focused lifecycle measurements remain the appropriate retention check. [Full resource records](continuation/full-resources.jsonl) and [host timeline](continuation/full-host-timeline.jsonl) preserve load, thermal readings and memory pressure. The thermal-zone reading ranged from 78 to 99 C during the recorded full-screen samples; brief memory-pressure events occurred. No CPU, GC or governor setting was changed.

The private provider definition table has an explicitly measured one-time cost: **4,992 bytes and 24 allocations per process**, from twenty alternating `GODEBUG=inittrace=1` diagnostic pairs. The instrumented median core initialization clock was 0.001 ms; this is not an uninstrumented startup latency claim. A separate twenty-pair cold minimal-command control includes process/package startup, compilation, execution and cleanup. Its elapsed time is **4.195→4.126 ms**, p=0.947; CPU time is unchanged within uncertainty. Peak RSS is 18,500→18,320 KiB in that control. See [initialization records](continuation/provider-init-summary.json), [initialization runner](continuation/check-provider-init.py), and [cold-command benchstat](continuation/cold-command-benchstat.txt). The steady per-bundle savings do not hide the new one-time definition cost.

| Task | Status | Accepted result |
| --- | --- | --- |
| Worker diagnostic correction | Implemented and verified | Explicit raw/policy/public paths, production policy resolution, truthful limits, code-image release, configuration/race tests and new samples |
| WASI construction | Implemented and verified | Local provider patch at the exact pinned revision; 70 fewer allocations per bundle; passing ownership/isolation/race/fuzz checks, host controls and full screens |
| WASI resource ownership | Diagnosed, with no accepted production change | Supported provider cleanup verified; raw Imports still has no automatic provider-cleanup contract; completed runtime cleanup uses CloseContext/WaitClosed |
| Worker temporary storage | Diagnosed, with no accepted production change | Allocation ownership and concurrent cost measured; existing bounds/shared metadata confirmed; default remains one |
| Memory policy comparison | Diagnosed, with no accepted production change | Bounded policy/latency/retention comparison; existing reclamation and zeroing preserved |
| Writable-page preparation optimization | Not completed | No MADV_POPULATE_WRITE production patch or acceptance claim |
| TinyGo standalone success | Not completed | Two linker failures remain explicit; the duplicate-symbol failure reproduced on the new baseline |

The provider patch is **locally tested and unpublished**. It is not silently enabled through the main go.work or a dependency upgrade. The patch and test patch are independently reviewable under [continuation/patches](continuation/patches); [reproduce-provider.sh](continuation/reproduce-provider.sh) creates isolated source/workspace copies from the pinned commit. No root production change beyond the already-completed snapshot fix was introduced. Another architecture, a full specification matrix, precise worker launch/imbalance attribution, and a production memory optimization are not claimed.
