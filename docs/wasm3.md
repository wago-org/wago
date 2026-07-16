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
| Tail calls | Decoder and validator understand direct, indirect, and reference tail-call forms. Tail results use covariant reference matching, while invalid narrowing remains rejected. Separate compile-only frontend bits admit bounded direct/indirect and typed-tail slices. | linux/amd64 has local register/wrapper `return_call`, tail-position host imports, retained integer cross-instance direct tails plus exactly `(i32, f64) -> f64` and `(f64) -> i32` with a separate fixed four-word root/nested record, per-table finite immutable-local `return_call_indirect` proofs, tagged same-instance internal/scalar-wrapper `return_call_ref`, retained typed cross-instance root/nested transfers, and one canonical funcref result returned in RAX. Exact pinned accounting is gap-free for all three files: `return_call` 47 commands / 3 modules / 33 assertions / 11 invalid; `return_call_indirect` 79 / 3 / 49 / 16 invalid / 11 malformed; `return_call_ref` 51 / 5 / 35 / 11 invalid. Other float/oversized direct signatures, mutable/imported/exported/host-descriptor indirect tables, foreign-float/general reference-result tails, snapshots, public admission, and arm64 remain fail-closed. | 🚧 All staged official tail files are gap-free; not a public product claim. |
| Typed function references | `ref.func` has the declared non-null indexed function type. Indexed references match by bounded coinductive structural equivalence across duplicate and recursive groups, including function/struct/array shapes, supers, and descriptor metadata. `call_ref` rejects abstract funcref while accepting nullable indexed references for dynamic null traps. `br_on_non_null` validates a label prefix plus non-null reference branch payload and consumes the reference on null fallthrough. Typed/tail opcodes contribute exact required-feature bits. Iteration 31 enforces recursive-group scope for relative indices and compares whole recursive groups, closing the five pinned invalid recursive/indexed gaps with exact `ErrUnknownType`/`ErrTypeMismatch` results on AST and byte-backed paths. | The internal gate admits indexed signatures/storage, explicit func/extern block types, `ref.null`, `ref.is_null`, `ref.as_non_null`, both null branches, `select`, `call_ref`, and bounded typed-tail contexts. The 14-file schema-2 matrix accounts 422 commands: 50 modules, 211 assertions, 65 invalid, 2 malformed, and 1 unlinkable pass; 12 exact gates leave 36 dependents blocked, with zero hidden failures. `call_ref` is gap-free at 4 modules / 27 assertions / 4 invalid. Cross-instance function imports compare exact structural signatures across shifted and recursive graphs; nested-reference producers survive logical close and release on consumer teardown. Empty source recursive groups compact to dense product group IDs while preserving absolute indexes and codec v27. Remaining measured gates are 11 GC and 1 exception-reference. Typed/tail snapshots, public admission, broader tails, GC/EH integration, and arm64 remain fail-closed. | 🚧 Complete measured non-GC validator/execution accounting except official modules inseparably mixed with GC/EH; no public execution claim. |
| GC | Recursive types, instructions, descriptor lowering, collector profiles, exact `RootSet` scanning, and a validated target-neutral native root-map metadata contract exist. | The compiler describes fixed EH payload slots by post-prologue frame offset and distinguishes collector-owned compact `gc.Ref` handles from funcref producer-lifecycle roots. Catch-all maps derive uniform ownership from all bounded tags and reject scalar/reference or GC/funcref conflicts. The narrow local funcref EH product initializes and clears an un-published lifecycle root, but runtime safepoint publication/consumption, WasmGC opcode lowering, allocation calls, GC barriers/remark, tag-discriminated mixed maps, and teardown scanning remain unconnected. | 🚧 Runtime and metadata foundation plus one non-collector local lifecycle proof; no WasmGC execution claim. See `docs/gc.md`. |
| Exception handling | Tags, `throw`, `throw_ref`, and `try_table` decode and validate strictly across AST and byte-backed paths; non-empty tag results reject, `noexn <: exn`, bottom heaps remain beneath indexed function heaps, and throw operands are exact. Catch payload/depth typing is complete for `catch`, `catch_ref`, `catch_all`, and `catch_all_ref`: reference catches produce non-null `(ref exn)` and may widen to nullable labels. The intentional source-only malformed `try_table` lines 339/344 remain rejected. | Strict schema-2 accounting covers `exceptions/{tag,throw,throw_ref,try_table}` plus mixed `ref_null`: 147 commands / 11 modules / 66 assertions / 16 invalid / 2 malformed / 2 unlinkable / 2 exact gates / 32 blocked, with zero hidden failures. The complete official `exceptions/try_table` file is gap-free under staged admission at 5 modules / 45 assertions / 9 invalid / 2 malformed. linux/amd64 explicit bounds admits at most 9 tags, 24 `try_table`s/module, 8 ordered clauses/table, 4 nested seven-word handlers, and 4 fixed three-word exception roots/function. Exact retained `() -> ()` cross-instance calls carry the handler in RBP and true tails discard dead scopes. One exact local-only tag payload may carry a non-null indexed `() -> ()` funcref from one declarative local descriptor: catch/catch_ref/catch-all preserve canonical identity, roots zero before handler publication and clear on immediate exn drop, teardown and codec-v27 metadata are bounded, and repetition is allocation-free. Nullable, extern/GC, foreign, wider, escaping-root, tail, host, snapshot, public, guard, and arm64 variants remain closed. | 🚧 All official EH modules except two mixed WasmGC `ref_null` leaders execute internally; not a public product claim. |
| Multi-memory | Indexed immediates and compact imports decode/validate strictly on AST and byte-backed paths; default Release 2 admission still rejects them explicitly. | Exact product directories, policy accounting, duplicate aliases, codec v27, every indexed scalar/SIMD/bulk/data operation, snapshot-v3 owned-local state, and bounded shared-memory co-tenants are staged on linux/amd64 explicit bounds. A finite proof admits exact native directories plus optional imported scalar-global pointers and exactly one bounded imported funcref table under a numeric-signature, no-element, no-ref.func/indirect-call, null/get/set/size-only scan. Retained scalar direct imports may re-enter producers that use the exact same memory-0 mapping: each eligible instance owns one stable 256-byte arena image, native calls save/install/restore images recursively, and trap recovery saves the image named by the active basedata slot. Root/nested calls now compose with imported numeric-global pointers and the sole imported funcref table simultaneously while shared `memory.grow`, global updates, table state, nested traps, concurrency, independent memory/global/table/function close ordering, and steady-state allocation freedom remain proven. Host callbacks, foreign-memory bindings, imported tail calls, broader reference/table/passive state, codec serialization of live bindings, imported/shared snapshots, guard mode, public admission, and arm64 remain fail-closed. The complete 42-file matrix remains gap-free at 913 commands, 79 modules, 771 assertions, 4 invalid, 22 unlinkable, and 20 uninstantiable cases. | 🚧 Complete official family accounting and bounded internal execution; not a public product claim. |
| memory64 | Limits, i64 address typing, 64-bit memarg offsets, and operation validation are present. The staged support pass admits size/grow, integer/float scalar memory operations, every SIMD memory load/store/extend/splat/zero/lane form, active and passive data lifecycle, and `memory.copy`/`memory.fill`. Core validation rejects limits above 2^48 pages and accepts the exact maximum. | One linux/amd64 explicit-bounds path accepts exactly one non-shared local or instance-exported imported memory. Valid declared maxima through 2^48 pages persist exactly in memory directories, codec v27, inspection, imports/re-exports, policy, and managed accounting whenever the minimum remains allocatable; only the direct memory-0 execution reservation is capped at 65,535 pages. No-maximum declarations preserve `HasMax=false` under that finite reserve. Unavailable growth returns `-1` without changing size, and arithmetic/policy/managed-budget overflow rejects fail-closed. Import matching preserves provider max/no-max identity across re-export, shared grow visibility is exact, and producer roots attach/roll back transactionally without increasing the 40-byte lifecycle sidecar. Scalar/SIMD operations check address+offset+width carry, exact lane/end bounds, and trapping-store atomicity. Active data preserves validated i64 programs in the codec expression field. Passive `memory.init` keeps zero-extended i32 source/length with an i64 destination; full-u64 carry/end, source bounds, drop state, zero-length-after-drop, trap atomicity, and reload are proven. Bulk copy/fill checks both full-u64 ranges before writes and preserves overlap. The complete sixteen-file non-table matrix is gap-free at 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / 0 gates / 0 blocked, with zero hidden failures. Mixed memory32/memory64 imports reject before attachment. Host memory64 construction, shared/multi-memory execution, unallocatable minima, guard mode, public admission, snapshots, and arm64 remain gated. | 🚧 Bounded local/imported scalar/SIMD/active+passive-data/copy/fill execution and complete gap-free non-table family accounting; product/platform admission remains staged. |
| table64 | Limits and i64 index/result typing have AST and byte-backed validator coverage, including table.init's i64 destination with i32 element source/length, table.copy's per-table/minimum-width operands, exact u64 declaration limits, and malformed-above-u64 LEB rejection. | The complete nine-file linux/amd64 explicit-bounds family is gap-free at 2,802 commands / 107 modules / 2,600 assertions / 81 invalid / 0 malformed / 0 gates / 0 blocked. Sole/local-or-instance-import funcref operations, two-table mixed-width operations, local externref slices, and exact table32/table32/table64 passive init/drop/copy/call-indirect modules execute with retained imported descriptors, full-u64 checks, exact traps, and transactional lifecycle. Inert table64 declarations retain exact maxima through `2^64-1` in u64 metadata and codec v27 while allocating only the minimum; declaration-only two-local and `spectest.table64` imported/local no-maximum products preserve index order, zero-minimum descriptors, no-max identity, policy accounting, rollback, and close-order release. Ordinary inert table32 maximum-only declarations use the same exact-declaration/minimum-storage split, preserving Release 2 conformance. The 16,384-entry funcref and 1,024-entry externref execution reservations remain explicit implementation bounds. Broader imported copy/init/grow/indirect, guard mode, public admission, snapshots, and arm64 remain fail-closed. | 🚧 Complete official table64 family accounting and exact bounded product representation are proven internally; product/platform admission remains staged. |
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

The compiled codec is now version 27. Version 21 introduced deferred scalar
initializer/offset programs. Version 22 added exact structural type graphs,
value-type pools, function signature references, full-width required-feature
bits, and strict recursive/index/ABI validation. Version 23 added exact indexed
memory declarations/imports/exports and the direct memory-0 execution cache.
Version 24 added the exact target memory index for every active data segment.
Version 25 replaced persisted compact 32-bit function signature discriminators
with 64-bit SHA-256-derived structural keys used by native descriptors and
indirect/reference call checks. Version 26 added the exact table32/table64 address
form to every persisted local/imported table record. Version 27 adds a bounded
exception-tag directory containing each exact structural type index, import key,
and export alias map; runtime owners, active handlers, and exception-root addresses
remain deliberately absent.

Version 26 and older blobs are rejected explicitly: interpreting an old table
record without its address form, treating an older 32-bit ID as a native key, or
dropping an active-data memory index would be unsafe. Exact
count bounds are checked before allocating the widened key slice. Extended-const
source syntax remains compiled into initializer metadata rather than re-decoded
from the original Wasm expression. Typed-reference, multi-memory, memory64, and
table64 artifacts still fail public load because their executable feature bits are not
advertised; codec support is representation work,
not admission. Iteration 12 fixes required-feature accounting for `call_ref`, typed
null control, and all tail-call opcodes. Iteration 13 changes no codec version:
compact import grouping is source-binary syntax and expands into the existing exact
import metadata, while the typed-tail admission bits remain compile-only in the
non-serialized code-cache sidecar. Public codec load therefore cannot accidentally
inherit either staged execution gate. Iteration 21 keeps codec v26 unchanged and
explicitly refuses to serialize retained same-memory native bindings because their
linked code contains live instance addresses and stable basedata-image pointers;
private memory64 passive-data and table64 products continue to round-trip under their
existing staged test loaders. Iteration 22 also keeps codec v26 unchanged: no-maximum
memory64 declarations already persist exact `HasMax=false` metadata separately from the
finite execution reservation, table64 fill adds no product field, and the new direct-tail
shape remains a compile-only live binding. Iteration 23 again keeps v26 unchanged:
memory64 handles consume the already-persisted address-form bit at import matching,
table64 i64 active offsets reuse the existing initializer-expression field, and the
combined imported-global/native-call proof adds no serialized live-binding state. Iteration 24 also keeps v26 unchanged: imported memory64 declarations already persist exact address/max forms, private no-maximum table64 uses the existing `HasMax=false` record, `call_indirect` adds no metadata, and imported-table/native-call composition remains an unserializable live binding. Iteration 25 again keeps v26 unchanged: table records already separate exact `HasMax`/`Addr64` type metadata from finite runtime capacity, `table.copy` adds no product field, and the simultaneous imported-global/table/native-call binding remains intentionally unserializable. Iteration 26 also keeps v26 unchanged: exact u64 memory maxima already fit memory-directory records, passive/declarative table segment state already persists in the existing element records, and two-local mixed-address table metadata already uses the per-table `Addr64` bit plus export directory. Iteration 27 again keeps v26 unchanged: indexed size/get/set/grow/fill consume the existing per-table descriptor directory, while passive/declarative init/drop reuses the persisted element records and original segment indexes. Iteration 28 also keeps v26 unchanged: exact externref element types, no-maximum declarations, per-table address forms, limits, and directories were already persisted; the 1,024-entry externref growth reservation is runtime policy rather than Wasm type metadata; and official host-token replay remains test-only live state. Iteration 29 again keeps v26 unchanged: table records already encode limits as u64, so local `TableMax`, additional-table maxima, and public `TableMetadata.Min/Max` now retain exact table64 declarations through `2^64-1` without changing the wire record. Runtime capacity is derived separately and collapses only exact inert, unexported, operation-free declarations to their allocatable minimum; a focused Release 2 regression proves the same split still admits oversized inert table32 declarations. The three-table init/call-indirect shape and declaration-only imported/local directory add no persisted field or serializable live binding. No fixed runtime, descriptor, basedata, or lifecycle-sidecar layout grows. Iteration 30 also keeps v26 unchanged. Empty source recursive groups now map to dense `DefinedTypeDescriptor.RecGroup` values while every absolute type index and recursive reference still uses the original flattened type space; this corrects metadata validation without a wire field or version change. Exact cross-instance signature matching consumes the already-persisted type graph. Typed null-control required-feature bits survive private reload, public load remains rejected, and snapshots still reject unresolved descriptor state. No fixed runtime, native descriptor, basedata, or lifecycle layout changes. Iteration 33 advances to v27: local and imported declaration-only tag products now round-trip exact structural/member identity, aliases, and re-exports through ordinary transactional instantiation. Function-free local declaration tags may also enter snapshot-v3 products because each restored instance creates fresh local identity while preserving aliases; imported tags and every module with executable EH functions remain rejected. Retained same-memory native bindings continue to reject serialization explicitly, now naming codec v27.

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

### Iteration 18 memory64 data, reference-result tails, and imported globals

Iteration 18 advances three bounded lifecycle/ABI areas without opening a public
Core 3 gate:

1. Active data segments are now admitted by the bounded local memory64 path. Their
   validated i64 constant-expression bytes are retained in the existing `OffsetInit.Expr`
   field, so codec v25 and fixed metadata layouts do not change or truncate high bits.
   Instantiation evaluates the offset as i64, rejects u64 offset+length carry before
   addition, checks against the current initial size before copying, and performs no
   partial copy on rejection. Passive data remains gated, and snapshot preflight now
   rejects memory64 explicitly. The six-file matrix improves from 1 module / 8 assertions /
   42 gates / 614 blocked commands to 7 modules / 92 assertions / 36 gates / 530 blocked,
   with 83 invalid and 59 malformed cases unchanged. All six `float_memory64` modules
   and their 84 assertions are green.
2. The final pinned `return_call_ref` module now compiles and instantiates. Under the
   staged tail sidecar only, a numeric-parameter signature with one funcref result may
   use the existing register ABI and return its canonical descriptor pointer in RAX.
   Covariant reference result types share that one-slot native shape after validation;
   direct and tailed public tokenization both resolve to the same function identity.
   The file is gap-free at 51 commands, 5 modules, 35 assertions, and 11 invalid modules.
   Foreign wrappers, floats, multiple/general reference results, snapshots, public
   admission, and arm64 remain gated.
3. The shared-memory basedata proof now admits finite imported numeric-global arrays.
   Only i32/i64/f32/f64 imports are eligible; local, reference, and vector globals remain
   excluded. Explicit global owners gain ordinary importer retention, so host-created
   storage cannot close while a tenant's arena-backed pointer array is live. Owner and
   tenant calls continue to serialize one fixed basedata image through the memory
   lifecycle mutex. A 1,000+1,000 concurrent owner/tenant test and the race detector are
   green; local globals still reject before live instantiation. The official 42-file
   multi-memory matrix remains gap-free and byte-identical.

No compiled codec, snapshot format, fixed runtime, basedata, descriptor, or public
product version changes. Five 500 ms samples measured imported-global tenant calls at
78.63-82.27 ns/op, canonical funcref-result tails at 97.15-99.04 ns/op, memory64 f64
loads at 39.01-39.67 ns/op, and memory64 integer store/load at 38.13-40.92 ns/op; every
sample reported 0 B/op and 0 allocations/op.

The official public Release 3 schema-2 inventory remains byte-for-byte unchanged:
144 green/114 red files, 1,691 passed/535 skipped modules, and 51,765 passed/5
failed/6,268 skipped assertions. Release 1 and Release 2 remain zero-gap.

### Iteration 19 memory64 SIMD, direct cross tails, and imported tables

Iteration 19 advances three bounded native/lifecycle areas without opening a public
Core 3 gate:

1. The one-local bounded memory64 path now executes every SIMD memory form:
   `v128.load/store`, six load-extend operations, four splats, two zero loads, and
   all eight lane loads/stores. Frontend and hint walkers consume u64 SIMD memarg
   offsets after module validation; amd64 routes memory64 SIMD addresses through the
   same checked u64 base+offset+width path as scalars. Focused coverage proves carry,
   width/lane end traps, trapping lane-store atomicity, AST/byte-backed admission,
   and byte-for-byte unchanged memory32 SIMD code. The six pinned memory64 files do
   not contain a newly admissible bounded SIMD module, so their exact accounting is
   unchanged at 807 commands / 7 modules / 92 assertions / 36 gates / 530 blocked /
   83 invalid / 59 malformed.
2. A retained int-register cross-instance direct `return_call` no longer uses the
   typed-reference descriptor path. Link-time immutable wrapper/home addresses feed
   a separate fixed four-word nested record; root adapters discard their continuation,
   while nested callers restore caller basedata/module context plus up to two integer
   results through a local trampoline. Producer roots transfer transactionally through
   existing import attachment. Million-step root/nested calls, 10,000 repeated
   transfers, logical producer close, foreign trap recovery, snapshot/public gates,
   and oversized-signature link rejection pass without recursion-dependent state.
3. The shared-memory basedata serializer now admits exactly one bounded imported
   funcref table. The compile-time proof requires an explicit max <=65,535, numeric-
   only function signatures, no elements, no local/second table, no `ref.func` or
   indirect/reference calls, and only null/get/set/size table operations. Independent
   memory and table owners remain retained through tenant close. An out-of-bounds
   `table.set` leaves the prior descriptor intact, a successful null overwrite remains
   visible after both owners logically close, and a 1,000+1,000 concurrent owner/
   tenant test plus the race detector pass. `table.grow` and broader table/reference
   contexts remain explicit instantiation failures.

