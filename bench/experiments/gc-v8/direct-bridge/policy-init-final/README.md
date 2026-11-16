# Constructor × retirement policy final gate

## Scope and provenance

Bounded 2×2: baseline(headers), adaptive, initheaders(refined constructor), initadaptive(refined constructor+adaptive), plus contemporaneous V8. No sparse cleanup, no paired/range loop option. All four share the same seven baseline compiler tags, with only retirement tag added to adaptive variants. Exact tags/binary/artifact hashes retained in JSON.

Baseline code 93c7ec34, binary built at documentation source 4d524db8; adaptive built at 4327a2eb, whose differences from 93c7ec34 are evidence/docs only. Constructor code d62c7ca2e5e02ceecb6464965a025c88d276091a; production difference is gc_direct_offset_alloc.go constructor initialization, with correctness tests/docs alongside. Driver source_commit4327a2eb identifies the harness checkout, not all binary build sources. See binary-sources.json for per-variant attribution.

Linux x86-64 Xeon Platinum 8573C, Go 1.27.1; Node 24.22.1/V8 13.6.233.17-node.58 --no-liftoff. GOMAXPROCS1. Serialized in reserved CPU window. 24/24 Wago audit receipts,90/90 timed process receipts,72/72 compile measurements complete, none missing. Exact checksums/artifact hashes/native direct receipts validated. Allocation/minor/full counts match all Wago variants/rounds within each case. Audits: 128 warmups include actual collections; timed: 100 warmups then 9 × 32 calls, 3 rotating fresh-process rounds. 2 MiB young / two 8 MiB old, age2/elision off.

These are six local synthetic artifacts, not the full wasm.fyi suite. V8 internal allocation elimination/GC policy remains unmeasured. Timings include execution and collection, not isolated collector efficiency.

## Runtime medians

Median 27 batch-per-call samples, µs.

| Case | Headers | Adaptive | Init headers | Init adaptive | V8 |
|---|---:|---:|---:|---:|---:|
| struct-allocation-access | 6015.553 | 4959.952 | 6094.516 | 4502.592 | 1388.628 |
| array-allocation-access | 12926.781 | 12004.204 | 12599.113 | 11478.987 | 8573.601 |
| array-fill | 15392.299 | 17524.504 | 13912.149 | 15566.622 | 11408.221 |
| array-copy-checksum | 331.148 | 342.469 | 365.727 | 337.064 | 161.320 |
| array-loop-copy-checksum | 548.798 | 603.703 | 618.545 | 634.449 | 193.330 |
| tree-survival | 2222.430 | 1817.739 | 1832.036 | 1712.770 | 982.507 |

### V8 gate (≤1.15× per case)

| Case | Headers/V8 | Adaptive/V8 | Init headers/V8 | Init adaptive/V8 |
|---|---:|---:|---:|---:|
| struct-allocation-access | 4.332× MISS | 3.572× MISS | 4.389× MISS | 3.242× MISS |
| array-allocation-access | 1.508× MISS | 1.400× MISS | 1.470× MISS | 1.339× MISS |
| array-fill | 1.349× MISS | 1.536× MISS | 1.219× MISS | 1.365× MISS |
| array-copy-checksum | 2.053× MISS | 2.123× MISS | 2.267× MISS | 2.089× MISS |
| array-loop-copy-checksum | 2.839× MISS | 3.123× MISS | 3.199× MISS | 3.282× MISS |
| tree-survival | 2.262× MISS | 1.850× MISS | 1.865× MISS | 1.743× MISS |

### Isolated factors and interaction

Ratios below 1 favor treatment. Interaction=(initadaptive/initheaders)/(adaptive/baseline); deviations from 1 may include noise, not necessarily a mechanistic interaction.

| Case | Policy, old constructor | Policy, new constructor | Constructor, headers | Constructor, adaptive | Interaction |
|---|---:|---:|---:|---:|---:|
| struct-allocation-access | 0.825× | 0.739× | 1.013× | 0.908× | 0.896× |
| array-allocation-access | 0.929× | 0.911× | 0.975× | 0.956× | 0.981× |
| array-fill | 1.139× | 1.119× | 0.904× | 0.888× | 0.983× |
| array-copy-checksum | 1.034× | 0.922× | 1.104× | 0.984× | 0.891× |
| array-loop-copy-checksum | 1.100× | 1.026× | 1.127× | 1.051× | 0.932× |
| tree-survival | 0.818× | 0.935× | 0.824× | 0.942× | 1.143× |

## Compilation footprint

Three rotating fresh-process rounds, 256 Compile+Close iterations per case. Medians below; bytes/op are Go allocations, not peak compiler RSS. Input loading excluded.

