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
