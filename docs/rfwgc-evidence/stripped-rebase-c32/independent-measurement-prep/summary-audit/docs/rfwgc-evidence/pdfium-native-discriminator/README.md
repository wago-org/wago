# Targeted native PDFium strict-checked discriminator

Owner authorized launch 2026-11-02 after run41501609461. Full CI is not green.

Baseline9713878ab452848f7eb7549a90be94b938a898be and candidate889b46c45fc54e2fb4b4617eb0c5cc5f54de5db7 run sequentially on one macos-15 ARM64 runner, Go1.27.1, -race -tags=wago_regalloccheck. One native job, 45-minute ceiling including builds/setup. This is correctness diagnosis, not benchmark sampling or final CI approval.

A Go overlay inserts four phase markers per worker configuration (compile-begin/end and execute-begin/end, workers1/4). Markers append to an external file without timestamps; child capture and raw test output are retained. Original test byte equality baseline/candidate is asserted. Removing inserted calls/helper exactly recovers the original test. Every fixture/oracle and the direct RunIsolated(t,60*time.Second) call are unchanged. Parent test binary has a separate 2-minute watchdog; subprocess outer watchdog150s does not extend the child's60s. Sequential cells continue after a normal test failure; source/hash/identity violations fail closed. No phase marker is substituted for successful test/oracle completion.

The pass validator requires exit0, exact parent TestPDFiumCorpus pass, no failure/skip/race marker, and all8 ordered phase markers. Failure and partial logs remain. Candidate and baseline source pins, fixture/test/overlay/binary hashes, commands, tool version, phases and outcomes are archived. Sources stay clean. Native build uses race; local cross-builds are nonrace compile-only, explicitly not execution proof.

Deployment uses the existing registered ci.yml workflow on a dedicated diagnostic branch with the exact ci.yml payload in this directory. It has no push/PR triggers and no check named CI. The campaign branch's normal workflow is not modified. Baseline/candidate checkouts are hard-pinned; workflow/harness identity is checked against supplied SHA. No shell-interpolated inputs. All actions pinned. Artifacts upload even on failure.

Validation: selftest includes four negative acceptance cases; exact reverse-overlay proof; all-three Darwin ARM64 cross-compiles; explicit actionlint1.7.7. Initial mise actionlint shim error is recorded in local-review.json and gives no validation credit.

Disposition: candidate-only reproducible failure requires phase-specific regression investigation. Both fail implies a concrete instrumentation-budget question, not automatic deadline relaxation. Both pass leaves the original full-race failure intact and requires suite-context reconciliation. No native result or performance conclusion is preclaimed.
