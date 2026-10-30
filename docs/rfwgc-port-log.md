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
