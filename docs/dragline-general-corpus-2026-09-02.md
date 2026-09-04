# General corpus comparison — 2026-09-02

The website General tab compares Railshot, Dragline, wazero, and Wasmtime's
Cranelift backend on the same non-ISA corpus. Every value is a geometric mean;
lower is better.

## Results

### Darwin ARM64 — Apple M4 Max

| Engine | Compile time | Compile peak RSS | Instantiate | Execute |
|---|---:|---:|---:|---:|
| Railshot | 8.03 ms | 10.33 MiB | 8.82 µs | 10.38 µs |
| Dragline | 13.21 ms | 11.67 MiB | 8.97 µs | 5.90 µs |
| wazero | 10.01 ms | 11.60 MiB | 10.99 µs | 9.86 µs |
| Wasmtime / Cranelift | 9.11 ms | 22.15 MiB | 3.01 µs | 6.83 µs |

### Linux AMD64 — AMD Ryzen 7 7800X3D, CPU 7 pinned

| Engine | Compile time | Compile peak RSS | Instantiate | Execute |
|---|---:|---:|---:|---:|
| Railshot | 9.46 ms | 11.04 MiB | 9.45 µs | 20.36 µs |
| Dragline | 17.79 ms | 12.78 MiB | 12.73 µs | 50.29 µs |
| wazero | 12.25 ms | 11.89 MiB | 29.87 µs | 27.50 µs |
| Wasmtime / Cranelift | 20.80 ms | 28.20 MiB | 3.60 µs | 8.83 µs |

## Method

- Source: Wago `758787a0dcb9899be832532c857b20e83f6bc7a6`.
- Tools: Wasmtime 46.0.1, wazero 1.9.0, Go 1.26.5 on Darwin and
  Go 1.22.2 on Linux.
- Compile time covers all 36 application modules. Each engine compiles in a
  fresh process six times in interleaved order. The per-module median is taken
  before the corpus geometric mean. Railshot and Dragline use native targets,
  explicit bounds checks, and one compiler worker. Wasmtime uses Cranelift at
  optimization level 2 with backtracking register allocation.
- Compile memory is peak resident set size for those fresh compiler processes.
  Darwin uses the six-sample per-process reports. On Linux, the Go harness's
  fork inherited its parent's resident-set high-water mark, so the published
  values instead come from one direct `/usr/bin/time` compiler process per
  engine and module; this removes the false common RSS floor.
- Instantiate and execute cover the 30 runnable application modules. Wago and
  wazero use the standard calibrated Go benchmarks. Wasmtime uses four
  calibrated 100 ms rounds and the median for each module/export.
- Modules with multiple execution exports are reduced to one geometric mean per
  module before the corpus geometric mean, so extra exports do not overweight a
  module.
- The ISA micro-corpus is excluded throughout.
