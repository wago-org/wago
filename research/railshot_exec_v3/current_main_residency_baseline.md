# Current-main residency baseline

Date: 2026-09-07

This captures the first Railshot R3 execution-performance baseline on the exact
branch base, before changing allocation or code-generation policy. The only
branch-side code active during these measurements is opt-in statistics
collection; codegen-neutrality tests verify that enabling statistics does not
change emitted code for their covered shapes.

## Environment

- Source base: `a07de0973191efab1d32677eff527952c7f9cdd2`
- Branch plan commit: `f08c9611810ee699345350a0ad17c41528fb73c1`
- Host: Apple M4 Max, Darwin arm64
- OS: Darwin 25.6.0 (`RELEASE_ARM64_T6041`)
- Go: `go1.26.5 darwin/arm64`
- Execution: `GOMAXPROCS=1`

## Regional-residency debt

Collected with:

```sh
go run ./bench/cmd/explain corpus/blake-as.wasm
go run ./bench/cmd/explain corpus/blake3sum.wasm
```

| Workload | Candidates | Activations | Loads | Pressure misses | Evictions | Dirty writebacks | Final transfers | Max active |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| blake-as | 39 | 49 | 21 | 132 | 15 | 13 | 34 | 18 |
| blake3 | 37 | 55 | 19 | 166 | 21 | 17 | 34 | 18 |

Both functions reported zero ordinary allocator spills and reloads. The lease
telemetry therefore exposes substantial local-residency contention that the
existing spill counters do not describe.

## Execution baseline

Collected with ten 500 ms samples per benchmark:

```sh
GOMAXPROCS=1 go test -run '^$' \
  -bench '^BenchmarkExec/(blake-as\.hashN|blake3\.blake3_hash)$' \
  -benchtime=500ms -count=10 -benchmem
```

| Benchmark | Median | Allocations | Calls per batch |
| --- | ---: | ---: | ---: |
| `BenchmarkExec/blake-as.hashN` | 392,746 ns/op | 0 | 6 |
| `BenchmarkExec/blake3.blake3_hash` | 240,135 ns/op | 0 | 8-9 |

The execution medians are reference points, not proof of a change. Future
candidate comparisons must use alternating paired samples against this exact
source base or a newly recorded matched baseline.

## Full compilation baseline

Collected with eight 300 ms samples per benchmark:

```sh
GOMAXPROCS=1 go test -run '^$' \
  -bench '^BenchmarkCompileFull/(blake-as|blake3)$' \
  -benchtime=300ms -count=8 -benchmem
```

| Benchmark | Median | Native code | Heap bytes | Allocations |
| --- | ---: | ---: | ---: | ---: |
| `BenchmarkCompileFull/blake-as` | 357,142 ns/op | 11,404 B | 45,672 B/op | 201 |
| `BenchmarkCompileFull/blake3` | 994,129 ns/op | 32,064 B | 75,896 B/op | 246 |

The first blake-as sample reported 45,748 B/op; the other samples reported
45,672 B/op. Native-code sizes were stable across all samples.

## Initial interpretation

The ARM64 interval region reaches its 18-register limit in both workloads.
Pressure misses outnumber activation loads by roughly 6.3x for blake-as and
8.7x for BLAKE3, while dirty evictions remain comparatively small. The first
policy experiments should therefore distinguish avoided future reads from
writeback cost and should be evaluated in shadow mode before changing emitted
code.

## Bounded event-tape qualification

The codegen-neutral event-tape slice records 1,076 events for blake-as and 1,045
for BLAKE3, with no cap overflow. Both retain the exact baseline native sizes:
11,404 B and 32,064 B respectively.

Eight alternating 300 ms full-compilation pairs against the exact base produced:

