# Stage 3 mirror assessment — 2026-11-01

Documentation-only research handoff for the full Snap region-first GC port.
Initial Wago head: `a3c08365f531eb3c9003e3acf3e0a822c12c31f4`, branch
`experiment/rfwgc-port`, existing worktree
`/home/dev/.t3/worktrees/wago/experiment-rfwgc-port`.
Only this directory is owned by this worker. No runtime source, shared log,
Stage 2 scripts, submodule, branch, PR or push is changed. Documentation cannot
affect runtime performance: these files are not consumed by runtime or compiler
builds. No performance measurement was performed or inferred. Independent
measurement under the global lease remains required for implementation changes.

Read the launch brief and addendum, root AGENTS.md, `../../rfwgc-port-plan.md`,
`../../rfwgc-port-log.md`, Stage 1 evidence README and Stage 2 implementation,
orchestrator, corpus and independent corpus-audit handoffs. Historical
measurement-procedure exemptions in those handoffs do not supersede the current
owner's lease/temperature instructions.

## Decision

Proceed with real region storage plus region-first tracing/reclamation under
Wago's stable-handle ABI. A larger scalar proof pass is not completion of this
mission. Retain the baseline collector as an explicit oracle/fallback, stage
native allocation only after transactional region allocation is correct, and
preserve shared-domain root admission before any destructive collection work.
See [architecture.md](architecture.md) for implementation boundaries and
[correctness-matrix.md](correctness-matrix.md) for required observable gates.

## Source identity and exhaustive range disposition

Slipstream was fetched at launch. All source reads used `git show origin/master`
or `git grep ... origin/master`, never dirty working files. Launch cursor is
`506b08d3d3fa3c8e8b1ce9ac0cb4f30a2463cdc3`; previous assessment cursor is
`db6014de2316f13dd08ea8618d614c28132f2688`. `launch.json` pins identities;
`landed-range.txt` records every intervening commit and changed-file inventory.
`final-fetch.json` records the required single completion fetch and any advance.
The exact changes, including source tests, are retained in `dispatch-trim.patch`
and `wake-join.patch`. These are evidence artifacts, not patches to apply to Wago.

| Commit | Landed behavior and decision |
| --- | --- |
| `cc48b98eaa147966a2a9ca8ecbc7b48bb51fef6d` | **Adapt principle.** `NG_PARALLEL_BYTES=65536`; minor tracing below threshold avoids epoch/wake/join; mature sweep at or below threshold stays local. Major tracing initially still dispatches helpers. Tests assert epoch counts. Wago `CollectMinor`/`CollectFull` are serial today, so preserve owner-only execution without introducing a pool merely to imitate this change. If parallel collection is later added, owner-only phases must publish no helper epoch. Thresholds are source policy, not calibrated Wago constants. |
| `cf05aaf8149c9b0f38a74b57454f98254e8f9fc6` | **Adapt protocol; defer helper implementation until serial region semantics pass.** Per-helper cache-line tickets replace broadcast epoch sleep. Completion is a ticket plus helper bitmap; CAS acknowledgment seals writes, completion is idempotent, only final completion normally wakes the owner. Owner spins boundedly then sleeps with full-word predicate rechecks. Supervisor failure uses the same acknowledgment/completion path, poisons the Store, drains dispatched work, and disables allocation. Ticket exhaustion fails closed; shutdown wakes all live helpers. See details below. |
| `2d6af18d1` | fdstat synchronous-flag tests; no collector, lifetime, allocation, or host GC-root change. No GC port action. |
| `3dbc7e1b5`, `f663906e3` | WASI `proc_raise` signal delivery and bounded/reaped process tests; not collector helper dispatch or host GC reentry. No code transplant; keep signal/trap cleanup in integration gates. |
| `3ac3a3fc5`, `506b08d3d` | confined `path_link` source following and symlink tests; unrelated to GC port. No action. |

No new compiler lifetime or GC host-boundary landing appears in this range.
The earlier direct-host-entry optimization at `5a5bee94c` retains the previous
adapt/defer decision: Wago's invocation-domain leases and suspended roots are
the authoritative boundary, not Slipstream's native owner-word ABI.

