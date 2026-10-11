//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func publicHostReferenceBoundarySource(types []wasm.ValType, tail ...bool) []byte {
	body := make([]byte, 0, 2*len(types)+3)
	for i := range types {
		body = append(body, 0x20)
		body = appendPublicReferenceBoundaryU32(body, uint32(i))
	}
	opcode := byte(0x10)
	if len(tail) != 0 && tail[0] {
		opcode = 0x12
	}
	body = append(body, opcode, 0, 0x0b)
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(2, wasmtest.Vec(im)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestPublicHostReferenceBoundaryAdmission(t *testing.T) {
	types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
	source := publicHostReferenceBoundarySource(types)
	if !admitPublicHostReferenceBoundaryWire(source) || AdmitProductionSource(source) != nil {
		t.Fatal("canonical host reference source was not admitted")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitPublicHostReferenceBoundary(m) || !admitPublicReferenceCarrierBoundary(m) {
		t.Fatal("canonical host reference module was not recognized")
	}
	if err := admitClosedReferenceFunctionModule(m, false); err != nil {
		t.Fatal("closed host reference module", err)
	}
	opts := closedReferenceProductionOptions(false, false)
	opts.ImportBindings = hostImportBindings(1)
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal("host reference lowering", err)
	}
	if image == nil || !image.NativeReferenceFunctions || image.NativeReferenceCalls || image.NativeReferenceControl || len(image.Code) == 0 || len(image.Entry) != 1 {
		t.Fatal("host reference compiler witnesses", image)
	}
	tailSource := publicHostReferenceBoundarySource(types, true)
	if !admitPublicHostReferenceBoundaryWire(tailSource) {
		t.Fatal("canonical host reference tail source was not admitted")
	}
	tailModule, err := wasm.DecodeModule(tailSource)
	if err != nil {
		t.Fatal(err)
	}
	tailOpts := closedReferenceProductionOptions(true, false)
	tailOpts.ImportBindings = hostImportBindings(1)
	tailImage, err := CompileModuleWith(tailModule, tailOpts)
	if err != nil {
		t.Fatal("host reference tail lowering", err)
	}
	if tailImage == nil || tailImage.NativeTail == nil || len(tailImage.NativeTail.Plans) != 1 {
		t.Fatal("host reference tail compiler plan", tailImage)
	}
	p := tailImage.NativeTail.Plans[0]
	if p.Kind != enc.NativeTailDirectImport || p.TypeIndex != 0 || p.FunctionIndex != 1 || p.ImportIndex != 0 || p.SourcePC != 13 || p.FrameBytes != 144 || p.SourceBase != 96 || p.SourceStride != 8 || p.ReferenceBase != 0 {
		t.Fatal("host reference tail compiler plan fields", p)
	}
}

func TestPublicHostReferenceTailPlanLayouts(t *testing.T) {
	wide := make([]wasm.ValType, 64)
	wide[0] = wasm.FuncRef
	for i := 1; i < len(wide); i++ {
		wide[i] = wasm.I32
	}
	for _, tc := range []struct {
		name                  string
		types                 []wasm.ValType
		pc, frame, sourceBase int
	}{
		{"one", []wasm.ValType{wasm.ExternRef}, 3, 64, 56},
		{"max-host-arity", wide, 129, 1072, 560},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(publicHostReferenceBoundarySource(tc.types, true))
			if err != nil {
				t.Fatal(err)
			}
			opts := closedReferenceProductionOptions(true, false)
			opts.ImportBindings = hostImportBindings(1)
			image, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if image == nil || image.NativeTail == nil || len(image.NativeTail.Plans) != 1 {
				t.Fatal("host reference tail plan", image)
			}
			p := image.NativeTail.Plans[0]
			if p.Kind != enc.NativeTailDirectImport || p.TypeIndex != 0 || p.FunctionIndex != 1 || p.ImportIndex != 0 || p.SourcePC != uint32(tc.pc) || p.FrameBytes != tc.frame || p.SourceBase != tc.sourceBase || p.SourceStride != 8 || p.ReferenceBase != 0 {
				t.Fatal("host reference tail layout", p)
			}
		})
	}
}

