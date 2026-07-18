# No-IR plan — railshot optimization & product roadmap (2026-07-03)

Triage of an external repo review (LLM with GitHub read access; code/doc
inspection only — no local build or bench run) against actual repo state,
turned into an implementation plan. The review independently converged on the
same strategy as `docs/valent-blocks-expansion-plan.md` — *shrink the hard
sinks, measurement first* — and contributed a number of genuinely new items
(bounds facts, inline caches, CPUID gating, multi-result ABI, product/CLI
work). This doc schedules both.

How to read: `docs/perf-plan-2026-07.md` §1 (measurement protocol) and
`docs/valent-blocks-expansion-plan.md` (the V-phase designs) still apply and
are referenced as "VB §n" below. This doc **supersedes both docs' ordering**
(and the SSA/E-gate decision framing) but not their designs or pitfalls.

## 0. The architecture decision: no IR on the execution path

Decided 2026-07-03: **wago builds no SSA and no whole-function IR on any
execution path. Railshot is the one and only backend.** The prior framing
(OPTIMIZATIONS.md "two tiers, SSA optional later"; the E-gate SSA spike in
`perf-plan-2026-07.md` §0 / VB §0.3) is retired. Consequences:

- `src/core/compiler/ir` stays in-tree as an **off-path research/debug
  package** (future differential oracle, dominance-checked shape tests). It is
  not a planned execution tier; no runtime path imports it (verified — no
  non-test imports outside the package). Don't delete it; don't grow it.
- The structural ceiling the SSA tier was reserved for (cross-block register
  allocation) is attacked incrementally instead: flags-resident values,
  pending sets, call-surviving trees, bounds facts — "Tier 1.5", VB's framing.
- The public identity line: *wago compiles validated Wasm bytes directly to
  native code — no AST, no SSA, no IR pilgrimage — using valent-block deferred
  expression trees and a single-pass whole-register-file allocator.*
- `scanBody`/`scanBodyBytes` (`hints.go`) stays **summary-only** (hotness
  scores, feature/shape flags, small-function classification). The moment it
  stores instruction graphs it has become IR in a trench coat. Guard this in
  review.

## 1. Review triage — verified against the tree (2026-07-03, post-#115)

### 1.1 Correct and open (adopted — scheduled in §2)

| Claim | Evidence |
|---|---|
| `materializePendingLoads` is alias-blind: any store/copy/fill forces ALL deferred loads | `regalloc.go:227` takes no args; call sites `memory.go` (memStore + 3 more) |
| `drop` condenses a deferred tree just to discard it | `driver.go:63` handles concrete kinds only; `popValue` (`driver.go:583`) condenses first |
| No flags-resident compare results (`stFlags`); fusion is adjacency-only | only `condenseToFlags` (`fuse.go:71`) used by fused `br_if`/`if` |
| `setcc; movzx; store8` keeps a dead `movzx` | OPTIMIZATIONS R3, unchanged |
| Mixed (float-carrying) reg-ABI calls do full `flush()` + canonical-slot staging | `call.go:419` (`emitMixedRegisterCall`), comment says so explicitly |
| `call; local.set` result fusion is int-only | `call.go:111` |
| Float pinned locals still use the eager spill-all call model; no in-place XMM accumulation | OPTIMIZATIONS R2; `fp.go` has no in-place path |
| Reg ABI is single-result only | `sigFitsRegABI` rejects `len(Results) > 1` (`call.go:69`) |
| No CPUID/feature probe anywhere; `smallBulkMax` fixed at 96 | grep; `memory.go:43` |
| No BMI2 shifts (RCX round-trip on variable shifts) | grep; VB §9.4 |
| Immutable numeric globals are not const-folded at `global.get` | `globals.go:56`; `Mutable` only consulted for pin selection (`compile.go:352`) |
| Block/if merges >1 result go through slots (`regMerge1` is single-result) | `control.go:296` |
| Sync host-import **results** still missing — the biggest functional unlock (WASI) | FEATURES: host imports are void-result batched-replay; #111/#115 added typed params, funcref use, not results |
| Only ~8 `WAGO_*` env flags; no per-optimization A/B switches or explain output | grep `os.Getenv("WAGO` |
| ROADMAP.md badly stale (lists float traps, memory.grow, reg-ABI calls as "next" — all long done) | ROADMAP "Next"/"Engine & performance" sections |

