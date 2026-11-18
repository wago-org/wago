//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func shiftedMultiplyModule(t *testing.T, wide bool, mode byte, c uint64) *wasm.Module {
	typ, constant, mul, xor := wasm.I32, byte(0x41), byte(0x6c), byte(0x73)
	if wide {
		typ, constant, mul, xor = wasm.I64, 0x42, 0x7e, 0x85
	}
	// The empty loop selects the ordinary Valent path rather than scalar admission.
	body := []byte{0, 0x03, 0x40, 0x0b, 0x20, 0}
	switch mode {
	case 1:
		body = append(body, 0x20, 1, 0x21, 0)
	case 2:
		body = append(body, 0x20, 1, xor)
	case 3:
		body = append(body, 0x20, 0)
	}
	body = append(body, constant)
	if wide {
		body = append(body, wasmtest.SLEB64(int64(c))...)
	} else {
		body = append(body, wasmtest.SLEB32(int32(c))...)
	}
	body = append(body, mul)
	switch mode {
	case 1:
		body = append(body, 0x20, 0, xor)
	case 3:
		body = append(body, 0x21, 0, 0x20, 0, xor, 0x20, 1, xor)
	}
	// Observe I32 canonicalization through a Wasm zero extension, rather than
	// depending on unspecified upper bits of a raw I32 ABI return carrier.
	result := typ
	if !wide {
		body = append(body, 0xad)
		result = wasm.I64
	}
	body = append(body, 0x0b)
	return mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{result}, body)
}

func TestShiftedIntegerMultiplyWrapAndCapturedRead(t *testing.T) {
	for _, wide := range []bool{false, true} {
		width := 32
		if wide {
			width = 64
		}
		coefficients := map[uint64]bool{0: true, 1: true, 2: true, 6: true, 7: true, 10: true}
		for k := 1; k < width; k++ {
			coefficients[1+(uint64(1)<<k)] = true
			coefficients[1-(uint64(1)<<k)] = true
		}
		for c := range coefficients {
			for _, mode := range []byte{0, 1, 2, 3} {
				m := shiftedMultiplyModule(t, wide, mode, c)
				for _, x := range []uint64{0, 1, 7, 0x7fffffff, 0x80000000, 0xffffffff, 0x8000000000000000, 0xffffffffffffffff} {
					y := x ^ 0xfedcba9876543210
					want := x * c
					switch mode {
					case 1:
						want ^= y
					case 2:
						want = (x ^ y) * c
					case 3:
						want ^= x ^ y
					}
					if !wide {
						want = uint64(uint32(want))
						x |= 0xaabbccdd00000000
						y |= 0x1234567800000000
					}
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, x, y)
					if err != nil || got != want {
						t.Fatalf("wide%t mode%d c%x x%x y%x: got%x/%v want%x", wide, mode, c, x, y, got, err, want)
					}
				}
			}
		}
	}
}

func TestShiftedIntegerMultiplyRemovesNativeMultiply(t *testing.T) {
	old := shiftedIntegerMultiplyEnabled
	defer func() { shiftedIntegerMultiplyEnabled = old }()
	for _, wide := range []bool{false, true} {
		cs := []uint64{17, 33, 129, ^uint64(14), ^uint64(30)}
		if wide {
			cs = append(cs, 1+(uint64(1)<<32), ^uint64((1<<32)-2), 1+(uint64(1)<<63))
		}
		for _, c := range cs {
			count := func(enabled bool) int {
				shiftedIntegerMultiplyEnabled = enabled
				m := shiftedMultiplyModule(t, wide, 0, c)
				cm, err := CompileModuleWith(m, CompileOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer cm.CodeImage.Close()
				n := 0
				for i := 0; i+4 <= len(cm.Code); i += 4 {
					if binary.LittleEndian.Uint32(cm.Code[i:])&0x1f000000 == 0x1b000000 {
						n++
					}
				}
				return n
			}
			if on, off := count(true), count(false); on != 0 || off == 0 {
				t.Fatalf("wide%t c%x: native multiplies on%d off%d", wide, c, on, off)
			}
		}
	}
}

func TestShiftedIntegerMultiplyKeepsLoadTrapPrefix(t *testing.T) {
	for _, wide := range []bool{false, true} {
		width, load, store, constant, mul := uint32(4), byte(0x28), byte(0x36), byte(0x41), byte(0x6c)
		if wide {
			width, load, store, constant, mul = 8, 0x29, 0x37, 0x42, 0x7e
		}
		for _, c := range []uint64{0, 17, ^uint64(14), 1 + (uint64(1) << 31), 1 + (uint64(1) << 63)} {
			body := []byte{0, 0x41, 0, 0x41, 42, 0x3a, 0, 0, 0x41, 8, 0x20, 0, load, 0, 0, constant}
			if wide {
				body = append(body, wasmtest.SLEB64(int64(c))...)
			} else {
				body = append(body, wasmtest.SLEB32(int32(c))...)
			}
			body = append(body, mul, store, 0, 0, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, nil, body)
			for _, addr := range []uint32{0, 1, 65536 - width, 65537 - width, 65536, 0xffffffff} {
				want := make([]byte, 65536)
				want[0] = 42
				valid := uint64(addr)+uint64(width) <= 65536
				if valid {
					if wide {
						v := binary.LittleEndian.Uint64(want[addr:])
						binary.LittleEndian.PutUint64(want[8:], v*c)
					} else {
						v := binary.LittleEndian.Uint32(want[addr:])
						binary.LittleEndian.PutUint32(want[8:], v*uint32(c))
					}
				}
				got, err := runScalarUnrollMemory(t, m, CompileOptions{}, uint64(addr)|0xaabbccdd00000000)
				if (err == nil) != valid || !bytes.Equal(got, want) {
					t.Fatalf("wide%t c%x addr%x: valid%t err%v memory%t", wide, c, addr, valid, err, bytes.Equal(got, want))
				}
			}
		}
	}
}

func TestShiftedIntegerMultiplyKeepsCachedMultiplier(t *testing.T) {
	old := shiftedIntegerMultiplyEnabled
	shiftedIntegerMultiplyEnabled = true
	defer func() { shiftedIntegerMultiplyEnabled = old }()
	for _, typ := range []machineType{mtI32, mtI64} {
		for _, value := range []int64{17, -15, 4294967297} {
			f := fn{iconstN: 1}
			f.iconsts[0] = intConstReg{typ: typ, bits: value, reg: X25}
			if _, _, ok := f.selectShiftedIntegerMultiply(storage{typ: typ, cval: value}, typ.is64()); ok {
				t.Fatalf("cached multiplier selected: type%d value%d", typ, value)
			}
		}
	}
}
