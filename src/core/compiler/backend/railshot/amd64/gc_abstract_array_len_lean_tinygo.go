//go:build amd64 && tinygo && wago_lean

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

func (f *fn) tryFuseAbstractArrayCastLen(bool, *wasm.Reader) (bool, error) { return false, nil }
func emitModuleGCAbstractArrayCastLen(*x64.Asm) {
	panic("amd64: lean compiler emitted a disabled abstract-array native stub")
}
