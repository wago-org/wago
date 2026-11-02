Census completed against **Wago `90f2b57da`**, using `git show`. Read the full brief and Slipstream design at `bd640ddf0`, including safety contract v2. No files changed, children started, builds/tests/benchmarks run, branches created, or PRs opened. The excluded GC campaign and its machinery were untouched.

**Main finding:** Wago already has reflection-free registration, resolved typed bindings, scalar portals, borrowed-slot views, bounded bridges, and certified native arithmetic leaves. The remaining opportunity is narrower than “replace a reflective generic host dispatcher.”

All Wago anchors below refer to the pinned revision, not the current checkout.

### Registration → resolved callable

| Stage | Precise anchor | Existing behavior |
|---|---|---|
| Embedder registration | `src/wago/imports.go:77–98`, `136–159` | `Imports.HostFunc` classifies the callback, records inferred types, and stores it under a collision-free module/name key. First snapshot seals registration and rejects inferred/declaration mismatches. |
| Plugin registration | `src/wago/access.go:27–70` | `HostImportRegistrar.HostFunc` requires authority and exact granted module scope; uses the same classifier. |
| Explicit declarations | `src/wago/registry.go:169–245` | `registeredImport` retains callback and declared/inferred signatures. Builders copy parameter/result types and reject mutation after sealing. |
| Plugin validation/gating | `src/wago/plugin_plan.go:585–604`, `778`; `src/wago/hostcall.go:1651` | Checks declarations and wraps callbacks with the plugin call gate. Gate admission/revocation is part of semantics. |
| Runtime resolution | `src/wago/runtime.go:1058–1108` | Resolves overrides/provider bindings; missing imports fail. Plugin declaration signatures are compared with requested signatures. |
| Instantiation | `src/wago/instantiate.go:895–909` | Allocates and initializes the synchronous control frame and builds resolved hosts. |
| Callable construction | `src/wago/hostcall.go:1862–2028`, `2113–2169` | `bindSyncHostImport` validates supported callback signatures; `buildSyncHosts` creates immutable, import-indexed `syncHostBinding` entries with signature pointers, exact descriptors, and scalar/reference class. `InstanceExport` bindings are native cross-instance calls and are skipped. |
| Native entries | `src/wago/instantiate.go:2523–2607`, `2610` onward | Selects native cross-instance entries, owned-host thunks, or cached shared host thunks. Numeric `HostFuncRef` bindings already have a shortcut. |

**Reflection finding:** `inspectHostFuncSignature` is a Go type switch (`hostcall.go:1583–1648`), not reflection. Dispatch likewise uses typed fields/type switches. Reflection observed here is installation-time identity/type inspection for native-leaf admission and alias detection (`host_execution.go:1083–1117`; `src/core/runtime/native_leaf_native.go:45–80`). There is no `reflect.Call` cost to remove from these paths.

### Existing specialized signature inventory

`hostcall.go:1586–1645`, `1862–2110`, and `host_execution.go:1000–1056` establish:

| Supported ordinary callback shape | Existing representation |
|---|---|
| `() → ()` | Dedicated typed field and scalar kind |
| `i32 → ()`, `(i32,i32) → ()` | Dedicated typed fields |
| `i32 → i32`, `(i32,i32) → i32` | Dedicated typed fields; scalar portals; potentially certified native leaves |
| `i32 → (i32,i32)`, `(i32,i32) → (i32,i32)` | Dedicated typed fields; expanded scalar portal packs two 32-bit results into one `uint64`, then restores two zero-extended slots |
| `T → T`, `(T,T) → T`, for `T=i64,f32,f64` | Resolved scalar kinds and expanded portals; callback invocation uses concrete type assertions |
| Unary identity-shaped `FuncRef`, `ExternRef`, `ExnRef`, `GCRef`, `I31Ref` callbacks | Supported typed forms, with reference classification/translation |
| `HostCallFunc`, `CallerHostCallFunc` | General borrowed-slot interface, with frozen signature/exact descriptor |
| `I32HostEvent` | Separate deferred-event contract; not interchangeable with synchronous calls |

Arbitrary mixed or additional ordinary Go signatures are **not accepted automatically**. General shapes use `HostCallFunc`/`CallerHostCallFunc`. Public v128 callbacks are explicitly rejected at `hostcall.go:1869–1870`.

### Code-evident crossing costs

These are source operations, not timings or claims about final machine instruction counts.

