# Native run 41431926958: Darwin qualification FAIL; native cancellation PASS

Run: https://github.com/wago-org/wago/actions/runs/41431926958 (completed, attempt 1).
Source **da19f1a8d29fec7305d937a42622a198b8e79717**, harness
**336416d2a006bbefded9e5b0b8539199bd6b0e3b**, activation review
**2bccd8713772af59d047810c9d915334e9b539d0**. Original `dispatch.json` is unchanged.
All three Darwin jobs failed; all three Windows jobs passed; special summary
failed and 20 ordinary CI jobs skipped. This is not full CI success.

The source includes the ARM64 join/helper-fixture/worker-comparator repairs and
c786 token authentication. Later RR-1/RR-2 (`9ad…`) and RR-3 runtime (`5847…`)
repairs are outside this execution pin. The separately reported new RR-3 ARM64
QEMU timer failure is not this run's native cancellation outcome.

## Immediate blocker and minimal ownership scope

**Native proof, identical in all three modes:** the Core 3 child emits
`spectest_exec_test.go:1833: gc/const-expr-gc.wast.wast:96 module instantiate rejected: opaque converted publication is unsupported on this target`.
The retained commands map the line-96 `module_instance` to `commands.5.wasm`.
The subsequent line-97 assertion invokes `extern-convert-any-const`, expecting 55.
The child does not emit an independent reason for that skipped assertion.
Generated command coordinates are not physical line numbers in the retained,
commented `source.wast`.

The authenticated protocol-2 marker occurs exactly once, with the expected
fixture and nonce `00112233445566778899aabbccddeeff`: **9 modules passed,
1 skipped, 0 failed; 17 assertions passed, 1 skipped, 0 failed**. The child has
exactly one top-level test PASS and process exit 0. The required 10/18 zero-skip
census fails, correctly making the diagnostic lane FAIL. Go skip events are zero;
the missing module/assertion are protocol payload skips, not Go test skips.

**Static source explanation, distinct from the native emitted reason:** the
sixth module contains an externref global initialized by
`extern.convert_any (struct.new $s (i32.const 55))`. The non-Linux adapter's
`initializeConversionReceipts` calls `unsupportedConversionWord` for externref
globals and rejects unsupported converted identities. The output confirms the
rejection phase/reason and module; it does not supply a production stack trace.
Closing the 10/18 gate requires a production ownership/lifetime/publication
implementation (initialization and persistent GC stores), centered on
`src/wago/gc_conversion_store_other.go` and its receipt/rooting integration.
No census reduction, expectation change or harness-only fix can qualify it.
The narrow harness already exposes the exact failure correctly.

A second native failure in all modes is
`TestExternrefGlobalHoldingConvertedGCReference`,
`externref_global_conversion_test.go:77: SetValue converted rejection: <nil>`.
The test first verifies converted initializer rejection, creates a null global,
checks converted result roundtrip, then expects `g.SetValue(make(42))` to reject.
It instead returns nil. Native evidence stops at that fatal assertion: subsequent
unchanged-global, roundtrip and null-reset assertions are **not executed**.
This is a verified contract mismatch; it does not prove a dangling root, memory
corruption, or the exact token representation. Minimal production investigation
scope is `globals.go:Global.SetValue`, `gc_conversion_store_other.go:
setConversionGlobalPublic/unsupportedConversionWord`, and the result-token
classification path. The pinned adapter rejects ownership wrappers but accepts
authenticated ordinary slots; identifying which branch this actual result uses
requires further targeted evidence. Do not alter the test to accept nil.

## Gap matrix (same outcome in ordinary, wagodebug, wago_regalloccheck)

| Gate | Observed per mode | Verdict / ceiling |
| --- | --- | --- |
| Unchanged cancellation | 1 exact narrow PASS, exit 0, no skips | Native Darwin closed at this pin; not Linux/QEMU closure |
| Core 3 child | 9/10 modules, 17/18 assertions, 1/1 skipped | FAIL, unsupported converted initialization |
| Ten repaired contracts | 9 PASS, 1 FAIL, no skips | Global SetValue rejection mismatch |
| Focused Darwin contracts | 5 top-level PASS, no failures/skips | PASS at pin |
| Generated candidate graph leaves | 6 exact PASS | noGC/scalar/graph × false/true; hashes match |
| Broad commands | 21 attempted, 13 pass or pass-with-skips, 8 fail | FAIL; skips require review |
| Broad Core 3 total | 205/215 modules, 672/690 assertions | Entire failing 10/18 fixture excluded by parent accounting |
| Windows | 4 exact contracts PASS, no failures/skips | Storage/rejection/admission only; no GC JIT qualification |
| Patched native TinyGo | No execution | NOT_RUN / OPEN |
| Matched baseline graphs at a3c08365f531eb3c9003e3acf3e0a822c12c31f4 | No execution | NOT_RUN / OPEN |
| H6 Darwin ownership | Unsupported adapter remains | OPEN; no implementation credited |
| Later RR repairs / new QEMU timer failure | Outside source pin | Not qualified or adjudicated by this run |

