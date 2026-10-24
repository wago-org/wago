# Parallel Throughput collection

**Status:** correctness qualification passes; the PR remains a draft pending
survivor-minor and one-worker performance tuning described below.

`GCConfig.Workers` and `wago run --gc-workers N` opt standard Go Throughput
collectors into parallel collection. Zero and one use the original serial
algorithms, allocate no pool/claims, and start no helpers. Values two through
eight request participants including the caller. Tiny and TinyGo reject values
greater than one. A pool is created only when a collection has enough work,
and retains at most `min(Workers, GOMAXPROCS)` participants at creation.
`Close` stops and joins its parked helper goroutines before releasing backing.
Collectors still require external serialization of mutator/API access.

## Source study and Wago design

The reference is Slipstream's Snap collector in `gc/native.goose`, specifically
`ng_enqueue`, `ng_pop`, `ng_scan`, `ng_trace`, `ng_worker`, `ng_supervisor`,
`ng_sweep`, and `ng_collect_owned` (the local source checkout was
`/home/dev/Projects/slipstream`, commit
`87aef66ef6608d95bd9568af1bed8d3f602f16cc`). Snap has private tracing segments, bounded
shared batches, overflow work bits, atomic forwarding/mark ownership, array
chunks, claimed root ranges, and disjoint block sweeps. Its outstanding-work
counter includes queued and in-flight objects, array chunks, and root ranges.
Private chains do not wake every worker. Reusable helpers park on futex epochs;
collection joins every helper before reclaiming source pages. Small minors
select one participant. Parked supervisors contain Goose worker aborts and
publish acknowledgments; a process-wide allowance bounds native helper threads.

Wago has stable compact handle references rather than Snap's moving offset
references. The caller already stops its domain's mutator and publishes exact
roots. Copying changes handle locations, never references inside graph edges.
The Throughput allocator, remembered cards, free lists, and transactional
promotion admission share coordinator-owned state. The Tiny collector has a
separate bounded incremental state machine.

The port follows those ownership boundaries:

- Root callbacks, native frame walks, persistent slots, and remembered-card
  scans execute on the caller. They seed the existing mark stack before dispatch.
  Callbacks need no new concurrency contract. Ownership-edge sources on RISC-V64
  retain serial tracing because their callback interface promises no concurrent
  invocation.
- A separate atomic epoch word per handle elects exactly one scanner and one
  writer of that handle's ordinary mark byte. Seeds are claimed before helpers
  start. Null, i31, free handles, opaque references, and old objects in a minor
  retain their established filtering. Epoch wrap clears claims before reuse.
- Helpers drain private stacks and share batches through a mutex-protected queue
  capped at 4,096 tasks. A full shared queue leaves work in private stacks; no
  edge is dropped and no bitmap overflow mechanism is needed. Reference arrays
  split into disjoint 1,024-element ranges. Private stacks and capture buffers
  retain capacity and may grow when a new work distribution needs more storage.
- An active-worker count includes unpublished private work. An idle helper waits
  on a condition variable while an active helper can still publish. Completion
  requires an empty shared queue and zero active workers, followed by a phase
  join. There is no shared increment or wake for every private edge.
- Dense full sweeps partition handle discovery into disjoint contiguous ranges.
  After joining, the caller frees discovered handles in ascending handle order
  through the existing allocator/card paths. Sparse indexed sweeps keep their
  existing serial algorithm.
- Large promotion batches copy disjoint, already-admitted source/destination
  payloads in parallel, including old-header flags and source poisoning. All
  transactional admission/failure points complete before copying. Only after
  joining does the caller publish handles, counters, generations, and cards.
  Smaller batches keep the existing copy loop.

Go channels, condition variables, and wait groups provide persistent helper
lifecycle and phase joins; no native thread pinning or supervisor goroutines
are needed. Internal helper panics retain Go's process-fatal behavior, like
existing collector invariant panics. This does not introduce recoverable worker
abort/poison semantics. Go's scheduler bounds concurrent execution across pools;
there is no separate process-wide parked-goroutine lease.

Parallel tracing starts at 8,192 handles (young handles for minors), or when an
admitted seed is a reference array with at least 8,192 elements. Parallel copies
require at least 8,192 plans and 1 MiB of copied payload. One available Go
processor selects the serial path even if a pool already exists. These are
performance choices, not correctness conditions. Roots, admission, allocator
mutation, handle publication, and sparse sweeps remain serial, so scaling is
workload-dependent.

