//go:build linux && amd64

package wago

import (
	"encoding/binary"
	"testing"
)

// memprogWasm (wago_test.go) declares a linear memory, so it gives us an
// instance to exercise the typed accessors without running its functions.

func TestMemoryAccessors(t *testing.T) {
	c, err := Compile(nil, memprogWasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	lin := in.Memory().Bytes()
	if len(lin) < 64 {
		t.Fatalf("linear memory too small: %d", len(lin))
	}

	if !in.WriteUint32Le(8, 0xDEADBEEF) {
		t.Fatal("WriteUint32Le")
	}
	if v, ok := in.ReadUint32Le(8); !ok || v != 0xDEADBEEF {
		t.Fatalf("ReadUint32Le = %#x, %v", v, ok)
	}
	// The unsafe accessor and encoding/binary must agree (little-endian).
	if got := binary.LittleEndian.Uint32(lin[8:]); got != 0xDEADBEEF {
		t.Fatalf("raw slice = %#x, want matching accessor", got)
	}

	if !in.WriteUint64Le(16, 0x1122334455667788) {
		t.Fatal("WriteUint64Le")
	}
	if v, ok := in.ReadUint64Le(16); !ok || v != 0x1122334455667788 {
		t.Fatalf("ReadUint64Le = %#x, %v", v, ok)
	}
	if !in.WriteUint16Le(24, 0xABCD) {
		t.Fatal("WriteUint16Le")
	}
	if v, ok := in.ReadUint16Le(24); !ok || v != 0xABCD {
		t.Fatalf("ReadUint16Le = %#x, %v", v, ok)
	}
	if !in.WriteUint8(26, 0x7F) {
		t.Fatal("WriteUint8")
	}
	if v, ok := in.ReadUint8(26); !ok || v != 0x7F {
		t.Fatalf("ReadUint8 = %#x, %v", v, ok)
	}
	if !in.WriteFloat32Le(28, 3.5) {
		t.Fatal("WriteFloat32Le")
	}
	if v, ok := in.ReadFloat32Le(28); !ok || v != 3.5 {
		t.Fatalf("ReadFloat32Le = %v, %v", v, ok)
	}
	if !in.WriteFloat64Le(32, 2.5) {
		t.Fatal("WriteFloat64Le")
	}
	if v, ok := in.ReadFloat64Le(32); !ok || v != 2.5 {
		t.Fatalf("ReadFloat64Le = %v, %v", v, ok)
	}
	if !in.Write(40, []byte("hello")) {
		t.Fatal("Write")
	}
	if b, ok := in.Read(40, 5); !ok || string(b) != "hello" {
		t.Fatalf("Read = %q, %v", b, ok)
	}

	// Out-of-range access returns false and writes nothing.
	end := uint32(len(lin))
	before := append([]byte(nil), lin[end-4:]...)
	if _, ok := in.ReadUint32Le(end - 3); ok {
		t.Fatal("ReadUint32Le straddling the end should fail")
	}
	if in.WriteUint32Le(end-3, 0x99999999) {
		t.Fatal("WriteUint32Le straddling the end should fail")
	}
	if _, ok := in.ReadUint8(end); ok {
		t.Fatal("ReadUint8 at end should fail")
	}
	if _, ok := in.Read(end-2, 5); ok {
		t.Fatal("Read past the end should fail")
	}
	if in.Write(end-2, []byte("xxxxx")) {
		t.Fatal("Write past the end should fail")
	}
	for i, b := range lin[end-4:] {
		if b != before[i] {
			t.Fatalf("rejected write mutated memory at end-%d", 4-i)
		}
	}
}

// BenchmarkInstanceUint32 exercises the typed accessor on the real mmap-backed
// linear memory (no DCE): a single bounds-checked aligned load/store, ~3-4x
// faster than the encoding/binary idiom under TinyGo (see docs/tinygo.md).
func BenchmarkInstanceUint32(b *testing.B) {
	c, err := Compile(nil, memprogWasm)
	if err != nil {
		b.Fatalf("Compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		b.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	var sum uint32
	for i := 0; i < b.N; i++ {
		off := uint32(i&0x3FFF) * 4 // 0..65532, in range for a 64 KiB page
		in.WriteUint32Le(off, uint32(i))
		v, _ := in.ReadUint32Le(off)
		sum += v
	}
	_ = sum
}
