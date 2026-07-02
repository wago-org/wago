# Valent-block expansion plan (Workstream V, 2026-07-02 handoff)

Plan for generalizing railshot's valent-block model beyond WARP and the paper.
Written for a fresh session with no conversation context. Read
`docs/perf-plan-2026-07.md` §1 (measurement protocol) and §8 (pitfalls) first —
they all still apply — then this doc. `docs/perf-session3-plan.md` is the
sibling plan for float parity / guard segfault; see §0.3 below for how the two
tracks interact.

**The paper:** Scheidl, "Valent-Blocks: Scalable High-Performance Compilation of
WebAssembly Bytecode For Embedded Systems" (IEEE 2020) — in-repo copy at
`warp/docs/paper/valent-blocks.pdf`. Its core move (§III-D-1): classify wasm
instructions as side-effect-free (recordable as deferred expression trees =
valent blocks) vs side-effecting (**sinks** that force LIFO condensation).
Railshot already matches or exceeds the paper on the pure-expression side
(deferred trees, const fold/strength-reduce, cmp→branch fusion, cmov select,
deferred loads with r/m folding — the paper has no deferred loads at all).

**The organizing idea of this plan:** the remaining power is in *shrinking the
set of hard sinks*. Calls, local.sets, stores, and block labels all force full
condensation today; each one we soften widens the fusion window. Plus one
non-codegen use the paper promises that we never ported: concurrent validation
(§III-D-4), which can kill the decode/validate AST pass — the known
compile-latency bottleneck.

## 0. State on entry

### 0.1 Repo / branch state (as of 2026-07-02)

- PRs #94 (immediate-only const stores) and #95 (B3 findings doc) are **merged**
  (`e784bb7`, `adf48eb`). The session-3 plan's "merge #94/#95" housekeeping is done.
- **The working tree may contain uncommitted Workstream-C (float parity) WIP** on
  branch `perf/float-parity`: modified `driver.go`, `exec_test.go`, `fp.go`,
  `regalloc.go`, `stack.go` (railshot) and `asm_sse.go` (encoder). This is a
  different workstream's in-progress state. **Do not discard, commit, or stash
  it without asking the user.** Ask whether to (a) finish/land the float work
  first, or (b) park it (commit as WIP on its branch) and start Workstream V
  from a fresh branch off up-to-date `main`.
- Commit this plan file via its own docs PR if it is still untracked.
- Every phase below: its own branch `perf/vb-<topic>` off `main`, own PR, full
  gate battery (§8). Branch **before** the first edit. Merges need an explicit
  user "yes".

### 0.2 Perf state (json-as, guard mode)

