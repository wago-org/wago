# Stage 3 full historical corpus correctness execution

This worker owns only this directory. Production pins are pre-region
`a3c08365f531eb3c9003e3acf3e0a822c12c31f4` and Stage 3
`5b31c81ec0c74695a90bc8d36754b92845b59f14`. Execution uses fresh `git archive`
exports, no source overlays, separate local build cache, Go 1.27.1 on Linux
amd64, and two-way Go build parallelism (`GOMAXPROCS=2`, `GOFLAGS=-p=2`).
Starting shared branch head was `94846b133a4c851ec5110d688453c3e9a2bf466f`.
The evidence commit is identified in the final worker handoff.

## Result

**PASS for this pinned historical-corpus gate.** All six commands exited zero.
All 768 ID/revision/mode records pass their required compile and runtime/oracle
checks; all 384 paired ID/mode joins are baseline-matched passes. There are zero
regressions, baseline failures, missing workload outcomes, or skipped Wago
workloads. Every run contains 828 passing terminal subtests (including parent
subtests) and 12 reference-only skips. Fresh wazero comparisons pass for 51
commands in each run: 306 passes total. The remaining 72 reference skips are the
same 12 explicitly designated captured-reference commands in all six runs.

No verified engine failure arose, so there is no defect-specific sibling triage
or engine fix. This closes execution/reconciliation of the requested historical
corpus pins only, not the full port, region-enabled graph gate, final-head gate,
performance gate or mandatory review fan.

Export verification checked 7,898 baseline and 7,930 candidate regular files
against fresh git archives after execution: zero byte differences. The two dirty
Stage 2 scripts retained their recorded hashes. The shared port log changed
concurrently while this worker ran and was not touched or staged by this worker;
start/end hashes retain that distinction. Conformance submodules were untouched.
The shared branch advanced concurrently; runtime qualification remains pinned to
the two explicit exports, not the eventual evidence commit's production tree.

After execution, the local driver was hardened to propagate recorded child
failure exits to its caller. This affects only failure reporting, not commands
or test selection; all six recorded child exits were already zero. Python files
were syntax-checked. The full test matrix was not repeated for that reporting-only
change. Temporary owned exports/cache were removed after verification; all raw
logs, command records, catalog, hashes and per-ID joins are retained.

## Reproduction and limits

Run from the existing experiment/rfwgc-port worktree:

```
python3 docs/rfwgc-evidence/stage3-corpus-execution/run.py
python3 docs/rfwgc-evidence/stage3-corpus-execution/summarize.py
python3 docs/rfwgc-evidence/stage3-corpus-execution/audit-types.py docs/rfwgc-evidence/stage3-corpus-execution/.exports/baseline > docs/rfwgc-evidence/stage3-corpus-execution/baseline-type-inventory.json
python3 docs/rfwgc-evidence/stage3-corpus-execution/audit-types.py docs/rfwgc-evidence/stage3-corpus-execution/.exports/port > docs/rfwgc-evidence/stage3-corpus-execution/port-type-inventory.json
python3 docs/rfwgc-evidence/stage3-corpus-execution/verify-exports.py
```

`run.py` requires a fresh `.exports` directory; it refuses to overwrite existing
exports. The source archives and build cache are disposable and excluded from
the evidence commit. `*.command.json` records each exact command, working
directory and overridden environment; `identity.json` pins source commits,
compiler, catalog, every binary and all 47 declared input references. Raw Go JSON
and stderr are retained without filtering. No benchmark flag, measurement runner,
lease acquisition, temperature sampling or performance comparison is used.
Go test timestamps/durations and canonical runner duration bookkeeping are
incidental correctness-run output, not acquired performance evidence. No
application duration report is requested, and the join ignores durations.

The canonical prepared `stage3-measurement/corpus-gate.py` was adapted within this
owned directory to create fresh exports and use `WAGO_APP_CORPUS_REFERENCE=all`.
Its exact three selectors are TestCorpus, TestCorpusSemanticExec and
TestApplicationCorpusRuns, with `-wago.corpus=all`, `-count=1`, and `-timeout=60m`.
Each TestCorpus child is limited to one minute (`WAGO_CORPUS_TIMEOUT=1m`), the
canonical maximum, to accommodate concurrent correctness workers. Semantic cases
retain 15-second per-invocation catalog limits (30 seconds for the three
Blake3 checks), fresh/second-instance checks, and 40 same-instance repetitions. Command cases
retain canonical adapters and the whole-suite deadline; no per-command timeout
is added. Test execution is serial across revisions and build modes.

