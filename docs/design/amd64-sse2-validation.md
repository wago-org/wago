# AMD64 SSE2 scalar migration checkpoint

This is an intermediate implementation report, not the final SSE2 acceptance
report. Work starts at `1f137e8e6` (merged #693/#696). The complete original
instruction-family inventory and remaining migration order are in
[the audit](amd64-sse2-audit.md).

## Implemented coverage

- Backend compile-time capability mask and explicit CPU-profile injection.
  Selection is copied into reusable compiler scratch; there is no mutable global
  test CPU state and no feature dispatch in generated guest invocation code.
- Legacy scalar add/sub/mul/div, sqrt, abs/neg/copysign, conversion zeroing,
  and existing SSE2 comparisons, min/max, conversions and loads/stores.
- All eight scalar rounding operations: integer bit decomposition, signed zero,
  subnormals, all exponent ranges, infinities, quiet/signaling NaNs and nearest
  ties to even. No runtime helper or FP-state mutation.
- Existing CLZ/CTZ/POPCNT fallbacks remain, and explicit feature masks constrain
  the legacy bit-count selection field. BMI2 rotate selection is constrained too.
- Legacy vector movement in ABI/spills/locals and scalar bulk-memory paths;
  baseline fill-pattern construction; AVX bulk paths are selected at compile time.
- Incomplete SIMD/plugin profiles fail closed. Public CPU admission remains
  unchanged. SIMD and relaxed-SIMD are **not** claimed baseline-compatible.

The provisional mask has SSSE3, SSE4.1, SSE4.2, AVX, AVX2, BMI1, BMI2, LZCNT,
POPCNT, and FMA bits. It is not yet the final unified host/artifact model. The
explicit-selection flag preserves existing internal compiler defaults during
migration. The current SSE4.1/AVX scalar optimizations remain available.

## Semantic and instruction checks

The rounding tests cover each exponent, both signs, fraction boundaries, ties
and neighboring values, plus 8,192 deterministic random bit patterns per width.
Standalone baseline emitters are compared with reference results and, where
available, SSE4.1 instructions. Compiled scalar tests use SSE2, SSSE3, SSE4.1,
SSE4.2, modern AVX, and modern-plus-BMI1/POPCNT profiles. Alias tests cover local
assignment into either source; memory tests cover overlapping copies and fills
across scalar/XMM/YMM thresholds.

GNU objdump validates instructions rather than matching byte substrings. The
current checks cover rounding stubs and representative complete scalar objects
without literal pools. Full SIMD/object validation with data-island boundaries
is still required.

## Focused measurements

Measured on AMD Ryzen 7 8845HS with standard Go 1.27.1, three short repetitions.
These measurements do not establish a performance improvement. The execution
microbenchmark includes the runtime invocation boundary and is unsuitable for
isolating a few cycles of rounding latency.

| Operation/profile | Compile ns/op range | Native bytes | B/op | allocs/op |
|---|---:|---:|---:|---:|
| f32 add, SSE2 | 3323–3418 | 59 | 8136 | 22 |
| f32 add, modern | 3030–3251 | 54 | 8136 | 22 |
| f64 add, SSE2 | 3305–4479 | 59 | 8136 | 22 |
| f64 add, modern | 3183–3469 | 54 | 8136 | 22 |
| f32 nearest, SSE2 | 3375–3575 | 231 | 8920 | 23 |
| f32 nearest, modern | 3000–3167 | 46 | 8088 | 21 |
| f64 nearest, SSE2 | 3496–3606 | 256 | 8920 | 23 |
| f64 nearest, modern | 3062–3452 | 46 | 8088 | 21 |

The larger baseline rounding objects require more compilation buffer capacity;
whole-module compilation is not allocation-free. Reusing the emitter buffer
measured **0 B/op and 0 allocs/op** for both profiles. Emission alone measured
133–141 ns for the fallback versus 6.4–6.7 ns for one ROUND instruction. The
rounding sequence alone is 196 bytes (f32) or 217 bytes (f64), versus 7 bytes for
the tested high-register SSE4.1 encoding. No invocation allocation was measured.

| nearest execution, including invocation | ns/op range | B/op | allocs/op |
|---|---:|---:|---:|
| f32 SSE2 | 21.62–22.15 | 0 | 0 |
| f32 modern | 21.74–25.29 | 0 | 0 |
| f64 SSE2 | 21.47–22.02 | 0 | 0 |
| f64 modern | 22.28–30.08 | 0 | 0 |

The existing small-scalar compilation benchmark retained 22 allocations/op
before and after. Short-run timing and pooled B/op were noisy during build
activity; these are not evidence of an improvement or a completed modern-path
performance signoff. The full requested scalar/integer/SIMD benchmark matrix
remains pending.

## Validation status

Passed during development:

- Encoder, AMD64 backend and frontend package tests.
- Public Wago suite with `-skip '^TestStaged'` (including its codec/CPU tests).
- `just lint`, documentation-link validation, and `git diff --check`.
- CI-pinned TinyGo 0.41.1 / Go 1.22.12 runtime and public API tests.
- Forced-SSE2 f64 nearest standalone execution under TinyGo, including signed
  zero, half ties and infinities. Reproduce with:
  `tinygo run -scheduler=tasks ./tests/tools/amd64-sse2-check`.

The full AMD64 backend test package cannot currently compile under TinyGo:
existing `compile_test.go` assertions index the standard-Go-only scratch result
array, which has zero length under TinyGo. The standalone probe avoids that
unrelated test-package limitation. No claim of a full backend TinyGo test pass
is made.

Official core/relaxed SIMD fallback suites and full remote CI have not been run.
Artifact requirements, artifact versioning and plugin requirement consolidation
remain unchanged and incomplete. The #693 gate continues to protect old hosts.

## CI-toolchain binary size: acceptance gate still failing

Measured with the repository's exact `scripts/size-card.sh`, Go 1.22.12 and
TinyGo 0.41.1, Linux AMD64, without changing budgets. The initial revision was
measured in a separate checkout with the same toolchain. VCS stamping was disabled
for the temporary baseline checkout after TinyGo's Go loader rejected its VCS
lookup; the normal working-tree size command ran without that workaround.

| Profile | Initial bytes | Current bytes | Delta | Budget |
|---|---:|---:|---:|---:|
| manager | 7,970,968 | 7,970,968 | 0 | 9,000,000 |
| runtime-standard | 8,155,288 | 8,175,768 | +20,480 | 8,870,000 |
| runtime-minimal | 7,831,704 | 7,843,992 | +12,288 | 8,560,000 |
| runtime-minimal-tiny | 2,347,752 | 2,353,264 | +5,512 | 2,352,000 |

The TinyGo minimal binary exceeds its existing budget by **1,264 bytes**.
Consequently this checkpoint does not pass the release-size gate and is not
ready for final acceptance. Sharing the legacy vector memory encoder reduced
some duplication; no-inline/build-tag experiments that failed to produce useful
savings were removed. Further compacting of the fallback compiler is required.

An allocation profile of the baseline rounding compile benchmark identified
native code buffer growth (`CodeBuffer.growPreserving` and encoder buffer append
sites), supporting the explanation for its extra allocations. There is no
retained per-instruction fallback representation or runtime feature map.

The next implementation stages are core SIMD, relaxed SIMD, host/TinyGo CPU
mask consolidation, emitted-instruction artifact requirements and versioning,
then admission-gate relaxation and final performance/size/conformance signoff.