### Wake/join details that must survive adaptation

`ng_dispatch`, `ng_publish_helper`, `ng_ack_phase`, `ng_complete`, `ng_join`,
`ng_drain_phase_acks`, `ng_worker`, `ng_supervisor`, and `ng_close` jointly define
the lifetime protocol. A wake is a notification, never proof of completion.
An inactive helper receives no new ticket and is not joined. A late wake cannot
complete another generation. STOP and DEAD are distinct from finite tickets;
56-bit exhaustion cannot wrap into a live generation. Failure during publication
must drain already-dispatched helpers before freeing storage. The assembly
comment relies on x86 locked operations and Goose extern compiler barriers;
Go/ARM64 implementations must establish their own publication ordering.

Worker sizing is also changed: trace work uses previous moved-byte history plus
major occupied-line work, saturating addition, a 256 KiB quantum and an available
pool cap. A bounded major preflight inspects at most 32 root records/slots,
checks encodings and descriptors, and allows owner-only pointer-free work.
Unknown/reference-bearing majors retain at least two participants when available;
pressure and fault hooks request all available participants. Sweep uses a 2 MiB
quantum above its local threshold. `N_LAST_WORK` records moved-byte deltas.

**Reservation is deliberately separate:** `ng_collect_owned` still computes
`reservation` using the prior small-minor/full-pool policy. `promote_all`,
`ng_admit`, and `ng_evacuation` use that reservation count, not reduced active
workers. Do not attribute smaller admission or allocation slack to wake/join.
Source tests cover active masks, 10,000 alternating phases, low futex-word
rollover, duplicate/early/delayed completion, exhaustion, corrupt generation,
tag-safe major preflight and pressure. Their presence is source evidence; this
worker did not execute Slipstream tests or reproduce performance claims.

### Old-space reservation: launch state, not an assumed landing

At launch `ng_admit` is unchanged from the previous cursor. It resets run count
and claim; zero promoting bytes succeed without runs. Otherwise it seeks credit
for `bytes + workers * NG_BLOCK`, skips evacuation blocks, walks free-line runs
in `[NG_OLD, NG_LOS)`, excludes runs whose size is `<= max(16,largest)`, and credits
`size - maximum`. It lazily protects blocks, updates the old high-water mark,
and fails if 65,536 runs or available space cannot cover the bound.
`ng_old_destination` claims those runs; exhaustion after relocation is an error.
`ng_refresh_block` reconstructs free-block availability; major sweep intersects
starts with marks and rebuilds occupancy before discarding empty blocks.

These bounded semantics do not establish the reported 18,115-collection failure
or its cause in Wago. The launch addendum says a fix is landing under owner
waiver, but intent is not a landed diff. The completion fetch result below is
binding for this handoff. Until a landed diff is inspected, do not copy a pending
fix or relax admission. Wago's analogous requirements are exact rollback before
handle relocation, bounded reservation ownership, and cancellation/recycling of
unused runs; see architecture gate R1/R2 and matrix rows A/G/R.

### Parked type-cap C1/C2: explicitly excluded

`01303964bf52d2f3c86f556480cb07cf5ec35165` is the report commit; implementation
is `059d10f17` (full SHA in `launch.json`). Neither is an ancestor of the launch
cursor. `parked-typecap.patch` preserves the exact **excluded** base-to-report
diff for comparison, not adoption. It grows Store-owned scoped canonical maps
and checked layout arrays before transactional registration, republishes flat
pointer/length views through JIT/native shims, and tests C1 IDs 4095/4096 and C2
managed-layout counts 4095/4096 plus early-managed 4097/20000. `gc/native.goose`
and its separate 65,536 limit are unchanged. Do not confuse this parked adapter
capacity fix with earlier landed canonical metadata work (`88c8c8563` history).

