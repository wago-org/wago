# Native batch cancellation

AMD64 reserves 512 handles for native structs. Array helper calls can cancel the
batch after only one or two handles are used. Cancellation returns and checks
the unused handles, scans the nursery tail, and clears the batch. Repeated mixed
allocations therefore do work proportional to the reserved batch rather than
the number of objects. A smaller fixed batch helps mixed code but hurts long
struct-only runs.

## Reproduction

`tests/fixtures/wasm/gc-allocation-mix.wat` retains a nested struct across
`array.new_fixed`, `array.new_default`, or `array.new`. Each export returns a
checksum that depends on the retained fields and array elements. The `structs`
export also exposes cancellation from reference-field struct helpers;
`scalar-structs` is the native struct-only control. Regenerate the binary with:

```sh
wasm-tools parse tests/fixtures/wasm/gc-allocation-mix.wat \
  -o tests/fixtures/wasm/gc-allocation-mix.wasm
```

The execution benchmark compiles once, enables collection and native allocation,
warms the instance, and runs 8192 guest iterations per call. Collection remains
inside the timed workload. The collector benchmark measures reservation,
publication, cancellation, and periodic collection with one or eight live
objects per canceled batch.

```sh
go test ./src/wago -run '^TestGCAllocationMix$'
go test ./src/wago -run '^$' -bench '^BenchmarkGCAllocationMix$' -benchmem -count=5
go test ./src/core/runtime/gc/native -run '^$' \
  -bench '^BenchmarkNativeStructBatchCancellation$' -benchmem -count=5
```

The batch feedback tests first fail on main: a batch canceled after one object
still reserves 512 handles on the next request. They require a small batch for
mixed allocations, recovery to the configured maximum for full struct batches,
and feedback isolated from other collectors, array chunks, and dead structs.


## Fix

Keep feedback in each collector. When a live-struct batch is canceled with less
than one quarter used, reserve four times the observed use next time, with a
minimum of eight handles. Double after 32 consecutive full batches, up to the
configured maximum. Partial use resets that run so short bursts do not regrow
large batches. Arrays and one-handle dead-struct proofs do not change this
feedback.
Choose the next size after cancellation so a full refill sees current feedback.
The first reservation keeps the configured maximum.

The three bytes of feedback fit existing collector padding. Fixed collector sizes,
existing field offsets, native allocation state, and the artifact ABI stay the
same. Cleanup and checked-handle generation rules use the existing paths. After
warmup, a one-object mixed batch returns seven unused handles instead of 511.

## Measurements

Linux/AMD64, Ryzen 7 8845HS, Go 1.27.1, 2026-10-20. Compare runtime code from
`ed76fd17957fd472b92a69d2fa3f5f63680f2cd5` with the fix, using the same fixture.
Five alternating baseline/fixed pairs, 150 ms per sample, `GOMAXPROCS=1`, CPU 15.
Times below are medians. Execution times are per 8192 guest iterations;
collector times include one reservation/cancellation cycle and periodic GC.
Use the standard ns/op metric for comparison. The optional ns/iteration field
in the saved samples was removed because TinyGo's testing API lacks Elapsed.
Other programs were active; treat the small control delta as noise, not a gain.

| Workload | Baseline | Fixed | Time delta | Speedup |
|---|---:|---:|---:|---:|
| Native scalar structs | 78.231 us | 78.796 us | +0.72% | 0.99x |
| Nested structs with helper calls | 13.171 ms | 3.334 ms | -74.69% | 3.95x |
| Structs + array.new_fixed | 15.023 ms | 4.871 ms | -67.58% | 3.08x |
| Structs + array.new_default | 14.615 ms | 4.562 ms | -68.78% | 3.20x |
| Structs + array.new | 14.943 ms | 4.835 ms | -67.64% | 3.09x |
| Batch cycle: one live object | 1306.000 ns | 47.320 ns | -96.38% | 27.60x |
| Batch cycle: eight live objects | 1354.000 ns | 143.700 ns | -89.39% | 9.42x |

[Raw samples and all median ratios](native-batch-cancellation-results/paired-summary.json)
are retained beside the note. Build benchmark test binaries from the benchmark
commit and the fix, then alternate these commands to repeat the measurements:

```sh
GOMAXPROCS=1 taskset -c 15 /path/to/wago.test -test.run='^$' \
  -test.bench='^BenchmarkGCAllocationMix$' -test.benchtime=150ms
GOMAXPROCS=1 taskset -c 15 /path/to/native.test -test.run='^$' \
  -test.bench='^BenchmarkNativeStructBatchCancellation$' -test.benchtime=150ms
```

Run each binary from its Go package directory so the checked-in fixture resolves.


## Validation

The complete `src/wago` and native collector suites pass in normal mode and
with `wago_regalloccheck`, including the pinned Core 3 GC tests. The native
collector also passes `wagodebug`, including collection-failure and cancellation
recovery tests. QEMU ARM64 passes batch adaptation, short bursts, and the fixed
collector size/offset assertions.

All three contributor-required workspace commands were run. Initial broad runs
found missing pinned test checkouts, an unavailable Core 3 reference tool, and
PowerShell's missing ICU support. The checkouts and pinned reference tool were
configured before the complete GC/runtime reruns. PowerShell ran with
`DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1`. A temporary directory inside a Git
checkout made the registry non-repository test fail; it passed with a temporary
directory outside all checkouts. Two CLI plugin publication tests intermittently
reported `text file busy` under concurrent broad runs; the complete plugin and
registry packages passed a separate register-check rerun. The benchmark module
suite passed. No checks were disabled and no expected-accounting files changed.

To run the GC/runtime suites, initialize the pinned submodules and set
`WAGO_SPEC_INTERPRETER` and `WAGO_SPEC_INTERPRETER_REVISION` using
`scripts/bootstrap-spec-interpreter.sh --print-path` and `--print-revision`.

```sh
go test ./src/wago ./src/core/runtime/gc/native
go test -tags=wago_regalloccheck ./src/wago ./src/core/runtime/gc/native
go test -tags=wagodebug ./src/core/runtime/gc/native
```


## Full Dewdrop compile on the local GC stack

Apply the same runtime change to the existing GC stack at
`e2fe43ffc83d31b16c7d82e66dc5a97782db942d`. The private test host also has the
existing worker-host integration patch; workers were disabled for these runs.
The new batch tests passed in that dependency source before the host was built.

Use the saved compiler and request recorded in
[sample identity](native-batch-cancellation-results/sustained-identity.json).
Both runs used a fresh native cache, a transform result cache miss, eight compile
workers, a 512 MiB heap, and a 64 MiB nursery. Both output Wasm and compiler cache
hashes matched. The output passed wasm-tools validation and returned 42 in Node.

| Local stacked runtime | Wall time | Peak RSS |
|---|---:|---:|
| Before | 123.955 s | 1288220 KiB |
| With batch feedback | 93.822 s | 1152748 KiB |
| Change | -24.31% | -10.52% |

This is one full-job pair on the local stack, separate from the upstream-main
microbenchmarks. Other programs were active. The full compile remains slow;
the result does not establish interactive compile latency.
