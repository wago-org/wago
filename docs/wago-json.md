# `wago.json` and `wago-lock.json`

`wago.json` contains human-owned project intent. `wago-lock.json` contains the
machine-resolved, authority-bearing graph. The generated runtime is a cache of
that graph, never a source of intent.

The v1 formats are intentionally incompatible with the unreleased v0 design.
Unknown fields, shorthand Plugin IDs, coarse capabilities, and old lock entries
are errors rather than migration hints.

## `wago.json`

Start every manifest with the canonical schema URI:

```json
{
  "$schema": "https://wago.sh/v1/schema.json"
}
```

The canonical draft-2020-12 schema is committed as
[`schema.json`](../schema.json) and served at the URI above. It rejects unknown
fields. Validate the local copy in CI with any conforming validator, for example:

```sh
npx --yes --package ajv-cli@5 --package ajv-formats@3 \
  ajv validate --spec=draft2020 -c ajv-formats \
  -s schema.json -d wago.json
```

### Direct Plugin Requirements

`plugins` maps canonical Go module or package paths to semantic-version ranges:

```json
{
  "$schema": "https://wago.sh/v1/schema.json",
  "plugins": {
    "github.com/JairusSW/pool": "^0.1.0",
    "github.com/wago-org/wasi": "~0.2.0"
  }
}
```

The full path is the Plugin ID in the manifest, registry, generated provider
catalog, lock graph, runtime plan, and diagnostics. Relative aliases such as
`JairusSW/pool` are invalid.

Ranges accept exact versions, comparators, caret and tilde ranges, partial and
x-ranges, hyphen ranges, whitespace-separated intersections, and `||` unions.
Pre-release matching follows node-semver rules. Examples include `1.2.3`,
`^1.2.0`, `~1.2.3`, `>=1.2 <2`, `1.2.x`, `1.2.3 - 2.0.0`, and
`^1.0.0 || ^2.0.0`.

Only direct requirements belong in the manifest. The resolver discovers and
locks transitive requirements from published Plugin Definitions.

### Project settings

`settings` contains sparse project overrides for `wago run`, `wago build`,
`wago compile`, and `wago validate`:

```json
{
  "$schema": "https://wago.sh/v1/schema.json",
  "settings": {
    "features": {
      "simd": false
    },
    "optimizations": {
      "inline-loop-callees": true
    },
    "runtime": {
      "parallel": "auto",
      "deferredBoundsChecking": false
    }
  }
}
```

Project values override the user's global defaults and remain below explicit
command flags. Resetting a project setting removes it so the global value is
inherited again. Experimental settings require the CLI's `--experimental`
acknowledgement.

### Package metadata

A publishable plugin module puts public discovery and provenance metadata under
`package`:

```json
{
  "$schema": "https://wago.sh/v1/schema.json",
  "package": {
    "module": "github.com/acme/wago-observability",
    "version": "0.1.0",
    "name": "Wago Observability",
    "description": "Tracing and metrics for Wago runtimes.",
    "stability": "experimental",
    "license": "Apache-2.0",
    "repository": "https://github.com/acme/wago-observability",
    "homepage": "https://github.com/acme/wago-observability#readme",
    "category": "observability",
    "tags": ["metrics", "tracing"],
    "authors": [
      {
        "name": "Example Maintainer",
        "email": "maintainer@example.com",
        "github": "example"
      }
    ],
    "engines": {
      "wago": "^0.1.0"
    },
    "platforms": ["darwin/arm64", "linux/amd64"]
  }
}
```

`module`, `name`, `description`, `license`, `repository`, and at least one author
are required for publication. `repository` is an absolute HTTPS URL and
`license` is an SPDX expression. If `version` is absent, the publisher resolves
the newest Git tag and includes that exact version in the signed publish
request.

The manifest does not hand-author provider definitions or authority requests.
Run `wago plugin catalog` to execute the current module's explicit `/register`
catalog and write canonical `wago.providers.json` at the module root. The
snapshot uses
[`https://wago.sh/v1/providers.schema.json`](../providers.schema.json). Commit it
before creating the release tag, and use `wago plugin catalog --check` in CI to
reject a missing or stale snapshot.

