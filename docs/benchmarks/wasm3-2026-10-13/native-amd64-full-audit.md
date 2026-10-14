# Full native AMD64 audit

Source `d4a5d2ff` is retained after the complete 266-case baseline/current/peer audit on Hub's Ryzen 7 7800X3D, Linux AMD64, CPU 7. Go 1.27.1 and pinned SIMD w2c2-clang `f303ed27` / Clang 18.1.3 match the focused comparison. This is a local-only snapshot; the broader parity goal remains incomplete.

The audit verified 798 captures and 3,192 phase rows, exact source/runtime versions, artifact and contract hashes, resource outcomes, and sample counts. Wago's functional outcomes are unchanged. Five native JSPI host-profile failures remain; the peer records 29 failed cases, including unsupported contracts. These outcomes remain nonnumeric. No browser-only Go counterpart was implemented.

| Wago after / before | Main | Features | All |
|---|---:|---:|---:|
| Positive steady pairs | 148 | 107 | 255 |
| Steady geometric ratio | 0.9882 | 0.8807 | 0.9416 |
| Compile geometric ratio | 0.9964 | 0.9992 | 0.9975 |
| Compile RSS geometric ratio | 0.99985 | 1.00029 | 1.00003 |

Peer comparisons use their own exact successful common cohort:

| Steady common cohort | Pairs | Before / peer | Current / peer |
|---|---:|---:|---:|
| Main | 148 | 1.2525 | 1.2377 |
| Features | 54 | 2.0726 | 1.8427 |
| All | 202 | 1.4330 | 1.3767 |

The 255 before/current pairs and 202 peer pairs are different cohorts. Their ratios must not be divided to infer another comparison. Compile phases also compare different JIT and native C compilation pipelines.

Fourteen workloads were repeated in both source orders with isolated baseline/current SDK roots: 56 more captures and 224 phases. Ordinary thread callbacks improved 93.1–93.2%, compact imports 97.5%, and the minimal host loop 75.6–75.7%. Opaque Go callbacks remained within 0.5% of baseline. See the earlier focused report for the fresh/reversed peer timings, where ordinary thread and compact callbacks outperform clang.

Outliers remain visible. Graph Kruskal reversed direction (0.753 and 1.354); same-binary controls also varied by up to 26%. Many-functions results quantize around 40–50 ns. Audio autocorrelation remained 8.3–11.8% slower in both repeats, while its same-binary controls stayed near 1.0; this is an unresolved regression. Actual adapter code-image exports prove that audio, graph, and eight other control workloads have byte-identical generated code across the source change. This narrows the investigation but does not erase the measured audio slowdown. Compile outliers also repeated with mixed signs; all raw outcomes are retained in validation artifacts.

Follow-up ARM64 experiments were withheld: broader fixed-shared direct Go lowering improved opaque callbacks but regressed ordinary native callbacks; removing shared-base reloads had no useful gain; borrowed memory bases and masked-shift fusion also regressed the ordinary thread workload. All prototype source and tests were removed from the working tree. Their evidence remains under `.validation/wasm3-parity/native-leaf-amd64`.

The adjacent JSON records cohort counts, repeated results, image hashes, same-binary controls, and caveats. The planned local refresh contains only the complete candidate and peer full passes, excluding baselines, repeats, and controls. Existing Apple M4 results remain source `334732e1`; native Hub results belong to their own platform metadata. No production deployment or push is authorized or performed.
