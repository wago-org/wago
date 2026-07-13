# Streaming and bounded-memory single-pass pipeline

Status: in progress. The first bounded-input foundation is landed: named reader
APIs spool through a fixed 32 KiB Go-heap window with a configured input limit,
then stream strict section framing from that file on Unix: unknown custom data
is drained, while only code and data sections receive short-lived private
mappings for the existing decoder/lowering pipeline. Active and passive data
are product-owned copies.
`CompileLimits` now gives input, per-body, native-code, and retained-data
budgets deterministic resource-limit errors before a compiled artifact is
published, including deferred imported-function recompiles.
The native-code budget is additionally checked during Railshot module assembly,
before the output buffer grows past it; a single oversized function is rejected
after its bounded function scratch attempt rather than being appended to module
code.
Reader compilation is covered at every chunk boundary for a representative
module and by a short-read/malformed-name fuzz target; its strict decode result
matches the byte-slice entry point. The spooler also tolerates bounded
intermittent zero-byte/no-error reads before treating a reader as stalled.

Initial measurement (Darwin/arm64, Apple M4 Max, one 4 MiB unknown custom
section, `-benchtime=1x`): the section-streaming `CompileReader` took 2.07 ms,
used 98,456 B, and made 41 Go heap allocations. The custom payload is drained
rather than copied; this is a
heap-allocation measurement, not a peak-RSS claim while the temporary file is
mapped.
For the bounded code-image path, the small scalar compile benchmark on the same
host (`-benchtime=10x`) measured 12.3 µs/op, 27,641 B/op, and 69 allocations
with `WithSealedCode(true)`, versus 15.4 µs/op, 27,816 B/op, and 70 allocations
for the default heap-code representation. This is a small-module allocation
measurement, not a general throughput claim; the important ownership result is
that the sealed artifact retains one RX image and no mutable heap code slice.
`Compiled.Footprint` also reports exact retained native-code/data/replay byte
ranges (and their backing capacities) without pretending that allocator traffic
or process RSS is a stable product-size number.
Railshot's module hint pass now retains only module-wide aggregate facts and
recomputes each function's local pinning hint immediately before lowering, so
the hint workspace no longer scales with function count times global count.
The score and loop-eligibility vectors used for that immediate scan are also
reused for the largest function in the module, rather than allocated for every
lowered body.
Its operand-stack nodes now use that existing opcode pre-scan to reserve one
pointer-stable slab before each function, reusing the largest slab for the
whole module instead of allocating standalone nodes after a fixed 256-node
threshold. Thus this part of compile workspace is bounded by the largest local
body, not by the sum of all bodies. On the Ruby corpus (Darwin/arm64, M4 Max),
this reduced full-compile allocation traffic from 594 MiB / 4.37M allocations
to 235 MiB / 0.95M allocations (−60.4% bytes, −78.4% objects). Fixed-index,
reused trap-site lists and an ARM64 peephole branch-target bitset remove the
remaining per-function maps from those bookkeeping paths. These changes reduce
allocator traffic, rather than the roughly 218 MB Ruby process-RSS peak, which
is dominated by source and retained native code.
Register-pin selection also retains only the fixed register-pool-sized top
candidates while scanning locals, rather than allocating and sorting a list for
every scalar local in a generated function.
Control-edge local-state snapshots are likewise compact: they record only
register-pinned locals, never all declared locals. Since the pin pools are
fixed-size, structured control no longer turns a generated function with many
locals into O(blocks × locals) workspace. Control frames themselves are now a
reused, cleared scratch stack, so their high-water mark is maximum nesting
depth and they cannot retain prior functions' type/state slices.
The compile-oriented backend also releases each lowered `BodyBytes`; only
bounded inline candidates retain private replay copies, while direct-callee
pin-preservation facts are computed in the first pass. General backend callers
retain their input module by default.
Railshot also reserves its normal per-body code capacity once and emits directly
into the final heap code backing store on the common path; oversized functions
fall back safely. The speculative module reservation is capped at 8 MiB, so a
large generated module cannot reserve its entire native-code budget before it
has emitted that much code. This is an interim copy/growth reduction, not the
final RW/RX native code arena. When the retained compiler backing is more than
twice the used native code, the published product also compacts it, avoiding a
large speculative tail in `Compiled.Code`.
AMD64's opcode pre-scan also preallocates each function's direct-call relocation
list and keeps just a 25% operand-stack-node cushion (with pointer-stable heap
fallback for unusual lowering). This prevents call-heavy generated functions
from repeatedly growing relocation slices and avoids retaining a 50% unused
operand-stack slab after the largest function has compiled.
For the amd64 throughput profile, the initial scan products are retained for the
duration of one module compile and passed directly into lowering. This trades
O(functions × locals/globals) temporary hint vectors for eliminating an entire
second bytecode scan before every function's code generation; it is intentionally
the high-throughput counterpart to the lower-retention streaming profile.
The byte-backed validator now also carries decoded instructions by pointer into
its shared validation step, avoiding an instruction-structure copy for every
opcode. The next validator profile work is intentionally aimed at replacing the
generic instruction carrier for common scalar opcodes altogether.
The first fused validator path now handles no-immediate scalar operators directly
from the byte stream; complex, proposal, and control opcodes still use the
generic decoder. Element payload validation is cached by segment index as well,
so repeated `table.init` references never revalidate the same const expressions.
The fast path now also consumes local/global accesses and direct calls in-place,
which eliminates the generic `Instruction` carrier for the highest-frequency
indexed opcodes in generated modules. Memory, indirect-call, and proposal
opcodes remain intentionally isolated behind the generic decoder until each
receives an equally complete direct validator.
The direct path now includes linear-memory loads/stores, direct/indirect tail
calls, numeric constants, and the core structured-control instructions
(`block`, `loop`, `if`, `else`, `end`, branches, `return`, and `drop`). This
removes the large generic decoded-operation carrier from nearly every opcode in
ordinary generated bodies. Bulk-memory, SIMD, GC, and EH proposal instructions
remain on the generic path, where feature completeness matters more than
avoiding their comparatively sparse carrier allocations.
Const-expression validation reuses a validator-owned one-result slice instead
of allocating a new result signature for every global, element, table, and data
offset expression. This cuts the remaining large validation allocation source
without retaining source bodies or weakening expression checks.
On the hub amd64 corpus, the direct validator paths reduced end-to-end Ruby
`CompileFull` to about 895 ms and esbuild to about 945-956 ms, while esbuild
compile allocation traffic fell to about 138.8 MiB/op. Validation remains a
separate semantic boundary for now: compiler-scan fusion must prove equivalent
coverage for every accepted and rejected opcode before it can replace it.
The single supported memory type is cached in the module validator after its
first lookup. This removes an import-list scan from every memory opcode and is
especially important for generated memory-heavy modules.
A 16-entry direct-mapped function-signature cache similarly avoids repeated
function-import/type resolution for call-heavy bodies while keeping validator
metadata bounded independently of module function count.
Operand validation fast-paths exact non-reference value types before entering
general subtype logic, retaining the complete reference-subtyping path only
where it is semantically required.
Imported-global count is likewise cached once, so the common local-global path
does not scan the module import list for every `global.get` or `global.set`.
On the same native AMD64 host, direct control and numeric-constant validation
changed `BenchmarkValidate` from 163.1 ms to about 136.9 ms for Ruby (-16%) and
from 106.7 ms to about 86.1 ms for esbuild (-19%), with allocation counts
unchanged (the win is CPU work and stack traffic, not retained memory).
The validator operand stack now stores scalar type tags compactly and keeps
reference payloads in a side slab indexed by stack slot. It retains full
reference subtyping and refinement semantics, while avoiding a full `ValType`
copy for every numeric push/pop. On that same host this reduced Ruby validation
again from 136.9 ms to 110.8 ms (-19%) and esbuild from 86.1 ms to 67.7 ms
(-21%); temporary allocation also fell slightly without a per-module cache.
Borrowing the already-stored local type on the byte-backed `local.get/set/tee`
path removes the remaining local-value copy: Ruby reached about 106.6 ms and
esbuild about 65.2 ms, with the same allocation counts.
`RuntimeConfig.WithSealedCode(true)` now completes the native-image phase for
direct, non-link-deferred modules: Railshot emits into that same bounded RW
mapping, relocations are patched there, and the mapping is sealed RX before the
artifact is returned. `Compiled.Code` is nil in this opt-in profile, so there is
no heap-code-to-RX copy. If the bounded estimate cannot hold a pathological
function, the compiler retries the established bounded heap path and seals it
before publication; this keeps the memory optimization non-semantic and
deterministic. `Compiled.MaterializeCode` remains the explicit compatibility
escape hatch for callers that need mutable bytes or serialization.
ARM64 module-layout relocation now rejects a direct wasm-call displacement
outside the architectural `BL` range instead of silently leaving a bad patch.
AMD64 likewise rejects a direct-call displacement outside `rel32` range.
Function-import recompilation now retains a compact, product-owned structural
link artifact rather than raw wasm: custom sections and data payloads are not
kept merely for a future cross-instance bind. For modules whose function imports
are all instance exports, Railshot now emits one shared-image dynamic binding
path: each instance supplies only a 16-byte `{linear-memory, wrapper-entry}`
descriptor per import in its off-heap arena, avoiding a per-binding recompilation
and executable image. Mixed host/cross-import modules still use the established
static linker while dual dispatch is developed. Deferred linker replay bodies
are stored in one unlinked Unix file and mapped only around an actual link-time
recompile; ordinary host-only modules retain no body mapping in RSS. The section-stream
decoder now spools/maps every section independently: compact metadata is copied
then unmapped immediately, while code/data mappings live only through
validation/lowering. It reuses the established strict section decoders rather
than mapping the whole source. The native code arena remains the next phase.

