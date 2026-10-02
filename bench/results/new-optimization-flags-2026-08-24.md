# Wago optimization toggle matrix: ARM64 and AMD64

> Generated from raw benchmark captures on 2026-08-22. Each catalog optimization was measured explicitly on and off while every other option remained at its host-selected default.

## Executive summary

- Coverage: 11 ARM64 options and 10 AMD64 options, four samples per state.
- Percentages below are **disabled versus enabled**. Positive execution time means disabling made execution slower (the optimization helped); negative means disabling made execution faster.
- This is a broad screening matrix, not automatic deletion authority. Correctness/safety responsibilities, native-code size, static hit counts, and focused reruns still gate removal.

## Environment

| Architecture | Commit | CPU / OS | Go | GOMAXPROCS | Affinity | Benchtime | Samples/state |
|---|---|---|---|---:|---|---:|---:|
| arm64 | `b2f30d71026d` | Darwin Jairus-Tanaka.local 25.5.0 Darwin Kernel Version 25.5.0: Mon Apr 27 20:41:15 PDT 2026; root:xnu-12377.121.6~2/RELEASE_ARM64_T6041 arm64 | `go version go1.26.5 darwin/arm64` | 1 | none | 100ms | 4 |
| amd64 | `b2f30d71026d` | Linux hub 7.0.0-30-generic #30~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Fri Aug  7 13:27:52 UTC 2 x86_64 x86_64 x86_64 GNU/Linux | `go version go1.22.2 linux/amd64` | 1 | 0 | 100ms | 4 |

## Method

1. The source was detached at exact `origin/main` commit `ef129fdbb8201048077eeea484637bd4a628d4cc` in isolated local and remote worktrees.
2. The canonical architecture catalog (`OptimizationInfos`) supplied the inventory; unregistered legacy environment switches were intentionally excluded.
3. Each process selected one option with immutable `RuntimeConfig.WithOptimization(name, state)`. All other options retained host defaults; compilation used one function worker.
4. Each option ran in ABBA-balanced state order over four samples per state. `BenchmarkMatrixCompileFull` measured decode + validate + codegen and reported `ns/op`, `B/op`, and `allocs/op`; `BenchmarkMatrixExec` measured prepared host-to-Wasm calls.
5. Per-workload values are medians of four process samples. Aggregate deltas are geometric means of per-workload off/on ratios, preventing Ruby/esbuild compile latency from numerically drowning small modules.
6. ARM64 used Apple M4 Max with `GOMAXPROCS=1`; AMD64 used Ryzen 7 7800X3D with `GOMAXPROCS=1` and `taskset -c 0`.

## Reading the tables

- `Exec delta`: geometric-mean execution-time change when disabled.
- `Compile delta`: geometric-mean full-compile-time change when disabled.
- `Compile B delta`: geometric-mean bytes allocated per full compile when disabled.
- `Worst exec row`: largest workload slowdown when disabled; it protects optimizations with narrow but material wins from being hidden by a neutral aggregate.
- `Spread`: median within-state max/min spread across the four samples; effects near or below spread should be treated as noise.
- `removal-screen` is deliberately narrow: |exec| <= 0.25%, worst slowdown <= 2%, compile <= +0.5%, compile bytes <= +0.1%. It is only a shortlist.

## ARM64 complete catalog

