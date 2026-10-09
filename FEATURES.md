# Wago feature support

AMD64 uses SSE2 as its architectural baseline. Newer CPU extensions are optional
compile-time optimization tiers and are recorded in native artifact requirements
when emitted. Scalar, core SIMD, and supported relaxed SIMD have baseline
fallbacks. The `wago_amd64_sse2` build tag selects portable baseline code generation
on modern hosts. Native artifact format 3 records optional CPU requirements;
older artifacts must be rebuilt. Optional native plugins can require newer CPU
extensions even when ordinary Wasm code can use the baseline.

This is the feature-support matrix for Wago's pure-Go, no-cgo engine. Wago
supports Linux, macOS, and Windows on amd64 and arm64. For planned work, see
[ROADMAP.md](ROADMAP.md).

Experimental Linux/RV64 requires `wago_rv64_experimental`. Its bounded numeric
profile includes scalar/SWAR SIMD execution and one nullable funcref table32
combined with numeric function imports. Imported function references use retained
importer-owned thunks for Go callbacks, HostFuncRef and native targets. Reference
element expressions support bounded `ref.func` and `ref.null func` literals,
with active offsets from i32 constants or immutable imported-i32 globals.
Scalar/SWAR modules also support immutable numeric/vector import copies into
local globals and imported-i32 active-data offsets. Bounded integer constant
expressions support add/sub/mul and immutable imported integer reads (128 bytes,
eight operands), including prior immutable local globals. Immutable nullable
funcref globals support local copy chains, exports, and same-Runtime imports
from explicit Global owners with no table, an owned table, or a host-created
imported table. Nullable funcref locals and control values, including body-level
`ref.func` of export-declared local or native-imported numeric/vector functions,
also work without globals or tables. Local mutable nullable funcref globals
retain full-width own descriptors in table-free modules without reference imports,
reference-valued signatures, or mutable reference exports. Table-free declarative segments and repeated
`elem.drop` use dropped element state; unused legacy declarations require no
function descriptors or element directory. Empty and state-only modules have no
executable mapping. Bounded opaque custom metadata and empty supported sections
are accepted. Passive segments and immutable funcref `global.get` element
initializers preserve canonical descriptor ownership, including partial
initialization failures. Indexed memory32/memory64 scalar, FP, SWAR and bulk
operations support up to 100 owned/imported memories, including mixed address
widths, aliased imports and growth. Memory64 retains exact declared limits with
a finite owned reservation of at most 65,535 pages; address and growth overflow
checks preserve all 64 bits. Fallback scalar/SWAR control nesting is bounded at
128 constructs, independently of the shared scalar fast path.
Compiler diagnostics builds report finalized code/frame sizes. With `wago_profile`,
optional source maps record scalar and SWAR opcode origins in finalized code,
including cold traps, with explicit wrapper/helper gaps. Unwind maps and detailed
compiler-site attribution remain pending. Function signatures remain numeric/vector;
Go callback and HostFuncRef reexports remain rejected. Frozen
attachments retain the actual descriptor storage through producer retirement,
parked callbacks, and indirect calls. Imported host tables retain the actual
descriptor owners after writers return or close. Reference imports combined with
instance-owned imported tables, general mutable reference imports/exports,
broader reference signatures/equality, GC/EH,
threads and table64 remain unavailable.
A private qualification profile additionally exercises proper local,
native-import, indirect and typed-reference tail calls, ordinary `call_ref`, and
synchronous scalar Go/owned-host targets. Its numeric/vector guest signatures
and scalar host signatures remain bounded. Internal function references also
support `ref.as_non_null`, `br_on_null` and `br_on_non_null`, with full-width
reference predicates, typed label transfers and precise traps. Explicit
abstract and bounded indexed function-reference block results, typed selects and initialized
non-null locals use
the same full-width carriers. Their retained-carrier artifact requirement does
not allocate call transport; equivalent nullable syntax keeps ordinary
ReferenceTypes admission. Nullable indexed annotations require the typed witness
as well. Unreachable untyped selects retain their polymorphic carrier until a typed use;
known dead operands still undergo class and control-floor checks.
Source compilation and retained artifacts also carry mixed function-reference
parameters and results through indexed blocks, loops and branches. These internal
control declarations do not enter the numeric/vector callable ABI or enlarge
call transport. Non-null/indexed declarations retain the typed witness even when
unused, and currently require a defined code owner. Aggregate decoded type bytes
are bounded before validation. Private same-image direct-call lowering
preserves function-reference arguments/results, mixed compact cells and
reference root joins. Its separately enabled cooperative proper tails require
exact reference result types and reuse the checked frame protocol. This private
mode rejects reference-valued imports and indirect/reference calls or tails; legacy production
staging routes reject the mode. Private decoded whole-module compilation also
checks numeric exports and descriptor targets, bounded declarations/code and
inert metadata before validation. It admits bounded numeric resource imports and prevents
reference-bearing functions from escaping; legacy isolated instantiation rejects
the mode before allocation. Private sparse owner snapshots retain every physical
function frame while leaving internal-only descriptor and target rows empty.
They preserve function indices, authenticate the code lifetime, and exclude those
rows from dynamic target selection. An explicit backend compiler permission now
stages closed scalar/SWAR modules with internal reference-valued functions after
rechecking confinement and every emitted function mode. The image records a
distinct selected ABI witness; bare nullable references do not require the typed
call/control witnesses. Permission alone does not change numeric, imported or
control-only modules. Private Wago Runtime permission also admits source and
trusted artifacts for these closed modules. The retained witness does not grant
destination permission: default instances reject before executable publication.
Null-only modules keep an anchor without indexed arenas; indexed owners preserve
empty internal rows and numeric ordinals. Foreign numeric calls retain closed
producers and size their internal transport from actual signatures. These closed
modules also compose with bounded owned numeric globals, unshared memory32/64,
multiple memories and active/passive data. References remain internal across
growth, bulk memory operations and local proper tails. Owned numeric memories
and globals may be exported with exact names and aliases; export counts and name
bytes share the module budget. Bounded ordinary numeric function imports also
compose with private references in locals and lower operands. Native imports and
scalar Go/owned callbacks preserve dirty-i32 normalization and scalar/SWAR payloads;
local proper tails remain available. Sparse imported owners retain numeric target
identities and empty internal rows through foreign entry and callback interruption.
Go/owned signatures remain scalar with at most 64 arguments/results. Numeric
global imports and unshared memory32/64 imports also compose with this mode.
Interleaved import kinds retain separate bounded namespaces and exact dotted
names; imported global placeholders carry no initializer payload. Memory/global
aliases share storage, including refreshed non-primary memory aliases after a
parked callback. Resource-only providers retain physical storage without becoming
callable generation owners. Numeric direct imported proper tails also compose
with private reference frames. A private reference parameter, local or lower
operand is discarded at the tail exit; only the exact numeric target arguments
and results enter external transport. Ordinary callers continue after the tail,
including foreign entry with aliased memory growth and callback interruption.
Finalized source staging checks the exact ordered plan directory and emitted
frame; retained trusted artifacts recheck direct target, numeric result and
frame/home bounds. Private source rows remain noncallable. Native vector targets
use paired transport; Go callbacks remain scalar. Reference-valued
imports/exports remain unsupported in this mode. Initializer
programs and data share the aggregate admission budget, and exact memory exports
prevent legacy name fallback. Foreign numeric callers retain all producer memories
through final attachment retirement. Separate memory-only and global-only borrowers
also preserve the entire producer lifetime, including after all function callers close.
Standalone `ref.null` constants with bounded numeric
function-type indices carry the typed witness, including unreachable uses,
without allocating declared-call transport. Invocation
scopes retain imported
descriptor owners, including modules with no defined functions, and certified
snapshots authenticate them across parked callbacks, reentry, table replacement
and cold cleanup. Source and compiler-free
artifact tests cover these paths; public TailCall and TypedFunctionReferences
feature admission remains disabled.
This is not a supported
release target; native cache and performance qualification remain outstanding.
See [the runtime contract](ARCHITECTURE.md).

