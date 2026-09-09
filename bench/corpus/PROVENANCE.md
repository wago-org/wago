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

`APPLICATION_SHA256SUMS` additionally locks every application-corpus module
and input. `build-applications.sh` rebuilds into a temporary directory and
refuses any byte difference; it never refreshes the checked-in files in place.

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
| application corpora | `build-applications.sh`, WASI SDK 34, Binaryen 130 | pinned source revisions and byte-for-byte rebuild check |

The manifest is the acceptance inventory. ISA modules are in
`isa-manifest.json` and are opt-in through `BENCH_ISA=1`; `inflate.wasm`,
`bignum.wasm`, `regexmatch.wasm`, and wasm3 artifacts are explicitly marked
`regression-only` instead of being silently omitted.

## Application Corpora

`application-manifest.json` admits 73 fixed workloads. Command-style modules
run in a fresh instance, so `_start` process state is never reused between
samples. `CommandExec` therefore measures instantiate plus one complete command
or replay; compilation remains outside that measurement. Sightglass output is
checked against its upstream oracle before benchmarking. Embench and TACLeBench
retain their built-in verification return values.

| corpus | revision | admitted workloads | adaptation |
| --- | --- | --- | --- |
| PolyBench/C | `5474c59fe88f4e36ba968e8f8c4ac913ee83f0d0` | all 30 kernels, `SMALL_DATASET` | WASI SDK 34, `-O3`, scalar vectorizers disabled, no internal timer or cache flush |
| Embench | `09c2ed8c3b7008c95d08b038de4a3f6dc103ed70` | all 19 programs | fixed scale 1 and verification-preserving WASI driver |
| Sightglass | `9ce88522d75b2d155e358f576e7d88ed26d14de8` | 13 representative applications and kernels | Binaryen 130 links local no-op `bench.start/end`; original inputs and output oracles retained |
| Wasm-R3 | `576f499476060026f6c18cf6edd255ffcbeb5e4f` | Bullet, Parquet, Boa, Sandspiel, SQL GUI, pathfinding | checked-in standalone replay binaries; `_start` invoked directly |
| WABench | `ed7ea81ed6f8f3bbe39972a7c38edecfd7d249b8` | bzip2, Snappy, GNU Chess | WASI SDK 34; bzip2 uses a non-TTY adapter, GNU Chess uses its pinned stdin |
| TACLeBench | `c6a0d73e47bbd2bc86e34637156fb26dd4d5cf08` | bubble sort, binary search | import-free `bench_run() -> i32`, exact zero result required |

Two inspected candidates are intentionally not admitted. The Sightglass Rust
compression module produced a different Brotli oracle under Wago, and the
Wasm-R3 FFmpeg replay exceeded the current ARM64 backend's transient-register
admission limit. Keeping them out prevents a compile-only or incorrect workload
from being labeled runnable.

All 73 admitted artifacts remain in cross-platform decode and compile stages.
Fourteen command executions are currently limited to Darwin/ARM64 in the
manifest: Embench picojpeg fails its verifier on Linux/AMD64; Bullet reaches an
invalid native target and pathfinding does not terminate in the AMD64 backend;
and the file-backed Sightglass workloads plus WABench bzip2 expose Linux/AMD64
WASI or code-generation failures.
Those restrictions keep the Linux suite runnable without hiding the artifacts
from compile coverage, and should be removed individually as their underlying
runtime failures are fixed.
