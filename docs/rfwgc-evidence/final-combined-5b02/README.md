# Immutable combined qualification: 5b02 plus explicit later deltas

**Completed, not accepted: 216 PASS / 4 FAIL / 0 unrun across all 220 required 5b02 gates.** Full-suite source: `5b02ec67afe0dce181a7f8d508e270c7ecb0ecfc`. Full-run checkpoint: `7496854a54467817e78204f70bad20d9d23f5992`. `FINAL-STATUS.json` is the combined disposition; `final-summary.json` is the canonical 220-cell ledger. Legacy `qualification.json` also includes auxiliary recipes and displaced aliases; its raw totals are not the required-gate census.

| Evidence | Actual execution pin | Result |
|---|---|---|
| Full combined scope | 5b02ec67afe0dce181a7f8d508e270c7ecb0ecfc | 216 PASS, 4 FAIL; all 220 completed |
| Fresh formatting, RR + multitable all3, CI policy/planner all3, actionlint | 041f7ed4c376e2a33ce21b22075cd617ddca6597 | 8 PASS |
| Reused actual patched Linux TinyGo resume oracle | 4913c91e1ad5636543d0ea7eed740b16de1b9bf2 | 3 positive PASS; 3 expected wrong-field negative FAIL |

No full-suite result is relabeled as execution on a later pin. The prepared ca164 delta driver was never executed. The later 041f driver was explicitly authorized and ran only after the full 5b02 process drain.

## Failures and acceptance

The 5b02 formatting gate failed on `docs/rfwgc-evidence/darwin-final-codegen-recheck/_probes/branch19_repatch.go` and `src/wago/rr_recheck_flags_current_test.go`. Exact source identities and whitespace-only diff are retained. Fresh 041f formatting passed; the old failure remains in its original ledger.

Each original ARM64 QEMU repair cell (ordinary, wagodebug, wago_regalloccheck) failed:

- `TestRRRecheckStorageOnlyReturnBehindDynamicReader/{none,table}`: `create native deadline timer: invalid argument`.
- `TestSharedEntryGuardCancellationAndGC`: `native loop did not cancel`.

These match previously documented failures. The guard failure is not attributed to QEMU without proof. Deadlines, signal selection, source tests, and expectations were not changed. The original ordinary attempt and exit are retained. A separate hashed finite continuation verified source/tool/driver/raw-log identities, reused the completed ordinary cell without rerunning it, and completed remaining cells while allowing only these exact known controlled failures. All three RISC-V repair cells passed. See `qemu-repair-blocker.json`, `qemu-repairs/continuation-before.json`, `qemu-repairs/continuation-final.json`, and `repair-final-census.json`.

**The delta results do not establish final-head acceptance while these three required ARM64 cells remain failed.** Native ARM64/Darwin discrimination and final approval belong to the parent. Native 5847 receipts, Darwin 7e41 receipts, and the parent native overlay rerun are separately pinned evidence, not current-head local qualification. No new verified production defect or runtime repair estimate is asserted for the unresolved guard mechanism.

## Completed scope and exact censuses

All three modes passed full root, CLI, installer, nested-module tests; full GC/runtime/wago/integration race; collector/guard; backend and both diagnostic routes; repeated hardening and host transitions; profile, native join, shared-scalar and seeded concurrency checks. All ten bounded fuzz gates passed. All four Staticcheck profiles had zero new findings. Generation, docs, lint/scripts, build/vet and applicable tooling checks passed apart from the preserved old formatting gate.

The canonical corpus has 128 IDs, 121 distinct binary hashes, and 47 input records. All 768 candidate/baseline mode rows passed and all 384 paired joins completed. Baseline `a3c08365f531eb3c9003e3acf3e0a822c12c31f4` passed checked compile preflight before execution; no unsafe no-inline clang baseline was executed. All 384 RFW catalog ID/mode rows passed. All 12 spec lanes (v1/v2/v3/signals × three modes) verified exact totals, zero gaps and zero skips. Six real-GC-graph/control suites passed with 18 exported binary identities.

Actual QEMU collector, facade and guest families passed in all three modes on ARM64 and RISC-V; ARM64 H6 and full RISC-V managed-function controls passed. Original source blobs are unchanged; declared test-only portability adapters are recorded separately, including the guest graph-construction adapter. These are emulator executions, not native hardware claims.

Actual patched TinyGo passed the full 34-command lane, all nine evacuation/joint/conversion supplements, all three public-rejection supplements, and all three natural-build-selection unsupported-global followups. All eleven CLI Fibonacci checks produced 6765. No tag adapter substitutes for the unsupported-global followups.

`exact-skip-census.json` retains 6,300 skip/exclusion records across repeated suites: 5,893 Go JSON records, 22 target skips with reasons, 257 plain-runner records, and 128 TinyGo excluded bodies despite terminal PASS. These are not unique-test counts. Excluded bodies are not claimed executed. `reference-only-skips.json` separately identifies 72 corpus rows with optional reference-engine skips; candidate/baseline expected-result checks passed.

## Later delta and external receipt integrity

`final-head-delta-041f-plan.json` compares the complete trees. Exactly five non-doc paths differ: the CI workflow, its policy tests, RR-helper whitespace, the canonical TinyGo physical-stack oracle, and the multitable capability predicate. All 2,447 production/support entries are identical. The fresh 041f export verifies every tracked blob and exact gitlinks, runs eight gates, and verifies unchanged source afterward. See `delta-041f/summary.json` and per-command receipts.

`reused-4913-tinygo.json` verifies evidence commit `375e47bb81ee2969615cf48ec31b94eb1edf8daf`: all checksum-listed committed artifacts, 6,151 restored export/Git-blob identities, archive, patched compiler/runtime hashes, binary hashes, exact serialized argv, raw positive/negative logs, and drained processes. The 479 runtime/bridge/module dependency entries are identical between 4913 and 041f. **Provenance limit:** the Linux launcher was a shell here-document; no contemporaneous launcher file/hash survives. The parent explicitly authorized reuse of the pinned command recipe and other manifests with this limit disclosed. No original launcher was invented or reconstructed, and no targeted test was duplicated.

## Retention and ownership

`source.json`, `source-identity.json`, tool manifests, frozen driver hashes, `integrity-latest.json`, `all-export-integrity.json`, and `nested-integrity.json` preserve source/tool/driver/raw-log proof. Every original blob in all seven actual TinyGo/QEMU exports was reverified. `base-process-drain.json` preserves the pre-delta drain; `process-drain.json` records the final drain. All owned commands finished.

Raw logs are losslessly compressed. `compression-ledger.json` retains original and compressed hashes for 105 additional logs; generic command logs were already compressed. The final skip census is preserved verbatim. Some frozen historical collectors glob plain `.log` files; use the archived census or losslessly restore those logs before rerunning such collectors, rather than overwriting the census with an incomplete scan. No test rerun is needed for audit.

Only this evidence directory and its recorded external scratch/cache were owned. Shared dirt/index/submodules were preserved. No production edits, measurements, benchmark execution, push, PR, subagents, or new review were performed. All historical failures and partial attempts remain attributable to their original pins.
