# RFWGC port log

## 2026-10-30 — initial mapping checkpoint

**Scope:** documentation only; no engine changes or runtime claims. Deliverables are `docs/rfwgc-port-plan.md` and this log. The plan contains the ownership map, source symbols, portability decisions, initial implementation contract and validation handoff.

### Identity and pre-existing state

- Worktree: `/home/dev/.t3/worktrees/wago/experiment-rfwgc-port`.
- Branch verified: `experiment/rfwgc-port`.
- Wago HEAD verified: `9713878ab452848f7eb7549a90be94b938a898be`.
- Parent reports initial `fc6af651c` was fast-forwarded to requested `9713878ab`; no rebase. Mapping began after that transition.
- Pre-existing dirty submodules: `tests/conformance/spec-v1`, `tests/conformance/spec-v2`. No submodule contents, indexes, or revisions changed by this worker.
- Source repository: `/home/dev/Projects/slipstream`. Fetched `origin/master` verified as `db6014de2316f13dd08ea8618d614c28132f2688`. Used ref-qualified `git show`, `git grep`, `ls-tree` and history for the study; no source checkout edits or branch changes. No fetch was needed in this cycle.
- Applicable instructions: user AGENTS block and task constraints; no local/ancestor filesystem AGENTS.md found. Read Slipstream root AGENTS.md at the pinned ref. Its bounded streaming and conservative measurement requirements are reflected in the plan.

### Findings and decisions

1. **Wago ownership verified:** native package is the runtime's collector (`src/wago/instance.go`, `instantiate.go`, `reference_store.go`), with Go-backed nursery/survivor/throughput/Tiny byte storage (`gc/native/collector.go`, `layout.go`, `throughput_alloc.go`, `tiny_alloc.go`). Native JIT allocation uses that backing, stable handles and versioned views; it is not Slipstream's ABI2 mapped-offset heap. The checked Go API is an ownership/generation wrapper over the same raw collector. Keep both host backing lifetime and exact guest roots explicit.
2. **Existing optimization substrate verified:** `amd64/gc_struct.go` already forwards scalar constructor reads and selected expressions; `gc_dead_new.go` and `gc_dead_group.go` preserve checked reservation effects. Adapt bounded lifetime proof across locals to this substrate; do not present adjacent scalar forwarding as new work or remove allocation effects without a separate contract.
3. **Smallest implementation chosen:** Off/Analyze, one pointer-free scalar struct in a closed straight-line body, bounded provenance and complete fallback reasons, no emitted-code changes. A later explicitly enabled consumer can extend checked lowering beyond adjacent patterns. No collector replacement, stack regions, module-summary system or parallel collector is needed first.
4. **Portability:** carry proof/fallback/replay/ownership invariants. Redesign storage and budgets in Go and Wago compiler scratch; do not transplant Goose capabilities, mapped offsets, fixed partitions, native assembly or facts family masks.
5. **Source results:** owner-provided 97.55% elimination and 98.10% nursery capture are recorded as Slipstream results, not Wago predictions. No independent denominator recomputation or performance measurement was performed.

### Mirror assessment at db6014de2

| Change / pending item | Initial disposition | Evidence |
| --- | --- | --- |
| Region-first default, historical `b3e802899` checkpoint | Keep Wago opt-in; preserve baseline oracle | Source history and scalar Off/Analyze/On routes in `src/jit_parallel.goose` |
| B1 `861c6e519` | Adopt necessary-condition gating; adapt to Wago's validated feature metadata | `src/facts_scalar.goose`: `scalar_cheap_eligible`; `src/instructions.goose`; Wago `gc_product_analysis.go` |
| B7 `8eef46cd5` | Adopt consumer-driven work admission from first implementation | `src/facts_schema.goose`, `src/facts_summary.goose`, `src/jit.goose`, `docs/perf-b7-consumer-facts.md` |
| Canonical type metadata growth `88c8c8563` | Assess equivalent tests in later cycle; no fix in this lane | Source Store growth history; Wago `gc/native/canonical_growth_regression_test.go`, `addtypes_retention_test.go` |
| Host boundary `5a5bee94c`, qualifications through `db6014de2` | Landed source fix; defer ABI-specific transplant | `gc/native.goose` new mutator owner/depth/waiter state; `native/gc_linux_x86_64.S`; `native/jit_linux_x86_64.S`: direct entry and thunk inflation |
| Old-space ceiling after 18,115 collections | Pending owner report; no reproduction or established landed fix; avoid importing fixed-window assumptions | `gc/native.goose`: `ng_admit` bounded old block scan/run cap. Exact symptom cause remains unverified |
| Inactive helper wake/completion cost | Still present; future parallel-GC design warning, not first-checkpoint blocker | `ng_worker` acknowledges inactive workers; collection dispatch and `ng_sweep` broadcast/wait for `N_HELPERS`. Host boundary change does not fix this |

**Mirror cursor:** `db6014de2316f13dd08ea8618d614c28132f2688`.
Each next cycle must inspect every newly landed collector/lifetime/host-boundary change since this cursor and append its adopt/adapt/defer/not-applicable decision with paths before advancing the cursor. Record no-change cycles. A source fix does not automatically authorize a Wago bug fix; sensitive engine defects go to Daybreak Blue.

### Verification, checkpoint and handoff

- Read source and named test contracts only; no build, benchmark, conformance run or runtime test. No build artifacts were generated, so build cleanup was unnecessary.
- Checked referenced key symbols/files and reviewed whitespace with `git diff --check`; final staged-path/whitespace check is required before commit.
- Exact commit paths: `docs/rfwgc-port-plan.md`, `docs/rfwgc-port-log.md`. Commit SHA is reported in the worker handoff, not embedded here to avoid a self-referential hash.
- No PR, push, upstream publication, rebase, subdelegation, or source-code mutation. Upstream backlog cap unchanged.
- No sensitive engine defect was independently established. Pending Slipstream issues above are explicitly reported rather than fixed.
- **Blockers:** none for the proposed Analyze checkpoint. Exact source metric reproduction, source old-space root cause, runtime region design and Wago performance qualification remain unverified future work.
- **Next worker:** ordinary sol implementation, following the precise file/strategy/test steps in the plan; commit completed coherent checkpoints using exact paths, remove artifacts after every build, preserve pre-existing dirt, and report SHA/blockers.

## 2026-10-30 — bounded Analyze implementation checkpoint

**Identity:** branch `experiment/rfwgc-port`, starting HEAD `f83004ebc` (mapping), engine base `9713878ab`. Pre-existing dirty `tests/conformance/spec-v1` and `spec-v2` untouched. No PR, push, rebase, subdelegation or upstream backlog change.

**Mirror:** read-only `git -C /home/dev/Projects/slipstream fetch origin master`; old and new `origin/master` both `db6014de2316f13dd08ea8618d614c28132f2688`. `git log db6014de2..origin/master` is empty. No newly landed relevant collector/compiler-lifetime/host-boundary changes; all initial mirror dispositions remain unchanged, including pending old-space ceiling and inactive-helper wake/join work. Source checkout untouched. Cursor remains db6014de2. B1 negative gating and B7 consumer-scoped local work remain adopted; no source allocation percentages transfer to Wago.

**Final scope:** analysis-only, off-by-default experimental `gc-lifetime-analyze` option in the existing catalog/bindings/config schema. A single serial bounded pre-worker pass publishes deterministic per-function source-level proof/fallback diagnostics to an explicit backend sink and, in diagnostic builds, ModuleStats/explain. This serial ownership is a deliberate adaptation of the proposed worker integration: fixed aggregate work/report caps rather than worker-owned persistent plans; no codegen retry state or consumer. Results use only owned value records. No collector/native allocator/lowering changes and no runtime elimination claim. See the implemented-slice section of the plan for exact limits, opt-in syntax, coverage and rejection semantics.

**Tests and evidence:** retained command/output logs in `docs/rfwgc-evidence/`. Go 1.27.1, Linux AMD64. Commands are documented in that directory's README.

- Full default AMD64 backend + optimization catalog + CLI settings/flag packages pass after regenerating `schema.json` with `go generate ./cli/internal/settings`. The initial settings run correctly failed the stale-schema assertion before regeneration.
- Focused Analyze tests pass in ordinary and diagnostics builds; race-enabled diagnostics tests pass. Off/Analyze machine bytes and complete frame-root plans agree with matching choices at 1/4/8 workers for qualifying aliases, rejected control flow and no-GC controls. Report order/content also agree. Tests cover adjacent control, aliases/tees, overwrites, all four scalar field types, default constructors, parameter provenance/write rejection, reference layouts/results, unknown/call/store/branch/identity/array opcodes, malformed prefixes/end/immediates, budget failures/exact instruction/local boundaries, unknown local objects and body fingerprint changes.
- Focused runtime GC-constructor and optimization-selection tests pass (existing runtime tests; no new runtime behavior).
- Broad `-tags=wago_codegenstats` AMD64 suite is **not green**: 24 top-level failures, all reproduced on an isolated unmodified `9713878ab` export, which additionally failed `TestSharedEstablishedPathPairs`. Full outputs retained; no fixes attempted. These are counter/admission assertions, not a newly established sensitive engine defect. Report to owner/Daybreak Blue for any engine investigation, not this lane.
- The self-contained benchmark fixture compiles on an isolated unmodified base export. No benchmarks executed; parent owns the measurement run. An initial attempt hit mise's untrusted temporary config, then an explicit-toolchain attempt hit inherited GOROOT mismatch; setting the matching Go binary/GOROOT resolved fixture compilation. Temporary exports were cleaned with Python TemporaryDirectory. No generated executables/test binaries/native artifact files retained. Go test caches are external; schema is an intentional tracked generated source update, not disposable build output.