No compiled codec, snapshot version, fixed runtime/basedata/descriptor layout, or
public product version changes. Five 500 ms samples measured memory64 `v128.load` at
35.03-35.82 ns/op, retained direct cross-instance tails at 60.97-61.67 ns/op, and
sole imported-table tenant calls at 108.7-109.5 ns/op; every sample reported 0 B/op
and 0 allocations/op.

The official public Release 3 schema-2 inventory remains byte-for-byte unchanged:
144 green/114 red files, 1,691 passed/535 skipped modules, and 51,765 passed/5
failed/6,268 skipped assertions. Release 1 and Release 2 remain zero-gap.

### Iteration 20 memory64 bulk, table64 beachhead, and mixed direct tails

Iteration 20 advances three bounded native/product areas without opening a public
Core 3 gate:

1. The one-local bounded memory64 path now executes `memory.copy` and `memory.fill`.
   Destination, source, and length are full i64 operands. amd64 checks u64 carry
   before comparing against the bounded byte-size cache, validates both copy ranges
   before writing, preserves memmove overlap semantics, and leaves memory untouched
   on every reached trap. Constant-size memory32 unrolling remains separate; enabling
   the staged bit leaves ordinary memory32 bulk code bytes unchanged. Passive
   `memory.init`/`data.drop`, imports, shared/multi-memory, unbounded/excessive
   reservations, guard mode, snapshots, public admission, and arm64 remain gated.
2. A first table64 execution/product slice runs on linux/amd64 explicit bounds. It
   accepts exactly one local funcref table with an explicit maximum no greater than
   16,384 entries and no element segments or initializer expression. `table.size`
   returns i64; `table.get/set` consume i64 indexes; full-width comparison makes
   2^32 and larger indexes trap rather than truncate. Table32 instruction bytes are
   unchanged. Codec v26 persists the exact table address form for every local/imported
   table record and rejects v25 and older blobs; `ModuleMetadata.Tables` reports
   `Addr64`. Public load/admission, imports, multiple tables, grow/fill/copy/init/
   indirect calls, snapshots, guard mode, and arm64 remain fail-closed.
3. The separate retained direct cross-instance tail record now admits exactly
   `(i32, f64) -> f64` in addition to its integer shapes. Arguments continue through
   the target wrapper's fixed tail bank; nested callers restore the sole result slot
   into XMM0 before resuming their internal continuation. Root and nested million-step
   transfers, 10,000 repetitions, producer logical close, foreign trap recovery, and
   continued rejection of other float shapes pass without allocation.

The compiled fixed layouts remain `Compiled=712`, `Instance=792`, native descriptor
32 bytes, and basedata 256 bytes. `tableDef` grows from 48 to 56 bytes so additional
indexed tables can retain an exact address-form bit; the staged one-table table64
fixture does not allocate an extra-table record. Five 500 ms samples measured
memory64 64-byte fill at 40.14-41.75 ns/op, table64 size at 35.02-42.10 ns/op, and
mixed-float direct cross-instance tails at 61.37-63.30 ns/op; all reported 0 B/op
and 0 allocations/op.

The official public Release 3 schema-2 inventory remains byte-for-byte unchanged:
144 green/114 red files, 1,691 passed/535 skipped modules, and 51,765 passed/5 failed/
6,268 skipped assertions. The complete staged multi-memory and tail matrices remain
gap-free, and the six-file memory64 matrix remains 807 commands / 7 modules / 92
assertions / 36 gates / 530 blocked / 83 invalid / 59 malformed. Release 1 and
Release 2 remain zero-gap.

### Iteration 21 passive memory64, table64 grow, and native co-tenant re-entry

Iteration 21 advances three bounded lifecycle/execution areas without opening a
public Core 3 gate:

1. The one-local bounded memory64 path now executes passive `memory.init` and
   `data.drop`. The destination remains i64, while the passive source offset and
   length retain their Core 3 i32 types and are explicitly zero-extended before
   native arithmetic. The backend checks destination u64 carry and end bounds plus
   passive source bounds before any copy. Focused coverage proves trap atomicity,
   successful drop, nonzero post-drop failure, zero-length-after-drop success,
   codec-v26 private reload, snapshot rejection, and byte-for-byte unchanged
   memory32 code. The official six-file accounting is unchanged at 807 commands /
   7 modules / 92 assertions / 36 gates / 530 blocked / 83 invalid / 59 malformed.
2. The bounded local funcref table64 path now executes `table.grow`. The delta and
   previous-size result are i64; full u64 delta/addition checks run before comparing
   the explicit maximum, failure returns all-ones (`-1`) without changing the table,
   and successful growth publishes the old size. Externref and every imported,
   multiple-table, element/initializer, fill/copy/init/indirect, snapshot, guard,
   public, and arm64 shape stay rejected. A nine-file supplementary runner uses the
   pinned official interpreter only for the `module definition` syntax WABT 1.0.41
   cannot parse. Exact accounting is 2,802 commands / 68 modules / 2,330 assertions /
   39 expected feature gates / 270 blocked / 81 invalid / 0 malformed, with zero
   unexpected compile/link/assertion gaps.
3. Restricted shared-memory tenants may now make retained direct native calls into
   producers that use the exact same memory-0 mapping. Every eligible instance owns
   one stable 256-byte arena-backed basedata image; offset 56 names the currently
   installed image. amd64 saves the caller image, installs the callee image, republishes
   this execution's trap/fence cells, and restores recursively on return. A nested
   trap that bypasses restore is recovered by saving the image named by the active
   slot before the outer mutex restores its baseline. Shared `memory.grow` updates are
   copied into caller images and R15 is refreshed. Root and two-level nested calls,
   trap recovery, 400 concurrent calls under the race detector, producer/tenant close
   ordering, exact +256-byte arena accounting, and codec/snapshot/public/guard/arm64
   rejection pass. Host callbacks, foreign-memory bindings, imported `return_call`,
   references, passive tenant state, and broader table contexts remain fail-closed.

The fixed basedata bank remains 256 bytes and `Compiled`/`Instance` layouts do not
change. Eligible instances add exactly one 256-byte off-heap image plus one bounded
256-byte Go scratch image; ordinary instances allocate neither. Five benchmark
samples for a two-level same-memory native call measured 242.3-268.6 ns/op, 0 B/op,
and 0 allocations/op. Earlier focused samples measured memory64 passive init at
38.25-40.63 ns/op and table64 grow-by-zero at 36.96-38.42 ns/op, also with 0 B/op
and 0 allocations/op.

The public Release 3 schema-2 inventory reproduced byte-for-byte at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. The
42-file multi-memory matrix remains gap-free at 913 commands / 79 modules / 771
assertions, memory64 remains exactly 807 / 7 / 92 with its pinned gates, and the new
nine-file table64 matrix remains exactly 2,802 / 68 / 2,330 with 39 gates and zero
hidden failures. Release 1 and Release 2 remain zero-gap.

### Iteration 22 no-maximum memory64, table64 fill, and a mixed direct tail

Iteration 22 advances three bounded policy/execution/ABI areas without opening a
public Core 3 gate:

1. The one-local non-shared memory64 path now accepts a declaration without an
   explicit maximum. Exact `HasMax=false` limits survive metadata and codec-v26
   reload; the implementation reserves at most 65,535 pages, policy and managed-
   instance budgets charge that finite reservation, and `memory.grow` beyond the
   available reserve returns `-1` without changing size. This is permitted resource
   failure, not a synthetic declared maximum. The change removes all 36 measured
   gates and unblocks all 530 dependents in the six-file matrix: 807 commands now
   report 43 modules / 622 assertions / 83 invalid / 59 malformed with zero feature
   gates, blocked commands, or hidden failures. The delta is +36 modules and +530
   assertions. Declared maxima above 65,535 pages, excessive minima, imports, shared/
   multi-memory, snapshots, guard mode, public admission, arm64, and the ten remaining
   memory64 files outside this matrix remain fail-closed or unaccounted.
2. The bounded local funcref table64 path now executes `table.fill`. Start and
   length are i64; amd64 checks full-u64 addition carry and exact end bounds before
   snapshotting the source descriptor or writing any entry. Successful non-null
   fills, zero-length-at-boundary behavior, high operands, trap atomicity, codec-v26
   private reload, AST/byte-backed admission, and byte-identical table32 code are
   proven. The nine-file matrix remains exactly 2,802 commands / 68 modules / 2,330
   assertions / 39 gates / 270 blocked / 81 invalid because the official fill
   modules first encounter still-gated elements/initializers or other table shapes.
3. The retained direct cross-instance `return_call` transition now admits exactly
   `(f64) -> i32` in addition to integer shapes and `(i32, f64) -> f64`. Raw f64
   arguments continue through the fixed tail bank; nested callers restore the one
   integer result from the fixed four-word record into RAX. Root and nested calls,
   10,000 repetitions, foreign traps and recovery, producer logical close, public
   admission failure, and continued rejection of `(f64) -> f64` and oversized shapes
   are proven. The pinned `return_call` file remains gap-free at 47 commands / 3
   modules / 33 assertions / 11 invalid.

No codec, snapshot, fixed runtime, basedata, descriptor, or product-layout version
changes. Five benchmark samples measured no-maximum memory64 `memory.size` at
34.57-35.87 ns/op, table64 zero-length fill at 37.53-38.22 ns/op, and the exact
`(f64) -> i32` retained direct tail at 115.7-139.4 ns/op; every sample reported
0 B/op and 0 allocations/op.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 1,691
passed / 535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions.
Release 1 and Release 2 remain zero-gap. The six-file memory64 matrix is now gap-free,
while complete memory64 family accounting, table64 elements/copy/init/indirect,
exceptions, GC, public admission, and arm64 execution remain incomplete.

### Iteration 23 complete memory64 accounting, table64 initialization, and composed re-entry

Iteration 23 advances three bounded accounting/product/lifecycle areas without opening
any public Core 3 gate:

1. The memory64 supplementary runner now replays all sixteen non-table files under
   `test/core/memory64`, including registrations, named/module definitions, imports,
   every action, and the pinned official-interpreter fallback for `memory64.wast`.
   Schema 2 records exact gate reasons per file and in aggregate. All 5,904 commands
   are accounted: 132 modules, 5,334 assertions, 292 invalid, 60 malformed, and 4
   unlinkable cases pass; 63 feature gates leave 3 commands blocked, with zero hidden
   compile/link/action failures. The gates are exactly 34 memory64 import/multi-memory
   shapes, 27 table64 call-indirect/import shapes, and 2 declarations outside the
   bounded reservation policy. The expanded import coverage found and fixed missing
   memory address-form matching: host/exported memory handles retain memory32 versus
   memory64 identity, and incompatible imports reject before attachment.
2. The sole bounded local funcref table64 path now admits table initializer expressions
   and active element segments. Validated i64 offset programs use the existing codec-v26
   expression field; instantiation evaluates the full u64 value, rejects offset+length
   carry or end overflow before writes, fills initializer entries first, and then applies
   active elements in order. A null active element overriding a non-null initializer,
   all-ones offset rejection, AST/byte-backed admission, private codec reload, and
   passive-element/public/product gates are proven. The nine-file matrix remains exactly
   2,802 commands / 68 modules / 2,330 assertions / 39 gates / 270 blocked / 81 invalid:
   wider operations, externref, and multiple-table shapes still dominate every reached
   official module, so the initializer prerequisite has a measured zero accounting delta.
3. Retained same-memory native re-entry now composes with imported mutable numeric-global
   pointers. A memory owner, intermediate function owner, root tenant, and independent
   host global execute root/nested calls, observe global updates, propagate shared growth,
   recover after nested traps, and run concurrent calls under the race detector. Memory,
   function, and global owners retain independently until the final root closes. Codec,
   snapshot, public, host-callback, foreign-memory, and imported-tail boundaries remain
   explicit. No eligibility or rollback widening was required; the prior finite proof
   was already compositional.

No codec, snapshot, fixed runtime, basedata, descriptor, or product-layout version
changes. Five benchmark samples measured memory64 `memory.size` at 35.75-37.03 ns/op,
initialized table64 `table.get`/`ref.is_null` at 36.46-36.78 ns/op, and composed nested
same-memory/global re-entry at 122.4-126.4 ns/op versus 121.5-122.6 ns/op without the
imported global. Every sample reported 0 B/op and 0 allocations/op.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. Release 1
and Release 2 remain zero-gap. Complete non-table memory64 accounting is now present,
but its import/table64/policy gates, wider table64 operations and shapes, exceptions,
GC, public admission, and arm64 execution remain incomplete.

### Iteration 24 bounded memory64 imports, table64 indirect calls, and table composition

Iteration 24 advances three bounded lifecycle/execution areas without opening a public
Core 3 gate:

1. Exactly one non-shared instance-exported memory64 import now executes on
   linux/amd64 explicit bounds. The importer may require a compatible min/max or no
   maximum; provider declarations preserve exact max/no-max identity across re-export,
   so a no-maximum provider is not mistaken for its finite 65,535-page implementation
   reservation. Imported growth is visible to producer and consumer, producer roots
   attach and roll back transactionally, codec-v26 metadata and policy accounting stay
   exact, and mixed memory32/memory64 imports reject before attachment. Host memory64,
   shared/multi-memory, snapshot, guard, public, and arm64 paths remain closed. The
   complete memory64 matrix moves by +14 modules and +20 expected unlinkables while
   removing all 34 memory64 import gates.
2. A sole local funcref table64 now executes `call_indirect`. Private non-growing
   no-maximum tables use finite initial-size capacity while retaining exact
   `HasMax=false` metadata. amd64 compares the full u64 index before entry addressing,
   then preserves null and 64-bit structural-signature traps; table32 instruction bytes
   are unchanged. Enabling this path exposed and fixed missing table address-form import
   matching, so table64 providers cannot satisfy table32 imports (or vice versa) before
   attachment. The official `call_indirect64` module and assertion are green. The
   sixteen-file memory64 matrix is now 5,904 commands / 150 modules / 5,335 assertions /
   292 invalid / 60 malformed / 24 unlinkable / 25 gates / 1 blocked. The nine-file
   table64 matrix is 2,802 commands / 70 modules / 2,330 assertions / 37 gates / 270
   blocked / 81 invalid: +2 modules and -2 gates, with no assertion delta because the
   indirect file is accounted in the non-table memory64 matrix.
3. Retained same-memory native re-entry now composes with the sole imported funcref
   table. Root/nested calls preserve table size/null/get/set state, shared growth, trap
   recovery, concurrency, and independent memory/table/function owner close ordering.
   Codec, snapshot, public, host-callback, foreign-memory, and imported-tail boundaries
   remain explicit. No eligibility or rollback widening was required; the existing
   finite serializer proof was already compositional.

No codec, snapshot, fixed runtime, basedata, descriptor, or product-layout version
changes. `Memory` remains 16 bytes, `memoryState` remains 40 bytes, `Compiled` remains
712 bytes, `Instance` remains 792 bytes, native descriptors remain 32 bytes, and
basedata remains 256 bytes. Five benchmark samples measured imported memory64 size at
35.74-36.90 ns/op, table64 `call_indirect` at 36.39-37.26 ns/op, and imported-table
nested same-memory re-entry at 164.4-168.5 ns/op versus 122.9-126.7 ns/op without the
table operations; every sample reported 0 B/op and 0 allocations/op.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. Release 1
and Release 2 remain zero-gap. Remaining staged memory64 gates are exactly 23 table64
imports plus 2 declarations outside bounded policy; broader table64 operations/shapes,
exceptions, GC, public admission, and arm64 execution remain incomplete.

### Iteration 25 bounded table64 imports, table copy, and simultaneous side state

Iteration 25 advances three bounded lifecycle/execution areas without opening a public
Core 3 gate:

1. Exactly one instance-exported funcref table64 may now be imported on linux/amd64
   explicit bounds. An explicit maximum remains capped at 16,384 entries. An exported
   or growing no-maximum provider preserves exact `HasMax=false` Wasm/codec metadata
   while using a finite 16,384-entry implementation reservation; private non-growing
   no-maximum tables retain initial-size capacity. Import matching proves min/max/no-max
   compatibility, exact table32/table64 address form, shared size/grow visibility,
   re-exported provider type, producer retention and failed-link rollback. Codec-v26,
   `ModuleMetadata`, `ImportSpec.Addr64`, and table policy accounting remain exact, while
   host table64 construction, externref, multiple-table, snapshot, guard, public, and
   arm64 paths stay closed. This removes all 23 prior table64-import gates from the
   complete memory64 matrix: it is now 5,904 commands / 167 modules / 5,335 assertions /
   292 invalid / 60 malformed / 30 unlinkable / 2 gates / 0 blocked. The delta is
   +17 modules, +6 expected unlinkables, -23 gates, and -1 blocked command.
2. A sole local funcref table64 now executes `table.copy`. Destination, source, and
   length are full i64 operands; amd64 checks u64 addition carry and exact end bounds
   for both ranges before writing, preserves memmove overlap, and leaves the table
   unchanged on every reached trap. Codec-v26 reload executes, and enabling the staged
   bit leaves table32 `table.copy` bytes unchanged. Imported table64 copy, multiple
   tables, externref, and passive-init contexts remain explicit gates. The official
   `table_copy64` file and complete nine-file table64 matrix have an exact zero accounting
   delta because multiple-table shapes remain the leading gate: 2,802 commands / 70
   modules / 2,330 assertions / 37 gates / 270 blocked / 81 invalid / 0 malformed.
3. Retained same-memory native re-entry now composes with imported numeric globals and
   the sole imported funcref table simultaneously. Root/nested calls prove global
   updates, table size/null/get/set state, shared growth, nested trap recovery,
   concurrency, and independent memory/global/table/function owner close ordering.
   Codec, snapshot, public, host-callback, foreign-memory, and imported-tail boundaries
   remain explicit. No runtime eligibility or rollback widening was required; the
   existing finite serializer proof was already compositional.

No codec, snapshot, fixed runtime, basedata, descriptor, or product-layout version
changes. `Compiled` remains 712 bytes, `Instance` remains 792 bytes, `Table` remains
64 bytes, native descriptors remain 32 bytes, and basedata remains 256 bytes. Five
benchmark samples measured imported table64 `table.size` at
71.59-74.22 ns/op, zero-length table64 `table.copy` at 38.22-42.57 ns/op, and the
simultaneous imported-global/table nested call at 168.0-174.4 ns/op. Three comparison
samples measured table-only composition at 161.3-168.0 ns/op, global-only at
124.4-125.2 ns/op, and the plain nested chain at 123.3-127.8 ns/op. Every sample
reported 0 B/op and 0 allocations/op.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. Release 1
and Release 2 remain zero-gap. Remaining staged memory64 gates are exactly 2 declarations
outside bounded policy. Broader table64 shapes/element lifecycle, exceptions, GC, public
admission, and arm64 execution remain incomplete.

### Iteration 26 exact memory limits and bounded table lifecycle/copy

Iteration 26 closes the bounded memory64 declaration matrix and opens two table64
execution slices without widening public Core 3 admission:

1. Valid memory64 declarations no longer acquire a synthetic 65,535-page declared
   maximum merely because that is the implementation reservation ceiling. The validator
   admits the exact Core 3 maximum of 2^48 pages and rejects larger limits. When the
   minimum is allocatable, exact maxima above 65,535 pages persist through memory
   directories, codec v26, metadata/inspection, imports/re-exports, policy accounting,
   and managed-instance accounting. Only the direct memory-0 reservation remains capped.
   Growth beyond available reserve returns `-1` atomically; u64-to-platform, policy, and
   aggregate-budget overflow reject before allocation or attachment. The complete
   sixteen-file matrix is now gap-free at 5,904 commands, 169 modules, 5,335 assertions,
   292 invalid, 60 malformed, and 30 unlinkable cases, with zero gates, blocked commands,
   or hidden failures. The iteration-25 delta is +2 modules and -2 gates.
