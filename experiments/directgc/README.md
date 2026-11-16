# Experimental direct-offset collector substrate

This package explores a breaking reference representation, independently of Wago's
runtime. It is **not wired into Wago**, does not replace the native or checked
collectors, and provides no V8 performance claim.

Planned correctness baseline: one bounded byte arena, a copying nursery, two old
semispaces, exact mutable roots, object-declared reference fields, and an explicit
old-to-young write barrier. References are aligned nonzero arena offsets, not
indices into a handle table. Collection plans destinations before mutation so
capacity failure leaves roots and the object graph unchanged. Every nursery
survivor is initially promoted; survivor aging is deliberately deferred.

A runtime integration would require a new reference ABI, compiler object access,
precise writable safepoint stack/register maps, host handles that pin or update,
mutable globals/tables/exception roots, and snapshot/checked-reference adaptation.
Unregistered Go copies of offsets cannot survive collection. Existing collector
and Tiny behavior remain unchanged.

## Prototype contract and limits

`New(nurseryBytes, oldBytes)` allocates one arena of
`8 + nurseryBytes + 2*oldBytes`. The first 8 bytes reserve null. Every object has
an aligned 16-byte header, declared 32-bit reference slots, padding, and 64-bit
scalar words. `SetRef` supplies the old-to-young barrier. `Minor` and `Full`
require exact mutable `[]*Ref` roots; allocation never collects implicitly.
Collection preflights the complete relevant graph and capacity, then performs
non-failing disjoint copies and rewrites. Repeated/aliased root pointers are
supported. Nursery survivors all tenure immediately. Minor scans all dirty old
objects conservatively, including globally unreachable ones; Full reclaims those.

There is no raw byte/pointer access, concurrency, type/subtype enforcement,
large-object path, pinning, finalization, weak references, stack map, or host-token
adapter. Interior and inactive-space addresses are rejected. **Stale offsets that
alias a later allocation cannot be detected:** callers must register every live
reference across collection and may not use an offset from another collector.
Go process allocation failure during construction is not an `ErrOOM`; controlled
arena exhaustion during allocation/collection is. The declared bounds concern
collector-owned backing, not arbitrary caller root-list size.

This first correctness representation deliberately spends substantial fixed
metadata: two booleans and one 32-bit forwarding entry per 8-byte arena granule,
plus a 32-bit queue entry per minimum 16-byte object. `FixedBytes` reports the
exact scalar backing bytes including arena, excluding Go headers. For a 1 MiB
nursery and two 4 MiB old spaces, this is approximately 18 MiB before Go headers.
No handle lookup is needed for an object's address, but checked API validation
still consults an object-start bitmap. These tradeoffs are **not** evidence of a
memory or throughput improvement.

Validation:

```sh
go test ./experiments/directgc -count=1
go test -race ./experiments/directgc -count=1
```

Tests cover cycles/sharing, exact roots and duplicate slots, repeated full
collection, old-to-young updates/overwrites, scalar payload preservation,
randomized graph-oracle comparison, bounded allocation arithmetic, invalid
references, zero Go allocations during collection, and byte-for-byte arena/root
invariance on failed minor/full collection followed by a successful retry.