**Handoff:** commit is the coherent Analyze checkpoint; exact SHA reported externally. Runtime consumer deferred because complete replay, storage/spill integration and charge/trap/profile validation require another experiment. The diagnostic identity is explicitly not a lowering certificate. The benchmark entrypoint is `BenchmarkGCLifetimeCompile/{qualifying,nonqualifying,no-gc}/{off,on}`; baseline adapter is the same single portable file with explicit unsupported-On skip. No performance claims were fabricated. Review `git diff --check`, exact staged paths and residual submodule dirt before commit.

## 2026-10-30 — committed first-slice compile benchmark

**Compared identities:** unmodified main `9713878ab452848f7eb7549a90be94b938a898be`
versus committed Analyze slice `160ce3a893996d1cffae4e70a61678b5169dcdad`.
Both are `git archive` exports, not shell worktrees; the baseline has only the
self-contained committed `gc_lifetime_bench_test.go` added. The port benchmark
binary is built from the exact committed slice, before adding a temporary proof
logging test in the export. No implementation/harness fix was needed.

**Mirror:** fetched Slipstream `origin/master`; old/new hash both
`db6014de2316f13dd08ea8618d614c28132f2688`; `git log db6014de2..origin/master`
is empty. No new changes to assess. B1/B7 remain adopted; ABI-specific host
boundary work remains deferred; old-space ceiling and helper wake/join work
remain pending. Wago retains Go-backed storage and stable handles. Source
97.55%/98.10% figures belong to Slipstream and are not Wago measurements.
Upstream backlog unchanged; no sensitive engine fixes in this lane.

**Correctness evidence:** `docs/rfwgc-evidence/benchmark/raw/focused.txt` reruns
all existing `TestGCLifetime*` tests from the committed port, without the whole
suite. In particular `TestGCLifetimeCompileIdentity/{qualifying,nonqualifying,no-gc}`
checks byte-for-byte native code and deep equality of the complete frame-root
plans for matching Off/Analyze choices at 1/4/8 workers (eight-function modules),
along with deterministic reports. This is within-port identity, not a claim of
cross-revision identity for every module. Existing tagged/race/runtime evidence
and known-red broad tagged-suite baseline comparison remain in force.
`benchmark/raw/sites.txt` separately logs the actual 32-function fixtures:
qualifying has 32/32 candidates, one proved site per function and known
denominators; nonqualifying has zero candidates, one observed constructor per
function before unsupported block opcode 0x02, with **unknown** complete
denominators; no-gc has zero candidates/sites and known denominators. The early
fallback is not proof of a complete allocation denominator. These are diagnostic
proofs, not eliminated allocations.

**Method and reproducibility:** `docs/rfwgc-evidence/benchmark/run.py` and
`summarize.py`; exact invocation and interpretation in the evidence README;
per-invocation commands, raw measurements, host metadata and process inspection
retained under `benchmark/raw/`. Compile one validated 32-function module per op;
decode/validation excluded; serial compilation (`Workers=1`), ordinary build
(no stats/race tags), identical catalog defaults apart from the Analyze toggle,
`GCStructHelpers=true`, deferred code mapping and matching cleanup. All three
variants are serial, never concurrent. Ten independent repetitions per cell,
one-second Go benchtime plus automatic calibration; round order rotates the nine
cells. Time/B/op/allocs are **host compiler costs**, not guest heap statistics.

**Runtime interpretation:** zero runtime allocation elimination/improvement by
design: this option only diagnoses; there is no lowering consumer. No guest
runtime speed measurement was performed. Any apparent compile-time speedup is
not a runtime speed claim. No collector throughput, RSS, real-world coverage,
Tiny/moving-GC speed, or Slipstream-equivalent elimination claim follows from
these small synthetic modules.

**Fixture limits:** these are short synthetic bodies with identical functions and
hot validated module data, not independent real-world programs. The no-gc case
has numeric-only expressions but retains the shared struct type and nullable
reference locals from the portable fixture, so it exercises the body negative
gate, not an entirely GC-feature-free module. No guest functions are invoked.
Deferred code mapping excludes executable mmap cost; worker parallelism is not
timed. A single host/core/toolchain and observation-based CPU admission cannot
exclude frequency/thermal drift, short-lived jobs or sub-threshold background
activity; process pinning is not exclusive CPU/SMT/cache reservation. Min/max and
sample CV describe observed spread, not statistical significance or confidence
bounds. Percentage effects below use ratios of medians, not paired confidence
intervals. Baseline-Off versus port-Off is the control for port-wide overhead;
port-On versus port-Off isolates enabling diagnostics.

**Owner steering / load qualification:** the initial quiet-admission attempt
stalled on unrelated long-running workloads. Owner explicitly directed a fresh
complete matrix under contention, with no indefinite rejection/retry and no
isolated speed-significance claim. That initial attempt (two accepted samples,
one rejected sample) and its script are preserved separately as
`benchmark/quiet-admission-attempt/` and `quiet-admission-run.py`; none of its
samples enter the table. The final `benchmark/raw/` matrix never rejects/retries
on load and observes contemporaneous processes/load during each invocation.
No external workloads were killed, repinned or otherwise changed.

The parent owns `docs/rfwgc-evidence/baseline-qualification/`; it is untouched by
this checkpoint. Owner names the remaining gap as the missing pinned Release 3
interpreter (bootstrap failed: missing dune); installed WABT 1.0.42 mismatched
pinned 1.0.41, and after verified 1.0.41, 18 staged failures needed the interpreter.
These are owner-reported qualification facts, not tests run or fixed here.

**Declared concurrent qualification:** parent ran final available root tests,
staged conformance explicitly skipped, with `taskset` CPU 7, `GOMAXPROCS=2`,
`-p=2`, during this exploratory run. CPUs 6 and 7 are SMT siblings on this host:
this is potential contention on the **same physical core**, in addition to other
recorded host activity. No admission wait or retry is used. Timing deltas cannot
isolate analysis overhead from that contention. Parent-owned qualification
outputs are deliberately excluded from this commit's staged paths.

**Host/run:** Linux x86-64, AMD Ryzen 7 8845HS (8 cores/16 threads),
Go 1.27.1; CPU 6 pinned, sibling 7, GOMAXPROCS=1; performance governor, boost
enabled, no fixed frequency or thermal control. Final run UTC 2026-10-30T20:28:12.366904+00:00
through 2026-10-30T20:31:49.621942+00:00 (includes builds/validation). One-minute load average
observed during timing: 16.89–31.44; 90/90 invocations
observed another process above 0.25 CPU in at least one sampling interval.
No sample was discarded from the final matrix. Raw per-sample process/load
observations remain in `during-timing.jsonl`. GOGC/GOMEMLIMIT/GODEBUG unset
(Go defaults); GOFLAGS/GOEXPERIMENT empty. Exact run:

```sh
python3 docs/rfwgc-evidence/benchmark/run.py --out docs/rfwgc-evidence/benchmark/raw
python3 docs/rfwgc-evidence/benchmark/summarize.py docs/rfwgc-evidence/benchmark/raw
```

| Case | Variant | n | Median µs/module | Median B/op | Median allocs/op | Time vs baseline/off | Time vs port/off | Min–max µs | Sample CV |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| qualifying | baseline-off | 10 | 298.67 | 131072 | 274 | +0.00% | +0.00% | 251.60–374.94 | 15.93% |
| qualifying | port-off | 10 | 301.34 | 131072 | 274 | +0.89% | +0.00% | 249.76–501.16 | 23.80% |
| qualifying | port-on | 10 | 368.72 | 147496 | 692 | +23.45% | +22.36% | 320.69–460.75 | 14.80% |
| nonqualifying | baseline-off | 10 | 268.56 | 119312 | 275 | +0.00% | +0.00% | 248.94–356.81 | 14.97% |
| nonqualifying | port-off | 10 | 305.02 | 119312 | 275 | +13.58% | +0.00% | 255.24–375.79 | 15.53% |
| nonqualifying | port-on | 10 | 320.76 | 124982 | 309 | +19.44% | +5.16% | 260.56–409.79 | 16.36% |
| no-gc | baseline-off | 10 | 95.90 | 33168 | 55 | +0.00% | +0.00% | 83.57–118.78 | 13.04% |
| no-gc | port-off | 10 | 94.15 | 33168 | 55 | -1.82% | +0.00% | 81.69–139.73 | 19.42% |
| no-gc | port-on | 10 | 97.65 | 38064 | 57 | +1.83% | +3.72% | 81.04–125.03 | 14.60% |

- qualifying: enabling Analyze adds median 16424 B/module (12.53%) and 418 allocations/module (152.55%).
- nonqualifying: enabling Analyze adds median 5670 B/module (4.75%) and 34 allocations/module (12.36%).
- no-gc: enabling Analyze adds median 4896 B/module (14.76%) and 2 allocations/module (3.64%).

**Findings:** Off host allocation medians match baseline exactly in every case
(0% B/op and allocs/op change). Enabled diagnostics add host allocation work,
including even the cheap no-gc report path. Observed median time deltas On/Off
are +22.36%, +5.16%, +3.72%; Off/baseline ranges from -1.82% to +13.58%.
Sample time CV is 13.04–23.80%, with wide ranges and declared SMT contention.
Those Off-control shifts and variance prevent attributing time differences to
the port alone or asserting isolated regression/speed significance. Treat the
times as exploratory host observations; allocation counts/bytes provide clearer
evidence of the diagnostic cost. Runtime improvement remains zero by design,
not a measured speed claim. No Wago elimination percentage is established.

