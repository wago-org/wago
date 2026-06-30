# Optimizations & remaining ports from WARP

What is still worth porting from the reference C++ WARP engine (`warp/`) into wago,
from a 3-axis survey (codegen optimizations, wasm features, runtime/infra).

**Framing:** WARP is essentially **MVP-scoped**. Most "advanced" wasm — SIMD, threads/atomics,
exception handling, tail calls, full reference types, the bulk-memory remainder, multi-memory,
memory64 — is **not in WARP either** (it throws `FeatureNotSupportedException`), so those are
greenfield with no reference to lean on. wago is in fact already **ahead** of WARP on
`trunc_sat`, `select t`, and imported/mutable/typed globals.

Legend: effort S/M/L · value ⬜ low · 🟦 medium · 🟩 high · ⭐ very high · status ✅ done · 🚧 partial · ⬜ todo

## A. Codegen optimizations

These are the perf levers. Several are gated on one architectural change: WARP defers every
arithmetic op into a **deferred-action tree** and condenses a whole sub-expression at once
(side-effects → scratch-pressure → target-hints), whereas wago evaluates **eagerly** (one op at a
time). The deferred tree is the keystone that unlocks target hints, side-effect scheduling, and
scratch-pressure condensation.

| Optimization | Effort | Value | Status | WARP ref |
|---|:--:|:--:|:--:|---|
| Locals pinned in registers | L | 🟩 | ✅ | `ModuleInfo`, `Common.cpp` |
| Globals pinned in registers (lazy write-back) | M | 🟩 | ✅ | `ModuleInfo` |
| Constant folding/propagation, compare/branch fusion | M | 🟦 | ✅ | `fold.go`/`fuse.go` |
| **RegisterCopyResolver** (parallel-move / cycle solver) | S–M | 🟦 | ⬜ | `common/RegisterCopyResolver.hpp` |
| **Register-based internal call ABI** | L | 🟩 | ⬜ | `x86_64_call_dispatch.cpp:117-237`, `x86_64_cc.cpp` |
| **Deferred-action tree** (keystone) | L | 🟩 | ⬜ | `Common.cpp:524-612` |
| **Target hints** (result lands in its final reg) | L (M partial) | 🟩 | ⬜ | `condenseWithTargetHint`, `Common.cpp:560` |
| **Block params/results in registers** | L | 🟩 | ⬜ | `Common.cpp:332-383` |
| Side-effect scheduling (hoist div/loads) | M | 🟦 | ⬜ | `condenseSideEffectInstructionBelow`, `Common.cpp:420` |
| Cost-ordered `selectInstr` + reg-mem operand folding | M | 🟦 | ⬜ | `x86_64_assembler.cpp:218-590` |

Do **not** port: `SKIP` stack elements (vestigial — never written in WARP); strength reduction
(WARP has none). `memory.copy`/`fill` already use `rep movsb`/`stosb` (a fine alternative to WARP's
unrolling).

Today every control-flow boundary (`flush`, `control.go`) and every internal call (`emitWrapperCall`)
spills the whole operand stack to frame slots — the two biggest structural perf costs left.

## B. Features still in WARP that wago lacks (real reference exists)

| Feature | Effort | Value | WARP ref |
|---|:--:|:--:|---|
| **Synchronous host calls w/ return values** (V2 imports) | L | ⭐ | `emitWasmToNativeAdapter`/`emitV2ImportAdapterImpl`, `ImportFunctionV2.hpp`, `NativeSymbol.hpp` |
| **Multi-value** (block params + multiple results) | M–L | 🟩 | `Frontend.cpp:1340-1730`, `reserveStackFrame` |
| **memory.grow / memory.size** | M | 🟩 | `executeMemGrow`/`executeGetMemSize` (`x86_64_backend.cpp:2766`), `ExtendableMemory` |
| **Float→int trapping truncation** (trap on NaN/overflow) | S–M | 🟦 | `emitInstrsTruncFloatToInt` (`:3512`), `FloatTruncLimitsExcl.hpp` |
| **start function** | S | ⬜ | `Frontend.cpp:1050` |

wago's deferred-log host model (void, one i32 arg, replayed after return) is the biggest functional
limitation; basedata already reserves `offCustomCtx` for the WARP V2 ctx pointer.

## C. Runtime / infrastructure

