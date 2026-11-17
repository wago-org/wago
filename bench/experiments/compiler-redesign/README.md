# AMD64 compiler redesign experiment

Baseline: main `227dfd64ddb3760ae8547eb69a27da38c6cd8676`.

Acceptance targets, not measured results:

- At least 40% lower full compilation latency.
- Under 5 MiB arithmetic-average absolute peak process RSS on the pinned
  wasm.fyi workload set. Report incremental memory separately, never substitute
  it for this target. A process/runtime floor is part of the result.
- No more than 5% execution-latency regression, with per-workload results and
  repeated paired trials, not only an aggregate that hides regressions.
- AMD64 first, pure Go, correctness and TinyGo compatibility retained.

## Candidate designs

1. Shared bounded analysis: reuse validation facts in hint collection and
   lowering; eliminate repeated decode/classification and redundant whole-body
   pattern scans. Keep memory bounded by the current function rather than build
   a whole-module instruction IR.
2. Indexed optimization dispatch: partition recognizers by opcode and proven
   function/loop capabilities, preserve explicit ordering and dependencies,
   share lookahead decoding across competing recognizers. Retain good lowering
   without paying every pattern's failed match cost at every instruction.
3. Streaming function lifetime: separate compact interprocedural summaries from
   per-function detail, release temporary analysis immediately after emission,
   and avoid whole-module sidecars when consumers need only one function.

These are competing/complementary hypotheses. Profile and prototype before
choosing; consolidation alone is not evidence of improvement. Do not use corpus
names, hashes, or benchmark identity to select compiler behavior.

## Measurement

The probe compiles one module in a fresh process, using the public API and one
function worker. Input reading and SHA-256 hashing are outside compilation time,
but inside process RSS. It performs exactly one compilation; no warmup, forced
GC, cache priming, or post-compilation RSS subtraction. Collect absolute Linux
peak RSS using `/usr/bin/time -f '%M'` (KiB). An `-empty` process establishes the
probe/runtime floor but does not reduce the reported compiler RSS.

Use identical toolchain, build flags, machine, CPU affinity and corpus bytes for
baseline and candidate. Build outside timing windows, alternate order, save all
raw samples, and exclude concurrent GC/compiler benchmark jobs. Hash artifacts
and binaries. Measure both cold compilation and separately steady-state compile
through the existing suite. Run existing corpus execution oracles before paired
execution comparisons. The catalog `website` profile is only a candidate set;
confirm the currently published wasm.fyi set and record exact IDs before claiming
coverage. Never silently skip failing modules.