ser ~193–200ns / deser ~191ns per unit. WARP 97/164, wazero 147/305. Deserialize
is done (1.43× faster than wazero); **serialize is the open front, still ~2×
WARP**, and fn26 (wat 27, the serializer) = 52% of ser profile. Burst-global
register residency is EXHAUSTED as an angle (see OPTIMIZATIONS.md R4 — wago's
burst codegen already beats WARP's instruction-for-instruction). The one open
serialize lead: **fn26 makes ~9–10 calls per invocation** and call overhead was
never measured. Phase V0 measures it; Phase V1 is the corresponding fix shape.

### 0.3 Relationship to existing plans

- This plan **supersedes Workstream D** (`docs/perf-plan-2026-07.md` §5,
  `perf-session3-plan.md` §4): V2 delivers D's target patterns; V3 generalizes.
- Phase V0 **is** the E-gate prework that `perf-session3-plan.md` §5 calls for
  ("spend ≤1h on the B3 lead" before scoping the SSA spike). Do V0 before any
  SSA decision.
- Strategic frame: V1–V4 together are a "Tier 1.5" — a meaningful chunk of what
  Tier-2 SSA would buy (cross-sink value survival), incremental and per-phase
  gated, reusing proven machinery. Re-run the E-gate table after V1+V2 land; the
  SSA spike may shrink or die.

### 0.4 Code map (orient here first)

| Where | What |
|---|---|
| `railshot/stack.go` | `elem`/`storage` model, `pushBinOp`, const fold, `baseOfValentBlock` |
| `railshot/emit.go` | the condense engine (materialize/condense/selectInstr) |
| `railshot/fuse.go` | `flushBelow` (per-root selective flush — the template for V1), `condenseToFlags`, `brIfFused` |
| `railshot/control.go` | `flush`/`setDepth` (all-or-nothing, frees ALL regs), `convergeEdgeTo`, `regMerge1` |
| `railshot/call.go` | `emitRegisterCallVia` (arg staging, `flushBelow(argRoots[0])`/`flush()` at :236-238), `spillLocalsForCall` |
| `railshot/regalloc.go:224` | `materializePendingLoads` — flushes ALL deferred loads at ANY store, no alias reasoning |
| `railshot/localstate.go` | pinned-local lsReg/lsMem/lsStackReg (STACK_REG laziness) |
| `railshot/globals.go` | `stGlobReg` borrowed global reads, module pinning (K=1) |
| `railshot/driver.go:63` | `drop` — condenses a deferred tree just to discard it |
| `railshot/hints.go` | prepass AST walks incl. `localHotness`/`globalHotness` |

Key ABI fact for V1: **no GP register value survives a wasm→wasm call.** wago
wasm functions do not save/restore callee-saved regs (`cc.go`: pinned locals are
"clobbered by wasm callees"; RBP "not preserved across wasm calls"). Only
RSP and RBX (linMem, re-established by every prologue) are stable; R15
(memBytes cache) is preserved by construction, not as a value holder.

## 1. Phase V0 — measure fn26 call overhead (S, measurement only)

Exit: a number — what fraction of fn26 self+child time is call overhead
(arg staging, `flushBelow`/`flush` slot traffic, `spillLocalsForCall` spills,
post-call slot reloads, wrapper vs reg-ABI entry).

1. Re-pin the baseline (`TestJsonAsGuard` + one corpus pass, 2–3 runs).
2. Disassemble fn26 (`DirectBackend.CompileModule` → objdump, protocol in
   `perf-plan-2026-07.md` §1). Count, per call site: `mov [rsp+off],r` stores
   before / `mov r,[rsp+off]` loads after, arg-staging movs, wrapper adapters.
   Remember: wat function index = wago local index + 1 (`env.abort` import).
3. A/B `WAGO_Amd64_NOREGABI=1` to split reg-ABI vs wrapper cost.
4. `perf record` on `bench/cmd/jsonprof` (perf-map offsets map 1:1 to the memfd
   blob) — attribute time to the call-adjacent instruction clusters.
5. Also note WHICH callees fn26 hits (ensure-capacity/`__renew` chain) and
   whether any are wrapper-only for a fixable reason.

Decision rule: call overhead ≥20% of fn26 → V1 is the serialize lever, do it
next with high expectation. <20% → still do V1 (it is broad: every
expression-context call in every module) but expect the ser gap to be
cross-block scheduling → raise E-gate priority after V1/V2.

## 2. Phase V1 — valent blocks that survive calls (M)

**What:** stop flushing call-invariant operand trees at call sites. Today
`emitRegisterCallVia` does `flushBelow(argRoots[0])` (or `flush()` for p=0),
dumping every root below the args to canonical slots; `(a + f(x)) * b` pays a
slot store + reload for `a` around the call.

**Correctness model.** A callee can write **globals** and **memory**, and every
GP register is clobbered — but it can never touch caller **locals**, and frame
**slots** are caller-private. Therefore a deferred tree survives a call iff all
its leaves are, after conversion:

- `stConst`, `stSlot`, `stLocalRef` — survive as-is;
- `stLocalReg` (borrowed pinned read) — **convert to `stLocalRef`**: valid
  because `spillLocalsForCall` stores dirty pins before the call, making the
  frame slot authoritative; the post-call lazy-reload model (lsMem) is
  untouched;
- `stReg` (owned temp), `stMemRef` (deferred load; must read pre-call memory),
  `stGlobalRef`/`stGlobReg` (callee may write the global; must capture the
  pre-call value) — tree does NOT survive; flush that root to its canonical
  slot as today.

**How:**

1. `callInvariantTree(e *elem) bool` — recursive walk over `arg0/arg1`, leaf
   check per the table above.
2. `flushForCall(argBase *elem)` modeled on `fuse.go`'s `flushBelow` (which
   already does per-root `replaceStorage` without resetting the stack model):
   for each root below the args, either rewrite its `stLocalReg` leaves to
   `stLocalRef` and leave it deferred, or flush it to position-slot `i`.
   Keep `invalidateGlobalsCache()`. Slot numbering stays position-indexed, so a
   later full `flush()` at a control boundary still assigns consistently.
