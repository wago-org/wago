# Recovered immutable f2bb correctness qualification

Source: `f2bb27287220783e86a4ead41fee78693a5129de`. Evidence commit: supplied in the final handoff; no later source head is qualified.

Local required-gate verdict: **FAILED / INCOMPLETE**. Darwin/Windows remote verdicts belong to the parent. No review, inclusion, performance, or upstream-readiness claim.

## Blocking TinyGo finding

`tinygo-34/guest-threads-ordinary` exited 1. `TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries/cross-runtime` and `/forged` returned `opaque converted publication is unsupported on this target`; the existing test requires `invalid externref token`. See `blocking-finding.json` and `tinygo-34/guest-threads-ordinary.log`.

`Instance.SetGlobalValue` calls `setConversionGlobalPublic` at `src/wago/instance_call.go:348`. The TinyGo/unsupported-target adapter in `gc_conversion_store_other.go:16-35,50-64` returns its opaque-conversion rejection for unauthenticated words before the ordinary invalid-token checks. This is a verified error-classification/test-contract failure. No memory corruption is demonstrated; the failing assertion precedes the post-rejection null-state check.

All later qualification execution stopped. The 34-command lane completed only host-ordinary and standalone-ordinary successfully, then failed guest-threads-ordinary. Its remaining 31 commands, nine evacuation/joint/conversion supplements, and three repair/public/rejection supplements are NOT_RUN. No results from older pins close these 43 gates. No targeted rerun or production/expectation edit was made after the failure.

