// Package gc at the native import path is the trusted WasmGC execution ABI.
// Compact Ref words are collector-local indexes, with no ownership/generation
// proof. JIT adapters must validate Go/public tokens before converting them to
// these words. Normal Go callers must use the checked parent gc package instead.
// NativeView, prevalidated operations and direct root words are internal runtime
// mechanisms, not general-purpose reference ingress or memory safety boundaries.
package gc
