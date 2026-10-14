# Complete retained snapshot after thread and import improvements

The complete dde95c0e snapshot finished on Apple M4 Max at approximately
2026-10-14 01:24 UTC. All 266 exact-source cases completed with zero collector
errors or resource failures: 148 main workloads and 118 feature cases. Each
capture preserves compile, instantiate, first-call and steady phases. All
successful latency phases have 15 timing samples. Functional outcome counts
match 49145ddf: main 148/148 successful, feature steady 107 successful and
11 unsupported. Unsupported phases and code-size measurements remain nonnumeric.

The comparison audit checks 798 immutable current/baseline/peer captures against
source metadata, artifact and contract hashes, phase, sample counts and resource
status. Both baseline corpora use complete source 49145ddf. The SIMD Clang peer
is pinned f303ed27; its full-suite captures precede this snapshot, so aggregate
ratios describe snapshots rather than isolating a compiler change. Separate
fresh reversed pairs establish the targeted gains in native-import-narrow.md.

| Target | Current steady | Current / peer | Current / 49145ddf |
| --- | --- | --- | --- |
| Ordinary compact imports | 679 us | 2.275x | 0.640x |
| Owned identity thread callback | 342 us | 1.757x | 0.563x |
| Owned opaque thread callback | 5.458 ms | 28.204x | 0.501x |

The corrected minimal callback loop was independently verified unchanged in both
fresh matched orders; the earlier broad shortcut candidate was superseded before
publication. The new source preserves that lean route.

Across successful positive-latency peer pairs, main steady geometric mean is
1.1177x (109/148 slower), features 0.3449x (13/54 slower), and combined 0.8162x
(122/202 slower). Versus 49145ddf, main is 0.9966x and features 0.9822x.
Each-feature parity remains incomplete, especially opaque Go callbacks, compact
imports and identity thread callbacks. Aggregate wins do not satisfy that goal.

The full audit retained this source after unchanged functional counts, strict
source/contract/resource checks, fresh minimal-loop regression guards and broad
aggregate checks. Local publication is scheduled no earlier than
2026-10-14 02:59:58 UTC, respecting the requested two-hour interval. The publisher
also waits for the complete retention receipt and verifies the exact Features
URL in Chrome, including new Threads and compact-import values. No production
deployment or push occurred.

The accompanying JSON keeps all per-phase comparison rows, source plan,
capture hash manifest and retention receipt. Full raw evidence remains under
`.validation/wasm3-parity/full-parity-dde95c0e/` with its hash recorded in the JSON.
