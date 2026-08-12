# Code-image and artifact pipeline

Status: phases 1-4 complete on `main`; direct serial emission follow-up complete.

Tracking issues: #316, #330, #331.

## Goal

Give one bounded owner responsibility for native code from Railshot emission to
execution and `.wago` persistence wherever the balanced performance gate allows
it. The serial and streamed-artifact paths must not construct the same full
native image repeatedly. The parallel join may retain its heap image only when
direct executable-image alternatives measurably regress compile latency.

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

The original phase 1 image was off heap, but each function was still encoded
into reusable heap scratch and copied once into that image. The direct-emission
follow-up makes the writable image tail the serial assembler's output buffer and
commits it transactionally. If a function exceeds the reserved tail, the
assembler moves to heap and the compiler safely falls back to `Append`; a
capacity estimate can still affect performance, never correctness.

On the same Apple M4 Max, five 500 ms samples of the public serial compile path
measured:

| Module | Full compile before | Direct emission | Delta | Go heap before | Direct emission | Delta |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| many_funcs | 314.4 us | 306.2 us | 1.027x faster | 217,203 B/op | 216,644 B/op | 1.003x less |
| json-as | 1.468 ms | 1.387 ms | 1.058x faster | 400,023 B/op | 355,959 B/op | 1.124x less |
| sqlite3 | 90.79 ms | 88.43 ms | 1.027x faster | 7,092,716 B/op | 6,615,945 B/op | 1.072x less |
| ruby | 1.069 s | 1.029 s | 1.039x faster | 40,137,960 B/op | 37,394,792 B/op | 1.073x less |
| esbuild | 709.3 ms | 688.9 ms | 1.030x faster | 46,570,752 B/op | 42,765,200 B/op | 1.089x less |

The ruby and esbuild latency changes reached significance in these five-sample
runs (`p=0.032` and `p=0.016`); the smaller corpora did not. Five fresh esbuild
processes reduced median peak RSS from 138,706,944 to 136,527,872 bytes, 1.016x
less. The completed-function `CodeBuffer.Append` copy disappeared from the CPU
profile; first-touch page cost moved into encoder stores, explaining why the
latency gain is much smaller than the removed copy's former 33.4% CPU-profile
share. Native code size stayed byte-for-byte unchanged. Twelve interleaved
execution samples were also unchanged: fib_iter was 66.72 vs 66.40 ns/op
(`p=0.590`) and json-as serialization was 19.04 vs 19.01 us/op (`p=0.671`),
both at zero B/op and zero allocations/op.

The review gate was repeated after rebasing the complete branch onto
`5a5327bf`. Median allocation counts moved from 403 to 400 allocs/op on
many_funcs, 1,350 to 1,339 on json-as, 37,369 to 37,355 on sqlite3, 252,216 to
252,198 on ruby, and 81,198 to 81,182 on esbuild. A first-load-only json-as
benchmark recompiled before each timed first instantiation and alternated ten
200-iteration samples: the median was 29.86 us on main and 27.64 us on the
branch (7.45% lower), with both sides at 199 allocs/op and approximately 6,033
B/op. Sample variance was high, so this is evidence against a regression, not a
claimed first-load speedup. The compile-produced mappings were also exactly the
same page-rounded sizes (32,768 bytes for many_funcs and 81,920 for json-as).

Artifact and native-code bytes were byte-identical; the raw sizes below apply
to both main and the branch. Direct emission removes the completed-function
copy (zero such copy on the branch); the code-image size is the conservative
upper bound on bytes formerly copied because it also includes alignment and
module-level code.

| Module | Native code bytes | `.wago` bytes |
| --- | ---: | ---: |
| many_funcs | 28,984 | 34,636 |
| json-as | 77,388 | 86,000 |
| sqlite3 | 4,029,344 | 4,207,097 |
| ruby | 44,146,196 | 49,584,314 |
| esbuild | 31,506,684 | 35,097,196 |

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
- [x] Emit each serial function directly into the writable image tail.
- [x] Retain a checked heap-copy fallback for a tail-capacity underestimate.

### Phase 2: bounded parallel join investigation

The direct-mapping join is deliberately rejected. On the 1,600-function p4
benchmark it reduced Go heap from 6,250,485 B/op to 3,727,622 B/op (1.68x less)
but increased median compile latency from 19.97 ms to 22.54 ms (12.9% slower).
The mmap-backed join was removed rather than trading away parallel throughput.
A second version computed compact offsets first and had four workers populate
disjoint final ranges concurrently, leaving mapping pages untouched until each
worker first-used them. It still measured about 22.7 ms and added four join
allocations. A third version allocated the mapping concurrently with codegen,
prefaulted it, kept the original workers alive across an offset barrier, and had
those workers populate disjoint final ranges. It reduced Go heap from about
6.25 MB/op to 3.73 MB/op, but the best six-sample p4 median moved from 21.92 ms
to 22.44 ms (+2.4%) and added about six allocations. The version without
prefaulting was slower. All three prototypes were removed: merely replacing the
final heap slice does not pass the balanced gate.

- [x] Measure the existing worker arenas and deterministic heap join.
- [x] Prototype a direct RW-image join and record heap/latency deltas.
- [x] Prototype precomputed offsets with concurrent disjoint population.
- [x] Prototype overlapped mapping allocation, prefaulting, and worker reuse.
- [x] Retain serial/parallel byte identity and source-ordered errors.
- [x] Retain the heap-backed parallel join as the measured low-latency exception.

Parallel workers will keep independent per-function scratch. Sharing a mutable
assembler or making output order scheduling-dependent is out of scope.

### Phase 3: sectioned artifact (public version 1)

- [x] Attribute bytes to code, entries, types, imports/exports, names, globals,
  tables, data, memories, tags, feature requirements, and GC roots.
- [x] Replace the positional outer codec with strictly ordered, length-delimited
  sections. Unknown required sections and duplicate sections fail closed.
- [x] Add `Compiled.WriteTo(io.Writer)` without first materializing the complete
  artifact.
- [x] Add a bounded reader API with explicit code and metadata section limits;
  nested counts and strings remain bounded by their section's remaining bytes.
- [x] Decode code directly into an RW image and seal it on first execution.
- [x] Reject incompatible development blobs rather than retain an unreleased compatibility reader.
- [x] Keep malformed structured metadata strict and deterministic.

For the 1,600-function imported-module fixture, five one-second samples put
`WriteTo(io.Discard)` at a 182.5 us median and 263,265 B/op versus
`MarshalBinary` at 280.3 us and 1,992,885 B/op: 1.54x faster and 7.57x less Go
heap. Streamed read plus first code acquisition measured 264.7 us and
224,095 B/op versus 255.3 us and 190,512 B/op for whole-blob unmarshal plus its
first mapping: 3.7% slower and 1.18x more decoder heap. That comparison excludes
the caller-owned complete artifact buffer required by `UnmarshalBinary`;
`ReadFrom` consumes a file/network stream directly and therefore avoids that
additional artifact-sized allocation. `ReadFrom` alone is intentionally slower
than slice-backed unmarshal because it performs the executable mapping up front.

### Phase 4: compact public surface

- [x] Make native code storage private.
- [x] Replace mutable `Compiled.Code` access with `CodeSize` and streaming/debug
  accessors that cannot mutate a sealed image.
- [x] Move hand-built test modules to a dedicated internal constructor.
- [x] Make `Close`, decode replacement, and live-instance ownership rules
  explicit in API documentation and tests.

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
