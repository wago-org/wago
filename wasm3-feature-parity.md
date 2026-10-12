# WebAssembly feature implementation and performance parity

The full objective remains open: implement WebAssembly Core 3 and proposals in
phases 4 and 5, and reach feature-by-feature execution performance parity with
the SIMD-capable JairusSW/w2c2 fork, while protecting compile time and memory.

Starting Wago revision: `2286d676facdfa1cabe2d1c61072e505438ca7f2`.
Branch: `jairus/wasm3-feature-parity`.

The [official proposal tracker](https://github.com/WebAssembly/proposals), read
2026-10-12, lists phase 4 threads, compact imports, and wide arithmetic; phase 5
JS Promise Integration and Web Content Security Policy. Core 3 already has
executable implementation and conformance gates in this checkout; their recorded
support alone does not establish performance parity.

## Implementation checkpoints

- Compact imports now have an independent public bit and CLI discovery. Both
  grouped encodings run with Core 2 plus compact imports, and survive artifact
  reload. Their generated native code is byte-identical to ordinary imports.
  The previous multiple-memory admission route is retained for compatibility.
- Wide arithmetic now decodes, validates, classifies, encodes, and compiles all
  four proposal instructions. Native add/subtract use carry/borrow chains;
  multiplication uses hardware signed/unsigned widening multiply. Both result
  words use existing multi-value paths. Artifacts preserve the required feature
  bit. Earlier deferred traps materialize in source order.
- Threads now support instance-owned shared memory as well as imported shared
  memory. Separate backing and instance-control mappings preserve the existing
  atomic addressing path, with ownership retained while importers remain live.
  Memoryless `atomic.fence` and shared `memory.size` are admitted. Constant-zero shared waits now lower
  directly to native loads and comparisons on arm64 and amd64.
  Ordinary scalar and SIMD loads/stores now execute against shared backing.
  ARM64 atomics use CPU-gated LSE with an exclusive-loop fallback and persisted
  artifact requirements. Both backends prove aligned constant atomic bounds
  against declared memory minimums.
  Shared `memory.copy` and `memory.fill` now execute with bounds-before-write
  and overlap-safe copying, including small unrolled paths on both backends.
  Shared growth now publishes a single atomic extent after guarded pages are
  accessible; shared accesses and size queries see growth by other instances.
  Active data segments now initialize shared backing before start execution;
  passive `memory.init` and instance-local `data.drop` are admitted.
  This remains partial thread support: shared memory64, multiple memories,
  host function imports, tables, tags, and element segments remain restricted.

## Current validation

Apple M4 Max, Darwin/arm64, Go 1.27.1:

- Decoder/validator/frontend/ARM64 encoder and backend packages passed with the
  allocation checker enabled.
- Wide arithmetic: 500 input vectors per instruction and pressure variant,
  executed from both original and codec-loaded products. Edge vectors include
  zero, all ones, signed extrema, and the 32-bit boundary. Eight values remain
  live beneath the operands in pressure variants. Feature-disabled compilation
  rejects each instruction. An overflow-before-divide-by-zero probe verifies
  trap order.
- Wasmtime Core 3 regression corpus passed: 215 modules and 690 assertions,
  enforced by `TestWasmtimeCore3Corpus` (not the full official Core 3 suite).
- Native Linux/amd64 on hub@hub (Ryzen 7 7800X3D): the compact-import and wide
  arithmetic execution/codec/pressure/trap tests passed with the allocation
  checker enabled.

Five-sample ordinary compile comparison, against the starting revision:

| Fixture | Baseline median | Current median | Allocated bytes | Allocations |
| --- | ---: | ---: | ---: | ---: |
| Small scalar, default config | 6,103 ns | 6,030 ns | 19,202 unchanged | 74 unchanged |
| Small scalar, explicit Core 3 | 6,007 ns | 6,069 ns | 19,058 unchanged | 73 unchanged |

These microbenchmarks establish no allocation increase and roughly unchanged
latency for these fixtures only. Broader compile/RSS measurements remain needed.
Raw outputs are in ignored `.validation/wasm3-parity/compile-{before,after}.txt`.

## Performance work still required

wasm.fyi's source-built performance script produced 90 long execution contracts,
3 lifecycle probes, and 9 compile probes. Its local configured wasm-bench checkout
lacks the AOT adapter; an isolated copy of the existing hub native adapter was
retrieved to restore measurement capability without modifying that checkout.
The local SIMD fork is `f303ed2`; its translator and an AOT adapter build
successfully. The initial 90-contract comparison is complete: Wago passed 77; w2c2 passed 51,
with 27 unsupported and 12 failed. All 51 successful pairs match artifacts and
met the 50 ms sample-duration target. See [the full initial comparison](wasm3-feature-baseline.md).
Per-feature gap diagnosis, optimized lowerings, missing host profiles, and
both-platform compile/RSS measurements remain open.

The subsequent thread checkpoint passed all five previously rejected defined
shared-memory contracts, bringing cumulative Wago coverage to 82/90. Its
wait/notify contract improved by 48.7% across three launches after the zero-timeout
lowering. Native execution tests pass on both architectures. See
[thread implementation and measurement evidence](wasm3-threads-progress.md).
The eight remaining host-profile rejections are JSPI and JS strings. This
checkpoint does not establish w2c2 parity or complete thread conformance.

The next checkpoint executes ordinary shared scalar/SIMD memory accesses and
adds two wasm.fyi contracts (cumulative Wago coverage 84/92). Correcting the
isolated AOT adapter's thread-profile admission and linking pthread/futex support
produces seven successful exact-artifact thread comparisons. Wago remains
1.555–7.097 times slower on the memory/atomic kernels; the fence-only kernel has
a larger gap because Clang collapses its guest loops to one fence and closed-form
arithmetic. The linked thread report retains those results and configuration.

The LSE and constant-bounds checkpoint reduces the exact atomic-add and CAS
workloads by 72.7% and 67.4% respectively versus the preceding Wago capture.
Current times remain 1.209 and 1.140 times w2c2; other kernels retain the gaps
above. Sequential compile samples show unchanged allocation counts and code
size for the measured fixtures. The thread report records CPU admission,
codec/differential validation, alternating measurements, and raw evidence.

Shared copy/fill adds two independently verified wasm.fyi contracts, bringing
cumulative Wago coverage to 86/94. Native execution and codec checks pass on both
architectures. On the 33-byte arm64 workloads, fill remains 1.919 times w2c2 and
overlapping copy 1.117 times w2c2 after unrolling; the thread report records the
exact capture identities and remaining limits.

The shared-growth checkpoint passes native/codec/concurrent/guarded-race checks
on both architectures and adds size-query and growth-lifecycle contracts.
Cumulative long execution coverage is 87/95. Zero-growth/size queries are about
10.6 times faster than w2c2 on the new workload, while live extent reads add cost
to several earlier shared-access kernels. Nonzero growth's short single-operation
samples remain unqualified for sustained parity. Compile fixture allocations
and generated code size remain unchanged; the thread report records the gaps.

The stable-backing cache checkpoint retains live extent reads and reduces the
controlled shared scalar/SIMD workloads by about 9%, fill by 18%, and overlapping
copy by 22% relative to the growth checkpoint. Native/codec/ABI/pressure/guarded
race checks pass on both architectures. Controlled ordinary compile differences
are below 0.7%, with unchanged allocation counts and code size on the measured
fixtures. Per-feature parity, broader composition, and full conformance remain
open; the thread report retains both timing captures and the controlled evidence.

The SSH control connection persists for 24 hours; detached tmux session
`wago-wasm3-parity` survives disconnection. The dedicated remote workspace is
`~/.cache/wago-wasm3-parity/wago`.
