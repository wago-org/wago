package wasm

import (
	"math/rand"
	"reflect"
	"testing"
)

func checkImmediateSkipParity(t *testing.T, op byte, data []byte) {
	t.Helper()
	for _, multi := range []bool{false, true} {
		for _, widths := range []memargWidths{{}, {fixed64: true}, {indexed64: []uint64{2, 1}}} {
			classifier := ModuleInstructionClassifier{widths: widths, multiMemory: multi}
			for _, start := range []int{0, 3} {
				encoded := append(make([]byte, start), data...)
				reference, direct := ReaderFrom(encoded), ReaderFrom(encoded)
				reference.pos, direct.pos = start, start
				var imm InstructionImmediate
				want := classifier.ClassifyInto(&reference, op, &imm)
				got := classifier.SkipImmediate(&direct, op)
				if reference.Offset() != direct.Offset() || !reflect.DeepEqual(want, got) {
					t.Fatalf("op=%x data=%x multi=%t widths=%+v start=%d: classify=%d/%v skip=%d/%v", op, data, multi, widths, start, reference.Offset(), want, direct.Offset(), got)
				}
			}
		}
	}
}

func TestSkipImmediateMatchesClassifier(t *testing.T) {
	rng := rand.New(rand.NewSource(47))
	for op := 0; op < 256; op++ {
		for _, data := range [][]byte{nil, {0}, {0x40, 1, 0}, {0x7b}, {0xff, 0xff, 0xff, 0xff, 0x0f}, {0x80, 0x80, 0x80, 0x80, 0x10}, {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1}} {
			for n := 0; n <= len(data); n++ {
				checkImmediateSkipParity(t, byte(op), data[:n])
			}
		}
		for n := 0; n < 64; n++ {
			data := make([]byte, n%25)
			_, _ = rng.Read(data)
			checkImmediateSkipParity(t, byte(op), data)
		}
	}
}

func FuzzSkipImmediateMatchesClassifier(f *testing.F) {
	for _, op := range []byte{0, 2, 0x0e, 0x10, 0x28, 0x3e, 0x41, 0x42, 0x43, 0x44, 0xfc, 0xfd, 0xfe, 0xfb} {
		f.Add(op, []byte{0x40, 1, 0x80, 0x80, 0x80, 0x80, 0x10})
		f.Add(op, []byte{})
	}
	f.Fuzz(func(t *testing.T, op byte, data []byte) {
		checkImmediateSkipParity(t, op, data)
	})
}
