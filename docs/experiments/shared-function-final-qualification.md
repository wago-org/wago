# Shared function compilation: rebased final qualification

Latest qualification: [regression mitigation and final measurements](shared-function-regression-mitigation.md). The measurements below are preserved as the previous comparison.

**Recommendation: keep the bounded pilot for review; make PR #802 ready as requested.** The 256-local allocation regression is reversed, and live-value reuse greatly reduces compiler scratch and allocation bytes. Process peak RSS is **not uniformly improved**: the benchmark executable still has a repeatable increase, while standalone runtime results have broad overlapping ranges. This is a measured experiment with remaining costs, not a claim that every workload or memory metric improves.

This report supersedes the current recommendation in [the previous review report](shared-function-review-fixes.md). Historical samples and source hashes remain unchanged there and in [the initial A/B/C experiment](shared-function-compilation.md). Those pre-rebase timings are not evidence about this final branch.

## Frozen sources and changes

The rebase onto fetched `origin/main` completed without conflicts. Main was fetched again before measurement and still matched M. Related open/recently merged PRs were reviewed; the snapshot and range-diff are in the durable evidence directory. Overlapping open work includes #782 compiler resources, #798 checker core, #799 bulk scratch and #800 immutable GP checking. Previously merged trap, GC, EH, ARM64 and plugin fixes are included in M. Unrelated changes in the original checkout remain outside the experiment.

| Point | Exact commit | Meaning |
|---|---|---|
| M (new A) | `0ef007c70581bf56155a4daf6fce8bda3f2c5ff1` | Frozen main baseline |
| B | `e026bfc92918b9576007e2ca1e659e62f42e4a13` | Rebased helper extraction |
| Initial C | `a09625f7cdcca77deba90e60a3c65fc60bd264ca` | Rebased initial streaming pilot; historical comparison only |
| R | `8849505d1ba7b59b14de16302f077f32516d896f` | Rebased reviewed PR before these mitigations |
| Z | `d6e0ff12f056166eae2f426b6653f38f5fd843e5` | Initial declared zeros shared by integer width |
| T | `5c46cccf00f3f2aa1a57a950445ad3d86621ba59` | Separate compact-counter representation stage |
| P (final C) | `95732e7454186fccd788b9c4e883354e6772a31a` | Dead-value reuse and bounded initial reservation |

Z keeps parameter identities distinct and gives each local binding/read its own reference to a shared i32 or i64 initial zero. Old reads survive overwrites. Control reconciliation still stores every required home and restores distinct borrowed local identities. Admission limits and the conservative creation budget are unchanged.

T packs reference count and creation age into the existing four-byte field: nodes remain 32 bytes and ScalarState does not grow. Admission bounds fit uint16; correctness builds check creation bounds and invalid reference transitions. Spill selection uses creation age rather than an arena index.

P reuses an index only after its last reference and deferred edges die. Recursive child release finishes before allocator linkage overwrites fields. The sentinel holds a free-list head and creation counter, reset each function. New values receive new ages, preserving the old spill-victim policy after index reuse. Initial node reservation is at most 64 records; storage grows geometrically with live demand. Replaced backing is counted as discarded. Growth can transiently retain both arrays; the payload counters do not measure that simultaneous peak. Worst-case capacity can overshoot live demand through geometric growth. This changes storage management, not target lowering.

## Shared boundary and admission

Both targets use the actual common streaming body driver for eligible functions. It owns logical stack, live value identities, deferred edges, authoritative locations, owned/borrowed GP registers, local versions/home validity, spills, join agreements and return preparation. Target code supplies banks/reservations/clobbers, legal operands, native instruction selection, ABI offsets, loads/stores/branches/returns, encoding and relocation. It has no independent logical local/value tracker for the admitted body. The existing target stack/local/control implementations remain for fallback.

The subset is unchanged: i32/i64 constants and integer parameters/locals; local.get/set/tee; drop/nop; add/sub/mul, bitwise operations, shifts and integer comparisons; inline-type block/if/else/end; one integer result with implicit or tail explicit return. Bounds remain 256 locals, 512 logical stack entries, 32 controls, 64 KiB body and 16,384 creation-budget units. Deferred depth remains six. Integer widths, memory operand opportunities and direct comparison-to-branch lowering remain supported within this subset.

