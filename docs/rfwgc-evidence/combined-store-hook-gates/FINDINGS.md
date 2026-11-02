# Blocking findings at immutable 501e9ab46

Exact source: `501e9ab46c4f68090c7bbeb17a6ad14f4fadda6d`.
No production file or test expectation was changed. All line references below
refer to that commit, not concurrent worktree edits.

## 1. Reference-store publication races with the lifetime-boundary hook

**FAIL, verified by the Go race detector.** Full ordinary race command exited 1.
`race-ordinary/command.json` is the exact executed command/environment;
`race-ordinary/raw.log.gz` is complete output, and `race-finding.txt` extracts the
bounded failing case `TestPreparedGCRevocableEntryExternalAccess/reference-store`.

- Writer: `src/wago/reference_store.go:4157`, `referenceStoreForBoundary`, assigns
  `in.refStore` while holding `in.lifeMu`; called through `NewExternRef`.
- Reader: `src/wago/function_environment_platform_host.go:27`,
  `managedFunctionAfterLifetimeBoundary`, evaluates `in.refStore` without that
  lock before calling `managedFunctionGraphs.Load`.
- Invocation tail: `src/wago/instance_lifecycle.go:312` defers that reader;
  private-entry teardown reaches it through `instance_admission.go:123` and
  `prepared_gc.go:218`.
- Existing test: `src/wago/prepared_gc_native_test.go:489` publishes a reference
  from another goroutine; line 508 releases the private invocation lease.

`sync.Map.Load` does not synchronize evaluation of its argument. The detector
reports conflicting accesses to the Instance field, not an internal sync.Map
race. This is a concurrency defect; no UAF, corrupted payload, escaped call or
post-failure stress reproduction is claimed. The reader line is attributed by
Git blame to `9573c62491`; this is provenance, not an exhaustive introduction bisect.

The executed full command was Go 1.27.1, GOMAXPROCS=2, GOFLAGS=-p=2:

```sh
go test -json -count=1 -timeout=60m -failfast -race \
  ./src/core/runtime/gc/... ./src/core/runtime ./src/wago ./tests/integration/...
```

A narrow owner reproduction selector is
`-race -count=1 -failfast -run '^TestPreparedGCRevocableEntryExternalAccess$/^reference-store$' ./src/wago`.
That reduced command is suggested, not claimed executed. The existing test and
full recorded stack are sufficient evidence; this worker did not repeat the
unsafe case after detection. Race/debug/guard follow-through was not launched.
Go's failfast run still emitted ordinary package Examples after the failing test;
no additional explicit escaped-reference access was introduced by this worker.

## 2. Explicit RFW catalog gate violates a checked deferred-load invariant

**FAIL, reproduced by compile-only baseline control; not established as a new
port regression.** `TestRFWStage3CorpusCompile/clang-cpp-kernels` fails with:

```text
function 32917: internal compiler error in function 32944 at wasm offset 0x404:
regalloccheck: function 32944 pc 1028: deferred load address register 5
 overwritten at code offset 3036 (load size 8, displacement 0, borrowed local -1)
```

The gate is `src/wago/rfwgc_stage3_measurement_bench_test.go:177`; its
`rfw3Config` selects Core V3, disables inlining and enables `gc-lifetime-analyze`.
The checker is `src/core/compiler/backend/railshot/amd64/regalloc_check_enabled.go:420`.
Owning implementation area: Railshot AMD64 deferred memory operand/register
lifetime tracking. The precise overwrite emission site has not been isolated;
no speculative production fix is included.

Exact combined command and error: `graphs-and-explicit-catalog-port-wago_regalloccheck/`.
The current command includes the generated graph controls and all RFW catalog
entries, with `RFW_CATALOG` explicitly set to the pinned catalog. It stopped at
the first checked failure. There was no invocation of the rejected module.
The canonical bench corpus uses its own configuration and is a separate gate;
its ordinary/debug passes do not waive this explicit RFW failure.

