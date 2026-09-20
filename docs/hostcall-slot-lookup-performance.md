# HostCall slot lookup review (PR #637)

Base: `b221bb1ea09844a16a8eb65476a0f6a6b8269698` (main fetched on
2026-09-19). Reviewed PR head: `6c6dc9499d722ed7061e0edde93407712fe8b097`.
The final pushed head and CI status are recorded in the PR description.

## Layout and ownership

The four accessors compare parameter and result counts independently. A complete
view has one slot for each scalar/reference and two for each v128; no value has
zero slots. Equal counts therefore prove that logical and physical indexes match.
Bounds/type checks precede the shortcut; both index-zero typed paths remain.
Mixed-v128 synthetic views retain the scan. No signature scan/cache is added.

Construction audit:
- `instantiateCoreWithModuleLease` freezes compiled metadata before binding;
  `buildSyncHosts` validates both slot counts and binds `c.importFuncSigs`.
  `freezeExecution`/`cloneCompiledMetadata` deep-copy public containers, and
  `Compiled.Signature` returns copies.
- `callBoundUnchecked`, scalar/reference dispatch, `newHostDispatch`, the two
  `host_execution.go` portals, and `zz_host_execution_fixed.go` borrow the same
  binding signature. They do not truncate or change the view during callbacks.
- Engine copied/wide dispatch reads the compiler's separate `n`/`nres` counts,
  checks capacity and passes `args[:n]`/`results[:nres]`. Fixed-view dispatch
  constructs both slices with their complete bound lengths; foreign frames take
  checked fallback dispatch. Host thunks write these counts from validated types.
- Imported starts have the validated empty signature and empty buffers.
- `NewHostFuncRef` exposed a real ownership defect: the owner copied the public
  signature but its captured callback binding still aliased the public slices.
  The fix moves those same two copies before binding, so owner and callback share
  private metadata. No extra successful-construction or callback allocation is
  added. The native mutation regression fails on main and passes with this fix.
- Unsupported concurrent mutation and deliberately malformed private HostCall
  structs are outside the valid-view contract. Native v128 rejection remains.

## Tests and limits

`TestHostCallIndexedNative` executes compiled guest code for HostCallFunc and
CallerHostCallFunc, with both typed and raw indexed access. The signatures are
0-to-0, 0-to-1, 1-to-0, 1-to-1, 4-to-3, 8-to-4, 16-to-8, and mixed numeric.
Every input contributes a position-weighted value to every output. All outputs
are checked, and the tests assert the actual universal callback binding type.
`TestHostCallIndexedNativeReferences` swaps two runtime-issued externrefs around
an i64 value through both callback forms. It uses no invented reference tokens.

Synthetic tests cover independent parameter/result layouts, empty and zero
views, every position, invalid indexes and types, raw floating-point bit
patterns (including negative zero), and vectors at the start, middle, and end.
The bounded fuzz target builds complete slot slices and uses an independent
physical cursor as the reference mapping. Each raw result write checks all
slots, including adjacent sentinels. Existing scalar/reference API coverage,
current-type mutation coverage, and native v128 rejection remain in place.

The ownership regression fails on unmodified main and passes after moving the
existing signature copies before binding. Other new tests are parity tests:
they pass with both current-main accessors and optimized accessors.

Local validation:

- Native Linux/amd64 focused accessor, binding, execution and allocation tests pass.
- Both 16-to-8 callback forms remain at zero direct-dispatch and invocation allocations.
- Full `go test ./src/wago -count=1` passes with pinned WABT 1.0.41 and the
  pinned Release 3 interpreter. Initial runs failed due to missing submodules
  and WABT 1.0.42; those environment errors were corrected.
- Focused race tests cover HostCall, Caller, re-entry, plugin gates, unsupported
  signatures and owned host-function signatures; they pass.
- Ten seconds of bounded slot fuzzing pass (146,770 executions in the recorded run).
- Linux/arm64 cross-build and QEMU execution of the new native tests, allocation
  checks and ownership regression pass. This is emulation, not native ARM64 execution.
- `just test` reaches two TinyGo linker failures: duplicate `tinygo_task_exit`
  in `TestBuildTinyGoEmbedsArtifactWithoutCompiler` and
  `TestBuildTinyGoStripsByDefault`. Both reproduce on the same main base.
  The worktree also needs `GOFLAGS=-buildvcs=false` for these external builds.
  Later unit-recipe steps are not counted as passed.
- The independent `just test corpus all` check passes.
- `just lint` returns success. Its recipe tolerates standard-build staticcheck
  findings already present on main; runtime-tagged staticcheck passes. The one
  new nil-context test warning was corrected.
- `git diff --check` passes.

The native execution file is selected by the ordinary CI package tests on all
six native OS/architecture targets. It excludes TinyGo. CI results for the exact
pushed commit belong in the PR, not in a claim about local architecture coverage.

## Measurement method

Two builds use the same current-main base and identical tests/benchmarks.
Both include the narrow owned-signature fix, to isolate the optimization.
A has the current production accessors; B has the four optimized accessors.
No other production code differs between A and B.

Host: Linux/amd64, AMD Ryzen 7 8845HS, Go 1.27.1, `GOMAXPROCS=4`, CPU affinity
4-7, frequency boost enabled. Six serial pairs run in AB, BA, AB, BA, AB, BA
order, with 100 ms per benchmark. No tests, builds, fuzzing or profiles run
concurrently with timings. The machine is not a reserved laboratory host.

The second comparison uses fresh `go test -a -c` builds, six alternating pairs,
and 500 ms samples for legacy/caller/call arities 0-to-0, 1-to-0, 1-to-1, 8-to-4,
16-to-8 and all mixed synthetic layouts. Benchmark source and workloads are
unchanged. Statistical comparisons use benchstat's Mann-Whitney test; p-values
are unadjusted across many comparisons. Small effects need caution.

`HostCallIndexedMixed` is a synthetic accessor view. `HostCallIndexed` is direct
synchronous binding dispatch. `HostCallIndexedInvoke` includes complete guest
invocation. Binding gains must not be read as whole-invocation gains.
`CallerArity` writes bulk slices and does not use the changed accessors, including
its legacy 8-to-4 row. Such controls can still change due to code layout or
inlining, so their results are retained.

## Results and unresolved controls

Binding dispatch improves at arity 4, 16 and 64; the 64-value typed/raw medians
improve 71.23% / 76.70%. Complete 16-to-8 raw invocations improve 10.97% for
HostCallFunc and 9.67% for CallerHostCallFunc. The typed HostCallFunc invocation
has almost no median change (-0.23%); the typed caller form improves 3.85%.
These are different measurement layers, not interchangeable speedups.