Loops, arbitrary branches, calls, div/rem, guest memory/global effects, FP/vector/reference operations, indexed block types and custom instructions use admission-before-emission function fallback. GC/EH conditions and source/unwind recording also conservatively exclude the pilot. The admitted subset has no trapping or observable guest effects; effectful/deferred trapping cases stay on the established compiler. There is no mid-function state switch or partial-output rollback. There is no mandatory full-function IR, instruction object array or SSA graph. Value storage now follows live demand rather than every creation event.

Fresh diagnostic runs prove **1,333/1,380 functions and 23,451/42,481 body bytes** use the shared path on both targets: synthetic 1,031/1,032 functions and 21,406/21,412 bytes; selected existing corpus 302/348 functions and 2,045/21,069 bytes. The existing corpus is fallback-heavy by body bytes. The pressure fixture causes 27 AMD64 scalar spills and one explicit reload; memory operands are additional memory traffic. Fresh diagnostics use actual target admission timers, including local-type expansion. The older copied admission-probe benchmark is not used as evidence of current admission cost.

## Correctness and independent review

M and P pass `go test -p 1 ./...`, their full `wago_regalloccheck` equivalents, and the checked bench module suite. P also passed focused ordinary/checked allocator, zero-alias, overwrite/version, join, width and return tests. New tests cover shared-child cascade release, recycled indices versus spill age, repeated large-then-small reuse/reset and cleanup accounting. Existing tests cover register-move cycles, fixed-register nesting, pressure/local reads, spills across joins and trapping/mixed-bank fallback.

Focused public/shared/ARM64 backend execution passes under QEMU in ordinary and checked builds, including both-width zero probes. Darwin and Windows AMD64/ARM64 suite/backend cross-builds pass in both modes. Cross-builds are not execution tests. Native ARM64 hardware and timing were unavailable.

The independent contract reviewer found no concrete defect in the final ownership, effect-order, control or allocator contracts. All three requested coverage gaps were resolved; reviewed source hashes match P. Checked builds add reference/creation checks, but do not establish complete transition-level checking of every common allocation operation.

Strict OS mapping-identity qualification remains unavailable: `WAGO_REQUIRE_MAPPING_IDENTITY=1` reports `invalid argument` from the host naming facility. Ordinary ownership/release tests pass, and retained/released mapping registration counts are recorded. This is not a claim that strict identity qualification passed. Full emulated target-wide execution beyond the focused tests remains incomplete; the historical full-backend QEMU failure is not cleared by cross-building.

## Measurement method

All binaries were built before timing. Identical sharing harness files were used on M/B/R/P; standalone runner bytes are identical. Source, harness, corpus and binary hashes are in the manifests. Fixture selection preceded these mitigations: nine synthetic modules plus tiny, fib_rec, many_funcs, JSON-AS and xxHash, with fixed inputs and verified outputs. The large-then-small fixture is one module, exercising real per-module worker reuse. Representative corpus checks reuse bench/suite and semantic corpus coverage.

Host: Linux AMD64, Ryzen 7 8845HS, Go 1.27.1, GOAMD64=v1, default optimizer, explicit bounds checks, no production build tags. Primary GOMAXPROCS=1, one compiler worker, CPU 4, GOGC=100 and GOMEMLIMIT=off. Normal worker policy is also measured with GOMAXPROCS=4 on CPUs 2–5. Governor/power preference were performance; frequency/thermal conditions and desktop activity were not completely controlled. Build/test/profile work was stopped during measurements. CPU features, kernel and tool versions are in environment.json. THP was left at host policy `[always] madvise never`; no forced memory release was used.

Each cohort uses three complete **M/B/R/P/P/R/B/M** blocks: six independent fresh processes per revision, not six iterations in one process. Primary rows run 200 ms, synthetic execution repeats use 1 s, worker rows use 200 ms. All **192 final processes** passed; no sample was discarded. The separate representation/storage follow-up adds 18 balanced Z/T/P/P/T/Z processes. Profiles and mapping diagnostics run outside these timing cohorts. Statistical results are pointwise exploratory comparisons without multiplicity correction. A significant result is an investigation signal, not automatic acceptance.

