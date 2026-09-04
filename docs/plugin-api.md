# Plugin API

Wago plugins are trusted Go packages linked into a generated runtime. They can
run ordinary Go code, so Plugin Authorities are not a sandbox. Authorities
instead make every privileged Wago integration explicit, scoped, reviewable,
and deny-by-default. Do not compile a plugin whose Go source you do not trust.

The vNext API is intentionally breaking. It has no `Extension`, global
registration, compatibility alias, or coarse capability group.

## One definition, one explicit provider

Every plugin publishes an immutable `PluginDefinition` and an explicit factory:

```go
package metrics

import (
    "encoding/json"

    "github.com/wago-org/wago"
)

var Definition = wago.PluginDefinition{
    ID:          "github.com/acme/wago-metrics",
    Name:        "Metrics",
    Version:     "0.1.0",
    Description: "Exports runtime metrics.",
    Stability:   wago.Experimental,
    Provenance: wago.PluginProvenance{
        Repository: "https://github.com/acme/wago-metrics",
        License:    "Apache-2.0",
    },
    Authorities: []wago.AuthorityRequest{
        {
            Name:   wago.AuthorityHostImportDefine,
            Mode:   wago.AuthorityRequired,
            Reason: "expose the acme_metrics guest API",
            Scope:  wago.AuthorityScope{Modules: []string{"acme_metrics"}},
        },
    },
}

func Provider() wago.PluginProvider {
    return wago.PluginProvider{
        Definition: Definition,
        New: func() wago.Plugin { return new(plugin) },
        ValidateConfig: func(raw json.RawMessage) error {
            // Validate semantic constraints that JSON Schema cannot express.
            return nil
        },
    }
}
```

A module's conventional `register` package exports all its providers as values:

```go
func Providers() []wago.PluginProvider {
    return []wago.PluginProvider{metrics.Provider()}
}
```

It must not register providers from `init`. Generated runtimes import each
`/register` package explicitly and combine those catalogs. This removes hidden
process-global selection and makes an empty runtime stay empty. Linked Go
packages can still contain ordinary Go `init` functions; Wago does not claim to
sandbox or defer those.

## Release snapshot and publication

`wago plugin catalog` executes the current local `/register` catalog and writes
canonical `wago.providers.json` at the module root. The snapshot uses
[`https://wago.sh/v1/providers.schema.json`](../providers.schema.json) and holds
each immutable definition, its canonical digest, and the `/register` import
path. Commit it before creating the release tag. `wago plugin catalog --check`
verifies that the committed snapshot still matches the current local catalog.

`wago plugin publish` reruns only that current local catalog to check drift. It
then downloads the exact tagged Go module by version and `h1:` checksum, and
requires the tagged artifact's `wago.json` `package` metadata and
`wago.providers.json` to match the local release inputs before submission. The
registry independently downloads the same exact module, verifies its `h1:`
checksum, and reads both files from the artifact root without building or
executing plugin code.

The verified `PluginDefinition` is included in the release fingerprint. The
installer reviews that metadata before download or build. The generated runtime
then hashes the linked definition and refuses to activate it if it differs from
the reviewed `definitionDigest` in `wago-lock.json`.

## Registration is a transaction

A plugin has one method:

```go
type Plugin interface {
    Register(*Registrar) error
}
```

`Register` decodes configuration and declares contributions into a scratch
plan. It must not start goroutines, open sockets, mutate package globals, or
perform other externally visible work. Wago validates the complete dependency,
authority, contribution, and Contract graph before committing anything.

Activation and teardown are declared explicitly:

```go
func (p *plugin) Register(reg *wago.Registrar) error {
    var cfg Config
    if err := reg.Config(&cfg); err != nil {
        return err
    }

    imports, err := reg.HostImports()
    if err != nil {
        return err
    }
    module, err := imports.Module("acme_metrics")
    if err != nil {
        return err
    }
    module.Func("increment", p.increment).Params(wago.I32)

    return reg.Lifecycle(wago.PluginLifecycle{
        Start: p.start,
        Stop:  p.stop,
    })
}
```

`Start` runs only after the complete plan commits. If any start fails, already
started plugins stop in reverse graph order. Normal shutdown also stops
consumers before providers. `Start` receives no `*Runtime`; privileged work is
available only through the exact handles acquired during registration. Those
handles remain inactive before commit and are revoked during teardown. During
`Start`, committed Contract references and granted core handles are active, so a
plugin can call an already-started dependency or compile and instantiate through
its reviewed limits. Public Runtime operations remain excluded until the whole
Plugin Set has started, so unrelated callers cannot observe a partially started
graph.