The reported legacy 8-to-4 change (650.8 to 695.0 ns, +6.8%) does not reproduce:
the first run is 543.3 to 538.0 ns (p=.084), and the fresh-build 500 ms repeat is
532.2 to 533.9 ns (p=.589). This bulk-slot callback never calls the four changed
accessors. These measurements do not explain the older result or prove immunity
to layout/inlining effects.

Other regressions remain. The fresh zero-value HostCall bulk control moves from
293.7 to 316.4 ns (+7.71%, p=.002); one-to-zero moves from 306.6 to 322.7 ns
(+5.27%, p=.002). Both callbacks avoid the changed accessors. Five-second CPU
profiles show the same fixed-view dispatch path. Objdump comparisons find
identical instruction sequences after address normalization for the bulk
callback (19 instructions), `dispatchSingleHostCall` (211), and
`dispatchSingleHostCallFixedView` (196). Several callback/dispatch symbols move
by 128 bytes. A layout effect is possible, but this does not establish its cause.
No speculative padding or dispatch redesign was added. The cause remains
unresolved, and the merge decision remains open.

Mixed synthetic arities 16 and 64 also regress in the longer repeat (+2.67% and
+1.09%, both p=.002). These valid views still scan offsets and now check counts
first. They remain part of the report despite native v128 callbacks being
unsupported. There is no claim of universal improvement or no regression.

Callback allocation counts do not change. Indexed synthetic, binding, and full
invocation cases use 0 B/op and 0 allocs/op. Existing legacy controls retain
64 B and one allocation per callback; loop controls report invocation totals.

## Six-pair results (100 ms samples)

Entries are medians with the full six-sample minimum and maximum. Deltas use
unrounded medians. All benchmark names omit the common `Benchmark` prefix and
`-4` CPU suffix. No aggregate geomean is used across the measurement layers.

### Synthetic accessor views

| Benchmark | A ns/op [min, max] | B ns/op [min, max] | Delta | p | B/op A / B | allocs/op A / B |
|---|---:|---:|---:|---:|---:|---:|
| HostCallIndexedMixed/arity4 | 38.18 [37.94, 38.89] | 38.61 [38.14, 41.36] | +1.11% | 0.290 | 0 / 0 | 0 / 0 |
| HostCallIndexedMixed/arity16 | 205.10 [204.50, 206.60] | 211.20 [209.80, 220.90] | +2.97% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedMixed/arity64 | 1973.00 [1962.00, 1986.00] | 1992.00 [1983.00, 2087.00] | +0.96% | 0.004 | 0 / 0 | 0 / 0 |

### Direct synchronous binding dispatch

| Benchmark | A ns/op [min, max] | B ns/op [min, max] | Delta | p | B/op A / B | allocs/op A / B |
|---|---:|---:|---:|---:|---:|---:|
| HostCallIndexed/arity1/rawfalse | 18.73 [18.46, 19.32] | 18.70 [18.63, 18.79] | -0.21% | 0.786 | 0 / 0 | 0 / 0 |
| HostCallIndexed/arity1/rawtrue | 19.34 [19.26, 19.93] | 17.51 [17.29, 17.79] | -9.49% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexed/arity4/rawfalse | 49.73 [49.15, 50.68] | 45.15 [45.07, 45.74] | -9.22% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexed/arity4/rawtrue | 45.44 [45.01, 46.76] | 38.09 [37.65, 39.38] | -16.16% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexed/arity16/rawfalse | 232.05 [230.00, 234.10] | 153.50 [152.80, 154.80] | -33.85% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexed/arity16/rawtrue | 213.30 [212.80, 220.80] | 123.20 [121.20, 126.20] | -42.24% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexed/arity64/rawfalse | 2041.00 [2035.00, 2062.00] | 587.25 [584.20, 597.00] | -71.23% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexed/arity64/rawtrue | 1976.00 [1962.00, 2017.00] | 460.50 [458.20, 463.60] | -76.70% | 0.002 | 0 / 0 | 0 / 0 |

### Complete indexed invocation

| Benchmark | A ns/op [min, max] | B ns/op [min, max] | Delta | p | B/op A / B | allocs/op A / B |
|---|---:|---:|---:|---:|---:|---:|
| HostCallIndexedInvoke/mixed/rawfalse/callerfalse | 432.20 [425.80, 444.30] | 420.00 [418.10, 423.90] | -2.82% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/mixed/rawfalse/callertrue | 600.05 [595.50, 618.80] | 585.75 [581.90, 589.20] | -2.38% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/mixed/rawtrue/callerfalse | 384.65 [381.30, 411.70] | 379.80 [375.80, 392.50] | -1.26% | 0.041 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/mixed/rawtrue/callertrue | 556.10 [551.60, 565.70] | 533.25 [529.50, 552.20] | -4.11% | 0.006 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/0-0/rawfalse/callerfalse | 298.95 [298.30, 309.10] | 314.85 [311.40, 325.20] | +5.32% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/0-0/rawfalse/callertrue | 448.45 [445.20, 457.40] | 444.20 [441.80, 454.20] | -0.95% | 0.132 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/0-0/rawtrue/callerfalse | 299.40 [295.70, 308.30] | 314.40 [311.60, 317.30] | +5.01% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/0-0/rawtrue/callertrue | 445.45 [443.00, 461.60] | 454.60 [443.40, 475.00] | +2.05% | 0.093 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/0-1/rawfalse/callerfalse | 318.20 [312.70, 325.10] | 326.50 [325.50, 328.00] | +2.61% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/0-1/rawfalse/callertrue | 469.55 [463.10, 483.80] | 466.80 [464.60, 472.30] | -0.59% | 0.818 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/0-1/rawtrue/callerfalse | 313.10 [312.10, 323.50] | 324.80 [323.50, 334.60] | +3.74% | 0.004 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/0-1/rawtrue/callertrue | 466.15 [464.00, 472.60] | 458.70 [453.30, 476.20] | -1.60% | 0.310 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/1-0/rawfalse/callerfalse | 319.30 [317.20, 330.30] | 332.50 [328.70, 343.00] | +4.13% | 0.004 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/1-0/rawfalse/callertrue | 472.75 [469.70, 486.20] | 470.40 [467.40, 475.30] | -0.50% | 0.240 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/1-0/rawtrue/callerfalse | 317.60 [315.60, 325.50] | 329.95 [326.40, 330.90] | +3.89% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/1-0/rawtrue/callertrue | 467.40 [463.30, 474.70] | 474.40 [466.10, 500.40] | +1.50% | 0.221 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/1-1/rawfalse/callerfalse | 336.20 [330.30, 348.90] | 345.15 [338.80, 365.00] | +2.66% | 0.093 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/1-1/rawfalse/callertrue | 489.05 [478.40, 495.60] | 484.35 [481.80, 487.20] | -0.96% | 0.119 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/1-1/rawtrue/callerfalse | 328.40 [323.90, 339.10] | 339.55 [336.60, 349.50] | +3.40% | 0.009 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/1-1/rawtrue/callertrue | 486.10 [481.10, 495.10] | 476.25 [472.90, 490.80] | -2.03% | 0.026 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/4-3/rawfalse/callerfalse | 397.50 [396.50, 416.60] | 397.80 [393.30, 428.10] | +0.08% | 0.788 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/4-3/rawfalse/callertrue | 568.80 [567.40, 584.70] | 557.20 [541.90, 581.30] | -2.04% | 0.195 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/4-3/rawtrue/callerfalse | 366.80 [365.40, 372.30] | 366.20 [361.90, 369.60] | -0.16% | 0.288 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/4-3/rawtrue/callertrue | 544.00 [535.00, 546.30] | 527.05 [516.00, 541.40] | -3.12% | 0.009 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/8-4/rawfalse/callerfalse | 462.45 [455.00, 479.70] | 457.85 [452.50, 462.10] | -0.99% | 0.310 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/8-4/rawfalse/callertrue | 637.30 [630.00, 661.20] | 611.85 [608.00, 613.80] | -3.99% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/8-4/rawtrue/callerfalse | 416.95 [408.60, 422.90] | 401.05 [398.60, 412.70] | -3.81% | 0.009 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/8-4/rawtrue/callertrue | 594.10 [589.50, 603.40] | 555.90 [549.40, 573.50] | -6.43% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/16-8/rawfalse/callerfalse | 596.30 [595.20, 621.50] | 594.95 [589.20, 598.40] | -0.23% | 0.050 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/16-8/rawfalse/callertrue | 789.95 [789.40, 802.50] | 759.55 [755.50, 767.30] | -3.85% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/16-8/rawtrue/callerfalse | 537.95 [534.10, 550.60] | 478.95 [473.00, 504.50] | -10.97% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedInvoke/16-8/rawtrue/callertrue | 730.70 [723.60, 741.70] | 660.05 [645.30, 672.90] | -9.67% | 0.002 | 0 / 0 | 0 / 0 |

