# wago optimization roadmap

Two complementary lenses on the same question — *how do we make wago faster without
destroying the reason it exists* (fast compile, no cgo, tiny footprint, single pass):

1. **Make the single-pass backend smarter** — better-informed choices inside the existing
   railshot tier.
2. **Port what's still worth porting from WARP** (`warp/`) — the C++ reference engine the
   backend is a port of. Used as a *reference axis*, not a target to clone.

The headline architectural decision (see the end, **revised 2026-07-03**): **no IR on any
execution path.** Railshot is the one and only backend; the `src/core/compiler/ir` SSA
package stays as an off-path research/debug tool, not a planned tier. The ceiling SSA was
reserved for is attacked incrementally instead — see `docs/no-ir-plan.md`.

Legend: effort S/M/L · value ⬜ low · 🟦 medium · 🟩 high · ⭐ very high.

---

## What's in place (updated 2026-07-03)

The backend (`src/core/compiler/backend/railshot`) is the full WARP-architecture port: a
single-pass x86-64 codegen over a valent-block operand stack (deferred-action trees,
condense engine) with an on-the-fly whole-register-file allocator. Landed, in rough order:

**Storage model / register allocation**
- **Register-ABI internal calls** (old P1) — args/results in registers between wasm
  functions; wrapper ABI kept at the Go boundary. Includes the parallel-move resolver.
- **Hotness-aware local pinning** (old P2) — loop-weighted scores from a one-pass
  `scanBody` pre-scan (`hints.go`), WARP-style whole-file pin pool for call-making
  functions too (up to `file − 4 scratch`), STACK_REG lazy spill (dirty-only stores at
  calls, lazy reload) for **all** call-making functions. #68's real root cause (the
  `opElse` merge edge skipping reconciliation) was found and fixed with regression tests.
