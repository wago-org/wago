//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64MixedTable64ActiveTailSource() []byte {
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x05, 4, 4)
	importedActive := []byte{0x04, 0x42, 1, 0x0b, 0x02, 0xd2, 0x00, 0x0b, 0xd2, 0x01, 0x0b}
	ownedActive := []byte{0x06, 0x01, 0x42, 1, 0x0b, 0x70, 0x02, 0xd2, 0x00, 0x0b, 0xd2, 0x01, 0x0b}
	tail := func(table byte) []byte { return []byte{0x20, 0, 0x13, 0, table, 0x0b} }
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x05, 4, 4})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("tail0", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("tail1", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("table0", byte(wasm.ExternTable), 0),
			wasmtest.ExportEntry("table1", byte(wasm.ExternTable), 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(importedActive, ownedActive)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
			wasmtest.Code([]byte{0x41, 8, 0x0b}),
			wasmtest.Code(tail(0)),
			wasmtest.Code(tail(1)),
		)),
	)
}

func TestRV64PublicMixedTable64ActiveTail(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "mixed-table64-active-tail", rv64MixedTable64ActiveTailSource(), artifact)
			if !rv64SourceContractValid(c.executionView()) {
				t.Fatal("mixed table64 active tail metadata contract")
			}
			imported := rv64MixedSizeTestTable(t, true, 4, 4)
			defer imported.Close()
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64HostTailInstantiate(t, rt, c, NewImports().Table("env", "table", imported))
			owned, err := in.ExportedTable("table1")
			if err != nil {
				t.Fatal(err)
			}
			defer owned.Close()
			if err := c.Close(); err != nil {
				t.Fatal("close compiled handle before mixed table64 tails", err)
			}

			for _, tc := range []struct {
				name  string
				index uint64
				want  uint64
			}{{"tail1", 2, 8}, {"tail0", 1, 7}, {"tail1", 1, 7}, {"tail0", 2, 8}} {
				if got, callErr := in.Invoke(tc.name, tc.index); callErr != nil || !slices.Equal(got, []uint64{tc.want}) {
					t.Fatal("mixed table64 active tail", tc, got, callErr)
				}
			}

			// Make table 0 row 1 null while table 1 row 1 remains live. A
			// successful tail1 followed by a trapping tail0 causally proves
			// directory selection rather than accidentally reusing table 0.
			const row1 = 8 + coreruntime.TableEntryBytes
			importedRow := slices.Clone(imported.desc[row1 : row1+coreruntime.TableEntryBytes])
			clear(imported.desc[row1 : row1+coreruntime.TableEntryBytes])
			if got, callErr := in.Invoke("tail1", 1); callErr != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("mixed table64 directory selection", got, callErr)
			}
			got, callErr := in.Invoke("tail0", 1)
			var trap *coreruntime.TrapError
			if callErr == nil || got != nil || !errors.As(callErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
				t.Fatal("mixed table64 primary descriptor was not selected", got, callErr)
			}
			copy(imported.desc[row1:row1+coreruntime.TableEntryBytes], importedRow)

			beforeImported, beforeOwned := slices.Clone(imported.desc), slices.Clone(owned.desc)
			for _, tc := range []struct {
				name     string
				function uint32
				index    uint64
			}{{"tail0", 2, 1<<32 | 2}, {"tail1", 3, 1<<32 | 2}, {"tail0", 2, ^uint64(0)}, {"tail0", 2, 0}, {"tail1", 3, 3}} {
				got, callErr := in.Invoke(tc.name, tc.index)
				var trap *coreruntime.TrapError
				if callErr == nil || got != nil || !errors.As(callErr, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds || len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != tc.function {
					t.Fatal("mixed table64 active tail index did not trap", tc, got, callErr)
				}
				if !slices.Equal(imported.desc, beforeImported) || !slices.Equal(owned.desc, beforeOwned) {
					t.Fatal("mixed table64 active tail trap changed storage", tc)
				}
				if state := &in.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || state.tableDepth != 0 || rv64EntryCapability(in) != 0 {
					t.Fatal("mixed table64 active tail leaked generation", tc)
				}
			}
			if got, callErr := in.Invoke("tail1", 2); callErr != nil || !slices.Equal(got, []uint64{8}) {
				t.Fatal("mixed table64 active tail reuse", got, callErr)
			}
			fn, err := in.WasmFunc("tail1")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			if got, callErr := session.Invoke(2); callErr != nil || !slices.Equal(got, []uint64{8}) {
				t.Fatal("prepared mixed table64 active tail", got, callErr)
			}
			session.Close()
		})
	}
}

