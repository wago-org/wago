# Guarded range-versioning protocol preparation

Pinned qualified canonical `d0c35039`, range tag `wago_gc_compact_range_loop`. Baseline paired+refstruct versus guarded range+refstruct, shared_checks OFF both. No trusted mode or allocation elision.

All six exact artifacts: explicit 2 MiB Eden, two 8 MiB old semispaces, separate 128-warmup actual-GC audits; bounded three warmups/five timed calls/three rotating processes. Preserve checksums, exact allocation counts, controls and outliers. Separate Compile+Close/code-size measurement. Require exact target-site selection proof, not inferred loop attribution.

Then full scalar-copy versus V8, 100 warmups + nine batches of 32, three rotating processes, --no-liftoff. Scalar-only success cannot establish all-six target. No builds until explicit release.
