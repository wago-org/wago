# Performance and security audit repair plan

This plan covers every finding in the supplied GPT 5.6 audit. The inspected
checkout is `f0f4951138b8`; the audit compared it with `447f057115ee` on `main`.
The code paths and existing benchmark names below were checked in this checkout.
The audit's timings were not reproduced for this plan. No source fixes or
benchmark runs are part of this planning task.

Keep the branch unmerged until the correctness and lifecycle fixes pass their
tests. Keep strict rejection of malformed structured custom sections. Keep the
native scheduler transition. No feature-support change is proposed.

Use the audited branch as the baseline for each repair. Use the audited `main`
commit as a second reference for the final end-to-end report. Comparing only
with `main` would hide which costs a repair adds to, or removes from, this branch.

**Order and commit scope**

1. Capture baselines and add the missing measurement fixtures.
2. Fix interrupt lifetime/ordering, decode admission, snapshot publication, and
   manager state/release lifetime. Each independent fix gets its own commit.
3. Remove signature, registry, import-map, and private-metadata copies.
4. Fix JSON/lock serialization, checked-GC scratch, and the small utility costs.
5. Correct GC API docs and run the full release gates.

The work groups below are review units, not a request for one large commit per
group. Include the matching test and documentation change with each repair.
Use `skills/commit/SKILL.md` when preparing implementation commits. Preserve the
existing local `AGENTS.md` edit.

**1. Interrupt lifetime, ordering, and context overhead**

Audit coverage: merge blocker 1; ARM64 ordering; performance item 8; repeated
signal negotiation and registry scans from item 9.

- Reset token issuance under the request coordinator when no request is active,
  with a new nonzero cookie distinct from the old one. Coordinate cookie changes
  with handler setup and all signal readers. A sequence reset alone is unsafe.
  Define the temporary exhaustion behavior while requests remain active; new
  requests must work again after they drain. Late signals from an old generation
  must not interrupt a reused trap cell or break signal chaining.
- Review each ARM64 publication pair, including code-range bounds and the linear
  memory registry. Use acquire reads where release publication requires them.
  Preserve the reader protection that keeps memory alive during signal handling.
  Race-detector success does not prove assembly ordering.
- Return the shared no-op watcher before channels, timers, or `AfterFunc` setup
  when the context is nil or `Done()` is nil. Keep deadline and cancellable
  contexts on their existing supported path.
- After the lifetime fix, measure handler negotiation and scan cost separately.
  Reuse established signal ownership while registered code needs it. Preserve
  handler restoration and third-party signal-handler coexistence. Use bounded
  registry bookkeeping; optimize scans only where the measurements show a cost.

Tests: token-generation transition, old-token rejection, active/shared request
lifetime, slot reuse, full-capacity admission, handler restoration, memory close
during a signal reader, and cancel/deadline races around native entry and host
return. Run ARM64 tests on native Linux hardware. Test Background, TODO,
`WithoutCancel`, explicit cancellation, deadline cleanup, and repeated stop calls.
Use isolated test processes for global signal state.

Benchmarks: B1 and B2 below. Track cancel-to-return p50/p95/p99, CPU time, syscall
counts in a separate profile run, and goroutines/timers left after cleanup.

**2. Structured decode budgets and type storage**

Audit coverage: merge blocker 2; performance item 3.

- Give `name` and branch-hint decoders the parent allocation budget. Reserve
  concrete container capacity, strings, owned payloads, temporary state, and
  growth before allocation. Use checked arithmetic and a documented allowance
  for allocator/map overhead. Encoded payload length times 128 is not a measure
  of decoded storage. Avoid charging borrowed bytes or the same allocation twice.
- Preserve all structural checks. Replace the malformed budget fixture with
  separate valid-large, malformed, and genuine over-budget cases. A valid input
  may still exceed an explicit resource limit; the acceptance test must fit the
  decoded-memory limit.
- Allocate singleton subtype storage only when an implicit singleton occurs.
  Compare lazy chunks with geometric slabs. Prefer bounded chunks if geometric
  growth leaves old slabs retained by group slices. Keep explicit recursive
  groups contiguous and singleton slice capacities isolated from neighbors.

Tests: AST and byte-backed paths; valid names above the old roughly 2 MiB cutoff;
long names versus many short names; nested local/field maps; malformed UTF-8,
counts, ordering, and subsection lengths; branch hints; exact budget boundaries;
mixed explicit/implicit groups; total subtype limits; arithmetic overflow.

