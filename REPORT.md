# PR #564 correctness and performance qualification

## Bounded final memory follow-up

The [last-pass report](bench/results/pr564-last-pass/README.md) qualifies two
allocation-only changes after `b4f2360f5`: a tighter non-compact parallel join
capacity and stack-backed dynamic-copy patch lists. The 468-process focused
comparison took 217 seconds. Selected esbuild allocated bytes are 5.6–11.1%
below main and allocation counts 7.7–15.1% below main. All selected cold byte
medians are lower; some allocation-count, peak-RSS, and timing losses remain.
All source, race, ARM64/QEMU, and bench checks pass, and all 112 checked AMD64
native-code pairs still match main. This is not a new full-suite qualification;
the following full report remains tied to its earlier measured code.

## Final measured losses against main 731e95ff2

Production code `b4f2360f5` is compared with freshly measured main `731e95ff2`.
The [complete final report](bench/results/pr564-final-main731/README.md) saves
all 16,144 metrics, all raw samples, and every positive timing or memory cost.
All 1,020 full-suite, 4,080 repeat, and 504 fixed-work processes passed.
The full screen has 950 higher Wago timing medians out of 3,000 rows, including
37 unadjusted p < 0.05 warnings. The 170-case repeat has 55 higher timing
medians, including 11 such warnings. These are not zero-regression results.

Memory increases remain in esbuild and SQLite compilation and in some process
peak-memory checks. All costs remain visible, including nonsignificant median
increases and earlier screen losses that reverse in the repeat. Ruby fixed-work
instantiation uses about 71% fewer allocated bytes and 92% fewer allocations;
this does not mean every process-memory result improves. No further tuning or
diagnostic benchmark round was run after the user requested the loss report.

The production allocation fix is pushed. All enabled CI checks pass for that
code; human approval is still required. The report records local Wine failures
on both main and the PR and does not claim native ARM64 speed measurements.

## Integration with main 731e95ff2

The branch also incorporates the new prepared-call and ARM64 hot-path work.
Parallel ARM64 hint scans now collect loop constants in worker-local sparse
storage and merge them in function order, with the serial sidecar capacity
contract. Tests compare enabled/disabled facts, ordering, and backing capacity
with 1, 2, 4, and 8 workers.

The retained dirty-parameter test caught an incompatible new main assumption:
`i32` alone was treated as proof of zero high carrier bits. The merged branch
keeps the recorded machine-value proof rule. Unknown serialized parameters still
get address canonicalization, including across blocks. Proven computed values
still omit the hot clearing instruction; main's cold-tail padding keeps later
function positions stable. This is a correctness restriction on the new main
optimization, not a claim of unchanged ARM64 native output relative to main.

## Compiler scratch memory checkpoint

[The memory report](bench/results/pr564-memory/README.md) records all selected
warm and fixed-work numbers against main `9708df167`. Parallel scratch reuse
reduces allocation counts without narrowing indexes or changing code output in
the checked corpus. Small byte-count and serial summary-storage costs remain and
are listed. The later `731e95ff2` main update requires a separate integration
check; it is not the baseline for these saved numbers.

## Integration with main 9708df167

The branch now incorporates main's regional-residency compiler work. Parallel
hint workers own bounded local-event tapes and write summaries into distinct
function-ordered slots. Serial and parallel scans use the same summary planner,
including detailed diagnostics. ARM64 keeps main's value-version bounds
invalidation and this PR's cached type lookup. Tests compare basic and detailed
serial/parallel sidecars. Earlier benchmark tables remain tied to their original
commits; they do not measure this merged state.

Native CI caught a Windows ARM64 test-build failure: portable hint tests used
a fixture loader defined only in the Linux/Darwin native-entry test file. The
loader now lives with the portable hint tests. No test or platform is skipped.

## Instantiation allocation follow-up — measured checkpoint 3a84fa628

Production code `3a84fa628` removes temporary integer-ABI signature slices and
reduces type-key storage for modules that reuse declared types. Exact admission,
collision, lifetime, data-initialization, and bounds checks remain in place.
Source tests and focused checks pass. The full comparison against pinned main
passed all 1,020 processes. [Full numbers are saved here](bench/results/pr564-absolute-costs/README.md).
All 348 longer-repeat cases and final timing and memory checks are complete.
Ruby instantiation is about 11% faster than freshly measured main. Three small
timing costs (+0.82%, +1.72%, +1.69%) and compiler allocation increases remain.
All 108 executable-corpus byte comparisons match main. The pinned TinyGo
minimal-runtime smoke crashes on main and this checkpoint; this is not a clean release result.
The reports below qualify earlier checkpoints. Later changes require new checks.

