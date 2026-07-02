# Perf continuation plan (2026-07-02 handoff)

Execution plan for the next session(s), written at the end of the #87/#88/#89 sweep.
Everything here is grounded in measurements from 2026-07-02 on the dev machine; re-verify
the baseline table first — numbers drift with machine state, and tight loops swing ±20%
on code-layout luck alone.

## 0. Context and current state

Three PRs form the lineage (read their descriptions first):
- **#87** (merged): WARP regalloc parity — memSize in R15, STACK_REG everywhere
  (root-caused #68), full pin pool.
- **#88** (pending): backlog sweep — cold trap stubs, strength reduction, scaled-index
  LEA, lazy merge agreement, br_table jump tables, reg-ABI call_indirect, const
  bulk-mem unroll, leaf fence skip, one-pass `scanBody`, guard-mode default.
- **#89** (pending, stacked on #88): WARP head-to-head replication — no per-call
  environment protocol, borrowed load addresses, module-wide global pinning, inline
  chunk-loop memmove for small dynamic copies.

**First task: land #88 then #89** (rebase if main moved, re-run the full gate battery
per PR, merge). Everything below assumes both are on main.

### Baseline to verify (json-as, ns per serialize/deserialize unit)

| engine | ser | deser |
|---|---:|---:|
| WARP (passive bounds) | 97 | 164 |
| wazero v1.9 | 141 | 293 |
| wago guard | **189** | **185** |
| wago explicit | 197 | 204 |

Corpus (explicit): fib_rec(28) 1.44ms · sieve(50000) 95µs · memory_tree(8,24) 11.6µs ·
linked_list(4096) 9.4µs · blake 700µs · utf 196µs · dispatch 17.2ns · memory.sum 230ns
(== guard). wago beats wazero everywhere except json **serialize** (0.72×) and blake.

**The headline chase: serialize 189 vs WARP 97.** Deserialize is done (1.13× of WARP).

## 1. Measurement protocol (use for every item)

```bash
# json-as (explicit + wazero comparison; module at ~/Code/AssemblyScript/json-as/build/wago-bench.swar.wasm)
cd bench && go test -run TestJsonAsBench -count=1 -v
# json-as guard vs explicit
go test -tags wago_guardpage -run 'TestJsonAsGuard$' -count=1 -v
# corpus exec (manifest args; run 2-3x, tight loops are layout-sensitive)
WAGO_BOUNDS= go test -bench 'Exec/(sieve|memory_tree|blake-as|utf-as|fib_rec|linked_list|dispatch)' -benchtime 1s -count=1 -run NONE

# WARP reference (bench main is locally patched — the warp/ submodule is intentionally dirty; do NOT reset it)
warp/build-bench/bin/vb_bench $HOME/Code/AssemblyScript/json-as/build/wago-bench.swar.wasm serializeN 256
#   per-unit = exec_ns / 256; rebuild: cmake --build warp/build-bench --target vb_bench -j
#   WARP compiles with LINEAR_MEMORY_BOUNDS_CHECKS=0 on Linux (passive) → compare against wago GUARD mode.

# per-function profile (wago)
cd bench && go build -tags wago_guardpage -o /tmp/jp ./cmd/jsonprof
WAGO_JSONPROF_ONLY=ser perf record -F 4000 -o /tmp/p.data -- /tmp/jp 10s guard
perf report -i /tmp/p.data --stdio | grep wasmfunc | head
# per-function profile (WARP): perf on vb_bench; the JIT runs from a memfd mapping whose
# file-relative offsets == compiled-blob offsets. The patched bench dumps the blob to
# /tmp/warp-json.bin; bucket the perf IPs and objdump the blob at hot regions.
```

Disassembly dumps: compile via `wago.CompileWithConfig` in a scratch main, write
`c.Code` to a file, `objdump -D -b binary -m i386:x86-64 -M intel`; `c.Entry[i]` gives
function offsets. (A guard-mode dump needs `-tags wago_guardpage`.)

### Gate battery (every change, no exceptions)
1. `go test ./...`
2. `WAGO_BOUNDS= go test -run TestSpecExec -count=1 -v .` — expect exactly the
   pre-existing failures: `linking` FAIL (unsupported import shapes) and `data` BLOCKED.
   Anything else is yours.
3. Explicit-vs-guard differential on the real corpus (blake `hashN(100)` =
   -1321215924, utf `convertN(200)` = 819200, json `serializeN(64)` = 6912 /
   `deserializeN(64)` = 542208, memory_tree `run(6,4)` = 252796408, sieve
   `count(50000)` = 5133). **Item 2.1 below makes this a permanent test — do that
   first.**
4. Bench the touched dimension + one unrelated module (watch for pool-pressure and
   layout side effects).

## 2. Workstream A — housekeeping (do first, ~1h)

