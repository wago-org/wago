# Recovery provenance

The execution environment was replaced after the original experiment. This reconstruction starts from verified remote commit `cc33d1037757988bf5e4a79eab4c59d38a26c1f4`.

All 62 SSE implementation, test, and README file blobs were reconstructed and checked against the pre-loss publication manifest. Every blob hash matches exactly. Three source blobs were retrieved from the connector by their previously uploaded hashes; the remaining text was reconstructed from retained task context and independently hash-checked.

This is a new commit with new recovery provenance, not a recreation of the previous commit identity. The README's qualification numbers describe the historical pre-loss run. Historical binaries and raw timing outputs were not recovered by this reconstruction. Fresh qualification is pending; prior results are not represented as fresh tests.

## Fresh recovered-source qualification

Requalified the recovered source on Go 1.27.1, serial `-p 1`, with tags `wago_gc_compact_range_sse2 wago_gc_compact_refstruct wago_regalloc_checks wago_codegenstats`:

- AMD64 backend `TestCompact(Range|PairLoop|Loop)`: PASS, 0.012s. Includes SSE instruction selection, no-free-XMM fallback, and range/pair/loop regressions.
- Wago `^(TestCompactGC|TestCompactNative|TestGCCompact)`: PASS, 1.169s. Includes vector chunk/tail, fabricated in-backing overlap controls, moving roots, null/bounds and applicable compact regressions.

These are fresh post-recovery tests of the exact restored source, not a fresh performance measurement. No benchmark was run. The official pinned corpus was linked from the recovered canonical workspace; no corpus change is part of this commit.
