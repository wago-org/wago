package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func portableFuncImportEntry(module, name string, typeIdx uint32) []byte {
	entry := append(wasmtest.Name(module), wasmtest.Name(name)...)
	entry = append(entry, 0x00)
	return append(entry, wasmtest.ULEB(typeIdx)...)
}

func sharedMemoryImportEntry(module, name string) []byte {
	entry := append(wasmtest.Name(module), wasmtest.Name(name)...)
	return append(entry, 0x02, 0x01, 0x01, 0x01) // memory, min=1, max=1
}

func crossHostProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			portableFuncImportEntry("env", "async", 0),
			portableFuncImportEntry("env", "sync", 1),
			portableFuncImportEntry("env", "reenter", 1),
			sharedMemoryImportEntry("env", "memory"),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, append(append([]byte{0x41}, wasmtest.SLEB32(111)...), 0x0b)))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call_async", 0, 3),
			wasmtest.ExportEntry("call_sync", 0, 4),
			wasmtest.ExportEntry("target", 0, 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x02, 0x1a, 0x23, 0x00, 0x0b}),
		)),
	)
}

func voidImportForwarderModule(importModule, importName, exportName string) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(portableFuncImportEntry(importModule, importName, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry(exportName, 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func privateSharedMemoryReaderModule(initial int32) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(sharedMemoryImportEntry("env", "memory"))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, append(append([]byte{0x41}, wasmtest.SLEB32(initial)...), 0x0b)))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0x00, 0x0b}))),
	)
}

func crossHostConsumerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			portableFuncImportEntry("producer", "async", 0),
			portableFuncImportEntry("producer", "sync", 1),
			portableFuncImportEntry("producer", "target", 1),
			sharedMemoryImportEntry("env", "memory"),
		)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1),
			wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(1),
		)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x03})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("direct_async", 0, 3),
			wasmtest.ExportEntry("direct_sync", 0, 4),
			wasmtest.ExportEntry("direct_target", 0, 5),
			wasmtest.ExportEntry("indirect_async", 0, 6),
			wasmtest.ExportEntry("indirect_sync", 0, 7),
			wasmtest.ExportEntry("indirect_target", 0, 8),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0, 1, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x10, 0x02, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0x11, 0x01, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x02, 0x11, 0x01, 0x00, 0x0b}),
		)),
	)
}

func TestReplayableHostProducerPropagatesSynchronousDispatch(t *testing.T) {
	producerCode := MustCompile(voidImportForwarderModule("env", "tick", "run"))
	defer producerCode.Close()
	calls := 0
	producer, err := Instantiate(producerCode, Imports{"env.tick": HostFunc(func(HostModule, []uint64, []uint64) { calls++ })})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if !producer.syncMode {
		t.Fatal("replayable host-only producer did not enable synchronous dispatch")
	}

	target, err := producer.ExportedFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	consumerCode := MustCompile(voidImportForwarderModule("producer", "run", "call"))
	defer consumerCode.Close()
	consumer, err := Instantiate(consumerCode, Imports{"producer.run": target})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if !consumer.syncMode {
		t.Fatal("consumer did not inherit replayable producer host capability")
	}
	if results, err := consumer.Invoke("call"); err != nil || len(results) != 0 {
		t.Fatalf("call = %v, %v", results, err)
	}
	if calls != 1 {
		t.Fatalf("host calls = %d, want 1", calls)
	}
}

func TestCrossInstanceHostDispatchUsesActiveCallee(t *testing.T) {
	m1, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer m1.Close()
	m2, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()

	readerCode := MustCompile(privateSharedMemoryReaderModule(222))
	defer readerCode.Close()
	reader, err := Instantiate(readerCode, Imports{"env.memory": m2})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	producerCode := MustCompile(crossHostProducerModule())
	defer producerCode.Close()
	asyncCalls, syncCalls, nestedCalls := 0, 0, 0
	producer, err := Instantiate(producerCode, Imports{
		"env.memory": m2,
		"env.async": HostFunc(func(_ HostModule, _, _ []uint64) {
			asyncCalls++
		}),
		"env.sync": HostFunc(func(_ HostModule, _, results []uint64) {
			syncCalls++
			results[0] = I32(73)
		}),
		"env.reenter": HostFunc(func(_ HostModule, _, results []uint64) {
			nestedCalls++
			values, callErr := reader.Invoke("get")
			if callErr != nil || len(values) != 1 || AsI32(values[0]) != 222 {
				panic("nested shared-memory reader used the wrong instance context")
			}
			results[0] = 0
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if !producer.syncMode {
		t.Fatal("host-capable producer did not enable synchronous dispatch")
	}

	asyncExport, _ := producer.ExportedFunc("call_async")
	syncExport, _ := producer.ExportedFunc("call_sync")
	targetExport, _ := producer.ExportedFunc("target")
	consumerCode := MustCompile(crossHostConsumerModule())
	defer consumerCode.Close()
	consumer, err := Instantiate(consumerCode, Imports{
		"env.memory":      m1,
		"producer.async":  asyncExport,
		"producer.sync":   syncExport,
		"producer.target": targetExport,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if !consumer.syncMode {
		t.Fatal("consumer did not inherit host-parking capability from producer")
	}

	for _, name := range []string{"direct_async", "indirect_async"} {
		if values, callErr := consumer.Invoke(name); callErr != nil || len(values) != 0 {
			t.Fatalf("%s = %v, %v", name, values, callErr)
		}
	}
	for _, name := range []string{"direct_sync", "indirect_sync"} {
		values, callErr := consumer.Invoke(name)
		if callErr != nil || len(values) != 1 || AsI32(values[0]) != 73 {
			t.Fatalf("%s = %v, %v; want 73", name, values, callErr)
		}
	}
	for _, name := range []string{"direct_target", "indirect_target"} {
		values, callErr := consumer.Invoke(name)
		if callErr != nil || len(values) != 1 || AsI32(values[0]) != 111 {
			t.Fatalf("%s = %v, %v; want producer private global 111", name, values, callErr)
		}
	}
	if asyncCalls != 2 || syncCalls != 2 || nestedCalls != 2 {
		t.Fatalf("producer host calls async/sync/nested = %d/%d/%d, want 2/2/2", asyncCalls, syncCalls, nestedCalls)
	}
}
