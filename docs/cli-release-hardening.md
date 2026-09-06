# CLI release and credential hardening

Wago's manager applies explicit resource and publication policies to networked
release, registry, and authentication operations.

## HTTP policies

The shared internal HTTP client uses request contexts plus operation-class
limits instead of the process default client:

- small registry/API/OAuth JSON requests have a 30-second overall deadline;
- release metadata and checksums have a 45-second overall deadline;
- bounded release/source streams have a 30-minute overall deadline; and
- dial, TLS-handshake, response-header, idle-connection, and
  `Expect: 100-continue` waits have independent transport timeouts.

A parent command cancellation is propagated to active manager requests,
installer downloads, and Git/TinyGo/Go subprocesses used by source-build
fallback. In-memory bodies are explicitly bounded: registry and GitHub release
JSON use a 4 MiB limit, OAuth
responses use 128 KiB, release checksums use 4 KiB, and non-success response
capture uses an independent 64 KiB limit. Declared oversized bodies are rejected
before reading; chunked or dishonest responses are stopped after the configured
limit. Release/commit browsing is capped at ten 100-item pages so repeated full
pages cannot accumulate metadata indefinitely. Rolling-channel lookup follows
validated same-origin GitHub `Link` pagination within that budget, skips draft
releases, and requires the selected release to expose a canonical 40-hex commit
target. OAuth device-flow lifetimes and server-provided polling intervals are
also capped.

Browser and headless registry login both use GitHub's one-time device
authorization. Browser mode opens only GitHub's verification URL after printing
the short-lived user code; headless mode leaves that URL for the user to open
elsewhere. The GitHub access token is sent to the registry in a bounded POST
body, and the long-lived Wago bearer token is accepted only from that exchange's
bounded response body. Both registry requests remain pinned to the origin chosen
at login start, redirects are not followed, and bearer validation uses that same
origin. Registry authentication requires HTTPS except for explicit loopback
development servers. Browser targets must match GitHub's device-verification
origin and path. Wago does not run a loopback callback server or place either
credential in a browser-visible URL.

The registry's advertised GitHub scopes are restricted to `read:user` and
`user:email`. Authentication identifiers, codes, tokens, displayed registry
text, stdin token input, and the local credential store have explicit size and
printable-text limits. All registry network operations—including dependency
catalog lookup and install metrics—reject remote plaintext HTTP origins and
redirects; HTTP remains available for loopback development servers.
Bodies returned by failed authenticated requests are discarded so reflected
credentials cannot reach errors or terminal output. Contradictory OAuth
success/error responses fail closed, duplicate authentication JSON members are
rejected before decoding, and credential stores must be regular files containing
unambiguous JSON entries. Catalog source checksums, versions, and digests are
validated before use; invalid remote metadata is not copied into terminal errors.
Catalog pages are preflighted before materializing release structs, and each
requested plugin is limited to 1,024 candidates across at most four 256-release
pages and 16 MiB of cumulative response metadata. Non-schema catalog objects,
arrays, and nesting are structurally bounded to 128 members and 32 levels before
decoding; case-sensitive property names and collections inside embedded
configuration JSON Schema remain distinct and retain their schema semantics.
Duplicate catalog struct fields fail closed, and dependency backtracking stops
after 2,048 unique catalog queries and 64 MiB of aggregate response metadata,
independently of the solver's CPU-step ceiling. Exact plugin/constraint queries
are pinned and reused throughout one solve, preventing repeated downloads and
mid-resolution result changes. Registry error and package-resolution JSON also
rejects ambiguous fields; resolved module paths are canonical, and typo
suggestions are limited to 1,024 bounded identifiers with banded edit distance.

## Release downloads

Canary and nightly binaries now stamp `canary@<full-sha>` or
`nightly@<full-sha>` as their comparison identity; user-facing labels remain
abbreviated. Canary release tags also use the full commit ID. Legacy abbreviated
stamps are treated as stale rather than compared by prefix, so a colliding short
hash cannot suppress an update.