**Completion/cleanup:** 90 final measurements (10 per cell), all focused tests
and supplemental proof diagnostics passed. No additional full-suite run by this
worker. A final mirror fetch again reports db6014de2 unchanged. Temporary archive
exports, proof fixture, test executables and test-created temporary files were
removed after each attempt; only scripts/raw evidence/docs retained. No engine
or existing portable fixture edits, no additional workers, no review delegation,
PR/push/rebase or upstream backlog change. Exact checkpoint paths are this log,
`docs/rfwgc-evidence/README.md`, and `docs/rfwgc-evidence/benchmark/` only;
parent baseline-qualification and dirty spec-v1/spec-v2 remain untouched.


## 2026-10-30 — orchestrator consolidation

All five T3 sol worker tasks have completed and their results have been collected: mapping, initial shared baseline, isolated baseline qualification, implementation, and benchmarks. Maximum concurrent workers was two (owner limit eight). The benchmark worker survived parent yields; it was steered out of indefinite quiet-host waiting into a bounded, explicitly contention-qualified run. No replacement worker was needed. The initial shared baseline's checked run was contaminated by concurrent edits and is superseded by the isolated qualification. No Claude review or sensitive engine bug fix was performed.

### Delivered scope and checkpoint chain

- Mapping: `f83004ebcfd27ec75b0fe78d8b0921d23a5e7be9`, `docs/rfwgc-port-plan.md`.
- First slice: `160ce3a893996d1cffae4e70a61678b5169dcdad`, opt-in AMD64 bounded lifetime **analysis only**, through scalar-struct local aliases. Enable `--gc-lifetime-analyze` or `WithOptimization("gc-lifetime-analyze", true)`. It proves 32/32 qualifying fixture sites; it does not consume proofs for lowering or eliminate runtime allocations. A replay-checked lowering consumer remains the next experiment.
- Measurement: `4c31bab7f3027e44c0c7dd088bede4466d8f97b1`, 90 samples against exact `9713878ab` and port Off/Analyze snapshots. Parent reran `summarize.py` and reproduced the table above. Median host allocation costs are informative; time deltas are exploratory due to 13–24% CV and declared contention, including shared physical-core activity. No speedup or Wago elimination percentage is claimed.
- Final evidence: isolated baseline reports, raw outputs, bootstrap failure evidence and reproduction script are retained in `docs/rfwgc-evidence/baseline-qualification/`. This consolidation commit's SHA is reported in the final handoff rather than embedded self-referentially here.

### GAP-CONFORMANCE-TOOLCHAIN — open, named qualification gap

The exact unmodified baseline passed **136 packages** and failed one package (`src/wago`) in both ordinary and regalloccheck root suites. All 7,374 tracked blobs matched the base before and after. Initial 24 staged-test failures were caused by installed WABT **1.0.42** differing from pinned **1.0.41**. After verified 1.0.41 bootstrap, **18 staged tests** still needed the pinned official WebAssembly **Release 3 interpreter** via `WAGO_SPEC_INTERPRETER`. Interpreter bootstrap failed with the exact message: **`dune is required to build the official Release 3 interpreter`**. `dune` is the missing build tool; the pinned interpreter is the missing conformance executable.

To close this gap, provide `dune` and interpreter prerequisites, successfully run `scripts/bootstrap-spec-interpreter.sh --verify`, set `WAGO_SPEC_INTERPRETER` to the pinned interpreter, and rerun staged conformance on baseline and port with WABT 1.0.41. No conformance expectation files were changed. Full conformance is not qualified and a full-green suite is not claimed. See `baseline-qualification/README.md`, `report.txt`, `interpreter-bootstrap.txt`, and the pinned-WABT logs for commands and evidence.

The broader codegenstats suite's 24 reproduced baseline counter/admission failures remain separately recorded in the Analyze checkpoint; the focused diagnostics/race/identity tests pass. These are not silently folded into the missing-tool gap.

### Final mirror and repository disposition

The orchestrator fetched Slipstream master again after collecting benchmark completion: old/new cursor both `db6014de2316f13dd08ea8618d614c28132f2688`. No newly landed collector/lifetime/host-boundary changes. B1/B7 adaptations remain present; the source-specific host-boundary transplant remains deferred; old-space ceiling and helper wake/join changes remain pending source work with applicability decisions above. No rebase, PR, push (including upstream), or maintainer backlog change. The branch stays local. Pre-existing spec-v1/spec-v2 submodule differences are preserved. Temporary worker build trees, binaries, downloaded tools and fixtures were cleaned; retained artifacts are evidence/scripts and intentional source/schema changes.


### Parent final validation and audit adjustment

The parent ran `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 GOMAXPROCS=2 GOFLAGS=-p=2 taskset -c 7 go test -count=1 -timeout=5m -skip '^TestStaged' ./...`. CPU pinning was released for that test process tree after the benchmark worker completed; no external workload affinity was changed. The staged skip is explicit and does not close GAP-CONFORMANCE-TOOLCHAIN.

This wider run caught `TestProductionPolicyRejectsWorkloadIdentity`: its blanket hash/byte-matcher prohibition also rejected the new diagnostic-only report identity and negative GC-prefix eligibility gate. The parent aligned that audit with the diagnostic contract using an exact `amd64/gc_lifetime.go` SHA-256 import exception and an AST-checked exception restricted to `analyzeGCLifetime` calling `bytes.Contains(c.BodyBytes, []byte{0xfb})`. New scope tests reject other files, hashes, functions, needles and prefix matches. The full Railshot policy package then passed. No production code changed, no audit expectation was removed, and the benchmarked implementation is still exact `160ce3a89`.

The constrained run also hit a five-minute standalone-package timeout; its isolated baseline had passed. A targeted recheck at normal parallelism (`GOMAXPROCS=8 GOFLAGS=-p=8`, ten-minute timeout) was launched, with its outcome recorded below. This timeout is distinct from staged conformance tooling and is not evidence of an engine fix.

The constrained root run exited 1: 134 packages passed; three packages failed (the corrected policy audit, standalone TinyGo interruption timeout, and `src/wago` timeout in `TestAtomicWaitFastZeroOrdering/notify-literal-zero`). Raw output is `docs/rfwgc-evidence/final-available-tests.txt`. The runtime package was also rechecked with GOMAXPROCS=8 and the same explicit staged skip. These low-parallelism timeout observations are retained, not erased by later targeted passes.

The runtime recheck passed: `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 GOMAXPROCS=8 GOFLAGS=-p=8 go test -count=1 -timeout=5m -skip '^TestStaged' ./src/wago` (see `final-runtime-recheck.txt`). This establishes the available runtime tests at normal parallelism, not staged conformance or qualification at GOMAXPROCS=2.

The standalone recheck also passed: `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 GOMAXPROCS=8 GOFLAGS=-p=8 go test -count=1 -timeout=10m ./cli/manager/internal/standalone` (see `final-standalone-recheck.txt`). All three failing packages from the parent available-suite run now have passing targeted follow-ups; the other 134 packages passed that run. There was no additional all-packages rerun after the test-only audit adjustment. Full conformance, the previously reproduced broad codegenstats failures, low-parallelism timeout qualification, and uncontended timing remain explicitly open limitations.

Final parent checks: benchmark table regenerated from all 90 raw samples, policy scope tests passed, reproduction shell syntax checked, and whitespace/staged paths reviewed. All T3 workers and parent test processes completed. Go test temporary build outputs were cleaned by the tool; only intended source/schema/docs/evidence remain, alongside the untouched pre-existing submodule differences.

## 2026-10-30 — quiet-admitted sequential benchmark rerun

**Compared identities:** candidate `60fdefe0b2781c1868e2351a482179f9e89c1ede`
against unmodified main `9713878ab452848f7eb7549a90be94b938a898be`.
The candidate backend and portable fixture are unchanged from first-pass
`160ce3a89` (`git diff` of the AMD64 backend is empty). Both revisions were
`git archive` exports; only the identical portable benchmark fixture was added
to main. No production source changed. Work ran in the supplied T3 checkout,
initially `t3/rerun-port-benchmarks` at `60fdefe0b`; the completed evidence commit
is also fast-forwarded onto the requested `experiment/rfwgc-port` branch.
Pre-existing spec-v1/spec-v2 submodule dirt is preserved in both checkouts.
No agents were launched, no other test suite ran during sampling, and all
benchmark invocations were sequential fresh processes.

**Host and admission:** Linux x86-64, AMD Ryzen 7 8845HS (8 cores/16 threads),
Go 1.27.1; CPU 6 pinned, SMT sibling 7; GOMAXPROCS=1, performance governor,
boost enabled, no fixed frequency or thermal control. Initial inspection at
20:47:49 UTC showed load averages **3.11 / 10.57 / 12.25** and no visible fleet
build/test process. Load average is historical runnable demand, not a CPU
contention percentage. The old table's 13–24% values are sample CVs, also not
an instantaneous CPU contention measurement.

The complete run was **20:48:54–21:32:13 UTC** including preparation and waits.
A Starshine `cmd.exe` compilation launched by `/tmp/hso-current-o4.py` appeared
after the initial inspection, using about one CPU. Admission waited through it;
no external process was killed, repinned or otherwise altered. Additional CPU
bursts continued after it exited. No competing compiler/test process was
observed in the accepted timing observations or retained process snapshots
before/after rounds. This is observation-based qualification, not exclusive
host reservation; short-lived processes can escape process sampling.

