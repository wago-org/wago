# Wago main versus w2c2: Apple M4 Max arm64 steady execution times

Read from the local wasm.fyi database on 2026-10-09T04:48:08+00:00.

Current optimization priority: [54 qualified focused workloads on this device](arm64-performance.md). That report combines retained-image Wago captures against the historical w2c2 column below; capture protocols and ranges are included, and the complete recorded ranking remains available here.

The [retained ARM64 changes](arm64-performance.md) improve point-in-polygon, Boolean ANF, KMeans, polygon clipping, register VM, polyline simplification and particle physics, with exact validation and compile costs recorded separately.

Source: `../../Web/wasm.fyi`, served at `http://localhost:8080/api/benchmarks?platform=<id>&phase=steady`. Measurement semantics are documented in the sibling checkout's `../../Web/wasm.fyi/API.md`. All paginated results were read. No benchmarks were rerun for this original database capture.

Each row is an individual corpus workload. Times are **microseconds per reported operation** (`latencyNs / 1,000`), using the database’s steady median; warmups are excluded. Operations differ between workloads; the host callback loop is normalized per boundary call.

**Worst-to-best** means descending `Wago / w2c2` on the same platform, artifact and contract. The percentage is `(Wago / w2c2 − 1) × 100`; positive means Wago is slower, negative means Wago is faster. “Regression” here means a performance gap against w2c2, not a change from an older Wago revision. Only this device’s Apple M4 Max (`darwin/arm64`) results are included.

Wago uses `railshot`, source `main@2286d676facd` (`2286d676facdfa1cabe2d1c61072e505438ca7f2`, source date 2026-10-09). w2c2 uses `w2c2-gcc` / `c-aot`, source `main@700f5565` (`700f556539ea7c7995e6645b9b80e3edf9c9b271`, source date 2026-10-04). These measurements describe the recorded builds, not the current working tree.

Only the completed Wago main source snapshot is included; 78 retained beta.12 records are excluded without backfilling missing main results. Where multiple records exist for the same engine/workload within that snapshot, the latest capture is used. Missing or unsuccessful timings are listed after the ranked rows and never treated as zero. Matching artifact and contract hashes are required for ranking. Captures are not simultaneous; their ranges and configuration IDs appear below.

## Worst gaps

- `applications/search-aho-corasick`: **7.71×** slower (+670.6%), Wago 287.375000 µs; w2c2 37.292000 µs.
- `applications/graphics-reed-solomon`: **2.47×** slower (+147.4%), Wago 217.167000 µs; w2c2 87.792000 µs.
- `applications/stats-linear-regression`: **2.36×** slower (+136.4%), Wago 4.334000 µs; w2c2 1.833000 µs.
- `applications/ml-inference`: **2.28×** slower (+127.9%), Wago 13.958000 µs; w2c2 6.125000 µs.
- `applications/vision-dilation`: **2.25×** slower (+125.0%), Wago 66.084000 µs; w2c2 29.375000 µs.

## Complete ranking

Platform ID: `c651ca72c500632bbabb7fc8e1f80bda57f5bb8d57ff522ad62880bb548d5c77`. Database revision: `181ab7110cd3d84d5e8e59e2b32b0d477e11d3c22be7ba961badbf231348b2d9`.

Kernel: `Darwin 25.6.0 Darwin Kernel Version 25.6.0: Fri Jul 31 19:17:26 PDT 2026; root:xnu-12377.161.14~5/RELEASE_ARM64_T6041`. RAM: 64.00 GiB (zero means unspecified).

- wago: captures `2026-10-09T04:39:59.401Z` through `2026-10-09T04:40:27.677Z`; configuration `2286d676facdfa1cabe2d1c61072e505438ca7f2/source-eba3fb46c2bcc78a8552e04d7458bb921153cf8ec61248109ae1834ad5470a12`.
- w2c2-gcc: captures `2026-10-09T03:35:34.208Z` through `2026-10-09T03:55:32.743Z`; configuration `w2c2-gcc:cee833bf2dc5ccc143cc97ddc637f5fcd7813ac0d8ecb6d1d1cd55519ab6d951`.

146 comparable workloads; 98 slower in Wago; 47 faster; 2 unranked.

