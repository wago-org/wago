# Wago optimization toggle matrix: ARM64 and AMD64

> Generated from raw benchmark captures on 2026-08-22. Each catalog optimization was measured explicitly on and off while every other option remained at its host-selected default.

## Executive summary

- Coverage: 39 ARM64 options and 38 AMD64 options, four samples per state.
- Fail-closed result: `arm64/stack-reg` (4 nonzero exits; samples on/off=4/4); `amd64/reg-abi` (4 nonzero exits; samples on/off=4/4); `amd64/stack-reg` (4 nonzero exits; samples on/off=4/4).
- The strongest broadly visible execution wins are ARM64 `reg-abi` (+20.42% disabled penalty), AMD64 `v128-const-cache` (+6.09%), branch folding (+2.30% ARM64 / +3.39% AMD64), AMD64 `inline` (+2.27%), and vector pins (+2.56% ARM64 / +1.84% AMD64).
- The clearest cost/benefit review target is `loop-precheck`: disabling it cuts full-compile allocation bytes by 6.38% on ARM64 and 6.09% on AMD64 and improves compile time by 5.36% / 3.05%, while broad execution changes only +0.21% / +0.11%; focused rows still lose as much as 3.72% / 2.88%, so this is a default/removal investigation, not an immediate deletion.
- The cleanest low-consequence implementation candidates are ARM64 `v128-const-cache`, shared `v128-sink`, AMD64 `affine-lea`, AMD64 `call-next-use`, and the default-off experimental `inline-loop-callees`. Each needs focused code-size and hit-count evidence before removal.
- Follow-up implemented: `loop-precheck` and `v128-sink` now default off on both architectures; ARM64 also defaults `deep-fp-pins` off; AMD64 also defaults `call-next-use`, `affine-lea`, `tee-spill-elide`, and `commute-self-update` off. The already-off `inline-loop-callees` override and its unreachable backend paths were removed.
- In a fresh combined-profile rerun, the new defaults changed execution by **+0.14% ARM64 / -0.01% AMD64**, while improving compile time by **8.73% / 7.99%**, compile allocation bytes by **6.38% / 6.09%**, and compile allocation counts by **2.93% / 4.37%**.
- Percentages below are **disabled versus enabled**. Positive execution time means disabling made execution slower (the optimization helped); negative means disabling made execution faster.
- This is a broad screening matrix, not automatic deletion authority. Correctness/safety responsibilities, native-code size, static hit counts, and focused reruns still gate removal.

## Implemented default trim and combined rerun

The follow-up policy makes the low-footprint choice the default without deleting
optimizations that still have narrow wins. Every retained option can be enabled
through the existing runtime/project optimization map.

| Architecture | Newly default-off options | Removed surface |
|---|---|---|
| ARM64 | `deep-fp-pins`, `loop-precheck`, `v128-sink` | `inline-loop-callees` |
| AMD64 | `affine-lea`, `call-next-use`, `commute-self-update`, `loop-precheck`, `tee-spill-elide`, `v128-sink` | `inline-loop-callees` |

The bundle was then measured against the exact former defaults from the same
modified source: the old-default profile explicitly re-enabled every option in
the table above, while the new-default profile used an unmodified
`NewRuntimeConfig`. Four samples per profile ran in ABBA-balanced order with the
same corpus, one compile worker, `GOMAXPROCS=1`, 100 ms benchmark windows, and
AMD64 pinned to CPU 0.

| Architecture | Execution time | Full compile time | Compile B/op | Compile allocs/op | Worst execution row |
|---|---:|---:|---:|---:|---|
| ARM64 | +0.14% | **-8.73%** | **-6.38%** | **-2.93%** | `fannkuch.run` +3.06% |
| AMD64 | -0.01% | **-7.99%** | **-6.09%** | **-4.37%** | `fannkuch.run` +2.85% |

These percentages are **new default versus old default**. The execution geomean
is effectively flat, while the compile-resource reduction is large and repeats
on both machines. The focused `fannkuch` loss is the retained reason to leave
`loop-precheck` available as an explicit opt-in instead of deleting it.

### Follow-on ARM64 vector-cache removal

ARM64 `v128-const-cache` was removed after the default trim. Its matrix result was
execution-neutral (-0.06% when disabled, worst slowdown +0.78%) and disabling it
improved full compile time by 1.36%. The deletion removes the function-body
pre-scan, fixed candidate buffer, reserved-register state and masks, allocator
exclusions, backend binding, and cache-specific tests: 249 implementation/test
lines removed for five small call-site simplifications.

AMD64 keeps `v128-const-cache` default-on and remains the only architecture that
advertises the option. Its measured +6.09% aggregate execution benefit and
+186.23% focused `utf-as-simd.validateN` benefit make that implementation clearly
worth retaining. Making the catalog entry AMD64-only preserves the catalog as the
single source of truth without introducing architecture-dependent defaults for
one shared definition.

### Follow-on ARM64 deep-float-pin removal

ARM64 `deep-fp-pins` was also removed rather than left as a permanent default-off
switch. Disabling it changed aggregate execution by -0.08%, full compile time by
-0.06%, and compile allocation bytes by effectively zero; its worst focused
slowdown was 1.61% on `fib_iter`. The option only reopened the highest V-register
pin range for one call-making signature class. The backend now consistently caps
that class at 23 pinned float registers, and the catalog, manifest/schema, binding,
environment control, and dead conditional path are gone.

## Environment

| Architecture | Commit | CPU / OS | Go | GOMAXPROCS | Affinity | Benchtime | Samples/state |
|---|---|---|---|---:|---|---:|---:|
| arm64 | `ef129fdbb820` | Darwin Jairus-Tanaka.local 25.5.0 Darwin Kernel Version 25.5.0: Mon Apr 27 20:41:15 PDT 2026; root:xnu-12377.121.6~2/RELEASE_ARM64_T6041 arm64 | `go version go1.26.5 darwin/arm64` | 1 | none | 100ms | 4 |
| amd64 | `ef129fdbb820` | Linux hub 7.0.0-28-generic #28~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Wed Jul  1 15:50:57 UTC 2 x86_64 x86_64 x86_64 GNU/Linux | `go version go1.22.2 linux/amd64` | 1 | 0 | 100ms | 4 |

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
| `bounds-facts` | on | no | -0.63% | -0.23% | -0.10% | `json-as-simd.serializeN` +2.81% | 6.09% | mixed/noisy |
| `st-flags` | on | no | -1.69% | +0.25% | +0.05% | `linked_list.sum` +3.67% | 3.77% | mixed/noisy |
| `reg-merge` | on | no | +1.48% | +0.62% | -0.00% | `json-as.deserializeN` +6.74% | 3.21% | retain |
| `tee-sink` | on | no | +1.65% | -1.56% | -0.29% | `blake-as.hashN` +13.36% | 4.04% | retain |
| `unary-sink` | on | no | +0.43% | +0.43% | -0.00% | `linked_list.sum` +2.64% | 3.74% | mixed/noisy |
| `three-op-sink` | on | no | +0.49% | +0.02% | +0.00% | `xjb-mulhi.runN` +8.79% | 3.25% | retain |
| `olddest-rhs-sink` | on | no | +0.42% | -0.12% | -0.00% | `blake-as.hashN` +5.43% | 2.10% | retain |
| `branch-fold` | on | no | +2.30% | -0.93% | +0.01% | `globals.accumulate` +64.72% | 1.62% | retain |
| `store-load-fwd` | on | no | +0.19% | -2.12% | +0.00% | `json-as-simd.serializeN` +6.20% | 1.73% | retain |
| `uxtw-add` | on | no | -0.06% | -0.49% | -0.00% | `linked_list.sum` +6.12% | 1.63% | retain |
| `value-facts` | on | no | +0.11% | +0.06% | -0.01% | `linked_list.sum` +3.96% | 1.60% | mixed/noisy |
| `load-pair` | on | no | +0.14% | +0.22% | -0.01% | `globals.accumulate` +7.99% | 1.38% | retain |
| `entry-param-pairs` | on | no | +0.99% | +0.08% | +0.00% | `crc32.hashN` +2.56% | 4.70% | mixed/noisy |
| `entry-zero-pairs` | on | no | +0.08% | +0.86% | -0.29% | `raytrace.render` +1.93% | 1.45% | mixed/noisy |
| `entry-arg-pins` | on | no | +0.17% | +0.00% | -0.00% | `sha256.hashN` +2.62% | 1.59% | mixed/noisy |
| `x8-pin` | on | no | +0.11% | -0.33% | -0.00% | `linked_list.sum` +2.72% | 1.33% | mixed/noisy |
| `deep-fp-pins` | on | no | -0.08% | -0.06% | -0.00% | `fib_iter.fib` +1.61% | 1.26% | removal-screen |
| `ext-fp-pins` | on | no | +1.83% | +0.11% | +0.00% | `blake-as-simd.hashN` +58.99% | 1.35% | retain |
| `merge-next-use` | on | no | +0.08% | +0.57% | -0.00% | `quicksort.sortN` +0.91% | 1.77% | mixed/noisy |
| `leaf-scratch-pins` | on | no | +0.11% | -1.34% | -0.00% | `globals.accumulate` +3.20% | 1.59% | mixed/noisy |
| `immutable-table` | on | no | -0.07% | -0.04% | -0.02% | `globals.accumulate` +2.49% | 1.34% | mixed/noisy |
| `immutable-table-type` | on | no | -0.08% | -0.58% | -0.02% | `globals.accumulate` +2.25% | 1.30% | mixed/noisy |
| `inline-callfree` | on | no | +0.16% | -0.16% | -0.02% | `xjb-mulhi.mulhi` +1.69% | 1.79% | removal-screen |
| `store-forward` | on | no | +1.49% | +0.30% | +0.00% | `memory_tree.run` +66.54% | 1.84% | retain |
| `frame-elide-reghomed` | on | no | +0.05% | -0.06% | -0.00% | `linked_list.sum` +1.40% | 1.56% | removal-screen |
| `small-frame` | on | no | +0.17% | +0.22% | -0.00% | `linked_list.sum` +3.23% | 1.55% | mixed/noisy |
| `v128-const-cache` | on | no | -0.06% | -1.36% | +0.00% | `branches.classify` +0.78% | 1.38% | removal-screen |
| `v128-pins` | on | no | +2.56% | +0.86% | -0.00% | `blake-as-simd.hashN` +72.54% | 1.74% | retain |
| `v128-sink` | on | no | +0.07% | +0.13% | +0.00% | `crc32.hashN` +1.10% | 1.77% | removal-screen |
| `reg-abi` | on | no | +20.42% | -0.37% | +1.01% | `json-as-simd.serializeN` +131.17% | 1.34% | retain |
| `inline` | on | no | +0.07% | -1.50% | -1.71% | `many_funcs.run` +6.16% | 1.31% | retain |
| `inline-loop-callees` | off | yes | +0.12% | -0.07% | +0.03% | `swar-pack-parse.pack` +2.51% | 2.30% | mixed/noisy |
| `loop-precheck` | on | no | +0.21% | -5.36% | -6.38% | `globals.accumulate` +3.72% | 4.00% | mixed/noisy |
| `loop-region-pins` | off | yes | +6.09% | -0.62% | +0.18% | `utf-as-simd.validateN` +624.17% | 4.15% | retain |
| `immutable-poly-fastpath` | off | yes | -0.10% | +0.09% | +0.11% | `fib_rec.fib` +1.99% | 2.49% | mixed/noisy |
| `legacy-fp-pins` | off | yes | -1.94% | -0.90% | +0.00% | `globals.accumulate` +2.15% | 2.14% | mixed/noisy |
| `legacy-gp-pins` | off | yes | +0.83% | -0.65% | +0.00% | `fib_rec.fib` +3.27% | 3.46% | mixed/noisy |
| `stack-fence` | on | no | -0.66% | -1.40% | -0.19% | `dispatch.apply` +2.20% | 4.30% | mixed/noisy |
| `stack-reg` | on | no | failed | failed | partial | 4 nonzero exits; samples on/off=4/4 | n/a | **failed** |

