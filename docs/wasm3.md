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

A full Release 3 execution baseline was **not** produced in this iteration because
`wast2json` was not installed. `make spec3` failed immediately with the explicit
missing-WABT diagnostic. Suite discovery itself was exercised successfully.

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
| Relaxed SIMD | Complete through `0xfd 275`, with reserved holes rejected. | Deterministic lowering is present on the documented linux/amd64 SIMD baseline. | ✅ Existing completed support, represented by `CoreFeatureSIMD`. |
| Tail calls | Decoder and validator understand direct, indirect, and reference tail-call forms. | Frontend rejects them explicitly; no railshot frame-reuse lowering exists. | ⬜ Not executable. |
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

## Iteration commits

This bounded iteration contains exactly three code/test commits and this
substantial documentation commit:

1. `f98f89fc` — pin the official WebAssembly 3.0 suite and make Release 3 skips
   fail the harness.
2. `298a20c7` — add the mandatory 3.0 feature model, platform admission metadata,
   and explicit frontend family errors.
3. `d768006c` — implement and execute basic extended constant expressions,
   including `.wago` v21 persistence.

## Validation performed

Commands were run from the repository root on linux/amd64.

| Command | Result |
|---|---|
| `go test ./internal/spectest ./src/core/compiler/wasm ./src/core/compiler/frontend ./src/wago -run 'TestDiscoverRelease3\|TestResolveSpecPlanRelease3\|ExtendedConst\|CoreFeaturesV3\|RejectUnsupportedProposalFeaturesDecodedByWasm3' -count=1` | PASS in all four packages. |
| `go test ./... -count=1` | PASS after regenerating `wago.go`; all repository Go packages built and tested. |
| `go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1` | PASS. The serialization test uses explicit bounds because signal-bounds artifacts are intentionally non-serializable. |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o .validation/wago-arm64.test ./src/wago` | PASS; cross-compiled test binary produced and removed. This is a build gate, not native arm64 execution evidence. |
| `make spec3` | BLOCKED/FAIL as designed: `wast2json (wabt) not on PATH`. No Release 3 assertion totals are claimed. |
| `git -C tests/spec-v3 rev-parse HEAD`, `git -C tests/spec-v3 tag --points-at HEAD`, and corpus count | Confirmed commit `9d360199...`, tag `wg-3.0`, and 258 core `.wast` files. |

Existing WebAssembly 1.0/2.0 and relaxed-SIMD claims were not re-baselined against
external WABT corpora because the tool was unavailable. The in-repository full Go
suite and guard-page suite passed. No performance-sensitive native instruction
lowering changed in this iteration.

## Architecture policy

The primary claim remains linux/amd64. Unsupported 3.0 feature bits are rejected
before backend execution with an error that includes the current `GOOS/GOARCH`.
This prevents arm64 from silently accepting tail calls, typed function references,
GC, exceptions, multi-memory, memory64, or table64.

Extended constant expressions are architecture-neutral compile/instantiation
metadata and the linux/arm64 test binary cross-compiled successfully. Native arm64
execution was not run in this iteration, so the final 3.0 completion gate still
requires either native parity evidence or an explicit platform restriction for
each executable family.

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

- current host WABT absence prevents measured Release 3 gap totals;
- codec v21 intentionally invalidates v20 caches;
- multi-memory changes instance metadata, import/export APIs, snapshots, and every
  memory opcode hot path;
- memory64 can turn existing 32-bit arithmetic assumptions into overflow or
  reservation bugs;
- typed refs, exceptions, and GC all interact with native frame roots and call
  boundaries;
- GC collector code is meaningful but must not be mistaken for executable WasmGC
  until safepoint maps and barriers are connected;
- arm64 must remain fail-closed for every family that lacks native execution tests.

## Next bounded implementation slice

The next recursive iteration should again make exactly three atomic code/test
commits followed by one documentation commit:

1. **Reproducible Release 3 tool/baseline commit.** Pin or bootstrap a known WABT
   version without cgo, add deterministic version checks, run the 258-file suite,
   and commit a machine-readable failure inventory grouped by mandatory family.
   Tool/parser failures must remain hard failures.
2. **Direct tail-call milestone.** Implement amd64 `return_call` for local/direct
   functions with exact result-shape checks and frame/stack reuse tests. Keep the
   public tail-call family disabled if indirect/reference forms are still absent.
3. **Indirect tail-call milestone.** Implement `return_call_indirect` for the
   existing funcref table/signature model, including trap parity and explicit
   arm64 rejection/build tests. Leave `return_call_ref` gated with typed function
   references unless it is completed atomically.
4. **Documentation commit.** Record official-suite totals, tail-call coverage and
   remaining skips, disassembly/stack measurements, platform gates, and the next
   three-commit slice.

## Completion gate

WebAssembly 3.0 is not complete. Completion still requires every mandatory area
to decode, validate, compile, instantiate, execute, round-trip through product
metadata/lifecycle rules, and pass the pinned official Release 3 suite with zero
unexplained failures or feature skips on linux/amd64, while preserving 1.0/2.0,
no-cgo operation, bounded memory, and hot-path performance. Arm64 must either
reach parity or remain explicitly gated and documented.
