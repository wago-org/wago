//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

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
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 0, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("size", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfc, 0x10, 0, 0x0b}))),
	)
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
		"instruction": rv64Table64SizeInstructionSource(),
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
		"missing-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTable64 },
		"table32":         func(v *Compiled) { v.TableAddr64 = false },
		"start":           func(v *Compiled) { v.HasStart = true },
		"initializer":     func(v *Compiled) { v.HasTableInitFunc = true },
		"element":         func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"passive":         func(v *Compiled) { v.passiveElems = []ElemInit{{Mode: ElemModePassive}} },
		"externref":       func(v *Compiled) { v.TableType = ValExternRef },
		"capacity":        func(v *Compiled) { v.TableMax = 1025 },
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
