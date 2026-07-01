# wago optimization roadmap

Two complementary lenses on the same question — *how do we make wago faster without
destroying the reason it exists* (fast compile, no cgo, tiny footprint, single pass):

1. **Make the single-pass backend smarter** — better-informed choices inside the existing
   Valent-Block tier (the bulk of the actionable near-term work).
2. **Port what's still worth porting from WARP** (`warp/`) — the C++ reference engine wago
   is a port of. Used here as a *reference axis*, not a target to clone.

The headline architectural decision (see the end): **keep two tiers and do not blend them.**
The single-pass tier stays single-pass; the `src/core/compiler/ir` SSA package becomes an
*optional* optimizing tier later, never something a plain `Compile` pays for.

Legend: effort S/M/L · value ⬜ low · 🟦 medium · 🟩 high · ⭐ very high ·
status ✅ done · 🚧 partial · ⬜ todo. All wago citations verified against current source.

---

## What's already in place

The backend (`src/core/compiler/backend/amd64`) is a single-pass x86-64 codegen fused with
register allocation over a symbolic operand stack (`vConst`/`vLocal`/`vReg`/`vSpill`/`vPinned`),
flushing to deterministic frame slots at control-flow joins. Already done:

- **Immediate compare/branch fusion** — a compare/`eqz` keeps EFLAGS live for exactly one step
  and fuses into an adjacent `if`/`br_if`/`select`, skipping `setcc`+`test` (`fuse.go:26`).
- **Constant folding** — integer arith, shifts, compares, unary, safe div/rem, some float
  (`fold.go`).
- **Lazy + pinned locals** — `local.get` stays symbolic (`vLocal`); first few integer locals
  pinned to `RBX/R13/R14/R15` (`compile.go:38-43,81-82`).
- **Pinned mutable integer globals** — lazy write-back, using pinned slots left after locals
  (`compile.go:45-53,1164-1228`).
- **Memarg offset folding** — constant memarg offset folded into the addressing mode (PR #36,
  `−17%` on an offset-heavy sum, bounds check preserved).
- **Memory-base pinning** — `linMem` kept in `R12` for the whole function instead of reloading it
  from `[RBP-16]` before every memory/global access; gated per function on actual memory/global use
  (`PinMemoryBase`, default on). `−16%` serialize / `−6%` deserialize on json-as SWAR. See P3.
- **Experimental guard-page bounds-check elision** — `4096`-load array sum `3566 → 2686 ns/op`
  (`−24.7%`); see `docs/guardpage-spike.md`.
