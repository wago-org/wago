# Dragline, Railshot, and Cranelift corpus performance — ARM64 — 2026-08-30

Apple M4 Max results from commit `ef92bc66e29a9f77f8bde8d5e044a71e00006fd2`. The compile set contains all 36 application modules and 17 admitted scalar/MVP ISA modules; 47 modules execute without an external host runtime.

Compile latency, peak compile RSS, and native-code size are medians of six fresh child processes. Execution latency is the geometric mean of a module's export medians; each export median has four independently instantiated 100 ms samples. Cross-corpus averages are geometric means. Lower is better.

## Averages

| Corpus | Engine | Compile | RSS | Execution | Native code |
| --- | --- | ---: | ---: | ---: | ---: |
| All 53 / 47 executable | Dragline | 8.203 ms | 12.24 MiB | 6.071 µs | 5.53 KiB |
|  | Railshot | 6.140 ms | 10.68 MiB | 11.096 µs | 6.46 KiB |
|  | Cranelift | 7.524 ms | 20.33 MiB | 8.906 µs | 5.71 KiB |
| Applications 36 / 30 executable | Dragline | 10.348 ms | 13.08 MiB | 7.208 µs | 7.22 KiB |
|  | Railshot | 7.008 ms | 11.51 MiB | 8.149 µs | 6.77 KiB |
|  | Cranelift | 8.794 ms | 22.30 MiB | 6.572 µs | 5.97 KiB |
| MVP ISA 17 / 17 executable | Dragline | 5.017 ms | 10.64 MiB | 4.484 µs | 3.14 KiB |
|  | Railshot | 4.641 ms | 9.11 MiB | 19.132 µs | 5.85 KiB |
|  | Cranelift | 5.408 ms | 16.71 MiB | 15.226 µs | 5.20 KiB |

| Corpus | D/R compile | D/R RSS | D/R exec | D/R code | D/C compile | D/C RSS | D/C exec | D/C code |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| All | 1.336x | 1.147x | 0.547x | 0.856x | 1.090x | 0.602x | 0.682x | 0.968x |
| Applications | 1.477x | 1.137x | 0.885x | 1.067x | 1.177x | 0.587x | 1.097x | 1.210x |
| MVP ISA | 1.081x | 1.167x | 0.234x | 0.538x | 0.928x | 0.637x | 0.294x | 0.605x |

Dragline is faster than Railshot on 211/216 individual runnable exports in this four-round sweep. Short-call rows near parity are sensitive to host noise; longer focused gates are documented in `dragline-execution-optimization-arm64-2026-08-28.md`.

The stricter ISA-only gate uses four balanced alternating 300 ms rounds per backend/export. All 180/180 admitted Wasm 1.0 exports beat Railshot by balanced median; the cross-export execution geometric mean is 0.2324x Railshot (about 4.30x faster). The closest row is `isa_f64.sqrt` at 0.9992x by median and 0.9995x by paired median.

The other three apparent losses in the short sweep are 16--20 ns application calls. Ten alternating 500 ms rounds put `branches.classify` at 0.9893x Railshot, `tiny.add` at 1.0008x, and `dispatch.apply` at 1.0081x by median. Dragline's `tiny.add` native body is already the irreducible `add; ret`; these two remaining rows measure the prepared host-call floor rather than additional guest work.


## Per-module results

Columns are compile latency in ms, peak RSS in MiB, execution latency in µs, and native code in KiB for Dragline (`D`), Railshot (`R`), and Cranelift (`C`). A dash marks a compile-only module. For modules with multiple exports, execution is the geometric mean of their per-export medians.