| Optimization | Selected default | Experimental | Exec delta | Compile delta | Compile B delta | Worst exec row | Exec spread | Triage |
|---|:---:|:---:|---:|---:|---:|---|---:|---|
| `simd-superopt` | on | no | +2.16% | +1.44% | -0.00% | `utf-as-simd.convertN` +80.02% | 8.54% | retain |
| `swar-idioms` | on | no | +1.78% | +0.87% | -0.00% | `swar-pack-parse.runN` +29.61% | 3.12% | retain |
| `interval-region-pins` | on | no | -0.11% | -1.31% | +0.00% | `blake-as.hashN` +6.72% | 3.29% | retain |
| `fcmp-fuse` | on | no | -0.21% | -0.92% | +0.00% | `swar-pack-parse.pack` +2.46% | 1.57% | mixed/noisy |
| `magic-div` | on | no | +0.41% | -0.79% | -0.11% | `swar-pack-parse.pack` +5.03% | 1.50% | retain |
| `shared-trap-body` | on | no | -0.31% | +0.74% | +0.00% | `many_funcs.run` +0.86% | 2.18% | mixed/noisy |
| `shared-adapters` | on | no | -0.16% | +0.25% | -0.00% | `sieve.count` +1.12% | 1.36% | removal-screen |
| `zero-branch` | on | no | +0.26% | +0.14% | -0.00% | `globals.accumulate` +5.97% | 1.38% | retain |
| `mul-add-fuse` | on | no | +0.32% | +0.51% | +0.01% | `globals.accumulate` +4.45% | 1.24% | mixed/noisy |
| `entry-init-elision` | on | no | -0.12% | +0.55% | +0.01% | `xjb-mulhi.mulhi` +3.50% | 2.02% | mixed/noisy |
| `v128-direct-results` | on | no | +0.38% | -0.50% | -0.00% | `blake-as-simd.hashN` +21.73% | 1.69% | retain |

## AMD64 complete catalog

| Optimization | Selected default | Experimental | Exec delta | Compile delta | Compile B delta | Worst exec row | Exec spread | Triage |
|---|:---:|:---:|---:|---:|---:|---|---:|---|
| `simd-superopt` | on | no | +0.13% | +0.77% | -0.00% | `utf-as-simd.validateN` +1.34% | 0.86% | mixed/noisy |
| `swar-idioms` | on | no | +1.41% | +0.16% | -0.00% | `swar-pack-parse.runN` +27.77% | 0.92% | retain |
| `interval-region-pins` | on | no | +0.45% | +0.47% | -0.04% | `blake-as.hashN` +10.21% | 0.81% | retain |
| `fcmp-fuse` | on | no | +0.07% | -0.09% | -0.00% | `swar-pack-parse.parse4` +1.33% | 0.63% | removal-screen |
| `magic-div` | on | no | -0.14% | -0.16% | -0.11% | `xjb-mulhi.mulhi` +0.77% | 0.66% | removal-screen |
| `shared-trap-body` | on | no | +0.02% | +0.02% | +0.00% | `swar-pack-parse.pack` +0.68% | 0.61% | removal-screen |
| `shared-adapters` | on | no | +0.10% | -0.32% | +0.00% | `arith.run` +1.11% | 0.90% | removal-screen |
| `dead-gc-new` | on | no | -0.11% | +0.22% | -0.00% | `utf-as-simd.validateN` +0.77% | 1.02% | removal-screen |
| `gc-ref-facts` | on | no | -0.02% | -4.10% | -3.83% | `utf-as-simd.validateN` +0.81% | 0.67% | removal-screen |
| `gc-native-alloc` | on | no | +0.05% | +0.11% | -0.00% | `swar-pack-parse.pack` +0.65% | 0.87% | removal-screen |

## Mechanical removal screen

These pass the numeric screen only. The interpretation section below must still exclude safety mechanisms, compatibility paths, size-only optimizations, and options with known focused workloads outside this corpus.

| Architecture | Optimization | Exec delta | Worst exec slowdown | Compile delta | Compile B delta |
|---|---|---:|---:|---:|---:|
| arm64 | `shared-adapters` | -0.16% | +1.12% | +0.25% | -0.00% |
| amd64 | `fcmp-fuse` | +0.07% | +1.33% | -0.09% | -0.00% |
| amd64 | `magic-div` | -0.14% | +0.77% | -0.16% | -0.11% |
| amd64 | `shared-trap-body` | +0.02% | +0.68% | +0.02% | +0.00% |
| amd64 | `shared-adapters` | +0.10% | +1.11% | -0.32% | +0.00% |
| amd64 | `dead-gc-new` | -0.11% | +0.77% | +0.22% | -0.00% |
| amd64 | `gc-ref-facts` | -0.02% | +0.81% | -4.10% | -3.83% |
| amd64 | `gc-native-alloc` | +0.05% | +0.65% | +0.11% | -0.00% |

