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
make test-corpus                         # quick profile
make test-corpus CORPUS=tag:application
make bench-check CORPUS=tiny,coremark
make bench-all                           # complete benchmark inventory
```

To admit a workload, add its artifact, provenance, digest, and execution oracle
to `catalog.json`, then run `make test-corpus CORPUS=<id>` and
`make bench-check CORPUS=<id>`. Compile-only workloads must declare stages which
exclude execution. Command workloads may declare an explicit `platforms`
allowlist when their host adapter is not portable. Missing artifacts, unknown
selectors, duplicate IDs, digest mismatches, and executable entries without an
oracle fail closed.
