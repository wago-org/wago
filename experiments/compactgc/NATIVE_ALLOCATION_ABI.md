# Compact native allocation ABI2 (experimental)

ABI2 extends the separate Layout16 compact collector view from56 to72 bytes:
authoritative nursery cursor address at56, nursery end at64, enabled flag at68.
The instance prefix stays32 bytes. ABI1 native artifacts must be rejected;
helper-layout receipt1 is a separate capability and is unchanged.

This exposes no allocation counter. A complete published start is counted by
Stats/retirement exactly once, including dead nursery objects. Native code must
validate capacity, alignment, capability and immutable layout before its first
write. It initializes the complete compact header, payload and padding, publishes
the start byte, then advances both actual and view cursors without calls, traps
or safepoints. This is serialized with all collector/host execution. Old object
addresses may not survive helper fallback or any other safepoint.

EveryAlloc, stress nursery/barriers, failure-injection, verify/poison, disabled
collection/moving nursery, closed heaps and unsupported targets expose no writable
cursor. ForceMajorEveryMinor alone does not disable the capability. Compiler
admission separately excludes interruptible functions and unsupported shapes.

First emitter scope is bounded pointer-free scalar structs, not reference fields,
arrays, SIMD or packed fields. The ABI is not an alternative16-byte native object
header: compact structs store only TypeID4 and descriptor-aligned payload.

Source tests cover simulated native publication mixed with helper construction,
minor/full retirement, nondestructive exact accounting, copied-map/backing checks,
ABI1 rejection, EveryAlloc disabling and Close clearing.

Go1.27.1 qualification of1e8588a0: full compactgc passed0.011s;
compact_reads-tag native TestCompact passed0.003s; default empty-state/footprint
selection passed0.007s. Native emitted allocator qualification is separate.