| Case | Variant | ns/op | B/op | allocs/op | code-B |
|---|---|---:|---:|---:|---:|
| struct-allocation-access | baseline | 293695 | 56026 | 232 | 1775 |
| struct-allocation-access | adaptive | 334514 | 56026 | 232 | 1775 |
| struct-allocation-access | initheaders | 188162 | 56026 | 232 | 1775 |
| struct-allocation-access | initadaptive | 233073 | 56026 | 232 | 1775 |
| array-allocation-access | baseline | 254089 | 56075 | 223 | 1967 |
| array-allocation-access | adaptive | 221658 | 56075 | 223 | 1967 |
| array-allocation-access | initheaders | 209688 | 56075 | 223 | 1967 |
| array-allocation-access | initadaptive | 172625 | 56075 | 223 | 1967 |
| array-fill | baseline | 231562 | 59979 | 233 | 2543 |
| array-fill | adaptive | 192207 | 59979 | 233 | 2543 |
| array-fill | initheaders | 203743 | 59979 | 233 | 2543 |
| array-fill | initadaptive | 249079 | 59979 | 233 | 2543 |
| array-copy-checksum | baseline | 341837 | 76508 | 276 | 4719 |
| array-copy-checksum | adaptive | 300713 | 76508 | 276 | 4719 |
| array-copy-checksum | initheaders | 285668 | 76508 | 276 | 4719 |
| array-copy-checksum | initadaptive | 288685 | 76508 | 276 | 4719 |
| array-loop-copy-checksum | baseline | 292793 | 76508 | 276 | 4719 |
| array-loop-copy-checksum | adaptive | 336155 | 76508 | 276 | 4719 |
| array-loop-copy-checksum | initheaders | 293641 | 76508 | 276 | 4719 |
| array-loop-copy-checksum | initadaptive | 304246 | 76508 | 276 | 4719 |
| tree-survival | baseline | 237913 | 57339 | 267 | 1455 |
| tree-survival | adaptive | 211450 | 57339 | 267 | 1455 |
| tree-survival | initheaders | 253795 | 57339 | 267 | 1455 |
| tree-survival | initadaptive | 223983 | 57339 | 267 | 1455 |

## Round medians (µs)

Retained for spread and ordering inspection; do not treat 27 samples from 3 processes as 27 independent experiments.

| Case | Variant | Round0 | Round1 | Round2 |
|---|---|---:|---:|---:|
| struct-allocation-access | baseline | 5772.317 | 6227.162 | 6467.513 |
| struct-allocation-access | adaptive | 4062.844 | 5122.352 | 4990.444 |
| struct-allocation-access | initheaders | 6178.878 | 6342.190 | 5672.509 |
| struct-allocation-access | initadaptive | 4331.705 | 4699.301 | 4771.337 |
| struct-allocation-access | v8 | 1159.137 | 1784.385 | 1274.550 |
| array-allocation-access | baseline | 12200.799 | 13628.358 | 12289.380 |
| array-allocation-access | adaptive | 12004.204 | 12446.725 | 10374.083 |
| array-allocation-access | initheaders | 13052.449 | 12083.706 | 12937.054 |
| array-allocation-access | initadaptive | 10224.284 | 11478.987 | 12871.619 |
| array-allocation-access | v8 | 9000.937 | 8163.563 | 8284.520 |
| array-fill | baseline | 15392.299 | 16533.140 | 14640.338 |
| array-fill | adaptive | 29660.829 | 16208.874 | 14770.283 |
| array-fill | initheaders | 13192.593 | 13741.730 | 17489.747 |
| array-fill | initadaptive | 16007.630 | 15566.622 | 14660.845 |
| array-fill | v8 | 8935.573 | 11593.245 | 11821.030 |
| array-copy-checksum | baseline | 358.977 | 290.673 | 370.272 |
| array-copy-checksum | adaptive | 368.452 | 342.469 | 272.683 |
| array-copy-checksum | initheaders | 356.744 | 461.781 | 326.155 |
| array-copy-checksum | initadaptive | 267.288 | 324.595 | 405.007 |
| array-copy-checksum | v8 | 161.320 | 236.514 | 150.300 |
| array-loop-copy-checksum | baseline | 548.798 | 584.319 | 543.530 |
| array-loop-copy-checksum | adaptive | 627.526 | 603.703 | 523.319 |
| array-loop-copy-checksum | initheaders | 555.512 | 648.261 | 618.545 |
| array-loop-copy-checksum | initadaptive | 651.739 | 555.054 | 563.278 |
| array-loop-copy-checksum | v8 | 241.183 | 170.022 | 192.629 |
| tree-survival | baseline | 2130.365 | 2178.647 | 2410.500 |
| tree-survival | adaptive | 1701.523 | 2026.164 | 2472.400 |
| tree-survival | initheaders | 2299.289 | 1818.404 | 1700.814 |
| tree-survival | initadaptive | 1817.953 | 1840.061 | 1623.279 |
| tree-survival | v8 | 982.507 | 898.943 | 1154.365 |

