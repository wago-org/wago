# Bounded stable promotion buckets

## Problem and scope

The Dewdrop compiler CPU profile from 2026-10-21 attributes about 0.54 seconds
of a 51.45-second sample to stable promotion-plan sorting. Most of the full job
runs in generated native code, so this change cannot close that full gap.
This PR changes promotion-plan ordering work only. A full Dewdrop comparison
is still required before claiming a full-job improvement.

`promoteNursery` and `promoteMarkedNurseryImmediate` sort live plans by object
kind and old-space allocation size. The old stable comparison sort repeatedly
moves records even though small allocations use only 18 size classes.

## Change and proof

For 64 through 65,536 plans, use stable counting buckets in this order:

1. Survivor-space copies, in original order.
2. Old-space size classes, in increasing allocation-size order.
3. Oversized old-space allocations, sorted by the original stable comparator.
4. Large objects, in original order.

Each input plan is scattered into the next slot of its bucket. Thus equal keys
keep their input order. This matches the original sort, including allocation
runs and failure-point order. The sorter does not change any plan field,
handle, object, or allocator state. All destination and commit failure points
remain in their original places.

Small plans take O(N + C) work, with C = 21 bucket slots. For K oversized plans,
only those K plans retain the comparison sort. More than half oversized,
less than 64 plans, more than 65,536 plans, and nonnative collectors use the
original in-place sort. Go's stable comparison sort uses O(K log K)
comparisons and O(K log-squared K) swaps.

The extra workspace has a hard cap of 65,536 20-byte records (1.25 MiB).
It is cleared after each use and reused. It contains no Go pointers. Its
24-byte slice header is appended to Collector, preserving all previous field
offsets. Collector close releases it. Handle and native-view layouts do not
change. Footprint tests assert the exact added header cost.

## Measurements

CPU: AMD Ryzen 7 8845HS. Linux AMD64, CPU 15, GOMAXPROCS=1. Three 200 ms
samples per case. Both paths include the same input copy. The control caches
size keys before timing, matching the stronger comparison sort in the local
Dewdrop stack rather than only origin/main's repeated key calculation.
Warm runs allocate zero bytes and zero objects in both paths.

| Small plans | Cached stable sort median | Bucket median | Ratio |
| --- | ---: | ---: | ---: |
| 64 | 6,667 ns | 691.7 ns | 9.64x |
| 4,096 | 813,870 ns | 47,095 ns | 17.28x |
| 32,768 | 7,893,768 ns | 832,772 ns | 9.48x |

These are sorting results, not complete collector or compiler results. The
benchmark command, including its build, took 8.702 seconds:

```sh
GOMAXPROCS=1 taskset -c 15 go test ./src/core/runtime/gc/native -run '^$' -bench '^BenchmarkPromotionPlanSort/mixed-false/plans-(64|4096|32768)/(cached|buckets)$' -benchtime=200ms -count=3
```

An initial 32-plan oversized-heavy case regressed. The final minimum is 64,
and oversized-majority inputs retain the comparison sort.

## Checks

Stable-order checks compare every complete plan to the original sort across
four size-class limits, mixed survivor/old/large plans, equal keys, and both
sides of the scratch cap. Warm allocation checks also verify scratch clearing.
Debug tests inject failures before, during, and after allocation, at three
positions in each phase, for both moving and immediate promotion. They compare
the complete heap/allocator state, then retry and check every surviving value.

The first full package run exposed two footprint assertions. The scratch
header now follows all existing fields, and both assertions describe the exact
new size. No old field offset changes.

```sh
go test ./src/core/runtime/gc/native -count=1
go test -tags wagodebug ./src/core/runtime/gc/native -count=1
```

Standard native package tests pass in 3.560 seconds including the build.
The full `wagodebug` native package passes in 3.348 seconds including the build.

## Integration note

This branch starts at origin/main `cd029409b`. The local Dewdrop stack already
has a separate change that stores cached allocation keys in `plan.entry.size`.
When applying this commit there, retain that cached-key comparator in
`sortPromotionPlansByComparison`; the bucket pass preserves all plan fields.
This avoids undoing its existing speedup on fallback inputs.

## Full branch validation

The full checked repository suite passes in 134.886 seconds, and the checked
benchmark module passes in 40.046 seconds. The normal run passes all compiler,
collector, and public API packages; two plugin-install tests hit transient
`text file busy` failures. Their entire package passes on rerun in 19.284
seconds. The initial failures remain visible in the saved result records.

[Raw benchmark samples](gc-promotion-buckets-results/bench.txt) and the
[full check records](gc-promotion-buckets-results/astra-buckets-followup-tests.json)
are included. No full Dewdrop speedup is claimed for this change.
