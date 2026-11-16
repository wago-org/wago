# Direct16 paired and scalar-range prototype

`wago_gc_direct_pair_loop` enables bounded two-receiver leases. `wago_gc_direct_range_loop` additionally enables the generic scalar transfer latch. Both extend existing trusted direct policy6, not compact admission. Neither enables weighted reduction or SIMD automatically; explicitly compose identical weighted settings in comparisons.

The existing bounded scanner must certify exactly two static accesses to distinct invariant locals of the same final integer array type. Two clean pinned pointer caches are lazily populated at their original access sites; a third/repeated/unknown scanner receiver is rejected. The ordinal certificate binds the two accesses even if canonical spilling later loses local provenance. Known mismatches fail closed. Seven available GP registers are required, and no helper, allocation or safepoint may occur in the loop.

Direct access uses Length@8 and Payload@16. Per-access null and logical bounds remain under the existing trusted reference admission. The optional range latch executes only after the original checked iteration and original true backedge condition. Both lengths must cover the complete remaining span; otherwise execution returns to the original checked body. Ordered scalar loads/stores preserve forward overlapping-copy semantics, and the pinned/canonical induction value remains synchronized. Compact transfer SIMD and stride are explicitly gated out of this direct path.

Source-only checkpoint, no tests or performance measurement yet. Actual-source review precedes qualification. Use the supported `wago_regalloccheck` tag for instrumented tests. This improves compiler-stage parity but is not a layout-only comparison against checked compact mode, because existing direct trusted admission differs.

## Initial instrumented qualification

Go 1.27.1, serial `-p 1`, using the supported `wago_regalloccheck` spelling (verified against the enabled instrumentation file), plus `wago_codegenstats`:

- `wago_gc_direct_pair_loop`: backend `TestCompactPair|TestDirectLoop|TestGCModeBuild` PASS 0.031s. Strong direct composition runtime `^(TestDirectGC|TestTrustedDirect|TestCompactNativePair)` PASS 0.689s.
- `wago_gc_direct_range_loop wago_gc_direct_weighted_sum`: backend `TestCompact(Pair|Range|Weighted)|TestDirectLoop|TestGCModeBuild` PASS 0.034s. Runtime `^(TestDirectGC|TestTrustedDirect|TestCompactNative(Pair|Range|Weighted))` PASS 0.715s.
- Strong composition adds identical `wago_gc_direct_trusted_refstruct wago_gc_direct_trusted_alloc wago_gc_direct_fill_sse2 wago_gc_direct_copy` tags. Runtime fixtures include forced moving collections, both receiver nulls/zero trip, aliased receivers, short-source/target partial prefixes, packed widths, final index and existing receipt/ingress tests.
- Mixed `wago_gc_direct_range_loop wago_gc_compact_reads wago_regalloccheck`: explicit rejection and source-mode exclusivity PASS 0.019s.
- Untagged `TestReferenceGlobalPersistenceAndFootprintsStayBounded`: PASS 0.013s.

No production fixes were needed. No benchmark was run. The source-only statement above records the original checkpoint status, before this distinct instrumented qualification. Shared compact-mode integration should retain its own composition tests.

## Integrated qualification

At integrated source `d09ab0f8`, Go 1.27.1:

- Default GC wrapper/native, direct and compact collector packages, AMD64 backend, and the full `src/wago` package pass `go test -p 1` (cached successful results on the final verification invocation).
- Native `wagodebug` tests pass (5.224s).
- Strong direct range + weighted composition with `wago_regalloccheck wago_codegenstats`: applicable backend selection `TestCompact(Pair|Range|Weighted)|TestDirectLoop|TestGCModeBuild` passes (0.054s); selected direct/trusted and compact loop runtime fixtures pass (0.853s).
- Compact pair/range/weighted composition with native allocation, reference store, array access/allocation, fill, copy and register checks passes the selected pair/range/weighted runtime fixtures (0.541s).

A preliminary broader backend test regex also selected `TestDirectGCStructReferenceReadRetainsRoot`, which directly invokes old handle-native lowering under the helper-only direct bridge and panics with `amd64: native GC lowering in helper-only bridge`. That invocation failed; the subsequent backend pass deliberately uses the documented applicable selection, not a claim that every backend test works with these experimental tags.

TinyGo 0.42 with the repository Go 1.27 compatibility patch and `-scheduler=tasks` passes native GC (3.935s) and wrapper GC (0.005s). The first invocation was blocked by its default cache under read-only `/home/agent/.cache`; rerunning with a writable `XDG_CACHE_HOME` passed. This is default portable collector coverage, not TinyGo support for the AMD64-only direct lowering. No benchmark or V8 parity claim is attached to this checkpoint.
