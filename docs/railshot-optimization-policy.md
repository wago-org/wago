# Railshot optimization policy

Status: implemented

Source baseline: `main` at `4799c654`

## Decision

Railshot is Wago's default high-performance compiler backend. It has one
performance policy: generate the fastest practical native code while preserving
Railshot's direct, bounded compilation, low memory use, and short startup
latency. There are no `-O1`, `-O2`, `-O3`, Speed, Balanced, Size, or Embedded
modes and no public optimization-objective API.

Railshot is not divided into build-tagged variants. Its qualified, bounded
single-pass optimizations are part of the backend. A future multi-pass optimizer
is a separate backend rather than another Railshot mode.

The governing rule is:

> **Railshot has one fast default. Keep measured code-quality wins that fit its
> bounded compiler. Individual toggles are laboratory controls. Safety
> invariants are not flags. Multi-pass optimization belongs in a separate
> backend.**

## One compilation policy

Ordinary compilation starts from the optimization catalog's qualified defaults
and applies validated individual overrides. Defaults favor measured execution
wins while treating compile latency and memory as hard constraints. The
resolved selection is an immutable bitset owned by each compilation. Hot
lowering performs a bit test; it does not read a map, acquire a catalog lock,
inspect a snapshot revision, or mutate package-global compiler state.

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
they are direct, bounded code-generation decisions. A mechanism is default-on
when broad measurement supports it; an unqualified experiment can remain
default-off without creating a user-visible profile.

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

## Performance contract

Railshot's default is the product. It is not a midpoint between separately
selectable speed and size modes.

- Prefer measured execution wins on representative workloads.
- Keep compile latency, peak memory, and allocation growth within the project's
  Railshot gates.
- Track generated native bytes, but do not trade common-path execution speed for
  size unless the runtime result is neutral or better.
- Keep exact, bounded producer and WasmGC specializations when they improve the
  workloads they recognize without harming the broad corpus.
- Leave speculative or workload-negative mechanisms default-off until paired
  evidence qualifies them.

Native compaction remains an internal measurement path because its main effect
is output bytes and it adds compilation work without an established broad
execution win. Removing the capability tags does not change the ordinary
default selection, so this revision is not expected to change compile memory,
compile latency, execution latency, or generated native bytes for ordinary
builds.

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
| Railshot capability tags and masks | Deleted; Railshot has one default backend policy |
| Native finalizer and compaction | Retained; internal measurement path remains |
| Associative tree covering | Retained with experiment override |
| Producer needles | Retained in the qualified default selection |
| Native WasmGC optimization | Retained in the qualified default selection |
| Native plugin lowering | Demand-linked; no build tag |
| Stats and explain output | Retained for measurement; no objective selector |
| ARM64 beachhead | Deleted as historical throwaway code |
| Stale `_port` notes | Moved to `docs/archive` |

## Validation gates

- removed Railshot tags have no source or behavior;
- the ordinary default retains the pre-change optimization selection and native output;
- experimental defaults change only with paired compile and execution evidence;
- artifact-cache format and policy identity tests pass;
- benchmark explain output has only an explicit `-compact` laboratory switch;
- repository source contains no optimization-objective API or profile selector;
- AMD64 and ARM64 compile and conformance suites pass.
