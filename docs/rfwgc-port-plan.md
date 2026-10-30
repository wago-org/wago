# Wago region-first GC port: source mapping and implementation handoff

Study date: 2026-10-30. This is a source study, not an implementation or runtime qualification.

## Pinned inputs and scope

- Wago: `9713878ab452848f7eb7549a90be94b938a898be`, branch `experiment/rfwgc-port`, worktree `/home/dev/.t3/worktrees/wago/experiment-rfwgc-port`.
- Slipstream: `/home/dev/Projects/slipstream`, fetched `origin/master` = `db6014de2316f13dd08ea8618d614c28132f2688`. Source was read with `git show origin/master:<path>` / `git grep ... origin/master`, not from the source checkout. All Slipstream paths/symbols below mean this ref unless explicitly historical.
- Parent fast-forwarded the initial Wago worktree from `fc6af651c` to the requested `9713878ab`; this was not a rebase. Mapping worker verified the resulting HEAD, not the earlier operation itself.
- User-supplied AGENTS instructions apply. No filesystem AGENTS.md was found in this worktree or its ancestor chain; Slipstream's tracked root AGENTS.md was read. Its streaming/bounded-input and measurement rules inform this mapping. No Goose code was written and no builds were needed.
- Only the two requested documentation files are delivered. No PR, upstream push, rebase, source checkout mutation, subdelegation, build, or sensitive engine bug fix. Existing dirty `tests/conformance/spec-v1` and `tests/conformance/spec-v2` remain untouched. Upstream backlog cap is unchanged. Ordinary later implementation uses sol; sensitive engine bug fixes belong only to Daybreak Blue. Report discoveries rather than fixing them here.

## Recommendation

Start with an **off-by-default, bounded, function-local lifetime analysis in Analyze mode**, initially AMD64 and one pointer-free scalar struct per straight-line function. Prove aliases through locals/tees and scalar reads, reject every escape/unknown, and report why each function falls back. Analyze emits the existing compiler output unchanged. Then, in a separate checkpoint, let an explicitly enabled consumer extend Wago's existing checked scalar/dead-constructor lowering across local aliases. Preserve allocation charges, traps and safepoints with the existing reservation helpers; do not begin with true allocation removal, frame regions, new collector storage, or whole-module escape inference.

This provides information Wago's adjacent-pattern recognizers cannot currently prove while preserving Wago's allocation-failure and root contracts. The first checkpoint is analysis-only and therefore does not itself promise allocation savings. The second should remove payload population/loads and handle publication only where an existing checked charge supports it. Any stronger elimination needs a separately approved semantic contract and evidence.

## Wago ownership: Go backing and native execution are different axes

