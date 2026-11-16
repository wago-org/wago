# Matched ABI3 reference-array store experiment

Source c08836aff24e0c3feb67edd40c237ae71a622c55. Same compiler/collector source and explicit64KiB Eden /8MiB per old semispace; only native allocator versus native refstore tag changes. Allocation elision off. Three fresh processes each,3warmup+5checked timed calls.

| Workload | Variant | median ms | samples ms | timed minor/full | proc VmHWM MiB | Go heap MiB |
|---|---|---:|---|---|---:|---:|
|struct-allocation-access|native_alloc|75.655|75.655, 74.026, 78.423|742/5, 742/5, 742/5|36.37|24.58|
|struct-allocation-access|native_refstore|22.871|23.212, 22.871, 22.805|742/5, 742/5, 742/5|36.29|24.58|
|array-allocation-access|native_alloc|251.460|265.423, 190.761, 251.460|1693/14, 1693/14, 1693/14|36.78|24.70|
|array-allocation-access|native_refstore|183.245|182.918, 285.924, 183.245|1693/14, 1693/14, 1693/14|36.34|24.70|
|tree-survival|native_alloc|38.677|60.211, 38.677, 36.648|165/0, 165/0, 165/0|18.01|24.56|
|tree-survival|native_refstore|37.291|36.841, 56.582, 37.291|165/0, 165/0, 165/0|17.91|24.56|

Exploratory native-store versus native-allocation median deltas:
- struct-allocation-access: -69.8% elapsed time.
- array-allocation-access: -27.1% elapsed time.
- tree-survival: -3.6% elapsed time.

Noise warning: array and tree samples show large opposing outliers; the array median delta is inconclusive and the tree control supplies no reliable speed claim. Struct samples were consistently lower in all three launches.

All successful launches require exact artifact SHA256, numeric checksums, expected per-call allocation counts, and actual collection. Full stdout/stderr, configurations, counters, binary hashes and /proc memory data are retained in rawJSON.

This short isolated experimental comparison does not establish V8 parity or a final runtime performance guarantee. Heap policies are fixed; any nursery-size experiment must be reported separately. Direct copied/promoted-byte and scan counters remain uninstrumented. Process memory includes Go and runtime overhead.

## Go allocations per timed invocation

| Workload | Variant | median mallocs/call | median allocated bytes/call |
|---|---|---:|---:|
|struct-allocation-access|native_alloc|902.6|19254.4|
|struct-allocation-access|native_refstore|902.6|19254.4|
|array-allocation-access|native_alloc|2065.4|44059.2|
|array-allocation-access|native_refstore|2065.4|44059.2|
|tree-survival|native_alloc|360.2|14576.0|
|tree-survival|native_refstore|360.2|14576.0|

These counters are consistent with several host scratch allocations per collection, but allocation bytes are modest relative to the live Go heap. They do not alone prove material host-GC pressure or quantify time attributable to scratch management; profile before prioritizing reuse.
