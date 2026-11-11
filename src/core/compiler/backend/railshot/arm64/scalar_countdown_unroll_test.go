//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func scalarUnrollFixture(t testing.TB) *wasm.Module {
	b := []byte{2, 2, 0x7f, 1, 0x7e, 0x20, 1, 0x41}
	b = append(b, wasmtest.SLEB32(8191)...)
	b = append(b, 0x71, 0x41, 3, 0x74, 0x21, 3, 0x20, 1, 0xad, 0x21, 4, 0x20, 0, 0x21, 2,
		0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x45, 0x0d, 1,
		0x20, 3, 0x20, 4, 0x20, 1, 0x20, 2, 0x6a, 0xad, 0x85, 0x37, 3, 0,
		0x20, 4, 0x20, 3, 0x29, 3, 0, 0x7c, 0x21, 4,
		0x20, 3, 0x41)
	b = append(b, wasmtest.SLEB32(64)...)
	b = append(b, 0x6a, 0x41)
	b = append(b, wasmtest.SLEB32(65528)...)
	b = append(b, 0x71, 0x21, 3, 0x20, 2, 0x41, 1, 0x6b, 0x21, 2, 0x0c, 0, 0x0b, 0x0b,
		0x41, 8, 0x20, 2, 0x36, 2, 0, 0x41, 12, 0x20, 3, 0x36, 2, 0,
		0x41, 0, 0x20, 4, 0x37, 3, 0, 0x41, 0, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
}

func TestScalarLoopUnrollMemory(t *testing.T) {
	for _, count := range []uint64{0, 1, 2, 3, 4, 7, 8, 15, 16, 23, 24, 25, 31, 32, 33, 0x8877665500000000, 0x8877665500000001, 0x8877665500000018} {
		for _, seed := range []uint64{0, 1, 8191, 0xfffffff0, 0xffffffff, 0x8877665544332211} {
			m := scalarUnrollFixture(t)
			for _, on := range []bool{false, true} {
				want := make([]byte, 65536)
				s32 := uint32(seed)
				ptr, acc := (s32&8191)<<3, uint64(s32)
				for i := uint32(count); i != 0; i-- {
					value := acc ^ uint64(s32+i)
					binary.LittleEndian.PutUint64(want[ptr:], value)
					acc += value
					ptr = (ptr + 64) & 65528
				}
				binary.LittleEndian.PutUint64(want, acc)
				binary.LittleEndian.PutUint32(want[8:], 0)
				binary.LittleEndian.PutUint32(want[12:], ptr)
				got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"scalar-loop-unroll": on}}, uint64(count), uint64(seed))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("count%d seed%x on%v err%v equal%v", count, seed, on, err, bytes.Equal(got, want))
				}
			}
		}
	}
}

func TestScalarLoopUnrollSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, on := range []bool{false, true} {
		var stats ModuleStats
		cm, err := CompileModuleWith(scalarUnrollFixture(t), CompileOptions{Stats: &stats, Optimizations: map[string]bool{"scalar-loop-unroll": on}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		hit := stats.Funcs[0].Peephole["scalar-loop-unroll"] + stats.Funcs[0].Peephole["scalar-loop-unroll-layout"]
		if (hit != 0) != on {
			t.Fatalf("on%v hits%d", on, hit)
		}
	}
}

func TestScalarUnrollWordGuards(t *testing.T) {
	for _, w := range []uint32{0xd10043ff, 0x71000529, 0x54000001, 0x94000000, 0xd61f0200, 0xf9400129, 0xffffffff} {
		if _, ok := scalarUnrollWord(w); ok {
			t.Fatalf("accepted %08x", w)
		}
	}
	for _, w := range []uint32{0x11000529, 0xca0902a9, 0xf8296b49, 0xf94003e9, nopWord} {
		if _, ok := scalarUnrollWord(w); !ok {
			t.Fatalf("rejected %08x", w)
		}
	}
}

func TestScalarLoopUnrollMatcherGuards(t *testing.T) {
	b := []byte{}
	put := func(w uint32) { b = binary.LittleEndian.AppendUint32(b, w) }
	put(0x34000013 | 5<<5)
	put(0x0b1302c9)
	put(0xca0902b5)
	put(0x71000673)
	put(0x54000001 | uint32(0x7ffff-2)<<5)
	for _, tc := range []struct {
		name   string
		off    int
		word   uint32
		accept bool
	}{
		{"accepted", 8, rdWord(b, 8), true},
		{"counter overwrite", 8, 0x11000673, false},
		{"flag dependency", 8, 0xba0902b5, false},
		{"flag definition", 8, 0x6b1302c9, false},
		{"call", 8, 0x94000000, false},
		{"missing zero guard", 0, nopWord, false},
		{"wrong zero exit", 0, 0x34000013 | 4<<5, false},
		{"wide counter", 12, 0xf1000673, false},
		{"other decrement", 12, 0x71000a73, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := append([]byte(nil), b...)
			wrWord(c, tc.off, tc.word)
			g, ok := matchScalarUnroll(c, 16, len(c))
			if ok != tc.accept {
				t.Fatalf("got%v want%v loop%+v", ok, tc.accept, g)
			}
		})
	}
}