## Follow-up regression fixes — 2026-09-08

This checkpoint is `d006d3d10`. Small invocation results now use
Go-owned inline storage, and small compiler arenas and sparse hints use bounded
capacity rules. Validation, feature admission, bounds, native-entry, reference,
and trap checks are unchanged by these follow-up fixes.

The full 1,020-process comparison passed. The three large parallel-call cases
are 65–78% faster than pinned main in focused repeats. Full-pipeline compilation
is about 24–25% faster across the non-ISA corpus. All 37 new timing warnings were
repeated. One guard-mode float case was +1.22% in a 500 ms repeat, then +0.20%
without a clear statistical difference in longer samples. It remains a small
uncertainty, not a claimed fix. Some memory and binary-size costs also remain.

Full native and guard source tests, focused race checks, and the full emulated
ARM64 backend suite pass. Native ARM64 timing and the earlier platform limits
are not cleared. See the [follow-up report and raw numbers](bench/results/pr564-regression-fixes/README.md).

## Initial qualification — production code `16124d763`

The following preserves the initial results. Its six timing regressions and
arena figures describe that checkpoint, not the follow-up implementation.

The correctness fixes are local on `fix/pr564-correctness-performance`.
No changes were pushed or posted to the PR.

Full-pipeline compile time is lower, but 6 timing slowdowns
remain in the focused repeats. This is **not a zero-regression result**.
The full root suite still has two Wine test failures. Native ARM64 latency is
not measured on this AMD64 host.

- Reviewed PR head: `ab29bf3a9ad215833b5138220de6cd7190461a78`.
- Reviewed PR base: `447f057115ee04d9e58580061dbee696becec21f`.
- New benchmark baseline, pinned `origin/main`: `a07de0973191efab1d32677eff527952c7f9cdd2`.
- Qualified production code commit: `16124d7639983fca0243f77486e32dc5ac74ab57`.
  Later test/report/data commits do not change production code.

## Changes

- Typed-reference instructions keep exact admission checks. Heap-dependent
  references also keep exact requirement checks. Type-indexed control records
  multi-value, including zero- and one-result block types.
- Tree-based and mixed modules do not publish incomplete validation summaries.
  Summary storage is private, accessors return copies, and the compilation phase
  requires the validated module to stay immutable.
- Fast admission uses an explicit complete instruction-class list. Targeted
  tests disable each feature separately and check that fast acceptance implies
  exact acceptance. They also cover indexed memory and table64 admission.
- Type caches use one import fill walk, followed by local definitions. Optional
  cache storage has a 1 MiB budget and falls back to exact lookups above it.
  Dynamic-call facts reuse the validator's resolved type cache.
- Parallel hint merge checks the total and allocates its destination once.
  Serial and parallel storage share one capacity rule. Workers do not start
  higher-index work after a known error; lower-index work still selects the
  deterministic error.
- AMD64 tests now require the actual 28-byte hint record and the intended
  52-entry medium-function arena chunk. No field was narrowed to reach 24 bytes.
- Address-fact tests compare results and traps with facts on/off across calls,
  local storage, imported globals, and joins at 63/64/65 locals. ARM64 adapter
  tests compare cached/uncached bytes, entries, call targets, and GC metadata.

## Full native AMD64 comparison

Linux AMD64, Ryzen 7 8845HS, Go 1.27.1, `GOMAXPROCS=8`, `GOGC=100`.
Six fresh process samples per revision, benchmark group, and build/default
bounds mode; 100 ms requested sample time. Baseline/candidate order alternates.
The full default suite includes the generated ISA cases and worker matrices.
Each ratio uses the median of each revision's samples; aggregate ratios are
unweighted geometric means. Negative changes are faster or smaller.

The table excludes ISA cases so they do not dominate the application/compiler
summary. All ISA and per-worker rows remain in the data. Plugin rows are whole
workloads. Row counts are benchmark cases, not independent applications.

