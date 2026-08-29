# Native memory limits

This document describes the native memory mapping limits in Wago.
It also gives configuration and monitoring guidance.

The WebAssembly specification permits limits on module structure and allocated
memory instances. See the [WebAssembly implementation limits][core-limits].
The JavaScript API uses an exact limit of 100 memories per module.
This count includes imported memories. See the [JavaScript API limits][js-limits].

[core-limits]: https://webassembly.github.io/spec/core/appendix/implementation.html
[js-limits]: https://webassembly.github.io/spec/js-api/#implementation-defined-limits

## Meaning of a native memory mapping

The runtime type for this mapping is `JobMemory`.
Each mapping contains a control area and a linear memory area.
The control area is immediately before the linear memory base.
JIT code uses fixed negative offsets from the base to read the control area.

A mapping is not a Wasm page count or a byte limit.
A mapping is also not always equal to one module or one instance.

The following rules apply:

- A memoryless instance owns one mapping for its control area.
- An instance with one local memory owns one mapping.
- Each additional local memory owns one additional mapping.
- An imported memory uses the mapping of its provider.
- A threaded instance can own a separate control mapping for imported memory 0.
- `memory.grow` changes the logical size. It does not add a mapping.
- `NewMemory` and `NewSharedMemory` add host-owned mappings.
- A released mapping can stay in a bounded reuse cache.

The process registry includes active mappings and cached mappings.
It does not count Wasm pages, committed physical memory, or reserved virtual bytes.

## Limit layers

Use all applicable limit layers for untrusted modules.

| Layer | Default | Configuration | Purpose |
|---|---:|---|---|
| Memories in one module | 100 | `WithMaxMemoriesPerModule` | Reject a large declaration during validation. |
| Mappings owned by live instances in one `Runtime` | No lower limit | `WithNativeMemoryMappingLimit` | Isolate one runtime or tenant from the process limit. |
| Linux host interrupt registry | 4,096 | Fixed | Bound signal-handler work and static storage. |
| Guard-page reservation registry | 256 | Fixed | Bound guarded virtual address reservations. |

The configurable module maximum is 4,096.
The default value of 100 gives JavaScript API compatibility.
Use a lower value for a controlled module set.
A value from 4 through 8 covers the current tracked Wago corpus.
The limit also applies when you bind a precompiled module to a `Runtime`.

The 4,096 process limit is an emergency safety limit.
It is not a tenant quota.
Do not increase it through `RuntimeConfig`.
Use more processes when one process needs more mappings.

The process registry contains 4,096 pointer entries.
It uses 32 KiB on a 64-bit system.
The JIT hot path does not scan this registry.
Only the cold Linux signal-handler path scans it.

## Recommended configuration

Start with a runtime mapping limit of 256 for untrusted modules.
Measure the application before you increase this value.
Also set an instance count and a declared memory byte limit.

```go
cfg := wago.NewRuntimeConfig().
    WithMaxMemoriesPerModule(8).
    WithInstanceLimits(256, 8<<30).
    WithNativeMemoryMappingLimit(256)

rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
```

Use `Policy.MaxMemories` when one instance needs a lower limit.

```go
policy := wago.Policy{
    MaxMemories:   4,
    MaxMemoryBytes: 512 << 20,
}
```

A declared memory byte limit is not a mapping limit.
A module can declare many zero-page memories.
Such a module uses mapping slots but can declare zero initial memory bytes.

## Usage telemetry

Use `ProcessNativeMemoryStats` for the Linux process registry.

```go
process := wago.ProcessNativeMemoryStats()
fmt.Printf("active=%d cached=%d registered=%d peak=%d capacity=%d scan=%d\n",
    process.Active,
    process.Cached,
    process.Registered,
    process.PeakRegistered,
    process.Capacity,
    process.ScanSpan,
)
```

`Supported` is false when the build does not use the Linux host interrupt registry.
`Registered` is the sum of active and cached mappings.
`PeakRegistered` is the process high-water value.
`ScanSpan` is the current maximum scan length for a signal handler.
Registry holes can make `ScanSpan` larger than `Registered`.

Use `Runtime.ResourceStats` for a configured runtime limit.

```go
usage := rt.ResourceStats()
fmt.Printf("runtime mappings=%d peak=%d limit=%d\n",
    usage.NativeMemoryMappings,
    usage.PeakNativeMemoryMappings,
    usage.MaxNativeMemoryMappings,
)
```

The runtime mapping counters are active only when the configured limit is nonzero.
They count mappings that live instances in that `Runtime` own.
They include direct instances and plugin-managed instances.
They do not charge an imported mapping again.
Process telemetry includes the imported provider mapping.

Inspect `Module.Metadata().Memories` before instantiation.
The slice length gives the imported and local memory count.
Use this value to select a lower module policy.

Alert before the available capacity is smaller than the largest expected request.
For example, keep at least eight free slots when an instance can own eight mappings.
Also monitor `PeakRegistered` during representative load tests.

## Limit errors

Process and runtime mapping failures return `*ResourceLimitError`.
The error matches `ErrResourceLimit`.
A configured runtime rejection also matches `ErrPermissionDenied`.

```go
var limitErr *wago.ResourceLimitError
if errors.As(err, &limitErr) {
    log.Printf("scope=%s resource=%s used=%d requested=%d limit=%d",
        limitErr.Scope,
        limitErr.Resource,
        limitErr.Used,
        limitErr.Requested,
        limitErr.Limit,
    )
}
```

Close unused `Instance` and `Memory` values after a limit error.
The runtime can retain one classic mapping and one guarded mapping for reuse.
These cached mappings stay registered.
Reuse an applicable cached mapping when possible.
Use another process when the workload needs more than 4,096 mappings.

## Empirical basis

The measurement used all tracked `.wasm` files on 2026-08-29.
It decoded each file with `wasm.DecodeModule`.
It excluded six malformed regression files from the distribution.

| Memories in a module | Valid modules |
|---:|---:|
| 0 | 1,128 |
| 1 | 189 |
| 2 | 13 |
| 3 | 1 |
| 4 | 2 |

The valid set contains 1,333 modules.
Of these modules, 98.8 percent declare zero or one memory.
No valid tracked module declares more than four memories.

Use `max(1, memory count)` as a conservative mapping estimate for this corpus.
The mean estimate is 1.016 mappings per independent instance.
At this mix, 4,096 slots support approximately 4,032 live instances.
The observed four-memory maximum supports at least 1,024 such instances.

Imports can reduce the owned mapping count because they reuse provider mappings.
Threaded imports can add a separate control mapping.
Use telemetry for the final production value.

## Security guidance

Reject modules that exceed the configured module memory count.
Do this before compilation and instantiation consume more resources.

Set a runtime mapping limit for each untrusted runtime.
Do not rely only on a declared memory byte limit.

Keep the fixed 4,096 process limit.
This limit bounds the signal-handler scan and the registry storage.
The limit also prevents an unbounded mapping registry.

Separate tenants into processes when they do not share one trust boundary.
A process boundary also contains host faults and process-wide signal ownership.

Guard-page mode has a lower limit of 256 live reservations.
Each guarded memory reserves approximately 8 GiB of virtual address space.
It also consumes one Linux host interrupt registry slot.
The guard-page limit normally rejects the request first.
