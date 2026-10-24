# amd64 compile resources, 2026-10-03

## Outcome

The requested 30% compile-latency and 50% peak-memory reductions were **not reached**.
The selected implementation improves compilation latency by **12.00%** geometrically
across 22 fixed workloads versus measurement-time main, and **11.67%** versus the original
pinned main. All 22 latency medians improve. Cold-process peak RSS improves **4.39%**
geometrically versus measurement-time main; the largest input, esbuild, improves **19.99%**
(139.64 MB to 111.73 MB). Small-input RSS is not uniformly lower.

| Metric | Against measurement-time main |
|---|---:|
| CompileFull latency, geometric mean | -12.00% |
| CompileFull Go bytes/op, geometric mean | -20.22% |
| CompileFull allocations/op, geometric mean | -4.37% |
| Cold compile total allocated Go bytes, geometric mean | -19.79% |
| Cold whole-process peak RSS, geometric mean | -4.39% |
| esbuild cold peak RSS | -19.99% |
| esbuild post-GC retained Go heap | -6.04% |

Go allocation traffic, retained heap, and peak RSS are different measurements.
The process peak includes native mappings, input, metadata and runtime overhead;
allocation improvements must not be presented as peak-memory improvements.

## Revisions and repeatability

- Original main: `b70db34c0184bfab4dcc61e59f4f3cd75796d8ff`
- Measurement-time main: `bdf04d32ee8079eb7e21a3975aa11261b175592b`
- Measured selected source: `c6f19404b1cac13618c9e0fc2bfb36d4de68f246`
- Linux/amd64, AMD EPYC 9V74, Go 1.27.1, CGO_ENABLED=0,
  GOAMD64=v1, -buildvcs=false, GOMAXPROCS=1, taskset CPU 0
- Default GC settings; no forced GC inside timed compilation
- Six alternating/permuted timing samples per variant, 150 ms benchtime;
  five alternating fresh-process pairs for each memory comparison
- `final-corpus.json` pins all 22 input paths, sizes and SHA-256 hashes
- Native code sizes and SHA-256 hashes match in all 440 cold samples

The baseline executables use exactly the same benchmark/harness source and build
settings as the candidate. Main advanced to `02bc29a3dcc646ffde3aab73a6b09df1caf38e71` after measurements.
Its additional CLI/plugin lifecycle files do not overlap this patch. This report
does not claim a remeasurement against that newer revision.

The newer measured baseline preserves upstream exception-scope
and pending-tail-call-trap fixes. Comparisons to both baselines are retained.
The source revision is a local recovery commit; publication may use a squashed
commit with an identical production tree and these additional result files.

Timing command from `bench/suite` (substitute the pinned corpus IDs and binary):

```sh
GOMAXPROCS=1 taskset -c 0 ./suite.test -test.run '^$' \
  -test.bench '^BenchmarkCompileFull$' -test.benchmem \
  -test.count=1 -test.benchtime=150ms -test.timeout=0 -wago.corpus="$CORPUS"
```

Cold memory uses `scripts/compilemem/compare.py`. Its lightweight, non-tail-exec
shell launcher was calibrated against direct shell runs to avoid inheriting the
Python parent's RSS high-water floor. No unrelated high-water values are
subtracted. See [memory-report.md](memory-report.md) for ranges and attribution.

## Implementation

- Skip redundant GC-helper and extern-conversion scans using validated requirements
- Reject known fixture recognizers by exact byte length before hashing
- Skip inline caller scans for functions whose existing hints prove no calls
- Recycle amd64 operand arenas at proven-empty ordinary reader boundaries,
  clearing current and cached transient handles; exclude inline readers and profiling
- Preallocate data vectors exactly after existing admission-budget and encoding checks
- Remove the duplicate active-offset validation directory
- Compact large immutable execution data snapshots into pointer-free records while
  preserving public metadata, artifact bytes and mutable-view isolation

No public API or artifact migration is required. Padded malformed data sections
can now allocate the full budget-admitted vector before an early syntax failure;
the preexisting conservative admission budget is unchanged.

## Execution and validation

The full six-round execution comparison has a +0.41% geometric-mean time change.
Most rows are statistically indistinguishable. One initial json-as deserialize
row is +3.74% (p=.026). A ten-pair, one-second focused follow-up did not
reproduce it: 54.78 µs versus 54.65 µs, p=.853. Both datasets are retained;
no reproducible execution regression was demonstrated. All execution allocation counts/bytes
are unchanged.
Generated native code is byte-identical across every corpus input.

Selected TinyGo 0.42.0 optimized minimal release is **2,717,456 bytes**, using the
CI GNU strip recipe, below the unchanged 2,720,000-byte budget. Three fresh version
and fib(20) runs succeed with result 6765. Decoder, register-checker and focused public tests are recorded in validation.txt.
TinyGo checker, encoder, runtime and public-API results are in tinygo-tests.txt.
Earlier pure-Go memory-implementation cross-builds passed for Linux arm64 and
both Darwin/Windows architectures; these were build checks, not native execution. An independent read-only source review found
no blocking issue; it did not independently remeasure performance.

Full repository conformance is not claimed. The sandbox cannot supply supervisor
procfs child-list checks; the pinned official Release 3 interpreter is unavailable,
and WABT alone cannot convert all GC/exception fixtures. Those blocked suites are
not passes. Official v1 on the exact selected source matches the baseline: 55/57 applicable
files fully passing, 16,585 assertions passed, zero failed, 1,598 skipped.
Imports/names remain partial due to duplicate spectest.print_i32 imports.
This is not complete WebAssembly conformance.

## Rejected work and next opportunity

A pointer-free node/allocator redesign reduced allocation slightly further but
only improved full-corpus latency 9.83%, versus 12.00% for the simpler selected
implementation. It is excluded. Smaller arena caps gave mixed timing without
useful peak savings. Early writable native mappings mainly shifted accounting
without reducing true peak RSS; they are excluded too.

Esbuild's 20.68 MB source, 45.07 MB native output and 100,000 active segments
create a substantial retained floor. Further work should reduce overlapping
source, decoded metadata, public mutable metadata and immutable execution snapshots,
for example through a compact canonical representation and delayed materialization
of compatibility views. That would require explicit ownership/mutation and artifact
semantics, plus new measurements; no additional percentage gain is promised.

Raw benchmark output, benchstat output, cold samples and ranges accompany this
report. MB denotes decimal bytes; benchstat's Ki/Mi units remain binary units.

Committed text logs have trailing whitespace normalized and workspace-specific
absolute paths replaced with relative paths; numeric samples and hashes are unchanged.