Benchmarks: B3 and B4. Compare actual allocation bytes, reserved budget, retained
capacity, and peak RSS. All-explicit groups should reserve and allocate no unused
singleton slab. Tiny modules must not pay a large minimum chunk cost.

**3. Publish and bound hand-built `Compiled` state**

Audit coverage: both parts of merge blocker 3.

- Publish the cache, validation state, and execution snapshot through one coherent
  initialization protocol. All readers must follow it, including reflection,
  serialization, code access, instantiation, and close. Prefer a shared private
  owner with initialization state and safe pointer publication. Preserve the
  compiler-produced single-owner allocation.
- Do not place a used `sync.Once` or atomic wrapper into a value that existing
  snapshot code then copies with `out := *c`. Audit value-copy and codec-replace
  behavior before choosing the representation. A CAS loser must not clone a
  second large snapshot while waiting for the winner.
- Add a checked, allocation-free size preflight before deep cloning. Cover all
  nested signature/type/name/GC-field/element/data slices and map storage, not
  just top-level lengths. Count the destination expansion even when source
  slices alias. Check both sums and byte-size products.
- Make admission failures reach error-returning API boundaries before cloning.
  Apply the effective decoded-metadata limit, with a documented default for
  hand-built values. The current instance off-heap metadata quota is not a Go
  snapshot-memory quota; connect the appropriate policy explicitly. Preserve
  existing public accessor signatures. Separate a shared structural-validation
  result from checks of a caller's stricter resource policy.

Tests: concurrent first instantiation/reflection of one fresh hand-built object,
without warming the snapshot first; one owner/snapshot; close and codec ownership;
repeated nested slices under a small limit; preflight overflow; public-view
mutation isolation; successful use at the budget boundary. Concurrent mutation
of caller-owned public slices during admission remains outside the contract.

Benchmarks: B5. Check cold first use separately from warm use, parallel first-use
peak memory, and the existing packed-slice and fixed-owner allocation tests.

**4. Manager state and plugin operation consistency**

Audit coverage: merge blockers 5, 6, and 7.

- Add a cross-process mutation lock keyed by the canonical installed version.
  Put its stable path outside the version tree that uninstall deletes. Install,
  update, selection/use, and uninstall must share it. Stage downloads/builds
  before the commit lock where possible, then revalidate under the lock.
- Set one lock order: version mutation before active-state publication. For a
  multi-version operation, acquire version locks in canonical order. Keep the
  version lock through removal and the conditional active-state update. Test
  failure ordering so selection does not point to a partially removed version.
- Read the atomically published active record through the existing
  `regularfile.ReadAtomicSnapshot`; ordinary reads must not create directories,
  lock files, or change modes. Use the writer lock for migration and re-read
  after acquiring it. Define legacy read-only behavior explicitly: read the
  frozen legacy tuple without a mandatory write, and migrate when a writer can
  publish the complete record. Preserve strict corruption errors.
- Resolve one version/profile/build tuple per plugin operation, including
  environment overrides. Pass it through path, source, compiler, cache-key, and
  artifact-publication helpers. Remove downstream active-version re-reads.
  Where a build must survive uninstall, pin or revalidate its selected source
  under the version lifetime protocol before using or publishing it.

Tests: forced install/uninstall/use interleavings across processes; reinstall of
the same version; operations on different versions; readers during atomic
replacement; read-only current and legacy configuration as an unprivileged user;
no filesystem mutation from reads; local/global plugin operations during version
switches, including profile/build overrides. The expected result is one complete
operation tuple, never a build from one tuple under another tuple's paths.

Benchmarks: B6. Include cold reads and repeated reads, uncontended mutation cost,
same-version contention, independent versions, and plugin scope resolution.

**5. Recover interrupted Windows uninstall**

Audit coverage: merge blocker 4.

- Replace the empty pending marker with a bounded, atomically written cleanup
  record. Store operation/generation identity, coordinator file identity,
  worker process identity including creation time, cleanup phase, and validated
  target scope. A PID or elapsed timeout alone is not proof that work is stale.
- Under the publication lock, recognize live work or resume/recover stale work.
  Make each phase idempotent. A delayed old worker must revalidate its generation
  under the same lock before deletion, so it cannot delete a newer installation.
- Cover the worker-start handoff and recovery of old empty markers. Keep a
  retryable, diagnostic state when cleanup cannot finish. Remove the record only
  when it is safe for a new publication to proceed.

Tests: worker-start failure, termination between each phase, simulated reboot,
PID reuse, old marker migration, locked targets, repeated recovery, and a stale
worker returning after reinstall. Use disposable roots on native Windows; script
text tests and cross-compilation alone do not verify the lifecycle.

