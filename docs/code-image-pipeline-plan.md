# Code-image and artifact pipeline

Status: active. Phase 1 is implemented on `codex/code-image-pipeline`.

Tracking issues: #316, #330, #331.

## Goal

Give one bounded owner responsibility for native code from Railshot emission to
execution and `.wago` persistence. The finished path should not construct the
same full native image repeatedly in assembler scratch, a Go-heap module slice,
a serialized blob, and an executable mapping.

This is not a revival of the superseded WARP topology redesign. The frontend
and architecture backends remain separate where their semantics differ. The
shared seam is ownership of already-selected native bytes, their relocations,
and their persisted sections.

## Current costs

Before this project, serial compilation copied every completed function from a
reused assembler scratch into a growable Go slice. First instantiation then
allocated an RW executable mapping, copied the complete slice again, changed it
to RX, and discarded the heap backing. `MarshalBinary` constructs another full
blob. `UnmarshalBinary` requires the complete artifact in memory and copies its
code into RX on first instantiation.

The initial Darwin/ARM64 baseline was captured on an Apple M4 Max with Go's
default benchmark duration, eight repetitions:

| Measurement | Before | Phase 1 | Delta |
| --- | ---: | ---: | ---: |
| Serial 1,600-function compile, median | 36.93 ms | 36.81 ms | 1.003x faster |
| Serial 1,600-function compile, Go heap | 3,283,350 B/op | 759,784 B/op | 4.32x less |
| Serial 1,600-function compile allocations | 1,705 allocs/op | 1,704 allocs/op | 1 fewer |
| Serial 1,600-function compile, median peak RSS | 25,886,720 B | 20,119,552 B | 1.29x less |
| 1 MiB code transition, copy and seal vs seal in place | 72.08 us | 2.68 us | 26.9x faster |
| Small artifact decode, Go heap | 1,857 B/op | 1,857 B/op | unchanged |

Peak RSS is the median of five fresh ten-compile processes, alternated between
the main and phase-1 test binaries. The transition microbenchmark times only the
first-execution mapping step; RW image allocation and compiler writes occur
during compilation. Steady-state execution, artifact bytes, and machine-code
bytes are intentionally unchanged. The ordinary instantiate benchmark is also
unchanged because it amortizes the one-time transition across all iterations.

## Ownership model

The neutral `core/codeimage.Image` interface transfers an image between the
backend and public runtime without making encoders depend on runtime internals.
The current implementation has these states:

1. Railshot allocates a page-backed RW image using its measured capacity hint.
2. It appends aligned functions and patches relocations in that image.
3. Capacity underestimates grow geometrically and preserve existing bytes; they
   never reject an otherwise valid module.
4. `Compiled` takes the raw mapping and records the writable state in its
   existing compact cache flags. No extra per-decoded-module owner allocation or
   interface storage remains.
5. First instantiation flips the same mapping to RX and registers it for host
   interruption. The address cannot change across the transition.
6. `Compiled.Close` owns unmapping before first instantiation; after sealing,
   instances retain the existing reference-counted lifetime.

There is no RWX state. Darwin uses `MAP_JIT` plus `mprotect`, Linux uses an
anonymous RW mapping plus `mprotect`, and Windows uses `VirtualProtect` and
flushes the instruction cache.

## Phases

### Phase 1: serial direct image

- [x] Add a checked growable RW code-image owner.
- [x] Keep platform-specific W^X operations in the runtime.
- [x] Emit the serial AMD64 and ARM64 module image off heap.
- [x] Patch calls before sealing.
- [x] Transfer ownership without growing decoded-module footprint.
- [x] Prove first instantiation retains the exact code address.
- [x] Preserve byte-identical code, codec output, and execution behavior.

### Phase 2: bounded parallel join

- [ ] Replace per-worker geometric byte slices with reusable bounded arenas.
- [ ] Precompute deterministic final offsets after workers report function sizes.
- [ ] Join worker products directly into one final RW image.
- [ ] Retain serial/parallel byte identity and source-ordered errors.
- [ ] Measure compile latency, Go allocations, peak RSS, and copied bytes at 1,
  2, 4, 8, and adaptive workers on the benchmark corpus.

Parallel workers will keep independent per-function scratch. Sharing a mutable
assembler or making output order scheduling-dependent is out of scope.

### Phase 3: sectioned artifact v33

- [ ] Attribute bytes to code, entries, types, imports/exports, names, globals,
  tables, data, memories, tags, feature requirements, and GC roots.
- [ ] Replace the positional codec with strictly ordered, length-delimited
  sections. Unknown required sections and duplicate sections fail closed.
- [ ] Add `Compiled.WriteTo(io.Writer)` without first materializing the complete
  artifact.
- [ ] Add a bounded reader API with explicit artifact, section, count, string,
  and native-code limits.
- [ ] Decode code directly into an RW image and seal it on first execution.
- [ ] Reject v32 rather than retain an unreleased compatibility reader.
- [ ] Keep malformed structured metadata strict and deterministic.

### Phase 4: compact public surface

- [ ] Make native code storage private.
- [ ] Replace mutable `Compiled.Code` access with `CodeSize` and streaming/debug
  accessors that cannot mutate a sealed image.
- [ ] Move hand-built test modules to a dedicated internal constructor.
- [ ] Make `Close` and instance ownership rules explicit in package docs.

This is intentionally breaking: Wago has not released the artifact or the
mutable `Compiled.Code` field, so carrying aliases or two codec generations
would add permanent surface without protecting real users.

## Gates

Every phase must keep:

- Wasm semantics and malformed-module rejection unchanged;
- serial and parallel native bytes identical for the same target and config;
- RW to RX transition only, with no writable executable interval;
- bounded counts and sizes for every loaded section;
- deterministic errors independent of worker scheduling;
- no regression in steady-state execution or native code size.

Measurements must report raw values and deltas for compile latency, first-load
latency, execution, Go allocations, peak RSS, `.wago` bytes, native-code bytes,
and bytes copied. A phase that moves cost off Go's heap without lowering RSS is
reported as such rather than called a memory reduction.
