# Railshot optimization policy

Status: implemented

Source baseline: `main` at `4799c654`

## Decision

Railshot is Wago's lean compiler backend. It has one ordinary
performance-oriented compilation policy: no `-O1`, `-O2`, `-O3`, Speed,
Balanced, Size, or Embedded modes, and no public optimization-objective API.

Railshot is not divided into build-tagged compact, producer-needle, GC
optimizer, or full variants. Its bounded single-pass optimizations are part of
the backend. A future heavyweight optimizer belongs in a separate multi-pass
backend instead of making Railshot internally selectable between lean and heavy
forms.

The governing rule is:

> **Railshot stays lean and single-pass. Cheap code-quality decisions use
> immutable bits. Individual toggles are laboratory controls. Safety invariants
> are not flags. Heavy optimization belongs in a separate backend.**

## One compilation policy

Ordinary compilation starts from the optimization catalog defaults and applies
validated individual overrides. The resolved selection is an immutable bitset
owned by each compilation. Hot lowering performs a bit test; it does not read a
map, acquire a catalog lock, inspect a snapshot revision, or mutate
package-global compiler state.

There is no optimization objective in `RuntimeConfig`, backend compile options,
serialized provenance, or artifact-cache identity. Removing that API is
intentionally breaking. The artifact-cache key format is bumped so old and
objective-free key layouts cannot collide.

Native compaction remains an internal `CompileOptions.CompactNative` path for
benchmarks, byte accounting, and rollout checks. It is not a public runtime
mode. `WAGO_COMPACT=1` remains the force-on measurement oracle and
`WAGO_COMPACT=0` the global rollback oracle; neither creates an optimization
profile.

CPU instruction legality remains separate from optimizer choice and is selected
from the target feature mask.

## Experiment controls

The existing individually named optimization metadata remains useful for tests,
benchmark bisection, and environment kill switches. It resolves once into the
immutable selection bitset before compilation. It is not the normal public
configuration surface and does not own compiler state.

This includes associative tree covering, bounded producer-specific SWAR/SIMD
recognizers, and native WasmGC optimizations. They remain in Railshot because
they are direct, bounded code-generation decisions. Unbounded analysis or
iterative rewriting belongs in the future multi-pass backend.

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
correct struct/array/i31 behavior are always present. Native WasmGC code quality
can still be bisected with experiment controls, but correctness cannot.

## Footprint and performance

Removing the four Railshot capability tags leaves no tag-dependent Railshot
behavior. With Go 1.26.5, `CGO_ENABLED=0`, `-ldflags '-s -w'`, and the
`wago_runtime,wago_lean,wago_minimal` base tags, the resulting CLI is 7,477,762
bytes on Darwin/ARM64 and 8,143,010 bytes on Linux/AMD64. Compared with the
previous capability experiment's stripped lean variant, retaining the complete
Railshot backend costs 82,624 linked bytes on ARM64 and 126,976 bytes on AMD64.
These are linked-output deltas, not additive subsystem sizes.

The ordinary untagged build already contained those systems, so this revision is
not expected to change its compile memory, compile latency, execution latency,
or generated native bytes. `wago_lean` now affects the surrounding runtime and
CLI only; it no longer changes Railshot code generation.

No numeric runtime or compiler-memory delta is claimed until paired
measurements are recorded. The retained optimizations have workload-dependent
effects:

| Railshot mechanism | Compile memory and latency | Execution latency | Generated native bytes |
|---|---|---|---|
| Native compaction | Adds bounded metadata and finalization work when selected | Normally little change | Smaller where relaxation, packing, or sharing applies |
| Producer needles | Adds bounded recognizer work | Faster for matching producer idioms | Usually smaller for matching idioms |
| Native GC optimization | Adds bounded GC analysis and code generation | Faster on matching GC-heavy paths | Workload-dependent; avoids helper calls at some sites |

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

The runtime build identity distinguishes binaries. The effective option bits and
target/runtime inputs distinguish native-code decisions within one build.

## Removal and retention summary

| Item | Treatment |
|---|---|
| Optimization objectives and profiles | Deleted as a breaking API change |
| Railshot capability tags and masks | Deleted; Railshot is one backend |
| Native finalizer and compaction | Retained; internal measurement path remains |
| Associative tree covering | Retained with experiment override |
| Producer needles | Retained as bounded Railshot lowering |
| Native WasmGC optimization | Retained as bounded Railshot lowering |
| Native plugin lowering | Demand-linked; no build tag |
| Stats and explain output | Retained for measurement; no objective selector |
| ARM64 beachhead | Deleted as historical throwaway code |
| Stale `_port` notes | Moved to `docs/archive` |

## Validation gates

- default and `wago_lean` builds expose the same Railshot optimization catalog;
- removed Railshot tags have no source or behavior;
- artifact-cache format and policy identity tests pass;
- benchmark explain output has only an explicit `-compact` laboratory switch;
- repository source contains no optimization-objective API or profile selector;
- linked-size measurements are recorded without claiming runtime deltas; and
- AMD64 and ARM64 compile and conformance suites pass.
