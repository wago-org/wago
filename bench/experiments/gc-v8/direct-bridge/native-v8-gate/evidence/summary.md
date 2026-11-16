# Full-protocol experimental direct pipeline versus V8

Source `b5eb2b37f51384f9fc86cb8da759a83194480816`. Wago uses explicit 512 KiB Eden and two 8 MiB old semispaces, with allocation elision disabled. Checked and trusted variants share identical source. Each fresh process performs 100 warmup invocations and 9 timed batches of 32 invocations. Three process rounds rotate workload and engine order. V8 uses `--no-liftoff`; exact versions and binary/artifact hashes are in raw JSON.

| Workload | Engine | Median ms/call | Process medians ms | Ratio to V8 | <=1.15x gate | Timed minor/full | Median peak RSS MiB |
|---|---|---:|---|---:|---|---|---:|
| struct-allocation-access | checked | 9.534 | 9.599, 9.534, 9.410 | 9.934x | MISS | 5172/94, 5172/94, 5172/94 | 37.37 |
| struct-allocation-access | trusted | 7.119 | 7.119, 7.571, 6.729 | 7.418x | MISS | 5172/94, 5172/94, 5172/94 | 37.58 |
| struct-allocation-access | v8 | 0.960 | 1.186, 0.960, 0.913 | 1.000x | reference | not instrumented | 105.25 |
| array-allocation-access | checked | 18.055 | 18.055, 18.999, 17.405 | 3.081x | MISS | 11776/512, 11776/512, 11776/512 | 38.25 |
| array-allocation-access | trusted | 15.928 | 14.868, 15.928, 16.553 | 2.718x | MISS | 11776/512, 11776/512, 11776/512 | 38.53 |
| array-allocation-access | v8 | 5.861 | 5.861, 5.837, 5.954 | 1.000x | reference | not instrumented | 105.12 |
| array-fill | checked | 65.010 | 66.640, 65.010, 64.233 | 8.834x | MISS | 11776/512, 11776/512, 11776/512 | 38.87 |
| array-fill | trusted | 70.149 | 70.149, 67.080, 78.581 | 9.533x | MISS | 11776/512, 11776/512, 11776/512 | 39.06 |
| array-fill | v8 | 7.359 | 7.500, 7.359, 7.125 | 1.000x | reference | not instrumented | 105.22 |
| array-copy-checksum | checked | 1.596 | 1.596, 1.475, 1.636 | 9.219x | MISS | 10/0, 10/0, 10/0 | 16.50 |
| array-copy-checksum | trusted | 1.067 | 1.125, 0.979, 1.067 | 6.164x | MISS | 10/0, 10/0, 10/0 | 16.57 |
| array-copy-checksum | v8 | 0.173 | 0.181, 0.167, 0.173 | 1.000x | reference | not instrumented | 44.44 |
| array-loop-copy-checksum | checked | 1.998 | 2.075, 1.987, 1.998 | 13.056x | MISS | 10/0, 10/0, 10/0 | 16.51 |
| array-loop-copy-checksum | trusted | 0.671 | 0.668, 0.686, 0.671 | 4.384x | MISS | 10/0, 10/0, 10/0 | 16.61 |
| array-loop-copy-checksum | v8 | 0.153 | 0.153, 0.154, 0.146 | 1.000x | reference | not instrumented | 44.37 |
| tree-survival | checked | 38.081 | 39.153, 36.251, 38.081 | 61.898x | MISS | 1181/6, 1181/6, 1181/6 | 38.09 |
| tree-survival | trusted | 36.456 | 36.456, 36.451, 37.951 | 59.257x | MISS | 1181/6, 1181/6, 1181/6 | 38.29 |
| tree-survival | v8 | 0.615 | 0.647, 0.593, 0.615 | 1.000x | reference | not instrumented | 72.75 |

## Outcome

All six workloads miss the <=1.15x target in both Wago modes. The target is not achieved. Trusted reads/access improve several end-to-end cases relative to checked mode, but fill remains slower and tree is dominated by unsupported native reference-field paths. The measured V8 version is 13.6.233.17-node.58 under Node 24.22.1. Retained-object workloads performed real major collections in every process; copying kernels performed only minor collections, as expected for their short-lived objects.

## Scope and caveats

This is an end-to-end experimental engine comparison, not an isolated collector-efficiency proof or the default Wago runtime. Retained exported roots constrain the latest iteration but do not prove identical internal allocation behavior in V8. Tree reference-field operations remain helper-heavy in this source. Every per-workload target miss is retained; no aggregate substitutes for the <=1.15x gate. Numerical checksums, exact artifacts and expected Wago allocation counts are verified for all six cases.

Compilation is excluded. Process RSS includes distinct engine/host overhead and only resident pages, not an equalized memory budget. V8 collection counts and copied/promoted bytes are not instrumented. Wago direct copied/promoted-byte and scan counters are also uninstrumented. Do not infer equivalent collection schedules, allocation-elision behavior in V8, peak pause guarantees or universal engine superiority from this corpus. Go host GC is invoked outside timing after warmup; V8 uses its ordinary heap policy without a forced GC. Worker builds/tests were held during the measurement window; host scheduling/frequency remain uncontrolled.
