# Tiny transient root staging

PR #685 fixes GC-06 without discarding the active collection when a replacement
root source fails. Tiny reads each transient source once per root phase into a
reusable buffer. Only a successful read can replace the epoch, mark colors,
gray queue, or scan cursor. The same staging rule applies to incremental remark.

The buffer reuses `Collector.markStack`, otherwise used only by Throughput.
It stores live Tiny handle indexes, has no effect on the Collector struct size,
and is emptied after success, rejection, or a panicking enumeration. Capacity
is retained until `Close`. Null, immediate, freed, and out-of-range references
do not enter the buffer. Duplicate live roots do enter it: retained storage is
proportional to the largest transient enumeration, including duplicates, with
Go slice capacity rounding. Telemetry already accounts for this buffer capacity.

## Preserved and extended tests

The old failure sources rejected their second invocation. Removing the counting
walk made those sources succeed in the synchronous build and moved their error
to remark in the incremental build. The replacements reject before or after
providing a root on one invocation and record the state and phase they observed.

The existing cycle-preservation assertions remain: epoch, colors and capacity,
gray queue and capacity, scan cursor, root phase, sweep cursor, and sweep limit
must survive rejected input unchanged. They now cover rejection before and
after a root, including idle, unfinished marking, partial scanning, and partial
sweeping. Separate tests cover remark rejection and retry, unsupported nested
root groups, panic cleanup, and the live-object loss found during review.

Epoch tests explicitly start accepted but unfinished cycles before attempting
rejected restarts. Failures alone no longer stand in for an interrupted cycle.
The tests retain graph survival, garbage reclamation, handle reuse, verification,
and later-collection checks. They check all 128 completed starting epochs,
explicitly assert the completed-127 to active-0 transition, and retain repeated
unfinished restarts that expose a full-period alias under the old algorithm.
The existing three-period completed-cycle/handle-reuse test is unchanged.

One-shot tests now include children reachable through a parent, direct roots,
classified roots, composite roots, telemetry, and release on the following
rootless collection. A 4,096-object test checks buffer growth and allocation-free
reuse, followed by a smaller list that must not retain stale roots.

## Mutation checks

Run from the repository root:

```sh
python3 docs/performance/tiny-transient-roots/verify_mutations.py
```

The script uses temporary Go source overlays and never edits the worktree. It
requires the indicated tests to fail through test assertions, so a compilation
error does not count as catching a mutation. All four mutations were caught in
both default and `wago_tiny_nonincremental` builds:

| Mutation | Regression detected |
| --- | --- |
| Restore `mark.go` and `tiny_collect.go` from main before the PR | One-shot roots and their graphs are lost by the double read |
| Restore those files from the rebased PR before staging | Rejected roots change the active cycle and publish partial marks |
| Remove both active-restart color clears | Recovery loses a reachable child |
| Advance the epoch unconditionally on both restart paths | Repeated restarts wrap into stale marks and lose a reachable child |

Historical sources are pinned to main `30919ef8c5e993ed4dcea7af4c2b580e01dbec21`
and the rebased PR `1180bab1d56ae90d75ffff913e596b3ec8829030`. Updated test and
benchmark sources remain identical across all variants. The pinned commits
must be available in the local Git object store to run the script.

## Validation

Passed locally on Linux/amd64:

- All native GC tests, default and nonincremental, each with telemetry enabled
  and disabled at build time.
- `go test -race -count=1 ./src/core/runtime/gc/...`.
- Runtime and public API tests in default, nonincremental, and guard-page builds.
- `FuzzCollectorOperations`, 15 seconds, two workers, 121,717 executions.
- `just lint`, installer tests, and the benchmark module's unit gate.

The complete `just test unit` command was attempted. Root-module tests and the
separately run runtime CLI tests have exactly two failures:
`TestBuildTinyGoEmbedsArtifactWithoutCompiler` and
`TestBuildTinyGoStripsByDefault`. Both fail with the installed TinyGo linker's
`duplicate symbol: tinygo_task_exit`. Both failures reproduce on the unchanged
main production sources in the baseline worktree. They were not skipped or
changed for this PR. The remaining commands in the unit recipe were run
separately after that failure. This is not a claim that the whole unit gate is
green or that local TinyGo validation succeeded.

The pinned spec submodules were initialized. WABT 1.0.41 preceded the installed
1.0.42 in `PATH` for the runtime/public API tests and unit command.

## Performance and memory

Environment: AMD Ryzen 7 8845HS, Linux/amd64, Go 1.27.1, `GOMAXPROCS=1`.
Five 200 ms samples per benchmark and build mode were run serially after other
tests finished. These are focused measurements, not application speedup claims
or statistical significance estimates. Historical variants use the same source
overlays as the mutation checks and the same final benchmark definitions.

```sh
GOMAXPROCS=1 go test ./src/core/runtime/gc/native -run '^$' \
  -bench '^(BenchmarkTinyFullWithDirectRoot|BenchmarkTinyTransientRootStaging|BenchmarkTinyRetainedHighWaterRecovery)$' \
  -benchmem -benchtime=200ms -count=5
```

Repeat with `-tags=wago_tiny_nonincremental` for synchronous collection. Add a
Go `-overlay` mapping the two historical production files for either baseline.

Median nanoseconds per operation:

