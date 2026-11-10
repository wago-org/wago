package wasm

import (
	"bytes"
	"testing"
)

func TestDecodedDataOffsetViewsPreserveMixedSegments(t *testing.T) {
	payload := []byte{4,
		0, 0x41, 7, 0x0b, 2, 'a', 'b',
		1, 1, 'c',
		2, 1, 0x23, 0, 0x0b, 1, 'd',
		2, 1, 0x42, 5, 0x0b, 0,
	}
	r := reader{data: payload}
	var dm directModule
	if err := decodeDirectDataSection(&dm, &r); err != nil {
		t.Fatal(err)
	}
	wants := [][]byte{{0x41, 7, 0x0b}, nil, {0x23, 0, 0x0b}, {0x42, 5, 0x0b}}
	for i, d := range dm.m.Data {
		if !bytes.Equal(d.Mode.Offset.BodyBytes, wants[i]) || len(d.Mode.Offset.Instrs) != 0 {
			t.Fatalf("segment%d offset changed: %x", i, d.Mode.Offset.BodyBytes)
		}
	}
	if dm.m.Data[1].Mode.Kind != DataPassive || dm.m.Data[2].Mode.Mem != 1 || dm.m.Data[3].Mode.Mem != 1 {
		t.Fatal("segment modes changed")
	}
	start := bytes.Index(payload, []byte{'a', 'b'})
	if &dm.m.Data[0].Init[0] != &payload[start] {
		t.Fatal("payload is no longer borrowed")
	}
}

func TestByteBackedDataOffsetViewsValidateAllForms(t *testing.T) {
	for _, tc := range []struct {
		name           string
		memory, offset []byte
		imports        bool
		features       ValidationFeatures
	}{
		{"i32", []byte{1, 0, 1}, []byte{0x41, 7, 0x0b}, false, ValidationFeatures{}},
		{"i64", []byte{1, 4, 1}, []byte{0x42, 7, 0x0b}, false, ValidationFeatures{}},
		{"global", []byte{1, 0, 1}, []byte{0x23, 0, 0x0b}, true, ValidationFeatures{}},
		{"extended", []byte{1, 0, 1}, []byte{0x23, 0, 0x41, 1, 0x6a, 0x0b}, true, ValidationFeatures{ExtendedConstGlobals: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sections [][]byte
			if tc.imports {
				sections = append(sections, section(secImport, 1, 1, 'm', 1, 'g', 3, 0x7f, 0))
			}
			sections = append(sections, section(secMemory, tc.memory...))
			data := append([]byte{1, 0}, tc.offset...)
			data = append(data, 1, 42)
			sections = append(sections, section(secData, data...))
			dm, err := DecodeModuleByteBacked(module(sections...))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(dm.Module.Data[0].Mode.Offset.BodyBytes, tc.offset) {
				t.Fatal("ordinary Module lost offset bytes")
			}
			if err := ValidateDecodedByteBackedModuleWithConfig(dm, tc.features, 1, ValidationLimits{}); err != nil {
				t.Fatal(err)
			}
			if err := ValidateModuleWithAnalysis(dm.Module, tc.features, 1, ValidationLimits{}, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestByteBackedDataValidationReadsCurrentOffsetView(t *testing.T) {
	data := module(section(secMemory, 1, 0, 1), section(secData, 1, 0, 0x41, 0, 0x0b, 0))
	dm, err := DecodeModuleByteBacked(data)
	if err != nil {
		t.Fatal(err)
	}
	dm.Module.Data[0].Mode.Offset.BodyBytes = []byte{0x42, 0, 0x0b}
	if err := ValidateDecodedByteBackedModule(dm); err == nil {
		t.Fatal("byte-backed validation ignored changed offset type")
	}
	if err := ValidateModule(dm.Module); err == nil {
		t.Fatal("ordinary validation ignored changed offset type")
	}
}
