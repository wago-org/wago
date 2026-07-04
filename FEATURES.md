# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. Target today is
**linux/amd64**. For the actionable plan behind the planned rows, see
[ROADMAP.md](ROADMAP.md).

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

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
| **Globals (`global.get` / `global.set`, mutable)** | ✓ | ✅ done for numeric globals (`i32`, `i64`, `f32`, `f64`); reference/vector globals are rejected clearly |
| Linear memory load/store (all widths, signed/unsigned) | ✓ | ✅ done |
| **`memory.size` / `memory.grow`** | ✓ | ✅ done (grow up to the declared max via an up-front reservation; no remap) |
| Active data segments | ✓ | ✅ done |
| Tables + active element segments | ✓ | ✅ done |
| Function imports / exports | ✓ | ✅ done (host imports: void result, any numeric params, batched replay; cross-instance function calls via a link-time recompile + native context-swap) |
| Memory / table / global imports & exports | ✓ | ✅ done (cross-instance function / global / table / memory linking, incl. shared mutable tables + memories, and host functions used as table funcrefs) |
| `start` function | ✓ | ✅ done (local, or an imported void host function) |

## Extra features (post-1.0)

Later proposals and engine/platform capabilities beyond the MVP.

| Feature | Planned | Status |
|---|:---:|---|
| Sign-extension ops (`i32.extend8_s`, …) | ✓ | ✅ done |
| Non-trapping float→int (`trunc_sat`) | ✓ | ✅ done |
| Multi-value (multiple block/func results) | ✓ | 🚧 partial |
| Reference types (`funcref`/`externref`, `select t`, `ref.*`, `table.get/set`, multi-table) | ✓ | 🚧 partial (`select t` done) |
| Bulk memory (`memory.copy`/`fill`/`init`, `data.drop`, `table.*`) | ✓ | 🚧 partial (`memory.copy`/`memory.fill` done; `memory.init`, `data.drop`, `table.*` planned) |
| Tail calls (`return_call` / `return_call_indirect`) | ✓ | ⬜ planned |
| SIMD (`v128`) | ✓ | ⬜ planned |
| Threads & atomics | ✓ | ⬜ planned |
| Synchronous host-import results | ✓ | ✅ done |
| WASI preview 1 | ✓ | ⬜ planned |
| Architectures beyond linux/amd64 (arm64, macOS, Windows) | ✓ | ⬜ planned |
| Multi-memory | ✗ | ❌ not planned |
| Exception handling proposal | ✗ | ❌ not planned |
| Garbage collection proposal (wasm GC) | ✗ | ❌ not planned |
| Interpreter tier (wago is JIT-only) | ✗ | ❌ not planned |
