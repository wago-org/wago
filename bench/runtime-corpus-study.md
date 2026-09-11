# Cross-runtime corpus study

Generated 2026-09-11 from Wago 7c382581d8c5. Lower is better.

## Method

- Corpus: all 63 modules in `corpus/catalog.json`. Compile failures are shown as em dashes; interpreters without a separate compile stage are marked N/A.
- Compile latency: median of 3 fresh processes after 1 warmup, including process startup, file load, validation, and code generation.
- Used heap: allocated Go heap is captured inside Wago, Wazero, and Wazy adapters. Other engines do not expose an equivalent CLI counter; peak RSS is reported for every successful compile as the cross-language memory proxy. Peak RSS includes runtime/process baseline and is not interchangeable with allocated heap.
- Execution latency: median of 3 fresh-process runs of the first declared `exec` case for each of 48 directly runnable modules. It includes load, compile, instantiate, and first invocation. Semantic fixtures and WASI command cases are excluded because their host setup is not portable across these CLIs.
- Wago, Wazero, and Wazy use single-purpose Go adapters so their compile and exported-function APIs have the same process boundary as the native CLIs. The allocated-heap value is the `runtime.MemStats.TotalAlloc` delta around compilation, excluding adapter/runtime construction.
- The three imported AssemblyScript modules (`json-as`, `json-as-simd`, and `utf-as-simd`) are retained as explicit execution gaps because they require an `env.abort` host function. Wasm3 and wasmi have one additional execution gap (`blake-as-simd`) because those installed builds do not support the module's SIMD instructions.
- ARM64 and AMD64 ran independently on their named machines. AMD64 was pinned to CPU 7.

## ARM64 — Apple M4 Max

Source: `7c382581d8c53f06bb53879ddf4e28db6c47ce80`.

### Engine versions

| Engine | Version |
|---|---|
| wago | wago 0.0.0 (7c382581) |
| wazero | wazero v1.12.0 adapter |
| wazy | v0.3.0 |
| wasmtime | wasmtime 46.0.1 (823d1b8f2 2026-06-24) |
| v8 | V8 version 15.2.20 |
| wasm3 | Wasm3 v0.5.0 on arm64-v8a |
| wasmi | wasmi_cli 1.1.0 |
| wavm | WAVM version 0.0.0-prerelease |
| wasmer | wasmer 7.3.0 |

### Coverage and geometric means

| Engine | Compile coverage | Compile wall | Allocated heap | Peak RSS | Execute coverage | Cold execute |
|---|---:|---:|---:|---:|---:|---:|
| wago | 63/63 | 4.43 ms | 67.81 KiB | 7.76 MiB | 45/48 | 4.77 ms |
| wazero | 63/63 | 5.32 ms | 1.03 MiB | 8.90 MiB | 45/48 | 5.27 ms |
| wazy | 63/63 | 5.09 ms | 990.20 KiB | 8.94 MiB | 45/48 | 5.14 ms |
| wasmtime | 63/63 | 7.12 ms | N/A | 16.89 MiB | 45/48 | 5.95 ms |
| v8 | 63/63 | 14.75 ms | N/A | 31.77 MiB | 45/48 | 15.59 ms |
| wasm3 | 0/63 | N/A | N/A | N/A | 44/48 | 5.21 ms |
| wasmi | 0/63 | N/A | N/A | N/A | 44/48 | 7.35 ms |
| wavm | 63/63 | 33.69 ms | N/A | 33.52 MiB | 45/48 | 25.27 ms |
| wasmer | 63/63 | 8.91 ms | N/A | 20.01 MiB | 45/48 | 6.76 ms |

### Compile wall latency

| Module | wago | wazero | wazy | wasmtime | v8 | wasm3 | wasmi | wavm | wasmer |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 7.00 ms | 7.08 ms | 6.79 ms | 6.39 ms | 13.63 ms | N/A | N/A | 8.32 ms | 6.51 ms |
| fib_rec | 3.75 ms | 3.98 ms | 3.91 ms | 5.87 ms | 13.10 ms | N/A | N/A | 9.17 ms | 6.57 ms |
| memory | 4.13 ms | 4.00 ms | 4.01 ms | 6.44 ms | 15.56 ms | N/A | N/A | 12.01 ms | 7.60 ms |
| memory_tree | 3.96 ms | 4.10 ms | 4.02 ms | 5.43 ms | 13.12 ms | N/A | N/A | 10.17 ms | 6.70 ms |
| dispatch | 4.15 ms | 4.02 ms | 3.87 ms | 5.71 ms | 12.75 ms | N/A | N/A | 10.28 ms | 6.51 ms |
| many_funcs | 4.07 ms | 4.49 ms | 4.07 ms | 10.54 ms | 15.72 ms | N/A | N/A | 46.00 ms | 11.19 ms |
| linked_list | 4.55 ms | 4.83 ms | 4.97 ms | 6.40 ms | 18.82 ms | N/A | N/A | 11.06 ms | 6.60 ms |
| nbody | 4.24 ms | 4.43 ms | 4.23 ms | 6.04 ms | 13.98 ms | N/A | N/A | 18.13 ms | 11.64 ms |
| fannkuch | 4.20 ms | 4.46 ms | 4.36 ms | 6.15 ms | 20.19 ms | N/A | N/A | 23.95 ms | 7.92 ms |
| matmul | 4.20 ms | 4.36 ms | 4.18 ms | 5.90 ms | 14.49 ms | N/A | N/A | 18.75 ms | 6.71 ms |
| sha256 | 3.81 ms | 4.38 ms | 4.34 ms | 5.90 ms | 14.26 ms | N/A | N/A | 20.46 ms | 7.34 ms |
| raytrace | 4.35 ms | 5.09 ms | 4.96 ms | 6.62 ms | 12.83 ms | N/A | N/A | 24.32 ms | 8.87 ms |
| json-as | 5.22 ms | 8.66 ms | 9.40 ms | 9.39 ms | 16.30 ms | N/A | N/A | 164.26 ms | 15.51 ms |
| blake-as | 4.72 ms | 4.89 ms | 4.97 ms | 7.56 ms | 16.60 ms | N/A | N/A | 41.53 ms | 8.66 ms |
| utf-as | 4.33 ms | 4.51 ms | 4.48 ms | 6.68 ms | 12.88 ms | N/A | N/A | 25.99 ms | 7.65 ms |
| json-as-simd | 4.98 ms | 8.69 ms | 7.64 ms | 9.00 ms | 13.43 ms | N/A | N/A | 214.44 ms | 20.02 ms |
| blake-as-simd | 5.98 ms | 7.02 ms | 6.48 ms | 9.46 ms | 17.25 ms | N/A | N/A | 150.84 ms | 13.07 ms |
| utf-as-simd | 4.58 ms | 5.75 ms | 5.36 ms | 7.15 ms | 12.75 ms | N/A | N/A | 74.38 ms | 11.10 ms |
| coremark | 4.66 ms | 6.40 ms | 5.76 ms | 7.42 ms | 15.07 ms | N/A | N/A | 81.75 ms | 9.95 ms |
| blake3 | 4.84 ms | 6.10 ms | 5.46 ms | 7.39 ms | 12.88 ms | N/A | N/A | 230.85 ms | 11.99 ms |
| qoi | 3.90 ms | 4.73 ms | 4.67 ms | 6.17 ms | 12.78 ms | N/A | N/A | 32.43 ms | 7.96 ms |
| lz4 | 3.99 ms | 4.57 ms | 4.46 ms | 6.52 ms | 15.51 ms | N/A | N/A | 35.24 ms | 8.64 ms |
| zlib | 5.00 ms | 9.12 ms | 8.08 ms | 11.25 ms | 15.61 ms | N/A | N/A | 148.89 ms | 14.41 ms |
| zstd | 7.25 ms | 24.23 ms | 18.60 ms | 23.13 ms | 14.34 ms | N/A | N/A | 538.76 ms | 26.14 ms |
| embench-crc32 | 4.20 ms | 4.04 ms | 3.82 ms | 5.57 ms | 14.49 ms | N/A | N/A | 9.53 ms | 6.78 ms |
| embench-huffbench | 3.98 ms | 4.76 ms | 4.58 ms | 6.58 ms | 13.38 ms | N/A | N/A | 33.94 ms | 9.03 ms |
| embench-matmult-int | 4.25 ms | 4.49 ms | 4.31 ms | 6.56 ms | 17.51 ms | N/A | N/A | 21.47 ms | 7.45 ms |
| embench-nettle-aes | 4.08 ms | 4.44 ms | 4.39 ms | 8.14 ms | 19.12 ms | N/A | N/A | 27.60 ms | 7.99 ms |
| embench-nettle-sha256 | 4.23 ms | 4.69 ms | 4.45 ms | 7.15 ms | 16.74 ms | N/A | N/A | 44.52 ms | 9.77 ms |
| embench-qrduino | 5.15 ms | 9.24 ms | 9.05 ms | 12.36 ms | 15.63 ms | N/A | N/A | 141.69 ms | 14.29 ms |
| sightglass-shootout-base64 | 5.66 ms | 13.66 ms | 12.48 ms | 12.77 ms | 13.13 ms | N/A | N/A | 271.19 ms | 22.87 ms |
| sightglass-libsodium-hash | 6.62 ms | 15.41 ms | 13.78 ms | 14.72 ms | 14.01 ms | N/A | N/A | 444.27 ms | 25.39 ms |
| tacle-bsort | 5.08 ms | 5.40 ms | 5.44 ms | 7.21 ms | 16.86 ms | N/A | N/A | 14.09 ms | 6.76 ms |
| polybench-2mm | 4.18 ms | 4.79 ms | 4.67 ms | 6.71 ms | 13.75 ms | N/A | N/A | 27.89 ms | 7.58 ms |
| polybench-3mm | 3.98 ms | 4.58 ms | 4.61 ms | 6.48 ms | 13.30 ms | N/A | N/A | 34.09 ms | 8.33 ms |
| polybench-adi | 4.36 ms | 4.80 ms | 4.53 ms | 6.37 ms | 13.56 ms | N/A | N/A | 33.60 ms | 8.09 ms |
| polybench-atax | 3.96 ms | 4.54 ms | 4.47 ms | 6.43 ms | 13.38 ms | N/A | N/A | 20.81 ms | 7.14 ms |
| polybench-bicg | 4.18 ms | 4.65 ms | 4.62 ms | 6.33 ms | 21.58 ms | N/A | N/A | 21.18 ms | 8.23 ms |
| polybench-cholesky | 4.48 ms | 5.23 ms | 4.73 ms | 6.71 ms | 13.10 ms | N/A | N/A | 30.84 ms | 8.28 ms |
| polybench-correlation | 3.86 ms | 5.52 ms | 4.85 ms | 7.46 ms | 13.94 ms | N/A | N/A | 28.07 ms | 8.09 ms |
| polybench-covariance | 3.91 ms | 4.42 ms | 4.48 ms | 6.17 ms | 12.97 ms | N/A | N/A | 24.31 ms | 7.93 ms |
| polybench-deriche | 4.44 ms | 4.81 ms | 4.88 ms | 6.51 ms | 13.55 ms | N/A | N/A | 33.13 ms | 7.75 ms |
| polybench-doitgen | 4.16 ms | 5.07 ms | 4.42 ms | 6.61 ms | 13.52 ms | N/A | N/A | 29.32 ms | 8.47 ms |
| polybench-durbin | 4.79 ms | 4.89 ms | 4.75 ms | 6.35 ms | 13.13 ms | N/A | N/A | 21.58 ms | 7.59 ms |
| polybench-fdtd-2d | 3.98 ms | 4.94 ms | 4.61 ms | 6.41 ms | 15.67 ms | N/A | N/A | 33.69 ms | 8.02 ms |
| polybench-floyd-warshall | 4.13 ms | 4.46 ms | 4.52 ms | 6.49 ms | 17.83 ms | N/A | N/A | 19.42 ms | 6.96 ms |
| polybench-gemm | 3.95 ms | 4.42 ms | 4.41 ms | 5.95 ms | 14.63 ms | N/A | N/A | 24.70 ms | 8.62 ms |
| polybench-gemver | 4.21 ms | 4.97 ms | 4.74 ms | 6.76 ms | 13.26 ms | N/A | N/A | 29.47 ms | 7.99 ms |
| polybench-gesummv | 4.02 ms | 4.41 ms | 4.24 ms | 5.97 ms | 12.79 ms | N/A | N/A | 24.03 ms | 8.32 ms |
| polybench-gramschmidt | 4.45 ms | 5.38 ms | 4.42 ms | 6.39 ms | 13.28 ms | N/A | N/A | 28.34 ms | 7.33 ms |
| polybench-heat-3d | 4.35 ms | 4.71 ms | 4.30 ms | 6.40 ms | 13.94 ms | N/A | N/A | 36.43 ms | 7.90 ms |
| polybench-jacobi-1d | 4.04 ms | 4.51 ms | 4.31 ms | 6.18 ms | 13.38 ms | N/A | N/A | 17.89 ms | 7.08 ms |
| polybench-jacobi-2d | 4.68 ms | 5.36 ms | 4.61 ms | 6.19 ms | 14.29 ms | N/A | N/A | 21.46 ms | 8.17 ms |
| polybench-lu | 4.56 ms | 4.71 ms | 4.79 ms | 6.33 ms | 18.83 ms | N/A | N/A | 33.41 ms | 7.86 ms |
| polybench-ludcmp | 4.56 ms | 4.98 ms | 4.80 ms | 6.87 ms | 19.20 ms | N/A | N/A | 40.08 ms | 8.17 ms |
| polybench-mvt | 3.88 ms | 4.42 ms | 4.57 ms | 6.49 ms | 15.38 ms | N/A | N/A | 24.18 ms | 7.35 ms |
| polybench-nussinov | 3.93 ms | 4.43 ms | 4.29 ms | 6.05 ms | 13.06 ms | N/A | N/A | 23.01 ms | 7.23 ms |
| polybench-seidel-2d | 3.90 ms | 4.41 ms | 4.53 ms | 5.95 ms | 12.66 ms | N/A | N/A | 18.69 ms | 7.09 ms |
| polybench-symm | 3.70 ms | 4.51 ms | 4.65 ms | 7.65 ms | 13.41 ms | N/A | N/A | 29.42 ms | 7.70 ms |
| polybench-syr2k | 3.95 ms | 4.47 ms | 4.36 ms | 6.09 ms | 16.81 ms | N/A | N/A | 27.73 ms | 8.65 ms |
| polybench-syrk | 4.56 ms | 5.37 ms | 4.90 ms | 7.54 ms | 16.22 ms | N/A | N/A | 27.38 ms | 8.96 ms |
| polybench-trisolv | 4.70 ms | 5.22 ms | 5.11 ms | 7.16 ms | 16.14 ms | N/A | N/A | 24.22 ms | 9.17 ms |
| polybench-trmm | 5.05 ms | 5.80 ms | 5.78 ms | 8.26 ms | 16.82 ms | N/A | N/A | 27.09 ms | 7.95 ms |

