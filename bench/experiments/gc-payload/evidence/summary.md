# Matched payload-boundary microcomparison

Baseline `ccbb259` is runtime base `bb0c4e3` plus the exact same benchmark source as
candidate `737ea3c`. These are not measurements of the later canonical runtime.
Go 1.27.1, AMD64, GOMAXPROCS=1; three alternating fresh-process pairs, 100ms per
case. Builds finished before timing, known workers idle; shared-host noise remains
uncontrolled. All six processes completed successfully.

All cases reported **0 B/op and 0 allocs/op**. Constructor averages are benchmark
reports, not a claim that setup/collection never allocates. No consistent material
regression appears in this small sample. Tiny array-get median rose 3.8% with
mixed rounds and overlapping ranges; Throughput array-set rose 1.4% likewise.
Numeric array borrow was consistently lower across all runs, compatible with
resolving header and payload together. Other apparent gains are noisy; this is
not a universal speedup, V8 comparison, or formal equivalence proof.

| Profile / operation | Baseline median ns (range) | Candidate median ns (range) | Median change |
|---|---:|---:|---:|
| throughput/struct-get | 18.98 (15.88–19.44) | 16.65 (16.30–17.12) | -12.3% |
| throughput/struct-set | 30.57 (29.06–31.27) | 30.55 (30.30–32.24) | -0.1% |
| throughput/array-get | 27.41 (26.51–27.60) | 26.69 (26.36–32.41) | -2.6% |
| throughput/array-set | 37.97 (36.13–41.13) | 38.51 (36.02–43.97) | +1.4% |
| throughput/struct-new | 50.96 (47.86–67.32) | 44.76 (42.90–46.69) | -12.2% |
| throughput/array-new | 58.00 (55.92–61.97) | 55.41 (55.41–57.74) | -4.5% |
| throughput/array-borrow | 22.10 (21.65–23.53) | 17.20 (17.18–19.86) | -22.2% |
| tiny/struct-get | 16.78 (16.20–17.04) | 16.86 (16.77–17.78) | +0.5% |
| tiny/struct-set | 32.38 (30.20–32.61) | 31.53 (31.49–34.17) | -2.6% |
| tiny/array-get | 27.93 (27.65–28.43) | 28.98 (27.18–30.77) | +3.8% |
| tiny/array-set | 39.04 (36.59–49.38) | 37.54 (37.18–40.32) | -3.8% |
| tiny/struct-new | 105.70 (95.80–106.40) | 95.05 (92.86–98.47) | -10.1% |
| tiny/array-new | 137.30 (133.00–139.20) | 123.40 (122.00–135.80) | -10.1% |
| tiny/array-borrow | 22.79 (21.85–23.24) | 18.20 (17.60–18.64) | -20.1% |

Raw process output and binary SHA256 are in `results.json`; exact sources,
benchmark-source hash and build command are in `provenance.json`. Collector
layout/footprint is unchanged; correctness normal/debug tests were recorded
separately in the payload adapter preparation note.
