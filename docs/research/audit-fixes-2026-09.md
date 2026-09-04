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