Native timing includes analysis, lowering, encoding and supported Close; decode/validation/setup are excluded and immutable decoded inputs are reused. Full timing includes decode, validation, analysis, native compilation and Close. Every iteration calls compilation; the direct backend and public PreparedCompile.Compile path do not return cached compiled outputs. Public compilation defers executable mappings until needed. Execution compiles/instantiates before timing and verifies fixed outputs. Synthetic Invoke/verification and amortized deferred cleanup are included; existing corpus execution excludes cleanup and batches calls. JSON-AS retains the existing per-instance guest allocator state caveat, so its execution row is not strict fresh-guest-state qualification.

## Timing results

Cells below are process medians with benchstat 95% envelopes, n=6 per revision. Aggregates are equal-weight per-process geometric means: eight migrated synthetic cases, five existing modules for compile, six existing exports for execution, and 14 compile/15 execution cases overall. They are not a production traffic mix. `~` means inconclusive.

| Aggregate | Main M | Final P | M→P | Helper B→P | Pre-mitigation R→P |
|---|---:|---:|---|---|---|
| MigratedNative | 61.49µ ±2% | 47.30µ ±2% | -23.07% (p=0.002 n=6) | -22.23% (p=0.002 n=6) | +2.64% (p=0.041 n=6) |
| MigratedFull | 77.56µ ±2% | 65.82µ ±5% | -15.14% (p=0.002 n=6) | -12.60% (p=0.002 n=6) | ~ (p=0.093 n=6) |
| MigratedExec | 45.49n ±3% | 42.02n ±1% | -7.63% (p=0.002 n=6) | -7.55% (p=0.002 n=6) | ~ (p=0.310 n=6) |
| ExistingNative | 112.8µ ±4% | 114.8µ ±2% | ~ (p=0.240 n=6) | +2.00% (p=0.026 n=6) | +2.41% (p=0.026 n=6) |
| ExistingFull | 143.6µ ±3% | 144.1µ ±4% | ~ (p=0.699 n=6) | ~ (p=0.485 n=6) | ~ (p=0.093 n=6) |
| ExistingExec | 5.276µ ±2% | 5.322µ ±2% | ~ (p=0.310 n=6) | ~ (p=0.065 n=6) | ~ (p=0.818 n=6) |
| AllNative | 67.12µ ±3% | 58.38µ ±2% | -13.02% (p=0.002 n=6) | -12.09% (p=0.002 n=6) | +2.90% (p=0.009 n=6) |
| AllFull | 83.72µ ±2% | 76.11µ ±5% | -9.09% (p=0.002 n=6) | -7.63% (p=0.002 n=6) | +1.84% (p=0.015 n=6) |
| AllExec | 284.6n ±1% | 273.5n ±1% | -3.90% (p=0.002 n=6) | -3.44% (p=0.002 n=6) | ~ (p=0.310 n=6) |

Selected native allocation results are deterministic medians across the same six processes. Timing uncertainty is in MP-timing.text; no execution allocation is attributed to compiler scratch.

| Native workload | M B/op | R B/op | P B/op | M→P bytes | M/P allocs/op |
|---|---:|---:|---:|---:|---:|
| small | 8,248 | 7,536 | 7,536 | -712 (-8.63%) | 21/20 |
| pressure | 19,488 | 12,120 | 10,072 | -9,416 (-48.32%) | 18/23 |
| join | 10,152 | 7,936 | 7,936 | -2,216 (-21.83%) | 26/21 |
| large | 228,544 | 72,904 | 9,416 | -219,128 (-95.88%) | 24/18 |
| deep | 11,504 | 8,496 | 8,496 | -3,008 (-26.15%) | 22/21 |
| locals | 29,192 | 20,592 | 14,160 | -15,032 (-51.49%) | 21/22 |
| many | 50,600 | 49,888 | 49,888 | -712 (-1.41%) | 24/23 |
| large_then_small | 275,320 | 119,680 | 56,192 | -219,128 (-79.59%) | 30/24 |
| fallback | 8,248 | 8,504 | 8,504 | +256 (+3.10%) | 21/21 |
| 256 zero locals, i64 | 14,569 | 21,384 | 11,976 | -2,593 (-17.80%) | 16/17 |

