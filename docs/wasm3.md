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
function identity through the public instance helper. Iteration 13 decodes the
compact import groups that previously stopped the reached multi-memory consumers,
and a bounded staged runner proves the official `memory_grow`,
`memory_size_import`, and safe `linking0`-`3` shapes. The public feature gates
remain closed, so the schema-2 inventory and its five reached failures are still
byte-for-byte unchanged; the failures are no longer attributed to an unknown
compact binary grammar. Iteration 14 adds the supplementary
`tests/spec-v3-staged-multi-memory.json` began as a schema-1 safe delta. Iteration
15 upgrades it to a schema-2 complete family matrix covering all 41 pinned multi-
memory files plus `simd/simd_memory-multi`. Iteration 17 closes the three former
shared-basedata file gates through a finite owner/tenant proof. The matrix is now
fully gap-free at 913 exact commands, 79 instantiated modules, 771 successful
assertions, 4 invalid, 22 unlinkable, and 20 uninstantiable cases, with zero feature
rejects, blocked commands, or unexpected compile/link/assertion gaps. This remains
staged evidence, not a public-suite skip reclassification.

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
| Tail calls | Decoder and validator understand direct, indirect, and reference tail-call forms. Tail results use covariant reference matching, while invalid narrowing remains rejected. Separate compile-only frontend bits admit bounded direct/indirect and typed-tail slices. | linux/amd64 has local register/wrapper `return_call`, tail-position host imports, per-table finite immutable-local `return_call_indirect` proofs, tagged same-instance internal/scalar-wrapper `return_call_ref`, and retained cross-instance root/nested transfers. Exact pinned accounting is: `return_call` 47 commands / 3 modules / 33 assertions / 11 invalid, all green; `return_call_indirect` 79 commands / 3 modules / 49 assertions / 16 invalid / 11 malformed, all green; `return_call_ref` 51 commands / 4 modules / 35 assertions / 11 invalid / one reference-result gate. Cross-instance direct, mutable/imported/exported/host-descriptor indirect tables, broader typed-tail results, snapshots, public admission, and arm64 remain fail-closed. | 🚧 Complete direct/indirect staged files plus exact reference-tail accounting; not a public product claim. |
| Typed function references | `ref.func` has the declared non-null indexed function type. Indexed references match by bounded coinductive structural equivalence across duplicate and recursive groups, including function/struct/array shapes, supers, and descriptor metadata. Typed/tail opcodes contribute exact required-feature bits. | The internal gate admits indexed signatures/storage, typed block immediates, `ref.null`, `call_ref`, null control, and bounded typed-tail contexts. Exact cross-module subtype/equivalence governs staged storage/imports; public/host/global boundaries enforce exact type/nullability. Native descriptors use bounded 64-bit SHA-256-derived structural keys. Distinct cross-instance `InstanceExport` producers are retained transactionally; their int-register wrapper descriptors carry a separate immutable context tag used by root or nested `return_call_ref`. Shifted types survive producer logical close. Null/wrong-key/host contexts trap without corrupting later calls. Typed/tail snapshots still reject before imports or state mutation. Persisted live reference state, broader tail contexts, remaining GC/reference instructions, public admission, and arm64 execution parity remain gated. | 🚧 Validator, staged storage/control/table execution, exact boundaries, bounded lifecycle, product representation, retained cross-instance calls, and root/nested typed-tail transfers are proven; no public execution claim. |
| GC | Recursive types, instructions, descriptor lowering, and a collector foundation exist. | Native frame roots, safepoint maps, opcode lowering, allocation calls, and write-barrier emission are not connected. | 🚧 Runtime foundation only; see `docs/gc.md`. |
| Exception handling | Tags, `throw`, `throw_ref`, and `try_table` syntax/validation foundations exist. | Tag imports/exports/sections and exception instructions are frontend-rejected; no unwind/runtime ABI exists. | 🚧 Syntax/validation foundation only. |
| Multi-memory | Indexed immediates and compact imports decode/validate strictly on AST and byte-backed paths; default Release 2 admission still rejects them explicitly. | Exact product directories, policy accounting, duplicate aliases, codec v25, every indexed scalar/SIMD/bulk/data operation, snapshot-v3 owned-local state, and bounded shared-memory co-tenants are staged on linux/amd64 explicit bounds. A finite compile-only proof excludes tables, globals, passive/reference state, host calls, and native imported calls; admitted owners/tenants serialize fixed basedata images through the memory lifecycle mutex and refresh exact native directories. The complete 42-file matrix is gap-free at 913 commands, 79 modules, 771 assertions, 4 invalid, 22 unlinkable, and 20 uninstantiable cases, with zero feature rejects or blocked commands. Imported/shared/registered snapshots, guard mode, broader shared-state composition, public admission, and arm64 remain gated. | 🚧 Complete official family accounting and bounded internal execution; not a public product claim. |
| memory64 | Limits, i64 address typing, 64-bit memarg offsets, and operation validation are present. The staged support pass admits size/grow plus integer and float scalar memory operations and rejects SIMD/bulk/data families explicitly. | One linux/amd64 explicit-bounds local path accepts exactly one non-shared memory with an explicit max <=65,535 pages. Exact 64-bit metadata/codec limits and policy accounting round-trip; `memory.size/grow` use i64; all 19 integer and 4 float scalar operations check carry on address+offset+width, preserve integer extension/exact-width stores and float NaN payload bits, and trap at the exact end boundary. The float matrix is 173 Wasm bytes / 972 code bytes and f64 load is 35.68-36.56 ns/op allocation-free. Exact supplementary accounting covers 807 commands across six pinned address/alignment/float/load/grow/trap files: 1 module / 8 assertions green, 42 explicit gates / 614 blocked dependents / 83 invalid / 59 malformed, with zero hidden gaps. Imports, shared/data/multi-memory, unbounded/excessive reservations, SIMD/bulk, guard mode, public admission, and arm64 reject before allocation/execution. | 🚧 Bounded scalar execution/product slice plus exact official-family accounting; broader lifecycle remains. |
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

The compiled codec is now version 25. Version 21 introduced deferred scalar
initializer/offset programs. Version 22 added exact structural type graphs,
value-type pools, function signature references, full-width required-feature
bits, and strict recursive/index/ABI validation. Version 23 added exact indexed
memory declarations/imports/exports and the direct memory-0 execution cache.
Version 24 added the exact target memory index for every active data segment.
Version 25 replaces persisted compact 32-bit function signature discriminators
with 64-bit SHA-256-derived structural keys used by native descriptors and
indirect/reference call checks.

Version 24 and older blobs are rejected explicitly: interpreting an old 32-bit ID
as a native key or dropping an active-data memory index would be unsafe. Exact
count bounds are checked before allocating the widened key slice. Extended-const
source syntax remains compiled into initializer metadata rather than re-decoded
from the original Wasm expression. Typed-reference, multi-memory, and memory64
artifacts still fail public load because their executable feature bits are not
advertised; codec support is representation work,
not admission. Iteration 12 fixes required-feature accounting for `call_ref`, typed
null control, and all tail-call opcodes. Iteration 13 changes no codec version:
compact import grouping is source-binary syntax and expands into the existing exact
import metadata, while the typed-tail admission bits remain compile-only in the
non-serialized code-cache sidecar. Public codec load therefore cannot accidentally
inherit either staged execution gate.

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

The indirect milestone began at table index 0. Iteration 17 generalizes the proof
independently across the finite set of local tables: each admitted table must be
unexported, unimported, never mutated, and populated only by same-module functions.
Function imports elsewhere in the module are allowed but cannot enter those tables.
Iteration 4 removed the former integer-only argument restriction; signatures may
stage independent GP and XMM banks whenever they fit the bounded register ABI. The lowering preserves ordinary
`call_indirect` parity for:

- table bounds;
- null entries; and
- canonical 64-bit structural signature keys.

After those checks, the code pointer is stored in bounded basedata scratch,
integer and floating-point arguments are staged, the current frame is released,
and the pointer is reloaded for an indirect jump. Scalar staged tail modules use
internal GP/XMM descriptors; wrapper-only mixed-result callers marshal through the
fixed 16-slot argument bank and jump to offset-0. A million-step mixed `i32`/`f64`
table-recursive test returns the exact `f64` bits, and the pinned multi-table file
passes all 49 actions. Exporting or mutating a table still makes compilation fail
closed. Imported tables, reference/vector signatures, cross-instance descriptors,
and host funcrefs are not yet tail-safe and remain rejected.

### Typed `call_ref` and `return_call_ref`

Funcref values use immutable 32-byte descriptor pointers containing a code
pointer, canonical 64-bit structural signature key, home linear-memory pointer,
and identity slot. Iteration 4 consumes that representation directly:

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

### Iteration 8 structural signature and boundary coverage

Iteration 8 closes three typed-reference identity/boundary holes without widening
the public feature claim:

