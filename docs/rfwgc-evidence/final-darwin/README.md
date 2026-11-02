# Reviewed non-Linux execution preparation — source 9180

This intermediate commit retains the provisional 9180 pin. A subsequent
harness-identity commit must pin the CI-repaired source including parent
archived-docs repair 07dd245528e3ce94d126f2e5cf8609ddb2737e6c.
Activation requires a fresh narrow parent review. **Darwin ARM64 and Windows
AMD64 execution remain NOT_RUN in all three modes.** No publication, dispatch,
PR, timing, measurement lease, runtime port or production edit was performed.

Source is exactly `9180f09f7590d029bc90df2fa20c0decc590505a`. The workflow/harness
is independently checked out at the dispatch `github.sha`; source is checked out
at the full input SHA. `identity.py` rejects other source pins, a mismatched
driver HEAD and dirty harnesses. Native drivers repeat source/tool/host checks
and compare source and harness manifests after execution. The historical 96f/7e
reviews and `local-validation.json` remain unchanged; new preparation receipts
are under `revision-9180/`. Old command receipts describe recipes, not results
for the new pin. No old target result qualifies 9180.

After review and parent-authorized publication, verify the remote branch head
matches the reviewed workflow commit and that the source pin is reachable. The
exact activation command is:

```sh
gh workflow run ci.yml --repo wago-org/wago --ref experiment/rfwgc-port \
  -f darwin_qualification=true \
  -f source_sha=9180f09f7590d029bc90df2fa20c0decc590505a
```

The existing input name is retained for compatibility but now selects **both**
native matrices. A fresh attempt produces three Darwin and three Windows
artifacts. Check the run head SHA against the reviewed commit, then retain all
artifacts and setup/job logs. Missing artifacts, setup failures, cancellation,
timeouts, skipped bodies and absent test events do not qualify execution.

The special summary requires both matrices successful and every normal job
skipped. The disabled normal summary is named `Full CI not run (non-Linux
dispatch)`, including its skipped check: no special dispatch emits `CI`.
Normal event job bodies and planner contracts remain unchanged. The normal
aggregate now also depends on all three special jobs, which skip on normal
events. The special summary never depends on ci-ok; the graph is acyclic.
The existing special concurrency suffix isolates normal CI. Neither special
summary success nor contract success is full CI or port inclusion approval.

## Exact new contract selection

`contracts.py` uses Go 1.27.1, CGO disabled, GOWORK off and no inherited build
or target overrides. Hashes bind the source tests, admission policy, TinyGo
patch inputs and supplementary fixtures. Git checkout on Windows disables CRLF
conversion before either checkout. Package build-file census and full JSON test
events are retained. Any failure/skip, missing or duplicate top-level pass, wrong
fixture output or changed source/driver fails the contract. All three modes use
the same exact selection:

| Test | Darwin ARM64 | Windows AMD64 |
| --- | --- | --- |
| TestDesignRepairPublicTableOrdinary | required | required |
| TestDesignRepairUnsupportedBulkAtomicity | required | required |
| TestDesignRepairUnsupportedConvertedPublicToken | required | required |
| TestDesignRepairUnsupportedConvertedPublication | required | not selected: Core 3 not admitted |
| TestRFWStage3MeasurementGuest | required + six exact leaf subtests | not applicable |
| TestQualificationWindowsGCAdmission | not applicable | required |

Thus Darwin requires five top-level passes, including six graph leaf passes;
Windows requires four. The three common tests check ordinary/null public table
storage, rejection without destination mutation, bulk/initialization rejection
and authentication of ownership wrappers before public unwrapping.

Darwin's converted-publication test invokes actual conversion and rejects guest
and public converted publication before writes. Windows `supportsCompleteCore3Backend`
excludes AMD64/Windows: the durable converted-guest test would fail during
Compile, before the intended publication assertion. The separate hashed Windows
fixture instead asserts `UnsupportedFeatureError` and no compiled result for
Core 3. This is real Windows storage/admission execution when run, **not Windows
GC JIT functionality or equivalent execution of the Darwin conversion body**.
No feature mask is weakened to manufacture a Windows pass. Parent must explicitly
accept this support boundary; a demand for converted Windows guest execution
requires a separate runtime/backend implementation task.

