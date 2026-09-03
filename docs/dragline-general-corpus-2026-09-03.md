# General Go-engine corpus comparison — 2026-09-03

The website comparison tabs cover the three Go engines—Railshot, Dragline, and
wazero—on the same non-ISA corpus. Values are corpus geometric means and lower
is better. Compile heap is the Go benchmark's allocation bytes per full decode,
validate, and compile operation. The separate website End-to-end latency chart
retains the broader cross-runtime comparison.

## Results

### Darwin ARM64 — Apple M4 Max

| Engine | Compile mean | Compile heap | Instantiate mean | Execution mean | Call latency | End-to-end |
|---|---:|---:|---:|---:|---:|---:|
| Railshot | 8.57 ms | 162.1 KiB | 1.17 µs | 7.29 µs | 7.65 ns | 8.57 ms |
| Dragline | 14.07 ms | 423.3 KiB | 1.49 µs | 5.83 µs | 6.07 ns | 14.07 ms |
| wazero | 10.41 ms | 1.11 MiB | 10.96 µs | 9.91 µs | 19.48 ns | 10.42 ms |

Dragline executes the runnable corpus 41.1% faster than wazero on ARM64.
Railshot allocates 85.8% fewer Go-heap bytes per compile than wazero;
Dragline allocates 62.9% fewer.

### Linux AMD64 — AMD Ryzen 7 7800X3D, CPU 7 pinned

| Engine | Compile mean | Compile heap | Instantiate mean | Execution mean | Call latency | End-to-end |
|---|---:|---:|---:|---:|---:|---:|
| Railshot | 4.56 ms | 144.2 KiB | 2.70 µs | 8.69 µs | 7.95 ns | 4.56 ms |
| Dragline | 8.75 ms | 772.0 KiB | 2.71 µs | 20.79 µs | 7.88 ns | 8.76 ms |
| wazero | 5.97 ms | 1.13 MiB | 13.14 µs | 13.11 µs | 27.70 ns | 5.98 ms |

Dragline remains 58.6% slower than wazero on the AMD64 execution corpus, while
Railshot is 33.7% faster. Railshot allocates 87.6% fewer Go-heap bytes per
compile than wazero; Dragline allocates 33.5% fewer.

## Method

- Engine source: Wago `ca6c0c91e19064c6c0c7ae71abb32eab4c7aa3db`.
  Benchmark-adapter correctness and checkpoint fixes are in `5e707c07` and do
  not change compiler or runtime engine code.
- Tools: wazero 1.9.0, Go 1.26.5 on Darwin, and Go 1.22.2 on Linux.
- Compile covers all 36 application modules in fresh processes. Each module's
  median is taken before the corpus geometric mean. ARM64 uses three
  interleaved rounds; AMD64 uses six. Railshot and Dragline use native targets,
  explicit bounds checks, and one compiler worker.
- Compile heap covers all 36 modules using Go benchmark `B/op`: total Go-heap
  allocation bytes for the full decode, validate, and compile operation. Each
  module contributes equally through the corpus geometric mean. It is
  allocation volume, not peak live heap or process RSS.
- Instantiate and execute cover the 30 runnable application modules using the
  standard calibrated Go benchmarks.
- Modules with multiple execution exports are reduced to one geometric mean per
  module before the corpus geometric mean, so extra exports do not overweight a
  module.
- Call latency is the `tiny.add` host-to-Wasm call. End-to-end is the compile
  corpus mean plus the instantiate corpus mean; the separate website
  End-to-end latency section remains the directly measured process
  spawn-to-exit dataset.
- The ISA micro-corpus is excluded throughout.