## Detailed per-option results

Each subsection lists aggregate on/off medians plus the five execution rows most helped and most hurt by disabling. Workload deltas use the same off-versus-on sign convention.

### ARM64

#### `simd-superopt` — SIMD superoptimization

Recognize bounded multi-operation simd sequences. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.600 us | 6.743 us | +2.16% | 8.54% |
| Full compile time | 203.808 us | 206.738 us | +1.44% | 10.24% |
| Full compile bytes | 150.17 KiB | 150.17 KiB | -0.00% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.convertN` | +80.02% | optimization helps |
| `utf-as-simd.validateN` | +13.70% | optimization helps |
| `globals.accumulate` | +3.57% | optimization helps |
| `xjb-mulhi.mulhi` | +1.83% | optimization helps |
| `blake-as-simd.hashN` | +1.56% | optimization helps |
| `dispatch.apply` | -3.68% | disabled is faster |
| `swar-pack-parse.pack` | -2.54% | disabled is faster |
| `swar-pack-parse.parse4` | -1.41% | disabled is faster |
| `quicksort.sortN` | -0.83% | disabled is faster |
| `tiny.add` | -0.56% | disabled is faster |

#### `swar-idioms` — SWAR idioms

Recognize bounded open-coded packed-byte algorithms. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.631 us | 6.749 us | +1.78% | 3.12% |
| Full compile time | 207.000 us | 208.802 us | +0.87% | 7.44% |
| Full compile bytes | 150.17 KiB | 150.17 KiB | -0.00% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `swar-pack-parse.runN` | +29.61% | optimization helps |
| `utf-as.convertN` | +16.57% | optimization helps |
| `linked_list.sum` | +5.56% | optimization helps |
| `mandelbrot.render` | +4.84% | optimization helps |
| `branches.classify` | +2.84% | optimization helps |
| `raytrace.render` | -4.07% | disabled is faster |
| `quicksort.sortN` | -1.29% | disabled is faster |
| `json-as.serializeN` | -0.86% | disabled is faster |
| `matmul.run` | -0.28% | disabled is faster |
| `fib_rec.fib` | -0.19% | disabled is faster |

#### `interval-region-pins` — Interval-region pins

Reuse registers across bounded straight-line local lifetimes. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.695 us | 6.688 us | -0.11% | 3.29% |
| Full compile time | 211.299 us | 208.539 us | -1.31% | 8.98% |
| Full compile bytes | 150.17 KiB | 150.17 KiB | +0.00% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `blake-as.hashN` | +6.72% | optimization helps |
| `blake-as-simd.hashN` | +4.32% | optimization helps |
| `linked_list.sum` | +2.99% | optimization helps |
| `branches.classify` | +1.62% | optimization helps |
| `tiny.add` | +1.15% | optimization helps |
| `swar-pack-parse.pack` | -3.78% | disabled is faster |
| `arith.run` | -1.65% | disabled is faster |
| `fib_iter.fib` | -1.46% | disabled is faster |
| `xjb-mulhi.mulhi` | -1.39% | disabled is faster |
| `quicksort.sortN` | -1.36% | disabled is faster |

#### `fcmp-fuse` — Float compare fusion

Fuse ordered floating-point comparisons into conditional branches. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.644 us | 6.630 us | -0.21% | 1.57% |
| Full compile time | 206.540 us | 204.631 us | -0.92% | 5.65% |
| Full compile bytes | 150.17 KiB | 150.17 KiB | +0.00% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `swar-pack-parse.pack` | +2.46% | optimization helps |
| `dispatch.apply` | +0.86% | optimization helps |
| `nbody.step` | +0.54% | optimization helps |
| `blake-as-simd.hashN` | +0.51% | optimization helps |
| `raytrace.render` | +0.46% | optimization helps |
| `globals.accumulate` | -2.06% | disabled is faster |
| `xjb-mulhi.mulhi` | -1.64% | disabled is faster |
| `fib_iter.fib` | -1.29% | disabled is faster |
| `quicksort.sortN` | -1.19% | disabled is faster |
| `sha256.hashN` | -0.73% | disabled is faster |

#### `magic-div` — Magic division

Lower constant integer division through multiply-high sequences. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.622 us | 6.649 us | +0.41% | 1.50% |
| Full compile time | 204.757 us | 203.139 us | -0.79% | 3.39% |
| Full compile bytes | 150.17 KiB | 150.00 KiB | -0.11% | 0.00% |
| Full compile allocations | 382.5 | 377.2 | -1.37% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `swar-pack-parse.pack` | +5.03% | optimization helps |
| `tiny.add` | +4.85% | optimization helps |
| `branches.classify` | +3.57% | optimization helps |
| `xjb-mulhi.mulhi` | +2.92% | optimization helps |
| `dispatch.apply` | +1.50% | optimization helps |
| `globals.accumulate` | -3.60% | disabled is faster |
| `fib_iter.fib` | -1.31% | disabled is faster |
| `json-as.deserializeN` | -1.08% | disabled is faster |
| `linked_list.sum` | -0.74% | disabled is faster |
| `json-as-simd.serializeN` | -0.64% | disabled is faster |

#### `shared-trap-body` — Shared trap bodies

Share repeated cold trap bodies in size-oriented code. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.645 us | 6.624 us | -0.31% | 2.18% |
| Full compile time | 203.545 us | 205.050 us | +0.74% | 4.62% |
| Full compile bytes | 150.17 KiB | 150.17 KiB | +0.00% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `many_funcs.run` | +0.86% | optimization helps |
| `xjb-mulhi.runN` | +0.45% | optimization helps |
| `globals.accumulate` | +0.45% | optimization helps |
| `float.run` | +0.38% | optimization helps |
| `arith.run` | +0.38% | optimization helps |
| `tiny.add` | -2.89% | disabled is faster |
| `swar-pack-parse.pack` | -2.50% | disabled is faster |
| `quicksort.sortN` | -1.99% | disabled is faster |
| `sieve.count` | -0.85% | disabled is faster |
| `swar-pack-parse.parse4` | -0.78% | disabled is faster |

#### `shared-adapters` — Shared adapters

Share byte-identical host adapters in size-oriented code. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.623 us | 6.613 us | -0.16% | 1.36% |
| Full compile time | 204.229 us | 204.738 us | +0.25% | 4.74% |
| Full compile bytes | 150.17 KiB | 150.17 KiB | -0.00% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `sieve.count` | +1.12% | optimization helps |
| `branches.classify` | +1.01% | optimization helps |
| `swar-pack-parse.pack` | +0.56% | optimization helps |
| `float.run` | +0.53% | optimization helps |
| `dispatch.apply` | +0.52% | optimization helps |
| `linked_list.sum` | -2.12% | disabled is faster |
| `swar-pack-parse.parse4` | -2.11% | disabled is faster |
| `globals.accumulate` | -1.58% | disabled is faster |
| `xjb-mulhi.mulhi` | -1.54% | disabled is faster |
| `quicksort.sortN` | -0.67% | disabled is faster |

#### `zero-branch` — Zero branches

Select direct zero-test branches when flags are not live. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.628 us | 6.645 us | +0.26% | 1.38% |
| Full compile time | 206.669 us | 206.958 us | +0.14% | 3.85% |
| Full compile bytes | 150.17 KiB | 150.17 KiB | -0.00% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +5.97% | optimization helps |
| `tiny.add` | +4.34% | optimization helps |
| `json-as-simd.deserializeN` | +2.10% | optimization helps |
| `json-as-simd.serializeN` | +1.74% | optimization helps |
| `json-as.deserializeN` | +1.33% | optimization helps |
| `dispatch.apply` | -1.73% | disabled is faster |
| `json-as.serializeN` | -1.47% | disabled is faster |
| `linked_list.sum` | -1.40% | disabled is faster |
| `branches.classify` | -1.14% | disabled is faster |
| `fannkuch.run` | -0.79% | disabled is faster |

#### `mul-add-fuse` — Multiply-add fusion

Fuse multiply-add and multiply-subtract expressions. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.624 us | 6.645 us | +0.32% | 1.24% |
| Full compile time | 206.477 us | 207.533 us | +0.51% | 4.03% |
| Full compile bytes | 150.17 KiB | 150.18 KiB | +0.01% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +4.45% | optimization helps |
| `swar-pack-parse.parse4` | +3.68% | optimization helps |
| `branches.classify` | +2.17% | optimization helps |
| `swar-pack-parse.pack` | +1.30% | optimization helps |
| `fib_iter.fib` | +0.94% | optimization helps |
| `linked_list.sum` | -1.77% | disabled is faster |
| `raytrace.render` | -0.55% | disabled is faster |
| `spectralnorm.run` | -0.53% | disabled is faster |
| `sha256.hashN` | -0.52% | disabled is faster |
| `dispatch.apply` | -0.43% | disabled is faster |

#### `entry-init-elision` — Entry initialization elision

Skip initialization of locals overwritten before their first read. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.659 us | 6.651 us | -0.12% | 2.02% |
| Full compile time | 207.800 us | 208.948 us | +0.55% | 5.17% |
| Full compile bytes | 150.17 KiB | 150.19 KiB | +0.01% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `xjb-mulhi.mulhi` | +3.50% | optimization helps |
| `blake-as.hashN` | +1.58% | optimization helps |
| `globals.accumulate` | +1.55% | optimization helps |
| `json-as-simd.deserializeN` | +1.03% | optimization helps |
| `json-as.deserializeN` | +0.70% | optimization helps |
| `fannkuch.run` | -3.83% | disabled is faster |
| `swar-pack-parse.parse4` | -2.37% | disabled is faster |
| `linked_list.sum` | -2.36% | disabled is faster |
| `swar-pack-parse.pack` | -2.34% | disabled is faster |
| `branches.classify` | -1.07% | disabled is faster |

#### `v128-direct-results` — Direct vector results

Write vector results directly into eligible pinned locals. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.674 us | 6.699 us | +0.38% | 1.69% |
| Full compile time | 208.226 us | 207.190 us | -0.50% | 4.92% |
| Full compile bytes | 150.17 KiB | 150.17 KiB | -0.00% | 0.00% |
| Full compile allocations | 382.5 | 382.5 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `blake-as-simd.hashN` | +21.73% | optimization helps |
| `utf-as-simd.validateN` | +4.37% | optimization helps |
| `utf-as-simd.convertN` | +2.64% | optimization helps |
| `swar-pack-parse.pack` | +1.59% | optimization helps |
| `branches.classify` | +1.03% | optimization helps |
| `linked_list.sum` | -4.53% | disabled is faster |
| `globals.accumulate` | -4.21% | disabled is faster |
| `quicksort.sortN` | -1.69% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.72% | disabled is faster |
| `swar-pack-parse.parse4` | -0.63% | disabled is faster |

### AMD64

#### `simd-superopt` — SIMD superoptimization

Recognize bounded multi-operation simd sequences. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.561 us | 6.569 us | +0.13% | 0.86% |
| Full compile time | 345.330 us | 347.980 us | +0.77% | 3.13% |
| Full compile bytes | 138.56 KiB | 138.56 KiB | -0.00% | 0.00% |
| Full compile allocations | 475.4 | 475.4 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.validateN` | +1.34% | optimization helps |
| `xjb-mulhi.mulhi` | +1.25% | optimization helps |
| `branches.classify` | +0.55% | optimization helps |
| `swar-pack-parse.pack` | +0.36% | optimization helps |
| `dispatch.apply` | +0.26% | optimization helps |
| `json-as.serializeN` | -1.18% | disabled is faster |
| `swar-pack-parse.parse4` | -0.53% | disabled is faster |
| `spectralnorm.run` | -0.19% | disabled is faster |
| `sieve.count` | -0.17% | disabled is faster |
| `blake-as.hashN` | -0.06% | disabled is faster |

