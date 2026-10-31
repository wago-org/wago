# Bulk-memory execution qualification

The ARM64 candidate reaches Wasmtime parity on the five steady-state bulk-memory
contracts currently used by wasm.fyi. This is a local execution comparison of the
exact Wasm artifacts, not a published wasm.fyi result or a compilation, RSS or
host-call-latency claim. Baseline is Wago
`90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`; candidate source is the tree containing
this report on `jairus/tweak-bulk-memory`.

## ARM64, 2026-10-31

Apple M4 Max, macOS/Darwin arm64, Go 1.27.1, Rust 1.98.1, Wasmtime crate 50.0.0
with Cranelift. The two small protocol adapters use Wago's public
`WasmFunc.Invoke` and Wasmtime's Rust `Func.call`. Wago uses one compiler worker.
Both capture every call's result inside the timing window and verify it afterward.
They are local core embeddings, not complete copies of the wasm.fyi adapters.

The [raw samples and identities](arm64.json) retain eight independent launches
per engine/workload, seven measured samples per launch, and a minimum 75 ms
per sample. Each workload alternates runtime order between launches in fresh
processes, with GOMAXPROCS=1, foreground task policy and the existing wasm.fyi
measurement lease. There is no concurrent build. Other desktop applications were
running; timing variation is retained rather than filtered away.

Times below are medians of launch medians in microseconds per exported call.
Each call contains 64 in-Wasm repetitions. Deltas are medians of paired launch
ratios: `(candidate / comparison - 1) * 100`; negative is faster.

| Contract | Wago baseline us | Wago candidate us | Wasmtime us | Delta vs baseline | Delta vs Wasmtime |
| --- | ---: | ---: | ---: | ---: | ---: |
| fill, 65536x64 | 39.062 | 34.660 | 35.883 | -10.2% | -4.4% |
| copy-overlap, 65536x64 | 98.900 | 69.659 | 75.344 | -30.0% | -8.5% |
| record-pack, 64x64 | 28.562 | 4.779 | 19.653 | -83.2% | -76.1% |
| sliding-window-compaction, 64x64 | 8.446 | 8.558 | 8.871 | +3.5% | -3.3% |
| doubling-pattern-expansion, 64x64 | 25.300 | 3.717 | 18.753 | -85.3% | -80.3% |

Fill is close enough that the claim is parity, not a reliable small win: its
paired ratios span 0.903–1.026 and Wasmtime sample CV reaches 0.125. The other
four workloads beat Wasmtime in every pair. Compaction regresses against the
Wago baseline in this run; the improvement claim does not hide that regression.

The first full campaign used 512-byte fill groups. After choosing 1024-byte
fill groups, fill and copy-overlap were requalified for another eight launches.
The other three contracts have byte-identical native code between the two
candidates; the JSON records those hashes. The three earlier measurements are
retained on that basis, rather than implying a second full campaign occurred.
All 840 measured samples pass the independent exact-return oracles.

## Inputs and reproduction limits

The JSON records every workload's artifact SHA-256, every raw timed sample,
launch order, duration qualification and verification result. The fixture
manifest is the bulk-memory subset of wasm.fyi's
`.wasmbench/feature-performance-current/manifest.json`; all five artifact hashes
match its live ARM64 contracts at measurement time. Expected returns are
896, 1408, 4234330112, 4342784 and 4085170176, respectively. The separate
init/drop lifecycle probe is not part of these five stateless steady workloads.

Local driver, adapter sources, Cargo.lock, binaries, plans, original campaigns
and logs remain in the ignored `.validation/bulk-memory/` directory of the
`wago-bulk-memory` worktree. They reference the local wasm.fyi checkout and are
not a portable benchmark distribution. The checked-in samples document this
historical run; reproducing it requires those adapters and exact fixtures.
Exploratory screens, including rejected copy/fill strategies, are excluded.

## Correctness

- Full `go test -tags=wago_regalloccheck ./src/... -count=1` passes.
- Targeted ordinary, codegen-stat, profiling and guard-page checked tests pass.
- Whole-memory differential tests cover overlap, all small-copy lengths,
  alignment, dirty upper halves of memory32 operands, exact bounds and traps
  before writes. Filled-source forwarding tests also check a completed fill
  remains visible when the following copy traps, cross-memory copies, and
  rejection after a mutation or changed source/count.
- Byte reductions check both final locals against independent modular arithmetic,
  signed-boundary weights, static offsets, empty ranges and bounds fallback.
  Selection tests reject shared/memory64, interruption and source-recording paths.

AMD64 qualification is pending native Hub access; this report makes no AMD64
performance claim. Hosted CI has not been run for this branch.
