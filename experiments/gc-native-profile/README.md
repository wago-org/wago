# Direct bridge native profiling metadata qualification

Partial qualification: four of six pass; copy cases fail emitted-byte parity. See `evidence/summary.md`. Build the exact seven direct
bridge tags plus `wago_profile`, then run `TestGCNativeProfileProbe` with
`GC_NATIVE_PROFILE_OUT` naming a new output directory. No sampling tools are
invoked. The test compares profiling disabled/enabled inside that binary for all
six GC artifacts, preserving allocation elision off, nursery 2 MiB, two old
semispaces of 8 MiB, and 128 checked calls per mode. It verifies compiled and
loaded native-byte parity, matching actual collector counts, nonzero collections,
source/site/region bounds, and monotonic matched load/retire lifetimes.

Receipts export native bytes, source maps, compiler statistics and image journal
for diagnostic use. Coverage counts describe bytes, NOT CPU hotness. Unmapped
ranges stay unknown. These process addresses and lifetimes cannot symbolize a
future process: later native sampling must collect a new contemporaneous journal.
No attempt is made to recover native PCs discarded into Go `_ExternalCode`.
Profiler-on/off parity in one `wago_profile` binary does not independently prove
cross-build parity against a binary compiled without that build tag.

Exact tags:

```
wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum,wago_profile
```
