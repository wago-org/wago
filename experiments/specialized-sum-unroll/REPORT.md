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

## Phase 3: bounded mitigation study

This study continues at `975247c4a`; it does not replace either earlier phase.
The fixed plan is [MITIGATION-PLAN.md](MITIGATION-PLAN.md). Tests were committed
first at `7cb15e21d`, followed by paired-tail emission at `2540e0c38`, bounded
buffer reservation at `b21f51545`, and the corpus selector correction at
`38eff78fb`. All implementations remain private, build-tagged experiments.

### Designs and limits

P keeps D's 16/4 main group. It changes only the remaining scalar work after
Wago's first-iteration peel. An odd remainder enters the second half of a
pair. Each load has native displacement zero and each address/count update
uses 32-bit arithmetic. This preserves wrapping on the short-count dispatch
that bypasses the main-group wrap check. The pair uses the original accumulator
and one of the three existing partial sums, which the common exit combines
once. It adds no temporary register, allocation, threshold, or four-load body.
The conservative latch budget increases from 448 to 480 bytes.

DR keeps D's native instructions and changes a bounded code-buffer estimate.
It reuses function hints already available in the existing module body-size
loop. A small (at most 2048 Wasm body bytes), i64-first-result function with
memory and a loop can request 128 extra logical bytes. Calls, tail calls,
SIMD and bulk memory disqualify the hint. Total headroom is at most 1024
logical bytes per module. This is a sizing predictor, not exact recognition.
It never enables an emitter or changes source Wasm. It is active only for
one-worker, deferred, noncompact compilation. Effective WAGO_COMPACT and
implicit memory-pressure callbacks disable it. Explicit callback thresholds
are unchanged. Constant false/zero stubs inline in normal builds; see
[default-inline-summary.txt](results/mitigation/default-inline-summary.txt).

The cap is not a total allocated-byte bound. Worker and join buffers can each
use the estimate; Go rounds allocation sizes, and large serial heap compilation
can reduce its initial capacity. `memory.sum` changes the two initial size
classes from 512 to 640 bytes, avoiding their later growth. Thus the expected
cost relative to 4/4 is 256 bytes, while two reallocation objects disappear.
PR combines P with the same reservation. No new threshold or layout search
was performed.

### Correctness, code and coverage

All seven tested modes (baseline, D, P, DR, PR, H, T64) pass the selected native
oracle, wrap, trap, exit-local, overflow, pressure, selection and fallback
matrix with register checks. The 4 GiB tests execute; they are not skipped.
P/PR tests require the paired-tail marker. Atomic register-pressure rejection
and the 479-byte budget fallback are tested separately. The fallback is also
executed against the independent oracle. Deferred reserved/unreserved modules
have identical code and entries and execute against the oracle. Callback
counts and bytes match at thresholds 0, 1 and 1024. Public allocation tests
were first recorded failing before reservation, then passing after it.

The assembly has an identical 100-byte main group/backedge in D and P. P's
pair uses seven instructions for two loads instead of eight, but pays one
extra parity test and branch at entry. Both address and count updates remain
per-load. The tail has two accumulation chains rather than one. Any timing
change must therefore be assessed against the parity cost and changed exit
layout; fewer branches alone is not proof of a win. The pair adds 25 native
bytes. Kernel sizes at pressure 0/4/12 are D=410/457/577 and P=435/482/602;
frames remain 56/88/168. There are no new operand spills/reloads. The corpus
sum function is 312 bytes for baseline, 375 for D/DR, and 400 for P/PR; its
frame is 40 bytes in every mode. Whole memory-module sizes are 472/535/560.
DR native bytes equal D; PR equals P.

The eight negative controls have identical native SHA-256 values across all
five modes and zero specialized-sum admissions. `seqtk-fastq-to-fasta` is a
real module with a false-positive sizing hint: local function 5 requests
128 logical bytes but no exact sum emitter runs. Its large serial heap path
reduces that increment to about 96 requested bytes; allocation rounding can
absorb it. It does not test a worst-case allocation-class boundary.

The separate hint scan covers 121 checked-in `.wasm` files, with 120 decoded
and one explicit disabled-exception-handling exclusion (MicroPython).
Fourteen modules request headroom; thirteen are false positives relative to
the earlier exact-recognizer corpus scan. Raw paths, hashes, local indices,
base capacities and exclusions are in
[headroom-corpus.json](results/mitigation/admission/headroom-corpus.json).
This differs from the earlier catalog's 125 records: this scan counts files,
not workload entries. There is still no eligible real application that proves
an execution benefit. `memory.sum` remains a synthetic corpus kernel.

### Measurement method

All timing binaries were built before measurement. They have no diagnostic
or register-check tags. Every decisive row has 20 serial interleaved pairs,
with alternating order, GOMAXPROCS=1, CPU 2, and a 100 ms benchmark window.
CPU is AMD Ryzen 7 8845HS; exact Go, OS, flags, affinity, environment, binary
hashes and order are saved per run. Inputs and runtime options are unchanged.
Kernel execution excludes compilation/setup. Public compile and the complete
single-call lifecycle are separate. The old normal corpus binary is frozen
from phase 2 for the disabled-experiment control. Its identity is retained in
[build.json](results/mitigation/build.json).

