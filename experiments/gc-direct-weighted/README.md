# Direct16 weighted reduction prototype

## Correction: initial worker instrumentation was not enabled

The initial worker commands recorded below actually used `wago_regalloc_checks`. This was not a documentation-only typo: the supported build tag in `src/core/compiler/backend/railshot/amd64/regalloc_check_enabled.go` is `wago_regalloccheck`. Go accepted the unknown tag, but it did not enable register-allocation instrumentation. Those reported successful executions establish only the tests that ran without that instrumentation; they must not be described as instrumented qualification. The original commands and durations are deliberately preserved rather than silently relabeled. Any separately recorded canonical run has its own command/provenance; correct-tag reruns must be reported independently.


Separate tags `wago_gc_direct_weighted_sum` and `wago_gc_direct_weighted_sse2` extend the existing trusted direct loop build/receipt (policy 6), not compact admission. The latter selects the shared six-clean-XMM SSE2 reduction; pressure fallback remains scalar. Default builds retain empty weighted state.

The existing final i32 array, one-receiver/no-safepoint scanner and original checked first access remain authoritative. The shared bounded matcher and arithmetic are unchanged in semantics. The latch explicitly requires the direct trusted lease and uses payload offset 16; compact uses its own active lease and offset 8. Direct length remains supplied by the existing direct loop's Length@8 handling. No direct/compact native view aliases or new unchecked entry paths are introduced.

This is an incremental compiler-parity experiment, NOT an isolated layout comparison: direct still lacks compact's paired/range-transfer stages. Compare direct trusted-loop vs direct weighted first, with identical collector and unrelated options, before making any layout judgment. No allocation elision or constructor/store fusion is included.

Source-only checkpoint. Independent review and fresh tests are required; no performance measurements or gain claims.

## Initial qualification

Independent actual-source review found no blocker. Go 1.27.1, serial `-p 1`:

- Direct scalar weighted backend selection/matcher/direct-loop/receipt filter: PASS 0.030s with `wago_gc_direct_weighted_sum wago_regalloc_checks wago_codegenstats`.
- Scalar runtime `^(TestDirectGC|TestTrustedDirect|TestCompactNativeWeighted)`: PASS 1.134s, adding `wago_gc_direct_trusted_refstruct wago_gc_direct_trusted_alloc wago_gc_direct_fill_sse2 wago_gc_direct_copy` to those tags.
- Replacing weighted_sum with weighted_sse2 in the composed configuration: backend PASS 0.032s; same runtime filter PASS 0.686s. Includes lane/tail, negative/high-bit weights, stale-loop, roots, private receipt/null-PC, and ingress coverage.
- Mixed `wago_gc_direct_weighted_sse2 wago_gc_compact_reads`: explicit compact/native receipt rejection and mode source-selection tests PASS 0.013s.
- Default untagged `TestReferenceGlobalPersistenceAndFootprintsStayBounded`: PASS 0.007s.

No source fixes were required. No performance timing was conducted. Compact-mode shared-emitter composition should be requalified during canonical integration; the source-only checkpoint status above predates these direct-mode tests.
