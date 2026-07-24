//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func TestSnapshotProductsRejectCodecV23ReferenceState(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	for _, tc := range []struct {
		name string
		wat  string
		want string
	}{
		{name: "table", wat: `(module (table 1 externref))`, want: "tables"},
		{name: "reference-global", wat: `(module (global externref (ref.null extern)))`, want: "reference global"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := MustCompile(watToWasm(t, tc.wat))
			inMemory := &Snapshot{c: c}
			if _, err := Instantiate(inMemory); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Instantiate(in-memory forged snapshot) = %v, want %q rejection", err, tc.want)
			}

			blob := rawSnapshotBlobForCompiled(t, c)
			if _, err := LoadSnapshot(blob); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadSnapshot(codec-v23 %s module) = %v, want %q rejection", tc.name, err, tc.want)
			}
		})
	}
}

func rawSnapshotBlobForCompiled(t *testing.T, c *Compiled) []byte {
	t.Helper()
	compiled, err := c.MarshalBinary()
	if err != nil {
		if guardPageBuilt {
			t.Skip("signals-based (guard-page) modules are not serializable")
		}
		t.Fatalf("MarshalBinary compiled snapshot fixture: %v", err)
	}
	out := append([]byte{}, snapshotMagic...)
	out = append(out, snapshotVersion, byte(SnapshotInit))
	out = binary.AppendUvarint(out, uint64(len(compiled)))
	out = append(out, compiled...)
	out = binary.AppendUvarint(out, 0) // memory pages
	out = binary.AppendUvarint(out, 0) // stored memory bytes
	out = binary.AppendUvarint(out, 0) // globals
	out = binary.AppendUvarint(out, 0) // passive data lengths
	return out
}

type inspectedModuleMetadata struct {
	functions []inspectedFunctionMetadata
	globals   []inspectedGlobalMetadata
	tables    []inspectedTableMetadata
}

type inspectedFunctionMetadata struct {
	index           int
	params, results []ValType
	importModule    string
	importName      string
	exports         []string
}

type inspectedGlobalMetadata struct {
	index        int
	type_        ValType
	mutable      bool
	importModule string
	importName   string
	exports      []string
}

type inspectedTableMetadata struct {
	index        int
	type_        ValType
	min          int
	max          int
	hasMax       bool
	importModule string
	importName   string
	exports      []string
}

func TestModuleMetadataReportsAllReferenceTypesAndTablesAfterCodecLoad(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "explicit")
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(watToWasm(t, `(module
		(import "env" "shared" (table 1 2 funcref))
		(import "env" "shared" (table 1 2 funcref))
		(import "env" "g" (global (mut externref)))
		(table 3 externref)
		(table 4 5 funcref)
		(global (export "fref") funcref (ref.null func))
		(func (export "refs") (param funcref externref) (result externref funcref)
			(local.get 1) (local.get 0))
		(export "first" (table 0))
		(export "second" (table 1))
		(export "local" (table 2))
		(export "bounded" (table 3)))`))
	if err != nil {
		t.Fatalf("Compile inspection fixture: %v", err)
	}

	want := inspectedModuleMetadata{
		functions: []inspectedFunctionMetadata{{index: 0, params: []ValType{ValFuncRef, ValExternRef}, results: []ValType{ValExternRef, ValFuncRef}, exports: []string{"refs"}}},
		globals: []inspectedGlobalMetadata{
			{index: 0, type_: ValExternRef, mutable: true, importModule: "env", importName: "g"},
			{index: 1, type_: ValFuncRef, exports: []string{"fref"}},
		},
		tables: []inspectedTableMetadata{
			{index: 0, type_: ValFuncRef, min: 1, max: 2, hasMax: true, importModule: "env", importName: "shared", exports: []string{"first"}},
			{index: 1, type_: ValFuncRef, min: 1, max: 2, hasMax: true, importModule: "env", importName: "shared", exports: []string{"second"}},
			{index: 2, type_: ValExternRef, min: 3, exports: []string{"local"}},
			{index: 3, type_: ValFuncRef, min: 4, max: 5, hasMax: true, exports: []string{"bounded"}},
		},
	}
	if got := inspectModuleMetadata(t, mod.Metadata()); !reflect.DeepEqual(got, want) {
		t.Fatalf("compiled ModuleMetadata = %#v, want %#v", got, want)
	}

	blob, err := mod.Compiled().MarshalBinary()
	if err != nil {
		if guardPageBuilt {
			t.Skip("signals-based (guard-page) modules are not serializable")
		}
		t.Fatalf("MarshalBinary inspection fixture: %v", err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatalf("Load inspection fixture: %v", err)
	}
	if got := inspectModuleMetadata(t, rt.buildModule(loaded).Metadata()); !reflect.DeepEqual(got, want) {
		t.Fatalf("loaded ModuleMetadata = %#v, want %#v", got, want)
	}
}

