# Native ARM64 RR3 diagnostics activation recheck

Activation: **YES**, narrowly for publishing and dispatching the exact reviewed
harness below against the unchanged source below. This is not a native pass,
full-CI qualification, performance result, or approval of later production work.

- Reviewed harness: `4af18b3a2484b901e11f7347adab8e547176dbc6`.
- Source to execute: `5847df30f1af15d10b743d90a24b1429b290d358`.
- Previously reviewed harness: `6c71c4d6747456578f55928e900e36bb2d1449a8`.
- Failed-run audit: `832e5cbe56fac8cf97517ff25fdcb04bf0cc19b8`.
- Worktree HEAD observed during review: `8e6f8007f90fbbb88c7834419f87361ff6dc14e2`;
  this moving HEAD is NOT the reviewed harness pin.

## Findings and boundary

No activation blocker found in the narrow parent repair. Its commit changes only
native-arm64-rr3/run.py, test_harness.py and README.md. Relative to the original
reviewed harness, the committed CI workflow is byte-identical. The plan (including
source, 85/1/11/9 census and original probe hashes), timer source, deadlines,
identity checks, strict zero-skip census and artifact preservation are unchanged.
The repair adds `wago_codegenstats` only for the backend binary, retaining the
selected mode tag. Ordinary wago, wago-race and runtime commands remain ordinary.

The source-pinned TestRegisterCallOverlappingArgumentSlotARM64 calls
requireCompilerDiagnostics, whose skip is conditional on diagnosticsEnabled.
The source-pinned diagnostics_enabled.go selects true under wago_codegenstats;
the disabled file excludes that tag. Actual Go file selection was checked in
all three modes. This removes the diagnosed prerequisite skip; it does not prove
the direct/indirect executable assertions pass.

Committed workflow controls still require the full lowercase 40-character harness
SHA to equal the dispatch workflow SHA, the exact source SHA, and the other
qualification toggle false. Separate source and harness checkouts are validated
by HEAD identity and before/after blob manifests. The native runner is
ubuntu-24.04-arm; host/tool ELF and override checks remain intact. Normal CI's
planner and CI status are excluded on native dispatch, Darwin/Windows lanes are
excluded, and the native summary requires native success with every other needed
job skipped. Ordinary CI conditions are unchanged. The source input default is
not this source: dispatch must explicitly supply the exact source above.

Production files differ between the original and new harness commits. They are
excluded from this activation: the workflow executes the separate hard-pinned
5847 source checkout. No current-worktree production files, dirty CI edits, or
Darwin worker changes were used as review inputs or included in this report.
Any different harness/workflow SHA needs review; do not silently substitute HEAD.

## Bounded validation

`validate.py` reads committed blobs, exports only required source directories from
the exact source commit into a temporary directory under this report, and removes
that directory on completion. `validation.log` records:

- Six committed Python harness contracts PASS, including all three mode tag
  selections, exact identity refusals, native refusals and strict census checks.
- Independent expected-tag comparison for backend/wago/wago-race/runtime in all
  three modes.
- Backend Go file selection includes diagnostics_enabled.go and excludes
  diagnostics_disabled.go in all modes.
- Go 1.27.1 linux/amd64 cross-builds to linux/arm64 PASS for:
  `-tags=wago_codegenstats`, `-tags=wagodebug,wago_codegenstats`, and
  `-tags=wago_regalloccheck,wago_codegenstats`.
  Each output is ELF machine 183; hashes and exact commands are in the log.

These cross-builds use CGO_ENABLED=0 and do not replace the native harness's
CGO_ENABLED=1 toolchain or race execution checks. No ARM64 binary was executed
locally. The initial source export omitted codegen/internal dependencies and
failed setup; its unmodified receipt is `incomplete-export.log`. Adding those
committed directories to the local export fixed the review fixture, after which
all three cross-builds passed. No product or harness changes were made.

The committed native-run-41442912023 evidence directory remains unchanged against
its audit commit. That run remains aggregate FAIL: eight backend passes and one
skip per mode, despite the recorded focused/race/guard/runtime/timer successes.
The README correction preserves that failure. Failed raw artifacts were neither
rewritten nor removed.

## Remaining execution and handoff

Native execution with the new harness is **NOT_RUN**. The parent must publish the
exact reviewed harness and rerun against the exact source, retaining raw artifacts
and checking all nine backend tests without skips in each mode, plus the unchanged
other lanes and aggregate. This report does not dispatch, push, or create a PR.

This is a review-only evidence change: it cannot affect production performance.
No benchmark or performance timing was taken. There is no new verified product
finding to triage or cross-check against sibling projects; the already-audited
harness omission is the sole review subject.
