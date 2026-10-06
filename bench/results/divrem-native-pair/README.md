# AMD64 adjacent div/rem reuse

For `a / b` immediately followed by `a % b` on the same local operands, retain
both results from the hardware divide. This removes the second divide without
the multiply/subtract dependency introduced by quotient reconstruction.

The optimization supports signed/unsigned i32/i64, quotient first, with no
intervening instruction except the two matching local reads. Constants and
bytecode-sensitive regional lifetimes use the existing lowering. Existing
zero/overflow checks and earlier pending-trap ordering remain in effect.

## Results

Seven rotating-order samples, 200 ms each, GOMAXPROCS=1, CPU 4, Linux AMD64,
AMD Ryzen 7 8845HS, Go 1.27.1. Baseline commit `67102e5f` is main
`d000bc590d3676c123a50bb1aabc8f078826adf3` plus the signed-division correctness
fix from #851. All three forms include that fix. This isolates the optimization
from the correctness repair; these are not measurements against current main.

Each prepared call performs 4096 pairs and verifies its result, with zero
allocations. Seed dividend is 123456789 and divisor is 37. The dependent loop
feeds `q+r` into the next dividend; the biased variant adds 1048576 to avoid
convergence to zero. Throughput increments the dividend independently. Pressure
keeps seven eager carriers live across the pair.

| Case | Native vs separate operations | Native vs quotient reconstruction |
|---|---:|---:|
| i32 unsigned dependent | -35.56% | -45.24% |
| i32 signed dependent | -34.95% | -44.78% |
| i64 unsigned dependent | -38.78% | -45.65% |
| i64 signed dependent | -38.54% | -45.49% |
| i32 unsigned dependent, biased | -31.47% | -41.87% |
| i32 signed dependent, biased | -31.85% | -41.04% |
| i64 unsigned dependent, biased | -35.16% | -40.56% |
| i64 signed dependent, biased | -34.49% | -40.76% |
| i32 unsigned throughput | -48.70% | +3.77% |
| i32 signed throughput | -49.92% | -7.98% |
| i64 unsigned throughput | -49.76% | -8.87% |
| i64 signed throughput | -50.33% | -5.26% |
| i32 unsigned pressure | -26.10% | -10.27% |
| i32 signed pressure | -19.15% | -22.59% |
| i64 unsigned pressure | -18.47% | -2.42% (inconclusive) |
| i64 signed pressure | -19.37% | -23.05% |

Negative deltas mean less elapsed time. Every native-vs-baseline runtime
comparison has p=0.001 (n=7). Native is 3.77% slower than reconstruction for
i32 unsigned throughput (p=0.016), but still 48.70% faster than separate
operations. The i64 unsigned pressure comparison with reconstruction is
inconclusive (p=0.535). These are unadjusted pairwise benchstat comparisons on
one AMD CPU; other workers and system activity were not controlled.

Disassembly shows two divides in baseline, one divide plus one multiply in
reconstruction, and one divide with no multiply in native pairing. Each native
fixture has exactly one fusion. Code shrinks by 21/36 bytes for unsigned/signed
i32 and 24/41 bytes for unsigned/signed i64. Spills fall from 1 to 0, or 5 to 4
under pressure. i64 frames shrink by 16 bytes; i32 frames are unchanged.

## Production coverage and controls

All 121 workload modules compiled with diagnostics: zero errors and **zero
eligible adjacent pairs**. The four broader opportunities from the original
#831 experiment require intervening instructions and are excluded here.
Age and esbuild native modules are byte-identical to baseline. Their command
and full-compilation timing differences were inconclusive, as were the three
small compile-time controls. No application speedup is claimed.

This result supports the narrow adjacent pattern only. Wider pairing and
performance on Intel/other architectures remain unmeasured.

## Reproduction

The retained raw files are from the research harness's `BenchmarkPairs`;
`summary.csv` records all medians, observed minima/maxima, and sample counts.
The same fixtures, oracle and timed loops are now retained in
`src/wago/divrem_pair_*test.go` as `BenchmarkDivRemPairAMD64`. The offline
quotient-reconstruction comparator is research-only and is not shipped.

```sh
benchstat bench/results/divrem-native-pair/{baseline,reconstruct,native}.txt
GOMAXPROCS=1 taskset -c 4 go test ./src/wago -run '^$' \
  -bench '^BenchmarkDivRemPairAMD64$' -benchtime=200ms -count=7
go test -tags=wago_regalloccheck,wago_codegenstats ./src/wago -run '^TestDivRemPair'
```

For a before/after run, copy the new test files into a checkout of `67102e5f`,
build both test binaries before timing, then rotate their execution order.
Diagnostic fusion-count assertions are expected to fail on the baseline;
execution/oracle tests should pass. No benchmark numbers in this report come
from register-allocation-check or diagnostic builds.
