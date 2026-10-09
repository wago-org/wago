# Follow-up plan, fixed before candidate measurements

Continue head d49f7252a906af9ffab94dabe1c83059c8589819 on PR #910.
Preserve the original evidence. Do not change the recognizer or production defaults.

1. Measure the unchanged corpus with normal 4/4 and enabled D (16/4).
   Confirm the timed memory.sum function selects the candidate. Other application
   modules are controls until function-level diagnostics prove admission.
2. Add one hybrid: groups of 16, then groups of 4, then scalar loads. Reuse
   four accumulators and a single wrap dispatch. Compare with normal 4/4 and D.
3. Explore only three thresholds: 64, 128, and 256 *remaining* elements after
   the first scalar load. Small counts use a shared four-element group; large
   counts use uniform 16/4 and its scalar tail. Keep proof/setup/combine shared.
   Measure all three once, then freeze at most one for a separate confirmation.
   Add a threshold-gated hybrid only if the first hybrid/threshold results show
   a real need; do not tune instruction alignment.
4. Each decisive comparison uses 20 alternating pairs on CPU 2, GOMAXPROCS=1.
   Repeat promising results in separate processes. Include counts around each
   threshold, short counts, every modulo-16 remainder, aligned/unaligned cache
   inputs, and a buffer larger than L3. No builds during timings.
5. Stop after these designs. Report any absent real-application coverage.
   Do not infer application gains from synthetic memory.sum.

The corpus benchmark selector is a test-only linkname bridge to a private
build-tagged setter (the original shared-record bridge was replaced during
correctness review). It is not linked into production. Function-level diagnostic
assertions and native artifacts must confirm that this bridge selects the emitter.
