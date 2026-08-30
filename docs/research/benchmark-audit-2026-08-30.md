# Benchmark audit — 2026-08-30

Host: Linux/amd64, AMD Ryzen 7 8845HS, Go 1.24.4. Focused runs used
`GOMAXPROCS=1` and `-cpu=1`.

## Scope

The tracked tree contains 451 benchmark functions across 17 package directories.
The audit ran one-iteration scout passes over the frontend, wasm decoder and
validator, AMD64 backend, runtime, collector, public runtime, and CLI/internal
benchmark packages. It also ran the complete `src/core/compiler/wasm`,
`src/core/runtime/gc`, and `src/wago` benchmark sets after the changes below.

## Retained: allocation-free indexed memarg classification

The bytecode classifier claimed an allocation-free interface, but indexed
multi-memory memargs called the AST decoder. The AST `MemArg` representation uses
an optional `*MemIdx`, so every classified indexed load allocated one small Go
object.

The classifier now decodes the align/index/offset fields directly into
`InstructionImmediate`. The AST decoder and its public representation are
unchanged.

Command:

```sh
GOMAXPROCS=1 go test ./src/core/compiler/wasm -run '^$' \
  -bench '^BenchmarkModuleInstructionClassifier(ManyImportsAndOps|MixedLateMemory)$' \
  -benchmem -benchtime=200ms -count=5 -cpu=1
```

Median result for 10,000 indexed memory64 loads:

| metric | before | after | change |
|---|---:|---:|---:|
| time | 287.6 us/op | 211.7 us/op | -26.4% |
| Go bytes | 42,183 B/op | 1,915 B/op | -95.5% |
| allocations | 10,001/op | 1/op | -99.99% |

The remaining allocation is the mixed-memory width bitset created once per
classifier. The immediate-free control stayed allocation-free and improved from
about 50.6 us/op to 48.3 us/op in the final run.

## Retained: linear imported-function metadata passes

`BenchmarkCompileImportMetadata` exposed quadratic compile time. Two module
prepasses iterated imported function indexes and called `FuncSignature` for every
index. `FuncSignature` rescanned the import section from its start, while repeated
`ImportedFuncCount` calls performed more full scans. A CPU profile attributed
about 63% of samples to `Module.importCount` and 36% to `Module.FuncTypeIndex`.

The runtime now ranges the import section once and resolves each function import
by its actual import-section index through `Module.ImportFuncType`.
Frontend admission also stopped formatting a diagnostic string for every valid
function import; the exact text is now built only on an error path.

Command:

```sh
GOMAXPROCS=1 go test ./src/wago -run '^$' \
  -bench '^BenchmarkCompileImportMetadata/imports=(1000|10000)/low-level$' \
  -benchmem -benchtime=1x -count=5 -cpu=1
```

Median results:

| imports | metric | before | after | change |
|---:|---|---:|---:|---:|
| 1,000 | time | 2.428 ms | 0.636 ms | -73.8% |
| 1,000 | Go bytes | 393,560 B/op | 307,608 B/op | -21.8% |
| 1,000 | allocations | 7,798/op | 4,054/op | -48.0% |
| 10,000 | time | 199.726 ms | 30.442 ms | -84.8% |
| 10,000 | Go bytes | 6,364,072 B/op | 5,486,008 B/op | -13.8% |
| 10,000 | allocations | 79,818/op | 40,071/op | -49.8% |

Three one-iteration corpus compile samples were flat after the change:

| corpus | before median | after median |
|---|---:|---:|
| sqlite3 | 77.474 ms | 77.810 ms |
| ruby | 797.416 ms | 796.829 ms |
| esbuild | 530.954 ms | 530.050 ms |

Corpus compile bytes and allocation counts were unchanged.

## Benchmark repairs

The previous resource-policy change exposed three stale benchmark assumptions:

- staged validation benchmarks constructed an internal validator without the
  normalized default limits;
- import validation generated 10,000 memories even though the executable
  implementation ceiling is 4,096; and
- the dirty-persistent-root collector benchmark expected first-minor promotion
  after survivor aging became the default Throughput policy.

The validation benchmarks now use explicit valid limits. The pure-memory matrix
ends at 4,096 entries. The persistent-root benchmark disables moving survivors
because it measures dirty-root scanning, not tenuring policy. The complete wasm
and collector benchmark packages now pass with `-benchtime=1x`.

## Measured and rejected

### Adjacent import-module string reuse

A one-entry adjacent-name reuse experiment halved allocations in a synthetic
repeated-module decode and improved that microbenchmark by about 8%. Real corpus
decode removed only 21 to 54 allocations and showed no timing win: ruby was about
2.7% slower, sqlite3 about 1.8% slower, and esbuild was flat in five cold samples.
The change was reverted. `BenchmarkImportNameDecode` remains as a permanent
repeated-module, repeated-field, and all-unique watchpoint.

### Inline first mixed-memory width word

Keeping the first 64 mixed-memory width bits inline removed the sole allocation
for a two-memory classifier, but construction regressed from about 13.9 ns/op to
18.0 ns/op. The retained slice representation is faster and the experiment was
reverted.

## Verification

```sh
go test ./src/core/compiler/wasm ./src/core/compiler/frontend ./src/core/runtime/gc -count=1
go test ./src/wago -count=1
go test ./src/core/compiler/wasm -run '^$' -bench . -benchmem -benchtime=1x -count=1
go test ./src/core/runtime/gc -run '^$' -bench . -benchmem -benchtime=1x -count=1
go test ./src/wago -run '^$' -bench . -benchmem -benchtime=1x -count=1
```

All passed on the audit host.
