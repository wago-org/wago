# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. Target today is
**linux/amd64** with a documented modern CPU baseline: SSSE3/SSE4.1 plus
AVX/VEX.128 XMM encodings, but not AVX2/FMA/VNNI unless explicitly feature-gated later. For
the actionable plan behind the planned rows, see [ROADMAP.md](ROADMAP.md).

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

`CoreFeaturesV2` is the static WebAssembly 2.0 release group. `CoreFeaturesV3`
describes the mandatory WebAssembly Core 3.0 scope, and `SupportedFeatures()`
reports the executable build/host set. The compatibility default remains the
Release 2 feature set plus completed extended constants; callers opt into the
full Release 3 surface with `WithCoreFeatures(CoreFeaturesV3)`. On the primary
linux/amd64 explicit-bounds target, every Core 3 family is admitted and the
pinned 258-file suite is green: **2,226 modules and 58,038 assertions passed,
with zero failures, skips, or gap categories**. Release 1 and Release 2 defaults
remain unchanged. See [docs/wasm3.md](docs/wasm3.md) for the implementation
ledger.

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
| Reference types (`funcref`/`externref`, `select t`, `ref.*`, `table.get/set`, multi-table) | ✓ | ✅ done: nullable and non-null `funcref` plus `externref` execute in signatures, locals, control, globals, host calls, typed elements, and multiple local/imported tables. Indexed get/set/size/grow/fill/copy/init/drop and nonzero-table `call_indirect` preserve exact type/store ownership. `Runtime.NewHostFuncRef`, `Runtime.NewFuncRefGlobal`, `Runtime.NewExternRefGlobal`, and `Runtime.NewExternRefTable` provide explicit bounded owners; raw unowned host descriptor egress stays fail-closed. Snapshot products reject every table/reference-global module. Deterministic `ModuleMetadata` reports every function/global/table index, reference type, import, export, and exact declared limit, including duplicate aliases and codec-v27-loaded modules. Consolidated close-order tests cover shared globals, duplicate funcref table aliases, externref tables, traps, and producer teardown. The official Release 2 execution corpus is green at 1,600 modules and 48,248 assertions with zero gaps. |
| Bulk memory (`memory.copy`/`fill`/`init`, `data.drop`, `table.*`) | ✓ | ✅ done for linear memory plus funcref and externref tables: passive data/elements, active/declarative dropped state, indexed `table.init`/`table.copy`, `elem.drop`, overlap/bounds behavior, and all remaining table operations execute across compatible imported/local tables. |
| Extended constant expressions | ✓ | ✅ done for the basic Release 3 extension: `i32`/`i64` add/sub/mul, imported and prior immutable globals, compile-time folding, instantiate-time deferred evaluation for globals and active offsets, strict AST/byte-backed validation, and `.wago` v27 metadata (the initializer representation originated in v21). GC-added constant instructions remain in the GC row. |
| Tail calls (`return_call` / `return_call_indirect` / `return_call_ref`) | ✓ | ✅ done on the Core 3 linux/amd64 explicit-bounds product, including direct, indirect, typed-reference, host, cross-instance, trap, and validation paths exercised by the official suite. |
| Typed function references / `call_ref` | ✓ | ✅ done for Core 3: recursive structural typing, typed tables/elements/globals, `call_ref`, casts/tests, null branches, imports/exports, descriptor ownership, and official invalid/unlinkable behavior are gap-free. |
| Multi-memory | ✓ | ✅ done for Core 3 on linux/amd64 explicit bounds: indexed scalar/SIMD/bulk/data operations, compact imports, linking/store behavior, generalized shared-memory basedata serialization, metadata, codec, and official traps all pass. |
| memory64 | ✓ | ✅ done for the mandatory Core 3 suite on linux/amd64 explicit bounds, with exact u64 declaration metadata, full-width address checks, scalar/SIMD/bulk/data execution, imports/re-exports, and bounded reservations. |
| table64 | ✓ | ✅ done for the mandatory Core 3 suite on linux/amd64 explicit bounds, including mixed-width tables, full-u64 declarations, all table operations, indirect calls, imports, codec metadata, and `spectest.table64`. |
| Exception handling | ✓ | ✅ done for Core 3: arbitrary tag directories, imported/exported tags, `throw`, `throw_ref`, `try_table`, rooted exception references, tail interaction, linking, codec metadata, and official malformed/invalid cases are gap-free. |
| Garbage collection (WasmGC) | ✓ | ✅ mandatory Core 3 suite complete on linux/amd64 explicit bounds: recursive/subtyped types, struct/array/i31 operations, conversions, casts/tests/branches, data/element array initialization, linking, roots/barriers, collector profiles, metadata, and all official invalid/unlinkable behavior pass. Bounded implementation and platform limits remain documented in `docs/gc.md`. |
| SIMD (`v128`) | ✓ | ✅ done for the documented linux/amd64 baseline. All decoded core SIMD and deterministic relaxed SIMD `0xfd` opcodes through 275 are validated, frontend-admitted, and lowered by railshot; 20 reserved proposal-table holes are invalid-decode tests. The documented baseline is SSSE3/SSE4.1 plus AVX/VEX.128 only; AVX2/FMA/VNNI remain future feature-gated fast paths. Public `v128` representation is `[16]byte` (`wago.V128`); locals, params/results, control flow, globals, cross-instance imports, and host imports/results are supported. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions). The pinned Release 3 relaxed-SIMD family also passes all 8 modules and 69 assertions, including official `either` result alternatives. See `docs/simd-relaxed-plan.md` and `docs/simd-performance-2026-07.md`. |
| Branch hinting (`metadata.code.branch_hint`) | ✓ | ✅ done: the current code-metadata wire format is decoded strictly (unique/pre-code section, ordered function/offset vectors, one-byte payload, and only `if`/`br_if` targets). ARM64 railshot uses `if` likelihood to weight local/global pinning and defers non-empty unlikely `br_if` reconciliation into cold target fragments. |
| Threads & atomics | ✓ | ⬜ planned |
| Synchronous host-import results | ✓ | ✅ done |
| Bounded function-pipeline parallelism | ✓ | ✅ done: validation and native codegen share one deterministic serial/adaptive/forced worker policy. The default is serial; `WithFunctionWorkers(0)` and CLI `-p` select adaptive mode, while explicit maxima remain capped by `GOMAXPROCS` and local-function count. |
| Cooperative invocation cancellation | ✓ | 🚧 partial — ARM64 `Instance.Call(ctx, ...)` interrupts native execution at function entries and loop headers; amd64 currently honors cancellation before entry only. |
| Architectures beyond linux/amd64 (arm64, macOS, Windows) | ✓ | 🚧 partial — Linux/arm64 and Darwin/arm64 have native CI for the encoder, backend, runtime/API, explicit and signal-backed guard-page bounds, and corpus correctness. ARM64 reference globals and heterogeneous indexed tables execute; amd64 cancellation polling and Windows remain planned. |
| Interpreter tier (wago is JIT-only) | ✗ | ❌ not planned |

### Iteration 72 staged M8 boundary

The exact source-lines-652–659 M8 provider/four-import consumer pair is admitted without widening public WasmGC support. The 100-byte provider and 92-byte consumer have wasm/code/codec sizes 100/253/531 and 92/0/315 bytes, own bounded 96/160-byte descriptor arenas, deduplicate four imports to one retained producer, and keep `Instance.gc` nil. Official type-subtyping accounting is 40 passed modules / 23 passed assertions / 5 exact gates / 11 blocked commands / 24 invalid / 6 executed plus 2 blocked unlinkables / zero validator gaps, unexpected failures, or hidden failures.

### Iteration 73 complete `gc/type-subtyping` boundary

The exact M9 eight-import recursive link pair, both M10/M11 expected unlinkables, and all six non-flat exported-function assertions are now admitted and executed. Official accounting is complete at 170 commands / 45 passed modules / 29 passed assertions / 24 invalid / 8 expected unlinkables / zero gates, blocked commands, validator gaps, unexpected failures, or hidden failures. This completes the official `gc/type-subtyping.wast` file without widening unrelated Core 3 public admission.
