# Integer values across a conditional join

`src/core/compiler/backend/railshot/amd64/integer_join_pressure_test.go`
adds bounded AMD64 regression profiles for the structured-control part of
[#810](https://github.com/wago-org/wago/issues/810). It does not change the
compiler. Current main already produces correct results for these profiles.

The generator covers i32 and i64, 1/8/24 live operand values, and two operation
schedules: 12 modules. Each prefix computes a distinct parameter-dependent
XOR, signed/unsigned division/remainder, variable shift/rotate, and add. Those
values remain below a two-way conditional on the Wasm operand stack. Both arms
write an identity marker, increment a work counter, and return a distinct
parameter-dependent value. All prefix results and the selected arm result are
returned separately. There is no common checksum that can hide cancelling
errors and no guest call inside the function.

Each module runs 160 bounded inputs (1,920 executions total), including signed
boundaries, shift counts at/beyond the type width, deterministic generated
values, and i32 conditions 0, 1, 0xffffffff, and 0x100000000. The last condition
must truncate to zero. Divisors are +3 or -3; this profile does not test divide
traps. Host expectations use Go integer semantics and the existing independent
shift/division test oracles. Checks also cover unchanged arguments, guarded
result storage, prefix/arm/end markers, and completed work. Corrupted slots,
guards, inputs, markers and counts reject for their expected reason. A real
wrong-arm invocation and an omitted invocation must also reject.

The compile configuration uses one worker, explicit memory bounds, and the
baseline AMD64 feature mask (0). Code mapping is deferred, then the test maps
and compares the exact native bytes before calling the entry adapter through
`Engine.CallPrepared`. The adapter calls the internal function; the generated
Wasm body itself is call-free. This does not qualify other public API paths.
Ordinary numerical tests run without compiler diagnostics. With
`wago_codegenstats`, the test reports selected shared/fallback path, code/frame
size, and spill/reload counts, and requires spill/reload activity at 24 live
values. Whole-function counters do not locate individual spill lifetimes.

A separate GNU objdump gate checks every shape's native body. Unique markers
bound the relevant conditional and alternate arm. It requires exactly N divide
and N variable shift/rotate instructions before that conditional, with none
later. The conditional reaches a decoded instruction at or before the alternate arm
marker; the arm exit skips that marker and reaches a decoded instruction at or before
the end marker. A numerically valid straight-line substitute rejects because
it lacks the conditional shape. Decoder-output controls check missing, extra,
late or out-of-range arithmetic, incorrect/interior targets, missing exits,
calls and duplicate markers. This is a bounded placement/shape observation,
not a complete control-flow, original-source, or register-transport proof.
GNU objdump absence produces an explicit skip for this gate.

Run from the repository root:

```sh
go test ./src/core/compiler/backend/railshot/amd64 -run 'Test(IntegerLiveAcrossJoin|JoinPressure)' -count=1
go test -tags=wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run 'Test(IntegerLiveAcrossJoin|JoinPressure)' -count=1
go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run 'Test(IntegerLiveAcrossJoin|JoinPressure)' -count=1 -v
go test ./src/core/compiler/backend/railshot/amd64 -run '^$' -bench '^BenchmarkIntegerLiveAcrossJoin$' -benchtime=100ms -benchmem
```

The benchmark uses schedule 1 at low/high pressure, with separate compile rows
and execution rows for each arm. Compilation includes decode, validation and
native code generation; it excludes executable mapping. Execution reuses one
prepared instance and fixed inputs, with allocation/setup and independent
correctness checks outside the timer. Timed work includes the guest marker and
counter stores plus the runtime entry boundary. All setup resources close at
subbenchmark cleanup. Allocation numbers are Go allocation traffic, not native
mapping size or retained memory. `native-B` is generated guest code size, not
release binary size.

For comparisons, transplant the exact test file onto a pinned main checkout and
use the same compiler, inputs, CPU and build tags on both sides. Alternate short
baseline/candidate pairs and report medians, full ranges and variance, including
outliers. Run ordinary timing separately from diagnostic and checked builds.
This tests-only addition supplies a repeatability control, not an optimization
or evidence that unchanged sources guarantee identical performance. Synthetic
pressure results do not establish application speedups.

Native ARM64, FP/SIMD/reference profiles, loops, parallel-copy algorithm
qualification and independent source/transport verification remain separate.
The broader issue stays open.


## Native comparison with current main (2026-10-13)

Fresh main is `069a26ea07bdd312b672d4fc56b989aa55e89e98`; measured PR
head is `1bf37fe458728f2053dd3b677c38572beeacb4c4`. The earlier merge
already includes this main revision. Their production sources match; the PR
adds the test and documentation. The baseline uses that exact main with only
the identical join-pressure test file copied in as the shared harness. Both
ordinary and `wago_regalloccheck` correctness and observer-control suites pass
on both builds. All 12 fixture source/native hash pairs match.

Measured natively on Linux AMD64, Ryzen 7 8845HS, Go 1.27.2, with ordinary
builds, `GOMAXPROCS=1`, CPU 4 affinity, one compiler worker, explicit bounds,
and AMD64 feature mask 0. Eight pairs alternate main/PR then PR/main order.
Each fresh process measures all 12 rows at 100 ms per row. All 192 [raw paired samples](integer-join-pressure-20261013.csv) are retained. CPU frequency and other host work were not isolated.

The delta is the median of eight paired `(PR/main - 1)*100` values, rather than
the ratio of the two group medians. Brackets show the observed paired min/max;
they are not a confidence interval. CV is the sample standard deviation divided
by the mean, in percent.

| Row | Main median ns/op | PR median ns/op | Paired median delta [observed min, max] | CV main / PR |
| --- | ---: | ---: | ---: | ---: |
| `compile/i32/live=1` | 13857.50 | 14263.00 | +1.81% [-4.45%, +47.72%] | 6.95% / 21.46% |
| `execute/i32/live=1/arm=0` | 20.70 | 20.49 | -0.36% [-5.56%, +52.73%] | 2.70% / 19.54% |
| `execute/i32/live=1/arm=1` | 20.79 | 20.58 | +0.34% [-5.20%, +17.69%] | 2.58% / 6.95% |
| `compile/i32/live=24` | 48865.00 | 48072.50 | +1.31% [-5.24%, +6.50%] | 7.11% / 4.71% |
| `execute/i32/live=24/arm=0` | 54.62 | 53.90 | -0.68% [-6.55%, +0.94%] | 2.19% / 1.23% |
| `execute/i32/live=24/arm=1` | 54.38 | 54.12 | -1.00% [-2.54%, +1.84%] | 1.63% / 0.76% |
| `compile/i64/live=1` | 14392.50 | 14084.50 | -2.56% [-5.42%, +1.50%] | 6.91% / 7.06% |
| `execute/i64/live=1/arm=0` | 20.75 | 20.66 | -0.25% [-4.00%, +3.91%] | 2.38% / 2.41% |
| `execute/i64/live=1/arm=1` | 20.74 | 20.71 | -0.96% [-18.50%, +2.40%] | 8.54% / 1.46% |
| `compile/i64/live=24` | 49564.00 | 51951.00 | +0.90% [-5.50%, +87.58%] | 7.34% / 24.57% |
| `execute/i64/live=24/arm=0` | 70.06 | 69.47 | -0.85% [-2.83%, +20.09%] | 1.69% / 6.81% |
| `execute/i64/live=24/arm=1` | 70.55 | 68.92 | -1.44% [-4.69%, +0.82%] | 2.27% / 0.74% |

Every paired range crosses zero. The retained compile outliers reach +87.58%,
and PR compile CV reaches 24.57%. These short synthetic samples do not
establish a performance improvement or timing equivalence. No compiler or
runtime optimization is part of this PR.

Generated guest bytes and compile allocation traffic match in every sample:

| Shape | Native B, both | Compile B/op, both | Compile allocs/op, both |
| --- | ---: | ---: | ---: |
| `i32/live=1` | 409 | 29752 | 56 |
| `i32/live=24` | 2177 | 68392 | 85 |
| `i64/live=1` | 418 | 29752 | 56 |
| `i64/live=24` | 2463 | 76328 | 87 |

Every execution row reports 0 B/op and 0 allocs/op on both builds. Allocation
and generated-code deltas are zero. These are Go allocation traffic and guest
code bytes; release binary size, native allocations, retained memory, and RSS
were not measured.

The shared test-file SHA-256 is
`651110c52e9202e5204e419e8e0181f6eb0380ad48be491ff7a2c6f6cc1ad315`.
The separately built test binaries have SHA-256:

- Main: `920f54e2b77de5b1717f73d77c29d334362263806e838dba6eff110b754018e4`.
- PR: `1299858f2afb838c0ad52b29b0a47c95358911f371377df0531c994d61ec33e6`.

To reproduce, copy the exact test file to a worktree at the main revision, then
build each test binary with the same toolchain and flags:

```sh
go test -p=2 -c -o /absolute/path/join-bench.test \
  ./src/core/compiler/backend/railshot/amd64
GOMAXPROCS=1 WAGO_BOUNDS=explicit taskset -c 4 /absolute/path/join-bench.test \
  -test.run='^$' -test.bench='^BenchmarkIntegerLiveAcrossJoin$' \
  -test.benchtime=100ms -test.count=1 -test.benchmem
```

Alternate the two binaries for eight pairs and retain every raw stdout, stderr,
exit status, pair order, and environment record. The local review packet retains
the raw 16 process logs, all 192 rows, the pairing script, summaries, correctness
logs, and exact source/build identities. This documentation follow-up changes
no measured source or harness.
