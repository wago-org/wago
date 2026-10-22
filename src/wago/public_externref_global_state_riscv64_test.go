//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type rv64StateOnlyMixedGlobal struct {
	typ    wasm.ValType
	bits   uint64
	parent int
}

func rv64StateOnlyMixedExternrefSource(globals []rv64StateOnlyMixedGlobal, exports ...int) []byte {
	entries := make([][]byte, len(globals))
	for i, g := range globals {
		var init []byte
		switch {
		case wasm.EqualValType(g.typ, wasm.I32):
			init = append([]byte{0x41}, wasmtest.SLEB32(int32(g.bits))...)
		case wasm.EqualValType(g.typ, wasm.I64):
			init = append([]byte{0x42}, wasmtest.SLEB64(int64(g.bits))...)
		case wasm.EqualValType(g.typ, wasm.F32):
			init = make([]byte, 5)
			init[0] = 0x43
			binary.LittleEndian.PutUint32(init[1:], uint32(g.bits))
		case wasm.EqualValType(g.typ, wasm.F64):
			init = make([]byte, 9)
			init[0] = 0x44
			binary.LittleEndian.PutUint64(init[1:], g.bits)
		case wasm.EqualValType(g.typ, wasm.ExternRef) && g.parent < 0:
			init = []byte{0xd0, 0x6f}
		case wasm.EqualValType(g.typ, wasm.ExternRef):
			init = append([]byte{0x23}, wasmtest.ULEB(uint32(g.parent))...)
		default:
			panic("unsupported mixed-global test type")
		}
		entries[i] = wasmtest.GlobalEntry(g.typ, false, append(init, 0x0b))
	}
	sections := [][]byte{wasmtest.Section(6, wasmtest.Vec(entries...))}
	if len(exports) != 0 {
		out := make([][]byte, len(exports))
		for i, index := range exports {
			out[i] = wasmtest.ExportEntry(fmt.Sprintf("g%d", index), byte(wasm.ExternGlobal), uint32(index))
		}
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(out...)))
	}
	return wasmtest.Module(sections...)
}

func rv64StateOnlyExternrefGlobalSource(export bool) []byte {
	sections := [][]byte{wasmtest.Section(6, wasmtest.Vec(
		wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}),
	))}
	if export {
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0))))
	}
	return wasmtest.Module(sections...)
}

func rv64StateOnlyExternrefGlobalDirectorySource(count int, exports ...int) []byte {
	globals := make([][]byte, count)
	for i := range globals {
		globals[i] = wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b})
	}
	sections := [][]byte{wasmtest.Section(6, wasmtest.Vec(globals...))}
	if len(exports) != 0 {
		entries := make([][]byte, len(exports))
		for i, index := range exports {
			entries[i] = wasmtest.ExportEntry(fmt.Sprintf("g%d", index), byte(wasm.ExternGlobal), uint32(index))
		}
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(entries...)))
	}
	return wasmtest.Module(sections...)
}

func rv64StateOnlyExternrefGlobalChainSource(parents []int, exports ...int) []byte {
	globals := make([][]byte, len(parents))
	for i, parent := range parents {
		init := []byte{0xd0, 0x6f, 0x0b}
		if parent >= 0 {
			init = append([]byte{0x23}, wasmtest.ULEB(uint32(parent))...)
			init = append(init, 0x0b)
		}
		globals[i] = wasmtest.GlobalEntry(wasm.ExternRef, false, init)
	}
	sections := [][]byte{wasmtest.Section(6, wasmtest.Vec(globals...))}
	if len(exports) != 0 {
		entries := make([][]byte, len(exports))
		for i, index := range exports {
			entries[i] = wasmtest.ExportEntry(fmt.Sprintf("g%d", index), byte(wasm.ExternGlobal), uint32(index))
		}
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(entries...)))
	}
	return wasmtest.Module(sections...)
}

