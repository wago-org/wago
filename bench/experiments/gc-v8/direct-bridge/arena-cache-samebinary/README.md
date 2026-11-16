# Same-binary arena-cache confirmation

Source `85d533a1`, qualification `bb9d8733`. ONE existing binary, SHA-256 `aab5ad744e54c31a3381991b20dab8803dd049fdb88b46acc5ec7196253ba82b`, with explicit `GC_BRIDGE_ARENA_CACHE=0` or `1`. Tag default is OFF; no inference from build tags alone. Compiled code admission/byte receipts verified separately under the same options.

32/32 runtime processes passed; all checksum/artifact/config/native-view and explicit override receipts verified. Allocation/minor/full counts match across all processes for each case. Eight adjacent off/on pairs per case, reversing order each round; case order also alternates. Each fresh process performs100warmups followed by9batches of64calls. GOMAXPROCS1, tasksetCPU0;2MiB young/two8MiB old, age2/elisionoff. All local jobs paused during timing; this is not a proven-exclusive host. Per-process UTC/load/affinity retained. This affinity-controlled diagnostic has no V8 comparator and must not be pooled with the primary unpinned9×32gate.

## Process-level results

Use eight independent paired processes, not72internal batch samples as independent observations. Values below are medians of process medians.

| Case | Off µs | On µs | On/off ratio of medians | Median paired ratio |
|---|---:|---:|---:|---:|
| struct-allocation-access | 6477.988 | 6514.332 | 1.0056× | 1.0084× |
| array-fill | 17064.962 | 16978.308 | 0.9949× | 1.0066× |

| Case | Round | Order | Off µs | On µs | On/off |
|---|---:|---|---:|---:|---:|
| struct-allocation-access | 0 | off/on | 6929.094 | 6905.748 | 0.9966× |
| struct-allocation-access | 1 | on/off | 6358.468 | 6729.223 | 1.0583× |
| struct-allocation-access | 2 | off/on | 6482.240 | 6661.923 | 1.0277× |
| struct-allocation-access | 3 | on/off | 6428.630 | 6562.461 | 1.0208× |
| struct-allocation-access | 4 | off/on | 6601.452 | 6201.853 | 0.9395× |
| struct-allocation-access | 5 | on/off | 6572.310 | 6466.203 | 0.9839× |
| struct-allocation-access | 6 | off/on | 6473.736 | 6253.961 | 0.9661× |
| struct-allocation-access | 7 | on/off | 6334.158 | 6462.313 | 1.0202× |
| array-fill | 0 | off/on | 18249.651 | 17098.510 | 0.9369× |
| array-fill | 1 | on/off | 16745.791 | 17459.663 | 1.0426× |
| array-fill | 2 | off/on | 17792.231 | 18517.041 | 1.0407× |
| array-fill | 3 | on/off | 17384.133 | 17317.088 | 0.9961× |
| array-fill | 4 | off/on | 16487.377 | 16768.122 | 1.0170× |
| array-fill | 5 | on/off | 16425.072 | 16354.569 | 0.9957× |
| array-fill | 6 | off/on | 17912.143 | 16858.106 | 0.9412× |
| array-fill | 7 | on/off | 16049.388 | 16727.555 | 1.0423× |

## Actual compiler receipts and forwarding correction

The instrumented receipt-only build completed BEFORE runtime timing; its test-only patch adds a log of `gc-fresh-struct-get-forward` without changing production compiler behavior. Actual struct artifact off/on/off compiles report cache admission0/1/0; emitted bytes1775/1839/1775, with matching off hashes. Other five artifacts remain nonadmitted and byte-identical across modes. Exact logs retained.

Crucially, **`gc-fresh-struct-get-forward` is zero for every function in the actual struct artifact off/on/off compilation**. Existing generic forwarding implementation/tests do not establish activation for this benchmark. The earlier source-pattern inference that this opportunity was already covered on the artifact was too strong. Investigate where the fact is lost under this exact compilation configuration before proposing a new optimization; preserve allocations and store traps if extending it. The receipt does not itself identify the reason.

## Verdict

No demonstrated arena-cache gain: struct on/off is1.0056× by process medians and1.0084× by paired ratios, with pair directions mixed. The byte-identical nonadmitted fill control also varies around parity. This controlled same-binary result does not substantiate the earlier different-binary apparent7.6%gain. Keep cache off/experimental; do not promote. No new V8 or overall-GC-performance conclusion follows from this two-case diagnostic.

Reproduction artifact paths are relative to an experiment workspace root; local execution prefixes are omitted. Runtime measurements and binary hashes are unchanged.
