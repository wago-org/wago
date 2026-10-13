package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func affineTableFixture(t *testing.T, scale uint32, biases []uint32) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.AffineI32Table(scale, biases, uint32(len(biases)), false, false))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAffineI32TableEntries(t *testing.T) {
	for _, scale := range []uint32{0, 1, 2, 0xffffffff, 0x80000001} {
		for _, n := range []int{1, 2, 3, 16} {
			biases := make([]uint32, n)
			for i := range biases {
				biases[i] = 0xfffffff1 + uint32(i)*0x80000007
			}
			m := affineTableFixture(t, scale, biases)
			p, ok := InspectAffineI32TableEntries(m)
			if !ok || p.Count != uint32(n) || p.Scale != scale || p.Bias != biases[0] || n > 1 && p.Step != 0x80000007 {
				t.Fatalf("scale%x n%d: %+v %t", scale, n, p, ok)
			}
			for _, x := range []uint32{0, 1, 7, 0x80000000, 0xffffffff} {
				for i, bias := range biases {
					if got, want := p.Scale*x+p.Bias+uint32(i)*p.Step, scale*x+bias; got != want {
						t.Fatalf("got%x want%x", got, want)
					}
				}
			}
		}
	}
	// Later active segments override earlier entries; do not retain stale biases.
	m := affineTableFixture(t, 3, []uint32{11, 23})
	e := m.Elements[0]
	e.Kind.Funcs = []wasm.FuncIdx{1}
	e.Mode.Offset = wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const, I32: 0}}}
	m.Elements = append(m.Elements, e)
	p, ok := InspectAffineI32TableEntries(m)
	if !ok || p.Bias != 23 || p.Step != 0 {
		t.Fatalf("overlap: %+v %t", p, ok)
	}
}

func TestAffineI32TableEntryRejections(t *testing.T) {
	for _, name := range []string{"empty", "large", "table64", "imported-function", "imported-table", "initializer", "hole", "outside", "expressions", "different-scale", "non-affine-bias", "different-type", "trapping-leaf", "ast-leaf"} {
		t.Run(name, func(t *testing.T) {
			m := affineTableFixture(t, 1, []uint32{1, 3, 5})
			switch name {
			case "empty":
				m.Tables[0].Type.Limits.Min = 0
			case "large":
				m.Tables[0].Type.Limits.Min = 17
			case "table64":
				m.Tables[0].Type.Limits.Addr64 = true
			case "imported-function":
				m.Imports = append(m.Imports, wasm.Import{Type: wasm.NewFuncExternType(wasm.TypeIdx{Index: 0})})
			case "imported-table":
				m.Imports = append(m.Imports, wasm.Import{Type: wasm.NewTableExternType(m.Tables[0].Type)})
			case "initializer":
				m.Tables[0].Init = &wasm.Expr{}
			case "hole":
				m.Elements[0].Kind.Funcs = m.Elements[0].Kind.Funcs[:2]
			case "outside":
				m.Elements[0].Mode.Offset = wasm.Expr{BodyBytes: []byte{0x41, 1, 0x0b}}
			case "expressions":
				m.Elements[0].Kind.Kind = wasm.ElemFuncExprs
			case "different-scale":
				m.Code[1].BodyBytes = []byte{0x20, 0, 0x41, 2, 0x6c, 0x41, 3, 0x6a, 0x0b}
			case "non-affine-bias":
				m.Code[2].BodyBytes = []byte{0x20, 0, 0x41, 6, 0x6a, 0x0b}
			case "different-type":
				ft, _ := m.TypeFunc(0)
				ft.Params[0] = wasm.I64
			case "trapping-leaf":
				m.Code[1].BodyBytes = []byte{0x20, 0, 0x41, 0, 0x6d, 0x0b}
			case "ast-leaf":
				m.Code[1].BodyBytes = nil
			}
			p, ok := InspectAffineI32TableEntries(m)
			if ok || p != (AffineI32Table{}) {
				t.Fatalf("accepted: %+v %t", p, ok)
			}
		})
	}
}

func TestAffineI32LeafSymbolicArithmetic(t *testing.T) {
	for _, c := range []struct {
		body        []byte
		scale, bias uint32
	}{
		{[]byte{0x20, 0, 0x0b}, 1, 0},
		{[]byte{0x41, 7, 0x0b}, 0, 7},
		{[]byte{0x41, 7, 0x20, 0, 0x6b, 0x0b}, 0xffffffff, 7},
		{[]byte{0x20, 0, 0x20, 0, 0x6a, 0x41, 3, 0x6c, 0x0b}, 6, 0},
	} {
		a, b, ok := inspectAffineI32Leaf(c.body)
		if !ok || a != c.scale || b != c.bias {
			t.Fatalf("%x => %x,%x,%t", c.body, a, b, ok)
		}
	}
	for _, body := range [][]byte{nil, {0x20, 1, 0x0b}, {0x20, 0, 0x20, 0, 0x6c, 0x0b}, {0x20, 0, 0x0b, 0x01}, {0x20, 0, 0x28, 2, 0, 0x0b}, {0x41, 0x80}, {0x41, 0, 0x41, 0, 0x0b}} {
		if _, _, ok := inspectAffineI32Leaf(body); ok {
			t.Fatalf("accepted %x", body)
		}
	}
}
