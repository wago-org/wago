# RR-1 / RR-2 invocation repair

Implementation evidence only. Parent owns fresh review and final acceptance.
No performance measurement, push, PR, subagent, or new Claude review was run.

Review input: `0efde30d7e1b95412808516c9a2ef307cd97f160`, full
`final-review-design-recheck/README.md`; its failed target was
`d2464100f1945d38f9d1e25196855fe9699d9875`. Prior evidence remains unchanged.
Source/test pin and immutable qualification results are recorded below when
available; development passes alone do not qualify the implementation.

## Design and lifetime proof

Only registered conversion-only admission changes. It snapshots the sorted
local/imported domains plus live conversion publisher/receipt-claim domains
under the topology reader. Before dropping that reader it validates every
registered domain and increments its generic collector claim. Validation is
all-or-nothing, including overflow. Private static domains retain their existing
instance/import lifetime proof. The topology reader is dropped before waiting
for any invocation domain or native execution. Truly dynamic managed-function
admission, including its empty-domain control, retains its existing reader.

A cold topology registry records snapshots by authenticated invocation identity
and root. It also prevents terminal store retirement while admission pins exist.
Canceled acquisition unwinds partial domain gates before dropping its pins.
The acquired snapshot, rather than a recomputed live topology, supplies helper
and unlock authority. Pins decrement under the topology writer only after domain
and native gates have been released; final unlink is atomic with that decrement.
Collector Close and token/finalizer cleanup happen after writer/store unlock.
No pin becomes a permanent publisher or object root.

Arbitrary callbacks and shared waits suspend the exact acquired domain gates.
The parked invocation keeps lifetime pins but no executing authority. Resume
releases the old pins and performs a fresh complete admission before reacquiring
native execution; the outer lease object is updated to the new snapshot.
Nested callback scopes keep a stack of same-identity snapshots. Final return and
trap cleanup consume the actual current snapshot. Failed resume cleanup can see
an already-unpinned inactive snapshot without double-releasing gates or claims.

Conversion helper IDs 53–58 and native arities remain unchanged. The existing
native host loop passes its root and invocation context explicitly before
callee-local dispatch. Neither the callee's public invocation field nor the
first domain's current holder supplies authority. The authenticated root's
Runtime must match, and its acquired domains must still have its invocation ID.
Single and bulk publication validate both displaced and incoming receipt domains
against that view, including identities registered after admission. Missing
membership rejects before checked-root or visible storage mutation. Public
module-container setters and initialization apply the same membership guard.
The ordinary-token/null bypass retains its existing two-sided authentication,
so scalar cross-Runtime ordinary storage remains supported.

## Regression provenance and scope

`src/wago/design_recheck_adversary_test.go` is byte-identical to the review's
`_probes/design_recheck_adversary_test.go`; `provenance.json` records both hashes.
It retains the original deadlines and assertions. Additional tests in
`design_invocation_snapshot_test.go` cover partial ordered acquisition/cancel,
all-or-nothing pin overflow, topology-writer progress during domain wait, late
old/new domain rejection for single/bulk stores, publisher physical Close with
collector lifetime held only by a pin, exact last-pin unlink, stale identity and
unknown native-control rejection, valid root single/bulk operations, complete
callback gate release, and return/trap cleanup. Existing DR/H2/H6, admission,
start and cross-instance tests remain in focused/race gates.

Sibling read-only audit is the review's recorded Slipstream
`084be3d4bf70fb5337085ceddfab1bfa9db8b1c9` / `dcb45b56386327a1f71fb039ed02e13562e1e2d7`
and Starshine `1d6c0af47140299d8f3c01ed03060d279c43ee03` audit. No additional
sibling or independent pre-existing defect is asserted by these repairs.

## Preserved development failures

- Lifecycle attempt 1 and development-2's final regalloccheck/race cell hit the
  same newly introduced synthetic fixture's nil `instances` map before runtime
  admission. Lifecycle attempt 2 corrects the fixture. Development-2's cells
  therefore do not form a common fixed-source qualification matrix.
- Development-3 ordinary caught an implementation classification error in the
  existing true-dynamic empty-domain control. An empty view did not prove
  conversion-only admission. Development-4 requires explicit registered
  conversion-only metadata; the original control is unchanged.