func inspectModuleMetadata(t *testing.T, meta ModuleMetadata) inspectedModuleMetadata {
	t.Helper()
	rv := reflect.ValueOf(meta)
	return inspectedModuleMetadata{
		functions: inspectFunctionMetadata(t, metadataSliceField(t, rv, "Functions")),
		globals:   inspectGlobalMetadata(t, metadataSliceField(t, rv, "Globals")),
		tables:    inspectTableMetadata(t, metadataSliceField(t, rv, "Tables")),
	}
}

func metadataSliceField(t *testing.T, rv reflect.Value, name string) reflect.Value {
	t.Helper()
	field := rv.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("ModuleMetadata is missing %s", name)
	}
	if field.Kind() != reflect.Slice {
		t.Fatalf("ModuleMetadata.%s kind = %s, want slice", name, field.Kind())
	}
	return field
}

func inspectFunctionMetadata(t *testing.T, values reflect.Value) []inspectedFunctionMetadata {
	t.Helper()
	out := make([]inspectedFunctionMetadata, values.Len())
	for i := range out {
		v := values.Index(i)
		out[i] = inspectedFunctionMetadata{
			index:        metadataIntField(t, v, "Index"),
			params:       metadataValTypesField(t, v, "Params"),
			results:      metadataValTypesField(t, v, "Results"),
			importModule: metadataStringField(t, v, "ImportModule"),
			importName:   metadataStringField(t, v, "ImportName"),
			exports:      metadataStringsField(t, v, "Exports"),
		}
	}
	return out
}

func inspectGlobalMetadata(t *testing.T, values reflect.Value) []inspectedGlobalMetadata {
	t.Helper()
	out := make([]inspectedGlobalMetadata, values.Len())
	for i := range out {
		v := values.Index(i)
		out[i] = inspectedGlobalMetadata{
			index:        metadataIntField(t, v, "Index"),
			type_:        metadataValTypeField(t, v, "Type"),
			mutable:      metadataBoolField(t, v, "Mutable"),
			importModule: metadataStringField(t, v, "ImportModule"),
			importName:   metadataStringField(t, v, "ImportName"),
			exports:      metadataStringsField(t, v, "Exports"),
		}
	}
	return out
}

func inspectTableMetadata(t *testing.T, values reflect.Value) []inspectedTableMetadata {
	t.Helper()
	out := make([]inspectedTableMetadata, values.Len())
	for i := range out {
		v := values.Index(i)
		out[i] = inspectedTableMetadata{
			index:        metadataIntField(t, v, "Index"),
			type_:        metadataValTypeField(t, v, "Type"),
			min:          metadataIntField(t, v, "Min"),
			max:          metadataIntField(t, v, "Max"),
			hasMax:       metadataBoolField(t, v, "HasMax"),
			importModule: metadataStringField(t, v, "ImportModule"),
			importName:   metadataStringField(t, v, "ImportName"),
			exports:      metadataStringsField(t, v, "Exports"),
		}
	}
	return out
}

func metadataField(t *testing.T, v reflect.Value, name string) reflect.Value {
	t.Helper()
	field := v.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("%s is missing %s", v.Type(), name)
	}
	return field
}

func metadataIntField(t *testing.T, v reflect.Value, name string) int {
	t.Helper()
	return int(metadataField(t, v, name).Int())
}

func metadataBoolField(t *testing.T, v reflect.Value, name string) bool {
	t.Helper()
	return metadataField(t, v, name).Bool()
}

func metadataStringField(t *testing.T, v reflect.Value, name string) string {
	t.Helper()
	return metadataField(t, v, name).String()
}

func metadataValTypeField(t *testing.T, v reflect.Value, name string) ValType {
	t.Helper()
	return metadataField(t, v, name).Interface().(ValType)
}

func metadataValTypesField(t *testing.T, v reflect.Value, name string) []ValType {
	t.Helper()
	return append([]ValType(nil), metadataField(t, v, name).Interface().([]ValType)...)
}

func metadataStringsField(t *testing.T, v reflect.Value, name string) []string {
	t.Helper()
	return append([]string(nil), metadataField(t, v, name).Interface().([]string)...)
}

