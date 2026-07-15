# WebAssembly 3.0 implementation status

Last updated: 2026-07-15.

This document is the implementation ledger for the WebAssembly Core 3.0 effort.
The primary product target is `linux/amd64`. A row is not complete merely because
`src/core/compiler/wasm` can decode an opcode: wago claims support only when the
feature is decoded, validated, admitted by configuration, lowered by railshot,
instantiated, executed, represented in public metadata and `.wago` artifacts,
and covered by the applicable official tests.

The implementation remains deliberately strict. Malformed modules and malformed
structured custom sections are rejected. Unsupported valid features stop at an
explicit validation or frontend boundary; they are never ignored or silently
executed with older semantics.

## Release definition and official suite

The independently pinned Release 3 source is:

- repository: `WebAssembly/spec`;
- tag: `wg-3.0`;
- commit: `9d36019973201a19f9c9ebb0f10828b2fe2374aa`;
- upstream commit date: 2025-09-26;
- checkout: `tests/spec-v3`;
- official core directory: `tests/spec-v3/test/core`;
- discovered corpus size in this iteration: 258 `.wast` files.

`internal/spectest.DiscoverRelease3` requires the official `test/core` layout and
sentinels for extended constants, tail calls, typed function references, GC,
exceptions, multi-memory, memory64, table64, and relaxed SIMD. This prevents a
Release 2 checkout or a legacy proposal aggregate from being mislabeled Release
3. `make spec3` now targets this pin.

The execution harness is intentionally red until support is real:

- Release 3 parser/tool failures are hard failures, not toolchain skips;
- compile and instantiate rejections remain counted as feature gaps;
- Release 3, like Release 2, is required to finish with zero skipped modules and
  zero skipped assertions before a conformance claim is made.

Iteration 2 pinned WABT `wast2json` 1.0.41 and bootstrapped checksum-verified
official release archives through `scripts/bootstrap-wabt.sh` on linux/amd64,
linux/arm64, and darwin/arm64. Its first complete pass established the historical
red baseline: WABT converted 230 files, failed on 28, and exposed 1,656 passing
modules plus 51,678 passing assertions.

Iteration 3 closes that text-oracle blocker without exclusions. It bootstraps the
official WebAssembly/spec 3.0.0 reference interpreter directly from the pinned
`wg-3.0` submodule revision through `scripts/bootstrap-spec-interpreter.sh`.
Admission checks require all of the following:

- the suite checkout is exactly `9d36019973201a19f9c9ebb0f10828b2fe2374aa`;
- the installed tool carries the same source-revision stamp;
- the binary identifies itself as `wasm 3.0.0 reference interpreter`;
- WABT still reports exactly 1.0.41 and remains the primary converter.

For each file, `make spec3` first runs WABT. Only when WABT rejects the text does
the official interpreter run in dry mode and emit a binary script. The strict
`scripts/spec-interpreter-json.py` converter parses that documented binary-script
subset, writes exact embedded Wasm bytes, and preserves module definitions,
repeated instances, registrations, actions, assertion kinds, scalar/reference
values, and alternative result patterns for the Go execution harness. Unknown
commands, malformed strings, unsupported values, missing definitions, tool
identity mismatches, and failures from either converter remain hard errors.
Generated command line numbers refer to the canonical binary script rather than
the original source; this affects diagnostics only, not module bytes or command
order.

The current schema-2 inventory at `tests/spec-v3-baseline.json` processes all 258
files and reports:

- 230 files converted directly by WABT and 28 through the official interpreter;
- zero parser/tool failures and no excluded files;
- 1,691 modules passed, 535 were compile/instantiate feature gaps, and none reached
  the harness's module-failed bucket;
- 51,765 assertions passed, 5 failed, and 6,268 were unavailable behind feature
  gaps;
- gap counts are 536 compile rejections, 15 instantiate rejections, and 6,252
  module-unavailable assertions;
- 144 files are green and 114 retain an execution or feature gap.

The five reached-but-failing assertions are all linking-state gaps: two are in
`linking`, one is in `multi-memory/linking0`, and two are in
`multi-memory/linking3`. Iteration 7 fixed the former `select` failure by treating
WABT funcref value `"0"` as the `(ref.func)` non-null wildcard rather than a raw
function index/token value. Positive indexed patterns can compare canonical
function identity through the public instance helper. The remaining linking
failures expose memory/table import-state gaps around currently unsupported
Release 3 forms and must be explained or fixed before conformance completion.

`scripts/spec3-baseline.sh` refreshes this inventory and deliberately returns the
failing suite status. Parser failures may reappear only as hard red entries if
both pinned conversion paths fail; they can never be reclassified as skips.

### Text-oracle footprint measurement

