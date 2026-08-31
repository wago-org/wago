# Format versioning

Wago has not published its first release. Most Wago-owned persisted formats,
machine-readable schemas, snapshot formats, and metadata ABIs use **version 1**.

The compiled `.wago` executable codec uses **version 2**. Version 2 was introduced
on August 30, 2026 because generated `memory.grow` code and the native instance
context gained a runtime memory-page quota. Rejecting version-1 executable code
is required so an older artifact cannot bypass a stricter runtime configuration.

Readers remain strict: an unsupported version is rejected rather than guessed,
upgraded, or partially decoded. Cache-key encodings have their own explicit
version. Before the first stable release, incompatible development layouts can
still be consolidated when they do not cross an executable safety or policy
boundary.
