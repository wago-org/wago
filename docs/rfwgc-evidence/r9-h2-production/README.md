# H2 production prerequisites — incomplete implementation

This checkpoint contains production changes, but **does not complete H2**.
No INCLUDED-FIX, final bucket, final combined-head, or READY TO MEASURE claim.
No external permission/tooling blocker was established. Surviving-provider
ownership remains unimplemented; the original stopped evidence is preserved.

## Production and test pins

- `90fac4d60`: host allocating extern-conversion products with typed function
  storage participate in managed function classification. AMD64/ARM64 frame
  planners admit opaque extern global imports without treating their words as
  compact frame roots. The former stopped fixture now has managed=true,
  frameRoots=true, and validated allocation safepoints. Numeric/nonallocating
  controls remain outside function ownership. No DisableCollection bypass or
  native collector/configuration change was made.
- `3cd7e60a9`: each conditional owner slot records its exact collector. Mark,
  dead-slot clearing, member removal, and collector retirement use this identity,
  permitting a nil-collector owner to hold slots in multiple domains. The bounded
  test proves live-owner tracing, no unconditional seed after owner removal from
  the live set, equal slot indexes in different collectors, idempotent binding,
  and cross-Runtime rejection. It does not install container receipts or leases.
- `00043664c`: existing mirrored extern table replacement now acquires the new
  receipt before its visible write and releases the old receipt afterward. All
  root/count validation precedes publication. A rejected publication rolls back
  the new count and preserves the old root/storage. Same-value and clear controls
  pass; the existing6a25 overflow rollback test remains intact. This does not
  intercept imported/global or ordinary inline backend stores.
- `8b1f324e0`: collecting local-owner function aggregate control, default and
  actual region shift6. It asserts the live conversion checked root and function
  environment before explicit collection, then explicitly clears its local
  conversion before owner Close. It is not a surviving-provider test.
- `5156fcc29`: makes those host tests runnable on ARM64 without test adaptation.
  This is the first integrated source pin; its later wagodebug race failure is retained below.

The H6 typed mutation safepoint code, strong active callback roots,631cb wakeup
scheduler and shared-domain membership proof are unchanged. RV64 compiled
classification and dispatch semantics are unchanged; its shared conditional-slot
representation now carries an explicit collector identity.

## Qualification

`admission-90fac4d60/` and `slots-3cd7e60a9/`: each immutable source passed six
commands: focused H2/H6 ownership plus scoped race, in ordinary, wagodebug and
wago_regalloccheck. Each unchanged shell driver exited0. Exact commands, pinned
corpus/tool provenance, logs and result ledgers are retained.

Integrated source5156fcc29 qualification is recorded in
`integrated-5156fcc29/`, `targets-5156fcc29/`, and `tinygo-5156fcc29/`.
Consult their final ledgers for completion and exact failures/skips; running or
missing results are not passes. The integrated driver runs focused, full Wago
with exported pinned spec submodules, scoped race, full AMD64/ARM64/Wasm backend
packages and two target cross-builds in each mode. Cross-builds are compile-only.
The target driver is derived from stage8-platform-execution/run-guests.py and
uses actual QEMU10.0.13 execution, checked ELF architecture, and an in-guest
architecture assertion. Its selector covers H2 host tests and existing RV64
managed-function/compact-extern controls. RV64 does not execute the host H2 tests.
The supported TinyGo smoke selects conversion tests; host owner sidecars remain
build-excluded and are not counted as TinyGo H2 support. Tool paths are explicit,
using the stage3/foreign-stack dependencies, not mise shims.

No benchmark or performance sample was taken. Incidental test durations are not
measurements. Performance-sensitive paths include frame admission, managed
function classification, conditional mark/retirement, and conversion table
replacement. Independent measurement belongs to the parent after completion.

## Preserved failed development/setup attempts

- `development-slots-ordinary.log`: test-oracle mistake, expecting an empty leaf
  object in the child-work queue. Corrected to inspect the seen bitmap; no invalid
  object access followed the assertion.
- `development-collecting-local.log`: conversion result was dropped and optimized
  away. The test stopped on absent identity before explicit collection/teardown.
  Corrected fixture stores to a local global, checks ownership, then clears.
- `integrated-prerequisites/`: setup exit1 before any suite. The worktree lacked
  the pinned spec-v3 tree. The corrected driver uses the existing parent-checkout
  corpus fallback. The original driver and failure are retained.

## Remaining blocking implementation invariants

1. An opaque conversion identity still belongs to its publishing state. A
   surviving physical container needs its own receipt and conditional checked
   slot, keyed by actual identity plus collector, not a compact interpretation
   of the opaque word. One shared entry root cannot be assigned to several
   independently reachable container owners.
2. The collector/domain claim must survive publisher physical Close without
   retaining the publisher itself. Receipt-only domains must remain in ordered
   invocation and joint collection leases. Final claim release must be journaled
   outside topology readers, with631cb membership revalidation preserved. The
   current scheduler also needs an entrypoint when no collector-owning instance
   remains; selecting only a live collector instance cannot cover that case.
