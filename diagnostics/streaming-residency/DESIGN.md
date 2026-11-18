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
