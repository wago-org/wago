# Matched checked compact lazy loop diagnostic

Final qualified source must be pinned before building. Same-source baseline tags `wago_gc_compact_copy,wago_gc_compact_refstruct`; candidate `wago_gc_compact_loop,wago_gc_compact_refstruct`. No trusted check removal or allocation elision. All six exact artifacts, primary 2 MiB Eden and two 8 MiB old semispaces.

Separate 128-warmup real-GC audits precede bounded three-warmup/five-call diagnostics, three rotating fresh-process rounds. Exact checksums/counts remain, with explicit zero-GC allowance only in bounded copy measurements. Separate Compile+Close/code-size metrics and loop selection telemetry (`gc-compact-loop-plan`, `resolve`, `reuse`) accompany results. TestCompactLoopLowering under codegenstats and TestCompactNativeGCLoop tests establish selection and trap order separately; telemetry must not contaminate timed binaries.

After correctness and this diagnostic, run full scalar-copy versus V8 using the original 100 warmups/nine batches of 32/three rounds; report separately, not as an all-six target pass. Existing full gate remains failure until a proper replacement gate is measured. Preserve all control regressions and selection caveats.
