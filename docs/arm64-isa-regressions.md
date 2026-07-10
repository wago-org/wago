# ARM64 ISA benchmark regression queue

This is the working queue for the native Darwin/ARM64 railshot backend. The
benchmark is the generated ISA suite in `bench/corpus/isa-manifest.json`, run
with guard-page bounds enabled and compared with wazero's compiler runtime on
the same host.

Command used for the queue snapshot:

```sh
cd bench
WAGO_BOUNDS=signals go test -run '^$' -tags wago_guardpage \
  -bench '^BenchmarkExec$/^isa_' -benchmem -count=1 -benchtime=50ms \
  -timeout 0 -wago.bench.isa .
WAGO_BOUNDS=signals go test -run '^$' -tags wago_guardpage \
  -bench '^BenchmarkWazeroExec$/^isa_' -benchmem -count=1 -benchtime=50ms \
  -timeout 0 -wago.bench.isa .
```

The short run is for triage only. Any change is validated with targeted longer
runs and the default plus guard-page test suites before it is accepted.

## Landed during this pass

| Cluster | Change | Before vs wazero | Result |
|---|---|---:|---|
| Linear memory loads/stores | Permit local-register pinning in call-free memory functions; retain stack locals for memory-touching call-makers. | 2.65–3.53× slower | All six ISA memory rows are now faster in the guard-page snapshot. |
| Variable shifts/rotates | Sink `local.set x (shift (local.get x) y)` into `x`'s register when the count does not alias it. | ~2.3× slower | Shift/rotate rows now meet or beat wazero in the snapshot. |
| Scalar `f32`/`f64` min/max | Sink the existing NaN/signed-zero-correct sequence into the target V register. | ~2.0–2.4× slower | Longer sampling leaves only a small gap (about 2–6%); retain the correctness sequence and continue with a separate fast-path design. |
| `i32.wrap_i64(i64.extend_i32_{s,u}(x))` | Eliminate the semantically redundant widen/narrow pair while retaining a canonical W-register carrier. | ~2.0× slower | Improved to ~1.3× slower; expression selection remains. |
| Indirect local calls | Mark local int-only register-ABI funcref descriptors and guard an internal-entry dispatch against the existing wrapper path. | 2.22× slower | ~34.4 µs/export on the five-run guard-page sample, faster than wazero (50.2 µs) and WARP (43.0 µs); host and cross-instance entries retain the wrapper path. |

## Remaining queue (short-run triage)

| Priority | ISA row / cluster | Approx. wago ÷ wazero | Likely next step |
|---:|---|---:|---|
| 1 | `isa_ctl.if_else` | 2.28× | Add a guarded if-conversion path for simple result-producing integer arms; preserve branch/trap order. |
| 2 | `isa_i32.clz`, `isa_i64.clz` | 1.72× / 1.36× | Improve expression selection around unary results consumed by a binary local update; `CLZ` itself is already native. |
| 3 | `isa_var.local_getset` | 1.61× | Reduce local-set/get canonicalization when the local remains register-resident. |
| 6 | `isa_cvt.i64_extend_u` | 1.33× | Extend the round-trip rewrite through surrounding i32 arithmetic only when the carrier's zero-extension invariant is proven. |
| 7 | `isa_ctl.select`, `isa_ctl.br_table` | 1.26× / 1.14× | Compare emitted CSEL/jump-table code with WARP's x86-64 selection strategy and add focused execution tests. |
| 8 | Remaining 5–12% rows | — | Re-sample at longer duration before changing code; these are within normal short-run variation on the host. |

## Reference sources

- `src/core/compiler/backend/railshot/arm64/` — native implementation and
  ARM64-specific register/ABI constraints.
- `warp/src/core/compiler/backend/x86_64/` — WARP's mature x86-64 selection,
  control, call, and local-allocation reference.
- `warp/src/core/compiler/backend/aarch64/` — WARP's AArch64 instruction and
  control-flow reference when the x86 technique has no ISA-equivalent form.

Do not claim overall performance parity from this file. The queue is closed only
after a repeated paired run shows every applicable row faster than wazero and
the full default and guard-page suites remain green.
