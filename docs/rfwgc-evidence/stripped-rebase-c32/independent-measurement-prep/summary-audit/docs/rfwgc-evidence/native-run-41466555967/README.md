# Native run 41466555967 evidence audit

[Run 41466555967](https://github.com/wago-org/wago/actions/runs/41466555967), attempt 1:
**Darwin debug and regalloc FAIL; Darwin ordinary CANCELLED with no artifact;
Windows admitted storage PASS in all three modes. No final approval.**

Source `08f511fac41a77f4c0f9046f88ebc78ad05a5da6`, harness
`041f7ed4c376e2a33ce21b22075cd617ddca6597`, activation review
`57c04aa09c53321e86a8358ae7fc53688af82b2b`. The reviewed revision is
`final-darwin/revision-08f511fac`. Historical `dispatch.json` is unchanged,
including its pending verdict. This audit does not alter dispatch state.

## First exact new failure and repair scope

The earliest fatal event in the available raw receipts is
**2026-11-02T15:19:06.344839Z**, `race-wago_regalloccheck`,
`TestGCARM64LiveStructDoesNotPrepareUnusedHandles`:

```
fatal error: checkptr: pointer arithmetic result points to invalid allocation
runtime.checkptrArithmetic(...)
...TestGCARM64LiveStructDoesNotPrepareUnusedHandles(...)
.../src/wago/gc_native_reservation_arm64_test.go:37
```

Debug repeats the same failure at **15:20:23.368405Z**. Each process exits 1
with one package failure event and a started test lacking a terminal event.
This is **a test inspection defect exposed by Go checkptr in the race lane**:
line 37 converts `view.StructAllocState`, a stored `uintptr`, to a Go pointer.
Pinned `native_view.go` originally populates that integer from the collector's
Go-owned allocation state. The failure identifies the conversion in the test;
it does not establish that the preceding guest invocation returned an incorrect
value or that the collector state was invalid. It is not a data-race report,
setup/build failure, or a swallowed workflow exit.

Minimal repair scope: replace the test's raw integer-to-pointer inspection with
a lifetime-correct inspection mechanism, retaining the unused-handle and exact
allocation assertions. Review the analogous large-array conversion at line 79
in the same file as part of that tight repair; it is a similar static risk, not
an independently observed failing test here. Do not globally disable checkptr,
skip the test, or count its unexecuted assertions as passing. No repair is made.

A separate **15:36:21.596742Z** fatal event occurs in
`diagnostics-established-wago_regalloccheck`,
`TestTwoIntegerResultRegisterCall`:

```
runtime: split stack overflow: 0x158fd7f60 < 0x5230ec5e0000
fatal error: runtime: split stack overflow
runtime.sigpanic()
.../arm64spike.enterNativeSpike(...)
.../src/core/runtime/arm64spike/spike_arm64.s:41
```

This is **an unresolved native execution failure in the backend test bridge**.
The recorded SP lies outside the Go stack while the trace enters `sigpanic`;
the bridge deliberately switches to an off-heap stack. The logs alone do not
identify the triggering signal, faulting generated instruction, ABI corruption,
or whether the defect is in generated code, the test bridge, or signal handling.
There is no register-checker mismatch message proving a register allocation bug.
The same test passed in this mode's default diagnostic lane and in the previous
run's established lane; that does not erase this failure or establish flakiness.
Minimal investigation scope is this test, its two-result generated call, and
`arm64spike.Call2`/`enterNativeSpike`. A production fix scope is **not yet known**.
No Go/toolchain defect or production runtime defect is established from this trace.

Original complete raw logs remain inside the hash-verified ZIPs. `failure-details/`
contains verbatim output for each crashing test and exact effective argv;
`first-failures.json` records chronological locations. The failed package's later
tests are not assumed to have executed. `unfinished-tests.json` records started
but unterminated tests. No failure follow-through or runtime/build rerun occurred.

Read-only sibling inspection before triage examined immutable HEADs in Slipstream
and Starshine (`sibling-audit.json`). No matching Wago bridge or raw-state test
implementation was found. This is not sibling correctness certification, and
there is no verified production finding to transfer to either sibling.

## Ordinary evidence is absent

Both the GitHub connector and direct REST records expose **five**, not six,
artifacts. All five were downloaded. Job
[125348879775](https://github.com/wago-org/wago/actions/runs/41466555967/job/125348879775)
is `cancelled`, has no steps, runner ID 0 and an empty runner name. There is no
`darwin-ownership-ordinary-1` artifact or ordinary job log in the complete run
log archive. `ordinary-job.json`, `jobs.json` and `artifacts.json` preserve these
records. The requested ordinary-success premise cannot be substantiated.
Cancellation actor and cause are unknown. No rerun or dispatch was requested.

The complete available job-log ZIP contains six aggregate logs and their step
logs: two Darwin, three Windows, and the failed non-Linux summary. It is retained
unchanged and expanded in `job-logs/`. There are 24 intentionally skipped jobs,
three successful Windows jobs, three failures including the summary, and one
cancelled Darwin job. Full CI, Linux ARM64 and TinyGo were outside this dispatch.

## Positive closure and native gate verdicts

The previous `native-run-41447853354` audit at `c25943944` remains unchanged.
Its stale multi-table failure and its aggregate failures remain historical FAILs.
Commit 08f itself changes only the test predicate plus repair evidence; the full
7e41-to-08f history also contains wrapper/test/CI changes, enumerated separately
in `pinned-source-audit.json`. New observations are not attributed causally to the
one-line predicate change. The reservation test and bridge files are unchanged
from 7e41. The prior race receipts have no events for the reservation test;
the prior established diagnostic receipts pass both implicated tests.

**Positive multi-table follow-through is verified in debug and regalloc only.**
Each has six exact pass events: focus, repaired-contracts, full-root replay, race
(before its later crash), default diagnostics and established diagnostics.
The pinned source and selected host adapter establish that the Wasm externref
table selects the positive branch, requiring sum **545**, host **hello**, and a
nonzero full-collection count. A PASS would be impossible if one of those
assertions failed. No overlay replaces this test. Ordinary has no evidence, so
all-three-mode closure is **OPEN**. These are assertion-based proofs, not printed
value receipts. `closure-events.json` records all 12 passes.

| Gate | Debug | Regalloc | Ordinary |
| --- | --- | --- | --- |
| Darwin focus | 73 top-level PASS, includes 22 RR | Same | NO RECEIPT |
| make(42), initializer(55), eight migrated positive children | PASS | PASS | NO RECEIPT |
| Candidate noGC/scalar/graph false and true | Six leaves PASS | Six leaves PASS | NO RECEIPT |
| Matched baseline noGC/scalar/graph false | Three leaves PASS | Three leaves PASS | NO RECEIPT |
| Cancellation | PASS, exit/census 0/0 | PASS, exit/census 0/0 | NO RECEIPT |
| Core 3 isolated child | Protocol 2, 10 modules/18 assertions, zero gaps | Same | NO RECEIPT |
| Broad Core 3 | Parent + 103 fixtures PASS, enforced 215/690 | Same | NO RECEIPT |
| Spec v1 and v2 | PASS | PASS | NO RECEIPT |
| Spec v3 explicit and signal routes | Each 2,226 modules/58,238 assertions, zero gaps | Same | NO RECEIPT |
| Installer | 10 pass events | 10 pass events | NO RECEIPT |
| Focused backend codegen | 53 pass events | 53 pass events | NO RECEIPT |
| Default diagnostic codegen gates | 62 pass events | 62 pass events | NO RECEIPT |
| Four public native variant gates | 12 pass events | 12 pass events | NO RECEIPT |
| Synthetic worker comparisons | Six leaf passes | Six leaf passes | NO RECEIPT |
| Windows storage/admission | Six top-level PASS | Six top-level PASS | Six top-level PASS |

RR tests use the declared build-constraint overlays. They are not unmodified
Linux-only tests executing on Darwin. Windows proves admitted storage/rejection
contracts, not converted-GC JIT support. Baseline
`a3c08365f531eb3c9003e3acf3e0a822c12c31f4` uses identical Go graph fixture and three
Wasm hashes; it has no nursery/true counterpart. Graphs are correctness evidence,
not measurements. Broad Core 3 totals are proved by the passing unchanged parent
assertion; successful child output is not separately printed by that parent.

## Per-suite native verdicts

Cells are status; passing test events / failure events / skip events;
process exit / package-census exit. Counts include subtests and replay, not unique
workloads. Fatal failures produce package fail events without test fail events.
All 63 original broad plans are unchanged; 42 ran in the two available modes,
21 ordinary plans have no receipts. Six additional discriminators yield 48
recorded Darwin suite commands; candidate/baseline/Windows add seven commands.

| Suite | Debug | Regalloc | Ordinary |
| --- | --- | --- | --- |
| cancellation | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | NO RECEIPT |
| core3-child | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | NO RECEIPT |
| repaired-contracts | PASS; 10/0/0; 0/0 | PASS; 10/0/0; 0/0 | NO RECEIPT |
| full-root-replay | PASS_WITH_SKIPS; 27687/0/336; 0/0 | PASS_WITH_SKIPS; 33637/0/327; 0/0 | NO RECEIPT |
| collector | PASS_WITH_SKIPS; 2292/0/3; 0/0 | PASS; 2210/0/0; 0/0 | NO RECEIPT |
| race | FAIL; 6138/1/22; 1/0 | FAIL; 6056/1/19; 1/0 | NO RECEIPT |
| guard | PASS_WITH_SKIPS; 9368/0/35; 0/0 | PASS_WITH_SKIPS; 9368/0/35; 0/0 | NO RECEIPT |
| backend | PASS_WITH_SKIPS; 9310/0/225; 0/0 | PASS_WITH_SKIPS; 9989/0/225; 0/0 | NO RECEIPT |
| cli | PASS_WITH_SKIPS; 999/0/41; 0/0 | PASS_WITH_SKIPS; 999/0/41; 0/0 | NO RECEIPT |
| installer | PASS; 10/0/0; 0/0 | PASS; 10/0/0; 0/0 | NO RECEIPT |
| corpus | PASS_WITH_SKIPS; 880/0/12; 0/0 | PASS_WITH_SKIPS; 880/0/12; 0/0 | NO RECEIPT |
| guard-corpus | PASS_WITH_SKIPS; 628/0/1; 0/0 | PASS_WITH_SKIPS; 628/0/1; 0/0 | NO RECEIPT |
| host-transition | PASS; 48/0/0; 0/0 | PASS; 48/0/0; 0/0 | NO RECEIPT |
| hardening | PASS_WITH_SKIPS; 6876/0/9; 0/0 | PASS; 6630/0/0; 0/0 | NO RECEIPT |
| profile-runtime | PASS_WITH_SKIPS; 62/0/3; 0/0 | PASS_WITH_SKIPS; 62/0/3; 0/0 | NO RECEIPT |
| profile-metadata | PASS; 157/0/0; 0/0 | PASS; 157/0/0; 0/0 | NO RECEIPT |
| diagnostics-default | PASS_WITH_SKIPS; 18388/0/44; 0/0 | PASS_WITH_SKIPS; 19076/0/38; 0/0 | NO RECEIPT |
| diagnostics-established | PASS_WITH_SKIPS; 18388/0/44; 0/0 | FAIL; 9636/1/33; 1/0 | NO RECEIPT |
| shared-scalar | PASS; 83/0/0; 0/0 | PASS; 85/0/0; 0/0 | NO RECEIPT |
| seeded-concurrency | PASS; 24/0/0; 0/0 | PASS; 24/0/0; 0/0 | NO RECEIPT |
| spec-v1 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | NO RECEIPT |
| spec-v2 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | NO RECEIPT |
| spec-v3 | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | NO RECEIPT |
| spec-v3-signals | PASS; 1/0/0; 0/0 | PASS; 1/0/0; 0/0 | NO RECEIPT |

Skips are retained individually with raw reasons in `skip-accounting.json` and
platform catalog receipts. They include architecture/build-tag exclusions,
optional JSON fixture absence, reference-only corpus checks, mutable-context
handoff exclusions, profile tests requiring `GODEBUG=panicnil=1`, and no-test-file
package events. A passing process with skips is not complete passing coverage.
Passing targeted codegen subsets do not qualify the failing established suite.

## Provenance, cost and preservation

`verify.py` independently checks ZIP sizes and GitHub SHA256 digests; native
Go 1.27.1 Darwin/ARM64 and Windows/AMD64 receipts; exact source/driver identities;
21,322 source and 21,421 harness Git blob hashes plus baseline blobs; test and
fixture hashes; before/after integrity; exact focus and graph terminal censuses;
every available broad result, process exit and package-census exit. `gates.py`
checks effective argv, all 63 unchanged plans, package errors, gates and skips.
`audit.py` additionally checks focused/baseline package parsing, closure events,
source excerpts, prior event comparisons, missing ordinary evidence and siblings.
All offline checks PASS for **available evidence**, not native qualification.

The 3,614 candidate submodule file hashes agree across the two available Darwin
receipts but are not independently attested against nested Git objects. Runner
infrastructure and tool binaries are supported by receipts, not independent
machine attestation. GitHub supplies no digest for the full log ZIP here; its
local digest and all archive member hashes are recorded. Known credential
pattern scans find no matches; this is not proof against unknown formats.

Historical GitHub timestamps give a **42m35s** run window (14:55:27–15:38:02 UTC).
Completed Darwin debug and regalloc job windows are 32m48s and 41m58s. The
cancelled ordinary record's 15m01s interval is not runner execution time. The
reviewed ceiling is 1,173 job-minutes, not measured or billed usage. Repair time,
queue/revalidation time, and monetary cost are **unknown**. `cost.json` contains
the original timestamp accounting; no benchmark or new timing was taken.

Outstanding: repair and validate the test inspection defect; diagnose the native
bridge crash; obtain missing ordinary evidence; revalidate affected suites in
all three modes under the owning task's authorization. No deadline estimate or
final qualification approval follows from this audit.

Offline verification (no candidate build or execution):

```
PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/native-run-41466555967/verify.py
PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/native-run-41466555967/gates.py
PYTHONDONTWRITEBYTECODE=1 python3 docs/rfwgc-evidence/native-run-41466555967/audit.py
```

Only this owned directory is committed with `git commit --only`; dispatch bytes,
unrelated dirt, index entries and gitlinks are preserved. No source/workflow
changes, push, dispatch, PR, build/runtime rerun, benchmark, subagent or Claude
review. Evidence-only changes cannot affect shipped runtime performance, so no
measurement lease is required. The handoff supplies the evidence head commit.