| Build | Workload | Main before PR | PR before staging | With staging |
| --- | --- | ---: | ---: | ---: |
| Default | Original single-root benchmark | 105.8 | 94.62 | 104.5 |
| Default | Warm buffer, 1 root | 83.20 | 74.45 | 83.89 |
| Default | Warm buffer, 256 roots | 2,415 | 1,716 | 2,341 |
| Default | Warm buffer, 4,096 roots | 37,287 | 26,545 | 36,210 |
| Default | Retained graph, 5 rejected restarts | 80,794 | 87,140 | 80,741 |
| Nonincremental | Original single-root benchmark | 71.87 | 62.51 | 67.57 |
| Nonincremental | Warm buffer, 1 root | 50.54 | 43.72 | 49.80 |
| Nonincremental | Warm buffer, 256 roots | 1,315 | 851.6 | 1,126 |
| Nonincremental | Warm buffer, 4,096 roots | 20,259 | 12,991 | 17,302 |
| Nonincremental | Retained graph, 5 rejected restarts | 37,075 | 39,901 | 37,023 |

Preserving rejected input costs about 10.4% in the original single-root default
benchmark and 8.1% in its synchronous counterpart relative to the earlier PR.
The same samples are close to or faster than main's former double walk.
Large repeated-root lists are about 36%/33% slower than the earlier PR in the
default/synchronous builds. The retained-graph recovery benchmark includes
explicit unfinished-cycle setup and rejected root input in every variant.

All warm-buffer benchmarks report zero bytes and zero allocations per operation.
The original single-root benchmark still reports 24 bytes and one allocation
per operation in all variants because it boxes its root slice inside the loop.
The buffer-specific benchmark boxes roots once outside timing.

First buffer growth has a real cost. The cold-buffer cases deliberately discard
only the root buffer each iteration; object tracing storage remains warm. They
are buffer-growth measurements, not full collector-construction measurements.
Both build modes report the same memory figures:

| Roots supplied | Retained buffer bytes | Bytes allocated during growth | Allocations during growth |
| ---: | ---: | ---: | ---: |
| 0 | 0 | 0 | 0 |
| 1 | 8 | 8 | 1 |
| 256 | 1,024 | 2,040 | 8 |
| 4,096 | 16,384 | 49,784 | 14 |

These sizes describe this Go version's capacity growth. The buffer is reused on
later walks and cycles, including the second incremental root phase.

Raw samples: [main/default](main-default.txt),
[PR before staging/default](pr-before-default.txt),
[staging/default](staged-default.txt),
[main/nonincremental](main-nonincremental.txt),
[PR before staging/nonincremental](pr-before-nonincremental.txt),
[staging/nonincremental](staged-nonincremental.txt).

## Follow-up review: release size and panic telemetry

The size failure at `93622a9e43236372ebe6557ce476985ac5f6fa53` reproduces with
CI's Go 1.22.12 and TinyGo 0.41.1 on Linux/amd64: `runtime-minimal-tiny` was
2,352,560 bytes, 560 bytes above its unchanged 2,352,000-byte budget.

Root walks now carry one compact status through recursive adapters and staging,
then construct the existing error at the caller. The active root sink records
whether staging completed, avoiding a second captured cleanup flag. Pointer
classified roots unwrap directly, and telemetry bookkeeping is excluded when
telemetry is disabled. Root-buffer storage and collection-state guarantees are
unchanged. The complete `scripts/size-card.sh` gate passes with the same pinned
toolchains and stripping options:

| Profile | Before (bytes) | After (bytes) | Budget (bytes) |
| --- | ---: | ---: | ---: |
| manager | 7,970,968 | 7,970,968 | 9,000,000 |
| runtime-standard | 8,130,712 | 8,122,520 | 8,870,000 |
| runtime-minimal | 7,798,936 | 7,798,936 | 8,560,000 |
| runtime-minimal-tiny | 2,352,560 | 2,351,328 | 2,352,000 |

Review also found that a panicking root source during incremental `Step` left
telemetry active after recovery, both at initial marking and at remark. Cleanup
now closes that telemetry cycle as failed. The new regression failed in both
phases before the fix and checks successful retry and telemetry reset afterward.
Another regression checks that rejection inside nested pointer root groups
stops subsequent sources and does not retain partial roots.

Validation includes all four native GC build combinations (incremental and
nonincremental, with and without `wago_gcstats`), the telemetry-enabled GC race
suite, runtime/public API suites in default, nonincremental, and guard-page
builds, targeted TinyGo one-shot/rejection/panic tests, all eight mutation checks,
and lint. Using the pinned Go/TinyGo pair also makes the full `just test unit`
gate pass, including the TinyGo linker tests noted in the earlier validation.

A focused single-root regression check used five serial 200 ms samples per
variant on the same CPU, Go 1.27.1, `GOMAXPROCS=16`, after other checks finished.
The median changed from 101.7 to 97.12 ns/op in the default build and from 63.66
to 63.26 ns/op in the nonincremental build. All samples retained the benchmark's
24 bytes and one allocation per operation. These samples are a focused
regression check; the larger benchmark tables above describe the earlier
staging revision. The comparison overlays only `tiny_collect.go` from the
before-review commit and uses identical benchmark sources.

Raw follow-up samples: [before/default](review-before-default.txt),
[after/default](review-after-default.txt),
[before/nonincremental](review-before-nonincremental.txt),
[after/nonincremental](review-after-nonincremental.txt).
