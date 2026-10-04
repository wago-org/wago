# ARM64 leaf and terminal-cleanup mitigation for PR #802

**Recommendation: keep these fixes and the bounded shared pilot, with the remaining costs below explicitly accepted.** ARM64 parameter ownership removes the measured leaf code/frame growth. Batched terminal cleanup removes most of the new 256-local cleanup cost. The final native AMD64 comparison improves aggregate compilation and execution; it does not prove universal non-regression. Updated native Mac timing/memory is still not measured.

## Frozen revisions and evidence

- Original A: `0ef007c70581bf56155a4daf6fce8bda3f2c5ff1`; helper-only B: `e026bfc92918b9576007e2ca1e659e62f42e4a13`. Their original three-stage comparison remains in [final qualification](shared-function-final-qualification.md).
- Fresh main/A here: `da456bc1c00c89e42cbfec54d4d93300d8601d85`. Its only changes since the original baseline are justfile tasks; compiler code is unchanged. It was fetched again after measurements and remains the exact main head.
- Previous PR/C: `ba13d00926a70976c5be5f6375a244e0e631fe5f`, measured on the returned M4 Max run and in this new comparison.
- Incoming-register/frame fix: `be218129c2c56c0d5bbc60263f847c705ca973e7`; CI/TinyGo qualification Q: `502d8b1a40cae2d995e304df1c4acaafec43f7ee`.
- Batched cleanup: `eb91956185a0e752a52bd8090d72fcadfab408f8`; final measured R: `2392a43173e5fc343d776fde8bc3dec6c309151f`. Its last commit adds the interleaved-alias test and runner workspace fix; production compiler code matches the batch commit. No source rebase or production change occurred during a comparison.

[Build manifest](shared-function-leaf-data/build-manifest.json), [all commands/processes](shared-function-leaf-data/final-measurement-runs.json), [raw timing](shared-function-leaf-data/raw-timing.csv), [raw memory](shared-function-leaf-data/raw-memory.csv), [diagnostics](shared-function-leaf-data/diagnostics.csv), [independent review](shared-function-leaf-data/independent-review.txt) and [data checksums](shared-function-leaf-data/sha256.json) preserve the compact evidence. The manifest includes binary, harness and corpus SHA-256 values. Large logs, binaries, CPU/heap profiles, the earlier inconclusive Q cohort and disassembly remain outside tracked source at `.worktrees/pr802-final-evidence/leaf-repair-20261004/` (relative to the original checkout, not this isolated clone). No unfavorable samples were removed.

## Boundary and fixes

The existing shared direct streaming driver remains authoritative for scalar value identities, stack and deferred expression edges, locations/register ownership, local validity, spills, agreements and final return preparation. Targets supply register banks/constraints, legal operands, selection/encoding, branches, loads/stores and physical ABI. Admission completes before emission; unsupported functions use the established target compiler throughout. No full-function instruction array or SSA graph is added.

The subset remains i32/i64 constants, get/set/tee, admitted nontrapping integer arithmetic/bit operations/comparisons/conversions, single results, structured scalar joins and actual pressure spills/reloads. Loops, unsupported effects/types/control, trapping/memory/GC/EH paths and source/unwind-recording configurations fall back. Deferred effects on fallback keep their established ordering. Simple multi-argument leaves retain the established fast path; functions within the scalar boundary can use all eight mixed-width ARM64 argument registers when otherwise eligible.

ARM64 now transfers incoming GP registers to shared ownership rather than always homing/reloading them. A transferred value has no implicit memory home: eviction creates a real spill, and a control agreement establishes homes before borrowing/restoration. Terminal return drops local roots while retaining the result and deferred edges, allowing arithmetic directly in the return register. Raw incoming i32 carriers are marked in the existing tagged register payload and normalized only when still raw at return. Nodes remain 32 bytes. Frame elision requires no actual frame access/spill/call and honors the existing policy option; it cannot hide an uninitialized parameter slot. AMD64 keeps its existing ingress ABI.

