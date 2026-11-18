# Experimental streaming residency

Checkpoint b05bb49a adds an optional shared-backing-cell layer to the experimental
streaming engine. Enable `WAGO_EXPERIMENTAL_STREAM_RESIDENCY=1` alongside the
streaming/register-home options. This first stage retains two selected homes;
it is not SSA, does not scan the historical value arena, and admits no new
language features.

Each borrowed value node and home-map entry holds a cell reference. A cell owns
one authoritative register or immutable spill slot. Pressure evacuates the cell
before reusing its register; materialization re-reads the backing after allocation.
Canonical transfers preserve old generations and restore fixed home agreements.
Direct sinks require both node and cell uniqueness. Raw-register branch proofs
are disabled in this mode.

The combined ownership-checker/statistics gate passed 50 focused top-level tests
in 0.094 seconds without skips or failures, including 12 residency tests. These
cover pressure evacuation, detached aliases, single slot release/reuse, allocator
rereads, two-home swaps, sink alias rejection, dead-path restoration, and result
register exclusion. Production 145-workload runtime validation and actual RSS
measurements remain pending. No timing or performance improvement is claimed.

The predecessor's affected-kernel screens did not establish an acceptable
execution regression or compilation gain; its latest controls failed agreement
checks. Those historical results are retained in
[the streaming experiment log](../streaming-operands/DESIGN.md). This checkpoint
is source backup for further experimentation, not a PR or merge recommendation.

## Prior two-home runtime/RSS diagnostic (b05bb49a)

A matched single-trial memory run passed 435 runtime oracles: 145 each for main,
candidate residency OFF and candidate ON. It reused one fixed executable slot.
Mean absolute compiled-barrier VmHWM was 11.986934 / 11.996794 / 11.996471 MiB
respectively. Mean anonymous RSS was 1.581627 / 1.578691 / 1.578637 MiB, and
file-backed RSS 10.403960 / 10.416649 / 10.416649 MiB. Candidate OFF retained
the earlier streaming, home, sink and lifetime options.

No RSS gain or latency result is established; the absolute <5 MiB target is
unmet. Different file-backed baselines preclude attributing differences from
earlier historical runs to compiler source changes.

## Wider-home checkpoint (b119ad62)

`WAGO_EXPERIMENTAL_STREAM_WIDE_HOMES=1` selects scored homes from the existing
allowed GP pool, bounded by eight slots and a four-register scratch floor.
Branches resolve retained value identities after canonicalization rather than
caching physical registers across it. Changed generations referenced only by
their map are discarded without a needless spill; surviving aliases retain
backing snapshots. Allocation victims must belong to the configured register
bank.

The frozen wider checkpoint passed 55 focused ownership/statistics tests with
zero skips or failures (0.126 seconds). An ordinary-entry fixture proved six
actual backed homes in both interruptibility modes with the expected result.
Additional cases cover direct branches, old snapshots, swaps, pressure and dead
generation handling. Production corpus/native validation and RSS measurements
for this wider checkpoint remain pending. The preceding b05 measurements do
not qualify this revision. No timing, speedup or target-success claim is made.

## Completed wider-home screen (b119ad62)

All 600 affected-screen records and 435 memory-profile runtime oracles verified.
Same-binary OFF retained the prior four streaming options but disabled residency
and wider homes. The timing screen did not establish acceptable performance:

- Steady execution, arithmetic mean of workload ON/main ratios: 1.55324
  (95% interval [1.29894, 2.33035]); ON/OFF: 1.01862
  ([0.93212, 1.30141]). Aggregate main A/A was 0.99461, but individual and
  unrelated controls failed, including Aho A/A 0.7895. Aho ON/main was 4.058
  at the point estimate, which is not a qualified precise slowdown.
- Arithmetic means of steady timings were 78,343.825 / 73,563.333 /
  125,093.725 ns for main A / main B / ON. These differ from the mean of
  per-workload ratios above.
- Compilation ON/main was 0.96459 ([0.70223, 1.15585]); main A/A 1.02313
  failed. First-call A/A 0.96479 also failed. No compilation gain is established.

The fixed-slot, 145-workload memory run measured mean absolute compiled-barrier
VmHWM of 11.985022 / 11.954580 / 11.956627 MiB for main / OFF / ON.
The absolute <5 MiB target remains unmet. Failed timing controls and confidence
intervals prevent claiming the execution limit or compilation target is met.
No target is qualified, and no PR or promotion is proposed.

