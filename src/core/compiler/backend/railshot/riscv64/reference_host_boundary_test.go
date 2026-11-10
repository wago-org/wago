//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"fmt"
	"slices"
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

func publicDynamicHostReferenceBoundarySource(ordinary ...bool) []byte {
	types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
	callerTypes := append(slices.Clone(types), wasm.I32)
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	var args []byte
	for i := range types {
		args = append(args, 0x20, byte(i))
	}
	identity := append(slices.Clone(args), 0x0b)
	opcode := byte(0x15)
	if len(ordinary) != 0 && ordinary[0] {
		opcode = 0x14
	}
	body := []byte{0x20, byte(len(types)), 0x04, 3}
	body = append(body, args...)
	body = append(body, 0xd0, 2, opcode, 2, 0x05)
	body = append(body, args...)
	body = append(body, 0xd2, 0, opcode, 2, 0x0b, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(callerTypes, types), wasmtest.FuncType(types, types), wasmtest.FuncType(nil, types))),
		wasmtest.Section(2, wasmtest.Vec(im)),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("identity", 0, 1))),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(identity), wasmtest.Code(body))),
	)
}

func TestPublicDynamicHostReferenceBoundaryAdmission(t *testing.T) {
	for _, ordinary := range []bool{false, true} {
		t.Run(fmt.Sprintf("ordinary%t", ordinary), func(t *testing.T) {
			source := publicDynamicHostReferenceBoundarySource(ordinary)
			if len(source) != 174 || !admitPublicDynamicHostReferenceBoundaryWire(source) || AdmitProductionSource(source) != nil {
				t.Fatal("canonical dynamic host reference source was not admitted", len(source))
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			if !admitPublicDynamicHostReferenceBoundary(m) || !admitPublicReferenceCarrierBoundary(m) {
				t.Fatal("canonical dynamic host reference module was not recognized")
			}
			opts := closedReferenceProductionOptions(!ordinary, true)
			opts.ImportBindings = hostImportBindings(1)
			image, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if image == nil || (image.NativeTail != nil) == ordinary || !image.NativeReferenceFunctions || !image.NativeReferenceCalls || !image.NativeReferenceControl {
				t.Fatal("dynamic host reference compiler witnesses", image)
			}
			if ordinary {
				return
			}
			want := []enc.NativeTailPlan{{Kind: enc.NativeTailReference, TypeIndex: 2, FunctionIndex: 2, SourcePC: 19, FrameBytes: 160, SourceBase: 104, SourceStride: 8, ReferenceBase: 152}, {Kind: enc.NativeTailReference, TypeIndex: 2, FunctionIndex: 2, SourcePC: 36, FrameBytes: 160, SourceBase: 104, SourceStride: 8, ReferenceBase: 152}}
			if !slices.Equal(image.NativeTail.Plans, want) {
				t.Fatal("dynamic host reference compiler plans", image.NativeTail.Plans)
			}
		})
	}
}

func TestPublicDynamicHostReferenceBoundaryRejectsNearMisses(t *testing.T) {
	base := publicDynamicHostReferenceBoundarySource()
	mutations := map[string]func(*wasm.Module){
		"import-name":         func(m *wasm.Module) { m.Imports[0].Name = "other" },
		"import-type":         func(m *wasm.Module) { m.Imports[0].Type = wasm.NewFuncExternType(wasm.TypeIdx{Index: 1}) },
		"function-binding":    func(m *wasm.Module) { m.FuncTypes[1] = wasm.TypeIdx{Index: 2} },
		"target-type":         func(m *wasm.Module) { m.Types[2].SubTypes[0].Comp.Results[0] = wasm.I64 },
		"identity-local-decl": func(m *wasm.Module) { m.Code[0].LocalDeclBytes = 2 },
		"caller-local-decl":   func(m *wasm.Module) { m.Code[1].LocalDeclBytes = 2 },
		"identity-expr-body":  func(m *wasm.Module) { m.Code[0].Body.BodyBytes = []byte{0x0b} },
		"caller-expr-body":    func(m *wasm.Module) { m.Code[1].Body.BodyBytes = []byte{0x0b} },
		"identity-body":       func(m *wasm.Module) { m.Code[0].BodyBytes[1]++ },
		"caller-body":         func(m *wasm.Module) { m.Code[1].BodyBytes[18] = 0x14 },
		"element":             func(m *wasm.Module) { m.Elements[0].Kind.Funcs[0] = 1 },
		"export":              func(m *wasm.Module) { m.Exports[0].Index.Index = 1 },
		"table":               func(m *wasm.Module) { m.Tables = append(m.Tables, wasm.Table{}) },
		"name-metadata":       func(m *wasm.Module) { m.RawNameSecPayload = []byte{0} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(base)
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitPublicDynamicHostReferenceBoundary(candidate) {
				t.Fatal("dynamic host reference semantic near miss admitted")
			}
		})
	}
	wires := map[string][]byte{
		"truncated": append([]byte(nil), base[:len(base)-1]...),
		"trailing":  append(append([]byte(nil), base...), 0),
		"body":      append([]byte(nil), base...),
		"name":      append([]byte(nil), base...),
	}
	wires["body"][len(wires["body"])-3] ^= 1
	if at := bytes.Index(wires["name"], []byte("target")); at >= 0 {
		wires["name"][at] ^= 1
	}
	for name, source := range wires {
		if admitPublicDynamicHostReferenceBoundaryWire(source) {
			t.Fatal("dynamic host reference wire near miss admitted", name)
		}
	}
	for _, ordinary := range []bool{false, true} {
		source := publicDynamicHostReferenceBoundarySource(ordinary)
		decoded, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		needle := append([]byte{0}, decoded.Code[0].BodyBytes...)
		localAt := bytes.Index(source, needle)
		if localAt < 2 || source[localAt-1]&0x80 != 0 {
			t.Fatal("canonical first function body layout changed")
		}
		codeAt := bytes.LastIndex(source[:localAt], []byte{10})
		if codeAt < 0 || source[codeAt+1]&0x80 != 0 {
			t.Fatal("canonical code section layout changed")
		}
		noncanonical := append([]byte(nil), source[:localAt]...)
		noncanonical = append(noncanonical, 0x80, 0)
		noncanonical = append(noncanonical, source[localAt+1:]...)
		noncanonical[localAt-1]++
		noncanonical[codeAt+1]++
		candidate, err := wasm.DecodeModule(noncanonical)
		if err != nil {
			t.Fatal("decode noncanonical zero-local vector", err)
		}
		if candidate.Code[0].LocalDeclBytes != 2 || !bytes.Equal(candidate.Code[0].BodyBytes, decoded.Code[0].BodyBytes) {
			t.Fatal("noncanonical zero-local vector did not preserve decoded body", candidate.Code[0].LocalDeclBytes)
		}
		if admitPublicDynamicHostReferenceBoundary(candidate) || admitPublicDynamicHostReferenceBoundaryWire(noncanonical) {
			t.Fatal("noncanonical zero-local vector inherited dynamic host reference admission", ordinary)
		}
	}
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
	checkPublicPrimitiveModule(t, oversizedModule, oversizedOpts)
	// The private 64-slot profile stays closed; the public host limit is 256.
	tooMany := append(append([]wasm.ValType(nil), oversizedTypes...), make([]wasm.ValType, 257-len(oversizedTypes))...)
	for i := len(oversizedTypes); i < len(tooMany); i++ {
		tooMany[i] = wasm.I32
	}
	m, err := wasm.DecodeModule(publicHostReferenceBoundarySource(tooMany))
	if err != nil {
		t.Fatal(err)
	}
	if image, err := CompileModuleWith(m, oversizedOpts); image != nil || err == nil {
		t.Fatal("public host arity limit bypassed", err)
	}
}
