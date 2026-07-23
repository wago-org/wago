# Architecture

`wago` is a pure-Go (no-cgo) WebAssembly engine. It decodes, validates, and
compiles wasm modules to native machine code with a single-pass backend, then
executes that code directly from Go — no C toolchain, no cgo, no FFI. The
host-boundary shape and runtime ABI are derived from [WARP](https://github.com/wasm-ecosystem/wasm-compiler),
a C++ single-pass wasm engine (vendored at `warp/` as a reference submodule).

<!-- architecture:targets linux/amd64 linux/arm64 darwin/arm64 -->
The mature production target is **linux/amd64**. Native Railshot compilation and
runtime support also exist for **linux/arm64** and **darwin/arm64**, with the
remaining platform qualification tracked in [FEATURES.md](FEATURES.md) and the
ARM64 documents under `docs/`.

<!-- artifact:codec-version 23 -->

**CPU baseline: modern x86-64 with SSSE3/SSE4.1 plus AVX/VEX.128 XMM encodings.** The backend emits
some instructions beyond original x86-64 without a CPUID gate or fallback:
`POPCNT`, `LZCNT`/`TZCNT` (clz/ctz/popcnt), `ROUNDSS`/`ROUNDSD` (scalar
f32/f64 `ceil`/`floor`/`trunc`/`nearest`), `VROUNDPS`/`VROUNDPD` (packed
f32x4/f64x2 rounding), and 128-bit VEX-encoded XMM operations used by scalar
float and SIMD lowering, including SSSE3-family SIMD operations such as `pshufb`, packed abs, horizontal add, and `pmulhrsw`-style helpers. This is an intentional "modern amd64" assumption,
not "any amd64"; running generated code on an older CPU would fault with an
illegal instruction.

The baseline does **not** include AVX2, FMA, VNNI, or wider YMM/ZMM vector forms.
Those may only be emitted after an explicit feature gate or a documented baseline
change. SIMD lowering should therefore prefer SSE4.1/SSSE3-compatible semantics
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
packed float/int conversions and f32/f64 lane-width demote/promote, plus comparisons. Core packed-float min/max use the shared scalar Wasm-correct lane sequence for NaN and signed-zero behavior; core packed rounding uses SSE4.1 VROUNDPS/VROUNDPD with suppress-precision immediates for ceil/floor/trunc/nearest-even while preserving signed-zero and NaN result semantics covered by tests; core packed float/int conversions currently use scalarized lane extraction/conversion to preserve saturating and unsigned edge cases; f32x4.demote_f64x2_zero and f64x2.promote_low_f32x4 currently scalarize through lane extract, scalar CVTSD2SS/CVTSS2SD, and lane insert while preserving demote high-lane zeroing and promote high-lane ignore semantics; core pmin/pmax use swapped native packed min/max so the first operand wins equal and NaN-second lanes. Relaxed truncations intentionally use the conservative saturating result policy (NaN and negative unsigned lanes become zero; overflows clamp; f64x2-zero forms clear high lanes). Relaxed packed-float min/max intentionally use native MINPS/MAXPS/MINPD/MAXPD, returning the second source for NaN and equal signed-zero lanes under the current lowering order; relaxed packed-float madd/nmadd intentionally use separate packed multiply plus add/subtract instead of FMA. Relaxed dot products currently use deterministic signed i8 products, signed saturating i16 pair sums, scalar SSE4.1 lane extraction/insertion, and GPR arithmetic instead of AVX2/VNNI. `i64x2.shr_s` and signed ordered `i64x2` comparisons are lowered with baseline-safe scalarized
qword-lane sequences that mask shift counts modulo 64 and avoid SSE4.2 `pcmpgtq`.
Unsupported `0xfd` opcodes remain frontend errors instead of falling through to
backend codegen.

---

## 1. The pipeline

```
 wasm bytes
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

`Compile` (in `src/wago/api.go`) runs decode → validate → backend codegen and
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
tests/testdata/                   small checked-in wasm fixtures
spectest_exec_test.go             wasm 1.0 conformance harness (+ SPECTEST.md)
bench/                            benchmarks vs wazero (separate Go module)
warp/                             upstream C++ reference (submodule)
```

The root module is dependency-free (stdlib only); `bench/` is a separate module
so the public package stays clean.

---

## 3. Front end — decode & validate (`src/core/compiler/wasm`)

- `decode.go` parses the binary into a `Module` (types, funcs, tables, memory,
  globals, imports/exports, element/data segments, code bodies).
- `validate.go` / `validate_ops.go` enforce the wasm type rules: a structured
  operand-stack/control-frame validator that type-checks every opcode and
  rejects malformed or ill-typed modules before any code is emitted.
- Module declarations and constant expressions, including element initializers,
  validate serially. With function workers enabled, independent bodies use
  worker-local stacks/readers. Shared module/element metadata is immutable, the
  type cache is frozen, and `table.init` reads only the validated element type;
  errors are selected by lowest function index so diagnostics match serial
  validation.
- Unsupported value types and opcodes are rejected explicitly rather than
  silently accepted — correctness and explicit failure come first.

Validation is intentionally stricter than the narrow const-expression decoder
the compiler uses for global/segment initializers: the validator guarantees
shape, the backend then trusts it.

---

## 4. Back end — Valent-Block codegen (`src/core/compiler/backend/railshot`)

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

`Compiled` holds the emitted code plus everything `Instantiate` needs without
re-decoding:

- `Code`, wrapper/internal `Entry` offsets, and `Funcs` signatures
- `Imports` / `NumImports`, import signatures, dynamic-dispatch shape, and `Exports`
- `GlobalImports`, `Globals`, `GlobalExports` (numeric global metadata)
- `TableSize`, `FuncTypeID`, `Elems` (active element segments)
- `Data` (active data segments)

`Compiled` serializes to a compact versioned **`.wago` blob**
(`MarshalBinary`/`UnmarshalBinary`, magic `WAGO` + version byte). `Load` accepts
either a precompiled blob (fast reload, no recompile) or raw wasm (compiled on
load); `IsCompiled` distinguishes them. `validate()` hardens every blob against
malformed metadata before any memory is mapped. Codec v23 persists the
binding-independent imported-call shape, so modules with function imports can be
serialized before host or instance targets are known; live addresses and store
identity are installed only during instantiation.

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

Memory-size/growth fields belong to the memory backing. The pointer subset from
40 and 80–136 is modeled as a 64-byte `InstanceContext`, captured in the
instance arena and rebound before every public native entry. Shared-memory users
serialize entry while rebinding, so one linear-memory mapping can safely serve
instances with independent globals, tables, host state, segments, and import
bindings. Direct and indirect cross-instance calls copy the target context into
its home basedata and restore the caller context on normal return.

Mapping off-heap is essential: every native-visible address must be **stable**
for the lifetime of execution (the Go GC must never move it).

### Off-heap allocation

`Arena` hands out stable, off-heap, 8-byte-aligned buffers for everything native
code touches: the globals pointer table and cells, the table descriptor, the
host-call log, and the per-call argument/result/trap slots. Native code never
receives a Go heap pointer.

### Mapping code (W^X)

`MapCode` mmaps the compiled bytes `PROT_READ|PROT_WRITE`, copies the code in,
then `mprotect`s to `PROT_READ|PROT_EXEC` — write-xor-execute. A refcounted cache
shares one executable mapping across instances of a `Compiled`; the mapping is
unmapped after the compiled owner is closed and the last instance releases it.

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

Every public, prepared, and managed native entry acquires an invocation lease.
`Instance.Close` first marks the instance logically closed, publishes
`TrapInterrupted` to an active caller, and prevents new entries. Executable
mappings, engine stacks, arenas, and owned memory are released only after both
invocation leases and retained reference/import roots reach zero. This ordering
allows a host-parked activation to unwind without a use-after-unmap while still
making close interruption bounded at generated safepoints.

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
- Coherence invariant (see `docs/runtime-abi.md`): the cell is the sole
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
`Instance.Memory().Bytes()` — writes are visible in both directions without
copying. Because a raw `[]byte` cannot own or release the mmap lifetime,
`Memory.Bytes`, access through a returned slice, and `Instance.Close`/`Memory.Close`
must be externally synchronized; the view is invalid once its owner closes.
Explicit mode checks the current size cached in basedata; supported platforms
can instead use guard-page reservations. `memory.grow` raises the logical size
within a stable pre-reserved mapping, preserving the native base.
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

---

## 14. Relationship to WARP

wago is an independent Go reimplementation that deliberately stays
**ABI-compatible** with WARP's runtime conventions:

- the Valent-Block compilation approach,
- the `[basedata | linear memory]` JobMemory layout and negative-offset fields
  (verified against `warp/src/core/common/basedataoffsets.hpp`),
- the `WasmWrapper(serArgs, linMem, trap, results)` boundary shape.

`warp/` is vendored as a submodule purely as a reference oracle; it is C++ and is
not built or needed to build/test the Go module.

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

- Wago is JIT-only; there is no interpreter tier.
- **linux/amd64** is the mature production target. **linux/arm64** and
  **darwin/arm64** have native compiler/runtime support but remain under platform
  qualification; Windows is not supported.
- WebAssembly 1.0 and the documented WebAssembly 2.0 feature set are complete on
  the supported amd64 baseline. Tail calls, threads/atomics, multi-memory,
  exception handling, and the Wasm GC proposal remain outside the current
  supported feature set unless [FEATURES.md](FEATURES.md) says otherwise.
- The off-path `src/core/compiler/ir` package is a research/debug oracle, not an
  execution tier. Railshot is the only production backend.

This section only sketches scope — **[FEATURES.md](FEATURES.md) is the source of
truth** for per-feature status, with [ROADMAP.md](ROADMAP.md) for the plan and
[SPECTEST.md](SPECTEST.md) for the live spec-conformance board.
```
