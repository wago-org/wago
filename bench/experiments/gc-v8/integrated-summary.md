# Integrated candidate vs V8: target not met

Candidate `bd7fe5286876e79db0e6af1e162b1ae60922d377` includes collector metadata,
native-codegen loop improvements, and corrected survivor-occupancy tenuring.
Go 1.27.1. V8 13.6.233.17-node.58 through Node 24.22.1, `--no-liftoff`.
Shared Linux amd64 AMD EPYC 9V74, no CPU/frequency isolation. Other assigned
workers confirmed idle before this build/timing window.

Three alternating-order fresh processes per engine/workload. Each uses 100 warmup
calls and nine batches of 32 calls on one instance. Table reports median of
process medians of batch-average invocation times. Allocation elision disabled
for Wago; other compiler optimizations active. Default GC configuration.
All 36 processes passed exact checksum checks. These are invocation times, not
collector pause times. No memory collection in this timing pass.

| Workload | Candidate µs | V8 µs | Candidate/V8 | ≤1.15× gate | Candidate process range µs | V8 process range µs |
|---|---:|---:|---:|---|---|---|
| struct-allocation-access | 4103.55 | 938.86 | 4.371× | FAIL | 3837.39–4226.59 | 862.19–991.95 |
| array-allocation-access | 7148.78 | 5676.37 | 1.259× | FAIL | 7008.67–7258.35 | 5595.80–6287.76 |
| array-fill | 6846.61 | 7036.75 | 0.973× | pass (observed) | 6761.67–7854.03 | 6892.12–7270.12 |
| array-copy-checksum | 321.85 | 174.71 | 1.842× | FAIL | 316.46–338.96 | 170.39–174.73 |
| array-loop-copy-checksum | 1288.54 | 144.57 | 8.913× | FAIL | 1277.24–1305.79 | 139.95–148.90 |
| tree-survival | 1895.07 | 613.59 | 3.089× | FAIL | 1863.77–2004.83 | 602.54–689.28 |

The per-case gate fails, so the candidate cannot be described as matching or
beating V8. Passing an individual noisy synthetic cell is not proof of a broad
speedup. These results must not be averaged with i31/cast microprobes to hide
allocation/copy failures. Raw samples: integrated-results.json. Collection-on
and GC correctness qualification are separate evidence. Application workloads,
long-lived major-collection behavior, matched memory and pause distributions,
and controlled native hardware remain necessary for broader conclusions.
