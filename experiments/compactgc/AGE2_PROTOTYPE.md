# Isolated compact age-two prototype

This is newly reconstructed source based on recovered `cc33d103`, not a claim
that lost experimental source or prior qualifications were recovered.

Build `wago_gc_compact_age2` and explicitly call
`NewAge2WithTypes(totalYoungBytes, oldBytes, survivorCapBytes, types)`.
Default constructors remain promote-all. Without the tag the new constructor
returns ErrConfig and the embedded policy state is empty. No native Collector
field, runtime configuration, compiler receipt or default policy changes here.

The TOTAL young budget is split into two equal spaces. Total must be at least
64 bytes and divisible by32; cap is16-aligned and at most total/4. This reserves
at least half the active space for fresh allocation after each minor. Arena,
mark/start tables and bounded tracing queue use the same total budget as the
promote-all comparator. Tagged collector state adds scalar policy metadata;
MemoryUsage includes that struct cost. No extra per-object/header/age metadata.

The occupied active range contains age-one survivors as a prefix, followed by
fresh allocations. A minor promotes live prefix objects, retains live fresh
objects up to the explicit cap, and promotes overflow. Preflight validates the
graph and BOTH destination capacities; commit replays the same queue order,
copies into disjoint regions, then rewrites roots and edges. Full promotes all
reachable objects into the alternate old space and empties young spaces.

Only previous dirty parents and newly promoted parents are reconsidered for
old-to-young edges. Queue overflow retains its conservative dirty-old scan.
Successful minors retain required dirty certificates rather than blindly
clearing them. Failed preflight leaves authoritative state unchanged.

Allocation accounting retires only fresh suffix starts, not survivor copies;
pending snapshots scan the fresh suffix. Objects larger than an ACTIVE half
are directly old allocated, so this threshold differs from equal-total
promote-all. Comparisons must report that traffic, collection frequency and
retention-cap pressure, not merely reduced promotion counts.

ReadView/AllocationWords reflect current active bounds with a stable cursor
address, but runtime use is NOT admitted by this core change. Future adapter
must explicitly distinguish policy in its receipt/ABI, refresh views after all
allocation/collection outcomes, and qualify native publication/dirty reuse.
Current promote-all cohort diagnostics are not valid age-two instrumentation.

Source regression fixtures cover graph cycles/duplicate roots/i31, promoted
parent to younger survivor, remembered overflow, retention overflow, atomic
old OOM/full retry, exact counts, oversized threshold and zero-allocation
collection. Tests and performance are NOT yet qualified at this checkpoint.

## Reconstructed core qualification

Go1.27 default core passed (0.034s), age-two-tagged full core passed (0.028s),
and tagged race passed (1.168s). These are correctness test durations, not
performance measurements. Zero-allocation collection assertions passed.
Measured amd64 collector footprint: default224B with empty policy state;
tagged248B with20B policy state plus alignment. Arena and existing tracing
scratch sizes remain equal for equal TOTAL young budget. Runtime/native
integration and end-to-end performance remain unqualified.
