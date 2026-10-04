# Native macOS ARM64 qualification of PR #802

The M4 Max run completes the available native ARM64 performance comparison. The shared path improves aggregate compilation and execution and has lower median peak RSS in every fixed-work scenario. Remaining regressions concentrate in small-function compilation, ARM64 code/frame size, and automatic-worker retained heap. **Recommendation: keep the shared boundary, revise these remaining costs; these results do not establish universal non-regression.** The PR remains ready for review; no merge was performed.

Measured commits:

- Main/A: `da456bc1c00c89e42cbfec54d4d93300d8601d85`.
- PR/C: `ba13d00926a70976c5be5f6375a244e0e631fe5f`.
- Previous Linux baseline: `0ef007c70581bf56155a4daf6fce8bda3f2c5ff1`. The main commit used here adds only justfile tasks; compiler source is unchanged. Neither measured revision was rebased during the run.
- The later report/parser-only change does not alter compiler code or these binaries. Helper-only B was measured on Linux; it was not measured on this Mac.

## Evidence and measurement conditions

The downloaded `results.tar.gz` has SHA-256 `ab96e89fc334f56f3c9e33ce8279a908900a0e032c64c1ea67ec712c8d40802f`. All 238 listed evidence-file checksums verified. All built benchmark/fixture hashes match between A and C. Binary hashes, commands, worker policies and exact source revisions are in [manifest.json](shared-function-macos-data/manifest.json) and [runs.json](shared-function-macos-data/runs.json).

Host: Apple M4 Max, 16 logical CPUs, 64 GiB RAM, macOS 26.6.2, native ARM64 Go 1.27.1. Production binaries use CGO=0, default optimization and explicit bounds, GOARM64=v8.0, GOGC=100, GOMEMLIMIT=off, no check/diagnostic tags. Controlled results use GOMAXPROCS=1 and one compiler worker. Normal-policy results use GOMAXPROCS=16 and adaptive workers; the workers cohort also tests one worker at GOMAXPROCS=16.

There are 27 complete balanced A/C/C/A blocks across nine cohorts: three blocks/cohort, six fresh processes/revision/cohort, 108 primary timing/fixed-work processes total. Compilation rows run 200 ms and execution rows 1 s; iterations within one process are not independent samples. Builds, downloads, checks, profiles and instrumentation run outside the primary timings. All samples, including wide outliers, remain in the evidence. Benchstat is pinned to x/perf `v0.0.0-20260908200009-22c9c6c9d4da`.

The laptop was **on battery throughout**, discharging from 85% to 79%. No thermal/performance warnings were reported, but stable power/frequency/core placement is not proven. macOS CPU affinity is uncontrolled. These are one-host results; confirm important marginal regressions in a plugged-in repeat before treating the percentages as portable guarantees.

## Primary comparison

Negative changes favor the PR. Each aggregate is a geometric mean of the prespecified rows within each process, compared across six processes. The shown ± values are benchstat uncertainty; they are not spread across adaptive iterations.

| Area | Main | PR | Change | Samples / uncertainty |
|---|---:|---:|---:|---|
| All native compilation | 35.97 µs | 32.42 µs | −9.87% | n=6 each; ±6% / ±1%; p=.002 |
| Migrated synthetic native compilation | 34.31 µs | 28.68 µs | −16.39% | n=6; ±3% / ±2%; p=.002 |
| All full compilation | 54.59 µs | 51.77 µs | −5.17% | n=6; ±19% / ±4%; p=.004 |
| All execution | 238.8 ns | 232.6 ns | −2.59% | n=6; ±2% / ±1%; p=.002 |
| Migrated synthetic execution | 39.93 ns | 38.17 ns | −4.39% | n=6; ±2% / ±1%; p=.002 |
| Join execution | 11.460 ns | 11.485 ns | +0.22% | bootstrap 95% [−1.51%, +3.12%]; inconclusive |
| Tiny add execution | 10.435 ns | 10.220 ns | −2.06% | bootstrap 95% [−4.17%, +0.88%]; inconclusive |
| 256-local native compile | 8.175 µs | 8.742 µs | +6.93% | bootstrap 95% [−2.96%, +7.88%]; p=.065, inconclusive |

No individual execution row has a statistically significant slowdown in this cohort. The 192-local execution fixture improves 28.40% (90.80→65.01 ns). The two previously problematic execution cases show no detected regression; the intervals still permit small differences. The 256-local timing improvement seen on AMD64 does **not** carry over conclusively here; its allocation bytes improve only 424 B/op, 15,704→15,280 (−2.70%), with 17→18 allocations.

Native compile timing excludes decode/validation and includes backend analysis, generation and explicit output Close. Full compile includes decode, validation, analysis, generation and Close. Existing reusable decoded inputs stay outside native timing. These paths call the compiler directly; no compiled-output cache supplies the result. Execution compiles/instantiates outside timing, verifies outputs and closes outside timing. Synthetic invocations check every result; the existing semantic corpus verifies its oracles before timing. JSON-AS keeps mutable guest state between calls and is not a strict reset-state measurement.

