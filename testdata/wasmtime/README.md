# Wasmtime core regression fixtures

This directory contains portable WebAssembly core tests adapted from Wasmtime's
`tests/misc_testsuite` corpus.

- upstream: `github.com/bytecodealliance/wasmtime`
- revision: `a5720e50d5ec9eab34eed690eee952abfdd0e3ba`
- revision date: 2026-07-24
- generator: WABT `wast2json` 1.0.41 at commit
  `03a00a1334e6121fb0cce4fccbd6bb109b68acaa`
- machine-readable pins and ledgers: `PROVENANCE.json`, `UPSTREAM_INVENTORY.tsv`,
  `DIRECT_ARTIFACTS.tsv`, and `RUST_PORTS.tsv`
- license: Apache License 2.0 with LLVM exceptions; the upstream license is
  preserved in `testdata/wasmtime/LICENSE`

`UPSTREAM_INVENTORY.tsv` classifies all 324 `.wast` files under the pinned
upstream source root as ported, explicitly excluded, or out of scope.
`EXCLUSIONS.md` is generated from it. The importer rejects new, removed, stale,
or multiply classified upstream paths. `MANIFEST.tsv` is the exact port-mode
ledger for the 104 ported entries. Its test gate requires sorted, unique paths,
sorted and unique coverage labels from a closed vocabulary, the exact artifact
shape for each port mode, and a one-to-one mapping between manifest rows and
`source.wast` files. The current port contains:

- 54 feature-focused WebAssembly 2.0 regressions covering sign extension,
  non-trapping conversions, multi-value, reference types, bulk memory, and SIMD;
- 41 general compiler/runtime regressions covering control flow, traps, calls,
  wide signatures, numeric lowering, stack exhaustion, Winch regressions, and
  historical issues;
- 4 Emscripten/Embenchen compile-and-link workloads;
- 1 malformed-binary validation regression;
- 2 concurrent instance-lifecycle and memory-reuse regressions adapted from
  Wasmtime's pooling tests to Wago's runtime model; and
- 2 post-WebAssembly-2 branch-hinting regressions, including Wago's intentional
  strict rejection of malformed structured metadata.

The 97 `wast-json` entries preserve the upstream WAST source and checked-in WABT
1.0.41 JSON/wasm conversion. The three `direct-go` entries use exact Go
assertions where WABT cannot serialize the upstream oracle or annotation. The
two `direct-invalid` entries preserve malformed binaries, and
`direct-concurrency` translates WAST thread commands into Go goroutines while
retaining the original source and exact Wasm modules.

The Embenchen sources carry a prominent modification notice: Wago's standard
WAST replay requires the named `$env` provider to be explicitly registered.
No workload module was otherwise changed. Direct Go execution tests additionally
run all four `_main` exports through a bounded legacy Emscripten host shim and
verify exact output digests. The shim has no generic zero-result fallback:
result-bearing imports fail unless their direct-workload behavior is modeled
explicitly. In particular, `___syscall54` accepts only the known stdout/stderr
`TCGETS` query; new imports or syscall shapes fail closed.

The fixture tree contains 364 upstream or mechanically generated artifacts:
104 WAST sources, 97 JSON command files, and 163 wasm modules. Its replay-order
path-and-content SHA-256 is stored in `PROVENANCE.json`, so the importer and test
harness share one updateable source of truth. `DIRECT_ARTIFACTS.tsv` separately
binds every direct fixture's source hash to its `module.*.wasm` artifact digest;
a changed direct source cannot be blessed while its binaries remain unchanged.

Each `wast-json` fixture executes in a fresh test process with a hard deadline
(`WAGO_WASMTIME_TIMEOUT`, default 20s). The child uses a versioned, nonce-bound
outcome protocol and reports per-fixture module and assertion accounting, which
is checked against the commands in that fixture; crashes, hangs, spoofed or
missing outcomes, failures, and skips are all hard errors. Direct fixture and
workload oracles use the same exact-target success protocol. Child output is
bounded while it is produced, and timeouts kill the complete child process
group. Coverage-enabled child processes write into Go's shared coverage data
directory, so native execution remains isolated instead of being replayed in
the parent. `commands.json` is decoded strictly and must reference exactly the
safe, contiguous `commands.*.wasm` artifact set. The same corpus runs under
explicit bounds and `-tags wago_guardpage`; canonical commands pin and assert
the intended bounds mode rather than inheriting `WAGO_BOUNDS` accidentally.

The Go replay and direct assertions live in
`src/wago/wasmtime_core_port_test.go`. Portable Rust API, compiler, lifecycle,
and trap adaptations are recorded in the machine-readable `RUST_PORTS.tsv`
ledger. Exact scopes extracted from Go documentation must match the ledger, and
each pinned upstream Rust function body has a reviewed SHA-256. The adaptations
are implemented in the other `src/wago/wasmtime_*_port_test.go` files.

## Verify or update

From the repository root, with WABT 1.0.41 available:

```sh
# Clone/fetch the exact Wasmtime revision, then verify sources and generated files.
go run ./scripts/wasmtime-corpus -fetch

# Refresh source.wast and wast-json artifacts, and update the fixture-tree digest.
go run ./scripts/wasmtime-corpus -fetch -write
```

Unmodified sources are compared byte-for-byte with the pinned upstream checkout.
The four Embenchen sources receive one deterministic transformation: insertion
of `(register "env" $env)` plus the required modification notice. `wast-json`
artifacts are regenerated and compared byte-for-byte with the exact pinned WABT
version; the declared JSON repair derives command metadata from WABT's malformed
output rather than hardcoding source line numbers. The complete prospective tree
is validated for orphan files, directories, symlinks, command references, and
mode-specific artifact shapes before replacement. Refreshes are staged, metadata
files are synced before rename, and injected-failure tests cover commit rollback. Direct fixtures
retain their exact checked-in binaries because they contain malformed, thread,
or metadata syntax WABT cannot serialize. If an upstream direct source changes,
the importer refuses to overwrite it: update and review `source.wast`, the
corresponding modules, and `DIRECT_ARTIFACTS.tsv` together.

See `docs/wasmtime-corpus.md` for the maintenance workflow and
`testdata/wasmtime/EXCLUSIONS.md` for the small set of Wasmtime-specific files
that cannot preserve their upstream oracle in Wago.
