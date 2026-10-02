# Format versioning

Wago has not published its first release. Every Wago-owned codec, persisted
format, machine-readable schema, cache-key encoding, snapshot format, and native
metadata ABI therefore uses **version 1**.

Incompatible development layouts are consolidated into version 1 rather than
consuming public-looking version numbers before users can depend on them. Readers
remain strict: an unsupported version is rejected rather than guessed, upgraded,
or interpreted as an older layout.

After Wago's first release, an incompatible change to a persisted or
native-visible contract must increment the relevant version and document the
compatibility and migration policy. Third-party and standards-defined versions
(such as WebAssembly releases, Go module versions, tool versions, and JSON Schema
dialects) are outside this policy.
