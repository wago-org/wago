# Broad fixed-profile source organization outcome

All arms use T402000/R4096, all 20 experimental flags OFF, full API. All fixed rows verified: long540/cold2900/RSS435/pages435. First attempt, no reruns. Source trees remain clean. These are combined source-organization effects, including small trusted-Go register/load/stack codegen changes, not pure page-layout effects or pristine-main40% qualification.

## Cold compile

Arithmetic mean of per-workload medians; 5000 paired within-workload trial bootstraps over the fixed145 suite.
- Baseline A/B: 1.00327 [0.95824, 1.10308]
- Early/baseline: 1.01970 [0.95953, 1.06006]
- Late/baseline: 0.99624 [0.94814, 1.03188]
- Early/late: 1.02354 [0.97379, 1.07881]
- Baseline control FAILS. No established compile gain or40% qualification.

## Independent compiled-barrier RSS435

| Arm | Mean MiB | Min | Max | Below5 MiB |
|---|---:|---:|---:|---:|
| Baseline | 9.444073 | 8.839844 | 12.199219 | 0/145 |
| Early | 9.471821 | 8.925781 | 11.808594 | 0/145 |
| Late | 9.621686 | 8.929688 | 12.250000 | 0/145 |

No RSS improvement versus baseline and no sub5MiB result. Page-worker estimates are separate measurements.

## All15 execution cases

Ratios with paired95% intervals; late guard additionally requires the same passing baseline control. Historical subgroup labels in raw analysis are not new eligibility/control designations.

| Case | A/A control | Early/base | Late/base | Early/late | Early guard | Late guard |
|---|---|---|---|---|---|---|
| wago/blake3/hash | FAIL 1.01930 [0.96041, 1.13122] | 1.02213 [0.94272, 1.05921] | 0.97943 [0.93268, 1.03938] | 1.04360 [0.94671, 1.10136] | NOT QUALIFIED | NOT QUALIFIED |
| wago/monocypher/aead-blake2b | FAIL 1.00182 [0.99932, 1.42865] | 0.99967 [0.83572, 1.16861] | 0.99908 [0.82586, 1.44551] | 1.00059 [0.70237, 1.41501] | NOT QUALIFIED | NOT QUALIFIED |
| wago/pcre2/compile-match | PASS 0.98622 [0.97488, 0.99714] | 1.00219 [0.99148, 1.01873] | 1.00584 [0.99672, 1.01355] | 0.99637 [0.98947, 1.00847] | PASS | PASS |
| wago/yyjson/parse-edit-write | FAIL 1.05407 [0.97845, 1.10203] | 0.97718 [0.90859, 1.02551] | 0.96314 [0.92937, 1.03516] | 1.01458 [0.91678, 1.07352] | NOT QUALIFIED | NOT QUALIFIED |
| applications/search-aho-corasick | FAIL 1.03152 [0.97201, 1.09962] | 1.02037 [0.93974, 1.06254] | 1.00261 [0.95263, 1.11402] | 1.01771 [0.92027, 1.08219] | NOT QUALIFIED | NOT QUALIFIED |
| wago/nanosvg/parse-structure | PASS 1.00353 [0.97881, 1.02105] | 0.99694 [0.97809, 1.01533] | 1.00764 [0.98181, 1.02996] | 0.98939 [0.95673, 1.01549] | PASS | PASS |
| wago/libtommath/modular-arithmetic | PASS 0.99896 [0.99066, 1.01250] | 0.99137 [0.98166, 1.00660] | 1.00838 [0.99596, 1.07659] | 0.98313 [0.93442, 1.00023] | PASS | NOT QUALIFIED |
| wago/drwav/pcm-decode-seek | PASS 1.00060 [0.99654, 1.00338] | 0.99682 [0.99359, 1.00293] | 0.99682 [0.99288, 0.99983] | 1.00000 [0.99752, 1.00648] | PASS | PASS |
| applications/bio-global-alignment | FAIL 0.98919 [0.92441, 1.04339] | 1.00395 [0.96781, 1.02731] | 0.96761 [0.94195, 1.02653] | 1.03755 [0.98143, 1.03993] | NOT QUALIFIED | NOT QUALIFIED |
| wago/fastfloat/decimal-parse | FAIL 0.97836 [0.95605, 1.03848] | 0.97728 [0.94035, 0.99433] | 0.98962 [0.95115, 1.07467] | 0.98753 [0.90221, 1.00000] | NOT QUALIFIED | NOT QUALIFIED |
| applications/ml-kmeans | FAIL 1.00642 [0.95342, 1.03793] | 0.99015 [0.96412, 1.01779] | 0.99564 [0.96775, 1.02458] | 0.99448 [0.96413, 1.02300] | NOT QUALIFIED | NOT QUALIFIED |
| applications/crypto-aes128 | FAIL 0.97987 [0.97262, 0.99719] | 1.05360 [0.97748, 1.07582] | 0.98587 [0.96397, 1.05947] | 1.06870 [0.98629, 1.09017] | NOT QUALIFIED | NOT QUALIFIED |
| applications/search-rabin-karp | FAIL 1.00666 [0.96345, 1.04756] | 1.00623 [0.97281, 1.03451] | 1.01397 [0.97703, 1.06091] | 0.99237 [0.94376, 1.04158] | NOT QUALIFIED | NOT QUALIFIED |
| applications/document-kerning | FAIL 1.00403 [0.95075, 1.08050] | 0.99185 [0.97175, 1.05958] | 1.02491 [0.95855, 1.04535] | 0.96775 [0.95989, 1.05704] | NOT QUALIFIED | NOT QUALIFIED |
| applications/files-path-normalize | FAIL 0.97691 [0.91167, 1.03296] | 0.99756 [0.95398, 1.03534] | 1.00984 [0.95289, 1.08734] | 0.98784 [0.90889, 1.07878] | NOT QUALIFIED | NOT QUALIFIED |

Only pcre2, nanosvg, libtommath and drwav pass baseline controls. All four pass early execution guard; late fails the libtommath upper bound. Remaining11 cases are unqualified, including AES (largest early point regression,1.05360) and fastfloat (favorable point but failed control). No overall execution-guard qualification.

Complete raw receipts are retained separately; no favorable-case retry or omission. All phases completed on their first attempt. A stale copied source annotation in cold-analysis output was corrected without changing numbers or rerunning measurements.
