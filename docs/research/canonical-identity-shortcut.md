# Canonical GC identity shortcut (2026-10-21)

AMD64 defined casts, tests, and branch casts resolved a valid compact object, then
used subtype intervals for ordinary type checks even when canonical type IDs
were equal. Equality proves the subtype relation. The native stub now accepts
equal IDs before loading either interval. Unequal exact casts still fail;
ordinary unequal types use the original interval check. Null, foreign anyref,
i31, handle, backing, and object-extent checks remain before the shortcut.
No collector metadata or object layout changes.

Validation proves static type use, not a downcast's run-time outcome. This change
reduces a necessary check's cost; it does not remove the downcast. The functional
identity test covers cast and test results across collection with normal and Tiny
collectors. Existing branch, subtype, canonical-alias, foreign-reference, and exact
cast tests continue to cover the shared stub. The functional test passes on the
parent too; performance measurements establish this optimization's effect.

Five alternating 200 ms samples per version on Ryzen 7 8845HS, CPU 15,
`GOMAXPROCS=1`. Both binaries use the new test harness; the baseline overlays only
`gc_direct.go` with the parent version. Instruction loops report per-instruction
metrics; branch benchmarks report a whole 1024-cast invocation.

| Case | Before | After | Change |
| --- | ---: | ---: | ---: |
| Identity `ref.cast` | 3.492 ns/instruction | 2.480 ns/instruction | 1.41x faster |
| Identity `ref.test` | 3.583 ns/instruction | 2.602 ns/instruction | 1.38x faster |
| Proper-subtype `ref.cast` control | 3.493 ns/instruction | 3.622 ns/instruction | 3.69% slower |
| Branch cast, same type | 5.449 µs/invocation | 4.608 µs/invocation | 1.18x faster |
| Branch cast, proper subtype | 5.413 µs/invocation | 5.432 µs/invocation | 0.35% slower |
| Branch cast, mismatch | 5.897 µs/invocation | 5.877 µs/invocation | 0.34% faster |

The unequal-ID path has an extra comparison/branch. The proper-subtype microloop
shows its small cost; branch subtype/mismatch ranges overlap. Raw samples and
medians are in `canonical-identity-results/`.

The real Dewdrop compiler did not show a useful isolated gain: one cold-cache run
was 63.386 seconds against a preceding 63.583-second run. Do not infer a full
compiler speedup from this difference. Additional dynamic uniform array and
cast/field-read changes in other PRs reduce that combined stack to 50.551 seconds.
All outputs match and `main` returns 42. V8 on identical compiler bytes previously
took 3.487 seconds. This is not parity.