2. A sole local finite funcref table64 now executes passive/declarative `table.init` and
   `elem.drop`. Validation retains the Core 3 operand split: destination is i64 while
   source and length are i32. amd64 explicitly zero-extends those segment operands,
   checks destination addition carry and exact end, checks source end against the current
   segment length, and performs no copy until both ranges pass. Repeated drop, declarative
   initially-dropped state, nonzero-after-drop traps, zero-length-at-boundary and zero-
   length-after-drop success, trap atomicity, codec-v26 reload, and byte-identical table32
   code are proven. Imported table64 init remains explicit. The official `table_init64`
   delta is exactly zero because its three remaining gated modules first require broader
   table shapes; the focused implementation proof is nevertheless end to end.
3. An exact two-local-table copy-only slice admits two finite local funcref tables with
   explicit maxima, including table64/table64 and mixed table32/table64 forms. Each
   destination/source operand is canonicalized to its table's address width; length uses
   the minimum width and is i64 only when both tables are table64. Both ranges, including
   u64 carry, are checked before mutation. Same-table overlap and cross-table copies use
   the existing descriptor stride and native table directory; traps leave both tables
   unchanged. Exact per-table metadata/exports, codec-v26 reload, and ordinary table32
   code stability are proven. Imports, externref, initializer expressions, no-maximum
   two-table declarations, and every table operation other than copy remain fail-closed.
   `table_copy64` becomes gap-free at 52 modules and 1,675 assertions; the valid
   `table_copy_mixed` module is admitted. Nine-file totals become 2,802 commands / 93
   modules / 2,352 assertions / 14 gates / 248 blocked / 81 invalid / 0 malformed. The
   iteration-25 delta is +23 modules, +22 assertions, -23 gates, and -22 blocked commands.

No compiled codec, snapshot, fixed runtime, basedata, descriptor, or lifecycle-sidecar
layout changes. Existing layout assertions remain `Compiled=712`, `Instance=792`,
`tableDef=56`, `Table=64`, native funcref descriptor=32, basedata=256, and
`memoryState=40` bytes. Five 500 ms benchmark samples measured no-maximum memory64 size
at 37.96-38.34 ns/op, imported memory64 size at 37.59-37.76 ns/op, zero-length table64
init at 42.20-42.78 ns/op, and zero-length two-local table64 copy at 38.64-39.10 ns/op;
every sample reported 0 B/op and 0 allocations/op.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. Release 1
and Release 2 remain zero-gap. Broader table64 operations/lifecycle, typed-reference
completion, exceptions, GC, public admission, and arm64 execution remain incomplete.

### Iteration 27 exact two-local table operations and lifecycle

Iteration 27 expands the exact two-local finite-funcref table slice without opening any
public Core 3 gate:

1. Two local tables now execute indexed `table.size/get/set` as well as the prior copy
   path. Table64 indexes remain full u64 while mixed table32 operands retain i32
   canonicalization. Null and non-null descriptor writes use the target table's native
   directory entry; high table64 indexes do not truncate; trapping writes leave both
   tables unchanged. Exact per-table address forms, limits, exports, and directory order
   survive codec-v26 reload, and enabling the staged bit leaves ordinary two-table
   table32 code byte-identical.
2. The same exact shape now executes `table.grow/fill`. Delta/start/length operands follow
   the target table's i32/i64 address form. Full-u64 addition carry, end, and capacity
   checks occur before writes; unavailable growth returns all-ones (`-1`) without changing
   size; successful growth updates the selected descriptor length and initializes entries
   from a snapshotted null/non-null descriptor. Fill checks its complete range before
   mutation. Source and codec-reloaded products behave identically, and ordinary table32
   bytes remain unchanged.
3. Passive/declarative `table.init`/`elem.drop` now target either exact local table,
   including mixed table32/table64 address forms. Destination width follows the selected
   table, while source and length remain zero-extended i32. Destination carry/end and
   segment source bounds precede copies; original segment indexes retain independent
   values/drop state; repeated drop, declarative initially-dropped state, nonzero-after-
   drop traps, and zero-length boundary/drop success are exact. Codec-v26 reload preserves
   both table metadata and all segment lifecycle records.

No codec, snapshot, fixed runtime, basedata, descriptor, or lifecycle-sidecar layout
changes. Existing assertions remain `Compiled=712`, `Instance=792`, `tableDef=56`,
`Table=64`, native funcref descriptor=32, basedata=256, and `memoryState=40` bytes.
Five 500 ms samples measured two-local table64 `table.size` at 36.25-36.92 ns/op,
`table.grow 0` at 37.28-38.98 ns/op, and zero-length `table.init` at 38.66-39.43 ns/op;
every sample reported 0 B/op and 0 allocations/op.

The pinned nine-file accounting remains exactly 2,802 commands / 93 modules / 2,352
assertions / 14 gates / 248 blocked / 81 invalid / 0 malformed, with zero hidden
failures. The iteration-27 deltas for `table_size64`, `table_get64`, `table_set64`,
`table_grow64`, `table_fill64`, and `table_init64` are each exactly zero: the remaining
official modules first require externref tables, no-maximum declarations, wider three/four-
table shapes, or `call_indirect` combined with those shapes. This is a measured boundary,
not an unrecorded skip.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. Release 1
and Release 2 remain zero-gap. Externref and wider table64 shapes, imported copy/init,
typed-reference completion, exceptions, GC, public admission, and arm64 execution remain
incomplete.

### Iteration 28 exact local externref table64 execution

Iteration 28 opens four bounded local externref shapes without widening any public Core 3
gate:

1. The official no-maximum `table_get64` and `table_set64` modules now execute through an
   exact two-local table64 externref/funcref read-write slice. The supplementary runner
   issues stable per-instance opaque externref tokens for each official `ref.extern` id
   and resolves returned tokens back to that identity instead of treating the text id as
   raw native bits. Null/non-null writes, active funcref initialization, full-u64 indexes,
   high-index non-truncation, trap atomicity, minimum-only descriptor capacity, exact
   metadata, and codec-v26 reload are proven. Both files become gap-free, adding 2 modules
   and 28 assertions while removing 2 gates and 28 blocked commands.
2. The exact official mixed table32/table64 no-maximum externref `table_fill64` module now
   executes. Table32 start/length operands are explicitly canonicalized to i32; table64
   uses full-u64 addition carry and end checks. Opaque-token and null snapshotting,
   zero-length-at-boundary behavior, trapping-write atomicity, minimum-only capacity,
   exact metadata, and codec reload are proven. `table_fill64` becomes gap-free at one
   module, 70 assertions, and 9 invalid modules; the iteration delta is +1 module,
   +70 assertions, -1 gate, and -70 blocked commands.
3. Externref `table.grow` now follows the selected table address form. Table64 consumes an
   i64 delta, performs full-u64 add/carry/max checks, returns the old size or all-ones i64
   `-1`, and publishes the selected directory length only after initialization succeeds;
   table32 retains i32 behavior. No-maximum externref growth reserves exactly 1,024 entries
   per local table while preserving `HasMax=false`. The sole-table `table_grow64` module
   and four-local `table_size64` module are gap-free, adding 2 modules and 57 assertions
   while removing 2 gates and 57 blocked commands. Per-table capacities, codec reload,
   token initialization, and atomic max/resource failure are exact.

The complete nine-file table64 matrix is now 2,802 commands / 98 modules / 2,507
assertions / 9 gates / 93 blocked / 81 invalid / 0 malformed, with zero hidden compile,
link, action, or assertion failures. The iteration-27 delta is +5 modules, +155 assertions,
-5 gates, and -155 blocked commands. Five operation files are now gap-free:
`table_get64`, `table_set64`, `table_fill64`, `table_grow64`, and `table_size64`.
The nine remaining gates are measured: `table64` has four oversized declaration maxima,
one declaration-only two-local no-maximum shape, and one mixed imported/local declaration;
`table_init64` has three table32/table32/table64 funcref modules combining passive init,
drop/copy, and table64 `call_indirect`, accounting for all 93 blocked assertions.

No codec, snapshot, fixed runtime, basedata, descriptor, or lifecycle-sidecar layout changes.
Existing assertions remain `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`,
native funcref descriptor=32, basedata=256, and `memoryState=40` bytes. Five 500 ms samples
measured exact externref table64 get at 47.79-51.69 ns/op, zero-length externref table64
fill at 47.28-50.24 ns/op, and externref table64 grow-by-zero at 48.84-50.82 ns/op; every
sample reported 0 B/op and 0 allocations/op.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. Release 1
and Release 2 remain zero-gap. Declaration/product completion, wider table init/indirect
contexts, imported copy/init, typed-reference completion, exceptions, GC, public admission,
and arm64 execution remain incomplete.

### Iteration 29 gap-free table64 family and exact declaration products

Iteration 29 closes all nine measured table64 family gates without widening the public
Core 3 feature bit:

1. The three official table32/table32/table64 `table_init64` modules now pass the exact
   three-local finite-funcref product scan. Imported functions are retained through active
   and passive element descriptors, table64 `table.init`, `elem.drop`, same-table
   `table.copy`, and table-2 `call_indirect`. The existing backend directory selects the
   third descriptor, keeps the i64 destination/index separate from i32 element source and
   length, and preserves null, signature, bounds, and trap atomicity. Linked structural
   table/element metadata reloads through codec v26. `table_init64` becomes gap-free at
   38 modules / 770 assertions / 67 invalid, adding 3 modules and 93 assertions while
   removing 3 gates and all 93 blocked commands.
2. Four valid declaration-only local funcref table64 modules with maxima 65,536,
   `2^32-1`, `2^32`, and `2^64-1` now instantiate with header/minimum-only storage while
   retaining the exact declared maximum in `Compiled`, deterministic inspection, policy
   input, and codec v26. `TableMetadata.Min/Max` and local table maximum storage are u64;
   the wire record was already u64, so codec v26 is unchanged. Values above u64 remain
   malformed LEB128, unallocatable minima reject, and adding an export or operation
   restores the 16,384-entry executable ceiling. The runtime-capacity split is shared
   with inert table32 declarations; a Release 2 regression caught and fixed an initial
   over-allocation, and the full Release 2 corpus remains zero-gap.
3. The declaration-only two-local no-maximum table64 module and exact
   `spectest.table64` imported-table-plus-local-table module now instantiate. The staged
   runner owns a bounded instance-exported table64 provider. Exact index order,
   `HasMax=false`, zero-minimum header-only local descriptors, table32/table64 mismatch
   rejection, policy accounting, codec reload, attachment rollback, producer retention,
   and close-order release are proven. Adding `table.size` or any other unproven operation
   returns to an explicit gate.

The complete nine-file table64 matrix is now gap-free at 2,802 commands / 107 modules /
2,600 assertions / 81 invalid / 0 malformed / 0 gates / 0 blocked, with zero hidden
compile, link, action, or assertion failures. The iteration-28 delta is +9 modules,
+93 assertions, -9 gates, and -93 blocked commands. All nine files are gap-free.

No codec version or fixed layout changed. Assertions remain `Compiled=712`,
`Instance=792`, `tableDef=56`, `Table=64`, native funcref descriptor=32,
basedata=256, and `memoryState=40` bytes. Five 500 ms samples measured sole-table
`table.size` at 37.01-38.31 ns/op, imported `table.size` at 72.40-75.57 ns/op,
zero-length table64 init at 78.77-86.65 ns/op, and table64 `call_indirect` at
61.59-82.18 ns/op; every sample reported 0 B/op and 0 allocations/op.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. Release 1
and Release 2 remain zero-gap. Table64's official family is complete only under staged
linux/amd64 explicit bounds; broader imported operations, snapshots, guard/public/arm64
admission, typed-reference completion, exception handling, GC, and full-suite admission
remain mandatory work.

### Iteration 30 typed-reference accounting, null control, and `call_ref` lifecycle

Iteration 30 advances the non-GC typed-reference surface without opening the public Core 3
feature bit:

1. `tests/spec-v3-staged-typed-reference.json` is a schema-2 inventory over 14 pinned
   top-level typed-reference and structural files: `br_on_non_null`, `br_on_null`,
   `call_ref`, `ref`, `ref_as_non_null`, `ref_func`, `ref_is_null`, `ref_null`, `select`,
   `type`, `type-canon`, `type-equivalence`, `type-rec`, and `unreached-valid`. The runner
   converts all files with pinned WABT 1.0.41 and requires the revision-stamped official
   interpreter for `ref_null`, `type-canon`, `type-equivalence`, and `type-rec`. It rejects
   omitted dependent commands, unknown gate text, hidden compile/link/action failures, and
   invalid modules that unexpectedly compile unless their exact suite line is pinned as a
   validator gap. Initial accounting was 422 commands / 35 modules / 202 assertions / 59
   invalid / 2 malformed / 1 unlinkable / 33 exact gates / 45 blocked commands. GC,
   exception-reference, and typed-function-reference gates are represented separately.
2. The exact non-GC null/control surface now executes through the existing one-slot
   reference lowering. `br_on_non_null` validates the label prefix separately from its
   appended non-null reference payload; a null fallthrough consumes the reference rather
   than leaking it onto the operand stack. amd64 decodes one-byte abstract and explicit
   `ref null`/`ref` block result types, carries the reference only on the taken branch, and
   preserves preceding label arguments. The staged frontend admits explicit func/extern
   and nofunc/noextern forms while continuing to reject any/eq/i31/none/struct/array and
   exn/noexn as GC/EH work. `br_on_non_null`, `br_on_null`, `ref`, `ref_as_non_null`,
   `ref_is_null`, and `unreached-valid` become gap-free; typed required bits survive codec
   v26 reload, public load and snapshots remain fail-closed, guard mode remains excluded,
   and arm64 remains unadvertised. This slice moves the matrix to 43 modules / 211
   assertions / 25 gates / 36 blocked commands: +8 modules, +9 assertions, -8 gates, and
   -9 blocked.
3. `call_ref` now accepts only nullable or non-null references to its selected indexed
   function type; abstract funcref no longer validates, while a typed null still reaches
   the required dynamic null trap. Cross-instance imports compare full parameter/result
   descriptors by bounded structural equivalence across independent shifted and recursive
   type graphs instead of comparing only compact ABI categories. Empty source recursive
   groups receive dense product group IDs without changing flattened type indexes. The
   official `call_ref` file is gap-free at 4 modules / 27 assertions / 4 invalid, all 21
   valid `type-equivalence` modules instantiate, and the leading function-only `type-rec`
   product round-trips. A shifted nested-reference import executes after producer logical
   close and releases the producer after consumer teardown. Local/global/table storage,
   null and wrong-signature traps, synchronous host boundaries, codec reload, and snapshot
   rejection remain covered by the focused typed suites. This slice adds 7 modules and 1
   expected-invalid result while removing 8 gates. Broad staged validation also exposed a
   stale compact-import test reload sidecar; the third commit was amended so the test-only
   codec proof restores its previously proven shared-basedata eligibility bit without
   serializing that live admission state.

The final iteration-30 matrix is 422 commands / 50 modules / 211 assertions / 60 invalid /
2 malformed / 1 unlinkable / 17 exact feature gates / 36 blocked commands, with zero
unexpected compile rejects, link rejects, action failures, or assertion failures. Relative
to the initial accounting commit, the delta is +15 modules, +9 assertions, +1 invalid,
-16 gates, and -9 blocked commands. The remaining gates are exact: 10 GC struct/array
modules, 1 GC any/none reference module, 1 exception-reference module, and 5 invalid
indexed/recursive modules that the validator still accepts. Thirty-two blocked commands
belong to the mixed GC/EH `ref_null` modules; four belong to gated GC shapes in `type-rec`.

No codec or fixed layout changed. Assertions remain `Compiled=712`, `Instance=792`,
`tableDef=56`, `Table=64`, native funcref descriptor=32, basedata=256, and
`memoryState=40` bytes. Five 500 ms samples measured staged null control at
46.73-47.21 ns/op and retained cross-instance `call_ref` at 40.48-51.55 ns/op; every
sample reported 0 B/op and 0 allocations/op.

The public Release 3 schema-2 inventory remains byte-for-byte unchanged at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. Release 1
and Release 2 remain zero-gap. Typed-reference public admission is still closed because
five strict-validator gaps, mixed GC/EH reference modules, broader typed-tail contexts,
live descriptor snapshots, and arm64 execution parity remain incomplete.

### Iteration 31 recursive validation and bounded exception handling

Iteration 31 closes the typed-reference validator debt, pins the complete exception family,
and opens the first executable exception-handling boundary without enabling the public Core
3 feature bit:

1. Recursive type-index validation now distinguishes in-group relative indices from prior
   absolute indices. Out-of-scope relative references reject as `ErrUnknownType`, and type
   equivalence compares the complete recursive groups containing projected members rather
   than treating a projected member as an implicit singleton group. Exact AST and byte-
   backed tests pin `type-equivalence` line 46 and `type-rec` lines 9 and 16 to
   `ErrUnknownType`, plus `type-rec` lines 37 and 46 to `ErrTypeMismatch`. The five-item
   validator allowlist is removed. The typed-reference matrix therefore moves from 60 to
   65 passing invalid modules and from 17 to 12 exact gates while preserving 422 commands,
   50 valid modules, 211 assertions, 2 malformed, 1 unlinkable, and 36 blocked commands.
   The only remaining gates are 11 GC shapes and 1 exception-reference shape.
2. `tests/spec-v3-staged-exception-handling.json` is a strict schema-2 inventory over
   `exceptions/tag`, `exceptions/throw`, `exceptions/throw_ref`,
   `exceptions/try_table`, and the exn/noexn portions of `ref_null`. It accounts 147
   commands, 16 invalid modules, 2 source-only malformed commands at `try_table` lines
   339 and 344, 15 exact feature gates, and 101 blocked dependents, with zero unexpected
   compile rejects, link rejects, action failures, or assertion failures. Observed gates
   are exact: 1 general catch-validator gate, 3 exception-reference/root gates, 5 general
   native-unwind gates, and 6 tag import/export/cross-module product gates. Unknown gate
   text, omitted dependent commands, and any hidden failure reject the matrix. Validator
   support added in the same accounting work rejects non-empty tag results, preserves
   `noexn <: exn`, keeps bottom heaps below indexed function heaps, and types `throw` and
   `throw_ref` as unreachable after consuming their operands.
3. One internal linux/amd64 explicit-bounds product now accepts exactly one local,
   non-exported tag with one or two `i32`/`i64` parameters, no results, and exactly one
   catch-only `try_table`. `throw` stores the tag and payload in a fixed six-word/48-byte
   native-stack handler record containing the prior handler, catch target, target stack
   and frame pointers, and two payload words. Nested local calls unwind directly to that
   record; catches restore payloads in declaration order; uncaught throws report trap code
   17 (`unhandled WebAssembly exception`). A prepared-call trap clears a stale active
   handler on the cold error path, so an unrelated trap inside a try cannot poison the
   next invocation. Focused tests cover caught and uncaught paths, nested calls, payload
   order, exact tag typing, trap recovery, 10,000 repeated catches, start-function failure
   without a partial instance, metadata, and close/teardown of the local-only product.

The product boundary remains intentionally narrow. Tag imports/exports, host imports,
cross-instance tag identity, multiple tags/tries/catches, `catch_ref`, `catch_all_ref`,
`throw_ref`, exception-reference values, GC payloads, tables, memories, globals, passive
state, guard-page mode, public admission, and arm64 execution all reject before native
execution. Transient `TagMetadata` is available through `ModuleMetadata`, but codec v26
serialization rejects staged EH explicitly instead of changing the persisted format;
snapshots likewise reject before capture. The EH active-handler pointer occupies the
existing 256-byte basedata arena at offset 152, and staged tag metadata lives in an
existing product sidecar, so fixed public/runtime structures remain `Compiled=712`,
`Instance=792`, `tableDef=56`, `Table=64`, native descriptor=32, basedata=256, and
`memoryState=40` bytes.

