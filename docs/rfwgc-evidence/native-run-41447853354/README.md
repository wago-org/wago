# Native run 41447853354: Darwin FAIL; Windows admitted storage PASS

Run: https://github.com/wago-org/wago/actions/runs/41447853354 (attempt 1, completed).
Source **7e41fafbbcd737ed1edce11b94b43b4eda134dcf**; harness
**5fb47abb4b30472bd74751a5ea96b51d668e488a**. `dispatch.json` is preserved byte-for-byte,
including its historical pending status. No results from later 5b02/ca164 sources
or other runs are credited. Four jobs failed (three Darwin plus summary), three
Windows jobs passed, and 22 jobs were skipped. This is not full CI qualification.

## First exact defect and minimal repair scope

All three modes independently show the same residual test defect:
`src/wago/gc_extern_multi_table_test.go:39-46` selects the old unsupported-adapter
expectation using `runtime.GOOS == "darwin"`. At this source, the build constraint
on `gc_conversion_store_host.go` includes Darwin ARM64; `gc_conversion_store_other.go`
excludes it. Raw package censuses confirm selection of the host implementation
and this test. `store` returns nil, causing `store converted rejection: <nil>`.
The broad overlay does not replace this file. This is a stale platform expectation,
not evidence that the supported publication itself is erroneous.

Reproduction recipe, **not executed by this audit**: on native Darwin ARM64 at
exact source 7e41 with the recorded tools, run
`go test -count=1 -run '^TestGCExternConversionAcrossReferenceTables$' ./src/wago`,
then the equivalent `-tags=wagodebug` and `-tags=wago_regalloccheck` commands.
The exact archived command, environment, overlay and package census for each
observed failure are retained in the ZIPs and command receipts. The failure is
observed separately in repaired-contracts, root replay, race, default diagnostics,
and established diagnostics in each mode: **15 failed commands**, each with one
test failure and one package failure. All 72 command results were examined; this
conclusion is not extrapolated from a single log.

Minimal repair scope is the test's unsupported-target predicate, aligned with the
actual supported adapter, retaining its positive sum=545, host-value and collection
assertions. Those assertions occur after the fatal rejection branch and were **not
executed by this failing test**; no closure is claimed for them. No production or
workflow repair is made here, and no unsafe failure follow-through is attempted.
The existing focused positive ownership gates pass independently. No new production
defect was verified. Read-only sibling snapshots of Slipstream at
`dcb45b56386327a1f71fb039ed02e13562e1e2d7` and Starshine at
`b28144a7c50f1dc360825f7f68220022101b9921` retain relevant host/reference code and
search results in `sibling-audit.json`; no matching Wago implementation was found.
This scoped audit is not a general sibling correctness certification.

Deadline cost: the archived run was created at 12:49:40Z and last updated at
13:32:01Z on November 2 (42m21s wall-clock run window, not a benchmark). Repair
and fresh all-three-mode native validation remain necessary before the November 4
deadline. No repair or future queue/run duration estimate is supported; this audit
neither dispatches nor reruns anything.

## Exact successful native gates

Each row applies independently to ordinary, wagodebug and wago_regalloccheck.

| Gate | Native result in each mode |
| --- | --- |
| Darwin focused ownership | 72 exact top-level passes, no failures/skips; includes 22 RR/admission tests (not 72+22 distinct tests) |
| make(42) / initializer(55) | Both exact named positive gates PASS |
| Migrated unsupported tests | Eight exact mapped positive child passes |
| Candidate graphs | Six exact noGC/scalar/graph false+true leaves PASS |
| Matched baseline graphs | Three exact false leaves PASS at a3c08365f531eb3c9003e3acf3e0a822c12c31f4; identical Go fixture and three Wasm hashes |
| Windows storage/admission | Six exact top-level contracts PASS, no failures/skips |
| Cancellation | Exact TestSharedEntryGuardCancellationAndGC PASS, exit/census 0/0, no skip |
| Core 3 isolated child | Protocol 2, exact fixture and nonce, 10 modules / 18 assertions; zero failures/skips |
| Broad Core 3 | Parent and 103 fixture subtests PASS; unchanged enforced 215 modules / 690 assertions |
| Official spec-v3 explicit and signal routes | Each 2,226 modules / 58,238 assertions, zero failures/skips/gaps, REQUIRE_ZERO_GAPS=1 |
| Installer | 10 test passes, exit/census 0/0, no skips |
| Focused backend codegen gates | 53 pass events, no failures/skips |
| Codegen diagnostic repair gates | 62 pass events in default diagnostic lane, no failures/skips |
| Four public native variant gates | 12 pass events, no failures/skips |
| Synthetic worker comparisons | Six synthetic leaf passes across both worker tests |

