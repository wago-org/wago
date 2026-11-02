# Independent Darwin TinyGo CI activation recheck

**Activation: YES for exact installed harness `8801252952255edf6ab6196250bd544873785328`. Native Darwin ARM64 TinyGo: NOT_RUN.**

Execution source remains exactly `7e41fafbbcd737ed1edce11b94b43b4eda134dcf`. This is an activation and protocol review only, not native qualification or final platform approval. No workflow/source edit, push, dispatch, PR, benchmark, or subagent was performed.

The branch-only registration blocker is repaired: the default-false opt-in is installed in the existing registered `.github/workflows/ci.yml`. The current workflow is byte-identical to the workflow at the exact harness commit (SHA-256 `ec245e6453c125fbc3e2618957108bb6bdc9c87288951a6dcebe713e565e3882`). Dispatch must use that commit as both the ref and `harness_sha`; the workflow rejects any mismatch with `GITHUB_SHA` and rejects a source other than the exact execution pin.

## Workflow findings

- Ordinary CI remains isolated. With the new input false, the established planner and `CI` aggregate conditions retain their normal path and both added jobs skip. On the supplement dispatch, `ci-ok` is skipped and its conditional name is `Full CI not run (qualification dispatch)`, so the required `CI` success name is not emitted.
- `ci-ok` depends on both `darwin-tinygo-native` and `darwin-tinygo-summary`, preserving the all-job aggregate contract. The special summary does not depend on `ci-ok`. Independent traversal of all 27 jobs found no cycle or missing dependency.
- `changes`, both Linux ARM jobs, both ordinary Darwin/Windows jobs, and their summaries exclude the supplement. Each native matrix cell also rejects either conflicting qualification flag before checkout/compiler setup. The summary accepts only native matrix success and every other dependency skipped.
- The exact summary Python expression was executed directly. Its positive control passed. Native `failure` and `cancelled`, unexpected ordinary-job `success`, and unexpected ordinary-job `failure` all returned nonzero.
- Dedicated concurrency appends the run ID and disables cancellation only for the supplement dispatch. Separate supplement attempts cannot cancel one another or existing ordinary/Darwin/ARM runs; other events preserve cancellation behavior.
- The matrix contains exactly the three modes, `max-parallel: 1`, and one native harness invocation per matrix cell. Each matrix job is capped at 110 minutes and the summary at 3 minutes: 330 + 3 = **333 maximum job-minutes**. There is no second cross-build job or duplicate harness invocation.

## Identity and unchanged evidence

The installed job pins Go 1.27.1, the TinyGo 0.42.0 Darwin ARM64 archive digest `493da3585e66c5da677a4be39402faf2041b4e45a61eb456e4547eca753a544e`, and source `7e41fafbbcd737ed1edce11b94b43b4eda134dcf`. The unchanged harness verifies compiler digest `1b74ee7ef9841f01364823be1254833a96832e74076957c956d89872f705b7d2`. `harness.py` remains SHA-256 `583192e4f217ff8ea2fc56eb16a0ed23f60df05ec679ff1a553a62d9890ffe82`.

The integration commit does not change any file under `darwin-tinygo-supplement`, and no later commit through review HEAD changes the installed workflow, CI policy tests, or supplement. The native harness, fixture/probe inputs, tool attestations, and retained cross evidence are therefore unchanged. An independent recount of the suite plan gives **33/36/33** top-level tests for ordinary/wagodebug/wago_regalloccheck; explicit subtests are additional. Existing cross artifacts were not rebuilt or run.

## Bounded checks

All passed:

- `go test ./tests/integration/cipolicy ./tests/tools/ci-plan -count=1`
- the same full packages with `-tags=wagodebug`
- the same full packages with `-tags=wago_regalloccheck`
- actionlint 1.7.7 on `.github/workflows/ci.yml`
- `review.py`, including exact-commit identity, graph, isolation, census, bound, and summary negative controls

Machine-readable identities and controls are in `review.json`; command outputs are in the three mode logs and `actionlint.log`.

## Remaining gaps

Native execution remains **NOT_RUN**. Commit `5b02ec67afe0dce181a7f8d508e270c7ecb0ecfc` remains outside source `7e41...` and cannot be qualified by a future run of this exact pin. Successful activation would still require dispatching the exact harness ref/input, auditing all native receipts, identities, census and failures, and making a separate platform decision. This review grants no final platform approval.
