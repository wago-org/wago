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