### Allocated compile heap

Only Wago, Wazero, and Wazy expose this measurement.

| Module | wago | wazero | wazy | wasmtime | v8 | wasm3 | wasmi | wavm | wasmer |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 26.49 KiB | 275.55 KiB | 276.38 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| fib_rec | 27.48 KiB | 277.70 KiB | 247.96 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| memory | 29.37 KiB | 302.84 KiB | 304.80 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| memory_tree | 31.98 KiB | 308.34 KiB | 309.02 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| dispatch | 38.01 KiB | 282.98 KiB | 253.82 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| many_funcs | 157.17 KiB | 333.91 KiB | 348.57 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| linked_list | 29.66 KiB | 321.86 KiB | 280.13 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| nbody | 60.52 KiB | 701.96 KiB | 615.16 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| fannkuch | 62.60 KiB | 1013.63 KiB | 975.27 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| matmul | 41.05 KiB | 592.19 KiB | 462.64 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| sha256 | 44.27 KiB | 808.26 KiB | 777.01 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| raytrace | 96.88 KiB | 1.42 MiB | 1.42 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| json-as | 143.12 KiB | 2.53 MiB | 2.30 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| blake-as | 56.64 KiB | 873.78 KiB | 882.69 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| utf-as | 123.68 KiB | 1.14 MiB | 1.01 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| json-as-simd | 153.24 KiB | 2.25 MiB | 2.22 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| blake-as-simd | 203.08 KiB | 1.46 MiB | 1.53 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| utf-as-simd | 186.55 KiB | 1.15 MiB | 1.19 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| coremark | 66.28 KiB | 2.02 MiB | 1.78 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| blake3 | 83.15 KiB | 1.45 MiB | 1.38 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| qoi | 49.25 KiB | 831.41 KiB | 711.02 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| lz4 | 44.85 KiB | 945.25 KiB | 827.71 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| zlib | 239.34 KiB | 7.24 MiB | 6.69 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| zstd | 412.86 KiB | 23.72 MiB | 22.31 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-crc32 | 55.45 KiB | 372.95 KiB | 301.80 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-huffbench | 82.81 KiB | 1.83 MiB | 1.56 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-matmult-int | 65.26 KiB | 1.09 MiB | 1.02 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-nettle-aes | 95.18 KiB | 1.61 MiB | 1.21 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-nettle-sha256 | 70.77 KiB | 1.39 MiB | 1.39 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-qrduino | 256.88 KiB | 7.31 MiB | 5.62 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| sightglass-shootout-base64 | 401.05 KiB | 8.32 MiB | 9.40 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| sightglass-libsodium-hash | 308.51 KiB | 9.02 MiB | 10.07 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| tacle-bsort | 39.98 KiB | 465.80 KiB | 436.46 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-2mm | 61.16 KiB | 1.00 MiB | 1.02 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-3mm | 61.22 KiB | 1.35 MiB | 1.38 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-adi | 62.49 KiB | 1.24 MiB | 1.14 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-atax | 44.42 KiB | 797.04 KiB | 752.32 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-bicg | 44.45 KiB | 859.57 KiB | 859.36 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-cholesky | 60.84 KiB | 1.04 MiB | 984.09 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-correlation | 60.79 KiB | 956.91 KiB | 886.95 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-covariance | 60.73 KiB | 847.19 KiB | 758.95 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-deriche | 61.16 KiB | 1.02 MiB | 1023.65 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-doitgen | 65.33 KiB | 1.20 MiB | 1.01 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-durbin | 44.56 KiB | 797.88 KiB | 721.70 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-fdtd-2d | 60.91 KiB | 1.08 MiB | 1.03 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-floyd-warshall | 44.12 KiB | 633.05 KiB | 606.16 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-gemm | 60.75 KiB | 847.12 KiB | 797.96 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-gemver | 62.28 KiB | 1.23 MiB | 1.43 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-gesummv | 43.68 KiB | 789.09 KiB | 770.19 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-gramschmidt | 60.85 KiB | 967.30 KiB | 861.86 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-heat-3d | 62.33 KiB | 1.09 MiB | 992.36 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-jacobi-1d | 43.61 KiB | 661.38 KiB | 583.45 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-jacobi-2d | 43.98 KiB | 736.65 KiB | 658.16 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-lu | 60.84 KiB | 1.05 MiB | 965.17 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-ludcmp | 62.78 KiB | 1.63 MiB | 1.51 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-mvt | 60.69 KiB | 929.21 KiB | 927.00 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-nussinov | 44.70 KiB | 798.63 KiB | 751.72 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-seidel-2d | 44.16 KiB | 644.22 KiB | 605.98 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-symm | 60.84 KiB | 957.90 KiB | 939.01 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-syr2k | 60.10 KiB | 837.78 KiB | 790.31 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-syrk | 60.81 KiB | 842.75 KiB | 786.34 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-trisolv | 43.74 KiB | 749.52 KiB | 728.77 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-trmm | 60.93 KiB | 843.81 KiB | 766.11 KiB | N/A | N/A | N/A | N/A | N/A | N/A |

### Compile peak RSS