## Remaining regressions, ranked

1. **ARM64 `many_funcs` code and full-compilation storage.** Code grows 9,732→13,332 B, +3,600 B/+36.99%. Function frame totals grow 32→4,832 B; 300 shared functions retain 16-byte homes. Public one-worker full compilation allocates 167,384→195,442 B/op, +28,058 B/+16.76%, and 91→96 allocations. At GOMAXPROCS=16, one-worker full compile increases 277.22→302.88 µs (+9.25%, p=.009; bootstrap [+4.38%, +32.36%]); adaptive workers increase 242.90→253.84 µs (+4.50%, p=.026, with a bootstrap interval crossing zero). At GOMAXPROCS=1, native compile increases 4.92% (p=.041), while full compile +4.64% is inconclusive. The conservative homed-parameter entry explains the frame/code direction; direct attribution of the entire latency/storage change requires further profiling. The next bounded mitigation is register-homing or selective function admission that retains pressure/control workloads.
2. **Small/fallback compile overhead.** `tiny` native compile increases 5.103→5.460 µs, +357.5 ns/+7.01% (p=.015), and +712 B/op/+6.21%, two extra allocations. The fallback fixture increases 4.130→4.370 µs, +240.5 ns/+5.82% (p=.002), and +640 B/op/+5.61%, with no allocation-count increase or native code change. Other unmigrated modules also add about 640 B/op in native compilation. The shared state embedded in worker scratch and admission scanning are plausible contributors; the diagnostic admission clock is not a production timing attribution. Investigate worker-struct size classes and allocation sites before changing the interface.
3. **Synthetic many-small-function full compilation.** `SharingFull/many` increases 553.95→571.25 µs, +17.30 µs/+3.12% (p=.009; bootstrap [+0.90%, +5.90%]). Native time is inconclusive, with only +200 B/op in the full path. Native code grows 20,512→24,608 B (+4,096 B/+19.97%); summed function frames grow 0→8,192 B. These totals are not simultaneously live native stack.
4. **Automatic public-engine retained heap.** After the third release, HeapAlloc increases 1,750,432→1,803,248 B, +52,816 B/+3.02%; HeapInuse increases +143,360 B/+5.22%. Runtime teardown returns HeapAlloc to near parity (521,584→520,160 B). The retained-instance runner has no matching HeapAlloc increase, and current/peak RSS remain lower. Do not label this difference a scratch leak or assume a plateau from three cycles: both automatic revisions grow modestly between cycles.
5. **256-local timing uncertainty.** Its +566.5 ns/+6.93% median warrants a focused repeat, but the bootstrap interval crosses zero and benchstat p=.065. It is an investigation item, not an established regression.

## Memory lifecycle results

Each memory process does 270 compile-and-explicit-close operations, then three fixed batches of 36 modules/4,128 functions kept alive and released. The standalone runner also retains 36 instances with verified outputs. Output references are removed, GC occurs outside latency measurements, the engine stays alive during released observations, and final runtime Close is followed by another observation. No forced memory release or finalizer assumption is used. Corpus/input retention is equal.

| Scenario | Main peak RSS | PR peak RSS | Absolute / percentage change | n=6 range, main / PR |
|---|---:|---:|---:|---|
| Backend, one worker | 15,949,824 B | 15,802,368 B | -147,456 B / -0.92% | [15,810,560, 16,105,472] / [15,532,032, 15,974,400] |
| Backend, adaptive | 17,309,696 B | 16,875,520 B | -434,176 B / -2.51% | [17,170,432, 17,678,336] / [16,515,072, 17,383,424] |
| Public compile, one worker | 17,317,888 B | 16,867,328 B | -450,560 B / -2.60% | [17,121,280, 17,383,424] / [16,777,216, 17,186,816] |
| Public compile, adaptive | 19,972,096 B | 19,447,808 B | -524,288 B / -2.63% | [19,742,720, 20,070,400] / [19,202,048, 20,185,088] |
| Retained instances, one worker | 16,908,288 B | 16,826,368 B | -81,920 B / -0.48% | [16,859,136, 17,235,968] / [16,662,528, 17,022,976] |
| Retained instances, adaptive | 19,595,264 B | 19,480,576 B | -114,688 B / -0.59% | [19,349,504, 19,841,024] / [18,841,600, 19,677,184] |

All medians are lower, but some process ranges overlap. This shows no detected Mac RSS spike in these scenarios; it does not erase the measured Linux default-policy RSS increase or prove lower RSS under every workload/power condition. Current RSS is also lower at the listed retained/released phases; all phases, HeapObjects, HeapReleased, allocation totals and spreads are in [memory-summary.csv](shared-function-macos-data/memory-summary.csv) and [raw-memory.csv](shared-function-macos-data/raw-memory.csv).

