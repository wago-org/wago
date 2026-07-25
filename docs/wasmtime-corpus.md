# Wasmtime corpus maintenance

The checked-in Wasmtime regression corpus lives under `testdata/wasmtime` and is
pinned by `PROVENANCE.json`. `UPSTREAM_INVENTORY.tsv` classifies every upstream
`.wast` path and generates `EXCLUSIONS.md`; `MANIFEST.tsv` is the port-mode ledger
for the classified-as-ported subset. `RUST_PORTS.tsv` records exact portable Rust
test functions plus reviewed function-body hashes, and `DIRECT_ARTIFACTS.tsv`
binds direct sources to their reviewed binary artifact digests.

## Correctness gates

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

## Verify the checked-in import

Install the exact WABT version recorded in `PROVENANCE.json`, then provide an
exact Wasmtime checkout:

```sh
make wasmtime-corpus-check \
  WASMTIME_CHECKOUT="$PWD/.tmp/wasmtime-corpus-upstream" \
  WAST2JSON=wast2json
```

The check is byte-for-byte. It verifies the Wasmtime commit, exact WABT version,
the complete upstream inventory, upstream sources, exact Rust-port functions and
body hashes, direct source/artifact bindings, deterministic local adaptations,
strict generated JSON/Wasm graphs, and final fixture-tree digest. CI checks out
and builds the WABT commit recorded in provenance from fresh source on every run,
verifies origin/commit/worktree/submodule state, and runs the same verifier
against a freshly fetched Wasmtime checkout. Workflow actions are pinned by full
commit SHA. WABT embeds its input path in each `commands.json`; the small legacy
subset generated from `testdata/wasmtime/core` instead of the normal
`testdata/wasmtime/wasm2` layout is explicitly listed in
`legacy_core_source_filenames` so regeneration remains deterministic without
trusting the generated JSON to choose its own expected path. The
`normalized_wabt_json_fixtures` list separately declares fixtures requiring a
reviewed deterministic JSON repair; currently this covers WABT 1.0.41's
malformed multi-result metadata for `winch/use-innermost-frame.wast`. The repair
parses the generated command prefix and preserves its actual lines, action, and
trap text; an unexpected or already-fixed WABT shape fails closed.

## Refresh

```sh
make wasmtime-corpus-sync WAST2JSON=wast2json
```

The sync target fetches the pinned revision, stages a complete copy of the core
tree, replaces unmodified sources from upstream, applies the deterministic
Embenchen registration transformation, regenerates all `wast-json` artifacts,
regenerates `EXCLUSIONS.md`, and updates the digest in `PROVENANCE.json`. The
prospective tree is fully validated before replacement. Metadata writes are
synced before rename; parent directories are synced after commit; rollback errors
are surfaced; and failpoint tests cover each commit stage.

Direct modes are intentionally not synthesized by WABT:

- `direct-go` preserves annotations or identity-sensitive assertions replayed in
  Go;
- `direct-invalid` preserves malformed binary encodings;
- `direct-concurrency` preserves thread-command modules replayed with Go
  goroutines.

Their exact bytes remain covered by both the fixture-tree digest and
`DIRECT_ARTIFACTS.tsv`. The importer never overwrites a changed direct source. A
pin update that changes one must be reviewed and applied manually together with
its modules and Go oracle; source-only changes are rejected when artifact hashes
remain unchanged.

## Updating the pin

1. Change the Wasmtime revision/date and—if needed—the WABT version, repository,
   and exact commit in `testdata/wasmtime/PROVENANCE.json`.
2. Review every addition/removal reported against `UPSTREAM_INVENTORY.tsv`.
   Classify it explicitly, update `MANIFEST.tsv` for newly ported fixtures, and
   never edit generated `EXCLUSIONS.md` directly.
3. Run `make wasmtime-corpus-sync`.
4. Review all source diffs and generated artifact changes.
5. Update the fixed manifest/mode totals only when the reviewed applicability
   inventory changed.
6. Run the focused explicit and guard-page suites:

   ```sh
   WAGO_BOUNDS=explicit go test -count=1 ./src/wago -run '^TestWasmtime'
   WAGO_BOUNDS=signals go test -count=1 -tags wago_guardpage ./src/wago -run '^TestWasmtime'
   ```

Use `WAGO_WASMTIME_TIMEOUT` to adjust the default 20-second per-fixture child
limit only when a measured slow target requires it.

## Stress and fuzz maintenance

Run the same matrix used by the scheduled workflow with:

```sh
make wasmtime-stress
```

For a shorter local smoke run, set `WAGO_STRESS_COUNT` and
`WAGO_STRESS_FUZZTIME`, for example:

```sh
WAGO_STRESS_COUNT=2 WAGO_STRESS_FUZZTIME=5s make wasmtime-stress
```

The canonical run repeats lifecycle/reuse/resource tests across several
`GOMAXPROCS` values, shuffles the complete suite under conservative optimizer
switches, repeats guard-page execution, and fuzzes path/Rust parsing, WABT JSON
normalization, trap matching, and Emscripten memory ranges.
