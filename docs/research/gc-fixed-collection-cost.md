# Fixed collection costs in Wago

This ports the applicable mechanisms from Slipstream's Snap work through
[`9db7ca4`](https://github.com/jtenner/slipstream/commit/9db7ca45a0e1ceb2eccc5bca273604b8dfd3e618)
and
[`356662f`](https://github.com/jtenner/slipstream/commit/356662f7bdff1269e6692fe078bcfd0c5e116f66).
The main cost reductions are in the former's parent, `e67c8bde`; `9db7ca4`
adds the failed-major root-admission correction. Wago's full Throughput
collection marks and sweeps; only minor collection moves nursery payloads.
Compact references name stable handles, so moving a payload does not rewrite
root words. Collection threading is outside this change.

## Mechanism mapping

| Snap mechanism | Wago analog and disposition |
|---|---|
| Consume root/remembered card words once; restore them on admission failure | Wago already enumerates sparse `slotCards`, `remembered` owners, and per-owner `objectCards` ranges. It marks directly, without Snap's bounded root-admission table. Keep these inputs intact until promotion succeeds, preserving retry after allocation failure. On complete young evacuation, clear each remembered owner's flag and card-list head together, drop the range list without revisiting each range's owner, and stop zeroing discarded scalar lists. Persistent card bits still clear through their sparse slot list. |
| Reset object-start bitmap words only below used span tops | Wago has no young object-start bitmap. Its full mark state is a handle-indexed `[]bool` whose length follows historical allocation. Track an exclusive highest-marked-handle bound while draining trace stacks and clear only that prefix at the next full reset. Include queued roots in the bound before resetting. Minor success/failure still clears young marks through the young ledger. The bound uses existing Collector padding, without changing size or existing offsets. |
| Retain bounded Eden pages; discard only used retired-survivor prefixes | Already partly satisfied: Wago retains and reuses one Go-owned backing allocation for Eden and both survivor spaces. Minor collection resets bumps rather than discarding/refaulting Eden or clearing its entire byte capacity. Allocation still initializes payloads. There is no collector-owned page retirement operation to shorten. Fixed configurations retain `align(NurseryBytes, 16) + 2 * SurvivorBytes`; automatic growth caps Eden at 2 MiB and each survivor at 512 KiB. This change adds no retained backing. |
| Apply Linux `MADV_NOHUGEPAGE` before any young write | Does not transfer: `makeAlignedBytes` allocates Go-managed storage, with object alignment rather than a private page-aligned mapping. Applying advice to rounded pages could affect other Go allocations, and does not reproduce Snap's untouched mapping-tail contract. Moving nursery ownership to mmap would be a separate allocator/lifetime redesign across supported platforms and TinyGo. No huge-page or nonresident-survivor-tail guarantee is introduced here. |

Snap's major evacuation/admission planning also does not transfer to Wago's
full mark-and-sweep collection. Full collection retains live nursery payloads
and their card inputs for a later minor; it must not consume those cards just
because the major succeeded. The post-minor card check remains necessary when
young survivors remain: stable slot values do not reveal which handles promoted
until the transaction commits. There is no Wago equivalent of Snap's second
bitmap-clearing-only admission walk to remove.

## Safety and regressions

Only stack drains update the mark bound, keeping the per-edge marking helpers
small. A reset also accounts for roots still queued on the stack. A newly
allocated mark slice is already zeroed. Existing minor rollback paths clear
young marks before returning an error. A bound larger than the remaining set
of marked handles is conservative and costs at most one longer reset.

Every live object-card range belongs to a remembered owner. Complete young
evacuation clears owner heads through that owner list; general/full card cleanup
still visits range owners when that premise is unavailable. Dropped remembered,
object-card, and slot-card entries contain only scalar values. Append overwrites
each complete entry, and resetting the tombstone free-list head prevents stale
links from becoming reachable. Persistent slot bitmap bits are still cleared.

`fixed_collection_cost_test.go` covers queued and drained marks, transitive
high-handle reachability, shrinking the reset bound, reused identities across
minor/full collection, empty collections, and repeated sparse card-list reuse
with global/table bits crossing word boundaries. Existing survivor, full-card,
promotion transaction/failure, malformed-card, and debug fault-injection tests
cover the paths whose lifecycle rules remain in place.

## Measurements

Linux/AMD64, Ryzen 7 8845HS, Go 1.27.1, 2026-10-24. The baseline is branch
starting commit `fc6af651c15e286409d9cc117c94181e9ee50ff3` (main when this work
started). The baseline binary was saved before the runtime edits; the fixed
binary was rebuilt from implementation commit `6e4f56eee2efd89496408febe83c490bd10217d2`,
using the same checkout and toolchain. Seven fresh-process pairs used
seed 640611 to shuffle baseline/fixed order within each pair, `GOMAXPROCS=1`,
CPU 2, and 1000 iterations per sample. The global timing lease covered this run
from **2026-10-24 12:57:22 UTC to 12:58:47 UTC**. Slipstream correctness gates had finished by 12:54 UTC, and the
parallel-collection thread confirmed its measurements stopped before this run.
No concurrent benchmarks or builds from these threads overlapped the run. The
lease was released to the parallel thread immediately after measurement.

This leased run supersedes the original paired round (approximately
12:43:44–12:45:15 UTC, reconstructed from output file timestamps), which preceded
the lease steering note and overlapped Slipstream correctness work. Samples
from the two rounds are not pooled; the original round remains in commit
`6e4f56eee`.

All workloads are existing native GC benchmarks. Time includes the work each
fixture places inside its timer, rather than whole-process startup. Medians and
sample minimum/maximum follow; overlapping ranges limit small-delta claims.

| Workload | Baseline median [range], ns/op | Fixed median [range], ns/op | Time delta |
|---|---:|---:|---:|
| Full, 65,536 historical handles, one live | 664.3 [655.7–668.9] | 211.2 [204.1–234.6] | -68.21% |
| Full, 262,144 historical handles, one live | 2056 [2028–2078] | 284.2 [278.9–295] | -86.18% |
| Full, 262,144 historical handles, clustered live | 6095 [6083–6157] | 4568 [4446–8577] | -25.05% |
| Full, 262,144 historical handles, spread live | 137394 [137084–141444] | 137465 [136594–141146] | +0.05% |
| Full, 65,536 live handles (dense control) | 1059138 [1054617–1062392] | 1080606 [1076279–1101332] | +2.03% |
| Full, 262,144 live handles (dense control) | 4249627 [4237738–4297982] | 4320382 [4300514–4449702] | +1.66% |
| Full, 9000 live pointer-free objects | 144996 [143951–148241] | 146584 [146297–150462] | +1.10% |
| Full, 90 live pointer-free objects | 1512 [1477–1611] | 1564 [1501–1639] | +3.44% |
| Minor, 8 sparse remembered ranges | 810.8 [758.4–912.4] | 801.2 [747.1–817.8] | -1.18% |
| Minor, 64 sparse remembered ranges | 4709 [4478–4810] | 4672 [4408–5189] | -0.79% |
| Minor, 512 sparse remembered ranges | 34800 [34069–35318] | 33936 [33784–35007] | -2.48% |
| Minor, one dirty root among 4096 global slots | 219.2 [203.2–231.1] | 218.4 [208–247.1] | -0.36% |
| Mixed lifetime graph, immediate promotion | 223.2 [208.4–224.1] | 215.2 [181.9–223.7] | -3.58% |
| Mixed lifetime graph, survivor aging | 179.4 [170.4–203.4] | 175.3 [171.3–180.4] | -2.29% |
| Dead churn, immediate promotion | 48.48 [46.52–54.42] | 47.14 [45.73–47.92] | -2.76% |
| Dead churn, survivor aging | 49.44 [46.8–53.4] | 47.9 [46.99–54.06] | -3.11% |

The clear gain is repeated full collection of a historically large handle
table with low live identities. Spread live identities still require a long
clear prefix. Dense full-collection controls cost 1.66–2.03% more, with separated ranges for the two large dense
cases; maintaining the reset bound adds work per traced live object. Card-heavy
minor results are small and overlap; no material minor-collection or dead-churn
speedup is established. All cases retain the same median B/op and allocs/op. Nursery capacity and Collector size are
unchanged. These measurements do not reproduce Slipstream's 97% dead-churn
improvement and do not measure whole-process RSS or a Linux mapping policy.

[Baseline samples](gc-fixed-cost-results/baseline.txt),
[fixed samples](gc-fixed-cost-results/fixed.txt), and
[all median/range/allocation comparisons](gc-fixed-cost-results/summary.json)
include every measured subcase, including cases omitted from the selected table.
[Run metadata and per-sample UTC timestamps](gc-fixed-cost-results/run.json)
record the global-lease interval and measured source/binary identities.
Reproduce with a benchmark binary built on each revision:

```sh
go test -c -o /tmp/gc.test ./src/core/runtime/gc/native
GOMAXPROCS=1 taskset -c 2 /tmp/gc.test -test.run '^$' \
  -test.bench 'Benchmark(FullSparseHandleBlocks|ThroughputFullLivePointerFree|ThroughputZeroSurvivalPolicy|ThroughputMixedLifetimeGraph|GCDirtyPersistentRoots|GCSparseCardWholeMinor)$' \
  -test.benchtime=1000x -test.count=1
```

Run baseline and fixed once per pair for seven pairs, shuffling their order
with `random.Random(640611).shuffle(order)` in Python. The measured binary
SHA-256 hashes were
`ba9ea8a57ea8e37d66c626eabe51c6164237e103f98e37d8c527e3b47ce79f9f`
(baseline) and
`f5cfe48c30c748a2399e73a22a1c2d39aa73a04132ab7d5211d2b7633e2adb28`
(fixed).
