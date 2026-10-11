# Retained ARM64 priority subset, 2026-10-11

Apple M4 Max, arm64. 54 focused workloads ranked worst-to-best against historical w2c2. These captures cover the compiler with the five retained changes through `cset-lineage`, based on `7257c1c0`. No full timing sweep was run. Times are median microseconds per exact invocation after warmup; every raw observation remains available.

Fourteen native-affected rows use the qualified CSET-lineage four-pair 100ms repeat. Its final six reversed-process 150ms source pairs override six overlapping cases and also refresh unchanged ADPCM. The source candidates contain the three earlier changes plus CSET lineage; qualification proves their measured images match the final images with all five retained changes, including preservation of the existing register-VM dispatch. Earlier three/four-change source pairs, isolated-select repeats and older captures remain only for images unchanged by CSET lineage. Dates/protocols differ, and ranges expose outliers. This is a priority list, not a simultaneously measured parity claim.

w2c2 was not rerun: its microseconds come from the [complete historical device ranking](../../steady-exec-wago-vs-w2c2.md), source `main@700f5565`, captured 2026-10-09. Positive gaps mean Wago is slower. Workload contracts differ; compare each row only to its matching corpus.

| Rank | Corpus workload | Retained Wago µs | Observed range µs | Historical w2c2 µs | Ratio | Gap | Wago capture |
|---:|---|---:|---:|---:|---:|---:|---|
| 1 | `wago/libtommath/modular-arithmetic` | 11.875176 | 11.662257–12.393849 | 6.500000 | 1.827× | +82.7% | CSET source; final image identical; 6×150ms processes |
| 2 | `applications/compiler-register-allocation` | 17.240390 | 17.163977–17.477321 | 9.916000 | 1.739× | +73.9% | four retained defaults; 4×150ms processes |
| 3 | `applications/geo-point-in-polygon` | 20.839113 | 20.580021–21.371876 | 12.208000 | 1.707× | +70.7% | three retained defaults; 4×150ms processes |
| 4 | `applications/files-glob-match` | 131.767017 | 130.509938–132.048902 | 78.542000 | 1.678× | +67.8% | four retained defaults; 4×150ms processes |
| 5 | `applications/vision-components` | 38.618160 | 37.612128–39.223597 | 23.084000 | 1.673× | +67.3% | isolated select; integrated image identical; 4×100ms |
| 6 | `wago/pcre2/compile-match` | 8.131038 | 8.073984–8.140213 | 4.917000 | 1.654× | +65.4% | isolated select; integrated image identical; 4×100ms |
| 7 | `wago/nanosvg/parse-structure` | 2.436406 | 2.346309–2.629518 | 1.542000 | 1.580× | +58.0% | CSET source; final image identical; 6×150ms processes |
| 8 | `wago/zstd/decompress` | 5.669000 | 5.627000–5.694000 | 3.667000 | 1.546× | +54.6% | isolated select; integrated image identical; 4×100ms |
| 9 | `applications/video-optical-flow` | 18.533638 | 18.458929–18.579270 | 12.041000 | 1.539× | +53.9% | 37174169 unchanged image; 8×150ms |
| 10 | `applications/language-register-vm` | 10.320478 | 10.165086–10.402835 | 6.875000 | 1.501× | +50.1% | four retained defaults; 4×150ms processes |
| 11 | `wago/monocypher/aead-blake2b` | 28.863728 | 28.777737–29.065558 | 19.458000 | 1.483× | +48.3% | isolated select; integrated image identical; 4×100ms |
| 12 | `wago/fastfloat/decimal-parse` | 0.665286 | 0.655291–0.671548 | 0.458000 | 1.453× | +45.3% | isolated select; integrated image identical; 4×100ms |
| 13 | `applications/video-dct` | 39.714747 | 39.537327–40.151062 | 27.417000 | 1.449× | +44.9% | 37174169 unchanged image; 8×150ms |
| 14 | `applications/compiler-dead-code` | 4.143694 | 4.141541–4.153806 | 2.875000 | 1.441× | +44.1% | isolated select; integrated image identical; 4×100ms |
| 15 | `applications/ml-kmeans` | 22.769538 | 22.493220–23.947364 | 16.208000 | 1.405× | +40.5% | CSET source; final image identical; 6×150ms processes |
| 16 | `wago/coremark/2k-performance` | 26146.838500 | 25820.010000–26346.479000 | 18705.583000 | 1.398× | +39.8% | isolated select; integrated image identical; 4×100ms |
| 17 | `applications/hardware-boolean-anf` | 68.084392 | 67.213542–68.687518 | 49.000000 | 1.389× | +38.9% | four retained defaults; 4×150ms processes |
| 18 | `applications/vision-dilation` | 40.576645 | 40.100277–41.461593 | 29.375000 | 1.381× | +38.1% | 37174169 unchanged image; 8×150ms |
| 19 | `applications/ml-knn` | 30.529874 | 30.218816–30.798480 | 22.917000 | 1.332× | +33.2% | isolated CSET; final image identical; 4×100ms |
| 20 | `applications/files-path-normalize` | 14.404930 | 14.146318–14.478503 | 10.958000 | 1.315× | +31.5% | 37174169 unchanged image; 6×125ms |
| 21 | `applications/search-aho-corasick` | 48.632632 | 47.754456–50.833191 | 37.292000 | 1.304× | +30.4% | 37174169 unchanged image; 6×125ms |
| 22 | `applications/graphics-reed-solomon` | 114.128669 | 113.693431–114.779836 | 87.792000 | 1.300× | +30.0% | 37174169 unchanged image; 8×150ms |
| 23 | `applications/vision-distance-transform` | 63.294720 | 62.869025–63.677123 | 48.750000 | 1.298× | +29.8% | isolated select; integrated image identical; 4×100ms |
| 24 | `applications/grid-pathfinding` | 37.913750 | 37.828256–38.041870 | 29.250000 | 1.296× | +29.6% | isolated select; integrated image identical; 4×100ms |
| 25 | `applications/audio-adpcm` | 30.276603 | 29.973345–30.997391 | 23.458000 | 1.291× | +29.1% | CSET source; final image identical; 6×150ms processes |
| 26 | `applications/hardware-prime-implicants` | 48.513927 | 48.084828–50.330551 | 38.584000 | 1.257× | +25.7% | CSET source; final image identical; 6×150ms processes |
| 27 | `wago/drwav/pcm-decode-seek` | 7.408717 | 7.364969–7.456564 | 5.959000 | 1.243× | +24.3% | isolated select; integrated image identical; 4×100ms |
| 28 | `applications/vision-otsu` | 8.544959 | 8.479596–8.578684 | 6.916000 | 1.236× | +23.6% | isolated select; integrated image identical; 4×100ms |
| 29 | `applications/serialization-protobuf` | 16.944530 | 16.563838–17.542246 | 13.833000 | 1.225× | +22.5% | 37174169 unchanged image; 6×125ms |
| 30 | `applications/geo-polyline-simplify` | 4.344481 | 4.238077–4.412075 | 3.708000 | 1.172× | +17.2% | CSET source; final image identical; 6×150ms processes |
| 31 | `applications/video-motion` | 435.926489 | 430.437590–441.518907 | 373.584000 | 1.167× | +16.7% | isolated CSET; final image identical; 4×100ms |
| 32 | `applications/stats-bootstrap` | 21.052628 | 20.917667–23.280068 | 18.167000 | 1.159× | +15.9% | 37174169 unchanged image; 6×125ms |
| 33 | `applications/ml-convolution` | 24.032964 | 23.763445–24.824683 | 21.084000 | 1.140× | +14.0% | 37174169 unchanged image; 6×125ms |
| 34 | `wago/yyjson/parse-edit-write` | 0.764035 | 0.763351–0.765126 | 0.709000 | 1.078× | +7.8% | isolated select; integrated image identical; 4×100ms |
| 35 | `applications/audio-fir` | 23.618080 | 23.487818–24.047471 | 23.250000 | 1.016× | +1.6% | 37174169 unchanged image; 6×125ms |
| 36 | `applications/stats-linear-regression` | 1.784026 | 1.748828–1.833316 | 1.833000 | 0.973× | -2.7% | 37174169 unchanged image; 6×125ms |
| 37 | `applications/ml-inference` | 5.834762 | 5.804469–5.866644 | 6.125000 | 0.953× | -4.7% | 37174169 unchanged image; 6×125ms |
| 38 | `applications/stencil-wave-equation` | 63.349705 | 63.142292–63.845407 | 66.833000 | 0.948× | -5.2% | isolated CSET; final image identical; 4×100ms |
| 39 | `applications/vision-fast-corners` | 776.777668 | 771.560227–791.185227 | 849.000000 | 0.915× | -8.5% | isolated select; integrated image identical; 4×100ms |
| 40 | `applications/stats-quickselect` | 15.173907 | 14.691976–15.538669 | 17.458000 | 0.869× | -13.1% | isolated CSET; final image identical; 4×100ms |
| 41 | `applications/document-edit-distance` | 72.522073 | 72.130825–73.411502 | 85.167000 | 0.852× | -14.8% | 37174169 unchanged image; 8×150ms |
| 42 | `applications/numeric-cordic` | 49.110379 | 46.443531–51.152996 | 62.667000 | 0.784× | -21.6% | isolated CSET; final image identical; 4×100ms |
| 43 | `applications/graphics-bresenham` | 145.557501 | 145.238147–146.525024 | 195.209000 | 0.746× | -25.4% | isolated CSET; final image identical; 4×100ms |
| 44 | `wago/raytrace/render` | 248.970216 | 246.952995–250.538317 | 351.250000 | 0.709× | -29.1% | isolated select; integrated image identical; 4×100ms |
| 45 | `applications/document-layout` | 100.186127 | 95.334442–102.513680 | 145.083000 | 0.691× | -30.9% | isolated select; integrated image identical; 4×100ms |
| 46 | `applications/particle-physics` | 483.606772 | 469.325590–494.084010 | 730.584000 | 0.662× | -33.8% | CSET source; final image identical; 6×150ms processes |
| 47 | `wago/kissfft/complex-roundtrip` | 158.142190 | 157.621324–159.451238 | 276.667000 | 0.572× | -42.8% | isolated select; integrated image identical; 4×100ms |
| 48 | `applications/compression-lzw` | 341.373097 | 337.999429–343.569160 | 619.084000 | 0.551× | -44.9% | 37174169 unchanged image; 8×150ms |
| 49 | `applications/search-kmp` | 77.960256 | 76.947554–78.606908 | 170.083000 | 0.458× | -54.2% | isolated CSET; final image identical; 4×100ms |
| 50 | `applications/geo-polygon-clipping` | 28.484908 | 28.227046–29.079695 | 64.125000 | 0.444× | -55.6% | isolated CSET; final image identical; 4×100ms |
| 51 | `applications/triangle-raster` | 130.494680 | 128.914078–132.564401 | 297.125000 | 0.439× | -56.1% | 37174169 unchanged image; 8×150ms |
| 52 | `applications/image-median` | 305.425775 | 296.985856–321.653416 | 704.375000 | 0.434× | -56.6% | 37174169 unchanged image; 8×150ms |
| 53 | `wago/utf8proc/normalize-casefold` | 0.821946 | 0.818103–0.826620 | 2.459000 | 0.334× | -66.6% | isolated select; integrated image identical; 4×100ms |
| 54 | `wago/zlib/inflate` | 6.355500 | 6.351000–6.380000 | 44.666000 | 0.142× | -85.8% | isolated select; integrated image identical; 4×100ms |

