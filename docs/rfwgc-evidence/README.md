# Analyze checkpoint verification

Environment: Linux AMD64, Go 1.27.1. These are correctness/compilation outputs, not benchmark timings. Trailing whitespace in captured output is normalized.

| Output | Command |
| --- | --- |
| tests-default.txt | `go test ./src/core/compiler/backend/railshot/amd64 ./src/core/compiler/optimization ./cli/internal/settings ./cli/runtime/commands/run -count=1` |
| tests-focused-stats.txt | `go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^TestGCLifetime' -count=1` |
| tests-race.txt | `go test -race -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^TestGCLifetime' -count=1` |
| tests-runtime.txt | `go test ./src/wago -run 'Test(ConfigOptimizationSelectionIsImmutableAndValidated\|DeadGC\|GCConstructor\|GCStructReduction)' -count=1` (shell regex uses ordinary `|`, table escapes are formatting only) |
| tests-stats.txt | `go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 ./src/core/compiler/optimization -count=1` — fails; see baseline comparison |
| baseline-fixture.txt | In isolated `git archive 9713878ab` export with only `gc_lifetime_bench_test.go` copied in: `go test ./src/core/compiler/backend/railshot/amd64 -run '^$' -count=1` |
| baseline-stats.txt | Same export: `go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -count=1` — fails |

All 24 top-level failing tests in tests-stats.txt also fail on the base. The base additionally fails TestSharedEstablishedPathPairs; this is not evidence of a fix here. No engine fixes made. The passing race run includes the complete new test family. Full tagged suite outputs predate only later diagnostic offset reporting/additional focused tests, with no emitted-code changes.

Temporary baseline exports were created with Python TemporaryDirectory and extracted via `git archive`; they were removed on exit. To avoid mise trust/toolchain mismatches outside this checkout, that check used `/home/dev/.local/share/mise/installs/go/1.27.1/bin/go` with `GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1` and `GOTOOLCHAIN=local`. No benchmarks ran. See the plan for the portable benchmark entrypoint, baseline setup and matched measurement requirements.

## Pinned first-slice compile benchmark

`benchmark/run.py` exports both exact commits with `git archive`, copies only the
committed self-contained `gc_lifetime_bench_test.go` into baseline, builds test
executables outside the checkout, runs focused original tests plus a temporary
32-function proof diagnostic on the port, and serializes timing. Requirements:
Python 3.12+, Linux `/proc`, `taskset`, and Go (override `--go-root` as needed).
No worktree/checkout changes, implementation edits, diagnostic build tags or guest
execution. Temporary exports and binaries are removed on exit; raw text remains.

From repository root, to a **fresh output directory**:

```sh
python3 docs/rfwgc-evidence/benchmark/run.py \
  --go-root /home/dev/.local/share/mise/installs/go/1.27.1 \
  --cpu 6 --repeats 10 --benchtime 1s \
  --out /tmp/wago-lifetime-benchmark-results
python3 docs/rfwgc-evidence/benchmark/summarize.py \
  /tmp/wago-lifetime-benchmark-results
```

The final full matrix is `benchmark/raw/`: **contention-qualified exploratory
measurement**, per owner's direction after initial quiet-host admission stalled.
No sample is rejected/retried because of load in this matrix. Pinning does not
reserve a physical core or isolate caches, memory, SMT or package power/thermals.
Other workloads were never killed/repinned. `during-timing.jsonl` records UTC
bounds and roughly one-second instantaneous external CPU samples above 0.25 CPU,
plus loadavg, during every calibration/measurement invocation. Short-lived
processes and sub-threshold activity may escape this sampling. `processes-*.txt`
captures round-end processes; `host.json` records toolchain/host/pinning metadata
and the harness SHA-256. Each measured file records its exact command (temporary
binary paths vary). Only trailing whitespace is normalized in text logs.

The nine cells rotate each round, with ten independent invocations per cell.
`ns/op`, `B/op`, and `allocs/op` describe **compiling a 32-function module**,
including cleanup, with decoding/validation outside timing. They do not measure
guest execution or guest heap allocations. Findings/table are in
`../rfwgc-port-log.md`; known-red broad-suite evidence above still applies.

`benchmark/quiet-admission-attempt/` preserves the **separate, incomplete prior
attempt** (two accepted samples, one rejected sample and CPU admission logs).
None of those timings enter the final table. `quiet-admission-run.py` is its
archived script, retained for audit only, not the completion recipe. The initial
pre-monitor wait is `preparation-contention.txt` within that attempt. Owner
explicitly superseded quiet admission with the fresh full exploratory matrix.

`baseline-qualification/` is parent-owned; this benchmark does not modify it.
Owner reports the separate qualification gap: missing pinned Release 3
interpreter; bootstrap failed for missing dune. Installed WABT 1.0.42 mismatched
pinned 1.0.41; after verified 1.0.41, 18 staged failures needed the interpreter.
This is owner-provided context, not a benchmark result or a claim of broad-suite
qualification here.

## Final orchestrator qualification

See `baseline-qualification/README.md` for GAP-CONFORMANCE-TOOLCHAIN and the isolated baseline's exact missing interpreter/`dune` evidence. `final-available-tests.txt` retains the constrained parent root run with `^TestStaged` explicitly skipped (134 packages passed; audit failure and two timeouts). `final-policy-tests.txt`, `final-runtime-recheck.txt`, and `final-standalone-recheck.txt` retain passing targeted follow-ups after the test-only audit scoping and restoration of normal parallelism. Exact commands, limitations and disposition are in the port log. No all-green full-conformance claim is made.
