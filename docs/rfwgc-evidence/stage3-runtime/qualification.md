# Stage 3 runtime implementation handoff

This is a bounded production slice, **not completion of the full Snap port**.
Source is based on Wago a3c08365f; concurrent documentation-only mirror commit
69a6d9ec5b09a2e8fb6086763e1c112f0719411b was read before handoff.
Its README, architecture and correctness-matrix requirements are incorporated
below. No parked Wago implementation was read/imported. No PR/push/delegation.

## Implemented behavior

`WithGC(GCConfig{NurseryBytes: 65536, NurseryRegionShift: 12})` selects 16
4096-byte Eden regions. Shift zero is the explicit unchanged-policy baseline.
The option also reaches the checked collector through its existing Config alias.
Only fixed-size Throughput Eden is admitted; invalid/Tiny/collection-disabled,
stress-size override, and pause-target combinations return configuration errors.
No silently enabled fallback is counted as a region hit.

Real struct and array payloads occupy disjoint bounded intervals of existing
aligned Go-owned Eden; a live object cannot span regions. Oversized objects use
the existing nonmoving large space. Exact full tracing/sweep on Eden exhaustion
preserves live payload locations, invalidates dead handles and resets only empty
regions. Mixed-region bump ownership is retained even if its tail object died.
Allocation retries those regions before ordinary transactional minor evacuation.
Successful evacuation resets Eden metadata; failed evacuation does not. No new
relocation path or partial-commit path is introduced. Handle-publication fault
injection restores the bump before region membership is committed.

The allocator uses a bounded circular region search (at most 65536 entries).
Region membership is reconstructed from the existing dense young-handle ledger
at full collection; per-region metadata has no independent object pointers.
This first policy still performs exact per-object tracing and handle sweeping;
it does not promise bulk handle invalidation or constant-time region collection.
The verifier checks each live Eden extent against its region's used extent.

All contiguous native allocation reservations and handleless batches reject the
new mode; the NativeCollectorView advertises zero native allocation capacity.
Checked helpers still allocate real payloads and native generated code still
resolves/accesses them. Exact frames, initializer staging, root/domain admission,
old/large cards, survivor ages, promotion destination transactions and checked
handle generations use existing paths. Native view/handle ABI and version 9
remain unchanged. Private Config grows 64 to 68 bytes; on amd64 Collector grows
40 bytes (aligned config plus ledger/cursor). Two existing footprint/offset test
assertions were updated for this intentional private layout change; their ABI
checks were not disabled. Off-mode branch/layout overhead needs measurement.

## Mirror and sibling disposition

Independent launch, implementation-boundary and completion fetches all pin
Slipstream origin/master 506b08d3d3fa3c8e8b1ce9ac0cb4f30a2463cdc3. Source reads
were git show origin/master. Old-space reservation is still NOT landed; parked
type-cap 01303964b is excluded. Wake/join active helper reduction is separate
from admission reservation count: the landed ng_collect_owned retains its prior
reservation policy for ng_admit/ng_evacuation. This slice changes neither Wago
promotion reservation nor worker participation and adds no helper pool.

Read-only Starshine cross-check at e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1 used
wasm-gc-core-boundary and heap2local strategy documentation: compiler elimination
and model support are not native runtime-region evidence. Slipstream ng_allocate,
ng_collect_owned, ng_region, ng_admit and dispatch/join were inspected. No new
pre-existing engine defect is asserted or triaged. These are design cross-checks,
not sibling or Wago performance results.

## Tests and exact evidence scope

Linux x86-64, Go go1.27.1. `results.json` and six suite logs record exit zero for:

- Entire `./src/core/runtime/gc/...` suites in ordinary, wagodebug and
  wago_regalloccheck modes. This includes the mirror's five contract tests on
  the **new production implementation**, not just its original snapshot.
- `./src/wago -run '^TestGC'` in all three modes, including existing GC native,
  frame, lifetime, allocation and collection families selected by that prefix.

`region-results.json` and three verbose region logs cover the final expanded
new test set in all three modes, with no skipped new test. Final additions after
broad suite runs are test-only. The new scenarios are:

- 20000 reuse cycles (40000 transient allocations) with a fixed live object in
  the adjacent region, exact payload and handle-entry preservation, zero minor
  collections/copy bytes, at most five handle entries and exactly two regions.
