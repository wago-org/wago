# Synchronous host-call performance

## Invariants

These constraints apply before and after each optimization:

1. A callback value carries an immutable generation snapshot. It expires on
   return and cannot gain authority when a later callback starts.
2. Re-entry requires the active invocation chain. The public root identity,
   active callee, reservation, and collector lease owner are distinct concepts.
3. Activation counts describe parked callbacks, not native execution ownership.
   Native and collector leases remain required.
4. Cached native pointers may be reused only when all their owners and values
   remain valid. Unknown mutation takes the existing restoration path.
5. Parked native roots remain published through lease reacquisition. Host Go
   code runs only after Go scheduler state has been restored.
6. Unproven native work releases the scheduler. An entry proof does not prove a
   host-return continuation or its callers.
7. Panic, trap, exit, cancellation, and nested calls must run the same cleanup.
8. Every shortcut needs a conservative fallback. State exhaustion must not
   silently remove identity or authorization checks.

## Measurement method

`BenchmarkHostRoundtripLoop` uses one compiled module and the same `run` export,
public API, and instance options for counts 0, 1, 8, 64, and 1024. Its second
argument chooses a host increment or the matched guest increment. The returned
sum is checked. Counts do not change host admission. Zero-call time estimates
fixed invocation cost. Subtract guest slope from host slope to estimate repeated
boundary cost; do not subtract a different cheap export from a host export.
Memory counts 0, 1, and 4 expose directory refresh scaling. Parallel runs share
compiled code but use one independent instance per worker.

Retain `BenchmarkInvokeHostFuncDirect` for real single-call public API cost.
Run benchmarks without concurrent test or build jobs, with the same CPU count,
and retain ns/op, B/op, and allocs/op at each stage.

## Native context validity

The shortcut requires independent execution both before parking and after lease
reacquisition, no WasmGC collector, no threaded memory, and an unchanged local
version. Independent admission excludes imported memory, globals, tables, and
native functions. Publishing shared native control or exporting resources
revokes admission. Uncertain cases retain the old full restore path.

The cached words are the custom control frame, table and function descriptors,
passive element/data directories, globals pointer directory, table and memory
directories, import dispatch directory, and native GC view. These directories
and the backing reservation addresses have stable instance lifetimes. Invocation
admission keeps them live across a callback. Table growth and global writes
update the pointed-to storage; their shared/exported forms revoke independent
admission. Memory reservations do not move on growth. Indexed memory sizes do
need synchronization, so every nested native entry invalidates the version and
the old memory-directory refresh runs on return. The refresh visits all memory
entries and is linear in memory count.

`bindNativeContext` invalidates before binding. `prepareHostReentryState`
invalidates before swapping the engine, control frame, trap, and immutable
context image. Guarded host storage access invalidates under the native lease,
even when it only reads. Invalidating before acquiring that lease would be
insufficient: a pending writer could change state after the park snapshot.
Byte writes through `HostModule.Memory` do not change cached pointers or sizes.
There is no public host `Memory.Grow` API; host-directed growth uses a nested
Wasm invocation. Go GC does not move these off-heap reservations. WasmGC,
collector changes, shared resources, and threaded memory use the safe path.

The version saturates at uint64 maximum, permanently disabling reuse. The tests
use that state as the forced-restore control. Trap publication, interruption,
root publication, result validation, collector lease resume, native lease
reacquisition, `prepareHostResume`, and scheduler transitions are unchanged.

## Scheduler segment experiment: disabled

Existing prepared-entry proofs admit small, acyclic static call graphs, with
bounded work and call depth. Some immutable-table calls have a complete target
set. The proof is attached to a function entry, not to a saved host-return PC.
Neither `enterNative` nor `resumeNative` was changed by this work.

The new `shared.AnalyzeNativeSegment` checks an explicitly supplied trusted
graph. It rejects unknown cost, unknown targets, cycles, missing boundaries,
overflow, and excess work. Analysis is capped at 4096 nodes, 8192 edges, and
128 recursive visits in depth. Its temporary arrays die with the analysis; no
cache is retained. It is not called by runtime admission or module compilation.
The verifier is a separate compiler experiment. Its result is not an admission
token, and no public API can use a supplied graph to bypass scheduler release.

Enabling segments still requires all of the following:

- A backend producer of conservative native work costs, including prologues,
  epilogues, runtime helpers, memory slow paths, GC work, and tail transfers.
- Exact entry and resume-PC maps that survive relocation, inlining, code
  compaction, codec reload, and both architecture ABIs.
