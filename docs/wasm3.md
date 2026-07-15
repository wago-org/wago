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
- 51,764 assertions passed, 6 failed, and 6,268 were unavailable behind feature
  gaps;
- gap counts are 536 compile rejections, 15 instantiate rejections, and 6,252
  module-unavailable assertions;
- 143 files are green and 115 retain an execution or feature gap.

The six reached-but-failing assertions are no longer relaxed-SIMD oracle noise:
two are in `linking`, three are in `multi-memory/linking0` or
`multi-memory/linking3`, and one is the typed-funcref identity result in `select`.
The linking failures expose memory/table import-state gaps around currently
unsupported Release 3 forms; the `select` failure requires exact non-null
funcref-pattern identity support in the harness/product path. They remain red and
must be explained or fixed before conformance completion.

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
| Tail calls | Decoder and validator understand direct, indirect, and reference tail-call forms. | linux/amd64 has internal frame-reuse milestones for local `return_call` targets that fit the register ABI and `return_call_indirect` through private immutable table 0 with int-only signatures. Public frontend admission remains disabled; imported/wrapper direct targets, mutable/imported/exported/nonzero indirect tables, mixed indirect signatures, and `return_call_ref` remain unsupported. | 🚧 Backend milestone only; not a public product claim. |
| Typed function references | Substantial type/ref/call syntax and validation exists. | Non-basic typed-reference instructions and `call_ref` remain frontend-rejected; runtime representation and call lowering are incomplete. | 🚧 Syntax/validation foundation only. |
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

The compiled codec is now version 21. Version 21 adds:

- deferred scalar initializer programs on `GlobalDef`;
- deferred scalar offset programs on `OffsetInit`;
- strict load/marshal validation of expression opcodes, types, global visibility,
  mutability, stack shape, termination, and mutually exclusive initializer forms.

Version 20 blobs are rejected explicitly. This is an intentional format break:
loading old metadata under an extended initializer layout would be unsafe and
ambiguous. Extended-const source syntax is not added to the byte-sized runtime
required-feature mask because the source feature is compiled into v21 initializer
metadata; the loaded artifact does not re-decode the original Wasm expression.

### Footprint and allocation measurement

A synthetic linux/amd64 module used by the focused execution test contains an
imported `i32`, two dependent extended global initializers, one extended active
data offset, and two exported functions. A temporary measurement test (not
committed) reported:

| Measurement | Result |
|---|---:|
| Wasm module size | 106 bytes |
| `.wago` v21 blob size | 434 bytes |
| Deferred expression payload | 18 bytes |
| `unsafe.Sizeof(GlobalDef{})` | 80 bytes |
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
Imported direct targets and signatures requiring the wrapper ABI fail explicitly
inside the backend. The public frontend still rejects `return_call`, so source
modules cannot accidentally claim broader support.

### Indirect `return_call_indirect`

The indirect milestone is intentionally narrower. It accepts table index 0 only
when module analysis proves a private, immutable, local funcref table and the
selected type is integer-only register ABI. The lowering preserves ordinary
`call_indirect` parity for:

- table bounds;
- null entries; and
- canonical structural signature IDs.

After those checks, the code pointer is stored in bounded basedata scratch,
arguments are staged, the current frame is released, and the pointer is reloaded
into `RSI` for an indirect jump. A million-step table-recursive test passes;
focused OOB, null, and wrong-signature tests produce the existing trap classes.
Exporting the table makes compilation fail closed. Mutable/imported tables,
nonzero tables, mixed/reference/vector signatures, cross-instance descriptors,
and host funcrefs are not yet tail-safe and remain rejected. `return_call_ref`
remains coupled to typed-function-reference work.

### Focused code/stack measurements

A temporary opt-in `ModuleStats` measurement (not committed as a test) reported:

| Synthetic function | Module code | Function code | Frame | Max spill slots | Tail sites |
|---|---:|---:|---:|---:|---:|
| Direct million-step countdown | 142 bytes | 142 bytes | 40 bytes | 0 | 1 direct |
| Indirect table countdown caller | 351 bytes total | 285 bytes | 40 bytes | 0 | 1 indirect |

Both tests complete 1,000,000 recursive steps with a real 40-byte frame, showing
that the frame is reused rather than accumulated. These are code-size/stack
correctness measurements, not throughput benchmarks. No public invocation hot
path changes because the tail-call feature gate remains off.

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

Iteration 3 contains exactly three code/test commits and this documentation
commit:

1. `ce608c61` — pin and bootstrap the official Release 3 reference interpreter.
2. `3453490d` — convert all 28 WABT-rejected files through the official binary
   script path and refresh the zero-parser-failure inventory.
3. `8fbab308` — implement official alternative result matching and close all 32
   relaxed-SIMD harness failures.

## Validation performed

Commands were run from the repository root on linux/amd64.

