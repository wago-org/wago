# General corpus comparison — 2026-09-03

The website General tab compares Railshot, Dragline, wazero, Wasmtime,
V8, and WAVM on the same non-ISA corpus. Timing values are geometric means and
lower is better. Compile memory growth is the arithmetic mean of each module's
fresh-process peak RSS above that engine's `tiny.wasm` peak. This removes most
of the executable and runtime startup floor while retaining process-wide
compiler, allocator, native-library, and artifact memory.

## Results

### Darwin ARM64 — Apple M4 Max

| Engine | Compile mean | Compile memory growth | Instantiate mean | Execution mean | Call latency | End-to-end |
|---|---:|---:|---:|---:|---:|---:|
| Railshot | 8.57 ms | 9.56 MiB | 1.17 µs | 7.29 µs | 7.65 ns | 8.57 ms |
| Dragline | 14.07 ms | 13.78 MiB | 1.49 µs | 5.83 µs | 6.07 ns | 14.07 ms |
| wazero | 10.41 ms | 22.27 MiB | 10.96 µs | 9.91 µs | 19.48 ns | 10.42 ms |
| Wasmtime / Cranelift | 9.74 ms | 42.79 MiB | 3.10 µs | 6.99 µs | 12.42 ns | 9.74 ms |
| V8 | 13.78 ms | 2.10 MiB | 7.75 µs | 4.78 µs | 2.34 ns | 13.79 ms |
| WAVM | 57.52 ms | 391.78 MiB | 49.54 µs | 6.93 µs | 32.48 ns | 57.57 ms |

Dragline executes the runnable corpus 41.1% faster than wazero on ARM64.
Railshot's average compile-memory growth is 57.1% lower than wazero's;
Dragline's is 38.1% lower.

### Linux AMD64 — AMD Ryzen 7 7800X3D, CPU 7 pinned

| Engine | Compile mean | Compile memory growth | Instantiate mean | Execution mean | Call latency | End-to-end |
|---|---:|---:|---:|---:|---:|---:|
| Railshot | 4.56 ms | 8.44 MiB | 2.70 µs | 8.69 µs | 7.95 ns | 4.56 ms |
| Dragline | 8.75 ms | 23.63 MiB | 2.71 µs | 20.79 µs | 7.88 ns | 8.76 ms |
| wazero | 5.97 ms | 21.53 MiB | 13.14 µs | 13.11 µs | 27.70 ns | 5.98 ms |
| Wasmtime / Cranelift | 10.91 ms | 38.09 MiB | 3.37 µs | 8.39 µs | 15.98 ns | 10.91 ms |
| V8 | 11.05 ms | 2.14 MiB | 11.43 µs | 7.02 µs | 2.99 ns | 11.06 ms |
| WAVM | 82.38 ms | 178.05 MiB | 78.21 µs | 7.97 µs | 41.75 ns | 82.46 ms |

Dragline remains 58.6% slower than wazero on the AMD64 execution corpus, while
Railshot is 33.7% faster. Railshot's average compile-memory growth is 60.8%
lower than wazero's. Dragline currently uses 9.8% more than wazero on AMD64.

## Method

- Engine source: Wago `ca6c0c91e19064c6c0c7ae71abb32eab4c7aa3db`.
  Benchmark-adapter correctness and checkpoint fixes are in `5e707c07` and do
  not change compiler or runtime engine code.
- Tools: Wasmtime 46.0.1, V8 15.5.4, WAVM
  `4e82bb9fecf9c1bdb4d00f96fa89063ee4382d09` with LLVM 21.1.8, wazero
  1.9.0, Go 1.26.5 on Darwin, and Go 1.22.2 on Linux.
- Compile covers all 36 application modules in fresh processes. Each module's
  median is taken before the corpus geometric mean. ARM64 uses three
  interleaved rounds; AMD64 uses six. Railshot and Dragline use native targets,
  explicit bounds checks, and one compiler worker. Wasmtime uses Cranelift at
  optimization level 2 with backtracking register allocation.
- Compile memory covers all 36 modules. Each sample runs the complete decode,
  validate, and compile operation in a fresh process. For each engine, the
  median `tiny.wasm` peak is subtracted from every module's median peak (with
  negative noise clamped to zero), then the 36 growth values are averaged.
  ARM64 uses the three interleaved compiler-harness rounds. AMD64 uses a clean
  isolated `/usr/bin/time` sample per engine and module; the Linux harness's
  cumulative `ru_maxrss` values are deliberately excluded.
- The tiny-module process floors are 7.59 MiB Railshot, 7.98 MiB Dragline,
  7.30 MiB wazero, 14.23 MiB Wasmtime, 31.56 MiB V8, and 25.30 MiB WAVM on
  ARM64; and 8.60, 8.85, 7.54, 20.23, 27.30, and 39.87 MiB respectively on
  AMD64. Memory-growth values measure module-sensitive working set, not total
  process memory or an RSS time average.
- Instantiate and execute cover the 30 runnable application modules. The three
  Go engines use the standard calibrated Go benchmarks. Wasmtime, V8, and WAVM
  use persistent workers with four calibrated 100 ms rounds and a per-workload
  median. V8 runs with its default tiering behavior; WAVM uses its LLVM-backed
  runtime.
- Modules with multiple execution exports are reduced to one geometric mean per
  module before the corpus geometric mean, so extra exports do not overweight a
  module.
- Call latency is the `tiny.add` host-to-Wasm call. End-to-end is the compile
  corpus mean plus the instantiate corpus mean; the separate website
  End-to-end latency section remains the directly measured process
  spawn-to-exit dataset.
- The ISA micro-corpus is excluded throughout.
