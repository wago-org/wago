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
additionally run `make test-guard`. A separate mandatory Linux/amd64 **Core v2
conformance** job installs WABT, initializes the pinned `tests/spec-v2`
submodule, and runs `make spec2`; it is included in the final `CI` aggregate, so
it cannot be replaced by a skipped ordinary wrapper or an informational report.
Darwin/amd64 is a native portability check
for architecture-neutral compiler and encoder packages; wago does not yet
implement its JIT ABI or signal-backed guard pages for that target, so runtime,
corpus, and SIMD execution are deliberately excluded.

Linux/amd64 continues to host architecture-independent lint, TinyGo, coverage,
and binary-size jobs. TinyGo mirrors the native matrix: Linux/amd64 and
Linux/arm64 build, test, and smoke-run the CLI; Darwin/arm64 runs the runtime and
public API suites; Darwin/amd64 runs the same portable compiler/encoder scope as
Big Go. The CI card runs broader WebAssembly 1.0, 2.0, and 3.0 summaries for
visibility; those reports remain informational, while the dedicated exact Core
v2 job is required. The final `CI` aggregation job is the stable
branch-protection check and fails if any required matrix cell or supporting job,
including Core v2 conformance, fails.

Nightly, canary, and tagged release workflows attempt Linux, Darwin, and Windows
CLI builds for both amd64 and arm64, then publish every successful binary with
its SHA-256 checksum. A push to `main` becomes a canary only after that commit's
`CI` workflow succeeds; failed or cancelled CI runs do not publish a canary.
Manual canary runs build the current `main` tip. Nightly and canary use unique
never-retargeted prerelease tags; the CLI resolves the newest `nightly-*` or
`canary-*` tag when a channel is installed. Every target publishes a
standard-Go CLI plus Normal builds of the Standard, Lite, and Minimal runtimes.
Linux also requires Tiny builds of all three profiles; other platforms publish
each feature-complete Tiny profile supported by their TinyGo port. Normal favors
runtime speed; Tiny favors executable size. Windows
uses cross-compilation from a Windows amd64 runner for arm64. Unsupported native JIT targets are
best-effort: a failed target is omitted and does not block the release. Every
channel uses `wago-<goos>-<goarch>` for the manager and
`wago-runtime-<profile>-<build>-<goos>-<goarch>` for runtimes.
Both the matrix job and publisher run `scripts/verify-channel-assets.sh`; an
otherwise successful platform is rejected unless its CLI, all three Normal
runtimes, and their checksums are present and valid. Linux additionally requires
all three Tiny runtimes; optional Tiny assets on other platforms are verified
when present.
When a runtime asset or checksum is omitted, the CLI builds that release tag
from source on the user's host; checksum mismatches still fail closed.

For a local native approximation, run:

```sh
make lint
make test
make test-guard   # only on a supported guard-page target
WAGO_CORPUS_TIMEOUT=20s make test-corpus
make simd
git submodule update --init tests/spec-v2
make spec2        # exact mandatory Linux/amd64 Core v2 gate
```

The public website verification headline and coverage percentage use exactly
five gates: the normal Go suite, guard-page tests, WebAssembly 1.0 execution,
the pinned WebAssembly 2.0 validation plus execution gate, and the standalone
SIMD proposal execution suite. Run `make verify-public` to regenerate
`VERIFICATION.md`, `coverage.out`, and `coverage-report.md`. The verification
headline counts each independently required gate's checks; the coverage report
merges source blocks across all five gates and counts a block covered when any
gate executes it. `VERIFICATION.md` also records the merged coverage percentage
so consumers using a clean checkout do not depend on the ignored detailed
coverage report.
