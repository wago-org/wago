# Independent activation/protocol review

**Activation: NO for the submitted proposal. Native Darwin ARM64 TinyGo: NOT_RUN.**
This is a bounded activation-safety review, not native qualification or approval
inferred from cross compilation. No production or installed workflow was changed,
no dispatch/push/PR/benchmark/subagent/watch was performed.

Reviewed supplement commit: `a60b1a57f9b973952747177d541480a21b65ea01`.
Execution source: `7e41fafbbcd737ed1edce11b94b43b4eda134dcf`.
Review started at worktree HEAD `8a91f426a24d466bbd5012fdca3c4903f83f2f68`.
Every tracked supplement file matched the reviewed commit before and after checks.
Exact SHA-256 identities:

- `harness.py`: `583192e4f217ff8ea2fc56eb16a0ed23f60df05ec679ff1a553a62d9890ffe82`
- `workflow.proposal.yml`: `d91cee62f83741df4e154b7675d069afa9db411ef95f68599d7dcfabc8e9597a`
- `workflow.proposal.diff`: `3d75059c1a2ecff9de0b4e652c5a17532d483ff7e68149bb1334de97a02c5cba`

## Blocking activation finding

The diff adds a NEW `.github/workflows/darwin-tinygo-supplement.yml`.
GitHub requires a workflow to exist on the default branch before it receives
`workflow_dispatch`; selecting a feature-branch ref does not register a new
workflow. Therefore installing this new file only on the work branch cannot
activate the proposed run. See [GitHub's workflow syntax contract](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_dispatch)
and [manual execution instructions](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow).
Do not work around this by pushing upstream main.

The exact alternative is `ci.integration.proposal.diff`, with its complete YAML
in `ci.integration.proposal.yml`. Both are review artifacts, NOT installed.
They extend the existing registered `ci.yml` on the reviewed feature branch:

1. Add default-false boolean `darwin_tinygo_supplement`; reuse exact source/harness
   string inputs. Select this flag alone; conflicting qualification flags fail.
2. Add dedicated native matrix and summary jobs. Keep 110-minute native jobs,
   three modes, max-parallel 1, fail-fast false, exact source/archive/compiler
   identities and setup gates. The matrix remains 330 job-minutes plus 3 summary.
3. Exclude the new opt-in from `changes`, normal required `CI` aggregate and its
   steps, and existing ARM/Darwin/Windows qualification jobs/summaries. Preserve
   their old behavior whenever the new flag is false. Rename the skipped normal
   aggregate on this dispatch so it is never presented as full `CI` success.
4. Use a supplement-and-run-ID suffix in concurrency for the opt-in, with
   cancel-in-progress false there. Other events keep their existing groups and
   cancellation behavior. Neither ARM nor ordinary Darwin runs are cancelled.
5. Summary requires native success and every existing job to be skipped. It
   cannot bless a partial matrix or accidentally executed normal CI job.
6. Parent must install/review/commit this integration on the work branch and use
   the NEW full commit as both dispatch ref identity and harness_sha. The source
   input stays `7e41...`. The reviewed supplement commit alone does not contain
   an installed activation workflow. Publication remains through the authorized
   PR/feature-branch process, never a direct upstream-main push.

Both the original proposal and alternative pass local Actionlint 1.7.7. This is
syntax validation, not an executed dispatch test. Re-review the final installed
diff and check default-branch registration of `ci.yml` before activating.

## Compiler, patch, source and execution protocol

Fresh official GitHub API response (`publisher.json`) confirms v0.42.0 Darwin
ARM64 asset 539147844, 163104695 bytes, publisher SHA-256
`493da3585e66c5da677a4be39402faf2041b4e45a61eb456e4547eca753a544e`.
The actual local archive matches. The compiler read directly from that archive
matches `1b74ee7ef9841f01364823be1254833a96832e74076957c956d89872f705b7d2`.
All three pristine and four patched file hashes and four pinned patch/bootstrap
inputs match. The runner extracts a private tool tree; negative controls use
copies and do not patch the shared installation.

Independent `git archive` comparison verifies every regular archived source file
against the retained cross-build export. Stage8 and unsupported-platform copies
change only filename/build constraint; their bodies and oracles match exactly.
Fixture copies match the reviewed templates. No current checkout production
bytes are silently substituted into the export.

Native gate checks Darwin/arm64, rejects Rosetta, validates full harness HEAD and
GITHUB_SHA, requires a clean checkout, exact archive/compiler/patch hashes and
Go `1.27.1 darwin/arm64` / TinyGo `0.42.0 darwin/arm64`. It attests Go executable
hash, OS, Xcode, SDK path, Clang and Python. SDK/OS are attestations, not hermetic
pins. Unknown tooling/setup failures remain red. Native execution builds each
package and directly runs the same SHA-256-checked ARM64 Mach-O; no cross binary
is substituted. Per-command process-group deadlines and job caps bound hangs.

The effective source and full private tool tree are hashed before the native
suite and compared afterward; the four effective driver inputs are also checked.
After-comparison is a boolean receipt, not a second complete manifest. Exceptions
or external cancellation before that point leave INCOMPLETE/failed evidence and
may lack final hashes. Such attempts must not be accepted as qualification.
The successful nine cross builds are correctly labeled CROSS_ONLY; independent
binary hash/header/name checks passed without rebuilding or executing them.

## Fixture/protocol assessment and limits

- STLR/LDAR rendezvous uses aligned 32-bit flags and host atomics. Entry is observed
  before eight runtime.GC calls and release follows their completion. Guest SP
  remains foreign throughout that interval. Return, trap-unwind and interrupted
  trap variants restore the saved trap reentry SP. The interrupted trap variant
  is synthetic; the separate cancellation fixture exercises actual cancellation.
- Prepared entry uses X26 and returns 42. Host roundtrip insertion follows the
  unique BLR X16; inspection of the original stub finds no crossing relative
  branch. The inserted code clobbers scratch X12/X13, which are recomputed or
  unused by the resumed body. Saved LR/X20 and result pointer X19 remain intact;
  20*2+111 gives the asserted 151. Nested entry and parked/resumed collections
  cover the intended lifecycle. These are not exhaustive AAPCS64 register tests.
- TinyGo Apple code uses SIGIO/SA_RESTART, dispatch semaphores, pointer pthread
  identities and exit notification. Patched GC locks the active-task list before
  signaling, normalizes foreign SP to saved host SP, scans current registers and
  registered mappings, then resumes. ARM64 thunks save X19–X30 and low D8–D15 in
  a 176-byte area. Signal-frame layout, root readability and task exit remain
  native requirements. These fixtures do not isolate a uniquely held heap root
  in every register/foreign-stack position: a passing run is lifecycle evidence,
  not exhaustive root-retention proof.
- Actual Darwin libc-backed mmap/mprotect/munmap paths exist. MAP_JIT allocation,
  copy-then-RX and zeroing madvise fallback are selected; no unavailable raw
  syscall is substituted. Unaligned thunk-unmap failure, unregister-before-GC,
  retry Close and owned thunk release are asserted. Some shared fixture cleanup
  ignores Close errors and deferred code-unmap errors; the suite therefore does
  not prove every mapping was reclaimed or inspect kernel permissions directly.
  Actual JIT entitlement/cache/permission behavior still requires native execution.
- Cancellation uses encoded Wasm, an import-entry handshake, repeated GC, then
  context cancel or Close and an interrupted-trap assertion. The import proves
  invocation entry, not that the collector necessarily caught the infinite-loop
  instruction. The runtime foreign-stack fixture supplies the stronger SP
  rendezvous. Blocking Close/GC is bounded by the outer 600-second process limit.
  There is no external-WAT early-return dependency in this added cancellation test.
- TinyGo opaque converted host containers/imports/receipts/public tokens stay
  unsupported. Retagged rejection assertions remain unchanged. Ordinary/null
  storage acceptance and foreign/forged/expired/wrapper classification remain;
  all eight validation-ordering children are explicitly required. Collector helper
  pools and native object-edge/reclaim callbacks are not qualified by this suite.

No independently verified production defect emerged from this bounded audit.
Read-only sibling audit at the recorded Slipstream/Starshine heads found no
TinyGo/foreign-stack implementation in tracked src. Slipstream has no workflow
directory; Starshine has existing manual workflows, not this proposed Wago path.
The activation finding is a GitHub workflow registration constraint.

## Local checks and boundary of the conclusion

`review_checks.py`, `checks.json`, `_controls/`, `discovery.json`, and actionlint
logs retain the bounded validation. All three modes have passing local negative
and census controls. Required top-level counts are 33/36/33 = 102, plus explicit
children. Missing/duplicate/unexpected names, failures, skips and known silent
WAT early-return messages reject. All nine cross binaries were rehashed and
checked for Mach-O ARM64 headers and required names; none was rebuilt or run.
Go package discovery under the supplement yields zero packages in every mode.
No `_cache`, `_source`, `_export` tool/source tree is tracked in the supplement.
Private review outputs are underscore-prefixed and only review-owned evidence
is committed. No benchmark or elapsed-performance sample was taken; this
review-only commit cannot change shipped runtime performance.

The newer `5b02ec67afe0dce181a7f8d508e270c7ecb0ecfc` cleanup is explicitly excluded
from the execution pin. Its entire production delta is deletion of two wrapper
forwarders and conversion of two unsupported-fixture calls to their invocation
entrypoints with an empty context. That is evidence about the diff, not proof of
no compiler/behavior effect. Native results at 7e41 cannot qualify 5b02 without
separate review/revalidation. Do not rewrite the pinned oracle to hide this gap.

Existing Darwin Go run 41447853354 is separate. ARM run 41445401972 is reported
successful by the parent and requires its own audited receipts; neither is
Darwin TinyGo execution evidence. This review did not poll either run.

Deadline pressure does not waive the activation blocker or native gate. The
remaining path is reviewed existing-workflow integration followed by actual
native execution and audit of all modes, source/tool identities and failure-free
censuses. Until then native Darwin TinyGo remains NOT_RUN.
