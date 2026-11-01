# Stage 7 native evacuation and bounded run admission

Production head: `75e438784` (building on `97cb56269` and `b5fef423e`).
Primary qualification/test pin: `146fd03b5`. Supplemental capacity/test pin is
`f97b91c3c`; final edge/test pin is `831200ae6`. Exact identities are in
`summary.json` and the source records. Branch: `experiment/rfwgc-port`.
This implements the remaining standard-Go native stage; it is not a final
combined-port, review, publication, or performance verdict. No PR, push, closure,
subagent, benchmark, timing experiment, or measurement lease was used.

## Implemented behavior

`GCConfig` already aliases native `Config`, so no instance/domain construction
rewrite was necessary. `Workers` accepts 0/1 (serial), 2..8 total participants;
Tiny/TinyGo helpers remain rejected. CLI runtime parsing, fallback command schema,
and manager argument routing now expose `--gc-workers`, `--gc-region-shift`, and
`--gc-mature-evacuation`. The latter is default off and requires collecting region
Throughput under standard Go. Region-off/default policies preserve existing
exact-span promotion and work-count scheduling. The new conservative run policy,
byte/history active policy, and mature movement roll out together under the
explicit evacuation switch. `DisableMovingNursery` does not disable mature moves.
Actual TinyGo evacuation is explicitly rejected, not silently accepted.

Before major tracing, selection builds a bounded overlay of 32-KiB blocks and
256-byte occupied lines from **all physical old/LOS starts**, including objects
that the upcoming trace finds dead. Class rounding and absorbed tails count.
Every block touched by LOS is excluded. Crossing old extents join components;
every block must have 1..32 occupied lines, and the complete component must fit
the candidate budget. Components are never truncated. A deterministic rotating
cursor considers later components after a budget skip. Candidate and run counts
are bounded at 65536; overlay size is ceil(heap limit / 32768), computed in uint64.
No separate virtual capacity or source raw-pointer/forwarding representation is
introduced. The existing shared old/LOS budget remains authoritative.

The pure run planner walks ordered free intervals and the actual unallocated
suffix, splits them at 32-KiB boundaries, excludes every selected source block,
and checks exact alignment/end arithmetic. With D physical demand, M=max(16,
largest physical request), B=32768, and fixed reservation participants R:

- need = D + R*B, with checked addition and bounded R;
- each admitted run contributes max(0, run length - M);
- run length is at most B; the final partial block contributes only real bytes;
- D=0 requires no runs, credit, or worker slack;
- run storage capacity as well as length is capped at 65536.

The coordinator reserves whole runs, accounts their physical intersections as
reserved descriptor occupancy, and simulates deterministic per-participant cursor
streams. Exact destinations preserve the source class/tail representation for
old-to-old movement. Dead-candidate spans and unused suffixes are released once
before copying. Required young admission uses the same bounded run proof when
requests fit B; oversized young requests retain the existing exact-span proof.
The mature candidate cap does **not** limit the existing young plan array.
Small required young demand reserves one participant; otherwise reservations use
the configured participant bound, independently of CPU fallback/active sets.

Rollback uses a private allocator-metadata checkpoint rather than a per-tree-
rotation inverse journal. It clones the existing free-node arena, pending debt,
and mature descriptor vector, plus bump/backing headers. These are bounded by the
configured heap/existing allocator high-water; this is additional graph-sized
metadata cost, **not** a claim of fixed per-worker scratch or O(candidate-count)
rollback storage. Payload backing is not cloned. Since the serialized collector
owns all allocator mutations and no helper allocates, restoring the checkpoint
after join restores exact ownership, including partially released run suffixes,
without freeing both a run and its subdivisions. This representation adaptation
and its allocation cost belong in independent measurement and final review.

Copy helpers reuse prepared-copy plans whose sources resolve through unchanged
handles; they have no young-source assumption. They only write admitted disjoint
destinations. Old destinations copy object bytes and initialize physical padding.
Helpers perform no backing growth, allocator/descriptor mutation, callbacks,
handle publication, source poisoning, or native refresh. Every participant joins
before rollback or publication. Recoverable copy failure leaves source entries
and payloads intact. Unexpected helper/invariant failure poisons checked and
native admission; Close drains the existing pool lifetime.

The commit barrier precedes descriptor conversion, stable-handle publication,
preallocated source debt append, and source live-occupancy subtraction. Admission
is closed before subtracting source occupancy. Identity/generation, reference
words, remembered bits, and card-slot identity are retained. Old-to-old moves do
not call releaseHandle/promoteHandleTo and do not change young ages/census or
promotion counters. Source payload poisoning occurs only on the coordinator after
successful copy join. Dead identities are reclaimed once by the normal joined
sweep; live movement emits no reclaim callback. Card address-cache bounds are
invalidated and native views refreshed before guest reentry. Young survivors keep
full-collection card evidence. Postcommit reconciliation failure retains committed
handles and authoritative debt and reports `committed-maintenance`, not rollback.

