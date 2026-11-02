# H2 module-owned end-to-end publication hooks

Source/test pin: `501e9ab46c4f68090c7bbeb17a6ad14f4fadda6d`.
This implements module-owned Linux AMD64/ARM64 publication paths on receipt core
`79db49daa1ea414d9c25e753e8ddd72ba0d6be78`. It is not an aggregate port,
performance, upstream publication, or final H2 closure claim.

## Production slices

- `1fb8f9423`: AMD64/ARM64 extern table/global publication helpers, canonical
  imported physical owners, single-cell receipt adapters, full-range bulk
  preparation, pre-admitted invocation domains, and the original provider probe.
- `6a7731c13`: staged active initialization, publisher-independent public-token
  lookup, public table set/clear, and descriptor-key migration preserving receipt
  objects and indexes. Public global validation retains exact nullability.
- `5b8c58b93`: synchronous helper call-frame and loop planning, including ARM64
  link-register preservation; per-segment Wasm trap ordering; real compiled
  global publication in conditional-cycle/callback/contention tests; migration
  rejection and identity-preservation tests.
- `501e9ab46`: independent physical tables populated by real guest table.set and
  table.copy, cross-Runtime public-token rejection, exact extern/noextern helper
  type routing, and ordinary host-token/null controls in host-created storage.

The original no-type/harmless-type, global/table provider shapes remain intact.
Their publishers execute actual `extern.convert_any`. The old audit expectation
that the publisher conversion state must remain live is replaced by the stronger
canonical-container/opaque-identity/exact-collector/checked-root oracle. The
publisher must physically retire. Only after this guard succeeds do tests collect
and access surviving containers. Global access and same-value replacement use the
public API; table clear executes in a real collector-free compiled guest. Final
clear proves domain release without another invocation. Historical audit artifacts
under `r9-converted-provider` are unchanged.

## Contract and behavior

All receipt-capable invocations pre-admit the topology's sorted domain set before
native entry, including an older collector-free provider. Helpers verify that
set belongs to the current invocation and acquire no additional invocation lease.
The public adapters acquire topology/domain authority before native state. Final
claim journals drain after native/domain/topology release. The `631cb` registration
proof and `10c2006fb` deferred-finalizer journal were not changed.

A physical global cell has one receipt index. Imported handles resolve their
original Instance owner. Tables canonicalize on the actual descriptor and never
allocate per-alias receipts. Opaque internal words, public conversion tokens, and
compact references remain distinct; public ingress authenticates the exact store.
Receipt creation uses checked collector root slots, not nursery object allocation
or collection. Existing frame-root maps and shadow edges remain in force; helper
call planning and synchronous control frames now include collector-free writers.

Fill, copy, init and grow validate bounds and prepare all incoming destination
receipts before any visible write. Copy snapshots sources before replacement,
including self-copy and overlapping aliases. Commit writes are infallible after
preparation, followed by exact old-receipt release. Grow publishes length last and
uses existing reserved capacity. Failed grow/bounds checks preserve storage and
receipts. Active imported segments evaluate in private storage; each completed
segment is atomic, and earlier completed segments retain their Wasm-mandated
side effects if a later segment traps. Local initialization acquires receipts
before the new instance becomes externally available.

Native host table growth does not relocate its reserved descriptor allocation.
`gcConversionContainer.migrate` supplies an explicit transactional key migration
for allocation replacement, preserving canonical ownership and receipt indexes.
Its callback must update all descriptor aliases and leave them unchanged on error.
The test uses a distinct native arena, checks rejected migration, then verifies
canonical reuse and final release through the public table API.

`Table.SetValue(index, ValueExternRef(...))` supports module-owned extern tables;
a null value clears the physical receipt. Module-owned export-handle Close retains
its existing no-op semantics; physical Instance.Close consumes owned receipts.
No permanent publisher retain or unconditional collector seed replaces ownership.
The conditional-cycle fixture now uses compiled global.set as its sole publication
hook and retains the full external-root, callback, contention and final-release
assertions from receipt-core qualification.

## Explicit remaining limits

- Host-created Table/Global storage with no Instance owner has no host-container
  conditional receipt adapter. Converted publication there is rejected before
  the visible write; ordinary store tokens and null retain their existing paths.
  The new public Table.SetValue requires module-owned storage. If host-created
  converted containers are part of the final H2 acceptance scope, that path remains
  implementation work; these results must not be used to close it.
- The migration adapter has no production relocation caller because current
  host growth stays within its reserved descriptor. A future relocating allocator
  must update every native/exported alias in the adapter's publication callback.
- RV64 retains its separate compact-extern implementation. TinyGo and other-host
  receipt implementations remain build-excluded stubs. Their regression/smoke
  results do not qualify host opaque receipts on those targets.
- Final combined-head qualification, complete optional external corpus/conformance
  execution, independent performance measurement and reviewer fan remain parent-owned.

See `qualification.json` for authoritative per-suite verdicts, counts and exact
skip names. Final lanes reuse unchanged committed drivers from `r9-h2-production`,
with immutable source archives and explicit stage8/platform toolchain paths.
Earlier failures and interrupted attempts remain failures/incomplete in their own
ledgers. `DEVELOPMENT.md` explains them. `sibling-readonly.json` records the
read-only Slipstream `084be3d4bf` and Starshine boundary audit before implementation.

Owned source slices were committed with --only. Existing stage2 scripts, shared
port log, index and dirty submodules were preserved. No subagents, Claude reviews,
benchmarks, timing workloads, measurement lease, PR, push or thread closure.

## Final suite results

Counts below are top-level passed tests; skip counts include nested skip entries.

| Suite | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| focused | PASS 42; skips 0 | PASS 42; skips 0 | PASS 42; skips 0 |
| full | PASS 2497; skips 44 | PASS 2491; skips 50 | PASS 2497; skips 44 |
| race | PASS 42; skips 0 | PASS 42; skips 0 | PASS 42; skips 0 |
| backend | PASS 998; skips 552 | PASS 996; skips 554 | PASS 1121; skips 553 |
| cross-arm64 | PASS 0; skips 0 | PASS 0; skips 0 | PASS 0; skips 0 |
| cross-riscv64 | PASS 0; skips 0 | PASS 0; skips 0 | PASS 0; skips 0 |
| arm64 QEMU | PASS 34; skips 0 | PASS 34; skips 0 | PASS 34; skips 0 |
| riscv64 QEMU | PASS 45; skips 0 | PASS 45; skips 0 | PASS 45; skips 0 |
| TinyGo smoke | PASS 5; skips 0 | PASS 5; skips 0 | PASS 5; skips 0 |

All 18 integrated, 14 target-driver and 3 TinyGo commands exited zero. Cross-target integrated lanes are compile-only; the QEMU rows execute real target binaries. Driver hashes match the unchanged production drivers. Integrated export integrity checks found no changed source files.
