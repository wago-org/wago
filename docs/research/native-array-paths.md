# Native array paths

## Shared nursery bump

Mixed struct/array allocation previously sent every small array through Go
when a struct had prepared a chunk-free handle batch. The array stub required a
private chunk, although the batch already owned usable free identities. The
helper then canceled those identities.

The AMD64 array stub now accepts that reservation. It checks the object size
against the nursery object threshold, aligns and bounds the nursery bump, writes
the payload, advances the bump, and publishes the handle. Collection and helper
entry still cancel the batch. Cursor reconciliation counts both structs and
arrays once. Large arrays retain their exact reservation and chunk ownership.

The new `gc-shared-nursery-array.wat` fixture keeps a live scalar struct across
each fixed, default, or uniform array. Its two-iteration test proves that one Go
constructor serves all four allocations: the remaining three consume the same
chunk-free batch. The test fails against the parent code with cursor zero, and
passes after the change. Allocation counts and a verified collection also pass.
The earlier nested-reference fixture remains separate because the public base
does not admit typed-reference struct fields to its runtime refill path.

Three 200 ms benchmark samples per case, with collection enabled and
`GOMAXPROCS=1`, measured 8192 iterations per operation on a Ryzen 7 8845HS:

| Mixed array instruction | Before | After | Speedup |
| --- | ---: | ---: | ---: |
| `array.new_fixed` | 2.850 ms | 0.185 ms | 15.41x |
| `array.new_default` | 2.710 ms | 0.178 ms | 15.19x |
| `array.new` | 2.862 ms | 0.183 ms | 15.67x |

Raw samples and the median calculations are in
[`native-array-path-results`](native-array-path-results/shared-summary.json).
These are loop timings, not a claim of V8 parity. The external Dewdrop compiler
currently takes about 94 seconds in the preceding local GC stack. V8 takes
3.83–4.03 seconds with the identical compiler Wasm and request bytes, and produces
the same output hashes. The new full-workload result will be recorded separately.

Build the test binary with the normal Go worker count, then run the benchmark
with `GOMAXPROCS=1`. Applying that variable to `go test` also serializes Go's own
compiler and needlessly increases build time.
