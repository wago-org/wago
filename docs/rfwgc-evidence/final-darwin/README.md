# Darwin execution route investigation

Source: `5345bd365c13963f1bf60e26bb80789ab9ead4cc`. Prior combined evidence: `2c9f72744`.
Investigation date: 2026-11-02. Target date: November 4; the deadline does not waive execution.

**Darwin correctness remains NOT_RUN in ordinary, wagodebug and wago_regalloccheck.**
No Darwin compilation or execution occurred during this task. Linux QEMU receipts
in the prior evidence do not qualify Darwin. No performance measurements occurred.

## Discovery and blocker

`discovery.json` records read-only GitHub queries and the initial worktree/index state.
Both connected T3 endpoint identities describe the same Linux x64 environment,
not a second Mac. Device discovery returned “This MCP client signed in from
outside a thread.” No SSH config exists. An existing private-key file is not
evidence of an authorized Mac endpoint; its contents were not read. Tailscale
status failed because no local tailscaled was running; no daemon was started.
T3 settings expose no top-level runner/host configuration. No available connected
tool exposes arbitrary remote shell execution on a Mac.

GitHub repository runner discovery succeeded with zero self-hosted runners.
This does not rule out organization runners or GitHub-hosted runners. The exact
source SHA has zero Actions runs. The named experiment branch has no matching
remote branch. The repository is public; the authenticated repository response
reports admin/push access, but that does not authorize this child to publish.
The active CI workflow exists. Its source version selects `macos-15` for Darwin
ARM64, but `just test unit`, corpus and guard recipes do not supply the required
wagodebug matrix. A plain dispatch cannot close the three-mode requirement.

## Concrete proposal for parent coordination

Use the standard GitHub-hosted `macos-15` ARM64 runner. GitHub documents that
label as ARM64 and standard runner compute as free for public repositories:
https://docs.github.com/en/actions/reference/runners/github-hosted-runners
(checked 2026-11-02). Proposed compute cost is $0 on this public repository's
standard runner; artifact storage is separate and no billing balance was queried.
Use seven-day artifact retention. Three jobs each have a six-hour hard ceiling;
this is a safety bound, not a measured runtime estimate or a November 4 guarantee.

`workflow.proposal.yml` is an inactive, reviewable proposal under this evidence
directory. It creates three Darwin ARM64 jobs, downloads pinned Go 1.27.1 and
just 1.58.0, invokes the source's checksum-verified WABT bootstrap, executes the
51-command plan, and uploads receipts. Node and PowerShell must already be
available on the runner; missing tools fail setup rather than waive tests.

Parent actions needed:

1. Coordinate publishing the source and evidence on a non-main branch, and
   install the proposed workflow through a reviewed PR. Do not bypass the
   repository's PR-only upstream rule. Before dispatch, verify the full pinned
   source can be fetched remotely and the selected workflow ref contains this
   exact harness and plan. Workflow-dispatch registration may require the
   workflow on the default branch; parent must choose/coordinate that route.
2. Dispatch only after that coordination. The workflow checks out the source
   SHA independently from the harness branch; it never substitutes latest main.
3. Download the three artifacts and full setup/job logs. Account for every
   planned lane, interruption, failure, skipped test and build-excluded file.
   Resolve any unexpected exclusions before claiming qualification.

No new secret is presently needed for this proposal: authenticated read APIs
already work and Actions can use its built-in contents-read token. Dispatch/write
permission must be verified by the parent at execution time. If choosing a private
Mac instead, the missing inputs are an explicitly authorized hostname, username,
verified SSH host-key fingerprint and existing authentication route. Only if that
route lacks authentication would a scoped SSH credential be needed, delivered by
the parent's private secret tool, never chat. No credential was requested here.

## Prepared harness and scope

`plan.json` copies 17 command families in each of the three modes from the prior
immutable evidence, retaining each original command receipt's SHA256. It covers
root packages, collector, race, guard pages, backend, runtime CLI, installer,
canonical and guard corpus, repeated host transitions and hardening, profile
correctness, diagnostics, shared-scalar controls and seeded concurrency. These
are `go test` correctness commands; none selects benchmarks.

`run.py SOURCE OUTPUT --mode MODE` refuses Linux, translated x86 macOS, a dirty
tracked tree, the wrong SHA or wrong Go version/target. OUTPUT must be new and
outside SOURCE. It records host identity, source file hashes before/after,
command receipts, package build-file census, complete JSON test output, skips,
failure events and test pass counts. Zero-test commands fail. Skips yield
PASS_WITH_SKIPS and require review; the summary never automatically declares
final qualification. Unexpected termination leaves a missing summary/lane and
must be reported as incomplete. Preserve full Actions logs for setup failures.

This proposed native matrix is not a claim that every prior Linux gate applies
unchanged to Darwin. Official spec interpreters, patched TinyGo, generated graph
overlays and target-specific H6 adapters are not installed by this proposal.
The root package census and skip records expose the resulting platform boundary;
the parent must decide and prepare any required Darwin supplements, not promote
Linux passes or absent test bodies into Darwin passes. Keep all source overlays
separately hashed and documented if supplements are subsequently authorized.

Local validation is limited to harness syntax, plan integrity and rejection of
this Linux host in all three modes. See `local-validation.json`. The harness has
not run on Darwin and is not yet Darwin-qualified. Only evidence/harness files
were changed; they cannot affect shipped performance. No CI configuration,
production source, shared submodule, existing dirt or staged content was edited;
no PR, push, dispatch, external setup or contact was performed.
