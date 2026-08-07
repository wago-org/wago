//go:build linux && amd64

package amd64

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// sum5FloatModule is a reg-ABI function with five f64 params
// (a+b+c+d+e). Five hot float locals exceed the base four XMM pins (XMM12-15), so
// the fifth reaches the extended slot XMM8 — the deep-FP-pin path.
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

func sum8FloatAcrossCallModule(t *testing.T) *wasm.Module {
	f64 := wasm.F64
	params := []wasm.ValType{f64, f64, f64, f64, f64, f64, f64, f64}
	body := []byte{0x00, 0x10, 0x01} // no locals; call the empty function
	body = append(body, 0x20, 0x00, 0x20, 0x01, 0xa0)
	for i := byte(2); i < 8; i++ {
		body = append(body, 0x20, i, 0xa0)
	}
	body = append(body, 0x0b)
	return modFuncs(t,
		funcDef{params: params, results: []wasm.ValType{f64}, body: body},
		funcDef{body: []byte{0x00, 0x0b}},
	)
}

func TestDeepFPPinFires(t *testing.T) {
	if s := compileWithStats(t, sum5FloatModule(t), false).Funcs[0]; s.Peephole["deep-fp-local-pin"] == 0 {
		t.Fatalf("deep-fp-local-pin = 0, want >=1 (all: %v)", s.Peephole)
	}
	// Disabled: the fifth float local no longer reaches the extended pool.
	saved := extendedFPPinsEnabled
	extendedFPPinsEnabled = false
	defer func() { extendedFPPinsEnabled = saved }()
	if s := compileWithStats(t, sum5FloatModule(t), false).Funcs[0]; s.Peephole["deep-fp-local-pin"] != 0 {
		t.Fatalf("deep-fp-local-pin still fired with extended FP pins disabled: %v", s.Peephole)
	}
}

func TestDeepFPPinsAcrossCall(t *testing.T) {
	savedInline := inlineEnabled
	inlineEnabled = false
	defer func() { inlineEnabled = savedInline }()
	m := sum8FloatAcrossCallModule(t)
	s := compileWithStats(t, m, false).Funcs[0]
	if s.PinnedLocals != 8 || s.Peephole["deep-fp-local-pin"] != 4 {
		t.Fatalf("call-making FP pins = %d deep=%d, want 8/4 (all: %v)", s.PinnedLocals, s.Peephole["deep-fp-local-pin"], s.Peephole)
	}
	args := make([]uint64, 8)
	for i := range args {
		args[i] = math.Float64bits(float64(i + 1))
	}
	if got := math.Float64frombits(runAmd64u(t, m, args...)); got != 36 {
		t.Fatalf("sum across call = %g, want 36", got)
	}

	saved := extendedFPPinsEnabled
	extendedFPPinsEnabled = false
	defer func() { extendedFPPinsEnabled = saved }()
	s = compileWithStats(t, m, false).Funcs[0]
	if s.PinnedLocals != baseFPPins || s.Peephole["deep-fp-local-pin"] != 0 {
		t.Fatalf("disabled call-making FP pins = %d deep=%d, want %d/0", s.PinnedLocals, s.Peephole["deep-fp-local-pin"], baseFPPins)
	}
}
