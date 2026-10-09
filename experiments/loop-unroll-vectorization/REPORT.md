# Loop unrolling and vectorization experiments

These opt-in prototypes have completed AMD64 correctness and timing checks.
**Native ARM64 validation is incomplete because native hardware is unavailable.**
The results support further work on narrow integer reductions and 128-bit maps.
They do not support promotion of general ordered replication to the defaults.

| Family | Status | Result |
|---|---|---|
| A: sum choices | Tested | Range proof and independent chains both help; emitter setup also changes the result. |
| B: nearby reduction forms | Tested | i32 add, integer XOR, add-minus-one, and tee/drop ending work. No added sampled corpus coverage. |
| C: ordered scalar replication | Tested | Factors 2/4 and guarded 2 preserve dependencies. Costs exceed useful measured gains. |
| D: existing Wasm SIMD | Tested | Factor 4 saves 9% at 8192; almost doubles compile time. Width stays 128 bits. |
| E: scalar maps | Tested | Modern-feature i32 saves 48%, f32 saves 35%; both need about 2.7 times the compile time. |
| F: ARM64 | Port and cross-build complete | Shared legality/replay uses separate ARM64 lowering. Native execution and timing remain untested. |

## Identity and evidence

Repository: `wago-org/wago`. Branch: `experiment/loop-unroll-vectorization`.
Worktree: `/home/jtenner/.codex/worktrees/loop-unroll-vectorization/wago`.
Fixed base: `2286d676facdfa1cabe2d1c61072e505438ca7f2` (fetched `origin/main`).
Existing work in `/home/jtenner/Projects/wago` was preserved. No rebase was used.
The family commits are A `5232db45a`, B `224e16797`, C `a7138fb0d`, D
`2527ab235`, E `eb72a5bd9`, and F `9bf0cd8d6`. The trap/native-budget repair is
`a7ecd8d29`. Later commits add qualification and measurement work; see
[commits.txt](results/commits.txt). The production compiler did not change during
the final measurements.

[environment.json](results/environment.json) records the machine and measurement
lineage. It is an AMD Ryzen 7 8845HS, 8 cores/16 threads, 16 MiB L3, Linux
6.12.111, Go 1.27.1. Boost was on; the governor was `powersave`. Timings used
`GOMAXPROCS=1`, no CPU affinity, and no machine isolation. Desktop background
activity caused some large outliers. All decisive benchmark rows have six
serial, interleaved samples with alternating variant order. The compile-regression
follow-up has 12 samples per version. Builds are outside timing. Fast-path
assertions and code statistics are outside normal timing.

Inputs are identical within each comparison. WAT and Wasm fixtures are in
`src/core/compiler/backend/railshot/amd64/testdata/loop_experiment/`.
[input-sha256.txt](results/input-sha256.txt) includes corpus and fixture hashes.
[final/summary.json](results/final/summary.json) retains every sample and median;
`benchstat-*.txt` supplies significance. It checks **1145 benchmark rows with six
samples each**, plus 38 subprocess groups with six verified results each.
Only `results/final/` and the named 12-sample follow-up are decisive. Earlier
outputs in `results/` include pilots, incomplete selections, and failed checks.
The first final corpus pass failed because direct test binaries used a different
working directory. A test-helper repair added the repository-root path. The run
resumed at that phase, retaining the completed first-round samples. No compiler
change was made for the repair.

The fixed base confirms the existing four-chain i64 sum on both targets, the
bounded AMD64 f64 paths, and default qualified AVX paths. The combined sum option
changes both bounds treatment and unrolling. The memory32 repair from PR #726
is retained. Existing path controls were audited before the measurements.

## A and B: integer reductions

At 8192 i64 elements, the following are medians. Compile timings are in µs;
execution timings are in ns. Each function has a 40-byte frame and zero reported
spills/reloads. All use the same scalar first-iteration peel and register policy.

| Variant | Checks | Factor/chains | Execute | Compile | Native bytes |
|---|---|---:|---:|---:|---:|
| A | Ordinary | 1/1 | 3100.5 | 10.70 | 165 |
| B | Range proof | 1/1 | 1769.5 | 11.12 | 198 |
| H | Range proof, matched grouped emitter | 1/1 | 3525.0 | 11.09 | 277 |
| C | Range proof | 2/1 | 1777.5 | 11.21 | 282 |
| D | Range proof | 4/1 | 1740.5 | 11.08 | 292 |
| E | Range proof | 2/2 | 1748.5 | 11.07 | 287 |
| G | Range proof | 4/2 | 969.7 | 10.94 | 297 |
| F | Range proof | 4/4 | 911.9 | 11.07 | 307 |