Benchmarks: B7. Separate ordinary publication from recovery. Track cleanup time,
filesystem operations, and retry allocations. These are availability fixes;
small bounded cold-path costs may be justified with measurements.

**6. Transfer one release lease across launcher exec**

Audit coverage: inherited release leases.

- On Unix, pass one lease descriptor through launcher-to-payload exec. Validate
  its identity against the selected release, adopt it in the payload, clear the
  handoff metadata, and restore `CLOEXEC` before plugins or child commands run.
  Direct payload execution acquires one private close-on-exec lease.
- Close failed transfers without leaks or double unlock. Preserve pruning and
  legacy-release rules. If a child actually needs release source after its parent
  exits, give that operation an explicit scoped lease.
- Keep the Windows parent-held lease protocol unless native tests show a change
  is needed. Descriptor containment is not a new security boundary between
  processes with the same user privileges.

Tests: one payload lease, direct launch, exec failure, invalid handoff, ordinary
child exec with no lease, manager exit with an unrelated long-lived child, and
launch/prune races. Verify the running payload stays pinned until its exit.

Benchmarks: B7 and B8. Count open descriptors before/after repeated dispatch and
measure actual launcher startup, not only the lease helper.

**7. Remove signature and artifact-write allocations**

Audit coverage: performance item 1.

- Add allocation-free exact-signature validation. Compare descriptor ABI types
  directly against the existing slices; do not allocate legacy conversion
  slices merely to compare them.
- Write indexed signatures directly after validation. Use borrowed immutable
  views internally; clone only when returning caller-owned public results.
  Keep structural checks for both indexed and non-indexed signatures.

Tests: ABI mismatch, indexed reference and recursive types, multi-result and
v128 signatures, public-return mutation isolation, artifact round trips, and
writer errors. Preserve artifact bytes for this implementation-only change.

Benchmarks: B9. The audit reported 268,101 ns/op, 263,319 B/op, and 12,829 allocs/op
for `BenchmarkWriteCompiledImportedModule`. The target is to remove the roughly
12,800 signature-related allocations and make remaining streaming-write
allocation count independent of signature count. This is a target, not a new
measured result. Do not promise zero allocations for output-buffer APIs.

**8. Registry snapshots, import ownership, and metadata views**

Audit coverage: performance items 2, 4, 5, and the inline import-index suggestion.

- Publish an immutable registry generation containing both import values and
  metadata. Compile reads one generation. Copy on mutation/publication boundaries
  and batch extension registration; avoid turning N initial registrations into
  N full-map copies. Preserve the existing compile-time binding policy.
- Old modules may retain their required generation. Avoid an extra global
  history/cache. Measure retention when an old module remains open through many
  registry changes, then after it closes.
- Build the per-instance resolved import map once and transfer ownership down
  the private instantiation path. Preserve one defensive copy for caller-owned
  maps at the public low-level API. Check rollback and resolution failures.
- Size `ImportSpec` storage from checked import-kind counts. Borrow signatures
  from the private execution snapshot. `ModuleView` can retain private immutable
  import metadata; its public accessor performs the single required deep copy.
- Measure a small inline import-identity/index table before choosing its size.
  Keep a map fallback for larger modules. Duplicate and ambiguous identity checks
  must remain exact. Extra inline bytes must be justified for importless modules.

Tests: registration versus compile, old/new generation isolation, revoked or
failed registration, ownership of host metadata/maps, hook-return mutation,
mixed import kinds, duplicate identities, failed instantiation, and close.
Update benchmark setup that writes `rt.imports` directly to use the actual
registration path, outside the timed compile/instantiate region.

Benchmarks: B10 and B11. Importless compile/instantiate overhead must not grow
with unrelated registry size. Also measure registry-write cost; moving all work
onto startup or retaining all old generations is not an acceptable hidden cost.

**9. Strict JSON and generated lock encoding**

Audit coverage: performance items 6 and 7.

- Build immutable struct descriptors keyed by `reflect.Type`. Bound both cache
  entry count and retained descriptor bytes, with a correct uncached fallback.
  Measure cold construction, warm hits, and type churn.
- Resolve exact and folded names through lookup tables, fold each input key at
  most once, and use field IDs with a small bitset for recognized fields. Retain
  the duplicate-key tracking required for unknown fields, maps, and RawMessage.
  Cover tags, embedded fields, ambiguous names, and Unicode folding against the
  supported Go `encoding/json` behavior. Preserve the project's stricter
  duplicate rejection and its byte/depth/value limits.