Admission requires **three consecutive two-second intervals <=5%** on both
host busy CPU and each of CPUs 6/7. During an invocation, host external busy CPU
is `/proc/stat` busy time over **the other 15 logical CPUs**, excluding the
benchmark's pinned CPU 6; CPU 7 is also checked separately against 5%.
Approximately one-second observations cover calibration and measurement;
final intervals shorter than 0.25 seconds are not scored. Any scored interval
above either limit rejects the entire invocation, then admission is repeated.
This is an explicit CPU-activity proxy for contention, not a measurement of
benchmark slowdown or proof of zero scheduling/cache/frequency interference.

First sampling began at **20:53:06 UTC**, after approximately **190 seconds**
of initial admission waiting. Across the complete run, 1,154 admission intervals
consumed **2,309.5 seconds** (38m29.5s), including 576 over-limit intervals totaling
**1,152.7 seconds**; maximum observed admission host busy CPU was **17.02%**.
These waits are deliberately excluded from benchmark ns/op. **24 compile and
10 analysis invocations were rejected**, retained separately, and retried;
90 compile plus 30 analysis invocations were accepted.

During accepted compile invocations, 177 scored observations had external host
CPU mean/max **0.70% / 4.98%**, sibling-7 maximum **4.04%**, and one-minute load
**0.50–1.21**. During accepted analysis invocations, 60 observations had host
mean/max **0.68% / 4.56%**, sibling maximum **3.13%**, and load **0.51–1.06**.
One accepted analysis observation saw the T3 service at 48.88% of one CPU;
its aggregate external host activity was 4.52%, within admission policy.
There was no threshold relaxation. No result was removed based on its timing.

**Method:** same original `BenchmarkGCLifetimeCompile` nine cells, ten accepted
independent repetitions per cell, one-second benchtime with Go calibration,
rotating nine-cell order. Each operation compiles a validated 32-function module;
decode/validation excluded, Workers=1, ordinary build, identical option defaults
except `gc-lifetime-analyze`, GCStructHelpers enabled, deferred mapping and
matching cleanup. Bytes/allocations are host compiler costs, not guest heap
statistics. Off on main is the meaningful oracle; main lacks the Analyze option.
Go GOGC/GOMEMLIMIT/GODEBUG unset; GOFLAGS/GOEXPERIMENT empty. No guest execution,
runtime elimination, collector throughput or real-world coverage is measured.
The first-pass fixture/denominator limitations remain unchanged.

| Case | Variant | n | Median µs/module | Median B/op | Median allocs/op | Time vs baseline/off | Time vs port/off | Min–max µs | Sample CV |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| qualifying | baseline-off | 10 | 130.33 | 131072 | 274 | +0.00% | +0.00% | 128.94–134.95 | 1.31% |
| qualifying | port-off | 10 | 127.95 | 131072 | 274 | -1.82% | +0.00% | 126.98–129.99 | 0.87% |
| qualifying | port-on | 10 | 157.82 | 147495 | 692 | +21.09% | +23.34% | 156.94–160.01 | 0.53% |
| nonqualifying | baseline-off | 10 | 127.48 | 119312 | 275 | +0.00% | +0.00% | 127.24–129.16 | 0.47% |
| nonqualifying | port-off | 10 | 125.70 | 119312 | 275 | -1.40% | +0.00% | 124.67–127.61 | 0.79% |
| nonqualifying | port-on | 10 | 131.00 | 124982 | 309 | +2.76% | +4.22% | 130.41–134.33 | 0.94% |
| no-gc | baseline-off | 10 | 42.28 | 33168 | 55 | +0.00% | +0.00% | 42.10–43.89 | 1.44% |
| no-gc | port-off | 10 | 41.62 | 33168 | 55 | -1.57% | +0.00% | 41.37–42.43 | 0.76% |
| no-gc | port-on | 10 | 42.60 | 38064 | 57 | +0.75% | +2.35% | 42.06–42.88 | 0.63% |

Off host allocation medians exactly match main in all three fixtures. Enabling
analysis adds 418 / 34 / 2 host allocations per module and approximately
16,423 / 5,670 / 4,896 B/module respectively. The qualifying B/op is one byte
below the first-pass rounded median; the allocation-work conclusion reproduces.
Off time is 1.40–1.82% below main here; this small synthetic control difference
is not established as a general speedup. Compile sample CV is **0.47–1.44%**,
versus **13.04–23.80%** in the exploratory table.

**Analysis cost itself:** a separate temporary benchmark calls
`analyzeGCLifetimes(m)`, the full serial module pass used by
`--gc-lifetime-analyze`, retaining its report in a package-level sink. The same
three validated 32-function fixtures are constructed outside timing. Ten
fresh-process repetitions per fixture, rotating order, same one-second
calibration/pinning/admission policy, run after the compile matrix. This times
analysis/report construction, excluding CLI startup, formatting, validation and
native compilation. The original compile binary was built before adding this
extra benchmark; it cannot affect the compile cells.

| Case | Candidate analysis µs/module | n | B/op | allocs/op | Min–max µs | Sample CV | Main analysis |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| qualifying | 28.7915 | 10 | 16417 | 418 | 28.600–29.600 | 1.04% | N/A: pass absent |
| nonqualifying | 4.5990 | 10 | 5664 | 34 | 4.579–4.637 | 0.49% | N/A: pass absent |
| no-gc | 0.8203 | 10 | 4896 | 2 | 0.8146–0.8345 | 0.88% | N/A: pass absent |

Whole-compile On-minus-Off differences of medians are approximately
29.87 / 5.30 / 0.98 µs/module. These independently measured differences need not
equal standalone pass timings; neither is a guest runtime measurement.

**Verdict:** the qualitative added analysis/allocation cost holds (+23.34%,
+4.22%, +2.35% On/Off), but first-pass absolute timings and the apparent +13.58%
nonqualifying Off regression do not hold; use this quieter table instead.

**Reproduction/evidence:** `docs/rfwgc-evidence/benchmark/quiet-rerun.py` pins both
revisions and retains the original fixture hash, host metadata, exact invocation
commands, accepted/rejected raw timings, admission intervals, per-invocation CPU
observations, process snapshots, focused test output and 32-function proof logs
under `benchmark/quiet-2026-10-30/`. Run from the repository root to a fresh output
directory (runtime is dominated by admission waits):

```sh
python3 docs/rfwgc-evidence/benchmark/quiet-rerun.py --out /tmp/wago-quiet-results
python3 docs/rfwgc-evidence/benchmark/summarize.py /tmp/wago-quiet-results
```

The standalone analysis benchmark source is embedded in the runner; its raw
`??-*-analysis.txt` files report ns/op, B/op and allocs/op directly. The log's
standalone table uses medians, range and sample standard deviation/mean.
Focused `TestGCLifetime*` tests and actual-fixture candidate/site checks passed;
no broad-suite/conformance rerun or new qualification claim was made.

**Cleanup and repository scope:** temporary archive trees, fixture additions,
all test binaries, GOTMPDIR outputs and the private GOCACHE were removed by
TemporaryDirectory, verified absent after completion. The metadata's `go env`
query precedes the temporary GOCACHE/GOTMPDIR override; actual builds use that
private disposable cache. An earlier preparation was interrupted before any
sampling to correct monitoring bookkeeping; its temporary directory was also
removed. No generated build outputs remain; only the log, reproduction script
and raw text/JSON evidence are committed. Existing unrelated submodule dirt and
prior evidence are untouched. No PR, push, rebase or upstream publication.

## Stage 2 implementation cycle — 2026-10-30

Started on experiment/rfwgc-port at quiet baseline 6ceb86221. Read the local
plan/log and supplied instructions. Two read-only fetches of Slipstream
origin/master during implementation both resolve to
`db6014de2316f13dd08ea8618d614c28132f2688`: no new collector/lifetime/host-boundary
changes to mirror. The prior ABI-specific host-boundary defer still applies.
No source checkout mutations, delegation, Claude review, engine bug fixes, push,
PR, rebase or upstream backlog work. Existing spec-v1/spec-v2 dirt is untouched.

Implemented sealed/replayed checked scalar replacement in
`amd64/gc_lifetime_consumer.go` and connected it before ordinary body emission.
It owns and replays typed semantic records and full relevant structural context;
it never consults diagnostic Identity to authorize lowering. Original byte PCs
and constructor safepoints are retained. Native dead charging removes published
object/handle/payload work on successful fast paths; ordinary helpers remain on
all slow boundaries. The opt-in remains `gc-lifetime-analyze`, default Off.

Focused execution, code identity at 1/4/8 workers, replay mismatch/context tests,
parent Railshot policy audit, native reservation tests, diagnostics and enabled
regalloccheck consumer tests pass. A separate wagodebug+regalloccheck run confirms
failure-injection builds take baseline fallback. The final evidence checkpoint
records commands/results and portability qualification. Benchmarks are deliberately
not run; parent owns serial benchmark and broad-suite qualification.

### Stage 2 evidence and handoff checkpoint

Final read-only mirror fetch again returned unchanged db6014de2; no additional
collector/lifetime/host changes to mirror. Added an explicit consumer entrypoint
so mismatch tests execute rejection with no emission/error, then ordinary body
lowering continues. Added portable `docs/rfwgc-evidence/stage2/runtime.py` and
retained seven passing text logs plus a README with exact commands, limitations,
metrics and the parent-only benchmark entrypoints. Both baseline archive and
current-tree check-only harness invocations pass; no benchmark was executed.

