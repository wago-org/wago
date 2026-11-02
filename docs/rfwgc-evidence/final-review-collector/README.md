# Final collector-family review

## Identity and scope

- Production source pin: `5345bd365c13963f1bf60e26bb80789ab9ead4cc`.
- Evidence head: `2c9f72744dd6f414fc8f236c2d45aa39ce6ced69`.
- Campaign base: `9713878ab452848f7eb7549a90be94b938a898be` (resolved from `9713878ab`).
- Review source: ref-qualified `git show`, `git diff`, and a `git archive` export of the production pin. The dirty worktree was not used as production evidence.
- Independent slice: `src/core/runtime/gc/native` collector families and their Wago integration edges, including region/young/mature/LOS ownership, bounded run reservation, old-to-old evacuation, private metadata rollback/publication, pressure/debt, parallel admission/join/wakeup/reclaim, callback guards, TinyGo serial paths, and PR1019/1020/1030 with PR1003/1022 interactions.

The existing worktree dirt in `docs/rfwgc-evidence/stage2-corpus/{measure.py,summarize.py}`, `docs/rfwgc-port-log.md`, and the two conformance submodules was preserved. No production source, submodule, index entry, benchmark, timing run, PR, push, closure, or external repository was modified.

## Verified defect

### C1 (low): region-mode configuration documentation contradicts native batch behavior

**Source.** At the production pin, `src/core/runtime/gc/native/collector.go:88-95` documents `NurseryRegionShift` and states: “Native allocation batches are disabled in this mode.” The implementation does the opposite for bounded small allocations: `src/core/runtime/gc/native/native_alloc.go:114-118` and `:137-141` route region-mode struct and small-array preparation to `prepareNativeRegionAllocation`; `src/core/runtime/gc/native/native_regions.go:3-34` reserves a bounded Eden interval and a handle batch. Only the general baseline path is excluded by `nativeAllocationAllowed` (`native_alloc.go:68-71`), while large-array native reservation is deliberately rejected (`native_alloc.go:176-180`). `src/core/runtime/gc/native/rollup_reservation_test.go:8-44` confirms the intended split: small region reservations succeed and large native reservations refuse.

**Concrete trigger and consequence.** Any maintainer or API consumer selecting `NurseryRegionShift != 0` and relying on the `Config` field contract will infer that all native batching is disabled, although generated-code small struct/array construction may use a region-owned native batch. This does not establish memory corruption or a behavioral failure, but it misstates the ownership/performance surface and can cause an incorrect safety review, integration assumption, or test plan.

**Baseline/introduced distinction.** The base `9713878ab452848f7eb7549a90be94b938a898be` has neither `NurseryRegionShift` nor the contradictory comment. Both the field contract and region-native reservation behavior were introduced by this campaign; the contradiction is therefore introduced, not inherited.

**Suggested repair.** Replace the final sentence with the precise policy: region mode disables the baseline direct-bump and native large-array paths, but permits bounded region-owned small struct/array batches whose unused tail is transactionally cancelled. Keep the existing reservation tests as the executable contract.