A temporary local measurement converted the fixed 28-file fallback set with the
cached official interpreter and the committed Python converter:

| Measurement | Result |
|---|---:|
| Official interpreter executable | 7,265,760 bytes |
| Committed converter source | 12,253 bytes |
| 28-file fallback elapsed time | 1.017 seconds |
| Maximum child-process RSS | 15,100 KiB |
| Canonical binary-script output | 193,337 bytes |
| JSON command output | 320,667 bytes |
| Extracted module files / bytes | 358 / 35,010 bytes |

Elapsed time used Python `time.perf_counter`; RSS used
`resource.getrusage(RUSAGE_CHILDREN)` on the current linux/amd64 host. These are
development-tool measurements, not runtime throughput or product-footprint
claims. The 7.3 MB interpreter and temporary conversion artifacts live under
`.tools`/test temporary directories and are not linked into wago. Normal WABT-
convertible files do not invoke the fallback. Building the interpreter requires
OCaml, dune, and menhir; cached verification does not rebuild it.

## Feature model

`CoreFeaturesV3` describes the mandatory Core 3.0 release scope. It includes
separate public bits for:

- extended constant expressions;
- tail calls;
- typed function references;
- GC;
- exception handling;
- multi-memory;
- memory64;
- table64.

The pre-existing `CoreFeatureSIMD` remains the admission bit for both core SIMD
and relaxed SIMD. Splitting that already executable surface would be a public
compatibility change without adding safety, so `CoreFeaturesV3` documents relaxed
SIMD through the existing bit.

`CoreFeaturesV3` is a release description, not a promise that every bit is
currently executable. `SupportedFeatures()` is the executable build/host set.
Unsupported requests return `UnsupportedFeatureError` with the exact requested
bits, admitted bits, and `GOOS/GOARCH` platform. Frontend rejection messages name
the disabled 3.0 family for tail calls, typed function references, GC, exception
handling, multi-memory, memory64, and table64.

## Mandatory area status

| Area | Decode / validate | Frontend / codegen / runtime | Product status |
|---|---|---|---|
| Extended constant expressions | Basic Release 3 numeric extension is complete on AST and byte-backed paths: `i32`/`i64` add, sub, mul, imported globals, and earlier immutable local globals. Forward, mutable, mixed-type, stack-shape, unsupported-opcode, and local-global offset forms are rejected strictly. | Complete for the basic extended-const proposal. Literal arithmetic folds at compile time. Global-dependent scalar programs are persisted and evaluated during instantiation for globals and active data/element offsets. | ✅ Executable and enabled as `CoreFeatureExtendedConstExpressions`. GC-added constant instructions remain part of the GC row, not this completed basic proposal. |
| Relaxed SIMD | Complete through `0xfd 275`, with reserved holes rejected. | Deterministic lowering is present on the documented linux/amd64 SIMD baseline. The Release 3 harness now honors official `either` result patterns; all 8 converted modules and 69 assertions pass with zero failures/skips. | ✅ Existing completed support, represented by `CoreFeatureSIMD`. |
| Tail calls | Decoder and validator understand direct, indirect, and reference tail-call forms. | linux/amd64 has internal frame-reuse milestones for local register-ABI and fixed-bank wrapper-ABI `return_call`, mixed GP/XMM `return_call_indirect` through private immutable table 0, and same-instance int-register `return_call_ref` descriptors. Public frontend admission remains disabled; imported/cross-instance direct targets, oversized wrapper signatures, mutable/imported/exported/nonzero indirect tables, wrapper/host/cross-instance reference descriptors, and arm64 remain unsupported. | 🚧 Backend milestones only; not a public product claim. |
| Typed function references | `ref.func` has the declared non-null indexed function type. Indexed references match by bounded coinductive structural equivalence across duplicate and recursive groups, including function/struct/array shapes, supers, and descriptor metadata. | The internal gate admits indexed signatures/storage, typed block immediates, `ref.null`, `call_ref`, `ref.as_non_null`, `br_on_null`, and `br_on_non_null`. Exact cross-module subtype/equivalence governs staged global/table/element imports and initializers; amd64 executes the null-control paths; public structural descriptors/codec v22 remain exact; and the reached funcref wildcard assertion is green. Remaining typed instructions, lifecycle/host/snapshot boundaries, public admission, and arm64 remain gated. | 🚧 Validator, staged storage/control execution, product representation, and internal call path advanced; no public execution claim. |
| GC | Recursive types, instructions, descriptor lowering, and a collector foundation exist. | Native frame roots, safepoint maps, opcode lowering, allocation calls, and write-barrier emission are not connected. | 🚧 Runtime foundation only; see `docs/gc.md`. |
| Exception handling | Tags, `throw`, `throw_ref`, and `try_table` syntax/validation foundations exist. | Tag imports/exports/sections and exception instructions are frontend-rejected; no unwind/runtime ABI exists. | 🚧 Syntax/validation foundation only. |
| Multi-memory | Indexed immediates and substantial syntax support exist. | Module validation still rejects multiple memories, and frontend/runtime/metadata are single-memory. | ⬜ Not executable. |
| memory64 | Limits, address typing, and instruction validation foundations exist. | Frontend rejects memory64; runtime reservations, public limits, imports/exports, and backend address paths remain 32-bit. | 🚧 Validation foundation only. |
| table64 | Limits and index typing have validator coverage. | Frontend rejects table64; runtime table sizes/indexes and codegen remain 32-bit. | 🚧 Validation foundation only. |
| Text annotations | Text-format concern; no native execution semantics are required. | No runtime work planned unless tooling integration exposes a concrete need. | Not a native runtime feature. |
| Deterministic profile | Separate optional profile, not part of the current Core 3.0 product claim. | No profile claim is made by this document. Deterministic relaxed-SIMD lowering does not by itself implement the full optional deterministic profile. | Optional/separate. |

