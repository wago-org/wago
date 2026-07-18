//go:build linux && riscv64

package wago

// RV64 uses the synchronous no-cgo re-entry protocol for every host import. It
// is the only policy that supports arbitrary signatures and deterministic trap
// propagation on the foreign execution stack.
const forceSyncHostImports = true