#### `swar-idioms` — SWAR idioms

Recognize bounded open-coded packed-byte algorithms. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.521 us | 6.613 us | +1.41% | 0.92% |
| Full compile time | 346.220 us | 346.770 us | +0.16% | 3.97% |
| Full compile bytes | 138.56 KiB | 138.56 KiB | -0.00% | 0.00% |
| Full compile allocations | 475.4 | 475.4 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `swar-pack-parse.runN` | +27.77% | optimization helps |
| `utf-as.convertN` | +14.29% | optimization helps |
| `xjb-mulhi.mulhi` | +8.58% | optimization helps |
| `blake-as-simd.hashN` | +4.34% | optimization helps |
| `swar-pack-parse.pack` | +1.20% | optimization helps |
| `swar-pack-parse.parse4` | -1.66% | disabled is faster |
| `quicksort.sortN` | -0.79% | disabled is faster |
| `tiny.add` | -0.55% | disabled is faster |
| `json-as-simd.deserializeN` | -0.36% | disabled is faster |
| `linked_list.sum` | -0.36% | disabled is faster |

#### `interval-region-pins` — Interval-region pins

Reuse registers across bounded straight-line local lifetimes. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.489 us | 6.518 us | +0.45% | 0.81% |
| Full compile time | 343.455 us | 345.079 us | +0.47% | 3.65% |
| Full compile bytes | 138.56 KiB | 138.51 KiB | -0.04% | 0.00% |
| Full compile allocations | 475.4 | 474.9 | -0.10% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `blake-as.hashN` | +10.21% | optimization helps |
| `blake-as-simd.hashN` | +6.22% | optimization helps |
| `json-as.serializeN` | +1.22% | optimization helps |
| `tiny.add` | +0.83% | optimization helps |
| `utf-as-simd.validateN` | +0.82% | optimization helps |
| `xjb-mulhi.mulhi` | -1.19% | disabled is faster |
| `arith.run` | -0.45% | disabled is faster |
| `quicksort.sortN` | -0.45% | disabled is faster |
| `mandelbrot.render` | -0.41% | disabled is faster |
| `raytrace.render` | -0.24% | disabled is faster |

