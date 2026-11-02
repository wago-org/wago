# B host-import implementation and pinned qualification

B source commit: `7b14d483a9d4774b9e87ea3f3f14b4190eb3c9e8`.
Qualification source: `3b04e68376024c689ec651adf1817955ea75d707` (A + B + parent lifetime-boundary race repair).
Source archive SHA256: `fd3a97eed70c9a762328454952336fb98a993ac64180e4d978f92dac4faff213`.
Prepared patch SHA256: `35d9ef36fabff1042b4eae9b060d15994e5cf00fae665ed54fef7bd2da7666f5`.

The four source hashes were rechecked before the explicit-path source commit and again after concurrent parent commits. No patch reapplication was performed. The B commit contains only `gc_conversion_store.go`, the host-import test, and the AMD64/ARM64 backend contract test files. Helper IDs 53–58, helper signatures, opaque words and the frozen adapter API were preserved. Parent race repair, A paths, shared logs/scripts, submodules, and the later lint-worker reservations were not edited by this worker.

## Behavioral coverage

Nine host-import tests executed in every ordinary/wagodebug/wago_regalloccheck normal/race run and every ARM64 QEMU mode. They distinguish real host handles with nil Instance owners from invalid handles; exercise real compiled global/table stores, bulk fill/copy/init/grow, both overlap directions, bounds and failed-growth rollback, initialization trap ordering, aliases and independent allocations, ordinary/null/foreign values, and public forwarding. Old collector-free compiled writers remain usable after publisher retirement. The compiled converted cycle asserts survival under an external host root and exactly one final physical release outside the relevant locks. The authority test asserts ordered admission of two independent domains, rejection without invocation authority, and contention retry/drain without lost wakeup.

Supported regional cases run with shifts 0 and 6; no unsupported-region skip is represented as coverage. A's six host-container tests and existing receipt/store-hook regression cells also execute.

## Completed gates

- All 17 qualification regex cells: 102 normal/race executions across the three modes, plus nonempty test-list guards. Zero skips.
- Nine B tests in each of six normal/race runs. Zero skips.
- Both backend contract packages: three tests each in every mode, with list guards. Zero skips. These are contract tests on AMD64, distinct from actual ARM64 QEMU execution.
- Unchanged integrated driver: 18/18 commands exit zero, driver unchanged, source export unchanged. Cross commands are compilation only.
- Unchanged target driver: 14/14 commands exit zero; verified QEMU package hash, ELF machine checks and runtime architecture probes. ARM64 executes 49 top-level tests per mode (including all nine B and six A tests); RV64 executes 45 separate compact-extern/control tests per mode. Zero skips. RV64 does not qualify host opaque receipt adapters.
- Unchanged TinyGo driver: 3/3 modes pass, five tests each, zero skips; compatibility smoke only, with host receipts excluded.
- Full-package race: all three modes PASS with 2,511/2,505/2,511 top-level PASS assertions, 44/50/44 explicitly enumerated skips and zero failures. Source export unchanged. This is separate from the integrated driver's focused race selector; see `full-race-results.json`, `full-race-final.json` and `full-race-skip-admission.json`.

`acceptance-summary.json` records every driver suite's PASS/FAIL/SKIP counts. Integrated full Wago skip counts are 44/50/44; backend counts are 552/554/553 (ordinary/wagodebug/regalloccheck). Every skip is enumerated with its admission message in `skip-admission.json`; no unresolved admission reason remains. Diagnostic-tag cases, optional external fixtures, guarded-page cases and architecture-specific skips remain explicit limitations, not passes. Full-package race skips are separately enumerated.

## Integration limits

This immutable run predates the subsequent AMD64 compiler repair, diagnostic-emitter test adjustment and lint cleanup. It cannot qualify those later changes. The next combined pin must include all fixes and rerun the required gates, including the separately required compiler checked invariant, diagnostics and staticcheck lanes. Parent owns final closure; H2 is not closed by this report. No production defect was observed by the completed commands on this source pin. No benchmark, timing workload, measurement lease, subagent, PR or push was used.

READY TO MEASURE: branch `experiment/rfwgc-port`, B source commit `7b14d483a9d4774b9e87ea3f3f14b4190eb3c9e8`; performance-sensitive path `src/wago/gc_conversion_store.go`; proposed workloads are imported host global/table single stores and fill/copy/init/grow, with old writers and multiple conversion domains. Measurement belongs to the separate lease holder.
