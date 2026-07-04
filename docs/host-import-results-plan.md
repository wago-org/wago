# Sync host imports with results — plan (P8.1, the WASI unlock)

Status: design. Supersedes the ARCHITECTURE §11 "V2 spike" paragraph as the
implementation plan. `no-ir-plan.md` §P8.1 calls this the single highest-value
item in the file.

## 0. Goal

A wasm import can call a Go function **synchronously and get a typed result
back**, so imports like `wasi_snapshot_preview1.fd_write() -> errno` work. Today
host imports are void-only, first-i32-only, run *after* the wasm call returns
(async log-and-replay). WASI is impossible without synchronous, value-returning
re-entry.

## 1. Why the async model can't return a value

`callHost` (`backend/railshot/call.go:139`) appends `(importIdx, arg_i32)` to an
off-heap log at `[linMem-offCustomCtx]` (basedata offset 40); `Invoke` replays
the log against the Go `HostFunc`s **after** `Engine.Call` has fully returned
(`src/wago/api.go:754 replayHostLog`). By then there is no live wasm
continuation to receive a result. Enforced gates: backend `call.go:141`, linker
`api.go:261` (a returning import is legal only as a *cross-instance* binding),
frontend `frontend.go:184`.

## 2. Mechanism: save-state + resume trampoline (WARP/wazero model)

The spike (`CallWithHost` + `stubHostCall`, `hostCallPending=0x10000`) proves the
Go-side loop and trap-slot signaling, but re-enters via `enterNative(code,…)`
which **resets RSP to the foreign-stack top** (`trampoline_amd64.s:36-47`). That
only resumes a *stateless* stub. A real function's operand stack, spilled
locals, and nested frames live on the foreign stack below the top and would be
clobbered by re-entry from the top.

The trampoline already contains the two primitives we need:
- Go callee-saved regs are stashed in a 64-byte save area at
  `foreignStackTop-64` on entry (`.s:38-47`); restored on return (`.s:63-71`).
- A trap anywhere in the call tree unwinds in one jump via
  `mov rsp,[linMem-offTrapStackReentry(24)]; ret` back into `enterNative` right
  after `CALL R11`, which then runs its Go-context epilogue.

So a host call reuses the trap-unwind to get back to Go, but **saves the wasm
deep state first** and adds a **resume** entry that restores it:

**Host-call site (codegen, returning imports only):**
1. `flush()` the operand stack + spill any pinned locals held in caller-saved
   regs (RDI/RSI) to slots — as `callHost` already flushes today.
2. Marshal the N typed params into the control frame's arg slots.
3. Store `importIdx` into the control frame.
4. `CALL hostCallStub` (shared per-engine stub; pointer read from a new basedata
   slot `offHostTrampoline`).
5. On return: read the M typed results from the control frame; push onto the
   operand stack (reload locals as needed).

**`hostCallStub` (shared native code, entered via CALL so [RSP]=resume addr):**
1. Save wasm callee-saved `RBX,RBP,R12,R13,R14,R15` + current `RSP` into the
   control frame's save area. (`RSP` points at the resume return address.)
2. Write `hostCallPending` into the trap cell (`[linMem-TrapCellPtrOffset(104)]`).
3. Unwind to Go: `mov rsp,[linMem-offTrapStackReentry(24)]; ret`. `enterNative`'s
   epilogue restores the Go context and returns to the exec loop. The deep wasm
   frames (below the save area) are untouched while Go runs.

**Go exec loop (generalized `CallWithHost`):** on `hostCallPending`, read
`importIdx` + typed args from the control frame, invoke the bound host func,
write typed results back, then re-enter via `resumeNative`.

**`resumeNative` (new asm, mirror of `enterNative`):**
1. Re-stash the Go callee-saved + goroutine SP into the `foreignStackTop-64`
   save area and re-arm `[linMem-offTrapStackReentry]` (identical prologue to
   `enterNative`), so a later trap/host-call still unwinds correctly.
2. Restore wasm `RBX,RBP,R12,R13,R14,R15` + `RSP` from the control frame.
3. `ret` — pops the saved resume address, resuming the wasm function on the
   instruction after `CALL hostCallStub`, with its full stack intact.

Register save set rationale: at a host call the operand stack is flushed, so the
only live wasm state is the module invariants (`RBX`=linMem, `R14`=globals,
`R15`=memBytes) + pinned locals in callee-saved regs (`R12,R13,RBP`); caller-
saved pinned locals (`RDI,RSI`) are flushed by codegen. Saving the 6 callee-saved
regs + RSP covers all of it. `RAX/RCX/RDX/R8-R11` are scratch — dead across the
flush.

## 3. ABI additions

- **Control frame** replaces the flat V2 ctrl block (`stubs_amd64.go:103`). Off-
  heap (arena), pointer installed at `offCustomCtx(40)` — reuses the host-log
  slot (a returning import needs no async log). Layout (u32/u64 fields, 16B
  aligned):
  - `importIdx u32`
  - `nargs u32`, `nresults u32` (or derive from the resolved signature — TBD)
  - saved `RSP u64`, saved `RBX,RBP,R12,R13,R14,R15` (6×u64 = 48B)
  - `args[K] u64` slots, `results[K] u64` slots (K = max import arity in module)
  - i32/f32 in the low 32 bits, matching the wrapper ABI.
- **`offHostTrampoline`** — new basedata u64 slot holding the `hostCallStub`
  pointer, so `callHost` emits `call [linMem-offHostTrampoline]`. Extends
  `BasedataSize` from 112 to 128 (16B step; re-derive `basedata_test.go` +
  `runtime-abi.md` + guard tests). Alternatively map the stub once per engine
  and thread its pointer at instantiate — decide in PR1.
