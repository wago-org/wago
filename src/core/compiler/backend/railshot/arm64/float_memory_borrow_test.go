//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func floatMemoryBorrowFixture(t testing.TB, wide bool) *wasm.Module {
	typ, load, store, reinterpret, back := byte(0x7d), byte(0x2a), byte(0x38), byte(0xbe), byte(0xbc)
	size := byte(4)
	if wide {
		typ, load, store, reinterpret, back, size = 0x7c, 0x2b, 0x39, 0xbf, 0xbd, 8
	}
	b := []byte{1, 2, typ, 0x20, 1}
	if !wide {
		b = append(b, 0xa7)
	}
	b = append(b, reinterpret, 0x21, 2, 0x20, 1)
	if wide {
		b = append(b, 0x42)
		b = append(b, wasmtest.SLEB64(-9223372036854775808)...)
		b = append(b, 0x85)
	} else {
		b = append(b, 0xa7, 0x41)
		b = append(b, wasmtest.SLEB32(-2147483648)...)
		b = append(b, 0x73)
	}
	b = append(b, reinterpret, 0x21, 3,
		0x20, 0, 0x20, 2, store, 0, 0,
		// Capture the old pinned destination across assignment from the load.
		0x20, 0, 0x20, 3, 0x20, 0, load, 0, 0, 0x21, 3, store, 0, 3*size,
		0x20, 0, 0x20, 3, store, 0, size,
		0x20, 0, 0x20, 2, store, 0, 2*size,
		0x20, 3, back)
	if !wide {
		b = append(b, 0xad)
	}
	b = append(b, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}, b)
}

func TestFloatMemoryBorrowBitsAndPartialTraps(t *testing.T) {
	bitsCases := []uint64{0, 1, 0x80000000, 0x7f800000, 0x7f800001, 0x7fc12345, 0xffc12345, 0x8000000000000000, 0x7ff0000000000000, 0x7ff0000000000001, 0x7ff8123456789abc, 0xffffffffffffffff, 0x8877665544332211}
	for _, wide := range []bool{false, true} {
		size, sign := 4, uint64(1<<31)
		if wide {
			size, sign = 8, 1<<63
		}
		m := floatMemoryBorrowFixture(t, wide)
		for _, on := range []bool{false, true} {
			for _, address := range []uint64{0, 1, 9, 65504, 65512, 65520, 65528, 65535, 0xffffffff, 0x8877665500000001} {
				for _, bits := range bitsCases {
					value := bits
					if !wide {
						value = uint64(uint32(bits))
					}
					want, trap := make([]byte, 65536), false
					for _, slot := range []int{0, 3, 1, 2} {
						addr := uint64(uint32(address)) + uint64(slot*size)
						if addr+uint64(size) > 65536 {
							trap = true
							break
						}
						v := value
						if slot == 3 {
							v ^= sign
						}
						if wide {
							binary.LittleEndian.PutUint64(want[addr:], v)
						} else {
							binary.LittleEndian.PutUint32(want[addr:], uint32(v))
						}
					}
					opts := CompileOptions{Optimizations: map[string]bool{"float-memory-borrow": on}}
					got, err := runScalarUnrollMemory(t, m, opts, address, bits)
					if (err != nil) != trap || !bytes.Equal(got, want) {
						t.Fatalf("wide%v on%v addr%x bits%x err%v trap%v memoryEqual%v", wide, on, address, bits, err, trap, bytes.Equal(got, want))
					}
					result, resultErr := runArm64WrapperWithOptions(t, m, opts, address, bits)
					if (resultErr != nil) != trap || !trap && result != value {
						t.Fatalf("return wide%v on%v addr%x bits%x got%x err%v trap%v", wide, on, address, bits, result, resultErr, trap)
					}
				}
			}
		}
	}
}

func TestFloatMemoryBorrowSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, wide := range []bool{false, true} {
		for _, on := range []bool{false, true} {
			var stats ModuleStats
			cm, err := CompileModuleWith(floatMemoryBorrowFixture(t, wide), CompileOptions{Stats: &stats, Optimizations: map[string]bool{"float-memory-borrow": on}})
			if err != nil {
				t.Fatal(err)
			}
			cm.CodeImage.Close()
			p := stats.Funcs[0].Peephole
			if (p["float-store-borrow"] > 0) != on || (p["float-load-local-sink"] > 0) != on {
				t.Fatalf("wide%v on%v counts%v", wide, on, p)
			}
		}
	}
}