1. Runtime function signature IDs no longer call the one-byte value encoder for
   indexed references and accidentally mix zero for every unencodable type. The
   module-aware path hashes reachable function/struct/array definitions,
   nullability/exactness, subtype and descriptor metadata, fields, nested indexed
   signatures, and recursive back-edges without including raw module type indexes.
   Equivalent graphs shifted to different indexes, shared versus duplicated
   equivalent leaves, and self-recursive graphs produce the same ID; mismatched
   nested signatures do not. Non-indexed signatures retain the existing ID
   algorithm, so ordinary Release 1/2 descriptor IDs and codec-v22 artifacts are
   unchanged. Immutable-table analysis on both backends now uses the module-aware
   ID. At that iteration the representation remained a compact 32-bit runtime
   discriminator; iteration 11 supersedes it with the bounded 64-bit native key.
   Full descriptor comparisons remain authoritative at product/storage boundaries.
2. Public `Call` and low-level `Invoke` now obtain a non-allocating exact signature
   view from immutable compiled metadata. Non-null funcref tokens resolve to their
   canonical owner and declared function type; ingress requires structural
   subtyping and rejects null for non-null parameters. Result descriptors undergo
   the same proof before token issuance. Invalid/foreign tokens keep invalid-token
   diagnostics, while valid wrong-type tokens fail as exact structural mismatches.
   Staged tests cover nullable and non-null signatures, stable identity round trips,
   and incompatible indexed functions.
3. Synchronous host import translation applies the same checks in both directions.
   Wasm-to-host arguments are tokenized only after their native descriptor matches
   the import's exact type. Host-to-Wasm result tokens are resolved only when their
   canonical function type is compatible and nullability permits the value. A
   staged amd64 host echo accepts the matching indexed function, rejects a valid
   token of a different indexed signature, and preserves nullable null.

These paths add no process-global cache and do not change descriptor/table/global
cell sizes. A temporary `testing.AllocsPerRun(1000, ...)` test, removed after
capture, measured 0 allocations/run for staged exact typed `Invoke` and 3 for the
high-level typed `Call`. The latter includes the existing high-level result/value
shape; the exact indexed-signature view aliases compiled metadata rather than
allocating a descriptor copy. A three-sample 200 ms codec-v22 watchpoint on the
same host measured scalar marshal at 961-1,034 ns/op (528 B, 14 allocs), structural
marshal at 2.28-2.30 us/op (1,344 B, 21 allocs), scalar unmarshal at 1.86-1.89 us/op
(1,601-1,602 B, 27 allocs), and structural unmarshal at 4.21-4.31 us/op (3,164 B,
52 allocs). These are current-host watchpoints, not before/after throughput claims.

Public typed-function-reference admission remains disabled. Dynamic typed table/
global mutation and alias lifecycle still need dedicated exact-type/retention
proofs, snapshots remain rejected, imported/dynamic typed tail contexts remain
unsupported, and arm64 receives no execution claim from architecture-neutral
identity/boundary code.

### Iteration 9 mutable globals, bounded lifecycle, and multi-memory validation

Iteration 9 advances two typed-reference product boundaries and opens a strict
multi-memory validator path without widening either public feature claim:

1. Mutable indexed funcref globals no longer accept every token in the compact
   `ValFuncRef` ABI category. `Instance.SetGlobalValue`, `Global.SetValue`,
   `GlobalValue`, and `Global.GetValue` resolve the token or descriptor to its
   canonical owner, compare full structural function types across module graphs,
   and enforce nullability. Equivalent types at shifted indexes pass; a valid
   token of a different indexed signature fails before mutation; rejected writes
   leave the cell unchanged. The exact view aliases immutable compiled/owner
   metadata and adds no process-global registry or descriptor copy.
2. A shared funcref table/global now releases a logically closed producer as soon
   as a completed overwrite removes its final descriptor. Guest calls that import
   funcref storage reconcile owner roots only after native execution, host replay,
   and reference-result tokenization. Thus successful `table.set`/fill/copy/init/
   grow or `global.set` overwrites can release mapped code/arena/home context,
   while an out-of-bounds/trapping write leaves both descriptor and root intact.
   Host `Global.SetValue` performs the same bounded reconciliation immediately.
   Retention remains bounded by finite table capacity or one global cell; no
   overwrite history is retained.
3. `ValidationFeatures{MultiMemory: true}` now provides an explicit internal AST
   and byte-backed Core 3 validation path. The compact decoder consumes canonical
   ULEB memory indexes for `memory.size`/`memory.grow` when a module declares
   multiple memories, while indexed load/store memargs continue through their
   existing representation. Valid memory-1 operations pass and unknown indexes
   fail with `ErrUnknownMemory`. Default `ValidateModule` and
   `ValidateByteBackedModule` preserve the Release 2 reserved-zero/single-memory
   contract. The public compile path does not request the new gate, and frontend,
   runtime, metadata, codec, imports/exports, snapshots, and codegen remain
   single-memory and fail closed.

No descriptor, table entry, global cell, `Instance`, `Compiled`, or native basedata
layout changed. A temporary `testing.AllocsPerRun(1000, ...)` test, removed after
capture, measured zero allocations/run for matching indexed
`Instance.SetGlobalValue` and `Global.SetValue`. Three 200 ms benchmark samples
for ordinary imported-table invocation remained allocation-free: one imported
table measured 63.77-64.38 ns/op, while two imported tables measured
88.69-90.96 ns/op. These are current-host watchpoints, not before/after claims.
The reconciliation path is limited to modules importing funcref storage and scans
only finite declared containers. Codec-v22 watchpoints were scalar marshal
956.5-971.1 ns/op (528 B, 14 allocs), structural marshal 2.307-2.338 us/op
(1,344-1,345 B, 21 allocs), scalar unmarshal 1.838-1.885 us/op
(1,601-1,602 B, 27 allocs), and structural unmarshal 4.144-4.688 us/op
(3,164 B, 52 allocs); the last range contains one slower sample and is not an
attributed regression.

Public typed-function-reference admission remains disabled. Full dynamic typed
`table.*` subtype/alias execution proofs, collision-safe native call identity,
typed tail contexts, and snapshots remain. Multi-memory validation is not a
runtime directory or execution claim. Arm64 remains explicit and fail-closed.

### Iteration 10 dynamic tables and indexed memory execution

Iteration 10 closes one typed-table lifecycle hole and advances multi-memory from
validation-only metadata to a bounded native beachhead without enabling either
public feature:

1. A shifted indexed function type now executes end to end through
   `table.get/set/grow/fill/copy/init`, an imported and re-exported table alias,
   local and foreign producers, and `call_ref`. Trapping writes leave the old
   descriptor/root intact. A successful overwrite performed by the local table
   owner now reconciles its exported handles as well as imported storage, so the
   final descriptor replacement releases a logically closed consumer and its
   physical code/arena/home resources. The scan remains bounded by finite table
   capacity and ordinary table invocation stays allocation-free.
2. Compiled modules now carry an exact, bounded memory declaration/import/export
   directory behind one sidecar pointer while retaining `HasMemory`,
   `MemMinPages`, and `MemMaxPages` as the direct memory-0 execution cache.
   `MemoryImports`, `MemoryMetadata`, `ImportSpec`, exact named exports/re-exports,
   aggregate policy/managed-instance accounting, import ordering/limits, and
   lifecycle ownership all use the directory. Codec v23 persists exact 32/64-bit
   address form, shared flag, declared min/max, import key, export indexes, and the
   direct memory-0 cache; v22 and older blobs are rejected. Unsupported staged
   feature bits remain load/instantiate fail-closed.
3. On linux/amd64 with explicit bounds only, an internal `MultiMemory` frontend
   gate now instantiates local or imported secondary memories and writes a
   16-byte native entry per index: `{base u64, current-bytes u32,
   current-pages u32}`. Offset 64 in basedata reuses the former unused memory
   helper slot for the directory pointer; the canonical 256-byte basedata and
   fixed 16-slot wrapper-tail bank do not grow. Native `memory.size 1`,
   `i32.load` and `i32.store` execute with independent bounds/traps and memory-0
   isolation. The amd64 SIB encoder now emits a zero displacement byte when an
   allocated indexed base is RBP/R13, avoiding the no-base encoding. Indexed
   grow, other scalar/SIMD widths, bulk/data operations, snapshots, guard mode,
   public admission, and arm64 remain rejected.

The two-memory synthetic module emitted 654 bytes total: wrapper/function spans
were 128 bytes (`memory.size 1`), 176 bytes (`i32.store 1`), 192 bytes
(`i32.load 1`), and 158 bytes (ordinary memory-0 `i32.load`). Fixed measured
layouts are `Compiled=712`, `Instance=792`, `memoryDef=40`, compiled memory
sidecar=40, and instance memory sidecar=48 bytes. The instance sidecar is nil for
ordinary single-memory instances; a two-memory native directory consumes 32
arena bytes. Three 200 ms samples measured staged memory-0 load invocation at
27.98-29.18 ns/op and memory-1 at 31.29-31.39 ns/op, all 0 B/op and 0
allocations/op. These are current-host watchpoints, not a before/after claim.

Codec-v23 watchpoints were scalar marshal 1.027-1.061 us/op (528 B, 14
allocations), structural marshal 2.397-2.443 us/op (1,344 B, 21 allocations),
scalar unmarshal 1.982-2.115 us/op (1,762 B, 29 allocations), and structural
unmarshal 4.428-4.478 us/op (3,325 B, 54 allocations). Imported-table invocation
remained allocation-free: one imported table plus one local table measured
65.92-67.13 ns/op; the first of two imported tables measured 94.80-97.57 ns/op.
These are codec-v23/current-host watchpoints and are not attributed regressions.

