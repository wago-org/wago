//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64StateOnlyTable64Source(min, max byte, exports ...string) []byte {
	sections := [][]byte{wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, min, max}))}
	if len(exports) != 0 {
		entries := make([][]byte, len(exports))
		for i, name := range exports {
			entries[i] = wasmtest.ExportEntry(name, byte(wasm.ExternTable), 0)
		}
		sections = append(sections, wasmtest.Section(7, wasmtest.Vec(entries...)))
	}
	return wasmtest.Module(sections...)
}

func TestRV64PublicStateOnlyTable64Export(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-table64", rv64StateOnlyTable64Source(2, 4, "table", "alias"), artifact)
			if !c.TableAddr64 || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !compiledStructuralRequiredFeatures(c).IsEnabled(CoreFeatureTable64) || !rv64StateOnlyTable64(c) {
				t.Fatal("table64 metadata contract")
			}
			rv64CheckStateOnlyCode(t, c, 0)
			instances := make([]*Instance, 0, 2)
			tables := make([]*Table, 0, 2)
			for range 2 {
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = in.Close() })
				instances = append(instances, in)
				table, err := in.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = table.Close() })
				tables = append(tables, table)
				alias, err := in.ExportedTable("alias")
				if err != nil || alias != table {
					t.Fatal("table64 export alias identity", err)
				}
				if table.Size() != 2 {
					t.Fatal("table64 size", table.Size())
				}
				for i := uint64(0); i < 2; i++ {
					if null, err := table.EntryIsNull(i); err != nil || !null {
						t.Fatal("table64 null entry", i, null, err)
					}
				}
				if _, err := table.EntryIsNull(2); err == nil {
					t.Fatal("table64 public index bound ignored")
				}
			}
			if tables[0] == tables[1] {
				t.Fatal("owned table64 storage shared between instances")
			}
			rv64CheckStateOnlyCode(t, c, 2)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			for i, table := range tables {
				if null, err := table.EntryIsNull(0); err != nil || !null {
					t.Fatal("table64 did not outlive compiled handle", i, null, err)
				}
				if err := table.Close(); err != nil {
					t.Fatal(err)
				}
				if err := instances[i].Close(); err != nil {
					t.Fatal(err)
				}
			}
			rv64CheckStateOnlyCode(t, c, 0)
		})
	}
}

func TestRV64PublicStateOnlyTable64CompilerFreeGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") != "load" {
		t.Skip("actual precompiled runtime only")
	}
	if c, err := Compile(nil, rv64StateOnlyTable64Source(0, 4, "table")); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("compiler-free table64 witness used source compilation", err)
	}
}

func rv64Table64SizeInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x10, 0, 0x0b}))),
	)
}

func rv64Table64UnsupportedInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x10, 0, 0x1a, 0x42, 0, 0x0b}))),
	)
}

func rv64Table64GrowInstructionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 0, 0x0b}))),
	)
}

func rv64Table64GrowNoMaxSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x04, 2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 0x0f, 0, 0x0b}))),
	)
}

func rv64RelabelledTable32FunctionSource() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 2, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("value", byte(wasm.ExternFunc), 0), wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x42, 7, 0x0b}))),
	)
}

func TestRV64PublicOwnedTable64Size(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-table64-size", rv64Table64SizeInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64SizeABI || !rv64OwnedTable64Size(c) || !c.requiredFeatures.IsEnabled(CoreFeatureTable64) {
				t.Fatal("table64.size metadata contract")
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table, err := in.ExportedTable("table")
			if err != nil || table.Size() != 2 {
				t.Fatal("table64.size export", table, err)
			}
			t.Cleanup(func() { _ = table.Close() })
			for range 3 {
				if got, err := in.Invoke("size"); err != nil || !slices.Equal(got, []uint64{2}) {
					t.Fatal("table64.size", got, err)
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("size"); err != nil || !slices.Equal(got, []uint64{2}) {
				t.Fatal("table64.size after compiled close", got, err)
			}
		})
	}
}

func TestRV64PublicOwnedTable64Grow(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "owned-table64-grow", rv64Table64GrowInstructionSource(), artifact)
			if c.nativeTable64ABI != nativeTable64GrowABI || !rv64OwnedTable64Grow(c) || !c.requiredFeatures.IsEnabled(CoreFeatureTable64|CoreFeatureReferenceTypes) {
				t.Fatal("table64.grow metadata contract")
			}
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = in.Close() })
			table, err := in.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = table.Close() })
			for _, step := range []struct {
				delta, old uint64
				size       int
			}{
				{^uint64(0), ^uint64(0), 2},
				{1, 2, 3},
				{1, 3, 4},
				{1, ^uint64(0), 4},
				{1 << 32, ^uint64(0), 4},
			} {
				got, err := in.Invoke("grow", step.delta)
				if err != nil || !slices.Equal(got, []uint64{step.old}) || table.Size() != step.size {
					t.Fatal("table64.grow", step, got, table.Size(), err)
				}
			}
		})
	}
}

