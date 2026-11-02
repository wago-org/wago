# Independent Darwin reservation checkptr repair recheck

## Scope

Independent bounded review of the test change in
`65df156853df5a266d35c780a6092a3825a4c1ac` against its parent, limited to
`src/wago/gc_native_reservation_arm64_test.go`, plus receipt verification of
`../darwin-reservation-checkptr-repair/`. No source, CI, shared-log, sibling
project, or existing evidence file was changed. No benchmark, timing,
measurement lease, dispatch, push, pull request, native rerun, or broad suite
was performed.

## Findings and objections

No blocking finding or objection in the reviewed test repair.

- The removed reads reconstructed Go pointers from the `uintptr` values in
  `NativeView().StructAllocState` and `NativeView().LargeArrayAllocState`. The
  native Darwin race receipt records the resulting `checkptr` abort at the
  live-struct test. The replacement performs no `uintptr` conversion and uses
  no checkptr suppression.
- `nativeReservationCount` reaches the same collector-owned state objects by
  their exact `Collector` field names, then requires an exact struct and
  `uint32 Count` shape before calling `Uint`. `nativeStructAllocState.Count`
  and `nativeLargeArrayAllocState.Count` are both `uint32`; the production ABI
  layout checks independently bind each field to
  `NativeStructAllocCountOffset` (8). The observed scalar is therefore the
  same field previously read through the raw ABI address, not a substitute
  oracle.
- Each read occurs synchronously after `in.Invoke` returns. The instance and
  its collector remain strongly reachable until the deferred `in.Close`, so
  the collector is live and guest execution is idle for the read. The helper
  does not retain a reflection value beyond the call.
- The 47 live-struct invocations and 2,000 alternating wide-array invocations
  are unchanged. All invocation error, result-length, returned-value,
  zero-reservation, and exact allocation-total assertions are preserved.
  The helper's shape failures are fail-closed, so the oracle is not weakened.
- The commit changes no production API or production source. Reflection is
  confined to this package-local test helper.

## Receipt verification and bounded validation

- `sha256sum -c` passed for all ten files listed by the repair evidence's
  `SHA256SUMS`.
- The reviewed test file SHA-256 is
  `d2d57dd1ba93753d9cf05b3897fd73b93c6d65aa44a1023f748406611277c4ec`,
  exactly the overlay hash in `provenance.json`.
- `results.json` records build and execution exit 0 in ordinary, wagodebug,
  and wago_regalloccheck. Each execution log records both focused tests as
  PASS, with no skip. Builds used `-gcflags=all=-d=checkptr=2`; executions used
  ARM64 QEMU against immutable base
  `08f511fac41a77f4c0f9046f88ebc78ad05a5da6` plus the exact single-file
  overlay above.
- `git diff --check` passed for the reviewed file.

This is meaningful all-three-mode strict-checkptr ARM64 validation of the exact
test overlay. It is not native Darwin execution and does not exercise `-race`.

## Native rerun readiness

READY FOR NATIVE RERUN. The reviewed repair removes the raw ABI
`uintptr`-to-Go-pointer reconstruction that caused native audit `e3222e019` /
`native-run-41466555967`, retains the original test oracles, and has bounded
all-three-mode ARM64 strict-checkptr evidence. A native Darwin all-three-mode
race rerun is still required to confirm the platform-specific failure is
closed.

## Overall qualification

NOT QUALIFIED. No post-repair native Darwin/race receipt exists. The original
native failures remain unchanged, including the separately unresolved
`TestTwoIntegerResultRegisterCall` split-stack overflow. This review does not
upgrade the broader run or those preserved failures.

