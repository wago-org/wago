# Rejected integer-only shared callback bridge

A diagnostic prototype omitted the V8–V15 save/restore pairs while retaining the
existing GP/root/trap records and complete owned callback portal. It was enabled
only for the pinned integer-only opaque fixture c9310c60; no production admission
proof was added.

The same compiled diagnostic executable ran six processes in order
normal/integer/integer/normal/normal/integer, GOMAXPROCS=1, one second each, with
exact performance(4096) oracle 536739840. Normal mean was 4,277,668 ns and integer
mean 4,278,317 ns: ratio 1.00015, effectively no difference. A follow-up profile
contains inlineHostIntegerScalarPrototypeEnter, integer staged bridge and integer
resume symbols, proving the experiment reached the intended path.

Discarded the prototype rather than adding compiler proof and runtime branching
without a measured benefit. All diagnostic hooks and assembly additions were
removed. The JSON preserves results, experiment source and profile attribution;
raw binary/profile evidence remains private under shared-integer-bridge.
No benchmark capture or local/production publication was made from this source.
The next experiment targets the full portal's method-value wrapper while keeping
normal FP preservation and callback ownership behavior.