Exact 128-call qualifying fixture observations: Throughput Off/On both charge128,
layout3072B, observed nursery advance3072B and retained3072B. Materialized objects
and published handles fall128->1; successful handleless charges0->127. Small
nursery materializations fall128->4, with identical charge/advance totals and
retained48B. Stress/Tiny/disabled collection keep128 materializations. Nonqualifying
keeps128 materializations; no-GC keeps0. Layout24B is not throughput heap Align16
rounded32B: the existing nursery path's observed24B bump advance is reported as
an observation, not inferred from layout or a changed allocator contract. Stats
Allocations still counts all charges; LiveObjects is never a publication metric.

Focused parent Railshot policy package and four-package tests, active-consumer
regalloccheck, diagnostics, combined debug fallback/native reservation tests and
expanded accounting tests pass. The full standalone execution suite was not run;
parent owns final full-suite and serial benchmark qualification. No build artifacts
remain; temporary exported baseline trees were removed and evidence logs retained.

## Stage 2 full-suite qualification — 2026-10-30

Qualified the exact `1ad799605c7018457b4b0cef4190c90132e337ac` archive in a disposable
snapshot, without a shell worktree or source edits. Evidence/reproduction recipe:
`docs/rfwgc-evidence/stage2-suite/`. All 7,706 tracked blobs verified before and
after testing. Isolated pinned spec-v1/v2/v3 fixtures, verified WABT 1.0.41,
Go/GOROOT 1.27.1, automatic GOWORK, trusted temporary mise config,
GOMAXPROCS=8, GOFLAGS=-p=8, DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1;
15-minute per-package timeouts and no CPU affinity restriction.

**Actual state: GAP-CONFORMANCE-TOOLCHAIN; full roots are not green.** Both
`go test -count=1 -timeout=15m ./...` and the same command with
`-tags=wago_regalloccheck` completed: each 136 passing packages, 1 failing package
(`src/wago`), 50 packages with no test files. Each has the same 18 top-level staged
failures: pinned WABT cannot convert some Release 3 syntax; the required official
interpreter fallback is unavailable. Bootstrap fails explicitly at missing dune;
ocaml/ocamlc and menhir are absent too. No system installs or expectation changes.
Raw logs, exact failing tests/subtests, converter errors and accounting diffs are
retained. This matches the earlier pinned-WABT baseline failure set, demonstrates
no engine regression, and leaves the blocked staged paths unqualified.

Only the failing runtime package needed a remainder check: ordinary and checked
`-skip '^TestStaged' ./src/wago` each passed (1 package apiece). Root standalone
TinyGo, plugin and all other tested packages passed; no package timeout. The
additional `-tags=wago_runtime ./cli/...` suite passed: 55 packages, 0 failures,
36 with no test files. Full roots were run without staged exclusions first;
these remainder results do not substitute for full conformance qualification.

Slipstream read-only fetch remains db6014de2316f13dd08ea8618d614c28132f2688,
with no newly relevant changes. Region-first default b3e802899; 97.55%/98.10%
are source Slipstream figures, not Wago measurements. B1/B7 remain; source host
fix db6014de2 is landed there with ABI-specific transplant deferred; old-space
ceiling and helper wake/join remain pending. Stage2 retains the same opt-in flag:
qualifying128 calls reduce published materializations128->1 with charges128
unchanged. No benchmarks run; parent can now benchmark without suite CPU load.
No implementation/sensitive-engine fixes (DaybreakBlue only), delegation, PR,
push, rebase or upstream backlog action. Snapshot, builds, fixtures, downloads,
private cache and test temp outputs removed; evidence retained. Existing
spec-v1/spec-v2 dirt and parent-owned stage2-orchestrator files are preserved
and excluded from staging.

## Stage2 measured quiet comparison — 2026-10-30

Pinned baseline `6ceb862219e64480c0503836b19ee1e02b209cc7` versus Stage2 `d28005d48f42524f9bb8a6556d92c5b701c42cd9`. Evidence: `docs/rfwgc-evidence/stage2-benchmark/` (scripts, raw accepted/rejected samples, telemetry, environment, cleanup, mirror checks). Disposable archives; identical portable runtime harness copied only into baseline; original three bodies plus export, unchanged 32-function compile fixtures. No engine edits or repeated full suites.

Fresh matched run: CPU10 / SMT sibling11 (CPU6 long groups rejected and retained separately), Go1.27.1, GOMAXPROCS1 compiler/runtime, GOWORKoff, private cache/GOTMPDIR. Six accepted500ms samples per cell; serial Off+On fixture pairs, alternating revision/suite order and rotating fixtures. Three2s <=5% external-host/both-core admission windows, then <=5% external-host excluding benchmarkCPU and sibling for every monitored interval over the entire invocation.72 accepted/24 rejected pairs; no parent builds/tests. Initial cwd failure retained separately; reused existing binaries after parent method steering. Fresh window22:37:17–22:55:37UTC; bounded no-progress guard never exceeded in fresh run.

### Results

Each timing cell: median [min, max]; sample CV. Time unit ns/op; lower is faster. Percentages compare medians.

| Suite / fixture / option | n baseline/stage2 | Baseline ns/op [min,max]; CV | Stage2 ns/op [min,max]; CV | Stage2 vs baseline | Host B/op baseline → stage2 | Host allocs/op baseline → stage2 |
|---|---:|---|---|---:|---:|---:|
| compile / no-gc/off | 6/6 | 41797.500 [41497.000, 41928.000]; 0.37% | 42441.000 [41898.000, 42767.000]; 0.82% | +1.54% | 33168 → 33168 | 55 → 55 |
| compile / no-gc/on | 6/6 | 42650.500 [42561.000, 43026.000]; 0.39% | 44286.000 [43735.000, 44716.000]; 0.88% | +3.83% | 38064 → 38064 | 57 → 57 |
| compile / nonqualifying/off | 6/6 | 126547.500 [126133.000, 129745.000]; 1.08% | 128257.000 [127646.000, 129597.000]; 0.51% | +1.35% | 119312 → 119312 | 275 → 275 |
| compile / nonqualifying/on | 6/6 | 132045.000 [130931.000, 133253.000]; 0.62% | 142825.500 [141950.000, 143976.000]; 0.51% | +8.16% | 124982 → 125750 | 309 → 341 |
| compile / qualifying/off | 6/6 | 128385.000 [127317.000, 129972.000]; 0.70% | 129865.500 [129401.000, 136185.000]; 2.01% | +1.15% | 131072 → 131072 | 274 → 274 |
| compile / qualifying/on | 6/6 | 160469.000 [159543.000, 169893.000]; 2.47% | 352295.500 [351209.000, 354125.000]; 0.31% | +119.54% | 147496 → 299472 | 692 → 2463 |
| runtime / throughput/no-gc/on=false | 6/6 | 397.950 [397.200, 409.800]; 1.22% | 410.850 [409.200, 415.300]; 0.65% | +3.24% | 0 → 0 | 0 → 0 |
| runtime / throughput/no-gc/on=true | 6/6 | 398.900 [397.800, 407.100]; 0.88% | 419.050 [412.200, 427.000]; 1.28% | +5.05% | 0 → 0 | 0 → 0 |
| runtime / throughput/nonqualifying/on=false | 6/6 | 278.400 [276.900, 279.500]; 0.30% | 289.050 [286.600, 304.700]; 2.31% | +3.83% | 0 → 0 | 0 → 0 |
| runtime / throughput/nonqualifying/on=true | 6/6 | 278.550 [277.100, 281.800]; 0.57% | 289.650 [288.000, 296.500]; 1.12% | +3.98% | 0 → 0 | 0 → 0 |
| runtime / throughput/qualifying/on=false | 6/6 | 279.000 [277.100, 280.200]; 0.46% | 287.800 [287.000, 289.300]; 0.33% | +3.15% | 0 → 0 | 0 → 0 |
| runtime / throughput/qualifying/on=true | 6/6 | 279.200 [276.400, 286.800]; 1.62% | 276.700 [275.000, 285.900]; 1.48% | -0.90% | 0 → 0 | 0 → 0 |

Within-revision On vs Off median runtime/compile costs:

| Suite / fixture | Baseline On vs Off | Stage2 On vs Off |
|---|---:|---:|
| compile / no-gc | +2.04% | +4.35% |
| compile / nonqualifying | +4.34% | +11.36% |
| compile / qualifying | +24.99% | +171.28% |
| runtime / throughput/no-gc | +0.24% | +2.00% |
| runtime / throughput/nonqualifying | +0.05% | +0.21% |
| runtime / throughput/qualifying | +0.07% | -3.86% |

Guest accounting (separate fresh instance, exactly 1024 invocations; NOT timed). All repeated observations must agree. Totals below are exact counters reconstructed from per-op values when representable; raw Go output rounds the fractions.

