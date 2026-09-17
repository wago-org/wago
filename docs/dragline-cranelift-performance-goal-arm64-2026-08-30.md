# Dragline application performance goal — ARM64 — 2026-08-30

This report records the optimization pass whose acceptance gate was a current,
paired application execution geometric mean below Cranelift. Measurements are
from an Apple M4 Max worktree based on `4cbeacd77523e2accfcc394f25bc93c094afeb4e`
with the changes described here applied. Lower is better.

## Result

Dragline meets the gate: across the 30 runnable application modules it is
`0.883x` Cranelift execution latency, or about 11.7% faster. It is `0.702x`
Railshot, or about 29.8% faster. The pre-pass paired result was `1.073x`
Cranelift, so this slice reduced the application execution geometric mean by
17.7% relative to that baseline.

| Corpus | Engine | Compile | Peak RSS | Execution | Native code |
| --- | --- | ---: | ---: | ---: | ---: |
| All 53 / 47 executable | Dragline | 8.108 ms | 12.29 MiB | 5.183 us | 5.48 KiB |
|  | Railshot | 6.008 ms | 10.54 MiB | 10.999 us | 6.46 KiB |
|  | Cranelift | 7.206 ms | 20.23 MiB | 8.751 us | 5.71 KiB |
| Applications 36 / 30 executable | Dragline | 10.328 ms | 13.18 MiB | 5.677 us | 7.15 KiB |
|  | Railshot | 6.867 ms | 11.35 MiB | 8.083 us | 6.77 KiB |
|  | Cranelift | 8.511 ms | 22.29 MiB | 6.428 us | 5.97 KiB |
| MVP ISA 17 / 17 executable | Dragline | 4.856 ms | 10.60 MiB | 4.413 us | 3.13 KiB |
|  | Railshot | 4.528 ms | 9.01 MiB | 18.939 us | 5.85 KiB |
|  | Cranelift | 5.065 ms | 16.47 MiB | 15.084 us | 5.20 KiB |

| Corpus | D/R compile | D/R RSS | D/R exec | D/R code | D/C compile | D/C RSS | D/C exec | D/C code |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| All | 1.349x | 1.166x | 0.471x | 0.849x | 1.125x | 0.607x | 0.592x | 0.960x |
| Applications | 1.504x | 1.161x | 0.702x | 1.056x | 1.213x | 0.591x | 0.883x | 1.198x |
| MVP ISA | 1.072x | 1.176x | 0.233x | 0.535x | 0.959x | 0.643x | 0.293x | 0.602x |

Dragline beats Cranelift on 13 of 30 application modules and 15 of 17 ISA
modules. It beats Railshot on 27 of 30 applications and all 17 ISA modules.
The geometric mean is below Cranelift because the large wins now outweigh the
remaining individual losses; this is not a claim that every application wins.

## Implemented changes

- Compiler-proven call-free integer leaves use a narrow Go-stack entry instead
  of constructing the full foreign-stack trap transition. A distinct ABI class
  also admits functions only after every source call has been replaced in the
  emitted body. `tiny` fell from about 15 ns to 4.7 ns, `branches` to 4.8 ns,
  `fib_iter` to 8.8 ns, and `many_funcs` to 4.7 ns.
- The full prepared-integer transition now preserves only the registers it
  actually overwrites; RailMach private entries already preserve allocated
  AAPCS64 callee-saves.
- Canonical i64 Fibonacci loops use a verified two-iteration recurrence, and a
  verified dense four-way i32 `br_table` lowers branchlessly.
- Repeated ARM64 add, subtract, comparison, and logical constants remain
  immediate operands instead of being repeatedly materialized.
- A verified scalar byte-widen chain lowers to NEON `uxtl`.
- Floating-point comparisons feeding control branches keep NZCV live through
  the branch instead of materializing `cset; cmp` booleans.
- Structured application functions cache hot memory bounds and promoted-global
  descriptor pointers, fuse checked promoted-global stores, and avoid redundant
  call-argument reloads.
- The Dragline function-artifact compiler revision was advanced so older ABI
  class and code-generation products cannot be reused by the cache.

## Remaining application gaps

These are the current Dragline/Cranelift module execution ratios. They remain
the next optimization queue even though the aggregate gate passes.

