# Independent corpus audit

This supplement independently checks the corpus denominator for the Stage 2
extension. It is not a second performance run. The worker's complete compiler,
oracle and measurement evidence lives in `../stage2-corpus/`.

Run `python3 docs/rfwgc-evidence/stage2-corpus-orchestrator/inventory.py` from any
directory inside this checkout. Its JSON output reproduces `inventory.json`.
It verifies every catalog artifact hash and walks the complete type section,
rejecting an unfamiliar non-function type instead of guessing its extent.

The catalog contains 128 workload entries backed by 121 unique binary hashes:
48 export workloads, 17 semantic workloads, and 63 commands. All artifacts are
available. The unique binaries declare 2,782 function types and no struct or
array types. This corroborates the worker's independent production-decoder
instruction census. It does not itself claim successful compilation or execution.
Guest programs implementing their own collector in linear memory are distinct
from the WasmGC constructors targeted by this experiment.

`check_census.py` independently reconciles every completed compiler report with
the catalog and verifies duplicate binaries have identical counts. Its output
is retained in `census-check.json`: 128 successful compiler attempts, zero proven
sites and zero finalized scalar replacements. Catalog-weighted totals are
294,080 functions, 84,787 analyzed, 209,293 omitted, and 7,944 analyzed functions
with unknown denominators. Deduplicating by the 121 binary hashes gives 220,624
functions, 65,520 analyzed, 155,104 omitted, and 5,984 analyzed functions with
unknown denominators. These bounded diagnostic counts are not an exhaustive
allocation-site denominator; the independent complete type and instruction
inventories establish the zero-constructor result.

The orchestrator reviewed the archived harness and corrected the measurement
plan before timing: both primary revisions enable the option; the baseline is
analysis-only, while Stage 2 includes the checked consumer. Workload pairs replace
whole-catalog timing groups. The semantic oracle helper also receives the same
option as the measured runtime. Rebuilt binaries completed correctness validation
before timing. Ordinary, debug and regalloccheck validation is separate from the
ordinary binaries used for performance measurements.

The benchmark follows the previously accepted quiet-baseline method with its
unchanged 5% external-host and sibling limits. An initial brief incorrectly
extended the separate main checkout's local AGENTS.md temperature rule to this
worktree; that rule is outside this worktree's instruction ancestry. The brief
was corrected before timing. CPU-package temperatures remain recorded as
descriptive evidence. An advisory lock coordinates cooperating measurement
processes; it does not establish exclusive access to the entire host.

Staged conformance is a separate inventory. The previous full-suite evidence has
18 failing top-level tests and 20 distinct source paths with converter errors.
The 102 unique WAST sources inventoried from 19 accounting snapshots are not all
known blocked: some families previously passed. Their 1,242 lexical module forms
include negative inputs; 522 direct module commands are not a fresh successful
execution count. The missing Release 3 interpreter and its `dune` bootstrap
dependency remain an explicit qualification gap.