- In `EncodeLock`, validate the graph once, marshal once, then keep the strict
  token/limit pass needed for embedded RawMessage. Remove the second typed decode
  and graph validation. Preserve encoded-byte, nesting, and collection limits
  that the old `DecodeLock` call supplied. Check large host graphs before building
  an avoidably oversized output where the existing shape limits permit it.

Tests: duplicate/case-folded members, nested maps and raw plugin config, trailing
values, malformed raw values, depth/count/byte boundaries, shared source-release
rules, dependency graph errors, deterministic output, and concurrent cache use.

Benchmarks: B12 and B13. Warm typed lookup should scale with input keys and bytes,
not keys times struct fields. Cache memory must plateau under type churn.

**10. Checked-GC scratch and GC API documentation**

Audit coverage: root/constructor scratch from item 9; tertiary GC API issue.

- Reuse bounded per-collector root and constructor scratch. Clear used slots on
  success, error, panic unwind, and close. Do not retain oversized one-off
  buffers indefinitely. Keep storage valid until the native operation finishes.
- Preserve the existing callback-then-validation order. Root callbacks can
  re-enter or invalidate the collector, so use scoped scratch ownership or a
  bounded nested fallback. Keep the documented external-synchronization contract.
  Do not remove owner/generation checks to obtain a lower allocation count.
- Correct telemetry examples for the checked `gc` versus trusted `gc/native`
  APIs. Prefer small checked telemetry forwarders if that keeps the documented
  API useful without exposing raw references. Add a migration note listing the
  source-breaking reference, constructor, and root API changes. Do not restore
  unsafe raw-reference aliases as a compatibility shortcut.
- Compile examples with and without `wago_gcstats`; check disabled telemetry
  behavior. If forwarders are added, verify their close behavior and result
  ownership. Keep ordinary builds free of telemetry instrumentation cost.

Tests: foreign/stale references, callback re-entry/close/panic, large then small
root sets, constructor failures, collection during constructors, and both Tiny
and Throughput collectors.

Benchmarks: B14 and B15. Warm scratch should avoid per-element Go allocations;
report fixed callback overhead separately. Measure retained scratch after a large
outlier and after close, as well as native managed-GC work.

**11. Small file, lock, and cleanup costs**

Audit coverage: bounded reads, cleanup ancestors, and retry timers from item 9.

- Preallocate reads from the checked opened-file size. Keep the independent
  limit-plus-one guard, identity checks, and change detection. Size is only an
  allocation hint; growth, truncation, symlinks, and replacement still need checks.
- Allocate the file-lock retry timer only after the first failed lock attempt,
  then stop/drain/reset it safely for Go 1.22 and later. The uncontended path must
  not gain a timer. Check cancellation at acquisition and retry boundaries.
- Precompute protected cleanup ancestry for both configured and resolved paths.
  Keep file-identity and Windows case/volume behavior. Retain fresh validation at
  destructive boundaries and do not traverse directory links. A string-prefix
  set is not a replacement for the current path-protection rules.

Tests: bounded-file and atomic-snapshot suites; retry cancellation and lock-file
retirement; protected aliases, deep paths, missing paths, Windows casing, and
symlink changes in disposable cleanup trees.

Benchmarks: B16. Report filesystem operation counts separately from allocation
counts so a lower Go allocation total does not conceal extra OS work.

**Benchmark inventory**

Names under “Existing” are present in the inspected tree. Names under “Add” are
proposed and must be implemented before their selectors can provide coverage.
All Go benchmark rows report `ns/op`, `B/op`, and `allocs/op`. Check useful results
or expected errors. Do not let missing fixtures, skipped execution, or rejected
valid inputs appear as a fast successful benchmark.

