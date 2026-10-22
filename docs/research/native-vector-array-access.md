# Native vector array access

The Dewdrop compiler profile recorded 90,707,302 `array.get` calls and 15,063,614
`array.set` calls on final arrays of `v128`. Each instruction crossed the parked
Go helper boundary, although the scalar native path already had the required
handle, type, bounds, and object-extent checks.

AMD64 now uses that checked path with 16-byte vector loads and stores. The
existing CPU feature selector emits SSE2 or AVX encodings. Dynamic indexes use
widened offset arithmetic. Constant bounds keep their existing length checks.
The separate access-layout query leaves scalar fill/copy admission unchanged.
Nonfinal arrays and reference-element arrays keep their existing paths.

`TestGCVectorArrayAccessStaysNative` fails before the change with 192 helper-call
bytes and passes after it with zero. Run this assertion with
`-tags=wago_codegenstats`. Runtime tests verify both 64-bit lanes, null and
out-of-bounds traps, relocation across collection, forced collection, the Tiny
collector, and vector arithmetic consumers. Register allocation checks pass.

## Instruction benchmark

`gc-array-vector-access.wat` reads or writes a retained 64-element vector array.
Each operation has 8192 guest loop iterations and checks its result. Five
alternating 200 ms sample pairs ran on CPU 15, Ryzen 7 8845HS, Go 1.27.1.
Compilation and instantiation are excluded. Wago uses `GOMAXPROCS=1`. V8 warms
up before measuring the identical module on the same CPU.

| Operation | Wago before | Wago after | Speedup | V8 | Wago / V8 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Read | 688.549 us | 31.149 us | 22.11x | 5.694 us | 5.47x |
| Write | 807.223 us | 30.141 us | 26.78x | 4.737 us | 6.36x |

These are medians. V8 is Node v24.21.0, V8 13.6.233.17-node.53. Wago retains
its public invocation boundary cost. The measured baseline had the separate
shared-nursery allocation fix; that fix does not change the access code, and
the benchmark allocates its array before timing.

Raw samples, module identity, and calculations are in
[`native-vector-array-results`](native-vector-array-results/vector-summary.json).
The [benchmark runner](../../bench/gc-native/README.md) prints both engines'
medians and exits nonzero when Wago exceeds V8. It currently **fails**. A large
speedup over the old helper path does not establish V8 parity.

## Full compiler check

Both engines ran the exact compiler Wasm SHA-256
`c64fcf044b72cb7356ad598a35670045931337cfa7a6bb9d5916cd304b23fba6`
and request SHA-256
`077b0ff0271706cc3034f3e06c7a228f227c49aeec7786d21373b2072af89c72`.
The generated Wasm and cache hashes matched, validation passed, and the program
returned 42. All result-cache checks were misses.

V8 took 3.83–4.03 seconds. The local Wago stack with the preceding batch fix
took 93.82 seconds. Adding shared small-array reservations took 79.59 seconds.
Adding this vector access fix took 71.24 seconds wall time and 67.68 seconds CPU.
That run overlapped test builds; it is not a controlled final parity result.
It also contains the existing local GC stack and the separate shared-reservation
fix, so the full reduction is not attributed to this PR alone.

A final private helper-count profile confirms zero vector array-get/set helper
calls. It took 60.48 seconds, including instrumentation, with 49.40 seconds
sampled in generated native code. Remaining calls include 20.91 million casts,
8.05 million struct allocations, and 3.94 million array copies. Native samples
need further instruction attribution; they must not all be called GC cost.

The full compiler still misses the V8 target. No timeout is used as a substitute
for that target.