**Sibling cross-check before triage.** Read-only searches used Slipstream `dcb45b56386327a1f71fb039ed02e13562e1e2d7` and Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03`. Slipstream has native root-region and collector machinery but no analogous Wago `Config.NurseryRegionShift`/Go native-batch contract; Starshine is a compiler/model boundary and likewise has no analogous collector configuration surface. Neither sibling contradicts the Wago implementation or supplies a shared upstream wording defect. Both sibling worktrees were already dirty and remained untouched.

## No verified behavioral defect in this slice

Source review did not establish a collector correctness defect. In particular:

- Eden allocations are confined to one region, region bumps publish only after handle-publication failure points, and native region cancellation rewinds only an exactly owned tail (`eden_regions.go:12-30`, `alloc.go:128-183`, `native_regions.go:37-60`).
- Mature/LOS accounting distinguishes reserved from live bytes, checkpoints private allocator metadata before evacuation, restores it on precommit failure, excludes source blocks from destinations, and publishes handles only after joined copy completion (`mature_regions.go:28-75`, `mature_evacuation.go:235-257`, `:315-455`).
- Bounded run credit uses exact intervals, explicit worker slack, source exclusion, largest-object discount, and a 65,536-run ceiling; oversized promotion requests stay on exact-span allocation (`mature_evacuation.go:188-233`, `:458-579`).
- Required promotion failures retain pressure and clear private plans; optional old-to-old evacuation can refuse without corrupting required collection state (`mature_evacuation.go:37-62`, `:315-455`, `:472-579`).
- Parallel helpers read immutable heap/descriptor state, claim marks atomically, write disjoint copy/sweep outputs, join before callback/publication/reclaim, preserve sticky poison for unexpected helper panics, and close only after helper loops stop (`parallel.go:38-76`, `:230-333`, `:347-488`, `:490-642`). Ownership callbacks disable helper tracing (`parallel.go:78-101`) and remain coordinator-side after joins.
- PR1019 mark high-water clearing covers queued and helper-marked handles across failed walks; PR1030 cancellation preserves an independently pending large reservation and region-tail ownership (`mark.go:3-35`, `native_alloc.go:353-503` in the production source). PR1003’s widened extent validation remains before header decoding (`alloc.go:283-314`).
- TinyGo rejects helper configurations and uses serial trace/copy/sweep while retaining mature evacuation admission and callback behavior (`config.go:20-29`, `parallel_disabled.go:1-27`). The combined evidence includes actual patched TinyGo execution; compilation alone was not treated as proof.

These conclusions are bounded source-review results, not a final approval claim. Passing tests were used to check exercised contracts, not as substitutes for the ownership arguments above.

## Questions and coverage gaps (not verified defects)

1. Native Darwin execution remains `NOT_RUN`. Build portability and Linux/QEMU execution do not establish Darwin callback, mapping, or scheduler behavior.
2. Performance remains pending under the global measurement lease. This review ran no benchmark and makes no pause, throughput, wakeup, reservation-density, or helper-admission performance claim.
3. The campaign has deterministic failure injection for metadata, backing, promotion, reconciliation, helper panic, and callback ordering, but it cannot exhaustively model host OOM/runtime-fatal allocation behavior after Go has committed process memory. That is a general Go runtime boundary, not an observed regression.
4. TinyGo evidence is execution-qualified for the recorded patched 0.42 lanes and serial evacuation fixtures, not every TinyGo target/allocator combination. Unsupported helper mode is explicitly rejected rather than silently treated as covered.

## Commands and results

Read-only source/evidence inspection included:

```text
git rev-parse 9713878ab 5345bd365c13963f1bf60e26bb80789ab9ead4cc 2c9f72744dd6f414fc8f236c2d45aa39ce6ced69
git diff --name-status 9713878ab452848f7eb7549a90be94b938a898be..5345bd365c13963f1bf60e26bb80789ab9ead4cc -- src/core/runtime/gc/native src/core/runtime/gc src/wago
git log --oneline 9713878ab452848f7eb7549a90be94b938a898be..5345bd365c13963f1bf60e26bb80789ab9ead4cc -- src/core/runtime/gc/native src/core/runtime/gc
git show <pin>:<requested docs and relevant collector/evidence files>
git archive 5345bd365c13963f1bf60e26bb80789ab9ead4cc
```

Focused deterministic replay in the immutable archive, Go 1.27.1:

```text
go test ./src/core/runtime/gc/native -run 'Test(Evacuation|Parallel|RFWGC|NativeRegion|EdenRegion|FixedCollectionCost|NativeBatchCancel|Rollup)' -count=1
PASS (13.797s incidental test-framework duration)

go test -tags=wagodebug ./src/core/runtime/gc/native -run 'Test(Evacuation|Parallel|RFWGC|NativeRegion|EdenRegion|FixedCollectionCost|NativeBatchCancel|Rollup)' -count=1
PASS (7.800s incidental test-framework duration)

go test -tags=wago_regalloccheck ./src/core/runtime/gc/native -run 'Test(Evacuation|Parallel|RFWGC|NativeRegion|EdenRegion|FixedCollectionCost|NativeBatchCancel|Rollup)' -count=1
PASS (13.783s incidental test-framework duration)
```

The first ordinary invocation stopped before test execution because the archive’s `mise.toml` was untrusted. It was replayed with the explicit pinned Go binary and `GOROOT`; no trust mutation was made. No test ran past a known ownership failure. The archive path was `/tmp/wago-collector-review.MqhkQ3`; it is outside the worktree and contains no authoritative evidence beyond the commands recorded here.

The committed combined evidence reports 195 effective required `PASS` receipts and native Darwin `NOT_RUN`; its source pin and collector command receipts agree with the immutable production pin. No benchmark or timing measurement was performed in this review.

## Handoff

This review contributes one low-severity production-documentation finding and no verified behavioral defect. Parent owns consolidation, any repair, and fresh re-review. This report makes no final approval, closure, performance, or release-readiness claim.