| Module | D/C execution |
| --- | ---: |
| `dispatch` | 1.553x |
| `json-as-simd` | 1.348x |
| `json-as` | 1.337x |
| `matmul` | 1.280x |
| `sieve` | 1.268x |
| `sha256` | 1.258x |
| `quicksort` | 1.249x |
| `raytrace` | 1.211x |
| `fannkuch` | 1.201x |
| `mandelbrot` | 1.180x |
| `memory_tree` | 1.132x |
| `blake-as-simd` | 1.112x |
| `nbody` | 1.107x |
| `blake-as` | 1.060x |
| `crc32` | 1.032x |

`dispatch` deliberately retains the trap-capable transition: its optimized
dense table still has an observable out-of-range `call_indirect` trap path. It
cannot use the call-free entry used by `many_funcs`.

## Per-module results

Columns are compile latency in ms, peak compile RSS in MiB, execution latency
in us, and native code in KiB for Dragline (`D`), Railshot (`R`), and Cranelift
(`C`). A dash marks a compile-only module. Multiple exports within one module
are combined with a geometric mean after taking each export's median.

| Module | D ms | D RSS | D exec | D code | R ms | R RSS | R exec | R code | C ms | C RSS | C exec | C code |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `arith` | 4.164 | 7.33 | 1.342 | 0.20 | 4.288 | 7.22 | 1.413 | 0.19 | 4.325 | 13.73 | 1.339 | 0.41 |
| `blake-as` | 10.721 | 10.84 | 381.434 | 10.95 | 4.715 | 9.31 | 389.457 | 11.14 | 5.991 | 17.59 | 359.757 | 7.32 |
| `blake-as-simd` | 11.931 | 13.00 | 399.243 | 32.30 | 5.518 | 11.31 | 398.837 | 31.31 | 7.000 | 20.28 | 359.164 | 20.51 |
| `branches` | 4.461 | 7.33 | 0.005 | 0.25 | 4.131 | 7.19 | 0.018 | 0.19 | 4.400 | 13.59 | 0.011 | 0.46 |
| `crc32` | 4.554 | 7.75 | 20.386 | 2.03 | 4.255 | 7.22 | 22.403 | 1.68 | 4.550 | 14.92 | 19.755 | 1.39 |
| `dispatch` | 4.522 | 8.80 | 0.020 | 0.40 | 4.143 | 8.25 | 0.020 | 1.30 | 4.855 | 15.55 | 0.013 | 1.72 |
| `esbuild` | 1407.023 | 487.12 | — | 64564.82 | 325.904 | 215.52 | — | 30615.77 | 515.955 | 490.48 | — | 15284.75 |
| `fannkuch` | 6.248 | 8.39 | 932.466 | 7.71 | 4.415 | 7.44 | 1201.130 | 5.22 | 5.274 | 15.14 | 776.598 | 2.50 |
| `fib_iter` | 4.193 | 7.33 | 0.009 | 0.19 | 3.967 | 7.11 | 0.025 | 0.18 | 4.693 | 13.70 | 0.015 | 0.40 |
| `fib_rec` | 4.174 | 7.47 | 1031.570 | 0.15 | 3.968 | 7.20 | 1023.770 | 0.27 | 4.415 | 13.66 | 1057.600 | 0.48 |
| `float` | 4.240 | 7.45 | 2.221 | 0.20 | 3.979 | 7.20 | 2.481 | 0.21 | 4.156 | 13.70 | 2.210 | 0.41 |
| `globals` | 4.397 | 7.84 | 0.489 | 0.23 | 4.212 | 7.69 | 0.559 | 0.34 | 4.337 | 14.44 | 2.059 | 0.75 |
| `isa_bulk_mem` | 4.682 | 9.75 | 1.073 | 4.34 | 4.559 | 9.08 | 1.464 | 6.11 | 5.096 | 16.12 | 1.522 | 3.36 |
| `isa_call` | 4.233 | 8.11 | 7.354 | 0.36 | 4.023 | 7.98 | 21.443 | 2.04 | 4.996 | 15.67 | 27.563 | 3.03 |
| `isa_cmp_f32` | 5.163 | 12.44 | 33.583 | 4.22 | 4.766 | 9.36 | 35.870 | 6.75 | 4.942 | 16.33 | 33.675 | 5.48 |
| `isa_cmp_f64` | 5.082 | 12.12 | 33.496 | 4.22 | 4.545 | 9.39 | 36.051 | 6.75 | 4.768 | 16.22 | 33.745 | 5.50 |
| `isa_cmp_i32` | 5.270 | 12.62 | 7.880 | 4.81 | 4.503 | 9.19 | 8.793 | 5.95 | 5.251 | 16.69 | 8.419 | 6.47 |
| `isa_cmp_i64` | 5.179 | 12.62 | 7.931 | 4.81 | 4.736 | 9.33 | 8.843 | 6.12 | 5.250 | 16.94 | 8.443 | 6.47 |
| `isa_ctl` | 4.424 | 8.48 | 0.420 | 0.38 | 4.291 | 8.72 | 12.036 | 3.72 | 4.864 | 16.17 | 5.598 | 3.25 |
| `isa_cvt` | 4.655 | 9.72 | 3.006 | 2.03 | 4.477 | 8.84 | 56.707 | 5.44 | 5.063 | 16.72 | 56.714 | 3.57 |
| `isa_cvt_mvp` | 5.601 | 13.44 | 17.299 | 9.96 | 4.880 | 9.75 | 100.041 | 26.12 | 5.484 | 17.52 | 92.999 | 26.43 |
| `isa_f32` | 5.270 | 12.44 | 9.650 | 8.41 | 4.406 | 9.11 | 47.522 | 9.58 | 4.927 | 16.36 | 45.120 | 6.57 |
| `isa_f64` | 5.104 | 12.64 | 11.806 | 8.48 | 4.491 | 9.03 | 48.992 | 9.21 | 5.007 | 16.25 | 46.489 | 6.57 |
| `isa_i32` | 4.658 | 9.00 | 2.427 | 8.09 | 4.838 | 9.44 | 31.726 | 11.77 | 5.470 | 18.09 | 26.572 | 11.06 |
| `isa_i64` | 4.841 | 9.83 | 1.456 | 8.48 | 4.772 | 9.56 | 31.689 | 11.83 | 5.455 | 17.38 | 26.881 | 11.07 |
| `isa_mem` | 4.642 | 9.81 | 7.955 | 3.04 | 4.465 | 9.12 | 11.674 | 5.30 | 5.042 | 16.39 | 7.810 | 4.21 |
| `isa_mem_narrow` | 5.300 | 12.34 | 6.483 | 10.33 | 4.839 | 9.47 | 10.720 | 14.58 | 5.473 | 17.69 | 8.676 | 11.07 |
| `isa_signext` | 4.411 | 9.31 | 1.100 | 0.90 | 4.157 | 8.39 | 7.819 | 1.53 | 4.731 | 15.39 | 0.917 | 1.56 |
| `isa_var` | 4.311 | 8.11 | 0.330 | 0.38 | 4.339 | 7.72 | 10.507 | 0.68 | 4.423 | 14.47 | 11.633 | 0.97 |
| `json-as` | 14.574 | 16.12 | 24.188 | 88.45 | 5.991 | 12.92 | 26.597 | 75.70 | 7.262 | 21.39 | 18.090 | 31.92 |
| `json-as-simd` | 9.279 | 15.33 | 33.127 | 110.22 | 6.019 | 13.45 | 33.931 | 90.32 | 7.550 | 21.53 | 24.577 | 35.23 |
| `linked_list` | 4.232 | 7.56 | 4.280 | 0.56 | 4.042 | 7.17 | 5.811 | 0.41 | 4.298 | 13.92 | 4.470 | 0.46 |
| `lua` | 44.183 | 29.86 | — | 1622.02 | 17.906 | 21.58 | — | 984.39 | 26.579 | 47.11 | — | 505.28 |
| `mandelbrot` | 4.333 | 7.39 | 214.053 | 0.54 | 4.034 | 7.28 | 213.352 | 0.43 | 4.315 | 13.81 | 181.405 | 0.55 |
| `many_funcs` | 4.975 | 9.80 | 0.005 | 18.81 | 4.772 | 9.31 | 0.017 | 27.13 | 9.686 | 17.88 | 0.013 | 8.68 |
| `matmul` | 4.988 | 7.95 | 93.733 | 2.47 | 4.267 | 7.31 | 147.587 | 2.49 | 4.713 | 14.64 | 73.253 | 1.21 |
| `memory` | 4.499 | 8.08 | 0.162 | 0.48 | 4.148 | 7.64 | 0.299 | 0.50 | 4.419 | 14.59 | 0.252 | 0.79 |
| `memory_tree` | 4.390 | 7.70 | 5.599 | 0.56 | 4.193 | 7.67 | 8.571 | 0.73 | 4.446 | 14.45 | 4.947 | 0.82 |
| `nbody` | 6.006 | 8.09 | 138.109 | 5.04 | 4.223 | 7.42 | 140.016 | 4.05 | 5.147 | 14.88 | 124.755 | 2.55 |
| `quicksort` | 5.031 | 8.27 | 59.772 | 2.68 | 4.132 | 7.91 | 66.975 | 2.41 | 4.668 | 15.16 | 47.852 | 1.57 |
| `raytrace` | 9.982 | 9.02 | 214.488 | 10.23 | 4.549 | 8.17 | 247.281 | 10.88 | 5.673 | 16.00 | 177.064 | 6.13 |
| `regexmatch` | 81.201 | 57.72 | — | 5432.11 | 33.104 | 39.97 | — | 3155.45 | 81.141 | 95.11 | — | 1289.33 |
| `ruby` | 2490.681 | 546.30 | — | 67690.95 | 467.964 | 255.19 | — | 38538.78 | 1019.012 | 745.08 | — | 24204.32 |
| `sha256` | 6.069 | 7.95 | 26.332 | 5.67 | 4.425 | 7.45 | 27.968 | 3.82 | 4.906 | 15.27 | 20.926 | 2.58 |
| `sieve` | 4.282 | 7.69 | 56.640 | 0.74 | 4.077 | 7.25 | 96.437 | 0.45 | 4.399 | 13.95 | 44.679 | 0.49 |
| `spectralnorm` | 5.065 | 8.09 | 333.606 | 2.91 | 4.201 | 7.45 | 380.433 | 2.70 | 4.902 | 14.72 | 353.588 | 1.55 |
| `sqlite3` | 390.763 | 74.67 | — | 6160.01 | 45.798 | 46.84 | — | 3898.54 | 67.118 | 112.11 | — | 1847.62 |
| `swar-pack-parse` | 4.406 | 8.53 | 0.026 | 0.42 | 4.338 | 7.91 | 0.066 | 0.63 | 4.579 | 14.95 | 0.050 | 1.20 |
| `tiny` | 4.872 | 7.66 | 0.005 | 0.10 | 4.845 | 7.45 | 0.016 | 0.25 | 5.029 | 14.33 | 0.012 | 0.74 |
| `utf-as` | 8.543 | 12.12 | 67.373 | 6.73 | 4.573 | 8.59 | 102.564 | 4.46 | 5.678 | 16.78 | 68.147 | 4.09 |
| `utf-as-simd` | 6.313 | 13.52 | 77.143 | 27.50 | 4.952 | 10.70 | 88.103 | 21.37 | 5.979 | 18.67 | 99.614 | 13.66 |
| `wasm3` | 27.123 | 24.80 | — | 1031.82 | 13.107 | 21.55 | — | 1004.95 | 25.551 | 39.56 | — | 445.76 |
| `xjb-mulhi` | 4.312 | 8.16 | 0.072 | 0.32 | 4.344 | 7.66 | 0.140 | 0.50 | 4.352 | 14.52 | 0.110 | 0.94 |

## Method

- Compile metrics are medians of three fresh child processes per module and
  engine. They include process startup, strict decode and validation, native
  compilation, and artifact serialization. RSS is the child-process peak high
  water mark. Wago native code is exact `Compiled.CodeSize()`; Cranelift code is
  the sum of defined function symbols in Wasmtime's compilation image.
- Execution excludes compile, instantiate, initialization, warmup, and
  calibration. Each export has three independently instantiated 100 ms samples;
  engine order rotates each round. Wago uses fixed-arity prepared calls and
  Wasmtime uses its unchecked prepared function API.
- Dragline and Railshot use native targeting, explicit bounds checks, and eight
  compile workers. Cranelift is Wasmtime 46.0.1 with
  `opt-level=2,regalloc-algorithm=backtracking`.
- Toolchain: Go 1.26.5, Darwin ARM64. Samples: 477 compile processes and 1,944
  execution measurements.