### 1.2 Stale in the review (already done/changed — do NOT redo)

| Review said | Reality |
|---|---|
| Float min/max branchy (2.2×) | **Done #97** (min/max parity + deferred float loads; VEX 3-op in #79) |
| json serialize ~2× WARP, chase call overhead | **Resolved #99/#100**: backward `rep movsb` was the bottleneck; guard ser **93ns beats WARP 97**; deser 175 vs WARP 164. Serialize chase is CLOSED (V0/V1 premise refuted, VB §1–2) |
| Validation builds a 424 B/instr AST; fused validation kills the compile bottleneck | **#96 landed**: validation is byte-backed, no-body, validator/reader reuse, allocs −90%. V7's premise must be re-measured before any fused-validation work (§2 P7) |
| `hints.go` walks the AST | Byte scanner `scanBodyBytes` (`hints.go:115`) for decoded modules; AST walk only for programmatically-built modules |
| Spec gate = 16500 pass / 13 fail, `linking` FAIL / `data` BLOCKED | **57/57 files, pass=16592 fail=0 skip=1591** since #111–#115 (SPECTEST.md). Gate expectations updated in §4 |
| "adaptive threshold" global pinning could use a K override + debug print | Adaptive K **landed #100**; the K override + `WAGO_DEBUG_MODGLOBALS` print are adopted into P1 |

### 1.3 Rejected / deferred (with reasons)

- **SSA/IR execution tier** — decided against, §0.
- **`stAddrExpr` storage kind** — mini-IR risk the review itself flags; squeeze
  `memAddr`/`stMemRef` + bounds facts (P6) first. Revisit only with disasm
  evidence of repeated re-computed addresses that P6 can't catch.
- **General pending `local.set` with owned registers** (VB §5 options a/b) —
  allocator-invisible trees; only the register-free restriction (c) is
  scheduled (P4). Revisit a/b only if (c) measures well but misses cases.
- **Persistent/general known-bits lattice state** — rejected. Revisited 2026-07-17
  after the Souper and packed-byte/SWAR designs: Railshot now has a bounded,
  allocation-free estimator that recursively visits only its existing depth-capped
  deferred tree. It carries no facts across nodes or blocks. The profitable cases
  remain narrow/shift mask elision (P2) and direct `(word & laneMask) == 0` flag
  lowering; boolean-ness remains subsumed by `stFlags` (P3).
- **Tiny unroll (const trip ≤4)** — layout-luck risk (±20% swings) exceeds the
  expected win; not now.
- **Induction/accumulator pattern recognition + extra hint scoring terms** —
  defer until a bench demands it; pinning already covers the measured hot cases.
  Exception: the *loop-header facts* byte-scan record is adopted (P6.2 needs it).
- **SIMD `memory.copy`/`fill`** — after SIMD exists at all.
- **Store queue beyond a one-entry combining window** — no; V6 is already the
  delicate edge of what trap semantics allow.
- **`memory.size` micro-optimization** — skip until it shows up in a profile.
- **Deleting `warp/` or `ir/`** — no. Reference axis + future oracle.

## 2. Phase plan

Every phase: own branch (`perf/<topic>` or `feat/<topic>`) off `main`, own PR,
gate battery (§4), merges need an explicit user yes. Phases are ordered by
(leverage ÷ risk); P8 is a parallel product track, not last in urgency.