The other nine repaired tests pass individually: `TestGCExternConversionAcrossReferenceTables`,
`TestFixedSharedDirectGoImport`, `TestFixedSharedDirectGoCallerScope`,
`TestFixedSharedDirectGoImportPolicy`, `TestSharedSingleEntryGuardAndGoFallback`,
`TestSharedEntryRegisterResultAndGoFallback`, `TestSharedEntryRegisterArgumentAndGoFallback`,
`TestSharedEntryGuardForeignFallback`, and `TestSharedSingleNativePinsAndGoFallback`.
All ten terminal outcomes are retained in `diagnostics/<mode>/repaired-contracts-<mode>/raw.jsonl`.

The cancellation test additionally has four broad PASS occurrences per mode;
`closure-events.json` records each exact member/package/test. Its intrinsic
three-second deadline and admission/GC assertions were not changed. This is
actual native completion, not an inference from absence in a failure list.

## ARM64 join and prior-failure closure evidence

`TestDynamicImportUnwindLocalJoinARM64` and
`TestNativeLeafContextShortcutGCPolicyARM64` each pass four times per mode with
no fail/skip events. All recorded `TestExample` executions pass, including the
formerly failing public API example; no raw broad output contains
`direct Go import local-state join differs`. Backend, corpus, guard-corpus and
spec-v2 now pass (some have recorded skips). This supplies native closure for
this repaired join and its executed regressions, not a proof of all ARM64 codegen.

The table GetSet/Init/Fill/Grow/Copy fixtures, `TestBulkPhysicalArgumentSlots`,
`TestTableSetHelperCallHintsArm64`, and `TestWorkerModuleStatsComparisonARM64`
each pass four times per mode. Worker determinism tests have two PASS and two
SKIP occurrences per mode, so only their executed variants are credited.
The c786-related `TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries`
and its forged/cross-runtime leaves pass. No blanket inference from lower failure
counts is used: the exact terminal event receipt supports these statements.

Compared with prior run 41396042416, broad failed lanes fall from 11 to 8 and
failure events from 376 to 78 per mode. Prior archives remain untouched. Different
pins and repaired fixtures mean this comparison is correctness evidence only,
not a performance comparison or proof of unexecuted cases.

## Remaining failures, separated by scope

- **Production unsupported-publication gate:** Core 3 as above. Spec-v3 and
  spec-v3-signals both execute 2,226 modules successfully, but have 58,233 passed
  and five failed assertions in every mode. `gc/ref_test.wast:44 init("0")`
  traps with `wago: WasmGC struct helper: opaque converted publication is unsupported on this target`.
  Lines 102/103 return `0x2` instead of 0; lines 108/109 return `0x1` instead of 2.
  The failed initializer plausibly leaves state explaining the later mismatches;
  that causal attribution is source inference, not separately proven by the log.
  All five concrete native diagnostics are in `spec-v3-diagnostics.json`.
- **Production ingress contract mismatch:** global SetValue returns nil as above,
  repeated in root, race and diagnostics lanes. Synthetic unsupported-token
  focused tests still pass; they do not substitute for this actual result path.
- **Compiler selection / fixture-policy mismatch:**
  `TestNativeImportVariantSourceArtifactAndCalls` fails all four compact/pair
  combinations at line 123, “guarded variant selector did not add a second body”;
  `TestNativeImportVariantTrap` fails line 91, “trap fixture did not exercise guarded body”.
  Native proof is missing expected specialization, not a wrong result in a body
  that ran. Static eligibility in ARM64 `compile.go` depends on remaining pins,
  X24 reservation, memory/import policy and environment selectors. The precise
  rejected eligibility predicate is not printed. Audit these fixtures against
  the repaired allocation policy before choosing a compiler or fixture repair;
  do not disable their assertions here.
