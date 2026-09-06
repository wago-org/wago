# Audit repair results

Implementation of the [accepted plan](performance-security-repair-plan-2026-09-05.md).
Baseline: `f0f4951138b8`; scheduler comparison: main `447f057115ee`.
The repairs are committed on `fix/september-audit`; the final source repair is
`d261e75c1`. No push or merge was done.
The user's unrelated `AGENTS.md` edit is preserved and is not part of the commits.

Implementation covers all seven blockers, both lower security findings, all nine
performance priorities, and the checked-GC documentation issue. Qualification is
not complete: native Windows recovery/PowerShell and Linux ARM64 signal ordering
need native hosts; the full Go 1.27.1 raw-call benchmark has an unresolved timing
outlier. Two Wine installer failures, 25 staticcheck diagnostics, and a decoder
fuzz error-phase mismatch also occur on the audited baseline. The external SQLi
benchmark fixture is absent. Do not treat this branch as fully qualified to merge.

The sections below retain the initial measurements and later verification.
Final A/B samples use prebuilt binaries, alternate order, run serially with
`GOMAXPROCS=1` and `-cpu=1`, and compare matching rows. Small controls use ten
200 ms samples per side; large corpus stages use ten single-iteration samples.
Exceptions are labeled. Host: Linux/amd64, AMD Ryzen 7 8845HS, kernel
6.12.101+deb13, Go 1.27.1. Correctness gates also use CI-pinned Go 1.22.12.
CPU affinity is 0–15 with host-default power/thermal settings; a raw-call diagnostic
also pinned CPU 2. No test/build jobs ran concurrently with timed A/B processes.
GOGC and GOMEMLIMIT use defaults except labeled memory runs. Bounds are explicit
unless labeled guard-page. Raw captures, scripts, environment, fixture hashes,
and profiles are local under `/tmp/wago-audit-repair`, not checked into the repo.
The initial cold Go test-binary build exceeded 30 seconds; measured individual
Wasm compilations stayed below 1.3 seconds in the selected final corpus.

## Audit-to-commit map

IDs M/S/P/D follow the order of the original audit. B numbers refer to the
[accepted benchmark inventory](performance-security-repair-plan-2026-09-05.md).

| Audit item | Repair commits | Main proof / benchmark group |
|---|---|---|
| M1 token exhaustion; S1 ARM64 ordering | `5479ff1bb` | Idle rollover/shared ownership tests; B2; native ARM64 still required |
| M2 structured custom budgets | `5b277e23d` | Valid 3 MiB names, exact quotas, malformed rejection; B3 |
| M3 first publication | `044e78384` | Fresh hand-built concurrent first-use race test; B5 |
| M3 pre-clone aggregate quota / codec ownership | `71b065c7f`, `d261e75c1` | Aliased destination charge, cold retry, warm quota, no-allocation preflight; B5 |
| M4 stale Windows uninstall | `db7cc716f`, `3e9315365` | Stale/live worker and coordinator tests, Wine supplement; B7/native gate |
| M5 version lifecycle race | `2feba6552` | Removal/selection serialization, cancellation, stable lock identity; B6 |
| M6 read-only active state | `d5c58a033` | Read-only and legacy tests, read syscall trace; B6/B8 |
| M7 mixed plugin selection | `3f0be121e` | Captured version/profile/build tests, plugin/build suites; B6 |
| S2 inherited release leases | `4bf88d665` | One payload lease, zero ordinary-child leases, invalid handoff tests; B7/B8 |
| P1 signature allocations | `586b8758f` | Signature/codec ownership tests; B9 |
| P2 registry copies | `ddb3996d1`, `85a745ecd` | Generation isolation/release and real public registration; B10 |
| P3 unused singleton slab | `4d5fc3e61`, `9c73dcd48` | Explicit-only quota, mixed and implicit tradeoff; B4 |
| P4 duplicate import maps | `42ef69220`, `fc9005d54` | Override/mutation and public ownership tests; B11 |
| P5 private metadata copies | `9ffb68334` | Snapshot/reflection/ownership tests; B10/B11 |
| P6 JSON reflection | `e1fe56450`, `a0a7c21e7`, `cb0b46452` | Strict field semantics, bounded churn, race tests; B12 |
| P7 lock reparse | `842d8da74` | Lock graph and strict token tests; B13 |
| P8 inert cancellation watcher | `7f0fc3519` | Background/TODO/WithoutCancel allocation test; B1 |
| P9a checked-GC scratch; D1 telemetry API | `036cef4eb` | Reentry/panic/close/outlier and telemetry tests; B14/B15 |
| P9b bounded reads | `98332ee81` | Growth/identity/limit tests; B16 |
| P9c cleanup ancestors | `136b7dfa1`, `3e9315365` | Protected aliases/replacement tests, depth controls; B16 |
| P9d lock timer | `0265ef283` | Go 1.22/current cancellation and retry tests; B16 |
| P9e signal negotiation/scans | `9354cd0b8` | Recheck actual signal action; bounded registry scan retained on measurements; B2 |
| P9f small import index | `3aa120a63` | Exact component/collision tests around four-row boundary; B11 |
| Call refinement / measurement | `ec99d6cd4`, `f58e021db`, `ed9a99e69` | Trap reset race tests, bounded cancellation tails, checked raw-call fixture; B1/B2 |
| Superseded helpers | `52f5cc70c` | No new staticcheck diagnostics |