| Module | wago | wazero | wazy | wasmtime | v8 | wasm3 | wasmi | wavm | wasmer |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 7.23 MiB | 7.34 MiB | 7.53 MiB | 14.14 MiB | 31.69 MiB | N/A | N/A | 25.33 MiB | 17.86 MiB |
| fib_rec | 7.34 MiB | 7.44 MiB | 7.72 MiB | 13.59 MiB | 31.75 MiB | N/A | N/A | 26.55 MiB | 17.97 MiB |
| memory | 7.36 MiB | 7.39 MiB | 7.69 MiB | 14.58 MiB | 31.69 MiB | N/A | N/A | 28.16 MiB | 18.30 MiB |
| memory_tree | 7.44 MiB | 7.61 MiB | 7.59 MiB | 14.41 MiB | 31.70 MiB | N/A | N/A | 28.66 MiB | 18.39 MiB |
| dispatch | 7.45 MiB | 7.50 MiB | 7.58 MiB | 15.83 MiB | 31.69 MiB | N/A | N/A | 27.83 MiB | 18.44 MiB |
| many_funcs | 7.61 MiB | 7.45 MiB | 7.53 MiB | 18.16 MiB | 31.72 MiB | N/A | N/A | 38.89 MiB | 18.84 MiB |
| linked_list | 7.36 MiB | 7.48 MiB | 7.75 MiB | 13.97 MiB | 31.70 MiB | N/A | N/A | 28.53 MiB | 18.09 MiB |
| nbody | 7.80 MiB | 7.97 MiB | 8.03 MiB | 14.75 MiB | 31.69 MiB | N/A | N/A | 30.42 MiB | 18.95 MiB |
| fannkuch | 7.63 MiB | 8.48 MiB | 8.70 MiB | 15.02 MiB | 31.70 MiB | N/A | N/A | 31.19 MiB | 19.17 MiB |
| matmul | 7.61 MiB | 7.84 MiB | 7.78 MiB | 14.56 MiB | 31.72 MiB | N/A | N/A | 30.31 MiB | 18.75 MiB |
| sha256 | 7.61 MiB | 8.11 MiB | 8.30 MiB | 15.36 MiB | 31.69 MiB | N/A | N/A | 30.67 MiB | 18.89 MiB |
| raytrace | 7.84 MiB | 8.75 MiB | 8.97 MiB | 16.00 MiB | 31.78 MiB | N/A | N/A | 32.33 MiB | 20.11 MiB |
| json-as | 8.23 MiB | 10.98 MiB | 10.34 MiB | 21.33 MiB | 31.86 MiB | N/A | N/A | 43.05 MiB | 21.91 MiB |
| blake-as | 7.97 MiB | 8.28 MiB | 8.42 MiB | 17.53 MiB | 31.78 MiB | N/A | N/A | 33.08 MiB | 20.16 MiB |
| utf-as | 7.84 MiB | 8.50 MiB | 8.61 MiB | 16.94 MiB | 31.75 MiB | N/A | N/A | 31.83 MiB | 19.53 MiB |
| json-as-simd | 8.50 MiB | 9.95 MiB | 10.11 MiB | 21.34 MiB | 31.88 MiB | N/A | N/A | 46.48 MiB | 22.05 MiB |
| blake-as-simd | 8.03 MiB | 9.03 MiB | 9.42 MiB | 20.19 MiB | 31.86 MiB | N/A | N/A | 38.30 MiB | 21.47 MiB |
| utf-as-simd | 8.16 MiB | 8.47 MiB | 8.83 MiB | 19.27 MiB | 31.83 MiB | N/A | N/A | 35.48 MiB | 20.97 MiB |
| coremark | 7.86 MiB | 9.69 MiB | 9.45 MiB | 18.08 MiB | 31.77 MiB | N/A | N/A | 36.22 MiB | 20.61 MiB |
| blake3 | 8.14 MiB | 9.11 MiB | 9.02 MiB | 20.19 MiB | 31.83 MiB | N/A | N/A | 46.03 MiB | 21.45 MiB |
| qoi | 7.94 MiB | 8.09 MiB | 8.28 MiB | 17.30 MiB | 31.75 MiB | N/A | N/A | 32.30 MiB | 19.86 MiB |
| lz4 | 7.88 MiB | 8.19 MiB | 8.34 MiB | 17.17 MiB | 31.75 MiB | N/A | N/A | 32.28 MiB | 19.72 MiB |
| zlib | 8.33 MiB | 14.97 MiB | 14.23 MiB | 21.97 MiB | 31.81 MiB | N/A | N/A | 44.41 MiB | 24.03 MiB |
| zstd | 8.55 MiB | 29.73 MiB | 26.27 MiB | 36.41 MiB | 31.88 MiB | N/A | N/A | 74.34 MiB | 32.11 MiB |
| embench-crc32 | 7.63 MiB | 7.56 MiB | 7.78 MiB | 15.05 MiB | 31.77 MiB | N/A | N/A | 28.70 MiB | 18.77 MiB |
| embench-huffbench | 7.83 MiB | 9.52 MiB | 9.23 MiB | 16.34 MiB | 31.80 MiB | N/A | N/A | 32.63 MiB | 19.83 MiB |
| embench-matmult-int | 7.70 MiB | 8.42 MiB | 8.64 MiB | 15.84 MiB | 31.75 MiB | N/A | N/A | 31.19 MiB | 19.44 MiB |
| embench-nettle-aes | 7.81 MiB | 8.98 MiB | 8.70 MiB | 16.28 MiB | 31.73 MiB | N/A | N/A | 31.98 MiB | 19.89 MiB |
| embench-nettle-sha256 | 7.94 MiB | 8.88 MiB | 9.13 MiB | 17.05 MiB | 31.83 MiB | N/A | N/A | 32.84 MiB | 20.44 MiB |
| embench-qrduino | 8.30 MiB | 15.48 MiB | 13.95 MiB | 22.02 MiB | 31.83 MiB | N/A | N/A | 43.42 MiB | 23.83 MiB |
| sightglass-shootout-base64 | 8.84 MiB | 16.91 MiB | 17.08 MiB | 24.56 MiB | 32.36 MiB | N/A | N/A | 52.98 MiB | 26.45 MiB |
| sightglass-libsodium-hash | 8.59 MiB | 17.59 MiB | 17.47 MiB | 27.44 MiB | 31.94 MiB | N/A | N/A | 62.47 MiB | 26.36 MiB |
| tacle-bsort | 7.48 MiB | 7.67 MiB | 7.81 MiB | 14.14 MiB | 31.75 MiB | N/A | N/A | 29.52 MiB | 18.42 MiB |
| polybench-2mm | 7.67 MiB | 8.73 MiB | 8.52 MiB | 16.22 MiB | 31.77 MiB | N/A | N/A | 31.98 MiB | 19.66 MiB |
| polybench-3mm | 7.73 MiB | 8.95 MiB | 8.98 MiB | 16.59 MiB | 31.73 MiB | N/A | N/A | 32.63 MiB | 19.95 MiB |
| polybench-adi | 7.72 MiB | 8.88 MiB | 8.91 MiB | 16.41 MiB | 31.73 MiB | N/A | N/A | 32.20 MiB | 19.77 MiB |
| polybench-atax | 7.69 MiB | 8.23 MiB | 8.28 MiB | 15.88 MiB | 31.77 MiB | N/A | N/A | 31.39 MiB | 19.34 MiB |
| polybench-bicg | 7.56 MiB | 8.22 MiB | 8.36 MiB | 16.00 MiB | 31.75 MiB | N/A | N/A | 31.45 MiB | 19.52 MiB |
| polybench-cholesky | 7.55 MiB | 8.42 MiB | 8.48 MiB | 16.27 MiB | 31.77 MiB | N/A | N/A | 32.23 MiB | 19.67 MiB |
| polybench-correlation | 7.45 MiB | 8.64 MiB | 8.48 MiB | 16.19 MiB | 31.75 MiB | N/A | N/A | 31.66 MiB | 19.41 MiB |
| polybench-covariance | 7.64 MiB | 8.39 MiB | 8.27 MiB | 15.92 MiB | 31.75 MiB | N/A | N/A | 31.33 MiB | 19.44 MiB |
| polybench-deriche | 7.77 MiB | 8.61 MiB | 8.53 MiB | 16.22 MiB | 31.78 MiB | N/A | N/A | 32.33 MiB | 19.70 MiB |
| polybench-doitgen | 7.70 MiB | 8.53 MiB | 8.64 MiB | 16.23 MiB | 31.78 MiB | N/A | N/A | 32.30 MiB | 19.78 MiB |
| polybench-durbin | 7.53 MiB | 8.09 MiB | 8.17 MiB | 15.91 MiB | 31.75 MiB | N/A | N/A | 31.39 MiB | 19.28 MiB |
| polybench-fdtd-2d | 7.58 MiB | 8.83 MiB | 8.67 MiB | 16.47 MiB | 31.78 MiB | N/A | N/A | 32.95 MiB | 19.94 MiB |
| polybench-floyd-warshall | 7.64 MiB | 7.97 MiB | 8.02 MiB | 15.83 MiB | 31.77 MiB | N/A | N/A | 31.00 MiB | 19.17 MiB |
| polybench-gemm | 7.67 MiB | 8.17 MiB | 8.34 MiB | 16.11 MiB | 31.78 MiB | N/A | N/A | 31.48 MiB | 19.38 MiB |
| polybench-gemver | 7.63 MiB | 8.77 MiB | 9.09 MiB | 16.38 MiB | 31.75 MiB | N/A | N/A | 32.59 MiB | 19.84 MiB |
| polybench-gesummv | 7.50 MiB | 8.22 MiB | 8.36 MiB | 16.11 MiB | 31.80 MiB | N/A | N/A | 31.34 MiB | 19.48 MiB |
| polybench-gramschmidt | 7.61 MiB | 8.44 MiB | 8.34 MiB | 16.17 MiB | 31.75 MiB | N/A | N/A | 31.67 MiB | 19.44 MiB |
| polybench-heat-3d | 7.73 MiB | 8.39 MiB | 8.56 MiB | 16.09 MiB | 31.73 MiB | N/A | N/A | 33.66 MiB | 19.52 MiB |
| polybench-jacobi-1d | 7.70 MiB | 8.03 MiB | 8.16 MiB | 15.78 MiB | 31.73 MiB | N/A | N/A | 30.95 MiB | 19.31 MiB |
| polybench-jacobi-2d | 7.67 MiB | 7.95 MiB | 8.28 MiB | 16.02 MiB | 31.78 MiB | N/A | N/A | 31.11 MiB | 19.33 MiB |
| polybench-lu | 7.66 MiB | 8.59 MiB | 8.55 MiB | 16.13 MiB | 31.77 MiB | N/A | N/A | 32.05 MiB | 19.50 MiB |
| polybench-ludcmp | 7.77 MiB | 9.20 MiB | 9.08 MiB | 16.48 MiB | 31.78 MiB | N/A | N/A | 33.02 MiB | 20.02 MiB |
| polybench-mvt | 7.72 MiB | 8.39 MiB | 8.45 MiB | 16.08 MiB | 31.77 MiB | N/A | N/A | 31.69 MiB | 19.45 MiB |
| polybench-nussinov | 7.67 MiB | 8.16 MiB | 8.38 MiB | 16.00 MiB | 31.80 MiB | N/A | N/A | 31.44 MiB | 19.41 MiB |
| polybench-seidel-2d | 7.61 MiB | 8.02 MiB | 8.09 MiB | 15.73 MiB | 31.72 MiB | N/A | N/A | 30.91 MiB | 19.28 MiB |
| polybench-symm | 7.45 MiB | 8.33 MiB | 8.50 MiB | 16.13 MiB | 31.75 MiB | N/A | N/A | 32.48 MiB | 19.52 MiB |
| polybench-syr2k | 7.66 MiB | 8.23 MiB | 8.30 MiB | 15.91 MiB | 31.77 MiB | N/A | N/A | 31.59 MiB | 19.45 MiB |
| polybench-syrk | 7.73 MiB | 8.06 MiB | 8.20 MiB | 16.16 MiB | 31.75 MiB | N/A | N/A | 31.47 MiB | 19.48 MiB |
| polybench-trisolv | 7.56 MiB | 8.09 MiB | 8.36 MiB | 16.05 MiB | 31.72 MiB | N/A | N/A | 31.22 MiB | 19.44 MiB |
| polybench-trmm | 7.75 MiB | 8.11 MiB | 8.38 MiB | 16.00 MiB | 31.67 MiB | N/A | N/A | 31.61 MiB | 19.42 MiB |