A terminal group of consecutive identical local IDs now subtracts its binding roots in one operation before the ordinary final release. It stays bounded O(locals), adds no allocations and preserves later groups/result roots. The direct A,A,B,B,A,A test guards spill cleanup and older live values. TinyGo uses explicit pre-emission fallback for the pilot; shared helper extraction remains enabled. This tradeoff saves 16,720 B in the lean release relative to the failed prior CI build. All four final local release profiles pass unchanged budgets, including TinyGo 2,817,584/2,820,000 B (2,416 B headroom).

ARM64 disassembly proves the old extra store/reload/result move, plus frame adjustments. The ordinary add leaf now matches the established instruction bytes. Module hashes can differ through instruction offsets/register choices, so equal size is not claimed as whole-module byte identity. AMD64 code/frame/spill totals match the previous PR; several hashes change through register choices at return.

| ARM64 fixture | Main code / summed frames | Previous PR | Final code / summed frames |
|---|---:|---:|---:|
| many_funcs | 9,732 / 32 B | 13,332 / 4,832 B | 9,732 / 32 B |
| many (512 leaves) | 20,512 / 0 B | 24,608 / 8,192 B | 16,416 / 0 B |
| large then small | 24,612 / 0 B | 28,712 / 8,208 B | 20,504 / 0 B |
| 192 locals | 2,948 / 1,552 B | 648 / 1,552 B | 124 / 0 B |
| pressure | 672 / 160 B | 524 / 160 B | 516 / 160 B |

Main/previous columns are the frozen native Mac diagnostics; the final column is the Linux ARM64 emission diagnostic executed under QEMU with identical fixture hashes. The separate Linux ARM64 baseline/parent leaf disassembly corroborates 9,732/13,332 B. These are generated-storage results, not new native Mac timing. Pressure still has 17 spill slots and 17 explicit reloads (one fewer ingress reload than the previous PR); join retains its real 16-byte frame with no spills. Summed frames are not a simultaneously live call stack.

Both targets still migrate **1,332/1,380 functions and 23,445/42,481 body bytes**. The eight migrated synthetics account for 1,031/1,032 functions; the explicit divide fixture falls back. Existing corpus coverage is 301/348 functions and 2,039/21,069 bytes, including 300/301 many_funcs functions. Tiny still has a shared constant-only second function while add falls back. Admission diagnostics remain separate: final AMD64 totals are 802 ns for small, 11,962 ns for large, 45,013 ns for 512 leaves, 26,438 ns for many_funcs and 441 ns for fallback. These single instrumented observations include clock cost and are not production admission-only benchmark samples.

## Final native AMD64 measurements

Host: AMD Ryzen 7 8845HS, 16 logical CPUs, Linux 6.12.111+deb13-amd64, Go 1.27.1. Production tags none, CGO=0, GOAMD64=v1, default optimization/explicit bounds, GOGC=100, GOMEMLIMIT=off. Controlled compilation uses one worker/GOMAXPROCS=1 pinned to CPU4. Normal adaptive-worker comparisons use GOMAXPROCS=4 on CPUs2–5. Existing performance governor/boost and transparent huge pages (`always`) are recorded in [environment](shared-function-leaf-data/final-environment.json); power/thermal behavior is not actively fixed. Desktop apps remained open. No other agent builds/tests/profiles ran during measurement.

There are **126 fresh processes**, seven cohorts, three complete balanced A/C/R/R/C/A blocks/cohort and six processes/revision/cohort. Timing is 200 ms/row; a prespecified eight-row execution cohort also runs 1 s/row. Independent samples are processes, not adaptive iterations. Benchstat is x/perf `v0.0.0-20260908200009-22c9c6c9d4da`; all raw samples and wide outliers remain. Earlier Q timing had wide variation and was inconclusive; it remains preserved and is not pooled with the final run.

