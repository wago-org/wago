//go:build amd64 && tinygo && wago_lean

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// The lean TinyGo release keeps ordinary constructor lowering to bound compiler
// code size. Other builds retain the terminal scalar-selection optimization.
func (f *fn) tryForwardGCConstructorIf(*wasm.Reader, uint32, wasm.SubType) ([3]storage, wasm.Reader, bool) {
	return [3]storage{}, wasm.Reader{}, false
}