| Revision / runtime cell | Charges | Handleless | Materialized / published | Layout B | Observed nursery advance B | Retained nursery B | Rounded heap request B (calculated) |
|---|---:|---:|---:|---:|---:|---:|---:|
| baseline / throughput/no-gc/on=false | 0 | 0 | 0 / 0 | 0 | 0 | 0 | 0 |
| stage2 / throughput/no-gc/on=false | 0 | 0 | 0 / 0 | 0 | 0 | 0 | 0 |
| baseline / throughput/no-gc/on=true | 0 | 0 | 0 / 0 | 0 | 0 | 0 | 0 |
| stage2 / throughput/no-gc/on=true | 0 | 0 | 0 / 0 | 0 | 0 | 0 | 0 |
| baseline / throughput/nonqualifying/on=false | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| stage2 / throughput/nonqualifying/on=false | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| baseline / throughput/nonqualifying/on=true | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| stage2 / throughput/nonqualifying/on=true | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| baseline / throughput/qualifying/on=false | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| stage2 / throughput/qualifying/on=false | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| baseline / throughput/qualifying/on=true | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| stage2 / throughput/qualifying/on=true | 1024 | 1023 | 1 / 1 | 24576 | 24576 | 24576 | 32768 |

### Interpretation

Guest publication reduction is measured: qualifying Stage2 On makes 1 materialized
object / published handle in 1024 fresh calls versus baseline Off/On and Stage2 Off
1024, with 1023 handleless charges. All four still charge 1024 allocations, consume
24576 layout bytes, and show 24576 bytes observed nursery advance / retained end.
Calculated throughput rounded requests remain32768B; this is not measured physical
reservation traffic. Nonqualifying remains1024 materializations/charges; no-GC
remains0. Host runtime allocations remain0 B/op and0 allocs/op in all twelve cells.
This extends, and does not replace, the existing128-call128->1 publication proof.

Qualifying runtime On is276.7ns/op versus baselineOn279.2ns/op (-0.90% latency,
+0.90% reciprocal throughput); ranges overlap275–285.9 versus276.4–286.8ns and CVs
1.48%/1.62%. Within Stage2, On versusOff287.8ns is-3.86% latency (+4.01% throughput).
Controls also move: Off runtime costs rise3.15–3.83% across revisions, nonqualifying
On rises3.98% and no-GCOn5.05%. This does not establish a robust net runtime speedup
against the quiet baseline; the qualified result is a modest local On/Off effect
plus clear guest-publication reduction. It is not a general application claim.

Compiler cost is substantial: qualifyingOn160469->352295.5ns (+119.54% versus
baselineOn); host147496->299472B/op and692->2463allocs/op. Stage2On versusOff is
+171.28%, versus newly measured baseline+24.99%. NonqualifyingOn rises8.16% across
revisions (host124982->125750B/op,309->341allocs/op); no-GCOn rises3.83% (host38064B,
57allocs unchanged). Off compiler changes are+1.15%,+1.35%,+1.54% qualifying,
nonqualifying,no-GC respectively, with unchanged host allocation counts. Prior
quiet Stage1 option costs+23.34/+4.22/+2.35% are context, not Stage2 measurements.
Keep the optimization opt-in; guest handle publication savings do not justify a
claim of free allocation removal or a clear end-to-end throughput win here.

The fresh matched dataset took18m19s including quiet waits:72 accepted pairs,
24 rejected pairs, six accepted500ms samples per cell. Accepted monitored maxima:
4.7873% external host and2.9703% SMT sibling; full raw telemetry retained. Initial
long-group and preparation attempts extend total wall time beyond that fresh
window. No statistical significance test or exclusive-machine isolation; CV and
min/max are descriptive. Tiny timing omitted; existing fallback tests are the
only Tiny evidence. No new sensitive-engine issue was fixed or investigated.

Start/final Slipstream fetch unchanged `db6014de2316f13dd08ea8618d614c28132f2688`: no new relevant changes. Region-first default b3e802899; B1/B7; ABI-specific hostfix db6014de2 deferred for Wago; old-space ceiling/helper wake-join pending; Go-backed arenas/stable handles retained. No source coverage percentage is a Wago measurement. All owned archives/binaries/GOTMPDIR/privatecache removed. Parent profile-test guards modified during run were excluded and do not affect pinned binaries. Existing spec-v1/v2 dirt preserved. No sensitive fixes (DaybreakBlue only), subdelegation, PR/push/rebase/upstream backlog changes.


## 2026-10-30 — Stage 2 final orchestrator handoff

All five Stage 2 T3 sol tasks completed and their results were collected: implementation, allocation-contract study, independent correctness review, isolated full-suite qualification, and quiet benchmarks. Maximum concurrent workers was two, below the owner limit of eight. No worker was abandoned. The benchmark worker was steered from repeatedly rejected large CPU6 groups to smaller matched CPU10/sibling11 pairs; the rejected attempt is retained separately and excluded from the final tables. No other workload was killed or repinned.

### Implemented result and evidence

- Production checkpoint: `e98051264`; implementation/test handoff: `1ad799605`. The existing, default-off `gc-lifetime-analyze` flag now includes structurally replayed checked scalar replacement. It avoids qualifying native fast-path object/header/handle/payload materialization while retaining checked charges and fallbacks. It does not remove all reservations or replace the collector.
- Additional mixed-field and caller-frame-root tests: `07d938f37`. Default and regalloccheck executions passed at the 32-field boundary, with exact scalar bits, helper/refill pressure and collections across a non-inlined callee. The read-only review reported no actionable correctness defect; exhaustive host-reentry/regional-register combinations remain outside this evidence.
- Full-suite evidence: `d28005d48`. Exact production snapshot `1ad7996`: ordinary and regalloccheck roots each passed 136 packages, failed only `src/wago` staged tooling tests, and had 50 no-test packages. Both available-runtime rechecks passed; CLI runtime-tag suite passed 55 test packages with zero failures. Nested workspace modules are not implied by root `./...`.
- Quiet measurement: `c1fe1a6f2`, against exact `6ceb86221`. Parent regenerated the tables without changes and verified the accepted telemetry. Every required cell has six accepted 500 ms samples. All 72 accepted pair invocations satisfy the unchanged 5% external-host/SMT-sibling threshold. Fixture expression bytes were independently compared with the quiet-baseline compiler fixtures and match in all three cases.
- Final test-only correction: profiling builds intentionally bypass the consumer; runtime assertions now account for that and explicitly test the long-alias profiling fallback. Focused profile/backend/policy tests and the default Stage 2 runtime family passed after timing completed. Raw results and commands are in `stage2-orchestrator/`. No production change followed the measured checkpoint.

The measured qualifying result is **1,024 to 1 materialized objects/published handles**, with 1,023 handleless charges. Allocation charges remain 1,024 and observed nursery advance remains 24,576 bytes. Nonqualifying materializations stay 1,024; no-GC stays zero. Host runtime allocation counters are zero in all cases. This is a narrow fixture result, not a Wago-wide elimination percentage or zero-reservation claim.

Qualifying enabled runtime median is 279.2 to 276.7 ns (−0.90%), with overlapping ranges; a robust baseline speedup is not established. The rejected/no-GC enabled controls are 3.98%/5.05% slower, and Off controls also move upward. Enabled qualifying compilation is 160.469 to 352.2955 microseconds (+119.54%), with 692 to 2,463 host allocations per 32-function module. The useful result is publication elimination with preserved semantics; runtime benefit remains modest/uncertain and compilation cost is substantial. Keep it opt-in. The full before/after and Off/On tables, CVs, byte counts and raw evidence above are authoritative; previous Stage 1 cost percentages are historical context.

### GAP-CONFORMANCE-TOOLCHAIN remains open

Each full root run has the same 18 top-level staged failures as the pinned-WABT baseline. Verified WABT **1.0.41** still needs the official WebAssembly **Release 3 interpreter** for unsupported syntax. Its bootstrap fails because **`dune` is missing**; `ocaml`, `ocamlc` and `menhir` are also absent. This is an explicit qualification gap, not a green full-conformance result. No expectations were rewritten and no system toolchain was installed. Exact errors and closure/reproduction steps remain in `stage2-suite/` and the earlier `baseline-qualification/` evidence.

### Mirror, cleanup and repository state

Final orchestrator read-only fetch again returned Slipstream master **`db6014de2316f13dd08ea8618d614c28132f2688`**. Old/new cursor is unchanged; no new collector, lifetime-analysis or host-boundary change requires a mirror. B1/B7 adaptations and prior applicability decisions remain; ABI-specific host-boundary transplant stays deferred, and old-space ceiling/helper wake-join changes remain pending source work. No source percentage is adopted as a Wago measurement.

All workers, owned benchmark runners and parent test processes have completed. Worker archives, fixtures, binaries and private caches were removed, and parent Go test temporary outputs were cleaned. Retained files are intentional code/tests/docs/evidence. No PR, push, rebase, upstream publication or maintainer-backlog change. The pre-existing spec-v1/spec-v2 submodule differences remain untouched. Final head is reported in the handoff rather than embedded self-referentially in this commit.


## 2026-11-01 — Owner order: combined Snap port and tabled GC fixes

Joshua supersedes the earlier prohibition on bundling tabled GC changes. The
single ready port PR now includes every compatible fix from PRs 997, 1003, 1019,
1020, 1026, 1027 and 1030, plus the in-flight TinyGo foreign-stack GC repair.
No existing PR is to be closed now. Included PRs are closed as superseded only
when the combined PR opens. The final combined diff still requires the same
three-Daybreak-plus-Claude consolidated review, zero unresolved findings, and
full green final-head gates.

For each PR the final disposition will be exactly INCLUDED (integrated and
required gates green), SUPERSEDED (port makes the change moot, with evidence), or
INCOMPATIBLE (left tabled, with concrete reason). Assessment is in progress;
no final bucket is asserted before its supporting evidence exists.

Fresh GitHub PR heads verified and fetched into refs/port-assessment/prNUMBER:

