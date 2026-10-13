# Complete main and feature snapshot at 34b940db

Collected all 266 local corpus workloads on Apple M4 Max under the shared device
lock: 148 main workloads and 118 feature workloads. Every capture includes four
phases, three launches × five samples per successful phase, and resource status.
All main workloads passed. Feature steady outcomes were 107 passed and 11
unsupported; there were no collector errors or resource failures. Component
instantiate/first-call timing can be measured while native code size remains
unsupported; these missing code measurements are preserved explicitly.

The audit checks exact source identity against each plan, workload/artifact
coverage, phase/sample counts, and matching contract hashes before comparing
latencies. All 798 current, baseline, and peer captures are retained privately;
[full-parity-34b940db.json](full-parity-34b940db.json) contains hashes, source
identities, exact-contract pair values, statuses, and the complete collection
plan. Peer SIMD w2c2 Clang is pinned at f303ed27. Its timings precede the current
snapshot, so these comparisons describe progress rather than isolate one change.

| Steady cohort | Current / peer geomean | Slower successful pairs | Current / earlier Wago geomean |
| --- | ---: | ---: | ---: |
| Main | 1.1260× | 109 / 148 | 0.9615× vs a7a9b6e9 |
| Features | 0.3620× | 13 / 54 | 0.9957× vs 6b46132b |
| Combined | 0.8313× | 122 / 202 | 0.9757× across the two explicit baselines |

The main and feature baselines are different source revisions. Their combined
ratio is an equal-workload descriptive rollup, not a fabricated unified source
snapshot. Unsupported, failed, zero-latency, and missing measurements do not
enter numeric ratios. Compile phases compare JIT work with translation/Clang
work and must not be described as equivalent native compiler jobs.

Each-feature parity remains incomplete. Leading successful feature gaps include
opaque Go callbacks (57.2× peer time), ordinary compact imports (3.55×), the
shared-memory identity profile (3.14×), ordinary memory.fill (1.88×), ordinary
indirect calls (1.73×), f32 saturation (1.51×), and f64 global arithmetic
(1.30–1.34×). The separate paired experiments document which changes repeat;
this full snapshot does not replace that attribution evidence.

A complete 266-capture package is prepared for local publication no earlier than
2026-10-14 00:58:58 UTC, respecting the requested roughly two-hour refresh.
Publication must verify every row and preserve history, then inspect the actual
Features page in Chrome. At report creation the package is private and the
previous complete 6b46132b feature snapshot remains served locally. No production
publication or push was performed.
