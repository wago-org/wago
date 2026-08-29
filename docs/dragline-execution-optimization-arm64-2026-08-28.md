# Dragline application execution diagnosis — ARM64 — 2026-08-28

Dragline's implementation roadmap is present, but its performance plan is not
complete. The application execution gate still fails: after this optimization
slice, Dragline is 2.374x Railshot by geometric mean across the 30 executable
application modules on an Apple M4 Max. Lower is better.

This report supplements
`dragline-railshot-cranelift-corpus-arm64-2026-08-28.md`. The original report's
release-shaped execution measurements use four 100 ms samples per export. The
diagnostic corpus passes below use three alternating 50 ms samples per export;
the focused `matmul` checks use five alternating 100 ms samples. A release
decision requires refreshing the longer paired Dragline/Railshot/Cranelift run.

## Root cause

The minimized `matmul(64)` case initially measured 8.781x Railshot. Scaling the
matrix from 8 to 64 increased the ratio from 2.266x to 8.948x, locating the
problem in the hot nested loop rather than invocation or host-call overhead.

The initial Dragline body contained 1,587 ARM64 instructions and 777
stack-relative memory operations. Railshot emitted 638 instructions and 39
stack-relative operations. Dragline's structured emitter coupled two independent
decisions: it used registers for the operand stack only when every local also
fit in the local-register set. `matmul` has 21 locals but a shallow operand
stack, so every Wasm intermediate was unnecessarily written to and read from
the native frame.

After separating those decisions, the Dragline body has 1,276 instructions and
187 stack-relative operations. Remaining hot-loop debt is visible in repeated
explicit bounds checks, integer address construction, local-home traffic, and
floating-point values represented in GPR-backed operand slots.

## Implemented optimizations

1. Shallow scalar operand stacks now remain in registers even when the
   function has more locals than the full structured-register mode can hold.
   Existing local-register peepholes retain their stricter original gate.
2. Eligible scalar functions cache the linear-memory byte length in a reserved
   register. Calls and `memory.grow` disable the cache, so neither a callee nor
   the function can make the value stale.
3. Adjacent same-width floating-point binary operations reuse FP scratch
   registers. They remain two distinct instructions; using FMA would violate
   WebAssembly's separately rounded operation semantics.
4. Floating-point memory loops are admitted to RailMach when the rest of the
   function is supported. Rust `matmul` still uses the structured path because
   its setup includes `memory.fill` and saturating conversion, which RailMach
   does not yet finalize.

## Measured progression

| State | `matmul` Dragline | D/R ratio | 30-app D/R geometric mean | Native bytes |
|---|---:|---:|---:|---:|
| Original report / minimized reproduction | 1,345.7 us | 8.781x | 3.048x | 6,348 |
| Register-backed shallow operand stack | 437.1 us | 2.981x | 2.428x | 5,104 |
| Cached immutable memory length | 401.5 us | 2.684x | 2.403x | 4,952 |
| FP binary-pair scratch reuse | 390.5 us | 2.593x | 2.374x | 4,936 |

The focused `matmul` result is a 71.0% latency reduction and a 22.2% native-code
reduction from the minimized starting point. The application-suite ratio
improved by 22.1%, from 3.048x to 2.374x Railshot.

## Corpus campaign update — `blake-as` — 2026-08-29

The first corpus-by-corpus pass remeasured `blake-as.hashN(100)` on the current
branch before changing code. Five alternating 100 ms samples put Dragline at
661.6 us and Railshot at 387.4 us, or 1.708x Railshot. This is the relevant
baseline for the new allocator work; it supersedes the older 13.646x row below.

RailMach's greedy allocator previously ranked intervals by weighted live-range
area. In the 1,080-instruction BLAKE compression kernel, that score retained
long-lived, sparsely used state while repeatedly spilling short-lived values
used in the hot straight-line rounds. Large integer-only functions now rank
those intervals by squared weighted-use density. Smaller functions and
functions with FPR values retain the conservative area policy.

The final nine-round alternating 500 ms pass measured Dragline at 374.8 us and
Railshot at 380.0 us by median: 0.986x Railshot, or 1.4% faster. Relative to the
fresh pre-change Dragline baseline, execution latency fell 43.3%. The kernel's
native body moved from 7,256 to 5,108 bytes (-29.6%), ARM64 instructions from
1,808 to 1,270 (-29.8%), and stack-relative references from 693 to 162 (-76.6%).

Every one of the manifest's 36 executable exports completed with the new
policy. A three-round alternating 50 ms before/after diagnostic pass improved
their execution geometric mean by 2.1%; `blake-as-simd` also improved by 14.8%.
Those short suite-wide timings are prioritization evidence, not a replacement
for the release-shaped paired report.

## Corpus campaign update — `utf-as-simd` — 2026-08-29

The next corpus pass started with `convertN(200)` at 6.435x Railshot in the
three-round prioritization run. Its structured ARM64 body was 1,596 bytes and
contained 391 instructions, including 160 stack-relative references. The
scalar operand stack was disabled solely because the function also used v128,
even though v128 values already had a separate vector-register stack.

