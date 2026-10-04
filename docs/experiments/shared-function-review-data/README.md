# PR #802 review-fix evidence

See [the report](../shared-function-review-fixes.md) for interpretation, source revisions, timing boundaries and limitations.

- `raw-timing.csv`: one row per benchmark per fresh process; iterations are not independent samples. Primary, long-execution and worker cohorts each have six processes per revision.
- `raw-memory.csv`: every phase of both fixed-work primary policies. `total_alloc` and `mallocs` are cumulative; derive deltas against that process/scenario's initial phase. Native `mapped_bytes` is actual owned capacity; rounded payload is separate. Public `native` contains runtime-native accounting and is not interchangeable with heap/RSS.
- `memory-phase-summary.csv`: medians, min/max and sample counts for every phase/metric, plus compile/release deltas.
- `raw-memory-investigation.csv`: separately labeled normal-policy replication and child-local THP-disabled diagnostics. These do not replace primary results.
- `process-resources.json`: fresh-process peak RSS, faults and CPU usage. `rss-investigation-summary.json` also includes the separate sampled-map diagnostic; that diagnostic has observer overhead and is not primary evidence.
- `diagnostic-summary.csv`: admission, corpus/native hashes, code/frame/spill and scratch accounting. Peaks are accounting envelopes. These diagnostic binaries are separate from production timing.
- `stage-allocation-diagnostic.csv`: single-process control/storage captures isolate allocations; their times are not significance evidence.
- `zero-probes.csv`: fixed probe bytes/native hashes and code sizes for both widths.
- `AH-*` and `GH-*`: complete benchstat comparisons. Nominal p-values are not corrected for multiple comparisons. `A/H-zero256-*` files summarize separately collected fixed-work allocation profiles.
- Manifests and command JSON files identify source, harness, binaries, validation merge, build environment, tests and every fresh-process invocation. In `cross-checks.json`, old-G reproduction failures and the strict unavailable mapping-identity gate are intentional nonzero statuses; none is presented as a passing qualification.

Full binaries, raw logs, profiles and scripts remain outside Git at the durable evidence path stated in the report. Paths in command records refer to that machine. Recreate A/G/H checkouts and the identical H measurement harness, then substitute local binary/corpus paths when replaying commands. No forced memory release was used.
