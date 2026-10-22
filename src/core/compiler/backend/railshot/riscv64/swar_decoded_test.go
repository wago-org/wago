//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSWARDecodedAdapterIdenticalImagesAndOwnedSnapshots(t *testing.T) {
	plain := binaryModule(fixtureFunc{params: []byte{0x7b, 0x7f}, results: []byte{0x7b, 0x7f}, locals: []byte{1, 126, 0x7b}, body: []byte{0x20, 0, 0x20, 1, 0x0b}})
	table, _, _, _ := swarCombinedStateSource()
	host, _, _, _, _ := swarHostCombinedStateSource(256)
	for _, test := range []struct {
		name   string
		source []byte
		policy swarModulePolicy
	}{{"plain", plain, swarModulePlain}, {"table", table, swarModuleTables}, {"host", host, swarModuleHosts}} {
		t.Run(test.name, func(t *testing.T) {
			mode := emissionMode{production: true, interruptible: true}
			wire, err := compileSWARModulePolicy(test.source, mode, test.policy)
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(test.source)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := compileSWARDecodedModule(m, mode, test.policy)
			if err != nil || !reflect.DeepEqual(decoded, wire) {
				t.Fatal("wire/decoded image, relocation, source metadata or state differs", err)
			}
			for i := range m.Code {
				clear(m.Code[i].BodyBytes)
				clear(m.Code[i].Locals.Runs)
			}
			for i := range m.Types {
				clear(m.Types[i].SubTypes[0].Comp.Params)
				clear(m.Types[i].SubTypes[0].Comp.Results)
			}
			for i := range m.Globals {
				clear(m.Globals[i].Init.BodyBytes)
			}
			for i := range m.Data {
				clear(m.Data[i].Init)
				clear(m.Data[i].Mode.Offset.BodyBytes)
			}
			for i := range m.Elements {
				clear(m.Elements[i].Kind.Funcs)
				clear(m.Elements[i].Mode.Offset.BodyBytes)
			}
			for i := range m.Memories {
				m.Memories[i].Limits.Min = 65536
			}
			for i := range m.Tables {
				m.Tables[i].Type.Limits.Min = MaxOwnedTableEntries
			}
			clear(test.source)
			if !reflect.DeepEqual(decoded, wire) {
				t.Fatal("decoded image retained caller bodies/types/state storage")
			}
		})
	}
}