## Background cancellation watcher

The new inert-context allocation test failed before the fix with 7 allocs/op for
Background, TODO, and WithoutCancel. All now take the shared no-op path before
callback/channel allocation. Nil contexts remain allocation-free. The focused
cancellation, deadline, and invocation-context tests pass. The new benchmark
reports zero bytes and allocations for each inert context. No native polling or
scheduler transition was changed. Pinned TinyGo tests later pass; native non-Linux execution remains unrun.

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

These initial samples confirm the allocation reduction. Final interleaved
measurements are recorded below; native-platform execution remains unrun.

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
and scan measurements are recorded in their section below.

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
11.3 ms to 10.0 ms; final A/B timing is recorded below. The explicit-only 100000-group
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
`snapshot-publication.txt`; final public and snapshot controls are recorded below.

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
fall from about 2.5 ms to 27–30 us. Final ten-sample timing is recorded below;
public ownership boundaries are preserved.

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
samples are in imports-ab.txt; final public call and import controls are recorded
below. The 10000-module-import row itself remains a short allocation scout.

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

## Checked collector scratch and telemetry

Each checked collector retains one idle scratch record, with at most 256 entries
in each root/input vector. Reentrant callbacks use separate records. Owner and
generation checks run after callbacks. Release clears references and drops large
outlier buffers; close drops the idle record. Nil-root collection keeps its direct
path. Checked telemetry aliases and guarded snapshot/reset methods now match the
documented API, with normal and `wago_gcstats` tests.

Checked GC race and telemetry-tag tests pass. Warm 1/16/256-root collection now
uses zero bytes and allocations, versus 6/10/14 allocations before. Throughput
256-root collection falls from about 4185 to 2061 ns; Tiny from 6183 to 3836 ns.
Four-value construction falls from 184 B/5 allocations to zero, and about 157 to
88 ns (Throughput). A 4096-root outlier drops from 370752 B/20 allocations to
318656 B/13 allocations, without a claimed timing win or retained large buffer.
Nil-root times do not regress in the short samples. Capture: gc-bounded-ab.txt.
The first constructor fixture exhausted its heap because it supplied nil roots;
it failed visibly and was corrected to explicit EmptyRoots on both sides.

## Cleanup ancestor reuse

Reinstall cleanup captures configured and resolved protected ancestry once, then
compares file identities while traversing. It still checks protected path identity
at each removal boundary and does not traverse directory links. Existing nested,
alias, and reinstall tests pass. Identity comparisons can grow with depth; the
repeated filesystem ancestry walks are removed.

At depth 128, BenchmarkReinstallCleanup falls from about 371 ms, 52.07 MB and
281501 allocations to 4.10 ms, 480942 B and 2987 allocations. Depth 1 falls from
168 to 124 us and 412 to 305 allocations. Capture: cleanup-ab.txt.

## Linux signal negotiation

An idle handler installation first rechecks the previously usable signal action.
An unchanged action avoids a full signal scan; a changed action uses the original
negotiation path. No signal-handler scan or restoration rule is relaxed. The
isolated ownership test now changes the idle signal action and checks restoration.
Interrupt race tests pass. Negotiation falls from about 5902 to 652 ns with zero
allocations. Empty MapCode/Unmap falls from 12.7 to 7.2 us; at 2048 registered code
spans it remains about 11 us. Capture: signal-ab.txt. Keep the bounded registry
scans: these lifecycle measurements do not justify more assembly complexity.

