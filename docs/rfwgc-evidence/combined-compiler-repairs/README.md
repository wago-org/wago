# Compiler gate repairs

**PASS for all 18 requested compiler-lane commands** on immutable source
`bbe9ae5201e9d43907146e6af9262e8a2a0216d4`. This report does not qualify later
branch heads or close the parent's combined gate, platform, host-container,
lint, or performance obligations.

| Gate | Ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Explicit RFW catalog compile, original configuration | PASS, 128 IDs | PASS, 128 IDs | PASS, 128 IDs |
| Focused regressions | PASS | PASS | PASS |
| Full `./src/core/compiler/backend/...` | PASS | PASS | PASS |
| Full backend diagnostics, default shared route | PASS | PASS | PASS |
| Full backend diagnostics, `WAGO_SHARED_SCALAR=0` | PASS | PASS | PASS |
| Canonical corpus/application/reference execution | PASS, 128 IDs | PASS, 128 IDs | PASS, 128 IDs |

`qualification.json` and `gates/ledger.json.gz` retain exact terminal events and
skip accounting. `catalog-per-id.json` has 384/384 passing compile rows;
`corpus-per-id.json` has 384/384 passing execution rows with every required stage,
export, semantic case, and application contract accounted for. Each execution
mode retains 12 captured-reference-only skips: Wago checks the pinned contracts;
those skips do not claim fresh application V8/wasmtime runs. Other suite skips
are separately enumerated in the qualification file.

All 24 repaired diagnostic contracts pass all six mode/route combinations
(144/144 rows in `repaired-diagnostic-verdicts.json`). All 32 existing checked
address-lifetime test terminal events pass unchanged. No remaining blocker was
found in this compiler lane. Qualification used Linux/amd64, Go 1.27.1.
`source-integrity-after.json` verifies all 10,546 candidate export files unchanged;
`export-integrity.json` also compares both exports directly with pinned Git blobs.

## Deferred address repair

Source/test commit: `5c0309145`.

The checked clang-cpp failure is an emission bug, not a checker false positive.
`condenseBinary` checks whether its original right deferred load borrows the
requested destination. Later, `commute-mem-left` can swap a left deferred load
into the right operand. If that load's address is the requested destination,
`condenseInto` overwrites the address with the original right register before
`applyALU` reads memory. The original rejection was function 32944, pc 1028,
register 5, offset 3036, reached from `reconcileMerge1`.

The repair excludes precisely that unsafe memory commute. The existing left
load then reads through the original address into the destination, followed by
the register ALU operation. Other memory commutes and shared scalar admission
remain enabled. No checker code, assertions, or diagnostics were removed.

`TestCommuteMemoryPreservesDestinationAddress` is a deterministic compile-only
emitter fixture for i32 and i64. It checks the actual instruction bytes as well
as the unchanged checked build's address lifetime observer. On immutable parent
`3b04e6837` with only this test overlaid, checked compilation rejects the write;
ordinary/debug emit `4189fc4633642308` instead of `468b6423084131fc` for i32.
The former moves the RHS into R12 before reading through R12. The latter loads
through the original R12 and then XORs the RHS. No rejected module or emitted
baseline code was executed. Complete baseline logs are in `baseline-control/`.
The earlier pre-region `a3c08365f` catalog reproduction remains in the combined
store-hook report and is not used to waive any repaired-source gate.

## Diagnostic route contracts

Test commit: `bbe9ae520`.

All 24 failures in the complete default diagnostic report assert established
Valent/region emitter activation, residency, or instruction selection. They
select that route explicitly using a serial, cleanup-restored test helper.
`compile.go:runBody` directly returns `scalarBody` for admitted shared functions;
the established driver/peephole code is not run on that route. The helper is
called by the individual contract tests, not by compilation helpers or global
suite setup. Production routing and original counter/semantic expectations
remain unchanged. `diagnostic-contracts.json` enumerates the tests and source
sites for their named counters.

`TestScalarRouteRegionAndSelfUpdateContracts` additionally compiles and executes
the same region-pressure and self-update bodies through both routes, asserting
positive shared admission in diagnostic builds and independent expected results
(210 and 0x55). Existing shared/established semantic matrix, independent V8,
shared zero-local, source-observer, and worker-reset coverage remains enabled.
The final gate must pass both default and explicit `WAGO_SHARED_SCALAR=0`
configurations; this is not an environment-only waiver of the default suite.

## Reproduction and boundaries

`qualify.py` is a correctness-only driver. It runs the explicit RFW catalog
compile gate first in checked, ordinary, and debug modes using the original
`rfw3Config`: Core V3, inline disabled, lifetime analysis enabled. It then runs
focused tests, full backend tests, full default/established diagnostics, and
canonical corpus execution. Commands, relevant environment, complete compressed
output, raw-output hashes, and test terminal events are retained per gate.
Go is pinned to `/home/dev/.local/share/mise/installs/go/1.27.1/bin/go`; Node to
`/home/dev/.local/share/mise/installs/node/24.22.2/bin/node` through PATH.

`source-files.json` records every exported tracked file; the source export is
not modified during qualification. Catalog bytes and artifact hashes are checked
by the existing driver. No fixture expectations, old evidence, or existing
qualification drivers were regenerated. The baseline has only the documented
test overlay. Read-only sibling cross-checks are recorded in `sibling-audit.json`.

Reserved H2 files, all `src/wago` files, shared index entries, A/B work, existing
logs, and measurement scripts were not edited or included in these commits.
No subagents, Claude reviews, benchmark/timing runs, measurement lease, PR, push,
or closure. Framework durations are incidental test output. This emission change
can affect performance and requires measurement by the separate lease owner.


## Handoff

Source/regression commit: `5c0309145c49cf95560d86c0102f0d0faa9b660b`.
Diagnostic tests commit: `bbe9ae5201e9d43907146e6af9262e8a2a0216d4`.
The evidence commit is named in the worker's final handoff (a commit cannot
contain its own hash). Compiler implementation and required qualification are
complete; the parent's remaining combined, platform, lint and performance gates
are not waived. No reserved-file handoff is needed.

Rebuild the reports without executing tests:

```sh
python3 docs/rfwgc-evidence/combined-compiler-repairs/summarize.py
```

READY TO MEASURE — branch `experiment/rfwgc-port`, tested commit
`bbe9ae5201e9d43907146e6af9262e8a2a0216d4`; performance-sensitive path
`src/core/compiler/backend/railshot/amd64/emit.go:condenseBinary` (memory-left
commutation). Proposed workloads: compile-only RFW clang-cpp kernels and the
catalog compile matrix; valid memory-fold ALU kernels for execution comparison.
Do not execute a baseline module rejected by checked compilation. The separate
measurement owner must hold the global lease; no measurement was performed here.