`wago plugin publish` executes only the current local `/register` catalog, and
only to check snapshot drift. It then downloads the exact tagged Go module at
the selected version and `h1:` checksum. The tagged artifact's `wago.json`
`package` metadata must match the local manifest, and its `wago.providers.json`
must exactly match the current canonical snapshot, before anything is submitted.
The registry independently downloads that exact module and verifies its `h1:`
checksum, then reads both root files without building or executing plugin code.

Modules that publish several Plugin IDs describe discovery-only child entries
with `package.subpackages`. Their definitions still come from the single
explicit catalog; relative child-manifest loading is not part of v1.

### Root fields

| Field | Purpose |
|---|---|
| `$schema` | Must be `https://wago.sh/v1/schema.json`. |
| `plugins` | Direct canonical Plugin IDs mapped to version ranges. |
| `settings` | Sparse project-local runtime defaults. |
| `package` | Public package metadata used only when publishing. |

No grant or plugin configuration appears in `wago.json`; both are reviewed lock
state.

## `wago-lock.json`

The lockfile records everything needed to reproduce the selected plugin plan:

- direct and transitive Plugin IDs;
- source module, exact Go module version, and `h1:` checksum;
- exact `/register` import path and immutable definition digest;
- registry release fingerprint covering source and every published definition;
- non-empty dependency constraints copied from the selected definition;
- published Authority Requests and consumer-reviewed Authority Grants;
- provided and consumed Contract IDs, majors, and modes;
- exact consumer-to-provider Contract bindings; and
- opaque plugin configuration.

For a project that directly selects Pool while Pool depends on Workers, a lock
graph has this shape (digests and checksums shortened only for this explanation):

```json
{
  "formatVersion": 1,
  "plugins": {
    "github.com/JairusSW/pool": {
      "direct": true,
      "source": {
        "module": "github.com/JairusSW/pool",
        "version": "v0.1.2",
        "checksum": "h1:complete-go-module-checksum"
      },
      "provider": {
        "importPath": "github.com/JairusSW/pool/register"
      },
      "definitionDigest": "sha256:complete-definition-digest",
      "releaseFingerprint": "sha256:complete-release-fingerprint",
      "dependencies": {
        "github.com/wago-org/workers": "^0.1.0"
      },
      "requestedAuthorities": [],
      "grants": [],
      "contracts": {
        "provides": [
          {
            "id": "github.com/JairusSW/pool/service",
            "major": 1
          }
        ],
        "requires": [
          {
            "id": "github.com/wago-org/workers/service",
            "major": 1,
            "mode": "required"
          }
        ]
      },
      "bindings": [
        {
          "id": "github.com/wago-org/workers/service",
          "major": 1,
          "providers": ["github.com/wago-org/workers"]
        }
      ],
      "config": {}
    },
    "github.com/wago-org/workers": {
      "direct": false,
      "source": {
        "module": "github.com/wago-org/workers",
        "version": "v0.1.4",
        "checksum": "h1:complete-go-module-checksum"
      },
      "provider": {
        "importPath": "github.com/wago-org/workers/register"
      },
      "definitionDigest": "sha256:complete-definition-digest",
      "releaseFingerprint": "sha256:complete-release-fingerprint",
      "dependencies": {},
      "requestedAuthorities": [
        {
          "name": "instance.manage",
          "mode": "required",
          "reason": "own a bounded set of worker instances",
          "scope": {
            "maxInstances": 8,
            "maxMemoryBytes": 67108864
          }
        }
      ],
      "grants": [
        {
          "name": "instance.manage",
          "scope": {
            "maxInstances": 8,
            "maxMemoryBytes": 67108864
          }
        }
      ],
      "contracts": {
        "provides": [
          {
            "id": "github.com/wago-org/workers/service",
            "major": 1
          }
        ],
        "requires": []
      },
      "bindings": [],
      "config": {
        "maxWorkers": 8,
        "maxQueueBytes": 1048576
      }
    }
  }
}
```

