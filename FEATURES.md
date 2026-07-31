# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. Linux, macOS, and
Windows on amd64 and arm64 are supported. The amd64 backend has a documented
modern CPU baseline: SSSE3/SSE4.1 plus AVX/VEX.128 XMM encodings, but not
AVX2/FMA/VNNI unless explicitly feature-gated later. For
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
remain unchanged. This official-suite result is not an unrestricted WasmGC
claim. Shape-independent struct/array helper admission now also compiles, links,
and starts the 3,225,249-byte MoonBit Starshine CLI smoke payload (SHA-256
`3a92309ca48f80594c88ea6c3508982d6fc34953c018ce31786382e08a18d046`). A
separate checked-in MoonBit JSON source fixture builds a pinned 44,023-byte,
import-free WasmGC module and verifies deterministic parse/stringify/reparse
checksums through `make test-moonbit-json`. Linux/amd64 generated code publishes exact roots across the admitted local,
indirect, reference, host-re-entry, EH, and same-Runtime cross-instance boundaries;
Throughput and Tiny collectors may collect while those native frames are active.
Codec v30 reloads and strictly validates the root metadata. Snapshot v4 captures
reachable local-global WasmGC graphs with stable IDs, cycles, and sharing. Exact
same-Runtime, descriptor-identical, global/table-free cross-instance calls transfer
compact references through one shared collector and walk foreign caller frames.
Arm64 explicit-bounds builds lower struct, array, and i31 helpers through the
parked synchronous ABI; active-frame collection there remains bounded/disabled
pending arm64 stack maps. Shared GC globals/tables, whole-domain snapshots, and
broader host ownership remain fail-closed.
See [docs/wasm3.md](docs/wasm3.md) for the implementation ledger.

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
| Reference types (`funcref`/`externref`, `select t`, `ref.*`, `table.get/set`, multi-table) | ✓ | ✅ done: nullable and non-null `funcref` plus `externref` execute in signatures, locals, control, globals, host calls, typed elements, and multiple local/imported tables. Indexed get/set/size/grow/fill/copy/init/drop and nonzero-table `call_indirect` preserve exact type/store ownership. `Runtime.NewHostFuncRef`, `Runtime.NewFuncRefGlobal`, `Runtime.NewExternRefGlobal`, and `Runtime.NewExternRefTable` provide explicit bounded owners; raw unowned host descriptor egress stays fail-closed. Snapshot products reject every table/reference-global module. Deterministic `ModuleMetadata` reports every function/global/table index, reference type, import, export, and exact declared limit, including duplicate aliases and codec-v28-loaded modules. Consolidated close-order tests cover shared globals, duplicate funcref table aliases, externref tables, traps, and producer teardown. The official Release 2 execution corpus is green at 1,600 modules and 48,248 assertions with zero gaps. |
| Bulk memory (`memory.copy`/`fill`/`init`, `data.drop`, `table.*`) | ✓ | ✅ done for linear memory plus funcref and externref tables: passive data/elements, active/declarative dropped state, indexed `table.init`/`table.copy`, `elem.drop`, overlap/bounds behavior, and all remaining table operations execute across compatible imported/local tables. |
| Extended constant expressions | ✓ | ✅ done for the basic Release 3 extension: `i32`/`i64` add/sub/mul, imported and prior immutable globals, compile-time folding, instantiate-time deferred evaluation for globals and active offsets, strict AST/byte-backed validation, and `.wago` v29 metadata (the initializer representation originated in v21). GC-added constant instructions remain in the GC row. |
| Tail calls (`return_call` / `return_call_indirect` / `return_call_ref`) | ✓ | ✅ done on the Core 3 linux/amd64 explicit-bounds product, including direct, indirect, typed-reference, host, cross-instance, trap, and validation paths exercised by the official suite. |
| Typed function references / `call_ref` | ✓ | ✅ done for Core 3: recursive structural typing, typed tables/elements/globals, `call_ref`, casts/tests, null branches, imports/exports, descriptor ownership, and official invalid/unlinkable behavior are gap-free. |
| Multi-memory | ✓ | ✅ done for Core 3 on linux/amd64 explicit bounds: indexed scalar/SIMD/bulk/data operations, compact imports, linking/store behavior, generalized shared-memory basedata serialization, metadata, codec, and official traps all pass. |
| memory64 | ✓ | ✅ done for the mandatory Core 3 suite on linux/amd64 explicit bounds, with exact u64 declaration metadata, full-width address checks, scalar/SIMD/bulk/data execution, imports/re-exports, and bounded reservations. |
| table64 | ✓ | ✅ done for the mandatory Core 3 suite on linux/amd64 explicit bounds, including mixed-width tables, full-u64 declarations, all table operations, indirect calls, imports, codec metadata, and `spectest.table64`. |
| Exception handling | ✓ | ✅ done for Core 3: arbitrary tag directories, imported/exported tags, `throw`, `throw_ref`, `try_table`, rooted exception references, tail interaction, linking, codec metadata, and official malformed/invalid cases are gap-free. |
| Garbage collection (WasmGC) | ✓ | ✅ mandatory Core 3 suite complete on linux/amd64 explicit bounds. 🚧 General generated payloads now have semantic struct/array helper admission, subtype-aware access, reference constructors, GC constant expressions, pointer-free `Array<v128>` construction/access/bulk/data initialization, two-slot `v128` struct fields, codec-v30 reload, replay-safe initialization snapshots for immutable local GC graphs, and passing MoonBit Starshine/JSON smoke coverage. A bounded linux/amd64 local-call-graph slice publishes liveness-exact local and hidden-spill roots through per-site IDs, walks cross-function and recursive callers by return-PC maps, persists those maps in codec v30, and supports verified Throughput/Tiny collection inside an invocation; direct tail calls discard their caller frame, while numeric host imports now preserve suspended outer roots across same-instance re-entry and mutable module-local GC globals synchronize checked collector slots before allocation. Private local `call_indirect` and `return_call_indirect` sites now publish exact roots; one private mutable collector-reference table is scanned directly at every collection; and generic struct/array execution is covered under linux/amd64 guard-page bounds checks. Local-only `call_ref`/`return_call_ref` now use exact callsites, and EH functions union conservative local masks with fixed GC-payload record roots. A bounded same-Runtime cross-instance slice now shares one collector for descriptor-identical, global/table-free modules, transfers non-null GC parameters/results without copying, and walks exact foreign caller frames. Snapshot v4 persists reachable local-global object graphs with stable IDs and two-pass cycle/sharing reconstruction. Arm64 explicit-bounds builds lower struct/array/i31 helpers through the synchronous ABI; collection while arm64 native frames are active remains disabled until exact arm64 stack maps land. See `docs/gc.md`. |
| SIMD (`v128`) | ✓ | ✅ done for the documented linux/amd64 baseline. All decoded core SIMD and deterministic relaxed SIMD `0xfd` opcodes through 275 are validated, frontend-admitted, and lowered by railshot; 20 reserved proposal-table holes are invalid-decode tests. The documented baseline is SSSE3/SSE4.1 plus AVX/VEX.128 only; AVX2/FMA/VNNI remain future feature-gated fast paths. Public `v128` representation is `[16]byte` (`wago.V128`); locals, params/results, control flow, globals, cross-instance imports, and host imports/results are supported. The official SIMD proposal corpus passes via WABT `wast2json` (24,325 assertions, 0 skipped modules/assertions). The pinned Release 3 relaxed-SIMD family also passes all 8 modules and 69 assertions, including official `either` result alternatives. See `docs/simd-relaxed-plan.md` and `docs/simd-performance-2026-07.md`. |
| Branch hinting (`metadata.code.branch_hint`) | ✓ | ✅ done: the current code-metadata wire format is decoded strictly (unique/pre-code section, ordered function/offset vectors, one-byte payload, and only `if`/`br_if` targets). ARM64 railshot uses `if` likelihood to weight local/global pinning and defers non-empty unlikely `br_if` reconciliation into cold target fragments. |
| Threads & atomics | ✓ | ⬜ planned |
| Synchronous host-import results | ✓ | ✅ done |
| Bounded function-pipeline parallelism | ✓ | ✅ done: validation and native codegen share one deterministic serial/adaptive/forced worker policy. The default is serial; `WithFunctionWorkers(0)` and CLI `-p` select adaptive mode, while explicit maxima remain capped by `GOMAXPROCS` and local-function count. |
| Invocation cancellation | ✓ | ✅ done on amd64 and arm64: Linux/amd64 and Linux/arm64 use a thread-directed reserved real-time signal, executable-range validation, and `ucontext` stack/PC rewriting, so generated Wasm contains no cancellation polls. Other targets use function-entry and loop-header safepoints. Closing an active instance publishes the same interruption request, while invocation leases defer unmapping until unwind completes. See `docs/linux-host-interrupt.md`. |
| Linux, macOS, and Windows on amd64 and arm64 | ✓ | ✅ done — all six release targets execute the encoder, backend, runtime/API, explicit bounds, corpus, and SIMD suites in native CI. Linux/amd64, Linux/arm64, and Darwin/arm64 additionally support signal-backed guard pages and Linux uses signal-context asynchronous cancellation; the other targets use compiler-emitted cooperative safepoints. |
| Interpreter tier (wago is JIT-only) | ✗ | ❌ not planned |

### Iteration 72 staged M8 boundary

The exact source-lines-652–659 M8 provider/four-import consumer pair is admitted without widening public WasmGC support. The 100-byte provider and 92-byte consumer have wasm/code/codec sizes 100/253/531 and 92/0/315 bytes, own bounded 96/160-byte descriptor arenas, deduplicate four imports to one retained producer, and keep `Instance.gc` nil. Official type-subtyping accounting is 40 passed modules / 23 passed assertions / 5 exact gates / 11 blocked commands / 24 invalid / 6 executed plus 2 blocked unlinkables / zero validator gaps, unexpected failures, or hidden failures.

### Iteration 73 complete `gc/type-subtyping` boundary

The exact M9 eight-import recursive link pair, both M10/M11 expected unlinkables, and all six non-flat exported-function assertions are now admitted and executed. Official accounting is complete at 170 commands / 45 passed modules / 29 passed assertions / 24 invalid / 8 expected unlinkables / zero gates, blocked commands, validator gaps, unexpected failures, or hidden failures. This completes the official `gc/type-subtyping.wast` file without widening unrelated Core 3 public admission.
