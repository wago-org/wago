# PR memory review, 2026-09-21

The [direct-dispatch follow-up](../direct-dispatch/README.md) removes a measured WASI host-call heap allocation and reverses the host-call slowdown. It also records the remaining RSS and GC-phase limits; earlier data below remain historical evidence.

The construction allocation reductions are real. They do **not** imply unchanged heap or RSS. The fixed provider table owns 4,992 bytes allocated once at process initialization. The large ordinary HeapAlloc increases primarily reflect a different GC phase: less allocation means fewer collections, so more dead objects can remain at a fixed command endpoint. The reported post-scavenge RSS increase appeared in one new session but did not repeat with a narrow confidence interval in the selected confirmation. No provider memory leak or safe production memory fix was demonstrated. Both existing optimizations remain in place.

## Source identities and controls

| Role | Wago | WASI |
| --- | --- | --- |
| PR base at review start | `95be283fa511db7b01d86ef72b6c58fbe1ab607a` | `aef440ccb4368b87f122038cf31295bba1b4bd46` |
| PR head at review start | `0e5a0648f87b403e95e26878c86f980bee94ff82` | `a6169cc6ebf86b5d2a98bd009e672dac02be1542` |
| Historical provider experiment base | snapshot fix present in both builds | `6a6684d2ecd2be2d17e5792733d1d0e03b2f2c0e` |
| Current provider comparison | `81066a0438a4ac60f58e220ffcf1b8faa85f1192` in both builds | current PR base, with only the saved production table patch in candidate |
| Snapshot-only comparison | same current diagnostic source; original imports.go through baseline overlay | `a6169cc6ebf86b5d2a98bd009e672dac02be1542` fixed in both builds |

The historical and current provider bases differ. Their `internal/core/core.go` production content is identical, but their repository identities and dependency graphs are recorded separately. Historical-v2 uses Wago `6053223ac18aa579592a86a07c4bc2e28f597347` plus the identical memory test later committed in `81066a0`. Each build manifest records source and benchmark SHA-256 hashes, dependency graphs, flags, commands and binary hashes. Historical reproduction is not labeled current-head validation.

All work used separate temporary worktrees. The original checkout and its unrelated changes/untracked files were preserved. No dependency pin, global module cache, branch history, or original published sample was changed. No merge was performed. Test-only failure cleanup added after collection does not change the successful checkpoints; its later smoke check is separate from these historical binaries.

Environment: Linux amd64, kernel `6.12.107+deb13-amd64`, AMD Ryzen 7 8845HS, Go 1.27.1 (the stable toolchain observed in CI), GOAMD64=v1, CGO_ENABLED=1, `wago_guardpage`, `WAGO_BOUNDS=signals`, GOMAXPROCS=16, affinity CPUs 0–15, GOGC=100, GOMEMLIMIT=off, empty GODEBUG except explicit profiles. Default compile workers remain one. Governor and energy preference were performance. The host had 64 GiB RAM and existing swap use. Per-process records include memory pressure, free memory, load and available thermal readings. These were observed, not changed. Toolchains for correctness: Go 1.22.12 and 1.27.1. No race-enabled performance binary was used.

## Confirmed reproduction defects

- Both worker profile selectors used the obsolete `concurrent/full/requested=0` path and could succeed without running the target. They now anchor each component of `json-as/callers=N/full/public/requested=0`. The caller count follows the argument/GOMAXPROCS rather than a fixed 16. Both profiles must contain one valid leaf with the expected requested workers, phase input, phase limits, callers and GOMAXPROCS. The limits are not observed simultaneous activity. A real CPU profile contains compiler work; its allocation profile contains compiler allocation sites. Setup/profile calibration can still appear in process-wide profiles.
- The paired runner could append new samples to published files and restart numbering. It now requires a new or empty output directory, explicit build/binary paths, binary hashes, a UUID, fixed sample counts, alternating AB/BA order and separate files per process. Summary generation rejects incomplete, duplicate, mixed, changed or invalid records. The memory summary also checks every runtime setting and lifecycle checkpoint.
- Script paths depended on the original user's checkout and a different fixed binary directory. All launchers now resolve their source checkout from their own location and pass explicit build/output paths. Quoted paths with spaces and non-default directories are tested. Missing executables, failed processes, empty filters and existing output files fail.
- The first new legacy replay selected a narrower corpus before its subtest filter. That changed untimed fixture loading and GC phase. Its `historical-exact` raw files are preserved but **invalid as an exact historical replay**. `historical-full-context` uses the original `cjson,tinyxml2` corpus in a new 20-pair run. No samples were overwritten or combined.

