# GC rollup assessment

Assessment base: `35c36bc594d2a027317cfcd321379f2d40c6b219`, production `501e9ab46c4f68090c7bbeb17a6ad14f4fadda6d`, captured from `experiment/rfwgc-port` on 2026-11-02. Support branch: `docs/gc-port-astra-support`.

All seven verified topics belong in the port. **INCLUDED means the port should carry the topic**, as requested in this brief; it is an advisory scope verdict, not the parent's final acceptance, PR closure, or a claim that final combined gates/reviews passed. Parent retains final bucket authority. References below are repository-relative file:line at this base unless an explicit source ref precedes them.

## Verified source identities

| PR | Repository ref | Full source head | Bucket |
| --- | --- | --- | --- |
| 997 | refs/port-assessment/pr997 | e2736f0f37e614218ed7da7ff4040cf1697431c3 | INCLUDED |
| 1003 | refs/port-assessment/pr1003 | 9666ec48e18cb2f7a77c188dc52f093770859f26 | INCLUDED |
| 1019 | refs/port-assessment/pr1019 | b6cb0e829011ab08d98d64eaefbc0085c34a0284 | INCLUDED |
| 1020 | refs/port-assessment/pr1020 | ce63d517a55f2981b7ccf6e150d1b9050fb5f953 | INCLUDED |
| 1026 | refs/port-assessment/pr1026 | 4101e75a3b915ce8b0217cd356b9837339c54ee3 | INCLUDED |
| 1027 | refs/port-assessment/pr1027 | 29e59c6413e502b000d00e40836350190db1f3c1 | INCLUDED |
| 1030 | refs/port-assessment/pr1030 | fba8e2b50ea0cbc31089789dd27dde628c55fe9d | INCLUDED |

These are local Git objects, not an assertion about today's remote PR heads. `git merge-base HEAD refs/port-assessment/prN` gives `9713878ab452848f7eb7549a90be94b938a898be` for all seven. Compare topic commits as well as that range: 1019/1020/1026/1027/1030 ranges contain shared upstream prerequisites, which must not be misattributed to each topic.

Fresh object lookups still reject the fleet-mentioned `421119d1d` and `11bcf23fd`. Do not claim those revisions integrated. Verified local alternatives include PR997 `fix/gc-extern-local-roots` at `e94701b95` and `t3/fix-extern-root-rebase` at `c7cc340a3`; PR1003 `t3/pr1003-cleanup-20261029` at `be7e6a973`. The older PR1003 fusion stack is distinct from its extent-only cleanup. The saved source/range comparisons in `rfwgc-evidence/rollup-rooting-assessment/README.md` explain the lineage; current refs and `cat-file` checks confirm the available identities. Recovering the two missing full objects remains a provenance obligation if the parent intends to claim those exact heads.

## PR 997 INCLUDED

Retain exact extern-conversion roots, caller identity, foreign/collector-free relay traversal and bounded native adapters. Source `e2736f0f3:src/wago/reference_store.go:1063` continues through the physical caller and adapter rather than truncating at a foreign return. Current `src/wago/reference_store.go:1093` rejects missing foreign owners/maps and `:1127` rejects nested/unbounded adapters; `src/wago/gc_frame_roots_runtime_linux_amd64.go:72` preserves domain host roots. Integration `4ad440e86` carries this cluster with PR1026. Region allocation and mature descriptors cannot discover omitted frame roots, so this is neither moot nor incompatible. Keep the source-engine extern/frame/foreign-relay tests, not obsolete artifact-engine assertions.

## PR 1003 INCLUDED

Retain the current extent-validation topic, not the historical abstract-array fusion proposal. Source `9666ec48e:src/core/runtime/gc/native/alloc.go:260` selects the backing space and `:271` validates the complete extent with widened arithmetic. Current `src/core/runtime/gc/native/alloc.go:283` through `:313` preserves that check before header access; `src/core/runtime/gc/native/ref_desc_bounds_test.go:5` retains malformed-extent coverage. Integration `a3f26602d` adapts it to the port. Region descriptors do not make forged/stale handle extents safe, so carry the guard and regressions. No verdict about the absent `11bcf23fd` object or the retired optimization follows from this verdict.

## PR 1019 INCLUDED

Retain bounded mark clearing and its queued-mark correctness repair. Current `src/core/runtime/gc/native/mark.go:6` clears the recorded high-water prefix; `:21` preserves the full-trace bound across nursery reset. `src/core/runtime/gc/native/fixed_collection_cost_test.go:101` exercises panic/minor/full graph preservation and `:154` covers queued and drained roots. The source stack includes `4cbaa605a` (queued bounds), not merely its initial optimization. Integration `e23657e6a` carries the topic into the region collector. New allocation geometry does not eliminate interrupted tracing or the need to reset every previously marked handle. This is a semantic inclusion, with no performance conclusion.

## PR 1020 INCLUDED

Carry parallel collection as a port-adapted feature rather than replaying the old allocator implementation. Source `ce63d517a:src/core/runtime/gc/native/parallel.go` supplies worker tracing/copy/sweep and explicit disabled-target handling. Current `src/core/runtime/gc/native/parallel.go:40` defines caller-owned callbacks/publication, `:245` creates a fresh phase and `:261` joins participants before inspecting results; `:350` and `:394` implement disjoint sweep/promotion work. Integration `bc05eef3e`, tests `779fb3f70`, and stage7 followups reconcile region/mature ownership. Parallel collection remains a requested capability; replacing the worker protocol to meet Snap invariants is adaptation, not whole-PR supersession. TinyGo remains serial and target exclusions must remain explicit.

