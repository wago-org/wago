# SIMD byte-projection reduction

Retain measurement source `73639628` on Hub AMD64 and Apple ARM64. The reduction proves a pure counter-to-byte projection and replaces the loop with a periodic sum. It accepts `i8x16.splat`, optional constant strict swizzle, and unsigned lane extraction. Selected swizzle indices outside 0–15 yield zero. Existing effect, control-flow, cancellation, type, and register-pressure gates remain in force.

The exact SDK swizzle fixture (SHA-256 `dbfb948584cf1d082de0b73c4965e761ba864e42d388a80d88d0f189e6463721`) returns 33423360 using the existing performance(4096), 64-repeat contract. Fresh and reversed SDK orders measure 201 ns on Hub versus approximately 5.98 µs for pinned w2c2-clang `f303ed27`; Apple measures 103–104 ns versus approximately 73–75 µs. Five public API pairs independently confirm the change, including preserved cold Apple measurements. Generated swizzle code changes 399→351 bytes on AMD64 and 416→432 on ARM64. All 254 other inspected images are identical on each host.

| Candidate / prior retained source | Hub `7be64a44` | Apple `1dda2768` |
|---|---:|---:|
| All 255 positive steady pairs, geometric ratio | 0.97379 | 0.97223 |
| Compile geometric ratio | 0.99953 | 1.00147 |
| Compile RSS geometric ratio | 0.99967 | 0.98704 |
| Feature steady ratio, 107 pairs | 0.93642 | 0.94345 |
| Exact successful feature peer cohort, 54 pairs | 0.87676 | 0.29792 |
| Exact successful all peer cohort, 202 pairs | 1.12996 | 0.78099 |

Each host has 798 full captures and 48 focused captures; source-order outlier rechecks add 68 Hub and 48 Apple captures. Each successful phase has 15 timing samples. Artifact, contract, source/runtime version, platform, resource, and functional outcome checks pass. Existing five JSPI failures remain unchanged. Native targeted tests pass 1,683 cases and Apple tests pass 1,709, including unsigned extrema, independent finite-period oracles, rejection cases, public configuration combinations, and artifact round trips.

Outliers remain unresolved. Hub memory sum repeats at +4.3/+15.0%, compact imports at approximately flat/+7.7%, and path-normalization compile time at +9.8/+9.9%. Apple vision dilation repeats at +10.7/+19.6%, despite an identical generated image. Apple bulk fill’s +24% full-pass outlier does not repeat (0.9974/1.0278). These controls do not receive performance improvement claims. Aggregate compile and RSS remain near the retained baseline; individual compile costs remain visible in the adjacent JSON.

Full-pass peer timings predate this snapshot; focused peer comparisons remeasure the pinned source in both orders. Compiler phases compare Wago JIT compilation with translation plus Clang and represent different work. Aggregate ratios do not establish parity for every feature. Apple ordinary thread callbacks remain about 1.8 times peer, compact imports about 1.55 times, and opaque Go callbacks about 22 times. The broader parity goal remains active.

Only the two complete retained candidate passes are eligible for the next local refresh, after 13:06:22 UTC on October 14. Baselines, focused repeats, and controls are excluded. Existing peer captures stay in place. No production deployment or push is authorized.
