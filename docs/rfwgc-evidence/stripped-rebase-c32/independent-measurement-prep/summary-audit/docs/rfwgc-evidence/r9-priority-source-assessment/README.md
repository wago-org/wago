# Round 9 priority lifecycle source assessment

Date: 2026-11-01. Scope: source inspection and this documentation only.

## Result and provenance

| Finding | Main | Port | Final disposition |
| --- | --- | --- | --- |
| H2-F1: pre-collector persistent containers | Ownership mechanisms exist; exact pre-collector provider path not established end to end | Same relevant lifecycle mechanisms; no demonstrated repair for this case | **Unresolved** on both pins |
| H6-F3: nested function owner after failed initialization | Direct-container retention and RV64 managed-edge machinery exist; exact failed-start nested path not established end to end | Same relevant lifecycle machinery; no demonstrated repair for this case | **Unresolved** on both pins |

Neither row is assigned INCLUDED-FIX, PRESENT-OUT-OF-SCOPE, or ABSENT. Lack of a completed proof is not evidence of a Wago defect. Conversely, the presence of root enumeration or direct-container retention does not prove either finding absent. INCLUDED-FIX requires an integrated fix and green gates, which this assessment does not supply.

Immutable inputs inspected with `git show` and ref-qualified searches/diffs:

- Wago main: `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`.
- Wago port: `ca30843801956bae67d5cd1219f66d191518c125`.
- Slipstream report: `dcb45b56386327a1f71fb039ed02e13562e1e2d7:hunt-findings.md`; its reviewed source is `82d546679bab4887acfab056f6ad773a969b1e03`. Its runtime findings are source-report context, not independently reproduced Wago evidence. No reproducer was copied or executed.
- Later committed Wago checkpoints inspected separately: `b46fefb2afe9f58ec3f64f2fba88a41b52a577d6` and `57a99d01df0ae2beb5afc7a947bc9e06621b2259`.
- Starshine checkout located at `/home/dev/Projects/starshine-master-base`, pinned HEAD `1d6c0af47140299d8f3c01ed03060d279c43ee03`.

Read `/home/dev/Projects/wago/AGENTS.md` and the latest owner orders in the assigned worktree's `docs/rfwgc-port-log.md`, including the round-9 propagation order committed in `27c8ad85b`. The log was read for instructions only. Dirty production files and untracked regression files in the shared worktree were not treated as verified source. No branch switch, worktree creation, delegation, source change, test, build, benchmark, debugger, crash experiment, PR, push, or closure occurred.

## H2-F1: collector and persistent container ownership

Evidence below uses port paths/symbols; the central lifecycle files are identical at the main pin unless the difference is stated.

1. `src/wago/instantiate.go:287`, `instanceBuilder.prepareCollector`, derives collector need from compiled execution/type metadata, helpers and extern conversion. Its call at line 512 is in instance construction before import attachment, not a decoder type-section callback. It can return with no collector. This refutes transferring Slipstream's specific decoder-callback explanation directly to Wago, but does not settle pre-existing containers.
2. `src/wago/code_mapping.go:407`, `Compiled.needsRuntimeGCCollectorDomain`, includes generic execution, collector call boundaries, owned exceptions, opaque extern helpers, and certain declaration roots with safepoints/callsites. `sharedGCPersistentDomainSafe` at line 579 distinguishes GC, function, and compact extern table representations. A declaration-only provider must be traced through these predicates rather than assumed to receive a collector.
3. `src/wago/gc_type_domain.go:404`, `preferredGCCollectorFromImports`, checks store/domain compatibility. Its `consider` closure ignores an instance whose collector is nil. It also consults compact extern host-resource bindings. Selecting an existing collector is not itself proof of retroactive registration of every older provider.
4. `src/wago/reference_store.go:1588`, `rangeGCDomainPersistentRoots`, and line 1667, `rangeGCDomainPersistentRootsClassified`, enumerate registered instances, but filter out released instances and instances whose `candidate.gc != collector`. They enumerate eligible GC global cells and call `rangeLocalGCTableRoots`. `gcNativeFrameRoots.walk` and `RangeClassifiedRootRefs` use this domain enumeration for store-owned collectors. The equality filter is a concrete proof obligation for a provider initially constructed without a collector; it is not proof that its imported cell is unrooted, since another registered owner or representation-specific binding can cover it.
5. RV64 `gc_compact_extern_host_table_riscv64.go`, `railshotAttachCompactExternHostTable`, and `gc_compact_extern_host_global_riscv64.go`, `railshotAttachCompactExternHostGlobal`, explicitly acquire a collector-domain claim for host-owned extern resources. Both distinguish `owner.instance == nil`, and return without attachment when the collector is nil. These are useful late-binding mechanisms, but host-owned resources are not interchangeable with module-owned exported containers.

