# H2 receipt core implementation (qualification in progress)

This directory owns the bounded H2 production slice. No final H2 or measurement
readiness claim. Baseline source10c2006fb, evidence500eacbc8.

The core registers a conversion domain independently of its publisher, gives
physical container slots distinct checked roots keyed by actual opaque identity
and exact collector, and journals domain-claim release outside topology readers.
The joint collection scheduler now has a store/domain entrypoint without a live
collector-owning Instance. Mirrored/local table replacement calls prepare/commit;
rollback and release are explicit. Publisher close clears only its floating root.
Conditional receipt roots participate in H6, with strong ordinary/callback roots.

Initial production test passes publisher physical close with two real provider
slots retaining one opaque conversion through separate roots, receipt-only joint
collection, and final domain release without another guest invocation, both
default and actual region shift6. Focused existing H2/ownership/conversion tests
pass in development ordinary mode. Further tests and immutable gates are pending.

Read-only sibling crosscheck: Slipstream084be3d4bf src/jit.goose registers imported
physical root regions and runtime authority before semantic publication.
Starshine1d6c0af471 docs/wiki/wasm-gc-core-boundary.md describes model/codec/
validation, with no equivalent native receipt lifetime mechanism. No new sibling
finding or native collector/backend/configuration edit.

Remaining integration hooks (next task): public/imported/global stores, table
fill/copy/init/grow and initialization, public clear and their teardown adapters.
They must share one gcConversionContainer for each physical allocation (aliases
must not manufacture owners), prepare the full range before the first write,
commit under topology/ordered-domain/native authority, and drain the deferred
journal after unlocking. Backend inline stores are not intercepted by this core.
