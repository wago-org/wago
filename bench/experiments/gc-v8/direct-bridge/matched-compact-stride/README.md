# Strength-reduced range comparison

Source 6237226a qualified and released. Baseline range_loop+refstruct versus range_stride+refstruct, shared checks OFF. No cohort diagnostic, trusted mode or allocation elision.

All-six bounded comparison: separate 128 warmup actual-GC audits, three warmups/five timed calls/three rotating processes. Explicit 2 MiB Eden/two 8 MiB old semispaces. Separate compile footprint and exact scalar target selection evidence. Full scalar-copy V8 gate follows with 100 warmups/nine batches of 32/three processes; no all-six target claim from scalar only. No builds until explicit release.
