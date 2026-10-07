//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceGlobalModule(vector, borrowed, imported, null bool, count int) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil), wasmtest.FuncType(nil, nil)))}
	var imports [][]byte
	if imported {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))
	}
	if borrowed {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 5))
	}
	if len(imports) != 0 {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	funcs := []byte{7, 0, 1, 2, 3, 4, 5, 6}
	bodies := [][]byte{wasmtest.Code([]byte{0x20, 0, 0x0b})}
	if imported {
		funcs = []byte{6, 1, 2, 3, 4, 5, 6}
		bodies = nil
	}
	sections = append(sections, wasmtest.Section(3, funcs))
	if !borrowed {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 2, 5}))
	}
	var globals [][]byte
	for i := 0; i < count; i++ {
		expr := []byte{0xd0, 0x70, 0x0b}
		if i == count-1 && !null {
			expr = []byte{0xd2, 0, 0x0b}
		}
		globals = append(globals, wasmtest.GlobalEntry(wasm.FuncRef, false, expr))
	}
	get := append([]byte{0x23}, wasmtest.ULEB(uint32(count-1))...)
	install := append(append([]byte{0x20, 0}, get...), 0x10, 6, 0x26, 0, 0x0b)
	isnull := append(slices.Clone(get), 0xd1, 0x0b)
	grow := append(slices.Clone(get), 0x20, 0, 0xfc, 15, 0, 0x0b)
	fill := append(append([]byte{0x20, 0}, get...), 0x20, 1, 0xfc, 17, 0, 0x0b)
	bodies = append(bodies, wasmtest.Code(install), wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}), wasmtest.Code(isnull), wasmtest.Code(grow), wasmtest.Code(fill), wasmtest.Code([]byte{1, 0x0b}))
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("install", 0, 1), wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("isnull", 0, 3), wasmtest.ExportEntry("grow", 0, 4), wasmtest.ExportEntry("fill", 0, 5), wasmtest.ExportEntry("table", 1, 0))), wasmtest.Section(10, wasmtest.Vec(bodies...)))
	return wasmtest.Module(sections...)
}

func TestRV64PublicReferenceGlobals(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, borrowed := range []bool{false, true} {
			for _, null := range []bool{false, true} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-borrowed%t-null%t-artifact%t", vector, borrowed, null, artifact), func(t *testing.T) {
						c := rv64TableStageCompile(t, fmt.Sprintf("ref-global-vector%t-borrowed%t-null%t", vector, borrowed, null), rv64ReferenceGlobalModule(vector, borrowed, false, null, 1), artifact)
						im := NewImports()
						if borrowed {
							table, err := NewTable(2, 5)
							if err != nil {
								t.Fatal(err)
							}
							defer table.Close()
							im.Table("env", "table", table)
						}
						in, err := Instantiate(c, im)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						c.Close()
						isnull := uint64(0)
						if null {
							isnull = 1
						}
						importedGlobalInvoke(t, in, "isnull", []uint64{isnull})
						if _, err := in.Invoke("install", 0xfeedface00000000); err != nil {
							t.Fatal("reference across call", err)
						}
						if _, err := in.Invoke("fill", 1, 1); err != nil {
							t.Fatal(err)
						}
						importedGlobalInvoke(t, in, "grow", []uint64{2}, 2)
						args, want := []uint64{0xfeedface00000007}, []uint64{7}
						if vector {
							args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
							want = args
						}
						runtime.GC()
						for index := uint64(0); index < 4; index++ {
							callArgs := append(slices.Clone(args), index)
							if null {
								_, err := in.Invoke("call", callArgs...)
								tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
							} else {
								importedGlobalInvoke(t, in, "call", want, callArgs...)
							}
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceGlobalRetiredWriter(t *testing.T) {
	testRV64PublicReferenceGlobalRetiredWriter(t, false)
}

func testRV64PublicReferenceGlobalRetiredWriter(t *testing.T, copies bool) {
	for _, kind := range []string{"local", "go", "native", "vector"} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", kind, artifact), func(t *testing.T) {
				vector, imported := kind == "vector", kind != "local"
				source := rv64ReferenceGlobalModule(vector, true, imported, false, 1)
				key := fmt.Sprintf("ref-global-writer-vector%t-imported%t", vector, imported)
				if copies {
					source = rv64ReferenceCopyModule(t, vector, true, imported, false, 8, false)
					key += "-copies"
				}
				c := rv64TableStageCompile(t, key, source, artifact)
				reader := rv64TableStageCompile(t, fmt.Sprintf("ref-global-reader-vector%t", vector), rv64ReferenceGlobalModule(vector, true, false, true, 1), artifact)
				table, err := NewTable(2, 5)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				im := NewImports().Table("env", "table", table)
				calls := 0
				var leaf *Instance
				var lc *Compiled
				if kind == "go" {
					im.Function("env", "host", slotHostFunc(func(_ HostModule, p, r []uint64) { calls++; runtime.GC(); r[0] = p[0] }))
				} else if imported {
					typ := wasm.I32
					if vector {
						typ = wasm.V128
					}
					lc = rv64TableStageCompile(t, fmt.Sprintf("ref-global-leaf-vector%t", vector), rv64PublicNativeIdentityModule([]wasm.ValType{typ}), artifact)
					leaf, err = Instantiate(lc)
					if err != nil {
						t.Fatal(err)
					}
					defer leaf.Close()
					ex, err := leaf.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					im.Function("env", "host", ex)
				}
				writer, err := Instantiate(c, im)
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
				consumer, err := Instantiate(reader, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				if leaf != nil {
					leaf.Close()
					lc.Close()
				}
				if _, err = writer.Invoke("install", 0); err != nil {
					t.Fatal(err)
				}
				if _, err = writer.Invoke("fill", 1, 1); err != nil {
					t.Fatal(err)
				}
				importedGlobalInvoke(t, writer, "grow", []uint64{2}, 2)
				writer.Close()
				c.Close()
				runtime.GC()
				if !writer.hasPhysicalResources() {
					t.Fatal("reference-global writer retired while table reachable")
				}
				args, want := []uint64{0xdeadbeef00000007}, []uint64{7}
				if vector {
					args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
					want = args
				}
				for index := uint64(0); index < 4; index++ {
					importedGlobalInvoke(t, consumer, "call", want, append(slices.Clone(args), index)...)
				}
				if kind == "go" && calls != 4 {
					t.Fatal("retired reference-global host route", calls)
				}
				if _, err = consumer.Invoke("fill", 0, 4); err != nil {
					t.Fatal(err)
				}
				if writer.hasPhysicalResources() || c.codeCache.mem != nil || leaf != nil && (leaf.hasPhysicalResources() || lc.codeCache.mem != nil) {
					t.Fatal("cleared reference-global writer/producer retained")
				}
			})
		}
	}
}