## AMD64 complete catalog

| Optimization | Selected default | Experimental | Exec delta | Compile delta | Compile B delta | Worst exec row | Exec spread | Triage |
|---|:---:|:---:|---:|---:|---:|---|---:|---|
| `bounds-facts` | on | no | +0.96% | +0.19% | +0.22% | `matmul.run` +13.54% | 1.35% | retain |
| `st-flags` | on | no | -0.06% | -0.31% | +0.00% | `branches.classify` +1.14% | 0.73% | removal-screen |
| `store8-flags` | on | no | +0.05% | +0.01% | +0.00% | `tiny.add` +2.93% | 0.55% | mixed/noisy |
| `reg-merge` | on | no | -0.01% | +0.23% | +0.02% | `linked_list.sum` +3.61% | 0.74% | mixed/noisy |
| `tee-sink` | on | no | +0.38% | +0.39% | -0.00% | `matmul.run` +6.45% | 1.14% | retain |
| `unary-sink` | on | no | -0.03% | +0.05% | +0.00% | `globals.accumulate` +0.35% | 0.76% | removal-screen |
| `branch-fold` | on | no | +3.39% | -0.05% | -0.15% | `globals.accumulate` +48.47% | 0.86% | retain |
| `value-facts` | on | no | +0.12% | -0.28% | -0.00% | `memory_tree.run` +9.64% | 0.74% | retain |
| `entry-arg-pins` | on | no | +0.10% | -0.23% | +0.00% | `utf-as-simd.validateN` +1.57% | 0.75% | removal-screen |
| `ext-fp-pins` | on | no | +1.11% | -0.02% | -0.00% | `utf-as-simd.validateN` +20.40% | 0.83% | retain |
| `call-next-use` | on | no | +0.10% | -0.25% | -0.00% | `json-as-simd.serializeN` +0.84% | 0.69% | removal-screen |
| `affine-lea` | on | no | -0.07% | -0.66% | -0.00% | `memory.sum` +0.60% | 0.84% | removal-screen |
| `tree-order` | on | no | +0.02% | +0.04% | -0.00% | `sha256.hashN` +1.79% | 0.75% | removal-screen |
| `assoc-tree` | on | no | +0.25% | +0.28% | +0.00% | `xjb-mulhi.runN` +6.78% | 0.85% | retain |
| `bmi2-rorx` | on | yes | +0.10% | -0.40% | -0.00% | `sha256.hashN` +4.27% | 0.74% | mixed/noisy |
| `vex-float-mem` | on | no | +0.42% | -0.01% | +0.00% | `matmul.run` +10.53% | 0.70% | retain |
| `multi-bounds-cert` | on | no | +0.35% | +0.09% | +0.02% | `matmul.run` +13.47% | 0.54% | retain |
| `addr-zext-elim` | on | no | +0.11% | -0.08% | -0.00% | `utf-as-simd.validateN` +2.80% | 0.65% | mixed/noisy |
| `immutable-table` | on | no | +0.25% | -0.17% | +0.08% | `dispatch.apply` +5.65% | 0.70% | retain |
| `immutable-table-type` | on | no | +0.09% | -0.11% | -0.03% | `dispatch.apply` +1.30% | 0.67% | removal-screen |
| `inline-callfree` | on | no | +1.85% | -0.07% | -0.00% | `many_funcs.run` +94.01% | 0.49% | retain |
| `store-forward` | on | no | +0.49% | +0.12% | -0.00% | `memory_tree.run` +17.77% | 0.96% | retain |
| `frame-elide` | on | no | +0.19% | +0.16% | -0.00% | `json-as.serializeN` +2.26% | 0.64% | mixed/noisy |
| `compact-i32-frame` | on | no | +0.12% | +0.36% | +0.00% | `json-as-simd.deserializeN` +1.39% | 0.70% | removal-screen |
| `local-slot-order` | on | no | +0.07% | +0.46% | +0.00% | `json-as.serializeN` +1.43% | 0.56% | removal-screen |
| `tee-spill-elide` | on | no | +0.04% | -0.13% | +0.00% | `arith.run` +1.12% | 0.56% | removal-screen |
| `commute-self-update` | on | no | +0.08% | -0.20% | +0.00% | `blake-as.hashN` +1.35% | 0.60% | removal-screen |
| `i64-mask32` | on | no | +0.61% | +0.37% | -0.00% | `xjb-mulhi.runN` +17.13% | 0.71% | retain |
| `accumulator-immediate` | on | no | -0.06% | -0.43% | +0.00% | `branches.classify` +1.11% | 0.69% | removal-screen |
| `v128-const-cache` | on | no | +6.09% | -0.38% | -0.22% | `utf-as-simd.validateN` +186.23% | 0.97% | retain |
| `v128-pins` | on | no | +1.84% | -0.05% | -0.00% | `utf-as-simd.validateN` +72.94% | 0.56% | retain |
| `v128-sink` | on | no | +0.07% | -0.27% | +0.00% | `xjb-mulhi.mulhi` +0.96% | 0.55% | removal-screen |
| `reg-abi` | on | no | failed | failed | partial | 4 nonzero exits; samples on/off=4/4 | n/a | **failed** |
| `inline` | on | no | +2.27% | -1.79% | -2.65% | `many_funcs.run` +122.63% | 0.60% | retain |
| `inline-loop-callees` | off | yes | +0.01% | -0.62% | -0.49% | `blake-as-simd.hashN` +1.75% | 0.47% | removal-screen |
| `loop-precheck` | on | no | +0.11% | -3.05% | -6.09% | `fannkuch.run` +2.88% | 0.61% | mixed/noisy |
| `stack-fence` | on | no | -0.10% | -0.25% | -0.01% | `quicksort.sortN` +12.00% | 0.67% | retain |
| `stack-reg` | on | no | failed | failed | partial | 4 nonzero exits; samples on/off=4/4 | n/a | **failed** |

## Mechanical removal screen

These pass the numeric screen only. The interpretation section below must still exclude safety mechanisms, compatibility paths, size-only optimizations, and options with known focused workloads outside this corpus.

| Architecture | Optimization | Exec delta | Worst exec slowdown | Compile delta | Compile B delta |
|---|---|---:|---:|---:|---:|
| arm64 | `deep-fp-pins` | -0.08% | +1.61% | -0.06% | -0.00% |
| arm64 | `inline-callfree` | +0.16% | +1.69% | -0.16% | -0.02% |
| arm64 | `frame-elide-reghomed` | +0.05% | +1.40% | -0.06% | -0.00% |
| arm64 | `v128-const-cache` | -0.06% | +0.78% | -1.36% | +0.00% |
| arm64 | `v128-sink` | +0.07% | +1.10% | +0.13% | +0.00% |
| amd64 | `st-flags` | -0.06% | +1.14% | -0.31% | +0.00% |
| amd64 | `unary-sink` | -0.03% | +0.35% | +0.05% | +0.00% |
| amd64 | `entry-arg-pins` | +0.10% | +1.57% | -0.23% | +0.00% |
| amd64 | `call-next-use` | +0.10% | +0.84% | -0.25% | -0.00% |
| amd64 | `affine-lea` | -0.07% | +0.60% | -0.66% | -0.00% |
| amd64 | `tree-order` | +0.02% | +1.79% | +0.04% | -0.00% |
| amd64 | `immutable-table-type` | +0.09% | +1.30% | -0.11% | -0.03% |
| amd64 | `compact-i32-frame` | +0.12% | +1.39% | +0.36% | +0.00% |
| amd64 | `local-slot-order` | +0.07% | +1.43% | +0.46% | +0.00% |
| amd64 | `tee-spill-elide` | +0.04% | +1.12% | -0.13% | +0.00% |
| amd64 | `commute-self-update` | +0.08% | +1.35% | -0.20% | +0.00% |
| amd64 | `accumulator-immediate` | -0.06% | +1.11% | -0.43% | +0.00% |
| amd64 | `v128-sink` | +0.07% | +0.96% | -0.27% | +0.00% |
| amd64 | `inline-loop-callees` | +0.01% | +1.75% | -0.62% | -0.49% |

## Interpretation and stripping recommendations

### Fix or withdraw the broken off states

These are more important than the small percentage movements:

- **`stack-reg=false` is not a safe advertised selection on either architecture.** ARM64 reproducibly reaches `fib_rec` and then dies with `runtime: split stack overflow`; AMD64 reaches the memory sequence and dies with a fatal fault at `0x1800000008`. All four disabled processes fail on both hosts. Either implement the non-dedicated-stack-register path completely or remove this option from the public catalog. It cannot currently serve as a differential oracle.
- **`reg-abi=false` is not a safe AMD64 selection for the full supported corpus.** Compilation and 35/36 execution rows complete, but `dispatch.apply` fails on warmup with `wasm trap: tail call target requires an unsupported context switch`. ARM64's disabled state works and shows why the ABI matters: disabling it costs 20.42% at the execution geomean. AMD64 needs a fallback repair or a documented validation rejection for the unsupported combination.

No performance aggregate is reported for a state whose process failed; its compile rows remain in the raw captures only.

### Best candidate: reconsider `loop-precheck`

`loop-precheck` is the only option with a large, repeatable compile-resource bill on both machines and little broad execution consequence:

| Architecture | Disabled exec | Disabled compile time | Disabled compile B/op | Worst focused exec loss |
|---|---:|---:|---:|---:|
| ARM64 | +0.21% | **-5.36%** | **-6.38%** | +3.72% (`globals.accumulate`) |
| AMD64 | +0.11% | **-3.05%** | **-6.09%** | +2.88% (`fannkuch.run`) |

For Wago's low-memory direction, this deserves the first focused follow-up. The likely choices are: make it default-off, reduce its per-function analysis state, or narrow admission to loops where a precheck is proven to remove enough dynamic checks. Immediate deletion would discard real 3–4% focused wins, but the present all-functions cost is disproportionate to the broad aggregate.

### Plausible low-consequence removals

1. **ARM64 `v128-const-cache`.** Disabling is execution-neutral (-0.06%, worst slowdown +0.78%) and improves full compile time by 1.36%, with no compile-memory change. The same feature is emphatically valuable on AMD64 (+6.09% aggregate, +186.23% on `utf-as-simd.validateN`), so only the ARM64 implementation is a candidate.
2. **Shared `v128-sink`.** It is neutral on both architectures (+0.07% aggregate on each; worst disabled penalties +1.10% ARM64 and +0.96% AMD64) and does not reduce compile memory. The static explainer shows this path fires heavily on SIMD code, which makes its lack of measured execution effect notable. Measure native bytes and a longer SIMD-only run; if those are also flat, deleting the sink machinery would remove several target-specific lowering branches and tests.
3. **AMD64 `affine-lea`.** Disabling is execution-neutral (-0.07%, worst +0.60%) and compile-time-positive (-0.66%), with no memory movement. It is a bounded local matcher with focused tests, so removal is contained. Re-run the dedicated affine fixture and record native instruction bytes before deleting it.
4. **AMD64 `call-next-use`.** Disabling changes aggregate execution by +0.10%, worst +0.84%, while improving compile time by 0.25%. The bounded call-liveness scan is more code than the measured result justifies. A static hit count plus a call-heavy focused run should decide it.
5. **Experimental `inline-loop-callees`.** It is default-off. On AMD64, keeping it off is execution-neutral (+0.01% off versus on), 0.62% faster to compile, and 0.49% lower in compile bytes. ARM64 is noisier and has one +2.51% focused difference, but the broad result is +0.12%. If there is no near-term plan to ship it, removing the catalog surface and opt-in path is simpler than carrying a dormant experimental branch.

### Weak candidates, not first cuts