| Item | Effort | Value | WARP ref |
|---|:--:|:--:|---|
| **Interruption / cooperative cancel** (kill runaway loops) | S–M | 🟩 | `checkForInterruptionRequest` (`x86_64_backend.cpp:616`), `requestInterruption` |
| **Wasm-level stack trace on trap** | M | 🟩 | `emitStackTraceCollector`, frame-ref chain, `iterateStacktraceRecords` |
| **Debug mode + bytecode→machine debug map** | M | 🟦 | `Compiler.hpp` `enableDebugMode`/`retrieveDebugMap` |
| **arm64 backend** | L | 🟩¹ | `backend/aarch64/` (self-contained; reuses ported `common/`) |
| Native disassembler (drop objdump dep) | S–M | ⬜ | `disassembler/` (wraps Capstone) |
| Builtin intrinsics (linked-memory, trace points) | M | ⬜ | `BuiltinFunction.hpp`, `execBuiltinFncCall` |
| Passive (guard-page) memory protection | L | 🟦 | conflicts with wago's no-signal design; defer |

¹ value high only if arm64 (Apple Silicon / arm64 Linux) matters. tricore backend: skip (embedded DSP).

## D. Beyond WARP — single-pass ideas (evaluated)

A brainstorm of "emit execution patterns, not instructions" techniques, triaged by whether they
fit wago's **single-pass** backend or require a separate **optimizing tier** (a multi-pass IR over
the `ir` package — a different, larger project that trades wago's fast-compile edge for code
quality). Single-pass-feasible ones are the actionable set.

### Single-pass-feasible (do in the backend)

| Idea | Effort | Value | Status | Notes |
|---|:--:|:--:|:--:|---|
| **Partial evaluation: fold memarg `offset` into addressing** | S | 🟦 | ✅ | PR #36. Folds the validation-time-constant offset into the `lea`/SIB disp32; −2 instrs per offset access, −17% on an offset-heavy sum, bounds check preserved. |
| **Reg-mem operand folding for loads** (`add r,[mem]`) | M | 🟦 | ⬜ | Fold a bounds-checked load as the ALU source of its sole consumer (the "lazy materialization" case). |
| **Superinstructions** (frame-local update `add [slot],imm`) | M | 🟦 | ⬜ | Lookahead fusion. Mostly subsumed for *pinned* locals (already `add reg,imm`); win is on frame-resident locals. |
| **linMem / memBytes base caching** | S–M | 🟦 | ⬜ | Cache the stable memory base in a reserved reg (no `memory.grow` yet); reload after calls. Competes with pinned locals/globals for the pool. |
| **Bounds-check coalescing** | M | 🟩 | ⬜ | Per-block proven-safe tracking: consecutive same-base const-offset accesses share one check. Highest real-world memory win; soundness-critical. |
| Peephole rule table (vs ad-hoc `if`s) | M | ⬜ | ⬜ | Engineering of the above; defer until there are enough rules. |

### Already substantially done by Valent

- **Value-location IR** (`#6`): `ventry` carries location (`vReg`/`vSpill`/`vConst`/`vLocal`/`vPinned`);
  **flags-as-location** is done (`fuse.go`: a compare feeding a branch sets EFLAGS, no setcc/test).
- **Lazy materialization** (`#9`) and **on-demand (delayed) allocation** (`#7`, local form).

### Requires an optimizing tier (NOT single-pass — the `ir`-package project)

Region-based compilation, trace compilation (guess hot paths from the structured CFG),
global **Memory SSA**, **equality saturation** (localized e-graphs), region-wide delayed register
allocation, CPU-port scheduling, and multi-version codegen. The thread's grand pipeline
(Region SSA → const-prop / value-numbering / Memory SSA / equality-sat / superinstruction fusion →
**Valent as the lowering target**) is this tier: Valent stops being the optimizer and becomes the
machine-code emitter after the expensive reasoning. Worth pursuing as a *second* opt-in tier, not a
single-pass change.

## Greenfield (NOT in WARP — no reference)

SIMD/v128, threads & atomics, exception handling, tail calls (`return_call*`), full reference types +
`table.*` ops, bulk-memory remainder (`memory.init`/`data.drop`/`table.init`/`copy`/`fill`/`elem.drop`),
passive data/element segments, memory64, multi-memory, imported memory/table.

## Recommended order

1. **RegisterCopyResolver → register-based call ABI** — highest-return perf: erases the call-heavy
   regressions from register-pinning and targets the one benchmark wago loses to wazero.
2. **Synchronous host calls (V2 imports)** — biggest functional unlock (WASI / real host APIs).
3. **memory.grow**, then **multi-value**.
4. **Interruption** (cheap, high value), then **trap stack traces** + **debug map**.
5. Deferred-action tree → target hints / block-register results (the larger codegen refactor).
