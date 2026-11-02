# Independent activation review

Activation: **YES**, for publication and subsequent bounded execution of exact
harness **5fb47abb4b30472bd74751a5ea96b51d668e488a** with execution source
**7e41fafbbcd737ed1edce11b94b43b4eda134dcf**. No harness activation blocker found.
This verdict does not grant portability/full-port approval or waive a Nov 4 gate.
No parent implementation fix is requested.

The review used a private detached checkout of the committed harness, not the
shared working tree. Local AGENTS rules were read. Only this evidence directory
was written in the shared checkout. No production/workflow change, benchmark,
push, dispatch, PR, Claude review or subagent was used. This documentation-only
review cannot affect runtime performance; no measurement lease was needed.

## Independent checks

`local-validation.json` and its log record a fresh bounded run of the committed
validator, with explicit Go 1.27.1 PATH/GOROOT, GOTOOLCHAIN=local, GOENV=off and
PYTHONDONTWRITEBYTECODE=1. Syntax, actionlint 1.7.7, real-Git identity rejection,
Linux refusal, exact/negative census and 64 aggregate states passed. CI policy,
planner tests and full-profile verification passed in ordinary, wagodebug and
wago_regalloccheck. The validator's generated receipt was copied from the private
checkout; its only modified file was that receipt.

`audit.py` independently checks ancestry, checksum scope, baseline sums, retained
command results, exact selection, preservation and every exported Git blob.
`independent-audit.json` records PASS: 18 existing cross-build receipts and six
installer checks, 18,798 candidate blobs and 7,898 baseline blobs verified against
Git, with identical before/after manifests. These builds and installer checks
were inspected, not rerun or relabeled as native execution.

Installer repair ddc488cc0071df12c3150a26b5b2d2cb1fb771c6 is an ancestor of
source and changes only four cli/wago-installer/go.sum entries. Installer go.mod
and versions are unchanged. Independent baseline root go.sum comparison is
byte-identical to source (SHA256 recorded); both x/sys entries match it. All
four installer entries match retained sumdb receipts. The published Wago module
entries are additional installer checksums, not baseline root dependencies.

## Reviewed contracts

- Full source/harness SHA dispatch inputs, matching workflow SHA and clean harness
  HEAD are enforced before source execution. Native OS/architecture, Rosetta
  rejection, independent clean exact sources, exact Go version, setup GOROOT/Go
  path, native tool versions/hashes, initialized broad-source submodules and
  source/driver integrity checks are retained. Setup failures prevent execution.
- The Darwin selector contains exactly 72 top-level contracts, including the 22
  RR/admission contracts (not 72 plus 22). Exact selection matches in all modes.
  RR adaptations change only build constraints/formatting and extract the
  unchanged bounded lifecycle helper. Graph leaves and eight migration children
  are mandatory; missing, duplicate, skipped and empty selections fail.
- make(42) classification/receipt and initializer(55) persistent-root assertions
  retain post-collection value checks, receipt/identity and retirement checks.
  Positive externref-global, H2/H6 ownership, import/container, admission and
  shape tests remain selected. Windows retains unsupported behavior and exactly
  six rejection/storage/admission contracts.
- Cancellation source is byte-identical to the earlier harness source. Core 3
  child requires authenticated protocol 2, 10 modules/18 assertions and zero
  gaps. Broad parent remains 215/690; spec-v3 assertions and zero-gap environment
  remain. All 63 broad recipes are identical to 336416d2a. Effective argv adds
  readonly modules and declared test overlays without changing original recipes.
- Baseline a3c08365f531eb3c9003e3acf3e0a822c12c31f4 uses the identical graph Go
  fixture, Wasm hashes and assertions. Its three false leaves match candidate
  false leaves; absence of NurseryRegionShift does not require baseline true
  mode. Both retain 64 calls, result 7, collection, 2,048 graph allocations,
  32 live objects and noGC zero allocations. Candidate six leaves remain exact.
- Continuation retains failed command/step outcomes. No continue-on-error masks
  failure. Summary requires both native matrices successful and normal jobs
  skipped. Raw outputs, exits, package/skip census, probe hashes, commands,
  identity and integrity manifests are retained; interrupted artifacts remain
  incomplete. Broad PASS_WITH_SKIPS still requires explicit parent skip review.
- Normal jobs, ci-ok dependency/verification rules, native ARM jobs and summaries
  match 6c71 (apart from the explicit Darwin-summary time bound). The native ARM
  harness is byte-identical to 4af18. Distinct concurrency groups prevent a
  Darwin-only dispatch from cancelling the ARM run. ARM ceiling remains 138;
  Darwin is 1,080 Mac + 90 Windows + 3 summary job-minutes. Special dispatches
  cannot create a successful required check named CI.

## Prior evidence and remaining gates

Read the final-Darwin README/handoff, diff from 336416d2a, native-run
41431926958 failure evidence, Darwin conversion review 8526d0610, unsupported
recheck 495a5cd7e89af01cf0c4d82c5fad7fad9ec5221c and codegen recheck b33f0f5c5.
The old native child exited zero yet correctly failed exact census at 9/17 with
one skipped module/assertion in each mode. Those failures remain failures.
The 8526 imported-global blocker is closed by the scoped 495a review at 7e41;
router production review is separately closed. Neither gives native execution
credit at the new source.

Native Darwin ownership/cancellation/corpus/spec and matched baseline, plus
Windows follow-up contracts at 7e41, remain **NOT_RUN**. Complete all-mode native
artifacts and platform skip review are still required. Native TinyGo compiler
archive/hash, exact patched runtime identity and actual all-mode execution remain
**OPEN**, outside this 81-command workflow. No native TinyGo pass is inferred
from tagged Go tests or cross-compilation.

Parent may publish the reviewed harness through its authorized workflow. Before
dispatch, verify the published execution SHA is exactly the reviewed harness and
use both full pins with darwin_qualification=true only. This review itself makes
no publication or dispatch. Its evidence commit is not a replacement harness pin.