| Layer | Concrete source / symbols | Ownership and port consequence |
| --- | --- | --- |
| Checked Go API | `src/core/runtime/gc/collector.go`: `Collector`, `NewCollector`, `unwrap`, `wrap`; `ref.go`; package `doc.go` | Checked references bind owner and generation to raw identities. This wrapper delegates to the native package; it is not a second Go object graph storing each guest object as a Go struct. Preserve cross-owner/stale-reference rejection. |
| Runtime/JIT owner | `src/wago/instance.go`: `Instance.gc` imports **gc/native**; `instantiate.go`: `instanceBuilder.prepareCollector`; `reference_store.go`: `acquireGCCollector` | Instance may own a private collector or acquire a Runtime Store domain shared by linked instances. Collector-free products can skip creation. Do not assume one region/collector per function or per instance. |
| Guest heap backing | `src/core/runtime/gc/native/collector.go`: `Collector`, `NewCollector`, `Close`; `layout.go`: `makeAlignedBytes`; `throughput_alloc.go`: `throughputHeap.Init`, backing growth; `tiny_alloc.go`: `newTinyCollector` | Nursery/survivors and Tiny storage are aligned Go `[]byte` allocations; throughput backing grows with `makeAlignedBytes` and metadata are Go slices. `Close` drops backing references. No mmap allocator was found in gc/native. Host Go GC owns the backing allocation lifetime; Wago's explicit collector owns guest liveness and reuse inside it. Go GC does not trace compact guest references in the bytes. |
| Guest identity / movement | `gc/native/ref.go`, `collector.go`: `handleEntry`, `handles`; `native_view.go`: `NativeCollectorView`, `NativeSpaceView`, `NativeABIVersion = 9` | Raw object word is handle index shifted left one, not a Slipstream ABI2 heap offset. Moving guest payload updates handles. Generated code must reload relocatable backing pointers/counts from a stable view. Never place Go pointers in compact guest reference slots or infer liveness from uintptr fields. |
| Native fast allocation | `src/wago/gc_native_alloc_amd64.go`: `prepareNativeArrayAllocationMode`; `gc/native/native_alloc.go`, `native_alloc_batch_amd64.go`, `native_view.go`; compiler `amd64/gc_struct.go` | Native means trusted generated-code access and reserved allocation batches into the same Go-owned collector backing. AMD64 explicitly supports publishing live structs from reserved batches. This is not an independent OS-native region allocator. |
| Roots and collection | `src/wago/gc_collect.go`: `CollectGC`, `collectGCWithJoint`, `prepareGCCollectionRootsLocked`; `gc_frame_roots_runtime_linux_amd64.go`; compiler `shared/gc_frame_roots.go`: `GCFrameRootPlan`, safepoint/callsite builders | Exact native frames, persistent globals/tables, parked host activations, retained public refs and same-domain instances participate. Collection serializes with native execution, domain mutation and public root state. Borrowed guest storage rejects public collection. Preserve the whole root universe and host reentry behavior. |
| Unsupported root coverage | `src/wago/instantiate.go`: `prepareCollector` | Generic execution without the required exact frame-root slice uses bounded throughput with `DisableCollection`. Atomic-wait helper paths also constrain domain sharing. Lifetime analysis must not weaken these guards. |
| Separate native resources | `src/wago/instance.go`: `ar`, `thunkMem`, `tableDescPtr`, `offHeapPtr` | Executable arenas/table-descriptor mappings have separate owners. Their existence does not establish off-heap guest GC backing. Linear memory and code memory are not region-first object heaps. |

Throughput and Tiny are different collector families (`gc/native/collector.go`: `ProfileThroughput`, `ProfileTiny`). Throughput uses a moving young generation and reusable old/large spans; Tiny is fixed nonmoving incremental mark/sweep. Default nursery is 64 KiB with eligible adaptive growth up to 2 MiB, not Slipstream's fixed mapped window. Keep stress, Tiny, no-collection and moving-nursery configurations as explicit fallback/validation axes.

## Existing Wago optimizations: build on these

- `src/core/compiler/backend/railshot/amd64/gc_struct.go`: `emitFB` dispatches constructors; `tryForwardGCConstructorGet` recognizes adjacent scalar field reads; constructor-if forwarding and `gcStructHasOnlyDirectScalars` gate scalar replacement. It sets `nativeStructAllocDead` and still calls the allocation helper. A result-forwarding optimization is not proof that allocation effects can disappear.
- `amd64/gc_dead_new.go`: `checkedDeadGCConstructorUse`, `gcConstructorFeedsDroppedTree`, `reserveDeadGCStructConstructor`, `reserveDeadGCFixedArrayConstructor`, `finishCheckedDeadGCConstructor`. Immediate drops and bounded (32-instruction) nested postfix trees use checked reservations. Nested results remain real compact references so earlier children stay rooted across later allocations. Dynamic constructors cannot be treated like fixed numeric trees.
- `amd64/gc_dead_group.go`: `tryDeadGCDefaultGroup` admits a constrained function-entry group without local/operand state. It preserves fallback allocation safepoints. `gc/native/dead_struct_batch.go`, `dead_array_batch.go`, `dead_batch_try.go` provide runtime reservation/batch machinery; these are preferable to inventing another allocator.
- `amd64/knobs.go` binds `dead-gc-new` and `gc-native-alloc`; `src/core/compiler/optimization/catalog.go` defines the catalog entries. Use its immutable option snapshot pattern, rather than mutable process globals changed by workers.
- `src/wago/gc_product_analysis.go`: `gcProductAnalysisNeeded` already avoids product work for modules whose validated feature requirements cannot use GC. Preserve this gate and the existing architecture/product fallbacks.
- Existing evidence targets: `amd64/gc_dead_new_opt_test.go`, `src/wago/gc_dead_new_amd64_test.go`, `gc_constructor_forward_amd64_test.go`, `gc_constructor_if_pressure_amd64_test.go`, `gc_dead_group_amd64_test.go`, and `gc_struct_reduction_native_test.go`. Read the native charge behavior and these tests before connecting a new consumer.

