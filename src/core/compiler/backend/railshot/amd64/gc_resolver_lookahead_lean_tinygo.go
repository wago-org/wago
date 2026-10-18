//go:build amd64 && tinygo && wago_lean

package amd64

import "github.com/wago-org/wago/src/core/compiler/wasm"

func (f *fn) retainGCResolvedAddress(_ *wasm.Reader, _ int, _ bool) bool { return false }
