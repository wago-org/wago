# Plugin system vNext plan

Wago's pre-release plugin system is being replaced, not version-adapted. The
new system treats installation, authority review, linking, planning, activation,
inspection, and teardown as one coherent contract. There are no compatibility
aliases for the old Extension API, global registration, coarse capabilities, or
rolling-v0 manifest.

## Design constraints

- Plugins are trusted Go code linked into a custom Wago binary. Authorities gate
  privileged Wago interfaces; they do not sandbox arbitrary Go behavior.
- A consumer reviews immutable published metadata before plugin code is
  downloaded, built, or executed. The linked definition must match the reviewed
  digest.
- Registration is declarative. The complete graph validates before any
  contribution is committed, any handle becomes active, or any plugin starts.
- Startup rolls back and shutdown runs in reverse dependency order. Every
  resource-owning interface is revocable and bounded.
- Unknown authorities, authority fields, definition fields, lock fields, and
  configuration fields fail closed.
- An unlinked plugin adds no goroutines or runtime allocation. Planning uses
  bounded data proportional to the selected plugin set.

## Interfaces considered

The minimal design returned one large immutable `Plan` from a single plugin
method. It had excellent depth, but one growing plan struct made unrelated
features share a shallow schema and made configuration-dependent typed handles
awkward.

The maximally flexible design modeled every contribution and authority as an
open interface. It supported third-party contribution kinds, but that would let
plugins invent apparently privileged authorities Wago could not enforce and
would add reflection or allocation to the planning path.

The common-caller design paired an immutable definition with a one-method
plugin and a typed registrar. It made ordinary host-import plugins short while
keeping advanced handles explicit. This is the selected seam, with the minimal
design's explicit provider catalog and the flexible design's scoped grants and
versioned contracts.

## Selected public shape

Illustrative declarations (the implementation may split these across files):

```go
type Plugin interface {
    Register(*Registrar) error
}

type PluginProvider struct {
    Definition     PluginDefinition
    New            func() Plugin
    ValidateConfig func(json.RawMessage) error
}

type PluginSet struct {
    Providers  []PluginProvider
    Selections []PluginSelection
}

func (rt *Runtime) LoadPlugins(context.Context, PluginSet) error
func InspectPluginPlan(PluginSet) (*PluginPlan, error)
```

`PluginDefinition` contains canonical module-path identity, compatibility,
provenance, required plugins, requested Authorities, configuration schema, and
provided or required Contracts. Its canonical JSON has a SHA-256 digest.
`PluginSelection` contains that digest, exact Authority Grants, and opaque
configuration. A mismatch rejects the plan before `New` or `Register` runs.

There are no optional lifecycle interfaces. A plugin declares start and stop
callbacks through `Registrar.Lifecycle`; start receives no `*Runtime` escape
hatch. Privileged operations are available only from authority-specific,
revocable handles acquired during registration.

Each module's conventional `register` package exports
`Providers() []wago.PluginProvider`. It has no registration `init`. Generated
binaries import each package explicitly and pass the combined provider catalog
to the runtime. Inspection reads immutable definitions and never registers or
starts plugins.

## Exact authorities

Authority names are exact and non-inheriting. Dots group related operations for
display only; granting a parent or wildcard is invalid.

| Authority | Privileged interface |
|---|---|
| `host.import.define` | Define guest host functions in the named import modules. |
| `host.caller.identify` | Resolve the exact active instance during a synchronous host call. |
| `host.arguments.read` | Read the guest argument vector exposed by the host. |
| `runtime.close.observe` | Observe logical runtime shutdown. |
| `module.source.transform` | Replace module bytes before compilation. |
| `module.compile.observe` | Correlate source processing with compile success or failure through opaque identities and the final transformed-source digest. |
| `module.close.observe` | Observe logical close of a runtime-bound module. |
| `instance.instantiate.intercept` | Reject a request, or attach fallible identity-keyed state after initialization and before the start function. |
| `instance.instantiate.observe` | Observe successful or failed instantiation. |
| `instance.close.observe` | Observe exact-instance logical close. |
| `instance.invoke.intercept` | Inspect or reject a runtime-managed invocation. |
| `instance.invoke.observe` | Observe invocation results and traps. |
| `instance.manage` | Create and own bounded managed instances. |
| `core.module.compile` | Compile core modules for an execution-model plugin. |
| `core.instance.instantiate` | Instantiate and own core modules for an execution-model plugin. |
| `core.funcref.create` | Create typed host function references for an execution model. |
| `compiler.type.define` | Define custom compiler value types. |
| `compiler.instruction.define` | Define and lower custom Wasm instructions. |

`Runtime.Compile` reports an opaque `ModuleSourceDigest` for the exact bytes
after every source transformer; transformers can compare it with
`DigestModuleSource` without exposing source through the observer API. The
digest is zero for `Runtime.Module`, where source is unavailable. A
runtime-bound `Module` emits one close event with its final opaque identity and
then rejects new runtime instantiation through that wrapper. Closing the wrapper
does not close or transfer ownership of its caller-visible `Compiled` artifact.