#### `fcmp-fuse` — Float compare fusion

Fuse ordered floating-point comparisons into conditional branches. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.469 us | 6.474 us | +0.07% | 0.63% |
| Full compile time | 343.611 us | 343.290 us | -0.09% | 4.10% |
| Full compile bytes | 138.56 KiB | 138.56 KiB | -0.00% | 0.00% |
| Full compile allocations | 475.4 | 475.3 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `swar-pack-parse.parse4` | +1.33% | optimization helps |
| `arith.run` | +1.06% | optimization helps |
| `swar-pack-parse.pack` | +1.03% | optimization helps |
| `raytrace.render` | +0.94% | optimization helps |
| `mandelbrot.render` | +0.27% | optimization helps |
| `json-as.serializeN` | -1.45% | disabled is faster |
| `quicksort.sortN` | -0.41% | disabled is faster |
| `tiny.add` | -0.21% | disabled is faster |
| `memory.sum` | -0.17% | disabled is faster |
| `blake-as.hashN` | -0.15% | disabled is faster |

#### `magic-div` — Magic division

Lower constant integer division through multiply-high sequences. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.465 us | 6.456 us | -0.14% | 0.66% |
| Full compile time | 342.592 us | 342.055 us | -0.16% | 3.30% |
| Full compile bytes | 138.56 KiB | 138.41 KiB | -0.11% | 0.00% |
| Full compile allocations | 475.4 | 471.1 | -0.89% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `xjb-mulhi.mulhi` | +0.77% | optimization helps |
| `utf-as.convertN` | +0.37% | optimization helps |
| `json-as.deserializeN` | +0.36% | optimization helps |
| `nbody.step` | +0.29% | optimization helps |
| `raytrace.render` | +0.20% | optimization helps |
| `dispatch.apply` | -3.16% | disabled is faster |
| `swar-pack-parse.pack` | -0.93% | disabled is faster |
| `branches.classify` | -0.62% | disabled is faster |
| `utf-as-simd.validateN` | -0.56% | disabled is faster |
| `json-as.serializeN` | -0.44% | disabled is faster |

