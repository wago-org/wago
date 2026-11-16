# ABI4 loop/copy evidence

Measured source `978d456`, runtime `6de74d7`. Same-source baseline and candidate both use ABI4; candidate additionally enables loop and checked-copy capabilities. Allocation elision is disabled. Both old semispaces remain 8 MiB. Full gates use 100 warmup calls, 9 batches of 32 calls and three rotating process rounds. The 2 MiB audit alone uses 128 warmups. Short A/B uses 100 warmups and five timed calls per process. All 36 audit processes and 108 execution timing processes passed. Separate compile measurements used three rotating processes per binary, 256 Compile+Close iterations per case.

## matched

| Case | Engine | Median ms | Process medians ms | Candidate/reference | Timed minor/full | Peak RSS MiB |
|---|---|---:|---|---:|---|---:|
| struct-allocation-access | baseline | 6.4406 | 6.3777, 6.4406, 6.9170 | reference | 89/2 | 36.45 |
| struct-allocation-access | candidate | 7.3106 | 6.5169, 7.3106, 7.5027 | 1.135x | 89/2 | 36.66 |
| array-allocation-access | baseline | 14.1689 | 14.1689, 14.7087, 13.9454 | reference | 204/9 | 37.13 |
| array-allocation-access | candidate | 14.5384 | 14.5384, 13.5835, 16.5493 | 1.026x | 204/9 | 37.47 |
| array-fill | baseline | 16.2321 | 15.3015, 16.3296, 16.2321 | reference | 204/9 | 37.14 |
| array-fill | candidate | 15.5373 | 15.2189, 100.4377, 15.5373 | 0.957x | 204/9 | 37.46 |
| array-copy-checksum | baseline | 1.0055 | 0.9803, 1.0055, 1.1163 | reference | 0/0 | 16.52 |
| array-copy-checksum | candidate | 0.4228 | 0.4133, 0.5766, 0.4228 | 0.420x | 0/0 | 16.74 |
| array-loop-copy-checksum | baseline | 0.6246 | 0.6246, 0.6792, 0.5646 | reference | 0/0 | 16.55 |
| array-loop-copy-checksum | candidate | 0.5657 | 0.5657, 0.5634, 0.5976 | 0.906x | 0/0 | 16.72 |
| tree-survival | baseline | 1.5478 | 1.9254, 1.3443, 1.5478 | reference | 20/0 | 36.54 |
| tree-survival | candidate | 1.4155 | 1.4056, 1.4211, 1.4155 | 0.915x | 20/0 | 37.20 |

## primary

| Case | Engine | Median ms | Process medians ms | Candidate/reference | Timed minor/full | Peak RSS MiB |
|---|---|---:|---|---:|---|---:|
| struct-allocation-access | candidate | 6.6094 | 6.7034, 6.4780, 6.6094 | 7.029x | 5172/94 | 37.61 |
| struct-allocation-access | v8 | 0.9403 | 1.0079, 0.9403, 0.9171 | reference | unobserved | 105.25 |
| array-allocation-access | candidate | 14.5494 | 16.0013, 14.1844, 14.5494 | 2.293x | 11776/512 | 38.46 |
| array-allocation-access | v8 | 6.3461 | 6.5136, 6.3461, 5.6535 | reference | unobserved | 105.22 |
| array-fill | candidate | 16.3136 | 16.3136, 16.6802, 16.2875 | 2.283x | 11776/512 | 38.50 |
| array-fill | v8 | 7.1461 | 7.1461, 7.1875, 7.0140 | reference | unobserved | 105.11 |
| array-copy-checksum | candidate | 0.3976 | 0.3721, 0.4118, 0.3976 | 2.292x | 10/0 | 16.74 |
| array-copy-checksum | v8 | 0.1735 | 0.1857, 0.1735, 0.1666 | reference | unobserved | 44.41 |
| array-loop-copy-checksum | candidate | 0.6325 | 0.6401, 0.6325, 0.5677 | 4.352x | 10/0 | 16.81 |
| array-loop-copy-checksum | v8 | 0.1453 | 0.1486, 0.1453, 0.1354 | reference | unobserved | 44.41 |
| tree-survival | candidate | 1.7513 | 1.7513, 1.7526, 1.3560 | 3.044x | 1181/6 | 37.79 |
| tree-survival | v8 | 0.5753 | 0.6191, 0.5753, 0.5547 | reference | unobserved | 72.68 |

## sensitivity

