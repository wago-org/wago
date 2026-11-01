# Release 3 conformance toolchain qualification

**GAP-CONFORMANCE-TOOLCHAIN is closed for the pinned baseline and port snapshots.**
Both rebuilt interpreters pass `scripts/bootstrap-spec-interpreter.sh --verify`.
All 18 previously blocked staged cases pass on both snapshots in all three modes.

| Snapshot | Mode | Converter | Harness | Staged | Failures/skips |
| --- | --- | ---: | ---: | ---: | ---: |
| Port a3c08365f | ordinary | 3 | 4 | 168 | 0/0 |
| Port a3c08365f | wagodebug | 3 | 4 | 168 | 0/0 |
| Port a3c08365f | wago_regalloccheck | 3 | 4 | 168 | 0/0 |
| Baseline 9713878ab | ordinary | 3 | 4 | 168 | 0/0 |
| Baseline 9713878ab | wagodebug | 3 | 4 | 168 | 0/0 |
| Baseline 9713878ab | wago_regalloccheck | 3 | 4 | 168 | 0/0 |

Counts are top-level tests; no nested failures or skips occurred either.
`results.txt` records all 18 command exits; `summary.json` maps every previously
blocked case to PASS for each of the six staged runs. No selected test remains
blocked or failing. The full-root and concurrent-runtime limitations below remain.

Scope: close the missing-interpreter dependency gap from Stage 2 using immutable
exports. This is correctness/toolchain evidence, not performance evidence.

Pinned inputs:

- Port: `a3c08365f531eb3c9003e3acf3e0a822c12c31f4`.
- Original qualification baseline: `9713878ab452848f7eb7549a90be94b938a898be`.
- Official spec: `9d36019973201a19f9c9ebb0f10828b2fe2374aa`.
- WABT: 1.0.41 (checked by the existing checksummed bootstrap).
- Native interpreter prerequisites: opam 2.6.1, OCaml 4.14.3, dune 3.22.1,
  menhir 20260209 (including its matching libraries).
- Go: 1.27.1, with explicit matching GOROOT; GOMAXPROCS=2, GOFLAGS=-p=2.

The pinned interpreter requires dune language 2.9, OCaml >=4.14 and menhir
>=20220210, from its `interpreter/dune-project`. Its native `wasm.exe` target
does not require the optional js_of_ocaml JavaScript target dependencies.

## Reproduction

From `/home/dev/.t3/worktrees/wago/experiment-rfwgc-port`:

```sh
E=docs/rfwgc-evidence/stage3-toolchain
bash "$E/bootstrap-local.sh"
# Existing exports are retained for reuse. Use new names to rebuild from scratch.
bash "$E/prepare-export.sh" a3c08365f531eb3c9003e3acf3e0a822c12c31f4 .tools/stage3-toolchain/port-new
bash "$E/run-tests.sh" .tools/stage3-toolchain/port-new "$E" port-new
bash "$E/prepare-export.sh" 9713878ab452848f7eb7549a90be94b938a898be .tools/stage3-toolchain/baseline-new
bash "$E/run-tests.sh" .tools/stage3-toolchain/baseline-new "$E" baseline-new
python3 "$E/summarize.py"
```

`prepare-export.sh` clones the local spec object repository and checks out the
exact pin **only in the private export**. Its optional third argument accepts
`https://github.com/WebAssembly/spec` if no local source exists. No original
fixture checkout is initialized, checked out, cleaned or built in place.

The local opam root is `/home/dev/.local/share/wago-rfwgc-opam`, switch `rfwgc`.
`bootstrap-local.sh` neither changes shell setup nor installs system packages.
`OPAM_BIN` and `WAGO_OCAML_ROOT` can override those user-local locations.
Package recipes retained here include upstream archive checksums. The full
opam switch export additionally records dependency metadata for reconstruction.

Interpreter and WABT reusable paths are under each private export's `.tools/`:

- `.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm`
- `.tools/wabt-1.0.41-linux-x64/bin/wast2json`