The 22 RR tests use declared build-constraint test overlays; these are not claims
that Linux-gated source tests ran unmodified on Darwin. Windows has no Darwin/RR
overlays and proves storage/admission only, not Windows converted GC JIT support.
Baseline has no true/nursery counterpart. Graph tests are bounded correctness
checks, not performance evidence. The Core 3 parent validates each child's
identity and exact fixture counts and enforces total 215/690 in pinned source;
successful broad child output is not echoed, so the total proof is the passing
unchanged assertion rather than a separate printed aggregate marker.

Relative to run 41431926958, the Core 3 child (previous 9/17), broad Core 3,
spec-v3, installer dependency setup, positive converted-global gate and targeted
backend/public/diagnostic repairs now have native passing evidence at 7e41.
The stale converted-table rejection is the remaining observed failure. Passing
subsets do not turn their failing aggregate commands into passes.

## Per-command native verdict

Cells below are **status; passing test events / failure events / skip events;
process exit / package-census exit**. Failures include package events; passes
include subtests and replayed tests and are not unique workload counts. All 63
original broad plans are identical to the prior harness, plus nine narrow
commands. Nine focused commands (candidate/baseline/Windows) bring the total to
81 go-test commands, each paired with a package census.

| Suite | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| cancellation | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 |
| core3-child | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 |
| repaired-contracts | FAIL; 9/2/0; 1/0 | FAIL; 9/2/0; 1/0 | FAIL; 9/2/0; 1/0 |
| full-root-replay | FAIL; 27613/2/327; 1/0 | FAIL; 27686/2/336; 1/0 | FAIL; 33636/2/327; 1/0 |
| collector | PASS; 2210/0/0; 0/0 | PASS_WITH_SKIPS; 2292/0/3; 0/0 | PASS; 2210/0/0; 0/0 |
| race | FAIL; 6055/2/19; 1/0 | FAIL; 6137/2/22; 1/0 | FAIL; 6055/2/19; 1/0 |
| guard | PASS_WITH_SKIPS; 9368/0/35; 0/0 | PASS_WITH_SKIPS; 9368/0/35; 0/0 | PASS_WITH_SKIPS; 9368/0/35; 0/0 |
| backend | PASS_WITH_SKIPS; 9310/0/225; 0/0 | PASS_WITH_SKIPS; 9310/0/225; 0/0 | PASS_WITH_SKIPS; 9989/0/225; 0/0 |
| cli | PASS_WITH_SKIPS; 999/0/41; 0/0 | PASS_WITH_SKIPS; 999/0/41; 0/0 | PASS_WITH_SKIPS; 999/0/41; 0/0 |
| installer | PASS; 10/0/0; 0/0 | PASS; 10/0/0; 0/0 | PASS; 10/0/0; 0/0 |
| corpus | PASS_WITH_SKIPS; 880/0/12; 0/0 | PASS_WITH_SKIPS; 880/0/12; 0/0 | PASS_WITH_SKIPS; 880/0/12; 0/0 |
| guard-corpus | PASS_WITH_SKIPS; 628/0/1; 0/0 | PASS_WITH_SKIPS; 628/0/1; 0/0 | PASS_WITH_SKIPS; 628/0/1; 0/0 |
| host-transition | PASS; 48/0/0; 0/0 | PASS; 48/0/0; 0/0 | PASS; 48/0/0; 0/0 |
| hardening | PASS; 6630/0/0; 0/0 | PASS_WITH_SKIPS; 6876/0/9; 0/0 | PASS; 6630/0/0; 0/0 |
| profile-runtime | PASS_WITH_SKIPS; 62/0/3; 0/0 | PASS_WITH_SKIPS; 62/0/3; 0/0 | PASS_WITH_SKIPS; 62/0/3; 0/0 |
| profile-metadata | PASS; 157/0/0; 0/0 | PASS; 157/0/0; 0/0 | PASS; 157/0/0; 0/0 |
| diagnostics-default | FAIL; 18396/2/38; 1/0 | FAIL; 18387/2/44; 1/0 | FAIL; 19075/2/38; 1/0 |
| diagnostics-established | FAIL; 18396/2/38; 1/0 | FAIL; 18387/2/44; 1/0 | FAIL; 19075/2/38; 1/0 |
| shared-scalar | PASS; 83/0/0; 0/0 | PASS; 83/0/0; 0/0 | PASS; 85/0/0; 0/0 |
| seeded-concurrency | PASS; 24/0/0; 0/0 | PASS; 24/0/0; 0/0 | PASS; 24/0/0; 0/0 |
| spec-v1 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 |
| spec-v2 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 |
| spec-v3 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 |
| spec-v3-signals | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 |

