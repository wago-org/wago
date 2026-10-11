# Retained ARM64 priority subset, 2026-10-11

Apple M4 Max, arm64. 54 focused workloads ranked worst-to-best against historical w2c2. These captures cover the qualified defaults after `76e52be8` plus `integer-select-local-read`. No full timing sweep was run. Times are median microseconds per exact invocation after warmup; every raw observation remains available.

Seven rows use the final integrated source compiler in reversed process comparisons. Other changed images use the final isolated-select affected-workload repeat; combined validation proves those native images identical. Older captures are retained only for workloads whose native images remain identical. Captures have different dates/protocols, and ranges expose outliers. This is a priority list, not a simultaneously measured parity claim.

w2c2 was not rerun: its microseconds come from the [complete historical device ranking](../../steady-exec-wago-vs-w2c2.md), source `main@700f5565`, captured 2026-10-09. Positive gaps mean Wago is slower. Workload contracts differ; compare each row only to its matching corpus.

| Rank | Corpus workload | Retained Wago µs | Observed range µs | Historical w2c2 µs | Ratio | Gap | Wago capture |
|---:|---|---:|---:|---:|---:|---:|---|
| 1 | `wago/libtommath/modular-arithmetic` | 11.729894 | 11.594755–58.860480 | 6.500000 | 1.805× | +80.5% | isolated select; integrated image identical; 4×150ms |
| 2 | `applications/compiler-register-allocation` | 17.190280 | 17.145296–17.356742 | 9.916000 | 1.734× | +73.4% | three retained defaults; 4×150ms processes |
| 3 | `applications/geo-point-in-polygon` | 20.839113 | 20.580021–21.371876 | 12.208000 | 1.707× | +70.7% | three retained defaults; 4×150ms processes |
| 4 | `applications/files-glob-match` | 131.637603 | 129.534298–143.049184 | 78.542000 | 1.676× | +67.6% | three retained defaults; 4×150ms processes |
| 5 | `applications/vision-components` | 38.618160 | 37.612128–39.223597 | 23.084000 | 1.673× | +67.3% | isolated select; integrated image identical; 4×150ms |
| 6 | `wago/pcre2/compile-match` | 8.131038 | 8.073984–8.140213 | 4.917000 | 1.654× | +65.4% | isolated select; integrated image identical; 4×150ms |
| 7 | `applications/language-register-vm` | 10.986821 | 10.866755–11.103671 | 6.875000 | 1.598× | +59.8% | 37174169 unchanged image; 8×150ms |
| 8 | `wago/nanosvg/parse-structure` | 2.385712 | 2.375358–2.396604 | 1.542000 | 1.547× | +54.7% | isolated select; integrated image identical; 4×150ms |
| 9 | `wago/zstd/decompress` | 5.669000 | 5.627000–5.694000 | 3.667000 | 1.546× | +54.6% | isolated select; integrated image identical; 4×150ms |
| 10 | `applications/video-optical-flow` | 18.533638 | 18.458929–18.579270 | 12.041000 | 1.539× | +53.9% | 37174169 unchanged image; 8×150ms |
| 11 | `wago/monocypher/aead-blake2b` | 28.863728 | 28.777737–29.065558 | 19.458000 | 1.483× | +48.3% | isolated select; integrated image identical; 4×150ms |
| 12 | `applications/ml-kmeans` | 23.762335 | 23.108571–26.647098 | 16.208000 | 1.466× | +46.6% | three retained defaults; 4×150ms processes |
| 13 | `wago/fastfloat/decimal-parse` | 0.665286 | 0.655291–0.671548 | 0.458000 | 1.453× | +45.3% | isolated select; integrated image identical; 4×150ms |
| 14 | `applications/video-dct` | 39.714747 | 39.537327–40.151062 | 27.417000 | 1.449× | +44.9% | 37174169 unchanged image; 8×150ms |
| 15 | `applications/compiler-dead-code` | 4.143694 | 4.141541–4.153806 | 2.875000 | 1.441× | +44.1% | isolated select; integrated image identical; 4×150ms |
| 16 | `applications/geo-polyline-simplify` | 5.328101 | 5.324952–5.333418 | 3.708000 | 1.437× | +43.7% | isolated select; integrated image identical; 4×150ms |
| 17 | `wago/coremark/2k-performance` | 26146.838500 | 25820.010000–26346.479000 | 18705.583000 | 1.398× | +39.8% | isolated select; integrated image identical; 4×150ms |
| 18 | `applications/vision-dilation` | 40.576645 | 40.100277–41.461593 | 29.375000 | 1.381× | +38.1% | 37174169 unchanged image; 8×150ms |
| 19 | `applications/hardware-boolean-anf` | 67.620056 | 66.830062–74.161324 | 49.000000 | 1.380× | +38.0% | three retained defaults; 4×150ms processes |
| 20 | `applications/ml-knn` | 30.148343 | 29.834279–30.388322 | 22.917000 | 1.316× | +31.6% | 37174169 unchanged image; 8×150ms |
| 21 | `applications/files-path-normalize` | 14.404930 | 14.146318–14.478503 | 10.958000 | 1.315× | +31.5% | 37174169 unchanged image; 6×125ms |
| 22 | `applications/search-aho-corasick` | 48.632632 | 47.754456–50.833191 | 37.292000 | 1.304× | +30.4% | 37174169 unchanged image; 6×125ms |
| 23 | `applications/graphics-reed-solomon` | 114.128669 | 113.693431–114.779836 | 87.792000 | 1.300× | +30.0% | 37174169 unchanged image; 8×150ms |
| 24 | `applications/vision-distance-transform` | 63.294720 | 62.869025–63.677123 | 48.750000 | 1.298× | +29.8% | isolated select; integrated image identical; 4×150ms |
| 25 | `applications/audio-adpcm` | 30.448622 | 30.317605–30.920817 | 23.458000 | 1.298× | +29.8% | three retained defaults; 4×150ms processes |
| 26 | `applications/grid-pathfinding` | 37.913750 | 37.828256–38.041870 | 29.250000 | 1.296× | +29.6% | isolated select; integrated image identical; 4×150ms |
| 27 | `applications/hardware-prime-implicants` | 48.777185 | 48.002881–49.343929 | 38.584000 | 1.264× | +26.4% | isolated select; integrated image identical; 4×150ms |
| 28 | `wago/drwav/pcm-decode-seek` | 7.408717 | 7.364969–7.456564 | 5.959000 | 1.243× | +24.3% | isolated select; integrated image identical; 4×150ms |
| 29 | `applications/vision-otsu` | 8.544959 | 8.479596–8.578684 | 6.916000 | 1.236× | +23.6% | isolated select; integrated image identical; 4×150ms |
| 30 | `applications/serialization-protobuf` | 16.944530 | 16.563838–17.542246 | 13.833000 | 1.225× | +22.5% | 37174169 unchanged image; 6×125ms |
| 31 | `applications/video-motion` | 449.366112 | 442.402523–455.812307 | 373.584000 | 1.203× | +20.3% | isolated select; integrated image identical; 4×150ms |
| 32 | `applications/stats-bootstrap` | 21.052628 | 20.917667–23.280068 | 18.167000 | 1.159× | +15.9% | 37174169 unchanged image; 6×125ms |
| 33 | `applications/ml-convolution` | 24.032964 | 23.763445–24.824683 | 21.084000 | 1.140× | +14.0% | 37174169 unchanged image; 6×125ms |
| 34 | `wago/yyjson/parse-edit-write` | 0.764035 | 0.763351–0.765126 | 0.709000 | 1.078× | +7.8% | isolated select; integrated image identical; 4×150ms |
| 35 | `applications/audio-fir` | 23.618080 | 23.487818–24.047471 | 23.250000 | 1.016× | +1.6% | 37174169 unchanged image; 6×125ms |
| 36 | `applications/stats-linear-regression` | 1.784026 | 1.748828–1.833316 | 1.833000 | 0.973× | -2.7% | 37174169 unchanged image; 6×125ms |
| 37 | `applications/ml-inference` | 5.834762 | 5.804469–5.866644 | 6.125000 | 0.953× | -4.7% | 37174169 unchanged image; 6×125ms |
| 38 | `applications/stencil-wave-equation` | 61.918642 | 61.537504–62.143176 | 66.833000 | 0.926× | -7.4% | 37174169 unchanged image; 8×150ms |
| 39 | `applications/vision-fast-corners` | 776.777668 | 771.560227–791.185227 | 849.000000 | 0.915× | -8.5% | isolated select; integrated image identical; 4×150ms |
| 40 | `applications/stats-quickselect` | 14.886887 | 14.785611–14.963881 | 17.458000 | 0.853× | -14.7% | isolated select; integrated image identical; 4×150ms |
| 41 | `applications/document-edit-distance` | 72.522073 | 72.130825–73.411502 | 85.167000 | 0.852× | -14.8% | 37174169 unchanged image; 8×150ms |
| 42 | `applications/particle-physics` | 598.207708 | 597.885289–598.990964 | 730.584000 | 0.819× | -18.1% | isolated select; integrated image identical; 4×150ms |
| 43 | `applications/graphics-bresenham` | 149.667400 | 148.817144–150.466521 | 195.209000 | 0.767× | -23.3% | isolated select; integrated image identical; 4×150ms |
| 44 | `applications/numeric-cordic` | 47.670516 | 46.638772–54.984503 | 62.667000 | 0.761× | -23.9% | isolated select; integrated image identical; 4×150ms |
| 45 | `wago/raytrace/render` | 248.970216 | 246.952995–250.538317 | 351.250000 | 0.709× | -29.1% | isolated select; integrated image identical; 4×150ms |
| 46 | `applications/document-layout` | 100.186127 | 95.334442–102.513680 | 145.083000 | 0.691× | -30.9% | isolated select; integrated image identical; 4×150ms |
| 47 | `wago/kissfft/complex-roundtrip` | 158.142190 | 157.621324–159.451238 | 276.667000 | 0.572× | -42.8% | isolated select; integrated image identical; 4×150ms |
| 48 | `applications/compression-lzw` | 341.373097 | 337.999429–343.569160 | 619.084000 | 0.551× | -44.9% | 37174169 unchanged image; 8×150ms |
| 49 | `applications/search-kmp` | 79.050680 | 78.789841–79.578453 | 170.083000 | 0.465× | -53.5% | isolated select; integrated image identical; 4×150ms |
| 50 | `applications/geo-polygon-clipping` | 28.967301 | 28.757380–29.939061 | 64.125000 | 0.452× | -54.8% | three retained defaults; 4×150ms processes |
| 51 | `applications/triangle-raster` | 130.494680 | 128.914078–132.564401 | 297.125000 | 0.439× | -56.1% | 37174169 unchanged image; 8×150ms |
| 52 | `applications/image-median` | 305.425775 | 296.985856–321.653416 | 704.375000 | 0.434× | -56.6% | 37174169 unchanged image; 8×150ms |
| 53 | `wago/utf8proc/normalize-casefold` | 0.821946 | 0.818103–0.826620 | 2.459000 | 0.334× | -66.6% | isolated select; integrated image identical; 4×150ms |
| 54 | `wago/zlib/inflate` | 6.355500 | 6.351000–6.380000 | 44.666000 | 0.142× | -85.8% | isolated select; integrated image identical; 4×150ms |

The dispatch-index prototype and subsequent experiments are excluded. OS-thread locking and QoS do not establish physical-core affinity. See [retained changes and compile costs](next-20261011-results.md), [combined native proof](next-20261011-select-retain-native-proof.txt), and [source comparison](next-20261011-select-cross-summary.md).

Source SHA256:

- `hour-20261011-focused-exec.jsonl`: `71191dc6f9ebf06a0c4c00aac288642c82290206fc3df851f66fe2e8c10f9c0c`
- `next-20261011-gap-screen-exec.jsonl`: `e1e47b3b356aa3a8ea74f18772d49afcb74071e98a96dfb5987b13f72ebc763d`
- `hour-20261011-focused-core-exec.jsonl`: `6e6114e954591f4a4ed94c29b367548320f1ebee2a31eff5b6841418fbf525a2`
- `integer-select-local-read/affected-subset/all.jsonl`: `bee6f5b96f699e6a1fb33567f5ad817b3859ff610c0a4b1b29a82d936c82391b`
- `next-20261011-select-cross-build.jsonl`: `6f9080290f02c7cfee8edd61cd5953dc2eb668ef93888ed4e65518fc0538ff92`
