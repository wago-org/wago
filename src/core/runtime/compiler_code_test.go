package runtime

import (
	"bytes"
	"errors"
	"testing"
)

func TestCompilerBufferAnonymousContract(t *testing.T) {
	b, err := NewCompilerCodeBuffer(16)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	want := []byte{1, 2, 3, 4}
	if err := b.Append(want); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReclaimCompilerPages(0, len(want)); err != nil {
		t.Fatal(err)
	}
	if err := b.FinishCompilerCode(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatal("compiler image changed")
	}
	for _, r := range [][2]int{{-1, 1}, {3, 2}, {0, 5}} {
		if _, err := b.ReclaimCompilerPages(r[0], r[1]); err == nil {
			t.Fatalf("accepted range %v", r)
		}
	}
}

func TestCodeBufferCloseAuxiliaryErrorDoesNotRetainUnmappedMemory(t *testing.T) {
	b, err := NewCodeBuffer(16)
	if err != nil {
		t.Fatal(err)
	}
	marker := errors.New("close auxiliary handle")
	unmapped, closed := 0, 0
	b.ops = &codeBufferOps{
		unmap:      func(mem []byte) error { unmapped++; return munmap(mem) },
		afterUnmap: func([]byte) error { closed++; return marker },
	}
	if err := b.Close(); !errors.Is(err, marker) {
		t.Fatalf("Close=%v", err)
	}
	if b.mem != nil || b.retired != nil {
		t.Fatal("retained an unmapped image")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if unmapped != 1 || closed != 1 {
		t.Fatalf("cleanup counts %d/%d", unmapped, closed)
	}
}

func TestCodeBufferGrowthAuxiliaryErrorReleasesBothMappings(t *testing.T) {
	b, err := NewCodeBuffer(16)
	if err != nil {
		t.Fatal(err)
	}
	marker := errors.New("close retired handle")
	unmapped, closed := 0, 0
	b.ops = &codeBufferOps{
		unmap: func(mem []byte) error { unmapped++; return munmap(mem) },
		afterUnmap: func([]byte) error {
			closed++
			if closed == 1 {
				return marker
			}
			return nil
		},
	}
	if err := b.AppendZeros(len(b.mem)); err != nil {
		t.Fatal(err)
	}
	if err := b.AppendZeros(1); !errors.Is(err, marker) {
		t.Fatalf("growth=%v", err)
	}
	if b.mem != nil || b.retired != nil || !b.closed {
		t.Fatal("growth retained an unmapped image")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if unmapped != 2 || closed != 2 {
		t.Fatalf("cleanup counts %d/%d", unmapped, closed)
	}
}

func TestCodeBufferTakeAuxiliaryErrorKeepsMappingOwned(t *testing.T) {
	b, err := NewCodeBuffer(16)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	marker := errors.New("close transfer handle")
	b.ops = &codeBufferOps{take: func([]byte) error { return marker }}
	mem, base, err := b.Take()
	if !errors.Is(err, marker) || mem != nil || base != 0 {
		t.Fatalf("Take=%v/%x/%v", mem, base, err)
	}
	if b.mem == nil || b.closed {
		t.Fatal("lost mapping ownership on transfer failure")
	}
}