A temporary direct-backend measurement of the three-function EH fixture emitted 926 bytes
of railshot code: 198/463/254 bytes per function with 104/136/136-byte aligned frames and
2 spill slots each. The final linked `Compiled.Code` image was 1,068 bytes with entries at
0, 256, and 768. Every function in a staged EH module reserves the fixed 48-byte handler
region before ordinary spills; no heap exception object or unbounded handler stack exists.
Five 500 ms benchmark samples measured the caught nested-call path at 41.29-42.29 ns/op,
all at 0 B/op and 0 allocations/op. The full `CGO_ENABLED=0` suite passes.

The public Release 3 schema-2 baseline remains byte-for-byte unchanged at 1,691 passed /
535 skipped modules and 51,765 passed / 5 failed / 6,268 skipped assertions. This bounded
internal slice does not satisfy the completion gate: the official EH modules still lead
with broader validator, product, unwind, and exception-root requirements; GC, public
feature admission, snapshots/persistence, and arm64 parity also remain incomplete.

### Iteration 32 complete catch validation, generalized scalar unwind, and tag products

Iteration 32 closes the remaining exception decoder/validator gate, broadens the local
scalar native ABI without materializing exception references, and makes declaration-only
tag identity a bounded link product. The public Core 3 feature bit remains disabled.

1. `try_table` catch typing is now identical on the decoded AST and direct byte-backed
   validator paths. `catch` contributes the selected tag's parameter vector;
   `catch_ref` contributes that vector followed by non-null `(ref exn)`;
   `catch_all` contributes no values; and `catch_all_ref` contributes non-null
   `(ref exn)`. Ordinary reference covariance permits either reference catch to target a
   nullable exn label, but scalar/missing/extra payloads reject with `ErrTypeMismatch`.
   Catch depths are checked against the enclosing outer control stack and unknown depths
   reject with `ErrUnknownLabel`. Exact positive and negative tests cover all four catch
   kinds on both validator paths. The former general-catch validator allowlist is gone,
   while source-only malformed `exceptions/try_table` lines 339 and 344 remain explicit.
2. The linux/amd64 explicit-bounds execution slice now admits at most eight tags, sixteen
   `try_table` constructs per module, eight ordered catch clauses per table, and four
   simultaneously nested handler records. A clause may be `catch` or `catch_all`; tag
   mismatch continues through later clauses or the prior handler. Up to two scalar
   payload words preserve exact `i32`, `i64`, `f32`, and `f64` bits and declaration order.
   Tests cover ordered tag selection, catch-all fallback, pair order, nested and sequential
   tables, nested local calls, uncaught propagation, start failure, stale-handler recovery,
   and post-trap reuse. `throw_ref`, `catch_ref`, `catch_all_ref`, GC/reference payloads,
   tail transfer, guard mode, public admission, arm64, and every cross-instance native
   throw remain rejected before execution.
3. Tag declarations are now exact bounded products. `TagMetadata` carries index, type
   index, scalar parameters, import module/name, and sorted exports; `ImportTag` exposes
   imported types through inspection, and generated facade aliases expose `Tag` and the
   expanded metadata. Stable `ExportedTag` handles preserve duplicate alias identity,
   re-exports forward the producer handle, and import matching compares the complete
   recursive group plus the selected member position. Deduplicated attachments retain
   one producer resource root, roll back transactionally on a later mismatch, and release
   in independent producer/consumer close order. `Policy.MaxTags` bounds declared plus
   imported tags. Codec v26 and snapshots reject these staged products explicitly because
   restorable identity is not encoded; importing/exporting a tag in any throwing module is
   declaration-only until basedata handler transfer is proven.

The strict EH schema-2 matrix improves from iteration 31's 5 passing modules / 15 gates /
101 blocked commands to 5 modules / 9 assertions / 8 gates / 90 blocked commands while
preserving 147 commands, 16 invalid modules, 2 source-only malformed commands, and adding
2 expected unlinkables from official tag-link mismatch coverage. The removed gates are
the single decoder/validator gate and all six tag-product gates. The remaining exact gates
are three exception-reference/root shapes and five broader native-unwind shapes. The
14-file typed-reference matrix is unchanged at 422 commands / 50 modules / 211 assertions /
65 invalid / 2 malformed / 1 unlinkable / 12 gates / 36 blocked.

Every EH function frame reserves four records of six 64-bit words, a fixed 192-byte
region independent of dynamic nesting or throw count. No exception object, exception
reference, heap allocation, or unbounded handler stack is created. The generalized
14-function fixture emits 5,417 raw function bytes and a 5,498-byte linked code image;
measured frames are 216-248 bytes with 0-2 operand spill slots. The 192-byte EH reserve
accounts for the increase from iteration 31's single 48-byte record. `testing.AllocsPerRun`
measures the generalized caught invocation at exactly zero allocations, and five 500 ms
samples of the retained nested catch benchmark measure 39.69-40.53 ns/op, 0 B/op, and
0 allocs/op. The iteration-31 recorded range for the same benchmark was 41.29-42.29 ns/op;
the ranges are reported as measurements, not as a statistically established speedup.

No persisted codec or fixed public/runtime layout changes. The assertions remain
`Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, native descriptor=32,
basedata=256, and `memoryState=40` bytes. The EH handler pointer still occupies basedata
offset 152; tag metadata and lifecycle attachments use existing bounded sidecars.
Release 1 remains zero-gap at 629 modules / 16,026 assertions, Release 2 remains zero-gap
at 1,600 modules / 48,248 assertions, and the public Release 3 schema-2 inventory remains
byte-for-byte unchanged at 1,691 passed / 535 skipped modules and 51,765 passed / 5 failed /
6,268 skipped assertions.

Iteration 32 is not a completion claim. Exception references still need rooted native
values, the five official general-unwind gates remain, tag identity cannot yet cross a
native throw/catch boundary, persistence and snapshots remain closed, and public/guard/
arm64 admission is absent. GC execution, staged typed-reference/tail/memory products, and
the broader Core 3 public baseline also remain incomplete.

### Iteration 33 exact leaders, rooted exception values, and persisted tag products

Iteration 33 removes the misleading scalar-unwind diagnosis, adds bounded local exception
values, and persists declaration/link tag products. The public Core 3 feature bit remains
disabled.

1. The five official `exceptions/try_table` module leaders are now classified from their
   decoded structure rather than assigned one generic native-unwind gate. The exported
   throwing provider is a tag-product boundary; the imported mismatch module requires
   cross-instance basedata/handler transfer; two modules require reference catches; and
   the final module has a GC-managed function-reference tag payload. The two `ref_null`
   modules are likewise recorded as mixed any/none plus exn/noexn GC-root products. This
   removes all five `native-unwind-abi` gates without claiming those modules executable:
   exact product, exception-root, and GC reasons replace them one-for-one, with command and
   blocked totals unchanged at that commit boundary.
2. linux/amd64 explicit bounds now has a private exception-reference admission bit. Every
   EH function reserves four fixed three-word exception roots in addition to the existing
   four six-word handler records. `catch_ref` and `catch_all_ref` copy tag plus two scalar
   payload words into the selected stable root before branching; the one-slot native value
   is its frame-relative address. `throw_ref` checks null, copies the rooted identity back
   into the active handler, and follows the same ordered local unwind path as `throw`.
   Four roots/function are a compile-time limit, independent of invocation count. The
   official `exceptions/throw_ref` module is gap-free at 1 module / 12 assertions / 2
   invalid modules. Nested local-call catch/rethrow, two sequential retained exceptions,
   recatch, stack-polymorphic rethrow, null trapping, cold-trap recovery, 10,000 repeats,
   and successful-recatch `AllocsPerRun(1000)==0` pass. GC/reference tag payloads, exported
   exception-reference values, cross-instance rethrow, guard mode, public admission, and
   arm64 remain closed.
3. The compiled artifact format is now codec v27. It appends a bounded tag directory and
   exact export map after memory metadata: each record carries only its import key and
   structural type index. Count decoding rejects more than eight tags before allocation;
   imported records must precede locals; malformed strings, type indexes, and export maps
   continue through strict metadata validation. Local and imported declaration products
   round-trip exact metadata and recreate alias/re-export identity through normal
   transactional attachment. Snapshot v3 admits only function-free local declaration tags;
   imported tags, throwing functions, active handlers, rooted exception values, and native
   cross-instance bindings remain fail-closed. Codec v26 and older artifacts reject
   explicitly because they lack the tag directory/export map.

The strict EH matrix improves from iteration 32's 5 modules / 9 assertions / 8 gates / 90
blocked commands to 6 modules / 21 assertions / 7 gates / 78 blocked commands while
preserving 147 commands, 16 invalid, 2 source-only malformed, and 2 unlinkable cases. The
remaining seven exact gates are 2 exception-reference leaders inseparably mixed into the
large `try_table` module, 3 GC/mixed-reference products, and 2 cross-instance/product
leaders. There is no remaining generic scalar/native-unwind gate.

EH frame storage grows by a fixed 96 bytes: four handlers x six words x eight bytes = 192
bytes, plus four roots x three words x eight bytes = 96 bytes, for 288 bytes per EH function
frame. The generalized scalar fixture remains a 5,498-byte linked code image and now has
312-344-byte frames with 0-2 spills; its direct backend image is 4,876 bytes. The rooted
three-function fixture is 81 Wasm bytes, 814 direct backend bytes, 956 linked bytes, and
312/328/328-byte frames with 0-1 spills. Codec-v27 blobs are 6,342 bytes for the six-tag
scalar fixture, 1,156 bytes for the rooted fixture, 160 bytes for the two-tag declaration
producer, and 217 bytes for the three-tag imported/local consumer. Fixed public/runtime
layouts remain `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, native
descriptor=32, basedata=256, and `memoryState=40` bytes.

Five 500 ms samples of the retained scalar catch benchmark measure 42.42-43.40 ns/op,
0 B/op, and 0 allocs/op. The iteration-32 range was 39.69-40.53 ns/op; these watchpoints
are current-host measurements, not a statistically established regression attribution.
The additional 96-byte frame reserve is paid only by staged EH functions; ordinary
Release 1/2 and non-EH Core 3 frames are unchanged.

Iteration 33 is not a completion claim. Cross-instance native tag/handler transfer remains
unproven; the two reference-catch leaders inside the mixed official `try_table` module are
still blocked with its imported/tail contexts; GC payloads and mixed any/none references
remain non-executable; public/guard/arm64 admission remains closed; and the staged tail,
typed-reference, multi-memory, memory64, and table64 families are not yet public products.
The public Release 3 baseline therefore remains unchanged.

### Iteration 34 exact foreign handlers, mixed try-table execution, and native root maps

Iteration 34 executes the two largest remaining scalar/rooted EH products, makes true-tail
handler lifetime explicit, and defines the metadata contract required by the final
reference-payload leader. The public Core 3 feature bit remains disabled.

1. Exact retained cross-instance EH calls now carry the active handler in reserved RBP
   instead of publishing mutable handler state through shared basedata. Each bounded handler
   record grows from six to seven words so it owns the catcher's RBX/basedata value. A
   foreign throw restores the catcher basedata before dispatch, while the throwing producer
   remains retained through consumer teardown. Tag matching uses a bounded per-instance
   native identity directory at basedata offset 160 and compares producer identities rather
   than coincidental module-local tag indexes. Admission is deliberately limited to the exact
   retained `() -> ()` function ABI. Tests cover same-index foreign mismatch, cross-index
   aliases, uncaught propagation, cold recovery, rollback, both close orders, and concurrent
   consumers. Wider/chained transfer, live imported handlers, hosts, tails, snapshots,
   guard mode, and arm64 remain rejected.
2. The bounded product increases to nine tags and twenty-four `try_table` constructs so the
   large official mixed module executes. Internal metadata/codec conversion recognizes the
   exception-reference value category without exposing exception references through the
   public ABI. The exact immutable local table required by the EH tail cases is admitted.
   Before a true direct or indirect tail releases its frame, codegen discards every handler
   owned by that frame, preventing exceptions from the tail target from being caught by a
   dead try scope; `return_call_ref` remains closed for this composition. The official
   `exceptions/try_table` file now executes four modules and forty assertions while retaining
   nine invalid, two source-only malformed, one final reference-payload gate, and five
   dependents blocked. Nine-tag codec round-trip passes and a tenth tag rejects before
   allocation/attachment.
3. `src/core/nativeabi` now defines dependency-neutral native root maps shared by compiler
   and runtime. Maps identify a local function, minimum post-prologue frame prefix, strictly
   ordered aligned slots, and one of two ownership kinds: compact collector `gc.Ref` or
   funcref producer-lifecycle identity. `src/core/runtime/gc` exposes and validates the same
   contract; malformed function indexes, order, kinds, alignment, duplicates, and frame
   bounds reject before scanning. The amd64 builder derives slots from the fixed EH records:
   four seven-word handlers (224 bytes) plus four three-word roots (96 bytes), a 320-byte
   EH reserve. With the 16-byte frame header and no locals/spills, the mapped prefix is 336
   bytes; the single typed-funcref payload maps at offset 248. A fifth root is rejected.
   These maps are descriptive only: execution remains closed until initialization,
   publication/withdrawal, producer retention, barrier/remark, live-record scanning, and
   teardown are wired and proven.

The strict five-file EH matrix improves from iteration 33's 6 modules / 21 assertions /
7 gates / 78 blocked commands to 10 modules / 61 assertions / 3 gates / 37 blocked commands.
The full totals remain 147 commands, 16 invalid modules, 2 source-only malformed commands,
and 2 unlinkable modules, with zero hidden compile/link/action/assertion failures. The
remaining leaders are exactly one local typed-funcref tag-payload/root-lifecycle product and
two mixed any/none plus exn/noexn WasmGC products. There is no generic native-unwind or
cross-instance scalar-handler gate left in this matrix.

The handler expansion adds a fixed 32 bytes per EH function, so the EH reserve is now
320 bytes/function rather than 288. No `Compiled`, `Instance`, table, memory-sidecar,
basedata, or codec version field grows; tag-directory base storage remains in the existing
lazy plugin sidecar. The root-map metadata is compile-time Go data in the current slice and
adds no native hot-path instruction or allocation. Five benchmark samples of the retained
scalar catch path measure 41.48-41.91 ns/op, all at 0 B/op and 0 allocations/op. These
current-host ranges are watchpoints, not a statistically established change from iteration
33's 42.42-43.40 ns/op.

Iteration 34 is not a completion claim. `make spec3` still fails the repository completion
gate at the unchanged public baseline: 1,691 passed / 535 skipped modules and 51,765 passed /
5 failed / 6,268 skipped assertions. `SupportedFeatures()` still does not admit the mandatory
Core 3 families publicly. The final local typed-funcref EH payload, the two mixed GC/reference
leaders, public/guard/arm64 products, live state persistence, and broader cross-instance EH
ABIs remain incomplete. The next bounded slice should use the new ownership map to execute
only the final local typed-funcref `try_table` module first, proving descriptor identity,
root zeroing/live-record lifetime, tail/trap cleanup, public result matching, and
allocation-free repetition while retaining explicit GC/cross-instance gates. If that proof
cannot be made coherent, the module must stay gated and the slice should wire/test map
publication without widening admission.

### Iteration 35 catch-all ownership and local typed-funcref payloads

Iteration 35 closes the final non-GC official EH leader while preserving the boundary between
funcref lifecycle identity and compact collector references. The public Core 3 feature bit
remains disabled.

1. Native root-map construction now describes `catch_all_ref` payload words rather than
   treating an untyped catch as payload-free. For each of the two bounded payload positions,
   the builder examines every tag that can reach the catch. A uniformly typed reference word
   becomes either `RootFuncRef` or `RootGCRef`; absent payloads are harmless; a position that
   mixes scalar and reference values or mixes funcref and GC ownership rejects before a
   scanner can trust the map. The exact one-tag funcref catch-all slot remains offset 248 in
   the 336-byte no-local/no-spill mapped prefix.
2. amd64 now accepts scalar tag payloads plus one backend shape for a non-null indexed
   function reference. Reference catches zero their selected three-word fixed root before
   installing the handler. Branch-result metadata preserves the fact that an exn value is a
   frame-relative root pointer, and `drop` clears all three words. The compiler-side storage
   record remains 32 bytes. Externref, nullable, exact, non-function indexed, and abstract
   reference payloads reject at the backend boundary.
3. The product gate admits only one local tag with one non-null indexed `() -> ()` funcref
   payload, one declarative local function descriptor of that exact type, one `ref.func` and
   one `throw`, no imports/start/storage state, no calls through references, no tails, and
   exactly one immediately dropped rooted exn value per reference-catching function. The
   executing instance owns the descriptor for the whole native call, so this slice does not
   publish a collector frame or retain a foreign producer. Public funcref results are matched
   against canonical function identity rather than opaque token bits. Nullable, extern/GC,
   foreign, wider, escaping-root, tail, host, snapshot, guard, public, and arm64 products
   remain fail-closed. Codec v27 serializes the exact structural/tag metadata, while ordinary
   public load continues to reject the unsupported typed/EH feature bits.

The complete official `exceptions/try_table` file is now gap-free under staged admission:
5 modules, 45 assertions, 9 invalid modules, and 2 source-only malformed commands. The
five-file EH matrix improves from 10 modules / 61 assertions / 3 gates / 37 blocked to
11 modules / 66 assertions / 2 gates / 32 blocked while preserving 147 commands, 16 invalid,
2 malformed, 2 unlinkable, and zero hidden failures. The remaining leaders are exactly the
two mixed any/none plus exn/noexn WasmGC products in `ref_null`; no ordinary EH module remains
gated.

The local typed-funcref catch benchmark measures 135.1-145.7 ns/op over five 500 ms samples,
0 B/op, and 0 allocs/op. This includes public tokenization of the returned descriptor and is
not compared as the same operation as the retained scalar catch's 41.48-41.91 ns/op. Fixed
native EH storage remains 320 bytes/function, the mapped prefix remains 336 bytes, the first
payload root remains offset 248, and `storage{}` remains 32 bytes.

Iteration 35 is not a completion claim. `make spec3` still reproduces the unchanged public
baseline byte-for-byte and exits nonzero: 1,691 passed / 535 skipped modules and 51,765 passed /
5 failed / 6,268 skipped assertions. WasmGC opcodes and native safepoint publication are not
wired; the two mixed `ref_null` leaders remain gated; mandatory families remain staged rather
than publicly admitted; and guard/arm64/live-state product work remains. The next slice should
classify the two remaining modules down to exact opcodes, object/root/barrier requirements,
and public result actions, then implement the smallest collector-backed allocation/root/
access path that removes a real leader without weakening Tiny/Throughput or fail-closed gates.

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

Iteration 18 contains exactly three code/test commits and this documentation
commit:

1. `c3698e68` — initialize active memory64 data with exact i64 codec metadata,
   checked u64 offset+length arithmetic, and explicit passive/snapshot gates.
2. `3702353e` — return one canonical funcref result through the staged amd64
   register ABI and make the pinned `return_call_ref` file gap-free.
3. `29821463` — serialize finite imported numeric-global tenant state, retain its
   owner through close, and prove race-safe allocation-free calls.

Iteration 19 contains exactly three code/test commits and this documentation
commit:

1. `ae94396f` — execute every memory64 SIMD memory form with u64 memargs, exact
   width/lane traps, store atomicity, and unchanged memory32 SIMD code.
