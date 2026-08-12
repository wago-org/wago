# ADR 0002: Link explicit plugin providers and grant exact authorities

## Status

Accepted for the unreleased v1 plugin format.

## Context

The original plugin API mixed package discovery, process-global registration,
runtime mutation, coarse capabilities, and unversioned shared values. That made
the result depend on import side effects and left no trustworthy point at which
the CLI could review a complete dependency graph before running downloaded Go
code. It also made teardown unsafe: a consumer could retain a provider value
after the provider had stopped.

Wago plugins are trusted native Go code, not sandboxed Wasm. The boundary can
therefore make privileged integration auditable and reproducible, but it cannot
contain arbitrary behavior inside plugin code.

## Decision

Every linked source exports an explicit `Providers()` catalog. A provider pairs
an immutable Plugin Definition with `New` and optional configuration validation;
the runtime never discovers providers through `init` or mutable global state.
The lockfile records the exact source, checksum, definition digest, dependency
edges, Authority Grants, configuration, and Contract bindings reviewed by the
user.

Every source module commits canonical module-root `wago.providers.json`, written
by `wago plugin catalog` from its current `/register` catalog and identified by
`https://wago.sh/v1/providers.schema.json`. The snapshot is committed before the
release tag; `wago plugin catalog --check` rejects drift. Publication executes
only the current local catalog to perform that drift check, then downloads the
exact tagged Go module by version and `h1:` checksum. Its `wago.json` `package`
metadata and `wago.providers.json` must match the local release inputs. The
registry independently downloads the exact `h1:` artifact and reads both root
files without executing plugin code.

The resolver selects an exact version and checksum for every direct and
transitive Plugin Requirement satisfying all non-empty constraints. Those
requirement edges and typed Contract bindings form one DAG. A required Contract
binds exactly one selected provider, an optional Contract binds zero or one, and
a many Contract binds every selected matching provider in reviewed order. Wago
rejects missing or incompatible providers, unreviewed bindings, and cycles before
registration, then starts providers before consumers and stops consumers before
providers. Contract values are accessible only through callback-scoped
references. Teardown runs the consumer's `Stop` while dependencies remain usable,
then revokes its references; retained references fail closed. Before provider
`Stop`, Wago rejects new Contract calls and drains calls already in flight.
Guest-callable callbacks use a separate gate: Wago closes admission, lets that
plugin's `Stop` cancel admitted work, and then drains it before releasing
plugin-owned state.

Each privileged integration operation has an exact, non-inheriting Plugin
Authority. A grant can omit an optional request or narrow its named resources
and positive instance or aggregate-memory limits, but cannot widen the published
request. New authorities are denied unless a future plugin definition requests
them and a future lockfile grants them.

Installation stages source verification, provider linking, definition and
configuration validation, registration, and the generated runtime together.
The manifest, lockfile, and runnable artifact are replaced only after the whole
staged plan succeeds. Metadata inspection never invokes plugin factories or
lifecycle code.

## Consequences

- Builds need a generated provider table. Publishers must commit the provider
  snapshot before tagging, and the tagged package metadata and snapshot must
  match the reviewed release inputs.
- Plugin IDs are full canonical Go paths and releases use semantic versions;
  v0 aliases, manifests, and registration APIs are rejected rather than
  reinterpreted.
- Cross-plugin APIs require a stable interface Contract and a positive major
  version. Breaking interface changes use a new major and an explicit binding.
- Authority review and lifecycle handling are more verbose, but resolution,
  activation, inspection, rollback, and shutdown become deterministic and
  testable.
- This design limits access to Wago-owned integration seams. It does not sandbox
  native plugin code or make an untrusted Go dependency safe.