| Stage | Non-ISA rows | Explicit build/default | Guard build/default |
|---|---:|---:|---:|
| Decode | 42 | -8.40% | -10.85% |
| Validate | 42 | -7.75% | -14.22% |
| ValidateWorkers | 28 | -3.49% | -10.72% |
| Compile | 42 | -8.40% | -16.39% |
| CompileCompact | 42 | -11.57% | -11.18% |
| CompileWorkers | 36 | -23.83% | -26.91% |
| CompileFull | 42 | -23.47% | -24.86% |
| CompileFullWorkers | 45 | -33.40% | -35.58% |
| CompileMultiModuleThroughput | 6 | -28.20% | -28.36% |
| Instantiate | 36 | +0.97% | -1.29% |
| Exec | 46 | -3.82% | +0.27% |
| ExecParallel | 72 | -2.12% | -0.63% |
| PluginInstantiate | 5 | -1.47% | -1.84% |
| PluginExec | 5 | -0.27% | -0.81% |

The mode label describes the build and runtime default. Direct backend
Compile/CompileCompact/CompileWorkers helpers and the standalone JSON benches
explicitly retain their own explicit-bounds setup in both builds. Decode,
validation, and wazero controls are bounds-independent. Public full-pipeline
and normal runtime cases use the selected bounds mode.

Worker matrices contain repeated modules at different worker settings.
MultiModuleThroughput and ExecParallel measure aggregate throughput (elapsed
time divided by total operations), not single-call or single-module latency.
They must not be read as latency gains for one request.

Unchanged-engine controls also move on this shared host:

| Control, non-ISA geomean | Explicit build | Guard build |
|---|---:|---:|
| WazeroCompile | -1.57% | -1.26% |
| WazeroInstantiate | -1.45% | +7.14% |
| WazeroExec | -0.96% | +1.14% |

These control shifts limit claims about small runtime differences. They do not
provide a correction factor for the compiler results, since their samples were
taken at other times. Host-load records and all control samples are retained.

### Five large modules: public compile pipeline

| Mode | Module | Main time | Candidate time | Time change | Heap change | Allocations/op | Code-size change |
|---|---|---:|---:|---:|---:|---:|---:|
| explicit | json-as | 1.886 ms | 1.357 ms | -28.03% | +0.43% | 405 -> 406 | 0.00% |
| explicit | lua | 26.721 ms | 18.980 ms | -28.97% | +0.17% | 2533 -> 2533 | 0.00% |
| explicit | sqlite3 | 103.516 ms | 72.824 ms | -29.65% | +0.23% | 7909.5 -> 7909 | 0.00% |
| explicit | ruby | 1107.533 ms | 725.186 ms | -34.52% | -2.86% | 40553 -> 40544 | 0.00% |
| explicit | esbuild | 722.025 ms | 486.157 ms | -32.67% | -0.09% | 19625 -> 19623 | 0.00% |
| signals | json-as | 1.726 ms | 1.168 ms | -32.30% | +0.43% | 404 -> 405 | 0.00% |
| signals | lua | 23.884 ms | 16.203 ms | -32.16% | +0.18% | 2524 -> 2524 | 0.00% |
| signals | sqlite3 | 88.932 ms | 62.319 ms | -29.93% | +0.24% | 7882 -> 7883 | 0.00% |
| signals | ruby | 1010.459 ms | 630.011 ms | -37.65% | -2.87% | 40496 -> 40490 | 0.00% |
| signals | esbuild | 649.487 ms | 410.623 ms | -36.78% | -0.09% | 19649 -> 19647 | 0.00% |

Large-module full-pipeline geomeans: explicit
-30.81%, guard
-33.83%.
Large-module backend geomeans: explicit-build
-15.33%, guard-build
-20.62%.

### Focused implementation checks

These are six-sample, 200 ms component A/B medians within the changed source.
They compare the former lookup strategy with the new one; they are not extra
main-versus-PR whole-compiler samples. Setup is outside the timed operation.

| Component | Former strategy | New strategy | Allocation note |
|---|---:|---:|---|
| Dynamic-call facts, 128 type groups | 59.15 ns | 10.425 ns | 0 B/op, 0 allocs/op in both |
| Dynamic-call facts, 4,096 type groups | 1,853 ns | 10.09 ns | 0 B/op, 0 allocs/op in both |
| Type-cache build, 32 imports | 446.05 ns | 204.55 ns | Raw counts retained |
| Type-cache build, 1,024 imports | 285.725 us | 5.688 us | Raw counts retained |
| Type-cache build, 8,192 imports | 18.403 ms | 43.640 us | 196,608 B/op, 1 alloc/op in both |