Public typed-reference and multi-memory bits remain disabled, so the official
Release 3 schema-2 inventory remains byte-for-byte unchanged at 144 green/114 red
files, 1,691 passed/535 skipped modules, and 51,765 passed/5 failed/6,268 skipped
assertions. Release 1 and Release 2 remain zero-gap.

### Iteration 11 indexed memory completion and native type keys

Iteration 11 completes the staged scalar/bulk multi-memory slice on
linux/amd64 explicit bounds and removes the demonstrated compact typed-call
identity weakness without widening public admission:

1. Every indexed scalar memory operation now lowers through the exact native
   directory: integer and floating-point loads/stores, all narrow signed/unsigned
   extensions, and indexed `memory.grow`. Successful growth updates the memory
   backing, directory base, current bytes, and current pages together after
   overflow/declared-max checks; failure returns `-1` and leaves all cached and
   exported state unchanged. Local, imported, and re-exported aliases observe the
   grown page, and the ordinary memory-0 instruction bytes remain unchanged.
2. Active data segments retain their exact memory index. Indexed
   `memory.init/copy/fill` execute with exact source/destination bounds,
   cross-memory copies, same-memory overlap, passive drop state, and trap
   atomicity while preserving the optimized memory-0 paths. Duplicate imported
   memory declarations may alias one host `Memory`; attachment and detachment are
   deduplicated, while policy accounting remains declaration-based. Codec v24
   introduced the active-data memory index and rejects v23 and older artifacts.
3. The legacy 32-bit FNV discriminator remains only as a diagnostic regression
   oracle. Compiled metadata, codec v25, 32-byte funcref/table descriptors,
   immutable-table proofs, and amd64/arm64 native checks now use 64-bit keys
   derived from the complete canonical structural stream with SHA-256. Two valid
   signatures deliberately colliding at FNV value `0xed3a07d1` produce different
   keys. Equivalent shifted, shared/duplicated, recursive, codec-reloaded, and
   cross-instance types agree; a forged table key traps as wrong-signature. The
   canonicalizer streams bytes into the digest, retains no process-global cache,
   and fails closed when indexed graph expansion exceeds a fixed 1 MiB work
   budget.

No fixed runtime layout grows: `Compiled` remains 712 bytes and table/funcref
entries remain 32 bytes. Three 200 ms benchmark samples on the current
linux/amd64 host measured ordinary fixed table-0 indirect invocation at
28.48-31.29 ns/op and an imported table plus local table at 69.36-73.24 ns/op;
both remained 0 B/op and 0 allocations/op. Staged memory-0 loads measured
27.90-30.29 ns/op and memory-1 loads 32.07-34.15 ns/op, also allocation-free.
Codec-v25 watchpoints were scalar marshal 1.053-1.216 us/op (528 B, 14 allocs),
structural marshal 2.528-2.568 us/op (1,344 B, 21 allocs), scalar unmarshal
2.040-2.129 us/op (1,778 B, 29 allocs), and structural unmarshal
4.330-4.396 us/op (3,333 B, 54 allocs). These are current-host watchpoints, not
before/after attribution claims.

Public typed-reference and multi-memory feature bits remain disabled. Indexed
SIMD memory operations, snapshots, guard-page multi-memory, typed tail contexts,
remaining reference/GC instructions, public admission, and arm64 execution parity
remain completion work. The official Release 3 baseline therefore remains
byte-for-byte unchanged.

### Iteration 12 indexed SIMD, registered memories, and descriptor ownership

Iteration 12 completes the remaining staged linux/amd64 explicit-bounds
multi-memory instruction family and closes two lifecycle boundaries without
widening public admission:

1. Every SIMD memory opcode now reads the exact encoded memory index. This covers
   `v128.load/store`, all six load-extend forms, four splats, two zero loads, and
   all eight lane loads/stores. Indexed address/base registers are pinned across
   temporary scalar extraction so lane stores cannot clobber the directory base.
   Focused tests cover unaligned effective addresses, declared alignment,
   `u32`-offset overflow, width-specific end-of-memory traps, trap atomicity,
   memory-0 isolation, and byte-for-byte stability of ordinary memory-0 code.
2. The spec harness resolves every declared memory import instead of only the
   single-memory convenience accessor. A restricted registered-memory execution
   shape can now keep multiple consumers live when memory index 0 is exported by
   a module with no native functions and the consumers have no function or host
   imports. Each consumer owns one fixed 256-byte basedata image plus scratch in
   its existing multi-memory sidecar. Calls serialize on the memory lifecycle
   sidecar, restore the consumer image, refresh every 16-byte native directory
   entry, propagate monotonic memory-0 growth back to the owner image, and restore
   the prior tenant. Two consumers observe each other's imported growth and keep
   the producer alive until the final close. Executable producers and broader
   call contexts remain explicit instantiation errors rather than unsafe aliases.
3. Every distinct cross-instance function producer is retained transactionally
   before link-time recompilation and released on consumer close or any failed
   instantiation path. A shifted indexed `call_ref` therefore remains callable
   after producer logical close. The retained set is bounded by the finite import
   set and stored only in the lazy instance sidecar; `Instance` remains 792 bytes,
   `Compiled` remains 712 bytes, and native descriptors remain 32 bytes. The
   `Memory` handle remains 16 bytes; its host/export lifecycle sidecar grows from
   32 to 40 bytes for the staged basedata serializer mutex.

No codec version changes in this iteration. Codec v25 already stores the full
feature word and exact structural/memory metadata. The feature scanner now marks
`call_ref`, typed null-control, direct/indirect/reference tail calls, and their
combined requirements. In-memory staged compilation carries its private typed
admission bit only in the non-serialized code cache. Consequently a codec-loaded
typed artifact remains public fail-closed. Snapshot preflight also rejects every
typed-reference or tail-call artifact before retaining imports, running start, or
mutating memory/globals; multi-memory snapshots remain rejected as before.

The restricted registered-memory path is not a general shared-basedata ABI. It
intentionally rejects an executable memory owner, any consumer function import,
and any host-call context. At the end of iteration 12, compact-import encodings in
the pinned Release 3 `memory_grow` and `memory_size_import` conversion still failed
decode; iteration 13 closes that decoder gap without opening the public feature
gate. The official schema-2 totals remain byte-for-byte unchanged.

Measured on the current linux/amd64 host with three 200 ms samples:

| Watchpoint | Range | Allocation |
|---|---:|---:|
| staged memory-0 scalar load | 28.14-28.81 ns/op | 0 B/op, 0 allocs/op |
| staged memory-1 scalar load | 31.03-31.90 ns/op | 0 B/op, 0 allocs/op |
| staged memory-1 `v128.load` | 27.64-27.96 ns/op | 0 B/op, 0 allocs/op |
| registered imported-memory basedata switch | 87.18-88.19 ns/op | 0 B/op, 0 allocs/op |
| retained cross-instance typed `call_ref` | 40.05-51.14 ns/op | 0 B/op, 0 allocs/op |

These are current-host watchpoints, not before/after attribution claims. The
basedata serializer cost is confined to the staged registered-memory shape;
ordinary single-memory, local multi-memory, table, and public typed-call hot paths
do not enter it.

### Iteration 13 compact imports, linking state, and retained typed tails

Iteration 13 removes the reached compact-binary blocker, proves official linking
store order under the existing safety boundaries, and adds one bounded
cross-instance typed-tail context without widening public admission:

1. The import decoder now understands both Core 3 compact group markers. `0x7f`
   groups mixed external kinds under one module name; `0x7e` shares one external
   kind across the group. The enclosing import count remains authoritative, so a
   group cannot expand past it. Invalid kinds, malformed or excessive counts,
   invalid limits/types, unknown function type indexes, malformed UTF-8, and
   truncated payloads fail on both decoded and byte-backed paths. Default
   validation rejects `UsesCompactImports`; the staged multi-memory compiler
   admits it explicitly. The actual pinned `memory_grow` and
   `memory_size_import` files execute every assertion in focused tests, and exact
   memory import order survives codec-v25 metadata reload. Public raw compile and
   public codec load remain fail-closed.
2. Instantiation preflights every global, memory, and table binding before
   attaching storage owners. Function bindings keep their existing signature-
   specific link/binder checks, which also run before active segments. Focused
   tests execute the official `linking0`-`3` binaries through the staged path:
   unknown table/memory imports do not mutate store state; earlier successful
   table/data segments persist when a later segment traps; a trapping start
   leaves imported memory and table writes visible; and imported growth respects
   the producer's declared maximum. The executable-owner plus function/local-
   multi-memory contexts forbidden by the registered basedata design still fail
   before mutation. Snapshot rejection now distinguishes owned-local multiple
   memories from imported/shared shapes and occurs before attachment.
3. Retained int-register `InstanceExport` descriptors now carry a separate
   immutable cross-instance context tag. At root `return_call_ref`, amd64 proves
   that the current return address is the function's own adapter continuation,
   copies arguments to the target's fixed basedata tail bank, copies trap/fence
   control words, tears down the current frame and adapter continuation, and
   jumps to the target offset-0 wrapper. The target returns directly to the
   original native caller. A cross transfer followed by 1,000,000 target-local
   tail steps completes with fixed stack use; shifted equivalent types remain
   callable after producer logical close. Nested callers, local wrapper targets,
   host and untagged foreign descriptors, nulls, wrong keys, public admission,
   snapshots, and arm64 remain explicit failures.