### P0. Housekeeping (S) — this PR + one follow-up
1. ~~This doc + OPTIMIZATIONS.md rewrite (no-IR decision, post-sweep state).~~
   **Done (#116).**
2. ~~**ROADMAP.md refresh** (separate PR): delete the done-items-as-planned rot
   ("Numeric completeness" block, memory.grow, reg-ABI calls, locals-in-regs);
   align with FEATURES.md + this plan.~~ **Done** (`docs/roadmap-refresh`): rewrote
   to a Done/Next-two-tracks structure that defers to FEATURES.md (matrix),
   OPTIMIZATIONS.md (codegen rationale), and this doc (P0–P8 detail); the no-IR
   decision is now a first-class Non-goal.

### P1. CodegenStats + explain mode (S/M) — ✅ LANDED (`perf/codegen-stats`)
Was OPTIMIZATIONS R6; the review is right that it comes first.
- `CodegenStats` per function, collected only when requested (nil when off —
  zero hot-path cost): code/frame bytes, max spill slots, flushes /
  `flushBelow`s, condenses, deferred loads folded vs forced, bounds checks
  emitted (elided later, P6), trap stubs, calls by kind
  (reg-ABI/mixed/wrapper/indirect/host), pins (locals, value-pinned globals,
  module-pinned K + which regs), spills/reloads, peephole hits by name
  (`map[string]int` is fine off the hot path).
- Surfacing: `CompileConfig` option + `WAGO_EXPLAIN=1` env; a
  `bench/cmd/explain` (or test helper) that dumps the per-function report like
  the review's mock. Ship `WAGO_DEBUG_MODGLOBALS=1` (the #90-era temp print)
  and `WAGO_PIN_GLOBAL_K=auto|0..3` override here.
- Golden disassembly tests: extend the existing objdump-based codegen tests
  into per-optimization goldens — cmp→branch fusion, immediate stores, LEA
  mul/scaled-index, `call; local.set`, reg-ABI `call_indirect`, forward
  `memory.copy`, guard-vs-explicit shapes. Every later phase adds its golden
  here as its regression net.
- Optional: formalize `WAGO_PERFMAP=1` (the jsonprof memfd offset protocol,
  `perf-plan-2026-07.md` §1) as a first-class runtime flag.

Exit: explain dump on json-as and blake matches the known facts (K pins, B2
immediate stores, forward-copy path) — i.e. the counters are trustworthy.

**Landed** (`src/core/compiler/backend/railshot/stats.go`): `CodegenStats` +
`ModuleStats` threaded through the `fn` via a nil `stats` field — every counter
is a nil-safe no-op when off, so codegen is byte-identical (proved by
`TestCodegenStatsCodegenNeutral`). Counters: code/frame bytes, spill high-water,
flushes/flushBelows, condenses, spills/reloads, `MemRefsForcedByStore` (the P2.1
alias-blind signal), `BoundsChecks` (P6 target), trap stubs, `Calls` by kind
(regabi/mixed/wrapper/host/indirect/crossinstance), pinned locals + value-pinned
globals + module-pin K/regs, and a `Peephole` map (const-fold, alu-identity,
strength-reduce, lea-scaled-index, store-imm, memcopy/memfill-unroll,
cmp-branch-fuse, select-cmov, br-table-jump, call-localset-fuse). Surfaced via
`CompileOptions.Stats` sink + `WAGO_EXPLAIN=1` (stderr dump) + a `bench/cmd/explain`
tool; `WAGO_DEBUG_MODGLOBALS=1` and `WAGO_PIN_GLOBAL_K=auto|0..3` shipped.
Verified on-corpus: json-as K=3 (g2/g4/g25), blake K=1 (g11). Deviations from the
plan text: (a) there were **no** pre-existing objdump codegen tests to "extend" —
the golden harness (`golden_test.go`, objdump-based, skips when absent) is new;
(b) "deferred loads folded vs forced" ships as the single `MemRefsForcedByStore`
counter (the number P2 must drive down); the "folded" side is implicit. Remaining
P1 nice-to-haves deferred: `call; local.set` and reg-ABI `call_indirect` goldens
(counters exist), and formalizing `WAGO_PERFMAP=1`.

### P2. Cheap railshot wins, one batch PR (S each)
**⚠️ MEASURED near-dead on the real corpus (2026-07-04, via P1 counters) —
deprioritized below P3.** With the P1 dashboard: `MemRefsForcedByStore` = 0 across
*every* corpus module except json-as (=1, and that one is not a keepable
same-base-disjoint case — alias-on vs alias-off are identical). `const-fold`,
`same-operand`, `alu-identity`, and `strength-reduce` are all **0** on
json-as/blake/sieve/mandelbrot/memory_tree. Root cause: the corpus is
AS/binaryen-optimized output — it contains no `x+0`, `x*8`, `x==x`, and its
deferred loads are folded into a consumer before any store, so nothing piles up to
force. These peepholes defend against *naive* producers, not the measured
workloads. A prototype of P2.1 (`materializePendingLoadsBeforeStore`, sound
borrow-based same-base-disjoint predicate) was built and **reverted** — it moved no
counter and added correctness-sensitive complexity to `memStore`. Meanwhile
`cmp-branch-fuse` fires *hundreds* of times on json-as alone → the leverage is in
**P3 (stFlags)**, which extends exactly that. Revisit individual P2 items only if a
future non-AS producer (hand-written/naive wasm, a different frontend) shows the
counter is nonzero. The one item still worth a look on its own merits is
**narrow-load mask elision** (P2.3, second half) — but measure `and`-after-load8/16
frequency first.

The original P2 design, retained for when a workload justifies it:
1. **Alias-aware pending loads** (VB §6, unchanged design):
   `materializePendingLoadsBeforeStore(base Reg, disp int32, size int)` — keep
   a deferred load iff same base register and provably disjoint
   `[disp,disp+size)`. Different base regs → conservative flush. Stores pass
   the window; `memory.copy`/`fill`/`grow` keep flush-all.
2. **`drop` of side-effect-free trees** (VB §9.2): recursive
   `discardTreeIfPure` — pure ALU/compare/const/local/slot trees release with
   no emission; div/rem (trap), float→int trunc (trap), and guard-mode loads
   (the load IS the trap) still condense. Wire into `popValue`'s drop path.
3. **Const-fold pack** (`stack.go` fold table): compares, eqz, clz/ctz/popcnt,
   sign/zero extensions, wrap/extend, reinterpret-of-const; div/rem only when
   divisor is a nonzero const and the `INT_MIN / −1` case is excluded.
   Plus **narrow-load/known-zero mask elision** (landed 2026-07-17):
   `load8_u & 0xff`, `load16_u & 0xffff`, and masks already implied by constant
   shifts/bitwise trees drop the outer `and`. The same bounded facts support direct
   `TEST`/`TST` lowering for packed-word mask predicates.
4. **Same-operand int compare identities**: `x==x→1`, `x!=x→0`, `≤/≥→1`,
   `</>→0` — same-local-no-intervening-set keying as the existing `x-x→0`.
   **Int only** (NaN forbids it for floats).
5. **Store-narrowing peephole** (R3): `setcc; movzx; store8` → byte store of
   the setcc reg. Standalone version here if trivial; else it falls out of
   `stFlags` (P3) for free — don't build scaffolding twice.

Gates: spec (i32/i64/int_exprs/conversions/memory/address/align/endianness),
corpus differential both modes, ISA micro-suite before/after, new goldens.

### P3. Flags: V2 window → `stFlags` (M) — *the main near-term codegen unlock*
**Opportunity MEASURED (2026-07-04, via the new `compare-setcc` counter):** on
json-as **130** compares are materialized to a 0/1 boolean instead of fused into a
branch (vs 327 that fuse); utf-as 27, blake 8, sieve/mandelbrot/memory_tree ≤1. So
the raw 130 looked promising — **but consumer categorization refutes it.** The 130
break down (json-as) as: boolean-logic operand of `and`/`or` etc. **32**,
control-boundary flush (spilled merge value) **18**, `select` **13**, `local.set/tee`
**1**, and ~66 other (return / store / br-value). **Only `select` (~13 json-as, 5
blake, 0 utf-as) is actually flag-optimizable** — a compare feeding `i32.and`,
spilled across a merge, returned, or stored *inherently* needs a materialized 0/1
value; you cannot keep it in flags. So the real stFlags opportunity is ~13–18
occurrences (select-from-cmov), not 130. And even that needs intricate reordering
of `emitSelect` (arms are buried under `cond`, but the compare's CMP must be the
last flag-writer before the cmov) for a marginal, likely-unmeasurable gain.
`eqz`-of-compare inversion also measured **0** (binaryen pre-folds `!(a<b)`).
**Verdict: P2 and P3 are BOTH near-dead on the AS/binaryen corpus — deprioritized.**

**Strategic pivot (the important finding):** P2/P3 optimize *wasm-level* patterns,
which binaryen already pre-optimizes — that's why they're empty. **P6 (bounds facts)
is different: it optimizes bounds checks that *wago itself inserts* in explicit
mode. Binaryen never sees those (wasm has no bounds checks), so P6 is the first
lever that is genuinely wago's to win — no upstream optimizer has touched it.** P5
(call staging) is similar (ABI is wago's). Next work should target **P6 then P5**,
not the remaining P2/P3/P4 peephole/flags items. `compare-setcc` counter kept as
evidence (branch `perf/stflags`, PR #122).

Designs unchanged from VB §3–4; the review adds consumers worth listing:
1. **V2 one-deep window**: `cmp; local.set/tee $c; br_if/if` — setcc into the
   local is flag-transparent before `jcc`.
2. **V3 `stFlags` storage kind**: `{kind: stFlags, cond}` single owner
   (`f.flagsOwner`), demoted (setcc+movzx) before any flag-clobbering emission;
   clobber discipline via railshot-level emit helpers, encoder stays dumb.
3. Consumers: `br_if`/`if` (exists), `select` (cmov straight from flags),
   `local.set/tee`, `i32.store8` (subsumes P2.5), nested/double `eqz`
   (condition inversion, no materialization), `and(x,const); eqz` → `TEST`.
4. **Float compares → stFlags**: `ucomiss/ucomisd` with the unordered/parity
   fixup (NaN → JP handling per predicate) — the cond mapping exists for the
   cmov path; reuse it.

Gates: spec br_if/if/block/loop/select/local_*/f32_cmp/f64_cmp; sieve,
dispatch, branches benches (likely winners); json/blake no-regression; goldens
for each consumer; `WAGO_NO_STFLAGS=1` A/B flag.

### P4. Locals: restricted pending `local.set`/`tee` (M, decision-gated)
VB §5 restriction (c) only: pending binding iff the tree is register-free
(const/slot/localref leaves — the V1 predicate). Flush points exactly as VB §5.
Wins: dead-set elimination (incl. at returns), set→get forwarding, const/copy
forwarding. **Gate on P1 data**: count set→get-adjacent pairs and overwritten
sets per corpus module first; if the counters say <1% of instructions, park it.
Local const/copy *facts* (zero-propagation beyond init, `y=x` forwarding) only
if the restricted version measures.

### P5. Calls (M)
1. **Mixed-call parallel staging**: extend the parallel-move resolver to the
   XMM bank; replace `emitMixedRegisterCall`'s `flush()` + slot staging with
   `flushBelow(argRoots[0])` like the int path.
2. **Float `call; local.set` fusion**: XMM0 result → pinned float local
   (int-only today, `call.go:111`).
3. **Limited multi-result reg ABI**: 2 int → RAX,RDX; 2 float → XMM0,XMM1;
   mixed → RAX+XMM0. Internal calls first; pairs with a `regMerge2` for
   two-result block/if merges (review §C4). Unblocks "multi-value 🚧".
4. **Call-surviving valent trees** (VB §2, correctness table unchanged) — a
   breadth bet, NOT a serialize fix (premise refuted). Only start with P1
   counters showing call-adjacent flush traffic somewhere real (fib_rec,
   dispatch are the candidates).
5. **Tiny bytecode inliner** (review §I4) — byte-prescan classifier (≤N body
   bytes, no loops/calls/grow/branches, ≤1 result), inline by recursive
   restricted body walk. Riskiest P5 item; needs P1 call-site counters to size
   the prize first. AS getter/setter chains are the target.

### P6. Memory & bounds, explicit-mode focus (M/L)
**⚠️ MEASURED — the live lever for COMPUTE kernels, dead on AS (2026-07-03, three
count-only counters on `perf/bounds-facts`, codegen-neutral; spec 57/57,
differential both modes green). The producer matters, exactly as this plan
predicted:**
- **On the AS/binaryen corpus (json/blake/utf), P6 is near-dead:** P6.1
  (`BoundsChecksElidable`, a same-source certificate — memBytes is monotonic so a
  fact holds until the source is set or a control join) = **~7%** (json-as 42/638),
  spread thin; P6.2 (`BoundsChecksHoistable`, loop-invariant local base) = **0**.
  AS loop accesses use a computed `base+index` or an advancing pointer, and its
  redundant checks come from AS-level `ensureCapacity`+burst (a semantic guarantee
  invisible per-instruction). Same "binaryen already optimized the wasm level"
  story as P2/P3.
- **On the non-AS corpus (Rust/C compute kernels), P6 is BIG and HOT-concentrated:**
  nbody `step` alone = bounds 96 / **elidable 60 (63%)** / **hoistable 37**;
  fannkuch = 128 / 36 / **63**; raytrace = 94 / **50 (53%)** / 12; sha256 = 52 / 3
  / **16**. These access arrays through a **loop-invariant local pointer** — the
  pattern AS lacks — so both P6.1 AND P6.2 have real, hot targets. (`hoistable`
  counter verified: synthetic invariant-base loop → 1, set-in-loop → 0.) All four
  are in `TestCorpusDifferential` (the bounds-mode-desync-sensitive cases), so the
  #68-class net covers the implementation.
- **Verdict: implement P6 — it's the TinyGo/embedded/compute-kernel explicit-bounds
  win the plan claimed, just invisible on AS serialization.** Order: **P6.1** first
  (63% on nbody, hot, contained cert already built on `perf/bounds-facts`), then
  **P6.2** (now that `hoistable` is nonzero — the sound hybrid = precheck → dual
  loop body to avoid spurious 0-iteration/early-exit traps). P5 (call mechanics) is
  dead even on non-AS (`mixed`/`wrapper` = 0 everywhere) — deprioritize it.

1. **Straight-line bounds facts** (review §M3, "certificates"): after a check
   of `base+hi`, later accesses `base+[lo,hi)` in the same straight-line
   region skip the check. Same base reg + const offsets only; invalidated by
   call (callee may grow), `memory.grow`, base redefinition, any label/join.
   Explicit mode only (guard mode has nothing to elide). Counters: `BoundsChecksElided`.
2. **Hybrid loop precheck** (review §M2): `if ptr+n ≤ memBytes` → unchecked
   loop body, else checked loop. Needs the loop-header byte-scan record
   (bodyStart, touchesMemory, hasCall, hasGrow — the adopted slice of §N1).
   Big for the TinyGo/embedded (explicit-bounds) story.
3. **Store combining** (VB §7, unchanged): explicit-only, union check on the
   hot path + sequential replay on the cold trap path; guard mode excluded
   (precise-fault commit semantics). Gate hard on memory_trap/address/align.
4. **Load-after-store forwarding**: same base+disp+width within the window,
   no intervening aliasing store/call — pairs naturally with 1.
5. **CPUID probe + gated codegen**: JIT a ~20-byte CPUID stub through the
   existing trampoline at engine init (keeps zero deps, TinyGo-safe), cache a
   feature set. Then: **BMI2** `shlx/shrx/sarx` (kills the RCX round-trip,
   VB §9.4), `smallBulkMax` tuned by FSRM/ERMSB (lower when present — `rep`
   is cheap earlier; keep 96 otherwise), future paths gated the same way.

### P7. Compile path (M, premise-gated)
#96 already removed the validator's AST and 90% of its allocs — the review's
"fused validation kills the AST bottleneck" is stale. So: **re-profile
`CompileModule` first** (decode / validate / hints / railshot split, allocs
and ns). Only if standalone validation still shows up: fuse validation into
railshot's walk behind `WAGO_FUSED_VALIDATE=1` (VB §8's checks: per-op typing
against the machine stack, label arity, the polymorphic-unreachable hard 20%),
differential-fuzzed old-vs-fused on accept/reject agreement. Keep the
standalone validator as the API surface either way. Add compile-side stats
(allocs, arena high-water) to P1's stats while in here.

### P8. Runtime & product track (parallel; feature value, not exec perf)
In order:
1. **Sync host imports with results** (⭐ — the WASI unlock; runtime half
   already spiked per ARCHITECTURE §V2). Design per review §P1: host-call
   frame in basedata, HOST_CALL status back through the trampoline, Go invokes,
   writes results, resumes at continuation. Do NOT let this force wrapper-level
   conservatism on the pure-wasm call path.
2. **WASI preview 1, minimal**: fd_write, clocks, args/env, random, proc_exit.
3. **Interruption / cooperative cancel**: check at loop backedges + function
   entries; same machinery later serves Go-GC safe points (ROADMAP item).
4. **Wasm-level stack traces on trap**: trap site → func idx → wasm pc table.
5. **Remaining post-MVP semantics**: passive element execution,
   `table.get/set/size/grow/fill/copy/init`, `elem.drop` — completes the
   bulk-memory + reference-types FEATURES rows. Passive data `memory.init` /
   `data.drop` is done.
6. **`call_indirect` inline caches** (review §J): table epoch (cheap once
   `table.set/grow` exist and bump it), then monomorphic IC
   (cached idx+epoch → direct call), then a small polymorphic IC if dispatch
   benches justify. Constant-index devirtualization falls out of the epoch.
7. **`.wago` productization**: cache key = module hash + compiler version +
   CPU features + bounds mode + ABI version; `wago compile/run/inspect` CLI
   (size-gated or a separate `wagoc` binary); blob verification hardening.
8. **arm64 backend** (L) — after the above; WARP `backend/aarch64` as the
   reference axis, same port discipline as the x64 effort.

## 3. New env flags (A/B discipline)

Every risk-carrying optimization lands with a kill switch, named consistently:

| Flag | Phase |
|---|---|
| `WAGO_EXPLAIN`, `WAGO_DEBUG_MODGLOBALS`, `WAGO_PIN_GLOBAL_K` | P1 |
| `WAGO_NO_ALIAS_LOADS` | P2 |
| `WAGO_NO_STFLAGS` | P3 |
| `WAGO_NO_PENDING_SET` | P4 |
| `WAGO_NO_BOUNDS_FACTS`, `WAGO_NO_STORE_COMBINE`, `WAGO_NO_BMI2` | P6 |
| `WAGO_FUSED_VALIDATE` (opt-in, flips to opt-out when proven) | P7 |

Existing: `WAGO_BOUNDS`, `WAGO_REG_MERGE`, `WAGO_Amd64_NOREGABI`,
`WAGO_Amd64_NOSTACKREG`, `WAGO_DEBUG_PANIC`, `WAGO_JSON_MODULE`,
`WAGO_JSONPROF_ONLY`, `WAGO_SPECTEST_DIR`, `WAGO_SPEC_VERSION`.

## 4. Gate battery (updated counts — supersedes VB §11 / perf-plan §1 line 2)

1. `go test ./...` at root; `cd bench && go test ./...` — both plain and
   `-tags wago_guardpage`.
2. `WAGO_BOUNDS= go test -run TestSpecExec -count=1 -v .` → **57/57 applicable
   files, pass=16592 fail=0 skip=1591** (SPECTEST.md). Any new fail/crash is
   yours.
3. `TestCorpusDifferential` (bench, both modes) — the #68-class net; run it
   early and often, not just pre-PR.
4. Bench the touched dimension + one unrelated module, 2–3 runs (±20%
   layout-luck rule: diff disassembly before believing any single number).
5. From P1 on: the phase's golden disasm test + its CodegenStats counter
   moving in the right direction.

KNOWN: `src/wago` package tests segfault under `-tags wago_guardpage`
(pre-existing, needs its own session — see `perf-session3-plan.md` §3).

## 5. Sequencing (the review's closing advice, adjusted)

```
P0 docs (this PR + ROADMAP refresh)
→ P1 CodegenStats + explain            [the dashboard everything else reports to]
→ P2 cheap-wins batch                  [alias loads + pure drop + fold pack]
→ P3 V2 window → stFlags               [the codegen unlock]
→ P5.1–.3 call mechanics               [mixed staging, float fusion, 2-result ABI]
→ P6.1–.2 bounds facts + loop precheck [explicit/TinyGo story]
→ P4 / P5.4–.5 / P6.3–.5               [each gated on P1 counters]
→ P7 compile-path                      [premise re-measured first]
P8 runs in parallel whenever exec-perf work is blocked or a feature is wanted;
   P8.1 (sync host imports) is the single highest-value item in this file.
```

Pitfalls: VB §12 applies verbatim (branch before first edit; warp/ submodule
stays dirty; wat idx = wago idx + 1; short commit subjects; explicit merge
consent; layout-luck discipline).
