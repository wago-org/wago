# September 2026 audit fixes

Base: `c46f2129e` (updated origin/main). Findings refer to the audit of
`01c7971d61`. Each numbered finding has one fix commit, combining its regression
coverage and implementation so each commit is independently reviewable.

## 1. Own authenticated artifact input

`UnmarshalBinary` now uses the streaming loader's owned RW code image and owned
metadata buffer. It rejects trailing input before publishing receiver state.
The input may be reused after the method returns. First instantiation seals the
same owned image, without a second native-code copy.

Validation: `go test ./src/wago -run
'Test(UnmarshalOwnsArtifactBytes|Compiled(ReadFrom|Sections)|MarshalRoundTrips)'
checks buffer reuse, section framing, and the owned-image path.

## 3. Snapshot low-level import bindings

Instantiation makes one shallow copy before validation. Binding, re-export
lookup, and resource teardown retain that same copy. Mutation or deletion in
caller maps after return cannot change an instance's bindings. Resource objects
remain shared by identity. Cost: one map copy per non-nil import set; no call-path
allocation is added.

Validation: import snapshot, function re-export, and table-close tests pass.

## 4. Use canonical global storage types

Numeric and vector accessors reject inconsistent public metadata and use the
owner's fixed type and mutability. Both numeric getters return zero for reference
cells, including GetV128. Setters reject these cells without changing storage.
The check adds no allocations or new locks.

Validation: global metadata mutation, opaque reference access, and global tests
pass (`go test ./src/wago -run 'Test(GlobalRejectsMetadataMutation|ReferenceGlobalScalarAccessIsOpaque|.*Global)'`).

## 2. Freeze compiled execution metadata

Compile and artifact loading publish a private deep execution snapshot. Existing
and future instances share it. Runtime modules retain a separate public Compiled
view. Public signatures return copies. Hand-built metadata freezes on first use.
Changes to public maps, slices, scalars, nested type/GC descriptors, segment
expressions, or names cannot change execution. Do not mutate published metadata
to configure a module; low-level synthetic tests use separate unpublished copies.
Reflection methods use the same snapshot: ABI/exact signatures, type definitions,
exports, global counts/definitions, function names, and native GC admission.
Returned structural descriptors and global initializer bytes are independent
copies. `TestCompiledReflectionUsesExecutionSnapshot` checks compiler, loader,
and frozen hand-built values against later public edits.
Artifact marshaling, streaming, and section-size reporting validate and serialize
that same frozen snapshot. Public edits cannot change a cached artifact. An
unpublished hand-built value still serializes its current metadata. The
`TestCompiledArtifactUsesExecutionSnapshot` regression covers valid and invalid
public edits for both compiler and loader publication through all three APIs.
It fails before snapshot routing and passes afterward. Serialization reuses the
snapshot with no additional metadata copy or retained storage.
Three 100 ms Linux/amd64 samples retain 560 B/op and 14 allocations for small
scalar marshaling (1,097–1,117 ns before; 1,103–1,123 ns after). Imported-module
streaming retains about 263 KB/op and 12,829 allocations (244–255 microseconds
before; 248–260 after). These short samples support the unchanged allocation
count, not a throughput claim.

The public compiler finalizer is allocated separately from the grouped private
cache owner to avoid a finalizer cycle. Instantiation keeps the public owner live
until it has acquired its code lease. Parallel compiler output is mapped before
publication, so public and private code views share one image. The embedded
staging Compiled value is cleared after transfer: its allocation remains live
through the cache, and must not retain the original Go code buffer or metadata.
Serial compiler images still seal in place on first instantiation. A 1 MiB
staging-buffer test checks both views share the mapping and the staging view no
longer retains code. The real parallel-import regression fails before this fix
and passes afterward. On Linux/amd64 Go 1.27.1, the 8-worker import benchmark
emits the same 1,228,829 code bytes: 23.8–24.8 ms before, 23.8–25.0 ms after,
about 7.78 MB allocated per compile in both cases (three 100 ms samples). This
removes the retained heap copy for live modules, with no throughput claim.
Parallel images now stay writable until their first activation, which seals and
registers them under the cache lock. Compiled-only caches do not consume the
fixed 4,096-entry Linux execution registry. A regression retains 4,097 images
then activates a real module; it fails with eager registration and passes with
lazy registration. The existing shared-image and staging-ownership tests remain
valid. Three 100 ms samples keep about 7.78 MB per 8-worker import compile
(23.9–27.3 ms before, 24.9–25.5 ms after). Repeated small-module instantiation
retains 1,520 B/op and 10 allocations (1.97–2.11 microseconds before,
2.02–2.07 after). No throughput claim follows from these short samples.
The constructor allocation test measures platform environment lookup overhead
separately and retains a one-allocation budget for the configuration. Go 1.22
Windows/amd64 with BMI2 uses four allocations for its two environment lookups;
the former fixed total budget of one failed before compilation. The calibrated
budget passes on Windows/Wine and Linux without changing runtime code.

Validation: full `src/wago` tests pass with pinned WABT 1.0.41 and the pinned
Release 3 interpreter. Snapshot mutation tests cover existing/new instances,
Module.Compiled, signatures, and nested containers. One 100 ms amd64 sample:
small compile 12,202 -> 14,660 B/op, 52 -> 67 allocations; runtime instantiation
stays 1,520 B/op and 10 allocations. Snapshot copy alone: 1,560 B/op, 14
allocations, about 498 ns/op. Timing samples are not a throughput claim.

## 13. Validate internal native entry offsets

Marshal and instantiate validate internal-directory length and every normalized
offset against code size. Decode also rejects negative wire entries, so artifacts
cannot enable compile-only direct-prepared markers. The empty legacy directory
remains supported. Fresh compiler markers are stripped by serialization.

Validation: internal-entry boundary/marker tests and focused codec tests pass.
The shared ARM64 element fixture now builds on every ARM64 runtime target,
including Windows. Native Windows CI exposed that its earlier Unix-only test
file made two existing Windows test files fail to compile. The Windows ARM64
backend test binary now cross-builds with the unchanged helper body.

## 14. Align Windows guard-commit calls

The AMD64 guard thunk saves flags before adjusting SP, normalizes SP to a
16-byte boundary, and stores the original synthetic-frame address separately.
It restores that address and uses LEA when returning so restored flags survive.
Both framed and frame-elided faults use aligned Windows shadow/call storage.
The ARM64 commit thunk also saves the interrupted LR across VirtualAlloc and
restores it before retrying a leaf function's access. Its failure branch passes
the primary memory base in X9 to the trap landing pad. The saved LR uses the
remaining eight bytes of the existing 672-byte frame; normal nonfaulting entry
is unchanged. The successful commit path adds one store and one load.
Growth tests cover AMD64 frame elision and both ARM64 wrapper/register calling
conventions. The native ARM64 allocation-failure fixture installs the guard
handler before injecting a failed commit and checks the defined trap. The
Windows guard gate includes the core runtime package. Native
Windows ARM64 CI exposed the missing LR preservation in the engine memory
fixture after command failures stopped being masked. Both guard test binaries
cross-build with Go 1.22.12; the shared test passes on Windows/amd64 under Wine.

Validation: Windows/amd64 guard-tag cross-build passes. The new grown-page store
regression passes under Wine with frame elision enabled and disabled. Native
Windows execution remains a CI requirement; Wine is not native Windows coverage.

## 20. Retain source backups when rollback fails

Both installer and manager publication join the publish and restore errors.
When restore fails, deferred cleanup keeps the only backup and the error reports
its exact path. Successful publication or restoration still removes the backup.
The manager's publication step now has a local rename seam for fault injection.

Validation: installer/manager source tests pass, including separate injected
publish and restore failures and verification of the retained old contents.

## 11. Expose close-operation completion

Instance.Close now documents its prompt return when another caller owns closure.
Instance.WaitClosed waits with a context and returns the active operation's same
result. It does not initiate closure or promise physical release while guest
calls or retained references remain. Callbacks use Close and must not wait for
their own operation. This uses the audit's explicit public-wait alternative.

Validation: race-enabled close tests pass. All 32 waiters observe ErrCallbackPanic
from a blocked close owner; a cancelled wait remains cancellable.

## 15. Reject ambiguous project JSON fields

The OAuth unique-key validator now lives in internal/jsonstrict and is reused
by project readers. Typed validation follows struct fields while preserving
case-sensitive map keys and RawMessage/configuration subtrees. Exact duplicate
keys are rejected everywhere; typed lock-field case variants are rejected too.
Registry wrappers preserve existing callers and behavior.

Validation: project and registry suites pass. Added tests cover repeated plugin
and grant fields, case-folded grant fields, duplicate nested configuration keys,
and distinct case-sensitive map/configuration keys.

## 16. Bound project metadata reads and structures

Manifest and lock reads are limited to 4 MiB each; recovery journals to 12 MiB.
Manifest and lock encoders include their trailing newline in the same 4 MiB
limit. Exact-limit outputs decode successfully; one byte over is rejected.
`TestEncodedProjectMetadataIncludesNewlineInLimit` covers both encoders and
fails before the final-length check. The change does not add an output copy.
Regular-file reads reject symlinks and detect file identity/size/time changes.
Unix opens are nonblocking and no-follow, so replacement with a FIFO cannot
block opening. JSON validation limits depth to 64 and aggregate values to
100,000 before typed decoding, covering plugins, dependencies, grants, contracts,
bindings, and arbitrary configuration. Writes enforce the same readable limits.

Validation: regular-file, JSON, and project suites pass, including oversized
sparse files, FIFO/symlink rejection, nested JSON, and aggregate value limits.

## 8. Index imports once during validation

All five import kinds share one compact uint32 index allocation. Instruction
lookups and validator index counts now take constant time. The indexes are built
before parallel body validation and remain immutable. Interleaved import order
is preserved. No indexes or cache are retained after validation.

Validation: Wasm package tests pass. Single-iteration AMD64 measurements with
25,000/50,000 imports and calls: before 416/1,652 ms; after 1.05/2.06 ms.
Doubling both dimensions now takes about 1.96x time instead of 3.97x. Temporary
allocation changes from 1,232 B to 107,872/206,176 B (one 4-byte index per import,
plus allocator rounding and validator state); 7 -> 8 allocations.

## 12. Decode memory immediates with explicit features

Feature-aware AST and byte-backed decoders now use the selected multi-memory
feature for scalar, SIMD, atomic, and bulk memory immediates. V2 requires literal
reserved zero bytes; V3 permits explicit memory indexes even with one memory.
Syntax-only decoder and backend walkers accept the grammar superset. Public
compilation and conformance decoding select their grammar before validation.

Validation: runtime, frontend, and Wasm suites pass with pinned WABT and the
pinned official interpreter. The grammar matrix covers explicit/padded zero
indexes across eight instruction forms and all three decode/validate paths.

## 17. Publish one active installation record

Runtime version, profile, and build now use one validated, bounded JSON record,
written by durable atomic replacement under a cross-process lock. Readers load
one tuple. Legacy files are migration inputs only; clearing selection writes a
tombstone with standard/normal defaults, so neither legacy state nor a removed
runtime's profile/build can carry into the next selection. Plugin path construction also
uses one tuple per path. Invalid profile/build input fails before publication.

Validation: version race tests pass, including concurrent writers/readers,
publication failure, legacy migration, and invalid records. Version and plugin
suites pass with TMPDIR inside the repository, where temporary Go modules can
use normal Git VCS stamping.

## 7. Fail deadline admission before native entry

Kernel deadline setup now returns errors for request capacity, timer creation,
and timer arming failures. All public cancellation entry paths propagate these
errors before native execution. Request ownership updates are serialized to
prevent a last-release/new-acquire race from clearing a live slot.

Validation: deadline/cancellation tests pass. Capacity-plus-one admission is
checked with bounded slot reservations and no guest execution; shared ownership
is checked through final release. No hot native path gains a lock.

## 10. Bound aggregate artifact metadata allocation

ArtifactLimits adds MaxDecodedBytes (default 256 MiB; zero selects the default).
One budget covers the metadata input buffer and all decoded collections. Each
container reserves four times its actual element size before allocation for
decoded/frozen storage and bounded validation/capacity overhead. Maps, import
sidecars, and GC-offset interning add their own entry allowances. Nested
collections and strings are charged separately. Indexed signature expansion is
charged per use. Native code remains governed independently by MaxCodeBytes.
A 65,536-function module produces a 2,227,258-byte artifact. Its default-limit
round trip through ReadFrom and UnmarshalBinary fails with the previous blanket
1 KiB charge and passes with type-specific reservations; both loaded forms run
the final export. Mixed-width and nested-budget regressions still reject
exhausted budgets. Three 100 ms Linux/amd64 Go 1.27.1 decode samples take
5.60–6.25 ms, about 11.74 MB/op, and 38 allocations. These are allocation
measurements for the newly admitted artifact, not a throughput comparison.

Validation: runtime suite passes with pinned conformance tools. Budget tests
cover aggregate collections, nested signature expansion, overflow-safe charging,
and refusal before the metadata payload is read. The conservative reservations
can reject metadata before its actual heap footprint reaches the stated limit.

## 9. Flatten singleton types and bound decoding

Implicit recursive type groups share a flat subtype slab. Decode limits default
to 100,000 types and 256 MiB of aggregate metadata reservations. The byte-backed
API accepts explicit limits; nested readers share the same budget. Reservations
cover vector growth, parallel decode sidecars, structured custom metadata, and
bounded instruction nesting state. Opaque custom sections reserve their owned
payload plus allocator rounding; only structured name/branch-hint sections use
the expansion allowance. A 3 MiB debug-section regression checks admission and
explicit budget exhaustion. Public compilation defaults to a 64 MiB
input limit; explicitly setting its input limit to zero does not remove decode
limits. The embedded validator reader grows by one pointer (8 bytes on AMD64).
The flattened-type converter's Windows allocation check runs in a fresh test
process because AllocsPerRun reads process-wide allocation counters. Its exact
one-result-allocation limit is unchanged. The parent requires a successful exit
and an explicit passing-test record. The unchanged check passed 20 isolated
Windows/Wine runs; the child-process form passed ten further runs alongside
the large artifact regression, and Linux converter checks passed ten runs.

Validation: Wasm and runtime suites pass. Tests cover singleton/explicit group
limits, metadata limits, shared nested budgets and overflow. One-iteration
100,000-type measurements: 28,970,512 -> 17,621,464 B/op and 100,057 -> 25
allocations; 13.24 -> 14.23 ms. These samples establish the allocation reduction,
not a speed improvement.

## 5. Authenticate and release Linux signal ownership

Installation negotiates signals 35..64, preferring an ignored signal and then a
Go dispatcher; libc-reserved signals and unrelated native handlers are excluded.
Go preinstalls dispatchers across this range, so sharing a dispatcher preserves
os/signal use rather than claiming exclusive ownership. Queued broadcasts and
kernel timers carry a random process cookie and a non-reused request sequence.
Both handlers require the exact active token before interrupting a guest. Late
timer deliveries cannot affect a later call using the same trap address.

The full flags, mask, and restorer are preserved. Every unowned delivery chains
to the prior dispatcher. Final code unmapping restores the complete action only
while it still matches Wago's installed action. Timer deletion does not drain
unrelated pending signals. Request storage grows by 512 fixed bytes (64 tokens).

Validation: race-enabled isolated-process tests pass for os/signal notification
during installation and after final unmap, exact action restoration, reinstallation,
and token changes on trap reuse. Deadline/cancellation tests pass. Linux ARM64
and Windows AMD64 runtime test binaries cross-build.

## 6. Release the Go processor during foreign execution

Standard-Go entry, resume, and prepared integer transitions now record a
scannable Go-stack boundary with entersyscall, run the non-splitting assembly
leaf, restore the Go stack, and reacquire a P with exitsyscall. Host callbacks
run after reacquisition. This permits cancellation callbacks and Go GC to run
when every execution thread is in Wasm. No new per-call allocation is added.
Slice owners remain live through native return and the full host-resume loop.
Raw address arguments retain their caller-owned stable-storage contract.
`TestForeignCallRetainsBufferOwners` uses bounded native calls with concurrent
GC and detects premature buffer finalizers before the fix. Normal and prepared
calls pass afterward. Three 100 ms Linux/amd64 samples keep zero allocations:
Engine.Call measures 19.9–20.8 ns before and 21.8–22.5 ns after; host round trips
measure 105.5–106.4 ns before and 105.5–116.3 ns after. Retention sits outside
the host loop; these short samples are not a throughput claim.

TinyGo's threads scheduler can publish cancellation concurrently. Other TinyGo
schedulers now reject cancelable native calls before entry with an explicit
scheduler error; they cannot promise concurrent cancellation. Ordinary calls
remain available. The obsolete informational GC-stall test was replaced by a
bounded 40 ms foreign-call test that requires GC progress with one P.
Callback-context tests retain lifetime and entry-path coverage with background
parents on cooperative TinyGo schedulers, and explicitly require cancelable
calls to fail before entering host code. Threaded cancellation has a separate
standalone gate; the full TinyGo suite keeps the tasks scheduler for GC safety.

Validation: full runtime/wago suites pass; cooperative-target cancellation tests
pass; the one-P boundary test passes with the race detector. The TinyGo threads
standalone cancellation test passes (38.6 s including cold compilation). Windows
AMD64 entry runs under Wine; Darwin ARM64 and Linux ARM64 cross-build. Single
100 ms samples: Engine.Call 9.38 -> 18.71 ns/op; InvokeAddOne 103.7 -> 106.2 ns/op;
both retain zero B/op and zero allocations.

## 18. Publish manager and source as one release

Installation and self-update prepare an immutable binary/source pair, verify it,
sync it, and switch one versioned selection record under a process lock. A
stable dispatcher runs the selected payload; executable-path resolution keeps
renamed symlinks and caller-supplied argv[0] in manager mode. Tests use directory
aliases on Linux too and compare canonical source paths. Source lookup follows the running
payload's own directory. Older running managers retain their original source.
The current and previous release remain available for rollback. Successful
publication prunes older pairs only after acquiring an exclusive lease; running
processes hold shared leases. Unix dispatch preserves the lease descriptor across
exec; Windows holds it while waiting for its child. New payloads also pin their
own release. Retirement renames the directory under the exclusive lease before
removal, closing the launch/prune race. Unpublished recovery and legacy data are
excluded. Tests retain live source through four further updates, then prove the
next update returns to two pairs after process exit on Linux and Windows/Wine.
Selected-release lookup plus lease acquisition/close measures 38.5–66.4 µs,
5.1 KB, and 59 allocations per manager startup on Linux/amd64 Go 1.27.1; this
adds no runtime hot-path work or background worker. Windows atomic-record reads
permit pathname replacement and retry transient sharing conflicts for at most
32 attempts with 10 ms waits, preserving persistent errors. This fixes a Wine
concurrency failure also reproduced before the lease change. Failed
bootstrap restores the old selection and retains staged recovery data. Release
publication also restores the selection if root-directory sync or writing the
published marker fails. First-publication rollback removes the new pointer;
later-publication rollback restores its prior bytes. Marker-failure tests prove
that retry succeeds and subsequent updates can prune the retried release. A
failure syncing an already-written marker reports that the manager is selected.
Bootstrap returns an undo action for its launcher replacement. Pre-commit
rollback runs that action under the publication lock even if pointer restoration
fails. First installations remove their new dispatcher; legacy and managed
installations restore prior launcher bytes and permissions from the retained
`previous-launcher` backup. Tests cover bootstrap and later marker failures for
all three states, plus an undo failure that reports the exact backup path.
Reinstall
cleanup compares configured and physical ancestry by filesystem identity,
preserving protected releases and their directory aliases across symlinks and
Windows case variants. Configured bin/source symlinks beneath the cleanup root
also survive when their targets are outside that root, including aliases in
parent components. Both outward-alias regressions lose the configured paths
before this fix and preserve them afterward while obsolete root data is removed.
Alias, case-variant, and nested-cache cleanup regressions pass on Linux and Wine.
All uninstall modes remove manager release artifacts without deleting unrelated
launcher-directory siblings. Minimal mode preserves separately installed
runtimes. Windows removal waits for the actual payload, which can differ from
the launcher. Tests cover all modes at default/custom locations and deferred
Windows payload removal. Preparation, publication, and uninstall now share one
coordinator. Uninstall preserves that file and its ancestors until destructive
cleanup ends, then renames and removes the lock while holding it. Waiters reject
the retired inode; only nonrecursive empty-directory removal follows retirement,
so a later installation survives. Windows lock handles permit rename/deletion.
The deferred PowerShell worker takes the same byte-range lock, checks its file
identity against the scheduling process, and follows the same retirement order.
It also includes managed paths that a publisher can create before the worker
starts. Tests cover preparation and uninstall waiting for publication, stale
waiters, and preserving a later installation. Linux race checks and Windows/Wine
lock and synchronous uninstall tests pass. Native Windows CI covers the actual
PowerShell worker; Wine's PowerShell stub emits no output for a Write-Output
probe and cannot run those integration tests locally. These locks add no runtime
hot-path work or persistent coordinator after a completed uninstall.
Native Windows CI additionally exposed short-path aliases during recursive
cleanup and directory renames blocked by a held child lease. The worker expands
8.3 paths before coordinator comparisons. Pruning first renames the lease to a
retirement marker, closes it, then renames the containing directory. Failed
retirement can resume from the marker; transient Windows sharing conflicts have
a bounded 320 ms retry budget. Marker-resumption and full publication/lease
suites pass with the race detector and on Windows/Wine.
The Windows CI build/test and guard steps now stop on each failed native command.
This prevents a later passing command from hiding an earlier package failure.
Before starting a deferred worker, uninstall creates a synced pending marker
while holding the coordinator. Both preparation and publication reject that
marker, including during the handoff before the worker acquires the lock. The
worker takes ownership before waiting for the parent and preserves the marker
until destructive cleanup finishes. Failed process start rolls back only a
new marker; an earlier cleanup intent survives another scheduling failure.
The worker then removes the marker before retiring the held lock. A failed
cleanup retains its marker and reports pending cleanup to later installers.
The handoff regression shows both preparation writes and publication occur
without the gate, then verifies both are blocked with it. Linux race and
Windows/Wine pending-marker, rollback, and worker-start-failure checks pass.

Validation: publication/rollback, concurrent reader/writer, source-pinning and
Unix dispatch tests pass with the race detector. Windows publication tests pass
under Wine; the complete Wine installer flow passes with Go 1.27.1 and checks
the selected binary/source pair. Installer, manager, plugin build, version and self-update suites pass
with TMPDIR inside the repository. All three CI standalone TinyGo gates pass
with pinned TinyGo 0.41.1 and Go 1.22.12 (94.7 s combined). Local TinyGo 0.42.0
fails tasks builds with a duplicate tinygo_task_exit linker symbol on both this
branch and unchanged main. Threaded TinyGo cancellation passes.
