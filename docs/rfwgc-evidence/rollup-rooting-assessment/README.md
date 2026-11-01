# Rooting roll-up assessment — 2026-11-01

Read-only assessment for the combined ready Snap-port PR. **No final INCLUDED,
SUPERSEDED or INCOMPATIBLE disposition is established here.** Recommend integrating
all four verified topic fixes, with the 997/1026 semantic reconciliation below.
None is moot merely because the port's narrower tests passed. Parent owns final
port-log dispositions after integration and final-head gates.

## Pins, authority and qualification limits

- Initial production: `98939b4dd875efbcda6dd53ba769ad25dec2b4f1`.
- Current comparison production, supplied during assessment:
  `14f1f7696344b7d24e9f968b02a0b6f2cf747651` (mature descriptors, exact-span
  transactions, descriptor-driven reclamation). All recommendations below use it.
- Owner-order commit: `e4870a1d3` (full identity in `sources.json`). The November 1
  order in `docs/rfwgc-port-log.md` supersedes the older exclusion in the /tmp
  resumption brief and stage4 mirror. Read the launch addendum, local AGENTS,
  stage3-status, stage4-mirror/native/native-review/mature handoffs, stage5 mature
  README, and TinyGo DIAGNOSIS. No historic exclusion is reapplied.
- Observed upstream main/local origin main:
  `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e` (GitHub branch API checked).
- Common port/topic base: `9713878ab452848f7eb7549a90be94b938a898be`.
- 1026/1027 current-main topic base:
  `27abf4470e1137c18233d246e05dee6fb76ef266`.

| PR | Freshest verified semantic input selected provisionally | Recommendation |
| --- | --- | --- |
| 997 | `e2736f0f37e614218ed7da7ff4040cf1697431c3` | Integrate full source-engine extern/caller-root fix and regressions; unresolved supplied-hash provenance remains explicit. |
| 1003 | `9666ec48e18cb2f7a77c188dc52f093770859f26` | Integrate the current extent-validation topic and regression; distinguish the retired historical optimization stack. |
| 1026 | `4101e75a3b915ce8b0217cd356b9837339c54ee3` | Integrate domain-reentry semantics/tests jointly with 997; do not overwrite 997's stronger walker. |
| 1027 | `29e59c6413e502b000d00e40836350190db1f3c1` | Integrate collector-selection predicate and tests, plus actual exception-transfer gates. |

No benchmarks, timings, tests, production edits, integration, branch/worktree
changes, push, PR publication, closures or delegation were performed. This is
source/diff/evidence assessment, not execution verification. All ordinary,
wagodebug and wago_regalloccheck integration gates remain required. Documentation
and saved read-only diffs cannot affect shipped runtime/compiler performance.
The final consolidated three-Daybreak-plus-Claude review is still mandatory;
this assessment is not that fan or an independent final-head review.

## Missing supplied hashes and semantic freshness

Neither `421119d1d` nor `11bcf23fd` resolves as an object in this repository.
Reasonable recovery searches completed without modifying any refs or sibling
branches:

1. `rev-parse`/object lookup, all refs and registered worktrees; all common
   reflogs and per-worktree HEAD reflogs under `/home/dev/Projects/wago/.git`.
2. `git cat-file --batch-all-objects --batch-check` examined 127125 objects,
   including unreachable objects and alternate stores; neither prefix exists.
   Reflog and object results are saved alongside this report. There is no local
   dangling object with either prefix to recover.
3. Read related checkout refs: `fix/gc-extern-local-roots`,
   `t3/fix-extern-root-rebase`, `t3/rebase-pr-997`,
   `perf/gc-abstract-array-length`, `t3/pr1003-refdesc-extent-validation`, and
   the two historical PR1003 sweep worktrees. Most cleanup scratch worktrees
   have since reset to `fc6af651c15e286409d9cc117c94181e9ee50ff3`; their current
   HEAD is not proof that the missing revision existed.
4. Searched relevant /tmp briefs and documentation in the main repository,
   cleanup coordinator, extern rebase and historical 997/1003 checkouts. No
   supplied-prefix evidence was found there.
