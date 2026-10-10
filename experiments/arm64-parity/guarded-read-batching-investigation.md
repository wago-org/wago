# General guarded read batching investigation

Source inspection only; no implementation or performance claim.

The application source's dilation loop reduces neighbor cells with wrapping i32 bitwise OR under coordinate bounds, then feeds the result into a scalar rolling checksum. It is not a min/max reduction. Existing pure_reduce accepts independent additive i64 reductions, and stream_reduce accepts independent products followed by wrapping sum; neither grammar covers conditional read-only expressions and an order-dependent scalar recurrence.

A broader general seam would batch independent per-iteration integer terms while preserving the scalar recurrence in original lane order. Eligibility must be based on operations and effects: affine induction/addresses, invariant predicates or proven full-lane predicate ranges, read-only memory, and no calls/atomic/shared-memory/EH/custom effects. Complete address/wrap/bounds proofs must precede batched reads; failure takes the unchanged scalar path so invalid inputs retain trap/side-effect ordering. No stencil width, checksum constant, workload identifier, or input dimension should appear in admission.

Before attempting it, inspect actual lowered bytecode and current profiles. The former general store-map vectorizer was rejected after several mitigations because compilation grew9–20% with weak gains on many cases; do not restore it wholesale. An eventual bounded DAG should use compact node/child ids and avoid heap allocations/repeated decoding, and demonstrate meaningful execution gains against compile cost on a focused affected set.

Lowered bytecode saved in vision-dilation-investigation.wat. Its neighbor loop is unrolled into guarded reads. The retained sampled assembly contains ordinary CMP/B.lt + CMP/B.gt and CMP/B.lt + CMP/B.ge pairs to the same exit; existing guardedTestCCMP covers TST/AND tests and skips this grammar. A separate general size-stable common-exit compare draft is prepared under common-exit-compare-prototype, unintegrated/untested. This may provide a smaller, cheaper stepping stone before building guarded loop batching.

## Lowered-loop inspection after compare prototype

The actual inner loop has multiple induction locals: a coordinate advances by one and a byte address advances by four. Its exit compares the updated coordinate against an invariant bound, rather than using the countdown grammar in `inspectPureReduce`. The existing scanner also accepts no blocks or guarded local merges. Extending its opcode list alone would therefore be incorrect and would not admit this loop.

A reusable extension needs three separate proofs: (1) affine induction updates and trip count, including zero-trip and i32 wrap; (2) structured conditional expressions with explicit local merge values; (3) separation of independent terms from the scalar recurrence. Here the recurrence is wrapping multiply of accumulator XOR term, so reassociation is invalid. Batch terms in parallel only after proving all lane addresses and predicates, then consume the resulting terms in lane order. Multiple affine inductions can be related by initial values and steps; do not require the particular coordinate/address relationship observed here.

Guarded loads cannot become unconditional merely because their addresses lie within linear memory: the original predicate may suppress a trapping load. Each inactive lane must preserve the original merge value. A batch fast path can require all predicates true for the entire batch, with the original scalar loop handling boundary iterations; alternatively masked expressions require independently safe addresses. Initial implementation should choose the former proof to keep compile cost bounded. Tail execution and failures use the unchanged scalar path.

Common-exit comparison folding is now integrated as a default-off candidate, fully oracle-qualified. Its initial focused result is dilation -2.55% execution with +0.90% compilation. Scanner prefilter mitigation and repeat measurements are pending. This does not establish a result for loop batching.
