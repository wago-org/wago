# WARP x64 backend — status & game plan (2026-07-01, HEAD 826f0d2)

Branch `port/warp-x64`. Worktree `/home/hub/Code/Wago/wago/.claude/worktrees/json-as-bench`.
Companion to `docs/warp-x64-port-plan.md` (Codex's 7-step storage-model plan).

## Status: CUTOVER COMPLETE — x64 is the sole backend

x64 is now the only code generator and the default. The amd64 *codegen* has been
deleted; `src/core/encoder/amd64` is the **encoder-only** package (`Asm`, `Reg`,
`Cond`, `CompiledModule`) that x64 drives. The `WithX64`/`WAGO_X64`/`useX64` toggle
and the dead `registerCallABI` knob are gone.

Standing correctness gate:
- **`TestSpecSuiteExec` (src/wago)** — the WebAssembly spec suite as a native x64
  execution oracle. 15,106 assertions pass (gated on `WAGO_SPECTEST_DIR` +
  `wast2json`). THIS is the authoritative gate. Pin the testsuite to a commit
  wabt can parse (2021-10 `6aacfd8` works with wabt 1.0.34; newer commits merge
  reference-type cases that fail to convert and silently drop coverage).
- x64 exec + runtime (default & `-tags wago_guardpage`) + src/wago.

### Bugs the validation found + fixed this session (x64 register allocator)
Deep differential fuzzing (random nested-expression modules vs amd64) + the spec
suite surfaced four real x64 codegen bugs, all fixed and hand-verified against
wasm semantics (not merely matched to amd64, which is a spike with its own bugs —
e.g. it spuriously raises the div_s overflow trap when the divisor comes from
`trunc_f64_s`, where x64 is correct):
1. **div/rem by zero + INT_MIN/-1** raised a hardware #DE (hard crash) — no trap
   checks were emitted. (`condenseDivRem`)
2. **Nested divisor**: `div(a, rem(b,c))` put the divisor in RDX; the unsigned
   div's `xor rdx,rdx` zeroed it → #DE. Relocate divisor out of RAX/RDX.
3. **Nested variable shift/rem as a shift count** clobbered the shift value.
   Shift now computes the value into a scratch reg immune to RAX/RDX/RCX.
4. **A div/shift consumer (dest=RAX) or a div LHS clobbered a live RHS operand**
   that a binary op had just condensed into that register. `condenseBinary` now
   relocates the RHS clear of RAX/RDX/RCX/dest and pins it.

The differential x64-vs-amd64 tests (corpus + fuzzer) were **removed** with the
cutover (no amd64 codegen left to compare against); the fuzzer's methodology is
in git history (commit `bf08528`). A self-contained x64 fuzz with a Go reference
oracle would be a good follow-up to restore "fuzz as a standing gate."

### Prior status (pre-cutover)
Was: functionally complete, beats amd64 everywhere, all tests green — x64 exec +
amd64 golden + runtime + src/wago + differential corpus (11 modules).

### Landed this session
- **Handler-jump traps** (`66ac5bf`): a trap does `mov rsp,[linMem-offTrapStackReentry]; ret`,
  unwinding the whole native call tree in one jump; per-call trap check removed.
  Trampoline (`trampoline_amd64.s` + tinygo thunk) stores the entry SP at `[linMem-24]`.
  **fib_rec 532→395µs (−26%).**
- **Frameless** (`cbe6177`): no frame pointer; one `sub rsp,frameSize` (biased ≡8 mod 16);
  RSP-relative frame (`frTrapOff=0`, `frResultsOff=8`, locals at `16+8i`, spills after);
  **RBP is now allocatable**; wrapper-call args/results **reuse spill slots** (RDI=&slot[d-p],
  RCX=&slot[d]) — no transient SubRsp/AddRsp. Fixed amd64 `ImulRM`/`fmemDisp` to emit SIB for
  RSP base. **json-as ser +10%, FibLoop +7%.**
- **Codex (10 commits `cbe6177..826f0d2`)**: `references.go` occurrence-tracking (WARP reference
  map, O(1) storage replacement, wired throughout) · local-state consolidation · target-hinted
  local sets (plan step 4, partial) · float register-call ABI (step 5, partial) · frameless
  guard traps · `WAGO_BOUNDS=guard` from default config · `memory_tree` fixture · plan doc.

## Perf standing (this machine, verified)

| workload | wago-x64 | wazero | WARP | verdict |
|---|--:|--:|--:|---|
| FibLoop (compute) | 19.1 ns | 43.8 | 36.5 | wago **wins both** (2.3× / 1.9×) |
| CallOverhead | 9.9 ns | 32.2 | — | wago **3.2× vs wazero** |
| FibRec (recursion) | ~410 µs | 507 | 294 | beats wazero; **~1.4× behind WARP** |
| json-as ser | 247 / **240 guard** | 146 | ~98 | 0.6× wazero; ~2.5× behind WARP |
| json-as deser | 443 / **386 guard** | 300 | ~170 | 0.7× wazero |
| memory_tree(8,24) | 17.5 / **11.95 guard** | 21.1 | 8.55 | guard **beats wazero**; 1.4× behind WARP |

Reproduce: `WAGO_X64=1 go test -run '^$' -bench Exec ./bench` · json-as:
`WAGO_JSON_MODULE=…/wago-bench.swar.wasm go test -run TestJsonAsGuard -tags wago_guardpage ./bench`
· WARP: `warp/build-bench/bin/vb_bench <wasm> <fn> <args>`.

## Root cause (durable, from deep profiling)

Compute/calls are competitive-to-winning. **json-as is MEMORY/GC-bound** (TLSF alloc + itcms GC
+ SWAR string copy, dependency-chain limited) — codegen micro-opts wash out (STACK_REG,
target-hints, references-infra all measured ~neutral on json-as). The remaining WARP gap on
json-as is **not primarily codegen**; chasing it via codegen is low ROI. `memory_tree`
(call+branch churn, less GC) is where richer call ABI + operand-register-reconciliation help.

## Game plan

### ▸ A. Cut over to x64 and REMOVE amd64 — ✅ DONE (see Status above)

Steps 1 (spec suite on x64), 2 (differential fuzz), 4 (flip default), 5 (delete
amd64 codegen + relocate encoder via encoder-only `backend/amd64`), and 6 (remove
differential scaffolding + toggles) are complete. The original plan text follows
for reference.

Endgame: x64 is the **only** codegen backend; the amd64 *codegen* (the old single-pass
wasm→native compiler) is deleted and the `WAGO_X64`/`WithX64` toggle is gone.

**Hard constraint — the encoder survives the cutover.** x64 is literally built on
`backend/amd64`'s instruction **encoder** (`Asm` in `asm.go`/`asm_sse.go`, plus `Reg`, `Cond`,
`CompiledModule`) — every `f.a.Load64(...)` etc. So "remove amd64" means: delete the amd64
*codegen* files (the wasm-lowering compiler: its `memory.go`/compile logic/`CompileModuleWith`/
`CompileOptions`), and **relocate the encoder** so it outlives the amd64 package. Two ways: (a)
keep `backend/amd64` as an encoder-only package (drop its codegen files, keep `Asm`); or (b) move
the encoder into `backend/x64` (or a neutral `backend/encoding` pkg) and delete `backend/amd64`
entirely. (b) is the truest "swap it out completely"; (a) is lower-churn. Pick (b) if the goal is
zero references to a package named amd64.

