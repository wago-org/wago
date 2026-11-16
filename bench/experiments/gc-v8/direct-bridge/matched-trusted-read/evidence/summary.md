# Matched trusted scalar-read experiment

Source `5ec0e915eed4cdf20666d0dfef8058aa00a079c2`. Same source and collector, checked reference-store versus trusted scalar reads (policy 1). This does not enable trusted stores. Eden policies are explicit: 512 KiB primary, 64 KiB retained control; both old semispaces remain 8 MiB each. Allocation elision is off. Three fresh processes per case/policy/variant, with 3 warmup and 5 checked timed calls each.

| Workload | Eden KiB | Variant | Median ms/call | Samples ms/call | Timed minor/full | Median VmHWM MiB | Median Go heap MiB |
|---|---:|---|---:|---|---|---:|---:|
| struct-allocation-access | 64 | checked_refstore | 24.231 | 25.484, 24.231, 21.373 | 742/5 | 36.36 | 24.58 |
| struct-allocation-access | 64 | trusted_read | 20.885 | 22.056, 20.586, 20.885 | 742/5 | 36.03 | 24.58 |
| struct-allocation-access | 512 | checked_refstore | 10.277 | 10.277, 9.643, 11.697 | 90/2 | 36.46 | 25.16 |
| struct-allocation-access | 512 | trusted_read | 12.139 | 12.139, 9.704, 12.470 | 90/2 | 36.17 | 25.16 |
| array-allocation-access | 64 | checked_refstore | 185.499 | 503.766, 183.451, 185.499 | 1693/14 | 36.86 | 24.70 |
| array-allocation-access | 64 | trusted_read | 232.883 | 199.207, 257.078, 232.883 | 1693/14 | 36.32 | 24.70 |
| array-allocation-access | 512 | checked_refstore | 211.227 | 171.447, 238.246, 211.227 | 205/9 | 36.65 | 25.18 |
| array-allocation-access | 512 | trusted_read | 181.375 | 202.444, 172.905, 181.375 | 205/9 | 36.70 | 25.18 |
| tree-survival | 64 | checked_refstore | 35.960 | 33.950, 39.178, 35.960 | 165/0 | 17.90 | 24.56 |
| tree-survival | 64 | trusted_read | 36.163 | 34.493, 36.163, 41.043 | 165/0 | 18.28 | 24.56 |
| tree-survival | 512 | checked_refstore | 35.125 | 35.125, 37.305, 34.466 | 20/0 | 17.21 | 25.16 |
| tree-survival | 512 | trusted_read | 48.271 | 65.002, 48.271, 35.755 | 20/0 | 17.37 | 25.15 |

## Median elapsed-time differences

- struct-allocation-access, 64 KiB: -13.8%.
- struct-allocation-access, 512 KiB: +18.1%.
- array-allocation-access, 64 KiB: +25.5%.
- array-allocation-access, 512 KiB: -14.1%.
- tree-survival, 64 KiB: +0.6%.
- tree-survival, 512 KiB: +37.4%.

## Interpretation

No broad improvement is established. The 64 KiB struct samples favor trusted reads in each paired round, but the primary 512 KiB struct samples do not. Array samples have large opposing noise; tree provides no reliable gain and has large trusted outliers at 512 KiB. Keep the mode experimental rather than selecting it based on median-only wins. Collection counts and host allocation traffic are identical within each matched case/policy.

These exploratory medians must be read together with all samples, not as confidence intervals or V8 comparisons. This short protocol does not establish steady-state performance, peak pauses or V8 parity. Host scheduling and frequency remain uncontrolled. All successful probes verify exact artifact hashes, checksums, expected per-call allocation counts and actual collection. Raw JSON retains stdout/stderr, binary hashes, configurations and complete counters. Direct copied/promoted-byte and scanned-object counters are not instrumented.

## Host allocations per timed invocation

| Workload | Eden KiB | Variant | Median mallocs/call | Median allocated bytes/call |
|---|---:|---|---:|---:|
| struct-allocation-access | 64 | checked_refstore | 902.6 | 19254.4 |
| struct-allocation-access | 64 | trusted_read | 902.6 | 19254.4 |
| struct-allocation-access | 512 | checked_refstore | 113.0 | 2409.6 |
| struct-allocation-access | 512 | trusted_read | 113.0 | 2409.6 |
| array-allocation-access | 64 | checked_refstore | 2065.4 | 44059.2 |
| array-allocation-access | 64 | trusted_read | 2065.4 | 44059.2 |
| array-allocation-access | 512 | checked_refstore | 267.8 | 5710.4 |
| array-allocation-access | 512 | trusted_read | 267.8 | 5710.4 |
| tree-survival | 64 | checked_refstore | 360.2 | 14576.0 |
| tree-survival | 64 | trusted_read | 360.2 | 14576.0 |
| tree-survival | 512 | checked_refstore | 39.6 | 1640.0 |
| tree-survival | 512 | trusted_read | 39.6 | 1640.0 |
