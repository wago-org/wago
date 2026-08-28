# Dragline Apple M4 ARM64 cost calibration — 2026-08-28

This calibration supplies the first measured CPU-family override used by
Dragline's RailSpec scheduling model. It does not infer costs from whole-Wasm
benchmarks. The native `bench/cmd/arm64cost` harness emits equal-length chains
directly with Wago's ARM64 encoder and executes them through the no-cgo native
trampoline.

Each observation contains 2,000,000 iterations of 64 dependent operations. The
ADD chain repeatedly consumes its previous result. The load chain repeatedly
loads a pointer to the same cache-resident pointer cell, making every load
dependent on the prior load. Nine samples are run in alternating order on one
locked OS thread, and the median is reported.

## Apple M4 Max observations

Three independent process runs:

| Run | ADD ns/op | L1 dependent load ns/op | Load / ADD | Rounded units |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 0.225087 | 0.674970 | 2.9987 | 3 |
| 2 | 0.225119 | 0.675086 | 2.9988 | 3 |
| 3 | 0.225127 | 0.675911 | 3.0024 | 3 |

The `apple-m4` table therefore overrides only
`RuleFoldedMemoryAddress.Latency`, from the generic four units to three. Native
byte size and uop/resource cost remain unchanged. Every unmeasured rule and
every unknown CPU family stays on the generated generic table.

Reproduce on an ARM64 Darwin or Linux host from `bench/`:

```sh
go run ./cmd/arm64cost
```

The tool reports the exact CPU and tuning identities, chain dimensions, raw
medians, ratio, and selected integer latency unit as JSON.
