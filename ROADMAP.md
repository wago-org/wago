# wago roadmap

wago is a pure-Go (no cgo) single-pass WebAssembly engine — a from-scratch port
of [WARP](warp/)'s design. Target today is **linux/amd64** with a modern CPU
baseline of SSSE3/SSE4.1 plus AVX/VEX.128 XMM encodings; AVX2/FMA/VNNI remain
outside the baseline and require explicit feature gates. This file tracks what
works and what's next at a glance.

Three companion docs go deeper:
- [FEATURES.md](FEATURES.md) — the per-feature support matrix (source of truth for
  spec-feature status).
- [OPTIMIZATIONS.md](OPTIMIZATIONS.md) — the optimization roadmap (what codegen work
  is landed / pending, and why).
- [docs/no-ir-plan.md](docs/no-ir-plan.md) — the phased execution plan (P0–P8) that
  the "Next" section below is a summary of.

Status: [x] done · 🚧 in progress · [ ] planned.

## Done

**Full WebAssembly 1.0 (MVP).** The pinned pre-reference-types spec testsuite passes
in full — 57/57 applicable files, 0 failing assertions (see [SPECTEST.md](SPECTEST.md)).

**Frontend (`src/core/compiler/wasm`)**
- [x] Binary decoder for all sections; byte-backed `DecodeModule` (function bodies
  stay raw bytes, not materialized AST)
- [x] Full validator (operand/control stack typing), byte-backed and differential-tested
  against the official spec testsuite

**Compiler backend (`src/core/compiler/backend/railshot`)**
- [x] Single-pass x86-64 codegen with the WARP Valent-Block register allocator
  (symbolic operand stack, deferred-action trees, whole-register-file allocation,
  spill-to-canonical-slot)
- [x] Value types **i32, i64, f32, f64** — arithmetic, bitwise, shifts/rotates,
  clz/ctz/popcnt, comparisons, conversions, reinterpret, `ceil`/`floor`/`trunc`/
  `nearest`/`copysign`, trapping float→int truncation, `trunc_sat`, sign-extension ops
- [x] Control flow: block / loop / if / else / br / br_if / br_table / return
- [x] Linear memory load/store (all widths, signed/unsigned); two bounds modes —
  explicit (memBytes in R15) and guard-page (`-tags wago_guardpage`)
- [x] `memory.size` / `memory.grow` (up-front reservation, grow to declared max)
- [x] Bulk memory `memory.copy` / `memory.fill` (small-n unrolled; forward `rep movsb`) plus passive data `memory.init` / `data.drop`
- [x] Calls: direct, recursion, `call_indirect` (table + signature check) over a
  single-result **register ABI** with a parallel-move resolver; host imports
  (numeric scalar and `v128` params/results via synchronous re-entry, legacy void
  `HostFunc` replay, host functions usable as table funcrefs)
- [x] `select` / `select t`; active element and data segment initialization; `start`
- [x] Hotness-aware local pinning + value-pinned/module-pinned hot globals

**Runtime (`src/core/runtime`)**
- [x] No-cgo execution: W^X `mmap`, foreign-stack trampoline, `g` preservation,
  trap→error, zero-copy linear memory
- [x] Cross-instance linking: function / global / table / memory imports & exports,
  including shared mutable tables + memories, via link-time recompile + context-swap
