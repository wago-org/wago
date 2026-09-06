# Semantic corpus

Use this corpus to test real WebAssembly programs with real outputs. Each
program is pinned to an upstream revision and checked against an exact,
independently verifiable oracle.

A module does not pass merely because it compiles, instantiates, and exits. Its
observed result must exactly match the expected result.

This corpus complements `tests/regressions/`, which focuses on specification,
validation, runtime, and ABI edge cases.

## Run It

From the repository root:

```sh
go test ./tests/semanticcorpus
```

The manifest gate checks every artifact digest and copied license without a
toolchain. The execution gate compiles each artifact, creates a fresh instance,
then creates a second instance from the same compiled module. Both must produce
the exact oracle. Each case has a `timeout_ms` limit. `InvokeContext` stops a
miscompiled guest from running forever.

## Layout

- `MANIFEST.json` — the acceptance inventory. Each row names a checked-in `.wasm`
  artifact, its SHA-256, upstream provenance, the invocation (core-ABI export +
  arguments + optional memory inputs), and the exact oracle.
- `<corpus>/` — per-corpus directory containing the checked-in artifact, the
  reviewed porting/build files, the copied upstream license, and a `build.sh`
  that rebuilds the artifact from the pinned revision and verifies the digest.
- `../semanticcorpus/` — the Go package that loads the manifest and executes
  every case through the wago core API, comparing results exactly.

## Oracle Strengths

The manifest `expect`/`vectors` block is always an exact oracle, not a smoke
check. The first tranche uses these documented strengths:

| Strength | Meaning                                 | Example here          |
| -------- | --------------------------------------- | --------------------- |
| 1        | Published vectors / prescribed output   | BLAKE3 test vectors   |
| 2        | Upstream checker / self-test            | CoreMark known CRCs   |
| 3        | Native build of the same pinned source  | CoreMark native diff  |
| 5        | Round-trip checked by an independent decoder | QOI, zlib, zstd native-encoded reference streams |

CoreMark uses its published per-kernel CRC table for the standard 2K performance
run: `crclist=0xe714`, `crcmatrix=0x1fd7`, and `crcstate=0x8e3a`. The table is
from the pinned upstream `core_main.c` and is independently confirmed by a
native arm64 build of the reviewed port.

BLAKE3 uses upstream `test_vectors.json`: 35 input lengths in all three modes
(hash, keyed hash, and derive key). Each check compares the full 131-byte
published digest/XOF. QOI, zlib, and zstd decode a native-encoded reference
stream and compare the exact decompressed bytes. A native encoder, from the
pinned toolchain or the host, makes the stream. The wasm decoder must reproduce
the original deterministic pattern byte for byte.

## Guest-Owned Buffers

Core-ABI runners own their input and output buffers. They expose zero-argument
pointer exports, such as `blake3_input_ptr` and `blake3_output_ptr`. The harness
resolves the addresses at run time instead of assuming a linear-memory layout.

Do not put host-written buffers at a fixed low offset. The guest's default
64 KiB stack starts at `__stack_pointer = 65536`; a low buffer can collide with
the stack and corrupt a large input. Corpus runners must not hardcode a
host-visible buffer offset below the guest stack.

## Known Issues and Current Status

A case with a `known_issue` field is skipped by the execution gate with that
message. It is not silently dropped. Its pinned artifact, oracle, and build
script remain checked in. Clearing the field after a fix re-enables the exact
same case.

The previously recorded LZ4 JIT discrepancy no longer reproduces. Compression
and decompression are enabled and checked against their exact size and byte
oracles on every supported corpus-test platform.

## Rebuild an Artifact

Artifacts are checked in, so normal suite runs need no toolchain. To verify one
after a rebuild, run its build script. It stages output in a temporary directory,
compares SHA-256 with the checked-in artifact, and does not overwrite it by
default:

```sh
tests/corpora/coremark/build.sh   # needs wasi-sdk (WASI_SDK=/path/to/wasi-sdk)
```

After you review a deliberate port or upstream change, rebuild with `UPDATE=1`
to replace the artifact. Then update `MANIFEST.json`'s `artifact_sha256` and
record new provenance. A default digest mismatch is intentional. It means the
checked-in bytes and manifest pin disagree and need review.

## Provenance Rules

Each case records its repository, exact revision, revision date, license, and
toolchain version in `MANIFEST.json`. The corpus directory includes the upstream
license. Build scripts clone the pinned revision into the ignored
`.tmp/upstream/` directory. They do not build an unpinned checkout.

Wago-owned porting patches are checked in as reviewed source. They compile
against the pinned upstream translation units.

## Current Cases and Future Work

Current cases are CoreMark (self-validating CRC), BLAKE3 (105 published-vector
checks across hash, keyed, and derive modes), QOI (exact encode and decode
against a native reference), LZ4 (compress/decompress round trip), zlib inflate
(exact decode of a native-encoded stream), and zstd decompress (exact decode of
a native-encoded frame).

Planned work follows the same pattern: JSONTestSuite classification, Stockfish
`perft` node counts, and a 6502 emulator running the Klaus functional test.
WASI Preview 1 cases are later work. They need the external `wago-org/wasi` host
plugin. The manifest `abi` field is reserved for them; `core` is the only
admitted value today.
