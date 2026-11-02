# Exact-pin activation review

**Activation YES**, for harness `336416d2a006bbefded9e5b0b8539199bd6b0e3b`
and explicit source input `da19f1a8d29fec7305d937a42622a198b8e79717`.
**Full platform qualification NO.** This review authorizes no publication or
execution; parent still owns activation and the independent ARM64 review.

Reviewed on 2026-11-02 in the shared experiment-rfwgc-port worktree. Read the
local AGENTS.md from /home/dev/Projects/wago (absent in this worktree), current
final-darwin README, commit diff, drivers, setup, workflow, pinned test source,
and previous final-darwin-repaired-review and final-darwin-graph-recheck.
The shared HEAD advanced during review; the reviewed workflow, complete
final-darwin directory, CI-policy and planner remained byte-identical to the
requested harness commit. All 16 recorded harness-manifest entries match.
This review does not extend to later production commits.

## Finding to parent (nonblocking for the explicit-pin command)

`.github/workflows/ci.yml:27` retains source_sha default
`f2bb27287220783e86a4ead41fee78693a5129de`. The new plan/identity allow only
`da19f1a8d29fec7305d937a42622a198b8e79717`. Omitting the source input therefore
fails before source checkout/setup. It cannot execute or falsely qualify the
old source. Exact fix: replace that default with the new full source SHA,
refresh its recorded workflow hash and local validation, and review the new
harness commit before activation. No edit made here. The documented command
already supplies the correct explicit source input, so this is not a blocker
for that command. Do not inherit the prior review's assertion that the workflow
default matches the source pin.

## Activation and identity findings

The workflow independently checks out source at inputs.source_sha and harness
at github.sha. Identity validation requires the full reviewed source, exact
execution HEAD and clean harness. The broad driver repeats identity validation,
source HEAD and dirty-tree checks before creating output or running commands;
it also checks initialized submodules, native Darwin ARM64, Rosetta refusal,
Go 1.27.1 and inherited overrides. Thus a focused contracts failure cannot
bypass source/harness identity when broad execution continues. Both drivers
compare before/after manifests. The harness commit changes no src paths.
Only the declared test overlay is used for focused contracts.

The broad step's explicit !cancelled() condition allows it after a contracts
failure only when darwin-setup.outcome is success. Setup has ordinary implicit
success gating; failed initial identity, checkout or tool installation prevents
setup success. Setup's pipefail preserves failures through tee. No
continue-on-error hides a failure. The failed focused step leaves its job
failed even when later diagnostics pass. Diagnostic lane failures do not break
the driver loop, and its aggregate exit remains nonzero. Exceptions or crashes
fail closed; a failed/incomplete run is not qualification.

Normal job bodies, ci-ok dependencies, conditional display name and special
summary remain intact. On a special event there is no successful or skipped
check named CI: ci-ok is named Full CI not run (non-Linux dispatch). The special
summary requires both native matrices success and all normal jobs skipped;
the complete dependency graph is acyclic. Limits remain three 360-minute Mac
jobs and three 30-minute Windows jobs (1,080 + 90 job-minutes per attempt),
plus summary, with seven-day artifact retention. These are configured limits,
not measurements.

## Census and output audit

- Cancellation file is byte-identical to source 30b0813f7881a6fbb938a21535a205803bd27a0c;
  SHA256 a7a719c91e0afe10dab1440814b572a77815b5b219928fe4ef80d80225c445ac.
  Admission, GC progress and cancellation assertions retain their intrinsic
  three-second deadlines. The enclosing 30-second command timeout does not
  weaken them. Exactly one top-level pass and no skip/failure are required.
- The direct Core 3 child uses protocol 2, the explicit matching nonce, fixture
  gc/const-expr-gc.wast and explicit bounds. Exactly one matching marker with
  10 modules/18 assertions and zero failed/skipped counts is required alongside
  exact top-level pass, zero command exit and successful package census.
  Missing/duplicate/malformed/wrong-nonce/gapped payloads fail. The nonce is a
  fixed diagnostic identity token, not a cryptographic security claim.
- The pinned parent still enforces 215 modules/690 assertions, rejects skips
  before accumulation, and retains its 30-second child budget. No 205/672
  adjustment was made. The broad plan retains all 63 commands and adds nine
  discriminators across modes.
- Raw JSONL and decoded child output are retained. Rejection phase, reason and
  source line map through commands.json to module_definition filenames;
  full command data and fixture hashes retain assertion sites. Mapping line 96
  to commands.5.wasm is tested. Unemitted reasons are not invented.
- Real Go JSON decoding is combined with process exit and failure events;
  a valid marker cannot override nonzero exit or failed package census.
  Non-JSON diagnostic lines are retained in raw output rather than treated as
  test passes. Exact narrow censuses reject absent/duplicate top-level passes.
- All six graph leaves must pass exactly once; missing or duplicate leaves
  fail. Fixtures preserve the prior graph-option repair and all Wasm hashes,
  64 invocations, result oracle, collection, >=2,048 graph allocations, exactly
  32 graph live objects and zero noGC allocations. The ten repaired-test names
  are unique; removal of any required pass fails. Windows retains four
  storage/rejection/admission tests, not a converted-GC-JIT claim.

## Executed local checks and evidence

The existing validate_local.main was executed with only its final receipt write
redirected into this owned directory. Reproducer: `_probes/validate.py`.
Environment: PYTHONDONTWRITEBYTECODE=1, GOTOOLCHAIN=local,
GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1,
PATH=/home/dev/.local/share/mise/installs/go/1.27.1/bin:/usr/bin:/bin.
Verified version: go version go1.27.1 linux/amd64.

`local-validation.json` and `.log` retain PASS for Python/Bash syntax,
actionlint 1.7.7, CI policy, planner tests and full-profile verifier in ordinary,
wagodebug and wago_regalloccheck, normal CI equivalence, 64 summary states,
identity negatives, Linux native refusals, hashes and census negatives.
No existing validation receipt was rewritten.

`_probes/driver_checks.py`, `independent-checks.json` and `.log` retain 27
independent mocked orchestration cases across all three modes. Each verifies
success, diagnostic failure continuation, child nonzero exit, malformed marker,
9/17 plus 1/1 gaps, package census failure, and early identity/source/dirty
refusal. All passed. Mocks exercise the actual driver control flow but are not
native runtime evidence. Additional checks remove/duplicate each graph leaf,
remove each repaired pass, compare cancellation bytes and verify source/harness
pins. No broad runtime duplicate was executed.

## Separate open gates and scope

Native Darwin/Windows at this source remain NOT_RUN. Darwin cancellation,
Core 3, repaired contracts and broad correctness require actual artifacts.
Darwin TinyGo with reviewed compiler/runtime identity and matched baseline
graphs at a3c08365f531eb3c9003e3acf3e0a822c12c31f4 remain open. ARM64 implementation
review and Linux ARM64 cancellation remain separate parent gates. A suspected
converted-global initializer rejection is not a verified runtime finding here.
No sibling runtime audit was triggered: the only finding is CI input-default
configuration, with no runtime counterpart.

Only this review directory was written/committed. No production/workflow edit,
push, dispatch, PR, benchmark, timings, Claude review or subagent. Existing
unrelated dirt, index and submodules were preserved. Probes are under _probes;
no discoverable Go package was added. Review evidence cannot affect production
performance. The report commit is the commit introducing this directory.
