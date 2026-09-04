# Railshot corpus deltas versus main — September 4, 2026

This is the review snapshot for PR #560: current `main` `01c7971d` versus `d3265e41`. The final tip-only changes after the measured compiler commit are tests and documentation; they do not change production compiler or runtime code.

Each value is the median of five samples with a 200 ms Go benchmark target using prebuilt test binaries. Compile is the backend `BenchmarkCompile` stage over an already decoded and validated module. Heap is allocated bytes per compile (`B/op`), not peak live RSS. Native bytes are the complete generated module image. Execution covers every export declared by the default corpus manifest; unavailable execution stages are absent rather than estimated.

- ARM64: Apple M4 Max, Darwin, native arm64.
- AMD64: AMD Ryzen 7 7800X3D, Linux, native amd64 via `hub@hub`.
- Negative deltas are improvements. The complete baseline sweep ran before the complete tip sweep, so small latency movements should be treated as screening results; focused decisions in the implementation plan use alternating-process measurements.

## Aggregate

| Architecture | Compile latency geomean | Allocated heap, sum of module medians | Allocations, sum | Native bytes, sum | Execution latency geomean |
|---|---:|---:|---:|---:|---:|
| ARM64 | -9.05% | 61.42 MiB -> 37.12 MiB (-39.57%) | 309,567 -> 37,716 (-271,851) | 77,428,800 -> 77,426,708 (-2,092) | +1.54% |
| AMD64 | -16.88% | 60.20 MiB -> 41.65 MiB (-30.82%) | 400,102 -> 40,399 (-359,703) | 69,675,879 -> 69,683,590 (+7,711) | +0.84% |

The heap and allocation aggregate is the sum of per-module medians, useful as a fixed-corpus score; it is not a claim that all modules are live simultaneously.

## ARM64: compilation, allocated heap, and generated machine code

