Census verdict at **Wago `90f2b57da`**: **DIRECT already exists for mechanically verified register-only Go i32 leaves.** Scalar and borrowed-slot portals also exist, but remain **BOUNDARY** under safety contract v2 because they execute normal Go callbacks. A narrow new DIRECT admission is plausible without public API changes: **zero-argument, constant-producing `func() int32` leaves**, subject to the qualification below. No new SPECIALIZED semantic binding is established by this runtime-only scope.

Read-only inspection only: no edits, builds, tests, benchmarks, timings, branches, or PRs. The excluded GC-port worktree and qualification machinery were not accessed. Anchors below refer to the pinned commit, not current working-tree contents.

**Call routes and code-evident costs**

| Shape | Pinned anchors | Crossing work evident in code |
|---|---|---|
| Direct synchronous host import | `railshot/amd64/call.go:1272–1490`; arm64 equivalent `call.go:1350–1600` | Flush operands, preserve pinned/global state, marshal parameters into control slots, materialize root locals and spill call-clobbered locals, write import index/counts, call trampoline, restore state and load results. i32/f32 carriers are zero-extended; v128 occupies two u64 slots. |
| Generic parked callback | `runtime/engine_host.go:304–378` | Native entry/park, inspect trap and active control frame, decode shape; for inline-capacity calls, copy **n parameter slots**, clear **r result slots**, invoke callback, copy **r result slots** back, then resume native execution. These counts exclude compiler-side marshaling. |
| Scalar portal | `engine_host.go:349–360,441–468` | Retains park/resume; reads up to two arguments directly and writes the scalar result, bypassing generic scratch copies when handled. Expanded two-result transport splits one u64 into **two i32 results**, not two arbitrary u64 values. |
| Borrowed-slot portal | `engine_hostview.go:73–97,121–129` | Creates views over parked control storage and clears result slots. Successful handling removes the Go scratch copies; declined inline-capacity handling takes the copied route. Wide handling uses checked extension storage directly. |
| Fixed/prepared callback | `engine_host.go:83–185`; `prepared_scalar_host.go:25–70`; `prepared_scalar_host_live.go:24–69` | Hoists entry validation and binds immutable shape/buffers. Still checks live ownership/mode and enters a rooted Go callback owner. |
| Live inline owner | `inline_host_amd64.s:7–139,148–233`; `inline_host_arm64.s:8–115` | Switches between guest stack and normal Go owner; checks frame/index/shape and traps; calls typed Go target; restores native registers/continuation. “Inline” does **not** mean arbitrary Go runs on the guest stack. |
| Indirect/ref host thunk | `hostthunk/amd64.go:50–105`; arm64 `hostthunk/arm64.go:57–90`; RV64 `host_indirect.go:35–115` | Additional wrapper-slot → control-slot argument copies and control-slot → wrapper result copies. Preserves caller state and result destination. Home-context and explicitly owned forms differ. |
| Deferred void event | amd64 `call.go:1173–1205`; `hostthunk/amd64.go:29–46` | Appends an import index plus first i32 argument to an event log; overflow traps. No synchronous callback at that point. This is a distinct contract, not a valid optimization for arbitrary synchronous effects. |
| Verified native leaf | `native_leaf_amd64.go:254–329`; arm64 `native_leaf_arm64.go:124–208` | Executes copied, validated register arithmetic. Control-frame entry guards index/shape and falls back to ordinary stub; direct wrapper entry reads argument homes and writes normalized result. Avoids Go callback and stack transition. |

No per-call reflection is evidenced in these engine loops. Native-leaf reflection, instruction inspection, executable mapping and cache acquisition happen during registration: `native_leaf_native.go:45–80`. Other API-level callback adapters may have their own costs; they should not be attributed to every crossing.

**Ownership, roots, traps and target selection**

- **Root versus binding owner is explicit.** `engine_host.go:305,321–332` retains root control identity but dispatches the exact active frame published at `trap+8`. `src/wago/host_execution.go:19–41` separately records owning instance and root invocation identity/reservation.
- **Ordinary Go is reached safely by parking.** amd64 `hostcall_amd64.go:66–88` saves RSP plus six callee-saved registers, publishes the active frame, writes `hostCallPending`, and unwinds through the invocation’s reentry stack. Resume repairs trap/fence state (`:101–107`). arm64 has its own GPR/FP saved prefix (`hostcall_arm64.go:20–48`); offsets are not portable.
- **GC obligations are already in compiler lowering.** amd64 `call.go:1298–1302,1415–1421,1467–1469` prepares root maps, materializes lazy reference locals and records the callsite. Host mutation/reentry requires global reloads; `:1580` explicitly documents reentrant memory growth. Removing transport is not permission to remove those obligations.
- **Go owners remain live.** `engine_host.go:243–248`, prepared calls, and live assembly preserve rooted callback/context owners. `hostScratchInUse` selects separate scratch for nested calls (`:235–241`).
- **Cancellation survives resume.** `engine_host.go:311–317` preserves `TrapInterrupted` and checks it before resumption. Live bridges check traps before and after callbacks.
- **Tail/ref/cross-instance paths need independent qualification.** amd64 `call.go:2784–2788,3061–3076,3125,3190–3215` distinguishes wrapper kinds and proper host tails. Native binding deliberately leaves shared host thunks/reference descriptors staged (`native_leaf_native.go:105–120`).
- **RV64 is not feature-equivalent by assumption.** `riscv64/host_import.go:14–38` rejects foreign/immediate import bindings in this admission path. `call.go:769–789` validates code, alignment, home and context identity before arena writes. Its prepared scalar bridge currently uses the ordinary stub (`prepared_scalar_host_riscv64.go:13–24`), not the amd64/arm64 live assembly route.