## Goal

Make Wago's direct ("single-pass") compiler pipeline consume WebAssembly in
chunks and bound its *transient* memory use. The target is not constant total
memory: a reusable compiled product must still retain native code, function
metadata, names, and active/passive segment payloads proportional to the
module. Instead, separate those unavoidable outputs from a fixed or explicitly
budgeted input/validation/code-generation workspace.

The primary target is the Railshot path on amd64 and arm64. It remains a direct
wasm-to-native backend; replaying a body or code section is acceptable when it
preserves current code quality and does not introduce an IR tier.

## Current constraints

Today `Compile` accepts `[]byte`; the CLI uses `os.ReadFile`; and byte-backed
decode is a cursor over that complete allocation. Decoded bodies, const
expressions, and active data are slices of the source buffer. The compile path
then performs separate decode, validation, support, hint, and code-generation
walks.

The backend compiles functions sequentially, but it first retains a
module-wide hint for every function. Those hints contain per-local and
per-global arrays, so their retained cost can be O(functions * globals).
Forward inlining, module-global pinning, and immutable-table specialization
also depend on future bodies. The emitted module code grows in a heap `[]byte`,
then the first instantiation copies it into an RX mapping.

Function imports add a separate whole-source dependency: link-time recompiling
currently copies, re-decodes, and re-lowers the original wasm in order to bake
cross-instance call addresses into code.

