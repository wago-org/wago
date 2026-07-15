# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. Target today is
**linux/amd64** with a documented modern CPU baseline: SSSE3/SSE4.1 plus
AVX/VEX.128 XMM encodings, but not AVX2/FMA/VNNI unless explicitly feature-gated later. For
the actionable plan behind the planned rows, see [ROADMAP.md](ROADMAP.md).

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

`CoreFeaturesV2` is the static WebAssembly 2.0 release group. `CoreFeaturesV3`
describes the mandatory WebAssembly Core 3.0 scope, while `SupportedFeatures()`
reports what this build can execute. The current default is WebAssembly 2.0 plus
completed basic extended constant expressions; it omits SIMD when the CPU lacks
the documented baseline. Other 3.0 families remain explicit, disabled gates.
See [docs/wasm3.md](docs/wasm3.md) for the implementation ledger. The pinned
Release 3 core inventory currently processes all 258 files with zero parser
failures: 144 are green and 114 retain execution/feature gaps.

## WebAssembly 1.0 (MVP)

The core spec — **complete**. The pinned pre-reference-types spec testsuite passes
in full (57/57 applicable files, 0 failing assertions; see [SPECTEST.md](SPECTEST.md)).

| Feature | Planned | Status |
|---|:---:|---|
| i32 / i64 integer ops (arith, bitwise, shift/rotate, clz/ctz/popcnt, compare, eqz) | ✓ | ✅ done |
| f32 / f64 ops (add/sub/mul/div/sqrt/abs/neg/min/max, compare) | ✓ | ✅ done |
| f32 / f64 `ceil` / `floor` / `trunc` / `nearest` / `copysign` | ✓ | ✅ done |
| Conversions + reinterpret (wrap/extend/convert/trunc, i↔f bit casts) | ✓ | ✅ done |
| Float→int `trunc` NaN/overflow **traps** | ✓ | ✅ done |
| Control flow: block / loop / if / else / br / br_if / br_table / return | ✓ | ✅ done |
| `call` / `call_indirect` (table + signature check) | ✓ | ✅ done |
| `select`, `drop`, `nop`, `unreachable` | ✓ | ✅ done |
| Locals (`local.get` / `local.set` / `local.tee`) | ✓ | ✅ done |
| **Globals (`global.get` / `global.set`, mutable)** | ✓ | ✅ done: numeric, `v128`, `funcref`, and `externref` globals support local definitions, imports/exports, shared mutable identity, imported immutable `global.get` initializers, exact type/mutability checks, and store-safe typed host access. Reference globals use 8-byte cells and share only through an explicit compatible store owner. |
| Linear memory load/store (all widths, signed/unsigned) | ✓ | ✅ done |
| **`memory.size` / `memory.grow`** | ✓ | ✅ done (grow up to the declared max via an up-front reservation; no remap) |
| Active data segments | ✓ | ✅ done |
| Tables + active element segments | ✓ | ✅ done |
| Function imports / exports | ✓ | ✅ done (host imports: numeric scalars and `v128` params/results via synchronous parked-host dispatch, including legacy void `HostFunc`; every imported call compiles once and binds through a per-instance dispatch cell with native context switching and active-callee host routing) |
| Memory / table / global imports & exports | ✓ | ✅ done (cross-instance function / global / table / memory linking, incl. shared mutable tables + memories, and host functions used as table funcrefs) |
| `start` function | ✓ | ✅ done (local, or an imported void host function) |

## Extra features (post-1.0)

Later proposals and engine/platform capabilities beyond the MVP.

