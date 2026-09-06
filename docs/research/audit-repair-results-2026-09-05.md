# Audit repair results

Implementation of the [accepted plan](performance-security-repair-plan-2026-09-05.md).
Baseline: `f0f4951138b8`. Host: Linux/amd64, Go 1.27.1. Initial serial samples use
`GOMAXPROCS=1`, `-cpu=1`, 100 ms, five samples. Raw local captures are in
`/tmp/wago-audit-repair`; final release measurements need the plan's longer A/B
and native-platform runs. The initial Go test-binary build exceeded 30 seconds;
process inspection showed compilation followed by a 12.8-second benchmark suite,
not an individual Wasm compile over 30 seconds.

## Background cancellation watcher

The new inert-context allocation test failed before the fix with 7 allocs/op for
Background, TODO, and WithoutCancel. All now take the shared no-op path before
callback/channel allocation. Nil contexts remain allocation-free. The focused
cancellation, deadline, and invocation-context tests pass. The new benchmark
reports zero bytes and allocations for each inert context. No native polling or
scheduler transition was changed. Native non-Linux and TinyGo tests are pending.

## Structured custom-section budgets

Name and branch-hint readers now share the parent budget. Exact-capacity
structured vectors reserve storage and allocator rounding before allocation;
strings and the owned raw payload are charged separately. Strict section checks
remain in place. A valid 3 MiB module name now decodes with a 16 MiB metadata
budget and rejects with an 8 MiB budget. The former malformed fixture now checks
that the structural parser rejects it rather than codifying a payload multiplier.
The complete wasm package tests pass. Structured-name benchmark captures are in
`decode-budget.txt`; large valid names could not be benchmarked successfully on
the old implementation. Type-slab changes are a separate repair.

## Signature validation and streaming artifacts

Signature validation compares each exact descriptor to its ABI type without
building temporary slices. Indexed artifact signatures write the index directly;
non-indexed signatures stream their values. Public signature access still copies.
The focused signature, codec, artifact, and Compiled tests pass.

Five serial 100 ms samples on the same host give these medians:

| Benchmark | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| WriteCompiledImportedModule | 316910 | 147912 | 263074 | 154207 | 12829 | 21 |
| ValidateFuncSignature | — | 14.47 | — | 0 | — | 0 |

These short sequential samples confirm the allocation reduction; final
interleaved timing and native-platform checks remain pending.

## Linux interrupt generations and ARM64 ordering

Token issuance renews its cookie and resets its sequence only after all requests
have drained. Shared owners retain their token at the generation boundary.
Cookie initialization and renewal share the request lock and atomic publication.
The ARM64 handler acquires published code/memory registry entries and publishes
trap acknowledgement with release ordering. The existing fixed registry sizes
and native call path are unchanged.

Focused interrupt/deadline/lifetime tests pass, including under `-race`.
The Linux/arm64 runtime test binary cross-builds. Native ARM64 execution remains
required; the race detector cannot prove assembly ordering. Signal negotiation
and scan optimization still need their separate measurements.

## Lazy type slabs

Implicit groups allocate slabs only as needed, growing from 16 to at most 1024
subtypes per slab. Explicit groups allocate only their own exact subtype vector.
Each reservation covers actual capacity and rounding. Singletons still expose
capacity one; at most 1023 unused subtype slots can remain in the final slab.
All wasm tests pass, including an explicit-only 1000-group fixture within a
400000-byte quota.

The 100000-singleton control changed from 17605392 B/op and 6 allocations to
17611408 B/op and 108 allocations. The extra 102 allocations are bounded slab
allocations, not one allocation per type. Short samples improved from about
11.3 ms to 10.0 ms; final A/B timing is pending. The explicit-only 100000-group
case uses 18401040 B/op, with no unused 15.2 MB singleton slab. Small one-group
implicit and explicit cases each use 968 B/op and 6 allocations. Captures are in
`type-slabs-after.txt`. The allocation/retention tradeoff is explicit here because
reducing unused storage does not reduce every benchmark's allocation count.

## Concurrent first publication

Cache pointers use atomic first publication; the cache lock serializes memo and
snapshot construction. Readers acquire memo/snapshot pointers, including
reflection sidecars. Raw pointer fields keep Compiled copyable and retain the
existing compiler-owner allocation. A short first-initialization lock publishes only one cache. Repeated race
tests exposed a losing-CAS write racing with established pointer readers; the
lock removes that write. Warm access uses atomic reads without this lock.

