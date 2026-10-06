# Dragline AMD64 optimization checkpoint

Objective: **50% higher execution throughput than Railshot PR #780**,
confirmed by the user. The module-balanced paired execution-time ratio must be
at or below **2/3**, with complete semantic/result coverage. The pinned PR head
is `efa9aa22dfb3781284a55c465f7f57544eafade3`.

**Toolchain audit correction:** earlier PR comparisons silently mixed Dragline
Go 1.22.2 with PR Go 1.27.1. The PR benchmark module's `toolchain go1.27.1`
directive caused automatic selection; querying `go version` outside that module
did not identify the binary's actual compiler. The recorded 1.0061183383 ratio
in `native-wide-combined-full-02` is historical mixed-toolchain evidence and
**does not establish a matched-toolchain result**. Raw samples remain intact.

`pr780-baseline-02` rebuilds the identical pinned sources and harness with
`GOTOOLCHAIN=local` and `/usr/lib/go-1.22/bin/go`; embedded binary metadata confirms
Go 1.22.2, and the semantic gate passes. New runners check `go version -m` for
every measured binary. The prior `matched-full-08` reference comparison covers
all 65 modules / 72 exports. Its historical time / PR is **0.9723980261**, or
**1.0283854688 × PR throughput** (2.84% higher). That run's candidate many_funcs.run and PR
Doitgen are noisy (max/min 1.2395 / 1.4398). Both binaries pass all result gates.
The 50% target remains unmet.

**Current retained experimental reference: even constant divisibility.**
The [verified full comparison](divisibility-full-02/README.md) measures
**0.9159781825 times PR780 time, or 9.17% higher throughput**. Candidate/odd-only
control is0.9996433814 (only0.036%less time). UTF8proc repeats3.53%less time;
changed10geomean0.9931983654,unchanged55geomean1.0008196879. No series exceeds
20%spread. Code grows86bytes and compilation remains near neutral. Three small
changed-module regressions remain included. The50%target is unmet. Earlier9.26%
is a separate run, not a paired regression. Source is experimental, unpromoted.

**Previous retained reference: odd constant divisibility.** [Odd constant zero tests](divisibility-01/README.md)
passes 9,561,888 independent arithmetic calls per native policy (99,360 traps),
all 65/72 corpus cases and 21 semantic cases. Its [six-round paired screen](divisibility-screen-01/README.md)
shows LZ4 compression taking 9.10% less time (LZ4 module 4.82% less), with zlib
near neutral at 0.31% less. Only LZ4/zlib code changes, removing 19 bytes.
[Compile costs](divisibility-compile-01/README.md) are near neutral. The
[full 65/72 comparison](divisibility-full-01/README.md) completed and verifies
**0.9152816704 times PR780 time, or 9.26% higher throughput**. Same-binary
control ratio is 0.9992687891 (only 0.073% less time). LZ4 improves 3.98%;
zlib regresses 0.531%. All noisy series remain included. The 50% target remains
unmet. Prior 8.75% is from a separate run, not a paired improvement claim. [Profile reuse](integer-profile-reuse-01/README.md) verifies
the motivating samples against current retained code.

The [even-divisor extension](divisibility-02/README.md) passes local arithmetic,
AMD64/Rosetta and ARM oracles (15,278,832 calls each) and all 195 code comparisons.
It changes ten more modules, adding 86 bytes versus odd-only. Native qualification
passes all three policies (15,278,832 oracle calls each, 65/72 corpus and 21 semantic
cases). The [focused screen](divisibility-screen-02/README.md) measures UTF8proc
3.53% less time, with the other nine changed modules near neutral; no series
exceeds20%spread. [Compile costs](divisibility-compile-02/README.md) are near neutral.
The [full comparison](divisibility-full-02/README.md) completed and supports the
experimental retention reported above. The [consumer census](divisibility-census-02/README.md)
also records a signed Nussinov check; signed remainder is not changed.

[Native profiles11](native-profile-11/README.md) verify PCRE2, UTF8proc and NanoSVG
against the retained code census. NanoSVG has87/878 native samples at REP STOSB.
A [qword fill prototype](bulk-fill-01/README.md) passes376,832 local full-memory,
return and trap oracle calls per policy, including74,368 expected traps. It changes
25modules (+4076bytes), with all65 control images matching the retained parent.
Native qualification also passes376,832 calls per policy,65/72corpus and21semantic
cases. The [completed screen](bulk-fill-screen-01/README.md) rejects qword-only:
changed25time/control1.0037749923, with a noisy YYJSON control series retained.
No full or compile-cost comparison is warranted for that version.
The [fill-size diagnostic](fill-distribution-01/README.md) preserves AMD64 results
and full memory. It also documents a standalone ARM NanoSVG failure in unchanged
physical production sources, separate from this AMD64 emitter change.

The [guarded nested-loop prototype](nested-loop-01/README.md) now emits f64x2
for seven nests across 2mm, 3mm, GEMVER and MVT. Native AMD64 qualification passes
27,648 calls / 12,544 traps per policy, 65 modules / 72 exports, 21 semantic cases,
and compiler/runtime suites. All 65 controls match even02; four candidate modules
change (+6279 bytes). Local Rosetta execution covers fallback because it lacks
AVX2; forced-AVX2 compilation proves all 288 fixture transforms. The
[focused screen](nested-loop-screen-01/README.md) completed with25.37% less time
across the four changed modules and neutral controls. [Compilation costs](nested-loop-compile-01/README.md)
rise1.61–2.42x, so this remains experimental. The [full comparison](nested-loop-full-01/README.md)
completed session54313: experimental11.01% higher throughput than PR780, with
noisy series retained. The [scratch-local follow-up](nested-loop-02/README.md) is
natively qualified and lowers compilation time1.4–5.5% versus raw01, but the
large regression versus even02 remains. [Grouped guards](nested-loop-03/README.md)
pass expanded native qualification. Their completed screen shows mixed execution
(0.291% more time across four modules versus compact02) while compilation
falls12–19%; this remains an experimental parent. [Paired scalar loads](nested-loop-04/README.md)
now cover two additional GEMVER/MVT nests. All three native policies pass405,504
calls/167,520traps plus65/72corpus and suites. Screen and compile costs run
completed in81582: GEMVER/MVT use10.6%/13.1% less time versus grouped03,
with24.8%/33.5% higher compilation time. The [full04 comparison](nested-loop-full-04/README.md)
completed original16151 with exit0; all raw samples verify. Its calculated11.55%
throughput increase is too noisy for a new performance claim:215 of216 export
series exceed20% spread. All samples are retained; quiet repeat required.
The [bounded call-tree prototype](call-tree-01/README.md) changes six modules.
Three Rosetta oracle policies and native ARM fallback pass160,000calls/87,760traps
each; native AMD64 qualification completed72509 with all three65/72corpus gates,
21semantics and compiler/runtime suites. A fresh probe foundCPU7 mostly idle;
the matched [call-tree screen](call-tree-screen-01/README.md) completed32571:
1.026% less time across six changed modules, mixed module results and no series
over20% spread. [Compilation costs](call-tree-compile-01/README.md) completed5144: JSON costs58–61%
more time and over twice the allocations. This revision remains unretained.
A [direct-wrapper census](call-wrapper-census-01/README.md) identifies58 potential
wrappers across11modules for investigation; no emission or speed claim.
A [positive-stride source census](nested-gather-plan-01/README.md) finds two further
nests in GEMVER/MVT, without new module coverage or emitted code.
The [affine analyzer](nested-loop-plan-01/README.md) retains its independent
address and integer-exit trace proofs. A separate
[Railshot signals-mode constant-store bug](cross-page-store-01/README.md) is
reproduced on native AMD64, ARM64 and Rosetta; it is not a candidate failure.

A [code-preserving nested-loop census](nested-loop-census-01/README.md) records
30matching syntax groups across17modules, including2mm/3mm. All65images remain
identical to even02. This identifies shapes for investigating independent outer
outputs; no dependence, alias, bounds or trip-count proof exists yet. No nested
loop transform or performance gain is claimed.

[Constant scale multiplication](scale-mul-01/README.md) is locally and natively qualified on
the retained even-divisibility02 base. It tests LEA-only versus LEA-plus-shift
integer constant lowering; all three AMD64/Rosetta policies and native ARM each
pass3,126,256 modular arithmetic oracle calls. Candidate changes40modules,
+1428bytes; LEA-only changes11,+16bytes. All65retained images match even02.
Native qualification76627completed0: all three policies pass the arithmetic oracle,
65/72corpus,21semantic cases and compiler/runtime suites. The [focused screen](scale-mul-screen-01/README.md)
completed original session65108. Its small 0.168% aggregate improvement is deferred: changed-module outcomes are mixed and three control series exceed20% spread. No retention or full65/PR780 claim. This experiment does not include the fill prototypes.

The [constant-fill follow-up](bulk-fill-03/README.md) is locally and natively qualified.
Existing SSA proofs remove known-byte broadcast and known-length dispatch.
All three local AMD64/Rosetta policies pass327,960 constant-fixture calls and
157,176expected traps across799functions, plus the existing376,832dynamic calls.
ARM oracles and compiler/runtime suites pass. All65images match each control's
parent; candidate removes8,849bytes across25modules versus short-fill02. Native
qualification8406completed0: all three policies pass both oracles,65/72corpus,
21semantic cases and compiler/runtime suites. The [focused screen](bulk-fill-screen-03/README.md)
completed in original session23080 and rejects the candidate: changed25time/retained
1.0012647280, focused27 1.0011479849. Candidate Blake spread1.3160 remains included.
No compile/fullrun. These prototypes remain based on odd-only divisibility01.

The [short-fill follow-up](bulk-fill-02/README.md) also passes local qualification:
three policies each pass376,832 full-memory/return/trap checks, and195code images
verify both controls against their parents. It adds12,484bytes versus qword-only;
all three native policies also pass65/72corpus,21semantic cases and the full-memory
oracle. The [completed screen](bulk-fill-screen-02/README.md) rejects this version:
changed25time/retained1.0054751557; focused27 1.0049981358, with no series above
20%spread. NanoSVG regresses2.83%; no compile/fullrun for this version.
A [code-preserving constant census](fill-constant-census-01/README.md) identifies
known fill bytes/lengths whose generic setup may be avoidable.

**Direct-emission follow-up also rejected:** [Native scalar reductions](native-scalar-local-01/README.md)
passes all correctness gates, but the [six-round screen](native-scalar-local-screen-01/README.md)
regresses all six changed kernels by 7.77–30.09%; both unchanged controls remain
neutral. [Compile time](native-scalar-local-compile-01/README.md) is near control
(0.997–1.012 times), so this avoids the earlier rewrite's compile cost without
establishing an execution gain. It adds 14,776 native bytes. The retained PR780
aggregate below is unchanged. [Profile 10](native-profile-10/README.md) records
the earlier rewrite's executed loop; these screens do not prove the regression's cause.

**Latest experiment rejected:** [Scalar local-carried reductions](scalar-local-01/README.md)
passes 407,952 oracle calls per native policy and all 65/72 corpus cases, but its
[six-round screen](scalar-local-screen-01/README.md) regresses all six changed
kernels by 5.61–28.94%. It adds 10,768 native bytes and takes
[1.89–2.56 times the control compile time](scalar-local-compile-01/README.md).
Both unchanged execution controls remain near neutral. The
[code-preserving admission census](compute-loop-census-01/README.md) and
[GEMM native profile](native-profile-09/README.md) document the investigation.
The retained reference and full-suite PR780 score below are unchanged.

**Previous retained experimental execution reference:**
[Aligned SIMD literal data](vector-literal-align-01/README.md), based on rotate01.
Its [six-round full comparison](vector-literal-align-full-01/README.md) measures
**0.9195803910 times PR780 time**, or **1.0874525053 times throughput (8.75% higher)**.
It takes **0.055% less time** than the same-executable retained control. UTF SIMD
repeats its focused gain at0.9833984208 (1.66% less time); validation improves3.51%
while conversion regresses0.23%. Blake SIMD0.9972804317 and JSON SIMD1.0000453042
are near neutral. Changed3 average0.9935479380; unchanged62 average0.9997336756.
The aggregate difference is tiny. Noisy Doitgen, Jacobi2D, LU and LUdcmp series
remain in the score; see the full report for every variant's spread. Earlier8.82%
is a separate run, not a paired regression. Separate PR executable layout and
host variation limit absolute comparisons.

Both native policies pass65 modules/72 exports,21 semantic cases and268,800
independent full-memory/trap oracle calls. Local ARM and actual serial/parallel
literal alignment/cache checks pass. Only three code images change (+13 total
bytes). [Changed-module compilation costs](vector-literal-align-compile-01/README.md)
are near neutral; the unchanged Memory control is7.92% slower versus the previous
executable, limiting broader claims. The **50% target remains unmet**.
Production v77 is unchanged; no promotion, commit or push occurred.

**Previous retained reference:**
[AVX512VL vector rotates](vector-rotate-01/README.md).
Its [complete six-round comparison](vector-rotate-full-01/README.md) measures
**0.9189740075 times PR780 time**, or **1.0881700591 times throughput (8.82% higher)**.
It uses **0.063% less time** than remainder01 in the same executable. The only
changed module,Blake SIMD, repeats its focused gain at0.9625786703 (3.74% less);
unchanged64 average0.9999532972. Candidate JSON/JSON SIMD serialization and
control Doitgen exceed20% spread; all samples remain. Earlier8.54% and7.18%
headlines are separate runs, not paired deltas. Separate executable layout and
host variation limit the absolute PR comparison.

Both policies pass65 modules/72 exports,21 semantic cases and88,704 independent
full-memory/trap oracle calls. Native compiler/runtime and localARM64 gates pass.
Code shrinks2208bytes; [Blake SIMD compile time](vector-rotate-compile-01/README.md)
falls about19.8% with0.74% more allocated bytes. The **50% target remains unmet**.
Production v77 is unchanged; no promotion, commit or push occurred.

**Latest rejected experiment:** [SIMD constant operands](vector-constant-screen-01/README.md).
Both policies pass 268,800 oracle calls, all 65/72 exports, 21 semantic cases,
and compiler/runtime gates. Only UTF SIMD changes, shrinking 224 bytes, but the
six-round paired screen regresses validation 7.67% and the module 3.78%.
Conversion and unchanged controls remain near neutral. All samples are archived.
[Native PC profiling](native-profile-08/README.md) is complete; it does not
establish the regression cause. The [exact executed-plan diagnostic](utf-validation-plan-01/README.md)
preserves code and avoids using an unselected emission trial as evidence.

The [Boolean-tree experiment](vector-boolean-screen-01/README.md) is rejected:
UTF validation regressed 7.92%, while JSON SIMD was neutral. All raw samples,
including a noisy retained JSON deserialization series, remain archived. The
retained reference above is unchanged.

Older checkpoint figures below retain their original comparisons and decisions.

**Prior retained experimental reference:** [leaf-density01](leaf-density-01/README.md).
Its [full six-round comparison](leaf-density-full-01/README.md) measures
**0.9612940105** times PR780 time, or **1.0402644654 × throughput** (4.03% higher).
It uses 0.350% less time than large-leaf02. All 65/72 result gates and fresh
native/ARM checks pass. Changed Monocypher, NanoSVG and PCRE2 gains repeat;
unchanged-code shifts, notably many_funcs at -8.81%, also contribute. No series
exceeds 20% spread, but that does not eliminate placement/order variation.
The [compile-cost report](leaf-density-compile-01/README.md) covers five changed
modules and two controls: changed compile-time ratios are 0.97977–1.00243, with
1,296 fewer native bytes. Earlier large-leaf compilation costs remain archived.
Production v77 is unchanged and the 50% target remains unmet.

The [completed segmented-threshold full comparison](segmented-threshold-full-02/README.md)
remeasures the same retained leaf-density01 at **1.0430297472 × PR780 throughput**
(4.30% higher). This is measurement variation from the earlier 4.03%, not a new
optimization. Threshold revision02 is not retained: full time / retained is
1.0011498869 despite a repeated LZ4 gain. Unchanged many_funcs shifts +9.50%;
several QOI, Blake and Doitgen series exceed 20% spread. All raw results remain.

Earlier experiment: [bounded shared liveness](segment-bitset-01/README.md).
It passes full native/ARM correctness and changes nine modules relative to
threshold02, with 2,380 fewer bytes. Its [separate-binary screen](segment-bitset-screen-01/README.md)
measures 1.0114427754 times retained time, dominated by a 20.33% regression in the
unchanged memory control. Changed-module results are mixed. No series exceeds
20% sample spread, which does not rule out a systematic layout effect.

A [single diagnostic executable](segment-policy-single-binary-01/README.md)
now selects all three policies at process start. All [195 native-code/input
comparisons](segment-policy-code-census-01/README.md) match their references;
all 65/72 native results and 21 semantic cases pass separately in every mode.
The [one-binary twelve-module screen](segment-policy-single-binary-screen-01/README.md)
is complete: time / retained is 0.9942475784 and memory is 0.9985765782.
The previous unchanged-memory regression disappears with the executable held
fixed, while NanoSVG still regresses 4.08%. No series exceeds 20% spread.
Bitset01 remains unretained; this focused result does not establish a complete
corpus gain. Its prepared full compile/performance comparisons have not started.

A separate [giant-function diagnostic](giant-segment-census-01/README.md)
completes liveness for PCRE2 function60 and yyjson function18 without changing
emitted code. Their bitmap/queue scratch totals are about 14.2/9.2 MB. No giant
runtime gain has been established. The new [giant-only prototype](giant-bitset-01/README.md)
uses those ranges with the fast linear allocator. Local AMD64/ARM gates pass;
all 65/72 native results and 21 semantic cases pass, as do the independent
memory/arithmetic gates. Its completed [four-module screen](giant-bitset-screen-01/README.md)
finds PCRE2 2.72% slower and yyjson 3.41% faster. Unchanged memory shifts +18.66%
with 28.83% sample spread; aggregate attribution is limited. The prototype remains
unretained and no full run follows. Its [static census](giant-bitset-code-census-01/README.md)
changes only PCRE2 and yyjson, but both need larger frames. Production v77 remains
unchanged; leaf-density01 was the reference for that experiment.

The follow-up [giant02](giant-bitset-02/README.md) uses exact conflicts for
call-crossing promotions. It passes full native and fresh ARM gates. Promotions
recover from 75 to 121 in PCRE2 and 85 to 123 in yyjson; code shrinks 4,256 bytes
versus giant01, but frames remain larger than retained.
A [single executable](giant-policy-single-binary-01/README.md) selects giant02,
giant01 and retained modes. All [195 code/input comparisons](giant-policy-code-census-01/README.md)
match. Native qualification passes in all modes. The [controlled screen](giant-policy-single-binary-screen-01/README.md)
rejects giant02: time / retained is 1.0124339833, PCRE2 regresses 4.18%, and yyjson
improves only 0.64%; both are slower than giant01. No sample series exceeds 20%
spread. Giant01 remains unretained at 1.0011824804 times retained time. No full
run follows these prototypes. Leaf-density01 was their execution reference.

Retained follow-up: [two bounded recursive-inline levels](recursive-depth-01/README.md)
on AMD64, retaining the existing pure-integer eligibility and ARM's one level.
It passes full native correctness and local AMD64/ARM checks, including 24,552
independent recurrence calls per architecture. Its [65-module census](recursive-depth-code-census-01/README.md)
changes only fib_rec: 206 to 373 bytes, no spill slots. A [diagnostic executable](recursive-policy-single-binary-01/README.md)
reproduces both policies in all [130 code/input comparisons](recursive-policy-code-census-01/README.md).
Both modes pass native qualification. The [controlled screen](recursive-policy-single-binary-screen-01/README.md)
measures Fibonacci at 0.7503852019 times retained time (24.96% less), with all
three unchanged controls within 0.13% and no series above 20% spread.
[Compile measurement](recursive-policy-compile-01/README.md) finds Fibonacci
time +56.9%, allocated bytes +38.8% and allocations +12.7%; its median compile
time rises 0.309 to 0.484 ms. The retained dispatch compile control has 20.11%
spread. The [full 65/72 comparison](recursive-policy-full-01/README.md) verifies a
0.318% time improvement versus leaf-density01 and 4.00% higher throughput than
PR780. The two-level source is now the retained experimental reference.

A parallel [three-level prototype](recursive-depth-02/README.md) keeps the last
complete expansion when the 1,024-byte cap is reached. Local compiler checks and
29,304-call recurrence oracles pass; only fib_rec changes in its [static census](recursive-depth-code-census-02/README.md),
now 768 bytes with one spill slot. Full native qualification passes, including
all 65/72 results, 21 semantic cases and the 29,304-call recursive oracle.
Execution timing and compile cost remain unmeasured. The first ARM full run reproduces the existing cancellation
failure; 1,000-repetition probes fail 5 retained-baseline and 4 candidate test
repetitions, all nil instead of canceled in default Railshot. A fresh full
candidate run passes, but the intermittent runtime failure remains unresolved.

