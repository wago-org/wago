# Bounded compact allocator diagnostic

Pin final qualified source before building. Same-source compact checked reads (`reads.test`, tag `wago_gc_compact_reads`) versus reads plus native allocation (`alloc.test`, tag `wago_gc_compact_alloc`). Refstore ABI3 is excluded. Both use 2 MiB Eden and two 8 MiB old semispaces.

Separate 128-warmup audits demonstrate actual collection for all six exact artifacts. Bounded diagnostic remains three warmups plus five timed calls, three rotating fresh-process rounds; copy may legitimately have zero collections during that interval, recorded explicitly after its separate collection audit. Hashes, checksums and expected object counts remain mandatory. No V8 gate or universal gain inference.

After timing, run separate Compile+Close footprint probes on both binaries. Record per-artifact code-size changes. Run allocator sentinel tests (`TestCompactNativeGCBumpPublicationAndFallback` and `TestCompactNativeGCBumpBoundedShapes`) separately: these prove roomy supported constructors avoid Go helper entry and fallback paths remain active. Fixture path selection does not prove every corpus allocation uses that path. Preserve controls/regressions with unknown mechanism rather than attributing them from timing alone.
