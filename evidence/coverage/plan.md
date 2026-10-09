Continuation: compare all54previously omitted Fibonacci bytes without fabricated
instruction/source semantics; reduce avoidable comparator work/allocations.
Pre-change diagnostic baseline8e82e1b26e0b5ac015b761f0c4bf5c9c894fb0b2
(source60481522). Current main fetched privately:c95243aa438e4026bf5d2f263c83e53f2616d7da;
only upstream samply-profile metadata files changed since05af; merge before edits.

Hypothesis: profile-owned raw gaps partition the native image with mapped regions,
while mapped operand qualification remains separate. Identical instruction pairs
can decode once and branch encoding constants avoid allocations without changing
unknown or invalid-relocation behavior. KEEP if source/owner alignment controls,
unknown/limits and original paired controls pass; REVISE on false qualification.
No CALL/RET/PUSH/POP semantics or invented source locations will be added. Raw gap
changes are byte observations, including layout/relocation noise. Raw comparisons
must never grant new relocation waivers or proof of semantic equivalence.

Budgets: JSON1MiB;64mapped regions;128raw gaps;4096mapped instructions;64KiBimage;
512mapped changes/unknown sites/raw changes. Linear partition checks and anchored
positional comparison; no wholeimage LCS or snapshot cache. Short paired diagnostic
benchmarks: three/five alternating samples,100iterations/sample (capture5),CPU15,
GOMAXPROCS1. Finish builds before timing and coordinate with independent reviewer.
Production compiler/runtime untouched: verify matching current-main ordinary
runtime hash and focused native/publicAPI checks. Memory B/op not peak/retained.

Parent authorized publication explicitly after the user's 'Yes': finish local
review/documentation/checks, duplicate-check, push isolated branch, separate draft
PR, verify exact published head and draftsmokeCI; do not merge.
