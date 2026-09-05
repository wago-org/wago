# Audit repair results

Implementation of the [accepted plan](performance-security-repair-plan-2026-09-05.md).
Baseline: `f0f4951138b8`. Host: Linux/amd64, Go 1.27.1. Initial serial samples use
`GOMAXPROCS=1`, `-cpu=1`, 100 ms, five samples. Raw local captures are in
`/tmp/wago-audit-repair`; final release measurements need the plan's longer A/B
and native-platform runs. The initial Go test-binary build exceeded 30 seconds;
process inspection showed compilation followed by a 12.8-second benchmark suite,
not an individual Wasm compile over 30 seconds.

## Background cancellation watcher

The new inert-context allocation test failed before the fix with 7 allocs/op for
Background, TODO, and WithoutCancel. All now take the shared no-op path before
callback/channel allocation. Nil contexts remain allocation-free. The focused
cancellation, deadline, and invocation-context tests pass. The new benchmark
reports zero bytes and allocations for each inert context. No native polling or
scheduler transition was changed. Native non-Linux and TinyGo tests are pending.

## Structured custom-section budgets

Name and branch-hint readers now share the parent budget. Exact-capacity
structured vectors reserve storage and allocator rounding before allocation;
strings and the owned raw payload are charged separately. Strict section checks
remain in place. A valid 3 MiB module name now decodes with a 16 MiB metadata
budget and rejects with an 8 MiB budget. The former malformed fixture now checks
that the structural parser rejects it rather than codifying a payload multiplier.
The complete wasm package tests pass. Structured-name benchmark captures are in
`decode-budget.txt`; large valid names could not be benchmarked successfully on
the old implementation. Type-slab changes are a separate repair.

## Remaining work

The other work groups and full release gates in the accepted plan are pending.
This file is updated with each repair; a partial result is not a merge approval.