- **Value-pinned hot globals** sharing the pin pool (#84–#86).
- **memBytes in R15** (old P3) — explicit-bounds mode keeps the memory size in a
  module-wide reserved register (WARP `REGS::memSize`); checks are `lea; cmp; ja stub`.
- **Lazy per-frame merge agreement** (old P6, locals half) — control-flow edges agree
  per-frame on each pinned local's merge state (`lsStackReg` or `lsMem`), so a
  call-clobbered local can stay slot-only across a merge until actually read. Loop tops
  stay eager (reloads hoisted out of bodies). Conditional returns converge nothing.

**Bounds checks / traps**
- **Guard-page mode** (old P5) is first-class behind `-tags wago_guardpage` and is the
  *default* bounds mode in such builds (`WAGO_BOUNDS=explicit` overrides).
- **Shared cold trap stubs** (old P9) — one stub per trap code per function; every check
  is a fall-through `ja stub`. (~23% smaller code on memory-heavy modules.)
- **Stack-fence elision for small call-free leaves** — a leaf's one unchecked frame is
  absorbed by the fence's 256 KiB margin.

**Instruction selection**
- Compare→branch fusion; constant folding; memarg offset folding; deferred loads folded
  as ALU r/m operands; in-place accumulation; cmov select.
- **Algebraic identities + strength reduction** (old P4) — `x±0`, `x&~0`, `x|0`, `x^0`,
  shifts by 0, `x*1`, `x*0`, `x*2ⁿ→shl`, `x*{3,5,9}→lea`, `x/ᵤ2ⁿ→shr`, `x%ᵤ2ⁿ→and`,
  `x-x`/`x^x→0`, `x&x`/`x|x→x` — at `pushBinOp`, before a node exists.
- **Scaled-index LEA fusion** — `add(x, shl(y, k≤3))` → `lea [x + y*2ᵏ]` (the
  AssemblyScript array-address shape).
- **`br_table` jump tables** (old P7) — n≥5 dispatches through a RIP-relative offset
  table with deduplicated per-case stubs; smaller tables keep the cmp/jne chain.
- **Small constant `memory.fill`/`copy` unrolled** — n≤32 lowers to overlap-safe
  load-all/store-all chunks (memmove semantics preserved); no `rep` microcode startup.
- **`call; local.set` result fusion** — a register-ABI call result lands directly in the
  pinned local's register.
- **Register-ABI `call_indirect`** — the table entry's pad word carries the internal-entry
  delta, so compatible signatures skip the wrapper adapter.
- **Code layout** — 16-byte aligned functions, internal entries, and loop tops (multi-byte
  NOPs on the entry path). Tight-loop benchmarks swing ±20% on layout luck without this;
  treat any single-module regression as suspect until the disassembly is diffed.

**MVP completeness** (old "completion batch"): memory.grow/size, trapping float→int
truncation + trunc_sat, start function, multi-value, imported/mutable globals.

**Compile speed**: decoded modules keep byte-backed function bodies. The optional
`scanBody` instruction walk is used only for programmatically constructed modules that
provide decoded instructions; normal decoded modules use BodyBytes and first-N pinning.
Validation is byte-backed and no-body too (#96: type-cache + validator/reader reuse,
validation allocs −90%).

**Landed since the #87/#88 sweep (2026-07-02 → 07-03)**
- **Borrowed reads for value-pinned globals** (`stGlobReg`, #93) and
  **immediate-only constant stores** (`StoreImmIdx`, #94).
- **Float parity batch** (#97): `minss/maxss`-based min/max with NaN fixup + deferred
  float loads (VEX 3-op float ops were #79).
- **Forward `rep movsb` for disjoint `memory.copy`** (#99) — the json serialize fix; the
  backward-copy path gets no ERMSB/FSRM and was ~89% of serializer samples.
- **Adaptive per-module global-pin K (1–3)** (#100) and **`x*{3,5,9}` → LEA** (#101).
- **Instantiate reuse** (#105 explicit, #108 guard: 12→3.4µs) and faster validation (#96).
- **Full wasm 1.0 MVP: 57/57 spec files, 0 failing assertions** (#111–#115: spectest host
  module, cross-instance function/global/table/memory linking, host functions as table
  funcrefs).

### Measured (2026-07-02, explicit bounds, vs the pre-sweep #87 baseline)

| bench | #87 | sweep | Δ |
|---|---:|---:|---:|
| sieve | 163µs | 123µs | **−24%** (beats wazero) |
| memory_tree | 14.6µs | 11.8µs | **−19%** |
| linked_list | 11.3µs | 9.4µs | **−17%** |
| dispatch (call_indirect) | 19.1ns | 17.6ns | −8% |
| blake-as | 729µs | 700µs | −4% |
| json-as ser / deser | 218 / 396 | 197 / 204 | −10% / **−48%** |
| memory.sum (explicit vs guard) | 337 | 230 | **explicit == guard** |

Cumulative from before #87 (main@22c09be): json ser 257→197, deser 420→204;
memory.sum 552→230; sieve 165→95; memory_tree 17.2→11.6; wazero-relative json
0.56x→0.72x ser / **0.70x→1.43x deser (wago now wins)**. wago beats wazero on
fib_rec, sieve, memory_tree, linked_list, dispatch, branches, and json deserialize;
loses on json serialize and blake.

The deserialize flip came from running WARP itself on json-as (passive/bounds-off
build, ser 97ns / deser 164ns per unit) and replicating its remaining structural
edges: no per-call environment protocol (RBX/linMem as module invariant, trap cell
in basedata — no trap-clear on returns), module-wide global register pinning (the
AS shadow-stack pointer), pinned-register-borrowed load addresses, and — decisive
for deserialize — an inline 8-byte chunk-loop memmove for small dynamic
memory.copy/fill instead of `rep movsb` (whose startup latency dominated the
string-append copies AssemblyScript's `__renew` makes constantly). wago-guard
deser is now within 1.13× of WARP.

**Update (post #97/#99/#100/#101, guard mode):** json ser **93ns / deser 175ns**
per unit — **serialize now beats WARP (97)**; deser is 1.07× WARP (164). wago
beats wazero (147/305) on both json directions. The serialize chase is closed;
see R4.

---

## Remaining roadmap (priority-ordered)

The detailed, phase-by-phase execution plan for everything below is
**`docs/no-ir-plan.md`** (2026-07-03, incorporating an external repo review that was
triaged against the tree). R-numbers here are stable labels; Pn are that plan's phases.

### R0. `CodegenStats` + explain mode  · ✅ LANDED (`perf/codegen-stats`)
Per-function counters (spills/flushes/condenses/store-forced deferred loads/bounds
checks/trap stubs/calls by kind/pins/peephole hits) collected only on request via a
nil-safe `stats` field on `fn` — byte-identical codegen when off. Surfaced through
`CompileOptions.Stats`, `WAGO_EXPLAIN=1`, and `bench/cmd/explain`; ships the
`WAGO_DEBUG_MODGLOBALS` / `WAGO_PIN_GLOBAL_K=auto|0..3` knobs and an objdump-based
golden-disasm harness (`golden_test.go`). Every subsequent optimization lands with
its counter moving and a golden. (Plan P1; see `docs/no-ir-plan.md` P1 for the
counter list and on-corpus verification.)

### R1. `stFlags` — compare fusion past adjacency  · M · 🟩 (old P8)
**`eqz`-of-compare fusion LANDED** (`perf/railshot-stflags`, gated `WAGO_NO_STFLAGS`):
`condenseToFlags` peels `eqz` wrappers around a fusable compare and INVERTS the branch
condition instead of materializing the inner boolean — `eqz(a<b); if` → `cmp; jcc`
(inverted) rather than `cmp; setcc; movzx; test; jz`. Nested `eqz` double-inverts. The
inner CMP is still emitted last, so flag safety is unchanged (no register/merge hazard).
This was the dominant realizable slice: **esbuild 26,344 folds, compare-setcc 44,495→18,151
(−59%), −272 KB code (0.84%)**; sqlite 425. Verified: spec suite (16,022 asserts) + full
corpus/WASI differential + A/B kill switch.

**Premise correction (profiled 2026-07-08):** the roadmap framed the flags-resident
*storage* + local round-trip (`cmp; local.tee $c; br_if`, `eqz; local.set/get; if`) as the
unlock. Corpus scan refuted it: adjacent `compare→branch` already fuses ~99% (compilers
emit the compare adjacent to its branch), and the round-trip patterns barely exist
(`cmp;set;get;br` = **0** across esbuild/sqlite/lua/json; `cmp;tee;br` = 0/72/22/0). The big
`compare-setcc` counts are overwhelmingly GENUINE booleans (stored/returned/used in
arithmetic), not missed fusions — EXCEPT `eqz`-of-compare, which is huge and is what landed.
The full flags-resident storage kind (single-owner + demote-before-clobber, select-from-
flags on a local, store8-of-flags) remains available but is now low-ROI and high-risk (the
branch-merge machinery — `convergeEdgeTo`'s xor-zeroing, `flushBelow`'s condensing — clobbers
EFLAGS between an early CMP and its Jcc; a pinned flags-local desyncs across the merge). Not
recommended without a new payoff signal. (Plan P3.)

### R2. Float lowering parity — remaining half  · M · 🟦
~~min/max branchy lowering~~ (done #97, with deferred float loads; VEX 3-op #79).
Still open: in-place XMM accumulation (int path has it), float pinned locals still on
the eager call-spill model, float `call; local.set` fusion (int-only today), mixed-call
parallel staging (`emitMixedRegisterCall` still does full `flush()` + slot staging).
(Plan P5.1–.2.)

### R3. Store-narrowing peephole  · S · ⬜
`setcc; movzx; store8` keeps a dead `movzx` (sieve's inner loop). Ships standalone if
trivial, otherwise falls out of `stFlags` for free — don't build scaffolding twice.
(Plan P2.5/P3.)

### R4. json serialize gap — ✅ RESOLVED (2026-07-02)
Closed by #99 + #100: guard-mode ser 190→**93ns (beats WARP's 97)**; deser 175ns
(1.07× WARP). The forensic trail (B1 `stGlobReg` #93, B2 immediate stores #94, the B3
WARP wat-27 burst diff, the K-sweep) is preserved in PR #95's findings doc and
`docs/valent-blocks-expansion-plan.md` §0–§2. The punchline for posterity: the
bottleneck was never call overhead or global register residency — **~89% of serializer
samples were one backward `std; rep movsb`** in `memory.copy` (no ERMSB/FSRM on
backward copies) on copies that were disjoint and forward-safe. B1+B2 remain as real
codegen improvements (wago's burst emits fewer instructions than WARP's), and the
V0 measurement discipline that found this is now doctrine: profile before chasing a
hypothesis (memory `wago-serialize-memcopy-win`; ≤0x18 perf bins). Serialize is now
flat/GC-bound; no further wago-side lever identified.

### R5. Runtime / infra from WARP (plan P8)
| Item | Effort | Value | Notes |
|---|:--:|:--:|---|
| Sync host calls w/ return values (V2 imports) | L | ⭐ | runtime half spiked; biggest functional unlock (WASI). #111/#115 added typed params + host funcrefs; **results** still missing |
| WASI preview 1 (minimal) | M | 🟩 | after sync imports |
| Interruption / cooperative cancel | S–M | 🟩 | loop backedges + entries; same machinery as Go-GC safe points |
| Wasm-level stack trace on trap | M | 🟩 | trap site → func idx → wasm pc |
| Debug mode + bytecode→machine map | M | 🟦 | |
| arm64 backend | L | 🟩¹ | WARP `backend/aarch64` as reference |

¹ if Apple Silicon / arm64 Linux matters.

### R6. Measurement hardening → **promoted to R0**, see above.

### R7. Adopted from the 2026-07-03 external review (new items; plan §2)
Codegen, cheap-and-safe first: **alias-aware pending loads** (any store currently
flushes ALL deferred loads — keep same-base provably-disjoint ones, plan P2.1) ·
**pure-tree `drop` discard** (P2.2) · **const-fold pack** — compares/eqz/clz/ctz/
popcnt/extensions + narrow-load mask elision (P2.3) · **same-operand int compare
identities** (P2.4). Then: **limited multi-result register ABI** (RAX,RDX / XMM0,XMM1 —
unblocks multi-value, with `regMerge2`, P5.3) · **straight-line bounds facts** +
**hybrid loop precheck** (explicit mode; the TinyGo story, P6.1–.2) · **store
combining** (explicit-only, cold-path sequential replay for trap semantics, P6.3) ·
**CPUID probe** (JIT'd stub, zero deps) gating **BMI2 shifts** + `smallBulkMax`
tuning (P6.5) · **immutable-global const folding** incl. link-time specialization of
imported ones (fits the existing link-time recompile) · **`call_indirect` inline
caches** behind a table epoch (P8.6) · **`.wago` cache keys + CLI**
(compile/run/inspect, P8.7) · **call-surviving valent trees** and a **tiny bytecode
inliner** (both decision-gated on R0 counters, P5.4–.5) · **fused validate+compile**
(premise re-measured post-#96, P7). Rejected (with reasons — plan §1.3): `stAddrExpr`,
known-bits lattice, general pending sets with owned regs, tiny unroll, SIMD copy/fill
now, `memory.size` micro-opt.

### Greenfield (not in WARP either)
SIMD/v128, threads & atomics, exception handling, tail calls, full reference types +
`table.*`, passive element execution, remaining bulk-memory table ops, memory64,
multi-memory. (Cross-instance linking + imported memory/table/global landed
in #112–#115; the `linking`/`data` spec files now pass.)

---

## The one architecture choice (revised 2026-07-03)

**No IR on any execution path — railshot is the only backend.** The earlier "Tier 2
optional SSA" framing is retired; the E-gate SSA-spike question in the perf plans is
answered: no.

- **The pipeline is the identity**: `decode → validate (byte-backed) → scanBody hints
  (summary facts only) → railshot single-pass codegen → native`. Fast validated bytes →
  direct native code; no AST, no SSA, no whole-function IR on the hot path.
- **The ceiling gets attacked incrementally** ("Tier 1.5"): flags-resident values,
  restricted pending sets, call-surviving trees, alias-aware load windows, bounds
  facts — each a small extension of the valent-block storage model, each individually
  gated and measured (`docs/no-ir-plan.md`). The original case for SSA (wazero's json
  edge = its register allocator) has weakened: wago now beats wazero on both json
  directions and most of the corpus without it.
- **`src/core/compiler/ir` stays off-path** as a research/debug package (potential
  differential oracle); it is not a planned tier, not deleted, and not grown.
- **Guardrail**: `scanBody` stays summary-only (scores, shape flags). If it starts
  storing instruction graphs, it has become IR in a trench coat — reject in review.

wago's identity is **low-latency compile**: the single-pass tier is informed,
flush-light, and register-resident, and it stays single-pass.