**Sequencing (validate BEFORE deleting — amd64 is today's default AND the differential oracle):**
1. **Wire the WebAssembly spec suite to run against x64.** `TestSpecSuite`
   (`src/core/compiler/wasm/spectest_test.go`) exists but is amd64-only AND skipped (needs
   `WAGO_SPECTEST_DIR=<checked-out WebAssembly/testsuite>`). Add an x64 path (compile with
   `WithX64(true)`) and run it green. **THE cutover blocker** — real correctness validation that
   finds bugs unit tests miss. (No testsuite on disk; `git clone WebAssembly/testsuite`.)
2. **Differential-fuzz x64 vs amd64** beyond the 11 corpus modules (random valid modules / wider
   args) while amd64 still exists — this is the last chance to use amd64 as the oracle, so mine it
   hard here.
3. **Decide guard-as-default-bounds** policy for `wago_guardpage` builds (guard is the only
   measured json-as/memory win, 11–16%).
4. **Flip the default to x64** in `src/wago` (config.go/api.go): x64 always used; remove the
   `useX64` field, `WithX64`, and the `WAGO_X64` env. Gate on 1–2 green.
5. **Delete amd64 codegen + relocate the encoder** (see constraint above). Update every importer
   (`src/wago/api.go`, benches, tests) to the new encoder location.
6. **Remove the now-dead scaffolding**: the differential corpus test (x64-vs-amd64 has no amd64
   to compare against), the A/B env toggles (`WAGO_X64_*`), `WithRegisterCallABI`'s amd64-only
   branches, and any amd64-only bench variants. Replace the differential oracle with the spec
   suite + fuzz as the standing correctness gate.

### ▸ B. WARP storage-model perf work (Codex's plan) — orthogonal, do opportunistically / after cutover
Infra (`references.go`) is done. Remaining: finish step 4 (target hints on returns/select/
compares/mem-result/call-params), step 5 (2 GP + 2 FPR register returns, stack fallback), step 6
(explicit-bounds relayering behind A/B), then the big one — **operand-stack-in-registers across
branches** (WARP RegisterCopyResolver: reconcile the operand stack to REGISTERS at merges instead
of wago's flush-to-canonical-memory-slots). **Measure on `memory_tree`, not json-as.**

### Rules
- **Do NOT** chase json-as via codegen (proven memory-bound).
- **Do** measure any call/branch codegen work on `memory_tree`.
- Acceptance (from plan doc): no >3% regression on json-as-guard / memory_tree-guard / fib_rec
  without a documented larger win in the same commit.

### If you do exactly one thing
Do **A1 (spec-suite on x64)** — it's the gate that makes the whole port real.

## Housekeeping
- `make bench` runs the bench suite under guard-page bounds by default (`-tags wago_guardpage` + `WAGO_BOUNDS=signals`); `make bench-noguard` for explicit bounds.
