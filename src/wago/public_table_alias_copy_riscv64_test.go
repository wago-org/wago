//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64AliasedTableImport(name string) []byte {
	entry := append(wasmtest.Name("env"), wasmtest.Name(name)...)
	return append(entry, 1, 0x70, 1, 4, 4)
}

func rv64AliasedTableProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 4, 4})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(9, wasmtest.Vec(
			[]byte{0, 0x41, 0, 0x0b, 1, 0},
			[]byte{0, 0x41, 2, 0x0b, 1, 0},
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))),
	)
}

func rv64AliasedTableConsumerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil,
		))),
		wasmtest.Section(2, wasmtest.Vec(
			rv64AliasedTableImport("a"), rv64AliasedTableImport("b"),
		)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("copy", 0, 0),
			wasmtest.ExportEntry("a", 1, 0),
			wasmtest.ExportEntry("b", 1, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 14, 0, 1, 0x0b,
		}))),
	)
}

func TestRV64PublicAliasedImportedTableCopy(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact=%t", artifact), func(t *testing.T) {
			compile := func(source []byte) *Compiled {
				t.Helper()
				compiled, err := Compile(source)
				if err != nil {
					t.Fatal(err)
				}
				if artifact {
					blob, err := compiled.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := LoadTrustedArtifact(blob)
					if err != nil {
						t.Fatal(err)
					}
					if err := compiled.Close(); err != nil {
						t.Fatal(err)
					}
					compiled = loaded
				}
				t.Cleanup(func() { _ = compiled.Close() })
				return compiled
			}

			producer, err := Instantiate(compile(rv64AliasedTableProducerModule()))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			table, err := producer.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			consumer, err := Instantiate(
				compile(rv64AliasedTableConsumerModule()),
				NewImports().Table("env", "a", table).Table("env", "b", table),
			)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			a, err := consumer.ExportedTable("a")
			if err != nil {
				t.Fatal(err)
			}
			b, err := consumer.ExportedTable("b")
			if err != nil {
				t.Fatal(err)
			}
			if a != table || b != table {
				t.Fatal("imports do not share the source table")
			}

			nulls := func() []bool {
				result := make([]bool, 4)
				for index := range result {
					var err error
					result[index], err = table.EntryIsNull(uint64(index))
					if err != nil {
						t.Fatal(err)
					}
				}
				return result
			}
			if got := nulls(); !slices.Equal(got, []bool{false, true, false, true}) {
				t.Fatal("initial table", got)
			}
			if _, err := consumer.Invoke("copy", 1, 0, 3); err != nil {
				t.Fatal(err)
			}
			if got := nulls(); !slices.Equal(got, []bool{false, false, true, false}) {
				t.Fatal("overlapping aliased copy", got)
			}
		})
	}
}