## Slipstream lifetime pipeline and collector map

| Component | Source / symbols | What is portable |
| --- | --- | --- |
| Facts production | `src/instructions.goose`: `scalar_prepare`; `facts_schema.goose`: `FunctionFacts`, `ScalarPlanHeader`, `ScalarPlanOwner` | Bounded decode into compact semantic records, explicit work/byte admission, and separate owners for temporary facts and persistent plans. Wago should reuse its validated Wasm model/reader rather than transplant this decoder. |
| Points-to / containment / escape | `src/facts_objects.goose`: `fact_objects_build_containment`, `fact_objects_escape_work`, `fact_objects_exact_operand`, `fact_objects_eligibility_impl`, `fact_objects_dce_impl`, `fact_objects_finalize` | Conservative alias closure, containment edges and MAY escape dominance. Unknown must never become nonescape. General graph precision is a later extension, not required for the first one-object proof. |
| Calls and environment | `src/facts_summary.goose`: call-summary production; `facts_schema.goose` consumer configuration | Summary/configuration identity is part of the proof. Unknown/import/indirect/recursive/tail/EH effects are fallback for the initial port; do not copy optimistic callee assumptions. |
| Diagnostic regions | `src/facts_regions.goose`: `r1_gate`, `r1_candidate`, `r1_finalize`; `facts_regions_report.goose`: `r1_report`, `r1_report_coverage` | Classify before freeze; reporting serializes stored results. Explicit incomplete denominator, initializer exclusion and reason codes are directly useful. R1 is diagnostic, explicitly not allocation/lowering permission. |
| Scalar proof | `src/facts_scalar.goose`: `scalar_prepare_begin`, `scalar_cheap_eligible`, `scalar_finalize_impl`, `scalar_close_graph`, `scalar_plan_admit`, `scalar_plan_seal` | Closed opcode whitelist, single constructor, scalar layout/initializer restrictions, parameter-write restrictions, bounded aliases and liveness, explicit fallback reasons. Constants include 64 local/operand slots, 256 records, 8 KiB payload / 16 KiB owner ceilings; these are source limits, not automatic Wago tuning choices. |
| Replay and lifecycle | same file: `scalar_plan_record`, `scalar_replay_local`, `scalar_replay_reset`, `scalar_plan_release`, `scalar_plan_abandon`; `src/jit_parallel.goose`: `compile_native_scalar_body`, `compile_native_body` | Seal immutable plan records, replay the entire body and identity, not just a hash or selected offsets. Retry baseline from clean attempt state on mismatch; release on every exit. Off bypasses admission; Analyze emits baseline. |
| Parallel compilation | `src/jit_parallel.goose`: private module/reader/scratch, report slots, joined worker publication | Private per-worker analysis, shared sealed metadata and bounded aggregate admission. Parallel configurations up to eight are part of source qualification, not a reason to share mutable proof state. Wago already has `amd64/compile.go`: `compileModuleParallel`; integrate there through existing worker ownership. |
| Runtime collector | `gc/native.goose`: `ng_open`, `ng_allocate`, `ng_admit`, `ng_trace`, `ng_worker`, `ng_sweep`; `native/gc_linux_x86_64.S` | Source uses an mmap/protection-backed ABI2 window, offset references, object-start/mark/card metadata, private tracing work plus shared marking/claims. Algorithms/invariants can guide a later redesign, but its offsets, headers, synchronization and pointer rewrite ABI cannot be copied into Wago's handles and Go backing. |

Region-first is default in the source history at `b3e802899` (documentation qualification checkpoint; default mechanics precede it). Old collector remains the oracle. This does **not** authorize changing Wago's defaults. Owner-provided source results are 97.55% allocation-opportunity elimination and 98.10% nursery capture of allocating functions. They are source results, not Wago predictions or measurements made here; this study did not independently recompute their denominators. Do not equate those broad results with the narrow single-site scalar consumer's coverage.