Plugin loading and runtime shutdown also form one ordered transaction. Once
loading begins, shutdown may publish the closing state immediately, but it
defers its teardown snapshot until startup has stopped changing plugin teardown
eligibility. A successful load publishes that boundary after every startup turn;
a failed `Start` publishes it before rollback waits on the same shutdown result.
Each plugin becomes eligible for `Stop` immediately before its startup turn,
whether or not it defines a `Start` callback. Therefore a successful `Start`
racing `Runtime.CloseContext` is stopped exactly once before closure completes,
while a later plugin whose startup turn was never reached is not stopped.
Neither callback runs while the runtime mutex is held.

Configuration is opaque to Wago but not permissive: the registrar rejects
unknown struct fields and trailing JSON, the provider validates its published
JSON Schema, and `ValidateConfig` can enforce additional semantic rules.

## Wasm GC host imports

Declarative host imports registered through `HostImports` may use the public
`ValAnyRef` and `ValI31Ref` ABI categories. Wago validates the plugin's declared
ABI against each importing module, but it does not turn one plugin function into
one `HostFuncRef`.

A `HostFuncRef` remains one concrete Wasm function identity with one exact
structural signature and, for `NewGCHostFuncRef`, one bound Runtime collector
domain. A generic plugin `HostFunc` has different ownership:

- the exact parameter and result descriptors come from the calling compiled
  module;
- module-local defined-type indexes remain local to that module;
- the collector and GC domain come from the calling instance; and
- two simultaneously live modules may call the same plugin import with
  structurally different caller-defined GC types and different Runtime GC
  domains.

Collector objects never arrive as raw collector handles or object pointers.
Non-null object parameters are translated to temporary opaque `uint64` tokens.
Inside the active callback, the plugin resolves a token with
`GuestStorage.GCRef` and receives a callback-scoped `GuestGCRef`. Null and i31
values preserve their Wasm semantics. Results may be null, an allowed i31 value,
or a callback-scoped token created through Wago's active host APIs, including
`GuestGCArrayAllocatorHostModule.NewGCArrayResult`. Raw compact collector
references are rejected.

GC handles, result tokens, and borrowed slices expire at their documented
callback or `WithGuestStorage` boundary. Wago checks the Runtime, instance,
collector domain, exact caller type, and active view. Plugins must not retain or
forge them. Scalar-only plugin imports remain ordinary `HostFunc` bindings and
do not acquire GC root maps, a collector, or GC token bookkeeping.

See [Host guest-storage access](host-guest-storage.md) for exact type inspection,
array allocation, and zero-copy numeric array access. Wago intentionally exposes
no raw GC pointer API.

## Callback invocation context

A plugin granted `host.caller.identify` may obtain the active guest invocation's
cancellation and deadline inside one of its synchronous host callbacks:

```go
callers, err := reg.HostCallers()
if err != nil {
    return err
}

module.Func("fetch", func(caller wago.HostModule, _, _ []uint64) {
    ctx, err := callers.InvocationContext(caller)
    if err != nil {
        panic(wago.HostTrap{Err: err})
    }
    // Use ctx only for work owned by this callback.
})
```

The returned context is callback-scoped. Its `Done` channel closes when the
parent invocation is canceled, its deadline expires, or the host callback
returns. Retaining it is safe only for observing that terminal cancellation;
it must not be used to extend callback-owned work. Repeated resolution during
the same callback returns the same context. Nested re-entry receives a distinct
context and does not shorten the outer callback's lifetime.

Invocation contexts deliberately expose no parent context values. Plugins must
pass explicit dependencies instead of using context values as ambient data.
Calls without a cancellable public parent, including raw `Invoke`, prepared
calls, and start-time callbacks, receive a live callback-scoped context without
a deadline. Forged, expired, cross-Runtime, and low-level callers are rejected
with `ErrPermissionDenied`.

## Active caller re-entry

A plugin granted `host.caller.invoke` may synchronously invoke an export on the
exact guest making its active host call. This is intended for callback ABIs such
as Emscripten trampolines; it does not grant instance discovery, retention,
close, or invocation outside that callback:

