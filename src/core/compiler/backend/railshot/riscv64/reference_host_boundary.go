//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"slices"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// admitPublicHostReferenceBoundaryWire recognizes the canonical source form
// before decoding can discard section provenance. The exact module has one
// imported host function and one exported forwarding wrapper with the same
// bounded scalar/reference signature.
func admitPublicHostReferenceBoundaryWire(data []byte) bool {
	if !publicHostReferenceBoundaryWireShape(data) {
		return false
	}
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(data, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	return err == nil && admitPublicHostReferenceBoundary(decoded.Module)
}

func publicHostReferenceBoundaryWireShape(data []byte) bool {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) {
		return false
	}
	offset := 8
	var sections [5][]byte
	for i, id := range []byte{1, 2, 3, 7, 10} {
		if offset >= len(data) || data[offset] != id {
			return false
		}
		offset++
		n, ok := publicReferenceCallBoundaryWireU32(data, &offset)
		if !ok || uint64(offset)+uint64(n) > uint64(len(data)) {
			return false
		}
		sections[i] = data[offset : offset+int(n)]
		offset += int(n)
	}
	if offset != len(data) || !bytes.Equal(sections[1], []byte{1, 3, 'e', 'n', 'v', 6, 't', 'a', 'r', 'g', 'e', 't', 0, 0}) || !bytes.Equal(sections[2], []byte{1, 0}) || !bytes.Equal(sections[3], []byte{1, 4, 'c', 'a', 'l', 'l', 0, 1}) {
		return false
	}

	typeOffset := 0
	count, ok := publicReferenceCallBoundaryWireU32(sections[0], &typeOffset)
	if !ok || count != 1 {
		return false
	}
	params, results, ok := publicReferenceCallBoundaryWireType(sections[0], &typeOffset)
	if !ok || typeOffset != len(sections[0]) {
		return false
	}
	reference := false
	for _, list := range [][]byte{params, results} {
		for _, typ := range list {
			reference = reference || typ == 0x70 || typ == 0x6f
		}
	}
	if !reference {
		return false
	}

	codeOffset := 0
	codeCount, ok := publicReferenceCallBoundaryWireU32(sections[4], &codeOffset)
	if !ok || codeCount != 1 {
		return false
	}
	bodyBytes, ok := publicReferenceCallBoundaryWireU32(sections[4], &codeOffset)
	if !ok || bodyBytes == 0 || uint64(codeOffset)+uint64(bodyBytes) != uint64(len(sections[4])) {
		return false
	}
	body := sections[4][codeOffset:]
	want := []byte{0} // canonical zero local-declaration vector
	for i := range params {
		want = append(want, 0x20)
		want = appendPublicReferenceBoundaryU32(want, uint32(i))
	}
	want = append(want, 0x10, 0, 0x0b)
	return bytes.Equal(body, want)
}

// admitPublicHostReferenceBoundary is deliberately a single import and a
// single forwarding export. It admits only abstract nullable funcref and
// externref alongside the shared scalar carriers. The universal host bridge
// authenticates references in both directions; no table, global, initializer,
// start or opaque metadata can inherit this exception.
func admitPublicHostReferenceBoundary(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.Imports) != 1 || len(m.FuncTypes) != 1 || len(m.Code) != 1 || len(m.Exports) != 1 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 || len(m.Tags) != 0 || len(m.StringRefs) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Elements) != 0 || m.Start != nil || len(m.Customs) != 0 || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	group := m.Types[0]
	if len(group.SubTypes) != 1 {
		return false
	}
	sub := group.SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() || sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) || len(sub.Comp.Params) > coreruntime.MaxHostArity || len(sub.Comp.Results) > coreruntime.MaxHostArity || !publicReferenceCarrierTarget(&sub.Comp) {
		return false
	}
	im := m.Imports[0]
	if im.Module != "env" || im.Name != "target" || im.Type != wasm.NewFuncExternType(wasm.TypeIdx{}) || m.FuncTypes[0] != (wasm.TypeIdx{}) {
		return false
	}
	fn := m.Code[0]
	if len(fn.Locals.Runs) != 0 || len(fn.Body.Instrs) != 0 {
		return false
	}
	want := make([]byte, 0, 2*len(sub.Comp.Params)+3)
	for i := range sub.Comp.Params {
		want = append(want, 0x20)
		want = appendPublicReferenceBoundaryU32(want, uint32(i))
	}
	want = append(want, 0x10, 0, 0x0b)
	if !slices.Equal(fn.BodyBytes, want) {
		return false
	}
	ex := m.Exports[0]
	return ex.Name == "call" && ex.Index.Kind == wasm.ExternFunc && ex.Index.Index == 1
}

func admitPublicReferenceCarrierBoundary(m *wasm.Module) bool {
	return admitPublicReferenceCallBoundary(m) || admitPublicHostReferenceBoundary(m)
}