## Direct ports, Go redesigns, and exclusions

**Directly portable ideas:** conservative proof lattice and explicit reasons; scalar eligibility whitelist; constructor/use provenance through locals; fail-closed budget handling; immutable plans; Off/Analyze separation; whole-body replay; consumer-driven admission; diagnostic coverage accounting; paired oracle measurements. Translate contracts and tests, not Goose syntax.

**Go redesign required:** use bounded Go slices/index records and existing compiler scratch lifetimes; account aggregate memory across compiler workers; keep plan records free of pointers into reset scratch; tie identity to module types/layouts, function body and option snapshot. Keep backing Go owners alive across native use under existing execution leases. Preserve stable handle identity and NativeABIVersion/view refresh rules. Any stack/frame allocation would need its own GC visibility, escape, trap/unwind, tail-call, suspension and host-retention design. Any real region allocator would need stable handle/view integration plus Tiny and cross-domain policies; it is outside the first implementation.

**Nonportable as code/ABI:** Goose scoped data stacks, shared-memory capability types and contained-thread cleanup; Linux x86-64 assembly/syscalls and fixed address offsets; Slipstream ABI2 offset references and forwarding headers; fixed heap partitions; its summary/facts family bitmasks and native page accounting. A shared mark implementation must not use Go atomics over movable/unowned backing or introduce Go-pointer writes into unscanned storage. No collector parallelization is proposed here.

## Current-master mirror assessment

The assessment cursor is `db6014de2316f13dd08ea8618d614c28132f2688`, not source checkout HEAD.

| Source change / issue | Status at cursor and applicability |
| --- | --- |
| Region-first default (`b3e802899`) | Landed source policy. Retain Wago default/off oracle; source qualification does not transfer. |
| B1 eligibility (`861c6e519`) | Landed: `scalar_cheap_eligible` rejects necessary signature failures and scans the complete bounded body for byte `0xfb` before expensive facts. Absence is a safe no-GC proof; embedded immediate bytes are only false positives. Adopt a cheap validated feature/signature gate from day one; Wago already has a module feature gate. |
| B7 consumers (`8eef46cd5`) | Landed: `docs/perf-b7-consumer-facts.md`, `src/facts_schema.goose`, `facts_summary.goose`, `jit.goose`. Off/baseline selects no families/summary; scalar selects only its consumed families. Port the principle, not `0x6cff`. Do not build whole-module summaries for a local consumer. Source qualification includes normalized code/map identity at 1/4/8 workers; reproduce Wago's own identity checks. |
| Canonical metadata ceiling (`88c8c8563`, subsequent qualification docs) | Landed Store-driven canonical metadata growth. Distinct from old-space capacity. Wago has `gc/native/collector.go`: `AddTypes` and `canonical_growth_regression_test.go`, `addtypes_retention_test.go`; compare scenarios in a later cycle, not blind-copy limits. Any discovered engine defect is Daybreak Blue work only. |
| Host boundary (`5a5bee94c`, docs through `db6014de2`) | Landed direct uncontended entry and lazy inflation (`ss_ng_enter_direct`, `ss_ng_inflate`, `N_MUTATOR_WAITERS`), removing unconditional wake on that exit path. This is not a general collector-helper wake fix. Wago's execution/domain/host leases differ; defer ABI transplant. `db6014de2` qualifies the performance claim; do not call this an unqualified universal gain. |
| Old-space ceiling after 18,115 collections | Owner-reported pending issue, not reproduced here. `ng_admit` still searches bounded `[NG_OLD, NG_LOS)` blocks, has a 65,536 run ceiling, and can fail admission. These facts establish bounded capacity, not the cause of that exact collection count. No verified fix for the reported symptom is established at this cursor. Wago has its own bounded heap config, handle and free-span reuse; add long-run plateau/reuse tests if later changing regions. Do not import Slipstream partition limits or claim the same bug in Wago. |
| Inactive helper wake/completion cost | Still present: `ng_worker` skips tracing when worker >= `N_ACTIVE` but always acknowledges epoch and increments/wakes `N_DONE`; `ng_trace` collection dispatch and `ng_sweep` broadcast epoch and wait for `N_HELPERS`, not just active helpers. Pending source optimization remains relevant as a design warning if Wago later parallelizes collection. It does not block local compiler analysis and must not be described as fixed by host entry. |