func TestRV64PublicStateOnlyExternrefGlobalChain(t *testing.T) {
	maximum := make([]int, 256)
	maximum[0] = -1
	for i := 1; i < len(maximum); i++ {
		maximum[i] = i - 1
	}
	for _, tc := range []struct {
		name    string
		parents []int
		exports []int
	}{
		{name: "two", parents: []int{-1, 0}, exports: []int{1}},
		{name: "roots-and-repeats", parents: []int{-1, -1, 0, 0, 2}, exports: []int{0, 1, 4}},
		{name: "maximum", parents: maximum, exports: []int{0, 127, 255}},
	} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", tc.name, artifact), func(t *testing.T) {
				name := "state-only-externref-chain-" + tc.name
				c := rv64TableStageCompile(t, name, rv64StateOnlyExternrefGlobalChainSource(tc.parents, tc.exports...), artifact)
				wantFeatures := CoreFeatureReferenceTypes | CoreFeatureExtendedConstExpressions
				if !rv64StateOnlyExternrefGlobals(c) || c.requiredFeatures != wantFeatures || compiledStructuralRequiredFeatures(c) != wantFeatures || len(c.Globals) != len(tc.parents) {
					t.Fatal("externref chain metadata contract")
				}
				for i, parent := range tc.parents {
					g := c.Globals[i]
					if (parent >= 0) != g.HasInitGlobal || parent >= 0 && g.InitGlobal != parent || parent < 0 && g.InitGlobal != 0 {
						t.Fatal("externref chain initializer", i, parent, g)
					}
				}
				rv64CheckStateOnlyCode(t, c, 0)
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = in.Close() })
				for i, cell := range in.globalCells {
					if cell == nil || len(cell.cell) != 8 || i > 0 && cell == in.globalCells[i-1] {
						t.Fatal("externref chain cell", i)
					}
				}
				for _, index := range tc.exports {
					got, err := in.GlobalValue(fmt.Sprintf("g%d", index))
					if err != nil || got.Type() != ValExternRef || !got.ExternRef().IsNull() {
						t.Fatal("externref chain value", index, got, err)
					}
				}
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				rv64CheckStateOnlyCode(t, c, 0)
			})
		}
	}
}

func rv64StateOnlyMixedExternrefFixture() []rv64StateOnlyMixedGlobal {
	return []rv64StateOnlyMixedGlobal{
		{typ: wasm.I32, bits: 0xffffffff},
		{typ: wasm.ExternRef, parent: -1},
		{typ: wasm.F32, bits: 0x80000000},
		{typ: wasm.ExternRef, parent: 1},
		{typ: wasm.I64, bits: 0x8000000000000001},
		{typ: wasm.F64, bits: 0x7ff8000000000042},
		{typ: wasm.ExternRef, parent: -1},
		{typ: wasm.ExternRef, parent: 3},
	}
}

func TestRV64PublicStateOnlyMixedExternrefGlobals(t *testing.T) {
	globals := rv64StateOnlyMixedExternrefFixture()
	exports := make([]int, len(globals))
	for i := range exports {
		exports[i] = i
	}
	wantTypes := []ValType{ValI32, ValExternRef, ValF32, ValExternRef, ValI64, ValF64, ValExternRef, ValExternRef}
	wantBits := map[int]uint64{0: 0xffffffff, 2: 0x80000000, 4: 0x8000000000000001, 5: 0x7ff8000000000042}
	wantParents := map[int]int{3: 1, 7: 3}
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-mixed-externref-globals", rv64StateOnlyMixedExternrefSource(globals, exports...), artifact)
			wantFeatures := CoreFeatureReferenceTypes | CoreFeatureExtendedConstExpressions
			if !rv64StateOnlyExternrefGlobals(c) || c.requiredFeatures != wantFeatures || compiledStructuralRequiredFeatures(c) != wantFeatures || len(c.Globals) != len(globals) || len(c.ValueTypes) != 5 {
				t.Fatal("mixed state-only metadata contract")
			}
			for i, g := range c.Globals {
				plain, _ := valueTypeDescriptorFromValType(wantTypes[i])
				exact, err := c.globalExactType(i)
				if err != nil || exact != plain || g.Type != wantTypes[i] || g.Mutable || g.V128 != (V128{}) || g.HasInitFunc || len(g.InitExpr) != 0 {
					t.Fatal("mixed state-only global type", i, g, exact, err)
				}
				if parent, ok := wantParents[i]; ok {
					if !g.HasInitGlobal || g.InitGlobal != parent || g.Bits != 0 {
						t.Fatal("mixed state-only externref chain", i, g)
					}
				} else if g.HasInitGlobal || g.InitGlobal != 0 || g.Bits != wantBits[i] {
					t.Fatal("mixed state-only literal", i, g)
				}
			}
			rv64CheckStateOnlyCode(t, c, 0)
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			for i := range in.globalCells {
				if in.globalCells[i] == nil || len(in.globalCells[i].cell) != 8 {
					t.Fatal("mixed state-only cell", i)
				}
				for j := 0; j < i; j++ {
					if in.globalCells[i] == in.globalCells[j] {
						t.Fatal("mixed state-only aliased cells", i, j)
					}
				}
			}
			for i, bits := range wantBits {
				got, err := in.Global(fmt.Sprintf("g%d", i))
				if err != nil || got != bits {
					t.Fatal("mixed scalar value", i, got, err)
				}
			}
			for _, i := range []int{1, 3, 6, 7} {
				got, err := in.GlobalValue(fmt.Sprintf("g%d", i))
				if err != nil || got.Type() != ValExternRef || !got.ExternRef().IsNull() {
					t.Fatal("mixed externref value", i, got, err)
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Global("g5"); err != nil || got != wantBits[5] {
				t.Fatal("mixed scalar did not outlive compiled owner", got, err)
			}
			if got, err := in.GlobalValue("g7"); err != nil || !got.ExternRef().IsNull() {
				t.Fatal("mixed externref did not outlive compiled owner", got, err)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			rv64CheckStateOnlyCode(t, c, 0)
		})
	}
}

