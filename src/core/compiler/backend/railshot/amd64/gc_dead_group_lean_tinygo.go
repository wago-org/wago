//go:build amd64 && tinygo && wago_lean

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// The lean TinyGo release keeps individual checked dead allocations to limit
// compiler code size. Other builds retain the grouped native charge.
func (f *fn) tryDeadGCDefaultGroup(*wasm.Reader, uint32) (bool, error) {
	return false, nil
}
