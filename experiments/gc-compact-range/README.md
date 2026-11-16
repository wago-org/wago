# Checked compact scalar transfer range version

Experimental tag: `wago_gc_compact_range_loop`, composed with `wago_gc_compact_refstruct` for the complete tested pipeline. Baseline comparator: `wago_gc_compact_pair_loop`, with shared-checks disabled in both.

This stage recognizes only a bounded, canonical scalar array transfer: two unchanged final same-type array locals, a shared i32 index, one load/store pair, index increment by one, and unsigned comparison to a positive constant bound. It preserves the initial checked iteration at the original instruction PCs. On its true backedge, full-span predicates choose an ordered scalar steady loop or return to the original checked header. A short receiver therefore still traps after exactly the original partial writes. There is no allocation elimination, bulk/memmove substitution, trusted ingress shortcut, or new ISA requirement.

Selection requires a synchronized pinned integer index, no carried stack values or loop parameters/results, two certified receiver pointers, clean scratch registers, and a checked native body no larger than 1KiB. Matching is bounded to 128 source bytes. The experimental compiler state stores only index/bound (8 bytes); the default build uses an empty embedded state. The fast loop synchronizes the updated index's frame slot before joining the original exit.

Independent source review found no blocker. Qualification uses Go1.27.1 and serial `-p 1`:

- Backend `wago_gc_compact_range_loop wago_gc_compact_refstruct wago_regalloc_checks wago_codegenstats`, `TestCompact(Range|PairLoop|Loop)`: passed. Exact Wasm artifact selects the scalar copy kernel once and does not select the bulk kernel.
- Wago `wago_gc_compact_range_loop wago_gc_compact_refstruct wago_regalloc_checks`, `^(TestCompactGC|TestCompactNative|TestGCCompact)`: passed (0.608s).
- Fixtures cover final induction value, false first backedge, packed signed values, same-object aliases, repeated moving collections, and every prefix element after source/target bounds failures.

These are correctness results, not performance measurements. No speedup is claimed before matched benchmarking.

## Byte-offset strength-reduction comparator

Separate tag `wago_gc_compact_range_stride` retains all preceding admission, first-iteration checks, span guards, and scalar order. It computes the widened byte offset once, then advances by element width instead of recalculating index*width each iteration. It restores the final induction value to the proven constant bound before synchronizing the frame slot. No register/state or SIMD capability is added. If the widened endpoint exceeds positive signed imm32 range, the previous scalar loop is emitted unchanged.

Actual source review found no blocker. Initial qualification caught a compile-time `MovImm32` parameter type mismatch, corrected with an explicit signed conversion after the existing positive-bound proof. The compile-only large-end fixture verifies fallback at byte endpoint `0x80000000`; ordinary exact-artifact selection verifies strength reduction. Runtime fixtures now include 8-byte scalar elements in addition to 1/2/4-byte elements. Timing comparison remains separate from correctness qualification.

Final serial qualification (`-p 1`): backend selection/matcher/large-end fallback/loop-pressure tests with regalloc and codegenstats passed0.005s; applicable Wago compact matrix with `wago_gc_compact_range_stride wago_gc_compact_refstruct wago_regalloc_checks` passed0.692s. No runtime failures occurred.
