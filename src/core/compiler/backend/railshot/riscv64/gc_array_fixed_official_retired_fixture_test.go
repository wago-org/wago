//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func rv64ExactArrayType(group *wasm.RecType, mutable bool) bool {
	if group == nil || len(group.SubTypes) != 1 {
		return false
	}
	wantMut := wasm.Const
	if mutable {
		wantMut = wasm.Var
	}
	sub := &group.SubTypes[0]
	return sub.Final && !sub.HasPrefix && len(sub.Supers) == 0 && !sub.Metadata.Describes.Present() && !sub.Metadata.Descriptor.Present() &&
		sub.Comp.Kind == wasm.CompArray && sub.Comp.Array == wasm.NewFieldType(wasm.StorageVal(wasm.F32), wantMut)
}