- Proof for the complete remaining native stack: returning from a callee may
  resume a caller's loop, recursive cycle, indirect call, or unknown helper.
  A local `return` is not necessarily a scheduler-safe boundary.
- Revalidation of dynamic target sets and cross-instance resource ownership.
  Unsupported indirect calls, call_ref, imported targets, tail calls, and GC
  helpers must fall back. Scalar host signatures prove none of this.
- Differential execution, cancellation, GC, and scheduler-progress checks for
  each newly admitted entry and continuation. Arbitrary host code must still
  return to the normal Go runtime context.

`BenchmarkHostSchedulerPotential` compares the existing proven whole-entry
path with its scheduler-releasing control on the same prepared export/API.
It estimates one possible source of savings. It is not a host-call benchmark,
and its difference must not be claimed as an isolated host-call improvement.

## Recorded measurements

Host: Linux/amd64, AMD Ryzen 7 8845HS, Go 1.27.1, GOMAXPROCS=16.
Numbers below are medians of three 100 ms samples. Other user workloads were
active on this host; these are not isolated lab measurements or significance
tests. Stage benchmarks ran apart from our full test jobs (the reservation
allocation check overlapped an external TinyGo build). The original pre-fixture
single-call samples were 503.6, 517.6, and 515.4 ns/op, 112 B/op, 1 alloc/op. The matched
baseline series below is the comparison basis.

Stages are cumulative. Each row was measured before the next runtime change.
The final row also includes the inline reservation correction. The experimental
analyzer adds no runtime work and enables no scheduler shortcut.

| Stage | Public one-call ns/op | Delta vs prior | B/op | allocs/op | Repeated host ns/call, memory 0 | Parallel aggregate ns/call, 16 instances |
|---|---:|---:|---:|---:|---:|---:|
| Baseline | 524.8 | — | 112 | 1 | 255.3 | 304.3 |
| Local activation/reservation | 472.1 | -10.0% | 112 | 1 | 224.6 | 56.8 |
| Context pass-through | 409.2 | -13.3% | 112 | 1 | 236.7 | 55.9 |
| Compact token/local generation | 434.3 | 6.1% | 64 | 1 | 212.7 | 45.6 |
| Context validity | 411.4 | -5.3% | 64 | 1 | 199.6 | 43.9 |
| Combined + inline reservation | 400.9 | -2.6% | 64 | 1 | 187.9 | 37.9 |

Context pass-through has mixed short-sample results: the public call improved,
but the memory-0 repeated median regressed 5.4%; memory-1 and memory-4 improved.
The compact-token public median also regressed 6.1%, while its repeated and
parallel measurements improved. Do not claim a statistically proved latency
improvement for each of these stages. Allocation size and removed shared state
are directly measured; the combined result is faster across the matched cases.

### Matched memory-0 calls per invocation

Times are ns per public invocation. Allocations are for the host arm; the guest
control has 0 B/op and 0 allocs/op. Parallel times measure aggregate throughput,
not the latency experienced by a worker. Parallel allocation bytes can include
small benchmark-worker overhead.

| Calls | Baseline host | Combined host | Baseline guest | Combined guest | Baseline B/op | Combined B/op | allocs/op | Baseline parallel host | Combined parallel host |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 0 | 121.2 | 114.5 | 124.4 | 114.7 | 0 | 0 | 0 | 20.2 | 21.2 |
| 1 | 513.7 | 410.6 | 120.4 | 117.7 | 112 | 64 | 1 | 414.5 | 74.3 |
| 8 | 2351.0 | 1763.0 | 125.1 | 119.0 | 896 | 512 | 8 | 2697.0 | 352.0 |
| 64 | 16838.0 | 12338.0 | 147.2 | 144.0 | 7168 | 4096 | 64 | 19571.0 | 2487.0 |
| 1024 | 262030.0 | 192994.0 | 610.0 | 573.5 | 114688 | 65536 | 1024 | 311645.0 | 38804.0 |

### Memory directory scaling

| Memories | Baseline fixed ns | Combined fixed ns | Baseline host slope ns | Pre-validity slope ns | Combined slope ns |
|---:|---:|---:|---:|---:|---:|
| 0 | 121.2 | 114.5 | 255.3 | 212.7 | 187.9 |
| 1 | 118.2 | 113.5 | 279.8 | 211.9 | 188.0 |
| 4 | 142.3 | 142.2 | 294.3 | 231.9 | 189.7 |

The guest loop contributes about 0.45–0.51 ns/iteration. The private unchanged
context path removes the memory-count-dependent restore work; initial entry
still refreshes the directory. This is not a claim about shared/GC instances.

