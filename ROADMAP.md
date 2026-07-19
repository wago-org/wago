# wago roadmap

wago is a pure-Go (no cgo) single-pass WebAssembly engine — a from-scratch port
of [WARP](warp/)'s design. The primary target is **linux/amd64**, with native
**linux/arm64**, **darwin/arm64**, and **linux/riscv64** backends. The
amd64 baseline is SSSE3/SSE4.1 plus AVX/VEX.128 XMM encodings; AVX2/FMA/VNNI
remain outside the baseline and require explicit feature gates. This file tracks
what works and what's next at a glance.

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
- [x] Workers plugin: the separate `github.com/wago-org/workers` extension
  owns a transactional worker service with bounded copied tagged delivery,
  cooperative kill, neutral exit events, and creator-authorized lifetime links;
  actor/PID/mailbox/supervisor policy remains plugin-owned
- [x] Benchmarks vs wazero (compile ~34× faster; wago wins fib_rec, sieve, memory_tree,
  linked_list, dispatch, branches, json deserialize; loses on json serialize, blake)

**Arm64 acceptance (in progress)**
- [x] Parent/child corpus runner with hard per-case deadlines and explicit/guard/wazero outcomes
- [x] Darwin/arm64 guard-page execution via synchronous SIGSEGV/SIGBUS context rewriting (Mach-port receiver avoided)
- [x] Verify json-as serialize/deserialize in explicit and guard modes and SQLite's
  recursive-CTE aggregate workload against committed goldens on Darwin/arm64
- [x] Reference globals, heterogeneous indexed table operations, and nonzero-table
  `call_indirect`, with native Linux/arm64 and Darwin/arm64 CI gates

**Linux/RV64 acceptance**
- [x] Native RV64G encoder, no-cgo foreign-stack runtime, W^X publication, and
  process-wide Linux instruction-cache synchronization
- [x] Production scalar railshot lowering for integer/FP, control, calls,
  explicit-bounds memory, bulk memory, tables, globals, references, and host re-entry
- [x] Exact public scalar corpus parity with amd64 and a zero-failure curated
  WebAssembly 1.0 suite under QEMU
- [x] Linux/RISC-V guard-page signal handling, lazy page commitment after
  `memory.grow`, reservation reuse, and public guard-mode execution under QEMU
- [x] Baseline RV64G SWAR lowering for all 256 decoded core/relaxed SIMD
  instructions, public v128/host/cross-instance ABI support, and zero-failure
  official SIMD plus relaxed-SIMD execution under QEMU
- [x] Reproducible `scripts/riscv64-qualify.sh` QEMU/native gate for minimal
  RV64G execution, RVV positive/negative policy, explicit/guard modes, proposal
  suites, exhaustive v128 misalignment, cross-thread publication stress, host
  metadata, and benchmark artifacts
- [ ] Optional RVV optimization after native measurements justify the tier;
  native-hardware guard-page, cross-hart publication, correctness, code-size,
  memory, and performance qualification

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
- 🚧 **P5 — calls**: ARM64 mixed GP/FP parallel staging, two-integer-result
  `X0/X1` returns, and proven monomorphic indirect calls are landed. Broader
  multi-result register shapes and mutable-table epoch caches remain.
- [ ] **P6 — memory & bounds** (explicit mode): straight-line bounds facts, hybrid loop
  precheck, store combining, load-after-store forwarding, CPUID probe → BMI2 shifts
