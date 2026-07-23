package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func noArgI32ProducerModule(withDescriptorArena bool, value int32) []byte {
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
	}
	if withDescriptorArena {
		sections = append(sections,
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		)
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 0))))
	if withDescriptorArena {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))))
	}
	body := append([]byte{0x41}, wasmtest.SLEB32(value)...)
	body = append(body, 0x0b)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	return wasmtest.Module(sections...)
}

func importedFuncrefTableWriterModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			portableFuncImportEntry("env", "target", 0),
			tableTestImportTable("env", "table", 1, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
	)
}

func importedFuncrefTableCallerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}))),
	)
}

func importedFuncrefTableClearerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "table", 1, 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("clear", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0xd0, 0x70, 0x26, 0x00, 0x0b}))),
	)
}

func clearSharedFuncrefSlot(t *testing.T, table *Table) {
	t.Helper()
	code := MustCompile(importedFuncrefTableClearerModule())
	defer code.Close()
	clearer, err := Instantiate(code, Imports{"env.table": table})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clearer.Invoke("clear"); err != nil {
		t.Fatal(err)
	}
	if err := clearer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestImportedAndProxyFuncrefsTransferWriterLifetimeToTable(t *testing.T) {
	for _, tc := range []struct {
		name                string
		withDescriptorArena bool
	}{
		{name: "producer canonical descriptor", withDescriptorArena: true},
		{name: "importer proxy descriptor", withDescriptorArena: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table, err := NewTable(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			producerCode := MustCompile(noArgI32ProducerModule(tc.withDescriptorArena, 77))
			defer producerCode.Close()
			producer, err := Instantiate(producerCode)
			if err != nil {
				t.Fatal(err)
			}
			export, err := producer.ExportedFunc("target")
			if err != nil {
				t.Fatal(err)
			}
			writerCode := MustCompile(importedFuncrefTableWriterModule())
			defer writerCode.Close()
			writer, err := Instantiate(writerCode, Imports{"env.target": export, "env.table": table})
			if err != nil {
				t.Fatal(err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if !writer.hasResourceRoots() {
				t.Fatal("external table did not retain the writer attachment chain")
			}

			callerCode := MustCompile(importedFuncrefTableCallerModule())
			defer callerCode.Close()
			caller, err := Instantiate(callerCode, Imports{"env.table": table})
			if err != nil {
				t.Fatal(err)
			}
			values, err := caller.Invoke("call")
			if err != nil || len(values) != 1 || AsI32(values[0]) != 77 {
				t.Fatalf("call through retained descriptor = %v, %v; want 77", values, err)
			}
			if err := caller.Close(); err != nil {
				t.Fatal(err)
			}
			clearSharedFuncrefSlot(t, table)
			if writer.hasResourceRoots() || !writer.resourcesClosed || !producer.resourcesClosed {
				t.Fatalf("overwrite did not release writer/producer: writer roots=%v closed=%v producer closed=%v", writer.hasResourceRoots(), writer.resourcesClosed, producer.resourcesClosed)
			}
			if err := table.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHostFuncRefProxyTransfersWriterLifetimeToTable(t *testing.T) {
	rt := NewRuntime()
	owner, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, _, results []uint64) {
		results[0] = I32(91)
	}), FuncSig{Results: []ValType{ValI32}})
	if err != nil {
		t.Fatal(err)
	}
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	writerMod, err := rt.Compile(importedFuncrefTableWriterModule())
	if err != nil {
		t.Fatal(err)
	}
	writer, err := rt.Instantiate(context.Background(), writerMod, WithImports(Imports{"env.target": owner, "env.table": table}))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err == nil || !strings.Contains(err.Error(), "live importer") {
		t.Fatalf("HostFuncRef.Close while table retains proxy = %v", err)
	}

	callerMod, err := rt.Compile(importedFuncrefTableCallerModule())
	if err != nil {
		t.Fatal(err)
	}
	caller, err := rt.Instantiate(context.Background(), callerMod, WithImports(Imports{"env.table": table}))
	if err != nil {
		t.Fatal(err)
	}
	values, err := caller.Call(context.Background(), "call")
	if err != nil || len(values) != 1 || values[0].I32() != 91 {
		t.Fatalf("call retained HostFuncRef proxy = %v, %v; want 91", values, err)
	}
	if err := caller.Close(); err != nil {
		t.Fatal(err)
	}
	clearSharedFuncrefSlot(t, table)
	if err := owner.Close(); err != nil {
		t.Fatalf("HostFuncRef.Close after overwrite: %v", err)
	}
	if err := table.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}
