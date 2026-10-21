# One native check for an abstract array cast and length read

## Measured cause

The saved Dewdrop compiler profile records 20,913,295 Go `ref.cast` helper
calls. Every one targets the abstract array heap type. All 214 static abstract
array cast sites in that compiler are followed immediately by `array.len`.
Those broad-reference casts still need a dynamic type check. The length read
does not need to repeat object resolution after that check.

Astra reviewed this change before the branch was made. The read-only Go
helper already preserves native allocation reservations. Removing its Go
transition is the intended saving; avoiding batch cancellation is not claimed.

## Change and proof

The AMD64 compiler matches only an adjacent abstract-array cast and length
read. One shared, noncollecting native leaf validates the complete reference,
handle index, live space, backing and object extent, header size, and actual
type-table index. It checks array kind before reading the length from Aux.
No raw object address escapes, and no allocation or collection can intervene.
Null and invalid cases retain the original cast and length helpers, preserving
the distinction between cast failure and a nullable cast followed by null
dereference. Pending earlier traps are still evaluated first.

The existing eight-byte DFS interval entry carries kind without another table:
entry ticks are `2*t + isArray`, and exit ticks are `2*t`. For distinct ticks,
this preserves their strict order, so all old interval containment comparisons
remain valid. A bound on type count prevents the doubled counter from wrapping.
Object, handle, and native-view sizes do not grow.

The former padding word at native-view offset 20 records encoding 1. The leaf
requires that value before reading the kind bit. Legacy ABI-9 runtimes have
encoding zero and take the Go path. The interval representation remains usable
by old native code, which only compares timestamps. Native ABI version 9 and
the 200-byte view size remain unchanged. Lean TinyGo retains its original path.

## Checks

- Compare interval containment for every pair in a mixed struct/array forest
  against an independent parent walk; also check appended subtype entries.
- Check native-view offsets, exact view size, and legacy/unknown encoding.
- Assert code generation combines nullable and nonnullable cast/length pairs.
- Execute scalar, vector, and reference arrays at lengths 0, 1, 64, and 4096.
- Repeat across collection, forced collection on allocation, and Tiny profiles.
- Keep 24 integer locals live across the native and legacy Go paths.
- Check null, struct, i31, foreign references, and earlier divide traps.
- Replace packed timestamps with actual legacy timestamps to catch a missing
  encoding guard: an odd struct entry tick must not be mistaken for array kind.
- Keep a live struct and native allocation reservation across length reads.
  This is an integration control and also passes without the optimization.

The interval regression fails on the original builder: the first struct has an
odd unencoded entry timestamp. The checked focused tests pass in 18.908 seconds
including compilation. The full normal suite passes in 162.677 seconds.
The full checked run passes all compiler, collector, and API packages; one
plugin-install test fails with `text file busy`, then its complete package
passes on rerun in 18.301 seconds. The checked benchmark module passes in
34.934 seconds. Documentation validation passes.

The ARM64 subtype and native-view tests pass under `qemu-aarch64`. Saved native
code built with this change runs on the prior ABI-9 runtime, and code built by
the prior runtime runs on the new runtime; both return the expected length and
live-local sum. This uses the same artifact codec on both sides. The independent
origin/main review branch uses an older artifact codec than the combined stack,
so cross-codec rejection is not treated as an ABI compatibility test.

## Small benchmarks

Each operation executes 8,192 iterations. Five alternating 200 ms samples,
CPU 15, GOMAXPROCS=1. The stronger control uses the full installed GC stack,
which already has a native general `array.len`. Both sides use the same new
runtime metadata; only the new cast/length lowering differs.

| Loop | Installed control median | New path median | Ratio |
| --- | ---: | ---: | ---: |
| Cast and length | 686,615 ns | 33,652 ns | 20.40x |
| Allocation, cast and length | 762,479 ns | 104,588 ns | 7.29x |

The earlier 39.36x loop result uses a branch without native general length
reads. It is not the improvement over the installed stack. Neither ratio is
a full-compiler speed claim.

```sh
go test ./src/wago -run '^$' -bench '^BenchmarkGCAbstractArrayCastLen$' -benchtime=200ms -count=5
```

## Full compiler comparison

Four fresh-process runs use the installed GC stack as control and that same
stack plus this change as candidate. Run order is control/candidate/candidate/
control. Both use CPU affinity 12,14 and GOMAXPROCS=2. Native caches are fresh;
transform-result caches miss. No other task-owned build or test runs alongside
this measurement. The host is shared, so this is a bounded observation.

| Run | Wall seconds |
| --- | ---: |
| Control 1 | 49.893 |
| Candidate 1 | 47.190 |
| Candidate 2 | 47.832 |
| Control 2 | 49.134 |
| Control median | 49.514 |
| Candidate median | 47.511 |