## Extended constant-expression implementation

The completed basic extension follows Release 3 semantics:

- constant expressions admit `i32.add/sub/mul` and `i64.add/sub/mul`;
- global initializers may read imported immutable globals and earlier immutable
  local globals;
- table/data/element offset contexts remain restricted to the globals permitted
  by their validation context;
- integer operations wrap at 32 or 64 bits;
- non-constant instructions, mutable globals, forward globals, unavailable
  globals, operand type mismatches, result mismatches, stack underflow, missing
  `end`, and trailing bytes fail closed.

Pure literal expressions are folded during compilation. Expressions depending on
runtime import values are stored as validated Wasm expression bytes. The same
small strict stack evaluator is used to validate persisted metadata and to
evaluate it during instantiation. This keeps execution out of the invocation hot
path and avoids introducing a general interpreter tier.

### `.wago` codec impact

The compiled codec is now version 22. Version 21 introduced deferred scalar
initializer/offset programs and strict extended-constant-expression validation.
Version 22 retains those records and adds:

- flattened public `DefinedTypeDescriptor` graphs with recursive-group boundaries,
  supers, descriptor metadata, function signatures, struct fields, and arrays;
- exact `ValueTypeDescriptor` records for nullability, exactness, abstract heaps,
  and indexed heaps;
- function signature references into the type graph;
- a deduplicated value-type pool referenced by globals, tables, and element
  segments, including imported declarations and inspection metadata;
- a full-width `CoreFeatures` required-feature word, so post-bit-7 families such
  as typed function references cannot be truncated; and
- strict bounds/kind/ABI consistency validation for type indexes, pool indexes,
  recursive layouts, and malformed old/new records.

Version 21 and older blobs are rejected explicitly. This intentional format break
prevents the old generic `ValFuncRef`/`ValExternRef` records from being mistaken
for exact indexed identity. Extended-const source syntax remains compiled into
initializer metadata rather than re-decoded from the original Wasm expression.
Typed-reference artifacts still fail public load because the executable feature
bit is not advertised; codec support is representation work, not admission.

### Footprint and allocation measurement

A synthetic linux/amd64 module used by the focused execution test contains an
imported `i32`, two dependent extended global initializers, one extended active
data offset, and two exported functions. A temporary measurement test (not
committed) reported:

| Measurement | Result |
|---|---:|
| Wasm module size | 106 bytes |
| Historical `.wago` v21 blob size | 434 bytes |
| Deferred expression payload | 18 bytes |
| Historical `unsafe.Sizeof(GlobalDef{})` | 80 bytes |
| `unsafe.Sizeof(OffsetInit{})` | 40 bytes |
| Instantiate allocations, extended form | 12 allocations/run |
| Instantiate allocations, equivalent literal metadata | 11 allocations/run |

Allocation counts used `testing.AllocsPerRun(100, ...)`. This is a focused
engineering measurement, not a throughput benchmark. It indicates one additional
allocation in the synthetic instantiation path. Invocation code and hot native
memory/call paths are unchanged. Deferred byte storage is bounded by input module
size; evaluator stack growth is bounded by validated expression operand depth and
starts with capacity for four values.

## Tail-call backend milestones

### Direct `return_call`

On linux/amd64, a validated local direct target can tail-jump when both caller and
callee fit the existing internal register ABI. The lowering:

1. commits dirty value-pinned globals to their cells;
2. stages integer and floating-point arguments with parallel GP/XMM moves;
3. patches an `add rsp, frameSize` at each tail site; and
4. emits a relocated `jmp` to the target's internal entry instead of a `call`.