func TestRV64PublicReferenceGlobalIndex255(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("ref-global-index255-vector%t", vector), rv64ReferenceGlobalModule(vector, false, false, false, 256), artifact)
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if _, err := in.Invoke("install", 0); err != nil {
					t.Fatal(err)
				}
				if vector {
					importedGlobalInvoke(t, in, "call", []uint64{0x0123456789abcdef, 0xfedcba9876543210}, 0x0123456789abcdef, 0xfedcba9876543210, 0)
				} else {
					importedGlobalInvoke(t, in, "call", []uint64{7}, 0xfeedface00000007, 0)
				}
			})
		}
	}
}

func TestRV64PublicReferenceGlobalArtifactAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source metadata mutations")
	}
	c := rv64TableStageCompile(t, "ref-global-metadata", rv64ReferenceGlobalModule(false, false, false, true, 1), false)
	for index, mutate := range []func(*Compiled){
		func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		func(v *Compiled) { v.Globals[0].Mutable = true },
		func(v *Compiled) { v.Globals[0].Bits = 1 },
		func(v *Compiled) { v.Globals[0].HasInitGlobal = true; v.Globals[0].InitGlobal = 0 },
		func(v *Compiled) { v.Globals[0].InitExpr = []byte{0xd0, 0x70, 0x0b} },
		func(v *Compiled) { v.Globals[0].HasInitFunc = true; v.Globals[0].InitFunc = uint32(len(v.Funcs)) },
		func(v *Compiled) { v.GlobalExports = map[string]int{"reference": 1} },
		func(v *Compiled) { v.ValueTypes[v.Globals[0].ValueTypeIndex].Ref.Nullable = false },
		func(v *Compiled) { v.HasTable = false },
	} {
		v := *c.executionView()
		v.Globals = slices.Clone(v.Globals)
		v.ValueTypes = slices.Clone(v.ValueTypes)
		mutate(&v)
		if err := railshotValidateCompiled(&v); err == nil {
			t.Fatal("unqualified reference global metadata admitted", index)
		}
		blob, err := marshalCompiled(&v)
		if err != nil {
			continue
		}
		loaded, err := LoadTrustedArtifact(blob)
		if index == 0 && err == nil {
			repaired := loaded.executionView()
			if !repaired.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || railshotValidateCompiled(repaired) != nil {
				loaded.Close()
				t.Fatal("invalid reconstructed reference feature")
			}
			loaded.Close()
			continue
		}
		if loaded != nil {
			loaded.Close()
		}
		if err == nil {
			t.Fatal("unqualified reference global artifact admitted", index)
		}
	}
	if c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false), rv64ReferenceGlobalModule(false, false, false, true, 1)); c != nil || err == nil {
		if c != nil {
			c.Close()
		}
		t.Fatal("disabled reference feature admitted", err)
	}

}
