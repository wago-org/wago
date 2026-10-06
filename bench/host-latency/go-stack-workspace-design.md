# Go-owned numeric guest workspace experiment

Status: isolated AMD64 runtime prototypes now exist; not retained or performance-qualified. This is a next architectural experiment if the remaining bridge/entry candidates leave the Wasmtime gap intact. It retains the user API and ordinary Go callbacks.

The current integer bridge parks the guest on an engine-owned foreign stack and restores a normal Go owner before invoking the callback. A bounded, resource-free integer module has no Go pointers in its native local/spill frame. This suggests reserving the native frame inside a normal Go assembly owner's declared NO_LOCAL_POINTERS workspace, while keeping the prepared object and callback as ordinary pointer-bearing Go arguments.

A possible AMD64 layout is a normal, splitting Go assembly frame with an 8 KiB workspace: outgoing Go callback spill space at its bottom, then the native guest frame, then owner metadata outside the guest frame. The guest frame size is currently align16(header + locals + EH + maximum spills) + 8; eligible numeric modules exclude EH/native callees. Admission must bound actual frame size, not infer it from wasm byte length. Enter the guest with a constructed SP so its prologue lands at ownerSP + callbackSpillBytes. On a callback, save the JIT return PC, remove the native CALL word, move down only by callbackSpillBytes, restore Go BP/g, and jump to a label in the normal Go owner. Invoke the Go closure there with its ordinary ABI/root maps. Resume by computing guestSP from the current ownerSP, restoring the immutable context, and jumping to the saved native PC.

This could avoid most parked-stack bookkeeping at each callback. It does not run arbitrary Go directly under an unknown JIT return PC, and does not require pretending JIT frames have Go stack maps: when Go can scan/grow the stack, the active return PC and SP must describe the real declared Go owner. While guest code runs, unknown JIT PCs remain unsafe for async suspension; the existing bounded host-segment certificate must ensure timely return to a valid Go safe point.

Required proof and tests before even a performance claim:

- The Go assembler frame must split normally (not NOSPLIT at 8 KiB); inspect generated prologue, PCSP and argument maps.
- Every live native value is numeric and inside the copied workspace. No raw pointer into that workspace may survive in off-heap control state across stack growth. Save offsets or recompute from the current Go owner SP. Refresh trap reentry pointers after every callback.
- Guest locals/spills must never overlap Go callback outgoing spills, saved Go BP/return PC, owner metadata, or pointer-bearing Go arguments. Use actual compiler frame bounds and stack-fence checks.
- Restore Go g and BP before arbitrary Go; guest allocator may use R14/RBP between callbacks. Keep the prepared object and closure rooted throughout GC, panic unwinding and stack movement.
- Native return/trap paths must restore the normal owner's SP/BP/g and trap semantics. Foreign-stack paths remain available for modules outside admission.
- Exercise huge callback stack growth, GC-induced stack shrinking, nested reentry, retained Caller expiry, panics/HostTrap/HostExit, callback FP state, wide live operands, branches, local pins, cancellation, close and artifact rejection on native AMD64.
- Compare source-pinned baseline/candidate with checked ordinary Go atomic callbacks, including full public/prepared/session round trips and batching. Do not replace callbacks with native-lowered benchmark bodies.

An arbitrary Go-stack JIT frame or a raw SP cached off-heap is not sufficient. The workspace and relocation protocol are the core of this proposal; failure to prove them rejects the experiment.

AMD64 source audit on 2026-10-06 narrows the relocation requirements:

- Railshot's native frame is RSP-relative. RBP is a general numeric allocator
  register, not a native frame pointer (`amd64/compile.go`, frame-layout comment
  and `mergeReg`). Save it as numeric state; do not relocate it as a stack address.
- The ordinary native prologue subtracts its finalized frame and writes the
  result-buffer pointer before emitting the stack-fence check (`fn.prologue`).
  A rewritten fence alone therefore cannot safely bound an 8 KiB Go workspace.
  Actual finalized per-function frame sizes are required before entry. Existing
  `CodegenStats.FrameBytes` is diagnostic-only and cannot be an admission source.
- The parked integer bridge stores raw guest SP in `hcSavedRSP` and a raw
  parked-frame address at 40(ownerSP). Both would point into a copied Go stack in
  this experiment. Save guest SP as an offset from ownerSP before arbitrary Go,
  and recompute the parked metadata address from current ownerSP after return.
  Refresh the native trap reentry anchor and fence before resuming the guest.
- Normal integer-only certification already excludes local native calls and
  resource helpers. A workspace certificate must additionally reject absent
  frame-size metadata, oversized frames and stale/deserialized compile-only
  metadata. Keep ordinary foreign-stack entry as fallback.

At the initial source audit this was design-only; the subsequent isolated
runtime experiments are described below. A compiler-emitted frame-size sidecar, finalized after frame packing,
with equivalent serial and parallel compiler coverage, is the next prerequisite.

An isolated AMD64 compiler prerequisite now exists at
`/tmp/wago-workspace-frame-bounds-20261006`: an optional compile-only finalized
frame-size sidecar. Native serial/parallel, compact/noncompact tests passed,
including a frame above 8 KiB, diagnostic frame-size comparison and identical
emitted bytes with/without recording. Evidence and patch are under
`measurements/2026-10-06-geneva/workspace-frame-bounds/`. It is not retained in
production, consumed by the runtime, serialized or sufficient to admit a
workspace. The runtime workspace and relocation protocol remain unimplemented.

Runtime experiment update: v2 now has native race evidence that a traced normal
workspace owner survives actual active Go-stack relocation. v3 also passes that
strict proof and host/native trap recovery. Initial bridge and cold-route defects
were corrected in isolated copies; their raw logs remain retained. In-process
diagnostics do not establish a speedup (Minecraft was running, no Rust reference
or interference guard). These workspace prototypes are not production changes.
See `measurements/2026-10-06-geneva/owned-workspace/README.md` for exact coverage,
failures, source hashes and remaining verification.
