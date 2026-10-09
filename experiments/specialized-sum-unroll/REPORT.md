# Specialized integer-sum unrolling

This experiment keeps the existing four-element, four-accumulator path as the
default. It changes only the AMD64 sum latch. It does not rewrite Wasm or add a
general loop optimizer. The branch starts at `2286d676facdfa1cabe2d1c61072e505438ca7f2`.
The tests-first commit is `ec0f965ea`; the implementation commit is `052b248bb`.
The original worktree and [PR #909](https://github.com/wago-org/wago/pull/909)
remain unchanged.

## Scope and constraints

The existing recognizer admits this exact top-tested loop:
`acc += i64.load(addr); addr += 8; counter--; br 0`.
The load must use memory 0 and offset 0. Address and counter must be distinct
`i32` locals. The accumulator must be `i64`. All three must have pinned GP
registers when the latch is emitted. The loop has no calls or memory writes.
Shared memory, memory64, interruption polls, unsupported control shapes, and
unproved ranges retain their existing fallback rules.

The bounds proof zero-extends address and count, then computes the end in 64
bits. A failed range normally traps. Full 4 GiB memory32 permits aligned
wrapping loads; an unaligned load that crosses the boundary must trap. The
latch sends wrapping ranges to the scalar tail. That proof and dispatch remain
unchanged. Native address updates still use 32-bit arithmetic.

The first scalar iteration is unchanged. A count of 8 therefore leaves 7 loads,
which use the scalar tail in factors 8 and 16. Power-of-two counts leave tails
of 3, 7, or 15 loads for factors 4, 8, or 16. The measurements include these
costs. Zero count skips the body and setup. Addition is modulo 2^64. Extra
accumulators start at zero and each is added once to the live sum.

`wago_sumunroll` is a build tag. Only tagged builds contain the experimental
emitter and its setting. A package-local test helper selects A–E. Normal builds
inline a wrapper to the unchanged emitter. No production option or environment
lookup is added to compilation. The experiment uses a fixed `[8]Reg` array.
It uses only free registers. It does not spill or evict pins to admit a
candidate. Failed register or encoding-budget checks occur before mutation and
select the current four-element path. The encoding bound is
`256 + 8*factor + 16*chains`, at most 512 bytes for these configurations. This
bounds the added latch; it is not a new limit on whole native functions.

PR #909 showed that general loop copying can replace a good specialized path
with slower code. Its sum factors stop at four. Its scalar, generic, and SIMD
comparisons are not used as this experiment's baseline. This experiment uses
normal Wago with its sum path enabled.

## Measurement method

The CPU is an AMD Ryzen 7 8845HS, with 16 MiB L3. Go is 1.27.1 on Linux/AMD64.
Each decisive comparison has 20 serial baseline/candidate pairs. Order
alternates for each pair. `GOMAXPROCS=1` and `taskset -c 2` are used. Boost is
on. The desktop remains active; the CPU is pinned but not isolated. No build
or test runs overlap decisive timings. Source review and light file operations
can occur during timings. Environment, command lines, sample order, return
codes, and all raw benchmark output are saved.

Both sides use identical Wasm, data, addresses, CPU-feature defaults, and
compiler options. Optional vector instructions are not used by these latches.
The execution fixture returns the sum, exit address, exit count, and a checksum
slot. Its frame and ABI costs are common to all configurations. Execution
includes the native call boundary. Compilation, memory fill, oracle calculation,
and preflight execution are outside its timer. Compiler measurements include
`CompileModule` and `CodeImage.Close`; decode and validation are outside the
timer. Timing binaries omit statistics and register-check tags. Native artifacts
use both tags. All execution measurements use the fixture without extra live
parameters. Pressure cases are correctness and compile controls.

The input sizes span short loops, 4 KiB, 64 KiB, 2 MiB, and nearly 64 MiB.
The last size exceeds L3. Addresses 0 and 1 distinguish aligned and unaligned
loads. Repeated calls use the same buffer. Setup initializes every byte.

`benchstat` reports unpaired significance from the interleaved samples. A
reported `p=0.000` means `p<0.001`. CSV deltas are ratios of the two medians;
the CSV also has the median of paired deltas. JSON retains every sample. No
claim of equivalence is made from a nonsignificant result. Tiny statistically
significant differences are not treated as useful gains.

See [build.txt](results/build.txt) for binary lineage. Run1 A/B predates binary
hash capture; its candidate hash was not saved before a rebuild. Its emitter
and timed benchmark sources did not change. The original native artifacts are
retained. All later runs record binary hashes. The separate repeat is required
for any A/B gain used in the recommendation.

## First sweep

These are medians from 20 samples per side. Each delta uses that variant’s
interleaved normal-Wago baseline. It is not a comparison with scalar Wasm.
The separate repeat is reported below.

Address 0. Positive deltas mean slower execution.

| Variant (factor/chains) | 512, ns | 8192, ns | 262144, µs | 8388607, ms |
|---|---:|---:|---:|---:|
| A (8/4) | 70.09 (-7.88%) | 918.60 (-2.18%) | 31.41 (-4.45%) | 1.34 (-0.72%) |
| B (8/8) | 70.70 (-8.41%) | 923.55 (-2.58%) | 31.55 (-4.12%) | 1.35 (-1.17%) |
| C (2/2) | 123.75 (+62.86%) | 1773.00 (+87.62%) | 59.39 (+82.58%) | 1.90 (+42.18%) |
| D (16/4) | 66.20 (-12.93%) | 889.10 (-6.53%) | 30.97 (-6.18%) | 1.30 (-3.78%) |
| E (16/8) | 66.95 (-13.10%) | 897.90 (-5.92%) | 30.80 (-7.77%) | 1.32 (-3.56%) |

Address 1. Positive deltas mean slower execution.

| Variant (factor/chains) | 512, ns | 8192, ns | 262144, µs | 8388607, ms |
|---|---:|---:|---:|---:|
| A (8/4) | 71.59 (-7.01%) | 1005.00 (+2.69%) | 34.80 (-9.10%) | 1.38 (-1.56%) |
| B (8/8) | 71.38 (-8.19%) | 1014.00 (+2.53%) | 34.60 (-10.24%) | 1.38 (-1.54%) |
| C (2/2) | 125.05 (+60.76%) | 1764.00 (+80.13%) | 59.11 (+53.95%) | 1.86 (+24.87%) |
| D (16/4) | 69.27 (-10.00%) | 953.30 (-2.78%) | 34.25 (-10.51%) | 1.37 (-5.01%) |
| E (16/8) | 69.17 (-11.30%) | 989.10 (-1.34%) | 34.43 (-11.72%) | 1.37 (-8.01%) |

| Variant | Compile + close, µs | Δ | Bytes/op | Allocs/op | Internal + trap bytes | Total native bytes | Frame bytes |
|---|---:|---:|---:|---:|---:|---:|---:|
| Baseline 4/4 | 12.95 | — | 26072 | 29 | 303 | 347 | 56 |
| A | 13.20 | +1.99% | 26072 | 29 | 323 | 367 | 56 |
| B | 13.48 | +3.55% | 26072 | 29 | 346 | 390 | 56 |
| C | 12.99 | -2.38% | 26072 | 29 | 283 | 327 | 56 |
| D | 12.91 | +0.87% | 26072 | 29 | 366 | 410 | 56 |
| E | 13.58 | -2.45% | 26072 | 29 | 389 | 433 | 56 |

Compile baseline medians vary across pairs. None of these first-sweep compile
changes is significant at p<.05. This does not establish equal compile cost.
All measured execution rows report 0 B/op and 0 allocs/op.
Frames are 56/88/168 bytes for pressure 0/4/12. Operand spill and reload
counters are zero. Whole functions still have ABI and local frame transfers.
B and E select the original four-chain path at pressure 12; their compile
rows at that pressure are fallback measurements.

Complete short-loop, pressure, allocation, and timing comparisons:
[A/B CSV](results/run1-AB/comparison.csv),
[C/D/E CSV](results/run1-CDE/comparison.csv).
Corresponding `*-benchstat.txt` files retain significance and confidence intervals.

## Separate repeat and limits

The second run uses new processes and another 20 samples per side. It records
binary hashes. All four candidates with first-run gains were repeated.

| Variant | Aligned 512 | Aligned 8192 | Aligned 262144 | Unaligned 512 | Unaligned 8192 | Unaligned 262144 |
|---|---:|---:|---:|---:|---:|---:|
| A | -9.42% | -2.24% | -4.38% | -8.14% | +2.45% | -9.40% |
| B | -8.88% | -1.92% | -3.95% | -7.64% | +2.97% | -9.99% |
| D | -13.61% | -6.65% | -7.04% | -11.14% | -3.59% | -9.87% |
| E | -13.51% | -5.92% | -5.99% | -11.32% | +1.22% | -11.14% |

D has repeated gains of 12.93–13.61% at aligned 512, 6.53–6.65% at aligned
8192, and 6.18–7.04% at aligned 262144. All have p<.001 in both runs.
At unaligned 512 it saves 10.00–11.14%; at unaligned 262144 it saves
9.87–10.51%. The unaligned 8192 gain is smaller, 2.78–3.59%.

D also repeats a short-loop cost. At count 16, the aligned loss is 3.49% in
run1 and 1.67% in run2 (both significant). E loses 7.82% and 8.50% at that
count. A/B have a repeated 2.45–2.97% loss at unaligned 8192. More branches
removed does not imply a gain on every input.

No aligned 64 MiB gain is significant in the repeat. D’s unaligned streaming
gain shrinks from 5.01% to 2.19%; E’s 8.01% first-run gain is not significant
in run2. These results do not support a repeatable 5% gain for streaming
loads. The large-buffer timings are consistent with memory bandwidth limiting
the gain, but hardware counters were not collected.

Compiler allocations remain 26072 B and 29 allocations for pressure0; pressure12
remains 26296 B and 30 allocations. Compile + close changes remain insignificant
in run2. D’s median is 14.38 µs versus 14.00 µs (p=.265), a +0.38 µs
difference. This is a noisy cost estimate, not a proven compile regression.

Using that positive median difference gives about 36 calls at aligned 512,
6 calls at aligned 8192, or 1 call at aligned 262144 to repay the cost.
The first-run median difference was only +0.11 µs. The uncertainty is material.
At inputs where execution is slower, no positive break-even count exists.

[Repeat CSV](results/run2/comparison.csv) has every row, including short loops,
pressure, and allocation metrics. The saved benchstat files give significance
for each comparison.

## Assembly and accumulator comparisons

All variants keep one 8-byte memory add per element. A full group adds
`factor` loads, one address update, two counter operations, and one backward
branch. Static group instruction counts are 8 for baseline, 12 for A/B,
6 for C, and 20 for D/E. Branch frequency falls from one per four elements
for baseline to one per eight or sixteen. It rises to one per two for C.

A and D retain four dependency chains. Their gains show that grouping can
help without more accumulators. C combines fewer chains with more loop-control
work and is much slower, despite saving 20 native bytes. The experiment does
not measure a pure factor-two/four-chain control, so it cannot divide C’s
loss between those causes.

B and E add four register chains. They also add four zeroing operations and
four final combine operations. Their hot group starts at 0xca instead of 0xbf.
This changes instruction layout as well as register use. No candidate spills
in its admitted hot group. Eight chains instead reject when there are too few
free registers. More native code is not automatically faster.

The following comparisons hold factor fixed. Each has 20 interleaved samples
per side. They measure complete candidates, including layout and setup.

| Change | Aligned 8192 | Aligned 262144 | Unaligned 8192 | Unaligned 262144 |
|---|---:|---:|---:|---:|
| 8/4 → 8/8 | +0.57%, not significant | +0.06%, not significant | −0.53%, not significant | −0.56%, not significant |
| 16/4 → 16/8 | −0.17%, not significant | −1.39%, not significant | **+4.70%, p<.001** | **−1.37%, p=.004** |

The small 2 MiB unaligned gain from eight chains comes with a larger 64 KiB
loss, 23 more native bytes, and stricter register admission. Four chains are
the better tradeoff in this evidence. Hardware counters and an instruction
layout control were not collected. CPU throughput explanations are therefore
inferences from code and timings. See [native-analysis.txt](results/native-analysis.txt),
[factor-eight chain comparison](results/chains-A/B-benchstat.txt), and
[factor-sixteen chain comparison](results/chains-D/E-benchstat.txt).

## Useful input sizes

Two further runs measure counts 64, 128, 256, 1024, 2048, and 4096. Each
has 20 interleaved samples per side. They use the same Wasm, data pattern,
seed, and native-call timer. Both sides use a 64 KiB memory region.

| Variant/address | 64 | 128 | 256 | 1024 | 2048 | 4096 |
|---|---:|---:|---:|---:|---:|---:|
| A, address 0 | -1.65%/-1.98% | -5.14%/-4.92% | -6.92%/-7.28% | -8.02%/-8.96% | -9.99%/-10.74% | -2.84%/-4.27% |
| A, address 1 | -1.28%/-1.73% | -4.60%/-4.46% | -5.57%/-6.66% | -6.03%/-8.19% | -8.80%/-9.49% | +0.02%/+0.35% |
| D, address 0 | +1.03%/+1.17% | -5.09%/-5.27% | -10.55%/-9.29% | -16.44%/-15.94% | -20.13%/-18.85% | -7.84%/-8.37% |
| D, address 1 | +1.83%/+2.33% | -3.96%/-4.13% | -7.24%/-8.26% | -12.50%/-11.02% | -14.11%/-14.32% | -6.01%/-6.03% |

Each cell shows run1/run2. D first reaches the 5% target in both runs at
128 aligned elements and 256 unaligned elements. Its unaligned 128 gain is
about 4%, which is credible but below that target. A first reaches the target
in both runs at 256 for both addresses. These are tested sizes, not a universal
cutoff: remainder counts, alignment, cache capacity, and instruction layout
can change the result. D has small positive median costs at 64 elements.
Its aligned first-run 64 difference is not significant (p=.055).

D saves about 20% at aligned 2048 (16 KiB), but about 6.6% at aligned
8192 (64 KiB). The benefit is not monotonic with size. It falls as memory
cost becomes more important. These threshold and cache results still use
a controlled sum kernel, not an enabled-candidate corpus or application study.

[Threshold run1 CSV](results/threshold1/comparison.csv) and
[run2 CSV](results/threshold2/comparison.csv) retain all timings and costs.

## Default compiler controls

The current normal build is compared with the tests-first compiler, with the
experimental build tag absent. Each comparison has 20 samples per side.

| Corpus row | Baseline | Current | Δ |
|---|---:|---:|---:|
| Compile/memory | 14546.00 ns | 14581.50 ns | +0.24% |
| Compile/many_funcs | 325949.50 ns | 323399.00 ns | -0.78% |
| Compile/linked_list | 14864.50 ns | 14864.00 ns | -0.00% |
| Compile/blake-as-simd | 782017.50 ns | 786446.50 ns | +0.57% |
| Compile/utf-as-simd | 375709.50 ns | 376994.00 ns | +0.34% |
| Exec/memory.sum | 98.88 ns | 98.59 ns | -0.29% |
| Exec/many_funcs.run | 14.57 ns | 14.57 ns | +0.03% |
| Exec/linked_list.sum | 8154.50 ns | 8174.00 ns | +0.24% |
| Exec/blake-as-simd.hashN | 387090.50 ns | 387818.50 ns | +0.19% |
| Exec/utf-as-simd.convertN | 56705.50 ns | 56667.50 ns | -0.07% |
| Exec/utf-as-simd.validateN | 144333.50 ns | 144531.00 ns | +0.14% |

None of these time differences is significant. Compiler allocated bytes and
allocation counts are unchanged. The normal sum execution and compile matrix
also has no significant differences. These are negative controls, not evidence
of faster corpus execution with a candidate enabled. The corpus memory sum
uses 512 elements; linked-list, many-function, and SIMD workloads cover other
compiler and runtime paths. No broad workload coverage claim is made.

[Corpus CSV](results/default-corpus/comparison.csv),
[corpus benchstat](results/default-corpus/default-benchstat.txt),
[normal sum CSV](results/default-sum/comparison.csv), and
[normal sum benchstat](results/default-sum/default-benchstat.txt) retain all costs.

The compiler confirms that the normal wrapper is inlined at the latch call:
[default-inlining.txt](results/default-inlining.txt). There is no experimental
setting or array in that build.

## Correctness and review

The first commit adds native tests and benchmarks against the unchanged
compiler. The second commit adds the emitter. Tests compare candidate native
execution with a byte-addressed Go oracle, the existing optimized path, and a
scalar Wago path. They check all four results, trap code and function data,
unchanged memory, and unchanged source bytecode. Trap metadata must match the
current optimized path. The current bounds-hoisted path can omit a trap PC;
this existing limit is preserved. The scalar reference can report that PC.

Counts cover 0–9, 15–17, 31–33, 512, 8192, and 262144. Tests include seeded
sum overflow, unaligned addresses, final valid and invalid loads, very large
counts whose byte calculation overflows 32 bits, a full sparse 4 GiB memory,
wrapping exit addresses, and live-parameter pressure. Both bounded and
unbounded memory declarations are tested. Diagnostic checks confirm candidate
selection at low pressure and fallback at high pressure. Direct emitter tests
check register cleanup, the encoding bound, and atomic rejection. A native test
executes the fallback for register and code-budget failures.

An independent source review found no blocking defect. Its three test-coverage
findings were addressed and reviewed again. See
[correctness-review.txt](results/correctness-review.txt). Claude Code was
attempted with the requested read-only command, but returned `Not logged in`.
No Claude review occurred. See [claude.txt](results/claude.txt).

## Validation status

Passed:

- `just test unit`, including ordinary and checked builds, after fetching the
  pinned spec-v3 submodule and installing the existing pinned conformance tools.
- Full checked `bench` module.
- Focused native correctness, selection, fallback, and emission tests for
  baseline and A–E with statistics and allocation checks.
- Full AMD64 statistics/checked suite with `WAGO_SHARED_SCALAR=0`, which
  exercises the legacy emitter used by these memory sums.
- Guard-page runtime and public-API tests; `just lint`; `just docs`.
- Shell and Python syntax checks, shellcheck, and the checked ARM64 cross-build.

The full AMD64 statistics suite under the normal shared-scalar default fails
with 86 failed test/subtest entries, including parent entries. The tests-first baseline has the exact
same failure names. The independent review traced these to the baseline shared
scalar compiler bypassing legacy peepholes. No unrelated repair is included.
The initial unit attempt failed because spec-v3 was not checked out; the full
unit rerun passed after setup. All attempt logs remain in `results/check-*.txt`.
[check-status.txt](results/check-status.txt) records the attempts and
[baseline-failure-comparison.json](results/baseline-failure-comparison.json)
records the matched failures.

The full native OS/architecture CI matrix was not run here. The ARM64 result
is a cross-build, not native execution. Claude review was unavailable. Hardware
performance counters, enabled-candidate corpus timing, and execution timing
with extra live parameters remain unmeasured. These limits prevent promotion.

## Recommendation

**Investigate further: 16 elements with four accumulators. Keep the current
default. Do not promote any configuration in this PR.**

The answer is a qualified yes: specialized factor 16 beats the current factor
4 on cache-resident sums without measured allocation or frame growth. D is
about 13% faster at aligned 512, 20% faster at aligned 2048, 6.6% faster at
aligned 8192, and 6–7% faster at aligned 262144. The 8192 gain repeats with
p<.001. Factor 8 also helps some inputs, but repeats an unaligned 8192 loss.
Eight accumulators give no consistent extra benefit. D has the best useful
overall tradeoff; there is no winner for every input.

D adds 63 bytes to the native function plus adapter (347→410), or 63 to the
internal function plus trap code (303→366). Its 56-byte frame is unchanged.
Compilation remains 26072 B/op and 29 allocations. The noisy positive repeat
compile + close delta is 0.38 µs, with no significant difference. Bounded
groups and unchanged chains keep the compile work and register use bounded.

Fewer loop-control instructions explain a plausible part of D’s gain. Four
chains already provide useful parallelism. Extra chains add setup, combination,
code, and admission costs. Streaming gains do not meet a repeated 5% target.
Short-loop losses, one-CPU evidence, and lack of enabled-candidate real-workload
measurements rule out automatic promotion. C is rejected on this CPU; A/B/E
are not preferred over D. Retain these opt-in cases to reproduce the evidence.

The independent performance review verified raw samples, sample order, medians,
allocations, native artifacts, significance, and break-even estimates. Its
rounding findings were corrected. See
[performance-review-final.txt](results/performance-review-final.txt).

## Reproduction

From a fresh checkout of this branch, with Go, Python 3, binutils, `taskset`,
and `benchstat` installed:

```sh
experiments/specialized-sum-unroll/run.sh /tmp/wago-sum-results 2
```

The script archives the tests-first commit to build the unchanged baseline.
It builds and tests all binaries before timing. Use an allowed CPU ID if CPU 2
is not available. Each output directory must be new. The command keeps all raw
samples and assembly. `analyze.py` checks equal sample counts and produces the
complete comparison CSV and numerical JSON.

## Follow-up experiments

1. Isolate the promising 128-bit `i32` and `f32` SIMD maps from PR #909. Test
   direct native emission that avoids module cloning and rewritten-Wasm
   allocations. Keep CPU-feature profitability checks.
2. Find real workload reductions that the current sum recognizer misses.
   Extend recognition only when measured coverage and speed justify it.

Neither follow-up is implemented here. No other PR is opened by this task.
