# CLI release hardening

Wago's manager applies explicit resource and publication policies to networked
release operations.

## HTTP policies

The shared internal HTTP client uses request contexts plus operation-class
limits instead of the process default client:

- release metadata and checksums have a 45-second overall deadline;
- bounded release/source streams have a 30-minute overall deadline; and
- dial, TLS-handshake, response-header, idle-connection, and
  `Expect: 100-continue` waits have independent transport timeouts.

A parent command cancellation is propagated to active manager release requests.
In-memory GitHub release JSON uses a 4 MiB limit, release checksums use 4 KiB,
and non-success response capture uses an independent 64 KiB limit. Declared
oversized bodies are rejected before reading; chunked or dishonest responses are
stopped after the configured limit.

## Release downloads

Executable checksums are fetched before assets. The parser accepts exactly one
64-hexadecimal SHA-256 record naming the expected asset. The executable is then
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
replace-existing and write-through flags. Running-manager self-replacement keeps
its specialized delayed-restart fallback when Windows prevents immediate
replacement.

File contents are synced before release publication, but Wago does not claim
perfect power-loss durability because the parent directory is not synced on all
platforms.