### Whole-invocation controls

| Benchmark | A ns/op [min, max] | B ns/op [min, max] | Delta | p | B/op A / B | allocs/op A / B |
|---|---:|---:|---:|---:|---:|---:|
| InvokeHostFuncDirect | 499.40 [497.50, 501.80] | 495.40 [489.50, 501.60] | -0.80% | 0.037 | 64 / 64 | 1 / 1 |
| CallerArity/0-0/legacy | 492.75 [487.40, 523.80] | 488.65 [482.80, 511.40] | -0.83% | 0.240 | 64 / 64 | 1 / 1 |
| CallerArity/0-0/caller | 451.55 [446.00, 463.30] | 449.90 [448.80, 467.80] | -0.37% | 0.974 | 0 / 0 | 0 / 0 |
| CallerArity/0-0/call | 293.10 [292.00, 299.40] | 313.40 [311.90, 316.30] | +6.93% | 0.002 | 0 / 0 | 0 / 0 |
| CallerArity/1-0/legacy | 504.30 [496.00, 512.70] | 496.50 [490.00, 499.80] | -1.55% | 0.013 | 64 / 64 | 1 / 1 |
| CallerArity/1-0/caller | 460.20 [450.90, 470.00] | 456.35 [453.00, 463.90] | -0.84% | 0.485 | 0 / 0 | 0 / 0 |
| CallerArity/1-0/call | 306.90 [305.80, 318.90] | 318.45 [313.50, 324.90] | +3.76% | 0.026 | 0 / 0 | 0 / 0 |
| CallerArity/1-1/legacy | 508.30 [495.70, 523.10] | 502.60 [493.10, 517.60] | -1.12% | 0.485 | 64 / 64 | 1 / 1 |
| CallerArity/1-1/caller | 463.80 [456.80, 483.30] | 456.95 [453.60, 459.40] | -1.48% | 0.015 | 0 / 0 | 0 / 0 |
| CallerArity/1-1/call | 315.95 [310.90, 325.90] | 320.60 [317.90, 333.60] | +1.47% | 0.065 | 0 / 0 | 0 / 0 |
| CallerArity/4-1/legacy | 514.65 [511.20, 530.60] | 513.70 [505.60, 519.90] | -0.18% | 0.485 | 64 / 64 | 1 / 1 |
| CallerArity/4-1/caller | 478.35 [473.50, 483.90] | 479.20 [466.00, 490.40] | +0.18% | 1.000 | 0 / 0 | 0 / 0 |
| CallerArity/4-1/call | 321.75 [319.60, 327.70] | 326.35 [324.00, 329.10] | +1.43% | 0.093 | 0 / 0 | 0 / 0 |
| CallerArity/8-4/legacy | 543.30 [537.10, 561.70] | 538.00 [531.70, 546.70] | -0.98% | 0.084 | 64 / 64 | 1 / 1 |
| CallerArity/8-4/caller | 496.65 [492.70, 505.60] | 486.75 [485.90, 495.60] | -1.99% | 0.015 | 0 / 0 | 0 / 0 |
| CallerArity/8-4/call | 330.05 [325.90, 340.30] | 331.60 [330.90, 340.80] | +0.47% | 0.589 | 0 / 0 | 0 / 0 |
| CallerArity/16-8/legacy | 579.95 [578.70, 594.90] | 574.30 [570.50, 581.00] | -0.97% | 0.026 | 64 / 64 | 1 / 1 |
| CallerArity/16-8/caller | 539.15 [526.80, 552.60] | 524.10 [519.90, 544.00] | -2.79% | 0.132 | 0 / 0 | 0 / 0 |
| CallerArity/16-8/call | 350.65 [348.80, 357.50] | 354.10 [350.90, 363.40] | +0.98% | 0.093 | 0 / 0 | 0 / 0 |
| CallerArity/24-12/legacy | 611.05 [606.20, 625.90] | 613.00 [596.50, 623.80] | +0.32% | 0.589 | 64 / 64 | 1 / 1 |
| CallerArity/24-12/caller | 566.80 [557.40, 577.10] | 556.25 [554.50, 573.00] | -1.86% | 0.089 | 0 / 0 | 0 / 0 |
| CallerArity/24-12/call | 374.25 [364.50, 381.30] | 380.95 [373.70, 384.10] | +1.79% | 0.071 | 0 / 0 | 0 / 0 |
| CallerArity/32-16/legacy | 639.85 [632.50, 646.00] | 637.55 [627.90, 646.20] | -0.36% | 0.485 | 64 / 64 | 1 / 1 |
| CallerArity/32-16/caller | 599.05 [593.60, 618.50] | 590.70 [585.00, 599.70] | -1.39% | 0.065 | 0 / 0 | 0 / 0 |
| CallerArity/32-16/call | 396.25 [390.50, 400.50] | 402.05 [397.60, 416.20] | +1.46% | 0.037 | 0 / 0 | 0 / 0 |
| CallerArity/32-32/legacy | 697.25 [690.90, 720.40] | 672.85 [668.90, 677.80] | -3.50% | 0.002 | 64 / 64 | 1 / 1 |
| CallerArity/32-32/caller | 655.05 [646.20, 664.50] | 641.80 [637.80, 644.40] | -2.02% | 0.002 | 0 / 0 | 0 / 0 |
| CallerArity/32-32/call | 436.70 [431.90, 447.70] | 440.40 [431.50, 451.20] | +0.85% | 0.589 | 0 / 0 | 0 / 0 |
| CallerArity/48-24/legacy | 713.85 [709.30, 723.30] | 701.35 [694.30, 715.10] | -1.75% | 0.015 | 64 / 64 | 1 / 1 |
| CallerArity/48-24/caller | 672.50 [661.00, 690.50] | 659.50 [657.10, 666.40] | -1.93% | 0.015 | 0 / 0 | 0 / 0 |
| CallerArity/48-24/call | 444.15 [438.40, 449.60] | 452.05 [448.40, 466.10] | +1.78% | 0.006 | 0 / 0 | 0 / 0 |
| CallerArity/48-48/legacy | 799.85 [791.30, 883.30] | 779.40 [774.50, 801.30] | -2.56% | 0.026 | 64 / 64 | 1 / 1 |
| CallerArity/48-48/caller | 752.70 [744.60, 766.80] | 740.50 [734.70, 765.90] | -1.62% | 0.041 | 0 / 0 | 0 / 0 |
| CallerArity/48-48/call | 494.00 [490.10, 510.00] | 503.40 [500.00, 507.50] | +1.90% | 0.065 | 0 / 0 | 0 / 0 |
| CallerArity/64-64/legacy | 889.05 [885.20, 893.30] | 868.85 [863.80, 879.10] | -2.27% | 0.002 | 64 / 64 | 1 / 1 |
| CallerArity/64-64/caller | 864.30 [846.00, 883.90] | 832.45 [827.40, 873.80] | -3.69% | 0.065 | 0 / 0 | 0 / 0 |
| CallerArity/64-64/call | 563.70 [555.70, 586.40] | 566.40 [561.80, 572.60] | +0.48% | 0.394 | 0 / 0 | 0 / 0 |
| CallerGCLoop/concretefalse/n0 | 450.65 [448.10, 467.20] | 445.35 [440.70, 451.00] | -1.18% | 0.037 | 0 / 0 | 0 / 0 |
| CallerGCLoop/concretefalse/n1 | 838.75 [827.90, 851.80] | 834.95 [832.00, 853.70] | -0.45% | 0.818 | 64 / 64 | 1 / 1 |
| CallerGCLoop/concretefalse/n8 | 2836.00 [2799.00, 3019.00] | 2808.50 [2780.00, 2854.00] | -0.97% | 0.169 | 512 / 512 | 8 / 8 |
| CallerGCLoop/concretefalse/n64 | 18845.50 [18549.00, 20566.00] | 18085.50 [17690.00, 18774.00] | -4.03% | 0.015 | 4096 / 4096 | 64 / 64 |
| CallerGCLoop/concretefalse/n1024 | 288305.00 [285008.00, 296641.00] | 274970.00 [271879.00, 283793.00] | -4.63% | 0.002 | 65536 / 65536 | 1024 / 1024 |
| CallerGCLoop/concretetrue/n0 | 450.60 [446.60, 461.90] | 446.75 [444.10, 461.70] | -0.85% | 0.240 | 0 / 0 | 0 / 0 |
| CallerGCLoop/concretetrue/n1 | 808.75 [797.90, 840.70] | 800.45 [789.20, 833.40] | -1.03% | 0.394 | 0 / 0 | 0 / 0 |
| CallerGCLoop/concretetrue/n8 | 2623.00 [2575.00, 2652.00] | 2574.00 [2526.00, 2624.00] | -1.87% | 0.045 | 0 / 0 | 0 / 0 |
| CallerGCLoop/concretetrue/n64 | 16596.00 [16482.00, 17247.00] | 15960.50 [15870.00, 16442.00] | -3.83% | 0.002 | 0 / 0 | 0 / 0 |
| CallerGCLoop/concretetrue/n1024 | 255394.00 [252937.00, 289024.00] | 243703.00 [241545.00, 245249.00] | -4.58% | 0.002 | 0 / 0 | 0 / 0 |
| CallerDomainLoop/dynamicfalse/n0 | 216.55 [214.60, 220.00] | 208.95 [205.90, 219.30] | -3.51% | 0.039 | 0 / 0 | 0 / 0 |
| CallerDomainLoop/dynamicfalse/n1 | 573.40 [568.50, 587.90] | 567.35 [563.10, 570.20] | -1.06% | 0.013 | 0 / 0 | 0 / 0 |
| CallerDomainLoop/dynamicfalse/n8 | 1930.50 [1922.00, 1957.00] | 1944.00 [1926.00, 2021.00] | +0.70% | 0.258 | 0 / 0 | 0 / 0 |
| CallerDomainLoop/dynamicfalse/n64 | 12436.50 [12268.00, 12577.00] | 12374.00 [12317.00, 12762.00] | -0.50% | 1.000 | 0 / 0 | 0 / 0 |
| CallerDomainLoop/dynamicfalse/n1024 | 191407.00 [190382.00, 196825.00] | 190539.00 [188142.00, 192452.00] | -0.45% | 0.240 | 0 / 0 | 0 / 0 |
| CallerDomainLoop/dynamictrue/n0 | 337.40 [334.00, 357.20] | 325.90 [322.60, 338.90] | -3.41% | 0.015 | 16 / 16 | 1 / 1 |
| CallerDomainLoop/dynamictrue/n1 | 721.10 [714.00, 744.20] | 699.85 [693.10, 723.00] | -2.95% | 0.024 | 16 / 16 | 1 / 1 |
| CallerDomainLoop/dynamictrue/n8 | 2242.50 [2205.00, 2330.00] | 2182.00 [2163.00, 2193.00] | -2.70% | 0.002 | 16 / 16 | 1 / 1 |
| CallerDomainLoop/dynamictrue/n64 | 13846.00 [13775.00, 14017.00] | 13595.00 [13518.00, 13662.00] | -1.81% | 0.002 | 16 / 16 | 1 / 1 |
| CallerDomainLoop/dynamictrue/n1024 | 213402.00 [210863.00, 224167.00] | 208804.50 [207875.00, 210953.00] | -2.15% | 0.004 | 16 / 16 | 1 / 1 |
| InvokeCallerHostFuncDirect | 459.70 [454.00, 464.80] | 462.60 [454.00, 479.70] | +0.63% | 0.515 | 0 / 0 | 0 / 0 |

