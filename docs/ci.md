# Continuous integration

Pull requests and pushes to `main` run `.github/workflows/ci.yml`. Markdown-only
changes use the lightweight aggregation gate; every code or build change runs
the complete native platform matrix:

| Runner | OS | Architecture | Standard suite | Guard pages | Corpus | SIMD |
|---|---|---|---:|---:|---:|---:|
| `ubuntu-24.04` | Linux | amd64 | yes | yes | yes | yes |
| `ubuntu-24.04-arm` | Linux | arm64 | yes | yes | yes | yes |
| `macos-15-intel` | Darwin | amd64 | portable compiler/encoder | no | no | no |
| `macos-15` | Darwin | arm64 | yes | yes | yes | yes |

Each matrix cell asserts `go env GOOS` and `GOARCH` before testing. WABT is
installed explicitly so tests that need `wat2wasm` do not silently skip because
the runner image lacks the tool.

The three supported runtime targets run `make test`, which builds and tests every
Go package, followed by `make test-corpus` with a bounded per-case timeout and
`make simd` against the official SIMD proposal corpus. Their guard-page cells
additionally run `make test-guard`. Darwin/amd64 is a native portability check
for architecture-neutral compiler and encoder packages; wago does not yet
implement its JIT ABI or signal-backed guard pages for that target, so runtime,
corpus, and SIMD execution are deliberately excluded.

Linux/amd64 continues to host architecture-independent lint, TinyGo, coverage,
and binary-size jobs. The CI card runs the WebAssembly 1.0, 2.0, and 3.0 suites
for visibility without making their current gaps required checks. The final
`CI` aggregation job is the stable branch-protection check and fails if any
required matrix cell or supporting job fails.

For a local native approximation, run:

```sh
make lint
make test
make test-guard   # only on a supported guard-page target
WAGO_CORPUS_TIMEOUT=20s make test-corpus
make simd
```
