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

## Dynamic uniform array lengths (2026-10-21)

`array.new` with a run-time length previously always entered Go, even when its
object fit the existing 256-byte native limit. A dynamic uniform site now checks
the computed size and uses the same checked small-object reservation. Larger
uniform arrays retain the rooted helper. Dynamic default arrays keep their
separate large-object reservation; uniform arrays do not use that protocol.
The count argument follows the initializer's one or two value slots.

The reservation test fails on the parent with cursor 0 rather than 3 for two
mixed iterations. The new boundary test covers length zero, the 256-byte edge,
the first helper-sized object, large arrays, and three initializer bit patterns
under normal, collect-every-allocation, and Tiny collectors, with verification
after collection. Existing vector initializer tests cover two-slot values.

Five alternating samples of exactly 100 warmed invocations on CPU 15,
`GOMAXPROCS=1`, collection enabled, each invocation doing 8192 mixed iterations:
13.052 ms before (12.871–13.300), 0.1911 ms after (0.1907–0.1916), or 68.30x.
Both binaries use the new fixture and test harness. The baseline overlay replaces
only production `gc_array.go` and `gc_direct.go` with the parent versions. Raw
samples and medians are in `native-array-path-results/dynamic-warm-*`. These
timings include collector work and are not a V8 parity claim.

A separate local combined stack, including the canonical-identity shortcut,
reduced one cold-cache Dewdrop compile from 63.386 to 59.467 seconds after this
change. The following cast/field-read change reduced it further to 50.551 seconds.
The Wasm and cache output hashes matched throughout, and the output's `main`
returned 42. These sequential single runs are directional evidence, not an
isolated statistical estimate. V8 on the same compiler bytes previously took
3.487 seconds; the full workload still has a large gap.

V8 13.6 (Node 24.21.0), same CPU and dynamic uniform fixture, five groups of
1000 warmed invocations, measured 0.02624 ms at the median. The native Wago loop
remains 7.28x slower. This comparison includes each engine's collector and
optimization policy; it does not establish single-allocation latency.
