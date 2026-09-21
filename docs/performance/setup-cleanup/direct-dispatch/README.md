# WASI host-call allocation fix

The tested provider fix removes a 224-byte heap allocation from each WASI host call. The call-local Plugin copy remains private, but direct handler dispatch lets Go keep it on the stack. Against the current PR code, the single-call control improves 12.98% and the 1,024-call control improves 22.26%, with 20 matched process pairs. This addresses the repeated host-call slowdown found in the earlier review. It is not a claim that whole-process RSS is now lower in every workload.

## What the earlier +799 KiB meant

One session was a batch of 20 baseline and 20 candidate fresh processes, paired in balanced order. Each process ran one warm tinyxml2 command and 1,000 measured commands, completed cleanup, then explicitly requested GC and scavenging. Mean whole-process RSS was 20,872.4 KiB before versus 21,671.8 KiB after: +799.4 KiB, or +3.83%. It was not a per-command allocation or an observed 799 KiB leak from every instance.

That session's paired 95% interval was +232.2 to +1,340.0 KiB. A separate session found +234.4 KiB, with an interval −350.8 to +824.0. The repeat does not rule out an increase; it leaves its size uncertain. The ~5 KiB permanent provider table and the much smaller post-GC live-heap change do not explain that RSS difference by themselves. The [earlier memory review](../memory-review/README.md) keeps all those data and limits.

## Source and mechanism

Provider fix commit: `1cad4801f12ec66e5ab621f5b0832a146052ba6d`.

Baseline Wago: `3c56233591dee5e325f7c24b86f665e6107eae96`. Baseline provider: `08cbb5523586427f060a5b0b422239aadc19448d`. Both already include their respective snapshot and construction fixes. This comparison does not count the earlier 46 or 70 allocation reductions again. The candidate changes only provider `internal/core/core.go`; the [production patch](provider.patch) applies to that exact provider base. The test addition is separate from benchmark source and covers the same valid operation in both versions.

In `binding.callback`, `current := *e` allocated 224 bytes because its address went through an indirect function-valued handler. Escape analysis identifies that exact call as the cause. A separate rate-1 allocation profile records 218.97 KiB at that line for 1,001 minimal-command calls. The candidate uses a private bounded handler ID and a direct-call switch. The same local copy no longer escapes; the corresponding heap allocation disappears from both escape analysis and the profile. A 64-byte Caller interface allocation remains. The existing stateFor signature, state resolution, locking, error paths and callback cleanup are unchanged.

The added test fails on the baseline with two objects per host call and passes on the candidate with at most one. A differential test invokes all 46 public imports with zero and maximum-width argument values and compares the original direct method reference's results, guest memory, output and exit status. Existing valid-operation, isolation, mutation, permission, trap, cancellation and cleanup tests also pass.

## Timing and allocation results

Twenty new process pairs were selected before verification. AB/BA order alternates, and both binaries contain identical benchmark code and workload bytes. Ordinary timing runs contain no profiling. The independent original-provider comparison holds Wago fixed and restores only the original provider core.go from `aef440ccb4368b87f122038cf31295bba1b4bd46` in its baseline; it is not a subtraction across sessions.

| Current PR → direct dispatch | Before | After | Change |
| --- | ---: | ---: | --- |
| One host call | 572.6 ns ±1%; 288 B / 2 allocations | 498.3 ns ±1%; 64 B / 1 allocation | −12.98%, p<0.001, n=20 |
| 1,024 host calls | 280.0 us ±1%; 294,912 B / 2,048 allocations | 217.7 us ±1%; 65,536 B / 1,024 allocations | −22.26%, p<0.001, n=20 |
| 1,024 calls including setup/cleanup | 301.6 us ±1% | 235.8 us ±2% | −21.82%, p<0.001, n=20 |
| Complete cjson command | 33.48 us ±1% | 33.51 us ±1% | no significant change, p=0.718 |
| Complete tinyxml2 command | 41.74 us ±1% | 41.47 us ±1% | no significant change, p=0.583 |
| Provider setup/close | 120.0 us ±1% | 119.6 us ±2% | no significant change, p=0.989 |

Against the original provider code, a separate 20-pair comparison gives 587.0→503.8 ns for a single call (−14.17%) and 280.1→228.2 us for 1,024 calls (−18.53%), both p<0.001. The earlier measured slowdown is reversed in these controls. Cjson and tinyxml2's measured exports do not exercise this callback allocation; their construction allocations remain unchanged. Savings apply to actual WASI calls, not unconditionally to every command or registration.

## Memory results and limits

The fixed-work matrix uses 20 fresh process pairs per workload/API/intervention, three workloads, raw and reused-provider APIs, and separate ordinary, GC-only and scavenging processes. One warm command precedes 1,000 commands. Another 20-pair matrix has four equal 1,000-command epochs. A separate preselected 20-pair confirmation checks the minimal command's ordinary endpoint. All samples and phase counters are retained in the archives; confidence intervals use the existing paired bootstrap (10,000 resamples, 95%).