```go
invoker, err := reg.HostCallerInvoker()
if err != nil {
    return err
}

module.Func("callback", func(caller wago.HostModule, params, results []uint64) {
    nested, err := invoker.Invoke(context.Background(), caller, "host_callback", params...)
    if err != nil {
        panic(wago.HostTrap{Err: err})
    }
    copy(results, nested)
})
```

The caller token expires when the host function returns. Re-entry uses Wago's
isolated native stack and call buffers and remains subject to the outer
invocation's cancellation and normal recursion limits.

## Exact authorities and scopes

Authority names are exact. Dots are for display grouping and do not grant a
parent, wildcard, descendant, or future authority.

| Authority | Allows |
|---|---|
| `host.import.define` | Define host functions in specifically granted import modules. |
| `host.caller.identify` | Resolve the exact active instance and its callback-scoped invocation cancellation/deadline during a synchronous host call. |
| `host.caller.invoke` | Synchronously invoke an export on the exact guest making the active host call. |
| `host.arguments.read` | Read guest arguments exposed by the host. |
| `runtime.close.observe` | Observe logical runtime close. |
| `module.source.transform` | Replace module bytes before compilation. |
| `module.compile.observe` | Correlate source processing with compile success or failure through opaque identities and the final transformed-source digest. |
| `module.close.observe` | Observe logical close of a runtime-bound module. |
| `instance.instantiate.intercept` | Reject a request, or attach fallible identity-keyed state after initialization and before the start function. |
| `instance.instantiate.observe` | Observe successful or failed instantiation. |
| `instance.close.observe` | Observe exact-instance logical close. |
| `instance.invoke.intercept` | Inspect or reject runtime-managed calls. |
| `instance.invoke.observe` | Observe call results and traps. |
| `instance.manage` | Create and own bounded managed instances. |
| `core.module.compile` | Compile core modules for an execution model. |
| `core.instance.instantiate` | Instantiate core modules within reviewed limits. |
| `core.funcref.create` | Create typed host function references. |
| `compiler.type.define` | Define custom compiler value types. |
| `compiler.instruction.define` | Define and lower custom Wasm instructions. |

Every request says whether it is `required` or `optional`, gives a human reason,
and carries any enforceable scope. A required Authority must be present but its
scope may be narrowed; an optional Authority may also be denied. Registration
fails clearly if the plugin cannot operate within the reviewed scope.
`host.import.define` and
`compiler.instruction.define` grant exact module names;
`compiler.type.define` grants exact type namespaces. Instance-owning authorities
carry positive instance limits and an aggregate declared-memory ceiling across
all live instances owned through the handle. A plugin cannot use an authority
it did not declare, and a lockfile cannot grant an authority the plugin did not
request.

For automation, `wago plugin grant`, `wago add`, and `wago plugin update` accept
one strict `--scopes` JSON object keyed by full Plugin ID and exact Authority.
Named scopes must be non-empty subsets of the request; instance-owning scopes
must specify both positive limits no greater than the request. Optional
Authorities are still selected explicitly with `--allow`, `--allow-all`, or
`--deny-all`. The candidate lock graph and runtime are validated through the
normal staged transaction before publication.

Source transformers, compile success, and compile failure share one comparable
`CompilationIdentity`; the resulting `ModuleView.Identity()` is also opaque and
comparable. A successful `Runtime.Compile` event carries an opaque
`ModuleSourceDigest` of the final bytes after every transformer. A transformer
can compare it with `DigestModuleSource` without compile observers receiving the
source itself. The precompiled `Runtime.Module` path reports a zero digest
because its original source is unavailable. Artifact integrations use
`Runtime.PrepareCompile` to retain one admitted transform/observer/import/custom-
instruction generation across lookup: transforms run exactly once, and warm
adoption emits compile success under the same immutable generation. A prepared
compilation must terminate through `Compile`, `Adopt`, or `Close` before runtime
shutdown can tear down its plugin generation. `PreparedCompile.Source()` is a
read-only view of runtime-owned transformed bytes. Transformer return slices are
snapshotted after each callback; after successful `Compile`, the returned
`Module` retains that source storage until it closes.