## Import option regression coverage

The option-helper test now checks the resolved owned import map, including later
option precedence, instead of requiring options to merge their private storage
immediately. The full-suite run exposed this stale implementation assertion; it
was not suppressed. Public behavior and the ownership regression remain checked.

## Registration cost and final call controls

Registry scaling and unrelated-import instantiation fixtures now register through
LoadPlugins and the granted HostImports API. A separate BenchmarkRegisterImports
measures batch setup/close. Ten alternating 200 ms samples at GOMAXPROCS=1 show
10000-registration importless compile falling from median 2.158 ms, 2472221 B and
20148 allocations to 27.198 us, 17216 B and 75 allocations. Candidate allocation
counts are constant at all four registry sizes. Batch registration itself remains
about 9 ms and 100236 allocations at 10000 imports; no per-compile saving is hidden
in timed setup. Small registry/runtime objects gain about 16 fixed bytes.

The same run keeps InvokeAddOne at 97.26 ns and PreparedInvokeAddOne at 17.66 ns,
both zero allocations. Direct host calls remain 553.9 ns, 112 B and one allocation.
Small compile is +1.0% and small instantiate +0.3% in medians, with unchanged
allocation counts. Imported numeric-global instantiation is 2675 ns, 2224 B and
18 allocations, versus 2879.5 ns, 2880 B and 22. Streaming artifact write is
126952 ns, 154208 B and 21 allocations, versus 270380.5 ns, 263078 B and 12829.
These are per-row medians, not statistical equivalence claims. Raw captures:
final-wago-a.txt and final-wago-b.txt.

## Small import identity lookup

For up to four imports, reuse the immutable ImportSpec rows for identity checks
and lookup. No extra Module field or sidecar is needed. Larger dotted namespaces
retain the map. Separator-aware comparisons preserve empty components and reject
ambiguous flattened identities on both sides of the threshold.

The exhaustive component comparison and collision tests pass. Ten alternating
samples show one-row index construction falling from 118.7 ns, 480 B and three
allocations to 7.17 ns and zero allocations; four rows fall from 247.35 ns, 528 B
and six allocations to 54.89 ns and zero. Eight-row and larger allocation counts
stay unchanged. Captures: final-index-a.txt and final-index-b.txt. The baseline is
the pre-index repair state, so this isolates the small-table change.

## Fold each JSON key once

Descriptor lookup returns its canonical key for wide and unknown-field duplicate
tracking, avoiding a second Unicode fold. Common exact field IDs need no folded
string. Exact-subtree behavior remains covered by the existing strict JSON tests.
The race suite passes. Ten-sample final captures retain 3888 B/204 allocations at
64 fields and 29680 B/987 allocations at 256 fields, around 6.1 and 41 us.
Captures: final-json-a.txt and final-json-b.txt.

## Windows identity snapshot follow-up

Wine testing exposed lazy Windows FileInfo identity loading: comparing an old
Stat result only after path replacement can read the new identity. Cleanup now
forces identity capture at snapshot time on Windows. Unix keeps the prior Stat
path. A protected-root replacement regression passes on Linux and Wine. The stale
coordinator regression also freezes its original ID before rotation; all pending
cleanup/recovery tests then pass under Wine. This supplements native Windows
qualification; it does not replace the PowerShell worker gate.

## Lint fixture correction

The ambiguous embedded JSON-field fixture uses a dynamic outer type so go vet
can still reject accidental duplicate tags in ordinary source. A padding field
avoids reflect.StructOf's unsupported direct-interface embedded layout. The
strict semantic assertion is unchanged; Go 1.22 and current race tests pass.

## Remove superseded private helpers

Static analysis found old private wrappers left unused by the budget, signature,
JSON descriptor, and captured plugin-selection paths. Remove those wrappers.
The remaining staticcheck diagnostics are checked against the audited baseline,
rather than disabled or hidden. These removals do not change public APIs.

## Host release gates and existing failures

