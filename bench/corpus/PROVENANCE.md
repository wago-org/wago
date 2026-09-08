# Benchmark Corpus Provenance

Use this file when you review or refresh a benchmark artifact. The committed
wasm corpus is locked to the SHA-256 values below. Rebuild scripts must write to
a temporary directory, then compare the new values. They must not overwrite a
checked-in artifact in place.

Run this command from the repository root and compare its output at the review
commit:

```bash
shasum -a 256 bench/corpus/*.wasm bench/corpus/vendor/*.wasm
```

The manifest records each artifact's source class. This makes an unlisted
artifact unable to disappear silently from acceptance.

## Rebuild Rules

Synthetic modules in `src/*.wat` are reproducible with the WABT version pinned
by CI. Rust modules are reproducible with the pinned `wasm32-wasip1` toolchain.
AssemblyScript and third-party binaries are fetched or built artifacts. They are
regression-only unless their manifest entry declares an executable export.

## Artifact Classes

| artifact class | source/tool | reproducibility |
| --- | --- | --- |
| synthetic | `bench/corpus/build.sh`, pinned wabt | reproducible |
| Rust compute/WASI | `build-rust.sh`, `rust-wasi/build.sh`, pinned Rust target | reproducible with toolchain |
| AssemblyScript | `build-as.sh`, reviewed source revision | fetched/build; revision required before refresh |
| focused AssemblyScript idioms | `build-as.sh`, checked-in `as/*.ts` source | reproducible with the selected asc toolchain |
| semantic programs | `tests/corpora/MANIFEST.json`, pinned source and artifact digests | referenced in place; exact execution oracles remain authoritative |
| third-party engines | `fetch.sh` or reviewed regression source | fetched/regression-only |

The manifest is the acceptance inventory. ISA modules are in
`isa-manifest.json` and are opt-in through `BENCH_ISA=1`; `inflate.wasm`,
`bignum.wasm`, `regexmatch.wasm`, and wasm3 artifacts are explicitly marked
`regression-only` instead of being silently omitted.
