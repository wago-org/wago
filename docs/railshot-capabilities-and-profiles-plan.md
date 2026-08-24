# Railshot build capabilities and optimization profiles

Status: architecture and migration plan

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
> immutable profile bits. Individual toggles exist only in development builds.
> Safety invariants are not flags.**

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

The remaining problems are ownership and product surface. The public catalog,
runtime maps, snapshots, environment controls, and package-global backend
booleans still define or mirror dozens of individual choices. Large systems are
always compiled in even when a product never uses them. This plan preserves the
existing immutable hot path while separating build availability, normal profile
policy, and laboratory controls.

## Policy representation

Use separate fixed-width masks for compiled capabilities and cheap lowering
decisions:

```go
type railshotCapabilities uint64

const (
	capNativeCompaction railshotCapabilities = 1 << iota
	capProducerNeedles
	capNativeGCOptimizations
	capCodegenPlugins
	capDiagnostics
)

type railshotOptions uint64

const (
	optInline railshotOptions = 1 << iota
	optLoopPrecheck
	optAssociativeTrees
	optNativeCompaction
	optProducerNeedles
	optNativeGC
	optModuleTrapSharing
	// Core code-quality decisions continue here.
)
```

Every module and function compilation receives one resolved value:

```go
type CodegenPolicy struct {
	Objective    OptimizationObjective
	Capabilities railshotCapabilities
	Options      railshotOptions
	CPUFeatures  CPUFeatures

	FunctionAlignLog2 uint8
	LoopAlignLog2     uint8
}
```

The function hot path remains a bit test:

```go
func (f *fn) enabled(option railshotOptions) bool {
	return f.policy.Options&option != 0
}
```

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
| `wago_codegen_plugins` | Allocator-integrated native custom-instruction and custom-value lowering |
| `wago_codegen_debug` | Stats, explain mode, per-option overrides, kill switches, validation modes, and experimental thresholds |
| `wago_railshot_full` | Convenience umbrella for all optional **production** Railshot capabilities |

`wago_codegen_debug` stays independent of `wago_railshot_full`; a full production
compiler should not accidentally include the optimizer laboratory.

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

Positive tags naturally make the untagged build lean. That is an intentional
product change, not a mechanical refactor. During migration, preserve current
untagged behavior until tagged and untagged distributions, binary-size savings,
performance, and downstream compatibility have been measured. Change the
untagged product only at an explicit release boundary; do not let file movement
silently redefine the default build.

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

Associative tree covering is allowed only when the local byte-cost model proves
it positive.

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
| Associative trees | Off | On | Byte-positive only | Off |
| Loop prechecks | Off | On | Off | Off |
| Native compaction | Off | Off | If available | If available |
| Producer needles | Off | If available | Off | Off by default |
| Native GC optimizer | Off by default | If available | Off by default | Off by default |
| Function workers | Existing default | Existing default | Existing default | Serial |
| Individual overrides | Debug build only | Debug build only | Debug build only | Debug build only |

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
thresholds have also regressed execution. Keep it on for Speed, byte-cost it for
Size, and leave it off for Balanced and Embedded.

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
untagged lean build:
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

Put allocator-integrated native custom-instruction support behind
`wago_codegen_plugins`:

- custom machine types and multi-register values;
- YMM/ZMM and vector-register ownership;
- plugin register allocation and reservation;
- opaque machine fragments and encoder access;
- plugin memory checking;
- custom output state; and
- plugin-aware finalizer exclusions.

The current design directly participates in Railshot register ownership and raw
machine encoding. Compiling that integration out meaningfully simplifies the
backend for products that do not need native codegen extensions. Registration
and handler-backed ordinary imports may remain in the core API; a custom type or
instruction that requires native lowering receives a clear unavailable-
capability error.

### Diagnostics and optimizer laboratory

Put these facilities behind `wago_codegen_debug`:

- `CodegenStats` and structured native-byte attribution;
- `WAGO_EXPLAIN` reports;
- peephole maps and candidate counters;
- per-option environment variables and kill switches;
- legacy algorithm selection;
- inliner and finalizer reports;
- finalizer validation modes; and
- experimental thresholds and fuel overrides.

The release build should compile calls out where practical. Where shared code
would become unreadable, use zero-sized no-op implementations that inline away.
Environment variables must be parsed once while resolving policy, never from a
lowering hot path.

## Experiment-only overrides

Under `wago_codegen_debug`, retain stable fine-grained names for tests,
benchmarks, and bisection, but parse them once into the immutable option mask.
Prefer one grammar over dozens of environment variables:

```bash
WAGO_RAILSHOT_OPTS="+assoc-tree,-inline,+loop-precheck"
```

The equivalent CLI surface is diagnostic-only:

```bash
wago run \
  --codegen-profile balanced \
  --codegen-opt +assoc-tree \
  --codegen-opt -inline
```

```go
func parseExperimentOptions(base railshotOptions, args []string) (railshotOptions, error)
```

The existing catalog can supply generated experiment metadata, help text, and
test inventories. It should no longer own production compiler state or require
`RuntimeConfig` to expose every individual optimization.

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

CPU features are target capabilities, not preferences:

```go
if policy.CPUFeatures.Has(CPUBMI2) {
	emitRORX(...)
}
```

An experiment may force a generic legal lowering. It may never force an
instruction the target cannot execute.

## Revised treatment of removal candidates

