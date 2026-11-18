//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wr "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

// runStreamGlobalFixture supplies the real basedata pointer-table ABI. The
// arena, cell and table remain mapped until native execution has returned.
func runStreamGlobalFixture(t *testing.T, m *wasm.Module, opts CompileOptions, initial uint64, args ...uint64) (uint64, uint64, error) {
	t.Helper()
	cm, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	eng, err := wr.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	jm, err := wr.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer jm.Close()
	arena, err := wr.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	cell, table := arena.Alloc(8), arena.Alloc(8)
	binary.LittleEndian.PutUint64(cell, initial)
	binary.LittleEndian.PutUint64(table, uint64(uintptr(unsafe.Pointer(&cell[0]))))
	jm.SetGlobalsPtr(uintptr(unsafe.Pointer(&table[0])))
	mem, entry, err := wr.MapCode(cm.Code)
	if err != nil {
		t.Fatal(err)
	}
	defer wr.Unmap(mem)
	inputs, outputs, trap := arena.Alloc(256), arena.Alloc(256), arena.Alloc(wr.TrapBufferBytes)
	for i, v := range args {
		binary.LittleEndian.PutUint64(inputs[i*8:], v)
	}
	err = eng.Call(entry+uintptr(cm.Entry[0]), inputs, jm.LinearMemory(), trap, outputs)
	return binary.LittleEndian.Uint64(outputs), binary.LittleEndian.Uint64(cell), err
}

func streamGlobalFixture(t *testing.T, wide, imported, trap bool) *wasm.Module {
	t.Helper()
	typ, add := wasm.I32, byte(0x6a)
	if wide {
		typ, add = wasm.I64, 0x7c
	}
	// Snapshot old global on stack, overwrite it, then add old+current value.
	body := []byte{0, 0x23, 0, 0x20, 0, 0x24, 0, 0x23, 0, add, 0x0b}
	if trap {
		body = []byte{0, 0x20, 0, 0x24, 0, 0x00, 0x0b}
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ})))}
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "g", typ, true))))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))))
	if !imported {
		op := byte(0x41)
		if wide {
			op = 0x42
		}
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(typ, true, []byte{op, 0, 0x0b}))))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	m, err := wasm.DecodeModule(wasmtest.Module(sections...))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStreamGlobalSnapshotsRuntime(t *testing.T) {
	oldStream, oldJoint, oldPins := streamingOperandsEnabled, streamingCallGlobalsEnabled, pinGlobalK
	t.Cleanup(func() {
		streamingOperandsEnabled, streamingCallGlobalsEnabled, pinGlobalK = oldStream, oldJoint, oldPins
	})
	for _, on := range []bool{false, true} {
		for _, pins := range []int{0, 1} {
			for _, wide := range []bool{false, true} {
				for _, imported := range []bool{false, true} {
					for _, trap := range []bool{false, true} {
						t.Run(fmt.Sprintf("on=%v/pins=%d/wide=%v/imported=%v/trap=%v", on, pins, wide, imported, trap), func(t *testing.T) {
							streamingOperandsEnabled, streamingCallGlobalsEnabled, pinGlobalK = on, on, pins
							var stats ModuleStats
							opts := CompileOptions{}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							m := streamGlobalFixture(t, wide, imported, trap)
							initial, input := uint64(17), uint64(0xfedcba9812345678)
							got, cell, err := runStreamGlobalFixture(t, m, opts, initial, input)
							wantCell := input
							if !wide {
								wantCell = uint64(uint32(input))
							}
							want := initial + wantCell
							if !wide {
								want = uint64(uint32(want))
							}
							if (err != nil) != trap || cell != wantCell || !trap && got != want {
								t.Fatalf("got=%x cell=%x error=%v want=%x/%x trap=%v", got, cell, err, want, wantCell, trap)
							}
							if diagnosticsEnabled && on && !regallocCheckEnabled && stats.Funcs[0].Peephole["stream-global-set"] != 1 {
								t.Fatal("global fixture did not select stream lowering")
							}
						})
					}
				}
			}
		}
	}
}