2. `f877a769` — tail-call retained instance exports through a separate fixed
   root/nested direct-return transition with lifecycle, trap, and oversized gates.
3. `305e2611` — serialize one bounded imported funcref-table tenant with exact
   operation scanning, independent owner retention, concurrency, and trap atomicity.

Iteration 20 contains exactly three code/test commits and this documentation
commit:

1. `2253fabc` — execute bounded memory64 `memory.copy`/`memory.fill` with u64
   carry checks, overlap, trap atomicity, and unchanged memory32 bulk code.
2. `578197c9` — open one local explicit-max table64 `size/get/set` path, persist
   exact table address forms in codec v26, and preserve product/platform gates.
3. `16da7cf2` — tail-call exactly `(i32, f64) -> f64` through the separate retained
   direct cross-instance return record with root/nested lifecycle and trap proof.

Iteration 21 contains exactly three code/test commits and this documentation
commit:

1. `ca1430d9` — execute bounded memory64 passive `memory.init`/`data.drop` with
   mixed i64/i32 operands, u64 carry/source checks, drop semantics, trap atomicity,
   codec reload, and unchanged memory32 code.
2. `0eaa4f21` — grow one bounded local funcref table64 with full u64 delta/add/max
   checks, i64 old-size/`-1` results, atomic failure, codec reload, and exact nine-file
   official accounting.
3. `a06a08de` — re-enter retained same-memory imported functions through stable
   arena-backed basedata images with nested/trap/grow/concurrency/lifecycle proof and
   zero-allocation steady-state calls.

Iteration 22 contains exactly three code/test commits and this documentation
commit:

1. `7f77b4ad` — admit exact no-maximum memory64 declarations under a finite
   65,535-page implementation reservation, preserve codec/policy metadata, and make
   the six-file matrix gap-free.
2. `56d16bb7` — execute bounded local funcref table64 `table.fill` with full-u64
   carry/end checks, trap atomicity, codec reload, and unchanged table32 code.
3. `10d7e5df` — tail-call exact `(f64) -> i32` retained instance exports through
   the fixed root/nested direct-return transition with lifecycle and trap proof.

Iteration 23 contains exactly three code/test commits and this documentation
commit:

1. `83740669` — account all sixteen non-table memory64 files with exact schema-2
   gate reasons and reject mixed memory32/memory64 imports before attachment.
2. `4e967653` — initialize bounded local funcref table64 tables through initializer
   expressions and active i64-offset elements with full-u64 checks and codec reload.
3. `0791441e` — compose retained same-memory native re-entry with imported numeric-
   global pointers and prove trap/grow/concurrency/independent-lifecycle behavior.

Iteration 24 contains exactly three code/test commits and this documentation
commit:

1. `e1fbbed6` — import one bounded instance-exported memory64 with exact provider
   max/no-max lifecycle metadata, grow visibility, rollback, policy, and platform gates.
2. `0ae10a68` — execute sole-local table64 `call_indirect` with full-u64 indexes,
   private no-maximum bounds, exact traps, table address-form matching, and official
   accounting deltas.
3. `821c3b6d` — compose retained same-memory native re-entry with the sole imported
   funcref table and prove table state, traps, growth, concurrency, close order, and
   fail-closed product/host/foreign/tail boundaries.

Iteration 25 contains exactly three code/test commits and this documentation
commit:

1. `aa92f681` — import one bounded instance-exported funcref table64 with exact
   max/no-max reservation metadata, growth, retention/rollback, inspection/policy,
   platform/product gates, and complete memory64 accounting deltas.
2. `c3371526` — execute sole-local table64 `table.copy` with full-u64 carry/end
   checks, overlap, trap atomicity, codec reload, unchanged table32 bytes, and an
   explicit imported-copy gate.
3. `3a3ee65a` — compose retained same-memory native re-entry with imported numeric
   globals and the sole imported funcref table simultaneously, proving state, traps,
   growth, concurrency, close order, and fail-closed boundaries.

Iteration 26 contains exactly three code/test commits and this documentation
commit:

1. `37dd0e9d` — preserve valid memory64 declared maxima above the finite execution
   reservation, enforce the Core 3 2^48-page ceiling, retain exact product/import/
   policy/managed metadata, and make the sixteen-file matrix gap-free.
2. `a0c9c464` — execute sole-local passive/declarative table64 `table.init`/
   `elem.drop` with mixed i64/i32 operands, full-u64 destination checks, source/drop/
   zero-length semantics, trap atomicity, codec reload, and unchanged table32 bytes.
3. `d01d9b3f` — execute exact two-local funcref `table.copy`, including mixed
   table32/table64 forms, through per-operand width checks and the native table
   directory; prove overlap/cross-table atomicity, metadata/codec reload, and official
   `table_copy64`/`table_copy_mixed` deltas.

Iteration 27 contains exactly three code/test commits and this documentation
commit:

1. `b55f510c` — execute exact two-local finite-funcref `table.size/get/set`, including
   mixed table32/table64 forms, with high-index, descriptor-write, trap-atomicity,
   native-directory, metadata/codec, and table32-stability proof.
2. `48fc44f5` — execute exact two-local `table.grow/fill` with per-table operand widths,
   full-u64 carry/end/max checks, atomic `-1` failure, descriptor snapshotting,
   directory updates, codec reload, and unchanged table32 bytes.
3. `bc84c73a` — execute exact two-local passive/declarative `table.init`/`elem.drop`
   with mixed destination widths, zero-extended segment operands, independent segment
   identity/drop state, trap atomicity, codec reload, and unchanged table32 behavior.

Iteration 28 contains exactly three code/test commits and this documentation
commit:

1. `01498f9f` — execute exact local no-maximum externref table64 get/set with
   per-instance official host-token identity, null/value writes, full-u64 indexes,
   trap atomicity, bounded minimum-only capacity, metadata/codec reload, and gap-free
   `table_get64`/`table_set64` accounting.
2. `fe2bf3bc` — execute the exact mixed table32/table64 no-maximum externref get/fill
   module with i32 canonicalization, full-u64 table64 carry/end checks, token/null
   snapshotting, zero-length boundaries, trap atomicity, codec reload, and gap-free
   `table_fill64` accounting.
3. `904aa692` — correct externref table64 grow to i64/full-u64 semantics, reserve
   no-maximum externref tables in a fixed 1,024-entry window, and execute the sole-table
   `table_grow64` plus four-local `table_size64` shapes with exact directory/metadata/
   codec and atomic `-1` failure proof.

Iteration 29 contains exactly three code/test commits and this documentation
commit:

1. `c261d8a9` — admit and execute the exact three-local table32/table32/table64
   init/drop/copy/call-indirect shape with retained imported functions, linked metadata
   reload, and gap-free `table_init64` accounting.
2. `61be7ea5` — preserve exact inert table64 maxima through `2^64-1` in u64 product
   metadata and codec v26 while allocating only the minimum, retaining explicit
   executable/export gates and malformed-above-u64 rejection.
3. `2e720d69` — admit declaration-only two-local and `spectest.table64` imported/local
   no-maximum products with bounded provider lifecycle, codec/policy/index-order proof,
   operation gates, and the Release 2 inert-table capacity regression fix.

Iteration 30 contains exactly three code/test commits and this documentation commit:

1. `bf04b1b0` — pin complete schema-2 accounting for 14 typed-reference/structural files,
   distinguish typed/GC/EH gates, and reject omitted commands, unknown gates, and hidden
   failures.
2. `2b761363` — correct `br_on_non_null` typing/fallthrough semantics, lower explicit
   reference block types, admit staged func/extern abstract forms, and prove codec/public/
   snapshot/platform boundaries.
3. `515a0f4a` — make `call_ref` reject abstract funcref, match cross-instance signatures
   structurally, compact empty recursive product groups, prove shifted nested lifecycle,
   close the official `call_ref` valid slice, and repair the stale compact-import staged
   reload proof by amendment rather than adding a fourth code/test commit.

Iteration 31 contains exactly three code/test commits and this documentation commit:

1. `64808986` — enforce recursive-group scope and whole-group equivalence, pin all five
   invalid typed-reference modules to exact errors on AST and byte-backed paths, and remove
   their validator allowlist.
2. `3a762c54` — add strict schema-2 accounting for the five-file exception-handling family,
   exact boundary reasons, source-only malformed commands, and foundational tag/heap
   validation fixes.
3. `5dd12273` — validate throw reachability and execute the bounded local scalar
   tag/throw/try_table slice with fixed native handlers, product metadata, cold-trap
   recovery, explicit persistence/public/platform gates, benchmark proof, and the generated
   public facade update; broad-suite issues were fixed by amendment rather than adding a
   fourth code/test commit.

Iteration 32 contains exactly three code/test commits and this documentation commit:

1. `ff1898ef` — complete AST and byte-backed `try_table` catch payload/depth validation,
   including exact non-null exception-reference catch sources, nullable-label widening,
   all four catch kinds, mismatch/depth errors, and removal of the final validator gate
   while preserving source-only malformed lines 339/344.
2. `874456b4` — generalize linux/amd64 explicit-bounds scalar unwind to bounded multiple
   tags, ordered catches plus catch-all, nested/sequential tables, and exact i32/i64/f32/f64
   payload words through four fixed six-word handler records, with official throw and
   zero-allocation proof.
3. `0a645429` — link bounded declaration-only tag products with exact recursive type/member
   identity, stable aliases/re-exports, transactional deduplicated retention and rollback,
   close-order lifecycle, inspection, policy, generated facade aliases, explicit codec/
   snapshot rejection, and a closed cross-instance native throw boundary.

Iteration 33 contains exactly three code/test commits and this documentation commit:

1. `ef77e779` — classify the five official `try_table` leaders by exact rooted-reference,
   GC-payload, exported-tag, and cross-instance-handler boundaries, removing the obsolete
   generic native-unwind gate without changing command coverage.
2. `c77de451` — add four fixed rooted exception values per EH function, lower
   `catch_ref`/`catch_all_ref`/`throw_ref` with null and rethrow semantics, execute all 12
   official `throw_ref` assertions, and prove nested-call/repeat/zero-allocation behavior.
3. `fa206275` — persist exact bounded tag declarations/imports/exports in codec v27,
   harden counts/order, recreate aliases/re-exports through normal lifecycle attachment,
   and admit snapshots only for function-free local declaration tags.

Iteration 34 contains exactly three code/test commits and this documentation commit:

1. `4c8e1af8` — transfer exact active handlers through retained `() -> ()` cross-instance
   calls using RBP, catcher-owned basedata, and bounded producer tag identity, with alias,
   mismatch, trap, rollback, concurrency, and close-order proof.
2. `5b287818` — execute the large mixed official `try_table` slice, increase the bounded
   product to nine tags/twenty-four tables, preserve internal exnref codec metadata, and
   discard dead handlers before direct/indirect true-tail transfer.
3. `ac842884` — define and validate exact target-neutral native exception-root maps,
   distinguish compact collector refs from funcref lifecycle roots, derive amd64 fixed
   offsets, and keep the final reference-payload product explicitly gated.

Iteration 35 contains exactly three code/test commits and this documentation commit:

1. `559be32f` — derive `catch_all_ref` payload ownership from every bounded tag, map exact
   funcref slots, and reject scalar/reference or GC/funcref ownership conflicts.
2. `eb1da7c8` — admit the backend's non-null indexed-function payload shape, initialize
   fixed roots before handler publication, preserve rooted-exn branch provenance, clear all
   three words on drop, keep compiler storage at 32 bytes, and reject non-function refs.
3. `cb7727d7` — admit the exact local-only typed-funcref EH product, preserve canonical
   public identity, close all five final official `try_table` actions, add strict negative
   product gates, and improve EH accounting to 11 modules / 66 assertions / 2 gates /
   32 blocked commands.

## Validation performed

Commands were run from the repository root on linux/amd64.