The new memory diagnostic uses bounded test-only storage and external sampling. Every child is stopped if the observer fails. Owned compiled/runtime state is closed on diagnostic failure as well as success. The AMD64 and ARM64 worker backend diagnostics already close each returned CodeImage. Serial compiler error paths use deferred code-buffer cleanup; parallel paths join workers before returning errors. No new production counters or pools were added.

## What the historical numbers measured

The original raw command test compiles once, runs one warm command, then runs exactly 1,000 commands. Each command constructs fresh WASI imports/configuration, snapshots and binds imports, instantiates, looks up and invokes the export, validates the result and closes the instance. Compilation and fixture loading are outside the command loop. The `closed-normal` observation still owns the compiled module. `released-normal` follows module close. The phase called `released-gc` actually calls `debug.FreeOSMemory`, which requests **GC and scavenging**, not GC alone.

The quoted minimal 1.492→2.110 MiB and tinyxml2 1.690→2.197 MiB are median `closed-normal` HeapAlloc values from 20 samples. Tinyxml2 664.4→669.4 KiB is heap at `released-gc`; 21,210→21,966 KiB is RSS at that same scavenging endpoint. The original +756 KiB, +3.56%, p=0.004 is reproducible from the published raw rows. Those rows remain historical evidence, not a proof of permanently retained state.

The corrected historical replay reproduced a large ordinary heap increase: tinyxml2 paired mean +506,809 B (95% interval +120,928 to +839,911 B); minimal +493,788 B (+163,712 to +765,729 B). Tinyxml2 post-scavenge RSS changed by +317 KiB (−284 to +931 KiB), so the earlier precise RSS increase did not repeat. The narrower single-module memory sessions are separate experiments, not replacements for this replay.

## New fixed-work memory results

The [plan](PLAN.md) fixed 20 matched fresh-process pairs, AB/BA order, primary outcomes and confidence method before each principal run. One warm command precedes either 100 or 1,000 commands per epoch; repeated runs use four equal 1,000-command epochs. The largest workload is 4,001 commands. Each process has a 60 s timeout and 512 MiB observed RSS limit. No process limit failure is discarded as a sample.

Raw and reused-provider APIs have separate matrices. Normal, GC-only and FreeOSMemory endpoints run in separate processes. Checkpoints cover package initialization/startup, test readiness, runtime/provider/compiled setup, warmup, each completed epoch, all owner cleanup, and the selected diagnostic intervention. The provider setup includes loading its plugin once; instances then use its documented cleanup observer. All measured raw fixtures have no owned OS files or preopens. This does not establish automatic cleanup for a raw bundle with guest-opened files.

At each checkpoint, Go records HeapAlloc, HeapObjects, HeapInuse, HeapSys, HeapIdle, HeapReleased, StackInuse, Sys, TotalAlloc, Mallocs, Frees, NextGC, NumGC and PauseTotalNs, plus available live-heap/GC-target metrics, native-memory registry counts and goroutines. Missing runtime metrics are marked unavailable. A parent samples Linux RSS every 5 ms and obtains PSS, mappings, faults and descriptor counts at checkpoints. Go counters precede the pipe handshake; the observation duration is recorded. This is not a zero-overhead observer, but it is identical in each pair. Sampled phase peaks can miss short peaks; whole-process maximum RSS is recorded separately.