3. **Do not call `setDepth`** on this path — `setDepth` zeroes `regUser`/
   `pinned` wholesale (`control.go:108`). Surviving trees hold no registers
   (by construction), so after flushing the non-invariant roots, all operand
   registers are free exactly as before; verify `f.refs` occurrence-chain
   entries for flushed elems are cleaned the way `flushBelow` does.
4. Wire into `emitRegisterCallVia` and the wrapper-call path (read all of
   `call.go` first). v1 scope: leave `call_indirect`'s wrapper path, host
   calls, and bulk-memory helper emission (`memory.go`) on full flush; extend
   in a follow-up once green.
5. Order constraint: pending deferred loads must still materialize before the
   call if the tree is flushed anyway (`materialize` on `stMemRef` does this);
   surviving trees cannot contain `stMemRef` at all.

**Gates:** full battery (§8) + benches: json ser/deser (the target), fib
(recursive, call-dense), dispatch (call_indirect), blake (regression watch),
`TestCorpusDifferential` both modes. This is beyond-WARP territory — the
differential corpus test is the #68-class net; treat it as load-bearing.

**Exit:** json ser improvement measured (V0 predicts the ceiling); no
regression elsewhere; disasm of one expression-context call site shows the
surviving operand's store/reload pair gone.

## 3. Phase V2 — compare fusion past adjacency (Workstream D, reframed) (M)

**What:** `cmp; local.tee $c; br_if` and `eqz; local.set; local.get; if`
currently break fusion because the set/tee materializes a setcc between the
compare and the branch. Implement with a **one-deep deferred-set window**
instead of ad-hoc Reader peeking:

1. `local.set`/`local.tee` of a **fusable compare node on top of stack**
   (`isFusableCompare`, fuse.go) does not condense: record
   `f.pendingSetLocal = x` / keep the compare node, mark the tee's pushed-back
   value as reading the pending result.
2. Window closes at the very next instruction:
   - `br_if`/`if`: `flushBelow` → `condenseToFlags` (existing) → **`setcc` the
     pending local before the `jcc`** — setcc and the mov into the pinned reg /
     frame slot are flag-transparent, so the fusion survives. This is exactly
     the sequencing `perf-session3-plan.md` §4 sketched; the pending-set slot
     just makes it uniform for set and tee.
   - anything else: materialize setcc → complete the set as today (no win, no
     loss).
3. Fuse only when everything below the compare is already concrete, or flush
   first exactly as `brIfFused` does — no flag-clobbering emission may land in
   the window.

**Gates:** spec `br_if`/`if`/`block`/`loop`/`local_*` + branches.wasm; json +
blake unchanged or better; sieve and dispatch (branchy) are the likely winners
along with deser's TLSF/GC path.

## 4. Phase V3 — flags as a storage kind (`stFlags`) (M, after V2)

**What:** formalize V2's window: a compare's result may live in EFLAGS as
first-class storage `{kind: stFlags, cond}` with a single owner slot
(`f.flagsOwner *elem`). Any emission outside a whitelist (mov, lea, setcc,
FMov, plain loads/stores, jcc) demotes it: emit `setcc + movzx` into an
allocated register *before* the clobbering instruction. Every consumer —
`br_if`, `if`, `select`, nested `eqz` chains, `and`+`eqz` — then gets flag
forwarding without per-pattern code, and V2's special cases collapse into it.

Implementation note: don't try to annotate every encoder method. Only track
clobbers while `flagsOwner != nil` (rare), and route emission through a small
set of railshot-level helpers that call `f.clobberFlags()` first. Audit
`emit.go`'s emitters once; the encoder (`Asm`) stays dumb.

Include here (small adds, same flag machinery): `and(x, const); eqz` →
`TEST r, imm` fold; the `store8`-of-setcc movzx peephole (backlog item 9).

## 5. Phase V4 — general deferred `local.set` (L, decision-gated)

