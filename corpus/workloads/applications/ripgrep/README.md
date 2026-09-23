# ripgrep

`rg.wasm` is the `rg` asset from a-Shell-commands release `0.1`, updated
2025-03-15 (asset ID `RA_kwDOD6Z6Xc4OLJYu`). It identifies itself as
ripgrep 14.1.1. The module SHA-256 is
`bd3d27817f2f34d625a3d24029eb2d5b16746ee4bb097d01eed8c99be9b83263`.

The workload searches QuickJS's pinned JavaScript source. `-N` suppresses
automatic line numbering so output has the same byte-exact interpretation in
Wago, wazero, and Wasmtime. This artifact imports only WASI preview1; no
a-Shell-specific mocks are needed.

The command is admitted on ARM64 hosts. Its explicit-bounds amd64 execution
currently reaches a Rust comparator-invariant panic, so amd64 remains excluded
instead of accepting output that differs from the independent oracle.