Every request is `required` or `optional`, includes a human reason, and may
carry an Authority Scope. `host.import.define` and
`compiler.instruction.define` scope exact module names;
`compiler.type.define` scopes exact type namespaces; and `instance.manage` plus
`core.instance.instantiate` carry positive `maxInstances` and
aggregate `maxMemoryBytes` across all live instances owned through that handle.
Zero is never an implicit unlimited grant. A grant may equal
or narrow a request but never widen it. `required` records that the plugin needs
the Authority for its intended behavior, but users may still decline its grant
or narrow its scope; an optional Authority may be omitted.
Registration fails clearly if a plugin cannot operate within a narrowed grant.
New authorities are denied by old hosts and old lockfiles.

Guest capabilities such as `fs.read` and `net.outbound` remain a separate
domain. They describe powers a plugin offers to Wasm; Plugin Authorities
describe powers Wago offers to trusted Go code.

## Contracts

Plugin composition uses typed, major-versioned Contracts:

```go
var Workers = plugin.NewContract[Service](
    "github.com/wago-org/workers/service", 1,
)

plugin.Provide(reg, Workers, service)
ref, err := plugin.Require(reg, Workers)
```

Every explicit dependency has a non-empty semantic-version constraint. The
resolver selects an exact version and `h1:` checksum for every direct and
transitive Plugin ID satisfying all incoming constraints. Those dependency
edges and the reviewed typed Contract binding edges form one DAG.

A required consumption binds exactly one selected provider, an optional
consumption binds zero or one, and a multi-provider consumption binds every
selected matching provider in reviewed order. New optional bindings with an
available provider require review and default to no provider. Multi-provider
updates preserve the order of survivors, append new providers deterministically,
and re-review additions or removals. Duplicate single providers, missing
required providers, incompatible majors, unreviewed bindings, and cycles reject
the complete plan before a factory runs.

References remain unreadable before commit. Providers start before consumers;
on shutdown, a consumer's references remain usable through its own `Stop`, then
Wago revokes them and retained references fail closed. Before provider `Stop`,
Wago rejects new Contract calls and drains calls already in flight. Guest-callable
callbacks have a separate admission gate: Wago closes admission before that
plugin's `Stop`, lets `Stop` cancel admitted work, and then drains it.

## Manifest and lock contract

`wago.json` records direct Plugin Requirements using canonical Go module or
package IDs:

```json
{
  "$schema": "https://wago.sh/v1/schema.json",
  "plugins": {
    "github.com/wago-org/wasi": "^0.1.0"
  }
}
```

Publish metadata moves beneath `package`; a package may expose one or more
providers. `wago plugin catalog` executes the current local `/register` catalog
and writes canonical module-root `wago.providers.json` using
[`https://wago.sh/v1/providers.schema.json`](../providers.schema.json). The
publisher commits that snapshot before tagging. CI runs
`wago plugin catalog --check`.

`wago plugin publish` executes only the current local catalog to check drift. It
downloads the exact tagged Go module by selected version and `h1:` checksum,
then requires the tagged `wago.json` `package` metadata and
`wago.providers.json` to match the local release inputs before submission. The
registry independently downloads the exact `h1:` artifact and reads both root
files without building or executing plugin code.

`wago-lock.json` records the complete direct and transitive graph: source module,
exact version and checksum, definition digest, requested Authorities, granted
scopes and limits, exact Contract bindings, and plugin configuration. It is
strict and authority-bearing. The manifest never contains grants or
configuration.

## Installation transaction

`wago add` performs one transaction:

1. Resolve package metadata. An interactive package-root install chooses either
   the root bundle or an explicit subset of its published subpackages;
   non-interactive root installs keep the root and therefore select everything.
2. Resolve the requested plugins and transitive definitions from the registry.
3. Compute the proposed manifest, lock graph, authority review, and build inputs
   entirely in memory.
4. Review all new or widened required and optional Authorities in one prompt.
   Rejecting a required Authority cancels installation.
   Non-interactive callers may author exact narrower scopes with one strict
   `--scopes` JSON object keyed by full Plugin ID and exact Authority; optional
   selection remains explicit through `--allow`, `--allow-all`, or `--deny-all`.
5. Download and verify modules into a staging build directory.
6. Build an explicit provider catalog, verify every definition digest, validate
   config, and dry-run the complete Plugin Plan.
7. Atomically publish the manifest, lockfile, and built runtime. Any error keeps
   the previous project and runtime usable.

Updates use the same transaction and re-review only new or widened authority.
Removing a direct requirement prunes unreachable transitive plugins. Locked CI
never mutates resolution or grants.

## Dependency seams

Runtime planning and authority enforcement are in-process modules tested through
the Plugin Plan interface. Filesystem and Go-build state use local test adapters.
The owned registry is accessed through a catalog port with HTTP and in-memory
adapters. GitHub and the Go module proxy remain true external dependencies and
are exercised through narrow injected ports in manager tests.
