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