`Module.Close` emits one close event with the final module identity, clears the
wrapper identity, and rejects later runtime instantiation through that wrapper.
Modules returned by `Runtime.Compile` or `Runtime.AdoptModule` own their
`Compiled` artifact; `Runtime.Module` remains the explicit borrowing path.
Existing instances retain their executable mapping until they close. A module-
close observer may call `Module.Close` or `Runtime.Close` reentrantly; those calls
publish closure without waiting on the active observer, and runtime teardown
drains the admitted module-close generation before stopping providers. Plugins
can therefore move metadata into module- and instance-identity maps and release
it deterministically without retaining a `Runtime`, `Module`, `Compiled`, or
`Instance` through an identity.
The instantiate interceptor's `After` phase is fallible and runs after an exact
instance identity exists but before the Wasm start function and success
observers. Failure closes the partial instance through normal close observation
and then emits the instantiation error event.

Guest permissions such as `fs.read` and `net.outbound` are a different domain.
They describe powers a plugin offers to Wasm. Plugin Authorities describe powers
Wago offers to trusted Go code.

## Dependencies and typed Contracts

`PluginDefinition.Requires` contains package dependencies needed for resolution
and linking. IDs are canonical Go module or package paths. Every dependency has
a non-empty constraint using the same semantic-version range language as
`wago.json`. The resolver selects one exact version and `h1:` checksum for every
direct and transitive Plugin ID that satisfies all incoming constraints; the
lockfile records those selections.

Plugins call each other only through typed, major-versioned Contracts:

```go
package workers

var Contract = plugin.NewContract[Service](
    "github.com/wago-org/workers/service",
    1,
)
```

The provider declares the same ID and major in `PluginDefinition.Provides`, then
registers its value:

```go
if err := plugin.Provide(reg, workers.Contract, service); err != nil {
    return err
}
```

The consumer declares it in `PluginDefinition.Consumes` and requests the typed
reference during registration:

```go
workersRef, err := plugin.Require(reg, workers.Contract)
if err != nil {
    return err
}

err = workersRef.With(func(service workers.Service) error {
    return service.Submit(ctx, job)
})
```

`Require`, `Optional`, and `Many` express exactly one required provider, zero or
one provider, and zero or more providers. A required plugin dependency selects
the package; a Contract selects the stable interface among the resolved
providers. The lockfile records the exact binding so replay does not silently
choose a different implementation.

A new optional Contract with available providers is always reviewed; its safe
proposal is no provider, and an interactive install can select one. `Many`
binds every selected provider for the Contract. Updates retain the reviewed
order of providers that remain, append newly selected providers in lexical ID
order, and review every addition or removal.

The exact dependency edges and reviewed Contract binding edges form one
deterministic DAG. Wago rejects missing requirements, incompatible majors,
duplicate single providers, unreviewed bindings, and cycles before calling a
factory. Diamond dependencies resolve once. Equal ready nodes use lexical
Plugin ID order.

Contract values never escape through a raw `Get`. `Ref.With` holds an in-flight
lease for the callback, and the value's supported lifetime ends when that
callback returns. Native Go plugins remain trusted to honor the lifetime. During
close, a consumer can use its dependencies from its own `Stop`; Wago then
revokes that consumer's references, and any retained reference fails closed.
Before stopping a provider, Wago rejects new contract calls and waits for every
in-flight contract call to finish. This makes cross-plugin shutdown deterministic
even when calls race with close.

Guest-callable host imports and host funcrefs created through a plugin's core
handle use the same stop boundary. Runtime shutdown rejects new callback entries
before invoking that plugin's `Stop`, then drains entries already in flight.
`Stop` may therefore signal or cancel a callback that is waiting on plugin-owned
state, but must not free state until those callbacks have returned. A low-level
instance may retain a Runtime instance's native export after logical close, but
calling through that retained export cannot re-enter stopped plugin code; the
callback traps with `ErrPermissionDenied` instead.

## Inspection and loading

Embedder code supplies one explicit catalog and reviewed selection set:

```go
set := wago.PluginSet{
    Providers: append(wasi_register.Providers(), metrics_register.Providers()...),
    Selections: selectionsFromLock,
}

plan, err := wago.InspectPluginPlan(set)
if err != nil {
    return err
}
if err := runtime.LoadPlugins(ctx, set); err != nil {
    return err
}
```

Inspection uses definitions only: it does not call factories, registration, or
lifecycle code. Loading validates the same immutable graph, then creates and
registers selected plugins transactionally.

`wago plugin tree` explains direct and transitive causes. `wago plugin list
--json` reports every linked immutable definition together with its
side-effect-free plan entry, including granted scopes, Contract providers, and
activation order. `wago plugin rebuild --locked` is the CI check: it verifies
the committed graph and provenance, rebuilds the runtime, and validates its
linked definitions and complete dry-run Plugin Plan before publication.

