# Direct bridge exploratory comparison

Runtime basis: 4959f11 plus public admission fix from d179ec0, isolated harness 11ddd9a6ac7f19e6682988a8f63d419626fe4009.
The additional canonical unknown-ABI receipt rejection guard is absent from this isolated snapshot; valid freshly compiled ABI1 artifacts do not exercise that branch. Three fresh processes per variant/workload; 3 warmup calls then 5 checked calls per sample. This is not the earlier V8 protocol.

| Case | Variant | median ms/call | three samples ms | timed minor/full per launch | timed RSS (invalid) | median Go heap MiB |
|---|---|---:|---|---|---:|---:|
|struct-allocation-access|helper_only/handles|199.299|217.421, 199.299, 192.789|647/7, 647/7, 647/7|discarded|14.71|
|struct-allocation-access|helper_only/direct|307.288|314.749, 304.792, 307.288|742/5, 742/5, 742/5|discarded|24.58|
|struct-allocation-access|native_direct_reads/direct|277.793|265.187, 291.927, 277.793|742/5, 742/5, 742/5|discarded|24.58|
|array-allocation-access|helper_only/handles|181.671|181.671, 186.896, 179.452|1723/16, 1723/16, 1723/16|discarded|10.95|
|array-allocation-access|helper_only/direct|357.895|357.895, 362.926, 348.211|1693/14, 1693/14, 1693/14|discarded|24.69|
|array-allocation-access|native_direct_reads/direct|338.488|309.587, 338.488, 339.577|1693/14, 1693/14, 1693/14|discarded|24.70|
|tree-survival|helper_only/handles|46.317|46.845, 46.140, 46.317|165/0, 165/0, 165/0|discarded|3.26|
|tree-survival|helper_only/direct|56.477|49.625, 56.477, 56.681|165/0, 165/0, 165/0|discarded|24.55|
|tree-survival|native_direct_reads/direct|44.549|44.300, 45.347, 44.549|165/0, 165/0, 165/0|discarded|24.55|

All variants use a configured 64 KiB nursery and 8 MiB old capacity. Direct actually reserves TWO 8 MiB old semispaces plus tracing metadata, promotes all nursery survivors, and uses allocation-heavy exact-root snapshots. Stable handles have different survivor and growth policies. Thus this compares whole experimental collectors, not reference representation alone.

RSS is process high water including Go/compiler/runtime state; Go HeapAlloc is the post-run snapshot, not a dedicated guest-heap measure. Direct scan/copied/promoted-byte counters are unimplemented; zero values are not measured zero work. Invocation and checksum checks are included in samples. No native-fast-path or V8 parity conclusion follows.

## Memory audit (separate non-timed processes)

Timing-process ru_maxrss is INVALID for comparison: the launcher hashed a 48 MiB binary by reading it whole before spawning, causing an inherited 58.5 MiB high-water floor. Those values are retained only in raw evidence and excluded above. The separate three-call audit used a small launcher without this large allocation; its process RSS values follow. Future probe source additionally records /proc/self/status VmHWM/VmRSS and streams hashing.

| Case | Variant | audit peak RSS MiB | Go HeapAlloc MiB |
|---|---|---:|---:|
|struct-allocation-access|helper_only/handles|49.24|14.71|
|struct-allocation-access|helper_only/direct|35.37|24.49|
|struct-allocation-access|native_direct_reads/direct|35.39|24.49|
|array-allocation-access|helper_only/handles|41.07|10.95|
|array-allocation-access|helper_only/direct|35.87|24.48|
|array-allocation-access|native_direct_reads/direct|35.14|24.49|
|tree-survival|helper_only/handles|16.99|0.91|
|tree-survival|helper_only/direct|18.12|24.48|
|tree-survival|native_direct_reads/direct|18.27|24.48|
