package wago

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestCompiledExecutionSnapshot(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	first, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	params, results, err := c.Signature("f")
	if err != nil {
		t.Fatal(err)
	}
	params[0], results[0] = ValI64, ValI64
	c.Funcs[0].Params[0] = ValF64
	c.Funcs[0].Results[0] = ValF64
	c.Entry = nil
	c.InternalEntry = nil
	c.NumImports = 5
	delete(c.Exports, "f")
	second, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for _, in := range []*Instance{first, second} {
		if len(in.c.Entry) != 1 || in.c.Funcs[0].Params[0] != ValI32 || in.c.NumImports != 0 {
			t.Fatal("execution metadata changed")
		}
		out, err := in.Invoke("f", 41)
		if err != nil || len(out) != 1 || out[0] != 42 {
			t.Fatalf("Invoke = %v, %v", out, err)
		}
	}
}

func TestCompiledArtifactUsesExecutionSnapshot(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		name := "compiler"
		if loaded {
			name = "loader"
		}
		t.Run(name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), benchAddOneModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			want, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if loaded {
				c = &Compiled{}
				if err := c.UnmarshalBinary(want); err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				want, err = c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
			}
			sizes, err := c.ArtifactSectionSizes()
			if err != nil {
				t.Fatal(err)
			}
			for _, invalid := range []bool{false, true} {
				// Both valid edits and malformed public metadata must leave the
				// published artifact and its validation bound to the snapshot.
				delete(c.Exports, "f")
				c.Exports["renamed"] = 0
				if invalid {
					c.Funcs[0].Params[0] = ValF64
					c.Globals = []GlobalDef{{Type: ValI32, Bits: I32(7)}}
					c.Entry = nil
					c.InternalEntry = []int{len(c.code)}
					c.NumImports = 99
				}
				name := "valid public edits"
				if invalid {
					name = "invalid public edits"
				}
				t.Run(name, func(t *testing.T) {
					t.Run("MarshalBinary", func(t *testing.T) {
						got, err := c.MarshalBinary()
						if err != nil || !bytes.Equal(got, want) {
							t.Fatalf("artifact changed after public edits: %v", err)
						}
					})
					t.Run("WriteTo", func(t *testing.T) {
						var dst bytes.Buffer
						n, err := c.WriteTo(&dst)
						if err != nil || n != int64(len(want)) || !bytes.Equal(dst.Bytes(), want) {
							t.Fatalf("stream changed after public edits: %d bytes, %v", n, err)
						}
					})
					t.Run("ArtifactSectionSizes", func(t *testing.T) {
						got, err := c.ArtifactSectionSizes()
						if err != nil || got != sizes {
							t.Fatalf("sizes changed after public edits: %+v, %v; want %+v", got, err, sizes)
						}
					})
				})
			}
		})
	}
}

func TestCompiledReflectionUsesExecutionSnapshot(t *testing.T) {
	for _, source := range []string{"compiler", "loader", "hand-built"} {
		t.Run(source, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), benchAddOneModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if source == "loader" {
				blob, err := c.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				c = &Compiled{}
				if err := c.UnmarshalBinary(blob); err != nil {
					t.Fatal(err)
				}
				defer c.Close()
			} else if source == "hand-built" {
				c = mutableCompiledFixture(c)
				c.Globals = []GlobalDef{{Type: ValI32, InitExpr: []byte{0x41, 7, 0x0b}}}
				c.GlobalExports = map[string]int{"g": 0}
				c.Names = &wasm.NameSec{FunctionNames: wasm.NameMap{{Name: "named"}}}
				c.freezeExecution()
			}
			read := func() []any {
				params, results, err := c.SignatureDescriptor("f")
				if err != nil {
					t.Fatal(err)
				}
				name, named := c.FuncName(0)
				local, localNamed := c.LocalFuncName(0)
				global, exported := c.ExportedGlobal("g")
				return []any{params, results, c.TypeDefinitions(), c.ExportedFunctions(), c.ExportedGlobals(),
					c.ImportedGlobalCount(), c.LocalGlobalCount(), name, named, local, localNamed,
					c.FuncDebugName(0), global, exported, c.GCNativeRootAdmission()}
			}
			want := read()
			// Returned descriptors and initializer bytes must also be owned copies.
			params, _, err := c.SignatureDescriptor("f")
			if err != nil {
				t.Fatal(err)
			}
			params[0].Kind = ValueTypeF64
			types := c.TypeDefinitions()
			types[0].Params[0].Kind = ValueTypeF64
			if global, ok := c.ExportedGlobal("g"); ok {
				global.InitExpr[1] = 9
			}
			c.Exports, c.Funcs, c.Types, c.Names = nil, nil, nil, nil
			c.Globals, c.GlobalExports = nil, nil
			c.GlobalImports = []GlobalImportDef{{}}
			c.NumImports = 99
			if got := read(); !reflect.DeepEqual(got, want) {
				t.Fatalf("reflection changed after public edits:\n got %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestCompiledSnapshotOwnsNestedPublicMetadata(t *testing.T) {
	name := "module"
	c := &Compiled{
		Entry: []int{1}, InternalEntry: []int{2}, Funcs: []FuncSig{{Params: []ValType{ValI32}, Results: []ValType{ValI64}}},
		Types:   []DefinedTypeDescriptor{{Supers: []uint32{1}, Params: []ValueTypeDescriptor{{Kind: ValueTypeI32}}, Fields: []FieldTypeDescriptor{{}}}},
		Imports: []string{"env.f"}, Exports: map[string]int{"f": 0},
		Names:   &wasm.NameSec{ModuleName: &name, FunctionNames: wasm.NameMap{{Name: "f"}}, LocalNames: wasm.IndirectNameMap{{Names: wasm.NameMap{{Name: "p"}}}}},
		Globals: []GlobalDef{{InitExpr: []byte{1}}}, GlobalExports: map[string]int{"g": 0},
		Elems: []ElemInit{{Offset: OffsetInit{Expr: []byte{1}}, Values: []RefInit{{Expr: []byte{1}}}}},
		Data:  []DataInit{{Offset: OffsetInit{Expr: []byte{1}}, Bytes: []byte{1}}}, PassiveData: []PassiveDataInit{{Bytes: []byte{1}}},
		GCTypeDescs: []gc.TypeDesc{{Fields: []gc.FieldDesc{{}}}},
	}
	snapshot, want := cloneCompiledMetadata(c), cloneCompiledMetadata(c)
	var change func(reflect.Value)
	change = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					change(v.Field(i))
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				change(v.Index(i))
			}
		case reflect.Pointer:
			if !v.IsNil() {
				change(v.Elem())
			}
		case reflect.Map:
			v.Clear()
		default:
			if v.CanSet() {
				v.Set(reflect.Zero(v.Type()))
			}
		}
	}
	change(reflect.ValueOf(c).Elem())
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatal("nested public metadata aliases execution snapshot")
	}
}

// Some low-level fixtures deliberately attach synthetic GC/type metadata to
// numeric machine code. Give them a separate unpublished metadata owner.
func mutableCompiledFixture(c *Compiled) *Compiled {
	out := cloneCompiledMetadata(c)
	out.validateMemo = nil
	return out
}

func TestModuleCompiledMetadataDoesNotChangeExecution(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	view := mod.Compiled()
	view.Exports = nil
	view.Entry = nil
	if mod.c.Exports["f"] != 0 || len(mod.c.Entry) != 1 {
		t.Fatal("Module.Compiled exposed execution metadata")
	}
}

func BenchmarkCompiledSnapshotCopy(b *testing.B) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchCompiledSink = cloneCompiledMetadata(c)
	}
}
