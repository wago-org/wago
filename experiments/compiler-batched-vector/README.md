# Batched independent-vector interruption experiment

Source-only PoC; NOT yet built, tested or benchmarked. No acceptance claims.
Requires `wago_experimental_bounded_vector_polls`; ordinary builds retain their
existing exclusion of interruptible region loops. Default batch size is four
vector groups; additionally enabling `wago_experimental_vector_poll8` selects
eight. The latter tag alone cannot enable the experiment. These compile-time
constants avoid new mutable global configuration or test races.

The existing remaining-group counter decrements only after all group stores and
stream updates complete. A nonzero low-bit mask branches directly to the body;
otherwise the existing scratch-preserving interruption check runs. A separate
counter TEST after that check handles final zero because the check destroys
flags. Thus entry/header and final-group checks remain, with no missing final-zero
poll. First batch length is group-count modulo batch size (or a full batch when
divisible); subsequent batches are full-sized. No extra register, spill slot,
FP body duplication, or additional bytecode scan is introduced.

This deliberately changes polling cadence: interruption may be observed after
up to four/eight vector groups (8–32 scalar iterations depending on features),
not after every group. It bounds work, NOT wall-clock time or scheduler response.
All guest stores form a completed prefix at interruption. This is not exact
scalar/backedge cancellation timing. Existing independent-lane, range/alias,
threaded-memory, recurrence, EH, profile and GC exclusions remain unchanged.
Checked tails and alias fallback retain their original checks. TinyGo tasks
scheduler limitations are not relaxed; polling does not create asynchronous
cancellation support.

Deterministic test injection covers SSE2/AVX group counts around B and 2B,
partial-first and final-only batches, entry/header, tail/odd fallback, alias
fallback, interrupted memory prefixes, grouped trap attribution and engine reuse.
Non-interrupted memory/oracle comparisons cover batch boundaries and scalar
remainders. The existing fixture is a bottom-tested increment loop, so n=0 does
not mean zero trips; it is deliberately not used as a zero-trip test here.
No tests have run yet. Existing site-count assumptions and all new prefix cases
must be validated before any performance qualification.

Pending serial qualification: default backend suite; experimental B=4 and B=8
focused/full suites; checker variants; all145 oracles and changed-image audit;
matched same-toolchain/stack execution, true cold compilation and absolute RSS.
A workload-level <=5% regression gate is still required. No claim that reduced
poll frequency alone closes the Go/TinyGo Jacobi gap or fixes yyjson regressions.