See [memory tables](RESULTS.md) for baseline/candidate means, paired differences and intervals across all three workloads/APIs. Intervals are a deterministic paired bootstrap, 10,000 resamples, 95%; repeated epochs within a process are not independent observations. There is no multiple-comparison adjustment. Separate sessions are not pooled.

For current-base tinyxml2 raw commands:

| Endpoint | Paired mean change | 95% interval |
| --- | ---: | ---: |
| Normal HeapAlloc after cleanup | +925,945 B | +696,569 to +1,175,127 B |
| Normal RSS after cleanup | +29.8 KiB | −659.4 to +730.6 KiB |
| Post-GC HeapAlloc | +993 B | −951 to +2,803 B |
| Post-GC RSS | −30.6 KiB | −627.8 to +570.2 KiB |
| Post-scavenge RSS, first session | +799.4 KiB (+3.83%) | +232.2 to +1,340.0 KiB |
| Post-scavenge RSS, selected confirmation | +234.4 KiB (+1.12%) | −350.8 to +824.0 KiB |

The confirmation also reproduced ordinary HeapAlloc growth (+547,774 B, interval +313,396 to +823,524), but post-GC heap changed by only +488 B (−1,121 to +2,136). Historical single-module sessions gave post-scavenge RSS changes −3.6 and −168.8 KiB, both with wide intervals. Normal RSS also changed direction across sessions. This evidence does not support a stable RSS saving or unchanged RSS, and it does not prove the original RSS increase was impossible.

## Attribution and alternatives

| Explanation | Evidence and limit |
| --- | --- |
| Permanent shared definitions | Initialization tracing enabled before package init reports exactly 4,992 B / 24 allocations. The live profile after cleanup and GC has 4.88 KiB at `core.init.func1`; no other provider allocation sites remain in that raw profile. This is a real bounded startup/live-memory cost. |
| Different GC timing and dead objects | For 1,000 raw tinyxml2 commands, total allocation falls about 11.4 MB and median GC cycles fall 13→9. The ordinary heap difference largely vanishes after GC. The direction depends on where the fixed command count falls in each collection cycle. Minimal/cjson can show lower ordinary heap in another fixture context. |
| Fragmentation / allocator classes | Startup HeapInuse can grow by several Go spans while requested object bytes grow only ~5 KiB. Allocation profiles show different object-size distributions. Endpoint span counters do not prove a particular fragmentation mechanism. |
| Resident unused heap / GC runtime memory | In the first positive RSS session, anonymous writable mappings account for about +748 KiB and file mappings +51 KiB. Post-scavenge HeapInuse changes −7 KiB, while HeapSys changes +1.06 MB and HeapReleased +0.94 MB; Sys changes +0.895 MB. Live objects cannot explain the RSS delta. Scavenging does not release every runtime page. Detailed smaps are retained, but allocator/runtime metadata and page residency are not fully separable here. |
| Retained registration/instance owners | Static definitions contain names, signatures, capabilities, docs and method expressions, with no Plugin or caller capture. Separate rate-1 live profiles and state-map/cleanup tests do not show retained per-command provider owners. Repeated epochs have bounded state counts. This is evidence against a lifecycle leak, not a general proof for every application. |
| Native linear memory and JIT code | Completed epochs have zero active native linear memories and the existing bounded cache has one entry. Reserved inaccessible ranges have zero RSS. Anonymous executable RSS is 12 KiB after cleanup in both current builds; no growing code mapping explains the delta. Native virtual reservation is not resident RAM. |
| Descriptors and goroutines | Completed epochs retain seven observed descriptors (including the two measurement pipes) and two Go goroutines. Neither grows across the bounded runs. The added provider test checks empty instance-state maps after 400 completed commands with guest-opened files. |
| Noise / observation context | Corpus selection materially changes the normal GC endpoint. Current and repeated RSS estimates vary by hundreds of KiB. Thermal/load/pressure and observation delays are retained. The remaining RSS attribution is unresolved; the 5 KiB table cannot be claimed to explain 756–799 KiB. |