After stopping current-source execution, a compile-only control on immutable
pre-region `a3c08365f531eb3c9003e3acf3e0a822c12c31f4`, with the identical test-only
harness overlaid, reproduced the same function/PC/register/load/displacement
failure (code offset 2936). See `baseline-compile-adapter.json` and
`baseline-checked-clang-cpp/`. No production overlay and no guest execution.
The compiler's rejection is verified; a runtime miscompile is not asserted.

Reproduction selector:

```sh
RFW_CATALOG="$SOURCE/corpus/catalog.json" go test -json -count=1 \
  -timeout=60m -tags=wago_regalloccheck -failfast \
  -run '^TestRFWStage3CorpusCompile$/^clang-cpp-kernels$' ./src/wago
```

## 3. Known default-route compiler diagnostics remain FAIL

The complete ordinary `wago_codegenstats` default-route suite exits 1 with the
same 24 top-level counter/admission failures recorded in historical
`tests-stats.txt`. Both interval tests explicitly reproduce on current source
and the pre-region baseline in all three build modes plus `wago_codegenstats`.
`TestIntervalRegionDynamicReuse` reports `interval-region = 0, want 1`;
`TestIntervalRegionControlBoundaries/if-else` reports no control-region activation.
`TestCommuteSelfUpdate` also reports zero expected activations after its guest
result assertions pass. Owners include `amd64/interval_region_test.go:140,214`,
`amd64/commute_self_update_test.go:58`, and the default routing switch in
`amd64/scalar.go:12`.

The full default ordinary run is `default-diagnostic-complete-wago_codegenstats`.
`historical-diagnostic-failures.json` records exact old assertion text and log
hashes. The monitor permits only those already identified non-safety assertion
messages to finish reporting; it never changes their FAIL status. An initial
conservative attempt stopped on CommuteSelfUpdate and is retained separately.
The debug full run was interrupted at the later unsafe qualification stop;
checked full default diagnostics were not started.

CI explicitly sets `WAGO_SHARED_SCALAR=0` for its established-path diagnostic
suite. That complete ordinary configuration passes (`diagnostic-wago_codegenstats-first`).
It does not make the failing default configuration green. No skips or expectation
changes hide the 24 failures. Other two CI diagnostic configurations remain unrun.

## 4. Staticcheck reviewed-baseline gate remains FAIL

After fixing tool discovery, `staticcheck-final` reports 24 findings across its
three profiles: eight distinct source locations, not 24 independent defects.
No baseline update or source suppression was made.

| Source location | Finding |
| --- | --- |
| gc/native/mature_review_stage5_test.go:35,38 | S1016 struct conversion |
| wago/function_environment_dependencies_host.go:95 | U1000 unused proof type |
| wago/function_environment_linux.go:155 | U1000 unused exception field |
| wago/function_environment_mark_linux.go:51 | U1000 unused receipts field |
| wago/function_environment_platform_host.go:15 | U1000 unused gate-owner helper |
| wago/function_environment_platform_host.go:114 | U1000 unused exception scratch type |
| wago/gc_frame_roots_linux_amd64.go:324 | U1000 unused conservative-mask helper |

All paths are relative to `src/core/runtime/` for the first row and `src/` for
other rows. These are lint-gate failures, not independently proven runtime defects.

## Read-only sibling cross-check and disposition

Before triage, inspected Slipstream Git objects at
`dcb45b56386327a1f71fb039ed02e13562e1e2d7` and Starshine at
`1d6c0af47140299d8f3c01ed03060d279c43ee03`.
`sibling-defect-crosscheck.txt`, `sibling-readonly.json`, and the sibling excerpts
retain commands/pins and source evidence. Slipstream uses native recursive Store
mutator owner/depth/waiters and a distinct compiler representation; the exact
Wago lazy Instance field hook and Railshot checker have no exact counterpart in
that scoped search. Starshine's inspected GC boundary is model/codec/validation/
generator support. No sibling execution, mutation, or blanket absence claim.

The combined pin is **BLOCKED**. Parent/runtime owner must address the race;
compiler owner must resolve or explicitly triage the checked compile failure;
known diagnostic and Staticcheck failures remain open. Remaining execution was
stopped and recorded as INTERRUPTED/NOT_RUN, never PASS. This report cannot close
host-created containers, excluded targets, future-head qualification, or review.
