# Intermediate Stage 3 runtime correctness review

Reviewed exact production diff `a3c08365f531eb3c9003e3acf3e0a822c12c31f4..5b31c81ec0c74695a90bc8d36754b92845b59f14` on
`experiment/rfwgc-port`, in the existing worktree, 2026-11-01.
Read the resume brief, launch addendum, local AGENTS.md, stage3-mirror README,
architecture and correctness matrix, and stage3-runtime design/qualification.
Mirror commit: `69a6d9ec5b09a2e8fb6086763e1c112f0719411b`.

**No verified correctness findings in the reviewed diff.** No severity, defect
trigger, or fix recommendation is assigned to an unverified suspicion. This is
an intermediate independent review, not the mandated final four-reviewer fan,
and does not mark Stage 3 or the full Snap port complete.

## Source audit and independent probes

- `native/eden_regions.go:12-29`, `alloc.go:118-169`: allocation searches bounded
  region-relative bumps, routes oversized objects to LOS, and commits membership
  only after publication failure injection. The global nursery bump can differ
  from a region's retained bump after full compaction; the region path does not
  use that global bump to authorize allocation, so this does not permit overlap.
- `eden_regions.go:32-48`, `collect.go:28-34,109-114`: occupancy is reconstructed
  after sweeping/compacting the young handle ledger. Only Eden offsets count;
  survivor-space handles share spaceNursery but are excluded. A successful minor
  evacuates Eden before clearing region state; error returns precede that clear.
  Independent `TestStage3ReviewSurvivorAndEdenReuse` puts a persistent-rooted
  object in survivor space, retains a separate Eden object, then repeatedly
  reuses empty Eden with poisoning enabled. Both identities, offsets and payloads
  survive, followed by minor promotion and complete root-drop reclamation.
- `collect.go` promotion planning preserves existing destination/commit rollback.
  `TestStage3ReviewLatePromotionRollback` independently injects destination and
  commit failures, verifies region ledger and live entry preservation, verifies
  the heap and successfully retries. This supplements the existing publication
  and plan-failure tests. It exercises survivor destinations, not an exhaustive
  old-space allocation-failure matrix.
- `config.go:21-29`: invalid shifts are rejected before division/modulo can use
  a zero shifted value. `TestStage3ReviewConfigShiftDomain` checks every uint8
  shift with a 128-byte nursery: only zero, six and seven are accepted. Existing
  backing-size overflow checks still run after normalization. Explicit fixed
  nursery admission prevents the default adaptive-growth path from enabling.
- `native_alloc.go:68-71,250-254`, `native_view.go:320-323`: struct, dead-struct,
  chunk-array, immediate-array and large-array preparation all reach region-mode
  refusal. `dead_struct_batch.go`, `dead_array_batch.go`, and `dead_batch_try.go`
  share that guard; fallback charges materialize through the checked allocator.
  Reviewed AMD64 constructor/dead-group guards and ARM64 array/dead-struct guards:
  zero advertised object limit/capacity forces fallback. Native resolving/access
  still sees the whole nursery backing, including survivors.
- `native_view.go` ABI constants/validation and handle layout are unchanged.
  Config/Collector private sizes change intentionally; reviewed footprint tests
  and offset consumers, including generated-code consumers of NativeCollectorView.
  No changed raw Collector offset contract was found.
- Exact root admission remains in the checked wrapper's `prepareScratch`, before
  invoking the raw allocator. Raw full tracing visits supplied roots plus strong
  persistent slots and drains the graph; minor tracing uses remembered/card
  inputs. Region allocation does not substitute lifetime hints for tracing.
  Constructor initializer staging and existing caller-frame guest tests remain
  relevant. This is not comprehensive region-enabled host/domain qualification.

Paths above are relative to `src/core/runtime/gc/` except compiler consumers.
All three independent probes passed; none reproduced the suspected failure.

## Sibling read-only cross-check

Read Slipstream's pinned `506b08d3d3fa3c8e8b1ce9ac0cb4f30a2463cdc3:gc/native.goose`
using git show, specifically ng_admit (909), ng_collect_owned (1099), ng_allocate
(1208), and ng_region's root-region API. The local origin/master still resolves
to that pin. Admission reserves before movement and separates reservation worker
count from active dispatch count. The old-space fix is absent at this cursor;
no pending or parked type-cap patch is treated as landed. Wago's partitioned Eden
policy is an adaptation, not a literal copy of Slipstream's root-region API.

Read Starshine `e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1`'s
`docs/wiki/wasm-gc-core-boundary.md` through git show. Its type/codec/validator
and authoring layers do not constitute a native region allocator. No equivalent
runtime defect is asserted there. No sibling modifications, tests, fetch, or
performance measurements were made. With no verified finding, there is no
finding-specific triage to assign after this cross-check.

## Validation and provenance

GOMAXPROCS=4, GOFLAGS=-p=4; all commands use -count=1 and -timeout=5m:

- Entire `./src/core/runtime/gc/...` passed ordinary, wagodebug and
  wago_regalloccheck (`*-collector.txt`).
- `./src/core/runtime/gc/native -run 'TestStage3Review|TestEdenRegions' -v`
  passed in all three modes (`*-probes.txt`). The later failure probes are
  deliberately wagodebug-only, matching the failure-injection facility.
- Guest `./src/wago -run '^TestGC'` initial attempts failed to compile because
  another worker's uncommitted `gc_eden_region_integration_amd64_test.go` had an
  unused context import (`*-guest.txt`). This file is outside the reviewed diff
  and was not modified. `guest-overlay.json` excludes concurrent untracked tests
  without editing them; isolated reruns passed in all three modes (`*-guest-isolated.txt`).

The shared branch advanced to documentation-only toolchain commit d546a1af
while this review ran. A diff against 5b31c81ec confirmed no tracked source
changes at that point. Concurrent integration and measurement test files were
left untouched; no benchmarks were invoked. Go test's printed durations are
incidental runner output, not measurements or performance evidence.

Only this review directory and the two stage3_review_* test files are owned by
this review. Commit identity is supplied in the handoff, avoiding a self hash.
No production fix, branch switch, worktree creation, subdelegation, PR or push.
The pre-existing dirty corpus scripts, port log and spec submodules are preserved.

## Limitations and disposition

Proceed with the next correctness stages; there is no verified finding to fix
from this review. Keep native batches disabled until separately implemented and
qualified. This does not close mature/large region porting, source old-space
mirror, region-enabled host/shared-domain/EH/tail/borrowed-storage coverage,
conformance, cross-platform execution, race/sanitizer, or final review gates.
Linux amd64 execution only; ARM64 consumers were inspected, not executed.

This change adds only test and evidence files and cannot affect production
runtime or compiler performance. No timing/benchmark was taken. Independent
measurement under the global lease remains necessary for the reviewed production
change; this review provides no performance approval.
