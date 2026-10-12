# Initial wasm.fyi feature performance comparison

2026-10-12, Apple M4 Max, Darwin/arm64. This is an exploratory comparison, not a completed parity claim. One independent launch per adapter, five verified samples per contract, two measured warmup batches, with adaptive batching targeting at least 50 ms. Rows use nanoseconds per entire guest invocation, not per guest primitive. Each invocation includes the corpus repetition loop.

Wago: `0aaba49b1901404cbfd3c44da2f216bed6d1b517/source-7d57caed1ee5d15e2289beb40082c119ab27202617306c94b135a3f9d9383115`.
w2c2 SIMD fork: `f303ed27a8673d8cb7a6b7c39dd72a57a3b9999b`; translator identity `w2c2-clang:b33c05478a568a1def0b74cbf04244a657a9a112aa741998b54dfb63dd87fe15`.
Compiler: Apple Clang 21.0.0; `-O3 -march=native -fno-strict-aliasing`, with the Xcode SDK selected explicitly. The AOT adapter was built from an isolated copy of the existing hub adapter. Both adapters run in process through their ordinary export lookup/marshalling path.

Wago outcomes: {'ok': 77, 'unsupported': 13}. w2c2 outcomes: {'ok': 51, 'unsupported': 27, 'failed': 12}.
51 pairs have successful exact oracles and matching artifact SHA-256. All paired batches duration-qualified: True. Scalar baseline controls remain in this table and do not establish proposal coverage.

Several deterministic arithmetic loops run in hundreds of nanoseconds after C compilation. Loop simplification is an inference from the timings; generated-code inspection is still required. These gaps are retained because closing them is part of the performance objective.

Wago JSPI, JS-string and defined-shared-memory contracts are rejected by host-profile handling. The thread corpus requires local defined shared memory, beyond Wago's current exact-maximum imported shared-memory product. w2c2 failures and unsupported outcomes remain unpaired, with their raw reasons retained; no unavailable feature receives a numeric comparison.