## Design invariants

- Preserve strict binary decoding. A malformed structured custom section,
  including `name`, must still fail; unknown custom payloads may be drained but
  not silently accepted.
- Preserve the current decode-before-validate diagnostic precedence. A
  speculative body validation/codegen error must be recorded while decoding
  continues through EOF; a later decode error wins.
- Do not publish executable code or instantiate a module until all sections,
  including trailing data/custom sections, are accepted.
- Keep local wasm-to-wasm calls direct on the hot path. Do not replace them with
  a universal dispatch table merely to simplify streaming.
- Keep compilation sequential initially. Parallel compilation increases the
  peak working set and is not needed for the first bounded-memory result.
- Make every budget explicit: input window, body/spool policy, validation
  workspace, metadata, native code, and retained segment bytes. Exceeding a
  configured budget must return a clear resource-limit error.

## Target pipeline

```text
io.Reader
  -> strict chunked section decoder
       -> compact structural metadata
       -> code-body replay store + validation/body summaries
       -> retained data/element/name product data
  -> replay each body through Railshot
       -> contiguous private RW native-code arena
  -> resolve relocations, seal used pages RX, publish compiled artifact
```

The section decoder uses a small fixed ring plus bounded section readers. It
parses scalar sections incrementally, drains unknown custom payloads, and
strictly parses the `name` section without retaining raw custom bytes in the
production compile path. WebAssembly section order is favorable: the type,
import, function, table, memory, global, export, element, and `data_count`
information needed by function validation precedes the code section. The final
data-count equality check remains after the later data section.

## Compilation modes

### Optimized bounded-RAM mode (default)

Use a replayable source for the code section: a caller-supplied `ReaderAt` or
seekable file when available, otherwise a bounded-chunk temporary spool. The
first body pass validates and produces a compact `BodySummary`; after module
facts are known, Railshot replays one body at a time. This preserves forward
inlining, module-global pin selection, immutable-table proof, lookahead-based
lowering, and existing retry behavior while keeping input RAM bounded.

The first pass should share one opcode decoder across validation, feature
admission, required-feature discovery, segment-index tracking, and function
hints. This replaces several current scans while keeping validation separate
from backend lowering and auditable.

### One-way streaming mode (optional)

For a non-seekable source that may not be spooled, validate and lower one body
as it arrives, retaining only the current body/workspace. This mode must use
conservative per-function decisions: no forward inlining, no module-global
pins, and no optimization that requires replaying bytes. It is a memory/latency
profile, not a semantic subset.

Current Railshot needs random access within a body for loop scans, constant
preloads, and some speculative lowering. Therefore the initial one-way mode
may retain one complete body in a bounded buffer or spill oversized bodies; a
truly fixed body window requires further backend refactoring or an explicit
maximum-body limit.

## Native-code storage and publication

Replace the growing module `[]byte` accumulator with a contiguous private RW
virtual code arena. Railshot can emit each function directly at the aligned
unused tail: existing function-relative branch patching and retry/rewind logic
still work, and the used offset advances only after success. Store entry offsets
and unresolved direct-call fixups, patch them after final layout, then protect
the used pages RX.