### Cold execution latency

| Module | wago | wazero | wazy | wasmtime | v8 | wasm3 | wasmi | wavm | wasmer |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 4.03 ms | 4.04 ms | 4.09 ms | 5.80 ms | 14.12 ms | 3.16 ms | 4.07 ms | 9.72 ms | 6.22 ms |
| fib_rec | 4.86 ms | 5.59 ms | 5.76 ms | 6.36 ms | 13.79 ms | 12.23 ms | 15.41 ms | 10.12 ms | 7.24 ms |
| memory | 3.89 ms | 3.87 ms | 3.99 ms | 5.44 ms | 13.61 ms | 3.29 ms | 4.68 ms | 10.99 ms | 6.09 ms |
| memory_tree | 3.81 ms | 3.99 ms | 4.16 ms | 5.69 ms | 16.86 ms | 2.77 ms | 5.07 ms | 14.26 ms | 7.57 ms |
| dispatch | 4.96 ms | 4.45 ms | 4.09 ms | 5.48 ms | 13.42 ms | 2.87 ms | 3.66 ms | 12.11 ms | 6.31 ms |
| many_funcs | 4.18 ms | 4.60 ms | 4.63 ms | 5.40 ms | 16.67 ms | 3.30 ms | 4.88 ms | 47.91 ms | 7.65 ms |
| linked_list | 4.88 ms | 4.71 ms | 4.87 ms | 6.18 ms | 14.37 ms | 2.95 ms | 3.82 ms | 11.42 ms | 6.12 ms |
| nbody | 4.15 ms | 4.71 ms | 4.36 ms | 5.60 ms | 16.06 ms | 3.80 ms | 5.91 ms | 20.22 ms | 6.57 ms |
| fannkuch | 5.19 ms | 5.81 ms | 5.57 ms | 6.22 ms | 15.32 ms | 6.59 ms | 8.61 ms | 24.01 ms | 7.19 ms |
| matmul | 4.41 ms | 4.38 ms | 4.20 ms | 5.52 ms | 14.07 ms | 3.92 ms | 5.43 ms | 21.64 ms | 6.41 ms |
| sha256 | 4.25 ms | 4.79 ms | 4.55 ms | 5.58 ms | 14.03 ms | 3.23 ms | 5.75 ms | 20.19 ms | 6.43 ms |
| raytrace | 4.51 ms | 5.19 ms | 5.24 ms | 5.84 ms | 15.13 ms | 3.98 ms | 5.99 ms | 25.94 ms | 6.49 ms |
| json-as | — | — | — | — | — | — | — | — | — |
| blake-as | 4.75 ms | 5.09 ms | 4.83 ms | 5.89 ms | 19.09 ms | 6.68 ms | 10.72 ms | 41.07 ms | 6.68 ms |
| utf-as | 4.18 ms | 4.75 ms | 4.81 ms | 6.10 ms | 14.05 ms | 4.03 ms | 5.92 ms | 27.63 ms | 6.43 ms |
| json-as-simd | — | — | — | — | — | — | — | — | — |
| blake-as-simd | 6.67 ms | 7.35 ms | 7.03 ms | 7.04 ms | 24.30 ms | — | — | 140.33 ms | 6.73 ms |
| utf-as-simd | — | — | — | — | — | — | — | — | — |
| polybench-2mm | 4.47 ms | 5.22 ms | 4.60 ms | 5.45 ms | 15.36 ms | 4.29 ms | 6.07 ms | 29.00 ms | 6.68 ms |
| polybench-3mm | 4.51 ms | 5.55 ms | 5.88 ms | 6.30 ms | 15.61 ms | 5.31 ms | 7.45 ms | 33.55 ms | 6.64 ms |
| polybench-adi | 5.83 ms | 6.33 ms | 7.74 ms | 7.15 ms | 15.94 ms | 7.37 ms | 12.12 ms | 33.67 ms | 7.46 ms |
| polybench-atax | 3.96 ms | 4.64 ms | 4.69 ms | 5.37 ms | 13.10 ms | 2.88 ms | 3.89 ms | 21.92 ms | 6.75 ms |
| polybench-bicg | 4.36 ms | 4.68 ms | 4.69 ms | 5.55 ms | 12.93 ms | 2.93 ms | 3.94 ms | 24.01 ms | 6.71 ms |
| polybench-cholesky | 6.47 ms | 6.96 ms | 6.69 ms | 6.88 ms | 18.70 ms | 13.73 ms | 19.36 ms | 34.27 ms | 6.99 ms |
| polybench-correlation | 4.34 ms | 4.89 ms | 4.76 ms | 5.54 ms | 14.28 ms | 4.61 ms | 6.58 ms | 30.07 ms | 7.14 ms |
| polybench-covariance | 4.57 ms | 4.94 ms | 4.66 ms | 5.47 ms | 14.39 ms | 4.62 ms | 6.46 ms | 25.59 ms | 6.47 ms |
| polybench-deriche | 4.50 ms | 5.09 ms | 4.93 ms | 5.77 ms | 15.99 ms | 6.09 ms | 8.15 ms | 33.15 ms | 6.61 ms |
| polybench-doitgen | 4.35 ms | 5.82 ms | 5.09 ms | 5.81 ms | 14.86 ms | 5.47 ms | 7.73 ms | 33.42 ms | 6.96 ms |
| polybench-durbin | 4.39 ms | 4.49 ms | 4.56 ms | 5.55 ms | 13.52 ms | 2.74 ms | 3.84 ms | 21.83 ms | 7.17 ms |
| polybench-fdtd-2d | 6.09 ms | 6.09 ms | 5.46 ms | 6.10 ms | 15.33 ms | 8.34 ms | 11.73 ms | 35.68 ms | 6.71 ms |
| polybench-floyd-warshall | 10.48 ms | 10.06 ms | 8.25 ms | 7.44 ms | 19.70 ms | 66.29 ms | 51.21 ms | 22.28 ms | 8.28 ms |
| polybench-gemm | 4.17 ms | 4.75 ms | 4.70 ms | 5.41 ms | 14.51 ms | 4.88 ms | 7.25 ms | 25.87 ms | 6.37 ms |
| polybench-gemver | 4.12 ms | 4.69 ms | 4.71 ms | 5.76 ms | 13.62 ms | 3.05 ms | 4.23 ms | 31.61 ms | 6.11 ms |
| polybench-gesummv | 4.24 ms | 4.53 ms | 4.41 ms | 4.95 ms | 13.27 ms | 2.95 ms | 4.08 ms | 21.28 ms | 6.29 ms |
| polybench-gramschmidt | 4.36 ms | 5.06 ms | 5.03 ms | 5.71 ms | 15.09 ms | 5.45 ms | 7.80 ms | 28.81 ms | 6.44 ms |
| polybench-heat-3d | 4.95 ms | 6.06 ms | 7.30 ms | 6.08 ms | 14.92 ms | 11.37 ms | 19.38 ms | 38.44 ms | 7.12 ms |
| polybench-jacobi-1d | 4.19 ms | 4.46 ms | 4.58 ms | 5.66 ms | 18.26 ms | 2.94 ms | 3.89 ms | 19.02 ms | 6.45 ms |
| polybench-jacobi-2d | 5.02 ms | 5.74 ms | 5.33 ms | 5.84 ms | 15.16 ms | 8.83 ms | 14.25 ms | 22.84 ms | 6.49 ms |
| polybench-lu | 6.01 ms | 6.94 ms | 6.46 ms | 6.79 ms | 15.95 ms | 14.24 ms | 21.93 ms | 32.72 ms | 7.15 ms |
| polybench-ludcmp | 6.24 ms | 7.00 ms | 6.24 ms | 6.47 ms | 15.07 ms | 13.25 ms | 18.84 ms | 40.70 ms | 6.94 ms |
| polybench-mvt | 4.01 ms | 4.73 ms | 4.52 ms | 5.46 ms | 24.26 ms | 3.02 ms | 4.10 ms | 25.25 ms | 6.46 ms |
| polybench-nussinov | 6.59 ms | 6.79 ms | 5.68 ms | 6.27 ms | 15.80 ms | 9.84 ms | 11.85 ms | 24.58 ms | 6.90 ms |
| polybench-seidel-2d | 7.15 ms | 7.62 ms | 7.17 ms | 8.67 ms | 18.45 ms | 10.75 ms | 17.00 ms | 21.82 ms | 8.75 ms |
| polybench-symm | 4.36 ms | 5.16 ms | 5.26 ms | 6.35 ms | 21.00 ms | 4.31 ms | 6.24 ms | 31.36 ms | 6.48 ms |
| polybench-syr2k | 4.47 ms | 5.07 ms | 4.96 ms | 5.66 ms | 15.19 ms | 5.21 ms | 7.24 ms | 25.50 ms | 6.42 ms |
| polybench-syrk | 4.24 ms | 4.64 ms | 4.52 ms | 5.41 ms | 14.48 ms | 4.35 ms | 6.26 ms | 24.70 ms | 6.64 ms |
| polybench-trisolv | 3.94 ms | 4.89 ms | 4.34 ms | 5.38 ms | 13.05 ms | 2.75 ms | 3.73 ms | 21.09 ms | 6.42 ms |
| polybench-trmm | 4.30 ms | 5.69 ms | 6.03 ms | 6.88 ms | 16.97 ms | 4.35 ms | 5.66 ms | 26.44 ms | 7.16 ms |

