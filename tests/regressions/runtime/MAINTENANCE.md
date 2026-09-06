# Runtime regression corpus maintenance

Use this guide only when you verify, refresh, or update the runtime regression
corpus. The checked-in corpus is in `tests/regressions/runtime` and is pinned by
`PROVENANCE.json`.

| File | Purpose |
|---|---|
| `UPSTREAM_INVENTORY.tsv` | Classifies every upstream `.wast` path and generates `EXCLUSIONS.md`. |
| `MANIFEST.tsv` | Records the port mode for each ported fixture. |
| `RUST_PORTS.tsv` | Records portable Rust test functions and reviewed function-body hashes. |
| `DIRECT_ARTIFACTS.tsv` | Binds direct sources to reviewed binary artifact digests. |

Do not edit generated `EXCLUSIONS.md` directly. Change its source ledger, then
run the refresh workflow.

## Safety Checks

- Every upstream `.wast` path is classified exactly once as ported, excluded, or
  out of scope; stale and newly unclassified paths fail verification.
- Manifest paths are sorted, unique, relative, and one-to-one with `source.wast`.
- Coverage labels are sorted and unique and, with port modes, use closed vocabularies.
- Every fixture has the exact artifact shape required by its mode. Strict
  `commands.json` decoding rejects unknown fields, unsafe names, missing modules,
  duplicates, and orphan modules/files/directories.
- The complete core tree is protected by the path-and-content digest in
  `PROVENANCE.json`.
- Every `wast-json` fixture runs in its own process with a hard deadline and a
  versioned, nonce-bound result protocol. Its module/assertion totals must exactly
  account for the commands in its own JSON; failures and skips are forbidden.
- Direct regressions and workload cases also run in child processes and must emit
  one exact-target, nonce-bound success outcome. Output is retained in bounded
  head/tail buffers, and timeout cancellation kills the complete process group.
- Coverage builds pass Go's coverage data directory into children, preserving
  process isolation without replaying native execution in the parent.
- Trap assertions match Wago `TrapCode` values, not merely the presence of an
  error.
- Canonical commands pin and assert explicit or signals-based bounds rather than
  inheriting an accidental `WAGO_BOUNDS` value. SIMD is required on supported
  corpus targets rather than silently skipped.
- A scheduled stress workflow shuffles and repeats lifecycle tests across
  `GOMAXPROCS` values, runs a conservative optimizer matrix, exercises guard-page
  mode, and fuzzes metadata, normalization, traps, and Emscripten bounds.

## Verify the Checked-In Import

Install the exact WABT version recorded in `PROVENANCE.json`, then provide an
exact Wasmtime checkout:

```sh
make regression-corpus-check \
  REGRESSION_UPSTREAM="$PWD/.tmp/regression-corpus-upstream" \
  WAST2JSON=wast2json
```

This check is byte for byte. Before it reads an upstream source, it verifies the
Wasmtime origin, commit, and clean tracked worktree. It also verifies the WABT
version, complete upstream inventory and sources, Rust-port functions and body
hashes, direct source/artifact bindings, deterministic local adaptations,
generated JSON/Wasm graphs, and the final fixture-tree digest.

On every CI run, WABT is built from the provenance-pinned source. CI verifies
origin, commit, worktree, and submodule state, then runs the same verifier
against a freshly fetched Wasmtime checkout. Workflow actions use full commit
SHAs.

WABT embeds its input path in `commands.json`. A small legacy subset keeps the
historical `testdata/wasmtime/core` path instead of the normal
`testdata/wasmtime/wasm2` path. `legacy_core_source_filenames` explicitly lists
it, so regeneration stays deterministic without trusting generated JSON to pick
its expected path.

`normalized_wabt_json_fixtures` lists fixtures that need a reviewed,
deterministic JSON repair. It currently covers WABT 1.0.41's malformed
multi-result metadata for `winch/use-innermost-frame.wast`. The repair parses the
generated command prefix and keeps its actual lines, action, and trap text. An
unexpected or already-fixed WABT shape fails closed.

## Refresh the Corpus

```sh
make regression-corpus-sync WAST2JSON=wast2json
```

The sync target rejects a Wasmtime checkout with staged or unstaged tracked
changes before it fetches or checks out the pin. It then fetches the pinned
revision, stages a full core-tree copy, replaces unmodified upstream sources,
applies the deterministic Embenchen registration transformation, regenerates
all `wast-json` artifacts and `EXCLUSIONS.md`, and updates the
`PROVENANCE.json` digest.

The prospective tree is validated before replacement. Metadata writes sync
before rename. Parent directories sync after commit. Rollback errors are shown,
and failpoint tests cover each commit stage.

WABT intentionally does not synthesize these direct modes:

- `direct-go` preserves annotations or identity-sensitive assertions replayed in
  Go;
- `direct-invalid` preserves malformed binary encodings;
- `direct-concurrency` preserves thread-command modules replayed with Go
  goroutines.

Both the fixture-tree digest and `DIRECT_ARTIFACTS.tsv` cover their exact bytes.
The importer never overwrites a changed direct source. If a pin update changes
one, review and apply it manually with its modules and Go oracle. Source-only
changes fail when artifact hashes stay unchanged.

## Update the Pin

1. Change the Wasmtime revision/date and—if needed—the WABT version, repository,
   and exact commit in `tests/regressions/runtime/PROVENANCE.json`.
2. Review every addition/removal reported against `UPSTREAM_INVENTORY.tsv`.
   Classify it explicitly, update `MANIFEST.tsv` for newly ported fixtures, and
   never edit generated `EXCLUSIONS.md` directly.
3. Run `make regression-corpus-sync`.
4. Review all source diffs and generated artifact changes.
5. Update the fixed manifest/mode totals only when the reviewed applicability
   inventory changed.
6. Run the focused explicit and guard-page suites:

   ```sh
   WAGO_BOUNDS=explicit go test -count=1 ./src/wago -run '^TestRuntimeRegression'
   WAGO_BOUNDS=signals go test -count=1 -tags wago_guardpage ./src/wago -run '^TestRuntimeRegression'
   ```

Change `WAGO_REGRESSION_TIMEOUT` from its default 20-second child limit only
when measurements show that a target needs more time.

## Stress and Fuzz Maintenance

Run the same matrix used by the scheduled workflow with:

```sh
make regression-stress
```

For a shorter local smoke run, set `WAGO_STRESS_COUNT` and
`WAGO_STRESS_FUZZTIME`, for example:

```sh
WAGO_STRESS_COUNT=2 WAGO_STRESS_FUZZTIME=5s make regression-stress
```

The full run repeats lifecycle, reuse, and resource tests at several
`GOMAXPROCS` values. It shuffles the full suite with conservative optimizer
switches, repeats guard-page execution, and fuzzes path and Rust parsing, WABT
JSON normalization, trap matching, and Emscripten memory ranges.