**What:** generalize V2 to arbitrary values: `local.set` records a pending
binding instead of materializing. Wins: dead-set elimination (set overwritten
before any get/merge — free DSE at returns since locals die), set→get
forwarding without a register round trip, and pending sets legally float past
**traps** (locals are function-private and unobservable after unwind — a
pending set does not constrain bounds checks, div traps, or stores).

Flush points (the complete list): a later `local.get`/`local.set` of the same
local (get: forward or materialize; set: previous binding **dies** = DSE),
control-flow merges (`flush`/`convergeEdgeTo`), calls (register liveness only —
the callee can't read the local, but the binding's tree must go through V1's
`flushForCall` logic; materialize before `spillLocalsForCall`), function
end/return (die).

**The central design decision (resolve before coding):** where does the pending
tree live? The register allocator finds spill victims by walking the physical
stack; an off-stack tree with owned registers would be invisible (leak /
unspillable). Options: (a) keep the node ON the physical stack as a new
`elemKind` that `depth()`/`rootsBottomToTop()` skip for operand positioning —
invasive for every stack walker; (b) off-stack tree but `regUser` entries keep
pointing at its elems and the spill path learns one extra scan; (c) v1
restriction: defer only bindings whose tree is register-free (const / slot /
localref leaves — the V1 predicate again), so the question vanishes. **Start
with (c)**; it already covers DSE and const/copy forwarding, and measure before
paying for (a)/(b).

Do V4 only if V2+V3 measurements suggest more is on the table (deser TLSF/GC
path, dispatch); otherwise skip to V6/V7.

## 6. Phase V5 — alias-aware pending loads (S — do early, can ride any lull)

`materializePendingLoads` (`regalloc.go:224`) forces **every** deferred load at
**every** store with zero alias reasoning. Make it
`materializePendingLoads(base Reg, disp int32, size int)`: keep a deferred load
iff it uses the **same base register** and a provably disjoint
`[disp, disp+size)` window (different base regs might alias → materialize,
conservative). Borrowed-address safety is already handled: a `local.set` of a
borrowed address local triggers `realizeLocalRefs`.

Effect: loads keep folding as r/m operands through store bursts. Watch: memory
ordering is wasm-single-threaded, so same-address load/store ordering is the
only constraint — the disjointness check IS the proof. Gates: spec `memory`,
`address`, `align`, `endianness`; corpus differential both modes.

## 7. Phase V6 — store combining (M/L, optional, careful)

One-instruction deferred-store window: on a store, if the previous instruction
was also a store with the same base reg and contiguous/overlapping window —
merge (two i32 → one i64; overwritten pending store dies). AS struct-init and
copy lowering emit exactly these runs.

**Trap semantics are the whole difficulty.** Wasm requires: if store A is in
bounds and store B traps, A's write is committed and observable after the trap
(spec tests read memory post-trap). A merged store that traps commits neither.
Handling:

- **Explicit-bounds mode:** hot path checks the union extent once and emits the
  merged store; the **cold trap-stub path replays the stores sequentially**
  (check A, store A, check B, trap). Correct and fast.
- **Guard mode:** x86 faults are architecturally precise (a faulting store
  commits nothing), so a merged store near the memory boundary would lose A's
  committed write with no cold path to save it. Either (a) don't merge in guard
  mode, or (b) merge only when the merged access provably cannot fault
  partially — there is no static alignment guarantee in wasm, so (a) is the
  honest v1. Ironically this optimization is for **explicit mode** — which is
  the TinyGo/embedded story, the paper's own target domain.

Gate hard on spec `memory_trap`, `address`, `align` + corpus differential. If
the win on real modules is small (json bursts are already i64-wide), park it —
this phase is the most delicate for the least certain payoff.

## 8. Track V7 — concurrent validation (parallel track; compile latency, not exec)

The paper's §III-D-4: validation rides the compile-time stack. wago still runs
decode → validate → compile as separate passes, and the transient 424B/instr
Instruction AST is the known compile-path bottleneck — while railshot itself
compiles straight from the byte `Reader`. If railshot's traversal validated as
it goes, the AST pass could leave the compile path entirely.

1. **Verify the premise first** (S): profile `CompileModule`; confirm what
   still builds the AST (validator? hints.go prepass?) and what fraction of
   compile time it is. If hints.go also needs the AST, count that.
