# Weighted reduction reconstruction

Reconstructed from retained implementation context after the cloud workspace was replaced. Unlike the preceding SSE reconstruction, no independent pre-loss weighted file-blob manifest is available. This is new source/provenance requiring fresh review and qualification. The lost weighted experiment had not completed source review or tests; none are claimed here.

The separate tag `wago_gc_compact_weighted_sum` recognizes only a bounded canonical i32 weighted reduction over one unchanged final i32 array, with distinct clean pinned induction and accumulator locals. It preserves the original checked first iteration and falls back to the original loop if the validated array length does not cover the full remaining span. Fast arithmetic remains modulo32, with both local homes synchronized at exit. No artifact names or specific bound/weight constants are recognized.