| Workload | Wago ns/invocation | w2c2 ns/invocation | Wago / w2c2 |
| --- | ---: | ---: | ---: |
| `features/core-glb/mutable-accumulate/performance-4096x64` | 313851.1 | 268.7 | 1168.056 |
| `features/mutable-globals/mutable-accumulate/performance-4096x64` | 309382.4 | 272.3 | 1136.149 |
| `features/core-ctl/direct-call/performance-4096x64` | 191966.8 | 271.7 | 706.642 |
| `features/mutable-globals/global-swap/performance-4096x64` | 180274.6 | 272.8 | 660.773 |
| `features/core-glb/global-swap/performance-4096x64` | 171355.4 | 270.1 | 634.475 |
| `features/core-num/signed-zero-bits/performance-4096x64` | 146288.7 | 268.4 | 545.059 |
| `features/mutable-globals/i64-global-split/performance-4096x64` | 142919.4 | 271.0 | 527.361 |
| `features/core-glb/i64-global-split/performance-4096x64` | 140877.6 | 270.0 | 521.802 |
| `features/saturating-float-to-int/f64-unsigned-negative-clamp/performance-4096x64` | 132406.6 | 268.2 | 493.699 |
| `features/simd/i32x4-add-multiply/performance-4096x64` | 108740.2 | 272.5 | 399.084 |
| `features/core-num/integer-multiply-add/performance-4096x64` | 79023.2 | 264.4 | 298.833 |
| `features/simd/arithmetic-scalar-baseline/performance-4096x64` | 77510.4 | 269.5 | 287.641 |
| `features/core-glb/immutable-constant/performance-4096x64` | 73447.7 | 267.9 | 274.139 |
| `features/memory64/addressing-scalar-baseline/performance-4096x64` | 68440.0 | 275.4 | 248.469 |
| `features/threads/non-atomic-scalar-baseline/performance-4096x64` | 65906.7 | 271.6 | 242.673 |
| `features/mutable-globals/exported-read/performance-4096x64` | 66134.2 | 273.2 | 242.071 |
| `features/tail-call/iterative-recursion-scalar-baseline/performance-4096x64` | 66041.9 | 274.0 | 241.070 |
| `features/gc/access-scalar-baseline/performance-4096x64` | 65797.1 | 273.2 | 240.869 |
| `features/exceptions/normal-control-scalar-baseline/performance-4096x64` | 65932.2 | 273.9 | 240.748 |
| `features/core-ctl/branch-mixed/performance-4096x64` | 165343.6 | 18615.1 | 8.882 |
| `features/sign-extension/i64-extend32-high-word/performance-4096x64` | 88991.1 | 11109.7 | 8.010 |
| `features/sign-extension/i64-extend16/performance-4096x64` | 79800.8 | 11142.3 | 7.162 |
| `features/sign-extension/i64-extend8/performance-4096x64` | 79497.1 | 11267.0 | 7.056 |
| `features/core-num/integer-rotate-popcount/performance-4096x64` | 77887.3 | 11625.5 | 6.700 |
| `features/sign-extension/i32-extend8/performance-4096x64` | 72163.5 | 11125.1 | 6.487 |
| `features/sign-extension/i32-extend16/performance-4096x64` | 72538.8 | 11211.7 | 6.470 |
| `features/core-num/integer-div-rem/performance-4096x64` | 235280.1 | 40666.5 | 5.786 |
| `features/mutable-globals/i64-carry-overflow/performance-4096x64` | 180275.7 | 32524.1 | 5.543 |
| `features/core-glb/i64-carry-overflow/performance-4096x64` | 171508.4 | 32989.4 | 5.199 |
| `features/core-glb/f32-rounding-state/performance-4096x64` | 798436.6 | 159710.5 | 4.999 |
| `features/mutable-globals/f32-rounding-state/performance-4096x64` | 791706.8 | 164863.9 | 4.802 |
| `features/saturating-float-to-int/f32-in-range/performance-4096x64` | 131389.4 | 52172.2 | 2.518 |
| `features/saturating-float-to-int/f32-overflow-clamp/performance-4096x64` | 132983.9 | 54662.8 | 2.433 |
| `features/mutable-globals/dependent-read-write/performance-4096x64` | 253026.8 | 122580.7 | 2.064 |
| `features/core-glb/dependent-read-write/performance-4096x64` | 244297.4 | 122483.9 | 1.995 |
| `features/core-tab/indirect-call/performance-4096x64` | 329379.6 | 183311.9 | 1.797 |
| `features/mutable-globals/f64-global-scale/performance-4096x64` | 175977.1 | 99117.3 | 1.775 |
| `features/saturating-float-to-int/i64-in-range/performance-4096x64` | 141079.0 | 81376.4 | 1.734 |
| `features/core-glb/f64-global-scale/performance-4096x64` | 175206.5 | 103013.3 | 1.701 |
| `features/core-mem/load-store-sequential/performance-4096x64` | 147804.4 | 91847.5 | 1.609 |
| `features/simd/load-store-unaligned/performance-4096x64` | 168915.8 | 106351.5 | 1.588 |
| `features/bulk-memory/fill/performance-65536x64` | 47226.5 | 30902.6 | 1.528 |
| `features/core-mem/load-store-unaligned/performance-4096x64` | 134371.0 | 99752.1 | 1.347 |
| `features/bulk-memory/copy-overlap/performance-65536x64` | 90710.9 | 72200.7 | 1.256 |
| `features/simd/f32x4-sqrt/performance-4096x64` | 249614.1 | 206004.3 | 1.212 |
| `features/core-mem/narrow-load-sign-extension/performance-4096x64` | 146130.6 | 129812.0 | 1.126 |
| `features/simd/i8x16-saturating-add/performance-4096x64` | 78204.2 | 74279.8 | 1.053 |
| `features/simd/swizzle-lanes/performance-4096x64` | 77355.8 | 75207.1 | 1.029 |
| `features/core-num/f64-sqrt-convert/performance-4096x64` | 205807.1 | 202495.4 | 1.016 |
| `features/saturating-float-to-int/nan-and-infinity/performance-4096x64` | 222866.2 | 245843.5 | 0.907 |
| `features/core-num/f32-round-convert/performance-4096x64` | 190583.5 | 215771.5 | 0.883 |

Raw evidence is local and ignored by Git:
- `.validation/wasm3-parity/feature-baseline.json` SHA-256 `aa72548f8e03a770c25fd9ee68fb3bc6d55fb35fbdfb6b3016eed59b5cc34cc7`.
- `.validation/wasm3-parity/feature-w2c2-sdk.json` SHA-256 `db569696e703f5990039b5015e5c5cb7068c195454a692b118ae938ce2d206ef`.
- `.validation/wasm3-parity/feature-plan.json` and `feature-w2c2-plan.json` retain commands, workload artifacts, oracles and batching controls.

The probe previously sent JSON numbers for u64 arguments/oracles, which the strict native adapter rejected. wasm.fyi commit `a6f786a` now normalizes lossless values at its protocol boundary. The successful w2c2 capture uses this canonical representation; earlier failed protocol/SDK attempts remain separate files and are excluded.