- ARM64 `deep-fp-pins` is neutral, but it is only one conservative admission condition over the existing pin allocator; deleting it saves little implementation complexity.
- AMD64 `tee-spill-elide` and `commute-self-update` are neutral and slightly cheaper when disabled, but each is a small local condition. They are reasonable cleanup only after higher-complexity candidates.
- AMD64 `unary-sink`, `entry-arg-pins`, and `immutable-table-type` pass the mechanical screen, but their shared or structural roles and focused improvements make them poor first removals.

### Do not strip based on this matrix

- **Size-policy work:** `accumulator-immediate`, `compact-i32-frame`, and `local-slot-order` are justified primarily by native-code/frame size. Balanced-mode execution and heap-allocation neutrality do not evaluate their purpose.
- **Core scheduling/fusion:** AMD64 `st-flags` and `tree-order` look flat here, but are shared codegen seams with focused fixtures and interaction with other covers; sub-percent broad numbers alone are insufficient.
- **ARM64 `inline-callfree`:** ARM64 is flat, but AMD64 gains +1.85% broadly and +94.01% on `many_funcs.run`; do not remove the shared concept.
- **ARM64 `frame-elide-reghomed`:** its purpose includes generated frame size and prologue/epilogue removal, which this three-metric matrix does not fully value.
- **Safety:** `stack-fence` is not a discretionary speed peephole. Its neutral aggregate cannot justify removal.

### Retain/high-value signals

- ARM64 `reg-abi`: +20.42% broad disabled penalty and +131.17% on `json-as-simd.serializeN`.
- Branch folding: +2.30% ARM64 / +3.39% AMD64 broadly, with +64.72% / +48.47% on `globals.accumulate`.
- Vector pins: +2.56% ARM64 / +1.84% AMD64, with 70%+ SIMD focused losses when disabled.
- AMD64 vector constant caching: +6.09% broad and +186.23% on `utf-as-simd.validateN`.
- AMD64 inlining: +2.27% broad and +122.63% on `many_funcs.run`, despite costing 1.79% compile time and 2.65% compile bytes.
- ARM64 `store-forward` and AMD64 `store-forward`: only +1.49% / +0.49% broadly, but +66.54% / +17.77% on `memory_tree.run`.

## Detailed per-option results

Each subsection lists aggregate on/off medians plus the five execution rows most helped and most hurt by disabling. Workload deltas use the same off-versus-on sign convention.

### ARM64

#### `bounds-facts` — Bounds facts

Straight-line bounds-check elision. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.718 us | 6.676 us | -0.63% | 6.09% |
| Full compile time | 221.210 us | 220.707 us | -0.23% | 11.84% |
| Full compile bytes | 160.39 KiB | 160.23 KiB | -0.10% | 0.00% |
| Full compile allocations | 394.0 | 394.1 | +0.03% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `json-as-simd.serializeN` | +2.81% | optimization helps |
| `nbody.step` | +2.25% | optimization helps |
| `float.run` | +1.09% | optimization helps |
| `json-as-simd.deserializeN` | +1.06% | optimization helps |
| `json-as.deserializeN` | +0.74% | optimization helps |
| `linked_list.sum` | -4.14% | disabled is faster |
| `fannkuch.run` | -3.88% | disabled is faster |
| `many_funcs.run` | -3.11% | disabled is faster |
| `branches.classify` | -2.64% | disabled is faster |
| `swar-pack-parse.pack` | -2.04% | disabled is faster |

#### `st-flags` — Flags results

Keep comparison results in the flags register. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.977 us | 6.859 us | -1.69% | 3.77% |
| Full compile time | 229.616 us | 230.197 us | +0.25% | 7.60% |
| Full compile bytes | 160.39 KiB | 160.47 KiB | +0.05% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `linked_list.sum` | +3.67% | optimization helps |
| `float.run` | +3.14% | optimization helps |
| `fib_rec.fib` | +1.89% | optimization helps |
| `branches.classify` | +1.67% | optimization helps |
| `utf-as.convertN` | +1.33% | optimization helps |
| `fannkuch.run` | -49.67% | disabled is faster |
| `globals.accumulate` | -4.28% | disabled is faster |
| `swar-pack-parse.pack` | -2.26% | disabled is faster |
| `crc32.hashN` | -1.69% | disabled is faster |
| `raytrace.render` | -1.09% | disabled is faster |

#### `reg-merge` — Register merge

Keep block results in registers across joins. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.910 us | 7.012 us | +1.48% | 3.21% |
| Full compile time | 234.730 us | 236.176 us | +0.62% | 7.09% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.2 | +0.06% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `json-as.deserializeN` | +6.74% | optimization helps |
| `globals.accumulate` | +6.08% | optimization helps |
| `json-as-simd.serializeN` | +5.74% | optimization helps |
| `json-as-simd.deserializeN` | +5.40% | optimization helps |
| `linked_list.sum` | +4.80% | optimization helps |
| `fib_rec.fib` | -1.50% | disabled is faster |
| `fib_iter.fib` | -1.26% | disabled is faster |
| `utf-as-simd.convertN` | -0.90% | disabled is faster |
| `quicksort.sortN` | -0.84% | disabled is faster |
| `matmul.run` | -0.50% | disabled is faster |

#### `tee-sink` — Tee sinking

Sink local.tee expressions into local registers. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.818 us | 6.931 us | +1.65% | 4.04% |
| Full compile time | 232.494 us | 228.865 us | -1.56% | 8.33% |
| Full compile bytes | 160.39 KiB | 159.93 KiB | -0.29% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `blake-as.hashN` | +13.36% | optimization helps |
| `utf-as.convertN` | +8.88% | optimization helps |
| `xjb-mulhi.runN` | +7.94% | optimization helps |
| `spectralnorm.run` | +7.06% | optimization helps |
| `blake-as-simd.hashN` | +5.90% | optimization helps |
| `linked_list.sum` | -2.94% | disabled is faster |
| `json-as.deserializeN` | -2.11% | disabled is faster |
| `fannkuch.run` | -1.64% | disabled is faster |
| `tiny.add` | -1.43% | disabled is faster |
| `branches.classify` | -1.06% | disabled is faster |

#### `unary-sink` — Unary sinking

Sink unary and conversion expressions in place. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.821 us | 6.850 us | +0.43% | 3.74% |
| Full compile time | 227.876 us | 228.850 us | +0.43% | 9.22% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `linked_list.sum` | +2.64% | optimization helps |
| `fib_rec.fib` | +1.48% | optimization helps |
| `globals.accumulate` | +1.44% | optimization helps |
| `utf-as-simd.convertN` | +1.41% | optimization helps |
| `nbody.step` | +1.27% | optimization helps |
| `many_funcs.run` | -0.73% | disabled is faster |
| `json-as-simd.serializeN` | -0.70% | disabled is faster |
| `tiny.add` | -0.55% | disabled is faster |
| `json-as.serializeN` | -0.43% | disabled is faster |
| `branches.classify` | -0.38% | disabled is faster |

#### `three-op-sink` — Three-operand sinking

Sink binary operations into pinned locals. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.844 us | 6.878 us | +0.49% | 3.25% |
| Full compile time | 229.448 us | 229.496 us | +0.02% | 9.19% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | +0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `xjb-mulhi.runN` | +8.79% | optimization helps |
| `linked_list.sum` | +4.99% | optimization helps |
| `utf-as-simd.convertN` | +3.74% | optimization helps |
| `utf-as.convertN` | +3.30% | optimization helps |
| `blake-as.hashN` | +3.17% | optimization helps |
| `globals.accumulate` | -12.80% | disabled is faster |
| `fannkuch.run` | -5.80% | disabled is faster |
| `json-as-simd.serializeN` | -1.65% | disabled is faster |
| `mandelbrot.render` | -1.26% | disabled is faster |
| `swar-pack-parse.pack` | -1.08% | disabled is faster |

#### `olddest-rhs-sink` — Old destination reuse

Reuse an old destination register as the right operand. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.856 us | 6.885 us | +0.42% | 2.10% |
| Full compile time | 227.334 us | 227.064 us | -0.12% | 6.44% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `blake-as.hashN` | +5.43% | optimization helps |
| `blake-as-simd.hashN` | +3.18% | optimization helps |
| `json-as.serializeN` | +1.73% | optimization helps |
| `linked_list.sum` | +1.61% | optimization helps |
| `mandelbrot.render` | +1.00% | optimization helps |
| `quicksort.sortN` | -1.50% | disabled is faster |
| `fib_iter.fib` | -0.46% | disabled is faster |
| `memory_tree.run` | -0.45% | disabled is faster |
| `tiny.add` | -0.35% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.25% | disabled is faster |

#### `branch-fold` — Branch folding

Fold branch pairs into one conditional branch. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.853 us | 7.011 us | +2.30% | 1.62% |
| Full compile time | 227.668 us | 225.552 us | -0.93% | 4.64% |
| Full compile bytes | 160.39 KiB | 160.41 KiB | +0.01% | 0.00% |
| Full compile allocations | 394.0 | 394.3 | +0.08% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +64.72% | optimization helps |
| `fib_iter.fib` | +23.81% | optimization helps |
| `mandelbrot.render` | +6.36% | optimization helps |
| `spectralnorm.run` | +5.62% | optimization helps |
| `matmul.run` | +4.89% | optimization helps |
| `memory.sum` | -3.53% | disabled is faster |
| `sieve.count` | -2.79% | disabled is faster |
| `float.run` | -2.65% | disabled is faster |
| `fannkuch.run` | -1.53% | disabled is faster |
| `xjb-mulhi.mulhi` | -1.16% | disabled is faster |

#### `store-load-fwd` — Store/load forwarding

Forward stores into loads after assembly. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.858 us | 6.871 us | +0.19% | 1.73% |
| Full compile time | 227.186 us | 222.367 us | -2.12% | 4.19% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | +0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `json-as-simd.serializeN` | +6.20% | optimization helps |
| `json-as-simd.deserializeN` | +2.45% | optimization helps |
| `json-as.serializeN` | +1.51% | optimization helps |
| `sieve.count` | +1.01% | optimization helps |
| `json-as.deserializeN` | +0.71% | optimization helps |
| `globals.accumulate` | -2.39% | disabled is faster |
| `tiny.add` | -1.66% | disabled is faster |
| `swar-pack-parse.parse4` | -1.41% | disabled is faster |
| `branches.classify` | -1.22% | disabled is faster |
| `spectralnorm.run` | -0.47% | disabled is faster |

#### `uxtw-add` — Extended adds

Fold zero-extension into add uxtw. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.866 us | 6.862 us | -0.06% | 1.63% |
| Full compile time | 228.892 us | 227.779 us | -0.49% | 4.25% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `linked_list.sum` | +6.12% | optimization helps |
| `crc32.hashN` | +0.89% | optimization helps |
| `sha256.hashN` | +0.57% | optimization helps |
| `quicksort.sortN` | +0.39% | optimization helps |
| `spectralnorm.run` | +0.34% | optimization helps |
| `globals.accumulate` | -1.53% | disabled is faster |
| `swar-pack-parse.parse4` | -1.30% | disabled is faster |
| `arith.run` | -1.01% | disabled is faster |
| `many_funcs.run` | -0.95% | disabled is faster |
| `raytrace.render` | -0.76% | disabled is faster |

#### `value-facts` — Value facts

Propagate bounded upper-zero and boolean provenance. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.852 us | 6.860 us | +0.11% | 1.60% |
| Full compile time | 229.868 us | 230.009 us | +0.06% | 3.93% |
| Full compile bytes | 160.39 KiB | 160.38 KiB | -0.01% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `linked_list.sum` | +3.96% | optimization helps |
| `globals.accumulate` | +0.99% | optimization helps |
| `memory_tree.run` | +0.88% | optimization helps |
| `blake-as-simd.hashN` | +0.80% | optimization helps |
| `swar-pack-parse.pack` | +0.76% | optimization helps |
| `mandelbrot.render` | -0.82% | disabled is faster |
| `raytrace.render` | -0.80% | disabled is faster |
| `tiny.add` | -0.66% | disabled is faster |
| `branches.classify` | -0.42% | disabled is faster |
| `nbody.step` | -0.36% | disabled is faster |

