# Direct collector ordinary imported memory

Bounded API coverage experiment from `663a27f2`, not a performance change.

Direct16 admits one imported memory at index zero, with a non-threaded memory32
module declaration and known live ordinary backing. Both declaration and backing
are checked: `NewSharedMemory` is rejected even if supplied to an ordinary import.
Ownership sharing of an exported ordinary memory is allowed; it is not the same
as Wasm thread sharing. Unknown, closed, memory64, threaded and multiple-memory
import shapes remain excluded. Compact admission and function/global/table import
bans are unchanged. Existing fresh compiler receipts and exact-root Runtime
requirements remain mandatory. No handle-collector fallback is introduced.

Memory contains bytes, not managed references. The ordinary AMD64 growth path
changes size caches in a stable reservation without entering a collector. Shared
basedata is rebound under existing construction/native-entry leases, including
the instance-specific direct GC view. Memory import attachments retain physical
producer ownership until consumers close. Close/type/limits checks are repeated
by the ordinary attachment path after the early GC admission check.

Tests cover scalar/array arguments moved by actual callee collection after grow
zero/success/failure, prepared calls, grown-byte visibility, different collectors
and types alternating on the same backing, exact context restoration on data
initialization failure, recovery after a trapping start, host Close ordering,
producer logical closure and final physical release, and rejected backing/shapes.

Initial fixture errors are retained in evidence: a wrong prepared-method name,
and releasing a token from an Invoke result slice after a later invocation had
reused that slice. Tests now retain scalar token values and check release errors.
Those fixture failures are not treated as passing tests or production defects.

No runtime TinyGo or all-WasmGC qualification is claimed. No benchmark is run.

A second fixture disables forced collection and verification-on-collection,
keeps native allocation enabled, alternates tenants and checks ordinary nursery
pressure eventually collects while preserving the argument. This establishes
behavior with native allocation enabled, not a measured fast-path hit count.
Its deferred runtime cleanup is not the ownership-leak oracle; the dedicated
explicit-close lifetime fixtures provide that assertion.

Independent source review approved production/tests `fa61a880` and the additional
pressure fixture `22e5aafb`. No production change was needed after review.

## Qualification

Final source `22e5aafb`, Go 1.27.1 Linux amd64, serial
`GOMAXPROCS=2 GOGC=50 go test -p=1`, count=1. All final rows PASS:

- Race, trusted-direct: memory fixtures and import-policy matrix, 1.108s.
- Seven-tag direct/trusted regression (`TestDirectGC`, `TestTrustedDirect`,
  `TestDirectMemoryImportPolicy`): 1.085s, including pressure fixture.
- Helper-only direct: memory fixtures and import-policy matrix, 0.012s.
- Default memory/shared-host-memory/token/public-GC/policy regression: 0.364s.

Exact commands, logs and exit statuses are under `evidence/`. Reported durations
are correctness-test execution times, not performance comparisons. Earlier
`corrected.log` is the pre-pressure targeted normal pass at `fa61a880`; final
normal qualification including pressure is the seven-tag regression above.
