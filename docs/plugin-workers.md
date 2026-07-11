# Workers plugin

The `github.com/wago-org/wago/workers` package exposes Wago's neutral worker
service as a normal plugin. The plugin uses a narrow core execution service to
authorize callers and own native instances, but plugin selection, identity, and
service ownership live outside the runtime. It deliberately does **not** provide actors, PIDs, guest
mailboxes, signals, monitoring, naming, restart policies, or supervision. A
plugin may build those policies over the neutral primitives described here.

## Registration

A worker handle becomes active only if the entire `Runtime.Use` operation
commits successfully.

```go
workerPlugin := workers.New()
if err := runtime.Use(workerPlugin); err != nil {
    return err
}
service := workerPlugin.Service()
```

Higher-level plugins that compose worker policy can retain the same service and
register handlers during their own registration:

```go
func (e *ActorExtension) Register(reg *wago.Registry) error {
    e.workers = reg.Workers() // the core kernel remains plugin-only

    e.workers.OnMessage(func(ctx *wago.MessageContext) error {
        // Decode ctx.Tag and ctx.Payload, update plugin state, or write into
        // ctx.Caller.Memory(). The caller is suspended on this goroutine.
        return nil
    })

    e.workers.OnExit(func(ctx *wago.WorkerExitContext) {
        // Translate this into plugin-specific links, monitors, logs, restarts,
        // or nothing. Core applies no supervision policy.
    })

    reg.ImportModule("example_worker").
        Func("next", func(caller wago.HostModule, _ []uint64, results []uint64) {
            if err := e.workers.DispatchNext(caller); err != nil {
                results[0] = pluginErrno(err)
            }
        }).
        Results(wago.ValI32)

    return nil
}
```

Applications may also select the standalone plugin by its registered name,
`workers`, through the ordinary plugin registry. The standalone plugin declares
no guest imports: actor, mailbox, and receive ABIs remain the responsibility of
the higher-level plugin using the service. Hosts that select it indirectly can
retrieve the plugin-owned service through the extension ID:

```go
if err := runtime.UsePlugin("workers"); err != nil {
    return err
}
ext, _ := runtime.Extension("wago.workers")
service := ext.(*workers.Plugin).Service()
```

`OnMessage` and `OnExit` registrations are snapshotted when a worker is spawned,
so plugins should normally register them inside `Register`. Each `OnExit`
observer is isolated: if one panics, later observers and shutdown still run. The
recovered panic is retained and returned by `Runtime.Close` as part of its
aggregated error.

## Execution model

A plugin starts a worker from one of its host imports:

```go
id, err := workers.Spawn(caller, tableIndex, wago.WorkerOptions{
    QueueCapacity:   64,
    MaxPayloadBytes: 64 << 10,
    MaxQueueBytes:   1 << 20,
})
```

`Spawn`:

1. identifies the current instance from `caller`;
2. validates that its declared imports are safe to inherit;
3. transactionally reserves the runtime's live-worker and aggregate configured
   queue-byte quotas;
4. creates a fresh instance of that caller's compiled module with the same safe
   imports and GC configuration;
5. resolves `tableIndex` against the new child's initialized table;
6. rejects an out-of-bounds, null, or non-`() -> ()` entry; and
7. starts the selected callback on a dedicated goroutine through a shared native
   dispatcher containing a real Wasm `call_indirect`.

Workers copy only imports declared by the module. Host functions and by-value
`GlobalImport` values are safe to inherit. Spawn rejects imported `*Memory`,
`*Table`, `*Global`/`GlobalImport{Global: ...}`, and `*InstanceExport` values with
`ErrWorkerImportLifetime`: those objects carry external owner lifetimes and an
unlinked worker could otherwise retain native descriptors or mappings after the
owner closes. This is intentionally strict until core has an explicit retention
or ownership-transfer mechanism.

The callback is a one-shot worker entry function. It may loop and call
plugin-defined host imports such as `example_worker.next`. A normal return, or
`HostExit{Code: 0}`, emits `WorkerReturned`; a nonzero host exit, Wasm trap,
message-handler failure, or recovered Go panic emits `WorkerFailed`.

The child owns its `Instance`. No other goroutine closes or invokes that instance.

## Tagged messages

```go
err := workers.Send(id, tag, payload)
```

`Send` is nonblocking and copies `payload` before returning. The caller may reuse
or mutate its original buffer immediately.

Possible sentinel errors include:

- `ErrWorkerNotFound`
- `ErrWorkerStopping`
- `ErrWorkerQueueFull`
- `ErrPayloadTooLarge`
- `ErrWorkerRuntimeClosed`
- `ErrWorkerQuotaExceeded` (from Spawn)
- `ErrWorkerImportLifetime` (from Spawn)

