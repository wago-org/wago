# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. The primary target
is **linux/amd64**, with native **linux/arm64**, **darwin/arm64**, and
**linux/riscv64** backends. The amd64 CPU baseline is SSSE3/SSE4.1 plus
AVX/VEX.128 XMM encodings, but not AVX2/FMA/VNNI unless explicitly feature-gated later. For
the actionable plan behind the planned rows, see [ROADMAP.md](ROADMAP.md).

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

`CoreFeaturesV2` is the static WebAssembly 2.0 release feature group and includes
core SIMD. `SupportedFeatures()` is the build- and host-admitted form of that
group; it omits SIMD when the selected backend lacks its documented baseline.
Linux/RV64 provides SIMD through RV64G SWAR and does not require RVV. Post-release
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
| Multi-value (multiple block/func results) | ✓ | ✅ done (decoder/validator, block/if/branch/br_if/br_table/function results, direct and cross-instance calls, public `Invoke`/typed `Call`, and `.wago` metadata are executable; ARM64 additionally returns two integer results in `X0/X1`, while broader optimized register-return shapes remain a performance item) |
| Reference types (`funcref`/`externref`, `select t`, `ref.*`, `table.get/set`, multi-table) | ✓ | ✅ done: nullable and non-null `funcref` plus `externref` execute in signatures, locals, control, globals, host calls, typed elements, and multiple local/imported tables. Indexed get/set/size/grow/fill/copy/init/drop and nonzero-table `call_indirect` preserve exact type/store ownership. `Runtime.NewHostFuncRef`, `Runtime.NewFuncRefGlobal`, `Runtime.NewExternRefGlobal`, and `Runtime.NewExternRefTable` provide explicit bounded owners; raw unowned host descriptor egress stays fail-closed. Snapshot products reject every table/reference-global module. Deterministic `ModuleMetadata` reports every function/global/table index, reference type, import, export, and exact declared limit, including duplicate aliases and codec-v20-loaded modules. Consolidated close-order tests cover shared globals, duplicate funcref table aliases, externref tables, traps, and producer teardown. The official Release 2 execution corpus is green at 1,600 modules and 48,248 assertions with zero gaps. |
| Bulk memory (`memory.copy`/`fill`/`init`, `data.drop`, `table.*`) | ✓ | ✅ done for linear memory plus funcref and externref tables: passive data/elements, active/declarative dropped state, indexed `table.init`/`table.copy`, `elem.drop`, overlap/bounds behavior, and all remaining table operations execute across compatible imported/local tables. |
| Tail calls (`return_call` / `return_call_indirect`) | ✓ | ⬜ planned |
| SIMD (`v128`) | ✓ | ✅ done on linux/amd64 and linux/riscv64. All 256 decoded core SIMD and relaxed-SIMD `0xfd` instructions through 275 are validated, frontend-admitted, and lowered by railshot; 20 reserved proposal-table holes remain invalid. AMD64 uses SSSE3/SSE4.1 plus AVX/VEX.128. RV64 uses two-GPR little-endian `v128` values and RV64G SWAR, including memory, globals, control, direct/indirect/cross-instance calls, host calls, and deterministic relaxed projections; RVV is optional future optimization. Under QEMU, the current official core SIMD corpus passes 473 modules and 24,335 assertions with zero failures; its one multi-memory module is reported separately as the existing project-wide unsupported feature. The relaxed-SIMD corpus passes 8 modules and 69 assertions with zero gaps. Public representation is `[16]byte` (`wago.V128`). See `docs/simd-relaxed-plan.md`, `docs/simd-performance-2026-07.md`, and `docs/riscv64-port.md`. |
| Branch hinting (`metadata.code.branch_hint`) | ✓ | ✅ done: the current code-metadata wire format is decoded strictly (unique/pre-code section, ordered function/offset vectors, one-byte payload, and only `if`/`br_if` targets). ARM64 railshot uses `if` likelihood to weight local/global pinning and defers non-empty unlikely `br_if` reconciliation into cold target fragments. |
| Threads & atomics | ✓ | ⬜ planned |
| Synchronous host-import results | ✓ | ✅ done |
| Cooperative invocation cancellation | ✓ | ✅ done on native amd64, arm64, and linux/riscv64 backends: `Instance.Call(ctx, ...)` and `InvokeContext` interrupt execution at function entries and loop headers, including synchronous host-call loops, and leave the trap cell reusable for later calls. |
| Architectures beyond linux/amd64 (arm64, riscv64, embedded arm32/riscv32, macOS, Windows) | ✓ | 🚧 partial — Linux/arm64 and Darwin/arm64 have native CI for the encoder, backend, runtime/API, explicit and signal-backed guard-page bounds, and corpus correctness. Linux/riscv64 has a native RV64G encoder, no-cgo runtime, explicit and signal-backed guard-page bounds, full public scalar/`v128` API coverage, exact scalar corpus parity with amd64, and zero-failure curated WebAssembly 1.0 plus official SIMD/relaxed-SIMD execution under QEMU. Experimental RP2350 work has cross-host RV32IM and Thumb-2 encoders; complete mixed one/two/four-slot module frames; scalar, bulk-memory, table/reference, imported-callback/global/memory/table, direct/indirect-call, start/export-entry, and 256-op SIMD lowering; fixed-capacity runtime resources; and a bounded closed-module firmware-image builder. It is not public backend support: shared imported-memory/table bundle descriptors, RP2350 SDK transport I/O/low-level generated-entry invocation, official module-suite execution, and Pico 2 qualification remain. Optional RVV optimization, native-hardware validation, and Windows remain planned. |
| Multi-memory | ✗ | ❌ not planned |
| Exception handling proposal | ✗ | ❌ not planned |
| Garbage collection proposal (wasm GC) | ✗ | ❌ not planned |
| Interpreter tier (wago is JIT-only) | ✗ | ❌ not planned |
