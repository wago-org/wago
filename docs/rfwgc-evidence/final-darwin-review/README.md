# Fresh activation review of `96f0700d2`

Review scope: `.github/workflows/ci.yml` and
`docs/rfwgc-evidence/final-darwin/{setup.sh,identity.py,run.py,plan.json,validate_local.py,README.md}`
at `96f0700d298f48c81e07487d63472bfd8e102070`, against source target
`5345bd365c13963f1bf60e26bb80789ab9ead4cc`.

## Activation verdict

**NO: not activation-safe at this commit.** One verified workflow blocker must be
fixed before the Darwin dispatch is activated. This is a review of the dispatch
route only, not broad approval of the RFWGC port. Darwin execution is still
required by the parent before the final port PR.

## Verified blocker

### B1: the special, deliberately incomplete dispatch publishes the required `CI` check name

The existing `ci-ok` job is named `CI` and is documented in the workflow as the
sole required status check. The new Darwin-only path reaches that same job after
all normal jobs skip, and the special step exits successfully when the three
Darwin matrix jobs succeed. Therefore a manual Darwin dispatch can publish a
successful `CI` check on the branch commit even though normal CI did not run.

Required checks are associated with the head commit and check context; they do
not encode this harness's statement that the special run "does not qualify full
CI." If a PR is subsequently opened or updated at that same SHA, this successful
special-run `CI` context can satisfy the required check before a normal PR run
has established the real full-CI result. That violates the policy that no unrun
gate ships.

Precise correction: give the special path a check/job name that can never equal
the required `CI` context. The robust form is to split the summaries:

- keep `ci-ok` named `CI`, guard the job against
  `workflow_dispatch && inputs.darwin_qualification`, and leave its normal
  verifier contract intact;
- add a separate always-evaluated special summary job named, for example,
  `Darwin qualification summary`, with the current Darwin-result and
  all-normal-jobs-skipped assertions;
- ensure no job named `CI` succeeds during the special dispatch; and
- extend local validation to assert the special workflow's emitted summary job
  name differs from `CI`, in addition to testing its result-state script.

A conditional expression in `ci-ok.name` could also separate contexts, but two
summary jobs make the required-context boundary explicit and easier to validate.
After correction, rerun actionlint and the CI planner tests in all three modes,
then review the exact workflow diff before dispatch.

## Verified contracts (static/local)

- Existing manual dispatch registration can execute the workflow version from
  the supplied branch ref. The opt-in boolean defaults false; normal manual
  dispatch remains on the planner path. Push, PR, merge-group and ordinary
  manual job bodies are unchanged by the reviewed diff except for the planner
  guard and summary dependency/guards needed to route the special dispatch.
- Harness checkout is pinned to the dispatch `github.sha`; source checkout is
  independent and pinned to the input. `identity.py` accepts only the exact
  lowercase 40-hex source SHA from `plan.json`, requires harness HEAD to equal
  `QUALIFICATION_DRIVER`, and rejects a dirty harness before source checkout.
  `run.py` repeats source/harness identity, independence, native Darwin ARM64,
  Rosetta, clean source/submodule, Go target/version, and inherited-environment
  checks before creating output or executing tests.
- Setup refuses non-native Darwin ARM64, requires Go 1.27.1, just 1.58.0, cgo and
  Xcode tooling, and checks clean pinned submodules before and after bootstrap.
  The Node 24.22.2 archive digest matches Node's published SHASUMS256; the
  PowerShell 7.6.8 archive digest matches its GitHub release asset digest. The
  source-pinned WABT bootstrap has a Darwin ARM64 1.0.41 digest and provenance
  verification. The official interpreter bootstrap pins submodule revision
  `9d36019973201a19f9c9ebb0f10828b2fe2374aa` and verifies compiler/tool/binary
  provenance. Homebrew/opam metadata and the runner image remain honestly
  recorded infrastructure, not hermetic pins.
- `plan.json` contains 63 unique lanes: 21 each for ordinary, wagodebug and
  wago_regalloccheck. Commands are `go test -json` correctness commands, with no
  benchmark selection. The driver records effective WAGO environment, package
  build-file census, raw JSON events, failures, skips and pass-event counts;
  zero-test or census-failure lanes fail. It does not promote skipped bodies to
  passes, and the final qualification remains `REQUIRES_REVIEW`.
- The special concurrency suffix prevents a Darwin-only dispatch from canceling
  ordinary CI on the same ref. Its matrix result and all-normal-skipped assertion
  are otherwise fail-closed for failed, canceled or skipped Darwin execution.

## Honest gaps, not blockers in this narrow route review

These remain required supplements outside the 63-command route and must not be
credited as Darwin passes:

- patched TinyGo native ARM64 foreign-stack runtime, evacuation, joint and
  supported-conversion execution in all three modes: `NOT_RUN`;
- generated GC graph/scalar/noGC overlays and their stated 32-live-object / 2048
  allocation oracle: `NOT_RUN`;
- the H6 ownership/wakeup Darwin adapter with both bodies and package/test
  census: `NOT_RUN`;
- review of ignored build files, Go skips, Linux-only profiling/perf paths,
  AMD64-only scalar bodies and captured external-runtime oracle selections:
  `REQUIRES_REVIEW` after real artifacts exist.

The route is appropriately labeled as incomplete evidence, but that labeling
does not cure B1's required-check collision.

## Local validation performed

- `go test ./tests/tools/ci-plan -count=1`: pass.
- `go test -tags=wagodebug ./tests/tools/ci-plan -count=1`: pass.
- `go test -tags=wago_regalloccheck ./tests/tools/ci-plan -count=1`: pass.
- `actionlint` v1.7.7 on `.github/workflows/ci.yml`: pass.
- Bash syntax for `setup.sh` and Python compilation for the three Python drivers:
  pass.
- Node and PowerShell archive digests were cross-checked against their upstream
  published metadata: match.
- No dispatch, push, PR, benchmark, timing run or Darwin execution was performed.

Existing unrelated dirty, indexed and submodule state was preserved.
