# Checked compact loop diagnostic and scalar-copy gate

Runtime source `05fd9b55`; evidence driver `23c77c16`. Same-source fully checked baseline (`compact_copy` + `compact_refstruct`) versus lazy loop candidate (`compact_loop` + `compact_refstruct`). Build tags carry the `wago_gc_` prefix. Allocation elision disabled. Explicit 2 MiB nursery and two 8 MiB old semispaces. No shared-checks patch, trusted mode, or compact8.

Twelve separate 128-warmup audits passed actual-collection and checksum checks. Bounded measurements use three warmups and five timed calls, three rotating fresh processes per variant/case. All 36 passed exact allocation counts/checksums. These are exploratory samples, not the full steady-state protocol. Copy bounded samples legitimately collect zero times; audits verify collection separately.

| Case | Baseline ms (range) | Loop ms (range) | Loop / baseline | Timed minor/full |
|---|---:|---:|---:|---:|
| struct-allocation-access | 5.8996 (5.6541–7.1731) | 6.2914 (6.2487–6.7584) | 1.066× | 13/0 |
| array-allocation-access | 12.5875 (12.4154–13.1876) | 25.4600 (13.1079–37.8923) | 2.023× | 51/2 |
| array-fill | 14.3146 (13.5022–17.8525) | 14.5235 (13.8893–17.9113) | 1.015× | 51/2 |
| array-copy-checksum | 1.0017 (0.8523–2.1676) | 0.3913 (0.3500–0.5118) | 0.391× | 0/0 |
| array-loop-copy-checksum | 2.0183 (2.0010–2.5348) | 1.4983 (1.4604–1.6682) | 0.742× | 0/0 |
| tree-survival | 1.9609 (1.7868–1.9722) | 1.8142 (1.7909–2.2617) | 0.925× | 3/0 |

Both copy cases improve in every paired round. Array-allocation control regresses materially with large candidate outliers; do not omit it. Its native code size and compile allocation counts are unchanged, so new loop instructions are not established as the cause. Code-size equality is not byte-for-byte identity. Other controls overlap, and the short shared-host sample is noisy. No blanket performance win or default-enablement recommendation follows.

## Separate full scalar-copy versus V8

Original protocol: 100 separate warmups, nine batches of 32 calls, three rotating fresh processes. Same candidate binary and heap policy, Node 24.22.1 / V8 13.6.233.17-node.58 with `--no-liftoff`. No other worker CPU jobs overlapped the authorized window. No CPU affinity was imposed.
Candidate **1.5486 ms** versus V8 **0.1545 ms**, **10.022×**, clearly **MISS** against 1.15×. Candidate performs three minor collections and zero majors during 288 timed calls, with exact 36,864 allocations. This is an engine benchmark, not an isolated collector comparison; V8 internal allocations/elision are not observed. The earlier all-six gate remains failed.

## Separate compile and selection evidence

256 Compile+Close iterations per artifact, three rotating fresh processes. Values below are process medians. Timed binaries exclude codegenstats. Qualification independently passed `TestCompactLoopLowering` telemetry and runtime loop/trap-order tests; code-size deltas show changes only in the two copy artifacts, but do not identify the optimized loops. No exact-artifact site telemetry was captured. Source inspection finds the two-receiver scalar-copy loop ineligible for the current one-receiver scanner; initialization and checksum loops may qualify, subject to register-pressure selection. Do not attribute this result to hoisting the copy loop itself.

| Case | Baseline/loop compile µs | Baseline/loop code bytes | Baseline/loop Go bytes | Baseline/loop allocs |
|---|---:|---:|---:|---:|
| struct-allocation-access | 177.10/164.95 | 3183/3183 | 68131/68131 | 253/253 |
| array-allocation-access | 200.11/157.38 | 3439/3439 | 71987/71987 | 242/242 |
| array-fill | 192.48/182.74 | 3951/3951 | 76299/76299 | 246/246 |
| array-copy-checksum | 206.56/231.01 | 7343/7535 | 80420/82852 | 288/289 |
| array-loop-copy-checksum | 204.82/222.88 | 7343/7535 | 80420/82852 | 288/289 |
| tree-survival | 176.07/172.09 | 4015/4015 | 69731/69731 | 289/289 |

Both copy cases add 192 native bytes, 2,432 Go compile bytes, and one allocation. Other code sizes/Go allocation counts are unchanged. Compile timings overlap/noisy and do not establish a compile-speed gain.

Raw JSON preserves artifact and binary hashes, configuration, individual timings, exact counters, Go heap/allocation deltas and post-run `/proc` RSS/HWM. Ignore `maxrss_kib` due inherited process high-water artifacts; prefer `proc_VmHWM_kib`. Copied/promoted/scan counters remain unimplemented and zeros do not mean zero work. Memory differences in these short runs are not evidence of a layout improvement.
