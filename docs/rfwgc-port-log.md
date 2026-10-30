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
