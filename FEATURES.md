# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. Target today is
**linux/amd64**. For the actionable plan behind the planned rows, see
[ROADMAP.md](ROADMAP.md).

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

## WebAssembly 1.0 (MVP)

The core spec. Completing this is the priority — globals and `memory.grow` are the
notable gaps that block running arbitrary compiler output.

| Feature | Planned | Status |
|---|:---:|---|
| i32 / i64 integer ops (arith, bitwise, shift/rotate, clz/ctz/popcnt, compare, eqz) | ✓ | ✅ done |
| f32 / f64 ops (add/sub/mul/div/sqrt/abs/neg/min/max, compare) | ✓ | ✅ done |
| f32 / f64 `ceil` / `floor` / `trunc` / `nearest` / `copysign` | ✓ | ⬜ planned |
| Conversions + reinterpret (wrap/extend/convert/trunc, i↔f bit casts) | ✓ | ✅ done |
| Float→int `trunc` NaN/overflow **traps** | ✓ | 🚧 computes, doesn't trap yet |
| Control flow: block / loop / if / else / br / br_if / br_table / return | ✓ | ✅ done |
| `call` / `call_indirect` (table + signature check) | ✓ | ✅ done |
| `select`, `drop`, `nop`, `unreachable` | ✓ | ✅ done |
| Locals (`local.get` / `local.set` / `local.tee`) | ✓ | ✅ done |
| **Globals (`global.get` / `global.set`, mutable)** | ✓ | ✅ done |
| Linear memory load/store (all widths, signed/unsigned) | ✓ | 🚧 done (i64 sub-width loads pending) |
| **`memory.size` / `memory.grow`** | ✓ | ⬜ planned |
| Active data segments | ✓ | ✅ done |
| Tables + active element segments | ✓ | ✅ done |
| Function imports / exports | ✓ | ✅ done (host imports: void, batched) |
| Memory / table / global imports & exports | ✓ | 🚧 partial (memory.grow pending) |
| `start` function | ✓ | ⬜ planned |

## Extra features (post-1.0)

Later proposals and engine/platform capabilities beyond the MVP.

| Feature | Planned | Status |
|---|:---:|---|
| Sign-extension ops (`i32.extend8_s`, …) | ✓ | ⬜ planned |
| Non-trapping float→int (`trunc_sat`) | ✓ | ⬜ planned |
| Multi-value (multiple block/func results) | ✓ | 🚧 partial |
| Reference types (`funcref`/`externref`, `select t`, `ref.*`, `table.get/set`, multi-table) | ✓ | 🚧 partial (`select t` done) |
| Bulk memory (`memory.copy`/`fill`/`init`, `data.drop`, `table.*`) | ✓ | ⬜ planned |
| Tail calls (`return_call` / `return_call_indirect`) | ✓ | ⬜ planned |
| SIMD (`v128`) | ✓ | ⬜ planned |
| Threads & atomics | ✓ | ⬜ planned |
| Synchronous host-import results | ✓ | ⬜ planned |
| WASI preview 1 | ✓ | ⬜ planned |
| Architectures beyond linux/amd64 (arm64, macOS, Windows) | ✓ | ⬜ planned |
| Multi-memory | ✗ | ❌ not planned |
| Exception handling proposal | ✗ | ❌ not planned |
| Garbage collection proposal (wasm GC) | ✗ | ❌ not planned |
| Interpreter tier (wago is JIT-only) | ✗ | ❌ not planned |