The original adapter remains below the root internal activation, so the final
callee returns results through the existing one-result, two-integer-result, or
single-float adapter path. Focused tests execute one million recursive tail steps,
two integer results, an `f64` argument/result, and callee trap propagation.
Iteration 7 adds a local same-instance wrapper path. Wrapper-only callers marshal
up to 16 ABI slots into `[linMem-256, linMem-128)`, preserve the current wrapper's
result pointer, commit module/global state, release the current frame, and jump to
the target's offset-0 entry. The bank is fixed per `JobMemory`, reused at every
step, and adds no recursion-dependent allocation. A `(i32, funcref) -> i32`
countdown completes one million steps. Imported/cross-instance targets, a
register-ABI caller targeting a wrapper-only callee, and signatures above 16 slots
still fail explicitly. The public frontend still rejects `return_call`, so source
modules cannot accidentally claim broader support.

### Indirect `return_call_indirect`

The indirect milestone remains restricted to table index 0 when module analysis
proves a private, immutable, local funcref table. Iteration 4 removes the former
integer-only restriction: signatures may now stage independent GP and XMM banks
whenever they fit the bounded register ABI. The lowering preserves ordinary
`call_indirect` parity for:

- table bounds;
- null entries; and
- canonical structural signature IDs.

After those checks, the code pointer is stored in bounded basedata scratch,
integer and floating-point arguments are staged, the current frame is released,
and the pointer is reloaded into `RSI` for an indirect jump. A million-step mixed
`i32`/`f64` table-recursive test returns the exact `f64` bits. Exporting the table
still makes compilation fail closed. Mutable/imported tables, nonzero tables,
reference/vector signatures, cross-instance descriptors, and host funcrefs are
not yet tail-safe and remain rejected.

### Typed `call_ref` and `return_call_ref`

Funcref values already use immutable 32-byte descriptor pointers containing a
code pointer, canonical structural signature ID, home linear-memory pointer, and
identity slot. Iteration 4 consumes that representation directly:

- `call_ref` checks null and canonical signature identity, then uses either the
  tagged same-instance internal register entry or the existing wrapper/home-aware
  call path. Untagged wrapper descriptors can therefore retain ordinary non-tail
  host or cross-instance context switching.
- `return_call_ref` currently accepts only int-register signatures whose
  descriptor is tagged as an internal entry and whose untagged home pointer is
  the current instance. It stages arguments, releases the frame, and jumps to the
  descriptor code pointer.
- Null references retain the existing indirect-null trap class and wrong
  signatures retain the canonical-signature trap class. Wrapper, host, and
  cross-instance descriptors take a dedicated fail-closed runtime trap because
  their adapters are not yet proven tail-safe.

A million-step `ref.func` countdown passes through `return_call_ref` without
stack growth. This is still an internal backend beachhead: typed-reference and
tail-call public gates remain disabled, and the official suite therefore does not
exercise these paths yet.

### Iteration 5 typed-reference validation and admission

Iteration 5 closes three prerequisites without widening the public product claim:

1. `ref.func` now pushes `(ref <declared-function-type>)`, non-null, instead of
   the abstract `funcref` supertype. A shared function-index helper covers imports
   and locals. This preserves Release 2 behavior because every indexed function
   reference remains a subtype of `funcref`, while Core 3.0 indexed locals,
   globals, calls, and returns receive the required precision.
2. The validator now compares indexed heap types by a bounded coinductive walk of
   reachable type pairs. Duplicate simple definitions and isomorphic recursive
   graphs match; parameter/result, field mutability/storage, explicit supers,
   nullability/exactness, and descriptor metadata remain part of the comparison.
   Deliberately different definitions still fail with `ErrTypeMismatch`. The same
   equivalence is used by ordinary reference subtyping, exact descriptor checks,
   branch shape checks, and tail-result matching.
3. A new frontend-only `TypedFunctionReferences` gate admits resolved indexed and
   non-null function signatures, recursive function-only groups, typed block
   immediates, typed `ref.null`, and `call_ref`. The public runtime config maps the
   existing feature bit to this gate, but `RuntimeConfig.Validate` still rejects
   that bit because `SupportedFeatures()` does not advertise it. Thus an indexed
   descriptor reaches the proven amd64 `call_ref` lowering in focused tests while
   ordinary `Compile` remains fail-closed.

Iteration 7 widens only the internal staged path. Typed globals, tables, and
elements now compile and instantiate against exact descriptors; imports compare
bounded cross-module structural subtype/equivalence instead of raw indexes or
`ValFuncRef`; amd64 lowers `ref.as_non_null`, `br_on_null`, and
`br_on_non_null`; and a dedicated null-reference trap preserves the proposal's
trap class. The public feature remains disabled. GC-only casts/tests, remaining
reference instructions, host/lifecycle/snapshot boundaries, general tails, and
arm64 are still gated.

