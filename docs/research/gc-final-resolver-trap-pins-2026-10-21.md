# Restore global pins before final-cast resolver traps

## Reproduced fault

The AMD64 final-cast struct-field resolver saves caller-owned R9, R10 and
R11 before using them as scratch. Its failure branches previously entered
the enclosing function's trap tail without restoring those registers.
That tail writes dirty global value pins to their cells. A failed cast
therefore replaced those globals with resolver scratch values, including
native handle-table and object addresses.

The regression fixture has eight hot mutable integer globals and no
parameters or locals. Each of two functions changes all eight globals,
then performs a final struct cast and field read. The test checks the
emitted resolver save sequence to require actual R9/R10/R11 global pins.
It tests nonnullable and nullable casts with a valid object, an object of
the wrong type, and null, both before and after artifact codec reload.

Before the fix, every wrong-type case corrupted globals 4, 5 and 6; globals
4 and 6 received native addresses. Every null case changed global 5 to
zero. Valid reads passed. The original focused run failed as expected in
13.416 seconds, including compilation.

## Bounded fix

Only `emitNativeFinalCastStructRefResolverStub` changes. Its cast failures
join one cold block and its nullable-null failure joins another. Each
block restores exactly the registers saved at entry, in reverse order,
before entering the existing function trap tail. This removes the leaf's
saved-register stack entries before global writeback and trap unwinding.

The blocks follow the successful return. The successful instruction
sequence, resolver ABI, type checks, object checks and result loads remain
the same; failure branch destinations change. Nonnullable null still traps
as a cast failure, while nullable null followed by a field read still traps
as a null reference. No module-wide resolver or getter inlining is added.

This branch starts at PR 948 commit `049cb7e40`. Before work, remote PR 948
still pointed there; PR 979 was `28a72d351` and PR 980 was `d0f8afb0b`.

## Validation

The runtime regression passes in 12.496 seconds including compilation.
Checked code generation for all field widths and the runtime regression
pass in 17.458 seconds, with `wago_codegenstats,wago_regalloccheck` enabled.
The runtime checks also cover success and preserve the original trap codes.
The complete AMD64 compiler package passes in 7.278 seconds. The broader
checked final-cast, reference-read, and defined-cast runtime tests pass in
0.488 seconds. Both times include compilation.

```sh
go test ./src/wago -run '^TestGCFinalResolverTrapPreservesGlobalPins$' -count=1
go test -tags wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 ./src/wago -run 'GCFinalCastFieldRead|GCFinalResolverTrapPreservesGlobalPins' -count=1
go test ./src/core/compiler/backend/railshot/amd64 -count=1
go test -tags wago_codegenstats,wago_regalloccheck ./src/wago -run '^TestGC(FinalCast|FinalResolver|ReferenceRead|NativeDefinedCast)' -count=1
```

This is a correctness fix. No speed claim is made.

### Complete-suite checks on the review branch

The complete ordinary and checked repository suites were run in 133.714 and
120.439 seconds. All backend and native-collector packages passed. Initial
runtime conformance failures were setup errors in this fresh worktree: missing
pinned corpus checkout, then an unset official interpreter. PowerShell also
needed the host's no-ICU setting. After initializing all three pinned corpora,
set:

```sh
export DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1
export WAGO_SPEC_INTERPRETER=/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm
export WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa
```

Complete public-runtime package reruns pass ordinary/checked in
21.816/22.770 seconds, including all staged conformance tests. The root package
passes ordinary/checked with the PowerShell setting in 1.923/6.026 seconds.
The checked plugin-install package had a transient ETXTBSY error; its complete
rerun passes in 21.224 seconds. The checked benchmark-module suite passes in
34.915 seconds. Thus every package has a passing run; the initial full-command
failures remain recorded and are not described as clean full-command passes.

With the complete environment restored, both full repository commands also
pass cleanly: ordinary 35.574 seconds, checked 34.777 seconds. No source change
was needed for the earlier setup failures. `go run ./tests/tools/docs-check`
passes after the final research update.
