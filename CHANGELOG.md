# Changelog

Notable user-facing changes to Wago are recorded here. This changelog starts
with `v0.1.0-beta.8`; earlier prereleases remain available in the
[GitHub Releases](https://github.com/wago-org/wago/releases) history.

## [Unreleased]

## [v0.1.0-beta.10] - 2026-09-23

### Added

- The shared correctness and benchmark corpus now runs 40 full application
  executables end to end, with pinned provenance and exact independent output
  oracles for application-scale JavaScript, SQL, compression, CLI, compiler,
  language-runtime, cryptography, and FPGA-tool workloads.

### Changed

- Host imports instantiate faster and allocate less by sharing immutable
  dispatch thunks across instances and deferring instance-specific state until
  it is needed.
- Dynamic `memory.copy` and `memory.fill`, `memory.grow(0)`, and large funcref
  `table.fill` operations are faster across AMD64 and ARM64.
- Compiler value-type reuse, scalar host-call handling, wide GC dirty-range
  scanning, and Linux idle-memory reclamation reduce runtime and compilation
  overhead.

### Fixed

- Correct ARM64 miscompiles involving mixed float-call arguments, large `v128`
  stack displacements, pinned and deferred loads, `memory.fill` scratch
  registers, and guard-mode indexed-base reuse. These fixes address PDFium
  rendering failures and bulk-memory corruption under register pressure.
- Ensure reused linear memory is zeroed on Darwin when `MADV_ZERO` is
  unavailable.
- Initialize cached float constants only at function entry so branch-specific
  lowering cannot reuse an uninitialized register on AMD64 or ARM64.
- Correct nested shifts and rotates, non-null global writes, shared-memory
  imports, reference widening and nullability, multi-memory encoding, AVX-512
  high-register forms, large-offset bounds checks, GC maps and safepoints,
  passive-element overflow, and aliased code-buffer growth.
- Correct explicit Core-profile precedence, `_start` signature validation,
  watch-flag parsing, result formatting with mismatched metadata, plugin source
  selection, semantic-version wildcard validation, and full uninstall cleanup
  for active XDG directories and Fish completions.

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

[Unreleased]: https://github.com/wago-org/wago/compare/v0.1.0-beta.10...HEAD
[v0.1.0-beta.10]: https://github.com/wago-org/wago/compare/v0.1.0-beta.9...v0.1.0-beta.10
[v0.1.0-beta.9]: https://github.com/wago-org/wago/compare/v0.1.0-beta.8...v0.1.0-beta.9
[v0.1.0-beta.8]: https://github.com/wago-org/wago/releases/tag/v0.1.0-beta.8