No serialized/runtime layout grows. `Compiled` remains 712 bytes, `Instance`
remains 792 bytes, native descriptors remain 32 bytes, and basedata remains 256
bytes; two existing wrapper-tail-bank slots are reused only during the mutually
exclusive register-ABI cross-tail transfer. The temporary decoded `wasm.Module`
layout grows from 360 to 368 bytes for the compact-import source marker. Three
200 ms samples measured registered-memory basedata switching at 90.90-108.3
ns/op, retained non-tail cross-instance `call_ref` at 38.45-38.75 ns/op, and the
new root cross-instance `return_call_ref` transfer at 60.73-61.37 ns/op. Every
invocation benchmark reported 0 B/op and 0 allocations/op.

The official schema-2 inventory remains byte-for-byte unchanged at 144 green/114
red files, 1,691 passed/535 skipped modules, and 51,765 passed/5 failed/6,268
skipped assertions. Diagnostics now report explicit `compact imports` feature
rejection where the public gate is closed instead of `invalid import`; this does
not reclassify a parser or tool failure as a skip.

### Iteration 14 staged accounting, owned snapshots, and nested typed tails

Iteration 14 advances three bounded product/runtime boundaries without widening
public Core 3 admission:

1. `tests/spec-v3-staged-multi-memory.json` is a committed schema-1 delta over the
   exact pinned WABT JSON streams for 28 safe multi-memory files. The runner
   substitutes only the internal compact-import + multi-memory compile/instantiate
   gate, then replays commands in order with normal registrations/imports/store
   effects. It accounts for 767 commands: 38 successfully instantiated modules,
   709 successful action/return/trap assertions, 2 expected-invalid modules, and
   14 expected-uninstantiable modules. Unexpected compile rejects, link rejects,
   and assertion failures are all zero. The runner includes local address/alignment,
   integer/float load/store/trap, bulk copy/fill/init/drop, data/start, size/grow,
   imported grow/size, `store2`, and the safe `linking2` shape. `load1`, `store1`,
   and the mixed/private linking contexts remain outside this green delta because
   they require the deliberately unsupported general shared-basedata ABI; their
   focused tests continue to require explicit failures rather than hidden skips.
   Indexed SIMD remains covered by the complete staged opcode fixture while broader
   official safe-family accounting is still completion work.
2. Snapshot format version 3 stores a bounded memory count followed by one page
   count and independently zero-tail-trimmed image per memory. Owned local multi-
   memory capture copies every image and grown size plus passive-data drop lengths;
   restore sizes all mappings first, copies each stored prefix into zeroed memory,
   and rebuilds every 16-byte native directory entry. Blob loading retains trimmed
   prefixes instead of allocating declared zero tails before module-limit checks.
   Malicious count, page maximum, and image-length records reject before unsafe
   allocation or mutation. The sparse fixture occupies 198,339 bytes for 327,680
   bytes of live pages; `Snapshot` grows from 160 to 184 bytes and `memorySnap` is
   32 bytes. Imported/shared memories, function/global/table imports in this staged
   shape, registered tenants, guard mode, tables/reference globals, and typed/tail
   artifacts remain fail-closed. Public loading still rejects the unsupported
   multi-memory feature bit.
3. Retained int-register `InstanceExport` `return_call_ref` now resumes a nested
   internal caller. After argument staging and frame release, amd64 replaces the
   discarded callee activation with one fixed 32-byte stack record containing a
   generated trampoline address, caller linmem, and two integer result slots. The
   foreign wrapper writes results and returns through the trampoline; it restores
   caller RBX/memory-size/module globals and RAX/RDX before the ordinary post-call
   continuation. A nested caller adds to the returned value after 1,000,000 target-
   local tail steps, proving continuation and fixed-context behavior. Null, wrong-
   key, host, wrapper/untagged foreign, imported-direct, general-table, public,
   snapshot, and arm64 paths remain explicit. Three 200 ms samples measured the
   root transfer at 61.21-63.24 ns/op and nested transfer at 87.35-87.76 ns/op,
   both 0 B/op and 0 allocations/op.

No serialized compiled codec version changes: codec v25 already represents exact
memory/type metadata and public feature gates remain closed. Fixed runtime layouts
remain `Compiled=712`, `Instance=792`, descriptor=32 bytes, and basedata=256 bytes.
Snapshot v3 is an independent product-blob version change and versions 1-2 remain
accepted for their compatible single-memory layouts.

The official schema-2 inventory remains byte-for-byte unchanged at 144 green/114
red files, 1,691 passed/535 skipped modules, and 51,765 passed/5 failed/6,268
skipped assertions. The schema-1 staged delta is supplementary evidence, not a
reclassification of public feature skips.

### Iteration 15 complete family accounting, official typed tails, and bounded memory64

Iteration 15 advances three bounded areas without opening any public Core 3 gate:

1. The staged multi-memory evidence is now a complete schema-2 family matrix. It
   replays every command from all 41 pinned `multi-memory` files plus
   `simd/simd_memory-multi`: 913 commands, 76 successfully instantiated modules,
   748 successful assertions, 4 expected-invalid, 20 expected-unlinkable, and 20
   expected-uninstantiable cases. `linking1`, `load1`, and `store1` each hit one
   exact general shared-basedata safety rejection; their 23 dependent commands are
   recorded as blocked rather than omitted. Unexpected compile rejects, link
   rejects, and assertion failures are zero. Missing WABT still skips the whole
   supplementary runner rather than producing a partial green result.
2. `tests/spec-v3-staged-return-call-ref.json` accounts for all 51 commands in the
   pinned official file. Four modules instantiate, all 35 reached scalar/trap
   assertions pass, and all 11 invalid modules reject. The remaining valid module
   uses reference-valued results and stops at the explicit backend ABI gate. A
   third descriptor home tag distinguishes exact same-instance scalar wrappers
   from host thunks sharing caller basedata. Nested retained cross-instance tails
   restore both integer results, survive producer logical close, and repeat 10,000
   transfers through the fixed 32-byte return record. Root transfer measures
   62.93-67.25 ns/op and nested transfer 82.06-89.72 ns/op, both allocation-free.
3. A first memory64 execution/product slice runs on linux/amd64 explicit bounds.
   It requires exactly one local non-shared memory, an explicit maximum no greater
   than 65,535 pages, and no data segments. Exact u64 limits survive metadata and
   codec-v25 round trips; policy accounting uses the declared reservation;
   `memory.size/grow` consume/produce i64; and `i32.load/store` use full u64
   addresses and offsets with carry checks before the bounded byte comparison.
   Imports, shared/multi-memory, unbounded/excessive reservations, other operation
   families, guard mode, public admission, and arm64 fail closed. The fixture is
   144 Wasm bytes, emits 744 code bytes, marshals to 1,069 codec bytes, reserves
   196,608 bytes, and measures 39.28-41.23 ns/op at 0 B/op and 0 allocs/op.

No compiled codec or fixed runtime layout version changes. Codec v25 already
stores exact memory address form/limits and required features; the in-memory
memory64 admission bit is not serialized, so public load remains fail-closed.
`Compiled=712`, `Instance=792`, native descriptors=32 bytes, basedata=256 bytes,
`Snapshot=184`, and `memorySnap=32` remain unchanged. Snapshot-v3 capture was also
corrected to omit the runtime's legacy scratch page for modules that declare no
Wasm memory.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 144
green/114 red files, 1,691 passed/535 skipped modules, and 51,765 passed/5 failed/
6,268 skipped assertions. The new matrices are supplementary staged evidence.

### Iteration 16 direct-tail completion, indirect-tail accounting, and integer memory64

Iteration 16 advances two mandatory families without opening a public Core 3 gate:

1. A private direct/indirect tail frontend switch now reaches amd64 lowering while
   an explicit product boundary rejects the staged switch on arm64 before codegen.
   Local direct tails retain their register/wrapper frame-reuse paths. A direct tail
   to a host import uses the existing bounded async/synchronous host bridge and then
   returns immediately from the current Wasm function; cross-instance imported
   direct targets remain rejected. `tests/spec-v3-staged-return-call.json` accounts
   all 47 pinned commands: 3 modules instantiate, all 33 assertions pass, and all
   11 invalid modules reject, with zero unexpected gaps.
2. `tests/spec-v3-staged-return-call-indirect.json` accounts all 79 pinned commands.
   Two standalone valid modules instantiate; all 16 invalid and 11 malformed cases
   are recorded. The main valid module stops at the exact general multi-table gate
   because the current proof covers only private immutable local table 0; all 49
   dependent return/trap actions are recorded as blocked. Unexpected compile/link/
   assertion gaps are zero. This accounting does not widen the table proof or
   reclassify public skips.
3. The bounded local memory64 path now admits every integer scalar operation: 12
   loads and 7 stores. AST and byte-backed support paths agree; signed/unsigned
   narrow loads extend exactly; narrow stores write only their declared width; and
   every width traps at the exact end boundary. Float memory operations remain an
   explicit frontend gate, as do SIMD, bulk/data, imports, shared/multi-memory,
   guard mode, public admission, and arm64. The matrix is 502 Wasm bytes, emits
   3,227 code bytes, and covers 19 operations. The original product fixture remains
   144 Wasm bytes / 744 code bytes / 1,069 codec bytes / 196,608 reserved bytes.

