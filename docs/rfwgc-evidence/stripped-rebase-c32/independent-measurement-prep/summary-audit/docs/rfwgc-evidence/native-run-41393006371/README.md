# Completed native run 41393006371 — failed Darwin fixture configuration

Run: https://github.com/wago-org/wago/actions/runs/41393006371 (attempt 1).
Harness: `6bd79ae0403ed75e7f20489d93d731ce9a42baeb`.
Execution source: `f2bb27287220783e86a4ead41fee78693a5129de`.
This report preserves the failed attempt; it does not qualify a repaired harness.

## Immediate diagnosis and proposed repair

Every Darwin graph leaf fails at `qualification_supplement_test.go:49` with
`wago: unknown arm64 optimization "gc-lifetime-analyze"` in all three modes.
The fixture's `rfw3Config` (line 44) copied an AMD64-specific optimization override
from the historical measurement helper. The pinned optimization catalog declares
it `experimentalAMD64` (`src/core/compiler/optimization/catalog.go:526`), and
`src/wago/config.go:886-890` correctly rejects it for ARM64. Failure occurs during
Compile, before Instantiate, native graph invocation, collection, or heap inspection.
All six leaves ran and failed; no graph leaf passed. Generating matching Wasm bytes
before Compile does not establish graph execution.

This is a verified harness assumption error, not a verified production defect.
Parent should remove `.WithOptimization("gc-lifetime-analyze", true)` from the
Darwin-only `revision-f2bb27287/_fixtures/graph_test.go`, then update that fixture's
SHA256 in `revision-f2bb27287/contracts.json` and the applicable preparation receipts.
Retain CoreFeaturesV3, disabled inline, the same Wasm fixtures, all six exact leaves,
64 calls, collection, 32 live graph objects, at least 2,048 allocations, and zero
noGC allocations. Do not bypass architecture validation or weaken the census.
This repair addresses the observed first failure only; later execution is unproven.
Parent owns repair/review/new activation. This worker changed only this evidence
directory, with no production or workflow edits and no remote rerun.

The four Darwin storage/conversion-rejection tests all passed, including actual
safe conversion followed by rejected guest/public publication. No failed ownership
checkpoint was followed by this investigation: diagnosis used retained logs and
read-only pinned source inspection only. No verified production finding arose,
so the conditional sibling production-defect cross-check is not triggered.

## Exact native census

Counts below are identical in ordinary, wagodebug, and wago_regalloccheck.

| Platform | Top-level pass | Top-level fail | Failed leaves | Skip events | Package |
| --- | ---: | ---: | ---: | ---: | --- |
| Windows AMD64 | 4 | 0 | 0 | 0 | PASS |
| Darwin ARM64 | 4 | 1 | 6 | 0 | FAIL |

Windows passes exactly once each per mode:

- `TestDesignRepairPublicTableOrdinary`
- `TestDesignRepairUnsupportedBulkAtomicity`
- `TestDesignRepairUnsupportedConvertedPublicToken`
- `TestQualificationWindowsGCAdmission`

These are 12 actual top-level passes across three modes, qualifying ordinary
storage, atomic rejection/authentication behavior, and Core 3 admission rejection.
They are **not Windows Core 3 GC JIT execution**. Converted-publication execution
is deliberately not selected on Windows, whose backend does not admit Core 3.

Darwin passes the first three common tests above plus
`TestDesignRepairUnsupportedConvertedPublication` once each per mode (12 passes).
`TestRFWStage3MeasurementGuest` fails once per mode. Its exact six failed leaves
are `noGC/false`, `noGC/true`, `scalar/false`, `scalar/true`, `graph/false`, and
`graph/true` (18 failed leaves across modes). Package fail events are additional
aggregate events, not extra tests. Contract exit codes are `[0, 1]` on Darwin
and `[0, 0]` on Windows; package discovery succeeded in every cell.

Darwin setup succeeded in all three jobs. `Execute correctness and retain census`
was skipped in all three: none of the broad 63-command route ran in this attempt.
The special summary correctly failed. API job census: three successful Windows
jobs, three failed Darwin jobs plus one failed special summary, and 20 skipped
jobs (including the disabled ordinary CI aggregate). CI review approved activation;
it did not establish platform correctness or full CI qualification.

## Evidence and independent verification

- `artifacts/`: all six original compressed artifact ZIPs. Each byte count and
  SHA256 matches the final GitHub API metadata in `artifacts.json`.
- `full-job-logs.zip`: complete GitHub run log download, 89 entries including
  seven aggregate logs covering all executed jobs, setup, steps and cleanup.
  Skipped jobs have no executed log body. `jobs.json` retains their states.
- `receipts/`: small original host, command, summary and full Go JSON event files
  extracted byte-for-byte for direct review. Large manifests/package censuses and
  Darwin setup receipts remain in the ZIPs.
- `run.json`, `jobs.json`, `artifacts.json`: completed API metadata.
- `verify.py`, `verification.json`: independent offline verifier and result.
  Run from this checkout with `python3 docs/rfwgc-evidence/native-run-41393006371/verify.py`.
  It needs the pinned local Git objects, for the main repository;
  it does not execute test binaries, build, dispatch CI, or take measurements.
- `SHA256SUMS`: hashes of all retained evidence except itself.

Verification checks all six archive identities/digests, exact source/harness pins,
native target/machine and Go 1.27.1 receipts, declared source/fixture hashes against
Git objects, complete main-repository manifest contents, agreement of Darwin submodule manifests across modes, mode flags/environment, unique terminal events, exact pass/fail/skip
census, the six identical initial diagnostics, and all generated Wasm hashes.
There are 16,184 source and 16,194 harness main-repository blobs per cell; Darwin
also includes 3,614 submodule file hashes, identical across modes. These submodule
hashes are retained and cross-compared, but not independently rehashed against Git
objects: the local checkout lacks the pinned nested KaTeX checkout. An attempted
recursive verifier stopped on that missing path; it ran no tests and changed no
submodules. This limitation does not affect the focused contract package or fixtures,
whose complete main-repository contents are independently verified. Windows source/harness and
Darwin harness do not initialize submodules. Receipts report source/harness unchanged
at completion. Native Go binary hashes are recorded consistently per platform;
this report does not independently attest runner binaries or runner infrastructure.

GitHub already masks credentials in job logs. A further local scan of retained
logs and textual artifact members found no token/private-key/unmasked Basic-auth
or credential-bearing URL patterns (`log-safety.json`). No local auth configuration,
environment secret dump or token was collected. The original parent failed-log
file remains outside this evidence; the complete run archive supersedes it.

## Remaining gates and ownership

Darwin generated graph execution remains failed/unqualified. The matched baseline
graph supplement and native patched TinyGo execution remain NOT_RUN. Unsupported
Darwin H6 ownership behavior is not newly qualified. No performance conclusion,
full CI success, platform inclusion approval or repaired-harness result is claimed.

Ownership: `docs/rfwgc-evidence/native-run-41393006371/` only. Unrelated worktree
changes and index entries must be preserved. No push, PR, production edit, workflow
edit, remote execution, benchmark, timing, subagent or Claude review was performed.
Evidence-only additions cannot affect shipped runtime performance; no measurement
lease or READY TO MEASURE declaration applies to this commit.