No card, existing bitmap, or nursery-mapping implementation was changed. Two
existing footprint tests account for the added worker-config word and trailing
pool pointer (16 bytes per 64-bit collector). Opt-in parallel pools additionally
retain four claim bytes per handle, worker stacks/capture buffers, and a bounded
shared queue. Native handle/view ABI layouts are unchanged.

## Qualification

The checked and native collector suites passed before the change in ordinary,
`wagodebug`, and `wago_regalloccheck` modes. New tests compare full liveness
against the independent shadow tracer for 1/2/4/8 requested workers, check
minor scan counts, cycles, duplicate roots, globals/tables, remembered edges,
survivors, immediate promotion, poisoning, transaction rollback/retry, private
chains, epoch wrap, handle-table growth, small/one-processor fallback, queue
pressure, chunk participation, and repeated close. The compiled
`gc-parallel-graph.wat` fixture retains a shared graph through repeated native
allocation-triggered collections with one and four workers.

Validation commands and results are recorded with the measurements below.
The checkout needed the pinned Core 3 submodule initialized. WABT cannot parse
all Release 3 GC syntax, so the tests used the existing verified official
interpreter at revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa` from the main
checkout. PowerShell needed `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1` because
this environment lacks ICU. An unrelated plugin install test initially returned
`text file busy`; its isolated retry passed. The pre-existing spec-v1/spec-v2
submodule differences were left untouched.

## Measurements

These are five alternating-order before/after pairs on an AMD Ryzen 7 8845HS,
Linux AMD64, Go 1.27.1, `GOMAXPROCS=4`, pinned to CPUs `8,10,12,14`. Those are
four distinct physical cores (SMT sibling pairs are `8-9`, `10-11`, `12-13`,
`14-15`). Final captures ran from 13:08:38 through 13:13:36 UTC on
2026-10-24 under the exclusive global Wago/Slipstream wall-time benchmark lease,
with no concurrent benchmark/build/test jobs from this task. The lease was
released at 13:15:28 UTC. Earlier interrupted/two-core pilots are excluded.

The baseline is main commit `fc6af651c15e286409d9cc117c94181e9ee50ff3` with the
same benchmark fixtures overlaid. It has no worker configuration, so both
baseline worker labels execute serial collection; the table uses its one-worker
label as the reference. The candidate uses the implemented worker setting.
Root admission, tracing, sweep discovery/commit, and minor promotion remain
inside the collection timer; setup/refill is excluded. Compiled-Wasm timings
include invocation, allocation, and collection. Small fixture boxing already
costs one allocation per timed collector call in both builds; parallel scratch
can additionally grow when scheduling creates a new peak distribution.

Times below are medians in microseconds with `[minimum, maximum]` ranges.
Negative deltas mean less wall time than the serial baseline.

| Collection case | Before, serial | After, 1 worker | After, 4 workers | 4-worker delta | Speedup, 1 to 4 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Full: 32,768 nodes, 2 edges/node | 763.2 [754.2, 773.8] | 756.9 [754.9, 801.3] | 344.7 [342.8, 359.8] | -54.8% | 2.20× |
| Full: 32,768 nodes, 32 edges/node | 2241.7 [2215.3, 3019.4] | 2212.1 [2203.0, 2232.7] | 1290.5 [1279.4, 1340.8] | -42.4% | 1.71× |
| Minor: survivor copies, 16,384 nodes | 772.5 [743.3, 790.9] | 774.4 [723.7, 993.3] | 899.0 [878.7, 949.8] | +16.4% | 0.86× |
| Minor: immediate promotion, 16,384 nodes | 790.0 [696.9, 848.8] | 798.2 [711.6, 816.0] | 765.8 [730.4, 793.0] | -3.1% | 1.04× |

Full graph pauses improve **2.20× and 1.71×** from one to four participants.
The survivor-minor case regresses **16.4% versus baseline** (16.1% versus the
candidate's one-worker path). Immediate promotion is close to flat with
substantially overlapping ranges. Parallel collection is therefore not yet
performance-qualified across the minor portfolio.

These controls retain the existing single-worker path. The compiled allocation
mix has no useful scaling on these inputs. Times are median microseconds;
raw files and the JSON summary retain all ranges and samples.

| Existing control | Before | After, 1 worker | 1-worker delta | After, 4 workers | 4-worker delta |
| --- | ---: | ---: | ---: | ---: | ---: |
| Full pointer-free, 90 objects | 1.473 | 1.533 | +4.1% | — | — |
| Full pointer-free, 900 objects | 14.841 | 15.093 | +1.7% | — | — |
| Full pointer-free, 9000 objects | 149.501 | 154.951 | +3.6% | — | — |
| Old heap pause, 1000, 0% survival | 6.878 | 6.754 | -1.8% | — | — |
| Old heap pause, 1000, 50% survival | 11.182 | 11.105 | -0.7% | — | — |
| Old heap pause, 10000, 0% survival | 64.989 | 63.739 | -1.9% | — | — |
| Old heap pause, 10000, 50% survival | 109.872 | 108.274 | -1.5% | — | — |
| Allocation mix, scalar-structs | 74.940 | 75.400 | +0.6% | 76.910 | +2.6% |
| Allocation mix, structs | 3399.451 | 3488.043 | +2.6% | 3497.789 | +2.9% |
| Allocation mix, alternating | 4998.539 | 5235.934 | +4.7% | 5129.953 | +2.6% |
| Allocation mix, alternating-default | 4736.022 | 4656.292 | -1.7% | 4713.562 | -0.5% |
| Allocation mix, alternating-fill | 4950.010 | 5016.150 | +1.3% | 5036.617 | +1.7% |

The pointer-free controls regress 1.7–4.1% with one worker, although the graph
and old-heap controls are flat or faster. The allocation-mix one-worker deltas
range from −1.7% to +4.7%. These results need profiling and another paired
qualification after tuning; correctness preservation does not establish an
unchanged performance envelope.

## Remaining work before readiness

- Remove the survivor-minor regression: tune work admission and the balance of
  tracing/capture merging, copying, and serial publication using pause profiles.
- Recover the single-worker pointer-free controls while preserving the original
  serial hot loops and avoiding helper dispatch costs.
- Repeat ordinary/debug/register-checking GC gates and paired one/four-worker
  measurements after those changes. Preserve transactional failure/retry and
  join-before-reclamation guarantees.

The current implementation is a coherent correctness-tested slice, and is
submitted as a draft because those performance goals remain open. Additional
native platform performance is unmeasured; ARM64/RISC-V64 collector checks here
are compile-only.

## Reproduction and evidence

The [validation record](parallel-collection-results/validation.txt) lists all
commands and prerequisites. Full repository tests pass in ordinary and
`wago_regalloccheck` modes; GC tests additionally pass under `wagodebug` and the
parallel paths pass the race detector. The benchmark module's register-checking
suite and documentation validation also pass.

Build before acquiring the exclusive global Wago/Slipstream wall-time lease:

```sh
python3 docs/research/parallel-collection-results/baseline-overlay.py fc6af651c

