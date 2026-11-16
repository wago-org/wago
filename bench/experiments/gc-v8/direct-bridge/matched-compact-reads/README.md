# Bounded compact native-read diagnostic

Pin final qualified source before building. Same source: `helpers.test` with `wago_gc_compact_helpers`, `reads.test` with `wago_gc_compact_reads`. Both select compact runtime configuration. Allocation, stores, fill and copy remain helper paths; this is not the final native pipeline.

All six exact artifacts remain. Explicit 2 MiB Eden and two 8 MiB old semispaces. Run separate `--audit` first: 128 warmup calls, no timed calls, real collection required. Then run the clearly separate bounded diagnostic: three warmup calls plus one five-call timed batch, three rotating fresh-process rounds. Copy workloads need not collect during the bounded diagnostic; a recorded opt-in permits that only after the separate collection audit. Exact checksums and object-allocation counts still apply. Report actual zero/nonzero collections transparently, not as an equivalent collection schedule.

No V8 comparison or target acceptance is inferred. The full 100 + 9x32 protocol is reserved for a sufficiently native later pipeline. Preserve geometry, Go heap/RSS and all control cases; do not equate a read-path gain with overall collector superiority.
