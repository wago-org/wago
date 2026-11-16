# Direct16 heap verification

Base: `d747cdcb`. Implementation: `2d7f5954`, tightened header flags in
`69d11e62`; tests: `8b3e78c9` and `69d11e62`.

`directgc.Collector.Verify` is a serialized, read-only representation diagnostic.
It validates arena geometry and occupied ranges, immutable descriptors, exact
header extents and flags, all start bits, every occupied object's reference
edges, roots, and completeness/consistency of the remembered set. It checks
unreachable occupied objects too. Temporary maps and descriptor copies are
private; collector scratch, roots, accounting, and heap bytes are not modified,
including on error. This diagnostic allocates Go memory and is not benchmarked.

Null and i31 references are valid. An object edge must point to an exact occupied
object start, not an interior or inactive-space address. Old-to-young edges need
a dirty parent and either a remembered queue entry or overflow coverage.
Conservative dirty parents remain legal. This is not a Wasm subtype/nullability
validator. Offsets lack ownership/generation tags, so reused stale offsets cannot
be distinguished from references to a new object at the same address.

The native adapter delegates explicit Verify and now admits VerifyAfterCollect
for direct16 only. Post-collection verification runs after successful relocation,
root publication, and statistics updates. A diagnostic error does not roll back
that completed collection. Compact verification remains explicitly unsupported.
No import/table/external-reference/platform admission was widened. Poison and
barrier stress modes remain rejected. Verification disables native allocation
fast paths using the existing guard, but checked allocation still uses the same
direct16 collector, not the handle collector. Benchmark configurations leave it
off.

## Qualification

Go 1.27.1, patched TinyGo 0.42.0, Linux amd64. These are correctness runs, not
performance measurements. Commands use writable shared toolchain/module/build
caches. Logs are sanitized to omit machine-local toolchain paths.

- `go test ./experiments/directgc ./src/core/runtime/gc/native`: PASS, final source.
- `go test -race ./experiments/directgc ./src/core/runtime/gc/native`: PASS, final source.
- `tinygo test -scheduler=tasks ./experiments/directgc`: PASS, final source; CORE only.
- `go test -tags=wago_gc_retire_adaptive ./experiments/directgc`: PASS before final flag-only tightening.
- `go test -tags=wago_gc_compact_helpers ./src/core/runtime/gc/native -run '^$'`: compile PASS; no compact verifier claim.
- `go test ./src/wago -run TestDirect`: PASS before final flag-only tightening.

Tests cover pre/post minor/full collection, cycles, aliases, null/i31, invalid
roots, malformed headers/descriptors/array extents, missing/interior/inactive
start bits, missing barriers, remembered overflow, large-flag consistency, and
failed collection followed by verification and successful retry. Corruption
tests assert no mutation of heap, start/dirty maps, mark/queue scratch or state.
Adapter tests assert direct selection, preserved payloads, relocation, explicit
verification, VerifyAfterCollect, and native-allocation diagnostic gating.

An initial tagged Wago compile was killed while correctness compilers overlapped.
The log is retained; this is not relabeled a test pass. Serial retry uses
`GOMAXPROCS=2 GOGC=50 go test -p=1` with tags
`wago_gc_direct_trusted,wago_gc_direct_trusted_alloc,wago_gc_direct_trusted_store,wago_gc_direct_trusted_arrays,wago_gc_direct_array_alloc,wago_gc_direct_trusted_refstruct,wago_gc_direct_copy`
and `./src/wago -run '^(TestDirectGC|TestTrustedDirect)'`.

Serial tagged retry: **PASS**, final source, 0.921s test execution. Original
killed compilation remains in `evidence/wago-tagged.log`; successful retry is
`evidence/wago-tagged-retry.log`. No full WasmGC corpus/admission claim follows
from this selected direct-runtime regression suite.

## Integrated checkpoint

At integrated source `013aabcc`, direct core and native packages passed (0.030s,
2.962s). The initial combined command was interrupted during its full Wago stage;
that stage is not recorded as passing. A separate serial rerun of
`go test -p=1 ./src/wago` completed successfully (56.284s), using Go 1.27.1,
`GOMAXPROCS=2`, and `GOGC=50`. Logs are in `evidence/integrated`.
These are default-runtime compatibility checks, not full experimental direct16
WasmGC corpus qualification or performance measurements.
