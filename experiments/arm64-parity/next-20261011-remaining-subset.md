# Current ARM64 priority subset, 2026-10-11

Apple M4 Max, arm64. These 32 focused workloads combine completed captures of the qualified default compiler at `37174169`. No full timing sweep was performed. Times are median microseconds per invocation, after warmup, for each workload's exact cached contract. The latest exploratory changes are excluded until retained and qualified.

The w2c2 column comes from the [complete historical device ranking](../../steady-exec-wago-vs-w2c2.md), captured 2026-10-09 for `w2c2-gcc` / `c-aot`, source `main@700f5565`. w2c2 was not rerun. Ratios combine different captures and protocols; they prioritize remaining work and do not prove current full-corpus parity. Positive gaps mean Wago is slower. Workloads map by exact corpus ID; missing timings are never zero.

| Rank | Corpus workload | Current Wago µs | Current range µs | Historical w2c2 µs | Ratio | Gap | Current capture |
|---:|---|---:|---:|---:|---:|---:|---|
| 1 | `wago/libtommath/modular-arithmetic` | 11.803072 | 11.780708–11.888986 | 6.500000 | 1.816× | +81.6% | 37174169; 6×150ms |
| 2 | `applications/geo-point-in-polygon` | 21.359996 | 21.255691–21.493378 | 12.208000 | 1.750× | +75.0% | 37174169; 4×100ms processes |
| 3 | `applications/compiler-register-allocation` | 16.725354 | 16.702152–16.847135 | 9.916000 | 1.687× | +68.7% | 37174169; 8×150ms |
| 4 | `applications/files-glob-match` | 131.006088 | 129.579794–132.419782 | 78.542000 | 1.668× | +66.8% | 37174169; 8×150ms |
| 5 | `applications/language-register-vm` | 10.986821 | 10.866755–11.103671 | 6.875000 | 1.598× | +59.8% | 37174169; 8×150ms |
| 6 | `applications/vision-components` | 36.279886 | 35.683090–36.704167 | 23.084000 | 1.572× | +57.2% | 37174169; 8×150ms |
| 7 | `applications/video-optical-flow` | 18.533638 | 18.458929–18.579270 | 12.041000 | 1.539× | +53.9% | 37174169; 8×150ms |
| 8 | `applications/ml-kmeans` | 24.923163 | 24.696828–198.569597 | 16.208000 | 1.538× | +53.8% | 37174169; 6×125ms |
| 9 | `applications/hardware-boolean-anf` | 73.293554 | 72.547958–73.955526 | 49.000000 | 1.496× | +49.6% | 37174169; 6×125ms |
| 10 | `wago/fastfloat/decimal-parse` | 0.670742 | 0.660340–0.676441 | 0.458000 | 1.465× | +46.5% | 37174169; 6×150ms |
| 11 | `applications/video-dct` | 39.714747 | 39.537327–40.151062 | 27.417000 | 1.449× | +44.9% | 37174169; 8×150ms |
| 12 | `applications/vision-dilation` | 40.576645 | 40.100277–41.461593 | 29.375000 | 1.381× | +38.1% | 37174169; 8×150ms |
| 13 | `applications/grid-pathfinding` | 38.496967 | 37.601204–39.525685 | 29.250000 | 1.316× | +31.6% | 37174169; 6×125ms |
| 14 | `applications/ml-knn` | 30.148343 | 29.834279–30.388322 | 22.917000 | 1.316× | +31.6% | 37174169; 8×150ms |
| 15 | `applications/files-path-normalize` | 14.404930 | 14.146318–14.478503 | 10.958000 | 1.315× | +31.5% | 37174169; 6×125ms |
| 16 | `applications/search-aho-corasick` | 48.632632 | 47.754456–50.833191 | 37.292000 | 1.304× | +30.4% | 37174169; 6×125ms |
| 17 | `applications/graphics-reed-solomon` | 114.128669 | 113.693431–114.779836 | 87.792000 | 1.300× | +30.0% | 37174169; 8×150ms |
| 18 | `applications/audio-adpcm` | 29.827262 | 29.475295–30.205043 | 23.458000 | 1.272× | +27.2% | 37174169; 8×150ms |
| 19 | `applications/hardware-prime-implicants` | 48.219160 | 47.572765–48.977798 | 38.584000 | 1.250× | +25.0% | 37174169; 6×125ms |
| 20 | `applications/serialization-protobuf` | 16.944530 | 16.563838–17.542246 | 13.833000 | 1.225× | +22.5% | 37174169; 6×125ms |
| 21 | `applications/stats-bootstrap` | 21.052628 | 20.917667–23.280068 | 18.167000 | 1.159× | +15.9% | 37174169; 6×125ms |
| 22 | `applications/ml-convolution` | 24.032964 | 23.763445–24.824683 | 21.084000 | 1.140× | +14.0% | 37174169; 6×125ms |
| 23 | `applications/audio-fir` | 23.618080 | 23.487818–24.047471 | 23.250000 | 1.016× | +1.6% | 37174169; 6×125ms |
| 24 | `applications/stats-linear-regression` | 1.784026 | 1.748828–1.833316 | 1.833000 | 0.973× | -2.7% | 37174169; 6×125ms |
| 25 | `applications/ml-inference` | 5.834762 | 5.804469–5.866644 | 6.125000 | 0.953× | -4.7% | 37174169; 6×125ms |
| 26 | `applications/stencil-wave-equation` | 61.918642 | 61.537504–62.143176 | 66.833000 | 0.926× | -7.4% | 37174169; 8×150ms |
| 27 | `applications/document-edit-distance` | 72.522073 | 72.130825–73.411502 | 85.167000 | 0.852× | -14.8% | 37174169; 8×150ms |
| 28 | `wago/kissfft/complex-roundtrip` | 156.373549 | 155.704132–157.364995 | 276.667000 | 0.565× | -43.5% | 37174169; 6×150ms |
| 29 | `applications/compression-lzw` | 341.373097 | 337.999429–343.569160 | 619.084000 | 0.551× | -44.9% | 37174169; 8×150ms |
| 30 | `applications/geo-polygon-clipping` | 30.503833 | 30.104380–139.779748 | 64.125000 | 0.476× | -52.4% | 37174169; 8×150ms |
| 31 | `applications/triangle-raster` | 130.494680 | 128.914078–132.564401 | 297.125000 | 0.439× | -56.1% | 37174169; 8×150ms |
| 32 | `applications/image-median` | 305.425775 | 296.985856–321.653416 | 704.375000 | 0.434× | -56.6% | 37174169; 8×150ms |