No codec, snapshot, fixed runtime, basedata, descriptor, or product layout changes.
The compile-only tail switch is retained only in the in-memory staged feature
sidecar, and public codec load still rejects the serialized Core 3 feature bits.
Five 500 ms memory64 samples measured 36.73-37.33 ns/op at 0 B/op and 0 allocs/op.
Three 200 ms typed-tail samples measured root transfers at 63.65-64.89 ns/op and
nested transfers at 75.82-78.51 ns/op, also allocation-free.

The official public Release 3 schema-2 inventory remains byte-for-byte unchanged:
144 green/114 red files, 1,691 passed/535 skipped modules, and 51,765 passed/5
failed/6,268 skipped assertions. Release 1 and Release 2 remain zero-gap.

### Iteration 17 complete multi-memory accounting, indirect tails, and float memory64

Iteration 17 closes three bounded areas without opening a public Core 3 gate:

1. Immutable-local table analysis is now finite per table rather than a module-wide
   table-0 boolean. Each admitted table is local, unexported, unmutated, and contains
   only same-module functions; imported functions elsewhere in the module do not
   weaken that proof. Scalar tail modules select GP/XMM internal descriptors only
   under the compile-only tail sidecar, while wrapper-only mixed-result targets use
   the existing fixed 16-slot basedata argument bank. Ordinary Release 1/2 float
   descriptors remain on the wrapper path. All 79 pinned `return_call_indirect`
   commands are green: 3 modules, 49 assertions, 16 invalid, and 11 malformed.
2. The shared-memory basedata serializer now has an explicit compile-time eligibility
   proof and an atomically installed owner/tenant context. Admitted modules may have
   an executable owner or re-export-only cross-instance function imports, but no
   tables, globals, passive/reference state, host calls, imported starts, or native
   calls to imported functions. Every owner/tenant invocation serializes on the
   memory lifecycle mutex, swaps one fixed basedata image, refreshes the finite native
   memory directory, and restores the previous image. Prepared and managed calls use
   the same boundary. A race-focused 1,000+1,000-call owner/tenant test is green.
   The complete 42-file multi-memory matrix now has no gates: 913 commands, 79
   modules, 771 assertions, 4 invalid, 22 unlinkable, and 20 uninstantiable cases.
3. The bounded local memory64 path executes `f32.load/store` and `f64.load/store` in
   addition to all integer scalars. Full u64 offsets, carry, exact end boundaries,
   exact store widths, and signaling/arithmetic NaN payload bits are preserved;
   enabling the staged feature leaves memory32 integer and float code bytes unchanged.
   The float fixture is 173 Wasm bytes and emits 972 code bytes. A new exact
   supplementary matrix accounts 807 commands from six pinned address/alignment/
   float/load/grow/trap files: 1 module and 8 assertions execute, 42 bounded-policy/
   data/family gates block 614 dependents, and 83 invalid plus 59 malformed cases are
   recorded with zero unexpected compile/link/assertion gaps.

No codec, snapshot, fixed runtime, basedata, descriptor, or public product version
changes. The multi-memory eligibility and widened scalar descriptor choice live only
in compile-time/in-memory sidecars; codec load remains fail-closed. Five 500 ms samples
measured executable shared-memory owners at 80.14-87.47 ns/op, their tenants at
92.39-98.03 ns/op, f64 memory64 load at 35.68-36.56 ns/op, and integer memory64
store/load at 36.10-36.83 ns/op, all 0 B/op and 0 allocations/op.

The official public Release 3 schema-2 inventory remains byte-for-byte unchanged:
144 green/114 red files, 1,691 passed/535 skipped modules, and 51,765 passed/5
failed/6,268 skipped assertions. Release 1 and Release 2 remain zero-gap.

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

Iteration 8 contains exactly three code/test commits and this documentation
commit:

1. `7da7dd94` — hash indexed and recursive function signatures structurally and
   route amd64/arm64 immutable-table proofs through module-aware IDs.
2. `ecc571f8` — enforce exact indexed funcref type/nullability at public
   `Call`/`Invoke` ingress and result egress.
3. `6bd66998` — enforce the same exact typed-reference contract across
   synchronous host arguments and results.

Iteration 9 contains exactly three code/test commits and this documentation
commit:

1. `eb5dff8b` — enforce exact indexed funcref type/nullability at mutable global
   public/host ingress and egress.
2. `847bae9d` — release closed funcref producers after successful final table or
   global overwrite while preserving roots on traps.
3. `5757b454` — add explicit staged AST/byte-backed multi-memory validation with
   indexed `memory.size`/`memory.grow` decoding and default fail-closed behavior.

Iteration 10 contains exactly three code/test commits and this documentation
commit:

1. `29b7b712` — prove dynamic typed-table operations/aliases and reconcile roots
   after local table-owner overwrites.
2. `6836de2a` — add exact indexed memory directories, inspection, aggregate
   policy accounting, and codec v23 persistence.
3. `89314785` — execute staged local/imported memory-1 size/i32 load/store on
   linux/amd64 with bounded native directories and fail-closed platform/product
   gates.

Iteration 11 contains exactly three code/test commits and this documentation
commit:

1. `d34b75d6` — execute every indexed scalar memory width and indexed
   `memory.grow` with exact cache/export update and failure semantics.
2. `ad55a250` — execute indexed bulk/data lifecycle, persist active-data memory
   indexes in codec v24, and deduplicate duplicate imported-memory aliases.
3. `db703743` — replace native 32-bit function signature discrimination with
   bounded 64-bit structural keys, persist codec v25, and prove legacy collision,
   recursive/cross-instance identity, traps, reload, and guard/platform gates.

Iteration 12 contains exactly three code/test commits and this documentation
commit:

1. `8cf29a07` — lower every indexed SIMD memory form through exact memory
   directories with bounds/overflow/lane tests and allocation-free watchpoints.
2. `84aab6e1` — serialize restricted registered-memory basedata tenants, resolve
   all harness memory imports, synchronize grow visibility, and prove rollback,
   close order, snapshot preflight, and fail-closed owner/context gates.
3. `f2ccb21f` — retain cross-instance function producers, persist typed/tail
   opcode feature requirements, and reject unresolved typed/tail snapshots before
   mutation while preserving staged `call_ref` execution.

Iteration 13 contains exactly three code/test commits and this documentation
commit:

1. `d00de828` — decode bounded `0x7f`/`0x7e` compact import groups, keep default
   validation fail-closed, and execute the pinned imported grow/size modules with
   exact codec metadata order.
2. `37f26376` — preflight storage bindings, prove official `linking0`-`3` store
   ordering under explicit safety gates, and make multi-memory snapshot rejection
   shape-specific before attachment.
3. `20228cce` — add a compile-only typed-tail gate and root amd64
   `return_call_ref` transfer through retained cross-instance exports with
   million-step, trap, lifecycle, host, nested-context, and arm64 gates.

Iteration 14 contains exactly three code/test commits and this documentation
commit:

1. `2cf93058` — replay 767 exact pinned commands across 28 safe staged multi-
   memory files and commit the zero-hidden-gap schema-1 delta.
2. `d808215a` — add snapshot-v3 owned-local multi-memory capture/restore with
   bounded per-memory records, passive-data lifecycle, native-directory rebuild,
   and malicious metadata rejection.
3. `f5424668` — resume nested retained cross-instance typed tails through one
   fixed 32-byte return record and caller-context trampoline.

Iteration 15 contains exactly three code/test commits and this documentation
commit:

1. `a00e0fc2` — account for the complete 42-file staged multi-memory/SIMD family
   with three exact feature gates and no omitted dependent commands.
2. `1da704b1` — replay the pinned official `return_call_ref` file, admit tagged
   same-instance scalar wrapper tails, restore two retained integer results, and
   prove repeated cross-instance transfers while preserving host/platform gates.
3. `54565222` — stage one bounded linux/amd64 local memory64 size/grow/i32-load/
   store path with exact metadata/codec limits and checked u64 arithmetic.

Iteration 16 contains exactly three code/test commits and this documentation
commit:

1. `367d3a10` — add a private direct-tail gate, tail-return through bounded host
   imports, keep cross-instance/arm64 paths fail-closed, and make all 47 pinned
   `return_call` commands green.
2. `cb2e0758` — account all 79 pinned `return_call_indirect` commands with exact
   invalid/malformed totals, one general multi-table gate, and 49 blocked actions.
3. `3f2fc17f` — execute all 19 integer scalar memory64 load/store opcodes with
   signed/unsigned, exact-width, bounds, AST/byte-backed, and float-gate coverage.

Iteration 17 contains exactly three code/test commits and this documentation
commit:

1. `956d8dc3` — generalize immutable-local proofs per table, keep scalar internal
   descriptors staged, tail-enter wrapper-only mixed results through the fixed bank,
   and make all 79 pinned `return_call_indirect` commands green.
2. `b850c7ee` — serialize bounded executable-owner/tenant basedata contexts, reject
   unproven imported-call/private-state shapes, and make the complete 913-command
   multi-memory matrix gap-free.
