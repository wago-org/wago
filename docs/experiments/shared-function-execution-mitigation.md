# Shared function compilation: execution mitigation

> Accounting correction (PR #802 review): historical `mapped_bytes_page_accounting` values below count page-rounded code payload, not owned mapping capacity. The raw historical files are preserved. Corrected A/G/H measurements and remaining qualification are in [the review follow-up](shared-function-review-fixes.md). Actual retained A/G mapping capacity is 606,208 bytes; the historical 278,528 bytes understated it by 327,680 bytes. No corrected mapping capacity is asserted for unrerun intermediate revisions.

The execution-focused follow-up removes the deep-block regression. Across the eight fixed migrated synthetic workloads, execution is **12.45% faster than the original pilot and 3.94% faster than the frozen baseline**. This is an equal-weight geometric mean, not production-weighted throughput. Conditional joins remain slower than baseline, and pressure execution remains noisy. Keep these mitigations on the experiment branch; continue revising the pilot before merging.

This supplements [the initial experiment](shared-function-compilation.md). Its frozen corpus, shared ownership boundary, fallback rules, original A/B/C comparison and limitations still apply. No rebase, corpus selection change, merge or remote publication occurred.

## Sources and changes

| Point | Exact commit | Meaning |
|---|---|---|
| A | `31547a885b6770cc0f1718f69bce495b5f3e38ce` | Frozen main baseline |
| B | `9ff39cf3a2aabb9fb2204cdb5d426a6bb59f207a` | Original helper extraction; not retimed here |
| C | `0a577e4514d1b9893e1665cf82fa55b74e6608fa` | Original pilot delivery; production identical to `c33e728ce92f8317469226d3ece0e603dac7ba09` |
| D1 | `662defa26ce65f50335041f84d023700f4e0ff73` | Preserve state through fallthrough blocks |
| D2 | `c9f2fdaadd00b7c36d3fb1b8fdd038504f7c47a4` | Use deeper commutative results as accumulators |
| D | `ddb6abda094f2af9103beb47c4452ad0c43c6424` | Reserve agreement slots only when a function contains an if |

All implementation commits are on `experiment/shared-function-compilation` in `/home/jtenner/.codex/worktrees/shared-function-pilot/wago`. A and C binaries were rebuilt with the identical delivered harness before implementation. Final delivery adds only tests and this report after D; measured production code is D.

1. Plain block entry and exit no longer canonicalize values. Admission excludes branches to blocks, loops, indexed block signatures and non-tail returns, so these blocks have only fallthrough. If entry, else and if exit retain reconciliation. Deferred values and authoritative locations now survive nested plain blocks.
2. For admitted nontrapping commutative add/mul/and/or/xor/eq/ne, materialization can evaluate the deeper right expression into the accumulator first. Spilled and borrowed leaves can remain legal memory operands. Scaled-add selection runs first. Swapping child IDs preserves references and value identities; nested fixed-register operations still trigger authoritative location relookup. Noncommutative operations retain operand order.
3. Admission records `HasIf` in existing summary padding. Functions without an if reserve only return slot zero before temporary spills. Functions with an if retain the canonical operand range and separate temporary slots. This shrinks pressure frames and permits shorter stack offsets.

Both targets continue using the same streaming driver and semantic state. No target receives independent logical state, no full-function IR is introduced, and the opcode subset is unchanged. The two execution optimizations and frame-layout optimization have separate commits. Intermediate code diagnostics are retained; D1 and D2 were not separately timed.

## Measurement protocol

The same 14 fixed modules and inputs are used. Diagnostic hashes confirm identical input bytes and admission counts between C and D: **1,333 of 1,380 functions, covering 23,451 body bytes**, use the shared path. Synthetic coverage is 1,031/1,032 functions and 21,406/21,412 body bytes; existing corpus coverage is 302/348 functions and 2,045/21,069 bytes. Twelve of fourteen modules have byte-identical C/D native output; only deep and pressure change.

Host: AMD Ryzen 7 8845HS, Debian 13, Linux 6.12.111+, Go 1.27.1, native AMD64, GOAMD64=v1. Production builds have no tags, normal optimization and explicit bounds checks. GOMAXPROCS=1, one compilation worker (the public default), GOGC=100, GOMEMLIMIT=off, CPU affinity 2. The performance governor and boost were enabled. Frequency was not locked and ordinary desktop activity was not completely eliminated. Full CPU/Go settings are preserved in `environment.txt`.

Three complete **A/C/D/D/C/A** blocks give six independent fresh processes per revision, 200 ms per benchmark and count 1. All builds, tests and independent review finished before primary timing. There are 43 benchmark rows per process, including fixed synthetic and existing tiny/fib_rec/many_funcs/xxhash/json-as workloads. No sample was discarded. Benchstat supplies medians and 95% confidence envelopes; iterations within a process are not independent samples. Equal-weight aggregate values are computed separately within each process before comparison. Marginal per-workload p values should be interpreted in light of multiple comparisons.

Timing boundaries remain those of the initial report: native compile excludes decode/validation setup, includes analysis/codegen and supported Close; full compile includes decode/validation/analysis/codegen and Close; execution compiles outside timing and includes Invoke with verified outputs. Every compilation calls the compiler, without a compiled-output cache. Public/native outputs are closed and not accumulated in timing loops. The existing JSON-AS execution benchmark retains its same-instance incremental-GC caveat.

Diagnostic builds and fixed-work memory runs are separate from timing. The standalone copied admission microbenchmark was excluded because it does not include the new HasIf summary assignment; actual compiler admission counters/times remain in diagnostic output. No new standalone admission-cost comparison or four-worker comparison is claimed.

## Timing results

Each entry is median with benchstat's 95% envelope, n=6 fresh processes. All aggregate A/D and C/D comparisons below have p=.002. “Full sample” includes the eight migrated synthetic workloads, synthetic fallback and five existing modules.

| Boundary / equal-weight aggregate | A | C | D | C→D | A→D |
|---|---:|---:|---:|---:|---:|
| Migrated execution | 40.88 ns ±1% | 44.86 ns ±1% | 39.28 ns ±2% | −12.45% | −3.94% |
| Migrated native compilation | 52.88 µs ±1% | 46.80 µs ±1% | 43.85 µs ±1% | −6.30% | −17.09% |
| Migrated full compilation | 67.28 µs ±1% | 63.38 µs ±4% | 60.08 µs ±1% | −5.20% | −10.69% |
| Full sample native compilation | 57.76 µs ±2% | 54.19 µs ±1% | 51.79 µs ±0% | −4.44% | −10.33% |
| Full sample full compilation | 72.70 µs ±1% | 70.16 µs ±3% | 67.67 µs ±1% | −3.55% | −6.92% |

Selected execution workloads, same n=6:

| Workload | A | C | D | Interpretation |
|---|---:|---:|---:|---|
| Deep blocks | 14.24 ns ±7% | 38.54 ns ±2% | 14.39 ns ±2% | C→D −62.67%, p=.002; A/D inconclusive |
| Pressure | 44.79 ns ±1% | 47.96 ns ±1% | 45.61 ns ±15% | C/D inconclusive, p=.058; A→D +1.82%, p=.004 |
| Conditional join | 14.55 ns ±2% | 15.22 ns ±1% | 15.21 ns ±1% | C/D inconclusive; A→D +4.53%, p=.002 |
| Many locals | 77.06 ns ±2% | 51.41 ns ±1% | 51.89 ns ±5% | C/D inconclusive; A→D −32.66%, p=.002 |

Deep native/full compilation also improves 28.90%/26.30% from C. Many-functions native compilation improves 6.02% despite unchanged generated output; compiler code/layout effects can affect timing, so not all aggregate compile gains are attributed to fewer emitted instructions. Other migrated execution C/D differences are inconclusive. Small marginal changes in existing corpus execution have identical generated code and do not establish an optimization effect.

The wide pressure interval triggered an additional **one-second** experiment with the same three A/C/D/D/C/A blocks. A is 44.67 ns ±1%, C 48.33 ns ±2%, D 45.48 ns ±13%, n=6. C/D remains inconclusive (p=.065); A→D is +1.81% (p=.002). Both experiments are preserved. The lower C/D median is not reported as a demonstrated gain.

## Generated code and frames

These are deterministic diagnostic values for the changed functions, not timing samples. Scalar spill/reload counts are events, while slot counts are high-water storage. Baseline event counters have different semantics, so baseline values are shown only for comparable code/frame storage.

| Function / revision | Code bytes | Frame bytes | Scalar spills | Explicit scalar reloads | Spill-slot high water |
|---|---:|---:|---:|---:|---:|
| Deep / A | 45 | 0 | — | — | 0 |
| Deep / C | 954 | 40 | 0 | 119 | 2 |
| Deep / D1, D2, D | 46 | 24 | 0 | 1 | 0 |
| Pressure / A | 825 | 232 | — | — | 27 |
| Pressure / C, D1 | 881 | 584 | 36 | 32 | 70 |
| Pressure / D2 | 695 | 584 | 28 | 1 | 70 |
| Pressure / D | 611 | 248 | 28 | 1 | 29 |

Pressure C→D code decreases 270 bytes (30.65%), frame decreases 336 bytes (57.53%), and slot high water decreases 41 (58.57%). Frame storage remains 16 bytes above baseline. Deep loses 908 code bytes (95.18%) and 16 frame bytes (40%). Its remaining 24-byte frame is still above baseline despite execution parity within noise. Join remains 117 code bytes/40 frame bytes versus baseline 85/24.

## Fixed-work memory

Identical memory runners execute 270 compile-and-close operations, then three batches of 36 retained modules followed by Close, reference removal and GC with Runtime alive, then Runtime teardown. A separate native-output scenario records page-rounded code payload. All GC observations are outside latency timing. Inputs stay alive equivalently. No forced OS memory release is used. Six fresh processes per revision run both scenarios; all lifecycle phases, HeapAlloc/Inuse/Objects/Released, allocation deltas, current RSS, code and page-rounded payload are preserved in `memory-samples.json` and `memory-summary.json`.

| Measurement | A median | C median | D median | C→D |
|---|---:|---:|---:|---:|
| Public compile/release allocated bytes, 270 compiles | 37,770,160 | 34,862,488 | 34,774,372 | −88,116 (−0.25%) |
| Public compile/release allocation count | 55,063.5 | 55,778 | 55,690.5 | −87.5 (−0.16%) |
| Native compile/release allocated bytes, 270 compiles | 19,245,136 | 16,287,360 | 16,287,360 | 0 |
| Native compile/release allocation count | 6,295 | 6,925 | 6,925 | 0 |
| Retained 36-module native code bytes | 167,268 | 176,484 | 171,772 | −4,712 (−2.67%) |
| Retained native page-rounded payload | 278,528 | 294,912 | 294,912 | 0 |
| Public HeapAlloc after Runtime release | 211,248 | 206,168 | 205,928 | −240 (−0.12%), overlapping spread |
| Native HeapAlloc after third batch release | 370,488 | 365,408 | 365,168 | −240 (−0.07%), overlapping spread |
| Peak process RSS, KiB | 22,656 | 23,156 | 23,046 | −110 (−0.48%), inconclusive |
| Peak RSS min–max, KiB | 20,432–22,788 | 23,000–25,144 | 21,748–25,072 | Six processes each |

Fractional allocation-count medians arise from an even sample count. Go allocations omit native mappings. Less code does not reduce page-rounded payload here. Owned mapping capacity was not measured by this historical runner. Frame reduction is native function storage, not an equivalent retained-Go-heap saving. Common scratch allocation and retained legacy reservations remain; HasIf fits summary padding and adds no allocated array or per-operation object. Scratch accounting remains an envelope of worker high-water counters, not a simultaneous process peak.

Public single-GC release observations plateau around 1.55 MB; the prior report's extra-GC diagnostic showed delayed output reclamation with Runtime still alive. This follow-up does not reclassify that as engine cache or shared scratch. There is no observed continued per-cycle heap growth in this fixed run. The tiny final-heap differences and overlapping RSS ranges do not establish a retained-memory fix. Current RSS can reflect resident code and retained allocator pages. The earlier 9.6% RSS increase belongs to a different sample set and must not be mixed with these new measurements. No new heap profile was needed to attribute a substantial new retained difference; original profiles remain preserved.

## Correctness and review

Native AMD64 core/compiler/runtime, public API and conformance tests pass with the same pinned WABT 1.0.41 and spec interpreter revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Fixed fixtures and selected corpus correctness pass. Checked focused shared/backend/public tests pass. New tests cover deferred local versions across blocks, a block around a conditional join, both-width commutative trees with nested fixed-register shifts, and actual pressure spills without a join. The final test-only addition exercises compact spill layout for both widths and overflow/sign-edge inputs.

ARM64 cross-build and focused execution under QEMU pass, including the new tests with `wago_regalloccheck`. This is emulated execution, not native ARM64 validation. The independent `contract_review` agent reviewed block/control, value ownership, effect order, commutation and HasIf slot separation and reported no concrete findings. The review is preserved in `independent-review.txt`.

## Remaining downsides, ranked

1. **Conditional/leaf state handling:** join execution remains 4.53% slower than baseline; join frames are 16 bytes larger. Many small functions retain ingress/return frame overhead. Preserve clean local homes and improve frame elision next.
2. **Pressure:** slot and code storage improve substantially, but execution remains about 1.8% above baseline and improvement versus C is inconclusive even in the longer run. Frames still exceed baseline by 16 bytes. Investigate physical allocation/ingress choices before claiming parity.
3. **Compiler allocation:** many-locals native allocation remains 58,888 versus 29,192 B/op (+29,696, 101.73%). Shared state and legacy reservations still coexist. Native fixed-work allocation counts remain 630 above A (+10.01%). Make fallback scratch reservation lazy without weakening admission/fallback contracts.
4. **Retained native/process storage:** fixed-batch page-rounded payload remains 16,384 above baseline (+5.88%). Code is 4,504 bytes above baseline (+2.69%). RSS ranges overlap; exact residency attribution remains incomplete.

The dominant deep-block execution regression is resolved for this corpus. Keep these three changes, but **revise the overall pilot before merge**. No expanded subset, native ARM64 measurements, four-worker rerun, strict JSON-AS per-call state reset, or transfer-level checker for the shared allocator is claimed. Prior full-root installer/CLI failures and the baseline-reproducing full ARM64 QEMU backend crash were not rerun; those qualification gaps remain as documented in the initial report.

## Reproduction and evidence

All new raw evidence is outside the tracked source tree at:

`/home/jtenner/.codex/experiments/wago-sharing-execution-20261003`

`manifest.json` contains exact A/C/D source commits, production/diagnostic binary SHA256 and identical harness hashes. `diagnostics.json` contains A/C/D1/D2/D per-function counters, corpus hashes and code hashes. `measurement-runs.json` records all 36 successful primary timing/memory processes; `pressure-runs.json` records 18 successful longer pressure runs. `*-timing.text/.csv`, `*-aggregates.text/.csv`, raw block files, memory JSON and review/test logs preserve absolute results and uncertainty. Original profiles, corpus catalog and initial A/B/C measurements remain in `/home/jtenner/.codex/experiments/wago-sharing-20261003`.

Use worktrees at A/C/D, and copy the identical five `bench/suite/sharing_*_test.go` harness files from C into A; C/D already contain them. Their SHA256 values must match the manifest. Build all binaries before timing:

```bash
go test -c -o /tmp/A-suite.test ./bench/suite
go test -c -tags=wago_codegenstats -o /tmp/A-diagnostic.test ./bench/suite
# Repeat from C and D with corresponding output names.
cd bench/suite
GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off taskset -c 2 /tmp/A-suite.test -test.run='^$' -test.bench='^(BenchmarkSharing(Native|Full|Exec)|BenchmarkCompile|BenchmarkCompileFull|BenchmarkExec)$' -test.benchtime=200ms -test.count=1 -wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as
GOMAXPROCS=1 GOGC=100 GOMEMLIMIT=off WAGO_SHARING_MEMORY=1 taskset -c 2 /tmp/A-suite.test -test.run='^TestSharing(Memory|MappedMemory)$' -test.v -test.count=1
GOMAXPROCS=1 /tmp/A-diagnostic.test -test.run='^TestSharingDiagnostic$' -test.v -wago.corpus=tiny,fib_rec,many_funcs,xxhash,json-as
# Repeat three A/C/D/D/C/A blocks. Unset WAGO_SHARED_SCALAR.
```

The saved `run_measurements.py`, `resource_wrapper.py`, `run_pressure.py` and `analyze.py` implement the actual order, resource collection and analysis. The focused pressure command changes the benchmark expression to `^BenchmarkSharingExec$/^pressure$` and benchtime to `1s`. Run scripts only after preserving any previous output directory, since their filenames are fixed.

Correctness from the candidate repository root:

```bash
PATH=/home/jtenner/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:$PATH WAGO_SPEC_INTERPRETER=/home/jtenner/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa go test -p 1 ./src/core/... ./src/wago/... ./tests/...
go test -tags=wago_regalloccheck ./src/core/compiler/backend/railshot/shared ./src/core/compiler/backend/railshot/amd64 ./src/wago -run 'TestShared|TestGolden|TestExec' -count=1
GOOS=linux GOARCH=arm64 go test -c -tags=wago_regalloccheck -o /tmp/D-arm64-checked.test ./src/wago
/tmp/qemu-aarch64-static /tmp/D-arm64-checked.test -test.run '^TestSharedScalar' -test.v
```

For missing native ARM64 validation, run the same correctness commands on native Linux ARM64, replace the backend package with `railshot/arm64`, build A/C/D binaries natively, and repeat the same timing/memory blocks on an available physical CPU. Do not use emulated timing as native evidence.
