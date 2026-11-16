# Experimental direct-offset collector substrate

This package explores a breaking reference representation, independently of Wago's
runtime. It is **not wired into Wago**, does not replace the native or checked
collectors, and provides no V8 performance claim.

Implemented correctness baseline: one bounded byte arena, a copying nursery, two old
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
`16 + nurseryBytes + 2*oldBytes`. The first 16 bytes reserve null. Arena backing and all object starts
are 16-byte aligned. Every object has the native-compatible 16-byte
`TypeID/Size/Aux/Flags` header. `Size` rounds the logical extent to 8 bytes;
physical stride rounds `Size` to 16 bytes. `Aux` preserves exact array length,
including packed arrays. This initial uniform alignment inflates a 24-byte
object to a 32-byte allocation. `SetRef` supplies the old-to-young barrier. `Minor` and `Full`
require exact mutable `[]*Ref` roots; allocation never collects implicitly.
Collection preflights the complete relevant graph and capacity, then performs
non-failing disjoint copies and rewrites. Repeated/aliased root pointers are
supported. Nursery survivors all tenure immediately. Minor scans all dirty old
objects conservatively, including globally unreachable ones; Full reclaims those.

There is no concurrency, type/subtype enforcement,
large-object path, pinning, finalization, weak references, stack map, or host-token
adapter. Interior and inactive-space addresses are rejected. **Stale offsets that
alias a later allocation cannot be detected:** callers must register every live
reference across collection and may not use an offset from another collector.
Go process allocation failure during construction is not an `ErrOOM`; controlled
arena exhaustion during allocation/collection is. The declared bounds concern
collector-owned backing, not arbitrary caller root-list size.

This first correctness representation deliberately spends substantial fixed
metadata: two booleans per 8-byte arena granule, a mark bit per 16-byte granule,
plus a 32-bit queue entry per minimum 16-byte object. `FixedBytes` reports the
exact scalar backing bytes including arena/alignment slack, excluding Go headers
and separately registered descriptors/convenience shape mappings. For a 1 MiB
nursery and two 4 MiB old spaces, this is approximately 13.57 MiB before Go headers.
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

## Typed foundation

`NewWithTypes` deep-copies validated `Descriptor` layouts; `AddTypes` atomically
appends immutable layouts. Struct descriptors enumerate exact payload-relative
reference offsets. Array descriptors supply element width (1/2/4/8/16 bytes) and
whether elements are 32-bit references. `NewStruct`/`NewArray` zero-initialize
payloads. Null and low-bit-tagged i31 values are preserved and not traced.
These are pointer maps, **not** complete Wasm subtype/nullability checks: the
runtime adapter must enforce concrete reference assignment and initialization.

`Payload` is a trusted mutable borrow of logical payload bytes, never the header
or padding. Any collection invalidates the borrow. Raw reference writes require
value validation and `Remember(parent)`; `SetRef`/`ArraySetRef` do both reference
validity checking and the barrier. `RefField`/`SetRef` index the descriptor's sorted
reference-slot list for structs, not the full Wasm field list. `WriteBytes` rejects
writes overlapping reference slots. Generic `NewObject`/`Word` convenience helpers
remain only for graph tests, and may register shapes/allocate setup metadata.

Typed tests cover mixed scalar/reference/vector payloads, packed/numeric/reference
arrays, descriptor ownership/atomic append, i31 roots/edges, raw-write barriers,
and exact length preservation across repeated copies. A header-compatible layout
does **not** yet make this collector a drop-in Wago adapter.

Typed allocations whose physical stride exceeds the entire nursery capacity
allocate directly into the active old semispace, with `Old|Large` flags. They
still move during Full; there is no pinning promise. Normal nursery exhaustion
does not silently spill into old space. Trusted constructors must Remember an
old object after initializing references through borrowed bytes. `ObjectBytes`
provides a full native-Size lease for adapter use, but never authorizes changing
layout/header metadata; `IsYoung` and `LiveObjects` expose basic adapter state.

Collection no longer allocates a dense offset-to-forwarding table. Preflight
validates/marks the graph and reserves the total destination extent without
changing source headers. Only after all fallible work succeeds does commit copy
each original object to its disjoint destination, then reuse the source header's
Aux/Forwarded fields for forwarding. The destination retains the original Aux,
so packed-array lengths are never reconstructed from padded Size. Existing roots
and edges are rewritten before retiring source objects. This reduces fixed
backing allocation by about 4.43 MiB for the 9 MiB arena example; it is an exact
backing-size reduction, not an RSS, pause-time, or throughput measurement.

Minor collection records dirty old parents in a deduplicated queue allocated at
construction (at most 1024 entries, 4096 bytes). Both preflight and rewriting
visit only that queue unless it overflows; overflow conservatively falls back to
walking the entire old region in both phases. OOM leaves the queue, dirty bits,
and overflow state intact. Successful minor clears only covered dirty bits; Full
resets all tracking. `FixedBytes` includes queue backing. Future native reference
stores must enqueue or force overflow: setting a dirty byte alone is no longer
sufficient. No throughput improvement is claimed without matched measurements.
