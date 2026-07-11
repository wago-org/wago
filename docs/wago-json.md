# `wago.json` reference

`wago.json` is both Wago's project configuration and its open-source package
manifest. The same file can contain consumer settings, publish metadata, or both.

Add the schema URI for editor completion, inline documentation, and typo
detection:

```json
{
  "$schema": "https://wago.sh/schema.json",
  "schema": "wago/v1"
}
```

The canonical schema is also committed as [`schema.json`](../schema.json).
It uses JSON Schema draft 2020-12 and rejects unknown fields. Plugin-owned
`config` is the deliberate exception: its shape belongs to that plugin.

Validate a manifest in CI with any draft-2020-12 validator. For example:

```sh
npx --yes --package ajv-cli@5 --package ajv-formats@3 \
  ajv validate --spec=draft2020 -c ajv-formats \
  -s schema.json -d wago.json
```

## Dependencies versus plugins

These are intentionally separate:

- `dependencies` lists public Go modules compiled into the generated Wago host.
- `plugins` lists the compiled plugin registrations activated at runtime.
- `plugins[].capabilities` grants privileged access to Wago integration APIs.

Compiling code does not activate it, and activation does not grant authority.

```json
{
  "$schema": "https://wago.sh/schema.json",
  "schema": "wago/v1",
  "dependencies": [
    "github.com/wago-org/wasi",
    "github.com/wago-org/workers"
  ],
  "plugins": [
    {
      "name": "wasi",
      "capabilities": ["host.imports", "host.environment"]
    },
    {
      "name": "workers",
      "capabilities": ["instance.manage"]
    }
  ]
}
```

`wago pkg add <module>` adds the module dependency and scaffolds a plugin entry
with an empty capability list. Review the plugin source, then grant only the
capabilities it needs.

## Plugin entries

Each plugin entry supports:

| Field | Required | Meaning |
|---|:---:|---|
| `name` | yes | Registry name compiled into the host, such as `wasi`, `workers`, or `wasi/p1`. |
| `capabilities` | yes | Explicit Wago host-integration grants. May be empty. |
| `before` | no | Load this plugin before the named selected plugins. |
| `after` | no | Load this plugin after the named selected plugins. |
| `config` | no | Arbitrary plugin-owned JSON passed through `Registry.Config`. |

Duplicate plugin names, duplicate capabilities, missing mandatory plugin
dependencies, unknown capabilities, and load-order cycles are rejected before
the plan commits.

### Host-integration capabilities

| Capability | Allows the plugin to |
|---|---|
| `host.imports` | Add host functions to Wasm import namespaces. |
| `host.environment` | Read the narrow host environment explicitly exposed by Wago. |
| `runtime.lifecycle` | Observe runtime shutdown and release plugin resources. |
| `module.compile` | Transform Wasm bytes before compilation or observe compiled modules. |
| `instance.lifecycle` | Observe or affect instantiation and instance close. |
| `instance.invoke` | Veto or observe runtime-managed function calls. |
| `instance.manage` | Create and own restricted managed instances for workers, pools, schedulers, and routers. |

These grants do not sandbox arbitrary Go code. Plugins are forced-open-source,
compiled into the consumer's binary, and expected to be audited like any other
Go dependency. The grants control access to privileged Wago API surfaces.

Guest permissions such as `fs.read`, `net.outbound`, or `wasi` are different:
plugins provide those to Wasm modules, and runtime `Policy` controls whether a
module may use them.

## Load ordering

Plugin packages may declare mandatory dependencies and default ordering. A
project may add `before` and `after` constraints:

```json
{
  "plugins": [
    {
      "name": "tracing",
      "capabilities": ["instance.invoke"],
      "before": ["metrics"]
    },
    {
      "name": "metrics",
      "capabilities": ["instance.invoke"]
    }
  ]
}
```

Wago resolves one directed acyclic graph. Mandatory dependencies must be
selected. Missing optional `before`/`after` targets are ignored. Unrelated ready
plugins use lexical name order, making startup reproducible. Shutdown callbacks
run in reverse resolved order.

## Plugin configuration

Wago does not interpret `config`:

```json
{
  "name": "workers",
  "capabilities": ["instance.manage"],
  "config": {
    "maxWorkers": 8,
    "maxQueueBytes": 1048576
  }
}
```

The plugin decodes this value through `Registry.Config`. Consult that plugin's
own schema or documentation for supported fields. A plugin should reject invalid
configuration during transactional registration.

## Publishing an open-source package

When `module` is present, the schema also requires `schema`, `license`, and an
HTTPS `repository`:

```json
{
  "$schema": "https://wago.sh/schema.json",
  "schema": "wago/v1",
  "module": "github.com/acme/wago-observability",
  "version": "1.2.0",
  "name": "Wago Observability",
  "short": "observability",
  "description": "Tracing and metrics plugins for Wago hosts.",
  "license": "Apache-2.0",
  "repository": "https://github.com/acme/wago-observability",
  "homepage": "https://github.com/acme/wago-observability#readme",
  "category": "observability",
  "tags": ["metrics", "tracing"],
  "authors": [
    {
      "name": "Example Maintainer",
      "email": "maintainer@example.com"
    }
  ],
  "subpackages": ["./metrics/wago.json", "./tracing/wago.json"]
}
```

`version` follows semantic versioning and may omit a leading `v`. Publishing
falls back to the newest Git tag when it is absent. `license` is an SPDX license
expression. Relative subpackage manifests are recursively inlined before upload.

Manifest-loaded runtime plugins additionally declare repository and license
provenance in their compiled `ExtensionInfo`; Wago validates that metadata during
plugin planning.

## Root fields

| Field | Purpose |
|---|---|
| `$schema` | Editor-facing JSON Schema URI. |
| `schema` | Wago manifest format, currently `wago/v1`. |
| `dependencies` | Go modules compiled into the custom host. |
| `plugins` | Runtime plugin activation, grants, ordering, and config. |
| `module` | Canonical Go module path for publishing. |
| `version` | Semantic package version. |
| `name`, `short`, `description` | Registry display and discovery metadata. |
| `license`, `repository`, `homepage` | Open-source provenance. |
| `category`, `tags` | Registry discovery metadata. `keywords` is the legacy alias for tags. |
| `authors` | String or structured author records. |
| `subpackages` | Inline manifests or relative child `wago.json` references. |
| `stability` | `experimental`, `stable`, or `deprecated`. |
| `engines`, `platforms` | Compatible toolchain ranges and GOOS/GOARCH targets. |