**Every next cycle:** read-only fetch if appropriate; record old/new source hashes; inspect every newly landed collector/compiler-lifetime/host-boundary change since the cursor; append per-change adopt/adapt/defer/not-applicable decisions and concrete paths to `rfwgc-port-log.md` before implementation. Record a no-change cycle too. Do not silently advance the cursor, assume checkout matches, or merge source fixes wholesale. Pending issues remain open until a landed diff and evidence are inspected.

## Precise next-worker steps

1. Read this map and log, confirm branch/base and existing dirt, then perform the mirror assessment. Do not build for another source-only pass. No source checkout mutations. No rebase absent owner direction/conflict necessity.
2. In `src/core/compiler/backend/railshot/amd64/compile.go`, inspect `CompileOptions`, `compileFunc`, `compileFuncAttempt`, `scratch`, and retry/parallel ownership. Add an explicit per-compilation Analyze option with zero-value Off, using the existing optimization option plumbing (`amd64/knobs.go`, `src/core/compiler/optimization/catalog.go`) only where needed. Avoid a broad public API/CLI rollout in the first checkpoint.
3. Add proposed `amd64/gc_lifetime.go` and `gc_lifetime_test.go`. Analyze validated function bodies with a private `wasm.Reader`; record instruction offsets and typed local/operand origins with fixed limits. Initial candidate: exactly one final pointer-free struct with direct numeric fields, scalar constants/unwritten parameters as initializers, local get/set/tee copies, drop and scalar field reads, no returned object. Numeric scalar result is allowed. Reject branches/loops/calls/EH/tail calls, globals/tables/stores, identity tests/casts outside the explicit whitelist, reference fields, dynamic arrays, unknown/malformed inputs and budget overflow. Track overwritten locals and final uses; all aliases must close before function end. A complete proof is required, not a successful prefix.
4. Seal a small per-function analysis result with function/type/layout/option identity and explicit Candidate/Fallback reasons, site count and denominator-known flag. No source rewrite or guest storage change in Analyze. Reuse Wago's existing validated module body storage; do not add a whole-function IR or import all Slipstream facts families. Bound records, stack, locals, work and worker aggregate bytes; excess always falls back. Release scratch on errors/retries and publish reports deterministically after workers join.
5. Verify Off bypasses scanning and allocations; compare Off/Analyze native code **and root maps**, preserving existing option choices. Include one newly recognized nonadjacent local-alias case, adjacent existing-forwarding control, numeric-only/no-GC control, written parameter/local overwrite, object return/store/call rejection, multiple constructors, unknown opcode, malformed input, boundary limits and forced budget failure. Verify serial/parallel results agree without shared mutable plans. Use focused Go package tests first; no fabricated performance claims.
6. Commit the coherent Analyze checkpoint with exact paths. Only afterward propose the opt-in consumer in `amd64/gc_struct.go` / `gc_dead_new.go` plus a new plan-replay helper. Recheck every opcode/immediate and identity before consuming a plan. On mismatch rebuild baseline using existing retry/reset ownership. Forward scalar values across local aliases using existing compiler storage/spill conventions; keep checked reservation at the original constructor safepoint. Do not simply delete `struct.new` or its root-map event. Existing adjacent optimizations remain the baseline, so measure incremental benefit beyond them.
7. Before that consumer lands, exercise throughput/Tiny, collect-every-alloc, small nursery, disabled collection, forced allocation failure, host reentry and exact roots. Extend the named Wago test families; validate results/trap ordering/allocation charges and GC verification. Any unsupported profile/path stays baseline. Use matched Off/Analyze/On builds/options for compile time, RSS, native bytes and execution only after correctness; no target gain is assumed. Every build must remove its generated artifacts afterward, without deleting pre-existing user artifacts.

There is no blocker to implementing the analysis-only checkpoint. Runtime replacement, exact source metric reproduction, long-run old-space diagnosis and performance qualification remain separate work. Stop and report any sensitive engine defect to the owner for Daybreak Blue; do not fix it in this lane.

