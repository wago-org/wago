# Wide struct card ranges: PR 642

Baseline: `27c1f066a3e04bc31ca82d6b55ce4977b19c35ee` (fetched current main).
Reviewed PR head: `774e03621bca05e1cc3b492658ec092bf5477fc6`.
Main was merged without conflicts in `b7ab5eeb7`. The final PR description
records the pushed head that contains this report. No other open optimization
branch was included. The production diff against main remains the original
119-line local change in `src/core/runtime/gc/native/cards.go`; main's newer
chain detachment and full-scan correctness rules remain intact.

## Correctness

- A chain with 33 valid ranges uses the existing per-range scanner. It does not
  enable collector-wide fallback. Duplicate/overlapping records rejected by the
  optimized helper also retain the existing scanner's semantics.
- Tests inject wrong owners, invalid links, cycles, reversed intervals,
  misaligned starts/ends, and payload overflows after 16 valid linked records.
  Direct rejection preserves marks, nonempty pending work, handle/card records,
  fallback state, and telemetry. Production collection preserves distinct young
  children outside the valid prefix, detaches the corrupt chain, retains global
  fallback while survivors remain, and clears metadata after promotion.
- The parent alone is rooted. Children have distinct observable integer payloads.
  Moving and non-moving configurations cover 15/16/32/33 actual ranges, old and
  large parents, ordered/reversed/shuffled descriptors, mixed field kinds, nulls,
  repeated children, adjacent coalesced cards, and a partial final payload card.
  Tests read children from current parent fields. They run repeated minors,
  check promotion against the current tenuring policy, reuse cards, remove edges,
  and use full collection to check reclamation of promoted children.
- Valid padded geometry tests 255/256/257 fields without changing card size.
  Direct admission checks establish the selected path. A bounded differential
  test and fuzz target compare marked sets, not stack insertion order.
- Lifecycle and differential parity tests pass on both main and the optimized
  implementation. No collector correctness defect was found that required a
  failing-before regression. Admission-specific tests apply to the new helper.
- Tagged telemetry parity includes shuffled, mixed, repeated/null-reference,
  and coalesced layouts. Original per-range descriptor-order accounting remains.

## Allocation and stack use

Go 1.22.12 warmed direct helper scans with 16 and 32 distinct children allocate
**0 B/op and 0 allocs/op**. Warmed 15/33-range, narrow-struct, and array fallback
scans also allocate zero. Mark-stack capacity is warmed before measurement;
marks and pending work reset each time. Fixture construction is outside timing.
Cold mark-stack growth allocates four times for 16 children and five times for
32 children on both implementations. These are not allocation-free GC claims.
See [allocation results](allocations.txt) and [escape diagnostics](escape.txt).

The fixed `[32]structCardRange` array is **512 bytes**, does not escape, and adds
no heap scratch buffer. The Go 1.22.12 AMD64 helper prologue reserves 840 bytes
and saves an 8-byte frame pointer (848 bytes excluding the return address).
The ARM64 prologue adjusts SP by 848 bytes. See the [AMD64](stack-amd64.txt) and
[ARM64](stack-arm64.txt) disassembly. Neither figure is the total cost of the
nested call chain or a goroutine stack growth. No Collector fields, retained
caches, locks, or per-object heap allocation sites were added.

## Measurement

Go 1.22.12, Linux/AMD64, Ryzen 7 8845HS, CPU affinity 2, performance governor,
boost enabled, `GOMAXPROCS=1`. Six serial pairs alternate AB/BA order, with
200 ms per case. There were no concurrent builds, tests, fuzzers, or profiles.
Both binaries use identical benchmark and fixture source on the same main base.
See [environment](environment.txt), [pair order](pairs.txt), [raw baseline](baseline.txt),
[raw optimized](optimized.txt), [benchstat](benchstat.txt), and [all sample ranges](spread.csv).
Benchstat uses its default statistical comparison; p-values are not adjusted
for the number of cases. Do not use the cross-workload geomean as a product claim.

Selected scan-only medians (ns/op):

