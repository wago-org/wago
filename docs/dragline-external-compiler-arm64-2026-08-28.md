# Dragline external compiler qualification — ARM64 — 2026-08-28

This is the current-host isolated-process compilation gate for the 17 admitted
ISA modules. Each engine ran in a fresh child process for six balanced
alternating rounds. Values below are the median Dragline result divided by the
median Cranelift result for the same Wasm. The geometric means are across the 17
per-module ratios.

The harness includes process startup. It measures end-to-end compiler command
cost, not in-process compiler latency. Artifact byte ratios compare each
engine's serialized product and therefore are footprint observations, not a
native-code-quality comparison.

## Environment and commands

- Host: Darwin ARM64, Apple M4 Max.
- Dragline worker: built into `compilerharness`, Go 1.26.5, executable SHA-256
  `f37820fc80d07041498b1badc98447aa5d18419a5fef130272e802ea68525366`.
- Cranelift: Wasmtime 46.0.1 (`823d1b8f2`, 2026-06-24), executable SHA-256
  `ac78ac0fb2715d2ff03cb1944e17b2aa76cc308baad315b92162db8562a4c15d`,
  configured with `opt-level=2,regalloc-algorithm=backtracking`.
- LLVM: unavailable; `wasmer-llvm` was not present in `PATH` and was recorded as
  unavailable in every raw report.
- Command shape:

  ```sh
  cd bench
  go build -o /tmp/wago-compilerharness ./cmd/compilerharness
  /tmp/wago-compilerharness -config external-compilers.json \
    -rounds 6 -out report.json corpus/isa_cmp_f64.wasm
  ```

## Aggregate result

| Dragline mode | Wall geometric mean | Wall range | Wall wins | CPU geometric mean | CPU range | RSS geometric mean | RSS range | Artifact geometric mean |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Compatibility | 0.917x | 0.766–1.144x | 12/17 | 0.333x | 0.245–0.593x | 0.437x | 0.403–0.467x | 0.071x |
| Native | 0.899x | 0.769–1.124x | 13/17 | 0.328x | 0.247–0.575x | 0.437x | 0.402–0.471x | 0.071x |

Both modes pass the master plan's current isolated-process compilation gates
against Cranelift on every admitted ISA module: the worst wall ratio is 1.144x,
below the 3x ceiling, and the worst peak-RSS ratio is 0.471x, below the 1.5x
ceiling. This does not discharge native AMD64, Linux PMU, LLVM, or larger
neutral optimized-Wasm release gates.

## Per-module ratios

| Module | Compat wall | Native wall | Compat CPU | Native CPU | Compat RSS | Native RSS | Artifact |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `isa_bulk_mem` | 0.809x | 0.806x | 0.283x | 0.283x | 0.425x | 0.423x | 0.052x |
| `isa_call` | 0.847x | 0.834x | 0.427x | 0.421x | 0.455x | 0.453x | 0.013x |
| `isa_cmp_f32` | 1.144x | 1.116x | 0.428x | 0.416x | 0.464x | 0.462x | 0.093x |
| `isa_cmp_f64` | 1.137x | 1.124x | 0.408x | 0.407x | 0.456x | 0.458x | 0.093x |
| `isa_cmp_i32` | 0.989x | 0.986x | 0.323x | 0.319x | 0.436x | 0.433x | 0.109x |
| `isa_cmp_i64` | 1.004x | 0.982x | 0.328x | 0.322x | 0.434x | 0.434x | 0.109x |
| `isa_ctl` | 0.816x | 0.814x | 0.295x | 0.294x | 0.434x | 0.435x | 0.014x |
| `isa_cvt` | 0.837x | 0.808x | 0.310x | 0.295x | 0.421x | 0.422x | 0.037x |
| `isa_cvt_mvp` | 1.078x | 1.057x | 0.326x | 0.320x | 0.440x | 0.435x | 0.154x |
| `isa_f32` | 0.887x | 0.857x | 0.307x | 0.300x | 0.445x | 0.449x | 0.172x |
| `isa_f64` | 0.890x | 0.838x | 0.301x | 0.289x | 0.440x | 0.442x | 0.147x |
| `isa_i32` | 0.766x | 0.769x | 0.245x | 0.247x | 0.403x | 0.402x | 0.178x |
| `isa_i64` | 0.812x | 0.786x | 0.254x | 0.247x | 0.408x | 0.407x | 0.178x |
| `isa_mem` | 0.850x | 0.840x | 0.310x | 0.307x | 0.421x | 0.415x | 0.066x |
| `isa_mem_narrow` | 1.040x | 1.033x | 0.334x | 0.331x | 0.434x | 0.435x | 0.300x |
| `isa_signext` | 0.893x | 0.883x | 0.328x | 0.329x | 0.460x | 0.468x | 0.027x |
| `isa_var` | 0.903x | 0.865x | 0.593x | 0.575x | 0.467x | 0.471x | 0.011x |

The complete version-3 reports, including every raw round, exact Wasm hashes,
tool fingerprints, wall/CPU/RSS values, and artifact bytes, are in
`dragline-external-compiler-arm64-2026-08-28.jsonl`.