#### `load-pair` — Adjacent load pairs

Combine adjacent full-width scalar loads from one local address into ldp. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.855 us | 6.865 us | +0.14% | 1.38% |
| Full compile time | 228.903 us | 229.412 us | +0.22% | 3.94% |
| Full compile bytes | 160.39 KiB | 160.38 KiB | -0.01% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +7.99% | optimization helps |
| `linked_list.sum` | +1.49% | optimization helps |
| `swar-pack-parse.parse4` | +0.94% | optimization helps |
| `blake-as-simd.hashN` | +0.61% | optimization helps |
| `json-as-simd.serializeN` | +0.45% | optimization helps |
| `sieve.count` | -0.89% | disabled is faster |
| `tiny.add` | -0.70% | disabled is faster |
| `swar-pack-parse.pack` | -0.64% | disabled is faster |
| `fib_rec.fib` | -0.57% | disabled is faster |
| `quicksort.sortN` | -0.50% | disabled is faster |

#### `entry-param-pairs` — Entry parameter pairs

Pair adjacent serialized wrapper parameter homes in function prologues. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.791 us | 6.858 us | +0.99% | 4.70% |
| Full compile time | 229.386 us | 229.572 us | +0.08% | 10.47% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | +0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `crc32.hashN` | +2.56% | optimization helps |
| `spectralnorm.run` | +2.46% | optimization helps |
| `linked_list.sum` | +2.24% | optimization helps |
| `sha256.hashN` | +2.00% | optimization helps |
| `nbody.step` | +1.63% | optimization helps |
| `globals.accumulate` | -3.31% | disabled is faster |
| `tiny.add` | -0.71% | disabled is faster |
| `swar-pack-parse.parse4` | -0.26% | disabled is faster |
| `json-as.serializeN` | +0.11% | optimization helps |
| `swar-pack-parse.pack` | +0.26% | optimization helps |

#### `entry-zero-pairs` — Entry zero pairs

Pair adjacent declared-local zero stores in function prologues. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.638 us | 6.643 us | +0.08% | 1.45% |
| Full compile time | 215.917 us | 217.770 us | +0.86% | 3.55% |
| Full compile bytes | 160.39 KiB | 159.93 KiB | -0.29% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `raytrace.render` | +1.93% | optimization helps |
| `fib_iter.fib` | +1.90% | optimization helps |
| `linked_list.sum` | +1.38% | optimization helps |
| `json-as-simd.deserializeN` | +1.17% | optimization helps |
| `many_funcs.run` | +0.92% | optimization helps |
| `globals.accumulate` | -3.44% | disabled is faster |
| `swar-pack-parse.pack` | -0.78% | disabled is faster |
| `blake-as-simd.hashN` | -0.75% | disabled is faster |
| `branches.classify` | -0.44% | disabled is faster |
| `json-as.serializeN` | -0.43% | disabled is faster |

#### `entry-arg-pins` — Entry argument pins

Keep entry arguments in incoming registers. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.608 us | 6.620 us | +0.17% | 1.59% |
| Full compile time | 217.040 us | 217.041 us | +0.00% | 3.73% |
| Full compile bytes | 160.40 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 393.9 | -0.02% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `sha256.hashN` | +2.62% | optimization helps |
| `globals.accumulate` | +1.71% | optimization helps |
| `swar-pack-parse.parse4` | +1.01% | optimization helps |
| `sieve.count` | +0.83% | optimization helps |
| `quicksort.sortN` | +0.42% | optimization helps |
| `branches.classify` | -0.79% | disabled is faster |
| `crc32.hashN` | -0.70% | disabled is faster |
| `blake-as-simd.hashN` | -0.60% | disabled is faster |
| `json-as-simd.serializeN` | -0.59% | disabled is faster |
| `fib_rec.fib` | -0.40% | disabled is faster |

#### `x8-pin` — X8 scratch pin

Pin a scratch value in call-free functions. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.606 us | 6.613 us | +0.11% | 1.33% |
| Full compile time | 216.838 us | 216.128 us | -0.33% | 3.66% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `linked_list.sum` | +2.72% | optimization helps |
| `json-as-simd.serializeN` | +0.88% | optimization helps |
| `sieve.count` | +0.77% | optimization helps |
| `raytrace.render` | +0.69% | optimization helps |
| `fib_rec.fib` | +0.67% | optimization helps |
| `fannkuch.run` | -2.65% | disabled is faster |
| `globals.accumulate` | -0.93% | disabled is faster |
| `many_funcs.run` | -0.36% | disabled is faster |
| `json-as.deserializeN` | -0.32% | disabled is faster |
| `nbody.step` | -0.26% | disabled is faster |

#### `deep-fp-pins` — Deep float pins

Pin additional float locals in call-free functions. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.606 us | 6.601 us | -0.08% | 1.26% |
| Full compile time | 215.357 us | 215.222 us | -0.06% | 3.75% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `fib_iter.fib` | +1.61% | optimization helps |
| `raytrace.render` | +1.27% | optimization helps |
| `spectralnorm.run` | +0.54% | optimization helps |
| `xjb-mulhi.runN` | +0.53% | optimization helps |
| `quicksort.sortN` | +0.52% | optimization helps |
| `linked_list.sum` | -3.35% | disabled is faster |
| `globals.accumulate` | -2.17% | disabled is faster |
| `swar-pack-parse.parse4` | -0.81% | disabled is faster |
| `json-as-simd.serializeN` | -0.66% | disabled is faster |
| `blake-as.hashN` | -0.54% | disabled is faster |

#### `ext-fp-pins` — Extended float pins

Use the larger floating-point register pool. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.581 us | 6.701 us | +1.83% | 1.35% |
| Full compile time | 215.613 us | 215.845 us | +0.11% | 4.17% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | +0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `blake-as-simd.hashN` | +58.99% | optimization helps |
| `raytrace.render` | +12.76% | optimization helps |
| `many_funcs.run` | +1.57% | optimization helps |
| `nbody.step` | +1.26% | optimization helps |
| `globals.accumulate` | +1.21% | optimization helps |
| `quicksort.sortN` | -0.80% | disabled is faster |
| `blake-as.hashN` | -0.54% | disabled is faster |
| `memory.sum` | -0.51% | disabled is faster |
| `spectralnorm.run` | -0.36% | disabled is faster |
| `linked_list.sum` | -0.32% | disabled is faster |

#### `merge-next-use` — Merge next-use

Keep dead forward-merge locals lazy with bounded post-merge lookahead. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.592 us | 6.597 us | +0.08% | 1.77% |
| Full compile time | 215.101 us | 216.337 us | +0.57% | 4.67% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `quicksort.sortN` | +0.91% | optimization helps |
| `nbody.step` | +0.69% | optimization helps |
| `blake-as-simd.hashN` | +0.63% | optimization helps |
| `sha256.hashN` | +0.62% | optimization helps |
| `xjb-mulhi.mulhi` | +0.61% | optimization helps |
| `globals.accumulate` | -2.98% | disabled is faster |
| `branches.classify` | -0.79% | disabled is faster |
| `json-as-simd.serializeN` | -0.50% | disabled is faster |
| `utf-as-simd.validateN` | -0.24% | disabled is faster |
| `spectralnorm.run` | -0.23% | disabled is faster |

#### `leaf-scratch-pins` — Leaf scratch pins

Pin scratch values in leaf functions. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.594 us | 6.601 us | +0.11% | 1.59% |
| Full compile time | 219.515 us | 216.581 us | -1.34% | 3.95% |
| Full compile bytes | 160.40 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +3.20% | optimization helps |
| `quicksort.sortN` | +1.28% | optimization helps |
| `xjb-mulhi.mulhi` | +0.98% | optimization helps |
| `fib_rec.fib` | +0.65% | optimization helps |
| `sieve.count` | +0.56% | optimization helps |
| `swar-pack-parse.pack` | -1.68% | disabled is faster |
| `json-as.deserializeN` | -0.66% | disabled is faster |
| `branches.classify` | -0.64% | disabled is faster |
| `many_funcs.run` | -0.58% | disabled is faster |
| `fib_iter.fib` | -0.28% | disabled is faster |

#### `immutable-table` — Immutable tables

Specialize calls through never-written tables. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.610 us | 6.605 us | -0.07% | 1.34% |
| Full compile time | 216.791 us | 216.709 us | -0.04% | 3.81% |
| Full compile bytes | 160.39 KiB | 160.36 KiB | -0.02% | 0.00% |
| Full compile allocations | 394.0 | 393.7 | -0.07% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +2.49% | optimization helps |
| `dispatch.apply` | +1.36% | optimization helps |
| `fannkuch.run` | +0.48% | optimization helps |
| `sieve.count` | +0.34% | optimization helps |
| `json-as.deserializeN` | +0.23% | optimization helps |
| `linked_list.sum` | -4.12% | disabled is faster |
| `crc32.hashN` | -0.73% | disabled is faster |
| `spectralnorm.run` | -0.53% | disabled is faster |
| `blake-as-simd.hashN` | -0.47% | disabled is faster |
| `many_funcs.run` | -0.36% | disabled is faster |

#### `immutable-table-type` — Immutable table types

Skip redundant immutable-table type checks. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.610 us | 6.605 us | -0.08% | 1.30% |
| Full compile time | 218.244 us | 216.971 us | -0.58% | 4.26% |
| Full compile bytes | 160.39 KiB | 160.36 KiB | -0.02% | 0.00% |
| Full compile allocations | 394.0 | 393.7 | -0.07% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +2.25% | optimization helps |
| `branches.classify` | +1.07% | optimization helps |
| `swar-pack-parse.parse4` | +0.87% | optimization helps |
| `quicksort.sortN` | +0.84% | optimization helps |
| `crc32.hashN` | +0.58% | optimization helps |
| `linked_list.sum` | -3.75% | disabled is faster |
| `swar-pack-parse.pack` | -1.31% | disabled is faster |
| `json-as-simd.serializeN` | -0.74% | disabled is faster |
| `fib_iter.fib` | -0.66% | disabled is faster |
| `fib_rec.fib` | -0.66% | disabled is faster |

#### `inline-callfree` — Call-free inline hints

Prioritize call-free functions for inlining. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.610 us | 6.620 us | +0.16% | 1.79% |
| Full compile time | 218.559 us | 218.205 us | -0.16% | 4.07% |
| Full compile bytes | 160.39 KiB | 160.36 KiB | -0.02% | 0.00% |
| Full compile allocations | 394.0 | 393.5 | -0.12% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `xjb-mulhi.mulhi` | +1.69% | optimization helps |
| `many_funcs.run` | +1.51% | optimization helps |
| `arith.run` | +1.15% | optimization helps |
| `linked_list.sum` | +1.14% | optimization helps |
| `fib_iter.fib` | +1.09% | optimization helps |
| `globals.accumulate` | -1.28% | disabled is faster |
| `branches.classify` | -1.18% | disabled is faster |
| `sieve.count` | -1.15% | disabled is faster |
| `swar-pack-parse.pack` | -0.81% | disabled is faster |
| `fannkuch.run` | -0.38% | disabled is faster |

#### `store-forward` — Store forwarding

Forward straight-line stores into loads. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.607 us | 6.706 us | +1.49% | 1.84% |
| Full compile time | 218.536 us | 219.186 us | +0.30% | 5.17% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | +0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `memory_tree.run` | +66.54% | optimization helps |
| `globals.accumulate` | +3.24% | optimization helps |
| `fannkuch.run` | +1.69% | optimization helps |
| `dispatch.apply` | +0.84% | optimization helps |
| `json-as.serializeN` | +0.77% | optimization helps |
| `swar-pack-parse.parse4` | -1.38% | disabled is faster |
| `linked_list.sum` | -1.24% | disabled is faster |
| `fib_iter.fib` | -0.94% | disabled is faster |
| `memory.sum` | -0.54% | disabled is faster |
| `spectralnorm.run` | -0.52% | disabled is faster |

