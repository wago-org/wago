# H2 parallel implementation slices

Coordination checkpoint: assessed production/source head `35c36bc594d2a027317cfcd321379f2d40c6b219`; observed port head `8a3defbcc1027b6c4afdd37e2fec5de5c54e0425` adds only the coordination log, with no `src` changes. Source conclusions remain pinned to the assessed head. The port orchestrator is the sole implementation authority; this document is advisory integration input, not a landing decision. **UNVERIFIED OWNERSHIP for the proposed new slices:** path sets are textually disjoint, but they are not active worker assignments. The integration owner must confirm current ownership and adopt the API contract before launch. The committed coordination log confirms the prior hook worker released its files; that release does not assign them to these proposed workers.

Base `35c36bc594d2a027317cfcd321379f2d40c6b219` (production `501e9ab46`). This is an advisory launch contract for the integration owner, who must adopt or reject it before dispatch. The prior hook worker has released all files. The combined qualification worker alone owns `docs/rfwgc-evidence/combined-store-hook-gates/` and its immutable exports; neither proposed worker may touch those paths.

The old `rfwgc-evidence/r9-h2-receipt-core/API.md` describes missing hooks at `79db49daa`. The newer `rfwgc-evidence/r9-h2-store-hooks/README.md` and source supersede that missing-work list: module-owned global/imported/public stores, bulk transactions, active initialization and compiled-global cycle tests now exist. Remaining necessary work, if host-created converted containers are included in final acceptance, is physical host-container authority and its guest integration. Preserve module-owned behavior as regression coverage.

## Launch contract and serialized boundary

Two workers can develop concurrently using the existing typed signatures below. Slice A owns their semantics; Slice B only calls them. Before launch the parent must approve the following nil-owner convention: a nil Instance means an authenticated host-owned physical container, resolved from the current store and storage identity, never the publishing Instance. Unknown, closed or foreign-store storage must fail before publication. Do not pass a fabricated Instance merely to satisfy today's API.

- `publishConversionStore(owner *Instance, storage []byte, index, word uint64, publish func() error) error`
- `publishConversionRange(owner *Instance, storage []byte, start, limit uint64, words []uint64, publish func()) error`
- `setConversionGlobalPublic(access *Instance, g *Global, value Value) (bool, error)`
- `setConversionTablePublic(t *Table, index uint64, value Value) error`

The existing functions compile on both branch bases. Slice A supplies canonical registration/lookup through its host adapter and public/container attachment paths. Slice B forwards authenticated imported descriptors and nil Instance identity without early rejection. This permits parallel coding without parallel edits to shared files; it does **not** make B's host-container acceptance independently green before A lands. Merge A first, then B, then run the combined immutable matrix. No production prerequisite patch is needed if this contract is accepted. If implementation reveals a required signature/metadata change, the parent serializes that narrow API commit first and rebases both slices; neither worker edits outside its allowlist.

## Slice A host container authority and public adapters

Implement host Table/Global physical ownership, store binding and receipt lifecycle independent of any publisher or importing Instance. Public set/same-value/clear and Close must transact exact roots, reject foreign tokens, preserve ordinary tokens/null and release the final domain claim exactly once. A live externally held host container is a real root; importer retirement must not destroy its receipt. Closing the container and removing its last external edge must permit converted producer/provider cycles to drain. Extend joint reachability with a real host-container authority, not an unconditional collector seed or permanent publisher retain.

Exact existing production-file allowlist:

```text
src/wago/cross_instance.go
src/wago/globals.go
src/wago/reference_store.go
src/wago/gc_conversion_receipt_host.go
src/wago/gc_conversion_receipt_other.go
src/wago/gc_conversion_receipt_riscv64.go
src/wago/gc_conversion_store_host.go
src/wago/gc_conversion_store_other.go
src/wago/function_environment_collection_linux.go
src/wago/function_environment_mark_linux.go
src/wago/function_environment_roots_host.go
src/wago/function_environment_reclaims_linux.go
```

Exact new paths reserved for A:

```text
src/wago/gc_conversion_host_container_host.go
src/wago/gc_conversion_host_container_other.go
src/wago/gc_conversion_host_container_host_test.go
```

Use matching build exclusions for the new host implementation and its portable stub. A owns any necessary shape adjustments to `gcConversionContainer`, `prepareLimit`, `migrate`, `conversionBoundaryWord`, `initializeConversionReceipts`, and the public adapter bodies in `gc_conversion_store_host.go`. Preserve signatures used by B. If another joint-graph file proves necessary, request a parent allowlist amendment before editing it. Existing production-file ownership is permission for necessary edits, not a requirement to change every file.

A's new test names must use `TestR9H2HostContainer...`, with `PublicGlobal`, `PublicTable`, `Close`, `Cycle`, `Migration` and `Authority` suffixes for targeted selection. Inline WAT fixtures belong in that new test file so fixture paths cannot collide. Existing receipt/store-hook tests remain read-only regression inputs for A.

