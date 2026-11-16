# Matched checked compact shared-check diagnostic

Runtime source 50c3065d, pending canonical qualification. Same-source baseline tags `wago_gc_compact_loop,wago_gc_compact_refstruct`; candidate adds `wago_gc_compact_shared_checks`. No trusted mode, compact8 or allocation elision.

All six exact artifacts, explicit 2 MiB Eden and two 8 MiB old semispaces. Separate 128-warmup actual-GC audits, then three warmups and five timed calls in three rotating fresh processes. Keep checksums, exact allocation counts, collection counters, Go heap and `/proc` RSS. Zero collection is allowed only in bounded measurements, not audits. Separate Compile+Close/code-size/allocation comparison follows timings. Code-size equality is not byte identity and changes do not prove exact selected loop sites. Exact-artifact telemetry is separate from timed binaries.

No V8 rerun is planned for this small slice unless a substantial gain warrants one. The full target remains unmet. Do not build or time before explicit quiet-window release.
