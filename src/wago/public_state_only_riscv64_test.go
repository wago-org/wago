//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64StateOnlyVector() (V128, []byte) {
	want := V128{1, 2, 3, 4, 5, 6, 7, 8, 0xff, 0xfe, 0xfd, 0xfc, 0x80, 0, 0, 1}
	expr := append([]byte{0xfd, 0x0c}, want[:]...)
	return want, append(expr, 0x0b)
}

func rv64CheckStateOnlyCode(t *testing.T, c *Compiled, refs int) {
	t.Helper()
	if !c.hasNoFunctionCode() || c.codeCache.mem != nil || c.codeCache.base != 0 || c.codeCache.sealed || c.codeCache.flags&compiledCacheWritableCode != 0 || c.codeCache.refs != refs {
		t.Fatalf("state-only code owner: mem=%d base=%#x sealed=%v refs=%d want=%d", len(c.codeCache.mem), c.codeCache.base, c.codeCache.sealed, c.codeCache.refs, refs)
	}
}

func rv64StateOnlyTypedDeclarationSource(vector bool, shape string, table bool) []byte {
	numeric := wasm.I32
	if vector {
		numeric = wasm.V128
	}
	ref := []byte{0x64, 0x70} // (ref func)
	if shape == "indexed" {
		ref = []byte{0x63, 0} // (ref null 0)
	}
	control := []byte{0x60, 1}
	control = append(control, ref...)
	control = append(control, 1)
	control = append(control, ref...)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{numeric}, []wasm.ValType{numeric}),
		control,
	))}
	if table {
		sections = append(sections,
			wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))),
		)
	}
	return wasmtest.Module(sections...)
}

func TestRV64PublicStateOnlyTypedDeclarations(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, shape := range []string{"nonnull", "indexed"} {
			for _, table := range []bool{false, true} {
				source := rv64StateOnlyTypedDeclarationSource(vector, shape, table)
				if c, err := compatibilityDefaultConfig().Compile(source); err == nil || c != nil {
					if c != nil {
						_ = c.Close()
					}
					t.Fatal("typed declaration bypassed feature gate", shape)
				}
				for _, artifact := range []bool{false, true} {
					name := fmt.Sprintf("state-only-typed-v%t-%s-table%t", vector, shape, table)
					t.Run(fmt.Sprintf("artifact%t-%s", artifact, name), func(t *testing.T) {
						c := rv64NativeTailCompile(t, name, source, artifact)
						if !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences|CoreFeatureReferenceTypes) || (!artifact && !c.stagedFeatures().IsEnabled(CoreFeatureTypedFunctionReferences)) || !compiledStructuralRequiredFeatures(c).IsEnabled(CoreFeatureTypedFunctionReferences) || c.nativeReferenceControlABI || c.nativeReferenceCallABI || railshotRequiresNativeOwnerScope(c) != table {
							t.Fatal("declarative typed metadata/owner contract")
						}
						if c.requiredFeatures.IsEnabled(CoreFeatureSIMD) != vector {
							t.Fatal("declarative typed profile")
						}
						if p, r, err := railshotImportedCallSlots(c, 3, 4); err != nil || p != 3 || r != 4 {
							t.Fatal("declarative directory contributed callable slots", p, r, err)
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
							if table {
								exported, err := in.ExportedTable("table")
								if err != nil {
									t.Fatal(err)
								}
								t.Cleanup(func() { _ = exported.Close() })
								if null, err := exported.EntryIsNull(0); err != nil || !null {
									t.Fatal("declarative typed table state", null, err)
								}
								if err := exported.Close(); err != nil {
									t.Fatal(err)
								}
							}
						}
						rv64CheckStateOnlyCode(t, c, 2)
						if err := c.Close(); err != nil {
							t.Fatal(err)
						}
						for _, in := range instances {
							if err := in.Close(); err != nil {
								t.Fatal(err)
							}
						}
						rv64CheckStateOnlyCode(t, c, 0)
					})
				}
			}
		}
	}
}

func TestRV64PublicStateOnlyTypedDeclarationAdmission(t *testing.T) {
	c := rv64NativeTailCompile(t, "state-only-typed-admission", rv64StateOnlyTypedDeclarationSource(false, "indexed", false), false)
	for name, mutate := range map[string]func(*Compiled){
		"missing-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTypedFunctionReferences },
		"native-marker":   func(v *Compiled) { v.nativeReferenceControlABI = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid declarative typed metadata admitted")
			}
		})
	}
	plain := rv64TableStageCompile(t, "state-only-typed-spurious", wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil)))), false)
	bad := *plain.executionView()
	bad.requiredFeatures |= CoreFeatureTypedFunctionReferences
	if err := bad.validate(); err == nil {
		t.Fatal("typed feature without exact declaration witness admitted")
	}
}