With Go 1.22.12, WABT 1.0.41, and the pinned Core 3 interpreter plus its revision
environment variable, the root package suites pass except two Wine installer
tests. Both failures also occur at f0f4951138b8: Wine lacks the CMD download path,
and its Go 1.22 directory enumeration reports `Invalid function`. No test was
weakened or skipped to hide these failures. Runtime-tag CLI packages pass when
using pinned TinyGo 0.41.1; installed TinyGo 0.42.0 showed a duplicate-symbol link
failure before the pinned version was used. Release asset and qualification
script tests pass.

Concurrency, guard-page, corpus (explicit and guard), semantic corpus, spec 1/2/3,
and spec3-signals gates pass. TinyGo build and runtime/public API tests pass with
0.41.1 and Go 1.22.12. Current-Go race suites pass for all changed runtime, decoder,
GC, ownership, manager, and utility packages; the full wago race package takes
267.5 seconds, including many process-isolated cases and race-runtime exit waits.

Formatting, generation, vet, docs, and website-generator checks pass. Staticcheck
still reports 25 existing baseline diagnostics; after removing superseded helpers,
there are no new diagnostics. Captures: gate-*.txt, staticcheck-{baseline,final}.txt,
final-race.txt, baseline-wine.txt. Final source static analysis is also recorded
in staticcheck-owner.txt and compared by diagnostic text against the baseline.

A ten-second codec fuzz run passes 69954 cases. The decoder differential fuzz
harness stops on an existing error-phase mismatch: AST reports validation and the
byte-backed path reports decode; both reject the input. The exact generated case
also fails on the audited baseline. Keep this gap visible; it is not an admission
success or a new acceptance regression. Captures: fuzz-decoder.txt,
fuzz-decoder-baseline.txt, decoder-phase-case, fuzz-codec.txt. The generated failing
case is preserved with those local artifacts rather than added to the repo corpus.

## Cancellation tail fixture

BenchmarkInterruptCancelLatency reports p50/p95/p99 cancel-to-return latency
separately from the 100 us scheduled work interval. Its sample window is capped
at 1024 entries. Every call must return context.Canceled. Ten alternating runs
of 100 calls per side pass; the normal benchmark allocation columns include
context/timer setup and invocation cleanup. No unbounded measurement buffer is
introduced. Captures: final-cancel-{a,b}.txt and final-cancel-benchstat.txt.

## Avoid a redundant trap-cell write

A clear trap cell needs no compare-and-swap from zero to zero. An acquire load
and early return preserve a concurrent interruption because they never overwrite
it. Nonzero traps retain the compare-and-swap retry, and Interrupted is preserved.
The auxiliary trap payload is still cleared as before. The added concurrent-reset
test covers both zero and nonzero starting states under the race detector.

Ten alternating 200 ms samples reduce the direct core boundary control from
22.79 ns to 21.46 ns (-5.84%), with zero bytes and allocations. Public Invoke,
PreparedInvoke, and direct host-call controls show no detected timing change and
retain their allocation counts. After this change, normal, guard-page, gcstats,
spec3-signals, TinyGo, vet, docs, and formatting gates pass. Focused interrupt,
trap-reset, cancellation, and deadline race tests pass. Captures:
final-corerefined-{a,b}.txt, final-callsrefined-{a,b}.txt, refined-gates-status.txt.
The scheduler transition remains required and unchanged.

## Measure the type-slab tradeoff

BenchmarkDecodeMixedTypeGroups measures one implicit group followed by explicit
groups. Ten paired 200 ms samples at 1000 groups fall from 101.38 us and 332.9 KiB
to 98.10 us and 183.5 KiB (-44.88% bytes). At 100000 groups, 14.97 ms and
32.05 MiB become 12.97 ms and 17.55 MiB (-45.24% bytes).

The all-implicit 1000-group control costs more: 73.44 to 78.74 us (+7.22%,
p=0.007), 181008 to 187024 B, and six to eleven allocations. Keep the 16-entry
initial slab because reserving 1024 entries to recover this row would restore
unused storage on sparse mixed input. The 100000 implicit control improves from
11.77 to 10.27 ms, with 6016 extra bytes and 102 extra bounded slab allocations.
At 100000 explicit groups, 16.13 ms and 33605392 B become 14.12 ms and 18401040 B.
Single-group cases retain 968 B and six allocations. These results justify a
bounded memory tradeoff; they do not claim every decoder row is faster. Captures:
final-mixed-{a,b}.txt and final-types-{a,b}.txt.

