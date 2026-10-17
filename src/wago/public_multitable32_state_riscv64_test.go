//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64StateOnlyTwoTable32Source() []byte {
	return wasmtest.Module(
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x70, 0x01, 1, 2},
			[]byte{0x70, 0x01, 3, 4},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
	)
}

func TestRV64PublicStateOnlyTwoTable32Export(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "state-only-two-table32", rv64StateOnlyTwoTable32Source(), artifact)
			if c.nativeTable64ABI != nativeTable64NoABI || !rv64StateOnlyTwoTable32(c) || c.tableCount() != 2 || c.requiredFeatures.IsEnabled(CoreFeatureTable64) || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
				t.Fatalf("state-only two-table32 metadata contract: witness=%d qualified=%v tables=%d features=%#x table0=%+v table1=%+v funcs=%+v exports=%v tableExports=%v refs=%v keys=%v", c.nativeTable64ABI, rv64StateOnlyTwoTable32(c), c.tableCount(), c.requiredFeatures, c.tableDef(0), c.tableDef(1), c.Funcs, c.Exports, c.tableExports, c.NeedsFuncRefDescs, c.FuncTypeID)
			}
			rv64CheckStateOnlyCode(t, c, 0)
			instances := make([]*Instance, 0, 2)
			tables := make([][2]*Table, 0, 2)
			for range 2 {
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = in.Close() })
				instances = append(instances, in)
				first, err := in.ExportedTable("table0")
				if err != nil || first.Size() != 1 {
					t.Fatal("state-only table0 export", first, err)
				}
				second, err := in.ExportedTable("table1")
				if err != nil || second.Size() != 3 || second == first {
					t.Fatal("state-only table1 export", second, err)
				}
				if in.jm.TableDirPtr() == 0 || len(first.desc) < 8 || len(second.desc) < 8 || &first.desc[0] == &second.desc[0] {
					t.Fatal("state-only two-table32 descriptor directory")
				}
				for i := 0; i < first.Size(); i++ {
					if null, err := first.EntryIsNull(uint64(i)); err != nil || !null {
						t.Fatal("state-only table0 null entry", i, null, err)
					}
				}
				for i := 0; i < second.Size(); i++ {
					if null, err := second.EntryIsNull(uint64(i)); err != nil || !null {
						t.Fatal("state-only table1 null entry", i, null, err)
					}
				}
				if out, err := in.Invoke("missing"); err == nil || len(out) != 0 || !strings.Contains(err.Error(), "no exported function") {
					t.Fatal("state-only invocation admitted", out, err)
				}
				tables = append(tables, [2]*Table{first, second})
			}
			if tables[0][0] == tables[1][0] || tables[0][1] == tables[1][1] || &tables[0][0].desc[0] == &tables[1][0].desc[0] || &tables[0][1].desc[0] == &tables[1][1].desc[0] {
				t.Fatal("state-only table storage shared between instances")
			}
			consumerCode := rv64TableStageCompile(t, "state-only-two-table32-borrower", rv64ImportedTableModule([]wasm.ValType{wasm.I32}, 7, false, false, false, 1), artifact)
			for index, table := range tables[0] {
				consumer, err := Instantiate(consumerCode, NewImports().Table("env", "table", table))
				if consumer != nil {
					_ = consumer.Close()
				}
				if err != nil || consumer == nil {
					t.Fatal("state-only table borrower", index, err)
				}
			}
			closed, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = closed.Close() })
			closed0, err := closed.ExportedTable("table0")
			if err != nil {
				t.Fatal(err)
			}
			closed1, err := closed.ExportedTable("table1")
			if err != nil {
				t.Fatal(err)
			}
			if err := closed.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := closed0.EntryIsNull(0); err == nil {
				t.Fatal("table0 remained readable after owner close")
			}
			if _, err := closed1.EntryIsNull(0); err == nil {
				t.Fatal("table1 remained readable after owner close")
			}
			rv64CheckStateOnlyCode(t, c, 2)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			for i := range instances {
				if null, err := tables[i][1].EntryIsNull(2); err != nil || !null {
					t.Fatal("state-only table did not outlive compiled handle", i, null, err)
				}
				for _, table := range tables[i] {
					if err := table.Close(); err != nil {
						t.Fatal(err)
					}
					if err := table.Close(); err != nil {
						t.Fatal("repeated table close", err)
					}
				}
				if err := instances[i].Close(); err != nil {
					t.Fatal(err)
				}
				if err := instances[i].Close(); err != nil {
					t.Fatal("repeated instance close", err)
				}
			}
			rv64CheckStateOnlyCode(t, c, 0)
		})
	}
}

