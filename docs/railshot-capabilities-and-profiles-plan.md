# Railshot build capabilities and optimization profiles

Status: implemented, with measured scope reductions noted below

Source baseline: `main` at `4799c654`

Measurement basis: the checked-in
[optimization toggle matrix](optimization-toggle-matrix-2026-08-22.md),
[native-code size results](generated-native-code-size-results.md), and the
[focused](dew-map-set-wasmgc-performance-2026-08.md)
[WasmGC reports](wasmgc-v8-cranelift-research-2026-08.md). This document
proposes packaging and policy boundaries; it does not claim new measurements.

## Decision

Use **flag first, delete later** for substantial Railshot subsystems. Retain
implemented work while giving embedders a smaller compiler that is easier to
audit. Do not achieve that by adding more public booleans.

Railshot should expose three distinct layers:

1. **Build capabilities** compile large optional subsystems out of the binary.
2. **Optimization profiles** select coherent runtime groups.
3. **Experiment-only option overrides** support benchmarking and bisection in
   diagnostic builds.

Safety and semantic invariants are outside all three layers.

The governing rule is:

> **Large optional systems use build flags. Cheap code-quality decisions use
> immutable profile bits. Individual toggles remain compatibility and experiment
> controls, not the normal production interface.
> Safety invariants are not flags.**

## Implementation outcome

The implementation kept the pieces that created a real product boundary:

- `wago_lean` now omits native compaction, producer needles, and AMD64 native
  WasmGC optimization. Each subsystem can be restored independently, or all can
  be restored with `wago_railshot_full`.
- the ordinary untagged build preserves the pre-migration full compiler;
- `CodegenPolicy` carries an immutable compiled-capability mask alongside the
  existing immutable `optimization.Selection`;
- public Balanced, Speed, Size, and Embedded objectives resolve coherent option
  groups, and explicit compatibility overrides win after profile selection;
- unavailable explicitly requested options fail validation with the required
  build tag; and
- the superseded ARM64 beachhead was deleted, while the useful `_port` notes
  moved to `docs/archive`.

Two proposed splits were rejected after implementation experiments. Native
codegen plugins changed the stripped lean binary by only 320 bytes because Go's
linker already removes unused plugin lowering. A diagnostics build split cut
across the measurement and code-neutrality tests that justify optimizer work and
did not provide a clean independent subsystem. Both remain demand-linked or
runtime opt-in instead of gaining build tags.

Stripped CLI measurements from the implementation worktree used Go 1.26.5,
`CGO_ENABLED=0`, `-ldflags '-s -w'`, and the
`wago_runtime,wago_lean,wago_minimal` base tags. ARM64 was linked on the Darwin
host; AMD64 was cross-linked for Linux:

| Build | ARM64 bytes | AMD64 bytes |
|---|---:|---:|
| Lean | 7,411,714 | 8,024,226 |
| + compact | 7,444,850 | 8,065,186 |
| + needles | 7,444,738 | 8,052,898 |
| + GC optimizer | no ARM64 subsystem | 8,089,762 |
| Full | 7,494,386 | 8,155,298 |

The full-minus-lean reduction is 82,672 bytes on ARM64 and 131,072 bytes on
AMD64. Individual deltas are not strictly additive because linked text and
alignment interact. Unstripped symbol checks found no `finalizeNativeCode`,
`buildCompactionPlan`, SWAR/SIMD needle entry, or dead-GC-constructor entry
symbols in either lean target.

## Current baseline

The migration does not start from the old global-map design. Current Railshot
already has useful pieces of the target architecture:

- [`shared.CodegenPolicy`](../src/core/compiler/backend/railshot/shared/policy.go)
  is immutable per compilation and already carries the objective, layout
  budgets, and an immutable `optimization.Selection`.
- [`optimization.Selection`](../src/core/compiler/optimization/catalog.go) is
  already a 64-bit architecture-specific set, and hot paths use pre-resolved
  `optimization.Option` tokens rather than string or map lookup.
