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

`BenchmarkCompileImportMetadata` exposed quadratic compile time. GC-boundary,
synchronous-host-slot, and imported-signature prepasses iterated function indexes
while `FuncSignature` or `FuncTypeIndex` rescanned the import section from its
start. Repeated `ImportedFuncCount` calls performed more full scans. The initial
CPU profile attributed about 63% of samples to `Module.importCount` and 36% to
`Module.FuncTypeIndex`. After the first two prepasses became linear, an adversarial
follow-up profile found the imported-signature loop still spent 86.7% of samples
in `FuncTypeIndex`.

All three runtime prepasses now range the import section once. Direct
import-section lookups use `Module.ImportFuncType` where the stored signature is
sufficient; imported-signature conversion keeps the exact function namespace
while resolving types directly from each function import. Frontend admission also
stopped formatting a diagnostic string for every valid function import; the exact
text is now built only on an error path.

Command:

```sh
GOMAXPROCS=1 go test ./src/wago -run '^$' \
  -bench '^BenchmarkCompileImportMetadata/imports=(1000|10000)/low-level$' \
  -benchmem -benchtime=1x -count=10 -cpu=1
benchstat main.txt head.txt
```

Ten-sample `benchstat` results:

| imports | metric | before | after | change |
|---:|---|---:|---:|---:|
| 1,000 | time | 2.452 ms | 0.394 ms | -83.94% |
| 1,000 | Go bytes | 393,560 B/op | 307,608 B/op | -21.84% |
| 1,000 | allocations | 7,798/op | 4,054/op | -48.01% |
| 10,000 | time | 217.278 ms | 5.361 ms | -97.53% |
| 10,000 | Go bytes | 6,364,072 B/op | 5,486,008 B/op | -13.80% |
| 10,000 | allocations | 79,818/op | 40,071/op | -49.80% |

Ten interleaved one-iteration corpus samples found no significant compile-time
change. The geomean moved -0.43%:

| corpus | before median | after median | p value |
|---|---:|---:|---:|
| sqlite3 | 83.36 ms | 83.76 ms | 0.436 |
| ruby | 847.5 ms | 844.8 ms | 0.796 |
| esbuild | 571.1 ms | 562.9 ms | 0.143 |

Corpus compile bytes and allocation counts were unchanged in every sample.

### Release binary size

The retained changes do not change the stripped manager profile. On Linux/amd64,
the stripped `runtime-standard` and `runtime-minimal` profiles each grow by one
4 KiB file-alignment page:

| profile | before | after | change |
|---|---:|---:|---:|
| manager | 8,601,784 B | 8,601,784 B | 0 B |
| runtime-standard | 8,466,616 B | 8,470,712 B | +4,096 B |
| runtime-minimal | 8,159,416 B | 8,163,512 B | +4,096 B |

## Benchmark repairs

The previous resource-policy change exposed three stale benchmark assumptions:

- staged validation benchmarks constructed an internal validator without the
  normalized default limits;
- import validation generated 10,000 memories even though the executable
  implementation ceiling is 4,096; and
- the dirty-persistent-root collector benchmark expected first-minor promotion
  after survivor aging became the default Throughput policy.

The validation benchmarks now use explicit valid limits. The pure-memory
validation matrix ends at 4,096 entries, while decode and metadata-iteration
watchpoints retain their 10,000-import scale. The persistent-root benchmark
disables moving survivors because it measures dirty-root scanning, not tenuring
policy. The complete wasm and collector benchmark packages now pass with
`-benchtime=1x`.

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
