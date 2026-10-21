# Feature support

WebAssembly features follow the phase groups and row order on
[webassembly.org/features](https://webassembly.org/features/)

Wago supports Linux, macOS, and Windows on amd64 and arm64, plus Linux on
RISC-V64. Linux/RV64 is supported in ordinary builds without an experimental
build tag and covers Core 1, Core 2, and Core 3, plus Wago's threads, guard-page,
artifact, host, plugin, profiling, and wide-arithmetic paths. See
[tests/README.md](tests/README.md) for acceptance commands. QEMU provides
functional validation; performance measurements require native RV64 hardware.

The [committed Core 3 baseline](tests/conformance/baselines/spec-v3-baseline.json)
records **2,226 modules and 58,038 assertions passed** across the pinned 258-file
suite, with zero failures, skips, or gap categories.

The emoji legend is:

- ✔️ · fully supported
- 🏁 · fully supported via flag
- 🔌 · provided through an optional plugin; combine with its implementation status
- 🚧 · work in progress
- ⏱️ · planned
- ❌ · not supported
- 🕳️ · not going to support

| Perspective | Description |
| --- | --- |
| good | Wago considers this specification conceptually good and worth looking into. |
| uncertain | Wago is uncertain about this specification and is not in a hurry to implement it. |
| harmful | Wago considers this specification harmful in its current state. |
| — | No position recorded or does not apply |

Support depends on the build and host. Use `SupportedFeatures()` to check the
executable feature set. Unsupported rows do not imply a decision against future support. JavaScript, browser, and
text-format proposals are listed for completeness; Wago accepts binary modules
and exposes a Go API.

## Phase 5 – The Feature is Standardized

| Feature | Status | Perspective |
| --- | :---: | --- |
| [JS BigInt to Wasm i64 Integration](https://github.com/WebAssembly/JS-BigInt-integration) | 🕳️ | — |
| [Branch Hinting](https://github.com/WebAssembly/branch-hinting/blob/master/proposals/branch-hinting/Overview.md) | ✔️ | good |
| [Bulk Memory Operations](https://github.com/WebAssembly/bulk-memory-operations/blob/master/proposals/bulk-memory-operations/Overview.md) | ✔️ | good |
| [Custom Text Format Annotations](https://github.com/WebAssembly/annotations/blob/main/proposals/annotations/Overview.md) | ⏱️ | good |
| [Exception Handling with exnref](https://github.com/WebAssembly/exception-handling/blob/master/proposals/exception-handling/Exceptions.md) | ✔️ | good |
| [Extended Constant Expressions](https://github.com/WebAssembly/extended-const/blob/master/proposals/extended-const/Overview.md) | ✔️ | good |
| [Garbage Collection](https://github.com/WebAssembly/gc) | ✔️ | good |
| [JS String Builtins](https://github.com/WebAssembly/js-string-builtins/blob/main/proposals/js-string-builtins/Overview.md) | 🕳️ | — |
| [JS Promise Integration](https://github.com/WebAssembly/js-promise-integration) | ⏱️ | uncertain |
| [Memory64](https://github.com/WebAssembly/memory64/blob/master/proposals/memory64/Overview.md) | ✔️ | good |
| [Multiple Memories](https://github.com/WebAssembly/multi-memory/blob/master/proposals/multi-memory/Overview.md) | ✔️ | good |
| [Multi-value](https://github.com/WebAssembly/spec/blob/master/proposals/multi-value/Overview.md) | ✔️ | good |
| [Import/Export of Mutable Globals](https://github.com/WebAssembly/mutable-global/blob/master/proposals/mutable-global/Overview.md) | ✔️ | good |
| [Reference Types](https://github.com/WebAssembly/reference-types/blob/master/proposals/reference-types/Overview.md) | ✔️ | good |
| [Relaxed SIMD](https://github.com/WebAssembly/relaxed-simd/tree/main/proposals/relaxed-simd) | ✔️ | good |
| [Non-trapping float-to-int Conversions](https://github.com/WebAssembly/spec/blob/master/proposals/nontrapping-float-to-int-conversion/Overview.md) | ✔️ | good |
| [Sign-extension Operators](https://github.com/WebAssembly/spec/blob/master/proposals/sign-extension-ops/Overview.md) | ✔️ | good |
| [Fixed-width SIMD](https://github.com/WebAssembly/simd/blob/master/proposals/simd/SIMD.md) | ✔️ | good |
| [Tail Call](https://github.com/WebAssembly/tail-call/blob/master/proposals/tail-call/Overview.md) | ✔️ | good |
| [Typed Function References](https://github.com/WebAssembly/function-references/blob/main/proposals/function-references/Overview.md) | ✔️ | good |
| [Web Content Security Policy](https://github.com/WebAssembly/content-security-policy/blob/main/proposals/CSP.md) | 🕳️ | — |

## Phase 4 – Standardize the Feature

| Feature | Status | Perspective |
| --- | :---: | --- |
| [Compact Import Section](https://github.com/WebAssembly/compact-import-section/blob/main/proposals/compact-import-section/Overview.md) | ✔️ | good |
| [Threads](https://github.com/WebAssembly/threads/blob/main-legacy/proposals/threads/Overview.md) | 🏁 | good |
| [Wide Arithmetic](https://github.com/WebAssembly/wide-arithmetic/blob/main/proposals/wide-arithmetic/Overview.md) | ✔️ | good |

## Phase 3 – Implementation Phase

| Feature | Status | Perspective |
| --- | :---: | --- |
| [Custom Descriptors and JS Interop](https://github.com/WebAssembly/custom-descriptors/blob/main/proposals/custom-descriptors/Overview.md) | ❌ | uncertain |
| [Custom Page Sizes](https://github.com/WebAssembly/custom-page-sizes/blob/main/proposals/custom-page-sizes/Overview.md) | ⏱️ | good |
| [ESM Integration](https://github.com/WebAssembly/esm-integration) | 🕳️ | — |
| [Stack Switching](https://github.com/WebAssembly/stack-switching/blob/main/proposals/stack-switching/Explainer.md) | ⏱️ | good |

## Phase 2 – Proposed Spec Text Available

| Feature | Status | Perspective |
| --- | :---: | --- |
| [Acquire-Release Atomics](https://github.com/WebAssembly/acquire-release-atomics) | ⏱️ | good |
| [Compilation Hints](https://github.com/WebAssembly/compilation-hints/blob/main/proposals/compilation-hints/Overview.md) | ⏱️ | good |
| [Extended Name Section](https://github.com/WebAssembly/extended-name-section/blob/main/proposals/extended-name-section/Overview.md) | ❌ | — |
| [JS Primitive Builtins](https://github.com/WebAssembly/js-primitive-builtins/blob/main/proposals/js-primitive-builtins/Overview.md) | 🕳️ | — |
| [Multibyte Array Access](https://github.com/WebAssembly/multibyte-array-access) | ❌ | — |
| [Numeric Values in WAT Data Segments](https://github.com/WebAssembly/wat-numeric-values/blob/main/proposals/wat-numeric-values/Overview.md) | ❌ | — |
| [Relaxed Dead Code Validation](https://github.com/WebAssembly/relaxed-dead-code-validation/blob/main/proposals/relaxed-dead-code-validation/Overview.md) | ❌ | — |
| [Rounding Variants](https://github.com/WebAssembly/rounding-mode-control/blob/main/proposals/rounding-mode-control/Overview.md) | ❌ | — |

## Phase 1 – Feature Proposal

| Feature | Status | Perspective |
| --- | :---: | --- |
| [Component Model](https://github.com/WebAssembly/component-model) | 🔌 🚧 | mixed |
| [Flexible Vectors](https://github.com/WebAssembly/flexible-vectors/blob/main/proposals/flexible-vectors/Overview.md) | ❌ | good |
| [Frozen Values](https://github.com/WebAssembly/frozen-values/blob/main/proposals/frozen-values/Overview.md) | ❌ | — |
| [Half Precision](https://github.com/WebAssembly/half-precision/blob/main/proposals/half-precision/Overview.md) | ❌ | — |
| [JIT Interface](https://github.com/WebAssembly/jit-interface/blob/main/proposals/jit-interface/Explainer.md) |  | harmful |
| [Memory Control](https://github.com/WebAssembly/memory-control/blob/main/proposals/memory-control/Overview.md) | ❌ | — |
| [More Array Constructors](https://github.com/WebAssembly/more-array-constructors/blob/main/proposals/more-array-constructors/Overview.md) | ❌ | — |
| [Profiles](https://github.com/WebAssembly/profiles/blob/main/proposals/profiles/Overview.md) | ❌ | — |
| [Shared-Everything Threads](https://github.com/WebAssembly/shared-everything-threads/blob/main/proposals/shared-everything-threads/Overview.md) | ❌ | good |
| [Reference-Typed Strings](https://github.com/WebAssembly/stringref/blob/main/proposals/stringref/Overview.md) | ❌ | — |
| [Type Imports](https://github.com/WebAssembly/proposal-type-imports/blob/main/proposals/type-imports/Overview.md) | ❌ | — |
| [Type Reflection for JS API](https://github.com/WebAssembly/js-types/blob/main/proposals/js-types/Overview.md) | ❌ | harmful |
| [WebAssembly C and C++ API](https://github.com/WebAssembly/wasm-c-api) | ❌ | harmful |

## Inactive

| Feature | Status | Perspective |
| --- | :---: | --- |
| [Legacy Exception Handling](https://github.com/WebAssembly/exception-handling/blob/master/proposals/exception-handling/legacy/Exceptions.md) | ❌ | — |
| [Instrument and Tracing Technology](https://github.com/WebAssembly/instrument-tracing/blob/main/proposals/instrument-tracing/Overview.md) | ❌ | — |

## Host APIs and ecosystem interfaces

These interfaces are separate from core WebAssembly proposals. Wago loads host
capabilities through optional plugins; core instruction support alone does not
provide a host API. Status reflects the available integration, including its
published limits, rather than compatibility with every program using that API.

| Feature | Status | Perspective |
| --- | :---: | --- |
| [WASI legacy snapshot (`wasi_unstable`)](https://github.com/WebAssembly/WASI/tree/main/legacy) | ❌ | — |
| [WASI 0.1 / Preview 1 (`wasi_snapshot_preview1`)](https://github.com/wago-org/wasi/tree/main/p1) | 🔌  | — |
| [WASI 0.2 / Preview 2 (`wasi:cli/command`)](https://github.com/wago-org/wasi/tree/main/p2) | 🔌  | — |
| [WASI 0.3 / Preview 3](https://wasi.dev/) | ⏱️ | — |
| [WASI HTTP (`wasi:http`)](https://github.com/WebAssembly/WASI/tree/main/proposals/http) | ❌ | — |
| [WASI threads (`wasi_thread_spawn`)](https://github.com/WebAssembly/wasi-threads) | ❌ | — |
| [WASI neural-network inference (`wasi-nn`)](https://github.com/WebAssembly/wasi-nn) | ❌ | — |
| [WASIX](https://wasix.org/) | ⏱️ | — |
| [Emscripten host runtime](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/Interacting-with-code.html) | ⏱️ | — |
| [Go JavaScript host API (`syscall/js`)](https://pkg.go.dev/syscall/js) | ❌ | — |
