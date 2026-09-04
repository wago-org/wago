# Dragline, Railshot, and Cranelift corpus performance — ARM64 — 2026-08-28

Current Apple M4 Max results for every Dragline-admitted application and
WebAssembly 1.0 ISA module. The compile set is 53 modules: all 36 applications
and all 17 admitted scalar/MVP ISA modules. Forty-seven modules execute without
an external host runtime.

Compile latency, peak compile RSS, and native-code size are medians of six fresh
child processes. Execution latency is the geometric mean of a module's export
medians; each export median has four independently instantiated 100 ms samples.
The cross-corpus averages are geometric means.

## Averages

| Corpus | Engine | Compile | RSS | Execution | Native code |
| --- | --- | ---: | ---: | ---: | ---: |
| All 53 / 47 executable | Dragline | 8.498 ms | 12.14 MiB | 13.987 µs | 7.29 KiB |
| | Railshot | 5.914 ms | 10.54 MiB | 11.613 µs | 6.46 KiB |
| | Cranelift | 7.134 ms | 20.33 MiB | 8.986 µs | 5.71 KiB |
| 36 applications / 30 executable | Dragline | 11.177 ms | 13.44 MiB | 26.251 µs | 10.90 KiB |
| | Railshot | 6.731 ms | 11.35 MiB | 8.612 µs | 6.77 KiB |
| | Cranelift | 8.293 ms | 22.30 MiB | 6.548 µs | 5.97 KiB |
| 17 MVP ISA modules | Dragline | 4.756 ms | 9.78 MiB | 4.605 µs | 3.10 KiB |
| | Railshot | 4.496 ms | 9.03 MiB | 19.680 µs | 5.85 KiB |
| | Cranelift | 5.186 ms | 16.72 MiB | 15.712 µs | 5.20 KiB |

| Corpus | D/R compile | D/R RSS | D/R exec | D/R code | D/C compile | D/C RSS | D/C exec | D/C code |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| All | 1.437x | 1.151x | 1.204x | 1.128x | 1.191x | 0.597x | 1.556x | 1.276x |
| Applications | 1.661x | 1.185x | 3.048x | 1.611x | 1.348x | 0.603x | 4.009x | 1.827x |
| MVP ISA | 1.058x | 1.083x | 0.234x | 0.531x | 0.917x | 0.585x | 0.293x | 0.597x |

Lower is better. Dragline's MVP ISA path is 4.27x faster than Railshot and
3.41x faster than Cranelift while emitting about half as much code. Its general
application path is currently 3.05x slower than Railshot and 4.01x slower than
Cranelift. Ruby is the largest compile outlier: 19.216 s Dragline, 423.526 ms
Railshot, and 1.044 s Cranelift.

## Per-module results

Columns are compile latency in ms, peak RSS in MiB, execution latency in µs,
and native code in KiB for Dragline (`D`), Railshot (`R`), and Cranelift (`C`).
A dash marks a compile-only module. For modules with multiple exports, execution
is the geometric mean of their per-export medians.