3. `270039cb` — execute all four float scalar memory64 operations and account 807
   exact pinned address/alignment/float/load/grow/trap commands under explicit gates.

## Validation performed

Commands were run from the repository root on linux/amd64.

| Command | Result |
|---|---|
| iteration 17 staged family runners | PASS: multi-memory is gap-free at 42 files / 913 commands / 79 modules / 771 assertions / zero gates / zero blocked; `return_call` remains 47 commands / 3 modules / 33 assertions / 11 invalid; `return_call_indirect` is fully green at 79 commands / 3 modules / 49 assertions / 16 invalid / 11 malformed; `return_call_ref` remains 51 commands / 4 modules / 35 assertions / 11 invalid / one reference-result gate; memory64 accounting is 6 files / 807 commands / 1 module / 8 assertions / 42 exact feature gates / 614 blocked / 83 invalid / 59 malformed. Log `.validation/iteration17-staged-final.log`. |
| iteration 17 shared-basedata proof | PASS: executable owner and tenant switching, re-export-only function context, native imported-call rejection, exact official linking order, 1,000+1,000 concurrent owner/tenant calls, and the complete matrix. Race-focused test PASS. Logs `.validation/iteration17-commit2-focused.log`, `.validation/iteration17-commit2-package.log`, and `.validation/iteration17-commit2-race.log`. |
| iteration 17 memory64 scalar proof | PASS: all 19 integer plus 4 float operations, exact NaN bits, u64 offset carry, width-specific end traps, AST/byte-backed admission, bulk/data gates, and unchanged memory32 integer/float code. Logs `.validation/iteration17-commit3-focused.log`, `.validation/iteration17-commit3-packages.log`, and `.validation/iteration17-memory64-official.log`. |
| `go generate ./...` plus generated diff check | PASS; no generated diff. Log `.validation/iteration17-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD. Log `.validation/iteration17-all.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; staged multi-memory shared contexts, tails, and memory64 remain explicit-bounds-only. Log `.validation/iteration17-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence only; no arm64 shared-context, indirect-tail, or memory64 scalar execution claim. Log `.validation/iteration17-arm64-build.log`. |
| WABT/interpreter verification | PASS: WABT 1.0.41 and official interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration17-wabt.log` and `.validation/iteration17-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration17-spec1.log` and `.validation/iteration17-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration17.log` and `.validation/iteration17-spec3-baseline.log`. |
| iteration 17 benchmarks | PASS: shared executable owner 80.14-87.47 ns/op, tenant 92.39-98.03 ns/op, existing registered tenant 90.55-108.2 ns/op, memory64 f64 load 35.68-36.56 ns/op, integer store/load 36.10-36.83 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration17-shared-basedata-bench.log` and `.validation/iteration17-memory64-bench.log`. |
| iteration 16 staged family runners | PASS: multi-memory remains 42 files / 913 commands / 76 modules / 748 assertions / 3 exact gates / 23 blocked; `return_call` is 47 commands / 3 modules / 33 assertions / 11 invalid / zero gaps; `return_call_indirect` is 79 commands / 2 modules / 16 invalid / 11 malformed / 1 exact multi-table gate / 49 blocked / zero unexpected gaps; `return_call_ref` remains 51 commands / 4 modules / 35 assertions / 11 invalid / 1 reference-result gate. Log `.validation/iteration16-staged-final.log`. |
| iteration 16 memory64 integer scalar matrix | PASS: all 12 integer loads and 7 integer stores, signed/unsigned extension, exact-width writes, per-width end traps, AST/byte-backed admission, float rejection, and unchanged prior gates. Matrix wasm=502 bytes, code=3,227 bytes, operations=19. Logs `.validation/iteration16-commit3-focused2.log` and `.validation/iteration16-commit3-matrix.log`. |
| `go generate ./...` plus generated diff check | PASS; no generated diff. Log `.validation/iteration16-go-generate.log`. |
| `go test ./... -count=1` | PASS. Log `.validation/iteration16-all.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; staged tail and memory64 execution remain explicit-bounds-only. Log `.validation/iteration16-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence only; staged direct/indirect tail and memory64 execution reject on arm64. Log `.validation/iteration16-arm64-build.log`. |
| WABT/interpreter verification | PASS: WABT 1.0.41 and official interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration16-wabt.log` and `.validation/iteration16-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration16-spec1.log` and `.validation/iteration16-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration16.log` and `.validation/iteration16-spec3-baseline.log`. |
| iteration 16 benchmarks | PASS: typed root 63.65-64.89 ns/op, typed nested 75.82-78.51 ns/op, memory64 store/load 36.73-37.33 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration16-bench.log` and `.validation/iteration16-memory64-bench-final.log`. |
| complete staged multi-memory family matrix | PASS: 42 files, 913 commands, 76 modules, 748 assertions, 4 invalid, 20 unlinkable, 20 uninstantiable, 3 exact feature rejects, 23 dependent blocked commands, and zero unexpected gaps. Log `.validation/iteration15-staged-final.log`; committed schema-2 matrix reproduced exactly. |
| exact pinned `return_call_ref` runner | PASS: 51 commands, 4 modules, 35 assertions, 11 expected-invalid modules, one explicit reference-result ABI gate, and zero unexpected gaps. Logs `.validation/iteration15-commit2-final-focused.log` and `.validation/iteration15-staged-final.log`. |
| staged bounded memory64 focused suite | PASS: exact metadata/codec/policy round trip, i64 size/grow, grown-page i32 load/store, oversized-delta failure atomicity, address/offset wrap traps, public/import/shared/guard/arm64 gates, and unchanged memory32 code. Fixture wasm=144, code=744, codec=1,069, reservation=196,608 bytes. Logs `.validation/iteration15-commit3-final-focused.log` and `.validation/iteration15-staged-final.log`. |
| final focused frontend/backend/runtime/`src/wago` package suites | PASS. Logs `.validation/iteration15-commit2-packages3.log` and `.validation/iteration15-commit3-packages3.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade is current. Log `.validation/iteration15-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD. Log `.validation/iteration15-all.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; staged multi-memory, typed tails, and memory64 remain explicit-bounds-only. Log `.validation/iteration15-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence only; ordinary call paths mask the local-wrapper descriptor tag, while multi-memory restore, typed tails, and memory64 remain unadvertised. Log `.validation/iteration15-arm64-build.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and official interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration15-wabt.log` and `.validation/iteration15-spec-interpreter.log`. |
| `make spec1` and `make spec2` with pinned WABT on `PATH` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; all gaps zero. Logs `.validation/iteration15-spec1.log` and `.validation/iteration15-spec2.log`. |
| `make spec3` | FAIL as required at the unchanged zero-parser-failure baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268. Log `.validation/spec3-iteration15.log`. |
| `python3 scripts/spec3-baseline.py .validation/spec3-iteration15.log .validation/spec3-iteration15.json --exit-code 2 && cmp tests/spec-v3-baseline.json .validation/spec3-iteration15.json` | PASS: public schema-2 inventory reproduces byte-for-byte. Log `.validation/iteration15-spec3-baseline.log`. |
| three-sample 200 ms typed-tail and memory64 benchmarks | PASS: root 62.93-67.25 ns/op, nested 82.06-89.72 ns/op, memory64 store/load 39.28-41.23 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration15-commit2-bench.log` and `.validation/iteration15-commit3-bench2.log`. |
| staged safe multi-memory exact-command runner | PASS: 28 files, 767 commands, 38 modules, 709 execution assertions, 2 expected-invalid, 14 expected-uninstantiable, and zero unexpected compile/link/assertion gaps; committed schema-1 delta reproduced exactly. Logs `.validation/iteration14-commit1-focused.log` and `.validation/iteration14-staged-multi-memory.log`. |
| snapshot-v3 owned-local multi-memory focused suites | PASS: two grown images, independent zero-tail trim, passive-data drop state, native-directory rebuild, in-memory/blob restore, public feature rejection, imported/shared/import-bearing preflight, and malicious count/page/image rejection. Fixture blob=198,339 bytes; `Snapshot=184`, `memorySnap=32`. Logs `.validation/iteration14-commit2-focused.log` and `.validation/iteration14-commit2-package.log`. |
| typed-tail frontend/backend/runtime focused suites | PASS: retained root and nested cross-instance `return_call_ref`, 1,000,000 target-local steps, resumed caller continuation/result, producer close order, null/wrong-key recovery, host failure, and existing local tail paths. Log `.validation/iteration14-commit3-focused.log`. |
| final focused amd64/frontend/runtime/`src/wago` package suites | PASS. Log `.validation/iteration14-commit3-packages.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade is current. Log `.validation/iteration14-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD. Log `.validation/iteration14-all.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; staged multi-memory restore and typed tails remain linux/amd64 explicit-bounds-only. Log `.validation/iteration14-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence only; no arm64 multi-memory restore or typed-tail execution claim. Log `.validation/iteration14-arm64-build.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and official interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration14-wabt.log` and `.validation/iteration14-spec-interpreter.log`. |
| `make spec1` and `make spec2` with pinned WABT on `PATH` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; all gaps zero. Logs `.validation/iteration14-spec1.log` and `.validation/iteration14-spec2.log`. |
| `make spec3` | FAIL as required at the unchanged zero-parser-failure baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268. Log `.validation/spec3-iteration14.log`. |
| `python3 scripts/spec3-baseline.py .validation/spec3-iteration14.log .validation/spec3-iteration14.json --exit-code 2 && cmp tests/spec-v3-baseline.json .validation/spec3-iteration14.json` | PASS: schema-2 inventory reproduces byte-for-byte. Log `.validation/iteration14-spec3-baseline.log`. |
| three-sample 200 ms typed cross-instance benchmarks | PASS: root `return_call_ref` 61.21-63.24 ns/op, nested continuation 87.35-87.76 ns/op; both 0 B/op and 0 allocs/op. Log `.validation/iteration14-commit3-bench.log`. |
| compact-import and pinned `memory_grow`/`memory_size_import` focused suites | PASS on AST, byte-backed, malformed-group/type/index, default-gate, exact import order, codec reload, and every official assertion. Log `.validation/iteration13-commit1-focused.log`. |
| official `linking0`-`3`, storage preflight, runtime trapped-start, and snapshot-shape focused suites | PASS: unknown imports are atomic; reached prior segment/start writes persist; safe memory-only/grow consumers execute; executable-owner/function/private-table contexts and imported/shared snapshots fail before mutation. Log `.validation/iteration13-commit2-focused.log`. |
| typed-tail frontend/backend/runtime focused suites | PASS: shifted retained cross-instance root transfer, 1,000,000 target-local steps, producer close order, null/wrong-key recovery, nested and host traps, existing local `return_call_ref`, and non-tail retained `call_ref`. Log `.validation/iteration13-commit3-focused.log`. |
| final focused package suites for wasm/frontend, amd64 backend, runtime, and `src/wago` | PASS. Logs `.validation/iteration13-commit1-packages.log`, `.validation/iteration13-commit2-packages.log`, and `.validation/iteration13-commit3-packages.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade is current. Log `.validation/iteration13-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD. Log `.validation/iteration13-all.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; compact/multi-memory staged execution and cross-instance typed tails remain explicit-bounds amd64-only. Log `.validation/iteration13-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence only; no compact-linked multi-memory or typed-tail execution claim. Log `.validation/iteration13-arm64-build.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and official interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration13-wabt.log` and `.validation/iteration13-spec-interpreter.log`. |
| `make spec1` and `make spec2` with pinned WABT on `PATH` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; all gaps zero. Logs `.validation/iteration13-spec1.log` and `.validation/iteration13-spec2.log`. |
| `make spec3` | FAIL as required at the unchanged zero-parser-failure baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268. Log `.validation/spec3-iteration13.log`. |
| `python3 scripts/spec3-baseline.py .validation/spec3-iteration13.log .validation/spec3-iteration13.json --exit-code 2 && cmp tests/spec-v3-baseline.json .validation/spec3-iteration13.json` | PASS: schema-2 inventory reproduces byte-for-byte. Log `.validation/iteration13-spec3-baseline.log`. |
| three-sample 200 ms registered-memory and typed cross-instance benchmarks | PASS: basedata switch 90.90-108.3 ns/op, non-tail `call_ref` 38.45-38.75 ns/op, root `return_call_ref` 60.73-61.37 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration13-commit2-bench.log` and `.validation/iteration13-commit3-bench.log`. |
| decoded module footprint measurement | `unsafe.Sizeof(wasm.Module{})` is 368 bytes versus 360 bytes at `3c9a4976`; fixed runtime/product layouts are unchanged. Logs `.validation/iteration13-commit1-size.log` and `.validation/iteration13-commit1-size-before.log`. |
| `go test ./src/wago -run 'TestStagedMultiMemorySIMD' -count=1` | PASS: every indexed SIMD load/store/extend/splat/zero/lane form, unaligned access, offset overflow, exact lane-width traps, memory isolation, and memory-0 code stability. Log `.validation/iteration12-commit1-focused.log`. |
| `go test ./src/wago -run 'TestStagedMultiMemory(OfficialImportGrowLinking\|NativeProducerTenantFailsClosed\|FailedLinkIsAtomic\|SnapshotPolicyRejectsBeforeMutation)' -count=1` | PASS: all registered memory imports resolve, two consumers synchronize growth, close order and rollback are exact, snapshot rejection precedes start, and executable-owner contexts fail closed. Log `.validation/iteration12-commit2-focused.log`. |
| `go test ./src/wago -run 'TestStagedTyped(CrossInstanceCallRefRetainsProducer\|SnapshotPolicyRejectsCodecRoundTrip)' -count=1` | PASS: shifted cross-instance `call_ref` survives producer logical close, final consumer close releases resources, typed/tail feature bits survive codec metadata, public load fails closed, and snapshot preflight rejects before mutation. Log `.validation/iteration12-commit3-focused.log`. |
| final focused package suites for frontend, amd64 backend, runtime, and `src/wago` | PASS. Logs `.validation/iteration12-commit1-packages.log`, `.validation/iteration12-commit2-packages.log`, and `.validation/iteration12-commit3-packages.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade is current. Log `.validation/iteration12-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD. Log `.validation/iteration12-all.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; staged SIMD/registered-memory fixtures remain explicit-bounds-only and public multi-memory remains fail-closed. Log `.validation/iteration12-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence only; no arm64 indexed-memory or typed-call execution claim. Log `.validation/iteration12-arm64-build.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and official interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration12-wabt.log` and `.validation/iteration12-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; all gaps zero. Logs `.validation/iteration12-spec1.log` and `.validation/iteration12-spec2.log`. |
| `make spec3` | FAIL as required at the unchanged zero-parser-failure baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268. Log `.validation/spec3-iteration12.log`. |
| `python3 scripts/spec3-baseline.py .validation/spec3-iteration12.log .validation/spec3-iteration12.json --exit-code 2 && cmp tests/spec-v3-baseline.json .validation/spec3-iteration12.json` | PASS: schema-2 inventory reproduces byte-for-byte. Log `.validation/iteration12-spec3-baseline.log`. |
| three-sample 200 ms scalar/SIMD memory, registered-memory, and typed cross-instance benchmarks | PASS with the exact ranges above; all report 0 B/op and 0 allocs/op. Logs `.validation/iteration12-commit1-bench.log`, `.validation/iteration12-commit2-bench.log`, and `.validation/iteration12-commit3-bench.log`. |
| `go test ./src/wago -run 'TestStagedMultiMemoryScalarWidthsAndGrow\|TestStagedMultiMemoryLocalAndImportedExecution' -count=1` | PASS: every indexed scalar width, signed/unsigned extension, local/imported/re-exported grow visibility, overflow/max failure atomicity, grown-page access, and unchanged memory-0 code. Logs `.validation/iteration11-commit1-focused.log` and `.validation/iteration11-commit1-packages.log`. |
| `go test ./src/wago -run 'TestStagedMultiMemoryBulkDataAndAliasLifecycle\|TestCompiledCodecV24VersionContract\|TestCompiledIndexedMemoryDirectoryCodecAndMetadata\|TestCompiledReaderRejectsMaliciousCountsBeforeAllocation' -count=1` | PASS: active/passive memory-1 data, cross-memory and overlap copy, fill, traps/drop, duplicate aliases, grow/close visibility, policy totals, codec v24 persistence, and snapshot rejection. Logs `.validation/iteration11-commit2-focused.log` and `.validation/iteration11-commit2-packages.log`. |
| `go test ./src/core/compiler/wasm ./src/core/compiler/backend/railshot/amd64 ./src/wago -run 'TestStructuralTypeKey\|TestStructuralTypeIDCanonicalizesRecursiveAndDuplicateGraphs\|TestStructuralTypeIDIncludesIndexedReferenceStructure\|TestCallRefInvokesLocalDescriptorAndMatchesTraps\|TestReturnCallRefReusesFrameAndFailsClosed\|TestFuncrefTableInitializerExpressionCanTargetCrossInstanceImport\|TestCompiledCodecV25VersionContract\|TestCompiledReaderRejectsMaliciousCountsBeforeAllocation' -count=1` | PASS: deliberate 32-bit collision separation, recursive/duplicate/bounded identity, descriptor traps, cross-instance codec reload, and malicious-count rejection. Log `.validation/iteration11-commit3-focused.log`. |
| `go test ./src/core/compiler/frontend ./src/core/compiler/backend/railshot/amd64 ./src/wago -count=1` | PASS on the final code commit. Log `.validation/iteration11-commit3-packages.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade is current. Log `.validation/iteration11-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD. Log `.validation/iteration11-all.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS after excluding explicit-bounds-only staged execution fixtures; public multi-memory remains fail-closed. Log `.validation/iteration11-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence; arm64 uses the widened ordinary `call_indirect` key check but still does not admit typed references or multi-memory. Log `.validation/iteration11-arm64-build.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and the official 3.0 interpreter at `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration11-wabt.log` and `.validation/iteration11-spec-interpreter.log`. |
| `PATH="$(dirname "$(scripts/bootstrap-wabt.sh --print-path)"):$PATH" make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; all failures/skips/gaps are zero. Logs `.validation/iteration11-spec1.log` and `.validation/iteration11-spec2.log`. |
| `make spec3` | FAIL as required with zero parser failures and 28 official-interpreter fallbacks: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268. Public gates remain disabled. Log `.validation/spec3-iteration11.log`. |
| `python3 scripts/spec3-baseline.py .validation/spec3-iteration11.log .validation/spec3-iteration11.json --exit-code 2 && cmp tests/spec-v3-baseline.json .validation/spec3-iteration11.json` | PASS: the committed schema-2 inventory reproduces byte-for-byte. Log `.validation/iteration11-spec3-baseline.log`. |
| three-sample 200 ms multi-memory, indirect-call, and codec-v25 benchmarks | PASS with the exact ranges recorded above; all native invocation samples remain 0 B/op and 0 allocs/op. Logs `.validation/iteration11-multi-memory-bench.log`, `.validation/iteration11-type-key-bench.log`, and `.validation/iteration11-codec-bench.log`. |

