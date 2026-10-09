# Specialized integer-sum unrolling

The original study is retained below. The current continuation and decision are
in [Phase 2](#phase-2-hybrid-and-bounded-runtime-gates): retain opt-in evidence,
keep production 4/4, and investigate further without promotion.

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

## Phase 2: hybrid and bounded runtime gates

This section continues PR #910 from verified local and remote head
`d49f7252a906af9ffab94dabe1c83059c8589819`. The phase 1 report and all its raw
evidence above remain historical results. In particular, its lack of enabled
corpus measurements is addressed below. No rebase, new worktree, new PR,
production default change, ARM64 change, or general loop rewrite is included.
The continuation uses the same experimental branch and AMD64-only build tag.

### Designs and correctness

H uses sixteen loads per group with four chains, then four loads per group with
the **same four chains**, then scalar loads. T64, T128 and T256 compare the
remaining count once: below the gate, use four-element groups; at or above it,
use D's sixteen-element groups and scalar remainder. All counts in this report
are original call counts; the gate sees `count - 1` after the existing peel.
Thus original count 64 takes T64's small arm and count 65 takes its large arm.
Only the three planned gates are accepted. No gated hybrid or layout tuning
was added. The complete discovery sets were retained before T64 was selected
for independent confirmation; see [confirmation-plan.txt](results/followup/confirmation-plan.txt).

The bounds recognizer/proof, scalar peel, source Wasm, live exit locals and
fallback are unchanged. One widened wrap test routes full-memory32 wrapping
to the existing scalar tail before either grouped path. Address/counter updates
remain 32-bit; accumulator arithmetic remains modulo 2^64. Both arms share
setup, three temporary GP registers, scalar tail, combination and cleanup.
The implementation uses the existing fixed `[8]Reg` storage. It rejects an
unsupported configuration, insufficient free registers or insufficient byte
budget before emitting bytes or changing ownership. Conservative latch limits are
512 bytes for H and 528 for T, within the test selector's 576-byte budget.
No public option was added. The corpus bridge calls a private scalar-argument
setter from a tagged `_test.go` initializer, outside timing. Normal builds use
the unchanged four-element wrapper.

Tests preceded implementation. The native oracle matrix now covers 0–35,
63–66, 127–130, 255–258, 511–513, 8192 and 262144, with aligned/unaligned
loads, nonzero overflow seeds, all remainders, exit values, bounded/unbounded
memories, overflowed range calculations, OOB traps and trap metadata, unchanged
Wasm/memory, and live-parameter pressure. Full sparse 4 GiB wrapping tests ran
and passed for baseline, D, H and all three gates; they did not skip.
Diagnostic assertions require the selected H/T marker, so a silent fallback
cannot pass as candidate coverage. Direct tests separately reject register
pressure with sufficient budget and budget pressure with sufficient registers,
then check byte and ownership preservation. Native budget rejection tests
execute the original 4/4 fallback against the oracle. Existing scalar and
register-pressure fallback tests remain. Both sides and equality of each
post-peel gate execute in the native tests.

### Workload coverage

The conservative scan of 125 checked-in corpus modules and 223,466 function
bodies found only `synthetic/memory.wasm`, function 1, export `sum`. Function
diagnostics confirm exactly one specialized latch in that function for all
four principal configurations. The timed `memory.sum` calls count 512; its
existing zero-filled input and zero oracle remain identical. It is a synthetic
kernel, and no result here is an application-level speed claim. The new
lifecycle benchmark measures public compilation, instantiation, one sum call
and close, which supplies a complete small synthetic workload cost.

The other measured corpus modules—linked_list, blake-as-simd, utf-as-simd,
yyjson, xxhash and drwav—have zero selected sum latches. many_funcs was also
checked for admission. Their native sizes are equal across modes, with no
selected sum latch. The separate post-timing
[native-control-hashes.json](results/followup/native-control-hashes.json)
audit confirms byte equality for all seven negative controls across all six
modes. Original input hashes, sizes, admission counts and memory.bin bytes
also match. Only diagnostic output gained a native SHA-256 field in
`cb57571d5`; frozen emitter and benchmark timer sources were not changed.
They are negative controls, not positive coverage. Input SHA-256, function
statistics and assembly are in `results/followup/native-corpus-*`.

A bounded additional search fetched 21 published Wasm-R3 modules below 2 MiB,
9,441,146 bytes and 18,442 function bodies, from pinned revision
`bea10061c81428260ff029a9953273540a0c32e2` of
[`doehyunbaek/wasm-benchmarks`](https://github.com/doehyunbaek/wasm-benchmarks/tree/bea10061c81428260ff029a9953273540a0c32e2/wasm-r3-bench).
There were no canonical exact-shape matches. [r3-scan.json](results/followup/r3-scan.json)
retains pinned URLs, SHA-256 and Git blob checks. [scan-r3.py](scan-r3.py)
repeats that search. Fixed immediates use canonical LEB patterns; noncanonical
encodings and modules above the bound are outside the search. A byte match
would still need instruction/type/proof checks and native admission. These
negative modules were not compiled or run by this scan. Replay filenames do
not prove application provenance or an oracle. Arithmetic toys are excluded
from application claims. **No eligible real application was found.** This
limits the result and prevents promotion; recognition was not broadened to
manufacture coverage.

### Measurement process

The frozen confirmation implementation is commit
`1a4c6e1eac8d26c34a48494d80ffc9b12fa9ce0c`.
[build.json](results/followup/build.json) records source and binary SHA-256.
The first corpus D run used the earlier tests-first bridge at `5bbee26aa`;
the corpus repeat uses the final setter and compiler binary. Each set records
its own binary hashes. Guest D bytes are unchanged, but Go compiler layout
and selection plumbing are not asserted to be identical between those runs.
Every decisive row has 20 alternating baseline/candidate pairs, CPU 2,
`GOMAXPROCS=1`, and 150 ms benchmark duration. All builds, diagnostic checks
and profiling preceded their timing windows. The primary reference is a
normal-build Wago binary with unchanged 4/4; D→H and same-binary corpus
comparisons are explicitly supplementary. Native D bytes match the original
phase 1 artifact. Identical inputs/settings/features are retained. Execution
timers exclude compile/setup and report zero allocations. Compilation and
public lifecycle have separate timers. Allocation profiles are diagnostics;
their timings are not used as performance samples.

The CPU is the same Ryzen 7 8845HS and Go version is go1.27.1. Each run saves
environment and sample order. Turbo remains on, the desktop remains active,
the governor is powersave, and SMT sibling CPU 3 is not isolated. Pinning is
not CPU isolation. All samples, including unfavorable streaming outliers,
remain. `benchstat` supplies unpaired significance; `analyze.py` also retains
paired ratios and medians. A nonsignificant result does not prove equivalence.
Many rows are exploratory, without a multiple-comparison correction; isolated
small p-values are not adoption evidence. No hardware counters or controlled
instruction-alignment experiment was run.

### Native code and dependency costs

| Configuration | Kernel bytes incl. adapter | Corpus sum bytes | Corpus module bytes | Kernel frame, pressure 0/4/12 | Operand spills/reloads |
|---|---:|---:|---:|---|---|
| Existing 4/4 | 347 | 312 | 472 | 56/88/168 | 0/0 |
| Existing D 16/4 | 410 | 375 | 535 | 56/88/168 | 0/0 |
| H hybrid | 467 | 432 | 592 | 56/88/168 | 0/0 |
| T64 | 462 | 427 | 587 | 56/88/168 | 0/0 |
| T128 | 465 | 430 | 590 | 56/88/168 | 0/0 |
| T256 | 465 | 430 | 590 | 56/88/168 | 0/0 |

The corpus sum frame stays 40 bytes in every mode. Zero operand spills do not
mean there are no ABI/local stack transfers; the grouped loops introduce no
stack accesses. The existing frame slot maximum is unchanged. Four chains
keep register demand fixed. The sixteen-element body contains sixteen
memory-source ADDs, four dependent adds per chain, followed by two induction
updates, compare and backedge. The original body has four memory-source ADDs
and the same four control instructions per four elements. Thus the main group
reduces control work per element without adding accumulator parallelism.
H's four-element tail has four memory ADDs to the same four registers and one
group backedge. Combination occurs once after the common scalar tail.

T adds one CMP/conditional dispatch and an unconditional jump over the small
body on the large path; no complete second proof/setup/cleanup is emitted.
H also guards the four-tail block. Costs depend on the remainder after peel.
For original count 512, 511 remain: D processes 496 grouped plus 15 scalar,
where H processes 496 + 12 grouped plus 3 scalar. That is a plausible cause
of H's gains, not a measured hardware attribution. Code layout also changes:
D's main group starts at kernel offset 0xbf; H/T64 at 0xc9 and T128/T256 at
0xcc. Larger gate immediates add three bytes. Alignment and frontend effects
can explain differences but were not isolated. Assembly is retained for every
mode and pressure. Bigger code has a measured allocation consequence below.

### Kernel execution results

All cells below are median execution deltas against each interleaved normal
4/4 reference, first run / separate repeat. Negative means faster. The full
42 execution rows are retained; compilation has two additional rows per
confirmed mode. Addresses 0 and 1 use identical data bytes and sum seeds.
2 MiB (262144 elements) fits this CPU's 16 MiB L3; the approximately 64 MiB
8388607-element buffer exceeds L3. No synthetic geomean is an application gain.

| Original count | H aligned | H unaligned | T64 aligned | T64 unaligned |
|---:|---|---|---|---|
| 0 | -0.31% / -0.07% | -0.14% / -1.43% | +0.19% / -0.31% | +0.56% / +0.31% |
| 8 | +1.13% / +0.49% | -1.41% / -0.28% | -0.07% / -0.14% | +0.45% / +0.02% |
| 16 | +0.22% / +0.52% | -2.06% / +1.32% | -0.37% / -0.05% | +0.17% / -0.58% |
| 17 | -0.79% / -0.27% | -0.09% / -0.42% | — / +0.29% | — / +0.49% |
| 33 | -1.64% / -0.99% | -1.03% / -0.87% | — / -1.44% | — / -0.65% |
| 63 | -4.31% / -2.90% | -3.40% / -2.72% | — / +0.43% | — / -0.18% |
| 64 | -3.41% / -3.51% | -4.19% / -2.17% | -0.36% / -1.28% | -0.09% / -1.69% |
| 65 | -4.98% / -4.57% | -5.50% / -1.06% | -4.82% / -4.78% | -3.05% / -3.80% |
| 66 | -5.30% / -5.66% | -3.65% / -2.02% | — / -5.19% | — / -3.83% |
| 127 | -11.23% / -8.78% | -8.44% / -6.72% | — / -7.12% | — / -4.06% |
| 128 | -10.77% / -8.40% | -7.50% / -7.48% | -6.30% / -6.43% | -4.29% / -4.58% |
| 129 | -7.69% / -9.00% | -10.62% / -8.02% | -9.01% / -9.82% | -8.31% / -8.60% |
| 130 | -9.01% / -10.49% | -10.72% / -8.01% | — / -9.38% | — / -7.92% |
| 255 | -12.26% / -13.03% | -9.73% / -10.94% | — / -10.56% | — / -7.62% |
| 256 | -13.61% / -12.51% | -10.88% / -12.26% | -11.41% / -12.21% | -8.75% / -7.48% |
| 257 | -13.05% / -12.94% | -11.76% / -12.72% | -13.31% / -13.14% | -11.37% / -12.12% |
| 258 | -12.04% / -14.10% | -12.69% / -10.16% | — / -12.78% | — / -10.79% |
| 512 | -15.26% / -15.41% | -12.16% / -12.46% | -13.79% / -14.06% | -11.11% / -10.94% |
| 8192 | -7.91% / -6.24% | +0.55% / +0.32% | -6.08% / -6.88% | +0.99% / +0.82% |
| 262144 | -7.56% / -6.49% | -11.06% / -11.03% | -6.92% / -8.32% | -9.84% / -10.34% |
| 8388607 | -5.94% / -2.68% | -12.36% / -0.13% | -1.29% / -2.34% | -0.84% / -3.40% |

The first threshold discovery matrix did not include counts 17, 33, 63, 66,
127, 130, 255 or 258; their T64 repeat is a single measured set, while native
correctness executes them. No missing sample was discarded.

H retains repeatable gains at aligned 128, 256, 512, 8192 and 262144 (p<.001
in both runs). Unaligned 128/256/512 and 262144 gains also repeat. Unaligned
8192 is +0.55% / +0.32%, neither significant; no gain is established. Short
8/16-element aligned calls have no significant loss in either run. Unaligned
16 is -2.06% (p=.068) then +1.32% (p=.017): the loss is not repeated, but
“all short regressions eliminated” is not established. The scalar/grouped
tradeoff after the peel matters; boundary rows are kept above.

T64 preserves small-input four-group behavior. At aligned 8/16 and unaligned
8/16 its repeated differences are not significant. Its complete gated-path
measurements bound the observed cost of selection; an isolated CMP latency
was not measured. T64 repeats useful aligned cache gains, but does not improve
unaligned 8192. There is no evidence that 64 is a universal profitable gate.
T128/T256 delay the large arm and add three bytes without a clear overall
advantage. Their complete discovery rows and compile samples remain in
[threshold-discovery](results/followup/threshold-discovery/comparison.csv) and
[threshold-compile](results/followup/threshold-compile/comparison.csv); only
T64 was selected for repeat before confirmation began.

Streaming results do not support a repeatable 5% gain. H aligned is -5.94%
(p=.512) / -2.68% (p=.015); unaligned -12.36% (p=.024) / -0.13% (p=.289).
The first streaming run has wide scatter and paired medians only -1.12% /
-4.37%. T64 aligned is -1.29% (not significant) / -2.34% (p=.024); unaligned
-0.84% (not significant) / -3.40% (p=.001). All unfavorable samples remain.
Loop-control savings are a smaller part of total time when memory dominates.

The supplementary same-binary D→H comparison gives aligned 16/64/128 changes
-1.19% / -4.73% / -5.14%, and unaligned 16/64/128/512 changes -4.41% /
-1.01% / -1.60% / -4.77%, all significant. Aligned 512, both aligned cache
sizes beyond that, unaligned 262144 and both streams are not significant.
**Unaligned 8192 regresses +3.40% (p<.001)**. H uses 57 more bytes than D.
This is one direct set, not an independently repeated H-over-D advantage.
It improves middle/remainder behavior but is not a universal new winner.
See [direct-D-H](results/followup/direct-D-H/comparison.csv).

### Compilation and allocations

Raw kernel compilation uses 26072 B/op and 29 allocations at pressure 0,
26296 B/op and 30 allocations at pressure 12 for all new modes, unchanged.
H compile time first/repeat is -0.66%/-5.24% at pressure 0 and -1.77%/-1.23%
at pressure 12, all nonsignificant. T64 is +1.21%/+0.73% and +2.79%/-3.32%,
all nonsignificant. These estimates do not establish zero compile-time cost.
The raw corpus backend and public CompileFull must be assessed separately.

The public `memory.wasm` compile reveals a real hidden cost: 23361 B/op and
108 allocations for baseline versus 25153 B/op and 110 for D/H/T64. The one-call
lifecycle is 24945/117 versus 26737/119. **Native execution still allocates
zero; public compilation adds two allocations and about 1792 bytes.**
The emitter has no new heap object, but larger native bytes cross an existing
code-buffer capacity boundary. The 80-byte Wasm bodies give capacity 496;
normal worker/join output 470/472 fits, whereas every new mode exceeds it.
Two existing append-growth sites allocate 896 bytes each.
`compile.go`'s worker arena append and final join account for the entire
increase in paired allocation profiles; see
[diff-sites.txt](results/followup/allocation-profiles/diff-sites.txt).
The raw backend uses its mapped arena and remains 11216 B/op / 29 allocs for
this corpus module. No allocator or capacity-policy change is included.

The other two gates have no public allocation timing; their larger bytes also
exceed the capacity, but a matching allocation cost is an inference only.

### Corpus costs and negative controls

The table retains every corpus row. A star means unpaired benchstat p<.05;
these exploratory stars do not prove practical importance. Primary columns
compare normal Wago with enabled candidates. The final column is a separate
same-tagged-binary control, not a replacement primary reference. No execution
gain in a zero-admission control is attributed to sum unrolling.

| Benchmark | D first | D repeat | H first | T64 first | Same-binary D |
|---|---:|---:|---:|---:|---:|
| Compile/memory | +0.90% | +0.17% | -2.14% | +1.75% * | -0.39% |
| Compile/linked_list | +1.08% | +0.58% | -1.34% | -0.92% | -0.39% |
| Compile/blake-as-simd | +0.36% | -0.34% | +2.26% | +1.40% * | +0.13% |
| Compile/utf-as-simd | +0.18% | +0.57% | +1.02% | +1.12% | -0.52% |
| Compile/yyjson | -0.13% | +0.38% | -0.42% | +0.60% | +0.97% |
| Compile/xxhash | +0.68% | -0.46% | -0.06% | +1.13% | -0.87% |
| Compile/drwav | +1.25% | -2.67% | -1.82% | +1.79% | +1.23% |
| CompileFull/memory | +5.04% * | +3.70% * | +4.70% * | +5.35% * | +2.70% * |
| CompileFull/linked_list | +3.55% * | +2.32% * | +1.28% | +1.95% * | +0.02% |
| CompileFull/blake-as-simd | -1.80% * | +0.15% | -0.69% | +0.82% | +0.24% |
| CompileFull/utf-as-simd | -0.05% | +0.30% | +0.58% | +0.52% | +1.14% |
| CompileFull/yyjson | -0.06% | +1.27% | +2.22% | -0.80% | -0.18% |
| CompileFull/xxhash | +0.51% | +0.20% | -0.92% | +0.27% | +0.92% |
| CompileFull/drwav | +0.15% | -0.33% | -0.79% | +1.25% | -0.45% |
| Exec/memory.sum | -9.91% * | -10.60% * | -11.64% * | -11.21% * | -10.57% * |
| Exec/linked_list.sum | +0.82% | -1.17% | +0.52% | -0.11% | -0.62% |
| Exec/blake-as-simd.hashN | -0.09% | -0.11% | -0.07% | -0.05% | +0.22% |
| Exec/utf-as-simd.convertN | +0.07% | +0.09% | +0.00% | -0.07% | +1.30% * |
| Exec/utf-as-simd.validateN | -0.06% | +0.67% | +0.08% | +0.31% | +0.25% |
| Exec/yyjson.yyjson_run | -0.64% | +0.49% | -0.60% | -0.20% | -0.24% |
| Exec/xxhash.xxhash_run | +0.55% | -2.02% | -0.71% | -0.64% | -0.11% |
| Exec/drwav.drwav_run | +1.13% | +0.03% | +0.66% | -1.02% * | +0.74% |
| SumUnrollLifecycle/memory | +1.43% * | +1.41% * | +0.98% | +1.73% * | +0.88% * |

D's synthetic memory.sum gain repeats, as do its public compile and one-call
lifecycle losses. The linked_list public compile loss is +3.55% then +2.32%,
both significant; T64 first is +1.95% (p=.004). The same-binary D control
is +0.022% (p=.947). This supports a compiler-binary/init/layout contribution
to the cross-binary loss but does not establish its cause or prove that mode
overhead is zero. Same-binary utf convert is +1.30% (p=.020), and T64 first
raw blake compile is +1.40% (p=.024). These adverse rows are not discarded.
Other isolated small control gains are not optimization evidence.
The normal production emitter and wrapper sources have no continuation diff.
Disabling experiments has no new dispatch or emitter code in the normal build;
there is no reason to infer a production regression from enabled-only costs.

The separate memory repeat includes all four timers, not just execution:

| Timer | H first / repeat | T64 first / repeat |
|---|---:|---:|
| Compile/memory | -2.14% / +3.59% * | +1.75% * / +0.24% |
| CompileFull/memory | +4.70% * / +6.80% * | +5.35% * / +8.72% * |
| Exec/memory.sum | -11.64% * / -11.41% * | -11.21% * / -11.85% * |
| SumUnrollLifecycle/memory | +0.98% / +4.11% * | +1.73% * / +3.45% * |

H repeat raw compile is +3.59% (p=.028), public compile +6.80% (p=.009),
and lifecycle +4.11% (p=.043). T64 repeat public compile is +8.72% and
lifecycle +3.45% (both p<.001). Both execute about 11–12% faster (p<.001
in both runs). Thus the observed execution benefit does **not** make a
compile-and-run-once workload faster. Public repeat B/op is 23360→25153,
an observed 1793-byte delta; earlier medians are 23361→25153, 1792 bytes.
Both have 108→110 allocations. Do not normalize the raw one-byte rounding
difference away; the profiles locate two 896-byte growth allocations.
Lifecycle bytes/counts remain 24945/117→26737/119.

Median-only amortization for reused synthetic sum(512) is roughly D 84–121
executions, H 95–141, and T64 111–184 to recover extra public compile time.
This divides public compile delta by execution-only time saved and rounds up.
It excludes additional application work and uncertainty; it is not an oracle
for profitability. The raw kernel compile deltas are not significant, so they
do not give a credible added-cost break-even. Higher real call counts could
amortize compilation, but eligible real call counts were not found.

### Decision table

Execution and public compilation below are synthetic corpus memory.sum, first
run / independent repeat, relative to normal Wago. Native size is the low-
pressure kernel including its adapter. Runtime execution allocates zero in
every case. Public allocations list bytes/count; raw backend allocations stay
unchanged. Additional gates have discovery-only kernel evidence.

| Candidate | Execution | Compilation | Allocations | Native size | Decision |
|---|---|---|---|---:|---|
| Existing 4/4 | Reference | Reference | Public 23360–23361/108 | 347 B | Production reference |
| Existing experimental D 16/4 | -9.91% / -10.60% | Public +5.04% / +3.70%; raw not significant | Public 25153/110; +2 allocs | 410 B | Retain simplest experimental reference |
| H 16/4 + 4 tail | -11.64% / -11.41% | Public +4.70% / +6.80%; raw repeat +3.59% | Public 25153/110; +2 allocs | 467 B | Useful middle/cache gains; further evidence needed |
| Threshold-gated T64 | -11.21% / -11.85% | Public +5.35% / +8.72%; raw first +1.75%, repeat not significant | Public 25153/110; +2 allocs | 462 B | Do not prefer the gate for production |
| T128 discovery | Aligned kernel512 -13.99%; no corpus timer | Raw kernel -0.22%, not significant | Raw kernel26072/29; public unmeasured | 465 B | No separate repeat; not preferred |
| T256 discovery | Aligned kernel512 -13.94%; no corpus timer | Raw kernel -4.30%, not significant | Raw kernel26072/29; public unmeasured | 465 B | No separate repeat; not preferred |

**Investigate further. Keep 4/4 as the production default and D as the
smallest reference experimental winner.** H is the promising new tail design:
it retains aligned cache gains and improves several middle/remainder cases
over D. The direct comparison also finds an unaligned8192 loss, and H adds
57 bytes over D. It does not dominate D. T64's small-call behavior is useful
but is not justified by a real workload and adds 52 bytes over D. Higher gates
delay useful groups without a clear additional benefit. No gated hybrid was
justified. No candidate has verified application gains, unchanged public
allocations, or a faster one-call lifecycle. **Do not promote any candidate.**

The final question has a limited answer: the hybrid and one gate retain useful
cache-resident synthetic gains and improve many short/middle cases, but the
evidence does not establish elimination of short losses, real-application
gains, or preservation of public compilation costs/allocations. Streaming
gains are inconsistent. Stop this tuning study; retain all evidence and use
real coverage to decide any future recognition or production work.

### Independent reviews and final validation

Two independent agents performed read-only reviews. The correctness review
checked ownership/cleanup, flags and branch targets, range/wrap behavior,
trap/fallback paths, native oracle coverage and the test bridge. Its threshold
boundary finding was fixed by adding original counts 66/130/258, and its
shared-record concern was fixed by a private scalar-argument setter. No
blocking correctness finding remains. See
[correctness-final-review.txt](results/followup/correctness-final-review.txt).

The performance review checked raw samples, pair order, normal Wago references,
selection, medians, allocations, native bytes, significance and amortization.
Its control-code hash evidence finding was fixed with the separate audit,
without replacing original diagnostics or changing timed code. All adverse
short/cache/streaming and control results remain visible. See
[performance-confirmation-review.txt](results/followup/performance-confirmation-review.txt)
and [performance-corpus-final-review.txt](results/followup/performance-corpus-final-review.txt).
Claude Code was attempted with the requested read-only Opus command but
returned `Not logged in`; no Claude review occurred. See
[claude.txt](results/followup/claude.txt).

Passed checks: native oracle/selection/fallback tests for all
six modes with codegen statistics and register checks, full checked bench
module, full tagged AMD64 suite with `WAGO_SHARED_SCALAR=0`, guard-page
runtime/public API, `just lint`, `just docs`, tagged vet, Python syntax, and
checked ARM64 cross-build. The full ordinary/checked `just test unit` rerun and
corrected shellcheck also passed. All attempt and rerun statuses are recorded in
[check-status.txt](results/followup/check-status.txt).

The first unit attempt failed in TinyGo VCS stamping. The rerun uses the
established `GOFLAGS=-buildvcs=false`; the first log is retained. The first
hash-audit attempt used the wrong working directory and failed before running
the test; corrected audit logs are retained. Initial shellcheck flagged an
unquoted comma-list array argument; it was quoted and rerun. No timing sample
was replaced by these checks.

The full normal shared-scalar statistics suite still fails with the exact
same 86 test/subtest names as phase 1. See
[baseline-failure-comparison.json](results/followup/baseline-failure-comparison.json).
No unrelated compiler repair is included. Draft smoke and CI passed on the
implementation head `1a4c6e1e`; final pushed-head draft status is recorded on
PR #910 after push.
The full native OS/architecture, race/fuzz and conformance CI matrix is skipped
by draft policy; it was not run locally. ARM64 is a cross-build, not native
execution. Hardware counters, controlled layout, enabled execution timing
with extra live parameters, another CPU, and eligible application gains remain
unvalidated. These limits rule out promotion.

### Follow-up reproduction and retained evidence

Use a new output directory and an allowed CPU ID:

```sh
experiments/specialized-sum-unroll/followup.sh /tmp/wago-sum-followup-results 2
python3 experiments/specialized-sum-unroll/scan-r3.py --out /tmp/wago-r3-search
```

The follow-up script uses current normal Wago as its primary reference and
builds/tests all binaries before sampling. It retains initial discovery,
confirmation, direct D/H and same-binary control comparisons. Diagnostic JSON
now records native SHA-256. The first historical D corpus run's older binary
is identified above; new reproductions use the final bridge. Allocation
profile commands are in `build.json`. Existing `run.sh` still reproduces phase
1. [evidence-index.json](results/followup/evidence-index.json) lists all ten
follow-up sets, 353 comparison rows, hashes and windows. Each row has 20
samples per side; CSV/benchstat/raw/order/environment files are retained.
Original phase 1 evidence was neither rewritten nor removed.

The two recorded follow-up ideas remain SIMD map emission without module
cloning and broader reduction recognition supported by real coverage. Neither
is implemented, and no additional PR is opened. PR #910 remains draft for
human review. Do not merge or enable a new default from this experiment.