### Unsupported or failed cells

29 cells were unsupported or failed. Full reasons are retained in the raw JSON capture.

## AMD64 — AMD Ryzen 7 7800X3D 8-Core Processor

Source: `7c382581d8c53f06bb53879ddf4e28db6c47ce80`.

### Engine versions

| Engine | Version |
|---|---|
| wago | wago 0.0.0 (7c382581) |
| wazero | wazero v1.12.0 adapter |
| wazy | v0.3.0 |
| wasmtime | wasmtime 45.0.1 (83166ba31 2026-06-05) |
| v8 | V8 version 15.1.42 |
| wasm3 | Wasm3 v0.9.0 on x86_64 |
| wasmi | wasmi_cli 1.1.0 |
| wavm | WAVM version 0.0.0-prerelease |
| wasmer | wasmer 7.1.0 |

### Coverage and geometric means

| Engine | Compile coverage | Compile wall | Allocated heap | Peak RSS | Execute coverage | Cold execute |
|---|---:|---:|---:|---:|---:|---:|
| wago | 63/63 | 6.42 ms | 109.50 KiB | 6.77 MiB | 45/48 | 7.34 ms |
| wazero | 63/63 | 7.83 ms | 1.05 MiB | 8.35 MiB | 45/48 | 8.11 ms |
| wazy | 63/63 | 7.58 ms | 961.56 KiB | 8.42 MiB | 45/48 | 7.73 ms |
| wasmtime | 63/63 | 9.77 ms | N/A | 22.08 MiB | 45/48 | 7.56 ms |
| v8 | 63/63 | 14.44 ms | N/A | 27.55 MiB | 45/48 | 16.84 ms |
| wasm3 | 0/63 | N/A | N/A | N/A | 44/48 | 9.91 ms |
| wasmi | 0/63 | N/A | N/A | N/A | 44/48 | 12.34 ms |
| wavm | 63/63 | 62.98 ms | N/A | 45.78 MiB | 45/48 | 48.52 ms |
| wasmer | 63/63 | 14.80 ms | N/A | 56.84 MiB | 45/48 | 10.88 ms |

### Compile wall latency

| Module | wago | wazero | wazy | wasmtime | v8 | wasm3 | wasmi | wavm | wasmer |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 6.00 ms | 6.02 ms | 6.44 ms | 6.46 ms | 13.57 ms | N/A | N/A | 10.08 ms | 10.56 ms |
| fib_rec | 6.06 ms | 5.96 ms | 5.94 ms | 6.59 ms | 18.15 ms | N/A | N/A | 12.05 ms | 11.96 ms |
| memory | 6.14 ms | 5.99 ms | 6.17 ms | 6.99 ms | 14.91 ms | N/A | N/A | 17.52 ms | 12.32 ms |
| memory_tree | 6.13 ms | 6.05 ms | 6.14 ms | 7.01 ms | 16.39 ms | N/A | N/A | 16.33 ms | 12.71 ms |
| dispatch | 6.22 ms | 6.71 ms | 6.09 ms | 7.90 ms | 13.57 ms | N/A | N/A | 12.26 ms | 11.43 ms |
| many_funcs | 6.34 ms | 6.73 ms | 6.64 ms | 12.69 ms | 13.34 ms | N/A | N/A | 100.37 ms | 17.80 ms |
| linked_list | 6.13 ms | 6.10 ms | 6.12 ms | 7.40 ms | 15.75 ms | N/A | N/A | 13.17 ms | 12.25 ms |
| nbody | 6.22 ms | 6.43 ms | 6.34 ms | 7.38 ms | 14.38 ms | N/A | N/A | 25.92 ms | 12.50 ms |
| fannkuch | 6.17 ms | 6.65 ms | 6.58 ms | 7.86 ms | 13.43 ms | N/A | N/A | 34.98 ms | 12.11 ms |
| matmul | 6.13 ms | 6.45 ms | 6.22 ms | 8.38 ms | 14.04 ms | N/A | N/A | 48.51 ms | 11.36 ms |
| sha256 | 6.19 ms | 6.69 ms | 6.35 ms | 7.34 ms | 17.79 ms | N/A | N/A | 33.28 ms | 11.71 ms |
| raytrace | 6.32 ms | 7.44 ms | 6.75 ms | 9.43 ms | 13.86 ms | N/A | N/A | 36.49 ms | 14.87 ms |
| json-as | 6.64 ms | 13.46 ms | 11.95 ms | 21.58 ms | 15.27 ms | N/A | N/A | 291.01 ms | 26.04 ms |
| blake-as | 6.44 ms | 6.99 ms | 6.69 ms | 10.21 ms | 14.70 ms | N/A | N/A | 68.60 ms | 16.79 ms |
| utf-as | 6.41 ms | 6.76 ms | 6.65 ms | 8.79 ms | 14.11 ms | N/A | N/A | 43.55 ms | 14.36 ms |
| json-as-simd | 6.72 ms | 14.02 ms | 11.87 ms | 23.86 ms | 13.51 ms | N/A | N/A | 361.02 ms | 28.58 ms |
| blake-as-simd | 6.47 ms | 9.27 ms | 9.12 ms | 17.02 ms | 15.10 ms | N/A | N/A | 205.26 ms | 21.73 ms |
| utf-as-simd | 6.46 ms | 7.45 ms | 7.08 ms | 12.34 ms | 14.20 ms | N/A | N/A | 115.30 ms | 17.86 ms |
| coremark | 6.47 ms | 9.81 ms | 7.90 ms | 11.97 ms | 14.75 ms | N/A | N/A | 194.08 ms | 18.46 ms |
| blake3 | 6.44 ms | 9.63 ms | 8.90 ms | 16.43 ms | 15.71 ms | N/A | N/A | 574.95 ms | 20.33 ms |
| qoi | 6.38 ms | 6.93 ms | 6.81 ms | 8.99 ms | 13.64 ms | N/A | N/A | 81.02 ms | 13.97 ms |
| lz4 | 6.27 ms | 7.36 ms | 6.90 ms | 9.59 ms | 13.74 ms | N/A | N/A | 59.39 ms | 13.65 ms |
| zlib | 6.41 ms | 17.64 ms | 15.39 ms | 19.51 ms | 13.41 ms | N/A | N/A | 259.79 ms | 24.85 ms |
| zstd | 10.12 ms | 51.67 ms | 44.29 ms | 52.80 ms | 14.15 ms | N/A | N/A | 1.03 s | 60.29 ms |
| embench-crc32 | 6.17 ms | 6.37 ms | 6.21 ms | 6.67 ms | 14.28 ms | N/A | N/A | 14.00 ms | 10.81 ms |
| embench-huffbench | 6.35 ms | 7.76 ms | 7.01 ms | 8.54 ms | 14.04 ms | N/A | N/A | 74.78 ms | 15.74 ms |
| embench-matmult-int | 6.20 ms | 6.94 ms | 6.95 ms | 8.14 ms | 16.25 ms | N/A | N/A | 31.58 ms | 13.30 ms |
| embench-nettle-aes | 6.45 ms | 7.00 ms | 7.18 ms | 8.90 ms | 14.46 ms | N/A | N/A | 41.44 ms | 14.57 ms |
| embench-nettle-sha256 | 6.29 ms | 7.39 ms | 6.84 ms | 10.78 ms | 14.61 ms | N/A | N/A | 71.89 ms | 16.80 ms |
| embench-qrduino | 6.67 ms | 15.98 ms | 13.34 ms | 17.62 ms | 13.37 ms | N/A | N/A | 275.54 ms | 24.44 ms |
| sightglass-shootout-base64 | 8.42 ms | 26.80 ms | 23.06 ms | 31.30 ms | 13.81 ms | N/A | N/A | 468.43 ms | 36.04 ms |
| sightglass-libsodium-hash | 10.14 ms | 27.18 ms | 22.47 ms | 43.51 ms | 15.10 ms | N/A | N/A | 768.27 ms | 47.21 ms |
| tacle-bsort | 6.16 ms | 6.22 ms | 6.90 ms | 6.91 ms | 14.66 ms | N/A | N/A | 22.78 ms | 12.35 ms |
| polybench-2mm | 6.46 ms | 6.81 ms | 7.10 ms | 8.39 ms | 13.63 ms | N/A | N/A | 81.99 ms | 12.87 ms |
| polybench-3mm | 6.42 ms | 6.92 ms | 6.87 ms | 9.08 ms | 13.41 ms | N/A | N/A | 99.40 ms | 13.48 ms |
| polybench-adi | 6.29 ms | 6.86 ms | 6.85 ms | 8.76 ms | 13.53 ms | N/A | N/A | 66.88 ms | 12.63 ms |
| polybench-atax | 6.25 ms | 6.83 ms | 6.83 ms | 7.87 ms | 13.28 ms | N/A | N/A | 40.02 ms | 11.88 ms |
| polybench-bicg | 6.28 ms | 6.83 ms | 6.92 ms | 8.48 ms | 13.35 ms | N/A | N/A | 38.37 ms | 13.37 ms |
| polybench-cholesky | 6.35 ms | 7.15 ms | 6.82 ms | 8.32 ms | 14.96 ms | N/A | N/A | 81.90 ms | 13.71 ms |
| polybench-correlation | 6.28 ms | 6.96 ms | 6.84 ms | 8.16 ms | 13.78 ms | N/A | N/A | 61.49 ms | 13.79 ms |
| polybench-covariance | 6.23 ms | 6.70 ms | 6.67 ms | 8.14 ms | 14.59 ms | N/A | N/A | 52.75 ms | 12.11 ms |
| polybench-deriche | 6.26 ms | 7.24 ms | 6.78 ms | 8.42 ms | 16.41 ms | N/A | N/A | 83.07 ms | 12.56 ms |
| polybench-doitgen | 6.29 ms | 6.85 ms | 6.90 ms | 8.46 ms | 13.49 ms | N/A | N/A | 55.66 ms | 12.70 ms |
| polybench-durbin | 6.26 ms | 6.72 ms | 6.73 ms | 8.09 ms | 14.97 ms | N/A | N/A | 41.66 ms | 12.15 ms |
| polybench-fdtd-2d | 6.44 ms | 7.37 ms | 6.99 ms | 8.76 ms | 14.04 ms | N/A | N/A | 65.18 ms | 13.91 ms |
| polybench-floyd-warshall | 6.22 ms | 6.56 ms | 6.49 ms | 7.88 ms | 14.27 ms | N/A | N/A | 36.63 ms | 11.77 ms |
| polybench-gemm | 6.27 ms | 6.73 ms | 6.67 ms | 8.02 ms | 14.69 ms | N/A | N/A | 62.22 ms | 13.46 ms |
| polybench-gemver | 6.24 ms | 7.02 ms | 7.02 ms | 8.66 ms | 13.73 ms | N/A | N/A | 62.86 ms | 12.67 ms |
| polybench-gesummv | 6.26 ms | 6.68 ms | 6.68 ms | 7.94 ms | 13.70 ms | N/A | N/A | 39.86 ms | 13.56 ms |
| polybench-gramschmidt | 6.30 ms | 6.75 ms | 6.81 ms | 8.17 ms | 13.37 ms | N/A | N/A | 73.32 ms | 13.23 ms |
| polybench-heat-3d | 6.43 ms | 6.88 ms | 6.83 ms | 8.37 ms | 15.12 ms | N/A | N/A | 66.95 ms | 13.33 ms |
| polybench-jacobi-1d | 6.30 ms | 6.67 ms | 6.54 ms | 8.75 ms | 14.31 ms | N/A | N/A | 31.87 ms | 12.10 ms |
| polybench-jacobi-2d | 6.23 ms | 6.62 ms | 6.96 ms | 7.86 ms | 13.56 ms | N/A | N/A | 41.71 ms | 11.81 ms |
| polybench-lu | 6.28 ms | 6.84 ms | 7.02 ms | 8.35 ms | 13.51 ms | N/A | N/A | 84.22 ms | 12.30 ms |
| polybench-ludcmp | 6.37 ms | 7.45 ms | 7.54 ms | 9.08 ms | 13.05 ms | N/A | N/A | 117.53 ms | 14.54 ms |
| polybench-mvt | 6.28 ms | 6.75 ms | 7.07 ms | 8.18 ms | 14.56 ms | N/A | N/A | 52.85 ms | 12.44 ms |
| polybench-nussinov | 6.08 ms | 6.73 ms | 6.44 ms | 8.18 ms | 16.10 ms | N/A | N/A | 37.63 ms | 12.09 ms |
| polybench-seidel-2d | 6.15 ms | 6.59 ms | 6.48 ms | 7.69 ms | 15.52 ms | N/A | N/A | 30.58 ms | 13.17 ms |
| polybench-symm | 6.09 ms | 6.97 ms | 6.73 ms | 8.38 ms | 14.84 ms | N/A | N/A | 62.69 ms | 14.13 ms |
| polybench-syr2k | 6.31 ms | 6.66 ms | 6.73 ms | 8.18 ms | 14.57 ms | N/A | N/A | 60.94 ms | 12.44 ms |
| polybench-syrk | 6.26 ms | 6.93 ms | 6.68 ms | 8.32 ms | 16.12 ms | N/A | N/A | 58.31 ms | 12.13 ms |
| polybench-trisolv | 6.16 ms | 6.83 ms | 6.65 ms | 7.79 ms | 13.97 ms | N/A | N/A | 40.21 ms | 12.35 ms |
| polybench-trmm | 6.35 ms | 6.85 ms | 6.74 ms | 8.03 ms | 15.52 ms | N/A | N/A | 55.60 ms | 12.32 ms |

