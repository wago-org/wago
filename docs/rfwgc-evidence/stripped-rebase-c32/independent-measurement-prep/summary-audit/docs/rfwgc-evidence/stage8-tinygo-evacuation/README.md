# Stage 8: actual TinyGo serial mature evacuation

Branch: `experiment/rfwgc-port`. Production change:
`cff52eaa6` (configuration and serial participant diagnostics). Focused tests:
`3410e626d`, extended by `9c26df2d4b35c89fd3cf6e591f3b033370a50a07`.
The latter is the immutable expanded qualification pin. Evidence commit is
reported separately in the handoff. No dirty source overlay is used.

This supersedes the **TinyGo evacuation rejection** described in stage 7 only
for the qualified target below. It does not revise that historical evidence or
close the combined port, seven-PR, R9, H6/H2, review, or performance gates.

The change enables the existing serial evacuation transaction under TinyGo,
keeps Workers >1 rejected, and preserves the ProfileTiny versus compiler
distinction. ProfileThroughput with explicit region Eden and MatureEvacuation
is required; default/no-GC policies are unchanged. See [DESIGN.md](DESIGN.md)
for the transaction, runtime ownership boundary, and read-only sibling audit.

## Commands and source integrity

`initial/` records a complete eight-command narrow matrix on `cff52eaa6`:
compiler identity, pinned bootstrap, and Go/TinyGo evacuation selectors in all
three modes. It passed before the extra target card/pressure/guest fixtures.
It is not substituted for the expanded final tests.

`expanded/` records Go 1.27.1 and real patched TinyGo **0.42.0**, Linux amd64,
`-scheduler=threads -gc=conservative`, in ordinary, wagodebug and
wago_regalloccheck. This means a **serial Wago collector on TinyGo's threads
scheduler**, not an enabled collector helper pool. For each mode:

- full Go `./src/core/runtime/gc/...` (no benchmark selector);
- actual TinyGo `-run Evacuation ./src/core/runtime/gc/native`;
- actual TinyGo `-run 'GCStage8TinyGo|TinyGoForeignStack'` in runtime and wago.

Each invocation has an exact `.command.json`, exit status, test log and explicit
top-level pass/fail/skip lists in `results.json`. `summary.json` additionally
retains all subtests and exact skip names, and asserts required target fixtures ran. The immutable export, fresh private cache,
environment, archive hash and driver hash are in `source.json`; per-Go-file hashes
are in `source-hashes.json`. The runner asserts source and driver unchanged at
completion. Original driver bytes are retained as `driver.py`. Compiler and
patched runtime hashes are recorded outside the run directory. These are
correctness runs; incidental framework durations are not performance samples.

Reproduce without checking out a branch or touching the dirty shared worktree:

```sh
python3 docs/rfwgc-evidence/stage8-tinygo-evacuation/qualify-final.py \
  9c26df2d4b35c89fd3cf6e591f3b033370a50a07 /tmp/wago-stage8-new-evidence
```

The output directory must not exist. The pinned compiler location and bootstrap
are inherited from `tinygo-foreign-stack-fix/qualify.sh`; its full 34-command
prerequisite lane is documented there. This stage runs a focused joint smoke,
not a claim to have rerun all 34 commands or full conformance.

## Nonvacuous coverage and limits

Target tests verify serial movement with Workers 0/1 and region shifts 6/12,
region/default opt-out, required LOS admission failure triggering movement,
stable checked generations, exact payloads, credit deficits/overflow/run caps,
source exclusion, crossing extents and absorbed tails. High-card nonmoving LOS
references and an old self-edge/young child survive relocation and subsequent
minor collection. A 65,537-object young plan is not truncated by the mature
candidate cap. Root drop reclaims identities and repeated Close is safe.

Debug target tests inject metadata/backing refusal, precommit and first/last
partial serial-copy failure, verify unchanged source/allocator ownership, retry
success, and distinguish committed-maintenance failure from rollback. Artificial
failure hooks are intentionally compiled only in wagodebug.

The guest builds a live reference array, promotes objects, makes the old graph
sparse, triggers required allocation failure, asserts positive moved bytes and
native-view refresh, traps, explicitly invokes TinyGo runtime GC and reads 42
from a relocated object. Separate guest controls exercise defaults, no-GC,
region-only, and enabled region configurations through trap/GC/read/Close.
Existing foreign-stack tests force concurrent GC on the foreign mapping and
exercise return/trap/interruption, host reentry with parked roots, prepared entry,
and forced Close-unmap failure followed by cleanup. No foreign-stack fix bytes
were changed.

Build-excluded capabilities are **not passes or runtime skips**: TinyGo collector
helpers, native object-edge/reclaim callbacks, H6 host graph/extern receipts,
non-amd64 execution and other schedulers are outside this matrix. No disabled
core allocation/callback API is enabled here. H6 worker46303554 retains its
ownership; H2 production remains subject to H6's wakeup repair qualification.

## Final scoped result

All **11 expanded commands passed**, in addition to all eight initial commands.
The following counts include tests and subtests; no failure occurred in either
run. Exact names and commands remain in the adjacent JSON/log files.

| Mode | Full Go collector passes / skips | TinyGo collector passes / skips | Joint guest + foreign-stack passes / skips | Failures |
| --- | ---: | ---: | ---: | ---: |
| ordinary | 2225 / 0 | 15 / 0 | 9 / 0 | 0 |
| wagodebug | 2307 / 3 | 30 / 0 | 9 / 0 | 0 |
| wago_regalloccheck | 2225 / 0 | 15 / 0 | 9 / 0 | 0 |

The three existing debug skips are TestTryReserveDeadAllocationBatch,
TestPreparedDeadAllocationBatchLiveState, and TestPreparedDeadBatchEndBounds.
No new target test skips. Source checking verified **9,122 archived members**
byte-for-byte unchanged (`expanded/all-export-unchanged.json`), in addition to
the runner's per-Go-file and driver checks. Both owned runners drained; their
exact private exports/caches were removed with receipts in each cleanup.json.

`owned-source-equivalence.json` verifies every owned code/test file against the
qualified pin after concurrent H6 work advanced the branch to 86cacffeb. Those
new H6 bytes are **not qualified by this run**. No parent-owned dirty path or
index entry was included in these commits; the port log is deliberately left
to its owner. Final combined-head execution and review remain parent-owned.

READY TO MEASURE after all correctness workers drain, by an independent holder
of the global measurement lease. Production cff52eaa6; test/qualification pin
9c26df2d4b35c89fd3cf6e591f3b033370a50a07; branch experiment/rfwgc-port.
Sensitive paths: TinyGo config admission, serial bounded required promotion and
optional selection/reservation/checkpoint/copy/publication, metadata rollback,
card/native-view refresh, and interaction with registered foreign-stack GC.
Proposed workloads: default/region-only/no-GC controls; serial evacuation off/on;
sparse versus dense old graphs; fragmented old/LOS with cards; admission refusal
and retry; guest trap/reentry/Close and concurrent TinyGo runtime GC. ProfileTiny
is a separate nonmoving control. No performance direction is asserted.
