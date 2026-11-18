# Experimental compact scalar regions

Based on 2dd082d4. Disabled by default; opt in with
`WAGO_EXPERIMENTAL_SCALAR_REGIONS=1`.

A bounded compact value stack represents integer constants, local reads and owned
GP registers. One final arithmetic operation stays symbolic for legacy sink
selection. Earlier pure arithmetic in a chain can emit directly without building
legacy expression nodes. This is not full-function SSA and does not defer
compilation until execution.

Entry requires an empty operand stack and conservative feature/state guards.
Control, memory, calls and local writes remain legacy boundaries. Register
pressure exports live values before the next operation; the existing allocator
then owns their ordinary operands. All local writes use the existing snapshot,
root, bounds and dirty-state handling. Profile/checker, exception and unsupported
reference contexts fall back. Native code may differ from the original compiler.

## Widened checkpoint

Mixed numeric functions can enter integer regions; floating-point local reads
export compact values and use the original decoded local-get handler. The
original counted-loop latch gets first refusal at an empty compact stack.

A strict terminal self-update can emit directly into its pinned local home when
there is exactly one pending value, no older aliases, a constant immediate RHS,
and no multiply or identity/mask special case. The ordinary decoded local setter
still performs write tracking, snapshots, invalidation and tee/postfix handling.
Other terminal expressions retain legacy tree selection.

## Qualification at this checkpoint

Focused normal and codegen-statistics tests pass without skipped cases, including
runtime expectations, legacy comparisons, mixed numeric boundaries, counted-loop
priority and i64 terminal-sink cases. The statistics corpus completed 580 compiles
across 145 workloads and both interruptibility modes. Ordinary-mode counters show
641 strict terminal sinks and 914 generic region evaluations; these are coverage
counts, not performance evidence.

The normal public Compile API also passed all 145 workloads with matching
activation counts and no register-pressure failure. This verifies compilation
and activation, not the full workload execution oracle.

Full workload runtime, execution latency, first-compilation latency and absolute
RSS qualification remain pending. No performance improvement is claimed. This
branch is an experimental source checkpoint, not a merge recommendation.
