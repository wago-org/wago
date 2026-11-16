# Opt-in parent-array resolution cache

Build tag `wago_gc_parent_cache` enables a single-entry, per-instance exact parent-array resolver cache in the reference-array store leaf. Ordinary allocations, object lifetime, bounds checks and write barriers remain. Native ABI 10 adds 40 bytes to the instance view. Cache entries are not roots; the collector lifetime generation invalidates pointers, with saturation permanently disabling hits.

Do not enable by default from these results. Three alternating fresh-process rounds show a workload tradeoff:

- wasm.fyi struct allocation/access with stable parent: base 4.094/4.302/4.037 ms, cache 3.698/3.960/3.833 ms (5–10% better).
- Adversarial two-parent control: base 5.778/5.722/5.648 ms, cache 6.014/6.210/6.370 ms (4–13% worse). The supplied WAT extends the same workload to two roots and stores every new child into both, deliberately causing cache misses.

Baseline runtime revision 01c81f5; candidate code-equivalent to 8116178 plus its ABI ancestors (documentation-only changes do not affect measured binary). Go 1.27.1, AMD64 Linux cloud, default collector config, `GC_PROOFS=off`, 100 warmups, nine batches of 32 invocations per process, `performance(4096)`, checksum 536739840. Raw JSON includes each batch mean in nanoseconds.

Compilation comparison uses the same candidate tree with versus without the build tag, 64 warmups then 512 compile/close operations. Struct module native code grows 3956 to 4084 bytes; allocations remain approximately 253/op and allocated bytes approximately 66.2 KB/op. Compilation latency varies 128–140 microseconds/op; no reliable latency change claimed.

Validation: AMD64 compiler and Wago GC/Gc suites passed with official spec-v3 corpus available; focused cache and old-array barrier tests also pass `wago_regalloccheck`. Tests cover warmed stores, promotion, minor collection, saturated-generation poisoned-cache rejection and warmed out-of-bounds traps. Separate lifecycle audit is required before treating the generation contract as complete.