The larger skipped totals relative to the historical WABT-only baseline remain
intentional: 28 previously unparsed files contribute their real feature gaps.
Iteration 13 changes no official module/assertion counts because public typed-
reference, tail-call, compact-import, and multi-memory admission remains disabled.
The compact binary grammar and reached official imported-memory/linking files now
execute through bounded staged tests rather than synthetic equivalents, while the
public harness continues to count their explicit feature rejection. Release 1 and
Release 2 remain zero-gap.

## Architecture policy

The primary claim remains linux/amd64. Unsupported 3.0 feature bits are rejected
before backend execution with an error that includes the current `GOOS/GOARCH`.
This prevents arm64 from silently accepting tail calls, typed function references,
GC, exceptions, multi-memory, memory64, or table64.

Extended constant expressions, type-equivalence validation, exact staged storage
matching, bounded structural signature keys, exact public/host/mutable-global
boundary checks, bounded producer-root reconciliation, exact memory product/codec
directories, aggregate policy accounting, compact import decoding/storage
preflight, cross-instance producer retention, typed/tail required-feature
accounting, and the public descriptor model are architecture-neutral. The native
indexed-memory directory, all indexed scalar/SIMD loads/stores/grow, bulk/data
lowering, owned-local multi-memory restore, registered-memory basedata serializer,
root/nested cross-instance typed-tail transfers, and the bounded local memory64
size/grow/i32-load/store path are linux/amd64 explicit-bounds only; neither shared
metadata nor the internal frontend bits advertise execution on arm64. Snapshot-v3
record decoding is architecture-neutral, but admission rejects
unsupported target/product shapes before native restore. The
canonical basedata layout remains 256 bytes on amd64 and arm64, including an unused
on-arm64 128-byte wrapper-tail bank so offsets cannot drift by target. Basedata offset 64 is the indexed-memory directory pointer on amd64 and remains
unused for unsupported execution on arm64. Ordinary arm64 `call_indirect` uses the
64-bit structural key because reference-types execution is already public there.
Iteration 15 also makes ordinary arm64 indirect/reference calls mask the third
same-instance-wrapper descriptor tag; this is compatibility for shared descriptor
construction, not typed-tail admission. Iteration 16 adds a private direct/indirect
tail frontend bit so amd64 staged runners can reach proven code, but the compile
boundary rejects that bit on arm64 before backend execution. Iteration 17 adds
architecture-neutral per-table eligibility and shared-basedata safety scans, but
per-table tail jumps, owner/tenant image switching, directory refresh, and all 23
memory64 scalar operations have linux/amd64 explicit-bounds execution evidence only.
`call_ref`, typed null control, indexed multi-memory operations, memory64 execution,
and every tail-call lowering remain amd64-only and hidden behind unsupported family gates. The two
cross-tail scratch slots are layout constants inside the existing 256-byte bank;
ARM64 emits no code that consumes them. Unsupported cross-instance-direct/general-
table/reference-result/foreign-float tail contexts remain internal fail-closed
boundaries, not advertised Wasm semantics or arm64 features.
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
   and exception-safe call/runtime boundaries. Retained cross-instance typed-reference
   tails now have bounded root/nested context transfer; cross-instance direct tails
   and reference-result/native-root interactions remain separate work.