**Adapted classification and admission**

| Class/surface | Verdict |
|---|---|
| DIRECT: existing `(i32)->i32`, `(i32,i32)->i32` register-only copied bodies | **Existing admission machinery.** Instruction proof excludes memory, stack, calls, branches, closure/runtime state and FP. See amd64 `native_leaf_amd64.go:13–17,190–241`; arm64 `:20–24,72–121`. This is stronger than trusting a callback name or signature. |
| DIRECT: arbitrary scalar Go callback | **NOT VIABLE from signature alone.** It may allocate, block, panic, access closure state or reenter. Keep normal Go ownership and transition. |
| SPECIALIZED: scalar/view/fixed transport | **Transport specialization exists; semantic class remains BOUNDARY.** Borrowed slots and static targets do not prove NoGC/NoReenter/NoBlock. |
| SPECIALIZED: checked memory fast path | **Design-admissible per exact trusted binding, not established here.** Requires body-specific validation order, current descriptors, checked extents, bounded work and fallback before mutation. This scope does not establish a bundled operation’s complete proof. |
| BOUNDARY: Caller/resource/reference/GC/blocking/reentrant callbacks | **Retain.** Capability scope, root translation, leases and callback authority are observable requirements. `host_execution.go:79–107` illustrates that even bounded Caller dispatch retains scope and reentry authority. |
| Deferred events as replacement for synchronous callbacks | **NOT VIABLE.** Changes effect ordering and observation. |
| Native leaf inside bounded-Go-segment admission | **NOT VIABLE without a different scheduling proof.** `engine_hostbounded.go:15–18,183–196` requires real Go callbacks; replacing them with leaves removes the safe points on which that proof depends. |

Existing native admission is conservative beyond the instruction decoder: `host_execution.go:1088–1117` excludes GC domains and gated/mixed callbacks, and only aliases imports with matching typed shape and code identity. Registration disables this route during code profiling (`:199`). Code mappings are reference-counted and released after instance quiescence (`native_leaf_native.go:90–101`).

**Narrow proposed candidate**

Admit **`func() int32` whose validated machine body produces a constant using only the existing permitted register instructions**, initially with one import and no GC domain.

This fills a concrete hole: both decoders and the shared registration cache reject `params < 1`, although constant register producers fit the NoGC/NoReenter/NoBlock model. It needs no host-author effect declaration or public API change **provided the existing binding layer already represents this exact Go function shape**; otherwise that representation must first be confirmed as an internal adapter change.

Required changes would be internal: start defined-register sets empty for zero parameters, require a defined result, avoid reading nonexistent argument homes, add zero-argument shape admission, and update backend shortcut conditions currently requiring `p >= 1`. Do not broaden memory/call/branch acceptance.

Exact qualification:

1. Decoder acceptance for constants; rejection of closures, global loads, stack prologues, calls, branches, unsupported encodings and undefined result reads.
2. Forced-generic versus eligible-native execution, with **route evidence**, across zero, negative, min/max i32 and poisoned raw result homes.
3. Direct, table, `call_ref`, tail, reexport and supported cross-instance entry; unsupported routes must remain staged or reject normally.
4. Wrong shape/index/frame, missing target, stack fence, cancellation, nested callbacks, profiling and rejected-body fallback.
5. Code-cache sharing, close/quiescence, last-owner release and recreation.
6. Ordinary, `wagodebug`, and `wago_regalloccheck`; amd64/arm64 execution qualification separately. RV64 remains generic unless separately implemented and qualified.
7. Preserve syscall scheduling and guest entry/backedge polling: native leaves are not Go safe points.

This is a **proposed admission**, not a qualified candidate.

**Sibling cross-check**

- **Slipstream `bd640ddf0`:** `src/jit.goose:619–646` uses 16-byte carriers, span checks and trap propagation; `native/jit_linux_x86_64.S:822–840` publishes/reloads its collector cursor, inflates ownership and switches stacks around dispatch. Wago uses u64 slots and a parked Go-owner protocol. The common finding is the need to preserve owner/root/transition contracts; copying Slipstream’s offsets, span repair or cursor protocol into Wago would be unjustified.
- **Starshine `b28144a7c50f1dc360825f7f68220022101b9921`:** `src/internal/xfs/fs_wasm.mbt:85–126` confirms per-byte opaque-handle calls and EOF probing; `:46–69` documents code-point string reading. This supports adapter-shape hypotheses, not a Wago native-leaf admission: handle/state access fails Wago’s register-only proof, and those MoonBit imports must not be assumed present in Wago merely because the sibling uses them.

**Research status:** pinned-source census complete for the requested boundary machinery; no runtime correctness or performance claim. No implementation head or gate results exist because this assignment was read-only.
