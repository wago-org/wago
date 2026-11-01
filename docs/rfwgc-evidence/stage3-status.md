# Stage 3 orchestrator checkpoint — 2026-11-01

Stage 3 is OPEN. This is a checkpoint, not the consolidated stage exit report.
The full Snap port and final review/PR gates remain open.

| Gate | Evidence and disposition |
| --- | --- |
| Source mirror | `69a6d9ec5`: assessed Slipstream through `506b08d3d`. Dispatch trim and wake/join landed; old-space reservation landing not yet observed; parked type-cap excluded. See `stage3-mirror/`. |
| Implementation | `5b31c81ec`: opt-in fixed region Eden, exact tracing and empty-region reuse, stable handles, transactional evacuation fallback. Native batches deliberately disabled. See `stage3-runtime/`. |
| Slice correctness | Runtime worker passed collector suites, selected guest GC tests and new region cases in ordinary, wagodebug and wago_regalloccheck, including 20,000 reuse cycles. This is scoped evidence, not full integration coverage. |
| Independent review | `72e478f6c`: no verified correctness findings in `a3c08365f..5b31c81ec`. Independent survivor/Eden, late rollback and configuration probes passed. Concurrent unfinished test files were excluded through an overlay for guest tests; those tests were not thereby qualified. See `stage3-review/`. |
| Toolchain | `d546a1af4`: dependency gap closed on pinned `a3c08365f` and baseline `9713878ab`; each passed 168 staged, four harness and three converter tests per mode, without skips. Does not qualify the new runtime. See `stage3-toolchain/`. |
| Current full suites | Assigned on immutable `d546a1af4` export with repaired toolchain, all three modes. Pending result. Later test-only commits require their own scoped qualification. |
| Region host/domain integration | `e1feed09d`: 11 scenarios, each baseline/region paired, passed all three modes without skips. Includes reentry/trap recovery, retained tokens, globals/tables and close, foreign-domain rejection, borrow exclusion, EH/tails and small successful metadata growth. Forced collection and actual region occupancy asserted. See `stage3-integration/`; its uncovered rows remain open. |
| Runtime/compile measurements | Harness preparation assigned; no timing authorized during concurrent qualification. Independent measurement must acquire the global lease and satisfy temperature and matched-control rules. |
| Corpus census | Preparation assigned, including explicit graph fixtures because the historical 128-entry/121-binary corpus has zero GC constructors. Pending census evidence. |
| Consolidated stage exit | Pending correctness reconciliation, independent measured costs, census update and consolidated evidence commit. |

## Full-port work still required

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