#### `frame-elide-reghomed` — Register-homed frames

Omit frames when locals remain in registers. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.599 us | 6.602 us | +0.05% | 1.56% |
| Full compile time | 218.909 us | 218.774 us | -0.06% | 5.01% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `linked_list.sum` | +1.40% | optimization helps |
| `json-as-simd.serializeN` | +0.98% | optimization helps |
| `json-as-simd.deserializeN` | +0.86% | optimization helps |
| `swar-pack-parse.pack` | +0.80% | optimization helps |
| `fannkuch.run` | +0.80% | optimization helps |
| `globals.accumulate` | -1.29% | disabled is faster |
| `swar-pack-parse.parse4` | -0.73% | disabled is faster |
| `tiny.add` | -0.69% | disabled is faster |
| `float.run` | -0.63% | disabled is faster |
| `many_funcs.run` | -0.58% | disabled is faster |

#### `small-frame` — Small frames

Use compact stack adjustment forms. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.598 us | 6.610 us | +0.17% | 1.55% |
| Full compile time | 216.589 us | 217.059 us | +0.22% | 4.72% |
| Full compile bytes | 160.40 KiB | 160.40 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `linked_list.sum` | +3.23% | optimization helps |
| `json-as-simd.deserializeN` | +1.10% | optimization helps |
| `json-as.serializeN` | +1.04% | optimization helps |
| `json-as.deserializeN` | +0.87% | optimization helps |
| `xjb-mulhi.mulhi` | +0.76% | optimization helps |
| `memory.sum` | -1.20% | disabled is faster |
| `fannkuch.run` | -0.76% | disabled is faster |
| `swar-pack-parse.parse4` | -0.68% | disabled is faster |
| `globals.accumulate` | -0.56% | disabled is faster |
| `utf-as-simd.convertN` | -0.36% | disabled is faster |

#### `v128-const-cache` — Vector constant cache

Reserve vector registers for repeated constants. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.569 us | 6.565 us | -0.06% | 1.38% |
| Full compile time | 215.142 us | 212.225 us | -1.36% | 4.10% |
| Full compile bytes | 160.40 KiB | 160.40 KiB | +0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `branches.classify` | +0.78% | optimization helps |
| `fib_rec.fib` | +0.77% | optimization helps |
| `json-as-simd.serializeN` | +0.65% | optimization helps |
| `fib_iter.fib` | +0.63% | optimization helps |
| `json-as.deserializeN` | +0.56% | optimization helps |
| `linked_list.sum` | -3.26% | disabled is faster |
| `globals.accumulate` | -1.96% | disabled is faster |
| `tiny.add` | -0.72% | disabled is faster |
| `dispatch.apply` | -0.49% | disabled is faster |
| `json-as-simd.deserializeN` | -0.45% | disabled is faster |

#### `v128-pins` — Vector pins

Pin hot vector locals in registers. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.606 us | 6.776 us | +2.56% | 1.74% |
| Full compile time | 215.363 us | 217.217 us | +0.86% | 4.19% |
| Full compile bytes | 160.39 KiB | 160.38 KiB | -0.00% | 0.00% |
| Full compile allocations | 394.0 | 393.8 | -0.05% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `blake-as-simd.hashN` | +72.54% | optimization helps |
| `utf-as-simd.validateN` | +28.75% | optimization helps |
| `utf-as-simd.convertN` | +5.56% | optimization helps |
| `linked_list.sum` | +2.49% | optimization helps |
| `fib_iter.fib` | +1.27% | optimization helps |
| `globals.accumulate` | -0.94% | disabled is faster |
| `branches.classify` | -0.89% | disabled is faster |
| `sieve.count` | -0.88% | disabled is faster |
| `fannkuch.run` | -0.37% | disabled is faster |
| `many_funcs.run` | -0.17% | disabled is faster |

#### `v128-sink` — Vector sinking

Sink vector operations into pinned locals. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.630 us | 6.635 us | +0.07% | 1.77% |
| Full compile time | 216.103 us | 216.393 us | +0.13% | 3.72% |
| Full compile bytes | 160.39 KiB | 160.40 KiB | +0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `crc32.hashN` | +1.10% | optimization helps |
| `quicksort.sortN` | +1.07% | optimization helps |
| `utf-as-simd.validateN` | +0.97% | optimization helps |
| `blake-as-simd.hashN` | +0.91% | optimization helps |
| `json-as.deserializeN` | +0.80% | optimization helps |
| `globals.accumulate` | -2.26% | disabled is faster |
| `swar-pack-parse.pack` | -1.66% | disabled is faster |
| `fib_iter.fib` | -0.82% | disabled is faster |
| `swar-pack-parse.parse4` | -0.63% | disabled is faster |
| `many_funcs.run` | -0.41% | disabled is faster |

#### `reg-abi` — Register ABI

Use wago's internal register calling convention. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.629 us | 7.983 us | +20.42% | 1.34% |
| Full compile time | 218.676 us | 217.860 us | -0.37% | 4.99% |
| Full compile bytes | 160.39 KiB | 162.02 KiB | +1.01% | 0.00% |
| Full compile allocations | 394.0 | 395.1 | +0.29% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `json-as-simd.serializeN` | +131.17% | optimization helps |
| `json-as-simd.deserializeN` | +90.55% | optimization helps |
| `json-as.serializeN` | +75.82% | optimization helps |
| `json-as.deserializeN` | +73.12% | optimization helps |
| `globals.accumulate` | +51.30% | optimization helps |
| `linked_list.sum` | -1.15% | disabled is faster |
| `mandelbrot.render` | -0.60% | disabled is faster |
| `nbody.step` | -0.37% | disabled is faster |
| `sieve.count` | -0.35% | disabled is faster |
| `swar-pack-parse.runN` | -0.12% | disabled is faster |

#### `inline` — Inlining

Inline eligible callees. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.642 us | 6.646 us | +0.07% | 1.31% |
| Full compile time | 216.584 us | 213.336 us | -1.50% | 3.31% |
| Full compile bytes | 160.39 KiB | 157.64 KiB | -1.71% | 0.00% |
| Full compile allocations | 394.0 | 390.0 | -1.02% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `many_funcs.run` | +6.16% | optimization helps |
| `xjb-mulhi.mulhi` | +0.78% | optimization helps |
| `fib_iter.fib` | +0.75% | optimization helps |
| `swar-pack-parse.parse4` | +0.68% | optimization helps |
| `branches.classify` | +0.64% | optimization helps |
| `globals.accumulate` | -3.17% | disabled is faster |
| `linked_list.sum` | -1.47% | disabled is faster |
| `crc32.hashN` | -1.09% | disabled is faster |
| `swar-pack-parse.pack` | -0.96% | disabled is faster |
| `sha256.hashN` | -0.42% | disabled is faster |

#### `inline-loop-callees` — Loop-call inlining

Inline callees invoked from inside loops. Selected default: **off**; experimental: **yes**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.648 us | 6.656 us | +0.12% | 2.30% |
| Full compile time | 217.612 us | 217.461 us | -0.07% | 3.16% |
| Full compile bytes | 160.35 KiB | 160.39 KiB | +0.03% | 0.00% |
| Full compile allocations | 398.2 | 394.0 | -1.06% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `swar-pack-parse.pack` | +2.51% | optimization helps |
| `linked_list.sum` | +1.94% | optimization helps |
| `sieve.count` | +1.08% | optimization helps |
| `xjb-mulhi.mulhi` | +0.65% | optimization helps |
| `json-as-simd.serializeN` | +0.57% | optimization helps |
| `globals.accumulate` | -1.40% | disabled is faster |
| `many_funcs.run` | -0.51% | disabled is faster |
| `json-as.serializeN` | -0.42% | disabled is faster |
| `fib_rec.fib` | -0.38% | disabled is faster |
| `utf-as-simd.validateN` | -0.29% | disabled is faster |

#### `loop-precheck` — Loop prechecks

Hoist invariant bounds checks before loops. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.849 us | 6.863 us | +0.21% | 4.00% |
| Full compile time | 230.208 us | 217.869 us | -5.36% | 9.82% |
| Full compile bytes | 160.39 KiB | 150.17 KiB | -6.38% | 0.00% |
| Full compile allocations | 394.0 | 382.5 | -2.93% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +3.72% | optimization helps |
| `memory_tree.run` | +2.50% | optimization helps |
| `json-as-simd.serializeN` | +1.75% | optimization helps |
| `mandelbrot.render` | +1.61% | optimization helps |
| `fib_rec.fib` | +1.29% | optimization helps |
| `linked_list.sum` | -2.74% | disabled is faster |
| `sieve.count` | -1.50% | disabled is faster |
| `nbody.step` | -1.45% | disabled is faster |
| `json-as.deserializeN` | -1.35% | disabled is faster |
| `raytrace.render` | -1.28% | disabled is faster |

#### `loop-region-pins` — Loop-region pins

