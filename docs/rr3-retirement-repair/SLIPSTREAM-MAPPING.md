# Current-master release and cancellation comparison

Authoritative Slipstream pin: `d6802f5e389bded786f49b4c2a21df0efab7017c`.
The complete eight-site audit in thread
`mcp:c34c2783-bef7-43c2-8491-870dafb26ce3` was consumed before the
post-comparison matrix. Verdict: **RR3 ABSENT**. Pinned source was independently
read through `git show` in `/home/dev/Projects/slipstream`; source hashes are in
`slipstream-source.json`. No sibling files were edited. This supersedes the
initial realworld-branch 4db8 comparison for this repair.

| Slipstream source at the exact pin | Ordering and wait invariant | Wago mapping |
| --- | --- | --- |
| `gc/native.goose:878-905`, `1408-1412`; `src/jit_gc.goose:351-357` | Region creation validates and publishes metadata; region drop unmaps card metadata then marks inactive. Owner release loops over drops. No lock, join or worker-progress wait in release. | Snapshot registry removal and claim decrement under the short store mutex. No topology writer wait, domain wait, worker join or native wait in unpin. Actual unlink requires a separate retirement claim because Wago admits concurrent topology readers. |
| `src/jit_gc.goose:1477-1481`, `900-910`; `src/jit.goose:885-898`; guest lock `native/jit_linux_x86_64.S:89-90`, implementation `native/gc_linux_x86_64.S:18-63` | Element drop, active-element transfer and constant ownership release can occur under the recursive mutator lock; their release adds no progress-dependent wait edge. | Caller return, partial-admission cancellation and callback old-pin release must not become topology writers. The retirement worker likewise reserves no writer-priority ticket, preserving reader admission needed by a signaller. |
| `gc/native.goose:1414-1434`; `src/jit_gc.goose:1483-1503` | Resize prepares replacement metadata, then publishes/unmaps old metadata; failure precedes table descriptor publication. | Existing receipt prepare/commit and all-or-nothing pin admission remain unchanged. Failed pin acquisition unwinds acquired gates before nonblocking pin release. No runtime/backend table-resize rewrite. |
| `src/jit_store.goose:17-27,38-53`; `src/jit.goose:683-705,756-761,863-883,911-920` | Failed load/initialization/start rolls back owner roots through the same nonblocking region release before resource rollback. | Canceled or partial domain admission releases gates, removes the snapshot, decrements all pins, and leaves only registered retirement candidates for the worker. Cancellation never waits for the worker or a topology writer. |
| `gc/native.goose:760-779`, worker `669-677`; `src/jit_gc.goose:1516-1521` | Close publishes stop, wakes helpers, then joins without holding the mutator lock. Helpers need no mutator lock to exit. | Collector Close and token/finalizer release run only after store/topology locks are dropped; domain/native locks were already released before unpin. Callbacks may enqueue further work without recursion into held retirement locks. |
| `gc/native.goose:1171-1186,629-643,650-689` | Collection joins under its mutator lock, but helpers never acquire that lock; no reverse dependency. | **Not copied.** Wago native entrants can need topology reader admission to make shared-memory progress. Its retirement worker cannot queue as a writer or hold another lock while waiting for gate change. Collector implementation unchanged. |
| `src/jit_gc.goose:366-401,1152-1154` | ABI1 failed-runtime snapshot/restore is serial; no concurrent release/worker cycle. | No direct concurrent collector analogue; existing Wago snapshot lifetime/identity proof retained. |
| `src/parallel.goose:33-83,141-175`; `src/jit_parallel.goose:267-325` | Validation/JIT workers join before snapshot/staging teardown, without Store/collector locks; helpers use immutable metadata/atomic budget leases. | No compiler/JIT-worker changes. Wago retirement notification waits hold no store, topology, domain, native or graph lock. |

Wago-only adaptation: per-invocation domain claims, a topology reader/writer
gate, writer priority, a coalesced retirement worker, and shared-memory calls
across independently admitted instances/Runtimes have no counterpart in the
reviewed Slipstream subsystems. They are represented by
`reference_store.go`'s snapshot registry and `gc_invocation_retirement.go`, not
by invented equivalents in the collector. The worker keeps each queued domain
registered and its heap live; under an acquired writer it revalidates exact
registration plus zero refs/claims before unlink. Revival or another retirement
invalidates the candidate. Only successful unlink owns Close. Notification
subscription and failed try-acquisition are atomic under the gate mutex, and
queue/idle transitions share the store mutex, avoiding lost wakeups.

Development-1/2/3 preceded this comparison. They remain historical development
receipts, including the expected failing immutable 9ad nested-root control.
Post-comparison lifecycle cells are independently rerun; no historical pass is
renamed or promoted. Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03` remains a
model/codec/validator boundary (`docs/wiki/wasm-gc-core-boundary.md:45,57,115`),
without a native retirement-store analogue.