The inspected ordinary regression text in `gc_cross_instance_persistent_test.go`, `TestGCCrossInstanceCallsWithSharedPersistentRoots`, includes shared table/global state, Tiny and throughput configurations and continued use after provider close. Its provider fixture declares a struct and function types, allocates, and the test explicitly requires a non-nil shared collector. It therefore does not prove the declaration-only/no-type-section provider-before-collector case. Reading its configuration and assertions is not a claim that it passed in this assessment.

**Missing proof:** for each admitted target and representation, connect a module-owned table/global created before any collector to the later consumer's domain and actual root source. Show either its registration/binding, its alias being enumerated with sufficient lifetime after consumer retirement, or a documented admission rejection before publication. Include extern-token conversion versus compact managed references; do not equate their cell encodings. This assessment did not establish that complete chain and does not claim the registration link is absent from the repository.

## H6-F3: failed initialization and nested producer ownership

1. `instantiate.go` installs function/memory/global/table/frozen-global dependencies after normal Close owns rollback, registers the instance with the reference store, and binds function/extern construction state before start. `finishConstruction` routes failure through `in.Close()` and ends construction. This sequencing supplies a real ownership foundation.
2. `instantiate.go:2202` invokes `retainProducerRootsInImportedContainersForFinalization` on non-Runtime failure; `src/wago/reference_lifetime.go`, `referenceLifetime.finalize`, also performs retention before quiescence notification and the final physical-resource check. Thus the standalone defer is not the whole Runtime failure path. Logical close, quiescence and physical release are deliberately separate.
3. `src/wago/import_attachments.go:535`, `retainProducerRootsInImportedContainersForFinalization`, dispatches table/global retention. The generic multi-container path discovers descriptor owners and explicitly filters global imports to `ValFuncRef`; the RV64 path transfers exact dependency receipts. These are direct-container lifetime contracts. They cannot alone prove retention of a descriptor inside a struct/array reachable from another provider's global, especially where only a function import connects the instances.
4. RV64 non-TinyGo has a separate nested ownership mechanism. `function_environment_riscv64.go` defines managed environments, weak descriptor/owner maps, aggregate shadow edges and `ObjectEdgeCount`/`ObjectEdgeAt`. `function_environment_aggregate_riscv64.go`, `managedFunctionAggregateScratch.change`, resolves descriptors through `environmentForDescriptor`; `publish` records shadow edges and applies `WriteBarrierObject`. `function_environment_mark_riscv64.go` defines the joint mark over objects, owners, native roots and dependency receipts; `bindPersistentSlot` associates conditional slots with their physical owner. These are substantive nested-edge proof paths, not a completed failed-start proof.
5. `function_environment_construction_riscv64.go` journals opaque objects before the instance descriptor directory exists and describes binding before start and before release of the shared construction lease. `instantiate.go` calls that binding. In contrast, `function_environment_other.go` is selected outside Linux RV64/non-TinyGo: managed registration hooks are stubs, `compiledManagedFunctionReachability` is false and `collectManagedFunctionGraph` reports no managed collection. This target split must be reconciled with each backend's representation/admission and other owner-retention mechanisms; stub presence alone is not a defect verdict.

Existing normal ownership regression text inspected:

