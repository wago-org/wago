# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. Target today is
**linux/amd64** with a documented modern CPU baseline: SSSE3/SSE4.1 plus
AVX/VEX.128 XMM encodings, but not AVX2/FMA/VNNI unless explicitly feature-gated later. For
the actionable plan behind the planned rows, see [ROADMAP.md](ROADMAP.md).

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

`CoreFeaturesV2` is the static WebAssembly 2.0 release feature group and includes
core SIMD. It describes the specification's feature grouping, not a claim that
every family below is complete. Use `SupportedFeatures()` for build- and
host-admitted gates; it can omit SIMD when the CPU lacks the documented baseline.

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
| **Globals (`global.get` / `global.set`, mutable)** | ✓ | 🚧 numeric and `v128` globals are complete; module-local `funcref` globals support `ref.null` and structural `ref.func` initializers, mutable get/set, exported typed access, and same-store non-null token round trips. Imported/shared funcref globals and externref globals remain rejected clearly. |
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
| Reference types (`funcref`/`externref`, `select t`, `ref.*`, `table.get/set`, multi-table) | ✓ | 🚧 partial: nullable/local `funcref`, structural `ref.func`, typed `select`, local funcref globals, and multiple funcref tables execute when tables are all local or table 0 is the sole import followed by local definitions. Indexed active elements, all `table.*` operations, cross-table copy/init, nonzero-table `call_indirect`, and exact named exports/re-exports are supported for that shape. Multiple imported tables, broader host/shared funcref boundaries, and executable `externref` remain. |
| Bulk memory (`memory.copy`/`fill`/`init`, `data.drop`, `table.*`) | ✓ | ✅ done for linear memory and funcref tables: passive data/elements, drop state, indexed `table.init`/`table.copy`, and the remaining table operations execute across multiple local tables. Externref-table execution remains part of reference-types completion. |
| Tail calls (`return_call` / `return_call_indirect`) | ✓ | ⬜ planned |
| SIMD (`v128`) | ✓ | ✅ done for the documented linux/amd64 baseline. All decoded core SIMD and deterministic relaxed SIMD `0xfd` opcodes through 275 are validated, frontend-admitted, and lowered by railshot; 20 reserved proposal-table holes are invalid-decode tests. The documented baseline is SSSE3/SSE4.1 plus AVX/VEX.128 only; AVX2/FMA/VNNI remain future feature-gated fast paths. Public `v128` representation is `[16]byte` (`wago.V128`); locals, params/results, control flow, globals, cross-instance imports, and host imports/results are supported. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions), and relaxed SIMD coverage is locked by opcode parity and focused deterministic-lowering tests. See `docs/simd-relaxed-plan.md` and `docs/simd-performance-2026-07.md`. |
| Threads & atomics | ✓ | ⬜ planned |
| Synchronous host-import results | ✓ | ✅ done |
| WASI preview 1 (minimal) | ✓ | 🚧 partial — fd_write/read/close/seek/fdstat, proc_exit, args/environ, clock, random (the `wasi` plugin: `wasi.Ext`/`wasi.Imports`, CLI `--plugin wasi`); tracked via WebAssembly/wasi-testsuite |
| Architectures beyond linux/amd64 (arm64, macOS, Windows) | ✓ | ⬜ planned |
| Multi-memory | ✗ | ❌ not planned |
| Exception handling proposal | ✗ | ❌ not planned |
| Garbage collection proposal (wasm GC) | ✗ | ❌ not planned |
| Interpreter tier (wago is JIT-only) | ✗ | ❌ not planned |
