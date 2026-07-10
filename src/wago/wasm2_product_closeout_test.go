//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func TestClassReferenceStateIsIsolatedOrRejectedForEveryResetPolicy(t *testing.T) {
	localWasm := watToWasm(t, `(module
		(table $fun 1 funcref)
		(table $ext 1 externref)
		(global $g (mut externref) (ref.null extern))
		(func $dummy)
		(elem $fp funcref (ref.func $dummy))
		(func (export "set-global") (param externref)
			(global.set $g (local.get 0)))
		(func (export "get-global") (result externref)
			(global.get $g))
		(func (export "set-ext") (param i32 externref)
			(table.set $ext (local.get 0) (local.get 1)))
		(func (export "get-ext") (param i32) (result externref)
			(table.get $ext (local.get 0)))
		(func (export "init-fun")
			(table.init $fun $fp (i32.const 0) (i32.const 0) (i32.const 1)))
		(func (export "drop-fun") (elem.drop $fp))
		(func (export "fun-null") (result i32)
			(ref.is_null (table.get $fun (i32.const 0)))))`)
	importedWasm := watToWasm(t, `(module
		(import "env" "g" (global (mut externref)))
		(import "env" "t" (table 1 externref)))`)

	for _, policy := range []ResetPolicy{ResetReinstantiate, ResetMemorySnapshot, ResetCopyOnWrite} {
		t.Run(policy.String(), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			mod, err := rt.Compile(localWasm)
			if err != nil {
				t.Fatalf("Compile local reference class: %v", err)
			}
			class, err := rt.Class(mod, ClassOptions{Pool: PoolOptions{MinInstances: 1, MaxInstances: 1, Reset: policy}})
			if err != nil {
				t.Fatalf("Class local reference state: %v", err)
			}
			defer class.Close()

			firstRef := issueExternref(t, rt, "tenant-one")
			lease, err := class.Acquire(context.Background())
			if err != nil {
				t.Fatalf("Acquire tenant one: %v", err)
			}
			first := lease.Instance()
			if _, err := first.Call(context.Background(), "set-global", ValueExternRef(firstRef)); err != nil {
				t.Fatalf("set tenant-one global: %v", err)
			}
			if _, err := first.Call(context.Background(), "set-ext", ValueI32(0), ValueExternRef(firstRef)); err != nil {
				t.Fatalf("set tenant-one table: %v", err)
			}
			if _, err := first.Invoke("init-fun"); err != nil {
				t.Fatalf("initialize tenant-one funcref table: %v", err)
			}
			if _, err := first.Invoke("drop-fun"); err != nil {
				t.Fatalf("drop tenant-one passive element: %v", err)
			}
			if err := lease.Release(); err != nil {
				t.Fatalf("Release tenant one: %v", err)
			}

			lease, err = class.Acquire(context.Background())
			if err != nil {
				t.Fatalf("Acquire tenant two: %v", err)
			}
			defer lease.Release()
			second := lease.Instance()
			if second == first {
				t.Fatal("reference-bearing Class reused an instance in place instead of fresh reinstantiation")
			}
			for _, export := range []string{"get-global", "get-ext"} {
				var out []Value
				if export == "get-ext" {
					out, err = second.Call(context.Background(), export, ValueI32(0))
				} else {
					out, err = second.Call(context.Background(), export)
				}
				if err != nil || len(out) != 1 || !out[0].ExternRef().IsNull() {
					t.Fatalf("tenant-two %s = %v, %v; want null", export, out, err)
				}
			}
			out, err := second.Invoke("fun-null")
			if err != nil || len(out) != 1 || AsI32(out[0]) != 1 {
				t.Fatalf("tenant-two fun-null = %v, %v; want 1", out, err)
			}
			if _, err := second.Invoke("init-fun"); err != nil {
				t.Fatalf("tenant-two passive element was not restored: %v", err)
			}
			secondRef := issueExternref(t, rt, "tenant-two")
			if _, err := second.Call(context.Background(), "set-global", ValueExternRef(secondRef)); err != nil {
				t.Fatalf("set tenant-two global: %v", err)
			}
			got, err := second.Call(context.Background(), "get-global")
			if err != nil || len(got) != 1 || got[0].ExternRef() != secondRef {
				t.Fatalf("tenant-two global round trip = %v, %v; want %v", got, err, secondRef)
			}
		})

		t.Run(policy.String()+"-imported-state", func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			mod, err := rt.Compile(importedWasm)
			if err != nil {
				t.Fatalf("Compile imported reference class: %v", err)
			}
			_, err = rt.Class(mod, ClassOptions{Pool: PoolOptions{MaxInstances: 1, Reset: policy}})
			if err == nil || !strings.Contains(err.Error(), "reference") {
				t.Fatalf("Class imported reference state = %v, want explicit rejection", err)
			}
		})
	}
}

func TestSnapshotProductsRejectCodecV20ReferenceState(t *testing.T) {
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
			if _, err := Pool(inMemory, SnapshotPoolOptions{MaxInstances: 1}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Pool(in-memory forged snapshot) = %v, want %q rejection", err, tc.want)
			}
			if _, err := Instantiate(inMemory); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Instantiate(in-memory forged snapshot) = %v, want %q rejection", err, tc.want)
			}

			blob := rawSnapshotBlobForCompiled(t, c)
			if _, err := LoadSnapshot(blob); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadSnapshot(codec-v20 %s module) = %v, want %q rejection", tc.name, err, tc.want)
			}
		})
	}
}

func rawSnapshotBlobForCompiled(t *testing.T, c *Compiled) []byte {
	t.Helper()
	compiled, err := c.MarshalBinary()
	if err != nil {
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
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(watToWasm(t, `(module
		(import "env" "shared" (table 1 2 funcref))
		(import "env" "shared" (table 1 2 funcref))
		(import "env" "g" (global (mut externref)))
		(table 3 externref)
		(global (export "fref") funcref (ref.null func))
		(func (export "refs") (param funcref externref) (result externref funcref)
			(local.get 1) (local.get 0))
		(export "first" (table 0))
		(export "second" (table 1))
		(export "local" (table 2)))`))
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
		},
	}
	if got := inspectModuleMetadata(t, mod.Metadata()); !reflect.DeepEqual(got, want) {
		t.Fatalf("compiled ModuleMetadata = %#v, want %#v", got, want)
	}

	blob, err := mod.Compiled().MarshalBinary()
	if err != nil {
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
