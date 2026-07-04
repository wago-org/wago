# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. Target today is
**linux/amd64** with a documented modern CPU baseline: SSE4.1 plus AVX/VEX.128
XMM encodings, but not AVX2/FMA/VNNI unless explicitly feature-gated later. For
the actionable plan behind the planned rows, see [ROADMAP.md](ROADMAP.md).

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

## WebAssembly 1.0 (MVP)

The core spec. Completing this is the priority — `memory.grow` is the remaining
notable MVP gap that blocks running arbitrary compiler output.

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
| Function imports / exports | ✓ | ✅ done (host imports: void, batched) |
| Memory / table / global imports & exports | ✓ | 🚧 partial (global imports/exports done; memory/table import gaps, memory.grow pending) |
| `start` function | ✓ | ✅ done (local; imported/host start rejected) |

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
| SIMD (`v128`) | ✓ | 🚧 partial (amd64: `v128` params/locals/results, `v128.const`, `v128.load`/`store`, splats, lane extract/replace, `v128.and`/`andnot`/`or`/`xor`/`not`/`bitselect`, `v128.any_true`, all_true/bitmask for i8x16/i16x8/i32x4/i64x2, integer neg for i8/i16/i32/i64 lanes, abs for i8/i16/i32 lanes, i8x16 popcnt, signed/unsigned i8 narrow from i16 lanes, signed/unsigned i16 narrow from i32 lanes, signed/unsigned i8-to-i16, i16-to-i32, and i32-to-i64 widening extends, pairwise extadd from i8-to-i16 and i16-to-i32 lanes, signed/unsigned i8-to-i16, i16-to-i32, and i32-to-i64 extmul, add/sub for i8/i16/i32/i64 lanes, saturating add/sub for i8/i16 lanes, i16 q15mulr_sat_s, mul for i16/i32 lanes, eq/ne for those lanes, signed and unsigned ordered comparisons for i8/i16/i32, signed/unsigned min/max for i8/i16/i32, unsigned rounding averages for i8/i16, and f32x4/f64x2 abs/neg/sqrt/add/sub/mul/div plus comparisons; frontend rejects unsupported `0xfd`) |
| Threads & atomics | ✓ | ⬜ planned |
| Synchronous host-import results | ✓ | ⬜ planned |
| WASI preview 1 | ✓ | ⬜ planned |
| Architectures beyond linux/amd64 (arm64, macOS, Windows) | ✓ | ⬜ planned |
| Multi-memory | ✗ | ❌ not planned |
| Exception handling proposal | ✗ | ❌ not planned |
| Garbage collection proposal (wasm GC) | ✗ | ❌ not planned |
| Interpreter tier (wago is JIT-only) | ✗ | ❌ not planned |