## Exact-instance resource lifecycle

Resource-owning plugins key state by the exact `*wago.Instance`. Attach state in
an instantiate observer, associate synchronous host calls through the separately
granted caller resolver, and retire state from the close observer.

`BeforeClose` is the logical close event. The invocation gate has already
closed, so no new public call can begin. `AfterClose` is the terminal disposal
observer: it runs only after construction and admitted invocations have
quiesced, immediately before physical release can proceed. Concurrent close
calls share one logical close, every hook runs once, and callback panics are
reported as bounded `ErrCallbackPanic` errors without retaining panic values or
skipping remaining cleanup. A close callback may reenter `Instance.Close`; the
reentrant call returns promptly instead of waiting on itself.

A Runtime-owned instance is tracked immediately after construction, before its
post-create hooks or Wasm start function run. An ordinary invocation trap,
cancellation, or `HostExit` does not dispose an instance. A start-function
failure does: instantiation never succeeded, so Wago closes the partial instance
through the normal lifecycle before returning. Direct instances are logically
closed before plugin `Stop`. Managed-instance shutdown is three phase: close
manager admission, run plugin `Stop` while existing workers and dependent
contracts remain usable, then close and drain managed instances before revoking
provider state. A direct caller retains its handle only as an idempotent closed
handle; calling `Close` again is safe.

Managed-instance limits account for physical ownership, not just the managed
handle's logical close. If another instance retains an exported function, the
manager's instance and declared-memory reservations remain charged until that
last importer closes and physical resources are released.

`Runtime.Close` is always initiation-only: it publishes the shutdown gate and
returns promptly, including while plugin loading or a callback is active.
`Runtime.Closed` exposes completion, and `Runtime.WaitClosed(ctx)` returns the
single final joined teardown error. Callbacks must not wait on their own
runtime's completion before returning.

`Runtime.CloseContext` starts the same single teardown and waits selectably. Its
context bounds both plugin-loading waits and final completion, and the initiating
context is passed to plugin `Stop`; teardown still publishes one final result for
every later `WaitClosed` caller. Startup rollback is mandatory even if the load
context is canceled. Once shutdown is published, new compile, module binding,
direct/managed instantiation, invocation, and managed fork operations fail
closed. Already-admitted instantiation and invocation generations retain explicit
plugin-operation reservations, so their terminal observers and host callbacks
can finish after ordinary callback admission closes; provider teardown waits for
those reservations to drain.

Shutdown callbacks, including runtime-close observers, plugin `Stop`, manager
close/drain operations, internal close callbacks, contract/handle revocation, and
reference-store closure, are panic-contained. Errors match `ErrCallbackPanic`,
carry bounded phase attribution, and never include the recovered panic value or
stack.

The package-level low-level `Compile`, `Instantiate`, and `Invoke` APIs are
outside the Runtime plugin lifecycle. Native process crashes and forced
termination cannot reliably execute in-process cleanup; durable plugins need an
external recovery strategy.

## Core-size rule

Privileged APIs expose bounded mechanisms, not product policy. Pools, workers,
actors, routers, metrics aggregation, retries, and caching belong in plugins.
Core mechanisms must be useful to more than one plugin category. An unlinked
plugin must add no runtime goroutines or allocations.

## Direct guest storage from host imports

A synchronous host function that needs more than the `HostModule.Memory()`
memory-0 convenience can opt into callback-scoped guest storage.

`GuestStorageHostModule.WithGuestStorage` provides checked access to arbitrary
linear-memory indexes, Memory32/Memory64 metadata, Wasm GC arrays, nested GC
array references, and the importing module's exact structural parameter/result
types. `GuestGCArrayAllocatorHostModule.NewGCArrayResult` allocates the exact
caller-selected numeric or `v128` array result type and initializes it before
publication.

Every borrowed slice and callback-scoped GC reference expires when the storage
callback returns. Wago rejects Wasm re-entry while a direct guest-storage borrow
is active so memory growth or moving collection cannot invalidate a live host
view.

See [Host guest-storage access](host-guest-storage.md) for the complete API,
lifetime rules, and examples. [Facet](https://github.com/jtenner/facet-spec) is
one motivating consumer, but these interfaces are general Wago host APIs.