### Allocated compile heap

Only Wago, Wazero, and Wazy expose this measurement.

| Module | wago | wazero | wazy | wasmtime | v8 | wasm3 | wasmi | wavm | wasmer |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 23.30 KiB | 269.68 KiB | 237.35 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| fib_rec | 24.30 KiB | 274.08 KiB | 242.02 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| memory | 25.95 KiB | 297.08 KiB | 265.11 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| memory_tree | 30.68 KiB | 312.26 KiB | 278.88 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| dispatch | 42.30 KiB | 275.83 KiB | 244.40 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| many_funcs | 166.15 KiB | 347.24 KiB | 300.30 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| linked_list | 27.07 KiB | 312.07 KiB | 270.42 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| nbody | 83.66 KiB | 730.01 KiB | 630.47 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| fannkuch | 86.85 KiB | 1018.70 KiB | 906.32 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| matmul | 79.21 KiB | 623.99 KiB | 469.07 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| sha256 | 83.28 KiB | 807.08 KiB | 717.84 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| raytrace | 147.98 KiB | 1.55 MiB | 1.48 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| json-as | 220.73 KiB | 2.48 MiB | 2.21 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| blake-as | 155.70 KiB | 1.11 MiB | 991.08 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| utf-as | 217.11 KiB | 1.17 MiB | 1.02 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| json-as-simd | 231.53 KiB | 2.24 MiB | 2.11 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| blake-as-simd | 298.07 KiB | 1.95 MiB | 2.08 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| utf-as-simd | 236.46 KiB | 1.31 MiB | 1.24 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| coremark | 264.13 KiB | 2.05 MiB | 1.74 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| blake3 | 166.41 KiB | 1.55 MiB | 1.35 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| qoi | 89.28 KiB | 834.60 KiB | 693.27 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| lz4 | 81.09 KiB | 950.65 KiB | 783.45 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| zlib | 330.77 KiB | 7.33 MiB | 6.53 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| zstd | 1.04 MiB | 23.57 MiB | 21.87 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-crc32 | 54.55 KiB | 373.38 KiB | 290.59 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-huffbench | 164.09 KiB | 1.88 MiB | 1.51 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-matmult-int | 105.91 KiB | 1.09 MiB | 984.49 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-nettle-aes | 176.63 KiB | 1.64 MiB | 1.22 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-nettle-sha256 | 275.38 KiB | 1.53 MiB | 1.48 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| embench-qrduino | 557.72 KiB | 7.67 MiB | 5.92 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| sightglass-shootout-base64 | 566.32 KiB | 8.47 MiB | 9.16 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| sightglass-libsodium-hash | 417.27 KiB | 9.10 MiB | 10.01 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| tacle-bsort | 44.13 KiB | 472.53 KiB | 388.96 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-2mm | 85.60 KiB | 1.01 MiB | 990.73 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-3mm | 143.63 KiB | 1.35 MiB | 1.34 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-adi | 145.59 KiB | 1.26 MiB | 1.10 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-atax | 80.86 KiB | 802.88 KiB | 727.72 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-bicg | 83.90 KiB | 854.45 KiB | 834.63 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-cholesky | 85.52 KiB | 1.05 MiB | 952.66 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-correlation | 85.48 KiB | 943.35 KiB | 846.37 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-covariance | 81.91 KiB | 859.09 KiB | 745.44 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-deriche | 87.13 KiB | 1.03 MiB | 966.87 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-doitgen | 147.48 KiB | 1.22 MiB | 1020.38 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-durbin | 84.00 KiB | 817.47 KiB | 717.72 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-fdtd-2d | 143.05 KiB | 1.06 MiB | 1006.85 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-floyd-warshall | 81.27 KiB | 644.54 KiB | 570.63 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-gemm | 81.28 KiB | 860.73 KiB | 769.08 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-gemver | 145.16 KiB | 1.25 MiB | 1.38 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-gesummv | 80.87 KiB | 788.73 KiB | 741.61 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-gramschmidt | 85.32 KiB | 945.85 KiB | 804.73 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-heat-3d | 89.27 KiB | 1.12 MiB | 947.83 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-jacobi-1d | 80.80 KiB | 661.14 KiB | 566.20 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-jacobi-2d | 80.93 KiB | 739.98 KiB | 631.73 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-lu | 85.52 KiB | 1.05 MiB | 956.55 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-ludcmp | 145.22 KiB | 1.64 MiB | 1.46 MiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-mvt | 85.02 KiB | 924.52 KiB | 856.88 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-nussinov | 84.28 KiB | 818.87 KiB | 718.59 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-seidel-2d | 83.09 KiB | 646.87 KiB | 565.92 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-symm | 85.68 KiB | 966.88 KiB | 892.66 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-syr2k | 84.18 KiB | 850.18 KiB | 756.71 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-syrk | 84.16 KiB | 847.12 KiB | 748.21 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-trisolv | 83.02 KiB | 739.91 KiB | 697.26 KiB | N/A | N/A | N/A | N/A | N/A | N/A |
| polybench-trmm | 85.15 KiB | 860.43 KiB | 749.76 KiB | N/A | N/A | N/A | N/A | N/A | N/A |

### Compile peak RSS

