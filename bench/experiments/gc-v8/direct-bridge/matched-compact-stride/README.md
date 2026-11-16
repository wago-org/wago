# Strength-reduced range comparison

Source 6237226a pending canonical qualification/release. Baseline range_loop+refstruct versus range_stride+refstruct, shared checks OFF. No cohort diagnostic, trusted mode or allocation elision.

All-six bounded comparison: separate128 warmup actual-GC audits, three warmups/five timed calls/three rotating processes. Explicit2 MiB Eden/two8 MiB old semispaces. Separate compile footprint and exact scalar target selection evidence. Full scalar-copy V8 gate follows with100 warmups/nine batches32/three processes; no all-six target claim from scalar only. No builds until explicit release.
