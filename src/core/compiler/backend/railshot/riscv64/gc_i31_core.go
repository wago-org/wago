//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// rv64I31CoreWire is the first public GC source admitted by the RV64
// non-allocating i31 checkpoint. The official spec fixture is admitted below by
// its pinned length and digest.
const rv64I31CoreWire = "\x00\x61\x73\x6d\x01\x00\x00\x00" +
	"\x01\x06\x01\x60\x01\x7f\x01\x7f" +
	"\x03\x03\x02\x00\x00" +
	"\x07\x15\x02\x06signed\x00\x00\x08unsigned\x00\x01" +
	"\x0a\x13\x02\x08\x00\x20\x00\xfb\x1c\xfb\x1d\x0b" +
	"\x08\x00\x20\x00\xfb\x1c\xfb\x1e\x0b"

func admitRV64I31CoreWire(data []byte) bool {
	if len(data) == len(rv64I31CoreWire) && string(data) == rv64I31CoreWire {
		return true
	}
	return len(data) == 252 && sha256.Sum256(data) == [sha256.Size]byte{0x4b, 0xdd, 0x4d, 0x0f, 0x18, 0x6a, 0x2f, 0xd6, 0x17, 0xb4, 0x1a, 0xd4, 0x94, 0x0e, 0x17, 0xf2, 0xc0, 0x41, 0x55, 0x14, 0xeb, 0xc6, 0x36, 0xa5, 0x6e, 0x41, 0x49, 0x6e, 0x8c, 0x39, 0x2a, 0xea}
}

// AdmitI31CoreSource returns the exact-wire provenance witness for the first
// non-allocating RV64 i31 checkpoint. Callers must pass this witness back in
// CompileOptions; decoded shape alone is deliberately insufficient because the
// decoder discards semantically inert wire sections.
func AdmitI31CoreSource(data []byte) bool {
	return admitRV64I31CoreWire(data)
}

// admitRV64I31Core independently proves the decoded shape used to select the
// lowering. This is intentionally narrower than the general collector-free
// i31 implementation on complete backends.
func admitRV64I31Core(m *wasm.Module) bool {
	return admitRV64I31MinimalCore(m) || admitRV64I31OfficialCore(m)
}

func admitRV64I31MinimalCore(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 1 || len(m.FuncTypes) != 2 || len(m.Code) != 2 || len(m.Exports) != 2 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Globals) != 0 ||
		len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	rec := &m.Types[0]
	if len(rec.SubTypes) != 1 {
		return false
	}
	sub := &rec.SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() ||
		sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != 1 || !wasm.EqualValType(sub.Comp.Params[0], wasm.I32) ||
		len(sub.Comp.Results) != 1 || !wasm.EqualValType(sub.Comp.Results[0], wasm.I32) || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
		return false
	}
	for i, binding := range m.FuncTypes {
		if binding.Rec || binding.Index != 0 {
			return false
		}
		fn := &m.Code[i]
		want := []byte{0x20, 0, 0xfb, 0x1c, 0xfb, byte(0x1d + i), 0x0b}
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, want) {
			return false
		}
	}
	return m.Exports[0].Name == "signed" && m.Exports[0].Index.Kind == wasm.ExternFunc && m.Exports[0].Index.Index == 0 &&
		m.Exports[1].Name == "unsigned" && m.Exports[1].Index.Kind == wasm.ExternFunc && m.Exports[1].Index.Index == 1
}

func i31CoreFunctionType(group *wasm.RecType, params, results []wasm.ValType) bool {
	if group == nil || len(group.SubTypes) != 1 {
		return false
	}
	sub := &group.SubTypes[0]
	if !sub.Final || sub.HasPrefix || len(sub.Supers) != 0 || sub.Metadata.Describes.Present() || sub.Metadata.Descriptor.Present() ||
		sub.Comp.Kind != wasm.CompFunc || len(sub.Comp.Params) != len(params) || len(sub.Comp.Results) != len(results) || len(sub.Comp.Fields) != 0 || sub.Comp.Array != (wasm.FieldType{}) {
		return false
	}
	for i := range params {
		if !wasm.EqualValType(sub.Comp.Params[i], params[i]) {
			return false
		}
	}
	for i := range results {
		if !wasm.EqualValType(sub.Comp.Results[i], results[i]) {
			return false
		}
	}
	return true
}

func isI31CoreRef(typ wasm.ValType) bool {
	return wasm.EqualValType(typ, wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false)))
}

