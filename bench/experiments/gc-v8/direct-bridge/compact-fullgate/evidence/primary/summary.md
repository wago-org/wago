# Full-protocol checked compact pipeline versus V8

Source `32c0ae8f372decd93566faf95c300cd2d1772bda`. Wago uses explicit 2048 KiB Eden and two 8 MiB old semispaces, with allocation elision disabled. Candidate combines fully checked compact allocation, numeric/reference access, fill/copy and reference-containing construction. No trust, LICM, compact8 or allocation elision. Each fresh process performs 100 warmup invocations and 9 timed batches of 32 invocations. Three process rounds rotate workload and engine order. V8 uses `--no-liftoff`; exact versions and binary/artifact hashes are in raw JSON.

| Workload | Engine | Median ms/call | Process medians ms | Ratio to V8 | <=1.15x gate | Timed minor/full | Median peak RSS MiB |
|---|---|---:|---|---:|---|---|---:|
| struct-allocation-access | candidate | 6.079 | 6.079, 6.444, 5.909 | 6.471x | MISS | 716/7, 716/7, 716/7 | 39.29 |
| struct-allocation-access | v8 | 0.939 | 0.869, 0.939, 0.950 | 1.000x | reference | not instrumented | 105.29 |
| array-allocation-access | candidate | 12.452 | 12.452, 12.351, 13.229 | 1.886x | MISS | 2901/125, 2901/125, 2901/125 | 39.36 |
| array-allocation-access | v8 | 6.601 | 5.690, 6.601, 6.956 | 1.000x | reference | not instrumented | 105.26 |
| array-fill | candidate | 17.171 | 17.171, 16.924, 17.376 | 2.272x | MISS | 2901/125, 2901/125, 2901/125 | 39.29 |
| array-fill | v8 | 7.557 | 7.609, 7.072, 7.557 | 1.000x | reference | not instrumented | 105.21 |
| array-copy-checksum | candidate | 0.841 | 1.008, 0.834, 0.841 | 4.863x | MISS | 3/0, 3/0, 3/0 | 18.52 |
| array-copy-checksum | v8 | 0.173 | 0.170, 0.207, 0.173 | 1.000x | reference | not instrumented | 44.46 |
| array-loop-copy-checksum | candidate | 2.068 | 2.172, 2.068, 1.960 | 13.873x | MISS | 3/0, 3/0, 3/0 | 18.70 |
| array-loop-copy-checksum | v8 | 0.149 | 0.149, 0.143, 0.161 | 1.000x | reference | not instrumented | 44.34 |
| tree-survival | candidate | 2.174 | 2.174, 1.995, 2.271 | 3.704x | MISS | 147/1, 147/1, 147/1 | 31.00 |
| tree-survival | v8 | 0.587 | 0.592, 0.585, 0.587 | 1.000x | reference | not instrumented | 72.80 |

## Audit protocol

Timed runs retain 100 warmups plus 9x32 calls for both policies. Audit-only warmups are 128 at 2 MiB and 100 at 512 KiB, ensuring actual copy-kernel collection. All 24 audits and 72 timing processes passed. The same candidate binary is used for both policies.

## Outcome

The per-workload table above is the acceptance gate; any miss means the corpus target is not achieved. The measured V8 version is 13.6.233.17-node.58 under Node 24.22.1. Actual collection counts are reported without assuming major collections occur at either nursery policy.

## Scope and caveats

This is an end-to-end experimental engine comparison, not an isolated collector-efficiency proof or the default Wago runtime. Retained exported roots constrain the latest iteration but do not prove identical internal allocation behavior in V8. Every per-workload target miss is retained; no aggregate substitutes for the <=1.15x gate. Numerical checksums, exact artifacts and expected Wago allocation counts are verified for all six cases.

Compilation is excluded. Process RSS includes distinct engine/host overhead and only resident pages, not an equalized memory budget. V8 collection counts and copied/promoted bytes are not instrumented. Wago direct copied/promoted-byte and scan counters are also uninstrumented. Do not infer equivalent collection schedules, allocation-elision behavior in V8, peak pause guarantees or universal engine superiority from this corpus. Go host GC is invoked outside timing after warmup; V8 uses its ordinary heap policy without a forced GC. Worker builds/tests were held during the measurement window; host scheduling/frequency remain uncontrolled.