| ID / package | Existing benchmarks to run | Add or extend; required cases |
|---|---|---|
| B1 `src/core/runtime`; `src/wago` | `BenchmarkCrossBoundaryCall`, `BenchmarkHostCall`, `BenchmarkLinearMemoryAccess`; `BenchmarkInvokeAddOne`, `BenchmarkPreparedInvokeAddOne`, `BenchmarkInvokeHostFuncDirect`, `BenchmarkInvokeCrossInstanceDirect`, `BenchmarkInvokeCrossInstanceIndirect`, `BenchmarkCallerResolverInvocationContext` | `BenchmarkInvokeContext`: nil/Background/TODO/WithoutCancel/cancellable/deadline; direct and host-call paths. `BenchmarkCancellationWatch`: setup/stop separately. Core boundary benchmarks in `bench_test.go` are Linux/amd64-only; add equivalent native ARM64 coverage. |
| B2 `src/core/runtime` | Existing interrupt ownership/capacity tests supply fixtures; no dedicated interrupt benchmark was found. | `BenchmarkInterruptRequestLifecycle`, `BenchmarkInterruptDeadlineLifecycle`, `BenchmarkInterruptCancelLatency`, `BenchmarkInterruptRegistryScaling`: cold/warm handler; 1/8/64 requests; 1/64/4096 code/memory entries where the API permits; full, sparse, and churned registries; sequential and concurrent callers. Collect latency distributions outside ordinary mean-only Go benchmark output. |
| B3 `src/core/compiler/wasm`; frontend | `BenchmarkDecodeModuleByteBackedBranchHint`, `BenchmarkDecodeValidate`; frontend `BenchmarkDecodeValidateSmallScalar`, `BenchmarkDecodeValidateMediumControl`, `BenchmarkDecodeValidateSIMDHeavy` | `BenchmarkDecodeStructuredCustom`: valid name/branch-hint/opaque controls; 1 KiB/64 KiB/1 MiB/3 MiB/8 MiB payloads where valid; long strings and dense/nested records; AST and byte-backed entry points; budget rejection in separate rows. |
| B4 `src/core/compiler/wasm` | `BenchmarkDecodeSingletonTypes`, `BenchmarkTypeMetadataDecode`, `BenchmarkGCTypeDecode`, `BenchmarkGCTypeValidate`, `BenchmarkGCFlatTypeLookupManyGroups` | `BenchmarkDecodeTypeGroups`: 0/1/10/1000/100000 groups; implicit, explicit, mixed; vary explicit-group width within the total type quota. Report reserved budget and retained slab capacity. |
| B5 `src/wago` | `BenchmarkCompiledSnapshotCopy`, `BenchmarkFirstUseCompiledImportedModule`, `BenchmarkCompileSmallScalar`, `BenchmarkCompileSmallScalarCore3`, `BenchmarkRuntimeInstantiateSmallScalar` | `BenchmarkCompiledFirstUse`: hand-built/compiler/codec, tiny/import-heavy/nested metadata, cold/warm/parallel. `BenchmarkCompiledSnapshotPreflight`: small valid and bounded rejection. Extend snapshot-copy beyond its current tiny scalar fixture. |
| B6 version/plugin manager packages | Active-state, scope, and concurrency tests; no dedicated benchmark found. | `BenchmarkActiveInstallationRead`, `BenchmarkVersionMutation`, `BenchmarkPluginEnvironment`: current/legacy/read-only, serial/parallel read, writer contention, local/global scope, all profile/build choices. Track filesystem writes: zero for ordinary reads. |
| B7 `internal/managedrelease`; Windows replacement package | `BenchmarkSelectedReleaseLease` | `BenchmarkReleasePublication`, `BenchmarkUninstallRecovery`: no marker/live worker/stale marker/completed phases; separate file preparation from timed publication. Count descriptors and publication/cleanup operations. |
| B8 process harness | `bench/startup` work twins and startup method | A/B manager launcher `--version`, active-version query, direct payload launch, and runtime startup for json-as, fib, and sha256. Use temporary managed installations. Add a noop twin only when separating startup from guest work; verify the workload result and positive work-minus-noop delta. |
| B9 `src/wago` | `BenchmarkWriteCompiledImportedModule`, `BenchmarkMarshalCompiledImportedModule`, `BenchmarkReadCompiledImportedModule`, `BenchmarkUnmarshalCompiledImportedModule`, `BenchmarkFirstUseCompiledImportedModule`, `BenchmarkMarshalCompiledSmallScalar`, `BenchmarkUnmarshalCompiledSmallScalar`, `BenchmarkMarshalCompiledStructuralReferences`, `BenchmarkUnmarshalCompiledStructuralReferences` | `BenchmarkValidateFuncSignature`: indexed/non-indexed, scalar/reference/v128/multi-result; zero temporary allocations for validation. Extend streaming write to 0/10/1000/10000 signatures plus the existing 1600-import fixture; `io.Discard` and a reusable buffer; check byte counts/round trips outside timing. |
| B10 `src/wago` | `BenchmarkCompileImportMetadata`, `BenchmarkCompileImportedWorkers`, `BenchmarkRuntimeInstantiateUnrelatedImports` | `BenchmarkCompileRegistryScaling`, `BenchmarkRegisterImports`: independent registry sizes 0/10/1000/10000 and module import counts 0/1/8/64/1000; batch/one-at-a-time mutation; cold generation/warm generation; concurrent compile. Keep fixture setup outside timing. |
| B11 `src/wago` | All `BenchmarkRuntimeInstantiate*` and `BenchmarkInstantiate*` rows in `src/wago/bench_test.go` | `BenchmarkInstantiateImportOwnership`, `BenchmarkModuleImportViews`, `BenchmarkImportIdentityIndex`: public/private paths; all import kinds; hooks absent/no-op/reading metadata; cold/warm reflection; 0/1/4/8/16/64/1000 imports. Extend around the selected inline capacity. |
| B12 `internal/jsonstrict` | Strict JSON correctness fixtures; no benchmark found. | `BenchmarkValidateTypedJSON`, `BenchmarkJSONDescriptorCache`: 4/16/64/256 fields, sparse/dense keys, repeated objects, ASCII/Unicode, embedded fields, maps/RawMessage; cold/warm/parallel/cache churn. Include actual active-state, registry/catalog, and lock shapes. Bound churn fixture setup too. |
| B13 `cli/internal/project` | Lock validation/round-trip fixtures; no benchmark found. | `BenchmarkEncodeLock`, `BenchmarkDecodeLock`: 0/1/10/100/1000 plugins within configured quotas; sparse/dense dependency edges; small/large RawMessage; output bytes and real graph validation. Measure graph encode separately from atomic file publication. |
| B14 checked `src/core/runtime/gc` | `BenchmarkCheckedStructGet` | `BenchmarkCheckedRoots`, `BenchmarkCheckedConstructors`: 0/1/8/64/1024 roots/values plus a bounded large outlier; nil/empty, reference/scalar values, cold/warm scratch, re-entry, Tiny/Throughput. Separate conversion overhead from collection/constructor cost. |
| B15 native GC and `src/wago` | Native `BenchmarkArrayConstructors`, `BenchmarkGCRootClassMatrix`, `BenchmarkGCCollectionMatrix`, `BenchmarkTinyStepPersistentRoots`, `BenchmarkCollectorTelemetryOverhead`; Wago `BenchmarkGCNativeFrameRootEnumerationWidths`, `BenchmarkCompiledGCFrameRootsSafepointByIDDense` | Compare plain and `wago_gcstats` builds in separate runs. Report roots scanned, GC work, pause distributions, managed bytes, and pause plus first operation that consumes deferred work. |
| B16 `internal/regularfile`, `internal/filelock`, `cli/installer` | Existing read/lock/cleanup tests; no direct benchmark found. | `BenchmarkReadRegularFile`: 0 B/4 KiB/64 KiB/1 MiB/8 MiB, stable and atomic-snapshot modes, oversize rejection. `BenchmarkFileLockAcquire`: uncontended/1 retry/10 retries/cancelled, allocation count per wait. `BenchmarkCleanupProtectedPaths`: depths 1/8/32/128, width and protected aliases; relationship planning and real cleanup separately. |