- Every attempt has its own raw log. No failure is relabeled or overwritten.

## Qualification drivers

Drivers here derive from the previously committed runtime-repair and H2 drivers.
They use immutable source archives (excluding docs), explicit Go GOROOT/tool
paths, fresh output directories, and source/driver integrity checks. Integrated
scope is focused, full Wago, scoped race, backend/compiler, and ARM/RV build-only
in all three modes. QEMU runs original adversaries/new lifecycle tests plus H2
and conversion tests on ARM64, and relevant existing compact RV64 controls.
The inherited target-only portability adapter and architecture probe are hashed.
TinyGo executes its supported conversion compatibility smoke; it does not claim
host opaque receipt support. Darwin ARM64 and Windows AMD64 cross-builds are
compile-only; the parent owns actual native runner qualification.

## Immutable qualification at 9ad40bce4

Source/test pin: `9ad40bce4a7397dbadf0b0528f2587ed59d7d441`.
All 18 integrated, 14 target, six nonhost build and three TinyGo commands passed.
`summarize.py` asserts every required new regression in each host-focused/race
and ARM64 guest cell, zero skips in required cells, no DATA RACE, and unchanged
source exports/drivers. It independently compares every tracked `src` blob in
all four exports against the source commit. `summary.json` records the exact
blob census and all skip entries; `SHA256SUMS` covers retained artifacts.

| Lane (top-level passes / recorded skip entries) | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Linux AMD64 focused | 92 / 0 | 92 / 0 | 92 / 0 |
| Linux AMD64 scoped race | 92 / 0 | 92 / 0 | 92 / 0 |
| Full Wago | 2531 / 44 | 2525 / 50 | 2531 / 44 |
| Backend/compiler | 1006 / 552 | 1004 / 554 | 1129 / 553 |
| ARM64 QEMU execution | 67 / 0 | 67 / 0 | 67 / 0 |
| RV64 QEMU controls | 48 / 0 | 48 / 0 | 48 / 0 |
| TinyGo conversion compatibility | 5 / 0 | 5 / 0 | 5 / 0 |
| Darwin ARM64 build only | PASS | PASS | PASS |
| Windows AMD64 build only | PASS | PASS | PASS |

Full Wago/backend skips are existing architecture/optional/diagnostic entries,
retained verbatim; skip-entry counts include subtests. No required regression
was skipped. ARM/RV cross-builds in the integrated lane also passed all modes.
The architecture probe is included in target counts. Native Darwin/Windows
execution, full-package race, and a separate complete conformance campaign were
not run. TinyGo's five tests qualify only its supported conversion smoke.

RR-1 implementation verdict: repaired and locally qualified at this pin. The
original storage-only native global/table repro admits unrelated topology
writers; acquired conversion domains are independently lifetime-pinned and
cancellation unwinds them. Truly dynamic admission retains its original gate.
RR-2 implementation verdict: repaired and locally qualified at this pin. The
original valid nested programs pass; foreign/stale/late unauthorized single and
bulk stores remain fail-closed. These are implementation findings, not reviewer
approval, final inclusion, or final combined-head qualification.

## Independent measurement handoff

READY TO MEASURE (independent worker only, subject to the parent's remaining
review/native-platform gates). Branch `experiment/rfwgc-port`, source commit
`9ad40bce4a7397dbadf0b0528f2587ed59d7d441`.

Performance-sensitive paths: conversion-only invocation admission, snapshot
allocation/registry and collector pin release, callback suspend/resume, native
helper root dispatch, single/bulk receipt membership checks, public module
container writes/initialization, and terminal store retirement accounting.
Proposed workloads: storage-only scalar calls with empty and populated publisher
sets; disjoint GC instantiation while a storage-only guest runs; same-Runtime
nested scalar and storage relays; ordinary cross-Runtime relays; single and bulk
converted publication/clear; callback publisher replacement; repeated last-pin
retirement. Measure independently under the global lease and CPU rules. This
worker took no benchmarks or performance timings and acquired no measurement
lease. Parent owns fresh review and final acceptance.