The 256 probe drops 21,384→11,976 B/op from R (−9,408, −43.99%), below main's 14,569. Its native compile timing changes 17.14 µs ±5%→15.94 µs ±4% (−7.00%, p=.009). Generated i64 code stays 37 bytes on R/P, versus 2,038 on main; its conservative frame remains. Separate stage captures show Z makes the allocation reduction, T changes neither node size nor B/op, and P reduces live storage. Balanced Z/T and T/P timing tables are preserved; many stage latency differences are inconclusive. They do not support assigning every R/P slowdown to the free list.

## Generated output and scratch

Helper-only B matches all 14 main native hashes on both targets. T/P match all 14 module native hashes, frames and spill/reload counts on both targets, plus both-width zero probes. R/P module hashes also match. Total AMD64 module code is 111,347→103,871 bytes (−7,476, −6.71%). This is native payload, not mapping capacity. The identical stripped standalone runner is M 8,442,016 bytes, P 8,495,264 (+53,248, +0.63%); P is the same size as R.

| Module | M/P code bytes | M/P frame bytes, summed | M/P spill slots, summed | R/P scalar scratch envelope bytes |
|---|---:|---:|---:|---:|
| small | 46/43 | 0/24 | 0/0 | 184/184 |
| pressure | 825/567 | 232/232 | 27/27 | 4,340/2,484 |
| join | 85/84 | 24/24 | 1/0 | 576/576 |
| large | 4,106/3,103 | 0/24 | 0/0 | 64,120/2,072 |
| deep | 45/40 | 0/24 | 0/0 | 1,076/1,076 |
| locals | 2,832/103 | 792/792 | 0/0 | 7,192/1,080 |
| many | 14,865/13,329 | 0/12,288 | 0/0 | 184/184 |
| large_then_small | 18,954/16,415 | 0/12,312 | 0/0 | 64,120/2,072 |
| fallback | 59/59 | 0/0 | 0/0 | 0/0 |
| tiny | 96/102 | 0/24 | 0/0 | 152/152 |
| fib_rec | 320/320 | 40/40 | 2/2 | 0/0 |
| many_funcs | 7,771/8,363 | 0/7,200 | 0/0 | 148/148 |
| json-as | 58,150/58,150 | 2,376/2,376 | 110/110 | 0/0 |
| xxhash | 3,193/3,193 | 120/120 | 1/1 | 0/0 |

Large and large-then-small scalar payload falls 64,120→2,072 bytes (−62,048, −96.77%). This is a worker accounting envelope, not simultaneous process peak RSS. NodeScratch*/ControlScratch*, scalar peak/retained/discarded, hints, stage accounting and function attempts are in diagnostic-summary.csv. Initial scalar backing is lazy, with no separate ScalarScratchReserved counter. Retained diagnostics describe storage still held by a compilation worker at collection, not engine heap after outputs close. Conservative guest frames remain, particularly tiny/many_funcs; these sums are not simultaneous call-stack usage.

Actual admission timers in six fresh diagnostic processes have medians/ranges: small 706 ns [661,1092], pressure 961.5 [921,1012], join 366 [351,381], large 11,416.5 [11,362,15,549], deep 395.5 [371,481], locals 2,510 [2484,3597], and early fallback 656 [421,1293]. These are cold instrumented observations including timer overhead, not copied-probe production microbenchmarks. Many sums 76,704.5 ns over 512 functions, large-then-small 82,789 over 513. The scan remains bounded; large admission is about 9.4% of its production native timing. All samples and attempt counts are preserved.

## Memory lifecycles

Memory runs are fixed-work and separate from latency. Compile/release performs 270 compilations with every output explicitly closed. Each of three retained batches has 36 modules and 4,128 functions. The public compile-only runner keeps Runtime alive; the backend runner measures real owned CodeBuffer mapping capacity. The new standalone production runner also retains **36 live instances**, verifies each output, closes every instance/module, drops references, observes GC with Runtime alive, repeats three cycles, then closes Runtime and observes again. Input retention is equivalent. GC is outside timing; no finalizer or forced page-release assumption is used.

