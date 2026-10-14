# Narrow verified native-import context preservation

The corrected source `dde95c0e` keeps the existing lean route for a single
resource-free import. The context shortcut remains available for modules with
resources or multiple imports. This supersedes `d4dea937`, whose matched minimal
callback loop regressed 39–40% and was stopped before local publication.

Two reversed comparisons against `2cd322f0` and pinned SIMD w2c2-clang `f303ed27`
validate the exact artifact, contract, binding and source. All 24 captures / 96
phase rows have 15 samples per successful phase, with no collector/resource
failures.

| Steady workload | Change, first / reverse | Current latency | Current / peer |
| --- | --- | --- | --- |
| Ordinary compact imports | -36.17% / -36.82% | 676 us | 2.31x |
| Owned identity thread callback | -45.03% / -44.98% | 333 us | 1.75x / 1.74x |
| Owned opaque thread callback | +0.60% / +1.59% | 5.46–5.48 ms | 28.45x / 28.37x |
| Minimal dependent callback loop | -0.10% / -0.06% | 2.76–2.83 ns/cycle | 1.92x / 2.43x |

The opaque callback retains the preceding scheduler improvement: it remains
roughly twice as fast as before that separate change. Its small regression
versus the immediately preceding scheduler source is recorded as a tradeoff.
The minimal callback loop retains its original 560-byte code size and has no
measured regression. Compact code grows 1,184 to 1,248 bytes and thread callback
code 832 to 864 bytes. This is progress toward parity, not parity.

Focused native-leaf, scalar-host, shared-memory, owned-reference, cross-instance,
Core 3 and official atomic diagnostics pass at dde95c0e. AMD64 validation is from
d4dea937; the subsequent correction changes only ARM64 emission. Tests include
mutable globals, live locals, shared memory, ordinary versus pure callbacks,
register ABI and shared-base caching enabled/disabled, codec fallback, GC,
cancellation and callback traps. Earlier candidate evidence and the rejected
loop measurements remain in native-import-context.md/.json.

Three alternating compiler-cost pairs, each 100 passes over 218 Core 3 and 27
thread modules, show Core 3 code/allocation unchanged and RSS -0.38%; thread
allocation +0.24%, code +0.41%, RSS +0.88%. Elapsed changes -0.09% / -4.38% are
not claimed as compiler speedups from these short measurements.

A fresh complete 266-case dde95c0e snapshot is collecting. A durable coordinator
runs exact-source, contract, functional-outcome, paired-loop and aggregate checks
before writing its retention receipt. The local publisher waits for that receipt,
complete captures and the next permitted refresh at 2026-10-14 02:59:58 UTC.
The previously retained 49145ddf snapshot is already browser-verified at the
user's exact local Features URL. No production deployment or push occurred.
