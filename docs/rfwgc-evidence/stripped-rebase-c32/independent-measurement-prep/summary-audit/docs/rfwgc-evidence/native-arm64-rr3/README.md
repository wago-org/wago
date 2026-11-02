# Native Linux ARM64 RR3 qualification — awaiting parent review

Source **5847df30f1af15d10b743d90a24b1429b290d358**. Prior evidence commit
**0d460778c**, `docs/rr3-retirement-repair/summary.json` and
`TIMER-DIAGNOSIS.md`: QEMU ARM64 failed all three modes when timer_create used
signal 64 (EINVAL); signal 35 and native AMD64 controls worked. Those failures
remain evidence. Native Linux ARM64 at this pin is **NOT_RUN**.

This harness is proposed for parent review; it has not been pushed or dispatched.
The parent owns the active Claude RR3 review and acceptance. This is a focused
platform discriminator, not full CI, a performance result, or a closure claim.
No production code, original probes, timer selection, deadlines, or test oracles
are changed. CI/harness-only changes cannot affect shipped runtime performance.

## Runner and immutable inputs

The lane uses `ubuntu-24.04-arm`, already used in normal CI. GitHub's
[standard runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)
lists its Linux ARM64 architecture; its
[native ARM64 announcement](https://github.blog/changelog/2025-08-07-arm64-hosted-runners-for-public-repositories-are-now-generally-available/)
confirms native execution. No new secret is needed. The runner label is the
provider evidence of native hardware; uname, runner metadata, Python/Go/WABT/test
ELF machine 183, and native Go host/target configuration independently check the
execution environment. This does not attempt to detect a dishonest hypervisor.
No QEMU execution, cross-target override, or test adapter is permitted.

Source and reviewed harness use separate clean full-SHA checkouts. The explicit
`harness_sha` must match the workflow execution SHA, so a moving branch cannot
silently substitute an unreviewed driver. The source input must equal the fixed
source pin before checkout. Every tracked blob is compared with its Git blob ID,
and SHA256 manifests are retained before/after, including unchanged original
RR1/RR2, RR3, lifecycle, flags adapter and shared-entry guard hashes in `plan.json`.
Unused conformance submodules remain uninitialized; no full Core 3 interpreter
or corpus is fetched. No fixture portability overlay is necessary.

`setup-go` provisions exact Go **1.27.1**. The driver resolves its real executable,
sets matching GOROOT, disables automatic toolchain selection and persisted Go
configuration, and requires native linux/arm64 with CGO enabled for race support.
Inherited GOOS/GOARCH/GOFLAGS and execution overrides are rejected, as are mise
shims. The existing source-pinned WABT bootstrap installs **1.0.41** into the
separate harness checkout's ignored tool directory. ARM64 archive SHA256:
`5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf`.
Bootstrap, archive provenance and actual executable hashes are retained.

## Focused gates and evidence

Each of ordinary, wagodebug and wago_regalloccheck runs these independent gates:

| Gate | Exact top-level census per mode |
| --- | ---: |
| Original RR/design/H2/conversion/admission and retirement lifecycle | 85 |
| Original TestSharedEntryGuardCancellationAndGC | 1 |
| Same focused suite under race detector | 85 |
| Same original shared-entry guard under race detector | 1 |
| Native runtime interrupt/token/signal/deadline tests | 11 |
| ARM64 backend register/host-result/shared-layout tests | 9 |

`plan.json` enumerates every expected name. The focused selection includes all
original RR1/RR2/RR3 adversaries, all five retirement lifecycle/borrowed-root
families and available native ARM64 H2 controls. AMD64-only H6/start/frame-root
families are not represented as native ARM64 executions. The guard retains its
original three-second cancellation deadline and admission/GC assertions.

Each binary's native ELF identity and SHA256 are recorded. Its compiled test
listing must exactly match the plan, and verbose test2json events must include
exactly one start and pass for every expected top-level test, matching starts and
passes for every subtest, one package pass, zero failures/skips and exit zero.
Missing/duplicate tests, malformed output, unexpected skips, crash, unsupported
race build, or timeout fail the gate. Logs retain every event and all subtest
passes appear in the summary. Commands run independently after setup, so a
failed RR3 or guard does not suppress the other original tests or diagnostics.

The archived unarmed timer control is byte-identical to the prior diagnosis and
runs separately. All six CLOCK_MONOTONIC / notify 1,0,4 / signal 35,64 combinations
must return errno zero. This control does not prove timer delivery and never
substitutes for the original cancellation/deadline probes. Host monotonic clock
information is recorded without timing a workload. No benchmark is run.

Every driver command records argv, cwd, timeout, exit and a raw log; final source
and harness integrity checks also run on ordinary failures. Pending commands
have null exit if externally killed and cannot count as passes. Setup/action
failures remain in GitHub job logs and identity receipts. `always()` artifact
upload retains failed/partial evidence for 14 days, with run-attempt labels;
parent should also retain the full job logs. Hard runner loss/cancellation can
prevent upload and is an unqualified result, never a pass.

## CI isolation and bounds

`native_arm64_rr3` defaults false. Native dispatch skips the normal planner and
all normal platform jobs, Darwin and Windows qualification. Selecting both
manual flags is rejected. The special summary requires all three native matrix
cells successful and all its other dependencies skipped. No dependency cycle is
introduced. Normal `ci-ok` still depends on every job. On either special dispatch
it is disabled and named **Full CI not run (qualification dispatch)**, so even a
skipped check cannot impersonate required **CI**. Ordinary CI retains its
existing jobs and profile policy.

Each native job has a **45-minute** ceiling; its execution step has a 35-minute
ceiling with bounded setup and artifact steps. Test processes use a three-minute
test timeout and four-minute external watchdog; build commands have five-minute
watchdogs. The three cells have a combined **135 job-minute** ceiling, plus a
three-minute Linux summary: **138 job-minutes per attempt**, excluding queueing.
These are configured limits, not measurements or completion forecasts.

## Local validation and activation

`local-validation/` retains all attempts, command exits, cross-build receipts,
probe hash checks, syntax/actionlint, refusal/census tests, and policy/planner
checks in all three modes using explicit Go 1.27.1 and matching GOROOT. Cross
builds are compile-only, never native execution evidence. Other threads' mutable
production changes in the shared checkout are outside this qualification.

After parent review and separately authorized publication, use the full harness
commit named in the implementation handoff. Verify the remote branch equals it;
the workflow also enforces this equality. Proposed activation (not executed):

```sh
HARNESS_SHA=<full-reviewed-harness-commit-from-handoff>
gh workflow run ci.yml --repo wago-org/wago --ref experiment/rfwgc-port \
  -f native_arm64_rr3=true -f darwin_qualification=false \
  -f source_sha=5847df30f1af15d10b743d90a24b1429b290d358 \
  -f harness_sha="$HARNESS_SHA"
```

Retain all three `native-arm64-rr3-5847df30f-<mode>-<attempt>` artifacts and full
job logs, including failures. A changed harness needs fresh review and a new pin.
Native RR3, original guard, race, runtime/backend and timer-control results remain
**NOT_RUN** until those native artifacts exist and pass review. The November 4
target remains at risk pending dispatch and actual native results.

## Backend diagnostics correction after run 41442912023

The first native run passed the timer controls, focused/race, guard and runtime
lanes in all modes, but failed the backend census because one required test
skipped without `wago_codegenstats`. The backend binary now adds that tag in
each mode. Other binaries, source pin, deadlines, probes and census are unchanged.
The failed run is preserved; a reviewed native rerun is still required.
