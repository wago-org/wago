# Full-protocol experimental direct pipeline versus V8

Source `ba95cd3043274f6a26814872436ea82c28f5659d`. Wago uses explicit 2048 KiB Eden and two 8 MiB old semispaces, with allocation elision disabled. One candidate combines checked native fill with trusted numeric-array access, reference-struct access and pointer-free allocation. Each fresh process performs 100 warmup invocations and 9 timed batches of 32 invocations. Three process rounds rotate workload and engine order. V8 uses `--no-liftoff`; exact versions and binary/artifact hashes are in raw JSON.

| Workload | Engine | Median ms/call | Process medians ms | Ratio to V8 | <=1.15x gate | Timed minor/full | Median peak RSS MiB |
|---|---|---:|---|---:|---|---|---:|
| struct-allocation-access | candidate | 4.497 | 4.535, 4.208, 4.497 | 4.453x | MISS | 1275/23, 1275/23, 1275/23 | 38.71 |
| struct-allocation-access | v8 | 1.010 | 1.010, 1.047, 0.983 | 1.000x | reference | not instrumented | 105.14 |
| array-allocation-access | candidate | 8.084 | 7.986, 8.411, 8.084 | 1.392x | MISS | 2901/125, 2901/125, 2901/125 | 39.20 |
| array-allocation-access | v8 | 5.806 | 6.429, 5.680, 5.806 | 1.000x | reference | not instrumented | 105.27 |
| array-fill | candidate | 11.183 | 14.675, 11.183, 10.766 | 1.569x | MISS | 2901/125, 2901/125, 2901/125 | 39.21 |
| array-fill | v8 | 7.127 | 7.127, 7.281, 6.999 | 1.000x | reference | not instrumented | 105.11 |
| array-copy-checksum | candidate | 1.060 | 1.175, 1.021, 1.060 | 5.586x | MISS | 3/0, 3/0, 3/0 | 18.48 |
| array-copy-checksum | v8 | 0.190 | 0.185, 0.190, 0.238 | 1.000x | reference | not instrumented | 44.39 |
| array-loop-copy-checksum | candidate | 0.664 | 0.667, 0.664, 0.640 | 4.443x | MISS | 3/0, 3/0, 3/0 | 18.42 |
| array-loop-copy-checksum | v8 | 0.150 | 0.167, 0.139, 0.150 | 1.000x | reference | not instrumented | 44.35 |
| tree-survival | candidate | 1.427 | 1.354, 1.427, 1.450 | 2.280x | MISS | 293/3, 293/3, 293/3 | 38.97 |
| tree-survival | v8 | 0.626 | 0.658, 0.626, 0.591 | 1.000x | reference | not instrumented | 72.80 |

## Audit protocol

The timed protocol remains 100 warmups and 9 x 32 calls for both policies. The primary audit uses 100 warmups. The 2 MiB sensitivity audit uses 128 warmups because 100 copy-kernel calls allocate only 1,843,200 bytes, below the 2 MiB nursery; the original audit stopped solely on the no-collection guard, with its raw record preserved. No runtime/checksum failure occurred. The same candidate binary SHA is used for both policies. The audit-only driver adjustment happened after building ba95cd3 and does not change compiled source.

## Outcome

The per-workload table above is the acceptance gate; any miss means the corpus target is not achieved. The measured V8 version is 13.6.233.17-node.58 under Node 24.22.1. Actual collection counts are reported without assuming major collections occur at either nursery policy.

## Scope and caveats

This is an end-to-end experimental engine comparison, not an isolated collector-efficiency proof or the default Wago runtime. Retained exported roots constrain the latest iteration but do not prove identical internal allocation behavior in V8. Every per-workload target miss is retained; no aggregate substitutes for the <=1.15x gate. Numerical checksums, exact artifacts and expected Wago allocation counts are verified for all six cases.

Compilation is excluded. Process RSS includes distinct engine/host overhead and only resident pages, not an equalized memory budget. V8 collection counts and copied/promoted bytes are not instrumented. Wago direct copied/promoted-byte and scan counters are also uninstrumented. Do not infer equivalent collection schedules, allocation-elision behavior in V8, peak pause guarantees or universal engine superiority from this corpus. Go host GC is invoked outside timing after warmup; V8 uses its ordinary heap policy without a forced GC. Worker builds/tests were held during the measurement window; host scheduling/frequency remain uncontrolled.
