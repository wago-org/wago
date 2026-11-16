# Checked compact scalar transfer range version

Experimental tag: `wago_gc_compact_range_loop`, composed with `wago_gc_compact_refstruct` for the complete tested pipeline. Baseline comparator: `wago_gc_compact_pair_loop`, with shared-checks disabled in both.

This stage recognizes only a bounded, canonical scalar array transfer: two unchanged final same-type array locals, a shared i32 index, one load/store pair, index increment by one, and unsigned comparison to a positive constant bound. It preserves the initial checked iteration at the original instruction PCs. On its true backedge, full-span predicates choose an ordered scalar steady loop or return to the original checked header. A short receiver therefore still traps after exactly the original partial writes. There is no allocation elimination, bulk/memmove substitution, trusted ingress shortcut, or new ISA requirement.

Selection requires a synchronized pinned integer index, no carried stack values or loop parameters/results, two certified receiver pointers, clean scratch registers, and a checked native body no larger than 1KiB. Matching is bounded to 128 source bytes. The experimental compiler state stores only index/bound (8 bytes); the default build uses an empty embedded state. The fast loop synchronizes the updated index's frame slot before joining the original exit.

Independent source review found no blocker. Qualification uses Go1.27.1 and serial `-p 1`:

- Backend `wago_gc_compact_range_loop wago_gc_compact_refstruct wago_regalloc_checks wago_codegenstats`, `TestCompact(Range|PairLoop|Loop)`: passed. Exact Wasm artifact selects the scalar copy kernel once and does not select the bulk kernel.
- Wago `wago_gc_compact_range_loop wago_gc_compact_refstruct wago_regalloc_checks`, `^(TestCompactGC|TestCompactNative|TestGCCompact)`: passed (0.608s).
- Fixtures cover final induction value, false first backedge, packed signed values, same-object aliases, repeated moving collections, and every prefix element after source/target bounds failures.

These are correctness results, not performance measurements. No speedup is claimed before matched benchmarking.
