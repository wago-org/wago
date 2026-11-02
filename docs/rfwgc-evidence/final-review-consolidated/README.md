# Port review consolidation — current checkpoint, 2026-11-02

**Not final acceptance or PR readiness.** Current execution candidate is `08f511fac41a77f4c0f9046f88ebc78ad05a5da6`; harness/delta pin is `041f7ed4c376e2a33ce21b22075cd617ddca6597`. The initial fan reviewed `5345bd365` against campaign base `9713878ab`. Later repairs have separate independent reviews and exact-pin evidence; none inherits the original full qualification automatically.

## Initial required four-reviewer fan

| Reviewer/slice | Evidence commit | Initial result |
| --- | --- | --- |
| Daybreak collector | `39ae86d8a` | C1 documentation defect; no verified behavioral defect |
| Daybreak compiler/lifetime | `cf14188a1` | No verified production defect |
| Daybreak runtime/rooting | `e39f457f0` | No verified production defect |
| Claude design/judgment | `f8b0e1827` | DR-1/DR-2 executed regressions; DR-3 platform publication gap |

The no-finding slice reviews did not disprove Claude's later adversaries. Parent accepted those findings. Claude Q2 duplicates C1. Distinct symptoms DR-1 and DR-2 share an admission cause but retain separate regressions. RR-1 was residual DR-1; RR-2 and RR-3 were newly introduced repair defects, not relabeled old passes.

## Finding dispositions and re-review chain

| Finding | Repair and independent disposition |
| --- | --- |
| C1 stale region/native-batch comment | `942fe6c7d`; recheck `ad5813fbe`: CLOSED, documentation only |
| DR-1 unrelated GC-domain serialization | First repair `9180f09f7` fixed domain locking but left RR-1 topology blocking. RR-1/2 source `9ad40bce4`; Claude `3b8732ad3` closes those but finds RR-3. Final source `5847df30f`; Claude `5fda66aa4` closes RR-3 and retains RR-1/2 closure on host |
| DR-2 scalar cross-Runtime imports | Durable regressions pass; Claude `0efde30d7`, `3b8732ad3`, `5fda66aa4` retain closure |
| RR-2 stale nested invocation identity | Authenticated root/snapshot authority in `9ad40bce4`; closed in both later Claude rechecks |
| RR-3 blocking snapshot release | Non-prioritizing retirement in `5847df30f`; independent host/race closure `5fda66aa4`; current native Linux ARM64 audit `6a5d7272c` verifies original assertions all3 normally/race. QEMU failures remain separately open |
| DR-3 unsupported-platform fail-open publication | Rejection repairs `9180f09f7`, token classification `c786a9b84`; Darwin ownership `3f6161883`; namespace/order followups `15d7b24b7`/`7e41fafbb`. Reviews `8526d0610`, `495a5cd7e` close scoped implementation findings. Native Darwin H2/H6 positives pass at7e41; current full native rerun remains pending |
| CI-1 required-check impersonation | `7e887cb25`; recheck `446e9254d` closes. Later every-job dependencies and opt-in isolation independently reviewed through `57c04aa09`/`c24f72018`; special runs do not qualify normal CI |
| ARM64 import-state join | `d8dc1a6dd`/`a9151a44b`; independent `db9bc97ee`; actual Darwin source-da19 execution passes original join regressions |
| ARM64 router stale imm19 | `d6f4e94fe`; independent `b33f0f5c5`; old-code negative reproduces wrong0x444 instead of0x440 frame-save target. Native targeted gates pass at7e41 |
| TinyGo invalid token classification | `c786a9b84`; `613bb77b8` recheck and `d2464100f` actual all3 qualification |
| Unsupported global accessing namespace/order | `15d7b24b7`/`7e41fafbb`; independent `495a5cd7e`, pre-fix negative retained |
| New unused conversion wrappers | Removed `5b02ec67a`; independent `b8513359a`, all four Staticcheck profiles zero new findings. Uncalled wrappers; no shipped performance effect |
| Formatting failures | `ca164dd25`; originals preserved; fresh041f formatting PASS. Historical5b02 failure unchanged |
| Darwin TinyGo fixture/oracle failures | `4913c91e1`; static review `d03c1b057`, completed-receipt activation recheck `c24f72018`. Native audit `094558f99` PASS33/36/33 plus23 required children/mode; prior nativeFAIL preserved |
| Darwin multi-table stale rejection predicate | Test-only `08f511fac`; independent `57c04aa09`. Positive545/hello/collection and rejection assertions unchanged. Fresh native follow-through pending |

The authoritative Slipstream RR-3 comparison is `docs/rr3-retirement-repair/SLIPSTREAM-MAPPING.md`, source `d6802f5e389bded786f49b4c2a21df0efab7017c`, eight pinned sites. The deadlock shape is ABSENT there. Wago mirrors nonblocking release and Close/join outside progress locks; snapshot claims, topology reader/writer scheduling and cross-Runtime shared-memory coordination are Wago-only. Collector join-under-mutator-lock is not copied into topology retirement. Post-comparison tests and independent source checks are retained.

## Current gate ledger

