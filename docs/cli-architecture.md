# CLI architecture

Wago presents one command while shipping two executables:

- `cli/wago` is the build-tagged entrypoint for both artifacts.
- The default build produces the manager installed as `wago`.
- A `wago_runtime` build produces an engine installed as `wago-runtime`.

The split is an implementation detail, not a split in the user experience. The
manager owns releases, authentication, installation state, self-management,
project setup, and plugin mutations. The runtime owns WebAssembly execution and
commands that inspect compiled engine or plugin state. The manager forwards
runtime commands without re-parsing or consuming their engine flags. It does
observe shared automation flags before handoff so errors raised by the manager
itself still honor `--json`, `--no-input`, `--dry-run`, `--locked`, and
`--offline`; pass-through arguments after a module path remain guest-owned.

## Domain boundaries

Shared CLI foundations live directly under `cli/internal`:

```text
cli/internal/
  command/   command tree, parsing, dispatch, and help rendering
  handoff/   manager-to-runtime launch metadata and routing rules
  project/   v1 manifests, strict lock graphs, Plugin Requirements, and scope
  settings/  user runtime defaults, catalog, validation, and persistence
  tui/       shared radio, drill-down, and multi-select terminal interaction
  ui/        non-interactive output primitives
```

Manager-only workflows live under `cli/manager/internal`:

```text
cli/manager/internal/
  cache/     regenerable download/build cache inspection and cleanup
  config/    configuration TUI and shell completion installation
  plugin/    resolution, Authority review, staged builds, and atomic publication
  progress/  live status for long-running manager transactions
  registry/  credentials, OAuth, resolution, publishing, and registry HTTP
  self/      manager update, replacement, and uninstall
  standalone/ isolated native executable builds and target selection
  status/    read-only manager, runtime, project, plugin, and lock reporting
  version/   installed runtime state, discovery, download, source fallback, selection
```

Runtime-only workflows live under `cli/runtime/internal`:

```text
cli/runtime/internal/
  module/    module loading shared by runtime inspection commands
  plugin/    explicit provider catalogs, locked selections, planning, and inspection
  profile/   build-tag-selected runtime capabilities
  version/   runtime diagnostics and version reporting
```

`cli/internal/project` is the sole owner of v1 project manifests, complete lock
graphs, and Plugin Requirements. Its model is deliberately runtime-neutral: the
runtime adapts strict locked selections into engine configuration. Manager and
runtime code must not define separate `wago.json` or `wago-lock.json` models,
and the shared project package must not import the runtime facade.

`wago compile` is manager-owned because it orchestrates the Go toolchain and
cross-target builds. Its generated module embeds the Wasm command, imports the
active plugins' explicit `/register` provider catalogs, and embeds their exact
definition digests, Authority Grants, configuration, and Contract bindings. Each
package exports an explicit provider catalog; none self-registers through
`init`. The generated entry point also records the selected `--invoke`
export, Core feature set, function-worker policy, and resolved compiler-knob
overrides; `cli/internal/wasmcall` keeps its typed argument and result behavior in
parity with `wago run`. Command-line-only plugins are resolved into the isolated
Go module and imported through their conventional `/register` package. Linked
definitions must match the lockfile before activation. The resulting executable
imports `cli/standalone`, not the runtime CLI. The manager
reads the architecture-neutral settings and parallel-policy packages, while the
generated target applies the settings through the selected runtime backend.
GOOS/GOARCH select exactly one build-tagged Railshot backend, so an AMD64
executable does not link ARM64 codegen and vice versa. `--watch` intentionally
has no standalone equivalent because the embedded module cannot change.

`cli/internal/handoff.Metadata` is the sole definition of launch metadata. It
encodes and decodes the `WAGO_MANAGER_*` and `WAGO_RUNTIME_*` environment
contract. Routing decisions that determine whether the manager or runtime owns
an invocation live beside that contract and are tested independently. Runtime
command descriptions needed for cohesive manager help also live there, so the
manager never imports or links runtime command implementations.

`cli/internal/settings` owns runtime-default behavior, storage, and scope, but
not the feature or optimization inventory. A registered boolean setting owns
its canonical identity, value access, mutation, maturity, availability, and
grouping; persistence and terminal adapters must not interpret section prefixes
or select configuration maps themselves. Its `Target` interface hides the
global settings file and sparse local `wago.json` adapter from command and TUI
callers. Runtime precedence is built-in/environment defaults, global settings,
local overrides, then explicit command flags.

Feature and optimization surfaces are registration-driven:

- register a WebAssembly feature once in `src/wago/config.go`'s
  `featureRegistry`, alongside its runtime bit, label, description, and
  experimental status;
- register an Optimization Definition once in
  `src/core/compiler/optimization/catalog.go`, then provide its Optimization
  Binding in every supported backend's `knobs.go`. Backend initialization
  rejects missing, duplicate, unknown, and nil bindings.

