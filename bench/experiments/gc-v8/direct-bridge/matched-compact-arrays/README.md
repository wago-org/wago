# Bounded compact numeric-array diagnostic

Pin final qualified canonical source before any build. Three same-source tags isolate checked numeric-array access then allocation: `wago_gc_compact_refstore`, `wago_gc_compact_scalar_arrays`, `wago_gc_compact_array_alloc` (binaries refstore/scalararrays/arrayalloc.test).

All six exact artifacts remain, with explicit 2 MiB Eden and two 8 MiB old semispaces. Separate 128-warmup audits require actual collection; bounded timings use three warmups plus five calls, three rotating fresh-process rounds. Copy diagnostics may have zero collection only after their separate audit. Preserve exact object counts/checksums/hashes, memory, collection counts, all controls and unknown-mechanism regressions. Compile+Close/code-size selection measured separately after timing. No full-protocol V8 or universal performance claim; remaining fill/copy/reference-struct native paths are not assumed complete.
