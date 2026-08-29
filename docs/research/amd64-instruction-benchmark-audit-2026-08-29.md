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

## Next benchmark priorities

1. **Scalar float latency matrix.** Add guest-loop benchmarks for `sqrt`,
   `ceil/floor/trunc/nearest`, min/max ordinary and NaN/signed-zero edges,
   `copysign`, and trapping/saturating conversions. Existing SIMD benchmarks do
   not measure these scalar paths.
2. **Rotate and shift matrix.** Compare constant and variable counts, plus the
   host-gated BMI2 `rorx` path. This is needed before changing the BMI2 default or
   adding more constant-count selection.
3. **Flags consumers.** Measure compare-to-`select`, compare materialization, and
   compare-to-branch separately. `compare-setcc` statistics show the opportunity,
   but there is no execution benchmark for broader flags-resident consumers.
4. **Memory instruction matrix.** Measure signed/unsigned narrow loads, aligned
   and unaligned scalar loads/stores, store-to-load forwarding, and explicit versus
   guard bounds in one guest loop. Current one-call-per-access benchmarks are too
   host-boundary-heavy for instruction selection work.
5. **Call instruction matrix.** Separate local direct calls, proven monomorphic
   indirect calls, dynamic indirect calls, local `call_ref`, and same-domain
   cross-instance `call_ref` with identical argument/result shapes.
6. **Integer unary matrix.** Add `clz`, `ctz`, and `popcnt` benchmarks for zero,
   dense, and sparse values to verify the documented CPU baseline and identify any
   target-specific fallback need.

The highest implementation opportunity after the retained unit-divisor change is
not another division rewrite. It is benchmark coverage for scalar floats, memory
width/sign forms, and non-branch compare consumers, where the backend has several
selection paths but no stable execution watchpoint.
