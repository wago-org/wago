# Untimed promotion diagnostic

Build with `wago_gc_compact_cohort` only for an untimed replay. Default builds
embed an empty state and constant-false hooks. The diagnostic build adds snapshot
state, a nursery start-map count, and a validated-live-queue header pass per
collection. Do not compare its wall time with production or V8.

`CohortDiagnostics()` returns value snapshots, never callbacks or heap pointers.
Successful minor young-live bytes/objects equal bytes/objects promoted. Full
records old occupancy and exact live old/young bytes. Oversized successful direct
old allocations are counted separately; full identifies oversized survivors by
stride greater than immutable nursery capacity. Failed attempts increment only
failure counters, not successful totals or Last. Totals of occupancy are samples,
not unique byte counts. Minor old-live counters are zero, not a reachability claim.

This first phase does not establish object age or premature promotion. Full dead
old bytes include all earlier cohorts. Adapter-trigger attribution (explicit,
forced, capacity retry) and public reporting plumbing remain separate work.
Before an age-two policy, use an optional untimed whole-graph shadow trace before
each minor to measure reachability of the immediately preceding promotion range;
skip intervals crossed by a full. Do not use remembered conservative roots for
that survival measurement. Shadow tracing is NOT implemented here.

No survivor policy, heap/roots mutation, extra arena, timing claim, or default
policy change. Source and tests prepared; qualification pending an exclusive CPU
window. Core test covers committed minor/full samples, oversized objects, failed
preflight snapshot preservation, and repeated nonmutating observations.