### Matched host and guest loop controls

| Benchmark | A ns/op [min, max] | B ns/op [min, max] | Delta | p | B/op A / B | allocs/op A / B |
|---|---:|---:|---:|---:|---:|---:|
| HostRoundtripLoop/mem0/parallelfalse/host0/n0 | 165.85 [164.50, 182.00] | 170.30 [168.90, 171.70] | +2.68% | 0.065 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/parallelfalse/host0/n1 | 166.45 [164.30, 177.50] | 170.80 [169.90, 175.70] | +2.61% | 0.093 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/parallelfalse/host0/n8 | 169.45 [168.30, 173.40] | 174.35 [172.50, 178.50] | +2.89% | 0.009 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/parallelfalse/host0/n64 | 193.20 [191.60, 204.20] | 195.95 [195.30, 198.30] | +1.42% | 0.058 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/parallelfalse/host0/n1024 | 612.75 [599.90, 628.70] | 611.55 [606.80, 617.90] | -0.20% | 1.000 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/parallelfalse/host1/n0 | 165.45 [164.10, 176.90] | 170.60 [169.50, 171.40] | +3.11% | 0.084 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/parallelfalse/host1/n1 | 505.25 [500.40, 515.80] | 501.25 [494.60, 517.10] | -0.79% | 0.240 | 64 / 64 | 1 / 1 |
| HostRoundtripLoop/mem0/parallelfalse/host1/n8 | 1709.00 [1679.00, 1768.00] | 1702.00 [1695.00, 1767.00] | -0.41% | 0.965 | 512 / 512 | 8 / 8 |
| HostRoundtripLoop/mem0/parallelfalse/host1/n64 | 11193.50 [11019.00, 11879.00] | 11088.50 [10934.00, 11198.00] | -0.94% | 0.145 | 4096 / 4096 | 64 / 64 |
| HostRoundtripLoop/mem0/parallelfalse/host1/n1024 | 170582.50 [168222.00, 177554.00] | 172468.00 [167793.00, 176540.00] | +1.11% | 0.818 | 65536 / 65536 | 1024 / 1024 |
| HostRoundtripLoop/mem0/paralleltrue/host0/n0 | 86.31 [81.85, 87.76] | 89.74 [86.19, 91.68] | +3.97% | 0.093 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/paralleltrue/host0/n1 | 83.35 [82.26, 88.67] | 87.36 [86.39, 89.71] | +4.81% | 0.093 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/paralleltrue/host0/n8 | 84.28 [83.67, 87.73] | 89.22 [88.16, 92.62] | +5.87% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/paralleltrue/host0/n64 | 96.88 [96.26, 103.40] | 103.85 [100.50, 104.70] | +7.19% | 0.009 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/paralleltrue/host0/n1024 | 311.25 [310.20, 316.80] | 314.30 [312.70, 316.30] | +0.98% | 0.093 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/paralleltrue/host1/n0 | 83.56 [82.09, 87.83] | 87.50 [86.46, 92.06] | +4.72% | 0.015 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem0/paralleltrue/host1/n1 | 218.45 [215.70, 223.30] | 219.85 [217.70, 223.00] | +0.64% | 0.513 | 64 / 64 | 1 / 1 |
| HostRoundtripLoop/mem0/paralleltrue/host1/n8 | 771.30 [766.60, 786.90] | 766.60 [765.00, 767.60] | -0.61% | 0.015 | 512 / 512 | 8 / 8 |
| HostRoundtripLoop/mem0/paralleltrue/host1/n64 | 5198.00 [5126.00, 5247.00] | 5111.00 [5064.00, 5152.00] | -1.67% | 0.013 | 4096 / 4096 | 64 / 64 |
| HostRoundtripLoop/mem0/paralleltrue/host1/n1024 | 80445.50 [80118.00, 82025.00] | 79980.50 [78781.00, 82386.00] | -0.58% | 0.699 | 65536-65537 / 65536-65537 | 1024 / 1024 |
| HostRoundtripLoop/mem1/parallelfalse/host0/n0 | 166.70 [164.40, 173.40] | 170.90 [169.90, 176.20] | +2.52% | 0.041 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/parallelfalse/host0/n1 | 166.05 [165.50, 175.40] | 170.70 [167.90, 172.50] | +2.80% | 0.065 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/parallelfalse/host0/n8 | 167.45 [166.70, 171.70] | 172.35 [170.90, 174.30] | +2.93% | 0.009 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/parallelfalse/host0/n64 | 192.00 [190.30, 200.40] | 197.00 [194.80, 256.40] | +2.60% | 0.093 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/parallelfalse/host0/n1024 | 614.75 [601.40, 619.00] | 615.25 [607.40, 633.50] | +0.08% | 0.589 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/parallelfalse/host1/n0 | 165.30 [163.40, 174.40] | 170.10 [169.40, 171.10] | +2.90% | 0.225 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/parallelfalse/host1/n1 | 508.45 [502.70, 509.20] | 500.05 [497.50, 506.20] | -1.65% | 0.009 | 64 / 64 | 1 / 1 |
| HostRoundtripLoop/mem1/parallelfalse/host1/n8 | 1714.00 [1711.00, 1738.00] | 1699.50 [1682.00, 1724.00] | -0.85% | 0.234 | 512 / 512 | 8 / 8 |
| HostRoundtripLoop/mem1/parallelfalse/host1/n64 | 11162.00 [10922.00, 11616.00] | 11121.00 [10978.00, 11240.00] | -0.37% | 0.485 | 4096 / 4096 | 64 / 64 |
| HostRoundtripLoop/mem1/parallelfalse/host1/n1024 | 172354.50 [170138.00, 173419.00] | 169839.00 [167560.00, 171257.00] | -1.46% | 0.015 | 65536 / 65536 | 1024 / 1024 |
| HostRoundtripLoop/mem1/paralleltrue/host0/n0 | 82.66 [81.57, 84.10] | 86.96 [86.00, 89.99] | +5.20% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/paralleltrue/host0/n1 | 83.97 [82.51, 86.33] | 87.01 [86.29, 90.62] | +3.61% | 0.009 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/paralleltrue/host0/n8 | 86.44 [83.74, 88.92] | 91.53 [88.27, 93.43] | +5.89% | 0.015 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/paralleltrue/host0/n64 | 97.12 [96.36, 100.60] | 102.55 [101.00, 106.50] | +5.59% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/paralleltrue/host0/n1024 | 314.10 [308.40, 320.60] | 316.45 [314.10, 321.30] | +0.75% | 0.223 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/paralleltrue/host1/n0 | 86.13 [82.00, 88.05] | 86.47 [86.12, 86.89] | +0.39% | 0.310 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem1/paralleltrue/host1/n1 | 218.35 [216.50, 222.40] | 220.60 [218.90, 227.40] | +1.03% | 0.221 | 64 / 64 | 1 / 1 |
| HostRoundtripLoop/mem1/paralleltrue/host1/n8 | 776.30 [768.70, 786.30] | 767.65 [759.90, 787.30] | -1.11% | 0.087 | 512 / 512 | 8 / 8 |
| HostRoundtripLoop/mem1/paralleltrue/host1/n64 | 5208.00 [5139.00, 5269.00] | 5170.50 [5088.00, 5195.00] | -0.72% | 0.093 | 4096 / 4096 | 64 / 64 |
| HostRoundtripLoop/mem1/paralleltrue/host1/n1024 | 80693.50 [80056.00, 80865.00] | 79613.00 [78821.00, 80528.00] | -1.34% | 0.009 | 65536 / 65536 | 1024 / 1024 |
| HostRoundtripLoop/mem4/parallelfalse/host0/n0 | 182.90 [180.70, 190.30] | 197.70 [196.70, 221.00] | +8.09% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/parallelfalse/host0/n1 | 182.60 [181.10, 192.10] | 197.85 [196.70, 202.40] | +8.35% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/parallelfalse/host0/n8 | 186.85 [185.80, 189.80] | 201.85 [197.30, 226.00] | +8.03% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/parallelfalse/host0/n64 | 211.70 [210.90, 222.20] | 224.20 [222.50, 238.00] | +5.90% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/parallelfalse/host0/n1024 | 629.65 [622.20, 641.70] | 645.95 [638.80, 664.90] | +2.59% | 0.004 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/parallelfalse/host1/n0 | 181.70 [179.90, 192.20] | 195.90 [192.30, 222.00] | +7.82% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/parallelfalse/host1/n1 | 531.00 [527.30, 540.40] | 531.70 [527.60, 555.20] | +0.13% | 0.818 | 64 / 64 | 1 / 1 |
| HostRoundtripLoop/mem4/parallelfalse/host1/n8 | 1748.00 [1719.00, 1773.00] | 1746.50 [1712.00, 1766.00] | -0.09% | 1.000 | 512 / 512 | 8 / 8 |
| HostRoundtripLoop/mem4/parallelfalse/host1/n64 | 11303.00 [11210.00, 11465.00] | 11126.50 [11010.00, 11279.00] | -1.56% | 0.015 | 4096 / 4096 | 64 / 64 |
| HostRoundtripLoop/mem4/parallelfalse/host1/n1024 | 172643.50 [170486.00, 174753.00] | 172855.50 [169704.00, 177461.00] | +0.12% | 0.699 | 65536 / 65536 | 1024 / 1024 |
| HostRoundtripLoop/mem4/paralleltrue/host0/n0 | 97.34 [93.62, 102.30] | 102.25 [101.10, 108.00] | +5.04% | 0.015 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/paralleltrue/host0/n1 | 97.70 [95.64, 101.70] | 102.60 [101.30, 106.50] | +5.01% | 0.009 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/paralleltrue/host0/n8 | 97.29 [95.09, 98.00] | 105.30 [103.20, 108.90] | +8.23% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/paralleltrue/host0/n64 | 112.60 [109.40, 113.90] | 118.10 [116.10, 120.50] | +4.88% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/paralleltrue/host0/n1024 | 327.30 [321.80, 330.50] | 331.35 [329.50, 339.10] | +1.24% | 0.011 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/paralleltrue/host1/n0 | 96.82 [95.00, 101.60] | 103.30 [101.00, 106.00] | +6.69% | 0.004 | 0 / 0 | 0 / 0 |
| HostRoundtripLoop/mem4/paralleltrue/host1/n1 | 235.25 [232.20, 239.20] | 239.10 [235.80, 244.80] | +1.64% | 0.061 | 64 / 64 | 1 / 1 |
| HostRoundtripLoop/mem4/paralleltrue/host1/n8 | 788.00 [783.60, 795.30] | 784.40 [780.40, 791.50] | -0.46% | 0.132 | 512 / 512 | 8 / 8 |
| HostRoundtripLoop/mem4/paralleltrue/host1/n64 | 5184.50 [5159.00, 5232.00] | 5160.50 [5105.00, 5197.00] | -0.46% | 0.180 | 4096 / 4096 | 64 / 64 |
| HostRoundtripLoop/mem4/paralleltrue/host1/n1024 | 80973.00 [80200.00, 82451.00] | 79420.00 [78810.00, 80672.00] | -1.92% | 0.009 | 65536-65537 / 65536-65537 | 1024 / 1024 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host0/n0 | 166.35 [164.20, 174.90] | 171.50 [169.40, 172.60] | +3.10% | 0.065 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host0/n1 | 166.35 [163.90, 175.30] | 171.45 [169.20, 176.60] | +3.07% | 0.041 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host0/n8 | 169.35 [168.00, 172.50] | 172.85 [171.50, 178.20] | +2.07% | 0.009 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host0/n64 | 194.20 [190.70, 201.50] | 196.55 [194.30, 197.90] | +1.21% | 0.485 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host0/n1024 | 613.55 [601.60, 624.60] | 619.50 [609.00, 644.30] | +0.97% | 0.331 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host1/n0 | 166.15 [163.30, 175.40] | 169.85 [169.00, 175.00] | +2.23% | 0.093 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host1/n1 | 463.35 [458.90, 468.80] | 463.50 [460.10, 469.70] | +0.03% | 0.818 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host1/n8 | 1520.50 [1489.00, 1567.00] | 1529.00 [1512.00, 1567.00] | +0.56% | 0.623 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host1/n64 | 9553.50 [9440.00, 10155.00] | 9778.00 [9692.00, 9822.00] | +2.35% | 0.058 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/parallelfalse/host1/n1024 | 145954.50 [145221.00, 150591.00] | 150995.50 [148596.00, 153705.00] | +3.45% | 0.009 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host0/n0 | 82.16 [81.56, 85.84] | 86.41 [85.69, 91.30] | +5.17% | 0.004 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host0/n1 | 83.94 [82.23, 88.16] | 87.76 [86.17, 90.01] | +4.55% | 0.041 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host0/n8 | 84.29 [83.46, 85.13] | 89.19 [88.25, 91.84] | +5.81% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host0/n64 | 97.17 [96.26, 100.90] | 100.90 [100.40, 102.90] | +3.84% | 0.015 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host0/n1024 | 313.00 [309.90, 320.90] | 317.15 [315.30, 340.40] | +1.33% | 0.093 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host1/n0 | 83.75 [82.06, 86.59] | 86.25 [86.19, 87.31] | +2.99% | 0.026 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host1/n1 | 198.65 [197.00, 207.00] | 201.90 [199.80, 206.80] | +1.64% | 0.132 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host1/n8 | 672.00 [669.30, 680.20] | 674.75 [671.60, 689.60] | +0.41% | 0.258 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host1/n64 | 4387.50 [4344.00, 4400.00] | 4404.00 [4386.00, 4439.00] | +0.38% | 0.026 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem0/paralleltrue/host1/n1024 | 67396.00 [66696.00, 68126.00] | 67322.00 [67028.00, 68204.00] | -0.11% | 0.937 | 0 / 0-1 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host0/n0 | 165.50 [164.60, 169.60] | 171.25 [169.80, 173.10] | +3.47% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host0/n1 | 166.05 [163.40, 180.20] | 171.00 [169.20, 172.50] | +2.98% | 0.065 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host0/n8 | 167.90 [167.00, 169.90] | 172.80 [172.10, 180.60] | +2.92% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host0/n64 | 192.20 [188.90, 200.10] | 199.35 [195.90, 209.00] | +3.72% | 0.017 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host0/n1024 | 616.10 [599.30, 644.70] | 614.30 [606.80, 820.50] | -0.29% | 0.589 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host1/n0 | 164.45 [163.50, 174.20] | 170.05 [168.10, 174.30] | +3.41% | 0.093 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host1/n1 | 464.85 [455.10, 475.80] | 458.05 [456.10, 469.50] | -1.46% | 0.310 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host1/n8 | 1512.50 [1498.00, 1533.00] | 1527.50 [1515.00, 1556.00] | +0.99% | 0.041 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host1/n64 | 9680.00 [9543.00, 10061.00] | 9818.50 [9736.00, 9970.00] | +1.43% | 0.065 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/parallelfalse/host1/n1024 | 147427.00 [145959.00, 151527.00] | 151331.50 [149534.00, 153594.00] | +2.65% | 0.041 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host0/n0 | 83.06 [81.82, 85.86] | 86.19 [85.84, 87.62] | +3.77% | 0.004 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host0/n1 | 83.52 [82.52, 86.68] | 86.81 [86.23, 89.83] | +3.94% | 0.015 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host0/n8 | 86.69 [84.07, 88.92] | 89.24 [88.14, 92.14] | +2.94% | 0.041 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host0/n64 | 97.72 [97.02, 101.00] | 100.70 [100.30, 101.80] | +3.05% | 0.119 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host0/n1024 | 313.80 [308.60, 318.00] | 317.05 [314.30, 319.30] | +1.04% | 0.045 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host1/n0 | 82.90 [82.00, 86.76] | 87.39 [86.31, 90.43] | +5.42% | 0.015 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host1/n1 | 198.95 [196.50, 205.70] | 203.80 [199.00, 205.50] | +2.44% | 0.180 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host1/n8 | 671.80 [667.50, 683.40] | 678.40 [673.20, 683.40] | +0.98% | 0.097 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host1/n64 | 4376.00 [4324.00, 4438.00] | 4346.00 [4339.00, 4413.00] | -0.69% | 0.457 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem1/paralleltrue/host1/n1024 | 67568.00 [66892.00, 67736.00] | 67831.00 [66972.00, 68972.00] | +0.39% | 0.485 | 0-1 / 0-1 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host0/n0 | 183.80 [181.70, 193.50] | 195.80 [195.60, 197.60] | +6.53% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host0/n1 | 181.20 [180.20, 191.10] | 196.85 [194.60, 203.20] | +8.64% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host0/n8 | 190.25 [185.30, 195.40] | 200.95 [199.70, 202.90] | +5.62% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host0/n64 | 211.45 [208.70, 217.10] | 224.75 [222.90, 232.70] | +6.29% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host0/n1024 | 635.90 [624.30, 653.70] | 649.90 [637.70, 841.40] | +2.20% | 0.065 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host1/n0 | 181.95 [178.30, 185.10] | 196.25 [193.80, 199.90] | +7.86% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host1/n1 | 492.65 [487.20, 502.50] | 493.00 [489.30, 505.00] | +0.07% | 0.589 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host1/n8 | 1537.50 [1530.00, 1547.00] | 1551.00 [1540.00, 1560.00] | +0.88% | 0.017 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host1/n64 | 9671.50 [9589.00, 10037.00] | 9896.50 [9816.00, 10579.00] | +2.33% | 0.041 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/parallelfalse/host1/n1024 | 147901.00 [145432.00, 156365.00] | 151003.00 [150633.00, 152015.00] | +2.10% | 0.065 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host0/n0 | 99.06 [95.92, 100.30] | 105.75 [100.40, 108.10] | +6.75% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host0/n1 | 99.38 [95.44, 100.20] | 104.15 [101.80, 108.30] | +4.81% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host0/n8 | 97.99 [95.90, 103.00] | 107.25 [105.00, 109.50] | +9.45% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host0/n64 | 111.00 [109.90, 115.90] | 118.10 [116.70, 121.70] | +6.40% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host0/n1024 | 329.40 [325.20, 336.20] | 331.60 [328.50, 348.00] | +0.67% | 0.132 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host1/n0 | 97.40 [93.35, 100.30] | 105.45 [103.20, 109.10] | +8.27% | 0.002 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host1/n1 | 217.20 [214.70, 219.80] | 218.95 [216.20, 226.60] | +0.81% | 0.485 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host1/n8 | 687.40 [682.80, 699.10] | 693.25 [687.10, 709.20] | +0.85% | 0.093 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host1/n64 | 4391.50 [4360.00, 4459.00] | 4387.50 [4378.00, 4450.00] | -0.09% | 0.667 | 0 / 0 | 0 / 0 |
| HostRoundtripLoopCaller/mem4/paralleltrue/host1/n1024 | 67548.50 [67049.00, 68299.00] | 67538.50 [66924.00, 67967.00] | -0.01% | 1.000 | 0 / 0 | 0 / 0 |