Pin loop-carried values across loop regions. Selected default: **off**; experimental: **yes**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.549 us | 6.948 us | +6.09% | 4.15% |
| Full compile time | 235.996 us | 234.529 us | -0.62% | 8.01% |
| Full compile bytes | 160.11 KiB | 160.39 KiB | +0.18% | 0.00% |
| Full compile allocations | 398.5 | 394.0 | -1.13% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.validateN` | +624.17% | optimization helps |
| `utf-as-simd.convertN` | +31.99% | optimization helps |
| `globals.accumulate` | +4.33% | optimization helps |
| `swar-pack-parse.runN` | +3.56% | optimization helps |
| `json-as.deserializeN` | +3.31% | optimization helps |
| `json-as-simd.deserializeN` | -5.38% | disabled is faster |
| `float.run` | -5.26% | disabled is faster |
| `json-as.serializeN` | -2.50% | disabled is faster |
| `quicksort.sortN` | -2.26% | disabled is faster |
| `memory.sum` | -2.19% | disabled is faster |

#### `immutable-poly-fastpath` — Polymorphic table fast path

Specialize polymorphic immutable-table calls. Selected default: **off**; experimental: **yes**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.833 us | 6.826 us | -0.10% | 2.49% |
| Full compile time | 227.835 us | 228.037 us | +0.09% | 7.10% |
| Full compile bytes | 160.21 KiB | 160.39 KiB | +0.11% | 0.00% |
| Full compile allocations | 391.9 | 394.0 | +0.52% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `fib_rec.fib` | +1.99% | optimization helps |
| `globals.accumulate` | +1.68% | optimization helps |
| `swar-pack-parse.pack` | +1.05% | optimization helps |
| `fib_iter.fib` | +1.03% | optimization helps |
| `spectralnorm.run` | +0.54% | optimization helps |
| `linked_list.sum` | -2.82% | disabled is faster |
| `blake-as-simd.hashN` | -1.32% | disabled is faster |
| `fannkuch.run` | -1.16% | disabled is faster |
| `tiny.add` | -0.89% | disabled is faster |
| `json-as-simd.deserializeN` | -0.58% | disabled is faster |

#### `legacy-fp-pins` — Legacy float pins

Use the legacy floating-point pin allocator. Selected default: **off**; experimental: **yes**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.966 us | 6.831 us | -1.94% | 2.14% |
| Full compile time | 229.315 us | 227.254 us | -0.90% | 6.39% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | +0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +2.15% | optimization helps |
| `quicksort.sortN` | +1.76% | optimization helps |
| `linked_list.sum` | +1.65% | optimization helps |
| `swar-pack-parse.runN` | +1.00% | optimization helps |
| `matmul.run` | +0.93% | optimization helps |
| `blake-as-simd.hashN` | -37.14% | disabled is faster |
| `raytrace.render` | -18.12% | disabled is faster |
| `nbody.step` | -8.07% | disabled is faster |
| `utf-as-simd.validateN` | -3.65% | disabled is faster |
| `xjb-mulhi.mulhi` | -1.35% | disabled is faster |

#### `legacy-gp-pins` — Legacy integer pins

Use the legacy integer pin allocator. Selected default: **off**; experimental: **yes**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.808 us | 6.864 us | +0.83% | 3.46% |
| Full compile time | 230.713 us | 229.223 us | -0.65% | 9.26% |
| Full compile bytes | 160.39 KiB | 160.39 KiB | +0.00% | 0.00% |
| Full compile allocations | 394.0 | 394.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `fib_rec.fib` | +3.27% | optimization helps |
| `fannkuch.run` | +2.34% | optimization helps |
| `memory_tree.run` | +2.27% | optimization helps |
| `quicksort.sortN` | +2.13% | optimization helps |
| `json-as-simd.serializeN` | +1.73% | optimization helps |
| `raytrace.render` | -2.18% | disabled is faster |
| `tiny.add` | -0.38% | disabled is faster |
| `json-as.serializeN` | -0.33% | disabled is faster |
| `arith.run` | -0.10% | disabled is faster |
| `branches.classify` | +0.05% | optimization helps |

#### `stack-fence` — Stack fence

Emit the stack-overflow guard fence. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.835 us | 6.790 us | -0.66% | 4.30% |
| Full compile time | 231.407 us | 228.168 us | -1.40% | 9.02% |
| Full compile bytes | 160.39 KiB | 160.08 KiB | -0.19% | 0.00% |
| Full compile allocations | 394.0 | 393.1 | -0.23% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `dispatch.apply` | +2.20% | optimization helps |
| `raytrace.render` | +1.80% | optimization helps |
| `sha256.hashN` | +0.70% | optimization helps |
| `utf-as-simd.validateN` | +0.55% | optimization helps |
| `swar-pack-parse.parse4` | +0.42% | optimization helps |
| `json-as-simd.serializeN` | -5.95% | disabled is faster |
| `globals.accumulate` | -2.49% | disabled is faster |
| `fib_rec.fib` | -1.95% | disabled is faster |
| `swar-pack-parse.runN` | -1.64% | disabled is faster |
| `quicksort.sortN` | -1.49% | disabled is faster |

#### `stack-reg` — Stack register

Keep the guest stack pointer in a register. Selected default: **on**; experimental: **no**; triage: **failed**.

Measurement failed: 4 nonzero exits; samples on/off=4/4. Partial output is retained in the raw capture directory and excluded from aggregates.

### AMD64

#### `bounds-facts` — Bounds facts

Straight-line bounds-check elision. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.711 us | 6.775 us | +0.96% | 1.35% |
| Full compile time | 346.134 us | 346.789 us | +0.19% | 4.04% |
| Full compile bytes | 147.54 KiB | 147.86 KiB | +0.22% | 0.00% |
| Full compile allocations | 497.0 | 497.1 | +0.02% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `matmul.run` | +13.54% | optimization helps |
| `linked_list.sum` | +6.76% | optimization helps |
| `json-as.deserializeN` | +3.16% | optimization helps |
| `json-as-simd.serializeN` | +2.90% | optimization helps |
| `json-as.serializeN` | +2.74% | optimization helps |
| `arith.run` | -0.99% | disabled is faster |
| `many_funcs.run` | -0.42% | disabled is faster |
| `branches.classify` | -0.24% | disabled is faster |
| `globals.accumulate` | -0.16% | disabled is faster |
| `utf-as-simd.convertN` | -0.14% | disabled is faster |

#### `st-flags` — Flags results

Keep comparison results in the flags register. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.709 us | 6.705 us | -0.06% | 0.73% |
| Full compile time | 347.531 us | 346.439 us | -0.31% | 3.87% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `branches.classify` | +1.14% | optimization helps |
| `quicksort.sortN` | +0.39% | optimization helps |
| `json-as.deserializeN` | +0.27% | optimization helps |
| `raytrace.render` | +0.26% | optimization helps |
| `utf-as-simd.validateN` | +0.19% | optimization helps |
| `tiny.add` | -1.42% | disabled is faster |
| `utf-as.convertN` | -1.34% | disabled is faster |
| `json-as.serializeN` | -1.11% | disabled is faster |
| `spectralnorm.run` | -0.39% | disabled is faster |
| `sieve.count` | -0.23% | disabled is faster |

#### `store8-flags` — Byte-store flags

Sink comparison results directly into byte stores. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.675 us | 6.679 us | +0.05% | 0.55% |
| Full compile time | 346.254 us | 346.291 us | +0.01% | 4.60% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.0 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `tiny.add` | +2.93% | optimization helps |
| `json-as.serializeN` | +0.51% | optimization helps |
| `raytrace.render` | +0.46% | optimization helps |
| `sieve.count` | +0.21% | optimization helps |
| `matmul.run` | +0.20% | optimization helps |
| `utf-as.convertN` | -0.80% | disabled is faster |
| `crc32.hashN` | -0.34% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.31% | disabled is faster |
| `xjb-mulhi.runN` | -0.27% | disabled is faster |
| `memory_tree.run` | -0.27% | disabled is faster |

#### `reg-merge` — Register merge

Keep block results in registers across joins. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.667 us | 6.667 us | -0.01% | 0.74% |
| Full compile time | 345.643 us | 346.454 us | +0.23% | 4.49% |
| Full compile bytes | 147.54 KiB | 147.57 KiB | +0.02% | 0.00% |
| Full compile allocations | 497.0 | 499.1 | +0.42% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `linked_list.sum` | +3.61% | optimization helps |
| `json-as.serializeN` | +0.93% | optimization helps |
| `json-as-simd.serializeN` | +0.77% | optimization helps |
| `blake-as.hashN` | +0.73% | optimization helps |
| `float.run` | +0.43% | optimization helps |
| `xjb-mulhi.mulhi` | -2.08% | disabled is faster |
| `json-as-simd.deserializeN` | -1.44% | disabled is faster |
| `tiny.add` | -1.16% | disabled is faster |
| `branches.classify` | -0.94% | disabled is faster |
| `sieve.count` | -0.57% | disabled is faster |

#### `tee-sink` — Tee sinking

Sink local.tee expressions into local registers. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.649 us | 6.674 us | +0.38% | 1.14% |
| Full compile time | 344.846 us | 346.190 us | +0.39% | 4.10% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.0 | -0.01% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `matmul.run` | +6.45% | optimization helps |
| `swar-pack-parse.runN` | +5.81% | optimization helps |
| `xjb-mulhi.runN` | +5.57% | optimization helps |
| `sha256.hashN` | +3.79% | optimization helps |
| `utf-as.convertN` | +3.71% | optimization helps |
| `blake-as.hashN` | -7.79% | disabled is faster |
| `blake-as-simd.hashN` | -7.30% | disabled is faster |
| `crc32.hashN` | -1.67% | disabled is faster |
| `branches.classify` | -0.79% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.34% | disabled is faster |

#### `unary-sink` — Unary sinking

Sink unary and conversion expressions in place. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.635 us | 6.633 us | -0.03% | 0.76% |
| Full compile time | 344.654 us | 344.819 us | +0.05% | 3.75% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +0.35% | optimization helps |
| `utf-as-simd.validateN` | +0.34% | optimization helps |
| `raytrace.render` | +0.29% | optimization helps |
| `spectralnorm.run` | +0.28% | optimization helps |
| `branches.classify` | +0.26% | optimization helps |
| `json-as.serializeN` | -0.66% | disabled is faster |
| `many_funcs.run` | -0.55% | disabled is faster |
| `mandelbrot.render` | -0.50% | disabled is faster |
| `quicksort.sortN` | -0.41% | disabled is faster |
| `blake-as-simd.hashN` | -0.25% | disabled is faster |

#### `branch-fold` — Branch folding

Fold branch pairs into one conditional branch. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.631 us | 6.856 us | +3.39% | 0.86% |
| Full compile time | 344.106 us | 343.919 us | -0.05% | 3.32% |
| Full compile bytes | 147.54 KiB | 147.32 KiB | -0.15% | 0.00% |
| Full compile allocations | 497.1 | 492.2 | -0.97% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `globals.accumulate` | +48.47% | optimization helps |
| `fib_iter.fib` | +32.30% | optimization helps |
| `linked_list.sum` | +19.49% | optimization helps |
| `arith.run` | +14.74% | optimization helps |
| `sieve.count` | +6.67% | optimization helps |
| `tiny.add` | -1.96% | disabled is faster |
| `utf-as-simd.validateN` | -1.63% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.53% | disabled is faster |
| `float.run` | -0.48% | disabled is faster |
| `utf-as.convertN` | -0.47% | disabled is faster |

#### `value-facts` — Value facts

Propagate bounded upper-zero and boolean provenance. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.621 us | 6.629 us | +0.12% | 0.74% |
| Full compile time | 344.598 us | 343.626 us | -0.28% | 3.10% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.1 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `memory_tree.run` | +9.64% | optimization helps |
| `json-as.serializeN` | +0.84% | optimization helps |
| `float.run` | +0.19% | optimization helps |
| `fannkuch.run` | +0.12% | optimization helps |
| `utf-as.convertN` | +0.12% | optimization helps |
| `tiny.add` | -2.35% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.81% | disabled is faster |
| `quicksort.sortN` | -0.49% | disabled is faster |
| `utf-as-simd.validateN` | -0.43% | disabled is faster |
| `json-as-simd.deserializeN` | -0.37% | disabled is faster |

#### `entry-arg-pins` — Entry argument pins

Keep entry arguments in incoming registers. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.611 us | 6.618 us | +0.10% | 0.75% |
| Full compile time | 344.760 us | 343.983 us | -0.23% | 3.67% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.validateN` | +1.57% | optimization helps |
| `xjb-mulhi.mulhi` | +1.17% | optimization helps |
| `sieve.count` | +0.68% | optimization helps |
| `json-as.serializeN` | +0.63% | optimization helps |
| `fannkuch.run` | +0.28% | optimization helps |
| `nbody.step` | -0.44% | disabled is faster |
| `utf-as.convertN` | -0.42% | disabled is faster |
| `globals.accumulate` | -0.25% | disabled is faster |
| `swar-pack-parse.parse4` | -0.15% | disabled is faster |
| `json-as.deserializeN` | -0.14% | disabled is faster |

#### `ext-fp-pins` — Extended float pins