| Benchmark | Base median | Event-tape median | Delta | Heap delta | Allocation delta |
| --- | ---: | ---: | ---: | ---: | ---: |
| `BenchmarkCompileFull/blake-as` | 334,176 ns/op | 340,110 ns/op | +1.78% | +10.6% | +2 |
| `BenchmarkCompileFull/blake3` | 977,017 ns/op | 1,005,384 ns/op | +2.90% | +6.4% | +2 |

This remains below the phase ceilings of +25% compile latency and +20% compiler
memory on both target workloads. The reusable tape starts with a measured 1,152-
event reservation and grows only when a function requires it; the hard cap stays
32,768. The event tape is guidance-only at this stage and cannot affect generated
code.

## Shadow-planner qualification

The allocation-free shadow planner reports:

| Workload | Candidates | Versions/segments | Profitable | Reads/defines | Loads avoided | Pressure debt | Max live | Fail-soft |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| blake-as | 37 | 476 | 474 | 601 / 470 | 595 | 17,642 | 37 | 0 |
| blake3 | 37 | 469 | 467 | 580 / 464 | 575 | 18,430 | 37 | 0 |

The high overlap debt and 37 simultaneously interesting locals explain why the
current 18-register ARM64 lease pool sees pressure misses despite few dirty
writebacks. These are shadow estimates, not runtime savings.

Six alternating 300 ms compilation pairs against `a07de097` produced median
deltas of +3.50% for blake-as and +1.23% for BLAKE3. Heap deltas remain +10.7%
and +6.5%, with three additional allocations and identical native code sizes.
This remains within the phase ceilings.

## Rejected active-policy experiments

Two default-off ARM64 experiments consumed the event tape and were removed after
measurement:

1. Per-version final-read ownership transfer was approximately flat on blake-as
   but made BLAKE3 about 14% slower.
2. Restricting the policy to farthest-next-use admission and eviction made
   blake-as about 18% slower and BLAKE3 about 46% slower across six alternating
   400 ms pairs.
3. Preferring clean homes among otherwise eligible eviction victims produced
   sub-2% timing movement, but it did not remove a single dirty writeback. It
   instead raised blake-as from 15 to 19 evictions and BLAKE3 from 21 to 26,
   adding the same number of activation loads. The apparent timing movement was
   rejected as noise-amplifying churn.

The second form also reduced calls completed per benchmark batch, corroborating
the latency regression. The full ARM64 instruction corpus passed after a
fail-soft stale-lease repair, so correctness was not the reason for rejection.
The result instead shows that next-use distance alone is a poor proxy for
physical cost: it ignores dirty-home writes, expression-tree register demand,
and the benefit of keeping stable working-state locals resident. No code or
optimization flag from either rejected experiment remains on the branch.

## ARM64 lease-budget sweep

An experimental increase of the interval-region budget from 18 to 19 passed the
ARM64 backend suite and `TestCorpusSemanticExec`. Six alternating 400 ms pairs
in explicit-bounds mode showed small execution improvements: approximately 0.5%
on blake-as and 1.1% on BLAKE3. The physical and size movements were clearer:

| Workload | Pressure misses | Activation loads | Native function bytes |
| --- | ---: | ---: | ---: |
| blake-as, 18 leases | 132 | 21 | 5,308 |
| blake-as, 19 leases | 108 | 21 | 5,192 |
| BLAKE3, 18 leases | 166 | 19 | 5,560 |
| BLAKE3, 19 leases | 135 | 23 | 5,428 |

This is an 18-19% pressure-miss reduction and a 2.2-2.4% native-size reduction.
Twenty leases failed BLAKE3's semantic oracle on the 63-byte vector, establishing
that the remaining three registers in the ordered pool are a required transient
floor rather than spare capacity. The floor is encoded in a regression test.

