# Leaf scoped integer constant prototype

Default off: `leaf-scoped-int-const` / `WAGO_ARM64_EXPERIMENT_LEAF_SCOPED_CONST=1`. Extends existing effect-qualified, call-free loop leases to functions that make no calls. The function-wide literal floor remains seven; loop-scoped literal reservations retain five free temporaries and release their register and regional budget at loop exit. Nested scope handling reuses the existing restoration mechanism. New pressure admission excludes occupied or currently pinned physical registers.

No workload names, sizes, constants or hashes participate in production admission. The repeated multiplier motivating inspection is already selected by ordinary hints; insufficient entry-time register budget is the observed cause of missed caching. Earlier global floor changes were rejected, so this candidate isolates the lower-pressure reservation to proved scopes.

Independent pressure tests cover the five/six free-register boundary, an occupied candidate, a temporarily pinned candidate, unchanged global floor, and release restoring the original reservation. Existing nested lease, helper-effect rejection and native arithmetic tests remain applicable. Qualification and application native-impact inventory are queued through the shared device wrapper. No performance/correctness claim yet; retain only after affected-case execution/compilation measurements and both-bounds qualification.

## Initial evidence and mitigation

Backend/catalog diagnostics and all102 signal application exact oracles pass. Native images change only global alignment, local alignment and distance transform. Register allocation admits no extra scalar lease; its hints already contain the desired multiplier. Initial focused execution medians: global alignment +3.52%, local alignment +1.63%, distance transform -0.95%. Compilation is +5–12% across the seven measured cases, including unchanged-image controls; this is unfavorable.

Before rejecting, filter the leaf scoped hint list after global preload: omit page-address candidates and literals already cached globally, preserving the calling-function path. This avoids scanning loops when there is no additional literal to lease. It leaves the global floor seven and scoped floor five unchanged. An independent filter test covers typed uncached retention, address exclusion, globally cached exclusion, and preservation of established calling-function hints. Mitigated qualification and five-case paired timing are queued. No retention claim.

## Rejected after mitigation; rollback verified

Filtered variant passes diagnostics and all102 application exact oracles. All102 images are identical to the initial candidate, so runtime behavior is unchanged. Repeat changed-case execution: global alignment +4.74%, local alignment +1.52%, distance transform -0.57%. Compilation remains +5.87–10.96% in the focused subset. Removing already-cached/address hints did not rescue the tradeoff. Rejected and removed all production hooks, option/catalog entries and tests; archived the exact integration patch and independent tests under `experiments/rejected-leaf-scoped-const`.

Rollback diagnostics pass. All46 core plus102 application signal exact oracles pass, and all148 native images match the retained common-exit baseline exactly. Existing scoped leases for call-making functions and the global floor seven are restored. No new retained optimization from this experiment.
