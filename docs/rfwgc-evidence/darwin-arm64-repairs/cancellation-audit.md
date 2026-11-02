# Read-only cancellation follow-up

Requested after nonbackend handoff 921c8630a; read
`../darwin-nonbackend-repairs/README.md`. Its immutable execution source is
30b0813f7881a6fbb938a21535a205803bd27a0c, already including d8dc1a6dd.
The unchanged test fails at `native loop did not cancel` under Linux ARM64 QEMU
in all three modes. Admission and preceding GC progress pass. This audit did not
rerun that failure, relax its three-second deadline, edit runtime/public tests,
or use the running qualification driver's files.

## Backend repair impact

Source-supported conclusion: the join repair should not change the failing
loop's generated call path. This is static reasoning, not an old/new byte-image
comparison or a native execution result:

1. `fixedSharedHostLoopModule(false)` (native_leaf_alias_test.go) imports one
   numeric step function. Its looping body performs i32.store and i32.load before
   the import. The exported wrapper calls this defined function.
2. ARM64 compile.go's dynamicHostFast proof explicitly rejects
   hintTouchesMemory. Thus the memory loop takes emitCrossInstanceCall directly;
   it never emits the Go-host/native alternative whose false recordRoots argument
   caused the repaired mismatch.
3. api.go supplies candidate nativeUnwind plans for this imported ARM64 module.
   emitCrossInstanceCall calls prepareGCFrameCallsite and already supplies its
   true recordRoots value to nativeLeafContextShortcut. That predicate therefore
   rejected the shortcut before d8dc1a6dd and still rejects it afterward.
   Candidate plans also reject the separate entry-guarded native variant.
4. The repair does not alter Interruptible, trap cells, signal registration,
   native thread entry/exit, cancellation watchers or landing pads. The later
   owned commits change only tests/evidence.

This does not prove that the existing generic call path is defect-free. It
specifically separates the new join repair from the still-open cancellation
qualification failure.

## Cancellation route and discrimination points

- api.go CompileOptions sets Interruptible to !HostInterruptSupported(). Linux
  runtime/interrupt_linux.go returns true (Darwin ARM64 does too). Consequently
  ARM64 memory.go:emitInterruptCheck emits no cooperative entry/loop polling for
  this public compiled module. Explicit bounds checks do not enable cancellation
  polling. host_interrupt_linux_test.go explicitly tests this public policy.
- api.go:startCancellationWatch installs context.AfterFunc. Cancellation calls
  RequestInterruptAsync(activeTrap), which writes TrapInterrupted and retains a
  request token. The Linux worker retries queued broadcasts, up to 256 attempts,
  with a 50-microsecond ticker and acknowledgement checks. These are existing
  code constants, not measurements made by this worker.
- broadcastInterruptSignal enumerates /proc/self/task and invokes
  SYS_RT_TGSIGQUEUEINFO with SI_QUEUE and the authenticated token. It discards
  syscall results. Thus current test logs cannot distinguish failed delivery from
  a delivered-but-unmatched handler. No syscall errno was captured by this audit.
- interrupt_linux_arm64.s first checks SI_QUEUE/SI_TIMER and cookie, then saved
  PC membership in executableCodeRanges. It validates saved X26 against the
  registered linear memories, [X26-104] against the active request trap, and the
  complete token. Only then does it set ack, put linMem in saved X9 and redirect
  saved PC to nativeInterruptTrap. That landing pad restores the foreign stack
  and jumps to the invocation continuation.
- Admission and GC progress establish that the native path entered and other Go
  work ran. They do not establish AfterFunc execution, queued delivery, cookie/
  token matching, executable-PC matching, acknowledgement, or successful unwind.
  Setting the trap cell alone cannot stop a native numeric loop without polls.
- Darwin uses a materially different mechanism: requestDarwinInterrupt enumerates
  Mach task threads, samples/suspends eligible generated-code threads and rewrites
  their saved state. Linux user-mode QEMU does not exercise that implementation.

The evidence localizes the unresolved gate to cancellation publication/delivery/
matching/unwind, but does not establish which stage failed or a QEMU defect.
The parent's unchanged native ARM64/Linux and Darwin discriminator is necessary.
If native Linux also fails, request errno plus request/ack and handler-match
witnesses are the next bounded runtime diagnostic (outside this worker's source
ownership). If native passes, retain the QEMU failure and obtain guest syscall/
signal evidence before attributing it to emulation. Neither outcome justifies
weakening deadlines or accepting cancellation failure.

No verified additional production finding, no requested outside-ARM source repair,
and no new measurement. Fix cost remains unknown until the native discriminator;
the assigned backend source slices and their existing QEMU verdicts are complete.
