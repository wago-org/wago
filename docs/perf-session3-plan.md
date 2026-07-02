# Perf session 3 plan (2026-07-02 handoff, post B1/B2/B3)

Continuation of `docs/perf-plan-2026-07.md` — read that first for the measurement
protocol (§1), gate battery (§1), and pitfalls (§8); they all still apply. This doc
covers what changed since it was written and what to do next.

## 0. State on entry

- main = a4a3e3f (#93 stGlobReg borrowed reads). **Open PRs: #94** (B2
  immediate-only constant stores, green, ready) and **#95** (B3 diagnostic
  write-up, docs-only, stacked on #94 — auto-retargets to main when #94 merges).
- json-as guard: ser ~193–200ns / deser ~191ns. WARP 97/164, wazero 147/305.
  Serialize still ~2× WARP; deserialize done.
- **B3 verdict (read OPTIMIZATIONS.md R4 in full):** burst-global register
  residency is EXHAUSTED as an angle. wago's serializer-burst codegen now beats
  WARP's instruction-for-instruction (WARP reloads its write-pointer from
  basedata before *every* store; wago keeps it register-resident all burst with
  zero movabs), yet fn26 is still 52% of ser and the number didn't move.
  **Do not chase serialize via more burst/global codegen.** The one open
  serialize lead is call overhead (fn26 makes ~9–10 calls/invocation) — see §5.
- Corrections vs older write-ups: the K=1 module-pinned global is **global 2
  (the write pointer)**, not the AS shadow-stack pointer; K=2 already pins both
  burst globals (2 and 4). K=3's ~6–8% win comes from a third global (25,
  unidentified) at a ~7% blake cost — K=1 remains the shipped default.
- **Pre-existing, CI-invisible bug:** the `src/wago` package tests segfault
  under `-tags wago_guardpage` (`TestConfigSignalsBasedEndToEnd`,
  `TestAssemblyScriptFib`, others) while the SAME public API passes in bench's
  guard tests (TestJsonAsGuard, TestCorpusDifferential). Reproduced on clean
  main before this week's changes were applied — not caused by them, but
  never bisected. See §3.

## 1. Housekeeping (first, ~10 min)

1. Merge **#94**, then **#95** (it auto-retargets). Merging requires an explicit
   user "yes" — ask and wait for a real answer; a timed-out question is not consent.
2. `git checkout main && git pull`; re-run `TestJsonAsGuard` + one corpus exec
   pass to re-pin the baseline.
