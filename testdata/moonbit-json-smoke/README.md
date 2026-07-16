# MoonBit JSON WasmGC smoke workload

This source fixture builds a standalone WasmGC module that parses, stringifies,
reparses, compares, and checksums an embedded JSON corpus. Its exported
`run(i32) -> i64` function provides a deterministic execution result instead of
using successful startup as the only signal.

The fixture intentionally has no host imports and does not enable JS-string
builtins. Build products stay under `_build/` and are not committed.

## Pinned toolchain

The checked artifact metadata was produced with:

```text
moon 0.1.20260703 (6fbf8c3 2026-07-03)
```

Build and run it through Wago from the repository root:

```sh
make test-moonbit-json
```

Reference results from Node.js 26.3.0 are:

```text
run(1) = 1808148174
run(2) = 1512327905
run(8) = 828453439
```

When intentionally changing the source or pinned compiler, update the artifact
size, SHA-256, and expected results in the Wago smoke test together.

## Benchmark baseline

Measured July 16, 2026 on an AMD Ryzen 7 8845HS with Go 1.24.4, one pinned CPU,
`GOMAXPROCS=1`, five samples, and a two-second benchmark window:

| Stage | Median | Heap bytes/op | Allocations/op |
|---|---:|---:|---:|
| Decode | 0.276 ms | 169,656 | 829 |
| Validate | 1.380 ms | 407,864 | 2,146 |
| Production `Compile` (decode + validate + JIT) | 10.641 ms | 3,570,270 | 17,507 |
| Instantiate | 0.170 ms | 178,546 | 208 |
| Instantiate + `run(1)` | 4.733 ms | 577,678 | 3,055 |

Reproduce the workload-specific stages with:

```sh
COUNT=5 BENCHTIME=2s make bench-moonbit-json
```

The benchmark artifact is exact-hash checked before timing starts.
