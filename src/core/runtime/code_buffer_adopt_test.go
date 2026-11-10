package runtime

import (
	"bytes"
	"testing"
	"unsafe"
)

func TestHeapCodeBufferAdoptImage(t *testing.T) {
	b, err := NewHeapCodeBuffer(2)
	if err != nil {
		t.Fatal(err)
	}
	code := make([]byte, 3, 32)
	copy(code, []byte{1, 2, 3})
	base := unsafe.SliceData(code)
	if !b.AdoptHeapImage(code) || unsafe.SliceData(b.Bytes()) != base {
		t.Fatal("heap image was not adopted without a copy")
	}
	if b.AdoptHeapImage([]byte{9}) {
		t.Fatal("nonempty image accepted replacement")
	}
	if err := b.Append([]byte{4, 5}); err != nil {
		t.Fatal(err)
	}
	if unsafe.SliceData(b.Bytes()) != base || !bytes.Equal(b.Bytes(), []byte{1, 2, 3, 4, 5}) {
		t.Fatal("adopted capacity was not reused or image changed")
	}
	got, err := b.TakeHeap()
	if err != nil || unsafe.SliceData(got) != base {
		t.Fatalf("TakeHeap lost adopted image: %v", err)
	}
	if b.AdoptHeapImage([]byte{9}) {
		t.Fatal("transferred image accepted replacement")
	}
}

func TestCodeBufferAdoptImageDeclines(t *testing.T) {
	var absent *CodeBuffer
	if absent.AdoptHeapImage([]byte{1}) {
		t.Fatal("nil buffer accepted image")
	}
	b, err := NewCodeBuffer(8)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.AdoptHeapImage([]byte{1}) || len(b.Bytes()) != 0 {
		t.Fatal("mapped buffer accepted image or changed")
	}
	heap, err := NewHeapCodeBuffer(8)
	if err != nil {
		t.Fatal(err)
	}
	if heap.AdoptHeapImage(nil) || heap.AdoptHeapImage([]byte{}) {
		t.Fatal("empty image accepted")
	}
	if err := heap.Close(); err != nil {
		t.Fatal(err)
	}
	if heap.AdoptHeapImage([]byte{1}) {
		t.Fatal("closed image accepted replacement")
	}
}
