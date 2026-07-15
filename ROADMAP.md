# wago roadmap

wago is a pure-Go (no cgo) single-pass WebAssembly engine — a from-scratch port
of [WARP](warp/)'s design. Target today is **linux/amd64** with a modern CPU
baseline of SSSE3/SSE4.1 plus AVX/VEX.128 XMM encodings; AVX2/FMA/VNNI remain
outside the baseline and require explicit feature gates. This file tracks what
works and what's next at a glance.

Four companion docs go deeper:
- [FEATURES.md](FEATURES.md) — the per-feature support matrix (source of truth for
  spec-feature status).
- [OPTIMIZATIONS.md](OPTIMIZATIONS.md) — the optimization roadmap (what codegen work
  is landed / pending, and why).
- [docs/no-ir-plan.md](docs/no-ir-plan.md) — the phased execution plan (P0–P8) that
  the "Next" section below is a summary of.
- [docs/wasm3.md](docs/wasm3.md) — the mandatory Core 3.0 implementation ledger,
  official suite pin, measurements, platform gates, and recursive slices.

Status: [x] done · 🚧 in progress · [ ] planned.

## Done

**Full WebAssembly 1.0 (MVP).** The pinned pre-reference-types spec testsuite passes
in full — 57/57 applicable files, 0 failing assertions (see [SPECTEST.md](SPECTEST.md)).

**Frontend (`src/core/compiler/wasm`)**
- [x] Binary decoder for all sections; byte-backed `DecodeModule` (function bodies
  stay raw bytes, not materialized AST)
- [x] Full validator (operand/control stack typing), byte-backed and differential-tested
  against the official spec testsuite; independent function bodies support bounded,
  deterministic parallel validation through the function-worker policy

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
- [x] Bounded parallel function codegen with worker-local scratch/arenas and
  deterministic ordered assembly, sharing one policy with function validation

**Runtime (`src/core/runtime`)**
- [x] No-cgo execution: W^X `mmap`, foreign-stack trampoline, `g` preservation,
  trap→error, zero-copy linear memory
- [x] Cross-instance linking: function / global / table / memory imports & exports,
  including shared mutable tables + memories. Imported calls compile once and bind
  through per-instance dispatch cells with explicit direct/indirect context switching.
