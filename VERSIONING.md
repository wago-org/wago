# Wago format versions

Wago has not published its first release. This file describes version numbers
for Wago-owned stored data and interfaces. It does not describe the CLI release
number.

Most Wago-owned persisted formats, machine-readable schemas, snapshot formats,
and metadata ABIs use **version 1**.

The compiled `.wago` executable codec uses **version 4**. Version 4 changes the
native structural type-key derivation to hash each recursive group once and derive
member keys from its digest. Since executable code and metadata persist those
keys together, Wago rejects version-3 artifacts instead of mixing incompatible
native call discriminators. Version 3 added AMD64 CPU requirements, complete
feature metadata, and structural reference type codes. Version 2 introduced a
runtime memory-page quota. Wago must reject version-1 executable code so that
an older artifact cannot bypass a stricter runtime configuration.

Readers are strict. They reject an unsupported version instead of guessing,
upgrading, or partly decoding it. Cache-key encodings have their own explicit
version. Before the first stable release, Wago can consolidate incompatible
development layouts when they do not cross an executable safety or policy
boundary.
