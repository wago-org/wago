# Wago format versions

Wago has not published its first release. This file describes version numbers
for Wago-owned stored data and interfaces. It does not describe the CLI release
number.

Most Wago-owned persisted formats, machine-readable schemas, snapshot formats,
and metadata ABIs use **version 1**.

The compiled `.wago` executable codec uses **version 6**. Version 6 records the
producer architecture in the fixed header and rejects foreign-ISA native code
before allocating an executable image. Version 5 moved the EH tag-directory
pointer outside the wrapper tail-argument bank. Native instructions embed that
basedata offset, so Wago rejects version-4 artifacts instead of running code that
reads an argument as a tag-directory pointer. Version 4 changed native
structural type-key derivation to hash each recursive group once and derive member
keys from its digest. Version 3 added AMD64 CPU requirements, complete feature
metadata, and structural reference type codes. Version 2 introduced a runtime
memory-page quota. Wago must reject version-1 executable code so that an
older artifact cannot bypass a stricter runtime configuration.

Readers are strict. They reject an unsupported version instead of guessing,
upgrading, or partly decoding it. Cache-key encodings have their own explicit
version. Before the first stable release, Wago can consolidate incompatible
development layouts when they do not cross an executable safety or policy
boundary.
