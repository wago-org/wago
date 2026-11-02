# Incremental review: TinyGo public-global token classification

Reviewed source commit: `c786a9b84bb0298d5bbd6b9522ab611193440cfa` (`fix(gc): preserve invalid-token classification for public globals`).

Verdict: **RESOLVED for the scoped parent fix.** No counterexample found. This is an incremental review of the repair only; it does not replace the active original Claude DR review and is not final port qualification. Actual TinyGo execution in all three modes belongs to the separate qualification worker.

## Finding disposition

The f2bb failure was caused by `setConversionGlobalPublic` classifying every nonzero unsupported-target word with `unsupportedConversionWord` before `Instance.SetGlobalValue` reached its ordinary checked-global path. Consequently forged and cross-store public tokens incorrectly received `opaque converted publication is unsupported on this target` instead of `invalid externref token`.

The repair preauthenticates a nonzero `externref` token with the selected destination store:

- A missing store, forged token, expired token, or cross-store token returns `(false, nil)`. `SetGlobalValue` then takes its existing invocation/native-state lease and runs `setGlobalValueNoLease`, which performs type, mutability, nullability, owner, and exact-store validation before any `writeGlobalObject`.
- An authenticated ordinary token still reaches `unsupportedConversionWord`, is accepted there, and proceeds through the ordinary checked-global path.
- An authenticated converted wrapper remains distinguishable in the same store directory. `resolveExternref` unwraps it only for the authentication result; the subsequent `unsupportedConversionWord` directly inspects the slot wrapper and retains `opaque converted publication is unsupported on this target`.
- Null is unchanged: it bypasses preauthentication, is accepted by `unsupportedConversionWord`, and reaches the existing nullable/non-nullable check.
- A wrong `Value` type bypasses the adapter and reaches the ordinary type error. An immutable target with an invalid token likewise reaches ordinary sequencing and reports immutability before token validation. A closed instance is rejected by `beginInvocation` before ordinary token publication. The repair adds no publication or ownership transition.

## Safety and race review

The added operation is a read-only `referenceStore.resolveExternref`. It locks `referenceStore.mu`, validates the store key, index, generation, and live slot, optionally unwraps only the returned public value, then unlocks. It neither increments retention/pin counts nor changes slot ownership. It cannot write the destination global.

There is a deliberate second lookup after preauthentication. Release or close between the two checks cannot authorize a write: generation checking prevents a stale/reused token from authenticating as the original token, and the ordinary path revalidates before `writeGlobalObject`. If an authenticated converted wrapper is released between checks, `unsupportedConversionWord` still rejects the now-nonordinary slot. Lock acquisition is non-nested (`resolveExternref` returns before `unsupportedConversionWord` or the ordinary validator locks), so the change introduces no lock-order cycle.

Destination-store selection is exact: owned globals use `g.owner.store`; otherwise instance globals use `access.refStore`. A token from another runtime therefore fails preauthentication and cannot be confused with a genuine converted wrapper from the destination store. RV64 compact publication is unchanged because `compiledArchitecture == 4` exits before the repair.

## Verification

Verification used an archive of exact `HEAD` because a separate qualification worker concurrently created an untracked, initially incomplete fixture in the shared worktree. No worker file was changed or included.

Passed:

- `go test -tags=wago_target_tinygo ./src/wago -run TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries -count=1`
- `go test -tags='wago_target_tinygo wagodebug' ./src/wago -run TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries -count=1`
- `go test -tags='wago_target_tinygo wago_regalloccheck' ./src/wago -run TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries -count=1`
- `go test -race -tags=wago_target_tinygo ./src/wago -run TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries -count=10`
- `git diff HEAD^ HEAD --check`

The `wago_target_tinygo` selection compiles and exercises `gc_conversion_store_other.go` on the local executable target. Actual TinyGo all-mode qualification is explicitly not claimed here.

No source or expectation edits, benchmark, performance timing, push, PR, or subagent were used. The repair is not performance-qualified by this review.