For 270 backend compile/release operations at one worker, allocation bytes fall 9,427,032→5,639,096 B, −3,787,936 B/−40.18%, and allocation count 6,223→6,163. Public one-worker allocation bytes fall 28,416,096→24,628,136 B, −3,787,960 B/−13.33%. Standalone one-worker falls 28,426,448→24,639,864 B, −3,786,584 B/−13.32%. Automatic public and standalone batches improve about 7.9%.

The retained 36-module native payload **grows** 212,912→235,968 B, +23,056 B/+10.83%, while owned mapping capacity is unchanged at 851,968 B and becomes zero after release. Go allocation savings do not imply smaller native code. The largest individual growth is small-function homing, while the local-heavy function shrinks 2,948→648 B. One-worker released HeapAlloc stays near parity across all three cycles: public +296 B, backend +24 B, retained-instance runner +1,272 B at the third release. Automatic small increases require a longer plateau investigation if this remains a concern.

Public heap profiles sample retained `CodeBuffer` allocations in the one-worker diagnostic, consistent with engine-owned code-buffer reuse; extra-GC profiles later show no sampled live payload. Sampling does not identify every allocation and these one-worker profiles do not explain the automatic retained-heap difference. Retained heap is not all scalar scratch. Current RSS can stay elevated through page reuse; peak RSS is a high-water value; native mapping capacity and Go payload counters are distinct measures.

## Coverage, scratch and correctness

The diagnostic confirms C migrates **1,332/1,380 functions** and **23,445/42,481 body bytes**. All eight prespecified scalar synthetic modules use the path; the divide fixture falls back. Existing modules migrate 301/348 functions and 2,039/21,069 body bytes. The pressure function records 17 actual spills and 18 explicit reloads on ARM64; its frame stays 160 B. Join uses a 16-byte frame and no spills. The many-function frame totals above are storage accounting, not peak call-stack requirements.

Large and large-then-small shared scratch remains 2,072 B peak/retained accounting; pressure is 2,484 B. The large-then-small fixture includes the successors inside the same module, exercising the compiler's real scratch reuse lifetime. NodeScratch, ControlScratch, scalar counters, hint storage, attempts, spill counts and admission nanos are in [diagnostics.csv](shared-function-macos-data/diagnostics.csv). Scratch peaks sum individual worker high-waters and are accounting envelopes, not simultaneous process peaks. Diagnostics run separately from production timings.

Both revisions pass native ordinary/checked benchmark fixture and selected semantic corpus correctness, all native ARM64/shared backend tests, and scalar/trap public API checks. None of the selected correctness logs contains skipped tests. There are no failed primary measurement processes; the only nonzero metadata command is optional discovery of an Intel-only CPU key on ARM64. This establishes native ARM64 execution testing of the supported subset and selected fallback corpus, beyond the earlier cross-build/emulation evidence.

Incomplete: full Mac repository/conformance checks (`full_tests=false`), native helper-only B comparison, a plugged-in/thermal-controlled repeat, strict reset-state JSON-AS execution, causal profiles of automatic retained-heap changes and compile/code-size regressions, and a long automatic-worker retention plateau. Darwin AMD64 execution remains unmeasured. The independent ownership/effect/control review recorded with the compiler change has no unresolved findings; this report adds no production compiler changes.

## Reproduction and preserved originals

On a native Mac with git, Python 3.9+ and Go:

```sh
./run_me.sh --output "$HOME/wago-mac-results-repeat"
# To add full root tests before benchmarking:
./run_me.sh --full-tests --output "$HOME/wago-mac-results-full"
# Freeze the exact sources from this report:
./run_me.sh --main-ref da456bc1c00c89e42cbfec54d4d93300d8601d85 \
  --pr-ref ba13d00926a70976c5be5f6375a244e0e631fe5f \
  --output "$HOME/wago-mac-results-frozen"
```

Use a new output directory. The runner fetches/freezes commits, uses identical fixture/harness code, downloads the pinned tools and preserves exact commands. Raw benchmark logs, heap profiles, unchanged downloaded report and complete returned archive are preserved outside tracked production source at `.worktrees/pr802-final-evidence/mac-final-20261004/`. The downloaded archive is also unchanged in `/home/jtenner/Downloads/results.tar.gz`.

The original runner's key-row table was empty because Go omits the CPU suffix at GOMAXPROCS=1. Its timing/CSV/benchstat data are valid. The parser now strips only numeric suffixes, preserves `json-as`, and selects key rows with or without a CPU suffix. [corrected-runner-report.md](shared-function-macos-data/corrected-runner-report.md) fixes presentation; all raw timing/memory CSV, summary CSV and aggregate inputs were verified byte-identical during reanalysis. No samples were rerun, dropped or replaced.

Tracked CSV line endings are normalized to LF; the returned archive and preserved originals remain unchanged. The tracked-data `sha256.json` covers those repository copies. `evidence-sha256.json` is the original archive inventory, including files stored only in the preserved originals.
