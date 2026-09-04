# wago feature support

WebAssembly feature support for the pure-Go (no cgo) engine. Linux, macOS, and
Windows on amd64 and arm64 are supported. The amd64 backend has a documented
modern CPU baseline: SSSE3/SSE4.1/SSE4.2 plus AVX/VEX.128 XMM encodings, but not
AVX2/FMA/VNNI unless explicitly feature-gated later. For
the actionable plan behind the planned rows, see [ROADMAP.md](ROADMAP.md).

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

`CoreFeaturesV2` is the static WebAssembly 2.0 release group. `CoreFeaturesV3`
describes the mandatory WebAssembly Core 3.0 scope, and `SupportedFeatures()`
reports the executable build/host set. The compatibility default remains the
Release 2 feature set plus completed extended constants on incomplete backends.
Complete Core 3 backends additionally default on tail calls, typed function
references, multi-memory, memory64, and table64. WasmGC and exception handling
remain opt-in; callers select the full Release 3 surface with
`WithCoreFeatures(CoreFeaturesV3)`. On
linux/amd64 explicit/signal builds and Linux/Darwin arm64 explicit/signal builds,
every Core 3 family is admitted. ARM64 signal mode uses guard faults for eligible
memory-0 memory32 accesses and retains explicit checks for indexed memories and
memory64; native `spec3-signals` cells are mandatory on both operating systems. The pinned 258-file suite is green on linux/amd64 and under Linux/arm64
QEMU execution: **2,226 modules and 58,038 assertions passed, with zero failures,
skips, or gap categories**. These totals come from the checked-in
`tests/spec-v3-baseline.json`, which is the machine-readable source of truth. Native Linux/Darwin arm64 runs are mandatory CI gates.
Linux/amd64 signal-backed builds now admit every Core 3 family and pass the same
**2,226 modules and 58,038 assertions with zero failures, skips, or gaps**.
Indexed nonzero memories retain explicit directory bounds checks, while memory64
retains full-u64 explicit checks inside the signal-backed product.
`memory64` and typed function references including `call_ref` are implemented
on those explicitly admitted Core 3 products; this is not a claim for every Wago
target. `SupportedFeatures()` remains the executable build/host authority.
Explicit `CoreFeaturesV1` and `CoreFeaturesV2` selections remain unchanged. This official-suite result is
not an unrestricted WasmGC claim. Shape-independent struct/array helper admission now also compiles, links,
and starts the 3,225,249-byte MoonBit Starshine CLI smoke payload (SHA-256
`3a92309ca48f80594c88ea6c3508982d6fc34953c018ce31786382e08a18d046`).
Linux/amd64 generated code publishes exact roots across the admitted local,
indirect, reference, host-re-entry, EH, and same-Runtime cross-instance boundaries;
Throughput and Tiny collectors may collect while those native frames are active.
Root admission compacts locals dead at every collecting site and emits variable-size
exact root vectors; the old 1,024-live-root semantic ceiling is removed. Function
parameters plus declared locals now default to the current 65,535 representation
maximum, while a lower `WithMaxFunctionLocals` value remains optional admission
control. The independent native stack-fence check is unchanged.
The foreign execution stack keeps a 4 MiB default and fixed 256 KiB fence; callers
may select an aligned 512 KiB through 1 GiB capacity with
`RuntimeConfig.WithNativeStackBytes` or `wago run --native-stack`. Instance and
host-re-entry engines preserve the selected capacity, and the bounded cache reuses
only exact-capacity mappings. Codec version 2 reloads and strictly validates the
root metadata. Exact
same-Runtime cross-instance calls canonicalize recursive structural identities across
reordered or additional module-local types, transfer compact references through one
shared collector, and walk foreign caller frames. Imported/exported
collector-reference globals now share that domain for immutable and mutable aliases;
every live domain cell is scanned directly during collection, and checked slots retain
barrier/card state. Multiple heterogeneous imported/exported collector-reference
tables share the same exact domain, direct descriptor roots, growth state, rollback,
and close ordering. Exact GC-reference function imports coexist with those persistent
roots across collection, codec reload, and producer-first close.
Arm64 explicit-bounds builds lower struct, array, i31, cast/test, and branch-cast
helpers through the parked synchronous ABI. Exact roots cover locals, hidden
spills, direct/recursive calls, direct host re-entry, same-domain foreign calls,
mutable/shared GC globals, local/shared collector tables, indirect/reference calls,
discarded-frame proper tails, and fixed EH payload records. Native ARM64 also
publishes every native return path for polymorphic local `call_indirect` and
same-domain foreign `call_ref`, including the 64-byte cross-instance wrapper save
area. Descriptor-resolved `return_call_ref` now lowers local internal targets, host
wrappers, and retained same-Runtime cross-instance wrappers without retaining the
caller frame, including targets loaded from mutable/imported funcref tables. GC-bearing
typed tails compare exact collector-domain identities before discarding the caller;
host or foreign-domain GC transfer remains fail-closed. Dynamic indirect tails,
function-subtype checks, multi-memory, memory64, table64, `try_table`, `throw`, and
`throw_ref` execute. Multiple heterogeneous imported/exported collector-reference
tables share exact alias roots, growth, attachment rollback, codec reload, and close
ordering. Generic struct/array results may be retained as opaque `GCRef` tokens per producer
with exact store ownership, reusable inline slots plus dynamic overflow storage,
transactional multi-result rollback, and release after producer close.
Tokens may re-enter the same collector domain through exact structural subtype checks and reusable checked argument roots; stale, foreign,
and cross-domain tokens reject. `Runtime.NewGCHostFuncRef` additionally binds one
explicit host owner to the first importing canonical Runtime GC domain. Direct,
indirect, `call_ref`, and proper `return_call_ref` host transfers use temporary
opaque argument tokens, exact typed result roots, bounded parked activations, and
pre-discard domain checks; ordinary host owners and direct foreign Runtime domains
remain fail-closed. Explicit `target.CloneGCRefFrom(source, ref)` instead performs a
bounded transactional graph clone across distinct Runtime stores, preserving cycles
and internal sharing while assigning new target identity and rejecting non-null
opaque store-owned payloads.
See [docs/wasm3.md](docs/wasm3.md) for the implementation ledger and
[docs/function-local-limits.md](docs/function-local-limits.md) for the local and
root-map bounds.

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
| **`memory.size` / `memory.grow`** | ✓ | ✅ done (grow up to the declared max via an up-front reservation; no remap). Memory32 supports the complete 65,536-page / 4-GiB boundary through an authoritative u64 byte-size cache while preserving u32 page semantics; host-created memory rejects limits above 65,536 instead of truncating them. |
| Active data segments | ✓ | ✅ done |
| Tables + active element segments | ✓ | ✅ done |
| Function imports / exports | ✓ | ✅ done (host imports: numeric scalars and `v128` params/results via synchronous parked-host dispatch, including legacy void `HostFunc`; every imported call compiles once and binds through a per-instance dispatch cell with native context switching and active-callee host routing) |
| Memory / table / global imports & exports | ✓ | ✅ done (cross-instance function / global / table / memory linking, incl. shared mutable tables + memories, and host functions used as table funcrefs) |
| `start` function | ✓ | ✅ done (local, or an imported void host function) |

