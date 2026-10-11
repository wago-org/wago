//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCArrayInitElemHex = "0061736d01000000019580808000056000005e6400005e700160017f0060037f7f7f000386808080000500030004000484808080000170000106958080800002640100d200410cfb06010b640200410cfb07020b07c780808000040e61727261795f63616c6c5f6e746800011461727261795f696e69745f656c656d2d6e756c6c00020f61727261795f696e69745f656c656d00030964726f705f7365677300040990808080000101000c0000000000000000000000000acf80808000058280808000000b928080800000410023012000fb0b02260041001100000b8e8080800000d002410041004100fb1302000b8e80808000002301200020012002fb1302000b858080800000fc0d000b"

func admitRV64GCArrayInitElemWire(data []byte) bool {
	if !admitExactProductWireShape(data, []byte{1, 3, 4, 6, 7, 9, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCArrayInitElem(m)
}

func AdmitGCArrayInitElemSource(data []byte) bool { return admitRV64GCArrayInitElemWire(data) }

func admitRV64GCArrayInitElem(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 5 || len(m.FuncTypes) != 5 || len(m.Code) != 5 || len(m.Tables) != 1 || len(m.Globals) != 2 || len(m.Exports) != 4 || len(m.Elements) != 1 ||
		len(m.Imports) != 0 || len(m.Memories) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 || m.Start != nil ||
		len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports || !i31CoreFunctionType(&m.Types[0], nil, nil) {
		return false
	}
	ref0 := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	if !rv64ExactReferenceArrayType(&m.Types[1], ref0, false) || !rv64ExactReferenceArrayType(&m.Types[2], wasm.FuncRef, true) ||
		!i31CoreFunctionType(&m.Types[3], []wasm.ValType{wasm.I32}, nil) || !i31CoreFunctionType(&m.Types[4], []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil) {
		return false
	}
	if m.Tables[0].Type.Ref != wasm.AbsRef(wasm.HeapFunc) || m.Tables[0].Type.Limits.Min != 1 || m.Tables[0].Type.Limits.HasMax || m.Tables[0].Type.Limits.Max != 0 || m.Tables[0].Type.Limits.Addr64 || m.Tables[0].Init != nil {
		return false
	}
	wantGlobals := []struct {
		typeIndex uint32
		body      []byte
	}{{1, []byte{0xd2, 0, 0x41, 12, 0xfb, 6, 1, 0x0b}}, {2, []byte{0x41, 12, 0xfb, 7, 2, 0x0b}}}
	for i, want := range wantGlobals {
		g := &m.Globals[i]
		if g.Type.Mutable || g.Type.Type != wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: want.typeIndex}), false)) || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, want.body) {
			return false
		}
	}
	wantFuncTypes := []uint32{0, 3, 0, 4, 0}
	wantBodies := [][]byte{{0x0b}, {0x41, 0, 0x23, 1, 0x20, 0, 0xfb, 0x0b, 2, 0x26, 0, 0x41, 0, 0x11, 0, 0, 0x0b}, {0xd0, 2, 0x41, 0, 0x41, 0, 0x41, 0, 0xfb, 0x13, 2, 0, 0x0b}, {0x23, 1, 0x20, 0, 0x20, 1, 0x20, 2, 0xfb, 0x13, 2, 0, 0x0b}, {0xfc, 0x0d, 0, 0x0b}}
	for i := range m.Code {
		if m.FuncTypes[i] != (wasm.TypeIdx{Index: wantFuncTypes[i]}) || len(m.Code[i].Locals.Runs) != 0 || m.Code[i].LocalDeclBytes != 1 || len(m.Code[i].Body.Instrs) != 0 || len(m.Code[i].Body.BodyBytes) != 0 || !bytes.Equal(m.Code[i].BodyBytes, wantBodies[i]) {
			return false
		}
	}
	e := &m.Elements[0]
	if e.Mode.Kind != wasm.ElemPassive || e.Mode.Table != 0 || len(e.Mode.Offset.BodyBytes) != 0 || len(e.Mode.Offset.Instrs) != 0 || e.Kind.Kind != wasm.ElemFuncs || len(e.Kind.Funcs) != 12 || e.Kind.Ref != (wasm.RefType{}) || len(e.Kind.Exprs) != 0 {
		return false
	}
	for _, index := range e.Kind.Funcs {
		if index != 0 {
			return false
		}
	}
	wantExports := []struct {
		name  string
		index uint32
	}{{"array_call_nth", 1}, {"array_init_elem-null", 2}, {"array_init_elem", 3}, {"drop_segs", 4}}
	for i, want := range wantExports {
		got := m.Exports[i]
		if got.Name != want.name || got.Index.Kind != wasm.ExternFunc || got.Index.Index != want.index {
			return false
		}
	}
	return true
}
