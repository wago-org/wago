# Dedicated Eden prototype: source only

Opt in with `wago_gc_compact_eden` and `NewEdenWithTypes(eden, survivor, old, types)`.
Total young backing is Eden + TWO survivor capacities, not Eden + one. This is
Layout16 compactgc only; promote-all and existing age2 constructors are unchanged.
New allocations always use fixed Eden; only objects larger than Eden bypass to
old. First survivors fit the inactive survivor space in deterministic queue order;
overflow promotes, and every live prior survivor promotes. Full moves all live
objects to alternate old. Preflight reserves all destinations before any mutation.

Native ReadView and AllocationWords return zero for this policy: their current
single-young-range ABI cannot represent the new occupied survivor interval. No
runtime adapter/native compiler integration or TinyGo runtime support is claimed.
Core uses existing pure-Go storage/operations. Cohort/shadow, age2 diagnostics,
queue-reuse and compact8 combinations reject the new constructor explicitly.

New tests are source-stage, NOT executed or qualified yet. They cover distinct
regions, first/second survival, cycles, duplicate roots, old-to-young barriers,
retention overflow, Full across both young regions, partial OOM/invalid-reference
preflight repair and exact allocation accounting. Remembered overflow, zero-allocation assertions and Full failure with both young
source regions also have unexecuted tests. Random graph/property checks and
TinyGo core build remain additional qualification work. No performance or V8 comparison has been run. Full test and
fixed-total/equal-Eden benchmark gates are required before any conclusions.

## Core qualification checkpoint

Source6c3ed225 including deterministic randomized graph model passed Go1.27.1
normal/race/coexisting-age2 compact core and TinyGo0.42 task-scheduler core.
Default Wago, AMD64 backend, native GC, wrapper and direct core regressions pass;
incompatible constructor-tag rejection passes. Initial default-footprint fixture
and readonly-cache failures, fixes and successful reruns are preserved under
bench/experiments/gc-v8/direct-bridge/eden-core-qualification. No collector semantic
failure was observed. Above source-stage notes describe the earlier checkpoint;
all listed source tests now ran, including remembered overflow, minimumgeometry,
fullrollback, directoldthreshold, zeroallocation and randomized model checks.
This qualifies the isolated core, NOT a native runtime adapter or V8 performance.
