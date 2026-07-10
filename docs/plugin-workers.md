# Plugin worker primitives

Wago exposes a small worker service to extension authors through
`Registry.Workers()`. It deliberately does **not** provide actors, PIDs, guest
mailboxes, signals, monitoring, naming, restart policies, or supervision. A
plugin may build those policies over the neutral primitives described here.

## Registration

A worker handle is created during extension registration and becomes active only
if the entire `Runtime.Use` operation commits successfully.

```go
type Extension struct {
    workers *wago.Workers
}

func (e *Extension) Register(reg *wago.Registry) error {
    e.workers = reg.Workers()

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

`OnMessage` and `OnExit` registrations are snapshotted when a worker is spawned,
so plugins should normally register them inside `Register`.

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
2. creates a fresh instance of that caller's compiled module with the same
   effective imports and GC configuration;
3. resolves `tableIndex` against the new child's initialized table;
4. rejects an out-of-bounds, null, or non-`() -> ()` entry; and
5. starts the selected callback on a dedicated goroutine through a shared native
   dispatcher containing a real Wasm `call_indirect`.

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

Queues are bounded. Zero-valued options select these defaults:

- queue capacity: 64 messages;
- maximum payload: 64 KiB;
- aggregate queued payload bytes: 1 MiB.

Explicit options are also bounded:

- maximum queue capacity: 65,536 messages;
- maximum payload: 16 MiB;
- aggregate queued payload bytes: 64 MiB.

The queue preallocates only message headers. Payload storage is allocated when a
message is accepted, counted against `MaxQueueBytes`, and released after delivery
or worker termination. `MaxQueueBytes` must be at least `MaxPayloadBytes`, so any
otherwise-valid payload can fit in an empty queue.

## Current worker identity

A plugin host import can resolve its current neutral worker ID without adding a
core PID or process API:

```go
id, err := workers.Current(caller)
```

A direct instance that is not a managed worker returns `ErrWorkerNotFound`. An
actor plugin can expose this ID as a PID, wrap it in a richer identifier, or keep
it entirely internal.

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
to guest code. `MessageContext.Caller` exposes the restricted `HostModule`
surface rather than `*Instance`, preventing accidental re-entrant guest calls.
It can also be passed to `Spawn` or `Link` while the handler is active.

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

Unlinked children may outlive their creator instance. No worker may outlive its
runtime.

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
`Runtime.Close`.

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
instances. Until native interruption is implemented, a non-cooperative callback
can delay runtime shutdown.

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
1 MiB of queued payload data. A 64-byte `Workers.Send` was
measured at approximately 50–52 ns/op, 64 B/op, and 1 allocation/op on an AMD
Ryzen 7 8845HS; the allocation is the required payload copy. The `call_indirect`
dispatcher code mapping is compiled and mapped once per runtime, then shared by
all workers.

Each live worker still pays the normal instance cost, including the runtime's
current 4 MiB mmap-backed foreign-stack address-space mapping. Anonymous pages
are faulted in as used, but large worker fleets should account for this mapping;
reducing per-engine stack footprint is a separate runtime optimization rather
than something hidden by the worker API.
