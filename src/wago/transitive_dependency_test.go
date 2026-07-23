package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func forwardingConsumerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(portableFuncImportEntry("env", "target", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func importedTableUserModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("use", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tableTestBody(tableTestBulk(16, 0))))), // table.size 0
	)
}

func importedFuncrefGlobalUserModule() []byte {
	globalImport := append(wasmtest.Name("env"), wasmtest.Name("global")...)
	globalImport = append(globalImport, 0x03, 0x70, 0x01) // mutable funcref global
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("use", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0xd1, 0x0b}))),
	)
}

func importedHostFuncRefUserModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(portableFuncImportEntry("env", "host", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("use", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func TestCrossInstanceRetentionKeepsImportedOwnersAttached(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		owner, err := NewTable(1, 1)
		if err != nil {
			t.Fatal(err)
		}
		producerCode := MustCompile(importedTableUserModule())
		defer producerCode.Close()
		producer, err := Instantiate(producerCode, Imports{"env.table": owner})
		if err != nil {
			t.Fatal(err)
		}
		export, _ := producer.ExportedFunc("use")
		consumerCode := MustCompile(forwardingConsumerModule())
		defer consumerCode.Close()
		consumer, err := Instantiate(consumerCode, Imports{"env.target": export})
		if err != nil {
			t.Fatal(err)
		}
		if err := producer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := owner.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
			t.Fatalf("table Close while retained code is callable = %v", err)
		}
		if values, err := consumer.Invoke("call"); err != nil || len(values) != 1 || AsI32(values[0]) != 1 {
			t.Fatalf("retained table call = %v, %v; want 1", values, err)
		}
		if err := consumer.Close(); err != nil {
			t.Fatal(err)
		}
		if got := owner.owner.importers; got != 0 {
			t.Fatalf("table importer count after physical release = %d, want 0", got)
		}
		if err := owner.Close(); err != nil {
			t.Fatalf("table Close after final root: %v", err)
		}
	})

	t.Run("reference global", func(t *testing.T) {
		rt := NewRuntime()
		owner, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
		if err != nil {
			t.Fatal(err)
		}
		producerMod, err := rt.Compile(importedFuncrefGlobalUserModule())
		if err != nil {
			t.Fatal(err)
		}
		producer, err := rt.Instantiate(context.Background(), producerMod, WithImports(Imports{"env.global": owner}))
		if err != nil {
			t.Fatal(err)
		}
		export, _ := producer.ExportedFunc("use")
		consumerMod, err := rt.Compile(forwardingConsumerModule())
		if err != nil {
			t.Fatal(err)
		}
		consumer, err := rt.Instantiate(context.Background(), consumerMod, WithImports(Imports{"env.target": export}))
		if err != nil {
			t.Fatal(err)
		}
		if err := producer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := owner.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
			t.Fatalf("global Close while retained code is callable = %v", err)
		}
		if values, err := consumer.Call(context.Background(), "call"); err != nil || len(values) != 1 || values[0].I32() != 1 {
			t.Fatalf("retained global call = %v, %v; want 1", values, err)
		}
		if err := consumer.Close(); err != nil {
			t.Fatal(err)
		}
		if got := owner.owner.importers; got != 0 {
			t.Fatalf("global importer count after physical release = %d, want 0", got)
		}
		if err := owner.Close(); err != nil {
			t.Fatalf("global Close after final root: %v", err)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("host funcref", func(t *testing.T) {
		rt := NewRuntime()
		owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
			results[0] = I32(42)
		}), FuncSig{Results: []ValType{ValI32}})
		if err != nil {
			t.Fatal(err)
		}
		producerMod, err := rt.Compile(importedHostFuncRefUserModule())
		if err != nil {
			t.Fatal(err)
		}
		producer, err := rt.Instantiate(context.Background(), producerMod, WithImports(Imports{"env.host": owner}))
		if err != nil {
			t.Fatal(err)
		}
		export, _ := producer.ExportedFunc("use")
		consumerMod, err := rt.Compile(forwardingConsumerModule())
		if err != nil {
			t.Fatal(err)
		}
		consumer, err := rt.Instantiate(context.Background(), consumerMod, WithImports(Imports{"env.target": export}))
		if err != nil {
			t.Fatal(err)
		}
		if err := producer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := owner.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
			t.Fatalf("HostFuncRef.Close while retained code is callable = %v", err)
		}
		if values, err := consumer.Call(context.Background(), "call"); err != nil || len(values) != 1 || values[0].I32() != 42 {
			t.Fatalf("retained host call = %v, %v; want 42", values, err)
		}
		if err := consumer.Close(); err != nil {
			t.Fatal(err)
		}
		owner.mu.Lock()
		importers := owner.importers
		owner.mu.Unlock()
		if importers != 0 {
			t.Fatalf("host importer count after physical release = %d, want 0", importers)
		}
		if err := owner.Close(); err != nil {
			t.Fatalf("HostFuncRef.Close after final root: %v", err)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	})
}