Repeated epochs show sawtooth ordinary heap rather than monotonic live-state growth. For example, current candidate raw tinyxml2 heap rises in early epochs and falls again after another GC; reused-provider minimal ordinary heap can approach its 4 MiB GC goal while the last measured live heap remains about 600 KiB. These are normal-GC observations, not forced-GC production claims. The registration saving is once per registered provider, while raw construction saves on each bundle.

## Allocation and timing controls

The unchanged benchmark loops use 20 separate process samples per side, balanced order, 200 ms per timed case and 1,000 iterations for isolated phases. Profiles are separate. The complete raw loop includes fresh imports, instantiation, execution, validation and instance close. Reused-provider per-command loops exclude one-time runtime/provider/module setup and include instance cleanup. `ProviderSetupClose` separately includes runtime construction, provider load and completed runtime close; provider-definition/set preparation is outside its timer. Do not add phase times to predict total command time.

| Current provider base → table patch, snapshot fix in both | Before → after | Evidence |
| --- | --- | --- |
| Raw import construction | 15.99→12.72 us; 32,648→21,256 B; 374→304 allocs | −20.44% time; 70 fewer allocations per construction |
| Complete cjson raw command | 37.09→33.33 us; 421→351 allocs | −10.14% time |
| Complete tinyxml2 raw command | 45.86→41.36 us; 432→362 allocs | −9.81% time |
| Provider setup/close, confirmation | 123.6→120.6 us; 1,768→1,698 allocs | −2.37%; 70 saved once per registration |
| Reused-provider commands | 39 / 67 / 81 allocs, minimal/cjson/tinyxml2 | unchanged counts; no significant time change |
| Direct single WASI host call | 570.2→568.1 ns; 288 B / 2 allocs unchanged | p=0.482 |
| 1,024 WASI host calls | 274.5→277.9 us | +1.26%, p=0.001; selected repeat +1.75%, p=0.002 |

The allocation-site accounting is specific to this toolchain/build and the existing provider patch:

| Site | Per construction, before → after | Ownership and lifetime |
| --- | --- | --- |
| `Plugin.bindings`: temporary definition array/signatures plus bound handler closures | 70 allocations / 6,608 B → 0 | Immutable definitions move to provider-owned process lifetime; 46 owner-bound intermediate handlers disappear. Fresh callback owners remain separate. |
| `binding.callback`: outer adapter captures | 46 allocations / 5,888 B → 46 / 1,104 B | Each registration still owns 46 callbacks through its imports. Only Plugin pointer and handler are captured, instead of the full binding. |
| Fixed `importBindings` initialization | 0 → 24 allocations / 4,992 B once per process | Private immutable provider definitions; no command owner is reachable from the table. |
| Wago HostFunc records, import keys/maps, Params/Results copies | unchanged | Registration owns these values; public defensive copies and duplicate/sealing checks remain. |
| Config/FS/descriptor and instance state | unchanged | Fresh command or provider-instance owners; required cleanup follows the selected API. |

The first two rows sum to the measured 70 allocations / 11,392 B reduction per raw construction. Rate-1 allocation profiles separate those sites from one-time compilation and observer work. The profile is process-wide; it is not interpreted as a timed-phase profile merely because a benchmark timer stopped.

These WASI host calls already allocate; they are not the 72 zero-allocation corpus execution cases. Focused tiny/utf8proc/pcre2 execution controls remain zero B/op and zero allocs/op. The independent snapshot comparison retains its 46-allocation reduction with a fixed optimized provider: cjson 397→351 allocations and 35.40→33.33 us; tinyxml2 408→362 and 43.78→41.54 us. That saving is not counted as new work in this review.

The host-heavy slowdown is a real review finding and is not hidden by the construction benefit. A separate one-line overlay narrowed the private stateFor argument from HostModule to Caller to remove a stack conversion on the raw path. The first 20 pairs improved 1,024 calls by 1.95% but worsened the direct-call control by 0.71%. A new fixed 20-pair verification again improved 1,024 calls (−1.96%, p=0.001) but worsened the direct call (592.7→598.8 ns, +1.03%, p=0.003). Allocations were unchanged. This candidate was **rejected**. It never entered the PR production branch; its patch and both datasets are retained as experimental evidence. No memory acceptance matrix or multi-toolchain correctness claim is made for this rejected candidate. The small host-heavy regression from the original table patch remains unresolved.