Use the larger floating-point register pool. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.609 us | 6.682 us | +1.11% | 0.83% |
| Full compile time | 344.036 us | 343.964 us | -0.02% | 3.90% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.2 | +0.02% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.validateN` | +20.40% | optimization helps |
| `nbody.step` | +10.04% | optimization helps |
| `raytrace.render` | +6.87% | optimization helps |
| `blake-as-simd.hashN` | +3.10% | optimization helps |
| `mandelbrot.render` | +1.25% | optimization helps |
| `quicksort.sortN` | -0.70% | disabled is faster |
| `sha256.hashN` | -0.35% | disabled is faster |
| `json-as-simd.serializeN` | -0.28% | disabled is faster |
| `json-as.deserializeN` | -0.26% | disabled is faster |
| `utf-as.convertN` | -0.25% | disabled is faster |

#### `call-next-use` — Call next-use

Skip dead pinned-local stores before calls. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.609 us | 6.616 us | +0.10% | 0.69% |
| Full compile time | 343.374 us | 342.499 us | -0.25% | 3.68% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `json-as-simd.serializeN` | +0.84% | optimization helps |
| `json-as.serializeN` | +0.79% | optimization helps |
| `quicksort.sortN` | +0.44% | optimization helps |
| `fib_rec.fib` | +0.40% | optimization helps |
| `crc32.hashN` | +0.29% | optimization helps |
| `json-as-simd.deserializeN` | -0.61% | disabled is faster |
| `fannkuch.run` | -0.47% | disabled is faster |
| `float.run` | -0.28% | disabled is faster |
| `utf-as-simd.convertN` | -0.20% | disabled is faster |
| `dispatch.apply` | -0.11% | disabled is faster |

#### `affine-lea` — Affine LEA

Fold bounded affine index trees into scaled addressing. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.609 us | 6.604 us | -0.07% | 0.84% |
| Full compile time | 345.297 us | 343.006 us | -0.66% | 4.10% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.0 | -0.01% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `memory.sum` | +0.60% | optimization helps |
| `many_funcs.run` | +0.28% | optimization helps |
| `crc32.hashN` | +0.25% | optimization helps |
| `swar-pack-parse.runN` | +0.25% | optimization helps |
| `sieve.count` | +0.21% | optimization helps |
| `json-as-simd.deserializeN` | -1.56% | disabled is faster |
| `tiny.add` | -0.80% | disabled is faster |
| `float.run` | -0.49% | disabled is faster |
| `spectralnorm.run` | -0.39% | disabled is faster |
| `fannkuch.run` | -0.24% | disabled is faster |

#### `tree-order` — Valent tree ordering

Schedule bounded commutative trees by register need. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.602 us | 6.604 us | +0.02% | 0.75% |
| Full compile time | 342.781 us | 342.917 us | +0.04% | 4.01% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.0 | -0.01% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `sha256.hashN` | +1.79% | optimization helps |
| `tiny.add` | +1.48% | optimization helps |
| `many_funcs.run` | +0.65% | optimization helps |
| `quicksort.sortN` | +0.55% | optimization helps |
| `json-as-simd.serializeN` | +0.38% | optimization helps |
| `json-as-simd.deserializeN` | -2.12% | disabled is faster |
| `globals.accumulate` | -0.44% | disabled is faster |
| `arith.run` | -0.37% | disabled is faster |
| `crc32.hashN` | -0.28% | disabled is faster |
| `utf-as-simd.validateN` | -0.26% | disabled is faster |

#### `assoc-tree` — Associative tree cover

Cover high-pressure bounded associative trees with one accumulator. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.603 us | 6.619 us | +0.25% | 0.85% |
| Full compile time | 342.689 us | 343.649 us | +0.28% | 3.57% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `xjb-mulhi.runN` | +6.78% | optimization helps |
| `sha256.hashN` | +2.95% | optimization helps |
| `tiny.add` | +1.88% | optimization helps |
| `mandelbrot.render` | +0.82% | optimization helps |
| `quicksort.sortN` | +0.60% | optimization helps |
| `xjb-mulhi.mulhi` | -1.36% | disabled is faster |
| `many_funcs.run` | -1.22% | disabled is faster |
| `branches.classify` | -1.20% | disabled is faster |
| `json-as.serializeN` | -0.62% | disabled is faster |
| `globals.accumulate` | -0.29% | disabled is faster |

#### `bmi2-rorx` — BMI2 rotates

Use non-destructive immediate rotates on bmi2 hosts. Selected default: **on**; experimental: **yes**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.598 us | 6.605 us | +0.10% | 0.74% |
| Full compile time | 344.210 us | 342.832 us | -0.40% | 3.58% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.1 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `sha256.hashN` | +4.27% | optimization helps |
| `tiny.add` | +1.28% | optimization helps |
| `quicksort.sortN` | +0.77% | optimization helps |
| `fib_iter.fib` | +0.12% | optimization helps |
| `fib_rec.fib` | +0.10% | optimization helps |
| `float.run` | -0.57% | disabled is faster |
| `blake-as-simd.hashN` | -0.51% | disabled is faster |
| `utf-as.convertN` | -0.42% | disabled is faster |
| `blake-as.hashN` | -0.36% | disabled is faster |
| `json-as.deserializeN` | -0.32% | disabled is faster |

#### `vex-float-mem` — VEX memory operands

Fold scalar float loads into avx operations. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.598 us | 6.625 us | +0.42% | 0.70% |
| Full compile time | 343.240 us | 343.211 us | -0.01% | 4.07% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `matmul.run` | +10.53% | optimization helps |
| `nbody.step` | +2.70% | optimization helps |
| `branches.classify` | +1.36% | optimization helps |
| `xjb-mulhi.mulhi` | +1.01% | optimization helps |
| `swar-pack-parse.runN` | +0.36% | optimization helps |
| `linked_list.sum` | -0.34% | disabled is faster |
| `utf-as.convertN` | -0.26% | disabled is faster |
| `blake-as.hashN` | -0.21% | disabled is faster |
| `sieve.count` | -0.19% | disabled is faster |
| `memory_tree.run` | -0.18% | disabled is faster |

#### `multi-bounds-cert` — Multiple bounds proofs

Retain independent proofs for interleaved arrays. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.602 us | 6.626 us | +0.35% | 0.54% |
| Full compile time | 342.476 us | 342.791 us | +0.09% | 3.60% |
| Full compile bytes | 147.54 KiB | 147.57 KiB | +0.02% | 0.00% |
| Full compile allocations | 497.0 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `matmul.run` | +13.47% | optimization helps |
| `arith.run` | +1.17% | optimization helps |
| `json-as.serializeN` | +0.58% | optimization helps |
| `quicksort.sortN` | +0.56% | optimization helps |
| `json-as-simd.serializeN` | +0.53% | optimization helps |
| `json-as-simd.deserializeN` | -1.08% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.94% | disabled is faster |
| `many_funcs.run` | -0.58% | disabled is faster |
| `tiny.add` | -0.39% | disabled is faster |
| `raytrace.render` | -0.25% | disabled is faster |

#### `addr-zext-elim` — Memory32 address cleanup

Skip redundant zero-extension of proven-clean memory32 addresses. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.595 us | 6.602 us | +0.11% | 0.65% |
| Full compile time | 343.645 us | 343.375 us | -0.08% | 4.79% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.validateN` | +2.80% | optimization helps |
| `nbody.step` | +1.20% | optimization helps |
| `quicksort.sortN` | +0.89% | optimization helps |
| `json-as.deserializeN` | +0.56% | optimization helps |
| `json-as-simd.serializeN` | +0.39% | optimization helps |
| `json-as-simd.deserializeN` | -0.81% | disabled is faster |
| `json-as.serializeN` | -0.50% | disabled is faster |
| `utf-as.convertN` | -0.32% | disabled is faster |
| `tiny.add` | -0.27% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.23% | disabled is faster |

#### `immutable-table` — Immutable tables

Specialize calls through never-written tables. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.596 us | 6.612 us | +0.25% | 0.70% |
| Full compile time | 344.402 us | 343.818 us | -0.17% | 3.69% |
| Full compile bytes | 147.54 KiB | 147.66 KiB | +0.08% | 0.00% |
| Full compile allocations | 497.1 | 500.1 | +0.62% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `dispatch.apply` | +5.65% | optimization helps |
| `branches.classify` | +1.22% | optimization helps |
| `utf-as.convertN` | +1.11% | optimization helps |
| `json-as.serializeN` | +0.82% | optimization helps |
| `arith.run` | +0.45% | optimization helps |
| `tiny.add` | -1.09% | disabled is faster |
| `quicksort.sortN` | -0.43% | disabled is faster |
| `nbody.step` | -0.35% | disabled is faster |
| `globals.accumulate` | -0.26% | disabled is faster |
| `crc32.hashN` | -0.22% | disabled is faster |

#### `immutable-table-type` — Immutable table types

Skip redundant immutable-table type checks. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.595 us | 6.601 us | +0.09% | 0.67% |
| Full compile time | 343.765 us | 343.382 us | -0.11% | 3.88% |
| Full compile bytes | 147.54 KiB | 147.50 KiB | -0.03% | 0.00% |
| Full compile allocations | 497.0 | 496.7 | -0.07% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `dispatch.apply` | +1.30% | optimization helps |
| `arith.run` | +1.02% | optimization helps |
| `branches.classify` | +0.76% | optimization helps |
| `raytrace.render` | +0.67% | optimization helps |
| `utf-as.convertN` | +0.43% | optimization helps |
| `json-as.serializeN` | -0.88% | disabled is faster |
| `blake-as.hashN` | -0.36% | disabled is faster |
| `utf-as-simd.validateN` | -0.24% | disabled is faster |
| `float.run` | -0.21% | disabled is faster |
| `json-as-simd.deserializeN` | -0.20% | disabled is faster |

#### `inline-callfree` — Call-free inline hints

Prioritize call-free functions for inlining. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.593 us | 6.715 us | +1.85% | 0.49% |
| Full compile time | 342.434 us | 342.199 us | -0.07% | 4.04% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.1 | +0.01% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `many_funcs.run` | +94.01% | optimization helps |
| `mandelbrot.render` | +0.45% | optimization helps |
| `float.run` | +0.41% | optimization helps |
| `utf-as-simd.validateN` | +0.40% | optimization helps |
| `sieve.count` | +0.17% | optimization helps |
| `branches.classify` | -1.07% | disabled is faster |
| `swar-pack-parse.pack` | -0.23% | disabled is faster |
| `json-as-simd.serializeN` | -0.23% | disabled is faster |
| `dispatch.apply` | -0.20% | disabled is faster |
| `json-as.serializeN` | -0.19% | disabled is faster |

#### `store-forward` — Store forwarding

Forward straight-line stores into loads. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.593 us | 6.625 us | +0.49% | 0.96% |
| Full compile time | 343.850 us | 344.272 us | +0.12% | 3.79% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `memory_tree.run` | +17.77% | optimization helps |
| `utf-as-simd.convertN` | +0.55% | optimization helps |
| `swar-pack-parse.runN` | +0.47% | optimization helps |
| `xjb-mulhi.mulhi` | +0.31% | optimization helps |
| `dispatch.apply` | +0.27% | optimization helps |
| `branches.classify` | -1.10% | disabled is faster |
| `raytrace.render` | -0.33% | disabled is faster |
| `sha256.hashN` | -0.25% | disabled is faster |
| `many_funcs.run` | -0.12% | disabled is faster |
| `memory.sum` | -0.10% | disabled is faster |

#### `frame-elide` — Frame elision

Omit frames for small single-result functions. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.656 us | 6.669 us | +0.19% | 0.64% |
| Full compile time | 347.193 us | 347.753 us | +0.16% | 3.92% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `json-as.serializeN` | +2.26% | optimization helps |
| `json-as.deserializeN` | +1.28% | optimization helps |
| `branches.classify` | +1.07% | optimization helps |
| `utf-as-simd.validateN` | +0.50% | optimization helps |
| `mandelbrot.render` | +0.44% | optimization helps |
| `memory.sum` | -0.20% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.20% | disabled is faster |
| `float.run` | -0.19% | disabled is faster |
| `fib_iter.fib` | -0.12% | disabled is faster |
| `swar-pack-parse.parse4` | -0.12% | disabled is faster |

#### `compact-i32-frame` — Compact i32 frames

Pack i32 locals in straight-line call-free functions. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.665 us | 6.674 us | +0.12% | 0.70% |
| Full compile time | 348.038 us | 349.297 us | +0.36% | 3.31% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.1 | +0.01% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `json-as-simd.deserializeN` | +1.39% | optimization helps |
| `utf-as-simd.validateN` | +1.28% | optimization helps |
| `branches.classify` | +1.21% | optimization helps |
| `fannkuch.run` | +0.60% | optimization helps |
| `json-as.serializeN` | +0.44% | optimization helps |
| `tiny.add` | -1.19% | disabled is faster |
| `utf-as.convertN` | -0.54% | disabled is faster |
| `swar-pack-parse.runN` | -0.21% | disabled is faster |
| `swar-pack-parse.parse4` | -0.19% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.18% | disabled is faster |

#### `local-slot-order` — Symbolic local slot packing

Move exact referenced local homes into zero-reference compact slots. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.672 us | 6.677 us | +0.07% | 0.56% |
| Full compile time | 347.002 us | 348.591 us | +0.46% | 4.45% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `json-as.serializeN` | +1.43% | optimization helps |
| `quicksort.sortN` | +0.70% | optimization helps |
| `json-as.deserializeN` | +0.56% | optimization helps |
| `sieve.count` | +0.29% | optimization helps |
| `xjb-mulhi.mulhi` | +0.28% | optimization helps |
| `json-as-simd.deserializeN` | -0.62% | disabled is faster |
| `dispatch.apply` | -0.27% | disabled is faster |
| `swar-pack-parse.runN` | -0.25% | disabled is faster |
| `tiny.add` | -0.24% | disabled is faster |
| `utf-as-simd.validateN` | -0.24% | disabled is faster |

