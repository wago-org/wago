# Single-clear qualification and matched measurements (prepared, not executed)

Candidate49cded96 versus exact base35faf6c9. First run qualify.sh serialized after
CPU release; stop any promotion on unexplained failure. It covers full direct and
compact tests, race, direct retirement policies, compact age2/Eden/coexistence,
cohort/shadow/metrics/queue-reuse/compact8, admitted runtime/backend selections,
and TinyGo core. Preserve known admission/corpus setup failures separately and
rerun the matching baseline to distinguish pre-existing failures; never silently
omit failed rows or say all tests passed. New injected panic test must also pass.

Only after correctness succeeds, create an isolated baseline at35faf6c9. Copy
identical direct BenchmarkMarkCleanupGraphs fromc591378b into BOTH builds (its
benchmark/tests call no candidate-specific helper; avoid later d63 helper test).
Existing BenchmarkFullTracingShapes and retirement correctness tests are already
identical. Copy no production change to baseline. Build same Go1.27.1 with same
flags/GOMAXPROCS. Binary hashes and exact diff must establish only entry-clear
removal as production difference.

Quiet-window plan (no timing while any worker builds/tests/source or git jobs):
- Direct: five alternating baseline/candidate fresh-process rounds, existing
  BenchmarkMarkCleanupGraphs + BenchmarkFullTracingShapes,500ms per subbenchmark.
  Two heap budgets, empty/sparse/dense graphs, Minor/Full cases retained. The Minor
  graphs become empty-young after initial promotion: label this explicitly.
- Compact: identical gc-eden-core harness, both binaries enabled with age2+Eden,
  runtime policies promote,age2,Eden at fixed-total2MiB; rolling84x4096,
  dense-persistent,dead-heavy;128epochs and five rotating process repetitions.
  This isolates entry-clear removal within each policy, not policy gains.
- Pin every process to one allowed CPU with taskset, GOMAXPROCS1. Record each
  process start/endUTC, loadavg, allowed/selected affinity, code/binary hashes.
  Own jobs paused is not proof of exclusivehost; retain all sample vectors.
- Exact graph/allocation checks and collection counts must agree across revisions
  for each policy/case. Memory budgets unchanged; retain any code/Go-alloc changes.
- Analyze five process-level comparisons, not internal iterations as independent.
  Do not combine short-run historical absolute times with this protocol.

Estimated direct105seconds of measured work plus calibration/processoverhead;
compact roughly30–60seconds, reserve3–5minutes after builds. If noisy, report
inconclusive rather than infer speedup from deleting one source clear. No sparse
cleanup, adaptive-policy toggles, constructor changes or V8 comparisons are mixed
into this causal experiment. Full runtime/V8 gate comes only after a solid win.
