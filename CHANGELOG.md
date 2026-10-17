# Changelog

Notable user-facing changes to Wago are recorded here. This changelog starts
with `v0.1.0-beta.8`; earlier prereleases remain available in the
[GitHub Releases](https://github.com/wago-org/wago/releases) history.
The [merged PR index](#merged-pull-request-index) includes the complete merged
history, including work before beta.8.

## [Unreleased]

## [v0.1.0-beta.13] - 2026-10-15

Release preparation; the comparison below is pinned to the preparation commit.

### Added

- Support shared memory on AMD64 and ARM64: scalar/SIMD loads and stores, `memory.size`,
  `memory.grow`, overlap-safe `memory.copy`, `memory.fill`, classic atomics, and
  wait/notify. Preserve bounds checks, exact float/vector bits, quotas, codec reload,
  and backing ownership across exports and producer close. Growers publish the new
  extent only after pages are accessible; active instances read the live extent. Allow
  `atomic.fence` without linear memory.
  ([#963](https://github.com/wago-org/wago/pull/963))
- Support wide integer arithmetic on AMD64 and ARM64, including carry chains and
  signed/unsigned widening multiplies, with public feature selection and persisted
  artifact requirements. ([#963](https://github.com/wago-org/wago/pull/963))
- Expose compact imports through `CoreFeatureCompactImports` and CLI discovery,
  independently of multiple-memory admission. Grouped imports are enabled by default.
  ([#963](https://github.com/wago-org/wago/pull/963))
- Add SIMD floor-average and widening-multiply source-pair tests, benchmarks, and
  bounded corpus checks. ([#875](https://github.com/wago-org/wago/pull/875),
  [#874](https://github.com/wago-org/wago/pull/874))
- Add mixed-scalar host-call checks across nested memory, global, and table changes,
  including nested traps and repeated resolved calls.
  ([#868](https://github.com/wago-org/wago/pull/868))
- Bind corpus child completion to loaded input identity and exact invocation evidence,
  with omitted-call and same-result substitution controls.
  ([#876](https://github.com/wago-org/wago/pull/876))
- Extend the opt-in `wago_regalloccheck` diagnostics with control-flow graphs, original
  source recipes, immutable constant identities, and checked emission journals on AMD64
  and ARM64. ([#807](https://github.com/wago-org/wago/pull/807))
- Expand cached bounded-host rejection recovery checks and portable CLI/compiler
  fixtures. Document the bounded Wasm-R3 corpus admission exclusions.
  ([#941](https://github.com/wago-org/wago/pull/941),
  [#912](https://github.com/wago-org/wago/pull/912),
  [#949](https://github.com/wago-org/wago/pull/949),
  [#861](https://github.com/wago-org/wago/pull/861))

### Changed

- Share immutable native callback wrappers across instances; release the code mapping
  when its last owner closes. ([#963](https://github.com/wago-org/wago/pull/963))
- Reduce scalar host-import overhead with guarded ARM64 loop entry, register arguments,
  cached dispatch metadata, and smaller callback resume state on both native backends.
  Keep every original callback, Go/foreign fallback, artifact checks, and rollback
  policy. Fixed shared-memory integer routes still require whole-module and binding
  proofs. ([#963](https://github.com/wago-org/wago/pull/963))
- Forward an adjacent plain i32 store/load pair in one fixed-size shared memory32 on
  ARM64. Preserve the store and bounds/trap behavior; atomic, host-boundary, and
  profiling cases retain their access paths. Select with `store-forward`.
  ([#963](https://github.com/wago-org/wago/pull/963))
- Cache shared-memory backing addresses while reading live extents on AMD64 and ARM64
  (`shared-memory-base`). Skip atomic bounds checks only for proven aligned constant
  addresses (`atomic-static-bounds`).
  ([#963](https://github.com/wago-org/wago/pull/963))
- Use ARM64 acquire/release LSE atomics when supported, retaining the exclusive-loop
  fallback and artifact CPU requirements (`atomic-lse`). Handle literal zero-timeout
  waits natively while preserving alignment/bounds trap order
  (`atomic-wait-zero-timeout`); other waits keep the parking helper.
  ([#963](https://github.com/wago-org/wago/pull/963))
- Reuse exception payload summaries across functions on AMD64 and ARM64 to reduce
  compilation work for modules with many tags and `catch_all_ref` handlers.
  ([#837](https://github.com/wago-org/wago/pull/837))
- Improve ARM64 scalar/vector loops, register reuse, indexed addresses, integer
  reductions, floating-point maps, static table bounds, and arithmetic/branch
  simplification. Optional lowerings retain strict admission checks and per-compilation
  rollback. Reduce option-resolution, loop-scan, and native-image allocation work.
  ([#943](https://github.com/wago-org/wago/pull/943))
- Admit exact typed i64 synchronous host callbacks to the private prepared integer route
  when the bounded-work and detached-context proofs hold. Other callback shapes retain
  their existing route; each synchronous host binding grows by one function-value slot.
  ([#939](https://github.com/wago-org/wago/pull/939),
  [#940](https://github.com/wago-org/wago/pull/940))
- Reduce ordinary, prepared numeric, and synchronous numeric host-call entry overhead by
  combining instance lifetime and invocation-gate admission, preserving close, callback
  publication, revocation, and waiter handoff.
  ([#908](https://github.com/wago-org/wago/pull/908),
  [#938](https://github.com/wago-org/wago/pull/938))
- Reduce AMD64 compiler scratch growth through operand-arena recycling, constant-time
  control-frame tracking, and targeted pointer cleanup.
  ([#835](https://github.com/wago-org/wago/pull/835),
  [#890](https://github.com/wago-org/wago/pull/890))
- Reduce data-section decoding and execution-snapshot allocations with checked vector
  preallocation and compact immutable active-data records, preserving public data
  ownership and artifact encoding. ([#835](https://github.com/wago-org/wago/pull/835))
- Reduce AMD64 and ARM64 synchronous host-call round trips with bounded native bridges,
  private prepared owners, and specialized numeric marshalling. This adds 32 bytes to
  the instance plugin sidecar and about 192 KiB to stripped Go runtime binaries (63.3
  KiB for the minimal TinyGo runtime).
  ([#872](https://github.com/wago-org/wago/pull/872))
- Reduce repeated imported funcref retention scans and share source-root snapshots
  across imported tables and globals.
  ([#904](https://github.com/wago-org/wago/pull/904))
- Avoid quadratic recursive-group type-equivalence scans during validation.
  ([#902](https://github.com/wago-org/wago/pull/902))
- Reduce AMD64 branch-fold relocation cleanup and avoid duplicate ARM64 immediate store
  materialization. ([#903](https://github.com/wago-org/wago/pull/903),
  [#897](https://github.com/wago-org/wago/pull/897))
- Speed up profile sample resolution and Samply function metadata joins by indexing live
  images and repeated function lookups.
  ([#907](https://github.com/wago-org/wago/pull/907),
  [#900](https://github.com/wago-org/wago/pull/900))
- Improve AMD64 and ARM64 lowering for small blocks, vector fills, adjacent SIMD
  addresses, lane extraction, and proven minimum-memory bounds. Reuse pure integer
  terms, eligible floating-point globals, and loop constants on ARM64. Optional
  lowerings remain independently selectable.
  ([#963](https://github.com/wago-org/wago/pull/963))

### Fixed

- Order ordinary guest stores before the lock-free empty-notify check with a full memory
  barrier on AMD64 and ARM64, preventing missed waiter registration.
  ([#963](https://github.com/wago-org/wago/pull/963))
- Preserve the invocation parent for Caller context resolution through foreign native
  imports without publishing identity under a shared producer control frame. Independent
  relays retain separate callback scopes and cancellation.
  ([#963](https://github.com/wago-org/wago/pull/963))
- Read Bash completion output synchronously while preserving nested arguments, candidate
  text, and file-completion fallback for empty results.
  ([#946](https://github.com/wago-org/wago/pull/946))
- Keep ARM64 i64x2 multiply lowering within five transient vector registers under
  pinned-local pressure. ([#874](https://github.com/wago-org/wago/pull/874))
- Keep indexed memory sizes and bounds coherent across aliased imports and native calls
  on AMD64 and ARM64. Recompile native artifacts to receive the fix.
  ([#944](https://github.com/wago-org/wago/pull/944))
- Report batched Wago and wazero execution benchmark allocations per operation, matching
  their timing units instead of reporting allocations per batch.
  ([#878](https://github.com/wago-org/wago/pull/878))
- Check encoded ARM64 register aliases in NEON shuffle and byte-movemask helpers so
  source and scratch overlap cannot bypass their guards.
  ([#888](https://github.com/wago-org/wago/pull/888),
  [#893](https://github.com/wago-org/wago/pull/893))
- Keep ARM64 integer call results in frame slots when local pins leave too few registers
  for all result copies. ([#862](https://github.com/wago-org/wago/pull/862))
- Invalidate stale vector facts after indexed AMD64 loads in the optional
  register-allocation checker, including encoded register aliases, without allocating
  temporary kill values. ([#860](https://github.com/wago-org/wago/pull/860))
- Key ARM64 worker adapter reuse by the memory-size register to prevent stale entry code
  when leaf and memory-access functions share a type.
  ([#866](https://github.com/wago-org/wago/pull/866))
- Rearm cancellation overwritten during Windows host calls and preserve canceled context
  errors on void invocation returns. ([#889](https://github.com/wago-org/wago/pull/889))
- Drop native mapping owners and borrowed views before releasing memory, preventing
  stale pointers from being scanned after Windows address reuse.
  ([#947](https://github.com/wago-org/wago/pull/947))
- Remove the fixed native frame-depth limit from GC root discovery while checking mapped
  stack bounds, including parked host activations across reentry.
  ([#898](https://github.com/wago-org/wago/pull/898))
- Preserve imported funcref producer lifetimes across finalization and failed
  instantiation; clear early unowned table writes during rollback.
  ([#904](https://github.com/wago-org/wago/pull/904))
- Deduplicate executable code-range registration on Linux and Darwin ARM64, avoiding
  premature exhaustion of the range table.
  ([#886](https://github.com/wago-org/wago/pull/886))
- Preserve session PATH entries when refreshing a PowerShell installation.
  ([#887](https://github.com/wago-org/wago/pull/887))
- Reject existing symlinks and non-directory project build-cache roots during cleanup
  and pruning. Concurrent path replacement remains outside this check.
  ([#885](https://github.com/wago-org/wago/pull/885))
- Resolve descriptor instruction type indexes within their recursive group and report
  absolute byte offsets for byte-backed decode errors.
  ([#884](https://github.com/wago-org/wago/pull/884),
  [#896](https://github.com/wago-org/wago/pull/896))
- Match custom instruction imports by their exact module/name pair, avoiding collisions
  when either component contains dots.
  ([#883](https://github.com/wago-org/wago/pull/883))
- Escape every textual perf-map symbol field to prevent format injection.
  ([#894](https://github.com/wago-org/wago/pull/894))
- Preserve mixed scalar host-call results, restore pinned locals/globals after result
  loads, and prevent general-purpose allocation from spilling floating-point registers
  on ARM64. Reject out-of-range adapter PC-relative address patches.
  ([#868](https://github.com/wago-org/wago/pull/868),
  [#899](https://github.com/wago-org/wago/pull/899))
- Align the installer dependency and workspace replacement with beta.13 for release
  qualification.
  ([46d70c6](https://github.com/wago-org/wago/commit/46d70c6957a04c94e49330df3612fd185dd9823c))

## [v0.1.0-beta.12] - 2026-10-07

### Added

- Add opt-in native profiling commands to record workloads, inspect hot functions and
  instructions, compare captures, and view invocation timelines.
- Add compile-time register-transfer and immutable-register lifetime checks for AMD64
  and ARM64 through the `wago_regalloccheck` build tag, plus a bounded control-flow
  verification core.
- Add an experimental shared scalar compiler path for AMD64 and ARM64.
- Add Wazy to the cross-runtime benchmark corpus and publish its results.
- Add AMD64 bounds-proof invalidation tests and compile/execution benchmarks.
  ([#857](https://github.com/wago-org/wago/pull/857))
- Add rejection controls for omitted native padding and artifact metadata.
  ([#858](https://github.com/wago-org/wago/pull/858))
- Add host-event timing-boundary and replay-failure controls.
  ([#859](https://github.com/wago-org/wago/pull/859))
- Add exact core-SIMD source-pair checks and benchmarks for byte averages, high-byte
  extraction, and signed constant dot/sub expressions.
  ([#863](https://github.com/wago-org/wago/pull/863))
- Add semantic result-profile controls and loaded-artifact/omitted-case checks to the
  conformance test runner. ([#864](https://github.com/wago-org/wago/pull/864))
- Add shared/established compiler-path comparisons at admission boundaries, with
  typed-control validation, execution controls, and paired benchmarks.
  ([#865](https://github.com/wago-org/wago/pull/865))
- Add bounded rule-directed scalar, SWAR, and SIMD compiler checks, independent result
  models, activation controls, and equal-budget generation benchmarks.
  ([#867](https://github.com/wago-org/wago/pull/867))
- Add AMD64 loop-boundary regression checks for deferred producer placement,
  floating-point transport, register pressure, and zero-iteration traps.
  ([#853](https://github.com/wago-org/wago/pull/853))
- Add AMD64 fresh-worker comparisons for reused compiler scratch, error recovery, native
  execution, and worker memory release, with a test-only omitted-reset control.
  ([#854](https://github.com/wago-org/wago/pull/854))
- Add two prebuilt Grain standard-library assertion suites to the executable corpus,
  with matching guest sources, reproducible rebuild instructions, preserved licenses,
  and independently verified WASI execution oracles.
  ([#777](https://github.com/wago-org/wago/pull/777))

### Changed

- Reduce AMD64 compilation allocations and repeated work in immutable-table analysis,
  adapter grouping, spill tracking, local realization, and barriers.
- Avoid callback-context allocations and cancellation watchers for synchronous re-entry
  from invocations with no cancellation or deadline signal.
- Keep native profiling and compiler diagnostics out of ordinary builds; enable them
  with `wago_profile` and `wago_codegenstats` build tags.
- Raise release size limits to 12 MB for Go builds and 3 MB for TinyGo.
- Reuse both AMD64 hardware divide results for adjacent division/remainder pairs on the
  same local operands, reducing dependent-pair latency.
  ([#852](https://github.com/wago-org/wago/pull/852))
- Emit one AMD64 immediate store for i64 constants representable as a signed imm32, and
  shorten guarded constant-store register lifetimes under pressure.
  ([#850](https://github.com/wago-org/wago/pull/850))
- Improve AMD64 execution with bounded local and caller register reuse, direct
  branch-result transfers, memory operands, and scalar/vector loop lowering.
  ([#780](https://github.com/wago-org/wago/pull/780))
- Reduce compilation work by skipping GC conversion scans for validated non-GC modules
  and synchronizing only the globals assigned to physical registers.
  ([#780](https://github.com/wago-org/wago/pull/780))
- Reuse owned integer shift operands and an additional revocable local register in
  admitted straight-line AMD64 functions.
  ([#780](https://github.com/wago-org/wago/pull/780))

### Fixed

- Publish `wago build` output atomically, preserving existing artifacts when publication
  fails and respecting platform access controls and file metadata.
- Bound CLI module reads and artifact-cache admission, rejecting oversized inputs before
  cache lookup or allocation.
- Serialize plugin builds and configuration mutations, keep rejected staged rebuilds out
  of the active runtime, and cancel active Go commands during staged plugin changes.
  Bound verification and retry runners after publication.
- Read plugin metadata as one snapshot and isolate mutable plugin-set metadata.
- Make `wago add --allow-all` noninteractive, route plugin inspection aliases to the
  runtime, and group canonical plugin repository paths consistently.
- Keep version-cache cleanup within its intended tree, select stable semantic versions
  for official releases, and resolve annotated rolling-release tags.
- Reconnect refreshed installer shells to terminal input, await CLI runtime teardown,
  and preserve terminal close observers during shutdown.
- Reject stale local configuration saves instead of overwriting newer changes.
- Preserve guest roots and foreign-clone tokens during GC graph reconstruction; avoid
  retaining roots for immediately dropped extern-reference conversions.
- Preserve AMD64 pinned registers across `table.copy`, exclude fixed bulk scratch
  registers from immutable caches, and reload canonical ARM64 `externref` destinations
  for `table.fill`.
- Correct deferred spill preservation, fixed-register conversion and comparison
  operands, `i16x8.bitmask` under register pressure, and memory32 wraparound in unrolled
  linear sums. Restore pinned locals across control and exception edges.
- Honor configured Core features in standalone compilation, reject invalid bounds-check
  modes and nil modules, decode block type indexes as signed 33-bit values, and validate
  descriptor metadata and equality casts.
- Use structural identities for indirect-call types and sort provided imports by exact
  identity. Correct large ARM64 host-thunk transfers and wide synchronous host exchange
  areas.
- Correct worker scratch-growth coverage, bounds-test mode isolation, semantic mismatch
  counts, optional Node feature checks, and prepared-call benchmark setup.
  ([#869](https://github.com/wago-org/wago/pull/869))
- Match direct and validated feature summaries for bulk table instructions.
  ([#855](https://github.com/wago-org/wago/pull/855))
- Preserve live AMD64 values across signed i64 division when the overflow guard runs
  under register pressure. ([#851](https://github.com/wago-org/wago/pull/851))
- AMD64 guarded i64 constant stores use a single eight-byte native store. ARM64
  additionally checks the entire access before writing, preventing partial memory
  updates from trapping unaligned constant stores.
  ([#850](https://github.com/wago-org/wago/pull/850))
- ARM64 table64 bulk operations now trap when `start + count` wraps instead of accepting
  the wrapped range and addressing outside the table allocation.
  ([#791](https://github.com/wago-org/wago/pull/791))
- Preserve live AMD64 collector references loaded by `global.get` across later
  allocations. ([#785](https://github.com/wago-org/wago/pull/785))
- AMD64 and ARM64 now report deferred division, remainder, and guard-page load traps
  before later conversion, memory, table, reference, and indirect-call traps.
  ([#789](https://github.com/wago-org/wago/pull/789))
- Preserve earlier call arguments when later deferred expressions use fixed registers
  for shifts or division, including mixed and tail calls.
  ([#780](https://github.com/wago-org/wago/pull/780))
- Legacy exception-handling opcodes now report their unsupported encoding and a
  `try_table`/exnref migration hint instead of a generic invalid instruction.
  ([#774](https://github.com/wago-org/wago/pull/774))
- Recovered backend panics now return a distinct `InternalCompilerError` with
  function/bytecode context and bounded diagnostics instead of ordinary rejection.
  ([#778](https://github.com/wago-org/wago/pull/778))
- AMD64 and ARM64 preserve deferred division, remainder, and guard-page load traps
  before tail-call target checks or callee side effects.
  ([#773](https://github.com/wago-org/wago/pull/773))
- AMD64 guest floating-point execution now uses WebAssembly's nearest-even,
  masked-exception state and restores the host thread's MXCSR on every exit.
  ([#790](https://github.com/wago-org/wago/pull/790))
- AMD64 and ARM64 discard exited `try_table` handlers on taken branches, so later
  exceptions cannot jump into an exited scope or a returned frame.
  ([#772](https://github.com/wago-org/wago/pull/772))
- Preserve AMD64 `select` operands when deferred divisions, remainders, shifts, or
  rotates reclaim fixed registers, and report operand traps in WebAssembly evaluation
  order. ([#741](https://github.com/wago-org/wago/pull/741))
- Tables with no declared maximum can grow past 1,024 entries; a table can now at least
  double from its initial size. ([#770](https://github.com/wago-org/wago/pull/770))
- GC tables accept Core 3 initializers such as `(ref.null any)`, `ref.i31` and extended
  constants, and constant expressions accept chains of
  `any.convert_extern`/`extern.convert_any` on null. An allocating initializer is
  evaluated once, so every slot holds the same object.
  ([#770](https://github.com/wago-org/wago/pull/770),
  [#771](https://github.com/wago-org/wago/pull/771))

- AMD64 nullability refinements preserve whether a reference belongs to the compact
  collector, so native function references are not scanned as GC roots.
  ([#786](https://github.com/wago-org/wago/pull/786))
- ARM64 scalar `min`/`max` local sinking now preserves an aliased right operand instead
  of overwriting it with the left operand before the comparison.
  ([#792](https://github.com/wago-org/wago/pull/792))
- Non-nullable bottom reference types such as `(ref noextern)` are accepted.
  ([#770](https://github.com/wago-org/wago/pull/770))
- `Global.GetValue`/`SetValue` work for `externref` globals holding converted GC
  references. ([#770](https://github.com/wago-org/wago/pull/770))
- AMD64 no longer crashes when a tail target throws after a wrapper-ABI tail call made
  inside a `try_table`, and accepts tail calls from register-ABI to result-bearing
  wrapper-ABI functions. ARM64 mixed register/wrapper tail loops no longer grow the
  stack. ([#770](https://github.com/wago-org/wago/pull/770))
- Managed code-generation plugins can no longer type-assert their restricted callback
  context to the full raw-encoder context on AMD64 or ARM64.
  ([#793](https://github.com/wago-org/wago/pull/793))
- A `try_table` may have up to 1,024 catch clauses (previously 8).
  ([#770](https://github.com/wago-org/wago/pull/770))
- ARM64 tail calls from a function with reference-typed parameters into a numeric
  register-ABI function no longer lose the callee's updates to register-pinned module
  globals. ([#739](https://github.com/wago-org/wago/pull/739))
- AMD64 tail calls from a function with reference-typed parameters into a function that
  is only called directly now pass the correct arguments and keep its updates to
  register-pinned module globals. ([#739](https://github.com/wago-org/wago/pull/739))
- Decode `0x64` consistently as the Core 3 non-null reference prefix, avoiding
  cross-vector consumption when it appears at the end of a value-type vector.
  ([#794](https://github.com/wago-org/wago/pull/794))
- AMD64 and ARM64 compile a `try_table` that has parameters in unreachable code instead
  of failing with an internal code-generation error.
  ([#739](https://github.com/wago-org/wago/pull/739))
- Darwin/ARM64 cancellation and `Close` no longer hang behind a concurrent garbage
  collection, and an interrupted call is no longer occasionally reported as successful.
  ([#739](https://github.com/wago-org/wago/pull/739))
- AMD64 no longer skips code after a `try_table` that is exited by a branch to its own
  end (for example `br 0`), which previously ran the exception handler instead and could
  return early or report an unhandled exception.
  ([#742](https://github.com/wago-org/wago/pull/742))
- Full-access AMD64 plugins can declare every optional CPU feature exposed by the
  encoder, and compiled artifacts preserve those admission requirements.
  ([#795](https://github.com/wago-org/wago/pull/795))
- Dropping a null `exnref` produced by the fallthrough of a `catch_ref` or
  `catch_all_ref` target no longer crashes the process on AMD64 or ARM64.
  ([#742](https://github.com/wago-org/wago/pull/742))
- A module declaring an unused exception tag with more than eight parameters is now
  rejected at compile time with a clear limit error instead of failing at instantiation.
  ([#742](https://github.com/wago-org/wago/pull/742),
  [#771](https://github.com/wago-org/wago/pull/771))

- Exception records are sized per function: a function may nest up to 1,024 `try_table`
  levels and hold up to 1,024 `catch_ref`/`catch_all_ref` results (previously 4 each,
  reserved in every function of a module with tags).
  ([#771](https://github.com/wago-org/wago/pull/771))
- Exception handler and rooted-catch indexes retain their full admitted range above 256
  records on AMD64 and ARM64. ([#788](https://github.com/wago-org/wago/pull/788))
- Exception tags may carry up to eight values (previously two), including GC references
  such as structs, arrays, `i31ref`, `anyref` and `externref`.
  ([#771](https://github.com/wago-org/wago/pull/771))
- AMD64 `catch_all_ref` catches exceptions of every tag, not only the first declared
  one. ([#771](https://github.com/wago-org/wago/pull/771))
- `return_call_ref` works between any register- and wrapper-ABI signatures on AMD64. On
  ARM64, mixed tail loops through references no longer grow the stack, and a wrapper-ABI
  function's tail call into another instance no longer leaves that instance installed
  for its caller. ([#771](https://github.com/wago-org/wago/pull/771))
- Compiled artifacts record their native architecture and reject foreign-ISA code before
  allocating an executable image. ([#796](https://github.com/wago-org/wago/pull/796))
- Modules using `any.convert_extern`/`extern.convert_any` instantiate with any
  combination of reference tables. ([#771](https://github.com/wago-org/wago/pull/771))
- `ref.test` on an `anyref` converted from a host `externref` no longer traps at random
  on AMD64, and casts of such a value report a cast failure on ARM64.
  ([#771](https://github.com/wago-org/wago/pull/771))
- Global `ref.i31` initializers whose operand is extended-constant arithmetic, such as
  `(ref.i31 (i32.add (i32.const 1) (i32.const 2)))`, instantiate instead of failing as
  invalid compiled metadata. ([#771](https://github.com/wago-org/wago/pull/771))
- Custom plugin inputs can no longer be read through scalar or checked-memory accessors,
  which previously aliased them with spill slot zero.
  ([#797](https://github.com/wago-org/wago/pull/797))

## [v0.1.0-beta.11] - 2026-09-29

### Added

- Restore `WasmFunc.OpenSession` and `PreparedSession` for repeated calls under one
  instance reservation; `Close` releases the reservation.
- Add configurable export caching through `InvokeCacheSlots` and `WithInvokeCacheSlots`,
  with four inline slots by default and 1–255 entries.
- Add required WebAssembly 1.0 conformance checks and forced-SSE2 coverage.

### Changed

- Make SSE2 the AMD64 baseline with fallback lowering for core SIMD and supported
  relaxed SIMD; use newer CPU extensions when available.
- Speed ordinary and prepared invocations without requiring caller-owned sessions, and
  reduce ordinary Go host-callback overhead.
- Reduce quadratic work in recursive type analysis, compiler metadata, operand stacks,
  GC type mappings, and runtime lookups.
- **Breaking:** compiled artifacts use format v4. Recompile persisted artifacts from
  earlier releases before loading them.
- Make CI qualification profile-aware and remove duplicate corpus runs.

### Fixed

- Correct ARM64 overlapping call arguments, bulk-helper register clobbers, stale indexed
  addresses after signed loads, and wide fixed-array GC roots.
- Correct GC transient-root enumeration, reference-array initialization, promotion
  views, collection accounting, failed-restart recovery, and zero-value collector
  shutdown.
- Reject invalid GC headers, overlapping fields, invalid alignment, and incompatible
  inherited storage layouts.
- Validate malformed programmatic Wasm types, function bodies, instruction kinds,
  immutable-global writes, Memory64 offsets, and wrapped 32-bit indexes.
- Track CPU requirements for emitted plugin lowerings, so unused plugin imports no
  longer require AVX. Gate scalar bit-count instructions on CPU support.
- Correct canary artifact discovery and Windows publishing-cache setup.

## [v0.1.0-beta.10] - 2026-09-23

### Added

- The shared correctness and benchmark corpus now runs 40 full application executables
  end to end, with pinned provenance and exact independent output oracles for
  application-scale JavaScript, SQL, compression, CLI, compiler, language-runtime,
  cryptography, and FPGA-tool workloads.

### Changed

- Host imports instantiate faster and allocate less by sharing immutable dispatch thunks
  across instances and deferring instance-specific state until it is needed.
- Dynamic `memory.copy` and `memory.fill`, `memory.grow(0)`, and large funcref
  `table.fill` operations are faster across AMD64 and ARM64.
- Compiler value-type reuse, scalar host-call handling, wide GC dirty-range scanning,
  and Linux idle-memory reclamation reduce runtime and compilation overhead.

### Fixed

- Correct ARM64 miscompiles involving mixed float-call arguments, large `v128` stack
  displacements, pinned and deferred loads, `memory.fill` scratch registers, and
  guard-mode indexed-base reuse. These fixes address PDFium rendering failures and
  bulk-memory corruption under register pressure.
- Ensure reused linear memory is zeroed on Darwin when `MADV_ZERO` is unavailable.
- Initialize cached float constants only at function entry so branch-specific lowering
  cannot reuse an uninitialized register on AMD64 or ARM64.
- Correct nested shifts and rotates, non-null global writes, shared-memory imports,
  reference widening and nullability, multi-memory encoding, AVX-512 high-register
  forms, large-offset bounds checks, GC maps and safepoints, passive-element overflow,
  and aliased code-buffer growth.
- Correct explicit Core-profile precedence, `_start` signature validation, watch-flag
  parsing, result formatting with mismatched metadata, plugin source selection,
  semantic-version wildcard validation, and full uninstall cleanup for active XDG
  directories and Fish completions.

## [v0.1.0-beta.9] - 2026-09-16

### Added

- `wago run` accepts repeated `--invoke` flags and calls the selected exports in order
  on one module instance. Each call consumes only the positional values required by its
  signature, including typed values such as `7:i64` and `3.5:f64`.
  ([#605](https://github.com/wago-org/wago/pull/605))

- Core 3 is enabled by default, including compact imports and broader WebAssembly GC
  validation and execution coverage.
- Portable typed host callbacks support ordinary scalar Go functions, `func(HostCall)`,
  and caller-aware `func(Caller, HostCall)` callbacks under both Go and TinyGo.

### Changed

- The README's one-shot Go installer command now runs the installer from `@main`, and
  the redundant persistent-installer example has been removed.
  ([#605](https://github.com/wago-org/wago/pull/605))

- **Breaking:** host imports now use `NewImports` and flat `HostFunc(module, name, fn)`
  registration. The namespace builder and the old `Func`, `CallerFunc`, raw-slot
  callback, and signature-specific registration APIs have been removed.
- **Breaking:** `WasmFunc` replaces `PrepareFunction` and all signature-specific
  prepared handles. `WasmFunc.Invoke(args...)` is the only resolved-function invocation
  method; numbered `Invoke0` through `Invoke4` methods have been removed.
- **Breaking:** public prepared sessions and `OpenSession` have been removed. Each
  invocation now performs its own lifecycle admission.
- **Breaking:** by-name execution is `Invoke` or `InvokeContext`; tagged-value calls use
  `InvokeValues` instead of `Call`.
- Host/Wasm dispatch, typed callbacks, Railshot-generated execution, branch hints,
  import-range lookup, and prepared-artifact admission are faster while retaining the
  existing ownership and resource checks.

### Fixed

- Preserve WebAssembly GC roots and metadata across fixed-array results, prepared calls,
  table reads, shared domains, cancellation, and callbacks.
- Bound synchronous host callback re-entry to four active calls per invocation chain,
  including cross-instance re-entry, without retaining an execution reservation between
  calls.
- Harden instance shutdown, cancelable admission, retained memory callbacks,
  compile-lifetime observers, prepared-artifact limits, and host re-entry close
  interruption.
- Correct Core 3 validation and name decoding, ARM64 register allocation and branch
  patching, Windows cleanup behavior, semantic-version prerelease ranges, build output
  aliasing, and installer fallback behavior.

## [v0.1.0-beta.8] - 2026-09-10

### Added

- Add Beta and commit-addressed Canary release channels, with qualified release
  artifacts and a Go-native installation route.
- Add explicit native-artifact trust controls, runtime resource policies, callback
  re-entry, and broader Core 3 and WebAssembly GC support.
- Add end-to-end semantic corpus coverage and differential engine-state testing.

### Changed

- Replace the Make-based developer interface with Just.
- Reduce compiler memory use, compile latency, host-call overhead, and prepared
  invocation latency across AMD64 and ARM64.
- Simplify release notes and installer/channel discovery behavior.

### Fixed

- Fix JIT, invocation, validation, settings, TinyGo trampoline, signal,
  reference-lifetime, and resource-limit correctness issues found by the September
  audit.
- Fix AMD64 corpus miscompiles, ARM64 indexed bulk-memory bounds checks, and incomplete
  typed-select immediate decoding.
- Fix Beta discovery and retracted legacy tagged Canary module versions.

## Merged pull request index

This index includes all 717 merged PRs as of 2026-10-15 17:46 UTC, including
documentation, tests, CI, dependencies, reverts, and merges into feature branches.
The main-branch snapshot is
[`b36139a`](https://github.com/wago-org/wago/commit/b36139abe0f78d5cbc5c92b7fdfcdd9768a907dc);
the latest merge in this snapshot is [#964](https://github.com/wago-org/wago/pull/964).

Release groups use reachable merge commits or explicit PR references in tagged
commit history. Earlier rewritten commits are matched by their explicit PR
references, not by merge dates. Inclusion records merged work, including changes
later reverted; it does not mean every change remains active. PRs whose release
association is unconfirmed are listed separately.

### After v0.1.0-beta.12 (not yet tagged) — 45 PRs

- [#807](https://github.com/wago-org/wago/pull/807),
  [#835](https://github.com/wago-org/wago/pull/835),
  [#837](https://github.com/wago-org/wago/pull/837),
  [#860](https://github.com/wago-org/wago/pull/860),
  [#861](https://github.com/wago-org/wago/pull/861),
  [#862](https://github.com/wago-org/wago/pull/862),
  [#866](https://github.com/wago-org/wago/pull/866),
  [#868](https://github.com/wago-org/wago/pull/868)
- [#872](https://github.com/wago-org/wago/pull/872),
  [#874](https://github.com/wago-org/wago/pull/874),
  [#875](https://github.com/wago-org/wago/pull/875),
  [#876](https://github.com/wago-org/wago/pull/876),
  [#878](https://github.com/wago-org/wago/pull/878),
  [#883](https://github.com/wago-org/wago/pull/883),
  [#884](https://github.com/wago-org/wago/pull/884),
  [#885](https://github.com/wago-org/wago/pull/885)
- [#886](https://github.com/wago-org/wago/pull/886),
  [#887](https://github.com/wago-org/wago/pull/887),
  [#888](https://github.com/wago-org/wago/pull/888),
  [#889](https://github.com/wago-org/wago/pull/889),
  [#890](https://github.com/wago-org/wago/pull/890),
  [#893](https://github.com/wago-org/wago/pull/893),
  [#894](https://github.com/wago-org/wago/pull/894),
  [#896](https://github.com/wago-org/wago/pull/896)
- [#897](https://github.com/wago-org/wago/pull/897),
  [#898](https://github.com/wago-org/wago/pull/898),
  [#899](https://github.com/wago-org/wago/pull/899),
  [#900](https://github.com/wago-org/wago/pull/900),
  [#902](https://github.com/wago-org/wago/pull/902),
  [#903](https://github.com/wago-org/wago/pull/903),
  [#904](https://github.com/wago-org/wago/pull/904),
  [#907](https://github.com/wago-org/wago/pull/907)
- [#908](https://github.com/wago-org/wago/pull/908),
  [#912](https://github.com/wago-org/wago/pull/912),
  [#938](https://github.com/wago-org/wago/pull/938),
  [#939](https://github.com/wago-org/wago/pull/939),
  [#940](https://github.com/wago-org/wago/pull/940),
  [#941](https://github.com/wago-org/wago/pull/941),
  [#943](https://github.com/wago-org/wago/pull/943),
  [#944](https://github.com/wago-org/wago/pull/944)
- [#946](https://github.com/wago-org/wago/pull/946),
  [#947](https://github.com/wago-org/wago/pull/947),
  [#949](https://github.com/wago-org/wago/pull/949),
  [#963](https://github.com/wago-org/wago/pull/963),
  [#964](https://github.com/wago-org/wago/pull/964)

### v0.1.0-beta.12 — 118 PRs

- [#528](https://github.com/wago-org/wago/pull/528),
  [#608](https://github.com/wago-org/wago/pull/608),
  [#635](https://github.com/wago-org/wago/pull/635),
  [#641](https://github.com/wago-org/wago/pull/641),
  [#709](https://github.com/wago-org/wago/pull/709),
  [#713](https://github.com/wago-org/wago/pull/713),
  [#718](https://github.com/wago-org/wago/pull/718),
  [#720](https://github.com/wago-org/wago/pull/720)
- [#721](https://github.com/wago-org/wago/pull/721),
  [#722](https://github.com/wago-org/wago/pull/722),
  [#723](https://github.com/wago-org/wago/pull/723),
  [#724](https://github.com/wago-org/wago/pull/724),
  [#725](https://github.com/wago-org/wago/pull/725),
  [#726](https://github.com/wago-org/wago/pull/726),
  [#727](https://github.com/wago-org/wago/pull/727),
  [#728](https://github.com/wago-org/wago/pull/728)
- [#729](https://github.com/wago-org/wago/pull/729),
  [#730](https://github.com/wago-org/wago/pull/730),
  [#731](https://github.com/wago-org/wago/pull/731),
  [#732](https://github.com/wago-org/wago/pull/732),
  [#733](https://github.com/wago-org/wago/pull/733),
  [#734](https://github.com/wago-org/wago/pull/734),
  [#735](https://github.com/wago-org/wago/pull/735),
  [#736](https://github.com/wago-org/wago/pull/736)
- [#737](https://github.com/wago-org/wago/pull/737),
  [#738](https://github.com/wago-org/wago/pull/738),
  [#739](https://github.com/wago-org/wago/pull/739),
  [#740](https://github.com/wago-org/wago/pull/740),
  [#741](https://github.com/wago-org/wago/pull/741),
  [#742](https://github.com/wago-org/wago/pull/742),
  [#743](https://github.com/wago-org/wago/pull/743),
  [#744](https://github.com/wago-org/wago/pull/744)
- [#745](https://github.com/wago-org/wago/pull/745),
  [#746](https://github.com/wago-org/wago/pull/746),
  [#747](https://github.com/wago-org/wago/pull/747),
  [#748](https://github.com/wago-org/wago/pull/748),
  [#749](https://github.com/wago-org/wago/pull/749),
  [#750](https://github.com/wago-org/wago/pull/750),
  [#751](https://github.com/wago-org/wago/pull/751),
  [#752](https://github.com/wago-org/wago/pull/752)
- [#753](https://github.com/wago-org/wago/pull/753),
  [#754](https://github.com/wago-org/wago/pull/754),
  [#755](https://github.com/wago-org/wago/pull/755),
  [#756](https://github.com/wago-org/wago/pull/756),
  [#757](https://github.com/wago-org/wago/pull/757),
  [#758](https://github.com/wago-org/wago/pull/758),
  [#759](https://github.com/wago-org/wago/pull/759),
  [#760](https://github.com/wago-org/wago/pull/760)
- [#761](https://github.com/wago-org/wago/pull/761),
  [#762](https://github.com/wago-org/wago/pull/762),
  [#763](https://github.com/wago-org/wago/pull/763),
  [#764](https://github.com/wago-org/wago/pull/764),
  [#765](https://github.com/wago-org/wago/pull/765),
  [#766](https://github.com/wago-org/wago/pull/766),
  [#767](https://github.com/wago-org/wago/pull/767),
  [#768](https://github.com/wago-org/wago/pull/768)
- [#769](https://github.com/wago-org/wago/pull/769),
  [#770](https://github.com/wago-org/wago/pull/770),
  [#771](https://github.com/wago-org/wago/pull/771),
  [#772](https://github.com/wago-org/wago/pull/772),
  [#773](https://github.com/wago-org/wago/pull/773),
  [#774](https://github.com/wago-org/wago/pull/774),
  [#775](https://github.com/wago-org/wago/pull/775),
  [#776](https://github.com/wago-org/wago/pull/776)
- [#777](https://github.com/wago-org/wago/pull/777),
  [#778](https://github.com/wago-org/wago/pull/778),
  [#779](https://github.com/wago-org/wago/pull/779),
  [#780](https://github.com/wago-org/wago/pull/780),
  [#781](https://github.com/wago-org/wago/pull/781),
  [#783](https://github.com/wago-org/wago/pull/783),
  [#785](https://github.com/wago-org/wago/pull/785),
  [#786](https://github.com/wago-org/wago/pull/786)
- [#787](https://github.com/wago-org/wago/pull/787),
  [#788](https://github.com/wago-org/wago/pull/788),
  [#789](https://github.com/wago-org/wago/pull/789),
  [#790](https://github.com/wago-org/wago/pull/790),
  [#791](https://github.com/wago-org/wago/pull/791),
  [#792](https://github.com/wago-org/wago/pull/792),
  [#793](https://github.com/wago-org/wago/pull/793),
  [#794](https://github.com/wago-org/wago/pull/794)
- [#795](https://github.com/wago-org/wago/pull/795),
  [#796](https://github.com/wago-org/wago/pull/796),
  [#797](https://github.com/wago-org/wago/pull/797),
  [#798](https://github.com/wago-org/wago/pull/798),
  [#799](https://github.com/wago-org/wago/pull/799),
  [#800](https://github.com/wago-org/wago/pull/800),
  [#802](https://github.com/wago-org/wago/pull/802),
  [#803](https://github.com/wago-org/wago/pull/803)
- [#804](https://github.com/wago-org/wago/pull/804),
  [#805](https://github.com/wago-org/wago/pull/805),
  [#806](https://github.com/wago-org/wago/pull/806),
  [#808](https://github.com/wago-org/wago/pull/808),
  [#820](https://github.com/wago-org/wago/pull/820),
  [#834](https://github.com/wago-org/wago/pull/834),
  [#836](https://github.com/wago-org/wago/pull/836),
  [#838](https://github.com/wago-org/wago/pull/838)
- [#839](https://github.com/wago-org/wago/pull/839),
  [#840](https://github.com/wago-org/wago/pull/840),
  [#841](https://github.com/wago-org/wago/pull/841),
  [#842](https://github.com/wago-org/wago/pull/842),
  [#843](https://github.com/wago-org/wago/pull/843),
  [#844](https://github.com/wago-org/wago/pull/844),
  [#848](https://github.com/wago-org/wago/pull/848),
  [#850](https://github.com/wago-org/wago/pull/850)
- [#851](https://github.com/wago-org/wago/pull/851),
  [#852](https://github.com/wago-org/wago/pull/852),
  [#853](https://github.com/wago-org/wago/pull/853),
  [#854](https://github.com/wago-org/wago/pull/854),
  [#855](https://github.com/wago-org/wago/pull/855),
  [#857](https://github.com/wago-org/wago/pull/857),
  [#858](https://github.com/wago-org/wago/pull/858),
  [#859](https://github.com/wago-org/wago/pull/859)
- [#863](https://github.com/wago-org/wago/pull/863),
  [#864](https://github.com/wago-org/wago/pull/864),
  [#865](https://github.com/wago-org/wago/pull/865),
  [#867](https://github.com/wago-org/wago/pull/867),
  [#869](https://github.com/wago-org/wago/pull/869),
  [#873](https://github.com/wago-org/wago/pull/873)

### v0.1.0-beta.11 — 34 PRs

- [#678](https://github.com/wago-org/wago/pull/678),
  [#679](https://github.com/wago-org/wago/pull/679),
  [#680](https://github.com/wago-org/wago/pull/680),
  [#681](https://github.com/wago-org/wago/pull/681),
  [#682](https://github.com/wago-org/wago/pull/682),
  [#683](https://github.com/wago-org/wago/pull/683),
  [#684](https://github.com/wago-org/wago/pull/684),
  [#685](https://github.com/wago-org/wago/pull/685)
- [#686](https://github.com/wago-org/wago/pull/686),
  [#687](https://github.com/wago-org/wago/pull/687),
  [#688](https://github.com/wago-org/wago/pull/688),
  [#689](https://github.com/wago-org/wago/pull/689),
  [#690](https://github.com/wago-org/wago/pull/690),
  [#691](https://github.com/wago-org/wago/pull/691),
  [#692](https://github.com/wago-org/wago/pull/692),
  [#693](https://github.com/wago-org/wago/pull/693)
- [#695](https://github.com/wago-org/wago/pull/695),
  [#696](https://github.com/wago-org/wago/pull/696),
  [#697](https://github.com/wago-org/wago/pull/697),
  [#698](https://github.com/wago-org/wago/pull/698),
  [#699](https://github.com/wago-org/wago/pull/699),
  [#702](https://github.com/wago-org/wago/pull/702),
  [#704](https://github.com/wago-org/wago/pull/704),
  [#705](https://github.com/wago-org/wago/pull/705)
- [#706](https://github.com/wago-org/wago/pull/706),
  [#707](https://github.com/wago-org/wago/pull/707),
  [#708](https://github.com/wago-org/wago/pull/708),
  [#710](https://github.com/wago-org/wago/pull/710),
  [#711](https://github.com/wago-org/wago/pull/711),
  [#712](https://github.com/wago-org/wago/pull/712),
  [#714](https://github.com/wago-org/wago/pull/714),
  [#715](https://github.com/wago-org/wago/pull/715)
- [#716](https://github.com/wago-org/wago/pull/716),
  [#717](https://github.com/wago-org/wago/pull/717)

### v0.1.0-beta.10 — 23 PRs

- [#619](https://github.com/wago-org/wago/pull/619),
  [#621](https://github.com/wago-org/wago/pull/621),
  [#637](https://github.com/wago-org/wago/pull/637),
  [#640](https://github.com/wago-org/wago/pull/640),
  [#642](https://github.com/wago-org/wago/pull/642),
  [#655](https://github.com/wago-org/wago/pull/655),
  [#658](https://github.com/wago-org/wago/pull/658),
  [#659](https://github.com/wago-org/wago/pull/659)
- [#661](https://github.com/wago-org/wago/pull/661),
  [#662](https://github.com/wago-org/wago/pull/662),
  [#663](https://github.com/wago-org/wago/pull/663),
  [#666](https://github.com/wago-org/wago/pull/666),
  [#667](https://github.com/wago-org/wago/pull/667),
  [#668](https://github.com/wago-org/wago/pull/668),
  [#670](https://github.com/wago-org/wago/pull/670),
  [#671](https://github.com/wago-org/wago/pull/671)
- [#672](https://github.com/wago-org/wago/pull/672),
  [#673](https://github.com/wago-org/wago/pull/673),
  [#674](https://github.com/wago-org/wago/pull/674),
  [#676](https://github.com/wago-org/wago/pull/676),
  [#677](https://github.com/wago-org/wago/pull/677),
  [#701](https://github.com/wago-org/wago/pull/701),
  [#703](https://github.com/wago-org/wago/pull/703)

### v0.1.0-beta.9 — 40 PRs

- [#526](https://github.com/wago-org/wago/pull/526),
  [#604](https://github.com/wago-org/wago/pull/604),
  [#605](https://github.com/wago-org/wago/pull/605),
  [#606](https://github.com/wago-org/wago/pull/606),
  [#607](https://github.com/wago-org/wago/pull/607),
  [#614](https://github.com/wago-org/wago/pull/614),
  [#615](https://github.com/wago-org/wago/pull/615),
  [#616](https://github.com/wago-org/wago/pull/616)
- [#617](https://github.com/wago-org/wago/pull/617),
  [#618](https://github.com/wago-org/wago/pull/618),
  [#620](https://github.com/wago-org/wago/pull/620),
  [#622](https://github.com/wago-org/wago/pull/622),
  [#623](https://github.com/wago-org/wago/pull/623),
  [#624](https://github.com/wago-org/wago/pull/624),
  [#625](https://github.com/wago-org/wago/pull/625),
  [#626](https://github.com/wago-org/wago/pull/626)
- [#627](https://github.com/wago-org/wago/pull/627),
  [#628](https://github.com/wago-org/wago/pull/628),
  [#629](https://github.com/wago-org/wago/pull/629),
  [#630](https://github.com/wago-org/wago/pull/630),
  [#631](https://github.com/wago-org/wago/pull/631),
  [#632](https://github.com/wago-org/wago/pull/632),
  [#633](https://github.com/wago-org/wago/pull/633),
  [#634](https://github.com/wago-org/wago/pull/634)
- [#636](https://github.com/wago-org/wago/pull/636),
  [#639](https://github.com/wago-org/wago/pull/639),
  [#643](https://github.com/wago-org/wago/pull/643),
  [#644](https://github.com/wago-org/wago/pull/644),
  [#645](https://github.com/wago-org/wago/pull/645),
  [#646](https://github.com/wago-org/wago/pull/646),
  [#647](https://github.com/wago-org/wago/pull/647),
  [#648](https://github.com/wago-org/wago/pull/648)
- [#649](https://github.com/wago-org/wago/pull/649),
  [#650](https://github.com/wago-org/wago/pull/650),
  [#651](https://github.com/wago-org/wago/pull/651),
  [#652](https://github.com/wago-org/wago/pull/652),
  [#653](https://github.com/wago-org/wago/pull/653),
  [#654](https://github.com/wago-org/wago/pull/654),
  [#656](https://github.com/wago-org/wago/pull/656),
  [#657](https://github.com/wago-org/wago/pull/657)

### Through v0.1.0-beta.8 (historical) — 449 PRs

- [#3](https://github.com/wago-org/wago/pull/3),
  [#4](https://github.com/wago-org/wago/pull/4),
  [#5](https://github.com/wago-org/wago/pull/5),
  [#6](https://github.com/wago-org/wago/pull/6),
  [#7](https://github.com/wago-org/wago/pull/7),
  [#8](https://github.com/wago-org/wago/pull/8),
  [#9](https://github.com/wago-org/wago/pull/9),
  [#10](https://github.com/wago-org/wago/pull/10)
- [#11](https://github.com/wago-org/wago/pull/11),
  [#12](https://github.com/wago-org/wago/pull/12),
  [#13](https://github.com/wago-org/wago/pull/13),
  [#14](https://github.com/wago-org/wago/pull/14),
  [#15](https://github.com/wago-org/wago/pull/15),
  [#16](https://github.com/wago-org/wago/pull/16),
  [#17](https://github.com/wago-org/wago/pull/17),
  [#18](https://github.com/wago-org/wago/pull/18)
- [#19](https://github.com/wago-org/wago/pull/19),
  [#20](https://github.com/wago-org/wago/pull/20),
  [#21](https://github.com/wago-org/wago/pull/21),
  [#22](https://github.com/wago-org/wago/pull/22),
  [#23](https://github.com/wago-org/wago/pull/23),
  [#24](https://github.com/wago-org/wago/pull/24),
  [#25](https://github.com/wago-org/wago/pull/25),
  [#26](https://github.com/wago-org/wago/pull/26)
- [#27](https://github.com/wago-org/wago/pull/27),
  [#28](https://github.com/wago-org/wago/pull/28),
  [#30](https://github.com/wago-org/wago/pull/30),
  [#33](https://github.com/wago-org/wago/pull/33),
  [#34](https://github.com/wago-org/wago/pull/34),
  [#35](https://github.com/wago-org/wago/pull/35),
  [#36](https://github.com/wago-org/wago/pull/36),
  [#37](https://github.com/wago-org/wago/pull/37)
- [#38](https://github.com/wago-org/wago/pull/38),
  [#39](https://github.com/wago-org/wago/pull/39),
  [#40](https://github.com/wago-org/wago/pull/40),
  [#41](https://github.com/wago-org/wago/pull/41),
  [#42](https://github.com/wago-org/wago/pull/42),
  [#43](https://github.com/wago-org/wago/pull/43),
  [#44](https://github.com/wago-org/wago/pull/44),
  [#45](https://github.com/wago-org/wago/pull/45)
- [#46](https://github.com/wago-org/wago/pull/46),
  [#47](https://github.com/wago-org/wago/pull/47),
  [#48](https://github.com/wago-org/wago/pull/48),
  [#49](https://github.com/wago-org/wago/pull/49),
  [#50](https://github.com/wago-org/wago/pull/50),
  [#52](https://github.com/wago-org/wago/pull/52),
  [#53](https://github.com/wago-org/wago/pull/53),
  [#54](https://github.com/wago-org/wago/pull/54)
- [#56](https://github.com/wago-org/wago/pull/56),
  [#57](https://github.com/wago-org/wago/pull/57),
  [#58](https://github.com/wago-org/wago/pull/58),
  [#60](https://github.com/wago-org/wago/pull/60),
  [#61](https://github.com/wago-org/wago/pull/61),
  [#62](https://github.com/wago-org/wago/pull/62),
  [#63](https://github.com/wago-org/wago/pull/63),
  [#64](https://github.com/wago-org/wago/pull/64)
- [#65](https://github.com/wago-org/wago/pull/65),
  [#66](https://github.com/wago-org/wago/pull/66),
  [#67](https://github.com/wago-org/wago/pull/67),
  [#68](https://github.com/wago-org/wago/pull/68),
  [#69](https://github.com/wago-org/wago/pull/69),
  [#70](https://github.com/wago-org/wago/pull/70),
  [#71](https://github.com/wago-org/wago/pull/71),
  [#72](https://github.com/wago-org/wago/pull/72)
- [#73](https://github.com/wago-org/wago/pull/73),
  [#74](https://github.com/wago-org/wago/pull/74),
  [#75](https://github.com/wago-org/wago/pull/75),
  [#76](https://github.com/wago-org/wago/pull/76),
  [#77](https://github.com/wago-org/wago/pull/77),
  [#78](https://github.com/wago-org/wago/pull/78),
  [#79](https://github.com/wago-org/wago/pull/79),
  [#80](https://github.com/wago-org/wago/pull/80)
- [#82](https://github.com/wago-org/wago/pull/82),
  [#83](https://github.com/wago-org/wago/pull/83),
  [#84](https://github.com/wago-org/wago/pull/84),
  [#85](https://github.com/wago-org/wago/pull/85),
  [#86](https://github.com/wago-org/wago/pull/86),
  [#87](https://github.com/wago-org/wago/pull/87),
  [#88](https://github.com/wago-org/wago/pull/88),
  [#90](https://github.com/wago-org/wago/pull/90)
- [#91](https://github.com/wago-org/wago/pull/91),
  [#92](https://github.com/wago-org/wago/pull/92),
  [#93](https://github.com/wago-org/wago/pull/93),
  [#94](https://github.com/wago-org/wago/pull/94),
  [#95](https://github.com/wago-org/wago/pull/95),
  [#96](https://github.com/wago-org/wago/pull/96),
  [#97](https://github.com/wago-org/wago/pull/97),
  [#98](https://github.com/wago-org/wago/pull/98)
- [#99](https://github.com/wago-org/wago/pull/99),
  [#100](https://github.com/wago-org/wago/pull/100),
  [#101](https://github.com/wago-org/wago/pull/101),
  [#102](https://github.com/wago-org/wago/pull/102),
  [#103](https://github.com/wago-org/wago/pull/103),
  [#105](https://github.com/wago-org/wago/pull/105),
  [#106](https://github.com/wago-org/wago/pull/106),
  [#107](https://github.com/wago-org/wago/pull/107)
- [#108](https://github.com/wago-org/wago/pull/108),
  [#110](https://github.com/wago-org/wago/pull/110),
  [#111](https://github.com/wago-org/wago/pull/111),
  [#112](https://github.com/wago-org/wago/pull/112),
  [#113](https://github.com/wago-org/wago/pull/113),
  [#114](https://github.com/wago-org/wago/pull/114),
  [#115](https://github.com/wago-org/wago/pull/115),
  [#116](https://github.com/wago-org/wago/pull/116)
- [#117](https://github.com/wago-org/wago/pull/117),
  [#118](https://github.com/wago-org/wago/pull/118),
  [#119](https://github.com/wago-org/wago/pull/119),
  [#120](https://github.com/wago-org/wago/pull/120),
  [#121](https://github.com/wago-org/wago/pull/121),
  [#122](https://github.com/wago-org/wago/pull/122),
  [#124](https://github.com/wago-org/wago/pull/124),
  [#125](https://github.com/wago-org/wago/pull/125)
- [#126](https://github.com/wago-org/wago/pull/126),
  [#127](https://github.com/wago-org/wago/pull/127),
  [#128](https://github.com/wago-org/wago/pull/128),
  [#129](https://github.com/wago-org/wago/pull/129),
  [#130](https://github.com/wago-org/wago/pull/130),
  [#131](https://github.com/wago-org/wago/pull/131),
  [#132](https://github.com/wago-org/wago/pull/132),
  [#133](https://github.com/wago-org/wago/pull/133)
- [#134](https://github.com/wago-org/wago/pull/134),
  [#135](https://github.com/wago-org/wago/pull/135),
  [#136](https://github.com/wago-org/wago/pull/136),
  [#137](https://github.com/wago-org/wago/pull/137),
  [#138](https://github.com/wago-org/wago/pull/138),
  [#139](https://github.com/wago-org/wago/pull/139),
  [#140](https://github.com/wago-org/wago/pull/140),
  [#141](https://github.com/wago-org/wago/pull/141)
- [#142](https://github.com/wago-org/wago/pull/142),
  [#143](https://github.com/wago-org/wago/pull/143),
  [#144](https://github.com/wago-org/wago/pull/144),
  [#145](https://github.com/wago-org/wago/pull/145),
  [#146](https://github.com/wago-org/wago/pull/146),
  [#147](https://github.com/wago-org/wago/pull/147),
  [#148](https://github.com/wago-org/wago/pull/148),
  [#149](https://github.com/wago-org/wago/pull/149)
- [#150](https://github.com/wago-org/wago/pull/150),
  [#151](https://github.com/wago-org/wago/pull/151),
  [#152](https://github.com/wago-org/wago/pull/152),
  [#153](https://github.com/wago-org/wago/pull/153),
  [#154](https://github.com/wago-org/wago/pull/154),
  [#155](https://github.com/wago-org/wago/pull/155),
  [#156](https://github.com/wago-org/wago/pull/156),
  [#157](https://github.com/wago-org/wago/pull/157)
- [#158](https://github.com/wago-org/wago/pull/158),
  [#160](https://github.com/wago-org/wago/pull/160),
  [#161](https://github.com/wago-org/wago/pull/161),
  [#162](https://github.com/wago-org/wago/pull/162),
  [#163](https://github.com/wago-org/wago/pull/163),
  [#164](https://github.com/wago-org/wago/pull/164),
  [#165](https://github.com/wago-org/wago/pull/165),
  [#166](https://github.com/wago-org/wago/pull/166)
- [#167](https://github.com/wago-org/wago/pull/167),
  [#168](https://github.com/wago-org/wago/pull/168),
  [#169](https://github.com/wago-org/wago/pull/169),
  [#170](https://github.com/wago-org/wago/pull/170),
  [#171](https://github.com/wago-org/wago/pull/171),
  [#172](https://github.com/wago-org/wago/pull/172),
  [#173](https://github.com/wago-org/wago/pull/173),
  [#174](https://github.com/wago-org/wago/pull/174)
- [#175](https://github.com/wago-org/wago/pull/175),
  [#177](https://github.com/wago-org/wago/pull/177),
  [#178](https://github.com/wago-org/wago/pull/178),
  [#179](https://github.com/wago-org/wago/pull/179),
  [#180](https://github.com/wago-org/wago/pull/180),
  [#181](https://github.com/wago-org/wago/pull/181),
  [#182](https://github.com/wago-org/wago/pull/182),
  [#183](https://github.com/wago-org/wago/pull/183)
- [#184](https://github.com/wago-org/wago/pull/184),
  [#185](https://github.com/wago-org/wago/pull/185),
  [#186](https://github.com/wago-org/wago/pull/186),
  [#187](https://github.com/wago-org/wago/pull/187),
  [#188](https://github.com/wago-org/wago/pull/188),
  [#189](https://github.com/wago-org/wago/pull/189),
  [#190](https://github.com/wago-org/wago/pull/190),
  [#191](https://github.com/wago-org/wago/pull/191)
- [#192](https://github.com/wago-org/wago/pull/192),
  [#193](https://github.com/wago-org/wago/pull/193),
  [#194](https://github.com/wago-org/wago/pull/194),
  [#195](https://github.com/wago-org/wago/pull/195),
  [#196](https://github.com/wago-org/wago/pull/196),
  [#197](https://github.com/wago-org/wago/pull/197),
  [#198](https://github.com/wago-org/wago/pull/198),
  [#199](https://github.com/wago-org/wago/pull/199)
- [#200](https://github.com/wago-org/wago/pull/200),
  [#201](https://github.com/wago-org/wago/pull/201),
  [#202](https://github.com/wago-org/wago/pull/202),
  [#203](https://github.com/wago-org/wago/pull/203),
  [#204](https://github.com/wago-org/wago/pull/204),
  [#205](https://github.com/wago-org/wago/pull/205),
  [#206](https://github.com/wago-org/wago/pull/206),
  [#207](https://github.com/wago-org/wago/pull/207)
- [#208](https://github.com/wago-org/wago/pull/208),
  [#209](https://github.com/wago-org/wago/pull/209),
  [#210](https://github.com/wago-org/wago/pull/210),
  [#211](https://github.com/wago-org/wago/pull/211),
  [#213](https://github.com/wago-org/wago/pull/213),
  [#214](https://github.com/wago-org/wago/pull/214),
  [#215](https://github.com/wago-org/wago/pull/215),
  [#216](https://github.com/wago-org/wago/pull/216)
- [#217](https://github.com/wago-org/wago/pull/217),
  [#218](https://github.com/wago-org/wago/pull/218),
  [#219](https://github.com/wago-org/wago/pull/219),
  [#221](https://github.com/wago-org/wago/pull/221),
  [#222](https://github.com/wago-org/wago/pull/222),
  [#223](https://github.com/wago-org/wago/pull/223),
  [#224](https://github.com/wago-org/wago/pull/224),
  [#225](https://github.com/wago-org/wago/pull/225)
- [#227](https://github.com/wago-org/wago/pull/227),
  [#228](https://github.com/wago-org/wago/pull/228),
  [#229](https://github.com/wago-org/wago/pull/229),
  [#230](https://github.com/wago-org/wago/pull/230),
  [#231](https://github.com/wago-org/wago/pull/231),
  [#232](https://github.com/wago-org/wago/pull/232),
  [#233](https://github.com/wago-org/wago/pull/233),
  [#235](https://github.com/wago-org/wago/pull/235)
- [#236](https://github.com/wago-org/wago/pull/236),
  [#237](https://github.com/wago-org/wago/pull/237),
  [#238](https://github.com/wago-org/wago/pull/238),
  [#239](https://github.com/wago-org/wago/pull/239),
  [#241](https://github.com/wago-org/wago/pull/241),
  [#242](https://github.com/wago-org/wago/pull/242),
  [#243](https://github.com/wago-org/wago/pull/243),
  [#244](https://github.com/wago-org/wago/pull/244)
- [#245](https://github.com/wago-org/wago/pull/245),
  [#246](https://github.com/wago-org/wago/pull/246),
  [#247](https://github.com/wago-org/wago/pull/247),
  [#248](https://github.com/wago-org/wago/pull/248),
  [#249](https://github.com/wago-org/wago/pull/249),
  [#250](https://github.com/wago-org/wago/pull/250),
  [#251](https://github.com/wago-org/wago/pull/251),
  [#252](https://github.com/wago-org/wago/pull/252)
- [#253](https://github.com/wago-org/wago/pull/253),
  [#254](https://github.com/wago-org/wago/pull/254),
  [#255](https://github.com/wago-org/wago/pull/255),
  [#256](https://github.com/wago-org/wago/pull/256),
  [#257](https://github.com/wago-org/wago/pull/257),
  [#258](https://github.com/wago-org/wago/pull/258),
  [#259](https://github.com/wago-org/wago/pull/259),
  [#260](https://github.com/wago-org/wago/pull/260)
- [#261](https://github.com/wago-org/wago/pull/261),
  [#262](https://github.com/wago-org/wago/pull/262),
  [#263](https://github.com/wago-org/wago/pull/263),
  [#264](https://github.com/wago-org/wago/pull/264),
  [#266](https://github.com/wago-org/wago/pull/266),
  [#267](https://github.com/wago-org/wago/pull/267),
  [#268](https://github.com/wago-org/wago/pull/268),
  [#269](https://github.com/wago-org/wago/pull/269)
- [#270](https://github.com/wago-org/wago/pull/270),
  [#272](https://github.com/wago-org/wago/pull/272),
  [#273](https://github.com/wago-org/wago/pull/273),
  [#274](https://github.com/wago-org/wago/pull/274),
  [#275](https://github.com/wago-org/wago/pull/275),
  [#276](https://github.com/wago-org/wago/pull/276),
  [#277](https://github.com/wago-org/wago/pull/277),
  [#278](https://github.com/wago-org/wago/pull/278)
- [#279](https://github.com/wago-org/wago/pull/279),
  [#280](https://github.com/wago-org/wago/pull/280),
  [#281](https://github.com/wago-org/wago/pull/281),
  [#282](https://github.com/wago-org/wago/pull/282),
  [#283](https://github.com/wago-org/wago/pull/283),
  [#286](https://github.com/wago-org/wago/pull/286),
  [#287](https://github.com/wago-org/wago/pull/287),
  [#288](https://github.com/wago-org/wago/pull/288)
- [#289](https://github.com/wago-org/wago/pull/289),
  [#290](https://github.com/wago-org/wago/pull/290),
  [#291](https://github.com/wago-org/wago/pull/291),
  [#292](https://github.com/wago-org/wago/pull/292),
  [#293](https://github.com/wago-org/wago/pull/293),
  [#294](https://github.com/wago-org/wago/pull/294),
  [#295](https://github.com/wago-org/wago/pull/295),
  [#296](https://github.com/wago-org/wago/pull/296)
- [#298](https://github.com/wago-org/wago/pull/298),
  [#299](https://github.com/wago-org/wago/pull/299),
  [#323](https://github.com/wago-org/wago/pull/323),
  [#347](https://github.com/wago-org/wago/pull/347),
  [#349](https://github.com/wago-org/wago/pull/349),
  [#350](https://github.com/wago-org/wago/pull/350),
  [#351](https://github.com/wago-org/wago/pull/351),
  [#352](https://github.com/wago-org/wago/pull/352)
- [#354](https://github.com/wago-org/wago/pull/354),
  [#355](https://github.com/wago-org/wago/pull/355),
  [#356](https://github.com/wago-org/wago/pull/356),
  [#357](https://github.com/wago-org/wago/pull/357),
  [#358](https://github.com/wago-org/wago/pull/358),
  [#360](https://github.com/wago-org/wago/pull/360),
  [#361](https://github.com/wago-org/wago/pull/361),
  [#362](https://github.com/wago-org/wago/pull/362)
- [#363](https://github.com/wago-org/wago/pull/363),
  [#364](https://github.com/wago-org/wago/pull/364),
  [#365](https://github.com/wago-org/wago/pull/365),
  [#366](https://github.com/wago-org/wago/pull/366),
  [#367](https://github.com/wago-org/wago/pull/367),
  [#369](https://github.com/wago-org/wago/pull/369),
  [#370](https://github.com/wago-org/wago/pull/370),
  [#371](https://github.com/wago-org/wago/pull/371)
- [#372](https://github.com/wago-org/wago/pull/372),
  [#373](https://github.com/wago-org/wago/pull/373),
  [#374](https://github.com/wago-org/wago/pull/374),
  [#375](https://github.com/wago-org/wago/pull/375),
  [#378](https://github.com/wago-org/wago/pull/378),
  [#380](https://github.com/wago-org/wago/pull/380),
  [#381](https://github.com/wago-org/wago/pull/381),
  [#382](https://github.com/wago-org/wago/pull/382)
- [#383](https://github.com/wago-org/wago/pull/383),
  [#384](https://github.com/wago-org/wago/pull/384),
  [#385](https://github.com/wago-org/wago/pull/385),
  [#386](https://github.com/wago-org/wago/pull/386),
  [#387](https://github.com/wago-org/wago/pull/387),
  [#418](https://github.com/wago-org/wago/pull/418),
  [#420](https://github.com/wago-org/wago/pull/420),
  [#421](https://github.com/wago-org/wago/pull/421)
- [#422](https://github.com/wago-org/wago/pull/422),
  [#423](https://github.com/wago-org/wago/pull/423),
  [#426](https://github.com/wago-org/wago/pull/426),
  [#427](https://github.com/wago-org/wago/pull/427),
  [#428](https://github.com/wago-org/wago/pull/428),
  [#429](https://github.com/wago-org/wago/pull/429),
  [#430](https://github.com/wago-org/wago/pull/430),
  [#431](https://github.com/wago-org/wago/pull/431)
- [#432](https://github.com/wago-org/wago/pull/432),
  [#433](https://github.com/wago-org/wago/pull/433),
  [#441](https://github.com/wago-org/wago/pull/441),
  [#442](https://github.com/wago-org/wago/pull/442),
  [#443](https://github.com/wago-org/wago/pull/443),
  [#444](https://github.com/wago-org/wago/pull/444),
  [#446](https://github.com/wago-org/wago/pull/446),
  [#447](https://github.com/wago-org/wago/pull/447)
- [#448](https://github.com/wago-org/wago/pull/448),
  [#449](https://github.com/wago-org/wago/pull/449),
  [#450](https://github.com/wago-org/wago/pull/450),
  [#451](https://github.com/wago-org/wago/pull/451),
  [#452](https://github.com/wago-org/wago/pull/452),
  [#453](https://github.com/wago-org/wago/pull/453),
  [#454](https://github.com/wago-org/wago/pull/454),
  [#455](https://github.com/wago-org/wago/pull/455)
- [#456](https://github.com/wago-org/wago/pull/456),
  [#457](https://github.com/wago-org/wago/pull/457),
  [#458](https://github.com/wago-org/wago/pull/458),
  [#459](https://github.com/wago-org/wago/pull/459),
  [#460](https://github.com/wago-org/wago/pull/460),
  [#463](https://github.com/wago-org/wago/pull/463),
  [#465](https://github.com/wago-org/wago/pull/465),
  [#467](https://github.com/wago-org/wago/pull/467)
- [#468](https://github.com/wago-org/wago/pull/468),
  [#469](https://github.com/wago-org/wago/pull/469),
  [#470](https://github.com/wago-org/wago/pull/470),
  [#471](https://github.com/wago-org/wago/pull/471),
  [#472](https://github.com/wago-org/wago/pull/472),
  [#473](https://github.com/wago-org/wago/pull/473),
  [#474](https://github.com/wago-org/wago/pull/474),
  [#475](https://github.com/wago-org/wago/pull/475)
- [#476](https://github.com/wago-org/wago/pull/476),
  [#477](https://github.com/wago-org/wago/pull/477),
  [#484](https://github.com/wago-org/wago/pull/484),
  [#485](https://github.com/wago-org/wago/pull/485),
  [#486](https://github.com/wago-org/wago/pull/486),
  [#487](https://github.com/wago-org/wago/pull/487),
  [#488](https://github.com/wago-org/wago/pull/488),
  [#489](https://github.com/wago-org/wago/pull/489)
- [#491](https://github.com/wago-org/wago/pull/491),
  [#492](https://github.com/wago-org/wago/pull/492),
  [#493](https://github.com/wago-org/wago/pull/493),
  [#494](https://github.com/wago-org/wago/pull/494),
  [#495](https://github.com/wago-org/wago/pull/495),
  [#496](https://github.com/wago-org/wago/pull/496),
  [#497](https://github.com/wago-org/wago/pull/497),
  [#498](https://github.com/wago-org/wago/pull/498)
- [#500](https://github.com/wago-org/wago/pull/500),
  [#502](https://github.com/wago-org/wago/pull/502),
  [#505](https://github.com/wago-org/wago/pull/505),
  [#510](https://github.com/wago-org/wago/pull/510),
  [#512](https://github.com/wago-org/wago/pull/512),
  [#514](https://github.com/wago-org/wago/pull/514),
  [#515](https://github.com/wago-org/wago/pull/515),
  [#516](https://github.com/wago-org/wago/pull/516)
- [#518](https://github.com/wago-org/wago/pull/518),
  [#519](https://github.com/wago-org/wago/pull/519),
  [#520](https://github.com/wago-org/wago/pull/520),
  [#521](https://github.com/wago-org/wago/pull/521),
  [#523](https://github.com/wago-org/wago/pull/523),
  [#524](https://github.com/wago-org/wago/pull/524),
  [#525](https://github.com/wago-org/wago/pull/525),
  [#527](https://github.com/wago-org/wago/pull/527)
- [#529](https://github.com/wago-org/wago/pull/529),
  [#530](https://github.com/wago-org/wago/pull/530),
  [#531](https://github.com/wago-org/wago/pull/531),
  [#532](https://github.com/wago-org/wago/pull/532),
  [#533](https://github.com/wago-org/wago/pull/533),
  [#534](https://github.com/wago-org/wago/pull/534),
  [#539](https://github.com/wago-org/wago/pull/539),
  [#540](https://github.com/wago-org/wago/pull/540)
- [#541](https://github.com/wago-org/wago/pull/541),
  [#542](https://github.com/wago-org/wago/pull/542),
  [#543](https://github.com/wago-org/wago/pull/543),
  [#545](https://github.com/wago-org/wago/pull/545),
  [#546](https://github.com/wago-org/wago/pull/546),
  [#547](https://github.com/wago-org/wago/pull/547),
  [#548](https://github.com/wago-org/wago/pull/548),
  [#549](https://github.com/wago-org/wago/pull/549)
- [#550](https://github.com/wago-org/wago/pull/550),
  [#553](https://github.com/wago-org/wago/pull/553),
  [#554](https://github.com/wago-org/wago/pull/554),
  [#555](https://github.com/wago-org/wago/pull/555),
  [#556](https://github.com/wago-org/wago/pull/556),
  [#557](https://github.com/wago-org/wago/pull/557),
  [#558](https://github.com/wago-org/wago/pull/558),
  [#559](https://github.com/wago-org/wago/pull/559)
- [#560](https://github.com/wago-org/wago/pull/560),
  [#561](https://github.com/wago-org/wago/pull/561),
  [#562](https://github.com/wago-org/wago/pull/562),
  [#564](https://github.com/wago-org/wago/pull/564),
  [#565](https://github.com/wago-org/wago/pull/565),
  [#566](https://github.com/wago-org/wago/pull/566),
  [#568](https://github.com/wago-org/wago/pull/568),
  [#570](https://github.com/wago-org/wago/pull/570)
- [#571](https://github.com/wago-org/wago/pull/571),
  [#572](https://github.com/wago-org/wago/pull/572),
  [#573](https://github.com/wago-org/wago/pull/573),
  [#574](https://github.com/wago-org/wago/pull/574),
  [#575](https://github.com/wago-org/wago/pull/575),
  [#576](https://github.com/wago-org/wago/pull/576),
  [#577](https://github.com/wago-org/wago/pull/577),
  [#578](https://github.com/wago-org/wago/pull/578)
- [#579](https://github.com/wago-org/wago/pull/579),
  [#580](https://github.com/wago-org/wago/pull/580),
  [#581](https://github.com/wago-org/wago/pull/581),
  [#582](https://github.com/wago-org/wago/pull/582),
  [#583](https://github.com/wago-org/wago/pull/583),
  [#584](https://github.com/wago-org/wago/pull/584),
  [#585](https://github.com/wago-org/wago/pull/585),
  [#586](https://github.com/wago-org/wago/pull/586)
- [#587](https://github.com/wago-org/wago/pull/587),
  [#588](https://github.com/wago-org/wago/pull/588),
  [#589](https://github.com/wago-org/wago/pull/589),
  [#590](https://github.com/wago-org/wago/pull/590),
  [#591](https://github.com/wago-org/wago/pull/591),
  [#592](https://github.com/wago-org/wago/pull/592),
  [#593](https://github.com/wago-org/wago/pull/593),
  [#594](https://github.com/wago-org/wago/pull/594)
- [#595](https://github.com/wago-org/wago/pull/595),
  [#596](https://github.com/wago-org/wago/pull/596),
  [#597](https://github.com/wago-org/wago/pull/597),
  [#598](https://github.com/wago-org/wago/pull/598),
  [#599](https://github.com/wago-org/wago/pull/599),
  [#600](https://github.com/wago-org/wago/pull/600),
  [#601](https://github.com/wago-org/wago/pull/601),
  [#602](https://github.com/wago-org/wago/pull/602)
- [#603](https://github.com/wago-org/wago/pull/603)

### Other merged PRs — release association unconfirmed

These eight PRs are merged in GitHub but were not matched to the tagged main
history above. Their original target branches are recorded without claiming
release inclusion.

- [#32](https://github.com/wago-org/wago/pull/32) — target: `main`.
- [#89](https://github.com/wago-org/wago/pull/89) — target: `perf/backlog-sweep`.
- [#104](https://github.com/wago-org/wago/pull/104) — target: `main`.
- [#159](https://github.com/wago-org/wago/pull/159) — target: `main`.
- [#212](https://github.com/wago-org/wago/pull/212) — target: `main`.
- [#220](https://github.com/wago-org/wago/pull/220) — target: `main`.
- [#379](https://github.com/wago-org/wago/pull/379) — target: `main`.
- [#569](https://github.com/wago-org/wago/pull/569) — target: `jairus/railshot-r3-execution-performance`.

[Unreleased]: https://github.com/wago-org/wago/compare/v0.1.0-beta.12...HEAD
[v0.1.0-beta.13]: https://github.com/wago-org/wago/compare/v0.1.0-beta.12...46d70c6957a04c94e49330df3612fd185dd9823c
[v0.1.0-beta.12]: https://github.com/wago-org/wago/compare/v0.1.0-beta.11...v0.1.0-beta.12
[v0.1.0-beta.11]: https://github.com/wago-org/wago/compare/v0.1.0-beta.10...v0.1.0-beta.11
[v0.1.0-beta.10]: https://github.com/wago-org/wago/compare/v0.1.0-beta.9...v0.1.0-beta.10
[v0.1.0-beta.9]: https://github.com/wago-org/wago/compare/v0.1.0-beta.8...v0.1.0-beta.9
[v0.1.0-beta.8]: https://github.com/wago-org/wago/releases/tag/v0.1.0-beta.8