Wago mapping is `Collector.AddTypes`, `refreshNativeView`, subtype intervals,
`NativeInstanceView.LocalTypes`, `referenceStore.acquireGCCollector`, and
canonical-growth/addtypes-retention tests. Adopt the independent invariant that
registration growth must not invalidate existing instance views, but do not
import parked code or claim these source capacity tests qualify Wago.

## Sibling cross-check and evidence boundaries

Read-only Starshine audit pinned `origin/master` at
`e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1` in
`/home/dev/Projects/starshine-master-base`. Read
`docs/wiki/wasm-gc-core-boundary.md`, heap2local validation/fixup guidance, and
`src/passes/heap2local.mbt` owner-use checks (including replacement-constructor
old-owner reads and repeated-owner epochs). Starshine's model/validator and
heap2local rewrites provide fixture/alias/nullability checks; they are not the
Slipstream native collector. Its documentation also separates text-authoring
coverage from binary validation, so use verified binary fixtures if a text
surface is missing. Slipstream `ng_forward`, `ng_admit`, `ng_region`,
`ng_restore_root_cards` and the landed wake/join protocol provide the runtime
cross-check. No new engine defect was verified or triaged in this research
pass; requirements below are prospective risks/tests, not sibling bug claims.

Stage 1/2 evidence establishes bounded analysis and checked scalar publication
elimination only. Stage 2's 1,024-to-1 materializations retained 1,024 charges
and 24,576 nursery bytes in its fixture. The 128-entry/121-binary catalog has no
WasmGC constructors; it remains a control corpus, not coverage of region GC.
The historical `GAP-CONFORMANCE-TOOLCHAIN` stays open in this worker's evidence:
no new full conformance/toolchain qualification was run. The parent must close
it (or name an explicit temporary workaround) before the final full gate; the
launch perfection/review gate still forbids shipping an unrun required gate.

## Validation performed here

Five existing native collector tests were run with `-count=1 -v -timeout=5m`,
`GOMAXPROCS=4 GOFLAGS=-p=4`, separately in ordinary, `-tags=wagodebug`, and
`-tags=wago_regalloccheck` modes. Exact selector:

```
go test [mode flag] ./src/core/runtime/gc/native -run 'TestCheckedHandleGenerationRetiresBeforeWrap|TestNativeStructHandleBatchReservationAndCollection|TestNativeThroughputArrayReservationCanceledAtCollection|TestThroughputOldSpaceReuseAfterFullGC|TestCollectMinorPromotionFailureLeavesNurserySurvivorsUnmoved' -count=1 -v -timeout=5m
```

All five tests passed in each mode; raw outputs are `tests-ordinary.txt`,
`tests-wagodebug.txt`, and `tests-wago_regalloccheck.txt`. This is a focused
baseline contract check, not a test of a new region collector, generated-code
regalloc coverage, full runtime integration, or a performance sample. Standard
Go test duration fields are incidental runner output, not collected benchmarks.
No runtime change exists in this commit. Final prose/log integrity is checked with `git diff --cached --check` excluding
archived `*.patch` files. An unfiltered check reports whitespace on blank diff
context lines in those byte-exact source patches; the patches are intentionally
not normalized. `SHA256SUMS` covers all retained artifacts except itself. Only
owned files are committed. No PR or push.

## Completion mirror result

The single requested final fetch succeeded and remained at
`506b08d3d3fa3c8e8b1ce9ac0cb4f30a2463cdc3`: no new commits since launch.
Old-space reservation landing is **not visible** at this final cursor.
`ng_admit` is byte-for-byte unchanged since `db6014de2`; its exact final source
is archived in `old-space-landed-source.txt`. Both parked type-cap implementation
and report ancestry checks remain false. This is a pending source dependency,
not a reason to import a draft or pretend landed semantics are available. The
parent must inspect the eventual landed diff before building that corresponding
piece. No fetch loop, measurement, source modification, or shared-log edit was
performed. The final docs commit SHA is supplied in the worker report, avoiding
a self-referential hash in this document.

Other workers began runtime edits after these smoke tests; those edits were not
staged or tested by this worker. The recorded tests describe the initial
contract snapshot, not a later combined implementation head.