- `start_test.go`, `TestRuntimeStartTrapKeepsImportedMemoryAndTableSideEffects`: preserves active-segment memory and direct funcref-table effects after a start failure. It does not store the function inside a managed aggregate.
- `failed_instantiation_reference_store_test.go`, `TestFailedInstantiationTerminatesReferenceStoreRegistration`: checks quiescence/accounting and terminal token/externref cleanup. It does not prove an escaped nested function remains owned.
- `function_environment_public_aggregate_riscv64_test.go`, `TestRV64ManagedFunctionAggregateForeignExecutionAndArtifacts`: checks foreign function environments through struct/array operations and producer logical close, followed by terminal reclamation. It uses a store-issued token and successful instances; that additional root differs from a failed producer whose sole remaining path is nested managed data. The artifact loop in this test is only `false`.
- The same file's `TestRV64ManagedFunctionConstructionExecutionAndArtifacts` checks construction environments, collection and final reclamation, but successful construction rather than failed initialization.

**Missing proof:** connect an admitted nested function publication to the exact producer/code owner, then through failed-initialization Close, reference-store quiescence, managed graph/other retention and final physical release. Establish that the nested edge alone is sufficient, without a surviving public token or direct funcref-container root masking the result. Prove eventual release when that edge disappears. The inspected tests cover pieces of this chain but do not establish its composition on each target.

## Main/port differences and later integration

Ref-qualified comparison found no main-to-port changes in `instantiate.go`, `reference_store.go`, `gc_type_domain.go`, `import_attachments.go`, `reference_lifetime.go`, or the inspected `function_environment*` files. The `code_mapping.go` difference is compiler-owner construction with an initial `Compiled` value, not changes to the domain predicates cited above. `gc_array_globals.go:reconcileGCGlobalRootsAfter` adds cancellation of unused native allocation-batch reservations on failure before root reconciliation; that is not evidence of either persistent-container registration or nested function-owner retention. Other port/compiler changes exist, so identical lifecycle files do not prove identical end-to-end admission or execution.

Later commit `b46fefb2a` adds exact callsite/exception prerequisites: backend callsite/rootmap changes and frontend `GCTypeMetadataOptions.OwnedExceptionReferences`, with `api.go` using representation-aware metadata lowering. Its committed diff does not supply a pre-collector container lifecycle or failed-start nested owner proof. Commit `57a99d01d` changes TinyGo stack unregister ordering during fallible cleanup. Neither is assigned INCLUDED-FIX for these two findings. No later dirty rooting integration is counted, and any subsequently committed integration requires a new pinned assessment.

## Sibling cross-check and outstanding normal validation

Starshine's committed `docs/wiki/ir2/architecture-rules.md` and `docs/wiki/raw/binaryen/2026-07-15-flatten-version-130-internal-output-recursive-ownership-impact.md` describe compiler IR ownership, child-span contracts, and complete recursive ownership proofs. `docs/wiki/wasm-gc-core-boundary.md` explicitly separates type/model/codec/validator coverage from narrower authoring and other layers. These documents do not establish a Wago-like native collector/module rollback contract. No Starshine runtime verdict follows. The applicable lesson is to name the ownership layer and prove the complete reachable edge chain; syntax acceptance and direct-container ownership are insufficient substitutes.

Still missing, for separately authorized normal validation:

1. A reviewed ownership assertion matrix covering module-owned table and mutable global providers constructed before the first collector, with and without a type section, distinguishing compact and token extern representations. Evidence must identify domain membership/root coverage and balanced lifetime after consumer close. Host-created resources are a separate control.
2. A reviewed ownership assertion matrix for a failed initialization with a provider-visible nested managed function edge, proving physical producer/code retention and later reclamation without extra token/direct-container roots. Separate target admission, graph and teardown assertions; direct escape and successful construction are controls only.
3. Results for the final immutable integration head in ordinary, `wagodebug`, and `wago_regalloccheck` modes on the applicable supported targets, with unsupported/rejected cases recorded explicitly. This assessment ran none of those modes, by instruction, and does not propose executable crash demonstrations or retry the rejected audit.

This limited source assessment does not close the original runtime verification gate or bypass the prior automatic safety-review rejection. Production files were not modified. This documentation-only change cannot affect production performance; no measurement is needed or claimed. The documentation commit is identified in the handoff rather than embedded self-referentially here.