func TestRV64PublicStateOnlyModules(t *testing.T) {
	want, vector := rv64StateOnlyVector()
	for _, tc := range []struct {
		name   string
		source []byte
		check  func(*testing.T, *Instance)
	}{
		{name: "empty", source: wasmtest.Module()},
		{name: "scalar-types", source: wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil))))},
		{name: "vector-types", source: wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))))},
		{name: "null-table", source: wasmtest.Module(wasmtest.Section(4, []byte{1, 0x70, 1, 2, 4}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0)))), check: func(t *testing.T, in *Instance) {
			table, err := in.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			if table.Size() != 2 {
				t.Fatal("state-only table size", table.Size())
			}
			for i := uint64(0); i < 2; i++ {
				if null, err := table.EntryIsNull(i); err != nil || !null {
					t.Fatal("state-only table initializer", i, null, err)
				}
			}
			if _, err := table.EntryIsNull(2); err == nil {
				t.Fatal("state-only table bound ignored")
			}
		}},
		{name: "null-global", source: wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd0, 0x70, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))), check: func(t *testing.T, in *Instance) {
			if got, err := in.GlobalValue("g"); err != nil || got.Type() != ValFuncRef || got.Bits() != 0 {
				t.Fatal("state-only null global", got, err)
			}
		}},
		{name: "scalar-global", source: wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 7, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))), check: func(t *testing.T, in *Instance) {
			if got, err := in.Global("g"); err != nil || got != 7 {
				t.Fatal("state-only scalar initializer", got, err)
			}
			if err := in.SetGlobal("g", 0xffffffff); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Global("g"); err != nil || got != 0xffffffff {
				t.Fatal("state-only scalar mutation", got, err)
			}
		}},
		{name: "vector-global", source: wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, false, vector))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 3, 0)))), check: func(t *testing.T, in *Instance) {
			if got, err := in.GlobalV128("g"); err != nil || got != want {
				t.Fatal("state-only vector initializer", got, err)
			}
		}},
		{name: "memory-data", source: wasmtest.Module(wasmtest.Section(5, []byte{1, 1, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))), wasmtest.Section(11, []byte{1, 0, 0x41, 0, 0x0b, 2, 42, 43})), check: func(t *testing.T, in *Instance) {
			if got := in.Memory().UnsafeBytes(); len(got) != 65536 || !bytes.Equal(got[:2], []byte{42, 43}) {
				t.Fatal("state-only active data")
			}
		}},
	} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", tc.name, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, "state-only-"+tc.name, tc.source, artifact)
				rv64CheckStateOnlyCode(t, c, 0)
				instances := make([]*Instance, 0, 2)
				defer func() {
					for _, in := range instances {
						_ = in.Close()
					}
				}()
				for range 2 {
					in, err := Instantiate(c, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					instances = append(instances, in)
					if tc.check != nil {
						tc.check(t, in)
					}
				}
				rv64CheckStateOnlyCode(t, c, 2)
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
				rv64CheckStateOnlyCode(t, c, 2)
				if _, err := Instantiate(c, InstantiateOptions{}); err == nil {
					t.Fatal("closed state-only module instantiated")
				}
				for _, in := range instances {
					if _, err := in.Invoke("absent"); err == nil {
						t.Fatal("state-only instance acquired a callable function")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
				}
				rv64CheckStateOnlyCode(t, c, 0)
			})
		}
	}
}

func TestRV64PublicStateOnlyVectorGlobalLinking(t *testing.T) {
	want, expr := rv64StateOnlyVector()
	providerSource := wasmtest.Module(wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, false, expr))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("v", 3, 0))))
	consumerSource := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "v", wasm.V128, false))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.V128, true, []byte{0x23, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("imported", 3, 0), wasmtest.ExportEntry("copied", 3, 1))),
	)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			providerCode := rv64TableStageCompile(t, "state-only-vector-provider", providerSource, artifact)
			consumerCode := rv64TableStageCompile(t, "state-only-vector-consumer", consumerSource, artifact)
			provider, err := Instantiate(providerCode, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			global, err := provider.ExportedGlobalObject("v")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := Instantiate(consumerCode, NewImports().Global("env", "v", global))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if err := providerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumerCode.Close(); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"imported", "copied"} {
				if got, err := consumer.GlobalV128(name); err != nil || got != want {
					t.Fatal("linked vector global", name, got, err)
				}
			}
			changed := want
			changed[0] ^= 255
			if err := consumer.SetGlobalV128("copied", changed); err != nil {
				t.Fatal(err)
			}
			if got, err := provider.GlobalV128("v"); err != nil || got != want {
				t.Fatal("local vector copy aliased provider", got, err)
			}
			rv64CheckStateOnlyCode(t, providerCode, 1)
			rv64CheckStateOnlyCode(t, consumerCode, 1)
		})
	}
}

func TestRV64PublicStateOnlyInitializerRollback(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 1))),
		wasmtest.Section(11, []byte{1, 0, 0x41, 0xff, 0xff, 3, 0x0b, 2, 42, 43}),
	)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-failing-data", source, artifact)
			memory, err := NewSharedMemory(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer memory.Close()
			memory.UnsafeBytes()[65535] = 99
			for range 2 {
				in, err := Instantiate(c, NewImports().Memory("env", "memory", memory))
				if in != nil || err == nil {
					t.Fatal("out-of-bounds state initializer succeeded", err)
				}
				if memory.UnsafeBytes()[65535] != 99 {
					t.Fatal("failed state initializer partially wrote")
				}
				rv64CheckStateOnlyCode(t, c, 0)
			}
		})
	}
}

func TestRV64PublicStateOnlyRejectsFunctionMetadata(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-metadata-base", wasmtest.Module(), artifact)
			for _, mutate := range []func(*Compiled){
				func(c *Compiled) { c.code = []byte{0x13, 0, 0, 0} },
				func(c *Compiled) { c.Entry = []int{0} },
				func(c *Compiled) { c.InternalEntry = []int{0} },
				func(c *Compiled) { c.FuncTypeID = []uint64{1} },
				func(c *Compiled) { c.HasStart = true },
			} {
				bad := *c.executionView()
				mutate(&bad)
				if err := bad.validate(); err == nil {
					t.Fatal("invalid state-only metadata admitted")
				}
				blob, err := marshalCompiled(&bad)
				if err != nil {
					// Some malformed shapes (such as an unavailable start) are
					// rejected by serialization before an artifact can exist.
					t.Log("invalid metadata rejected while serializing:", err)
					continue
				}
				loaded, err := LoadTrustedArtifact(blob)
				if loaded != nil {
					_ = loaded.Close()
				}
				if err == nil {
					t.Fatal("invalid state-only artifact admitted")
				}
			}
		})
	}
}