## Implemented first slice (2026-10-30)

The Analyze checkpoint is implemented in `amd64/gc_lifetime.go`; **there is no lowering consumer and no runtime allocation elimination**. Existing checked allocation/reservation, traps, roots, nursery/Tiny behavior and backlog cap remain unchanged. The next experiment is a replay-checked consumer, following steps 6–7 above; do not use a diagnostic Candidate as permission to delete a constructor.

Enable through existing selection plumbing:

- Public Go: `wago.NewRuntimeConfig().WithOptimization("gc-lifetime-analyze", true)` (then the existing compile/instantiate API).
- AMD64 backend: `CompileOptions{Optimizations: selection, GCLifetimeReport: &report}` with `selection["gc-lifetime-analyze"] = true` layered on the existing complete selection. `report` is a `*GCLifetimeReport`; Off clears the sink even on an early compile error.
- Existing CLI paired flags: `wago run --gc-lifetime-analyze module.wasm`; `--no-gc-lifetime-analyze` restores Off. The generated configuration schema includes the option. It is experimental and defaults false.
- Human diagnostics: a CLI built with `-tags=wago_codegenstats` (or `wago_profile`), `WAGO_EXPLAIN=1`, and the same opt-in flag. Ordinary builds still perform enabled analysis but do not print compiler stats. Direct backend report sinks work without diagnostic build tags.

Coverage is deliberately narrow: exactly one final, nonsubtyped, descriptor-free struct with at most 32 unpacked direct i32/i64/f32/f64 fields; explicit constant or unwritten scalar-parameter initializers (including copies), or `struct.new_default`. The complete body may use nop, scalar constants, local get/set/tee, drop, scalar struct.get and final end; zero or one scalar result is allowed. Aliases may be overwritten and remaining locals die at exit. Parameter writes are rejected. Numeric arithmetic, reference parameters/results/fields, packed fields, subtype layouts, arrays, branches/loops, calls/tail calls/EH, globals/tables/memory effects, mutation, casts/identity operations and all other opcodes fall back. This is a validated-input compiler analysis, not a replacement Wasm validator; malformed/truncated inputs encountered by the interpreter fail closed. A body with no 0xfb byte takes the negative gate without semantic interpretation, so its no-GC classification does not assert general bytecode validity.

Resource bounds: 4096 expression bytes, 256 interpreted instructions, 64 combined parameters/locals, 64 local declaration runs (including zero-count adversarial runs), 64 operand slots and 32 fields. At most 4096 functions are analyzed/reported per module; the remainder is explicitly Omitted with unknown denominator. The diagnostic pass is serial before worker launch rather than allocating plans in workers: at most 1,048,576 interpreted instructions and 16 MiB of byte-gate input per module, one set of fixed scratch arrays at a time, and bounded report storage. This is an intentional first-slice adaptation of step 4, avoids aggregate worker amplification, and includes functions later omitted by inlining. There is no persistent instruction IR, module summary, replay plan, retry state or shared mutable proof state. Off bypasses this pass and its storage entirely.

Results contain function/type identity, constructor and last-alias-use offsets, counts of instructions/field reads/alias reads/local overwrites, Candidate/Fallback reason and a source-level SHA-256 fingerprint over the complete admitted body, signature, locals, type/field layout and Analyze-v1 contract. This fingerprint is diagnostic only; it is not native-layout identity or a future replay certificate. Fallback Sites is a lower bound until DenominatorKnown is true. Only complete supported bodies and the complete no-0xfb negative gate have known denominators; early rejection and budget exhaustion never claim complete site coverage. No elimination percentage is calculated.

Reproducible compile-overhead benchmark entrypoint (timing deferred to parent):

```sh
go test ./src/core/compiler/backend/railshot/amd64 -run '^$' \
  -bench '^BenchmarkGCLifetimeCompile$' -benchmem -count=10
```