**B versus A saves 42.93%; F versus A saves 70.59%** (`p=.002`). H exposes
grouped-emitter overhead: H is 13.69% slower than A. Within that emitter, C/D
save 49.57%/50.62% versus H. Holding factor 4 fixed, G/F save 44.29%/47.61%
versus D. B versus C/D alone cannot isolate reduced loop control because the
emitter structure changes. Two chains at factor 2 provide no clear extra gain.
At aligned start 128, the 32 MiB sum takes 1.591 ms in A and 0.593 ms in F.
Short counts and unaligned start 1 are retained in the address results.

A compilation allocates 11024 bytes/26 allocations; the proof/grouped variants
allocate 11048/27. Most compile-time differences in this small matrix are within
noise. Median-cost arithmetic suggests one 8192-element call repays the small
added compile cost, but this is not a precise compile-cost estimate.

B adds narrow recognition behind `WAGO_LOOP_REDUCTION_FORMS=1`. At 8192,
i32 addition saves **69.43%**, XOR **63.80%**, add-minus-one **67.61%**, and the
tee/drop ending **68.07%** (`p=.002`). Compile changes are within noise; accepted
forms add 24 allocated bytes and one allocation. The alternative `eq` zero
header remains rejected and shows no significant gain. This is a retained
negative result, not an implemented general header recognizer.

## C, D, and E: ordered replay and maps

The first C/D/E series uses the architectural SSE2 baseline with all optional
feature bits clear. Modern map comparisons use the same
`SSSE3|SSE4.1|SSE4.2|AVX` mask on both sides. All vector-map and Wasm-SIMD
variants retain four 32-bit lanes in **128 bits**. No wider map variant is mixed
into these comparisons.

| Kernel, count 8192 | Reference → experiment, µs | Execution delta | Compile delta | Native bytes; frame bytes |
|---|---:|---:|---:|---|
| i32 map, ordered count4, SSE2 | 4.159 → 4.403 | +5.88%, p=.041 | +110.99% | 254 → 512; 72 → 72 |
| Dependent f64, count4, SSE2 | 5.208 → 5.248 | No significant change | +97.08% | 241 → 459; 88 → 88 |
| Pointer traversal, count4, SSE2 | 6.895 → 6.789 | No significant change | +79.52% | 188 → 324; 56 → 56 |
| Existing SIMD, simd4, SSE2 | 4.163 → 3.789 | **−8.98%, p=.009** | +96.47% | 262 → 544; 120 → 120 |
| f32 map, vector, SSE2 | 5.894 → 2.381 | **−59.61%, p=.002** | +184.29% | 256 → 713; 104 → 120 |
| i32 map, vector, SSE2 | 4.159 → 13.366 | **+221.41%, p=.002** | +186.37% | 254 → 831; 72 → 136 |
| f32 map, vector, modern | 3.560 → 2.329 | **−34.59%, p=.002** | +167.58% | 246 → 687; 104 → 120 |
| i32 map, vector, modern | 4.139 → 2.157 | **−47.89%, p=.002** | +165.83% | 254 → 703; 72 → 88 |

General replay preserves original load/arithmetic/store order between copies.
It does not remove f64 or pointer dependencies. Count2, count4, and guard2 give
no sustained benefit on these dependent controls. Isolated small gains remain:
pointer/count2/512 saves 4.68% (`p=.026`), and dependent-f64/count4/262144 saves
2.58% (`p=.041`). Larger dependent inputs show no significant improvement.
Guarded i32 replication is 16.63% slower at 8192. SIMD2 has no significant
8192 gain. SIMD4 needs about **39 executions** to repay added compile time at
that size, using median costs.

The i32 feature split is material. SSE2 lowering uses four scalar multiplies and
stack transfers. Modern lowering uses `vpmulld` on XMM registers. Modern f32
uses separate `vmulps` and `vaddps`, with no FMA. Saved assembly shows no YMM
map operations. At 4,194,304 elements (16 MiB input plus output, larger than L3),
modern i32 saves **45.42%** and f32 saves **30.76%** (`p=.002`). At count 0,
the vector setup costs 7.42%/11.21% more for i32/f32; absolute differences are
about 2 ns. Short-count and remainder cases are retained.

