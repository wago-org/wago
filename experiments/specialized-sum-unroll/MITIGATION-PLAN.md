# Mitigation study, fixed before measurements

Continue PR #910 at 975247c4a. Keep prior evidence and production defaults.

- P: keep D's 16/4 main loop and replace its scalar remainder with two loads
  using two existing chains. Odd counts enter the second half. Each load uses
  displacement zero and advances the memory32 address separately. Keep the
  original range proof, wrap dispatch, exit count, and common combination.
- DR: D plus an isolated code-capacity hint. Reuse existing loop/memory/signature
  hints while counting module body bytes. Bound added space; do not change
  admission or shared allocation policy. Measure false positives in controls.
- PR: combine the two only after each independent comparison is recorded.
- No new threshold or instruction-layout search. Use normal 4/4 and D as
  references. Measure public compile, allocation bytes/count, execution,
  lifecycle, native code and frames. Keep aligned and unaligned short, cache,
  and streaming inputs, 20 interleaved pairs, CPU 2 and GOMAXPROCS=1. Repeat
  promising results once. Keep unfavorable results and stop after these designs.
- Add tests before implementations. Execute the full native oracle/wrap matrix
  and require specific candidate markers. Review correctness and performance
  independently. Keep the PR draft and do not enable a production default.
