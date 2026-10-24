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
started). Both native GC benchmark binaries were built from the same checkout
and toolchain, before/after the runtime edits. Seven fresh-process pairs used
seed 640611 to shuffle baseline/fixed order within each pair, `GOMAXPROCS=1`,
CPU 2, and 1000 iterations per sample. No concurrent builds were launched by
this thread during measurement; the machine was shared with other work.

All workloads are existing native GC benchmarks. Time includes the work each
fixture places inside its timer, rather than whole-process startup. Medians and
sample minimum/maximum follow; overlapping ranges limit small-delta claims.

| Workload | Baseline median [range], ns/op | Fixed median [range], ns/op | Time delta |
|---|---:|---:|---:|
| Full, 65,536 historical handles, one live | 685.2 [660.1–715.6] | 221.7 [213.9–233.5] | -67.64% |
| Full, 262,144 historical handles, one live | 2110 [2055–2254] | 295.8 [280.1–315.6] | -85.98% |
| Full, 262,144 historical handles, clustered live | 6326 [6105–8740] | 4576 [4438–4748] | -27.66% |
| Full, 262,144 historical handles, spread live | 142647 [141543–150856] | 143345 [139603–204778] | +0.49% |
| Full, 262,144 live handles (dense control) | 4391392 [4236165–6170477] | 4406309 [4278975–5135837] | +0.34% |
| Full, 9000 live pointer-free objects | 150490 [145439–155135] | 150905 [146257–167878] | +0.28% |
| Full, 90 live pointer-free objects | 1536 [1485–1687] | 1601 [1445–1719] | +4.23% |
| Minor, 8 sparse remembered ranges | 821 [807.2–954.4] | 798.7 [748.3–844.6] | -2.72% |
| Minor, 64 sparse remembered ranges | 4900 [4637–5055] | 4800 [4658–4968] | -2.04% |
| Minor, 512 sparse remembered ranges | 35882 [34233–36299] | 35335 [33961–35666] | -1.52% |
| Minor, one dirty root among 4096 global slots | 227.6 [203.5–251.9] | 232.6 [213.4–244.1] | +2.20% |
| Mixed lifetime graph, immediate promotion | 219 [196.2–245.3] | 218 [179.1–222] | -0.46% |
| Mixed lifetime graph, survivor aging | 174.2 [169.6–183.2] | 173.5 [172.2–188.6] | -0.40% |
| Dead churn, immediate promotion | 48.56 [46.27–54.69] | 49.49 [46.1–55.91] | +1.92% |
| Dead churn, survivor aging | 49.41 [47.42–68.25] | 51.62 [49.62–59.24] | +4.47% |

The clear gain is repeated full collection of a historically large handle
table with low live identities. Spread live identities still require a long
clear prefix. Card-heavy minor results are small and overlap; no material
minor-collection or dead-churn speedup is established. All cases retain the
same median B/op and allocs/op. Nursery capacity and Collector size are
unchanged. These measurements do not reproduce Slipstream's 97% dead-churn
improvement and do not measure whole-process RSS or a Linux mapping policy.

[Baseline samples](gc-fixed-cost-results/baseline.txt),
[fixed samples](gc-fixed-cost-results/fixed.txt), and
[all median/range/allocation comparisons](gc-fixed-cost-results/summary.json)
include every measured subcase, including small regressions omitted from the
selected table. Reproduce with a benchmark binary built on each revision:

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
`f4fc26364a74f013e4d8fbf6d731ac8ef42f0f970d0524ecb79d1095e7d75af6`
(fixed).
