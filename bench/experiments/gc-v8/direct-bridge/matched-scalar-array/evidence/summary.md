# Matched checked scalar-array experiment

Source `cf00d4ae060bd002f623cbb61503bec992f0fbfc`. Same source and collector, checked reference-store versus checked native scalar-array access. Both variants include final-array-child native reference stores. Eden is explicitly 512 KiB; both old semispaces remain 8 MiB each. Allocation elision is off. Three fresh processes per case/policy/variant, with 3 warmup and 5 checked timed calls each.

| Workload | Eden KiB | Variant | Median ms/call | Samples ms/call | Timed minor/full | Median VmHWM MiB | Median Go heap MiB |
|---|---:|---|---:|---|---|---:|---:|
| struct-allocation-access | 512 | checked_refstore | 11.230 | 11.170, 14.641, 11.230 | 90/2 | 36.49 | 25.16 |
| struct-allocation-access | 512 | scalar_arrays | 13.742 | 13.742, 15.684, 10.481 | 90/2 | 36.63 | 25.16 |
| array-allocation-access | 512 | checked_refstore | 194.148 | 250.350, 114.280, 194.148 | 205/9 | 36.50 | 25.18 |
| array-allocation-access | 512 | scalar_arrays | 79.226 | 78.278, 79.287, 79.226 | 205/9 | 36.64 | 25.18 |
| tree-survival | 512 | checked_refstore | 37.864 | 34.473, 78.978, 37.864 | 20/0 | 17.16 | 25.16 |
| tree-survival | 512 | scalar_arrays | 35.672 | 34.278, 35.672, 35.726 | 20/0 | 17.40 | 25.16 |

## Median elapsed-time differences

- struct-allocation-access, 512 KiB: +22.4%.
- array-allocation-access, 512 KiB: -59.2%.
- tree-survival, 512 KiB: -5.8%.

## Interpretation

Array access is consistently faster: every scalar-array sample is lower than every checked-reference-store sample, despite substantial baseline variance. The median reduction is 59.2% for this short experiment, not a universal speedup. Struct control samples overlap and its median is worse; tree samples overlap and the baseline has a large outlier. Neither control supports a speedup claim. Collection counts, expected object allocations and host allocation traffic match exactly, so the comparison does not trade away collection work or alter heap policy.

These exploratory medians must be read together with all samples, not as confidence intervals or V8 comparisons. This short protocol does not establish steady-state performance, peak pauses or V8 parity. Host scheduling and frequency remain uncontrolled. All successful probes verify exact artifact hashes, checksums, expected per-call allocation counts and actual collection. Raw JSON retains stdout/stderr, binary hashes, configurations and complete counters. Direct copied/promoted-byte and scanned-object counters are not instrumented.

## Host allocations per timed invocation

| Workload | Eden KiB | Variant | Median mallocs/call | Median allocated bytes/call |
|---|---:|---|---:|---:|
| struct-allocation-access | 512 | checked_refstore | 113.0 | 2409.6 |
| struct-allocation-access | 512 | scalar_arrays | 113.0 | 2409.6 |
| array-allocation-access | 512 | checked_refstore | 267.8 | 5710.4 |
| array-allocation-access | 512 | scalar_arrays | 267.8 | 5710.4 |
| tree-survival | 512 | checked_refstore | 39.6 | 1640.0 |
| tree-survival | 512 | scalar_arrays | 39.6 | 1640.0 |