## Raw-call benchmark checks and unresolved full-binary timing

The raw setup now checks decode, validation, export lookup, engine/memory/code
setup, and every call error. Failed setup releases resources already acquired.
The call-overhead row warms and verifies fib(1) == 1, excludes setup from timing,
and verifies the final result. The identical fixture was used on both revisions.

| Toolchain / linked benchmark | Baseline ns/op | Candidate ns/op | Change |
|---|---:|---:|---:|
| Go 1.27.1, final full bench binary | 25.25 | 36.53 | +44.67% |
| Go 1.22.12, final full bench binary | 30.60 | 28.43 | -7.09% |
| Go 1.27.1, three-file diagnostic binary | 25.22 | 22.70 | -10.01% |

All rows use ten alternating 200 ms samples, check the same result, and allocate
zero bytes. Both full-binary rows were repeated after the last loader refinement;
the three-file diagnostic predates it. The three-file binary is built with `go test -c bench_test.go
backend.go backend_amd64.go` in `bench`; it narrows the linked test program without
changing this fixture or the runtime source. The full Go 1.27.1 slowdown also
persists in a CPU-pinned run. It is not explained by additional JIT instructions:
both generated 105-byte guest bodies have SHA-256
`23884eaa206f055bd7862d2c03f2f0bbfeac39eadf7c02c80000c44af58e00ea`.
A 64-byte trampoline-alignment experiment did not recover the timing and was
removed. CPU profiles do not establish the cause; hardware counters are not
available under this host's perf permissions. The narrower binary and Go 1.22
results suggest linked-binary sensitivity, but do not prove its mechanism.

Keep the full Go 1.27.1 row as an unresolved performance qualification item.
Do not mask it by replacing its selector with the smaller binary. Other public
call and corpus execution controls are reported separately. Captures:
final-raw-owner-{a,b}.txt, final-go122-raw-{a,b}.txt,
checked-raw-{a,b}.txt, go122-raw-{a,b}.txt, isolated-raw-{a,b}.txt,
raw-call-pinned-{a,b}.txt, raw-{a,b}.cpu, and perf-access.txt.

## Whole-runtime and memory controls

The final corpus stage sweep covers tiny, fib_iter, json-as, sqlite3, ruby, and
esbuild where the manifest supports each stage. Ten one-iteration compile-full
medians are ruby 1.113 to 1.141 s (p=0.052), esbuild 718 to 727 ms (p=0.218),
sqlite3 101.7 to 101.4 ms, and json-as 1.866 to 1.869 ms. The largest observed
single compile is below 1.3 s. Ten 200 ms small/corpus hot controls find no timing
difference for tiny/fib execution and json-as serialization/deserialization.
Captures: final-corpus-{a,b}.txt and final-corpus-hot-{a,b}.txt.

Backend small/control allocation counts stay at 22/21; their timings show no
detected difference. GC-layout legacy/precomputed compile time increases by
1.25%/4.09% in this run, with identical 1.688 MiB, 2082 allocations, and
236.8 KiB generated code. These small timing costs remain visible; the native
backend implementation was not changed by this repair stack. Tagged native
telemetry is 24.86 to 25.20 ns disabled (no detected difference), and 832.2 to
854.8 ns enabled (+2.72%), both zero allocation. Native GC constructor/persistent
root sweeps in plain and stats builds are three single-iteration scouts, not
confidence-level timing claims. Captures: final-backend-*, final-telemetry-*,
final-nativegc-*, final-nativegcstats-*.

Worker controls run at GOMAXPROCS 2 and 4: validation and compilation of tiny,
json-as, and sqlite3, json-as multi-module throughput, and independent/process
execution of tiny.add and json-as exports. Three one-iteration samples check
execution and allocation shape; they do not establish steady-state parallel
throughput. An initially over-restricted selector missed parallel execution;
corrected selectors produce rows in corrected-parallel-* and
corrected-throughput-*. The other rows are in workers-{2,4}-{a,b}.txt.

Cancellation uses ten runs of 100 calls. Median per-run p50/p95/p99
cancel-to-return values are 20.09/41.04/52.25 us before and
20.93/42.39/52.16 us after. Both use 36 Go allocations for the full
context/timer/invocation lifecycle. Tail uncertainty is large (occasional
millisecond p99 samples), so this does not prove a tight tail-latency bound.

