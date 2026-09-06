# Continuous integration

Pull requests and pushes to `main` run `.github/workflows/ci.yml`. Documentation
changes run a lightweight, deterministic local-link and heading-anchor check;
every code or build change runs the complete native platform matrix:

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
Linux/amd64 and main-push coverage
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
be replaced by a skipped ordinary wrapper or an informational report. A separate
mandatory **Core v3 conformance** matrix runs `make spec3` and
`make spec3-signals` on Linux amd64, Linux arm64, and Darwin arm64. The final
`CI` aggregate depends on the complete Core v3 matrix as well, so any failed or
cancelled explicit- or signal-bounds cell makes the branch-protection check fail.
This is a tooling distinction, not a second test suite: `make spec2` selects
package tests that ordinary `go test ./...` also discovers. All six targets run the shared
single-P, parallel-fault, unrelated-fault chaining, public API, and
corpus-differential guard-page gates. Windows runs the equivalent Go commands
directly from PowerShell rather than through Make. Each native command in the
Windows build/test and guard steps checks `$LASTEXITCODE` immediately. A later
passing command must not hide an earlier package, compiler, or guard failure.
The Windows guard step runs both core runtime and public API tests, including
ARM64 page-commit allocation-failure and native return-state checks.

Linux/amd64 continues to host architecture-independent lint, TinyGo, and
binary-size jobs on pull requests and pushes to `main`; coverage runs only for
code changes pushed to `main`, keeping the expensive merged report off pull
requests while retaining it as a post-merge gate. A separate bounded runtime
concurrency matrix runs the deterministic public-API harness on native Linux
amd64 and arm64. The Linux/amd64 race lane runs the same harness under `-race`
alongside the focused runtime packages.
The size job runs `scripts/size-card.sh` for four explicit Linux/AMD64 release
profiles: manager, Standard runtime, Minimal runtime, and the TinyGo Minimal
runtime. It fails above the byte ceilings in
`scripts/release-size-budgets.tsv`, reports pull-request deltas, and uploads
`size-profiles.tsv` plus the 25 largest retained symbols for each standard-Go
profile in `size-symbols.tsv`. Build flags are fixed to stripped,
reproducible `-trimpath -buildvcs=false` outputs. The TinyGo profile additionally
uses the same Linux section stripping as release assets. Budgets describe the
current products rather than conflating them with the future artifact-only
loader; update a ceiling only with a measured release-profile rationale.
Darwin TinyGo release assets run Apple `strip -x` before checksums are written,
removing TinyGo's retained local Mach-O symbol and string tables while preserving
the external symbols and valid ad-hoc signature required to execute the binary.
TinyGo mirrors the native matrix: Linux/amd64 and
Linux/arm64 build, test, and smoke-run the CLI; Darwin/arm64 runs the runtime and
public API suites; Darwin/amd64 runs the same portable compiler/encoder scope as
Big Go. TinyGo runtime tests use verbose output so architecture-specific panics
identify the active test instead of reporting only an anonymous package failure.
`SupportedFeatures` and `RuntimeConfig.Validate` expose the complete Core 3
families on linux/amd64 plus linux/arm64 and darwin/arm64. Other runtime targets
retain the portable Release 2 surface plus extended constant expressions and
reject incomplete Core 3 families before decoding or native lowering.
The main-push coverage job runs the WebAssembly 1.0, 2.0, and 3.0 suites and
uploads their report fragments with the merged coverage profile. The final `CI`
aggregation job is the stable branch-protection check and fails if any required
matrix cell or supporting job fails or is cancelled. Every top-level job in
`.github/workflows/ci.yml` other than the aggregate itself must appear in its
`needs` set; `tests/cipolicy` enforces that policy so a newly added qualification
job cannot silently bypass branch protection. Path-gated jobs remain aggregate
dependencies and may report `skipped` when their documented condition does not
apply—for example, coverage on pull requests—but no independent top-level CI job
is merely informational.

The change detector reports documentation and code changes independently. Every
change runs `make docs-check`, because a code-only refactor can remove a source
path referenced by unchanged documentation. Documentation-only pull requests do
not start the native matrix, while mixed pull requests run both checks. The
documentation checker uses only the Go standard library and the tracked
repository files: it rejects missing relative targets, exact-case path
mismatches, and missing Markdown heading fragments without contacting external
sites. Run the same check locally with `make docs-check`.

Nightly, canary, and stable release workflows require Linux, Darwin, and Windows
CLI builds for both amd64 and arm64, then publish every successful binary with
its SHA-256 checksum. A push to `main` becomes a canary only after that commit's
`CI` workflow succeeds; failed or cancelled CI runs do not publish a canary.
Manual canary runs build the current `main` tip. Nightly and canary use unique
never-retargeted prerelease tags; canary tags contain the full 40-hex commit ID,
and both channels stamp binaries with their canonical full commit identity. The
CLI resolves the newest `nightly-*` or `canary-*` tag when a channel is
installed.

Stable releases are not tag-triggered. Dispatch `Qualify and publish stable
release` with an exact `vMAJOR.MINOR.PATCH` version and the full 40-character SHA
of a commit reachable from `main`. The workflow locates a successful `CI` push
run for that exact SHA and downloads its immutable qualification record. The
record names every aggregate dependency and stable publication rejects missing,
failed, cancelled, or skipped required jobs. This means a documentation-only CI
run with skipped executable qualification cannot authorize a stable binary
release. The CI policy test also requires every top-level job to remain in the
aggregate dependency set.

Only after source qualification does the stable workflow build the release
files. Each native matrix cell verifies checksums and the embedded version,
then executes a scalar Wasm module and reloads one generated `.wago` artifact
with every Normal or Tiny runtime file that cell will publish. The manifest job
records the source SHA, CI run ID and attempt, exact CI job results, and the
name, byte size, and SHA-256 hash of every release file. Publication downloads
those already tested files; it does not rebuild or replace them. It verifies the
manifest again and creates or accepts
only a lightweight version tag that points directly to the qualified SHA. A
retry may overwrite this workflow run's internal per-target artifacts or resume
a matching draft release, but it re-verifies the complete manifest and exact
draft asset-name set before publication. An already published GitHub Release is
never replaced. A manually created or moved `v*` tag does not start publication
and cannot reuse
a qualification record for another commit.

Every target publishes a standard-Go CLI plus Normal builds of the Standard and
Minimal runtimes.
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

Pushes to `main` that change `install.sh`, `install.cmd`, or `install.ps1` run the
`Publish installers` workflow. It uses `INSTALL_REPO_TOKEN` to copy only those
bootstraps to `wago-org/install`; that repository deploys them
to `https://install.wago.sh` with GitHub Pages. The installer site owns its
Pages workflow and custom domain, while this repository remains the source of
truth for installer behavior. `INSTALL_REPO_TOKEN` must grant Contents write
access to `wago-org/install`.

For a local native approximation, run the commands below. The formatting gate
checks tracked Go files only, so toolchains or diagnostics retained under `.git/`
do not contaminate `make lint`.

```sh
make docs-check
make lint
make test  # includes release-manifest, tamper-rejection, and artifact-smoke policy tests
make test-concurrency  # replay with WAGO_CONCURRENCY_SEED=<seed>[,<seed>...]
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
