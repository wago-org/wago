# Complete prepared shared context snapshot

Retain bf51902c after 266 complete exact-source captures on Apple M4 Max: 148 main and 118 feature cases, all four phases, 15 timing samples for every successful phase, matching artifact and contract hashes. All 798 current, e57e1232 baseline, and pinned f303ed27 SIMD w2c2 Clang captures passed the source and resource audit. Functional outcome counts are unchanged: 107 feature steady results succeed and 11 remain unsupported.

The source-specific opaque shared callback workload is 4.249 ms versus 193.5 µs for the peer, or 21.96 times slower. Ordinary compact imports are 680.4 µs versus 298.3 µs, or 2.28 times slower. Native thread identity is 333.2 µs versus 194.7 µs, or 1.71 times slower. These historical-peer comparisons describe remaining gaps; the fresh reversed paired captures isolate the prepared-context improvement.

Main steady geometric mean is 0.9982 times e57e1232 and feature mean is 0.9879 times e57e1232. Against the pinned peer, 107/148 main and 12/54 comparable feature workloads are slower. Small crossings near parity and aggregate timing movements are not attributed to this callback change. Each-feature parity remains incomplete.

The report includes all source identities, contracts, phase outcomes and per-workload ratios. Raw captures remain in the private validation directory. This snapshot is eligible for local publication after the existing two-hour refresh window at 2026-10-14 05:01:10 UTC. No production deployment.
