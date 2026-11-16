# Opt-in checked weighted SSE2 reduction

Tag `wago_gc_compact_weighted_sse2` implies the scalar weighted experiment and its prerequisite pipeline. Compare against `wago_gc_compact_weighted_sum` with identical collector settings and other tags. The default compiler is unchanged; no allocation is removed.

The exact bounded scalar weighted matcher, original checked first iteration, full-span guard and pinned index/total requirements are unchanged. Six clean XMM registers are required, with no spills or partial emission on pressure fallback. Remaining counts below four use the existing scalar path. Complete unaligned 16-byte loads use baseline SSE2 only; the exact scalar tail follows.

Unsigned PMULUDQ gives the same low 32 product bits as signed or wrapping Wasm i32 multiplication. Independent PADDD lanes 0 and 2 accumulate even/odd low products; discarded high-product lanes cannot carry into them. Only lanes 0 and 2 are reduced into the scalar accumulator. Full-span validation makes reassociation of these nontrapping modulo-32 operations valid. There are no array writes or alias-dependent reordering, and both canonical local homes are synchronized by the existing latch exit.

Source checkpoint prepared for independent review. Tests have not yet run and no speedup has been measured. Startup cost and six-register pressure can outweigh throughput benefits.