Evidence directory reserved for the eventual worker: `docs/rfwgc-evidence/r9-h2-host-containers/`. Preserve failed probes and name the immutable source pin. This support task creates no files there.

## Slice B guest imports and bulk publication

Carry host-created physical storage through existing helper dispatch and initialization. At `gc_conversion_store.go:39` and `:59`, distinguish an absent/invalid imported handle from a valid host-owned handle with no Instance. Keep validation and bounds ordering, and let A's adapter authenticate the storage against the current store. Table set/global set, fill/copy/init/grow and imported active segments must all reach the same physical receipt authority. Pre-existing module-owned receipts and native code generated before a conversion domain exists must continue to work.

Exact existing production-file allowlist:

```text
src/wago/gc_conversion_store.go
src/wago/instantiate.go
src/wago/gc_array_helper.go
src/wago/gc_ref_test_table.go
src/wago/instance_call.go
src/wago/api.go
src/wago/externref.go
src/wago/gc_frame_roots.go
src/wago/gc_frame_roots_arm64.go
src/wago/gc_frame_roots_linux_amd64.go
src/core/compiler/backend/railshot/shared/extern_store.go
src/core/compiler/backend/railshot/amd64/call.go
src/core/compiler/backend/railshot/amd64/control.go
src/core/compiler/backend/railshot/amd64/globals.go
src/core/compiler/backend/railshot/amd64/hints.go
src/core/compiler/backend/railshot/amd64/table.go
src/core/compiler/backend/railshot/arm64/call.go
src/core/compiler/backend/railshot/arm64/control.go
src/core/compiler/backend/railshot/arm64/globals.go
src/core/compiler/backend/railshot/arm64/hints.go
src/core/compiler/backend/railshot/arm64/table.go
```

Exact new test paths reserved for B:

```text
src/wago/gc_conversion_host_import_host_test.go
src/core/compiler/backend/railshot/amd64/extern_host_store_contract_test.go
src/core/compiler/backend/railshot/arm64/extern_host_store_contract_test.go
```

B owns helper IDs 53–58 and their dispatch, `ExternStoreMayCall`, `compiledExternStoreHelperSlots`, the `conversionInitialTable` staging record/producer, and the `Table.SetValue` forwarding wrapper. Freeze helper IDs, arities, receiver signatures and opaque-word representation. A owns the called host adapter, including all range preparation. The public forwarding wrapper must remain a delegate; do not duplicate A's ownership logic there. Initialization staging must preserve earlier completed segment side effects if a later segment traps. Invocation entry must acquire topology and the complete ordered domain set before native execution; helper calls must not wait for a new invocation lease under a partial lease.

B tests use `TestR9H2HostImport...` with `Global`, `Table`, `Bulk`, `Initialization`, `Cycle`, and `Authority` suffixes; backend tests use `TestExternHostStoreContract...`. Put fixtures inline in the new Go tests. Cover collector-free/older providers, aliases, both overlap directions, independent containers, failed bounds/grow, ordinary tokens/null, publisher retirement and final clear. The guest cycle must publish via real compiled instructions, not a receipt test helper.

Evidence directory reserved for the eventual worker: `docs/rfwgc-evidence/r9-h2-host-imports/`. B may establish module-owned regression results immediately, but host-container acceptance requires A's commit plus B's commit on the integration head.

## Disjointness and shared obligations

The two proposed production allowlists and new-path lists have empty intersections as a static path check; actual worker ownership remains UNVERIFIED OWNERSHIP until the integration owner assigns them. A owns all receipt/host-adapter/public-container lifecycle files; B owns generic dispatch/compiler/invocation/initialization files. Test files and evidence directories are distinct. Neither may edit the shared port log, the old API/README evidence, this support branch's documents, the other's tests or the combined qualification worker's directory. Parent owns the eventual integration ledger and consolidated test results.

| Concern | A responsibility | B responsibility |
| --- | --- | --- |
| Globals and public stores | Physical cell authority and public transaction | Imported global resolution and compiled publication |
| Imported tables | Authenticate canonical host descriptor/store | Pass actual descriptor and physical identity through helpers |
| Bulk operations | Prepare complete receipt range before visible writes | Bounds, overlap snapshot, helper ABI, length-last grow |
| Initialization | Commit staged receipt ownership | Stage source values and preserve segment trap order |
| Cycles and retirement | Conditional roots and exact final claim release | Real compiled publication/clear and importer retirement probes |
| Migration | Preserve migrate identity/rollback tests for the generalized authority | Preserve current in-place reserved-capacity growth |

There is no production relocation slice. Current native grow does not relocate (`gc_conversion_receipt_host.go:621–625`). A's migration test may exercise the generalized host authority with a replacement arena, but adding an allocator policy or new production relocation caller is outside these slices. RV64 compact extern and TinyGo stubs remain separate compatibility lanes, not claimed implementations of host opaque receipts.

Acceptance and executable commands are in `h2-qualification-matrix.md`. Parent must approve the ownership contract, exact allowlists and host-root lifetime semantics before launching two implementation workers. No worker is launched by this advisory task.
