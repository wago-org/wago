# AMD64 SSE2 implementation and validation

This report supersedes the scalar migration checkpoint. Work started at
`1f137e8e6`, main after #693/#696. The original instruction and caller inventory
is preserved in [the audit](amd64-sse2-audit.md).

## Execution policy and coverage

AMD64 uses SSE2 as its architectural baseline. Optional CPU extensions select
lowerings at compile time, and emitted requirements are recorded in native
artifacts. There is no new invocation-time CPU dispatch, helper call, cgo
requirement, or retained fallback IR. v128 remains in XMM registers with the
existing allocator, ABI, locals, spills and control merges.

Coverage includes ordinary scalar integer and floating-point code, all eight
scalar rounding operations, bit counts, memory/bulk memory, table movement,
core SIMD and the currently supported deterministic relaxed SIMD operations.
The public runtime passes detected capabilities into compilation. The
`wago_amd64_sse2` build tag restricts generated code to SSE2 on modern hosts and
provides an immutable conformance/artifact-generation profile. Direct backend
callers can inject value-based masks; omitting the explicit-selection option
retains their historical modern tier for source compatibility.

CPU detection failure still rejects native execution. Missing AVX, SSSE3,
SSE4.x, BMI1, BMI2, LZCNT or POPCNT does not reject ordinary Wasm. Requesting an
unavailable BMI2 rotate optimization selects the baseline rotate lowering.
Trusted native plugins must declare their optional requirements and are rejected
before emission when the selected capabilities are insufficient.
Managed plugin vector helpers also check and record their actual instructions,
including helpers with no error return. Full-access raw emitters retain the
existing trusted declaration contract.

## Original dependencies and implemented alternatives

| Original instruction family | Optional feature | Baseline implementation |
|---|---|---|
| VEX scalar arithmetic, sqrt, logical operations, conversion zeroing | AVX + OS state | Legacy SSE/SSE2; preserve destructive-source aliases |
| ROUNDSS/ROUNDSD | SSE4.1 | Integer IEEE-754 bit decomposition; signed zero, quiet NaNs, infinities and nearest-even |
| VEX packed arithmetic, logical, shifts, compare, pack/unpack, conversion, movement | AVX + OS state | Equivalent legacy SSE2 and explicit alias preservation |
| PSHUFB | SSSE3 | Sixteen bounded byte selections; raw relaxed high-bit-zero/low-nibble behavior preserved |
| PABSB/PABSW/PABSD | SSSE3 | Signed lane extraction, negate/select, reconstruction |
| PHADDD | SSSE3 | Scalar pair sums and reconstruction |
| PMADDUBSW | SSSE3 | Unsigned-byte × signed-byte pair sums, signed i16 saturation |
| PMULHRSW | SSSE3 | Signed products, rounding bias and arithmetic shift; existing core saturation fixup retained |
| PINSRB/D/Q, PEXTRB/D/Q | SSE4.1 | PINSRW/PEXTRW, MOVD/MOVQ and PSHUFD; preserve scratch registers |
| PCMPEQQ | SSE4.1 | Scalar qword equality and full-lane masks |
| PCMPGTQ | SSE4.2 | Scalar signed qword comparison; handles equal high words and unsigned low-word ordering naturally |
| PTEST | SSE4.1 | AND, PCMPEQB, PMOVMSKB and scalar comparison; only consumed ZF semantics are synthesized |
| PMULLD | SSE4.1 | Scalar dword products and reconstruction |
| PMULDQ | SSE4.1 | Sign-extended even-dword products |
| PMINSB/MAXSB, PMINUW/MAXUW, PMINSD/MAXSD, PMINUD/MAXUD | SSE4.1 | Signed/unsigned lane extension, compare and conditional move |
| PACKUSDW | SSE4.1 | Signed dword clamping to 0…65535 and word reconstruction |
| ROUNDPS/ROUNDPD, VEX packed rounding | SSE4.1 / AVX | Apply exact scalar rounding to each lane |
| LZCNT/TZCNT | LZCNT / BMI1 | Existing BSR/BSF with explicit zero handling |
| POPCNT | POPCNT | Existing inline SWAR for i32/i64 |
| RORX | BMI2 | Existing legacy rotate path |
| YMM VMOVDQU, VZEROUPPER in bulk/table paths | AVX + OS state | XMM/scalar movement paths |
| YMM arithmetic/VINSERTI128, EVEX/ZMM/VPTERNLOGD | Plugin AVX2 / AVX-512 tiers | Optional plugin declarations checked and persisted; no core-Wasm dependency |
| PHADDW, PBLENDW | Encoder/plugin vocabulary only | No admitted core lowering emits these; trusted plugin feature declaration remains required |

