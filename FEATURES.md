# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. Target today is
**linux/amd64** with a documented modern CPU baseline: SSSE3/SSE4.1 plus
AVX/VEX.128 XMM encodings, but not AVX2/FMA/VNNI unless explicitly feature-gated later. For
the actionable plan behind the planned rows, see [ROADMAP.md](ROADMAP.md).

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

`CoreFeaturesV2` is the static WebAssembly 2.0 release feature group and includes
core SIMD. `SupportedFeatures()` is the build- and host-admitted form of that
group; it omits SIMD when the CPU lacks the documented baseline. Post-release
proposals such as tail calls are separate and remain unsupported.

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
| Function imports / exports | ✓ | ✅ done (host imports: numeric scalars and `v128` params/results via `SyncHostFunc`/reflected functions, legacy void `HostFunc` batched replay; cross-instance function calls via a link-time recompile + native context-swap) |
| Memory / table / global imports & exports | ✓ | ✅ done (cross-instance function / global / table / memory linking, incl. shared mutable tables + memories, and host functions used as table funcrefs) |
| `start` function | ✓ | ✅ done (local, or an imported void host function) |

## Extra features (post-1.0)

Later proposals and engine/platform capabilities beyond the MVP.

| Feature | Planned | Status |
|---|:---:|---|
| Sign-extension ops (`i32.extend8_s`, …) | ✓ | ✅ done (decoder/validator plus railshot runtime/codegen coverage for all five scalar opcodes) |
| Non-trapping float→int (`trunc_sat`) | ✓ | ✅ done (decoder/validator plus railshot runtime/codegen coverage for all eight scalar opcodes, including NaN, negative unsigned, and overflow clamp cases) |
| Multi-value (multiple block/func results) | ✓ | ✅ done (decoder/validator, block/if/branch/br_if/br_table/function results, direct and cross-instance calls, public `Invoke`/typed `Call`, and `.wago` metadata are executable; optimized multi-result register ABI remains a performance item, not a semantics blocker) |
| Reference types (`funcref`/`externref`, `select t`, `ref.*`, `table.get/set`, multi-table) | ✓ | ✅ done: nullable and non-null `funcref` plus `externref` execute in signatures, locals, control, globals, host calls, typed elements, and multiple local/imported tables. Indexed get/set/size/grow/fill/copy/init/drop and nonzero-table `call_indirect` preserve exact type/store ownership. `Runtime.NewHostFuncRef`, `Runtime.NewFuncRefGlobal`, `Runtime.NewExternRefGlobal`, and `Runtime.NewExternRefTable` provide explicit bounded owners; raw unowned host descriptor egress stays fail-closed. Class pooling always reinstantiates local reference state and rejects imported reference globals/tables whose shared state cannot be reset between tenants; eligible explicit-bounds zero/one-page numeric instances use a measured in-place `ResetMemorySnapshot` fast path, with larger/unsupported shapes falling back. Snapshot products reject every table/reference-global module. Deterministic `ModuleMetadata` reports every function/global/table index, reference type, import, export, and exact declared limit, including duplicate aliases and codec-v20-loaded modules. Consolidated close-order tests cover shared globals, duplicate funcref table aliases, externref tables, traps, and producer teardown. The official Release 2 execution corpus is green at 1,600 modules and 48,248 assertions with zero gaps. |
| Bulk memory (`memory.copy`/`fill`/`init`, `data.drop`, `table.*`) | ✓ | ✅ done for linear memory plus funcref and externref tables: passive data/elements, active/declarative dropped state, indexed `table.init`/`table.copy`, `elem.drop`, overlap/bounds behavior, and all remaining table operations execute across compatible imported/local tables. |
| Tail calls (`return_call` / `return_call_indirect`) | ✓ | ⬜ planned |
| SIMD (`v128`) | ✓ | ✅ done for the documented linux/amd64 baseline. All decoded core SIMD and deterministic relaxed SIMD `0xfd` opcodes through 275 are validated, frontend-admitted, and lowered by railshot; 20 reserved proposal-table holes are invalid-decode tests. The documented baseline is SSSE3/SSE4.1 plus AVX/VEX.128 only; AVX2/FMA/VNNI remain future feature-gated fast paths. Public `v128` representation is `[16]byte` (`wago.V128`); locals, params/results, control flow, globals, cross-instance imports, and host imports/results are supported. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions), and relaxed SIMD coverage is locked by opcode parity and focused deterministic-lowering tests. See `docs/simd-relaxed-plan.md` and `docs/simd-performance-2026-07.md`. |
| Threads & atomics | ✓ | ⬜ planned |
| Synchronous host-import results | ✓ | ✅ done |
| WASI preview 1 (minimal) | ✓ | 🚧 partial — fd_write/read/close/seek/fdstat, proc_exit, args/environ, clock, random (the `wasi` plugin: `wasi.Ext`/`wasi.Imports`, CLI `--plugin wasi`); tracked via WebAssembly/wasi-testsuite |
| Architectures beyond linux/amd64 (arm64, macOS, Windows) | ✓ | 🚧 partial — Linux/arm64 and Darwin/arm64 support explicit and opt-in signal-backed guard-page bounds; Windows remains planned |
| Multi-memory | ✗ | ❌ not planned |
| Exception handling proposal | ✗ | ❌ not planned |
| Garbage collection proposal (wasm GC) | ✗ | ❌ not planned |
| Interpreter tier (wago is JIT-only) | ✗ | ❌ not planned |
