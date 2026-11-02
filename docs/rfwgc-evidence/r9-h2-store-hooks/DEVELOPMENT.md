# Preserved failures and corrections

Development logs are retained under `development/`. They are not final-pin
qualification. Early tests stopped at receipt invariants and were corrected
without turning their failures into passes.

1. Installing the previously missing publisher-local global receipt increased
   exact receipt counts by one while that publisher remained live. Core tests
   now count that real receipt; post-retirement counts remain unchanged.
2. The original imported table probe found that publisher table-state teardown
   cleared imported physical storage. Teardown now clears only local descriptors.
   The receipt check stopped before using the changed value.
3. The original audit's no-automatic-collection assertion was specific to its
   old unregistered conversion state. Registered receipts can request collection
   at invocation completion. The adapted test checks checked roots and exact
   physical/domain identity instead; it still stops before explicit collection
   or post-retirement access when ownership is missing.
4. `wago-h2-hooks-development3.log` records an unexpected native fault in the new
   collector-free guest clear: its helper had no synchronous control frame.
   This was not a stale-receipt reproduction. Frame allocation/admission was
   corrected, and an explicit control-frame invariant was added before invoking
   that fixture again. No failing pin was deliberately re-executed to reproduce
   the fault.
5. Immutable `integrated-1fb8f9423` passed focused tests and failed full ordinary
   Wago on public-global newly returned conversion tokens, established invalid
   token error wording, and non-nullability. The public adapter was corrected.
6. Immutable `integrated-6a7731c13` passed focused tests and failed full ordinary
   Wago on `TestActiveExternrefElementsPreserveDeclarationOrderOnFailedInstantiation`.
   This unchanged test requires an earlier completed active segment to persist
   when a later segment traps. Staging was corrected to preserve per-segment
   atomicity and declaration order; the regression was not weakened.
7. `targets-6a7731c13` is STOPPED, not PASS. ARM64 got through core tests and then
   stalled in the new collector-free guest table clear. The runner was terminated
   and the guest received SIGQUIT to preserve its diagnostic. LR equaled PC in
   the helper return path: the compiler still classified extern-only mutation
   functions as leaves. Both backends' call and loop planning now recognize the
   helper calls. New immutable target runs execute the corrected code. Remaining
   commands on the stopped run are unqualified.
8. Some development attempts failed to compile (wrong metadata helper name or
   control-frame representation). Their raw logs are retained. No target result
   is inferred from a compile failure.

Source `5b8c58b93` and `6a7731c13` intermediate lanes are attributable only to those
pins. The `*-final` lanes name `501e9ab46`. TinyGo smoke covers its existing conversion
state tests and build exclusions, not the Linux host receipt hooks. No optional
skip is promoted to PASS. Go's incidental test durations are not performance
measurements. Performance-sensitive paths include extern helper transitions,
pre-admitted invocation domains, checked-root transactions and public boundaries.
