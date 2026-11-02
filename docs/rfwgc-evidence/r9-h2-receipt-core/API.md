# Receipt-core handoff to remaining store hooks

Source/test pin `79db49daa`, including `b61de6ef1`, `ae64b76c1`,
`a01344bc8`, `85e1d8496`, and `c35acfe97`.
Implementation: `src/wago/gc_conversion_receipt_host.go`.
This is an internal Linux host API, exercised on AMD64 and ARM64. RV64 retains
its separate compact-extern representation; other/TinyGo builds use stubs.

## Physical identity and transaction

`newGCConversionContainer(owner, descriptor)` validates a local extern table's
actual descriptor against the physical owner, then canonicalizes on its storage
address. All aliases of that allocation receive the same container. A different
owner cannot claim the descriptor. Distinct allocations and equal slot numbers
remain independent. Growth/relocation will need an explicit descriptor migration
adapter; creating a fresh owner for the same live allocation is not valid.

`container.prepare(index, opaqueWord, source)` authenticates the actual opaque
identity, bounds, registered collector and physical owner. It acquires a fresh
checked table slot and a domain claim before publication. `source` is needed only
when first registering an identity from the publishing conversion state; an
already registered identity remains usable after that state closes. Public
store tokens and null require no GC receipt. A same-value prepare reuses the
existing physical receipt. Slot identity includes the canonical container,
index, opaque conversion identity and exact collector; opaque words are never
cast to compact references.

`transaction.commit(publish)` executes the visible write, then releases the old
receipt. The callback must not reenter or mutate ownership and must leave storage
unchanged on error. `rollback()` is idempotent; commit also rolls back on error or
panic. Multi-slot hooks must prepare the complete destination range before the
first visible write and then use infallible writes for the commit phase. The
current implementation wires this into `gcRefTestTableState.setTable`.

`receipt.release()` and `container.release()` are idempotent. Physical Instance
teardown consumes its canonical containers, including collector-free owners.
Terminal store retirement invalidates remaining metadata before collector close.

## Authority and retirement

Call prepare/commit/rollback/release only while the physical owner is live and
under topology -> sorted invocation domains -> native execution authority for
all participating domains. This API does not acquire that authority for a
caller already in a guest helper. Do not wait for a new domain while retaining a
partial domain/native lease. The future public adapter must acquire the whole
set before calling prepare. Successful acquisition makes the container owner's
subsequent invocations use dynamic ordered domain leases.

Private conversion type universes register in topology during construction and
are excluded from both shared type-universe selection and preferred shared-type
import admission. Lifetime registration does not change the original frame/type
isolation. Domain lifetime is independent
of the publishing Instance. A receipt does not increment publisher resourceRefs.
A converted object's real function edge can still retain its producer normally.

The entry's floating checked root remains strong during active execution and
callbacks. Successful receipt publication makes it conditional at the full H6
quiescent boundary. Each physical receipt has its own conditional root attached
to its owner. The joint fixed point traces object -> function producer -> physical
provider dependencies; one entry root cannot stand for multiple owners.

The scheduler invokes `collectManagedFunctionStore(store, invocationID)` after
acquiring topology, registered domains and native locks. It enumerates receipt
authority even when there is no collector-owning Instance. Registration is
revalidated before heap access. Existing coalesced retries wait on a single
blocker with no other locks, then restart the complete protocol.

Final receipt claims enter the graph's deferred journal. The registered domain
tracks those claims independently of the opaque directory, so the scheduler
still leases a domain after its last receipt row has been removed. Call
`drainManagedFunctionStoreReclaims(store)` after releasing every topology/domain/
native lease. It unlinks the final domain under the writer and closes the heap
after unlocking. Physical callbacks stay outside collection locks. The existing
10c2006fb pending-finalizer and retired-successor guards remain intact.

## Next hooks, not covered here

- Public and backend-inline extern table set/clear.
- Imported table stores, resolving aliases to the actual physical owner.
- Mutable global/imported-global cells, with a physical-cell adapter.
- Table fill/copy/init/grow, overlap handling and initialization.
- Public container disposal and migration adapters for those hooks.

These paths must transact opaque receipts before publishing values. Their
absence is not a qualification claim. The tests here directly use the real
mirrored-table transaction on actual provider descriptors; they do not claim
backend imported/global stores were intercepted. Existing local-table guest
conversion tests also run through the newly wired transaction.
