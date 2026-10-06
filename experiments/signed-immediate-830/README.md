# Issue #830: signed-immediate stores

Experiment for [wago issue #830](https://github.com/wago-org/wago/issues/830), October 5, 2026. Base: `47a4c8f6919b39ac5b0e2d06d6c5f2152c496487`. Isolated branch `codex/signed-immediate-830` in `/home/jtenner/.codex/worktrees/signed-immediate-830/wago`.

## Decision

The bounded AMD64 rule is promising. Replacing two dword immediate stores with one qword immediate store, only when the complete i64 equals the sign extension of its low i32, improves eligible loops by about 15–17% and pressure loops by 5–7%. A real corpus workload, json-as SIMD serialization, improves by 5.62% in this run. SQLite native code shrinks by 18,848 bytes.

Retain the prototype and evidence locally for review. The original experiment exposed a partial-write failure in the guard-page split-store fallback. A subsequent requested [correctness fix](correctness/README.md) now routes the remaining AMD64 constants, and all ARM64 guard-mode i64 constants, through a single native eight-byte store. AMD64 guard-mode regression tests and the full runtime suite pass. Native ARM64 validation remains outstanding. Issue #830 tracks the experiment and implementation review.

Following independent review, the current ARM64 constant-store path additionally emits a full-access bounds proof before writing. A single unaligned native store is insufficient to guarantee faulting stores leave memory unchanged on all ARM64 CPUs. The retained measurements and historical patches predate that ARM-only check; the AMD64 optimization and timing evidence are unchanged.

A further [pressure mitigation](pressure/README.md) delays AMD64 constant materialization until after address calculation. It removes the additional spill introduced by the correctness fix and keeps the single-store guarantee. The new timing comparison does not establish a significant speedup over that fix; the smaller code and removed spill are confirmed directly.

## Bounded change and proof

The existing primary-memory32 scalar constant-store path already avoids materializing a value register. Thus this experiment tests eliminating an unnecessary second store, rather than claiming a newly removed register dependency.

The only new selection condition is:

```go
size == 8 && int64(int32(v)) == v
```

For those constants, the encoder emits `REX.W C7 /0 imm32` at the same effective address. The exact eight stored bytes equal the guest value. For example, `-1` and `-2147483648` qualify; positive `0x80000000`, positive `0xffffffff`, and values with unrelated upper/lower halves do not. All other constants keep the existing split form. Stores of one, two, or four bytes retain their exact narrower widths. Memory64 and other-memory materialization paths are controls, outside this change.

The same pending-load ordering, constant-stack removal, address evaluation, full eight-byte bounds request, displacement, and address-register release surround both forms. There is no additional scratch register or lifetime, no cache/provenance state, and no IR rewrite. The instruction is part of baseline AMD64 and requires no optional CPU feature. [candidate.patch](candidate.patch) contains only the two production-file changes; encoder goldens, generated Wasm modules, runtime oracles, and benchmarks are separate.

## Measurement

Ryzen 7 8845HS, Linux AMD64, Go 1.27.1, CPU 4 pinned with `taskset`, `GOMAXPROCS=1`. Seven 200 ms samples per form, alternating baseline/candidate order and profile order. [environment.txt](environment.txt) records hardware, base commit, parameters, and the six benchmark-binary hashes.

There are 78 matched benchmark rows: 15 execution and 15 full-compile cases for each of default host features and forced SSE2; nine corpus execution rows, one SQLite command row, and eight corpus full-compile rows. All timing comparisons below use medians. Significance is the unadjusted `benchstat` result with seven samples per form. Other desktop/worktree activity was present; there is no multiple-comparison correction. Intel and native ARM64 were not measured.

Each synthetic execution call performs 4,096 iterations of four stores, rotating over 128 cache-resident groups. Reported `ns/op` includes guest address arithmetic, bounds checks, loop control, and host invocation; `ns/store` divides that whole cost by 16,384. These are not isolated instruction latency measurements. Pressure cases keep seven eager integer carriers across the stores and consume them in an independently checked checksum. Warmup and final checks validate all 512 written slots against little-endian Go bytes. Corpus benchmarks retain their existing result/output checks.

| Synthetic execution case | Default baseline → candidate, ns/op | Change | Forced SSE2 change |
| --- | ---: | ---: | ---: |
| zero | 8,205 → 6,939 | −15.43% | −16.20% |
| −1 | 8,336 → 6,923 | −16.95% | −16.76% |
| minimum signed i32 | 8,297 → 6,928 | −16.50% | −16.24% |
| maximum signed i32 | 8,194 → 6,986 | −14.74% | −15.42% |
| unaligned | 8,304 → 7,011 | −15.57% | −15.85% |
| cache-line crossing | 8,269 → 7,019 | −15.12% | −16.02% |
| pressure | 15,085 → 14,340 | −4.94% | −6.60% |

All seven eligible cases improve significantly in both profiles. Including the eight controls, synthetic execution geomeans are −6.82% default and −7.24% SSE2. Synthetic execution remains zero B/op and zero allocations/op. Eligible ordinary loop code shrinks from 314 to 286 bytes; pressure code from 437 to 403 bytes, including layout effects. Diagnostic ordinary loops have zero spills/reloads; pressure loops have one spill and zero reloads in both forms (the spilled carrier is consumed through a memory-source add). Complete generated sequences and diagnostic spill counts are generated under the ignored `code/` directory by the build script.

**Regression retained:** the default `near-negative` control (`MinInt32-1`) measures 8,230 → 8,317 ns/op, +1.06%, p=0.038. This case retains the split sequence and identical native size. It is a nominal regression in this run; unchanged selection does not justify discarding it, nor does the result establish that the new store caused it. Other synthetic controls and SSE2 regressions are not statistically significant. All individual rows are in [summary.csv](summary.csv).

| Real workload execution | Baseline → candidate, ns/op | Change |
| --- | ---: | ---: |
| nbody.step | 253,316 → 255,232 | +0.76% |
| matmul.run | 54,371 → 55,117 | +1.37% |
| raytrace.render | 359,553 → 362,205 | +0.74% |
| json-as.serializeN | 22,555 → 22,639 | +0.37% |
| json-as.deserializeN | 41,576 → 42,187 | +1.47% |
| blake-as.hashN | 459,196 → 459,656 | +0.10% |
| json-as-simd.serializeN | 28,747 → 27,130 | **−5.62%, p=0.001** |
| json-as-simd.deserializeN | 53,558 → 53,782 | +0.42% |
| blake-as-simd.hashN | 403,052 → 405,987 | +0.73% |

Only SIMD serialization has a significant execution change; the nine-row execution geomean is +0.02%, effectively flat. The suite's adaptive `calls/batch` metric is an invocation-batching choice, not a second independent guest throughput result. SQLite command execution improves 335,468,300 → 329,205,632 ns/op (−1.87%), without significance; it includes instantiation and WASI lifecycle costs.

| Full public compile | Native bytes baseline → candidate | Compile time change |
| --- | ---: | ---: |
| sqlite3-query | 4,148,118 → 4,129,270 | +0.25% |
| nbody | 3,615 → 3,583 | −0.86% |
| matmul | 2,829 → 2,829 | +1.43% |
| raytrace | 12,086 → 12,074 | +2.93% |
| json-as | 67,398 → 67,238 | +2.93% |
| blake-as | 10,315 → 10,315 | +3.11% |
| json-as-simd | 80,885 → 80,685 | +3.08% |
| blake-as-simd | 37,439 → 37,439 | +3.63% |

Full-compile geomean is +2.05%; no individual compile-time change is significant. Synthetic full-compile geomeans are −1.26% default and +1.26% SSE2, also without significant individual changes. Compile timings include decoding, validation, compilation, linking/mapping and close. Median allocation counts and B/op are unchanged for every matched row.

Diagnostic backend compilation finds 2,198 selected sites in SQLite, three in nbody, two in raytrace, 15 in json-as, 19 in SIMD json-as, and none in the other three modules. Corpus spills, reloads, and shared/established function counts are exactly unchanged. These [diagnostic CSVs](candidate-corpus.csv) use `CompileModuleWith`'s default backend profile, which differs from the public runtime feature profile. Their native-byte totals must not be substituted for the public `code-B` measurements above.

## Correctness and the guard-page failure

Runtime tests cover eleven constants, all four widths, three offsets, aligned/unaligned/cache-line accesses, last-valid addresses, crossing the memory end, and large invalid addresses. A full-memory sentinel oracle verifies both exact store bytes and absence of mutation on a trapping access. Additional tests verify one evaluation of an effectful address producer, pre/post-store effect ordering, retained constant reuse, and reject incorrect sign extension with an independent byte/range oracle before executing an invalid native candidate. Backend selection tests check explicit baseline and optional feature masks; encoder tests check low and extended-register REX.W bytes and GP-write bookkeeping.

The guard build reveals a pre-existing failure: an arbitrary i64 constant is emitted as two dword stores. At a partially accessible memory-end address, the first store writes four bytes before the second faults. The guest receives the expected out-of-bounds trap, but memory has changed. [WebAssembly store semantics](https://webassembly.github.io/spec/core/exec/instructions.html#exec-store) require the full access range to pass before replacing the bytes.

Before the follow-up correction, the baseline failed 33 subcases (eleven constants × three offsets), and the candidate failed 18 (the six non-representable constants × three offsets). All five qualifying constants passed in the candidate guard build on this host. The historical failure output is retained in [baseline-guard-tests.txt](baseline-guard-tests.txt) and [candidate-guard-tests.txt](candidate-guard-tests.txt). The current expanded regression tests pass with `wago_guardpage`; see the [follow-up results](correctness/README.md).

Normal runtime tests pass on baseline and candidate. Candidate tests pass with register-allocation checks in default and forced SSE2 builds. Full AMD64 backend, AMD64 encoder, ARM64 encoder, Wasm test-helper, and normal runtime packages pass. ARM64 backend cross-compiles; there was no native ARM64 execution. The initial full runtime-package attempt failed because the fresh worktree lacked the spec-v3 submodule's test corpus. For the successful run, its `test/` directory was copied from the original checkout at the verified pinned revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`, and the existing WABT 1.0.41 binary was put on PATH. Both attempts are retained in `final-runtime-package*.txt`. No conformance baseline was updated. The follow-up additionally records a passing full guard-enabled runtime run.

## Reproduce and inspect

Run these commands from this worktree. The build script recreates the original optimization comparison before the correctness fix, then restores the starting AMD64 and ARM64 production sources on exit. Do not concurrently edit those files while building.

```bash
./experiments/signed-immediate-830/build-forms.sh
./experiments/signed-immediate-830/run-bench.sh
python3 ./experiments/signed-immediate-830/analyze.py > ./experiments/signed-immediate-830/summary.txt

go test -tags=wago_regalloccheck ./src/wago -run '^TestSignedImmediateStore' -count=1
go test -tags=wago_regalloccheck,wago_amd64_sse2 ./src/wago -run '^TestSignedImmediateStore' -count=1
go test -tags=wago_guardpage ./src/wago -run '^TestSignedImmediateStoreBytesAndTraps$' -count=1
```

The guard test now passes with the corrected sources. The historical failure logs refer to the three-offset test matrix at the time of the experiment; the current expanded matrix also checks every partial store length, offsets at the 32-bit limits, pressure, and guard-mode effects. The performance script defaults to the measured seven samples; the analysis currently requires seven. Corpus artifacts must be available under `corpus/`, and `benchstat` must be on PATH. Raw `baseline-{modern,sse2,corpus}.txt` and `candidate-*` files retain every timed sample; `benchstat-*` files retain uncertainty and significance. Smoke runs are checks only, excluded from the comparison. Use the separate correctness scripts for the before/after fallback-fix comparison.

The current worktree contains the experimental production rule, the AMD64/ARM64 correctness correction, tests, benchmarks, raw data, disassemblies, and these scripts.