`benchstat` supplies unpaired significance; CSV/JSON also retain paired
ratios. P-values are not adjusted for the multiple comparisons. Isolated small
gains require independent repetition and a useful practical effect.
Nonsignificance is not proof of equivalence. All adverse rows remain.
A failed initial corpus selection used `seqtk` instead of its catalog ID;
that attempt failed before a timing comparison and is retained separately.
The corrected selector is `seqtk-fastq-to-fasta`. No sample was removed or
replaced from a completed comparison.

Normal-build machine code was checked separately. The original 58-byte
body-size loop is byte-identical and contains no experimental branch or call.
The complete compile function remains 18762 bytes with a 3360-byte frame,
but its stack-slot and relocated data addresses differ. Do not infer whole
function byte equality from the loop check. See
[default-compiler-code.json](results/mitigation/default-compiler-code.json) and
the compressed old/new assembly files. The disabled control compares normal
binaries and finds no statistically significant time row or allocation-count
change among 25 rows. Small B/op fluctuations remain; this is evidence of no
detected regression, not a proof of equivalence.

### Result and cost table

Negative execution/compile percentages mean faster. A star means benchstat p<.05. The D row preserves phase 2 results; it is not a fresh simultaneous normal-to-D comparison. P public results are one 20-pair cost-qualification run, not a promotion repeat. All rows below concern the synthetic corpus `memory.sum` at 512 elements.

| Candidate | Execution vs normal | Public compile vs normal | Public bytes / allocations | Sum native bytes | Decision |
|---|---:|---:|---:|---:|---|
| Existing 4/4 | Reference | Reference | 23360–23361 / 108 | 312 | Keep default |
| Existing D 16/4, phase 2 | -9.91%* / -10.60%* | +5.04%* / +3.70%* | 25153 / 110 | 375 | Main-loop reference |
| P: paired tail | -6.06%* | -0.04% | 25153 / 110 | 400 | Reject added tail complexity |
| DR: 16/4 + reservation | -9.57%* / -10.12%* | +0.00% / +1.33%* | 23617 / 108 | 375 | Best mitigation; investigate further |

PR (pair + reservation) passes correctness and byte-identity checks but is not timed. P did not earn a combined performance experiment under the frozen [confirmation rule](results/mitigation/confirmation-plan.txt). No new threshold was tested. Earlier H/T comparisons and all original evidence remain above.

DR restores the baseline allocation count with 256 extra bytes (+1.10%) for public compilation. Relative to D, the measured public compilation reduction is 1536 bytes in the first run and 1537 in the repeat, with two fewer allocation objects in both. The repeat records 23616 B/op rather than 23617; this one-byte accounting variation is retained. Complete lifecycle allocations similarly fall from 119 to 117; execution stays at zero allocations. Its native instructions, frame and spills are exactly D's. The direct comparison isolates the allocation change:

| D → DR | First run | Repeat |
|---|---:|---:|
| Compile/memory | -0.31% | +5.01% |
| CompileFull/memory | -1.47% | +1.53% |
| Exec/memory.sum | -0.02% | -0.55% |
| SumUnrollLifecycle/memory | +0.69% | +3.31% |

The first normal-to-DR public compilation difference is not significant. The repeat is +1.33% (p=.027); it must count as a measured cost. A faster full lifecycle is not established. The reduction of two allocation objects is exact; no compile-time reduction was established.

Using each run's public-compile and execution medians, the added compilation time divided by the per-call saving gives the following rough recovery counts. These estimates are not statistically established break-even points; setup/lifecycle noise is much larger than a saved nanosecond per call.

- corpus-DR-run1: added compile median 0.5 ns; execution saving 9.325 ns/call; arithmetic recovery 1 call.

- corpus-DR-repeat: added compile median 305.0 ns; execution saving 9.820 ns/call; arithmetic recovery 32 calls.
- corpus-memory-P: added compile median -9.0 ns; execution saving 5.850 ns/call; arithmetic recovery 0 calls.

### Full paired-tail execution comparison

All original boundary rows are retained. Dashes mark cases not included in the fixed 22-row confirmation, not discarded samples. Each populated cell represents 20 pairs. Both references are needed: normal-to-P measures total benefit, while D-to-P measures the tail's added value.