| Feature | Planned | Status |
|---|:---:|---|
| Sign-extension ops (`i32.extend8_s`, …) | ✓ | ✅ done (decoder/validator plus railshot runtime/codegen coverage for all five scalar opcodes) |
| Non-trapping float→int (`trunc_sat`) | ✓ | ✅ done (decoder/validator plus railshot runtime/codegen coverage for all eight scalar opcodes, including NaN, negative unsigned, and overflow clamp cases) |
| Multi-value (multiple block/func results) | ✓ | ✅ done (decoder/validator, block/if/branch/br_if/br_table/function results, direct and cross-instance calls, public `Invoke`/typed `Call`, and `.wago` metadata are executable; ARM64 additionally returns two integer results in `X0/X1`, while broader optimized register-return shapes remain a performance item) |
| Reference types (`funcref`/`externref`, `select t`, `ref.*`, `table.get/set`, multi-table) | ✓ | ✅ done: nullable and non-null `funcref` plus `externref` execute in signatures, locals, control, globals, host calls, typed elements, and multiple local/imported tables. Indexed get/set/size/grow/fill/copy/init/drop and nonzero-table `call_indirect` preserve exact type/store ownership. `Runtime.NewHostFuncRef`, `Runtime.NewFuncRefGlobal`, `Runtime.NewExternRefGlobal`, and `Runtime.NewExternRefTable` provide explicit bounded owners; raw unowned host descriptor egress stays fail-closed. Snapshot products reject every table/reference-global module. Deterministic `ModuleMetadata` reports every function/global/table index, reference type, import, export, and exact declared limit, including duplicate aliases and codec-v23-loaded modules. Consolidated close-order tests cover shared globals, duplicate funcref table aliases, externref tables, traps, and producer teardown. The official Release 2 execution corpus is green at 1,600 modules and 48,248 assertions with zero gaps. |
| Bulk memory (`memory.copy`/`fill`/`init`, `data.drop`, `table.*`) | ✓ | ✅ done for linear memory plus funcref and externref tables: passive data/elements, active/declarative dropped state, indexed `table.init`/`table.copy`, `elem.drop`, overlap/bounds behavior, and all remaining table operations execute across compatible imported/local tables. |
| Extended constant expressions | ✓ | ✅ done for the basic Release 3 extension: `i32`/`i64` add/sub/mul, imported and prior immutable globals, compile-time folding, instantiate-time deferred evaluation for globals and active offsets, strict AST/byte-backed validation, and `.wago` v23 metadata (the initializer representation originated in v21). GC-added constant instructions remain in the GC row. |
| Tail calls (`return_call` / `return_call_indirect` / `return_call_ref`) | ✓ | 🚧 decoder/validator foundation plus amd64 internal milestones: local register-ABI and local wrapper-ABI `return_call`, mixed GP/XMM `return_call_indirect` through private immutable table 0, and same-instance int-register `return_call_ref` reuse bounded frames for 1,000,000 recursive steps with trap parity. Wrapper direct recursion uses a fixed 16-slot basedata argument bank. Public admission remains disabled; imported/cross-instance direct targets, general mutable/imported/cross-instance tables, wrapper/host/cross-instance reference descriptors, oversized wrapper signatures, and arm64 remain fail-closed. |
| Typed function references / `call_ref` | ✓ | 🚧 `ref.func` carries its declared non-null indexed type, validation matches structurally equivalent recursive graphs, and an internal frontend gate reaches amd64 descriptor `call_ref`. Runtime signature IDs hash reachable indexed/recursive structure. Exact structural subtype/equivalence now governs staged storage/imports, public `Call`/`Invoke`, synchronous host arguments/results, mutable global boundaries, and dynamic typed tables. A shifted indexed type now executes through `table.get/set/grow/fill/copy/init`, imported/re-exported aliases, producer replacement, close order, and trapping writes; local table-owner overwrites release the final closed producer root. amd64 also lowers `ref.as_non_null`, `br_on_null`, and `br_on_non_null`. Public structural metadata and strict codec v23 persistence remain in place. The public feature stays disabled pending typed tails, snapshots, collision-safe native identity policy, remaining GC/reference instructions, and arm64. |
| Multi-memory | ✓ | 🚧 AST and byte-backed validation have an explicit internal `ValidationFeatures.MultiMemory` path that accepts indexed `memory.size`/`memory.grow` and indexed memargs while preserving default/WebAssembly 2.0 rejection. Exact compiled/product memory directories, deterministic inspection, imports/exports, aggregate policy accounting, and codec v23 persistence are staged. On linux/amd64 with explicit bounds, an internal frontend gate executes local/imported memory-1 `memory.size` plus `i32.load/store` through bounded 16-byte native directory entries, with bounds/trap isolation. Indexed grow, other scalar/SIMD widths, bulk/data operations, snapshots, public admission, guard mode, and arm64 remain fail closed. |
| memory64 | ✓ | 🚧 limit/address validation exists; frontend, runtime reservations, APIs, and backend addresses remain 32-bit and reject execution. |
| table64 | ✓ | 🚧 limit/index validation exists; frontend/runtime/backend remain 32-bit and reject execution. |
| Exception handling | ✓ | 🚧 tag/throw/try-table syntax and validation foundation; no runtime unwind ABI or codegen, so tags and exception instructions are rejected. |
| Garbage collection (WasmGC) | ✓ | 🚧 recursive types, descriptor lowering, and collector foundation exist; native roots, safepoints, opcode lowering, allocation calls, and barriers are not wired. |
| SIMD (`v128`) | ✓ | ✅ done for the documented linux/amd64 baseline. All decoded core SIMD and deterministic relaxed SIMD `0xfd` opcodes through 275 are validated, frontend-admitted, and lowered by railshot; 20 reserved proposal-table holes are invalid-decode tests. The documented baseline is SSSE3/SSE4.1 plus AVX/VEX.128 only; AVX2/FMA/VNNI remain future feature-gated fast paths. Public `v128` representation is `[16]byte` (`wago.V128`); locals, params/results, control flow, globals, cross-instance imports, and host imports/results are supported. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions). The pinned Release 3 relaxed-SIMD family also passes all 8 modules and 69 assertions, including official `either` result alternatives. See `docs/simd-relaxed-plan.md` and `docs/simd-performance-2026-07.md`. |
| Branch hinting (`metadata.code.branch_hint`) | ✓ | ✅ done: the current code-metadata wire format is decoded strictly (unique/pre-code section, ordered function/offset vectors, one-byte payload, and only `if`/`br_if` targets). ARM64 railshot uses `if` likelihood to weight local/global pinning and defers non-empty unlikely `br_if` reconciliation into cold target fragments. |
| Threads & atomics | ✓ | ⬜ planned |
| Synchronous host-import results | ✓ | ✅ done |
| Bounded function-pipeline parallelism | ✓ | ✅ done: validation and native codegen share one deterministic serial/adaptive/forced worker policy. The default is serial; `WithFunctionWorkers(0)` and CLI `-p` select adaptive mode, while explicit maxima remain capped by `GOMAXPROCS` and local-function count. |
| Cooperative invocation cancellation | ✓ | 🚧 partial — ARM64 `Instance.Call(ctx, ...)` interrupts native execution at function entries and loop headers; amd64 currently honors cancellation before entry only. |
| Architectures beyond linux/amd64 (arm64, macOS, Windows) | ✓ | 🚧 partial — Linux/arm64 and Darwin/arm64 have native CI for the encoder, backend, runtime/API, explicit and signal-backed guard-page bounds, and corpus correctness. ARM64 reference globals and heterogeneous indexed tables execute; amd64 cancellation polling and Windows remain planned. |
| Interpreter tier (wago is JIT-only) | ✗ | ❌ not planned |