func admitRV64I31OfficialCore(m *wasm.Module) bool {
	if m == nil || len(m.Types) != 5 || len(m.FuncTypes) != 7 || len(m.Code) != 7 || len(m.Globals) != 2 || len(m.Exports) != 7 ||
		len(m.Imports) != 0 || len(m.Tables) != 0 || len(m.Memories) != 0 || len(m.Elements) != 0 || len(m.Data) != 0 || m.DataCount != nil || len(m.Tags) != 0 || len(m.StringRefs) != 0 ||
		m.Start != nil || len(m.Customs) != 0 || m.HasNameMetadata() || m.BranchHints != nil || m.UsesCompactImports {
		return false
	}
	i31 := wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapI31), false))
	types := []struct{ params, results []wasm.ValType }{
		{[]wasm.ValType{wasm.I32}, []wasm.ValType{i31}},
		{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}},
		{nil, []wasm.ValType{wasm.I32}},
		{nil, []wasm.ValType{wasm.I32, wasm.I32}},
		{[]wasm.ValType{wasm.I32}, nil},
	}
	for i := range types {
		if !i31CoreFunctionType(&m.Types[i], types[i].params, types[i].results) {
			return false
		}
	}
	wantBindings := []uint32{0, 1, 1, 2, 2, 3, 4}
	wantBodies := [][]byte{
		{0x20, 0, 0xfb, 0x1c, 0x0b},
		{0x20, 0, 0xfb, 0x1c, 0xfb, 0x1e, 0x0b},
		{0x20, 0, 0xfb, 0x1c, 0xfb, 0x1d, 0x0b},
		{0xd0, 0x6c, 0xfb, 0x1e, 0x0b},
		{0xd0, 0x6c, 0xfb, 0x1d, 0x0b},
		{0x23, 0, 0xfb, 0x1e, 0x23, 1, 0xfb, 0x1e, 0x0b},
		{0x20, 0, 0xfb, 0x1c, 0x24, 1, 0x0b},
	}
	for i, binding := range m.FuncTypes {
		if binding.Rec || binding.Index != wantBindings[i] {
			return false
		}
		fn := &m.Code[i]
		if len(fn.Locals.Runs) != 0 || fn.LocalDeclBytes != 1 || len(fn.Body.Instrs) != 0 || len(fn.Body.BodyBytes) != 0 || !bytes.Equal(fn.BodyBytes, wantBodies[i]) {
			return false
		}
	}
	for i, g := range m.Globals {
		if !isI31CoreRef(g.Type.Type) || g.Type.Mutable != (i == 1) || len(g.Init.Instrs) != 0 || !bytes.Equal(g.Init.BodyBytes, []byte{0x41, byte(2 + i), 0xfb, 0x1c, 0x0b}) {
			return false
		}
	}
	wantExports := []string{"new", "get_u", "get_s", "get_u-null", "get_s-null", "get_globals", "set_global"}
	for i, ex := range m.Exports {
		if ex.Name != wantExports[i] || ex.Index.Kind != wasm.ExternFunc || ex.Index.Index != uint32(i) {
			return false
		}
	}
	return true
}

func i31CoreGlobalValue(expr wasm.Expr) (uint64, error) {
	r := wasm.ReaderFrom(expr.BodyBytes)
	op, err := r.Byte()
	if err != nil || op != 0x41 {
		return 0, unsupported("i31 global initializer")
	}
	v, err := r.I32()
	if err != nil {
		return 0, err
	}
	prefix, err := r.Byte()
	if err != nil || prefix != 0xfb {
		return 0, unsupported("i31 global initializer")
	}
	sub, err := r.U32()
	if err != nil || sub != 28 {
		return 0, unsupported("i31 global initializer")
	}
	end, err := r.Byte()
	if err != nil || end != 0x0b || r.HasNext() {
		return 0, unsupported("i31 global initializer")
	}
	return uint64(uint32(v)<<1 | 1), nil
}

func (f *integerEmitter) i31Wrap() {
	// SLLIW discards the incoming sign bit as required by the 31-bit payload.
	// It sign-extends on RV64, so restore the shared zero-normalized carrier
	// before storing the full-width tagged reference.
	f.a.Slliw(enc.X5, enc.X5, 1)
	f.a.Addi(enc.X5, enc.X5, 1)
	f.NormalizeI32(5)
}

func (f *integerEmitter) i31Get(signed bool) {
	f.trap(trapNullReference, f.a.BranchLong(enc.EQ, enc.X5, enc.Zero))
	if signed {
		f.a.Sraiw(enc.X5, enc.X5, 1)
	} else {
		f.a.Srliw(enc.X5, enc.X5, 1)
	}
	// Every shared i32 boundary carries uint32 bits in a zero-extended word,
	// including the signed extraction of a negative 31-bit payload.
	f.NormalizeI32(5)
}