**Whole-runtime regression coverage**

Run these after the targeted changes pass, then once for the full patch stack.
The `bench` directory is a separate Go module; `go test ./...` at the repo root
does not cover it.

- Corpus stages: `BenchmarkDecode`, `BenchmarkValidate`, `BenchmarkCompile`,
  `BenchmarkCompileFull`, `BenchmarkInstantiate`, and `BenchmarkExec`. Include
  small scalar/control modules, json-as, sqlite3, ruby, and esbuild for the stages
  each supports. Check `bench/corpus/manifest.json` and file hashes; missing
  required data is a reported coverage gap, not a passing skipped row.
- Worker controls: `BenchmarkValidateWorkers`, `BenchmarkCompileWorkers`,
  `BenchmarkCompileFullWorkers`, `BenchmarkCompileMultiModuleThroughput`, and
  `BenchmarkExecParallel`. Exercise serial defaults and bounded parallel work.
- Execution controls: `BenchmarkExecCallOverhead_wago`,
  `BenchmarkExecHostRoundtrip_wago`, `BenchmarkJsonAsSerialize_wago`,
  `BenchmarkJsonAsDeserialize_wago`, and `BenchmarkSqliBenign`, plus the corpus
  memory, branch, recursive-call, and table cases. Check exact workload results.
- Backend controls: native railshot small/control/import/GC metadata compilation,
  with `BenchmarkRailshotCompileSmallScalar`, `BenchmarkRailshotCompileMediumControl`,
  and `BenchmarkGCLayoutHeavyCompile` on amd64 and the matching native arm64
  compiler controls. Record generated native-code size.

**Measurement method and proposed acceptance gates**