## Start here

Use this file to check whether a WebAssembly feature can run in your selected
build. A completed feature is not necessarily enabled on every operating system,
architecture, or bounds-checking mode. `SupportedFeatures()` is the authority for
the executable build and host. The **Planned** column records product intent; it
does not indicate that a feature is available.

Status: ✅ done · 🚧 partial · ⬜ planned · ❌ not planned.

The amd64 backend needs a modern CPU with SSSE3, SSE4.1, SSE4.2, and AVX/VEX.128
XMM encodings. It does not use AVX2, FMA, or VNNI unless a future feature gate
enables them.

### Terms used here

- **Backend:** the compiler and runtime support for one CPU architecture.
- **Explicit bounds:** generated code checks each linear-memory access.
- **Signal-backed bounds:** eligible accesses use protected memory pages; some
  accesses still use explicit checks.
- **Core 3:** the mandatory feature scope of WebAssembly Core 3.0.

## Core 3 availability

`CoreFeaturesV2` is the static WebAssembly 2.0 release group. `CoreFeaturesV3`
describes the mandatory WebAssembly Core 3.0 scope.

- Complete Core 3 backends enable the entire Release 3 feature set by default,
  including WasmGC and exception handling. Use
  `WithCoreFeatures(CoreFeaturesV2)` when Release 2 compatibility is required.