## Matched loop slopes

For each sample: `((host1024-host0)-(guest1024-guest0))/1024` on the same export.
These are net ns/callback after subtracting the matched guest slope. Parallel
rows measure aggregate throughput across independent instances, not latency.

| Loop | A net ns/callback [min, max] | B net ns/callback [min, max] |
|---|---:|---:|
| HostRoundtripLoop/mem0/parallelfalse | 165.98 [163.67, 172.80] | 167.83 [163.27, 171.80] |
| HostRoundtripLoop/mem0/paralleltrue | 78.26 [77.94, 79.80] | 77.80 [76.63, 80.15] |
| HostRoundtripLoop/mem1/parallelfalse | 167.72 [165.55, 168.75] | 165.25 [163.01, 166.65] |
| HostRoundtripLoop/mem1/paralleltrue | 78.49 [77.88, 78.66] | 77.44 [76.67, 78.33] |
| HostRoundtripLoop/mem4/parallelfalse | 167.98 [165.87, 170.05] | 168.18 [165.08, 172.66] |
| HostRoundtripLoop/mem4/paralleltrue | 78.76 [78.00, 80.20] | 77.23 [76.63, 78.46] |
| HostRoundtripLoopCaller/mem0/parallelfalse | 141.93 [141.22, 146.45] | 146.83 [144.52, 149.49] |
| HostRoundtripLoopCaller/mem0/paralleltrue | 65.51 [64.83, 66.21] | 65.44 [65.12, 66.28] |
| HostRoundtripLoopCaller/mem1/parallelfalse | 143.37 [141.91, 147.37] | 147.09 [145.44, 149.39] |
| HostRoundtripLoopCaller/mem1/paralleltrue | 65.68 [65.02, 65.85] | 65.93 [65.09, 67.04] |
| HostRoundtripLoopCaller/mem4/parallelfalse | 143.81 [141.42, 152.09] | 146.82 [146.29, 147.82] |
| HostRoundtripLoopCaller/mem4/paralleltrue | 65.64 [65.16, 66.38] | 65.63 [65.03, 66.05] |