The hour application and core captures select all five retained optimization switches ON. The next 12-application capture selects `ordinary-shifted-immediate` ON, with the other retained features at their default. All ON rows represent the same qualified compiler defaults. The single polygon row uses the current-source process from four reversed old/current pairs. All remaining rows use alternating ON/OFF pairs in one frozen binary; only ON samples enter this ranking. These protocols request QoS33 and lock the OS thread, which does not establish physical-core affinity.

The old 7.706× Aho–Corasick gap is now 1.304× in this subset. Linear regression and ML inference are approximately at historical w2c2 parity. Modular arithmetic, point-in-polygon, register allocation, glob matching, and the register VM remain higher priorities. This is a current priority list, not a claim that the missing 116 contracts have comparable fresh timings.

Source SHA256:

- `hour-20261011-focused-exec.jsonl`: `71191dc6f9ebf06a0c4c00aac288642c82290206fc3df851f66fe2e8c10f9c0c`
- `next-20261011-gap-screen-exec.jsonl`: `e1e47b3b356aa3a8ea74f18772d49afcb74071e98a96dfb5987b13f72ebc763d`
- `hour-20261011-focused-core-exec.jsonl`: `6e6114e954591f4a4ed94c29b367548320f1ebee2a31eff5b6841418fbf525a2`
- `hour-20261011-cross-build.jsonl`: `73f6ccb1d1f0c41b9b67605c18fbfe5c2e39647fd2600cc3d61cf41b9221f15e`

See [the hour results](hour-20261011-results.md) for paired gains, compilation costs, exact validation, and native-image qualification. See [the new gap screen](next-20261011-gap-screen-summary.md) for the latest 12 switch comparisons; small movements in unchanged native images are not credited as execution improvements.