## Retained-copy checkpoint (ce9a47ef)

The opt-in `WAGO_EXPERIMENTAL_STREAM_COPY_PROOF=1` retains an immutable
backing-generation reference when a borrowed value is copied into an owned
register. Spilling an unchanged copy can return to that backing without another
store. Destructive operations relinquish the proof before writing; the existing
dirty-i32 carrier bit remains separate. This adds no value-node width and keeps
spill-slot ownership with the shared backing cell.

The frozen checkpoint passed 61 focused ownership/statistics tests without
skips or failures (0.142 seconds). All 145 production runtime oracles also passed with Go 1.27.1. Compilation,
execution-latency and RSS measurements remain pending for this revision. The preceding
negative wider-home results are historical, not qualification of this new
checkpoint. No gain, target success or merge recommendation is claimed.

### Completed retained-copy diagnostic (ce9a47ef)

All 600 timing-screen records and 435 RSS-profile runtime oracles verified.
Steady execution ON/main was 1.492718 (95% interval [1.327018, 1.906952]);
ON/same-binary proof-OFF was 1.030401 ([0.911682, 1.385978]). The main A/A
point ratio was 0.990510, but the unrelated control ratio 0.970862 failed
agreement. Compilation ON/main was 1.026430 ([0.800141, 1.462888]); its A/A
point ratio 1.012901 passed, but uncertainty remained wide. First-call A/A
1.043463 failed. These results do not establish a gain or qualify the execution
regression limit.

Mean absolute compiled-barrier VmHWM was 11.999353 / 11.974327 /
11.975512 MiB for main / proof-OFF / ON. No memory gain is established and
the <5 MiB target remains unmet. Source is unchanged; no PR or promotion
is proposed.

## Bounded pure-select checkpoint (fcaf7d20)

`WAGO_EXPERIMENTAL_STREAM_PURE_SELECT=1` restores a bounded conditional
evaluation strategy for unique, nontrapping integer ALU spines: at most twelve
operations with the existing cost/multiply admission threshold. Conditions,
inputs and scratch locations are prepared before the branch; no allocator,
spill or trapping operation occurs inside the conditional arm. Captured frame
operands may include unchanged local homes, which remain stable because the
arm performs no frame/local writes. Unsupported or shared forms remain eager.

The frozen checkpoint passed 66 focused ownership/statistics tests without
skips (0.114 seconds). All 145 production runtime oracles also passed. Performance qualification
remains pending for this revision. Prior negative evidence remains
applicable to its labeled checkpoints; no improvement or target success is
claimed for this new source checkpoint, and no PR is proposed.

### Completed pure-select diagnostic (fcaf7d20)

All 600 timing records and 435 memory-profile runtime oracles verified. On the
Aho workload, within-candidate steady ON/OFF was 0.20518 with exact paired
95% interval [0.10281, 0.26692]; ON/main was 0.98243
([0.72752, 1.24948]). First-call ON/OFF was 0.21480
([0.17657, 0.25857]); ON/main was 0.97543 ([0.77407, 0.99779]).

Eight actual guards restore 1,122 checksum multiplications rather than the
prior 245,760 for this input. This count is derived from input/control flow,
not a hardware instruction-counter measurement. It explains a workload-specific
recovery, not a general speedup.

Overall steady A/A 1.0575, compilation A/A 0.9535 and first-call A/A 1.0432
failed their agreement checks. No overall ≤5% execution qualification or
compilation improvement is established. Mean absolute compiled-barrier VmHWM
was 11.986773 / 11.966676 / 11.966433 MiB for main / pure-select OFF / ON,
essentially unchanged. The absolute memory target remains unmet. Source is
unchanged; no PR or promotion is proposed.

### Direct integer calls and globals checkpoint (f320c628)

Adds opt-in local direct calls with the existing integer register ABI (up to
eight arguments and one result), plus integer global reads and writes. Calls
canonicalize live values, evacuate register-backed generations, stage arguments
from frame slots, and capture results before global reloads. Global reads retain
snapshot semantics. Planned inline calls retain legacy lowering. Imports,
indirect calls, custom instructions and unsupported GC/EH paths remain excluded
from the new call path.

