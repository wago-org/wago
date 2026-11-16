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

## Bounded loop object resolution

The experimental AMD64 backend can retain one fully checked array-object
resolution across a straight-line, call/allocation-free loop. Per-access bounds
checks and payload loads remain. A bounded scanner rejects unsupported control,
reference writes, earlier trapping operations, safepoints and instrumentation
modes; raw addresses cannot survive collection or reentry. This is a general
compiler optimization, not a change to the collector or evidence that allocation
elimination meets the conventional-collector gate.

An exploratory same-host comparison against `227dfd6` used Go 1.27.1, default
runtime configuration (no `GC_PROOFS` override), 100 warmup invocations and nine
batches of 32 invocations per fresh process. Three rounds alternated process
order. The candidate was the loop-hoist implementation plus its lifetime guard
(`a6f23f7` and `a4ed011` before integration). Array-copy checksum process medians
fell from 0.680–0.712 ms to 0.310–0.385 ms; scalar-copy-loop medians fell from
1.718–1.857 ms to 1.298–1.396 ms. Struct results overlapped (roughly 4.0–4.2 ms).
All result checks passed. Raw samples are in
`bench/experiments/gc-v8/loop-resolver-exploratory.json`.

This small shared-host experiment did not rerun V8, measure compilation costs,
or establish parity. It is distinct from the earlier allocation-elision-off
baseline comparison; do not combine the two into a claimed V8 speedup.

## Survivor feedback correction

Tenuring policy now measures occupied survivor capacity rather than all copied
bytes: promotion copies into old space must not look like a full survivor
semispace. Nursery growth still receives total copied traffic. Deterministic
regressions cover promotion-only recovery, real survivor pressure, and large
objects tenured in place. Heap capacities and maximum tenuring age are unchanged.

Three alternating fresh-process launches per workload compare `3a8ac1f` with
`3249bea` (the same policy patch before integration), with allocation elision off,
default sizing, real collections, and retained-value checks. Median invocation
times changed by about 0% for structs, -11% for arrays, -13% for fill, and -12%
for the retained tree. Tree process peak RSS fell from 67.0 to 22.4 MiB and timed
promotion fell from 7.18 MiB to zero, at the same 396 minor collections. Array
minor counts increased while execution time decreased, showing policy and
automatic nursery growth interact. No full collections occurred. Process RSS
includes compilation and the Go runtime; these are not pause measurements.
See `bench/experiments/gc-v8/tenuring-summary.md` and its raw results. V8 was not
rerun in this comparison, and the overall acceptance gate remains unmet.

## Compilation-cost check

Five fresh-process rounds compared original `227dfd6` with the loop candidate:
64 warmup compilations, then 512 Compile+Close iterations, Go 1.27.1. Bulk-copy
code size stayed 5097 bytes and allocation cost stayed approximately 285
allocations / 77.8 kB per compile; struct code size fell 4020 to 3956 bytes,
with approximately 253 allocations / 66.2 kB unchanged. Latency was noisy
(bulk-copy medians 170 to 176 microseconds, struct 146 to 132), so no reliable
compile-latency improvement is claimed. Raw data and the standalone probe are
`loop-compile-results.json` and `compile-cost.go.txt` in the experiment directory.
The policy probe is `policy-probe.go.txt`, copied into `src/wago` as a temporary
`*_test.go` for reproduction; set `GC_CASE` and run `TestGCPolicyProbe` from that
package, then remove it. Probes are excluded from normal builds.

## Final integrated V8 gate

The integrated implementation was rerun against Node 24.22.1 / V8
13.6.233.17-node.58 on the same shared EPYC host, three alternating fresh-process
launches per workload, allocation elision off, identical checksummed binaries.
All 36 processes returned correct results. Median candidate/V8 invocation-time
ratios were: struct **4.371x**, array **1.259x**, fill **0.973x**, bulk-copy checksum
**1.842x**, scalar-copy checksum **8.913x**, retained tree **3.089x**.
Only fill met the observed <=1.15 gate. The requested performance target is
**not achieved**; this is a tested experimental checkpoint, not a completed
V8-competitive collector. Raw results, process ranges and binary hash are in
`bench/experiments/gc-v8/integrated-*`; correctness coverage is recorded in
`validation-20261116.md` there. Future work must address native allocation/access
costs and broader workloads, not hide these misses behind an aggregate score.
