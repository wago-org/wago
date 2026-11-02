# Independent native ARM64 RR3 activation review

Activation: **YES** for harness `6c71c4d6747456578f55928e900e36bb2d1449a8`
and execution source `5847df30f1af15d10b743d90a24b1429b290d358` only.
Specific activation blockers: **none identified**.
Platform coverage: **NO / NOT_RUN** until all three native artifacts and job
logs exist and pass receipt review. This is permission-readiness review, not a
publication, dispatch, full-CI, performance, or runtime-correctness result.

Reviewed immutable Git archives, not concurrent workers' runtime/backend files.
Read the supplied local rules and `/home/dev/Projects/wago/AGENTS.md` (the assigned
worktree has no AGENTS.md). No production/workflow edits, push, dispatch, PR,
benchmark, Claude review, or subagents. No runtime finding requiring sibling
triage was identified. Documentation and validation artifacts cannot affect
shipped performance. Later Darwin port `3f6161883` and ARM branch repatching are
explicitly outside this review.

## Findings and boundaries

- The workflow selects `ubuntu-24.04-arm`, with three modes and fail-fast false.
  GitHub identifies this label as Linux ARM64 in its [runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners),
  and describes these runners as native in its [announcement](https://github.blog/changelog/2025-08-07-arm64-hosted-runners-for-public-repositories-are-now-generally-available/).
  Host metadata, Python ELF, native Go host/target and executable ELF checks
  reinforce that provider identity. No emulator or execution adapter is used.
  This relies on the provider's runner identity, not hypervisor attestation.
- Source is an exact fixed full SHA. Independently supplied harness SHA must be
  a lowercase full SHA and equal workflow SHA. Separate checkouts are checked
  clean, with every tracked blob compared against Git and before/after SHA256
  manifests. Original probes and bootstrap are hash checked; source compilation
  occurs directly in the pinned checkout with no portability overlay.
- Census reconciliation is explicit: the prior **100** focused AMD64 tests become
  **85** applicable ARM64 tests. All 15 excluded tests are AMD64-build-constrained;
  no applicable name is omitted or added, in any mode. See `census-audit.json`
  for every excluded name, defining file and build constraint. Thus this lane
  must never be described as 100 native ARM64 focused passes. RR1/RR2/RR3 and
  all five retirement families remain unchanged and included. The README
  already discloses the architecture-specific exclusion.
- Each mode requires focused 85, original shared cancellation/GC guard 1,
  focused-race 85, original guard-race 1, runtime 11 and backend 9. Exact compiled
  listings and exact run/pass census reject omissions, duplicates, package
  mismatch, skips, failures and malformed output; command exit zero is required
  independently. Subtest starts/passes must balance without duplicates.
- Original shared guard admission checks and three-second entry/GC/cancel
  deadlines remain unchanged. The unarmed timer control is byte-identical to
  the prior diagnosis and checks all six notify/signal pairs, including signal
  64. It does not override the runtime signal or prove delivery. Inherited
  execution/environment overrides are rejected. No deadline weakening.
- Pending command exits remain null; failed/partial receipts are uploaded with
  always(), attempt identity and 14-day retention. Final integrity failure
  prevents qualification. Hard cancellation/runner loss can prevent final
  checks or upload; that remains unqualified, never a pass.
- Both manual flags default false. Both true is refused before checkout; normal
  planning and Darwin/Windows qualification are excluded from native dispatch.
  On either special dispatch the normal aggregate is disabled AND renamed
  `Full CI not run (qualification dispatch)`: successful special summaries or
  skipped aggregates cannot present required `CI`. Normal event behavior stays
  intact. ci-ok covers every job; special summaries remain upstream; no cycles.
- The native summary accepts only native matrix success with all other needs
  skipped. Actual extracted shell/Python admission and summary commands were
  exercised locally, not merely an equivalent hand-written predicate.
- Configured attempt ceiling: 3 × 45 native job minutes + 3 summary minutes =
  **138 job-minutes**, excluding queueing. Step/watchdog limits do not enlarge
  the job ceilings. No completion-time or performance measurement was made.

## Independent local checks

Explicit Go `/home/dev/.local/share/mise/installs/go/1.27.1/bin/go`, matching
GOROOT, GOTOOLCHAIN=local and GOENV=off were used. Commands and exits are retained
in `commands.json`; stdout/stderr are in corresponding logs.

- Policy and planner: PASS in ordinary, wagodebug, wago_regalloccheck.
- Harness refusal/census contracts: all five tests PASS.
- Actionlint v1.7.7: first attempt FAIL because a Git archive has no repository
  metadata for automatic `.github/actionlint.yaml` discovery; it reported
  existing custom runner labels. That log and exit are retained. The initial
  validation script consequently exited 1 at its final aggregate assertion.
  Rerun with the archived config explicitly supplied: PASS, no workflow edits.
- Nine independent compile-only builds: three packages × three modes, all PASS,
  all ELF machine 183; execution NOT_RUN. `cross-builds.json` records all nine.
  This independently corroborates the implementation's nine-build receipts.
- Every one of 18,364 source archive blobs still matches its pinned Git object
  after compilation. Six probe hashes, bootstrap and timer hashes PASS.
- Three Go package-list audits reconcile the original focused selection to the
  planned 85 names with no missing/extra tests. Timer byte comparison PASS.
- Five actual admission cases and 70 actual summary result cases PASS, including
  conflicting flags, wrong source, short harness, workflow mismatch, and every
  individual dependency's nonaccepted result. These are local policy tests,
  not simulated native qualification.

No native race build/execution, original guard result, runtime/backend result,
or timer delivery result is claimed. Existing QEMU failures remain evidence;
this review neither dismisses them nor substitutes cross-builds for native data.