Thirty alternating process samples use temporary managed installations and
cold runtime caches. These are wall-clock medians in milliseconds:

| Operation | Baseline | Candidate |
|---|---:|---:|
| Managed launcher --version | 4.406 | 4.395 |
| Direct payload --version | 2.668 | 2.598 |
| Active-version query | 4.249 | 4.154 |
| Cold fib startup twin | 7.522 | 7.467 |
| Cold sha256 startup twin | 3.210 | 3.206 |
| Cold json-as startup twin | 4.870 | 4.966 |

Commands must exit successfully; manager output is checked, and corpus suites
supply guest result oracles. These samples include guest work and do not claim
work-minus-noop startup deltas. Captures: startup-samples.json/startup-summary.txt.
The active-query syscall trace shows no candidate writer-lock creation or chmod;
the baseline opens/creates active-installation.lock. Linux handoff tests count
one lease in the payload and zero inherited leases in an ordinary child.

Stripped CGO_ENABLED=0 product sizes, verified after the final loader refinement:
manager 9662624 to 9695392 B (+0.34%); runtime-standard 9277600 to 9314464 B
(+0.40%); runtime-minimal 8945824 to 8978592 B (+0.37%). Build flags are
`-buildvcs=false -ldflags '-s -w -X main.version=audit'`, with `wago_runtime`
and additionally `wago_minimal` for the runtime products.

At GOMEMLIMIT=64MiB, five standalone-process median peak RSS samples in KiB are
fib 15424 to 15660, sha256 15452 to 15664, json-as 17576 to 17776. At 128MiB,
they are 15448 to 13612, 15448 to 15664, and 17584 to 17796. Process-to-process
noise is visible; this does not establish a memory decrease for fib. GOMEMLIMIT
limits Go heap work, not process or native memory. RSS uses a small local
fork/exec/wait4 diagnostic launcher so the Python orchestrator's own RSS does not
set an inherited measurement floor. The discarded Python-only measurements had
that floor and are not used. This launcher does not add cgo to wago.

A full-rate heap profile of one 10000-registration compile/close lifecycle has
60.80 KiB baseline and 60.35 KiB candidate post-GC in-use heap. Baseline registry
snapshot copies account for 2397.45 KiB cumulative allocation traffic; the
candidate removes that path. This single profile is not a long-running RSS
plateau proof. Deterministic churn/release tests separately enforce the JSON cache
bound (128 descriptors/256 KiB charge), checked-GC idle scratch bound (256 entries
per vector with outlier drop), registry generation release, and native mapping
lifetime. Captures: registry-{a,b}.pprof, rss-kib.json, measurement-environment.json.

## Benchmark coverage and limits

The accepted inventory is larger than a single confidence sweep. The table
records actual coverage; proposed combinations are not silently counted as run.
Use the package columns in the plan with `-run '^$' -bench '<selector>' -benchmem
-benchtime=200ms -count=10 -cpu=1`. For A/B work, prebuild both sides and alternate
one process at a time instead of running each whole side consecutively. Confirm
that output includes every requested row. Use `-v` when checking optional data.

