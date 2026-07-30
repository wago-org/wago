### Which file should I download?

Start with the Wago CLI for your platform:

`wago-<os>-<arch>`

The CLI installs and switches runtimes for you. Runtime files use this naming scheme:

`wago-runtime-<profile>-<build>-<os>-<arch>`

Profiles:

- `standard` — everything
- `minimal` — run only

Builds:

- `normal` — built with standard Go; choose this for the fastest runtime
- `tiny` — built with TinyGo; choose this for a smaller executable

For example, `wago-runtime-minimal-tiny-linux-arm64` is the smaller run-only runtime for Linux arm64. Every binary has a sibling `.sha256` checksum. Normal builds are provided for every successful platform; Tiny builds are included where TinyGo supports all features required by that profile.
