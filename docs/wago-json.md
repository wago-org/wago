# `wago.json` reference

`wago.json` is both Wago's project configuration and its open-source package
manifest. The same file can contain consumer settings, publish metadata, or both.

Add the schema URI for editor completion, inline documentation, and typo
detection:

```json
{
  "$schema": "https://wago.sh/v0/schema.json"
}
```

The canonical schema is also committed as [`schema.json`](../schema.json).
It uses JSON Schema draft 2020-12 and rejects unknown fields.

Validate a manifest in CI with any draft-2020-12 validator. For example:

```sh
npx --yes --package ajv-cli@5 --package ajv-formats@3 \
  ajv validate --spec=draft2020 -c ajv-formats \
  -s schema.json -d wago.json
```

## Local runtime settings

`settings` contains sparse project overrides for `wago run`, `wago build`, and
`wago validate`. They take precedence over the user's global configuration but
remain below explicit command flags:

```json
{
  "$schema": "https://wago.sh/v0/schema.json",
  "settings": {
    "features": {
      "simd": false
    },
    "optimizations": {
      "inline-loop-callees": true
    },
    "runtime": {
      "parallel": "auto"
    }
  }
}
```

Inside a project, `wago config` edits local overrides by default. Use
`wago config --global` for user-wide defaults, or select scope explicitly in
scripts with `--local`, `-l`, `--global`, or `-g`. Resetting a local setting
removes its override so it inherits the global value again. This field is
separate from opaque per-plugin `config`, which remains authority-bearing and
lockfile-owned.

## Plugin requirements

`plugins` maps GitHub-relative plugin IDs to semantic-version constraints. A
single entry declares the Go module build dependency and activates its
registration at runtime:

```json
{
  "$schema": "https://wago.sh/v0/schema.json",
  "plugins": {
    "wago-org/wasi": "^0.0.0",
    "wago-org/workers": "^0.0.0"
  }
}
```

Wago expands `wago-org/wasi` to `github.com/wago-org/wasi` for the Go module
build. `wago add <module>` resolves the installed version and writes a compatible
caret constraint. Exact resolution and runtime authority are kept out of the
manifest.

## `wago-lock.json`

The lockfile records exact versions, the capabilities declared by the package,
the subset reviewed and granted by the user, and opaque plugin configuration:

```json
{
  "plugins": {
    "wago-org/workers": {
      "version": "0.0.0",
      "requiredCapabilities": [
        "instance.manage"
      ],
      "capabilities": {
        "instance.manage": {
          "maxInstances": 8,
          "maxMemoryBytes": 4194304
        }
      },
      "config": {
        "maxWorkers": 8,
        "maxQueueBytes": 1048576
      }
    }
  }
}
```

Simple grants use a string array. The object form attaches core-enforced limits
to resource-owning capabilities while using `true` for an unlimited grant.
`maxInstances` limits simultaneously live managed instances.
`maxMemoryBytes` rejects modules whose declared maximum memory exceeds the
per-instance limit.

The lockfile is authority-bearing and parsed strictly. A malformed lockfile is
an error instead of being silently ignored. Lock entries not selected by
`wago.json` are ignored.

`wago add` and `wago plugin update` resolve manifest constraints and write exact
module versions here. `wago plugin outdated` reports newer module versions
without changing either file, while `wago plugin rebuild` reproduces the plugin
runtime from the locked versions.

### Host-integration capabilities

| Capability | Allows the plugin to |
|---|---|
| `host.imports` | Add host functions to Wasm import namespaces and resolve exact active caller identity. |
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

Plugin packages declare mandatory dependencies and default `before`/`after`
ordering in their compiled metadata.

Wago resolves one directed acyclic graph. Mandatory dependencies must be
selected. Missing optional `before`/`after` targets are ignored. Unrelated ready
plugins use lexical name order, making startup reproducible. Shutdown callbacks
run in reverse resolved order.

## Plugin configuration

Wago does not interpret a lock entry's `config`. The plugin decodes it through
`Registry.Config`. Plugins may expose a schema through `ConfigSchemaProvider`;
`wago plugin plan --json` includes it. A plugin must still reject invalid
configuration during transactional registration.

Use `wago plugin check` in CI to validate compiled availability, provenance,
grants, configuration registration, service dependencies, and load order. Use
`wago plugin plan` to inspect the exact order without starting plugins.

## Publishing an open-source package

When `module` is present, the schema also requires `license` and an HTTPS
`repository`:

```json
{
  "$schema": "https://wago.sh/v0/schema.json",
  "module": "github.com/acme/wago-observability",
  "version": "0.0.0",
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
| `plugins` | GitHub-relative plugin IDs mapped to version constraints. |
| `settings` | Sparse project-local runtime defaults layered over global settings. |
| `module` | Canonical Go module path for publishing. |
| `version` | Semantic package version. |
| `name`, `short`, `description` | Registry display and discovery metadata. |
| `license`, `repository`, `homepage` | Open-source provenance. |
| `category`, `tags` | Registry discovery metadata. `keywords` is the legacy alias for tags. |
| `authors` | String or structured author records. |
| `subpackages` | Inline manifests or relative child `wago.json` references. |
| `stability` | `experimental`, `stable`, or `deprecated`. |
| `engines`, `platforms` | Compatible toolchain ranges and GOOS/GOARCH targets. |
