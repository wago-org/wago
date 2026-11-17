# Exact F64-store region prefilter

Source PoC from `fc11f7a7`; no performance result. `inspectRegionLoop` accepts its
latch only with `storeN > 0`; its only store-producing instruction is F64Store
(opcode 0x39). Independent lanes, adjacent outputs and scalar recurrence are
classified only after that prerequisite. Thus a whole original function proven
not to contain F64Store cannot have an accepted region plan.

The existing byte and AST hint traversals collect this fact only when the
compile-time `boundedVectorPolls` experiment is enabled. A negative proof is set
only after the complete successful traversal. Unknown/zero hints and failed byte
scans fail open. There is no additional traversal or retained sidecar. Bit 14 of
`immediateFreeOps` is used; its counter saturates at 256 and bit 15 retains the
fixed-scratch lease flag. Counter accessor and increment preserve both flags.
Retained `funcHints` remains 28 bytes. The function scratch gets one boolean,
assigned from the current function's hints on every compilation.

The early exit is restricted to bounded + interruptible + inlineDepth == 0.
Noninterruptible planning is unchanged. Inline lowering always bypasses this
filter: localBase==0 alone would not prove it is the original body. Diagnostic
counter `region-skip-no-f64-store` records rejection without a reader operation.

Tests added for packed flags/counts, exact byte and AST store detection, malformed
and unknown fail-open behavior, inline/noninterruptible bypass, skip-reader
position and diagnostic stats. Existing TestGemmPairEmittedPackingAndFloatEdges
must still pass; it checks actual packed-poll emission and traps on scalar
fallback, not just planner acceptance. Existing bounded independent/adjacent,
checker, interrupt and full corpus image/oracle gates remain required.

No builds/tests performed at this source checkpoint. Collection is compile-tag
conditional, but default TinyGo ELF/compilation must still verify that unused
capture/state does not introduce host allocations. No regression attribution or
speedup is claimed; the measured planner overhead hypothesis needs a paired run.