Use prebuilt baseline/candidate benchmark binaries on the same host, toolchain,
build tags, bounds mode, and environment. Record CPU, OS/kernel, Go patch version,
commit IDs, affinity, power/thermal state, `GOMAXPROCS`, GC settings, sample count,
fixture sizes/hashes, and all flags. Warm builds before timing. Alternate A/B
order. Run one benchmark process at a time.

Use `GOMAXPROCS=1` and `-cpu=1` for serial comparisons, then separate concurrency
runs at 2 and 4 CPUs where the host supports them. Scout large cases with
`-benchtime=1x`; investigate any single compile taking over 30 seconds. For small
cases use about 200 ms per sample and at least 10 samples per side. Use one or a
few iterations for large cases with at least 10 independent samples. Split long
sweeps into bounded groups. Compare with `benchstat`, including per-row deltas
and uncertainty; a geomean alone can hide a bad row.

These are proposed budgets for this repair effort, not existing universal repo
rules or measured results:

| Area | Acceptance target |
|---|---|
| Correctness | All required cases pass; no new skips, weaker malformed-input checks, silent resource errors, or hidden race failures. |
| Established zero-allocation calls | Keep zero `B/op` and `allocs/op` after normal warmup. This includes the scalar add-one/prepared controls and no-op cancellation watcher. Do not impose zero on every API: the current scalar synchronous host-call test allows one scoped-handle allocation. |
| Allocation reductions | Eliminate the identified temporary signature work; one internal resolved import map; no registry-size-dependent copy on repeated importless compile; no singleton storage for explicit-only groups; no allocation per lock retry. Preserve public ownership copies. |
| Call timing | Investigate a repeatable slowdown above 3% in the B1 and whole-runtime call controls. Keep the scheduler transition as the baseline; do not target the audit's pre-transition 9.38 ns Engine.Call result by undoing it. |
| Compile, codec, instantiate, GC | Investigate a repeatable slowdown above 5% for unchanged semantic work, and any clearly measured regression even below that threshold. Correctness-only costs need an explicit measured justification. Targeted performance fixes must show the intended time/allocation/scaling gain. |
| CLI and filesystem | Separate OS noise from algorithm cost. Flag repeatable startup/operation regressions above 5%; report cancel/recovery tail latency and retry work separately. |
| Retained memory | Set explicit cache/scratch capacity bounds in each patch. Memory must plateau under repeated operations/type churn, and module/collector close must release its owned state. Report any peak RSS/heap/native-memory increase; investigate repeatable growth above 5% plus measurement noise. No unbounded cache or history retention. |

A noisy or underpowered comparison is inconclusive, not “no regression.” Repeat
the affected rows on a quiet host when the uncertainty includes the budget.
Timing limits do not override correctness; record the cost and optimize it without
removing the safety property.

Collect `alloc_space`/`alloc_objects` profiles to locate allocation traffic, and
`inuse_space`/`inuse_objects` after a completed workload and GC to locate retained
Go memory. Profile separately from benchmark timing. Use a standalone process
for peak RSS so compiler builds and the full corpus loader do not dominate it.
Track executable mappings, native stack/linear memory, managed GC memory, open
descriptors, and goroutine counts separately. Go `B/op` excludes mmap memory.
Run repeated create/use/close and large-then-small workloads to expose retention.

For a small-device check, use a native low-end Linux/arm64 host and a declared
process/container memory budget that fits the selected workload. Run a practical
subset at 64 MiB and 128 MiB Go heap targets where feasible, with RSS measured
separately. `GOMEMLIMIT` is not a process/native-memory cap. Record bounded,
diagnostic resource rejection separately from successful throughput. Also compare
stripped manager, runtime-standard, and runtime-minimal binary sizes.

**Command templates**

The following use existing benchmark names. Repeat from isolated baseline and
candidate checkouts, store raw results under `/tmp`, and compare matching rows.
Do not run independent benchmark jobs in parallel. For final A/B results, build
each package test binary once and alternate executions using the equivalent
`-test.bench`, `-test.benchmem`, `-test.benchtime`, and `-test.cpu` flags.

