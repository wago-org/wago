# Combined qualification stopped on repository defects

Source: `9180f09f7590d029bc90df2fa20c0decc590505a`.
Evidence input: `534eebc495d3a1bea0a4115dd44ec8e69cb74d47`.
Verdict: **FAILED / INCOMPLETE**. No correctness release, final approval,
inclusion, performance, PR, or READY TO MEASURE claim. November 4 does not waive
any gate. Earlier 195 passes qualify only the old defective source.

## Blocking findings

1. `docs/rfwgc-evidence/final-review-design/probes` is discovered by root
   `go test ./...` as a standalone package. Its two archived probe files reference
   Wago internals (`NewRuntimeConfig`, `Compile`, `instantiateCore`, etc.) that
   do not exist in that package. Compilation fails. The durable copies in
   `src/wago` pass; that does not repair root package discovery.
2. `TestAggregateCIRequiresEveryWorkflowJob` fails: `ci-ok` does not depend on
   `darwin-qualification` and `darwin-qualification-summary` in the frozen pin.
   This is an actual CI policy test failure, not a missing local Darwin runner.

Both findings reproduce with exit 1 in ordinary, wagodebug, and
wago_regalloccheck; see `defect-reproductions.json` and `reproduce-*` logs.
After detection, qualification commands were stopped. Only deterministic
package compilation and CI policy reproductions ran afterwards. No production,
workflow, or expectation edits were made. These fixes lie outside owned evidence.
Read-only sibling audit in `sibling-cross-check.json` found no analogous root Go
module/probe package or Wago CI aggregate policy in the inspected Slipstream and
Starshine pins; no sibling defect is asserted.

## Completed evidence and remaining gates

- DR focused tests and seven required regression-family census: PASS all three
  modes, including original DR probes, wait/notifier, conversion admission,
  scalar relay, concurrent publication/retirement, and ordinary public table.
- Explicit RFW compile catalog: 384 passing rows, all three modes. This is
  compilation, not canonical application execution.
- Official spec v1/v2/v3: PASS all three modes with zero gaps. Per mode:
  v1 629 modules / 16,026 assertions; v2 1,600 / 48,331;
  v3 2,226 / 58,238. Signals v3: ordinary PASS only.
- Ordinary full collector, reduced reference-store race, and root build: PASS.
- Actual ARM64 repair QEMU executions: ordinary and wagodebug PASS; remaining
  modes/targets interrupted or not run. Build-only receipts are not execution.
- Root suite FAILED before cancellation. Full collector/runtime/Wago/integration
  race was launched (not scoped) but interrupted. No full-race pass is claimed.
- Canonical identity verified: 128 IDs, 121 binaries, 47 input references.
  Baseline `a3c08365f531eb3c9003e3acf3e0a822c12c31f4` canonical compile preflight
  was interrupted; baseline execution never began. All 768 corpus rows and
  384 paired comparisons remain incomplete. Unsafe baseline noinline/lifetime
  clang-cpp execution never ran.
- Nested CLI/installer/bench, remaining root modes, guard/hardening,
  generated real-GC/noGC/scalar graphs, full backend and both diagnostic routes,
  four Staticcheck profiles, generation/lint/docs/scripts/build/vet completion,
  native joins/concurrency/fuzz/profiling, full ARM/RV QEMU families and census,
  patched TinyGo 34-command lane, evacuation and conversion/public-table/rejection
  additions are interrupted or NOT_RUN as indexed. No old pass is carried forward.
- Darwin ARM64 and Windows AMD64 execution: NOT_RUN; no executable runner
  established locally. Cross-build cannot close either gate. RV64 compact
  controls and TinyGo conversion compatibility cannot establish unsupported
  host-opaque receipt support. See `target-exclusions.json`.

`final-summary.json` is the authoritative terminal ledger; it distinguishes
cancellation from completed failures. `qualification.json` preserves the inherited
reporter's raw exit-based classification (including SIGTERM as FAIL).
Per-command receipts and raw logs retain all observations and optional skips.
`spec-accounting.json`, `catalog-per-id.json`, `corpus-per-id.json`, and
`corpus-joins.json` provide the detailed accounting. Unexecuted generated-graph
identities and reference rows are not evidence of coverage.

## Integrity, harness attempts, and handoff

All test sources came from immutable Git archives, including pinned spec
submodules. `source.json` and archive/export manifests identify them.
`original-drivers/` preserves final-combined-5345 drivers byte-for-byte from the
specified evidence commit. Adapted drivers are separately hashed before use;
no running driver was edited. Driver and original exported-file checks pass
with zero changed originals. Target-only added architecture/portability adapters
are described in target source receipts. TinyGo rejection adapter was prepared
as a driver but never installed/executed.

The first census reporter passed ordinary tests and its required-test assertions,
then failed while sorting a package-level event represented by None. That attempt
and immutable driver remain; `repairs-v2.py` filters package events and completes
all three censuses without changing tests or expectations. The initial AGENTS
lookup in the worktree found no file; the supplied instructions and
`/home/dev/Projects/wago/AGENTS.md` were read.

The first drain found an owned TinyGo timeout process group surviving its outer
shell; its failed drain and explicit termination are retained. The final
`process-drain.json` confirms no owned runtime processes remain. Private archives
are retained for diagnosis; they are not worktrees. No global cache or unrelated
process was removed. `preservation.json` records index equality and concurrent
changes by other workers. This worker owns only this evidence directory.

No benchmarks, performance timings, measurement lease, subagents, Claude review,
PR, or push. Test-framework durations are incidental correctness output.
Parent owns fresh review, repairs, the next exact-pin rerun, and final decisions.
Evidence-only changes cannot affect shipped performance.