- **Diagnostics-only expectations:** default diagnostics fail const-fold,
  same-operand and other peephole counts; wrapper parameter pairs are 16 versus
  expected 8; entry-zero pair count is 0 versus 32; interval-region, entry-init,
  local-pin and shifted-immediate counts are missing; zero-branch delta is 0
  versus 4. The established diagnostics configuration eliminates these extra
  failures, leaving the global/native-variant/Core 3 families. This supports a
  configuration/expectation investigation, not an unsupported claim that all
  remaining failures are harmless. Exact names/details are in each failure index.
- **Installer module setup:** dedicated installer census and test command both
  exit 1; no installer test passes. Error: “missing go.sum entry needed to verify
  package github.com/wago-org/wago/cli/wago-installer is provided by exactly one module”.
  `TestGoInstallBuildsNamedInstallerCommand` repeats this in root replay.
  Pinned nested `go.mod` requires Wago v0.1.0-beta.13 and has no tracked `go.sum`.
  Minimal scope is nested-module dependency/checksum preparation or an immutable
  harness dependency fixture, not ARM64 runtime. This evidence worker is forbidden
  source/harness changes or reruns; no proposed dependency repair is credited.

Read-only sibling cross-check was completed before final production triage.
Slipstream's inspected host resource path uses owner/epoch/slot checks;
Starshine's WAST lowering represents ExternConvertAny and delegates runtime
execution externally. Neither inspected tree contains Wago's adapter or its
exact rejection/join diagnostics. Relevant files match the prior audit hashes;
current heads/status and excerpts are in `sibling-audit.json`. No equivalent
sibling defect was verified; this scoped static audit is not runtime qualification.

## Commands, census and evidence integrity

The three diagnostics per mode use `go test -json -v -count=1 -timeout=30s`,
`-tags=<mode>` except ordinary, anchored exact selectors, and `./src/wago`.
The Core 3 lane sets `WAGO_WASMTIME_CORE3_FIXTURE=gc/const-expr-gc.wast`,
`WAGO_REGRESSION_CHILD_PROTOCOL=2`, the nonce above and `WAGO_BOUNDS=explicit`.
`command-census.json` records every exact argv, cwd and declared/effective
recorded environment for all **72** attempted commands: 63 original broad plus
9 diagnostics. Focused contract commands are separately preserved in `receipts/`.
Package discovery succeeds for **69 commands and fails for 3** of 72: the
installer census fails once per mode.

Broad event totals (overlapping selectors and package/subtest failures are not unique tests):

| Mode | Commands | Failed commands | PASS test events | FAIL events | SKIP events |
| --- | ---: | ---: | ---: | ---: | ---: |
| ordinary | 21 | 8 | 99,701 | 78 | 739 |
| wagodebug | 21 | 8 | 100,166 | 78 | 775 |
| wago_regalloccheck | 21 | 8 | 107,763 | 78 | 739 |

Per-command census: cells are **status; pass/fail/skip events; process exit / discovery exit**.

