# Matched retirement end-to-end experiment

Runtime pinned to qualified `d68b1f5`: adaptive retirement uses previous-cycle mean stride <=48 bytes.

All binaries share the best SSE2-fill pipeline: `wago_gc_direct_fill,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_trusted_loop,wago_gc_direct_copy,wago_gc_direct_fill_sse2`.

- `headers.test`: no retirement tag.
- `startmap.test`: add `wago_gc_retire_startmap`.
- `adaptive.test`: add `wago_gc_retire_adaptive`.

Run with explicit 2 MiB Eden primary and two 8 MiB old semispaces, using `--audit` first then the full run. Audits use 128 warmups, ensuring copying-kernel collection; full timed runs retain 100 warmups and 9 batches of 32 calls. Three fresh-process rounds rotate cases and variants. All six original cases remain, with exact hashes/checksums, expected allocation counts and actual collection checks. This isolates retirement traversal policy on identical compiler source; a fourth contemporaneous V8 variant uses --no-liftoff for an end-to-end per-case gate. V8 heap policy and internal allocation/elision are not equalized; collector-efficiency conclusions remain unsupported. Preserve every case, control and regression rather than reporting an aggregate. Previous-cycle adaptation can lag phase changes; macrobenchmarks complement, not replace, density/crossover microbenchmarks.