- Incomplete backends retain the Release 2 feature set plus completed extended
  constants rather than silently admitting a partial Core 3 product.
- Linux/amd64 explicit and signal builds, and Linux/Darwin arm64 explicit and
  signal builds, admit every Core 3 family. ARM64 signal mode uses guard faults
  for eligible memory-0 memory32 access. Indexed memories and memory64 keep
  explicit checks. Native `spec3-signals` cells are mandatory on both operating
  systems.
- The pinned 258-file suite passes on linux/amd64 and under Linux/arm64 QEMU:
  **2,226 modules and 58,038 assertions passed**, with zero failures, skips, or
  gap categories. `tests/conformance/baselines/spec-v3-baseline.json` is the machine-readable source
  of truth. Native Linux/Darwin arm64 runs are mandatory CI gates.
- Linux/amd64 signal-backed builds also admit every Core 3 family and pass the
  same suite. Indexed nonzero memories keep explicit directory bounds checks,
  and memory64 keeps full-u64 explicit checks in signal-backed builds.
- `memory64` and typed function references, including `call_ref`, are available
  only on those admitted Core 3 products. Explicit `CoreFeaturesV1` and
  `CoreFeaturesV2` selections do not change.
- The suite result is not a general WasmGC claim. The current
  shape-independent struct/array helper admission also compiles, links, and
  starts the 3,225,249-byte MoonBit Starshine CLI smoke payload (SHA-256
  `3a92309ca48f80594c88ea6c3508982d6fc34953c018ce31786382e08a18d046`).

### Detailed Core 3 implementation status
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
only exact-capacity mappings. Codec version 3 reloads and strictly validates the
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
The feature matrix below is the current support reference. `SupportedFeatures()`
is the runtime authority for a specific build and host.

## Feature matrix

### WebAssembly 1.0 (MVP)

The core specification is **complete**. The pinned pre-reference-types test suite
passes in full: 57 of 57 applicable files and 0 failing assertions. See
[SPECTEST.md](SPECTEST.md).

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

### Beyond WebAssembly 1.0