`CollectionDiagnostics` separates required kind/result/first cause from optional
outcome, demand, credit, need, runs, reservation participants, actual copy
participants, and committed evacuated payload/physical bytes. Failed required
admission latches pressure; failed root enumeration retains it (including the
existing root-panic contract). Enabled majors consume pressure only after tracing
has admitted roots and considered evacuation. No candidates and optional refusal
are successful no-move outcomes and clear the request. A subsequent successful
attempt clears the old required cause. Pressure is not inferred from nursery
growth or pause timing.

The opt-in active policy uses source byte thresholds (64 KiB minor threshold,
256 KiB trace quantum, 2 MiB sweep quantum), joined work history, a minimum major
trace tail, and the full available pool under major pressure. Wago payload/handle
and old-bump work estimates adapt the representation; they are not raw-address
or cycle-for-cycle scheduling parity claims. Reservation credit never shrinks
with the active set. Custom external-edge tracing remains serial even for scalar
FlagPointerFree payloads; source callbacks stay on the coordinator. Sweep/copy
may still use joined helpers. `ParallelActivity` records joined phase participant
counts without timings.

## Qualification and reproducibility

`validate.py` exports immutable git pin `146fd03b5`, with no dirty overlay, using
Go 1.27.3 on Linux amd64. `source.json` records its exact source hashes and export.
For ordinary, wagodebug, and wago_regalloccheck it runs:

- full `./src/core/runtime/gc/...`, ordinary and `-race`;
- `./src/wago -run '^TestGC'`;
- AMD64 backend selector `GC|Collector|NativeAlloc|Eden`;
- runtime CLI, fallback handoff, and manager scope/GC option selectors.

All commands use `-count=1`, a finite test timeout, and no benchmark selector.
`results.json` is authoritative for commands, exits, passes, and named skips;
Gzipped JSONL logs include incidental Go test durations, not performance samples.
`validate-supplement.py` independently exports the supplemental committed test
pin, runs source-sized capacity and GOMAXPROCS=1 fallback in all three modes with
and without race, checks explicit standard-Go TinyGo-tag rejection, and
cross-compiles native tests for ARM64/RV64. Cross-compilation is not execution or
actual TinyGo compiler qualification. Supplemental production equivalence is
recorded separately; parent combined-final-head gates remain mandatory.
`validate-edges.py` exports the final committed test pin and qualifies fragmented/
partial-run credit, checked generation identity, and full-old/LOS-heap zero demand
in all three modes with and without race. `production-equivalence.json` verifies
identical owned production bytes across all three exports and the final worktree.

New deterministic tests cover policies 0/6/12 with movement enabled only for
region policies, and actual enabled copy participants 1/2/4/8; public Workers 0/1/2/4/8 and actual guest helper work; actual guest reads
after old relocation triggered by failed required LOS allocation; independent
physical interval coverage; unchanged stable handle entries through copy
failure and unchanged checked generations after actual movement; source exclusion, 1/32/33-line selection, crossing-component movement,
LOS-intersection exclusion, class absorbed tails, partial blocks via bounded
planner inputs, and rotating candidate budgets. Tests exercise exact credit and
one-byte deficits, arithmetic overflow, run limits, zero old demand, and young
plans exceeding the mature candidate cap.

Cards/ownership tests relocate an old self-referencing parent with a young child,
retain its handle-keyed cards, and preserve a nonmoving LOS array with writes at
9999 and 19999 (above 65536 bytes). A portable scalar shadow-edge cycle proves
FlagPointerFree is not an external-edge exclusion; conditional root release and
post-join exactly-once reclamation are checked. Helper barriers hold the last
writer, prove no early handle publication, abort and retry, test sticky poison
and disabled native admission, and Close twice after drain. Arbitrary concurrent
Close versus mutator/collection is still outside the serialized collector API.

Debug-only fault fixtures cover metadata/backing failure on the second admitted
run, first/last serial partial copies, precommit failure, and postcommit debt
reconciliation/retry. Real helper partial-copy abort/poison runs in every mode.
The 256-MiB fixture uses 65536 literal 4096-byte nodes, batches required minor
promotion, traces an independently checked live chain, asserts all eight trace
and sweep participants, checks dense no-candidate pressure reset, then requires
complete identity/debt/bump reclamation. It does not claim sparse evacuation of
256 MiB or source address/window equivalence.