| Command | Result |
|---|---|
| `scripts/bootstrap-wabt.sh --verify` | PASS: checksum-pinned `wast2json 1.0.41` at `.tools/wabt-1.0.41-linux-x64/bin/wast2json`. |
| `scripts/bootstrap-spec-interpreter.sh --verify` | PASS: official `wasm 3.0.0 reference interpreter` built from `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. |
| `go test ./internal/spectest ./src/wago -run 'TestCommittedRelease3Baseline\|TestRelease3Interpreter\|TestResolveSpecInterpreter\|TestResolveWast2JSON\|TestResolveSpecPlanRelease3\|TestSpecInterpreterModuleDefinitionInstances\|TestMatchEitherResult\|TestSpecValueV128StructuredJSON' -count=1` | PASS: both tool pins, strict binary-script conversion, definition/instance replay, baseline accounting, and scalar/vector alternative results are covered. |
| `go test ./src/core/compiler/backend/railshot/amd64 -run 'TestReturnCallDirect\|TestReturnCallIndirect' -count=1` | PASS: the previous direct/indirect million-step tail milestones and trap/rejection boundaries remain green. |
| `go test ./... -count=1` | PASS on final code HEAD. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o .validation/wago-arm64.test ./src/wago` | PASS; artifact removed. This is build evidence only; arm64 remains explicitly fail-closed for unsupported 3.0 families. |
| `make spec3` | FAIL as required with zero parser failures and 28 official-interpreter fallbacks: modules pass=1,691/skip=535; assertions pass=51,764/fail=6/skip=6,268. Relaxed SIMD is green at 8 modules/69 assertions. |
| `python3 scripts/spec3-baseline.py .validation/spec3-iteration3-final.log .validation/spec3-iteration3-final.json --exit-code 2 && cmp tests/spec-v3-baseline.json .validation/spec3-iteration3-final.json` | PASS: the schema-2 committed inventory reproduces byte-for-byte. |

The larger skipped totals relative to the historical WABT-only baseline are
intentional: 28 previously unparsed files now contribute their real feature gaps.
The 32 removed failures were harness-oracle errors for valid relaxed-SIMD
alternative results, not backend changes. Existing 1.0/2.0 external corpora were
not rerun separately; repository-wide and guard-page suites passed.

## Architecture policy

The primary claim remains linux/amd64. Unsupported 3.0 feature bits are rejected
before backend execution with an error that includes the current `GOOS/GOARCH`.
This prevents arm64 from silently accepting tail calls, typed function references,
GC, exceptions, multi-memory, memory64, or table64.

Extended constant expressions are architecture-neutral compile/instantiation
metadata. Tail-call lowering is amd64-only and still hidden behind the public
unsupported family gate. The arm64 cross-compiled test binary includes an
architecture-specific assertion that tail calls are not advertised and that a
request reports `linux/arm64` (or the actual arm64 GOOS) in
`UnsupportedFeatureError`. Native arm64 execution was not run, so the final 3.0
completion gate still requires either parity evidence or the documented platform
restriction for each executable family.

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
- codec v21 intentionally invalidates v20 caches;
- multi-memory changes instance metadata, import/export APIs, snapshots, and every
  memory opcode hot path;
- memory64 can turn existing 32-bit arithmetic assumptions into overflow or
  reservation bugs;
- direct wrapper/import tail calls and mutable/cross-instance indirect tail calls
  need an ABI that removes the current activation without accumulating adapters;
- typed refs, exceptions, and GC all interact with native frame roots and call
  boundaries;
- GC collector code is meaningful but must not be mistaken for executable WasmGC
  until safepoint maps and barriers are connected;
- arm64 must remain fail-closed for every family that lacks native execution tests.

## Next bounded implementation slice

The next recursive iteration should again make exactly three atomic code/test
commits followed by one documentation commit:

1. **General direct tail ABI.** Extend `return_call` beyond local internal entries
   to wrapper-only, imported, and cross-instance targets. Preserve module/global
   context, host re-entry, traps, and results while proving bounded native-stack
   use across same-instance and cross-instance cycles.
2. **General indirect tail ABI.** Extend `return_call_indirect` to nonzero,
   mutable, imported, and exported tables plus mixed GP/XMM register-ABI
   signatures. Preserve OOB/null/canonical-signature traps and reject host or
   cross-instance descriptors until their context-switch path is demonstrably
   tail-safe.
3. **Typed-reference call beachhead.** Execute typed non-null funcref values and
   `call_ref`, then add `return_call_ref` only when it shares the proven tail ABI.
   Preserve null/signature traps and strict subtype validation. Keep
   `CoreFeatureTailCall` disabled unless all three tail instructions are complete.
4. **Documentation commit.** Refresh exact suite/parser totals, tail ABI coverage,
   typed-reference state, measurements, product/platform gates, and the following
   bounded slice.

## Completion gate

WebAssembly 3.0 is not complete. Completion still requires every mandatory area
to decode, validate, compile, instantiate, execute, round-trip through product
metadata/lifecycle rules, and pass the pinned official Release 3 suite with zero
unexplained failures or feature skips on linux/amd64, while preserving 1.0/2.0,
no-cgo operation, bounded memory, and hot-path performance. Arm64 must either
reach parity or remain explicitly gated and documented.