This table covers later WebAssembly proposals and Wago platform capabilities.

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
| Garbage collection (WasmGC) | ✓ | ✅ The mandatory Core 3 suite is complete on supported linux/amd64 explicit and signal-backed products. General generated-payload support continues to harden. Use `SupportedFeatures()` to confirm admission on a selected build and host. |
| SIMD (`v128`) | ✓ | ✅ done on supported SIMD hosts. The AMD64 baseline is SSSE3/SSE4.1/SSE4.2 plus AVX/VEX.128; AVX2/FMA/VNNI need future feature gates. ARM64 uses native NEON. Core and relaxed SIMD are validated, admitted, and lowered by Railshot. Public `v128` values use `[16]byte` (`wago.V128`). |
| Branch hinting (`metadata.code.branch_hint`) | ✓ | ✅ done: the current code-metadata wire format is decoded strictly (unique/pre-code section, ordered function/offset vectors, one-byte payload, and only `if`/`br_if` targets). ARM64 railshot uses `if` likelihood to weight local/global pinning and defers non-empty unlikely `br_if` reconciliation into cold target fragments. |
| Threads & atomics | ✓ | ✅ bounded experimental product on Linux/macOS amd64/arm64 with explicit bounds and one memory32. Shared memory must be an exact-maximum import. It supports concurrent distinct instances, classic scalar atomics, and bounded wait/notify. Opt in with `CoreFeatureThreads`; it is not part of `CoreFeaturesV3`. |
| Synchronous host-import results | ✓ | ✅ done |
| Bounded function-pipeline parallelism | ✓ | ✅ done: validation and native codegen share one deterministic serial/adaptive/forced worker policy. The default is serial; `WithFunctionWorkers(0)` and CLI `-p` select adaptive mode, while explicit maxima remain capped by `GOMAXPROCS` and local-function count. |
| Invocation cancellation | ✓ | ✅ done on amd64 and arm64. Linux uses a thread-directed signal; other supported standard-Go targets use compiler-emitted safepoints. Release TinyGo builds use the safe `tasks` scheduler and reject cancelable native calls before entry. |
| Linux, macOS, and Windows on amd64 and arm64 | ✓ | ✅ done — all six release targets execute the encoder, backend, runtime/API, explicit bounds, corpus, and SIMD suites in native CI. Linux/amd64, Linux/arm64, and Darwin/arm64 additionally support signal-backed guard pages and Linux uses signal-context asynchronous cancellation; the other targets use compiler-emitted cooperative safepoints. |
| Interpreter tier (native-code execution only) | ✗ | ❌ not planned |

### Legacy exception handling

Legacy EH encodings (`try`, `catch`, `catch_all`, `delegate`, and `rethrow`) are
not supported. They fail decoding with `legacy exception handling is not
supported; recompile with try_table (exnref)`. Configure the producer to emit
Core 3 exception handling (`try_table`, `throw`, and `throw_ref`) when rebuilding.

## Historical completion boundaries

### Iteration 72 staged M8 boundary

The exact source-lines-652–659 M8 provider/four-import consumer pair is admitted without widening public WasmGC support. The 100-byte provider and 92-byte consumer have wasm/code/codec sizes 100/253/531 and 92/0/315 bytes, own bounded 96/160-byte descriptor arenas, deduplicate four imports to one retained producer, and keep `Instance.gc` nil. Official type-subtyping accounting is 40 passed modules / 23 passed assertions / 5 exact gates / 11 blocked commands / 24 invalid / 6 executed plus 2 blocked unlinkables / zero validator gaps, unexpected failures, or hidden failures.

### Iteration 73 complete `gc/type-subtyping` boundary

The exact M9 eight-import recursive link pair, both M10/M11 expected unlinkables, and all six non-flat exported-function assertions are now admitted and executed. Official accounting is complete at 170 commands / 45 passed modules / 29 passed assertions / 24 invalid / 8 expected unlinkables / zero gates, blocked commands, validator gaps, unexpected failures, or hidden failures. This completes the official `gc/type-subtyping.wast` file without widening unrelated Core 3 public admission.

## Callback-scoped host storage

Synchronous host imports, including declarative Runtime plugin imports, can use
optional callback-scoped APIs. They provide zero-copy access to indexed linear
memory and Wasm GC arrays. GC-aware plugin imports keep their generic Go
`HostFunc`; exact structural types and collector domains remain specific to the
calling module and instance. The API identifies Memory32 or Memory64, checks
linear-memory ranges, preserves exact structural import types, supports nested
GC-array traversal, and can allocate an exact caller-selected numeric or `v128`
GC-array result.

Direct views are valid only during the host callback. Wago serializes
collector/native mutation while a view is active and rejects Wasm re-entry during
the borrow.