3. Commit this plan file if it is still sitting uncommitted in the tree.
4. `git stash list` has a stale `WIP on main … SPECTEST.md` entry (pass=16026
   noise from before #90) — safe to drop.
5. **Branch (`perf/<topic>`) BEFORE the first edit, always.** On 2026-07-02 an
   external checkpoint process committed mid-edit working-tree state directly to
   main; a feature branch contains that blast radius.

## 2. Priority 1 — Workstream C: float lowering parity (R2)

Exit criterion: `isa_f32`/`isa_f64` ≤1.2× wazero; min/max fixed; spec floats
green. Branch `perf/float-parity`.

**C0 — re-measure before implementing anything.** The "floats ~1.65×, min/max
~2.2×" figures predate PR #75 (read-only pinned-XMM operands) and PR #79
(3-operand VEX float ops), which may have already delivered part of C1. Baseline:

```bash
cd bench && WAGO_BOUNDS= go test -bench 'Exec/isa_f(32|64)' -benchtime 1s -count 3 -run NONE
WAGO_BOUNDS= go test -bench 'Exec/(float|mandelbrot)' -benchtime 1s -count 3 -run NONE
```

Then disassemble a small f64 accumulate loop (CompileWithConfig scratch main →
objdump, per plan §1) and list which fp ops still copy operands. Only implement
what the disassembly shows is still missing.

**C1 — in-place XMM accumulation** (S): fp.go's binary ops materialize both
operands and allocate a fresh dest. Mirror the int path (`condenseBinary`'s
`dest = left.st.reg` when left is an owned register): reuse an owned left XMM
as dest. `operandRegF` (fp.go) already gives read-only borrowing for pinned
float locals — this is about the *owned-temp* case.

**C2 — min/max via minss/maxss + fixups** (M): current lowering is branchy but
CORRECT (spec passes today) — this is perf-only, so correctness must not move.
Wasm semantics that raw minss/maxss get wrong: any-NaN → canonical NaN out;
min(+0,−0) = −0 / max(−0,+0) = +0. **Port WARP's reference lowering**:
`warp/src/core/compiler/backend/x86_64/x86_64_backend.cpp`, search `MIN` /
`emitFloatMinMax`. Gate on spec `f32`, `f64`, `float_misc`, `float_exprs`
(f32/f64 are 2500+ assertions each — they are the real net here).

**C3 — float deferred loads / r/m folding** (M, only if C0–C2 leave a gap):
mirror `stMemRef` for floats — `fload` pushes a bounds-checked-but-deferred
float ref; fp ALU emitters fold `addss xmm, [rbx+ea+disp]`. Encoder needs an
SSE mem-operand form (`SseIdx(prefix, op, xmm, base, index, disp)`;
`FLoadIdx`/`FStoreIdx` in asm_sse.go are the encoding pattern). Must hook
`materializePendingLoads` (load-before-store ordering). Keep v1 simple: owned
address registers only — skip the pinned-local `memBorrow` extension.

**C4 — verify float pinned locals in STACK_REG laziness** (S, verification
only): the `isFloat` paths exist in localstate.go; confirm with a
float-call-function disassembly + microbench that dirty-only spill / lazy
reload actually fires for XMM pins.

Per-commit gates: full battery (§6 below). Watch mandelbrot + float.wasm for
the win, blake for XMM-pool side effects, and one int module for accidents.

## 3. Priority 2 — root-cause the guard-mode segfault

(Promote above floats if correctness > perf this week — it is a real bug and CI
cannot see it.)

Repro: `go test -tags wago_guardpage -run TestConfigSignalsBasedEndToEnd ./src/wago/...`
(smallest case: 1-page memory, i32.load, OOB → expected SIGSEGV-trap; crashes
instead). `TestAssemblyScriptFib` and others in the same package also crash,
while bench's guard tests exercise the same public API fine — so suspect the
test binary/environment interaction (signal-handler install timing, test
ordering, a module shape bench never builds), not wholesale guard breakage.

1. **Bisect main** — the repro is seconds-cheap. First establish whether these
   tests EVER passed under the tag (config_guardpage_test.go's own history);
   candidate breakers: #90 (trap cell → basedata, no per-call trap protocol),
   #87/#88 (ABI rework), or it may predate all of this.
2. Get a real stack: `go test -c -tags wago_guardpage ./src/wago && gdb ./wago.test`,
   plus the perf-map trick to attribute a wasmfuncN IP. Distinguish "fault
   before handler installed" from "fault inside handler".
3. Fix, or write up precisely. Then **add a guard-tag test job to CI**
   (Makefile target + ci.yml step) so this class can't go invisible again.

## 4. Priority 3 — Workstream D: constrained compare fusion (R1)

Only after C lands; unchanged from `docs/perf-plan-2026-07.md` §5. Start with
`cmp; local.tee $c; br_if` only (in `setLocal`, peek the Reader the way
`callOp`'s fusion does; `mov`/`setcc` don't clobber flags; fuse only when
everything below is already concrete, or flush first as `brIfFused` does).
Then `eqz; local.set; local.get; if`. Gate: spec `br_if`/`if`/`block`/`loop` +
branches.wasm + json/blake unchanged.

## 5. Then the E-gate (decision, not code)

Re-measure the §0 table of `docs/perf-plan-2026-07.md`. Expected outcome: ser
still ≥1.5× WARP → per the agreed rule the remaining loss is cross-block
regalloc/scheduling, i.e. Tier-2 SSA territory. **Before committing to the
spike**, spend ≤1h on the B3 lead: measure fn26's call overhead — count calls
per invocation, A/B `WAGO_Amd64_NOREGABI=1`, and try again to read WARP's
ensure-capacity callee (blob offset ~0x484 — linear objdump desyncs there; use
function-boundary recovery or perf-annotate on the memfd mapping). If calls
explain <20% of the gap, scope the SSA spike per §6 of the original plan (ONE
function, linear-scan, env-gated hot-function whitelist, 2–3 sessions, never
blended into railshot).

## 6. Gate battery (updated)

1. `go test ./...` at root; `cd bench && go test ./...` **both with and without**
   `-tags wago_guardpage`.
2. `WAGO_BOUNDS= go test -run TestSpecExec -count=1 -v .` → exactly
   pass=16500 fail=13 skip=1672; only `linking` FAIL + `data` BLOCKED.
3. `TestCorpusDifferential` (bench, `-tags wago_guardpage`) — the #68-class net,
   now a real test.
4. Bench the touched dimension + one unrelated module, 2–3 runs (±20% layout luck).

KNOWN: the `src/wago` package under the guard tag segfaults (§3, pre-existing).
Until fixed, validate guard-mode behavior through the bench module's guard
tests; don't let the known crash block unrelated PRs — but don't use it to
excuse NEW guard failures either (diff against the known-crashing test list).

## 7. Process notes (hard-won this week)

- Feature branch before the first edit. PRs for everything, including docs.
- Short commit subjects, no bodies (repo convention).
- Merges need an explicit user yes. The auto-mode classifier blocks self-merges,
  correctly.
- The `warp/` submodule is intentionally dirty — never stage or reset it.
- wat function index = local index + 1 (`env.abort` import).
- Re-run any surprising bench result 2–3× before believing it, then diff
  disassembly in both directions.