The exact frozen checkpoint passed 76 focused ownership/statistics top-level
tests with zero failures or skips (0.169 s), including call/global integration
and memory-growth coherence. The production adapter also passed all 145 frozen corpus runtime oracles.
Performance/memory qualification for this revision remains pending. Earlier
negative results above remain applicable to their stated revisions; this
checkpoint makes no improvement claim and is not proposed for promotion.

### Completed call/global diagnostic (f320c628)

All 900 timing records and 435 memory-profile runtime oracles verified. The
predeclared five-trial screen contains 15 distinct workloads: the original
cohort, a cost-ranked newly admitted top eight, and two zero-selected controls.
The original affected eight and new top eight overlap; they are not independent
replications. OFF is the same binary with the previous eight streaming options
on and the new call/global option off. This is not full-corpus timing qualification.

For the original affected eight, steady ON/OFF was 0.97453 (95% bootstrap
interval [0.81705, 1.10553]) and ON/main 1.03050 ([0.92450, 1.56923]).
For the new top eight, steady ON/OFF was 1.19121 ([0.96170, 1.91830]) and
ON/main 1.17456 ([0.96743, 1.76628]). These are arithmetic means of per-workload
median-time ratios, not ratios of aggregate execution times. Zero-selected
control steady A/A was 1.03298, failing the 2% agreement gate; individual
workload controls also vary substantially. The broad intervals do not establish
an execution improvement or compliance with the 5% regression limit.

Compilation uses ratios of arithmetic mean workload medians. Original-eight
ON/OFF was 1.01344 ([0.76066, 1.33415]); new-top-eight ON/OFF was 1.01357
([0.75295, 1.39076]). Their A/A controls were 0.97462 and 0.97352, both failing.
First-call A/A also failed for both groups (1.06705 and 1.10269). No compilation
or first-call improvement is established. Intervals resample paired trials;
compilation intervals additionally resample workloads.

The public coverage diagnostic selected 575 functions in 120 workloads, versus
477 in 105 previously. Those functions account for 20.46% of frozen baseline
public compilation cost, not a measured saving or evidence for a 40% reduction.
The OFF native images matched the previous checkpoint for all 145 modules.

Mean absolute compiled-barrier VmHWM was 11.989709 / 11.972710 / 11.972037 MiB
for main / prior streaming / joint ON in the matched fixed-slot memory run.
Memory is effectively unchanged and the absolute 5 MiB goal remains unmet.
Source is unchanged. No promotion or PR is proposed.

### Compare-select and unsigned division checkpoint (c1cca19e)

Two additional default-off options preserve existing native selection policies:
`WAGO_EXPERIMENTAL_STREAM_COMPARE_SELECT=1` consumes a unique deferred integer
comparison directly as CMP/CMOV after eagerly preparing operands;
`WAGO_EXPERIMENTAL_STREAM_MAGIC32=1` uses unsigned i32 constant-divisor
product/shift or wide-reciprocal sequences. Signed/i64 division, zero divisors
and unsupported cases retain their existing paths. Expensive-arm pure-select
retains priority. All allocation and fixed-register preparation precedes the
comparison flags consumed by CMOV.

The exact frozen source passed 83 focused ownership/statistics top-level tests
with zero failures or skips (0.546 s), including independent alias, pressure,
width, eager-trap and arithmetic checks. The production adapter also passed
all 145 frozen corpus runtime oracles. Performance/memory qualification for
this revision remains pending. Earlier
negative results remain documented above. No speedup, target achievement or
promotion is claimed.

### Short/long execution protocol bridge (c1cca19e)

All 1,080 records verified in a preregistered execution-only comparison of the
same 15-case cohort, four arms, two regimes and nine trials. The short regime
retains original operation counts. Seven stateless cases use baseline-calibrated
20 ms batches with three warmup and three measured batches. Eight fresh-instance
or vector cases instead use separately reset/verified samples, capped at 32;
all eight remain below the 20 ms target. This preserves scenario semantics,
not a uniform-duration guarantee. No first-call or compilation result is inferred.

The two new policies are enabled together versus the same binary with both off.
Bio alignment improves within-candidate to ON/OFF 0.86572 (paired trial-bootstrap
95% interval [0.79949, 0.95247]), but remains ON/main 1.25107
([1.21890, 1.35334]); its main A/A control fails. Rabin–Karp shows no demonstrated
runtime benefit despite removing native DIV: ON/OFF 0.99538
([0.96060, 1.03952]), ON/main 1.62879 ([1.57276, 1.67923]). Its control also fails.

