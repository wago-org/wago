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

A parent command cancellation is propagated to active manager requests and to
Git/TinyGo/Go subprocesses used by source-build fallback. In-memory bodies are
explicitly bounded: registry and GitHub release JSON use a 4 MiB limit, OAuth
responses use 1 MiB, release checksums use 4 KiB, and non-success response
capture uses an independent 64 KiB limit. Declared oversized bodies are rejected
before reading; chunked or dishonest responses are stopped after the configured
limit. Release/commit browsing is capped at ten 100-item pages so repeated full
pages cannot accumulate metadata indefinitely. OAuth device-flow lifetimes and
server-provided polling intervals are also capped.

## Release downloads

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