## PR 1026 INCLUDED

Retain domain host-reentry discovery and scalar-adapter continuation as independently tested obligations, despite their overlap with 997. Source `4101e75a3:src/wago/gc_collect.go:13` enables domain discovery unconditionally. Current `src/wago/gc_collect.go:13`, `gc_frame_roots_runtime_arm64.go:67`, and `gc_frame_roots_runtime_linux_riscv64.go:17` preserve that behavior, while `reference_store.go:1127` keeps 997's stronger bounded continuation. Tests `src/wago/gc_domain_host_roots_native_test.go:38` and `gc_scalar_adapter_roots_test.go:42` remain distinct. Reconciliation `4ad440e86` avoids replacing the stronger walker with the older goto-based loop. Shared implementation does not remove the reentry/RV64 obligations, so INCLUDED is more accurate than SUPERSEDED.

## PR 1027 INCLUDED

Keep collector identity for scalar imports capable of carrying exception references. Source `29e59c641` changes `preferredGCCollectorFromImports`; current `src/wago/gc_type_domain.go:437` through `:442` considers staged and required EH features plus owned exception references, even for reference-free signatures. `src/wago/gc_type_domain_test.go:98` checks scalar exception imports, while `:137` retains the non-EH negative control. Integration `866e6b5e7` and EH repairs `27690e182` preserve the invariant beyond the initial source predicate. Snap tracing cannot repair a payload interpreted in the wrong collector, so retain this selection fix and actual exception-transfer coverage.

## PR 1030 INCLUDED

Keep live native array reservations intact when cancelling an unrelated batch, and reclaim promoted nursery storage only when no pending reservation needs it. Source `fba8e2b50` includes the post-fix reclaim followup, not just initial cancellation. Current `src/core/runtime/gc/native/native_alloc.go:359` cancels struct state separately; `:430` dispatches region cancellation or rewinds only an exclusively owned tail. `src/core/runtime/gc/native/native_batch_cancel_overlap_test.go:9` checks active reservation preservation and `:71` checks reclaim without a reservation. Integration `9bfe98360` adapts the ledger invariants to regions. A different allocation layout still needs exact reservation ownership, so neither dropping this fix nor wholesale replay over the region allocator is justified.

## Foreign stack GC fix INCLUDED

The TinyGo foreign-stack prerequisite is separate from 997's Wago guest frame traversal: TinyGo's own conservative collector must recognize registered foreign stack mappings and saved host SP boundaries. Current `src/core/runtime/trampoline_tinygo_amd64.go:126` registers the mapping, `foreign_stack_tinygo_threads.go:13` binds the patched runtime hook, and `engine_prepared_state_tinygo.go:14` unregisters during cleanup. Production `5bfd18174`, template correction `5a173c801`, and Close-failure correction `57a99d01d` are ancestors of this assessment base. `rfwgc-evidence/tinygo-foreign-stack-fix/README.md` records the deterministic pre-fix SIGSEGV, unchanged cancellation reproduction and 34 passing prerequisite commands in all three modes. The registered mapping must remain alive across failed thunk unmap, nested entry, trap and interruption. Retain this fix; ARM64 compilation alone is not execution qualification and old prerequisite results are not a combined-head pass.

## Remaining H2 gaps and triage

Module-owned global/public/imported stores, fill/copy/init/grow and staged initialization are implemented at this base. See `src/wago/gc_conversion_store.go:15`, `:100`, `gc_conversion_store_host.go:108`, `:190`, `:263`, and `:367`. Do not schedule them as missing implementations based on the older receipt-core API handoff alone.

Host-created converted storage remains unsupported: `gc_conversion_store_host.go:56` and `:220` reject converted words without an Instance owner, `:368` rejects ownerless public Table.SetValue, and `gc_conversion_receipt_host.go:88` requires an Instance. Guest single stores additionally reject missing imported owners at `gc_conversion_store.go:39` and `:59`. This is a documented acceptance gap, not a newly reproduced corruption. Supporting host-created converted containers requires real physical-container/root authority and close/clear semantics; silently using the publisher as owner would reintroduce H2.

Storage migration is **not missing production relocation work**: `gc_conversion_receipt_host.go:621` documents reserved-capacity growth, and `:626` supplies transactional migration for a future allocation replacement. The current migration test at `gc_conversion_store_host_test.go:443` is the caller. Preserve its identity/rollback contract; do not invent a relocating allocator for this campaign.

Read-only sibling cross-check: Slipstream object `084be3d4bf70fb5337085ceddfab1bfa9db8b1c9`, `src/jit.goose:294`, installs imported global regions before publication. Its region representation is not evidence that Wago's host receipt adapter exists. Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03`, `docs/wiki/wasm-gc-core-boundary.md:36`, identifies model/codec/validator/generator boundaries rather than an analogous native collector. These agree with the committed store-hook sibling audit. No new sibling defect or new executed port defect is asserted.

## Qualification status

The committed store-hook README records three-mode focused/full/race/backend checks and ARM64/RV64 QEMU on production `501e9ab46`, with explicit exclusions for host opaque receipts on RV64/TinyGo. Parent reports a separate combined qualification worker owns `docs/rfwgc-evidence/combined-store-hook-gates/`; its outcome is not assumed here. This assessment adds no implementation, builds, measurements, PR or remote push. Final combined gates, provenance reconciliation and review remain parent-owned.
