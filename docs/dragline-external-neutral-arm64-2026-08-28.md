# Dragline external neutral-corpus qualification — ARM64 — 2026-08-28

This is the current-host isolated-process compilation gate for 29 non-SIMD
optimized-Wasm application and kernel modules. Each engine ran in a fresh child
process for six balanced alternating rounds. Ratios below divide the median
Dragline result by the median Cranelift result for the same Wasm; geometric
means are across the 29 per-module ratios.

The harness includes process startup and artifact serialization. Artifact byte
ratios compare different serialized container formats and are footprint
observations, not native-code-quality measurements.

## Environment and commands

- Host: Darwin ARM64, Apple M4 Max.
- Dragline: built-in compatibility and native workers, Go 1.26.5, executable
  SHA-256 `23a2c05c53fd59f0b3064239fc1dd0e1a667503a05376b7882e56898cebca82a`.
- Dragline function policy: eight-worker forced maximum. Independent call-graph
  components run concurrently after their callees complete; schedule candidates
  for machine functions with at least 1,024 instructions also use three bounded
  workers. Final code layout and artifacts remain deterministic and byte-identical
  to the serial path.
- Cranelift: Wasmtime 46.0.1 (`823d1b8f2`, 2026-06-24), executable SHA-256
  `ac78ac0fb2715d2ff03cb1944e17b2aa76cc308baad315b92162db8562a4c15d`,
  configured with `opt-level=2,regalloc-algorithm=backtracking` and its normal
  speed-oriented parallel compilation.
- LLVM: unavailable; `wasmer-llvm` was absent from `PATH` and is recorded as
  unavailable in every raw report.

## Aggregate result

| Dragline mode | Wall geometric mean | Wall range | Wall wins | CPU geometric mean | CPU range | RSS geometric mean | RSS range | Artifact geometric mean |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Compatibility | 0.983x | 0.490–2.554x | 24/29 | 0.563x | 0.071–0.947x | 0.537x | 0.460–0.855x | 0.060x |
| Native | 0.966x | 0.483–2.554x | 24/29 | 0.556x | 0.070–0.951x | 0.538x | 0.461–0.839x | 0.060x |

Every module passes the master plan's current isolated-process compilation
gates against Cranelift: the worst wall ratio is 2.554x, below the 3x ceiling,
and the worst peak-RSS ratio is 0.855x, below the 1.5x ceiling. Dragline uses
less median total CPU and peak RSS on every module. It wins wall time on 24 of
29 modules in each target mode.

The five wall-time losses remain within the gate:

| Module | Compat wall | Native wall | Compat CPU | Native CPU | Compat RSS | Native RSS |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `blake-as` | 1.444x | 1.447x | 0.947x | 0.951x | 0.689x | 0.688x |
| `json-as` | 1.681x | 1.673x | 0.564x | 0.553x | 0.731x | 0.713x |
| `regexmatch` | 1.617x | 1.618x | 0.735x | 0.734x | 0.855x | 0.839x |
| `utf-as` | 1.169x | 1.163x | 0.544x | 0.544x | 0.543x | 0.547x |
| `wasm3` | 2.554x | 2.554x | 0.676x | 0.670x | 0.832x | 0.837x |

The complete version-3 reports, including all 522 measured child processes,
exact Wasm and executable hashes, tool versions, wall/CPU/RSS values, and
artifact bytes, are in
`dragline-external-neutral-arm64-2026-08-28.jsonl`.

This discharges the current ARM64 neutral-corpus Cranelift compile-latency and
memory gate. It does not discharge execution-quality, native AMD64, Linux PMU,
or LLVM qualification.