The [linear priority revision with retained schedule-debt units](linear-gpr-density-02/README.md)
is rejected. Its verified [twenty-module three-variant screen](linear-gpr-density-screen-02/README.md)
measures **1.0152248185** times retained time (1.52% slower), despite improving
on revision01 by 0.51%. KissFFT regresses 30.61%, Monocypher 6.53%, zlib 5.38%
and LZ4 2.41%; BLAKE3 and yyjson gains do not offset these losses. No series
exceeds 20% spread. Local AMD64, fresh guarded ARM and full native correctness
pass, including all 65/72 results. The priority/schedule-debt separation changes
27 selected schedules but does not produce an aggregate win. Leaf-density01
remains retained; no full run follows this rejected screen.

The [first linear-cost prototype](linear-gpr-density-screen-01/README.md) is
unretained: focused time / retained is 1.0058877054. Monocypher regresses 6.25%,
zlib 5.41%, and LZ4 3.54%, despite yyjson improving 3.39% and BLAKE3 2.16%.
No series exceeds 20% spread. The earlier small-loop admission extension also
remains unretained. No new full comparison has been started.

Historical diagnostic: [current-source scheduling sweep](schedule-policy-probe-02/README.md)
passes all policy result gates and completes four rounds across 65 modules / 72
exports. Forced source, latency and pressure policies take 1.00497, 1.00484 and
1.02847 times automatic scheduling time. None is retained. Memory-sum latency
scheduling is a focused lead at 0.83987 times automatic time; generated-code
evidence is in [static metrics03](schedule-policy-metrics-03/README.md). This
diagnostic does not replace the matched PR comparison above.

Follow-up [late comparison placement](late-flags-02/README.md) isolates the
memory-loop branch: removing boolean materialization while leaving a LEA between
CMP and branch regresses time 21.7%; placing CMP next to its branch instead
improves memory time 2.69% versus native-wide51204 in six paired rounds. Other
affected workloads are nearly unchanged. Both prototypes pass native correctness
gates; neither is promoted. The unchanged ARM64 baseline also reproduces an
intermittent cancellation-stress failure (7/1000 repetitions), recorded in
[revision01](late-flags-01/README.md). Revision02's fresh ARM64 suite passes, but
does not resolve that intermittent failure.

The [flags materialization cost experiment](fusion-cost-01/README.md) passes
correctness gates but is rejected after the full 65-module/72-export comparison:
time / previous is 0.9998344765, with no meaningful aggregate gain. Memory improves
4.55% while libtommath regresses 7.63%. Native compile-cost evidence is archived;
it does not change that conclusion. At that checkpoint the retained reference remained matched-full08. A
[spill tradeoff guard](fusion-cost-screen-02/README.md) also fails to improve its
16-module native screen and is rejected; its libtommath recovery comes with a
raytrace regression. A [single-binary placement experiment](module-placement-01/README.md) isolates
roughly 13% lower memory-loop time from a 32/48-byte shift with identical function
instructions. This is a local diagnostic; no general alignment policy or
full-corpus improvement is established.

The [64-byte loop placement prototype](loop-align64-full-01/README.md) is also
rejected after full native correctness and timing: time / previous is 1.0006459469,
with no aggregate gain and 6.31% module-balanced code growth. Native mapping and
function entry addresses confirm the alignment was realized. At that checkpoint the retained
candidate remained native-wide51204.

The [native-wide coverage audit](wide-coverage-01/README.md) finds no new emitted
loops from raising the operation limit or relaxing counter-step checks alone.
It identifies independent strided loads in ADI and SYR2K. A
[scalar gather prototype](strided-wide-01/README.md) passes all native result and
memory gates, but is rejected by a six-round focused screen: ADI is unchanged
and SYR2K takes 3.44816 times previous time. Three unchanged controls remain near
1.0, with no series above 20% spread. No full timing run follows this regression.

A follow-up [direct register gather](strided-wide-02/README.md) removes the
stack gather regression. Native path counts show SYR2K enters the vector path
96.25% of attempts. Keeping the guards and widths unchanged while packing
scalar loads directly into vectors improves SYR2K time about 10% in six paired
rounds. The [full four-round comparison](strided-wide-full-02/README.md) confirms
9.49% lower SYR2K time and measures 2.92% higher throughput than PR780 across all
65 modules / 72 exports. Unchanged-code timing variation limits the small
aggregate improvement. Correctness gates and fresh ARM checks pass. The completed
[compile-cost run](strided-wide-compile-02/README.md) measures under 1% more
compile time and 2.40–3.01% more allocation bytes for the changed modules, with
no >20% spread. Strided-wide02 is retained as the next experimental reference.

A [loop-hoisting dependency prototype](licm-literals-02/README.md) extends the
existing pure-integer planner to bounded literal/consumer groups. Revision01
fails CoreMark and BLAKE3 scheduling because selection retains original literal
dependencies after aliasing; revision02 preserves original and canonical
producers, passes all native correctness gates and fresh ARM checks, and changes
code in nine modules. Its [focused screen](licm-literals-screen-02/README.md)
measures 2.58% less PCRE2 time and 1.22% less monocypher time, with no noisy
series. The [full comparison](licm-literals-full-02/README.md) is effectively tied
(1.0005064407 times retained execution time), despite a repeated 1.96% PCRE2
gain. Compile costs pass verification but do not change that conclusion. The
prototype remains unretained; strided-wide02 remains the reference. A separate
[selection census](selection-alias-census-01/README.md) finds the alias-related
producer mismatch in 62 modules, motivating a broader literal-only selection
prototype. Canonical-immediate01 passes correctness but remains unretained after
an effectively tied 14-module screen (time / retained 1.0014033950); its fixed
candidate-first execution order also limits interpretation of small differences.

Local production is v77; runtime and loop performance transforms remain overlays.
Native compiler, full public runtime, semantic, and differential memory checks
pass. The measured candidate includes the interrupt-delivery overlay validated
with 1,000 reentry repeats, full native compiler/runtime suites, and single-CPU
and eight-CPU cancellation stress (see `cancel-delivery-02`). Unchanged production
v77 still reproduces that hang; the fix remains an overlay. Earlier ARM64 compiler/runtime
checks passed, but subsequent stress reproduces a Darwin interrupt crash during
concurrent invocation and close. A separate registry-based fix is under validation
(see darwin-interrupt-registry-01/02). The guarded compiler, core runtime and
public runtime suites pass with revision02; 1,000 classic and 100 guarded
stress repetitions pass (110,000 concurrent invocation/close cases).
Earlier 0.70 targets below are historical and do not define current completion.

Base commit: `34d1e4481ba7f93560c84a5deaef1fb71a862516` on
`jairus/dragline-mvp`. The changes described here are uncommitted. Per-run
metadata records source, binary, catalog, and Wasm artifact SHA-256 values.

## Environment and repeatable measurements

Native execution uses `hub@hub`, AMD Ryzen 7 7800X3D, Linux AMD64,
Go 1.22.2. The isolated source directory is
`/home/hub/wago-dragline-a82a-20261003`. The persistent SSH connection uses
`ControlPath=/tmp/wago-dragline-%C` and `ControlPersist=12h`.

```sh
# Run from the remote source directory.
python3 scripts/dragline-iterate.py --cpu 0 --pair-by-module --out iterate-next
python3 scripts/dragline-iterate.py --cpu 0 --rounds 4 --benchtime 300ms --pair-by-module --corpus all --out full-next
```

The runner builds with `wago_guardpage`, selects native targets for both
compilers, pins the selected CPU (now CPU 0), and sets `GOMAXPROCS=1`, `GOWORK=off`,
`GOFLAGS=-mod=readonly`, and `WAGO_BOUNDS=signals`. It alternates compiler order
between rounds and rejects missing or duplicate benchmark rows.
`--pair-by-module` places the two engines next to each other for each module,
limiting the time between paired samples. The report flags engine/export
samples whose maximum/minimum ratio exceeds 1.20. Ordinary
exports are checked against exact results; semantic exports also check the
Dragline instance against the catalog oracle before timing.

The fixed quick set contains 10 modules and 13 exports: `fib_rec`, `fannkuch`,
`matmul`, `json-as`, `blake3`, `coremark`, `xxhash`, `kissfft`, `polybench-gemm`,
and `polybench-floyd-warshall`. It samples calls, integer and floating-point
work, parsing, hashes, and loop nests. It is an iteration aid, not the final
acceptance set.

Reports take the median of paired Dragline/Railshot ratios for each export,
then a geometric mean within each module and across modules. This avoids
counting a module several times simply because it exports several workloads.
100 ms samples and two/four rounds are preliminary measurements; acceptance
requires longer repeated measurements and attention to host noise.

| Run | Compiler changes | Modules / exports | Paired time ratio | Lower time |
| --- | --- | --- | --- | --- |
| `iterate-01` | Regional entry ordering and empty-preheader liveness fixes | 10 / 13 | 0.9093 | 9.07% |
| `iterate-02` | Also mixed-width copy and cold-rematerialized shift fixes | 10 / 13 | 0.9022 | 9.78% |
| `full-01` | Also reject memory folds across address-register transitions | 65 / 72 | 0.8775 | 12.25% |
| `iterate-03` | BMI2 variable shifts (noisy Fibonacci samples) | 10 / 13 | 0.8579 | 14.21% |
| `full-03` | BMI2 candidate, four 300 ms rounds paired by module | 65 / 72 | 0.8807 | 11.93% |
| `iterate-04` | Earlier edge-use positions and safe store/load forwarding | 10 / 13 | 0.8615 | 13.85% |
| `full-04` | Same candidate, four 300 ms rounds paired by module | 65 / 72 | **0.8688** | **13.12%** |
| `full-07` | Resource ranking plus two scratch-contract repairs, four 300 ms rounds on CPU 0 | 65 / 72 | 0.8689 | 13.11% |

These runs compare each candidate with Railshot in the same executable.
Differences between rows do not isolate an individual patch's speed effect.
The first full run's worst ratios include Gram-Schmidt (1.352), Monocypher (1.338),
pcre2 (1.168), and yyjson (1.140). These rankings need confirmation with closer pairs.
Those are useful places to inspect dynamic
spills, loop invariants, copies, and call traffic next.

`full-02` is retained as **inconclusive timing evidence**. Its apparent 0.6739
ratio (32.61% lower time) coincided with a near-twofold timing change across
unrelated modules, including unchanged Railshot code. Most Dragline medians
halved while Railshot medians fell by about one quarter. That does not establish
the requested speedup. `iterate-03` also contains unusually large Fibonacci
variation. The remote host had other workloads; its governor was `performance`
and CPU 7 had no SMT sibling. No other user's processes were modified.

`full-03` completed four 300 ms rounds paired by module. All 72 exports were
present in every round for both engines; no engine/export sample spread
exceeded 20%. The resulting 0.8807009 time ratio corresponds to a 13.55%
throughput increase. It does **not** meet either interpretation of the 30%
objective. The sample is more stable than `full-02`, but still uses a shared
host. The BMI2 patch's isolated execution benefit is not yet established by
these aggregate runs.

The `full-03` Go source hash was
`0fd5ccada7d43936eb12407138096466bd97c59b1c998296268ac3a0e8d84828`, verified
equal in the local worktree and native run metadata. Leading remaining ratios
are Monocypher 1.3315, pcre2 1.1194, yyjson 1.0949, and blake-as 1.0811.
The next optimization pass should inspect their emitted hot-loop and call
traffic, then measure an isolated candidate against this binary as well as
against Railshot. That checkpoint used artifact revision v64; the edge-lifetime change below uses v65.

## Correctness work

The initial native semantic run failed five checks. Five independent defects
were found and repaired:

1. **Regional entry reloads after instruction elimination.** A late folded
   instruction skipped the reload scheduled at its position. Emit transitions
   before checking elimination masks. This repaired Monocypher and one Miniz
   failure. The emitter regression fails with the original ordering.
2. **Empty preheader mistaken for loop header.** Two blocks can have the same
   instruction position. A block parameter owned by the preheader was excluded
   from loop lifetime extension because it shared the header's position.
   Identify parameter ownership from CFG transfers. This repaired Miniz and
   zstd; the liveness regression fails before the fix.
3. **Wrong type when breaking a parallel-copy cycle.** The resolver saved a
   displaced i64 using the incoming i32 value's type, truncating its upper
   bits. The temporary now carries the displaced value's identity. Mixed-width
   register and spill swaps reproduce this; pcre2 now passes its full hash.
4. **Cold shift operand read from its old register.** RCX repair used the
   allocator's original location even when the operand had been rematerialized
   into RSI. Zlib's Huffman builder computed `2 << 2` in place of `1 << 2`.
   Use the emitter's actual operand register. A forced cold-use emitter test
   catches the wrong source; an extracted table builder confirmed the repair.
5. **Memory fold across an allocation transition.** Adjacent semantic
   instructions can have intervening regional edits. Zlib's folded load read
   its old address register after a fragment ended and the register was
   restored/reused. Post-RA planning now checks address residency and regional
   writes before delaying the load. Regression cases cover a spanning region,
   an ending region, a new occupant, and victim restoration.

`semantic-final.txt` records all **21 semantic checks passing**, including
same-instance repetition. `compiler-tests-final.txt` records native compiler
unit tests. The compiler subtree also passed on local ARM64; that is not an
ARM64 performance or full-corpus result. `semantic-shiftfix.txt` is deliberately
retained as the intermediate failing evidence before the memory-fold fix.
Diagnostic instrumentation was removed from production sources and the remote
benchmark package before `full-01`.

## Next optimization: BMI2 variable shifts

The candidate adds SHLX/SHRX/SARX and releases the legacy RCX count constraint
before scheduling/allocation. On BMI2 targets, the emitter consumes independent
source, count, and destination registers; compatibility targets keep CL shifts.
The output advertises its BMI2 requirement for artifact admission.

