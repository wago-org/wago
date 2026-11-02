# Stage2 quiet comparison — 2026-10-30

Baseline `6ceb862219e64480c0503836b19ee1e02b209cc7`; Stage2
`d28005d48f42524f9bb8a6556d92c5b701c42cd9` (production `1ad7996`, parent test-only
`07d938f`). Read Stage2 README/runtime.py and the prior benchmark/quiet-rerun.py.
No suites were repeated. No engine source was edited. Disposable git archives;
only the identical portable `gc_lifetime_stage2_amd64_test.go` was copied into
baseline. Both archived compile fixtures are identical (hashes in host.json).
Runtime bodies are the exact original expressions with an export; compiler
fixtures contain 32 functions. Compilation/instantiation and fresh-instance guest
accounting are excluded from timed runtime loops.

## Reproduction and sampling

Run from repository root, after all workers/builds/tests have stopped:

```sh
python3 docs/rfwgc-evidence/stage2-benchmark/measure.py \
  --out /tmp/stage2-evidence --cpu 10 --repeats 6 --benchtime 500ms
python3 docs/rfwgc-evidence/stage2-benchmark/summarize.py
```

The summary script reads evidence beside itself; copy it beside a new dataset
before summarizing that dataset. Go 1.27.1, GOMAXPROCS=1 for both compiler and
runtime; GOFLAGS='', GOEXPERIMENT='', GOWORK=off, GOTOOLCHAIN=local. Other host
runtime environment values and CPU information are recorded. No GOGC override.
Each process pinned to CPU10, SMT sibling11 observed. One fixture's Off+On cases
per process; 500ms requested per subbenchmark plus Go calibration. Six repeats,
revision order reversed on odd rounds, fixture order rotated, runtime/compile
suite order alternated. Three consecutive two-second admission intervals must
have <=5% external-host CPU and <=5% on both selected logical CPUs. Throughout
each complete invocation, every >=250ms observation (normally one second) must
have <=5% external-host CPU excluding the benchmark CPU, and <=5% sibling CPU.
Reject the whole pair otherwise; keep its output and monitoring separately.
This reproduces the previous quiet-rerun standard, without exclusive machine
access. Scheduling/turbo drift and sub-interval contention remain possible.
The runner aborts after five minutes without accepted progress.

Preparation-attempt records a pre-sampling cwd error (package init uses relative
fixtures). Corrected by running each binary in its package directory. Initial
CPU6/sibling7 1s/10-repeat, all-12-runtime-cases groups produced no accepted group;
large-group-attempt retains their output and monitoring. A concrete blocker was
sent to parent via T3 steering. Parent requested smaller pairs and six repeats
at >=500ms. CPU selection records five two-second all-CPU observations; sibling11
was 0–2.49%. The observation initially overlapped the ending *owned* CPU6 benchmark,
so CPU6's 100% there is not evidence of another task. The matched fresh dataset
uses only CPU10. The owned archive holder was paused to reuse all four already
built binaries, then resumed/interrupted for cleanup after the pair run. No other
process was killed or pinned. The fresh run made continuous bounded progress.
Tiny timing is omitted from the fresh dataset; its fallback behavior remains
covered by existing Stage2 tests, not a new measured performance claim.

## Units and interpretation

Host Go B/op and allocs/op are measured by Go's benchmark timer and are distinct
from guest allocations. Runtime accounting uses a separate **fresh 1024-call**
loop per sample, excluded from timing. Guest charges are Stats.Allocations deltas;
handleless native charges are sampled each invocation to account for cycle reset;
materialized objects = charges minus successful handleless charges; this fixture
publishes one handle per materialized object. This does not use LiveObjects or
handle capacity as an allocation proxy. Table integer totals are reconstructed by
rounding the harness's printed per-op fractions times 1024 (e.g. 0.0009766 ->1;
0.9990 ->1023). All accepted accounting readings are checked identical.