Long-regime arithmetic mean workload ratios for the new top-eight group are
ON/main 1.18067 ([1.15836, 1.19471]) and ON/OFF 1.00378
([0.98388, 1.01428]). The original eight have ON/main 1.05366
([1.03106, 1.06358]). These overlapping groups are not independent replications.
Numerous individual controls fail despite near-one group A/A point estimates;
zero-selected control uncertainty also fails the strict agreement requirement.
Only PCRE2, dr_wav and fast_float pass the preregistered per-case gate: main A/A
within 2%, its 95% interval within [0.97, 1.03], and ON/main upper interval ≤1.05.
Thus the engine is not qualified against the overall 5% execution limit.

The 83 focused tests and 145 production runtime oracles remain valid for this
unchanged source. There is no new RSS measurement for c1cca19e; the previous
f320c628 memory result must not be attributed to it. No promotion or PR is
proposed, and no compile-time or absolute-memory target achievement is claimed.

### Cached memory-bound checkpoint (e746f1ea)

`WAGO_EXPERIMENTAL_STREAM_CACHED_BOUND=1` reuses the existing unshared-memory
size register for scalar and bulk unsigned end-of-access comparisons when
available. It retains explicit checks and trap ordering; absent-cache and
shared-memory cases retain the memory-backed comparison. This is not a
dominated-check cache and does not eliminate checks.

The exact frozen source passed 86 focused ownership/statistics top-level tests
with zero failures or skips (0.698 s), including actual regional leases and
memory-growth coherence. Production corpus runtime verification and performance
qualification for this revision are pending. Prior c1cca19e runtime results are
not qualification for this new revision. No improvement or promotion is claimed.

### Same-value bounds facts checkpoint (5e6b747b)

`WAGO_EXPERIMENTAL_STREAM_BOUNDS_FACTS=1` adds a four-entry weak certificate
cache keyed by live operand identity and creation order. A successful dominating
check can cover a later access with no larger unsigned 64-bit extent. Effective
address arithmetic is unchanged; only the redundant comparison/trap branch is
omitted. Certificates retain no value references and are cleared at control,
call, bulk, canonicalization and reset boundaries. A bounded 72-byte requested
scratch object is reused per worker and included in scratch accounting; this
is not a measured RSS saving or a total allocator-footprint bound.

The exact frozen source passed 91 focused ownership/statistics top-level tests
with zero failures or skips (0.548 s), including five new fact-cache tests.
Production corpus runtime verification and performance/memory qualification
for this revision are pending. Earlier checkpoint results do not qualify this
revision. No improvement or promotion is claimed.

### Completed bounds-preservation diagnostic (5e6b747b)

The production adapter passed all 145 runtime oracles. A fixed 15-case,
nine-trial long-only execution screen verified all 540 records. It reused the
previous baseline-calibrated operation counts without recalibration: seven
stateless 20 ms-target batch regimes and eight cap-limited fresh/vector regimes.
The same binary's prior eleven options remained enabled; cached bounds and
same-value facts were switched off/on together. No first-call or compilation
result is inferred from this execution-only screen.

For the original eight, mean workload ON/main was 1.02147 (paired-bootstrap
95% interval [0.99255, 1.04181]); ON/OFF was 0.98194 ([0.94206, 0.99175]).
For the overlapping new top eight, ON/main was 1.14098 ([1.10586, 1.16707])
and ON/OFF 0.97243 ([0.92802, 0.98729]). Per-case controls still fail: the
zero-selected A/A interval is [0.97572, 1.06489], outside the required agreement
band. Only NanoSVG and dr_wav pass the strict per-case control and execution
gates. AES and Rabin–Karp remain approximately 1.458 and 1.501 times main at
the point estimates. These results do not qualify the overall 5% regression
limit or justify promotion.

Native diagnostics distinguish the mechanisms. The AES round loop retains
32 bounds comparisons and 32 unsigned trap branches, now using the cached
register instead of loading the bound. Facts eliminated outside that loop must
not be credited to its hot rounds. Other loops can eliminate dominated checks;
static instruction counts are not hardware execution counts or speedup proof.

All 435 matched fixed-slot, full-feature memory-profile oracles verified.
Arithmetic mean workload-median absolute compiled-barrier VmHWM was
11.9871767 / 12.0643319 / 12.0609644 MiB for main / bounds OFF / bounds ON.
There is no meaningful memory improvement and the absolute 5 MiB target is
unmet. Source is unchanged; no PR or promotion is proposed.

