# Final compiler/backend review

Review source: `5345bd365c13963f1bf60e26bb80789ab9ead4cc`

Campaign base: `9713878ab452848f7eb7549a90be94b938a898be`

Evidence head read: `2c9f72744dd6f414fc8f236c2d45aa39ce6ced69`

Independent slice: compiler/frontend/backend on all represented targets, lifetime replay and scalar lowering, native reservation ABI, typed mutation safepoints, EH roots, extern store helpers 53--58, and the deferred-load emission/diagnostic repairs. This is review evidence for the parent's final fan; it is not final approval, a performance verdict, or a closure claim.

## Verdict

No verified production defect was found in this slice.

The reviewed code preserves the specific invariants requested:

- AMD64 lifetime replacement is fail-closed. `gc_lifetime_consumer.go:47-76` seals a complete body/type/signature/local/layout/policy snapshot, `:193-207` independently reseals it before emission, and `:209-247` uses the ordinary helper path for the replacement allocation. Unsupported instructions, EH, custom instructions, non-native layouts and non-pointer-free layouts decline the optimization and return to ordinary lowering rather than partially emitting.
- Typed mutations that may create traced function environments become safepoints on AMD64 and ARM64. `amd64/gc_struct.go:1119-1148,1316-1331` and `arm64/gc.go:918-962,1166-1182` classify the function-carrying struct/array mutations as allocating before recording exact roots. Ordinary numeric, GC-reference and extern stores remain noncollecting under their locked transaction protocol.
- Helper 53--58 compiler ABIs match the runtime dispatcher: table/global set are 3/2 arguments, fill/copy/grow/init are 4/5/3+1/5. The AMD64 sites are `globals.go:174` and `table.go:170,257,339,430,602`; ARM64 mirrors them at `globals.go:186` and `table.go:199,267,324,396,557`; runtime arity/operand interpretation is `src/wago/gc_conversion_store.go:15-20,23-72,101-191`. Reference-valued fill/grow/set operands are passed with their Wasm reference types, so the common host-call lowering retains them through the helper call. Bulk copy/init snapshots source words before publication (`gc_conversion_store.go:172-189`), preventing aliased-source corruption.
- Collector-free providers still reserve real native call frames for extern stores. `shared/extern_store.go:5-22` feeds both hint and loop/call planning; the architecture contract tests explicitly cover those planning sites. This prevents ARM64 LR/frame elision and preserves unwindability even when the provider has no local collector.
- EH root maps use architecture-specific local-slot geometry and include imported plus declared tags. AMD64 uses one slot per local (`amd64/eh_rootmap.go:85-102`); ARM64 counts machine slots, including multi-slot values (`arm64/eh_rootmap.go:85-108`). Both map catch-ref payloads by typed lane and conservatively report all GC lanes for catch-all-ref, including tag-free foreign exceptions (`amd64/eh_rootmap.go:13-67,147-188`; ARM64 equivalent `:13-67,154-195`).
- The deferred-load repair is narrowly correct. At `amd64/emit.go:187-201`, a memory-left commute is rejected only when the load address is the requested destination; this keeps the address live until the load. `commute_mem_address_test.go:12-40` verifies emitted instruction order for i32/i64, while the preserved checked diagnostic catches the original clobber. The established-route diagnostic helper is test-only and restores the global after each serial test (`established_scalar_path_test.go:7-16`), so it does not alter production routing.
- The shared root-plan reservation API refuses arena growth after addresses escape (`shared/gc_frame_roots.go:385-403`), and flat callsite/safepoint streams validate bounds and completeness before use (`:149-225,249-329`). This closes the address-instability and malformed-metadata paths rather than relying on passing execution tests.

## Questions and coverage gaps (not verified defects)

1. Native Darwin execution remains `NOT_RUN`. The combined evidence is honest about this; source/build reasoning and Linux/QEMU results do not establish Darwin unwind, reservation, or EH behavior.
2. Lifetime replay is an AMD64-only optimization (`gc_lifetime_consumer.go:1`). ARM64 and RV64 therefore have no equivalent optimization to qualify; they use ordinary lowering. This is a scope boundary, not a cross-target semantic hole.
3. Host opaque conversion receipts are intentionally unavailable on `!linux`, RV64 and TinyGo. `src/wago/gc_conversion_store_other.go:1-27` disables hooks and explicitly rejects public extern table receipts. Guest store stubs publish without receipt tracking only where conversion initialization is disabled. The final evidence correctly does not promote those excluded host-receipt paths to passes.
4. RV64 is a separate bounded production compiler, not feature parity by inference. `riscv64/compile_production_module.go:17-43` retains wire admission, and `:44-83` rejects unsupported ABI/provenance combinations. The actual RV64 gates cover admitted products; they do not prove arbitrary modules outside that admission set.
5. The fresh review replay below ran natively only on Linux/AMD64. Existing immutable evidence supplies actual ARM64 and RV64 QEMU execution. I inspected their source/ABI paths but did not duplicate the full 195-gate campaign.

## Baseline/introduced distinction

No verified defect exists to triage. The campaign introduces the checked lifetime replacement, exact roots, extern store frames/helpers, EH-root repairs and the deferred-address fix relative to base. The remaining gaps above are explicit platform/feature boundaries at the source pin, not regressions demonstrated against `9713878ab452848f7eb7549a90be94b938a898be`.

Because there is no verified finding, no sibling defect triage was required. I nevertheless read the committed Slipstream/Starshine cross-check evidence in `combined-compiler-repairs/sibling-audit.json` and `rollup-rooting-assessment/`; neither sibling provides an analogous counterexample that changes this verdict.

## Commands and skips

All source reads and diffs used pinned Git objects (`git show`, `git diff base..source`, `git grep source -- ...`). The dirty worktree and index were not used as source input.

An immutable archive was created with:

```text
git archive 5345bd365c13963f1bf60e26bb80789ab9ead4cc | tar -x -C /tmp/wago-final-review-compiler.UzylIv
```

The initial focused run did not execute tests: inherited `GOROOT` pointed a Go 1.27.1 driver at Go 1.27.3 tools and produced version-mismatch build errors. The corrected run unset `GOROOT`, used `/home/dev/.local/share/mise/installs/go/1.27.1/bin`, and used an isolated `/tmp` build cache. In each of ordinary, `wagodebug`, and `wago_regalloccheck`, these passed:

```text
go test [mode tag] ./src/core/compiler/backend/railshot/shared ./src/core/compiler/backend/railshot/amd64 ./src/core/compiler/frontend -run 'Test(GCModuleFrameRootPlan|GCFrame|CommuteMemoryPreservesDestinationAddress|ExternHostStore|ExceptionRoot|GCLifetime|LowerGC|.*Safepoint)' -count=1
go test [mode tag] ./src/wago -run 'Test(GCScalar|GCException|GCExtern|GCForeign|EH|ImmutableNone)' -count=1
```

No benchmark or timing command was run. Test framework durations were incidental correctness output. Darwin was skipped because no execution runner is established. Full-suite, race, QEMU, TinyGo, corpus and diagnostic matrices were not duplicated; their pinned receipts were reviewed in `docs/rfwgc-evidence/final-combined-5345/` and `combined-compiler-repairs/`. No production file, submodule, pre-existing worktree modification, index entry, PR or remote state was changed.

## Handoff

The parent should consolidate this as a no-verified-defect compiler slice with the five explicit coverage boundaries above. Any later source repair requires a fresh review; this report says nothing about source after `5345bd365c13963f1bf60e26bb80789ab9ead4cc`.