3. Global/table set, fill, copy, init, grow, initialization, public clear and
   teardown must transact receipts at actual physical storage. Collector-free
   providers currently use inline backend stores; the mirrored-table transaction
   above does not cover them. Bulk alias/overlap/multislot and failure rollback
   require full-range acquisition before the first visible write.
4. Floating conversion roots must become conditional at store quiescence while
   remaining strong during active callbacks. The converted aggregate -> producer
   -> provider cycle requires exact release after the final external edge, with
   no permanent publisher/store retention and exactly-once physical cleanup.

No collecting surviving-provider, publisher-Close, converted-cycle, receipt-only
retirement contention, or final integrated H2 test is claimed passed. The original
H2 ownership failure evidence remains authoritative for its own pins. Broad green
prerequisite tests cannot substitute for these missing implementation gates.

## Read-only sibling check and scope

Read Slipstream actual084be3d4bf70fb5337085ceddfab1bfa9db8b1c9:
`src/jit.goose` imported table/global regions are registered before publication.
That obligation does not supply Wago's opaque identity/domain representation.
Read Starshine1d6c0af47140299d8f3c01ed03060d279c43ee03:
`docs/wiki/wasm-gc-core-boundary.md` describes model/codec/validation boundaries,
not an analogous native ownership contract. No new sibling defect is inferred.

All owned production slices were committed with git commit --only. Existing
stage2 scripts, shared port log and dirty submodules/index were preserved. No
shared log edit, native collector/config/TinyGo production edit, subagent, Claude
review, timing, lease, push, PR, closure or upstream action occurred. Final review
and combined PR remain parent-owned.

## Last-collector retirement correction

The integrated5156fcc29 driver is **FAIL**: commands1–8 passed, command9
(wagodebug scoped race) failed in
`TestR9OwnershipWakeupProviderAndStoreClose/region=0/store-close=true` because
both physical callbacks did not arrive within its five-second deadline. No data
race, crash, or invalid-object access was reported. The driver stopped, exited1,
and verified an unchanged export. The missing remaining nine commands are not
passes. Its actual QEMU gates passed independently (21 top-level ARM64 cases and
45 RV64 cases in each mode, zero skips), without qualifying that host race gate.

Production/test correction `10c2006fb` preserves pending finalizer/reclaim
journals when the last collector retires. Previously member removal discarded a
finalizer queued because another topology reader still existed. The graph now
stays reachable until post-gate drain consumes that journal; an empty graph is
retired afterward, with an already-retired guard protecting a successor graph.
No native collector/configuration or retry scheduling protocol is changed.
A deterministic metadata-only test covers this exact journal transition, and
8 repeated wagodebug race runs of it plus the provider/store-close gate pass.
`retirement-before-5156fcc29/` injects only that deterministic metadata probe
into a separate5156 archive: exit1 at "STOP collector retirement erased deferred
finalizer", as expected before the repair. No native object was allocated or
accessed in that probe. This is a preserved failing result, not a pass.

`integrated-retirement-fixed/`, `targets-10c2006fb/`, and `tinygo-10c2006fb/`
qualify the corrected integrated source. Their final ledgers and summary are
controlling. The former5156 failure remains preserved, never relabeled green.
This correction does not implement receipt-only domain collection or H2
surviving-provider ownership.

## Corrected integrated qualification result

Source `10c2006fb9f59ec2418b62a8d4cb19c9cdcd6fc7`: all18 integrated commands
PASS, unchanged driver exit0, unchanged exported members. Focused H2/H6 and
conversion tests, full Wago with pinned spec submodules, scoped race, full
AMD64/ARM64/Wasm backend packages, ARM64 cross-build and RV64 cross-build each
pass ordinary/wagodebug/wago_regalloccheck. Cross-builds alone are not execution.

Actual QEMU target execution separately passes all six guest commands plus six
build commands and two emulator-version checks. Each mode executes22 top-level
ARM64 cases and45 RV64 cases, with zero skips/failures. RV64 validates its existing
compact-extern/joint-mark semantics; it does not claim host opaque H2 support.

Actual TinyGo0.42 smoke passes all three commands, five conversion/transaction
tests per mode, zero skips/failures. Host managed-function sidecars and the new
host H2 tests are excluded by build tags. This is not the full foreign-stack,
CLI/release, or H2 TinyGo support gate.

Full Wago's existing optional/architecture/diagnostic skips are preserved verbatim
in per-suite ledgers and `summary.json`. In particular opt-in Stage3 external
corpus, MoonBit Starshine smoke, full official spec execution and official thread
script suites are not converted into passes. Focused, race, target and TinyGo
selected gates have no skipped cases. These qualification results apply only to
the implemented prerequisites and retirement correction; the four remaining H2
implementation invariants above still block completion.