| Corpus | Compile main | Compile tip | Delta | Heap main | Heap tip | Delta | Allocs main | Allocs tip | Native main | Native tip | Delta |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `tiny` | 4.64 us | 3.67 us | -20.9% | 13.62 KiB | 10.86 KiB | -20.2% | 28 | 18 | 128 | 128 | +0 |
| `fib_iter` | 6.54 us | 5.19 us | -20.6% | 18.12 KiB | 14.01 KiB | -22.7% | 36 | 22 | 128 | 128 | +0 |
| `fib_rec` | 6.21 us | 5.36 us | -13.7% | 15.62 KiB | 14.11 KiB | -9.7% | 42 | 31 | 220 | 220 | +0 |
| `arith` | 8.02 us | 5.88 us | -26.7% | 46.45 KiB | 27.98 KiB | -39.8% | 35 | 22 | 152 | 152 | +0 |
| `float` | 7.49 us | 5.80 us | -22.5% | 46.02 KiB | 28.05 KiB | -39.0% | 36 | 23 | 152 | 152 | +0 |
| `memory` | 9.63 us | 7.91 us | -17.9% | 19.33 KiB | 14.33 KiB | -25.9% | 45 | 25 | 404 | 404 | +0 |
| `memory_tree` | 13.85 us | 11.38 us | -17.8% | 47.01 KiB | 28.80 KiB | -38.7% | 55 | 38 | 628 | 628 | +0 |
| `globals` | 7.67 us | 6.09 us | -20.6% | 18.28 KiB | 14.59 KiB | -20.2% | 45 | 29 | 220 | 220 | +0 |
| `dispatch` | 13.56 us | 12.70 us | -6.3% | 46.21 KiB | 26.91 KiB | -41.8% | 56 | 42 | 972 | 972 | +0 |
| `branches` | 7.01 us | 6.38 us | -9.1% | 21.58 KiB | 14.10 KiB | -34.6% | 40 | 20 | 132 | 132 | +0 |
| `many_funcs` | 217.57 us | 207.78 us | -4.5% | 166.57 KiB | 65.06 KiB | -60.9% | 42 | 30 | 9,720 | 9,720 | +0 |
| `linked_list` | 10.31 us | 9.44 us | -8.4% | 24.67 KiB | 17.43 KiB | -29.4% | 48 | 27 | 364 | 364 | +0 |
| `mandelbrot` | 14.64 us | 14.01 us | -4.3% | 50.73 KiB | 28.64 KiB | -43.5% | 57 | 28 | 412 | 412 | +0 |
| `sieve` | 12.68 us | 11.98 us | -5.5% | 32.09 KiB | 20.38 KiB | -36.5% | 51 | 29 | 400 | 400 | +0 |
| `nbody` | 65.98 us | 63.63 us | -3.6% | 111.92 KiB | 63.12 KiB | -43.6% | 91 | 49 | 4,020 | 4,020 | +0 |
| `spectralnorm` | 51.68 us | 49.91 us | -3.4% | 106.97 KiB | 61.55 KiB | -42.5% | 93 | 48 | 2,764 | 2,764 | +0 |
| `fannkuch` | 84.41 us | 74.11 us | -12.2% | 123.52 KiB | 64.40 KiB | -47.9% | 109 | 55 | 5,144 | 5,144 | +0 |
| `matmul` | 43.90 us | 41.27 us | -6.0% | 112.91 KiB | 61.76 KiB | -45.3% | 85 | 47 | 2,396 | 2,396 | +0 |
| `quicksort` | 43.18 us | 41.41 us | -4.1% | 60.08 KiB | 30.30 KiB | -49.6% | 103 | 62 | 2,460 | 2,460 | +0 |
| `crc32` | 27.62 us | 23.65 us | -14.4% | 48.92 KiB | 28.98 KiB | -40.8% | 60 | 39 | 1,696 | 1,696 | +0 |
| `sha256` | 62.96 us | 56.20 us | -10.7% | 111.96 KiB | 62.42 KiB | -44.2% | 85 | 46 | 3,768 | 3,768 | +0 |
| `raytrace` | 149.66 us | 153.41 us | +2.5% | 224.73 KiB | 119.89 KiB | -46.7% | 101 | 66 | 11,016 | 11,016 | +0 |
| `regexmatch` | 36.269 ms | 35.453 ms | -2.3% | 3.26 MiB | 1.34 MiB | -58.9% | 12,978 | 3,072 | 3,089,752 | 3,089,768 | +16 |
| `wasm3` | 9.079 ms | 9.275 ms | +2.2% | 1.08 MiB | 390.55 KiB | -64.6% | 5,596 | 1,024 | 930,008 | 930,008 | +0 |
| `json-as` | 767.59 us | 794.91 us | +3.6% | 290.41 KiB | 131.80 KiB | -54.6% | 968 | 135 | 64,616 | 64,744 | +128 |
| `blake-as` | 226.10 us | 209.74 us | -7.2% | 235.26 KiB | 120.47 KiB | -48.8% | 129 | 78 | 11,056 | 11,056 | +0 |
| `utf-as` | 110.18 us | 95.60 us | -13.2% | 225.31 KiB | 119.35 KiB | -47.0% | 135 | 63 | 4,332 | 4,380 | +48 |
| `xjb-mulhi` | 13.96 us | 13.46 us | -3.6% | 46.66 KiB | 28.15 KiB | -39.7% | 36 | 24 | 408 | 480 | +72 |
| `swar-pack-parse` | 15.21 us | 13.00 us | -14.5% | 46.65 KiB | 28.13 KiB | -39.7% | 35 | 23 | 456 | 580 | +124 |
| `json-as-simd` | 878.92 us | 864.96 us | -1.6% | 311.64 KiB | 135.92 KiB | -56.4% | 1,093 | 141 | 73,480 | 73,512 | +32 |
| `blake-as-simd` | 660.97 us | 614.30 us | -7.1% | 465.86 KiB | 238.08 KiB | -48.9% | 200 | 88 | 31,304 | 31,304 | +0 |
| `utf-as-simd` | 348.84 us | 301.65 us | -13.5% | 239.77 KiB | 120.78 KiB | -49.6% | 257 | 80 | 20,588 | 20,668 | +80 |
| `lua` | 13.835 ms | 13.363 ms | -3.4% | 1.74 MiB | 701.63 KiB | -60.6% | 7,807 | 748 | 945,388 | 945,388 | +0 |
| `sqlite3` | 53.414 ms | 52.824 ms | -1.1% | 5.23 MiB | 1.73 MiB | -66.9% | 29,213 | 2,768 | 3,735,616 | 3,735,616 | +0 |
| `ruby` | 591.092 ms | 587.682 ms | -0.6% | 24.46 MiB | 13.18 MiB | -46.1% | 189,152 | 15,204 | 37,575,024 | 37,572,432 | -2,592 |
| `esbuild` | 369.558 ms | 367.861 ms | -0.5% | 22.41 MiB | 18.10 MiB | -19.2% | 60,625 | 13,472 | 30,899,476 | 30,899,476 | +0 |

## ARM64: execution latency

