# Payload-boundary preparation (compact mode still disabled)

The native collector adapter now exposes private payload-relative accessors.
They return real mutable storage, including the same existing tail padding;
logical host views still cap both length and capacity at logical array bytes.
Headers are values, never synthesized mutable expanded buffers. Ordinary field
and element reads/writes, constructor fills, bulk operations and numeric host
borrows use the boundary. Collector-internal legacy object-offset storage remains
available for the existing handle collectors.

No collector fields, interfaces, configuration or object layouts were changed.
That is not a performance-neutrality claim: default Throughput/Tiny operation
cost must be compared before integrating the refactor.

Correctness qualification with Go 1.27.1:
- Complete native package: pass (2.008s).
- Complete native package with `wagodebug`: pass (2.043s).
- Existing directgc: pass (0.019s); standalone compactgc: pass (0.011s).
- Existing array-data initialization regression caught an initially missed
  physical-header addition in padding clearing; corrected before these passes.
- New empty logical array / short physical header regression rejects the view.

## Remaining compact integration work

A build-selected backend/factory can avoid adding a second pointer or interface
to every Collector, but must be explicit and independently admitted. Compact
payload/header/descriptor resolution must be specialized together. Never call
native `objectPayload` on compact storage. Constructor size checks and handle-
specific clearing/scanning also require backend-aware dispatch. Compact factory
conversion must preserve actual descriptor alignment (not max(Align,8)); the
current direct factory's adjustment exists for its own layout contract.

A compact runtime needs its own configuration/admission and private compiler
receipt. Existing direct ABI artifacts must fail closed, not be silently reused.
SurvivorBytes currently has no new compact heap-budget meaning.

## Microcomparison protocol (not yet executed)

`BenchmarkPayloadBoundary` uses only APIs available before this refactor, so the
identical source can be copied to the parent revision. It covers Throughput and
Tiny typed struct/array get/set, struct/array construction and numeric payload
borrow. Build separate test binaries before timing; use three alternating fresh
process pairs with `-test.run=^$ -test.bench=^BenchmarkPayloadBoundary$`, fixed
`-test.benchtime=100ms`, and allocations reported. Retain raw samples and toolchain,
source and binary hashes. Existing `BenchmarkTypedGCStructAccess`,
`BenchmarkTypedGCArrayAccess` and `BenchmarkArrayConstructors` provide additional
Throughput cross-checks when useful. No performance result is available yet.