| Address / count | 4/4 → P first | 4/4 → P repeat | D → P first | D → P repeat |
|---|---:|---:|---:|---:|
| addr0/n0 | -2.01% | -0.19% | -0.56% | +1.31% |
| addr0/n8 | +0.35% | +0.59% | -0.45% | -0.02% |
| addr0/n16 | +2.06%* | +2.35%* | -1.32%* | -0.42% |
| addr0/n17 | -0.18% | -0.56% | +0.04% | -0.63% |
| addr0/n33 | +0.40% | -1.92%* | -0.50% | -0.04% |
| addr0/n63 | +1.40%* | — | — | — |
| addr0/n64 | +1.91%* | -0.33% | +0.13% | +1.22% |
| addr0/n65 | -4.86%* | — | — | — |
| addr0/n66 | -5.44%* | — | — | — |
| addr0/n127 | -5.70%* | — | — | — |
| addr0/n128 | -4.76%* | -5.53%* | +0.75% | +1.08% |
| addr0/n129 | -7.79%* | — | — | — |
| addr0/n130 | -8.77%* | — | — | — |
| addr0/n255 | -8.98%* | — | — | — |
| addr0/n256 | -9.36%* | — | — | — |
| addr0/n257 | -13.11%* | — | — | — |
| addr0/n258 | -12.06%* | — | — | — |
| addr0/n512 | -12.44%* | -13.35%* | -1.36%* | +2.42% |
| addr0/n8192 | -5.65%* | -5.64%* | +0.59% | +0.42% |
| addr0/n262144 | -7.53%* | -6.55%* | -1.29% | -0.16% |
| addr0/n8388607 | -4.82%* | -3.52%* | -1.93% | +0.16% |
| addr1/n0 | +0.44% | +0.34% | +0.38% | +0.29% |
| addr1/n8 | -0.12% | +0.14% | +0.66% | +1.64%* |
| addr1/n16 | +2.11%* | +1.75%* | +0.49% | -0.02% |
| addr1/n17 | -0.49% | -0.94%* | -0.52% | -0.11% |
| addr1/n33 | -1.34% | -1.19%* | -0.23% | +1.02% |
| addr1/n63 | -0.16% | — | — | — |
| addr1/n64 | +0.51% | +0.90%* | -0.04% | -0.18% |
| addr1/n65 | -4.37%* | — | — | — |
| addr1/n66 | -3.58%* | — | — | — |
| addr1/n127 | -3.35%* | — | — | — |
| addr1/n128 | -2.03%* | -4.15%* | -0.25% | -0.34% |
| addr1/n129 | -7.78%* | — | — | — |
| addr1/n130 | -8.09%* | — | — | — |
| addr1/n255 | -7.00%* | — | — | — |
| addr1/n256 | -6.13%* | — | — | — |
| addr1/n257 | -8.64%* | — | — | — |
| addr1/n258 | -8.83%* | — | — | — |
| addr1/n512 | -9.41%* | -9.02%* | +0.10% | +1.31% |
| addr1/n8192 | -1.85%* | -3.22%* | +0.86% | +1.21%* |
| addr1/n262144 | -10.32%* | -8.87%* | -0.12% | +0.50% |
| addr1/n8388607 | +1.22% | +2.17% | -0.09% | +2.05% |

P still loses at count 16 in both alignments in both normal-Wago runs: aligned+2.06/+2.35%, unaligned+2.11/+1.75%, all significant. Aligned count 64 loses+1.91% in the first run; unaligned count 64 loses+.90% in the repeat. The initial aligned count 63 loss+1.40% is retained and not repeated. The main16/4 group gains remain at cache-resident sizes, but a parity check and per-load address/count updates limit the tail benefit. It does not achieve the requested short-loop recovery.

Neither initial significant direct gain repeats: aligned count 16 becomes-0.42% (p=.337), and count 512 becomes+2.42% (p=.229). The repeat instead loses at unaligned count 8 (+1.64%, p=.037) and 8192 (+1.21%, p=.008). These results do not justify treating all larger-loop gains versus 4/4 as tail gains. The unchanged 16-element main loop explains most of them. Large 64 MiB streaming data are mixed by alignment; no repeatable 5% improvement there supports a promotion claim.

P raw kernel compilation is measured only in the first full44-row set:

| Kernel compile | Time vs4/4 | Baseline B / allocs | P B / allocs |
|---|---:|---:|---:|
| SumUnrollCompile/pressure0 | +1.90% | 26072 / 29 | 26072 / 29 |
| SumUnrollCompile/pressure12 | -0.92% | 26296 / 30 | 26296 / 30 |

### Complete corpus and disabled controls

All 25 corpus/control rows are shown. `Compile` excludes decode/validation; `CompileFull` is the public pipeline. `seqtk` has no execution row here: it is a real-module compilation control, not a positive application result. The hint did not change its allocation count or measured size class. These results do not bound the worst possible false-positive byte cost.

