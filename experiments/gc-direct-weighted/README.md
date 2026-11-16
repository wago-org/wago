# Direct16 weighted reduction prototype

Separate tags `wago_gc_direct_weighted_sum` and `wago_gc_direct_weighted_sse2` extend the existing trusted direct loop build/receipt (policy 6), not compact admission. The latter selects the shared six-clean-XMM SSE2 reduction; pressure fallback remains scalar. Default builds retain empty weighted state.

The existing final i32 array, one-receiver/no-safepoint scanner and original checked first access remain authoritative. The shared bounded matcher and arithmetic are unchanged in semantics. The latch explicitly requires the direct trusted lease and uses payload offset 16; compact uses its own active lease and offset 8. Direct length remains supplied by the existing direct loop's Length@8 handling. No direct/compact native view aliases or new unchecked entry paths are introduced.

This is an incremental compiler-parity experiment, NOT an isolated layout comparison: direct still lacks compact's paired/range-transfer stages. Compare direct trusted-loop vs direct weighted first, with identical collector and unrelated options, before making any layout judgment. No allocation elision or constructor/store fusion is included.

Source-only checkpoint. Independent review and fresh tests are required; no performance measurements or gain claims.