5. T3 exact-hash searches found only the current owner's unresolved 997 mention;
   the 1003 exact search returned nothing. Read and paged the messages of
   [Wago PR cleanup wave](t3-thread://v1/mcp:e0fe8ed9-661a-48b3-9911-8ba4ed64a3ef).
   Its reports confirm the extent-only 1003 transplant and published 997
   `e2736f0f3`, not either supplied prefix. T3 active-thread search is not a
   universal archived-message search; absence there is not proof of nonexistence.
6. GitHub commit lookup of both prefixes in wago-org/wago returns HTTP 422
   "No commit found". jtenner/wago returns HTTP 404 (repository/access absence,
   not proof about that repository's historical objects). Exact responses saved.

**Next source needed:** the owner/author's full SHA plus the originating clone
path, reachable remote ref, or a Git bundle containing the commit and parents.
A patch with its baseline and provenance would permit semantic assessment but
would not verify the missing commit identity. Do not label either unknown hash
as integrated, equivalent or obsolete. No shorter-hash substitution was made.

Verified candidates were compared by actual range-diff, not timestamps:

- 997 local `c7cc340a36d96685151ca4afc2482513e7b7cf28` on
  `6f4696ebb5aee0741e0825a9857421c307065c21` has the same core production fixes
  as the published sequence. Published e273 removes obsolete artifact/codec
  assertions, updates tests to source-engine coverage, and retains all substantive
  ownership/relay fixes. Local `e94701b955eba1d5a8ca658a939b21410a4fd726` is an
  older stack lacking the final foreign-relay coverage. Both full range-diffs
  are saved. Prefer verified published source-engine semantics.
- 1003 local `5b1943728e428a39ca154fc359b589be21cc7a6d` includes eight older
  changes: abstract array cast/length fusion, kind encoding, incremental-kind
  correction, disabled-stub control, unrelated-struct isolation, object extent
  validation, byte-space load and decoder checks. The published head instead
  contains only extent validation. Saved range-diff exposes this deliberate
  scope change; the stale PR title/body still describes the optimization.
  Cleanup-thread instructions explicitly requested the extent-only transplant
  from `f5a2c72417272b87c2aa641096c04f22fede041f`.

`pr*-published.diff` records **port merge-base..published-head** (so 1026/1027
include upstream prerequisites); `pr1026-topic.diff`/`pr1027-topic.diff` record
only their final topic commits. `sources.json` records exact bases and ancestry.
This distinction prevents accidentally bundling unrelated upstream fixes as if
part of the rooting topics.

## PR 997: exact extern and native caller roots

Carry the cumulative base..head change, including its test corrections. The
substantive production commits are:

- `75ad94720bad9dc92e1f47bd5ae50873afd159e8`: exact roots for allocating extern
  conversion products; source frame admission, checked private-table ownership,
  and collection fallback when an exact map is unavailable.
- `ff8417433b8175b8d73cb147a16d1a7e9b37570b`: exact caller-context ownership,
  entry return-slot validation and shared-code owner selection.
- `418352c39fba6eb731335521ae9bb1127d3d2bae`: caller continuation through native
  register adapters.
- `bb1724697b4989145f5995bcd54f590b931d5003`: retain scalar domain boundaries.
- `2fef3894a0ce33febf8bc255fc830803885605ab`: foreign/collector-free relays,
  zero-root unwind maps, retained imported-owner graph and host suspension.
- Preserve final test-only `e2736f0f37e614218ed7da7ff4040cf1697431c3` rather than
  restoring retired public artifact paths. Full ordered stack in pr997-commits.txt.

Exact hunk groups to retain:

1. `api.go` exactNativeGCRoots includes AMD64/ARM64 extern products; nativeUnwind
   builds RootNone return maps without granting GC allocation authority;
   empty extern plans remain source-admitted only. `code_mapping.go`,
   `gc_frame_roots.go` and `instantiate.go` must agree on those predicates.
2. Architecture frame planners admit private non-exported extern tables but
   reject external aliases and unsupported foreign proper-tail shapes. Keep
   the bounded/noncollecting fallback; do not broaden it opportunistically.
3. `shared/gc_frame_roots.go:UnwindOnly`, amd64 compile direct-entry predicate,
   amd64/arm64 dynamic host-call guards and logical callsite-index restoration
   keep root metadata aligned across the mutually exclusive machine branches.
4. `reference_store.go`: separate collecting owner from physical frame owner;
   skip foreign collectors' compact slots while continuing their physical
   return chain; authenticate the saved context in the 64-byte wrapper (AMD64
   +8, ARM64 +0); preserve shared-code exact instance identity; traverse retained
   import-owner graphs without acquiring foreign-store locks; filter checked
   mirrored tables and noncompact foreign anyrefs.
5. Adapter records: 16-byte physical bounds/alignment; AMD64 outer return at +8,
   ARM64 +0; nested-adapter rejection; known entry landing only; missing foreign
   map is an error, not arbitrary truncation of root traversal. RV64 keeps its
   architecture-specific validated boundary.
6. `basedata.go:TrapStackReentry`, helper-root entryReturnSlot and host activation
   push/pop fields/counter/signatures across all architecture files;
   `host_execution.go` suspends the root invocation's domain when required,
   including shared atomic wait. Retain the physical root Engine selection.

At mature target the root planner/walker/host files still lack these changes;
only amd64 compile.go has an intervening port change among 997's production
paths. Its directPrepared edit must preserve the port's lifetime-lowering work.
Mature descriptors and exact object tracing cannot discover a frame the root
producer never publishes. Therefore no SUPERSEDED rationale exists.

Retain all new tests from gc_extern_local_roots_test.go,
gc_extern_frame_owner_test.go, gc_extern_reentry_roots_test.go,
gc_foreign_frame_roots_test.go and gc_scalar_domain_adapter_test.go, plus helper
and host-plugin fixture signature corrections. They cover live/vector locals,
foreign anyrefs, many locals, checked tables, missing-map fallback, no-heap Tiny
profile, forged words, void imports, shared-code caller identity, entry-slot and
adapter bounds, foreign-tail fallback, direct/indirect/ref scalar relay,
revisited domains/stores, collector-free map rejection and host collection.

## PR 1026: overlapping mechanism, retained independent obligation

Carry topic `4101e75a3b915ce8b0217cd356b9837339c54ee3`, not its whole
9713878ab..head range. Its changes are three enable flags plus adapter continuation
and regressions. It does not replace 997's identity, bounds, foreign relay or
extern admission fixes.

Resolve jointly after 997:

- Both architecture gcHelperRoots functions set `domainHostRoots = true` in
  both topics. Keep one assignment and 997's foreign-anyref/entry-slot handling.
- `gcCollectFrameRoots` differs: 997 sets `compiledArchitecture != 4`, 1026 sets
  `true`. Carry 1026's unconditional domain-host discovery intent, including
  RV64; keep 997's runtime activation-counter guard restricted as written
  (`compiledArchitecture == 4 || counter != 0`). Verify RV64 behavior rather
  than assuming its omission is harmless.
- Do not replay 1026's `resolveReturn` goto over 997's bounded adapter/owner loop.
  It would lose newer exact ownership checks. Map its continuation obligation
  onto 997's loop and retain distinct 1026 acceptance tests.
- Keep TestGCDomainHostRootsAcrossReentry's all-native source fixture, replacing
  the former RV64-only test. Keep TestGCScalarAdapterPreservesNativeCallerRoot.
- Adapt TestGCNativeFrameRootsScalarAdapterContinuation: its `owner` has no
  compiled wrapper and its synthetic roots have no Engine/physical stack.
  997 returns early at `owner.c == nil`, and requires a nonzero bounded stack
  before reading the adapter. Thus unmodified test is incompatible with the
  stricter walker. Construct a real retained compiled owner, valid code map and
  bounded native stack/entry record, preserving both AMD64/ARM64 saved-return
  layouts and the **in-place 42 -> 43 slot rewrite** assertion. Do not remove
  bounds checks or delete the regression to make the merge green.

This is an integration-fixture conflict inferred directly from the two source
paths, not a newly executed failing production finding. 997's broader mechanism
may implement most of 1026, but whole-PR SUPERSEDED needs the RV64 flag semantics
and these tests passing on the integrated head. Recommendation: account for
1026 as integrated semantics plus retained tests once gates pass, not as moot.

## PR 1003: current extent guard, historical fusion explicitly separate

Carry `9666ec48e18cb2f7a77c188dc52f093770859f26` exactly in semantic scope:
`alloc.go:refDescPtrBytes` selects nursery (including survivor), shared old/large,
or Tiny backing from a validated space; rejects unknown space, size < HeaderSize,
and `uint64(off)+uint64(size) > len(span)` before slicing/header decoding.
Return the existing invalid-object diagnostic. This prevents malformed metadata
from panicking during dynamic type reads. Current mature target still directly
calls `c.bytes(r)` before its header read, so descriptor occupancy did not make
this guard redundant. Old/large still share throughput.mem; no new descriptor
mapping is necessary. alloc.go is an actual mature overlap, though this helper
was not rewritten by the mature commit. Preserve all new transaction code.

Retain TestRefDescRejectsInvalidObjectExtent, covering nursery/survivor/old/large/
Tiny, all invalid space tags, short/empty/oversized extents, offset overflow and
restoration. Extend qualification with region-mode live extents and survivor copying, checked
handles, malformed type IDs and valid post-promotion/post-reclamation references.

Do **not** describe the old abstract-array fusion as already on main/port:
`gc_abstract_array_len.go` is absent, the native view has no SubtypeEncoding marker,
and subtype_interval.go uses untagged timestamps at the pinned target. Existing
`emitNativeFinalCastArrayLen` is the defined-final-type fusion, not the abstract
HeapArray mechanism. Consequently the older kind-bit repair is moot only relative
to this absent encoding, not evidence that the historical optimization landed.

The historic eight-commit stack is not the current published fix and cannot be
silently resurrected from a stale PR body. If the owner intended that entire
older optimization in addition to the current verified topic, this assessment
has **not** approved its integration: recover the supplied full local head and
reassess its complete native-view/subtype/compiler changes against the current
ABI. Its tests for appended roots, unrelated structs, disabled shared stubs,
legacy encoding, null/trap order and decoder bounds must accompany any such
implementation. Do not final-bucket historical fusion as SUPERSEDED or INCLUDED
without that evidence. The current extent-only topic remains compatible.

## PR 1027: scalar exception collector identity

Carry `29e59c6413e502b000d00e40836350190db1f3c1` only:
`preferredGCCollectorFromImports` defines exceptionTransfers as staged EH OR
owned exception references, and considers scalar InstanceExport owners when
that predicate holds. Existing imported-global and same-store/conflicting-
collector checks remain. Mature target retains the old signature/owned-reference
predicate, so this fix is not superseded by root enumeration or region policy.
Selecting the wrong collector precedes all root-walking correctness.

Retain TestPreferredGCCollectorIncludesScalarExceptionImports: ordinary scalar
EH consumer chooses provider collector; foreign store and conflicting collectors
are rejected. Keep TestPreferredGCCollectorIgnoresReferenceFreeFunctionImports
as the non-EH control. Add/retain actual scalar-import throw/catch/catch_all_ref
transfer with live GC payload and forced collection; test same collector versus
cross-store/conflicting owners, consumed/dropped exception aliases and post-trap
reuse. Run with 997/1026 enabled: selection and enumeration are complementary.

## Main prerequisites and serialized integration order

`pr1026_1027Prerequisites` in sources.json enumerates nine commits already on
upstream main but absent by ancestry from mature port. These are #1015, #1016,
#1023, #1024, #1025, #1022, #1029, #1028 and #1031. Do not attribute them to
1026/1027 or claim the port already contains them. In particular #1025 custom
instruction GC maps and #1029/#1028/#1031 exception/root handling affect joint
qualification. Parent should first choose and record a current-main integration
baseline, resolving the port against those landed semantics; if retaining the
older baseline, explicitly port relevant prerequisite hunks and tests rather
than blindly copying both PR merge-base ranges. No baseline update was done here.

Proposed serialized execution:

1. Preserve committed mature 14f1f769, reconcile landed-main prerequisites and
   pin the resulting integration baseline. Obtain missing local objects or
   explicitly record provisional use of the verified published semantic inputs.
2. Integrate 1003's narrow refDescPtrBytes guard/regression; it is independent
   of frame ownership and preserves the mature allocator.
3. Integrate 1027's collector identity predicate and regression.
4. Integrate 997's cumulative source-engine fix, then reconcile 1026's flags,
   stronger adapter semantics and all three regression obligations as above.
   Commit this tight root-production/root-enumeration cluster coherently.
5. Serialize collector-side 1019/1030/1020 work under the other assessment's
   detailed plan. No native collector owner remains active after mature commit,
   but this assessment does not grant concurrent integration permission.
6. Preserve and jointly qualify the TinyGo worker's now-committed fix (see
   late evidence below). It is separate: TinyGo
   conservatively scans Go references across OS/foreign stacks; 997/1026 publish
   compact WasmGC roots. Neither repairs or supersedes the other. Reconcile
   stack entry/exit/resume, trap landing and reentry lifetimes together.
7. Run joint correctness, corpus/oracle/platform gates, independent leased
   measurement, then the final combined four-reviewer fan and fix/re-review
   cycle. Record final bucket for every one of the seven PRs in the port log.

Cross-cluster mature conflicts (read actual topic hunks; not full triage here):

- 1019 adds markHighWater and reduces remembered/card cleanup. Preserve stage5
  release/reconciliation order and failed-collection root/card retry evidence;
  no high-water reset may discard a partially queued root. Stage5 retirement
  reconciliation can fail after Eden/card cleanup. Review that exit against
  1019's clearNurseryMarks/clearRememberedMetadata/clearCardLists changes and
  panic/full/minor regressions. Descriptor occupancy does not replace marks.
- 1020 parallelizes promotion payload copies and sweep work. Stage5
  reserves every exact destination and descriptor before publication, commits
  mature admission, then publishes handle destinations. Preserve that serial
  prepare/precommit boundary; workers must not allocate descriptors, mutate
  allocator ledgers or commit handles concurrently without an explicit proof.
  Join/drain before publication or rollback. Reconcile footprint expectations
  with both the mature pointer and worker config/pool fields. Native region
  reservations are not evidence that worker-failure transactionality is safe.
- 1030 changes native_alloc.go:discardNativeStructHandles to retain a separately
  reserved, unpublished array identity/extent when trimming a non-tail batch;
  otherwise preserve dead-charge bump after compaction. Mature alloc.go now
  uses checked LOS transactions; region mode disables native LOS batches but
  default mode still exposes the overlapping-reservation path. Preserve both
  cancel paths, active-array ledger entry and exact physical occupancy; test
  checked/default/region fallbacks, publish versus cancel, collection failure,
  retry and no duplicate retirement. Do not supersede a default-policy defect
  solely because region mode takes a checked fallback.

At start native mature files were dirty; they were only read for ownership.
They are now committed at 14f1f769 and that object is authoritative for collector
comparison. Initial status is retained; all unowned dirt was preserved.

### Later committed evidence observed before handoff

The shared branch advanced while the report was being written:

- `5bfd1817463230f7b9fe1c8c45f9c9cfffc4135c` registers foreign stacks with the
  pinned TinyGo conservative collector, including engine lifetime hooks,
  AMD64/ARM64 registration and deterministic boundary tests.
- `5a173c801ad78996c635fe3714ce81bc9221edfb` renames the patch template to `.go.in`
  so ordinary package discovery does not compile a TinyGo runtime template.
- `8b079dd095d428842e371c566f792485580bc9de` independently reviews/tests mature
  14f1f769; `dd9ea665481aa640ea026f34c1da3b8c747facaa` records that checkpoint.

Read the later mature-review README and TinyGo DESIGN; inspected the committed
registration/template and trampoline hook. The design registers readable foreign
mappings under TinyGo's task lock before thunk publication, scans host and foreign
spans separately, normalizes host bottom from the saved host-SP cell, and
unregisters before unmapping. It does not change the trap landing offsets that
997 reads. The relevant basedata.go, host_execution.go, reference_store.go and
refDescPtrBytes paths have no changes from 14f1f769 through this checkpoint.
Thus the four topic recommendations remain unchanged. These later commits are
not qualified by the mature review's immutable 14f1f769 tests. The locally read
TinyGo qualification-results.txt contained `host-ordinary exit=1` at observation;
that may be initial in-flight evidence (the template follow-up addresses package
discovery), and is neither a final failure verdict nor a green aggregate gate.
Parent must take the worker's completed final-head report, not this live file.

997 adds JobMemory accessor/host activation state but does not edit TinyGo's
trampoline files. This remains a semantic boundary overlap requiring joint
entry/reentry/cancel tests, not a demonstrated textual conflict. No remaining
TinyGo dirt is used as verified source; late commits are recorded distinctly.

## Required post-integration gates

Every execution row below requires ordinary, wagodebug and wago_regalloccheck
where applicable; preserve exclusions explicitly rather than silently calling a
cross-compile an execution pass. Run from immutable final-head archives/probes.
No prior branch CI, narrow port pass or this source review substitutes for them.

- Full collector and Wago/API/root suites, AMD64 and ARM64 backend root/callsite
  tests, plus the named 997/1003/1026/1027 regressions. Run standard/default and
  region policy with actual collection/occupancy assertions. Use small fixed
  Eden and pressured mature/LOS graphs; verify exact values, reachable graph,
  domain identity and zero residual reservations/activations after success/error.
- Preserve native stage4 and mature stage4/stage5 probes: partial/refill/cancel,
  metadata growth, rollback at every destination/precommit boundary, consumed
  cards/persistent roots, repeated required refusal/retry, cross-page LOS and
  high/range writes. Include 1019 reset-panic, 1030 active-array overlap and
  1020 serial/parallel semantic parity, dispatch failure/drain/close cases.
- Native cross-instance relay plus host reentry, borrowed arrays and nested
  children, allocating host results rooted before publication/consumption,
  retained tokens after producer close, shared-code owners, foreign anyrefs,
  EH payloads, tail fallback, cancellation/trap and main/imported/exported exit.
  Check resumed owners remain usable and no domain lease or root is leaked.
- ARM64 actual execution (native or documented QEMU), including saved LR and
  context layout, plus RV64 parked-domain roots/non-EH scalar controls. Darwin
  ARM64 where supported; unsupported targets remain named gaps. Do not port
  AMD64/ARM64 adapter offsets into RV64.
- Source-engine admission and internal same-build cache metadata validation;
  keep retired public artifact rejection tests. Do not restore old artifact
  support or count deleted artifact=true fixtures as current coverage.
- Pinned TinyGo 0.42 toolchain patch verification, unchanged interruption runner,
  forced concurrent GC on foreign stack, normal/trap/cancel/host reentry/resume,
  original host roots and saved registers; full lane all required modes. The
  diagnosed SP/stackTop mismatch remains blocking until the committed fix passes.
- Full root and nested-workspace suites, toolchain-backed conformance, race and
  hardening lanes, lint/generation/script checks, complete corpus execution and
  output/oracle reconciliation. Resolve the recorded interval-register diagnostic
  failures; baseline-red status is evidence, not final gate waiver.
- Independent performance work only under the global lease after correctness
  workers drain: default/region no-GC controls, numeric host dispatch/compilation,
  frame-owner lookup and multi-store reentry, extern-heavy locals, EH imports,
  descriptor validation/casts, promotion/LOS/refill/cancel and worker sizes.
  Observe <=60 C sample-start rule. This assessment makes no timing claims.
- Final fan: three Daybreak plus one Claude, one consolidated report, zero open
  findings, every fix re-reviewed and all gates rerun on the final combined head.

## Read-only sibling cross-check before recommendation

Slipstream source pinned to `82d546679bab4887acfab056f6ad773a969b1e03`.
Inspected `src/jit_gc.goose:gc2_publish_chain`, `gc2_publish_frames`,
`gc2_allocate`, `host_gc_array`/`host_gc_new`, and the committed mirror contracts.
It validates frame bounds, publishes active and suspended parent frame chains
and rewritable exception slots before collection; root publication failure
restores the saved root count. That supports the root-lifetime obligation but
uses a different ABI/ownership model, not Wago's shared-code/collector-local
handle lookup. Do not transplant offsets or infer that it fixes Wago 997/1026/
1027. Its typed/bounded host array resolution likewise does not replace Wago's
refDescPtrBytes validation. No corresponding sibling production defect was
established by this bounded audit; this is not a global safety proof.

Starshine pin `e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1` in starshine-mb:
read docs/wiki/wasm-gc-core-boundary.md and searched src/tests for Wago root/
collector/TinyGo symbols. Its relevant surfaces are model, codec, validator and
authoring, with no equivalent native root walker/collector ownership repair in
the inspected surfaces. It can supply validated fixtures, not runtime-rooting
supersession evidence. Search artifacts retained. No sibling edits or execution.

## Evidence validation

Saved actual diffs, range-diffs, source pins, API metadata and search results;
checked README/JSON whitespace and hash manifest. Raw saved patch context
contains space-before-tab and blank context lines by Git diff design; a broad
whitespace check reports these preserved patch bytes, not source edits. No production tests
were run for this read-only documentation task, so no all-three-mode result is
claimed. The documentation-only exception to measurement applies: these files
are not shipped code, build inputs or runnable test additions. Report commit is
supplied in the handoff to avoid a self-referential SHA.
