# Future eight-byte alignment: bounds and copy design

This is a design note, not an enabled collector mode. The implemented prototype
retains sixteen-byte object alignment. No performance measurement is implied.

## Dense queue need not double

Let N be nursery bytes, O the capacity of **each** old semispace, and the existing
queue capacity Q = floor((16 + N + 2O) / 16). For eight-byte-compatible heap sizes,
Q >= floor(O / 8). The current promote-all collector reserves destination capacity
before marking or enqueueing a unique source. Each reservation consumes at least
eight bytes. Thus, even on a trace that subsequently fails:

    count <= floor((destinationLimit - initialDestinationBump) / 8)
          <= floor(O / 8)
          <= Q

A partially occupied active old space in a minor collection only tightens this
bound. Duplicate sources reserve once; old parents examined in a minor are not
enqueued as copied objects. OOM returns before enqueueing the object that would
exceed capacity. The scratch queue therefore does **not** inevitably require an
additional four bytes per eight-byte arena granule. An earlier estimate of a
mandatory extra 25% arena backing was overly conservative.

Reprove the bound before changing promote-all semantics, queueing noncopied
objects, adding survivor spaces, or moving capacity reservation after enqueue.
Mark bits must still distinguish every eight-byte source start, doubling that
small bitmap versus the current sixteen-byte granularity. Start/dirty maps are
already indexed by offset/8.

## Mixed SIMD alignment without gap objects

At object offset x with compact header width h and payload alignment a:

    payloadBase(x) = alignUp(x + h, a) - x
    stride(x)      = alignUp(payloadBase(x) + payloadBytes, 8)

A sixteen-byte-aligned payload can therefore live in an eight-byte-aligned
object without a separate gap object. Its payload offset and physical stride may
change when it moves. Preflight must reserve the **destination** layout at the
current destination cursor in the same order used during commit, with checked
64-bit arithmetic and explicit capacity checks.

Commit must copy the compact header and exact logical payload separately,
clearing destination padding as necessary. Copying the source physical stride
is wrong when source and destination alignment differ. Read original type and
array length before replacing the source type word with forwarding data.
Destination scans and public payload access derive the layout from the destination
address. Source start-map retirement already avoids interpreting forwarded
headers, and duplicate-root idempotence still relies on disjoint source/target
occupied ranges.

Required tests include both SIMD alignment residues, alternating mixed-width
objects, source stride larger/smaller than destination, exact-capacity OOM,
duplicate roots/cycles, packed array lengths, and memory/accounting invariance on
failed preflight. This work remains deferred until the adapter boundary is ready.
