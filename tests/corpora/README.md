# Semantic corpus

Real programs compiled to WebAssembly, each pinned to an upstream revision and
checked against an **exact, independently verifiable oracle**. The rule that
defines this corpus: a module that merely compiles, instantiates, and exits
successfully does **not** count as a pass — the observed result must match the
expected result exactly.

This is a complement to the existing regression corpora in `tests/regressions/`
(which focus on spec, validation, and runtime/ABI edge cases). It exists to
grow Wago's confidence in *real programs with real outputs*.

## Layout

- `MANIFEST.json` — the acceptance inventory. Each row names a checked-in `.wasm`
  artifact, its SHA-256, upstream provenance, the invocation (core-ABI export +
  arguments + optional memory inputs), and the exact oracle.
- `<corpus>/` — per-corpus directory containing the checked-in artifact, the
  reviewed porting/build files, the copied upstream license, and a `build.sh`
  that rebuilds the artifact from the pinned revision and verifies the digest.
- `../semanticcorpus/` — the Go package that loads the manifest and executes
  every case through the wago core API, comparing results exactly.

## Oracle strengths used

The manifest `expect`/`vectors` block is always an exact oracle, not a smoke
check. The first tranche leans on these document-described strengths:

| Strength | Meaning                                 | Example here          |
| -------- | --------------------------------------- | --------------------- |
| 1        | Published vectors / prescribed output   | BLAKE3 test vectors   |
| 2        | Upstream checker / self-test            | CoreMark known CRCs   |
| 3        | Native build of the same pinned source  | CoreMark native diff  |
| 5        | Round-trip checked by an independent decoder | QOI, zlib, zstd native-encoded reference streams |

CoreMark's oracle is its published per-kernel CRC table for the standard 2K
performance run (`crclist=0xe714`, `crcmatrix=0x1fd7`, `crcstate=0x8e3a`),
taken from the pinned upstream `core_main.c` and independently confirmed by a
native arm64 build of the same reviewed port. BLAKE3's oracle is the upstream
`test_vectors.json`: 35 input lengths across all three modes (hash, keyed hash,
derive key), each compared against the full 131-byte published digest/XOF.
QOI, zlib, and zstd cases decode a native-encoded reference stream and compare
the exact decompressed bytes: a native encoder (the pinned toolchain or a host
encoder) produced the stream, and the wasm decoder must reproduce the original
deterministic pattern byte-for-byte.

## Guest-owned buffers

The core-ABI runners own their input/output buffers and expose zero-arg pointer
exports (e.g. `blake3_input_ptr`/`blake3_output_ptr`); the harness resolves the
addresses at runtime rather than assuming a linear-memory layout. Placing
host-written buffers at a fixed low offset collides with the guest's default
64 KiB stack (`__stack_pointer` starts at 65536), which corrupts results for
inputs larger than the stack region. Corpus runners must never hardcode
host-visible buffer offsets below the guest stack.

## Running

```sh
go test ./tests/semanticcorpus
```

The manifest gate verifies every artifact digest and copied license without a
toolchain. The execution gate compiles each artifact, instantiates it twice
(fresh instance + second instance from the same compiled module), and requires
both to reproduce the exact oracle. A per-case `timeout_ms` bound prevents a
miscompiled guest from hanging the suite; `InvokeContext` interrupts runaway
native loops.

## Runtime findings

A case whose `known_issue` field is set is **skipped by the execution gate** with
that message rather than silently dropped: the pinned artifact, oracle, and
build script stay checked in, so clearing the field once an issue is fixed
re-enables the case verbatim.

The previously recorded LZ4 JIT discrepancy no longer reproduces. Compression
and decompression are enabled and checked against their exact size and byte
oracles on every supported corpus-test platform.

## Rebuilding an artifact

Artifacts are checked in so the suite needs no toolchain at run time. To verify
one after rebuilding, run its build script; it stages into a temp directory,
compares SHA-256 against the checked-in artifact, and does not overwrite it by
default:

```sh
tests/corpora/coremark/build.sh   # needs wasi-sdk (WASI_SDK=/path/to/wasi-sdk)
```

After reviewing a deliberate port or upstream change, rebuild with `UPDATE=1`
to replace that artifact, then update `MANIFEST.json`'s `artifact_sha256` and
re-record provenance. A default build failing with a digest mismatch is
intentional: it means the checked-in bytes and manifest pin disagree and must
be reviewed, not silently accepted.

## Provenance discipline

Every case records repository, exact revision, revision date, license, and
toolchain version in `MANIFEST.json`; the upstream license is copied into the
corpus directory. Build scripts clone the pinned revision (`.tmp/upstream/`,
git-ignored) rather than building from an unpinned checkout. Wago-owned patches
(the porting layer) are checked in as reviewed source and compiled against the
pinned upstream translation units.

## Status and next tranches

Current cases: CoreMark (self-validating CRC), BLAKE3 (105 published-vector
checks across hash/keyed/derive modes), QOI (exact encode + decode versus a
native reference), LZ4 (compress/decompress round-trip), zlib inflate (exact
decode of a native-encoded stream), and zstd decompress (exact decode of a
native-encoded frame). Planned next, following the same pattern: JSONTestSuite
classification, Stockfish `perft` node counts, and a 6502 emulator running the
Klaus functional test.
WASI-Preview-1 cases are a later tranche gated on the external `wago-org/wasi`
host plugin; the manifest `abi` field is reserved for them (`core` is the only
admitted value today).