func TestPublicHostReferenceBoundaryRejectsNearMisses(t *testing.T) {
	base := publicHostReferenceBoundarySource([]wasm.ValType{wasm.FuncRef, wasm.ExternRef, wasm.I32})
	decoded, err := wasm.DecodeModule(base)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*wasm.Module){
		"second-import": func(m *wasm.Module) { m.Imports = append(m.Imports, m.Imports[0]) },
		"wrong-module":  func(m *wasm.Module) { m.Imports[0].Module = "other" },
		"wrong-name":    func(m *wasm.Module) { m.Imports[0].Name = "other" },
		"wrong-type":    func(m *wasm.Module) { m.Imports[0].Type = wasm.NewFuncExternType(wasm.TypeIdx{Index: 1}) },
		"second-code": func(m *wasm.Module) {
			m.Code = append(m.Code, m.Code[0])
			m.FuncTypes = append(m.FuncTypes, m.FuncTypes[0])
		},
		"wrong-binding": func(m *wasm.Module) { m.FuncTypes[0] = wasm.TypeIdx{Index: 1} },
		"body":          func(m *wasm.Module) { m.Code[0].BodyBytes[2] = 0 },
		"export-name":   func(m *wasm.Module) { m.Exports[0].Name = "other" },
		"export-index":  func(m *wasm.Module) { m.Exports[0].Index.Index = 0 },
		"start":         func(m *wasm.Module) { v := wasm.FuncIdx(1); m.Start = &v },
		"table":         func(m *wasm.Module) { m.Tables = append(m.Tables, wasm.Table{}) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := *decoded
			candidate.Types = append([]wasm.RecType(nil), decoded.Types...)
			candidate.Imports = append([]wasm.Import(nil), decoded.Imports...)
			candidate.FuncTypes = append([]wasm.TypeIdx(nil), decoded.FuncTypes...)
			candidate.Code = append([]wasm.Func(nil), decoded.Code...)
			candidate.Code[0].BodyBytes = append([]byte(nil), decoded.Code[0].BodyBytes...)
			candidate.Exports = append([]wasm.Export(nil), decoded.Exports...)
			mutate(&candidate)
			if admitPublicHostReferenceBoundary(&candidate) {
				t.Fatal("near miss admitted")
			}
		})
	}

	wireMutations := map[string][]byte{
		"truncated":   append([]byte(nil), base[:len(base)-1]...),
		"trailing":    append(append([]byte(nil), base...), 0),
		"section-id":  append([]byte(nil), base...),
		"import-name": append([]byte(nil), base...),
		"call-opcode": append([]byte(nil), base...),
	}
	wireMutations["section-id"][8] = 2
	if at := bytes.Index(wireMutations["import-name"], []byte("target")); at >= 0 {
		wireMutations["import-name"][at] = 'x'
	}
	if at := bytes.LastIndex(wireMutations["call-opcode"], []byte{0x10, 0, 0x0b}); at >= 0 {
		wireMutations["call-opcode"][at] = 0x0f
	}
	for name, candidate := range wireMutations {
		if admitPublicHostReferenceBoundaryWire(candidate) {
			t.Fatalf("wire %s mutation admitted", name)
		}
	}

	numeric := publicHostReferenceBoundarySource([]wasm.ValType{wasm.I32})
	if admitPublicHostReferenceBoundaryWire(numeric) {
		t.Fatal("numeric host wrapper inherited reference boundary")
	}
	if err := AdmitProductionSource(numeric); err != nil {
		t.Fatal("ordinary numeric host wrapper", err)
	}
	oversizedTypes := make([]wasm.ValType, 65)
	oversizedTypes[0] = wasm.FuncRef
	for i := 1; i < len(oversizedTypes); i++ {
		oversizedTypes[i] = wasm.I32
	}
	oversized := publicHostReferenceBoundarySource(oversizedTypes)
	oversizedModule, err := wasm.DecodeModule(oversized)
	if err != nil {
		t.Fatal(err)
	}
	oversizedOpts := closedReferenceProductionOptions(false, false)
	oversizedOpts.ImportBindings = hostImportBindings(1)
	if admitPublicHostReferenceBoundaryWire(oversized) || admitPublicHostReferenceBoundary(oversizedModule) {
		t.Fatal("oversized host reference wrapper admitted")
	}
	if image, err := CompileModuleWith(oversizedModule, oversizedOpts); err == nil || image != nil {
		t.Fatal("oversized host reference wrapper compiled", image, err)
	}
}
