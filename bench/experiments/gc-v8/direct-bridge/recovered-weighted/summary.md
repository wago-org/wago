# Fresh weighted reduction comparison and V8 gate

Qualified runtime 79add634c9f29ceb65f0cc46014b6286c1e387db, evidence driver 8eb7286e. Same-source SSE+refstruct baseline versus weighted_sum+refstruct on Intel Xeon Platinum 8573C, Go 1.27.1. Promote-all collector, 2 MiB active nursery and two 8 MiB old semispaces in both; age2/shared/stride/cohort/shadow/metrics OFF. Allocation elision OFF. Fresh baseline and V8 measured here, not compared with historical AMD runs.

All 12 actual-GC audits, 36 bounded processes and 54 full processes passed checksums/exact allocation counts. Full protocol 100 separate warmups + 9 batches of 32 calls, 3 rotating fresh processes. Node 24.22.1 / V8 13.6.233.17-node.58 --no-liftoff. GOMAXPROCS 1; exclusive worker timing window, no affinity. V8 internal allocation/elision not observed; engine-level comparison only.

| Case | SSE ms | Weighted ms | V8 ms | Weighted/SSE | Weighted/V8 | Gate |
|---|---:|---:|---:|---:|---:|---|
| struct-allocation-access | 7.8124 | 7.5774 | 1.3576 | 0.970× | 5.582× | MISS |
| array-allocation-access | 17.4430 | 18.5471 | 7.9714 | 1.063× | 2.327× | MISS |
| array-fill | 19.3138 | 20.3757 | 10.1710 | 1.055× | 2.003× | MISS |
| array-copy-checksum | 0.3767 | 0.2643 | 0.1646 | 0.702× | 1.606× | MISS |
| array-loop-copy-checksum | 0.4612 | 0.2742 | 0.1733 | 0.595× | 1.582× | MISS |
| tree-survival | 2.7048 | 2.5871 | 0.8354 | 0.957× | 3.097× | MISS |

Both copy exports improve in every paired full-protocol round. Weighted checksumming reduces work shared by both exports; this is not a new collector gain. Exact weighted selection/regression qualification preceded these runs. All six cases still miss 1.15×; do not claim V8 parity. Array/fill median regressions are retained despite unchanged native code sizes; the controls are noisy and do not establish causality.

Timed collection counts match across variants and all rounds: struct 716 minor/7 full; array/fill 2901/125; copies 3/0; tree 147/1. Allocation counts remain exact. Generic promoted/scan counters are uninstrumented and not evidence of zero work.

## Compile footprint

Separate 256-iteration Compile+Close runs, 3 rotating processes.

| Case | SSE/weighted µs | Code bytes | Go bytes/op | Allocations/op |
|---|---:|---:|---:|---:|
| struct-allocation-access | 227.40/243.12 | 3183/3183 | 68131/68131 | 253/253 |
| array-allocation-access | 220.86/221.32 | 3439/3439 | 71987/71987 | 242/242 |
| array-fill | 249.40/215.02 | 3951/3951 | 76300/76299 | 246/246 |
| array-copy-checksum | 336.47/318.68 | 7727/7791 | 82852/84260 | 289/288 |
| array-loop-copy-checksum | 324.38/352.42 | 7727/7791 | 82852/84260 | 289/288 |
| tree-survival | 203.52/241.71 | 4015/4015 | 69731/69731 | 289/289 |

Copies add 64 native bytes and 1,408 Go compile bytes, while allocation count decreases by 1. Other code sizes/allocation counts unchanged; compile timing variance precludes a compile-speed claim.

## Memory and bounded controls

Full-run median process HWM/Go HeapAlloc (MiB):

| Case | SSE HWM/heap | Weighted HWM/heap |
|---|---:|---:|
| struct-allocation-access | 38.76/27.50 | 38.70/27.51 |
| array-allocation-access | 39.46/27.80 | 39.41/27.80 |
| array-fill | 39.46/27.80 | 39.51/27.80 |
| array-copy-checksum | 18.45/27.41 | 18.35/27.41 |
| array-loop-copy-checksum | 18.32/27.41 | 18.35/27.41 |
| tree-survival | 31.21/27.48 | 31.14/27.48 |

Bounded preliminary runs remain separate (3 warmups/5 timed calls/3 rounds); raw sample ranges and every outlier are retained. Use /proc HWM rather than inherited maxrss_kib. No substantial backing-memory improvement is expected from this compiler change. Raw files preserve binary/source/artifact hashes, per-process samples, complete configuration and counters. No default-promotion or all-six acceptance claim is made.
