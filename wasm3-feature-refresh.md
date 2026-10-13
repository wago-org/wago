# WebAssembly feature execution comparison

## Fresh local snapshot — 2026-10-13

The latest full capture uses Wago `e0067d93` and SIMD-capable JairusSW/w2c2
`f303ed27`, compiled with Clang on Apple M4 Max Darwin/ARM64. It covers
118 feature contracts and 148 main contracts per engine. All four lifecycle
phases and their explicit failed/unsupported outcomes are published to the local
wasm.fyi tables; the server remains at http://localhost:8788/benchmarks/.
No production deployment was made. The 575,121 existing historical observations
are preserved, and SQLite integrity and foreign-key checks pass.

Three independent launches retain five verified timing samples each. Feature
steady batches calibrate to at least 50 ms; these values cover the entire repeated
guest invocation. The peer uses unchecked native C with `-O3 -march=native`;
Wago retains its managed checks. This is a new cohort, separate from the earlier
raw-probe capture below. [The source-pinned report](docs/benchmarks/wasm3-2026-10-13/current-feature-cohort.json)
retains every feature row and sample, capture hashes, matched contract/artifact
identities, outcome counts, and the database audit.

There are 54 successful steady pairs; 25 exceed 1.1x the peer.
This is a triage threshold, not a completion criterion. Wago passes 107 feature
steady contracts with 11 unsupported and no failures; w2c2 passes 54, rejects 35
as unsupported, and records 29 failures. All 148 main contracts pass for both
engines. Five Wago JSPI outcomes were repaired through declared-capability
controller admission; the runtime binary and successful timings are unchanged,
and the original failed captures remain archived locally.

| Largest remaining feature gaps | Wago ns | w2c2 ns | Ratio |
| --- | ---: | ---: | ---: |
| `features/threads/host-opaque-identity/performance-4096x64` | 14,902,466.6 | 196,811.4 | 75.720 |
| `features/core-ctl/branch-mixed/performance-4096x64` | 179,567.1 | 19,711.0 | 9.110 |
| `features/sign-extension/i64-extend32-high-word/performance-4096x64` | 91,528.4 | 12,008.8 | 7.622 |
| `features/sign-extension/i64-extend16/performance-4096x64` | 84,854.1 | 12,029.5 | 7.054 |
| `features/sign-extension/i64-extend8/performance-4096x64` | 86,641.7 | 12,337.9 | 7.022 |
| `features/core-num/integer-rotate-popcount/performance-4096x64` | 80,204.2 | 12,216.8 | 6.565 |
| `features/sign-extension/i32-extend8/performance-4096x64` | 78,757.0 | 12,137.5 | 6.489 |
| `features/sign-extension/i32-extend16/performance-4096x64` | 77,948.3 | 12,413.6 | 6.279 |
| `features/core-num/integer-div-rem/performance-4096x64` | 252,460.6 | 42,271.2 | 5.972 |
| `features/core-glb/f32-rounding-state/performance-4096x64` | 804,359.2 | 171,897.0 | 4.679 |
| `features/mutable-globals/f32-rounding-state/performance-4096x64` | 791,358.4 | 169,387.7 | 4.672 |
| `features/compact-imports/ordinary-baseline/performance-4096x64` | 1,094,750.0 | 334,717.1 | 3.271 |

The earlier global-state improvements remain in the fresh full cohort. The
opaque host-call gap is the first investigation priority; branch, bit, sign,
conversion, floating-global and carry gaps remain open. Close differences need
targeted confirmation, and native AMD64 performance and full stage 4/5 coverage
remain unproven. Cancelled Go JSPI/CSP analogues remain cancelled.

## Earlier 115-contract cohort

The full implementation and performance objective remains open. This full
115-contract capture uses Wago production revision `b4ef4c22` on Apple M4 Max
Darwin/ARM64 and the pinned SIMD-capable w2c2 fork. The full Wago adapter identity
is `b4ef4c22296574efcb09a9e39d0914b468ddbecd/source-367d9e2e3727e1fe995cfb3973c373f35fcc91ffb727d4f798f69663dc36eabf`. wasm.fyi source revision is `9a11c7fc6c96e4714431c36c0a924272d9422a47`.

Three independent processes per contract/profile collect five verified samples,
each lasting at least 50 ms, with two warmup batches and GOMAXPROCS=1. Values below
are process medians in ns per entire repeated guest invocation. The peer uses
unchecked `-O3 -march=native` C; exact configuration and raw samples are retained
in [the report](docs/benchmarks/wasm3-2026-10-12/feature-refresh-oct13-results.json).