- Speed, Balanced, Size, and Embedded objectives already exist. Size and
  Embedded own physical compaction and compact layout choices.
- The [automatic artifact cache](../cli/runtime/internal/artifactcache/cache.go)
  already includes the runtime build identity, target, Wasm features, bounds
  policy, optimization objective, and selected option bits.

The implementation preserves the existing immutable hot path while separating
measured build availability from normal profile policy and compatibility
controls. Package-global bindings remain as the catalog's legacy/default input;
they are resolved before compilation and are not consulted by a valid function
policy.

## Policy representation

The implementation reuses `optimization.Selection` as the options word instead
of introducing a duplicate `railshotOptions` type. `Selection` was already an
immutable `uint64` with pre-resolved target-specific `Option` tokens, so another
mask would have added conversion and synchronization risk without improving the
hot path. `shared.Capabilities` is a separate `uint64` because build availability
has different validation semantics from runtime selection.

Every module and function compilation receives one `CodegenPolicy` containing
the objective, compiled capabilities, immutable selection, and bounded layout
budgets. Hot decisions remain pointer validation plus one bit test.

After policy resolution, lowering and finalization must not perform a map lookup,
acquire the catalog lock, inspect a snapshot revision, mutate an optimizer global,
or repeatedly read an environment variable. Parse configuration once, validate
it once, and pass the resolved policy through the existing module and `fn` state.

The current 64-option limit is acceptable while the production mask contains
only core code-quality decisions. Keep a compile-time inventory check. If the
core set outgrows one word, split it into typed fixed-width groups rather than
returning to strings or maps on the hot path.

## Capability resolution

Build availability and runtime selection have different semantics:

- A profile enables only the options available in the current build. For
  example, Speed may use native GC optimization when it is compiled in without
  making the entire Speed profile invalid in a lean build.
- An explicit request for an unavailable subsystem must fail validation with
  the missing build tag named. It must never be silently ignored.
- A module that requires native custom-instruction lowering must fail clearly
  when plugin codegen is unavailable. Handler-backed ordinary imports may still
  use their portable runtime path.
- Safety-preserving generic fallbacks are part of the core build. Compiling out
  an optimizer must not compile out the feature's correct implementation.

This makes the resolved option mask a function of the selected profile,
available capabilities, target CPU features, and diagnostic overrides:

```text
resolved options = profile(objective)
                 intersect options available in this build
                 intersect options legal on this CPU
                 then apply validated debug-only overrides
```

## Build tags

Introduce these coarse positive tags:

| Build tag | Included subsystem |
|---|---|
| `wago_railshot_compact` | Native finalization, branch relaxation, frame shrinking, local-slot packing, and size-oriented trap sharing |
| `wago_railshot_needles` | Producer-specific SWAR/SIMD sequence recognition and bounded bytecode lookahead |
| `wago_railshot_gcopt` | GC facts, native scalar access/allocation, dead-constructor optimization, resolver reuse, and specialized barriers |
| `wago_railshot_full` | Convenience umbrella for all optional Railshot capabilities |

Each capability uses complementary files so internal and public APIs remain
stable:

```go
// compact_enabled.go
//go:build wago_railshot_compact || wago_railshot_full

const nativeCompactionAvailable = true
```

```go
// compact_disabled.go
//go:build !wago_railshot_compact && !wago_railshot_full

const nativeCompactionAvailable = false
```

The existing `wago_lean` product selects the disabled side of each complementary
file. Untagged builds remain full for compatibility. Positive tags restore one
subsystem to a lean build; `wago_railshot_full` restores all three. This keeps the
default library behavior stable while making the existing lean/minimal runtime
meaningfully smaller.

## Profiles

The existing `OptimizationObjective` is the normal selection surface. It should
resolve to profile masks rather than expose another large production API.

### Balanced

Balanced remains the normal default and keeps broadly useful, bounded core
lowering:

- bounds and value facts;
- flags results and branch folding;
- register merging and local sinks;
- entry argument and high-value vector pins;
- register ABI;
- basic call-free inlining;
- store forwarding;
- immutable-table specialization;
- generic SIMD/V128 lowering; and
- frame elision.

`loop-precheck` is deliberately not in Balanced. The checked-in matrix found
only small aggregate execution movement while disabling it improved compile
time and allocation bytes materially on both architectures. It remains
available to Speed and experiments because focused workloads still benefit.

### Speed

Speed is Balanced plus measured speed-oriented work:

- associative tree covering;
- loop prechecks;
- larger pin pools;
- more aggressive bounded inlining;
- native GC optimization, when compiled in;
- producer needles, when compiled in; and
- less compact alignment and layout decisions.

### Size

Size is Balanced plus byte-positive decisions. When
`wago_railshot_compact` or `wago_railshot_full` is available, it enables:

- native finalization and physical hole deletion;
- short branches and compact immediates;
- frame-adjustment shrinking;
- local-slot packing;
- shared trap bodies and byte-identical adapters;
- packed function alignment; and
- strict size-positive inlining with dead standalone-body removal.

Associative tree covering stays off in Size until it has a local byte-cost model;
the current register-pressure threshold is not itself proof of a byte saving.

### Embedded

Embedded starts from Size and additionally selects deployment policy for low
peak memory:

- serial function validation and compilation;
- lower bounded optimizer fuel;
- no code duplication unless proven to shrink the artifact;
- smaller reusable worker scratch; and
- no native GC optimizer or producer needles unless the build and caller opt
  into those capabilities.

Worker scheduling does not affect emitted code and therefore remains outside
artifact identity even when the Embedded convenience path defaults it to one.

### Profile summary

| Decision | Balanced | Speed | Size | Embedded |
|---|---:|---:|---:|---:|
| Associative trees | Off | On | Off pending byte-cost proof | Off |
| Loop prechecks | Off | On | Off | Off |
| Native compaction | Off | Off | If available | If available |
| Producer needles | Off | If available | Off | Off by default |
| Native GC optimizer | Off | If available | Off | Off |
| Function workers | Existing default | Existing default | Existing default | Serial |
| Individual overrides | Compatibility/experiments | Compatibility/experiments | Compatibility/experiments | Compatibility/experiments |

## Capability boundaries

### Native finalization and code compaction

Put the machinery that exists only to reclaim or account for emitted bytes
behind `wago_railshot_compact`:

- AMD64 and ARM64 `finalize.go` implementations;
- bounded offset maps and finalizer metadata;
- rel32 inventories and short-branch solvers;
- dead-hole deletion;
- physical frame shrinking;
- local-slot packing and jump-table relaxation;
- loop compaction; and
- size-specific trap-body and adapter sharing.

The disabled implementation emits directly, performs normal relocation, and
records none of the optional finalizer metadata. It must preserve all metadata
needed for traps, safepoints, calls, and debugging.

The [native-code size results](generated-native-code-size-results.md) show why
this is a capability rather than a deletion candidate: physical compaction can
remove substantial native code on admitted workloads, generally costs compile
time, and has little measured execution effect.

### Associative tree covering

Do not delete the AMD64 tree cover. Select it through the profile:

```go
if f.enabled(optAssociativeTrees) {
	if reg := f.tryAssociativeTree(node, dest); reg != regNone {
		return reg
	}
}
```

It has proven native-size and spill reductions on focused shapes, but the broad
matrix does not establish a large aggregate execution win. More aggressive
thresholds have also regressed execution. Keep it on for Speed and leave it off
for Balanced, Size, and Embedded until Size has a real byte-cost gate.

### Producer-specific SWAR and SIMD needles

Put exact producer-shape recognition behind `wago_railshot_needles`:

- AssemblyScript `swar-widen4` and `swar-pack4`;
- the `xjb-as`/AssemblyScript unsigned high-multiply expansion;
- producer-specific bytecode lookahead; and
- narrow SIMD superinstruction patterns coupled to emitted sequences.