## Fresh-build repeats (500 ms samples)

| Benchmark | A ns/op [min, max] | B ns/op [min, max] | Delta | p | B/op A / B | allocs/op A / B |
|---|---:|---:|---:|---:|---:|---:|
| CallerArity/0-0/legacy | 493.10 [484.50, 495.60] | 488.80 [482.60, 502.40] | -0.87% | 0.240 | 64 / 64 | 1 / 1 |
| CallerArity/0-0/caller | 449.45 [446.80, 451.40] | 458.65 [456.70, 461.70] | +2.05% | 0.002 | 0 / 0 | 0 / 0 |
| CallerArity/0-0/call | 293.70 [292.60, 296.70] | 316.35 [312.70, 318.20] | +7.71% | 0.002 | 0 / 0 | 0 / 0 |
| CallerArity/1-0/legacy | 497.55 [493.10, 501.40] | 496.60 [490.00, 514.10] | -0.19% | 0.818 | 64 / 64 | 1 / 1 |
| CallerArity/1-0/caller | 453.85 [451.30, 456.90] | 465.10 [463.10, 471.70] | +2.48% | 0.002 | 0 / 0 | 0 / 0 |
| CallerArity/1-0/call | 306.55 [305.90, 307.10] | 322.70 [322.00, 324.00] | +5.27% | 0.002 | 0 / 0 | 0 / 0 |
| CallerArity/1-1/legacy | 500.10 [496.00, 503.50] | 496.50 [490.30, 502.10] | -0.72% | 0.310 | 64 / 64 | 1 / 1 |
| CallerArity/1-1/caller | 458.65 [456.50, 464.50] | 467.50 [463.10, 470.10] | +1.93% | 0.009 | 0 / 0 | 0 / 0 |
| CallerArity/1-1/call | 315.50 [314.70, 316.50] | 322.20 [318.20, 324.00] | +2.12% | 0.002 | 0 / 0 | 0 / 0 |
| CallerArity/8-4/legacy | 532.25 [531.00, 539.90] | 533.90 [530.80, 540.20] | +0.31% | 0.589 | 64 / 64 | 1 / 1 |
| CallerArity/8-4/caller | 494.80 [488.40, 500.40] | 498.65 [493.50, 502.20] | +0.78% | 0.236 | 0 / 0 | 0 / 0 |
| CallerArity/8-4/call | 329.55 [326.90, 332.60] | 335.35 [333.10, 337.60] | +1.76% | 0.002 | 0 / 0 | 0 / 0 |
| CallerArity/16-8/legacy | 575.85 [574.30, 580.40] | 569.10 [566.10, 572.00] | -1.17% | 0.002 | 64 / 64 | 1 / 1 |
| CallerArity/16-8/caller | 535.95 [527.30, 537.50] | 533.15 [532.80, 536.00] | -0.52% | 0.411 | 0 / 0 | 0 / 0 |
| CallerArity/16-8/call | 353.70 [349.60, 356.20] | 357.35 [354.70, 360.20] | +1.03% | 0.015 | 0 / 0 | 0 / 0 |