Major risks:

- the zero-parser-failure oracle now depends on development hosts having OCaml,
  dune, and menhir available for the pinned official interpreter build; the cached
  tool is revision-stamped, and any future binary-script grammar change must fail
  the strict converter rather than silently dropping commands;
- codec v25 intentionally invalidates v24 and older caches; its structural graph,
  value-type pool, indexed-memory/data metadata, and native type-key slice are
  bounded by decoded module declarations, but cache-size planning must use v25
  measurements;
- the staged multi-memory path now executes every scalar and SIMD memory form,
  indexed grow, and bulk/data lifecycle through exact directories. Compact import
  groups and the reached official imported grow/size/linking binaries are decoded
  and exercised directly. Restricted registered-memory tenants serialize fixed
  basedata images and refresh all entries. Snapshot v3 handles owned local memory
  sets. Iteration 17 closes the three official shared-basedata consumers with a
  finite eligibility proof and serialized owner/tenant images. This is still not
  a general shared-basedata ABI: native calls to imported functions, tables,
  globals, passive/reference state, host calls, imported/shared/registered snapshots,
  guard mode, public admission, and arm64 remain fail-closed;
- memory64 now checks u64 address+offset+width carry for all 19 integer and 4 float
  scalar operations, preserves float payload bits, and caps the staged reservation
  at 65,535 pages. The six-file accounting shows most official modules still require
  unbounded/excessive-memory policy, active data, SIMD/bulk, import, and allocation-
  before-validation work rather than silent omission;
- runtime call descriptors now use bounded 64-bit SHA-256-derived structural keys
  and reject canonicalization above a fixed 1 MiB work budget. This removes the
  demonstrated compact 32-bit collision class without a global cache or entry-size
  increase; full structural descriptors remain authoritative at product/storage
  boundaries, and public typed-reference admission still requires the remaining
  instruction, snapshot, tail-context, and platform work;
- local wrapper direct tails, tail-position host imports, and retained cross-instance
  root/nested `return_call_ref` transfers are bounded. Nested internal callers use
  one fixed 32-byte return record, restore two integer results, and repeat cross-
  instance transfers, while exact local scalar wrappers use a distinct tag from host
  thunks. Per-table immutable-local indirect tails are complete for the pinned file,
  while imported/mutable/exported/host-descriptor tables remain rejected. Cross-
  instance direct tails, reference-result/foreign-float typed contexts, oversized signatures, snapshots, and arm64
  still need proof; the unsupported-context trap must disappear from every publicly
  admitted valid path before the tail-call feature can be enabled;
- typed refs, exceptions, and GC all interact with native frame roots and call
  boundaries; validator, codec, staged storage imports, native signature keys,
  public/host/global signatures, harness result matching, and overwrite-triggered
  root release, dynamic table aliases, and native keys are now precise, but
  snapshots, typed tail contexts, and remaining instructions must consume exact
  descriptors before admission;
- GC collector code is meaningful but must not be mistaken for executable WasmGC
  until safepoint maps and barriers are connected;
- arm64 must remain fail-closed for every family that lacks native execution tests.

## Next bounded implementation slice

The next recursive iteration should again make exactly three atomic code/test
commits followed by one documentation commit:

1. **Widen memory64 lifecycle under exact official accounting.** Add active data
   initialization only after proving i64 constant offsets, u64 offset+length carry,
   bounds-before-copy, no allocation before declared-limit validation, codec metadata
   stability, and snapshot/public fail-closed behavior. Re-run the six-file matrix
   and turn the bounded `float_memory64`, address/load/trap modules green where their
   only remaining gate is data/unbounded policy; keep imports, shared/multi-memory,
   SIMD/bulk, guard mode, and arm64 explicit.
2. **Close one remaining typed/direct tail ABI family.** Prefer the pinned
   `return_call_ref` reference-result module with exact native root/result ownership,
   or implement cross-instance direct `return_call` with a separate fixed return
   record and producer lifecycle proof. Do not infer either from descriptor-based
   indirect tails or host-import direct tails. Preserve oversized-signature,
   snapshot, public, and arm64 gates.
3. **Generalize shared-memory contexts beyond the official multi-memory family.**
   Extend the finite basedata proof to one currently rejected valid composition—
   native calls to a retained same-memory import, or table/global/passive state—only
   with a boring lock/context-switch/close-order proof and allocation-free admitted
   calls. Keep host callbacks, snapshots, guard mode, public admission, and arm64
   fail-closed until each composes. Do not treat the now-green 42-file matrix as
   sufficient for public multi-memory admission.
4. **Documentation commit.** Record exact memory64 command deltas, typed/direct-tail
   ABI evidence, shared-context measurements, broad validation, public-suite baseline,
   remaining gates, and the next recursive slice.

## Completion gate

WebAssembly 3.0 is not complete. Completion still requires every mandatory area
to decode, validate, compile, instantiate, execute, round-trip through product
metadata/lifecycle rules, and pass the pinned official Release 3 suite with zero
unexplained failures or feature skips on linux/amd64, while preserving 1.0/2.0,
no-cgo operation, bounded memory, and hot-path performance. Arm64 must either
reach parity or remain explicitly gated and documented.