Layout is 24 bytes/object. Throughput heap Align16 rounded request is calculated
32 bytes/object; it is not measured physical reservation traffic. Actual observed
nursery bump advance is accumulated separately per invocation, including cycle
changes. Retained nursery end is an absolute snapshot. Native handle batches and
chunk reservations still exist. **This is not zero allocation or reservation.**

See tables.md and summary.json for all accepted timings, min/max/sample CV,
percent changes, host allocations, and guest accounting. Raw accepted outputs
are named NN-suite-revision-fixture.txt; rejected-* outputs and admission.jsonl /
during-timing.jsonl retain all qualification data. Original compile quiet costs
(+23.34% qualifying, +4.22% nonqualifying, +2.35% no-GC) were Stage1 On-vs-Off costs
against an older oracle, not new Stage2-vs-6ceb effects. The current tables give
both revision comparisons and within-revision option costs.

Read-only Slipstream fetches at start/final are logged. No new master changes
since db6014de2. Region-first default b3e802899 and B1/B7 remain source context;
db6014de2's host fix is ABI-specific, old-space ceiling and helper wake/join remain
pending. Wago retains Go-backed arenas and stable handles. No source coverage
percentage is a Wago result. No sensitive engine bug fixes (DaybreakBlue only),
subdelegation, PR/push/rebase or upstream backlog work. Existing dirty spec-v1/v2
and unrelated docs are excluded from staging. Cleanup evidence records removal
of archives, all binaries, GOTMPDIR and private GOCACHE.

## Verdict

Guest publication reduction is measured: qualifying Stage2 On makes 1 materialized
object / published handle in 1024 fresh calls versus baseline Off/On and Stage2 Off
1024, with 1023 handleless charges. All four still charge 1024 allocations, consume
24576 layout bytes, and show 24576 bytes observed nursery advance / retained end.
Calculated throughput rounded requests remain32768B; this is not measured physical
reservation traffic. Nonqualifying remains1024 materializations/charges; no-GC
remains0. Host runtime allocations remain0 B/op and0 allocs/op in all twelve cells.
This extends, and does not replace, the existing128-call128->1 publication proof.

Qualifying runtime On is276.7ns/op versus baselineOn279.2ns/op (-0.90% latency,
+0.90% reciprocal throughput); ranges overlap275–285.9 versus276.4–286.8ns and CVs
1.48%/1.62%. Within Stage2, On versusOff287.8ns is-3.86% latency (+4.01% throughput).
Controls also move: Off runtime costs rise3.15–3.83% across revisions, nonqualifying
On rises3.98% and no-GCOn5.05%. This does not establish a robust net runtime speedup
against the quiet baseline; the qualified result is a modest local On/Off effect
plus clear guest-publication reduction. It is not a general application claim.

Compiler cost is substantial: qualifyingOn160469->352295.5ns (+119.54% versus
baselineOn); host147496->299472B/op and692->2463allocs/op. Stage2On versusOff is
+171.28%, versus newly measured baseline+24.99%. NonqualifyingOn rises8.16% across
revisions (host124982->125750B/op,309->341allocs/op); no-GCOn rises3.83% (host38064B,
57allocs unchanged). Off compiler changes are+1.15%,+1.35%,+1.54% qualifying,
nonqualifying,no-GC respectively, with unchanged host allocation counts. Prior
quiet Stage1 option costs+23.34/+4.22/+2.35% are context, not Stage2 measurements.
Keep the optimization opt-in; guest handle publication savings do not justify a
claim of free allocation removal or a clear end-to-end throughput win here.

The fresh matched dataset took18m19s including quiet waits:72 accepted pairs,
24 rejected pairs, six accepted500ms samples per cell. Accepted monitored maxima:
4.7873% external host and2.9703% SMT sibling; full raw telemetry retained. Initial
long-group and preparation attempts extend total wall time beyond that fresh
window. No statistical significance test or exclusive-machine isolation; CV and
min/max are descriptive. Tiny timing omitted; existing fallback tests are the
only Tiny evidence. No new sensitive-engine issue was fixed or investigated.
