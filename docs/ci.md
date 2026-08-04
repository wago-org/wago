# Continuous integration

Pull requests and pushes to `main` run `.github/workflows/ci.yml`. Markdown-only
changes use the lightweight aggregation gate; every code or build change runs
the complete native platform matrix:

| Runner | OS | Architecture | Standard suite | Guard pages | Corpus | SIMD |
|---|---|---|---:|---:|---:|---:|
| `ubuntu-24.04` | Linux | amd64 | yes | yes | yes | yes |
| `ubuntu-24.04-arm` | Linux | arm64 | yes | yes | yes | yes |
| `macos-15-intel` | Darwin | amd64 | yes | yes | yes | yes |
| `macos-15` | Darwin | arm64 | yes | yes | yes | yes |
| `windows-2025` | Windows | amd64 | yes | yes | yes | yes |
| `windows-11-arm` | Windows | arm64 | yes | yes | yes | yes |

Each matrix cell asserts `go env GOOS` and `GOARCH` before testing. Unix runtime
jobs bootstrap checksum-pinned WABT 1.0.41 and add its complete `bin` directory
to `PATH`, so `wast2json` and `wat2wasm` do not depend on older runner packages.
Linux and Darwin/arm64 use upstream binary archives. Because upstream publishes
no Darwin/amd64 binary for 1.0.41, that runner builds `wast2json` from the
checksum-pinned release source with CMake. Windows downloads the checksum-pinned
official WABT archive because the project does not publish a Chocolatey package;
Windows 11 ARM runs that x64 tool through its application emulation layer.
Linux/amd64 and coverage
additionally initialize the pinned `tests/spec-v3` submodule, build the
interpreter from that exact checkout, and export its path and revision. The
focused Linux/amd64 race lane initializes the same submodule without building
the conversion tools: supplementary tests may then skip for an unavailable
tool, but corpus discovery cannot obscure a real race with a missing-checkout
failure. This is the authoritative fallback for exception-handling source forms
that WABT 1.0.41 cannot parse; other matrix cells avoid the large OCaml setup.

The six supported runtime targets build and test every Go package, including the
integrated regression corpus, followed by the corpus matrix with a bounded
per-case timeout and the official SIMD proposal corpus. Their guard-page cells
additionally run `make test-guard`. A separate mandatory Linux amd64/arm64
**Core v2 conformance** matrix initializes the pinned `tests/spec-v2` submodule
and runs `make spec2`; it is included in the final `CI` aggregate, so it cannot
be replaced by a skipped ordinary wrapper or an informational report. This is a
tooling distinction, not a second test suite: `make spec2` selects package tests
that ordinary `go test ./...` also discovers. All six targets run the shared
single-P, parallel-fault, unrelated-fault chaining, public API, and
corpus-differential guard-page gates. Windows runs the equivalent Go commands
directly from PowerShell rather than through Make.

Linux/amd64 continues to host architecture-independent lint, TinyGo, coverage,
and binary-size jobs. TinyGo mirrors the native matrix: Linux/amd64 and
Linux/arm64 build, test, and smoke-run the CLI; Darwin/arm64 runs the runtime and
public API suites; Darwin/amd64 runs the same portable compiler/encoder scope as
Big Go. TinyGo runtime tests use verbose output so architecture-specific panics
identify the active test instead of reporting only an anonymous package failure.
`SupportedFeatures` and `RuntimeConfig.Validate` expose the complete Core 3
families only on linux/amd64; other runtime targets reject backend-incomplete
families before decoding or native lowering.
The CI card runs the WebAssembly 1.0, 2.0, and 3.0 suites
for visibility without making their current gaps required checks. The final
`CI` aggregation job is the stable branch-protection check and fails if any
required matrix cell or supporting job fails.

Nightly, canary, and tagged release workflows require Linux, Darwin, and Windows
CLI builds for both amd64 and arm64, then publish every successful binary with
its SHA-256 checksum. A push to `main` becomes a canary only after that commit's
`CI` workflow succeeds; failed or cancelled CI runs do not publish a canary.
Manual canary runs build the current `main` tip. Nightly and canary use unique
never-retargeted prerelease tags; the CLI resolves the newest `nightly-*` or
`canary-*` tag when a channel is installed. Every target publishes a
standard-Go CLI plus Normal builds of the Standard and Minimal runtimes.
Linux also requires Tiny builds of both profiles; other platforms publish
each feature-complete Tiny profile supported by their TinyGo port. Normal favors
runtime speed; Tiny favors executable size. Windows uses a native Windows 11
arm64 runner for arm64. A failed target blocks the release rather than silently
omitting a supported platform. Every
channel uses `wago-<goos>-<goarch>` for the manager and
`wago-runtime-<profile>-<build>-<goos>-<goarch>` for runtimes.
Both the matrix job and publisher run `scripts/verify-channel-assets.sh`; an
otherwise successful platform is rejected unless its CLI, both Normal runtimes,
and their checksums are present and valid. Linux additionally requires both
Tiny runtimes; optional Tiny assets on other platforms are verified
when present.
When a runtime asset or checksum is omitted, the CLI builds that release tag
from source on the user's host; checksum mismatches still fail closed.

After a canary, nightly, or stable GitHub Release is published, its workflow
dispatches a `code-release` event to `wago-org/docs`. The docs repository records
canary provenance, snapshots nightly documentation, and promotes stable docs
only from a nightly snapshot for the exact same Wago commit. Configure a GitHub
App with Contents read/write access to `wago-org/docs`, install it for that
repository, set its application ID as the `DOCS_SYNC_APP_ID` repository
variable, and store its private key in the `DOCS_SYNC_APP_PRIVATE_KEY` secret.
If the App is unavailable, publishing still succeeds and the docs repository's
scheduled reconciler recovers the release within its next polling interval.

`tests/scripts/dispatch-docs-release.sh` verifies the dispatch payload and input
validation without contacting GitHub.

For a local native approximation, run the commands below. The formatting gate
checks tracked Go files only, so toolchains or diagnostics retained under `.git/`
do not contaminate `make lint`.

```sh
make lint
make test
make test-guard   # only on a supported guard-page target
WAGO_CORPUS_TIMEOUT=20s make test-corpus
make simd
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
