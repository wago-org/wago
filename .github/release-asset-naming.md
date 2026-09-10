# Release asset names

## Channels and versions

- `canary` resolves the newest successful main commit with a retained host
  artifact. Its canonical identity is `canary@<40-character-commit-sha>`.
  Canaries create neither git tags nor GitHub Releases. Legacy
  `vMAJOR.MINOR.PATCH-canary.g<7-character-commit-sha>` artifacts remain
  installable while they are retained.
- `beta` resolves the newest manually qualified beta, such as
  `v0.1.0-beta.1`.
- `latest` resolves the newest stable `vMAJOR.MINOR.PATCH` release.

The canary workflow builds automatically after main CI succeeds. Artifacts are
named `canary-<40-character-commit-sha>-<os>-<arch>` and retained for 90 days.
Manager and runtime installation
first attempt the matching host artifact and verify its bundled SHA-256 file.
Wago first asks an installed and authenticated GitHub CLI (`gh`) to download the
artifact. If that is unavailable, it tries the Actions API with
`WAGO_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN`. If both transports fail, or
the archive is expired or invalid, installation builds the exact commit source
with the requested `go` or `tinygo` executable from `PATH`. Beta and stable
releases are dispatched through
`release.yml` with an exact full commit SHA that already passed main CI; those
releases build, smoke-test, checksum, and publish the platform asset set.

## Release notes

GitHub generates public notes for beta and stable releases. Pull requests carrying the
`enhancement` label appear under **New features**; all other included pull
requests appear under **Changelog**. GitHub also identifies first-time
contributors. Qualification details and asset hashes stay in
`release-manifest.json` instead of cluttering the public notes.

## Choose a file

Start with the Wago CLI that matches your operating system and CPU architecture:

`wago-<os>-<arch>`

The bootstrap scripts download and start the matching installer executable. The
installer manages the cross-platform installation flow. After installation, the
bootstrap script only refreshes `PATH` in its own shell when requested:

`wago-installer-<os>-<arch>`

The CLI installs and switches runtimes. Runtime files use this name format:

`wago-runtime-<profile>-<build>-<os>-<arch>`

## Profiles

- `standard` — everything
- `minimal` — run only

## Builds

- `normal` — built with standard Go; choose this for the fastest runtime
- `tiny` — built with TinyGo; choose this for a smaller executable

For example, `wago-runtime-minimal-tiny-linux-arm64` is the smaller run-only
runtime for Linux arm64. Each binary has a sibling `.sha256` checksum file.
Normal builds are available for every successful platform. Tiny builds are
available where TinyGo supports all features required by that profile.