Wago passes 107 execution contracts in every process, with eight unsupported
JavaScript host profiles and zero failures. w2c2 passes 67, rejects 26 as unsupported,
and fails 22 during translation or guest execution. The 67 successful pairs use
identical bytecode and qualified durations; rejected/failed contracts have no
numeric comparison. Four wide-arithmetic contracts pass in Wago but remain
unsupported by the peer. Compact imports were absent from this snapshot; the
later source-built coverage exposed and repaired decoder errors (see
[the compact checkpoint](wasm3-feature-parity.md#source-built-compact-imports-reveal-and-repair-decoder-errors)).
Four lifecycle probes and nine compilation probes remain separate from these
execution measurements. The cancelled Go JSPI/CSP analogues have not been added.

## Improvements retained across the full corpus

These eight contracts had large gaps in the earlier `4680ca19` capture; their
bytecode is unchanged. The ratios below compare Wago with the peer measured in
each capture, so unrelated small timing differences are descriptive.

| Contract | Earlier Wago/peer | Current Wago | Current peer | Current Wago/peer |
| --- | ---: | ---: | ---: | ---: |
| `features/core-num/integer-multiply-add/performance-4096x64` | 288.033 | 98.39 | 296.86 | 0.331 |
| `features/core-num/signed-zero-bits/performance-4096x64` | 507.538 | 100.38 | 295.75 | 0.339 |
| `features/core-ctl/direct-call/performance-4096x64` | 671.414 | 112.04 | 293.17 | 0.382 |
| `features/core-glb/immutable-constant/performance-4096x64` | 243.494 | 127.40 | 292.00 | 0.436 |
| `features/mutable-globals/exported-read/performance-4096x64` | 228.952 | 126.73 | 297.09 | 0.427 |
| `features/saturating-float-to-int/f64-unsigned-negative-clamp/performance-4096x64` | 453.382 | 86.44 | 296.53 | 0.292 |
| `features/simd/i32x4-add-multiply/performance-4096x64` | 369.611 | 103.88 | 289.54 | 0.359 |
| `features/simd/arithmetic-scalar-baseline/performance-4096x64` | 270.402 | 103.35 | 289.19 | 0.357 |

## Later targeted checkpoints

Later captures close six of the large global-state gaps from the full snapshot:

| Contract | Production revision | Wago ns | Peer ns | Wago/peer |
| --- | --- | ---: | ---: | ---: |
| `core-glb/global-swap` | `32c26ea3` | 170.74 | 292.14 | 0.584 |
| `mutable-globals/global-swap` | `32c26ea3` | 166.00 | 290.07 | 0.572 |
| `core-glb/i64-global-split` | `81fba84c` | 129.88 | 294.19 | 0.441 |
| `mutable-globals/i64-global-split` | `81fba84c` | 131.44 | 294.89 | 0.446 |
| `core-glb/mutable-accumulate` | `fb801d8f` | 146.97 | 293.52 | 0.501 |
| `mutable-globals/mutable-accumulate` | `fb801d8f` | 147.21 | 290.25 | 0.507 |

These are matched targeted captures, separate from the full cohort below. Their
[global assignment report](docs/benchmarks/wasm3-2026-10-12/global-write-sum-results.json),
[mixed-width report](docs/benchmarks/wasm3-2026-10-12/mixed-width-sum-results.json),
and [recurrence report](docs/benchmarks/wasm3-2026-10-12/global-recurrence-sum-results.json)
retain raw rows, compile costs and validation. The aliased ordinary host control
also improves to 3.585x the peer in its later targeted checkpoint. Carry and floating
recurrences remain substantive gaps.

## Paired gaps in the full capture

Twenty-nine paired contracts exceeded 1.1x the peer median in this full snapshot;
six global-state contracts are closed by the later checkpoints above. This is a triage
threshold, not a universal completion rule. Global-state loops, opaque Go
callbacks, branch/bit operations and conversions remain substantive gaps.

| Contract | Wago | Unchecked w2c2 | Ratio |
| --- | ---: | ---: | ---: |
| `features/core-glb/i64-global-split/performance-4096x64` | 101,494.2 | 290.4 | 349.548 |
| `features/mutable-globals/i64-global-split/performance-4096x64` | 104,741.3 | 301.6 | 347.269 |
| `features/core-glb/global-swap/performance-4096x64` | 98,358.1 | 293.6 | 335.053 |
| `features/mutable-globals/global-swap/performance-4096x64` | 98,853.3 | 308.1 | 320.872 |
| `features/mutable-globals/mutable-accumulate/performance-4096x64` | 69,638.5 | 293.5 | 237.254 |
| `features/core-glb/mutable-accumulate/performance-4096x64` | 68,466.6 | 293.9 | 232.967 |
| `features/threads/host-opaque-identity/performance-4096x64` | 14,363,656.2 | 192,235.2 | 74.719 |
| `features/core-ctl/branch-mixed/performance-4096x64` | 165,761.0 | 19,668.7 | 8.428 |
| `features/sign-extension/i64-extend32-high-word/performance-4096x64` | 91,007.6 | 11,989.5 | 7.591 |
| `features/sign-extension/i64-extend8/performance-4096x64` | 87,044.9 | 12,077.2 | 7.207 |
| `features/sign-extension/i64-extend16/performance-4096x64` | 84,771.6 | 12,022.3 | 7.051 |
| `features/sign-extension/i32-extend16/performance-4096x64` | 79,548.0 | 12,078.4 | 6.586 |
| `features/core-num/integer-rotate-popcount/performance-4096x64` | 81,060.0 | 12,491.1 | 6.489 |
| `features/sign-extension/i32-extend8/performance-4096x64` | 75,894.6 | 11,948.5 | 6.352 |
| `features/core-num/integer-div-rem/performance-4096x64` | 240,978.2 | 43,625.9 | 5.524 |
| `features/mutable-globals/f32-rounding-state/performance-4096x64` | 879,839.9 | 175,292.5 | 5.019 |
| `features/core-glb/f32-rounding-state/performance-4096x64` | 775,991.4 | 169,501.9 | 4.578 |
| `features/saturating-float-to-int/f32-in-range/performance-4096x64` | 200,635.5 | 54,711.8 | 3.667 |
| `features/threads/host-identity/performance-4096x64` | 613,398.4 | 194,839.6 | 3.148 |
| `features/core-glb/i64-carry-overflow/performance-4096x64` | 90,956.9 | 35,281.6 | 2.578 |
| `features/mutable-globals/i64-carry-overflow/performance-4096x64` | 91,789.8 | 36,173.7 | 2.537 |
| `features/saturating-float-to-int/f32-overflow-clamp/performance-4096x64` | 137,009.9 | 57,467.6 | 2.384 |
| `features/core-tab/indirect-call/performance-4096x64` | 327,206.0 | 190,226.1 | 1.720 |
| `features/saturating-float-to-int/i64-in-range/performance-4096x64` | 136,030.2 | 85,432.0 | 1.592 |
| `features/core-glb/f64-global-scale/performance-4096x64` | 154,974.2 | 102,588.4 | 1.511 |

The largest process CV is 0.362 on bulk fill; several other rows also contain
timing variation, retained in the report. Small differences and near-parity
results need targeted confirmation. The large global/callback gaps remain clear.

The latest production change passes pinned ARM64 Core 3/threads conformance and
guarded race tests, with retained alternating compile-cost evidence. Targeted integer-sum execution and
diagnostic tests now pass on native AMD64 at `root@remote`; Hub is unavailable
for maintenance. Full AMD64 conformance and timed parity captures remain open. This report does not
establish all feature combinations or complete feature-by-feature parity.

### Private shared-memory control reuse — 2026-10-13

Wago `17c78d48` reuses instance-private threaded control after owned host
callbacks, while refreshing the shared-memory directory on every return.
Migration, changed context versions, and GC execution still restore context.
The regression covers defined/imported shared memory, concurrent growth,
both register ABIs, and artifact roundtrips. ARM64 normal/guarded-race tests,
pinned Core 3/threads conformance, and native AMD64 targeted tests pass.

A fresh locked comparison uses the same six contracts, four phases and
15 timing samples per successful row against Wago `e0067d93` and Clang w2c2
`f303ed27`. Opaque callbacks fall from 14.145 ms to 11.136 ms (21.3%), but
remain 60.87x the peer's 0.183 ms. The other five steady controls change
between -0.9% and +1.6%; their small differences require further confirmation.
All six generated Wago native images retain their code size.

Three alternating compile process pairs compare the exact parent `5458bb4f`
(documentation-only changes since `e0067d93`) with `17c78d48`. Median Core 3
corpus latency changes -1.39%, threads +1.34%; generated code is identical,
threads allocations are identical, and Core 3 differs by six allocations and
600 bytes across 21,800 compiles. Median process RSS changes +0.23% and -2.00%,
respectively. These are targeted measurements, not proof of all compile costs.

The [retained report](docs/benchmarks/wasm3-2026-10-13/host-control-reuse.json)
contains all phase samples, contract/artifact hashes, raw compile measurements,
red/green regression output, correctness checks, and six local publication
receipts. The new Wago results are available at http://localhost:8788/benchmarks/.
No production deployment occurred. Full parity and native AMD64 timing remain open.

### Shared backing handle checkpoint — 2026-10-13

Wago `283ec369` retains nonmoving shared backing handles in indexed-memory
directories, avoiding owner mutex acquisition during callback return while
still acquire-reading each live size. A new regression closes the producer
inside a host callback, verifies two consumer invocations, and confirms the
last consumer releases backing. ARM64 threads normal/guarded-race and pinned
Core 3/threads suites pass. Native AMD64 threads tests pass after copying the
missing atomic fixtures; optional pinned official suites were skipped there.

The fresh six-contract, four-phase capture (15 timing samples per successful
row) improves opaque callbacks from 11.084 ms at `17c78d48` to 10.605 ms
(4.3%), versus 0.186 ms at Clang w2c2 `f303ed27`: still **57.00x slower**.
The five other steady controls range from -1.3% to +1.1%, and all native image
sizes match. Six new Wago captures are published only to the local server.

Alternating compile process pairs show Core 3 -0.73%, threads +3.73%; code
sizes are identical and allocations effectively unchanged. Median RSS changes
-0.12% and -2.00%, respectively. Threads compile-cost neutrality is unproven.
The cache adds a 24-byte slice header to allocated indexed-memory sidecars
and a pointer array when shared memories exist; ordinary single-memory
instances still have no sidecar. Warmed atomic invocation remains allocation-free.
The [raw report](docs/benchmarks/wasm3-2026-10-13/shared-backing-host-return.json)
retains phase samples, hashes, compile rows, pilot evidence, validation logs,
and local publication receipts. Database integrity and all 575,121 historical
observations pass the post-publication audit. Full parity remains open.

### Pure parity-selected affine reduction — 2026-10-13

Wago `b1cd6134` reduces a proven pure i32 unit-stride sum whose affine branches
are selected by counter parity (optionally inverted). The declared counter
must start at zero; unsigned do-while semantics, final counter and modulo-2^32
coefficients/accumulation are preserved. Both backends use arithmetic
progressions. Memory, globals, calls, traps, nested control, mismatched counters
and interruptible entries reject this proof. Independent scalar execution tests
cover negative/overflowing coefficients, both ABIs, disabled optimization and
artifact roundtrips. ARM64 and native AMD64 diagnostics check unsigned extrema
and disabled/interruptible admission. Guarded race and pinned ARM64 Core 3/threads
conformance pass.

A fresh locked five-contract capture retains all four phases and 15 successful
timing samples per row. For `core-ctl/branch-mixed/performance-4096x64`, steady
execution drops from **164,513.5 ns** at `283ec369` to **127.7 ns**, versus
**18,184.4 ns** at Clang w2c2 `f303ed27` (current/peer **0.00702**, about
142x faster). The same-contract parity assertion fails on the old source at
9.047x and passes on the new source. First-call drops from 181,208 ns to 208 ns.
The four controls change +0.8% to +1.7%; global swap/accumulate remain faster
than the peer, while division and rotate/popcount remain slower.

The selected branch module's cold compile median rises 5.25% (23,792 to
25,041 ns), and its native image grows 96 bytes (400 to 496). Alternating
compile corpus pairs show Core 3 +2.59%, threads +1.52%, unchanged aggregate
code sizes and effectively unchanged allocations. Median RSS changes -2.05%
and +1.01%, respectively. This records the measured cost, not compile-cost
neutrality. The [raw report](docs/benchmarks/wasm3-2026-10-13/parity-affine-sum.json)
retains samples, hashes, parity checks, compile rows and validation output.
Five new Wago captures are published locally; database integrity and all
575,121 historical observations remain intact. Native AMD64 timing, callbacks,
sign/bit operations, division and full feature parity remain open.