#### `tee-spill-elide` — Reuse tee spill homes

Reuse a local.tee frame slot when spilling its still-live scalar result. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.680 us | 6.683 us | +0.04% | 0.56% |
| Full compile time | 348.645 us | 348.180 us | -0.13% | 3.46% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `arith.run` | +1.12% | optimization helps |
| `xjb-mulhi.mulhi` | +0.49% | optimization helps |
| `utf-as.convertN` | +0.41% | optimization helps |
| `float.run` | +0.35% | optimization helps |
| `globals.accumulate` | +0.27% | optimization helps |
| `branches.classify` | -1.29% | disabled is faster |
| `tiny.add` | -0.66% | disabled is faster |
| `xjb-mulhi.runN` | -0.16% | disabled is faster |
| `mandelbrot.render` | -0.16% | disabled is faster |
| `sha256.hashN` | -0.13% | disabled is faster |

#### `commute-self-update` — Commute self-updates

Make non-fixed destinations accumulate commutative self-update expressions in place. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.686 us | 6.692 us | +0.08% | 0.60% |
| Full compile time | 348.559 us | 347.867 us | -0.20% | 3.44% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `blake-as.hashN` | +1.35% | optimization helps |
| `blake-as-simd.hashN` | +0.97% | optimization helps |
| `json-as.serializeN` | +0.61% | optimization helps |
| `branches.classify` | +0.40% | optimization helps |
| `quicksort.sortN` | +0.31% | optimization helps |
| `json-as-simd.deserializeN` | -0.62% | disabled is faster |
| `nbody.step` | -0.39% | disabled is faster |
| `arith.run` | -0.30% | disabled is faster |
| `sha256.hashN` | -0.27% | disabled is faster |
| `utf-as-simd.validateN` | -0.21% | disabled is faster |

#### `i64-mask32` — Low-32 mask lowering

Lower i64 low-32 masks to zero-extending 32-bit ands. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.690 us | 6.731 us | +0.61% | 0.71% |
| Full compile time | 347.400 us | 348.690 us | +0.37% | 4.62% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.0 | -0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `xjb-mulhi.runN` | +17.13% | optimization helps |
| `utf-as.convertN` | +3.10% | optimization helps |
| `tiny.add` | +1.18% | optimization helps |
| `branches.classify` | +1.17% | optimization helps |
| `many_funcs.run` | +0.45% | optimization helps |
| `float.run` | -0.34% | disabled is faster |
| `raytrace.render` | -0.26% | disabled is faster |
| `sieve.count` | -0.21% | disabled is faster |
| `fannkuch.run` | -0.19% | disabled is faster |
| `blake-as.hashN` | -0.18% | disabled is faster |

#### `accumulator-immediate` — Accumulator immediates

Use modrm-free rax/eax imm32 encodings in size objectives. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.697 us | 6.692 us | -0.06% | 0.69% |
| Full compile time | 349.942 us | 348.450 us | -0.43% | 3.09% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `branches.classify` | +1.11% | optimization helps |
| `raytrace.render` | +0.36% | optimization helps |
| `utf-as-simd.validateN` | +0.32% | optimization helps |
| `globals.accumulate` | +0.28% | optimization helps |
| `nbody.step` | +0.24% | optimization helps |
| `json-as.serializeN` | -0.77% | disabled is faster |
| `mandelbrot.render` | -0.66% | disabled is faster |
| `json-as-simd.deserializeN` | -0.58% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.51% | disabled is faster |
| `json-as.deserializeN` | -0.34% | disabled is faster |

#### `v128-const-cache` — Vector constant cache

Reserve vector registers for repeated constants. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.700 us | 7.108 us | +6.09% | 0.97% |
| Full compile time | 349.135 us | 347.799 us | -0.38% | 4.60% |
| Full compile bytes | 147.54 KiB | 147.21 KiB | -0.22% | 0.00% |
| Full compile allocations | 497.0 | 492.4 | -0.94% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.validateN` | +186.23% | optimization helps |
| `utf-as-simd.convertN` | +98.53% | optimization helps |
| `raytrace.render` | +18.25% | optimization helps |
| `mandelbrot.render` | +10.93% | optimization helps |
| `nbody.step` | +9.04% | optimization helps |
| `json-as.serializeN` | -0.68% | disabled is faster |
| `utf-as.convertN` | -0.68% | disabled is faster |
| `dispatch.apply` | -0.36% | disabled is faster |
| `sha256.hashN` | -0.34% | disabled is faster |
| `json-as-simd.deserializeN` | -0.19% | disabled is faster |

#### `v128-pins` — Vector pins

Pin hot vector locals in registers. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.709 us | 6.832 us | +1.84% | 0.56% |
| Full compile time | 349.175 us | 349.016 us | -0.05% | 4.06% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | -0.00% | 0.00% |
| Full compile allocations | 497.0 | 497.0 | -0.02% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `utf-as-simd.validateN` | +72.94% | optimization helps |
| `blake-as-simd.hashN` | +9.18% | optimization helps |
| `utf-as-simd.convertN` | +4.98% | optimization helps |
| `utf-as.convertN` | +0.48% | optimization helps |
| `fib_iter.fib` | +0.29% | optimization helps |
| `tiny.add` | -2.02% | disabled is faster |
| `raytrace.render` | -0.45% | disabled is faster |
| `xjb-mulhi.mulhi` | -0.37% | disabled is faster |
| `json-as.deserializeN` | -0.35% | disabled is faster |
| `globals.accumulate` | -0.30% | disabled is faster |

#### `v128-sink` — Vector sinking

Sink vector operations into pinned locals. Selected default: **on**; experimental: **no**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.708 us | 6.712 us | +0.07% | 0.55% |
| Full compile time | 349.778 us | 348.817 us | -0.27% | 3.56% |
| Full compile bytes | 147.54 KiB | 147.54 KiB | +0.00% | 0.00% |
| Full compile allocations | 497.1 | 497.1 | +0.00% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `xjb-mulhi.mulhi` | +0.96% | optimization helps |
| `json-as.serializeN` | +0.95% | optimization helps |
| `utf-as-simd.validateN` | +0.90% | optimization helps |
| `blake-as-simd.hashN` | +0.83% | optimization helps |
| `arith.run` | +0.30% | optimization helps |
| `nbody.step` | -0.57% | disabled is faster |
| `utf-as.convertN` | -0.25% | disabled is faster |
| `crc32.hashN` | -0.18% | disabled is faster |
| `json-as.deserializeN` | -0.15% | disabled is faster |
| `sha256.hashN` | -0.15% | disabled is faster |

#### `reg-abi` — Register ABI

Use wago's internal register calling convention. Selected default: **on**; experimental: **no**; triage: **failed**.

Measurement failed: 4 nonzero exits; samples on/off=4/4. Partial output is retained in the raw capture directory and excluded from aggregates.

#### `inline` — Inlining

Inline eligible callees. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.705 us | 6.858 us | +2.27% | 0.60% |
| Full compile time | 348.929 us | 342.689 us | -1.79% | 4.67% |
| Full compile bytes | 147.54 KiB | 143.63 KiB | -2.65% | 0.00% |
| Full compile allocations | 497.1 | 490.0 | -1.42% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `many_funcs.run` | +122.63% | optimization helps |
| `branches.classify` | +1.06% | optimization helps |
| `raytrace.render` | +0.58% | optimization helps |
| `tiny.add` | +0.52% | optimization helps |
| `sha256.hashN` | +0.27% | optimization helps |
| `xjb-mulhi.mulhi` | -0.91% | disabled is faster |
| `fib_iter.fib` | -0.61% | disabled is faster |
| `float.run` | -0.44% | disabled is faster |
| `utf-as.convertN` | -0.42% | disabled is faster |
| `utf-as-simd.validateN` | -0.39% | disabled is faster |

#### `inline-loop-callees` — Loop-call inlining

Inline callees invoked from inside loops. Selected default: **off**; experimental: **yes**; triage: **removal-screen**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.710 us | 6.710 us | +0.01% | 0.47% |
| Full compile time | 351.524 us | 349.359 us | -0.62% | 3.54% |
| Full compile bytes | 148.27 KiB | 147.54 KiB | -0.49% | 0.00% |
| Full compile allocations | 506.1 | 497.1 | -1.80% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `blake-as-simd.hashN` | +1.75% | optimization helps |
| `json-as.serializeN` | +1.01% | optimization helps |
| `quicksort.sortN` | +0.15% | optimization helps |
| `fib_rec.fib` | +0.14% | optimization helps |
| `linked_list.sum` | +0.13% | optimization helps |
| `branches.classify` | -1.29% | disabled is faster |
| `many_funcs.run` | -0.64% | disabled is faster |
| `utf-as.convertN` | -0.47% | disabled is faster |
| `fannkuch.run` | -0.21% | disabled is faster |
| `xjb-mulhi.runN` | -0.20% | disabled is faster |

#### `loop-precheck` — Loop prechecks

Hoist invariant bounds checks before loops. Selected default: **on**; experimental: **no**; triage: **mixed/noisy**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.704 us | 6.711 us | +0.11% | 0.61% |
| Full compile time | 348.879 us | 338.231 us | -3.05% | 4.87% |
| Full compile bytes | 147.54 KiB | 138.56 KiB | -6.09% | 0.00% |
| Full compile allocations | 497.0 | 475.4 | -4.36% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `fannkuch.run` | +2.88% | optimization helps |
| `utf-as-simd.validateN` | +1.11% | optimization helps |
| `nbody.step` | +0.91% | optimization helps |
| `json-as.serializeN` | +0.41% | optimization helps |
| `json-as.deserializeN` | +0.22% | optimization helps |
| `json-as-simd.deserializeN` | -0.48% | disabled is faster |
| `swar-pack-parse.runN` | -0.39% | disabled is faster |
| `spectralnorm.run` | -0.20% | disabled is faster |
| `utf-as.convertN` | -0.20% | disabled is faster |
| `blake-as.hashN` | -0.20% | disabled is faster |

#### `stack-fence` — Stack fence

Emit the stack-overflow guard fence. Selected default: **on**; experimental: **no**; triage: **retain**.

| Metric | Enabled geomean | Disabled geomean | Disabled delta | Median sample spread |
|---|---:|---:|---:|---:|
| Execution time | 6.706 us | 6.699 us | -0.10% | 0.67% |
| Full compile time | 349.391 us | 348.516 us | -0.25% | 3.87% |
| Full compile bytes | 147.54 KiB | 147.53 KiB | -0.01% | 0.00% |
| Full compile allocations | 497.0 | 496.1 | -0.19% | 0.00% |

| Execution workload | Disabled delta | Direction |
|---|---:|---|
| `quicksort.sortN` | +12.00% | optimization helps |
| `tiny.add` | +2.19% | optimization helps |
| `fannkuch.run` | +0.60% | optimization helps |
| `utf-as-simd.validateN` | +0.49% | optimization helps |
| `raytrace.render` | +0.44% | optimization helps |
| `json-as-simd.serializeN` | -5.49% | disabled is faster |
| `json-as.serializeN` | -4.40% | disabled is faster |
| `json-as-simd.deserializeN` | -4.17% | disabled is faster |
| `memory_tree.run` | -1.26% | disabled is faster |
| `json-as.deserializeN` | -1.07% | disabled is faster |

#### `stack-reg` — Stack register

Keep the guest stack pointer in a register. Selected default: **on**; experimental: **no**; triage: **failed**.

Measurement failed: 4 nonzero exits; samples on/off=4/4. Partial output is retained in the raw capture directory and excluded from aggregates.

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