| Corpus export | Main | Tip | Delta |
|---|---:|---:|---:|
| `tiny.add` | 21.0 ns | 20.9 ns | -0.1% |
| `fib_iter.fib` | 30.5 ns | 29.3 ns | -3.8% |
| `fib_rec.fib` | 1.101 ms | 1.074 ms | -2.4% |
| `arith.run` | 1.49 us | 1.50 us | +0.5% |
| `float.run` | 2.51 us | 2.54 us | +1.2% |
| `memory.sum` | 309.0 ns | 314.7 ns | +1.8% |
| `memory_tree.run` | 8.71 us | 8.69 us | -0.2% |
| `globals.accumulate` | 598.5 ns | 634.3 ns | +6.0% |
| `dispatch.apply` | 24.2 ns | 24.3 ns | +0.6% |
| `branches.classify` | 21.0 ns | 20.9 ns | -0.7% |
| `many_funcs.run` | 21.2 ns | 22.1 ns | +4.4% |
| `linked_list.sum` | 5.66 us | 5.76 us | +1.7% |
| `mandelbrot.render` | 228.21 us | 227.43 us | -0.3% |
| `sieve.count` | 100.95 us | 99.36 us | -1.6% |
| `nbody.step` | 144.96 us | 145.39 us | +0.3% |
| `spectralnorm.run` | 383.66 us | 388.21 us | +1.2% |
| `fannkuch.run` | 1.262 ms | 1.250 ms | -0.9% |
| `matmul.run` | 148.36 us | 150.68 us | +1.6% |
| `quicksort.sortN` | 69.80 us | 70.27 us | +0.7% |
| `crc32.hashN` | 24.16 us | 25.16 us | +4.1% |
| `sha256.hashN` | 28.36 us | 28.53 us | +0.6% |
| `raytrace.render` | 260.50 us | 273.38 us | +4.9% |
| `json-as.serializeN` | 18.90 us | 19.15 us | +1.3% |
| `json-as.deserializeN` | 39.37 us | 38.97 us | -1.0% |
| `blake-as.hashN` | 398.59 us | 394.94 us | -0.9% |
| `utf-as.convertN` | 103.87 us | 121.91 us | +17.4% |
| `xjb-mulhi.mulhi` | 22.1 ns | 21.9 ns | -1.0% |
| `xjb-mulhi.runN` | 1.24 us | 1.23 us | -0.3% |
| `swar-pack-parse.pack` | 21.6 ns | 21.2 ns | -1.8% |
| `swar-pack-parse.parse4` | 21.1 ns | 21.1 ns | -0.1% |
| `swar-pack-parse.runN` | 943.1 ns | 1.23 us | +30.1% |
| `json-as-simd.serializeN` | 23.93 us | 23.29 us | -2.7% |
| `json-as-simd.deserializeN` | 51.77 us | 50.91 us | -1.7% |
| `blake-as-simd.hashN` | 424.59 us | 428.32 us | +0.9% |
| `utf-as-simd.convertN` | 56.42 us | 57.01 us | +1.1% |
| `utf-as-simd.validateN` | 142.12 us | 142.47 us | +0.2% |

## AMD64: compilation, allocated heap, and generated machine code

