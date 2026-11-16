# Partial native metadata qualification

Four of six artifacts pass exact same-build emitted/loaded byte parity,
collector geometry/accounting, 128 checked calls per mode, actual collections,
source/site/region bounds, inline ancestry and complete load/retire lifetimes.
No samples or performance timings were collected.

| Case | Native bytes | Mapped bytes | Unmapped bytes | Result |
|---|---:|---:|---:|---|
| struct-allocation-access | 1775 | 1176 | 599 | PASS |
| array-allocation-access | 1967 | 1367 | 600 | PASS |
| array-fill | 2543 | 1863 | 680 | PASS |
| array-copy-checksum | unknown | unknown | unknown | FAIL exact emitted parity |
| array-loop-copy-checksum | unknown | unknown | unknown | FAIL exact emitted parity |
| tree-survival | 1455 | 1137 | 318 | PASS |

The two copy artifacts failed before invocation. Do not apply source-map-mode
profiles to the baseline code for those workloads. Source inspection shows
several optimizations explicitly decline when RecordSources is true, including
`gc_loop_resolver.go`, `gc_array_reduction.go`, and `gc_array_fill_reduction.go`.
This is a plausible reason, not yet a measured byte-level attribution of either
failure. No production optimization or profiling guard was changed.

The first attempted build failed because the test addressed Module.code rather
than Module.c.code. The compiler error is retained; the corrected probe built
successfully. Runtime test failure is preserved separately and not described as
an all-six pass.

The exported image journals belong to these completed processes and cannot
symbolize another process's addresses. They contain static lowering provenance,
not sampled hotness. Struct's 66.3% byte coverage does not mean 66.3% CPU coverage.
Profiling-toggle parity in one profiling-enabled binary is not cross-build
parity against a binary without wago_profile. No V8 inference is supported.
