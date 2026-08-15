# Plugin scopes, resolution, and build isolation

Wago separates project intent, reviewed resolution, and generated artifacts:

- `wago.json` contains direct Plugin IDs and semantic-version ranges.
- `wago-lock.json` contains the complete direct and transitive graph, exact
  sources and checksums, immutable definition digests, reviewed Authority
  Grants, Contract bindings, and configuration.
- `.wago/` contains replaceable generated build artifacts for one exact Wago
  release, profile, target, and lock fingerprint.

Plugin IDs are canonical Go module or package paths everywhere. `wago add`
accepts `owner/repository` as GitHub shorthand, so `wago add wago-org/wasi`
resolves and stores the canonical `github.com/wago-org/wasi` Plugin ID. Other
hosts must be written as fully qualified paths; Wago does not infer them.

## Local projects

`wago add` changes the current project by default:

```sh
wago add wago-org/wasi
wago add github.com/JairusSW/pool
```

`wago plugin add` is the grouped spelling. `wago rm` and
`wago plugin remove` remove direct requirements. Add and remove operate on the
complete graph: transitive packages are deduplicated, shared diamond
dependencies stay selected once, and unreachable transitive packages are
pruned. If removal changes an optional or `many` Contract binding, Wago shows
the exact old and proposed provider lists before publishing the new graph. In
automation, pass `--accept-contracts`; omission fails without changing files.

If the directory has no `wago.json`, `wago init` creates one. Inside a Git work
tree it also ignores `.wago/`. Project intent lives at:

```text
<project>/wago.json
<project>/wago-lock.json
```

Generated project runtimes live beneath:

```text
<project>/.wago/builds/<wago>/<profile>/<target>/<lock-fingerprint>/
```

The generated directory is a cache, never authority or intent. Deleting it is
safe; a locked rebuild must reproduce it from the manifest and lockfile.

## One install transaction

Add, update, and remove use the same all-or-nothing transaction:

1. Read the current manifest and strict lockfile.
2. Resolve direct and transitive release metadata in memory.
3. Reject incompatible ranges, missing packages, missing or duplicate Contract
   providers, and explicit or Contract dependency cycles.
4. Present one consolidated review of new or widened Authorities, exact scopes,
   dependency causes, source versions, and Contract bindings.
5. Stage module downloads, checksum verification, explicit provider catalog,
   generated runtime build, definition-digest verification, configuration
   validation, and a complete plugin-plan dry run.
6. Atomically replace the manifest, lockfile, and artifact only after every
   prior step succeeds.

Rejecting a required Authority cancels the transaction. Optional Authorities
may be denied or narrowed. An error, interruption, failed build, mismatched
definition, or invalid plugin leaves the previous project and runnable artifact
unchanged.

## Author narrower grants

`wago plugin grant` accepts one strict `--scopes` JSON document. Its outer keys
are full Plugin IDs, its inner keys are exact Authority names, and each value is
the complete desired scope:

```sh
wago plugin grant github.com/acme/plugin --scopes '{
  "github.com/acme/plugin": {
    "host.import.define": {
      "modules": ["clock"]
    },
    "instance.manage": {
      "maxInstances": 2,
      "maxMemoryBytes": 67108864
    }
  }
}'
```

The Plugin ID appears in the document deliberately: the same shape works with
`wago add` and `wago plugin update`, where an override may target any direct or
transitive plugin in the newly resolved graph. For example, an install can
approve no optional Authorities while narrowing a required transitive worker
budget before any new runtime is published:

```sh
wago add github.com/acme/pool --deny-all --scopes '{
  "github.com/acme/workers": {
    "instance.manage": {
      "maxInstances": 2,
      "maxMemoryBytes": 67108864
    }
  }
}'
```

`modules` must contain at least one exact name drawn from the request. Resource
scopes must provide both positive `maxInstances` and positive aggregate
`maxMemoryBytes`. An override cannot add a module, increase either limit, target
an unscoped or unrequested Authority, or target an Authority that is not
granted. Use `--allow` or `--allow-all` alongside `--scopes` when selecting a
scoped optional Authority; `--scopes` changes limits, not the optional-grant
choice. Unknown fields, duplicate JSON keys, duplicate modules, malformed or
trailing JSON, and references outside the resolved lock graph fail before the
existing staged build and atomic publish boundary.

`--locked` never resolves a different version, changes grants or bindings, or
writes project files. It fails if the manifest, lock graph, checksum, linked
definitions, or generated artifact disagree.

## Global plugins

Use `--global` (`-g`) to manage the shared user scope:

```sh
wago add --global github.com/wago-org/wasi
wago plugin update --global
```

On macOS and Windows the shared manifest and lockfile are under `~/.wago/`.
Linux uses Wago's XDG data directory. `WAGO_HOME` relocates the complete layout.

Intent is shared across installed Wago versions, but compiled runtimes are not:

```text
<Wago data>/versions/<wago>/<profile>/<target>/plugins/<lock-fingerprint>/
```

Changing Wago version, target, profile, or lock fingerprint selects a different
artifact. Wago never copies a generated runtime across those boundaries.

## Runtime selection

Runtime commands choose one scope:

1. `--bare` disables plugins.
2. `--global` selects the shared user scope.
3. `--local` requires the current project scope.
4. Otherwise a nearby `wago.json` selects local scope; without one, Wago uses
   global scope.

```sh
wago run app.wasm
wago run --local app.wasm
wago run --global app.wasm
wago run --bare app.wasm
wago plugin list
wago plugin list --global
wago plugin inspect github.com/wago-org/wasi --local
```

Local and global plugin sets are never merged implicitly. A project therefore
does not acquire machine-specific plugins just because a contributor installed
them globally.

## Understand and verify a graph

```sh
wago plugin tree
wago plugin list --json
wago plugin inspect github.com/JairusSW/pool
wago plugin rebuild --locked
```

`tree` identifies each direct or transitive cause and locked release. JSON
listing and inspection report linked definitions, requested and granted scopes,
Contract consumers and providers, and activation order without starting plugin
code. `rebuild --locked` is the CI entry point for resolution, provenance,
build, digest, configuration, grant, binding, and plan verification.

## No legacy migration

The vNext format is a clean pre-release break. Wago rejects the v0 schema,
relative Plugin IDs, old coarse capabilities, old lock entries, and global
`init` registration. Create a v1 manifest and run `wago add` to produce a fresh,
reviewed lock graph. Old files are not silently reinterpreted because doing so
could manufacture authority or choose a different dependency graph.
