//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestCompareTeeBranchPreservesAssignedLocalArm64(t *testing.T) {
	knobs, _ := OptKnobSnapshot()
	selection := make(map[string]bool, len(knobs))
	for _, knob := range knobs {
		selection[knob.Name] = knob.On
	}
	selection["inline"] = false
	i32 := []wasm.ValType{wasm.I32}
	params := []wasm.ValType{wasm.I32, wasm.I32}
	// A call after the merge must reload the newly assigned comparison result,
	// including on the taken edge. The helper returns its argument unchanged.
	body := []byte{
		0x00,
		0x41, 0x00, 0x10, 0x01, 0x1a,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x01, 0x41, 0x05, 0x48,
		0x22, 0x00, 0x0d, 0x01,
		0x0b, 0x0b,
		0x41, 0x00, 0x10, 0x01, 0x1a,
		0x20, 0x00, 0x0b,
	}
	for _, arg := range []uint64{2, 9} {
		m := modFuncs(t, funcDef{params, i32, body}, funcDef{i32, i32, []byte{0x00, 0x20, 0x00, 0x0b}})
		got, err := runArm64WrapperWithOptions(t, m, CompileOptions{Optimizations: selection}, 73, arg)
		if err != nil {
			t.Fatal(err)
		}
		want := uint64(0)
		if arg < 5 {
			want = 1
		}
		if got != want {
			t.Errorf("argument %d: assigned local = %d, want %d", arg, got, want)
		}
	}
}
