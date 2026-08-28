# Dragline final master-plan audit

Date: August 28, 2026
Plan: `docs/dragline-final-master-plan.md`
Implementation ledger: `docs/dragline-plan-status.md`

## Verdict

All deliverables in roadmap phases 0 through 15 are implemented. The strict
WebAssembly 1.0 gate compiles 782 modules with zero MVP rejection, and the
generated 180-export MVP ISA inventory executes correctly through Dragline.
On the measured Apple M4 Max, every one of those 180 exports is faster than
Railshot in both compatibility and native modes under the retained six-round,
500 ms alternating gate.

The plan's experimental-branch promotion gates are not roadmap requirements.
APX and SVE2 instruction experiments, `RASSA`, vector packet lifting, automatic
single-call `TargetFatNative` packaging, and other items explicitly classified
as research remain optional follow-on work. The implemented fat-native
substrate is the bounded Railshot compatibility image plus one compact native
clone and one-time entry-table publication.

## Roadmap evidence

| Phase | Result | Primary evidence |
|---|:---:|---|
| 0 — sibling boundary | PASS | Shared compiler router/input/output, stable engine identity, strict Dragline errors, CLI/config selection, and dependency-policy tests. |
| 1 — measurement | PASS | Version-16 compiler metrics, replay artifacts, isolated compile/RSS/code-size harnesses, prepared-call execution worker, and checked-in raw corpus reports. |
| 2 — RailSSA | PASS | Dense structured CFG, typed block arguments, local SSA/value flow, sparse metadata, simplification, demand proofs, and independent verification. |
| 3 — RailMach echo | PASS | Dense machine SSA, explicit operands/constraints/effects, target verification, AMD64 and ARM64 emission. |
| 4 — late SSA exit | PASS | Affinity retention, physical-copy placement, cycle resolution, rematerialization, and edge-transfer verification. |
| 5 — optimizer/proofs | PASS | Bounded sparse simplification, aliases/GVN/ranges, dead code/blocks, semantic obligations, and independently replayed bounds certificates. |
| 6 — pressure shaping | PASS | Separate bank pressure, sinking, rematerialization, affine/induction planning, LICM, and hard budgets. |
| 7 — quality allocation | PASS | Linear and greedy allocators, progressive splitting, spillsets, regional fragments, fixed-use repair, and verifier-backed fallback. |
| 8 — RailSpec/selection | PASS | Versioned generated target rules, semantic hashes/proof status, feature predicates, integrated expression ordering, and target costs. |
| 9 — scheduling | PASS | Dependency DAG, three sequential bounded candidates, fusion/resource/pressure scoring, post-RA scheduling, and one bounded retry. |
| 10 — private ABI/IPRA | PASS | SCC call graph, callee-first contracts, exact clobbers, ABI classes, multi-result ABI, allocator-managed saves, shrink wrapping, and `FrameCompose`. |
| 11 — post-RA quality | PASS | AMD64 LEA/move/partial-register/fusion/rename work; ARM64 pairing, pre/post-index, offset folding, forwarding/promotion, physical rename, and MOPS; every scan is bounded and independently verified. |
| 12 — specialization | PASS | Same-instance calls, host effect contracts, bounds preservation, profiled indirect targets, exact WasmGC facts, fresh-object/store/allocation specialization, and narrow roots. |
| 13 — native/profile | PASS | Stable CPU/features, compatibility/native target identities, generic and measured Apple-M4 cost tables, bounded profile selection, ExtTSP-like layout, calibrated bulk memory, compact native clones, and fail-closed APX/SVE2 policy foundations. |
| 14 — production | PASS | Complete strict MVP coverage, stable artifacts, compatibility/native modes, typed diagnostics, explicit whole-module fallback, and release benchmark reports. |
| 15 — cache/daemon/tiering | PASS | Relocatable function artifacts, bounded shared cache, optional daemon, Railshot counters, bounded planners, cross-tier bridge, compact clone installation, exact GC generation switching, and no OSR. |

## Compact native-clone closure

`PlanCompilerNativeClones` ranks only roots that have measured heat, an explicit
native opportunity, and sufficient projected gain. Each admitted root expands
to its complete transitive local direct-call closure. The selection is bounded
by both function count and original body bytes; a closure that does not fit is
skipped atomically.

`CompileNativeClone` emits only the selected bodies on AMD64 and ARM64.
Unselected entry and internal-entry rows are zero, local direct calls may not
escape the image, and imported/indirect calls retain runtime dispatch. The
compact image preserves original function indexes and exact trap, safepoint,
and GC-root identities, but cannot be instantiated standalone or serialized.
`InstallDraglineTier` accepts it only under the exact sorted selection and
publishes entries once, leaving cold functions on Railshot.

This gives the fat-native shape required by the plan without retaining a second
whole native module and without a feature test in hot code.

## Current validation record

- `go test ./src/core/compiler/profile ./src/core/compiler ./src/core/compiler/backend/dragline/... ./src/wago -count=1` — PASS.
- `WAGO_DRAGLINE_MVP_COVERAGE=1 go test . -run 'TestDragline(MVPCoverage|MVPISAInventory|ISACorpus)$' -count=1 -v` from `bench` — PASS: 782 compiled, zero MVP rejection, 180 ISA exports.
- Linux AMD64 and ARM64 test-binary cross-builds for `src/wago` and the Dragline backend — PASS.
- AMD64 compact-clone, exact-GC tier, and complete ISA execution under Rosetta — PASS.
- Public-facade generation check — PASS.
- Repository-wide `go test ./... -count=1` — PASS after facade regeneration.

The exact performance evidence, environment, commands, per-module rows, and raw
inputs are in:

- `docs/dragline-isa-arm64-compat-2026-08-28.txt`
- `docs/dragline-isa-arm64-native-2026-08-28.txt`
- `docs/dragline-railshot-cranelift-corpus-arm64-2026-08-28.md`
- `docs/dragline-apple-m4-cost-calibration-arm64-2026-08-28.md`

## Qualification boundary

AMD64 code generation, compact cloning, GC generation switching, and the full
ISA inventory execute under Rosetta and cross-build for Linux AMD64. This host
cannot provide physical AMD64 PMU or native-hardware measurements. Those runs,
like LLVM comparison and qualification on an ARM64 CPU that actually exposes
FEAT_MOPS, are platform-specific release checks. No report here represents a
translated run as native hardware evidence.