- [x] Instance slot reuse (lower instantiate cost — explicit #105, guard-page #108)

**Tooling**
- [x] `wago` CLI: `run` / `validate` / `version`, typed args
- [x] Public API: `Run`/`RunValues`, `Compile`/`Compiled`, `Instance`, plus
  opt-in serial/adaptive/forced function-worker policy for validation and codegen
- [x] Workers plugin: the separate `github.com/wago-org/workers` extension
  owns a transactional worker service with bounded copied tagged delivery,
  cooperative kill, neutral exit events, and creator-authorized lifetime links;
  actor/PID/mailbox/supervisor policy remains plugin-owned
- [x] `wago run` and `wago validate` expose adaptive/forced function workers via
  `-p`, with serial defaults for predictable memory use
- [x] Benchmarks vs wazero (compile ~34× faster; wago wins fib_rec, sieve, memory_tree,
  linked_list, dispatch, branches, json deserialize; loses on json serialize, blake)

**Arm64 acceptance (in progress)**
- [x] Parent/child corpus runner with hard per-case deadlines and explicit/guard/wazero outcomes
- [x] Darwin/arm64 guard-page execution via synchronous SIGSEGV/SIGBUS context rewriting (Mach-port receiver avoided)
- [x] Verify json-as serialize/deserialize in explicit and guard modes and SQLite's
  recursive-CTE aggregate workload against committed goldens on Darwin/arm64
- [x] Reference globals, heterogeneous indexed table operations, and nonzero-table
  `call_indirect`, with native Linux/arm64 and Darwin/arm64 CI gates

## Next (near-term, linux/amd64)

The optimization plan remains **[docs/no-ir-plan.md](docs/no-ir-plan.md)** and the
Core 3.0 plan is **[docs/wasm3.md](docs/wasm3.md)**. Current tracks:

**WebAssembly 3.0** (bounded recursive slices; linux/amd64 is the primary claim)
- [x] Pin official `WebAssembly/spec` `wg-3.0` at `9d360199...`; route `make spec3`
  to its 258-file `test/core` corpus and make parser failures/skips fail closed.
- [x] Add `CoreFeaturesV3`, separate admission bits for mandatory families, and
  explicit `GOOS/GOARCH` unsupported-feature errors.
- [x] Execute the basic extended-constant-expression proposal and persist deferred
  scalar initializers/offsets in `.wago` codec v26 (initializer records introduced in codec v21).
- [x] Bootstrap checksum-pinned WABT 1.0.41, then pin the official
  WebAssembly/spec 3.0.0 interpreter at the suite revision for the 28 files WABT
  cannot parse. The schema-2 258-file inventory now has zero parser failures,
  144 green/114 red files, 535 skipped modules, 5 failed and 6,268 skipped
  assertions; tool/parser failures remain hard.
- [x] Honor official Release 3 relaxed-SIMD `either` result patterns: all 8
  converted modules and 69 assertions pass with zero failures/skips.
- 🚧 Tail calls and typed references: amd64 local direct, private-immutable-table
  mixed GP/XMM indirect, descriptor `call_ref`, and same-instance int-register
  `return_call_ref` milestones execute internally with bounded frames and trap
  checks. `ref.func` now preserves its indexed type, recursive structural type
  equivalence validates, and a staged frontend gate routes indexed signatures to
  `call_ref`. Public structural type descriptors, exact signature/global/table/
  element inspection, and codec-v26 persistence are now present. Staged runtime
  storage/import matching uses cross-module structural subtype/equivalence;
  native descriptors use bounded 64-bit SHA-256-derived structural keys that
  separate deliberate legacy 32-bit collisions and fail closed above a fixed
  canonicalization budget; public invocation,
  synchronous host boundaries, and mutable global ingress/egress enforce exact
  types/nullability. Dynamic typed `table.get/set/grow/fill/copy/init` now prove
  shifted-type imports/re-exports, producer replacement, close order, and trap
  atomicity; local table-owner overwrites release closed consumers. amd64 executes
  `ref.as_non_null` and both null branches; local wrapper direct tails use a fixed
  16-slot bank; and the reached `select` funcref assertion is green. Cross-instance
  `InstanceExport` imports now retain each distinct producer through consumer close,
  so shifted typed `call_ref` descriptors remain valid after producer logical close.
  Typed/tail opcodes persist required-feature bits and snapshots reject unresolved
  descriptor/tail contexts before mutation. A compile-only typed-tail gate now lets
  retained int-register `InstanceExport` descriptors transfer through a foreign
  wrapper on amd64 from root or nested internal callers. Nested callers reuse one
  fixed 32-byte return record, restore both integer results through a trampoline,
  and repeat 10,000 cross-instance transfers without retaining the discarded callee
  frame. Exact typed globals may tail-enter tagged same-instance scalar wrappers;
  hosts remain untagged and fail closed. A private direct-tail gate plus the existing
  host bridge now makes all 47 pinned `return_call` commands green: 3 modules, 33
  assertions, and 11 invalid modules. Per-table finite immutable-local proofs plus
  staged scalar descriptors and fixed-bank wrapper tails now make all 79
  `return_call_indirect` commands green: 3 modules, 49 assertions, 16 invalid, and
  11 malformed. The pinned `return_call_ref` runner is now gap-free at 51 commands,
  5 modules, 35 assertions, and 11 invalid modules; one canonical funcref result uses
  the staged RAX return path with exact descriptor ownership. Retained cross-instance
  direct `return_call` uses a separate fixed four-word root/nested transition, preserves
  producer lifetime and trap recovery, repeats without allocation, and now admits exactly
  `(i32, f64) -> f64` and `(f64) -> i32` in addition to the integer shapes. Public admission, other float/
  oversized direct tails, general-table/
  foreign-float/general reference-result tails, live typed snapshot state, remaining
  GC/reference instructions, and arm64 parity remain gated.
- 🚧 Multi-memory now has an explicit internal AST/byte-backed gate, exact
  compiled/product declaration/import/export directories, declaration-based
  policy accounting, duplicate imported-memory alias deduplication, and codec v26
  persistence. A linux/amd64 explicit-bounds staged path executes local/imported/
  re-exported indexed `memory.size/grow`, every scalar and SIMD memory form, active
  and passive data lifecycle, and `memory.init/copy/fill` with exact cross-memory,
  overlap, bounds, and drop behavior. A finite compile-only co-tenant proof now
  serializes owner/consumer basedata, refreshes bounded native directories, and
  admits executable owners, finite imported numeric-global pointer arrays, and one
  bounded imported funcref table under a null/get/set/size-only scan. Retained scalar
  direct imports may now re-enter producers sharing the exact memory-0 mapping through
  stable 256-byte arena images; nested calls, traps, shared growth, concurrency, and
  close ordering are proven allocation-free. Host callbacks, foreign-memory/imported-
  tail bindings, local/multiple/unbounded or wider-operation tables, local/reference/
  vector globals, passive/reference tenant state, and live-binding codec persistence
  remain rejected.
  Core 3 compact imports remain strict. The complete 42-file matrix is gap-free at
  913 commands, 79 modules, 771 assertions, 4 invalid, 22 unlinkable, and 20
  uninstantiable cases, with zero feature rejects or blocked commands.
  Snapshot v3 captures and restores every owned
  local memory image/grown size plus passive-data drop state, rebuilding native
  directory entries on restore. Executable-owner/function/private-basedata contexts,
  imported/shared/registered-tenant snapshots, guard mode, public admission, and
  arm64 remain.
- 🚧 memory64 has one bounded linux/amd64 local execution slice: exact 64-bit
  metadata/codec limits, declared maxima <=65,535 pages or an exact no-maximum type
  backed by a finite 65,535-page implementation reservation, checked u64 address/offset
  arithmetic, `memory.size/grow`, all 19 integer scalar operations, all four float
  scalar operations, every SIMD memory load/store/extend/splat/zero/lane form, active
  and passive data lifecycle, and `memory.copy`/`memory.fill`. Validated i64
  offset programs round-trip through codec v26; scalar/SIMD/data/bulk paths check full-
  width carry and exact end bounds, passive source/length remain zero-extended i32,
  copy preserves overlap, dropped-state semantics are exact, and trapping writes are
  atomic. No-maximum policy and managed-instance accounting charge the finite reserve,
  while growth beyond it returns `-1`. Snapshots remain an explicit gate.
  The six-file official matrix is now gap-free at 807 commands, 43 admitted modules,
  622 assertions, 83 invalid, and 59 malformed cases, with zero gates or blocked
  dependents. Imports, shared/multi-memory, excessive declared limits, the remaining
  unaccounted memory64 files, guard mode, public admission, snapshots, and arm64 remain.
  Table64 now has one local explicit-max `size/get/set/grow/fill` slice plus exact
  nine-file accounting at 2,802 commands / 68 modules / 2,330 assertions / 39 gates /
  270 blocked / 81 invalid; elements/initializers, copy/init/indirect, exception handling,
  and GC remain end-to-end work.
- [ ] Reach zero unexplained failures/skips in the official Release 3 core suite.

**Engine & performance** (no-ir-plan P1–P7, measured against P1's stats)
<!-- roadmap:P1 status=done -->
- [x] **P1 — `CodegenStats` + explain mode**: per-function counters,
  `WAGO_EXPLAIN`, golden-disassembly harness, `WAGO_DEBUG_MODGLOBALS`, and
  `WAGO_PIN_GLOBAL_K` are implemented on amd64 and arm64.
<!-- roadmap:P2 status=partial -->
- 🚧 **P2 — cheap railshot wins**: the const-fold pack and same-operand integer
  identities are landed; alias-aware pending loads, pure-tree `drop`, and
  narrow-load mask elision remain measurement-gated.
<!-- roadmap:P3 status=partial -->
- 🚧 **P3 — `stFlags` and compare fusion**: eqz-of-compare inversion and ordered
  float compare-to-branch fusion are landed; broader flags-resident consumers
  remain measurement-gated.
<!-- roadmap:P5 status=partial -->
- 🚧 **P5 — calls**: ARM64 mixed GP/FP parallel staging, two-integer-result
  `X0/X1` returns, and proven monomorphic indirect calls are landed. Broader
  multi-result register shapes and mutable-table epoch caches remain.
<!-- roadmap:P6 status=partial -->
- 🚧 **P6 — memory & bounds** (explicit mode): straight-line bounds facts are
  implemented; hybrid loop prechecks, store combining, load-after-store
  forwarding, and a CPUID-gated BMI2 path remain.
<!-- roadmap:P4 status=planned -->
- [ ] **P4 — restricted pending `local.set`/`tee`** *(gated on P1 counters)*
<!-- roadmap:P7 status=planned -->
- [ ] **P7 — compile path** *(premise re-measured post-#96)*: fused validate+compile

**Runtime & product** (no-ir-plan P8 — parallel track, feature value)
- [x] **Synchronous host-import results** — returning host imports use the no-cgo
  re-entry protocol; `v128` host params/results use the same two-slot public ABI.
- 🚧 Interruption / cooperative cancel: ARM64 `Call(ctx)` polls at function
  entries and loop headers and returns `context.Canceled`/`DeadlineExceeded`;
  amd64 native polling remains planned. The checkpoints also bound ARM64 Go-GC
  stalls during long native loops.
- [ ] Wasm-level stack traces on trap (trap site → func idx → wasm pc)
- [x] WebAssembly 2.0 product closeout: `.wago` codec v26 persists structural
  reference globals, indexed typed tables/exports/elements, exact local/imported
  table/memory-limit forms, indexed memory imports/exports, and required-feature
  bits without serializing live runtime
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

- [x] SIMD (`v128`) — complete for the documented linux/amd64 SSSE3/SSE4.1 + AVX/VEX.128 baseline: every decoded core SIMD opcode and deterministic relaxed SIMD opcode through 0xfd 275 is frontend-admitted, validator-admitted, and lowered by railshot; reserved proposal-table holes are invalid-decode tests. Public `[16]byte` (`wago.V128`) plumbing covers locals, params/results, control flow, globals, cross-instance imports, and host imports/results. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions). Keep AVX2/FMA/VNNI optimizations behind future CPU gates. Current metrics: [`docs/simd-performance-2026-07.md`](docs/simd-performance-2026-07.md).
- [ ] Threads & atomics
- 🚧 Tail calls (`return_call` / `return_call_indirect` / `return_call_ref`):
  decoder/validator foundation plus amd64 local register- and wrapper-ABI direct,
  tail-position host imports, private-immutable-table mixed indirect, same-instance
  local typed-reference, and retained cross-instance root/nested typed-reference
  frame-reuse milestones exist. The pinned `return_call` file is fully green at 47
  commands / 3 modules / 33 assertions / 11 invalid. The 79-command indirect file
  is fully green at 3 modules / 49 assertions / 16 invalid / 11 malformed. `return_call_ref`
  is gap-free at 51 commands / 5 modules / 35 assertions / 11 invalid, including one
  canonical funcref result. Retained integer plus exact `(i32, f64) -> f64` and
  `(f64) -> i32` cross-instance direct tails use a separate fixed root/nested return transition. Public
  admission, other float/oversized direct tails, general-table/foreign-float/general reference-result
  tails, snapshots, and arm64 execution remain.
- [x] Basic extended constant expressions: integer add/sub/mul, prior immutable
  globals, active offsets, strict validation, and codec-v26 persistence.
- 🚧 Typed function references: typed `ref.func`, recursive structural equivalence,
  and staged indexed-signature frontend admission now reach amd64 descriptor
  `call_ref` with null/signature checks and wrapper/context-aware non-tail calls.
  Public structural descriptors and codec-v26 exact metadata cover signatures,
  globals, tables, elements, imports/exports, and inspection without enabling the
  feature. Staged exact storage/import compatibility, indexed/recursive runtime
  signature identity, bounded collision-resistant native type keys,
  `ref.as_non_null`, both null branches, exact public/host funcref boundaries,
  and non-null harness result matching are now proven.
  Mutable global host/public boundaries now enforce exact indexed types and
  nullability, and shared table/global producer roots release on successful final
  overwrite without violating trap atomicity. Cross-instance typed descriptors now
  retain their producer through consumer close, and typed/tail opcode requirements
  survive codec metadata while snapshots reject unresolved contexts before mutation.
  Root and nested cross-instance typed tails now execute with explicit host and
  unsupported-shape failures; persisted live reference state, broader tails, public
  admission, remaining reference/GC instructions, and arm64 remain gated. Multi-
  memory now executes all indexed scalar, SIMD, and bulk/data operations internally
  on linux/amd64 explicit bounds, decodes compact import groups, accounts for all
  913 commands in the complete 42-file family matrix, snapshots owned local memory
  sets through snapshot v3, and stages bounded registered-memory co-tenants. The
  serializer now admits imported numeric globals, one bounded imported funcref table,
  and retained scalar direct calls to exact same-memory producers through recursive
  stable-image transitions; host/foreign/tail imports and broader reference state remain
  explicit gates. Memory64 now has one bounded local size/grow/scalar/SIMD-memory/
  active+passive-data/copy/fill slice with exact metadata/codec limits, checked u64
  arithmetic, overlap, drop state, trap atomicity, and finite no-maximum reservations.
  A local explicit-max table64 slice executes `size/get/set/grow/fill` with i64
  indexes/deltas/start/length and codec-v26 address-form
  metadata; its nine-file official accounting is pinned. Imported/shared snapshots,
  wider table64, guard mode, public admission, exception handling,
  and WasmGC remain active scope;
  see `docs/wasm3.md` for exact boundaries.
- [x] Reference-types product completion: signatures, locals, control,
  local/imported/shared globals, host ABI, explicit host funcref ownership/egress,
  typed 8-byte externref tables/elements, every `table.*` operation, multiple
  local/imported tables, exact exports/re-exports, codec-v26 structural metadata,
  snapshot isolation, complete inspection, cross-link teardown, and the
  zero-skip Release 2 execution corpus are done.
- 🚧 Additional targets: native **linux/arm64** and **darwin/arm64** backends and
  runtime paths are implemented and under qualification; Windows ABI support
  remains planned.
- [ ] wazero-compatible API shim for drop-in migration

## Non-goals (for now)

- An interpreter tier (wago is single-pass JIT only)
- **An SSA / IR execution tier** — decided against 2026-07-03; railshot is the one and
  only backend, and the ceiling is attacked incrementally instead
  (see [docs/no-ir-plan.md](docs/no-ir-plan.md) §0)
- Re-implementing WARP's linker/disassembler/fuzzer (they live in `warp/` as the
  reference)
