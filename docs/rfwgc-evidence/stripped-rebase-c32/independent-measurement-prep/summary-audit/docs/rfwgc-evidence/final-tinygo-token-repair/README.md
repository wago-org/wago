# TinyGo public-global token repair qualification

Production repair: `c786a9b84bb0298d5bbd6b9522ab611193440cfa` (`fix(gc): preserve invalid-token classification for public globals`).

This report is correctness evidence only. It grants no final approval, fresh-review approval, performance qualification, or non-Linux execution claim. Parent owns fresh review.

Original full foreign-stack lane: **34/34 PASS**. All nine evacuation/joint/conversion and all three public/rejection supplements also passed on the repair. Original test sources and expectations were unchanged. All three threaded guest modes pass the original cross-runtime/forged classification checks, including their subsequent unchanged-null assertions. All eleven CLI Fibonacci checks produced 6765.

| Supplemental gate | Exit | Passed events | Skips | Missing required tests |
|---|---:|---:|---:|---:|
| tinygo-evacuation-ordinary | 0 | 15 | 0 | 0 |
| tinygo-joint-ordinary | 0 | 9 | 0 | 0 |
| tinygo-conversion-ordinary | 0 | 5 | 0 | 0 |
| tinygo-repair-ordinary | 0 | 22 | 0 | 0 |
| tinygo-evacuation-wagodebug | 0 | 30 | 0 | 0 |
| tinygo-joint-wagodebug | 0 | 9 | 0 | 0 |
| tinygo-conversion-wagodebug | 0 | 5 | 0 | 0 |
| tinygo-repair-wagodebug | 0 | 22 | 0 | 0 |
| tinygo-evacuation-wago_regalloccheck | 0 | 15 | 0 | 0 |
| tinygo-joint-wago_regalloccheck | 0 | 9 | 0 | 0 |
| tinygo-conversion-wago_regalloccheck | 0 | 5 | 0 | 0 |
| tinygo-repair-wago_regalloccheck | 0 | 22 | 0 | 0 |
| native-amd64-ordinary | 1 | 35 | 0 | 4 |
| resume-tinygo-regression-ordinary | 0 | 13 | 0 | 0 |
| resume-tinygo-regression-wagodebug | 0 | 13 | 0 | 0 |
| resume-tinygo-regression-wago_regalloccheck | 0 | 13 | 0 | 0 |
| resume-native-amd64-ordinary | 0 | 40 | 0 | 0 |
| resume-native-amd64-wagodebug | 0 | 40 | 0 | 0 |
| resume-native-amd64-wago_regalloccheck | 0 | 40 | 0 | 0 |
| resume-native-arm64-ordinary-build | 0 | 0 | 0 | 0 |
| resume-native-arm64-ordinary | 0 | 40 | 0 | 0 |
| resume-native-arm64-wagodebug-build | 0 | 0 | 0 | 0 |
| resume-native-arm64-wagodebug | 0 | 40 | 0 | 0 |
| resume-native-arm64-wago_regalloccheck-build | 0 | 0 | 0 | 0 |
| resume-native-arm64-wago_regalloccheck | 0 | 40 | 0 | 0 |
| resume-native-riscv64-ordinary-build | 0 | 0 | 0 | 0 |
| resume-native-riscv64-ordinary | 0 | 24 | 0 | 0 |
| resume-native-riscv64-wagodebug-build | 0 | 0 | 0 | 0 |
| resume-native-riscv64-wagodebug | 0 | 24 | 0 | 0 |
| resume-native-riscv64-wago_regalloccheck-build | 0 | 0 | 0 | 0 |
| resume-native-riscv64-wago_regalloccheck | 0 | 24 | 0 | 0 |
| converted-arm64-ordinary | 0 | 7 | 0 | 0 |
| converted-arm64-wagodebug | 0 | 7 | 0 | 0 |
| converted-arm64-wago_regalloccheck | 0 | 7 | 0 | 0 |
| converted-riscv64-ordinary | 0 | 5 | 0 | 0 |
| converted-riscv64-wagodebug | 0 | 5 | 0 | 0 |
| converted-riscv64-wago_regalloccheck | 0 | 5 | 0 | 0 |

The first native attempt failed because the newly authored regression imposed local-global error wording on the existing native owned-global adapter. Native owned globals already return invalid or foreign externref token. The native production files are unchanged by the repair. STOP-attempt1.json, regression-attempt1.go.txt, and the failed raw log remain preserved. Only the new test was corrected to require each adapter’s established invalid-token wording; original expectations were not edited. The resume rows qualify the final test source in a fresh immutable export.

Counts include test/subtest events. Exact names and commands are retained in `full34-census.json`, `supplement-results.json`, `resume-results.json`, `native-converted-results.json`, and command receipts. Build gates are not execution passes.

The focused new regression checks local Instance.SetGlobalValue and owned Global.SetValue separately. Foreign, forged, and released tokens must retain invalid-token classification and leave the destination null. On unsupported opaque-publication adapters an authenticated wrapper must retain the unsupported-conversion rejection and leave the destination null. Ordinary positive publication and null clearing must work. Existing conversion and unsupported-publication fixtures supply the genuine converted-value coverage.

The test-only TinyGo portability adapter changes only the build constraint of design_repair_unsupported_test.go. Its body and the original source are hashed separately. No original expectation, production source, compiler source, or submodule was changed.

The original 34-gate export contains only the immutable repair. Supplements use a separate export of that same repair with the owned regression and test-only build-constraint adapter recorded by hash. Native ARM64 and RV64 results, when present above, are actual QEMU user-mode execution of cross-built ELF test binaries, not physical hardware results. Native AMD64 uses the standard Go runtime.

Unrun on this repair: actual Darwin and Windows execution, native ARM64/RV64 hardware, TinyGo ARM64 and other TinyGo platforms, and the unrelated broad suites represented by the historical f2bb 173 passes. Those historical passes are not credited to this revision. This report does not authorize a final combined-head verdict.

Raw logs are stored losslessly as `.log.gz`; the compression ledger records original and compressed hashes. `final-summary.json` distinguishes final coverage from preserved failed attempts. Original attempts remain preserved. `historical-f2bb-failed-guest.log.gz` retains the old raw failed guest output and its original raw hash. The new run does not relabel it. Driver/export/tool provenance and integrity receipts are included. Build caches and source exports are private under the recorded /tmp workspace. No benchmarks, measurement lease, push, PR, delegation, or Claude review occurred.

Only the new test and this evidence directory are owned. These test/documentation additions do not alter production binaries or performance; the parent production repair receives no performance claim from this work.
