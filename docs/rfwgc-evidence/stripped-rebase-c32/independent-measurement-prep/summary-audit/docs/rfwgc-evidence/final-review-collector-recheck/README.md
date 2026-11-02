# Final collector C1 recheck

## Scope and pins

- Original production source: `5345bd365c13963f1bf60e26bb80789ab9ead4cc`.
- Original collector review: `39ae86d8ad15fde4b890df338cfd1e3683b5b0b5`.
- Comment repair reviewed: `942fe6c7d731ffacfac61c4aaa51bf13d5063cd3`.
- Finding rechecked: final collector review C1 only.

This was a fresh source re-review of the `Config.NurseryRegionShift` contract and its native reservation implementations. It does not close or qualify the remaining fan, Darwin, or performance gates.

## Verdict

**C1 resolved.** The repaired comment accurately describes the implemented region-mode policy, and commit `942fe6c7d731ffacfac61c4aaa51bf13d5063cd3` contains no executable change.

The wording is supported clause by clause:

- “Bounded region-owned native batches” matches `prepareNativeRegionAllocation`: it rejects zero, large, over-chunk, and over-region requests; obtains one Eden interval with `regionAllocationOffset`; caps its end at both `NativeAllocationChunkBytes` and the current region boundary; reserves the handle batch before committing the interval; and records the exact start, cursor, end, and previous bump (`src/core/runtime/gc/native/native_regions.go:3-34`).
- “support small structs and arrays” matches both region dispatch edges. `PrepareNativeStructAllocation` and `PrepareNativeAllocation` route region mode to `prepareNativeRegionAllocation`; the latter is used by the small-array paths (`src/core/runtime/gc/native/native_alloc.go:147-155`, `:182-198`, `:222-253`). The generated-code integration tests exercise both struct and array publication (`src/wago/gc_native_regions_amd64_test.go:34-123`).
- “cancellation returns only their unused owned tail” matches the exact-tail reservation contract. `cancelNativeRegionChunk` rewinds to the cursor (or the pre-reservation bump when wholly unused) only when the current region bump still equals the reservation end; a later owner makes cancellation fail closed (`src/core/runtime/gc/native/native_regions.go:37-60`). `discardNativeStructHandles` selects this region-specific cancellation path (`src/core/runtime/gc/native/native_alloc.go:372-440`). The focused cancellation tests cover unused and partially published reservations (`src/core/runtime/gc/native/native_regions_test.go:5-60`).
- “baseline direct-bump ... remain[s] disabled” is accurate: `nativeAllocationAllowed` requires `NurseryRegionShift == 0`, while region-mode structs and small arrays take the distinct region-owned path (`src/core/runtime/gc/native/native_alloc.go:68-71`, `:147-155`, `:182-219`).
- “native large-array allocation paths remain disabled” matches `prepareNativeLargeArrayAllocation`, which rejects every nonzero `NurseryRegionShift` (`src/core/runtime/gc/native/native_alloc.go:222-307`). The rollup reservation contract also asserts that a small region reservation succeeds while a native large reservation refuses (`src/core/runtime/gc/native/rollup_reservation_test.go:8-50`).

The terms “baseline” and “native large-array” avoid implying that all native batching or all checked large-object allocation is disabled. The comment therefore repairs the original contradiction without overstating the supported region-mode surface.

## Diff and validation

`git show --stat` and the parent diff for `942fe6c7d731ffacfac61c4aaa51bf13d5063cd3` show one file changed, `src/core/runtime/gc/native/collector.go`, with three comment lines added and one comment line removed. A path-excluding diff against the commit parent was empty. There are no code, test, configuration, generated-file, or submodule changes in the repair commit.

No tests or benchmarks were run for this recheck. The original reviewer already ran the focused collector suite in ordinary, `wagodebug`, and `wago_regalloccheck` modes, and this repair changes documentation only. It cannot affect runtime performance. Existing unrelated worktree modifications and submodule dirt were preserved, and no production source was edited.

## Handoff

C1 has no remaining objection and is resolved at `942fe6c7d731ffacfac61c4aaa51bf13d5063cd3`. This verdict is limited to C1 and is not final campaign approval.
