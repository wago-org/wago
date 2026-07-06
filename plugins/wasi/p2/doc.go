// Package p2 is a placeholder for WASI preview 2.
//
// Preview 2 is not a wider set of host functions like the p1/unstable snapshots —
// it is a different model entirely: the guest is a WebAssembly Component (the
// component model) whose imports are WIT interfaces (wasi:cli, wasi:io,
// wasi:filesystem, …) lowered through canonical ABI adapters, not flat
// `(func (param i32 ...) (result i32))` core-wasm imports. Supporting it requires
// a component-model loader (WIT worlds, resources, the canonical ABI), which wago
// does not have yet.
//
// This package intentionally exposes no API; it marks the slot in the version
// layout (unstable → p1 → p2) so the eventual implementation has an obvious home.
// Until then, run preview1 modules with plugins/wasi/p1.
package p2
