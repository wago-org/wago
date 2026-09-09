# PR564 compiler scratch memory qualification

This is a measured checkpoint, not a claim that every PR memory metric is below
main. The baseline is main `9708df167`, fetched before this run. The later
`731e95ff2` main update is **not** included in these numbers. Do not relabel these
results as a measurement of that later baseline.

The candidate contains the bounded local reserve, shared dense allocation with
exclusive worker slices, small inline eligibility scratch, and reuse of existing
hint fields for temporary global offsets. The production patch and binary hashes
are in `compact-source.patch` and `compact-metadata.json`. The label `compact`
means the complete memory fix; the older `final` and `reserve` binaries listed in
metadata are development stages and are not the candidate in these tables.
The `before` label is the merged pre-memory-fix PR (`c0780a8d9`), not main.
The later name-validation/startup work is not in these binaries.

## Findings

In the warm checks, JSON parallel compile allocation counts are 1.35–5.87%
below main and Lua counts are 1.18–1.85% below main. Their allocated bytes remain
0.58–1.32% above main. Compiler time is lower in these selected cases. Esbuild's
allocation counts vary across samples; a small positive median is not cleared
merely because it lacks significance.

Ruby instantiation, with exactly 128 operations per process:

| Bounds | Metric | Main | After |
| --- | --- | ---: | ---: |
| Explicit | ns/op | 1,712,950 | 1,567,797.5 |
| Explicit | B/op | 302,066 | 86,630 |
| Explicit | allocs/op | 9,405 | 779 |
| Guard pages | ns/op | 1,713,647 | 1,560,652.5 |
| Guard pages | B/op | 302,068.5 | 86,629 |
| Guard pages | allocs/op | 9,405 | 779 |

Serial full compile still carries validation-summary storage: `many_funcs`
uses about 2,494 extra bytes and two extra allocations per warm operation;
`json-as` uses about 952 extra bytes and two extra allocations. This patch does
not remove that useful metadata to force every memory cell below main.
Cold one-operation counts can differ from warm counts, especially when worker
scheduling changes scratch growth; inspect both tables.

The fixed-work compact corpus has median peak RSS of 158,720 → 157,826 KiB
(explicit) and 160,230 → 159,656 KiB (guard pages). These changes are small and
not significant in this screen; the previous large RSS increase does not recur
at equal work here. Individual RSS results, including increases, remain in the
linked table.

## Full numbers and method

- [All selected metrics](main-vs-after.csv): main, candidate, pre-fix diagnostic,
  deltas, sample counts, bounds mode, and exact rank-test p-values.
- [Equal-work process RSS](equal-work-rss.csv): peak process memory includes
  setup, decoded corpus, Go heap, and native mappings. It is not retained compiler
  memory. Each process does the same number of operations on both versions.
- `compact-explicit` and `compact-signals`: six fresh alternating triples per
  case, 500 ms per timed sample, 11 cases per mode.
- `compact-memory-*`: six triples, one operation per compiler case. These are
  cold allocation checks, **not** timing claims.
- `compact-instantiate-*`: six triples, exactly 128 Ruby instantiations per
  process. Compilation/setup is outside the benchmark timer.
- `compact-rss-*`: six triples, one operation per full compact-compile corpus
  case, in one process. Use this for the earlier process-memory concern.

Linux AMD64, Go 1.27.1, `GOMAXPROCS=8`, `GOGC=100`. Guard-page and explicit builds
are kept separate. No other agent-run builds, tests, or profiles ran during these
timed checks. This is a shared host, not an isolated performance lab. Six samples
are a screen; p-values are unadjusted and do not prove equivalence. A full-suite
comparison of the final integrated head is separate work.

Each run stores identities, commands, status, combined benchmark text, and parsed
samples. Individual process output and resource logs are in
`raw-process-logs.tar.gz`. Scripts contain the original local paths; adjust their
root and binary paths when reproducing. Restore `code_audit_test.go.txt` to `.go`
only at the overlay target, not inside this evidence directory. `SHA256SUMS`
covers every exported file except itself.

## Safety checks

The native and guard-page `./src/...` suites pass. Targeted parallel backend and
shared-accumulator race tests pass. Full ARM64 backend suites pass in both modes
under QEMU with Go 1.22. Tests cover reserves through 65,535 locals, actual growth
past 64 locals, growth beyond both inline scope capacities, independent worker
marks/scores, deterministic serial/parallel code and metadata, and full-width
owner indexes. No feature checks or index limits were removed.

[Code audit](audit-compact-summary.json): all 112 checked AMD64 code pairs match
main byte for byte, including Ruby, wasm3, Coremark, and bulk memory. This is
corpus evidence, not a proof for all modules. ARM64 correctness uses its tests;
these are not native ARM64 performance measurements.

An optimized TinyGo startup failure was found independently of this compiler
allocation change, including on main and on `--version`. It is tracked separately;
these compiler results do not certify optimized TinyGo release behavior.
