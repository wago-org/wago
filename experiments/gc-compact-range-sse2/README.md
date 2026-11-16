# Guarded SSE2 scalar-transfer version

Tag `wago_gc_compact_range_sse2` implies the checked range-loop stage, NOT the independently selectable byte-stride stage. The matched baseline should use `wago_gc_compact_range_loop`; both may compose with `wago_gc_compact_refstruct`. Do not enable cohort diagnostic tags for timing.

The original checked first iteration and full-span predicates remain intact. Vector selection requires a clean XMM register, a complete 16-byte chunk remaining, and either identical source/destination bases or disjoint remaining touched byte ranges. The absolute pointer displacement is compared against widened remaining bytes. Dependent overlap, unavailable XMM, or short spans retain the ordered scalar path. Full 16-byte unaligned SSE2 loads/stores advance both induction and byte offset; a precise scalar tail follows. Final pin/frame-slot synchronization is unchanged. No allocation is removed and no AVX requirement is added.

Admission remains integer scalar widths 1/2/4/8 only. Arbitrary bit patterns are preserved, but this is NOT a claim that f32/f64 source operations receive this optimization. Float scanner/register admission remains deferred.

Independent actual source review found no blocker. Qualification fixtures include same-object alias, disjoint and adjacent touched spans, both dependent overlap directions, chunk boundaries/tails, near-bound starting indices, packed values, no-free-XMM fallback, legacy SSE2 disassembly, exact-artifact selection, and the existing null/bounds/partial-write/GC-root matrix. Private overlap fixtures fabricate bounded numeric interior starts solely inside allocated backing, run no collections, restore bytes before each invocation, and remove fabricated starts/global references before closing. They compare the entire arena against ordered scalar reference execution.

No performance benefit is claimed until matched timing.

Qualification (Go1.27.1, serial `-p 1`): backend `TestCompact(Range|PairLoop|Loop)` with regalloc/codegenstats passed0.009s; applicable `^(TestCompactGC|TestCompactNative|TestGCCompact)` Wago matrix with SSE2/refstruct/regalloc passed0.636s. Initial matrix also passed0.056s/0.700s before the explicit exact-artifact SSE assertion and result-length check. No qualification failures occurred.