Modern vector compilation costs 37.96 µs for i32 and 37.22 µs for f32 versus
14.28/13.91 µs scalar. Both increase allocations from **26160 bytes/30** to
**32632 bytes/55**. Median break-even counts at 8192 are **12 calls for i32** and
**19 calls for f32**. These estimates omit cache and startup variation.

At count 8192, native execution medians report **0 B/op and 0 allocs/op**.
The f32-vector/262144 medians report 3 B/op under SSE2 and 6 B/op with modern
features; some streaming raw samples also report allocated bytes. Rounded
allocation counts remain zero;
there is no new allocation in the loop body. Module clones, rewritten byte
buffers, and source-position tapes allocate during compilation. Escape analysis
retains fixed loop-plan storage outside the heap. Reported allocator spills and
reloads are zero even in the 14-parameter pressure case; its frames grow to
200/216 bytes. This does not mean no stack traffic: branch home stores, ABI
storage, and conservative SIMD multiply transfers are visible in assembly.
See [native-sizes-final.txt](results/native-sizes-final.txt),
[native-modern-sizes.txt](results/native-modern-sizes.txt), and the native files.

The existing f64 baselines independently confirm the value of current paths.
At 8192, independent scalar/pair128/wide256 take 4.041/1.784/0.955 µs; adjacent
scalar/128/256 take 7.831/3.657/2.743 µs. All optimized comparisons are significant
(`p=.002`). True scalar disables vector-map, adjacent-pair, wide-independent,
wide-adjacent, scalar-memory-recurrence, and zero-counter controls. The test
harness enables only the selected path, including its required planner control.
Fast assertions qualify the selected guards before timing.

## Corpus coverage and whole-command costs

The bounded scan covers nine modules. Existing emission includes the sum in
`memory.wasm`, two wide-adjacent loops in jacobi, and one in gemm. The new plans
accept only the already-optimized memory sum; they add no useful coverage here.
The writing fill reads the counter in its body and is rejected. Linked-list
header structure, memory-tree calls/prefix control, nested polybench structure,
and large SIMD functions/prefix control fail the current grammar or budgets.
`many_funcs.wasm` has 301 functions and no loops. Blake/UTF SIMD contain
2192/674 SIMD instructions, but no accepted new plan. See
[coverage.txt](results/coverage.txt) for exact rejection counts and native sizes.
These counts are per-function bounded recognition, not a dynamic hot-loop census.
Native per-loop sampling is blocked by OS perf permissions: `perf_event_paranoid`
is 3, and `perf stat -e cpu-clock:u -- true` fails with permission denied. The
failure is retained in [hotspot-perf-check.txt](results/hotspot-perf-check.txt).
Thus module timings identify workloads for further inspection; they do not rank
individual missed loops. No privileged kernel setting was changed.

Fast assertions on jacobi/gemm execute successfully with verified catalog
checksums. Focused map assertions distinguish valid disjoint/exact-overlap
inputs from rejected overlap/range inputs. Emission labels alone are not used
as evidence of guard success.

Replacing the real corpus sum's four-chain path with count2/count4/guard2 changes
0.958 µs to 2.840/3.000/3.183 µs: **+196%/+213%/+232%**, all `p=.002`.
This negative control rules out applying generic replication before the existing
specialized sum. Count2 also increases the many-functions compile median by
9.40% (`p=.041`); rejected modules retain their allocation counts.

Public subprocess runs measure compile, instantiate, invoke, and process wall
time separately. Memory/sum at one call has roughly 2.6–3.2 ms wall cost; at
1000 calls, A/F have 6.748/4.019 ms wall medians and 3.427/0.891 ms execution.
Default gemm/scalar at 20 calls has 6.514/9.442 ms wall medians. Linked-list,
Blake SIMD, and UTF SIMD are also checked. Their execute medians are
124/379/52 µs for default, with wall costs 3.3/5.1/4.5 ms. These are representative
workload costs, not per-loop hotspot measurements. Startup samples are noisy;
no subprocess significance claim is made. Public API allocation measurements
include instantiate/init/invoke and are separate from direct native calls.

Default sum execution and compilation versus the fixed base show no significant
change. However, an isolated large function with no eligible loop shows a
**2.16% compile regression**, 26.06 → 26.62 µs (`n=12`, benchstat rounded `p=.000`),
with unchanged 9456 bytes/11 allocations. The six-sample pass first showed
4.66%; the follow-up confirms a small cost. The prototypes preserve default
semantics but must not be described as having zero default compile cost.

## Correctness, limits, and validation

