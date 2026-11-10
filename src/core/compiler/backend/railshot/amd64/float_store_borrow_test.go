//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFloatStoreBorrowsPinnedSource(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ty, store, load, reinterpret, equal, width := wasm.F32, byte(0x38), byte(0x2a), byte(0xbc), byte(0x46), 4
		vals := []uint64{0, 0x80000000, 0x7f800000, 0x7fc01234, 0xffc01234, 0x3f987654}
		if wide {
			ty, store, load, reinterpret, equal, width = wasm.F64, 0x39, 0x2b, 0xbd, 0x51, 8
			vals = []uint64{0, 0x8000000000000000, 0x7ff0000000000000, 0x7ff8123456789abc, 0xfff8123456789abc, 0x3ff987654321abcd}
		}
		body := []byte{0, 0x20, 1, 0x41, 1, 0x6a, 0x20, 0, store, 0, 3, 0x20, 1, 0x41, 1, 0x6a, load, 0, 3, reinterpret, 0x20, 0, reinterpret, equal, 0x0b}
		m := modMem(t, 1, []wasm.ValType{ty, wasm.I32}, []wasm.ValType{wasm.I32}, body)
		for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
			for _, guard := range []bool{false, true} {
				for _, enabled := range []bool{false, true} {
					t.Run(fmt.Sprintf("wide=%v/features=%x/guard=%v/on=%v", wide, features, guard, enabled), func(t *testing.T) {
						for _, value := range vals {
							var stats ModuleStats
							result, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{ElideBoundsChecks: guard, CompactNative: true, AMD64Features: features, AMD64FeaturesSet: true, Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"float-store-borrow": enabled}}, func(mem []byte) {
								for i := 0; i < 32; i++ {
									mem[i] = 0xa5
								}
							}, value, 1)
							if err != nil || result != 1 {
								t.Fatalf("value=%x result=%d err=%v", value, result, err)
							}
							var raw [8]byte
							binary.LittleEndian.PutUint64(raw[:], value)
							if !bytes.Equal(mem[5:5+width], raw[:width]) || mem[4] != 0xa5 || mem[5+width] != 0xa5 {
								t.Fatalf("wrong store width or bits: %x", mem[:20])
							}
							if diagnosticsEnabled {
								if (stats.Funcs[0].Peephole["float-store-borrow"] > 0) != enabled {
									t.Fatalf("borrow not selected: %v", stats.Funcs[0].Peephole)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestFloatStoreBorrowsCachedConstant(t *testing.T) {
	for _, wide := range []bool{false, true} {
		width, op, store := 4, byte(0x43), byte(0x38)
		bits := uint64(0xffc01234)
		if wide {
			width, op, store, bits = 8, 0x44, 0x39, 0xfff8123456789abc
		}
		var raw [8]byte
		binary.LittleEndian.PutUint64(raw[:], bits)
		body := []byte{0}
		for _, offset := range []byte{0, 16} {
			body = append(body, 0x41, offset, op)
			body = append(body, raw[:width]...)
			body = append(body, store, 0, 0)
		}
		body = append(body, 0x0b)
		m := modMem(t, 1, nil, nil, body)
		var stats ModuleStats
		_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats), Optimizations: map[string]bool{"float-store-borrow": true}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(mem[:width], raw[:width]) || !bytes.Equal(mem[16:16+width], raw[:width]) {
			t.Fatalf("wide=%v constant altered: %x", wide, mem[:24])
		}
		if diagnosticsEnabled {
			if stats.Funcs[0].Peephole["float-store-borrow"] != 2 {
				t.Fatalf("wide=%v constant was not cached: %v", wide, stats.Funcs[0].Peephole)
			}
		}
	}
}

func TestFloatStoreBorrowPreservesBounds(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ty, op, width := wasm.F32, byte(0x38), 4
		if wide {
			ty, op, width = wasm.F64, 0x39, 8
		}
		m := modMem(t, 1, []wasm.ValType{ty, wasm.I32}, nil, []byte{0, 0x20, 1, 0x41, 1, 0x6a, 0x20, 0, op, 0, 3, 0x0b})
		for _, enabled := range []bool{false, true} {
			// The final in-bounds store must fit exactly; advancing by one must trap
			// before changing memory. The arithmetic wraps at i32 before the offset.
			for _, addr := range []uint64{uint64(65536 - width - 4), uint64(65536 - width - 3), 0xffffffff} {
				_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Optimizations: map[string]bool{"float-store-borrow": enabled}}, func(mem []byte) {
					for i := range mem {
						mem[i] = 0xa5
					}
				}, 0x12345678, addr)
				invalid := addr == uint64(65536-width-3)
				if (err != nil) != invalid {
					t.Fatalf("wide=%v enabled=%v addr=%x err=%v", wide, enabled, addr, err)
				}
				if invalid && !bytes.Equal(mem, bytes.Repeat([]byte{0xa5}, 65536)) {
					t.Fatal("trapping store changed memory")
				}
			}
		}
	}
}
