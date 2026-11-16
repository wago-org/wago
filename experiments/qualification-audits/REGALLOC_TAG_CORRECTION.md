# Worker qualification build-tag correction

The supported AMD64 instrumentation selector is `wago_regalloccheck`, as declared by `src/core/compiler/backend/railshot/amd64/regalloc_check_enabled.go`. The distinct string `wago_regalloc_checks` does not enable that instrumentation. Go permits unknown build tags, so a successful test command containing the latter does not establish an instrumented pass.

This audit checks retained worker tool-call commands and recovered repository qualification records. It does not reconstruct unavailable shell history or alter original timings. The worker's initial instrumentation-qualified descriptions are withdrawn where the wrong tag was executed. Test assertions and successful semantic executions remain as recorded; they are not silently relabeled as correct-tag runs. Separately evidenced canonical correct-tag checkpoints stand independently.

## Affected records

All commands below used Go 1.27.1 and serial `-p 1`; `wago_regalloc_checks` is preserved verbatim as the incorrect executed/recorded selector.

| Record | Mode tags accompanying the wrong selector | Retained result and scope |
| --- | --- | --- |
| `gc-compact-range/README.md`, range stage | `wago_gc_compact_range_loop wago_gc_compact_refstruct`; backend also `wago_codegenstats` | Backend pass with no duration recorded; runtime 0.608s. The document explicitly records the unsupported selector. Semantic-only, not instrumentation proof. |
| Same document, stride stage | `wago_gc_compact_range_stride wago_gc_compact_refstruct` | Backend 0.005s and runtime 0.692s. Runtime command explicitly records the unsupported selector; backend prose saying “with regalloc” is not an independently retained correct-tag receipt. |
| `gc-compact-range-sse2/RECOVERY.md`, fresh recovery | `wago_gc_compact_range_sse2 wago_gc_compact_refstruct wago_codegenstats` | Actual retained worker calls: backend 0.012s, runtime 1.169s. Instrumentation was not enabled. |
| `gc-compact-weighted-sum/RECOVERY.md`, initial reconstruction | `wago_gc_compact_weighted_sum wago_gc_compact_refstruct wago_codegenstats` | Actual retained calls: backend 0.021s, runtime 1.022s. |
| Same record, certificate-reset follow-up | Same tags | Actual retained calls: backend 0.014s, runtime 1.096s. |
| `gc-compact-weighted-sse2/README.md`, initial worker qualification | `wago_gc_compact_weighted_sse2 wago_gc_compact_refstruct wago_codegenstats` | Actual retained calls: backend 0.021s, runtime 1.026s. |
| `gc-direct-weighted/README.md`, scalar worker backend | `wago_gc_direct_weighted_sum wago_codegenstats` | Actual retained call: 0.030s. |
| Same record, scalar runtime | Previous tags plus `wago_gc_direct_trusted_refstruct wago_gc_direct_trusted_alloc wago_gc_direct_fill_sse2 wago_gc_direct_copy` | Actual retained call: 1.134s. |
| Same record, optional SIMD backend/runtime | Composed direct tags above, replacing `wago_gc_direct_weighted_sum` with `wago_gc_direct_weighted_sse2` | Actual retained calls: backend 0.032s, runtime 0.686s. |

The recovered historical `gc-compact-range-sse2/README.md` also says “with regalloc” for pre-loss runs (0.009s/0.636s and earlier 0.056s/0.700s), but does not include a full command. The original binaries and raw shell outputs were lost. This wording alone is not evidence of correct-tag instrumentation and must not be cited as such without a separate correct-tag receipt. Its exact recovered text is preserved.

Backend filters in the fresh worker calls were `TestCompact(Range|PairLoop|Loop)`, `TestCompact(Weighted|Range|PairLoop|Loop)`, or `TestCompactWeighted|Test.*Direct.*Loop|Test.*Receipt`, as appropriate. Fresh compact runtime filters were `^(TestCompactGC|TestCompactNative|TestGCCompact)`; direct runtime used `^(TestDirectGC|TestTrustedDirect|TestCompactNativeWeighted)`. The direct mixed-layout rejection and untagged default-footprint commands did not claim register-allocation instrumentation and are not invalidated by this correction.

## Remediation and boundaries

- Preserve the original commands, timings, source identities and semantic results.
- Report new executions using `wago_regalloccheck` as distinct qualification, including their actual test filters. In particular, direct-mode reruns need `TestCompactNativeWeighted`, because those shared fixture names do not begin with `TestDirectGC`.
- Do not retroactively apply a canonical correct-tag run to a different source revision or omitted filter.
- This correction changes no production code and makes no new performance claim. Benchmark measurements are separate from these correctness commands.