FMA and VNNI were not emitted by the original core backend and remain unused.
No core AES, PCLMUL, RDRAND, RDSEED or CMPXCHG16B emission was found. Scalar
atomic LOCK instructions and runtime ABI GPR/SSE2 operations remain baseline.
The audit links every original encoder instruction and its backend caller.

Core SIMD includes constants, memory variants, lanes, splats, shuffle/swizzle,
logical/select/reductions/bitmasks, integer and floating comparisons/arithmetic,
saturation, min/max, narrowing/extension, pairwise/extmul/dot, shifts and
conversions. Relaxed SIMD retains raw PSHUFB swizzle, bitselect lane selection,
saturating truncations, separate multiply/add or subtract, native min/max,
raw q15 rounding and the existing deterministic signed dot-product choices.

Nontrivial integer primitives use at most 80 bytes of reusable native-frame
scratch, including preserved GPRs. They are bounded inline sequences with no
runtime helper. Packed rounding reserves its vector snapshot separately from
allocator spills. Modern paths preserve the original instructions.

## Capability and artifact model

`shared.AMD64Features` is a uint32 with these bit assignments:

| Bit | Capability |
|---:|---|
| 0 | SSSE3 |
| 1 | SSE4.1 |
| 2 | SSE4.2 |
| 3 | AVX with OS XMM/YMM state |
| 4 | AVX2 with AVX state |
| 5 | BMI1 |
| 6 | BMI2 |
| 7 | LZCNT |
| 8 | POPCNT |
| 9 | FMA with AVX state |
| 10 | Plugin AVX-512 tier: F/DQ/BW/VL, AVX2, and OS opmask/ZMM state |

SSE2 has no optional bit. Detection is cached once without lookup allocation.
Standard Go uses CPUID/XGETBV; TinyGo reads Linux cpuinfo once and intersects
logical processors' flags. The existing compatibility test seams delegate to
the same cache. Compilation uses a per-compilation value, never a mutable global
CPU mask in the instruction emitter.

Artifact format version **3** places the capability mask in bits 32–51 of the
existing uint64 requirement word, without adding serialized bytes. The module's
runtime metadata consolidates the previous BMI2/bit-count/plugin booleans into
one mask. Unknown requirement bits and all older versions, including version 2,
are rejected. Baseline objects record zero optional features; selected optimized
instructions and plugin declarations contribute their requirements. Serial and
parallel compilation union requirements, and pooled scratch cannot leak flags
between compilations. Loading enforces `required ⊆ available`.

## Focused performance measurements

AMD Ryzen 7 8845HS, Go 1.27.1; three 50 ms samples per benchmark. Tables show
medians. The initial checkout and updated checkout use the same workload code.
These are short measurements on a shared development machine, not stable
performance-improvement claims. Invocation overhead is included. Modern native
code size **and CRC32 match the original for all 16 workloads**, with unchanged
compile allocation counts and zero invocation allocations.

### Compilation and emitted code

| Workload | Initial modern ns/op | Current modern ns/op | SSE2 ns/op | Modern code bytes | SSE2 code bytes | Modern B/op; allocs/op | SSE2 B/op; allocs/op |
|---|---:|---:|---:|---:|---:|---|---|
| f32_add | 2589 | 2779 | 2774 | 54 | 59 | 8136; 22 | 8136; 22 |
| f64_add | 2786 | 2828 | 2767 | 54 | 59 | 8136; 22 | 8136; 22 |
| f32_nearest | 2494 | 2649 | 3139 | 46 | 231 | 8088; 21 | 8920; 23 |
| f64_nearest | 2529 | 2737 | 3294 | 46 | 256 | 8088; 21 | 8920; 23 |
| f64_convert_i64 | 2483 | 2900 | 2772 | 49 | 48 | 8088; 21 | 8088; 21 |
| i64_clz | 2511 | 2838 | 2834 | 40 | 58 | 8088; 21 | 8088; 21 |
| i64_ctz | 2575 | 2785 | 2894 | 40 | 50 | 8088; 21 | 8088; 21 |
| i64_popcnt | 2565 | 2812 | 2888 | 40 | 127 | 8088; 21 | 8088; 21 |
| memory_copy | 4445 | 4768 | 4516 | 673 | 567 | 10680; 31 | 9528; 30 |
| swizzle | 2912 | 3086 | 4681 | 115 | 873 | 8200; 24 | 10472; 27 |
| i32_mul | 2900 | 3021 | 3306 | 63 | 165 | 8176; 23 | 8176; 23 |
| i64_gt | 2942 | 3060 | 3391 | 68 | 163 | 8176; 23 | 8176; 23 |
| i32_min | 2939 | 2876 | 3644 | 63 | 245 | 8176; 23 | 9136; 25 |
| f64x2_nearest | 2796 | 2861 | 3956 | 63 | 522 | 8112; 22 | 9904; 25 |
| relaxed_q15 | 2875 | 3004 | 3807 | 63 | 425 | 8176; 23 | 9136; 25 |
| lane_extract | 2564 | 2950 | 2927 | 57 | 72 | 8112; 22 | 8112; 22 |

