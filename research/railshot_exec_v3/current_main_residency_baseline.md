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