| Rank | Corpus workload | Wago steady (µs/op) | w2c2 steady (µs/op) | Wago / w2c2 | Gap |
| ---: | --- | ---: | ---: | ---: | ---: |
| 1 | `applications/search-aho-corasick` | 287.375000 | 37.292000 | 7.706× | +670.6% |
| 2 | `applications/graphics-reed-solomon` | 217.167000 | 87.792000 | 2.474× | +147.4% |
| 3 | `applications/stats-linear-regression` | 4.334000 | 1.833000 | 2.364× | +136.4% |
| 4 | `applications/ml-inference` | 13.958000 | 6.125000 | 2.279× | +127.9% |
| 5 | `applications/vision-dilation` | 66.084000 | 29.375000 | 2.250× | +125.0% |
| 6 | `applications/hardware-prime-implicants` | 83.125000 | 38.584000 | 2.154× | +115.4% |
| 7 | `mechanisms/wasm-host-wasm-loop` | 0.002792 | 0.001296 | 2.154× | +115.4% |
| 8 | `applications/serialization-protobuf` | 29.250000 | 13.833000 | 2.115× | +111.5% |
| 9 | `applications/audio-fir` | 48.166000 | 23.250000 | 2.072× | +107.2% |
| 10 | `applications/compiler-register-allocation` | 20.542000 | 9.916000 | 2.072× | +107.2% |
| 11 | `applications/video-dct` | 56.750000 | 27.417000 | 2.070× | +107.0% |
| 12 | `applications/ml-knn` | 46.750000 | 22.917000 | 2.040× | +104.0% |
| 13 | `applications/geo-point-in-polygon` | 24.333000 | 12.208000 | 1.993× | +99.3% |
| 14 | `applications/files-glob-match` | 154.542000 | 78.542000 | 1.968× | +96.8% |
| 15 | `applications/vision-components` | 43.917000 | 23.084000 | 1.902× | +90.2% |
| 16 | `applications/grid-pathfinding` | 55.208000 | 29.250000 | 1.887× | +88.7% |
| 17 | `applications/audio-adpcm` | 44.208000 | 23.458000 | 1.885× | +88.5% |
| 18 | `applications/language-register-vm` | 12.917000 | 6.875000 | 1.879× | +87.9% |
| 19 | `applications/ml-kmeans` | 29.292000 | 16.208000 | 1.807× | +80.7% |
| 20 | `applications/video-optical-flow` | 21.750000 | 12.041000 | 1.806× | +80.6% |
| 21 | `wago/libtommath/modular-arithmetic` | 11.411000 | 6.500000 | 1.756× | +75.6% |
| 22 | `applications/stats-bootstrap` | 31.500000 | 18.167000 | 1.734× | +73.4% |
| 23 | `applications/files-path-normalize` | 19.000000 | 10.958000 | 1.734× | +73.4% |
| 24 | `applications/hardware-boolean-anf` | 84.791000 | 49.000000 | 1.730× | +73.0% |
| 25 | `applications/ml-convolution` | 36.417000 | 21.084000 | 1.727× | +72.7% |
| 26 | `wago/fastfloat/decimal-parse` | 0.791000 | 0.458000 | 1.727× | +72.7% |
| 27 | `applications/columnar-query` | 24.791000 | 14.541000 | 1.705× | +70.5% |
| 28 | `wago/pcre2/compile-match` | 8.292000 | 4.917000 | 1.686× | +68.6% |
| 29 | `applications/db-bitmap-index` | 16.708000 | 9.917000 | 1.685× | +68.5% |
| 30 | `wago/zstd/decompress` | 6.084000 | 3.667000 | 1.659× | +65.9% |
| 31 | `applications/stats-gini` | 5.125000 | 3.167000 | 1.618× | +61.8% |
| 32 | `applications/image-composite` | 81.333000 | 50.708000 | 1.604× | +60.4% |
| 33 | `applications/search-rabin-karp` | 246.209000 | 155.209000 | 1.586× | +58.6% |
| 34 | `wago/xxhash/xxh64-boundaries` | 93.500000 | 59.125000 | 1.581× | +58.1% |
| 35 | `applications/db-btree` | 12.625000 | 8.041000 | 1.570× | +57.0% |
| 36 | `applications/vision-otsu` | 10.833000 | 6.916000 | 1.566× | +56.6% |
| 37 | `wago/nanosvg/parse-structure` | 2.375000 | 1.542000 | 1.540× | +54.0% |
| 38 | `applications/files-merkle-tree` | 8.584000 | 5.583000 | 1.538× | +53.8% |
| 39 | `applications/numeric-euclidean-gcd` | 171.083000 | 111.542000 | 1.534× | +53.4% |
| 40 | `applications/image-error-diffusion` | 54.375000 | 35.667000 | 1.525× | +52.5% |
| 41 | `applications/document-page-packing` | 46.542000 | 30.833000 | 1.509× | +50.9% |
| 42 | `wago/coremark/2k-performance` | 27936.542000 | 18705.583000 | 1.493× | +49.3% |
| 43 | `applications/compiler-dead-code` | 4.292000 | 2.875000 | 1.493× | +49.3% |
| 44 | `applications/crypto-aes128` | 38.125000 | 25.834000 | 1.476× | +47.6% |
| 45 | `applications/geo-polyline-simplify` | 5.459000 | 3.708000 | 1.472× | +47.2% |
| 46 | `applications/geo-geohash` | 44.084000 | 30.042000 | 1.467× | +46.7% |
| 47 | `applications/game-collision-impulse` | 22.625000 | 15.500000 | 1.460× | +46.0% |
| 48 | `applications/document-optimal-wrap` | 3.083000 | 2.125000 | 1.451× | +45.1% |
| 49 | `applications/ml-max-pooling` | 26.458000 | 18.375000 | 1.440× | +44.0% |
| 50 | `applications/mesh-skinning` | 26.500000 | 18.500000 | 1.432× | +43.2% |
| 51 | `applications/audio-autocorrelation` | 49.000000 | 34.375000 | 1.425× | +42.5% |
| 52 | `applications/language-forth` | 14.458000 | 10.208000 | 1.416× | +41.6% |
| 53 | `applications/frustum-culling` | 9.333000 | 6.625000 | 1.409× | +40.9% |
| 54 | `applications/document-bidi-reorder` | 18.916000 | 13.458000 | 1.406× | +40.6% |
| 55 | `applications/db-sort-merge-join` | 63.333000 | 45.084000 | 1.405× | +40.5% |
| 56 | `applications/vision-distance-transform` | 68.417000 | 48.750000 | 1.403× | +40.3% |
| 57 | `applications/ml-decision-tree` | 141.583000 | 102.125000 | 1.386× | +38.6% |
| 58 | `applications/text-substitution` | 66.042000 | 47.792000 | 1.382× | +38.2% |
| 59 | `applications/files-content-chunking` | 32.666000 | 23.667000 | 1.380× | +38.0% |
| 60 | `applications/audio-biquad` | 21.042000 | 15.417000 | 1.365× | +36.5% |
| 61 | `applications/db-hash-join` | 3.750000 | 2.750000 | 1.364× | +36.4% |
| 62 | `applications/video-motion` | 505.500000 | 373.584000 | 1.353× | +35.3% |
| 63 | `applications/bio-lcs` | 71.167000 | 52.959000 | 1.344× | +34.4% |
| 64 | `applications/checksum-crc32` | 117.834000 | 87.750000 | 1.343× | +34.3% |
| 65 | `applications/video-yuv420` | 50.416000 | 37.625000 | 1.340× | +34.0% |
| 66 | `applications/bio-kmer-count` | 36.500000 | 28.291000 | 1.290× | +29.0% |
| 67 | `applications/game-flocking` | 81.333000 | 63.458000 | 1.282× | +28.2% |
| 68 | `applications/graphics-png-paeth` | 40.166000 | 31.459000 | 1.277× | +27.7% |
| 69 | `applications/vision-fast-corners` | 1081.000000 | 849.000000 | 1.273× | +27.3% |
| 70 | `wago/monocypher/aead-blake2b` | 24.708000 | 19.458000 | 1.270× | +27.0% |
| 71 | `applications/files-path-trie` | 6.542000 | 5.250000 | 1.246× | +24.6% |
| 72 | `applications/stats-quickselect` | 21.750000 | 17.458000 | 1.246× | +24.6% |
| 73 | `applications/ray-box` | 17.791000 | 14.292000 | 1.245× | +24.5% |
| 74 | `wago/polybench-durbin/polybench_run` | 9.625000 | 7.792000 | 1.235× | +23.5% |
| 75 | `applications/compression-rle` | 18.167000 | 14.708000 | 1.235× | +23.5% |
| 76 | `applications/document-edit-distance` | 104.459000 | 85.167000 | 1.227× | +22.7% |
| 77 | `applications/compiler-constant-fold` | 2.042000 | 1.667000 | 1.225× | +22.5% |
| 78 | `applications/stencil-wave-equation` | 81.000000 | 66.833000 | 1.212× | +21.2% |
| 79 | `applications/video-deblocking` | 30.750000 | 25.542000 | 1.204× | +20.4% |
| 80 | `applications/document-kerning` | 24.667000 | 20.584000 | 1.198× | +19.8% |
| 81 | `wago/drwav/pcm-decode-seek` | 7.125000 | 5.959000 | 1.196× | +19.6% |
| 82 | `applications/graph-dijkstra` | 6.959000 | 5.917000 | 1.176× | +17.6% |
| 83 | `applications/document-layout` | 170.417000 | 145.083000 | 1.175× | +17.5% |
| 84 | `applications/game-spatial-hash` | 32.792000 | 28.834000 | 1.137× | +13.7% |
| 85 | `applications/bezier-tessellation` | 13.542000 | 11.959000 | 1.132× | +13.2% |
| 86 | `applications/stencil-lattice-boltzmann` | 171.375000 | 152.000000 | 1.127× | +12.7% |
| 87 | `applications/map-point-segment` | 31.792000 | 28.250000 | 1.125× | +12.5% |
| 88 | `applications/stats-welford` | 18.958000 | 16.959000 | 1.118× | +11.8% |
| 89 | `wago/yyjson/parse-edit-write` | 0.792000 | 0.709000 | 1.117× | +11.7% |
| 90 | `applications/serialization-msgpack` | 12.500000 | 11.334000 | 1.103× | +10.3% |
| 91 | `wago/qoi/encode` | 4.625000 | 4.250000 | 1.088× | +8.8% |
| 92 | `wago/blake3/hash` | 234.335000 | 216.206000 | 1.084× | +8.4% |
| 93 | `wago/polybench-trisolv/polybench_run` | 7.708000 | 7.125000 | 1.082× | +8.2% |
| 94 | `wago/lz4/compress` | 26.083000 | 24.291000 | 1.074× | +7.4% |
| 95 | `wago/sha256/hashN` | 23.875000 | 22.625000 | 1.055× | +5.5% |
| 96 | `applications/bio-global-alignment` | 85.000000 | 80.833000 | 1.052× | +5.2% |
| 97 | `applications/image-resize` | 26.833000 | 26.208000 | 1.024× | +2.4% |
| 98 | `applications/geo-convex-hull` | 14.625000 | 14.500000 | 1.009× | +0.9% |
| 99 | `applications/numeric-simpson` | 0.458000 | 0.458000 | 1.000× | +0.0% |
| 100 | `applications/bio-reverse-complement` | 42.709000 | 42.958000 | 0.994× | -0.6% |
| 101 | `applications/image-ycocg` | 50.250000 | 51.042000 | 0.984× | -1.6% |
| 102 | `wago/polybench-floyd-warshall/polybench_run` | 3641.625000 | 3974.334000 | 0.916× | -8.4% |
| 103 | `wago/polybench-cholesky/polybench_run` | 974.959000 | 1090.833000 | 0.894× | -10.6% |
| 104 | `wago/fib_rec/fib` | 542.709000 | 617.041000 | 0.880× | -12.0% |
| 105 | `applications/numeric-cordic` | 55.000000 | 62.667000 | 0.878× | -12.2% |
| 106 | `applications/graph-kruskal` | 40.959000 | 48.625000 | 0.842× | -15.8% |
| 107 | `applications/bio-local-alignment` | 96.417000 | 118.542000 | 0.813× | -18.7% |
| 108 | `wago/polybench-nussinov/polybench_run` | 688.084000 | 851.292000 | 0.808× | -19.2% |
| 109 | `applications/search-bk-tree` | 178.875000 | 221.916000 | 0.806× | -19.4% |
| 110 | `wago/polybench-jacobi-2d/polybench_run` | 731.041000 | 928.917000 | 0.787× | -21.3% |
| 111 | `wago/raytrace/render` | 274.791000 | 351.250000 | 0.782× | -21.8% |
| 112 | `wago/polybench-adi/polybench_run` | 1384.584000 | 1770.584000 | 0.782× | -21.8% |
| 113 | `applications/particle-physics` | 561.333000 | 730.584000 | 0.768× | -23.2% |
| 114 | `applications/graphics-bresenham` | 148.459000 | 195.209000 | 0.761× | -23.9% |
| 115 | `wago/polybench-seidel-2d/polybench_run` | 2504.083000 | 3348.875000 | 0.748× | -25.2% |
| 116 | `applications/video-temporal-denoise` | 111.833000 | 152.417000 | 0.734× | -26.6% |
| 117 | `applications/image-blur` | 141.709000 | 212.125000 | 0.668× | -33.2% |
| 118 | `applications/search-horspool` | 83.166000 | 126.875000 | 0.655× | -34.5% |
| 119 | `wago/kissfft/complex-roundtrip` | 178.708000 | 276.667000 | 0.646× | -35.4% |
| 120 | `wago/polybench-fdtd-2d/polybench_run` | 513.250000 | 796.541000 | 0.644× | -35.6% |
| 121 | `applications/geo-polygon-clipping` | 40.542000 | 64.125000 | 0.632× | -36.8% |
| 122 | `applications/game-cellular-automata` | 111.833000 | 178.375000 | 0.627× | -37.3% |
| 123 | `wago/polybench-gramschmidt/polybench_run` | 367.292000 | 592.417000 | 0.620× | -38.0% |
| 124 | `wago/memory_tree/run` | 5.750000 | 9.416000 | 0.611× | -38.9% |
| 125 | `applications/numeric-integer-sqrt` | 180.625000 | 310.042000 | 0.583× | -41.7% |
| 126 | `applications/search-kmp` | 96.167000 | 170.083000 | 0.565× | -43.5% |
| 127 | `applications/compression-lzw` | 344.375000 | 619.084000 | 0.556× | -44.4% |
| 128 | `applications/language-brainfuck` | 29.833000 | 54.375000 | 0.549× | -45.1% |
| 129 | `wago/fannkuch/run` | 969.375000 | 1842.958000 | 0.526× | -47.4% |
| 130 | `wago/polybench-correlation/polybench_run` | 231.750000 | 451.791000 | 0.513× | -48.7% |
| 131 | `wago/polybench-gemm/polybench_run` | 207.208000 | 422.625000 | 0.490× | -51.0% |
| 132 | `wago/polybench-deriche/polybench_run` | 409.708000 | 852.500000 | 0.481× | -51.9% |
| 133 | `applications/image-median` | 328.167000 | 704.375000 | 0.466× | -53.4% |
| 134 | `applications/triangle-raster` | 135.791000 | 297.125000 | 0.457× | -54.3% |
| 135 | `wago/polybench-lu/polybench_run` | 1185.500000 | 2616.292000 | 0.453× | -54.7% |
| 136 | `wago/nbody/step` | 134.708000 | 304.542000 | 0.442× | -55.8% |
| 137 | `wago/utf8proc/normalize-casefold` | 1.000000 | 2.459000 | 0.407× | -59.3% |
| 138 | `wago/memory/sum` | 0.167000 | 0.459000 | 0.364× | -63.6% |
| 139 | `applications/vision-sobel` | 75.709000 | 252.667000 | 0.300× | -70.0% |
| 140 | `wago/zlib/inflate` | 6.625000 | 44.666000 | 0.148× | -85.2% |
| 141 | `wago/many_funcs/run` | 0.042000 | 0.292000 | 0.144× | -85.6% |
| 142 | `wago/tiny/add` | 0.042000 | 0.292000 | 0.144× | -85.6% |
| 143 | `wago/linked_list/sum` | 4.042000 | 35.833000 | 0.113× | -88.7% |
| 144 | `wago/dispatch/apply` | 0.042000 | 0.708000 | 0.059× | -94.1% |
| 145 | `mechanisms/wasm-to-host-call` | 0.014021 | 0.302369 | 0.046× | -95.4% |
| 146 | `mechanisms/host-to-wasm-call` | 0.010947 | 0.291833 | 0.038× | -96.2% |

### Unranked workloads

| Corpus workload | Wago steady (µs/op) | w2c2 steady (µs/op) | Reason |
| --- | ---: | ---: | --- |
| `wago/json-as-simd/serializeN` | 11.000000 | unsupported | Wago: ok; w2c2: unsupported |
| `wago/utf-as-simd/validateN` | 136.209000 | unsupported | Wago: ok; w2c2: unsupported |
