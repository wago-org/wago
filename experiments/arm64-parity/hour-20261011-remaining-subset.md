# Remaining ARM64 gaps after the 2026-10-11 focused batch

Apple M4 Max, arm64. Fresh current Wago times are ON medians from 8 alternating 150ms rounds in the frozen `/tmp/paired-hour-20261011` build. Warmups are excluded; times are microseconds per invocation. The first 16 applications use that same-thread protocol; point-in-polygon is added only when its separate cross-build batch is complete. It uses four reversed-process 100ms pairs and the current candidate/ON median. `geo-point-in-polygon` and `geo-polygon-clipping` are distinct workloads.

The w2c2 values are the exact steady medians reported in [the historical device ranking](../../steady-exec-wago-vs-w2c2.md), captured 2026-10-09 for `w2c2-gcc` / `c-aot`, source `main@700f5565`. Workloads map by exact `applications/<name>` corpus ID. w2c2 was not rerun. These ratios are unpaired across captures and protocols and serve subset prioritization; they do not establish current w2c2 performance or full-corpus parity.

## Fresh current versus historical w2c2

Worst-to-best is descending current Wago / historical w2c2. Positive gap means Wago is slower.

| Rank | Corpus workload | Fresh ON median µs | ON range µs | Historical w2c2 steady µs | Ratio | Gap | Current source |
|---:|---|---:|---:|---:|---:|---:|---|
| 1 | `applications/geo-point-in-polygon` | 21.359996 | 21.255691–21.493378 | 12.208000 | 1.750× | +75.0% | 4×100ms reversed process |
| 2 | `applications/compiler-register-allocation` | 16.725354 | 16.702152–16.847135 | 9.916000 | 1.687× | +68.7% | 8×150ms same-thread ON |
| 3 | `applications/files-glob-match` | 131.006088 | 129.579794–132.419782 | 78.542000 | 1.668× | +66.8% | 8×150ms same-thread ON |
| 4 | `applications/language-register-vm` | 10.986821 | 10.866755–11.103671 | 6.875000 | 1.598× | +59.8% | 8×150ms same-thread ON |
| 5 | `applications/vision-components` | 36.279886 | 35.683090–36.704167 | 23.084000 | 1.572× | +57.2% | 8×150ms same-thread ON |
| 6 | `applications/video-optical-flow` | 18.533638 | 18.458929–18.579270 | 12.041000 | 1.539× | +53.9% | 8×150ms same-thread ON |
| 7 | `applications/video-dct` | 39.714747 | 39.537327–40.151062 | 27.417000 | 1.449× | +44.9% | 8×150ms same-thread ON |
| 8 | `applications/vision-dilation` | 40.576645 | 40.100277–41.461593 | 29.375000 | 1.381× | +38.1% | 8×150ms same-thread ON |
| 9 | `applications/ml-knn` | 30.148343 | 29.834279–30.388322 | 22.917000 | 1.316× | +31.6% | 8×150ms same-thread ON |
| 10 | `applications/graphics-reed-solomon` | 114.128669 | 113.693431–114.779836 | 87.792000 | 1.300× | +30.0% | 8×150ms same-thread ON |
| 11 | `applications/audio-adpcm` | 29.827262 | 29.475295–30.205043 | 23.458000 | 1.272× | +27.2% | 8×150ms same-thread ON |
| 12 | `applications/stencil-wave-equation` | 61.918642 | 61.537504–62.143176 | 66.833000 | 0.926× | -7.4% | 8×150ms same-thread ON |
| 13 | `applications/document-edit-distance` | 72.522073 | 72.130825–73.411502 | 85.167000 | 0.852× | -14.8% | 8×150ms same-thread ON |
| 14 | `applications/compression-lzw` | 341.373097 | 337.999429–343.569160 | 619.084000 | 0.551× | -44.9% | 8×150ms same-thread ON |
| 15 | `applications/geo-polygon-clipping` | 30.503833 | 30.104380–139.779748 | 64.125000 | 0.476× | -52.4% | 8×150ms same-thread ON |
| 16 | `applications/triangle-raster` | 130.494680 | 128.914078–132.564401 | 297.125000 | 0.439× | -56.1% | 8×150ms same-thread ON |
| 17 | `applications/image-median` | 305.425775 | 296.985856–321.653416 | 704.375000 | 0.434× | -56.6% | 8×150ms same-thread ON |

## Paired hour changes

