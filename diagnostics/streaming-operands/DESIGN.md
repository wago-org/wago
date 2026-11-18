# Experimental streaming operand engine

Based on 2dd082d4. Disabled by default; opt in with
`WAGO_EXPERIMENTAL_STREAM_OPERANDS=1`.

This engine specializes the shared scalar value/refcount/spill machinery for a
concrete AMD64 target. It consumes validated bytecode directly, retaining bounded
deferred integer expressions and canonical local/operand homes at structured
control joins. It is not full-function SSA and does not defer compilation until
execution. Unsupported functions retain the established compiler.

## Current integrated scope

Integer arithmetic/comparisons, block/if/loop, conditional/unconditional branches,
return, unreachable, scalar integer memory loads/stores and memory.copy/fill are
integrated. Admission is conservative: integer locals/signatures, bounded bodies
and stack/control depth, and only unshared memory32 zero for memory operations.
Calls, references, EH, unsupported proposals and other opcodes fall back before
streaming emission. Profile/checker and TinyGo builds retain the legacy path.

Canonical homes provide branch agreements without phi nodes. Conditions are
captured before local-home stores. Taken-edge result transfer does not overwrite
fallthrough state. Loop backedges include interrupt polling when enabled. Eager
memory accesses preserve bytecode trap order and check complete ranges before
writes. Bulk operations verify fixed-register leases and free vector scratch.
Legacy constant preloads are omitted for streaming functions.

## Focused qualification

The frozen checkpoint passed 14 top-level tests in both normal (0.055 seconds)
and codegen-statistics (0.079 seconds) builds, without skips. Coverage includes:

- Nine structured-control interruptibility cases with positive streaming/poll
  counters and independent expected runtime results.
- Scalar memory widths, address overflow, aliases and bulk operations.
- Exact source-PC attribution for a dropped out-of-bounds load in both modes.
- Bulk register preconditions and four legacy bulk regression cases.

Creation-budget and compact branch-chain bounds are checked. A live fallthrough
reuses the canonical state already produced at the join rather than rebuilding
it a second time.

The full workload corpus, execution latency, first-compilation latency and
absolute RSS have not yet been qualified. No performance improvement is claimed.
This is an incremental experimental source checkpoint, not a merge recommendation.