| Inventory | Actual fixture/selector coverage and limits |
|---|---|
| B1 | CrossBoundaryCall, HostCall, LinearMemoryAccess; InvokeAddOne, PreparedInvokeAddOne, InvokeHostFuncDirect, both InvokeCrossInstance paths, CallerResolverInvocationContext, CancellationWatch. Context variants also have correctness/race tests. No full direct/host/cancellable context benchmark cross-product. |
| B2 | InterruptRequestLifecycle; InterruptSignalNegotiation; ExecutableRegistryScaling at 0/16/256/2048; InterruptCancelLatency. Capacity/holes/churn/ownership in tests. Concurrent deadline tail matrix and native ARM64 timing unrun. |
| B3 | DecodeStructuredCustom at 1 KiB/64 KiB/3 MiB; quota/malformed tests and full decoder/corpus/spec tests. Baseline valid 3 MiB fails admission and is reported separately. No timing claim for the proposed 8 MiB/dense nested matrix. |
| B4 | DecodeSingletonTypes, DecodeTypeGroups (implicit/explicit), DecodeMixedTypeGroups at 1000/100000. Budget/storage bounds checked in tests. Explicit multi-type width matrix not separately timed. |
| B5 | CompiledSnapshotCopy, CompiledSnapshotPreflight, FirstUseCompiledImportedModule read/unmarshal, scalar compile/instantiate; hand-built parallel cold use under race. Preflight has no baseline counterpart. |
| B6 | ActiveInstallationRead plus active query startup/syscall trace; version and captured plugin tuple tests. Mutation/build contention has deterministic tests, not a latency distribution benchmark. |
| B7 | Release selection/publication/retention/recovery tests, Linux descriptor-count subprocesses, managed startup. Native Windows cleanup phases and recovery latency unmeasured. |
| B8 | Six process operations in the startup table, 30 pairs; no work-minus-noop claim. |
| B9 | ValidateFuncSignature; Write/Marshal/Read/UnmarshalCompiledImportedModule; small scalar and structural-reference codec controls; codec fuzz. Uses the existing 1600-import artifact, not every proposed signature-count/descriptor combination. |
| B10 | CompileRegistryScaling at 0/10/1000/10000 registrations, RegisterImports batch, CompileImportMetadata allocation scout, immutable generation/race tests. Registry sizes and module import sizes are not a full cross-product. |
| B11 | Public/private ownership tests, final import-kind instantiation controls, ImportIdentityIndex around the four-row cutoff; public metadata mutation tests. Hook/reflection combinations are correctness-tested, not a complete timing matrix. |
| B12 | ValidateTypedJSON 4/16/64/256 fields, strict embedded/Unicode/alias tests, bounded descriptor-churn/race tests. Cold/parallel/churn throughput is not separately claimed. |
| B13 | EncodeLock 0/1/10/100/1000 plugins (short A/B), strict graph/token/round-trip tests. No separate DecodeLock or atomic-publication timing claim. |
| B14 | CheckedCollectionRoots 0/1/16/256/4096, CheckedConstructors, Tiny/Throughput, reentry/panic/close/outlier tests. Warm common cases are zero-allocation; large outliers intentionally allocate transient storage. |
| B15 | Plain/stats ArrayConstructors and TinyStepPersistentRoots scouts, ten-pair CollectorTelemetryOverhead; frame-root and collection correctness suites. Full native GC pause/deferred-work distribution matrix unrun. |
| B16 | ReadRegularFile 0/4096/65536/1 MiB/8 MiB; FileLockAcquire 0/1/10 retries; ReinstallCleanup depths 1/16/64/128; correctness suites and active-read syscall trace. Filesystem timing samples are short, and complete syscall-count matrices are unrun. |

The optional `BenchmarkSqliBenign` explicitly skips because its external
`WAGO_SQLI_WASM` fixture is absent. Its PASS exit does not count as workload
coverage. The manifest corpus itself is present and its hashes are recorded.
Native Linux ARM64 (including the small-device 64/128 MiB process-budget check),
Darwin amd64/arm64, and Windows amd64/arm64 execution remain external gates.
Cross-builds of production packages and runtime/GC/wago/release test binaries pass
on all those targets; runtime cross-builds were repeated after the trap change
and wago cross-builds after the loader refinement.

## Artifact decoder owner follow-up

The expanded codec sweep exposed a small-artifact regression: about 1 KiB extra
Go allocation and a 5.69% time increase. A full-rate heap profile and compiler
escape analysis traced the extra storage to a second decoder staging value after
atomic cache publication, plus a temporary memo used only to carry the quota.
Return one staging owner through decode/validation, and install the source quota
in the final owner's memo before freezing its snapshot. No publication or quota
check is removed. The regression test checks default and explicit artifact quota
propagation. It compiles an explicit-bounds artifact even in guard test builds,
because signal-based artifacts intentionally cannot be serialized.

Ten final alternating 200 ms samples against f0f4951138b8:

| Operation | Before us | After us | Before B/op | After B/op | Before allocs | After allocs |
|---|---:|---:|---:|---:|---:|---:|
| Unmarshal small scalar | 12.09 | 12.01 | 4852 | 4823 | 50 | 46 |
| Unmarshal structural references | 16.25 | 15.96 | 7896 | 7864 | 80 | 78 |
| Read 1600-import artifact | 739.6 | 670.7 | 360301 | 305898 | 9656 | 3252 |
| Unmarshal 1600-import artifact | 737.1 | 674.2 | 360350 | 305946 | 9657 | 3253 |