| Benchmark | A ns/op [min, max] | B ns/op [min, max] | Delta | p | B/op A / B | allocs/op A / B |
|---|---:|---:|---:|---:|---:|---:|
| HostCallIndexedMixed/arity4 | 37.99 [37.90, 38.76] | 38.61 [38.42, 38.73] | +1.63% | 0.061 | 0 / 0 | 0 / 0 |
| HostCallIndexedMixed/arity16 | 206.20 [205.00, 206.60] | 211.70 [210.70, 212.20] | +2.67% | 0.002 | 0 / 0 | 0 / 0 |
| HostCallIndexedMixed/arity64 | 1978.50 [1974.00, 1986.00] | 2000.00 [1994.00, 2016.00] | +1.09% | 0.002 | 0 / 0 | 0 / 0 |

## Reproduction

Build test binaries in separate worktrees. Keep the owned-signature correction
and all benchmark source identical; restore only the four accessor bodies from
main for A. Build with `go test -c ./src/wago -o /tmp/VARIANT.test`; use `-a` for
the fresh-build repeat. Run binaries from `src/wago` so fixtures resolve.
For each of six pairs, alternate AB/BA and use:

```sh
GOMAXPROCS=4 taskset -c 4-7 /tmp/VARIANT.test -test.run '^$' \
  -test.bench '^Benchmark(HostCallIndexed|HostCallIndexedMixed|HostCallIndexedInvoke|CallerArity|InvokeHostFuncDirect|InvokeCallerHostFuncDirect|HostRoundtripLoop|HostRoundtripLoopCaller|CallerGCLoop|CallerDomainLoop)$' \
  -test.benchmem -test.benchtime=100ms -test.count=1
```

Repeat `BenchmarkCallerArity` for arities 0-0, 1-0, 1-1, 8-4, 16-8 at 500 ms;
run `BenchmarkHostCallIndexedMixed` separately at 500 ms, also with six pairs.
Use `benchstat` on each variant's six outputs. Profiles use the zero-value
`CallerArity/0-0/call` control at five seconds, outside all timing runs.

AI assistance was used to inspect construction paths, write tests, run checks,
and prepare the measurements and this report. No public ABI, dispatch design,
signature cache, or native v128 support changes are included.