Keep generic scalar/SIMD lowering, V128 pins and direct results, mask tests,
memory folding, and ordinary target instruction selection in the core backend.

The source explicitly describes these recognizers as exact `utf-as`,
AssemblyScript, `json-as`, or `xjb-as` shapes. That coupling is useful, measured
work, but it should not set the minimum compiler footprint for every embedder.

The resulting products are:

```text
`wago_lean` build:
  generic scalar, SIMD, and SWAR codegen

-tags wago_railshot_needles:
  generic codegen + producer-specific recognition

-tags wago_railshot_full:
  all optional production subsystems
```

### WasmGC optimizer

Split WasmGC correctness from native optimization.

Always retain when WasmGC is supported:

- generic helper lowering;
- exact root maps and root initialization;
- safepoints and exception-root handling;
- correct collector interaction and barriers;
- generic struct, array, and i31 behavior; and
- all validation and trap semantics.

Put behind `wago_railshot_gcopt`:

- structured GC reference facts and exact/non-null folding;
- native scalar struct and array access;
- native constructor allocation;
- recursive dead-constructor optimization;
- resolver reuse and shared resolver stubs;
- advanced barrier specialization; and
- GC-object store/load forwarding.

These are substantial systems rather than small peepholes. A lean GC build
remains correct and helper-heavy. A full build retains the current optimized
paths. Existing GC-specific measurements remain the acceptance baseline; the
non-GC toggle matrix is not sufficient evidence for changing GC profile
defaults by itself.

### Native codegen plugins

No build capability was retained. A prototype gate removed only 320 bytes from
the stripped ARM64 lean/minimal executable (1,104 bytes unstripped). Native
plugin implementations are demand-linked through registered extensions, so the
Go linker already provides nearly all of the proposed footprint benefit. The
extra validation modes and test matrix were not justified by that result.

### Diagnostics and optimizer laboratory

No diagnostics build capability was retained. `CodegenStats` is the shared
measurement substrate for code-neutrality, byte attribution, focused optimizer
tests, and regression analysis. Splitting it made ordinary lean test coverage
depend on a debug tag without establishing a useful standalone binary reduction.
Collection remains runtime opt-in (`Stats`, telemetry, or `WAGO_EXPLAIN`), and
nil sinks retain the existing no-op hot path.

## Experiment-only overrides

Stable fine-grained names remain available through the existing compatibility
API and environment controls. They are resolved once into the immutable
selection before compilation; profiles are applied first and explicit overrides
win. Consolidating the environment grammar and deprecating public per-option
configuration are separate API changes and were not required to create the
measured build boundaries.

## Decisions that are not optional

Do not model these as ordinary optimization options:

- exact GC root publication and safepoint correctness;
- exception-root handling;
- trap correctness and trap ordering;
- stack overflow protection;
- correct linear-memory bounds behavior;
- ABI preservation;
- memory-model and atomic semantics;
- CPU instruction legality; and
- serialized-artifact validation.

Current `stack-fence` and `stack-reg` entries are runtime/architecture modes,
not peer instruction-selection experiments. Move them out of the optimization
catalog only after their supported modes have dedicated validation and artifact
identity.

CPU features remain target capabilities, not preferences. Existing host checks,
option admission, emitted-code required-feature fields, and loader validation
continue to own that boundary; duplicating it in `CodegenPolicy` was unnecessary.

An experiment may force a generic legal lowering. It may never force an
instruction the target cannot execute.

## Revised treatment of removal candidates

