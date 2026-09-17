# General runtime corpus comparison — 2026-09-17

These results come from the complete benchmark catalog at Wago
`e1cf4601a61fe31194da390cae51af79ffa44f0a`. Values are corpus geometric means;
lower is better. Each module has equal weight after exports within a module are
folded to one value.

## Go-engine results

The compile set contains 63 modules. Instantiate and direct execution contain
54 runnable modules and 61 exports. The command set contains nine modules.

### Darwin ARM64 — Apple M4 Max

| Engine | Compile | Compile heap | Instantiate | Direct execution |
|---|---:|---:|---:|---:|
| Railshot | **0.171 ms** | **93.9 KiB** | **9.831 µs** | **66.133 µs** |
| Dragline | 0.969 ms | 419.7 KiB | 9.916 µs | 144.201 µs* |
| wazero | 0.848 ms | 1.04 MiB | 23.377 µs | 107.783 µs |

\* Dragline's execution value covers only the 52 passing modules and 57 passing
exports. `blake3` (`blake3_hash`, `blake3_keyed_hash`, and
`blake3_derive_key`) and `zstd` (`zstd_decompress_run`) trap with an out-of-bounds
linear-memory access during benchmark calibration. It is therefore not a
complete like-for-like corpus result. On that passing subset, Dragline is 33.8%
slower than wazero.

| Command engine | Nine-module execution mean |
|---|---:|
| Railshot | **221.318 µs** |
| wazero | 265.501 µs |

### Linux AMD64 — AMD Ryzen 7 7800X3D, CPU 7 pinned

| Engine | Compile | Compile heap | Instantiate | Direct execution |
|---|---:|---:|---:|---:|
| Railshot | **0.255 ms** | **137.5 KiB** | 6.997 µs | **93.494 µs** |
| Dragline | 5.313 ms | 828.9 KiB | **6.983 µs** | 94.653 µs |
| wazero | 1.210 ms | 1.06 MiB | 25.290 µs | 159.578 µs |

Dragline is 40.7% faster than wazero across the complete AMD64 direct-execution
corpus. Railshot compiles 4.74x faster than wazero and uses 87.3% less Go-heap
allocation volume per compile.

| Command engine | Nine-module execution mean |
|---|---:|
| Railshot | **208.781 µs** |
| wazero | 351.231 µs |

Compile heap is Go benchmark `B/op`: total allocation volume for decode,
validation, and compilation, not peak live heap or process RSS.

## AMD64 Dragline versus WAVM/LLVM

The external comparison covers 51 targets in six alternating one-second rounds.
Both workers were pinned to CPU 7; Wago used signal bounds. Export medians are
compared before the corpus geometric mean.

| Aggregate | Dragline relative to WAVM/LLVM |
|---|---:|
| Execution-time geometric mean | **1.255x** |
| Execution-time median | 1.275x |
| Targets within 10% | 9 / 51 |
| Targets faster in Dragline | 4 / 51 |

Group execution-time geometric means are 1.374x for Blake, 1.533x for JSON,
1.394x for UTF, 1.234x for compute, 1.261x for PolyBench, and 1.002x for the
synthetic group. The compiler contains no corpus-name, export-name, function-
index, body-hash, byte-sequence, or complete-algorithm selection paths.

## End-to-end startup

These are geometric means across six real workloads, measured from process
spawn through exit with `hyperfine -N --warmup 5 --min-runs 30`.

| Runtime | Apple M4 Max ARM64 | Ryzen 7 7800X3D AMD64, CPU 7 pinned |
|---|---:|---:|
| Railshot | **8.054 ms** | **7.161 ms** |
| Dragline | 9.091 ms | 14.897 ms |
| wazero | 8.459 ms | 8.556 ms |
| Wasmtime/Cranelift | 8.761 ms | 9.501 ms |
| V8 | 21.151 ms | 19.917 ms |
| Wasm3 | 22.177 ms | 41.205 ms |
| Wasmi | 38.159 ms | 54.895 ms |
| WAVM/LLVM | 22.477 ms | 41.963 ms |

These are different physical machines, so cross-architecture differences are
published-machine comparisons rather than pure ISA attribution.

## Method

- Wago source: `e1cf4601a61fe31194da390cae51af79ffa44f0a`.
- Go-engine command: `GOMAXPROCS=1 GOFLAGS=-mod=readonly WAGO_BOUNDS=signals go test -tags wago_guardpage ./suite -run '^$' -bench . -benchmem -benchtime 1s -count 1 -args -wago.corpus=all` from `bench/`.
- ARM64 used Go 1.26.5 on Darwin. AMD64 used Go 1.22.2 on Linux and was pinned to CPU 7.
- Railshot and Dragline used native targets, signal bounds, and one compiler worker.
- Direct-execution exports are folded into module means before the corpus mean,
  so modules with multiple exports do not receive extra weight.
- End-to-end startup uses fresh processes and is not derived from compile plus
  instantiate benchmarks. The runtime set is wazero 1.12.0, Wasmtime 46.0.1,
  V8 15.2.20, Wasm3 0.9.0, Wasmi 2.0.0, WAVM prerelease, and both Wago backends.