The config TUI, `config list` JSON, setting lookup and validation, `run`/`build`
flags, and cross-target `compile` flags derive from those registrations. Marking
an entry experimental keeps it out of the stable selectors and requires
`--experimental` for non-interactive mutation. `go generate ./...` derives the
feature and optimization name enums in `schema.json`; `schema_test.go` rejects a
stale artifact, so registration cannot silently outrun editor validation.

An Optimization Selection is immutable runtime compilation configuration.
`RuntimeConfig.WithOptimization` and `WithOptimizations` return copies; run,
build, standalone execution, and artifact-cache identity consume that selection
instead of mutating process-global knobs. Railshot currently adapts a selection
to its legacy booleans under one compile-scoped lock. Those booleans are an
implementation detail and can move into compiler-owned state without changing
callers.

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

Command descriptors keep ordinary options in `Flags` and backend optimization
switches in `Knobs`. Parsing treats both identically, while help and command
schemas always place `Knobs` last, after plugin, automation, and help flags.

`wago config` opens the shared selector UI. Stable WebAssembly features and
compiler optimizations are toggleable, runtime parallelism and deferred bounds
checking have editable defaults, and registered experiments appear in the
experimental preview. Every mutation also has a non-interactive `list`, `get`,
`set`, or `reset` form; experimental `set`, `enable`, `disable`, and targeted
`reset` operations require `--experimental`.

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
adapter. One manager-owned transaction resolves the graph, reviews Authorities,
stages downloads and builds, validates the linked plan, and atomically publishes
the manifest, lockfile, and artifact. Command packages do not coordinate those
steps.

Runtime Installation bootstrap policy lives in `internal/installbootstrap`.
It owns release-channel selection, target asset naming, and checksum validation.
The Unix and Windows scripts remain native adapters because no Wago executable
exists at their seam yet; semantic bootstrap tests keep those adapters aligned
with the same release contract. The downloaded native installer uses the module
directly for manager selection and verification.

All interactive manager workflows also expose flags for automation. A command
without enough information may open a selector, but CI and scripts can always
provide the version, profile, build, scope, confirmation, and Authority policy
directly. Bare `wago update` opens an all-selected multi-choice screen for the
manager, rolling runtime, and plugins. Positional targets and matching flags
provide one-shot updates; the narrower `self update`, `version update`, and
`plugin update` commands remain available for explicit control.

Shared automation policy lives in `cli/internal/automation`. The manager parses
global policy once and forwards it to a runtime through `WAGO_*` environment
variables, so a manager-to-runtime handoff preserves `--json`, `--no-input`,
`--dry-run`, `--locked`, and `--offline`. Command descriptors explicitly opt in
to JSON output and dry-run planning; unsupported combinations fail instead of
silently falling back to human output or performing a mutation.

`wago commands --json` and `wago help --json` serialize the command tree from
the same descriptors used for parsing and help. When a runtime is active, the
manager merges that binary's schema so profile-specific commands and backend
knobs stay exact; an installation-free fallback describes the stable runtime
surface. Keep this schema additive and machine-readable: arguments remain
syntax strings, flags include their type and aliases, and nested commands retain
the public command hierarchy. Errors use exit 2 for invalid invocation and exit
1 for operational failure.

## Runtime profiles and builds

Standard runtimes expose `run`, `build`, `validate`, `module`, and plugin
inspection. Minimal runtimes expose only `run`. A plugin-aware runtime is a
standard runtime build whose generated entrypoint imports plugin registration
packages; it does not become a third CLI role.

`run --watch` is runtime-owned. It relaunches the same runtime executable when
the input module changes, preserving guest arguments and plugin selection made
by the manager handoff. The runtime supervises one child process tree at a time,
keeps file polling active while the guest runs, and waits for stable content
before a restart. On Linux and macOS, a terminal-backed watcher and its direct
guest stay in the shell's foreground job group, so both receive terminal
interrupts. The watcher restores terminal ownership if a guest descendant
creates a separate foreground group. It also records bounded process identities
and checks each identity again before signaling it. Linux subreaper ownership
and macOS kernel fork events keep double-forked children tracked. On macOS, a
trusted shell trampoline waits on an inherited pipe until event tracking is
active, then replaces itself with the runtime in the same process. Cleanup
therefore reaches descendants that create a new session or process group. On
Windows, the extended process-start API puts the child in its kill-on-close job
before its first instruction runs. The watcher mirrors terminal stop and
continue events, and its status output remains safe when background terminal
writes are disabled. It can find the controlling terminal through stdin,
stdout, or stderr, including after a background job is foregrounded. Hangup,
interrupt, quit, and termination signals stop the child tree before the watcher
exits. Cheap file identity, size, modification, and change metadata gates full
content hashing. A full scan after 25 poll intervals is the bound when a file
system does not update that metadata. The hash still detects same-size rewrites
when modification timestamps do not change. The size-first `wago_lean` profile
does not include watch mode; its parser rejects `--watch`.

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

The manager and standard-runtime command-surface tests enumerate every leaf,
render every help page, and require completion coverage for every declared
flag. Update those inventories whenever a command is intentionally added,
removed, or moved across the manager/runtime boundary.
