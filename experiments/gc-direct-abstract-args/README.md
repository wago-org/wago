# Abstract managed-reference arguments for direct16

Base `09fb97cc`. Explicit allowlist expansion `a995dcb6`; conversion-path guard
`e9092a4b`. Final test source `83aa477f`.

Adds only any, eq, struct, array and none reference parameters to the existing
indexed struct/array and i31 admission. Both nullable and non-null signatures are
checked. Nullable none accepts only null; non-null none accepts no public value.
Exact abstract types remain excluded. Object arguments still require authenticated
same-instance public GC tokens. Nullability and dynamic family checks happen
before native entry. No import/table/element/extern/function/exception/string,
custom-instruction, plugin, platform or compact admission is added.

## Root and type proof

`collectorFrameRefType` explicitly classifies all five new families as collector
roots. Parameters enter the same param-first liveness plan and helper spill/reload
protocol qualified by the indexed-argument experiment. No new root representation
or calling convention is introduced. `gcRefMatchesValueType` allows i31 only for
any/eq/i31, checks nullability, and delegates object family checks to the direct
collector's RefTest. The same-instance token guard and authoritative checked-root
lookup remain in force.

Independent review identified an alternate AMD64 extern-conversion token path:
`hasOpaqueExternFrames` is not a sufficient AMD64 exclusion, and the specialized
extern product can bypass ordinary `stageGCRefArgument`. Direct16 now explicitly
rejects every product requiring extern-conversion state before admission. This
also conservatively excludes specialized abstract cast/test products requiring
that state until separately proved safe. A regression fixture has no extern
public signature, import or table, but performs an internal extern/any roundtrip;
it must hit the dedicated conversion-product rejection. No attempt is made to
make the conversion identity machinery offset-compatible.

## Tests and scope

The matrix exercises all five families, nullable/non-null, null/i31/struct/array/
raw inputs, wrong-family rejection and foreign-owner tokens. Accepted object
cases assert collection count growth AND an authoritative root-address change
during the callee, then verify the returned token points to that rewritten object
and preserves payload. Dead-allocation elimination is disabled. Mixed any+eq
aliases test two live parameters and two returned references. Heap verification
and argument-root cleanup are checked. Existing indexed boundary tests retain
coverage for cancellation, traps, OOM retry, typed/prepared calls and ordinary
allocation-pressure collection.

These are bounded API/rooting coverage changes, not a performance improvement or
full direct16 WasmGC support. No benchmarks are run. No TinyGo runtime support is
claimed. Final independent review is required before integration.

## Qualification

Go 1.27.1, Linux amd64; serial `GOMAXPROCS=2 GOGC=50 go test -p=1`, count=1,
writable shared caches. Initial and guarded trusted-direct matrices pass; final
matrix results and exact test selections are recorded with the evidence below.

Final source `83aa477f` qualification: all rows PASS.

- Seven-tag direct regression, `^(TestDirectGC|TestTrustedDirect|TestDirectAbstract)`:
  0.731s test execution. Tags:
  `wago_gc_direct_trusted,wago_gc_direct_trusted_alloc,wago_gc_direct_trusted_store,wago_gc_direct_trusted_arrays,wago_gc_direct_array_alloc,wago_gc_direct_trusted_refstruct,wago_gc_direct_copy`.
- Race, `wago_gc_direct_trusted`,
  `^TestDirect(GCAbstract|GCMixed|GCRejectsInternalExtern|AbstractArgument)`:
  1.110s test execution.
- Helper-only direct, `wago_gc_helper_only`, same pattern: 0.020s.
- Default mode, `^(TestGCToken|TestGCPublic|TestDirectGCRejectsInternalExtern|TestDirectAbstract)`:
  PASS; preserves the original token behavior and tests policy/guard rejection.

All logs and command exit statuses are retained under `evidence/`. Durations are
correctness-test execution reports, not comparative runtime measurements.
