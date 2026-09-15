# Runtime Corpus

`catalog.json` is the single inventory used by both correctness tests and
benchmarks. Each entry pins the artifact SHA-256. Executable entries also carry
an exact oracle; file-backed commands pin every input file and hash their output.

```text
corpus/
  catalog.json   profiles, checks, benchmark metadata, hashes, and oracles
  workloads/     committed Wasm and application inputs
  sources/       reviewed local WAT, Rust, AssemblyScript, and adapter sources
  build/         rebuild and refresh commands
  PROVENANCE.md  upstream revisions and admission policy
```

Selection is consistent across tests and benchmarks:

```sh
just test corpus                         # quick profile
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