| Shape/route | Work still present |
|---|---|
| Generic inline-frame synchronous call, `P` argument slots and `R` result slots | Native parks and returns to Go; loop decodes active frame/import/shape; copies **P uint64 argument slots**, zeros **R result slots**, dispatches, and copies **R results back**; checks interruption and resumes native execution. `src/core/runtime/engine_host.go:304–378`. Scratch buffers are reused; nested use has separate arrays. |
| Scalar portal | Loads up to two arguments and stores the scalar result without generic argument/result slice-copy loops. Still crosses into Go and parks/resumes execution ownership. `engine_host.go:349–360`; `host_execution.go:754–859`. |
| Expanded scalar portal | Same transport reduction for zero/two results; two i32 results are packed/unpacked, preserving zero extension. `engine_host.go:454–467`; `host_execution.go:1037–1042`. |
| Borrowed control-frame view | Constructs slices over parked argument/result areas and clears results; avoids the additional generic scratch copy on accepted routes. Declined inline-frame views use copied fallback. `src/core/runtime/engine_hostview.go:73–97`, `110–129`. Extended frames already receive direct views in the generic loop. |
| `HostCallFunc` | Constructs compact borrowed slot descriptors plus signature/exact pointers; no per-value `interface{}` boxing or `reflect.Call`. `hostcall.go:2320–2324`; `hostcall_compact_slots.go:17–28`. Oversized Go slice capacities have a separately allocated descriptor fallback; normal native frame capacities do not reach that threshold. |
| Caller-aware callback | Adds callback scope/generation, root invocation identity, reservation and exact-type authority; expires scope on return. `hostcall.go:2326–2339`. |
| Reference callback | Translates and validates argument tokens/types, maintains temporary roots/tokens, calls callback, translates/validates results, releases temporaries. `hostcall.go:2342–2384`, `2480` onward. |
| General boundary | Publishes GC activation/arguments, conditionally suspends GC leases, releases native execution lease, handles callback/reentry and panic cleanup, reacquires lease, and conditionally restores context. `host_execution.go:366–568`. |
| Foreign-instance boundary | Additionally saves/restores the producer control prefix: 56 bytes on the shown amd64 branch, architecture-sized prefix on arm64. `host_execution.go:316–329`. |
| Certified native scalar leaf | Copies only admitted register arithmetic into executable wrappers; direct instance-owned import cells avoid the Go boundary. Arguments/results still use wrapper homes in the shown amd64 direct entry. `native_leaf_amd64.go:190–245`, `308–323`; `native_leaf_native.go:105–121`. |

Existing optimizations also include:

- Single-import scalar and general-slot portals: `host_execution.go:861–993`, with eligibility at `1135–1186`.
- Bounded multiple-import views for **2–64 bindings**: `src/wago/host_multi.go:13–30`, `98–124`.
- Bounded live Go-frame bridges replacing park/return/resume under their existing proof: `src/core/runtime/inline_host.go:15–20`, `29–79`.
- Native leaf aliases when every import has the same certified callback PC and shape: `host_execution.go:1083–1117`.
- Explicit scheduling distinction: native leaves cease to be Go safepoints, so the bounded host-segment route is disabled for them (`src/wago/host_segments.go:53–56`).

### Classification and candidate scope

“Specialized transport” below does **not** imply SPECIALIZED under the reference taxonomy.

