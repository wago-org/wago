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

## Qualification at this checkpoint

Four focused test groups, including six runtime cases, pass in the normal build
with positive region admission and no skipped tests. Tests compare results with
independent expectations and legacy execution. The preceding checkpoint also
passed the focused codegen-statistics build; the final pending-source-PC and
reference-result guards have not yet been rerun under that tag.

Full workload correctness, execution latency, first-compilation latency and
absolute RSS qualification remain pending. No performance improvement is claimed.
This branch is an experimental source checkpoint, not a merge recommendation.