OFF disables, and ON enables, these 5 options in the same frozen binary: `regional-memory-read,dominated-indexed-key,bounded-signed-quotient,wide-reuse-pins,ordinary-shifted-immediate`. This measures their combined switches, not a complete revision-to-revision comparison. Median per-round ratios remain separate from the historical comparator above. Negative change means faster. Rows are ordered from largest slowdown to largest speedup.

| Corpus workload | OFF median µs | ON median µs | Median paired change | Paired change IQR |
|---|---:|---:|---:|---:|
| `applications/compression-lzw` | 340.758704 | 341.373097 | +0.11% | +0.07% to +0.26% |
| `applications/vision-dilation` | 40.537019 | 40.576645 | +0.01% | -0.71% to +1.18% |
| `applications/compiler-register-allocation` | 16.781185 | 16.725354 | -0.30% | -0.37% to +0.01% |
| `applications/ml-knn` | 30.273296 | 30.148343 | -0.32% | -0.86% to -0.00% |
| `applications/language-register-vm` | 11.020734 | 10.986821 | -0.35% | -0.56% to +0.01% |
| `applications/geo-polygon-clipping` | 30.639083 | 30.503833 | -0.62% | -1.28% to +0.59% |
| `applications/image-median` | 320.896280 | 305.425775 | -1.52% | -5.22% to +0.90% |
| `applications/files-glob-match` | 133.869105 | 131.006088 | -2.04% | -2.89% to -1.74% |
| `applications/vision-components` | 37.591971 | 36.279886 | -3.73% | -5.51% to -2.02% |
| `applications/video-optical-flow` | 19.259616 | 18.533638 | -3.89% | -4.00% to -3.75% |
| `applications/stencil-wave-equation` | 64.636905 | 61.918642 | -4.26% | -4.64% to -4.01% |
| `applications/video-dct` | 41.716996 | 39.714747 | -4.46% | -5.28% to -4.13% |
| `applications/graphics-reed-solomon` | 120.682344 | 114.128669 | -5.19% | -5.58% to -5.08% |
| `applications/document-edit-distance` | 77.446497 | 72.522073 | -6.54% | -6.94% to -5.68% |
| `applications/triangle-raster` | 143.015089 | 130.494680 | -8.46% | -9.52% to -7.76% |
| `applications/audio-adpcm` | 36.420125 | 29.827262 | -18.63% | -19.12% to -18.06% |

## Separate source-build comparisons

Four reversed-process 100ms pairs compare the actual pristine hour-start compiler (`c74d21c1`, `/tmp/paired-hour-base-c74d`) with the current default-on compiler (`/tmp/paired-hour-20261011`). Each process supplies its ordinary loop-memory-base ON state. These source-build changes use separate processes; they are not the 16 same-thread switch comparisons above. The ranking retains focused ON medians for those 16 and adds only point-in-polygon from this process protocol.

| Corpus workload | Old compiler median µs | Current candidate median µs | Median process-paired change | Paired change IQR |
|---|---:|---:|---:|---:|
| `applications/compiler-register-allocation` | 16.757789 | 16.726142 | -0.21% | -1.46% to -0.17% |
| `applications/geo-point-in-polygon` | 21.484454 | 21.359996 | -0.34% | -0.62% to -0.11% |
| `applications/vision-components` | 37.109382 | 36.390403 | -1.78% | -2.18% to -1.27% |
| `applications/files-glob-match` | 133.008263 | 129.197591 | -3.42% | -4.63% to -2.38% |
| `applications/graphics-reed-solomon` | 119.711573 | 115.061638 | -3.83% | -4.20% to -3.72% |
| `applications/video-dct` | 41.496446 | 39.703948 | -4.28% | -4.63% to -4.20% |
| `applications/image-median` | 312.592969 | 284.527277 | -8.85% | -10.99% to -6.72% |
| `applications/audio-adpcm` | 36.951979 | 29.634854 | -19.28% | -19.67% to -18.64% |

Ranked 17/17 applications (16 focused plus point-in-polygon); 16 complete same-thread cases and 8 complete source-build cases. No missing value is treated as zero.

Generated 2026-10-11T00:50:07+00:00. Raw input: `hour-20261011-focused-exec.jsonl`, SHA256 `71191dc6f9ebf06a0c4c00aac288642c82290206fc3df851f66fe2e8c10f9c0c`. The original historical ranking is unchanged. OS-thread locking and requested QoS33 do not establish physical-core affinity.

Cross-build raw input: `hour-20261011-cross-build.jsonl`, SHA256 `73f6ccb1d1f0c41b9b67605c18fbfe5c2e39647fd2600cc3d61cf41b9221f15e`.