| Layout | Main | Optimized | Change | p |
| --- | ---: | ---: | ---: | ---: |
| 8192 fields, ordered, 16 ranges | 72,537 | 32,617 | -55.03% | .002 |
| 8192 fields, ordered, 32 ranges | 140,190.5 | 40,860 | -70.85% | .002 |
| 256 padded mixed fields, 16 ranges | 3,730 | 1,469 | -60.62% | .002 |

All scan-only cases report 0 B/op and 0 allocs/op. The first two rows' sample
ranges are 70,650–73,364 versus 31,422–32,755 ns and 135,150–146,577 versus
39,910–41,418 ns. Reversed and near-ordered 8192-field eligible scans improve
54.02–71.85%. The mixed-parent controls now include 256 unrelated card records.
Array controls explicitly measure the count-check cost before struct rejection.

Selected **complete CollectMinor** medians, moving nursery, 4097 shuffled mixed
fields with distinct children in each dirty range:

| Ranges / parents | Main ns/op | Optimized ns/op | Change | Main / fix B/op | Allocs/op, both |
| --- | ---: | ---: | ---: | ---: | ---: |
| 16 / struct | 56,667 | 16,878.5 | -70.21% | 24 / 24 | 1 |
| 16 / struct + ineligible array | 60,411 | 19,091 | -68.40% | 24 / 24 | 1 |
| 32 / struct | 208,766.5 | 22,771.5 | -89.09% | 25 / 24 | 1 |
| 32 / struct + ineligible array | 218,564 | 25,013 | -88.56% | 27 / 24 | 1 |

Each row has p=.002, n=6 per version. For the single-parent rows, the sample
ranges are 55,923–57,162 versus 16,713–16,946 ns (16 ranges), and
201,846–211,152 versus 22,566–22,928 ns (32 ranges). Non-moving eligible cases
improve 68.08–88.98%; all complete-collection cases report one allocation per
operation. B/op includes amortized cold capacity growth and can vary with N;
these differences are not a claim that this patch removes those allocations.

Every timed collection has newly allocated young children and remembered edges.
Setup, range-count checks, payload checks, full-collection cleanup, and heap
verification are outside collection-only timing. These are not repeated scans
of an already-promoted graph, and not allocation-plus-collection cycle timings.

### Fallback limits

The earlier 7–12% increases in ordinary fallback controls did not recur. Most
now have 1–6% sample spread and no significant difference. The 8-field reversed
single-range control increases 45.26 to 45.61 ns (+0.77%, p=.022). Array medians
increase at most about 1.6%; lack of significance does not establish zero cost.

One shuffled 4097-field/33-range scan-only control initially increases
117,563.5 to 184,731.5 ns (+57.13%, p=.041). Its baseline range is
111,076–200,767 ns and optimized range 158,467–214,086 ns. This is a real limit
on the first measurement, not evidence of a free fallback.

A separate six-pair, alternating **one-second** recheck of 15/16/32/33 ranges
produces 192,214 versus 175,912.5 ns for 33 ranges (p=.394): baseline range
111,087–192,754, optimized 152,148–181,766 ns. The baseline 32-range result also
varies widely while optimized eligible scans remain stable. The large fallback
regression did not repeat; its cause is unresolved. The complete 33-range
collection controls show no repeatable increase. No speculative production
correction or admission-threshold change was made. See [recheck statistics](fallback-recheck/benchstat.txt)
and [sample ranges](fallback-recheck/spread.csv). A tighter bound on shuffled
fallback cost needs further measurement.

### Workload relevance

A temporary success-path probe recorded zero hits in 20-iteration runs of the
existing `BenchmarkGCCollectionMatrix`, `BenchmarkGCSparseRememberedArray`,
`BenchmarkThroughputMixedLifetimeGraph`, and
`BenchmarkThroughputMixedLifetimeFullGCPressure`. The probe was removed before
building measurement binaries. [Probe output](workload-probe.txt) records all
cases and exit status. Evidence for this optimization remains synthetic; no
application-throughput gain is claimed.

## Validation

Passed locally on AMD64:

