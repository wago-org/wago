# CLI architecture

Wago presents one command while shipping two executables:

- `cli/wago` is the build-tagged entrypoint for both artifacts.
- The default build produces the manager installed as `wago`.
- A `wago_runtime` build produces an engine installed as `wago-runtime`.

The split is an implementation detail, not a split in the user experience. The
manager owns releases, authentication, installation state, self-management,
project setup, and plugin mutations. The runtime owns WebAssembly execution and
commands that inspect compiled engine or plugin state. The manager forwards
runtime commands without re-parsing their engine flags.

## Domain boundaries

Shared CLI foundations live directly under `cli/internal`:

```text
cli/internal/
  command/   command tree, parsing, dispatch, and help rendering
  handoff/   manager-to-runtime launch metadata and routing rules
  project/   wago.json, plugin intent, and local/global/bare scope
  tui/       shared radio, drill-down, and multi-select terminal interaction
  ui/        non-interactive output primitives
```

Manager-only workflows live under `cli/manager/internal`:

```text
cli/manager/internal/
  cache/     regenerable download/build cache inspection and cleanup
  config/    persistent user configuration and shell completion installation
  plugin/    plugin lifecycle, capability review, migration, and custom runtimes
  progress/  live status for long-running manager transactions
  registry/  credentials, OAuth, resolution, publishing, and registry HTTP
  self/      manager update, replacement, and uninstall
  status/    read-only manager, runtime, project, plugin, and lock reporting
  version/   installed runtime state, discovery, download, source fallback, selection
```

Runtime-only workflows live under `cli/runtime/internal`:

```text
cli/runtime/internal/
  module/    module loading shared by runtime inspection commands
  plugin/    manifest loading and compiled plugin inspection
  profile/   build-tag-selected runtime capabilities
  version/   runtime diagnostics and version reporting
```

`cli/internal/project` is the sole owner of project manifests and plugin intent.
Its model is deliberately runtime-neutral: the runtime adapts `PluginIntent`
values into engine configuration. Manager and runtime code must not define
separate `wago.json` models, and the shared project package must not import the
runtime facade.

`cli/internal/handoff.Metadata` is the sole definition of launch metadata. It
encodes and decodes the `WAGO_MANAGER_*` and `WAGO_RUNTIME_*` environment
contract. Routing decisions that determine whether the manager or runtime owns
an invocation live beside that contract and are tested independently. Runtime
command descriptions needed for cohesive manager help also live there, so the
manager never imports or links runtime command implementations.

## Commands

Both CLIs mirror their public command tree in source:

```text
cli/manager/commands/<command path>/command.go
cli/runtime/commands/<command path>/command.go
```

Each leaf `command.go` owns its name, aliases, arguments, flags, help,
command-level validation, and dispatch. Group `command.go` files only assemble
children in display order. Reusable stateful behavior belongs to the appropriate
internal domain package; an internal package must never import a command
package. Architecture tests enforce both that dependency direction and the
manager/runtime compile seam.

The root packages contain only entrypoint behavior, command composition,
environment adapters, forwarding, and top-level output:

```text
cli/manager/
  main.go
  command_registry.go
  command_environment.go

cli/runtime/
  main.go
  command_registry_standard.go
  command_registry_minimal.go
  command_environment.go
```

The manager's `version.Toolchain` is the transaction boundary for list,
install, switch, update, and uninstall operations. Plugin mutation requests and
registry requests are domain-owned values assembled by the root environment
adapter. Command packages do not coordinate download, build, migration, or
installation steps.

All interactive manager workflows also expose flags for automation. A command
without enough information may open a selector, but CI and scripts can always
provide the version, profile, build, scope, confirmation, and capability policy
directly. Bare `wago update` opens an all-selected multi-choice screen for the
manager, rolling runtime, and plugins. Positional targets and matching flags
provide one-shot updates; the narrower `self update`, `version update`, and
`plugin update` commands remain available for explicit control.

## Runtime profiles and builds

Standard runtimes expose `run`, `build`, `validate`, `module`, and plugin
inspection. Minimal runtimes expose only `run`. A plugin-aware runtime is a
standard runtime build whose generated entrypoint imports plugin registration
packages; it does not become a third CLI role.

`run --watch` is runtime-owned. It relaunches the same runtime executable when
the input module changes, preserving guest arguments and plugin selection made
by the manager handoff.

The manager is the default Go build. Runtime builds require the `wago_runtime`
tag so an entrypoint cannot silently produce the wrong role:

```bash
go build -o wago ./cli/wago
go build -tags wago_runtime -o wago-runtime ./cli/wago
go build -tags wago_runtime,wago_minimal -o wago-runtime-minimal ./cli/wago
```

Prefer `make build-manager`, `make build-runtime-standard`, and
`make build-runtime-minimal` in automation.

## Adding or changing a command

1. Decide whether the operation owns manager state or compiled runtime state.
2. Put its CLI definition in the matching `commands/<path>/command.go`.
3. Put reusable behavior in the narrowest domain-owned internal package.
4. Adapt command options to domain requests in the CLI root.
5. If the runtime implements a manager-visible command, forward the original
   arguments and extend `cli/internal/handoff` only when the launch contract
   itself changes.
6. Test the command contract, the domain transaction, and both build-tag views:

```bash
go test ./cli/...
go test -tags wago_runtime ./cli/...
```