## Extra features (post-1.0)

Later proposals and engine/platform capabilities beyond the MVP.

| Feature | Planned | Status |
|---|:---:|---|
| WebAssembly Component Model | ✓ | ✅ Preview 2 component binaries execute through the Authority-gated [`github.com/wago-org/component-model`](https://github.com/wago-org/component-model) plugin, including typed values and resources, canonical ABI lift/lower, nested composition, typed host imports, and safe synchronous adapter re-entry. It provides the typed `github.com/wago-org/component-model/runtime` Contract at major version 1 for WASI and other component-world plugins while remaining absent from core-only dependency graphs. The plugin also contains an experimental Preview 3 async task/stream substrate; WASI policy and guest capabilities remain in the separate [`github.com/wago-org/wasi`](https://github.com/wago-org/wasi) host plugin. |
| Sign-extension ops (`i32.extend8_s`, …) | ✓ | ✅ done (decoder/validator plus railshot runtime/codegen coverage for all five scalar opcodes) |
| Non-trapping float→int (`trunc_sat`) | ✓ | ✅ done (decoder/validator plus railshot runtime/codegen coverage for all eight scalar opcodes, including NaN, negative unsigned, and overflow clamp cases) |
| Multi-value (multiple block/func results) | ✓ | ✅ done (decoder/validator, block/if/branch/br_if/br_table/function results, direct and cross-instance calls, public `Invoke`/typed `Call`, and `.wago` metadata are executable; ARM64 additionally returns two integer results in `X0/X1`, while broader optimized register-return shapes remain a performance item) |
| Reference types (`funcref`/`externref`, `select t`, `ref.*`, `table.get/set`, multi-table) | ✓ | ✅ done: nullable and non-null `funcref` plus `externref` execute in signatures, locals, control, globals, host calls, typed elements, and multiple local/imported tables. Indexed get/set/size/grow/fill/copy/init/drop and nonzero-table `call_indirect` preserve exact type/store ownership. `Runtime.NewHostFuncRef`, `Runtime.NewFuncRefGlobal`, `Runtime.NewExternRefGlobal`, and `Runtime.NewExternRefTable` provide explicit bounded owners; raw unowned host descriptor egress stays fail-closed. Deterministic `ModuleMetadata` reports every function/global/table index, reference type, import, export, and exact declared limit, including duplicate aliases and codec-loaded modules. Consolidated close-order tests cover shared globals, duplicate funcref table aliases, externref tables, traps, and producer teardown. The official Release 2 execution corpus is green at 1,600 modules and 48,248 assertions with zero gaps. |
| Bulk memory (`memory.copy`/`fill`/`init`, `data.drop`, `table.*`) | ✓ | ✅ done for linear memory plus funcref and externref tables: passive data/elements, active/declarative dropped state, indexed `table.init`/`table.copy`, `elem.drop`, overlap/bounds behavior, and all remaining table operations execute across compatible imported/local tables. |
| Extended constant expressions | ✓ | ✅ done for the basic Release 3 extension: `i32`/`i64` add/sub/mul, imported and prior immutable globals, compile-time folding, instantiate-time deferred evaluation for globals and active offsets, strict AST/byte-backed validation, and `.wago` version 2 metadata (the initializer representation originated in v21). GC-added constant instructions remain in the GC row. |
| Tail calls (`return_call` / `return_call_indirect` / `return_call_ref`) | ✓ | ✅ done on Core 3 linux/amd64 and Linux/Darwin arm64 explicit-bounds products, including direct, dynamic indirect, typed-reference, host, cross-instance, trap, subtype-result, and validation paths exercised by the official suite. |
| Typed function references / `call_ref` | ✓ | ✅ done for Core 3: recursive structural typing, typed tables/elements/globals, `call_ref`, casts/tests, null branches, imports/exports, descriptor ownership, and official invalid/unlinkable behavior are gap-free. Modules with dynamic indexed-function `ref.test` retain compact exact per-function type IDs beside the unchanged canonical descriptors, so tests remain correct after a funcref is loaded from GC structs or other storage; Dewdrop-style direct/environment-first closure dispatch is covered on amd64 and arm64. |
| Multi-memory | ✓ | ✅ done for Core 3 on linux/amd64 explicit and signal-backed bounds; Linux/Darwin arm64 explicit bounds execute indexed scalar/SIMD/bulk/data operations, size/grow, imports, directory refresh, traps, and codec reload. Signal mode uses guard-backed owned mappings for safe export/re-import while nonzero-memory accesses retain explicit directory checks. |
| memory64 | ✓ | ✅ done for the mandatory Core 3 suite on linux/amd64 explicit and signal-backed bounds; Linux/Darwin arm64 explicit bounds implement full-u64 scalar/float/SIMD memargs, carry-checked bulk/data operations, i64 size/grow, imports, codec, and bounded reservations. The amd64 signal product intentionally retains explicit full-u64 checks. |
| table64 | ✓ | ✅ done for the mandatory Core 3 suite on linux/amd64 explicit and signal-backed bounds; Linux/Darwin arm64 explicit bounds admit the same bounded full-width table backend with size/grow/get/set/bulk/indirect and codec coverage. |
| Exception handling | ✓ | ✅ done for Core 3 on linux/amd64 explicit and signal-backed bounds plus Linux/Darwin arm64 explicit bounds: arbitrary tag directories, imported/exported tags, `throw`, `throw_ref`, `try_table`, rooted exception references, nested/cross-instance unwind, tail interaction, linking, codec metadata, and official malformed/invalid cases are gap-free. |
| Garbage collection (WasmGC) | ✓ | ✅ mandatory Core 3 suite complete on linux/amd64 explicit and signal-backed bounds. 🚧 General generated payloads now have semantic struct/array helper admission, subtype-aware access, reference constructors, GC constant expressions, pointer-free `Array<v128>` construction/access/bulk/data initialization, two-slot `v128` struct fields, codec version 2 reload, and passing MoonBit Starshine/JSON smoke coverage. Struct constructors above 64 synchronous slots use a module-derived, u16-checked off-heap extension on AMD64 and ARM64 while preserving the inline frame for ordinary calls; a 403-field/404-slot reference constructor collects correctly across codec reload. A bounded linux/amd64 local-call-graph slice publishes liveness-exact local and hidden-spill roots through per-site IDs, walks cross-function and recursive callers by return-PC maps, persists those maps in codec version 1, and supports verified Throughput/Tiny collection inside an invocation; direct tail calls discard their caller frame, while numeric host imports now preserve suspended outer roots across same-instance re-entry and mutable module-local GC globals synchronize checked collector slots before allocation. Private local `call_indirect` and `return_call_indirect` sites now publish exact roots; private local collector-reference tables are scanned directly at every collection; and generic struct/array execution is covered under linux/amd64 guard-page bounds checks. Local-only `call_ref`/`return_call_ref` now use exact callsites, and EH functions union conservative local masks with fixed GC-payload record roots. A bounded same-Runtime cross-instance slice now shares one canonical collector across structurally compatible modules, including reordered/additional local type indexes, transfers non-null GC parameters/results without copying, and walks exact foreign caller frames. Imported/exported collector-reference globals participate in that domain with mutable/immutable alias identity, direct domain-cell roots, checked-slot barriers, rollback, codec, close-order, Throughput/Tiny, and native amd64/ARM64 coverage. Multiple heterogeneous imported/exported collector-reference tables share the actual descriptor roots across aliases, table.grow, attachment rollback, codec reload, and producer-first close; exact cross-instance GC-reference calls coexist with those shared globals and tables. Generic struct/array results issue opaque `GCRef` tokens through a 64-slot inline fast path plus reusable dynamic overflow storage, retain exact ownership after close, reuse released checked slots, and require explicit release. Tokens re-enter only their exact collector domain through structural subtype checks and reusable checked roots; stale and foreign tokens reject. Arm64 explicit-bounds builds lower struct/array/i31 and dynamic cast/test helpers through the synchronous ABI; exact roots cover locals, hidden spills, direct/recursive calls, direct host re-entry, same-domain foreign calls, mutable/shared GC globals, local/shared collector tables, indirect/reference calls, proper tails, and fixed EH payload records. Abstract `ref.null eq`, `ref.null i31`, `ref.null struct`, and `ref.null array` constant expressions initialize matching globals, and runtime casts from concrete GC structs to declared non-final supertypes remain collector-subtype checks even when the same function also contains indexed function-reference tests. Function subtype tests/casts and the complete official Core 3 GC corpus execute; polymorphic or foreign reference calls remain collection-disabled where ownership is unproved. Allocating generic array helpers synchronize mutable GC globals before collection, `array.new_elem` supports non-null reference element types without default initialization, and reusable array-root scratch keeps warmed reference constructors allocation-free. AMD64 native GC ABI version 1 publishes the immutable canonical subtype-interval table, so non-final defined struct/array `ref.cast` and `ref.test` execute without synchronous helpers after checked compact-handle resolution; exact casts retain canonical ID equality, and null, Tiny, nursery, old, moved, large-object, recursive-group, and linked-domain semantics remain covered. The same ABI batches contiguous handles and optional bounded nursery chunks for statically sized final numeric/vector/packed and nullable abstract-reference array constructors through 256 object bytes; generic public calls refill only after nine slow constructors, persistent products refill immediately, and broader shapes retain exact rooted helpers. See `docs/gc.md` and `docs/native-execution-stack.md`. |
| SIMD (`v128`) | ✓ | ✅ done on supported SIMD hosts. The documented amd64 baseline is SSSE3/SSE4.1/SSE4.2 plus AVX/VEX.128 only; AVX2/FMA/VNNI remain future feature-gated fast paths. ARM64 uses native NEON lowering and has full official SIMD corpus acceptance (470 modules / 24,325 assertions, zero failures, skips, or gaps). All decoded core SIMD and deterministic relaxed SIMD `0xfd` opcodes through 275 are validated, frontend-admitted, and lowered by railshot; 20 reserved proposal-table holes are invalid-decode tests. Public `v128` representation is `[16]byte` (`wago.V128`); locals, params/results, control flow, globals, cross-instance imports, and host imports/results are supported. The pinned Release 3 relaxed-SIMD family also passes all 8 modules and 69 assertions, including official `either` result alternatives. See `docs/simd-relaxed-plan.md` and `docs/simd-performance-2026-07.md`. |
| Branch hinting (`metadata.code.branch_hint`) | ✓ | ✅ done: the current code-metadata wire format is decoded strictly (unique/pre-code section, ordered function/offset vectors, one-byte payload, and only `if`/`br_if` targets). ARM64 railshot uses `if` likelihood to weight local/global pinning and defers non-empty unlikely `br_if` reconciliation into cold target fragments. |
| Threads & atomics | ✓ | ✅ bounded experimental product on Linux/macOS amd64/arm64 with explicit bounds and one memory32. Shared memory must be an exact-maximum import; unshared memory may be local or imported. The product includes true concurrent execution across distinct shared-memory instances, serialized entry into each individual instance, the full classic scalar atomic matrix, and bounded wait/notify. Memory growth, memory64, multi-memory, signal bounds, host/function imports, mutable global imports, tables, tags, segments, WasmGC, and exceptions remain rejected. Opt in with `CoreFeatureThreads`; it is not part of `CoreFeaturesV3`. See `docs/threads-atomics-plan.md`. |
| Synchronous host-import results | ✓ | ✅ done |
| Bounded function-pipeline parallelism | ✓ | ✅ done: validation and native codegen share one deterministic serial/adaptive/forced worker policy. The default is serial; `WithFunctionWorkers(0)` and CLI `-p` select adaptive mode, while explicit maxima remain capped by `GOMAXPROCS` and local-function count. |
| Invocation cancellation | ✓ | ✅ done on amd64 and arm64: Linux/amd64 and Linux/arm64 use a thread-directed authenticated real-time signal, executable-range validation, and `ucontext` stack/PC rewriting, so generated Wasm contains no cancellation polls. Cancellation cleanup is panic-safe, signal retries are bounded while host code is parked, and closing an active instance publishes the same interruption request while invocation leases defer unmapping until unwind completes. See `docs/linux-host-interrupt.md`. |
| Linux, macOS, and Windows on amd64 and arm64 | ✓ | ✅ done — all six release targets execute the encoder, backend, runtime/API, explicit bounds, corpus, and SIMD suites in native CI. Linux/amd64, Linux/arm64, and Darwin/arm64 additionally support signal-backed guard pages and Linux uses signal-context asynchronous cancellation; the other targets use compiler-emitted cooperative safepoints. |
| Interpreter tier (native-code execution only) | ✗ | ❌ not planned |

### Iteration 72 staged M8 boundary

The exact source-lines-652–659 M8 provider/four-import consumer pair is admitted without widening public WasmGC support. The 100-byte provider and 92-byte consumer have wasm/code/codec sizes 100/253/531 and 92/0/315 bytes, own bounded 96/160-byte descriptor arenas, deduplicate four imports to one retained producer, and keep `Instance.gc` nil. Official type-subtyping accounting is 40 passed modules / 23 passed assertions / 5 exact gates / 11 blocked commands / 24 invalid / 6 executed plus 2 blocked unlinkables / zero validator gaps, unexpected failures, or hidden failures.

### Iteration 73 complete `gc/type-subtyping` boundary

The exact M9 eight-import recursive link pair, both M10/M11 expected unlinkables, and all six non-flat exported-function assertions are now admitted and executed. Official accounting is complete at 170 commands / 45 passed modules / 29 passed assertions / 24 invalid / 8 expected unlinkables / zero gates, blocked commands, validator gaps, unexpected failures, or hidden failures. This completes the official `gc/type-subtyping.wast` file without widening unrelated Core 3 public admission.

## Callback-scoped host guest storage

Synchronous host imports, including declarative Runtime plugin imports, can use
optional callback-scoped APIs for zero-copy access to indexed linear memory and
Wasm GC arrays. GC-aware plugin imports keep their generic Go `HostFunc` while
exact structural types and collector domains remain per calling module and
instance. The API reports Memory32 versus Memory64, bounds linear-memory ranges,
preserves exact structural import types, supports nested GC-array traversal, and
can allocate an exact caller-selected numeric or `v128` GC-array result.

Direct views cannot outlive the host callback. Wago serializes collector/native
mutation while a view is active and rejects Wasm re-entry during the borrow.
See [`docs/host-guest-storage.md`](docs/host-guest-storage.md).