The reservation-only check exposed 192 B/op and 2 allocs/op in the first local
map version (median 102.1 ns/op). The corrected inline version measures 14.17
ns/op, 0 B/op, 0 allocs/op. Fallback maps release when empty. This correction
prevents a new allocation penalty for reservation-bearing plugin calls.

The scheduler-potential control measured 17.78 ns/op with scheduler release and
6.936 ns/op with the existing whole-entry proof, both 0 B/op and 0 allocs/op.
That 10.84 ns difference is for one prepared entry, not a host round trip.
There is no bounded-segment runtime before/after number: the path is disabled.

The standalone 64-node segment-analysis benchmark measured 532.3 ns/op,
320 B/op, and 2 allocs/op. These are analysis-only allocations. No segment
metadata, analysis, or new scheduler work is added to public invocation.

## Security and footprint review

| Change | Abuse case | Protection and fallback | Residual limit |
|---|---|---|---|
| Local activations | High callback rate, distinct chains, nested counts, lock contention | Per-instance mutex; exact ID/count; overflow panics before state changes; inline common entry; fallback entries removed on unwind | Fallback cardinality follows simultaneously parked distinct chains, not total historical calls. A blocked host can keep its live activation; no new global or permanent cache. |
| Local reservations | Reservation substitution across instances or IDs | Same `(instance, ID)` scope, with the instance in the owner rather than the map key; saved reservations restored on unwind | Inline common entry; live distinct IDs use a map released when empty. No added allocation in the ordinary reservation path. |
| Context pass-through | Active callee impersonates the root/public caller | Only the runtime builds the context; root ID, callee instance, reservation, and GC lease owner retain their separate roles | Cross-instance context registry still serves nested/foreign calls; it is not an execution lease. |
| Compact token | Retained A becomes valid for B; GC keeps stale handles alive; generation rollover | Boxed value is immutable, never pooled; signature is immutable; exact scope and generation checks remain; local sequence never rolls back and fails on exhaustion | One 64-byte allocation per scalar callback remains. Retention can keep the instance alive as before; it grants no later callback authority. |
| Context validity | Host mutates memory/resources, re-enters, or collects while parked | Private ownership plus version equality; full restoration on mutation, sharing, GC, threading, or saturated version | New pointer-changing operations must invalidate or revoke admission. Unsafe host byte misuse remains outside the API contract. |
| Segment analysis | Hidden loops, recursion, unknown helper costs, dynamic targets, unbounded continuations | Unknown work/targets/cycles fail; graph size, depth, edges, and cost are bounded; runtime optimization stays disabled | No compiler producer or continuation admission proof exists. No scheduler or GC progress claim is made for segments. |

No native lease, collector lease, interruption check, root publication, result
validation, or scheduler transition was removed. Synchronous recursion still
uses the existing separate native stacks and GC activation limits. This patch
does not introduce a new recursion policy or make an indefinitely blocking host
callback cancellable. Cleanup remains proportional to live nested activations.

On amd64 the callback value shrank from 104 bytes (112-byte allocator class) to
64 bytes. `Instance` remains 888 bytes. The lazy plugin sidecar grows from 136
to 208 bytes: 56 for local activation/reservation state, 8 for the callback
sequence, and 8 for the context version. The existing exact footprint test now
checks 208 explicitly. This fixed 72-byte cost replaces process-wide map entries
and reduces per-callback allocation by 48 bytes. There is no new cache or goroutine.

## Final profiles and supported follow-up work

The final 3-second memory-0/1024-call CPU profile recorded 5.92 CPU seconds
including calibration and GC workers. `dispatchSynchronousHostCall` accounts
for 64.53% cumulatively (8.28% flat); scalar dispatch 21.11% cumulatively;
scope construction through `beginHostCallScopeReservedWithID` 10.81%;
`suspendGCInvocation` 10.30%; `resumeNative` 9.97%. Go reports 8.78% as
unresolved external code, so this profile does not identify individual JIT
instructions. These cumulative percentages overlap and must not be summed.

The allocation profile attributes 99.64% of allocated bytes to scalar callback
boxing. Escape analysis before and after confirms the `caller` interface value
escapes through the arbitrary `HostFunc` call. Keeping this immutable allocation
avoids giving a retained pointer a later callback's generation.

The final parallel mutex profile attributes 92.72% of sampled delay to
`runtime.unlock` and 7.28% to lost contended runtime locks. No Wago activation
mutex appears among the leading delay sites. The latency with mutex profiling
is not used as an uninstrumented speed claim.

