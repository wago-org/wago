# WebAssembly specification testing

Wago keeps separate official corpora for the WebAssembly 1.0 baseline and the
WebAssembly 2.0 closeout gate. Do not repoint one checkout to cover both releases:
the 1.0 certification counts in `SPECTEST.md` must remain reproducible while 2.0
support is completed.

## Pinned corpora

| Release | Repository and revision | Local path | Core corpus |
|---|---|---|---|
| WebAssembly 1.0 baseline | `WebAssembly/testsuite` at `a8bcbafe6d2fb191ce0188de0e18fdc107fa2598` | `tests/spec` | checkout root |
| WebAssembly 2.0 | `WebAssembly/spec` tag `v2.0.0`, commit `05ca4182176763112561ae20153975c12bd689e4` | `tests/spec-v2` | `test/core` |

The Release 2 source is the specification repository because that tagged tree
contains the official release's complete core tests, including the nested
`test/core/simd` files. The old `WebAssembly/testsuite` checkout is a preserved
pre-reference-types baseline and its proposal directories are not a substitute
for a tagged Release 2 core corpus.

Initialize only the suite needed for a focused run:

```sh
git submodule update --init tests/spec       # WebAssembly 1.0
git submodule update --init tests/spec-v2    # WebAssembly 2.0
```

## Commands

Install WABT so `wast2json` is on `PATH`, then run:

```sh
make spec1
make spec2
```

`make spec2` sets `WAGO_SPECTEST_DIR` to the `tests/spec-v2` checkout and
`WAGO_SPEC_VERSION=2.0`; the execution harness resolves the tagged repository's
`test/core` layout. CI provisions WABT, initializes only `tests/spec-v2`, and
runs this same target in the `WebAssembly 2.0 spec` job.

The validation harness uses the same release discovery and can be run directly:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 -run '^TestSpecSuite$' -v ./src/core/compiler/wasm
```

The CI-card renderer can also consume captured suite logs through
`SPEC_LOG_DIR`; this keeps report parsing testable without rerunning WABT. Run
its committed synthetic fixture with:

```sh
scripts/spec-card_test.sh
```

The files under `scripts/testdata/spec-card` are parser fixtures, not published
conformance counts. Real support claims must come from a fresh WABT-backed run.

Both harnesses print per-file and total module/assertion pass, fail, and skip
counts. The execution totals also print a fixed, bounded reason vector:
`compile-rejected`, `instantiate-rejected`, `module-unavailable`,
`absent-export`, `reference-argument`, `reference-result`, and
`reference-global`. Reference-valued `get` assertions are counted as
`reference-global` rather than the broader result category. Unknown action/value
shapes are harness failures, not skips.

A missing/empty Release 2 checkout, a discovered file that disappears, or a
`wast2json` conversion failure is an error rather than a silent empty run.
During closeout, known unsupported modules and reference-valued assertions remain
reasoned skips rather than being treated as support. WebAssembly 2.0 completion
requires every feature-related reason count to reach zero; do not weaken
valid-module rejection, invalid-module acceptance, or missing-corpus failures
into silent skips.