The official module/skip totals remain unchanged because the public gate is still
disabled. Harness identity correction moves one reached `select` assertion from
failed to passed. Diagnostics did move materially: the leading `call_ref`, `ref_*`,
`type-equivalence`, and `type-rec` modules that previously stopped on messages
such as `funcref is not ref type N` now pass validation and stop at explicit
`typed-function-references disabled` or product-storage gates. This preserves the
schema-2 baseline while proving that validator mismatches were removed rather
than reclassified as skips.

### Focused code/stack measurements

Temporary opt-in `ModuleStats` measurements (not committed as tests) reported:

| Synthetic function | Module code | Function code | Frame | Max spill slots | Call sites / trap stubs |
|---|---:|---:|---:|---:|---:|
| Direct million-step countdown (iteration 2) | 142 bytes | 142 bytes | 40 bytes | 0 | 1 direct tail |
| Integer indirect table countdown (iteration 2) | 351 bytes total | 285 bytes | 40 bytes | 0 | 1 indirect tail |
| Mixed `i32`/`f64` indirect countdown | 285 bytes | 285 bytes | 56 bytes | 0 | 1 indirect tail / 2 traps |
| Local descriptor `call_ref` caller | 501 bytes total | 438 bytes | 72 bytes | 3 | 1 ref call / 3 traps |
| Local descriptor `return_call_ref` countdown | 334 bytes | 334 bytes | 40 bytes | 0 | 1 ref tail / 4 traps |

All three tail recursion tests complete 1,000,000 steps with fixed real frames;
the larger mixed frame reflects local/ABI layout rather than recursion depth.
These are code-size/stack correctness measurements, not throughput benchmarks.
The non-tail `call_ref` path adds no process-global cache and uses the existing
bounded descriptor and spill storage. No public invocation hot path changes
because both feature gates remain off.

Iteration 5 changed validation and support admission only; it emitted no new
runtime instruction sequence. A temporary `testing.AllocsPerRun(100, ...)`
measurement (test removed after use) validated two small modules containing
non-identical but equivalent indexed types:

| Validation shape | Allocations / `ValidateModule` |
|---|---:|
| Duplicate non-recursive function types | 12 |
| Separate self-recursive function groups | 12 |

These totals include the validator's existing control/type bookkeeping and are
not a before/after throughput claim. The new equivalence walk allocates one
pair-state map only when raw indexed identities differ; its entries are bounded
by the reachable Cartesian set of module-defined type pairs and are discarded at
validation completion. Equal raw indexes and all invocation/runtime hot paths do
not enter this walk. The staged indexed `call_ref` execution test uses the same
machine code path and descriptor layout measured in iteration 4.

### Iteration 6 product representation and measurements

Iteration 6 introduces a public type model independent of internal decoder
packages. `DefinedTypeDescriptor` flattens module type definitions while retaining
recursive-group membership, supers, descriptor metadata, function/struct/array
shape, field storage, and mutability. `ValueTypeDescriptor` retains nullability,
exactness, abstract heap kinds, and indexed heap identity. Function metadata
references a declared type definition; globals, tables, and elements reference a
deduplicated compiled value-type pool. Public inspection returns descriptors by
value rather than exposing pool indexes.

Codec v22 serializes that graph and pool, rejects v21 and older layouts, and stores
the complete 64-bit feature mask. Indexed function references now derive the
legacy runtime category as `ValFuncRef`; they no longer fall through the one-byte
value-code conversion and appear as `i32`. Malformed recursive indexes, pool
indexes, non-function signature indexes, invalid heap kinds, and ABI/category
mismatches fail closed. A codec-v22 artifact that requires typed references still
fails public load because `SupportedFeatures()` omits that bit.

Temporary tests removed after measurement reported:

| Measurement | Result |
|---|---:|
| `unsafe.Sizeof(Compiled{})` | 696 bytes |
| `unsafe.Sizeof(FuncSig{})` | 56 bytes |
| `unsafe.Sizeof(GlobalDef{})` | 88 bytes |
| `unsafe.Sizeof(GlobalImportDef{})` | 48 bytes |
| `unsafe.Sizeof(tableDef{})` | 48 bytes |
| `unsafe.Sizeof(ElemInit{})` | 80 bytes |
| `unsafe.Sizeof(HostFuncRef{})` | 120 bytes |
| Scalar add codec-v22 blob / type counts | 193 bytes / 1 defined / 0 pooled |
| Reference-global codec-v22 blob / type counts | 659 bytes / 2 defined / 1 pooled |
| Two-table codec-v22 blob / type counts | 1,218 bytes / 1 defined / 1 pooled |

