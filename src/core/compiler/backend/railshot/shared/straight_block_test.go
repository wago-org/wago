package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestStraightBlockSplicedCallProof(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	for _, tc := range []struct {
		body []byte
		want bool
	}{
		{[]byte{0x10, 7, 0x0b}, true},
		{[]byte{0x02, 0x40, 0x10, 7, 0x0b, 0x0b}, true},
		{[]byte{0x10, 7, 0x10, 8, 0x0b}, false},
		{[]byte{0x10, 0x80}, false},
		{[]byte{0x10, 7, 0x0c, 0, 0x0b}, false},
		{[]byte{0x03, 0x40, 0x10, 7, 0x0b, 0x0b}, false},
		{[]byte{0x04, 0x40, 0x10, 7, 0x0b, 0x0b}, false},
		{[]byte{0x12, 7, 0x0b}, false},
	} {
		r := wasm.ReaderFrom(tc.body)
		got := IsStraightLineBlockWithSplicedCalls(&r, classifier, func(index uint32) bool { return index == 7 })
		if got != tc.want || r.Offset() != 0 {
			t.Fatalf("body=%x got=%t want=%t reader=%d", tc.body, got, tc.want, r.Offset())
		}
		if IsStraightLineBlock(&r, classifier) {
			t.Fatalf("default proof admitted call in %x", tc.body)
		}
	}
	for _, count := range []int{31, 32} {
		body := make([]byte, 0, 2*count+1)
		for range count {
			body = append(body, 0x10, 7)
		}
		body = append(body, 0x0b)
		r := wasm.ReaderFrom(body)
		if got := IsStraightLineBlockWithSplicedCalls(&r, classifier, func(uint32) bool { return true }); got != (count == 31) {
			t.Fatalf("call count=%d accepted=%t", count, got)
		}
	}
}

func TestStraightBlockProofRejectsControlAndCalls(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	for _, body := range [][]byte{
		{0x01, 0x0b}, {0x02, 0x40, 0x0b, 0x0b}, {0xfc, 11, 0, 0x0b}, {0x41, 1, 0x1a, 0x0b}, {0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x1a, 0x0b},
	} {
		r := wasm.ReaderFrom(body)
		if !IsStraightLineBlock(&r, classifier) || r.Offset() != 0 {
			t.Fatalf("rejected %x or changed reader", body)
		}
	}
	for _, body := range [][]byte{{}, {0x00, 0x0b}, {0x03, 0x40, 0x0b, 0x0b}, {0x0c, 0, 0x0b}, {0x0d, 0, 0x0b}, {0x0f, 0x0b}, {0x10, 0, 0x0b}, {0x40, 0, 0x0b}, {0xfc, 10, 0, 0, 0x0b}, {0xfc, 15, 0, 0x0b}, {0xfc, 11}, {0xfb, 0, 0x0b}, {0xfe, 0, 0x0b}, {0x41}} {
		r := wasm.ReaderFrom(body)
		if IsStraightLineBlock(&r, classifier) || r.Offset() != 0 {
			t.Fatalf("accepted %x or changed reader", body)
		}
	}
	for _, length := range []int{32, 256} {
		body := make([]byte, length+1)
		for i := 0; i < length; i++ {
			body[i] = 1
		}
		body[length] = 0x0b
		r := wasm.ReaderFrom(body)
		if IsStraightLineBlock(&r, classifier) {
			t.Fatalf("accepted unbounded block length %d", length)
		}
	}
}

