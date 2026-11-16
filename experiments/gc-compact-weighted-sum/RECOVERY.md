# Weighted reduction reconstruction

## Correction: initial worker instrumentation was not enabled

The initial worker commands recorded below actually used `wago_regalloc_checks`. This was not a documentation-only typo: the supported build tag in `src/core/compiler/backend/railshot/amd64/regalloc_check_enabled.go` is `wago_regalloccheck`. Go accepted the unknown tag, but it did not enable register-allocation instrumentation. Those reported successful executions establish only the tests that ran without that instrumentation; they must not be described as instrumented qualification. The original commands and durations are deliberately preserved rather than silently relabeled. Any separately recorded canonical run has its own command/provenance; correct-tag reruns must be reported independently.


Reconstructed from retained implementation context after the cloud workspace was replaced. Unlike the preceding SSE reconstruction, no independent pre-loss weighted file-blob manifest is available. This is new source/provenance requiring fresh review and qualification. The lost weighted experiment had not completed source review or tests; none are claimed here.

The separate tag `wago_gc_compact_weighted_sum` recognizes only a bounded canonical i32 weighted reduction over one unchanged final i32 array, with distinct clean pinned induction and accumulator locals. It preserves the original checked first iteration and falls back to the original loop if the validated array length does not cover the full remaining span. Fast arithmetic remains modulo32, with both local homes synchronized at exit. No artifact names or specific bound/weight constants are recognized.

## First qualification of reconstructed source

Go 1.27.1, serial `-p 1`, tags `wago_gc_compact_weighted_sum wago_gc_compact_refstruct wago_regalloc_checks wago_codegenstats`:

- AMD64 backend `TestCompact(Weighted|Range|PairLoop|Loop)`: PASS, 0.021s. Exact-artifact telemetry confirms only the checksum function selects the new weighted reduction; the other three functions do not.
- Wago `^(TestCompactGC|TestCompactNative|TestGCCompact)`: PASS, 1.022s. Weighted fixtures exercise signed/overflowing weights and modulo-32 sums/products, first-condition false, one/many iterations, final index and sum, short-array traps, and repeated invocations alongside existing compact moving-root/SSE regressions.

This is the first tested reconstruction, not a restoration of historical weighted test results. No performance measurements have been made. Additional dedicated multi-loop stale-state and matcher-negative fixtures remain useful follow-up coverage.

Dedicated follow-up regressions in `fe8eb3cd` subsequently passed with the same Go version, tags, serial flags and filters: backend 0.014s, applicable runtime matrix 1.096s. This qualifies canonical/rejected/canonical matcher resets (including every truncated prefix), and sequential matching/nonmatching/matching loops with distinct bounds/weights, repeated invocations, wrapping results and a late middle-loop bounds trap. No production fixes were required and no performance timing was performed.

## Integrated ABI4 qualification

At canonical `4ffe2f5e`, the weighted + refstruct + regalloc runtime matrix
passed (1.176s), and weighted/range/pair/loop backend tests with codegenstats
passed (0.013s). Full default Wago (50.882s), native collector (3.515s), and
amd64 backend (11.533s) passed. Native compact tag unions were independently
checked against all 65,536 mode combinations; weighted does not imply age2.
This is correctness qualification, not a measured performance gain. The next
matched comparison keeps age2 and all collector diagnostics disabled in both
variants and uses fresh builds on the same recovered Intel host.
