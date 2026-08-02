# Wasmtime core corpus exclusions

This file is generated from `UPSTREAM_INVENTORY.tsv`; edit that ledger, not this document. At Wasmtime revision `a5720e50d5ec9eab34eed690eee952abfdd0e3ba`, all 324 `.wast` files under the pinned source root are classified: 104 ported, 4 excluded portable cases, and 216 out-of-scope proposal/component cases.

## Excluded portable core cases

| Upstream file | Reason |
|---|---|
| `big-memory-behavior.wast` | classic memory exceeds Wago 65535-page executable ceiling |
| `canonicalize-nan-scalar.wast` | Wasmtime-specific NaN canonicalization engine option |
| `simd/canonicalize-nan.wast` | Wasmtime-specific SIMD NaN canonicalization engine option |
| `wast-syntax.wast` | Wasmtime-specific WAST module definition and instance syntax |

## Out-of-scope categories

Every individual path is recorded in `UPSTREAM_INVENTORY.tsv`. These categories are separate feature corpora rather than silent exclusions.

| Reason | Files |
|---|---:|
| GC proposal is not supported | 79 |
| component model is outside the core runtime | 79 |
| component model threading is outside the core runtime | 9 |
| custom page sizes proposal is not supported | 4 |
| exception handling and GC proposals are not supported | 1 |
| exception handling proposal is not supported | 3 |
| memory64 proposal is not supported | 11 |
| multi-memory and memory64 proposal matrix is not supported | 2 |
| multi-memory proposal is not supported | 2 |
| multi-memory, memory64, and custom-page-size proposal matrix is not supported | 1 |
| multi-memory, memory64, threads, and custom-page-size proposals are not supported | 1 |
| shared-everything threads proposal is not supported | 1 |
| stack switching and GC proposals are not supported | 1 |
| table64 and memory64 proposals are not supported | 1 |
| tail calls proposal is not supported | 1 |
| threads and atomic memory operations are not supported | 1 |
| threads and atomics proposal is not supported | 12 |
| typed function references proposal is not supported | 6 |
| wide arithmetic proposal is not supported | 1 |
