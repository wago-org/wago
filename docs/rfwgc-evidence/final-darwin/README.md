# Reviewed-dispatch Darwin correctness route

**Activation requires parent review of this implementation. Darwin execution remains
NOT_RUN in ordinary, wagodebug and wago_regalloccheck.** No push, dispatch, PR,
benchmark or timing measurement was performed by the implementation child.

Immutable production source: `5345bd365c13963f1bf60e26bb80789ab9ead4cc`.
`942fe6c7d` is the later comment-only source clarification, not the tested source.
The harness is checked out at the dispatch's `github.sha` in `harness/`; source is
checked out independently by exact SHA in `source/`, with recursive pinned
submodules. Both identities are verified before tests. The driver records its
commit and hashes, the source manifest (including submodules), and post-run
integrity. No live developer workspace is used.

## Integration and activation

The already registered `.github/workflows/ci.yml` accepts optional boolean
`darwin_qualification` (default false) and string `source_sha`. Only an explicit
true manual dispatch selects the three `macos-15` matrix jobs. The source input
must equal the full reviewed SHA above; abbreviated SHAs, branches, arbitrary
commits and shell text fail before source checkout. The normal `changes` planner
is skipped for this special dispatch; its dependents and the draft-only smoke
job consequently skip. A separate `Darwin qualification summary` requires Darwin matrix success and
all normal jobs skipped. The normal summary is disabled and renamed
`Full CI not run (Darwin-only dispatch)` for this event, so even its skipped
check cannot impersonate the required `CI` check. This summary attests command
execution, not full CI qualification or closure of the remaining Darwin gaps.
Normal push, PR, merge_group and manual dispatch retain their planner and summary
contracts. The Darwin concurrency suffix prevents canceling ordinary CI on the
same ref. No new workflow registration or default-branch change is needed.
`workflow.proposal.yml` is now only a pointer to this integration.

After review, the parent must publish the port branch containing this harness,
workflow and plan, and ensure the exact source commit is reachable from that
repository (it is an ancestor of this local branch). Follow the parent's
publication policy; this document grants no direct-main publication authority.
Check the published branch head against the reviewed evidence commit and verify
source availability before activating:

```sh
gh api repos/wago-org/wago/git/ref/heads/experiment/rfwgc-port --jq .object.sha
gh api repos/wago-org/wago/commits/5345bd365c13963f1bf60e26bb80789ab9ead4cc --jq .sha
gh workflow run ci.yml --repo wago-org/wago --ref experiment/rfwgc-port \
  -f darwin_qualification=true \
  -f source_sha=5345bd365c13963f1bf60e26bb80789ab9ead4cc
```

Do not activate an unreviewed later branch head. `host.json` must identify the
reviewed harness commit; compare the run's `headSha` before accepting results.
The parent downloads all three artifacts and complete Actions logs, including
checkout/setup failures. A missing artifact, lane, final summary, canceled job or
timeout is incomplete evidence, never a pass. Seven-day retention is configured.

## Executable setup and its limits

`setup.sh` refuses Linux/x86/translated execution, checks exact source and Go
identity, clean source/submodules, and cgo/Xcode availability. It provisions:

- Go **1.27.1** and just **1.58.0**, via commit-pinned setup actions.
- Native Node **24.22.2** and PowerShell **7.6.8**, using hardcoded archive SHA256
  values from Node's release SHASUMS256 and PowerShell's GitHub release asset
  digest. Missing runner installations are not silently accepted.
