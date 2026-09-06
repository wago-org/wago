# Runtime regression corpus

This directory contains portable WebAssembly core tests adapted from Wasmtime's
`tests/misc_testsuite` corpus. Use it to find the exact upstream source,
checked-in artifacts, and replay rules. Use [MAINTENANCE.md](MAINTENANCE.md) to
refresh generated artifacts. Do not edit those artifacts by hand.

## Source and Ledgers

- upstream: `github.com/bytecodealliance/wasmtime`
- revision: `a5720e50d5ec9eab34eed690eee952abfdd0e3ba`
- revision date: 2026-07-24
- generator: WABT `wast2json` 1.0.41 at commit
  `03a00a1334e6121fb0cce4fccbd6bb109b68acaa`
- machine-readable pins and ledgers: `PROVENANCE.json`, `UPSTREAM_INVENTORY.tsv`,
  `DIRECT_ARTIFACTS.tsv`, and `RUST_PORTS.tsv`
- license: Apache License 2.0 with LLVM exceptions; the upstream license is
  preserved in `tests/regressions/runtime/LICENSE`

`UPSTREAM_INVENTORY.tsv` classifies all 324 `.wast` files under the pinned
upstream source root as ported, explicitly excluded, or out of scope.
`EXCLUSIONS.md` is generated from that ledger. The importer rejects new,
removed, stale, and multiply classified paths.

`MANIFEST.tsv` is the port-mode ledger for the 104 ported entries. Its test gate
requires sorted, unique paths; sorted, unique coverage labels from a closed
vocabulary; the exact artifact shape for each port mode; and one manifest row
per `source.wast` file.

## Coverage

The current port contains:

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

## Fixture Modes

- The 97 `wast-json` entries retain upstream WAST source and checked-in WABT
  1.0.41 JSON/wasm conversion.
- The three `direct-go` entries use exact Go assertions where WABT cannot
  serialize the upstream oracle or annotation.
- The two `direct-invalid` entries retain malformed binaries.
- `direct-concurrency` translates WAST thread commands into Go goroutines while
  retaining the original source and exact Wasm modules.

## Emscripten Workloads

The Embenchen sources carry a clear modification notice. Standard WAST replay
requires the named `$env` provider to be registered. No workload module has any
other change.

Direct Go execution tests run all four `_main` exports through a bounded legacy
Emscripten host shim and verify exact output digests. The shim has no generic
zero-result fallback. Result-bearing imports fail unless their direct-workload
behavior is modeled. In particular, `___syscall54` accepts only the known
stdout/stderr `TCGETS` query. New imports or syscall shapes fail closed.

## Artifact Integrity

The fixture tree has 364 upstream or mechanically generated artifacts: 104 WAST
sources, 97 JSON command files, and 163 wasm modules. `PROVENANCE.json` stores
the replay-order path-and-content SHA-256. The importer and test harness use it
as one updateable source of truth.

`DIRECT_ARTIFACTS.tsv` binds each direct fixture's source hash to its
`module.*.wasm` artifact digest. A changed direct source cannot be accepted
while its binary stays unchanged.

## Safe, Isolated Replay

Each `wast-json` fixture runs in a new test process. The hard deadline is
`WAGO_REGRESSION_TIMEOUT`, which defaults to 20 seconds. The child uses a
versioned, nonce-bound outcome protocol. It reports its module and assertion
counts, which are checked against that fixture's commands. Crashes, hangs,
spoofed or missing outcomes, failures, and skips are all hard errors.

Direct fixtures and workloads use the same exact-target success protocol. Child
output stays bounded as it is written. A timeout kills the complete child
process group. Coverage-enabled children write to Go's shared coverage data
directory. Native execution therefore stays isolated instead of running again
in the parent.

`commands.json` is decoded strictly. It must reference exactly the safe,
contiguous `commands.*.wasm` artifact set. The same corpus runs with explicit
bounds and with `-tags wago_guardpage`. Canonical commands pin and check the
intended bounds mode rather than accidentally inheriting `WAGO_BOUNDS`.

## Test Implementation

The Go replay and direct assertions are in `src/wago/regression_core_test.go`.
`RUST_PORTS.tsv` records portable Rust API, compiler, lifecycle, and trap
adaptations. Exact scopes from Go documentation must match that ledger. Each
pinned upstream Rust function body has a reviewed SHA-256. The other
`src/wago/regression_*_test.go` files implement the adaptations.

## Verify or Update

From the repository root, with WABT 1.0.41 available:

```sh
# Clone/fetch the exact upstream revision, then verify sources and generated files.
go run ./tests/tools/regression-corpus -fetch

# Refresh source.wast and wast-json artifacts, and update the fixture-tree digest.
go run ./tests/tools/regression-corpus -fetch -write
```

Unmodified sources are compared byte for byte with the pinned upstream checkout.
The four Embenchen sources receive one deterministic transformation: insert
`(register "env" $env)` and the required modification notice. `wast-json`
artifacts are regenerated and compared byte for byte with the pinned WABT
version. The declared JSON repair derives command metadata from malformed WABT
output; it does not hardcode source line numbers.

Before replacement, the prospective tree is checked for orphan files,
directories, symlinks, command references, and mode-specific artifact shapes.
Refreshes are staged. Metadata files sync before rename. Injected-failure tests
cover rollback.

Direct fixtures retain their checked-in binaries because WABT cannot serialize
their malformed, thread, or metadata syntax. If an upstream direct source
changes, the importer will not overwrite it. Update and review `source.wast`,
the matching modules, and `DIRECT_ARTIFACTS.tsv` together.

See `tests/regressions/runtime/MAINTENANCE.md` for the maintenance workflow and
`tests/regressions/runtime/EXCLUSIONS.md` for the small set of source-specific
files that cannot preserve their upstream oracle in Wago.