The wider Linux ARM64 guard-page CI gate subsequently found the missing resource
constraint: explicit bounds reserves X27 for the memory size. That leaves only
two scratch-capable tail registers at 19 active leases. In that configuration,
`blake-as-simd.hashN` returned `2841881934` instead of the golden `26497025`.
Signals-based bounds, which leave X27 free and preserve three transient
registers, remained correct. The final policy therefore keeps explicit bounds at
18 leases and permits 19 only for signals-based code. The earlier explicit-mode
timings above describe the rejected configuration, not a retained gain.
`TestCorpusDifferential` covers both modes and the policy split has a direct unit
test.

Eight signals-based samples per variant against exact main confirmed that the
mode-specific nineteenth lease is useful and correct:

| Workload | 18-lease median | 19-lease median | Delta |
| --- | ---: | ---: | ---: |
| blake-as | 372,114 ns/op | 366,308 ns/op | -1.56% |
| BLAKE3 | 231,678 ns/op | 228,548 ns/op | -1.35% |

## AMD64 effective-capacity admission

Native measurements ran on an AMD Ryzen 7 7800X3D under Linux with CPU affinity
pinned to CPU 2. The AMD64 interval region nominally permits nine leases, but
both BLAKE kernels peak at eight because one ordered register is unavailable.
Previously `claimIntervalReg` returned immediately in that state and never
reached its existing score-gated eviction selector.

Allowing the no-free-register case to fall through to that selector produced:

| Workload | Pressure misses | Native function bytes | Paired execution median |
| --- | ---: | ---: | ---: |
| blake-as, base | 662 | 6,144 | 674,421 ns/op |
| blake-as, candidate | 562 | 6,061 | 667,397 ns/op (-1.04%) |
| BLAKE3, base | 636 | 5,977 | 358,137 ns/op |
| BLAKE3, candidate | 468 | 5,588 | 357,056 ns/op (-0.30%) |

The candidate passed the full AMD64 backend suite and semantic execution corpus.
A three-pair, 46-workload screening run showed no material non-BLAKE regression;
short-run deltas stayed within -1.0% to +1.4%. No new unsafe operation, register,
or state transition is introduced: the change reaches the same eviction routine
already used when all nine nominal slots are active.

## AMD64 explicit-bounds last-choice R8 lease

In explicit-bounds mode, the call-free, control-free, bulk-memory-free interval
region can use R8 after the ordinary regional pool is exhausted. RAX, RCX, and
RDX remain outside the pool for multiply, divide, shift, and return lowering;
R8 is deliberately last because its encodings can require an extra REX prefix.

Eight focused samples per variant on the Ryzen host compared this policy with
`9955179c`:

| Workload | Nine-lease median | R8 median | Delta |
| --- | ---: | ---: | ---: |
| blake-as | 667,059 ns/op | 663,017 ns/op | -0.61% |
| BLAKE3 | 356,575 ns/op | 334,734 ns/op | -6.13% |

Two opposite-order, three-sample full-corpus screens bracketed the 46-workload
geometric mean between +0.11% and -1.05%. BLAKE3 remained 5.9-6.4% faster in
both screens; short sub-50 ns microbenchmarks moved with run order.

Compilation allocations and heap bytes were unchanged. Median compile latency
moved from 547,557 to 549,063 ns/op for blake-as (+0.28%) and from 1,486,591 to
1,493,565 ns/op for BLAKE3 (+0.47%). Module native code grew from 11,245 to
11,293 bytes (+0.43%) and from 31,554 to 32,194 bytes (+2.03%), remaining below
the 10% phase ceiling. Pressure misses fell from 562 to 448 for blake-as and
from 468 to 355 for BLAKE3. The native AMD64 backend suite and full explicit-
bounds semantic execution corpus passed before retention.

The Windows AMD64 guard differential subsequently exposed the mode-specific
resource conflict: signals-based bounds lowering still uses R8 as fixed scratch,
and leasing it corrupted `blake-as.wasm.hashN`. Native AMD64 guard testing
reproduced the same wrong result. The retained policy therefore caps the region
at nine leases in signals mode and permits the tenth R8 lease only with explicit
bounds; the focused speedups above measure that explicit-bounds configuration.

