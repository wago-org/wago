# Wago architecture

Wago is a pure-Go, no-cgo WebAssembly engine. It decodes, validates, and
compiles Wasm modules to native machine code with a single-pass backend. It then
executes that code directly from Go. It needs no C toolchain, cgo, or FFI. The
host-boundary shape and runtime ABI are derived from
[WARP](https://github.com/wago-org/warp), a C++ single-pass wasm engine maintained
as a separate repository.

## Start here

Wago processes a module in five steps: **decode**, **validate**, **compile**,
**instantiate**, and **call**. Start with [the pipeline](#1-the-pipeline) for the
full path from Wasm bytes to a function call.

| If you want to understand... | Start with... |
|---|---|
| where source code lives | [Repository layout](#2-repository-layout) |
| how Wago rejects invalid modules | [Front end](#3-front-end--decode-and-validate-srccorecompilerwasm) |
| how native code is made | [Back end](#4-back-end--valent-block-code-generation-srccorecompilerbackendrailshot) |
| how an instance runs native code | [Runtime](#7-runtime-srccoreruntime) |
| public Go APIs | [Public API](#13-public-api--the-generated-facade) |
| supported features | [FEATURES.md](FEATURES.md) |

The rest of this introduction records platform and implementation rules. The
numbered sections explain the system in the order that a module moves through it.

### Terms used here

- **Module:** a WebAssembly binary that Wago decodes and compiles.
- **Compiled:** Wago's compiled module: native code plus the metadata needed to
  create an instance.
- **Instance:** a runnable copy of a Compiled module with its own runtime state.
- **Trap:** a Wasm runtime error, such as an invalid memory access.

## Platform and artifact rules

<!-- architecture:targets linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64 -->
Railshot compilation and the native runtime support Linux, macOS, and Windows
on amd64 and arm64. Linux and Darwin/arm64 additionally support signal-backed
guard-page bounds checks; all six targets support explicit bounds checks and
cooperative cancellation safepoints.

<!-- artifact:codec-version 2 -->

Compiled artifact version 2 is a strict ordered section stream. It has a fixed
header and section count, followed by length-delimited native-code and metadata
sections. Wago rejects unknown, duplicate, reordered, truncated, over-limit, and
non-canonical section encodings. `Compiled.WriteTo` streams code without making a
second full image. `Compiled.ReadFromWithLimits` reads code directly into an RW
mapping, applies separate code and metadata bounds, validates all metadata, and
seals that mapping RX on first use. Non-global imports keep their legacy flat
lookup key and a validated module-name boundary. Source compilation and artifact
loading therefore expose the same module/name pair without decoding source again.
Plugin bindings must match that pair before they satisfy an import. This prevents
dotted flat-key collisions from crossing module authority boundaries. Artifact
decoding also caps the expanded function-import directory at 64 MiB, so compact
empty names cannot produce an unbounded slice allocation. Version 2 replaced the
initial version 1 format when generated `memory.grow` code and the native instance
context gained a runtime memory-page quota. Wago rejects every artifact version
other than 2, including version 1. There is no compatibility decoder or
dual-format ambiguity.

### CPU and SIMD baseline

**CPU baseline: modern x86-64 with SSSE3/SSE4.1/SSE4.2 plus AVX/VEX.128 XMM encodings.** The backend emits
some instructions beyond original x86-64 without a CPUID gate or fallback:
`POPCNT`, `LZCNT`/`TZCNT` (clz/ctz/popcnt), `ROUNDSS`/`ROUNDSD` (scalar
f32/f64 `ceil`/`floor`/`trunc`/`nearest`), `VROUNDPS`/`VROUNDPD` (packed
f32x4/f64x2 rounding), and 128-bit VEX-encoded XMM operations used by scalar
float and SIMD lowering, including SSSE3-family operations such as `pshufb`, packed abs, horizontal add, and `pmulhrsw`-style helpers plus SSE4.2 `pcmpgtq` for signed i64-lane operations. This is an intentional "modern amd64" assumption,
not "any amd64"; running generated code on an older CPU would fault with an
illegal instruction.

The baseline does **not** include AVX2, FMA, VNNI, or wider YMM/ZMM vector forms.
Those may only be emitted after an explicit feature gate or a documented baseline
change. SIMD lowering should therefore prefer SSSE3/SSE4.1/SSE4.2-compatible semantics
encoded with VEX.128 where possible, and use portable multi-instruction sequences
for relaxed SIMD dot products and madd/nmadd until newer-ISA gates exist. Core
`i32x4.dot_i16x8_s` uses VEX.128 `VPMADDWD`, which is within the documented
baseline and does not require AVX2/VNNI.

SIMD support is complete for the documented linux/amd64 baseline and remains
explicitly feature-gated: `v128` participates in the
railshot operand stack, params, locals, spills, control-flow frame slots/branches,
wrapper ABI calls/results, and linear memory load/store, extending-load/load-splat/load-zero ops, and lane memory load/store, with lowering for `v128.const`, i8x16.swizzle/shuffle, deterministic i8x16.relaxed_swizzle/relaxed_laneselect/relaxed truncations/relaxed packed-float minmax/relaxed packed-float madd/nmadd/i16x8.relaxed_q15mulr_s/relaxed dot products, splats, lane extract/replace,
basic bitwise ops, `v128.any_true`, all_true/bitmask for i8x16/i16x8/i32x4/i64x2,
integer neg for i8/i16/i32/i64 lanes, abs for i8/i16/i32/i64 lanes, i8x16 popcnt,
signed/unsigned i8 narrow from i16 lanes, signed/unsigned i16 narrow from i32 lanes, signed/unsigned i8-to-i16, i16-to-i32, and i32-to-i64 widening extends, pairwise extadd from i8-to-i16 and i16-to-i32 lanes, signed/unsigned i8-to-i16, i16-to-i32, and i32-to-i64 extmul, `i32x4.dot_i16x8_s`, add/sub for i8/i16/i32/i64 lanes, saturating add/sub for i8/i16 lanes,
i16 q15mulr_sat_s, i8/i16/i32/i64 lane shifts, mul for i16/i32/i64 lanes, eq/ne for those lanes, signed ordered comparisons for i64 lanes, signed and unsigned ordered comparisons for
i8/i16/i32 lanes, signed/unsigned min/max for i8/i16/i32 lanes, unsigned rounding
averages for i8/i16 lanes, and f32x4/f64x2 packed abs/neg/ceil/floor/trunc/nearest/sqrt/add/sub/mul/div/min/max/pmin/pmax,
packed float/int conversions and f32/f64 lane-width demote/promote, plus comparisons. Core packed-float min/max use a branchless packed Wasm-correct sequence for NaN and signed-zero behavior; core packed rounding uses SSE4.1 VROUNDPS/VROUNDPD with suppress-precision immediates for ceil/floor/trunc/nearest-even while preserving signed-zero and NaN result semantics covered by tests. Packed float/int conversions use branchless packed sequences, including exact unsigned conversions and f64x2-to-i32 saturation; f32x4.demote_f64x2_zero and f64x2.promote_low_f32x4 use VCVTPD2PS/VCVTPS2PD. Core pmin/pmax use swapped native packed min/max so the first operand wins equal and NaN-second lanes. Relaxed truncations intentionally use the conservative saturating result policy (NaN and negative unsigned lanes become zero; overflows clamp; f64x2-zero forms clear high lanes). Relaxed packed-float min/max intentionally use native MINPS/MAXPS/MINPD/MAXPD, returning the second source for NaN and equal signed-zero lanes under the current lowering order; relaxed packed-float madd/nmadd intentionally use separate packed multiply plus add/subtract instead of FMA. Relaxed dot products currently use deterministic signed i8 products, signed saturating i16 pair sums, scalar SSE4.1 lane extraction/insertion, and GPR arithmetic instead of AVX2/VNNI. `i64x2.shr_s` uses a baseline-safe scalarized qword-lane sequence that masks shift counts modulo 64; signed ordered `i64x2` comparisons and abs use SSE4.2 `pcmpgtq`.
Unsupported `0xfd` opcodes remain front-end errors instead of falling through to
backend code generation.

### WasmGC boundary

WasmGC uses stable compact references and bounded collector heaps. Generated
modules collect only where exact native roots are published; unsupported root
shapes remain fail-closed rather than using approximate scanning. A bounded linux/amd64 slice
admits in-invocation collection for local call graphs with numeric host imports,
module-local GC globals, private immutable local-function tables, one private
collector-reference table, direct recursion, and no start/tag/EH state. A
structured-CFG dataflow pass computes exact local liveness per allocation and
callsite; amd64 adds hidden operand spill offsets, compact safepoint IDs, frame
size, adapter return, and recursive call return-PC maps. The synchronous helper
control frame publishes parked RSP, and Go exposes validated off-heap slots from
each walked frame directly as mutable collector roots. Throughput/Tiny stress
collection and the root walker remain zero-allocation after warm-up. Codec version 2
persists and strictly revalidates the map, including dynamic-import stack
adjustments. Direct tail calls discard their caller frame. Numeric host callbacks
use a bounded suspended-activation stack plus separate nested foreign stacks, and
mutable module-local GC globals synchronize checked collector slots before every
allocating helper. Private local `call_indirect` and `return_call_indirect` sites publish exact
caller roots, while collector-reference table entries are scanned directly from
the mutable off-heap descriptor. Generic struct/array execution is also admitted
with guard-page bounds checks on linux/amd64. Local-only `call_ref` and
`return_call_ref` are admitted when no function descriptor can enter through a
parameter, result, global, import, or mutable/exported table. EH functions use
conservative all-local masks plus fixed GC-payload record offsets; record
initialization and clearing remain part of the native EH lowering. A bounded
same-Runtime cross-instance slice gives structurally compatible modules one
canonical collector domain, translates immutable module-local type indexes at helper
and snapshot boundaries, transfers compact GC references without copying, and switches root-map
ownership across foreign return PCs. Mutable and immutable imported/exported GC
globals use direct domain-wide alias-cell roots plus checked barrier slots. Multiple
heterogeneous imported/exported collector-reference tables use the same domain-wide
descriptor-root scan across growth, attachment rollback, and producer-first close.
Exact cross-instance GC-reference calls coexist with those shared persistent roots and
retain the same foreign-frame ownership across compiled-code codec reload. Generic
struct/array results may leave the domain as up to 64 opaque retained `GCRef` tokens
per producer. Partial multi-result issuance rolls back atomically; exact store/producer
ownership survives producer close, and non-null tokens may re-enter the same collector
domain through structural subtype checks plus reusable checked argument roots; stale
and foreign tokens reject. Explicit cross-Runtime transfer uses
`target.CloneGCRefFrom(source, ref)`: a bounded stable-ID graph clone maps
structurally equivalent target types, preserves cycles/internal sharing, assigns
new target identity, and rejects non-null opaque store-owned payloads. Direct
cross-Runtime compact-handle sharing remains impossible. Codec version 2 persists helper
admission, the required native-GC ABI version, and the 16-byte `v128` storage
contract, but never compact handles. AMD64 final scalar struct/array accesses and initialized final-struct
allocation use collector native ABI version 1. Artifact loading validates the Go/native
layout and codec version 2 records the required ABI; instantiation validates the immutable
instance view, local canonical-type map, collector identity, collector version, and
handle stride before publishing basedata offset 280. Native accesses then trust those
immutable facts while reloading and validating mutable handle ranges/liveness, heap
space and backing extents, object extents and canonical types, array bounds,
remembered membership, any existing object-card interval, and the transactional
native-allocation epoch. A module-owned 229-byte noncollecting resolver leaf replaces
repeated inline resolver bodies at modules with at least two candidate scalar sites;
one-site modules retain inline code. A single-function module with reuse enabled is
lowered inline first and adds the island only when at least two actual resolutions
remain, so one-object repeated runs avoid the fixed leaf while distinct objects retain
sharing. Bounded straight-line reuse may retain one derived object address only while
its unchanged compact local remains the root and no call,
allocation, collection, host transition, control/EH edge, local mutation, or other
invalidating opcode can intervene. Final abstract
reference stores may bypass helpers only when no metadata growth is required;
vector, non-final, bulk, cardless/unremembered, and Tiny barrier paths remain
helper-bound. Arm64 builds lower struct/array/i31 and dynamic cast/test helpers
through the synchronous ABI. The
bounded arm64 native-root product publishes liveness-exact locals and hidden
spills from parked SP, then follows saved-LR callsites through direct/recursive
calls, suspended direct-host activations, and same-domain foreign frames. Mutable
local/shared GC globals, local/shared collector tables, indirect/reference calls,
discarded-frame direct/indirect/reference tails, and fixed EH payload records are
exact. ARM64 `try_table`, `throw`, `throw_ref`, function subtype identity, indexed
multi-memory, memory64, and table64 are part of both ARM64 Core 3 bounds modes.
Signal mode guard-checks eligible memory-0 memory32 accesses while indexed memories
and memory64 retain explicit checks. Polymorphic local `call_indirect` and same-domain foreign `call_ref`
publish each possible native return PC; cross-instance wrapper paths carry their
64-byte save-area adjustment into frame walking. Descriptor-resolved `return_call_ref` uses bounded discarded-frame transfer for
local internal, host-wrapper, and retained same-Runtime cross-instance targets,
including mutable/imported funcref-table loads. GC-bearing typed tails compare the
numeric collector-domain identities stored in trailing native instance-context
metadata before the caller is discarded; ordinary host and direct foreign-domain
GC transfers remain fail-closed when exact ownership is unproved.

---

## 1. The pipeline

```
 Wasm bytes
     │
     ▼
 ┌─────────┐   ┌──────────┐   ┌─────────────────────┐   ┌──────────┐
 │ decode  │──▶│ validate │──▶│ compile (Valent-     │──▶│ Compiled │
 │         │   │          │   │ Block native codegen)│   │ metadata │
 └─────────┘   └──────────┘   └─────────────────────┘   └────┬─────┘
 src/core/compiler/wasm        src/core/compiler/backend/railshot│
                                                              │  (optional: MarshalBinary → .wago blob)
                                                              ▼
                                                       ┌─────────────┐
                                                       │ instantiate │  map code, build JobMemory,
                                                       │             │  globals, table, data segments
                                                       └──────┬──────┘
                                                              ▼
                                                       ┌─────────────┐
                                                       │   execute   │  Engine.Call on a foreign stack
                                                       └─────────────┘
                                              src/wago + src/core/runtime
```

In order:

1. **Decode** reads the Wasm binary format.
2. **Validate** checks the module against WebAssembly type and structure rules.
3. **Compile** creates native machine code and metadata.
4. **Instantiate** creates memory, globals, tables, and import bindings.
5. **Invoke** calls one exported function.

`Compile` (in `src/wago/api.go`) runs decode → validate → backend code generation and
returns a `*Compiled`: machine code plus the instantiate-time metadata
(signatures, imports/exports, globals, element/data segments, table size).
Validation and codegen can use the same bounded per-module function-worker policy;
module-wide analysis remains serial, and final errors/code layout remain ordered by
function index. `Instantiate` turns a `*Compiled` into a runnable `*Instance`.
`Invoke` calls an exported function.

---

## 2. Repository layout

```
src/wago/                         public API implementation (package wago)
  instantiate.go                  staged instance-construction transaction
  instance.go                     live instance state and native-visible handles
  instance_lifecycle.go           close/physical-release ownership transfer
  reference_lifetime.go           close/quiescence/root-transfer convergence
  import_attachments.go           imported owner attachment and root retention
wago.go                           generated root facade (re-exports src/wago)
internal/genfacade/               generator for wago.go (+ up-to-date test)
cli/wago/                         CLI entry point and command implementation
src/core/compiler/wasm/           decoder + validator (front end)
src/core/compiler/backend/railshot/  direct native codegen (Valent-Block)
  amd64/                            x86-64 selection, registers, ABI, encoding
  arm64/                            AArch64 selection, registers, ABI, encoding
  shared/                           architecture-neutral policy and metadata
src/core/runtime/                 mmap, foreign stack, JobMemory, traps
src/core/runtime/abi/             layout constants shared by codegen + runtime
tests/spec/                       WebAssembly spec testsuite (submodule, MVP-pinned)
tests/spec-v2/                    WebAssembly 2.0 specification (submodule)
tests/fixtures/                   small Wasm, benchmark, and parser fixtures
tests/regressions/                pinned binary regression corpus
tests/spectest/                   shared specification-test helpers
tests/wasmtest/                   programmatic Wasm fixture builders
tests/scripts/                    shell integration tests
spectest_exec_test.go             wasm 1.0 conformance harness (+ SPECTEST.md)
bench/                            benchmarks vs wazero (separate Go module)
```

The root module is dependency-free (stdlib only); `bench/` is a separate module
so the public package stays clean.

---

## 3. Front end — decode and validate (`src/core/compiler/wasm`)

- `decode.go` parses the binary into a `Module` (types, funcs, tables, memory,
  globals, imports/exports, element/data segments, code bodies).
- `validate.go` / `validate_ops.go` enforce the wasm type rules: a structured
  operand-stack/control-frame validator that type-checks every opcode and
  rejects malformed or ill-typed modules before it emits code.
- Module declarations and constant expressions, including element initializers,
  validate serially. With function workers enabled, independent bodies use
  worker-local stacks/readers. Shared module/element metadata is immutable, the
  type cache is frozen, and `table.init` reads only the validated element type;
  errors are selected by lowest function index so diagnostics match serial
  validation.
- Unsupported value types and opcodes are rejected explicitly rather than
  silently accepted. Correctness and clear failure come first.

Validation is intentionally stricter than the narrow const-expression decoder
the compiler uses for global/segment initializers: the validator guarantees
shape, the backend then trusts it.

---

## 4. Back end — Valent-Block code generation (`src/core/compiler/backend/railshot`)

The backend is a **single forward pass** that fuses code generation and register
allocation. It uses the *Valent-Block* technique from WARP: instead of emitting
a push/pop for every wasm operand, it keeps a **compile-time symbolic operand
stack** whose entries (`ventry`) are deferred values:

| kind     | meaning                                            |
|----------|----------------------------------------------------|
| `vConst` | an immediate constant, not yet materialized        |
| `vLocal` | a lazy reference to a local's frame slot           |
| `vReg`   | a value already resident in a scratch register     |
| `vSpill` | a value flushed to its canonical frame slot        |

Pure, stack-neutral instructions are recorded symbolically and stay
register-resident. Only when a value is actually **consumed**, or a
side-effecting instruction appears (`local.set`, `global.set`, `br_if`, a call,
a control-flow join), are the deferred operands *condensed* — materialized into
registers just in time. At control-flow joins the machine state is flushed to
deterministic frame slots so every incoming edge agrees on register/stack state.

The decoded module and complete function bodies remain available throughout
compilation. Railshot reuses module-scoped scratch arenas between functions on
the serial path. With function workers enabled, module-wide decisions finish
first, each worker owns private scratch and an append-only code arena, and the
results are joined and relocated in original function order. This reduces
one-module latency without making code layout or serialized output depend on
scheduling.

The net effect: straight-line code emits essentially no per-operation stack
traffic. `valent_test.go`'s `TestRegisterResident` disassembles a straight-line
function and asserts the body contains **zero** push/pop beyond the prologue's
`push rbp` — proof the operand stack lives in registers.

The production compiler path is still single-pass: there is no separate
register-allocation pass on the hot load path; Valent-Block is the compiler's
middle and back end in one pass.

---

## 5. Scalar SSA IR tier (`src/core/compiler/ir`)

The `ir` package contains a compact block-parameter SSA form for focused
verification and differential-oracle experiments. It is intentionally isolated:
production compiler, runtime, and public packages may not import it, and there is
no planned IR execution tier. Boundary tests enforce that quarantine.

Scope is deliberately scalar-only for now (`i32`, `i64`, `f32`, `f64`). Reference,
GC, vector, multi-memory, and multi-table behavior stays at the wasm validation
or unsupported-feature boundary until the IR has explicit opcodes and codegen
contracts for it.

The IR models locals as explicit stateful operations, keeps declared locals in
run-length encoded form, stores CFG edges and value lists in compact shared
pools, and carries effect flags for scheduling barriers. `VerifyModule` is the
intended gate for IR produced from whole modules; it checks shape, dominance,
definition coverage, canonical aux metadata, effect flags, and module-indexed
references before any optimizer or IR backend consumes a function.

---

## 6. The compiled artifact & serialization

`Compiled` privately owns the emitted code plus everything `Instantiate` needs
without re-decoding. `CodeSize` reports its length and `WriteCodeTo` provides a
read-only diagnostic stream; callers never receive a mutable alias:

- native code, wrapper/internal `Entry` offsets, and `Funcs` signatures
- `Imports` / `NumImports`, import signatures, dynamic-dispatch shape, and `Exports`
- `GlobalImports`, `Globals`, `GlobalExports` (numeric global metadata)
- `TableSize`, `FuncTypeID`, `Elems` (active element segments)
- `Data` (active data segments)

`Compiled` serializes to a compact versioned **`.wago` blob** through either the
slice APIs (`MarshalBinary`/`UnmarshalBinary`) or bounded streaming APIs
(`WriteTo`/`ReadFromWithLimits`). `Load` accepts untrusted raw Wasm and compiles
it; `LoadTrustedArtifact` performs the fast native-code reload only for
authenticated or locally produced `.wago` bytes. `IsCompiled` distinguishes the
formats, but callers must choose the trust boundary explicitly. `validate()`
hardens artifact metadata before mapping, but cannot sandbox hostile native code.
The codec persists the
binding-independent imported-call shape, so modules with function imports can be
serialized before host or instance targets are known; live addresses and store
identity are installed only during instantiation. See the codec-version comment
at the top of this file for the current wire version.

---

## 7. Runtime (`src/core/runtime`)

### JobMemory: `[ basedata | linear memory ]`

A single contiguous, mmap'd region. Native code receives a pointer to the
**linear-memory base**; the runtime's bookkeeping lives at **negative offsets**
below that base (`[linMem - off]`), a layout verified field-for-field against
WARP's `basedataoffsets.hpp`:

| offset | field |
|-------:|-------|
| 8 | actual linear-memory byte size (bounds checks) |
| 12 | declared/engine maximum pages |
| 16 / 24 | trap handler and stack-reentry pointers |
| 40 | host-call log or synchronous control-frame pointer |
| 48 | native scratch spill word |
| 72 | stack fence |
| 80 | table-0 descriptor |
| 88 | canonical funcref-descriptor array |
| 96 | indexed table-directory pointer |
| 104 | active trap-cell pointer |
| 112 | globals pointer table |
| 120 / 128 | passive element/data descriptor arrays |
| 136 | imported-function dispatch table |
| 280 | versioned per-instance native GC metadata view |

Memory-size/growth fields belong to the memory backing. Nine pointer fields from
40 and 80–136 form the 72-byte Go `InstanceContext`. Each instance owns a 112-byte
native context buffer: those nine pointers, an immutable numeric GC-domain identity,
three process-serialized descriptor-tail scratch words, and the per-instance native
GC-view pointer at byte 104. Binding copies the pointer prefix and GC-view pointer into
basedata, so shared-memory context switches select the target module's canonical type
map without aliasing EH tags or the wrapper argument bank. Shared-memory users serialize entry while rebinding, so one
linear-memory mapping can safely serve instances with independent globals, tables,
host state, segments, and import bindings. Direct and indirect cross-instance calls
copy the target pointer context into its home basedata and restore the exact caller
context on normal return.

Every native-visible address must be stable for the duration in which native code
can consume it. Most runtime state is off-heap. Native GC ABI version 1 is the narrow
exception: standard Go and the release TinyGo conservative collector are non-moving;
typed Collector/Instance fields retain the view, heap/handle/card backing, fixed
32-handle allocation state, epoch, nursery bump, and semantic counter. Production
code validates immutable view shape at artifact/collector/instantiation boundaries;
`wagodebug` additionally revalidates it at each Go-to-native entry. Generated code
reloads relocatable slice pointers after helper allocation, collection, or
card-backing refresh rather than caching them across safepoints. The only raw-address
reuse is a compiler-owned one-entry certificate within a mechanically safepoint-free
straight-line region; every may-collect, host-reentry, control/EH, tail, local-write,
or unknown edge invalidates it before lowering. Native allocation reserves identities
only; collection cancels unused reservations before tracing.

### Off-heap allocation

`Arena` hands out stable, off-heap, 8-byte-aligned buffers for everything native
code touches: the globals pointer table and cells, the table descriptor, the
host-call log, and the per-call argument/result/trap slots. Outside the explicitly
versioned collector metadata exception above, native code never receives a Go heap
pointer.

### Mapping code (W^X)

Serial compilation and streamed artifact loading write directly into an owned
`PROT_READ|PROT_WRITE` mapping, then first instantiation changes that same
mapping to `PROT_READ|PROT_EXEC`—write-xor-execute—without copying. The
latency-gated parallel compiler retains its faster heap join and maps it on first
use. A refcounted cache shares one executable mapping across instances of a
`Compiled`; `Close` rejects future instances, while existing instances retain
the mapping until the last one closes. Decode replacement is rejected while any
instance is live, so it cannot orphan that ownership chain.

### Execution: the foreign stack & trampoline

`Engine` owns a dedicated **4 MiB off-heap execution stack**. `Engine.Call`
enters native code through `enterNative` (`trampoline_amd64.s`), which:

1. switches `RSP` to the foreign stack top,
2. calls the wasm wrapper following the System V mapping
   `serArgs→RDI, linMem→RSI, trap→RDX, results→RCX`,
3. restores the Go context on return.

Running on a separate stack keeps native wasm code off the goroutine stack,
which Go may grow/move (`morestack`) — that would be catastrophic mid-execution.
After the call returns, a non-zero trap slot becomes a Go `*TrapError`.

---

## 8. The wrapper ABI

Every export is called through one fixed shape:

```
WasmWrapper(serArgs, linMem, trap, results)
```

Arguments and results are 8-byte slots in off-heap buffers; `i32`/`f32` use the
low 32 bits. `linMem` is the JobMemory linear-memory base (so the wrapper reaches
basedata at negative offsets). Traps are reported by writing a trap code into the
`trap` slot. This uniform shape is what makes the host↔wasm boundary cheap and
allocation-free on the hot path.

---

## 9. Globals

Each instance owns a **pointer table** (one 8-byte slot per global, in wasm
global-index order; imported globals first). Codegen reads/writes a global by
loading the table base from `[linMem - 112]`, indexing the slot, and
dereferencing the 8-byte cell.

- Module-local globals point at instance-local cells.
- **Imported mutable globals are shared by object identity**: a host-owned
  `*Global` cell is pointed at directly, so writes from wasm, `Instance.SetGlobal`,
  `g.Set`, and other instances importing the same `*Global` all observe the same
  storage. Duplicate imports of one key alias the same cell.
- Coherence invariant: the cell is the sole
  host-/cross-instance-visible storage. The current backend reads/writes it on
  every `global.get`/`global.set`; a future register-caching pass must spill at
  function return and around calls.

Element- and data-segment offsets may reference an imported immutable i32 global,
resolved at instantiate time after imports are bound.

---

## 10. Tables & `call_indirect`

For each funcref table, `Instantiate` builds a descriptor with an 8-byte
`{len,max}` header and 32-byte entries containing code pointer, canonical
signature id, home linear-memory pointer, and canonical funcref handle.
`call_indirect` bounds-checks the index, verifies the signature, resolves the
canonical descriptor's owning instance context, and then takes either the local
wrapper path or the cross-instance context-switch path. Externref tables use
8-byte store handles rather than function descriptors.

---

## 11. Host imports

Imported calls are compiled once as loads from the per-instance dispatch table.
At instantiation, each cell receives a wrapper entry, home linear-memory base,
target instance context, and caller context. Cross-instance cells point directly
at the producer's wrapper entry; host cells point at small instance-owned thunks.

Legacy void `HostFunc` signatures that fit the batched protocol may append calls
to the off-heap log at basedata offset 40 and replay them after native return.
Returning, vector, owned, reflected, or caller-sensitive host functions use the
synchronous `CallWithHost` control frame: native execution yields to Go at the
actual call site, Go writes results, and the same foreign-stack invocation
resumes. One instance selects exactly one host protocol because both use the
same context slot.

---

## 12. Memory model

Linear memory is the mmap-backed tail of JobMemory, exposed zero-copy via
`Instance.Memory().UnsafeBytes()` — writes are visible in both directions without
copying. Explicit mode checks the current size cached in basedata; supported
platforms can instead use guard-page reservations. `memory.grow` raises the
logical size within a stable pre-reserved mapping, preserving the native base.
Active and passive data operations retain strict bounds and dropped-state checks.

---

## 13. Public API & the generated facade

The public package lives at `src/wago/` (package `wago`). To keep the import path
clean (`github.com/wago-org/wago`) while the code lives under `src/`, the root
`wago.go` is a **generated facade** that re-exports every public symbol:

- types as aliases (`type X = impl.X` — methods/fields carry over),
- functions as forwarding wrappers (`func X(...) { return impl.X(...) }` — real
  functions, proper godoc, not reassignable).

`internal/genfacade` regenerates `wago.go` from `src/wago`'s exported symbols
(`go generate ./...`); a test (`TestFacadeUpToDate`) and a CI step fail if it
drifts. It rejects exported package-level vars (a var alias would copy, not
alias) and signatures needing external-package types.

Core intentionally exposes no instance-state snapshot or reset API. Reuse,
checkpointing, and reset policy belong in capability-gated plugins rather than
the runtime facade.

---

## 14. Relationship to WARP

wago is an independent Go reimplementation that deliberately stays
**ABI-compatible** with WARP's runtime conventions:

- the Valent-Block compilation approach,
- the `[basedata | linear memory]` JobMemory layout and negative-offset fields
  (verified against WARP's `src/core/common/basedataoffsets.hpp`),
- the `WasmWrapper(serArgs, linMem, trap, results)` boundary shape.

WARP remains an external reference oracle; it is not vendored, built, or needed
to build and test the Go module.

---

## 15. Conformance & testing

- **Execution conformance** (`spectest_exec_test.go`, `TestSpecExec`): runs the
  official WebAssembly testsuite (`tests/spec`, pinned to a pre-reference-types
  MVP commit) through compile→instantiate→invoke, scoring `assert_return` /
  `assert_trap` per file. Each file runs in an **isolated subprocess** so a JIT
  fault is recorded as `CRASH` rather than aborting the run. Results are written
  to `SPECTEST.md`. Gated on the submodule + `wast2json`; skips otherwise.
- **Validation conformance** (`src/core/compiler/wasm` spec harness): checks
  decode/validate against `assert_invalid` / `assert_malformed`.
- **Unit/codegen tests**: amd64 codegen is asserted by disassembling emitted
  bytes (`objdump`) and checking instruction shape; runtime has stress tests for
  stack, memory, host-call, and trap behavior.
- **Benchmarks** (`bench/`, separate module): wago vs wazero v1.9.

---

## 16. Current scope & limitations

- Wago has no interpreter tier: supported modules execute as native code. Wasm
  can be compiled in-process, serialized as a native `.wago` artifact, or
  embedded in a standalone executable that compiles it at startup.
- Linux, macOS, and Windows on amd64 and arm64 execute the native JIT and are
  required CI and release targets. Signal-backed guard pages remain specific to
  Linux/amd64, Linux/arm64, and Darwin/arm64; other targets use explicit bounds.
- WebAssembly 1.0, the documented WebAssembly 2.0 feature set, and the
  opt-in WebAssembly Core 3.0 feature families (tail calls, typed references,
  WasmGC, exception handling, multi-memory, memory64, table64, extended
  constants, and relaxed SIMD) are complete on linux/amd64, linux/arm64, and
  darwin/arm64. Threads & atomics are available as the bounded experimental
  explicit-bounds product documented in [FEATURES.md](FEATURES.md), which is the
  source of truth for per-feature status.
- The off-path `src/core/compiler/ir` package is a research/debug oracle, not an
  execution tier. Railshot is the only production backend.

This section only sketches scope — **[FEATURES.md](FEATURES.md) is the source of
truth** for per-feature status, with [ROADMAP.md](ROADMAP.md) for the plan and
[SPECTEST.md](SPECTEST.md) for the live spec-conformance board.
