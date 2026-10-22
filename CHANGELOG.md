# Changelog

Notable user-facing changes to Wago are recorded here. This changelog starts
with `v0.1.0-beta.8`; earlier prereleases remain available in the
[GitHub Releases](https://github.com/wago-org/wago/releases) history.

## [Unreleased]

### Fixed

- Mask narrow AMD64 plugin recipe results when a recipe returns its input
  directly, preserving the declared logical bit width.
- Keep the required atomic length read when an oversized memory64 shared grow
  request fails in the Go helper.

- Allow up to 32 nested synchronous host calls under standard Go so component
  adapter chains can cross more than four boundaries. TinyGo retains its
  three-call limit for fixed host stacks. Size parked GC callback and root storage
  for the outer callback plus every permitted reentry.
- Detect register clobbers during RV64 address, store, compare and paired-FP
  input preparation in checked builds.
- Preserve deferred load addresses derived from pinned mutable globals on
  AMD64, including Go-style exported function stack wrappers.

- Give imported start callbacks an invocation identity and parent context so
  authorized callbacks can initialize another instance through `InvokeFromHost`
  and observe instantiation cancellation and deadlines.

- Preserve deferred memory-load addresses when AMD64 binary operations write
  directly into a hinted register, fixing incorrect results in inlined C++
  wrappers with stack-based arguments.

### Changed

- Accept equal canonical GC type IDs before loading subtype intervals in AMD64
  native casts, tests, and branch casts.

- Execute branch casts to defined struct/array types, i31, and none in native
  AMD64 and ARM64 code. Preserve reference identity and helper fallbacks.
- Speed up AMD64 constant division/remainder pairs, scalar selects, predicate
  shifts, and contiguous SIMD byte shuffles.
  ([#895](https://github.com/wago-org/wago/pull/895))
- Use two SIMD lanes for admitted pure integer reduction loops on Linux AMD64
  with SSE4.1, with scalar execution below 16 iterations and for rejected loops.
  ([#895](https://github.com/wago-org/wago/pull/895))
- Add opt-in pure integer select branches through `WAGO_AMD64_PURE_SELECT=1`;
  keep them disabled by default because unpredictable conditions can regress.
  ([#895](https://github.com/wago-org/wago/pull/895))
- Handle shared memory growth with a statically impossible delta in native
  AMD64 and ARM64 code while retaining the required atomic length read.
- Compile bounded portable scalar plugin recipes to ARM64 instructions, preserving
  exact import identity and the portable fallback for larger or unsupported contracts.
- Adapt native struct handle reservations to batch use. Mixed struct/array code
  cancels small batches; sustained struct allocation recovers the configured
  maximum without changing the native allocation ABI.

- Add checked allocation-mix workloads and collector benchmarks for native
  struct batches canceled by array allocation helpers.
- Keep AMD64 `v128` array loads and stores in native code with checked handles,
  type identity, bounds, and object extents. Add a visible V8 timing comparison.

- Detect writes to live deferred-load address registers, cached global-cell
  pointers, and forwarded store values in the AMD64 and ARM64 compilers when
  `wago_regalloccheck` is enabled. Also guard inputs during address preparation
  in the common scalar store path and during second-input preparation in `fbin`
  and `fbinInto`.

- Combine bounded ARM64 scalar shift and mask pairs with existing bitfield
  encodings, including masks applied before an unsigned right shift.
- Reuse ARM64 variable-shift source and count registers in ordinary scalar
  lowering.

- Prepare native reservations only when the active backend has a matching
  consumer, avoiding unused ARM64 live-struct and large-array batches. Retain
  one reserved identity for proven-dead ARM64 scalar constructors.

- Preserve invocation cancellation between reduced GC allocation batches and
  slow per-object allocations.

- Keep ordinary complete-loop and constructor/fill GC lowering, plus checked
  helper-based dynamic array copies, in the lean TinyGo runtime to meet its
  existing size budget.

- Prepare one checked native identity for proven-dead GC batches on both
  backends, avoiding unused live-object handle refills at collection.

- Port checked GC induction reductions and prepared invocation paths to AMD64,
  sharing allocation accounting and admission checks with ARM64.

- Share sparse scans, occupied-block major collection, survivor-aware nursery
  policies, zero-live recycling, and bounded native handle batches across
  AMD64 and ARM64. Use exact local roots on both backends outside EH paths.

- Unify ARM64 and AMD64 GC helper dispatch without overlapping helper IDs;
  cached native GC artifacts require recompilation for ABI version 9.

- Recycle zero-live AMD64 nursery identities in one batch while preserving
  cleanup for owned storage and exact-capacity free lists.

- Retain a checked AMD64 WasmGC object address across selected reads of other
  objects, using a bounded compiler scan and the existing safety checks.

- Keep ordinary AMD64 GC constructor selection and individual dead allocations
  in the lean TinyGo build to meet the CLI size limit. Other builds retain the
  specialized paths.

- Require recompilation of cached native WasmGC artifacts after the native GC
  ABI update to version 8.

- Accelerate ARM64 WasmGC scalar constructor reads, small numeric array
  allocation and access, array fills, i31 round trips, and proven nonescaping
  scalar-struct, uniform-array, and unsigned i31 induction sums. Preserve
  allocation accounting, collection policies, and checked helper fallbacks.

- Accelerate AMD64 WasmGC allocation, array operations, subtype casts, and
  i31 round trips with specialized generated paths, bounded native-handle
  batches, and survivor-aware nursery management. Preserve the existing Go
  fallbacks and collector safety checks.

- Return from AMD64 and ARM64 atomic wait mismatches and empty notify calls
  before the Go-call argument setup. Preserve memory ordering for zero-count
  notify and keep matching dynamic waits on the synchronized helper path.


### Added

Require `wago_nativecompare` so ordinary builds and install discovery exclude it.

- Add an opt-in operand-aware native comparison diagnostic with bounded Wasm
  capture, source anchors, explicit opaque-byte coverage and focused test recipes.
- Add Linux AMD64 and ARM64 integer conditional-join regression profiles with
  independent result checks, native placement controls, and compile/execution
  benchmarks.
  ([#881](https://github.com/wago-org/wago/pull/881))

## [v0.1.0-beta.13] - 2026-10-15

### Changed

- Share validated immutable native callback wrappers across instances. Each control
  frame retains the code; closing the last owner releases its mapping.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Hoist validated native i32 import guards for fixed shared-memory ARM64 loops, keeping
  admitted arguments/results in registers while preserving imported calls and the
  complete Go/foreign fallback. ([#963](https://github.com/wago-org/wago/pull/963))

- Forward an immediately repeated plain i32 load from its identical store in one defined
  fixed-size shared memory32 on ARM64. Preserve the store and its bounds/trap; atomic,
  host-boundary and profiling cases keep their required access paths. The
  `store-forward` optimization selection controls this lowering.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Select a guarded native body once at entry for bounded integer loops over identical
  scalar import aliases on ARM64. Keep every original callback call, the complete
  Go/foreign fallback, artifact admission, and optimization policies.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Reduce proven scalar host-import overhead with register arguments and cached dispatch
  metadata on ARM64, and smaller rooted callback resume state on both native backends.
  Fixed shared integer guests use the existing integer context bridge only when the
  whole-module proof and binding guards hold; ARM64 direct Go dispatch preserves the
  original callback and honors its rollback policy.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Cache stable shared-memory backing addresses on amd64 and arm64 while continuing to
  read live atomic extents. `shared-memory-base` selects the lowering independently;
  helper/context returns reestablish the backing cache.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Prove aligned constant atomic addresses against declared memory minimums on amd64 and
  arm64, retaining runtime checks where the proof fails. `atomic-static-bounds` selects
  this optimization independently. ([#963](https://github.com/wago-org/wago/pull/963))

- Use single-instruction acquire/release LSE atomics for arm64 RMW and compare-exchange
  when supported by the host. Preserve exclusive-loop fallback through `atomic-lse`, and
  persist CPU requirements for artifact admission.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Resolve literal zero-timeout shared-memory waits in native code, preserving
  alignment/bounds trap order and retaining the parking helper for other waits.
  `atomic-wait-zero-timeout` selects the lowering independently per compile.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Reuse exception payload summaries across functions on AMD64 and ARM64 to reduce
  compilation work for modules with many tags and `catch_all_ref` handlers.
  ([#837](https://github.com/wago-org/wago/pull/837))

- Improve ARM64 execution with guarded scalar and vector loop lowering, bounded register
  reuse, borrowed operands, indexed address reuse, exact integer dot and
  absolute-difference reductions, min-plus updates, independent floating-point maps,
  vector unrolling, scoped constants, static table-address bounds and arithmetic/branch
  simplification. Each optional lowering has strict admission checks and a
  per-compilation rollback option. Reduce compilation overhead with linear option
  resolution, cheaper loop/local-state scans and sufficient native image capacity to
  avoid reallocating dense large modules.
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

- Bound AMD64 compiler scratch growth by recycling operand arenas at proven-empty
  instruction boundaries, tracking control-frame eligibility in constant time and
  clearing only pointer slots written since the previous cleanup.
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

- Improve AMD64 and ARM64 lowering for small straight-line blocks, constant vector
  fills, adjacent SIMD addresses, scalar lane extraction, and proven minimum-memory
  bounds. Fold repeated pure integer terms and retain eligible floating-point globals
  and loop constants on ARM64. Each optional lowering has an independent optimization
  selection. ([#963](https://github.com/wago-org/wago/pull/963))

### Added

- Execute shared `memory.grow` on amd64 and arm64, serializing growers and publishing
  the new extent only after guarded pages are accessible. Shared accesses and size
  queries read the live extent across active instances; constant-zero growth uses a
  native size query. Preserve quotas and codec reload.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Execute shared `memory.copy` and `memory.fill` on amd64 and arm64, with overlap-safe
  copying and bounds checks before writes. Use unrolled small constant operations
  against shared backing on both architectures.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Execute ordinary scalar and SIMD loads/stores against shared memory on amd64 and
  arm64, preserving exact float/vector bits and explicit bounds. Shared accesses use
  backing-directory addresses. ARM64 can forward an exact adjacent plain i32 store/load
  pair under the fixed-memory proof; other shared accesses keep their established
  load/store path. ([#963](https://github.com/wago-org/wago/pull/963))

- Execute `memory.size` against shared-memory backing on amd64 and arm64, including
  defined/imported memories and codec-loaded products. Admit `atomic.fence` in modules
  without linear memory. ([#963](https://github.com/wago-org/wago/pull/963))

- Execute defined shared memory with classic atomics and wait/notify, keeping shared
  backing separate from per-instance control. Preserve backing ownership across exports
  and producer close, and account for both native mappings.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Implement wide integer arithmetic with native amd64/arm64 carry chains and
  signed/unsigned widening multiplies, public feature selection, and persisted artifact
  requirements. Cover edge cases, register pressure, and trap order.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Expose compact import sections through `CoreFeatureCompactImports` and CLI discovery,
  independently of multiple-memory admission. Enable grouped imports by default without
  adding runtime metadata or changing native code.
  ([#963](https://github.com/wago-org/wago/pull/963))

- Add signed/unsigned SIMD floor-average source checks, baseline benchmarks, and a
  bounded decoded corpus census. ([#875](https://github.com/wago-org/wago/pull/875))

- Add bounded AMD64 integer loop-state regressions with independent state/work checks,
  native layout controls, and paired compile/execution benchmarks.
  ([#882](https://github.com/wago-org/wago/pull/882))

- Add full-vector widening-multiply source-pair tests, prepared-call benchmarks, and a
  bounded decoded corpus census. ([#874](https://github.com/wago-org/wago/pull/874))

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

- Align the installer module dependency and workspace replacement with beta.13 so
  release qualification accepts the requested version.

## [v0.1.0-beta.12] - 2026-10-07

### Changed

- Reduce AMD64 compilation allocations and repeated work in immutable-table
  analysis, adapter grouping, spill tracking, local realization, and barriers.
- Avoid callback-context allocations and cancellation watchers for synchronous
  re-entry from invocations with no cancellation or deadline signal.
- Keep native profiling and compiler diagnostics out of ordinary builds;
  enable them with `wago_profile` and `wago_codegenstats` build tags.
- Raise release size limits to 12 MB for Go builds and 3 MB for TinyGo.
- Reuse both AMD64 hardware divide results for adjacent division/remainder
  pairs on the same local operands, reducing dependent-pair latency.

- Emit one AMD64 immediate store for i64 constants representable as a signed
  imm32, and shorten guarded constant-store register lifetimes under pressure.
- Improve AMD64 execution with bounded local and caller register reuse, direct
  branch-result transfers, memory operands, and scalar/vector loop lowering.
- Reduce compilation work by skipping GC conversion scans for validated non-GC
  modules and synchronizing only the globals assigned to physical registers.
- Reuse owned integer shift operands and an additional revocable local register
  in admitted straight-line AMD64 functions.

### Added

- Add opt-in native profiling commands to record workloads, inspect hot
  functions and instructions, compare captures, and view invocation timelines.
- Add compile-time register-transfer and immutable-register lifetime checks
  for AMD64 and ARM64 through the `wago_regalloccheck` build tag, plus a bounded
  control-flow verification core.
- Add an experimental shared scalar compiler path for AMD64 and ARM64.
- Add Wazy to the cross-runtime benchmark corpus and publish its results.
- Add AMD64 bounds-proof invalidation tests and compile/execution benchmarks.

- Add rejection controls for omitted native padding and artifact metadata.

- Add host-event timing-boundary and replay-failure controls.

- Add exact core-SIMD source-pair checks and benchmarks for byte averages,
  high-byte extraction, and signed constant dot/sub expressions.

- Add semantic result-profile controls and loaded-artifact/omitted-case checks
  to the conformance test runner.

- Add shared/established compiler-path comparisons at admission boundaries,
  with typed-control validation, execution controls, and paired benchmarks.

- Add bounded rule-directed scalar, SWAR, and SIMD compiler checks, independent
  result models, activation controls, and equal-budget generation benchmarks.

- Add AMD64 loop-boundary regression checks for deferred producer placement,
  floating-point transport, register pressure, and zero-iteration traps.

- Add AMD64 fresh-worker comparisons for reused compiler scratch, error recovery,
  native execution, and worker memory release, with a test-only omitted-reset control.

- Add two prebuilt Grain standard-library assertion suites to the executable
  corpus, with matching guest sources, reproducible rebuild instructions,
  preserved licenses, and independently verified WASI execution oracles.

### Fixed

- Publish `wago build` output atomically, preserving existing artifacts when
  publication fails and respecting platform access controls and file metadata.
- Bound CLI module reads and artifact-cache admission, rejecting oversized
  inputs before cache lookup or allocation.
- Serialize plugin builds and configuration mutations, keep rejected staged
  rebuilds out of the active runtime, and cancel active Go commands during
  staged plugin changes. Bound verification and retry runners after publication.
- Read plugin metadata as one snapshot and isolate mutable plugin-set metadata.
- Make `wago add --allow-all` noninteractive, route plugin inspection aliases
  to the runtime, and group canonical plugin repository paths consistently.
- Keep version-cache cleanup within its intended tree, select stable semantic
  versions for official releases, and resolve annotated rolling-release tags.
- Reconnect refreshed installer shells to terminal input, await CLI runtime
  teardown, and preserve terminal close observers during shutdown.
- Reject stale local configuration saves instead of overwriting newer changes.
- Preserve guest roots and foreign-clone tokens during GC graph reconstruction;
  avoid retaining roots for immediately dropped extern-reference conversions.
- Preserve AMD64 pinned registers across `table.copy`, exclude fixed bulk
  scratch registers from immutable caches, and reload canonical ARM64
  `externref` destinations for `table.fill`.
- Correct deferred spill preservation, fixed-register conversion and comparison
  operands, `i16x8.bitmask` under register pressure, and memory32 wraparound in
  unrolled linear sums. Restore pinned locals across control and exception edges.
- Honor configured Core features in standalone compilation, reject invalid
  bounds-check modes and nil modules, decode block type indexes as signed
  33-bit values, and validate descriptor metadata and equality casts.
- Use structural identities for indirect-call types and sort provided imports
  by exact identity. Correct large ARM64 host-thunk transfers and wide
  synchronous host exchange areas.
- Correct worker scratch-growth coverage, bounds-test mode isolation, semantic
  mismatch counts, optional Node feature checks, and prepared-call benchmark setup.

- Match direct and validated feature summaries for bulk table instructions.

- Preserve live AMD64 values across signed i64 division when the overflow
  guard runs under register pressure.

- AMD64 guarded i64 constant stores use a single eight-byte native store.
  ARM64 additionally checks the entire access before writing, preventing
  partial memory updates from trapping unaligned constant stores.
- ARM64 table64 bulk operations now trap when `start + count` wraps instead of
  accepting the wrapped range and addressing outside the table allocation.
- Preserve live AMD64 collector references loaded by `global.get` across later
  allocations.
- AMD64 and ARM64 now report deferred division, remainder, and guard-page load
  traps before later conversion, memory, table, reference, and indirect-call traps.
- Preserve earlier call arguments when later deferred expressions use fixed
  registers for shifts or division, including mixed and tail calls.
- Local `wago-installer` builds no longer mistake a dirty tagged checkout for a
  published release on Go 1.24 through 1.26.
- Actions artifact downloads keep GitHub bearer tokens on the catalog origin
  instead of forwarding them to a cross-origin archive URL.
- Full uninstall removes a custom `WAGO_HOME` only when known Wago state cleanup
  leaves it empty, preserving unrelated files in a shared directory.
- Legacy exception-handling opcodes now report their unsupported encoding and
  a `try_table`/exnref migration hint instead of a generic invalid instruction.
- Recovered backend panics now return a distinct `InternalCompilerError` with
  function/bytecode context and bounded diagnostics instead of ordinary rejection.
- AMD64 and ARM64 preserve deferred division, remainder, and guard-page load
  traps before tail-call target checks or callee side effects.
- AMD64 guest floating-point execution now uses WebAssembly's nearest-even,
  masked-exception state and restores the host thread's MXCSR on every exit.
- AMD64 and ARM64 discard exited `try_table` handlers on taken branches, so
  later exceptions cannot jump into an exited scope or a returned frame.
- Preserve AMD64 `select` operands when deferred divisions, remainders, shifts,
  or rotates reclaim fixed registers, and report operand traps in WebAssembly
  evaluation order.
- Tables with no declared maximum can grow past 1,024 entries; a table can
  now at least double from its initial size.
- GC tables accept Core 3 initializers such as `(ref.null any)`, `ref.i31` and
  extended constants, and constant expressions accept chains of
  `any.convert_extern`/`extern.convert_any` on null. An allocating
  initializer is evaluated once, so every slot holds the same object.
- AMD64 nullability refinements preserve whether a reference belongs to the
  compact collector, so native function references are not scanned as GC roots.
- ARM64 scalar `min`/`max` local sinking now preserves an aliased right operand
  instead of overwriting it with the left operand before the comparison.
- Non-nullable bottom reference types such as `(ref noextern)` are accepted.
- `Global.GetValue`/`SetValue` work for `externref` globals holding converted
  GC references.
- AMD64 no longer crashes when a tail target throws after a wrapper-ABI tail
  call made inside a `try_table`, and accepts tail calls from register-ABI to
  result-bearing wrapper-ABI functions. ARM64 mixed register/wrapper tail
  loops no longer grow the stack.
- Managed code-generation plugins can no longer type-assert their restricted
  callback context to the full raw-encoder context on AMD64 or ARM64.
- A `try_table` may have up to 1,024 catch clauses (previously 8).
- ARM64 tail calls from a function with reference-typed parameters into a
  numeric register-ABI function no longer lose the callee's updates to
  register-pinned module globals.
- AMD64 tail calls from a function with reference-typed parameters into a
  function that is only called directly now pass the correct arguments and
  keep its updates to register-pinned module globals.
- Decode `0x64` consistently as the Core 3 non-null reference prefix, avoiding
  cross-vector consumption when it appears at the end of a value-type vector.
- AMD64 and ARM64 compile a `try_table` that has parameters in unreachable
  code instead of failing with an internal code-generation error.
- Darwin/ARM64 cancellation and `Close` no longer hang behind a concurrent
  garbage collection, and an interrupted call is no longer occasionally
  reported as successful.
- AMD64 no longer skips code after a `try_table` that is exited by a branch to
  its own end (for example `br 0`), which previously ran the exception handler
  instead and could return early or report an unhandled exception.
- Full-access AMD64 plugins can declare every optional CPU feature exposed by
  the encoder, and compiled artifacts preserve those admission requirements.
- Dropping a null `exnref` produced by the fallthrough of a `catch_ref` or
  `catch_all_ref` target no longer crashes the process on AMD64 or ARM64.
- A module declaring an unused exception tag with more than eight parameters is
  now rejected at compile time with a clear limit error instead of failing at
  instantiation.
- Exception records are sized per function: a function may nest up to 1,024
  `try_table` levels and hold up to 1,024 `catch_ref`/`catch_all_ref`
  results (previously 4 each, reserved in every function of a module with
  tags).
- Exception handler and rooted-catch indexes retain their full admitted range
  above 256 records on AMD64 and ARM64.
- Exception tags may carry up to eight values (previously two), including GC
  references such as structs, arrays, `i31ref`, `anyref` and `externref`.
- AMD64 `catch_all_ref` catches exceptions of every tag, not only the first
  declared one.
- `return_call_ref` works between any register- and wrapper-ABI signatures on
  AMD64. On ARM64, mixed tail loops through references no longer grow the
  stack, and a wrapper-ABI function's tail call into another instance no
  longer leaves that instance installed for its caller.
- Compiled artifacts record their native architecture and reject foreign-ISA
  code before allocating an executable image.
- Modules using `any.convert_extern`/`extern.convert_any` instantiate with any
  combination of reference tables.
- `ref.test` on an `anyref` converted from a host `externref` no longer traps
  at random on AMD64, and casts of such a value report a cast failure on
  ARM64.
- Global `ref.i31` initializers whose operand is extended-constant arithmetic,
  such as `(ref.i31 (i32.add (i32.const 1) (i32.const 2)))`, instantiate
  instead of failing as invalid compiled metadata.
- Custom plugin inputs can no longer be read through scalar or checked-memory
  accessors, which previously aliased them with spill slot zero.

## [v0.1.0-beta.11] - 2026-09-29

### Added

- Restore `WasmFunc.OpenSession` and `PreparedSession` for repeated calls under
  one instance reservation; `Close` releases the reservation.
- Add configurable export caching through `InvokeCacheSlots` and
  `WithInvokeCacheSlots`, with four inline slots by default and 1–255 entries.
- Add required WebAssembly 1.0 conformance checks and forced-SSE2 coverage.

### Changed

- Make SSE2 the AMD64 baseline with fallback lowering for core SIMD and
  supported relaxed SIMD; use newer CPU extensions when available.
- Speed ordinary and prepared invocations without requiring caller-owned
  sessions, and reduce ordinary Go host-callback overhead.
- Reduce quadratic work in recursive type analysis, compiler metadata,
  operand stacks, GC type mappings, and runtime lookups.
- **Breaking:** compiled artifacts use format v4. Recompile persisted artifacts
  from earlier releases before loading them.
- Make CI qualification profile-aware and remove duplicate corpus runs.

### Fixed

- Correct ARM64 overlapping call arguments, bulk-helper register clobbers,
  stale indexed addresses after signed loads, and wide fixed-array GC roots.
- Correct GC transient-root enumeration, reference-array initialization,
  promotion views, collection accounting, failed-restart recovery, and
  zero-value collector shutdown.
- Reject invalid GC headers, overlapping fields, invalid alignment, and
  incompatible inherited storage layouts.
- Validate malformed programmatic Wasm types, function bodies, instruction
  kinds, immutable-global writes, Memory64 offsets, and wrapped 32-bit indexes.
- Track CPU requirements for emitted plugin lowerings, so unused plugin imports
  no longer require AVX. Gate scalar bit-count instructions on CPU support.
- Correct canary artifact discovery and Windows publishing-cache setup.

## [v0.1.0-beta.10] - 2026-09-23

### Added

- The shared correctness and benchmark corpus now runs 40 full application
  executables end to end, with pinned provenance and exact independent output
  oracles for application-scale JavaScript, SQL, compression, CLI, compiler,
  language-runtime, cryptography, and FPGA-tool workloads.

### Changed

- Host imports instantiate faster and allocate less by sharing immutable
  dispatch thunks across instances and deferring instance-specific state until
  it is needed.
- Dynamic `memory.copy` and `memory.fill`, `memory.grow(0)`, and large funcref
  `table.fill` operations are faster across AMD64 and ARM64.
- Compiler value-type reuse, scalar host-call handling, wide GC dirty-range
  scanning, and Linux idle-memory reclamation reduce runtime and compilation
  overhead.

### Fixed

- Correct ARM64 miscompiles involving mixed float-call arguments, large `v128`
  stack displacements, pinned and deferred loads, `memory.fill` scratch
  registers, and guard-mode indexed-base reuse. These fixes address PDFium
  rendering failures and bulk-memory corruption under register pressure.
- Ensure reused linear memory is zeroed on Darwin when `MADV_ZERO` is
  unavailable.
- Initialize cached float constants only at function entry so branch-specific
  lowering cannot reuse an uninitialized register on AMD64 or ARM64.
- Correct nested shifts and rotates, non-null global writes, shared-memory
  imports, reference widening and nullability, multi-memory encoding, AVX-512
  high-register forms, large-offset bounds checks, GC maps and safepoints,
  passive-element overflow, and aliased code-buffer growth.
- Correct explicit Core-profile precedence, `_start` signature validation,
  watch-flag parsing, result formatting with mismatched metadata, plugin source
  selection, semantic-version wildcard validation, and full uninstall cleanup
  for active XDG directories and Fish completions.

## [v0.1.0-beta.9] - 2026-09-16

### Added

- `wago run` accepts repeated `--invoke` flags and calls the selected exports in
  order on one module instance. Each call consumes only the positional values
  required by its signature, including typed values such as `7:i64` and
  `3.5:f64`.
- Core 3 is enabled by default, including compact imports and broader
  WebAssembly GC validation and execution coverage.
- Portable typed host callbacks support ordinary scalar Go functions,
  `func(HostCall)`, and caller-aware `func(Caller, HostCall)` callbacks under
  both Go and TinyGo.

### Changed

- The README's one-shot Go installer command now runs the installer from
  `@main`, and the redundant persistent-installer example has been removed.
- **Breaking:** host imports now use `NewImports` and flat
  `HostFunc(module, name, fn)` registration. The namespace builder and the old
  `Func`, `CallerFunc`, raw-slot callback, and signature-specific registration
  APIs have been removed.
- **Breaking:** `WasmFunc` replaces `PrepareFunction` and all
  signature-specific prepared handles. `WasmFunc.Invoke(args...)` is the only
  resolved-function invocation method; numbered `Invoke0` through `Invoke4`
  methods have been removed.
- **Breaking:** public prepared sessions and `OpenSession` have been removed.
  Each invocation now performs its own lifecycle admission.
- **Breaking:** by-name execution is `Invoke` or `InvokeContext`; tagged-value
  calls use `InvokeValues` instead of `Call`.
- Host/Wasm dispatch, typed callbacks, Railshot-generated execution, branch
  hints, import-range lookup, and prepared-artifact admission are faster while
  retaining the existing ownership and resource checks.

### Fixed

- Preserve WebAssembly GC roots and metadata across fixed-array results,
  prepared calls, table reads, shared domains, cancellation, and callbacks.
- Bound synchronous host callback re-entry to four active calls per invocation
  chain, including cross-instance re-entry, without retaining an execution
  reservation between calls.
- Harden instance shutdown, cancelable admission, retained memory callbacks,
  compile-lifetime observers, prepared-artifact limits, and host re-entry close
  interruption.
- Correct Core 3 validation and name decoding, ARM64 register allocation and
  branch patching, Windows cleanup behavior, semantic-version prerelease
  ranges, build output aliasing, and installer fallback behavior.

## [v0.1.0-beta.8] - 2026-09-10

### Added

- Added Beta and commit-addressed Canary release channels, with qualified
  release artifacts and a Go-native installation route.
- Added explicit native-artifact trust controls, runtime resource policies,
  callback re-entry, and broader Core 3 and WebAssembly GC support.
- Added end-to-end semantic corpus coverage and differential engine-state
  testing.

### Changed

- Replaced the Make-based developer interface with Just.
- Reduced compiler memory use, compile latency, host-call overhead, and prepared
  invocation latency across AMD64 and ARM64.
- Simplified release notes and installer/channel discovery behavior.

### Fixed

- Fixed JIT, invocation, validation, settings, TinyGo trampoline, signal,
  reference-lifetime, and resource-limit correctness issues found by the
  September audit.
- Fixed AMD64 corpus miscompiles, ARM64 indexed bulk-memory bounds checks, and
  incomplete typed-select immediate decoding.
- Fixed Beta discovery and retracted legacy tagged Canary module versions.

[Unreleased]: https://github.com/wago-org/wago/compare/v0.1.0-beta.12...HEAD
[v0.1.0-beta.12]: https://github.com/wago-org/wago/compare/v0.1.0-beta.11...v0.1.0-beta.12
[v0.1.0-beta.11]: https://github.com/wago-org/wago/compare/v0.1.0-beta.10...v0.1.0-beta.11
[v0.1.0-beta.10]: https://github.com/wago-org/wago/compare/v0.1.0-beta.9...v0.1.0-beta.10
[v0.1.0-beta.9]: https://github.com/wago-org/wago/compare/v0.1.0-beta.8...v0.1.0-beta.9
[v0.1.0-beta.8]: https://github.com/wago-org/wago/releases/tag/v0.1.0-beta.8