func TestSWARDecodedAdapterBudgetsAndNormalValidation(t *testing.T) {
	base := binaryModule(fixtureFunc{body: []byte{0x0b}})
	mode := emissionMode{production: true, interruptible: true}
	for _, test := range []struct {
		name   string
		mutate func(*wasm.Module)
	}{
		{"body-bytes", func(m *wasm.Module) { m.Code[0].BodyBytes = bytes.Repeat([]byte{1}, (64<<10)+1) }},
		{"declaration-bytes", func(m *wasm.Module) { m.Code[0].LocalDeclBytes = MaxModuleBytes + 1 }},
		{"local-run-count", func(m *wasm.Module) { m.Code[0].Locals.Runs = make([]wasm.LocalRun, maxSWARLocals+1) }},
		{"missing-body-bytes", func(m *wasm.Module) { m.Code[0].BodyBytes = nil }},
		{"duplicate-body", func(m *wasm.Module) { m.Code[0].Body.BodyBytes = []byte{0x0b} }},
		{"structured-body", func(m *wasm.Module) { m.Code[0].Body.Instrs = []wasm.Instruction{{Kind: wasm.InstrNop}} }},
		{"signature", func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Params = make([]wasm.ValType, maxSWARLocals+1) }},
		{"type-fields", func(m *wasm.Module) { m.Types[0].SubTypes[0].Comp.Fields = []wasm.FieldType{{}} }},
		{"type-supers", func(m *wasm.Module) { m.Types[0].SubTypes[0].Supers = []wasm.TypeIdx{{Index: 0}} }},
		{"structured-table-init", func(m *wasm.Module) {
			m.Tables = []wasm.Table{{Init: &wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrRefFunc, Index: ^uint32(0)}}}}}
		}},
		{"oversized-table-init", func(m *wasm.Module) {
			m.Tables = []wasm.Table{{Init: &wasm.Expr{BodyBytes: make([]byte, MaxModuleBytes+1)}}}
		}},
		{"exports", func(m *wasm.Module) { m.Exports = make([]wasm.Export, MaxFunctions+1) }},
		{"custom-count", func(m *wasm.Module) { m.Customs = make([]wasm.CustomSec, MaxFunctions+1) }},
		{"custom-bytes", func(m *wasm.Module) {
			m.Customs = []wasm.CustomSec{{Name: "producers", Data: make([]byte, MaxModuleBytes)}}
		}},
		{"export-name", func(m *wasm.Module) { m.Exports[0].Name = string(bytes.Repeat([]byte{'a'}, MaxModuleBytes)) }},
		{"name-payload", func(m *wasm.Module) { m.RawNameSecPayload = make([]byte, MaxModuleBytes) }},
		{"parsed-module-name", func(m *wasm.Module) {
			name := string(bytes.Repeat([]byte{'a'}, MaxModuleBytes))
			m.NameSec = &wasm.NameSec{ModuleName: &name}
		}},
		{"parsed-name-map", func(m *wasm.Module) { m.NameSec = &wasm.NameSec{FunctionNames: make(wasm.NameMap, MaxFunctions+1)} }},
		{"parsed-name-string", func(m *wasm.Module) {
			m.NameSec = &wasm.NameSec{FunctionNames: wasm.NameMap{{Name: string(bytes.Repeat([]byte{'a'}, MaxModuleBytes))}}}
		}},
		{"parsed-indirect-map", func(m *wasm.Module) {
			m.NameSec = &wasm.NameSec{LocalNames: make(wasm.IndirectNameMap, MaxFunctions+1)}
		}},
		{"parsed-indirect-rows", func(m *wasm.Module) {
			m.NameSec = &wasm.NameSec{LabelNames: wasm.IndirectNameMap{{Names: make(wasm.NameMap, MaxFunctions+1)}}}
		}},
		{"parsed-indirect-string", func(m *wasm.Module) {
			m.NameSec = &wasm.NameSec{FieldNames: wasm.IndirectNameMap{{Names: wasm.NameMap{{Name: string(bytes.Repeat([]byte{'a'}, MaxModuleBytes))}}}}}
		}},
		{"global-structured", func(m *wasm.Module) {
			m.Globals = []wasm.Global{{Type: wasm.GlobalType{Type: wasm.I32}, Init: wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrI32Const}}}}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(base)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(m)
			if image, err := compileSWARDecodedModule(m, mode, swarModulePlain); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("decoded budget/byte provenance reached an image", err)
			}
		})
	}
	for _, n := range []int{MaxModuleBytes - 3, MaxModuleBytes - 2} {
		source := swarDataSource([]fixtureFunc{{body: []byte{0x0b}}}, false, [][]byte{{1}})
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		// One byte body, one byte declarations and one export-name byte share
		// the aggregate budget with the passive payload. No memory is needed.
		m.Data[0].Init = make([]byte, n)
		image, err := compileSWARDecodedModule(m, mode, swarModulePlain)
		if n == MaxModuleBytes-3 && (err != nil || len(image.data[0]) != n) || n == MaxModuleBytes-2 && (image != nil || !errors.Is(err, ErrUnsupported)) {
			t.Fatal("decoded aggregate byte boundary", n, err)
		}
	}
	m, err := wasm.DecodeModule(base)
	if err != nil {
		t.Fatal(err)
	}
	m.Code[0].BodyBytes = []byte{0x20, 0, 0x0b} // invalid local, within every budget
	if image, err := compileSWARDecodedModule(m, mode, swarModulePlain); image != nil || err == nil || errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded adapter skipped normal validation", err)
	}
	for _, test := range []struct {
		module *wasm.Module
		mode   emissionMode
		policy swarModulePolicy
	}{{nil, mode, swarModulePlain}, {m, emissionMode{}, swarModulePlain}, {m, mode, swarModuleHosts + 1}} {
		if image, err := compileSWARDecodedModule(test.module, test.mode, test.policy); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded caller/policy gate", err)
		}
	}
}

