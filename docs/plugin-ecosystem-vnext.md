# Plugin ecosystem vNext migration contract

This file is the cross-repository acceptance matrix for the breaking vNext
rollout. A repository is migrated only when its manifest, explicit provider
catalog, immutable definition, scoped registrations, typed Contracts, tests, and
documentation agree with this table.

Every provider ID is a canonical Go module or package path. Every source module
exports a root `/register` catalog containing all providers published from that
module and commits the canonical module-root `wago.providers.json` generated from
it. The snapshot uses `https://wago.sh/v1/providers.schema.json`. Leaf
`/register` packages may expose convenient subsets for embedders, but must not
self-register with `init`.

## Official and ecosystem providers

| Source repository | Provider IDs | Required Plugin Authorities | Explicit requirements and Contracts |
|---|---|---|---|
| `github.com/wago-org/wasi` | root aggregate plus `/p1`, `/p2`, and `/unstable` | P1 and unstable request exact host-import, argument, caller-identity, and close-observation access; P2 requests guest arguments | The root selects every WASI provider. P2 consumes the Component Model service; package selection can install a smaller explicit subset. |
| `github.com/wago-org/workers` | `github.com/wago-org/workers` | bounded `instance.manage`, `instance.close.observe` | Provides `github.com/wago-org/workers/service` major 1 as an interface Contract. |
| `github.com/JairusSW/pool` | `github.com/JairusSW/pool` | None directly | Requires `github.com/wago-org/workers` with a semver range; requires Workers Contract major 1; provides `github.com/JairusSW/pool/service` major 1. |
| `github.com/JairusSW/lease` | `github.com/JairusSW/lease` | None | Consumes every selected `github.com/JairusSW/lease/source` major 1 provider and provides `github.com/JairusSW/lease/service` major 1. Source factories and one-shot execution remain callback-scoped; discovery never requires a process-local snapshot or factory. |
| `github.com/JairusSW/wide` | `github.com/JairusSW/wide` | `compiler.type.define` scoped to the `wide` type namespace and `compiler.instruction.define` scoped to `as-simd` | None. Guest `compiler.codegen` remains a separate guest capability. |
| `github.com/wago-org/component-model` | `github.com/wago-org/component-model` | `core.module.compile`, bounded `core.instance.instantiate`, `core.funcref.create` | Provides `github.com/wago-org/component-model/runtime` major 1 as an interface Contract. |
| `github.com/wago-org/net` | root aggregate plus the existing selective package IDs such as `/tcp`, `/udp`, `/dns`, `/icmpv4`, `/ntp`, `/mdns`, `/dhcpv4`, `/linklocal4`, `/ipv6`, `/icmpv6`, and `/dhcpv6` | `host.import.define` scoped to `wago_net` and only the selected protocol modules, `host.caller.identify`, `instance.instantiate.intercept`, `instance.close.observe` | Protocols compose inside one immutable provider factory. They are not separate runtime plugins accidentally coupled through package globals. |

Published definitions use exact semantic versions. During stacked development,
ecosystem pull requests may require the exact Wago vNext commit as a Go
pseudo-version; local absolute `replace` directives are test-only and never
committed.

## Contract rules for ecosystem APIs

Public Contracts use interfaces, not concrete plugin structs. That keeps a
consumer coupled to a stable behavior seam rather than the provider's storage or
implementation. Contract IDs do not contain a version suffix; the positive
major is a separate field.

Pool explicitly requires the Workers package as well as its Contract. The
package edge lets the CLI resolve and link Workers; the Contract edge verifies
and binds the runtime implementation. The lockfile records
`github.com/JairusSW/pool` as direct, Workers as transitive, and the exact
Pool-to-Workers binding. A diamond of multiple consumers selects Workers once.

Every dependency has a non-empty semantic-version constraint. Resolution selects
one exact version and `h1:` checksum for every direct and transitive Plugin ID.
Those dependency edges and the reviewed typed Contract binding edges form one
DAG. A required Contract binds exactly one selected provider, an optional
Contract binds zero or one and defaults to none when first introduced, and a
many Contract binds every selected matching provider in reviewed deterministic
order. Available optional providers and changes to a many binding require review.

Tests in the owning repositories cover:

- a happy-path typed call through `Ref.With`;
- calls before commit and after revocation failing closed;
- a call racing runtime close while provider teardown waits;
- consumer `Stop` using its provider before the reference is revoked;
- missing required, absent optional, many-provider, incompatible-major, and
  duplicate-single-provider plans where applicable; and
- the provider definition's declared Contracts exactly matching registration.

No Contract exposes a raw `Get`. A callback lease is the only supported way to
use the value, and its documented lifetime ends when the callback returns. As
with every other Wago integration boundary, trusted native Go code can violate
that contract by retaining the interface; Wago does not claim to sandbox it.

Providers start before consumers and consumers stop first. A consumer's
Contract references remain usable through its own `Stop`; Wago then revokes
them, so retained references fail closed. Before provider `Stop`, Wago rejects
new Contract calls and drains calls already in flight. Guest-callable callbacks
use a separate admission gate that closes before that plugin's `Stop` and drains
after `Stop` has had a chance to cancel admitted work.

## Authority rules for ecosystem APIs

Definitions request the largest scope the release can support. Consumers may
narrow any scope. `required` means an Authority grant must exist; `optional`
means it may be omitted entirely. Registration must either adapt to the actual
grant or fail before commit with a path-specific error.

Instance-owning requests and grants always contain positive `maxInstances` and
aggregate live `maxMemoryBytes`; zero never means unlimited. Plugin
configuration can impose a smaller product-level limit but cannot exceed the
core-enforced grant.

Observation handles receive immutable views and opaque comparable instance
identities, not raw `*Runtime`, `*Instance`, `*Module`, mutable imports, or call
arguments. Caller identification returns the same opaque identity used by
instantiate and close observations. A plugin needs a separate Authority for
every operation it can perform.

## Repository release gate

For each repository:

1. Replace v0 `wago.json` fields with `$schema` v1 and nested `package`
   metadata; remove aliases, `keywords`, `private`, relative submanifest paths,
   and placeholder providers.
2. Replace `ExtensionInfo`, optional lifecycle interfaces, `Registry`, old
   services, and `RegisterExtension` with `PluginDefinition`, one-method
   `Plugin`, `Registrar`, `PluginLifecycle`, typed Contracts, and explicit
   `Providers()` catalogs.
3. Validate configuration strictly and publish a draft-2020-12 object schema
   with `additionalProperties: false` when configuration exists.
4. Run `wago plugin catalog`, commit the resulting root `wago.providers.json`
   before tagging, and run `wago plugin catalog --check` in CI.
5. Test the exact Authority scopes, definition digest, catalog contents,
   registration/definition agreement, lifecycle rollback, and Contract wiring.
6. Run the repository's full test suite against the exact Wago vNext commit on
   its supported platforms. Hot paths also retain or add allocation benchmarks.
7. Update README examples and generated metadata. A search for v0 schema URIs,
   shorthand IDs, global registration, old capability names, and old service
   helpers must return no live API references.
8. After tagging, run `wago plugin publish`. The publisher executes only the
   current local catalog to check drift, then downloads the exact tagged module
   by version and `h1:` checksum and requires its `wago.json` `package` metadata
   and `wago.providers.json` to match. The registry independently downloads that
   exact artifact and reads both root files without executing plugin code.
