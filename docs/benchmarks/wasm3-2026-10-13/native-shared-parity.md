# ARM64 shared host-call near parity

Measured source `d9866a7a1b9686a7c57dab5db44dbbfbd5ad9a6a`, retained at merge `1d5f7bc9`, combines an immutable native-import entry guard, register arguments/results inside that proven native body, and narrow plain-i32 shared store/load forwarding. It is independently compared with retained `0666c2e3`, direct-argument precursor `2a920c5d`, and pinned optimized w2c2 Clang. All use the identical frozen Wasm artifact and contract, ordinary owned typed callbacks and WasmFunc.Invoke on Apple M4 Max, Go 1.27.1, `GOMAXPROCS=1` under the device lock. Each host contract executes 262,144 imported callback CALLs per invocation.

## Exact peer controls

Nine independent launches and 45 samples per successful phase in each order:

| Contract | Current / `0666c2e3` | Current / `2a920c5d` | Current / Clang |
| --- | --- | --- | --- |
| Ordinary shared host loop | 0.6812 / 0.6469 | 0.7980 / 0.7610 | **1.0197 / 1.0233** |
| Opaque shared host loop | 0.6795 / 0.6821 | 0.7641 / 0.7492 | **1.0189 / 1.0180** |
| Compact-import control | 1.0037 / 1.0015 | 1.0074 / 1.0018 | **1.0346 / 1.0268** |

Thus the targeted thread-host kernels are within 2.4% of Clang; compact imports remain within 3.5%. This is near parity on these contracts, not equality of every feature aggregate or an arbitrary Go-transition speedup. Thread-target compile time rises 22–37% versus `0666c2e3`; each image grows 960→1,232 bytes (+28.3%). The complete phase/RSS/sample evidence is retained.

## What executes

A runtime native-leaf tag guards function entry, selecting a body only for immutable, bounded integer import shapes with fixed shared memory32. The validated native target is cached; a single i32 argument can avoid stack staging only with nontrapping preceding integer values and canonical homes. The native register-entry ABI returns a canonical i32, so the next arithmetic can consume X0 directly. The ordinary Go/foreign path remains complete. No callback is replaced with an assumed identity result: actual BLR calls remain, and nonidentity negative/bit-mixing callbacks are tested.

The shared forwarding change intentionally permits a redundant physical load to disappear. Only an immediately repeated, identical full-width plain i32 load can reuse its preceding plain store in one defined fixed shared memory32. The real store, its bounds/trap, and later import CALL remain. It never crosses a callback, atomic/fence, local mutation, custom instruction or trapping/non-pure address expression. Profiling, different addresses/widths, memory64, growable/imported/multiple memories retain their required paths. The existing store-forward optimization selection can also disable forwarding.

The own-store witness argument in `shared-memory-forwarding-semantics.md` treats this as allowed-behavior refinement under the threads relaxed-model draft. Races have defined nondeterministic behavior; this is not a C race-undefined-behavior argument. This explicitly supersedes the earlier additional promise to retain every physical guest load for this new optimization. Profiling keeps the physical accesses.

A separate diagnostic cloned the source and deliberately zeroed values at three points: guarded native return, direct native argument, and forwarded load. Exact typed-entry ordinary/opaque oracles failed with zero as expected at every point; effectful Go controls bypassed the native-only changes and retained 262,144 callbacks. The forwarding perturbation still reached all 262,144 Go callbacks with changed data. This proves the optimized paths are reached by the benchmark API. The diagnostic patch is archived, restored and excluded from the candidate; these are branch checks, not timing captures.

## Full-corpus and compatibility qualification

All 266 contracts per source complete (532 captures, 15 samples per successful phase), without outcome changes or collection/resource failures. Against `0666c2e3`, steady geometric mean improves 0.99% over 255 numeric pairs and 2.01% over 107 feature pairs. Compile geometric mean changes -0.42%, instantiate -0.40%, first-call -1.74%, and compile peak RSS -0.07%. These full-pass aggregates are separate from focused reversed controls.

All 373 canonical compatibility contracts match `0666c2e3` exactly: 346 pass, 27 unsupported, zero fail. Broad ordinary/checker backend/runtime/public tests and focused profiling tests pass, including native/shared OOB, canonical result bits, source/artifact/Go/foreign fallback, cancellation and GC progress.

All 255 image hashes/byte counts match SDK captures. Exactly four images change: the two host loops 960→1,232, ordinary shared load/store 448→432, and shared table-indirect 800→784. Disabling new shared admission and forwarding reproduces all 255 baseline images byte for byte.

The fresh-process public Compile control over 145 core applications uses five launches/source/artifact/order versus pinned main `069a26ea`. Compile time rises 7.92%/8.26%, allocations 0.48%/0.48%, and process peak RSS 2.66%/2.66%. Input/config creation is outside the compile timer; process RSS includes startup through close. It supplements SDK measurements and is not an execution comparison against main.

Reversed outlier review completes 64 selected workloads, 256 captures, 9 launches and 45 samples per successful phase/order. One steady loss above 5% repeats: fastfloat decimal parsing **+10.48%/+5.18%**. Its guest image is unchanged; the cause remains unproven. Six compile, two instantiate and one first-call cases also exceed 5% in both orders among this selected set. Full rows, including improvements, are preserved; these selected counts are not a whole-corpus failure rate.

The rechecks independently confirm ordinary host loops **-32.63%/-32.58%**, opaque host loops **-33.31%/-32.06%**, ordinary shared load/store **-10.05%/-10.58%**, and shared table-indirect calls **-33.90%/-31.38%**, versus `0666c2e3`. The known loss and startup costs preclude a regression-free claim.

## Delivery scope

ARM64 only. Existing w2c2 Clang -O0 compatibility outcomes and optimized numeric peer controls remain distinct; failed/unsupported complete-feature markers are preserved. No production deployment. Source `d9866a7a` is published only to the local server. All 639 captures / 1,437 observations are verified, all 615,246 prior observations are preserved, SQLite quick_check passes and foreign-key violations are zero. Browser verification at the requested mean/performance URL sees 845 current feature rows, 22 numeric Wago feature groups, Compact Import Section ~344 µs and Threads ~255 µs, with no errors. The compatibility page also sees the new source without errors. Receipts, source/capture hashes, preserved-history proof and browser evidence are in `local-publication-native-shared-parity.json`; `local-native-shared-parity-features.png` shows the verified view. Existing AMD64/native/peer/history data remain intact.

The readable aggregates/outliers are in `native-shared-parity.json`; `native-shared-parity-evidence.json.gz` includes complete captures, the four-source peer comparison, fresh-process main controls, image audit and entry-path diagnostic. The diagnostic patch was confined to a separate checkout, restored after the checks and never published. These compiler/runtime sources match all 3,214 measured source-input hashes after integration.