| PR | Verified published head | Final disposition |
| --- | --- | --- |
| 997 | e2736f0f37e614218ed7da7ff4040cf1697431c3 | Pending assessment/integration gates |
| 1003 | 9666ec48e18cb2f7a77c188dc52f093770859f26 | Pending assessment/integration gates |
| 1019 | b6cb0e829011ab08d98d64eaefbc0085c34a0284 | Pending assessment/integration gates |
| 1020 | ce63d517a55f2981b7ccf6e150d1b9050fb5f953 | Pending assessment/integration gates |
| 1026 | 4101e75a3b915ce8b0217cd356b9837339c54ee3 | Pending assessment/integration gates |
| 1027 | 29e59c6413e502b000d00e40836350190db1f3c1 | Pending assessment/integration gates |
| 1030 | fba8e2b50ea0cbc31089789dd27dde628c55fe9d | Pending assessment/integration gates |

Owner-supplied newer local rebases 997=421119d1d and 1003=11bcf23fd must be
located and verified before selecting those final inputs. Initial lookup of
421119d1d in this repository failed; this is a provenance task, not grounds to
silently prefer an older published head. Assessors will inspect local refs and
recovery evidence and report the freshest verified source for every PR.

Mature-region and foreign-stack workers retain current source ownership.
Read-only roll-up assessment can proceed concurrently; conflicting integration
will follow their coherent commits. Historical source/type-cap exclusions remain
source facts, but the seven Wago PR exclusions no longer apply. This order/log
entry changes documentation only and cannot affect runtime performance.


## 2026-11-01 — Owner order: propagate Slipstream round-9 findings

Source report: Slipstream `hunt/daybreak-slipstream-r9`, commit
`dcb45b56386327a1f71fb039ed02e13562e1e2d7`, `hunt-findings.md`, reviewed against
source master `82d546679bab4887acfab056f6ad773a969b1e03`. The report verifies
six Slipstream findings; it does not establish that Wago has the same defects.
Fresh Wago main comparison pin is `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`;
initial committed port comparison pin is `ca30843801956bae67d5cd1219f66d191518c125`.
Active rooting/collector edits must be assessed again after their commits.

Each finding must receive one evidence-backed final disposition: INCLUDED-FIX
(fix integrated into this branch/big PR and gates green), PRESENT-OUT-OF-SCOPE
(present in Wago outside port territory, evidence for Joshua, no separate PR),
or ABSENT (concrete file evidence). Assess both current main and port, including
patterns newly introduced by future rollups. Do not manufacture a final bucket
before assessment/fix gates complete. Mirror the source fix wave's correct shape
when it becomes available; the current hunt report itself makes no fixes.

| Finding | Priority and scope | Final disposition |
| --- | --- | --- |
| H2-F1 | HIGH; pre-GC table/global-only providers lack persistent root registration; first priority | Pending main/port audit and gates |
| H6-F3 | HIGH; failed-start rollback misses nested escaping function references; first priority | Pending main/port audit and gates |
| H4-F1 | HIGH; standards-only Core 3 Threads profile omission | Pending main/port audit and gates |
| H6-F1 | MEDIUM; managed-reference module without type section skips collector setup | Pending main/port audit and gates |
| H6-F2 | MEDIUM; immutable-global none-to-defined-heap subtype rejection | Pending main/port audit and gates |
| H1-F1 | LOW; truncated futex generation aliases lose phase/teardown wake | Pending main/port and upcoming parallel-rollup audit |

Two bounded audit workers own distinct new evidence/probes; active integrators
retain production ownership. Priority lifecycle findings are assessed before any
assumption that existing rooting work covers them. The helper-ticket finding is
also a mandatory design check for the forthcoming PR1020/Snap parallel adaptation.
All included fixes join the same combined perfection/review gate. No separate PR
or existing PR closure is authorized by this propagation work. Documentation-only
recording cannot affect shipped performance; no measurement occurred.

## Roll-up and R9 checkpoint — 2026-11-01

Collector #1030 (9bfe98360) and #1019 (e23657e6a) passed their scoped three-mode
and race gates. Descriptor #1003 plus Tiny prerequisite #1022 are integrated at
a3f26602d, with three-mode collector suites green and expanded region extent
fixtures green. #1003 uses verified published 9666ec48e; supplied 11bcf23fd is
still unavailable. All integrated tabled PR dispositions remain pending final
combined gates; no PR has been closed. Evidence: rollup-1003-1022/README.md
reconciles the 32 R9 evidence files accidentally included in the 1019 commit.

R9 H4-F1: PRESENT-OUT-OF-SCOPE. Core3 omits Threads on both main and port;
explicit Threads succeeds. Validator feature policy is outside this GC port,
recorded for Joshua, no separate PR. R9 H6-F1: ABSENT for the exact no-type
null-global trigger on both tested pins; setup uses metadata, and null handling
works without a type section. H1-F1: ABSENT on tested serial main/port and
published/tuned #1020 protocol pins: channel/Cond predicates and joins do not
wait on truncated tickets. Mandatory recheck after Snap parallel adaptation.
See r9-features-protocol/README.md for pins, tests and file evidence.

H6-F2 remains in scope and pending the rooting worker: bottom subtyping is
correct but null-only immutable import domain admission needs a narrow repair.
H2-F1 and H6-F3 remain unresolved pending bounded ownership-state qualification;
no final ABSENT or INCLUDED-FIX claim. Rooting commits b46fefb2a, 866e6b5e7,
4ad440e86 and 27690e182 await their final matrix and H6-F2 integration. Full
parallel parity, optional evacuation, TinyGo final qualification, measurements,
platform execution and final four-reviewer fan remain open.

## R9 priority ownership gate — 2026-11-01

Evidence d7e6f5dfc confirms H6-F3 on main90f2b57da and updated port45c83820f
in ordinary, wagodebug and wago_regalloccheck: a struct/array nested function
edge survives failed initialization but its producer physical owner is released.
The bounded assertion stops at the ownership failure; no escaped call, memory
inspection or crash reproduction was performed. This collector-integration
finding is IN SCOPE under Joshua's priority directive, despite the auditor's
conditional out-of-scope recommendation. Repair and INCLUDED-FIX disposition
remain pending implementation and green final gates. Final qualification blocks.

H2-F1 remains unresolved: ordinary extern-token retention passes; compact-anyref
imports reject before publication. Neither settles converted managed externref
root ownership. A separate bounded conversion/root-lease assessment is needed.
See r9-lifecycle-ownership-tests/README.md for exact pins, failed assertions,
sibling cross-check, repair proposal and unverified RV64 coverage.

## Rooting stage gate and ownership coordination — 2026-11-01

Rooting evidence5019bffa5164d7c6edb760e1ca9f4f7fafef8886 qualifies production
45c83820fbc10ee31eaaa8437e12464955b2fb9d: focused guest/backend/runtime and
diagnostic gates PASS ordinary, wagodebug and wago_regalloccheck. All3942
archived src blobs matched the pin. ARM64/RV64 cross-compilation passes;
actual execution remains open. Existing skips are enumerated in
rollup-rooting-integration/README.md. PR997 uses provisional verifiede2736f0f3;
missing421119d1d is not claimed equivalent. PR997/1026/1027 and R9 H6-F2 are
integrated with scoped gates green, but final INCLUDED/INCLUDED-FIX awaits
combined gates and review. No tabled PR closed, no measurement started.

H6-F3 worker owns portable object_edges/object_trace/reclaim APIs and corresponding
RV64/other variants/tests in gc/native. Parallel worker retains collect.go,
collector.go, mark.go and native_view.go; required cross-file changes must be
coordinated. Callback/conditional-root concurrency and post-join exactly-once
reclaim notifications are explicit joint gates. Preserve RV64 dependency receipt
semantics rather than blindly enabling them on host. Slipstream staged e1ef65d6b
conservative failed-runtime retention does not prove cycle/eventual-release
requirements; exact host graph remains required.

## H2-F1 confirmed converted-reference ownership gap — 2026-11-01

Evidence70359b920a7c4a37b18c8e1a52824037cab51b10 establishes24 bounded
ownership failures on main90f2b57da and port45c83820f across all three modes:
extern.convert_any publishes into an older provider's extern table/global, but
publisher Close drops the checked conversion root and private collector while
the provider survives.24 region cases explicitly skip at admission; they are
not passes. Every failing case stops before further access/collection/teardown.
H2-F1 is confirmed IN SCOPE and blocks final qualification; INCLUDED-FIX remains
pending repair and gates. Lifetime worker now owns this receipt repair alongside
H6-F3, in separate coherent commits. Provider-backed identity/root/domain leases
must survive publisher retirement and release exactly once on final clear;
permanent owner retention is insufficient. See r9-converted-provider/README.md.

Source-wave evidence records Slipstream084be3d4bf (merged1a4759e5c) installing
imported container roots and early runtime registration. Actual H6 fixef06c3710
conservatively retains failed ABI2 runtimes; Wago's exact cycle/eventual-release
obligation remains. Source-specific representations require adaptation.

## TinyGo prerequisite qualification gate — 2026-11-01

