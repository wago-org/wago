# Direct nursery policy sensitivity

Source `ec8589562c0024cb6b779cd0596d8b12093e89ea`. One unchanged checked-refstore binary, allocation elision off. Eden varies explicitly; both old semispaces remain 8 MiB each. Three fresh processes per case/policy, each with 3 warmup calls and 5 checked timed calls. All 9 audit processes and 27 timing processes passed.

| Workload | Eden KiB | Median ms/call | Samples ms/call | Timed minor/full | Median VmHWM MiB | Median Go heap MiB |
|---|---:|---:|---|---|---:|---:|
| struct-allocation-access | 64 | 23.521 | 23.380, 23.856, 23.521 | 742/5 | 36.24 | 24.58 |
| struct-allocation-access | 512 | 11.220 | 10.032, 17.532, 11.220 | 90/2 | 36.48 | 25.16 |
| struct-allocation-access | 2048 | 8.195 | 10.804, 8.195, 7.940 | 23/0 | 24.02 | 27.42 |
| array-allocation-access | 64 | 241.132 | 262.770, 199.899, 241.132 | 1693/14 | 36.36 | 24.70 |
| array-allocation-access | 512 | 244.356 | 244.356, 259.693, 236.703 | 205/9 | 36.62 | 25.18 |
| array-allocation-access | 2048 | 157.003 | 154.881, 177.677, 157.003 | 51/2 | 38.38 | 27.42 |
| tree-survival | 64 | 43.391 | 43.391, 52.241, 35.586 | 165/0 | 18.01 | 24.56 |
| tree-survival | 512 | 37.787 | 37.787, 39.073, 34.011 | 20/0 | 17.30 | 25.15 |
| tree-survival | 2048 | 37.066 | 37.066, 36.108, 39.789 | 5/0 | 18.91 | 27.41 |

## Interpretation

- Struct is consistently faster with either larger nursery. Array is consistently faster at 2 MiB than at 64 KiB across these launches; 512 KiB does not establish an array improvement. Tree samples overlap substantially and do not establish a reliable speedup.
- This collector promotes every surviving nursery object; it does not yet have survivor spaces. A larger nursery changes when temporary objects die relative to collection and can avoid promotion, not merely reduce a fixed collection cost.
- The 2 MiB struct run performs no major collection in the short timed window. Its lower resident peak does not mean a lower memory budget: one old semispace can remain untouched. A longer steady-state/forced-major experiment is required before selecting a default.
- Go heap allocated rises from about 24.6 MiB to 27.4 MiB as the nursery and its metadata grow. Process VmHWM includes runtime/Go overhead and only resident pages; it is not reserved arena size.
- The experiment changes heap policy, not code generation. It is not comparable to the historical V8 timing protocol and does not establish V8 parity. Keep 64 KiB as the explicit control in subsequent compiler comparisons; 2 MiB is a useful separately labeled sensitivity configuration, not an adopted default.

## Host allocations per timed invocation

| Workload | Eden KiB | Median mallocs/call | Median allocated bytes/call |
|---|---:|---:|---:|
| struct-allocation-access | 64 | 902.6 | 19254.4 |
| struct-allocation-access | 512 | 113.0 | 2409.6 |
| struct-allocation-access | 2048 | 27.8 | 590.4 |
| array-allocation-access | 64 | 2065.4 | 44059.2 |
| array-allocation-access | 512 | 267.8 | 5710.4 |
| array-allocation-access | 2048 | 66.2 | 1409.6 |
| tree-survival | 64 | 360.2 | 14576.0 |
| tree-survival | 512 | 39.6 | 1640.0 |
| tree-survival | 2048 | 10.0 | 348.8 |

Exact artifact hashes, checksums, expected per-call allocation counts (262208 struct/array; 67551 tree), actual collection counters, binary hash, source version and complete stdout/stderr are retained in raw JSON. Direct copied/promoted-byte and scanned-object counters remain uninstrumented; their zeros do not mean zero work. All timed collection counts in each case/policy were identical across the three processes. Other worker builds/tests were held during this timing window; host scheduling/frequency remain uncontrolled.
