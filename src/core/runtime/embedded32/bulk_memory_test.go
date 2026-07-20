package embedded32

import (
	"bytes"
	"testing"
)

func TestBulkMemorySemantics(t *testing.T) {
	memory, err := NewLinearMemory(make([]byte, WasmPageSize), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	store := NewDataStore([]DataSegmentInit{{Bytes: []byte("abcdef")}, {Bytes: []byte("active"), Dropped: true}})
	if trap := store.Init(memory, 0, 4, 1, 4); trap != TrapNone || !bytes.Equal(memory.Bytes()[4:8], []byte("bcde")) {
		t.Fatalf("init trap=%d bytes=%q", trap, memory.Bytes()[4:8])
	}
	if trap := store.Init(memory, 0, WasmPageSize, 6, 0); trap != TrapNone {
		t.Fatalf("zero-length boundary trap=%d", trap)
	}
	if trap := store.Init(memory, 1, 0, 0, 1); trap != TrapMemoryOutOfBounds {
		t.Fatalf("active segment trap=%d", trap)
	}
	if !store.Drop(0) || !store.Drop(0) {
		t.Fatal("data.drop was not idempotent")
	}
	if trap := store.Init(memory, 0, 0, 0, 1); trap != TrapMemoryOutOfBounds {
		t.Fatalf("dropped segment trap=%d", trap)
	}

	copy(memory.Bytes()[0:8], []byte("12345678"))
	if trap := memory.Copy(2, 0, 6); trap != TrapNone || string(memory.Bytes()[:8]) != "12123456" {
		t.Fatalf("overlap copy trap=%d bytes=%q", trap, memory.Bytes()[:8])
	}
	before := append([]byte(nil), memory.Bytes()[:8]...)
	if trap := memory.Copy(WasmPageSize-2, 0, 4); trap != TrapMemoryOutOfBounds || !bytes.Equal(before, memory.Bytes()[:8]) {
		t.Fatalf("oob copy trap=%d mutated=%t", trap, !bytes.Equal(before, memory.Bytes()[:8]))
	}
	if trap := memory.Fill(1, 0xaa, 3); trap != TrapNone || !bytes.Equal(memory.Bytes()[1:4], []byte{0xaa, 0xaa, 0xaa}) {
		t.Fatalf("fill trap=%d bytes=%x", trap, memory.Bytes()[1:4])
	}
	if trap := memory.Fill(WasmPageSize-1, 1, 2); trap != TrapMemoryOutOfBounds {
		t.Fatalf("oob fill trap=%d", trap)
	}
}
