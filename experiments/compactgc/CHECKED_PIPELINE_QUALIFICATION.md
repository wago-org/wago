# Checked compact native pipeline qualification

Runtime source: `c3ede430`. Modes: `wago_gc_compact_copy wago_gc_compact_refstruct`.
This combines checked compact numeric/reference reads, bounded constructors, reference stores, numeric array operations, SSE2 fill and overlap-safe copy. It does not enable trusted access, allocation elision, compact8, or loop-resolution caching.

## Verified checks

Go 1.27.1, serial package builds:

- Applicable compact runtime/private receipt tests with `wago_regalloccheck`: pass, 1.122 s.
- Unfiltered default Wago suite: pass, 42.757 s.
- Default native GC, checked wrapper, AMD64 backend, direct core and compact core: pass (2.183 / 0.003 / 6.427 / 0.022 / 0.016 s).
- Native `wagodebug`: pass, 1.906 s.
- TinyGo 0.42 native GC and wrapper, task scheduler: pass (2.456 / 0.003 s).
- Build-constraint exclusivity regression: pass. It evaluates absent, individual, pairwise and all experimental mode tags, including numbered tags.

An initial composed build failed before execution because an integration conflict placed the reference-constructor mode outside the helper-only disabled guard's negation. The guard now exactly complements the enabled mode union; the added regression covers this failure. No runtime result was claimed for the failed build.

Experimental runtime tests cover only the explicitly admitted compact feature set. The unfiltered default suite is not a claim that compact mode supports imports, tables, exceptions or other rejected features. Performance qualification of this combined source is separate. The requested V8 performance target remains unverified and has not been achieved by earlier measured pipelines.

## Checked loop and shared-view composition

Runtime source `50c3065d` adds lazy checked single-receiver loop certificates and separately selectable reuse of already-validated common view metadata within a reference store. All operand-specific checks and logical bounds remain. No trusted compact mode or allocation elision is enabled.

Serial qualification passed with `wago_gc_compact_loop wago_gc_compact_refstruct wago_gc_compact_shared_checks wago_regalloccheck`: applicable runtime 0.579 s; build-constraint exclusivity 0.006 s. Unfiltered default Wago 40.296 s, native GC 2.166 s, wrapper 0.015 s, AMD64 backend 6.293 s, direct core 0.020 s, compact core 0.013 s all passed. Native debug passed 2.009 s; TinyGo native/wrapper passed 2.590/0.004 s. No failures in this composed qualification.

The matched loop evidence retains a 10.022x scalar-copy/V8 miss and an unexplained array control regression. Shared-view performance is not yet measured. These test passes are not a performance-target completion claim.

## Paired checked loop checkpoint

Runtime `7483b3e0`, evidence through `879f4137`: the opt-in paired certificate caches two checked receivers for exactly two static accesses while retaining lazy validation at each original site, per-access length/bounds checks and the existing compiler-state footprint. Canonical paired/reference-constructor/shared-view composition with regalloc checks passed 0.622 s; mode-exclusivity passed 0.006 s.

Unfiltered default Wago 40.394 s, native GC 2.018 s, wrapper 0.003 s, AMD64 backend 6.519 s, direct core 0.020 s and compact core 0.012 s passed. Native debug passed 1.958 s; TinyGo native/wrapper passed 2.334/0.003 s. No failures in these runs.

Matched scalar-copy timing improved in every paired round, but its full-protocol result remains 0.6463 ms versus V8 0.1444 ms (4.476x, target missed). Bulk-copy and other control regressions are retained in the evidence. Both copy artifacts contain both kernels; code-size growth alone cannot establish which executed instructions changed. Exact function-site diagnostics are separate and pending. This checkpoint is not a blanket performance recommendation or target completion.

## Guarded range latch checkpoint

Runtime `d0c35039`, measured evidence through `697706d7`: exact transfer-loop specialization retains the first checked iteration, branches to the original checked loop when full-span predicates fail, and preserves ordered scalar alias semantics. Canonical combined range/reference-constructor/shared-view runtime with regalloc passed 0.753 s; mode-exclusivity passed 0.008 s.

Unfiltered default Wago 43.000 s, native GC 2.207 s, wrapper 0.003 s, AMD64 backend 7.168 s, direct core 0.025 s and compact core 0.014 s passed. Native debug passed 2.176 s; TinyGo native/wrapper passed 2.491/0.008 s. No failures.

Matched scalar timing improves every pair, but full scalar-copy remains 0.4383 ms versus V8 0.1474 ms (2.974x, missed target). Bulk and tree control regressions and outliers remain in evidence. No blanket enablement or all-workload target claim is made.
