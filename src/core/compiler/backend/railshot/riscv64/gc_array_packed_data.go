//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func admitRV64GCArrayPackedDataWire(data []byte) bool { return false }

func AdmitGCArrayPackedDataSource(data []byte) bool { return false }

func rv64ExactPackedI8ArrayType(group *wasm.RecType, mutable bool) bool {
	if group == nil || len(group.SubTypes) != 1 {
		return false
	}
	wantMut := wasm.Const
	if mutable {
		wantMut = wasm.Var
	}
	sub := &group.SubTypes[0]
	return sub.Final && !sub.HasPrefix && len(sub.Supers) == 0 && !sub.Metadata.Describes.Present() && !sub.Metadata.Descriptor.Present() &&
		sub.Comp.Kind == wasm.CompArray && sub.Comp.Array == wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), wantMut)
}

func admitRV64GCArrayPackedData(m *wasm.Module) bool { return false }
