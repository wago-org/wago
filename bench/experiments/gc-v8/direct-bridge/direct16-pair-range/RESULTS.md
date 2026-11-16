# Recovered direct16 paired/range measurements

Source: `3e534ad605c148c6c636252fd852e06c4b6b8846`. Linux x86-64, Intel Xeon Platinum 8573C, Go 1.27.1. Raw evidence contains exact source, binary/artifact hashes, build tags, CPU and runtime receipts. Node 24.22.1 / V8 13.6.233.17-node.58 uses `--no-liftoff`.

## Completeness and limitations

Audit: **18/18** successful case/variant receipts, 128 warmups; allocations and actual collections verified for all six artifacts and three variants. Bounded comparison: **54/54** successful receipts, three rotating rounds of three warmups followed by five calls. Each round supplies one per-call timing sample. Full V8 gate: **6/72** expected receipts recovered, all successful, ending after paired array-allocation-access in round zero. Interruption reason is not recorded; this is not a completed gate and must not be presented as one. No missing rows are inferred or filled from other runs.

Checksums and artifact hashes match the case manifest in every recovered row. All Wago receipts confirm direct mode, installed native view, allocation elision off and compact/age2 off. Young budget is 2 MiB, old storage 2 × 8 MiB. Collection audit does not measure GC in isolation; these are end-to-end execution timings. V8 internal allocation elimination remains unmeasured. These are local synthetic cases, not a completed wasm.fyi suite.

## Bounded comparison

Median of the three round samples, microseconds per call. Ratios below 1 favor the candidate. All controls and observed regressions retained; this short noisy sample is a lead, not robust proof of improvement.

| Case | Baseline µs | Paired µs | Range µs | Paired/base | Range/base |
|---|---:|---:|---:|---:|---:|
| struct-allocation-access | 6340.484 | 6892.568 | 6179.227 | 1.087× | 0.975× |
| array-allocation-access | 12806.435 | 15596.773 | 16510.910 | 1.218× | 1.289× |
| array-fill | 14048.300 | 16773.831 | 18598.717 | 1.194× | 1.324× |
| array-copy-checksum | 379.310 | 342.495 | 234.155 | 0.903× | 0.617× |
| array-loop-copy-checksum | 717.504 | 527.987 | 327.043 | 0.736× | 0.456× |
| tree-survival | 2786.200 | 1982.205 | 1837.906 | 0.711× | 0.660× |

Range improves the two copy medians in this bounded run, but array allocation and fill regress substantially. Tree and struct controls also move, indicating noise or code-layout effects rather than clean attribution. Do not enable a candidate based on this run alone.

## Partial V8 gate

Nine 32-call samples per recorded process after 100 separate warmups. Only struct-allocation-access has a matched V8 receipt (one of three planned rounds):

| Variant | Struct µs | Wago/V8 |
|---|---:|---:|
| baseline | 5517.050 | 4.218× |
| paired | 5722.295 | 4.375× |
| range | 5582.644 | 4.268× |
| v8 | 1307.957 | 1.000× |

All recorded Wago struct variants miss the ≤1.15× target. The two partial array-allocation receipts lack a V8 reference: baseline 12204.467 µs, paired 11429.712 µs. No other full-gate case comparison is available.

## Compilation and memory costs

No compilation timing, compilation memory, or generated machine-code byte measurements were recovered. Empty build logs do not establish those costs. Executable sizes (including test harness/debug information, not generated Wasm code size):

| Variant | Test binary bytes | Delta |
|---|---:|---:|
| baseline | 50420578 | +0 |
| paired | 50457414 | +36836 |
| range | 50485724 | +65146 |

Next: reserve an uncontended CPU window, rerun the entire rotating full gate without splicing partial runs, and measure per-artifact compilation time/allocations/code bytes using the existing compilation-footprint protocol. Keep paired/range opt-in until correctness qualification and repeatable gains justify promotion.

## Fresh rerun commands

From the repository root, after CPU coordination, with the existing identified binaries and a fresh output directory (never overwrite/splice the recovered partial run):

```sh
B=/workspace/shared/gc-direct-pair-range-evidence
P=bench/experiments/gc-v8/direct-bridge/direct16-pair-range
O=/workspace/shared/gc-direct-pair-range-rerun
python3 "$P/run.py" --binary-dir "$B" --output-dir "$O/audit" --audit
python3 "$P/run.py" --binary-dir "$B" --output-dir "$O/bounded" --matched
python3 "$P/full-v8.py" --binary-dir "$B" --output-dir "$O/full" --node node
```

Use Node 24.22.1 (verify `node --version`); verify binary SHA-256s against the evidence before reuse. The driver records its checkout HEAD, which is not necessarily the binary's build source: retain the original binary source attribution above when reusing binaries after reporting-only commits. For changed source, rebuild all three variants together and record the actual build source instead.

Scheduling estimate, not a measured completion time: reserve 5–10 minutes for these runtime probes, excluding rebuilding and compilation-footprint work. Recovered audit test durations total 20.95 seconds; bounded tests total 4.06 seconds. Five recorded Wago full-gate processes total 16.51 seconds; the remaining cases/rounds and process overhead are not measured. A fresh run is necessary to establish the true duration.
