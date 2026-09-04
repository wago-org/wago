# GC reference boundaries

Go callers use `github.com/wago-org/wago/src/core/runtime/gc`. Its `Ref` is an
opaque value with a collector owner and a handle generation. Null and i31 values
remain portable. Object references from another collector, freed objects, and
reused handle indexes are rejected before object access, stores, initialization,
root publication, collection, tests, or casts. Closing a collector invalidates
its object references. `RefEq` includes ownership and generation.

Keep objects in `RootSet` values across collection. Holding a Go `Ref` alone does
not root the guest object. Root callbacks finish before their collected values
are checked and passed to the collector. Object-returning reads mint checked
references for the same owner and current generation. Collector descriptors are
copied, including field slices. Collectors and their roots still need external
synchronization; the checked API does not promise concurrent mutation support.

## Native ABI and migration

The compact 32-bit execution representation now lives at
`github.com/wago-org/wago/src/core/runtime/gc/native`. That package retains the
name `gc` for existing trusted runtime/compiler adapters. Its `Ref` words remain
collector-local indexes without ownership proof. It is not a safe ingress API
for arbitrary Go values. Public Wago tokens continue to be validated by the
runtime's existing store/domain layer before conversion to native words.

This is a breaking change for direct low-level Go callers. Keep the parent
import and remove integer conversions of object references. Use
`NewCheckedGlobalSlot`, `CheckedGlobalSlot`, `NewCheckedTableSlot`, and
`CheckedTableSlot`, which return errors. Scratch-based, prevalidated, direct-word,
barrier-only, payload-view and native-view APIs are now native-only. Normal Go
callers use the checked constructors, getters, setters, copies and root APIs.
Do not switch a normal Go caller to the native import merely to retain integer
casts or unchecked operations.

Generation storage is enabled only for checked collectors. It costs one uint64
per handle; a handle is retired before generation wrap. Native collectors gain
one optional bookkeeping pointer and no generation array. The native handle,
object, descriptor and artifact ABI layouts remain unchanged. Native benchmarks
and collector implementation tests now live in `gc/native`; use `go test
./src/core/runtime/gc/...` to cover both boundaries.
