# Dedicated Eden core traffic methodology (unexecuted)

This harness requires the qualified dedicated-Eden prototype and
`wago_gc_compact_eden,wago_gc_compact_age2`. The original five-shape run passed 20 audits and 100 timed processes; those
results are preserved separately and do not establish recurring retention.
The added rolling workload and all controls passed a fresh complete run; see
[RESULTS.md](RESULTS.md) for counts, regressions and limitations. The harness must
be applied to an isolated worktree at the qualified prototype commit, not mistaken
for a runnable benchmark against this older evidence branch. Do not modify the
collector or qualify performance before model/property and core tests pass.

## Policies and accounting

One same-source Eden+age2-enabled core binary, runtime constructor selection.
All policies share enabled feature-state fields. Report collector struct bytes
and MemoryUsage.Total (including descriptors) as well as FixedBytes. This is not
a measurement of default production-runtime performance.

| Policy | Fresh allocation capacity | Survivor storage | Total young | Each old |
|---|---:|---:|---:|---:|
| promote | 2 MiB | none | 2 MiB | 8 MiB |
| age2 | 1 MiB maximum, reduced by retained survivors | two shared young semispaces; retention cap 512 KiB | 2 MiB | 8 MiB |
| eden | fixed 1.5 MiB | two dedicated 256 KiB spaces | 2 MiB | 8 MiB |
| eden-same-fresh | fixed 2 MiB | two dedicated 256 KiB spaces | 2.5 MiB | 8 MiB |

Primary comparison is equal total young memory (first three rows). The fourth is
separately labeled equal-Eden sensitivity: +25% young backing and approximately
+2.8% arena backing (18 → 18.5 MiB before fixed headers). Never pool it with the
fixed-budget results. Report FixedBytes too: this includes scalar side metadata,
not just arena bytes, but excludes descriptor and Go object/slice overhead.

## Graphs and lifetime schedules

Each epoch performs the same logical allocation sequence for every policy.
Collections occur only on allocation exhaustion, not on fixed caller phase
boundaries, except the separately labeled dense full-trace control below. Failed Minor due to old capacity retries Full; allocation retries
Full if needed. All successful and failed collection calls are counted separately.

- dead-heavy: 65,537 pointer-free 32-byte structs, all immediately dead.
- persistent344KiB: exactly 344 KiB Layout16 scalar-array object retained throughout
  dead-heavy traffic; payload marker checked after timing. This is a persistent large-object overflow control, not recurring survivor
  prefix pressure. It fits the existing
  age2 512 KiB retention cap but exceeds a new 256 KiB survivor space, so overflow
  promotion and extra old/full pressure are part of the result, not hidden.
- rolling84x4096: old root array of 84 slots, cyclically replaced by scalar
  arrays with 4,096-byte total stride. Latest payload IDs are checked. Each
  minor retains roughly 344,064 bytes of recent arrays (plus root header/slots);
  those cohorts die before the next minor. This approximates recurring retention,
  not exact reproduction of a Wasm artifact.
- array-bulk: 2,048 immediately dead 4,096-byte scalar arrays (plus headers).
- dense-persistent: old root array of 256 reference arrays, each holding 32 edges
  back to the root; retained across dead-heavy traffic, explicitly Full-collected
  once per epoch as a dense tracing control, and checked slot by slot.
- old-young-mutation: same old root array, replacing its 256 children cyclically
  with fresh scalar objects. Last children remain live; their references are
  checked after timing. Dense/mutation setup Full is outside timing and identical.

Setup is excluded. Every completed epoch includes allocation/barrier/collection
costs. Post-timing checks verify exact lifetime allocation deltas, valid roots,
retained scalar content and dense edges. Report objects/op, successful minor/op,
full/op, failed minor/op, fixed bytes and young/fresh/survivor budgets alongside
ns/op and Go allocations. Throughput is objects/op ÷ seconds/op. This is not
copied-byte traffic instrumentation: do not infer copied/promoted counts absent
explicit counters. The promoted cohort can differ; equal input graph does not
mean equal collection schedules or identical live placement.

## Proposed serialized measurement

After qualification and explicit CPU release: build one test binary in the new
worktree, run all policies/shapes with `-test.benchtime=1x` as a correctness audit,
then five rotating policy rounds, fresh process per policy/shape, GOMAXPROCS=1,
`-test.benchtime=8x -test.count=1 -test.benchmem`. Use exact selectors to avoid
benchmark calibration-dependent workload sizes. Preserve every process receipt,
return code, binary/source hashes, UTC start/end, affinity/model and load average.
Treat five process samples as five independent observations, not eight epochs per
sample as independent. Keep warm-up/preconditioning effects explicit: every
process starts with a fresh collector and no hidden preconditioning.

Example (not run):

```sh
GC_CORE_POLICY=eden GOMAXPROCS=1 ./eden-core.test \
  -test.run='^$' -test.bench='^BenchmarkEdenTraffic$/^eden$/^persistent344KiB$' \
  -test.benchtime=8x -test.benchmem -test.count=1
```

All policies must retain identical requested allocations and checked graph
contents. Stop on mismatch; keep failing output. Dense/retained/mutation regressions
remain first-class controls. No native adapter/compiler, host-call, TinyGo runtime,
Wasm execution or V8 performance claim follows from this core-only harness.
