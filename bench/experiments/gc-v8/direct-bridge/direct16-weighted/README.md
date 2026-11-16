# Fresh direct16 weighted attribution protocol

Completed against qualified runtime a2e066d26d0555e03e2831bd359da1d5f2a51b13
after explicit CPU release. See summary.md and evidence/ for all results.

Three same-source binaries: baseline.test, weighted.test and sse2.test. Baseline
uses `wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc`. Weighted adds
`wago_gc_direct_weighted_sum`; SSE2 adds `wago_gc_direct_weighted_sse2` instead.
All use runtime mode DIRECT, age2 OFF, no compact tags or diagnostic tags.
Explicit direct_array_alloc is redundant with trusted_alloc/fill_sse2, but is
retained identically in all variants for a clear constructor-stage configuration.
No direct/compact ABI aliases are introduced. Elision stays OFF.

Young nursery is 2 MiB, two old semispaces are 8 MiB each. Exact six artifacts,
SHA/checksum/allocation checks, actual collections and native direct receipt
installation must be verified before timing. A receipt failure aborts the run.

First audit each case/variant with 128 warmups and no timed calls, requiring
collection. Then bounded three warmups + five calls across three rotating fresh
process rounds. Report this stage before full 100 warmups + nine batches of
32 calls, three rotating rounds against fresh Node --no-liftoff. Separate
compile+close measurements: 256 iterations, three alternating processes, native
code bytes/Go B per op/allocations per op, and noisy compilation time. Capture
Go heap and proc VmHWM, not inherited ru_maxrss. No timing diagnostics.

This is compiler attribution within direct16, NOT an isolated direct-versus-
compact layout comparison: direct16 still lacks compact paired/range transfer.
Do not compare historical different-host results as a baseline. V8 internal
allocation elision remains unobserved. Retain every case, regression, control,
and per-round sample; no aggregate can establish the <=1.15x per-case target.