func TestRV64PublicStateOnlyMixedExternrefRequiresExternref(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler unavailable in precompiled runtime")
	}
	c, err := Compile(nil, rv64StateOnlyMixedExternrefSource([]rv64StateOnlyMixedGlobal{{typ: wasm.I64, bits: 7}}, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if rv64StateOnlyExternrefGlobals(c) {
		t.Fatal("pure scalar artifact misclassified as the externref profile")
	}
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Global("g0"); err != nil || got != 7 {
		t.Fatal("ordinary pure scalar state changed", got, err)
	}
}

func TestRV64PublicStateOnlyMixedExternrefTypePoolGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-mixed-externref-pool-admission", rv64StateOnlyMixedExternrefSource([]rv64StateOnlyMixedGlobal{
		{typ: wasm.I32, bits: 7},
		{typ: wasm.ExternRef, parent: -1},
	}, 0, 1), true)
	defer c.Close()
	if len(c.ValueTypes) != 2 || !rv64StateOnlyExternrefGlobals(c) {
		t.Fatal("small mixed type-pool baseline")
	}
	for name, exact := range map[string]ValueTypeDescriptor{
		"unused":    {Kind: ValueTypeF64},
		"duplicate": c.ValueTypes[c.Globals[0].ValueTypeIndex],
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Globals = append([]GlobalDef(nil), bad.Globals...)
			bad.ValueTypes = append(append([]ValueTypeDescriptor(nil), bad.ValueTypes...), exact)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			if len(bad.ValueTypes) > 5 || rv64StateOnlyExternrefGlobals(&bad) {
				t.Fatal("hostile in-bound mixed type pool admitted")
			}
			if err := bad.validate(); err == nil {
				t.Fatal("hostile in-bound mixed type pool passed railshot validation")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return // The writer can reject the inconsistent contract before decode.
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile in-bound mixed type pool artifact admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyMixedExternrefArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-mixed-externref-admission", rv64StateOnlyMixedExternrefSource(rv64StateOnlyMixedExternrefFixture(), 0, 1, 7), true)
	defer c.Close()
	for name, mutate := range map[string]func(*Compiled){
		"mutable-scalar":            func(v *Compiled) { v.Globals[0].Mutable = true },
		"scalar-chain":              func(v *Compiled) { v.Globals[2].HasInitGlobal = true; v.Globals[2].InitGlobal = 0 },
		"scalar-expression":         func(v *Compiled) { v.Globals[2].InitExpr = []byte{0x41, 0, 0x0b} },
		"i32-upper-bits":            func(v *Compiled) { v.Globals[0].Bits |= 1 << 32 },
		"f32-upper-bits":            func(v *Compiled) { v.Globals[2].Bits |= 1 << 32 },
		"extern-live-bits":          func(v *Compiled) { v.Globals[1].Bits = 1 },
		"extern-self":               func(v *Compiled) { v.Globals[1].HasInitGlobal = true; v.Globals[1].InitGlobal = 1 },
		"extern-scalar-parent":      func(v *Compiled) { v.Globals[3].InitGlobal = 0 },
		"extern-scalar-binding":     func(v *Compiled) { v.Globals[1].ValueTypeIndex = v.Globals[0].ValueTypeIndex },
		"scalar-extern-binding":     func(v *Compiled) { v.Globals[0].ValueTypeIndex = v.Globals[1].ValueTypeIndex },
		"missing-reference-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"missing-extended-feature":  func(v *Compiled) { v.requiredFeatures &^= CoreFeatureExtendedConstExpressions },
		"extra-feature":             func(v *Compiled) { v.requiredFeatures |= CoreFeatureSIMD },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Globals = append([]GlobalDef(nil), bad.Globals...)
			bad.ValueTypes = append([]ValueTypeDescriptor(nil), bad.ValueTypes...)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if rv64StateOnlyExternrefGlobals(&bad) {
				t.Fatal("hostile mixed exact profile admitted")
			}
			if err := bad.validate(); err == nil {
				t.Fatal("hostile mixed state-only metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "missing-reference-feature" || name == "missing-extended-feature" {
				if err != nil || loaded == nil || loaded.requiredFeatures != c.requiredFeatures || !rv64StateOnlyExternrefGlobals(loaded) {
					t.Fatal("mixed state-only feature metadata was not reconstructed", loaded, err)
				}
				_ = loaded.Close()
				return
			}
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile mixed state-only artifact admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyExternrefGlobalDirectory(t *testing.T) {
	all := make([]int, 256)
	for i := range all {
		all[i] = i
	}
	for _, tc := range []struct {
		name    string
		count   int
		exports []int
	}{
		{name: "two-none", count: 2},
		{name: "two-partial", count: 2, exports: []int{1}},
		{name: "two-complete", count: 2, exports: []int{0, 1}},
		{name: "maximum-complete", count: 256, exports: all},
	} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", tc.name, artifact), func(t *testing.T) {
				name := "state-only-externref-directory-" + tc.name
				c := rv64TableStageCompile(t, name, rv64StateOnlyExternrefGlobalDirectorySource(tc.count, tc.exports...), artifact)
				if !rv64StateOnlyExternrefGlobals(c) || len(c.Globals) != tc.count || len(c.ValueTypes) != 1 || len(c.GlobalExports) != len(tc.exports) {
					t.Fatal("externref global directory metadata contract")
				}
				rv64CheckStateOnlyCode(t, c, 0)
				instances := make([]*Instance, 0, 2)
				for range 2 {
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = in.Close() })
					instances = append(instances, in)
					if len(in.globalCells) != tc.count {
						t.Fatal("externref global cell count", len(in.globalCells), tc.count)
					}
					for i, cell := range in.globalCells {
						if cell == nil || len(cell.cell) != 8 {
							t.Fatal("externref global cell shape", i)
						}
						if i > 0 && cell == in.globalCells[i-1] {
							t.Fatal("externref globals alias storage", i)
						}
					}
					for _, index := range tc.exports {
						export := fmt.Sprintf("g%d", index)
						got, err := in.GlobalValue(export)
						if err != nil || got.Type() != ValExternRef || !got.ExternRef().IsNull() {
							t.Fatal("externref directory value", export, got, err)
						}
						if err := in.SetGlobalValue(export, ValueExternRef(ExternRef{})); err == nil || !strings.Contains(err.Error(), "immutable") {
							t.Fatal("externref directory mutation", export, err)
						}
					}
				}
				rv64CheckStateOnlyCode(t, c, 2)
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				for _, in := range instances {
					if len(tc.exports) != 0 {
						got, err := in.GlobalValue(fmt.Sprintf("g%d", tc.exports[0]))
						if err != nil || !got.ExternRef().IsNull() {
							t.Fatal("externref directory did not outlive compiled handle", got, err)
						}
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
				rv64CheckStateOnlyCode(t, c, 0)
			})
		}
	}
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") != "load" {
		rv64RequireCore3SourceValidation(t, rv64StateOnlyExternrefGlobalDirectorySource(257))
		exports := make([][]byte, 257)
		for i := range exports {
			exports[i] = wasmtest.ExportEntry(fmt.Sprintf("e%d", i), byte(wasm.ExternGlobal), 0)
		}
		source := wasmtest.Module(
			wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}))),
			wasmtest.Section(7, wasmtest.Vec(exports...)),
		)
		rv64RequireCore3SourceValidation(t, source)
	}
}

