# Full-protocol checked compact pipeline versus V8

Source `32c0ae8f372decd93566faf95c300cd2d1772bda`. Wago uses explicit 512 KiB Eden and two 8 MiB old semispaces, with allocation elision disabled. Candidate combines fully checked compact allocation, numeric/reference access, fill/copy and reference-containing construction. No trust, LICM, compact8 or allocation elision. Each fresh process performs 100 warmup invocations and 9 timed batches of 32 invocations. Three process rounds rotate workload and engine order. V8 uses `--no-liftoff`; exact versions and binary/artifact hashes are in raw JSON.

| Workload | Engine | Median ms/call | Process medians ms | Ratio to V8 | <=1.15x gate | Timed minor/full | Median peak RSS MiB |
|---|---|---:|---|---:|---|---|---:|
| struct-allocation-access | candidate | 8.402 | 8.402, 7.990, 8.778 | 8.622x | MISS | 2881/29, 2881/29, 2881/29 | 37.72 |
| struct-allocation-access | v8 | 0.974 | 0.976, 0.974, 0.971 | 1.000x | reference | not instrumented | 105.30 |
| array-allocation-access | candidate | 22.002 | 21.050, 22.002, 22.293 | 3.742x | MISS | 11776/512, 11776/512, 11776/512 | 38.67 |
| array-allocation-access | v8 | 5.879 | 5.935, 5.766, 5.879 | 1.000x | reference | not instrumented | 105.26 |
| array-fill | candidate | 26.740 | 23.817, 26.833, 26.740 | 3.672x | MISS | 11776/512, 11776/512, 11776/512 | 38.62 |
| array-fill | v8 | 7.282 | 7.282, 7.020, 7.526 | 1.000x | reference | not instrumented | 105.23 |
| array-copy-checksum | candidate | 0.913 | 0.876, 0.913, 1.042 | 5.007x | MISS | 10/0, 10/0, 10/0 | 17.00 |
| array-copy-checksum | v8 | 0.182 | 0.182, 0.201, 0.168 | 1.000x | reference | not instrumented | 44.42 |
| array-loop-copy-checksum | candidate | 1.980 | 1.980, 1.923, 2.000 | 12.973x | MISS | 10/0, 10/0, 10/0 | 16.82 |
| array-loop-copy-checksum | v8 | 0.153 | 0.139, 0.159, 0.153 | 1.000x | reference | not instrumented | 44.26 |
| tree-survival | candidate | 2.521 | 2.521, 2.509, 3.046 | 3.685x | MISS | 591/2, 591/2, 591/2 | 37.04 |
| tree-survival | v8 | 0.684 | 0.577, 0.684, 0.850 | 1.000x | reference | not instrumented | 72.79 |

## Audit protocol

Timed runs retain 100 warmups plus 9x32 calls for both policies. Audit-only warmups are 128 at 2 MiB and 100 at 512 KiB, ensuring actual copy-kernel collection. All 24 audits and 72 timing processes passed. The same candidate binary is used for both policies.

## Outcome

The per-workload table above is the acceptance gate; any miss means the corpus target is not achieved. The measured V8 version is 13.6.233.17-node.58 under Node 24.22.1. Actual collection counts are reported without assuming major collections occur at either nursery policy.

## Scope and caveats

This is an end-to-end experimental engine comparison, not an isolated collector-efficiency proof or the default Wago runtime. Retained exported roots constrain the latest iteration but do not prove identical internal allocation behavior in V8. Every per-workload target miss is retained; no aggregate substitutes for the <=1.15x gate. Numerical checksums, exact artifacts and expected Wago allocation counts are verified for all six cases.

Compilation is excluded. Process RSS includes distinct engine/host overhead and only resident pages, not an equalized memory budget. V8 collection counts and copied/promoted bytes are not instrumented. Wago direct copied/promoted-byte and scan counters are also uninstrumented. Do not infer equivalent collection schedules, allocation-elision behavior in V8, peak pause guarantees or universal engine superiority from this corpus. Go host GC is invoked outside timing after warmup; V8 uses its ordinary heap policy without a forced GC. Worker builds/tests were held during the measurement window; host scheduling/frequency remain uncontrolled.
