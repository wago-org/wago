# Release asset names

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
