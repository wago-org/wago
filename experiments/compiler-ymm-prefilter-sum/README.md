# Batched YMM/prefilter/cooperative-sum checkpoint

Final built source c3062c2a. Actual TinyGo tasks/conservative/opt2/default stack,
mapped+bounded+adjacent_ymm+cooperative_linear_sum experimental tags. Mapping
switch unset. Binary SHA256:
57f6b1f7639789dd8d8241c72ef97962fab74250fcbbf75ce90f9d6897d6a779

Earlier YMM/prefilter8bd3c3bd passed full Go default/YMM/checker backend+runtime,
actual TinyGo mapped roots/panic cleanup and delayed YMM boundary/tail tests.
Isolated unchanged-production public lifecycle/finalizer/scalar-argument tests
also passed before sum integration. These are not full public TinyGo suites.

Per latest user instruction, subsequent changes were batched and tested narrowly:
- Nested AST/byte F64Store, malformed stale-proof reset and actual inlinecallee
  tests pass. Initial AST fixture incorrectly assumed eager DecodeModule AST;
  corrected test constructs real nonempty nested AST using reflection of the
  private payload, with no production layout assumptions or API change.
- Sum selection, results/fallback, delayed interruption and sparse4GiB tests pass.
  These caught missing cooperative dispatch in driver; fixed narrow opt-in gate.
  Emission evidence is mandatory, not inferred from passing scalar fallback.
- One combined adapter built. Representative memory/sum,GEMM,yyjson,pcre2,
  json-as-simd,db-bitmap-index cold oracle/protocol smoke passed12/12 paired records.

No final145 sweep or repeated full suites, per user direction. No actual TinyGo
sum-specific unit execution after integration; combined memory/sum oracle passed.
No performance/RSS acceptance claim. Exact bitmap native image1333bytes retained
with receipt for follow-up inspection, SHA256:
e88328eb1ad9b9a1995b2d19dc5bf70cce6e4e65a80cc0834224d8d264574aab