| Module | wago | wazero | wazy | wasmtime | v8 | wasm3 | wasmi | wavm | wasmer |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 6.37 MiB | 6.70 MiB | 7.00 MiB | 20.64 MiB | 27.59 MiB | N/A | N/A | 38.24 MiB | 55.29 MiB |
| fib_rec | 6.37 MiB | 6.83 MiB | 7.00 MiB | 20.93 MiB | 27.43 MiB | N/A | N/A | 39.96 MiB | 55.24 MiB |
| memory | 6.50 MiB | 6.83 MiB | 7.00 MiB | 20.76 MiB | 27.59 MiB | N/A | N/A | 41.29 MiB | 55.45 MiB |
| memory_tree | 6.50 MiB | 6.83 MiB | 7.13 MiB | 21.03 MiB | 27.37 MiB | N/A | N/A | 41.31 MiB | 55.56 MiB |
| dispatch | 6.50 MiB | 6.83 MiB | 7.00 MiB | 21.10 MiB | 27.41 MiB | N/A | N/A | 40.50 MiB | 55.50 MiB |
| many_funcs | 6.62 MiB | 6.83 MiB | 7.00 MiB | 22.11 MiB | 27.54 MiB | N/A | N/A | 41.46 MiB | 55.34 MiB |
| linked_list | 6.50 MiB | 6.83 MiB | 7.00 MiB | 20.70 MiB | 27.44 MiB | N/A | N/A | 41.05 MiB | 55.43 MiB |
| nbody | 6.75 MiB | 7.45 MiB | 7.63 MiB | 21.38 MiB | 27.52 MiB | N/A | N/A | 42.59 MiB | 56.06 MiB |
| fannkuch | 6.75 MiB | 7.95 MiB | 8.00 MiB | 21.69 MiB | 27.53 MiB | N/A | N/A | 42.70 MiB | 56.40 MiB |
| matmul | 6.75 MiB | 7.33 MiB | 7.50 MiB | 21.32 MiB | 27.50 MiB | N/A | N/A | 43.48 MiB | 55.94 MiB |
| sha256 | 6.75 MiB | 7.58 MiB | 7.75 MiB | 21.33 MiB | 27.40 MiB | N/A | N/A | 43.02 MiB | 56.14 MiB |
| raytrace | 6.75 MiB | 8.33 MiB | 8.50 MiB | 22.09 MiB | 27.47 MiB | N/A | N/A | 43.52 MiB | 56.71 MiB |
| json-as | 7.06 MiB | 9.58 MiB | 9.63 MiB | 23.01 MiB | 27.57 MiB | N/A | N/A | 47.73 MiB | 58.11 MiB |
| blake-as | 6.87 MiB | 7.83 MiB | 8.00 MiB | 21.59 MiB | 27.50 MiB | N/A | N/A | 44.12 MiB | 56.37 MiB |
| utf-as | 6.75 MiB | 7.95 MiB | 8.13 MiB | 22.08 MiB | 27.45 MiB | N/A | N/A | 43.23 MiB | 56.72 MiB |
| json-as-simd | 7.06 MiB | 9.45 MiB | 9.38 MiB | 22.91 MiB | 27.56 MiB | N/A | N/A | 49.95 MiB | 57.20 MiB |
| blake-as-simd | 7.00 MiB | 8.82 MiB | 9.25 MiB | 22.16 MiB | 27.56 MiB | N/A | N/A | 47.38 MiB | 57.46 MiB |
| utf-as-simd | 6.93 MiB | 8.20 MiB | 8.38 MiB | 22.20 MiB | 27.66 MiB | N/A | N/A | 45.15 MiB | 56.77 MiB |
| coremark | 6.87 MiB | 9.33 MiB | 8.88 MiB | 22.61 MiB | 27.69 MiB | N/A | N/A | 48.27 MiB | 57.33 MiB |
| blake3 | 6.87 MiB | 8.58 MiB | 8.50 MiB | 22.23 MiB | 27.58 MiB | N/A | N/A | 63.18 MiB | 56.90 MiB |
| qoi | 6.75 MiB | 7.70 MiB | 7.75 MiB | 21.74 MiB | 27.63 MiB | N/A | N/A | 44.32 MiB | 56.63 MiB |
| lz4 | 6.81 MiB | 7.83 MiB | 7.88 MiB | 21.84 MiB | 27.34 MiB | N/A | N/A | 43.42 MiB | 56.41 MiB |
| zlib | 7.18 MiB | 14.70 MiB | 13.69 MiB | 25.07 MiB | 27.78 MiB | N/A | N/A | 56.30 MiB | 59.41 MiB |
| zstd | 7.75 MiB | 29.33 MiB | 27.07 MiB | 31.53 MiB | 27.66 MiB | N/A | N/A | 92.14 MiB | 66.68 MiB |
| embench-crc32 | 6.62 MiB | 7.08 MiB | 7.13 MiB | 20.82 MiB | 27.43 MiB | N/A | N/A | 41.45 MiB | 55.99 MiB |
| embench-huffbench | 6.87 MiB | 8.83 MiB | 8.63 MiB | 22.25 MiB | 27.52 MiB | N/A | N/A | 45.52 MiB | 56.66 MiB |
| embench-matmult-int | 6.87 MiB | 7.95 MiB | 8.00 MiB | 21.42 MiB | 27.50 MiB | N/A | N/A | 42.78 MiB | 56.33 MiB |
| embench-nettle-aes | 6.87 MiB | 8.58 MiB | 8.38 MiB | 21.98 MiB | 27.48 MiB | N/A | N/A | 43.06 MiB | 56.43 MiB |
| embench-nettle-sha256 | 6.87 MiB | 8.33 MiB | 8.63 MiB | 21.95 MiB | 27.59 MiB | N/A | N/A | 44.36 MiB | 56.90 MiB |
| embench-qrduino | 7.43 MiB | 14.95 MiB | 13.07 MiB | 23.74 MiB | 27.79 MiB | N/A | N/A | 53.62 MiB | 58.85 MiB |
| sightglass-shootout-base64 | 7.62 MiB | 15.95 MiB | 16.38 MiB | 25.49 MiB | 27.82 MiB | N/A | N/A | 62.95 MiB | 60.82 MiB |
| sightglass-libsodium-hash | 7.50 MiB | 16.32 MiB | 16.69 MiB | 26.50 MiB | 27.85 MiB | N/A | N/A | 77.51 MiB | 60.84 MiB |
| tacle-bsort | 6.50 MiB | 7.08 MiB | 7.25 MiB | 21.21 MiB | 27.52 MiB | N/A | N/A | 42.13 MiB | 55.93 MiB |
| polybench-2mm | 6.75 MiB | 8.20 MiB | 8.13 MiB | 22.20 MiB | 27.51 MiB | N/A | N/A | 46.60 MiB | 56.64 MiB |
| polybench-3mm | 6.75 MiB | 8.45 MiB | 8.38 MiB | 21.93 MiB | 27.48 MiB | N/A | N/A | 47.13 MiB | 56.62 MiB |
| polybench-adi | 6.75 MiB | 8.33 MiB | 8.38 MiB | 22.09 MiB | 27.45 MiB | N/A | N/A | 44.61 MiB | 56.60 MiB |
| polybench-atax | 6.50 MiB | 7.70 MiB | 7.75 MiB | 21.65 MiB | 27.54 MiB | N/A | N/A | 43.47 MiB | 56.44 MiB |
| polybench-bicg | 6.75 MiB | 7.70 MiB | 7.88 MiB | 21.59 MiB | 27.65 MiB | N/A | N/A | 43.62 MiB | 56.52 MiB |
| polybench-cholesky | 6.75 MiB | 7.95 MiB | 8.00 MiB | 21.79 MiB | 27.56 MiB | N/A | N/A | 45.42 MiB | 56.48 MiB |
| polybench-correlation | 6.75 MiB | 7.83 MiB | 8.00 MiB | 21.87 MiB | 27.53 MiB | N/A | N/A | 44.35 MiB | 56.46 MiB |
| polybench-covariance | 6.50 MiB | 7.70 MiB | 7.88 MiB | 21.98 MiB | 27.47 MiB | N/A | N/A | 44.23 MiB | 57.27 MiB |
| polybench-deriche | 6.75 MiB | 8.20 MiB | 8.13 MiB | 22.21 MiB | 27.47 MiB | N/A | N/A | 45.28 MiB | 56.54 MiB |
| polybench-doitgen | 6.75 MiB | 8.20 MiB | 8.13 MiB | 21.92 MiB | 27.65 MiB | N/A | N/A | 44.82 MiB | 56.63 MiB |
| polybench-durbin | 6.75 MiB | 7.70 MiB | 7.75 MiB | 21.95 MiB | 27.53 MiB | N/A | N/A | 43.92 MiB | 56.46 MiB |
| polybench-fdtd-2d | 6.75 MiB | 8.08 MiB | 8.13 MiB | 21.99 MiB | 27.66 MiB | N/A | N/A | 44.44 MiB | 56.88 MiB |
| polybench-floyd-warshall | 6.50 MiB | 7.45 MiB | 7.63 MiB | 21.87 MiB | 27.52 MiB | N/A | N/A | 43.37 MiB | 56.25 MiB |
| polybench-gemm | 6.50 MiB | 7.70 MiB | 7.88 MiB | 21.76 MiB | 27.54 MiB | N/A | N/A | 45.11 MiB | 56.47 MiB |
| polybench-gemver | 6.75 MiB | 8.20 MiB | 8.50 MiB | 21.83 MiB | 27.45 MiB | N/A | N/A | 45.14 MiB | 56.67 MiB |
| polybench-gesummv | 6.50 MiB | 7.58 MiB | 7.88 MiB | 21.84 MiB | 27.55 MiB | N/A | N/A | 43.69 MiB | 56.93 MiB |
| polybench-gramschmidt | 6.75 MiB | 7.83 MiB | 7.88 MiB | 21.64 MiB | 27.44 MiB | N/A | N/A | 45.03 MiB | 56.54 MiB |
| polybench-heat-3d | 6.75 MiB | 8.08 MiB | 8.13 MiB | 21.97 MiB | 27.61 MiB | N/A | N/A | 45.87 MiB | 56.57 MiB |
| polybench-jacobi-1d | 6.50 MiB | 7.45 MiB | 7.63 MiB | 21.38 MiB | 27.46 MiB | N/A | N/A | 43.26 MiB | 56.36 MiB |
| polybench-jacobi-2d | 6.50 MiB | 7.58 MiB | 7.75 MiB | 21.47 MiB | 27.61 MiB | N/A | N/A | 43.72 MiB | 56.21 MiB |
| polybench-lu | 6.75 MiB | 7.95 MiB | 8.00 MiB | 21.85 MiB | 27.67 MiB | N/A | N/A | 45.68 MiB | 56.73 MiB |
| polybench-ludcmp | 6.75 MiB | 8.83 MiB | 8.63 MiB | 22.10 MiB | 27.60 MiB | N/A | N/A | 47.23 MiB | 56.76 MiB |
| polybench-mvt | 6.75 MiB | 7.83 MiB | 7.88 MiB | 21.96 MiB | 27.62 MiB | N/A | N/A | 44.44 MiB | 57.00 MiB |
| polybench-nussinov | 6.75 MiB | 7.70 MiB | 7.75 MiB | 21.43 MiB | 27.57 MiB | N/A | N/A | 43.38 MiB | 56.52 MiB |
| polybench-seidel-2d | 6.75 MiB | 7.45 MiB | 7.63 MiB | 21.39 MiB | 27.33 MiB | N/A | N/A | 43.05 MiB | 56.95 MiB |
| polybench-symm | 6.75 MiB | 7.83 MiB | 8.00 MiB | 22.02 MiB | 27.56 MiB | N/A | N/A | 45.06 MiB | 56.55 MiB |
| polybench-syr2k | 6.75 MiB | 7.70 MiB | 7.88 MiB | 21.97 MiB | 27.42 MiB | N/A | N/A | 44.84 MiB | 56.50 MiB |
| polybench-syrk | 6.75 MiB | 7.70 MiB | 7.88 MiB | 21.53 MiB | 27.60 MiB | N/A | N/A | 45.03 MiB | 56.42 MiB |
| polybench-trisolv | 6.75 MiB | 7.58 MiB | 7.75 MiB | 21.52 MiB | 27.51 MiB | N/A | N/A | 43.59 MiB | 56.55 MiB |
| polybench-trmm | 6.75 MiB | 7.70 MiB | 7.88 MiB | 21.83 MiB | 27.71 MiB | N/A | N/A | 44.82 MiB | 56.63 MiB |