| Command | Result |
|---|---|
| iteration 35 focused code/test proof | PASS: catch-all root maps describe the exact funcref payload slot and reject scalar/reference plus GC/funcref ownership conflicts; amd64 initializes and clears fixed rooted exn records while retaining 32-byte compiler storage; the exact local-only non-null indexed `() -> ()` funcref product preserves canonical descriptor identity, codec-v27 metadata, public/snapshot/product gates, teardown, and zero-allocation repetition. Logs `.validation/iteration35-commit1-focused.log`, `.validation/iteration35-commit1-packages.log`, `.validation/iteration35-commit2-focused.log`, `.validation/iteration35-commit2-package.log`, `.validation/iteration35-commit2-wago.log`, `.validation/iteration35-commit3-focused.log`, and `.validation/iteration35-commit3-packages.log`. |
| iteration 35 staged family runners | PASS: EH is now 5 files / 147 commands / 11 modules / 66 assertions / 16 invalid / 2 malformed / 2 unlinkable / 2 exact gates / 32 blocked; the complete official `exceptions/try_table` file is gap-free at 5 modules / 45 assertions / 9 invalid / 2 malformed. Typed references remain 14 files / 422 commands / 50 modules / 211 assertions / 65 invalid / 2 malformed / 1 unlinkable / 12 gates / 36 blocked; multi-memory remains 42 files / 913 commands / 79 modules / 771 assertions / zero gates or blocked; memory64 remains 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 remains 9 files / 2,802 commands / 107 modules / 2,600 assertions / 81 invalid / zero gates or blocked; all three tail files remain gap-free. Every hidden-failure counter is zero. Log `.validation/iteration35-staged.log`. |
| `go test ./... -count=1` | PASS on final iteration-35 code HEAD. Log `.validation/iteration35-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration35-no-cgo.log`. |
| focused race validation | PASS for native root maps, collector boundary, amd64 EH/root lifetime, tag products, and the local funcref product. Log `.validation/iteration35-race.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; local typed-funcref EH execution remains explicitly unavailable in guard mode. Log `.validation/iteration35-commit3-guard.log`. |
| linux/arm64 compile-only frontend/railshot/runtime/`src/wago` | PASS compile/link evidence only; no typed-funcref EH execution claim. Log `.validation/iteration35-commit3-arm64-build.log`. |
| `go vet ./...`, `go generate ./...`, generated diff check | PASS. Logs `.validation/iteration35-vet.log`, `.validation/iteration35-generate.log`, and `.validation/iteration35-generate-diff.log`. |
| fixed layout/root lifetime evidence | PASS: EH reserve remains 320 bytes/function, no-local/no-spill mapped prefix 336 bytes, first payload root offset 248, and compiler `storage{}` remains 32 bytes. Reference catches emit one root initialization and immediate exn drop emits one three-word clear in the focused fixture. Logs `.validation/iteration35-commit1-focused.log` and `.validation/iteration35-commit2-focused.log`. |
| pinned tool verification | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration35-wabt.log` and `.validation/iteration35-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS zero-gap: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions. Logs `.validation/iteration35-spec1.log` and `.validation/iteration35-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/iteration35-spec3.log`, `.validation/iteration35-spec3-status.log`, and `.validation/iteration35-spec3-baseline.log`. |
| iteration 35 benchmark | PASS: local typed-funcref catch 135.1-145.7 ns/op across five 500 ms samples, all 0 B/op and 0 allocs/op. The path includes public funcref tokenization. Log `.validation/iteration35-funcref-benchmark.log`. |
| iteration 34 focused code/test proof | PASS: exact retained cross-instance handler/tag transfer covers aliases, same-index mismatch, uncaught/cold recovery, rollback, concurrency, and independent close order; the mixed official `try_table` slice, exact immutable tail table, true-tail handler discard, nine-tag codec bound, internal exnref category, and strict tenth-tag rejection pass; native root-map kind/order/alignment/frame validation, the typed-funcref offset-248 map, and fifth-root rejection pass. Logs `.validation/iteration34-commit1-focused.log`, `.validation/iteration34-commit1-packages.log`, `.validation/iteration34-commit2-focused.log`, `.validation/iteration34-commit2-packages.log`, `.validation/iteration34-commit3-packages.log`, and `.validation/iteration34-eh-rootmap.log`. |
| iteration 34 staged family runners | PASS: EH improves to 5 files / 147 commands / 10 modules / 61 assertions / 16 invalid / 2 malformed / 2 unlinkable / 3 exact gates / 37 blocked; typed references remain 14 files / 422 commands / 50 modules / 211 assertions / 65 invalid / 2 malformed / 1 unlinkable / 12 gates / 36 blocked; multi-memory remains 42 files / 913 commands / 79 modules / 771 assertions / zero gates or blocked; memory64 remains 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 remains 9 files / 2,802 commands / 107 modules / 2,600 assertions / 81 invalid / zero gates or blocked; all three tail files remain gap-free. Every hidden-failure counter is zero. Log `.validation/iteration34-staged.log`. |
| `go test ./... -count=1` | PASS on final iteration-34 code HEAD. Log `.validation/iteration34-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration34-no-cgo.log`. |
| focused race validation | PASS for native root maps, collector boundary, amd64 root derivation, EH transfer, and tag products; commit-1 concurrent consumers also pass independently. Logs `.validation/iteration34-race.log` and `.validation/iteration34-commit1-race.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; new EH execution remains explicitly unavailable in guard mode. Log `.validation/iteration34-guard.log`. |
| linux/arm64 compile-only railshot/runtime/`src/wago` | PASS compile/link evidence only; no arm64 EH execution claim. Log `.validation/iteration34-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration34-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; regenerated facade is unchanged and leaves no diff. Logs `.validation/iteration34-generate.log` and `.validation/iteration34-generate-diff.log`. |
| fixed layout and EH-root measurements | PASS: fixed assertions remain `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, basedata=256, and `memoryState=40`; the bounded EH reserve is 320 bytes/function (224 handler + 96 root), the no-local/no-spill mapped prefix is 336 bytes, and the exact typed-funcref payload slot is offset 248. Logs `.validation/iteration34-layout.log` and `.validation/iteration34-eh-rootmap.log`. |
| pinned tool verification | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration34-wabt.log` and `.validation/iteration34-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS zero-gap: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions. Logs `.validation/iteration34-spec1.log` and `.validation/iteration34-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/iteration34-spec3.log` and `.validation/iteration34-spec3-baseline.log`. |
| iteration 34 benchmark | PASS: retained scalar catch 41.48-41.91 ns/op across five samples, all 0 B/op and 0 allocs/op. Root-map construction is compile-time metadata and adds no native hot-path instruction. Log `.validation/iteration34-eh-benchmark.log`. |
| iteration 33 focused code/test proof | PASS: exact structural classification removes all five generic native-unwind gates; rooted `catch_ref`/`catch_all_ref`/`throw_ref` preserves nested/sequential identity, null traps, recatch, 10,000 repeats, and zero-allocation successful recatch; codec-v27 local/imported tag metadata, aliases/re-exports, malformed count/order hardening, and local-declaration snapshot policy pass. Logs `.validation/iteration33-commit1-focused.log`, `.validation/iteration33-commit2-focused.log`, `.validation/iteration33-commit3-focused.log`, and `.validation/iteration33-commit3-package.log`. |
| iteration 33 staged family runners | PASS: EH improves to 5 files / 147 commands / 6 modules / 21 assertions / 16 invalid / 2 malformed / 2 unlinkable / 7 exact gates / 78 blocked; typed references remain 14 files / 422 commands / 50 modules / 211 assertions / 65 invalid / 2 malformed / 1 unlinkable / 12 gates / 36 blocked; multi-memory remains 42 files / 913 commands / 79 modules / 771 assertions / zero gates or blocked; memory64 remains 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 remains 9 files / 2,802 commands / 107 modules / 2,600 assertions / 81 invalid / zero gates or blocked; all three tail files remain gap-free. Every hidden-failure counter is zero. Log `.validation/iteration33-staged.log`. |
| `go test ./... -count=1` | PASS on final iteration-33 code HEAD. Log `.validation/iteration33-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration33-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; rooted EH execution remains linux/amd64 explicit-bounds-only and tag products retain explicit guard gates. Log `.validation/iteration33-guard.log`. |
| linux/arm64 compile-only railshot/runtime/`src/wago` | PASS compile/link evidence only; no rooted EH execution claim. Log `.validation/iteration33-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration33-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged and regeneration leaves no diff. Logs `.validation/iteration33-generate.log` and `.validation/iteration33-generate-diff.log`. |
| fixed layout, EH root/frame, code, and codec measurements | PASS: fixed layouts remain `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, descriptor=32, basedata=256, `memoryState=40`; EH reserve is 288 bytes/function; scalar frames are 312-344 bytes and linked code remains 5,498 bytes; rooted frames are 312/328/328 bytes with 956 linked bytes; codec-v27 fixture sizes are 6,342/1,156/160/217 bytes. Logs `.validation/iteration33-layout.log`, `.validation/iteration33-code-size.log`, and `.validation/iteration33-codec-size.log`. |
| pinned tool verification | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration33-wabt.log` and `.validation/iteration33-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS zero-gap: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions. Logs `.validation/iteration33-spec1.log` and `.validation/iteration33-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/iteration33-spec3.log` and `.validation/iteration33-spec3-baseline.log`. |
| iteration 33 benchmark | PASS: retained scalar catch 42.42-43.40 ns/op across five 500 ms samples, all 0 B/op and 0 allocs/op; official successful rooted recatch independently passes `testing.AllocsPerRun(1000)` at zero. Log `.validation/iteration33-commit2-bench.log`. |
| iteration 32 focused code/test proof | PASS: exact AST and byte-backed payload/depth validation for `catch`, `catch_ref`, `catch_all`, and `catch_all_ref`; nullable exn widening and mismatch/unknown-depth rejection; generalized scalar bit identity, ordered catches/catch-all, nested/sequential tables, pair order, uncaught/start/stale-handler recovery; declaration-only tag metadata, exact recursive type identity, aliases/re-export, deduplicated retention, rollback, close order, policy, inspection, codec/snapshot/native-transfer gates; pinned official EH accounting. Log `.validation/iteration32-focused-pinned.log`. |
| iteration 32 staged family runners | PASS: typed-reference matrix unchanged at 14 files / 422 commands / 50 modules / 211 assertions / 65 invalid / 2 malformed / 1 unlinkable / 12 exact gates / 36 blocked; EH improves to 5 files / 147 commands / 5 modules / 9 assertions / 16 invalid / 2 malformed / 2 unlinkable / 8 exact gates / 90 blocked; multi-memory remains 42 files / 913 commands / 79 modules / 771 assertions / zero gates or blocked; memory64 remains 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 remains 9 files / 2,802 commands / 107 modules / 2,600 assertions / 81 invalid / zero gates or blocked; all three tail files remain gap-free. Every hidden-failure counter is zero. Log `.validation/iteration32-staged.log`. |
| `go test ./... -count=1` | PASS on final iteration-32 code HEAD. Log `.validation/iteration32-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration32-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; staged EH native execution and tag products remain explicit-bounds linux/amd64-only. Log `.validation/iteration32-guard.log`. |
| linux/arm64 compile-only `go test -exec=/bin/true` for railshot/arm64, runtime, and `src/wago` | PASS compile/link evidence only; no arm64 EH execution claim. Log `.validation/iteration32-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration32-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; `wago.go` includes `Tag`, `ImportTag`, and expanded `TagMetadata`, and regeneration leaves no diff. Logs `.validation/iteration32-generate.log` and `.validation/iteration32-generate-diff.log`. |
| fixed layout and EH footprint assertions | PASS: `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, native descriptor=32, basedata=256, `memoryState=40`; each EH function reserves four fixed 48-byte records (192 bytes), the generalized fixture emits 5,417 raw function bytes and a 5,498-byte linked image, and frames are 216-248 bytes with 0-2 spill slots. Logs `.validation/iteration32-layout.log` and `.validation/iteration32-code-size.log`. |
| pinned tool verification | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration32-wabt.log` and `.validation/iteration32-spec-interpreter.log`. |
| `make spec1` and `make spec2` with pinned WABT on `PATH` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration32-spec1.log` and `.validation/iteration32-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/iteration32-spec3.log` and `.validation/iteration32-spec3-baseline.log`. |
| iteration 32 benchmark | PASS: retained nested local scalar catch 39.69-40.53 ns/op across five 500 ms samples, all 0 B/op and 0 allocs/op; the generalized ordered-catch invocation independently passes `testing.AllocsPerRun(1000)` at exactly zero. Log `.validation/iteration32-bench.log`. |
| iteration 31 focused code/test proof | PASS: five exact recursive validator gaps closed on AST and byte-backed paths; strict five-file EH family classification; local scalar caught/uncaught execution; payload order; nested unwind; unrelated-trap recovery; start failure; metadata, codec-v26, snapshot, public, guard, and platform gates; 10,000 repeated catches at zero steady-state allocation. Logs `.validation/iteration31-commit3-validator.log`, `.validation/iteration31-commit3-focused.log`, `.validation/iteration31-commit3-packages.log`, `.validation/iteration31-commit3-no-cgo-packages.log`, `.validation/iteration31-commit3-guard.log`, `.validation/iteration31-commit3-arm64-build.log`, and `.validation/iteration31-commit3-official.log`. |
| iteration 31 staged family runners | PASS: typed-reference matrix 14 files / 422 commands / 50 modules / 211 assertions / 65 invalid / 2 malformed / 1 unlinkable / 12 exact gates / 36 blocked; exception handling 5 files / 147 commands / 16 invalid / 2 malformed / 15 exact gates / 101 blocked; multi-memory 42 files / 913 commands / 79 modules / 771 assertions / zero gates or blocked; memory64 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 9 files / 2,802 commands / 107 modules / 2,600 assertions / 81 invalid / zero gates or blocked; all three tail files remain gap-free. Every hidden-failure counter is zero. Log `.validation/iteration31-staged-final.log`. |
| `go test ./... -count=1` | PASS on final iteration-31 code HEAD. Log `.validation/iteration31-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration31-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; the staged EH slice remains explicit-bounds linux/amd64-only. Log `.validation/iteration31-guard.log`. |
| linux/arm64 compile-only `go test -exec=/bin/true` for railshot/arm64, runtime, and `src/wago` | PASS compile/link evidence only; no arm64 EH execution claim. Log `.validation/iteration31-arm64-build.log`. |
| `go vet ./...` | PASS after replacing unsafe integer-pointer arithmetic with `unsafe.Add` and moving stale-handler clearing to the typed `JobMemory` cold path. Log `.validation/iteration31-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; the generated facade includes `TagMetadata` and is current with no final diff. Logs `.validation/iteration31-generate.log` and `.validation/iteration31-generate-diff.log`. |
| fixed layout and EH footprint assertions | PASS: `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, native descriptor=32, basedata=256, `memoryState=40`; the EH fixture emits 926 direct-backend bytes and 1,068 linked product bytes, frames are 104/136/136 bytes, and each reserves one fixed 48-byte EH record. Logs `.validation/iteration31-layout.log` and `.validation/iteration31-code-size.log`. |
| pinned tool verification | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration31-wabt.log` and `.validation/iteration31-spec-interpreter.log`. |
| `make spec1` and `make spec2` with pinned WABT on `PATH` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration31-spec1.log` and `.validation/iteration31-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/iteration31-spec3.log` and `.validation/iteration31-spec3-baseline.log`. |
| iteration 31 benchmark | PASS: caught nested local scalar exception 41.29-42.29 ns/op, 0 B/op, 0 allocs/op across five 500 ms samples. Log `.validation/iteration31-bench.log`. |
| iteration 30 focused code/test proof | PASS: complete 14-file schema-2 accounting with strict typed/GC/EH gate classification; exact `br_on_non_null` label-prefix/non-null-payload validation and null-fallthrough consumption; explicit func/extern reference block lowering; typed required-feature/codec/public/snapshot boundaries; `call_ref` abstract-funcref rejection and nullable typed null trap; shifted/recursive cross-instance structural signature matching; dense empty recursive product groups; nested producer retention and teardown; local/null/wrong-signature/host boundary coverage. Logs `.validation/iteration30-commit1-focused.log`, `.validation/iteration30-commit1-packages.log`, `.validation/iteration30-commit2-focused.log`, `.validation/iteration30-commit2-packages.log`, `.validation/iteration30-commit2-official.log`, `.validation/iteration30-commit2-guard.log`, `.validation/iteration30-commit2-arm64-build.log`, `.validation/iteration30-commit3-focused.log`, `.validation/iteration30-commit3-nested.log`, `.validation/iteration30-commit3-official.log`, and `.validation/iteration30-commit3-packages.log`. |
| iteration 30 staged family runners | PASS: typed-reference matrix 14 files / 422 commands / 50 modules / 211 assertions / 60 invalid / 2 malformed / 1 unlinkable / 17 exact gates / 36 blocked; multi-memory 42 files / 913 commands / 79 modules / 771 assertions / zero gates or blocked, plus compact imported size/grow codec replay; memory64 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 9 files / 2,802 commands / 107 modules / 2,600 assertions / 81 invalid / zero gates or blocked; all three tail files remain gap-free. Every hidden-failure counter is zero. Log `.validation/iteration30-staged-final.log`. |
| `go test ./... -count=1` | PASS on final iteration-30 code HEAD. Log `.validation/iteration30-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration30-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; staged typed-reference execution remains explicit-bounds linux/amd64-only. Log `.validation/iteration30-guard.log`. |
| linux/arm64 compile-only `go test -exec=/bin/true` for railshot/arm64, runtime, and `src/wago` | PASS compile/link evidence only; no arm64 typed-reference execution claim. Log `.validation/iteration30-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration30-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. Log `.validation/iteration30-go-generate.log`. |
| fixed layout assertions | PASS: `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, native descriptor=32, basedata=256, `memoryState=40`; dense recursive group IDs and exact matching add no fixed field. Log `.validation/iteration30-layout.log`. |
| pinned tool verification | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration30-wabt.log` and `.validation/iteration30-spec-interpreter.log`. |
| `make spec1` and `make spec2` with pinned WABT on `PATH` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration30-spec1.log` and `.validation/iteration30-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration30.log` and `.validation/iteration30-spec3-baseline.log`. |
| iteration 30 benchmarks | PASS: staged null control 46.73-47.21 ns/op; retained cross-instance `call_ref` 40.48-51.55 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration30-commit2-bench.log` and `.validation/iteration30-commit3-bench.log`. |
| iteration 29 focused code/test proof | PASS: exact three-local table.init/drop/copy/call-indirect admission and official execution; exact inert table64 maxima 65,536 through `2^64-1`, malformed-above-u64 rejection, header/minimum-only instantiation, u64 metadata and codec-v26 reload; exact declaration-only two-local and `spectest.table64` imported/local products with index/no-max identity, mismatch rollback, retention and close-order release; oversized inert table32 regression coverage. Logs `.validation/iteration29-commit1-focused.log`, `.validation/iteration29-commit1-packages.log`, `.validation/iteration29-commit2-focused.log`, `.validation/iteration29-commit2-packages.log`, `.validation/iteration29-commit3-focused.log`, `.validation/iteration29-commit3-packages.log`, and `.validation/iteration29-regression-focused.log`. |
| iteration 29 staged family runners | PASS: multi-memory 42 files / 913 commands / 79 modules / 771 assertions / 4 invalid / 22 unlinkable / 20 uninstantiable / zero gates or blocked; memory64 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 9 files / 2,802 commands / 107 modules / 2,600 assertions / 81 invalid / 0 malformed / zero gates or blocked; all three staged tail files remain gap-free. Every hidden-failure counter is zero. Log `.validation/iteration29-staged-final.log`. |
| `go test ./... -count=1` | PASS on final iteration-29 code HEAD. Log `.validation/iteration29-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration29-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; table64 staged execution and new declaration products remain explicit-bounds-only. Log `.validation/iteration29-guard.log`. |
| linux/arm64 compile-only `go test -exec=/bin/true` for railshot/arm64, runtime, and `src/wago` | PASS compile/link evidence only; no arm64 table64 execution claim. Log `.validation/iteration29-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration29-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. Log `.validation/iteration29-go-generate.log`. |
| fixed layout assertions | PASS: `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, native descriptor=32, basedata=256, `memoryState=40`; u64 table limits reuse existing 8-byte fields and codec records. Log `.validation/iteration29-layout.log`. |
| pinned tool verification | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration29-wabt.log` and `.validation/iteration29-spec-interpreter.log`. |
| `make spec1` and `make spec2` with pinned WABT on `PATH` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration29-spec1.log` and `.validation/iteration29-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration29.log` and `.validation/iteration29-spec3-baseline.log`. |
| iteration 29 benchmarks | PASS: zero-length table64 init 78.77-86.65 ns/op; table64 `call_indirect` 61.59-82.18 ns/op; table64 size 37.01-38.31 ns/op; imported table64 size 72.40-75.57 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration29-commit1-bench.log`, `.validation/iteration29-commit2-bench.log`, and `.validation/iteration29-commit3-bench.log`. |
| iteration 28 focused code/test proof | PASS: exact local no-maximum externref table64/funcref get/set with per-instance host-token identity, null/value writes, active initialization, full-u64 high-index traps, atomicity, minimum-only capacity, metadata and codec reload; exact mixed table32/table64 externref fill/get with i32 canonicalization, full-u64 carry/end, token/null snapshotting, zero-length boundaries and atomic traps; corrected externref table64 grow i64 result/delta arithmetic, fixed 1,024-entry no-max reservation, sole-table and four-local directory updates, atomic all-ones failure, exact metadata and codec reload. Logs `.validation/iteration28-commit1-focused.log`, `.validation/iteration28-commit1-packages.log`, `.validation/iteration28-commit2-focused.log`, `.validation/iteration28-commit2-packages.log`, `.validation/iteration28-commit3-focused.log`, and `.validation/iteration28-commit3-packages.log`. |
| iteration 28 staged family runners | PASS: multi-memory 42 files / 913 commands / 79 modules / 771 assertions / 4 invalid / 22 unlinkable / 20 uninstantiable / zero gates or blocked; memory64 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 9 files / 2,802 commands / 98 modules / 2,507 assertions / 9 gates / 93 blocked / 81 invalid / 0 malformed; `return_call` 47 / 3 / 33 / 11 invalid; `return_call_indirect` 79 / 3 / 49 / 16 invalid / 11 malformed; `return_call_ref` 51 / 5 / 35 / 11 invalid. All hidden-failure counters are zero. `table_get64`, `table_set64`, `table_fill64`, `table_grow64`, and `table_size64` are gap-free. Log `.validation/iteration28-staged-final.log`. |
| `go test ./... -count=1` | PASS on final iteration-28 code HEAD. Log `.validation/iteration28-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration28-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; iteration-28 table64 execution remains explicit-bounds-only. Log `.validation/iteration28-guard.log`. |
| linux/arm64 compile-only `go test -exec=/bin/true` for railshot/arm64, runtime, and `src/wago` | PASS compile/link evidence only; no arm64 table64 execution claim. Log `.validation/iteration28-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration28-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. Log `.validation/iteration28-go-generate.log`. |
| fixed layout assertions | PASS: existing bounds remain `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, native descriptor=32, basedata=256, `memoryState=40`; no codec/runtime struct grew. Log `.validation/iteration28-layout.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration28-wabt.log` and `.validation/iteration28-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration28-spec1.log` and `.validation/iteration28-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration28.log` and `.validation/iteration28-spec3-baseline.log`. |
| iteration 28 benchmarks | PASS: externref table64 get 47.79-51.69 ns/op; zero-length externref table64 fill 47.28-50.24 ns/op; externref table64 grow-by-zero 48.84-50.82 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration28-commit1-bench.log`, `.validation/iteration28-commit2-bench.log`, and `.validation/iteration28-commit3-bench.log`. |
| iteration 27 focused code/test proof | PASS: exact two-local finite-funcref table64/table64 and mixed table32/table64 size/get/set/grow/fill/init/drop execution; per-index width and high-index behavior; null/non-null descriptor writes and snapshotting; full-u64 carry/end/max checks; atomic `-1` grow failure; target-directory updates; source/drop/declarative/zero-length and independent segment-index semantics; trap atomicity; metadata/exports; codec-v26 reload; unchanged table32 bytes; imports, externref, no-max, wider table counts, indirect, snapshots, guard, public, and arm64 remain explicit gates. Logs `.validation/iteration27-commit1-focused.log`, `.validation/iteration27-commit1-packages.log`, `.validation/iteration27-commit2-focused.log`, `.validation/iteration27-commit2-packages.log`, `.validation/iteration27-commit3-focused.log`, and `.validation/iteration27-commit3-packages.log`. |
| iteration 27 staged family runners | PASS: multi-memory 42 files / 913 commands / 79 modules / 771 assertions / 4 invalid / 22 unlinkable / 20 uninstantiable / zero gates or blocked; memory64 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 9 files / 2,802 commands / 93 modules / 2,352 assertions / 14 gates / 248 blocked / 81 invalid / 0 malformed; `return_call` 47 / 3 / 33 / 11 invalid; `return_call_indirect` 79 / 3 / 49 / 16 invalid / 11 malformed; `return_call_ref` 51 / 5 / 35 / 11 invalid. All hidden-failure counters are zero. Every measured iteration-27 table-size/get/set/grow/fill/init official delta is zero because broader externref/no-max/table-count/indirect shapes lead. Log `.validation/iteration27-staged-final.log`. |
| `go test ./... -count=1` | PASS on final iteration-27 code HEAD. Log `.validation/iteration27-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration27-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; iteration-27 table64 execution remains explicit-bounds-only. Log `.validation/iteration27-guard.log`. |
| linux/arm64 compile-only `go test -exec=/bin/true` for railshot/arm64, runtime, and `src/wago` | PASS compile/link evidence only; no arm64 table64 execution claim. Log `.validation/iteration27-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration27-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. Log `.validation/iteration27-go-generate.log`. |
| fixed layout assertions | PASS: existing bounds remain `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, native descriptor=32, basedata=256, `memoryState=40`; no codec/runtime struct grew. Log `.validation/iteration27-layout.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration27-wabt.log` and `.validation/iteration27-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration27-spec1.log` and `.validation/iteration27-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration27.log` and `.validation/iteration27-spec3-baseline.log`. |
| iteration 27 benchmarks | PASS: two-local table64 size 36.25-36.92 ns/op; grow-by-zero 37.28-38.98 ns/op; zero-length init 38.66-39.43 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration27-commit1-bench.log`, `.validation/iteration27-commit2-bench.log`, and `.validation/iteration27-commit3-bench.log`. |
| iteration 26 focused code/test proof | PASS: exact memory64 maxima above the 65,535-page reserve, exact 2^48 validation ceiling, product/import/policy/managed accounting and atomic unavailable growth; sole-local table64 passive/declarative init/drop typing, carry/source/drop/zero-length/trap atomicity, codec reload, imported gate, and unchanged table32 bytes; exact two-local table64/table64 and mixed table32/table64 copy, per-operand/minimum-width typing, native directory, overlap/cross-table atomicity, metadata/codec reload, and broader-operation/import/externref/no-max gates. Logs `.validation/iteration26-commit2-focused.log`, `.validation/iteration26-commit3-focused.log`, and `.validation/iteration26-commit3-packages.log`; commit-1 focused command passed as recorded in the iteration handoff. |
| iteration 26 staged family runners | PASS: multi-memory 42 files / 913 commands / 79 modules / 771 assertions / 4 invalid / 22 unlinkable / 20 uninstantiable / zero gates or blocked; memory64 16 files / 5,904 commands / 169 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked; table64 9 files / 2,802 commands / 93 modules / 2,352 assertions / 14 gates / 248 blocked / 81 invalid / 0 malformed; `return_call` 47 / 3 / 33 / 11 invalid; `return_call_indirect` 79 / 3 / 49 / 16 invalid / 11 malformed; `return_call_ref` 51 / 5 / 35 / 11 invalid. All hidden-failure counters are zero. `table_copy64` is gap-free at 52 modules / 1,675 assertions; `table_copy_mixed` admits its one valid module; `table_init64` has an exact zero accounting delta. Log `.validation/iteration26-staged-final.log`. |
| `go test ./... -count=1` | PASS on final iteration-26 code HEAD. Log `.validation/iteration26-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration26-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; all iteration-26 execution remains explicit-bounds-only. Log `.validation/iteration26-guard.log`. |
| linux/arm64 compile-only `go test -exec=/bin/true` for railshot/arm64, runtime, and `src/wago` | PASS compile/link evidence only; no arm64 memory64/table64 execution claim. Log `.validation/iteration26-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration26-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. Log `.validation/iteration26-go-generate.log`. |
| fixed layout assertions | PASS: existing bounds remain `Compiled=712`, `Instance=792`, `tableDef=56`, `Table=64`, native descriptor=32, basedata=256, `memoryState=40`; no codec/runtime struct grew. Log `.validation/iteration26-layout.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration26-wabt.log` and `.validation/iteration26-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration26-spec1.log` and `.validation/iteration26-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration26.log` and `.validation/iteration26-spec3-baseline.log`. |
| iteration 26 benchmarks | PASS: no-maximum memory64 size 37.96-38.34 ns/op; imported memory64 size 37.59-37.76 ns/op; table64 zero-length init 42.20-42.78 ns/op; two-local table64 zero-length copy 38.64-39.10 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration26-commit2-bench.log` and `.validation/iteration26-commit3-bench.log`; memory64 ranges are from commit 1's recorded proof. |
| iteration 25 focused code/test proof | PASS: bounded instance-exported table64 min/max/no-max matching, finite exported no-max reservation, shared size/grow, re-export, rollback/retention, codec/inspection/policy metadata, table32/host/public/snapshot/guard/arm64 gates; sole-local table64 copy AST/byte-backed admission, full-u64 destination/source/length carry/end traps, overlap, trap atomicity, codec reload, unchanged table32 bytes, and imported-copy gate; simultaneous imported-global/table same-memory root/nested calls, updates, table state, traps, growth, concurrency, independent close order, and codec/snapshot/public/host/foreign/tail gates. Logs `.validation/iteration25-commit1-packages.log`, `.validation/iteration25-commit1-memory64.log`, `.validation/iteration25-commit2-packages.log`, `.validation/iteration25-commit2-official-pre.log`, `.validation/iteration25-commit3-packages.log`, and `.validation/iteration25-race-final.log`. |
| iteration 25 staged family runners | PASS: multi-memory 42 files / 913 commands / 79 modules / 771 assertions / 4 invalid / 22 unlinkable / 20 uninstantiable / zero gates or blocked; memory64 16 files / 5,904 commands / 167 modules / 5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / 2 exact gates / 0 blocked; table64 9 files / 2,802 commands / 70 modules / 2,330 assertions / 37 gates / 270 blocked / 81 invalid / 0 malformed; `return_call` 47 / 3 / 33 / 11 invalid; `return_call_indirect` 79 / 3 / 49 / 16 invalid / 11 malformed; `return_call_ref` 51 / 5 / 35 / 11 invalid. All hidden-failure counters are zero. Log `.validation/iteration25-staged-final2.log`. |
| `go test ./... -count=1` | PASS on final iteration-25 code HEAD. Log `.validation/iteration25-all-final.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration25-no-cgo-final.log`. |
| `go test -race ./src/wago -run '^TestStagedMultiMemoryNativeSameMemory(ReentryLifecycle\|ImportedGlobalComposition\|ImportedTableComposition\|ImportedGlobalTableComposition)$' -count=1` | PASS; all serializer combinations remain race-clean. Log `.validation/iteration25-race-final.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; table64 import/copy and simultaneous side state remain explicit-bounds-only. Log `.validation/iteration25-guard-final.log`. |
| linux/arm64 `go test -c` for `./src/wago` and `./src/core/compiler/backend/railshot/arm64` | PASS compile/link evidence only; no arm64 execution claim. Log `.validation/iteration25-arm64-build-final.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration25-vet-final.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. Log `.validation/iteration25-go-generate-final.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration25-wabt.log` and `.validation/iteration25-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration25-spec1.log` and `.validation/iteration25-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration25.log` and `.validation/iteration25-spec3-baseline.log`. |
| iteration 25 benchmarks | PASS: imported table64 size 71.59-74.22 ns/op; table64 copy-by-zero 38.22-42.57 ns/op; simultaneous imported-global/table nested same-memory re-entry 168.0-174.4 ns/op versus table-only 161.3-168.0, global-only 124.4-125.2, and plain 123.3-127.8 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration25-commit1-bench.log`, `.validation/iteration25-commit2-bench.log`, `.validation/iteration25-commit3-bench.log`, and `.validation/iteration25-commit3-bench-compare.log`. |
| iteration 24 focused code/test proof | PASS: bounded instance-exported memory64 import min/max/no-max matching, grow visibility, re-exported type, rollback/retention, codec/policy metadata, mixed-form/public/snapshot/guard/arm64 gates; private no-max table64 admission, full-u64 `call_indirect`, null/signature/high-index traps, codec reload, table32 code stability, and table address-form rejection; imported-table plus same-memory native root/nested calls, table state, traps, growth, concurrency, independent close order, and codec/snapshot/public/host/foreign/tail gates. Logs `.validation/iteration24-commit1-focused.log`, `.validation/iteration24-commit1-official.log`, `.validation/iteration24-commit2-focused.log`, `.validation/iteration24-commit2-official.log`, `.validation/iteration24-commit3-focused.log`, and `.validation/iteration24-commit3-race.log`. |
| iteration 24 staged family runners | PASS: multi-memory 42 files / 913 commands / 79 modules / 771 assertions / 4 invalid / 22 unlinkable / 20 uninstantiable / zero gates or blocked; memory64 16 files / 5,904 commands / 150 modules / 5,335 assertions / 292 invalid / 60 malformed / 24 unlinkable / 25 exact gates / 1 blocked; table64 9 files / 2,802 commands / 70 modules / 2,330 assertions / 37 gates / 270 blocked / 81 invalid / 0 malformed; `return_call` 47 / 3 / 33 / 11 invalid; `return_call_indirect` 79 / 3 / 49 / 16 invalid / 11 malformed; `return_call_ref` 51 / 5 / 35 / 11 invalid. All hidden-failure counters are zero. Log `.validation/iteration24-staged-final.log`. |
| `go test ./... -count=1` | PASS on final iteration-24 code HEAD. Log `.validation/iteration24-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration24-no-cgo.log`. |
| `go test -race ./src/wago -run '^TestStagedMultiMemoryNativeSameMemory(ReentryLifecycle\|ImportedGlobalComposition\|ImportedTableComposition)$' -count=1` | PASS; all three same-memory serializer compositions remain race-clean. Log `.validation/iteration24-commit3-race.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; imported memory64, table64 indirect calls, and same-memory table composition remain explicit-bounds-only. Log `.validation/iteration24-guard.log`. |
| linux/arm64 `go test -c` for `./src/wago` and `./src/core/compiler/backend/railshot/arm64` | PASS compile/link evidence only; no arm64 execution claim. Log `.validation/iteration24-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration24-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. Log `.validation/iteration24-go-generate.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration24-wabt.log` and `.validation/iteration24-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration24-spec1.log` and `.validation/iteration24-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration24.log` and `.validation/iteration24-spec3-baseline.log`. |
| iteration 24 benchmarks | PASS: imported memory64 size 35.74-36.90 ns/op; table64 `call_indirect` 36.39-37.26 ns/op; imported-table nested same-memory re-entry 164.4-168.5 ns/op versus 122.9-126.7 ns/op baseline; all 0 B/op and 0 allocs/op. Logs `.validation/iteration24-commit1-bench.log`, `.validation/iteration24-commit2-bench.log`, and `.validation/iteration24-commit3-bench.log`. |
| iteration 23 focused code/test proof | PASS: complete sixteen-file memory64 replay with exact gate schema and mixed address-form import rejection; table64 initializer/active-element AST and byte-backed admission, i64 codec metadata, initializer override ordering, all-ones offset rejection, passive/public gates; imported-global plus same-memory native root/nested calls, updates, traps, shared growth, concurrency, independent close order, and codec/snapshot/public/host/foreign/tail gates. Logs `.validation/iteration23-commit1-official.log`, `.validation/iteration23-commit2-focused.log`, `.validation/iteration23-commit2-official.log`, `.validation/iteration23-commit3-focused.log`, and `.validation/iteration23-commit3-race.log`. |
| iteration 23 staged family runners | PASS: multi-memory 42 files / 913 commands / 79 modules / 771 assertions / 4 invalid / 22 unlinkable / 20 uninstantiable / zero gates or blocked; memory64 16 files / 5,904 commands / 132 modules / 5,334 assertions / 292 invalid / 60 malformed / 4 unlinkable / 63 exact gates / 3 blocked; table64 9 files / 2,802 commands / 68 modules / 2,330 assertions / 39 gates / 270 blocked / 81 invalid / 0 malformed; `return_call` 47 / 3 / 33 / 11 invalid; `return_call_indirect` 79 / 3 / 49 / 16 invalid / 11 malformed; `return_call_ref` 51 / 5 / 35 / 11 invalid. All hidden-failure counters are zero. Log `.validation/iteration23-staged-final.log`. |
| `go test ./... -count=1` | PASS on final iteration-23 code HEAD. Log `.validation/iteration23-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration23-no-cgo.log`. |
| `go test -race ./src/wago -run '^TestStagedMultiMemoryNativeSameMemory(ReentryLifecycle\|ImportedGlobalComposition)$' -count=1` | PASS; original and imported-global composed serializers remain race-clean. Log `.validation/iteration23-race.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; memory64/table64 execution and same-memory composition remain explicit-bounds-only. Log `.validation/iteration23-guard.log`. |
| linux/arm64 `go test -c` for `./src/wago` and `./src/core/compiler/backend/railshot/arm64` | PASS compile/link evidence only; no arm64 execution claim. Log `.validation/iteration23-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration23-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. Log `.validation/iteration23-go-generate.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration23-wabt.log` and `.validation/iteration23-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration23-spec1.log` and `.validation/iteration23-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration23.log` and `.validation/iteration23-spec3-baseline.log`. |
| iteration 23 benchmarks | PASS: memory64 no-maximum size 35.75-37.03 ns/op; initialized table64 get 36.46-36.78 ns/op; composed imported-global nested re-entry 122.4-126.4 ns/op versus 121.5-122.6 ns/op baseline; all 0 B/op and 0 allocs/op. Logs `.validation/iteration23-commit1-bench.log`, `.validation/iteration23-commit2-bench.log`, and `.validation/iteration23-commit3-bench-compare.log`. |
| iteration 22 focused code/test proof | PASS: no-maximum memory64 exact metadata/codec reload, finite reservation policy, successful bounded grow, atomic resource failure, and public/platform gates; table64 fill AST/byte-backed admission, non-null writes, zero-length boundary, u64 carry/end traps, trap atomicity, codec reload, and table32 code stability; exact `(f64) -> i32` retained direct tails at root/nested callers, 10,000 repeats, trap recovery, producer close order, and wider-shape/public gates. Logs `.validation/iteration22-commit1-official.log`, `.validation/iteration22-commit2-official.log`, and `.validation/iteration22-commit3-focused.log`. |
| iteration 22 staged family runners | PASS: multi-memory 42 files / 913 commands / 79 modules / 771 assertions / 4 invalid / 22 unlinkable / 20 uninstantiable / zero feature gates or blocked commands; memory64 6 files / 807 commands / 43 modules / 622 assertions / 83 invalid / 59 malformed / zero gates or blocked; table64 9 files / 2,802 commands / 68 modules / 2,330 assertions / 39 gates / 270 blocked / 81 invalid / 0 malformed; `return_call` 47 / 3 / 33 / 11 invalid; `return_call_indirect` 79 / 3 / 49 / 16 invalid / 11 malformed; `return_call_ref` 51 / 5 / 35 / 11 invalid. All hidden-failure counters are zero. Log `.validation/iteration22-staged-final.log`. |
| `go test ./... -count=1` | PASS on final iteration-22 code HEAD. Log `.validation/iteration22-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. Log `.validation/iteration22-no-cgo.log`. |
| `go test -race ./src/wago -run '^TestStagedMultiMemoryNativeSameMemoryReentryLifecycle$' -count=1` | PASS; retained same-memory trap/grow/concurrency/lifecycle proof remains race-clean. Log `.validation/iteration22-race.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; memory64, table64, and direct-tail staged execution remain fail-closed under guard mode. Log `.validation/iteration22-guard.log`. |
| linux/arm64 `go test -c` for `./src/wago` and `./src/core/compiler/backend/railshot/arm64` | PASS compile/link evidence only; no arm64 execution claim. Log `.validation/iteration22-arm64-build.log`. |
| `go vet ./...` | PASS. Log `.validation/iteration22-vet.log`. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. Log `.validation/iteration22-go-generate.log`. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration22-wabt.log` and `.validation/iteration22-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration22-spec1.log` and `.validation/iteration22-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration22.log` and `.validation/iteration22-spec3-baseline.log`. |
| iteration 22 benchmarks | PASS: no-maximum memory64 size 34.57-35.87 ns/op; table64 fill-by-zero 37.53-38.22 ns/op; exact `(f64) -> i32` retained direct tail 115.7-139.4 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration22-commit1-bench.log`, `.validation/iteration22-commit2-bench.log`, and `.validation/iteration22-commit3-bench2.log`. |
| iteration 21 focused code/test proof | PASS: memory64 passive init/drop mixed operand typing, destination carry/source/end traps, drop and zero-length semantics, codec reload, snapshot/public/platform gates, and unchanged memory32 code; table64 grow u64 delta/add/max checks, i64 old-size/`-1`, atomic failure, codec reload, table32 stability, and fail-closed externref/import/multiple/element/guard/public/arm64 gates; same-memory native root/nested calls, shared grow visibility, trap recovery, concurrency, close order, codec/snapshot/public/host/foreign-memory/tail gates, and exact +256-byte arena accounting. |
| iteration 21 staged family runners | PASS: multi-memory 42 files / 913 commands / 79 modules / 771 assertions / 4 invalid / 22 unlinkable / 20 uninstantiable / zero feature gates or blocked commands; memory64 6 files / 807 commands / 7 modules / 92 assertions / 36 feature gates / 530 blocked / 83 invalid / 59 malformed; table64 9 files / 2,802 commands / 68 modules / 2,330 assertions / 39 feature gates / 270 blocked / 81 invalid / 0 malformed; all hidden-failure counters zero. The table64 command used both pinned WABT 1.0.41 and the revision-stamped official interpreter fallback. |
| `go test ./... -count=1` | PASS on final iteration-21 code HEAD. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; full no-cgo suite. |
| `go test -race ./src/wago -run '^TestStagedMultiMemoryNativeSameMemoryReentryLifecycle$' -count=1` | PASS; nested trap/grow/concurrency/lifecycle proof. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; all three new staged execution slices remain explicit-bounds-only. |
| linux/arm64 `go test -c` for `./src/wago` and `./src/core/compiler/backend/railshot/arm64` | PASS compile/link evidence only; no arm64 execution claim. |
| `go vet ./...` | PASS. |
| `go generate ./...` plus generated diff check | PASS; generated facade unchanged. |
| `scripts/bootstrap-wabt.sh --verify` and `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: WABT 1.0.41 and interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions and Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Log `.validation/spec3-iteration21.log`. |
| iteration 21 benchmarks | PASS: memory64 passive init 38.25-40.63 ns/op; table64 grow-by-zero 36.96-38.42 ns/op; two-level retained same-memory native call 242.3-268.6 ns/op; all 0 B/op and 0 allocs/op. |
| iteration 20 staged family runners | PASS: multi-memory remains gap-free at 42 files / 913 commands / 79 modules / 771 assertions; `return_call` remains 47 commands / 3 modules / 33 assertions / 11 invalid; `return_call_indirect` remains 79 commands / 3 modules / 49 assertions / 16 invalid / 11 malformed; `return_call_ref` remains 51 commands / 5 modules / 35 assertions / 11 invalid; memory64 remains 6 files / 807 commands / 7 modules / 92 assertions / 36 gates / 530 blocked / 83 invalid / 59 malformed. Log `.validation/iteration20-staged-final.log`. |
| iteration 20 focused native/product proof | PASS: memory64 copy/fill u64 carry, overlap, trap atomicity, and memory32 code stability; table64 i64 size/get/set, high-index traps, codec-v26 metadata, v25 rejection, snapshot/public/guard/import/multiple/arm64 gates, and table32 code stability; exact mixed-float direct tails with million-step root/nested calls, 10,000 repetitions, trap recovery, close order, and wider-shape rejection. Logs `.validation/iteration20-commit1-focused.log`, `.validation/iteration20-commit1-packages.log`, `.validation/iteration20-commit2-focused.log`, `.validation/iteration20-commit2-packages.log`, `.validation/iteration20-commit2-arm64-build.log`, `.validation/iteration20-commit3-focused.log`, `.validation/iteration20-commit3-packages.log`, and `.validation/iteration20-commit3-official.log`. |
| `go generate ./...` plus generated diff check | PASS; no generated diff. Log `.validation/iteration20-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD. Log `.validation/iteration20-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; no-cgo full suite. Log `.validation/iteration20-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; new staged execution remains explicit-bounds-only. Log `.validation/iteration20-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence only; no arm64 memory64 bulk, table64, or mixed direct-tail execution claim. Log `.validation/iteration20-arm64-build.log`. |
| WABT/interpreter verification | PASS: WABT 1.0.41 and official interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration20-wabt.log` and `.validation/iteration20-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration20-spec1.log` and `.validation/iteration20-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration20.log` and `.validation/iteration20-spec3-baseline.log`. |
| iteration 20 benchmarks | PASS: memory64 64-byte fill 40.14-41.75 ns/op; table64 size 35.02-42.10 ns/op; exact mixed-float direct cross-instance tail 61.37-63.30 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration20-commit1-bench.log`, `.validation/iteration20-commit2-bench.log`, and `.validation/iteration20-commit3-bench.log`. |
| iteration 19 staged family runners | PASS: multi-memory remains gap-free at 42 files / 913 commands / 79 modules / 771 assertions; `return_call` remains 47 commands / 3 modules / 33 assertions / 11 invalid; `return_call_indirect` remains 79 commands / 3 modules / 49 assertions / 16 invalid / 11 malformed; `return_call_ref` remains 51 commands / 5 modules / 35 assertions / 11 invalid; memory64 remains 6 files / 807 commands / 7 modules / 92 assertions / 36 gates / 530 blocked / 83 invalid / 59 malformed. Log `.validation/iteration19-staged-final.log`. |
| iteration 19 focused native/lifecycle proof | PASS: every memory64 SIMD memory form, u64 carry, exact lane/end traps, store atomicity, and memory32 code stability; retained direct cross-instance root/nested tails, million-step and 10,000-transfer proofs, trap recovery, close order, snapshot/public/oversized gates; sole imported-table owner retention, trap atomicity, 1,000+1,000 concurrency, wider-operation rejection, and race detector. Logs `.validation/iteration19-commit1-focused.log`, `.validation/iteration19-commit1-packages.log`, `.validation/iteration19-commit2-focused.log`, `.validation/iteration19-commit2-backend-package.log`, `.validation/iteration19-commit2-wago-package.log`, `.validation/iteration19-commit3-focused.log`, `.validation/iteration19-commit3-race.log`, and `.validation/iteration19-commit3-package.log`. |
| `go generate ./...` plus generated diff check | PASS; no generated diff. Log `.validation/iteration19-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD. Log `.validation/iteration19-all.log`. |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS; no-cgo full suite. Log `.validation/iteration19-no-cgo.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; new staged execution remains explicit-bounds-only. Log `.validation/iteration19-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence only; no arm64 memory64 SIMD, direct cross-tail, or imported-table tenant execution claim. Log `.validation/iteration19-arm64-build.log`. |
| WABT/interpreter verification | PASS: WABT 1.0.41 and official interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration19-wabt.log` and `.validation/iteration19-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration19-spec1.log` and `.validation/iteration19-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration19.log` and `.validation/iteration19-spec3-baseline.log`. |
| iteration 19 benchmarks | PASS: memory64 `v128.load` 35.03-35.82 ns/op; retained direct cross-instance tail 60.97-61.67 ns/op; sole imported-table tenant 108.7-109.5 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration19-commit1-bench.log`, `.validation/iteration19-commit2-bench.log`, and `.validation/iteration19-commit3-bench.log`. |
| iteration 18 staged family runners | PASS: multi-memory remains gap-free at 42 files / 913 commands / 79 modules / 771 assertions; `return_call` remains 47 commands / 3 modules / 33 assertions / 11 invalid; `return_call_indirect` remains 79 commands / 3 modules / 49 assertions / 16 invalid / 11 malformed; `return_call_ref` is now gap-free at 51 commands / 5 modules / 35 assertions / 11 invalid; memory64 is 6 files / 807 commands / 7 modules / 92 assertions / 36 gates / 530 blocked / 83 invalid / 59 malformed. Log `.validation/iteration18-staged-final.log`. |
| iteration 18 focused lifecycle/ABI proof | PASS: active memory64 data codec/instantiate/overflow/snapshot gates; canonical funcref direct/tail identity; imported numeric-global owner retention, local-global rejection, 1,000+1,000 owner/tenant calls, and race detector. Logs `.validation/iteration18-commit1-focused.log`, `.validation/iteration18-commit1-package.log`, `.validation/iteration18-commit2-focused.log`, `.validation/iteration18-commit2-backend-package.log`, `.validation/iteration18-commit2-wago-package.log`, `.validation/iteration18-commit3-focused.log`, `.validation/iteration18-commit3-race.log`, and `.validation/iteration18-commit3-package-final.log`. |
| `go generate ./...` plus generated diff check | PASS; no generated diff. Log `.validation/iteration18-go-generate.log`. |
| `go test ./... -count=1` | PASS on final code HEAD. Log `.validation/iteration18-all.log`. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS; new staged execution remains explicit-bounds-only. Log `.validation/iteration18-guard.log`. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -exec=/bin/true ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -run '^$' -count=1` | PASS compile/link evidence only; no arm64 memory64 data, reference-result tail, or shared-global tenant execution claim. Log `.validation/iteration18-arm64-build.log`. |
| WABT/interpreter verification | PASS: WABT 1.0.41 and official interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Logs `.validation/iteration18-wabt.log` and `.validation/iteration18-spec-interpreter.log`. |
| `make spec1` and `make spec2` | PASS: Release 1 reports 629 modules / 16,026 assertions; Release 2 reports 1,600 modules / 48,248 assertions; zero gaps. Logs `.validation/iteration18-spec1.log` and `.validation/iteration18-spec2.log`. |
| `make spec3` plus baseline extraction/`cmp` | Expected FAIL at unchanged public baseline: modules pass=1,691/skip=535; assertions pass=51,765/fail=5/skip=6,268; committed schema-2 JSON reproduced byte-for-byte. Logs `.validation/spec3-iteration18.log` and `.validation/iteration18-spec3-baseline.log`. |
| iteration 18 benchmarks | PASS: imported-global tenant 78.63-82.27 ns/op; canonical funcref-result tail 97.15-99.04 ns/op; memory64 f64 load 39.01-39.67 ns/op; memory64 integer store/load 38.13-40.92 ns/op; all 0 B/op and 0 allocs/op. Logs `.validation/iteration18-commit3-bench.log`, `.validation/iteration18-reference-tail-bench.log`, and `.validation/iteration18-memory64-bench.log`. |
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
root/nested cross-instance typed-tail and direct-tail transfers, the bounded local-or-
instance-import memory64 size/grow/23-scalar/SIMD-memory/active+passive-data/copy/fill
path, local-or-instance-import table64 size/get/set/grow/fill/call_indirect plus sole-local
passive init/drop and exact two-local size/get/set/grow/fill/copy/init/drop execution, and re-entrant same-memory
basedata-image transitions are
linux/amd64 explicit-bounds only; neither shared
metadata nor the internal frontend bits advertise execution on arm64. Snapshot-v3
record decoding is architecture-neutral, but admission rejects
unsupported target/product shapes before native restore. The
canonical basedata layout remains 256 bytes on amd64 and arm64, including an unused
on-arm64 128-byte wrapper-tail bank so offsets cannot drift by target. Basedata offset
56 now names the active stable same-memory image on amd64 and remains unused by arm64
execution; offset 64 is the indexed-memory directory pointer on amd64
and remains unused for unsupported execution on arm64. Ordinary arm64 `call_indirect` uses the
64-bit structural key because reference-types execution is already public there.
Iteration 15 also makes ordinary arm64 indirect/reference calls mask the third
same-instance-wrapper descriptor tag; this is compatibility for shared descriptor
construction, not typed-tail admission. Iteration 16 adds a private direct/indirect
tail frontend bit so amd64 staged runners can reach proven code, but the compile
boundary rejects that bit on arm64 before backend execution. Iteration 17 adds
architecture-neutral per-table eligibility and shared-basedata safety scans. Iteration
18 adds architecture-neutral i64 active-data metadata validation and imported numeric-
global retention rules. Iteration 19 adds architecture-neutral memory64-aware
immediate walking and sole-imported-table eligibility/ownership scans. Iteration 20 adds architecture-neutral
codec-v26 table-address metadata and support scanning, but per-table/reference-result
tail code, direct cross-instance tail transitions, owner/tenant image switching,
global/table-pointer installation, directory refresh, memory64 scalar/SIMD/data/bulk
execution, table64 i64 lowering, exact mixed-float direct-tail restoration, and the
iteration-21 native same-memory image serializer have linux/amd64 explicit-bounds
evidence only. Iteration 21 also keeps its ImportBinding fields present but inert in
the arm64 backend so cross-compilation remains type-consistent without admission.
Iteration 22 changes architecture-neutral memory64 policy/metadata admission, but its
no-maximum execution reservation, table64 fill lowering, and `(f64) -> i32` direct-tail
transition retain linux/amd64 explicit-bounds evidence only. Iteration 23 adds
architecture-neutral memory address-form compatibility and i64 element-offset product
validation. Its table64 active initialization and imported-global/native-call composition
still have linux/amd64 explicit-bounds execution evidence only; arm64, guard mode, and
public admission reject before those paths. Iteration 24 adds architecture-neutral exact
memory max/no-max and table address-form owner metadata plus private no-maximum table
policy. Its imported memory64 execution/lifecycle, full-u64 table64 indirect lowering,
and imported-table/native-call composition have linux/amd64 explicit-bounds evidence
only; arm64, guard mode, snapshots, and public admission still reject before execution.
Iteration 25 adds architecture-neutral table64 import-limit/address metadata admission,
finite exported no-maximum policy, and simultaneous serializer eligibility. Its imported
size/grow lifecycle, full-u64 table copy lowering, and combined global/table/native-call
execution have linux/amd64 explicit-bounds evidence only; imported copy, arm64, guard
mode, snapshots, and public admission remain explicit gates. Iteration 26 adds
architecture-neutral Core-limit validation and exact memory declaration/product accounting,
plus compile-time two-local-table shape scanning and per-table address-form metadata reuse.
Its passive table64 init/drop lowering, per-operand mixed-width copy canonicalization, and
native two-table directory execution have linux/amd64 explicit-bounds evidence only.
Iteration 27 widens only that compile-time exact two-local shape to indexed read/write,
grow/fill, and passive/declarative lifecycle operations; it reuses the existing native
per-table descriptor directory and persisted segment records. Iteration 28 adds
architecture-neutral exact-shape scanning for local externref table64 read/write/fill/
size/grow and a fixed 1,024-entry no-maximum externref reservation policy. Its opaque-token
runner, i32/i64 operand canonicalization, full-u64 externref fill/grow lowering, and sole/
two/four-table native-directory execution have linux/amd64 explicit-bounds evidence only.
Iteration 29 adds architecture-neutral exact u64 local-limit representation and strict
shape scans for the retained-function three-table lifecycle plus declaration-only local
and imported/local products. Runtime directory execution, staged `spectest.table64`
ownership, and linked descriptor retention still have linux/amd64 explicit-bounds evidence
only. Broader imported copy/init/grow/indirect, arm64, guard mode, snapshots, and public
admission remain explicit gates. Iteration 30 adds architecture-neutral strict
`br_on_non_null` validation, exact `call_ref` callee typing, bounded cross-module structural
function-signature matching, and dense persisted recursive-group numbering. amd64 alone
has execution evidence for explicit reference block decoding and the corrected null-branch
stack transition. Public admission, guard mode, snapshots, and arm64 execution remain
closed; compile-only arm64 evidence confirms that the staged bits do not escape the
platform gate. Iterations 31-33 add architecture-neutral strict EH validation, exact tag
product metadata/lifecycle, codec-v27 tag records, and local-declaration snapshot policy.
The fixed native handler/root layout, catch dispatch, and `throw_ref` pointer transfer have
linux/amd64 explicit-bounds execution evidence only; guard mode and arm64 reject before
backend execution. Iterations 34-35 add architecture-neutral root ownership validation,
including catch-all conflict rejection, while active-handler transfer, root initialization/
clearing, and the exact local typed-funcref payload execute only on linux/amd64 explicit
bounds. The shared frontend recognizes exn/noexn block value types only under the private
exception-reference gate; no public or arm64 feature bit is widened.
`call_ref`, typed null control, indexed multi-memory operations, memory64/table64
execution, and every tail-call lowering remain amd64-only and hidden behind
unsupported family gates. The two
cross-tail scratch slots are layout constants inside the existing 256-byte bank;
ARM64 emits no code that consumes them. Unsupported wider/foreign-float direct,
general-table, and general-reference-result/foreign-float tail contexts remain internal fail-closed
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
   and int-register direct tails now have separate bounded root/nested context transfers;
   wider direct results and reference-result/native-root interactions remain work.