It decodes and validates outside timing, then compiles 32-function modules in serial with identical options/native mapping policy, reports allocations, and pairs Off/On for qualifying aliases, nonqualifying control flow and no-GC bodies. Copy **only** `src/core/compiler/backend/railshot/amd64/gc_lifetime_bench_test.go` into an isolated export of unmodified `9713878ab`; it has no dependency on the new implementation or tests. Its Off case is the baseline oracle and its On case explicitly skips if the catalog option is absent. The copied fixture was compilation-checked on that base. Keep toolchain, environment/default selection and benchmark tags matched. No benchmark timings, RSS claims or runtime gains have been measured in this checkpoint.

## Stage 2 implemented (2026-10-30; baseline 6ceb86221)

The same off-by-default `gc-lifetime-analyze` option now enables a consumer,
not only diagnostics. AMD64 seals bounded complete semantic records, owning
copies of the expression bytes, signature, local runs, selected struct type,
consumed native field/size/alignment layout and immutable codegen policy. It
independently replays the complete analysis/records/context before emission.
Mismatch returns to ordinary body compilation before any output/state mutation.
The diagnostic SHA-256 Identity is absent from the certificate and consumer.
The policy audit rejects diagnostic Identity reads outside reporting/analysis
and hash calls outside the diagnostic analysis function; the existing byte gate
exception remains restricted to the complete-body no-0xfb negative test.

The emitter retains original instruction PCs and the constructor safepoint. It
removes reference-local alias operations and field loads, replaying scalar
constants, default zeroes and never-written parameter provenance. Scalar copies
retain their original source across later local overwrites. Parameter forwarding
uses `pushGCForwardedValue`, recovering current pins/spills after helper calls;
no pre-helper register or deferred operand node survives as a certificate value.
It calls the existing constructor helper with `nativeStructAllocDead`, selecting
`emitNativeDeadStructChargeStub` when runtime admission succeeds. This eliminates
live handle publication and payload materialization on that path. Every slow
boundary retains the existing rooted allocation helper and can materialize an
object. No collector rewrite or changed allocation failure contract is involved.

Compilation admission requires native GC allocation, GC struct helpers, the
complete Stage 1 whitelist, matching staged/source types, pointer-free layout,
and a local function within the module budget. Calls, escaping/unknown values,
control flow, reference fields, packed fields, multiple objects, parameter writes,
EH, inlining, custom instructions, profiling builds and failure-injection builds
remain fallback. Tiny, stress collection and collection-disabled configurations
retain native runtime admission/fallback policy; they do not acquire a new fast
allocator. Off and rejected bodies retain baseline code/root maps; qualifying On
code intentionally changes and remains deterministic at 1/4/8 workers.

Full allocation/charge erasure remains outside the runtime contract. This stage
is checked scalar replacement: charges, nursery pressure and slow-path allocation
remain. `Stats.Allocations` counts charges, and `Stats.LiveObjects` includes pending
handleless dead charges, so neither is a materialized-object count. Stage 2's
portable fixture harness samples the native dead-charge counter across every
invocation, respecting collection-cycle resets, and reports charges, successful
handleless charges and materialized allocations (`charges - handleless`) separately.
For its sole i32 field, layout size is 24 bytes; throughput heap rounding is 32
bytes. Neither number alone measures actual nursery/chunk reservations. Report
observed nursery bump advances and the retained end bump separately, including
chunk reservations; do not turn these into zero-allocation or universal physical
bytes-per-object claims. Nursery allocation uses its own size/alignment machinery;
throughput heap size-class rounding is not an inferred nursery bump measurement.

The exact Stage 1 qualifying/nonqualifying/no-GC expressions are runnable in
`src/wago/gc_lifetime_stage2_amd64_test.go`, with only an export added. This single
file also builds at 6ceb86221; only it is copied for the portable benchmark.
Additional tests cover scalar bit patterns (including signed zero/NaNs), scalar
and reference alias overwrites, persistent roots, small nursery, stress, Tiny,
collection-disabled execution, forced Tiny exhaustion, replay corruption/context
changes and native storage checking. Throughput disabled-collection state is
reset at invocation boundaries by the existing runtime; repeatedly invoking one
allocation is not a valid forced-throughput-OOM test. Existing within-invocation
bounded constructor tests remain part of the focused run.

Timing and broad-suite qualification belong to the parent after this checkpoint.
No Stage 2 benchmark has been run. See the appended log and Stage 2 evidence for
commands, exact accounting observations and portable runner instructions.
