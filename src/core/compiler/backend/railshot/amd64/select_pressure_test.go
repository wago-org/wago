//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSelectXMMRegisterPressure(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64, wasm.V128} {
		for _, typed := range []bool{false, true} {
			for _, flags := range []bool{false, true} {
				t.Run(fmt.Sprintf("type=%v/typed=%v/flags=%v", typ, typed, flags), func(t *testing.T) {
					m, prefix := selectXMMPressureModule(t, typ, typed, flags)
					// Signed zero and a NaN payload must survive float selection
					// bit-for-bit. Vector operands differ in both 64-bit halves.
					var a, b [16]byte
					switch typ {
					case wasm.F32:
						binary.LittleEndian.PutUint32(a[:], 0x80000000)
						binary.LittleEndian.PutUint32(b[:], 0x7fc12345)
					case wasm.F64:
						binary.LittleEndian.PutUint64(a[:], 0x8000000000000000)
						binary.LittleEndian.PutUint64(b[:], 0x7ff8123456789abc)
					case wasm.V128:
						copy(a[:], []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15})
						copy(b[:], []byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0})
					}
					if diagnosticsEnabled {
						stats := compileWithStats(t, m, false).Funcs[0]
						if stats.Spills == 0 || stats.MaxSpillSlots == 0 {
							t.Fatalf("fixture did not create register pressure: spills=%d slots=%d", stats.Spills, stats.MaxSpillSlots)
						}
					}
					for _, condition := range []uint64{0, 1, 2} {
						t.Run(fmt.Sprintf("condition=%d", condition), func(t *testing.T) {
							got, mem, err := runMemAmd64(t, m, func(mem []byte) {
								copy(mem[64:], a[:])
								copy(mem[80:], b[:])
							}, condition)
							if err != nil {
								t.Fatal(err)
							}
							chooseA := condition != 0
							if flags {
								chooseA = !chooseA
							}
							want := b[:len(prefix)]
							if chooseA {
								want = a[:len(prefix)]
							}
							if !bytes.Equal(mem[:len(want)], want) {
								t.Fatalf("selected bits = %x, want %x", mem[:len(want)], want)
							}
							if got != 210 || !bytes.Equal(mem[32:32+len(prefix)], prefix) {
								t.Fatalf("live prefixes corrupted: integer=%d XMM=%x, want 210 and %x", got, mem[32:32+len(prefix)], prefix)
							}
						})
					}
				})
			}
		}
	}
}

func selectXMMPressureModule(t testing.TB, typ wasm.ValType, typed, flags bool) (*wasm.Module, []byte) {
	t.Helper()
	const pressure = 20 // More live values than either register file can hold.
	// Two temporary XMM locals hold the selected value and the prefix checksum.
	body := []byte{0x01, 0x02, wasm.MustEncodeValType(typ)}
	for i := 1; i <= pressure; i++ {
		body = append(body, 0x41, byte(i), 0x41, 0, 0x41, 1, 0x1b)
	}
	var prefix []byte
	var store, align byte
	var combine []byte
	for i := 1; i <= pressure; i++ {
		switch typ {
		case wasm.F32:
			body = append(body, 0x43)
			body = binary.LittleEndian.AppendUint32(body, math.Float32bits(float32(i)))
			body = append(body, 0x43, 0, 0, 0, 0)
			prefix = binary.LittleEndian.AppendUint32(nil, math.Float32bits(210))
			store, align, combine = 0x38, 2, []byte{0x92} // f32.store / f32.add
		case wasm.F64:
			body = append(body, 0x44)
			body = binary.LittleEndian.AppendUint64(body, math.Float64bits(float64(i)))
			body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0, 0)
			prefix = binary.LittleEndian.AppendUint64(nil, math.Float64bits(210))
			store, align, combine = 0x39, 3, []byte{0xa0} // f64.store / f64.add
		case wasm.V128:
			if prefix == nil {
				prefix = make([]byte, 16)
			}
			body = append(body, 0xfd, 0x0c) // v128.const
			for j := range 16 {
				v := byte(i * (j + 1))
				body = append(body, v)
				prefix[j] ^= v
			}
			body = append(body, 0xfd, 0x0c)
			body = append(body, make([]byte, 16)...)
			store, align, combine = 0x0b, 4, []byte{0xfd, 0x51} // v128.store / v128.xor
		}
		// Select eagerly owns each prefix value; bare constants would stay lazy
		// and would not exhaust XMM registers before the select being tested.
		body = append(body, 0x41, 1, 0x1b)
	}
	load := []byte{0x2a, 2, 0} // f32.load
	if typ == wasm.F64 {
		load = []byte{0x2b, 3, 0}
	} else if typ == wasm.V128 {
		load = []byte{0xfd, 0, 4, 0}
	}
	body = append(body, 0x41, 0xc0, 0) // i32.const 64
	body = append(body, load...)
	body = append(body, 0x41, 0xd0, 0) // i32.const 80
	body = append(body, load...)
	body = append(body, 0x20, 0, 0x41, 1, 0x6e) // cond / 1: deferred fixed-register work
	if flags {
		body = append(body, 0x45) // eqz must fall back to scalar condition recovery for XMM.
	}
	if typed {
		body = append(body, 0x1c, 1, wasm.MustEncodeValType(typ))
	} else {
		body = append(body, 0x1b)
	}
	body = append(body, 0x21, 1) // Save selected bits without doing floating arithmetic.
	for range pressure - 1 {
		body = append(body, combine...)
	}
	body = append(body, 0x21, 2)
	for range pressure - 1 {
		body = append(body, 0x6a) // Sum all live integer prefix values, too.
	}
	for _, output := range []struct{ address, local byte }{{0, 1}, {32, 2}} {
		body = append(body, 0x41, output.address, 0x20, output.local)
		if typ == wasm.V128 {
			body = append(body, 0xfd)
		}
		body = append(body, store, align, 0)
	}
	body = append(body, 0x0b)
	return modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body), prefix
}