The previous committed layout assertions were 632 bytes for `Compiled`, 48 bytes
for `FuncSig`, 80 bytes for `GlobalDef`, 40 bytes for `tableDef`, and 112 bytes for
`HostFuncRef`. The increases are fixed struct metadata: one module type slice, one
value-type-pool slice, compact type indexes/flags, and a widened feature word.
Runtime table entries, global cells, native descriptors, instance arena sizing,
and invocation machine code are unchanged. The pool is deduplicated by a bounded
linear compile-time scan and is bounded by distinct declared storage types; no
process-global cache or invocation-path allocation was added. Blob measurements
are representative fixtures, not before/after throughput claims.

### Iteration 7 staged storage, null control, and wrapper-tail measurements

The product metadata is now consumed at staged runtime boundaries. Reference
owners retain their exact descriptor and containing graph. Immutable global
imports are covariant; mutable globals and tables are invariant; active element
segments and `ref.func` payloads must subtype their destination. The cross-module
coinductive pair map is bounded by reachable type pairs and exists only during
validation/instantiation. Runtime cells and table entries remain 8 and 32 bytes.
Snapshots remain fail-closed.

amd64 now lowers `ref.as_non_null`, `br_on_null`, and `br_on_non_null` over the
existing one-slot reference representation. `ref.as_non_null` uses the new
`TrapNullReference`; branch tests cover null/non-null paths and identity
preservation. WABT's funcref value `"0"` is handled as its documented text-pattern
wildcard representation, while `Instance.FuncRefMatchesFunction` provides
canonical identity comparison for positive indexed patterns without exposing
descriptor addresses.

The local wrapper direct-tail path reserves a fixed 16-slot/128-byte basedata
bank. Consequently `abi.BasedataSize` grows from 128 to 256 bytes on every target;
no table/global/descriptor entry or `Instance` arena layout changes. The bank is
shared by all local wrapper tail steps in one instance and does not grow with
recursion.

Temporary tests removed after measurement reported:

| Measurement | Result |
|---|---:|
| `unsafe.Sizeof(Global{})` / `globalOwner{}` | 40 / 112 bytes |
| `unsafe.Sizeof(Table{})` / `tableOwner{}` | 64 / 104 bytes |
| Canonical basedata / wrapper-tail bank | 256 / 128 bytes |
| Staged local typed-table instantiate+close | 5 allocations/run |
| Wrapper-tail synthetic code / frame / max spills | 239 / 56 bytes / 2 slots |
| Wrapper-tail recursion | 1,000,000 steps, fixed frame, result 7 |

A short three-sample, 200 ms codec-v22 watchpoint measured scalar marshal at
840-854 ns/op (528 B, 14 allocs), structural marshal at 2.11-2.20 us/op
(1,344 B, 21 allocs), scalar unmarshal at 1.73-1.81 us/op (1,601-1,602 B,
27 allocs), and structural unmarshal at 4.11-4.22 us/op (3,164-3,165 B,
52 allocs). These are current-host watchpoints, not a before/after throughput
claim. Invocation hot paths are unchanged except when executing the newly staged
instructions or internal tail path.

## Iteration commits

Iteration 1 contained:

1. `f98f89fc` — pin the official WebAssembly 3.0 suite and make Release 3 skips
   fail the harness.
2. `298a20c7` — add the mandatory 3.0 feature model, platform admission metadata,
   and explicit frontend family errors.
3. `d768006c` — implement and execute basic extended constant expressions,
   including `.wago` v21 persistence.
4. `ad4bbe79` — record the first implementation ledger.

Iteration 2 contained exactly three code/test commits and one documentation
commit:

1. `69ea811a` — bootstrap checksum-pinned WABT 1.0.41 and commit the 258-file
   machine-readable red inventory.
2. `1a1dcec9` — implement local direct `return_call` frame reuse on amd64.
3. `0603ab8c` — implement private-local-table `return_call_indirect` frame reuse,
   trap parity, and explicit arm64 rejection coverage.
4. `fa8f1b1a` — record the second implementation iteration.

Iteration 3 contained exactly three code/test commits and one documentation
commit:

1. `ce608c61` — pin and bootstrap the official Release 3 reference interpreter.
2. `3453490d` — convert all 28 WABT-rejected files through the official binary
   script path and refresh the zero-parser-failure inventory.
3. `8fbab308` — implement official alternative result matching and close all 32
   relaxed-SIMD harness failures.
4. `dff8134e` — record the third implementation iteration.

Iteration 4 contained exactly three code/test commits and one documentation
commit:

1. `2be4c4b9` — stage mixed GP/XMM arguments for private-table
   `return_call_indirect` and prove one million recursive steps.
