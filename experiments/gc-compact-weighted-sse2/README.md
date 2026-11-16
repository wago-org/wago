# Opt-in checked weighted SSE2 reduction

## Correction: initial worker instrumentation was not enabled

The initial worker commands recorded below actually used `wago_regalloc_checks`. This was not a documentation-only typo: the supported build tag in `src/core/compiler/backend/railshot/amd64/regalloc_check_enabled.go` is `wago_regalloccheck`. Go accepted the unknown tag, but it did not enable register-allocation instrumentation. Those reported successful executions establish only the tests that ran without that instrumentation; they must not be described as instrumented qualification. The original commands and durations are deliberately preserved rather than silently relabeled. Any separately recorded canonical run has its own command/provenance; correct-tag reruns must be reported independently.


Tag `wago_gc_compact_weighted_sse2` implies the scalar weighted experiment and its prerequisite pipeline. Compare against `wago_gc_compact_weighted_sum` with identical collector settings and other tags. The default compiler is unchanged; no allocation is removed.

The exact bounded scalar weighted matcher, original checked first iteration, full-span guard and pinned index/total requirements are unchanged. Six clean XMM registers are required, with no spills or partial emission on pressure fallback. Remaining counts below four use the existing scalar path. Complete unaligned 16-byte loads use baseline SSE2 only; the exact scalar tail follows.

Unsigned PMULUDQ gives the same low 32 product bits as signed or wrapping Wasm i32 multiplication. Independent PADDD lanes 0 and 2 accumulate even/odd low products; discarded high-product lanes cannot carry into them. Only lanes 0 and 2 are reduced into the scalar accumulator. Full-span validation makes reassociation of these nontrapping modulo-32 operations valid. There are no array writes or alias-dependent reordering, and both canonical local homes are synchronized by the existing latch exit.

Source checkpoint prepared for independent review. Tests have not yet run and no speedup has been measured. Startup cost and six-register pressure can outweigh throughput benefits.

## Initial qualification

Actual source was independently reviewed before tests. Go 1.27.1, serial `-p 1`, tags `wago_gc_compact_weighted_sse2 wago_gc_compact_refstruct wago_regalloc_checks wago_codegenstats`:

- Backend `TestCompact(Weighted|Range|PairLoop|Loop)`: PASS, 0.021s. Exact-artifact checksum selection, legacy SSE2 instruction inspection, and 0/5/6/16 clean-XMM fallback controls.
- Runtime `^(TestCompactGC|TestCompactNative|TestGCCompact)`: PASS, 1.026s. Includes nonuniform signed/high-bit lane patterns, modulo overflow, 3/4/5/7/8/9 remaining elements, exact tails/final locals, prior multi-loop stale-certificate and moving-root regressions.

No production fix was needed after initial source review. No performance measurement has been made; the earlier paragraph's untested status describes the source checkpoint before this qualification.

Canonical composition at `a640af7d` passed fresh weighted-SIMD runtime/regalloc
(0.960s), separate age2 queue-reuse runtime/regalloc (1.221s) and core (0.018s),
and native build-constraint selection (0.017s). Full default Wago (53.483s),
amd64 compiler backend (10.698s), native GC (2.762s), GC wrapper (0.004s),
compact core (0.016s), debug native GC (3.184s), and patched TinyGo tasks-scheduler
native/wrapper (3.289s/0.004s) all passed. Independent review verified exact mode
selection/complements across all 131,072 production tag combinations. This is
correctness qualification only; SIMD and queue reuse remain separate experiments.