### 2.1 Promote the differential corpus runner into a real test
The explicit-vs-guard differential (gate 3) currently lives in a scratchpad. Add
`bench/corpus_differential_test.go` (build tag `wago_guardpage`): for each corpus
module above, compile+run under `BoundsChecksExplicit` and `BoundsChecksSignalsBased`,
assert identical results (and equal to the golden constants). This is the #68-class
regression net; it has caught every real miscompile this week.

### 2.2 Wire jsonprof's `WAGO_JSONPROF_ONLY` into docs
Already implemented (env: `ser`/`deser`/unset). Just make sure it survives.

## 3. Workstream B — the serialize gap (R4, the headline)

52% of ser is local fn 26 (**wat index 27** — wat = local + 1 because of the
`env.abort` import!), 16% fn 40 (wat 41). wat 27 is a serializer specialization whose
body is bursts of:

```
global.get 4 ; +170 ; global.set 4 ; global.get 4 ; i32.const 0 ; call 3   ← ensure-capacity
global.get 2 ; i64.const 0x75...   ; i64.store            ← write 8 ascii chars
global.get 2 ; i64.const ...       ; i64.store offset=8
global.get 2 ; i64.const ...       ; i64.store offset=16  (etc.)
global.get 2 ; i32.const 34 ; i32.add ; global.set 2      ← bump write pointer
```

Global 2 = output write pointer, global 4 = capacity watermark, global 25 =
AS shadow-stack pointer (already module-pinned in R14 by #89).

Attack in this order; measure after each step:

### B1. Borrowed reads for module-pinned globals (S–M, high confidence)
Module-pinning globals 2/4 (K=2/3) measured FLAT in #89 — almost certainly because
`globalGet` for a pinned global still does `allocReg + mov` (copy-out,
`globals.go:~68`), so each burst line still pays mov+movabs+store. Make pinned-global
reads borrowable like pinned locals:
- Add a storage kind `stGlobReg` (mirror `stLocalReg`: `reg` = the module reg, `idx` =
  global index). `globalGet` on a module-pinned global pushes it borrowed — no copy.
- Extend `materializeRead` (regalloc.go) to return it in place; audit the ~30
  `stLocalReg` consumer sites (applyALU/applyMul/condenseCompare/condenseToFlags
  read-only cases, flush/flushBelow store-to-slot cases, `leaRightOK`/`emitLeaAdd`,
  `tryLeaScaledAdd`) and add `stGlobReg` where semantics are read-only. Skip the
  deferred-load `memBorrow` extension initially (loads via globals are rarer; memAddr's
  `materializeRead` call will just copy for `stGlobReg` unless taught otherwise).
- **Realize-on-set**: mirror `realizeLocalRefs` — before `globalSet` overwrites a
  module-pinned global's register, materialize outstanding `stGlobReg` (and
  deferred-tree refs) for that global. Also materialize them in `flush()` (they store
  to canonical slots fine — copy the reg like the `stLocalReg` case).