Actual lockfiles contain complete SHA-256 definition digests and Go module
checksums. Empty arrays, objects, and config values are written explicitly so a
missing field cannot be confused with an older format or a default grant.

### Authority policy

An Authority Request is immutable publisher metadata:

```json
{
  "name": "host.import.define",
  "mode": "required",
  "reason": "provide the wasi_snapshot_preview1 API",
  "scope": {
    "modules": ["wasi_snapshot_preview1"]
  }
}
```

An Authority Grant is consumer-reviewed state. It can equal or narrow any
request but can never add a module, increase a resource limit, or grant an
authority that was not requested. A required Authority must remain present; an
optional Authority may be omitted. If a plugin cannot operate within a narrowed
required scope, its registration fails and the transaction is cancelled.
Authority names are exact and non-inheriting.

The CLI authors narrower grants with one strict JSON document rather than
repeated flags, which the command parser does not accumulate:

```sh
wago plugin grant github.com/acme/plugin --scopes '{
  "github.com/acme/plugin": {
    "host.import.define": {"modules": ["clock"]},
    "instance.manage": {"maxInstances": 2, "maxMemoryBytes": 67108864}
  }
}'
```

The same `--scopes` document is accepted by `wago add` and `wago plugin update`
and may name direct or transitive plugins in the resolved graph. Scope overrides
are applied to the in-memory lock candidate, validated as non-widening, and
included in the staged runtime validation before any lockfile or artifact is
published. Optional Authorities still require an explicit `--allow`,
`--allow-all`, or interactive choice.

`host.import.define` and `compiler.instruction.define` accept exact module-name
scopes; `compiler.type.define` accepts exact type namespaces. Instance-owning
authorities require positive `maxInstances` and aggregate live
`maxMemoryBytes`; zero is not an implicit unlimited grant. Other authorities
reject scope fields they cannot enforce.

The grants control access to privileged Wago handles. They do not sandbox the
ordinary Go code linked into the process.

### Contract bindings

A Contract identity is its canonical ID plus incompatible positive major
version. Consumption mode is one of:

- `required`: exactly one selected provider;
- `optional`: zero or one selected provider; or
- `many`: zero or more selected providers in the exact reviewed binding order.

A newly introduced optional consumption with available providers requires
review instead of silently choosing one. A `many` binding contains every
selected matching provider. On update, surviving providers keep their reviewed
order, new providers are appended in lexical Plugin ID order, and additions or
removals require review.

Every consumption has a `bindings` entry, including an empty binding for an
unavailable optional or many Contract. Locked replay compares those provider IDs
with the linked graph rather than choosing again. A missing provider,
incompatible major, duplicate single provider, altered binding, or cycle rejects
the complete plan before a plugin factory runs.

The resolver selects exact versions and checksums for every direct and transitive
requirement. Those requirement edges and the reviewed Contract binding edges are
validated as one DAG. Providers start before consumers and consumers stop first.
During consumer `Stop`, its dependency references still work. Wago then revokes
them, so a retained reference fails closed. Before provider `Stop`, Wago rejects
new Contract calls and waits for existing leased calls to return. Guest-callable
callbacks use a separate admission gate: Wago closes it before that plugin's
`Stop`, lets `Stop` cancel admitted work, and then drains it.

### Strictness and CI

The lock parser rejects unknown and missing fields, unsupported format versions,
invalid IDs or ranges, malformed checksums or digests, invalid scopes, widened
grants, inconsistent bindings, missing transitive resolutions, and graph cycles.
Entries not reachable from a direct manifest requirement are invalid rather
than silently ignored.

Use these commands to inspect and verify it:

```sh
wago plugin tree
wago plugin list --json
wago plugin inspect github.com/JairusSW/pool
wago plugin rebuild --locked
```

`tree` explains direct and transitive causes. `list --json` and `inspect` read
linked immutable definitions and side-effect-free plan entries without starting
plugins. `rebuild --locked` verifies the registry release, source checksum,
committed provider snapshot, linked definition digests, configuration, grants,
bindings, and dry-run Plugin Plan, then reproduces the artifact without changing
resolution or authority.
