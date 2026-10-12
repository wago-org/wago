package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDecodeAtomicMatrix(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bytes      []byte
		class      AtomicClass
		op         AtomicOperation
		size       uint8
		resultSize uint8
	}{
		{"notify", []byte{0x00, 0x02, 0x07}, AtomicNotify, AtomicNone, 4, 4},
		{"wait64", []byte{0x02, 0x03, 0x07}, AtomicWait, AtomicNone, 8, 4},
		{"fence", []byte{0x03, 0x00}, AtomicFence, AtomicNone, 0, 0},
		{"i32.load8", []byte{0x12, 0x00, 0x07}, AtomicLoad, AtomicNone, 1, 4},
		{"i64.store32", []byte{0x1d, 0x02, 0x07}, AtomicStore, AtomicNone, 4, 8},
		{"i32.add", []byte{0x1e, 0x02, 0x07}, AtomicRMW, AtomicAdd, 4, 4},
		{"i64.sub16", []byte{0x2a, 0x01, 0x07}, AtomicRMW, AtomicSub, 2, 8},
		{"i32.xor8", []byte{0x3c, 0x00, 0x07}, AtomicRMW, AtomicXor, 1, 4},
		{"i64.xchg32", []byte{0x47, 0x02, 0x07}, AtomicRMW, AtomicXchg, 4, 8},
		{"i64.cmpxchg32", []byte{0x4e, 0x02, 0x07}, AtomicCmpxchg, AtomicNone, 4, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := DecodeAtomic(wasm.NewReader(tc.bytes))
			if err != nil {
				t.Fatal(err)
			}
			if d.Class != tc.class || d.Operation != tc.op || d.Size != tc.size || d.ResultSize != tc.resultSize || (d.Class != AtomicFence && d.Offset != 7) {
				t.Fatalf("descriptor = %+v", d)
			}
		})
	}
}

func TestDecodeAtomicRejectsOutsideInitialBoundary(t *testing.T) {
	for _, body := range [][]byte{{0x04, 0, 0}, {0x4f, 0, 0}, {0x10, 0x40, 1, 0}, {0x03, 1}} {
		if _, err := DecodeAtomic(wasm.NewReader(body)); err == nil {
			t.Fatalf("accepted %x", body)
		}
	}
}

func TestDecodeAtomicMemory64Offsets(t *testing.T) {
	for _, tc := range []struct {
		bytes []byte
		want  uint64
	}{
		{[]byte{0x10, 2, 0x80, 0x80, 0x80, 0x80, 0x10}, 1 << 32},
		{[]byte{0x01, 2, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1}, ^uint64(0)},
	} {
		got, err := DecodeAtomicForMemory(wasm.NewReader(tc.bytes), true)
		if err != nil || got.Offset != tc.want {
			t.Fatalf("memory64 offset = %x, %v; want %x", got.Offset, err, tc.want)
		}
		if _, err := DecodeAtomic(wasm.NewReader(tc.bytes)); err == nil {
			t.Fatal("memory32 accepted a 64-bit offset")
		}
	}
}

func TestDecodeAtomicExplicitMemoryZero(t *testing.T) {
	for _, wide := range []bool{false, true} {
		got, err := DecodeAtomicForMemory(wasm.NewReader([]byte{0x10, 0x42, 0, 7}), wide)
		if err != nil || got.Align != 2 || got.Offset != 7 || got.Size != 4 {
			t.Fatalf("explicit zero descriptor = %+v, %v", got, err)
		}
	}
}

func TestDecodeAtomicIndexedAddressWidth(t *testing.T) {
	for _, wide := range []bool{false, true} {
		m := &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Addr64: !wide}}, {Limits: wasm.Limits{Addr64: wide}}}}
		body := []byte{0x10, 0x42, 1, 7}
		got, err := DecodeAtomicForModule(wasm.NewReader(body), m)
		if err != nil || got.MemoryIndex != 1 || got.Offset != 7 || got.Align != 2 {
			t.Fatalf("indexed descriptor = %+v, %v", got, err)
		}
		body = []byte{0x10, 0x42, 1, 0x80, 0x80, 0x80, 0x80, 0x10}
		got, err = DecodeAtomicForModule(wasm.NewReader(body), m)
		if wide {
			if err != nil || got.Offset != 1<<32 {
				t.Fatalf("indexed memory64 = %+v, %v", got, err)
			}
		} else if err == nil {
			t.Fatal("memory32 accepted memory64 offset")
		}
		if _, err := DecodeAtomicForModule(wasm.NewReader([]byte{0x10, 0x42, 2, 0}), m); err == nil {
			t.Fatal("accepted unavailable memory")
		}
	}
}
