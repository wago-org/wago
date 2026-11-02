# Native run 41501609461: PDFium deadline disposition

Source `889b46c45fc54e2fb4b4617eb0c5cc5f54de5db7`; harness `71fb2a16e716e62bd2fe6aa364c0557782268b4d`.

## Verdict per mode

- Darwin ordinary: all 26 command result receipts pass, including root, race, reservation and two-result regressions; existing skips retained.
- Darwin wagodebug: all 26 command result receipts pass; existing skips retained.
- Darwin wago_regalloccheck: 25 command receipts pass; `race-wago_regalloccheck` fails exclusively on `TestPDFiumCorpus` subprocess deadline. 10,818 passing test events, ZERO `WARNING: DATA RACE` reports. The package failure is the aggregate of that test failure, not another defect. Package elapsed 684.388s; test elapsed 60.01s.

This is NOT a verified data race. It is a required correctness-test timeout under combined race and register-allocation checking. No native acceptance waiver. All six original artifact ZIPs and full job logs are retained; ZIP SHA-256 values match GitHub API digests. All Darwin artifact source/harness identities match. Source-member manifests are retained in original ZIPs, not independently fully replayed in this margin audit. Windows original receipts remain retained; no new Windows GC claim.

## Existing elapsed-time evidence (not new measurement)

| TestPDFiumCorpus lane | ordinary | wagodebug | regalloccheck |
|---|---:|---:|---:|
| Race | PASS 24.23s | PASS 24.72s | TIMEOUT 60.01s |
| Full root, no race | PASS 1.15s | PASS 1.09s | PASS 4.64s |
| Default diagnostics | PASS 0.87s | PASS 0.94s | PASS 3.18s |
| Established diagnostics | PASS 0.76s | PASS 0.83s | PASS 4.37s |
| Guard | PASS 0.94s | PASS 0.90s | PASS 3.35s |

Passing race margins are 35.77s (ordinary) and 35.28s (debug), so those two modes are not narrowly grazing the limit. Prior native ordinary run 41490640588 also passed at 24.22s. Checked race did not finish: 60.01s is the kill boundary, not its natural runtime. Multiplying independent-mode ratios would be an unsupported prediction.

Historical Linux/AMD64 5b02 race receipts passed at 11.94/12.22/17.47s. These are different target/source/runner observations, NOT a matched native baseline and NOT proof that the current Darwin failure is transient. They establish no cross-target performance verdict.

## Deadline source and uncertainty

`src/wago/pdfium_corpus_test.go:40` calls `regressiontest.RunIsolated(t, 60*time.Second)` directly. `tests/support/regressiontest/process.go:62-85` starts a context-bounded child of the same test binary and reports the deadline after killing it. The child executes both compiler-worker configurations 1 and 4, compiling and then parsing/rasterizing against the unchanged V8 oracle. It inherits race and regalloccheck instrumentation. ARM64 checked compilation installs additional allocation-state observers/maps in `regalloc_check_enabled.go`.

The test and process helper are byte-identical at campaign baseline 9713878ab and candidate 889b46c45; hashes in `audit/unchanged-timeout-source.json`. This does not establish the rest of the compiler/runtime unchanged. `WAGO_CORPUS_TIMEOUT`, the Go package timeout, and GitHub job timeout do NOT control this direct 60-second call. `regressiontest.Timeout` exists but is not called here.

The captured child output contains no phase progress or stack dump. Therefore compilation vs guest execution vs scheduling delay is UNRESOLVED. Combined instrumentation is a credible explanation, supported by separate checked/nonchecked observations, but not a proven cause. No port performance defect is established or ruled out.

## Disposition before redispatch

Recommend a bounded targeted native correctness discriminator, NOT a blind full-mode retry and NOT an automatic deadline increase. Compare exact baseline 9713878ab and candidate 889b46c45 on the same native runner, ordinary/debug/checked with race, preserving the original 60-second child deadline and every fixture/oracle assertion. Run cells sequentially. Add diagnostic-only phase markers for worker 1/4 compile and execution, or a safely retained timeout stack, to identify where any repeated timeout occurs. These would require a reviewed test overlay; none has been implemented or dispatched by this audit. Do not call a diagnostic overlay an unmodified qualification pass.

- Candidate-only reproducible failure: investigate the first differing compiler/runtime phase; do not raise a limit to hide it.
- Baseline and candidate both fail under the same instrumentation: raise a concrete harness-budget question with that evidence; no automatic waiver.
- Both pass unchanged: preserve the failed broad-suite receipt and investigate suite contention; a targeted pass alone does not turn the original full race command green.

No new benchmark, sampling, measurement lease, source edit, timeout change, or redispatch. Performance measurement remains independently leased if needed. Wednesday risk is now one checked-mode corpus timeout plus unpredictable Mac acquisition capacity; diagnosis/repair cost cannot be reliably predicted without the native discriminator. The two clean native modes should not be discarded or rerun unnecessarily.