| Module | D ms | D RSS | D exec | D code | R ms | R RSS | R exec | R code | C ms | C RSS | C exec | C code |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `tiny` | 4.356 | 7.86 | 0.016 | 0.10 | 4.237 | 7.73 | 0.016 | 0.25 | 4.517 | 14.27 | 0.014 | 0.74 |
| `fib_iter` | 4.323 | 7.45 | 0.024 | 0.18 | 4.184 | 7.28 | 0.025 | 0.18 | 4.531 | 13.67 | 0.016 | 0.40 |
| `fib_rec` | 4.220 | 7.39 | 1049.286 | 0.15 | 4.127 | 7.31 | 1059.262 | 0.27 | 4.488 | 13.70 | 1069.670 | 0.48 |
| `arith` | 4.232 | 7.44 | 1.353 | 0.20 | 4.309 | 7.27 | 1.441 | 0.19 | 4.442 | 13.76 | 1.356 | 0.41 |
| `float` | 4.288 | 7.45 | 2.252 | 0.20 | 4.124 | 7.34 | 2.516 | 0.21 | 4.416 | 13.73 | 2.238 | 0.41 |
| `memory` | 4.313 | 8.08 | 0.169 | 0.48 | 4.214 | 7.83 | 0.308 | 0.50 | 4.667 | 14.62 | 0.259 | 0.79 |
| `memory_tree` | 4.395 | 7.92 | 5.610 | 0.57 | 4.232 | 7.67 | 8.615 | 0.73 | 4.527 | 14.50 | 4.962 | 0.82 |
| `globals` | 4.346 | 7.98 | 0.494 | 0.23 | 4.331 | 7.73 | 0.596 | 0.34 | 4.614 | 14.52 | 2.068 | 0.75 |
| `dispatch` | 4.471 | 8.80 | 0.020 | 0.40 | 4.247 | 8.30 | 0.020 | 1.30 | 4.958 | 15.59 | 0.012 | 1.72 |
| `branches` | 4.343 | 7.51 | 0.017 | 0.29 | 4.204 | 7.23 | 0.016 | 0.19 | 4.508 | 13.57 | 0.011 | 0.46 |
| `many_funcs` | 4.820 | 9.83 | 0.016 | 18.81 | 4.652 | 9.53 | 0.017 | 27.13 | 9.524 | 17.91 | 0.013 | 8.68 |
| `linked_list` | 4.419 | 7.59 | 4.434 | 0.56 | 4.309 | 7.30 | 5.638 | 0.41 | 4.491 | 13.97 | 4.644 | 0.46 |
| `mandelbrot` | 4.352 | 7.62 | 215.984 | 0.55 | 4.194 | 7.27 | 217.221 | 0.43 | 4.506 | 13.80 | 182.679 | 0.55 |
| `sieve` | 4.239 | 7.59 | 57.199 | 0.74 | 4.076 | 7.39 | 97.994 | 0.45 | 4.537 | 13.97 | 45.443 | 0.49 |
| `nbody` | 6.132 | 8.19 | 139.994 | 5.04 | 4.368 | 7.51 | 141.955 | 4.05 | 5.030 | 14.89 | 126.522 | 2.55 |
| `spectralnorm` | 5.091 | 7.95 | 353.723 | 2.94 | 4.255 | 7.52 | 382.345 | 2.70 | 4.935 | 14.74 | 360.981 | 1.55 |
| `fannkuch` | 6.357 | 8.23 | 980.460 | 7.74 | 4.363 | 7.55 | 1228.754 | 5.22 | 5.245 | 15.14 | 814.397 | 2.50 |
| `matmul` | 5.172 | 7.92 | 100.495 | 2.49 | 4.270 | 7.55 | 147.592 | 2.49 | 4.858 | 14.59 | 73.765 | 1.21 |
| `quicksort` | 4.999 | 8.23 | 63.992 | 2.70 | 4.343 | 8.02 | 68.675 | 2.41 | 4.947 | 15.14 | 50.309 | 1.57 |
| `crc32` | 4.681 | 7.82 | 20.779 | 2.04 | 4.305 | 7.53 | 22.810 | 1.68 | 4.740 | 14.92 | 19.929 | 1.39 |
| `sha256` | 6.125 | 7.96 | 26.516 | 5.68 | 4.262 | 7.63 | 28.182 | 3.82 | 5.059 | 15.25 | 20.890 | 2.58 |
| `raytrace` | 10.013 | 8.99 | 222.017 | 10.49 | 4.628 | 8.21 | 251.931 | 10.88 | 5.833 | 16.05 | 182.632 | 6.13 |
| `regexmatch` | 79.716 | 54.77 | — | 5532.27 | 33.245 | 39.90 | — | 3155.45 | 79.349 | 93.66 | — | 1289.33 |
| `wasm3` | 26.759 | 25.27 | — | 1042.83 | 13.695 | 21.80 | — | 1004.95 | 26.170 | 40.69 | — | 445.76 |
| `json-as` | 14.865 | 16.02 | 24.142 | 89.26 | 6.171 | 13.10 | 26.681 | 75.70 | 7.391 | 21.34 | 18.134 | 31.92 |
| `blake-as` | 10.946 | 10.84 | 383.026 | 10.96 | 4.995 | 9.49 | 390.706 | 11.14 | 6.248 | 17.47 | 367.515 | 7.32 |
| `utf-as` | 7.192 | 9.62 | 101.778 | 6.91 | 4.616 | 8.77 | 102.886 | 4.46 | 5.805 | 16.84 | 68.827 | 4.09 |
| `xjb-mulhi` | 4.528 | 8.02 | 0.137 | 0.32 | 4.338 | 7.70 | 0.142 | 0.50 | 4.713 | 14.59 | 0.111 | 0.94 |
| `swar-pack-parse` | 4.333 | 8.51 | 0.060 | 0.42 | 4.206 | 8.05 | 0.063 | 0.63 | 4.733 | 14.95 | 0.051 | 1.20 |
| `json-as-simd` | 9.360 | 15.24 | 33.776 | 111.02 | 6.081 | 13.49 | 33.964 | 90.32 | 8.068 | 21.27 | 24.561 | 35.23 |
| `blake-as-simd` | 13.182 | 12.91 | 409.751 | 32.36 | 6.483 | 11.59 | 410.269 | 31.31 | 8.548 | 20.31 | 369.660 | 20.51 |
| `utf-as-simd` | 7.352 | 13.10 | 78.155 | 27.64 | 5.995 | 10.73 | 88.823 | 21.37 | 7.387 | 19.25 | 100.700 | 13.66 |
| `lua` | 43.676 | 30.05 | — | 1650.28 | 17.943 | 21.48 | — | 984.39 | 27.707 | 47.23 | — | 505.28 |
| `sqlite3` | 388.867 | 71.25 | — | 6225.15 | 45.550 | 46.86 | — | 3898.54 | 67.403 | 105.12 | — | 1847.62 |
| `ruby` | 2457.286 | 551.76 | — | 68725.05 | 449.054 | 252.16 | — | 38538.78 | 1082.873 | 723.72 | — | 24204.32 |
| `esbuild` | 1381.990 | 486.01 | — | 65807.92 | 342.673 | 224.05 | — | 30615.77 | 508.667 | 522.84 | — | 15284.75 |
| `isa_i32` | 4.792 | 9.01 | 2.451 | 8.09 | 4.746 | 9.55 | 31.948 | 11.77 | 5.793 | 17.71 | 26.746 | 11.06 |
| `isa_i64` | 4.851 | 10.03 | 1.574 | 8.48 | 4.757 | 9.57 | 31.881 | 11.83 | 5.723 | 17.95 | 27.164 | 11.07 |
| `isa_cmp_i32` | 5.465 | 12.65 | 8.017 | 4.97 | 4.785 | 9.41 | 8.896 | 5.95 | 5.610 | 17.84 | 8.550 | 6.47 |
| `isa_cmp_i64` | 5.446 | 12.65 | 8.044 | 4.97 | 4.764 | 9.32 | 8.924 | 6.12 | 5.722 | 17.07 | 8.545 | 6.47 |
| `isa_f32` | 5.372 | 12.64 | 9.810 | 8.41 | 4.759 | 9.35 | 47.929 | 9.58 | 5.622 | 16.94 | 45.559 | 6.57 |
| `isa_f64` | 5.254 | 12.52 | 11.946 | 8.48 | 4.746 | 9.34 | 49.411 | 9.21 | 5.427 | 16.53 | 47.015 | 6.57 |
| `isa_cmp_f32` | 5.380 | 12.46 | 33.969 | 4.22 | 4.697 | 9.34 | 36.592 | 6.75 | 5.234 | 16.86 | 34.197 | 5.48 |
| `isa_cmp_f64` | 5.362 | 12.34 | 33.995 | 4.22 | 4.764 | 9.41 | 36.556 | 6.75 | 5.262 | 16.65 | 34.197 | 5.50 |
| `isa_mem` | 4.775 | 9.79 | 8.138 | 3.07 | 4.664 | 9.23 | 11.734 | 5.30 | 5.159 | 16.21 | 7.923 | 4.21 |
| `isa_mem_narrow` | 5.627 | 12.83 | 6.608 | 10.49 | 4.805 | 9.54 | 10.846 | 14.58 | 5.864 | 17.78 | 8.838 | 11.07 |
| `isa_bulk_mem` | 4.778 | 9.94 | 1.071 | 4.34 | 4.579 | 9.08 | 1.477 | 6.11 | 5.464 | 16.69 | 1.485 | 3.36 |
| `isa_ctl` | 4.570 | 8.48 | 0.423 | 0.38 | 4.445 | 8.77 | 12.260 | 3.72 | 5.134 | 16.10 | 5.673 | 3.25 |
| `isa_call` | 4.451 | 8.15 | 7.459 | 0.36 | 4.321 | 8.16 | 21.740 | 2.04 | 5.104 | 15.66 | 27.918 | 3.03 |
| `isa_var` | 4.456 | 8.07 | 0.329 | 0.38 | 4.247 | 7.74 | 10.614 | 0.68 | 4.713 | 14.42 | 11.743 | 0.97 |
| `isa_cvt` | 4.708 | 9.62 | 3.033 | 2.03 | 4.487 | 8.93 | 57.045 | 5.44 | 5.258 | 16.70 | 57.046 | 3.57 |
| `isa_cvt_mvp` | 5.615 | 13.12 | 17.555 | 9.96 | 4.888 | 9.94 | 100.362 | 26.12 | 5.954 | 17.88 | 93.210 | 26.43 |
| `isa_signext` | 4.660 | 9.28 | 1.114 | 0.90 | 4.498 | 8.55 | 7.906 | 1.53 | 5.071 | 15.52 | 0.929 | 1.56 |

## Method and raw data

- Compile includes child-process startup, strict decode/validation, native compilation, and artifact serialization. RSS is the OS peak high-water mark.
- Wago code size is exact `Compiled.CodeSize()`. Cranelift code size sums defined function symbols in Wasmtime's ELF compilation image, excluding alignment and non-code metadata.
- Execution excludes compile, instantiate, initialization, warmup, and calibration. Wago uses fixed-arity prepared calls when possible; Wasmtime uses its unchecked prepared function API. Engine order rotates every round.
- Dragline and Railshot use native targeting, explicit bounds checks, and eight compile workers. Cranelift is Wasmtime 46.0.1 with `opt-level=2,regalloc-algorithm=backtracking`.
- Host: Apple M4 Max, Darwin ARM64; Go 1.26.5. Source: `ef92bc66e29a9f77f8bde8d5e044a71e00006fd2`.
- Compile samples: 53 × 3 × 6 = 954. Execution samples: 216 × 3 × 4 = 2,592.

Raw captures:

- `dragline-corpus-performance-arm64-2026-08-30-final-compile.jsonl`
- `dragline-corpus-performance-arm64-2026-08-30-final-execution.jsonl`
