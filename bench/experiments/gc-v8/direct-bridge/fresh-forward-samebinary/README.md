# Same-binary fresh-struct forwarding confirmation

Code `1e9dba8b`, qualification `a9750059`. Binary SHA-256 `a76400feceef5dbcfac8c175e8bc16f50648800b398b0aef4ca39270f3d2470f`. Explicit `GC_BRIDGE_FRESH_STRUCT_FORWARD=0/1`; the optimization defaults off. The inherited JSON field `cache` is only the mode label in this reused driver; the actual asserted receipt is `fresh_struct_forward_override`, not arena cache.

32/32 processes passed: eight adjacent reversed-order pairs for struct and byte-identical nonadmitted fill control. Each process uses 100 warmups, nine batches of 64 calls, GOMAXPROCS=1, CPU0 affinity, 2 MiB young/two8MiB old, age2/elision off. Same binary and source for both modes. Checksums/artifact hashes, exact allocation/collection counts and override/native receipts agree. Per-process UTC/load metadata retained. Own local jobs paused, not proof of exclusive host. This is a diagnostic, not a new V8 comparison; do not pool absolute times with other protocols.

## Process-level results

Eight process medians/pairs per case; internal batch samples are not independent experiments.

| Case | Off µs | On µs | On/off median ratio | Median paired ratio |
|---|---:|---:|---:|---:|
| struct-allocation-access | 5472.673 | 5489.526 | 1.0031× | 1.0038× |
| array-fill | 14262.091 | 14315.589 | 1.0038× | 1.0098× |

| Case | Round | Off µs | On µs | On/off |
|---|---:|---:|---:|---:|
| struct-allocation-access | 0 | 5712.134 | 5524.466 | 0.9671× |
| struct-allocation-access | 1 | 5359.334 | 5501.613 | 1.0265× |
| struct-allocation-access | 2 | 5827.885 | 5547.578 | 0.9519× |
| struct-allocation-access | 3 | 5376.220 | 5468.349 | 1.0171× |
| struct-allocation-access | 4 | 5487.285 | 5477.439 | 0.9982× |
| struct-allocation-access | 5 | 6015.928 | 5337.281 | 0.8872× |
| struct-allocation-access | 6 | 5458.061 | 5606.728 | 1.0272× |
| struct-allocation-access | 7 | 5181.574 | 5230.713 | 1.0095× |
| array-fill | 0 | 14918.978 | 15012.491 | 1.0063× |
| array-fill | 1 | 14273.433 | 14598.580 | 1.0228× |
| array-fill | 2 | 14390.916 | 14605.363 | 1.0149× |
| array-fill | 3 | 14250.749 | 14676.521 | 1.0299× |
| array-fill | 4 | 14969.007 | 14013.299 | 0.9362× |
| array-fill | 5 | 13849.115 | 14032.599 | 1.0132× |
| array-fill | 6 | 13802.546 | 13666.401 | 0.9901× |
| array-fill | 7 | 13870.712 | 13835.023 | 0.9974× |

## Receipts and verdict

Qualified off/on/off compiler receipts show struct forwarding0/1/0 and emitted size1775/1711/1775bytes: the candidate saves64codebytes while retaining allocation. The five nonadmitted artifacts remain byte-identical. The mechanism is active, unlike the old missed-admission path, but runtime throughput does not show a consistent gain here: struct median paired ratio1.0038×, with directions mixed; fill control1.0098×. One favorable struct round does not establish a gain. Preserve this negative/inconclusive result rather than promote based on code-size savings alone. No broad performance, GC throughput or V8-parity claim is supported.
