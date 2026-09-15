# Changelog

Notable user-facing changes to Wago are recorded here. This changelog starts
with `v0.1.0-beta.8`; earlier prereleases remain available in the
[GitHub Releases](https://github.com/wago-org/wago/releases) history.

## [Unreleased]

### Added

- `wago run` accepts repeated `--invoke` flags and calls the selected exports in
  order on one module instance. Each call consumes only the positional values
  required by its signature, including typed values such as `7:i64` and
  `3.5:f64`.

### Changed

- The README's one-shot Go installer command now runs the installer from
  `@main`, and the redundant persistent-installer example has been removed.

## [v0.1.0-beta.8] - 2026-09-10

### Added

- Added Beta and commit-addressed Canary release channels, with qualified
  release artifacts and a Go-native installation route.
- Added explicit native-artifact trust controls, runtime resource policies,
  callback re-entry, and broader Core 3 and WebAssembly GC support.
- Added end-to-end semantic corpus coverage and differential engine-state
  testing.

### Changed

- Replaced the Make-based developer interface with Just.
- Reduced compiler memory use, compile latency, host-call overhead, and prepared
  invocation latency across AMD64 and ARM64.
- Simplified release notes and installer/channel discovery behavior.

### Fixed

- Fixed JIT, invocation, validation, settings, TinyGo trampoline, signal,
  reference-lifetime, and resource-limit correctness issues found by the
  September audit.
- Fixed AMD64 corpus miscompiles, ARM64 indexed bulk-memory bounds checks, and
  incomplete typed-select immediate decoding.
- Fixed Beta discovery and retracted legacy tagged Canary module versions.

[Unreleased]: https://github.com/wago-org/wago/compare/v0.1.0-beta.8...HEAD
[v0.1.0-beta.8]: https://github.com/wago-org/wago/releases/tag/v0.1.0-beta.8
