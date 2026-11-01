# Concrete diagnosis for production owner

Status: **production defect reproduced; aggregate gate remains BLOCKED**.
No production edits are made by this investigation. Evidence/harness-only work
cannot affect production performance.

The unchanged current d546a1af standalone runner built by TinyGo 0.43.0 aborts
in process replay (iteration 23, zero based). GDB reproduces SIGSEGV in
`runtime.scanConservative`, gc_blocks.go:693, while loading a root from the
address range derived from a paused guest thread. Its stack is on Wago's
foreign mapping; TinyGo retains that thread's original OS stack top.
`internal/task.GCStopWorldAndScan` scans between these unrelated bounds.
The faulting read in this capture is at 0x7fa960e00000.

The GC-triggering thread is the test's main thread:
`main.main:29 -> context.WithCancel callback -> cancelCtx.cancel -> sync.Once.Do
-> internal/task.start -> runtime.alloc -> runGC -> GCStopWorldAndScan
-> scanConservative`. In other words, ordinary cancellation allocates its
`context.AfterFunc` callback task while the guest is still spinning. The guest
thread is visibly paused in native machine code beneath `tinygo_task_gc_pause`.
No forced GC or source instrumentation was used in this reproduction.

TinyGo's SIGSEGV handler translates this into the generic nil-pointer panic,
which explains why the initial log named a nil dereference without identifying
the scanner. The original deleted executable/core cannot be recovered here;
this is a reproduction of the same test/failure class, not an assertion about
the unavailable original instruction address.

Pinned TinyGo 0.42.0, verified release archive plus CI task-exit patch, also
faults in `runtime.scanConservative` (gc_blocks.go:646) when `runtime.GC()` is
inserted immediately before cancel, after the original guest-start delay.
That intervention diagnoses the boundary; it is not a replacement test that
waives the original failure. Baseline 9713878ab fails the same forced-GC probe immediately under 0.42.0.
Its unchanged 0.43.0 runner also aborts in process replay (iteration 39), and
GDB confirms the same scanner fault. The before-entry GC control passes ten
executions. These controls localize the unsafe boundary; they are not a claim
that a finite number of passes proves safety.

Wago's existing `src/core/runtime/stress_tinygo_test.go:11-21` explicitly
documents this unsafe concurrent-GC/foreign-stack configuration. The
interruption test exercises it implicitly through normal cancellation
allocation. Changing TinyGo versions, adding retries, suppressing collection,
precollecting, increasing heap space, removing timeout allocation, or moving
one ticker allocation would only alter exposure; none establishes safe stack
scanning under arbitrary concurrent allocation/GC.

## Proposed production fix scope (for parent/native owner)

Implement a TinyGo-aware foreign-stack GC integration at **all** native
entry/exit/resume boundaries: preserve the original host stack span and its
live roots; publish the foreign stack span atomically with the switch; ensure
TinyGo's stop-the-world scan observes valid separate spans including saved
registers; restore state on ordinary return, traps, cancellation and host-call
reentry. This may require a version-pinned TinyGo runtime hook/patch because
its task stack scan currently assumes one contiguous OS stack. An alternative
is a properly bounded TinyGo entry path that remains on a correctly sized
registered host stack. Either requires design and production-owner work;
blindly replacing stackTop or ignoring foreign-stack roots is unsafe.

Acceptance requires unchanged cancellation runner plus forced collection
while the guest runs and host-call/reentry cases, baseline/current comparison,
and the full pinned lane. No proposed fix is applied or claimed verified here.

## Source relation and sibling triage

`relevant-source-delta.diff` is empty: baseline 9713878ab and current d546a1af
have identical interruption runner, TinyGo trampoline, Engine, cancellation
watch and native-call wrapper sources. Baseline executables independently reproduce both the unchanged 0.43.0
failure and the controlled 0.42.0 scanner fault. This establishes a pre-existing
defect, not a Snap introduction. It does not waive the port qualification gate.

Read-only sibling audit is in `sibling-audit.txt`: Slipstream's manifest marks
this TinyGo artifact case not applicable; Starshine has no matching TinyGo
foreign-stack/cancellation surface in its inspected src/tests. Neither provides
a transferable TinyGo collector fix. No sibling edits or execution occurred.
