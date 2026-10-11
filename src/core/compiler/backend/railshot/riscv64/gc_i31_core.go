//go:build linux && !tinygo

package riscv64

import (
	"bytes"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// rv64I31CoreWire is the only public GC source admitted by the first RV64
// non-allocating i31 checkpoint. The i31 carrier is created and consumed inside
// each function; both public signatures remain numeric.
const rv64I31CoreWire = "\x00\x61\x73\x6d\x01\x00\x00\x00" +
	"\x01\x06\x01\x60\x01\x7f\x01\x7f" +
	"\x03\x03\x02\x00\x00" +
	"\x07\x15\x02\x06signed\x00\x00\x08unsigned\x00\x01" +
	"\x0a\x13\x02\x08\x00\x20\x00\xfb\x1c\xfb\x1d\x0b" +
	"\x08\x00\x20\x00\xfb\x1c\xfb\x1e\x0b"

func admitRV64I31CoreWire(data []byte) bool {
	return len(data) == len(rv64I31CoreWire) && string(data) == rv64I31CoreWire
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
