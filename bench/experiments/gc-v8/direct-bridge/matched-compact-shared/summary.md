# Checked compact shared-check diagnostic

Runtime source `89ec24b7` (production `50c3065d`), driver `4ed5ef38`. Baseline checked compact loop + refstruct versus identical source adding shared_checks; no paired-loop mode. Explicit 2 MiB Eden and two 8 MiB old semispaces, allocation elision disabled. Sequential builds and exclusive worker timing window; no CPU affinity.

All 12 separate 128-warmup actual-GC audits and 36 bounded processes passed exact checksums/allocation counts. Bounded protocol: three warmups, five timed calls, three rotating fresh processes. Short copy measurements allow zero collections, separate audits do not.

| Case | Baseline ms (range) | Shared ms (range) | Ratio |
|---|---:|---:|---:|
| struct-allocation-access | 7.5569 (6.2897–8.2872) | 8.4031 (8.1596–8.7566) | 1.112× |
| array-allocation-access | 16.3765 (15.4768–17.5622) | 15.4297 (14.8018–82.8101) | 0.942× |
| array-fill | 19.8322 (19.0920–20.7523) | 20.0530 (18.3444–22.0325) | 1.011× |
| array-copy-checksum | 0.5640 (0.4996–0.6123) | 0.4117 (0.3893–0.4257) | 0.730× |
| array-loop-copy-checksum | 1.5130 (1.4556–3.4684) | 1.6932 (1.5445–3.6040) | 1.119× |
| tree-survival | 2.7239 (1.9951–3.1076) | 2.7696 (2.6911–3.6227) | 1.017× |

No reliable broad execution gain is established. Struct and scalar-copy regress; array has an 82.81 ms candidate outlier that is retained. Bulk-copy improves in each paired round but has unchanged code size and compile allocation counts, so attributing that improvement to shared checks is unsupported. Code-size equality is not byte identity. Large shared-host variance prevents fine-grained acceptance from these samples. No additional V8 gate is warranted for this slice; the full target remains unmet.

## Separate compile footprint

256 Compile+Close iterations per case, three rotating processes. Process medians:

| Case | Baseline/shared µs | Code bytes | Go bytes | Allocations |
|---|---:|---:|---:|---:|
| struct-allocation-access | 204.49/171.00 | 3183/3119 | 68131/68131 | 253/253 |
| array-allocation-access | 173.14/176.82 | 3439/3375 | 71987/71731 | 242/242 |
| array-fill | 221.99/239.60 | 3951/3823 | 76299/76299 | 246/246 |
| array-copy-checksum | 278.70/249.78 | 7535/7535 | 82852/82852 | 289/289 |
| array-loop-copy-checksum | 237.79/247.07 | 7535/7535 | 82852/82852 | 289/289 |
| tree-survival | 182.99/196.77 | 4015/4015 | 69731/69731 | 289/289 |

Shared checks reduce native code by 64 bytes for struct and array, 128 bytes for fill. Other cases retain the same size. Array compile bytes decrease by 256; allocation counts are unchanged. Compile timing noise precludes a speed claim.

Timed GC counts match between variants: struct 13 minor/0 major; array and fill 51/2; both copies 0/0; tree 3/0. Exact allocations per five-call batch remain 1,311,040 for struct/array/fill, 640 for copies, and 337,755 for tree. Raw JSON includes all individual counters, hashes, Go heap/allocation data and `/proc` RSS/HWM. Ignore inherited `maxrss_kib`; unimplemented copied/promoted/scan counters are not measured zero work.

## LICM site attribution carried forward

Separate codegen diagnostic identified checksum function 0 loop PC 3 (first access PC 16, local 0) and initialization functions 1/2 loop PC 24 (first access PC 38, local 1). The actual scalar-copy function 2 loop PC 87 is rejected by the two-receiver scanner gate; outer loops also reject. Instrumented and uninstrumented native hashes matched at 7,535 bytes. This diagnostic is not timed and does not make the scalar-copy loop itself optimized.