Go allocation totals exclude native mappings. Current RSS can stay high with reusable pages; peak RSS is a high-water mark. Native payload/frame/scratch counters are not physical memory. Actual retained backend mapping capacity is **606,208 bytes** on M/B/R/P; rounded payload is separately recorded. Standalone native registry counts are 36 active/registered when retained and zero after release. Public mapped-byte ownership is not exposed by that API, so its runner reports counts and code bytes rather than inventing mapped capacity.

The table shows medians [fresh-process min,max], n=6, one worker. Allocation deltas are fixed 270-compile totals.

| Scenario / metric | M | R | P | M→P |
|---|---:|---:|---:|---:|
| Public compile-only allocated bytes | 37,770,712 [37,770,560, 37,770,880] | 27,723,200 [27,723,120, 27,723,600] | 23,659,256 [23,659,152, 23,659,504] | -14,111,456 (-37.36%) |
| Backend allocated bytes | 19,246,168 [19,246,160, 19,246,176] | 9,237,168 [9,237,168, 9,237,184] | 5,173,488 [5,173,488, 5,173,504] | -14,072,680 (-73.12%) |
| Standalone allocated bytes | 37,785,988 [37,785,904, 37,786,360] | 27,738,548 [27,738,320, 27,738,776] | 23,674,664 [23,674,368, 23,674,824] | -14,111,324 (-37.35%) |
| Standalone retained code | 167,268 [167,268, 167,268] | 134,972 [134,972, 134,972] | 134,972 [134,972, 134,972] | -32,296 (-19.31%) |
| Standalone retained HeapAlloc | 1,171,120 [1,170,800, 1,176,632] | 1,170,736 [1,170,464, 1,170,896] | 1,171,136 [1,170,736, 1,175,896] | +16 (+0.00%) |
| Standalone retained HeapInuse | 1,884,160 [1,826,816, 1,884,160] | 1,851,392 [1,826,816, 1,875,968] | 1,941,504 [1,916,928, 1,974,272] | +57,344 (+3.04%) |
| Standalone retained HeapObjects | 1,693 [1,691, 1,700] | 1,678 [1,677, 1,679] | 1,680.5 [1,678, 1,683] | -12 (-0.74%) |
| Standalone retained HeapReleased | 3,547,136 [3,522,560, 7,536,640] | 3,907,584 [3,465,216, 7,847,936] | 5,804,032 [3,727,360, 7,979,008] | +2,256,896 (+63.63%) |
| Standalone outputs closed HeapAlloc | 1,079,896 [1,079,576, 1,085,408] | 1,079,608 [1,079,336, 1,079,768] | 1,080,008 [1,079,608, 1,084,768] | +112 (+0.01%) |
| Standalone runtime closed HeapAlloc | 150,568 [150,248, 156,208] | 150,424 [150,264, 150,584] | 150,816 [150,424, 155,584] | +248 (+0.16%) |
| Standalone runtime closed current RSS | 14,512,128 [13,344,768, 15,380,480] | 13,154,304 [12,820,480, 15,450,112] | 13,430,784 [12,943,360, 15,630,336] | -1,081,344 (-7.45%) |

Standalone P released-cycle HeapAlloc medians are 1,080,000, 1,080,000 and 1,080,008 bytes: a plateau in this fixed workload, not evidence of continued growth. Closing Runtime reduces it to 150,816 [150,424,155,584]. Sampled heap profiles attribute retained memory mainly to compiled metadata/export clones and JSON encoder caches, rather than the recycled scalar arena. Sampling estimates are not exact retained-byte attribution. Complete lifecycle allocation-count/byte deltas, HeapAlloc/Inuse/Objects/Released, current/peak RSS and native counts for both worker policies are in raw-memory.csv and memory-summary.csv.

Fresh-process high-water RSS, KiB, n=6 follows. The test runner executes the public and backend scenarios in one process, so its final process peak includes both; it is not deployed-runtime RSS.

