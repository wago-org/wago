# Fresh CI-1 re-review of `7e887cb25`

Review target: `7e887cb25623f711cd92d7ede88b83846f5fe95f` in the
`experiment-rfwgc-port` worktree. The review compared the target against the
original implementation at `96f0700d298f48c81e07487d63472bfd8e102070` and the
activation review in `docs/rfwgc-evidence/final-darwin-review/README.md`.

## Verdict

**CI-1 is resolved.** The Darwin-only dispatch can no longer publish a
successful or skipped check named `CI`. This narrow verdict does not authorize
an actual Darwin dispatch and does not qualify the production source.

## Darwin qualification summary

The special event has its own `darwin-qualification-summary` job named
`Darwin qualification summary`. It runs only for
`workflow_dispatch && inputs.darwin_qualification`, requires the Darwin matrix
to succeed, and rejects any normal job that did not skip.

The normal `ci-ok` job is guarded off for that event. Its conditional display
name evaluates to `Full CI not run (Darwin-only dispatch)` on the special event,
including when GitHub publishes the disabled job as a skipped check. Therefore
neither special-event job can emit the required `CI` context.

## Normal-event preservation

For push, pull request, merge group, and ordinary manual dispatch events,
`ci-ok` still evaluates to the literal display name `CI`, retains the original
normal-job `needs` set, and runs the original planner-derived verifier body.
The Darwin job is not added to the normal summary's dependencies. Removing the
conditional name and guard from `ci-ok` recovers its prior definition exactly;
all normal job bodies and the planner contract remain unchanged. The only
planner-side routing change remains the already reviewed special-dispatch guard
on `changes`.

## Validation

- Inspected the exact commit diff and source at `7e887cb25`, not merely the
  later worktree head.
- Confirmed `.github/workflows/ci.yml`, the local validator, and the Darwin
  route README are unchanged between the target and the current head.
- `git diff --check 7e887cb25^ 7e887cb25`: pass.
- Repository-configured actionlint 1.7.7 on the byte-identical current workflow:
  pass.
- Parent-reported `validate_local.py`, actionlint, and planner tests in ordinary,
  wagodebug, and wago_regalloccheck modes: pass. They were not redundantly rerun
  by this review except for the targeted actionlint check above.
- No dispatch, push, benchmark, timing run, production edit, or Darwin execution
  was performed.

An actionlint invocation against a copy in `/tmp` was discarded because moving
the file outside the repository prevented discovery of the repository's custom
runner-label configuration; its only diagnostics were the already configured
custom runner labels. Running against the repository path passed.

## Remaining exact blocker

Activation remains blocked independently of CI-1. The reviewed production pin
`5345bd365c13963f1bf60e26bb80789ab9ead4cc` has open runtime findings DR-1 and
DR-2 (executed regressions in all three modes) and DR-3 (a source-verified
non-Linux publication gap). A repaired production source must be reviewed and
qualified, then the Darwin harness must be refreshed to pin and attest that new
source identity. Until both occur, the now activation-safe CI routing does not
authorize dispatch.

Existing unrelated dirty files and submodule state were preserved.