### Metadata reuse and admission funnel checkpoint (db4f2dc7)

`WAGO_EXPERIMENTAL_STREAM_RESTORE_REUSE=1` retains sole-owned ordinary
canonical-frame local descriptors during same-path restoration. It excludes
shared values and selected register/backing homes, preserves capture/store and
stack handling, and refreshes allocator order at the original reconstruction
position without relaxing the creation budget. Alternate-path restoration is
unchanged. This tests metadata-work reduction, not a new lowering semantics.

`WAGO_EXPERIMENTAL_STREAM_HINT_FUNNEL=1` uses existing function-local negative
hints to avoid unsupported admission attempts after inline planning. It does
not prove eligibility or necessarily avoid a byte scan. The relevant small
admission-cost attribution does not imply a 40% compilation reduction.

The preceding b7f0b4f7 checkpoint passed 98 focused ownership/statistics
top-level tests with zero failures or skips and all 145 production runtime
oracles. This revision only reorders equivalent rejection predicates to check
node fields before scanning selected homes. The final db4f2dc7 focused
RestoreReuse/HintFunnel gate passed all 7 tests in 0.008 seconds with zero
skips or failures. Across 290 fresh public-API subprocesses, OFF/ON native
images and serialized metadata matched exactly for all 145 workloads, with
the same 575 selected functions. Diagnostics observed 63,142 same-path
descriptor reuses and 99 avoided admission attempts. These are mechanism
counts, not speedup measurements. Final rebuilt production runtime verification
and latency qualification remain pending. No measured improvement or
promotion is claimed; earlier results remain distinct.

### Metadata experiment outcome (source unchanged at db4f2dc7)

The final production build passed all 145 runtime oracles. Public-API native
images and serialized metadata matched OFF/ON for all 145 inputs; OFF native
images also matched the preceding 5e6b747b candidate. Selection remained 575
functions, with 63,142 same-path descriptor reuses and 99 avoided admission
attempts. Unchanged emitted code does not repair the earlier engine execution
regressions documented above.

The preregistered full-suite cold screen verified 2,900 fresh-process records:
145 inputs, five trials, four randomized adjacent arms. The same executable
slot and full-feature adapter were used. OFF retained the preceding thirteen
experimental flags but disabled restore reuse and the hint funnel. The
estimand was the arithmetic mean of per-workload median first public-Compile
times. Confidence intervals used 5,000 paired within-workload trial resamples,
with identical indices across arms and no workload resampling of this fixed
145-input suite.

Mean times were 725.119 / 742.951 / 760.174 / 753.588 microseconds for main A /
main B / metadata OFF / metadata ON. ON/OFF was 0.99134 (95% CI
[0.95307, 1.06813]); ON/mean-main was 1.02664 [0.95450, 1.09281]. Main A/B was
0.97600 [0.94240, 1.13871], failing the preregistered control gate. Thus no
qualified latency improvement or 40% reduction is established.

All 435 separate fixed-slot memory-profile records verified. Absolute
compiled-barrier mean VmHWM was 11.987904 / 12.062231 / 12.060722 MiB for main /
metadata OFF / metadata ON. There is no meaningful memory gain; the absolute
5 MiB goal remains unmet. These memory-profile observations are not latency
measurements. This remains an experimental checkpoint, with no promotion or
PR proposed.

### Warm production-path attribution (db4f2dc7, source unchanged)

A separate bounded, untagged standard-Go profile compared the same binary
with all fifteen experimental flags OFF against the preceding thirteen ON
and the two metadata flags OFF. An independent pristine-main public-API
reference was verified first. Each arm used two warm-up traversals, 120 fixed
CPU-profile traversals (17,400 Compile/Close calls), and a separate 30-traversal
allocation pass (4,350 calls). All 145 native images matched their reference
before and after each run. No outcome-dependent extension was performed.

CPU profiles contained 11.05 / 10.88 seconds of samples for OFF / ON. Encoder
sample unions were 4.80% / 4.78%. In ON, streaming body work covered 15.63%
and legacy body work 53.31%; canonicalization covered 6.62% and restoration
4.14%. These cumulative sample unions overlap and must not be added as
independent costs. Runtime GC samples were approximately 10%; they cannot be
assigned exclusively to lowering or converted into a cold-compile saving.