Executable checksums are fetched before assets. The parser accepts exactly one
64-hexadecimal SHA-256 record naming the expected asset; the checksum tools'
harmless `./` filename prefix is accepted for existing releases, while arbitrary
parent or nested paths are rejected. New release workflows emit basename-only
records. The executable is then
streamed through a 64 KiB copy buffer into a unique same-directory temporary
file while SHA-256 and progress are updated. Executables are limited to 512 MiB,
which leaves substantial headroom over current stripped Wago binaries without
allowing an endpoint to consume unbounded disk or memory. Source archives use a
separate 256 MiB streaming limit.

Downloaded source ZIPs are preflighted completely before filesystem mutation.
Extraction permits one top-level directory and strict regular files/directories
only. It rejects traversal, duplicate and case-colliding paths,
file/directory conflicts, non-portable names, encrypted entries, mixed or special
file modes, unsupported ZIP versions, compression, or general-purpose flags,
mismatched local headers or data descriptors, ZIP64 metadata, and local-record
gaps, overlaps, prefixes, or data ranges that reach the central directory, and
archives beyond 20,000 entries, 16,000 files,
4,000 unique directories, 16 MiB of central-directory metadata, 64 relative path
components, 255 bytes per component, 1,024 path bytes, 128 MiB per file, or 512
MiB expanded content. Directories must have trailing slashes and zero declared
content. Stored files must have equal compressed and uncompressed sizes. The
stripped archive root must contain an exactly spelled regular `go.mod` file.
GitHub zipballs currently label ordinary directory and Deflate entries as ZIP
version 1.0, so supported Store/Deflate entries retain that compatibility while
versions above 2.0 and their unsupported features remain rejected independently.

Path components are restricted to portable printable ASCII, including rejection
of Windows DOS device aliases such as `CON`, `CONIN$`, and `CONOUT$`, even when
spaces precede an extension. A single byte scan validates each path, each complete
relative path is canonicalized at most once,
and parent nodes are derived by slicing at slash offsets. The preflight therefore
scales linearly with accepted path metadata rather than repeatedly allocating
and joining every parent prefix. On Go 1.26.5/linux-amd64 on a Ryzen 7 7800X3D,
the 16,000-file, 63-shared-directory production-shaped benchmark improved from
1.103 seconds, 1,078,897,384 bytes, and 2,048,079 allocations per operation to
150 milliseconds, 20,184,512 bytes, and 64,200 allocations per operation after
strict central/local metadata agreement, exact local-record coverage, and
data-descriptor preflight.

The fixed-memory central-directory scanner and Go ZIP reader share one opened
regular-file descriptor, preventing pathname replacement between validation and
extraction. The descriptor is closed before publication. Decompressed bytes are
counted against the declared and aggregate limits instead of trusting ZIP
metadata. Extraction occurs in a private sibling staging directory, observes
manager and installer command cancellation, and publishes the complete tree with
an atomic no-replace rename. Every failure attempts to remove the staging tree,
and cleanup or close failures are surfaced rather than discarded. ZIP entry and
metadata limits remain checked before Go's ZIP reader allocates per-entry objects.

The August 2026 limit check used 3,521 tracked files, 441 unique directories,
467,141 central-directory bytes, 52,815,053 total bytes, a 16,243,226-byte
largest file, 100 path bytes, and eight path components on `main`; each
extraction ceiling therefore retains substantial headroom over the source tree
it protects.

The platform no-replace helpers remain no-cgo. On linux/amd64, the stripped,
trimmed standard manager measured 9,052,322 bytes before these follow-up guards
and 9,064,610 bytes after them, a 12,288-byte increase.

The destination is published only after the complete stream has the expected
length and digest, the executable mode is set, the file is synced, and it is
closed. Cancellation, malformed checksums, network interruption, oversized
content, hash mismatch, write/sync/close failure, and replacement failure leave
the previous destination unchanged and remove temporary files.

Publication rejects destination symlinks, directories, and non-regular files.
Unix uses same-filesystem rename replacement; Windows uses `MoveFileExW` with
replace-existing and write-through flags and briefly retries sharing conflicts
from readers or concurrent publishers. Running-manager self-replacement uses a
unique same-directory staging path and keeps its specialized delayed-restart
fallback when Windows prevents immediate replacement.

File contents are synced before release publication, but Wago does not claim
perfect power-loss durability because the parent directory is not synced on all
platforms.

## Registry credentials

Registry credentials remain a plaintext fallback store containing long-lived
bearer tokens. Every mutation, including the decision whether logout has anything to remove:

1. acquires an OS-backed exclusive lock associated with `credentials.json`;
2. re-reads and validates the latest JSON while locked;
3. applies the save or delete to that current state;
4. writes a unique same-directory temporary file created as `0600`;
5. syncs and closes it; and
6. atomically replaces the prior regular file.

This serializes separate Wago processes and prevents unrelated registry entries
from being lost. Malformed existing data is reported rather than overwritten.
Failures before replacement preserve the previous complete credential file and
remove token-bearing temporary files.

On Unix-like systems Wago repairs the configuration directory to `0700` and a
successfully updated credential file to `0600`, including files that were
previously more permissive. On Windows the same locking and atomic replacement
rules apply, but POSIX mode bits are not equivalent to Windows ACL guarantees;
this implementation does not claim ACL hardening beyond the current user's
normal filesystem protections.

## Paired manager releases

The installer and self-update stage the manager and plugin-build source together
under `bin/.wago-releases/release-*`. They verify the staged manager, sync the
pair, and atomically publish `bin/.wago-release.json` under a process lock. The
record names the previous release for rollback. Old release directories remain
intact while old managers run. Source lookup uses the running executable's own
release directory, never the current selection.

Each successful publication retains the current and previous releases and prunes
older inactive pairs. Running managers hold a shared file lease; Unix dispatch
keeps its descriptor across exec, and Windows dispatch holds it while waiting
for the child. New payloads also pin themselves when started directly. Pruning
takes an exclusive lease and renames the retired directory before unlocking,
so a concurrent launch cannot pin a directory already selected for removal.
The next publication removes pairs whose last process has exited. No background
worker or process registry is needed. Failed-publication recovery directories
and legacy directories without lease metadata stay until explicit recovery or
uninstall; they are not part of the normal successful-update retention set.

The stable `bin/wago` dispatcher enters the selected payload. Launcher role and
release discovery use the real executable path, so renamed symlinks and custom
`argv[0]` values cannot route manager calls into installer commands. The installer
supplies this dispatcher even for older manager payloads, passing paired source
through `WAGO_SRC`. It tracks that automatic binding with `WAGO_RELEASE_SOURCE`
so a nested launcher updates it; an explicit user `WAGO_SRC` override still wins.
Self-update can use its existing dispatcher without replacing a running Windows
executable. Legacy source, including `WAGO_SRC_DIR`, is preserved for old running
managers; new paired sources live beside their immutable manager payload.

First-install/legacy bootstrap retains any prior launcher as `previous-launcher`
in the staged release. A bootstrap failure restores the prior selection; a
failed restoration reports the retained release path. Staged verification and
publication failures never delete prior source. Reinstall data cleanup happens
after publication and preserves release/legacy source paths. Every self-uninstall
mode removes the stable launcher, release selection/lock files, and all paired
releases, including their source. Minimal mode still preserves separately
installed runtimes, configuration, and legacy source. Full uninstall also removes
the selected Wago home; a custom launcher directory keeps unrelated sibling
files. Windows defers removal until the running payload exits, even when its
path differs from the stable launcher.

To roll back an existing paired installation, select its recorded previous
release through the same locked atomic writer. For a legacy bootstrap, restore
the retained launcher and its prior selection (or remove the selection if none
existed). During update and rollback, do not delete a release while a process
still runs its payload. Explicit uninstall ends the paired-source guarantee.

## Plugin build lock removal races

The plugin build module uses a directory lock across processes. On Windows,
`mkdir` can return access denied while the previous owner removes that directory;
a following `Stat` may already see no lock. Acquisition retries one consecutive
permission failure without a visible lock, using the existing 50 ms poll. A
second failure returns the original permission error. Confirmed contention
retains the existing timeout. The callback runs only after `mkdir` succeeds.

`TestBuildLockRetriesRemovalRace` injects the removal window and verifies actual
lock creation; `TestBuildLockReportsPersistentPermissionFailure` checks the
bounded error path. The serialization test still checks that callbacks do not
overlap. The removal test fails without the retry. All build-lock tests pass in
20 repetitions on Linux and Windows/amd64 under Wine with Go 1.22.12. This affects
CLI lock contention only; Wasm execution and runtime memory layout are unchanged.
