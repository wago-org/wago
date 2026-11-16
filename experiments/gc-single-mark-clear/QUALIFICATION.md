# Single-clear correctness qualification

Production49cded96; final tests97725a2d; exact baseline35faf6c9.
Go1.27.1, patched TinyGo0.42.0 task scheduler. No performance measurements.

PASS after corrected fixture:
- Full directgc+compactgc core normal and race.
- Direct startmap/adaptive retirement.
- Compact age2, Eden, coexistence, cohort, shadow, age2metrics, age2queue-reuse,
  compact8 tag; coexistence race. Disabled policies are skipped explicitly by
  constructor checks, not counted as exercised policies.
- Unfiltered default Wago, native GC, wrapper and AMD64 backend.
- Experimental direct runtime TestDirectGC/TestTrustedDirect selection and
  applicable backend TestDirectLoop/TestGCModeBuild selection with exact tags
  in qualify.sh, including regalloccheck/codegenstats.
- TinyGo direct+compact default CORE and compact age2+Eden CORE.

Initial core failures were fixture errors: slicing arena length did not reduce
capacity, so the intended panic was not guaranteed (compact also validates extent).
97725a2d injects an impossible private pointer-map slot after a valid child instead;
normal tracing marks root/child before the bounds panic. It restores descriptor
metadata and checks zero scratch and successful retry. This does not promise
recovery from arbitrary heap corruption. All initial logs are retained in initial/.

The initially requested broad experimental backend regex FAILS on both candidate
and exact baseline: TestDirectGCStructReferenceReadRetainsRoot constructs a
handle-native lowering fixture under helper-only experimental build and panics
"amd64: native GC lowering in helper-only bridge". Both complete failure logs are
retained. It is not fixed or relabeled as passing. Full default backend passes;
qualify.sh now selects the applicable mode/loop tests for the experimental build.
No collector semantic regression was found. This is not a claim that every
repository test supports every experimental tag combination.

Pinned spec-v3 corpus reused from the established canonical checkout. Writable
GOPATH/GOCACHE/XDG_CACHE_HOME match qualify.sh. Evidence/status.txt is the initial
run (including fixture failures), status-rerun.txt records corrected cores and
baseline comparison. Runtime passes in the initial run remain valid because the
only subsequent source edit was the core test fixture, not production/runtime.

Production difference remains exactly four removed ENTRY bitmap clears; deferred
cleanup remains unconditional. Sparse cleanup, policy changes and arena-cache
optimizations are not included. Benchmarking is a separate next step.