func TestCrossLinkedReferenceOwnersCloseInExactOrder(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	producerMod, err := rt.Compile(watToWasm(t, `(module
		(table $fun 1 funcref)
		(table $ext 1 externref)
		(global $g (export "g") (mut externref) (ref.null extern))
		(func $target (result i32) (i32.const 42))
		(elem (table $fun) (i32.const 0) func $target)
		(export "fun" (table $fun))
		(export "ext" (table $ext)))`))
	if err != nil {
		t.Fatalf("Compile reference producer: %v", err)
	}
	consumerMod, err := rt.Compile(watToWasm(t, `(module
		(type $ret (func (result i32)))
		(import "producer" "g" (global $g (mut externref)))
		(import "producer" "fun" (table $fun0 1 funcref))
		(import "producer" "fun" (table $fun1 1 funcref))
		(import "producer" "ext" (table $ext 1 externref))
		(func (export "call0") (result i32)
			(call_indirect $fun0 (type $ret) (i32.const 0)))
		(func (export "call1") (result i32)
			(call_indirect $fun1 (type $ret) (i32.const 0)))
		(func (export "set") (param externref)
			(global.set $g (local.get 0))
			(table.set $ext (i32.const 0) (local.get 0)))
		(func (export "get-global") (result externref) (global.get $g))
		(func (export "get-table") (result externref) (table.get $ext (i32.const 0))))`))
	if err != nil {
		t.Fatalf("Compile reference consumer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatalf("Instantiate reference producer: %v", err)
	}
	global, err := producer.ExportedGlobalObject("g")
	if err != nil {
		t.Fatalf("Export producer global: %v", err)
	}
	funTable, err := producer.ExportedTable("fun")
	if err != nil {
		t.Fatalf("Export producer funcref table: %v", err)
	}
	extTable, err := producer.ExportedTable("ext")
	if err != nil {
		t.Fatalf("Export producer externref table: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerMod, WithImports(Imports{
		"producer.g":   global,
		"producer.fun": funTable,
		"producer.ext": extTable,
	}))
	if err != nil {
		t.Fatalf("Instantiate reference consumer: %v", err)
	}
	ref := issueExternref(t, rt, "cross-linked")
	if _, err := consumer.Call(context.Background(), "set", ValueExternRef(ref)); err != nil {
		t.Fatalf("seed linked reference state: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("logical close producer: %v", err)
	}
	producer.lifeMu.Lock()
	producerReleased := producer.resourcesClosed
	producer.lifeMu.Unlock()
	if producerReleased {
		t.Fatal("producer resources closed while linked reference owners were live")
	}
	global.owner.mu.Lock()
	globalImporters := global.owner.importers
	global.owner.mu.Unlock()
	funTable.owner.mu.Lock()
	funImporters := funTable.owner.importers
	funTable.owner.mu.Unlock()
	extTable.owner.mu.Lock()
	extImporters := extTable.owner.importers
	extTable.owner.mu.Unlock()
	if globalImporters != 1 || funImporters != 1 || extImporters != 1 {
		t.Fatalf("linked owner roots = global %d fun-table %d extern-table %d, want one per distinct owner", globalImporters, funImporters, extImporters)
	}
	for _, export := range []string{"call0", "call1"} {
		out, err := consumer.Invoke(export)
		if err != nil || len(out) != 1 || AsI32(out[0]) != 42 {
			t.Fatalf("%s after producer logical close = %v, %v; want 42", export, out, err)
		}
	}
	for _, export := range []string{"get-global", "get-table"} {
		out, err := consumer.Call(context.Background(), export)
		if err != nil || len(out) != 1 || out[0].ExternRef() != ref {
			t.Fatalf("%s after producer logical close = %v, %v; want %v", export, out, err, ref)
		}
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("Close reference consumer: %v", err)
	}
	global.owner.mu.Lock()
	globalImporters = global.owner.importers
	global.owner.mu.Unlock()
	funTable.owner.mu.Lock()
	funImporters = funTable.owner.importers
	funTable.owner.mu.Unlock()
	extTable.owner.mu.Lock()
	extImporters = extTable.owner.importers
	extTable.owner.mu.Unlock()
	if globalImporters != 0 || funImporters != 0 || extImporters != 0 {
		t.Fatalf("linked owner roots after consumer close = global %d fun-table %d extern-table %d, want zero", globalImporters, funImporters, extImporters)
	}
	producer.lifeMu.Lock()
	producerReleased = producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !producerReleased {
		t.Fatal("producer resources remained live after the final linked consumer closed")
	}
}

func TestWebAssembly2FeatureReportingCloseout(t *testing.T) {
	want := CoreFeaturesV2
	if !hostSupportsSIMD() {
		want &^= CoreFeatureSIMD
	}
	if got := SupportedFeatures(); got != want {
		t.Fatalf("SupportedFeatures = %s, want admitted WebAssembly 2.0 set %s", got, want)
	}
	if CoreFeaturesV2.IsEnabled(CoreFeatureTailCall) || SupportedFeatures().IsEnabled(CoreFeatureTailCall) {
		t.Fatal("WebAssembly 2.0 reporting unexpectedly includes the post-release tail-call proposal")
	}
}
