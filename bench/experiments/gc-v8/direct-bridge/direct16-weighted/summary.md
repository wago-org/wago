# Fresh direct16 weighted attribution results

Qualified runtime `a2e066d26d0555e03e2831bd359da1d5f2a51b13`; protocol-only HEAD is recorded in raw provenance. Same Intel Xeon Platinum 8573C host, Go 1.27.1 and Node 24.22.1 / V8 13.6.233.17-node.58 with `--no-liftoff`. Identical direct16 compiler pipeline except the weighted/SSE2 additions. Runtime direct receipts and native view confirmed; compact, age2, diagnostics and allocation elision OFF. Nursery 2 MiB; two 8 MiB old semispaces.

All 18 actual-GC audits, 54 bounded and 72 full processes passed SHA, checksum, exact Wago allocation and receipt/configuration checks. Audits use 128 warmups. Full protocol is 100 warmups + nine batches of 32 calls, three rotating fresh-process rounds. Bounded protocol is three warmups + five calls, three rounds; results are kept separate.

**Every variant misses the 1.15x V8 target on every case.** Values are medians of three process medians in milliseconds.

| Case | Baseline | Weighted | SSE2 | V8 | Weighted/V8 | SSE2/V8 |
|---|---:|---:|---:|---:|---:|---:|
| struct-allocation-access | 5.7756 | 5.6238 | 5.6383 | 1.2149 | 4.629 | 4.641 |
| array-allocation-access | 11.7611 | 11.3699 | 12.2468 | 8.6071 | 1.321 | 1.423 |
| array-fill | 13.3375 | 16.2816 | 14.8868 | 9.6857 | 1.681 | 1.537 |
| array-copy-checksum | 0.4345 | 0.2226 | 0.2265 | 0.1471 | 1.513 | 1.539 |
| array-loop-copy-checksum | 0.6674 | 0.5846 | 0.5300 | 0.1836 | 3.184 | 2.886 |
| tree-survival | 2.0332 | 2.0869 | 2.1016 | 0.8369 | 2.493 | 2.511 |

## Per-round changes versus baseline

| Case | Full weighted | Full SSE2 | Bounded weighted | Bounded SSE2 |
|---|---|---|---|---|
| struct-allocation-access | -37.0%, +19.0%, -2.6% | -35.6%, +5.7%, -2.4% | -0.7%, +5.1%, -2.0% | -11.5%, +5.7%, -4.8% |
| array-allocation-access | -14.2%, -4.6%, -0.7% | -11.1%, +11.2%, +6.9% | +24.9%, +51.6%, +11.8% | -33.9%, +39.9%, -5.1% |
| array-fill | +22.1%, -6.8%, +38.1% | +17.2%, +5.5%, +5.9% | -13.2%, +6.9%, +71.5% | -39.2%, +20.1%, +47.8% |
| array-copy-checksum | -21.8%, -28.7%, -51.1% | -59.4%, -27.4%, -47.0% | -33.0%, -17.5%, +133.0% | -1.6%, -18.9%, -9.1% |
| array-loop-copy-checksum | -15.4%, -22.4%, +5.0% | -19.5%, -44.5%, -4.8% | -36.9%, -16.2%, -20.2% | -44.0%, -40.8%, -13.1% |
| tree-survival | +10.9%, -1.6%, +12.5% | +3.4%, -8.5%, +30.3% | -45.8%, -9.5%, +10.7% | -53.1%, -0.9%, +18.6% |

## Counters and memory

Values listed baseline / weighted / SSE2. Generic copied/promoted/scan counters are not instrumented; their zero values do not measure zero work. Each timed process makes 288 calls.

| Case | Timed allocations | Minor/full | Go heap MiB | proc VmHWM MiB |
|---|---:|---|---|---|
| struct-allocation-access | 75515904 | 1275/23 / 1275/23 / 1275/23 | 27.58 / 27.58 / 27.58 | 38.47 / 39.12 / 38.58 |
| array-allocation-access | 75515904 | 2901/125 / 2901/125 / 2901/125 | 27.80 / 27.80 / 27.80 | 39.31 / 39.50 / 38.77 |
| array-fill | 75515904 | 2901/125 / 2901/125 / 2901/125 | 27.80 / 27.80 / 27.80 | 39.35 / 39.36 / 39.31 |
| array-copy-checksum | 36864 | 3/0 / 3/0 / 3/0 | 27.41 / 27.41 / 27.41 | 18.21 / 18.34 / 18.50 |
| array-loop-copy-checksum | 36864 | 3/0 / 3/0 / 3/0 | 27.41 / 27.41 / 27.41 | 18.20 / 18.37 / 18.57 |
| tree-survival | 19454688 | 293/3 / 293/3 / 293/3 | 27.55 / 27.55 / 27.55 | 38.62 / 38.64 / 38.75 |

## Separate compilation footprint

Compile+Close, 256 iterations, three rotating fresh processes. Values baseline / weighted / SSE2. ns/op is noisy; Go allocation bytes are not peak compilation memory.

| Case | Code bytes | Go B/op | Allocs/op | ns/op |
|---|---|---|---|---|
| struct-allocation-access | 1775 / 1775 / 1775 | 56026 / 56026 / 56026 | 232 / 232 / 232 | 207734 / 230193 / 191267 |
| array-allocation-access | 1967 / 1967 / 1967 | 56075 / 56075 / 56075 | 223 / 223 / 223 | 176327 / 196864 / 174885 |
| array-fill | 2543 / 2543 / 2543 | 59979 / 59979 / 59979 | 233 / 233 / 233 | 193604 / 208552 / 190844 |
| array-copy-checksum | 4655 / 4719 / 4911 | 76764 / 76508 / 72923 | 277 / 276 / 274 | 304666 / 297471 / 286525 |
| array-loop-copy-checksum | 4655 / 4719 / 4911 | 76764 / 76508 / 72923 | 277 / 276 / 274 | 259588 / 324130 / 248910 |
| tree-survival | 1455 / 1455 / 1455 | 57339 / 57339 / 57339 | 267 / 267 / 267 | 187350 / 195378 / 177491 |

## Interpretation

Bulk copy improves in this run with weighted reduction; scalar copy improves less and still lacks the compact paired/range-transfer path. SIMD is not uniformly better than scalar weighted reduction. Control regressions and considerable per-round variation remain visible above. These are compiler changes, not proof of a collector gain. No direct-versus-compact isolated layout claim is possible because compiler coverage and trusted policies differ.

All workers were idle during timing, but the physical host is shared and CPU affinity was not imposed. V8 allocation counts and internal optimizer elision are unobserved. These are end-to-end engine benchmarks, not isolated GC efficiency measurements. Historical other-host numbers are not comparators. Keep all controls and misses; no default or universal speed claim follows.
