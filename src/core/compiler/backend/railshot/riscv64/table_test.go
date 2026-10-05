//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func ownedTableFixture() []byte {
	data := binaryModule(fixtureFunc{params: []byte{0x7f, 0x7f}, result: 0x7f, body: []byte{0x20, 1, 0x20, 0, 0x11, 1, 0, 0x0b}}, fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}, unexported: true})
	data = modifySection(data, 7, 4, []byte{1, 0x70, 0, 3}, false)
	return modifySection(data, 10, 9, []byte{1, 0, 0x41, 0, 0x0b, 1, 1}, false)
}

func TestProductionPrivateTableFramedIndirectAdmission(t *testing.T) {
	for _, heap := range []bool{false, true} {
		image, err := CompileProduction(ownedTableFixture(), ProductionOptions{DeferCodeMapping: heap})
		if err != nil {
			t.Fatal(err)
		}
		if err := image.CodeImage.Close(); err != nil {
			t.Fatal(err)
		}
	}
	c, err := compileOptions(ownedTableFixture(), scalarRegisters[:], CompileOptions{production: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.functions[0].FrameHeaderBytes != 48 || c.functions[0].DirectCalls != 0 || !c.functions[0].Fallback {
		t.Fatal("indirect call needs a guarded call frame without direct relocation", c.functions[0])
	}
	if c, err := Compile(ownedTableFixture()); c != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("isolated table admission widened", c, err)
	}
	// Even dead indirect sites affect frame admission; unsupported dead reference
	// operators are still rejected before the whole module emits.
	m, err := wasm.DecodeModule(ownedTableFixture())
	if err != nil {
		t.Fatal(err)
	}
	m.Code[0].BodyBytes = []byte{0x00, 0x11, 1, 0, 0x0b}
	state, err := admitStateMode(m, true)
	if err != nil {
		t.Fatal(err)
	}
	ft, _ := m.LocalFuncType(0)
	s, err := walkInteger(&m.Code[0], ft, &state, nil)
	if err != nil || s.calls != 1 {
		t.Fatal("dead indirect call was omitted from frame budget", s, err)
	}
	m.Code[0].BodyBytes = []byte{0x00, 0xd0, 0x70, 0x1a, 0x0b}
	if _, err := walkInteger(&m.Code[0], ft, &state, nil); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unsupported dead reference escaped", err)
	}
}

func TestProductionPrivateTableRejectsShapesBeforeStaging(t *testing.T) {
	base, err := wasm.DecodeModule(ownedTableFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*wasm.Module)
	}{
		{"second-table", func(m *wasm.Module) { m.Tables = append(m.Tables, m.Tables[0]) }},
		{"table64", func(m *wasm.Module) { m.Tables[0].Type.Limits.Addr64 = true }},
		{"too-large-min", func(m *wasm.Module) { m.Tables[0].Type.Limits.Min = 1025 }},
		{"too-large-max", func(m *wasm.Module) { m.Tables[0].Type.Limits.HasMax = true; m.Tables[0].Type.Limits.Max = 1025 }},
		{"nonnull-init", func(m *wasm.Module) { m.Tables[0].Init = &wasm.Expr{BodyBytes: []byte{0xd2, 1, 0x0b}} }},
		{"externref", func(m *wasm.Module) { m.Tables[0].Type.Ref = wasm.ExternRef.Ref() }},
		{"passive", func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemPassive }},
		{"declarative", func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemDeclarative }},
		{"indexed-table", func(m *wasm.Module) { m.Elements[0].Mode.Table = 1 }},
		{"expression-elements", func(m *wasm.Module) {
			m.Elements[0].Kind.Kind = wasm.ElemFuncExprs
			m.Elements[0].Kind.Exprs = []wasm.Expr{{BodyBytes: []byte{0xd2, 1, 0x0b}}}
		}},
		{"unused-expression-elements", func(m *wasm.Module) { m.Elements[0].Kind.Exprs = []wasm.Expr{{BodyBytes: []byte{0xd2, 1, 0x0b}}} }},
		{"extended-offset", func(m *wasm.Module) { m.Elements[0].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x41, 0, 0x6a, 0x0b} }},
		{"global-offset", func(m *wasm.Module) { m.Elements[0].Mode.Offset.BodyBytes = []byte{0x23, 0, 0x0b} }},
		{"function-outside-locals", func(m *wasm.Module) { m.Elements[0].Kind.Funcs = []wasm.FuncIdx{2} }},
		{"segment-count", func(m *wasm.Module) { m.Elements = make([]wasm.Elem, 257) }},
		{"aggregate-payload", func(m *wasm.Module) { m.Elements[0].Kind.Funcs = make([]wasm.FuncIdx, 1025) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := *base
			m.Tables = append([]wasm.Table(nil), base.Tables...)
			m.Elements = append([]wasm.Elem(nil), base.Elements...)
			tc.mutate(&m)
			if _, err := admitOwnedTable(&m, true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("table shape escaped", err)
			}
			pressure := 0
			image, err := CompileModuleWith(&m, CompileOptions{DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
			if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("table shape reached image/pressure", image, err, pressure)
			}
		})
	}
}

func TestProductionPrivateTableAggregateBoundsAndUnsupportedExports(t *testing.T) {
	base, err := wasm.DecodeModule(ownedTableFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, segments := range []int{2, 256} {
		m := *base
		m.Tables = append([]wasm.Table(nil), base.Tables...)
		m.Tables[0].Type.Limits.Min = 1024
		m.Elements = make([]wasm.Elem, segments)
		for i := range m.Elements {
			m.Elements[i] = base.Elements[0]
			m.Elements[i].Kind.Funcs = nil
			if segments == 2 {
				m.Elements[i].Kind.Funcs = make([]wasm.FuncIdx, 512)
				for j := range m.Elements[i].Kind.Funcs {
					m.Elements[i].Kind.Funcs[j] = 1
				}
				m.Elements[i].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b}
			}
		}
		if err := wasm.ValidateModule(&m); err != nil {
			t.Fatal("invalid positive bound fixture", err)
		}
		if _, err := admitStateMode(&m, true); err != nil {
			t.Fatal("supported bound rejected", segments, err)
		}
		image, err := CompileModuleWith(&m, CompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := image.CodeImage.Close(); err != nil {
			t.Fatal(err)
		}
	}
	m := *base
	m.Exports = append(append([]wasm.Export(nil), base.Exports...), wasm.Export{Name: "escape", Index: wasm.ExternIdx{Kind: wasm.ExternTable, Index: 0}})
	if err := wasm.ValidateModule(&m); err != nil {
		t.Fatal("export rejection fixture must be valid", err)
	}
	if _, err := admitStateMode(&m, true); !errors.Is(err, ErrUnsupported) {
		t.Fatal("private table exported", err)
	}
	if image, err := CompileModuleWith(&m, CompileOptions{DeferCodeMapping: true}); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("export reached image", image, err)
	}
}