| Surface/opportunity | Adapted class and verdict | Candidate scope / admission requirement |
|---|---|---|
| Already certified i32 register-arithmetic callbacks | **DIRECT, existing** | Current machine-code admission proves a narrow body; numeric signature alone does not. Preserve runtime/toolchain ABI checks and scheduling restrictions. |
| Multiple **distinct** certified native leaves in one instance | **DIRECT, conditional new opportunity** | Current `nativeScalarLeafAliases` rejects distinct PCs/shapes. Scope: `host_execution.go:187–230`, `1083–1117`; runtime native-leaf registration/cache and instance-owned dispatch cells. Require per-binding retained leaf identity instead of the current one-leaf-per-control-frame assumption. Shared thunks/reference descriptors retain fallback. |
| Additional independently provable leaf shapes | **DIRECT, conditional research candidate** | Extend native-leaf decoder and wrappers one shape/backend at a time. Existing ordinary API forms such as `()→()` avoid a public API addition; i64/FP need separate width, register and FP-state proofs. No admission established by this census. |
| Resolved i64/FP typed dispatch | **BOUNDARY, transport-only candidate** | `callTypedScalar` still asserts `b.fn` types (`host_execution.go:1043–1054`); `typedI64` already exists and is populated (`hostcall.go:1475`, `1939`). A tight candidate could use the resolved i64 field consistently. It removes a source-level assertion, not parking, scheduling or callback effects. No speed claim. |
| Scalar `HostCallFunc` outside existing single/bounded-view admissions | **BOUNDARY, conditional transport candidate** | Generalize validated borrowed views only where non-reference shape, frame lifetime and reentry isolation are proved. Scope: `buildSyncHosts`, engine view loops, portal selection. Existing views and 2–64 bounded multi-import support must remain distinguished from new coverage. |
| Trusted checked-memory built-in | **SPECIALIZED only after per-binding proof** | Registration currently supplies callback shape/gates, not a general trusted semantic-lowering certificate. New internal metadata must be attached to the actual resolved implementation and exact signature. No particular memory operation is admitted by this registration-only census. |
| Arbitrary Go callbacks, including scalar-only functions | **BOUNDARY; DIRECT NOT VIABLE from shape alone** | Closures may allocate, block, panic or access captured public state. Retain Go-stack execution and lease protocol unless independently certified. |
| Caller/reference/plugin-gated callbacks | **BOUNDARY by default** | Cannot erase callback authority, token translation, GC roots, owner lifetime or revocation gates merely because a signature is small. |
| Deferred events substituted for synchronous imports | **NOT VIABLE** | Changes completion, ordering and error/return semantics. Existing event API remains a different contract. |
| New ordinary mixed/large signatures or public effect annotations | **Design finding only** | Changes the public host API acceptance/contract; not an invisible specialization candidate. |
| v128 public host callbacks | **NOT VIABLE within existing API contract** | Explicit rejection must not silently become support. |

### Adapter finding

`src/core/compiler/backend/railshot/shared/host_adapters.go:7–69` concerns **guest-defined local functions reachable through the wrapper ABI**, not Go host callback registration.

It marks exports, start, element payloads, global/table initializers, and retains synthetic function zero when no other entry exists. Direct same-module calls already use `InternalEntry` and can omit wrappers. amd64/arm64 consumers are at `amd64/compile.go:1907` and `arm64/compile.go:1743`.

Therefore **“elide direct-only guest wrappers” is already implemented**. Deleting the remaining addressable adapters would violate reference/start/export entry coverage.

### Required admission tests

For any later candidate:

- Force generic and candidate routes independently and assert route execution; compare raw slots, results, traps/exits, memory and callback effects.
- Reject wrong signatures, unsupported bodies, closure/state loads, calls, unsafe instructions and unsupported compiler ABIs.
- Cover distinct callbacks, aliases, mixed eligible/ineligible imports, cross-instance calls, reexports, tables, references and tails.
- Poison i32/f32 upper bits; test negative i32s, two-result order and FP bit patterns where relevant.
- Exercise panic/HostExit, cancellation, nested reentry, context mutation while parked, resource publication, plugin revocation and close/lifetime.
- For views, cover frame boundaries, fallback before mutation, result clearing and nested scratch isolation.
- Run ordinary, `wagodebug`, and `wago_regalloccheck` qualification later; none was run here.

Existing test anchors include `hostcall_test.go:273,325,349,372,405`, `host_call_indexed_test.go:8,98,265`, `host_adapters_test.go`, and runtime `native_leaf_{amd64,arm64,riscv64,native}_test.go`.

### Read-only sibling cross-check

**Slipstream `bd640ddf0`:** `src/jit.goose:176–210` performs built-in identity/signature resolution; `src/host.goose:10–32` still dispatches operation ranges and marshals through 16-element arrays. This confirms that its per-operation generic transport differs materially from Wago’s resolved callback lanes. Its safety contract’s identity, fallback, ownership and addressable-entry requirements transfer; its array counts, 16-byte carriers and result-span repair do not automatically describe Wago.

**Starshine `b28144a7c50f1dc360825f7f68220022101b9921`:** `src/internal/xfs/fs_wasm.mbt:85–126` shows byte-at-a-time imports with opaque external handles. `node/internal/runtime.js:171–198` performs handle lookup, append/growth, reader snapshot allocation, cursor mutation and deletion; import objects are assembled at `318–368`. This supports effect/lifetime-based classification and rejects inference from small signatures or import names. It supplies no evidence that Wago can inline those callbacks or that they share Wago’s ABI.

**Recommended first candidate:** multiple distinct **already provable** native i32 leaves, with per-import ownership and generic fallback preserved. Treat expanded leaf shapes and generalized views as separate candidates. No implementation admission or performance result is claimed.