| Minimal command, ordinary heap after completed cleanup | Paired mean change | 95% interval |
| --- | ---: | ---: |
| Raw, 1,000 commands | −219,376 B | −329,842 to −116,911 B |
| Raw, separate 1,000-command confirmation | −301,658 B | −430,653 to −163,982 B |
| Reused provider, 1,000 commands | −218,478 B | −220,016 to −217,001 B |
| Reused provider, separate confirmation | −217,481 B | −218,792 to −216,128 B |
| Reused provider, four 1,000-command epochs | −886,390 B | −934,737 to −832,125 B |
| Raw, four 1,000-command epochs | **+1,350,731 B** | **+822,995 to +1,854,723 B** |

The last row is important: fewer allocations can shift the collection point. At 4,000 raw commands, median GC count changes 29→28 and more dead heap remains uncollected. The last recorded live heap stays about 540 KiB in both versions. This is not a universal reduction in ordinary HeapAlloc. No production GC, scavenging call or GC-setting change was added to conceal it.

Whole-process RSS is not significantly reduced in the principal endpoint comparisons. For minimal/raw, the 1,000-command RSS difference is −192.8 KiB (95% interval −859.2 to +412.0), and confirmation is −193.2 KiB (−827.6 to +437.2). Tinyxml2/raw post-scavenge RSS changes −118.6 KiB (−683.6 to +417.0). Post-GC live-heap differences are small and uncertain. One tinyxml2/provider post-scavenge HeapInuse row increases about 30 KiB (interval +6 to +56 KiB), while other span observations change in both directions. These observations do not establish a general RSS or fragmentation fix.

Completed epochs retain zero active native linear memories, the existing one-entry bounded cache, seven observed descriptors including the two observer pipes, and two Go goroutines. No continuing descriptor/native-state growth was found. Sampled interval peaks, whole-process peaks, stack counters, mappings, faults, live heap, GC targets and all ordinary/diagnostic checkpoints are preserved. Sampled peaks use the existing 5 ms external observer and can miss short peaks. The 4,992-byte/24-allocation process-start definition table remains; it is not counted as a new saving.

## Ownership and security

The handler ID comes only from the private fixed table and cannot be selected as an unchecked guest value. Each callback still captures its own Plugin owner. The copy's fields and per-instance fsState selection are the same; only its allocation location changes. Go's escape analysis and the differential/lifecycle tests support its call-bounded lifetime. No handler retains that copy after return. Every lock and unlock remains in the original callback/state-resolution paths. No stream ownership, mount rights, capability checks, zeroing, bounds checks, cancellation path, import validation or public defensive copy was removed. No pool, cache, per-instance tracking state, unsafe alias, or guest-memory cache was added.

The additional dispatch has a fixed 46-handler bound. Its runtime cost is included in the host-call measurements above; the measured reduction in allocation and latency supports it. This does not promise that every unmeasured workload has exactly the same speedup.

## Validation and reproduction

Local tests passed on Go 1.22.12 and 1.27.1: complete provider race tests and vet against its pinned Wago dependency, provider race tests against current Wago, affected Wago benchmark race tests, corpus/oracle checks, and a 10-second isolation fuzz run. A new Wago-only screen checks all 72 execution leaves at 100 iterations each: all report zero B/op and zero allocs/op. No new full 984-case timing run is claimed. Local execution is Linux amd64. Provider production commit `1cad4801f12ec66e5ab621f5b0832a146052ba6d` passed all 14 CI checks, including ARM64, Windows, Go 1.22 and race coverage; final documentation-head CI results are recorded on the PRs. `just lint` runs before commits; its permitted standard staticcheck warnings remain explicit.

Environment: Go 1.27.1 for performance, Linux amd64, `wago_guardpage`, signal bounds checks, GOMAXPROCS=16, affinity CPUs 0–15, GOGC=100, GOMEMLIMIT=off, empty GODEBUG except separate profiles. Kernel/CPU, toolchain, build environment, source/benchmark/binary hashes, workload hashes, command lines, thermal/load/memory-pressure observations, run order and sample counts are in each manifest. No Wago dependency pin changes.

The reproduction runner now accepts an explicit production patch; its default remains the original historical construction patch. Run from this checkout, using new work/output directories:

```sh
bash docs/performance/setup-cleanup/continuation/reproduce-provider.sh \
  '/tmp/wago direct build' \
  --provider-base 08cbb5523586427f060a5b0b422239aadc19448d \
  --production-patch docs/performance/setup-cleanup/direct-dispatch/provider.patch
python3 docs/performance/setup-cleanup/continuation/run-wasi-pairs.py \
  --binaries '/tmp/wago direct build' --output '/tmp/wago direct timing' --samples 20
python3 docs/performance/setup-cleanup/memory-review/run-memory.py \
  --binaries '/tmp/wago direct build' --output '/tmp/wago direct memory' --samples 20
```

Use `--epochs 4 --interventions normal` for the repeated matrix, or `--modules minimal-wasi --interventions normal` for the independent confirmation. Profiles use their own new directory with `--samples 1 --modules minimal-wasi --interventions gc --profile`. Summarize the selected directory with the existing summary scripts. The [archive manifest](archives.json) identifies the raw runs and checksums. Build manifests and exact recorded build/check scripts are preserved with the evidence. Archive text has original bytes; trailing whitespace may be removed from derived text reports only.

Status: host-call temporary-allocation fix **implemented and verified locally**; memory diagnosis **completed with limits**. A general RSS reduction, lower ordinary heap at every command count, local execution on another architecture, and a new full 984-case timing comparison are **not completed**.