Native compilation excludes decoding/validation/setup and includes backend analysis, generation and Close. Full compilation includes decode, validation, analysis, native compilation and Close. Reusable decoded input is outside native timing and validated across repeated compiles. Backend/public Compile paths invoke compilation directly, without artifact-cache lookup. Execution compiles/instantiates/validates outputs outside timing and closes outside timing. Synthetic calls verify each result; corpus oracles are verified separately. JSON-AS remains stateful rather than a strict reset-per-iteration fixture.

| Area | Main/A | Final/R | Change | Independent samples / uncertainty |
|---|---:|---:|---:|---|
| All native compile | 58.73 µs | 51.22 µs | -12.78% | n=6; bootstrap 95% [-14.06%, -10.44%]; p=.002 |
| Migrated native compile | 54.07 µs | 42.25 µs | -21.86% | n=6; bootstrap 95% [-22.73%, -19.61%]; p=.002 |
| All full compile | 73.88 µs | 67.81 µs | -8.22% | n=6; bootstrap 95% [-9.84%, -7.00%]; p=.002 |
| All execution | 269.86 ns | 258.16 ns | -4.33% | n=6; bootstrap 95% [-5.37%, -1.07%]; p=.009 |
| 256-local native compile | 14.64 µs | 12.35 µs | -15.64% | n=6; bootstrap 95% [-25.19%, -7.69%]; p=.002 |
| many_funcs full, one worker at P=4 | 401.44 µs | 424.65 µs | +5.78% | n=6; bootstrap 95% [+2.75%, +8.07%]; p=.002 |
| many_funcs full, adaptive at P=4 | 345.00 µs | 333.72 µs | -3.27% | n=6; bootstrap 95% [-5.66%, -1.40%]; p=.002 |

Geometric means are formed from the prespecified rows within each process. The final/prior-PR aggregate changes (native −0.52%, full −0.61%, execution +0.65%) are all inconclusive (p=.310/.093/.394). The long execution comparison detects no slowdown; pressure improves 0.45% (p=.004), while other rows are inconclusive. One final small-execution process is a large outlier (benchstat ±71%); its cause is unknown and it is retained. [All benchstat tables](shared-function-leaf-data/A-R-timing.text) and [prior-PR comparison](shared-function-leaf-data/C-R-aggregates.text) provide each workload/allocation row.

The 256-local probe allocates 14,568→11,976 B/op (−2,592 B/−17.80%), with 16→17 allocations. Final versus prior PR is 12.404→12.352 µs, −0.42%, inconclusive (p=.818), with unchanged allocation bytes/counts. The separate fixed-million-compilation CPU diagnostic showed Q terminal return at 4.99% cumulative/release 3.98% flat; after batching R is 1.46%/0.086%. Diagnostic latency C/Q/R was 11,889/12,456/11,804 ns, not independent primary samples or a causal timing confidence interval.

## Fixed-work memory and remaining regressions

Every process compiles/releases 270 modules, then retains/releases three equal batches of 36 modules/4,128 functions. Engine/input lifetimes are equal. Output Close is explicit; references are removed and GC occurs outside timing. Backend mapping and public phases share a fresh process in the combined cohort, so its process peak covers both tests; scopes are preserved separately in CSV. The standalone runner additionally retains 36 instances with verified outputs. No forced page release is used.

| Process scenario | Main median peak RSS | Final median | Absolute / percent | n=6 process ranges, main / final |
|---|---:|---:|---:|---|
| Public + backend, one worker | 19,922,944 B | 19,963,904 B | +40,960 B / +0.21% | [19,587,072, 21,831,680] / [19,709,952, 21,774,336] |
| Public + backend, adaptive | 21,096,448 B | 20,967,424 B | -129,024 B / -0.61% | [19,279,872, 21,573,632] / [19,197,952, 21,159,936] |
| Retained instances, one worker | 92,680,192 B | 91,387,904 B | -1,292,288 B / -1.39% | [91,435,008, 93,982,720] / [90,558,464, 93,122,560] |
| Retained instances, adaptive | 92,715,008 B | 93,118,464 B | +403,456 B / +0.44% | [91,250,688, 93,736,960] / [93,003,776, 93,356,032] |