| Command | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| cancellation | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 |
| core3-child | FAIL; 1/0/0; 0/0 | FAIL; 1/0/0; 0/0 | FAIL; 1/0/0; 0/0 |
| repaired-contracts | FAIL; 9/2/0; 1/0 | FAIL; 9/2/0; 1/0 | FAIL; 9/2/0; 1/0 |
| full-root-replay | FAIL; 27376/14/327; 1/0 | FAIL; 27449/14/336; 1/0 | FAIL; 33399/14/327; 1/0 |
| collector | PASS; 2210/0/0; 0/0 | PASS_WITH_SKIPS; 2292/0/3; 0/0 | PASS; 2210/0/0; 0/0 |
| race | FAIL; 5859/2/19; 1/0 | FAIL; 5941/2/22; 1/0 | FAIL; 5859/2/19; 1/0 |
| guard | FAIL; 9143/11/35; 1/0 | FAIL; 9143/11/35; 1/0 | FAIL; 9143/11/35; 1/0 |
| backend | PASS_WITH_SKIPS; 9301/0/225; 0/0 | PASS_WITH_SKIPS; 9301/0/225; 0/0 | PASS_WITH_SKIPS; 9980/0/225; 0/0 |
| cli | PASS_WITH_SKIPS; 999/0/41; 0/0 | PASS_WITH_SKIPS; 999/0/41; 0/0 | PASS_WITH_SKIPS; 999/0/41; 0/0 |
| installer | FAIL; 0/1/0; 1/1 | FAIL; 0/1/0; 1/1 | FAIL; 0/1/0; 1/1 |
| corpus | PASS_WITH_SKIPS; 880/0/12; 0/0 | PASS_WITH_SKIPS; 880/0/12; 0/0 | PASS_WITH_SKIPS; 880/0/12; 0/0 |
| guard-corpus | PASS_WITH_SKIPS; 628/0/1; 0/0 | PASS_WITH_SKIPS; 628/0/1; 0/0 | PASS_WITH_SKIPS; 628/0/1; 0/0 |
| host-transition | PASS; 48/0/0; 0/0 | PASS; 48/0/0; 0/0 | PASS; 48/0/0; 0/0 |
| hardening | PASS; 6630/0/0; 0/0 | PASS_WITH_SKIPS; 6876/0/9; 0/0 | PASS; 6630/0/0; 0/0 |
| profile-runtime | PASS_WITH_SKIPS; 62/0/3; 0/0 | PASS_WITH_SKIPS; 62/0/3; 0/0 | PASS_WITH_SKIPS; 62/0/3; 0/0 |
| profile-metadata | PASS; 157/0/0; 0/0 | PASS; 157/0/0; 0/0 | PASS; 157/0/0; 0/0 |
| diagnostics-default | FAIL; 18139/34/38; 1/0 | FAIL; 18130/34/44; 1/0 | FAIL; 18818/34/38; 1/0 |
| diagnostics-established | FAIL; 18160/12/38; 1/0 | FAIL; 18151/12/44; 1/0 | FAIL; 18839/12/38; 1/0 |
| shared-scalar | PASS; 83/0/0; 0/0 | PASS; 83/0/0; 0/0 | PASS; 85/0/0; 0/0 |
| seeded-concurrency | PASS; 24/0/0; 0/0 | PASS; 24/0/0; 0/0 | PASS; 24/0/0; 0/0 |
| spec-v1 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 |
| spec-v2 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 |
| spec-v3 | FAIL; 0/2/0; 1/0 | FAIL; 0/2/0; 1/0 | FAIL; 0/2/0; 1/0 |
| spec-v3-signals | FAIL; 0/2/0; 1/0 | FAIL; 0/2/0; 1/0 | FAIL; 0/2/0; 1/0 |

`verify.py` independently checks the six archive sizes and SHA256 against GitHub
metadata, raw events against result receipts, all 72 plans/commands against the
pinned harness, protocol markers, fixture hashes, focused terminal census, native
OS/architecture/Go 1.27.1 identity and unchanged before/after manifests. It hashes
all **17,895 source blobs and 17,928 harness blobs** against local immutable Git
objects in every cell. All checks PASS. The three Darwin manifests also share
3,614 identical submodule file hashes; these are cross-compared, **not independently
attested against nested Git objects**. GitHub runner infrastructure is not independently
attested. Diagnostic evaluation is recomputed using the pinned harness evaluator;
raw marker contents are also explicitly reported above.

All six original compressed artifacts and `full-job-logs.zip` are retained without
rewriting. `raw-member-hashes.json` records every uncompressed member hash and size;
`SHA256SUMS` covers all retained files except itself. Original focused receipts,
three diagnostic lanes, command-source mapping and fixture manifests are directly
reviewable. Broad raw logs and setup/job logs remain losslessly compressed.
`log-safety.json` records a known-secret-pattern scan with no matches; this does
not establish absence of unknown secret formats. No credentials were collected.

Reproduce the offline validation from this checkout with
`PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/native-run-41431926958/verify.py`.
`audit.py` derives event/source/sibling receipts; sibling status is a live read-only
snapshot and may change on another invocation. Neither script runs runtime tests,
compiles software, dispatches CI or takes performance measurements.

Only this evidence directory was changed and committed; prior archives, dirty
worker files, submodules and preexisting index entries were preserved. Evidence-only
changes cannot affect shipped runtime performance, so no measurement lease is
needed. No source edits, expectations changes, runtime reruns, benchmark, push,
dispatch, PR, Claude review or subagents were used. The handoff names the evidence
head commit. Native TinyGo and matched baseline graphs remain OPEN. The Nov 4
deadline does not waive these gates; no repair-time estimate is supported here.
