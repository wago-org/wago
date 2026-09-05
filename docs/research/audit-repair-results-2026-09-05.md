# Audit repair results

Implementation of the [accepted plan](performance-security-repair-plan-2026-09-05.md).
Baseline: `f0f4951138b8`. Host: Linux/amd64, Go 1.27.1. Initial serial samples use
`GOMAXPROCS=1`, `-cpu=1`, 100 ms, five samples. Raw local captures are in
`/tmp/wago-audit-repair`; final release measurements need the plan's longer A/B
and native-platform runs. The initial Go test-binary build exceeded 30 seconds;
process inspection showed compilation followed by a 12.8-second benchmark suite,
not an individual Wasm compile over 30 seconds.

## Background cancellation watcher

The new inert-context allocation test failed before the fix with 7 allocs/op for
Background, TODO, and WithoutCancel. All now take the shared no-op path before
callback/channel allocation. Nil contexts remain allocation-free. The focused
cancellation, deadline, and invocation-context tests pass. The new benchmark
reports zero bytes and allocations for each inert context. No native polling or
scheduler transition was changed. Native non-Linux and TinyGo tests are pending.

## Structured custom-section budgets

Name and branch-hint readers now share the parent budget. Exact-capacity
structured vectors reserve storage and allocator rounding before allocation;
strings and the owned raw payload are charged separately. Strict section checks
remain in place. A valid 3 MiB module name now decodes with a 16 MiB metadata
budget and rejects with an 8 MiB budget. The former malformed fixture now checks
that the structural parser rejects it rather than codifying a payload multiplier.
The complete wasm package tests pass. Structured-name benchmark captures are in
`decode-budget.txt`; large valid names could not be benchmarked successfully on
the old implementation. Type-slab changes are a separate repair.

## Signature validation and streaming artifacts

Signature validation compares each exact descriptor to its ABI type without
building temporary slices. Indexed artifact signatures write the index directly;
non-indexed signatures stream their values. Public signature access still copies.
The focused signature, codec, artifact, and Compiled tests pass.

Five serial 100 ms samples on the same host give these medians:

| Benchmark | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| WriteCompiledImportedModule | 316910 | 147912 | 263074 | 154207 | 12829 | 21 |
| ValidateFuncSignature | — | 14.47 | — | 0 | — | 0 |

These short sequential samples confirm the allocation reduction; final
interleaved timing and native-platform checks remain pending.

## Linux interrupt generations and ARM64 ordering

Token issuance renews its cookie and resets its sequence only after all requests
have drained. Shared owners retain their token at the generation boundary.
Cookie initialization and renewal share the request lock and atomic publication.
The ARM64 handler acquires published code/memory registry entries and publishes
trap acknowledgement with release ordering. The existing fixed registry sizes
and native call path are unchanged.

Focused interrupt/deadline/lifetime tests pass, including under `-race`.
The Linux/arm64 runtime test binary cross-builds. Native ARM64 execution remains
required; the race detector cannot prove assembly ordering. Signal negotiation
and scan optimization still need their separate measurements.

## Lazy type slabs

Implicit groups allocate slabs only as needed, growing from 16 to at most 1024
subtypes per slab. Explicit groups allocate only their own exact subtype vector.
Each reservation covers actual capacity and rounding. Singletons still expose
capacity one; at most 1023 unused subtype slots can remain in the final slab.
All wasm tests pass, including an explicit-only 1000-group fixture within a
400000-byte quota.

The 100000-singleton control changed from 17605392 B/op and 6 allocations to
17611408 B/op and 108 allocations. The extra 102 allocations are bounded slab
allocations, not one allocation per type. Short samples improved from about
11.3 ms to 10.0 ms; final A/B timing is pending. The explicit-only 100000-group
case uses 18401040 B/op, with no unused 15.2 MB singleton slab. Small one-group
implicit and explicit cases each use 968 B/op and 6 allocations. Captures are in
`type-slabs-after.txt`. The allocation/retention tradeoff is explicit here because
reducing unused storage does not reduce every benchmark's allocation count.

## Concurrent first publication

Cache pointers use atomic first publication; the cache lock serializes memo and
snapshot construction. Readers acquire memo/snapshot pointers, including
reflection sidecars. Raw pointer fields keep Compiled copyable and retain the
existing compiler-owner allocation. A losing cache CAS allocates only a small
cache candidate, never a competing metadata clone.

A fresh hand-built Compiled now passes concurrent instantiation, invocation,
signature reflection, and CodeSize under `-race`. Focused Compiled/snapshot/code
ownership tests pass. Snapshot-copy and call-control measurements are in
`snapshot-publication.txt`; the separate pre-clone quota repair is still pending.

## Remaining work

The other work groups and full release gates in the accepted plan are pending.
This file is updated with each repair; a partial result is not a merge approval.