Major risks:

- the zero-parser-failure oracle now depends on development hosts having OCaml,
  dune, and menhir available for the pinned official interpreter build; the cached
  tool is revision-stamped, and any future binary-script grammar change must fail
  the strict converter rather than silently dropping commands;
- codec v27 intentionally invalidates v26 and older caches because v26 has no exception-
  tag directory/export map and earlier table records also lack an address-form bit. Its
  structural graph, value-type pool, indexed-memory/data/table metadata, tag directory,
  and native type-key slice are bounded by decoded module declarations. `Compiled`
  remains 712 bytes; each additional `tableDef` is 56 bytes and each tag record is an
  import string plus u32 type index, so cache/footprint planning must use v27 measurements;
- the staged multi-memory path now executes every scalar and SIMD memory form,
  indexed grow, and bulk/data lifecycle through exact directories. Compact import
  groups and the reached official imported grow/size/linking binaries are decoded
  and exercised directly. Restricted tenants serialize fixed basedata images and
  refresh all entries. Snapshot v3 handles owned local memory sets. Iterations 18-19
  admit finite imported scalar-global arrays and one bounded imported funcref table,
  retaining each explicit owner through tenant close. Iteration 21 also admits retained
  scalar direct calls whose producers use the exact same memory; stable 256-byte images
  switch recursively and recover the active callee image after traps. Iterations 23-25
  prove that those transitions compose first independently and then simultaneously with
  imported numeric-global pointer arrays and the sole imported funcref table, retaining
  memory, function, global, and table owners independently. This is still not a general
  shared-basedata ABI: host callbacks, foreign-memory/imported-tail bindings,
  local/multiple/wider-operation tables, local/reference/vector globals, passive/reference
  tenant state, imported/shared/registered snapshots, codec persistence of live bindings,
  guard mode, public admission, and arm64 remain fail-closed;
