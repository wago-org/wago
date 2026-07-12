# Capability-based plugin API

Wago plugins are open-source Go modules compiled into the host binary. A plugin
can run ordinary Go code; Wago does not pretend to sandbox that code. Instead,
the plugin API controls which privileged Wago integration surfaces the plugin
may use. Consumers review the source dependency and grant only the integration
powers they accept in `wago.json`.

## Manifest model

`dependencies` controls what source modules are compiled into the custom Wago
binary. `plugins` controls what is activated at runtime:

```json
{
  "dependencies": [
    "github.com/wago-org/workers",
    "github.com/acme/wago-metrics"
  ],
  "plugins": [
    {
      "name": "github.com/acme/wago-metrics",
      "capabilities": ["host.imports", "instance.invoke"],
      "before": ["github.com/wago-org/workers"],
      "config": {"sampleRate": 0.1}
    },
    {
      "name": "github.com/wago-org/workers",
      "capabilities": ["instance.manage"]
    }
  ]
}
```

Compiling a dependency does not activate it. Activating a plugin does not grant
it a capability. A plugin that exercises an ungranted API fails registration,
and the complete plan commits nothing.

`wago pkg add` adds both the source dependency and a disabled-by-authority
plugin entry with an empty capability list. The consumer must review the plugin
and fill in its grants.

The complete manifest field reference and JSON Schema are documented in
[`wago-json.md`](wago-json.md).

## Plugin capabilities

These capabilities authorize Wago integration, not arbitrary Go behavior:

| Capability | Authority |
|---|---|
| `host.imports` | Add host functions to Wasm import namespaces and resolve the exact active caller identity. |
| `host.environment` | Read the host environment explicitly exposed to plugins. |
| `runtime.lifecycle` | Observe runtime shutdown and release plugin resources. |
| `module.compile` | Observe or transform runtime module compilation. |
| `instance.lifecycle` | Observe or affect instantiation and instance close. |
| `instance.invoke` | Intercept calls and observe their results or traps. |
| `instance.manage` | Create and own restricted managed instance tasks. |

Plugin packages declare the capabilities they require in `ExtensionInfo`. The
runtime checks both that declaration and what the plugin actually registered.
This prevents a stale declaration from hiding newly-added authority.

Guest permissions such as `fs.read`, `net.outbound`, or `wasi` are separate.
Plugins provide those permissions for Wasm modules; host policy decides whether
a module may exercise them.

## Load order

Each plugin may declare mandatory `requires` dependencies plus optional
`before`/`after` constraints. A project's plugin entry may add `before` and
`after` edges for local integration needs.

The runtime builds one directed graph:

- every required plugin must be selected;
- requirements load before dependants;
- missing optional ordering targets are ignored;
- cycles reject the complete plan;
- unrelated ready plugins use lexical registry-name order.

Registration is planned in resolved order and committed transactionally.
Lifecycle teardown runs in reverse order so dependants stop before their
dependencies.

Plugins may also compose through typed services. A provider calls
`plugin.Provide`; a consumer calls `plugin.Require`. The service dependency is
added to the same load graph automatically, duplicate providers and missing
services are rejected, and the typed reference becomes readable only after the
complete graph resolves. `github.com/wago-org/workers` exposes
`workers.ServiceKey`, allowing pools,
actors, and schedulers to build on it without coupling to a concrete plugin
instance.

## Open-source provenance

Manifest-loaded plugins must declare an absolute HTTPS source repository and an
SPDX license identifier. `Private` plugins are rejected. The Go module fetched
under `dependencies` is the build input, so consumers can pin, mirror, audit,
and reproduce the exact source compiled into their host.

Wago does not claim that provenance metadata is a security sandbox. A host that
does not trust a plugin's Go source must not compile that plugin.

## Registration

```go
type Extension interface {
    Info() ExtensionInfo
    Register(*Registry) error
}
```

`Register` declares contributions into a scratch registry. It should not start
goroutines, open sockets, or mutate global state. Runtime activation occurs only
after the whole plan validates. Plugin-owned configuration is opaque JSON and
is decoded with `Registry.Config`.

Programmatic `Runtime.Use` remains a trusted embedder escape hatch.
`Runtime.LoadPlugins` is the strict manifest path and requires explicit grants.

Plugins can optionally implement `PluginStarter` and `PluginStopper`. Start is
called only after transactional commit; Stop runs in reverse order on shutdown
or startup rollback. `ConfigSchemaProvider` exposes plugin-owned JSON Schema to
inspection tools, while validation remains the plugin's responsibility during
Register. `wago plugin plan` shows the resolved graph and `wago plugin check`
validates it without starting plugins.

Each privileged surface also has a capability-specific handle (`HostImports`,
`ModuleCompiler`, `InstanceInvocation`, and so on), so APIs obtained for one
grant cannot be used to reach another. Resource-owning capabilities may carry
core-enforced budgets in the manifest.

