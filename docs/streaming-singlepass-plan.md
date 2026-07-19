# Streaming and bounded-memory single-pass pipeline

Status: superseded; retained as design history.

Wago does not plan to stream decode, validation, or Railshot compilation. The
current direction keeps the complete decoded module and function bodies
available to code generation, while reducing compile memory through reusable
per-function scratch, compact metadata, and pre-sized retained output. This
preserves unrestricted lookahead for current and future whole-module analyses.

## Historical goal

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

The default backend path compiles functions sequentially; the opt-in function-
worker policy can instead compile independent bodies with bounded worker-local
scratch and code arenas. Both paths first retain a module-wide hint for every
function. Those hints contain per-local and per-global arrays, so their retained
cost can be O(functions * globals). Forward inlining, module-global pinning, and
immutable-table specialization also depend on future bodies. The emitted module
code grows in a heap `[]byte`, then the first instantiation copies it into an RX
mapping.

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
- The historical streaming design would have kept compilation sequential at
  first. Production Wago now offers opt-in bounded function workers; any future
  streaming mode must preserve an explicit serial policy for its lowest-memory
  configuration.
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

This eliminates geometric code-buffer growth, the function-scratch-to-module
copy, and the later heap-to-RX copy. It requires checked emitter capacity rather
than unchecked `append`, a native-code size limit, and architecture-specific
branch-range handling. In particular, arm64 must reject/veneer calls outside
the ±128 MiB `BL` range rather than ignoring a failed patch.

The public mutable `Compiled.Code []byte` cannot silently become an RX mmap
view: callers could fault on an ordinary write, and `Close` could leave a
dangling slice. Introduce an internal immutable executable image/ownership
model first; retain a materialized compatibility and serialization path until a
new public artifact API is intentionally adopted.

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
- a replayable-source variant or options type for optimized mode;
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
