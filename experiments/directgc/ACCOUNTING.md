# Deferred allocation accounting (direct ABI 4)

The experimental direct collector counts every successful allocation, including
objects later found unreachable. Copies, promotions, and repeated Stats calls do
not add allocations. This is accounting only, not allocation elision.

New nursery objects are counted from published start bytes in the current
nursery range. Successful collection commits their count in the existing nursery
retirement walk. Failed preflight, including OOM, commits nothing. Oversized
objects allocated directly in old space increment a separate counter only after
successful initialization. Full/minor retries therefore cannot double-count.

AllocationSnapshot computes pending nursery count and current object count in a
single scan of the arena start map. Its cost is O(arena start-map length), not
O(1). It replaces the existing live-object scan in native Collector.Stats rather
than adding a second traversal. Native Close snapshots the allocation total
before releasing the direct heap; repeated Close preserves that total.

All direct Go helpers and native emitters use this same accounting scheme.
ABI 4 retains the former AllocationCounter field at offset 72 as reserved zero,
never a writable pointer. Old counter-writing native artifacts cannot use this
ABI. There is no public configuration switch or mixed accounting mode. Handle
and Tiny collectors retain their existing accounting. No performance improvement
is claimed without matching benchmark measurements.
