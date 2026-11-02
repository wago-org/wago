# Next native Darwin qualification: prepared, not dispatched

Execution source: **7e41fafbbcd737ed1edce11b94b43b4eda134dcf**.
`revision-7e41/ancestry.json` verifies inclusion of the Darwin port
3f616188315096d7ce1b5fe96b747516f99ffff0, ARM64 router repair
d6f4e94fe671542d7e22fe84e8fe1f9358661c3a, 3242 unsupported-adapter fix,
15d7b24b7 namespace follow-up, and checksum-only installer repair ddc488cc0.
The 7e41 validation-ordering follow-up is included. Earlier 3242 results do not
qualify these later changes. Parent reports the follow-up production review CLOSED with evidence
495a5cd7e89af01cf0c4d82c5fad7fad9ec5221c; Darwin scope review 8526d0610 and
ARM64 router review b33f0f5c5 report no scoped blocker. Independent harness
activation review remains required before publication or dispatch.

**Native Darwin and Windows execution at this pin: NOT_RUN.** Local cross-builds
are compilation/census evidence only. Native TinyGo remains OPEN. No push,
dispatch, PR, benchmark, timing run, Claude review, or subagent was used.

## Positive ownership gates and declared test overlays

The focused Darwin command requires **72 exact top-level passes**, no failures or
skips, six exact graph leaves, and all eight mapped migration child passes.
Missing, duplicate, or unexpected graph leaves fail. `revision-7e41/contracts.json`
is the complete selector, source-hash and fixture-hash manifest. It includes:

- Exact `TestConvertedMake42ClassificationAndReceipt` and
  `TestConvertedInitializer55PersistentRoot`; their assertion bodies are unchanged.
- Original positive `TestExternrefGlobalHoldingConvertedGCReference` and all
  Darwin-selected `TestR9H2*`, `TestR9Ownership*`, and `TestManagedFunctionHostShape`.
- **22 RR/admission tests** from seven Linux-gated test files, with only their
  build constraint adapted for Darwin and gofmt formatting. One unchanged `awaitGCLifecycle` helper
  is extracted from an otherwise Linux-gated test file. The original five-second
  helper bound and all test assertions remain. These are explicit test overlays,
  not runtime/platform implementation changes; native results remain required.
- Existing graph/scalar/noGC fixtures with unchanged Wasm bytes and assertions.
  Only the previously reviewed Darwin build constraint and removal of the
  AMD64-only `gc-lifetime-analyze` option differ from the source fixture.

The old Darwin unsupported-adapter gates cannot continue asserting that valid
converted publication fails. Their **names remain present** in a test-only
Go overlay, with these required positive replacements:

| Historical gate | Required supported-adapter checks |
| --- | --- |
| UnsupportedConvertedPublication | public global and public table ownership (make(42) and initializer(55) remain separate exact top-level gates) |
| UnsupportedBulkAtomicity | bulk store hooks, active initialization, independent containers and rollback |
| UnsupportedConvertedPublicToken | public global token classification, host container authority, host import authority |

Every mapped child must pass exactly once. Positive ownership, invalid/foreign
identity rejection, rollback, collection and close lifetimes are exercised;
there is no blanket acceptance of a formerly rejected store. The literal old
unsupported bodies remain untouched in the pinned source and remain active on
Windows. The overlay is applied to both focused tests and every broad Go command
so a root replay does not accidentally reintroduce the obsolete Darwin adapter
expectations. All original recipe argv remain in `plan.json`; execution records
add only explicit `-mod=readonly` and the hash-checked test overlay. Effective
argv, overlay map and original recipe are retained separately.

Windows now requires **six** exact contracts: the original four storage,
rejection and admission gates, plus
`TestUnsupportedConversionPublicGlobalRejectsOpaqueBeforeDirectory` and
`TestUnsupportedConversionGlobalValidationOrdering`. There are no Darwin/RR
source replacements in the Windows overlay. This is not Windows GC JIT qualification.

## Unchanged broad route, failure handling and matched baseline

