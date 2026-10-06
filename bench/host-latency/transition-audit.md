# Synchronous host transition audit

Source pins: Wasmtime/Cranelift 46.0.0 (the installed Cargo.lock reference) and
Go1.27.1, plus current Wago source hashes in
measurements/2026-10-06-transition-audit/source-pins.json. This is a code audit,
not a latency result. External collectors remain active on the available hosts;
no timing was run during this audit.

Wasmtime's TypedFunc call writes parameters into a stack-local union, enters its
trap-catching boundary, and calls VMFuncRef::array_call. Cranelift's
compile_wasm_to_array_trampoline saves Wasm exit FP/PC, spills arguments into
stack ValRaw slots, loads the host array-call address from its context, calls it
with the platform ABI, checks the success result and reloads returned values.
The Rust array_call_trampoline constructs Caller, establishes its GC LIFO scope,
runs call hooks and invokes the closure. These are concrete mechanisms to emulate:
validated context-directed trampolines, shared argument/result storage, and a
single outer trap boundary. This is not evidence that every Wasmtime configuration
has no stack switching, nor a claim about async/component/Pulley paths.

Wago's ordinary ARM64 inlineHostEnterForeign writes a foreign landing record,
switches SP to the engine stack, and calls guest code. The staged callback saves
native SP, GP registers, FP/LR and V8-V15 into the control frame; it restores the
live Go owner's SP/FP and jumps to the Go callback label. Resume updates the
foreign record with the possibly moved Go stack, republishes trap/return landing
state, restores the engine SP/registers and jumps to the guest continuation.
The engine stack's address remains stable while the callback grows its Go stack.

The structural difference suggests two distinct experiments, rather than another
metadata object around the same runtime calls:

1. A compiler-certified host-call register mask. Wasmtime's platform ABI lets
   generated code and register allocation enforce preservation. Wago's Go ABI
   bridge explicitly saves a broad native register bank. To reduce it safely,
   the compiler must identify every value/register live at the particular host
   point, including implicit memory/context registers, local pins, spills and
   control merges. The integer opcode certificate alone does not establish GP
   liveness. Retain the complete bridge for unknown/artifact/custom/helper cases.
2. Execute a strictly bounded resource-free guest inside reserved Go-owner stack
   space. This could reduce handoff work, but it requires a new relocation
   protocol, compiler frame bounds and unwind proof. It is not implemented or
   established safe by this audit.

For the second experiment, Go isAsyncSafePoint declines unknown (non-Go) PCs;
that behavior does not register JIT frames with the runtime or grant permission
to scan/unwind them. Go copystack copies the used stack, adjusts runtime context,
defers and panics, then uses frame maps to adjust tracked pointers. Raw native
SP/FP values in Wago's external control frame would not automatically become
tracked pointers. A prototype would need to express parked guest positions as
owner-relative offsets and reconstruct them after callback growth, repair all
native frame links/stack-relative register values, keep genuine Go arguments
rooted, and make Go unwinding skip the guest region reliably.

The reserve size must come from native compilation, not just Wasm-local/opcode
counts: ARM64 frameSize includes frame header, local slots, EH frame storage and
maximum spill slots. FrameBytes is computed after body generation. A large
reserve also cannot simply be added to a NOSPLIT owner: Go's linker enforces a
NOSPLIT chain limit (StackNosplitBase is 800 before target/runtime adjustments).
A splitting owner needs generated pointer maps and positive stack-growth tests.
Existing stack-fence basedata must describe the reserve during the call and be
restored on normal return, trap, panic, cancellation and callback-initiated Close.

Promotion requires quiet-host paired measurement and independent correctness
oracles: live callback owner frames, forced repeated Go stack growth/GC, native
locals across both register banks, nested same/different-instance entry, blocking
callbacks, interruptions, inherited parent bindings, host/guest traps, panic and
successful follow-up calls, deferred Close, artifact rejection and dynamic
resource-publication fallback. Neither proposed experiment is a completed change.

The next executable step is the compiler register-mask audit: enumerate the
ARM64 implicit register contract and host-point liveness before emitting a
specialized bridge. It preserves the existing stable engine-stack protocol while
addressing transition work directly. Wasmtime parity remains unachieved.

## Initial register-contract findings

ARM64 cc.go reserves X26 for the memory/context base and excludes X28 (Go g)
from native allocation. The GP pool includes X19-X25 and X27 as well as caller
saved scratch registers. Base local pins are X19-X23; extended pins and module
roles can use other registers. Thus omitting an arbitrary fixed GP subset is
not justified by a small integer Wasm signature.

callHostSync flushes the operand stack and calls spillLocalsForCall before
building control arguments. localstate.go applies an all-register clobber mask:
legacy local handling stores selected pins and reloads them after the call;
STACK_REG handling moves dirty locals to frame homes and marks them memory
resident for lazy reload. This is a concrete candidate for eliminating duplicate
preservation, but does not prove all saved registers are dead. compile.go also
tracks reserved memSizeReg/memLimitReg/trapCellReg roles; a mask must include
those roles and any global pins, constant-register cache, control merge or
return-path state that survives the host point. The compiler's checkCallClobber
and the pinned-local/native-FP test oracles should accompany a new proof.

No save-mask optimization or Go-stack prototype was enabled during this audit.
The available collector slots remain occupied; performance verification awaits
authoritative quiet-host checks.
