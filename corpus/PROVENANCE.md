# Corpus Provenance

The exact source revision, toolchain, artifact digest, inputs, and oracle for
semantic workloads are recorded directly in `catalog.json`. The remaining local
workloads are rebuilt from `sources/` with the scripts in `build/`.

| class | retained workloads | source or revision |
| --- | --- | --- |
| synthetic WAT | tiny, recursion, memory, indirect dispatch, function scale | reviewed files in `sources/wat` |
| Rust compute | linked list, nbody, fannkuch, matmul, SHA-256, ray tracing | reviewed files in `sources/rust` |
| AssemblyScript | json-as, blake-as, utf-as; scalar and SIMD | local adapters plus the corresponding upstream package checkout |
| semantic | CoreMark, BLAKE3, QOI, LZ4, zlib, zstd | revisions and WASI SDK versions pinned per catalog check |
| Embench | crc32, huffbench, matmult-int, nettle-aes, nettle-sha256, qrduino | `09c2ed8c3b7008c95d08b038de4a3f6dc103ed70`, WASI SDK 34 |
| Sightglass | shootout base64, libsodium hash | `9ce88522d75b2d155e358f576e7d88ed26d14de8`, Binaryen 130 timing-hook removal |
| TACLeBench | self-checking bubble sort | `c6a0d73e47bbd2bc86e34637156fb26dd4d5cf08`, WASI SDK 34 |
| compile-only | esbuild 0.21.5 | npm `esbuild-wasm@0.21.5` |

The suite intentionally excludes generated ISA sweeps, opaque Wasm-R3 replays,
platform-gated WABench ports, PolyBench commands without result oracles,
redundant microbenchmarks, and third-party programs that only proved they did
not trap. Those artifacts add maintenance and CI cost without providing a
stable correctness or performance signal.

Rebuild scripts never redefine admission. Review rebuilt bytes, update the
catalog digest and provenance deliberately, and rerun the individual
correctness and benchmark-wiring targets before committing.