The median drops 4.04%. Both candidate samples are below both controls. All
four output Wasm/cache hashes match, Wasm validation passes, and output `main`
returns 42. This does not establish V8 parity; the same request previously
took a median 3.676 seconds in V8 without this affinity restriction.

Compiler SHA-256:
`c64fcf044b72cb7356ad598a35670045931337cfa7a6bb9d5916cd304b23fba6`.
Request SHA-256:
`077b0ff0271706cc3034f3e06c7a228f227c49aeec7786d21373b2072af89c72`.
Output Wasm SHA-256:
`89af055df4b2a4e44be109f10f50d98b5e29deb4810194aca234c9e019912fbb`.

An earlier uncontrolled candidate run took 55.738 seconds while machine load
changed and other compilation work occurred. It was not used for the speedup
calculation. The full compiler is an external Dewdrop workload, not a Wago
fixture or a result synthesized from the small benchmark.

Raw paired timings and benchmark samples are in
[the saved results](abstract-array-cast-length-results/summary.json).

## Disabled shared-stub mode

The follow-up safety review of PR980 exposed the same configuration gap in
this adjacent cast/length path. `WAGO_AMD64_NO_GC_SHARED_STUBS=1` must disable
this module-shared leaf too. The matcher now checks the existing switch before
consuming either opcode, so disabled mode uses the original checked helpers.
Default-mode emitted code is unchanged.

The codegen test covers both cast nullabilities with the switch enabled and
disabled, and requires zero shared stubs/calls in disabled mode. It failed on
the prior code (one fused path while disabled). Normal and checked focused
backend/collector/public-runtime tests pass in 15.986/15.529 seconds. The
runtime tests also cover the independent-root type-growth repair above.

## Malformed metadata fallback safety

A fresh regression corrupts an array handle's space byte. The native abstract
array leaf rejects space5, but its original Go cast fallback then called
refDesc/header and panicked while slicing an empty object span. The first
baseline test also recorded the existing space0 invalid-object diagnostic;
the second baseline records the actual space5 panic and stack. Both failures
remain in the results directory.

refDesc now selects a supported physical space, requires a complete header,
and checks the full offset+size extent with uint64 arithmetic before reading
the type ID. Invalid metadata returns the existing invalid-object-ref error.
This preserves closed/null/i31/foreign and descriptor/type checks. It affects
ordinary Go access and GC scans as well as the native fallback; no cold-only
cost claim or speed claim is made.

Tests cover all252 invalid space bytes, short/empty headers, oversized spans,
offset overflow, nonzero adjacent metadata bytes, fresh and saved native code,
nursery, an explicitly moved survivor, old, large and Tiny heaps. They restore
all metadata before collection and teardown. Read-only review confirms storage
selection matches bytes() and owner/generation checks stay outside refDesc.
Final focused checked native/API tests pass in11.317 seconds. The full checks
for this safety fix and the separately bounded byte-load follow-up follow.


## Read the handle space byte directly

The abstract array leaf now reads the existing space byte with MOVZX. It
replaces a 32-bit load, a shift and a mask. The space test, both space branches,
span and type checks, and fallback register restores remain unchanged. This
saves two instructions and nine bytes at this decoder. It does not change the
metadata layout or remove a check.

A complete-module code test covers both cast forms. It requires the exact
six-byte load and rejects the old fifteen-byte decoder. It failed before the
change in 5.173 seconds and passes with the focused statistics/checker tests
in 14.842 seconds. The runtime metadata tests pass for both frozen benchmark
binaries.

The isolated benchmark uses two compiled test binaries above the same safety
commit. Only this emitter file differs. CPU15, GOMAXPROCS=1; process order is
baseline/candidate/candidate/baseline, three 200ms samples per case/process.
Each operation is one Invoke of 8,192 guest iterations, including the host
call. The cast/length median falls from33,634.5 to32,630.5 ns/Invoke (2.99%).
The allocating case changes from103,465.5 to103,369.5 ns/Invoke (0.09%); its
late baseline outliers169,553 and170,043 remain in the data. No useful speed
improvement is claimed for that allocating case or the full compiler here.

Required full checks for the metadata safety fix and byte decoder pass:
normal root126.263 seconds, checked root126.021 seconds, checked bench31.766
seconds. The root runs leave GOWORK unset so Go selects this worktree's
workspace and lets standalone tests select their generated child modules.
Earlier root runs with an inherited explicit GOWORK path failed only when
those standalone child modules were outside that workspace (90.771/92.507
seconds); those failed results remain saved. An earlier malformed -p2 flag
setup attempt is also retained locally. Use `go test -p 2 ./...`, not `-p2`.

Raw byte benchmarks, exact source/binary hashes, regression results, and final
full-check results are in [the saved results](abstract-array-cast-length-results/byte-summary.json).
