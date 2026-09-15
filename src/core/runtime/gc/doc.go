// Package gc provides collector-bound, generation-checked Go references.
// Object references are opaque values and cannot be transferred between
// collectors. Keep live objects in RootSets across collection. Collectors and
// their roots require external synchronization, as does the native collector.
//
// The native subpackage is the trusted JIT ABI. Its compact integers deliberately
// carry no ownership information and must never serve as a Go ingress API.
package gc
