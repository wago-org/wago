# Changelog

Notable user-facing changes to Wago are recorded here. This changelog starts
with `v0.1.0-beta.8`; earlier prereleases remain available in the
[GitHub Releases](https://github.com/wago-org/wago/releases) history.

## [Unreleased]

## [v0.1.0-beta.9] - 2026-09-16

### Added

- `wago run` accepts repeated `--invoke` flags and calls the selected exports in
  order on one module instance. Each call consumes only the positional values
  required by its signature, including typed values such as `7:i64` and
  `3.5:f64`.
- Core 3 is enabled by default, including compact imports and broader
  WebAssembly GC validation and execution coverage.
- Portable typed host callbacks support ordinary scalar Go functions,
  `func(HostCall)`, and caller-aware `func(Caller, HostCall)` callbacks under
  both Go and TinyGo.

### Changed

- The README's one-shot Go installer command now runs the installer from
  `@main`, and the redundant persistent-installer example has been removed.
- **Breaking:** host imports now use `NewImports` and flat
  `HostFunc(module, name, fn)` registration. The namespace builder and the old
  `Func`, `CallerFunc`, raw-slot callback, and signature-specific registration
  APIs have been removed.
- **Breaking:** `WasmFunc` replaces `PrepareFunction` and all
  signature-specific prepared handles. `WasmFunc.Invoke(args...)` is the only
  resolved-function invocation method; numbered `Invoke0` through `Invoke4`
  methods have been removed.
- **Breaking:** public prepared sessions and `OpenSession` have been removed.
  Each invocation now performs its own lifecycle admission.
- **Breaking:** by-name execution is `Invoke` or `InvokeContext`; tagged-value
  calls use `InvokeValues` instead of `Call`.
- Host/Wasm dispatch, typed callbacks, Railshot-generated execution, branch
  hints, import-range lookup, and prepared-artifact admission are faster while
  retaining the existing ownership and resource checks.

### Fixed

- Preserve WebAssembly GC roots and metadata across fixed-array results,
  prepared calls, table reads, shared domains, cancellation, and callbacks.
- Bound synchronous host callback re-entry to four active calls per invocation
  chain, including cross-instance re-entry, without retaining an execution
  reservation between calls.
- Harden instance shutdown, cancelable admission, retained memory callbacks,
  compile-lifetime observers, prepared-artifact limits, and host re-entry close
  interruption.
- Correct Core 3 validation and name decoding, ARM64 register allocation and
  branch patching, Windows cleanup behavior, semantic-version prerelease
  ranges, build output aliasing, and installer fallback behavior.

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

[Unreleased]: https://github.com/wago-org/wago/compare/v0.1.0-beta.9...HEAD
[v0.1.0-beta.9]: https://github.com/wago-org/wago/compare/v0.1.0-beta.8...v0.1.0-beta.9
[v0.1.0-beta.8]: https://github.com/wago-org/wago/releases/tag/v0.1.0-beta.8
