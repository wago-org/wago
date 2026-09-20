# Faster per-range struct fallback

Main baseline: `27c1f066a3e04bc31ca82d6b55ce4977b19c35ee`.
Previous PR implementation: `2e44bd850825cf0268020698f64feaa7b71194bd`.
The candidate is the commit that adds this report; the PR records its exact SHA.

`scanObjectPayloadRange` now tests the unsigned offset difference against a
range span computed before the field loop. One comparison excludes offsets on
both sides of the interval. The existing `end < start` guard remains. Fields
outside the interval skip the reference-kind check. No admission threshold,
chain validation, layout, card storage, or allocation behavior changes.

Bounds parity tests cover inclusive endpoints, unsigned underflow, maximum
uint32 endpoints, reversed bounds, nulls, and numeric fields containing
reference-shaped bits. Sparse shuffled layouts now extend lifecycle,
differential, telemetry, and benchmark coverage. Native and baseline parity
checks pass. This is a performance change, not a collector correctness fix.

## Measurement method

Go 1.22.12, Linux AMD64, Ryzen 7 8845HS, CPU affinity 2, GOMAXPROCS=1,
performance governor, boost enabled. Builds finish before measurements.
No builds, tests, fuzzers, or profiles run during timing. All versions use the
same benchmark and fixture source. The rebuilt final test binary matches the
measured binary byte-for-byte: [hashes](validation/binary-hashes.txt).

The isolated case uses six alternating three-version rounds, five seconds per
sample: main/previous/candidate, then candidate/previous/main. The wider suite
uses six main/candidate pairs with alternating order, 200 ms per case. A further
six three-version rounds at one second recheck narrow and sparse cases.
Benchstat uses six samples per version. Results are synthetic; previous
representative-workload probes did not reach the fast path. No application
throughput gain is claimed.

## Isolated 33-range result

| Version | Median ns/op | Minimum–maximum ns/op | B/op | allocs/op |
|---|---:|---:|---:|---:|
| Main | 132,402.5 | 120,783–205,227 | 0 | 0 |
| Previous PR | 195,706 | 136,662–210,601 | 0 | 0 |
| Candidate | 73,021 | 69,366–76,465 | 0 | 0 |

Candidate reduces time **44.85% against main** and **62.69% against the previous
PR**, both p=0.002. The wide baseline spread is reported, not hidden.
[Raw samples and summaries](isolated-33/summary.json),
[main comparison](isolated-33/main-benchstat.txt),
[previous comparison](isolated-33/previous-benchstat.txt).
The [earlier isolated rerun](../isolated-33-rerun/benchstat.txt) is retained as
historical evidence from before this range-filter change.

## Wider controls and complete collections

| Case | Main ns/op | Candidate ns/op | Change |
|---|---:|---:|---:|
| Ordered scan, 8192 fields, 16 ranges | 80,580 | 37,010 | -54.06% |
| Ordered scan, 8192 fields, 32 ranges | 160,730 | 45,030 | -71.98% |
| Ordered scan, 8192 fields, 33 ranges | 161,600 | 133,600 | -17.29% |
| Complete moving minor, 16 ranges | 63,260 | 18,490 | -70.76% |
| Complete moving minor, 32 ranges | 230,290 | 25,500 | -88.93% |
| Complete moving minor, 33 ranges | 242,130 | 74,210 | -69.35% |
| Complete moving minor, mixed parents, 33 ranges | 253,390 | 77,920 | -69.25% |

Values here are rounded from benchstat; exact samples and medians are in
[full results](full/summary.json). All listed changes have p=0.002. All measured
complete minor-collection medians improve. Collection setup, fresh young child
allocation, cleanup, and verification remain outside collection timing.
All scan cases report 0 B/op and 0 allocs/op. Complete minor collections report
1 alloc/op; sample B/op spans 24–175 on main and 24–72 on the candidate, including
amortized cold stack growth. This is not a claim that complete GC is allocation-free.
[All statistical comparisons, including bytes and allocations](full/benchstat.txt).

## Every slower scan median