Sampled allocation-space deltas were 421,555,831 / 351,581,921 bytes across
the 4,350 OFF / ON compiles. Operand-arena allocation estimates decreased from
102,777,520 to 33,611,989 bytes. Allocation profiles used 64 KiB sampling and
baseline subtraction, include observer allocations, and are approximate.
This supports reduced allocation work, not reduced retained RSS or qualified
cold latency. The run measures warmed, pooled Compile/Close behavior, not
first compilation. The preceding failed cold controls and unmet memory and
execution targets remain unchanged; no demonstrated 40% opportunity or
promotion follows from this diagnostic.

### Separate Go PGO experiment (frozen db4f2dc7, streaming flags OFF)

This build experiment is separate from subsequent source changes. Training
used only the preregistered 114-input training split; 31 held-out inputs were
primary for qualification. Both full-feature adapters used identical source,
Go 1.27.1 and build options except explicit PGO off versus the training CPU
profile. All fifteen streaming flags were OFF. All 145 native images and
serialized metadata matched, native images matched pristine main, and both
production adapters passed all 145 runtime oracles.

The fixed protocol verified 2,175 records each for cold compilation, first
call and steady execution, plus 435 memory-profile records. Ratios below are
arithmetic means of per-workload normalized median ratios, not ratios of
aggregate mean times. Intervals are 5,000 paired trial resamples without
workload resampling.

| Scope | Cold PGO/control (95% CI) | First call | Steady |
| --- | --- | --- | --- |
| Held-out 31 | 1.06954 [1.02713, 1.16387] | 1.01154 [0.94594, 1.18536] | 0.99772 [0.98068, 1.01867] |
| Training 114 | 1.13408 [1.10891, 1.19673] | 1.01824 [0.97415, 1.10116] | 1.00021 [0.99309, 1.02147] |
| Full 145 | 1.12028 [1.10148, 1.17514] | 1.01681 [0.98042, 1.08912] | 0.99968 [0.99343, 1.01705] |

All three cold and all three first-call A/A control gates failed. Held-out
steady controls also failed: A/A 1.01686, CI [0.99414, 1.03265]. Training and
full-suite steady aggregate controls and aggregate 5% guards passed, but this
does not override the held-out failure or establish universal per-input
qualification. Cold ratios of aggregate mean workload medians were 1.01080 /
1.02708 / 1.02390 for held-out / training / full, a different estimand from
the table. No qualified compilation improvement is established.

Absolute compiled-barrier mean VmHWM (control A / control B / PGO, MiB) was
12.167213 / 12.168725 / 12.233745 held-out, 12.064385 / 12.065138 / 12.129900
training, and 12.086369 / 12.087284 / 12.152101 full-suite. Memory increased
rather than approaching 5 MiB. These one-trial memory observations have no
confidence interval. No default PGO profile, source promotion, or target
success is proposed; failed controls preclude treating point changes as
qualified gains or precise causal regressions.

### Existing-quality preservation checkpoint (68cea43b)

Three separate default-off controls preserve existing lowering policies:
`WAGO_EXPERIMENTAL_STREAM_PRESERVE_DENSE` retains the legacy dense-rotate
regional cohort before streaming admission and reuses the predicate result
at the original policy check, avoiding duplicate decoding.
`WAGO_EXPERIMENTAL_STREAM_RORX` additionally requires that preservation flag
and the existing CPU/optimization policy before emitting immediate BMI2
rotates. `WAGO_EXPERIMENTAL_STREAM_MIN_BOUNDS` queries at most 32 existing
expression-node occurrences and uses the immutable memory declaration minimum
to omit only proven bounds checks. Address evaluation, eager child traps and
nonwrapping address formation remain intact; this proof does not populate
same-value bounds facts. No new feature admission or benchmark-specific
exclusion is introduced.

The exact checkpoint passed 108 focused ownership/statistics tests with zero
failures or skips in 1.029 seconds. An independent retained-alias RORX execution
matrix also passed untagged in 0.011 seconds. An earlier pressure fixture
incorrectly required identical physical operand locations for equivalent
aliases; its failure was retained and the test strengthened with generation
and execution checks, rather than changing production behavior to fit it.
Production 145-input runtime and performance qualification for this revision
are pending. Earlier PGO measurements apply only to db4f2dc7 with streaming
flags OFF, not this source checkpoint. All prior target failures remain;
no speedup, RSS improvement, promotion or PR is claimed.