### Cold execution latency

| Module | wago | wazero | wazy | wasmtime | v8 | wasm3 | wasmi | wavm | wasmer |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| tiny | 6.33 ms | 6.22 ms | 6.43 ms | 6.82 ms | 17.83 ms | 4.28 ms | 4.15 ms | 11.97 ms | 10.31 ms |
| fib_rec | 7.32 ms | 9.52 ms | 9.02 ms | 10.02 ms | 14.83 ms | 17.25 ms | 22.34 ms | 13.83 ms | 14.01 ms |
| memory | 6.28 ms | 6.31 ms | 6.34 ms | 6.87 ms | 15.01 ms | 4.06 ms | 4.17 ms | 19.17 ms | 9.95 ms |
| memory_tree | 7.36 ms | 6.18 ms | 6.28 ms | 6.85 ms | 17.54 ms | 4.46 ms | 4.74 ms | 18.45 ms | 10.13 ms |
| dispatch | 7.55 ms | 6.11 ms | 6.11 ms | 7.23 ms | 13.52 ms | 3.59 ms | 3.87 ms | 14.81 ms | 10.00 ms |
| many_funcs | 7.49 ms | 6.69 ms | 6.41 ms | 7.45 ms | 13.63 ms | 4.06 ms | 4.04 ms | 102.40 ms | 10.41 ms |
| linked_list | 6.84 ms | 6.20 ms | 6.29 ms | 6.69 ms | 13.75 ms | 4.28 ms | 4.70 ms | 15.00 ms | 10.22 ms |
| nbody | 7.87 ms | 6.85 ms | 6.81 ms | 7.52 ms | 15.26 ms | 6.80 ms | 9.03 ms | 27.05 ms | 10.44 ms |
| fannkuch | 7.21 ms | 9.15 ms | 8.57 ms | 9.96 ms | 16.70 ms | 11.41 ms | 18.42 ms | 38.69 ms | 11.92 ms |
| matmul | 6.42 ms | 7.12 ms | 6.87 ms | 6.60 ms | 15.35 ms | 9.29 ms | 11.95 ms | 50.49 ms | 10.11 ms |
| sha256 | 7.50 ms | 6.64 ms | 6.65 ms | 6.55 ms | 13.91 ms | 4.80 ms | 5.25 ms | 34.94 ms | 10.15 ms |
| raytrace | 7.69 ms | 8.08 ms | 7.33 ms | 7.86 ms | 16.88 ms | 6.74 ms | 8.15 ms | 38.47 ms | 11.14 ms |
| json-as | — | — | — | — | — | — | — | — | — |
| blake-as | 6.91 ms | 7.17 ms | 7.00 ms | 8.01 ms | 19.47 ms | 16.69 ms | 23.59 ms | 71.12 ms | 10.29 ms |
| utf-as | 7.32 ms | 7.25 ms | 7.11 ms | 6.82 ms | 16.50 ms | 6.94 ms | 12.56 ms | 44.97 ms | 10.66 ms |
| json-as-simd | — | — | — | — | — | — | — | — | — |
| blake-as-simd | 7.01 ms | 9.58 ms | 9.77 ms | 8.37 ms | 17.88 ms | — | — | 207.72 ms | 11.77 ms |
| utf-as-simd | — | — | — | — | — | — | — | — | — |
| polybench-2mm | 6.28 ms | 7.33 ms | 7.27 ms | 6.65 ms | 17.23 ms | 8.39 ms | 13.00 ms | 83.75 ms | 10.16 ms |
| polybench-3mm | 7.21 ms | 8.44 ms | 7.71 ms | 6.82 ms | 18.31 ms | 11.14 ms | 16.33 ms | 101.97 ms | 10.98 ms |
| polybench-adi | 8.88 ms | 10.84 ms | 10.29 ms | 8.36 ms | 19.85 ms | 21.03 ms | 26.15 ms | 69.59 ms | 13.01 ms |
| polybench-atax | 6.40 ms | 7.03 ms | 6.84 ms | 6.60 ms | 14.61 ms | 5.42 ms | 6.40 ms | 41.16 ms | 9.94 ms |
| polybench-bicg | 7.59 ms | 7.06 ms | 6.92 ms | 7.49 ms | 15.07 ms | 5.15 ms | 5.16 ms | 39.53 ms | 9.64 ms |
| polybench-cholesky | 8.25 ms | 10.80 ms | 10.08 ms | 7.63 ms | 20.56 ms | 35.95 ms | 43.82 ms | 84.18 ms | 11.07 ms |
| polybench-correlation | 6.75 ms | 7.59 ms | 7.34 ms | 7.84 ms | 17.51 ms | 9.78 ms | 11.62 ms | 63.19 ms | 10.92 ms |
| polybench-covariance | 6.05 ms | 7.44 ms | 7.23 ms | 6.92 ms | 16.97 ms | 8.67 ms | 11.78 ms | 54.75 ms | 10.51 ms |
| polybench-deriche | 7.61 ms | 8.56 ms | 7.75 ms | 7.29 ms | 17.55 ms | 10.81 ms | 14.51 ms | 85.11 ms | 11.16 ms |
| polybench-doitgen | 7.32 ms | 7.94 ms | 7.33 ms | 7.87 ms | 16.47 ms | 9.30 ms | 14.03 ms | 57.95 ms | 11.60 ms |
| polybench-durbin | 6.39 ms | 7.78 ms | 6.67 ms | 6.46 ms | 16.67 ms | 4.64 ms | 5.09 ms | 44.05 ms | 9.20 ms |
| polybench-fdtd-2d | 7.34 ms | 9.71 ms | 8.31 ms | 8.01 ms | 18.44 ms | 21.37 ms | 28.43 ms | 67.26 ms | 10.55 ms |
| polybench-floyd-warshall | 14.00 ms | 18.76 ms | 16.84 ms | 9.90 ms | 23.83 ms | 117.22 ms | 146.98 ms | 42.36 ms | 13.70 ms |
| polybench-gemm | 6.49 ms | 7.58 ms | 7.37 ms | 6.75 ms | 16.67 ms | 9.92 ms | 13.02 ms | 64.36 ms | 10.24 ms |
| polybench-gemver | 6.86 ms | 7.15 ms | 7.19 ms | 8.13 ms | 14.71 ms | 5.88 ms | 5.51 ms | 64.30 ms | 10.36 ms |
| polybench-gesummv | 6.40 ms | 7.01 ms | 6.85 ms | 7.50 ms | 14.40 ms | 4.99 ms | 5.42 ms | 41.73 ms | 10.10 ms |
| polybench-gramschmidt | 7.40 ms | 7.76 ms | 7.27 ms | 6.96 ms | 16.83 ms | 11.56 ms | 16.23 ms | 75.55 ms | 12.04 ms |
| polybench-heat-3d | 7.42 ms | 10.31 ms | 9.67 ms | 9.89 ms | 18.72 ms | 27.38 ms | 45.96 ms | 69.23 ms | 11.83 ms |
| polybench-jacobi-1d | 7.52 ms | 7.43 ms | 6.87 ms | 6.50 ms | 14.43 ms | 4.50 ms | 4.91 ms | 34.14 ms | 10.64 ms |
| polybench-jacobi-2d | 7.12 ms | 10.20 ms | 9.00 ms | 7.27 ms | 17.72 ms | 28.31 ms | 34.66 ms | 43.90 ms | 10.77 ms |
| polybench-lu | 8.52 ms | 12.32 ms | 11.14 ms | 10.50 ms | 20.99 ms | 40.92 ms | 50.48 ms | 89.51 ms | 12.83 ms |
| polybench-ludcmp | 8.60 ms | 11.48 ms | 10.09 ms | 9.37 ms | 19.20 ms | 39.49 ms | 44.27 ms | 119.75 ms | 12.25 ms |
| polybench-mvt | 7.62 ms | 7.03 ms | 6.95 ms | 6.49 ms | 14.74 ms | 5.05 ms | 5.77 ms | 54.56 ms | 10.47 ms |
| polybench-nussinov | 7.33 ms | 9.78 ms | 8.27 ms | 9.07 ms | 17.80 ms | 24.74 ms | 27.59 ms | 39.57 ms | 10.47 ms |
| polybench-seidel-2d | 11.39 ms | 11.85 ms | 11.81 ms | 10.28 ms | 22.15 ms | 23.09 ms | 35.59 ms | 36.29 ms | 14.99 ms |
| polybench-symm | 6.84 ms | 7.50 ms | 7.45 ms | 6.61 ms | 16.99 ms | 9.67 ms | 12.81 ms | 64.66 ms | 9.76 ms |
| polybench-syr2k | 7.53 ms | 8.05 ms | 7.55 ms | 6.75 ms | 16.50 ms | 10.90 ms | 14.54 ms | 63.20 ms | 10.61 ms |
| polybench-syrk | 5.93 ms | 7.16 ms | 6.97 ms | 6.68 ms | 16.71 ms | 8.29 ms | 12.26 ms | 60.85 ms | 10.50 ms |
| polybench-trisolv | 7.39 ms | 6.92 ms | 6.77 ms | 6.44 ms | 17.25 ms | 4.43 ms | 4.79 ms | 41.46 ms | 10.03 ms |
| polybench-trmm | 6.99 ms | 7.32 ms | 7.10 ms | 7.20 ms | 17.35 ms | 7.36 ms | 9.26 ms | 57.31 ms | 10.32 ms |

### Unsupported or failed cells

29 cells were unsupported or failed. Full reasons are retained in the raw JSON capture.