A fresh hand-built Compiled now passes concurrent instantiation, invocation,
signature reflection, and CodeSize under `-race`. Focused Compiled/snapshot/code
ownership tests pass. Snapshot-copy and call-control measurements are in
`snapshot-publication.txt`; final timing remains pending.

## Snapshot allocation quota

The snapshot preflight counts destination storage before any deep clone, including
aliased source vectors, nested expressions, name maps, and rounding. Its byte
quota also bounds packed integer counts. Runtime and low-level instantiation
callers can configure MaxCompiledMetadataBytes; zero selects 256 MiB. Artifact
loads retain their decode quota. A smaller caller quota applies to warm snapshots
as well. Quota rejection leaves a cold object available for a larger-budget retry.

Focused artifact, snapshot, Compiled, and ownership tests pass. Ten repeated
first-use/snapshot race runs pass. The scalar preflight has zero allocations.
Tests verify aliased destination charging, rejection before publication, public
mutation isolation, retry, and warm caller policy.

## Bounded regular-file reads

Reads allocate size plus one byte after checking the opened regular file. The
extra byte detects growth; size, path identity, and quota checks remain in place.
All regularfile tests pass. Two serial A/B samples reduced 1 MiB reads from
2229120 B/op and 34 allocations to about 1057900 B/op and 10 allocations;
time fell from 184–188 us to 101–102 us. At 8 MiB the count fell from 39 to 10.
Captures: utility-regularfile-ab.txt. Short timing samples remain provisional.

## Read-only active-state access

Readers consume one atomic record without creating or chmodding a writer lock.
Legacy reads do not migrate state; a later writer publishes the complete record.
The version tests pass, including read-only state and absence of a created lock.
Two A/B samples reduced reads from 5824 B/op, 97 allocations and 22.5–24.0 us
to 3544 B/op, 81 allocations and 11.8–12.0 us. This candidate also includes the
regular-file allocation fix. Capture: utility-version-ab.txt.

## Lock serialization

EncodeLock validates the graph once and checks the final JSON byte/depth/value
limits and typed key rules without allocating a second decoded graph. Project
tests pass. The 1000-plugin A/B fixture fell from 338303 to 327177 allocations,
about 16.67 MB to 13.39 MB per encode, and 34.4–35.0 ms to 28.2–28.6 ms.
Strict JSON field lookup remains a separate repair. Capture: utility-project-ab.txt.

## File-lock retry timers

Contended acquisition reuses one timer, resetting only after consuming its prior
channel event. Uncontended acquisition does not allocate a timer. Package tests
pass on Go 1.27.1 and Go 1.22.12. Go 1.27 removed the old asynctimerchan switch,
so validation used the actual older toolchain after that switch failed loudly.
Ten-retry A/B samples fell from 3744 B/op and 42 allocations to 1264 B/op and
12 allocations. The 105 ms time measures the intentional wait, not throughput.
Capture: utility-filelock-ab.txt.

## One runtime selection per plugin operation

Plugin add/remove/update/config/grant/rebuild and execution capture runtime
version, standard profile, build variant and paths once. Staging receives that
captured compiler config. A regression test changes active state and the build
environment override after capture: both local/global paths and compiler inputs
retain the old selection; the next operation sees the new one.

Plugin and plugin/build package tests pass. Validation exposed an existing
mismatch: identity's go list enabled VCS stamping while the actual generated
build disabled it. The identity query now also uses -buildvcs=false, allowing
the existing generated-runtime fixture to pass on both execution paths.

## Version lifecycle coordination

Install/update, installed selection, and removal use stable per-version locks in
config/version-locks, outside removable payload directories. Lock order is version
then active state. Case variants share a coordinator. Install/update release the
lock before a user prompt; selection reacquires it and checks that the runtime
still exists. Network/build work currently holds the same cancellable operation
lock, favoring coherent publication over concurrent updates of one version.

Version tests pass under -race. New tests hold the coordinator while removal
waits, verify another version remains independent, reject selection after removal,
and verify the coordinator inode survives removal/reinstall. Cancellation while
waiting is covered. Raw payload helper APIs accept arbitrary destinations and
remain caller-coordinated; the version lifecycle commands own this lock.

## Immutable import registry generations

Compilation snapshots share the paired import maps under rt.mu. Registration
forks a published map pair once before inserting already-owned immutable metadata.
Configuration reads no longer clone the whole config for binding metadata.
Consumed or closed PreparedCompile objects release the captured registry maps.