| Runner / policy | M median [range] | R median [range] | P median [range] | M→P | R→P |
|---|---:|---:|---:|---:|---:|
| Benchmark binary, one worker | 20,886 [20,588, 22,952] | 22,936 [22,588, 23,092] | 24,026 [22,968, 25,164] | +3,140 (+15.03%) | +1,090 (+4.75%) |
| Benchmark binary, auto | 22,270 [20,420, 22,816] | 24,226 [24,096, 24,572] | 24,446 [22,192, 24,784] | +2,176 (+9.77%) | +220 (+0.91%) |
| Standalone instances, one worker | 41,156 [35,996, 48,648] | 33,860 [15,504, 37,296] | 36,262 [15,584, 37,784] | -4,894 (-11.89%) | +2,402 (+7.09%) |
| Standalone instances, auto | 25,218 [21,508, 35,932] | 25,094 [18,328, 34,996] | 26,566 [21,700, 34,500] | +1,348 (+5.35%) | +1,472 (+5.87%) |

Peak RSS mitigation is therefore **inconclusive overall**. The benchmark binary increase persists; the standalone one-worker median is lower than main, but auto is higher and ranges overlap. P medians are higher than R in all four rows. Scratch/allocation improvements must not be presented as a demonstrated process-RSS reduction. Separate smaps/profile runs observed retained RSS of about 64–88 MiB with 50–78 MiB of anonymous huge pages; instrumentation and address placement change these observations. They demonstrate substantial huge-page variation, but do not uniquely explain the production differences. No host THP policy was changed. The complete primary samples and diagnostic profiles are preserved.

## Remaining regressions and investigation

Ranked by the evidence and practical impact:

1. **RSS qualification remains open.** Combined memory-test process peak is +3,140 KiB (+15.03%) versus main and +1,090 KiB (+4.75%) versus R. Standalone results are mixed/inconclusive. Allocation bytes and scratch improve substantially; retained heap does not show growing compiler state. A physical-RSS win is unproven.
2. **Join execution is slower.** The 1 s repeat confirms 15.84 ns ±4%→17.14 ns ±4%, +1.30 ns (+8.24%), p=.002. Disassembly shows the common canonical local home is stored on either edge and read by the final memory add; main carries the value through registers. This is a remaining conservative join cost, unchanged by the storage mitigations. Pressure execution improves 3.21%, locals 51.45%; other long synthetic rows are inconclusive. Native bytes alone do not predict latency.
3. **Compiler overhead remains.** Full fallback is 10.44→11.04 µs (+5.72%, p=.015); many_funcs native is 414.2→424.0 µs (+2.36%, p=.004); JSON-AS full is 1.783→1.824 ms (+2.32%, p=.026). Main/P fallback and JSON-AS native hashes match; those functions do not use the pilot. These pointwise rows require caution about noise, layout and worker overhead. Existing-corpus aggregate timing versus main is inconclusive; automatic-worker samples are especially noisy and do not prove a speedup.
4. **Mitigations cost compile time in the primary cohort.** R/P all-native aggregate is +2.90%, p=.009; many-native is +6.37%, p=.002. Separate Z/T/P blocks do not reproduce a significant T/P many slowdown (+1.77%, p=.093), while allocation reductions repeat. CPU profiles show much time in function setup/hints and a newly non-inlined add helper; they do not isolate the entire primary difference. Both profiles and the unfavorable primary samples remain available. No per-op allocation growth, new spill/frame/code growth or unbounded scan was found in T/P.
5. **Small storage costs persist.** The i64 256 probe has one additional allocation event (17 versus 16) despite −2,593 bytes (−17.80%). Pressure has 23 versus 18 allocation events, while bytes fall 19,488→10,072. Fallback worker bytes are 8,248→8,504 (+256, +3.10%). Tiny/many_funcs code and conservative frames remain larger than main. Executable size is +53,248 bytes (+0.63%). Source sharing is a maintenance result, not memory evidence.

