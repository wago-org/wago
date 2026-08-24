# Railshot build capabilities

Status: implemented

Source baseline: `main` at `4799c654`

## Decision

Use **flag first, delete later** for substantial Railshot subsystems. There is
one ordinary performance-oriented compiler policy: no `-O1`, `-O2`, `-O3`,
Speed, Balanced, Size, or Embedded modes, and no public optimization-objective
API.

The remaining controls have two purposes:

1. **Build capabilities** compile large optional subsystems out of lean binaries.
2. **Experiment-only option overrides** support measurement and bisection.

Safety and semantic invariants are never optional. The governing rule is:

> **Large optional systems use build flags. Cheap code-quality decisions use
> immutable bits. Individual toggles are laboratory controls. Safety invariants
> are not flags.**

## Build capabilities

The normal untagged build contains the full compiler. `wago_lean` omits three
large optional subsystems; a positive tag restores one subsystem, and
`wago_railshot_full` restores all of them.

| Build tag | Included subsystem |
|---|---|
| `wago_railshot_compact` | Native finalization, branch relaxation, frame shrinking, local-slot packing, and compact trap/adapter sharing |
| `wago_railshot_needles` | Producer-specific SWAR/SIMD recognition and bounded bytecode lookahead |
| `wago_railshot_gcopt` | AMD64 GC facts, native scalar access/allocation, dead-constructor optimization, resolver reuse, and specialized barriers |
| `wago_railshot_full` | Convenience umbrella for every optional Railshot capability |

Complementary build-tagged files make availability explicit. The resolved
capability mask is immutable and travels in `shared.CodegenPolicy` beside the
existing `optimization.Selection` bitset. Hot lowering performs a bit test; it
does not read a map, acquire a catalog lock, inspect a snapshot revision, or
mutate package-global compiler state.

An explicit request for an unavailable optimization fails validation and names
the build tag that supplies it. Correct generic lowering remains available when
an optimizer is absent.

## One compilation policy

Ordinary compilation starts from the optimization catalog defaults, intersects
them with the capabilities compiled into the binary, and applies validated
individual overrides. This preserves the performance-oriented behavior that
preceded the capability split.

There are no optimization profiles and no objective in `RuntimeConfig`, backend
compile options, serialized provenance, or artifact-cache identity. Removing
that API is intentionally breaking. The artifact-cache key format is bumped so
old and objective-free key layouts cannot collide.

Native compaction remains an internal `CompileOptions.CompactNative` path for
benchmarks, byte accounting, and rollout checks. It is not a public runtime
mode. `WAGO_COMPACT=1` remains the force-on measurement oracle and
`WAGO_COMPACT=0` the global rollback oracle; neither recreates an optimization
profile.

CPU instruction legality remains separate from optimizer choice and is selected
from the target feature mask.

## Experiment controls

The existing individually named optimization metadata remains useful for tests,
benchmark bisection, and environment kill switches. It resolves once into the
immutable selection bitset before compilation. It is not the normal public
configuration surface and does not own compiler state.

Diagnostics, native codegen plugins, and custom instructions remain
demand-linked rather than receiving build tags. The implementation experiments
found only a 320-byte stripped-binary change from splitting plugin lowering,
while a diagnostics split cut across the measurement and code-neutrality tests
that justify optimizer changes.

## What remains mandatory

These are invariants, not optimization switches:

- exact GC root publication and safepoints;
- trap and stack-overflow correctness;
- correct bounds behavior and memory semantics;
- ABI preservation and CPU instruction legality;
- artifact validation; and
- exception-root handling.

Generic WasmGC helper lowering, roots, safepoints, collector interaction, and
correct struct/array/i31 behavior remain in lean builds. Only the native WasmGC
optimizer is optional.

## Measured footprint

Stripped CLI measurements from the implementation worktree used Go 1.26.5,
`CGO_ENABLED=0`, `-ldflags '-s -w'`, and the
`wago_runtime,wago_lean,wago_minimal` base tags. ARM64 was linked on Darwin;
AMD64 was cross-linked for Linux.

| Build | ARM64 bytes | AMD64 bytes |
|---|---:|---:|
| Lean | 7,395,138 | 8,016,034 |
| + compact | 7,444,786 | 8,056,994 |
| + needles | 7,444,674 | 8,040,610 |
| + GC optimizer | 7,395,138 (no ARM64 subsystem) | 8,081,570 |
| Full | 7,477,810 | 8,147,106 |

Full minus lean is 82,672 bytes on ARM64 and 131,072 bytes on AMD64. Individual
deltas are not additive because linked text and alignment interact. Unstripped
symbol checks found no native finalizer, compaction planner, producer-needle, or
dead-GC-constructor entry symbols in the corresponding lean targets.

Compared with the immediately preceding objective-bearing implementation, the
breaking removal reduces the stripped lean and full CLIs by 16,576 bytes on
ARM64 and 8,192 bytes on AMD64. Across the individually restored variants, the
linked reduction ranges from 64 to 16,576 bytes on ARM64 and 8,192 to 12,288
bytes on AMD64 because section alignment changes. The full-minus-lean capability
delta is unchanged.

## Performance expectations

The normal and full builds keep the pre-split optimization selection, so this
change is not expected to alter compile memory, compile latency, execution
latency, or generated native bytes for those builds. That equivalence is a
policy invariant, not a claim that every workload has been re-benchmarked.

Lean-build deltas are intentionally workload-dependent:

| Omitted capability | Compile memory and latency | Execution latency | Generated native bytes |
|---|---|---|---|
| Native compaction | Lower metadata/work; usually faster compilation | Normally little change | Larger where relaxation, packing, or sharing applied |
| Producer needles | Lower recognizer code/work | Slower only for matching producer idioms | Usually larger for matching idioms |
| Native GC optimizer | Lower GC analysis/codegen work | Potentially slower on GC-heavy modules using helper fallbacks | Workload-dependent; helper-heavy code may be smaller at call sites |

No numeric runtime or compiler-memory delta is claimed until paired measurements
are recorded. The capability split's confirmed numeric result is linked binary
size.

## Artifact identity

Every native-code-affecting selection remains in artifact identity:

```text
cache key =
    wasm hash
  + backend build identity
  + target GOOS/GOARCH
  + CPU feature mask
  + immutable option bits
  + bounds mode
```

The runtime build identity already distinguishes capability-tagged binaries.
The effective option bits encode which compiled capabilities participate in
code generation, while target/runtime inputs distinguish native code choices
within one build.

## Removal and retention summary

| Item | Treatment |
|---|---|
| Optimization objectives and profiles | Deleted as a breaking API change |
| Native finalizer and compaction | Build capability; internal measurement path retained |
| Associative tree covering | Ordinary catalog optimization; experiment override retained |
| Producer needles | Optional build capability |
| Native WasmGC optimization | Optional build capability |
| Native plugin lowering | Demand-linked; no build tag |
| Stats and explain output | Retained for measurement; no objective selector |
| ARM64 beachhead | Deleted as historical throwaway code |
| Stale `_port` notes | Moved to `docs/archive` |

## Validation gates

- default and full builds preserve the catalog's ordinary selection;
- lean builds expose no option whose implementation was compiled out;
- requesting an unavailable option produces a clear validation error;
- capability combinations compile on AMD64 and ARM64;
- artifact-cache format and policy identity tests pass;
- benchmark explain output has only an explicit `-compact` laboratory switch;
- repository source contains no optimization-objective API or profile selector;
- linked-size measurements and symbol-absence checks are recorded before merge.