## Physical-debt policy screening

Three bounded ARM64 policy sweeps tested whether small dynamic costs could make
the existing whole-local lease policy phase-sensitive without retaining a
version plan. None met the retention threshold:

1. Adding a cost to dirty eviction was catastrophic at large values. The only
   plausible value (`+4`) was mixed across ten alternating samples: blake-as was
   approximately 0.3% slower while BLAKE3 was approximately 1% faster. This is
   below the phase threshold and does not transfer across the two ARX kernels.
2. Suppressing the first four reads after a pressure eviction changed only two
   BLAKE3 activations and one load. Ten alternating samples were approximately
   flat on blake-as and 0.3% faster on BLAKE3, with eight additional native
   bytes. The movement is noise-sized.
3. Crediting a newly defined dirty version by even one score unit increased
   BLAKE3 evictions from 21 to 39 and dirty writebacks from 17 to 35. Larger
   credits regressed execution materially. Without a future-use proof, rotating
   leases merely converts pressure misses into stores.

All experimental code and environment knobs were removed. These results rule
out scalar adjustments to the whole-local hotness score; the next active policy
must carry bounded per-version evidence.

An opt-in transition shadow now supplies that evidence without changing emitted
code. It scores each bounded version segment by avoided loads/stores minus
fixed-register, synchronization, and transient-pressure debt, then simulates a
one-unit-hysteresis lease matcher. Detailed simulation runs only when codegen
statistics are requested; ordinary compilation retains the cheaper aggregate
shadow.

On the M4 Max explicit-bounds baseline it predicts:

| Workload | Admissions | Evictions | Reloads | Dirty writebacks |
| --- | ---: | ---: | ---: | ---: |
| blake-as | 25 | 6 | 4 | 5 |
| BLAKE3 | 26 | 7 | 3 | 5 |

Those transition counts are far below the rejected definition-credit policy's
39 evictions and 35 writebacks on BLAKE3. They are a planning signal, not an
execution-speed claim; no transition decision is consumed by code generation.
Four alternating ordinary-compilation pairs showed noise-sized timing movement,
unchanged allocations and native bytes, and only 16-28 additional retained heap
bytes from the expanded pointer-free summary.

## Rejected active transition replay

The first attempt to consume the transition model retained a bounded sidecar of
per-local-version benefits. Code generation decremented the current benefit on
reads, used dirty-home cost as an eviction tie-breaker, and required one unit of
hysteresis before replacing a lease. A no-contention backend fixture immediately
exposed an over-broad version: native code grew from 412 to 564 bytes. Restricting
the policy to functions whose candidate count exceeded the safe lease budget
restored that fixture, but the intended BLAKE cases still failed the physical
gate:

| Workload | Baseline kernel bytes | Candidate kernel bytes | Baseline pressure misses | Candidate pressure misses |
| --- | ---: | ---: | ---: | ---: |
| blake-as | 5,308 | 7,600 | 132 | 679 |
| BLAKE3 | 5,560 | 7,804 | 166 | 695 |

Candidate activations also fell from 49 to 33 for blake-as and from 55 to 29
for BLAKE3. The offline score is an aggregate diagnostic: consuming it as a
per-read countdown double-charges future work and leaves too many useful values
memory-resident. The experiment failed before timing, and all active sidecar and
code-generation changes were removed. A retained policy needs exact transition
decisions or a codegen-native pressure model; the aggregate benefit is not a
safe control signal.

ARM64's effective-capacity admission was also tested independently by falling
through to its existing safe eviction selector when the nominal lease count was
below the cap but no physical register was free. Unlike the retained AMD64
change, this did not alter either BLAKE kernel: activation, miss, eviction,
writeback, transfer, and native-byte counts were identical. The ARM64 misses in
these kernels therefore occur at the actual lease ceiling, not because transient
register ownership makes the effective capacity smaller. The no-effect change
was removed.