- Full native suite, normal and `wago_gcstats`, Go 1.22.12.
- Focused race checks, using synchronous access to each collector.
- Bounded differential fuzzing: 10 seconds, two workers, 4,906 executions.
- Lifecycle/differential parity and cold mark-stack checks on main.
- Public runtime GC tests with pinned WABT 1.0.41; checked collector suite.
- `just test corpus all`, release asset checks, release qualification checks.
- `just lint` and `git diff --check` (final command results recorded with the PR).

`just lint` returns success under the current recipe, but standard-build
staticcheck reports existing findings. The exact same findings reproduce on
main ([baseline staticcheck](baseline-staticcheck.txt)); runtime-tagged staticcheck
passes. No new findings were introduced.

`just test` is **not green locally**. The first Go 1.22.12 run exposed missing
spec inputs and installed TinyGo 0.42's incompatible Go requirement. After
initializing the pinned spec-v3 submodule, using WABT 1.0.41, Go 1.27.1, and
`GOFLAGS=-buildvcs=false`, the only failures were
`TestBuildTinyGoEmbedsArtifactWithoutCompiler` and
`TestBuildTinyGoStripsByDefault`: duplicate `tinygo_task_exit` linker symbols.
The same failures were reproduced on the clean main baseline with the same
tools: [baseline result](baseline-tinygo-link.txt), [full recipe result](just-test-final.txt).
The runtime-tagged CLI gate under Go 1.22.12 also fails the installed TinyGo
version check. Remaining recipe steps were run separately and passed. No tests
were silently excluded or production code changed to fix these tool issues.

ARM64: Linux test binary cross-build and disassembly only. No local ARM64 host
or emulator was available. This is not native ARM64 execution. CI now explicitly
runs tagged GC tests in the six-platform native runtime matrix; normal tests are
selected by the existing complete package tests. The bounded fuzz recipe also
runs the new differential target with two workers. Exact pushed-head CI status is
in the PR description. Pending or failed CI is not full validation.

## Reproduce

Create clean worktrees at the baseline SHA and the final PR head. Copy these
four files from the PR checkout into the baseline checkout (production files
must remain untouched): `struct_card_scan_bench_test.go`,
`struct_card_mixed_bench_test.go`, `struct_card_collect_bench_test.go`, and
`struct_card_fixture_test.go`, all under `src/core/runtime/gc/native/`.

```sh
# In each checkout, use a different absolute output path:
GOTOOLCHAIN=go1.22.12 go test -c ./src/core/runtime/gc/native -o /absolute/path/native.test
# Run from the PR checkout after all builds/tests stop:
docs/performance/struct-card-ranges/measure.sh /absolute/path/base.test /absolute/path/fix.test /absolute/path/results
benchstat /absolute/path/results/baseline.txt /absolute/path/results/optimized.txt
# Focused fallback repeat, same toolchain/CPU/process settings:
BENCH_TIME=1s BENCH_PATTERN='^BenchmarkGCStructCardScanControls$/^fields=4097$/^ranges=(15|16|32|33)$' \
  docs/performance/struct-card-ranges/measure.sh /absolute/path/base.test /absolute/path/fix.test /absolute/path/recheck

GOTOOLCHAIN=go1.22.12 go test ./src/core/runtime/gc/native -count=1
GOTOOLCHAIN=go1.22.12 go test -tags wago_gcstats ./src/core/runtime/gc/native -count=1
GOTOOLCHAIN=go1.22.12 go test -race ./src/core/runtime/gc/native -run 'Test(StructCard|GCStruct|MalformedObjectCard)' -count=1
GOTOOLCHAIN=go1.22.12 go test ./src/core/runtime/gc/native -run '^$' -fuzz '^FuzzStructCardDifferential$' -fuzztime=10s -parallel=2
GOTOOLCHAIN=go1.22.12 go test ./src/core/runtime/gc/native -run 'TestStructCard.*Allocations' -v -count=1
GOTOOLCHAIN=go1.22.12 go test -gcflags='-m=2' ./src/core/runtime/gc/native -run '^$'
GOTOOLCHAIN=go1.22.12 GOOS=linux GOARCH=arm64 go test -c ./src/core/runtime/gc/native -o /absolute/path/arm64.test
```
