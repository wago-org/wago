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
export is the control. Regenerate the binary with:

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
