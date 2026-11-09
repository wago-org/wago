//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func directByteBoundsFixture(t testing.TB, op byte, offset uint32, prefix, control bool) *wasm.Module {
	wide := op == 0x30 || op == 0x31 || op == 0x3c
	store := op == 0x3a || op == 0x3c
	valueType := wasm.I32
	if wide {
		valueType = wasm.I64
	}
	b := []byte{0}
	if prefix {
		b = append(b, 0x41, 48, 0x41, 42, 0x3a, 0, 0)
	}
	if control {
		b = append(b, 0x02, 0x40)
	}
	if !store {
		b = append(b, 0x41, 16)
	}
	b = append(b, 0x20, 0)
	if store {
		b = append(b, 0x20, 1)
	}
	b = append(b, op, 0)
	b = append(b, wasmtest.ULEB(offset)...)
	if store {
		b = append(b, 0x41, 16, 0x20, 0, 0x2d, 0)
		b = append(b, wasmtest.ULEB(offset)...)
	}
	if !wide || store {
		b = append(b, 0xad)
	}
	b = append(b, 0x37, 0, 0)
	if control {
		b = append(b, 0x0b)
	}
	b = append(b, 0x41, 0, 0x0b)
	m := modMem(t, 0, []wasm.ValType{wasm.I32, valueType}, []wasm.ValType{wasm.I32}, b)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDirectByteBoundsMemoryAndTrapPrefix(t *testing.T) {
	old := directByteBoundsEnabled
	defer func() { directByteBoundsEnabled = old }()
	for _, on := range []bool{false, true} {
		directByteBoundsEnabled = on
		for _, compact := range []bool{false, true} {
			for _, control := range []bool{false, true} {
				opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"bounds-facts": false, "memory-min-bounds": false}}
				for _, op := range []byte{0x2c, 0x2d, 0x30, 0x31, 0x3a, 0x3c} {
					for _, offset := range []uint32{0, 1, 65535, 0xffffffff} {
						m := directByteBoundsFixture(t, op, offset, true, control)
						for _, base := range []uint64{0, 16, 48, 65535, 65536, 0xffffffff, 1<<32 | 65535, ^uint64(0)} {
							initial := make([]byte, 65536)
							for i := range initial {
								initial[i] = byte(i*17 + 9)
							}
							want := append([]byte{}, initial...)
							want[48] = 42
							address := uint64(uint32(base)) + uint64(offset)
							traps := address >= uint64(len(want))
							value := uint64(0x123456789abcde80)
							if !traps {
								if op == 0x3a || op == 0x3c {
									want[address] = byte(value)
								}
								result := uint64(want[address])
								if op == 0x2c {
									result = uint64(uint32(int32(int8(want[address]))))
								}
								if op == 0x30 {
									result = uint64(int64(int8(want[address])))
								}
								binary.LittleEndian.PutUint64(want[16:], result)
							}
							got, err := runLinearSpanMemory(t, m, opts, initial, base, value)
							if (err != nil) != traps || !bytes.Equal(got, want) {
								t.Fatalf("on%v compact%v control%v op%x offset%x base%x trap%v err%v got%x want%x", on, compact, control, op, offset, base, traps, err, got[:56], want[:56])
							}
						}
					}
					empty := directByteBoundsFixture(t, op, 0, false, control)
					for _, base := range []uint64{0, 1, 0xffffffff, 1 << 32} {
						got, err := runLinearSpanMemorySize(t, empty, opts, 0, nil, base, 0xff)
						if err == nil || len(got) != 0 {
							t.Fatalf("empty memory on%v compact%v control%v op%x base%x err%v", on, compact, control, op, base, err)
						}
					}
				}
			}
		}
	}
}

func TestDirectByteBoundsAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := directByteBoundsEnabled
	defer func() { directByteBoundsEnabled = old }()
	for _, on := range []bool{false, true} {
		directByteBoundsEnabled = on
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(directByteBoundsFixture(t, 0x31, 0, false, false), CompileOptions{Stats: stats, Optimizations: map[string]bool{"bounds-facts": false, "memory-min-bounds": false}})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if (stats.Funcs[0].Peephole["direct-byte-bounds"] > 0) != on {
			t.Fatalf("on%v stats%v", on, stats.Funcs[0].Peephole)
		}
	}
}
