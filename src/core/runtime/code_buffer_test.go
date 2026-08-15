//go:build (linux || darwin || windows) && (amd64 || arm64)

package runtime

import (
	"bytes"
	"testing"
)

func TestCodeBufferGrowSealClose(t *testing.T) {
	b, err := NewCodeBuffer(1)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	want := bytes.Repeat([]byte{0xa5}, 5000)
	if err := b.Append(want[:3000]); err != nil {
		t.Fatal(err)
	}
	if err := b.AppendZeros(16); err != nil {
		t.Fatal(err)
	}
	space, err := b.AppendSpace(32)
	if err != nil {
		t.Fatal(err)
	}
	for i := range space {
		space[i] = 0x5a
	}
	if err := b.Append(want[3000:]); err != nil {
		t.Fatal(err)
	}
	if got := b.Bytes(); !bytes.Equal(got[:3000], want[:3000]) || !bytes.Equal(got[3048:], want[3000:]) || !bytes.Equal(got[3000:3016], make([]byte, 16)) || !bytes.Equal(got[3016:3048], bytes.Repeat([]byte{0x5a}, 32)) {
		t.Fatal("grown code image did not preserve appended bytes and zero padding")
	}
	if _, err := b.AppendSpace(-1); err == nil {
		t.Fatal("AppendSpace accepted a negative length")
	}
	base := b.Base()
	if base == 0 {
		t.Fatal("code image has no base address")
	}
	if err := b.Seal(); err != nil {
		t.Fatal(err)
	}
	if b.Base() != base {
		t.Fatalf("Seal moved code image: %#x -> %#x", base, b.Base())
	}
	if err := b.Seal(); err != nil {
		t.Fatalf("second Seal: %v", err)
	}
	if err := b.Append([]byte{1}); err == nil {
		t.Fatal("Append succeeded after Seal")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if got := b.Bytes(); got != nil {
		t.Fatalf("Bytes after Close = %d bytes, want nil", len(got))
	}
}

func TestCodeBufferAppendTail(t *testing.T) {
	b, err := NewCodeBuffer(1)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if err := b.Append([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	tail, err := b.AppendTail(5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 0 || cap(tail) < 5000 {
		t.Fatalf("AppendTail = len %d cap %d, want len 0 cap >= 5000", len(tail), cap(tail))
	}
	code := append(tail, 4, 5, 6, 7)
	if !b.CommitTail(code) {
		t.Fatal("CommitTail rejected code emitted into the reserved tail")
	}
	if got, want := b.Bytes(), []byte{1, 2, 3, 4, 5, 6, 7}; !bytes.Equal(got, want) {
		t.Fatalf("Bytes = %v, want %v", got, want)
	}

	before := append([]byte(nil), b.Bytes()...)
	if b.CommitTail([]byte{8, 9}) {
		t.Fatal("CommitTail accepted a detached allocation")
	}
	if got := b.Bytes(); !bytes.Equal(got, before) {
		t.Fatalf("rejected CommitTail changed Bytes: %v", got)
	}
	if err := b.Append([]byte{8, 9}); err != nil {
		t.Fatal(err)
	}
	if got, want := b.Bytes(), []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}; !bytes.Equal(got, want) {
		t.Fatalf("Append fallback after rejected CommitTail = %v, want %v", got, want)
	}
	if _, err := b.AppendTail(-1); err == nil {
		t.Fatal("AppendTail accepted a negative capacity")
	}
}

func TestCodeBufferTruncate(t *testing.T) {
	b, err := NewCodeBuffer(16)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if err := b.Append([]byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if err := b.Truncate(2); err != nil {
		t.Fatal(err)
	}
	if got, want := b.Bytes(), []byte{1, 2}; !bytes.Equal(got, want) {
		t.Fatalf("Bytes after Truncate = %v, want %v", got, want)
	}
	if err := b.Truncate(3); err == nil {
		t.Fatal("Truncate grew the logical image")
	}
	if err := b.Truncate(-1); err == nil {
		t.Fatal("Truncate accepted a negative length")
	}
	if err := b.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := b.Truncate(1); err == nil {
		t.Fatal("Truncate succeeded after Seal")
	}
}

func BenchmarkCodeImageTransition(b *testing.B) {
	code := bytes.Repeat([]byte{0x90}, 1<<20)
	b.Run("copy-and-seal", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			mem, _, err := MapCode(code)
			if err != nil {
				b.Fatal(err)
			}
			if err := Unmap(mem); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("seal-in-place", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			image, err := NewCodeBuffer(len(code))
			if err == nil {
				err = image.Append(code)
			}
			b.StartTimer()
			if err != nil {
				b.Fatal(err)
			}
			if err := image.Seal(); err != nil {
				b.Fatal(err)
			}
			b.StopTimer()
			if err := image.Close(); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
		}
	})
}