Read-only sibling audit: Slipstream `dcb45b56386327a1f71fb039ed02e13562e1e2d7` uses different tagged references and trap-based host authentication; Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03` exposes a model/codec/validator boundary. No analogous Wago/TinyGo error-classification adapter was identified in the inspected paths. `sibling-cross-check.json` records pins and hashes; no sibling defect is asserted.

Parent owns triage and any production repair that distinguishes invalid/cross-store tokens from genuine converted wrappers while preserving converted-publication rejection, followed by fresh exact-pin qualification. No READY TO MEASURE claim.


| Gate | Ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Full root ./... | PASS | PASS | PASS |
| Runtime-tagged CLI | PASS | PASS | PASS |
| Installer nested module | PASS | PASS | PASS |
| Bench nested module | PASS | PASS | PASS |
| Full collector family | PASS | PASS | PASS |
| Full collector/runtime/Wago/integration race | PASS | PASS | PASS |
| Reduced parent reference-store race | PASS | PASS | PASS |
| Full runtime/Wago guard pages | PASS | PASS | PASS |
| Canonical candidate corpus | PASS | PASS | PASS |
| Fresh matched baseline corpus | PASS | PASS | PASS |
| Explicit RFW catalog, no inline + lifetime analysis | PASS | PASS | PASS |
| Actual candidate GC graphs and scalar/noGC controls | PASS | PASS | PASS |
| Baseline GC graphs and controls | PASS | PASS | PASS |
| Official spec v1 | PASS | PASS | PASS |
| Official spec v2 | PASS | PASS | PASS |
| Official spec v3 | PASS | PASS | PASS |
| Official spec v3 signals/guard pages | PASS | PASS | PASS |
| Guard-page corpus | PASS | PASS | PASS |
| Full backend | PASS | PASS | PASS |
| Full default backend diagnostics + Wago | PASS | PASS | PASS |
| Full WAGO_SHARED_SCALAR=0 diagnostics + Wago | PASS | PASS | PASS |
| Root build | PASS | PASS | PASS |
| Root vet | PASS | PASS | PASS |
| Runtime CLI build | PASS | PASS | PASS |
| Profile metadata | PASS | PASS | PASS |
| Profile runtime/unwind | PASS | PASS | PASS |
| Explicit shared-scalar controls | PASS | PASS | PASS |
| Native joins | PASS | PASS | PASS |
| Four seeded concurrency runs, 24 rounds each | PASS | PASS | PASS |
| Actual TinyGo serial evacuation | NOT_RUN | NOT_RUN | NOT_RUN |
| Actual TinyGo joint/foreign-stack supplement | NOT_RUN | NOT_RUN | NOT_RUN |
| Actual TinyGo supported conversion compatibility | NOT_RUN | NOT_RUN | NOT_RUN |
| Collector hardening, three repetitions / explicit bounds | PASS | PASS | PASS |
| Collector hardening, three repetitions / guard pages | PASS | PASS | PASS |
| Host transitions, three repetitions / explicit bounds | PASS | PASS | PASS |
| Host transitions, three repetitions / guard pages | PASS | PASS | PASS |
| Actual QEMU arm64 collector | PASS | PASS | PASS |
| Actual QEMU arm64 facade | PASS | PASS | PASS |
| Actual QEMU arm64 guest | PASS | PASS | PASS |
| Actual QEMU riscv64 collector | PASS | PASS | PASS |
| Actual QEMU riscv64 facade | PASS | PASS | PASS |
| Actual QEMU riscv64 guest | PASS | PASS | PASS |
| Actual ARM64 H6 ownership/wakeup adapter | PASS | PASS | PASS |
| Actual full RV64 managed-function controls | PASS | PASS | PASS |

| Repository / supplemental gate | Verdict |
| --- | --- |
| Generation | PASS |
| Generated-file diff | PASS |
| Formatting | PASS |
| Docs | PASS |
| Actionlint | PASS |
| Staticcheck, all four reviewed profiles | PASS |
| Native-compare recipe | PASS |
| Portable 386 package execution | PASS |
| RISC-V CI script contracts | PASS |
| Node update-website-bench | PASS |
| Node update-website-startup | PASS |
| Node engine-state-oracle | PASS |
| Node fuzz-engine-state | PASS |
| build-release-assets.sh | PASS |
| release-qualification.sh | PASS |
| ci-download-go-modules.sh | PASS |
| check-diagnostic-dce.sh | PASS |
| check-riscv64-diagnostics.sh | PASS |
| check-regalloc-code.sh | PASS |
| Actual patched TinyGo 0.42 foreign-stack lane, all 34 commands | FAIL |
| Configured bounded fuzz runs | PASS |
| Extended 30-second bounded fuzz runs | PASS |

| Repair / policy gate | Ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Full package discovery | PASS | PASS | PASS |
| Full package compilation | PASS | PASS | PASS |
| Full CI policy package | PASS | PASS | PASS |
| DR regression census | PASS | PASS | PASS |
| Actual TinyGo ordinary/public/rejection controls | NOT_RUN | NOT_RUN | NOT_RUN |
| Actual arm64 repair QEMU census | PASS | PASS | PASS |
| Actual riscv64 repair QEMU census | PASS | PASS | PASS |

Canonical checked baseline compile preflight: **PASS**.

Required receipts: `{'PASS': 173, 'NOT_RUN': 43, 'FAIL': 1}` across 217 gates (the prior 195 names plus 22 required additions). `final-summary.json` is authoritative; no old-pin pass is inherited.

## Recovery and retained attempts

The prior provider left no surviving test process in the initial process inventory. Its early all-mode CI policy, package compilation/discovery, DR census, RFW catalog and reduced-race receipts were verified against their raw-log hashes, driver hashes, unchanged exported sources and tools before reuse. A fresh Git archive exactly matched the recorded f2bb archive hash. Incomplete discovery/root/wrapper attempts remain interrupted, never passes.

The prior ordinary root run hit ENOSPC in a TinyGo build. The recovery uses separately hashed resume drivers with private GOCACHE/XDG caches under the owned /tmp export. During recovery the shared home filesystem filled again; an attempted receipt compression failed, was retained, and was repaired through verified lossless compression in /tmp. A temporary new-gate stop was lifted after the parent freed its own completed preparation cache. Unrelated processes/caches were not killed, moved or deleted. The repeated root gate and all subsequent terminal receipts remain separate from the interrupted attempt.

`recovery-inventory.json`, `recovery-processes.txt`, `recovery-integrity.json`, `recovery-source-pin.json`, compression manifests and the before/after driver/tool manifests retain the recovery chain. Raw logs compressed as gzip preserve their uncompressed-content hashes. The ordinary resumed root result is likewise retained losslessly as `result.json.gz`.

The native-compare required name may point to `tooling-native-compare`: it executes the same explicit just recipe in the isolated tooling export. This alias is recorded in `receipt_suite`; it does not reuse an older source result.

## Exact coverage and limits

The canonical corpus identity is 128 IDs, 121 distinct binaries and 47 input references. `corpus-per-id.json` lists all 768 expected ID/revision/mode rows and `corpus-joins.json` lists all 384 comparisons. Their statuses, not mere row existence, establish completion. Baseline source is `a3c08365f531eb3c9003e3acf3e0a822c12c31f4`. Only its canonical checked preflight/execution and declared graph controls are allowed; unsafe baseline no-inline/lifetime-analysis clang-cpp execution never runs.

`catalog-per-id.json` separately accounts for 384 candidate RFW compilation rows. `generated-graph-identities.json` identifies real WasmGC graph and scalar/noGC control fixtures; canonical application corpus coverage is not graph coverage. `spec-accounting.json` preserves per-file official v1/v2/v3/signals totals and gaps. `exact-skip-census.json` and raw logs retain optional/architecture/reference skips without promoting them to execution. It also records 32 TinyGo boundary-helper early returns that the runner prints as PASS; those bodies are not execution claims.

`repair-final-census.json` requires the original DR probes and repaired admission, wait/notifier, scalar-relay, concurrent publication/retirement and ordinary table families; it also checks that each race command covers full collector/runtime/Wago/integration packages without a test-name filter. Target coverage and TinyGo supplement censuses require named terminal test passes. Build-only receipts are separate from actual execution.

RV64 compact controls do not establish excluded opaque host-receipt support. TinyGo supported conversion and ordinary/public/rejection controls do not establish opaque host receipts. ARM64 H6 adapters and target repair adapters are recorded with source/binary/ELF identities. Darwin/Windows remote execution verdicts are parent-owned; no local cross-build is promoted to execution.

## Integrity and handoff

Final integrity reports cover immutable original exports, declared test overlays/adapters, drivers, tools, raw receipts and index/worktree preservation. `process-drain.json` describes owned-process drainage only, not a global measurement lease. See the final handoff for the evidence head commit.

Only this evidence directory is committed. No production or expectation edits, benchmarks, performance samples, measurement lease, PR, push, subagents or Claude review. These evidence-only files do not change shipped runtime/compiler paths and cannot affect their performance. Test-framework durations are incidental correctness output.

Rebuild summaries without executing qualification commands: `recovery-report.py`, `accounting.py`, `recovery-ledger-v2.py`, then `recovery-render-stopped.py`. Coverage/census scripts are separate assertions and must also succeed for a completed qualification.
