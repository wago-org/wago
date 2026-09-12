# Runtime entry safety audit

Base: `788a3bfb0` (main). This report records the fixes and the limits of the investigation.

## Confirmed defects

* Prepared scalar and direct integer calls used ownership decisions saved at preparation. After `ExportedFunc`, these calls could omit native locking and context binding. Every prepared entry retains lifetime protection. The isolated direct integer path now reserves invocation ownership and private entry together in the existing invocation gate. Other specialized paths retain their execution-flag reservation under the general gate. Sharing revokes direct gate admission before setting the shared execution flag, then waits for older reservations. Shared calls use guarded general entry. ARM64 call-block entry uses the same boundary as its other direct entries.
* Optimized typed and `HostCallFunc` callbacks kept the independent native mutex held. A closure that called `GlobalValue` or `SetGlobalValue` then deadlocked. Each optimized portal now releases the local native lease before Go code and defers reacquisition and context restoration. The saved state is a stack value. Shared execution keeps its existing global lease protocol. Public host access still takes its lock; callback activity grants no authority to another goroutine.
* `Call(ctx)` waited on a mutex after cancellation. The new invocation gate has a context-aware wait and uses an atomic reservation for uncontended admission. It checks cancellation before and after reservation. Cancellation visible at either check wins and releases any acquired slot. Contended wait registration and release use the same atomic state word to prevent missed wakeups. Cancellation after that decision is checked again before guest entry. A canceled waiter does not modify the owner's identity or trap. The uncontended gate allocates nothing. Contenders share one notification channel for the current wait interval; there is no waiter goroutine.
* The full race suite found a separate structural-call-identity cache race during concurrent instantiation. Writers held the code-cache mutex, but readers loaded the published cache pointer without synchronization. The pointer is now atomic. One acquire load reads a complete immutable cache. The writer lock, retention budget, lazy second-use construction, and exact identity comparisons remain in place. This changes neither object size nor allocation count.

The invocation gate adds 16 bytes to `instancePluginState` on amd64. The fast-entry reservation reuses the existing atomic execution flags and gate wait state. No new per-call allocation is required for `Invoke`, prepared entry, or optimized host callbacks. `Call` retains its existing typed-value allocations.

## Narrow direct prepared entry

The compiler's direct-entry metadata and isolated module shape establish that the entry cannot call a host, enter another instance, or use GC facilities. The module has no imports, collector, external global cells, or external memory. Isolated local tables require the existing compiler proof. The integer signature alone is not a safety proof. Those construction-time facts are immutable; current sharing and GC-domain exclusions are checked at each entry. Non-private GC domains are registered during construction. A late boundary-created store is private and cannot add GC domains to a module with no collector or imports. Resource sharing revokes direct admission before publishing a shared handle.

The gate state machine uses `Held`, `Fast`, `Waiters`, and permanent `Revoked` bits:

1. Acquire the existing lifetime lease. Its instance-local atomic count prevents physical teardown. Runtime-owned instances also retain runtime operation accounting.
2. CAS an idle, unrevoked gate to `Held|Fast`. This owns the same gate as ordinary calls. Check the current execution exclusions; release and fall back if any exclusion applies.
3. Enter native code without allocating an invocation ID, acquiring a GC domain lease, or taking a second ownership reservation.
4. Release the gate with CAS while preserving `Revoked`. Notify registered waiters if required. Release the lifetime lease after native and result processing finish.

Sharing sets `Revoked` in this same word. If it observes `Fast`, it registers for notification and waits until the call releases the gate. Thus sharing either prevents entry or waits for it; there is no load-to-native-entry gap. General admission can acquire a revoked gate, but direct entry cannot. A direct call cannot bypass a general call or its fallback. General waiters remain cancellable.

The omitted invocation ID has no observer: this entry cannot execute callbacks, cross-instance calls, or callback reentry. The omitted GC helper has no domain to own: isolated module metadata excludes local GC and the current flags exclude imported, dynamic, and store-owned GC. No synchronization has been removed from ordinary host callbacks.

Lifetime is independent of invocation ownership. The existing instance count and close bit order entry against physical release, including runtime shutdown. Runtime-owned instances still use the runtime mutex and operation count: plugin teardown drains these operations, not only the instance's native gate. This change does not substitute an unproven local lease for that runtime contract. The standalone hot path uses only local atomic lifetime accounting and the combined gate; it allocates nothing and adds no storage.

`TestPreparedDirectDoesNotAllocateInvocationIdentity` failed before the change and passed afterward. Additional tests cover the shared general gate, revocation and fallback, each GC exclusion bit, and both instance and runtime close while a direct lease is held. Existing tests cover actual concurrent export, context rebinding, trap handling, disabled-direct mode, call-block mode, and allocation counts. The GC-bit test checks the admission predicate, without inventing missing GC topology.

## Native code limit

`WithMaxNativeCodeBytes` is an accepted final output-size limit. It is not a peak compiler memory or work limit. The API comment now states this explicitly. The final check counts the complete module image after parallel workers have joined, including alignment, adapters, literals, and shared stubs. It is not a separate allowance for each worker. Failed compilation does not publish a `Compiled` value. The existing deferred code-image cleanup remains active.

The amd64 and arm64 emitters were inspected for incremental enforcement. Directly charging every reservation against this same limit would change accepted-output semantics:

* Serial emission reserves a mapping tail that includes relocation and local-reference scratch space.
* Assemblers emit through byte slices. A tail-capacity underestimate can use a detached slice and append it later.
* Function finalization and module-wide adapter/trap-body sharing can shrink emitted code. An intermediate image can therefore exceed the final accepted size.
* Parallel workers retain separate code and relocation arenas before final assembly. Charging each arena capacity would count temporary storage and duplicated code as final output.

No incremental emission or peak-memory guarantee is claimed by this change. A safe early cutoff needs a proven lower bound on final size, or a separate temporary-emission budget enforced by both encoders and all slice growth paths. Such a budget also needs shared worker cancellation and cleanup. That work remains separate; a raw reservation cap would incorrectly reject some modules that meet the existing API contract.

`TestNativeCodeLimitIsOneFinalModuleBudget` checks limits one byte above, exactly at, and below the measured final size with one and four workers. It checks that failure returns no artifact and that a subsequent exact-limit compilation executes correctly. `TestCompileByteAndNativeCodeQuotas` continues to cover the final compiler check and the decoded-artifact check. These contract tests pass on the original behavior; they do not claim an early-emission fix.

## Linux job control

The reported shared-foreground failure has not been reproduced. The investigation covered terminal process-group setup, the guardian's child-state loop, `waitWatchedProcess`, stop mirroring, `SIGCONT` handling, foreground restoration, interrupt forwarding/exclusion, and child cleanup. No timeout was increased and no production guarantee was removed.

Runs of the existing test passed with ordinary scheduling, the race detector, and one- and two-CPU scheduling. This evidence does not establish that the CI failure was a timeout or a test defect. Its root cause remains unresolved. A production change without a confirmed failing transition would be speculative.

## Validation

Validation results and remaining toolchain limits are recorded in the pull request. Raw local logs are in `/tmp/wago-entry-validation` and `/tmp/wago-pr617-perf` during this session.
