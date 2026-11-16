# Direct constructor initialization candidate

Source `c1d11f27`, parent `4327a2eb`. For bounded direct16 struct constructors,
nondefault initializer stores that cover an entire eight-byte payload word now
replace its otherwise redundant zero store. Partially initialized words,
alignment/tail padding and default constructors keep their existing zero stores.
No publication ordering, admission, safepoint or failure path changes. Compact
constructors are unchanged. The coverage check is compile-time only, bounded by
eight fields and a128-byte object; the existing RAX zero instruction remains.

## Correctness

Go1.27.1 Linux amd64:

- Default AMD64 backend passes (12.183s).
- Default full `src/wago` passes (82.003s) after installing the pinned Release3
  corpus test directory (`9d36019973201a19f9c9ebb0f10828b2fe2374aa`) in the new worktree.
  Initial run failed because this submodule's corpus was absent; log preserved.
- `wago_gc_direct_trusted_alloc wago_gc_direct_trusted_refstruct
  wago_regalloccheck wago_codegenstats`: selected `TestDirectGC|TestTrustedDirect`
  runtime tests pass (1.264s), including new poisoned-nursery mixed32/64/reference
  word/padding/default tests, existing heap-child moving-root retention and
  rooted initializer fallback tests. Applicable backend coverage/loop/mode tests
  pass (0.027s).
- Full runtime suite under those experimental tags FAILS. Most failures are
  default collector instantiations rejected with `native direct-read artifact
  requires direct-offset collector`; other ordinary codegen expectations also
  differ. The original broad run also lacked the corpus. Representative
  admission failures reproduce on unchanged source `f4b670e4` with identical
  tags. This is NOT a claim that all broad failures were individually classified
  or that experimental direct mode passes every GC test. Full raw logs retained.

## Compilation probe

Three alternating fresh-process rounds, each `GOMAXPROCS=1 GC_COMPILE_PROBE=1`
with `-test.run '^$' -test.bench '^BenchmarkGCDirectCompileProbe$'
-test.benchtime=256x -test.count=1`. Intel Xeon Platinum8573C. Baseline is the
previously qualified integrated binary, code source93c7ec34 (code-equivalent to
parent4327a2eb). Candidate rebuilt with EXACT matching tags:

```
wago_gc_direct_trusted_loop,wago_gc_direct_trusted_refstruct,wago_gc_direct_trusted_alloc,wago_gc_direct_fill_sse2,wago_gc_direct_copy,wago_gc_direct_array_alloc,wago_gc_direct_weighted_sum
```

| case | generated code bytes, both | candidate/baseline median compile time |
|---|---:|---:|
| struct allocation |1775|0.960|
| array allocation |1967|1.234|
| array fill |2543|1.027|
| bulk copy |4719|1.109|
| loop copy |4719|0.835|
| tree |1455|1.397|

All six cases have identical generated code size AND compilation allocation
bytes/count. Unchanged array controls show substantial timing variation, so these
three short rounds do not establish a compile-time improvement or regression.
Single narrow-field constructors still need zeroing for the uncovered half-word;
code alignment can also hide instruction differences. This corpus establishes
no code-size benefit. A wider/mixed-field runtime workload must show a material
benefit before promoting this candidate as a performance optimization. No new
Wasm execution timing or V8 speedup was measured.

## Narrow-field padding refinement

Source `d62c7ca2` widens a four-byte initializer store to eight bytes only when
its field starts at an eight-byte boundary and no other field overlaps the upper
four bytes. The preceding Load32 zero-extends RAX, so the wider store initializes
the field and zero padding together. The planned-store coverage check then omits
the redundant initial word-zero store. Default constructors, higher-half fields,
neighboring scalar/reference fields and wholly padded words retain correct zero
initialization. No additional allocation or runtime proof/safepoint is added.
New tests include lone scalar/reference words and scalar-plus-reference neighbors.

Fresh qualification on Go1.27.1: default AMD64 backendPASS12.142s; full default
WagoPASS72.502s; selected instrumented direct/trusted runtimePASS1.109s; applicable
backendPASS0.032s. Existing broad tagged admission failures remain unresolved;
that known failing broad invocation was not repeated for this refinement.

Three fresh alternating compilation-probe rounds again produce IDENTICAL code
bytes and compilation allocation bytes/count for all six cases. Median compile
time ratios range0.238–1.137, including large changes in unchanged array controls;
this noisy timing is not interpretable as an optimization benefit. Aligned total
code size is not an instruction-count measurement. Raw results retained under
`refined-evidence`. No refined execution speedup has yet been measured.

Two code-source-identical binaries were built: `candidate.test` uses the seven
baseline tags above; `constructor-adaptive.test` adds ONLY
`wago_gc_retire_adaptive`. No sparse mark cleanup is included. Binary SHA256s are
retained to support the subsequent separate2×2 source/policy execution experiment.