The design is useful as a bounded shared compiler and the latest changes reduce logical storage costs. Keep it for review with these measured limits. A wider pilot, register-only join state, parameter ingress/leaf-frame work and full common transition checking should be separate follow-ups; none is silently included in this result.

## Reproduction and evidence

Compact raw tables, hashes, commands and profile/disassembly summaries are in [shared-function-final-data](shared-function-final-data/sha256.json). Tracked text/CSV exports normalize line endings and trailing whitespace; original process output remains intact in the evidence directory. Large logs, binaries, profiles, submodule/build caches and full per-function diagnostics are outside tracked source at:

`/home/jtenner/.codex/worktrees/1791067335-346105/wago/.worktrees/pr802-final-evidence`

The branch checkout is its sibling `pr802-final`; frozen worktrees are `pr802-main`, `pr802-helpers`, `pr802-rebased`, `pr802-zero` and `pr802-packed`. They are isolated from the original checkout. Scripts take paths from their own directory; copy them to an external evidence directory with those sibling checkouts. Copy runtime_memory.go.txt as runtime_memory.go without changing bytes. Harness files are bench/suite/sharing_*_test.go from P, applied identically to the other points. Exact commands and binary/source/harness hashes are in the manifests; every process command is in the durable final-measurement-runs.json.

```bash
# Pinned conformance setup; repeat ordinary/checked gates on M and P.
git submodule update --init --depth 1 tests/conformance/spec-v1 tests/conformance/spec-v2 tests/conformance/spec-v3
export GOFLAGS=-buildvcs=false
export GOCACHE=/path/to/evidence/go-cache XDG_CACHE_HOME=/path/to/evidence/cache
export PATH=/path/to/wabt-1.0.41-linux-x64/bin:$PATH
export WAGO_SPEC_INTERPRETER=/path/to/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm
export WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa
go test -p 1 ./...
go test -p 1 -tags=wago_regalloccheck ./...
(cd bench && go test -p 1 -tags=wago_regalloccheck ./...)

# Build each point before any timing; EVIDENCE and REV identify output paths.
go test -c -o "$EVIDENCE/$REV-suite.test" ./bench/suite
go test -c -tags=wago_codegenstats -o "$EVIDENCE/$REV-diagnostic.test" ./bench/suite
go build -trimpath -ldflags='-s -w' -o "$EVIDENCE/$REV-runtime-memory" "$EVIDENCE/runtime_memory.go"

# One production process; script repeats the balanced order in fresh processes.
cd bench/suite
GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off taskset -c 4 "$EVIDENCE/$REV-suite.test" -test.run='^$' -test.bench='^(BenchmarkSharing(Native|Full|Exec|ZeroLocals)|BenchmarkCompile|BenchmarkCompileFull|BenchmarkExec)$' -test.benchtime=200ms -test.count=1 -wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as
GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off taskset -c 4 "$EVIDENCE/$REV-runtime-memory"
GOMAXPROCS=4 GOGC=100 GOMEMLIMIT=off WAGO_SHARING_WORKERS=0 taskset -c 2-5 "$EVIDENCE/$REV-runtime-memory"
# From the evidence directory after every build/test/profile process stops:
python3 run_measurements.py
python3 analyze_final.py
python3 run_stage_repeat.py
python3 profile_regressions.py
```

On native ARM64, check out the same commits and run these same suite, lifecycle and balanced-process commands with an available dedicated CPU affinity; that produces the missing native comparison. Linux ARM64 focused emulation can be reproduced with `GOOS=linux GOARCH=arm64 go test -c -tags=wago_regalloccheck -o arm64.test ./src/wago` and `qemu-aarch64 ./arm64.test -test.run='^TestSharedScalar'`; shared/backend equivalents are in run_cross.py. The emulated results are not native latency measurements.

Incomplete qualification: native ARM64 correctness/performance/RSS; broad full ARM64 execution under emulation; runtime execution on cross-built Darwin/Windows targets; strict host OS mapping identity; full common transfer-transition checker coverage; physical-RSS causality and a uniform RSS reduction; strict reset guest state for JSON-AS execution; conclusions for production workloads beyond this fixed corpus. All available required native AMD64 suites and the fixed comparisons are complete. PR readiness does not erase these limits.