- Cross-region reference closure, full-to-minor fallback, stable references after
  movement, old-to-young store/card survival and complete dead-graph reclamation.
- Dynamic reference arrays of lengths 2/5/32, constructor initializer rooting at
  exhaustion, self cycles, duplicate child edges, four full/minor pairs, exact
  two-object reachable oracle, and zero objects after dropping the root. Both
  explicit baseline and enabled region configurations execute identical checks.
- Large-object admission, missing-root exhaustion without counter mutation,
  rejected configuration combinations, and native batch/fast-view rejection.
- Poisoned storage, immediate promotion, CollectEveryAlloc and
  ForceMajorEveryMinor policies with preserved numeric payloads.
- wagodebug failures before handle publication and promotion planning, preserving
  region state/live entries and succeeding on retry.
- Actual AMD64 guest execution with a live caller local across 32 allocating
  callee calls, repeated 32 times. Both lifetime lowering On/Off and region
  On/Off return the independent expected scalar 7. Enabled runs assert actual
  full collections and at least 1024 allocation charges.

Initial development checks caught old private-layout assertions and test API
mismatches; these were corrected, not waived. Final recorded suites are green.
No benchmark flags or intentional timings were used; Go's standard test duration
fields are incidental correctness-run output. No performance claim is made.

## Matrix disposition and remaining gates

H/A/G/B/S have exercised slice coverage above, not universal closure. A's region
metadata is allocated once at construction with ordinary Go allocation; metadata
OOM is not an injected recoverable transaction in this slice. Existing promotion
and card failure suites run, but region-specific testing currently injects only
publication and plan failure. R's 20000-cycle fixture proves bounded Eden reuse;
it does not qualify old-space promotion churn beyond the source failure horizon.
I covers dynamic-array initializer staging. F covers actual caller frame roots,
not the complete SIMD/operand-stack/spill and exception/tail matrix. N explicitly
uses checked allocation fallback; region-aware native reservation is unimplemented.

Required subsequent full-port work: region-aware native reservation/refill and
cancellation (R3); mature/large-region occupancy/reclamation and transactional
old-space admission, including review of the eventual landed Slipstream fix;
region-enabled host reentry, shared-domain aliases/close/token lifetime, traps,
EH/tails and borrowed storage gates (R4); canonical metadata growth and cached
artifact/platform qualification in region mode; broader independent graph corpus
and full conformance/toolchain closure. Existing default-mode coverage does not
close those region-enabled rows. Optional parallel work must preserve separate
reservation/active counts and ticketed drain/poison invariants; if required for
full parity it remains an open R5 gate, not implicitly waived. Compiler-directed
frame lifetime ownership is a separate future contract, not required to call
these checked escaping objects region-allocated.

Full root `./...`, nested modules, full external conformance, other architectures,
race/sanitizer and the owner's four-reviewer gate were not run by this worker.
The parent/toolchain worker owns their reconciliation. No claim that the full
conformance gap is closed is made here.

## Independent measurement handoff

READY TO MEASURE on experiment/rfwgc-port; exact commit supplied in handoff.
Performance-sensitive paths: native/alloc.go, eden_regions.go, collect.go,
collector.go/config.go, native_alloc.go/native_view.go, verify.go and the private
collector layout. Native allocation refusal is deliberately a potential cost.

Compare baseline a3c08365f versus implemented head and same-head region Off/On,
under the global lease with the required independent measurer/temperature gates.
Matched runtime cases: no-GC controls; stage-2 qualifying/nonqualifying scalar
cases; real escaping structs and small/dynamic/cyclic reference arrays; low and
high survival; fragmented live/dead Eden; promotion and large-array churn;
caller-root pressure; then host/shared-domain workloads after their correctness
gates. Match compiler options and record real charges, live handles, region used
extents, survivor/old/large bytes, copied/promoted bytes and retained backing
separately. Compare compile costs on the same no-GC/scalar/graph module bytes;
region selection is an instantiation policy, so do not attribute it to compiler
proof coverage. Tiny/disabled-collection remain explicit baseline controls, not
region-enabled cells. No measurement was performed by this fix worker.