Ranges overlap. This final cohort does not reproduce the older approximately 9% Linux peak-RSS spike, but does not prove it impossible. Current RSS is phase-specific; final adaptive retained-instance release is +483,328 B/+2.99%, while its peak is +403,456 B/+0.44%. Peak RSS is a process high-water mark and Go allocation totals omit native mappings.

The final 36-module AMD64 payload falls 167,268→134,972 B (−32,296 B/−19.31%); backend mapped capacity is 606,208 B in both and zero after explicit release. The standalone native registry reports zero active and one cached mapping after release in both revisions; capacity is 4,096 slots. A released output count of zero does not imply every process cache is unmapped.

For 270 one-worker backend compilations, allocations fall 19,246,176→5,173,488 B (−14,072,688 B/−73.12%) and 6,303→5,883 allocations. Public allocations fall 37,770,856→23,659,456 B (−37.36%); retained-instance allocations fall 37,785,964→23,675,992 B (−37.34%). These savings are not retained-heap or RSS savings.

Ranked remaining costs:

1. **many_funcs full compile with one worker at P=4:** 401.442→424.646 µs, +23.204 µs/+5.78% (p=.002, bootstrap [+2.75%, +8.07%]). Four extra allocations (84→88), allocation bytes 179,222→179,632 B (+410/+0.23%). Shared admission plus mixed shared/fallback setup and per-function ownership remain costs; the ARM64 homing code fix does not remove those common costs. Adaptive workers improve 3.27%, so this is a worker-policy tradeoff, not a universal module slowdown. No extra scanning or new backing allocation was added by these fixes.
2. **Retained heap/in-use pages at defined phases:** after the first 270 public releases, one-worker HeapAlloc is 1,046,312→1,372,312 B (+326,000/+31.16%); it is essentially identical to the prior PR (−240 B). After the third batch release, public HeapAlloc is −10,584 B/−0.68%, but HeapInuse +69,632 B/+3.27%. Retained-instance HeapInuse is +131,072 B/+7.62% with HeapAlloc only +1,200 B/+0.11%. Adaptive final HeapAlloc is +11,380 B/+0.96%, and +11,372 B/+4.34% after runtime Close. All phase spreads, HeapObjects/HeapReleased and allocation deltas remain in [memory summary](shared-function-leaf-data/memory-summary.csv).
3. **Tiny/fallback compilation:** tiny native median +913 ns/+8.60% is inconclusive (p=.180; bootstrap [−11.01%, +21.33%]), fallback +316.5 ns/+3.67% is inconclusive (p=.065). Allocation bytes increase tiny 8,344→8,672 B (+328/+3.93%, +2 allocations), fallback 8,248→8,504 B (+256/+3.10%, unchanged count). Prior native Mac costs remain established historical evidence; updated Mac costs have not been measured.
4. **Native storage on unmigrated/AMD64 paths:** final AMD64 many_funcs is 8,363 B versus main 7,771 B (+592/+7.62%), frames 7,200 versus zero. This is unchanged from the prior PR, while ARM64 growth is eliminated. Frame/accounting sums are not compiler scratch or simultaneous stack requirements.

Separate precise heap profiling (`memprofilerate=1`, both revisions equally) attributes surviving phase payload mainly to compiled metadata/snapshots/entries and heap code buffers, rather than scalar scratch. Profiling itself changes allocation/GC behavior and is not primary memory evidence. An equally applied extra-GC diagnostic, keeping engines alive, drops A/R heap from 1,550,112/1,544,112 B after the first release observation to 208,976/208,176 B after the next GC, then stays there for two more observations. Thus a single post-GC observation includes collectible lifecycle payload; this does not establish prompt finalizer cleanup or replace the primary phase results. There is no proven long-term plateau from three batches.

Shared Node/Control scratch accounting remains bounded; scalar nodes remain 32 B. The large-then-small case uses one module/worker lifetime and retains the same 2,072 B scalar envelope as the prior PR; pressure retains 2,484 B on both targets. Reserved/peak/retained/discarded NodeScratch and ControlScratch, scalar scratch, hint bytes and attempts are all in diagnostics. Worker high-water sums are accounting envelopes, not simultaneous process peaks or physical memory. Lower compiler allocation totals follow avoided target arenas/deferred scalar state; the new fixes change ownership/register placement and terminal work, not backing-array capacity.