func TestRV64PublicOwnedTable64GrowArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-table64-grow-admission", rv64Table64GrowInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"size-witness":    func(v *Compiled) { v.nativeTable64ABI = nativeTable64SizeABI },
		"param":           func(v *Compiled) { v.Funcs[0].Params[0] = ValI32 },
		"result":          func(v *Compiled) { v.Funcs[0].Results[0] = ValI32 },
		"missing-ref":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"no-max":          func(v *Compiled) { v.TableHasMax = false },
		"cpu-bit-alias": func(v *Compiled) {
			v.nativeTable64ABI = nativeTable64NoABI
			v.requiredAMD64Features = shared.AMD64Features(1 << 15)
		},
		"extra-function": func(v *Compiled) {
			v.Funcs = append(v.Funcs, FuncSig{})
		},
		"element": func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"table32": func(v *Compiled) { v.TableAddr64 = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Params = slices.Clone(bad.Funcs[0].Params)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64.grow metadata admitted")
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
				t.Fatal("invalid table64.grow artifact admitted")
			}
		})
	}
}

func TestRV64PublicOwnedTable64SizeArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "owned-table64-size-admission", rv64Table64SizeInstructionSource(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64NoABI },
		"result":          func(v *Compiled) { v.Funcs[0].Results[0] = ValI32 },
		"export":          func(v *Compiled) { v.Exports["size"] = 1 },
		"extra-function": func(v *Compiled) {
			v.Funcs = append(v.Funcs, FuncSig{})
		},
		"element": func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"table32": func(v *Compiled) { v.TableAddr64 = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.Funcs[0].Params = slices.Clone(bad.Funcs[0].Params)
			bad.Funcs[0].Results = slices.Clone(bad.Funcs[0].Results)
			bad.Exports = maps.Clone(bad.Exports)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64.size metadata admitted")
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
				t.Fatal("invalid table64.size artifact admitted")
			}
		})
	}

	relabelled := rv64TableStageCompile(t, "ordinary-table32-i64", rv64RelabelledTable32FunctionSource(), false)
	if relabelled.nativeTable64ABI != nativeTable64NoABI {
		t.Fatal("ordinary table32 function acquired table64.size compiler witness")
	}
	bad := *relabelled.executionView()
	bad.TableAddr64 = true
	bad.requiredFeatures |= CoreFeatureTable64
	if err := bad.validate(); err == nil {
		t.Fatal("ordinary table32 function relabelled as table64.size")
	}
	blob, err := marshalCompiled(&bad)
	if err == nil {
		loaded, loadErr := LoadTrustedArtifact(blob)
		if loaded != nil {
			_ = loaded.Close()
		}
		if loadErr == nil {
			t.Fatal("ordinary table32 artifact relabelled as table64.size")
		}
	}
}

func TestRV64PublicStateOnlyTable64SourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source admission only")
	}
	source := rv64StateOnlyTable64Source(0, 4, "table")
	if c, err := NewRuntimeConfig().WithCoreFeatures(platformCoreFeatures() &^ CoreFeatureTable64).Compile(source); c != nil || err == nil {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("disabled table64 feature admitted", err)
	}
	secondTable := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0, 4}, []byte{0x70, 0x05, 0, 4})))
	withMemory := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0, 4})), wasmtest.Section(5, wasmtest.Vec([]byte{0, 0})))
	imported := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 0, 4)
	importedTable := wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(imported)))
	overCapacity := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0, 0x81, 0x08})))
	for name, bad := range map[string][]byte{
		"instruction": rv64Table64UnsupportedInstructionSource(),
		"grow-no-max": rv64Table64GrowNoMaxSource(),
		"second":      secondTable,
		"memory":      withMemory,
		"imported":    importedTable,
		"capacity":    overCapacity,
	} {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, bad)
			if c != nil {
				_ = c.Close()
			}
			if err == nil {
				t.Fatal("unsupported table64 source admitted")
			}
		})
	}
}

func TestRV64PublicStateOnlyTable64ArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-table64-admission", rv64StateOnlyTable64Source(0, 4, "table"), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-feature":            func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTable64 },
		"contradictory-size-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64SizeABI },
		"contradictory-grow-witness": func(v *Compiled) { v.nativeTable64ABI = nativeTable64GrowABI },
		"table32":                    func(v *Compiled) { v.TableAddr64 = false },
		"start":                      func(v *Compiled) { v.HasStart = true },
		"initializer":                func(v *Compiled) { v.HasTableInitFunc = true },
		"element":                    func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"passive":                    func(v *Compiled) { v.passiveElems = []ElemInit{{Mode: ElemModePassive}} },
		"externref":                  func(v *Compiled) { v.TableType = ValExternRef },
		"capacity":                   func(v *Compiled) { v.TableMax = 1025 },
		"extra-table": func(v *Compiled) {
			v.extraTables = append(slices.Clone(v.extraTables), tableDef{Type: ValFuncRef})
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Elems = slices.Clone(bad.Elems)
			bad.passiveElems = slices.Clone(bad.passiveElems)
			mutate(&bad)
			if err := bad.validate(); err == nil {
				t.Fatal("invalid table64 metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				_ = loaded.Close()
			}
			if name == "missing-feature" {
				if err != nil || loaded == nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureTable64) {
					t.Fatal("serialization did not restore structural table64 feature", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid table64 artifact admitted")
			}
		})
	}
	if c.codeCache.refs != 0 || c.codeCache.mem != nil || c.codeCache.base != 0 || c.codeCache.sealed {
		t.Fatal("table64 metadata gates reached executable publication")
	}
}