| Earlier direction | Revised treatment |
|---|---|
| Delete the optimization catalog | Keep metadata under debug; production consumes typed masks |
| Delete the finalizer | Build-tag it |
| Delete stats and explain mode | Build-tag them |
| Delete associative trees | Select through Speed and byte-positive Size policy |
| Delete producer needles | Put them in the optional needle build |
| Delete GC optimization | Put native GC optimization in its own capability |
| Delete plugin lowering | Put native plugin codegen in its own capability |
| Delete experimental pin allocators | Keep only in the debug build until promoted or removed with evidence |
| Delete the ARM64 beachhead | Still delete; it is historical throwaway code |
| Delete stale ARM64 `_port` material | Move useful historical contracts to `docs/archive`, then delete the code-tree copy |

Default-off is not a permanent cemetery. Every retained option needs an owner,
an evidence-backed profile, or an experiment purpose. After a deprecation window,
code that has no supported profile and no continuing experimental value can be
deleted in a separate measured change.

## Artifact identity and provenance

Every codegen-affecting decision must participate in automatic artifact-cache
identity:

```text
cache identity =
    wasm/source hash
  + compiler/runtime build identity
  + target GOOS/GOARCH
  + effective CPU feature mask used by codegen
  + optimization objective
  + resolved options mask
  + compiled capability mask
  + bounds mode
  + other existing codegen-affecting feature configuration
```

The current cache already covers most of this through build identity and
explicit objective/option/configuration fields. Add explicit capability and
effective CPU masks so custom test identities, provenance tools, and future
dispatch policy cannot accidentally hide a distinction behind the build hash.
Continue storing required CPU features in the artifact and rejecting an
incompatible host at load time.

Record the resolved selection as build provenance:

```go
type CompilerProvenance struct {
	Objective    OptimizationObjective
	Options      uint64
	Capabilities uint64
	CPUFeatures  uint64
	Target       string
}
```

Provenance does not control execution after native code is loaded. It exists for
reproducibility, artifact inspection, and bug reports. The loader remains
authoritative for format, target, CPU, and semantic compatibility.

## Migration order

1. Preserve the existing immutable `optimization.Selection` and
   `CodegenPolicy` baseline while adding tests that forbid hot-path maps and
   optimizer-global mutation.
2. Classify every current catalog entry as safety invariant, CPU feature,
   profile option, build capability, or debug-only experiment.
3. Introduce `railshotCapabilities` and generated availability stubs without
   changing emitted code.
4. Replace the production selection with typed `railshotOptions`; keep the
   catalog as an adapter for existing configuration during migration.
5. Define explicit Balanced, Speed, Size, and Embedded masks and test every
   profile on both architectures.
6. Move individual override parsing, catalog enumeration, environment kill
   switches, and stats/explain controls behind `wago_codegen_debug`.
7. Put finalization and physical compaction behind
   `wago_railshot_compact`/`wago_railshot_full`.
8. Put exact producer needles behind
   `wago_railshot_needles`/`wago_railshot_full`.
9. Split helper-correct WasmGC lowering from `wago_railshot_gcopt` native
   optimization.
10. Split handler-backed plugin imports from `wago_codegen_plugins` native
    allocator integration.
11. Add explicit capability/CPU fingerprints and compiler provenance to
    artifact/cache tests and inspection.
12. Measure the tagged product matrix, then make the untagged-default change at
    an explicit release boundary if it meets the gates below.

## Acceptance gates

### Correctness

- Default, each individual production capability, `wago_railshot_full`, and
  `wago_railshot_full,wago_codegen_debug` pass focused and full tests.
- AMD64 and ARM64 cross-builds cover complementary enabled/disabled files.
- Lean WasmGC passes the same semantic, root-map, safepoint, barrier, trap, and
  exception tests as the full GC optimizer build.
- A missing explicitly requested capability returns a stable validation error.
- Full builds preserve pre-migration code output for equivalent policy and CPU
  selection unless a separately reviewed codegen change explains the delta.

### Footprint and compile cost

- Record stripped binary and package archive size for every capability alone,
  the lean build, and the full build; do not claim simplification without
  measured byte savings.
- Re-run compile latency, B/op, allocs/op, peak RSS, and native-byte ledgers with
  serialized timing and exact commits.
- A disabled capability must not retain its metadata buffers, pre-scans, maps,
  counters, or reusable scratch in the compilation path.

### Execution and artifacts

- Compare equivalent profile masks before and after the split on the broad
  corpus and the focused workloads that justified each subsystem.
- Test that objective, resolved options, compiled capabilities, effective CPU
  features, bounds mode, target, source, and build identity all separate cache
  entries when they can change emitted code.
- Verify that serialized provenance round-trips and that loaders reject
  incompatible required CPU features and artifact formats.

### API and migration

- Keep the public compilation API stable while compatibility adapters exist.
- Document which tags official binaries use and whether the untagged library is
  lean or full for each release.
- Remove fine-grained production configuration only with a deprecation path;
  debug builds retain stable names for benchmark reproduction.

## Non-goals

- No SSA, whole-function IR, reconstructed CFG, or second semantic pass.
- No tolerance for malformed Wasm or structured custom sections.
- No flag may weaken traps, bounds checks, roots, safepoints, barriers, ABI, or
  CPU legality.
- No claim that all optional code should remain forever. Flags create a measured
  deprecation window; they do not replace evidence-based deletion.