All **63 original broad commands** remain, including ordinary/default diagnostics,
established diagnostics, race, guard, backend, collector, root/CLI/installer,
corpus and official spec routes. No failures are reclassified as success.
The three independent narrow commands per mode remain:

1. Unchanged `TestSharedEntryGuardCancellationAndGC`, exact pass and no skips.
   Its intrinsic cancellation deadline and GC/admission assertions are untouched.
2. Core 3 child protocol 2: exactly **10 modules and 18 assertions**, no gaps,
   one authenticated fixture/nonce marker, one test pass and process exit zero.
   Complete output, commands.json, fixture hashes and emitted rejection sites are retained.
3. The original ten repaired contracts, including the positive converted-global
   and converted-table contracts.

Broad Core 3 still requires **215 modules / 690 assertions**. Both spec-v3 routes
retain `WAGO_SPEC_REQUIRE_ZERO_GAPS=1` and all assertions. No census is reduced to
match the prior failing run's 205/672 or child 9/17 results. Catalog exclusions,
Go skips with reasons, failures, process exits and package census exits are retained.

The new matched baseline supplement checks out exact baseline
**a3c08365f531eb3c9003e3acf3e0a822c12c31f4** independently. It uses the **same graph
Go fixture and three Wasm hashes** as the candidate, with no baseline-specific
oracle edits. Baseline lacks `NurseryRegionShift`, so its exact expected leaves
are noGC/false, scalar/false, graph/false; these match the corresponding candidate
leaves. Candidate true leaves remain separately required and have no baseline
true counterpart. Both sides require the existing 64 invocations, result 7,
collection, graph allocation/live-object counts, and noGC allocation assertion.
This is bounded correctness comparison, not a benchmark or a speedup claim.

Baseline runs before the broad route with a 20-minute step limit. After successful
setup, baseline and broad steps use `!cancelled()` rather than success-only
conditions, preserving failures while continuing independent cells. Broad lanes
also continue after failed commands. There is no continue-on-error or weakened
aggregate. Setup failure/cancellation prevents unsafe execution. Always-upload
steps retain failed/partial artifacts and setup logs; timeout-interrupted runs
remain incomplete and cannot qualify missing cells.

Across all modes: **81 go test commands** = 63 broad + 9 narrow + 3 focused Darwin
+ 3 baseline + 3 Windows, each paired with a package census command. Top-level
focused requirements are 216 candidate passes, 3 baseline passes (9 leaves), and
18 Windows passes; overlapping nested/replayed checks are not unique test counts.
No TinyGo execution is hidden in this inventory.

## Identity, tools, CI isolation and bounds

Both full `source_sha` and full `harness_sha` are mandatory explicit dispatch
inputs. The obsolete default source was removed. `identity.py` verifies source
against the plan, harness input against workflow execution SHA, clean harness
HEAD, and full SHA syntax. Runtime drivers also require independent clean exact
source checkouts, native OS/architecture, exact Go **1.27.1**, and no target/test
overrides. Darwin rejects Rosetta; its broad source requires initialized exact
submodules. Source and harness manifests are checked before/after execution.

Setup requires explicit matching `GOROOT` and `$GOROOT/bin/go`, `GOTOOLCHAIN=local`,
`GOENV=off`, WABT **1.0.41** through the existing verified bootstrap, Node
**24.22.2** with pinned Darwin ARM64 archive SHA256, PowerShell **7.6.8**, Just
**1.58.0**, and the pinned official spec interpreter. Tool hashes and bootstrap
provenance, Go environment, runner OS/Xcode/Python and opam package versions are
retained. Runner infrastructure is recorded, not claimed hermetic.

The 6c71 native ARM64 RR3 job bodies, summary, **138 job-minute** ceiling and
concurrency groups are byte-preserved. No action is taken on runs 41442912023 or the active rerun 41445401972.
A Darwin-only dispatch uses the existing distinct concurrency group and cannot
cancel that ARM run. Normal CI planner/jobs/required-CI isolation are unchanged;
special dispatches never produce a successful required check named CI.

