# AMD64 instruction benchmark audit

Date: 2026-08-29

Host: Linux/amd64, AMD Ryzen 7 8845HS, `-cpu=1`. Timed instruction
fixtures execute `b.N` dependent guest operations in one public invocation, so
host-call cost is amortized. All reported cases are 0 B/op and 0 allocs/op.

## Existing benchmark coverage

The AMD64 backend already has strong focused coverage for:

- SIMD min/max edges, conversions, i64 lane operations, and relaxed dot products;
- compact `br_table` execution;
- shared adapter tails;
- GC static-site compilation and resolver code size;
- compiler throughput across scalar, control, SIMD, and branch-table modules; and
- selected peepholes such as SWAR packing and pure deferred drops.

The main execution-level gaps are scalar integer instructions, scalar floating
point, scalar memory width/sign combinations, compare consumers other than
branches, and constant-versus-dynamic division. Existing public single-operation
benchmarks often include one host boundary per instruction and therefore cannot
resolve low-single-digit nanosecond lowering differences.

## Added integer division matrix

`BenchmarkAMD64IntegerDivInstruction` covers:

- a loop/control floor;
- signed division and remainder by `1` and `-1`;
- dynamic unsigned division/remainder by `3`;
- constant `3` with magic multiplication enabled and disabled; and
- constant power-of-two division by `8`.

A short audit command stays below the repository's 30-second investigation
threshold on the measured host:

```sh
go test ./src/wago -run '^$' \
  -bench '^BenchmarkAMD64IntegerDivInstruction$' \
  -benchmem -benchtime=150ms -count=3 -cpu=1
```

Representative medians:

| Shape | Median |
|---|---:|
| loop control | 0.438 ns/op |
| dynamic `i64.div_u 3` | 2.819 ns/op |
| magic constant `i64.div_u 3` | 1.305 ns/op |
| IDIV constant-control `i64.div_u 3` | 2.870 ns/op |
| shift constant `i64.div_u 8` | 0.435 ns/op |
| dynamic `i64.rem_u 3` | 3.031 ns/op |
| magic constant `i64.rem_u 3` | 2.171 ns/op |
| IDIV constant-control `i64.rem_u 3` | 3.048 ns/op |

The existing magic-number division is therefore justified on this host: constant
unsigned division by `3` improves about 54.5%, and remainder improves about 28.8%
against the forced IDIV control.

## Retained signed unit-divisor optimization

Signed constant `1` was unnecessarily left on IDIV even though it cannot trap.
Signed constant `-1` was also left on IDIV even though a compare against `INT_MIN`
plus `neg` preserves the only overflow trap. Signed remainder by either unit
divisor is always zero, including `INT_MIN % -1`.

The retained lowering now performs:

- `x / 1` as identity;
- `x % 1` as zero;
- `x % -1` as zero; and
- `x / -1` as `INT_MIN` check followed by `neg`.

Measured before/after medians:

| Shape | Before | After | Change |
|---|---:|---:|---:|
| constant `i64.div_s 1` | 3.462 ns | 0.443 ns | -87.2% |
| constant `i64.rem_s 1` | 3.074 ns | 0.439 ns | -85.7% |
| constant `i64.rem_s -1` | 0.657 ns | 0.446 ns | -32.1% |
| constant `i64.div_s -1` | 3.650 ns | 0.444 ns | -87.8% |

Current generated-code comparisons against the matching dynamic fixtures are
101 versus 219 bytes for division by `1`, 103 versus 172 bytes for remainder by
`1`, and 158 versus 219 bytes for division by `-1`.

## Rejected remainder experiment

Replacing the existing `mov divisor; imul reg,reg` remainder reconstruction with
a three-operand immediate `imul` reduced the instruction count but changed the
focused `i64.rem_u 3` median from about 2.190 to 2.268 ns/op, roughly 3.6% slower.
The change was reverted. On this CPU the independent constant materialization can
overlap with the quotient path better than the immediate multiply's dependency.

## Scalar float matrix and retained lowering

`BenchmarkAMD64ScalarFloatInstruction` now measures dependent `f64` abs, neg,
rounding, sqrt, min/max path shapes, and copysign loops. The mask users include a
forced GPR-materialization control against the default constant-pool path.
Representative medians are:

| Shape | Median |
|---|---:|
| loop control | 0.228 ns/op |
| `f64.ceil` | 0.902 ns/op |
| `f64.floor` | 0.895 ns/op |
| `f64.trunc` | 0.895 ns/op |
| `f64.nearest` | 0.895 ns/op |
| `f64.sqrt` | 4.869 ns/op |
| ordered-distinct `f64.min` | 1.110 ns/op |
| ordered-equal steady `f64.min` | 0.741 ns/op |

The retained mask change loads implicit abs/neg/copysign masks from the existing
RIP-relative constant pool instead of rebuilding them through a GPR. In a paired
five-sample run, `f64.abs` improves **0.571→0.447 ns/op** (-21.8%) and `f64.neg`
improves **0.564→0.449 ns/op** (-20.4%). Each focused function grows by one byte
because the shared literal and its alignment replace the GPR sequence.

The retained copysign lowering uses `a ^ ((a ^ b) & signMask)` or the equivalent
magnitude-mask identity, selected by operand ownership. Three-operand VEX logic
reads pinned operands directly and uses one mask rather than copying both operands
and building two masks. The first step improves **1.179→1.008 ns/op** (-14.5%);
the constant-pool mask then improves the paired control **1.003→0.885 ns/op**
(-11.8%). The final fixture is 136 bytes versus the original 157 bytes, 21 bytes
smaller.

Reordering scalar min/max so ordered-distinct values fell through was measured and
rejected. It regressed all four distinct/equal rows by roughly 4.8% to 60%, despite
removing a taken conditional branch from the intended common path. The original
layout is retained.

## Rotate and shift matrix

`BenchmarkAMD64IntegerShiftInstruction` covers dynamic-CL rotate, immediate
rotate, BMI2 `rorx`, and masked-zero identities. On this host, constant rotate by
7 is flat at **0.447 ns/op** with the baseline form and **0.446 ns/op** with RORX;
RORX also grows the fixture from 99 to 101 bytes. Constant masked-zero shifts and
rotates already record `alu-identity` and compile to the 95-byte loop-control code
shape. This supports keeping BMI2 RORX experimental and default-off.

## Remaining benchmark priorities

1. **Flags consumers.** Measure compare-to-`select`, compare materialization, and
   compare-to-branch separately. `compare-setcc` statistics show the opportunity,
   but there is no execution benchmark for broader flags-resident consumers.
2. **Memory instruction matrix.** Measure signed/unsigned narrow loads, aligned
   and unaligned scalar loads/stores, store-to-load forwarding, and explicit versus
   guard bounds in one guest loop. Current one-call-per-access benchmarks are too
   host-boundary-heavy for instruction selection work.
3. **Call instruction matrix.** Separate local direct calls, proven monomorphic
   indirect calls, dynamic indirect calls, local `call_ref`, and same-domain
   cross-instance `call_ref` with identical argument/result shapes.
4. **Integer unary matrix.** Add `clz`, `ctz`, and `popcnt` benchmarks for zero,
   dense, and sparse values to verify the documented CPU baseline and identify any
   target-specific fallback need.

The highest remaining implementation opportunity is benchmark coverage for scalar
memory width/sign forms and non-branch compare consumers, where the backend has
several selection paths but no stable execution watchpoint.