## Ownership and correctness

- `core.importBindings` is package-private. Method expressions capture no registration owner. Each `binding.callback` captures its own Plugin and handler. `stateFor` resolves caller identity under the guard lock and locks that instance's filesystem state; the callback releases it on return, including panic unwinding. No mutable current-instance global, guest-memory cache or shared descriptor table was introduced.
- Wago HostFunc/Params/Results still own defensive signature copies. Public provider metadata is built/copied at its ownership boundary; mutation tests cover configuration slices and metadata. Import names, duplicate errors, signature validation, sealing and locking are unchanged. The snapshot fix avoids duplicate adapter construction; it does not remove import checks.
- Provider-owned descriptors are closed by the instance-close observer, then the provider stop path closes remaining initial/state resources. `closeInstance` deletes the identity before closing its locked state. Borrowed standard streams are not closed. Tests cover normal exit, guest/start traps, denied instantiation, cancellation, partial setup, repeated close and cleanup isolation. The Go 1.22-compatible closed-file test uses `File.Fd()` after close.
- Raw Imports is explicitly a one-instance stateful bundle without the provider lifecycle cleanup operation. A guest/caller must manage raw open descriptors according to that API. Instance.Close is not claimed to close raw provider descriptors. This review does not silently change that contract or hide resources in a pool.
- New instrumentation is test-only and bounded. The child sample loop stops on failure; compiled/runtime owners have success and failure cleanup. No new production resource amplification, bounds bypass, permission change, persistent cache or explicit collection was added.

## Checks, status and limits

All 20 scheduled local checks passed, including Wago public/import/host tests, guarded runtime tests, provider full race tests and vet on Go 1.22.12 and 1.27.1, benchmark import/configuration/worker race tests, selected corpus/oracle correctness suites, worker invalid/parallel race tests and a 10-second WASI isolation fuzz run. The final scripts pass eight fixture tests and real CPU/allocation profile and paired/memory smoke checks. The later diagnostic failure-cleanup source also compiles and passes focused tests on Go 1.22.12. `just lint` returned success before each commit. Its standard staticcheck pass still reports the existing unused-code findings that the repository recipe permits; the runtime-tagged staticcheck pass is clean. Those findings were not fixed or called clean. Exact output is saved.

A new Wago-only execution allocation screen ran all 72 leaves at 100 iterations per case: every leaf reports 0 B/op and 0 allocs/op. This is an allocation/correctness screen, not an independent latency comparison. It completed in 20.86 s with whole-process max RSS 34,288 KiB. Its executable hashes are in the final smoke build manifest; its commands and output are in the checks directory. An initial wrapper could not start because `/usr/bin/time` was unavailable; the replacement Python resource wrapper ran the benchmark successfully.

- Script corrections and bounded diagnostic coverage: **Implemented and verified**.
- Memory increase investigation: **Diagnosed, with no accepted production memory change**. Permanent table cost and GC-phase effects are supported; the exact residual RSS mechanism remains uncertain.
- Host-call experiment: **Diagnosed, with no accepted production change**; the narrow argument candidate failed the direct-call control.
- Local execution on another architecture: **Not completed**; no local ARM64 runner/emulator was available. CI architecture results are listed separately.
- No new all-engine/full 984 screen was run in this focused review. The prior compatible full screens remain historical evidence. The new Wago-only screen verified all 72 allocation results; it does not claim full 984-case timing coverage. No new TinyGo linker workaround was added. The earlier locally reported TinyGo failure was not rerun on this review baseline and is not newly classified as pre-existing.

No claim is made that memory use is unchanged, that lower allocations prove lower RSS, or that unresolved checks passed. Exact reproduction commands, source manifests, raw samples, profiles and their archive hashes are in the [evidence index](EVIDENCE.md). CI and commit identities are recorded in the final PR update.