| Corpus | Compile main | Compile tip | Delta | Heap main | Heap tip | Delta | Allocs main | Allocs tip | Native main | Native tip | Delta |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `tiny` | 6.65 us | 6.16 us | -7.5% | 9.83 KiB | 7.95 KiB | -19.1% | 24 | 20 | 96 | 96 | +0 |
| `fib_iter` | 7.83 us | 7.19 us | -8.2% | 12.79 KiB | 11.00 KiB | -14.0% | 32 | 24 | 99 | 99 | +0 |
| `fib_rec` | 7.94 us | 7.68 us | -3.4% | 11.52 KiB | 10.67 KiB | -7.3% | 39 | 33 | 163 | 163 | +0 |
| `arith` | 9.57 us | 8.09 us | -15.5% | 40.65 KiB | 26.32 KiB | -35.3% | 30 | 23 | 107 | 107 | +0 |
| `float` | 10.06 us | 8.51 us | -15.4% | 40.78 KiB | 26.45 KiB | -35.1% | 35 | 28 | 150 | 150 | +0 |
| `memory` | 12.19 us | 10.77 us | -11.7% | 13.97 KiB | 11.09 KiB | -20.6% | 42 | 28 | 292 | 292 | +0 |
| `memory_tree` | 18.10 us | 15.35 us | -15.2% | 41.75 KiB | 27.29 KiB | -34.6% | 58 | 46 | 566 | 566 | +0 |
| `globals` | 9.40 us | 8.52 us | -9.3% | 12.88 KiB | 10.47 KiB | -18.6% | 41 | 31 | 148 | 148 | +0 |
| `dispatch` | 14.64 us | 13.28 us | -9.3% | 41.80 KiB | 25.79 KiB | -38.3% | 46 | 40 | 475 | 475 | +0 |
| `branches` | 7.82 us | 8.15 us | +4.2% | 13.81 KiB | 10.66 KiB | -22.8% | 33 | 21 | 120 | 120 | +0 |
| `many_funcs` | 367.35 us | 301.10 us | -18.0% | 166.59 KiB | 68.62 KiB | -58.8% | 36 | 31 | 7,817 | 7,817 | +0 |
| `linked_list` | 13.97 us | 11.15 us | -20.2% | 19.95 KiB | 14.64 KiB | -26.6% | 53 | 34 | 274 | 274 | +0 |
| `mandelbrot` | 21.00 us | 14.05 us | -33.1% | 44.59 KiB | 27.72 KiB | -37.8% | 63 | 40 | 504 | 504 | +0 |
| `sieve` | 16.48 us | 13.66 us | -17.1% | 24.90 KiB | 16.77 KiB | -32.7% | 50 | 33 | 367 | 367 | +0 |
| `nbody` | 89.81 us | 65.30 us | -27.3% | 107.63 KiB | 62.52 KiB | -41.9% | 103 | 65 | 4,174 | 4,174 | +0 |
| `spectralnorm` | 71.88 us | 51.88 us | -27.8% | 101.85 KiB | 60.70 KiB | -40.4% | 109 | 63 | 2,499 | 2,499 | +0 |
| `fannkuch` | 111.74 us | 82.86 us | -25.8% | 109.13 KiB | 63.77 KiB | -41.6% | 119 | 68 | 4,707 | 4,707 | +0 |
| `matmul` | 57.54 us | 43.91 us | -23.7% | 102.89 KiB | 60.60 KiB | -41.1% | 92 | 60 | 1,932 | 1,932 | +0 |
| `quicksort` | 58.56 us | 44.38 us | -24.2% | 49.20 KiB | 28.81 KiB | -41.4% | 104 | 67 | 1,967 | 1,967 | +0 |
| `crc32` | 31.57 us | 26.21 us | -17.0% | 42.61 KiB | 27.41 KiB | -35.7% | 63 | 43 | 1,171 | 1,171 | +0 |
| `sha256` | 82.76 us | 63.09 us | -23.8% | 103.66 KiB | 60.82 KiB | -41.3% | 84 | 50 | 3,075 | 3,075 | +0 |
| `raytrace` | 203.70 us | 176.81 us | -13.2% | 222.80 KiB | 132.68 KiB | -40.4% | 122 | 89 | 13,282 | 13,282 | +0 |
| `regexmatch` | 44.199 ms | 37.796 ms | -14.5% | 3.09 MiB | 1.40 MiB | -54.8% | 12,467 | 2,769 | 2,289,010 | 2,288,626 | -384 |
| `wasm3` | 11.756 ms | 10.592 ms | -9.9% | 1.01 MiB | 435.36 KiB | -57.8% | 6,147 | 971 | 858,745 | 858,745 | +0 |
| `json-as` | 1.186 ms | 978.75 us | -17.4% | 272.04 KiB | 140.43 KiB | -48.4% | 1,318 | 162 | 57,470 | 57,470 | +0 |
| `blake-as` | 306.43 us | 253.23 us | -17.4% | 220.65 KiB | 127.19 KiB | -42.4% | 140 | 95 | 10,573 | 10,573 | +0 |
| `utf-as` | 159.27 us | 115.76 us | -27.3% | 218.56 KiB | 126.58 KiB | -42.1% | 139 | 68 | 4,673 | 4,769 | +96 |
| `xjb-mulhi` | 16.99 us | 15.36 us | -9.6% | 41.33 KiB | 25.04 KiB | -39.4% | 31 | 23 | 323 | 398 | +75 |
| `swar-pack-parse` | 18.40 us | 15.98 us | -13.2% | 41.27 KiB | 25.03 KiB | -39.4% | 29 | 22 | 401 | 469 | +68 |
| `json-as-simd` | 1.313 ms | 1.085 ms | -17.4% | 281.83 KiB | 144.09 KiB | -48.9% | 1,476 | 178 | 65,731 | 65,731 | +0 |
| `blake-as-simd` | 967.40 us | 718.84 us | -25.7% | 453.09 KiB | 259.67 KiB | -42.7% | 226 | 116 | 39,938 | 39,938 | +0 |
| `utf-as-simd` | 492.60 us | 373.01 us | -24.3% | 230.41 KiB | 132.79 KiB | -42.4% | 391 | 110 | 21,535 | 21,583 | +48 |
| `lua` | 19.121 ms | 16.312 ms | -14.7% | 1.74 MiB | 767.05 KiB | -56.9% | 10,298 | 940 | 889,172 | 889,172 | +0 |
| `sqlite3` | 72.459 ms | 62.850 ms | -13.3% | 5.20 MiB | 2.13 MiB | -59.1% | 35,380 | 3,363 | 3,618,073 | 3,634,105 | +16,032 |
| `ruby` | 752.281 ms | 688.910 ms | -8.4% | 23.65 MiB | 15.49 MiB | -34.5% | 260,679 | 11,998 | 35,163,610 | 35,155,402 | -8,208 |
| `esbuild` | 510.927 ms | 410.692 ms | -19.6% | 22.49 MiB | 19.73 MiB | -12.3% | 70,003 | 18,647 | 26,612,640 | 26,612,624 | -16 |

