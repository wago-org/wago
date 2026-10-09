package amd64

import (
	"bytes"
	"testing"
)

// These literal encodings also guard the ordinary build: observer hooks must
// not change the generated load/store instruction or its address operands.
func TestIndexedVectorEncoding(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*Asm)
		want []byte
	}{
		{"SSE-load", func(a *Asm) { a.MovdquLoadIdx(10, R8, R9, 127) }, []byte{0xf3, 0x47, 0x0f, 0x6f, 0x54, 0x08, 127}},
		{"SSE-store", func(a *Asm) { a.MovdquStoreIdx(R8, R9, 10, 127) }, []byte{0xf3, 0x47, 0x0f, 0x7f, 0x54, 0x08, 127}},
		{"AVX-load", func(a *Asm) { a.VMovdquLoadIdx(10, R8, R9, 127) }, []byte{0xc4, 0x01, 0x7a, 0x6f, 0x54, 0x08, 127}},
		{"AVX-raw-load", func(a *Asm) { a.VMovdquIdx(0x6f, 10, R8, R9, 127) }, []byte{0xc4, 0x01, 0x7a, 0x6f, 0x54, 0x08, 127}},
		{"AVX-store", func(a *Asm) { a.VMovdquStoreIdx(R8, R9, 10, 127) }, []byte{0xc4, 0x01, 0x7a, 0x7f, 0x54, 0x08, 127}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a Asm
			tc.emit(&a)
			if !bytes.Equal(a.B, tc.want) {
				t.Fatalf("bytes=%x want %x", a.B, tc.want)
			}
		})
	}
}

func BenchmarkIndexedVectorEncoding(b *testing.B) {
	for _, tc := range []struct {
		name string
		emit func(*Asm)
	}{
		{"SSE-load", func(a *Asm) { a.MovdquLoadIdx(10, R8, R9, 127) }},
		{"AVX-load", func(a *Asm) { a.VMovdquLoadIdx(10, R8, R9, 127) }},
		{"AVX-raw-load", func(a *Asm) { a.VMovdquIdx(0x6f, 10, R8, R9, 127) }},
		{"SSE-store", func(a *Asm) { a.MovdquStoreIdx(R8, R9, 10, 127) }},
		{"AVX-store", func(a *Asm) { a.VMovdquStoreIdx(R8, R9, 10, 127) }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			a := Asm{B: make([]byte, 0, 16)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.B = a.B[:0]
				tc.emit(&a)
			}
			b.StopTimer()
			if len(a.B) != 7 {
				b.Fatalf("instruction size=%d", len(a.B))
			}
		})
	}
}
