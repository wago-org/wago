# Stage 3 orchestrator checkpoint — 2026-11-01

Stage 3 is OPEN. This is a checkpoint, not the consolidated stage exit report.
The full Snap port and final review/PR gates remain open.

| Gate | Evidence and disposition |
| --- | --- |
| Source mirror | Initial `69a6d9ec5` through `506b08d3d`, superseded by `9d6d3ff7b` through `82d546679`. Landed old-space implementation `ccc6512de` expands mature virtual capacity 120→376 MiB, preserves 128 MiB LOS and reservation formula; diagnostics `bb0900efa` and host landings assessed. Parked type-cap excluded. See `stage4-mirror/`. |
| Implementation | `5b31c81ec`: opt-in fixed region Eden, exact tracing and empty-region reuse, stable handles, transactional evacuation fallback. Native batches deliberately disabled. See `stage3-runtime/`. |
| Slice correctness | Runtime worker passed collector suites, selected guest GC tests and new region cases in ordinary, wagodebug and wago_regalloccheck, including 20,000 reuse cycles. This is scoped evidence, not full integration coverage. |
| Independent review | `72e478f6c`: no verified correctness findings in `a3c08365f..5b31c81ec`. Independent survivor/Eden, late rollback and configuration probes passed. Concurrent unfinished test files were excluded through an overlay for guest tests; those tests were not thereby qualified. See `stage3-review/`. |
| Toolchain | `d546a1af4`: dependency gap closed on pinned `a3c08365f` and baseline `9713878ab`; each passed 168 staged, four harness and three converter tests per mode, without skips. Does not qualify the new runtime. See `stage3-toolchain/`. |
| Current full suites | `10d546b84`: full-root suites PASS on immutable `d546a1af4` in all modes; supplemental nested/race/hardening/spec/corpus gates pass within documented scope. Aggregate qualification BLOCKED by intermittent TinyGo debug CLI interruption abort; targeted later passes do not close its cause. Dedicated investigation assigned. Later changes and other platforms remain unqualified by this snapshot. |
| Region host/domain integration | `e1feed09d`: 11 scenarios, each baseline/region paired, passed all three modes without skips. Includes reentry/trap recovery, retained tokens, globals/tables and close, foreign-domain rejection, borrow exclusion, EH/tails and small successful metadata growth. Forced collection and actual region occupancy asserted. See `stage3-integration/`; its uncovered rows remain open. |
| Runtime/compile measurements | `2cc654c7d`: pinned harness preparation complete, 81 correctness subtests passed across revisions/modes. No timings yet. Wake/join owner released grant `WAKEJOIN-LAPTOP-20261031` at `2026-11-01T21:12:50.219814+00:00`, confirmed runners drained and lock unlocked; release evidence commit `19fed674ece7b155e8c6f70627e5c58eafc55242` on Slipstream `measure/wakejoin-laptop-20261031`. Retained artifacts reserve no host. This is not a Wago lease acquisition or a source performance verdict. Full-suite work must finish before independent leased measurements. |
| Corpus census | `2cc654c7d`: 128 entries / 121 binaries available and hash-matched; 47/47 declared input references matched. Separate graph manifest prepared. This is availability, not execution: full-corpus runtime/compile/oracle reconciliation remains open. Historical catalog has zero GC constructors. |
| Consolidated stage exit | Pending correctness reconciliation, independent measured costs, census update and consolidated evidence commit. |

## Full-port work still required

Native region reservation implementation committed at `98939b4dd`: bounded native
live structs/small arrays, refill/cancel and metadata/trap invalidation, with
large arrays/dead charges retaining checked fallback. Scoped collector/backend/
guest validation passed all three modes. Independent review `e33e4ac5d` found no
new correctness defects, with independent probes passing all modes on that frozen
head. ARM64 cross-compiles but execution is unverified. Two interval-register
diagnostic failures reproduce on complete baseline `5b31c81ec` in all modes;
they remain red qualification issues, not silently waived or new port regressions.

Mature baseline probes/design committed at `4eeaea8fc`: 12 cases per mode,
plus six debug rollback cases, all passing without skips on frozen `94846b133`.
These include 20,001 promotions, fragmentation, zero-credit survivor copies,
refusal/retry and high LOS cards. They establish existing allocator controls,
not implemented mature regions. Mature descriptor/transaction/reclamation work
is now active under one collector production owner.

TinyGo investigation `b6a36dec` identified a pre-existing foreign-stack scanning
defect on both baseline and current, including pinned TinyGo 0.42.0. GC scans
between foreign SP and the original host stack boundary. A separate prerequisite
fix worker owns TinyGo entry/toolchain integration, not collector source. Passing
retries cannot close this demonstrated defect; aggregate qualification stays
blocked until a correct fix and deterministic boundary tests pass.

Native region reservations/refill/cancellation; mature and large-space region
policy with transactional admission; inspection of the eventual landed source
old-space fix; region-enabled host/shared-domain, metadata, artifact and platform
gates; and parallel behavioral parity remain to be implemented or qualified.
Do not silently waive parallel parity on the basis that the first slice is serial.
Preserve default-policy oracle and exclude the tabled GC PR implementations.

The final owner-directed review fan remains three Daybreak reviewers and one
Claude Opus 5.5 reviewer, consolidated with every finding resolved and every fix
re-reviewed. This intermediate review is not a replacement. Open one ready PR
only after implementation and all final gates pass; link it to its owning thread.

This checkpoint changes documentation only and cannot affect shipped runtime or
compiler performance. No benchmark was run for this update. Pre-existing dirty
corpus scripts, shared port log and conformance submodules remain untouched.