| Workload | 4/4 → DR first | 4/4 → DR repeat | D → DR first | Old normal → new normal |
|---|---:|---:|---:|---:|
| Compile/seqtk-fastq-to-fasta | -0.06% | -0.39% | +0.50% | -0.37% |
| Compile/memory | -1.00% | -1.63%* | -0.31% | +3.64% |
| Compile/linked_list | -1.71% | -1.83%* | +0.56% | +0.14% |
| Compile/blake-as-simd | -0.02% | -1.35% | -5.29% | +6.14% |
| Compile/utf-as-simd | -2.60% | -4.97%* | -1.85% | +3.16% |
| Compile/yyjson | -1.63% | -2.04% | -0.70% | +0.04% |
| Compile/xxhash | -0.41% | +0.40% | -0.04% | +1.16% |
| Compile/drwav | +1.67% | +0.19% | +0.89% | +1.17% |
| CompileFull/seqtk-fastq-to-fasta | +0.76% | -4.86% | +0.04% | +0.88% |
| CompileFull/memory | +0.00% | +1.33%* | -1.47% | +0.75% |
| CompileFull/linked_list | +3.52% | +0.20% | +1.66% | +0.57% |
| CompileFull/blake-as-simd | +0.97% | -0.63% | -0.04% | +0.70% |
| CompileFull/utf-as-simd | -0.94% | -1.13% | +0.05% | +2.32% |
| CompileFull/yyjson | -2.31%* | -2.03% | -0.94% | -0.72% |
| CompileFull/xxhash | +0.46% | -0.92% | -0.93% | +0.22% |
| CompileFull/drwav | -0.91% | -1.43% | -0.57% | +0.96% |
| Exec/memory.sum | -9.57%* | -10.12%* | -0.02% | +0.04% |
| Exec/linked_list.sum | -1.24% | -0.67% | -0.18% | -0.66% |
| Exec/blake-as-simd.hashN | +0.18% | -0.02% | -0.86% | -0.26% |
| Exec/utf-as-simd.convertN | -1.26% | -0.69% | -0.60% | +0.03% |
| Exec/utf-as-simd.validateN | -0.49% | -0.25% | -0.15% | -0.40% |
| Exec/yyjson.yyjson_run | +1.14%* | +1.46%* | -0.36% | -0.44% |
| Exec/xxhash.xxhash_run | -0.57% | +0.32% | +0.23% | +0.12% |
| Exec/drwav.drwav_run | +1.02% | -0.07% | -0.52% | -1.39% |
| SumUnrollLifecycle/memory | +0.30% | +0.47% | +0.69% | -1.20% |

The zero-admission `yyjson` execution control loses+1.14% (p=.005) and+1.46% (p=.014) in the two normal-to-DR sets. Its guest code and sizing hint are unchanged. That rules out attribution to an emitted sum loop, but it does not erase the measured regression. Host-binary layout or another timing effect remains possible and unproved. The same-binary D-to-DR comparison and the disabled normal control have no significant timing row. Positive one-set control differences also remain unattributed. This evidence supports a bounded experimental allocation fix, not a claim that every adjacent workload is unaffected.

All 218 comparison rows, allocation medians and paired deltas are in [all-comparisons.csv](results/mitigation/all-comparisons.csv). The [evidence index](results/mitigation/evidence-index.json) records every set/window/hash. Each set retains raw samples, order, environment, benchstat, CSV and JSON. No unfavorable sample or earlier-phase evidence was removed.

### Decision

**Investigate further.** Keep the existing 16/4 emitter as the main-loop candidate and use the bounded reservation as the best mitigation in this study. It removes the known extra allocation objects without changing native execution. Do not promote it to a production default: eligible real-application evidence is still absent, short-loop losses remain, the repeat has a small public compile cost, and an adjacent control has a repeated small loss.

Reject the paired-tail change as the next optimization to adopt. It costs 25 extra native bytes without the required short-loop recovery or a broad, repeated advantage over plain 16/4. Keep its implementation and negative evidence opt-in for review. No further threshold, tail or layout tuning is included. A single fastest configuration does not exist across the measured sizes and alignments; the recommended tradeoff is D plus reservation, not a universal speed claim.

The two earlier follow-up ideas remain direct SIMD map emission without rewritten-Wasm allocation and broader reduction recognition justified by real workload coverage. Neither is implemented. No new public option, production default, ARM64 emitter change, worktree, or additional PR is introduced.


### Reviews, validation and reproduction

Two independent read-only reviews checked the emitter, ownership and cleanup,
wrap and trap paths, capacity bounds, callbacks, benchmarks, raw samples,
assembly and statistical claims. Their register-pressure coverage, effective
compact-mode gate, callback gate, corpus selector and unsupported-decoder
findings were addressed. No blocking finding remains. See the
[final correctness review](results/mitigation/correctness-final-review.txt)
and [final performance review](results/mitigation/performance-final-review.txt).

Passed local checks: full ordinary/checked unit suites, full checked bench
suite, all seven selected native matrices, DR/PR public allocation assertions,
legacy tagged AMD64 statistics suite, guard-page runtime/public API tests,
`just lint`, documentation links, tagged vet, shellcheck, shell/Python syntax,
and the checked ARM64 cross-build. See
[check-status.txt](results/mitigation/check-status.txt).

The full normal shared-scalar statistics suite still fails with exactly the
same 86 test/subtest names as the saved baseline, with no new failures. The
[comparison](results/mitigation/baseline-failure-comparison.json) and raw
failure log are retained; unrelated compiler repairs are outside this study.
The failed initial headroom decode scan is retained as a diagnostic-development
limit in the review; the final scan records its unsupported module explicitly.
The failed corpus selector attempt is retained with its raw output.

The full native OS/architecture, race/fuzz and conformance CI matrix remains
unrun under draft policy. ARM64 is a cross-build, not native execution. Hardware
counters, another CPU, controlled host code layout, and eligible application
speedups remain unvalidated. Draft smoke/CI status is checked on the final
pushed head and linked from PR #910; the PR stays draft for human review.

Claude Code was attempted with the requested read-only Opus command. It
returned `Not logged in`; no Claude review occurred. The failure is retained
in [claude.txt](results/mitigation/claude.txt).

Reproduce the fixed study with a new output directory and an allowed CPU:

```sh
experiments/specialized-sum-unroll/mitigation.sh /tmp/wago-sum-mitigation-results 2
```

