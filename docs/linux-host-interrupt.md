# Linux host interruption

Linux/amd64 and Linux/arm64 context cancellation and active-instance close use a
thread-directed real-time signal plus `ucontext` rewriting. Public compilation
therefore emits no interruption loads, counters, branches, or safepoints in
generated Wasm on either architecture.

## Execution contract

Ordinary native entries publish no activation and do not lock their goroutine
to an OS thread. Generated Wasm already keeps its linear-memory base in a fixed
register (RBX on amd64, X26 on arm64), and basedata stores the active trap-cell
pointer at a fixed negative offset. The signal handler therefore derives all
per-invocation state from the saved CPU context.

`MapCode` publishes executable ranges into a fixed 4,096-entry table. It writes
the range start last; `Unmap` clears it first. The signal handler therefore
performs a bounded, read-only scan without allocation or locking.

Each live `JobMemory` base is in a second fixed 4,096-entry table.
The table is process-wide and uses 32 KiB on a 64-bit system.
The table includes mappings in the bounded reuse caches.
See [Native memory limits](native-memory-limits.md) for configuration and telemetry.

Wago negotiates signals 35..64, preferring ignored signals and then compatible
Go dispatchers. It preserves the complete action and chains unowned deliveries.
A random process cookie and a non-reused request sequence authenticate queued
broadcasts and timer expirations. Final code unmapping restores the old action
only if Wago still owns it. The handler uses the alternate signal stack.

For a saved PC in a registered code range, the handler:

1. reads the fixed linear-memory register;
2. matches the register value against the live `JobMemory` table;
3. reads the basedata trap pointer only after the match;
4. matches the trap pointer and exact token against active cold requests;
5. writes `TrapInterrupted` to the preallocated invocation trap cell;
6. loads the trap re-entry stack pointer recorded by `enterNative`;
7. rewrites the saved stack and program context to a landing pad; and
8. returns through `rt_sigreturn`.

This order protects the first instructions of a native entry.
The saved fixed register can contain an unrelated value before the entry prologue sets it.
The handler does not dereference that value unless it matches a registered base.

Creation registers the base before the caller can use the mapping.
Close blocks new signal readers before it removes the base.
Close then waits for existing readers before it unmaps the memory.
This reader gate prevents a handler from reading an unmapped basedata region.

The landing pad returns into the ordinary `enterNative` epilogue, which restores
the Go stack and registers. Existing trap decoding then returns
`context.Canceled`, `context.DeadlineExceeded`, or the interruption trap used by
`Instance.Close`.

## Races and host code

An interruption request publishes its trap pointer in a fixed 64-entry table,
enumerates `/proc/self/task`, and uses `rt_tgsigqueueinfo` with its token to
broadcast to process threads. Deliveries outside registered generated code
return immediately. A generated-code delivery only commits when its basedata
trap pointer matches the request, then acknowledges the matching table entry.
This moves thread discovery and all registry traffic to the cold request side.
Request writers hold a mutex, initialize the token and reference count, and
publish the trap pointer last. The ARM64 handler uses an acquire load of that
pointer before reading the token; AMD64 provides the same load ordering. A
reused slot therefore cannot expose its previous token as a new request.

The trap is stored before the broadcast. Entry and host-return boundaries
preserve it, and bounded cancellation retries close the small check-to-entry
race. Context callbacks retry at most 256 times at 50-microsecond intervals; if
execution remains parked in host code, the trap stays armed and is consumed when
the host returns without continuing process-wide task enumeration indefinitely.
`Instance.Close` uses a bounded asynchronous retry whose stop function remains
attached to the close state until physical resource release; stale retries
therefore cannot outlive the arena containing the trap cell.

Context-aware calls register their interruption callback with
`context.AfterFunc`; they do not keep a watchdog goroutine blocked for the
duration of the call. Cleanup is deferred and idempotent, so arbitrary host
panics stop the callback before propagating. The retry callback starts only if
cancellation or the deadline actually fires.

Deadline contexts alone lock their goroutine for the duration of the call and
arm a `timer_create(CLOCK_MONOTONIC, SIGEV_THREAD_ID)` timer for that Linux TID.
Kernel delivery breaks an uninstrumented native loop even if the Go runtime is
waiting for that loop during stop-the-world GC. After the deadline, the timer
retries at a short interval and is deleted before the goroutine is unlocked.
The timer reuses the request-table match, so nested Wasm entered by a host
callback is not mistaken for the deadline's target. Setup failures return an
error before native entry. Cleanup deletes the timer and unpublishes the request
before unlocking the goroutine. Late expirations carry the old token and cannot
affect a later use of that trap address; unrelated signals are never drained. Explicit
cancellation uses the broadcast path because it has no predetermined kernel
deadline.

The mechanism deliberately moves cost to interruption. Ordinary host/native
entries execute the same runtime path as the non-signal design, and generated
Wasm pays no interruption cost per instruction or loop iteration. Deadline
calls pay for thread locking and timer setup; explicit cancellation and close
pay for request publication, task enumeration, and signal delivery.

The handler never discards a host/runtime stack. If an import does not return,
in-process cancellation cannot force it to unwind; physical instance resources
remain protected by the invocation lease. A worker process remains the terminal
fallback for hosts that require a guaranteed kill of arbitrary native code.

## Validation

Run the Linux architecture-specific gates on native amd64 and arm64 machines
(qemu-user is also useful for arm64 context-layout regression coverage):

```sh
go test ./src/wago -run 'Test(CallContextInterruptsNativeLoop|InvokeContextInterruptsNativeLoop|InvokeContextInterruptsHostCallLoop|KernelDeadlineInterruptsDuringStopTheWorld|CloseInterruptsInfiniteInvocation|PublicCompileOmitsCooperativeInterruptPolls)$'
go test ./src/core/runtime -run 'Test(ProcessNativeMemoryStatsTracksLifecycle|ProcessNativeMemoryStatsTracksCacheReuse|InterruptLinearMemoryCapacityReturnsTypedError|UnregisterWaitsForSignalReaders|InterruptLinearMemoryRegistrationScansPastHoles)$'
go test -race ./src/core/runtime -run 'TestInterrupt(Request|Token|SignalOwnership)' -count=10
go test ./src/core/runtime ./src/wago
```

The public-compile test compares the exact generated bytes against a backend
compile with interruption disabled and verifies that the cooperative form is
larger.
The request publication test observes repeated slot reuse through atomic loads;
it detects a published trap paired with an older token without sending signals.
Publication changes add no table storage or generated-Wasm instructions.
