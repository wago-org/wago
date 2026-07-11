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
| `host.imports` | Add host functions to Wasm import namespaces. |
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

## Core-size rule

Privileged APIs expose mechanisms, not product policy. Pools, workers, actors,
routers, metrics aggregation, caching, and retry behavior belong in plugins.
Core mechanisms must be bounded and useful to more than one plugin category.
An unlinked plugin must add no goroutines or allocations and should be removed
by TinyGo dead-code elimination.
