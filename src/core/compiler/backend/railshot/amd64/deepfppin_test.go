//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// sum5FloatModule is a call-free reg-ABI function with five f64 params
// (a+b+c+d+e). Five hot float locals exceed the base four XMM pins (XMM12-15), so
// the fifth spills into the extended call-free slot XMM8 — the deep-FP-pin path.
func sum5FloatModule(t *testing.T) *wasm.Module {
	f64 := wasm.F64
	return mod1(t, []wasm.ValType{f64, f64, f64, f64, f64}, []wasm.ValType{f64}, []byte{
		0x00,
		0x20, 0x00, 0x20, 0x01, 0xa0, // a + b
		0x20, 0x02, 0xa0, // + c
		0x20, 0x03, 0xa0, // + d
		0x20, 0x04, 0xa0, // + e
		0x0b,
	})
}

func TestDeepFPPinFires(t *testing.T) {
	if s := compileWithStats(t, sum5FloatModule(t), false).Funcs[0]; s.Peephole["deep-fp-local-pin"] == 0 {
		t.Fatalf("deep-fp-local-pin = 0, want >=1 (all: %v)", s.Peephole)
	}
	// Disabled: the fifth float local no longer reaches an XMM8-10 slot.
	saved := extendedFPPinsEnabled
	extendedFPPinsEnabled = false
	defer func() { extendedFPPinsEnabled = saved }()
	if s := compileWithStats(t, sum5FloatModule(t), false).Funcs[0]; s.Peephole["deep-fp-local-pin"] != 0 {
		t.Fatalf("deep-fp-local-pin still fired with extended FP pins disabled: %v", s.Peephole)
	}
}
