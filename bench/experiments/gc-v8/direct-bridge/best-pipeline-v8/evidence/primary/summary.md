# Full-protocol experimental direct pipeline versus V8

Source `ba95cd3043274f6a26814872436ea82c28f5659d`. Wago uses explicit 512 KiB Eden and two 8 MiB old semispaces, with allocation elision disabled. One candidate combines checked native fill with trusted numeric-array access, reference-struct access and pointer-free allocation. Each fresh process performs 100 warmup invocations and 9 timed batches of 32 invocations. Three process rounds rotate workload and engine order. V8 uses `--no-liftoff`; exact versions and binary/artifact hashes are in raw JSON.

| Workload | Engine | Median ms/call | Process medians ms | Ratio to V8 | <=1.15x gate | Timed minor/full | Median peak RSS MiB |
|---|---|---:|---|---:|---|---|---:|
| struct-allocation-access | candidate | 6.810 | 6.809, 6.810, 7.160 | 7.114x | MISS | 5172/94, 5172/94, 5172/94 | 37.46 |
| struct-allocation-access | v8 | 0.957 | 0.957, 0.924, 1.073 | 1.000x | reference | not instrumented | 105.31 |
| array-allocation-access | candidate | 15.099 | 14.627, 15.417, 15.099 | 2.676x | MISS | 11776/512, 11776/512, 11776/512 | 38.38 |
| array-allocation-access | v8 | 5.643 | 5.540, 5.643, 6.133 | 1.000x | reference | not instrumented | 105.23 |
| array-fill | candidate | 17.776 | 18.228, 17.776, 17.759 | 2.610x | MISS | 11776/512, 11776/512, 11776/512 | 38.38 |
| array-fill | v8 | 6.812 | 6.812, 7.228, 6.787 | 1.000x | reference | not instrumented | 105.26 |
| array-copy-checksum | candidate | 1.107 | 1.020, 1.107, 1.532 | 6.106x | MISS | 10/0, 10/0, 10/0 | 16.64 |
| array-copy-checksum | v8 | 0.181 | 0.179, 0.181, 0.186 | 1.000x | reference | not instrumented | 44.45 |
| array-loop-copy-checksum | candidate | 0.657 | 0.655, 0.657, 0.668 | 4.322x | MISS | 10/0, 10/0, 10/0 | 16.70 |
| array-loop-copy-checksum | v8 | 0.152 | 0.152, 0.146, 0.376 | 1.000x | reference | not instrumented | 44.36 |
| tree-survival | candidate | 1.617 | 1.601, 1.617, 1.729 | 2.832x | MISS | 1181/6, 1181/6, 1181/6 | 37.55 |
| tree-survival | v8 | 0.571 | 0.571, 0.559, 0.631 | 1.000x | reference | not instrumented | 72.77 |

## Audit protocol

The timed protocol remains 100 warmups and 9 x 32 calls for both policies. The primary audit uses 100 warmups. The 2 MiB sensitivity audit uses 128 warmups because 100 copy-kernel calls allocate only 1,843,200 bytes, below the 2 MiB nursery; the original audit stopped solely on the no-collection guard, with its raw record preserved. No runtime/checksum failure occurred. The same candidate binary SHA is used for both policies. The audit-only driver adjustment happened after building ba95cd3 and does not change compiled source.

## Outcome

The per-workload table above is the acceptance gate; any miss means the corpus target is not achieved. The measured V8 version is 13.6.233.17-node.58 under Node 24.22.1. Actual collection counts are reported without assuming major collections occur at either nursery policy.

## Scope and caveats

This is an end-to-end experimental engine comparison, not an isolated collector-efficiency proof or the default Wago runtime. Retained exported roots constrain the latest iteration but do not prove identical internal allocation behavior in V8. Every per-workload target miss is retained; no aggregate substitutes for the <=1.15x gate. Numerical checksums, exact artifacts and expected Wago allocation counts are verified for all six cases.

Compilation is excluded. Process RSS includes distinct engine/host overhead and only resident pages, not an equalized memory budget. V8 collection counts and copied/promoted bytes are not instrumented. Wago direct copied/promoted-byte and scan counters are also uninstrumented. Do not infer equivalent collection schedules, allocation-elision behavior in V8, peak pause guarantees or universal engine superiority from this corpus. Go host GC is invoked outside timing after warmup; V8 uses its ordinary heap policy without a forced GC. Worker builds/tests were held during the measurement window; host scheduling/frequency remain uncontrolled.