| Gate | Verified status and scope |
| --- | --- |
| Immutable5b02 combined220 | 216PASS/4FAIL/0unrun; `25f68f2c2`, final-combined-5b02. FAIL: historicalformatting and three ARM64QEMU repair cells |
| Fresh041f delta | 8/8PASS; formatting, RR/multi-table all3, CI-policy/planner all3, actionlint. Exactly five non-doc test/CI changes;2447 production/support entries identical. Full suite was NOT rerun on041f |
| Corpus/spec/race | All768 corpus rows/384 pairs,12 exact spec lanes zero gaps/skips, full host/runtime/collector/integration race all3 PASS within5b02 evidence |
| Native Linux ARM64 current source08f | `6a5d7272c`:192/192 per mode, normal/race original failed assertions pass. Native excludes QEMU-added architecture test but retains independent host/ELF identity. Assertion equivalence is not whole-gate/environment equivalence |
| QEMU repair cells | FAILall3 at5b02. Timer64 EINVAL and shared-entry cancellation failure kept distinct; guard cause remains under investigation. No deadline/signal/oracle change or native-pass promotion |
| Darwin ordinary Go/Windows | Run41466555967 source08f/harness041f active. Prior7e41 audit `c25943944`:72ownership including22RR, graph/baseline/cancellation/Core3/spec/installer/codegen PASS, stale multi-table test FAIL; Windows six contracts PASS, no GCJIT claim |
| Native Darwin TinyGo | `094558f99`:source7e41 plus declared test overlay/private0.42 patch,102top-level+69required childPASS, no skips. No5b02/08f/fullCI or unsupported-capability credit |
| Canonical TinyGo resume delta | Actual patched Linux0.42 source4913 positive3PASS/wrong-field3expectedFAIL, independently verified; no contemporaneous launcher-file/hash retained, exact serialized argv/tool/env/source/raw receipts disclosed |
| Independent measurement | Preparation only. No lease, acquisition, timings or performance verdict. Previous candidate seal invalid; current041f reseal active |

## Seven-PR rollup authority

Fresh read-only GitHub queries in `current-checkpoint/pr-heads.json` confirm all seven published heads still match the assessed sources. All remain OPEN/DRAFT; none closed. Owner order supersedes the original no-bundling instruction. Parent adopts inclusion scope for each, but final **INCLUDED** requires final green gates; no provisional scope verdict is mislabeled final acceptance. No topic is currently judged SUPERSEDED or INCOMPATIBLE.

| PR | Verified source | Integration disposition |
| --- | --- | --- |
|997|`e2736f0f37e614218ed7da7ff4040cf1697431c3`|Reconciled root walker with1026 in4ad440e86; included scope, final gates pending|
|1003|`9666ec48e18cb2f7a77c188dc52f093770859f26`|Published extent validation in a3f26602d; historical abstract-array fusion is NOT claimed imported|
|1019|`b6cb0e829011ab08d98d64eaefbc0085c34a0284`|Bounded clearing plus queued-mark repair in e23657e6a; final gates pending|
|1020|`ce63d517a55f2981b7ccf6e150d1b9050fb5f953`|Adapted parallel collector bc05eef3e and stage7/8; object-edge-source tracing stays serial, TinyGo helpers rejected|
|1026|`4101e75a3b915ce8b0217cd356b9837339c54ee3`|Reentry/scalar-adapter roots reconciled with997; distinct regressions retained|
|1027|`29e59c6413e502b000d00e40836350190db1f3c1`|866e6b5e7 plus tight EH prerequisite/followup repairs|
|1030|`fba8e2b50ea0cbc31089789dd27dde628c55fe9d`|9bfe98360 adapted cancellation preserving pending reservations|

Owner-supplied997 `421119d1d` and1003 `11bcf23fd` remain unavailable objects; no equivalence claim. Selection uses the freshly verified published heads, as permitted by the freshest-verifiable-head instruction. Assessment reports preserve the missing identities and source/hunk provenance. The advisory support documents use INCLUDED as scope only; parent explicitly rejects interpreting that as final gates-green acceptance.

## R9 and remaining assessment obligations

H2-F1, H6-F3 and H6-F2 have implemented fixes, scoped independent reviews and execution evidence; final INCLUDED-FIX awaits combined/platform/performance closure. H4-F1 remains PRESENT-OUT-OF-SCOPE (Core3 Threads feature omission), with evidence for Joshua and no separate PR. H6-F1 is ABSENT for the exact no-type null-global trigger; this is not blanket managed-module absence. H1-F1 is ABSENT from the adapted channel/join protocol, including stage7 checks; no low32 ticket protocol was introduced.

Claude Q1/Q4 require large-old-heap and non-GC externref measurement controls, included in the approved expanded plan. Q3 serial tracing with installed object-edge source is an explicit supported limitation, not parallel-tracing evidence. Q5 ordinary public Table.SetValue behavior and unsupported converted publication have durable target controls. Q6 wait/notifier and admission liveness regressions are covered by repair/native suites. Q7 destructive evacuation panic is fail-closed; collector tests reject subsequent checked/native admission and Close twice. Shared-domain consequences remain an explicit assessment item, not an established production defect or a claim of executed multi-instance poison coverage.

Final review closure, final gate acceptance and performance remain open. No known failure is waived. No PR opened, tabled PR closed, or performance measurement started. November4 remains at risk from remaining qualification, queue/repair costs and the still-unperformed independent measurement protocol. Documentation-only consolidation cannot affect runtime performance.
