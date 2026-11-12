package wasm

import "testing"

func checkScalarMemorySkip(t *testing.T, data []byte) {
	t.Helper()
	for _, multi := range []bool{false, true} {
		for _, widths := range []memargWidths{{}, {fixed64: true}, {indexed64: []uint64{2, 1}}} {
			classifier := ModuleInstructionClassifier{widths: widths, multiMemory: multi}
			reference, direct := ReaderFrom(data), ReaderFrom(data)
			var imm InstructionImmediate
			want := classifier.ClassifyInto(&reference, 0x28, &imm)
			got := classifier.SkipScalarMemoryImmediate(&direct)
			if reference.Offset() != direct.Offset() || (want == nil) != (got == nil) {
				t.Fatalf("%x multi=%t: reference=%d/%v direct=%d/%v", data, multi, reference.Offset(), want, direct.Offset(), got)
			}
			if want != nil {
				a, b := want.(*DecodeError), got.(*DecodeError)
				if a.Code != b.Code || a.Offset != b.Offset {
					t.Fatalf("%x: error mismatch %v vs %v", data, a, b)
				}
			}
		}
	}
}
func TestSkipScalarMemoryImmediateParity(t *testing.T) {
	checkScalarMemorySkip(t, nil)
	for alignment := 0; alignment < 256; alignment++ {
		for _, index := range []uint32{0, 1, 64, 65, 128} {
			for _, offset := range [][]byte{{0}, {0x80}, {0xff, 0xff, 0xff, 0xff, 0x0f}, {0x80, 0x80, 0x80, 0x80, 0x10}, {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1}} {
				data := u32(uint32(alignment))
				if alignment >= 64 && alignment < 128 {
					data = append(data, u32(index)...)
				}
				data = append(data, offset...)
				checkScalarMemorySkip(t, data)
				for n := 0; n < len(data); n++ {
					checkScalarMemorySkip(t, data[:n])
				}
			}
		}
	}
	for _, data := range [][]byte{{0x80, 0, 0}, {0xc0, 0, 0, 0}, {0xff, 0xff, 0xff, 0xff, 0x7f}} {
		checkScalarMemorySkip(t, data)
	}
}
func FuzzSkipScalarMemoryImmediate(f *testing.F) {
	for _, data := range [][]byte{{}, {2, 0}, {0x40, 1, 0}, {0x80}, {0xc0, 0, 0, 0}} {
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) { checkScalarMemorySkip(t, data) })
}