## Remaining gates and evidence limits

- Darwin aggregate qualification remains **FAIL** until the residual test is
  repaired and validated. The failed test's positive follow-through remains untested.
- Native patched TinyGo is **OPEN / outside this dispatch**. Later runs do not
  close it for this source in this report.
- Full CI, other architectures and general Windows JIT qualification are **NOT RUN**.
- Skip census remains explicit, not counted as passing coverage. Examples include
  12 corpus reference-only skips using pinned V8/Wasmtime oracles; missing optional
  `json-as.wasm`; three profile tests requiring `GODEBUG=panicnil=1`; mutable-context
  handoff exclusions; architecture-specific tests; disabled profiling/diagnostic
  configurations; and package-level no-test-file events. Compiler diagnostic tests
  skipped in untagged backend lanes have separate tagged evidence where listed.
  `skip-accounting.json` preserves every skip with its raw reason by lane and mode;
  catalog platform exclusions remain in `broad/*/catalog-platform-census.json`.
  This is not blanket approval of all skipped coverage.

Six original compressed artifact archives are retained unmodified; each raw ZIP
size and SHA256 matches GitHub's metadata digest. The complete run log ZIP retains
seven aggregate job logs and their step logs. GitHub provides no digest for that
log ZIP here: its local SHA256 and member hashes are recorded, not independently
server-attested. `raw-member-hashes.json` records all raw archive members.

Offline `verify.py` checks all **18,798 source blobs, 19,383 harness blobs**, and
baseline blobs against immutable Git objects, source and driver identity, native
Go 1.27.1/OS/architecture, fixture and script hashes, exact focused commands,
overlays, before/after manifests, full raw terminal census and every broad result.
The 3,614 candidate submodule file hashes agree across all three modes but are not
independently checked against nested Git objects. Runner infrastructure and tools
are supported by provenance receipts, not independent machine attestation.
`gates.py` verifies effective argv, package-census parsing and absence of package
errors, all original 63 plans, codegen gates, zero-gap spec totals and exact failures.
`audit.py` records pinned source excerpts, closure events, siblings and a known-secret
pattern scan (no matches; not proof against unknown secret formats).

Reproduce the offline checks from the worktree:

```
PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/native-run-41447853354/verify.py
PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/native-run-41447853354/gates.py
```

Only this owned evidence directory is committed. Historical evidence, dispatch,
dirty worker files, index entries and gitlinks are not edited by this audit; other
workers may concurrently update their own files. No build, runtime rerun, benchmark,
push, dispatch, PR, workflow edit, Claude review or subagent was used. Evidence-only
files cannot affect shipped runtime performance, so no measurement lease is needed.
The handoff identifies the evidence head commit.