| Earlier direction | Revised treatment |
|---|---|
| Delete the optimization catalog | Keep it as metadata and compatibility input; production consumes its immutable selection |
| Delete the finalizer | Compile its work out of `wago_lean`; restore with the compact/full tags |
| Delete stats and explain mode | Keep runtime opt-in; a build split was not worthwhile |
| Delete associative trees | Select through Speed; defer Size until a byte-cost gate exists |
| Delete producer needles | Put them in the optional needle build |
| Delete GC optimization | Put native GC optimization in its own capability |
| Delete plugin lowering | Keep demand-linked; an explicit build gate saved only 320 stripped bytes |
| Delete experimental pin allocators | Keep as explicit experiments until promoted or removed with evidence |
| Delete the ARM64 beachhead | Deleted as historical throwaway code |
| Delete stale ARM64 `_port` material | Useful contracts moved to `docs/archive`; code-tree copies removed |

Default-off is not a permanent cemetery. Every retained option needs an owner,
an evidence-backed profile, or an experiment purpose. After a deprecation window,
code that has no supported profile and no continuing experimental value can be
deleted in a separate measured change.

## Artifact identity and provenance

Every codegen-affecting decision participates in automatic artifact-cache
identity:

```text
cache identity =
    wasm/source hash
  + compiler/runtime build identity
  + target GOOS/GOARCH
  + optimization objective
  + resolved options mask
  + bounds mode
  + other existing codegen-affecting feature configuration
```

No additional cache fields were added. The Go build identity already includes
build tags, so the compiled capability mask would duplicate it. The effective
profile is already represented by objective plus option bits. CPU-dependent
codegen choices are represented by those bits and the artifact's existing
required-feature fields, which the loader validates. Adding a second serialized
provenance structure would create another identity source without a current
consumer.

## Implemented sequence

1. Preserved immutable `optimization.Selection` and added immutable compiled
   capabilities to `CodegenPolicy`.
2. Added complementary availability files for compaction, needles, and native
   GC optimization.
3. Added capability-aware defaults, profiles, explicit-override precedence, and
   unavailable-option validation.
4. Made disabled paths constant-fold before finalizer metadata, producer
   recognizers, or native GC optimizer work is retained.
5. Kept the untagged build full and made `wago_lean` the explicit lean selector.
6. Rejected plugin, diagnostics, duplicate options-mask, cache-fingerprint, and
   provenance additions that did not justify their extra state.
7. Removed historical ARM64 beachhead code and archived useful port notes.

## Acceptance gates

### Correctness

- Default, each individual production capability, and `wago_railshot_full` pass
  focused tests.
- AMD64 and ARM64 cross-builds cover complementary enabled/disabled files.
- Lean WasmGC passes the same semantic, root-map, safepoint, barrier, trap, and
  exception tests as the full GC optimizer build.
- A missing explicitly requested capability returns a stable validation error.
- Full builds preserve pre-migration code output for equivalent policy and CPU
  selection unless a separately reviewed codegen change explains the delta.

### Footprint and compile cost

- Record stripped binary size for every capability alone, the lean build, and
  the full build; do not claim simplification without measured byte savings.
- Re-run compile latency, B/op, allocs/op, peak RSS, and native-byte ledgers with
  serialized timing and exact commits.
- A disabled capability must not retain its metadata buffers, pre-scans, maps,
  counters, or reusable scratch in the compilation path.

### Execution and artifacts

- Compare equivalent profile masks before and after the split on the broad
  corpus and the focused workloads that justified each subsystem.
- Test that objective, resolved options, bounds mode, target, source, and build
  identity separate cache entries. Build identity contains capability tags;
  loaders continue rejecting incompatible required CPU features and formats.

### API and migration

- Keep the public compilation API stable while compatibility adapters exist.
- Document which tags official binaries use and whether the untagged library is
  lean or full for each release.
- Remove fine-grained production configuration only with a deprecation path;
  compatibility controls retain stable names for benchmark reproduction.

## Non-goals

- No SSA, whole-function IR, reconstructed CFG, or second semantic pass.
- No tolerance for malformed Wasm or structured custom sections.
- No flag may weaken traps, bounds checks, roots, safepoints, barriers, ABI, or
  CPU legality.
- No claim that all optional code should remain forever. Flags create a measured
  deprecation window; they do not replace evidence-based deletion.