The plan bounds bodies to 256 bytes/64 operations, 16 tracked locals, and four
memory accesses. Source functions are at most 4096 bytes; added source is at
most 1536 bytes/function and 32768 bytes/module. Native physical functions are
limited to 8192 bytes and module images to 262144 bytes. An oversized attempt
restores original compilation, including in parallel compilation. Fixed
register storage and existing allocation policy remain in use.

Tests compare outputs, live locals, operand state, trap PC, and memory effects.
They include counts 0–9 and tails, partial/exact overlap, unaligned addresses,
memory-immediate offset rejection, full 4 GiB memory32 wrap, final-boundary
crossing, and large trip counts which trap within two iterations. Floating
point tests cover order, signed zero, and permitted Wasm NaN results. Writing
maps use widened range/overlap guards and fall back to original scalar operations
on failure. They do not borrow the read-only sum's early-trap policy.
Calls, nested control/exits, references, exceptions, atomics/shared memory,
memory64/growth, and unsupported interruption/custom/profiling contexts reject
the rewrite. Original module storage remains unchanged. Repeated operations
retain original instruction positions, including an unchanged SIMD suffix.

The required local gates pass: `just test unit` (ordinary and checked builds),
the full checked `bench` module, `just lint`, guard-page runtime/public-API tests,
focused conservative and modern experimental tests, and documentation validation.
ARM64 checked/statistics test binaries cross-build. The OS/architecture native
CI matrix remains for CI; cross-build success is not native ARM64 qualification.
[REVIEW.md](REVIEW.md) records independent actual-diff correctness and measurement
reviews and the fixes. A Claude attempt failed with `Not logged in`; **no Claude
review occurred**.

## Reproduce

Run from the repository root. Use a fresh output directory. Dependencies are Go,
Python 3, `just`, `benchstat`, binutils, and the repository lint tools.

```sh
git submodule update --init tests/conformance/spec-v3
scripts/bootstrap-wabt.sh
export PATH="$PWD/.tools/wabt-1.0.41-linux-x64/bin:$PATH"
export GOFLAGS=-buildvcs=false
experiments/loop-unroll-vectorization/baseline.sh
WAGO_EXPERIMENT_RESULTS=experiments/loop-unroll-vectorization/results/reproduce \
  experiments/loop-unroll-vectorization/run.sh
experiments/loop-unroll-vectorization/run_extra.sh \
  experiments/loop-unroll-vectorization/results/reproduce
experiments/loop-unroll-vectorization/analyze.sh \
  experiments/loop-unroll-vectorization/results/reproduce
just test unit
(cd bench && go test -tags=wago_regalloccheck ./...)
go test -tags=wago_codegenstats,wago_regalloccheck \
  ./src/core/compiler/backend/railshot/amd64 \
  ./src/core/compiler/backend/railshot/shared -run 'TestExperimental' -count=1
WAGO_LOOP_FEATURES=modern go test -tags=wago_codegenstats,wago_regalloccheck \
  ./src/core/compiler/backend/railshot/amd64 -run 'TestExperimental' -count=1
go test -tags=wago_guardpage,wago_regalloccheck ./src/core/runtime ./src/wago
just lint
just docs
GOOS=linux GOARCH=arm64 go test -c -tags=wago_codegenstats,wago_regalloccheck \
  -o /tmp/wago-loop-arm64.test ./src/core/compiler/backend/railshot/arm64
```

For native artifacts, set `WAGO_EXPERIMENT_ARTIFACT_DIR` to an absolute directory
and run `TestExperimentalNativeArtifacts` with the checked/statistics tags. Repeat
with `WAGO_LOOP_FEATURES=modern`. Decode `.bin` files using
`objdump -D -b binary -m i386:x86-64`. On native ARM64, run
`TestExperimentalSumMatrixARM64|TestExperimentalSharedPlansARM64`, then run
`BenchmarkExperimentalSumMatrixARM64` in separate processes for A–H using
`WAGO_LOOP_SUM_EXPERIMENT`. These ARM64 steps have not been run here.

## Promotion decision

Keep the present defaults. Narrow B reduction forms are candidates after useful
production coverage is found. Modern 128-bit i32/f32 maps justify a follow-up
which adds coverage without the cloning and source-tape costs. Conservative
i32 multiplication needs a profitability/feature gate. Generic ordered replay
and SIMD4 do not justify default promotion from this evidence. Resolve the
small default compile regression before promoting any shared-path changes.
Native ARM64 correctness/performance and a detailed dynamic hotspot census
remain incomplete. No measured AMD64 gain establishes either result.