func TestSWARDecodedAdapterInertNamesAndAggregateElementPreflight(t *testing.T) {
	base := binaryModule(fixtureFunc{body: []byte{0x0b}})
	// Name subsections: module name "m", function0 "f", local0 "x".
	source := section(base, 0, []byte{4, 'n', 'a', 'm', 'e', 0, 2, 1, 'm', 1, 4, 1, 0, 1, 'f', 2, 6, 1, 0, 1, 0, 1, 'x'})
	m, err := wasm.DecodeModule(source)
	if err != nil || m.NameSec == nil {
		t.Fatal("inert parsed name fixture", err)
	}
	mode := emissionMode{production: true, interruptible: true}
	image, err := compileSWARDecodedModule(m, mode, swarModulePlain)
	if err != nil || image == nil {
		t.Fatal("ordinary inert name metadata rejected", err)
	}
	// Both segments are separately within1024; aggregate is one entry over.
	m.Elements = []wasm.Elem{{Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: make([]wasm.FuncIdx, MaxOwnedTableEntries)}}, {Kind: wasm.ElemKind{Kind: wasm.ElemFuncs, Funcs: []wasm.FuncIdx{0}}}}
	if image, err := compileSWARDecodedModule(m, mode, swarModuleTables); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("aggregate element payload reached validation/image", err)
	}
}

func TestSWARDecodedAdapterRequiresSeparateWireProvenance(t *testing.T) {
	base := binaryModule(fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}})
	mode := emissionMode{production: true, interruptible: true}
	for _, source := range [][]byte{
		modifySection(base, 7, 4, []byte{0}, false),
		modifySection(base, 1, 1, []byte{1, 0x4e, 1, 0x60, 1, 0x7b, 1, 0x7b}, true),
	} {
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 128}); err != nil {
			t.Fatal("wire provenance fixture must be normally valid", err)
		}
		if image, err := compileSWARDecodedModule(m, mode, swarModulePlain); err != nil || image == nil {
			t.Fatal("decoded shape unexpectedly retained lost wire provenance", err)
		}
		if image, err := compileSWARModule(source, mode); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("wire admission lost empty-section/explicit-rec provenance", err)
		}
	}
}

func TestSWARDecodedAdapterNamePayloadBudgetBoundary(t *testing.T) {
	mode := emissionMode{production: true, interruptible: true}
	for _, n := range []int{MaxModuleBytes - 11, MaxModuleBytes - 10} {
		// An unknown name subsection is inert and retained in both raw and
		// custom representations. Count its payload once: subsection header4,
		// custom name4, body1, declarations1 and export name1 consume11 bytes.
		payload := uleb([]byte{99}, uint32(n))
		payload = append(payload, make([]byte, n)...)
		source := section(binaryModule(fixtureFunc{body: []byte{0x0b}}), 0, append([]byte{4, 'n', 'a', 'm', 'e'}, payload...))
		m, err := wasm.DecodeModule(source)
		if err != nil || len(m.Customs) != 1 || !bytes.Equal(m.Customs[0].Data, m.RawNameSecPayload) {
			t.Fatal("retained duplicate raw name fixture", err)
		}
		image, err := compileSWARDecodedModule(m, mode, swarModulePlain)
		if n == MaxModuleBytes-11 && (err != nil || image == nil) || n == MaxModuleBytes-10 && (image != nil || !errors.Is(err, ErrUnsupported)) {
			t.Fatal("decoded name payload aggregate boundary", n, err)
		}
	}
}
