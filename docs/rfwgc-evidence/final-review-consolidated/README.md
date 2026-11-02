# Final review consolidation — interim checkpoint

Reviewed production pin: `5345bd365c13963f1bf60e26bb80789ab9ead4cc`.
Campaign base: `9713878ab452848f7eb7549a90be94b938a898be`.
Combined correctness evidence: `2c9f72744dd6f414fc8f236c2d45aa39ce6ced69`.

This is an interim consolidation, not final review approval, inclusion closure,
performance qualification or PR readiness. All four initial reviews have completed. The gate is BLOCKED on the Claude
findings and fresh repair qualification/re-review.

| Review | Evidence commit | Verdict |
| --- | --- | --- |
| Daybreak collector | `39ae86d8ad15fde4b890df338cfd1e3683b5b0b5` | One low documentation defect C1; no verified behavioral defect |
| Daybreak compiler/lifetime | `cf14188a1932f9c65d6abdf67ded69c79fa8baa2` | No verified production defect |
| Daybreak runtime/rooting | `e39f457f077f093b44c1c7be5ed77deb72b87342` | No verified production defect |
| Claude design/judgment | `f8b0e1827de2eb6d09c98ae10e4754f8fa9cca57` | DR-1/DR-2 executed regressions; DR-3 source-verified platform gap |

## Finding triage

**C1 — resolved and independently re-reviewed.** The region configuration comment
incorrectly said native allocation batches were disabled. Commit
`942fe6c7d731ffacfac61c4aaa51bf13d5063cd3` corrects it to describe bounded small
struct/array batches, exact owned-tail cancellation and the disabled baseline
direct-bump/native large-array paths. Fresh Daybreak re-review
`ad5813fbe0f7a6103c1c098e085e3db5edf7dade` confirms no remaining objection and no
executable change. This repair cannot affect runtime performance. No duplicate
verified findings or reviewer disagreements have been reported so far.

## Consolidated boundaries and outstanding gates

- Darwin execution is NOT_RUN and blocks final qualification. A runner-discovery
  assignment is active; no completion time is established. This is a risk to the
  November 4 landing target, not an approved omission.
- Independent runtime/compile measurement is pending. Protocol preparation is
  active; no lease or measurement is authorized by this checkpoint. Measurement
  must wait for test processes to drain and acquire the global lease.
- AMD64 lifetime replay is an optimization boundary; other targets use ordinary
  lowering. RV64 admission is explicitly bounded. Linux standard-Go opaque host
  receipts must not be claimed as supported on excluded RV64/TinyGo/non-Linux
  paths. Compatibility controls do not establish receipt implementation there.
- TinyGo serial foreign-stack registration relies on the reviewed invariant that
  native code cannot allocate or call Go on that foreign stack. The threaded
  patched-0.42 execution evidence does not broaden that serial contract.
- Reentrant Close returns promptly by contract; WaitClosed carries the eventual
  owning result. The runtime reviewer found no contradiction in the inspected
  source or terminal-result tests.
- The 195 executable combined gates qualify the recorded source pin. This
  interim report does not claim a fresh full-suite run after the comment repair.
  Final-head qualification and all subsequent repair re-reviews remain required.

The three completed reviewers ran focused immutable-archive checks in ordinary,
wagodebug and wago_regalloccheck. Their reports retain scope and harness failures;
those checks do not replace the full combined receipts. Seven-PR and R9 final
inclusion buckets remain pending all final gates. No PR has been opened or closed
by this checkpoint.

## Claude findings and subsequent CI review — blocking update

- **DR-1 OPEN:** externref storage alone sets dynamic invocation domains, locking
  unrelated GC domains for the guest call. Executed baseline controls pass and
  candidate fails in all three modes. Repair must narrow admission without
  losing conversion publication authority or creating lock-upgrade deadlocks.
- **DR-2 OPEN:** the same classification rejects previously valid cross-Runtime
  scalar-function imports. Same root cause as DR-1, distinct required regression.
- **DR-3 OPEN:** non-Linux opaque conversion publication can retain pre-fix H2
  behavior without rejection. Source-verified, not executed on Darwin/Windows.
  Requires fail-closed admission or actual portable ownership support and an
  explicit platform disposition; excluded tests do not establish absence.
- **CI-1 CLOSED after independent re-review:** Darwin-only dispatch reused required status
  name CI. Commit `7e887cb25623f711cd92d7ede88b83846f5fe95f` separates the Darwin
  summary and renames the skipped normal summary to prevent status impersonation.
  Local workflow contracts/actionlint and planner checks pass in all three modes.
  Independent re-review `446e9254dda5838241ddf0cf783d3c8ccd3fb37a` confirms
  that neither successful nor skipped special-event checks are named CI, while
  normal CI contracts remain unchanged. Darwin dispatch still waits for the
  runtime repairs and refreshed source/harness identity.

The earlier no-defect slice reviews do not contradict the concrete Claude probes:
those reviews did not establish absence of the newly exercised regressions. Parent
accepts DR-1/2/3 for repair. Claude Q2 duplicates resolved C1. Q1/Q4 require added
large-old-heap and non-GC externref measurement controls; Q3 requires honest serial
trace fallback accounting. Q5 public API target behavior, Q6 threaded wait liveness,
and Q7 shared-domain poison behavior remain explicit assessment obligations, not
silently closed defects. Review repair work invalidates the old candidate as a
final measurement pin. No acquisition or CI dispatch is authorized yet.

Deadline risk: runtime repair, platform validation, fresh review and combined-head
qualification add an unestimated critical-path cost. The November 4 target has no
waiver. Documentation and CI changes cannot affect shipped runtime performance.