| Module | D ms | D RSS | D exec | D code | R ms | R RSS | R exec | R code | C ms | C RSS | C exec | C code |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `tiny` | 4.242 | 7.59 | 0.030 | 0.11 | 4.139 | 7.62 | 0.023 | 0.25 | 4.668 | 14.30 | 0.012 | 0.74 |
| `fib_iter` | 4.099 | 6.91 | 0.041 | 0.15 | 4.112 | 7.11 | 0.029 | 0.18 | 4.144 | 13.69 | 0.016 | 0.40 |
| `fib_rec` | 4.129 | 7.27 | 1562.599 | 0.22 | 4.063 | 7.01 | 1051.164 | 0.27 | 4.297 | 13.70 | 1075.881 | 0.48 |
| `arith` | 4.067 | 6.94 | 3.921 | 0.19 | 4.052 | 7.06 | 1.467 | 0.19 | 4.179 | 13.73 | 1.383 | 0.41 |
| `float` | 4.103 | 6.91 | 16.696 | 0.19 | 4.041 | 7.21 | 2.504 | 0.21 | 4.193 | 13.73 | 2.224 | 0.41 |
| `memory` | 4.139 | 7.45 | 0.459 | 0.40 | 4.185 | 7.81 | 0.304 | 0.50 | 4.438 | 14.58 | 0.263 | 0.79 |
| `memory_tree` | 4.338 | 7.70 | 11.092 | 0.71 | 4.129 | 7.77 | 8.626 | 0.73 | 4.402 | 14.50 | 4.958 | 0.82 |
| `globals` | 4.170 | 7.75 | 3.142 | 0.19 | 4.111 | 7.75 | 0.597 | 0.34 | 4.285 | 14.47 | 2.068 | 0.75 |
| `dispatch` | 4.315 | 8.57 | 0.040 | 0.91 | 4.205 | 8.21 | 0.024 | 1.30 | 4.602 | 15.55 | 0.013 | 1.72 |
| `branches` | 4.103 | 7.23 | 0.031 | 0.34 | 3.977 | 6.95 | 0.022 | 0.19 | 4.253 | 13.58 | 0.011 | 0.46 |
| `many_funcs` | 4.703 | 9.61 | 0.034 | 18.94 | 4.571 | 9.44 | 0.021 | 27.13 | 9.799 | 18.06 | 0.013 | 8.68 |
| `linked_list` | 4.137 | 7.38 | 10.024 | 0.71 | 4.104 | 7.15 | 6.521 | 0.41 | 4.208 | 13.93 | 4.721 | 0.46 |
| `mandelbrot` | 4.331 | 7.30 | 264.593 | 0.70 | 4.161 | 7.17 | 217.588 | 0.43 | 4.289 | 13.78 | 182.484 | 0.55 |
| `sieve` | 4.167 | 7.36 | 139.496 | 0.96 | 4.044 | 7.24 | 98.762 | 0.45 | 4.218 | 13.98 | 46.246 | 0.49 |
| `nbody` | 4.137 | 7.25 | 3110.971 | 17.25 | 4.215 | 7.38 | 142.039 | 4.05 | 4.822 | 14.86 | 127.901 | 2.55 |
| `spectralnorm` | 4.272 | 7.21 | 7635.546 | 8.11 | 4.140 | 7.21 | 383.236 | 2.70 | 4.590 | 14.70 | 361.075 | 1.55 |
| `fannkuch` | 4.336 | 7.36 | 3917.696 | 19.39 | 4.213 | 7.38 | 1234.889 | 5.22 | 4.892 | 15.11 | 815.055 | 2.50 |
| `matmul` | 4.151 | 7.16 | 1325.104 | 6.20 | 4.129 | 7.31 | 146.837 | 2.49 | 4.612 | 14.66 | 73.554 | 1.21 |
| `quicksort` | 4.636 | 8.05 | 147.210 | 5.34 | 4.213 | 7.90 | 68.832 | 2.41 | 4.598 | 15.11 | 49.300 | 1.57 |
| `crc32` | 4.204 | 7.16 | 106.911 | 4.07 | 4.102 | 7.32 | 23.205 | 1.68 | 4.477 | 14.91 | 19.921 | 1.39 |
| `sha256` | 4.207 | 7.29 | 317.135 | 11.74 | 4.225 | 7.41 | 28.244 | 3.82 | 4.725 | 15.24 | 20.995 | 2.58 |
| `raytrace` | 5.040 | 8.42 | 2394.343 | 38.03 | 4.393 | 8.05 | 252.205 | 10.88 | 5.593 | 15.98 | 184.627 | 6.13 |
| `regexmatch` | 117.660 | 77.64 | — | 7064.54 | 32.615 | 39.98 | — | 3155.45 | 74.003 | 92.57 | — | 1289.33 |
| `wasm3` | 65.943 | 33.50 | — | 1220.82 | 13.321 | 22.10 | — | 1004.95 | 24.862 | 39.98 | — | 445.76 |
| `json-as` | 12.987 | 15.47 | 48.026 | 123.62 | 5.878 | 12.98 | 26.578 | 75.70 | 7.149 | 21.40 | 18.172 | 31.92 |
| `blake-as` | 8.699 | 11.88 | 5334.251 | 28.84 | 4.741 | 9.38 | 391.786 | 11.14 | 5.926 | 17.34 | 371.675 | 7.32 |
| `utf-as` | 6.616 | 9.33 | 147.042 | 9.57 | 4.508 | 8.70 | 102.864 | 4.46 | 5.557 | 16.80 | 68.415 | 4.09 |
| `xjb-mulhi` | 4.312 | 7.89 | 0.193 | 0.43 | 4.166 | 7.61 | 0.163 | 0.50 | 4.407 | 14.59 | 0.111 | 0.94 |
| `swar-pack-parse` | 4.256 | 8.29 | 0.108 | 0.58 | 4.128 | 7.91 | 0.075 | 0.63 | 4.544 | 14.93 | 0.050 | 1.20 |
| `json-as-simd` | 10.949 | 15.29 | 81.093 | 150.10 | 5.937 | 13.38 | 33.884 | 90.32 | 7.684 | 21.25 | 24.834 | 35.23 |
| `blake-as-simd` | 9.437 | 13.72 | 3456.850 | 103.54 | 5.537 | 11.53 | 413.183 | 31.31 | 6.918 | 20.13 | 369.965 | 20.51 |
| `utf-as-simd` | 6.252 | 12.88 | 364.648 | 50.86 | 4.940 | 10.73 | 88.763 | 21.37 | 6.010 | 18.86 | 100.939 | 13.66 |
| `lua` | 144.578 | 48.92 | — | 2041.60 | 17.732 | 22.29 | — | 984.39 | 26.686 | 49.09 | — | 505.28 |
| `sqlite3` | 736.896 | 98.08 | — | 7027.69 | 43.964 | 44.65 | — | 3898.54 | 66.524 | 106.63 | — | 1847.62 |
| `ruby` | 19216.176 | 875.27 | — | 96678.91 | 423.526 | 253.00 | — | 38538.78 | 1044.461 | 735.40 | — | 24204.32 |
| `esbuild` | 5323.296 | 683.80 | — | 80889.71 | 310.867 | 222.05 | — | 30615.77 | 477.523 | 530.93 | — | 15284.75 |
| `isa_i32` | 4.638 | 8.94 | 2.431 | 8.64 | 4.706 | 9.52 | 31.705 | 11.77 | 5.671 | 17.88 | 26.599 | 11.06 |
| `isa_i64` | 4.685 | 9.02 | 2.156 | 8.64 | 4.675 | 9.64 | 31.713 | 11.83 | 5.627 | 17.95 | 26.954 | 11.07 |
| `isa_cmp_i32` | 5.186 | 12.38 | 7.928 | 5.15 | 4.476 | 9.31 | 8.810 | 5.95 | 5.377 | 17.77 | 8.467 | 6.47 |
| `isa_cmp_i64` | 5.275 | 12.34 | 7.938 | 5.15 | 4.546 | 9.25 | 8.817 | 6.13 | 5.381 | 17.48 | 8.452 | 6.47 |
| `isa_f32` | 4.646 | 9.24 | 3.210 | 8.33 | 4.476 | 9.01 | 47.618 | 9.58 | 5.351 | 16.89 | 45.348 | 6.57 |
| `isa_f64` | 4.623 | 9.21 | 3.513 | 7.03 | 4.514 | 9.02 | 51.378 | 9.21 | 5.080 | 16.44 | 48.858 | 6.57 |
| `isa_cmp_f32` | 5.121 | 12.06 | 36.383 | 4.40 | 4.501 | 9.21 | 38.632 | 6.75 | 5.065 | 16.95 | 37.545 | 5.48 |
| `isa_cmp_f64` | 5.203 | 12.02 | 33.970 | 4.40 | 4.530 | 9.32 | 37.576 | 6.75 | 4.902 | 16.30 | 34.623 | 5.50 |
| `isa_mem` | 4.506 | 8.65 | 8.506 | 2.90 | 4.484 | 9.28 | 12.029 | 5.30 | 5.259 | 17.02 | 8.150 | 4.21 |
| `isa_mem_narrow` | 5.339 | 12.75 | 7.351 | 14.80 | 4.771 | 9.52 | 11.330 | 14.58 | 5.601 | 17.72 | 9.236 | 11.07 |
| `isa_bulk_mem` | 4.456 | 8.46 | 1.054 | 2.24 | 4.408 | 8.99 | 1.498 | 6.11 | 5.069 | 16.30 | 1.551 | 3.36 |
| `isa_ctl` | 4.422 | 8.36 | 0.772 | 0.46 | 4.382 | 8.72 | 12.775 | 3.72 | 4.860 | 16.04 | 5.869 | 3.25 |
| `isa_call` | 4.335 | 7.94 | 7.721 | 0.38 | 4.214 | 8.14 | 22.383 | 2.04 | 5.022 | 15.66 | 28.854 | 3.03 |
| `isa_var` | 4.289 | 7.38 | 3.035 | 0.34 | 4.177 | 7.65 | 11.199 | 0.68 | 4.422 | 14.45 | 12.299 | 0.97 |
| `isa_cvt` | 4.423 | 8.26 | 1.346 | 1.47 | 4.495 | 8.96 | 59.491 | 5.44 | 4.980 | 16.42 | 59.363 | 3.57 |
| `isa_cvt_mvp` | 5.523 | 13.07 | 19.906 | 12.01 | 4.803 | 9.79 | 106.325 | 26.13 | 5.753 | 17.67 | 98.717 | 26.43 |
| `isa_signext` | 4.450 | 9.09 | 1.281 | 1.02 | 4.325 | 8.48 | 8.686 | 1.53 | 4.936 | 15.71 | 1.028 | 1.56 |

