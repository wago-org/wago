# Borrowed logical immediates: arm64 prototype

Rejected after mitigation; option removed from production. Experimental configuration was `borrowed-logical-immediate` or `WAGO_ARM64_EXPERIMENT_BORROWED_LOGICAL_IMMEDIATE=1`. Concrete borrowed local/global sources can feed an encodable AND/OR/XOR immediate directly, avoiding the source-to-result copy. No deferred source, nonencodable immediate, or nonlogical arithmetic is admitted. Pin the source during result allocation, restore inherited pins, and preserve explicit local destination aliasing. No corpus identifiers or argument values affect eligibility.

Retained source snapshot: `/tmp/wago-retained-before-logical-immediate`, with Go source hashes in `retained-before-logical-immediate-source.json`. Queued retained profiles build from this snapshot. Diagnostic qualification and affected-image inventory are queued behind them. Source-reuse tests cover both widths, dirty i32 carriers, varied masks including nonencodable/zero/all-one, commuted operands, and both bounds with per-compilation on/off overrides. Additional strict admission/explicit destination tests are needed before retention. No timing or correctness claim yet.

## Initial qualification

Backend/catalog/encoder diagnostics pass. The later strict admission assertion and old-source-live-below-destination-alias fixture also pass. Enabled candidate passes all46core and102application signal oracles; 32core and21application native images change. Retained-source rollback proof passes all46core oracles and all46native images match the independently qualified disabled-prototype baseline exactly. The frozen snapshot initially omitted root internal/codegen/profile facade packages; adding their unchanged sources resolved build failures. All three retained profiles completed with exact declared contracts.

Fresh ADPCM retained assembly confirms MOV X9,X19; AND W9,W9,#15 in its loop, while the candidate image is four bytes smaller. Sample observations guide instruction inspection and are not CPU-time weights. Eight affected applications selected by original gap ranking and two upstream core workloads are being timed with same-thread alternating per-compilation overrides (8rounds,200ms/state). Compilation and execution are measured separately. No performance claim yet.

## Initial paired tradeoff

| Workload | Phase | Disabled µs | Enabled µs | Change |
| --- | --- | ---: | ---: | ---: |
| audio-adpcm | compile | 21.571674 | 21.722171 | +0.70% |
| compiler-register-allocation | compile | 60.072233 | 60.014707 | -0.10% |
| graphics-reed-solomon | compile | 92.439403 | 92.083120 | -0.39% |
| hardware-prime-implicants | compile | 42.350549 | 41.881023 | -1.11% |
| language-register-vm | compile | 39.264057 | 39.551698 | +0.73% |
| ml-knn | compile | 66.196366 | 66.135179 | -0.09% |
| search-aho-corasick | compile | 187.828993 | 188.462007 | +0.34% |
| serialization-protobuf | compile | 30.317381 | 29.986972 | -1.09% |
| complex-roundtrip | exec | 159.412894 | 159.770769 | +0.22% |
| decimal-parse | exec | 0.681809 | 0.679895 | -0.28% |
| complex-roundtrip | compile | 743.333872 | 746.935515 | +0.48% |
| decimal-parse | compile | 1175.517265 | 1189.120511 | +1.16% |
| audio-adpcm | exec | 38.089353 | 39.052375 | +2.53% |
| compiler-register-allocation | exec | 17.294708 | 17.062409 | -1.34% |
| graphics-reed-solomon | exec | 123.614658 | 122.895037 | -0.58% |
| hardware-prime-implicants | exec | 49.682033 | 49.874300 | +0.39% |
| language-register-vm | exec | 11.043126 | 11.242090 | +1.80% |
| ml-knn | exec | 31.743598 | 31.318172 | -1.34% |
| search-aho-corasick | exec | 50.946735 | 50.717273 | -0.45% |
| serialization-protobuf | exec | 16.461338 | 16.996890 | +3.25% |

Core paired commands initially selected the application manifest by default; corrected to the explicit cache corpus and basename IDs and reran only the two core cases. Application timing outputs were preserved. General layout-preserving mitigation adds a NOP where a temporary source-copy was removed; selected five-case compile/exec comparison and diagnostics queued. Feature remains default off.

## Layout-preserving mitigation

Backend diagnostics pass. Five-case paired results:

| Workload | Phase | Disabled µs | Enabled µs | Change |
| --- | --- | ---: | ---: | ---: |
| audio-adpcm | compile | 20.730846 | 20.937313 | +1.00% |
| compiler-register-allocation | compile | 61.256414 | 60.989937 | -0.44% |
| language-register-vm | compile | 37.181293 | 37.223851 | +0.11% |
| ml-knn | compile | 63.378292 | 63.598247 | +0.35% |
| serialization-protobuf | compile | 31.693889 | 31.814305 | +0.38% |
| audio-adpcm | exec | 39.688138 | 40.617872 | +2.34% |
| compiler-register-allocation | exec | 16.757967 | 16.836173 | +0.47% |
| language-register-vm | exec | 11.130449 | 11.156378 | +0.23% |
| ml-knn | exec | 32.061202 | 32.030861 | -0.09% |
| serialization-protobuf | exec | 16.831077 | 16.952025 | +0.72% |

## Decision: rejected after mitigation

The compact version's ~1.3% register-allocation/KNN gains came with ADPCM +2.53%, protobuf +3.25%, and register-VM +1.80%; compile effects mostly within ~1%. Preserving layout reduced the protobuf/VM regressions but did not fix ADPCM (+2.34%) and eliminated the positive register-allocation/KNN result. Core fastfloat/kissfft execution was flat. Reject the general prototype after mitigation; source/tests archived under `experiments/rejected-borrowed-logical-immediate`, production hook/bindings/catalog removed. Retained baseline remains unchanged. These measurements do not establish that copy elimination is universally bad; they show this lowering's current allocation/layout tradeoff is insufficient.
