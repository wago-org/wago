# Conditional snapshots and pinned locals

The amd64 backend can pin integer and floating-point locals at the same time.
Their combined count can exceed 16. A snapshot must have at least one entry for
every pinned local; its inline capacity is not a compilation limit.

`emitNativeBarrierSafeStructRefSet`, `emitNativeCardSafeArrayRefSet`, and
`emitDynamicFunctionSubtypeTest` use an inline `[16]locState` buffer and the
existing function-local state buffer pool for larger snapshots. The pooled slice
is a separate variable and is returned once, after emission. Go escape analysis
keeps the inline arrays on the stack. The generated native paths and
`reloadConditionalGCPinnedLocals` are unchanged. The helper branch restores
`lsReg`, `lsStackReg`, `lsMem`, and `lsConstZero` to the pre-branch contract.

The audit also found an eight-entry snapshot in amd64 `emitEHCatchRoute`.
Current public EH compilation disables local pins, so that limit is dormant.
It now uses the same inline-plus-pool storage without changing the EH pin policy.
A direct emitter test checks all four states and confirms that an active frame
snapshot does not enter the free pool.

Other amd64 snapshots in `call.go` already copy the full local slice with
`append`. The GP/FP candidate arrays in `assignPinnedLocals` represent individual
physical register banks, not combined local-state snapshots. They are unchanged.
No allocator policy or register limit changed.

## Regression coverage

- The original `TestGCConditionalStorePreservesWidePinnedLocals` remains intact.
  It covers struct and array stores with 19 live mixed numeric parameters.
- `TestGCConditionalSnapshotBoundariesAndFallback` checks actual compiler pin
  counts of 16, 17, and 19, with no pin relinquishment. Each numeric local has a
  separate return value. Alternate locals change before the operation, and FP
  results are compared by bits, including signed zero and a NaN payload.
- Each store test explicitly promotes a parent, leaves its heap-object child
  young, and starts with an unremembered parent and no object card. After the
  store, it checks all locals, the exact child reference, and the child's payload.
  It clears the separate child root, collects explicitly, and checks the child
  again through the parent.
- `TestGCConditionalSnapshotRepeatedWideStores` emits 64 stores with 19 actual
  pins. This is bounded snapshot stress, not a claim based on declared locals.
- With `wago_gcstats`, `TestGCConditionalSnapshotExecutesHelperFallback` confirms
  exactly one unremembered old-to-young helper mutation. The other 63 stores take
  the native path. Array checks also require that no card existed on fallback.
- `TestDynamicFunctionConditionalSnapshot` tests an exported mutable table at
  16, 17, and 19 pins. It covers a matching local function, an unrelated function,
  null, and an imported bare provider. The imported proxy's unknown compact type
  ID explicitly selects the full-metadata helper fallback. The 17- and 19-pin
  cases failed at the old compilation limit before the repair.
- `TestEHCatchSnapshotPreservesStatesAndPoolOwnership` checks 8, 16, 17, and 19
  entries, all four local states, active-snapshot isolation, and repeated buffer
  return. The inline cases must not return stack storage to the pool.

A larger synthetic-module framework remains outside this fix.
