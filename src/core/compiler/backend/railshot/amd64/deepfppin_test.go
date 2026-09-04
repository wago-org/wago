//go:build linux && amd64

package amd64

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoderamd64 "github.com/wago-org/wago/src/core/encoder/amd64"
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

func TestFPPinRelinquishmentAvoidsRetry(t *testing.T) {
	const local = 0
	stats := &CodegenStats{}
	f := fn{
		a:                &encoderamd64.Asm{},
		s:                newStack(),
		localType:        []machineType{mtF64},
		localSlot:        []uint32{0},
		locals:           []localDef{{reg: 12, isFloat: true, state: lsReg}},
		pinnedLocals:     []int{local},
		fpinnedLocalMask: regMask(0).add(12),
		stats:            stats,
		pinRelinquished:  false,
	}
	var avoid regMask
	for r := Reg(0); r < 16; r++ {
		if r != 12 {
			avoid = avoid.add(r)
		}
	}
	if got := f.allocFReg(avoid); got != 12 {
		t.Fatalf("relinquished register = %v, want XMM12", got)
	}
	if f.locals[local].state != lsMem || !f.pinRelinquished || stats.PinRelinquishments != 1 || stats.Peephole["fp-pin-relinquish"] != 1 {
		t.Fatalf("relinquishment state = local:%v active:%v stats:%+v", f.locals[local].state, f.pinRelinquished, stats)
	}
}

func TestRelinquishedFPPinWriteEvictsBorrower(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  machineType
	}{{"f64", mtF64}, {"v128", mtV128}} {
		t.Run(tc.name, func(t *testing.T) {
			typ := tc.typ
			const local = 0
			f := fn{
				a:                &encoderamd64.Asm{},
				s:                newStack(),
				localType:        []machineType{typ},
				localSlot:        []uint32{0},
				locals:           []localDef{{reg: 12, isFloat: true, state: lsMem}},
				pinnedLocals:     []int{local},
				fpinnedLocalMask: regMask(0).add(12),
				pinRelinquished:  true,
				stats:            &CodegenStats{},
			}

			borrower := f.pushFReg(12, typ)
			f.pushFReg(1, typ) // value for local.set
			f.setLocal(nil, local, false)
			if borrower.st.kind != stSlot {
				t.Fatalf("borrower storage after local.set = %v, want spill slot", borrower.st.kind)
			}
			if f.fregUser[12] != nil {
				t.Fatalf("relinquished XMM register still owns %#v", f.fregUser[12])
			}

			var reg Reg
			if typ == mtV128 {
				reg = f.materializeV128(borrower)
			} else {
				reg = f.materializeF(borrower)
			}
			if reg == regNone || borrower.st.kind != stReg {
				t.Fatalf("borrower was not reloadable: reg=%v storage=%v", reg, borrower.st.kind)
			}
		})
	}
}
