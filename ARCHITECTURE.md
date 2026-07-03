# Architecture

`wago` is a pure-Go (no-cgo) WebAssembly engine. It decodes, validates, and
compiles wasm modules to x86-64 machine code with a single-pass backend, then
executes that code directly from Go — no C toolchain, no cgo, no FFI. The
host-boundary shape and runtime ABI are derived from [WARP](https://github.com/wasm-ecosystem/wasm-compiler),
a C++ single-pass wasm engine (vendored at `warp/` as a reference submodule).

Target platform today: **linux/amd64**.

**CPU baseline: modern x86-64 with SSE4.1 and AVX/VEX.128.** The backend emits
some instructions beyond original x86-64 without a CPUID gate or fallback:
`POPCNT`, `LZCNT`/`TZCNT` (clz/ctz/popcnt), `ROUNDSS`/`ROUNDSD` (f32/f64
`ceil`/`floor`/`trunc`/`nearest`), and 128-bit VEX-encoded XMM operations used by
scalar float and SIMD lowering. This is an intentional "modern amd64" assumption,
not "any amd64"; running generated code on an older CPU would fault with an
illegal instruction.

The baseline does **not** include AVX2, FMA, VNNI, or wider YMM/ZMM vector forms.
Those may only be emitted after an explicit feature gate or a documented baseline
change. SIMD lowering should therefore prefer SSE4.1/SSSE3-compatible semantics
encoded with VEX.128 where possible, and use portable multi-instruction sequences
for relaxed SIMD dot products and madd/nmadd until newer-ISA gates exist.

Current SIMD support is partial and explicit-gated: `v128` participates in the
railshot operand stack, params, locals, spills, wrapper ABI results, and linear
memory load/store, with lowering for `v128.const`, splats, lane extract/replace,
basic bitwise ops, `v128.any_true`, all_true/bitmask for i8x16/i16x8/i32x4/i64x2,
integer neg for i8/i16/i32/i64 lanes, abs for i8/i16/i32 lanes, i8x16 popcnt,
signed/unsigned i8 narrow from i16 lanes, signed/unsigned i16 narrow from i32 lanes, add/sub for i8/i16/i32/i64 lanes, saturating add/sub for i8/i16 lanes,
i16 q15mulr_sat_s, mul for i16/i32 lanes, eq/ne for those lanes, signed and unsigned ordered comparisons for
i8/i16/i32 lanes, signed/unsigned min/max for i8/i16/i32 lanes, unsigned rounding
averages for i8/i16 lanes, and f32x4/f64x2 packed abs/neg/sqrt/add/sub/mul/div
plus comparisons.
`i64x2.gt_s` remains unsupported until a
baseline-safe sequence or a documented SSE4.2 gate exists.
Unsupported `0xfd` opcodes remain frontend errors instead of falling through to
backend codegen.

---

## 1. The pipeline

```
 wasm bytes
     │
     ▼
 ┌─────────┐   ┌──────────┐   ┌─────────────────────┐   ┌──────────┐
 │ decode  │──▶│ validate │──▶│ compile (Valent-    │──▶│ Compiled │
 │         │   │          │   │ Block x86-64 codegen)│   │ metadata │
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
`Instantiate` turns a `*Compiled` into a runnable `*Instance`. `Invoke` calls an
exported function.

---

## 2. Repository layout

```
src/wago/                         public API implementation (package wago)
wago.go                           generated root facade (re-exports src/wago)
internal/genfacade/               generator for wago.go (+ up-to-date test)
cli/wago/                         CLI: run; compile/profile/validate are stubs
src/core/compiler/wasm/           decoder + validator (front end)
src/core/compiler/backend/railshot/  single-pass x86-64 codegen (Valent-Block)
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

The net effect: straight-line code emits essentially no per-operation stack
traffic. `valent_test.go`'s `TestRegisterResident` disassembles a straight-line
function and asserts the body contains **zero** push/pop beyond the prologue's
`push rbp` — proof the operand stack lives in registers.

The production compiler path is still single-pass: there is no separate
register-allocation pass on the hot load path; Valent-Block is the compiler's
middle and back end in one pass.

---

## 5. Scalar SSA IR tier (`src/core/compiler/ir`)

The `ir` package adds a compact, block-parameter SSA form for validated wasm
functions. It is intentionally isolated today: no runtime path imports it yet.
Its job is to give the future optimizing/JIT tier a strict trust boundary while
sharing the same decoded wasm metadata as the single-pass backend.

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

- `Code`, `Entry` (per-function offsets), `Funcs` (signatures)
- `Imports` / `NumImports`, `Exports`
- `GlobalImports`, `Globals`, `GlobalExports` (numeric global metadata)
- `TableSize`, `FuncTypeID`, `Elems` (active element segments)
- `Data` (active data segments)

`Compiled` serializes to a compact versioned **`.wago` blob**
(`MarshalBinary`/`UnmarshalBinary`, magic `WAGO` + version byte). `Load` accepts
either a precompiled blob (fast reload, no recompile) or raw wasm (compiled on
load); `IsCompiled` distinguishes them. `validate()` hardens every blob against
malformed metadata before any memory is mapped. The size-focused CLI `run` path
accepts only raw wasm today, so the serialization path stays available to the Go
API without being pulled into the CLI binary.

---

## 7. Runtime (`src/core/runtime`)

### JobMemory: `[ basedata | linear memory ]`

A single contiguous, mmap'd region. Native code receives a pointer to the
**linear-memory base**; the runtime's bookkeeping lives at **negative offsets**
below that base (`[linMem - off]`), a layout verified field-for-field against
WARP's `basedataoffsets.hpp`:

| offset | field                                            |
|-------:|--------------------------------------------------|
| 8      | actual linear-memory byte size (bounds checks)   |
| 16     | trap handler pointer                             |
| 32     | runtime pointer                                  |
| 40     | host-import context pointer (the host-call log)  |
| 72     | stack fence (overflow check)                     |
| 80     | indirect-call table descriptor (wago extension)  |
| 88     | per-instance globals pointer table (wago ext.)   |
| 96     | `basedataSize` — keeps linMem 16-byte aligned    |

Mapping off-heap is essential: the address must be **stable** for the lifetime
of native execution (the Go GC must never move it).

### Off-heap allocation

`Arena` hands out stable, off-heap, 8-byte-aligned buffers for everything native
code touches: the globals pointer table and cells, the table descriptor, the
host-call log, and the per-call argument/result/trap slots. Native code never
receives a Go heap pointer.

### Mapping code (W^X)

`MapCode` mmaps the compiled bytes `PROT_READ|PROT_WRITE`, copies the code in,
then `mprotect`s to `PROT_READ|PROT_EXEC` — write-xor-execute. `Instance.Close`
unmaps it.

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
loading the table base from `[linMem - 88]`, indexing the slot, and
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

When a module has a table or active element segments, `Instantiate` builds a
**table descriptor** (`[len][entry...]`, each entry `{codePtr, sigID}`) in the
arena and points `[linMem - 80]` at it. `call_indirect` bounds-checks the index,
verifies the runtime signature id against the call site's expected id, and jumps.

---

## 11. Host imports

Host imports use a **deferred host-call log**, not synchronous re-entry. A host
import is a `func(arg int32)` (void, one i32). During execution, native code
appends `(importIndex, arg)` records to the off-heap log whose base is published
in basedata at offset 40. After `Engine.Call` returns, `Invoke` reads the log and
replays each recorded call against the registered Go `HostFunc`s.

Consequence: host functions run **after** the wasm call returns, on the goroutine
stack in normal Go context — so no external party observes or mutates instance
state mid-execution. (A synchronous, value-returning re-entry path —
`CallWithHost`, signaling via the trap slot and resuming native code — exists in
the runtime as an experimental "V2" spike but is not yet wired into the public
API.)

---

## 12. Memory model

Linear memory is the mmap-backed tail of JobMemory, exposed zero-copy via
`Instance.Memory().Bytes()` — writes are visible in both directions without
copying. Loads/stores are bounds-checked against the size cached in basedata.
Active data segments are copied in at instantiate time with bounds validation.
`memory.grow` is not yet supported.

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

- **linux/amd64 only**, JIT-only (no interpreter tier).
- The priority is completing **WebAssembly 1.0 (MVP)**. Most of the MVP is
  implemented; `memory.size`/`memory.grow`, `start` functions, the float rounding
  ops (`ceil`/`floor`/`trunc`/`nearest`/`copysign`), trapping float→int
  truncation, and i64 sub-width loads are the notable remaining MVP gaps. Several
  post-MVP proposals are partially in (multi-value, reference-type `select t`,
  saturating truncation, `memory.copy`/`fill`); others are planned.

This section only sketches scope — **[FEATURES.md](FEATURES.md) is the source of
truth** for per-feature status, with [ROADMAP.md](ROADMAP.md) for the plan and
[SPECTEST.md](SPECTEST.md) for the live spec-conformance board.
```
