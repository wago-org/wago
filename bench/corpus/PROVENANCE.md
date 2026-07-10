# Corpus provenance lock

The committed wasm corpus is locked at the SHA-256 values below. Rebuild
scripts must write into a temporary directory and compare these values; they
must never overwrite a checked-in artifact in place.

The synthetic modules (`src/*.wat`) are reproducible with `wabt` pinned by CI;
the Rust modules are reproducible with the pinned `wasm32-wasip1` toolchain;
AssemblyScript and third-party binaries are fetched/build artifacts and are
regression-only unless their manifest entry declares an executable export.

Run `shasum -a 256 bench/corpus/*.wasm bench/corpus/vendor/*.wasm` and compare
the output at the review commit. This lock intentionally records the source
classification in the manifest so unlisted artifacts cannot silently vanish
from acceptance.

| artifact class | source/tool | reproducibility |
| --- | --- | --- |
| synthetic | `bench/corpus/build.sh`, pinned wabt | reproducible |
| Rust compute/WASI | `build-rust.sh`, `rust-wasi/build.sh`, pinned Rust target | reproducible with toolchain |
| AssemblyScript | `build-as.sh`, reviewed source revision | fetched/build; revision required before refresh |
| third-party engines | `fetch.sh` or reviewed regression source | fetched/regression-only |

The manifest is the acceptance inventory. ISA modules are in
`isa-manifest.json` and are opt-in through `BENCH_ISA=1`; `inflate.wasm`,
`bignum.wasm`, `regexmatch.wasm`, and wasm3 artifacts are explicitly marked
`regression-only` instead of being silently omitted.