```sh
# Core call controls (Linux/amd64).
GOMAXPROCS=1 go test ./src/core/runtime -run '^$' \
  -bench '^Benchmark(CrossBoundaryCall|HostCall|LinearMemoryAccess)$' \
  -benchmem -benchtime=200ms -count=10 -cpu=1

# Public call and artifact controls.
GOMAXPROCS=1 go test ./src/wago -run '^$' \
  -bench '^Benchmark(InvokeAddOne|PreparedInvokeAddOne|InvokeHostFuncDirect|InvokeCrossInstanceDirect|WriteCompiledImportedModule|MarshalCompiledImportedModule|ReadCompiledImportedModule|UnmarshalCompiledImportedModule)$' \
  -benchmem -benchtime=200ms -count=10 -cpu=1

# Type decode storage. Start with a single-iteration scout.
GOMAXPROCS=1 go test ./src/core/compiler/wasm -run '^$' \
  -bench '^Benchmark(DecodeSingletonTypes|TypeMetadataDecode|GCTypeDecode)$' \
  -benchmem -benchtime=1x -count=10 -cpu=1

# Native GC controls; checked-wrapper benchmarks must be added separately.
GOMAXPROCS=1 go test ./src/core/runtime/gc/native -run '^$' \
  -bench '^Benchmark(ArrayConstructors|GCRootClassMatrix|GCCollectionMatrix|TinyStepPersistentRoots|CollectorTelemetryOverhead)$' \
  -benchmem -benchtime=1x -count=10 -cpu=1

# Run this block from the separate bench module.
cd /home/jtenner/Projects/wago/bench
GOMAXPROCS=1 go test . -run '^$' \
  -bench '^Benchmark(Decode|Validate|Compile|CompileFull|Instantiate)/(json-as|sqlite3|ruby|esbuild)$' \
  -benchmem -benchtime=1x -count=10 -cpu=1
GOMAXPROCS=1 go test . -run '^$' \
  -bench '^Benchmark(ExecCallOverhead_wago|ExecHostRoundtrip_wago|JsonAsSerialize_wago|JsonAsDeserialize_wago|SqliBenign)$' \
  -benchmem -benchtime=200ms -count=10 -cpu=1
```

Repeat relevant Wago/runtime/corpus rows with `-tags wago_guardpage` and
`WAGO_BOUNDS=signals` on supported native products. Run `wago_gcstats` separately
from normal performance measurements. Use `-race` for correctness, never for
release performance comparisons. Ensure selectors actually emit every intended
benchmark row; Go can exit successfully with no matching benchmark.

For process startup, use the method in `skills/startup-latency-bench/SKILL.md`
and the checked-in `bench/startup/twins` fixtures: prebuilt A/B binaries,
`hyperfine -N --warmup 5 --min-runs 30`, explicit cold application-cache state,
and a separate warm-cache run. Report OS page-cache state rather than calling a
warmed page cache cold. Use local JSON output paths; publishing website benchmark
data is not part of these repairs.

**Correctness and platform release gates**

Start with the touched packages and their new diagnostic regressions:

```sh
go test ./src/core/compiler/wasm ./src/core/compiler/frontend \
  ./src/core/runtime/... ./src/wago ./internal/jsonstrict \
  ./internal/regularfile ./internal/filelock ./internal/managedrelease \
  ./cli/internal/project ./cli/manager/internal/version \
  ./cli/manager/internal/plugin ./cli/manager/internal/self/replace ./cli/installer \
  -count=1

go test -race ./src/wago ./src/core/runtime/... \
  ./internal/jsonstrict ./internal/filelock ./internal/managedrelease \
  ./cli/internal/project ./cli/manager/internal/version ./cli/manager/internal/plugin \
  -count=1
```

Before merge, run `make test`, `make test-concurrency`, `make test-guard`,
`make test-corpus`, `make test-semantic-corpus`, `make spec`, and
`make spec3-signals` on their supported products. Run bounded local parser and
codec fuzz checks using the existing harnesses for the changed admission paths.
Run the normal lint/docs checks and compile the corrected telemetry examples.
Verify runtime CLI build tags as required by `make test`.

Use the CI-pinned Go 1.22 line and test timer/publication behavior on the current
supported development toolchain too. Keep TinyGo/no-cgo checks (`make tinygo-build`
and `make tinygo-test`) for the changed shared packages and runtime products.
Use native Linux amd64/arm64, Darwin amd64/arm64, and Windows amd64/arm64 CI
coverage. Native Linux arm64 ordering and native Windows recovery/lease tests
are required; QEMU and cross-builds supplement those tests.

Use the repository's WABT bootstrap and pinned `wast2json` 1.0.41 for the Core 3
gate. Run socket-dependent tests in an environment that permits local sockets.
The prior audit's socket and WABT failures describe its environment; do not assume
they are present here, and do not count them as passed coverage. Report any
remaining blocker with the exact failing command and reason.

Each repair report should contain the fixed behavior, test result, before/after
time/bytes/allocations, retained/native-memory notes, and tests not run. The final
report must map all audit items to commits and benchmark rows, with no unresolved
merge blockers or hidden skipped checks.
