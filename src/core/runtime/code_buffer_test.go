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
	if err := b.Append(want[3000:]); err != nil {
		t.Fatal(err)
	}
	if got := b.Bytes(); !bytes.Equal(got[:3000], want[:3000]) || !bytes.Equal(got[3016:], want[3000:]) || !bytes.Equal(got[3000:3016], make([]byte, 16)) {
		t.Fatal("grown code image did not preserve appended bytes and zero padding")
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
