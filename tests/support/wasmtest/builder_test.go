package wasmtest

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBinaryBuilderPrimitives(t *testing.T) {
	if got := ULEB(624485); !bytes.Equal(got, []byte{0xe5, 0x8e, 0x26}) {
		t.Fatalf("ULEB = %x", got)
	}
	for _, tc := range []struct {
		v    int64
		want []byte
	}{{0, []byte{0}}, {-1, []byte{0x7f}}, {64, []byte{0xc0, 0}}, {-65, []byte{0xbf, 0x7f}}} {
		if got := SLEB64(tc.v); !bytes.Equal(got, tc.want) {
			t.Fatalf("SLEB64(%d) = %x, want %x", tc.v, got, tc.want)
		}
		if tc.v >= -1<<31 && tc.v < 1<<31 {
			if got := SLEB32(int32(tc.v)); !bytes.Equal(got, tc.want) {
				t.Fatalf("SLEB32(%d) = %x, want %x", tc.v, got, tc.want)
			}
		}
	}
	if got := Name("hi"); !bytes.Equal(got, []byte{2, 'h', 'i'}) {
		t.Fatalf("Name = %x", got)
	}
	if got := Vec([]byte{1}, []byte{2, 3}); !bytes.Equal(got, []byte{2, 1, 2, 3}) {
		t.Fatalf("Vec = %x", got)
	}
	if got := Section(7, []byte{1, 2}); !bytes.Equal(got, []byte{7, 2, 1, 2}) {
		t.Fatalf("Section = %x", got)
	}
}

func TestStructuredBuilderHelpersProduceValidModule(t *testing.T) {
	nameMap := NameMap(NameAssoc{Index: 0, Name: "f"})
	if got := IndirectNameMap(IndirectNameAssoc{Index: 1, Names: []NameAssoc{{Index: 2, Name: "x"}}}); !bytes.Equal(got, []byte{1, 1, 1, 2, 1, 'x'}) {
		t.Fatalf("IndirectNameMap = %x", got)
	}
	if got := NameSubsection(1, nameMap); !bytes.Equal(got, []byte{1, 4, 1, 0, 1, 'f'}) {
		t.Fatalf("NameSubsection = %x", got)
	}
	if got := Custom("meta", []byte{9}); !bytes.Equal(got, []byte{0, 6, 4, 'm', 'e', 't', 'a', 9}) {
		t.Fatalf("Custom = %x", got)
	}
	if got := FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}); !bytes.Equal(got, []byte{0x60, 1, 0x7f, 1, 0x7e}) {
		t.Fatalf("FuncType = %x", got)
	}
	if got := GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}); !bytes.Equal(got, []byte{0x7f, 1, 0x41, 0, 0x0b}) {
		t.Fatalf("GlobalEntry = %x", got)
	}
	if got := ExportEntry("g", 3, 2); !bytes.Equal(got, []byte{1, 'g', 3, 2}) {
		t.Fatalf("ExportEntry = %x", got)
	}
	if got := GlobalImportEntry("m", "g", wasm.I64, false); !bytes.Equal(got, []byte{1, 'm', 1, 'g', 3, 0x7e, 0}) {
		t.Fatalf("GlobalImportEntry = %x", got)
	}
	if got := GlobalImportEntry("m", "g", wasm.I32, true); !bytes.Equal(got, []byte{1, 'm', 1, 'g', 3, 0x7f, 1}) {
		t.Fatalf("mutable GlobalImportEntry = %x", got)
	}
	if got := Code([]byte{0x0b}); !bytes.Equal(got, []byte{2, 0, 0x0b}) {
		t.Fatalf("Code = %x", got)
	}

	mod := Module(
		Section(1, Vec(FuncType(nil, nil))),
		Section(3, Vec(ULEB(0))),
		Section(7, Vec(ExportEntry("run", 0, 0))),
		Section(10, Vec(Code([]byte{0x0b}))),
	)
	decoded, err := wasm.DecodeModule(mod)
	if err != nil {
		t.Fatalf("DecodeModule: %v", err)
	}
	if err := wasm.ValidateModule(decoded); err != nil {
		t.Fatalf("ValidateModule: %v", err)
	}
}