- [ ] **P4 — restricted pending `local.set`/`tee`** *(gated on P1 counters)*
- [ ] **P7 — compile path** *(premise re-measured post-#96)*: fused validate+compile

**Runtime & product** (no-ir-plan P8 — parallel track, feature value)
- [x] **Synchronous host-import results** — returning host imports use the no-cgo
  re-entry protocol; `v128` host params/results use the same two-slot public ABI.
- [x] Interruption / cooperative cancel: amd64, ARM64, and Linux/RV64
  `Call(ctx)`/`InvokeContext` poll at function entries and loop headers and return
  `context.Canceled`/`DeadlineExceeded`, including synchronous host-call loops.
  The checkpoints also bound Go-GC stalls during long native loops.
- [ ] Wasm-level stack traces on trap (trap site → func idx → wasm pc)
- [x] WebAssembly 2.0 product closeout: `.wago` codec v20 persists structural
  reference globals, indexed typed tables/exports/elements, exact local/imported
  table-limit forms, and required-feature bits without serializing live runtime
  identity. Snapshot products reject every table/reference-global module.
  Deterministic module inspection reports all
  reference signatures/globals and every table/import/export/index/type/limit,
  including duplicate aliases and loaded modules. Consolidated trap and cross-link
  teardown tests cover globals, multiple table aliases, passive elements, store
  bindings, and producer/consumer close order. The official Release 2 execution
  harness remains zero-skip at 1,600 modules / 48,248 assertions.
- [ ] `call_indirect` inline caches behind a table epoch
- [ ] `.wago` productization: cache keys (module hash + compiler version + CPU features
  + bounds mode + ABI) and a compile/run/inspect CLI

## Verification & quality

- [ ] Differential oracle: fuzz modules, compare results/traps against C++ WARP (the
  off-path `src/core/compiler/ir` package is reserved as this oracle)
- [ ] Byte-for-byte codegen diffing against WARP for shared inputs
- [ ] Golden disassembly regression net (grows one golden per optimization from P1 on)

## Bigger bets

- [x] SIMD (`v128`) — complete for the documented linux/amd64 SSSE3/SSE4.1 + AVX/VEX.128 baseline and the linux/riscv64 RV64G SWAR baseline: all 256 decoded core/relaxed instructions through 0xfd 275 are frontend-admitted, validator-admitted, and lowered by railshot; reserved proposal-table holes are invalid-decode tests. Public `[16]byte` (`wago.V128`) plumbing covers locals, params/results, control flow, globals, cross-instance imports, and host imports/results. RV64's official current SIMD corpus passes 473 modules / 24,335 assertions, with the project-wide multi-memory case reported separately; relaxed SIMD passes 8 modules / 69 assertions. Keep AVX2/FMA/VNNI and RVV optimizations behind future CPU gates. Current amd64 metrics: [`docs/simd-performance-2026-07.md`](docs/simd-performance-2026-07.md).
- [ ] Threads & atomics
- [ ] Tail calls (`return_call` / `return_call_indirect`)
- [x] Reference-types product completion: signatures, locals, control,
  local/imported/shared globals, host ABI, explicit host funcref ownership/egress,
  typed 8-byte externref tables/elements, every `table.*` operation, multiple
  local/imported tables, exact exports/re-exports, codec-v20 structural metadata,
  snapshot isolation, complete inspection, cross-link teardown, and the
  zero-skip Release 2 execution corpus are done.
- 🚧 Additional targets: Linux/arm64, Darwin/arm64, and Linux/riscv64 are native
  backends; RV64 RVV optimization, native-hardware qualification, and Windows
  ABIs remain planned. Experimental RP2350 work now has cross-host fixed-width
  RV32IM and Thumb-2 encoders; complete mixed one/two/four-slot module lowering;
  scalar, SIMD, memory, table/reference, import, direct/indirect-call, start, and
  export execution; fixed-capacity embedded resources; and a bounded
  closed-module firmware-image builder plus an allocation-free framed transport
  endpoint, strict import/export contract resolution, and context-aware
  function/global/memory/table firmware bundles with transactional linked active
  data/elements and bundle-wide context-aware funcref identities. A freestanding
  C endpoint now supplies direct generated-entry invocation and the Pico SDK
  stdio loop. Both generated targets pass all 147 pinned WebAssembly 2.0 script
  files under QEMU (48,238 executed actions/assertions, with one documented
  bounded-frame resource module). Concrete board helper/image integration,
  physical Pico 2 qualification, and measured footprint/performance remain
  before public admission (see
  `docs/pico2-32bit-port.md`).
- [ ] wazero-compatible API shim for drop-in migration

## Non-goals (for now)

- An interpreter tier (wago is single-pass JIT only)
- **An SSA / IR execution tier** — decided against 2026-07-03; railshot is the one and
  only backend, and the ceiling is attacked incrementally instead
  (see [docs/no-ir-plan.md](docs/no-ir-plan.md) §0)
- The wasm exception-handling / GC proposals; multi-memory
- Re-implementing WARP's linker/disassembler/fuzzer (they live in `warp/` as the
  reference)
