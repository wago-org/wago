# Complete snapshot after shared-directory reuse

The e57e1232 snapshot completed all 266 cases on Apple M4 Max at approximately
2026-10-14 01:52 UTC: 148 main and 118 feature workloads, zero collector errors
and zero resource failures. All successful latency phases contain 15 samples.
Functional outcome counts exactly match dde95c0e: all 148 main cases pass; feature
steady has 107 passing and 11 unsupported cases. Unsupported/code-unavailable
measurements remain nonnumeric.

The audit validates 798 current/baseline/peer captures against exact source,
artifact, contract, phase, samples and resource fields. Both baseline corpora use
dde95c0e; the SIMD w2c2-clang peer is pinned f303ed27. Peer full-suite captures
precede the current snapshot. Fresh reversed pairs in shared-directory-stability
establish the approximately 20% opaque-callback improvement independently.

| Target | Current steady | Current / pinned peer |
| --- | --- | --- |
| Opaque thread callback | 4.449 ms | 22.988x |
| Ordinary compact imports | 692 us | 2.319x |
| Identity thread callback | 335 us | 1.721x |

Across successful positive-latency pairs, main steady geometric mean is 1.1277x
peer (109/148 slower), features 0.3443x (14/54 slower), combined 0.8212x
(123/202 slower). Versus the preceding full dde95c0e snapshot, main is 1.0090x
and features 1.0073x. These small descriptive shifts and the extra near-parity
crossing are not treated as evidence of a compiler change: the compiler and
native code sizes are unchanged, and the reversed targeted control comparisons
do not show persistent regressions. Each-feature parity is still incomplete.

ARM64 diagnostic, Core 3 and official atomic checks and native AMD64 host/thread
checks pass. Compiler allocation and native code are unchanged in alternating
corpus-cost measurements. The new sidecar flag records immutable shared-directory
entries and retains existing callback ownership, migration and publication
behavior.

After full source/contract/resource/functional and aggregate audits, this snapshot
safely superseded the queued dde95c0e local publisher before its first receipt.
The replacement waits for the same requested two-hour refresh window, no earlier
than 2026-10-14 02:59:58 UTC. The publisher will audit all 1,064 local phase rows,
preserve history and verify the exact Features URL in Chrome. No production
deployment or push occurred. Raw captures and the retention receipt remain under
`.validation/wasm3-parity/full-parity-e57e1232/`; the accompanying JSON records the
raw report hash and all comparison/manifest rows.
