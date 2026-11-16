# Matched trusted reference-store experiment

Source `d1e0c024e2d2df2ed854c99afd5770778ab8530d`. Same source and collector, trusted reads (policy 1) versus trusted reference stores (policy 2). Eden policies are explicit: 512 KiB primary, 64 KiB retained control; both old semispaces remain 8 MiB each. Allocation elision is off. Three fresh processes per case/policy/variant, with 3 warmup and 5 checked timed calls each.

| Workload | Eden KiB | Variant | Median ms/call | Samples ms/call | Timed minor/full | Median VmHWM MiB | Median Go heap MiB |
|---|---:|---|---:|---|---|---:|---:|
| struct-allocation-access | 64 | trusted_read | 23.079 | 21.807, 23.079, 31.398 | 742/5 | 36.10 | 24.58 |
| struct-allocation-access | 64 | trusted_store | 19.987 | 19.808, 21.942, 19.987 | 742/5 | 36.00 | 24.58 |
| struct-allocation-access | 512 | trusted_read | 10.221 | 10.221, 11.430, 9.806 | 90/2 | 36.30 | 25.16 |
| struct-allocation-access | 512 | trusted_store | 10.058 | 9.062, 82.046, 10.058 | 90/2 | 36.27 | 25.16 |
| array-allocation-access | 64 | trusted_read | 241.925 | 202.630, 285.840, 241.925 | 1693/14 | 36.77 | 24.70 |
| array-allocation-access | 64 | trusted_store | 183.358 | 262.635, 183.358, 183.249 | 1693/14 | 36.12 | 24.70 |
| array-allocation-access | 512 | trusted_read | 172.284 | 292.323, 172.284, 169.402 | 205/9 | 36.58 | 25.18 |
| array-allocation-access | 512 | trusted_store | 239.112 | 168.266, 260.944, 239.112 | 205/9 | 36.73 | 25.18 |
| tree-survival | 64 | trusted_read | 38.501 | 63.891, 35.604, 38.501 | 165/0 | 17.89 | 24.56 |
| tree-survival | 64 | trusted_store | 37.991 | 38.905, 35.927, 37.991 | 165/0 | 17.67 | 24.56 |
| tree-survival | 512 | trusted_read | 36.881 | 35.867, 36.881, 37.527 | 20/0 | 17.41 | 25.16 |
| tree-survival | 512 | trusted_store | 36.328 | 35.934, 36.328, 42.048 | 20/0 | 17.18 | 25.16 |

## Median elapsed-time differences

- struct-allocation-access, 64 KiB: -13.4%.
- struct-allocation-access, 512 KiB: -1.6%.
- array-allocation-access, 64 KiB: -24.2%.
- array-allocation-access, 512 KiB: +38.8%.
- tree-survival, 64 KiB: -1.3%.
- tree-survival, 512 KiB: -1.5%.

## Interpretation

No convincing gain is established at the primary 512 KiB policy: struct medians are nearly equal but policy 2 has an 82 ms outlier; array samples have opposing large outliers; tree samples overlap. At 64 KiB, struct favors policy 2 in each paired round. Array-child reference stores remain on the helper path in this source, so noisy array differences must not be attributed to native array-child stores. Collection counts and host allocation traffic are identical within each case/policy. These results support retaining experimental status, not a general speedup claim.

These exploratory medians must be read together with all samples, not as confidence intervals or V8 comparisons. This short protocol does not establish steady-state performance, peak pauses or V8 parity. Host scheduling and frequency remain uncontrolled. All successful probes verify exact artifact hashes, checksums, expected per-call allocation counts and actual collection. Raw JSON retains stdout/stderr, binary hashes, configurations and complete counters. Direct copied/promoted-byte and scanned-object counters are not instrumented.

## Host allocations per timed invocation

| Workload | Eden KiB | Variant | Median mallocs/call | Median allocated bytes/call |
|---|---:|---|---:|---:|
| struct-allocation-access | 64 | trusted_read | 902.6 | 19254.4 |
| struct-allocation-access | 64 | trusted_store | 902.6 | 19254.4 |
| struct-allocation-access | 512 | trusted_read | 113.0 | 2409.6 |
| struct-allocation-access | 512 | trusted_store | 113.0 | 2409.6 |
| array-allocation-access | 64 | trusted_read | 2065.4 | 44059.2 |
| array-allocation-access | 64 | trusted_store | 2065.4 | 44059.2 |
| array-allocation-access | 512 | trusted_read | 267.8 | 5710.4 |
| array-allocation-access | 512 | trusted_store | 267.8 | 5710.4 |
| tree-survival | 64 | trusted_read | 360.2 | 14576.0 |
| tree-survival | 64 | trusted_store | 360.2 | 14576.0 |
| tree-survival | 512 | trusted_read | 39.6 | 1640.0 |
| tree-survival | 512 | trusted_store | 39.6 | 1640.0 |
