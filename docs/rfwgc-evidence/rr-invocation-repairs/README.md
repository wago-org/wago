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