#### `shared-trap-body` — Shared trap bodies

Share repeated cold trap bodies in size-oriented code. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.443 us | 6.445 us | +0.02% | 0.61% |
| Full compile time | 341.906 us | 341.986 us | +0.02% | 3.49% |
| Full compile bytes | 138.56 KiB | 138.56 KiB | +0.00% | 0.00% |
| Full compile allocations | 475.4 | 475.4 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `swar-pack-parse.pack` | +0.68% | optimization helps |
| `json-as.serializeN` | +0.58% | optimization helps |
| `xjb-mulhi.mulhi` | +0.46% | optimization helps |
| `swar-pack-parse.runN` | +0.37% | optimization helps |
| `blake-as.hashN` | +0.29% | optimization helps |
| `tiny.add` | -1.29% | disabled is faster |
| `utf-as-simd.validateN` | -0.71% | disabled is faster |
| `branches.classify` | -0.36% | disabled is faster |
| `memory_tree.run` | -0.22% | disabled is faster |
| `quicksort.sortN` | -0.18% | disabled is faster |

#### `shared-adapters` — Shared adapters

Share byte-identical host adapters in size-oriented code. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.436 us | 6.442 us | +0.10% | 0.90% |
| Full compile time | 341.650 us | 340.544 us | -0.32% | 4.21% |
| Full compile bytes | 138.56 KiB | 138.56 KiB | +0.00% | 0.00% |
| Full compile allocations | 475.4 | 475.4 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `arith.run` | +1.11% | optimization helps |
| `swar-pack-parse.pack` | +1.08% | optimization helps |
| `utf-as.convertN` | +0.73% | optimization helps |
| `tiny.add` | +0.61% | optimization helps |
| `xjb-mulhi.mulhi` | +0.37% | optimization helps |
| `json-as.serializeN` | -0.69% | disabled is faster |
| `linked_list.sum` | -0.36% | disabled is faster |
| `crc32.hashN` | -0.28% | disabled is faster |
| `utf-as-simd.validateN` | -0.26% | disabled is faster |
| `float.run` | -0.16% | disabled is faster |