The run script wires **both** interpreter environment variables, as required by
the existing harness, and places pinned WABT first on PATH. It explicitly unsets
`WAGO_UPDATE_STAGED_SPEC`; no expected accounting is regenerated.

## Scope and limitations

The selected suites are Release 3 converter/bootstrap checks, runtime harness
checks, and all `^TestStaged` runtime tests, in ordinary, wagodebug and
wago_regalloccheck modes, sequentially on each export. They do not constitute a
full root suite, all upstream spec cases, other architectures, or qualification
of concurrent Stage 3 runtime edits. Recorded staged gate accounting is not a
claim of support for all gated proposal behavior.

The first harness preflight failed before tests because inherited GOROOT pointed
to 1.27.3 while the command invoked Go 1.27.1. Its log is retained as
`go-environment-preflight-failure.log`; explicitly setting matching GOROOT fixed
the environment. No runtime/harness source change was required for that failure.

Only evidence and reproduction files are changed. These files are not compiled
into shipped Wago runtime/compiler artifacts, so this change cannot affect
shipped runtime performance. No benchmarks or performance measurements were run;
ordinary Go test logs contain their normal built-in duration annotations, which
are not analyzed as measurements.

The initial provisioning commands were `opam init --bare --disable-sandboxing
--no-setup -y`, `opam switch create rfwgc ocaml-base-compiler.4.14.3 --jobs=2 -y`,
and `opam install --switch=rfwgc --jobs=2 -y dune.3.22.1 menhir.20260209`, all
with the user-local OPAMROOT above. Their separate logs are retained.
`bootstrap-local.sh` now imports `toolchain.export` (captured with
`opam switch export --switch=rfwgc --full`) to preserve those package recipes;
its successful no-op reuse verification is `bootstrap-reuse.log`.

Both exports build their own interpreter and pass bootstrap provenance checks.
Binary hashes differ across export paths (native builds can embed source paths);
this is a reproducible dependency/build procedure, not a bit-identical binary
claim. Per-export binary hashes, recipe hashes and tool versions are recorded in
`*-interpreter-provenance.txt`.


## Integrity and handoff

`identity-before.txt` and `*-identity-after.txt` verify all 7,898 port and 7,374
baseline tracked blobs against Git, before and after testing. The original
spec-v1/spec-v2 dirt and uninitialized spec-v3 state match in
`fixtures-before.txt` and `fixtures-after.txt`. The pre-existing corpus scripts
retain their checksums; this worker did not edit the dirty port log or any runtime
files. Concurrent runtime changes in the worktree are deliberately outside these
immutable-export results and need fresh qualification by the owning worker.

The first port driver completed all nine test commands successfully, then hit a
shell read-offset error after its script was edited while running. The incident
and first command exits are retained in `port-first-driver-note.txt` and
`port-first-results.txt`. The complete port matrix was rerun with the finalized
driver and exited 0 (`port-driver.log`); baseline driver also exited 0. The final
raw case logs and summary refer to those complete successful runs.

Read-only sibling cross-check: Slipstream `origin/master` at
`506b08d3d3fa3c8e8b1ce9ac0cb4f30a2463cdc3`, read with `git show`, uses
`tools/conformance.py` with WABT and its pinned spec 2.0 corpus. Starshine local
`origin/master` at `e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1` in
`/home/dev/Projects/starshine-master-base` was inspected through Git objects;
`tests/spec/extract-parts.sh` uses WABT, while `tests/spec/update-testsuite.py`
maintains upstream/proposal fixtures. Those inspected paths do not supply Wago's
Release 3 OCaml fallback. No sibling checkout was modified or executed, no
runtime defect was identified for triage, and no sibling performance conclusion
is drawn. This is a scoped toolchain cross-check, not a sibling correctness audit.

No runtime/compiler change, system package installation, expectation update,
subdelegation, PR or push was performed. User-local tools and private exports
are intentionally retained for the owning thread's next qualification run.