These results support linear cache construction and reuse of resolved types.
They do not establish a speedup of that size for a whole module.

## Regressions and limits

The first-pass timing screen repeats every positive change with exact rank-test
`p < 0.05`, plus every slowdown above 5%, in 12 alternating-order fresh pairs
with 300 ms requested samples. This is screening across many cases, not a
multiple-comparison-adjusted guarantee. The host also had unrelated active
workloads. Control results and load records are retained.

6 timing rows remain slower with `p < 0.05` in the repeat.
The complete list is in
[confirmed timing regressions](bench/results/pr564/confirmed-timing.md).
All observed increases, including small or unconfirmed ones, are in
[regressions.tsv](bench/results/pr564/regressions.tsv). A flat repeat does not
erase the first-pass result; both are stored.

The largest repeated timing increases are shown here. Parallel rows remain
throughput measurements; an 8 ns/op result is not an 8 ns single-call latency.

| Mode | Benchmark | Main | Candidate | Change |
|---|---|---:|---:|---:|
| explicit | BenchmarkExecParallel/process/swar-pack-parse.parse4 | 8.04 ns | 13.85 ns | +72.18% |
| explicit | BenchmarkExecParallel/independent/fib_iter.fib | 14.36 ns | 16.89 ns | +17.69% |
| explicit | BenchmarkExecParallel/process/xjb-mulhi.mulhi | 13.23 ns | 14.50 ns | +9.56% |
| explicit | BenchmarkExecGlobalGet_wago | 75.67 ns | 80.09 ns | +5.85% |
| explicit | BenchmarkExecParallel/independent/isa_simd_i64x2.shl | 157.05 ns | 163.60 ns | +4.17% |
| signals | BenchmarkInstantiate/zstd | 9.283 us | 9.437 us | +1.66% |

For swar-pack-parse, fib_iter, xjb-mulhi, and the corpus globals module, the
native code hashes, exact entry values, and prepared-call routing flags match
between revisions in eight module/configuration checks. This is narrower than
whole-runtime equivalence and does not attribute the observed timing change.
The corpus globals check is not the standalone GlobalGet microbenchmark.
The 4.17% i64x2 shift row is borderline (p about 0.04995) in this unadjusted
multi-case screen; retain it as a watch item, not a proven source-level cause.

A separate 12-pair, 300 ms check with GOMAXPROCS=1 does not reproduce the three
parallel-call regressions (benchstat p=0.417, 0.514, and 0.503). GlobalGet remains
slower (p=0.004). These samples are not mixed into the eight-worker comparison.

| Single-worker diagnostic | Main | Candidate | Change |
|---|---:|---:|---:|
| BenchmarkExecParallel/process/swar-pack-parse.parse4 | 17.51 ns | 17.47 ns | -0.23% |
| BenchmarkExecParallel/independent/fib_iter.fib | 28.68 ns | 28.86 ns | +0.65% |
| BenchmarkExecParallel/process/xjb-mulhi.mulhi | 18.23 ns | 18.26 ns | +0.19% |
| BenchmarkExecGlobalGet_wago | 77.22 ns | 81.86 ns | +6.01% |

The parallel costs are sensitive to worker count in this harness. Their exact
cause remains unresolved; matching native bytes and a flat one-worker result
do not erase the eight-worker regression. Investigate per-instance data layout
and host-call behavior under concurrent execution before claiming unchanged
runtime performance. This is a follow-up direction, not a proven attribution.

There are 552 observed resource-increase rows. These
include heap bytes, allocation counts, and normalized per-call counters; they
are not all statistically established regressions. The following are up to 20
compiler/setup heap increases above 1%; the full list has no percentage cutoff.

