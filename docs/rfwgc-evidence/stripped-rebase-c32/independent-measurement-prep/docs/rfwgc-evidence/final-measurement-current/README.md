# Current-candidate measurement preparation

**PREPARATION ONLY. Candidate NOT qualified by builds/checks alone. No acquisition authorization.**

The first unresolved external risk is the parent-owned ARM64/QEMU execution failure.
Native Darwin and target-specific TinyGo audits remain with the parent. Performance
cost and benefit are unknown: no benchmark, calibration, timing sample, lease,
flock, host reservation, dispatch, PR, push, or subagent is part of this work.
Correctness watchdogs and testing framework duration text are not performance data.

Candidate: `041f7ed4c376e2a33ce21b22075cd617ddca6597`.
Baseline: `a3c08365f531eb3c9003e3acf3e0a822c12c31f4`, from the original protocol.
Approval: `737528b2505ac1f007519690de1ac36ae1dedb1f`.
`source-relation.json` checks the candidate against 08f511fac and 5b02ec67:
production bytes are unchanged; test/CI/documentation changes are listed explicitly.
This does not assert identical executables or waive execution qualification.

The original `../final-measurement/README.md`, `CONTINUATION.md`, raw failures,
invalid candidate seal and original 316-cell plan remain historical evidence.
`original-evidence-manifest.json` and its final integrity check preserve their bytes.
`exports-incomplete-snapshot.json` retains an early audit that incorrectly treated
the two declared baseline stage3 overlay additions as drift; the checker now
recognizes those declared additions as well as tracked-file replacements.
`plan.json` repins the candidate and expands exactly the parent's approved 33 controls:
six non-GC extern-store cycles, eighteen domain controls, and nine large-old-heap
controls. No other measurement cells are added. The total is 349 cells / 2,094
accepted blocks / 8,376 accepted samples, maximum 25,128 attempted samples under
the three-attempt rule. The allowance remains two separately granted four-hour
windows; neither command counts nor build completion predict acquisition cost.

All **698 cell sides** have nonempty matching all-three-mode correctness bindings;
there are no unbound sides or paired-leaf mismatches. The retained command ledger
contains 2,901 commands: 2,853 passing correctness checks, 45 successful builds,
and three superseded guest harness build failures. The 36 patched-TinyGo checks,
12 DR probe checks, 30 detailed census checks and six size checks are included.
There are 321 diagnostic snapshots and 51 old-heap fixed-layout byte receipts.
Fresh corpus outcomes cover 1,536 ID/revision/mode/kind rows; `fresh-input-census.json`
checks all 47 inputs on both pins. `census.json` is the inherited identity ledger;
its original NOT_RUN labels are superseded only by the separate fresh result rows.
All eight exports pass the final 59,702-blob integrity audit, and 724 paired
workload/input paths match byte-for-byte. All known preparation children are reaped;
this is not global drain.

`summary.json` is the result index. `seal-manifest.json` binds the exact local
executables, tools, source/input exports, overlays, expanded future argv/env/cwd,
all-three-mode qualification and leaf census. Its status is PREPARATION_ONLY,
`candidate_valid=false`, and `acquisition_authorized=false`. Even completely
bound cells cannot make this an acquisition-valid seal. Missing/failed bindings
remain explicit. The expanded benchmark argv are data only; no executor is supplied
or invoked here. Binaries and exports remain local in `.prepared/`, excluded from
Git but hash-bound by the committed manifest.

Fresh exports use `git archive` of immutable pins, excluding documentation; dirty
production files, dirty corpus scripts and worktree gitlinks are never copied.
Paired workload overlays are identical. The archived corpus and its adapter are
compared byte-for-byte across baseline/candidate. The canonical corpus checked
compile precedes runtime execution. The unsafe baseline no-inline/lifetime-analysis
clang-cpp configuration is never executed. The complete 128-ID denominator and
all artifact/input references remain; external captured reference outputs are not
fresh execution. Compile+Close, invocation and full-process lifecycle metrics
retain the original protocol's separate meanings.

`qualification/` contains all expanded build/check commands, supplied environments,
raw logs, nonempty Test leaf gates and their corresponding benchmark names.
Benchmark functions are never invoked to discover leaves. Native modes are ordinary,
wagodebug and wago_regalloccheck. The actual patched TinyGo 0.42 tool uses
threads/conservative, including idle mapping dimensions, active stack selectors and
serial evacuation shift 6/12 Off/On in all modes. `tool-manifest.json` pins the entire
Go 1.27.1 and patched TinyGo trees, including foreign-stack registration and scanning;
`tinygo-tool-relation.json` compares TinyGo with the prior exact tool-tree seal.
Unsafe pre-registration scanner pairs and unsupported TinyGo host receipt tests are
not substituted. These local adapter checks do not replace the parent's native audits.

`gates/` reruns the preserved DR-1/DR-2 regression selectors without rewriting their
historical failures. The initial candidate guest builds collided with tests already
present in the repaired source. `adapter-repair.json` records the identical
overlay-entrypoint rename on both pins, all three raw failed builds and the
replacement 210 guest build/check commands; probe bodies were unchanged.
Domain overlap callbacks release invocation gates and therefore
cannot alone prove guest invocation independence. Non-GC extern operations use a
non-null host token and 256 scalar stores or bulk elements. Domain n=1 and n=2 are
not normalized as equal work; the same/separate Runtime comparison has an explicit
leaf mapping for that approved axis.

`detailed-census/` and `participant-census.json` retain actual joined phase counts,
allocation charges, space extents, promotion/copy/evacuation bytes, region extents,
credit, free runs and backing capacity. Fresh worker-8 graph checks witnessed two trace/sweep participants. Evacuation On
witnessed eight trace/copy and two sweep participants; Off witnessed two
trace/sweep participants. These are below-requested graph/sweep controls, not
eight-worker graph scaling. Requested 1/2/4/8 workers are not evidence
of that many active participants. `ParallelActivity` arrays count phases by joined
participant count, not worker identities. Below-requested participation and
object-edge-source serial tracing remain explicit. End-of-cycle backing and live
object extents are not peak RSS or fragmentation; extents include object headers.
The old-heap workload retains 8,192 8-KiB payloads then allocates 8,192 scalars
against 64-KiB Eden, with setup/checks/cleanup in whole-cycle scope.

`remaining-requirements.json` enumerates the outstanding review, execution, seal,
lease and host attestations. Global owner drain must cover all projects, namespaces,
containers and remote runners, block new starts, and be repeated after final builds.
Both canonical and legacy owner locks require reconciliation and separate measurement
authorization. Qualified temperature and throttling visibility is mandatory; frequency
variation is not a throttle oracle. No local process view or released record can
supply these owner attestations. The executor still needs independent review and
live telemetry qualification without benchmarks. This report cannot state
READY TO ACQUIRE.

Reproduction (fresh output required; preserve every existing raw attempt):

1. `python3 qualify.py` exports/builds and runs only Test selectors.
2. `python3 qualify-gates.py`, `python3 qualify-census.py`, and
   `python3 qualify-shapes.py` complete additional correctness and diagnostic gates.
3. `python3 summarize-census.py`, `python3 check-exports.py`, and
   `python3 record-provenance.py tools` / `original` audit identities.
4. `python3 seal.py` records the non-acquirable preparation manifest.

The added documentation, orchestration and test-only overlays do not enter shipped
builds and cannot affect production performance. They do define future measurement
workloads, whose checks and setup costs must remain labeled. No new runtime finding
is triaged here. Only this directory is owned; the shared port log is untouched.
