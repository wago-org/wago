# Plugin scopes and build isolation

Wago separates plugin **intent** from compiled plugin **artifacts**.

- A manifest says which packages are dependencies, which plugin registrations
  are enabled, and which capabilities they receive.
- A generated plugin runtime is a cache for one exact Wago
  version/profile/build. It is never reused by another toolchain.

## Local projects

`wago add` is local by default:

```sh
wago add wago-org/wasi
```

`wago plugin add` is the equivalent grouped form. Remove a plugin with
`wago rm wago-org/wasi` or `wago plugin remove wago-org/wasi`. Plugin commands
use `add` and `remove`; `install` and `uninstall` are reserved for
`wago version`.

If the current directory has no `wago.json`, the command creates one. Inside a
Git working tree it also adds `.wago/` to `.gitignore`. `wago init` performs
that initialization without adding a package and is safe to repeat.

Local intent lives in:

```text
<project>/wago.json
```

Local generated runtimes live in:

```text
<project>/.wago/builds/<version>/<profile>/<build>/
```

A local manifest is isolated: it replaces the global plugin set rather than
merging with machine-specific state.

## Global plugins

Use `--global` (`-g`) when a plugin should be enabled for commands outside
projects with their own manifest:

```sh
wago add --global wago-org/wasi
```

Global intent is shared by installed Wago versions. On macOS it is
`~/.wago/wago.json`; Linux uses Wago's XDG data directory. `WAGO_HOME`
relocates the complete layout.

Each toolchain still compiles an isolated runtime:

```text
<Wago data>/versions/<version>/<profile>/<build>/plugins/
```

The first command using a different Wago version/profile/build builds that
artifact lazily from the shared manifest. Version switching therefore needs no
plugin-copy prompt.

## Runtime selection

`wago run` resolves one scope:

1. `--bare` disables plugins.
2. `--global` ignores a project manifest and uses global intent.
3. A local `wago.json` selects the isolated local set.
4. Otherwise Wago uses global intent.

Examples:

```sh
wago run app.wasm
wago run --local app.wasm
wago run --global app.wasm
wago run --bare app.wasm
wago plugin list
wago plugin list --global
wago plugin inspect wago-org/wasi --local
```

Wago never merges local and global sets implicitly. This keeps project behavior
reproducible across machines. Plugin changes default to local; plugin
inspection follows the active scope (local when `wago.json` exists, otherwise
global) and accepts `--local` or `--global` to override it.

## Compatibility migration

Older Wago versions stored global manifests beneath a version directory. The
first compatible manager/runtime invocation copies that manifest and lock file
to shared global intent. Legacy files remain in place as a recoverable fallback;
compiled binaries are rebuilt in the new toolchain-specific layout.