The opt-in `RuntimeConfig.WithSealedCode(true)` implementation now realizes
this for the bounded normal reservation. Railshot receives a fixed output slice
backed by the arena and returns a distinct retryable error before it would grow
outside it. On success the runtime seals the full page-rounded mapping RX and
retains only its used code length. On an underestimate it retries the normal
heap path, seals that result, and still returns the same immutable artifact.
This eliminates geometric code-buffer growth, the function-scratch-to-module
copy, and (on the direct-arena success path) the later heap-to-RX copy. It
requires checked emitter capacity rather than unchecked `append`, a native-code
size limit, and architecture-specific branch-range handling. In particular,
arm64 must reject/veneer calls outside the ±128 MiB `BL` range rather than
ignoring a failed patch.

The public mutable `Compiled.Code []byte` cannot silently become an RX mmap
view: callers could fault on an ordinary write, and `Close` could leave a
dangling slice. Introduce an internal immutable executable image/ownership
model first; retain a materialized compatibility and serialization path until a
new public artifact API is intentionally adopted. `Compiled.SealCode` now makes
that transition explicit: it adopts the RX mapping and drops the mutable heap
slice; `MaterializeCode` (also used by `MarshalBinary`) restores the
compatibility bytes when needed.

## Linking and retained source

Remove the raw-wasm requirement for function imports. Two viable designs are:

1. Per-import thunks that load a per-instance binding descriptor (callee linear
   memory and entry) and branch indirectly. This keeps local calls direct and
   allows one generic compiled image to be shared, at a small imported-call
   cost.
2. A compact relocatable link artifact that patches a fresh per-binding mapping
   without re-decoding or re-running Railshot. This preserves more direct import
   calls but consumes a mapping per binding configuration.

Benchmark both before choosing. The streaming path must not retain the full
source merely to support cross-instance linking.

## API and ownership

Add named reader APIs rather than extending the variadic `Compile` API:

- `CompileReader` / `CompileReaderWithConfig` for low-level compilation;
- `CompileReaderAt` / `CompileReaderAtWithConfig` for a known-length replayable
  source, avoiding the input spool on Unix;
- `Runtime.CompileReader` with either new streaming hooks or an explicit
  materializing fallback when legacy `BeforeCompile(func([]byte) []byte)` hooks
  are installed.

Define product ownership deliberately: active and passive data retained for a
reusable compiled module must live in product-owned storage, never alias a
caller buffer. An immediate compile-and-instantiate API may later apply active
data directly to an unpublished instance, but that is not a replacement for
the reusable `Compiled` contract.

Do not place pointer-rich Go compiler state in a raw mmap arena: the garbage
collector will not scan it. Use typed Go slabs/pools for validator and Railshot
scratch, or convert pointer graphs to index-based structures before considering
off-heap storage.

## Implementation sequence

1. Add compile-memory instrumentation: input bytes, retained product bytes,
   native-code bytes, workspace high-water marks, allocation count, and peak
   process RSS for representative corpus modules.
2. Define compile resource limits and errors. Add tests for deterministic
   exhaustion and for no caller-buffer aliasing in compiled data.
3. Introduce the chunked binary reader and a compile-oriented decoder that
   retains only metadata needed by the product. Differential-test it against
   `DecodeModule` and the malformed/invalid spec corpus.
4. Introduce shared body decoding plus `BodySummary`; validate bodies during the
   first pass and replay them in the existing backend. Initially use a spool to
   preserve code quality.
5. Refactor Railshot module assembly around a checked contiguous code arena;
   retain direct local calls and add explicit amd64/arm64 relocation range
   checks.
6. Add executable-image ownership so instantiation adopts sealed code instead of
   copying it. Keep the existing `Compiled.Code` compatibility path separate.
7. Replace raw-source link-time recompilation with a measured thunk or compact
   relocation-artifact design.
8. Add the optional one-way profile only after the optimized path is correct and
   measured.

## Verification and acceptance

- Byte-for-byte/error-phase differential tests against the existing decoder and
  validator, including malformed `name` sections, section-size/order failures,
  data-count failures, and unreachable-code validation.
- Existing core and Release 2 spec suites, corpus differential tests, and both
  explicit and guard-page bounds modes on amd64 and arm64.
- New fuzzing across arbitrary chunk boundaries, short reads, and spool versus
  seekable replay.
- Memory reports for json-as, SQLite, Ruby, and esbuild that distinguish total
  allocation traffic from peak live heap/RSS and retained compiled-product
  footprint.
- No regression in local-call, host-call, instantiation, or code-size benchmarks
  beyond an agreed threshold. Measure imported-call tradeoffs separately.
