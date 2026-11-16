# Paired-loop comparison preparation

Pinned qualified canonical source `7483b3e0`; candidate tag `wago_gc_compact_pair_loop`. Baseline one-receiver loop+refstruct; candidate paired-loop+refstruct. Shared checks OFF both. No trusted mode, compact8 or allocation elision.

All six exact artifacts, 2 MiB Eden and two 8 MiB old semispaces. Separate 128-warmup actual-GC audits; bounded three warmups/five timed calls/three rotating processes. Separate Compile+Close/code-size/allocation comparison. Require exact-artifact evidence for actual two-receiver copy-loop selection, not just checksum/initialization.

Then separate scalar-copy versus V8, original 100 warmups + nine batches of 32, three rotating processes, --no-liftoff. Target remains unmet. No builds until explicit release.