The mixed structured path now keeps up to four scalar operands in X9-X12,
pins eleven integer locals without consuming the cached memory-bound register,
materializes byte splats with `MOVI`, writes vector operations directly to their
stack destinations, and fuses `i8x16.bitmask` plus `i32.popcnt`. Pinned scalar
local arithmetic and comparisons bypass redundant operand-stack shuffles.
SIMD memory checks reuse the immutable memory length, and their exact Wasm trap
bodies are outlined after the normal return rather than occupying hot-loop
layout.

The progression below uses seven alternating 300 ms samples per backend on the
same Apple M4 Max. Values are medians.

| State | Dragline | Railshot | D/R | Function bytes |
|---|---:|---:|---:|---:|
| SIMD scalar stack + bitmask/popcnt + `MOVI` | 111.84 us | 55.54 us | 2.014x | 1,160 |
| Direct local and constant flow | 85.70 us | 55.34 us | 1.549x | 984 |
| Pinned locals + cached SIMD bounds | 77.98 us | 55.62 us | 1.402x | 936 |
| Direct vector destinations | 66.26 us | 55.65 us | 1.191x | 904 |
| Scalar update/reduction/branch folds | 58.86 us | 55.38 us | 1.063x | 768 |
| Cold SIMD memory traps | 45.92 us | 55.05 us | **0.834x** | 764 |

The final result is 16.6% faster than Railshot and 51.9% smaller than the
initial Dragline function. All 30 runnable and six compile-only corpus modules
remain admitted, and every executable export agrees with Railshot. A focused
regression also executes the fused bitmask/popcount result and verifies that an
out-of-bounds vector load reaches the outlined linear-memory trap.

A fresh three-round alternating 100 ms prioritization pass across all 36
executable exports puts the Dragline/Railshot execution geometric mean at
1.099x, down from 1.198x before this corpus slice. Dragline is faster on 19 of
36 exports. The remaining top four application gaps are now
`json-as-simd.deserializeN` (2.806x), `json-as-simd.serializeN` (2.533x),
`blake-as-simd.hashN` (1.989x), and `sha256.hashN` (1.558x).

`validateN(200)` is complete for this campaign slice. SIMD-only functions now
cache the immutable memory bound, and repeated vector constants and shuffle
masks remain in otherwise-unused vector registers. A direct fold for
`i8x16.bitmask != 0` avoids reconstructing a scalar bitmask when only its
nonzero state is observed. Expanding the mixed scalar operand stack from four
to six registers also removed avoidable frame traffic in the large validator.

The progression below uses seven alternating 300 ms samples per backend on the
same Apple M4 Max. Values are medians.

| State | Dragline | Railshot | D/R |
|---|---:|---:|---:|
| Six scalar operand registers | 268.17 us | 139.93 us | 1.916x |
| Cached SIMD-only memory bound | 263.59 us | 138.75 us | 1.900x |
| Cached vector constants with alias flow | 224.40 us | 139.86 us | 1.605x |
| Direct bitmask-nonzero fold | 202.87 us | 139.56 us | 1.454x |
| Cached shuffle masks | 134.62 us | 139.77 us | **0.963x** |

The final focused result is 3.7% faster than Railshot. The validator's native
body fell from 11,724 to 6,564 bytes, a 44.0% reduction. The independent
all-corpus prioritization pass measured `validateN` at 0.955x Railshot.

## Historical application outliers

The table below predates the `blake-as` and `utf-as-simd` campaign slices and is
retained as the original diagnostic snapshot, not current ranking evidence.

The final three-round diagnostic pass produced these high-priority ratios:

| Module | Dragline | Railshot | D/R |
|---|---:|---:|---:|
| `blake-as` | 5,424.7 us | 397.5 us | 13.646x |
| `raytrace` | 2,418.1 us | 255.5 us | 9.462x |
| `blake-as-simd` | 3,452.0 us | 420.5 us | 8.210x |
| `float` | 16.936 us | 2.612 us | 6.485x |
| `nbody` | 906.1 us | 143.9 us | 6.298x |
| `spectralnorm` | 2,114.9 us | 384.9 us | 5.494x |
| `globals` | 3.109 us | 0.588 us | 5.291x |
| `matmul` | 387.6 us | 149.5 us | 2.592x |

No application module is faster than Railshot yet. The next coherent work is
not another corpus-specific peephole. It is:

1. give RailMach complete lowering for bulk memory and saturating conversions,
   then retest routing of large scalar loops;
2. carry typed floating-point operand values in FPRs instead of round-tripping
   through GPR bit representations;
3. strengthen induction/range proofs so loop memory checks can be eliminated or
   hoisted while preserving exact traps;
4. move cold trap materialization out of hot loop layout; and
5. diagnose `blake-as`, `raytrace`, and SIMD separately with counters on a
   qualified host.

## Verification

- `git diff --check`
- `go test ./...` in the main module: pass
- `go test ./...` in `bench`: pass
- 30 executable application modules, three alternating 50 ms samples per
  export, Dragline and Railshot: pass with matching execution
- focused `matmul`, five alternating 100 ms samples: pass

## Completion judgment

All numbered implementation phases in the ledger have code and verification
evidence. That is not the same as satisfying the master plan. Dragline remains
incomplete until the ledger's performance and platform gates pass, including
application execution, compile latency/allocation debt, native AMD64
qualification, and the unavailable LLVM comparison.
