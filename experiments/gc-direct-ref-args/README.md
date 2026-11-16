# Same-instance direct16 managed arguments

Base `647a5098`; production `89b4b74e`, narrowed by `16cf029c`.
This is coverage work, not a performance change or a full WasmGC admission claim.

The added exported parameter categories are indexed struct/array references
(nullable or non-null) and immediate i31 references. Abstract any/eq/struct/array,
extern/function/exception references remain outside this new admission. Compact
admission is unchanged. Existing imports, tables, element segments, plugins,
custom instructions, platform and exact compiler-receipt restrictions remain.
Non-null object arguments must use live public tokens issued by the receiving
instance. Raw offsets, wrong types, stale tokens, foreign runtime tokens and
other-instance tokens are rejected. Default handle-collector staging is unchanged.

## Entry rooting argument

1. `api.go` and `prepared_function.go` acquire the invocation/collector lease;
   boundary collection precedes public argument marshalling.
2. `reference_store.go:stageGCRefArgument` authenticates the token and exact type,
   reads its CURRENT checked global root slot rather than its issuance snapshot,
   and installs a classified argument root. Root-slot publication does not run
   Wasm collection. This candidate additionally rejects a different token owner
   for offset collectors. Serialization covers staging through native entry.
3. `gc_frame_roots_linux_amd64.go` adds function parameters to tracked collector
   locals before declared locals, then computes liveness. Existing admission
   requires the generic exact-root compiler receipt.
4. The AMD64 prologue loads parameter values into their register/frame homes
   before body execution. Allocating helper calls materialize tracked locals and
   spill pinned registers before the trampoline. Direct fast allocation cannot
   collect. Its fallback does the same materialization and reloads pinned locals
   from rewritten homes afterward (`call.go`, `localstate.go`).
5. Deferred argument-root cleanup runs on success, marshalling errors and traps.
   Prepared reference calls are not admitted to scalar-only invocation shortcuts.

## Tests

The fixture disables dead allocation elimination and inlining. It checks an
increase in full collections and a changed authoritative token-root address
DURING a callee read, as well as payload correctness. It exercises an internal
callee, typed/raw/prepared invocation, duplicate arguments, cyclic graph edges,
array arguments, null acceptance/non-null rejection, i31, distinct indexed struct
types, invalid/raw/released/foreign tokens, partial marshalling failure, traps,
cancellation, OOM and successful retry. Checks assert argument-root cleanup,
verified direct heap state and actual direct collector selection.

Qualification logs and final results are recorded separately below. No timing
measurements are performed. An intermediate test fixture lacked its newly added
function type declaration and failed validation; that raw failure is preserved,
and the declaration is fixed in `af849162`.

## Qualification details

Go 1.27.1, Linux amd64. Serial commands use `GOMAXPROCS=2 GOGC=50`, `go test
-p=1`, writable shared caches, and `-count=1`. No measurements of speed or
compilation cost were made.

- `wago_gc_direct_trusted`, explicit forced relocation fixture: PASS (`final-boundary.log`).
- Direct-only race, `-tags=wago_gc_direct_trusted -run '^TestDirectGCManagedReference'`:
  PASS for both forced and allocation-pressure fixtures, final source `88c73bbe`.
- Default mode, `-run '^(TestGCToken|TestGCPublic|TestDirectGCManaged)'`: PASS.
  Managed direct fixtures skip on default handle artifacts; existing public token
  tests execute normally and verify preservation of default behavior.
- Initial seven-tag selected direct regression: PASS at `613347db`; final row
  additionally includes the ordinary allocation-pressure fixture.

An overly broad initial race selection also included
`TestGCTokenBoundariesUseAuthoritativeRoot`, which constructs a handle collector.
It was rejected under direct-read tags with `native direct-read artifact requires
direct-offset collector`. This is retained in `race.log` and `status.txt`, not
claimed as a passing row. The corrected direct-only race row passes; the legacy
fixture passes in the default row. No compiler receipt was bypassed to make it
pass.

Final seven-tag configuration:
`wago_gc_direct_trusted,wago_gc_direct_trusted_alloc,wago_gc_direct_trusted_store,wago_gc_direct_trusted_arrays,wago_gc_direct_array_alloc,wago_gc_direct_trusted_refstruct,wago_gc_direct_copy`.
Run pattern: `^(TestDirectGC|TestTrustedDirect)`.
Helper-only direct configuration: `wago_gc_helper_only`, run pattern
`^TestDirectGCManagedReference`.

Final normal seven-tag direct regression: **PASS**, 0.771s test execution.
Final helper-only direct boundary and allocation-pressure fixtures: **PASS**.
Final race, final seven-tag and helper-only rows all use `88c73bbe` source.
Independent source review found no lifetime blocker and requested stronger
movement/type tests, now included. Final source approval is collected by the
coordinator before integration. This remains limited same-instance ingress,
not full direct WasmGC, TinyGo runtime or cross-instance reference support.

Integrated source `ef4427bd` also passes the complete default `./src/wago` test
package (47.129s) with the same serial Go 1.27.1 settings. See
`evidence/integrated-default.log`. This is compatibility coverage, not a full
experimental direct16 admission claim or a benchmark result.
