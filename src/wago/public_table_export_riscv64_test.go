//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicTableExportModule(vector, importedMemory bool) []byte {
	value := wasm.I32
	if vector {
		value = wasm.V128
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{value, wasm.I32}, []wasm.ValType{value}),
			wasmtest.FuncType([]wasm.ValType{value}, []wasm.ValType{value}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
	}
	if importedMemory {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(rv64VectorMemoryImport())))
	}
	sections = append(sections,
		wasmtest.Section(3, []byte{4, 0, 1, 2, 2}),
		wasmtest.Section(4, []byte{1, 0x70, 1, 2, 4}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("grow", 0, 2), wasmtest.ExportEntry("clear", 0, 3),
			wasmtest.ExportEntry("table", 1, 0), wasmtest.ExportEntry("alias", 1, 0))),
		wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 1}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 1, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 15, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0xfc, 16, 0, 0x0b}))),
	)
	return wasmtest.Module(sections...)
}

func TestRV64PublicOwnedTableExportAliasesMutationAndLifetime(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, importedMemory := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-memory%t-artifact%t", vector, importedMemory, artifact), func(t *testing.T) {
					var imports *Imports
					if importedMemory {
						_, memory := importedMemoryProvider(t)
						defer memory.Close()
						imports = NewImports().Memory("env", "memory", memory)
					}
					c := rv64PublicStartCompile(t, rv64PublicTableExportModule(vector, importedMemory), artifact)
					// Mutations to the detached compiler view must not change execution.
					c.tableExports = map[string]int{"wrong": 999}
					in, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					other, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer other.Close()
					if importedMemory && (in.jm != other.jm || in.nativeContext == other.nativeContext) {
						t.Fatal("shared-home siblings require distinct native identities")
					}
					table, err := in.ExportedTable("table")
					if err != nil || table == nil || table.Size() != 2 {
						t.Fatal("table export", table, err)
					}
					alias, err := in.ExportedTable("alias")
					if err != nil || alias != table {
						t.Fatal("table alias identity", err)
					}
					foreign, err := other.ExportedTable("table")
					if err != nil || foreign == table {
						t.Fatal("instances shared an owned table", err)
					}
					if _, err := in.ExportedTable("wrong"); err == nil {
						t.Fatal("detached metadata exposed an undeclared table")
					}
					if err := table.Close(); err != nil || table.Size() != 2 {
						t.Fatal("borrowed export handle closed its producer table", err)
					}
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					runtime.GC()
					for _, entry := range []struct {
						index uint64
						null  bool
					}{{0, false}, {1, true}} {
						if null, err := table.EntryIsNull(entry.index); err != nil || null != entry.null {
							t.Fatal("exported entry state", entry, null, err)
						}
					}
					if _, err := table.EntryIsNull(2); err == nil {
						t.Fatal("export diagnostic read exceeded length")
					}
					input, want := []uint64{0xbadcafe0deadbeef}, []uint64{0xdeadbeef}
					if vector {
						input = []uint64{0x7ff0000000000001, 0x80000000deadbeef}
						want = slices.Clone(input)
					}
					args := append(slices.Clone(input), 0xfeedface00000000)
					for range 3 {
						got, err := in.Invoke("call", args...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("exported local indirect target", got, err)
						}
					}
					if got, err := in.Invoke("grow", 0xdeadbeef00000001); err != nil || !slices.Equal(got, []uint64{2}) || table.Size() != 3 || foreign.Size() != 2 {
						t.Fatal("export growth/isolation", got, err)
					}
					if null, err := table.EntryIsNull(2); err != nil || !null {
						t.Fatal("new entry was not null", null, err)
					}
					if got, err := in.Invoke("grow", 2); err != nil || !slices.Equal(got, []uint64{0xffffffff}) || table.Size() != 3 {
						t.Fatal("failed growth changed exported descriptor", got, err)
					}
					if got, err := in.Invoke("clear", 0xdeadbeef00000000); err != nil || !slices.Equal(got, []uint64{3}) {
						t.Fatal("clear", got, err)
					}
					if null, err := alias.EntryIsNull(0); err != nil || !null {
						t.Fatal("alias missed mutation", null, err)
					}
					_, err = in.Invoke("call", args...)
					ownedTableTrap(t, err, coreruntime.TrapIndirectOutOfBounds)
					if got, err := other.Invoke("call", args...); err != nil || !slices.Equal(got, want) {
						t.Fatal("other owned table changed", got, err)
					}
					if err := in.Close(); err != nil || table.Size() != 0 {
						t.Fatal("closed instance left a readable export", err)
					}
					if _, err := alias.EntryIsNull(0); err == nil {
						t.Fatal("closed alias retained a dangling read")
					}
					if err := other.Close(); err != nil || c.codeCache.refs != 0 || c.codeCache.mem != nil {
						t.Fatal("last table owner left a code mapping", err)
					}
				})
			}
		}
	}
}

func TestRV64PublicOwnedTableExportImportsThroughQualifiedProfile(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 4))),
		wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x11, 0, 0, 0x0b}))),
	)
	c, err := Compile(nil, source)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	table, err := NewTable(2, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	in, err := Instantiate(c, NewImports().Table("env", "table", table))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	_, err = in.Invoke("call", 0)
	ownedTableTrap(t, err, coreruntime.TrapIndirectOutOfBounds)
}
