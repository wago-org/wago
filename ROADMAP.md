# wago roadmap

wago is a pure-Go (no cgo) single-pass WebAssembly engine — a from-scratch port
of [WARP](warp/)'s design. Target today is **linux/amd64** with a modern CPU
baseline of SSE4.1 plus AVX/VEX.128 XMM encodings; AVX2/FMA/VNNI remain outside
the baseline and require explicit feature gates. This file tracks what works,
what's next, and the bigger bets. Status: [x] done · 🚧 in progress · [ ] planned.

For an at-a-glance support matrix of every WebAssembly feature, see
[FEATURES.md](FEATURES.md). This file is the actionable plan behind it.

## Done

**Frontend (`src/core/compiler/wasm`)**
- [x] Binary decoder for all sections (types, imports, funcs, tables, memory,
  globals, exports, start, elements, code, data, custom)
- [x] Full validator (operand/control stack typing), differential-tested against
  the official WebAssembly spec testsuite

**Compiler backend (`src/core/compiler/backend/railshot`)**
- [x] Single-pass x86-64 codegen with the WARP Valent-Block register allocator
  (symbolic operand stack, lazy constants/locals, spill-to-canonical-slot)
- [x] Value types **i32, i64, f32, f64** — arithmetic, bitwise, shifts/rotates,
  clz/ctz/popcnt, comparisons, conversions, reinterpret
- [x] Control flow: block / loop / if / else / br / br_if / br_table / return
- [x] Linear memory load/store (sized + signed variants), active bounds checks
- [x] Calls: direct, recursion, `call_indirect` (table + signature type check),
  host imports (void/log-style, batched)
- [x] `select` / `select t`
- [x] Active element and **data segment** initialization
- [x] Bulk memory `memory.copy` / `memory.fill`

**Runtime (`src/core/runtime`)**
- [x] No-cgo execution: W^X `mmap`, foreign-stack trampoline, `g` preservation,
  trap→error, zero-copy linear memory

**Tooling**
- [x] `wago` CLI: `run` / `validate` / `version`, typed args. Public validation is `wago validate <file>`.
- [x] Public API: `Run`/`RunValues`, `Compile`/`Compiled`, `Instance`
- [x] Benchmarks vs wazero (compile ~34× faster, cross-boundary call ~3× faster)
- [x] Byte-backed `DecodeModule`: production validation/compile keeps function bodies as raw bytes instead of materialized AST instruction trees.

## Next (near-term, linux/amd64)

**Numeric completeness**
- [ ] Float `trunc` NaN/overflow traps; `trunc_sat` (saturating) conversions
- [ ] Spec-exact `min`/`max` NaN propagation; `copysign`
- [ ] `ceil` / `floor` / `trunc` / `nearest` (SSE4.1 `roundsd`)
- [ ] i64 sub-width loads (`i64.load8/16/32_s/u`)

**Memory & data**
- [ ] `memory.size` / `memory.grow` (remap + update size cache)
- [ ] Remaining bulk memory: `memory.init` / `data.drop`
- [ ] Passive element/data segments + `table.*` ops

**Module linking**
- [x] Mutable globals, numeric global imports/exports, and exported-global API accessors
- [ ] Synchronous host-import results (the foreign-stack→Go re-entry path; today
  host imports are void + batched)

## Engine & performance

- [ ] Register-based internal-call ABI (replace the per-call WasmWrapper buffer —
  the one microbenchmark where wazero currently wins, recursive calls)
- [ ] Locals-in-registers across blocks (reduce spill traffic)
- [ ] Cooperative GC checkpoints at loop back-edges / call boundaries (so long
  native loops don't stall Go's STW GC — see the design notes in README)
- [ ] Multiple instances sharing one engine/foreign-stack (lower instantiate cost)

## Verification & quality

- [ ] Differential oracle: fuzz modules, compare results/traps against C++ WARP
- [ ] Byte-for-byte codegen diffing against WARP for shared inputs
- [ ] Reintroduce a size-aware disassembler/AOT command when the CLI budget can
  absorb it.

## Bigger bets

- [ ] **WASI preview 1** (clocks, args/env, fd read/write) → run real CLI wasm
- [ ] Additional targets: **arm64**, then macOS / Windows ABIs
- [ ] SIMD (`v128`) — initial amd64 plumbing plus `v128.const`/load/store/bitwise, splat/lane extract/replace, integer unary (including i8x16 popcnt)/signed and unsigned i8/i16 narrow, signed and unsigned i8-to-i16, i16-to-i32, and i32-to-i64 widening extends, pairwise extadd from i8-to-i16 and i16-to-i32 lanes, signed/unsigned i8-to-i16, i16-to-i32, and i32-to-i64 extmul, add/sub/comparison (including signed and unsigned ordered comparisons for i8/i16/i32), i8/i16 saturating add/sub, i16 q15mulr_sat_s, i16 lane shifts, i16/i32 multiply, signed/unsigned i8/i16/i32 min/max, i8/i16 unsigned rounding averages, packed f32x4/f64x2 unary/arithmetic/comparison, and core all_true/bitmask reduction tranches landed; continue through the plan in [`docs/simd-relaxed-plan.md`](docs/simd-relaxed-plan.md), using VEX.128/SSE4.1 baseline only until AVX2/FMA/VNNI gates exist
- [ ] wazero-compatible API shim for drop-in migration

## Non-goals (for now)

- An interpreter tier (wago is single-pass JIT only)
- The wasm exception-handling / GC proposals
- Re-implementing WARP's linker/disassembler/fuzzer (they live in `warp/` as the
  reference)