Darwin configured bounds remain **3 × 360 = 1,080 Mac job-minutes** and Windows
**3 × 30 = 90 job-minutes**. Baseline's 3 × 20 step allowance is *inside* the Mac
ceiling. The special summary now has an explicit **3-minute** limit: total Darwin
profile ceiling **1,173 job-minutes**, separate from ARM's 138. These are configured
maximums, not runtime/cost estimates. Seven-day artifact retention is unchanged.

## Installer repair and local verification

The actual GOWORK=off missing-go.sum error was reproduced from immutable 3242
files in a private module copy. A fresh cache downloaded only the already-pinned
Wago beta.13 and x/sys v0.30.0 via proxy.golang.org and sum.golang.org. Four verified
checksum entries were committed as **ddc488cc0**; no versions, go.mod, installer
code or other production files changed. Official sumdb lookup receipts, original
failure, download logs, read-only tests and module verification are retained in
`revision-3242/`. Standalone installer tests and the root named-installer build
check passed in all three modes. Read-only sibling audit found no corresponding
installer module in Slipstream or Starshine. No fixture substitutes for a missing
published package, and no `-mod=mod` is used in qualification.

Current-pin results belong in `revision-7e41/`:

- `local-validation.json`: syntax/actionlint, real-Git identity refusal fixtures,
  Linux native-execution refusals, exact/negative census and 64 aggregate states;
  CI policy, planner and full-profile checks in all three modes.
- `export-validation-final/`: nine cross-builds (candidate Darwin, Windows, baseline
  Darwin × three modes), exact test selection and ignored-file census; all six
  relevant local installer tests and module verification at the final source.
  Every source and baseline Git blob is checked before and after. Cross-builds
  are **not native execution evidence**.
- Failed preparation builds and earlier 3242 validation remain in `revision-3242/`.
  The missing helper was repaired in the harness; no failed log was deleted.
  `gofmt-hook.json` verifies the commit-hook edits are exactly gofmt of the
  retained pre-format bytes. Final checks were refreshed after formatting.
  `guard-before.log` retains the guard-tag compile failure from duplicate calls
  to intentionally non-guard probes; those duplicates were removed from the
  migration wrapper, retaining both exact top-level probes and the positive
  global/table checks. `broad-tag-validation/` adds all-three-mode cross-builds
  for guardpage, profile and codegenstats tags.

Reproduce local harness contracts with explicit Go 1.27.1/GOROOT and
`python3 docs/rfwgc-evidence/final-darwin/validate_local.py`. For fresh private
Git archives, `validate_exports.py SOURCE BASELINE NEW_OUTPUT` records and checks
all cross-builds and installer checks without modifying the archives.

## Remaining activation gaps

The follow-up production review is closed as recorded above. Parent must complete
independent harness activation review, then authorize publication. The final report names the exact
committed harness SHA; use that full SHA as `harness_sha` and the source pin above
as `source_sha`, with only `darwin_qualification=true`. Verify the published branch
HEAD equals the reviewed harness before dispatch. No dispatch was made here.

Native Darwin ownership, cancellation, full corpus/spec census, Windows follow-up
contracts and matched baseline graphs remain **NOT_RUN at 7e41** until complete
native artifacts pass. Platform skip census still needs parent review. Prior
3242 Linux/QEMU results and old native run 41431926958 do not close these gates.

**Native TinyGo remains OPEN:** a separately reviewed native Darwin compiler
archive/hash, exact patched runtime identity and subsequent all-three-mode native
foreign-stack/evacuation/conversion execution are absent. `tinygo_setup.py` remains
preparation only; no TinyGo pass or silent exclusion is claimed. Adding that
execution requires a separately reviewed bounded plan, outside the 81 commands.
The Nov 4 deadline does not waive these gaps.

Harness/checksum evidence cannot change shipped runtime performance. No measurement
lease or performance qualification is claimed or needed for these changes.