The small scalar and structural timing differences are not significant. Large
artifact reads improve 9.32% and unmarshal improves 8.54%. Imported first-use
read/unmarshal also improve about 8.8%, with 3252/3253 allocations.
Captures: final-codec-owner-{a,b}.txt, small-codec-{a,b}.pprof, codec-escape.txt.
The earlier final-codec-extra and final-codec-refined captures retain the failed
intermediate performance checks; they are superseded by final-codec-owner.

Normal, guard, gcstats, and TinyGo public API tests pass after the loader change;
focused artifact/snapshot/ownership race tests pass. The first guard run caught
the fixture's default signal-bounds choice, which was corrected explicitly and
the full guard package rerun passed. Vet, formatting, and docs checks pass.
All five non-host wago test targets cross-build. The TinyGo LTO link exceeded
30 seconds; process inspection identified ld.lld, while its actual runtime and
public tests completed in 0.04 s and 1.91 s. Captures: codec-owner-{tests,race}.txt,
final-owner-gates-status.txt, final-owner-guard-fixed.txt, final-owner-tinygo.txt.
A final ten-second `FuzzCompiledCodecGeneratedValidModules` run passes 80887
cases with two workers; capture: final-owner-fuzz-codec.txt.

## Final public-call and import controls

Ten alternating 200 ms pairs retain zero allocations for cross-instance direct
and indirect calls (109.1 to 106.8 ns and 111.2 to 110.9 ns). Resolver invocation
context keeps its existing 208 B/two allocations. Snapshot copy stays at 1552 B,
11 allocations, about 540 ns. The candidate scalar snapshot preflight is 41.08 ns
and zero allocation; there is no matching baseline implementation.

Imported function/externref global instantiation loses four allocations per
operation. Imported memory reexport falls from 2.172 to 1.972 us, 2720 to 2064 B,
and 15 to 11 allocations. Imported table setup falls from 3.465 to 3.275 us,
2824 to 2168 B, and 21 to 17 allocations. Unrelated-import instantiation stays at
1552 B and 11 allocations across registry sizes; its 10000-namespace timing is
+1.37%. The unchanged externref control is +1.75% in this run with equal bytes and
allocations. These small increases are retained in the results. Captures:
final-public-extra-*, final-imports-extra-*.

Marshal small scalar falls from 1.263 to 1.127 us, 560 to 496 B, and 14 to six
allocations. Marshal imported falls from 416.7 to 266.8 us and about 12828 to 20
allocations. Structural-reference marshal has 16 allocations instead of 20;
its +0.88% time change is below the planned investigation threshold. Captures:
final-codec-extra-*. Read/unmarshal results from that intermediate capture are
superseded by the final owner table above.

## Remaining qualification actions

Run native Linux ARM64 interrupt/signal tests and the low-end memory check;
run native Darwin and Windows runtime/release suites, including the Windows
PowerShell worker, interruption/reinstall, and descriptor lifecycle tests. The
cross-build and Wine results here supplement those gates only.

Investigate the final Go 1.27.1 full-binary raw-call row on a host with hardware
performance counters. It still exceeds the 3% call-time gate, despite the stable
public controls and improved Go 1.22 result. No cause or correction is claimed.
The all-implicit 1000-type allocation/time tradeoff is explicitly justified by the
mixed-input storage measurements; it is not an unnoticed regression.

The commands that remain red on this host have diagnostic records:

- `GOTOOLCHAIN=go1.22.12 make test`: the root Wine tests
  `TestWineCmdBootstrapDownloadsVerifiesAndExecutesInstaller` and
  `TestWineInstallerCompletesNativeInstallFlow` fail on both revisions.
- `make lint`: staticcheck retains 25 baseline diagnostics; formatting, generated
  files, vet, website-generator, and docs checks pass individually.
- `go test ./src/core/compiler/wasm -run '^$' -fuzz '^FuzzDecodeValidateByteBackedDifferentialGenerated$' -fuzztime=10s`: stops on
  the same baseline error-phase mismatch. Both decoders reject the case.
- In `bench`, `go test -run '^$' -bench '^BenchmarkSqliBenign$' -benchtime=1x -v`:
  skips for the missing external fixture. Supply the project's fixture through
  `WAGO_SQLI_WASM` before claiming that workload's coverage.

These failures and gaps were not weakened, hidden, or turned into passing rows.