## Darwin supplements and remaining boundaries

The original 63-command Darwin route remains, now pinned to 9180. It runs after
the focused contract lane. It retains package census, JSON failures/skips and
platform/catalog omissions; its aggregate always says REQUIRES_REVIEW. Existing
Node 24.22.2, PowerShell 7.6.8 and WABT 1.0.41 hash-checked setup and pinned
OCaml/dune/menhir interpreter setup are unchanged. macOS/Xcode/Python,
Homebrew/opam metadata and runner image remain recorded infrastructure, not
hermetic pins. Setup failure is fatal. Captured external-runtime oracles are
not fresh V8/wasmtime executions.

- **Generated graphs:** `revision-9180/_fixtures/graph_test.go` is the existing
  candidate helper/test excerpt with a Darwin ARM64 constraint and benchmarks
  removed. A Go overlay adds it without modifying production source. It retains
  64 calls, collection, 32 live objects and at least 2,048 allocations for the
  graph; noGC requires zero allocations. All six name/region-state leaves must
  pass. Generated graph/scalar/noGC bytes must equal the separately hashed
  historical fixtures. Candidate execution is NOT_RUN. The separate baseline
  export at `a3c08365f531eb3c9003e3acf3e0a822c12c31f4` and matched baseline graph
  run remain a required supplement; candidate success cannot substitute for it.
- **H6:** `function_environment_roots_host.go` and aggregate host dispatch are
  Linux-only. The earlier ARM64 adapter was explicitly Linux ARM64 and relied
  on that implementation. Darwin's unsupported ownership adapter cannot supply
  `managedFunctionGraph`/host graph state. Copying the H6 bodies with Darwin tags
  would not qualify applicable behavior and would require a runtime port.
  This ownership graph is intentionally unsupported, not an unexplained Darwin
  skip. Applicable Darwin converted-publication rejection is covered above;
  no H6 ownership/wakeup pass is claimed.
- **TinyGo:** the 9180 `patch-tinygo-042.sh` invokes the hash-checked foreign-stack
  integration script as well as the scheduler fix. `tinygo_setup.py` prepares
  this exact patch on a separately provisioned native 0.42.0 toolchain, requires
  a separately reviewed native compiler SHA256, and records patched-file hashes.
  It is intentionally not wired into activation before artifact review. The
  historical Linux compiler hash is not a Darwin binary pin. Remaining work:
  provision/verify the Darwin compiler archive and complete patched runtime
  identity, review setup receipts, then execute evacuation, foreign-stack/joint
  and supported conversion selectors in all modes with exact target census.
  No TinyGo Darwin pass or runtime patch port is claimed.

## Local verification and ceiling

Run `PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/final-darwin/validate_local.py`.
New receipts include source/fixture identities; wrong identity/dirty harness
refusal; Linux refusal in all three modes for both native contract targets and
the existing driver; exact-census negative cases; 64 explicit special-summary
state combinations; the full tests/integration/cipolicy package (including
TestAggregateCIRequiresEveryWorkflowJob and the new isolation/cycle regression)
in all three modes; unchanged normal verifier contracts; planner tests and full-profile
verification in all three modes; actionlint 1.7.7; Python/Bash syntax.
`overlay-builds.json` records only new fixture cross-builds, not target execution.
The first local overlay attempt stopped at the untrusted mise shim; the second
stopped at an inherited GOROOT/version mismatch. Both attempts remain verbatim
in separate directories. The final build uses the absolute Go 1.27.1 binary
and matching GOROOT. None of these build receipts claim native execution.
Broad combined runtime tests are owned by the independent combined worker.

Three Darwin jobs retain 360 minutes each and three Windows jobs allow 30 minutes
each: **1,080 Mac + 90 Windows job-minutes per attempt**, plus the separate Ubuntu
summary job with its existing default timeout. These are hard job ceilings,
not estimates or timing measurements. Artifacts retain seven days. No monetary
or storage-cost guarantee is made. Reruns multiply these bounds. Evidence/opt-in
CI changes cannot affect shipped runtime performance; no performance claim or
READY TO MEASURE for production is made here.