func TestRV64PublicMixedTable64ActiveTailGates(t *testing.T) {
	base := rv64MixedTable64ActiveTailSource()
	for name, source := range map[string][]byte{
		"import-table32": bytes.Replace(bytes.Clone(base), []byte{0x70, 0x05, 4, 4}, []byte{0x70, 0x01, 4, 4}, 1),
		"tail1-table0":   bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x13, 0, 1}, []byte{0x20, 0, 0x13, 0, 0}, 1),
		"tail1-nontail":  bytes.Replace(bytes.Clone(base), []byte{0x20, 0, 0x13, 0, 1}, []byte{0x20, 0, 0x11, 0, 1}, 1),
		"target-order":   bytes.Replace(bytes.Clone(base), []byte{0xd2, 0, 0x0b, 0xd2, 1}, []byte{0xd2, 1, 0x0b, 0xd2, 0}, 1),
		"target-body":    bytes.Replace(bytes.Clone(base), []byte{0x41, 8, 0x0b}, []byte{0x41, 9, 0x0b}, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, err := rv64NativeTailConfig().Compile(source)
			if name != "import-table32" {
				if err != nil {
					t.Fatal("valid table variant", err)
				}
				defer c.Close()
				rv64RequireTableArtifactMetadata(t, c)
			} else {
				if c != nil {
					c.Close()
				}
				if err == nil {
					t.Fatal("invalid table index width admitted")
				}
			}
		})
	}

	compiled := rv64NativeTailCompile(t, "mixed-table64-active-tail-gates", base, false)
	for name, mutate := range map[string]func(*Compiled){
		"missing-tail-feature": func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTailCall },
		"table-size":           func(c *Compiled) { c.extraTables[0].Size-- },
		"entry":                func(c *Compiled) { c.Entry[3]-- },
		"function-key":         func(c *Compiled) { c.FuncTypeID[3] ^= 1 },
		"gc-descriptor":        func(c *Compiled) { c.GCTypeDescs[0].Final = false },
		"element":              func(c *Compiled) { c.Elems[1].Values[1].FuncIndex = 0 },
		"table-export":         func(c *Compiled) { c.tableExports["table1"] = 0 },
		"plan-function":        func(c *Compiled) { rv64NativeTailModule(c).Plans[1].FunctionIndex = 2 },
		"plan-home":            func(c *Compiled) { rv64NativeTailModule(c).Plans[1].ReferenceBase = 48 },
		"plan-source":          func(c *Compiled) { rv64NativeTailModule(c).Plans[1].SourcePC++ },
		"code": func(c *Compiled) {
			c.code = bytes.Clone(c.code)
			c.code[len(c.code)-1] ^= 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := rv64ReferenceImportTailCopy(compiled)
			bad.extraTables = slices.Clone(bad.extraTables)
			bad.Entry = slices.Clone(bad.Entry)
			bad.InternalEntry = slices.Clone(bad.InternalEntry)
			bad.FuncTypeID = slices.Clone(bad.FuncTypeID)
			bad.tableExports = maps.Clone(bad.tableExports)
			bad.Elems = slices.Clone(bad.Elems)
			for i := range bad.Elems {
				bad.Elems[i].Values = slices.Clone(bad.Elems[i].Values)
			}
			mutate(bad)
			if bad.validate() == nil {
				t.Fatal("hostile mixed table64 active tail metadata admitted")
			}

		})
	}
}