- WABT **1.0.41**, via the source's checksum-verified native ARM64 bootstrap.
- OCaml **4.14.3**, dune **3.22.1**, menhir **20260209**, via a fresh isolated opam
  switch. The official Release 3 interpreter is built and verified from pinned
  submodule revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa` by the source script.

Homebrew installs the opam bootstrap; Homebrew/opam, the opam repository metadata,
macOS image, Xcode and Python are recorded infrastructure, **not hermetic pins**.
Exact interpreter compiler/parser versions are requested; the installed opam
package list and switch export are retained. Setup logs record native versions,
image identity and tools; WABT/interpreter provenance and executable hashes are
uploaded. Setup is implemented but has not executed on Darwin. Any installation,
ABI or source-build failure fails the job and remains evidence. There is no
fallback to stock/missing tools or skipped setup. TinyGo is deliberately not
installed as an apparent substitute for the separately patched runtime.

## Execution scope and census

`plan.json` retains each prior command receipt and SHA256. The original 51
commands cover root packages, collector/race/guard/backend/runtime CLI/installer,
corpus, host transitions, hardening, profiling correctness, diagnostics,
shared-scalar controls and seeded concurrency. Twelve additional commands run
explicit official spec v1, v2, v3 and v3-signals gates in all three modes with
zero gaps required. Absolute Linux tool/source paths are replaced by setup paths;
`-failfast` is removed so failures do not suppress later test bodies. No command
selects `-bench`, fuzz timing, native performance capture or measurement recipes.
Framework durations in raw test output are incidental correctness data.

The environment audit retained `WAGO_APP_CORPUS_REFERENCE=all`: this means fresh
supported wazero comparisons plus explicitly skipped, previously captured
external-runtime oracles, not fresh wasmtime/V8 processes. `WAGO_CORPUS_TIMEOUT=1m`
is a portable per-workload safety limit. Guard/signals support Darwin ARM64.
Shared-scalar AMD64-only bodies and Linux profiling clock/perf paths can be
excluded on Darwin; package census and skip logs must be reviewed. Linux-only
semanticcorpus tests and Linux/QEMU adapters are not made portable by replaying
this plan. The catalog platform census records allowlisted command omissions
and captured oracle selections, which otherwise need not emit Go skip events.

Every lane retains effective command/environment, `go list -json -test` package
build-file census (including race/tag selection), full JSON test output,
structured failures/skips, passing-test counts and exit codes. Zero-test lanes
fail. Progress is written before each lane; failures do not stop later lanes.
A successful process with skips is `PASS_WITH_SKIPS`; the final qualification
field always remains `REQUIRES_REVIEW`.

The plan and every final summary explicitly retain these unmet supplements:

- **Patched TinyGo native ARM64:** reproduce the stage8 foreign-stack runtime
  patch set and exact tree hashes on Darwin, then evacuation, joint/foreign-stack
  and supported conversion commands in all modes. The ordinary CI scheduler
  patch alone does not establish this prerequisite.
- **Generated GC graph/scalar/noGC controls:** prepare separate hashed
  port/baseline exports and overlays from final-combined-5345 fixtures. Verify
  the graph oracle (32 live objects, at least 2,048 allocations after 64 calls
  and collection). Historical corpus/spec passes do not cover these graphs.
- **H6 ownership/wakeup adapter:** audit the prior ARM64 adapter's Darwin build
  constraints and preserve both bodies with actual package/test census. Linux
  QEMU execution and excluded host opaque receipts do not qualify Darwin.
- **Platform exclusions:** reconcile every ignored file and skip with the
  qualification owner. Count 63 commands as this route's scope, never “all Darwin.”

These are explicit NOT_RUN/review gaps, not waived gates. The source remains
unaltered; future overlays must be separately reviewed and hashed.

## Local verification and cost bounds

`python3 docs/rfwgc-evidence/final-darwin/validate_local.py` verifies receipt
integrity, no benchmark flags, real Git source/driver input rejection and dirty
harness rejection, Linux refusal in all three modes, unchanged normal CI job
bodies/summary contracts, CI planner tests in all three modes, actionlint 1.7.7,
and Python/Bash syntax. `local-validation.json` contains the receipts. This is
Linux harness validation, not Darwin compilation or execution.

Three Mac jobs each have a 360-minute hard ceiling: at most **1,080 Mac job-minutes
per attempt**, excluding queue time and the unchanged lightweight Ubuntu summary
job (its existing default timeout is unchanged). This is a safety bound, not a
runtime estimate; there is no measured completion estimate or deadline guarantee.
The public repository uses standard runners, whose compute is free under
[GitHub's runner policy](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
Artifact retention is seven days; actual artifact bytes, account allowance and
billing balance were not measured, so no total storage-cost guarantee is made.
See [GitHub Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions).
Reruns multiply the job ceilings. No measurement lease was acquired or needed:
this changes only opt-in CI and evidence tooling, cannot affect shipped runtime
performance, and provides no performance result.
