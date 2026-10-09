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
