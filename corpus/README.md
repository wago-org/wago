# Runtime Corpus

`catalog.json` is the single inventory used by both correctness tests and
benchmarks. Each entry pins the artifact SHA-256. Executable entries also carry
an exact oracle; file-backed commands pin every input file and hash their output.

`candidates.json` tracks requested full applications that are **not yet
admitted**. It is an acquisition queue, not a claim that the listed programs
run in Wago. Promote a candidate to `catalog.json` only after pinning its
artifact, imports, host contract, input files, limits, and independent output
oracle, and passing both Wago and reference-runtime execution. The a-Shell
`sqlite3.wasm` probe, for example, imports `ashell_system`, `ashell_chdir`, and
`ashell_getcwd`; the current WASI command harness does not provide these. The
[XZ reproducer](repro/xz/README.md) records a second, distinct a-Shell host
incompatibility.

The admitted full commands are esbuild, QuickJS, Duktape, SQLite,
swift-format, and xzdec.
The legacy LZMA decoder `lzmadec` and header inspector `lzmainfo` are
admitted alongside `xzdec`.
The `age` and `age-keygen` commands use a public, disposable test identity and
check deterministic decryption and recipient derivation; no real secret is
stored in the corpus.
The `jq` CLI is also admitted with an exact thousand-object JSON transformation.
The Brotli CLI is admitted for both compression and decompression of a pinned
JavaScript fixture.
The upstream `tree` CLI is admitted for a recursive preopened-directory listing.
The a-Shell tree 1.8.0 artifact has a separate workload using its bounded host
imports; a-Shell Ctags still has a [filesystem blocker](repro/ctags/README.md).
The a-Shell `json2csv` artifact converts pinned newline-delimited JSON into
an exact CSV stream.
YoWASP `icepll`, `icebram`, `icepack`, `iceunpack`, `icemulti`, `ecppll`, and
`ecpbram` are admitted with seeded calculations, exact bitstream round trips,
and exact generated-file checks. File-producing commands run against a fresh
temporary copy of their preopen; tests and benchmarks never write generated
files into the committed corpus.
The newer YoWASP 0.11.1 binaries for several tools exceed Wago's current
bounded-exception-handling limit, so the compatible 0.5.0 release is pinned
for those workloads.
The `ashell` corpus adapter is deliberately narrow: `ashell_getcwd` returns
the guest root `/`, `ashell_getenv` reports no environment value, and
`ashell_chdir`/`ashell_system` return `ENOSYS`. It never exposes the host
working directory or launches a host command. Other a-Shell binaries still
need their own import and filesystem audit before admission.
These are execution-tested fixtures, not a claim that the rest of the acquisition
queue runs. The Swift formatter uses a preopened source file because its stdin
path currently fails in Wago's WASI host with a bad descriptor. a-Shell
`funzip` similarly rejects Wago's character-device stdin; a filename does not
work with that artifact's host contract either.
The rebuilt upstream xzdec CLI does execute its decoder through Wago; the
full XZ CLI remains pending, as does the incompatible a-Shell xz binary.
The [SQLite reproducer](repro/sqlite3/README.md) records a file-backed query
that still traps in Wago despite passing Wasmtime and wazero; the admitted SQL
fixture uses a separate in-memory workload.

```text
corpus/
  catalog.json   profiles, checks, benchmark metadata, hashes, and oracles
  candidates.json  full-application acquisition queue (not executable)
  workloads/     committed Wasm and application inputs
  sources/       reviewed local WAT, Rust, AssemblyScript, and adapter sources
  build/         rebuild and refresh commands
  PROVENANCE.md  upstream revisions and admission policy
```

Selection is consistent across tests and benchmarks:

```sh
just test corpus                         # quick profile when selected directly
just test                                # all benchmark corpus workloads + unit/integration tests
just test corpus algorithms              # representative raw algorithms
just test corpus tag:polybench           # all 30 PolyBench/C kernels
just test corpus tag:application
just bench check tiny,coremark
just bench run all                       # complete benchmark inventory
just test regression build-polybench /opt/wasi-sdk
```

To admit a workload, add its artifact, provenance, digest, and execution oracle
to `catalog.json`, then run `just test corpus <id>` and
`just bench check <id>`. Every benchmark entry must declare exactly one
end-to-end contract: direct invocation with exact results, semantic execution
with an exact return/memory/vector oracle, or a command with a self-checking
exit status or exact output hashes. Command workloads may declare an explicit
`platforms` allowlist when their host adapter is not portable. Missing
artifacts, unknown selectors, duplicate IDs, digest mismatches, compile-only
entries, and execution without an oracle fail closed.

The public `just test` gate selects `all`, so every workload admitted to the
benchmark inventory is also compiled and executed by correctness testing. Set
`CORPUS=quick` only when a shorter local iteration is intentional.

The corpus recipe also runs catalog contract and semantic provenance tests from
the separate `bench` Go module. Ordinary Windows CI selects `all`; unsupported
WASI command adapters are explicit skips with the platform allowlist as the
reason. Linux and supported macOS runtime jobs use the same all-corpus default.

Bounds modes are separate gates. `just test guard all` checks guard-page traps
and direct corpus execution with signals-based bounds checks. It does not run
semantic or command workloads in guard mode; ordinary corpus success does not
establish complete guard-mode coverage. NanoSVG rendering remains excluded;
see the [reproducer](repro/nanosvg/README.md). Wren floating modulo is covered by
an exact floating-point bit oracle in addition to its existing VM workload.