- **Multi-value** — block params + multiple results, end-to-end (`ir/build.go:1417`,
  `control.go:50-71,202-207`). (Listed as a gap in older notes; it isn't one.)

So the roadmap is not "invent an optimizer." It's: make the existing tier more *informed*, less
*flush-happy*, and less *wrapper-ABI-dependent*.

---

## Phase 0 — build the measurement harness first (prerequisite)

Do this before adding cleverness. Every optimization below needs before/after evidence or it
becomes folklore.

- **Backend stats mode** (debug/test-only config): a `CodegenStats` counting
  `BytesEmitted, Spills, Reloads, Flushes, FlushSlots, Calls/InternalCalls/HostCalls,
  BoundsChecks, BoundsChecksElided, ConstFolds, CompareFusions, PinnedLocals/Globals,
  MaxStackDepth`. The decision-driving numbers are **spills/fn, flushes/fn, flush-slots/fn,
  bounds-checks/fn, code-bytes/fn, internal-call count, control-join count**.
- **Golden disassembly tests per optimization** — extend the `valent_test.go` instinct
  (asserting zero push/pop beyond prologue) with tiny `.wat` fixtures asserting instruction-
  pattern deltas for: local peepholes, compare→branch fusion, bounds-check folding,
  loop-carried local pinning, internal-call ABI, `br_table` lowering, `memory.copy`/`fill`.
- **Workload fixtures**: recursive-call, memory-walking (AssemblyScript-ish), branch-heavy,
  switch-heavy — so the priority items below each have a target benchmark.

---

## Part 1 — single-pass roadmap (priority-ordered, actionable)

### P1. Register-based internal-call ABI  · L · 🟩 (highest runtime upside)

The known loss: recursive calls, the one microbench wazero wins. Today `callInternal` →
`emitWrapperCall` (`compile.go:514-572`) marshals **all** args/results through a 16-aligned
`rsp` buffer with fixed *pointer* regs (`RDI`=args, `RCX`=results, `RSI`=linMem, `RDX`=trap) —
no value crosses the boundary in a register — then spills/reloads pinned locals+globals and
checks the trap slot. `flush()` (`control.go:111-148`) additionally spills the entire operand
stack to frame slots at every control boundary.

WARP instead passes 4 GPR + 4 FPR params / 2 GPR + 2 FPR results **in registers** via its
`WasmABI` (`x86_64_backend.cpp:1158-1207`, `x86_64_cc.hpp:44-94`).

Plan:
- Keep the wrapper ABI at the **public entry** (Go → wasm exports). Add a second entry offset
  per function (`EntryWrapper` / `EntryInternal`); the wrapper prologue unpacks export slots
  into the internal ABI.
- Internal wasm→wasm calls jump directly to `EntryInternal` with a register ABI:
  `RDI`=linMem, `RSI`=trap; int args `RAX,RCX,RDX,R8,R9,…`; float args `XMM0..7`; results in
  `RAX/RDX` or `XMM0/XMM1`; stack spill only on overflow. No need to cosplay the C ABI — wago
  compiles both sides.
- Fall back to wrapper-call lowering for host imports, `call_indirect` (initially), and
  multi-result until stable.

**This requires `RegisterCopyResolver` to land *with* it, not after.** Once args live in
registers, moving each into its ABI slot is a parallel-move problem with cycles
(`RAX→RCX` while `RCX→RAX`). Port WARP's resolver
(`warp/src/core/compiler/common/RegisterCopyResolver.hpp`): emit every move whose target isn't
someone's source, then break the remaining pure cycles — GPR with `XCHG`, XMM with a 3-move
dance through a scratch reg (`x86_64_call_dispatch.cpp:204-237`).

Wins: big recursive-call win, less `rsp` traffic, less pinned-local churn, smaller call-site
code, a foundation for tail-call-ish transforms.

### P2. Hotness-aware local pinning  · M · 🟩 · low risk

`assignPinnedLocals` pins the *first* few integer locals (params assumed hottest). Misses loop
counters, accumulators, compiler temporaries. Replace with a cheap **bytecode pre-scan** (not
an IR pass) that scores locals:

```
+1 local.get   ·  +2 local.set/tee  ·  +4 used at loop depth > 0
+8 carried across a loop back-edge  ·  −10 live across calls in a call-heavy fn
```

Pin the top-N integer locals by score. Fits Valent directly — it's assignment *quality*, not a
backend rewrite (`vLocal`/`vPinned` already exist). Gate behind a tier enum
(`PinningNone`/`PinningParams`/`PinningUseCount`), default to use-count once tests pass.

### P3. Memory-base pinning  · S–M · 🟩 · ✅ done (linMem half)

Every scalar access used to reload `linMem` from `[RBP-16]`, and every *global* access reloaded it
too before chasing `[linMem-88]` (globals table) → cell → value — four dependent loads per global
read. Profiling json-as (AssemblyScript, SWAR mode) showed the AS GC/serializer hot functions spend
**22–25% of all instructions** just reloading the linMem base and the globals pointer (`mov`
accounted for 47–75% of the hottest functions).

**Landed (`CompileOptions.PinMemoryBase`, default on via `RuntimeConfig`):** keep `linMem` in a
dedicated register for the whole function, primed once in the prologue. `R12` is the ideal choice
*because* it is otherwise unused — not in the scratch pool (it forces a SIB byte as a base) nor the
pinned-local pool — so nothing ever clobbers it and the pin survives every wasm→wasm call for free
(a callee re-establishes the same base). The SIB cost is nil on indexed accesses (`[base+ea]` needs
a SIB regardless). The base is stable across `memory.grow` (the reservation is not moved), so no
reload is needed. Required a one-line encoder fix so `memOp` emits the SIB byte for an `RSP`/`R12`
base (`asm.go`). Gated per function on a pre-scan (`funcHints.touchesMem`): pure-compute functions
never prime `R12`, so recursion/calls pay nothing. Sites converted: load/store (int + float),
`loadGlobalsBase`/`loadGlobalCellPtr`, `callHost`.

**Result (json-as SWAR, best-of-5, intra-wasm loop):** serialize **−16%** (447→375 ns/op),
deserialize **−6%** (648→610 ns/op). Corpus neutral (compute micros byte-identical; `memory.sum`
slight win). Verified against the full spec suite (`TestSpecExec`) + `-race`.

**Still open — memory-size (memBytes) pinning:** explicit-bounds mode still reloads memBytes from
`[linMem-8]` per check (now `[R12-8]`, one load not two). Pinning memBytes too is the second half,
but it *does* change on `memory.grow`, so a pinned memBytes must be reloaded after any call that can
grow. Deferred; smaller win than the base pin.

### P4. Algebraic peepholes + strength reduction  · S · 🟦 · low risk

Folding currently fires only when *both* operands are constant (`fold.go`). Add
*partial*-constant simplification in the centralized `binALU`/`mul` paths:

```
add/sub:  x+0→x · x-0→x · 0-x→neg · x+x→lea x*2 · x+small→lea
mul:      x*0→0 · x*1→x · x*-1→neg · x*2ⁿ→shl · x*{3,5,9}→lea
bitwise:  x&0→0 · x&-1→x · x|0→x · x^0→x · x^x→0 (if same ventry identity)
shift:    x<<0→x · x>>0→x  (const mask already in foldShift)
```

(Note: WARP itself has *no* strength reduction — this is a beyond-WARP single-pass idea, safe
because it's local and operand-level.)

### P5. Graduate guard-page mode behind a runtime config  · L · 🟦 (riskier)

The spike (`docs/guardpage-spike.md`) reserves ~8 GiB virtual space, commits active pages,
elides scalar load/store checks, and turns faults into wasm traps via a process-wide signal
handler (`memory.copy`/`fill` keep explicit checks — `rep movsb` can partially write). Make it
a first-class mode gated on: linux/amd64 only, no imported memory, no `memory.grow` (until
remap/protect is handled), documented signal-handler caveat. Add benchmark variants (tight
`i32.load` loop, mixed load/store, bounds-heavy AssemblyScript, string/JSON memory walking).

### P6. Reduce flushes at control joins ("compatible state joins")  · M–H · 🟩 · higher risk

`flush()` materializes the whole operand stack to canonical slots and clears reg state at every
boundary — correct but conservative. Add a per-frame join-state cache: the first incoming edge
records a preferred layout (`joinLoc{kind: slot|reg|const, …}`); later edges either match it or
trigger materialization. If all edges agree, skip the slot stores/reloads at the join. Keep the
first version constrained: single-or-zero result, no nested `br_table`, no pinned-global alias
ambiguity, no float-spill type ambiguity. (This is the single-pass analogue of WARP's
"block params/results in registers", `Common.cpp:332-383` — without adopting the deferred tree.)

### P7. `br_table` jump tables  · M · 🟦 (switch-heavy win)

`opBrTable` (`control.go:322`) emits a linear `cmp/jne` chain — fine for small `n`, tragic as it
grows. Threshold it: `n≤4` linear; `5≤n≤~64` bounds-check + inline `rip`-relative jump table
(`movsxd target,[tbl+idx*4]; add target,tbl; jmp target`); larger watched for code size. Because
cases target different control frames (`branchN`/`height`), table entries land on per-case
stubs emitted after the dispatch, not direct shared-end jumps.

### P8. Extend compare fusion past adjacency (`vFlags`)  · M · 🟦 · medium risk

Immediate fusion only fires when the branch *immediately* follows the compare (EFLAGS live one
step). Misses `cmp; local.tee $c; br_if` and `eqz; local.set/get; if`. Add a `vFlags` stack
entry (a boolean held in flags, carrying its `Cond`), materialized to `0/1` the instant any
flag-clobbering op appears. Start *pattern-constrained* (only `cmp;tee;br_if`,
`cmp;set;get;if`, `eqz;tee;if`), reusing the existing fusion lowering helpers — don't birth a
second compiler.

### P9. Cold trap stubs (hot/cold split)  · S–M · 🟦

`emitTrap` (`control.go:171-176`) inlines `store trap code; leave; ret` at every bounds/div/
indirect/unreachable site. Emit per-function cold stubs (`trap_mem_oob`, `trap_div_zero`, …)
and make hot checks `jcc ok; jmp trap_stub; ok:`. Smaller hot code, better I-cache, cleaner
layout, friendlier to the guard-page path. Keep inline traps for tiny functions if the extra
jump hurts.

### P10. Float spill type tracking  · S · 🟦 · low risk (correctness smell)

`isFloatOperand` returns false for `vSpill` ("type not tracked; assume integer",
`compile.go:801`), so float select/reload paths can guess wrong. Carry `fp`/`wide` metadata on
spills consistently. Cheap, removes a latent bug class, unlocks float-correct select/reload.

### P11. Compile-speed hygiene (don't give back the 34× edge)  · S · ⬜ · low risk

- Replace `map[byte]Cond` opcode dispatch (`i32cmp`/`i64cmp`, `compile.go:1083,1441,1701`) with
  `[256]Cond` + `[256]bool` tables — same codegen, less compile overhead.
- Pre-size the compiler slices in `cg` (symbolic stack, control stack, relocs, local/pinned
  arrays) from validation metadata.

### Tiny enabling pre-pass: `FuncHints`  · S · low risk

The one "pass" worth adding without betraying single-pass. A reconnaissance scan (not SSA)
producing per-function: local/global get/set lists, `MemOps`, `Calls`/`InternalCalls`/
`HostCalls`, `LoopDepthMax`, `HasBrTable`/`MaxBrTable`, `ConstMemOps`, `HasIndirectCall`. Feeds
P2 (local pinning), P3 (mem pinning), P7 (jump-table threshold), P1 (internal-ABI eligibility),
P9 (trap-stub choice), and buffer pre-sizing. Build the drone, not the aircraft carrier.

---

## Part 2 — WARP-port reference axis

WARP is essentially **MVP-scoped**: SIMD, threads/atomics, exception handling, tail calls, full
reference types, the bulk-memory remainder, multi-memory, memory64 are **not in WARP either**
(it throws `FeatureNotSupportedException`) — those are greenfield. wago is already **ahead** of
WARP on `trunc_sat`, `select t`, and imported/mutable/typed globals.

### Feature gaps with a real WARP reference

| Feature | Effort | Value | Status | Notes (verified) |
|---|:--:|:--:|:--:|---|
| **memory.grow / memory.size** | S–M | 🟩 | ⬜ | Decoder+validator+IR already model it (`ir/op.go:22-23`, `build.go:887-908`); only the frontend whitelist (`frontend.go:485-516`), backend lowering, and runtime remap/size-cache remain. *The* remaining MVP gap. |
| **Float→int trapping truncation** | S | 🟦 | ⬜ | Opcodes wired (`compile.go:1628-1639`) but lower to a bare `Cvttf2si` with no NaN/overflow guard (`fp.go:259-266`); `TrapTruncOverflow` already exists (`runtime/trap.go:22`). Smallest correctness gap. |
| **start function** | S | ⬜ | ⬜ | Hard-rejected today (`frontend.go:120-122`); needs instantiate-time invocation. |
| **Sync host calls w/ return values (V2 imports)** | L | ⭐ | 🚧 | Runtime re-entry half is spiked — `HostFunc`/`CallWithHost`/`hostCallPending` (`runtime/engine_linux_amd64.go:50-78`) + `stubHostCall` blob (`runtime/stubs_amd64.go:117-126`). Compiled path is still deferred-log (void, ≤1 i32 arg, `compile.go:646-670`). Missing piece: the JIT emitter generating the protocol. Biggest functional unlock (WASI). `offCustomCtx` already reserved. |

### Codegen items from WARP (the deferred-tree family)

WARP defers every arithmetic op into a **deferred-action tree** and condenses a whole
sub-expression at once (side-effects → scratch-pressure → target-hints), vs wago's eager
one-op-at-a-time. The tree is the keystone that unlocks the rest:

| WARP optimization | Effort | Value | WARP ref |
|---|:--:|:--:|---|
| **Deferred-action tree** (keystone) | L | 🟩 | `Common.cpp:524-612` |
| **Target hints** (result lands in its final reg) | L | 🟩 | `condenseWithTargetHint`, `Common.cpp:560` |
| **Block params/results in registers** | L | 🟩 | `Common.cpp:332-383` (see P6 for the single-pass approximation) |
| Side-effect scheduling (hoist div/loads) | M | 🟦 | `condenseSideEffectInstructionBelow`, `Common.cpp:420` |
| Cost-ordered `selectInstr` + reg-mem operand folding | M | 🟦 | `x86_64_assembler.cpp:218-590` |
| Reg-mem operand folding for loads (`add r,[mem]`) | M | 🟦 | fold a bounds-checked load as its sole consumer's ALU source |

Do **not** port: `SKIP` stack elements (vestigial — never written in WARP).

### Runtime / infra from WARP

| Item | Effort | Value | WARP ref |
|---|:--:|:--:|---|
| **Interruption / cooperative cancel** (kill runaway loops) | S–M | 🟩 | `checkForInterruptionRequest` (`x86_64_backend.cpp:616`) |
| **Wasm-level stack trace on trap** | M | 🟩 | `emitStackTraceCollector`, frame-ref chain |
| **Debug mode + bytecode→machine debug map** | M | 🟦 | `Compiler.hpp` `enableDebugMode`/`retrieveDebugMap` |
| **arm64 backend** | L | 🟩¹ | `backend/aarch64/` (self-contained; reuses ported `common/`) |
| Native disassembler (drop objdump dep) | S–M | ⬜ | `disassembler/` (wraps Capstone) |
| Passive (guard-page) memory protection | L | 🟦 | conflicts with no-signal default; see P5 |

¹ value high only if arm64 (Apple Silicon / arm64 Linux) matters. tricore backend: skip.

### Small constant bulk-memory specialization  · M · 🟦

`memory.copy`/`fill` lower to `rep movsb`/`stosb` with operands spilled to fixed regs
(`memory.go`) — sensible for dynamic sizes, overkill for tiny constant `n`. When `n` is a
`vConst`: `fill` → scalar store (`n≤8`) or short unrolled stores (`n≤32`); `copy` similar but
overlap-safe (wasm requires memmove semantics — the current fwd/bwd direction logic must be
preserved). Start with `fill`; it's easier.

---

## Greenfield (NOT in WARP — no reference)

SIMD/v128, threads & atomics, exception handling, tail calls (`return_call*`), full reference
types + `table.*` ops, bulk-memory remainder (`memory.init`/`data.drop`/`table.*`), passive
data/element segments, memory64, multi-memory, imported memory/table.

---

## Merged priority order

```
P0. Measurement: CodegenStats + golden disasm tests + workload fixtures   (prerequisite)
 1. FuncHints pre-scan                                                     (enables 2,3,7)
 2. Hotness-aware local pinning
 3. Memory base / memory-size pinning (no-call memory-heavy fns)
 4. Algebraic peepholes + strength reduction
 5. Register-based internal-call ABI  (+ RegisterCopyResolver, together)   ← biggest perf
 6. MVP-completion batch: memory.grow/size · trunc traps · start fn        (cheap, ships MVP)
 7. Graduate guard-page mode behind runtime config
 8. vFlags / limited compare-local-branch fusion
 9. Cold trap stubs
10. br_table jump tables
11. Compatible join-state optimization
12. Sync host calls (V2 imports) — finish the spiked runtime half
 -- only then: wire the ir/ SSA package as an OPTIONAL optimizing tier
```

Biggest likely wins: recursive/internal-call perf (P5 register ABI) · memory-loop perf
(P7 guard pages + P3 pinned linMem/memSize) · general scalar code (P2 pinning + P4 peepholes) ·
branchy code (P8 fusion + P6 fewer flushes) · switch-heavy code (P10 jump tables).

Independent quick wins worth pulling early: the **MVP-completion batch** (item 6) — three small,
high-confidence, independently-shippable PRs (`memory.grow` is the headline MVP gap; trunc-trap
and `start` are tiny).

---

## The one architecture choice

Keep **two tiers, unblended**:

- **Tier 1 — single-pass baseline JIT** (today's Valent-Block backend):
  `validated wasm → FuncHints pre-scan → single-pass codegen`. Goals: very fast compile, good-
  enough code, tiny footprint, predictable. Everything in Part 1 lives here.
- **Tier 2 — optional SSA optimizing tier**: the `src/core/compiler/ir` scalar SSA package
  already exists but no runtime path imports it. Keep it that way until the baseline is strong.
  Use it only for hot modules / AOT `.wago` / an explicit `Optimize` option / benchmarks — never
  on the default `Compile` path.

wago's identity is **low-latency compile**, not "Cranelift written by one mortal." Make the
single-pass tier more informed, less flush-happy, and less wrapper-ABI-dependent first; SSA is
the expensive opt-in tier, later.