Evidence462ada9235547b7242bfc6c1701832af01f270d1 closes the scoped foreign-stack
prerequisite on production57a99d01df0ae2beb5afc7a947bc9e06621b2259. All34 final
commands pass: host and actual TinyGo guest ordinary/debug/regalloccheck,
unchanged cancellation, deterministic forced-GC transition/host-root boundaries,
Close-failure cleanup, and fresh-cache Linux amd64 CI/tasks/CLI/release smoke.
No suppression/retry waiver or new skip. Implementation5bfd18174, template
placement5a173c801, cleanup correction57a99d01d. Evidence and reproduction:
tinygo-foreign-stack-fix/README.md and qualify.sh. ARM64 cross-compiles only;
ARM64/Darwin execution and combined final source remain unqualified. This closes
the earlier prerequisite diagnosis gate, not aggregate final-head qualification.
Independent measurement remains pending global lease and worker drain; complete
registered-stack scans and idle-engine conservative retention are explicit costs.

Stage6 parallel evidenceabcf7f58a qualifies productionbc05eef3e/tests779fb3f70:
all12 final collector/race/backend/guest commands pass all3 modes, with additional
callback/failure/target checks. Source callbacks serialize and reclamation occurs
once after join. R9H1-F1 ABSENT on this pinned channel/fresh-phase/join protocol;
recheck after stage7. Stage7 worker owns remaining public controls, bounded run
reservation, pressure policy and optional mature evacuation. Final INCLUDED
dispositions, H2/H6 repairs, full suites/corpus/conformance/platform execution,
measurements and consolidated review remain open.

## Owner checkpoint: immutable H6 gate before H2 production

Requested pin1059c88e8e97840c121b3e4bc4e9f44b4b0c9a0b. At checkpoint,
r9-nested-owner-fix/qualification-provenance.txt names that pin but the current
qualification-results.txt is empty; qualified-full-ordinary.log is unfinished.
Earlier backend log contains a mise trust harness failure from a different
archive. No final three-mode/full/race/backend/ARM64/RV64 cross PASS can be
attributed to the restarted run yet. All five suite categories remain PENDING
current authoritative results. Cross-builds will not count as target execution.
Owner's latest condition gates further H2 production on all requested H6 suites
green. Worker notified;6a25f86e5 is an already committed converted-replacement
validation prerequisite, not completed H2 and not covered by1059 qualification.

Seven-PR roll-up progress (final buckets require combined gates):
-997: integrated4ad440e86, provisional sourcee2736f0f3; missing421119d1d unresolved.
-1003: extent topic integrateda3f26602d from9666ec48e; missing11bcf23fd unresolved.
-1019: integratede23657e6a, scoped all3/race green.
-1020: native integrationbc05eef3e and tests779fb3f70 scoped green; stage7
97cb56269/e0d3abddd and followups under qualification.
-1026: reconciled4ad440e86; rooting matrix45c83820f scoped all3 green.
-1027: integrated866e6b5e7 plus distinct EH repairs27690e182; same scoped gate.
-1030: integrated9bfe98360; scoped all3/race green.
All seven target INCLUDED but none has final INCLUDED status before combined
gates/review. No SUPERSEDED or INCOMPATIBLE conclusion is currently established;
no PR closed. H2/H6, final collector/full/corpus/conformance/platform/TinyGo
joint qualification, measurement and consolidated review remain open.

## Immutable1059 requested suite gate PASS

Verified committed evidencec592d2dec at r9-nested-owner-fix/attempt-1059-clean-driver:
source1059c88e8e97840c121b3e4bc4e9f44b4b0c9a0b, all15 requested commands exit0
and unchanged driver exit0. Full src/wago with pinned corpus, scoped ownership/
lifecycle race, full AMD64/ARM64/Wasm backend packages, ARM64 cross-build and
RV64 cross-build each PASS ordinary/wagodebug/wago_regalloccheck. Cross checks
are compile-only; race scope is the recorded regex, not whole-suite race.
Historical failed attempts are retained; this clean replay supersedes neither
their recorded failures nor the separate6a25f86e5 transaction qualification.

H6 contention wakeup remains an open finding independent of these passes.
Worker authorized narrow host retry driver and deterministic contention/Close/
coalescing/no-lost-wake tests, then immutable qualification. H2 production remains
paused until that gate closes. Seven roll-up PRs retain integrated/pending-final
status; no final INCLUDED/INCLUDED-FIX or combined stage7/TinyGo/platform claim.

## Stage7 and actual TinyGo evacuation gates

Stage7 standard-Go collector qualification PASS: production75e438784,
tests831200ae6, evidence0fbc269b2;32 commands across all3 modes including full
collector/race, focused guest/backend/CLI, capacity/credit/identity and target
checks. ARM64/RV64 are compile-only. Bounded run/pressure/mature relocation and
public controls implemented; R9H1-F1 protocol checks pass. See stage7-evacuation.

Actual TinyGo serial evacuation PASS at9c26df2d4b35c89fd3cf6e591f3b033370a50a07,
productioncff52eaa6, evidence20a4f0a85efba65f7ac0dda66063cbf334a3d4c0. All11
expanded commands pass using real patched0.42 on Linux amd64 with threads
scheduler/conservative GC. Wago collector is serial; helpers remain rejected.
Ordinary/debug/regalloc TinyGo collector counts15/30/15, joint guest+foreign
stack9/9/9, zero target skips/failures; full Go collector2225/2307/2225 passes
with0/3/0 existing skips. All9122 export members unchanged. Focused joint smoke
is not the complete34-command foreign-stack lane or H6/H2 qualification.
TinyGo host sidecar/receipt APIs remain build-excluded and are not counted passed.

H6 source86cacffeb failed its ordinary scoped race retirement check; preserved
FAIL. Correction631cb7049 has fresh18-command qualification underway, no final
verdict yet. H2 remains paused. Seven tabled fixes retain integrated/pending-final
buckets; all combined-head/platform/corpus/conformance, measurements and final
four-reviewer gates remain open. No PR publication, closure or timings.

## H6 wakeup gate PASS; H2 production authorized

Verified committed30ad134bf48f94a3a64c7e0700d7fbf9e269c813: immutable source
631cb70499e3fa61357171a0e3d1a60e7a0883fa,18/18 commands exit0, driver0 and
unchanged hash35961ef71a6c2bebc253b8c152f110f792912e33f86ee3c4fec2d8a1937689d6.
Focused ownership/full Wago/scoped race/backend/ARM64 cross/RV64 cross each PASS
in ordinary, wagodebug and wago_regalloccheck. Cross-only, no target execution.
Failed86cacffeb retirement race remains recorded FAIL;631cb topology membership
proof fixes it. This closes the prerequisite pause, not final INCLUDED-FIX.

Parent explicitly authorizes H2 production now: before-publication converted
identity/root/domain receipts attached to actual surviving containers, exact
replacement/clear/bulk balance, publisher-independent collector lifetime, and
conditional participation in H6 joint reachability. Converted aggregate/producer/
provider cycles must release after final external edge without permanent roots.
Use separate coherent commits and immutable all3/race/target gates, retain prior
failed probes and honest supported-region admission. Final H6/H2 integration,
Stage7/TinyGo/QEMU combined-head and full conformance/corpus/performance/review
gates remain open; seven tabled PRs remain integrated/pending final buckets.

## Astra coordination and store-hook checkpoint — 2026-11-02

Assessed port35c36bc594d2a027317cfcd321379f2d40c6b219, production501e9ab46.
Receipt core79db49daa and module-owned store hooks are implemented; integrated
all3 suites, ARM64QEMU host tests/RV64 controls and supportedTinyGo smoke pass
within r9-h2-store-hooks documented scope. Original surviving-provider and
conditional-cycle gates pass. Final H2 scope/combined gates remain open.

Integration owner retains sole implementation authority. Support Astra
mcp:d176e968-4193-4ac3-8dec-d61fffb20e95 is advisory/docs-only on
docs/gc-port-astra-support. Hook worker64e91191 is completed, files released,
no unfinished edits. Active combinedqualification10ea28d4 owns evidence only.
No new parallel implementation assigned before supportmap is reconciled/adopted.

Source confirms host-created converted containers lack Instance-owned adapters
and remain rejected; support assessment agrees. migrate has only test callers;
current growth uses reserved capacity and does not relocate. Lack of relocation
caller is not itself a missing production path: do not invent allocator changes
solely to create a parallel slice. Any future relocating caller must use migration
contract. Await exact support definitions; final buckets/adoptions/disagreements
and reasons belong to integration owner. Seven PRs remain pending final gates.

## Support documents adopted and parallel ownership assigned — 2026-11-02

Received advisory docs throughd01193139 and integrated all six docs commits.
Assessed source35c36bc59/production501e9ab46 unchanged by these documentation
updates. Verified prior hook worker released all files, current source clean,
and exact15-pathA/24-pathB sets have empty intersection. Only other active worker
is combinedqualification10ea28d4 with separate evidence ownership.

ADOPT A host-container authority/public adapters and B guest imports/bulk map
exact allowlists/newtest/evidencedirs in h2-parallel-slices.md. ADOPT nilInstance
meaning authenticated host physical owner resolved via current store/storage;
never fabricate publisherInstance. Freeze typed signatures/helperIDs/arities and
opaque representation; any required APIchange returns to parent for serialized
prerequisite. ADOPT h2-qualification-matrix.md and realhostroot lifecycle. Develop
inparallel; A commits first, B waits for parent's A-landing notification before
committing, then combined immutable acceptance. No cross-allowlist edits.

Adopt all seven advisory INCLUDED scope recommendations, but REJECT interpreting
them as final INCLUDED acceptance: owner's bucket requires combined gatesgreen,
reviewzero unresolved; missing997421119d1d/100311bcf23fd provenance remains.
Migration adapter stays regression/futureallocator obligation; no new production
relocation requested because currentgrowthreservedcapacitydoesnotrelocate.