#### `dead-gc-new` — Dead GC constructors

Remove dropped gc constructor trees while preserving traps. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.437 us | 6.429 us | -0.11% | 1.02% |
| Full compile time | 341.204 us | 341.951 us | +0.22% | 4.58% |
| Full compile bytes | 138.56 KiB | 138.56 KiB | -0.00% | 0.00% |
| Full compile allocations | 475.3 | 475.4 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.validateN` | +0.77% | optimization helps |
| `memory.sum` | +0.25% | optimization helps |
| `linked_list.sum` | +0.21% | optimization helps |
| `many_funcs.run` | +0.21% | optimization helps |
| `float.run` | +0.19% | optimization helps |
| `swar-pack-parse.pack` | -1.23% | disabled is faster |
| `json-as.serializeN` | -0.87% | disabled is faster |
| `tiny.add` | -0.60% | disabled is faster |
| `swar-pack-parse.runN` | -0.40% | disabled is faster |
| `branches.classify` | -0.35% | disabled is faster |

#### `gc-ref-facts` — GC reference facts

Propagate exact reference facts and dependent bounded forwarding proofs. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.428 us | 6.427 us | -0.02% | 0.67% |
| Full compile time | 343.176 us | 329.114 us | -4.10% | 4.18% |
| Full compile bytes | 138.56 KiB | 133.25 KiB | -3.83% | 0.00% |
| Full compile allocations | 475.4 | 420.0 | -11.66% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.validateN` | +0.81% | optimization helps |
| `swar-pack-parse.runN` | +0.52% | optimization helps |
| `arith.run` | +0.46% | optimization helps |
| `tiny.add` | +0.30% | optimization helps |
| `branches.classify` | +0.26% | optimization helps |
| `nbody.step` | -0.72% | disabled is faster |
| `json-as.serializeN` | -0.64% | disabled is faster |
| `swar-pack-parse.pack` | -0.42% | disabled is faster |
| `sha256.hashN` | -0.22% | disabled is faster |
| `memory.sum` | -0.21% | disabled is faster |