- `hostCallPending` stays `0x10000` (outside `TrapCode` range).
- **`wagoVersion` bump** (codec.go): any `Compiled` shape/ABI change. Update
  `codec_test_helpers_test.go` + malicious-count fixtures.

## 4. Public API — DECIDED: reflection-ergonomic over a slot core

Users write **native-typed Go functions**, adapted to the slot ABI by
reflection at bind time (decision 2026-07-04). Current void `func(arg int32)`
(`globals.go:35`) stays valid (a subset). New capabilities:

- **Any numeric signature:** `func(a int32, b int64) int32`,
  `func(f float64) int64`, `func()`… params/results map i32↔int32, i64↔int64,
  f32↔float32, f64↔float64. Multiple results → multiple wasm results (needs the
  multi-result path; single-result first).
- **Optional leading `HostModule` for memory/instance access** (this is how WASI
  reaches guest memory — the bare reflection form can't): if the first Go param
  is `wago.HostModule`, reflection injects it and maps the *remaining* params to
  the wasm signature. `HostModule` exposes `.Memory() []byte` (+ later
  re-entrant `.Invoke`). So `func(m wago.HostModule, fd, iov, cnt, pw int32) int32`
  binds to a `(i32,i32,i32,i32)->i32` import with memory access.
- **Void imports keep the async log-and-replay fast path** (cheap, batched,
  back-compatible); **returning** imports use the sync re-entry path (§2).

Layering: the engine/runtime/codegen (PR1/PR2) is **slot-based** — the control
frame carries `[]uint64` params/results. A public slot form
`HostFunc func(m HostModule, params, results []uint64)` is the zero-alloc escape
hatch and the reflection adapter's target. Reflection lives entirely in
`src/wago` (PR3); the ABI never sees it.

Caveats to handle in PR3: `reflect.Value.Call` allocates per host call and is
slower — fine for WASI (syscalls dominate), documented for hot user imports
(point them at the slot form). **TinyGo `reflect` is partial** — verify
`reflect.Value.Call` works under TinyGo (memory: wago supports TinyGo, PR #39);
if not, the slot form is the TinyGo-supported path and the reflection adapter is
gated to standard Go.

## 5. Phasing (own branch + PR each; gate battery §7)

1. **PR1 — runtime save/resume protocol.** New `resumeNative` asm +
   `hostCallStub` + generalized control frame + `CallWithHost` loop taking typed
   arg/result slots. Prove with a runtime test that does a **deep nested call
   then a host call**, resumes, and returns a value (the spike only tested a
   flat stub). Highest asm risk — de-risk here, no codegen/API churn yet.
2. **PR2 — codegen.** `callHost` result path: marshal N params → control frame,
   `call [linMem-offHostTrampoline]`, read M results. Drop the `call.go:141`
   gate. Golden disasm test; `WAGO_*` A/B not needed (new capability, not a
   behavior change to existing code).
3. **PR3 — public API + wiring.** New returning-`HostFunc` type + `Imports`
   binding; `Invoke`/`invokeLocal` route modules with returning imports through
   `CallWithHost`; update `HostIndirectThunk` (table path) + imported-start;
   lift the linker gate (`api.go:261`). FEATURES/ARCHITECTURE/runtime-abi docs.
4. **PR4 — minimal WASI preview 1.** `wasi_snapshot_preview1`: `fd_write`,
   `clock_time_get`, `args_get/sizes_get`, `environ_get/sizes_get`,
   `random_get`, `proc_exit`. A `wago.WASI(...)` imports bundle + a CLI
   `--wasi`. Demonstrates the unlock end-to-end (a real WASI hello-world; the
   lua/ruby corpus modules that fail today with "host import with results not
   supported").

## 6. Resolved — API shape

Decided 2026-07-04: **reflection-ergonomic public API** (native Go signatures)
**layered on a slot-based core** (`func(m HostModule, params, results []uint64)`).
Memory access via an optional leading `HostModule` param. Rationale + caveats in
§4. Rejected: bare reflection with no memory access (WASI-incompatible), and
combinatorial typed variants (`HostFuncR`, `HostFunc2R`…).

## 7. Gate battery (per no-ir-plan §4)

- `go test ./...` root + `cd bench && go test ./...`, both plain and
  `-tags wago_guardpage`.
- Spec suite 57/57 (`WAGO_BOUNDS= go test -run TestSpecExec`).
- `TestCorpusDifferential` both modes.
- New: a wasm→host(returning)→wasm round-trip exec test; a WASI hello-world
  (PR4); golden disasm for the `callHost` result path (PR2).
- Runtime asm: a deep-stack resume test in `src/core/runtime` (PR1) — the spike's
  flat-stub test is necessary but not sufficient.

## 8. Risks / pitfalls

- **Re-entrancy (host calls back into wasm):** a single control frame is
  overwritten if a host func calls `Instance.Invoke`. MVP: the save area is on
  the *stack* (via the CALL-based stub), so nested wasm→host→wasm→host naturally
  gets distinct RSPs — but the control-frame arg/result/importIdx slots are
  single. Either stack the control frame or defer host→wasm re-entry to a
  follow-up; WASI preview 1 needs neither.
- **GC safepoints:** host runs on the goroutine stack in normal Go context
  (safe, as the spike comment notes) — the foreign stack is never scanned.
- **Guard mode:** the sync path must work under `-tags wago_guardpage` too
  (SIGSEGV handler + host re-entry coexist); test both.
- **Trap during a host call's arg marshaling** must still unwind cleanly.
- Layout-luck / basedata overlap discipline (16B steps) per perf-plan §8.
