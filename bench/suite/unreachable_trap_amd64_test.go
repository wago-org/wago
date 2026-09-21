//go:build amd64

package wagobench

import (
	"errors"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDraglineSharedUnreachableTrapKeepsSourceOffsets(t *testing.T) {
	body := []byte{
		0x20, 0x00, 0x41, 0x01, 0x46, 0x04, 0x40, 0x00, 0x0b, // if param == 1: unreachable
		0x20, 0x00, 0x41, 0x02, 0x46, 0x04, 0x40, 0x00, 0x0b, // if param == 2: unreachable
		0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	for _, bounds := range []wago.BoundsCheckMode{wago.BoundsChecksExplicit, wago.BoundsChecksSignalsBased} {
		compiled, err := wago.NewRuntimeConfig().WithCompiler(wago.CompilerDragline).WithTarget(wago.TargetNative).WithBoundsChecks(bounds).Compile(source)
		if err != nil {
			t.Fatalf("bounds=%v compile: %v", bounds, err)
		}
		instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
		if err != nil {
			compiled.Close()
			t.Fatalf("bounds=%v instantiate: %v", bounds, err)
		}
		if _, err := instance.Invoke("run", wago.I32(0)); err != nil {
			t.Fatalf("bounds=%v normal path: %v", bounds, err)
		}
		var offsets [2]uint32
		for index, value := range []int32{1, 2} {
			_, err := instance.Invoke("run", wago.I32(value))
			var trap *wago.TrapError
			if !errors.As(err, &trap) || trap.Code != wago.TrapUnreachable || len(trap.Frames) == 0 || !trap.Frames[0].HasProgramCounter {
				t.Fatalf("bounds=%v value=%d trap = %v", bounds, value, err)
			}
			offsets[index] = trap.Frames[0].ProgramCounter
		}
		if offsets[0] == offsets[1] {
			t.Fatalf("bounds=%v distinct unreachable sites have the same Wasm offset %d", bounds, offsets[0])
		}
		instance.Close()
		compiled.Close()
	}
}
