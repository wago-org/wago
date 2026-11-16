# Untimed compact cohort workload probe

Source preparation only: awaiting qualified cohort diagnostic and value-only native adapter accessor. Never add `wago_gc_compact_cohort` to timing binaries. This diagnostic adds traversal work and provides no speed evidence.

Run exact struct, array, fill, tree and both copy artifacts with 128 warmup calls and five further calls. Assert every checksum, artifact hash, exact allocation totals, and actual successful collections. Record cumulative snapshots before warmup, after warmup and after five calls; preserve failures separately from successful totals. Explicit 2 MiB Eden and two 8 MiB old semispaces. Record source/compiler tags and geometry.

Report successful minor young allocated bytes/objects and live bytes/objects (all live young promote in this collector); full old occupancy versus traced live, young live, oversized old live, oversized allocations, and minor/full failure counters. Ratios are sums over collection events, not unique-object lifetimes. Do not infer object ages, survival across multiple minors, future survivor-policy gains or pause times. No forced collection should alter the natural workload cohort before snapshots. A no-major case must remain explicitly unobserved, not treated as zero old survival.

No builds until canonical test release and final source/API pin.