go test -overlay=/tmp/wago-parallel-baseline-overlay/overlay.json \
  -c -o /tmp/wago-parallel-baseline-matrix.test ./src/core/runtime/gc/native
go test -c -o /tmp/wago-parallel-after.test ./src/core/runtime/gc/native

go test -overlay=/tmp/wago-parallel-baseline-overlay/overlay.json \
  -c -o /tmp/wago-parallel-wasm-before.test ./src/wago
go test -c -o /tmp/wago-parallel-wasm-after.test ./src/wago
```

Only while holding that lease, with other heavy jobs stopped, run:

```sh
python3 docs/research/parallel-collection-results/measure.py
```

The harness runs five alternating-order pairs: full collection at `300ms`,
minor pauses at `20x` with refill outside the timer, and compiled-Wasm allocation
mix at `200ms`. Choose four distinct physical cores on a different host and
record that affinity. Do not overlap benchmarks with builds or correctness jobs.

CPU-header trailing padding was removed from the raw captures; timing rows
are unchanged. Raw samples: [full before](parallel-collection-results/full-before.txt),
[full after](parallel-collection-results/full-after.txt),
[minor before](parallel-collection-results/minor-before.txt),
[minor after](parallel-collection-results/minor-after.txt),
[allocation mix before](parallel-collection-results/wasm-before.txt), and
[allocation mix after](parallel-collection-results/wasm-after.txt).
The [machine-readable summary](parallel-collection-results/summary.json) includes
all medians, ranges, individual samples, and host/lease metadata.
