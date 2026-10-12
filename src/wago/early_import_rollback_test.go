package wago

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestEarlyConstructionFailureClearsImportedFuncrefs(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			tableTestImportTable("env", "table", 2, 2),
			tableTestImportTable("env", "table", 2, 2),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElemAt(1, 0, 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0x0b}))),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	if err := compiled.validateCached(); err != nil {
		t.Fatal(err)
	}
	// Fault-inject metadata used after active elements are written, but before
	// an Instance exists. The validated frozen view makes this deterministic.
	compiled.executionView().maxParamSlots = -1
	table, err := NewTable(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := Instantiate(compiled, testImports("env.table", table))
	if instance != nil || err == nil || !strings.Contains(err.Error(), "negative slot count") {
		t.Fatalf("early construction failure = %p, %v", instance, err)
	}
	for slot := 0; slot < 2; slot++ {
		off := 8 + slot*coreruntime.TableEntryBytes
		if !bytes.Equal(table.desc[off:off+coreruntime.TableEntryBytes], make([]byte, coreruntime.TableEntryBytes)) {
			t.Fatalf("imported table slot %d retained a failed instance descriptor", slot)
		}
	}
	if err := table.Close(); err != nil {
		t.Fatalf("failed construction left a live table importer: %v", err)
	}
}

func TestEarlyConstructionFailureClearsForeignFuncref(t *testing.T) {
	producerCode := MustCompile(funcrefTokenProducerModule(55))
	defer producerCode.Close()
	producer, err := Instantiate(producerCode)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	target, err := producer.ExportedFunc("target")
	if err != nil {
		t.Fatal(err)
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			portableFuncImportEntry("env", "target", 0),
			tableTestImportTable("env", "table", 1, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	if err := compiled.validateCached(); err != nil {
		t.Fatal(err)
	}
	compiled.executionView().maxResultSlots = -1
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := Instantiate(compiled, testImports("env.target", target, "env.table", table))
	if instance != nil || err == nil || !strings.Contains(err.Error(), "negative slot count") {
		t.Fatalf("early construction failure = %p, %v", instance, err)
	}
	if isNull, err := table.EntryIsNull(0); err != nil || !isNull {
		t.Fatalf("foreign descriptor after rollback: null=%v, err=%v", isNull, err)
	}
	if err := table.Close(); err != nil {
		t.Fatalf("failed construction left a live table importer: %v", err)
	}
}
