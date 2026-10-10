# General store-map vectorization: rejected prototype

Apple M4 Max, arm64. Rejected and removed from the production compiler after mitigation and paired cost measurements. Production recognition uses decoded pure integer operations, induction steps and memory-range proofs; it reads no workload identities or argument values.

The prototype processes four iterations together, with full invocation bounds and disjointness guards before guest stores. Failed guards and scalar tails execute the original loop. Native tests cover aliases, partial effects before traps, final local values, unaligned stores and both bounds modes. The initial integrated version passed all 102 signal application oracles. Subsequent cost mitigations passed native diagnostics and 6,912 emitted ARM64 guard cases; final source needs refreshed application qualification before retention.

## Qualified affine prototype subset

Bracketed 100 ms pairs, four samples per state. These are focused local comparisons, not a refreshed full ranking.

| Workload | Scalar execution µs | Vector execution µs | Execution change | Compilation change |
| --- | ---: | ---: | ---: | ---: |
| image-resize | 22.845 | 15.405 | -32.6% | +3.6% |
| vision-components | 37.543 | 30.384 | -19.1% | +6.1% |
| ml-convolution | 25.125 | 21.552 | -14.2% | roughly flat |
| image-composite | 70.724 | 62.493 | -11.6% | +6.7% |
| vision-dilation | 42.633 | 38.112 | -10.6% | roughly flat |

ML inference and quickselect have severe off-batch outliers. Do not count pooled ML compilation -31.3% or quickselect execution -71.4%. Normal adjacent samples suggest roughly 12% execution gains; ML compilation is about 19–21% higher. Confirm before deciding. DCT's native image is unchanged; its execution variation is not a vectorization gain.

## Mitigations already measured

Initial components execution improved 16.8% with compilation +16%. The first broader repeat showed resize compilation +51.8%. Removing unused original-local snapshots and guard workspace operations brought resize compilation to +11%, while preserving the execution benefit. Folding monotone positive additions over original address locals reduced it further to +3.6% in the latest pairs. General shifted/multiplied address DAGs retain the original guard path. No workload-specific selection was introduced.

## Latest qualification and remaining work

The device pause was released and both queued jobs finished. The large-SP workspace fix passed six emitted ARM64 address cases and compiler/runtime/encoder/catalog diagnostics. Remaining-image timings are in `store-map-remaining-summary.tsv`: max pooling execution -29.9%, register VM -17.1%, deblocking -16.4%, error diffusion -10.8%. Audio autocorrelation, sort-merge join, path trie and bootstrap gain little while compilation rises roughly 5–14%; this tradeoff still needs mitigation before retention.

ML inference's pooled +87% compilation result includes an outlier on batch. A separate two-second profiled compile comparison measured 30.399 to 37.842 µs (+24.5%), with 104 to 108 allocations. CPU samples are dominated by scheduler/syscall locations; they do not establish those as compiler bottlenecks. Escape-analysis output is retained for further investigation.

Unit and power-of-two trip steps now use copies/shifts instead of division. Expanded emitted guards passed 20,736 unit/power-of-two/non-power-of-two cases. Focused repeat raw data is in `store-map-trip-*-*.txt`; pooled resize and quickselect changes contain outliers and must not be claimed as gains. Original affine subset raw timings are preserved under `store-map-affine-before-trip/`.

Fresh Wago/Samply capture `profile-components-store-map-trip-qualified` passed the unchanged 128 -> 1698848657 contract and completed 88,854 prepared invocations in three seconds. Its observations can include off-CPU samples and are not execution timings. The initial capture attempt used the wrong export name and failed; the qualified capture uses `benchmark`.

Still required: further compilation-cost mitigation, refreshed final-source application oracles in both bounds modes, and public per-compilation policy/rollback qualification before default retention. The prototype remains opt-in.

Raw evidence: `store-map-affine-before-trip/`, `store-map-remaining-summary.tsv`, `store-map-stack-*.txt`, `store-map-trip-guard-emulation.txt`, `store-map-trip-compile-*-top.txt`. Full history and earlier rejected/mitigated results are in `NOTES.md`.

## Latest cost mitigation results

Buffer ownership now avoids the second staging allocation after first-function encoder growth. Native bytes stayed identical; ML's vectorized compile uses about 4 KiB less memory and one fewer allocation, with compilation about 1.7% faster versus the preceding prototype. Simplifying constant-distance alias guards reduced compilation another 1–3% in the focused subset. That version passed all 20,736 emitted guard cases and all 102 application oracles in both explicit and signal bounds modes.

These improvements do not yet justify enabling every recognized store-map loop. Sort-merge join still has little execution benefit and a +2.7% execution variation in the latest paired subset; its compilation tradeoff needs further work. Public policy/rollback qualification and a final on/off cost decision remain outstanding. Default vectorization remains disabled.

Packed address-proof workspace is qualified: all 20,754 emitted guard cases and all 102 application oracles in both bounds modes pass. Compared with the preceding prototype, compilation improved 2–6% across the six-case subset, with execution gains preserved. A fresh validated `profile-components-store-map-packed` capture reports a 304-byte frame and 2,260-byte native function, versus the earlier prototype's 1,152-byte frame and 2,724-byte function. Profile observations are not CPU-time attribution. The general packing change does not alter admission or the guard predicate.

Public experimental option `store-map-vector` is now bound per compilation and defaults off. Concurrent on/off policy isolation and native admission checks pass. The same-thread execution comparison reports median paired changes of resize -35.6%, max pooling -34.1%, components -28.9%, ML -10.2%; the weak cases remain near flat. A matching compilation-phase comparison is queued. OS thread locking and user-interactive QoS do not bind a physical core. These figures remain prototype results, not a refreshed default-engine ranking.

## Final decision

The mitigated vectorizer remains a poor default tradeoff for multiple recognized loop classes. Same-thread paired compilation rises 10.4% for bootstrap and 11.2% for sort-merge join, with approximately 1% execution gains; ML compilation rises 19.6% for 10.2% execution gain. Strong cases gain substantially, but weak and strong cases share the same operation shapes, so a corpus-independent arithmetic-complexity cutoff cannot separate them.

The implementation, diagnostics and tests are archived as `.go.disabled` files under `rejected-store-map/`. The public experimental option and production hooks are removed. The independently qualified heap code-buffer ownership optimization remains. The same-thread harness, measurements and profiles remain available as evidence. Production rollback diagnostics, both application oracle modes and native-byte identity checks are queued in `store-map-removal-qualify.sh`. This decision does not complete the broader parity goal.