## Exact-instance resource lifecycle

A resource-owning plugin should key its private state by the exact
`*wago.Instance`. Attach state in `AfterInstantiate`, associate host calls with
that pointer through `CallerResolver`, and retire the state in `BeforeClose`.
The map belongs to the plugin object; Wago does not maintain or require a
process-global instance registry.

```go
type plugin struct {
    mu      sync.Mutex
    callers *wago.CallerResolver
    state   map[*wago.Instance]*resource
}

func (p *plugin) Register(reg *wago.Registry) error {
    host, err := reg.HostImports()
    if err != nil {
        return err
    }
    p.callers = host.CallerResolver()
    host.Module("acme_resource").
        Func("open", p.open).
        Params().Results()

    lifecycle, err := reg.InstanceLifecycle()
    if err != nil {
        return err
    }
    lifecycle.AfterInstantiate(p.attach)
    lifecycle.BeforeClose(p.detach)
    return nil
}

func (p *plugin) attach(_ *wago.InstantiateContext, in *wago.Instance) error {
    p.mu.Lock()
    defer p.mu.Unlock()
    if p.state == nil {
        p.state = make(map[*wago.Instance]*resource)
    }
    p.state[in] = newResource()
    return nil
}

func (p *plugin) open(caller wago.HostModule, _, _ []uint64) {
    in, err := p.callers.Resolve(caller)
    if err != nil {
        panic(err)
    }
    p.mu.Lock()
    resource := p.state[in]
    p.mu.Unlock()
    _ = resource
}

func (p *plugin) detach(ctx *wago.InstanceContext) {
    p.mu.Lock()
    resource := p.state[ctx.Instance]
    delete(p.state, ctx.Instance)
    p.mu.Unlock()
    if resource != nil {
        resource.Close()
    }
}
```

`BeforeClose` is the authoritative disposal event. It receives the same instance
identity delivered to successful instantiation and active caller resolution,
plus the owning runtime, compiled module, direct/managed origin, and a metadata
map shared with `AfterClose`. Hooks run in reverse registration order. Concurrent
`Instance.Close` calls wait for one close operation; `BeforeClose`, Wago's own
cleanup, and `AfterClose` each execute once. Each hook panic is recovered
independently, remaining hooks and internal cleanup continue, and `Close` returns
an aggregated error rather than silently losing the panic.

`CallerResolver` is deliberately available from the `host.imports` handle. It
grants identity only and does **not** grant instance creation, invocation, close,
management, pooling, or worker authority, so `instance.manage` is not required.
Resolution succeeds only during the synchronous `HostFunc` callback. Retained,
expired, forged, cross-runtime, low-level, and otherwise incompatible
`HostModule` values fail closed.

The runtime attaches ownership and origin before imported or local Wasm start
functions execute. If a start function calls a host import and then traps, exits,
or otherwise fails, the partially initialized instance runs the normal close
lifecycle before `Instantiate` returns. An `AfterInstantiate` failure is handled
the same way. `OnInstantiateError` still observes the failure after cleanup; a
panic in that observer is reported without replacing the original failure or
preventing disposal. Original instantiation and cleanup errors remain available
through `errors.Is`/`errors.As` because Wago joins them.

A trap, context cancellation, `HostExit`/`ExitError`, or exported-function error
from an ordinary invocation is **not** a disposal event. `AfterInvoke` may observe
it, but a caller-owned instance remains open until its owner closes it. A
`HostExit` raised while executing a start function is different: instantiation
never succeeds, so the partially created instance is closed as failed-start
cleanup.

Runtime-created direct instances remain caller-owned when `Runtime.Close` runs.
Managed instances, including forks still owned by an `InstanceManager`, are
closed by manager/runtime shutdown. Wago's managed API does not logically reset
or republish an instance: creation and fork paths physically instantiate. A
plugin that implements its own leases or pooling and owns non-Wasm sockets,
handles, quotas, registrations, or similar state must close the old physical
instance through the full lifecycle before publishing another owner, or require
physical reinstantiation.

The package-level low-level `Compile`/`Instantiate`/`Invoke` APIs remain outside
the Runtime plugin lifecycle. Their instances do not emit plugin hooks and their
`HostModule` values cannot be resolved by a runtime `CallerResolver`.

Finally, these guarantees cover controlled in-process Wago lifecycle paths. A
native host-process crash, `SIGSEGV`, forced termination, or power loss cannot
reliably execute an in-process close callback; plugins needing crash durability
must use operating-system or external-service recovery mechanisms.

## Core-size rule

Privileged APIs expose mechanisms, not product policy. Pools, workers, actors,
routers, metrics aggregation, caching, and retry behavior belong in plugins.
Core mechanisms must be bounded and useful to more than one plugin category.
An unlinked plugin must add no goroutines or allocations and should be removed
by TinyGo dead-code elimination.