2. `7f177947` — execute descriptor-pointer `call_ref` with null/signature checks
   and internal or wrapper/context-aware call paths.
3. `d9886047` — tail-jump through same-instance local typed references, add a
   fail-closed unsupported-context trap, and prove one million recursive steps.
4. `47ed7b47` — record the fourth implementation iteration.

Iteration 5 contains exactly three code/test commits and this documentation
commit:

1. `9bc4bdcc` — type `ref.func` by its declared indexed function type on both
   validation paths.
2. `e7d5b0e5` — match duplicate and recursive indexed references by bounded
   structural equivalence while preserving negative mismatches.
3. `c7995373` — add a staged typed-reference frontend gate and route indexed
   signatures to the existing amd64 `call_ref` lowering without public admission.

Iteration 6 contains exactly three code/test commits and this documentation
commit:

1. `3ebc2315` — add public structural value/heap/defined-type descriptors and
   strict recursive-index conversion tests.
2. `cae9b440` — persist declared function-type identity and the recursive type
   graph in codec v22, widen feature bits, and expose exact signature metadata.
3. `4a285b2c` — add the deduplicated exact storage-type pool for globals, tables,
   elements, imports/exports, inspection, and generated facade API.

Iteration 7 contains exactly three code/test commits and this documentation
commit:

1. `a2662141` — enforce exact staged global/table/element compatibility with
   bounded cross-module structural matching and normal compile/instantiate tests.
2. `5834a1f4` — lower typed-reference null control on amd64, add the null trap,
   and make harness funcref matching token-independent.
3. `92b28c12` — tail-enter local wrapper contexts through a fixed 16-slot
   basedata argument bank and prove one million recursive steps.

## Validation performed

Commands were run from the repository root on linux/amd64.

| Command | Result |
|---|---|
| `go test ./src/wago -run 'TestStagedTypedStorageExactImports\|TestTypedStorageMetadataRejectsIdentityCollapse\|TestPublicFuncrefEgressReturnsStableOpaqueToken\|TestSpecFuncrefResultMatchesCanonicalFunctionIdentity' -count=1` | PASS: exact staged storage/import matching and canonical funcref result handling are covered. |
| `go test ./src/core/compiler/frontend -run TestTypedFunctionReferenceGateRoutesCallRef -count=1` | PASS: staged call/storage/control admission works while the default public gate stays closed. |
| `go test ./src/core/compiler/backend/railshot/amd64 -run 'TestTypedRefControlNullBranches\|TestTypedRefAsNonNullPreservesIdentityAndTraps\|TestReturnCallDirect' -count=1` | PASS: null-control lowering and both register/wrapper direct-tail paths execute, including million-step recursion. |
| `go test ./src/core/runtime -run 'TestBasedataOffsetsMatchWARP\|TestJobMemory' -count=1` | PASS: the 256-byte canonical basedata and fixed tail bank are layout-checked. |
| `go generate ./...` | PASS; `wago.go` remained generated-clean. Log `.validation/iteration7-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD; log `.validation/iteration7-go-test.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; log `.validation/iteration7-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o .validation/wago-arm64.test ./src/wago && rm -f .validation/wago-arm64.test` | PASS. Build evidence only; arm64 still does not advertise typed references or tail calls. |
| `scripts/bootstrap-wabt.sh --verify` | PASS: checksum-pinned `wast2json 1.0.41`. |
| `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: official `wasm 3.0.0 reference interpreter` at `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. |
| codec-v22 benchmark command with `-benchtime=200ms -count=3` | PASS: current scalar/structural marshal/unmarshal watchpoints are recorded above; log `.validation/iteration7-codec-bench.log`. |
| temporary `TestIteration7Measure` and `TestIteration7TailMeasure`, then file removal | PASS: owner/basedata/allocation/code/frame measurements are recorded above; logs `.validation/iteration7-measure.log` and `.validation/iteration7-tail-measure.log`. |
| `make spec3` | FAIL as required with zero parser failures and 28 official-interpreter fallbacks: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268. `select` is now 124/124 green. |
| `python3 scripts/spec3-baseline.py .validation/spec3-iteration7.log .validation/spec3-iteration7.json --exit-code 2 && cmp tests/spec-v3-baseline.json .validation/spec3-iteration7.json` | PASS: the refreshed schema-2 committed inventory reproduces byte-for-byte. |

The larger skipped totals relative to the historical WABT-only baseline remain
intentional: 28 previously unparsed files contribute their real feature gaps.
Iteration 7 changes one reached assertion and no module/skip counts because public
typed-reference and tail-call gates remain disabled. The existing 1.0/2.0
external corpora were not rerun as separate commands; repository-wide and
guard-page suites passed.

## Architecture policy

