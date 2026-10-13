# Current feature performance comparison

Fresh wasm.fyi source build: 111 execution contracts, 4 separate lifecycle probes,
and 9 separate compilation probes. Apple M4 Max, Darwin/arm64. Wago adapter
revision `4680ca19`; wasm.fyi `d64d55c`; SIMD-capable w2c2 `f303ed27`.
Three processes per runtime and contract, five verified samples per process,
two warmup batches, and at least 50 ms per measured batch. Exact source
fingerprints, commands, bytecode hashes, process medians, CV ranges and failure
reasons are retained in `docs/benchmarks/wasm3-2026-10-12/feature-refresh-results.json`.
There were no concurrent builds, tests or other timing captures.

Wago succeeds on 103 contracts in each process; its eight unsupported contracts
need JavaScript host profiles (five JSPI and three JavaScript string builtins).
The cancelled native Go equivalents remain excluded. w2c2 succeeds on 67,
reports 22 unsupported and fails 22 consistently in each process. Most failures
are translation rejections; multi-memory addressing and private secondary
loads/stores trap. No failed or unsupported pair receives a numeric ratio.
The comparison is contract-specific and does not establish full feature parity.
Phase 4 wide arithmetic and compact imports have separate implementation tests
but are absent from this source corpus, so corpus completeness is also unproven.
Native AMD64 validation remains pending hub connectivity.

Largest paired execution gaps, ns per entire repeated guest invocation:

| Contract | Wago | Unchecked w2c2 | Ratio |
| --- | ---: | ---: | ---: |
| `features/core-ctl/direct-call/performance-4096x64` | 204,097.1 | 304.0 | 671.414 |
| `features/core-num/signed-zero-bits/performance-4096x64` | 149,796.5 | 295.1 | 507.538 |
| `features/saturating-float-to-int/f64-unsigned-negative-clamp/performance-4096x64` | 136,164.7 | 300.3 | 453.382 |
| `features/simd/i32x4-add-multiply/performance-4096x64` | 111,418.3 | 301.4 | 369.611 |
| `features/core-glb/i64-global-split/performance-4096x64` | 106,780.0 | 303.4 | 351.951 |
| `features/mutable-globals/i64-global-split/performance-4096x64` | 105,914.5 | 303.0 | 349.590 |
| `features/mutable-globals/global-swap/performance-4096x64` | 102,422.5 | 303.1 | 337.927 |
| `features/core-glb/global-swap/performance-4096x64` | 101,067.9 | 303.9 | 332.563 |
| `features/core-num/integer-multiply-add/performance-4096x64` | 84,389.2 | 293.0 | 288.033 |
| `features/simd/arithmetic-scalar-baseline/performance-4096x64` | 80,706.4 | 298.5 | 270.402 |
| `features/core-glb/mutable-accumulate/performance-4096x64` | 74,532.4 | 304.7 | 244.574 |
| `features/core-glb/immutable-constant/performance-4096x64` | 72,957.3 | 299.6 | 243.494 |
| `features/mutable-globals/mutable-accumulate/performance-4096x64` | 74,055.6 | 304.3 | 243.397 |
| `features/mutable-globals/exported-read/performance-4096x64` | 70,962.4 | 309.9 | 228.952 |
| `features/threads/host-opaque-identity/performance-4096x64` | 14,740,906.2 | 199,861.2 | 73.756 |
| `features/core-ctl/branch-mixed/performance-4096x64` | 180,532.1 | 19,471.2 | 9.272 |
| `features/sign-extension/i64-extend32-high-word/performance-4096x64` | 92,825.2 | 12,217.3 | 7.598 |
| `features/sign-extension/i64-extend8/performance-4096x64` | 84,464.4 | 12,462.8 | 6.777 |
| `features/sign-extension/i64-extend16/performance-4096x64` | 85,079.3 | 12,591.6 | 6.757 |
| `features/core-num/integer-rotate-popcount/performance-4096x64` | 80,457.9 | 12,307.3 | 6.537 |
| `features/sign-extension/i32-extend8/performance-4096x64` | 79,158.4 | 12,399.2 | 6.384 |
| `features/sign-extension/i32-extend16/performance-4096x64` | 78,602.2 | 12,437.9 | 6.320 |
| `features/core-num/integer-div-rem/performance-4096x64` | 248,564.0 | 43,310.3 | 5.739 |
| `features/core-glb/f32-rounding-state/performance-4096x64` | 889,456.6 | 176,796.5 | 5.031 |
| `features/mutable-globals/f32-rounding-state/performance-4096x64` | 875,465.2 | 182,833.9 | 4.788 |

The largest gaps involve loops that the C compiler can simplify much further.
The local multiply-add contract remains 84,389 vs 293 ns (288x). The next
candidate extends the existing source-proven integer reduction to pure affine
terms, retaining unsigned do-while semantics and modular arithmetic. Direct
calls, global state recurrences, conversions, narrow/sign operations, floating
state and other compositions still require work. This checkpoint preserves the
full requested scope; unavailable peer features are not treated as completed.
