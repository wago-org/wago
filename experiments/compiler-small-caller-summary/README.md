# Small-module caller summary experiment

The serial hint scan already classifies ordinary and non-inlineable calls. Reuse
those events for modules below the existing 64 KiB body-size threshold instead of
re-decoding calling bodies while building inline plans. The >=64 KiB dense path
retains its existing threshold, capacity growth and plan semantics.

Small modules allocate summary objects only when the existing decoder first sees
a direct or blocked call opcode. Entirely call-free modules allocate neither
optional object; no additional body scan is needed. They retain sorted sparse
function ranges only for bodies with
calls, preserving duplicates and first-call order. Optional storage is capped at
64 KiB including headers, backing capacities, and the old plus new arrays during
a growth copy. This is not an allocator/RSS bound: runtime allocation metadata and
garbage-collection retention are outside that accounting. In particular, prior
replaced arrays may remain uncollected (including under conservative GC), so
cumulative allocations and actual live heap/RSS can exceed 64 KiB. The bound
applies to currently reachable backing storage plus a single growth copy only. Overflow drops the
summary and preserves the original body-scanner fallback. AST bodies, parallel
hint scans, missing entries and disabled inlining retain existing fallbacks.

Ranges publish only after successful complete hint scanning. The summary is owned
by one compilation and never reused across mutable modules or compilations.
Validation, error offsets, inline eligibility and code-generation policy are not
changed. Sparse lookup adds a binary search and small-module recording has a cost;
no compilation gain or correctness qualification has yet been measured.