2. Scope the checks railshot must add: value-type agreement per op (the stack
   already carries `machineType`), label arity, unreachable-code validation
   (subtle: wasm validates dead code polymorphically; railshot's
   `skipImmediates` path currently skips without checking — this is the
   hardest 20%).
3. Keep the standalone validator as the API surface and for non-compiling
   paths; railshot-fused validation replaces it only on the compile path,
   behind a flag until the gate below is green.
4. **Gate:** the spec suite's `assert_invalid`/`assert_malformed` corpus is the
   real net here, plus the 15,106-assertion exec suite unchanged. The validator
   is the security boundary — a differential fuzz run (old validator vs fused,
   accept/reject agreement on mutated modules) is cheap insurance and worth the
   afternoon.

Payoff: compile-latency headline (streaming compile is wago's pitch vs wazero),
plus RAM-during-compile — the paper's own embedded argument.

## 9. Quick wins (S each — batch into one or two PRs, interleave anywhere)

1. **Per-module global-pin K:** B1 shipped K=1 globally because K=3 helps json
   (~6–8%) but hurts blake (~7%). `globalHotness` (hints.go) already exists —
   pick K per module (e.g. pin globals whose loop-access score clears a
   threshold, cap by pool pressure). Un-ships a known compromise. Gate: json
   AND blake both at their best-K numbers.
2. **`drop` of a deferred tree** (`driver.go:63`): today `popValue` condenses
   the tree just to discard it. Add a recursive discard (release regs/memRefs;
   keep guard-mode loads — the load IS the trap, the existing stMemRef case
   shows the pattern).
3. **LEA tree patterns** (with the planned scaled-index work, backlog item 3):
   `add(x, shl(y, k≤3))` → `lea [x+y*2ᵏ]` in plain arithmetic (not just
   addresses); mul by 3/5/9 → lea.
4. **BMI2 `shlx`/`shrx`/`sarx`** where CPUID allows: kills the RCX
   fixed-register round-trip on variable shifts. json-as SWAR code is
   shift-heavy; RCX pressure is real. Needs encoder additions + a cached
   feature probe; emit the legacy form when absent.

## 10. Recommended order

```
housekeeping (float-WIP disposition, commit this doc)
→ V0 (measure fn26 calls)            [S, decision gate]
→ V1 (call-surviving VBs)            [M, the strategic bet]
→ QW batch (§9) + V5 as filler       [S]
→ V2 (compare fusion / Workstream D) [M]
→ V3 (stFlags)                       [M]
→ E-gate re-measure (perf-plan-2026-07.md §0 table) — SSA decision here
→ V4 / V6 only if the E-gate says the remaining gap is theirs
→ V7 runs as an independent track whenever exec-perf work is blocked
```

## 11. Gate battery (unchanged from perf-session3-plan.md §6)

1. `go test ./...` at root; `cd bench && go test ./...` — both with and without
   `-tags wago_guardpage`.
2. `WAGO_BOUNDS= go test -run TestSpecExec -count=1 -v .` → exactly pass=16500
   fail=13 skip=1672; only `linking` FAIL + `data` BLOCKED.
3. `TestCorpusDifferential` (bench, `-tags wago_guardpage`).
4. Bench the touched dimension + one unrelated module, 2–3 runs.

KNOWN: `src/wago` package tests segfault under `-tags wago_guardpage`
(pre-existing, CI-invisible — see `perf-session3-plan.md` §3). Don't let it
block PRs; don't let it excuse NEW guard failures (diff against the known list).

## 12. Pitfalls (hard-won; repeated because they keep biting)

- Tight-loop corpus benches swing ±20% on pure code layout. Re-run 2–3×, then
  diff disassembly in both directions before believing any single number.
  Loop-top `Align16` exists; internal-entry alignment too.
- Branch (`perf/vb-<topic>`) BEFORE the first edit — an external checkpoint
  process has committed working-tree state to main mid-session before.
- The `warp/` submodule is intentionally dirty — never stage or reset it.
- wat function index = wago local index + 1 (`env.abort` import).
- Short commit subjects, no bodies. PRs for everything, docs included.
- Merges need an explicit user "yes"; a timed-out question is not consent.
- V1/V4/V6 are beyond-WARP — there is no reference implementation to diff
  against. The explicit-vs-guard differential corpus test is the safety net
  that caught #68-class bugs; run it early and often, not just pre-PR.