Focused tests verify old/new map-pair isolation, zero allocations for warm binding
snapshots, and preparation release. A scaling fixture covers 0/10/1000/10000
unrelated registrations with setup outside timing. The broader runtime/plugin race run passes (122.8 seconds total, including
subprocess regression suites). Two A/B samples at 10000 unrelated registrations
fall from about 2.47 MB and 20148 allocations to 17216 B/op and 75 allocations.
The candidate allocates the same amount at every tested registry size. Timings
fall from about 2.5 ms to 27–30 us. Final longer timing remains pending; no public ownership boundary is weakened.

## Instance import ownership

WithImports borrows source maps until resolution, which merges options into one
owned map. The private Runtime-to-instantiator path transfers that map; public
low-level calls still clone caller maps. Multiple options preserve last-write
precedence and explicit unused entries. Focused ownership/import tests and the
new override/mutation regression pass, including a focused race run.

The imported numeric-global fixture falls from 2880 B/op and 22 allocations to
2224 B/op and 18 allocations; short A/B times fall from about 3.66 us to 3.10 us.
Capture: imports-ab.txt. Multiple WithImports options need a small source-list
slice, but do not allocate another merged import map.

## Private import metadata

Module import storage is preallocated once. ABI/exact descriptor slices borrow
owned execution-snapshot storage; non-indexed hand-built signatures still convert.
ModuleView borrows the private ImportSpec slice and its public Imports method
copies on access. A nil Runtime config retains the prior default behavior.
Focused metadata, snapshot, ownership, and import tests pass with pinned WABT
1.0.41; the distro 1.0.36 caused visible version-check failures before PATH was
corrected. At 10000 imports, Runtime compile uses about 9.90 MB instead of
20.68 MB per operation. InvokeAddOne remains about 100–102 ns, zero allocations;
PreparedInvokeAddOne remains about 17.7–17.9 ns, zero allocations. Short A/B
samples are in imports-ab.txt; longer confidence checks remain pending.

## Strict JSON field lookup

A bounded FIFO cache holds up to 128 type descriptors and 256 KiB of conservative
storage charges. Oversized descriptors remain transient. Known fields use exact
lookup, then folded fallback; common duplicate IDs use a 64-bit inline bitset.
Wider/unknown fields retain ordinary duplicate-key maps. Case aliases remain
strict even where a Go struct declares two distinct exact spellings. Embedded
field dominance now determines each child's type; maps and RawMessage stay exact.

JSON, project, and version tests pass under -race. New tests cover embedded,
shadowed and ambiguous fields, raw JSON, Unicode aliases, exact type preference,
and cache churn bounds. Two A/B samples for 64 fields fall from 78656 B/op and
6583 allocations to 3888 B/op and 204 allocations, and about 302 us to 6.3 us.
At 256 fields: about 1.10 MB/99997 allocations becomes 29680 B/op/987 allocations;
time falls from about 4.6 ms to 42–46 us. Capture: json-ab.txt.

## Unix lease handoff

The launcher passes one validated descriptor through exec. The current payload
adopts that same open file description and restores close-on-exec before normal
startup. Direct payload launch opens a private close-on-exec lease. Handoff state
is consumed and removed from child environments. Windows keeps its waiting-parent
lease model. Old payload binaries keep their old behavior until updated.

Filelock and managedrelease tests pass under -race. Linux subprocess tests verify
one lease descriptor for launcher and direct payload startup, zero lease descriptors
in an ordinary child, close-on-exec flags, and malformed handoff rejection. Native
Darwin execution remains pending. No extra long-lived goroutine was introduced.

## Recover deferred Windows cleanup

Pending cleanup now records a random operation ID, scheduling phase, PID, and
process creation identity. The worker checks both this record and the publication
coordinator identity before it removes any target. Parent waiting holds one
verified process handle. A dead scheduler, dead worker, reused PID, or old empty
marker permits recovery only after coordinator rotation invalidates old waiters.
Rollback removes only its own operation. Live cleanup remains exclusive.

Managedrelease and filelock race tests pass on Linux, including stale/live records,
PID reuse, rollback isolation, and distinct coordinator identities. Windows amd64
managedrelease and replacement test binaries cross-build. Native Windows worker,
interrupted cleanup, and reinstall execution remain a required release gate.