### Execution

Every entry below measured **0 B/op and 0 allocs/op**.

| Workload | Initial modern ns/op | Current modern ns/op | SSE2 ns/op |
|---|---:|---:|---:|
| f32_add | 19.93 | 19.74 | 20.62 |
| f64_add | 19.86 | 20.02 | 20.11 |
| f32_nearest | 19.64 | 20.23 | 19.84 |
| f64_nearest | 19.92 | 19.52 | 20.05 |
| f64_convert_i64 | 19.77 | 19.60 | 19.79 |
| i64_clz | 20.20 | 19.53 | 19.30 |
| i64_ctz | 19.28 | 19.45 | 20.00 |
| i64_popcnt | 19.72 | 19.65 | 19.59 |
| memory_copy | 32.02 | 32.35 | 31.94 |
| swizzle | 20.13 | 19.79 | 33.51 |
| i32_mul | 20.27 | 19.57 | 25.83 |
| i64_gt | 20.22 | 19.73 | 25.11 |
| i32_min | 19.66 | 19.96 | 26.32 |
| f64x2_nearest | 19.84 | 19.66 | 29.07 |
| relaxed_q15 | 20.33 | 19.80 | 31.75 |
| lane_extract | 19.80 | 19.43 | 19.53 |

Modern execution medians differ by about −3.5% to +3.0% in these samples;
matching native bytes/checksums support preservation of the fast path.
Compilation medians are noisier and in several cases higher (up to about 17%,
roughly 0.4 microseconds); these short runs do not establish a stable regression
or improvement. Baseline code-buffer growth explains extra compilation
allocations for larger sequences; there are no new per-instruction objects,
per-module feature maps, or invocation allocations. Earlier allocation profiling
of scalar rounding identified encoder/code-buffer growth, and reusable emitter
benchmarks measured zero allocations.

Reproduce with `go test ./src/core/compiler/backend/railshot/amd64 -run '^$'
-bench '^BenchmarkAMD64Tiers$' -benchmem -benchtime=50ms -count=3`.
No 984-case benchmark campaign was run.

## Validation

- Encoder, AMD64 backend, frontend and public Wago (`-skip '^TestStaged'`) suites pass.
- The full public Wago suite also passes with `-tags wago_amd64_sse2`; tests of
  optional-instruction metadata respect the selected mask, and optional custom
  vector plugins are rejected. Tests specifically requiring an available native
  tier skip that success case under the forced baseline profile.
- `git diff --check`, `just lint` and `just docs` pass.
- Scalar rounding: exponent/boundary/NaN/signed-zero vectors and 8,192 deterministic
  random bit patterns per width, compared with reference and modern results.
- SIMD differential helpers exercise six masks; deterministic random tests cover
  optional primitive families, qword low-word tie breaks and swizzle indices 0–255.
- Objdump checks scalar and SIMD function instruction ranges, excluding literal
  pools, against an SSE2 allowlist. Extended scalar checks include bit counts,
  conversions, sign operations, comparisons and min/max.
- Official core SIMD, baseline **and** modern: **470 modules / 24,325 assertions**,
  zero failures or skips.
- Official relaxed SIMD, baseline **and** modern: **8 modules / 69 assertions**,
  zero failures or skips, from the pinned Release 3 corpus.
- Standard guard-page runtime/public suites and focused forced-SSE2 guard-page,
  memory and vector tests pass. Baseline GC/vector execution tests pass. Selected
  compiler-parallelism and CPU/artifact race checks pass. Linux ARM64 cross-build passes.
- TinyGo 0.41.1 with Go 1.22.12: encoder, runtime and public API suites pass. The forced-SSE2
  standalone probe passes scalar rounding, vector multiplication, raw relaxed q15,
  swizzle and packed rounding. The full backend TinyGo test package has an existing
  compile-time zero-length-array indexing error in `compile_test.go`; it is not
  included in this passing result.
- CI now includes forced-SSE2 core and relaxed SIMD conformance on Linux AMD64.
  Remote CI and merge status must be reported separately from local checks.

## Deferred size work and limits

TinyGo/minimal binary-size remediation is explicitly deferred to a separate PR
at the user's request. No size budget or enforcement was raised or weakened.
The previous scalar-only checkpoint measured 2,353,264 bytes against a 2,352,000
byte budget; that is **not** a measurement of this completed SIMD implementation.
A final size pass is not claimed here.

No admitted core or supported relaxed SIMD operation requires an optional CPU
extension under the explicit SSE2 profile. Native plugins may require their
specified tiers. The largest measured baseline object is the 873-byte swizzle
microbenchmark; future SSE2 vector-network optimizations could reduce scratch
traffic and code size. Optimizing these sequences must preserve the established
semantics and capability checks.