func TestRV64PublicStateOnlyExternrefGlobalDirectoryArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-externref-directory-admission", rv64StateOnlyExternrefGlobalDirectorySource(2, 0, 1), true)
	defer c.Close()
	for name, mutate := range map[string]func(*Compiled){
		"second-type":        func(v *Compiled) { v.Globals[1].Type = ValFuncRef },
		"second-mutable":     func(v *Compiled) { v.Globals[1].Mutable = true },
		"second-bits":        func(v *Compiled) { v.Globals[1].Bits = 1 },
		"second-init-global": func(v *Compiled) { v.Globals[1].HasInitGlobal = true; v.Globals[1].InitGlobal = 1 },
		"second-init-func":   func(v *Compiled) { v.Globals[1].HasInitFunc = true },
		"second-value-index": func(v *Compiled) { v.Globals[1].ValueTypeIndex = 1 },
		"export-range":       func(v *Compiled) { v.GlobalExports["g1"] = 2 },
		"export-257": func(v *Compiled) {
			v.GlobalExports = make(map[string]int, 257)
			for i := range 257 {
				v.GlobalExports[fmt.Sprintf("e%d", i)] = 0
			}
		},
		"global-257": func(v *Compiled) {
			for len(v.Globals) < 257 {
				v.Globals = append(v.Globals, v.Globals[0])
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Globals = append([]GlobalDef(nil), bad.Globals...)
			bad.ValueTypes = append([]ValueTypeDescriptor(nil), bad.ValueTypes...)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("hostile externref directory admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile externref directory artifact admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyExternrefGlobalSource(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-externref-global", rv64StateOnlyExternrefGlobalSource(true), artifact)
			if !rv64StateOnlyExternrefGlobals(c) || c.requiredFeatures != CoreFeatureReferenceTypes || compiledStructuralRequiredFeatures(c) != CoreFeatureReferenceTypes {
				t.Fatal("externref state-only metadata contract")
			}
			rv64CheckStateOnlyCode(t, c, 0)
			if len(c.Globals) != 1 || c.Globals[0].Type != ValExternRef || c.Globals[0].Mutable || c.Globals[0].Bits != 0 || len(c.ExportedGlobals()) != 1 || c.ExportedGlobals()[0] != "g" {
				t.Fatalf("externref global metadata = %#v exports=%v", c.Globals, c.ExportedGlobals())
			}

			instances := make([]*Instance, 0, 2)
			objects := make([]*Global, 0, 2)
			for range 2 {
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = in.Close() })
				instances = append(instances, in)
				got, err := in.GlobalValue("g")
				if err != nil || got.Type() != ValExternRef || !got.ExternRef().IsNull() {
					t.Fatalf("GlobalValue(g) = %v, %v; want null externref", got, err)
				}
				if err := in.SetGlobalValue("g", ValueExternRef(ExternRef{})); err == nil || !strings.Contains(err.Error(), "immutable") {
					t.Fatalf("immutable externref write error = %v", err)
				}
				object, err := in.ExportedGlobalObject("g")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = object.Close() })
				objects = append(objects, object)
				if value, err := object.GetValue(); err != nil || value.Type() != ValExternRef || !value.ExternRef().IsNull() {
					t.Fatalf("global object value = %v, %v", value, err)
				}
			}
			if objects[0] == objects[1] {
				t.Fatal("externref global storage shared between instances")
			}
			rv64CheckStateOnlyCode(t, c, 2)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			for i := range instances {
				if value, err := objects[i].GetValue(); err != nil || !value.ExternRef().IsNull() {
					t.Fatalf("global did not outlive compiled handle: %v, %v", value, err)
				}
				if err := objects[i].Close(); err != nil {
					t.Fatal(err)
				}
				if err := objects[i].Close(); err != nil {
					t.Fatal("repeated global close", err)
				}
				if err := instances[i].Close(); err != nil {
					t.Fatal(err)
				}
			}
			rv64CheckStateOnlyCode(t, c, 0)
		})
	}
}