Queues are bounded. Zero-valued options select these defaults:

- queue capacity: 64 messages;
- maximum payload: 64 KiB;
- aggregate queued payload bytes: 1 MiB.

Explicit options are also bounded:

- maximum queue capacity: 65,536 messages;
- maximum payload: 16 MiB;
- aggregate queued payload bytes: 64 MiB.

The queue preallocates only message headers. Payload storage is copied before the
worker mutex is acquired, so a large copy does not hold up Kill, shutdown,
dequeue, or another committed sender. The copy is counted against
`MaxQueueBytes` only when accepted and is released after delivery or worker
termination. A concurrent full/stopping decision can therefore discard a copy;
this preserves nonblocking control paths and caller payload ownership.
`MaxQueueBytes` must be at least `MaxPayloadBytes`, so any otherwise-valid
payload can fit in an empty queue.

## Current worker identity

A plugin host import can resolve its current neutral worker ID without adding a
core PID or process API:

```go
id, err := workers.Current(caller)
```

A direct instance that is not a managed worker returns `ErrWorkerNotFound`. An
actor plugin can expose this ID as a PID, wrap it in a richer identifier, or keep
it entirely internal. `Current`, `Spawn`, and `Link` require an active
synchronous host-call caller; retaining that caller after the import returns is
rejected with `ErrInvalidWorkerCaller`.

## Cooperative delivery

Core cannot run `OnMessage` concurrently with native Wasm without racing the
worker instance. Instead, a plugin chooses a cooperative receive point inside one
of its host imports:

```go
err := workers.DispatchNext(caller)
```

`DispatchNext` waits for a queued message or a stop request. For a message, it
runs all registered `OnMessage` handlers on the worker goroutine before returning
to guest code. A stop request unwinds the suspended callback instead of returning
to guest code. The supplied `HostModule` carries an active generation that is
invalidated before host dispatch returns; `DispatchNext` rechecks it before
worker-state access and message delivery. A retained or asynchronously continued
caller therefore cannot dispatch after Wasm resumes.

`MessageContext.Caller` exposes the restricted `HostModule` surface rather than
`*Instance`, preventing accidental re-entrant guest calls. It has a narrower
handler-only generation: it can be passed to `Current`, `Spawn`, or `Link` while
the handler is active, and its memory/worker authority expires before the
handler returns.

An `OnMessage` error immediately aborts the suspended callback, marks the
worker failed, and prevents further dispatch. Recursive `DispatchNext` calls are
rejected with `ErrWorkerDispatchActive`. Message handlers must not synchronously
call `Runtime.Close`, which waits for the worker goroutine they are currently
occupying; schedule application shutdown outside the handler instead.

Plugins own the guest ABI: blocking versus polling imports, errno values,
selective receive, priorities, decoding, and memory layout do not belong in core.

## Kill

```go
err := workers.Kill(id)
```

`Kill` is a Go plugin API and therefore returns `error`, not a guest errno. The
plugin maps sentinel errors into its chosen ABI.

A successful call:

- records a terminal stop request;
- prevents additional message dispatch;
- clears queued payloads;
- wakes a worker blocked in `DispatchNext` and unwinds its callback; and
- leaves disposal to the worker goroutine.

Repeated calls are idempotent while the worker remains in the stopping state.
After unregistration they return `ErrWorkerNotFound`.

Kill is currently cooperative. Native Wasm already executing outside a host-call
boundary cannot be preempted until engine interruption support lands.

## Lifetime links

```go
err := workers.Link(caller, childID)
```

A link succeeds only if the exact calling instance created the child. Core
rejects self, sibling, ancestor, foreign-extension, unknown-worker, and already
stopping links.

The link has one neutral effect: closing the parent requests that linked children
stop and waits for their disposal. Child failure does not kill the parent. A
plugin can record successful links and translate `OnExit` into its own signal or
monitor messages.

Unlinked children may outlive their creator instance because Spawn admits only
imports with no hidden borrowed native-resource lifetime. A module with imported
memory, tables, global objects, or cross-instance functions must use a different
ownership design; Spawn rejects it rather than creating an unsafe hidden
importer. No worker may outlive its runtime.

## Exit events

Every worker emits one `WorkerExitContext` after its instance is disposed:

```go
type WorkerExitContext struct {
    WorkerID wago.WorkerID
    Kind     wago.WorkerExitKind
    Err      error
}
```

Kinds are:

- `WorkerReturned`
- `WorkerFailed`
- `WorkerKilled`

