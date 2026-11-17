# Combined mapped/no-capture/bounded-poll experiment

UNQUALIFIED COMBINATION. No builds, tests or timings performed for this branch.
Historical evidence elsewhere in the tree pertains to its original revisions,
not this combination. No joint performance target is claimed.

Base efb32a64 contains mapped TinyGo operands, small chunks, public scalar writer
without capture, portability test fixes and default-stack finalizer qualification.
Imported source/test commits, in order:
92b5352a 13855a62 3341b751 e17979ad 1565718f 42572b63 46105bbb 1ddc679f.
All imported source/test blobs match adjacent-pair head1ddc679f. Historical
qualification prose in bounded-vector-polls.md is intentionally not copied.
Every base-changed path
relative to iterative2afef686 remains byte-identical to efb32a64. No path overlaps
or cherry-pick conflicts occurred. Historical vector qualification-only commits
were deliberately not imported.

Use the same no-capture/mapped base as timing control, with identical toolchain,
default task stack and mapping settings. The additional experimental tag
wago_experimental_bounded_vector_polls enables B4; adding
wago_experimental_vector_poll8 selects B8. Ordinary admission remains unchanged.
TinyGo mapped arena still requires wago_tinygo_mapped_operands. Do not increase
normal planner budgets or admit scalar recurrences/reassociate floating point.

Before performance: default/B4/B8/checker suites; deterministic prefix/trap/reuse
and FP cases; actual TinyGo mapped/lifecycle gates with explicit exclusions;
all145 oracles and changed-image audit. First investigate actual matcher behavior
using wago_codegenstats (NOT wago_profile) and native images. Exact PolyBench GEMM
and issue1060's Rust example are different artifacts; retain distinct hashes and
require actual emitted-adjacent evidence rather than just plan candidates.

Only after correctness: matched per-workload execution including early calls,
true cold compilation and absolute peak RSS. Target40% lower compile latency,
mean absolute peakRSS below5MiB and no workload execution regression above5%
remain unproven.