## Method and raw data

- Compile includes child-process startup, strict decode/validation, native
  compilation, and artifact serialization. RSS is the OS peak high-water mark.
- Wago code size is exact `Compiled.CodeSize()`. Cranelift code size sums all
  defined function symbols in Wasmtime's ELF compilation image, including Wasm
  bodies and trampolines but excluding alignment and non-code metadata.
- Execution excludes compile, instantiate, initialization, warmup, and adaptive
  calibration. Wago uses `PrepareFunction`; Wasmtime uses its unchecked prepared
  function API. Engine order rotates every round.
- Dragline and Railshot use native targeting, explicit bounds checks, and eight
  workers. Cranelift is Wasmtime 46.0.1 with
  `opt-level=2,regalloc-algorithm=backtracking`.
- Source base: `12649c09dc2850fd494f194a43412766bbc26d01`, plus the
  current uncommitted Dragline worktree; Go 1.26.5.
- Compile samples: 53 × 3 × 6 = 954. Execution samples: 216 exports × 3 × 4 =
  2592.

Raw data:

- `dragline-corpus-performance-arm64-2026-08-28-post-control-fix-compile.jsonl`
- `dragline-corpus-performance-arm64-2026-08-28-post-control-fix-execution.jsonl`

The compiler configuration is `bench/dragline-railshot-cranelift.json`. The
execution workers are `bench/cmd/executionworker` and
`bench/tools/wasmtime-execution-worker.c`.