Known likely improvement to investigate: specialize the no-GC lease/root setup
for a proven private scalar instance. The no-GC fixture still spends 10.30%
cumulatively in `suspendGCInvocation`; any shortcut must preserve imported-domain
leases and handle later sharing. Scope setup also merits a measured inlining
review. Neither change is included here.

Experimental ideas supported by the profile: a smaller immutable callback
capability to reduce boxing/GC pressure, and proof-backed scheduler segments to
reduce resume overhead. A zero-allocation retained-capability design and full
continuation proofs remain unresolved. Do not pool mutable callback tokens or
use host signatures as scheduler admission evidence.

## Tests and commands

Added tests:

- `TestInstanceActivationsMatchCountedIdentity` and
  `TestInstanceActivationsConcurrentIndependent`: counted-model comparison,
  zero/stale IDs, overlapping identities, concurrency, and complete cleanup.
- `TestInstanceReservationScope` and `TestInstanceReservationNestedIdentities`:
  reservation ownership, nesting, restoration, and fallback-map release.
- `TestHostInvocationContextCrossInstanceChain`: A → host → B → host → A,
  root identity, active owner, nested generation suspension, and cleanup.
- `TestRetainedHostTokenCannotGainLaterGeneration`: retain A, return, invoke B,
  then reject A's memory, re-entry, externref, GC, and guest-storage operations.
- `TestHostScopeGenerationMonotonicAndExhaustion` and `TestHostTokenSize`:
  nested generation restoration, non-reuse, exhaustion, and the 64-byte budget.
- `TestHostContextResumeMatchesForcedRestore`: no-op, read, write, guarded
  storage, grow, nested invocation, global mutation, export, Go GC, panic,
  HostExit, HostTrap, and cancellation; expected values plus safe-path equality.
- `TestHostContextVersionSaturates`: overflow cannot admit cached context.
- `TestHostImportsRemainOutsideBoundedSchedulerAdmission`: simple, looping,
  and multi-memory host functions keep scheduler fallback.
- `TestAnalyzeNativeSegment`, `TestAnalyzeNativeSegmentBoundsAnalysis`, and
  `TestNativeSegmentEntryDoesNotProveContinuation`: branches, loops, recursive
  cycles, unknown helpers/targets, non-boundary returns, work overflow, resource
  bounds, and a bounded prefix with an unbounded continuation.

Commands run from the repository root, unless stated otherwise:

```sh
# Original, then improved A/B/C/D/E stage series; each has its own log.
go test ./src/wago -run '^$' -bench '^BenchmarkInvokeHostFuncDirect$' -benchmem -benchtime=300ms -count=3
go test ./src/wago -run '^$' -bench 'Benchmark(InvokeHostFuncDirect|HostRoundtripLoop)$' -benchmem -benchtime=100ms -count=3
go test ./src/wago -run '^$' -bench 'Benchmark(InvokeHostFuncDirect|HostRoundtripLoop|HostSchedulerPotential|HostInvocationReservation)$' -benchmem -benchtime=100ms -count=3
go test ./src/wago -run '^$' -bench '^BenchmarkHostInvocationReservation$' -benchmem -benchtime=300ms -count=3
go test ./src/core/compiler/backend/railshot/shared -run 'Test(AnalyzeNativeSegment|NativeSegmentEntry)' -bench '^BenchmarkAnalyzeNativeSegment$' -benchmem -benchtime=300ms -count=3

# Full suite at baseline and each runtime stage. The corpus was absent initially.
go test ./...
git submodule update --init tests/conformance/spec-v3
WAGO_SPEC_INTERPRETER=/home/jtenner/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa go test ./...
(cd bench && go test ./...)

# Focused stage checks used the added test names above and existing nested,
# cross-instance, caller-context, scalar allocation, guest-storage, and host-ref tests.
go test -race ./src/wago ./src/core/runtime ./src/core/compiler/backend/railshot/shared -run 'Test(InstanceActivations|InstanceReservation|HostInvocationContextCrossInstanceChain|RetainedHostToken|HostScopeGeneration|HostTokenSize|HostContext|HostImportsRemain|AnalyzeNativeSegment|NativeSegmentEntry|NestedHost|IndependentHost|HostExit|CallerResolverInvocationContext|CrossInstanceHostDispatch|HostReentryRefreshes|InvokeContextHostPanic)' -count=1
go test -race ./src/wago -run 'Test(HostContext|InstanceReservation|HostInvocationContextCrossInstanceChain|RetainedHostToken|HostScopeGeneration|HostImportsRemain)' -count=5
go test -tags wago_guardpage ./src/wago ./src/core/runtime -run 'Test(HostContext|HostScopeGeneration|RetainedHostToken|HostInvocationContextCrossInstanceChain|InstanceActivations|InstanceReservation|NestedHost|HostExit|HostImportsRemain|IndependentHost)' -count=1
GOOS=linux GOARCH=arm64 go test -c -o /tmp/wago-host-arm64.test ./src/wago
go test ./src/wago -run '^$' -fuzz '^FuzzCompiledCodecGeneratedValidModules$' -fuzztime=5s -parallel=2
go vet ./src/wago ./src/core/compiler/backend/railshot/shared
git diff --check
make docs-check

# Escape analysis ran before and after token compaction.
go build -gcflags='-m=2' ./src/wago
go test ./src/wago -run '^$' -bench '^BenchmarkHostRoundtripLoop/mem0/parallelfalse/host1/n1024$' -benchtime=3s -cpuprofile=/tmp/wago-host-final-cpu.pprof -memprofile=/tmp/wago-host-final-alloc.pprof -o /tmp/wago-host-profile.test
# Run the compiled test binary from src/wago (it has relative fixture paths).
(cd src/wago && /tmp/wago-host-profile.test -test.run '^$' -test.bench '^BenchmarkHostRoundtripLoop/mem0/paralleltrue/host1/n1024$' -test.benchtime=2s -test.mutexprofile=/tmp/wago-host-final-mutex.pprof)
go tool pprof -top /tmp/wago-host-profile.test /tmp/wago-host-final-cpu.pprof
go tool pprof -top -alloc_space /tmp/wago-host-profile.test /tmp/wago-host-final-alloc.pprof
go tool pprof -top /tmp/wago-host-profile.test /tmp/wago-host-final-mutex.pprof
```