A linked-parent close waits until the child instance is disposed, but does not
need to wait for plugin exit policy. `Runtime.Close` waits for exit handlers to
complete before running runtime-close hooks. Exit handlers must not re-enter
`Runtime.Close`. A panic in one observer is recovered and recorded, later
observers still run, worker completion channels are always closed, and
`Runtime.Close` returns the recovered observer panic(s) with any dispatcher
close error.

## Lifecycle hooks

Worker-created instances use the normal runtime-aware instantiate and close
hooks. `InstantiateContext.Origin` and `InstanceContext.Origin` distinguish:

- `InstantiateDirect`
- `InstantiateWorker`

A plugin can use this field to avoid treating its own internal child instances as
new top-level application instances.

## Scope and ownership

Worker IDs are nonzero, runtime-scoped, monotonically allocated, and never
reused. Exhaustion returns `ErrWorkerIDExhausted` rather than wrapping.

Each `Workers` handle is extension-scoped. Operations performed through another
extension's handle treat the ID as unknown.

`Runtime.Close` stops all workers and waits for their owner goroutines to dispose
instances. A stop accepted before a worker crosses its lock-protected startup
linearization point skips callback entry and exits as `WorkerKilled`. Until
native interruption is implemented, a callback already started in
non-cooperative native Wasm can still delay runtime shutdown.

## Runtime-wide quotas

Worker queues are bounded individually and the runtime also applies hard
aggregate ceilings. Configure them at runtime construction:

```go
rt := wago.NewRuntime(wago.WithWorkerLimits(wago.WorkerLimits{
    MaxLiveWorkers: 32,
    MaxQueueBytes:  16 << 20,
}))
```

Zero fields select bounded defaults: 64 live workers and 64 MiB of aggregate
**configured** queue bytes. Spawn reserves both values under the worker-runtime
lock before compiling/instantiating resources, so concurrent Spawn calls cannot
oversubscribe the ceilings. Failed spawns release their reservation; worker
finalization releases it exactly once. Exceeding either ceiling returns
`ErrWorkerQuotaExceeded`.

## What an actor plugin can build

Core primitives map cleanly without forcing actor policy into the runtime:

| Actor-plugin concept | Neutral primitive |
|---|---|
| self/PID lookup | `Current` |
| process creation | `Spawn` |
| mailbox send | `Send` |
| receive import | `DispatchNext` + `OnMessage` |
| kill import | `Kill`, with plugin-owned errno mapping |
| links | `Link` for lifetime plus plugin-owned `OnExit` translation |
| monitors | plugin bookkeeping over `OnExit` |
| supervision | plugin restart policy over `OnExit` and `Spawn` |

Names, selective receive, priority, monitoring semantics, exit signals, restart
windows, distributed IDs, and remote routing remain outside core.

## Current cost notes

On linux/amd64, the default queue preallocates 64 message headers (about 1.5 KiB
with the current 24-byte header), reserves no payload storage, and admits at most
1 MiB of queued payload data. The runtime defaults permit at most 64 such live
workers and 64 MiB of aggregate configured queue capacity. At default queue
sizes, the fleet-wide preallocated message headers are about 96 KiB. The
64-worker ceiling also bounds the current 4 MiB-per-worker foreign-stack virtual
address mappings to about 256 MiB per runtime.

On an AMD Ryzen 7 8845HS, five 2-second runs measured:

- 64-byte `Workers.Send`: 50.5–50.8 ns/op, 64 B/op, 1 allocation/op after moving
  the payload copy outside the lock (the allocation is the required owned copy);
- Runtime synchronous host call before Workers activation: 100.1–101.5 ns/op,
  0 B/op, 0 allocations/op, preserving the allocation-free path (the static
  caller can never authorize workers if the service is activated later); and
- Runtime host call with Workers active: 120.9–122.4 ns/op, 24 B/op,
  1 allocation/op for the distinct expiring capability that makes retention
  enforceable.

The `call_indirect` dispatcher code mapping is compiled and mapped once per
runtime, then shared by all workers.

After integration with the reference-value runtime, `unsafe.Sizeof(Instance{})`
is 864 bytes on the standard 64-bit Go build, 88 bytes above main's 776-byte
layout. The delta holds the expiring host-call scope, inherited worker GC/origin
metadata, and worker/reference lifetime state; the footprint tests pin this
number so future changes cannot grow it silently.

Each live worker still pays the normal instance cost, including the runtime's
current 4 MiB mmap-backed foreign-stack address-space mapping. Anonymous pages
are faulted in as used, but large worker fleets should account for this mapping;
reducing per-engine stack footprint is a separate runtime optimization rather
than something hidden by the worker API.