func TestRV64PublicStateOnlyExternrefGlobalSourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler unavailable in precompiled runtime")
	}
	global := func(typ wasm.ValType, mutable bool, init []byte) []byte {
		return wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(typ, mutable, init)))
	}
	v128Init := append([]byte{0xfd, 0x0c}, make([]byte, 16)...)
	v128Init = append(v128Init, 0x0b)
	for name, source := range map[string][]byte{
		"mutable":         wasmtest.Module(global(wasm.ExternRef, true, []byte{0xd0, 0x6f, 0x0b})),
		"wrong-heap":      wasmtest.Module(global(wasm.ExternRef, false, []byte{0xd0, 0x70, 0x0b})),
		"indexed-type":    wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 0, 0xd0, 0, 0x0b}))),
		"imported":        wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "g", wasm.ExternRef, false)))),
		"table":           wasmtest.Module(global(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}), wasmtest.Section(4, []byte{1, 0x70, 0, 0})),
		"function":        wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), global(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b})))),
		"initializer-ref": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0x23, 0, 0x0b})))),
		"chain-self": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0x23, 0, 0x0b}),
		))),
		"chain-forward": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0x23, 1, 0x0b}),
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}),
		))),
		"chain-mutable": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.ExternRef, true, []byte{0xd0, 0x6f, 0x0b}),
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0x23, 0, 0x0b}),
		))),
		"chain-wrong-type": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0, 0x0b}),
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0x23, 0, 0x0b}),
		))),
		"chain-trailing": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}),
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0x23, 0, 0xd1, 0x0b}),
		))),
		"mixed-mutable-scalar": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}),
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}),
		))),
		"mixed-scalar-chain": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0, 0x0b}),
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}),
		))),
		"mixed-vector": wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.V128, false, v128Init),
			wasmtest.GlobalEntry(wasm.ExternRef, false, []byte{0xd0, 0x6f, 0x0b}),
		))),
	} {
		t.Run(name, func(t *testing.T) {
			rv64RequireCore3SourceValidation(t, source)
		})
	}
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false)
	if c, err := Compile(cfg, rv64StateOnlyExternrefGlobalSource(true)); c != nil || err == nil || !strings.Contains(err.Error(), "reference-types disabled") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("disabled reference-types admitted", err)
	}
	cfg = NewRuntimeConfig().WithFeature(CoreFeatureExtendedConstExpressions, false)
	if c, err := Compile(cfg, rv64StateOnlyExternrefGlobalChainSource([]int{-1, 0}, 1)); c != nil || err == nil || !strings.Contains(err.Error(), "extended-const-expressions disabled") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("disabled extended-const chain admitted", err)
	}
}

