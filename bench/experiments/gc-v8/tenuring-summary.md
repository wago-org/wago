# Tenuring feedback comparison

Baseline 3a8ac1f vs policy 3249bea; Go 1.27.1, shared EPYC 9V74 Linux amd64.
Three alternating-order fresh processes per engine/workload, 100 warmups and
nine batches of 32 calls. Default nursery/16 MiB old heap; allocation elision off.
All 24 processes passed checksums and actual collection assertions.
Workers confirmed idle; shared host scheduling/frequency still uncontrolled.

| Workload | Engine | Median ms | Process median range ms | Peak RSS MiB | Timed minors | Timed promoted MiB |
|---|---|---:|---|---:|---:|---:|
| struct-allocation-access | baseline | 4.074 | 3.898–4.417 | 22.13 | 1349 | 0.000 |
| struct-allocation-access | policy | 4.081 | 4.028–4.213 | 22.31 | 1349 | 0.000 |
| array-allocation-access | baseline | 7.673 | 7.616–8.072 | 27.76 | 3026 | 0.000 |
| array-allocation-access | policy | 6.842 | 6.802–7.468 | 23.93 | 6144 | 0.000 |
| array-fill | baseline | 7.749 | 7.613–7.842 | 27.13 | 3026 | 0.000 |
| array-fill | policy | 6.740 | 6.735–7.424 | 23.56 | 6144 | 0.000 |
| tree-survival | baseline | 2.117 | 2.097–2.367 | 67.01 | 396 | 7.179 |
| tree-survival | policy | 1.861 | 1.815–1.867 | 22.43 | 396 | 0.000 |

RSS is getrusage process lifetime peak, including compilation and Go runtime.
Timings are invocation latency, not collector pause. No full collections occurred.
These limited synthetic results support further validation of the policy fix,
not broad application gains or V8 parity. Raw samples: tenuring-results.json.