func TestStraightBlockNativeAtomics(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	for sub := byte(0x10); sub <= 0x4e; sub++ {
		r := wasm.ReaderFrom([]byte{0xfe, sub, 0, 0, 0x0b})
		if !IsStraightLineBlock(&r, classifier) || r.Offset() != 0 {
			t.Fatalf("rejected native atomic %x", sub)
		}
	}
	r := wasm.ReaderFrom([]byte{0xfe, 3, 0, 0x0b})
	if !IsStraightLineBlock(&r, classifier) {
		t.Fatal("rejected native fence")
	}
	for _, body := range [][]byte{{0xfe, 0, 2, 0, 0x0b}, {0xfe, 1, 2, 0, 0x0b}, {0xfe, 2, 3, 0, 0x0b}, {0xfe, 3, 1, 0x0b}, {0xfe, 0x10, 0}, {0xfe, 0x4f, 0, 0, 0x0b}} {
		r := wasm.ReaderFrom(body)
		if IsStraightLineBlock(&r, classifier) || r.Offset() != 0 {
			t.Fatalf("accepted helper/invalid opcode %x", body)
		}
	}
}

func TestStraightBlockNestedDepthAndEdges(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	for _, depth := range []int{1, 8, 9} {
		var body []byte
		for range depth {
			body = append(body, 0x02, 0x40)
		}
		for range depth + 1 {
			body = append(body, 0x0b)
		}
		r := wasm.ReaderFrom(body)
		if got := IsStraightLineBlock(&r, classifier); got != (depth <= 8) || r.Offset() != 0 {
			t.Fatalf("depth=%d accepted=%t reader=%d", depth, got, r.Offset())
		}
	}
	for _, body := range [][]byte{{0x02}, {0x02, 0x40, 0x0b}, {0x02, 0x40, 0x0c, 0, 0x0b, 0x0b}, {0x02, 0x40, 0x10, 0, 0x0b, 0x0b}, {0x02, 0x40, 0x03, 0x40, 0x0b, 0x0b, 0x0b}} {
		r := wasm.ReaderFrom(body)
		if IsStraightLineBlock(&r, classifier) || r.Offset() != 0 {
			t.Fatalf("accepted malformed/branch/call/loop body %x", body)
		}
	}
}

func TestFallthroughBlockContainsOnlyPrivateIfEdges(t *testing.T) {
	classifier := wasm.ModuleInstructionClassifier{}
	for _, test := range []struct {
		name string
		body []byte
		want bool
	}{
		{"if-else", []byte{0x20, 0, 0x04, 0x7f, 0x41, 7, 0x05, 0x41, 9, 0x0b, 0x0b}, true},
		{"nested-if", []byte{0x20, 0, 0x04, 0x40, 0x20, 1, 0x04, 0x40, 0x01, 0x0b, 0x05, 0x01, 0x0b, 0x0b}, true},
		{"outward-br", []byte{0x20, 0, 0x04, 0x40, 0x0c, 1, 0x0b, 0x0b}, false},
		{"private-br", []byte{0x20, 0, 0x04, 0x40, 0x0c, 0, 0x0b, 0x0b}, false},
		{"br-if", []byte{0x20, 0, 0x04, 0x40, 0x20, 0, 0x0d, 1, 0x0b, 0x0b}, false},
		{"return", []byte{0x20, 0, 0x04, 0x40, 0x0f, 0x0b, 0x0b}, false},
		{"loop", []byte{0x03, 0x40, 0x01, 0x0b, 0x0b}, false},
		{"call", []byte{0x20, 0, 0x04, 0x40, 0x10, 0, 0x0b, 0x0b}, false},
		{"stray-else", []byte{0x05, 0x0b}, false},
		{"duplicate-else", []byte{0x20, 0, 0x04, 0x40, 0x01, 0x05, 0x01, 0x05, 0x01, 0x0b, 0x0b}, false},
	} {
		r := wasm.NewReader(test.body)
		if got := IsFallthroughBlock(r, classifier, nil, true); got != test.want || r.Offset() != 0 {
			t.Fatalf("%s got%v want%v offset%d", test.name, got, test.want, r.Offset())
		}
		if IsStraightLineBlock(r, classifier) {
			t.Fatalf("%s admitted by original API", test.name)
		}
	}
}
