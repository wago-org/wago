# Wasmtime corpus maintenance

The checked-in Wasmtime regression corpus lives under `testdata/wasmtime` and is
pinned by `PROVENANCE.json`. `MANIFEST.tsv` is the core-fixture applicability and
port-mode ledger; `RUST_PORTS.tsv` records the portable Rust API/compiler/runtime
tests adapted into Go.

## Correctness gates

- Manifest paths are sorted, unique, relative, and one-to-one with `source.wast`.
- Coverage labels and port modes use a closed vocabulary.
- Every fixture has the artifact shape required by its mode.
- The complete core tree is protected by the path-and-content digest in
  `PROVENANCE.json`.
- Every `wast-json` fixture runs in its own process with a hard deadline. Its
  module/assertion totals must exactly account for the commands in its own JSON;
  failures and skips are forbidden.
- Trap assertions match Wago `TrapCode` values, not merely the presence of an
  error.
- The corpus runs in both explicit and `wago_guardpage` builds on supported
  native targets.

## Verify the checked-in import

Install the exact WABT version recorded in `PROVENANCE.json`, then provide an
exact Wasmtime checkout:

```sh
make wasmtime-corpus-check \
  WASMTIME_CHECKOUT="$PWD/.tmp/wasmtime-corpus-upstream" \
  WAST2JSON=wast2json
```

The check is byte-for-byte. It verifies the Wasmtime commit, WABT version,
upstream sources, Rust-port scopes, deterministic local adaptations, generated
JSON/Wasm files, and final fixture-tree digest. CI builds WABT 1.0.41 from its
official tag and runs the same verifier against a freshly fetched Wasmtime
checkout. WABT embeds its input path in each `commands.json`; the small legacy
subset generated from `testdata/wasmtime/core` instead of the normal
`testdata/wasmtime/wasm2` layout is explicitly listed in
`legacy_core_source_filenames` so regeneration remains deterministic without
trusting the generated JSON to choose its own expected path. The
`normalized_wabt_json_fixtures` list separately declares fixtures requiring a
reviewed deterministic JSON repair; currently this covers WABT 1.0.41's
malformed multi-result metadata for `winch/use-innermost-frame.wast`.

## Refresh

```sh
make wasmtime-corpus-sync WAST2JSON=wast2json
```

The sync target fetches the pinned revision, replaces unmodified sources from
upstream, applies the deterministic Embenchen registration transformation,
regenerates all `wast-json` artifacts, and updates the digest in
`PROVENANCE.json`.

Direct modes are intentionally not synthesized by WABT:

- `direct-go` preserves annotations or identity-sensitive assertions replayed in
  Go;
- `direct-invalid` preserves malformed binary encodings;
- `direct-concurrency` preserves thread-command modules replayed with Go
  goroutines.

Their exact bytes remain covered by the fixture-tree digest. Any direct binary
change must be reviewed together with its `source.wast` and Go oracle.

## Updating the pin

1. Change the revision, date, and—if needed—WABT version in
   `testdata/wasmtime/PROVENANCE.json`.
2. Review upstream `tests/misc_testsuite` additions and removals. Update
   `MANIFEST.tsv` and `EXCLUSIONS.md` explicitly; never silently ignore a new
   applicable fixture.
3. Run `make wasmtime-corpus-sync`.
4. Review all source diffs and generated artifact changes.
5. Update the fixed manifest/mode totals only when the reviewed applicability
   inventory changed.
6. Run the focused explicit and guard-page suites:

   ```sh
   go test -count=1 ./src/wago -run '^TestWasmtime'
   go test -count=1 -tags wago_guardpage ./src/wago -run '^TestWasmtime'
   ```

Use `WAGO_WASMTIME_TIMEOUT` to adjust the default 20-second per-fixture child
limit only when a measured slow target requires it.
