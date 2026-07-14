package wago

import "testing"

func TestMemoryAccessorsPortable(t *testing.T) {
	// A valid module containing one one-page linear memory and no functions.
	wasm := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0, 5, 3, 1, 0, 1}
	c, err := Compile(wasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	if !in.WriteUint8(0, 0x7f) || !in.WriteUint16Le(2, 0xabcd) || !in.WriteUint32Le(4, 0xdeadbeef) || !in.WriteUint64Le(8, 0x1122334455667788) || !in.WriteFloat32Le(16, 3.5) || !in.WriteFloat64Le(24, 2.5) {
		t.Fatal("typed write failed")
	}
	if v, ok := in.ReadUint8(0); !ok || v != 0x7f {
		t.Fatalf("ReadUint8 = %#x, %v", v, ok)
	}
	if v, ok := in.ReadUint16Le(2); !ok || v != 0xabcd {
		t.Fatalf("ReadUint16Le = %#x, %v", v, ok)
	}
	if v, ok := in.ReadUint32Le(4); !ok || v != 0xdeadbeef {
		t.Fatalf("ReadUint32Le = %#x, %v", v, ok)
	}
	if v, ok := in.ReadUint64Le(8); !ok || v != 0x1122334455667788 {
		t.Fatalf("ReadUint64Le = %#x, %v", v, ok)
	}
	if v, ok := in.ReadFloat32Le(16); !ok || v != 3.5 {
		t.Fatalf("ReadFloat32Le = %v, %v", v, ok)
	}
	if v, ok := in.ReadFloat64Le(24); !ok || v != 2.5 {
		t.Fatalf("ReadFloat64Le = %v, %v", v, ok)
	}
	if !in.Write(40, []byte{1, 2, 3}) {
		t.Fatal("Write failed")
	}
	if got, ok := in.Read(40, 3); !ok || string(got) != "\x01\x02\x03" {
		t.Fatalf("Read = %v, %v", got, ok)
	}

	const end = 65536
	if in.WriteUint8(end, 1) || in.WriteUint16Le(end-1, 1) || in.WriteUint32Le(end-3, 1) || in.WriteUint64Le(end-7, 1) || in.Write(65535, []byte{1, 2}) {
		t.Fatal("out-of-bounds write succeeded")
	}
	if _, ok := in.ReadUint8(end); ok {
		t.Fatal("out-of-bounds byte read succeeded")
	}
	if _, ok := in.ReadUint16Le(end - 1); ok {
		t.Fatal("out-of-bounds uint16 read succeeded")
	}
	if _, ok := in.ReadUint32Le(end - 3); ok {
		t.Fatal("out-of-bounds uint32 read succeeded")
	}
	if _, ok := in.ReadUint64Le(end - 7); ok {
		t.Fatal("out-of-bounds uint64 read succeeded")
	}
	if _, ok := in.Read(65535, 2); ok {
		t.Fatal("out-of-bounds slice read succeeded")
	}
}
