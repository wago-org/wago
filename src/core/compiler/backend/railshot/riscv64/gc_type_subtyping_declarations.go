//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"reflect"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// rv64GCTypeSubtypingDeclarationsWire is type-subtyping.0.wasm from the
// pinned Core 3 corpus. It contains only seven array type declarations and no
// functions, state, initializers or metadata.
var rv64GCTypeSubtypingDeclarationsWire = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0xa8, 0x80, 0x80, 0x80, 0x00, 0x07,
	0x50, 0x00, 0x5e, 0x7f, 0x00,
	0x50, 0x01, 0x00, 0x5e, 0x7f, 0x00,
	0x50, 0x00, 0x5e, 0x6e, 0x00,
	0x50, 0x00, 0x5e, 0x63, 0x00, 0x00,
	0x50, 0x00, 0x5e, 0x64, 0x01, 0x00,
	0x50, 0x00, 0x5e, 0x7f, 0x01,
	0x50, 0x01, 0x05, 0x5e, 0x7f, 0x01,
}

var rv64GCTypeSubtypingDeclarationsTypes = func() []wasm.RecType {
	m, err := wasm.DecodeModule(rv64GCTypeSubtypingDeclarationsWire)
	if err != nil {
		panic(err)
	}
	return m.Types
}()

func admitRV64GCTypeSubtypingDeclarationsWire(data []byte) bool {
	if !bytes.Equal(data, rv64GCTypeSubtypingDeclarationsWire) || !admitExactProductWireShape(data, []byte{1}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCTypeSubtypingDeclarations(m)
}

// AdmitGCTypeSubtypingDeclarationsSource reports whether data is the exact
// first declaration-only gc/type-subtyping product.
func AdmitGCTypeSubtypingDeclarationsSource(data []byte) bool {
	return admitRV64GCTypeSubtypingDeclarationsWire(data)
}

func admitRV64GCTypeSubtypingDeclarations(m *wasm.Module) bool {
	return m != nil && reflect.DeepEqual(m.Types, rv64GCTypeSubtypingDeclarationsTypes) &&
		len(m.FuncTypes) == 0 && len(m.Code) == 0 && len(m.Imports) == 0 && len(m.Tables) == 0 && len(m.Memories) == 0 &&
		len(m.Tags) == 0 && len(m.StringRefs) == 0 && len(m.Globals) == 0 && len(m.Exports) == 0 && m.Start == nil &&
		len(m.Elements) == 0 && len(m.Data) == 0 && m.DataCount == nil && len(m.Customs) == 0 && !m.HasNameMetadata() &&
		m.BranchHints == nil && !m.UsesCompactImports
}
