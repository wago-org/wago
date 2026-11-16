# Matched direct GC queue/allocation experiment

Three alternating fresh processes per case/variant; 3 warmup calls then 5 checked calls. Go1.27.1, GOMAXPROCS=1, exact artifact digests and binary/source hashes in raw JSON. All variants use direct offsets,64KiB Eden,8MiB per old semispace (two old semispaces). Allocation elision disabled.

| Workload | Variant | median ms | three samples ms | timed minor/full | proc VmHWM median MiB | Go heap median MiB |
|---|---|---:|---|---|---:|---:|
|struct-allocation-access|reads_before_queue|264.128|308.730, 261.297, 264.128|742/5, 742/5, 742/5|36.63|24.58|
|struct-allocation-access|reads_after_queue|140.169|140.976, 140.169, 129.312|742/5, 742/5, 742/5|36.18|24.58|
|struct-allocation-access|alloc_after_queue|72.281|72.146, 75.513, 72.281|742/5, 742/5, 742/5|36.36|24.58|
|array-allocation-access|reads_before_queue|339.266|339.266, 342.898, 320.317|1693/14, 1693/14, 1693/14|36.08|24.70|
|array-allocation-access|reads_after_queue|202.521|214.915, 202.521, 188.675|1693/14, 1693/14, 1693/14|36.84|24.70|
|array-allocation-access|alloc_after_queue|200.485|225.897, 200.485, 191.407|1693/14, 1693/14, 1693/14|36.42|24.70|
|tree-survival|reads_before_queue|47.694|49.171, 45.722, 47.694|165/0, 165/0, 165/0|19.92|24.55|
|tree-survival|reads_after_queue|38.761|38.761, 34.788, 44.471|165/0, 165/0, 165/0|17.97|24.56|
|tree-survival|alloc_after_queue|38.492|38.492, 34.482, 40.135|165/0, 165/0, 165/0|17.97|24.56|

## Exploratory deltas

- struct-allocation-access: queue -46.9%; native allocation after queue -48.4% (negative means lower elapsed time).
- array-allocation-access: queue -40.3%; native allocation after queue -1.0% (negative means lower elapsed time).
- tree-survival: queue -18.7%; native allocation after queue -0.7% (negative means lower elapsed time).

This is a short exploratory collector/codegen comparison, not a V8 target test. Queue comparison uses the same native-read compiler; allocator comparison holds queue implementation fixed. Exact allocation count assertions cover every timed invocation. Struct is the scalar allocation treatment; array/tree are controls where scalar-struct allocation may not apply. Do not infer significance from three samples alone.

Linux /proc VmHWM is used instead of inherited ru_maxrss; hashing streams small chunks. Process memory includes Go/compiler/runtime overhead, not just guest storage. Direct scan/copied/promoted-byte stats remain uninstrumented.