- memory64 checks u64 address+offset+width or length carry for all 19 integer,
  4 float, every SIMD memory operation, `memory.copy`, and `memory.fill`; preserves
  float payload bits and memmove overlap; initializes active data from exact i64
  expression metadata; and caps only staged implementation reservation at 65,535 pages.
  Trapping writes are atomic. Passive init/drop preserves the i64 destination plus zero-
  extended i32 source/length contract and dropped-state semantics. Valid declared maxima
  through the Core 3 limit of 2^48 pages remain exact across codec/product/import/policy/
  managed accounting whenever the minimum is allocatable; no-maximum declarations retain
  `HasMax=false`. Unavailable growth returns `-1`, while arithmetic and aggregate budget
  overflow reject fail-closed. One exact instance-exported import preserves provider type,
  shares growth, and retains/rolls back its producer without growing the 40-byte lifecycle
  sidecar. The complete sixteen-file matrix is gap-free at 5,904 commands / 169 modules /
  5,335 assertions / 292 invalid / 60 malformed / 30 unlinkable / zero gates or blocked.
  Exact address-form matching rejects memory32/memory64 import mismatches before attachment.
  Host memory64, unallocatable minima, and shared/multi-memory stay closed;
- table64's complete nine-file family is gap-free at 107 modules / 2,600 assertions /
  81 invalid / zero gates or blocked commands. Sole/imported funcref operations, exact
  two-table mixed widths, local externref forms, and table32/table32/table64 retained-
  function init/drop/copy/indirect all use the native directory with exact operand widths,
  full-u64 checks, trap atomicity, and bounded reservations. Inert declarations preserve
  exact maxima through `2^64-1` while allocating only their minimum, and declaration-only
  two-local plus `spectest.table64` imported/local products preserve no-max identity,
  index order, codec/policy metadata, rollback, retention, and close-order release. The
  same capacity split preserves Release 2 inert table32 behavior. Imported copy/init/grow/
  indirect beyond the proven shapes, snapshots, guard mode, public admission, and arm64
  remain;
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
  thunks. One same-instance canonical funcref result now uses RAX and preserves public
  identity. Per-table immutable-local indirect tails are complete for the pinned file,
  while imported/mutable/exported/host-descriptor tables remain rejected. A separate
  fixed record covers retained integer cross-instance direct tails and exactly
  `(i32, f64) -> f64` plus `(f64) -> i32`; other float/oversized direct signatures, foreign-float/general
  reference-result contexts, snapshots, and arm64 still need proof. The unsupported-
  context trap must disappear from every publicly admitted valid path before the tail-call feature can be enabled;
- typed refs, exceptions, and GC all interact with native frame roots and call
  boundaries. The 14-file matrix proves 50 modules and 211 assertions, exact null control,
  complete official `call_ref`, recursive cross-instance import equivalence, producer
  retention, codec group compaction, public/host/global/table boundaries, and allocation-
  free calls. Its remaining gates are 11 GC shapes and 1 exception-reference shape.
  Exception handling now executes every ordinary official EH module under staged admission,
  including exact narrow cross-instance handler transfer and one local typed-funcref tag
  payload. The remaining EH matrix gates are exactly two mixed WasmGC `ref_null` products.
  Imported-tag snapshots, foreign reference payloads, collector publication, broader typed-
  tail contexts, public admission, and arm64 still must consume the same exact descriptors;
- exception unwind storage is bounded but currently conservative: every EH function frame
  reserves 320 bytes for four seven-word handlers plus four three-word exception roots,
  even when fewer tables/references can be live. This keeps scalar catches, rooted rethrows,
  foreign basedata restoration, and the local funcref lifecycle simple and allocation-free.
  Reference roots initialize before handler publication and the exact local product clears
  them on immediate exn drop; roots are not generally published to a collector frame chain.
  Catch-all maps reject mixed ownership because a function-level map has no tag discriminator.
  Future reduction or broader scanning must be measured and must not reintroduce dynamic
  handler/object storage or scan stale/dead records;
- GC collector code is meaningful but must not be mistaken for executable WasmGC
  until safepoint maps and barriers are connected;
- arm64 must remain fail-closed for every family that lacks native execution tests.

## Next bounded implementation slice

The next recursive iteration should again make exactly three atomic code/test commits
followed by one documentation commit. Recommended iteration 36:

1. **Open the first null-only abstract-reference module without claiming WasmGC heap
   execution.** The first remaining `ref_null` module contains only `ref.null` for `any`,
   `func`, `exn`, `extern`, and one indexed function type, plus immutable null globals and
   five null-return actions. Extend the private support/product type categories so abstract
   any/exn nulls remain zero in the one-slot ABI, preserve exact metadata/codec categories,
   and keep every non-null GC object, cast/test, allocation, mutable/imported storage,
   snapshot, public, guard, and arm64 path closed.
2. **Admit the bottom-type global module as a separate exact product.** It uses immutable
   `none`, `nofunc`, `noexn`, and `noextern` null globals, widens those nulls to their supertypes,
   and returns only zero. Prove AST/byte-backed validation, frontend admission, global.get,
   exact public result matching, codec-v27 metadata, no private heap/collector allocation,
   and strict rejection of non-null/foreign/mutable/imported variants. Recheck whether the
   same work removes the typed-reference matrix's remaining exception-reference gate.
3. **Keep actual WasmGC work honest.** If both null-only modules become green, update EH and
   typed-reference accounting but do not advertise GC execution. Then classify the next
   real GC leader by exact opcodes/object shapes and choose the smallest collector-backed
   allocation/access/root/barrier slice. If null-only type representation cannot remain
   distinct without widening unsafe public ABI categories, land only the metadata/category
   proof and retain the gates.
4. **Documentation commit.** Record exact matrix deltas, zero/non-zero reference invariants,
   codec and footprint evidence, broad validation, unchanged public baseline, remaining
   product/platform work, and the next recursive slice.

## Completion gate

WebAssembly 3.0 is not complete. Iteration 35 fails the gate concretely: the public Release 3
run still has 535 skipped modules, 5 reached assertion failures, and 6,268 skipped assertions;
`make spec3` exits 2 and reproduces the committed baseline byte-for-byte. WasmGC opcodes,
allocation, native safepoint publication, and barriers are not executable. EH has exactly two
mixed null-only GC/reference gates and 32 blocked commands, and its staged execution is not a
public/guard/arm64 product. Codec-v27/snapshot progress covers declarations and exact metadata,
not live imported handlers or collector state. Tail/reference/multi-memory/memory64/table64
surfaces remain staged rather than admitted through `SupportedFeatures()`. Completion still
requires every mandatory area to decode, validate, compile, instantiate, execute, round-trip
through product metadata/lifecycle rules, and pass the pinned official Release 3 suite with
zero unexplained failures or feature skips on linux/amd64, while preserving Release 1/2,
no-cgo operation, bounded memory, and hot-path performance. Arm64 must either reach parity or
remain explicitly gated and documented.
