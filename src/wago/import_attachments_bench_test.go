package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep setup outside the timed region so this measures the retention pass,
// including scans of imported containers, rather than compilation or linking.
func BenchmarkRetainImportedFuncrefTableRoots(b *testing.B) {
	for _, count := range []int{1, 2, 4, 16, 32, 64} {
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

// Populated tables exercise descriptor resolution and propagation in the
// finalization path, where new producer roots can appear during a pass.
func BenchmarkRetainImportedFuncrefTableRootsFinalization(b *testing.B) {
	for _, count := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			rt := NewRuntime()
			defer rt.Close()
			producerMod, err := rt.Compile(noArgI32ProducerModule(true, 42))
			if err != nil {
				b.Fatal(err)
			}
			defer producerMod.Close()
			producer, err := rt.Instantiate(context.Background(), producerMod)
			if err != nil {
				b.Fatal(err)
			}
			defer producer.Close()
			descriptor, ok := producer.localFuncrefDescriptor(0)
			if !ok {
				b.Fatal("producer has no funcref descriptor")
			}
			entries := make([][]byte, count)
			imports := NewImports()
			tables := make([]*Table, count)
			for i := range entries {
				name := fmt.Sprintf("table%d", i)
				entries[i] = tableTestImportTable("env", name, 1, 1)
				table, err := NewTable(1, 1)
				if err != nil {
					b.Fatal(err)
				}
				tables[i] = table
				imports.Table("env", name, table)
			}
			mod, err := rt.Compile(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(entries...))))
			if err != nil {
				b.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
			if err != nil {
				b.Fatal(err)
			}
			defer func() {
				_ = in.Close()
				for _, table := range tables {
					_ = table.Close()
				}
			}()
			for _, table := range tables {
				binary.LittleEndian.PutUint64(table.desc[8+coreruntime.TableEntryRefSlotOffset:], descriptor)
			}
			if !retainProducerRootsInImportedContainersForFinalization(in) {
				b.Fatal("populated tables did not retain a producer")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !retainProducerRootsInImportedContainersForFinalization(in) {
					b.Fatal("populated tables lost their producer")
				}
			}
		})
	}
}

func BenchmarkRetainImportedFuncrefGlobalRoots(b *testing.B) {
	for _, count := range []int{1, 4, 16, 64} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			rt := NewRuntime()
			defer rt.Close()
			entries := make([][]byte, count)
			imports := NewImports()
			globals := make([]*Global, count)
			for i := range entries {
				name := fmt.Sprintf("global%d", i)
				entries[i] = wasmtest.GlobalImportEntry("env", name, wasm.FuncRef, true)
				global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
				if err != nil {
					b.Fatal(err)
				}
				globals[i] = global
				imports.Global("env", name, global)
			}
			mod, err := rt.Compile(wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(entries...))))
			if err != nil {
				b.Fatal(err)
			}
			defer mod.Close()
			in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
			if err != nil {
				b.Fatal(err)
			}
			defer func() {
				_ = in.Close()
				for _, global := range globals {
					_ = global.Close()
				}
			}()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if retainProducerRootsInImportedGlobals(in) {
					b.Fatal("empty globals retained a producer")
				}
			}
		})
	}
}
