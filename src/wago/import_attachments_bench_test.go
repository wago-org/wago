package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep setup outside the timed region so this measures the retention pass,
// including scans of imported containers, rather than compilation or linking.
func BenchmarkRetainImportedFuncrefTableRoots(b *testing.B) {
	for _, count := range []int{16, 32, 64} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			entries := make([][]byte, count)
			imports := NewImports()
			tables := make([]*Table, count)
			for i := range entries {
				name := fmt.Sprintf("table%d", i)
				entries[i] = tableTestImportTable("env", name, 0, 0)
				table, err := NewTable(0, 0)
				if err != nil {
					b.Fatal(err)
				}
				tables[i] = table
				imports.Table("env", name, table)
			}
			code := MustCompile(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(entries...))))
			defer code.Close()
			in, err := Instantiate(code, InstantiateOptions{Imports: imports})
			if err != nil {
				b.Fatal(err)
			}
			defer func() {
				_ = in.Close()
				for _, table := range tables {
					_ = table.Close()
				}
			}()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if retainProducerRootsInImportedTables(in) {
					b.Fatal("empty tables retained a producer")
				}
			}
		})
	}
}
