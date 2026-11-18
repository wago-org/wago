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

## Numeric checkpoint qualification

The integrated numeric checkpoint adds integer select, zero tests, bit counts,
rotates, width/sign conversions and signed/unsigned division and remainder.
Power-of-two constant divisors use eager reductions with the required overflow
trap; other divisors use hardware division. No performance advantage is claimed.

Normal and codegen-statistics focused gates passed 22 top-level tests without
skips (0.083 and 0.101 seconds respectively). These include independent expected
results, arithmetic edge cases, structured control, memory operations, trap
attribution, ownership aliases and existing bulk-memory regressions.

A shifted store-address regression refreshes operand locations after fixed-register
clobbers. The focused regression passes with the fix; reverting only that fix
reproduces an incorrect stored value. Source-PC and eager trap ordering remain
part of the tested contract.

Creation-budget and compact branch-chain bounds are checked. A live fallthrough
reuses the canonical state already produced at the join rather than rebuilding
it a second time.

The expanded numeric scope has not yet completed full-corpus runtime or execution
latency qualification. First-compilation latency and absolute RSS are not
qualified for this checkpoint. No performance improvement is claimed. This is an
incremental experimental source checkpoint, not a merge recommendation.

## Register-home checkpoint

An independent optional stream-value checker now verifies ownership/refcounts.
The separate `WAGO_EXPERIMENTAL_STREAM_HOMES=1` experiment preserves at most two
hot local homes in reserved registers for loop-bearing functions. Ordinary value
owners remain outside those home registers. Tagged register-backed values spill
to real temporary storage; changed home inputs are captured before either home
is overwritten. Parameters are loaded and declared locals explicitly zeroed.

The combined checker/statistics gate passed 27 top-level tests without skips
(0.093 seconds), with positive home-selection assertions, detached conditions,
local snapshots, two-home swaps, bulk/poll boundaries, real spill/backing checks
and result-register ordering. The earlier numeric checkpoint completed all 145
production runtime oracles. The new register-home checkpoint has not yet
completed those runtime oracles or performance qualification.

A single loop's static frame traffic changed from four reads/two writes to zero;
this is not a runtime speed measurement. The existing compiler's four-way loop
unrolling is not reproduced. No measured compilation, execution or RSS
improvement is claimed, and the performance targets remain unmet.

### Prior register-home diagnostic (eca4488a)

All 145 production runtime oracles passed with register homes enabled. A
100,000-iteration loop first-call diagnostic measured legacy controls at
31.526/31.727 microseconds (0.63% control difference), versus 58.216 microseconds
with register homes, about 1.84 times slower. This loop misses the requested
5% execution-regression limit despite eliminating its frame traffic.

A separate steady-state diagnostic had a 19.34% neutral-control difference and
is not qualified for performance conclusions. These limited measurements are
not full-corpus execution qualification. Preserving established loop
specializations remains necessary before broader promotion. This branch remains
experimental; no performance improvement or merge recommendation is claimed.

## Direct home-update checkpoint (5a1869fd)

The additional `WAGO_EXPERIMENTAL_STREAM_HOME_SINK=1` batch emits guarded
home-local ALU updates, short-lived current-home branch proofs, direct
conditional loop/resultless-block edges and live root fallthrough returns.
Old aliases and unsupported forms retain the established streaming paths.

The checker/statistics focused gate passed 33 top-level tests without skips
(0.098 seconds), including independent alias, branch and return regressions.
One loop's static image is 99 bytes with a 24-byte frame, zero spills, five hot
instructions and zero stack traffic. These are code-shape observations, not
measured speedups. Full production runtime oracles and timing for this new
checkpoint remain pending. The earlier eca4488a execution regression above is
not evidence of this batch's performance in either direction.

### Completed direct-home diagnostic

All 145 production runtime oracles passed for 5a1869fd. A predeclared nine-trial,
five-arm loop experiment passed 180 runtime oracles. For 100,000 iterations:

- First call: quality batch 32.488 microseconds versus legacy mean 32.548
  microseconds (−0.18%; legacy A/A difference 0.86%). The upper trial-based
  95% confidence bound was +1.32%.
- Steady execution: quality batch 30.555 microseconds versus legacy controls
  29.882/30.399 microseconds (+1.38%; A/A difference 1.70%). The trial-based
  95% interval was wide: −28.3% to +10.5%.

At 20 iterations, steady execution was +5.58% (95% interval +0.55% to +11.34%).
The short-loop first-call control failed its agreement check (4.13% A/A).

The prior approximately 1.84× deficit is closed on this particular long loop,
but the ≤5% execution-regression requirement is **not qualified**. These results
do not establish whole-corpus compilation or RSS improvements. No merge
recommendation or general speedup is claimed.
