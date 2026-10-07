//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"strconv"
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
	m.Code[0].BodyBytes = []byte{0x00, 0xd0, 0x6f, 0x1a, 0x0b}
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
		{"inactive-offset", func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemPassive }},
		{"unknown-mode", func(m *wasm.Module) { m.Elements[0].Mode.Kind = wasm.ElemModeKind(255) }},
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
	if image, err := CompileModuleWith(&m, CompileOptions{DeferCodeMapping: true}); image == nil || err != nil || len(image.Entry) != len(m.Code) {
		t.Fatal("public owned table export did not reach staging", image, err)
	}
}

func TestProductionOwnedTableExportAliasBounds(t *testing.T) {
	base, err := wasm.DecodeModule(ownedTableFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{MaxFunctions, MaxFunctions + 1} {
		m := *base
		m.Exports = make([]wasm.Export, count)
		for i := range m.Exports {
			m.Exports[i] = wasm.Export{Name: "table" + strconv.Itoa(i), Index: wasm.ExternIdx{Kind: wasm.ExternTable}}
		}
		if err := wasm.ValidateModule(&m); err != nil {
			t.Fatal("alias boundary fixture must be valid", err)
		}
		pressure := 0
		image, err := CompileModuleWith(&m, CompileOptions{DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
		if count == MaxFunctions {
			if err != nil || image == nil || len(image.Entry) != len(m.Code) || pressure != 1 {
				t.Fatal("bounded aliases did not stage", err, pressure)
			}
		} else if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
			t.Fatal("excess aliases reached image/pressure", err, pressure)
		}
	}
}

func TestProductionPrivateReferenceAdmissionAndCarrierClasses(t *testing.T) {
	data := binaryModule(
		fixtureFunc{params: []byte{0x7f}, result: 0x7f, locals: []byte{1, 1, 0x70}, body: []byte{
			0xd2, 1, 0x21, 1, 0x41, 0, 0x20, 1, 0x26, 0,
			0x41, 0, 0x25, 0, 0xd1, 0xfc, 16, 0, 0x6a, 0x0b,
		}},
		fixtureFunc{params: []byte{0x7f}, result: 0x7f, body: []byte{0x20, 0, 0x0b}, unexported: true},
	)
	data = modifySection(data, 7, 4, []byte{1, 0x70, 0, 3}, false)
	data = modifySection(data, 10, 9, []byte{1, 0, 0x41, 0, 0x0b, 1, 1}, false)
	c, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CodeImage.Close()
	if _, err := Compile(data); !errors.Is(err, ErrUnsupported) {
		t.Fatal("isolated private references admitted", err)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	state, err := admitStateMode(m, true)
	if err != nil {
		t.Fatal(err)
	}
	ft, _ := m.LocalFuncType(0)
	for _, body := range [][]byte{
		{0x42, 0, 0x21, 1, 0x41, 0, 0x0b}, // a wide integer is not a reference
		{0xd2, 1, 0x50, 0x0b},             // a reference is not an i64 arithmetic input
		{0x00, 0xd0, 0x6f, 0x1a, 0x0b},    // unsupported dead externref
		{0x00, 0xfc, 17, 1, 0x0b},         // unsupported dead indexed table.fill
		{0x00, 0x25, 1, 0x1a, 0x0b},       // unsupported dead table index
		{0x00, 0x02, 0x6f, 0x0b, 0x0b},    // externref control results remain gated
	} {
		m.Code[0].BodyBytes = body
		if _, err := walkInteger(&m.Code[0], ft, &state, nil); !errors.Is(err, ErrUnsupported) {
			t.Fatal("carrier/shape escaped admission", body, err)
		}
	}
	// Inline nullable funcref control results preserve their reference class
	// when a branch joins, while the function's public signature stays numeric.
	m.Code[0].BodyBytes = []byte{0x02, 0x70, 0xd2, 1, 0x0c, 0, 0x0b, 0x21, 1, 0x41, 0, 0x0b}
	if _, err := walkInteger(&m.Code[0], ft, &state, nil); err != nil {
		t.Fatal("inline reference control result lost its class", err)
	}
}

func TestProductionPrivateTableGrowthCapacityBeforeStaging(t *testing.T) {
	for _, tc := range []struct {
		minimum       uint32
		hasMax, admit bool
	}{{512, false, true}, {513, false, false}, {513, true, true}} {
		data := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0xd2, 1, 0x41, 1, 0xfc, 15, 0, 0x0b}}, fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}, unexported: true})
		flag := byte(0)
		if tc.hasMax {
			flag = 1
		}
		table := uleb([]byte{1, 0x70, flag}, tc.minimum)
		if tc.hasMax {
			table = uleb(table, 1024)
		}
		data = modifySection(data, 7, 4, table, false)
		data = modifySection(data, 10, 9, []byte{1, 0, 0x41, 0, 0x0b, 1, 1}, false)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		pressure := 0
		c, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
		if !tc.admit {
			if c != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("growth reached staging", c, err, pressure)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if c.CodeImage != nil {
			t.Fatal("deferred shared contract retained mapping", c.CodeImage)
		}
		// Dead growth sites enforce the same reserve bound before emission.
		m.Tables[0].Type.Limits.HasMax = false
		m.Tables[0].Type.Limits.Min = 513
		m.Code[0].BodyBytes = []byte{0x00, 0xfc, 15, 0, 0x0b}
		state, err := admitStateMode(m, true)
		if err != nil {
			t.Fatal(err)
		}
		ft, _ := m.LocalFuncType(0)
		if _, err := walkInteger(&m.Code[0], ft, &state, nil); !errors.Is(err, ErrUnsupported) {
			t.Fatal("dead growth reserve escaped", err)
		}
	}
}

func TestProductionPrivateElementsOriginalIndexAdmission(t *testing.T) {
	m, err := wasm.DecodeModule(ownedTableFixture())
	if err != nil {
		t.Fatal(err)
	}
	element := m.Elements[0]
	element.Mode = wasm.ElemMode{Kind: wasm.ElemPassive}
	m.Elements = make([]wasm.Elem, 256)
	for i := range m.Elements {
		m.Elements[i] = element
	}
	m.Code[0].BodyBytes = []byte{0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 12, 0xff, 1, 0, 0xfc, 13, 0xff, 1, 0x20, 1, 0x0b}
	c, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.CodeImage != nil {
		t.Fatal("unexpected deferred mapping")
	}
	state, err := admitStateMode(m, true)
	if err != nil {
		t.Fatal(err)
	}
	ft, _ := m.LocalFuncType(0)
	for _, body := range [][]byte{
		{0x00, 0xfc, 12, 0x80, 2, 0, 0x0b}, // element256
		{0x00, 0xfc, 13, 0x80, 2, 0x0b},
		{0x00, 0xfc, 12, 0xff, 1, 1, 0x0b}, // table1
	} {
		m.Code[0].BodyBytes = body
		if _, err := walkInteger(&m.Code[0], ft, &state, nil); !errors.Is(err, ErrUnsupported) {
			t.Fatal("dead element immediate escaped", body, err)
		}
	}
	m.Elements[255].Mode.Kind = wasm.ElemDeclarative
	if _, err := admitOwnedTable(m, true); err != nil {
		t.Fatal("declarative legacy mode", err)
	}
	// Aggregate bounds include declaration payloads even though they are dropped.
	m.Elements[255].Kind.Funcs = make([]wasm.FuncIdx, 1024)
	pressure := 0
	if c, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, MemoryPressure: func() { pressure++ }}); c != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
		t.Fatal("aggregate reached staging", c, err, pressure)
	}
}