| Case | Engine | Median ms | Process medians ms | Candidate/reference | Timed minor/full | Peak RSS MiB |
|---|---|---:|---|---:|---|---:|
| struct-allocation-access | candidate | 4.0495 | 4.0495, 4.0480, 4.3959 | 4.449x | 1275/23 | 38.61 |
| struct-allocation-access | v8 | 0.9102 | 0.9082, 0.9102, 0.9487 | reference | unobserved | 105.16 |
| array-allocation-access | candidate | 8.0962 | 8.0962, 8.3416, 7.9472 | 1.335x | 2901/125 | 39.27 |
| array-allocation-access | v8 | 6.0631 | 6.0631, 5.9473, 6.1564 | reference | unobserved | 105.23 |
| array-fill | candidate | 9.5930 | 10.6496, 9.5930, 9.2696 | 1.329x | 2901/125 | 39.48 |
| array-fill | v8 | 7.2190 | 7.1588, 7.2743, 7.2190 | reference | unobserved | 105.25 |
| array-copy-checksum | candidate | 0.3784 | 0.3916, 0.3784, 0.3688 | 2.187x | 3/0 | 18.53 |
| array-copy-checksum | v8 | 0.1730 | 0.1698, 0.1941, 0.1730 | reference | unobserved | 44.37 |
| array-loop-copy-checksum | candidate | 0.6256 | 0.6415, 0.6256, 0.6137 | 4.469x | 3/0 | 18.51 |
| array-loop-copy-checksum | v8 | 0.1400 | 0.1400, 0.1379, 0.1472 | reference | unobserved | 44.41 |
| tree-survival | candidate | 1.4131 | 1.6014, 1.1716, 1.4131 | 2.262x | 293/3 | 39.21 |
| tree-survival | v8 | 0.6247 | 0.7256, 0.5705, 0.6247 | reference | unobserved | 72.74 |

## Interpretation

Every full-protocol workload still misses the 1.15x V8 threshold at both policies. The target is not achieved. The same-source A/B demonstrates consistent bulk-copy improvement, with every candidate sample below every baseline sample. Scalar-copy and other controls overlap; fill has a large candidate outlier. Struct is slower in every paired short round, despite unchanged emitted code size and allocation footprint; preserve that control rather than claiming universal gain. No aggregate hides target misses.

## Separate compile footprint

| Case | Variant | Median us/op | Code bytes | Go bytes/op | Go allocations/op |
|---|---|---:|---:|---:|---:|
| struct-allocation-access | baseline | 176.240 | 1775 | 56026 | 232 |
| struct-allocation-access | candidate | 145.290 | 1775 | 56026 | 232 |
| array-allocation-access | baseline | 124.736 | 1967 | 56075 | 223 |
| array-allocation-access | candidate | 133.728 | 1967 | 56075 | 223 |
| array-fill | baseline | 140.945 | 2479 | 59979 | 233 |
| array-fill | candidate | 137.781 | 2479 | 59979 | 233 |
| array-copy-checksum | baseline | 160.819 | 3759 | 71579 | 270 |
| array-copy-checksum | candidate | 175.277 | 4655 | 76764 | 277 |
| array-loop-copy-checksum | baseline | 168.298 | 3759 | 71579 | 270 |
| array-loop-copy-checksum | candidate | 175.951 | 4655 | 76764 | 277 |
| tree-survival | baseline | 133.925 | 1455 | 57339 | 267 |
| tree-survival | candidate | 127.923 | 1455 | 57339 | 267 |

Copy artifacts add 896 native code bytes (+23.8%), 5185 Go bytes/compile (+7.2%) and 7 allocations/compile. Other artifacts have unchanged code size/Go allocations. Compile timing varies in controls, so do not infer a general compiler-speed improvement. This measures Compile+Close, not peak compiler RSS.

## Limits

V8 13.6.233.17-node.58 / Node 24.22.1 uses --no-liftoff. All exact artifact hashes, checksums and Wago allocation counters were verified. V8 internal allocation/elision and collection behavior are unobserved; retained roots do not prove equal optimizer allocation behavior. This is an end-to-end opt-in engine comparison, not isolated collector efficiency. Resident memory is not an equalized heap budget. Direct copied/promoted/scan counters are uninstrumented. Compilation is excluded from execution timings; Go host GC runs outside timing after warmup. CPU windows excluded worker builds/tests, but host scheduling/frequency remain uncontrolled. Raw samples, versions, counters and binary hashes are retained.