func TestRV64PublicStateOnlyExternrefGlobalChainArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-externref-chain-admission", rv64StateOnlyExternrefGlobalChainSource([]int{-1, 0, 1}, 2), true)
	defer c.Close()
	for name, mutate := range map[string]func(*Compiled){
		"root-self":              func(v *Compiled) { v.Globals[0].HasInitGlobal = true },
		"second-self":            func(v *Compiled) { v.Globals[1].InitGlobal = 1 },
		"second-forward":         func(v *Compiled) { v.Globals[1].InitGlobal = 2 },
		"predecessor-mutable":    func(v *Compiled) { v.Globals[0].Mutable = true },
		"predecessor-type":       func(v *Compiled) { v.Globals[0].Type = ValFuncRef },
		"conflicting-function":   func(v *Compiled) { v.Globals[1].HasInitFunc = true },
		"conflicting-expression": func(v *Compiled) { v.Globals[1].InitExpr = []byte{0x23, 0, 0x0b} },
		"nonnull-payload":        func(v *Compiled) { v.Globals[1].Bits = 1 },
		"inactive-index":         func(v *Compiled) { v.Globals[1].HasInitGlobal = false; v.Globals[1].InitGlobal = 1 },
		"missing-feature":        func(v *Compiled) { v.requiredFeatures &^= CoreFeatureExtendedConstExpressions },
		"extra-feature":          func(v *Compiled) { v.requiredFeatures |= CoreFeatureSIMD },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Globals = append([]GlobalDef(nil), bad.Globals...)
			bad.ValueTypes = append([]ValueTypeDescriptor(nil), bad.ValueTypes...)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("hostile externref initializer chain admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "missing-feature" || name == "conflicting-function" || name == "conflicting-expression" || name == "nonnull-payload" {
				want := CoreFeatureReferenceTypes | CoreFeatureExtendedConstExpressions
				if err != nil || loaded == nil || loaded.requiredFeatures != want || !rv64StateOnlyExternrefGlobals(loaded) {
					t.Fatal("externref initializer feature was not reconstructed", loaded, err)
				}
				_ = loaded.Close()
				return
			}
			if name == "inactive-index" {
				want := CoreFeatureReferenceTypes | CoreFeatureExtendedConstExpressions
				if err != nil || loaded == nil || loaded.requiredFeatures != want || loaded.Globals[1].HasInitGlobal || loaded.Globals[1].InitGlobal != 0 || !rv64StateOnlyExternrefGlobals(loaded) {
					t.Fatal("inactive externref initializer metadata was not cleared", loaded, err)
				}
				_ = loaded.Close()
				return
			}
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile externref initializer artifact admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyExternrefGlobalArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-externref-global-admission", rv64StateOnlyExternrefGlobalSource(true), true)
	defer c.Close()
	for name, mutate := range map[string]func(*Compiled){
		"wrong-type":         func(v *Compiled) { v.Globals[0].Type = ValFuncRef },
		"missing-value-type": func(v *Compiled) { v.Globals[0].HasValueType = false },
		"value-type-index":   func(v *Compiled) { v.Globals[0].ValueTypeIndex = 1 },
		"extra-value-type":   func(v *Compiled) { v.ValueTypes = append(v.ValueTypes, ValueTypeDescriptor{Kind: ValueTypeI32}) },
		"mutable":            func(v *Compiled) { v.Globals[0].Mutable = true },
		"nonnull-bits":       func(v *Compiled) { v.Globals[0].Bits = 1 },
		"vector-payload":     func(v *Compiled) { v.Globals[0].V128[0] = 1 },
		"init-global":        func(v *Compiled) { v.Globals[0].HasInitGlobal = true },
		"inactive-global":    func(v *Compiled) { v.Globals[0].InitGlobal = 1 },
		"init-function":      func(v *Compiled) { v.Globals[0].HasInitFunc = true },
		"inactive-function":  func(v *Compiled) { v.Globals[0].InitFunc = 1 },
		"init-expression":    func(v *Compiled) { v.Globals[0].InitExpr = []byte{0xd0, 0x6f, 0x0b} },
		"missing-feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"extra-feature":      func(v *Compiled) { v.requiredFeatures |= CoreFeatureSIMD },
		"function-entry":     func(v *Compiled) { v.Funcs = []FuncSig{{}} },
		"code-entry":         func(v *Compiled) { v.Entry = []int{0} },
		"global-import":      func(v *Compiled) { v.GlobalImports = []GlobalImportDef{{Type: ValExternRef}} },
		"wrong-export":       func(v *Compiled) { v.GlobalExports["g"] = 1 },
		"nonnullable-type":   func(v *Compiled) { v.ValueTypes[0].Ref.Nullable = false },
		"table":              func(v *Compiled) { v.HasTable, v.TableSize = true, 1 },
		"passive-data":       func(v *Compiled) { v.PassiveData = []PassiveDataInit{{Bytes: []byte{1}}} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := rv64ReferenceStartCopy(c.executionView())
			bad.Globals = append([]GlobalDef(nil), bad.Globals...)
			bad.GlobalImports = append([]GlobalImportDef(nil), bad.GlobalImports...)
			bad.ValueTypes = append([]ValueTypeDescriptor(nil), bad.ValueTypes...)
			bad.GlobalExports = maps.Clone(bad.GlobalExports)
			mutate(bad)
			inactive := name == "vector-payload" || name == "inactive-global" || name == "inactive-function"
			if err := bad.validate(); err == nil && !inactive {
				t.Fatal("hostile externref state-only artifact admitted")
			}
			blob, err := marshalCompiled(bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "missing-feature" || name == "vector-payload" || name == "inactive-global" || name == "inactive-function" {
				if err != nil || loaded == nil || loaded.requiredFeatures != CoreFeatureReferenceTypes {
					t.Fatal("canonical externref artifact metadata was not restored", loaded, err)
				}
				if !rv64StateOnlyExternrefGlobals(loaded) {
					t.Fatal("canonical externref artifact did not clear inactive metadata")
				}
				_ = loaded.Close()
				return
			}
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("hostile externref state-only artifact round-trip admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyExternrefGlobalCompilerFreeGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") != "load" {
		t.Skip("actual precompiled runtime only")
	}
	if c, err := Compile(nil, rv64StateOnlyExternrefGlobalSource(true)); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("compiler-free externref witness used source compilation", err)
	}
}