| Mode | Benchmark | Main B/op | Candidate B/op | Change |
|---|---|---:|---:|---:|
| explicit | BenchmarkCompileCompact/isa_call | 20920 | 23608 | +12.85% |
| signals | BenchmarkCompileCompact/isa_call | 20920 | 23608 | +12.85% |
| signals | BenchmarkCompile/isa_var | 23056 | 25184 | +9.23% |
| explicit | BenchmarkCompile/isa_var | 23072.5 | 25184 | +9.15% |
| explicit | BenchmarkCompileCompact/isa_var | 25664 | 27792 | +8.29% |
| signals | BenchmarkCompileCompact/isa_var | 25664 | 27792 | +8.29% |
| explicit | BenchmarkCompileFull/isa_var | 32017 | 34225.5 | +6.90% |
| signals | BenchmarkCompileFull/isa_var | 32018 | 34225 | +6.89% |
| signals | BenchmarkValidateWorkers/ruby/p2 | 397228 | 421812 | +6.19% |
| signals | BenchmarkValidateWorkers/esbuild/p8 | 5430457 | 5682494 | +4.64% |
| signals | BenchmarkCompile/isa_bulk_mem | 11176 | 11400 | +2.00% |
| signals | BenchmarkCompile/linked_list | 14220.5 | 14499.5 | +1.96% |
| explicit | BenchmarkCompile/isa_bulk_mem | 11276 | 11492 | +1.92% |
| signals | BenchmarkCompileFullWorkers/json-as/p8 | 829696 | 844651 | +1.80% |
| signals | BenchmarkCompileWorkers/sqlite3/p8 | 16463940.5 | 16749732 | +1.74% |
| signals | BenchmarkCompileFull/isa_bulk_mem | 22585 | 22953 | +1.63% |
| explicit | BenchmarkCompileWorkers/json-as/p8 | 748049.5 | 760231.5 | +1.63% |
| explicit | BenchmarkCompileFull/isa_bulk_mem | 22633 | 23001 | +1.63% |
| explicit | BenchmarkCompileFullWorkers/json-as/p8 | 875844.5 | 889962 | +1.61% |
| explicit | BenchmarkCompileCompact/isa_bulk_mem | 13984 | 14208 | +1.60% |

ExecParallel also reports small allocation-counter increases, such as 2 -> 7
B/op for independent matmul. Its timed RunParallel setup allocates worker and
closure state once, then amortizes that cost over the calibrated iteration
count. These values are not isolated steady-state call allocations. All such
raw counter changes remain in the data, with this interpretation limit.

Separate one-iteration instrumented checks locate part of this small-module
cost in the changed arena policy: reserved operand-node storage for isa_call is
6,776 -> 8,400 bytes; for isa_var it is 13,888 -> 14,336 bytes. isa_var's hint
sidecar is 32 -> 116 bytes under the common serial/parallel capacity contract.
These counters explain part, not all, of Go's heap increase. Their instrumented
times and heap totals are excluded from the normal comparison.

Across 298 paired native-code-size rows, 0 sizes
changed and 0 grew. Size equality does not prove byte equality
or semantic equivalence. ARM64 changed native output in the PR and must not be
described as compile-time-only. Native ARM64 speed was not measured on this host;
QEMU checks are correctness/code-size evidence only.

The new ARM64 explicit-bounds census has 60 paired modules, including
ISA fixtures: 27 shrink,
33 are equal, and 0 grow.

| ARM64 module | Main code bytes | Candidate code bytes | Change |
|---|---:|---:|---:|
| json-as | 72,648 | 71,048 | -2.20% |
| lua | 953,212 | 915,100 | -4.00% |
| sqlite3 | 3,746,864 | 3,539,664 | -5.53% |
| ruby | 37,600,848 | 36,201,472 | -3.72% |
| esbuild | 30,906,196 | 28,501,376 | -7.78% |

### Built binary footprint

Both revisions use the CI profile flags with Go 1.22.12, TinyGo 0.41.1,
LLVM 20.1.1, CGO disabled, and version 0.0.0. VCS stamping is disabled for the
detached baseline worktree. Tool versions, exact commands, and failed initial
VCS-stamping attempt are retained. Build times are not part of the JIT timing
comparison. Every candidate profile is below its checked-in byte budget.

| Profile | Main bytes | Candidate bytes | Delta bytes | Change | Budget bytes |
|---|---:|---:|---:|---:|---:|
| manager | 7,884,952 | 7,897,240 | +12288 | +0.16% | 9,000,000 |
| runtime-standard | 8,077,464 | 7,938,200 | -139264 | -1.72% | 8,870,000 |
| runtime-minimal | 7,762,072 | 7,618,712 | -143360 | -1.85% | 8,560,000 |
| runtime-minimal-tiny | 2,229,056 | 2,245,920 | +16864 | +0.76% | 2,317,000 |