An optional third argument supplies the frozen earlier normal corpus binary
for the disabled-experiment comparison. Without it, the script explicitly
omits that historical control; it still builds current normal and tagged
references and reproduces all candidate, direct and confirmation comparisons.
The saved old binary hash identifies this study's control. The script builds
and validates before timing, keeps all initial rows, and stops after the fixed
confirmation. Separate [analyze.py](analyze.py) recomputes medians and paired
ratios from raw files. The original run/followup scripts and their results are
unchanged.

## Phase 4: final application qualification

### Decision: close as “Useful but not practical”

The experiment has a repeatable synthetic gain. It does not have an admitted
real application with a measured benefit. The expanded search found no new loop
that the current recognizer can accept. Close PR #910 without merging or deleting
its branch. Keep production 4/4. Keep all experimental code, tests, reports and
raw measurements on the branch as research evidence.

This is a bounded negative result, not proof that no suitable application exists.
It supports closure under the user's stated decision rule. It does not support
production adoption of D or DR, or a recognition extension in this PR.

### Source baseline and PR #908

The branch started this phase at `6a06dbe1d6ed603e36ed464071d09f7ed620e91e`.
Fetched main was `796ccdea9e401097d0c73f8342b7075fbee1b843`. The shared historical
base remains `2286d676facdfa1cabe2d1c61072e505438ca7f2`.
Main adds [PR #908](https://github.com/wago-org/wago/pull/908), merge commit
`65194f23fc12222e006ccbf79d9880ca38182b23`, and PR #894's profile symbol escaping.

PR #908 combines instance leases and gate flags in one atomic word. It also
changes the first export cache and prepared-entry eligibility cache. Its published
26.2% ordinary-call gain is from an ARM64 M4 Max measurement. That percentage is
not an AMD64 result and is not used to adjust any result here. Its changed call
path can affect the public execution and lifecycle measurements in this report.
Neither main change touches the compiler, sum recognizer, bounds proof, or corpus.
The recognizer, bounds proof and production 4/4 function are text-identical to
current main; their hashes are in
[production-equality.json](results/qualification/production-equality.json).

No rebase, merge, new worktree, or history rewrite occurred. The prior samples
remain on their original source lineage. New yyjson controls use an ordinary
temporary source directory: `git archive 6a06dbe1`, then the same main-update
patch for both normal and tagged builds. The patch, binary hashes, Go version,
and hashes of 2,884 source files are retained in
[build.json](results/qualification/build.json) and
[updated-source-manifest.json.gz](results/qualification/updated-source-manifest.json.gz).
The compiler admission records use the unchanged branch compiler. No old sample
was relabeled as a current-main sample.

### Expanded research inventory

The limit was 256 MiB of pinned Git-hosted artifacts, one npm package below
160 MiB, the existing local corpus, and seven bounded CDN attempts. The actual
decoded inventory contains 467,426 function bodies and 435,740 loops. Artifacts
total 476,681,771 bytes, including the eight derived embedded cores.

| Source family | Artifacts | Core records decoded | Function bodies | Loops | Artifacts over 2 MiB | Exact recognizer bodies |
|---|---:|---:|---:|---:|---:|---:|
| Existing Wago corpus | 121 | 121 | 220,624 | 164,107 | 10 | 1, synthetic only |
| Sightglass | 120 | 118 | 78,424 | 83,481 | 6 | 0 |
| Published numerical / PolyBench / CHStone builds | 1,026 | 1,026 | 47,336 | 79,644 | 0 | 0 |
| All Wasm-R3 replay artifacts | 27 | 27 | 56,325 | 46,434 | 6 | 0 |
| DuckDB npm package | 3 | 1 | 64,575 | 62,046 | 3 | 0 |
| Core modules extracted from two Sightglass components | 8 derived | 8 | 142 | 28 | 0 | 0 |

There are 1,297 top-level artifacts, not 1,297 independent applications. The
1,026 published builds cover 42 program prefixes with different sizes and flags,
including numerical kernels, CHStone-style programs and an overhead control.
The eight embedded cores are derived from two entries already counted above.
This replaces the earlier 2 MiB external cutoff with complete collection scans.
The largest inspected application is DuckDB MVP at 41,325,187 bytes. Tract's
model benchmark is 27,992,676 bytes; Wasm-R3 Boa is 22,264,896 bytes.

The source families and their pins are:

- [Sightglass](https://github.com/bytecodealliance/sightglass/tree/a9023491c73d916ed329e4fd758cbae6380970a8),
  revision `a9023491c73d916ed329e4fd758cbae6380970a8`: the full committed Wasm set,
  including SQLite speedtest1, Rust JSON/HTML/protobuf, TinyGo JSON and regex,
  cryptography, compression, GCC loops, integer matrix work, image processing,
  and Tract inference. Wasmtime's main tree was also searched at
  `a8d33e523206646ca2850bac2744571b4827961b`; it has no committed `.wasm` artifacts.
  Sightglass is its upstream application benchmark source. The component-model
  online statistics and Kotlin Richards entries were unbundled with wasm-tools
  1.251.0 and all eight embedded core modules were decoded. Online statistics
  computes f64 Welford statistics through component calls; it is not this i64 sum.
- [Published benchmark builds](https://github.com/BenchmarkingWasm/BenchmarkingWebAssembly/tree/63e08d61ddb9ccbdba7e55aa25bfada728f550dd),
  revision `63e08d61ddb9ccbdba7e55aa25bfada728f550dd`: all 1,026 Wasm files.
  The Wago corpus's separate 30 PolyBench kernels were also decoded. Their
  common floating-point reductions do not establish demand for an i64 emitter.
- [Wasm-R3 artifacts](https://github.com/doehyunbaek/wasm-benchmarks/tree/bea10061c81428260ff029a9953273540a0c32e2/wasm-r3-bench),
  revision `bea10061c81428260ff029a9953273540a0c32e2`: all 27 replay artifacts,
  including Boa, FFmpeg, JSC, Parquet, browser graphics and games. Duplicate
  reduction-tool input copies were excluded. Arithmetic toys remain excluded
  from any application claim. The
  [Wasm-R3 paper](https://software-lab.org/publications/oopsla2024_Wasm-R3.pdf)
  explains how the replay collection was obtained; a replay name does not prove
  that a particular loop runs or has a useful count.
- [DuckDB-Wasm](https://github.com/duckdb/duckdb-wasm/tree/ef8a4f8912b6e7f62bc0cc490145ebd391b79e1f),
  npm `1.33.1-dev57.0`, git revision `ef8a4f8912b6e7f62bc0cc490145ebd391b79e1f`:
  the package integrity and SHA-256 are pinned. All three Wasm members were
  acquired. MVP was decoded and compiled in all three modes. COI and EH use
  legacy exception handling that Wago rejects; they are explicit exclusions.
  The MVP binary has no producers metadata, so its exact Emscripten version
  and unnamed function-to-source mapping are not claimed.
- Existing applications include SQLite, jq, Rust coreutils, ripgrep, seqtk,
  Brotli, XZ, age, Clang, Yosys, interpreters, and AssemblyScript JSON/UTF/hash
  modules. Their revisions, languages, toolchains, licenses and oracles remain
  in the corpus. The complete file/hash inventory is retained here as well.
- [Biowasm](https://github.com/biowasm/biowasm/tree/4afa546aa207172355fdf2f639167ea78ace539a),
  revision `4afa546aa207172355fdf2f639167ea78ace539a`: samtools 1.21,
  bcftools 1.10, bedtools 2.31.0, bowtie2 2.4.2, fastp 0.20.1, minimap2 2.22,
  and seqtk 1.4 were researched. Both public CDN URL forms returned HTTP 403.
  These seven modules were not acquired or scanned. The existing licensed
  seqtk corpus artifact was scanned and compiled. The manifest records the
  failed URLs and source submodule pins; no negative reduction claim follows
  from a failed download.
- The public Seb-C Go-runtime benchmark was inspected at
  `92d4a080f3bd61820a04a6e78dfa267539c6d9f5`. Its TinyGo program is add/Fibonacci
  without an array reduction. It was not built as a supposed application.
  Sightglass's actual TinyGo JSON and regex modules provide that language's
  artifact coverage instead.

The [DuckDB-Wasm paper](https://duckdb.org/pdf/VLDB2022-kohn-duckdb-wasm.pdf),
the Wasm-R3 paper, the published numerical suite, and upstream Sightglass
instructions supplied distinct application and benchmark sources. No research
paper is used as evidence that Wago selected the optimization.

[inventory.csv](results/qualification/inventory.csv) lists all 1,312 researched
artifact records: 1,297 top-level artifacts, eight derived cores and seven failed
CDN attempts. Eleven records are excluded from the core scan: two component
wrappers, two legacy-EH DuckDB modules and seven failed downloads. Component
wrappers have separate embedded-core coverage. Each acquired artifact has a
SHA-256; Git downloads also have a verified Git blob hash and pinned revision.
The manifests retain license scope and limits. Sightglass has MIT/Apache terms
with benchmark dependencies; DuckDB-Wasm's package is MIT with dependency terms;
Wasm-R3 artifact redistribution was not established. No new third-party binary
or source was added to the corpus. Existing corpus license checks remain intact.

### Instruction-aware admission analysis

The new [scanner](scan/main.go) uses Wago's module decoder and module-aware
instruction classifier. It reads actual instruction boundaries, immediates,
local types and nested loop ranges. It does not search arbitrary data bytes.
It separately checks the recognizer's exact suffix, independent of header shape.
Its broad straight-line recurrence screen is deliberately labeled a hypothesis:
it loses provenance across control, calls, stores and SIMD, and it can flag
address, hash and bigint work. Its hypothesis counts are not counts of true
reductions, eligible loops, or exercised application code.

Independent review found that the first exact-header test could miss harmless
header nops. This was corrected before the final scan. A regression compares
the scanner to the actual compiler marker: nops before/between the three header
instructions retain 4/4 admission; a nop after `br_if` prevents admission because
the exact body reader sees it. The header-independent body check also bounds
the negative result for other possible no-emission header forms. All final
scans still have one exact header and one exact body: synthetic `memory.sum`.
The earlier scanner outputs are retained and not used as the final count.

| Manually inspected case | Wasm function / expression PCs | Classification and precise rejection | Count / application exercise |
|---|---|---|---|
| DuckDB contiguous i64 sum | 8687, main loop 4779, tail 4854, sum write 4871 | Near: load precedes accumulator; address is `base + (i32.wrap_i64(index) << 3)`; i64 index and second counter increase; bottom `br_if`; main already sums four loads | Tail counter limit is `(end-start)&3`: at most three iterations by static inspection. Main count and reachability in a SQL workload were not measured |
| DuckDB pointer-gather sum | 4792, loop 185, write 198 | Near: `i32.load` gathers a pointer before `i64.load offset=48`; pointer-array stride 4; end-pointer comparison and bottom branch | No dynamic count or application trace; cannot use the current contiguous range proof |
| DuckDB tagged-record sum | 4192, loop 5002, write 5022 | Near: conditional exit on record tag; load offset 8; stride 32; pointer-end bottom branch | No dynamic count or application trace; cannot move loads past early exit |
| SQLite speedtest integer sum over typed records | 884, loop 25, write 65 | Near: stride-40 indexed records, flag-dependent branches, alternate call path, ascending index and nested control | Upstream speedtest1 is representative, but this unnamed function's dynamic exercise/count was not established |
| Tract shape-expression sum | 2532, loop 66, write 145 | Near: indirect call, tagged result, stores and early exit; load offset 8; pointer-end bottom branch; another live status local | Part of the upstream model application, but shape-sum reachability/count was not traced; observable effects prevent this proof |
| Sightglass rate-limit screen hit | 14, loop 296, write 450 | Unrelated to this reduction: SipHash-style XOR/rotate/add state with stores and calls, not a plain sum of i64 elements | No eligible reduction claim |

The decoded full ranges and local types are in
[loops/](results/qualification/loops/). Other screen hits were not promoted to
verified near matches. A module with no exact body is classified as “no current
recognizer match,” not as “contains no integer reductions.”

[Compiler diagnostics](results/qualification/admission/) cover 19 modules in
each of baseline, D and DR: 18 real application/benchmark/control modules and
the synthetic positive control. The positive control selects 4/4 once, or D/DR
once as requested. Every other module selects zero sum latches and has the same
native SHA-256 and size in all three modes. This includes DuckDB MVP, Tract,
SQLite speedtest, Rust JSON, TinyGo JSON/regex, GCC loops, RSA, Boa, FFmpeg,
Parquet, corpus SQLite/jq/coreutils/seqtk/AssemblyScript/PolyBench and yyjson.
These were compilations, not application execution claims. The explicit
optimization-disabled native oracle and source-preservation controls remain in
the existing correctness tests.

There is no qualifying workload to add to the corpus. Therefore no application
A/B timings, public compilation costs, lifecycle benefit, useful call-count
distribution or compile-recovery claim is manufactured. No downloaded guest
code was changed to force admission. No recognizer extension was made.

### Practical costs and retained synthetic results

The decisive synthetic measurements are the retained phase 2 and phase 3
results, not another tuning run. Negative H/T/P findings were not repeated.

| Candidate | Synthetic 512 execution vs normal 4/4 | Public compilation | Compiler memory / objects | Native module / sum function | Final decision |
|---|---|---|---|---|---|
| Existing 4/4 | Reference | Reference | 23,361 B / 108 | 472 / 312 B | Keep production |
| D 16/4 | −9.91% / −10.60% in phase 2 | +5.04% / +3.70% | 25,153 B / 110 | 535 / 375 B | Useful synthetic result; no practical adoption case |
| DR 16/4 with reservation | −9.57% / −10.12% in phase 3 | +0.0022% nonsignificant / +1.33%, p=.027 | 23,617 B / 108; direct D→DR repeat 23,616 B | 535 / 375 B | No application case; reservation defect remains |

These rows belong to different retained measurement phases and are not a new
simultaneous ranking. All execution rows have zero runtime allocations. The
sum frame stays 40 B with no new operand spill/reload counters. D's benefit
comes from less loop control with four independent sum chains. It adds 63 native
bytes to this sum and increases public compilation growth allocations. DR
restores object counts at a 256 B cost versus normal Wago in the primary sets;
it does not prove free memory use or faster compilation. DR's complete one-call
lifecycle changes +0.30% / +0.47%, both nonsignificant, and uses 25,201 B / 117
objects versus baseline 24,945 B / 117. There is no measured application break-even.
Known short-loop losses and the decline of the gain for large streaming buffers
remain in the previous tables. A cache-resident synthetic win does not establish
that production loops have the required structure, useful sizes or call frequency.

### yyjson control and host layout

All new sets have 20 alternating, serial pairs on CPU 2, `GOMAXPROCS=1`, Go
1.27.1, 150 ms per sample, the same input and CPU settings, and timing binaries
without diagnostic tags. Setup and compilation remain outside execution timing.
Normal and tagged builds use the same updated source. All output and benchstat
results are retained, including the noisy initial same-binary set.

| Control | Baseline median | Candidate median | Change | benchstat p |
|---|---:|---:|---:|---:|
| Same tagged binary, baseline → DR | 913.45 ns | 1,008.50 ns | +10.406% | .402 |
| Same tagged binary, independent repeat | 864.70 ns | 862.75 ns | −0.226% | .529 |
| Normal binary → tagged DR | 875.40 ns | 871.60 ns | −0.434% | .060 |
| Normal binary → tagged DR, independent repeat | 876.35 ns | 863.40 ns | −1.478% | .006 |
| Same binary and same baseline mode, placebo | 857.65 ns | 863.50 ns | +0.682% | .931 |

Each row has zero execution bytes and allocation objects. The first same-binary
set has ±20–30% time variation. It is not discarded and not treated as evidence
of equivalence. The stable repeat does not show a significant selection cost.
The old +1.14% / +1.46% cross-binary losses remain recorded; the updated runs do
not reproduce their direction. The significant new cross-binary improvement
cannot be assigned to a sum emitter that selected no latch.

Guest bytes and relative function offsets are identical. Saved `go tool nm`
records show different host symbol positions: for example `benchmarkExecBatch`
starts at `0xab85e0` in normal and `0xab9e20` in tagged. That establishes a host
layout difference, not its causal effect. Same-binary controls remove that
cross-binary difference. Absolute JIT placement and hardware cache effects were
not isolated. Noise and layout remain possible causes; no specific cause was
proved. This unresolved attribution is recorded as an experimental limit,
not as a sum-emitter regression or an application gain.

### DR reservation discontinuity

The defect is confirmed in current source. `compile.go:1926–1928` adds headroom
to `codeCap`; `compile.go:1954–1956` then reduces capacities at least 262,144
bytes by one quarter on the serial deferred-heap path.

For example, base capacity 262,016 plus one 128 B hint reaches 262,144, then
becomes 196,608. Without the hint it stays 262,016: added headroom reduces the
effective initial request by 65,408 B. The sizing formula reaches 262,016 with
32 functions and 52,288 body bytes; average body size 1,634 B also meets the
serial-heap condition. A false-positive hint can cause this. The reserve bound
does not make this boundary monotonic.

This affects allocation and compilation behavior, not native instructions,
results or explicit callback thresholds. Existing small capacity/callback tests
do not test this boundary. No eligible real workload justifies retaining DR, so
the task's stop rule applies: document the defect and do not polish the discarded
optimization. If a future study retains reservation, add failing tests below,
at and above the boundary and for false-positive hints first; apply serial
scaling before added headroom or otherwise prove a monotonic effective request.
Then remeasure allocation bytes/objects and callback behavior. No fix or normal
runtime check was added in this phase.

### Independent reviews, validation and limits

Separate read-only correctness and performance reviewers checked the new tools,
coverage, diagnostics, raw controls and conclusions. Correctness review found
the scanner nop issue and the DR boundary defect. The scanner issue and saved
type visibility were corrected, tested and rescanned; DR remains documented.
No new D semantic defect was found. Performance review checked all five control
sets, their 20 samples per side, alternating order, medians and benchstat values.
It confirmed zero admission in real modules and warned against counting build
variants as independent applications. Both reviews support closure and do not
approve production adoption. Their records are retained in this phase's results.
Claude Code was attempted in read-only mode but returned “Not logged in.” No
Claude review occurred.

Fresh checks passed: scanner tests, the actual-compiler nop regression, tool
package tests and vet, D and DR native correctness suites with codegen statistics
and register checks, including full 4 GiB memory32, scalar remainders, integer
overflow, live exit locals, trap metadata, source preservation and fallbacks.
Pair/hybrid-only tests correctly skip in the D/DR runs. Experimental artifact
tests without an output directory also skip; final admission data are from the
separate diagnostic command. Python syntax, full `just lint`, `just docs`, and diff whitespace checks pass.
The earlier affected unit, checked-native, guard, legacy, lint and ARM64
cross-build results remain preserved. The earlier ordinary shared-scalar suite
has its documented 86 baseline failures; it was not claimed fixed or rerun here.

The last draft CI before this phase, run `37960026881`, passed its smoke/summary
jobs. Its full matrix was skipped by draft policy. The new evidence commit's
actual CI state is reported in the final PR comment. No native second AMD64
machine or ARM64 measurement was available. The primary laptop still has boost,
desktop activity and an unisolated SMT sibling. No hardware-counter or absolute
JIT-placement causality was established. No downloaded application's dynamic
loop distribution or independent runtime oracle was measured because none
qualified. These limits bound the conclusion; they are not hidden proof of
equivalence or absence of all real reductions.

### Follow-up research, not implementation

1. Confirm source mapping, dynamic coverage and useful counts for real dense
   integer reductions, including already-unrolled DuckDB main loops. Only then
   consider a separate narrow recognition experiment. Pointer-end or ascending
   loops need zero-entry, count derivation, overflow and memory32 wrap proofs;
   gathers need per-load bounds and trap-order proofs. Calls, stores, tags and
   early exits cannot use this proof without new observable-effect reasoning.
   The tiny DuckDB remainder is not a reason to add deeper unrolling.
2. Keep the prior vector-map question separate: isolate PR #909's promising
   128-bit i32/f32 maps and assess direct native emission, CPU-feature
   profitability and avoidance of module cloning or rewritten-Wasm allocations.

No follow-up PR was opened. No production default or unrelated compiler behavior
changed. The final recommendation is **close #910 as Useful but not practical**.