- [x] Instance slot reuse (lower instantiate cost — explicit #105, guard-page #108)

**Tooling**
- [x] `wago` CLI: `run` / `validate` / `version`, typed args
- [x] Public API: `Run`/`RunValues`, `Compile`/`Compiled`, `Instance`
- [x] Benchmarks vs wazero (compile ~34× faster; wago wins fib_rec, sieve, memory_tree,
  linked_list, dispatch, branches, json deserialize; loses on json serialize, blake)

## Next (near-term, linux/amd64)

The detailed, phase-by-phase plan is **[docs/no-ir-plan.md](docs/no-ir-plan.md)**; the
codegen rationale is **[OPTIMIZATIONS.md](OPTIMIZATIONS.md)**. Summary of the two tracks:

**Engine & performance** (no-ir-plan P1–P7 — each its own PR, measured against P1's stats)
- [ ] **P1 — `CodegenStats` + explain mode** *(do first; everything after proves itself
  against it)*: per-function counters, `WAGO_EXPLAIN`, golden-disassembly harness,
  `WAGO_DEBUG_MODGLOBALS` / `WAGO_PIN_GLOBAL_K` knobs
- [ ] **P2 — cheap railshot wins**: alias-aware pending loads, pure-tree `drop` discard,
  const-fold pack + narrow-load mask elision, same-operand int compare identities
- [ ] **P3 — `stFlags`**: flags-resident compare results (fusion past adjacency); the
  main near-term codegen unlock
- [ ] **P5 — calls**: mixed-call parallel staging, float `call; local.set` fusion,
  limited multi-result register ABI (unblocks multi-value)
- [ ] **P6 — memory & bounds** (explicit mode): straight-line bounds facts, hybrid loop
  precheck, store combining, load-after-store forwarding, CPUID probe → BMI2 shifts
- [ ] **P4 — restricted pending `local.set`/`tee`** *(gated on P1 counters)*
- [ ] **P7 — compile path** *(premise re-measured post-#96)*: fused validate+compile

**Runtime & product** (no-ir-plan P8 — parallel track, feature value)
- [x] **Synchronous host-import results** — returning host imports use the no-cgo
  re-entry protocol; `v128` host params/results use the same two-slot public ABI.
- [x] **WASI preview 1**, minimal: fd_write/read/close/seek/fdstat, proc_exit, args/env, clock, random — the `wasi` plugin (`wasi.Ext(cfg)` / `wasi.Imports(cfg)`) + CLI `--plugin wasi` (built on synchronous host imports)
- [ ] Interruption / cooperative cancel (loop backedges + entries; also serves Go-GC
  safe points)
- [ ] Wasm-level stack traces on trap (trap site → func idx → wasm pc)
- [ ] Remaining post-MVP semantics: persistent typed reference metadata,
  host-created funcref globals, and the standard-harness instantiation gaps.
  Externref signatures, locals, control flow, local/imported/shared globals, public
  handles, reflection-free host params/results, typed 8-byte tables, active/passive/
  declarative null elements, indexed `get/set/size/grow/fill/copy/init`, `elem.drop`,
  runtime-owned sharing, exact local exports/re-exports, and explicit host funcref
  descriptor ownership/egress are executable. Multiple local/imported tables,
  duplicate aliases, active nonzero-table elements, nonzero-table `call_indirect`,
  and the final non-null funcref harness results are done.
- [ ] `call_indirect` inline caches behind a table epoch
- [ ] `.wago` productization: cache keys (module hash + compiler version + CPU features
  + bounds mode + ABI) and a compile/run/inspect CLI

## Verification & quality

- [ ] Differential oracle: fuzz modules, compare results/traps against C++ WARP (the
  off-path `src/core/compiler/ir` package is reserved as this oracle)
- [ ] Byte-for-byte codegen diffing against WARP for shared inputs
- [ ] Golden disassembly regression net (grows one golden per optimization from P1 on)

## Bigger bets

- [x] SIMD (`v128`) — complete for the documented linux/amd64 SSSE3/SSE4.1 + AVX/VEX.128 baseline: every decoded core SIMD opcode and deterministic relaxed SIMD opcode through 0xfd 275 is frontend-admitted, validator-admitted, and lowered by railshot; reserved proposal-table holes are invalid-decode tests. Public `[16]byte` (`wago.V128`) plumbing covers locals, params/results, control flow, globals, cross-instance imports, and host imports/results. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions). Keep AVX2/FMA/VNNI optimizations behind future CPU gates. Current metrics: [`docs/simd-performance-2026-07.md`](docs/simd-performance-2026-07.md).
- [ ] Threads & atomics
- [ ] Tail calls (`return_call` / `return_call_indirect`)
- [ ] Reference-types completion (remaining host-created funcref globals,
  persistent typed codec metadata, and standard-harness instantiation gaps;
  externref signatures, locals, control, local/imported/shared globals, host ABI,
  explicit host funcref ownership/egress, typed 8-byte tables/elements, every
  `table.*` operation, runtime-owned sharing, exact exports/re-exports, multiple
  local/imported funcref tables, and non-null harness results are done)
- [ ] Additional targets: **arm64** (WARP `backend/aarch64` as reference), then
  macOS / Windows ABIs
- [ ] wazero-compatible API shim for drop-in migration

## Non-goals (for now)

- An interpreter tier (wago is single-pass JIT only)
- **An SSA / IR execution tier** — decided against 2026-07-03; railshot is the one and
  only backend, and the ceiling is attacked incrementally instead
  (see [docs/no-ir-plan.md](docs/no-ir-plan.md) §0)
- The wasm exception-handling / GC proposals; multi-memory
- Re-implementing WARP's linker/disassembler/fuzzer (they live in `warp/` as the
  reference)
