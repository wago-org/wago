# Compiler optimization study

`optstudy` measures pinned wasm.fyi applications in fresh processes and compares
individual optimization selections with exact result oracles. Compilation,
execution, allocation traffic, and process peak RSS are separate measurements.
Allocation reductions do not establish a peak-RSS reduction.

## Build and run

From the repository root:

```sh
go build -o /tmp/wago-optstudy ./scripts/optstudy
python3 scripts/optstudy/run.py \
  --binary /tmp/wago-optstudy \
  --root /path/to/wasm.fyi/corpora/applications \
  --out .validation/optstudy --samples 1
```

Use one paired sample for a quick screen. Reuse the exact inputs, runtime,
platform, entry point, workers, and phase for each comparison. Alternate image
order and keep timing runs separate from compilation/test jobs.

The runner freezes inputs and binary identity in its manifest, writes each
sample atomically, and skips completed samples on resume. Repeating the same
command resumes that campaign. A changed binary or input set needs a new output
directory. The process lock prevents two writers to one campaign.

`-raw module.wasm` measures compilation without an execution oracle. It records
compile time, total process peak RSS, allocation traffic and native-code hash.
Its peak includes process startup and input residency. It is a compiler probe,
not full execution qualification. `-hold` retains the compiled module for an
external residency inspection.

## Corpus and switch checks

The benchmark module is separate from the root module:

```sh
cd bench
go test ./suite -run '^TestCorpus$' -wago.corpus=CORPUS_IDS
GOMAXPROCS=1 go test ./suite -run '^$' \
  -bench '^BenchmarkExecOptimizationAblation$' -benchtime=1x \
  -wago.bench.optimization-ablation -wago.corpus=CORPUS_IDS
```

Choose a broad frozen set covering scalar, SIMD, memory, calls and command
workloads. Count complete valid off/on pairs; the intentionally unsafe
stack-fence-off selection is excluded. A code-identical default image supports
instruction equality, while oracle checks still validate execution and runtime
metadata. Do not call an uncovered optimization universally irrelevant based
only on a low hit count in one suite.

The existing PR study has a separate frozen 20-workload manifest and Linux
measurement entry point:

- [Cold compilation report](../../docs/research/cold-compilation-2026-10-23.md)
- [Pinned workloads](../../docs/research/cold-compilation-2026-10-23/manifest.json)
- `TestOptimizationStudySample` in `bench/suite/optimization_study_linux_test.go`

Historical measurements in that report identify their tested source and base.
They must be requalified after integrating compiler changes; do not combine
ratios from different sources, harnesses or runtimes.

## ARM64 integration

The integrated campaign streams compiler hints from successful validation,
uses reusable register-move graphs, compacts transient metadata and places
ordinary compiler declarations together. The layout change moves complete
functions without adding workload recognition. Positive and negative feature
behavior remains covered by backend/runtime tests.

Seven weak, default-off ARM64 selections were removed: `guarded-test-ccmp`,
`streaming-i32-reduction`, `dot-loop-vector`, `crowded-products`,
`linear-integer-tree`, `loop-trap-cell` and `prepared-light-entry`. Direct compiler overrides now fail explicitly. Stored CLI and project settings
accept recognized retired names as ignored compatibility keys. New compilation no longer emits light-entry metadata; readers
for older artifacts remain. Direct and bounded prepared entries remain supported.

The independent floating-point-map part of the former loop-vector policy is
retained as `float-map-loop`. Expanded corpus coverage found a substantial gemm
benefit; integer dot/min-plus recognizers remain retired. Its admission test is
separate from its scalar oracle so a correct fallback cannot hide lost codegen.

The pre-integration Darwin/ARM64 Go screen against `fc6af651c` was about 29%
faster compilation and 16% lower process peak RSS. Those figures describe the
original campaign source, not the combined PR or its newer main baseline. The
30% physical-memory target remains unproven. Full local experiment and recovery
notes are retained under `.validation/pr1017-integration/` and are not published
as production documentation.