Subsequent exploratory changes are excluded. OS-thread locking and QoS do not establish physical-core affinity. See [retained changes and compile costs](next-20261011-results.md), [CSET final native proof](cset-lineage/retained/native-proof.txt), and [latest source comparison](cset-lineage/cross-cheap/summary.md).

Source SHA256:

- `hour-20261011-focused-exec.jsonl`: `71191dc6f9ebf06a0c4c00aac288642c82290206fc3df851f66fe2e8c10f9c0c`
- `next-20261011-gap-screen-exec.jsonl`: `e1e47b3b356aa3a8ea74f18772d49afcb74071e98a96dfb5987b13f72ebc763d`
- `hour-20261011-focused-core-exec.jsonl`: `6e6114e954591f4a4ed94c29b367548320f1ebee2a31eff5b6841418fbf525a2`
- `integer-select-local-read/affected-subset/all.jsonl`: `bee6f5b96f699e6a1fb33567f5ad817b3859ff610c0a4b1b29a82d936c82391b`
- `next-20261011-select-cross-build.jsonl`: `6f9080290f02c7cfee8edd61cd5953dc2eb668ef93888ed4e65518fc0538ff92`
- `next-20261011-dispatch-cross-build.jsonl`: `ca0eda0f394a7574c9c960a8eb449e4492a462130d93d70ef18881a3c13a7f1c`
- `cset-lineage/inventory/changes.json`: `ac589352e3c3025148d633d87817bf52c7243ce50f6277886877e69d103eb982`
- `cset-lineage/retained/native-proof.txt`: `1aacb4404778e2812b374df1382013c69fe85242e72a78a3c7009b8e8d54c878`
- `cset-lineage/cset-lineage-subset.py`: `4f3ca4c7af5622673b7f4adf0bbf606f00b7226e4a86189de378c78d48b10945`
- `cset-lineage/cset-lineage-cross-cheap.py`: `369b293a0c6a1a81469e5290e9e05432264f5edd4e0ed924559ab103fb2f888f`
- `cset-lineage/subset/all.jsonl`: `55253a7fc7447186d7d63b9bc4f2a1f659e3de5d7add1f26e133053383e67746`
- `cset-lineage/cross-cheap/all.jsonl`: `a6b0b7faa524db7b975ba5fd0a2fa7df89b14b5318c85ded37bd920744ba3533`
