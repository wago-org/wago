# Wago format versions

Wago has not published its first release. This file describes version numbers
for Wago-owned stored data and interfaces. It does not describe the CLI release
number.

Most Wago-owned persisted formats, machine-readable schemas, snapshot formats,
and metadata ABIs use **version 1**.

The compiled `.wago` executable codec uses **version 2**. Wago introduced version
2 on August 30, 2026. Generated `memory.grow` code and the native instance
context gained a runtime memory-page quota. Wago must reject version-1 executable
code so that an older artifact cannot bypass a stricter runtime configuration.

Readers are strict. They reject an unsupported version instead of guessing,
upgrading, or partly decoding it. Cache-key encodings have their own explicit
version. Before the first stable release, Wago can consolidate incompatible
development layouts when they do not cross an executable safety or policy
boundary.
