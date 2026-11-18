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
