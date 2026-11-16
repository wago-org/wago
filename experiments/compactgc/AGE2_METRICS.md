# Untimed age-two traffic metrics

Enable BOTH `wago_gc_compact_age2` and `wago_gc_compact_age2_metrics`, then use
the explicit age-two constructor. `Age2Metrics()` returns a value snapshot and
an enabled boolean; no pointers or callbacks are exposed. Disabled builds use
empty embedded state and constant-false instrumentation. No allocation-time
counter or collector policy decision changes.

Successful collection snapshots report fresh capacity/used bytes and object
starts, retained prefix before/after, all first-time survivors, age-one
promotion, retention-cap overflow promotion, full live young/old bytes,
remembered preflight parent/edge scans and overflow slot visits. FirstSurvivor
includes retained AND overflow-promoted fresh objects; it is not another
additive promotion total. Full live bytes are separate, not minor age claims.

Old-direct traffic is the contiguous old bump growth and existing old allocation
counter delta since the previous successful collection (or construction).
Failed attempts do not consume that interval. Promoted copies are excluded by
resetting the interval endpoint after commit. Allocations after the last
collection are not yet included. No per-allocation instrumentation is needed.

Failures increment only failure counters; Last and success totals remain intact.
Capacity, occupancy and remembered samples sum work per cycle, not distinct
objects. Remembered overflow slots count the preflight range walk, not all
passes; parents/edges describe preflight only. The extra fresh-start and live
queue scans alter execution costs: NEVER use this build for timing or V8
comparisons. Existing promote-all cohort/shadow tags remain incompatible.

Independent source review found no blocker. Go1.27 tests passed: default core
0.016s, age2-only core0.018s, metrics-tag core0.020s, metrics-tag race1.178s.
Regression cases cover overflow/age-one classification, old-direct intervals,
minor failure, full OOM/retry, and repeated snapshots. No performance
claim, default enablement, or scan optimization accompanies these counters.

Canonical composition at `bd4a4685` also passed fresh default core (0.025s),
age2+metrics core (0.019s), and tagged race (1.235s). No workload observation
or speed conclusion is implied by these tests. Timing binaries exclude this tag.
