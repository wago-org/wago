# GC throughput experiment

## Goal and status

The experimental goal is a conventional collector whose end-to-end execution
is no more than 15% slower than V8 on comparable WasmGC workloads, followed by
optional allocation-elimination and bounded-lifetime optimizations. This branch
has **not achieved that goal**. A geometric mean is not a substitute for the
per-workload gate, and an allocation-eliding loop is not collector evidence.

## Baseline architecture

Retain the existing pure-Go generational collector as the working baseline:

- Bump-allocated Eden, two survivor semispaces, adaptive tenuring, and a bounded
  paged old/large-object heap.
- Stable compact handle references; moving young objects changes their handle
  entries rather than every reference to them.
- Precise roots at supported allocation safepoints, old-to-young remembered
  cards, and synchronous full tracing/sweeping.
- A separate fixed-block incremental mark/sweep Tiny profile. Throughput-only
  changes must not add unnecessary metadata to this profile or bypass it.

General GC execution without validated native frame-root publication currently
uses a collection-disabled bounded fallback. No benchmark of that fallback may
be presented as a successful reclaiming collector. Retained allocation workloads
must demonstrate actual minor/full collections and validate retained values.

The handle model deliberately buys stable identity and simple relocation at the
cost of indirect object resolution and metadata. V8's direct compressed-pointer
model has a different cost profile. Replacing the representation is not a local
allocator edit: it would require precise updating roots, reference-field and
host-token handling, table/global boundaries, snapshot support, and all native
backends to agree on relocation.

## Changes in this experiment

1. Reset full-GC mark bytes only for conservatively occupied handle blocks when
   the heap is sparse. Synchronize native nursery publications into the index
   before reset. Dense heaps retain the flat clearing path. This addresses
   historical high-water memory scanning, not the allocation microbenchmark gap.
2. Resolve a traced object's descriptor and payload once rather than repeatedly
   decoding its handle and copying the descriptor. Both full and minor tracing
   retain the original reference order and filtering.
3. Do not allocate Tiny's color table in the immutable Throughput profile.
   Throughput continues using its mark table; Tiny continues using colors.
4. Load native generation bytes directly instead of extracting them with
   shift/mask sequences, and remove overwritten type-map loads from struct
   construction. The same runtime checks remain in place.

## Measurement rules

- Use identical Wasm binaries and verify checksums and result values for both
  engines. Separate cold compile/instantiate from steady-state execution.
- Report exact engine revisions, Go and V8 versions, CPU, flags, warmup, sample
  counts, memory limits, and the direction of the ratio.
- Include wasm.fyi's retained-object performance variants, not only its small
  throwaway probes; also use retained reference graphs, mutation/barriers,
  mixed lifetimes, large arrays, and repeated old-heap reclamation.
- Report per-workload wall time and memory, not just collector pause reductions
  or geomeans. Charge deferred allocator work to the following allocations.
- Capture a conventional-baseline run with allocation elimination disabled
  (`WithOptimization("dead-gc-new", false)`) separately from defaults. This
  switch is not a claim that every compiler value proof is disabled.
- Shared-host exploratory timings identify hypotheses; they are not a
  cross-machine claim of parity or a merge qualification.

## Proof layer above the nursery

Keep proofs optional and fail closed to normal allocation. Useful candidates
are same-block scalar replacement and bounded, no-escape fixed-frame storage.
A proof must account for returns, globals/tables, writes into older objects,
unknown calls, host reentry, exception edges, identity observations, and values
carried around loops. Bound analysis work deterministically; when the budget is
exhausted, keep the ordinary collector path. Never rewind a shared nursery
across a call or host reentry merely because one local value appears dead.

## Acceptance gate

Before describing this as complete: all supported GC correctness and stress
suites must pass, including checked-token boundaries, moving roots, barriers,
allocation-failure rollback, host reentry, official GC conformance, TinyGo, and
supported architectures. Publish any missing platform coverage explicitly.
Then require each selected workload to meet the V8 ratio gate without hiding
memory inflation, disabling collection, changing heap semantics, or replacing
observable allocation with a checksum-specific optimization.