## Verdict and regressions

All six cases miss the ≤1.15× V8 target for every Wago variant. Adaptive improves struct/tree medians but regresses array-fill by 13.9% with the old constructor and 11.9% with the new one; loop-copy also regresses. Refined constructor alone regresses copy and loop-copy by 10.4% and 12.7%, while other cases improve. These mixed observed effects do not justify globally promoting either change from this experiment; neither do they establish that either algorithm is intrinsically worse. See the process-level sensitivity below.

Generated code size, Go allocation bytes and allocations per compilation are identical across all four variants for each corpus case. Compilation timing moves substantially even where code and allocation counts are unchanged. Do not claim a measured compilation improvement or attribute all runtime control movement to these mechanisms. These results justify targeted attribution/repetition, not V8 parity or a universal speedup.

## Preserved setup failure

The initial full run stopped after four successful Wago struct processes and one failed Node process because the relocated harness lacked adjacent `v8.cjs` (MODULE_NOT_FOUND). Raw failed attempt is retained separately and excluded from every table. The exact canonical runner was copied (SHA-256 `640fe8db000f7cde9a12929c3d85a89f469920bd5f62860c48885cee69b9d3a8`), then all 90 runtime processes restarted from round zero. No partial results were spliced. Audit results were already complete and unaffected. No engine failure was concealed.

## Process-level sensitivity and environment caveat

The stored provenance starts at `2026-11-16T19:35:41Z`, Linux 6.18.57 x86-64/glibc 2.41, Intel Xeon Platinum 8573C. Cases rotate by round and variants rotate through baseline, adaptive, initheaders, initadaptive, V8. Per-process sample vectors and process order are retained. There are three independent processes per cell, not 27 independent repetitions.

| Comparison | Round 0 ratio | Round 1 ratio | Round 2 ratio | Median round ratio |
|---|---:|---:|---:|---:|
| Fill adaptive/headers | 1.927 | 0.980 | 1.009 | 1.009 |
| Fill initadaptive/initheaders | 1.213 | 1.133 | 0.838 | 1.133 |
| Copy initheaders/headers | 0.994 | 1.589 | 0.881 | 0.994 |
| Loop-copy initheaders/headers | 1.012 | 1.109 | 1.138 | 1.109 |
| Copy initadaptive/adaptive | 0.725 | 0.948 | 1.485 | 0.948 |
| Loop-copy initadaptive/adaptive | 1.039 | 0.919 | 1.076 | 1.039 |

These ratios use each process's nine-sample median, paired by round. They are a diagnostic sensitivity analysis, not a replacement selected to improve the primary pooled-median result. Fill's old-constructor policy regression is dominated by round 0: adaptive median 29.661 ms (20.416–63.939 ms sample range), versus 16.209 and 14.770 ms in rounds 1–2. Constructor copy changes sign by round; its median-of-process-medians ratio is 0.994, versus the primary pooled ratio 1.104. Constructor loop-copy is slower in all three header-policy rounds, but effect size varies; its adaptive-policy comparison changes sign. V8 control medians also move (fill 8.936 → 11.593 → 11.821 ms; copy 0.161 → 0.237 → 0.150 ms).

The coordinator reported a transient execution-environment `starting` state near completion. The output files, all 90 runtime receipts and 12 compile files persisted; no process reported a runtime failure. No per-row wall-clock timestamps, CPU-frequency/steal-time traces, affinity isolation, or external host-load telemetry were collected, so the event cannot be aligned to particular samples or ruled out as a source of variation. A reserved assistant CPU window does not establish an uncontended physical host. Do not discard the retained data or infer that this event caused any particular regression.

Identical code byte counts are not proof of identical instructions. Nevertheless, the large control movement and ordering sensitivity prevent attributing the pooled regressions specifically to collector or constructor logic. Repeat adjacent baseline/candidate pairs with reversed order across fresh processes on a stable host, first fill and both copy cases, keeping constructor and policy comparisons separate. Preserve process-level statistics and telemetry if available. Constructor header loop-copy merits particular confirmation because its direction is consistent across these three rounds. No default source/policy promotion or categorical bad-algorithm conclusion should follow without that confirmation. The large observed V8 gaps remain unmet in this run; this variance analysis does not manufacture parity.