The generated-module codec fuzz run passed 34,481 executions in 5 seconds.
It checks codec round trips, not adversarial scheduler execution. Focused race,
guard-page, and arm64 cross-build checks passed. No native arm64, Darwin,
Windows, full race suite, or newly optimized scheduler-path execution was run.
Existing scheduler-proof and parked-WasmGC tests ran in the full native suite.

Intermediate failures were kept visible and corrected: the new exact footprint
budget needed updating; an externref test assumed an error class the public API
does not promise; the cancellation test initially failed to wait for asynchronous
interrupt publication. Running the compiled profiling binary from the repository
root also failed on a relative fixture path; rerunning from `src/wago` passed.
No test failure was converted to a skip.

Final full-suite result: `src/wago` passed in 6.763 seconds with the pinned
reference interpreter. The overall `go test ./...` still fails on the three
baseline installer/toolchain tests: Wine CMD cannot download its installer,
and both TinyGo standalone builds report duplicate `tinygo_task_exit` symbols.
The standalone package took 64.235 seconds; inspection traced that time to the
external TinyGo build commands, not Wago JIT compilation. No runtime test remains
failing. `make docs-check` and `git diff --check` passed.

## Change locations

| Work | Implementation | Tests and documentation |
|---|---|---|
| Matched benchmarks | `src/wago/host_roundtrip_bench_test.go` | This report; `CONTRIBUTING.md` |
| Local activation/reservation | `src/wago/instance_native_context.go`, `src/wago/hostcall.go` | `src/wago/host_activation_test.go` |
| Context propagation | `src/wago/host_execution.go`, `src/wago/hostcall.go`, `src/wago/instance.go` | Chain test above; adjusted injected dispatch signatures in `gc_helper_dispatch_test.go` and `hostcall_hardening_test.go` |
| Compact callback | `src/wago/hostcall.go`, `src/wago/guest_storage.go`, `src/wago/guest_storage_alloc.go` | `src/wago/host_token_test.go`; updated signature fixture in `guest_storage_gc_amd64_test.go` |
| Context validity | `src/wago/instance_native_context.go`, `src/wago/host_execution.go`, `src/wago/hostcall.go` | `src/wago/host_context_resume_test.go`; budget in `gc_array_reference_amd64_test.go`; `ARCHITECTURE.md` |
| Disabled scheduler experiment | `src/core/compiler/backend/railshot/shared/native_segment.go` | Adjacent `native_segment_test.go`; `src/wago/host_scheduler_bench_test.go`; proof limits in this report |

The four runtime changes are enabled with conservative fallback. The fifth is
analysis/measurement infrastructure only. Scalar callback allocation count did
not fall: the delivered improvement is smaller immutable allocations, not a
zero-allocation claim.
