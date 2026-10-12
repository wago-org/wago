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
  Memoryless `atomic.fence` is admitted. Constant-zero shared waits now lower
  directly to native loads and comparisons on arm64 and amd64.
  This remains partial thread support: ordinary shared-memory loads/stores,
  growth, multiple memories, and broader import/segment combinations remain
  restricted.

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

The SSH control connection persists for 24 hours; detached tmux session
`wago-wasm3-parity` survives disconnection. The dedicated remote workspace is
`~/.cache/wago-wasm3-parity/wago`.