Execution allocation units need care: the batched harness normalizes time per
call but leaves Go's B/op and allocs/op counters per batch. The analysis adds
derived B/call and allocs/call from each sample before taking medians.
PluginExec stops Go's allocation timer; its printed zeros do **not** establish
allocation-free plugin execution.

### Process memory and run coverage

| Revision | Build/default mode | Processes | Failed | Largest RSS |
|---|---|---:|---:|---:|
| base | explicit | 252 | 0 | 1171.3 MiB |
| candidate | explicit | 252 | 0 | 1267.0 MiB |
| base | signals | 258 | 0 | 1980.6 MiB |
| candidate | signals | 258 | 0 | 2063.1 MiB |

RSS includes fixtures, Go heap, native mappings, and test infrastructure for the
whole group. Faster code can also run more iterations in the requested time.
It is not an isolated hint-scan peak measurement or a per-operation footprint.
Worker buffers still overlap the final hint destination; one destination
allocation removes intermediate merge growth but not that overlap.

The 139 focused repeat groups add 3,336 successful processes. The separate
one-worker diagnosis adds 96 successful processes. The repeat runner was drained
and paused once for correctness, size, and tool checks; none overlapped a timed
sample. One wrapper-duration field includes that pause; its benchmark and GNU
time measurements finished before the pause work. See `confirmation-pause.json`.

The initial unsplit baseline process received SIGKILL during CompileCompact;
the cause was not established. Its subsequent guard run was stopped. These
failed/incomplete captures are retained and excluded from the paired comparison.
The fresh-process comparison uses identical boundaries for both revisions.

The optional external Impart `sqli.wasm` fixture was unavailable, so its row
is visibly skipped. The candidate-only optimization-ablation matrix is opt-in
and was not enabled; it has no baseline counterpart. Neither is silently counted
as a measured case. The guard-only memory benchmark is included separately.

## Correctness qualification

- Focused validator, frontend, shared, AMD64, and runtime regression tests pass.
- Native compiler race tests pass.
- Native runtime tests pass with the pinned reference interpreter.
- Guard-page runtime tests and corpus differential tests pass.
- Benchmark-module tests pass.
- Core 2 spec run: 1,600 modules, 48,331 assertions, zero failures or gaps.
- Selected ARM64 backend and runtime tests pass under QEMU, including explicit
  and guard-page address-fact cases. This does not replace native ARM64 testing.

The full root `go test ./...` run was **not green**. In its final Go 1.22.12 run,
all packages other than the root package pass. Its two Wine test failures are installer
checksum verification and an unsupported directory operation during install.
The local Wine 10.0 certutil check exits zero without printing a hash for a known
file. Supplying official Windows curl in a temporary directory fixes download
availability but does not fix checksum verification. The curl source and digest
are retained in `wine-tools.json` ([publisher](https://curl.se/windows/)).
No check was weakened.

The earlier installed TinyGo 0.42.0 / Go 1.27.1 duplicate-symbol failure is
resolved with the CI-pinned TinyGo 0.41.1 / Go 1.22.12 pair; the full standalone
package passes. Pinned WABT/reference-interpreter and scoped CLI settings resolve
the other setup failures. The stronger mixed-local test passes on AMD64 and
emulated ARM64 in both bounds modes. An ARM64 corpus test launched from the
wrong directory failed; its rerun from the correct package directory passes.
The retained failed captures and successful reruns are included, with a
[test-command summary](bench/results/pr564/test-summary.md).

## Evidence retained for later comparisons

[Data and commands](bench/results/pr564/README.md) include raw captures,
all sample medians, exact rank-test screens, benchstat output, focused repeats,
per-process exit status/RSS, host/tool details, test logs, and SHA-256 checksums.

[Reported-metric inventory](bench/results/pr564/inventory-summary.md) indexes
1,036 numeric/performance-claim lines from the PR description, report, commit
messages, and attached CI size artifact. It keeps all historical sources and checkpoints separate from this
qualification. The historical 24-byte claim and unchanged-ARM64-code claim do
not describe the reviewed implementation.
