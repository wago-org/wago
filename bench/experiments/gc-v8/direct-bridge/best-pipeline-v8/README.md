# Best experimental native pipeline versus V8

Build `candidate.test` from this same source with `go test -p 1 -c -tags wago_gc_direct_fill,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc`. This combines checked native fill with policy 3 numeric-array, policy 4 reference-struct and policy 5 pointer-free allocation capabilities. It remains opt-in, not the default runtime.

Run the driver with `--binary-dir DIR --output-dir DIR --eden 524288 --audit`, then omit `--audit` for the primary run. Repeat using the SAME binary with `--eden 2097152` and a separate output directory for the explicitly labeled sensitivity run. Both old semispaces remain 8 MiB each. No default nursery change is implied.

All six original cases remain included. Each process uses 100 warmup calls and 9 batches of 32 checked timed calls; three rounds rotate workload and engine order. V8 uses `--no-liftoff`. Exact artifact hashes, checksums, expected Wago object allocations, actual collection counters, versions, binary hashes and resident memory are retained. Report every workload and target miss without aggregate substitution. A 1.15x per-workload threshold is only a corpus gate, not universal superiority.

This is an end-to-end engine comparison, not an isolated collector-efficiency proof. V8 internal allocation/elision and collection counts are unobserved; heap policies and host overhead differ. Compilation is excluded. Major counts must be reported, including zero if larger nursery avoids major collection even over the longer interval. Retain earlier pipeline evidence separately.