#### `gc-native-alloc` — Native GC allocation

Allocate admitted gc objects through native nursery fast paths. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.426 us | 6.429 us | +0.05% | 0.87% |
| Full compile time | 342.172 us | 342.553 us | +0.11% | 4.93% |
| Full compile bytes | 138.56 KiB | 138.56 KiB | -0.00% | 0.00% |
| Full compile allocations | 475.4 | 475.4 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `swar-pack-parse.pack` | +0.65% | optimization helps |
| `many_funcs.run` | +0.65% | optimization helps |
| `xjb-mulhi.mulhi` | +0.48% | optimization helps |
| `arith.run` | +0.46% | optimization helps |
| `crc32.hashN` | +0.34% | optimization helps |
| `utf-as-simd.validateN` | -1.39% | disabled is faster |
| `json-as.serializeN` | -0.88% | disabled is faster |
| `swar-pack-parse.runN` | -0.29% | disabled is faster |
| `nbody.step` | -0.15% | disabled is faster |
| `tiny.add` | -0.11% | disabled is faster |

## Reproduction

The benchmark-only harness and runner are uncommitted files in this isolated worktree:

- `bench/optimization_matrix_bench_test.go`
- `bench/run_optimization_matrix.sh`
- `bench/results/arm64/` and `bench/results/amd64/` raw captures

Representative invocation:

```sh
cd bench
go test -c -o optimization-matrix.test .
./run_optimization_matrix.sh results/arm64
MATRIX_CPU=0 ./run_optimization_matrix.sh results/amd64
```

## Limitations and next gates

- Four 100 ms samples are suitable for broad screening, not sub-percent release claims. Any deletion candidate needs longer focused interleaved reruns.
- The matrix measures compiler heap allocation (`B/op`), not peak RSS. Peak process RSS requires a separate one-shot harness and should be added before removing a mechanism primarily justified by bounded scratch reuse.
- Native-code size is not part of this request's three metrics. Size-objective and code-size-only transformations can look neutral here and must not be deleted without code-byte measurements.
- The executable corpus cannot run Ruby, esbuild, SQLite, Lua, wasm3, or regexmatch because their host environments are absent. They still contribute full compile time and memory.
- This matrix toggles registered optimizations individually. It does not test interactions among multiple disabled options or undocumented legacy environment switches.
- A crash or failure with an option disabled is a compatibility/correctness finding, not evidence that the optimization is fast.

