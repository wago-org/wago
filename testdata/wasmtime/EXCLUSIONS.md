# Wasmtime core corpus exclusions

The portable WebAssembly 1.0/2.0 `tests/misc_testsuite` inventory at Wasmtime
revision `a5720e50d5ec9eab34eed690eee952abfdd0e3ba` contains 106 applicable source
files. `MANIFEST.tsv` ports 102 of them and additionally carries the two supported
post-2.0 branch-hinting regressions. The remaining four core files cannot
preserve their upstream oracle without adding a different engine feature or
changing Wago's runtime representation:

| Upstream file | Reason |
|---|---|
| `big-memory-behavior.wast` | Requires growing a classic memory from 65,535 to 65,536 pages. Wago's classic-memory native context stores the current byte length in a 32-bit cache, so the documented executable ceiling is 65,535 pages. The first three assertions pass; the three 65,536-page assertions do not. |
| `canonicalize-nan-scalar.wast` | Tests Wasmtime/Cranelift's optional NaN-canonicalization engine setting. Wago preserves ordinary WebAssembly NaN behavior and has no corresponding compiler configuration. |
| `simd/canonicalize-nan.wast` | SIMD counterpart of the engine-specific NaN-canonicalization test above. |
| `wast-syntax.wast` | Tests Wasmtime's custom WAST extensions, including `(module definition ...)` and `(module instance)`, rather than WebAssembly module/runtime semantics. |

Backend disassembly goldens, the component model, threads, and post-2.0 proposal
tests are separate corpora, not silent exclusions from this ledger. They require
a distinct applicability pass against the feature surface of the branch they are
ported onto.
