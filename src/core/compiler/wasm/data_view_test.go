package wasm

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"
)

func TestCompilerDataViewMatchesExpandedDecode(t *testing.T) {
	features := ValidationFeatures{MultiMemory: true, ExtendedConstGlobals: true}
	mixed := []byte{3, 0, 0x41, 0, 0x0b, 0, 1, 2, 2, 3, 2, 0, 0x41, 0x7f, 0x0b, 1, 0x90}
	for _, tc := range []struct {
		name         string
		memory, data []byte
		valid        bool
	}{
		{"empty", []byte{1, 0, 1}, []byte{0}, true},
		{"noncanonical-i32", []byte{1, 0, 1}, []byte{1, 0, 0x41, 0x81, 0, 0x0b, 2, 0xaa, 0xbb}, true},
		{"mixed-modes-and-empty-payload", []byte{1, 0, 1}, mixed, true},
		{"full-memory-index", []byte{1, 0, 1}, []byte{1, 2, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x41, 0, 0x0b, 0}, false},
		{"memory64", []byte{1, 4, 1}, []byte{1, 0, 0x42, 1, 0x0b, 1, 7}, true},
		{"extended-offset", []byte{1, 0, 1}, []byte{1, 0, 0x41, 1, 0x41, 2, 0x6a, 0x0b, 1, 7}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bytes := module(section(secMemory, tc.memory...), section(secData, tc.data...))
			expanded, err := DecodeModuleForValidationWithFeatures(bytes, features)
			if err != nil {
				t.Fatal(err)
			}
			view, err := DecodeModuleForCompilation(bytes, features)
			if err != nil {
				t.Fatal(err)
			}
			if view.DataSegmentCount() != len(expanded.Data) || !reflect.DeepEqual(view.ExpandedDataSegments(), expanded.Data) {
				t.Fatal("data projection changed decoded records")
			}
			if len(expanded.Data) != 0 && (view.dataView == nil || view.Data != nil) {
				t.Fatal("compiler eagerly expanded data records")
			}
			wantErr := ValidateModuleWithFeatures(expanded, features)
			gotErr := ValidateModuleWithFeatures(view, features)
			if (gotErr == nil) != tc.valid || !reflect.DeepEqual(gotErr, wantErr) {
				t.Fatalf("validation changed: got %v, want %v", gotErr, wantErr)
			}
			want := append([]Data(nil), expanded.Data...)
			for i := range want {
				if view.DataSegmentIsPassive(i) != (want[i].Mode.Kind == DataPassive) {
					t.Fatal("mode-only projection changed segment kind")
				}
				if want[i].Init != nil {
					want[i].Init = append([]byte{}, want[i].Init...)
				}
				if want[i].Mode.Offset.BodyBytes != nil {
					want[i].Mode.Offset.BodyBytes = append([]byte{}, want[i].Mode.Offset.BodyBytes...)
				}
			}
			// Preserve nil-versus-empty vector identity for the empty-section case.
			if len(expanded.Data) == 0 {
				want = expanded.Data
			}
			expanded = nil
			bytes = nil
			runtime.GC()
			if !reflect.DeepEqual(view.ExpandedDataSegments(), want) {
				t.Fatal("data view lost input ownership")
			}
		})
	}
	if unsafe.Sizeof(dataSegmentSpan{}) != 20 {
		t.Fatal("data spans grew pointer-rich storage")
	}
}

func TestCompilerDataViewCountsAndFallback(t *testing.T) {
	bytes := module(section(secMemory, 1, 0, 1), section(secDataCount, 2), section(secData, 2, 0, 0x41, 0, 0x0b, 1, 7, 1, 1, 9))
	m, err := DecodeModuleForCompilation(bytes, ValidationFeatures{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	copy := m.ExpandedDataSegments()
	copy[0].Mode.Mem = 42
	if m.DataSegment(0).Mode.Mem != 0 || m.Data != nil {
		t.Fatal("expanded compatibility view changed module identity")
	}
	m.Data = []Data{{Mode: DataMode{Kind: DataPassive}, Init: []byte{1}}}
	if m.DataSegmentCount() != 1 || m.DataSegment(0).Init[0] != 1 {
		t.Fatal("explicit expanded records did not take precedence")
	}
}

func TestCompilerDataViewPreservesDecodeErrors(t *testing.T) {
	for _, body := range [][]byte{{1}, {1, 3}, {1, 0, 0x41}, {1, 0, 0x41, 0, 0x0b, 2, 1}, {1, 2, 0x80}, {0xff, 0xff, 0xff, 0xff, 0x0f}} {
		bytes := module(section(secData, body...))
		_, old := DecodeModuleForValidationWithFeatures(bytes, ValidationFeatures{})
		_, compact := DecodeModuleForCompilation(bytes, ValidationFeatures{})
		if old == nil || !reflect.DeepEqual(compact, old) {
			t.Fatalf("%x: got %v, want %v", body, compact, old)
		}
	}
}