## Correctness, CI and reproduction

Native AMD64 ordinary/checked benchmark fixture and selected tiny/fib/many_funcs/xxhash/json semantic corpus tests pass for all three measured binaries. Full checked root testing passes after the named installer test is run with its required workspace; the first GOWORK=off root run failed only that test and is preserved. Local normal/checked shared/backend/API tests pass. The separate full legacy-counter diagnostic (WAGO_SHARED_SCALAR=0) and shared diagnostic (1) pass; staticcheck has zero new findings and actionlint/script checks pass. CI now runs both diagnostic contracts rather than expecting legacy peephole counters from migrated functions.

Linux ARM64 checked backend/API, generated-code/frame tests and synthetic diagnostics execute under QEMU. Direct corpus child tests pass tiny.add, fib_rec.fib, many_funcs.run and JSON serialize/deserialize. The first full corpus attempt under QEMU could not spawn ARM children from the AMD64 host and xxhash could not create a native deadline timer; these environmental failures are preserved. Darwin AMD64/ARM64 builds pass; cross-build is not execution testing. Native multi-platform CI remains the merge gate. The independent review resolved raw-i32 normalization and frame-option findings before measurements; the alias-group suggestion has a direct test. No unresolved source findings remain.

Reproduce correctness (Go 1.27.1, pinned WABT 1.0.41/spec interpreter as CI):

```sh
go test -count=1 -tags wago_regalloccheck ./src/core/compiler/backend/railshot/... ./src/wago
WAGO_SHARED_SCALAR=0 go test -count=1 -tags wago_codegenstats ./src/core/compiler/backend/railshot/... ./src/wago
WAGO_SHARED_SCALAR=1 go test -count=1 -tags wago_codegenstats \
  -run "^Test(SharedScalar|ScalarIncoming|ScalarRawIncoming|ScalarTerminal|ScalarValueBounds)" \
  ./src/core/compiler/backend/railshot/... ./src/wago
go test -count=1 -tags wago_regalloccheck -run "^TestGoInstallBuildsNamedInstallerCommand$" .
GOWORK=off TERM=dumb go test -count=1 -tags wago_regalloccheck \
  -skip "^TestGoInstallBuildsNamedInstallerCommand$" ./...
```

For the missing updated native Mac comparison, plug in and keep the machine idle; pin main so a later merge cannot compare the PR against itself:

```sh
./run_me.sh --full-tests \
  --main-ref da456bc1c00c89e42cbfec54d4d93300d8601d85 \
  --pr-ref 2392a43173e5fc343d776fde8bc3dec6c309151f \
  --output "$HOME/wago-final-leaf-results"
```

The runner builds before timing, freezes identical harness/fixtures and preserves benchmark/memory/profile commands and hashes. Linux exact three-revision prepare/run/analysis scripts are checked into the data directory; reproduce their recorded commands from the fixed manifest, or copy them into an external `pr802-final-evidence/leaf-repair-20261004` directory alongside the three worktrees named in the scripts. Copy identical `bench`/`corpus` directories from R to A/C before preparation, and place the provided runtime_memory.go/resource_wrapper.py in its parent. The QEMU binary path is environment-specific. Compiler options and binary commands are in the preparation/measurement manifests. Large profile diagnostics run separately.

Incomplete measurements: updated native ARM64/Mac timing and lifecycle memory; native helper-only B on Mac; long automatic-worker retention plateau; fully fixed power/thermal behavior; strict reset-state JSON execution; source-level root tracing of every collectible metadata object; full ARM corpus under this emulation setup. CI can provide native correctness, but does not substitute for the missing Mac performance run.

Tracked text trims only padding spaces; the TSV quotes its empty delta field. Metric values are unchanged, and original raw logs/manifests remain untouched in the external evidence directory.