Prior stage4/5 and stage6 tests remain controls for native independent reservation
cancellation (1030), mark high-water/captured retry (1019), descriptor physical
extents (1003), Tiny prerequisite behavior (1022), graph/queue overflow, epoch and
phase boundaries, callbacks, and minor/promotion oracles. The matrix is scoped to
its recorded fixtures, not every possible Cartesian combination. Existing
backend diagnostic-tag skips and unrelated guest profile skips are enumerated;
no skip is silently counted as a pass.

`initial/` and `pre-root-diagnostics/` retain superseded runs. Their unfinished
commands are explicitly interrupted, not final passes. `capacity-fixture-correction/`
retains an initial test assertion error: TypeDesc.Size is payload size, so the
physical fixture assertion must include PayloadOffset. That was a test correction,
not a collector defect or a reduced capacity target.
`checked-fixture-correction/` similarly preserves an initial setup error: checked
identity tracking must be enabled before allocation. The corrected fixture tests
actual movement and generation equality; all final edge runs pass.

## Final scoped result

All **32 commands passed**: 15 primary, 11 capacity/target/cross-compilation, and
6 admission/identity edge commands. No new stage-7 test skipped. Counts below
include named tests and subtests; `summary.json` separately records top-level
counts and every existing skip.

| Mode | Collector / full race passes (each) | Guest passes / skips | Backend passes / skips | CLI passes |
| --- | ---: | ---: | ---: | ---: |
| ordinary | 2220 | 1755 / 9 | 134 / 26 | 26 |
| wagodebug | 2302 | 1746 / 15 | 128 / 28 | 26 |
| wago_regalloccheck | 2220 | 1755 / 9 | 144 / 26 | 26 |

The debug collector and race runs each retain three existing dead-allocation
batch test skips; ordinary/regalloccheck collector runs have none. Backend
skips retain their diagnostic/profile conditions. Supplemental tests pass without
skips; disabled-target checks are standard-Go tag tests, and ARM64/RV64 checks
only compile. All owned test processes drained and exact temporary exports were
removed; source pins, hashes, commands and compressed logs remain reproducible.

## Source cross-check and R9 H1-F1

Read-only Slipstream `82d546679bab4887acfab056f6ad773a969b1e03:gc/native.goose`
was checked for attempt/refusal/pressure, admission credit, active sets,
evacuation, and dispatch/join. The retained stage6 design/source excerpts and
stage4/roll-up assessments provide exact source context. Source constants are
adapted to Go-backed storage and stable handles; the default 16-MiB heap is not
silently enlarged to source virtual-window sizes. Parked type-cap code is not
claimed landed. Starshine `e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1`,
`docs/wiki/wasm-gc-core-boundary.md`, supplies model/validation context, not a
corresponding native relocation/worker implementation. No sibling was modified.

R9 H1-F1 remains absent in the touched protocol: channel delivery publishes a
fresh phase identity, mutex predicates govern trace waits, WaitGroup joins seal
writes, and persistent channel close plus lifetime join governs STOP. Neither
uint64 sequence nor uint32 claim epoch is a truncated notification/wait identity.
Existing R9 phase/registered-condition/STOP probes and wrap/duplicate/drain tests
are included in the full collector and race commands after these changes.

## Remaining combined-port gates and measurement handoff

H6 owns lifetime/converted-provider receipts, portable object-edge APIs, and
narrow safepoints; none of those files were edited here. This evidence does not
certify later concurrent H6 commits. TinyGo trampoline/toolchain work, actual
TinyGo evacuation support, non-amd64 execution, broad artifacts/corpus/conformance,
final seven-PR and R9 dispositions, combined final-head suites, and the mandated
three-Daybreak-plus-Claude review/rechecks remain parent-owned gates. This stage
closes neither a PR nor an owning thread. No upstream publication is authorized
by these scoped results.

READY TO MEASURE after correctness workers drain, by an independent holder of
the global measurement lease. Use branch `experiment/rfwgc-port` and the exact
production/test/evidence heads in the handoff. Sensitive paths: allocation and
promotion planning; optional metadata checkpoints; full tracing/selection/copy/
sweep/reconciliation; helper selection/join/history; card/native-view refresh;
CLI configuration and validation. Proposed workloads: default/region-off and
no-GC controls, evacuation-off/on, workers 1/2/4/8, dense and sparse old graphs,
source-sized live chains, fragmented mixed old/LOS, zero-credit survivor cycles,
required/optional refusal and retry, callbacks/cards/native guests, metadata/run
limits, processor/active-set transitions, and matched compiler controls. No
performance direction is asserted.
