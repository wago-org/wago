# Wago format versions

Wago has not published its first release. This file describes version numbers
for Wago-owned stored data and interfaces. It does not describe the CLI release
number.

Most Wago-owned persisted formats, machine-readable schemas, snapshot formats,
and metadata ABIs use **version 1**.

Native-code `.wago` module artifacts are no longer supported. Rebuild from the
original `.wasm` source; there is no artifact migration or compatibility loader.

Readers are strict. They reject an unsupported version instead of guessing,
upgrading, or partly decoding it. Cache-key encodings have their own explicit
version. Before the first stable release, Wago can consolidate incompatible
development layouts when they do not cross an executable safety or policy
boundary.
