# Integrated adaptive retirement comparison

Production code source93c7ec34. Control binary reused from full gate4d524db8; adaptive built from4327a2eb (only documentation/evidence changes versus93c7ec34) with identical compiler tags plus `wago_gc_retire_adaptive`. No sparse mark change or constructor change. Byte-identical production source confirmed; test-only marks benchmark file in adaptive build worktree is in a different package and not linked into this Wago binary. Exact tags/hashes/platform in evidence. Go1.27.1, Linux x86-64 Xeon8573C, GOMAXPROCS1.

Audit4/4 and timed12/12 process receipts passed, none missing. Checksums/artifact hashes/native direct-view receipts verified. Per-case allocation/minor/full-collection counts match across both variants and all rounds. Age2 and allocation elision OFF;2MiB young/two8MiB old. Audit128warmups; timing100warmups then9×32calls in3rotating case/variant rounds, serialized during reserved CPU window.

| Case | Headers median µs | Adaptive median µs | Adaptive/headers | Header round medians µs | Adaptive round medians µs |
|---|---:|---:|---:|---|---|
| struct-allocation-access | 5887.106 | 4895.215 | 0.832× | [5730.90621875, 5425.4958125, 6199.53825] | [5198.7616875, 4574.178125, 5223.32715625] |
| tree-survival | 2104.017 | 1726.930 | 0.821× | [2271.18, 2082.44484375, 2023.53609375] | [1688.29953125, 1678.43171875, 1893.02946875] |

Both measured cases improve in this window. This is a staged two-case comparison, not the six-case/V8 gate, not isolated collector efficiency, and not proof of universal improvement. There is no contemporaneous V8 receipt; do not combine historical V8 times to claim parity. Adaptive previous-cycle density can lag phase changes. Next: all-six-case contemporaneous V8/compile comparison before global promotion, keeping constructor changes separate.