## AMD64: execution latency

| Corpus export | Main | Tip | Delta |
|---|---:|---:|---:|
| `tiny.add` | 7.7 ns | 7.5 ns | -2.6% |
| `fib_iter.fib` | 19.3 ns | 19.2 ns | -0.5% |
| `fib_rec.fib` | 1.208 ms | 1.212 ms | +0.3% |
| `arith.run` | 1.27 us | 1.27 us | -0.2% |
| `float.run` | 3.78 us | 3.78 us | -0.1% |
| `memory.sum` | 233.3 ns | 233.4 ns | +0.0% |
| `memory_tree.run` | 8.88 us | 8.90 us | +0.2% |
| `globals.accumulate` | 849.4 ns | 850.7 ns | +0.2% |
| `dispatch.apply` | 20.6 ns | 19.9 ns | -3.2% |
| `branches.classify` | 7.7 ns | 7.5 ns | -2.5% |
| `many_funcs.run` | 7.8 ns | 8.2 ns | +5.1% |
| `linked_list.sum` | 7.78 us | 7.78 us | -0.0% |
| `mandelbrot.render` | 235.59 us | 234.98 us | -0.3% |
| `sieve.count` | 77.99 us | 78.06 us | +0.1% |
| `nbody.step` | 277.15 us | 264.37 us | -4.6% |
| `spectralnorm.run` | 620.28 us | 620.39 us | +0.0% |
| `fannkuch.run` | 1.060 ms | 1.059 ms | -0.0% |
| `matmul.run` | 138.60 us | 138.50 us | -0.1% |
| `quicksort.sortN` | 55.50 us | 55.84 us | +0.6% |
| `crc32.hashN` | 16.14 us | 16.12 us | -0.1% |
| `sha256.hashN` | 38.79 us | 38.68 us | -0.3% |
| `raytrace.render` | 361.96 us | 359.68 us | -0.6% |
| `json-as.serializeN` | 22.43 us | 22.46 us | +0.1% |
| `json-as.deserializeN` | 40.06 us | 40.12 us | +0.1% |
| `blake-as.hashN` | 652.43 us | 652.79 us | +0.1% |
| `utf-as.convertN` | 152.76 us | 174.51 us | +14.2% |
| `xjb-mulhi.mulhi` | 7.7 ns | 8.2 ns | +7.3% |
| `xjb-mulhi.runN` | 1.85 us | 1.85 us | +0.1% |
| `swar-pack-parse.pack` | 7.7 ns | 7.7 ns | -0.1% |
| `swar-pack-parse.parse4` | 7.9 ns | 7.7 ns | -2.5% |
| `swar-pack-parse.runN` | 1.35 us | 1.70 us | +26.1% |
| `json-as-simd.serializeN` | 27.86 us | 27.51 us | -1.3% |
| `json-as-simd.deserializeN` | 51.35 us | 51.35 us | -0.0% |
| `blake-as-simd.hashN` | 528.54 us | 528.81 us | +0.1% |
| `utf-as-simd.convertN` | 52.03 us | 51.88 us | -0.3% |
| `utf-as-simd.validateN` | 146.89 us | 145.91 us | -0.7% |