The primary claim remains linux/amd64. Unsupported 3.0 feature bits are rejected
before backend execution with an error that includes the current `GOOS/GOARCH`.
This prevents arm64 from silently accepting tail calls, typed function references,
GC, exceptions, multi-memory, memory64, or table64.

Extended constant expressions, type-equivalence validation, exact staged storage
matching, and the public/codec descriptor model are architecture-neutral. The
canonical basedata layout is now 256 bytes on amd64 and arm64, including an unused
on-arm64 128-byte wrapper-tail bank so offsets cannot drift by target. Executable
`call_ref`, null-control instructions, and tail-call lowering remain amd64-only and
hidden behind public unsupported family gates. The unsupported-tail-context trap
is an internal fail-closed boundary, not an advertised Wasm semantic or arm64
feature.
The arm64 cross-compiled test binary includes an architecture-specific assertion
that tail calls are not advertised and that a request reports `linux/arm64` (or
the actual arm64 GOOS) in `UnsupportedFeatureError`. Native arm64 execution was
not run, so the final 3.0 completion gate still requires either parity evidence
or the documented platform restriction for each executable family.

## Dependency order and risks

Recommended dependency order:

1. make the Release 3 oracle reproducible and obtain a measured red baseline;
2. tail calls, beginning with direct calls and exact frame/ABI invariants;
3. typed function references and `call_ref`, sharing call ABI work;
4. multi-memory metadata/runtime directories before memory64 widens addresses;
5. memory64 and table64 with explicit bounded reservation policies;
6. exception handling with a boring unwind/trap boundary;
7. GC opcode lowering, safepoints, native roots, and barriers on top of typed refs
   and exception-safe call/runtime boundaries.

Major risks:

- the zero-parser-failure oracle now depends on development hosts having OCaml,
  dune, and menhir available for the pinned official interpreter build; the cached
  tool is revision-stamped, and any future binary-script grammar change must fail
  the strict converter rather than silently dropping commands;
- codec v22 intentionally invalidates v21 and older caches; its structural graph
  and value-type pool are bounded by decoded module declarations, but cache-size
  planning must use v22 measurements rather than historical v20/v21 numbers;
- multi-memory changes instance metadata, import/export APIs, snapshots, and every
  memory opcode hot path;
- memory64 can turn existing 32-bit arithmetic assumptions into overflow or
  reservation bugs;
- local wrapper direct tails are bounded, but imported/cross-instance direct and
  mutable/cross-instance indirect/reference tails still need a context entry that
  removes the current activation; the iteration-4 unsupported-context trap must
  disappear from every publicly admitted valid path before the tail-call feature
  can be enabled;
- typed refs, exceptions, and GC all interact with native frame roots and call
  boundaries; validator, codec, staged storage imports, and harness result matching
  are now precise, but host signatures, dynamic table operations, snapshots,
  lifecycle retention, and remaining instructions must consume exact descriptors
  before admission;
- GC collector code is meaningful but must not be mistaken for executable WasmGC
  until safepoint maps and barriers are connected;
- arm64 must remain fail-closed for every family that lacks native execution tests.

## Next bounded implementation slice

The next recursive iteration should again make exactly three atomic code/test
commits followed by one documentation commit:

1. **Typed-reference lifecycle and dynamic storage.** Carry exact indexed types
   through typed host/function params/results, `table.get/set/grow/fill/copy/init`,
   global mutation, imported aliases, retention, and close order. Keep snapshots
   explicitly rejected and exercise the official typed-reference files under the
   internal gate without enabling the public bit.
2. **Imported and dynamic tail contexts.** Extend the bounded tail entry to
   imported host/cross-instance direct targets, then reuse it for mutable/imported
   table descriptors and wrapper/cross-instance `return_call_ref`. Preserve trap,
   result, host re-entry, module/global, and reference-root semantics; keep arm64
   fail-closed.
3. **Multi-memory runtime directory foundation.** Replace singleton compiled/
   instance memory metadata with bounded indexed declarations/imports/exports and
   exact lifecycle/inspection/codec representation, then execute a small indexed
   `memory.size`/load/store slice on amd64 without regressing memory-0 hot paths.
4. **Documentation commit.** Refresh exact suite/parser totals, lifecycle/tail/
   multi-memory coverage, measurements/caveats, product/platform gates, and the
   following bounded slice.

## Completion gate

WebAssembly 3.0 is not complete. Completion still requires every mandatory area
to decode, validate, compile, instantiate, execute, round-trip through product
metadata/lifecycle rules, and pass the pinned official Release 3 suite with zero
unexplained failures or feature skips on linux/amd64, while preserving 1.0/2.0,
no-cgo operation, bounded memory, and hot-path performance. Arm64 must either
reach parity or remain explicitly gated and documented.