| Case | Main ns/op | Candidate ns/op | Change | p |
|---|---:|---:|---:|---:|
| Eight fields, ordered (1 s recheck) | 48.145 | 50.430 | +4.75% (+2.285 ns) | 0.002 |
| Eight fields, reversed (1 s recheck) | 48.075 | 50.325 | +4.68% (+2.250 ns) | 0.002 |
| Array, 1 range | 107.15 | 108.10 | +0.89% | 0.818 |
| Array, 15 ranges | 1465.5 | 1481.5 | +1.09% | 0.394 |
| Array, 16 ranges | 1552.5 | 1590.0 | +2.42% | 0.310 |
| Array, 32 ranges | 3086.0 | 3113.5 | +0.89% | 0.615 |
| Array, 33 ranges | 3177.5 | 3236.0 | +1.84% | 0.699 |
| Mixed parent, 256 fields, 1 range | 232.15 | 242.35 | +4.39% | 0.394 |
| Ordered, 256 fields, 1 range | 235.10 | 235.75 | +0.28% | 1.000 |
| Near ordered, 256 fields, 1 range | 233.15 | 236.60 | +1.48% | 0.589 |
| Sparse references, 33 ranges (200 ms) | 70487 | 72856 | +3.36% | 0.180 |

The sparse-reference one-second recheck is 67.45 us versus 67.32 us, p=0.589:
the slower median did not repeat. The narrow recheck is also about 2 ns slower
than the previous PR. This small absolute cost is retained and disclosed.
Initial narrow samples were +2.86/+3.565 ns; the longer recheck above supersedes
them. Other slower medians are not statistically significant; that does not
establish zero overhead. All sample spreads remain in the raw data and summaries.
[Recheck against main](recheck/main-benchstat.txt),
[recheck against previous PR](recheck/previous-benchstat.txt).

Local pilots tested a kind-first range filter, a reused end variable, and an
inline kind interval. They did not improve the combined controls. The retained
change uses the existing reference-kind helper and a local span. Thresholds
were not adjusted to select favorable cases.

## Validation

Go 1.22.12: full native, tagged telemetry, focused race, public runtime GC,
checked collector, baseline parity, and warmed allocation checks pass.
Bounded differential fuzzing passes: 10 seconds, two workers, 12,880 executions.
Moving and non-moving nursery lifecycle checks and corrupt-chain fallback
checks pass. Warm direct 16/32-range scans and representative fallbacks allocate
zero; cold mark-stack growth remains four/five allocations for 16/32 children.
The fixed range array remains 512 nonescaping bytes. The complete helper frame
is 840 bytes plus saved BP on AMD64; ARM64 adjusts SP by 848 bytes.
Linux ARM64 cross-build passes; this is not native execution.

`just lint` passes with the recipe's previously documented baseline staticcheck
findings. `just docs` and `git diff --check` pass. Full `just test` again fails
only the two standalone TinyGo link tests with duplicate `tinygo_task_exit`
symbols, matching the recorded clean-main reproduction. Exact pushed-head CI
is recorded in the PR.
The prior head's complete CI run 35478853672 passed, including native ARM64 and
Windows tagged telemetry; that does not validate the new commit. See
[validation logs](validation/). The known local TinyGo toolchain failures and
clean-main reproduction remain documented in the [original report](../README.md).

## Reproduce

Build main, previous PR, and candidate in separate worktrees. Copy the same four
benchmark/fixture files (`struct_card_scan_bench_test.go`,
`struct_card_mixed_bench_test.go`, `struct_card_collect_bench_test.go`, and
`struct_card_fixture_test.go`) from the candidate into each comparison worktree.
Compile each with `GOTOOLCHAIN=go1.22.12 go test -c
./src/core/runtime/gc/native -o /absolute/path/version.test`.

From the candidate worktree, with absolute paths to those binaries:

```sh
bash docs/performance/struct-card-ranges/measure-three.sh "$main" "$previous" "$candidate" isolated
bash docs/performance/struct-card-ranges/measure.sh "$main" "$candidate" full
BENCH_TIME=1s BENCH_PATTERN='^BenchmarkGCStructCardScan(SparseReferences)?$/^(fields=8|ranges=33)$' \
  bash docs/performance/struct-card-ranges/measure-three.sh "$main" "$previous" "$candidate" recheck
benchstat isolated/baseline.txt isolated/optimized.txt
benchstat isolated/previous.txt isolated/optimized.txt
benchstat full/baseline.txt full/optimized.txt
benchstat recheck/baseline.txt recheck/optimized.txt
```

The [round order](isolated-33-order.txt), [full pair order](full-order.txt), and
[recheck order](recheck-order.txt) include timestamps. Validation commands are
in the original report; this follow-up uses the same commands and toolchain.
