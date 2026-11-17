# Bounded vector polls: unqualified experiment

Build tag: `wago_experimental_bounded_vector_polls`. The default build retains
its existing exact per-iteration policy. This tag deliberately changes safepoint
cadence, and must not be released as the existing cancellation contract.

Only independent-lane map loops are eligible. Scalar memory recurrences,
adjacent-output pairing, profiling, EH, GC roots, custom instructions and shared
memory remain excluded. The existing initial header check remains; guarded fast
execution checks after each complete packed group, including the final group.
Groups represent two or four original iterations. The checked scalar fallback
and remainder have their own check at the actual backedge target. Each poll records the original loop header internally; the existing grouped
interrupt trap stub reports function-only attribution (unknown PC), unchanged. A dedicated spill preserves its scratch
register, and the following decrement recreates flags before its conditional
backedge. Completed groups publish all guest stores before interruption.

This bounds completed guest work after trap-cell publication, not elapsed time.
It does not guarantee scheduler responsiveness or microsecond cancellation.
Existing TinyGo scheduler restrictions remain unchanged.

## Qualification status

Experimental focused oracle and injected-interruption tests passed before an
additional source-PC assertion exposed the existing function-only trap contract.
The assertion is corrected but awaits rerun; no qualification is claimed. Oracle
coverage is added for short/odd trips, alias fallback and exact/disjoint streams.
Still required before adoption:

- Extend deterministic same-width native poll injection beyond the first packed
  group to arbitrary later groups. Current tests cover entry/header, first/final
  group, scalar-tail entry and alias fallback; check completed-prefix memory and
  reuse of the same engine/job after interruption.
- Native branch audit showing every fast/fallback/remainder cycle polls.
- Negative admission tests for excluded shapes and feature/profile combinations.
- Threaded TinyGo cancellation and concurrent close; repeat invocation after trap.
- Full correctness suites and same-toolchain per-workload performance comparison.

Do not qualify this patch from eventual timeout tests alone. In particular,
ordinary memory-oracle tests do not prove interrupted-state correctness.
