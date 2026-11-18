//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wr "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestStreamCallLegacyMemoryGrow(t *testing.T) {
	oldStream, oldJoint, oldInline := streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled
	t.Cleanup(func() {
		streamingOperandsEnabled, streamingCallGlobalsEnabled, inlineEnabled = oldStream, oldJoint, oldInline
	})
	inlineEnabled = false
	caller := []byte{0, 0x20, 0, 0x10, 1, 0x1a, 0x20, 1, 0x2d, 0, 0, 0xb}
	callee := []byte{0, 0x20, 0, 0x40, 0, 0xb}
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(caller))), caller...), append(wasmtest.ULEB(uint32(len(callee))), callee...))),
	)
	for _, on := range []bool{false, true} {
		for _, interruptible := range []bool{false, true} {
			for _, tc := range []struct {
				delta, address uint64
				pages          uint32
				trap           bool
				want           uint64
			}{
				{1, 65536, 2, false, 0}, {2, 65536, 1, true, 0}, {0, 65536, 1, true, 0}, {0, 0, 1, false, 0x5a},
			} {
				t.Run(fmt.Sprintf("on=%v/interruptible=%v/delta=%d/address=%d", on, interruptible, tc.delta, tc.address), func(t *testing.T) {
					streamingOperandsEnabled, streamingCallGlobalsEnabled = on, on
					m, err := wasm.DecodeModule(raw)
					if err != nil {
						t.Fatal(err)
					}
					var stats ModuleStats
					opts := CompileOptions{Interruptible: interruptible}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					cm, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					if on && diagnosticsEnabled && !regallocCheckEnabled {
						if stats.Funcs[0].Peephole["streaming-direct-call"] != 1 || stats.Funcs[1].Peephole["streaming-function"] != 0 {
							t.Fatal("fixture did not select mixed stream caller/legacy callee")
						}
					}
					eng, err := wr.NewEngine()
					if err != nil {
						t.Fatal(err)
					}
					defer eng.Close()
					jm, err := wr.NewJobMemoryGrowable(65536, 131072)
					if err != nil {
						t.Fatal(err)
					}
					defer jm.Close()
					jm.LinearMemory()[0] = 0x5a
					arena, err := wr.NewArena(4096)
					if err != nil {
						t.Fatal(err)
					}
					defer arena.Close()
					mapped, entry, err := wr.MapCode(cm.Code)
					if err != nil {
						t.Fatal(err)
					}
					defer wr.Unmap(mapped)
					inputs, outputs, trap := arena.Alloc(128), arena.Alloc(128), arena.Alloc(wr.TrapBufferBytes)
					binary.LittleEndian.PutUint64(inputs, tc.delta)
					binary.LittleEndian.PutUint64(inputs[8:], tc.address)
					err = eng.Call(entry+uintptr(cm.Entry[0]), inputs, jm.LinearMemory(), trap, outputs)
					got := binary.LittleEndian.Uint64(outputs)
					if (err != nil) != tc.trap || jm.CurrentPages() != tc.pages || !tc.trap && got != tc.want || jm.CurrentBytes()[0] != 0x5a {
						t.Fatalf("got=%x pages=%d error=%v want=%x/%d trap=%v", got, jm.CurrentPages(), err, tc.want, tc.pages, tc.trap)
					}
				})
			}
		}
	}
}
