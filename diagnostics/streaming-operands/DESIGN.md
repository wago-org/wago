# Experimental streaming operand engine

Based on 2dd082d4. Disabled by default; opt in with
`WAGO_EXPERIMENTAL_STREAM_OPERANDS=1`.

This checkpoint specializes the existing shared scalar value/refcount/spill
engine for a concrete AMD64 compiler target. It retains bounded deferred
expressions and canonical local/operand homes at supported control joins. It is
not full-function SSA, and does not defer compilation until execution.

## Implemented and exercised

Whole-function admission and body dispatch are connected for the existing
integer ALU, block and if subset. Unsupported functions retain the established
compiler. Local pinning, zero initialization and worker cleanup are integrated.
Profile/checker and TinyGo builds retain the legacy path at this stage.

Focused normal and codegen-statistics runs passed three top-level tests without
skips. A result-if runtime fixture returned the independently expected 32 and 24
with the experiment both off and on; statistics verified one streaming function
when enabled and zero when disabled. Memory shape/address unit tests also pass.

## Implemented but not yet integrated or qualified

Scalar memory and bulk-copy/fill helper source and execution fixtures are
included for the next stage. Memory/bulk dispatch and loop admission are not yet
wired into this checkpoint. Those execution fixtures were not run and are not
claimed to pass. The full test suite, workload runtime oracle, execution latency,
first-compilation latency and absolute RSS have not been qualified.

No performance improvement is claimed. This is an incremental experimental
source checkpoint, not a merge recommendation.