The default/off runtime policy is intentional: this historical catalog contains
no WasmGC constructors, and the canonical tests do not enable region allocation.
This checks the changed runtime's ordinary paths and compiler compatibility. It
does not qualify region-enabled graph collection or later implementation heads.
No full root suite, nested suite outside bench/suite, or conformance suite is run.
The other worker's immutable d546a1af full-suite run is not duplicated.

## Denominator and oracle accounting

All 128 catalog IDs remain present for each revision and all three modes:
ordinary, wagodebug and wago_regalloccheck. There are 121 distinct binary hashes
and 47 declared command input references (including repeated references).
The catalog divides into 48 direct-result, 17 semantic, and 63 command workloads.
The 17 semantic workloads map to 21 named semantic checks.

`per-id-revision-mode.json` records each required canonical compile/decode/validate/
instantiate stage and runtime oracle status, including NOT_RUN/skip/fail rather
than treating absent events as passes. `revision-joins.json` compares exact
per-ID checks in matching modes. `summary.json` retains package exits, terminal
counts, failures and complete skip reasons. A matched failure is not a pass.

All IDs require Compile. CompileFull is required for 127 IDs; the catalog
explicitly excludes that standalone stage for micropython-squares, which instead
compiles through its custom command adapter. Direct execution checks exact
result words. Semantic execution checks pinned scalar/memory/vector oracles,
fresh/second instances, and same-instance repetitions. Command execution checks
catalog output contracts (stdout hashes, self-checks and declared output files)
with exact input validation. All 63 commands are eligible on Linux amd64.

Independent references use the canonical wazero 1.9.0 adapter where available.
Twelve command IDs explicitly designate captured V8 (8) or wasmtime (4) oracles;
their wazero subtests skip with the canonical explanation. Those pinned oracle
contracts are still checked by Wago, but fresh V8/wasmtime executions are not
claimed. Direct and semantic exact catalog oracles are not fresh cross-engine
comparisons. No unsupported reference is silently counted as an executed pass.

## Separate generated graph facts

`baseline-type-inventory.json` and `port-type-inventory.json` are fresh read-only
plain-function-type audits of the immutable exports, adapted from
stage2-corpus-orchestrator/inventory.py. They reject non-function composite types
rather than guessing. `historical-type-inventory.json` preserves the prior audit
for comparison. The historical 121 binaries have zero struct/array types and
zero WasmGC constructors; this corpus is not evidence of GC graph coverage.

`graph-manifest.json` is copied verbatim from the prepared measurement evidence;
these are separate synthetic fixtures, not additional historical IDs or fresh
execution results from this worker. It describes three distinct binaries:
graph (357 bytes, 32 constructor sites, 32 reachable nodes after 64 calls), scalar
(54 bytes, one constructor site, zero live objects), and noGC (47 bytes, zero
constructor sites). Each has independently specified return 7. Only graph is a
graph-producing binary. See the original stage3-measurement validation.json and
raw fixture validation logs for previous ordinary/wagodebug/wago_regalloccheck
baseline and region Off/On qualification. Cycles, dynamic arrays, host aliases,
reentry and EH/tails are not covered by that narrow synthetic manifest.

## Source boundary and performance

`landed-source-read.txt` records only immutable Slipstream git objects. Both
ccc6512de (expanded demand-paged old space and derived metadata layout) and
bb0900efa (fail-closed diagnostics) are ancestors of supplied master tip
82d546679bab4887acfab056f6ad773a969b1e03. The tip itself is documentation.
Neither parked type-cap implementation 059d10f17 nor report 01303964b is in that
ancestry. No sibling source, parked fix or tabled Wago PR is imported.

These scripts, JSON and Markdown are evidence only and are not consumed by
production compiler/runtime builds, so this commit cannot affect shipped
performance. Independent measurement of the production port remains required
under the global lease after correctness workers drain. This worker neither
acquires the lease nor makes a performance claim. Full-port implementation,
final-head qualification and the mandatory three-Daybreak-plus-Claude review fan
remain the parent's gates.