- Then re-run the K sweep (`moduleGlobalRegs` in compile.go): K=1 {R14} vs K=2
  {R14,R13} vs K=3. With borrowed reads, K=2/3 should finally move ser. Watch blake
  (39-local functions lose pool registers; it regressed ~3.5% at K=2, ~7% at K=3
  WITHOUT borrowed reads — re-judge with them). RBP must never be a module-global reg
  (it's `mergeReg` — K=4 with RBP corrupted memory in testing).

### B2. Constant-store splitting (S, independent, module-wide win)
`i64.const <imm>; i64.store` currently materializes a 10-byte movabs into a register
then stores. Lower constant stores immediate-only:
- i64 const store → two `mov dword [rbx+ea+disp], imm32` (C7 /0 with SIB). Needs an
  encoder op `StoreImmIdx(base, index Reg, disp int32, imm int32)`; StoreImm32Mem
  exists for the reg-base form (pattern to copy).
- i32/i16/i8 const stores → single imm store of matching width (C7/C6 + 66 prefix).
- Hook in `memStore` (memory.go): peek the value elem for `stConst` before
  materializeRead. Also `memoryFillConst` already computes patterns at compile time —
  unrelated.
- This kills a register + a dependency per JSON literal write, everywhere.

### B3. Read WARP's wat-27 codegen (M, diagnosis before more work)
Regenerate `/tmp/warp-json.bin` (run vb_bench once), profile WARP ser, find the wat-27
region (search the disasm for the distinctive burst constants, e.g. movabs of
`0x75...` / decimal 32932988889202811), and diff instruction-by-instruction against
wago's fn26 dump. Answer specifically:
- How many instructions per `global.get 2; i64.store` line does WARP emit?
- What does WARP's ensure-capacity call site look like vs wago's (wat func 3 — check
  whether wago gives it the reg-ABI or wrapper path; if wrapper, why)?
- Does WARP keep global 4's watermark arithmetic entirely in a register?
Write findings into OPTIMIZATIONS.md R4 before implementing anything further.

### B4. If B1–B3 close to ≤1.3× of WARP, stop here. Otherwise:
- Consider burst write-combining: two adjacent i64 const stores at disp/disp+8 →
  one 16-byte SSE store (`movups xmm, [rip-const]` needs a literal pool — bigger
  machinery; only if B3 shows WARP doing something equivalent, which is unlikely
  since WARP has a SIMD TODO).
- The fallback lever is the Tier-2 SSA question (§6).

Expected outcome for Workstream B: ser 189 → 140–160.

## 4. Workstream C — float lowering parity (R2) (M, mechanical)

ISA-suite lags (bench/corpus/isa_f32|f64 via `BenchmarkExec`): floats ~1.65×,
min/max ~2.2× vs wazero.

- **In-place XMM accumulation**: fp.go's binary ops materialize both operands and
  allocate a fresh dest. Mirror the int path (`condenseBinary`'s
  `dest = left.st.reg` when left is an owned reg): reuse an owned left XMM as dest.
- **r/m folding for float ops**: `addss xmm, [rbx+ea+disp]` — the deferred-load
  concept for floats. fp loads are NOT deferred today (they emit at `fload`); the
  cheap version is a peephole inside fp binary ops when the right operand elem is a
  just-pushed float load... simpler: teach `fload` to push a float memRef mirroring
  `stMemRef` (bounds check emitted, load deferred) and fold in the fp ALU emitters.
  Encoder needs SSE mem-operand forms (`SseIdx(prefix, op, xmm, base, index, disp)`).
- **min/max via minss/maxss + fixups**: wasm semantics (NaN → canonical NaN;
  min(+0,-0) = -0) don't match raw minss. **WARP has the reference lowering** — read
  `warp/src/core/compiler/backend/x86_64/x86_64_backend.cpp` (search `MIN`/`emitFloatMinMax`)
  and port it. Gate on spec `f32`/`f64`/`float_misc` files (2500+ assertions each).
- Verify float pinned locals participate in STACK_REG laziness (they should — the
  `isFloat` paths exist in localstate.go — but confirm with a float-call microbench).

## 5. Workstream D — vFlags: compare fusion past adjacency (R1) (M, riskier)

Missed shapes: `cmp; local.tee $c; br_if` and `eqz; local.set; local.get; if`
(AssemblyScript emits these). Key insight making this cheap: **`mov` and `setcc`
don't clobber flags**, and neither do the loads/stores that `convergeBranchLocals`
emits. So the constrained fusion is sound without a general vFlags entry:

- In `setLocal` (driver.go): when the value elem is a fusable compare
  (`isFusableCompare`) AND the target is a pinned int local AND the reader peeks
  `br_if`/`if` next (copy the Reader like `callOp`'s fusion does): emit
  `condenseToFlags` → `SetccReg(cc, pr)` → `markLocalDirty` → consume the branch
  opcode → emit the branch on the STILL-LIVE flags (converge/edge code between setcc
  and jcc is flag-safe; assert no allocReg-spill can emit an ALU — `flushBelow`
  materialize paths can emit `lea/mov` only for slot/local/const… a deferred
  materialize CAN emit ALU — so only fuse when everything below is already concrete,
  or flush first as `brIfFused` does).
- Start with `cmp; tee; br_if` only; add `eqz; set; get; if` after the first is green.
- Gate: spec `br_if`/`if`/`block` + branches.wasm corpus + json/blake.

## 6. Workstream E — the Tier-2 decision gate

After B+C+D, re-measure the table in §0. Decision rule agreed in OPTIMIZATIONS.md:
- If json ser ≤ ~1.3× WARP and nothing else regressed: single-pass tier is done as a
  competitive baseline; declare victory and shift to features/infra (Workstream F).
- If ser is still ≥1.5×: the remaining loss is cross-block register allocation and
  burst scheduling — exactly Tier-2 SSA territory (`src/core/compiler/ir/` is
  half-built). Scope a spike: SSA-from-wasm for ONE function (wat 27), linear-scan
  alloc, lower to the existing encoder, dispatch per-function (hot-function whitelist
  env). Budget ~2-3 sessions; do NOT blend into the single-pass tier (two-tier rule).

## 7. Workstream F — runtime/infra from WARP (functional value)

In descending value; each is independent:
1. **Cooperative interruption** (S–M): WARP `checkForInterruptionRequest`
   (x86_64_backend.cpp:616) — a basedata flag checked at loop back-edges (cheap:
   `cmp dword [rbx-off], 0; jne stub` at `Align16`'d loop tops, stub → trap-unwind).
   Unlocks kill-runaway-guest. Add `Instance.Interrupt()` Go API + a new trap code.
2. **Wasm stack traces on trap** (M): frame-ref chain per WARP; needs a per-frame
   push or a return-address walk keyed by `c.Entry` ranges (wago has the perf-map
   machinery already — a code-offset→function table exists at compile time).
3. **V2 sync host imports** (L, ⭐ functional): the runtime half is spiked
   (`CallWithHost`, `stubHostCall`); missing is codegen for the call protocol.
   This unlocks WASI. Big but well-scoped; read `runtime/engine_linux_amd64.go:68+`.
4. **arm64 backend** (L): WARP `backend/aarch64` as the reference, same porting
   playbook as railshot. Only if Apple Silicon demand exists.

## 8. Pitfalls (hard-won this week — read before touching anything)

- **wat vs local function indices are off by one** (`env.abort` import). wasmfuncN
  (perf maps, c.Entry) = LOCAL index; wat `(;N;)` includes imports.
- **Layout luck is ±20% on tight loops** (fib_rec, sieve). Before believing any
  single-module regression: re-run 2–3×, then diff disassembly (both directions —
  a "win" can be luck too). Loop tops + internal entries are Align16'd; function
  starts 16B-aligned at module layout.
- **The commit hook runs gofmt and FAILS the commit if it reformats** — `git add`
  again and re-commit. Also `git add src/` fails from `bench/` (cd to repo root).
- **basedata offsets are distances below linMem with field-specific widths — they
  can overlap.** A u64 write at [linMem-16] corrupts `offMaxLinMemPages` (-12).
  New slots: extend `abi.BasedataSize` in 16-byte steps (currently 112,
  `TrapCellPtrOffset` = 104).
- **Register fixed roles**: RBX=linMem (module invariant), R15=memBytes (explicit
  mode), R14=module-global(s), RBP=mergeReg (regMerge off if pinned; NEVER a module
  global), RAX/RDX/RCX/R8 = scratch-reserved (div/shift/host-log/rep use them; never
  pinnable), rep movs/stos clobber RDI/RSI/RCX, bulk-op scratch is RDX/R8.
- **Any change to `Compiled` requires a `wagoVersion` bump** (codec.go, currently 9)
  + updating `codec_test_helpers_test.go` prefix writers + the malicious-count
  fixtures in codec_count_test.go.
- **Every native-entry path must go through `Engine.Call`/`CallWithHost`** — they
  install + zero the trap cell (`installTrapCell`). A new entry path that skips it
  gets garbage trap pointers on the (cold) trap path.
- **Guard-mode anything needs `-tags wago_guardpage`**; `NewRuntimeConfig()` defaults
  to signals-based under that tag (WAGO_BOUNDS=explicit overrides).
- **A/B env knobs**: `WAGO_Amd64_NOSTACKREG`, `WAGO_Amd64_NOREGABI`,
  `WAGO_Amd64_NOFENCE`, `WAGO_REG_MERGE=0`, `WAGO_DEBUG_PANIC=1` (re-panic with stack
  from compileFunc's recover), `WAGO_BOUNDS`, `WAGO_JSONPROF_ONLY`.
- **The miscompile-debugging playbook that cracked #68** (use it for any wrong-result
  bug): (1) add a temporary per-function gate env (`funcIdx < N`) and bisect N over
  the differential corpus; (2) split the suspect mechanism into isolation knobs
  (e.g. store-all vs eager-reload) and run the matrix — complementary fingerprints
  localize the state machine bug; (3) write the minimal exec_test reproducing it,
  verify it FAILS with the fix reverted.
- **The warp/ submodule is intentionally dirty** (patched bench main + code-dump
  hack). Don't reset/clean it; the patch reference is bench/warp/bench-main.patch
  (the dump addition is only in the working tree).
- **allocReg exhaustion degrades, not panics** (`allocRegOrNone` + spill-slot
  fallback in condenseBinary), but new fixed-role paths must still respect
  `f.reserved`/`f.pinnedLocalMask`/`f.pinned`; `spillIfUsed` bypasses `pinned` (div
  hazard — see condenseBinary's RHS relocation comments).

## 9. Suggested session plan

| session | scope | exit criterion |
|---|---|---|
| 1 | Land #88+#89; Workstream A; B1 (stGlobReg) | differential test in tree; ser measurably < 189 or B1 findings documented |
| 2 | B2 (const-store split) + B3 (WARP wat-27 diff) | ser number + R4 updated with root cause |
| 3 | C (float parity) | isa_f32/f64 ≤1.2× wazero; min/max fixed; spec floats green |
| 4 | D (vFlags constrained) | branches/AS control benches; spec green |
| 5 | E decision gate; either wrap-up PR or SSA spike scoping | OPTIMIZATIONS.md updated with the decision |

Every session: branch from main (`perf/<topic>`), commits per logical change with
short subjects (repo convention: no commit bodies), full gate battery before each
commit, PR per workstream.
