# Independent bounded activation review

Activation verdict: **YES** for exactly execution source
`21d7be58952b64435110246f65d44e11046c5083` with harness/workflow execution
SHA `6d52c087f3136f0cb4d5457c733e1a92449df08b`.

This verdict authorizes neither dispatch nor native qualification. It means the
reviewed harness is suitable for an explicitly authorized native run at those
exact pins. The new source's native closure remains pending.

## Scope and identity

I independently reviewed the executable harness changes and receipts in
`docs/rfwgc-evidence/final-darwin/revision-21d7be589/`, the source reviews
`darwin-reservation-checkptr-recheck` (`4f77ca9ce`) and
`darwin-two-result-entry-recheck` (`367c02623`), and the source changes through
`21d7be589`. The source commit is an ancestor of the harness commit. The harness
requires full 40-character source, driver, and requested-harness identities;
the requested harness must equal the workflow execution SHA, the checked-out
harness HEAD must equal that SHA, and harness dirt is rejected before native
execution.

All revision and source-review checksum manifests verify. The source interval
from `041f7ed4c376e2a33ce21b22075cd617ddca6597` changes outside documentation
only:

- `src/wago/gc_native_reservation_arm64_test.go`;
- `src/core/compiler/backend/railshot/arm64/callexec_arm64_test.go`; and
- the explanatory `Call2` comment in `src/core/runtime/arm64spike/spike.go`.

The executable production implementation is unchanged. The exact workflow
`.github/workflows/ci.yml` is byte-identical to `041f7ed4c376e2a33ce21b22075cd617ddca6597`
(SHA-256 `f352281f3779615da24aa5ddf99a420d6007d94f2887f80352d48423f99ca0e6`).
Consequently its native ARM pin `08f511fac41a77f4c0f9046f88ebc78ad05a5da6`,
TinyGo pin `7e41fafbbcd737ed1edce11b94b43b4eda134dcf`, job names, summary behavior,
and check-name surface are unchanged. No CI check-name impersonation was added.

## Harness review

Each of ordinary, wagodebug, and wago_regalloccheck is a mandatory matrix mode.
Before the unchanged 21 broad commands, each mode runs the retained cancellation,
Core 3 child, and ten repaired-contract discriminators plus these independent
gates:

- reservation: the exact two reservation tests in `./src/wago`, with `-race`,
  `-count=1`, and `-timeout=30s`;
- two-result: exactly `TestTwoIntegerResultRegisterCall` in the ARM64 backend,
  with `wago_codegenstats`, the applicable mode tag, `WAGO_SHARED_SCALAR=0`,
  `-count=1`, and `-timeout=30s`.

There is no `-gcflags` checkptr suppression, race suppression, fail-fast, or
`continue-on-error` waiver. The isolated reservation cross-build receipt omits
`-race` only because it is a foreign CGO-disabled compile check; native Darwin
execution still requires the race gate. Exact top-level terminal census rejects
missing, duplicate, unexpected, failed, or skipped tests. A process failure or
package-census failure remains a command failure even when expected test events
exist.

The driver records effective argv/environment, raw JSON, package census, terminal
events, source and harness manifests, and per-command results. Independent command
failure does not stop the loop. The six synthetic continuation controls cover
reservation and two-result failure in all three modes; every case runs all 26
commands for that mode, retains every raw log, and returns aggregate exit 1.
These controls exercise the actual driver loop with mocked subprocesses and are
not native evidence.

The retained command inventory is internally consistent: 63 broad commands, 15
discriminators, three source focus commands, three matched-baseline graph commands,
and three Windows focus commands, for 87 test commands and paired censuses. Focus
retains 73 top-level tests including all 22 RR tests. Windows retains six exact
contracts per mode. Core 3 child and broad/spec totals remain exact. The matched
baseline graph is unchanged and has only the three false leaves; no nursery
counterpart was invented.

Fifteen unique Go 1.27.1 cross-build/census targets are retained instead of
rebuilding them in the continuation controls: all-three source Darwin focus,
reservation and two-result targets; all-three source Windows focus targets;
and all-three matched-baseline Darwin focus targets. Every recorded target exits
zero, and source/baseline manifests match immutable Git blobs before and after.
Foreign binaries were not executed.

The local receipts pass all-three policy and planner checks, actionlint, shell and
Python syntax, native-host refusal, exact/mismatched identity checks, exact-census
negative cases, and aggregate-summary truth-table checks. The ceiling remains
3×360 Darwin + 3×30 Windows + 3 summary = **1,173 job-minutes**. This is a
configured ceiling, not a measured duration, cost, or estimate.

## Cancellation and artifacts

External cancellation stops the driver without converting partial work to
success. Files already written remain in the runner output directory, including
progress and completed command receipts. The workflow's upload step is guarded by
`!cancelled()`, however, so a cancelled job does **not** guarantee a downloadable
artifact; the historical ordinary cancellation/no-artifact result remains an
accurate warning. Completed failures are uploadable because the upload step uses
`always()` subject to that cancellation guard.

## Qualification limits

The historical native run `41466555967` transfers no result to these pins:
ordinary was cancelled with no artifact, debug and checked failed the reservation
race/checkptr path, and checked failed the established two-result diagnostic.
The source reviews support rerun readiness but do not prove the precise historical
fault causes.

After activation, closure still requires an explicitly authorized run and review
of complete native Darwin receipts in all three modes (ordinary included), the
reservation race gate, two-result established diagnostic, all broad commands,
all Windows receipts, exact identities/manifests/censuses, and any skips or
failures. A discriminator pass alone cannot qualify the source. Native ARM and
TinyGo remain separate, unchanged contracts and provide no equivalence evidence
for this source.

No source, production harness, workflow, CI, dispatch, push, PR, benchmark,
timing, or measurement action was performed by this review.