Encoder golden bytes were cross-checked with Clang's x86-64 assembler. Tests
cover all six scalar shifts, native/compatibility targets, masked counts,
negative operands, and preservation of both inputs after the shift. This
candidate passed all 21 semantic checks (`semantic-bmi2.txt`), the encoder
and compiler suites, and 7,008 shift/input checks split across native and
compatibility modes (`compiler-tests-bmi2.txt`). Timings remain preliminary. Instruction semantics and encodings were checked against the
[Intel instruction reference](https://cdrdv2-public.intel.com/782151/253667-sdm-vol-2b.pdf),
SARX/SHLX/SHRX entry.

The source research on Wasmtime, regalloc2, LLVM, V8, and Wago knowledge is in
[the research note](../../dragline-optimization-research-2026-10-03.md).

## Next diagnosis: register pressure in Monocypher

`monocypher-metrics.json` and `monocypher-layout.json` were captured from the
current native compiler with `draglinemetrics -target native -bounds signals`.
They are static compiler diagnostics, not sampled execution attribution.
The native image is 30,880 bytes across eight functions. Functions 0 and 3
have 29/25 spill slots and 45/41 physical copies; function 6 has 40 spill slots
and 13,813 native bytes. The register-pressure diagnostics and the measured
1.3315 execution ratio make allocation and hot-loop scheduling concrete next
investigation points. Native code for inspection remains at
`/home/hub/wago-dragline-a82a-20261003/monocypher-native.bin`.

## Edge lifetimes and safe forwarding (v65)

A transfer out of a nonempty block now uses the final predecessor phase
(`end * 6 - 1`), before the successor's parameter definitions. Previously,
the transfer use shared the header definition position. Loop lifetime
extension consequently kept incoming values alive throughout the loop even
though the header parameter replaced them. The allocator and its verifier now
agree on the earlier edge-use phase. Empty blocks retain the boundary position.
A two-block regression checks both targets with one GPR: incoming, phi, and
latch values no longer overlap or require spills.

The initial lifetime probe exposed a zlib defect in store/load forwarding.
A regional reload for v788 overwrote v74's register between store 512 and
load 517, which are adjacent in the selected schedule. Merely comparing
`LocationAt(v74, store)` and `LocationAt(v74, load)` did not detect this:
v74 was dead, and both queries returned its old assignment. Forwarding now
checks intervening fragment entries and victim restores, source location
changes, cold rematerialization, and block boundaries. Memory folding shares
the same survival check. Both emitters read a surviving source from its
current regional location. Safe forwarding remains enabled.

The new forwarding regression failed before the fix for both AMD64 and ARM64.
It covers region exit, register reuse, victim restore, cold rematerialization,
cross-block forwarding, unchanged allocation, and a region spanning both
instructions. `semantic-edge-final.txt` records all 21 semantic checks passing.
`compiler-tests-edge-use.txt` records native AMD64 encoder, compiler, and
`src/wago` tests passing; encoder/compiler tests also passed locally on ARM64.
Diagnostic environment switches and the temporary probe harness were removed
before measurement.

`full-04` measured all 72 exports across 65 modules in every round. Its paired
time ratio is 0.8687848: **13.12% lower execution time / 15.10% higher throughput**
than Railshot. This does not achieve the goal. One Railshot `dispatch.apply`
series had a 1.615 max/min spread; all other series were below 1.20. Treat this
as a qualified shared-host measurement, not definitive attribution of the
patch's effect. The fixed quick set had no series above 1.20.

The local and remote Go source hashes match:
`c48c4f48c2b909ee3bd382eada04947b9ab5bf06d4ae28de2ad189a0a1aa709b`.
The benchmark runner now supports direct old/new Dragline comparisons, checks
the saved executable hash and matching target/environment/catalog/artifacts,
and records the baseline metadata:

```sh
python3 scripts/dragline-iterate.py --cpu 7 --rounds 4 --benchtime 100ms \
  --pair-by-module --corpus all --baseline-run full-03 --out ab-edge-next
```

Next machine-code opportunity: Railshot's `applyALU` directly consumes native
spill slots through `AluRM`; Dragline's scalar arithmetic currently materializes
those operands into scratch registers first. Investigate integer spill-source
folds using exact current locations, excluding elided pending spills and cold
rematerialization. Keep that experiment separate from this lifetime change.

### Isolated old/new result

`ab-edge-01` compares the saved `full-03` Dragline executable against v65,
with four 100 ms rounds paired by module across all 65 modules / 72 exports.
The candidate/baseline time ratio is **0.990981** (0.90% lower time). This is
preliminary: the baseline drwav series and both candidate qoi export series
had spreads above 20%. It does not justify a large aggregate speedup claim.

| Module | v65 / prior Dragline time |
| --- | ---: |
| Floyd-Warshall | 0.8139 |
| Jacobi-1D | 0.8640 |
| Monocypher | 0.9031 |
| Nussinov | 0.9040 |
| xxhash | 0.9083 |
| Heat-3D | 1.1910 |
| zlib | 1.1426 |
| lz4 | 1.0812 |
| raytrace | 1.0743 |

These are measured tradeoffs, not a claim that every function improved.
The next allocation investigation must account for Heat-3D and zlib's
regressions while retaining the smaller live ranges.

Fresh `monocypher-edge-final-metrics.json` / layout diagnostics show native
size falling from **30,880 to 28,605 bytes**. Function 0 goes from 29 to 12 spill
slots, 45 to 32 physical copies, and 2,385 to 1,539 native bytes. Function 3
goes from 25 to 17 slots, 41 to 26 copies, and 2,181 to 1,391 bytes. Function 6
still has 40 slots and approximately 13.8 KB of code. These static metrics
explain the direction of the Monocypher improvement without proving where
all dynamic time is spent.

### Confirmed regressions and schedule diagnosis

`ab-edge-focus-01` repeats six diagnostic modules for eight 500 ms rounds.
Monocypher is 0.8857 of the prior Dragline time; zlib is 1.1325 and Heat-3D is
1.1820. These three series have no spread above 20%. Qoi's baseline series
remain noisy, so this selected set is not used as an aggregate acceptance
result.

A Go overlay restores only the former edge-use phase in the otherwise-current
v65 compiler. `long-edge-diagnostic.patch` and its JSON identity record this
counterfactual; production sources remain on the shorter lifetimes. The paired
`*-long-edge-metrics.json` / `*-short-edge-metrics.json` captures show:

| Function | Long → short schedule | Spill slots | Physical copies | Native bytes |
| --- | --- | --- | --- | --- |
| zlib 1 | Pressure → Source | 28 → 24 | 140 → 109 | 4,627 → 4,565 |
| zlib 6 | Latency → Pressure | 72 → 62 | 1,423 → 1,215 | 28,510 → 25,568 |
| Heat-3D 1 | Pressure → Latency | unchanged | 85 → 70 | 4,486 → 4,151 |

The lower static costs select different schedules while measured execution
regresses. This points to schedule cost/selection as the next hypothesis to
isolate; it does not yet prove causation. First compare forced source, latency,
and pressure schedules under the shorter lifetimes, then inspect hot reloads
and copies. The current weighted spill debt derives from interval costs;
actual emitted, block-weighted traffic is another candidate measurement.

## Schedule isolation and rejected scalar spill folding

`schedule-probe-01` holds the v65 compiler and inputs fixed, forcing each of
source, latency, and pressure scheduling using temporary Go overlays. Four
300 ms rounds rotate the four executable variants on CPU 7. Each benchmark
still checks its result oracle. Relative to automatic selection:

| Forced schedule | zlib | Heat-3D |
| --- | ---: | ---: |
| Source | 1.0055 | 0.8401 |
| Latency | 1.0601 | 1.0000 |
| Pressure | 1.0088 | 0.8468 |

This isolates Heat-3D's loss to the selected schedule; choosing another
schedule does not recover zlib. Heat-3D's latency candidate has only two
planned post-RA elisions versus twelve for source/pressure. That is a concrete
hypothesis for a cost-model experiment, not a proven explanation yet.

A v66 experiment followed Railshot's `applyALU` / `AluRM` pattern to consume
native scalar spill slots directly in i32/i64 add, subtract, multiply, and
bitwise arithmetic. Twenty-two emission cases reproduced the missing memory
operand before the change and passed afterward. Eight selector cases covered
cold rematerialization, forwarded pending spills, address rematerialization,
regional residency, repeated operands, and subtraction ordering. The compiler
and runtime suites passed, including **6,240 high-pressure arithmetic checks**;
all **21 semantic checks** passed (`compiler-tests-spill-fold.txt`,
`semantic-spill-fold.txt`).

The first timing screen, `iterate-05`, is **invalid timing evidence**: another
compilation benchmark was running on CPU 7, and unchanged Railshot timings
rose sharply. `contention-iterate-05.json` records the observed contention.
The comparator now allows replaying a saved binary on a different CPU; both
executables are always pinned to the same current CPU, and historical timings
are never reused.

A new four-round 300 ms comparison on CPU 0 (`ab-spill-01`) had no sample
spread above 20%, but showed **13.26% higher time** across the quick set.
Blake3 was 1.5222 of v65 time and KissFFT was 2.2363; other modules changed
little. `ab-spill-01-summary.json` preserves the tool-reported result. Raw logs
remain on hub and must be retrieved before more detailed analysis. The failed
candidate's `full-05` run was deliberately stopped; it has no complete result.

**The scalar spill-folding experiment is not retained in production.** The
local compiler was restored to the exact v65 Go source hash recorded by
`full-04`; local compiler tests pass. The implementation and both new test
files are preserved in `scalar-spill-fold-experiment.patch`, and
`git apply --check` confirms it can be replayed on the retained tree. Fewer
native instructions alone did not establish better performance.

`spill-probe-01` completed after connectivity returned. Before restarting,
process inspection and the missing output directory established that the
original disconnected command had never started. Four rotated 300 ms rounds
on CPU 0 compare the saved v65 binary, the full v66 experiment, and three
Go-overlay variants of v66:

| Variant | Blake3 hash / v65 | KissFFT / v65 |
| --- | ---: | ---: |
| All eligible scalar spills | 1.5341 | 2.2418 |
| Folding disabled | 1.0021 | 0.9983 |
| Right operands only | 1.2116 | 1.4713 |
| Bitwise operations only | 1.1878 | 0.9937 |

The disabled-fold variant returns to baseline. The narrower enabled variants
still regress at least one workload, so none is retained. This isolates the
slowdown to spill folding, but does not yet identify its microarchitectural
cause. Raw `ab-spill-01` and `spill-probe-01` artifacts have been retrieved.
`full-05` is preserved as an intentionally aborted partial run.

Both local and remote production Go sources are restored to v65. The native
compiler/runtime tests were rerun after restoration. The persistent SSH socket
was reestablished after the outage. No diagnostic benchmark is left running.
The requested 30% objective remains open; the latest retained full-corpus
result is still the qualified `full-04` result above.


## Resource-aware schedule selection (v67 experiment, v68 repairs)

`heat-fold-probe-01` completes the Heat-3D intervention: six rotated 500 ms
rounds on CPU 0 compare the saved automatic binary, forced source scheduling,
and the same source schedule with heap-load folding disabled. Source takes
**0.839128** of automatic time; source without folding takes **1.001341**.
Preserving the heap-load folds explains the measured scheduling benefit.

The new `NativeResourceCost` field estimates block-weighted surviving selected
operations, operand reloads, spilled results, regional transitions, and edge
and fixed moves. Verified heap-load folds remove the standalone load and its
intermediate spill traffic from the estimate. AMD64 speed selection compares
this cost first, retaining the existing rules for ties. ARM64 selection is
unchanged. Diagnostics expose the cost in metrics version 31; scratch capacity
is included in retained-memory accounting. This is a ranking estimate, not an
exact instruction/port/latency model. Segmented allocation admission still
uses its established debt gates.

The initial v67 quick screen (`iterate-06`) measured **0.8662** on 10 modules /
13 exports. All 21 semantic checks passed. However, `full-06` failed its first
round on `blake-as-simd`; it is **not valid aggregate timing evidence**. Broader
selection exposed two independent emitter scratch-contract bugs:

| Failure | Cause and repair | Red evidence |
| --- | --- | --- |
| Blake SIMD hash returned 4292537149 instead of 26497025 | A forwarded vector result in XMM13 was overwritten by another operand reload into XMM13. Check actual scratch assignments, including cold rematerialization. | `pending-spill-red.txt`, `blake-spill-red.txt` |
| PolyBench 3MM returned 2535580613 instead of 2627163156 | A parallel edge-copy cycle saved a live base pointer in R10, then rematerialized zero into that same R10. Use the separate transfer temporary for rematerialized spill destinations. | `edge-remat-red.txt`, `matrix-edge-red.txt` |

The copy-cycle diagnosis was confirmed in emitted assembly: load spill 0 into
R10, overwrite R10 with zero, store zero to spill 0, then copy the overwritten
R10 into RDX. The resulting matrix base pointer was zero instead of 152896.
Disabling heap-load folding, all post-RA rewrites, regional allocation,
edge-result renaming, unrolling, float-constant retention, or loop rotation
individually did not repair this error.

Both defects have focused emission tests and corpus runtime regressions with
one/four compiler workers and explicit/signal bounds checks. v68 passes the
native compiler/encoder/runtime suites, all **72 execution-export result
checks**, and all **21 semantic checks**. Local ARM64 compiler tests also pass.
No diagnostic environment controls or trace writes are present in production.

v68 Go source SHA-256 (verified equal locally and in native run metadata):
`4cbcb1cbabfa17128ea0709febf445628cdd479e494ee4340a46bb9f6c99af89`.
`full-07` completed four paired 300 ms rounds: **0.8689138223** time ratio,
**13.1086% lower time**, **15.0862% higher throughput**. All 72 exports are
present in every round. Two Railshot series exceed the 20% spread threshold:
Zstd (1.4677) and Doitgen (1.2947). Another user's correctness process was
observed on other CPUs during the run; no other user's process was modified.

This does not establish an aggregate improvement over v65. Heat-3D's paired
ratio improved from 0.8229 to 0.6922 across the two Railshot comparisons, while
KissFFT, zlib, and LZ4 worsened. Those cross-run differences are diagnostic
signals, not isolated patch effects. `ab-resources-01` compares the saved v65
and v68 Dragline executables consecutively on the same CPU to isolate the
candidate's effect. Resource ranking remains under evaluation; the two
correctness repairs are independently justified by red/green regressions.


`ab-resources-01` completed all 65 modules / 72 exports, four 100 ms rounds,
replaying both Dragline executables on CPU 0. Its ratio is **0.99901048**
(**0.099% lower time**), with no series exceeding 20% spread. This does not
establish a useful aggregate gain from broad resource ranking. Focused ratios
against v65 include Heat-3D 0.8439, raytrace 0.9339, Monocypher 0.9475,
KissFFT 1.0874, LZ4 1.0802, and zlib 1.0604.

`resources-choice-metrics/` compares the new choice with the old comparator
using otherwise current source. The hot KissFFT schedule changed for about a
1% resource estimate reduction while estimated dependency cycles increased;
LZ4 changed for about 0.6%, and zlib's large function changed for about 0.6%.
Heat-3D's estimated resource reduction is about 10%, and raytrace's about 6.5%.
`resource-margin-probe-01` tests minimum estimated improvements of 2%, 4%, and
6.25% before this approximate dimension overrides existing scheduling policy.
These Go-overlay variants are diagnostic and are not production settings.


The margin probe completed four rotated 300 ms rounds across nine diagnostic
modules (ten exports), with no series above 20% spread. Relative to v65:

| Minimum estimated benefit | Diagnostic-set time ratio |
| --- | ---: |
| 2% | 0.959714 |
| 4% | 0.968220 |
| 6.25% | 0.972954 |

At 2%, Heat-3D takes 0.8376, raytrace 0.9254, Monocypher 0.9354, and utf8proc
0.9415 of v65 time; KissFFT, zlib, xxhash, and Syr2k are within 0.3%, and LZ4
is 1.0112. This supports testing the 2% margin on the complete corpus, not a
claim of 4.03% improvement on all workloads. The focused set was chosen from
the preceding gains/losses and is separate from the fixed quick-iteration set.

Production candidate v69 now requires an estimated resource reduction greater
than 2% to override the existing comparator. Boundary and reverse-order tests
were red before the change (`resource-margin-red.txt`). The 2% threshold is a
heuristic justified by this experiment, not a calibrated hardware cost model.
v69 passes the native compiler/encoder/runtime suites, all 72 execution-export
result checks, and all 21 semantic checks. Local ARM64 compiler tests pass.
`full-08` completed all 65 modules / 72 exports in four paired 300 ms rounds: **0.8683041578** time ratio, **13.1696% lower time**, and **15.1670% higher throughput**. Dragline QOI encode/decode and Doitgen exceeded the 20% spread threshold, so this remains a qualified shared-host result.

v69 Go source SHA-256 (verified against local source before the leaf-inliner changes, and recorded in `full-08/metadata.json`):
`69b8af4b6567b8328dd33dad5c65e66698a7d2a8425c65c842a7426fcc8e53c5`.


`ab-resource-margin-01` replayed v65 and v69 consecutively across all 65 modules /
72 exports, four 100 ms rounds on CPU 0. The ratio is **0.9943399924**
(**0.5660% lower time**). Candidate dispatch and QOI encode/decode exceed 20%
spread. Heat-3D 0.8404, raytrace 0.9288, utf8proc 0.9394, and Monocypher 0.9542
retain the focused improvements; KissFFT is 0.9988. This is a modest aggregate
change and does not close the gap to the 30% target.

### Bounded scalar leaf inliner (v70/v71 candidate)

The census in `diagnostics/inline-census.json` found small leaf helpers inside
hot loops in Blake3, yyjson, libtommath, and pcre2. Its static loop weights are
heuristics, not measured profiles; the census includes catalog modules outside
the 65-module execution timing set. `leaf-inline-red.txt` records the initial
regression failing because both calls remained and no callee loads were copied.

The new transform runs before CFG/SSA construction, binds parameters in reverse
stack order, rebases local accesses, and resets declared locals on every call.
A result-typed block replaces the callee function label; explicit returns become
branches to that block. Original caller instruction offsets survive rewriting,
and copied instructions map to the caller's call site. This preserves useful
caller attribution, not a full inline-frame debug tree. Copied body dependencies
already participate in the caller's transitive artifact identity.

Initial bounds are 384 callee bytes, 32 sites, 4096 added caller bytes, and 256
added locals. Numeric globals, memory loads/stores, control flow, and scalar traps
are eligible. Calls, references, SIMD, allocation, and runtime helper operations
are excluded. The bounds are experimental admission heuristics. Eligibility
and emitted behavior must pass the full result/semantic gates before performance
claims. ABI/cache tests pad their leaf fixtures beyond admission so they still
exercise real calls and safepoints.


The first full semantic screen failed Blake3 vectors at length 8192. The failure
was isolated to copying helper 7 into function 0 at caller offset 882. Other
caller/site probes passed. Fresh local bindings, all three forced schedules,
and individually disabling memory folds, forwarding, regional allocation,
LICM, pending spills, unrolling, rotation, renaming, or the module-global pin
failed to repair it. Recompiling the rewritten Wasm with Railshot preserved the
result, distinguishing the bytecode rewrite from Dragline native emission.

The native trace found that the newly expanded copy path need not even execute.
Inlining changed allocation elsewhere: a recursive result remained live in R12
across a ten-argument call. At native offset `0x951`, argument setup loaded zero
into R12 before entering the callee. The private ABI normally preserves R12
(allocation index 9), but the eighth argument uses it. A callee-body clobber
contract cannot cover a write that occurred before entry.

The repair models argument setup separately in call-survivor masks, retains its
write in ABI/clobber pruning, and saves the enclosing function's incoming R12.
v71 additionally makes shrink-wrap planning and verification account for that
write, preventing a restore before a later call setup. `call-setup-red.txt` and
`call-setup-shrink-red.txt` show the focused regressions failing before their
respective fixes. A native Blake3 regression tests the 8192-byte vector under
explicit/signal bounds and one/four workers, including repeated invocations.

v70 passes the native compiler/encoder/runtime suites, all **21 semantic checks**,
and all **72 execution-export result checks** (`*-leaf-r12.txt`). Local ARM64
compiler suites and the new inline runtime cases pass. A broader ARM64 runtime
screen exposed existing JSON startup and branch-cast failures and timed out in
a one-function loop test. JSON startup and branch-cast failures reproduced with
leaf inlining disabled (`pre-inline-arm64-failures.txt`). This is **not** a claim
of full ARM64 runtime correctness; the chosen performance target remains AMD64.

`iterate-07` is the fixed ten-module / thirteen-export quick screen, four paired
100 ms rounds on CPU 0: **0.8622850198**, **13.7715% lower time**, **15.9709% higher
throughput** versus Railshot. No series exceeds 20% spread. It does not establish
the full-corpus result or isolate the inliner's benefit from the earlier build.
v70 source SHA-256: `906433ff57434324d9b59803985b223c54e6fde0e9a147a3bae2c8662104dad6`.

v71 source SHA-256: `1569010fcc644c1c205c2e3935f28d749e3ecc003e11931bbbed9e90755af26c`.
The final v71 native compiler/encoder/runtime suites, 21 semantic checks, and
72 execution-export checks pass (`*-leaf-v71.txt`). `full-09` completed four
paired 300 ms rounds on all 65 modules / 72 exports: **0.8662910161** time ratio,
**13.3709% lower time**, **15.4346% higher throughput** versus Railshot. No series
exceeds 20% spread. This remains a shared-host measurement.

`ab-leaf-inline-01` compares v71 directly with saved v69 (`full-08`), all 65
modules / 72 exports, four paired 100 ms rounds: **0.9986517190**, or **0.1348%
lower time**, with no series exceeding 20% spread. This does not establish a
meaningful aggregate inlining win. Many-functions is 0.9457, libtommath 0.9808,
and pcre2 1.0486. The R12 repair remains a correctness requirement regardless
of the inliner decision. No commit or push has been made; the 30% target remains
unachieved.

### Loop census and guarded SIMD experiment

`diagnostics/loop-census.go.txt` builds CFG, local SSA, value flow, and semantic
IR for the same 65 execution modules. Its current report covers 823 functions,
935 self-loop blocks, and no failed function analyses. It finds 26 adjacent
scalar FP store-expression pairs in eight modules: matmul, nanosvg, Cholesky,
Covariance, Deriche, Gemver, LU, and Ludcmp. Pairs can overlap; this is not 26
independent transformations. Another screen finds 50 self-loop blocks with FP
arithmetic, loads and stores, no call, and no explicit FP phi recurrence.
Neither count proves independence, safety, hotness, or profitable vectorization.
In particular, memory-carried recurrences are not excluded by the phi screen.

The diagnostic generator `a82a-vector-probe.py` manually replaces matmul's
already paired scalar updates with two-lane FP arithmetic. This is a rewritten
fixture experiment, **not an implemented compiler vectorizer**. Original corpus
artifacts are unchanged. The guarded variant checks complete destination and
source ranges with i64 arithmetic before the loop, admits identical or disjoint
ranges, and executes the original scalar loop otherwise. This keeps partial
stores before a trap and overlapping scalar recurrences on the scalar path.
Each output retains its multiply-then-add ordering; no FMA or reassociation.
The original odd-element remainder remains in place.

Two standalone regions expose the same scalar/vector paths with arbitrary
pointers. Native explicit and signal bounds screens passed:

- 3,584 finite-input cases per three candidate implementations (scalar Dragline,
  vector Railshot, vector Dragline), with byte-exact whole-memory and trap-code
  comparison. Cases include unaligned/overlapping pointers, end-of-memory and
  memory32-wrap addresses, four even lengths, and signed-zero coefficients.
- 1,152 NaN cases per three implementations, with exact non-output memory and
  trap codes. All coefficients are NaNs and initial memory has no NaNs, so the
  oracle admits only canonical results for canonical coefficients and arithmetic
  NaNs for noncanonical coefficients; differing bits must be in destination
  elements. This is a semantic oracle, not blanket NaN normalization.
- Matmul results and complete linear memory agree for n=1..100 across all six
  measured variants.

The initial byte-exact NaN screen failed on the **existing scalar Dragline**
control before judging vectorization. Its differing noncanonical payload was
permitted by Wasm; the diagnostic and failure log are retained. See the
[Wasm NaN rules](https://webassembly.github.io/spec/core/exec/numerics.html#nan-propagation).
The corrected NaN screen is `a82a-vector-nan.go.txt`, with results in
`a82a-vector-nan.log`.

Six rotated 500 ms measurements on AMD64 CPU 0, n=64, gave these medians:

| Fixture / compiler | Time (us) | Ratio to original Dragline |
| --- | ---: | ---: |
| Original / Railshot | 84.3165 | 0.9514 |
| Original / Dragline v71 | 88.6262 | 1.0000 |
| Guarded SIMD / Railshot | 160.2559 | 1.8082 |
| Guarded SIMD / Dragline v71 | 148.1879 | 1.6721 |
| SIMD without guards / Dragline v71 | 131.1603 | 1.4799 |
| Guarded SIMD, broadcast hoisted / Dragline v71 | 138.4369 | 1.5620 |

All spreads are below 1%. The no-guard fixture only isolates guard overhead;
it is not a legal general replacement. These results reject this prototype as
a performance improvement. They do not prove that vectorization itself cannot
win with better lowering. The recorded native loop has repeated address copies,
materialized compare results, edge moves, and an in-loop scalar broadcast.

A specific scheduler interaction emerged: pressure scheduling reserves adjacency
to the branch for an induction update, which can prevent the comparison from
fusing with that branch. `a82a-fusion-probe.py` clears that induction reservation
when a valid comparison/branch pair exists. This is a Go-overlay experiment;
production source remains v71. The same six-variant diagnostic gives guarded
SIMD 138.1904 us, no-guard SIMD 102.3272 us, and hoisted SIMD 120.5767 us; original
Dragline is 88.9456 us. The improvement is promising for scheduling but SIMD
still loses. The two diagnostic runs were separate; they are not the matched
full-corpus evidence needed for retention.

`fusion-priority-probe-01` completed the scheduler overlay comparison against
saved v71, all 65 modules / 72 exports, four paired 100 ms rounds. Native compiler,
encoder, and runtime package gates and all 21 semantic checks pass. The time
ratio is **0.9996210361**, only **0.0379% lower time**, with no series above 20%
spread. LZ4 regresses to 1.0567; Gemver improves to 0.9807. This does not justify
changing the default scheduler. The overlay stays diagnostic, and production
remains v71. The probe archives the exact overlay, base-source pin, binary
hashes, per-export samples, and coverage checks.

`fusion-priority-red.txt` also reproduces the adjacency conflict in a small
pointer-induction loop on both targets. The overlay passes the focused case,
including reused scheduler state, and the complete local machine-IR package
(`fusion-priority-green-local.txt`). The test fragment is archived separately
as `diagnostics/a82a-fusion-test.go.txt`; it has not been added as a production
requirement for the rejected default policy.

Next investigation: explain and reduce emitted loop overhead (address copies,
edge-register placement, broadcast placement, branch conditions), and compare
actual generated code with the schedule cost estimate before automatic loop
vectorization is promoted. There is no production vectorizer in this experiment,
no 30% result, and no currently running timing job from this section.

## v72 SIMD lowering and v73 terminal-control scheduling

v72 (`c26b4aa2a4814f8e4d43fa4f6ef9c6b308eab579be8ee623063859f6e917ce92`)
reuses an allocated SIMD memory address when the existing scalar address proof
permits it, and emits one three-operand VEX unpack for f64x2.splat. Large offsets
retain the destructive-address scratch path. Native compiler/encoder/runtime
and 21 semantic checks pass (`compiler-tests-simd-v72.txt`,
`semantic-simd-v72.txt`); focused tests have archived red evidence.

`ab-simd-lowering-01` compares v72 with saved v71 over 65 modules / 72 exports,
four paired 100 ms rounds: time ratio **0.9999624172**. No aggregate speedup is
established. `iterate-08` measures 0.8583464163 against Railshot on the ten-module
quick corpus. The smaller instruction sequences are retained as lowering
improvements, without claiming a full-corpus performance gain.

Three reverse register-affinity policies were also tried with Go overlays.
Native package and semantic gates pass. The four-round quick comparison against
saved v72 gives symmetric 1.0029612736, backedge 1.0003982120, and self-loop
0.9991874699 (`affinity-probe-01`). None justifies promotion.

### A scheduler correctness fault exposed by unrolling

A diagnostic unroller repeats the known matmul scalar inner body 2/4/8 times,
with an unsigned remaining-distance/evenness guard and scalar remainder. It
preserves the original sequential memory and floating-point operation order.
The automatic schedule passes all tested cases, but forcing pressure scheduling
on the four-copy fixture traps at n=8 instead of returning 68297. Disabling only
fusion at machine comparison 179 fixes that case. Other pass disables do not.

The emitted faulty sequence is retained in
`diagnostics/a82a-pressure-unroll4.asm`:

```
4c1: cmp ecx,r12d       # intended unsigned remaining-distance comparison
4c4: cmp r9d,r8d       # dead comparison, scheduled after the branch pseudo-op
4c7: setne dil
4cb: movzx edi,dil
4cf: jae ...           # incorrectly consumes the second CMP flags
```

`scheduleControlOp` compared target-selected opcodes directly with semantic
Wasm opcodes. This allowed a branch before the end of its scheduled block;
the emitter places the actual jump at block end. v73 decodes the opcode before
classifying it, protects the adjacency fast path, and verifies that no
instruction follows a control instruction. Fusion also clears an incoming
operand-sink reservation that could otherwise force a three-instruction
adjacency chain too early. This preserves legal fusion instead of disabling it.

`schedule_control_test.go` exercises selected opcodes on both targets and all
three schedules, including a pressure-sink conflict, and rejects malformed
control order. `schedule-control-red.txt` records the original failure. Local
compiler/encoder tests and the complete native compiler/encoder/runtime gate
pass. All 21 semantic checks pass (`semantic-control-v73.txt`).

The corrected diagnostic passes 8,064 finite alias/bounds cases across four
implementations and result/full-memory checks at n=1..100 for 25 combinations
of compiler, schedule, and scalar/unrolled/vector fixture. Four rotated 250 ms
CPU-0 rounds are in `diagnostics/a82a-schedule-bench-fixed2.log`:

| Fixture | Auto (us) | Source (us) | Latency (us) | Pressure (us) |
| --- | ---: | ---: | ---: | ---: |
| Original | 89.338 | 89.127 | 88.965 | 115.899 |
| Unroll 2 | 131.531 | 129.766 | 131.781 | 141.348 |
| Unroll 4 | 123.441 | 115.207 | 123.528 | 127.754 |
| Unroll 8 | 105.714 | 107.204 | 139.846 | 106.408 |
| Guarded SIMD | 126.585 | 111.200 | 109.324 | 126.736 |
| Guarded SIMD, splat hoisted | 124.105 | 110.868 | 109.240 | 124.048 |

Original Railshot is 84.285 us. All spreads are below 1%. No unrolling or
vectorization transformation from this probe is promoted. Source scripts are
archived in `diagnostics/`; they generate temporary WAT/Wasm fixtures from the
pinned corpus matmul module.

v73 source hash is
`955fb855a4454408702fd577e4f24edf954d3d4ad4ad0641559acc3f089d6c0a`.
`iterate-09` gives a quick-corpus time ratio of **0.8633142915** against Railshot
(**13.6686% lower time**). Full matched runs are recorded below once complete.

`ab-control-order-01` completes v73 versus saved v72 over all 65 modules /
72 exports, four paired 100 ms rounds. Time ratio **1.0001497572**
(0.0150% higher time), with no series above 20%
spread. This establishes no aggregate performance change. Coremark is 1.0364;
drwav is 0.9855. The scheduler correction is retained for correctness.

`full-10` completes the fresh v73 comparison against Railshot over all
65 modules / 72 exports, four paired 300 ms rounds on AMD64 CPU 0. Time ratio
**0.8666792597**, **13.3321% lower time**,
**15.3829% higher throughput**. No series exceeds 20%
spread. Exact source and binary hashes, coverage, and all samples are archived.
The 30% time-reduction target is still unmet.

### Unused machine values: isolated candidate

`dce-probe-01` compares two overlays against saved v73 on the ten-module /
13-export quick corpus, four rotated 100 ms rounds. Both native package gates
and all 21 semantic checks pass. The scalar zero-use sweep gives time ratio
0.9991694718; tracing liveness through block parameters gives 0.9961701349.
The latter improves Coremark to 0.9644433614, while matmul is 0.9988544097.
This is preliminary evidence only; a full comparison follows.

Both variants retain potentially trapping/effectful operations and actual
function results. The graph variant additionally removes transfers into dead
numeric parameters, including unobserved loop recurrences. Reference values
remain roots. Focused tests retain division traps, memory loads, calls, results,
and live recurrences while deleting dead numeric recurrences. The source-map
fixture is changed in the overlay to use an observable block result; its former
dropped constant is intentionally removable, so its old two-native-offset
expectation no longer applies to this optimization. Production remains v73.

`dce-full-01` compares the graph candidate with saved v73 over all 65 modules /
72 exports, four paired 100 ms rounds. Ratio **0.9991062988** (0.0894% lower
time). Both QOI exports are noisy in both binaries (1.50–1.79 max/min spread).
The quick Coremark improvement does not repeat: full ratio 1.0138. This is
insufficient evidence for promotion. Both DCE variants remain diagnostic;
production source and the latest verified absolute result remain v73/full-10.

### Native PC sampling without perf privileges

`native-profile-01` is a profiling-only Go overlay. It keeps the interrupted
native PC when Go's stack unwinder cannot describe JIT code, instead of replacing
it with the synthetic ExternalCode symbol. A second hook records the executable
mapping, function entries, source hash, and exact relocated native bytes on first
activation. Production source and the system Go installation are unchanged.
The overlay sources, profiler binary hash, production source pin, raw pprof data,
code mappings, native images, disassembly, and reports are archived.

Raw Go profile locations use the return-PC convention; the report adds one byte
to recover the sampled AMD64 address. Samples and location records are parsed
separately from mapping records. All Dragline native samples and all matmul
Railshot samples land on decoded instruction boundaries. Eight of 1,151 PCRE2
Railshot native samples are retained as unaligned in the whole-image objdump;
embedded tables can disrupt linear disassembly. They are not assigned a guessed
instruction. Function attribution uses the recorded entry table independently.

| Workload / compiler | Total samples | Samples in recorded native code | Leading function shares of native samples |
| --- | ---: | ---: | --- |
| Matmul / Dragline | 612 | 611 | Function 0: 100% |
| Matmul / Railshot | 618 | 615 | Function 0: 100% |
| PCRE2 / Dragline | 1,645 | 1,176 | Function 60: 50.09%; function 8: 22.87% |
| PCRE2 / Railshot | 1,186 | 1,151 | Function 60: 49.35%; function 8: 24.33% |

Profiles use 100 Hz sampling, CPU 0, and 5 s matmul / 10 s PCRE2 benchmark
intervals; calibration and compilation are also present. Compare shares within
recorded native execution, not total-process sample counts. Function 60 is the
exported `pcre2_run`; the corpus module has already inlined substantial library
code into that function. These are diagnostic samples, not a matched execution
speed claim and not precise instruction latency measurements.

The hot matmul code exposes a concrete Railshot advantage. Each update folds
both source multiplication and destination addition into memory operands:
`vmulsd ...,[source]`, `vaddsd ...,[destination]`, store. Dragline folds the
multiply but retains a separate destination `movsd` before the multiplication.
Its memory-fold rule currently requires adjacency and the load in operand two.
Next experiment: investigate a legal commutative/nonadjacent load fold with
explicit address-lifetime and effect/trap-order proofs, then measure the fixed
quick corpus and complete corpus. The profiles also identify PCRE2 functions
60 and 8 as the main targets for inspecting call/edge spill traffic.


### Delayed scalar float memory folds (v74)

The native profiles motivated a bounded post-allocation pass for one-use f32/f64
loads. It can fold a load from either side of commutative add/multiply, or from
the right side of subtract/divide, within eight following instructions in the
same block. The pass accepts only unshared memory32 at memory index zero.
Pure arithmetic and private scalar reads may intervene; stores, calls, control,
growth, atomics, and different trap classes stop the scan. Reordering two reads
can change which failing read supplies a diagnostic source offset, but preserves
the memory-bounds trap class and memory effects.

The address proof requires the original physical location to survive regional
transitions and intervening definitions. Register/spill addresses must remain
inside one continuous live segment. Rematerialized addresses must be actual
integer constants, and the pass rejects addresses selected for later affine
recomputation. No allocator live range is extended.

The isolated prototype was measured against the saved v73 binary, with four
rotated 100 ms rounds on CPU 0:

| Run | Coverage | Candidate / v73 time | Selected module ratios |
| --- | --- | ---: | --- |
| `fold-probe-01` | 10 modules / 13 exports | 0.9869212177 | Matmul 0.9183; GEMM 0.9531 |
| `fold-full-01` | 65 modules / 72 exports | 0.9959472605 | Matmul 0.9351; GEMM 0.9553; syr2k 0.9186 |

The full aggregate is a modest 0.4053% time reduction; the baseline Doitgen series
has 1.3767 max/min spread. The production v74 proof is stricter than the prototype,
so these numbers do not establish v74 performance. Prototype source overlays,
runners, structural red/green logs, and semantic probes are preserved under
`diagnostics/a82a-fold-*`, with a SHA-256 archive manifest.

The prototype passed 127,776 differential float/value/memory/trap cases. Its
reference uses Railshot explicit bounds checks: Railshot signals mode produced
a division-by-zero trap before an earlier invalid load in the division-barrier
fixture. That observed reference discrepancy is preserved separately in
`diagnostics/a82a-fold-railshot-trap-order.log`; it is not counted as a Dragline
failure or fixed by this pass.

Production adds durable address-proof tests and a runtime differential matrix
covering both float widths, all four scalar arithmetic operations, signed zero,
infinities, canonical/noncanonical NaNs, subnormals, large finite inputs,
overlapping/unaligned/invalid addresses, stores, and division barriers. It checks
whole memory and trap codes, exact non-NaN bits, and WebAssembly NaN classes.
The native Linux test build covers explicit and signal-based bounds checks.
Local Rosetta execution covers explicit bounds; it is correctness evidence only.
A BMI2-specific emission test now skips when the host target lacks BMI2.

v74 native compiler, allocator, encoder, and runtime package tests passed on hub.
Local ARM64 compiler/encoder packages and the AMD64 compiler package under
Rosetta also passed. Matched v74 timing and complete semantic-corpus results
are recorded below after completion. The 30% target remains unmet.


v74 Go-source SHA-256 is
`d938a75cb39b4698d93264af83fa3ac2c3b215100e3d0c3291fec0fd829081ba`;
local and remote source hashes match. All 21 semantic checks pass, as do the
127,776 standalone differential cases without an overlay and the new durable
139,392-case native matrix. `iterate-10` reports quick time ratio
**0.8532725021**, **14.6727% lower time**.
The standalone probe overlapped part of this quick run, so treat it only as a
ballpark result; the complete comparisons run after the gates finish.

`ab-memory-fold-01` compares v74 and the saved v73 binary over 65 modules /
72 exports, four paired 100 ms rounds. Ratio **0.9951243012**, **0.4876% lower
time**. Matmul is 0.9213, GEMM 0.9521, and syr2k 0.9174. Baseline QOI encode/decode
and candidate Doitgen have more than 20% spread. The aggregate remains modest
and noisy, while the three FP-kernel improvements repeat. The following
absolute run uses four paired 300 ms rounds against Railshot.


`full-11` completes the v74 comparison with Railshot: 65 modules / 72 exports,
four paired 300 ms rounds on AMD64 CPU 0. Time ratio
**0.8626424440**, **13.7358% lower time**,
**15.9229% higher throughput**. The complete raw
samples, source/binary pins, and export-coverage evidence are archived.
No sample series exceeds 20% max/min spread.
The 30% time-reduction target remains unmet. Production stays v74 while
the next isolated PCRE2 allocation and jump-table experiments are evaluated.


### PCRE2 allocation and dispatch probes after v74

The native PCRE2 profile identified functions 60 and 8 as the main work, with
large stack offsets and a hot relative jump-table dispatcher. Two independent
v74 overlays are archived in `pcre-probe-01`:

- `large-greedy` retains the giant-function source schedule, retry policy, and
  regional cutoff, but replaces its linear allocation path with the existing
  full greedy allocator. This isolates promotion/eviction from scheduling.
- `dispatch` replaces register materialization plus compare with an immediate
  compare, and address LEA + load + sign extension with one scaled signed load.
  Encoder goldens cover high registers, scales, base special cases, and signed
  displacements. Edge thunks, relative tables, and selector values are unchanged.

Both overlays pass native compiler/allocator/encoder/runtime suites and the
PCRE2 semantic check. Four reversed-order 200 ms rounds compare each with saved
v74 on CPU 0:

| Variant | PCRE2 time / v74 | Dispatch time / v74 |
| --- | ---: | ---: |
| Full greedy for giant functions | 1.0082455732 | 1.0161205619 |
| Compact jump-table dispatch | 0.9897397334 | 0.9967003844 |

No series exceeds 20% spread. Full greedy does not improve this workload and
is not promoted. Compact dispatch is a small focused improvement, pending
complete semantic and corpus measurements. Production remains v74.

A preceding Rosetta diagnostic (`large-greedy-rosetta-01`) passed PCRE2 semantics
and measured ratio 0.9936167814; it did not predict the native result. It is
retained as translated-execution diagnostic evidence only. The next isolated
probe uses the existing segmented allocator for giant functions, allowing
register reuse across CFG lifetime holes while retaining their source schedule.


`pcre-probe-02` substitutes segmented greedy allocation only on the giant-function
path. Native compiler/allocator/encoder/runtime suites and all 21 semantic
checks pass. Four paired 200 ms rounds against v74 on CPU 0 give:

| Export | Candidate / v74 time |
| --- | ---: |
| PCRE2 | 1.0075878881 |
| yyjson | 1.0410657807 |
| LZ4 compress | 1.0274170058 |
| LZ4 decompress | 1.0014166694 |
| zlib inflate | 1.0056644366 |

No series exceeds 20% spread. This variant also fails to improve the affected
workloads and is not promoted. Both allocation probes leave the production v74
source unchanged. Further work on giant functions needs a better allocation or
emission mechanism; simply enabling these existing expensive paths does not
resolve the measured slowdown.


`dispatch-full-01` completes the compact-dispatch comparison against v74 over
65 modules / 72 exports, four paired 100 ms rounds. All 21 semantic checks and
native package gates pass. Time ratio **0.9994370064** (0.0563% lower time),
essentially neutral. PCRE2 is 0.9934936080. Baseline Blake-as and candidate
Doitgen exceed 20% spread. The candidate remains diagnostic; production stays
v74 and its latest verified absolute result remains `full-11`.

### Vector memory operands: prototype after v74

The scalar-fold address and effect proof is reused in an isolated overlay for
full-width v128 loads consumed by packed f32x4/f64x2 add/subtract/multiply/divide.
Only add/multiply may commute. The current prototype retains unshared memory32,
the same-block eight-instruction window, and existing live-segment constraints;
vector reads may cross other private reads, but stores and different traps stop
motion. It emits a VEX packed arithmetic memory operand with an exact 16-byte
bounds check. Large offsets above uint32 remain unfused.

Focused planning tests fail on baseline and pass on the overlay, checking both
float widths and a store barrier. Native compiler/allocator/encoder/runtime
packages pass. A new packed-float matrix passes **156,816 cases**, checking
exact non-NaN lanes, WebAssembly NaN classes, whole memory, and trap codes across
aligned/unaligned/overlapping/invalid pointers with explicit and signal-based
bounds checks. The combined vector-fold plus previous graph-DCE overlay also
passes native package gates. Matched guarded-vector fixture measurements follow;
these are opportunity experiments, not results for an automatic vectorizer.


`vector-fold-probe-01` compares three compiler variants on the same original,
guarded vector, and guarded vector with hoisted broadcast fixtures. Four
reversed-order 200 ms rounds on CPU 0, after finite alias/trap, NaN, and n=1..100
full-memory gates, give these median times in microseconds:

| Compiler variant | Original scalar | Guarded vector | Vector, hoisted broadcast |
| --- | ---: | ---: | ---: |
| v74 | 82.7433 | 125.8115 | 123.0774 |
| Vector memory folds | 82.5416 | 122.5930 | 117.7874 |
| Vector folds + graph DCE | 82.4521 | 108.1537 | 107.8195 |

The combined overlay improves the vector fixtures by about 12–14%, but they
remain slower than the original scalar fixture. No sample spread exceeds 2.1%.
These are manual transformed-fixture experiments, not automatic vectorization
or fixed-corpus performance results. Production remains v74.

`vector-schedule-probe-01` then compares source, latency, pressure, and automatic
schedules with native loop cloning enabled and disabled, using the combined
overlay. All eight configurations pass guarded alias/bounds cases and complete
memory comparisons for matrix sizes 1–100. Four reversed-order 200 ms rounds:

| Schedule / loop cloning | Original scalar (us) | Guarded vector (us) | Hoisted vector (us) |
| --- | ---: | ---: | ---: |
| v74 baseline | 82.0837 | 125.9206 | 123.5205 |
| Automatic / on | 82.1928 | 108.3578 | 108.0459 |
| Automatic / off | 81.9448 | 108.3435 | 108.9623 |
| Source / on | 81.6053 | 108.0348 | 107.8375 |
| Source / off | 82.5934 | 135.4017 | 135.4368 |
| Latency / on | 83.5283 | 81.2201 | 81.2239 |
| Latency / off | 83.5960 | 81.4748 | 81.1421 |
| Pressure / on | 108.1079 | 108.5978 | 108.2552 |
| Pressure / off | 108.0510 | 108.4989 | 109.1046 |

All spreads are below 4%. Fresh per-configuration assembly and complete machine
plans are archived in `native/`. The latency vector loop has folded packed mul
and add, a vector store, and direct compare/branch; the automatic loop instead
materializes the comparison, updates another pointer, then tests it. This
explains a concrete emitted-code difference, without isolating its causal share
of the schedule improvement. Latency-scheduled vector execution only roughly
matches the original scalar fixture so far. Next opportunity: avoid repeating
whole-range guards in every inner-loop entry, with a valid dominating range
proof and the scalar fallback retained.


`vector-once-probe-01` moves a single whole-frame range guard immediately after
the third `memory.fill`, preserving the original scalar fallback. The guard
checks `u64(base) + 221184 <= u64(memory.size) << 16`. The fixture clamps n to
1..96, uses three separate 73728-byte arrays, and has no intervening memory
shrink or frame-base mutation. Thus the guarded B and C ranges are in bounds,
nonwrapping, and disjoint; each vector iteration updates two independent
contiguous elements. The ordinary odd-element remainder is unchanged. The
three successful fills already provide relevant dominating range evidence;
this explicit guard exposes the opportunity without implementing that proof
in the compiler.

Both the guarded fixture and a forced-fallback fixture pass return-value and
complete-memory checks against the original Railshot scalar module for sizes
1..100 plus 0, INT32_MAX, INT32_MIN, and UINT32_MAX. This is eight combinations
of fixture, compiler, and explicit/signal bounds per scheduler configuration.
All four overlay schedules and baseline pass. Four reversed-order 200 ms CPU 0
rounds give microsecond medians:

| Compiler / schedule | Original | Repeated guard | Guard once | Forced scalar fallback |
| --- | ---: | ---: | ---: | ---: |
| v74 baseline | 81.9651 | 123.6407 | 76.0674 | 82.0224 |
| Combined overlay / automatic | 81.7469 | 108.2269 | 75.4191 | 81.3759 |
| Combined overlay / latency | 82.3771 | 81.4959 | 75.6296 | 81.6063 |
| Combined overlay / source | 82.0198 | 108.1248 | 101.5427 | 82.5386 |
| Combined overlay / pressure | 108.7060 | 108.7053 | 77.1478 | 109.5014 |

No series exceeds 1.75% spread. Guard hoisting also changes register pressure
and automatic schedule selection: the once-guard plan selects latency, while
the repeated-guard plan selects pressure. The automatic transformed fixture is
about 7.7% faster than its original scalar fixture. This does not meet the
overall goal and is not a fixed-corpus result: no automatic vectorizer or
range-proof propagation has been added. Fresh plans, disassembly, Wasm inputs,
driver, hashes, and raw samples are archived with the result. Production stays
v74 pending the independent vector-fold full-corpus experiment.

`vector-fold-full-01` completes the isolated vector memory-fold comparison
against saved v74 across all 65 modules / 72 exports, four paired 100 ms rounds
on CPU 0. Native compiler/allocator/encoder/runtime packages and all 21 semantic
corpus checks pass. The aggregate time ratio is **1.0008178477**, a 0.0818%
increase, effectively neutral. No sample series exceeds 20% spread (baseline
maximum 1.0708, candidate maximum 1.1037). Export coverage, four samples per
export, paired ratios, module-balanced aggregation, and archived overlay hashes
were independently checked after retrieval. The overlay is not promoted.

Production Go source remains
`d938a75cb39b4698d93264af83fa3ac2c3b215100e3d0c3291fec0fd829081ba` (v74).
Its latest absolute result remains `full-11`: 13.7358% lower execution time than
Railshot. All experiments in this section are complete; there is no active
benchmark from these probes and no commit or push. The overall performance goal
remains unmet. Further SIMD work needs a general range/alias proof and better
loop emission before an automatic transform can earn promotion.

### Delayed integer comparison experiment after v74

`late-compare-full-01` tests a different way to remove a materialized comparison:
delay the pure integer compare until its single branch use, when both operands
already remain live in the same allocated locations. The eight-instruction,
same-block window rejects effects, traps, control flow, cold or affine
rematerialization, lifetime holes, physical overwrites, and region transitions
that change the operand location. It reads locations at the branch position and
retains the existing adjacent fusion path. It does not assume EFLAGS survives
intervening arithmetic. Functions over 4096 machine instructions are excluded.

Focused tests cover resident/spilled operands and reject dead left/right inputs,
physical overwrites, regional interference, cold rematerialization, selected
address rematerialization, calls, multiple uses, and already fused comparisons.
Local translated AMD64 compiler/allocator/encoder tests pass; native packages
and all 21 semantic corpus checks pass. Four paired 100 ms rounds against saved
v74 over all 65 modules / 72 exports give time ratio **1.0035597235**, a 0.3560%
increase. Candidate Doitgen is noisy (1.3815 max/min); the other series stay below
20% spread. LZ4 compress improves to 0.9767 and nanosvg to 0.9859, but the broad
result does not justify promotion. Export coverage and paired module-balanced
aggregation were independently verified. Production remains v74.

### Updated Railshot comparator

The active objective now requests **50% faster** execution and points out the
Railshot optimization PR. PR #780's current head is
`efa9aa22dfb3781284a55c465f7f57544eafade3`; its body reports 20% higher throughput
against its pinned main, with a different Go version and CPU affinity from the
Dragline runs. That reported ratio cannot be multiplied into our measurements.
The 13.7358% lower-time `full-11` result remains against this checkout's original
Railshot implementation. It is not a comparison with PR #780.

An exact PR source archive and matching benchmark are being prepared separately.
Its 65 execution module artifacts, arguments/results, and semantic check
contracts match the fixed Dragline corpus. The benchmark invocation and
calibration helpers also match exactly. PR #780 has no compiler or target
selection API: its baseline uses its ordinary host AMD64 Railshot backend.
The initial harness build attempted newer selector APIs and failed; that
attempt is retained in `pr780-baseline-01/build-initial-failure.txt`. The
corrected harness changes only the benchmark entry-point name. Its compiler
and runtime source are untouched. Both old and PR Railshot comparisons will be
reported, with the PR #780 comparator subsequently confirmed by the user.


`pr780-compare-01` completes the matched three-variant comparison: the original
Railshot and Dragline v74 binary, plus the exact PR #780 compiler/runtime source
built with Go 1.22.2. Same AMD64 host, CPU 0, GOMAXPROCS=1, signal bounds, four
paired 100 ms rounds, all 65 modules / 72 exports. PR #780 passes its 21 semantic
checks and repeated-instance checks before timing. Its benchmark calibration
and timed-invocation helpers are byte-for-byte identical to this checkout's.
The source archive, harness overlay, binary hashes, arguments, raw samples,
artifact pins, and historical PR body are archived. Coverage, paired aggregate
calculations, source archive, and overlay hashes were independently verified.

| Comparison | Module-balanced time ratio | Export-balanced time ratio |
| --- | ---: | ---: |
| PR #780 Railshot / original Railshot | 0.8079899590 | 0.8203502597 |
| Dragline v74 / original Railshot | 0.8631638928 | 0.8751448593 |
| Dragline v74 / PR #780 Railshot | **1.0690508320** | **1.0674836286** |

Current Dragline takes **6.9051% more time** than the PR compiler on the primary
module-balanced aggregate. The original-Railshot libtommath series and PR
many_funcs / utf-as-simd series exceed 20% spread; no Dragline series does.
These shared-host qualifications remain attached to the comparison. The PR's
reported 20% gain was measured against another revision/toolchain, so the
matched result above supersedes arithmetic guesses based on that report.
The 50% faster goal is not met against either comparator.

The largest current Dragline/PR time ratios include Jacobi-2D 1.8853, BiCG
1.8722, Jacobi-1D 1.6889, tiny.add 1.5679, GEMM 1.5246, GESUMMV 1.5148, and matmul
1.4686. The tiny-call gap includes runtime entry costs. The numerical-loop gaps
justify prioritizing general loop lowering over further small dispatch changes.

`loop-opportunities-01` adapts the PR's pure bounded loop analysis into a local
standalone diagnostic, using current Dragline StackFunc local types. It scans
2487 loops with no stack-lowering failures: 33 structural matches in 21 modules,
of which 18 pass the independent-lane or adjacent-output structural checks.
The scanner retains affine i32 expressions, source-ordered f64 event trees,
loads/stores, loop counter/limit, and local exit expressions. It does not emit
code or establish complete bounds, alias, trip-count, ownership, or exit-state
legality; structural acceptance is only a candidate list. The exact original
PR analysis sources, adapted scanner, provenance hashes, and every candidate
function/offset are archived. This provides concrete inputs for implementing a
shared loop-proof model in Dragline without dispatching compilation to Railshot.

All measurements in this section are complete. Production is still v74, with
Go source SHA-256
`d938a75cb39b4698d93264af83fa3ac2c3b215100e3d0c3291fec0fd829081ba`.
No commits or pushes were made.


The user has confirmed that the 50% speedup must be **over PR #780**. The pinned
current comparator is `efa9aa22dfb3781284a55c465f7f57544eafade3`, and the required
module-balanced time ratio is at most 2/3. Relative to measured v74, that means
approximately **37.64% further execution-time reduction**. A gain over the old
Railshot baseline alone cannot satisfy this goal.


### Automatic adjacent-output SIMD prototype (experimental)

`loop-vector-diagnostics-01` and `loop-vector-full-01` contain an actual compiler
transform, applied through Go overlays. It matches bounded scalar loops with
two adjacent f64 output trees and generates f64x2 operations through Dragline's
ordinary SSA and machine pipeline. Corpus inputs are unchanged. It preserves
FP operand order, computes integer exit locals simultaneously, and extracts the
correct lane for written scalar FP locals. Runtime guards prove trip count,
nonwrapping memory32 ranges, complete memory bounds, and disjoint or exactly
matching streams. Failed guards execute a byte-for-byte scalar loop copy.
Shared memory, memory64, unsupported effects, and FP recurrences are rejected.
The latest revision additionally requires an AMD64 target advertising AVX2 and
merges statically identical stream guards; that revision is measured separately.

An initial prototype failed because generated source offsets decreased when
entering the scalar fallback. The archived red test reproduces this. Generated
fast instructions now map to the original loop entry, while fallback offsets
retain the original instruction locations; monotonicity and module validation
pass. The source module remains unchanged.

The corrected first prototype passes native compiler/allocator/encoder/runtime
suites, all 21 semantic corpus checks, 3,584 finite guard/alias/trap cases across
three configurations, 1,152 NaN cases across three configurations, and matmul
n=1..100 with exact full-memory comparison. An exit-state fixture checks all
returned integer/FP locals, full memory, and traps for 4,480 cases under each
bounds mode. Both the transform alone and its vector-fold/DCE combination pass
the exit-state and native corpus gates before full timing.

`loop-vector-full-01` is four paired 100 ms CPU-0 rounds over all 65 modules /
72 exports against v74, using the same native AMD64 host and Go 1.22.2. Coverage
and module-balanced aggregation were independently checked:

| Experimental variant | Time ratio to v74 |
| --- | ---: |
| Adjacent-output transform | 1.0170427883 |
| Transform plus vector memory folding and DCE | 1.0092319319 |

Neither is promoted. Matmul regresses to 1.5596 / 1.2651 and GEMM to 1.5123 /
1.3155, respectively. Baseline JSON deserialize and both QOI exports, prototype
QOI exports, and combined Doitgen exceed 20% max/min spread; all raw samples
are retained. Production remains v74. The next diagnostic removes duplicate
range guards and compares schedule choices with native code dumps on the ten
structurally affected modules. Its subset results cannot establish a full-corpus
speedup or satisfy the PR #780 target.


`loop-vector-schedule-01` completes the target-qualified, deduplicated-guard
screen. Four schedule choices each pass the native package suite, finite
matrix/alias/trap cases, 8,960 exit-state cases, and 21 semantic corpus checks.
Four paired 150 ms rounds cover ten affected modules; no series exceeds 20%
spread. Overlay hashes, coverage, and paired ratios were independently checked.

| Schedule | Matmul / v74 | GEMM / v74 | Jacobi-1D / v74 |
| --- | ---: | ---: | ---: |
| Automatic | 1.2140 | 1.1260 | 1.2066 |
| Latency | 1.2311 | 1.2950 | 1.2904 |
| Source | 1.2171 | 1.1244 | 1.2398 |
| Pressure | 1.3401 | 1.2876 | 1.2269 |

Changing schedule policy does not recover the losses. Native matmul assembly
shows repeated zero-plus-address expressions, integer constant materialization,
and invariant splats inside the vector loop, along with heavily spilled guard
arithmetic. The automatic plan uses 144 spill bytes. A follow-up overlay screen
changes affine expression emission, hoists pure invariant local/constant splats,
and simplifies range arithmetic as separate cumulative variants. No schedule
screen result is promoted, and no full-corpus claim is made for the subset.


`loop-vector-lean-01` independently measures three cumulative changes: affine
expressions start from a variable instead of zero; invariant local/constant
splats move before the fast loop; range arithmetic drops zero offsets and uses
power-of-two shifts. Each variant passes the native packages, finite matrix and
alias/trap cases, 8,960 exit-state cases, and all 21 semantic checks. Four paired
150 ms rounds cover ten affected modules with no >20% spread. Archived overlay
hashes, export coverage, and paired ratios were independently checked.

| Variant | Matmul / v74 | GEMM / v74 | Jacobi-1D / v74 |
| --- | ---: | ---: | ---: |
| Prior unique guards | 1.1648 | 1.1447 | 1.2075 |
| Affine emission | 1.5173 | 0.9493 | 0.9977 |
| Plus invariant splats | 1.5242 | 1.0078 | 1.0227 |
| Plus guard arithmetic | 1.1940 | 0.9965 | 0.9151 |

`loop-vector-short-01` changes the guard conjunction into early branches to the
unchanged scalar fallback. The same gates and ten-module protocol pass, with no
>20% spread. Matmul is 1.1845, GEMM 1.1806, and Jacobi-1D 1.1393 relative to v74;
it does not consistently improve the prior guard-arithmetic variant (1.1988,
0.9983, 0.9155 in this run). Its hashes and ratios were checked independently.
None of these variants is promoted.

Native disassembly explains one confounding interaction: the simplified matmul
loop is below the existing 64-byte unrolling threshold and uses an out-of-line
backedge move block. The older larger loop was unrolled four times. A follow-up
screen lowers that profitability threshold to 32 bytes while retaining every
existing correctness qualification. The original cost-model unit test rejects
this intentional policy change (`copies = 3, want 0` at 63 bytes), archived in
`loop-vector-unroll-01`. The revised overlay explicitly tests 31/32/63-byte
boundaries before measuring; production policy is unchanged.


`loop-vector-unroll-02` passes native package tests, finite matrix/alias/trap
checks, 8,960 exit-state cases, and 21 corpus semantics for both cumulative
variants. It changes only the minimum iteration-size profitability threshold
from 64 to 32 bytes, with updated 31/32/63-byte boundary expectations. Four
paired 200 ms rounds on matmul, GEMM, and Jacobi-1D show no >20% series spread.
Overlay hashes, export coverage, and paired ratios were independently checked.

| Variant | Matmul / v74 | GEMM / v74 | Jacobi-1D / v74 |
| --- | ---: | ---: | ---: |
| Arithmetic guards | 1.1907 | 0.9983 | 0.9157 |
| Arithmetic guards, 32-byte unroll minimum | **0.8030** | **0.8786** | **0.9161** |
| Early-fallback guards | 1.1805 | 1.1811 | 1.1287 |
| Early-fallback guards, 32-byte unroll minimum | 0.8317 | 0.8718 | 1.1284 |

Restoring unrolling removes the matmul regression and yields a useful candidate.
This does not establish a full-corpus gain or the PR #780 goal. A separate probe
keeps register moves inline on hot self-loop backedges, testing the branch-layout
interaction without changing the unrolling threshold. Production remains v74.


`hot-backedge-01` separately keeps outgoing register moves inline for hot
self-loop true edges, using the existing move emitter and leaving the scalar
fallback/edge semantics intact. Both the production-only overlay and its
arithmetic-guard combination pass native packages, finite matrix/alias/trap
checks, 8,960 exit-state cases, and all 21 corpus semantic checks. Four paired
200 ms rounds over three loops have no >20% spread. Hashes, coverage, and paired
ratios were checked independently. The production-only ratios are 0.9990
matmul, 0.9933 GEMM, and 0.9944 Jacobi-1D. With the vector transform they are
0.9449, 0.9108, and 0.9109. The matched vector/unroll variant remains better at
0.8321, 0.8922, and 0.9061. These are subset screens, not full-corpus claims.

A fresh GitHub check reports PR #780 merged, with its head unchanged at the
pinned `efa9aa22dfb3781284a55c465f7f57544eafade3`. Its source remains the target
comparator. The matched five-variant full-corpus comparison is underway.


`loop-vector-full-02` completes the five-variant fixed-corpus screen: four paired
100 ms rounds, all 65 modules / 72 exports, against v74 and the exact PR #780
binary. All experiment gates pass before timing. Raw row coverage, raw samples,
paired per-export ratios, module-balanced aggregation, and archived overlay
hashes were independently checked by `a82a-loop-vector-verify-full-02.py`.
No export series exceeds 20% max/min spread.

| Variant | Module-balanced time / v74 | Module-balanced time / PR #780 |
| --- | ---: | ---: |
| v74 | 1.0000000000 | 1.0716670151 |
| Vector transform plus guard arithmetic and 32-byte unroll minimum | **0.9947216326** | **1.0655563076** |
| Inline hot backedge moves only | 1.0004322347 | 1.0720931401 |
| Vector transform plus inline hot backedge moves | 0.9967198773 | 1.0678001208 |

The strongest experimental gain is only **0.5278% lower time than v74**, despite
the larger numerical-kernel gains. It remains **6.5556% slower than PR #780**.
The goal remains unmet. A four-round 300 ms confirmation of v74, PR #780, and the
strongest candidate is underway; nothing from this screen has been promoted.

A subsequent independent-iteration overlay pairs two scalar iterations with one
store per iteration. It reuses the existing whole-loop range/alias proof and
requires an even trip count, preserving the original scalar loop for odd or
otherwise rejected trips. Affine exit locals account for the second iteration;
written FP locals extract the high lane. Local AMD64 structural, module, and
source-offset checks pass. Native gates and performance for this extension are
pending; this is not yet a validated optimization.


`loop-vector-full-03` confirms the strongest adjacent-output candidate over all
65 modules / 72 exports with four paired **300 ms** rounds. Raw samples, complete
coverage, paired ratios, module aggregation, and archived overlay hashes were
independently checked. No series exceeds 20% max/min spread.

| Comparison | Module-balanced time ratio |
| --- | ---: |
| Experimental adjacent transform / v74 | **0.9949824694** |
| v74 / PR #780 | 1.0717013807 |
| Experimental adjacent transform / PR #780 | **1.0662710373** |

The full-corpus gain is **0.5018% lower time than v74**. It remains **6.6271% more
time than PR #780**, far from the required 2/3 ratio. The larger matmul/GEMM
improvements do not generalize to most of the corpus. This candidate remains an
overlay while independent-iteration vectorization is evaluated. Production Go
source remains `d938a75cb39b4698d93264af83fa3ac2c3b215100e3d0c3291fec0fd829081ba`.


`loop-vector-independent-01` validates the independent-iteration extension on
native AMD64. In addition to the previous native/semantic/matrix/exit gates, it
passes another 8,960 exact independent-loop exit-state cases and 2,016 NaN cases
covering allowed FP results, unchanged finite memory, and exact trap codes.
Four paired 200 ms rounds cover eight modules with no >20% series spread.
Overlay hashes, coverage, and paired ratios were independently checked.

| Module | Independent / v74 | Independent / adjacent candidate |
| --- | ---: | ---: |
| Matmul | 0.7814 | 0.9997 |
| GEMM | 0.8583 | 1.0010 |
| Jacobi-1D | 0.9212 | 0.9986 |
| ADI | 0.9988 | 0.9992 |
| Correlation | 1.0124 | 1.0218 |
| FDTD-2D | 1.0062 | 1.0485 |
| Heat-3D | 1.0549 | 1.0517 |
| Jacobi-2D | 0.8181 | 0.8162 |

`loop-vector-independent-dump-01` corrects the diagnostic dump to function 1,
where these PolyBench kernels live; previous function-0 dumps were setup code.
It changes only diagnostic output selection and uses single invocations that
are **not performance measurements**. Correlation function 1 is byte-identical
between variants; its small timing variation does not identify a code change.
Heat-3D has 18 scalar iterations in its inner loops. Its weighted spill estimate
rises from 6,630 to 304,271 and its spill frame from 80 to 224 bytes after the
transform. Jacobi-2D improves despite a larger frame, underscoring that the
allocator score alone is not a reliable profitability decision.

A revised profitability overlay leaves independent loops scalar when an
immediately preceding constant counter initialization and constant limit prove
fewer than 16 packed iterations. Unknown trip counts retain the original runtime
guards. The archived red test shows 0/18/30-trip loops were previously transformed;
the revised 0/18/30/32/64 boundary test passes locally. Native gates and the
profitability screen are pending. All loop-transform work remains experimental.


`loop-vector-profit-01` completes native validation and the eight-module
profitability screen. All package, 21 corpus-semantic, finite alias/matrix/trap,
prior exit-state, independent exit-state, and 2,016 NaN gates pass. Four paired
200 ms rounds have no >20% spread; archived overlay hashes, coverage, and paired
ratios were independently verified. The profitability candidate takes 1.0014
v74 time for Heat-3D, recovering the unfiltered independent variant's 1.1269 in
this run. Jacobi-2D remains 0.8212; FDTD-2D is still 1.0099 versus v74 and loses
the adjacent-only candidate's 0.9694 ratio. Matmul is 0.8630, GEMM 0.8853, and
Jacobi-1D 0.9207. These subset timings do not define the fixed-corpus aggregate.

`loop-vector-full-04` is now measuring the profitability candidate, v74, and
PR #780 over the complete 65-module / 72-export corpus with four paired 300 ms
rounds. Its result is pending. Production is unchanged and no commits or pushes
have been made.


`loop-vector-full-04` completes the independent-pairing profitability candidate
with four paired 300 ms rounds over all 65 modules / 72 exports. Raw coverage,
samples, ratios, module aggregation, and overlay hashes were independently
verified. The candidate/v74 ratio is **0.9936780705** (0.6322% lower time). Its
ratio to PR #780 is **1.0619044798**, while v74/PR is 1.0683813326 in this run.
Neither v74 nor the candidate has >20% spread, but PR Deriche (1.2824), QOI encode
(1.7859), and QOI decode (1.4179) do; the PR-relative figures retain that noise
qualification. The goal is still unmet, and the transform remains an overlay.

The preceding adjacent-only full run shows regressions in scalar `memory`
(1.1217) and `fastfloat` (1.0691). The 32-byte minimum was applied to every loop,
so a separate policy overlay now limits it to blocks containing selected packed
FP add/sub/mul/div. Scalar and integer-SIMD loops retain the original 64-byte
minimum. Local 31/32/63/64-byte policy cases pass. Native gates and the ten-module
screen are underway; no broader performance claim is yet available for this
revision.

PR #780's runtime changes also include inlineable direct-call admission and
atomic gate ownership. This checkout uses the older prepared-invocation mutex
layout and lacks those gate fields. The patches were inspected but not copied;
a lifecycle-preserving adaptation or base integration is separate work from the
loop transform. No runtime source was changed in this experiment series.


`loop-vector-packed-01` passes all native correctness gates and the ten-module
screen. Four paired 200 ms rounds have no >20% spread; overlay hashes, coverage,
and ratios were independently verified. Restricting the 32-byte minimum to packed
FP loops does **not** recover the scalar losses: `memory` is 1.1231 versus v74
(1.1046 under the prior policy), and `fastfloat` is 1.0506 (1.0558 prior). Matrix
and stencil ratios remain essentially unchanged. This falsifies the hypothesis
that broad short-loop unrolling is the primary scalar regression. The restriction
is not promoted; native code/plan dumps of v74 versus the profitability candidate
are the next diagnostic. One-iteration diagnostic invocations are not benchmarks.


`scalar-path-dump-01` shows both `memory` native functions are byte-identical
between v74 and the profitability candidate (193 and 249 bytes). Fastfloat's
parser function is also identical (36,304 bytes); its wrapper changes under the
broad unroll policy, but the packed-only rule restores that wrapper byte-for-byte
to v74 as well. Thus the remaining scalar timing gap cannot be attributed solely
to differences in these emitted instruction bytes. Runtime metadata, placement,
entry overhead, and binary layout are still possible causes; none is proved.

`scalar-controls-01` compares the original binaries and diagnostic-only rebuilds
with dumping disabled, six rotated/reversed paired 500 ms rounds over memory and
fastfloat. No series exceeds 20% spread. Coverage and paired ratios were checked.

| Binary / v74 | Memory | Fastfloat |
| --- | ---: | ---: |
| v74 diagnostic rebuild | 0.9987 | 1.0268 |
| Profitability candidate | 1.1207 | 1.0656 |
| Candidate diagnostic rebuild | 1.1330 | 1.0498 |
| Packed-only unroll policy | 1.1417 | 1.0398 |

Diagnostic rebuilding changes the small timings but does not eliminate the
candidate's memory gap. This remains an unresolved performance diagnosis;
identical function bytes alone do not prove identical runtime placement or
admission paths. All measurements above are complete. No native job remains
running from this checkpoint. Production is v74, all new loop changes are Go
overlays, and no commit or push has been made. The 50%-over-PR-780 goal is unmet.


## Runtime admission diagnostics and lease adaptation

`entry-state-01` records entry flags and address alignment modulo 64 with
one-iteration invocations; those invocation times are not benchmark evidence.
For `memory`, v74, the profitability candidate, and the packed-only policy have
identical inspected flags and alignment: direct isolated integer entry, no
bounded-work proof, ordinary direct mode, entry 16 and direct entry 32 modulo 64.
This rejects those inspected differences as an explanation of the scalar gap.
It does not establish identical placement at larger address granularities.

`direct-lease-01` adapts only the private uncontended lifetime admission from
PR #780 to this checkout. A successful 0-to-1 CAS has no managed Runtime
accounting. Busy, borrowed, closed, and managed states use existing admission;
release uses the existing finalization path if the sole-open-lease CAS fails.
The existing invocation gate, resource revocation, trap handling, and deferred
release order are retained. All changes are overlays.

Both the v74 and loop-profitability versions pass the native `src/wago` suite
(including close/lifetime tests), explicit admission/accounting cases, and all
21 semantic corpus checks. Six rotated/reversed paired 300 ms rounds cover
six modules / seven exports. Raw coverage, samples, paired ratios, spreads,
and archived overlay hashes were independently verified. No series exceeds
20% spread.

| Time / v74 | Tiny | Dispatch | Many functions | Memory | Fastfloat |
| --- | ---: | ---: | ---: | ---: | ---: |
| PR #780 | 0.6392 | 0.7093 | 0.6691 | 0.8009 | 1.0165 |
| Loop profitability | 0.9936 | 0.9971 | 0.9954 | 1.1274 | 1.0662 |
| Lease on v74 | 0.8972 | 0.9318 | 0.8949 | 0.9857 | 1.0343 |
| Lease on loop profitability | 0.8980 | 0.9294 | 0.8950 | 0.9860 | 1.0671 |

These are focused results, not a full-corpus improvement. The loop candidate's
memory gap disappears in this runtime overlay despite unchanged compiler code,
which narrows attention to entry/runtime/layout interactions without proving a
specific cause. JSON export ratios remain near 1.0. The 50%-over-PR-780 goal is
unmet.

Native Go 1.22.2 inlines `tryBeginDirectInvocation` (cost 61), but the initial
release helper's redundant nil check takes it to cost 82, above the budget 80.
A separate overlay removes that check only from release, whose caller already
holds a lease on a non-nil instance; it inlines at cost 78. Public nil-invocation
handling is unchanged. Its native and semantic gates pass; the focused screen
is pending. Cached invocation-gate and exact-state release probes are prepared
separately. Builders and verification scripts are archived under
`runtime-lease-diagnostics-01`. No production source is promoted or committed.


`direct-lease-inline-01` completes the six-round screen. Inline-release / v74
ratios are 0.8733 tiny, 0.9059 dispatch, 0.8624 many-functions, 0.9797 memory,
and 1.0433 fastfloat. Combining with the loop profitability overlay gives
0.8745, 0.8999, 0.8627, 0.9832, and 1.0548 respectively. All raw coverage,
paired ratios, native/semantic gates, and source hashes were verified. Neither
new inline-release variant has >20% spread; the older lease-v74 tiny control
has 1.3399 spread. This remains focused evidence and is still slower than PR
on the small-call cases. Cached-gate screens are now running sequentially on
the same native CPU. Production remains unchanged.


`direct-gate-01` completes three cached-gate adaptations on v74. All pass the
native package and 21 semantic checks. Six rotated/reversed paired 300 ms rounds
have no >20% spread; raw coverage, ratios, and archived hashes were verified.
The cached word is exactly `ensurePluginState().invokeMu`; production initializes
that state once via CAS and never replaces it. Admission validates fast-state
flags only after acquisition, and invalid acquired state is released before
falling back. Existing deferred gate-before-lifetime release is preserved.

| Time / v74 | Tiny | Dispatch | Many functions | Memory | Fastfloat |
| --- | ---: | ---: | ---: | ---: | ---: |
| Cached gate | 0.7405 | 0.7595 | 0.7766 | 0.9671 | 1.0392 |
| Cached gate + exact fast release | 0.7429 | 0.7494 | 0.7730 | 0.9654 | 1.0178 |
| Split body + exact fast release | 0.7682 | 0.7705 | 0.7356 | 0.9720 | 1.0184 |
| PR #780 | 0.6384 | 0.7069 | 0.6698 | 0.7950 | 1.0174 |

Exact fast release CASes only Held|Fast to zero; waiter or revocation bits force
the original Unlock path. Explicit tests cover all these states, notifications,
and unowned-release panic. Native Go 1.22.2 inlines this helper (cost 71) and
cached acquisition (cost 43). The split-body alternative trades small-call wins
and is not selected. The combined cached-gate/fast-release candidate is selected
for a full-corpus measurement, both on v74 and on the loop-profitability overlay.
It remains slower than PR in this small-call screen; the target is unmet.

`runtime-full-01` is underway with all 65 modules / 72 exports and four paired
300 ms rounds. It includes repeated race-enabled lifetime/revocation/close tests,
all compiler/runtime gates, finite/exit/NaN oracles, and semantic coverage before
timing. Its result is pending. No production source has been promoted.


`runtime-full-01` is complete. The selected runtime adaptation alone is
**0.9839912589 times v74** and **1.0564203406 times PR #780**. Combined with the
loop-profitability overlay it is **0.9756003510 times v74** and
**1.0477979910 times PR #780**. The same run measures v74/PR at 1.0744570527.
All 65-module / 72-export raw coverage, samples, paired ratios, both geometric
aggregations, overlay hashes, native/semantic gates, and repeated race gates
were independently verified. Finite guards/alias/trap checks, exact exit locals,
and NaN checks pass before measurement.

Noise qualification: v74 QOI encode and decode have spreads 1.5586 and 1.7985;
PR Gram-Schmidt is 1.2135; combined Doitgen is 1.3989. The runtime-only candidate
has no >20% series. The 2.44% time reduction versus v74 is therefore qualified
by those raw-series limitations; it is still far short of the requested target.
No production performance changes have been promoted from this series.

## Full-width vector cycle preservation

The wider-loop design investigation found a concrete existing correctness
bug: `MoveSaveTemporary` in both finalizers saved FPR values with a scalar move,
including `TypeV128`. AMD64 emitted MOVSS and ARM64 emitted a 32-bit FMOV. A
full vector save must preserve all 128 bits.

`vector-cycle-diagnostics-01` contains focused encoding tests across both
scratch temporaries, several source registers, and scalar/vector widths. The
new byte-oracle tests fail before correction and pass with vector-width saves.
A Wasm fixture swaps two v128 locals across a loop, then checks the entire
memory against an independent byte oracle. It reproduces data loss on ARM64
and native AMD64 in explicit and signal bounds modes (seed 0, iteration 1,
byte 36 is zero instead of 0x8d). AMD64 under local Rosetta cannot execute the
fixture because the host feature check rejects SIMD; those local execution
logs are not native evidence.

`vector-cycle-01` archives native AMD64 red/green results. Fixed compiler,
encoder, and runtime packages pass, as do 21 corpus semantic checks and the
standalone Railshot/Dragline vector-swap oracle. The fix uses VMOVDQU on AMD64
and NEON ORR on ARM64; scalar saves retain their existing instructions. The
overlay increments the function-artifact revision to v75 so stale compiled
functions cannot retain the truncated vector save. The broader local ARM64
suite is still pending. Production remains unchanged at this checkpoint.

`loop-bindings-01` is a design diagnostic, not an implementation or benchmark.
It scans 2,487 loops, finds the same 33 bounded structural plans with no frontend
failures, and records pre-simplification local SSA entry/exit bindings. All 18
independent/adjacent candidates have a self-edge and one exit edge; all bindings
are available. Most retain one or two operand-stack values across the loop.
Any native region interface must preserve those values and other live-through
values as well as local exit state. PR's four-lane loop support and Dragline's
current two-lane prototype remain distinct; wider emission is not implemented.


### v75 production checkpoint

The vector cycle fix is now applied locally, including both finalizers, focused
encoding tests, a 136-case full-memory loop-swap regression (two bounds modes,
four seeds, seventeen trip counts), and function artifact revision v75. Focused
checks pass on native ARM64 and in the local AMD64 encoding test. Native AMD64
full packages, the execution oracle, and 21 semantic checks passed on the exact
fix overlay before formatting/promotion. Source SHA-256 after promotion is
`ae51096283f2dad2ec3033e0cb680aeb877e6dc162027926a689e8288bd20415`.

The broader ARM64 runtime suite is **not green**. It reports JSON startup and
branch-cast failures and stalls in `loop_result_if`. A stack sample was captured
and the test process was deliberately terminated with SIGQUIT after over four
minutes. The same failures and ten-second loop-result timeout reproduce without
the fix on v74 (`a82a-vector-cycle-arm-baseline.txt`). These are existing branch
failures, not newly attributed to this fix. Disabling the ARM64 native backedge
fusion pass does not remove them; that diagnostic overlay is not applied.

`runtime-full-02` attempted a fresh comparison on the corrected v75 baseline,
but stopped **before timing** because `TestManagedForkContext/after-resume` and
`TestCloseInterruptsInfiniteInvocation` timed out in the runtime candidate's
package gate. Its failed logs and exact overlay sources are archived. The
isolated `runtime-gate-control-01` then passes both tests for v75 and the runtime
candidate with and without CPU pinning. This does not explain the suite failure.

`runtime-full-03` repeats the v75 runtime suite three times, then each candidate's
complete package gates three times with ordinary test scheduling; only benchmark
and oracle execution remains pinned. Repeated race gates and the existing loop
oracles precede a fresh four-round 65-module comparison against PR #780. All gates passed and the job completed. The results are recorded below. Runtime
and loop performance candidates remain overlays. No commit or push has been made. The
50%-over-PR-780 target remains unmet.


### v75 full comparison completed: runtime-full-03

All 65 modules / 72 exports completed four paired 300 ms rounds, with rotated
and reversed variant order on native AMD64 CPU 0. Native package gates passed
three repetitions for v75 and each candidate; combined race and loop oracles
also passed. `runtime-full-03/verify.py` rechecks archived source hashes, raw
coverage, samples, ratios, aggregates, native/semantic gates, and race results.

| Variant | Paired time / v75 | Paired time / PR #780 |
| --- | ---: | ---: |
| v75 | 1.0000000000 | 1.0700382570 |
| Runtime adaptation | 0.9851268418 | 1.0544004910 |
| Runtime plus loop profitability | 0.9766518426 | 1.0452482256 |

There is no >20% within-export max/min spread for v75 or PR. Runtime-only is
noisy in Doitgen (1.3874), QOI encode (1.5122), and QOI decode (1.7680).
The combined candidate is noisy in Doitgen (1.4080) and SYRK (1.4791).
The combined module-balanced result is 4.52% longer time than PR, far from the
required ratio of at most 2/3. No performance transform is promoted by this run.

### ARM64 correctness repairs: v76 and v77

`arm64-correctness-01` archives the red/green evidence and exact promoted files.
The v76 repair emits the return epilogue at the synthetic exit's layout position,
as AMD64 already does. Otherwise returning paths can fall into cold code placed
after that exit. The new encoding regression fails on v75; JSON initialization,
branch-cast helpers, and the previously hanging loop-result cases all pass after
the repair. Focused production checks passed three repeats.

The first broad run still found two existing failures, both reproduced on v75.
The nested branch returned zero instead of seven because a predicated edge copy
read a register whose constant producer had been folded away. v77 rejects that
predication and uses the normal edge rematerialization path. An added predicate
regression fails before the repair; the existing runtime control-flow regression
passes three repeats after it.

The other failure was a stale prepared-call test assertion: signal-bounds
memory-free direct entries use `directIsolated`, while `isolatedFast` describes
the general private entry. The test now checks the direct field, consistent with
the existing guard-page direct-entry test. Runtime entry policy is unchanged.
With both fixes, all ARM64 compiler, RailMach/SSA/spec, encoder, and runtime
packages pass three repeats on the combined overlay. The final production check
also passes all those packages (one repeat). Native AMD64 v77 was not rerun;
the latest native full comparison remains the archived v75 run. Source SHA-256 and per-file hashes are in
`arm64-correctness-01/promotion.json`. Changes remain uncommitted and unpushed.


### Width control and scalar memory recurrence prototype

`wide-controls-01` compares the exact pinned PR binary with both wide-loop flags
on and off, plus the previous runtime/loop Dragline candidate. Six paired 500 ms
rounds over six kernels have no >20% spread. Disabling the wide paths increases
PR time by 27.7% in Jacobi-1D, 26.2% in Jacobi-2D, 30.2% in GEMM, and 51.9% in
matmul. BiCG and GESUMMV change only about 1.0% and 0.6%. Their dominant remaining
gap instead matches PR's scalar invariant-cell recurrence optimization.

The new scalar prototype adapts PR's bounded recurrence admission to Dragline's
Wasm-to-SSA path. Complete trip/range/alias guards precede scalar temporaries
for up to two invariant memory cells. It preserves the source arithmetic/event
order and stages all local exits before publishing them. A failed guard executes
the original checked loop. Every other store/read range must be disjoint from
the delayed cells; shared memory and memory64 remain excluded. No reassociation
or FP vector widening is used for this path.

`scalar-recurrence-01` passes native AMD64 compiler/runtime packages and 21
semantic checks. Five fixtures cover one delayed cell plus moving stores, two
delayed cells, and memory immediates 7, 65528, and 4294967295. Each fixture checks
13,056 inputs against Railshot in explicit and signal bounds modes: complete
memory, result locals, and trap codes, with permitted NaNs recognized. Total:
65,280 inputs, each checked in both modes. Focused structural checks also validate
the transformed Wasm and both one/two-cell forms locally under AMD64 execution.

The six-round 300 ms eight-module screen has no >20% spread:

| Kernel | Prototype / previous candidate time | Prototype / PR #780 time |
| --- | ---: | ---: |
| BiCG | 0.585571 | 1.104725 |
| GESUMMV | 0.689243 | 1.024901 |
| 2MM | 1.002170 | 1.002442 |
| 3MM | 0.991865 | 0.990866 |
| ATAX | 1.010121 | 1.089901 |
| Trisolv | 1.006717 | 1.027082 |
| GEMM | 0.999056 | 1.381680 |
| matmul | 0.998622 | 1.314116 |

This is a focused screen, not a new corpus-wide performance conclusion.
The transforms and runtime changes remain overlays; local production remains
v77. `scalar-recurrence-full-01` rebases both the previous candidate and this
prototype onto the exact v77 source snapshot and compares them with v77 and
pinned PR #780 across all 65 modules / 72 exports. Source composition verifies
SHA-256 `41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f`.
The temporary merge retained the new ARM64 exit regression absent from the older
experiment; no repository merge or commit was performed. Fresh native, semantic,
race, and recurrence-oracle gates preceded timing. A host reboot interrupted
this run before it produced a result. All timing was restarted in full-02.
Scripts, source fixtures, and provenance are in `scalar-recurrence-diagnostics-01`.


The v77-rebased recurrence candidate also passes all relevant native ARM64
compiler, encoder, and runtime packages (one full repeat). This checks the
shared runtime adaptation against the v77 correctness fixes; the new scalar
transform is feature-gated to AMD64. The exact local log is archived with the
recurrence diagnostics. Native AMD64 v77 and the previous-candidate package
gates have passed. The completed full-02 comparison is recorded below.


### v77 full comparison completed: scalar-recurrence-full-02

The host reboot interrupted full-01. Its original SSH exited 255, no runner
remained, and no results.json existed. The restart rechecked exact binary and
source hashes, reran all 21 semantic cases for all three Dragline variants, and
started all four paired 300 ms timing rounds from scratch. No pre-reboot timing
was retained. Native package, race, and five recurrence-oracle logs come from
the exact same source/binaries before reboot; metadata labels that provenance.

| Variant | Time / v77 | Time / PR #780 |
| --- | ---: | ---: |
| v77 | 1.0000000000 | 1.0698829080 |
| Runtime + vector profitability | 0.9796094123 | 1.0480139029 |
| Above + scalar recurrence | 0.9638182353 | 1.0311185780 |

`scalar-recurrence-full-02/verify.py` independently validates all 65 modules /
72 exports, raw samples, paired ratios, aggregation, source hashes, and gate
logs. The recurrence candidate improves 1.69% over the previous candidate but
still takes 3.11% longer than PR #780. Its Doitgen (1.371×), QOI encode (1.512×),
QOI decode (1.781×), and tiny.add (1.248×) max/min spreads exceed 20%. v77
Doitgen is also noisy (1.395×); PR and the previous candidate have no such rows.
The 50% throughput goal remains unmet. Performance transforms remain overlays.


### Native four-lane region prototype: native-wide-02

A typed-stack adapter captures bounded straight-line loops before SSA scratch is
released. It maps loop locals and stack-prefix values to native SSA homes and
emits a guarded four-lane f64 region on its sole incoming edge. The original
loop handles failed guards. Integer/FP outputs are staged simultaneously; the
last lane is preserved. The frame reserves 1,024 bytes through normal frame
composition, and the artifact records its AVX2 requirement in cold and cached
compilation. Windows, memory64, shared memory, and fragmented functions are
excluded at this stage. Native instruction source metadata remains incomplete.

Raw capture found 12 corpus regions. Five regions in ATAX, GEMM, Jacobi 2D, and
SYMM qualify after allowing an overwritten input local to have no allocated home.
A previous conservative check incorrectly rejected those dead input bindings.
Three fixtures check plain loops, live integer/FP outputs, and adjacent outputs
with a live stack prefix. Each passes 1,152 inputs in both bounds modes against
Railshot, comparing full memory, returned values, and traps (permitted NaNs).
Instrumented copies record 136 successful wide entries and 2,168 fallback
entries per fixture. The uninstrumented sources are used for timing.

Native compiler/SSA/allocator/encoder packages, focused Dragline/Prepared runtime
tests, 21 corpus semantic cases, and selected corpus results pass. Local AMD64
(Rosetta) and ARM64 compiler checks also pass. The full runtime gate is **not
green**: `TestManagedTableReentryCleanup` hung during cancellation, and unchanged
v77 reproduces the hang on its second isolated repeat. It contains no memory
loop eligible for widening. The interrupted run and baseline timeout are kept in
`native-wide-01`; this candidate remains a prototype.

`native-wide-screen-01` measures six paired 300 ms rounds with exact binaries
from v77, PR #780, the recurrence candidate, and the wide-only prototype.
All recorded max/min spreads are at most 20%; the raw sample audit passes.

| Kernel | Wide / v77 time | Wide / recurrence time | Wide / PR #780 time |
| --- | ---: | ---: | ---: |
| ATAX | 0.917952 | 0.947736 | 1.026578 |
| GEMM | 1.195001 | 1.349246 | 1.750893 |
| Jacobi 1D | 1.000278 | 1.088743 | 1.644673 |
| Jacobi 2D | 0.642073 | 0.777132 | 1.197442 |
| SYMM | 1.000325 | 1.020237 | 0.930173 |
| matmul | 0.987299 | 1.203816 | 1.514394 |

The Jacobi 2D improvement establishes a useful native wide path, but GEMM
regresses. Jacobi 1D and matmul do not qualify due to allocation fragments.
This focused result does not replace the complete recurrence comparison or
meet the target. No performance transform has been promoted.


### Wide path diagnosis and scalar remainder handoff

`native-wide-paths-01` instruments the qualified entries separately from timing.
GEMM records zero successes and 148,800 failed group-divisibility checks; ATAX
and Jacobi 2D enter the fast path, and SYMM's qualified region runs rarely.

`native-wide-03` permits instruction-local register fragments whose primary
spill homes remain authoritative at control edges. The allocator verifies that
fragments do not cross a control boundary; the enclosing emitter flushes pending
spills and restores displaced registers before the bridge. Profile-guided
callee-save regions are excluded. All 12 captured corpus regions now qualify,
including matmul and Jacobi 1D. Scoped native packages, three differential
fixtures, all 21 semantics, and all eight selected corpus results pass.

`native-wide-04` runs complete vector groups then sends any remainder through
the original scalar loop. It passes the three small fixtures and semantic set,
but **fails GEMM with an out-of-bounds trap**, so no timing was taken. The
loop's dead address temporary v310 and live counter v321 both had R14 as their
allocated home. Publishing every Wasm local wrote the dead address over the
counter before the backedge copy. The small fixtures did not reproduce that
register reuse; the original GEMM artifact remains the runtime regression.

`native-wide-05` publishes only SSA values live at the loop edge, using the
allocator's intervals and optional live segments. Its planner regression checks
that GEMM publishes the counter and omits the two dead address temporaries.
Native validation and a fresh focused screen are complete below. All revisions remain
experimental overlays, and the full baseline runtime cancellation failure is
still unresolved.


### Native wide screen after live-output repair: native-wide-screen-02

`native-wide-05` passes the compiler packages, focused runtime tests, all three
differential fixtures, 21 semantic cases, and all eight selected corpus result
gates. GEMM's original failing artifact is now green. The screen compares exact
binaries for v77, PR #780, recurrence, wide-03, and wide-05 in six paired 300 ms
rounds. Catalog and Wasm artifact hashes are rechecked for both source trees.
All max/min spreads are at most 20%; the independent raw-sample audit passes.

| Kernel | Wide-05 / v77 | Wide-05 / recurrence | Wide-05 / PR #780 |
| --- | ---: | ---: | ---: |
| ATAX | 0.914427 | 0.946590 | 1.017374 |
| GEMM | 0.815170 | 0.922409 | 1.213972 |
| Jacobi 1D | 0.689511 | 0.749975 | 1.133718 |
| Jacobi 2D | 0.643374 | 0.780355 | 1.191685 |
| SYMM | 1.000498 | 1.019954 | 0.932766 |
| matmul | 0.891574 | 1.079763 | 1.365593 |
| FDTD 2D | 0.763886 | 0.754585 | 0.826451 |
| Heat 3D | 1.142626 | 1.138655 | 1.146339 |

The scalar remainder handoff improves GEMM and Jacobi 1D substantially. Heat 3D
still regresses, and matmul is slower than the earlier recurrence/vector
candidate. The next profitability work must account for short loops and the
register-save/guard overhead. The 50% target remains unmet; this focused screen
does not replace the full 65-module comparison recorded at the top.


### Combined candidate validation

`native-wide-paths-05` confirms that GEMM, Jacobi 1D, and Heat 3D now execute the
wide path and the scalar remainder, with no group-divisibility fallback. Its
instrumented corpus checks all pass; its timings are not used for speed claims.

`native-wide-combined-01` composes wide-05 with the previously validated runtime
adaptation, scalar recurrence transform, and two-lane vector fallback. The
bytecode stage retains loops matching the native-wide shape for later typed
planning; scalar recurrences retain first priority. The bounded-loop parser is
shared. Temporary file merges preserve both sets of compiler/encoder additions
and tests; no repository merge, commit, or promotion is performed. A prior
bytecode-vector assertion now checks native region selection for that shape.

Native compiler packages, focused Dragline/Prepared runtime tests, 21 semantic
cases, three wide fixtures, five recurrence fixtures (65,280 inputs, each in
two bounds modes), and all ten selected corpus results pass. Local ARM64 full
compiler/encoder/runtime packages pass. The ten-module full-memory differential
check also passes three repeated calls per module in each bounds mode.
The completed full timing comparison is below. Production Go source
still hashes to `41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f`.


`native-wide-combined-full-01` now compares the combined candidate against pinned
PR #780 over all 65 modules / 72 exports, four paired 300 ms rounds. It verifies
binary, source, catalog, and both trees' artifact hashes; all selected execution
results are gated before any timing samples. The source snapshot and prior
scoped native, semantic, recurrence, and full-memory checks accompany the run.
The completed result is recorded below. The unresolved baseline cancellation
hang remains a qualification on full runtime coverage.


`native-wide-06-local` is a separate, unmeasured encoding prototype. It folds a
single-use moving f64 load only when the very next FP event consumes it as the
right operand of add/subtract/multiply/divide. No store or FP operand ordering
is crossed. The packed memory form has golden encoding checks, and local
AMD64 compiler tests pass. Native execution and timing are pending; this source
is not part of the running combined full comparison.


### Completed combined full comparison: native-wide-combined-full-01

All 65 module result gates and all 72 execution exports pass. The four paired
300 ms rounds yield a module-balanced candidate / PR #780 time ratio of
**1.022188825099988** (throughput ratio **0.9782928314660283**). Neither candidate
nor PR has a max/min spread over 20%. The independent verifier rechecks raw
coverage, samples, paired medians, module/export aggregation, source archives,
and the archived scoped gates. Local source composition independently matches
the run's effective Go-source SHA-256
`df9623a40ba6bc052988e1f647b9a4538e61082861bf16e64873a0d8d9563f9a`.

The candidate still takes 2.22% longer than PR #780, far from the 2/3 time
target. This complete-corpus checkpoint is superseded by full-02 below; it did not
meet the target. The full runtime cancellation issue remains unresolved.
The vector memory-operand prototype in `native-wide-06` is now undergoing native
validation; it is not part of this measured combined candidate.


### Packed memory operands: native-wide-screen-03

Native-wide-06 passes native compiler packages, focused runtime checks, all 21
semantics, three differential fixtures, eight selected execution oracles, and
exact full-memory equality for ten modules called three times in each bounds
mode. Six paired 300 ms rounds compare wide-06, wide-05, and pinned PR #780.
All max/min spreads stay within 20%; the raw sample verifier passes.

| Kernel | Wide-06 / wide-05 time | Wide-06 / PR #780 time |
| --- | ---: | ---: |
| ATAX | 1.007158 | 1.024358 |
| GEMM | 0.992069 | 1.197403 |
| Jacobi 1D | 0.983066 | 1.130200 |
| Jacobi 2D | 0.976693 | 1.166346 |
| SYMM | 1.000188 | 0.933910 |
| matmul | 1.000569 | 1.347155 |
| FDTD 2D | 0.981569 | 0.816781 |
| Heat 3D | 0.977179 | 1.118372 |

This is a modest focused improvement; the latest full comparison remains the
2.22% longer execution time than PR recorded above.

### Hoisting and register preservation probes

Native-wide-07 computes invariant memory loads and their invariant FP expression
trees once after whole-loop range and alias guards. The guards prove those loads
are disjoint from writes. Operand order is retained; there is no reassociation.
Native-wide-08 adds bounded emission preflight to identify the actual FPR clobber
mask, preserving those registers plus input FPRs and scratch XMM15. All GPRs
remain preserved. This reduces unnecessary XMM saves and restores.

Both pass native compiler packages, focused runtime tests, 21 semantic cases,
five differential fixtures (including folded RHS and invariant-expression
cases), eight selected corpus results, and ten-module full-memory checks in
both bounds modes. Local AMD64 encoding/compilation and ARM64 compilation also
pass for wide-08. `native-wide-screen-04` completes six paired 300 ms rounds against wide-06 and
PR #780. Every variant stays within a 20% max/min spread; raw verification passes.
All sources remain overlays.

| Kernel | Wide-08 / wide-06 time | Wide-08 / wide-07 time | Wide-08 / PR #780 time |
| --- | ---: | ---: | ---: |
| ATAX | 0.966459 | 0.977365 | 0.984550 |
| GEMM | 0.910238 | 0.934381 | 1.087029 |
| Jacobi 1D | 0.935367 | 0.934985 | 1.062878 |
| Jacobi 2D | 0.920244 | 0.921775 | 1.085654 |
| SYMM | 1.000662 | 1.000818 | 0.939686 |
| matmul | 0.892892 | 0.895393 | 1.267596 |
| FDTD 2D | 0.981718 | 0.982030 | 0.793373 |
| Heat 3D | 0.951797 | 0.948836 | 1.067184 |

Reduced FPR preservation accounts for most of the improvement. These are
focused kernel results; the latest full-corpus result remains the wide-05
combined candidate's 2.22% longer time than PR #780.


### Integer register preservation: native-wide-screen-05

Wide-09 also limits GPR snapshots to scratch clobbers, allocated stream pointers,
and register inputs. Untouched registers stay in place; success and fallback
restore the same snapshot mask. All five native differential fixtures, 21
semantic cases, scoped native packages, eight corpus results, and ten-module
full-memory checks pass. Six paired 300 ms rounds have no spreads over 20%.

| Kernel | Wide-09 / wide-08 time | Wide-09 / PR #780 time |
| --- | ---: | ---: |
| ATAX | 1.007088 | 0.974632 |
| GEMM | 0.964315 | 1.048157 |
| Jacobi 1D | 0.991616 | 1.056713 |
| Jacobi 2D | 0.978814 | 1.054038 |
| SYMM | 0.998934 | 0.939704 |
| matmul | 0.938661 | 1.133970 |
| FDTD 2D | 0.993534 | 0.790016 |
| Heat 3D | 0.989150 | 1.054682 |

The combined-02 overlay merges these changes with the previously validated
runtime, scalar recurrence, and older two-lane fallback. Local AMD64 compiler
and ARM64 compiler/runtime checks pass. Native compiler, focused runtime, all
five wide fixtures, all five recurrence fixtures, 21 semantic cases, and
ten-module full-memory checks also pass. The full-corpus paired run is complete, with details below.
Production Go sources remain at v77.


### Combined full comparison: native-wide-combined-full-02

The combined runtime, recurrence, vector fallback, and native-wide-09 candidate
passes all 65 module result gates and covers all 72 exports in four paired
300 ms rounds. The module-balanced candidate / PR #780 time ratio is
**1.0061183382716679**, corresponding to **0.9939188681500647 × PR throughput**.
Candidate Doitgen has a 1.376392 max/min spread; PR `tiny.add` has 1.410125.
All other exports remain within 20%. No samples were discarded or replaced.

The verifier checks all raw samples, paired ratios, aggregation, source archives,
scoped native gates, five wide and five recurrence fixtures, and full-memory
checks. Independent local source reconstruction matches effective Go-source
SHA-256 `c1132a9e75ae7386d733851ee991d435f1ba3e9904c2f2bf37e4d0e46d0a1cfe`.
Production v77 source hash remains unchanged.

The observed full-corpus time is 0.61% longer than PR, with the noise qualifications
above. The target remains far unmet. The independently reproduced baseline
runtime cancellation hang still qualifies full native runtime coverage.
Experimental transforms remain overlays; no commit or push has been made.


### Spill input probe: native-wide-10 / native-wide-screen-06

Wide-10 skips canonical GPR restoration before reading spill-only inputs. All
scoped native gates, five differential fixtures, 21 semantics, eight corpus
results, and ten-module full-memory checks pass. Local AMD64 compiler checks
also pass using cached Go 1.22.2, after Homebrew Go binaries became unavailable.
The exact local Go 1.26.5 checks recorded for earlier versions remain historical.

Generated-code comparisons under the same Go 1.22.2 toolchain show seven of the
eight affected corpus functions byte-identical to wide-09. SYMM is 224 bytes
smaller. Six paired 300 ms rounds nevertheless give SYMM time / wide-09 =
**1.0004057291**; no measurable speedup is established. All variant spreads stay
within 20%. The other ratios range from 0.997925 to 1.008619, consistent with
small timing variation in unchanged code. This follow-up is not included in the
combined candidate or the latest full-corpus result.


### Toolchain audit and corrected baseline

Inspecting raw PR CPU profiles exposed Go 1.27.1 runtime locations despite the
recorded Go 1.22.2 shell version. `go version -m` confirms `pr780-baseline-01`
was built with Go 1.27.1. Every comparison using that binary, including both
native-wide combined full runs, mixes toolchains. Candidate-to-candidate focused
comparisons remain Go 1.22.2 comparisons; their PR ratios carry this qualification.

The pinned PR source digest and benchmark wrapper digest are unchanged in
`pr780-baseline-02`. Only toolchain selection changes. The build explicitly pins
Go 1.22.2, checks embedded binary metadata, and passes the semantic corpus.
No earlier samples are rewritten, discarded, or silently replaced.


### Recursive integer allocation: recursive-gpr-density-screen-01

The candidate applies squared-use-density costs to GPR intervals in large
recursive AMD64 functions, while preserving the existing conservative scalar-FP
policy and regional admission rules. Native compiler/focused-runtime, five wide
fixtures, five recurrence fixtures, 21 semantics, and full-memory gates pass.

A six-round 300 ms screen covers 13 modules / 14 exports. Embedded metadata
verifies all three binaries use Go 1.22.2. Kissfft time / combined-02 is
**0.7498748825**; time / pinned PR #780 is **0.9618194321**. Other candidate/control
ratios range from 0.991331 to 1.002896. No variant has a max/min spread over 20%.
Raw coverage, paired ratios, and toolchains are independently verified.
A corrected full-corpus comparison is pending; the 50% goal remains unmet.


### Corrected complete corpus: matched-full-01

Both the recursive GPR-density candidate and pinned PR #780 are verified from
embedded metadata as Go 1.22.2 binaries. Both pass every 65-module result gate,
covering 72 exports. Four paired 300 ms rounds yield module-balanced time / PR
**1.0025388651576999** and throughput / PR **0.9974675643549236**. Candidate
Doitgen has max/min spread **1.3870775430**; PR has no export over 20% spread.
Every raw sample is retained. This does not establish stable parity or the goal.

Independent verification covers both variants' result gates, raw samples,
paired medians, aggregation, source archives, semantic/recurrence/full-memory
gates, and embedded toolchains. Local reconstruction matches effective source
SHA-256 `0bdfb0c03e01601c95171f3825ed97f31dc5181fb1314cf0bed2849071b5c8a0`.
Local ARM64 compiler/runtime suites also pass using Go 1.22.2. Production Go
sources remain v77 and unmodified by these experiments. The baseline native
runtime cancellation failure remains a separate unresolved qualification.


### Guarded integer sum prototype: integer-sum-02

An exact top-tested i32 countdown loop with `acc += i64.load(addr)`, eight-byte
address increments, and no other effects is rewritten to two i64x2 accumulators.
A widened whole-range guard proves all vector accesses valid and rules out
memory32 wrap. Shared and memory64 memories are excluded. A single copy of the
original scalar block handles both failed guards and the 0–3 element remainder.
Integer regrouping preserves modulo-2^64 addition. Loop exit locals are retained.

Prototype 01 failed native metadata admission because remapped source offsets
moved backward. Prototype 02 maps its guarded prefix to the original block start
and places the scalar block after that prefix. The regression now runs the
native planner, and the original runtime failure passes.

Native compiler/focused runtime, prior wide/recurrence fixtures, 21 semantic
cases, selected corpus outputs, and ten-module full-memory checks pass. Four
integer fixtures cover offsets 0, 7, 65528, and 4294967295: **5,280 cases in each
of two bounds modes**, with an independent exact modular-sum oracle, exit-local
checks, traps, and unchanged full memory. Fixtures at offsets 0 and 7 exercise
444 and 428 vector-eligible inputs; the large-offset cases exercise fallback.
The focused matched-toolchain timing screen completes six paired 300 ms rounds.
Memory.sum time / previous candidate is **0.6374045982**, and time / PR #780 is
**0.7040964778**. No variant has a spread over 20%. The corpus admission check
finds only `memory/1` transformed. Local ARM64 compiler/runtime checks also pass.
This prototype is not part of matched-full-01. Its completed `matched-full-02`
run is recorded below. All changes remain overlays.


### Matched full corpus after integer reduction: matched-full-02

Both Go 1.22.2 binaries pass all 65-module / 72-export result gates. Four paired
300 ms rounds give module-balanced time / PR **0.9931789314157942** and
throughput / PR **1.0068679151041617**. Neither variant has any export with
max/min spread above 1.20. The 50% throughput goal remains unmet.

Independent verification reconstructs raw samples, paired medians, aggregation,
spreads, archived source hashes, embedded toolchains, and scoped correctness
gates. Local reconstruction matches effective Go-source SHA-256
`bf21b8ae1677d8253f5db54383664f174b4db800479423d10da96ab3478174b3`.
Production remains v77. The full native runtime cancellation hang is still
unresolved; focused runtime and ARM64 compiler/runtime suites pass.


### Interrupt delivery lifetime: cancel-delivery-01/02

The unchanged v77 control again times out in managed-table reentry cleanup. A
diagnostic retains the authenticated request across a brief scheduler sleep
when the Gosched polling loop has no acknowledgement. The v77-based diagnostic
passes 100 repeats and both full native runtime suites. Composed with
integer-sum-02, the formatted candidate passes 1,000 repeats, ten rounds of
cancellation tests, full native compiler/runtime suites, and ten rounds of
cancellation tests with eight CPUs. Source hashes and terminal logs verify.

This supports a delivery-lifetime hypothesis, without directly instrumenting
the presumed ordering. Production and measured performance sources are unchanged.
The combined validation source hash is
`30676d92127eb95226e5a5c4f51213870e3084dec8af8c7c7b7134e634329e00`.


### Giant-function regional reuse: fast-regional-01/02

The first probe keeps giant-function linear allocation and adds the existing
verified regional reuse planner. Unsafe cyclic calls with more than three
parameters remain excluded. Native compiler/public-runtime, 21 semantic cases,
all prior fixtures, selected corpus outputs, full-memory checks, and local
ARM64 compiler/runtime suites pass.

Six paired 300 ms rounds with Go 1.22.2 measure YYJSON at **0.9725682503 ×
integer-sum-02 time** and PCRE2 at **0.9945161236 ×**. Zstd measures 1.0241876998,
but compiler metrics show unchanged regional allocation there; the difference
is not attributed to this transform. No timing series exceeds 20% spread.
Static metrics identify 4,101 new fragments in PCRE2 function 60 and 1,535 in
YYJSON function 18. Their code sizes grow by 3,230 and 65 bytes, respectively.

Revision 02 excludes fragments confined to a single instruction position. Its
native/ARM64 gates pass, but the four-variant screen loses the YYJSON gain
(time / revision 01 = 1.0277392835). PCRE2 is noisy at 1.342 max/min. This filter
is rejected. Revision 01 repeats YYJSON / integer-sum-02 = 0.9738404772 and
PCRE2 = 0.9911403211, with no spread above 20% in its samples. Neither revision
has a new full-corpus performance result. The current full result remains matched-full-02 and the
50% target is unmet. Both probes remain isolated overlays.


Generated-code identity checks confirm LZ4, zlib, Zstd, and Monocypher are
byte-identical across both regional probes and integer-sum-02. Only PCRE2 and
YYJSON change among the six inspected modules. Remote effective source hashes
match independent local reconstruction. `matched-full-03` is now running all
65 modules / 72 exports for fast-regional-01 against the pinned Go 1.22.2 PR
baseline, with fresh result gates for both variants before timing.


### Regional candidate full corpus: matched-full-03

Both Go 1.22.2 binaries pass all 65-module / 72-export result gates. Four paired
300 ms rounds give module-balanced time / PR **0.9912150619239573**, or
**1.0088627972006308 × PR throughput**. Candidate Doitgen is noisy at max/min
1.3803029267; no PR sample series exceeds 20% spread. This remains a qualified
result and the 50% throughput target is unmet.

Raw samples, paired medians, aggregation, archived sources, embedded toolchains,
and correctness logs verify independently. Local effective source reconstruction
matches `a93b68b29532ebd0ea5a44f59a8a61dc40645a6fbbcb1f3802eb23c52e9c0c81`.
The candidate includes the validated interrupt-delivery overlay and passes the
full public runtime suite; core runtime was validated in cancel-delivery-02.
Production remains v77. A multi-destination native SIMD prototype is now in
correctness validation and is not part of this measurement.


### Multi-destination native SIMD: multi-store-01

Native wide loops now support up to four destination streams with pairwise
whole-range alias guards, original event order, and scalar fallback/remainders.
Two fixtures each pass 1,728 independent-oracle cases in both bounds modes and
Railshot; native compiler/full public runtime, all 21 semantics, prior fixtures,
and twelve-module full-memory checks pass. ARM64 compiler/runtime checks pass.
Path instrumentation exercises successful and fallback entries for two and four
stores, including the twelve-stream scratch limit. Correlation executes the new
path; ADI does not enter it.

Six paired 300 ms matched-toolchain rounds give correlation time / fast-regional-01
**0.9848710944**, with controls essentially unchanged and no spread above 20%.
This is a focused 1.51% improvement, not a new full-corpus result. The transform
remains isolated. Native profiles of matrix multiplication, GEMM, correlation,
and Doitgen are archived in native-profile-04. Both engines already emit packed
AVX2 arithmetic in the matrix hot loops. Profiles identify redundant Dragline
entry snapshots as an optimization candidate; instrumented timings are not
acceptance evidence.


### Direct SIMD input snapshots: revisions 01 and 02

Revision 01 snapshots register inputs before scratch reads, removing a redundant
backup reload and input copy. Revision 02 also removes backup/restore pairs for
input registers that the bridge never changes. A new dynamic floating-input
fixture and encoding regressions cover mixed homes and untouched register inputs.
Both pass native compiler/full public runtime, semantic and differential fixtures,
twelve-module full-memory gates, and local ARM64 compiler/runtime checks.
Remote effective source hashes match independent local reconstruction.

Two focused screens use six paired 300 ms rounds, ten selected modules/exports,
and Go 1.22.2 for every binary. Revision 01 matmul time / multi-store01 is
0.9888914909. In the second screen, revision 02 time / revision 01 is 0.9827490215
for matmul and 0.9826588815 for GEMM, while ATAX and Heat regress about 2%.
No timing series exceeds 20% max/min spread. These are small focused changes;
the latest aggregate remains matched-full-03 and the 50% target is unmet.
All revisions remain isolated overlays. Revision 03 also removes backup/restore pairs for reserved GPR emission scratch.
It passes the same native and ARM64 gates, and local/remote effective source
hashes match. Its focused screen gives matmul / multi-store01 = 0.9481751465
and GEMM / multi-store01 = 0.9698700857, while correlation is 1.0092339146.
No timing series exceeds 20% spread. Full-corpus evaluation completed as
matched-full-04: time / PR 0.9876689610, throughput / PR 1.0124849919, with
PR-side noise in Doitgen and Zstd decompression. The 50% target remains unmet.


### Reserved FPR backups and Darwin interrupt authentication

Snapshot04 filters floating-register backups through physical allocator homes,
using the same rule at setup and restore while preserving the full clobber mask.
Native AMD64 gates pass. Six paired 300 ms rounds give time / snapshot03 of
0.9980941442 for matmul, 0.9925621218 for GEMM, and 0.9787858633 for Heat.
ATAX, correlation and SYMM are slightly slower. No series exceeds 20% spread.
Eight paired 500 ms replication rounds give GEMM / snapshot03 = 0.9861710529
and Heat / snapshot03 = 0.9776926432, with no series above 20% spread.
Snapshot04 has no full-corpus result.

Local ARM64 testing exposed a crash in Darwin interruption while reading a
header through X26=15. Snapshot03 reproduces the same failure. Registry01 checks
that X26 is a registered JobMemory base and serializes header reads with
unregister-before-unmap. Classic unit tests, 100 stress repetitions and full
ARM64 suites pass. A 1,000-repeat run exceeds its three-minute time limit while
starting a fresh test case; it is incomplete, not a completed stress pass.

Registry02 also registers guarded Darwin mappings. The new lifecycle regression
fails before that addition; full guarded ARM64 compiler/core-runtime/public-runtime
suites pass afterward. The 1,000-repeat classic run completes in 343.942 seconds, and 100 guarded
repetitions also pass. Both full ARM64 configurations and source/log checks pass.
All sources and failures remain archived; production v77 is unchanged.


### Wider native profiles and bounded unsigned division

Native-profile-05 captures PCRE2, LZ4, YYJSON and Monocypher for snapshot04 and
PR780 with matched Go1.22.2 diagnostic binaries. Native sample mapping and image
hashes verify. LZ4's compression setup has concentrated samples in full-width
unsigned remainder sequences immediately after eight-/sixteen-bit masks.
PR780 already uses a shorter bounded reciprocal sequence there.

Bounded-div01 applies the same arithmetic proof after verifying a Dragline SSA
AND definition and constant mask. Unsupported bounds/divisors retain the existing
lowering. Local compiler, exhaustive reciprocal and encoding checks pass; native
compiler/full public runtime, semantics, differential fixtures and full-memory
gates pass. The public runtime regression covers 1,966,080 independent-oracle
invocations across Dragline and Railshot. A six-round paired screen reduces
LZ4 compression time 12.39% versus snapshot04 while decompression rises 2.83%.
LZ4 still trails PR780. Candidate Doitgen is noisy (max/min 1.41798).
Full-corpus evaluation completed as matched-full-05 at a qualified 1.55%
throughput gain over PR780; its PR UTF SIMD series is noisy. The candidate
remains an overlay based on snapshot04; ARM64 checks compose the independently
validated Darwin registry02 fix.


### Generic arithmetic immediate recognition

The arithmetic-immediate helper matched selected AMD64 opcodes even though
planning invokes it before final target selection. A regression with generic
operations and no selected combinations fails before the correction and passes
after switching to SemanticOpcode classification. Local compiler/encoder and
ARM64 suites (with Darwin registry02) pass; native validation is underway.

A local compiler census increases immediate uses from 1,112 to 3,801 in YYJSON
function18 and 1,901 to 6,009 in PCRE2 function60. This demonstrates coverage of
the intended plans, not speed. The isolated generic-immediate01 candidate is
based on bounded-div01. Allocation policy and arithmetic encodability limits
are unchanged.


## Generic arithmetic immediates: matched full06

Generic-immediate01 passes all native and ARM64 correctness gates (ARM64 uses
the separate Darwin registry02 fix). Matched-full-06 measures 2.68% higher
throughput, qualified by PR QOI noise. See the run archive for raw samples and
independent verification. The prior bounded-div01 full05 result was 1.55%.

## Simplifier fuel investigation

Both profiled giant functions exhaust the default 4096 rewrite budget before
common-expression elimination. An isolated scaled-fuel01 candidate gives AMD64
four rewrites per instruction, with a 4096 floor and 65536 ceiling. ARM64 retains
the original configuration. In the compiler census, YYJSON function18 consumes
15359 rewrites and PCRE2 function60 consumes 24574, allowing repeated FNV
constants to merge. Local compiler checks, guarded ARM64 suites, and native compiler/public runtime,
semantic, fixture and full-memory gates pass. Matched compile/execution costs
remain under evaluation.


Scaled-fuel01 passes all native correctness gates. Its six-round focused screen
measures time / generic-immediate01 of 0.9291656511 for PCRE2, 0.9732491791 for
YYJSON and 0.9579137723 for zstd. LZ4 decompression is 1.0105734131. Doitgen is
noisy in both Dragline variants. Compile cost is still under measurement; this
screen does not update the full-corpus aggregate.


The four-round scaled-fuel compile-cost comparison completes without a timing
series above 20% spread. PCRE2 time / previous is 0.9621081672 and zstd is
0.9696768569; other selected compile times are nearly unchanged. Most whole-process
RSS medians rise 2.7–6.1%, while PCRE2 falls 1.5%. RSS includes untimed setup and
default-engine code sizing. Timed allocation bytes stay within 0.36% of previous.
The full execution comparison is running as matched-full-07, followed by native
profiles of recursion, many-function dispatch, UTF SIMD, and BLAKE3.


Matched-full-07 completes at time / PR 0.9731969039, or 2.75% higher throughput,
with PR QOI/Doitgen noise and complete 65-module / 72-export result gates.
The next isolated native-wide51201 prototype doubles independent f64 lanes
from four to eight on AVX512F targets. It preserves per-lane arithmetic order,
all range/alias guards, scalar tails and the AVX2 fallback. The completed trip
counter moves beyond the 64-byte final-lane scratch. Local compiler/encoding
and guarded ARM64 suites pass; native execution validation is pending.


Native-wide51201 passes native compiler/full public runtime, all prior oracles,
twelve-module full-memory checks, and six extra fixtures covering every tail
length (2160 cases per fixture in both bounds modes, with AVX512 required).
The eight-module matched screen has no spread above 20%, but results are mixed:
matmul time / scaled-fuel01 is 0.9400383256, GEMM 1.0454219948, Heat 1.0391347477,
and Jacobi1D 1.1024923855. It remains experimental. Instrumented trip-count
collection is investigating whether extra scalar tails explain the regressions.


Trip diagnostics confirm GEMM has 35 paired iterations and Jacobi1D has 59;
both leave six scalar lanes after an eight-lane loop, versus two after AVX2.
Matmul has 32 paired iterations and no remainder. Heat has 18 ordinary
iterations and the same two-scalar tail at either width, so its regression
needs a separate explanation. All diagnostic fixtures exercise both success
and fallback paths while retaining exact oracle results.

Native-wide51202 adds an existing four-lane body after the eight-lane body when
needed, and handles initially short four-to-seven-lane regions directly.
It preserves stream starts and exact loop-carried exit locals across the width
transition. Local compiler checks pass; native and ARM64 validation are running.


Revision02's native differential gate catches incorrect memory for the short
invariant-load fixture (src=0, dst=0, n=4). Its local, ARM64 and native public
runtime suites had passed; those were insufficient to qualify the transform.
The cause is stale compile-time stream register assignments from the main body:
the short runtime path bypasses their initialization. Revision03 resets the
remainder stream descriptors so early invariant loads use stack bases. The
failing revision is archived, and the corrected revision is under validation.


Mixed-width revision03 passes all native and guarded ARM64 correctness checks,
but its matched screen regresses GEMM and Jacobi1D further, with no series above
20% spread. It is not promoted. Revision04 instead chooses eight lanes only
for complete groups, and otherwise runs the existing four-lane body for the
whole loop. This avoids copying loop state between vector widths. Local
compiler checks pass; native validation is running.


Revision04 passes all native and guarded ARM64 gates. Its matched eight-module
screen measures matmul time / AVX2 0.9327786005, GEMM 1.0201225290, Jacobi1D
1.0047476185, Heat 1.0012101277, and SYMM 1.0265868185. The mixed-remainder
regressions are mostly removed, but some smaller regressions remain. Full-corpus
measurement is active as matched-full-08, with an expanded runner/verifier that
also pins and checks the six new AVX512 tail fixtures and their oracle source.
The latest complete aggregate remains full07, 2.75% higher throughput with
PR-side noise. No prototype has been promoted to production v77.


## Canonical immediate selection: completed focused screen

Canonical-immediate01 passes full native correctness, all 65-module / 72-export
result gates, and 7,056 independent literal-oracle calls. Its six-round 14-module
time ratio versus retained strided-wide02 is 1.0014033950, effectively tied.
NanoSVG improves 3.62%, while drwav regresses 3.40% and PCRE2 1.54%. No series
exceeds 20% spread. The runner's rotation/reversal cancelled for two variants,
so candidate always ran first; small differences are subject to order bias.
The prototype is unretained. See canonical-immediate-screen-01 for raw samples,
source/binary pins, order audit and independent reconstruction. No native job
remains active at this checkpoint. Production v77 and retained strided-wide02
are unchanged, and the 50% target remains unmet.


## Larger leaf-body admission experiment

A [call admission census](leaf-admission-census-01/README.md) finds only five
additional call-chain leaves (13 static sites, three modules), and no additional
SIMD leaves under the existing 384-byte size limit. Raising only the scalar body
limit to 1,536 bytes admits 60 more callees at 312 static call sites across 43
modules. These are static opportunities, not measured dynamic savings.

[Large-leaf01](large-leaf-01/README.md) tests that size limit while preserving
the caller growth budget and other restrictions. All native correctness gates,
65/72 results, 7,056 independent arithmetic calls and fresh ARM suites pass.
The static census changes 41 modules and grows total native code 16.28%.
The completed native 18-module screen measures 0.9742154224 times retained
execution time (2.58% lower), with verified alternating pair order. Monocypher
improves 26.51% and zlib 13.43%; baseline yyjson is noisy. A full six-round
65/72 comparison against retained and pinned PR780 is running as large-leaf-full-01.
The retained candidate remains strided-wide02, and the 50% goal is unmet.

The lexical-loop follow-up (leaf-hot-sites-census-01) finds 49 newly eligible
large leaves with 148 loop-nested static call sites across 39 modules, including
common utility/setup routines. A loop-site restriction alone therefore cannot
be assumed to remove all cold expansion. This probe does not measure hotness.


Large-leaf01's full six-round 65-module / 72-export comparison is complete and
independently reconstructed: time / retained is 1.0012344501, effectively tied,
and time / PR780 is 0.9738075304. Monocypher improves 26.71% and zlib 13.04%,
but expanded Nussinov regresses 28.00%, Floyd-Warshall 8.12%, and SYMM 6.32%.
Tiny also moves 6.68% despite identical code; noise is recorded in the full
archive and timing shifts are not all attributable to the transform. Revision01
is unretained. Its compilation-cost run follows the execution run on CPU0.

A branch-table census finds 32 newly admitted callees at 209 sites in 31 modules
above a 32-entry aggregate budget. Revision02 applies that budget only to bodies
above 384 bytes. Local AMD64 and guarded ARM tests pass; 4,096 independent ARM
branch-arithmetic calls cover both sides of the 32/33-entry boundary. All 65
modules compile: revision02 changes 13 versus retained, reduces added native
bytes from 320,162 to 129,627, and keeps monocypher/zlib code byte-identical to
revision01. Native AMD64 correctness and timing are pending. Source and evidence
are archived under large-leaf-02, leaf-control-census-01 and large-leaf-census-02.

Revision01 compilation costs are complete and verified, with no series above
20% spread: sampled numerical modules take 5.11–6.24× compile time and
2.30–2.60× allocation bytes; zlib nearly doubles and monocypher rises 34.79%.
Revision02 now passes all native compiler/public runtime, semantic and 65/72
result gates, inherited independent oracles, 7,056 larger-body arithmetic calls
and 4,096 branch-table calls. Its 17-module native screen is running with six
balanced permutations of revision02/revision01/retained. Production remains v77,
retained remains strided-wide02, and the 50% throughput target remains unmet.


The completed large-leaf02 focused screen measures 0.9744161418 times retained
time and 0.9819430632 times revision01 time across 17 modules, with no >20%
spread. Monocypher improves 26.62% and zlib 12.62%; Nussinov/Floyd-Warshall
return near retained time. NanoSVG and PCRE2 regress 3.83% / 2.20%. The full
six-round 65/72 comparison is active as large-leaf-full-02.

A separate allocator census on retained strided-wide02 records 88 visits and
11 machine fingerprints across six modules for call-free all-integer AMD64
loop functions of 128–239 instructions. All 65 emitted hashes remain unchanged
in the logging-only probe. Medium-density01 tests squared-use GPR cost for that
class without changing regional admission or call rules, and without the
larger-leaf changes. Local AMD64/ARM suites pass; 4,032 ARM independent modular
recurrence calls pass, and both fixtures compile to 164 AMD64 instructions.
Five corpus modules change code. Weighted spill-debt units also change, so
old/new score reductions cannot prove fewer spills or better execution. Native
AMD64 validation and timing are pending. Archives: medium-density-census-01,
medium-density-01 and medium-density-code-census-01. No production promotion.


Large-leaf02's complete six-round 65-module / 72-export comparison is now
verified, including all six executed permutations, paired medians, raw rows,
corpus and binary pins. Time / retained is 0.9934198102 (0.658% less time),
and time / PR780 is 0.9658279986 (3.54% higher throughput). The 13 changed
modules average 0.9672360704; 52 unchanged-code modules average 1.0000757562.
Monocypher uses 26.27% less time, zlib 13.14%, and UTF SIMD 3.50%. NanoSVG
regresses 3.28%, PCRE2 1.75%, and CoreMark 2.32%. Candidate and retained have
no series above 20% spread; PR780 QOI and zstd series do, limiting the absolute
PR comparison. No samples were discarded. See large-leaf-full-02.
Compilation costs are running before retention is decided. Production v77 and
retained strided-wide02 remain unchanged; the 50% throughput target is unmet.
The medium-density prototype's native qualification follows compilation costs
on CPU0; its static allocator metrics are not execution-performance evidence.


The smaller-loop diagnostic is complete in small-density-census-01: 107 raw
allocator visits, 17 module/fingerprint entries (16 globally distinct structures) across six modules (json-as,
json-as-simd, yyjson, libtommath, NanoSVG and PCRE2), for call-free all-integer
loops with 64–127 machine instructions. All 65 complete native hashes and input
hashes match strided-wide02, proving the logging probe changes no emitted code.
These counts indicate static coverage, not hotness or runtime gain. The current
128–239-instruction density prototype's native focused screen is still running.


Mixed scalar-FP GPR-density coverage is now documented in
mixed-gpr-density-census-01. The logging-only strided-wide02 probe keeps all
65 native code hashes unchanged. It counts 139 allocator visits and 32 module/
fingerprint pairs but only five globally distinct structures: Matmul's 205-
instruction loop, NanoSVG's 201-instruction loop, and three structural variants
of the shared 143-instruction utility across thirty PolyBench modules. The
apparent breadth mostly repeats utility code; there is no new optimization or
runtime gain from this probe. The small-density census wording is also corrected:
17 module/fingerprint entries represent 16 globally distinct structures.


The [direct-wrapper prototype](direct-wrapper-01/README.md) now retains nested
scalar direct calls while inlining one small wrapper. Its local census changes
11modules (+76,439bytes);130 control images exactly match their qualified parents.
Four local existing-oracle runs pass160,000calls/87,760traps each, and residual
call/source-map unit checks pass. Dedicated effects/reentry tests and native
qualification remain required; no timing or retention claim.

### Direct-wrapper02: native qualification completed

The new effects/reentry gates exposed two pre-existing AMD64 structured-emitter
failures: a destination/right-operand alias and FP locals corrupted across a
call. [Diagnostic evidence](integer-alias-01/README.md) retains both failures and
their minimization. [Direct-wrapper02](direct-wrapper-02/README.md) fixes both
in its overlay and passes all native gates under candidate/tree/retained
policies: 29,744 V8 integer checks, 2,304 reentry calls, the existing 160,000-call
oracle, 65 modules / 72 exports, 21 semantic cases, and compiler/runtime suites.
Local and native archive verifiers pass. All 195 corpus code images match the
corresponding direct-wrapper01 images. Timing remains separate from correctness;
the 50% throughput target versus PR780 is still unmet.


Direct-wrapper02 screen66099 and compile5858 completed0 on quiet CPU7 after a
second probe found mean2.07%/max3.96% background activity. All234 screen runs /
306 export samples and156 compile runs verify. Candidate/leaf changed11 time
ratio0.988117914; unchanged controls0.999924097. No>20% series spread in either
experiment. JSON/JSON-SIMD execution ratios0.939336/0.937589 accompany compile
ratios1.282251/1.253129; LibTomMath compile1.398432 and Memory Tree2.902453
(about1.12ms extra). Broad wrapper02 is unretained; no full65/PR780 timing claim.
Correctness fixes are preserved in the qualified overlay and correctness-fixes.diff.
YYJSON is a narrower follow-up lead: execution0.962566, compile0.999184, without
a module-specific admission rule. No native job remains running. Goal50% stays
active and unmet; production Go sources are unchanged, with no commit or push.


## Quotient coverage and guarded word-copy follow-up

The guarded unsigned quotient-to-F64 prototype is locally correct but changes
none of the 65 corpus code images. It is unretained for lack of target-corpus
coverage; no native timing claim is made. The archive retains the failed
admission test before implementation, 24 successful AVX2/AVX512 emissions,
one million guarded arithmetic cases, and three 211,680-call execution oracles.
See vector-quotient-01.

A separate logging-only word-copy census preserves all 65 code hashes. Of 108
body-shape observations, two plans in Monocypher admit ordered four-word I64
copies. Word-copy01 extends the existing guarded AVX2 copy path to 16-, 32-,
and 64-bit elements. Its census changes only Monocypher (+1,120 native bytes).
All 65 retained images match divisibility02. Eighteen fixtures pass 36 forced
emissions; independent whole-word load/store oracles pass 30,456 calls per
policy on local ARM64 candidate and Rosetta AMD64 candidate/retained.
Native CPU7 qualification is running as session96990; its two word-copy
oracles, unit checks, and compiler suite have passed. Runtime and full corpus
checks remain pending. Unrelated host builds preclude a timing claim at this
checkpoint. Archives: word-copy-census-01 and word-copy-01. Production Go
source remains unchanged; no commit or push; the 50% target remains unmet.


Word-copy01 native session96990 completed with exit0: two 30,456-call memory
oracles, 36 forced emissions, compiler/runtime suites, 65 modules / 72 exports
per policy, and 21 semantic cases per policy pass. Local and native archive
verifiers pass. No timing was taken on the busy host; the prototype remains
unretained. The older native-profile05 shows only one of 705 Monocypher native
samples in function5, a historical prioritization clue rather than a current
performance claim. Both quotient01 and word-copy01 evidence are preserved.
No native job remains running. The 50% target is active and unmet.


## Constant vector-loop setup: broad qualification completed

The earlier immutable-local constant probe missed loop-entry values carried by
SSA edges. A new logging-only census preserves all65 input/code images and
finds94 of96 distinct regions across25 modules with known I32 entry inputs;
79 have known limits and counters. Raw192 visits and the first failed harness
compilation are preserved in wide-constant-census-02.

Wide-setup01 resolves those incoming values with bounded constant/phi analysis,
folds affine arithmetic only during setup, and replaces trip guards only when
their exact modulo-32 conditions are proven true. Body and exit arithmetic
continue using updated snapshots; memory/alias/conversion guards remain.
The census changes25 modules, removes5712 native bytes, and keeps all65 retained
images identical to divisibility02. It includes neither word-copy nor quotient
experiments. Production Go is unchanged.

Local tests pass90000 arithmetic cases,1024 trip-guard cases,edge/phi/cycle/type/
budget rejection checks,66 forced emissions and55836 independent byte-memory
calls per policy. Native sessions41801 and9342 complete0: both policies pass
65 modules/72 exports,21 semantic cases,55836 memory calls,compiler/runtime
suites,and20 FP fixtures with2160 cases each in2 Dragline bounds modes against
explicit Railshot. Fourteen static-trip FP fixtures also pass local Rosetta.
Source/code/input/raw-output/pin verifiers pass. No timing or compilation-cost
measurement exists yet; no retention or 50% claim. No native job is running.
Archive: wide-setup-01.


## Wide-setup measurement blocker

The qualified wide-setup01 source and matched Go1.22.2 candidate/retained/PR780
binaries pass measurement preflight. Both ten-second host activity gates fail;
native sessions50293 and72762 exit75 with zero benchmark measurements. The
first probe records CPU7 mean26.285%,peak59.596%,and5.471 busy background cores.
Evidence and independent gate verifiers are archived in wide-setup-screen-01
and wide-setup-full-preflight-01. A complete pinned runner is saved as
wide-setup-01/measure.py for focused execution,all65 compilation cost,and full
65-module/72-export comparison. Native contention has persisted across three
consecutive goal turns. No native job is running; a quiet host is required to
choose retention and substantiate progress toward50%. Production Go remains
unchanged; no commit,push,retention or performance claim.
