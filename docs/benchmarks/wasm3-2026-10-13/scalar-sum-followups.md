# Scalar sum follow-ups

Retain native AMD64 measurement source `7be64a44` and Apple ARM64 source `1dda2768`. Both complete candidate passes remain local-only. The broader each-feature parity goal is incomplete.

On Hub's Ryzen 7 7800X3D, Go 1.27.1 and pinned SIMD w2c2-clang `f303ed27` / Clang 18.1.3, bounded population-count and signed-affine reductions replace repeated scalar iteration. The signed reduction uses four general-purpose registers and frame scratch homes for Euclidean descent, with pressure, interruption, control-flow, and effect guards. Maximum-bound tests use independent bit-cycle, finite-period, and complete-sequence oracles. The final targeted native validation passed 1,586 tests; an additional known-zero accumulator boundary test verifies canonical i32 population-count results.

Fresh and reversed SDK orders put population count at 152–153 ns versus clang's 6.45–6.50 µs. All five sign-extension contracts also beat the pinned peer: Wago takes 0.55–1.89 µs, compared with approximately 6.5–6.8 µs for clang. These timings use the actual SDK fixtures, bindings, entry API, and 64-fold workload amortization.

| Native `7be64a44` / `36ed0ba6` | Main | Features | All |
|---|---:|---:|---:|
| Positive steady pairs | 148 | 107 | 255 |
| Steady geometric ratio | 0.99567 | 0.82367 | 0.91951 |
| Compile geometric ratio | 0.99447 | 1.00726 | 0.99982 |
| Compile RSS geometric ratio | 0.99982 | 1.00009 | 0.99993 |

The exact successful peer cohort contains 202 steady pairs, including 54 feature pairs. Current/peer is 1.16725 overall and 0.99471 for features: aggregate feature parity, with individual gaps still present. SIMD swizzle, saturating float conversion, and opaque Go host transitions remain slower. Do not infer a comparison by dividing ratios from different cohorts.

The native audit covers 798 full captures, 114 focused captures, and 162 original/intermediate/current repeat captures. All 798 full captures have identical platform metadata; source/runtime versions, artifacts, contracts, samples, and resources are checked. Wago's functional outcomes are unchanged, including five native JSPI profile failures. Only the five intended sign-extension images change across 255 inspected workloads. Population count changes one image in its preceding audit; the preceding zero-term cleanup changes six.

Outliers remain explicit. Quickselect repeats at approximately +4.6–4.8% and stencil at +7.9–10.2% against the immediate baseline; stencil reverses against the prior source. Max pooling and GC vary across repeated and same-binary controls. The earlier native-leaf audio regression against its original baseline remains unresolved. Byte-identical generated control code narrows these investigations without erasing the measurements.

On Apple M4 Max, removing zero counter contributions reduces six code images, including exported-global reads from 432 to 384 bytes. Five alternating public API pairs improve 9–12%; both SDK orders confirm 111–114 ns versus 121–128 ns immediately before the change and 266–278 ns for the pinned peer. The full 266-case pass has unchanged functional outcomes, steady ratio 0.99027, compile ratio 0.98015, and compile RSS ratio 1.00099 against retained source `334732e1`. There are 798 full, 72 focused, and 102 three-source repeat captures; 249 of 255 inspected control images are identical. Bulk fill reverses direction in repeats (0.730/1.418 against the original source), so its full-pass outlier is not presented as a stable change.

Apple host-call gaps remain: ordinary threads are about 1.8 times the peer, compact imports about 1.57 times, and opaque callbacks about 21 times. A wrapper experiment removed redundant pointer copies and result canonicalization, but same-binary tests regressed ordinary threads while leaving compact imports near flat. Preserving the staged entry's layout did not produce a reliable win. Its source and tests were removed; private evidence remains in validation artifacts.

The adjacent JSON records cohorts, source identities, focused and repeated timings, changed image hashes, and audit digests. Publish only the complete retained candidate passes at the next local refresh; pinned peer full passes are already present. Exclude baselines, repeats, and noise controls from ingestion. No production deployment or push is performed.
