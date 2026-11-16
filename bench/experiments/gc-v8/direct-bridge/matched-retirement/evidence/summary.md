# Matched retirement traversal evidence

Measured source `c51bfc5`, runtime `d68b1f5`. Three same-source best-SSE2-pipeline variants differ only in retirement traversal: header walk, always start-map, or adaptive previous-cycle mean stride <=48 bytes. Contemporaneous V8 provides an end-to-end gate, not isolated collector efficiency. Explicit Eden 2 MiB, two 8 MiB old semispaces. All 24 audits and 72 timing processes passed. Audits use 128 warmup calls; timed processes use 100 warmups and nine batches of 32 calls, three rotating rounds.

| Case | Variant | Median ms | Process medians ms | Ratio to V8 | Timed minor/full | Peak RSS MiB | Go heap MiB |
|---|---|---:|---|---:|---|---:|---:|
| struct-allocation-access | headers | 4.0414 | 4.0414, 3.8149, 4.1430 | 4.061x | 1275/23 | 38.78 | 27.58 |
| struct-allocation-access | startmap | 3.4786 | 3.4786, 3.2963, 3.4988 | 3.495x | 1275/23 | 39.00 | 27.58 |
| struct-allocation-access | adaptive | 3.5685 | 3.6855, 3.3171, 3.5685 | 3.585x | 1275/23 | 38.68 | 27.58 |
| struct-allocation-access | v8 | 0.9953 | 1.0638, 0.9953, 0.9437 | 1.000x | unobserved | 105.29 | n/a |
| array-allocation-access | headers | 7.8113 | 8.4444, 7.7716, 7.8113 | 1.333x | 2901/125 | 39.50 | 27.80 |
| array-allocation-access | startmap | 7.8214 | 8.0911, 7.5883, 7.8214 | 1.335x | 2901/125 | 39.43 | 27.80 |
| array-allocation-access | adaptive | 7.8586 | 8.0234, 7.8201, 7.8586 | 1.341x | 2901/125 | 39.41 | 27.80 |
| array-allocation-access | v8 | 5.8607 | 6.0580, 5.8607, 5.7480 | 1.000x | unobserved | 105.16 | n/a |
| array-fill | headers | 9.3381 | 9.5328, 9.3381, 9.0211 | 1.324x | 2901/125 | 39.48 | 27.80 |
| array-fill | startmap | 9.1755 | 9.2264, 8.6984, 9.1755 | 1.301x | 2901/125 | 39.51 | 27.80 |
| array-fill | adaptive | 8.9981 | 8.9981, 9.7934, 8.8259 | 1.275x | 2901/125 | 39.50 | 27.80 |
| array-fill | v8 | 7.0546 | 6.7965, 7.0546, 7.4323 | 1.000x | unobserved | 105.27 | n/a |
| array-copy-checksum | headers | 0.3686 | 0.3686, 0.4282, 0.3225 | 2.084x | 3/0 | 18.50 | 27.41 |
| array-copy-checksum | startmap | 0.3703 | 0.3657, 0.3875, 0.3703 | 2.093x | 3/0 | 18.47 | 27.41 |
| array-copy-checksum | adaptive | 0.3725 | 0.3299, 0.3725, 0.3943 | 2.106x | 3/0 | 18.46 | 27.41 |
| array-copy-checksum | v8 | 0.1769 | 0.1690, 0.1834, 0.1769 | 1.000x | unobserved | 44.34 | n/a |
| array-loop-copy-checksum | headers | 0.6290 | 0.6227, 0.6839, 0.6290 | 4.266x | 3/0 | 18.54 | 27.41 |
| array-loop-copy-checksum | startmap | 0.6374 | 0.6374, 0.6545, 0.6229 | 4.323x | 3/0 | 18.60 | 27.41 |
| array-loop-copy-checksum | adaptive | 0.6220 | 0.6220, 0.6646, 0.6006 | 4.218x | 3/0 | 18.50 | 27.41 |
| array-loop-copy-checksum | v8 | 0.1475 | 0.1357, 0.1620, 0.1475 | 1.000x | unobserved | 44.35 | n/a |
| tree-survival | headers | 1.2493 | 1.1457, 1.4392, 1.2493 | 2.195x | 293/3 | 39.26 | 27.55 |
| tree-survival | startmap | 1.1908 | 1.1908, 1.2008, 1.0808 | 2.092x | 293/3 | 39.33 | 27.55 |
| tree-survival | adaptive | 1.2008 | 1.0537, 1.2008, 1.2193 | 2.110x | 293/3 | 39.36 | 27.55 |
| tree-survival | v8 | 0.5692 | 0.5627, 0.5692, 0.5702 | 1.000x | unobserved | 72.66 | n/a |

## Interpretation

Struct improves consistently: always-map is 13.9% lower and adaptive is 11.7% lower in median than headers, with every map/adaptive sample below every header sample. Other cases overlap or have mixed paired-round directions; no general speedup is established there. This supports the struct retirement opportunity without proving always-map is appropriate for all object densities or phase changes. Previous-cycle adaptation can lag workload transitions. The independent crossover microbenchmarks remain relevant to policy selection.

Every workload still misses the <=1.15x V8 gate for all three variants. Exact artifact hashes, checksums and object-allocation counts passed; collection counts are retained above. No aggregate substitutes for misses. V8 uses --no-liftoff (Node24.22.1/V8 13.6.233.17-node.58); its internal allocation/elision and collection behavior are unobserved. Resident memory and Go heap are not equalized engine budgets. Direct copied/promoted and scan counters remain uninstrumented. Compilation is excluded; host Go GC runs outside timing after warmup. Worker builds/tests were excluded from this window, but host scheduling/frequency remain uncontrolled.