func TestRV64PublicStateOnlyTwoTable32CompilerFreeGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") != "load" {
		t.Skip("actual precompiled runtime only")
	}
	if c, err := Compile(nil, rv64StateOnlyTwoTable32Source()); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("compiler-free two-table32 witness used source compilation", err)
	}
}

func TestRV64PublicStateOnlyTwoTable32SourceGates(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source admission only")
	}
	third := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(
		[]byte{0x70, 0x01, 1, 2}, []byte{0x70, 0x01, 3, 4}, []byte{0x70, 0x01, 0, 1},
	)))
	mixedWidth := wasmtest.Module(wasmtest.Section(4, wasmtest.Vec(
		[]byte{0x70, 0x01, 1, 2}, []byte{0x70, 0x05, 3, 4},
	)))
	withMemory := wasmtest.Module(
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 1, 2}, []byte{0x70, 0x01, 3, 4})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 0})),
	)
	for name, bad := range map[string][]byte{
		"third":       third,
		"mixed-width": mixedWidth,
		"memory":      withMemory,
	} {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(nil, bad)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			rv64RequireTableSourceArtifact(t, c)
		})
	}
}

func TestRV64PublicStateOnlyTwoTable32ArtifactGates(t *testing.T) {
	c := rv64TableStageCompile(t, "state-only-two-table32-admission", rv64StateOnlyTwoTable32Source(), true)
	for name, mutate := range map[string]func(*Compiled){
		"missing-ref":   func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		"table64-ref":   func(v *Compiled) { v.requiredFeatures |= CoreFeatureTable64 },
		"bulk-feature":  func(v *Compiled) { v.requiredFeatures |= CoreFeatureBulkMemoryOperations },
		"size-witness":  func(v *Compiled) { v.nativeTable64ABI = nativeTable64SizeABI },
		"missing-table": func(v *Compiled) { v.extraTables = nil },
		"third-table": func(v *Compiled) {
			v.extraTables = append(v.extraTables, tableDef{Size: 0, Max: 1, HasMax: true})
		},
		"first-table64":  func(v *Compiled) { v.TableAddr64 = true },
		"second-table64": func(v *Compiled) { v.extraTables[0].Addr64 = true },
		"second-no-max":  func(v *Compiled) { v.extraTables[0].HasMax = false },
		"second-capacity": func(v *Compiled) {
			v.extraTables[0].Max = railshot.MaxOwnedTableEntries + 1
		},
		"second-externref": func(v *Compiled) { v.extraTables[0].Type = ValExternRef },
		"second-initializer": func(v *Compiled) {
			v.extraTables[0].HasInitFunc = true
			v.extraTables[0].InitFunc = 1
		},
		"function":        func(v *Compiled) { v.Funcs = []FuncSig{{}} },
		"function-export": func(v *Compiled) { v.Exports["f"] = 0 },
		"missing-export":  func(v *Compiled) { delete(v.tableExports, "table1") },
		"aliased-export":  func(v *Compiled) { v.tableExports["table1"] = 0 },
		"element":         func(v *Compiled) { v.Elems = []ElemInit{{Mode: ElemModeActive}} },
		"passive":         func(v *Compiled) { v.passiveElems = []ElemInit{{Mode: ElemModePassive}} },
		"start":           func(v *Compiled) { v.HasStart = true },
		"memory": func(v *Compiled) {
			v.HasMemory = true
			v.MemMinPages = 1
			v.MemMaxPages = 1
		},
		"reference-key": func(v *Compiled) { v.FuncTypeID = []uint64{1} },
		"defined-type": func(v *Compiled) {
			v.Types = []DefinedTypeDescriptor{{Kind: CompositeTypeFunction, Final: true}}
		},
		"gc-type": func(v *Compiled) {
			v.GCTypeDescs = append(v.GCTypeDescs, gc.TypeDesc{})
		},
		"value-type-extra": func(v *Compiled) {
			v.ValueTypes = append(v.ValueTypes, ValueTypeDescriptor{Kind: ValueTypeI32})
		},
		"second-value-index": func(v *Compiled) { v.extraTables[0].ValueTypeIndex = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *c.executionView()
			bad.Funcs = slices.Clone(bad.Funcs)
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.Types = slices.Clone(bad.Types)
			bad.GCTypeDescs = slices.Clone(bad.GCTypeDescs)
			bad.ValueTypes = slices.Clone(bad.ValueTypes)
			bad.Exports = maps.Clone(bad.Exports)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.Elems = slices.Clone(bad.Elems)
			bad.passiveElems = slices.Clone(bad.passiveElems)
			mutate(&bad)
			validDeclaration := false
			switch name {
			case "third-table", "second-capacity", "defined-type", "missing-export", "aliased-export", "element", "memory", "value-type-extra", "missing-ref", "size-witness", "second-no-max", "passive", "bulk-feature", "table64-ref":
				validDeclaration = true
			}
			// General declarations may be valid. Mutating this frozen image
			// must still fail its complete serialization/loading contract.
			if err := bad.validate(); err == nil && !validDeclaration {
				t.Fatal("invalid state-only two-table32 metadata admitted")
			}
			blob, err := marshalCompiled(&bad)
			if err != nil {
				if name == "missing-ref" {
					t.Fatalf("structural feature restoration must serialize: %v", err)
				}
				rv64RequireTableMutationSerializationError(t, name, err)
				return
			}
			loaded, err := LoadTrustedArtifact(blob)
			if name == "missing-ref" {
				if err != nil || loaded == nil || !loaded.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
					t.Fatal("structural multi-table feature was not restored", loaded, err)
				}
				_ = loaded.Close()
				return
			}
			if loaded != nil {
				_ = loaded.Close()
			}
			if err == nil {
				t.Fatal("invalid state-only two-table32 artifact admitted")
			}
		})
	}
	if c.codeCache.refs != 0 || c.codeCache.mem != nil || c.codeCache.base != 0 || c.codeCache.sealed {
		t.Fatal("state-only two-table32 gates reached executable publication")
	}
}

// Each mutant must fail its own structural check or the immutable code/metadata
// binding. An unrelated encoder failure must not satisfy a rejection test.
func rv64RequireTableMutationSerializationError(t *testing.T, name string, err error) {
	t.Helper()
	want := "RV64 native execution contract lacks admitted source or artifact metadata"
	switch name {
	case "invalid-witness":
		want = "unknown RV64 table64 compiler witness"
	case "missing-tail-feature":
		want = "RV64 native tail feature/plan contract"
	case "missing-import":
		want = "import module-name ends length 1 != non-global import count 0"
	case "plan-home":
		want = "RV64 dynamic tail full-width reference home"
	case "start":
		want = "start function requires a bounded defined signature and entry"
	case "second-value-index":
		want = "value type index 1 out of range"
	case "second-externref":
		want = "value type index 0 does not match ABI type externref"
	case "second-param", "first-param", "source-param", "count-param", "second-dest":
		want = "structural params do not match ABI params"
	case "second-result", "result", "drop-result", "target-result":
		want = "structural results do not match ABI results"
	}
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected serialization rejection: %v; want %q", err, want)
	}
}
