//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcAllocatingScalarProducerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	i32Type := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, i32Type)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("allocate", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}),
		)),
	)
}

func gcAtomicWaitReferenceResultModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	resultType := []byte{0x60, 0x00, 0x01, 0x63, 0x00}
	waitType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, byte(wasm.ExternMem), 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, resultType, waitType)),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("new", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("wait", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x00, 0x41, 0x00, 0x42, 0x00, 0xfe, 0x01, 0x02, 0x00, 0x0b}),
		)),
	)
}

func gcDynamicPrivateReferenceResultModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	resultType := []byte{0x60, 0x00, 0x01, 0x63, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, resultType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0x00, 0x01})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x70, 0x01, 0xd0, 0x70, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("new", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x01,
			0xfb, 0x00, 0x00,
			0x0b,
		}))),
	)
}

func gcReferenceTokenProducerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	resultType := []byte{0x60, 0x00, 0x01, 0x63, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, resultType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("new", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x01,
			0xfb, 0x00, 0x00,
			0x0b,
		}))),
	)
}

func gcAllocatingHostScalarProducerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7e, 0x01}
	i32Type := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("host")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc), 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, i32Type)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x42, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x10, 0x00, 0x0b}),
		)),
	)
}

func gcPrivateAllocatingFuncrefProducerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	targetType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	getType := wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, targetType, getType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
		)),
	)
}

func gcAllocatingFuncrefProducerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	targetType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	getType := wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, targetType, getType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
		)),
	)
}

func localFuncrefRelayModule() []byte {
	targetType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	callType := wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I32})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(targetType, callType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0x20, 0x00, 0x26, 0x00,
			0x41, 0x00, 0x11, 0x00, 0x00,
			0x0b,
		}))),
	)
}

func parameterFuncrefRelayModule() []byte {
	targetType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	callType := []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(targetType, callType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x20, 0x00,
			0x14, 0x00,
			0x0b,
		}))),
	)
}

func dynamicScalarRelayModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x01, 0x0b}))),
	)
}

func importedFuncrefGlobalRelayModule() []byte {
	targetType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	globalImport := append(wasmtest.Name("env"), wasmtest.Name("target")...)
	globalImport = append(globalImport, byte(wasm.ExternGlobal), byte(wasm.HeapFunc), 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(targetType)),
		wasmtest.Section(2, wasmtest.Vec(globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0x23, 0x00, 0x26, 0x00,
			0x41, 0x00, 0x11, 0x00, 0x00,
			0x0b,
		}))),
	)
}

func dynamicFuncrefGlobalHostRelayModule() []byte {
	targetType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	hostType := wasmtest.FuncType(nil, nil)
	hostImport := append(wasmtest.Name("env"), wasmtest.Name("install")...)
	hostImport = append(hostImport, byte(wasm.ExternFunc), 0x01)
	globalImport := append(wasmtest.Name("env"), wasmtest.Name("target")...)
	globalImport = append(globalImport, byte(wasm.ExternGlobal), byte(wasm.HeapFunc), 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(targetType, hostType)),
		wasmtest.Section(2, wasmtest.Vec(hostImport, globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x10, 0x00,
			0x41, 0x00, 0x23, 0x00, 0x26, 0x00,
			0x41, 0x00, 0x11, 0x00, 0x00,
			0x0b,
		}))),
	)
}

func gcInvocationFuncrefTableWriterModule() []byte {
	setType := wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, nil)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(setType)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "target", 1, 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x20, 0x00, 0x26, 0x00, 0x0b}))),
	)
}

func importedFuncrefTableRelayModule() []byte {
	targetType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(targetType)),
		wasmtest.Section(2, wasmtest.Vec(tableTestImportTable("env", "target", 1, 1))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}))),
	)
}

func scalarCrossInstanceRelayModule() []byte {
	i32Type := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("allocate")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc), 0x00)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(i32Type)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("direct", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("wrapped", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func scalarTwoProducerRelayModule() []byte {
	i32Type := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	firstImport := append(wasmtest.Name("env"), wasmtest.Name("first")...)
	firstImport = append(firstImport, byte(wasm.ExternFunc), 0x00)
	secondImport := append(wasmtest.Name("env"), wasmtest.Name("second")...)
	secondImport = append(secondImport, byte(wasm.ExternFunc), 0x00)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(i32Type)),
		wasmtest.Section(2, wasmtest.Vec(firstImport, secondImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x0b}))),
	)
}

func TestMutableFuncrefCalleesWaitForProducerGCInvocationLease(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	producerCode, err := Compile(cfg, gcAllocatingFuncrefProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	store := rt.refStore
	producer, err := instantiateCore(producerCode, InstantiateOptions{
		GC:    GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
		store: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	ref, err := producer.Invoke("get")
	if err != nil || len(ref) != 1 || ref[0] == 0 {
		t.Fatalf("producer get = %v, %v; want one non-null funcref", ref, err)
	}

	type result struct {
		values []uint64
		err    error
	}
	assertWaits := func(t *testing.T, call func() ([]uint64, error)) {
		t.Helper()
		lease := producer.lockGCInvocation(newInvocationID())
		done := make(chan result, 1)
		go func() {
			out, callErr := call()
			done <- result{values: out, err: callErr}
		}()
		select {
		case got := <-done:
			lease.unlock()
			t.Fatalf("mutable funcref call completed before producer GC lease release: %v, %v", got.values, got.err)
		case <-time.After(50 * time.Millisecond):
		}
		lease.unlock()
		select {
		case got := <-done:
			if got.err != nil || len(got.values) != 1 || AsI32(got.values[0]) != 7 {
				t.Fatalf("mutable funcref call after lease release = %v, %v; want [7], nil", got.values, got.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("mutable funcref call did not resume after producer GC lease release")
		}
	}

	t.Run("parameter-call-ref", func(t *testing.T) {
		code, compileErr := Compile(cfg, parameterFuncrefRelayModule())
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		defer code.Close()
		if !code.NeedsFuncRefDescs {
			t.Fatal("call_ref relay did not retain its instance-context descriptor header")
		}
		relay, instantiateErr := instantiateCore(code, InstantiateOptions{store: store})
		if instantiateErr != nil {
			t.Fatal(instantiateErr)
		}
		defer relay.Close()
		assertWaits(t, func() ([]uint64, error) { return relay.Invoke("call", ref[0]) })
	})

	t.Run("local-table-argument", func(t *testing.T) {
		code, compileErr := Compile(cfg, localFuncrefRelayModule())
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		defer code.Close()
		relay, instantiateErr := instantiateCore(code, InstantiateOptions{store: store})
		if instantiateErr != nil {
			t.Fatal(instantiateErr)
		}
		defer relay.Close()
		assertWaits(t, func() ([]uint64, error) { return relay.Invoke("call", ref[0]) })
	})

	t.Run("imported-global-later-mutation", func(t *testing.T) {
		global, globalErr := rt.NewFuncRefGlobal(NullFuncRef(), true)
		if globalErr != nil {
			t.Fatal(globalErr)
		}
		defer global.Close()
		code, compileErr := Compile(cfg, importedFuncrefGlobalRelayModule())
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		defer code.Close()
		relay, instantiateErr := instantiateCore(code, InstantiateOptions{store: store, Imports: Imports{"env.target": global}})
		if instantiateErr != nil {
			t.Fatal(instantiateErr)
		}
		defer relay.Close()
		if setErr := global.SetValue(ValueFuncRef(FuncRef{token: ref[0]})); setErr != nil {
			t.Fatal(setErr)
		}
		assertWaits(t, func() ([]uint64, error) { return relay.Invoke("call") })
	})

	t.Run("imported-table-later-mutation", func(t *testing.T) {
		table, tableErr := NewTable(1, 1)
		if tableErr != nil {
			t.Fatal(tableErr)
		}
		defer table.Close()
		writerCode, compileErr := Compile(cfg, gcInvocationFuncrefTableWriterModule())
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		defer writerCode.Close()
		relayCode, compileErr := Compile(cfg, importedFuncrefTableRelayModule())
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		defer relayCode.Close()
		writer, instantiateErr := instantiateCore(writerCode, InstantiateOptions{store: store, Imports: Imports{"env.target": table}})
		if instantiateErr != nil {
			t.Fatal(instantiateErr)
		}
		defer writer.Close()
		relay, instantiateErr := instantiateCore(relayCode, InstantiateOptions{store: store, Imports: Imports{"env.target": table}})
		if instantiateErr != nil {
			t.Fatal(instantiateErr)
		}
		defer relay.Close()
		if _, setErr := writer.Invoke("set", ref[0]); setErr != nil {
			t.Fatal(setErr)
		}
		assertWaits(t, func() ([]uint64, error) { return relay.Invoke("call") })
	})
}

func TestForeignRuntimeHostFuncrefTableImportRejected(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	producerCode, err := Compile(cfg, gcAllocatingFuncrefProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	writerCode, err := Compile(cfg, gcInvocationFuncrefTableWriterModule())
	if err != nil {
		t.Fatal(err)
	}
	defer writerCode.Close()
	relayCode, err := Compile(cfg, importedFuncrefTableRelayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCode.Close()
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	rtA := NewRuntime(WithRuntimeConfig(cfg))
	defer rtA.Close()
	rtB := NewRuntime(WithRuntimeConfig(cfg))
	defer rtB.Close()
	producer, err := instantiateCore(producerCode, InstantiateOptions{store: rtA.refStore})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	writer, err := instantiateCore(writerCode, InstantiateOptions{store: rtA.refStore, Imports: Imports{"env.target": table}})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := producer.Invoke("get")
	if err != nil || len(ref) != 1 || ref[0] == 0 {
		t.Fatalf("producer get = %v, %v; want one funcref", ref, err)
	}
	if _, err := writer.Invoke("set", ref[0]); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := instantiateCore(relayCode, InstantiateOptions{store: rtB.refStore, Imports: Imports{"env.target": table}}); err == nil || !strings.Contains(err.Error(), "different Runtime GC reference store") {
		t.Fatalf("foreign Runtime funcref-table import error = %v, want reference-store rejection", err)
	}
}

func TestPrivateGCProducerInHostFuncrefTableRejectedAcrossRuntime(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	producerCode, err := Compile(cfg, gcPrivateAllocatingFuncrefProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	writerCode, err := Compile(cfg, gcInvocationFuncrefTableWriterModule())
	if err != nil {
		t.Fatal(err)
	}
	defer writerCode.Close()
	relayCode, err := Compile(cfg, importedFuncrefTableRelayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCode.Close()
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	rtA := NewRuntime(WithRuntimeConfig(cfg))
	defer rtA.Close()
	rtB := NewRuntime(WithRuntimeConfig(cfg))
	defer rtB.Close()
	producer, err := instantiateCore(producerCode, InstantiateOptions{store: rtA.refStore})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	if rtA.refStore.ownsGCCollector(producer.gc) {
		t.Fatal("private funcref producer unexpectedly joined the Runtime topology")
	}
	writer, err := instantiateCore(writerCode, InstantiateOptions{store: rtA.refStore, Imports: Imports{"env.target": table}})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := producer.Invoke("get")
	if err != nil || len(ref) != 1 || ref[0] == 0 {
		t.Fatalf("private producer get = %v, %v; want one funcref", ref, err)
	}
	if _, err := writer.Invoke("set", ref[0]); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := instantiateCore(relayCode, InstantiateOptions{store: rtB.refStore, Imports: Imports{"env.target": table}}); err == nil || (!strings.Contains(err.Error(), "incompatible GC invocation domain") && !strings.Contains(err.Error(), "different Runtime GC reference store")) {
		t.Fatalf("private GC funcref-table import error = %v, want explicit domain rejection", err)
	}
}

func TestRuntimeGCDomainCreationRejectsSharedForeignFuncrefTable(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	relayCode, err := Compile(cfg, importedFuncrefTableRelayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCode.Close()
	producerCode, err := Compile(cfg, gcReferenceTokenProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	rtA := NewRuntime(WithRuntimeConfig(cfg))
	defer rtA.Close()
	rtB := NewRuntime(WithRuntimeConfig(cfg))
	defer rtB.Close()
	first, err := instantiateCore(relayCode, InstantiateOptions{store: rtA.refStore, Imports: Imports{"env.target": table}})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := instantiateCore(relayCode, InstantiateOptions{store: rtB.refStore, Imports: Imports{"env.target": table}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := instantiateCore(producerCode, InstantiateOptions{store: rtA.refStore}); err == nil || !strings.Contains(err.Error(), "cannot share a mutable funcref table") {
		t.Fatalf("GC domain creation with foreign funcref-table importer error = %v, want explicit rejection", err)
	}
}

func TestPrivateGCCollectorCreationRejectsSharedForeignFuncrefTable(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	relayCode, err := Compile(cfg, importedFuncrefTableRelayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCode.Close()
	producerCode, err := Compile(cfg, gcDynamicPrivateReferenceResultModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	table, err := NewTable(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	rtA := NewRuntime(WithRuntimeConfig(cfg))
	defer rtA.Close()
	rtB := NewRuntime(WithRuntimeConfig(cfg))
	defer rtB.Close()
	first, err := instantiateCore(relayCode, InstantiateOptions{store: rtA.refStore, Imports: Imports{"env.target": table}})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := instantiateCore(relayCode, InstantiateOptions{store: rtB.refStore, Imports: Imports{"env.target": table}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := instantiateCore(producerCode, InstantiateOptions{store: rtA.refStore}); err == nil || !strings.Contains(err.Error(), "cannot share a mutable funcref table") {
		t.Fatalf("private GC creation with foreign funcref-table importer error = %v, want explicit rejection", err)
	}
}

func TestDynamicPrivateCollectorUsesCompleteInvocationLease(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	code, err := Compile(cfg, gcDynamicPrivateReferenceResultModule())
	if err != nil {
		t.Fatal(err)
	}
	defer code.Close()
	if !compiledHasDynamicFuncrefReachability(code) || code.sharedGCPersistentDomainSafe() {
		t.Fatalf("dynamic private module admission = dynamic %v persistent-safe %v, want true/false", compiledHasDynamicFuncrefReachability(code), code.sharedGCPersistentDomainSafe())
	}
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	in, err := instantiateCore(code, InstantiateOptions{store: rt.refStore})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if rt.refStore.ownsGCCollector(in.gc) {
		t.Fatal("dynamic collector requiring private persistent roots unexpectedly joined the Runtime topology")
	}
	domain := in.gcInvocationDomain()
	if domain == nil || !domain.private || domain.collector != in.gc {
		t.Fatalf("dynamic private invocation domain = %#v, want instance collector", domain)
	}
	lease := in.lockGCInvocation(newInvocationID())
	collectDone := make(chan error, 1)
	go func() { collectDone <- in.CollectGC() }()
	select {
	case collectErr := <-collectDone:
		lease.unlock()
		t.Fatalf("CollectGC bypassed dynamic private complete-call lease: %v", collectErr)
	case <-time.After(50 * time.Millisecond):
	}
	lease.unlock()
	select {
	case collectErr := <-collectDone:
		if collectErr != nil {
			t.Fatal(collectErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CollectGC did not resume after dynamic private complete-call lease release")
	}
}

func TestGCAtomicWaitPrivateCollectorUsesCompleteInvocationLease(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	code, err := Compile(cfg, gcAtomicWaitReferenceResultModule())
	if err != nil {
		t.Fatal(err)
	}
	defer code.Close()
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	memory, err := NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	in, err := instantiateCore(code, InstantiateOptions{
		GC: GCConfig{}, store: rt.refStore, Imports: Imports{"env.memory": memory},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if rt.refStore.ownsGCCollector(in.gc) {
		t.Fatal("GC+Threads atomic-wait collector unexpectedly joined the Runtime topology")
	}
	domain := in.gcInvocationDomain()
	if domain == nil || domain.collector != in.gc {
		t.Fatalf("private collector invocation domain = %#v, want instance collector", domain)
	}
	lease := in.lockGCInvocation(newInvocationID())
	collectDone := make(chan error, 1)
	go func() { collectDone <- in.CollectGC() }()
	select {
	case collectErr := <-collectDone:
		lease.unlock()
		t.Fatalf("CollectGC bypassed private complete-call lease: %v", collectErr)
	case <-time.After(50 * time.Millisecond):
	}
	lease.unlock()
	select {
	case collectErr := <-collectDone:
		if collectErr != nil {
			t.Fatal(collectErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CollectGC did not resume after private complete-call lease release")
	}
	values, err := in.Invoke("new")
	if err != nil || len(values) != 1 || values[0] == 0 {
		t.Fatalf("private atomic-wait new = %v, %v; want one GC token", values, err)
	}
	if err := in.ReleaseGCRef(GCRef{token: values[0]}); err != nil {
		t.Fatal(err)
	}
}

func TestGCRefReleaseDoesNotInvertDynamicTopologyLock(t *testing.T) {
	if guardPageBuilt {
		t.Skip("signals-based execution uses instance-local native guards")
	}
	const childEnv = "WAGO_GC_REF_RELEASE_TOPOLOGY_CHILD"
	if os.Getenv(childEnv) == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGCRefReleaseDoesNotInvertDynamicTopologyLock$", "-test.count=1")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("GC reference release/topology lock-order child timed out: %v\n%s", ctx.Err(), out)
		}
		if err != nil {
			t.Fatalf("GC reference release/topology lock-order child failed: %v\n%s", err, out)
		}
		return
	}

	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	producerCode, err := Compile(cfg, gcReferenceTokenProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	relayCode, err := Compile(cfg, dynamicScalarRelayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCode.Close()
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	producer, err := instantiateCore(producerCode, InstantiateOptions{GC: GCConfig{}, store: rt.refStore})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	relay, err := instantiateCore(relayCode, InstantiateOptions{store: rt.refStore})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	values, err := producer.Invoke("new")
	if err != nil || len(values) != 1 || values[0] == 0 {
		t.Fatalf("producer new = %v, %v; want one non-null GC token", values, err)
	}
	ref := GCRef{token: values[0]}
	domain := producer.gcInvocationDomain()
	if domain == nil {
		t.Fatal("producer has no Runtime GC domain")
	}
	domain.mu.Lock()
	releaseDone := make(chan error, 1)
	go func() { releaseDone <- producer.ReleaseGCRef(ref) }()
	deadline := time.Now().Add(2 * time.Second)
	for nativeExecutionMu.TryLock() {
		nativeExecutionMu.Unlock()
		if time.Now().After(deadline) {
			domain.mu.Unlock()
			t.Fatal("GC reference release did not acquire the native guard")
		}
		time.Sleep(time.Millisecond)
	}
	invokeDone := make(chan error, 1)
	go func() {
		_, callErr := relay.Invoke("run")
		invokeDone <- callErr
	}()
	topology := rt.refStore.gcDomains
	if topology == nil {
		domain.mu.Unlock()
		t.Fatal("Runtime GC domain topology is unavailable")
	}
	for topology.TryLock() {
		topology.Unlock()
		if time.Now().After(deadline) {
			domain.mu.Unlock()
			t.Fatal("dynamic invocation did not acquire the topology read lease")
		}
		time.Sleep(time.Millisecond)
	}
	domain.mu.Unlock()
	select {
	case releaseErr := <-releaseDone:
		if releaseErr != nil {
			t.Fatal(releaseErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GC reference release deadlocked with dynamic invocation")
	}
	select {
	case callErr := <-invokeDone:
		if callErr != nil {
			t.Fatalf("dynamic invocation failed: %v", callErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dynamic invocation did not resume after GC reference release")
	}
}

func TestDynamicFuncrefHostResumeLeasesNewGCDomain(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	producerCode, err := Compile(cfg, gcAllocatingFuncrefProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	relayCode, err := Compile(cfg, dynamicFuncrefGlobalHostRelayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCode.Close()

	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer global.Close()

	type installedTarget struct {
		producer *Instance
		domain   *gcStoreDomain
	}
	installed := make(chan installedTarget, 1)
	var producer *Instance
	relay, err := instantiateCore(relayCode, InstantiateOptions{
		store: rt.refStore,
		Imports: Imports{
			"env.target": global,
			"env.install": HostFunc(func(HostModule, []uint64, []uint64) {
				var instantiateErr error
				producer, instantiateErr = instantiateCore(producerCode, InstantiateOptions{
					GC:    GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
					store: rt.refStore,
				})
				if instantiateErr != nil {
					panic(HostTrap{Err: instantiateErr})
				}
				ref, getErr := producer.Invoke("get")
				if getErr != nil || len(ref) != 1 {
					panic(HostTrap{Err: getErr})
				}
				if setErr := global.SetValue(ValueFuncRef(FuncRef{token: ref[0]})); setErr != nil {
					panic(HostTrap{Err: setErr})
				}
				domain := producer.gcInvocationDomain()
				if domain == nil {
					panic(HostTrap{Err: fmt.Errorf("new funcref producer has no Runtime GC domain")})
				}
				domain.invocationMu.Lock()
				installed <- installedTarget{producer: producer, domain: domain}
			})},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	type result struct {
		values []uint64
		err    error
	}
	done := make(chan result, 1)
	go func() {
		out, callErr := relay.Invoke("call")
		done <- result{values: out, err: callErr}
	}()
	var target installedTarget
	select {
	case target = <-installed:
	case <-time.After(5 * time.Second):
		t.Fatal("host callback did not install a new GC-domain funcref target")
	}
	select {
	case got := <-done:
		target.domain.invocationMu.Unlock()
		t.Fatalf("dynamic funcref call resumed without leasing the newly installed GC domain: %v, %v", got.values, got.err)
	case <-time.After(50 * time.Millisecond):
	}
	target.domain.invocationMu.Unlock()
	select {
	case got := <-done:
		if got.err != nil || len(got.values) != 1 || AsI32(got.values[0]) != 7 {
			t.Fatalf("dynamic funcref call after new-domain lease release = %v, %v; want [7], nil", got.values, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dynamic funcref call did not resume after new-domain lease release")
	}
	if target.producer != nil {
		if err := target.producer.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkDynamicFuncrefCrossGCDomain(b *testing.B) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	producerCode, err := Compile(cfg, gcAllocatingFuncrefProducerModule())
	if err != nil {
		b.Fatal(err)
	}
	defer producerCode.Close()
	relayCode, err := Compile(cfg, localFuncrefRelayModule())
	if err != nil {
		b.Fatal(err)
	}
	defer relayCode.Close()
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	producer, err := instantiateCore(producerCode, InstantiateOptions{GC: GCConfig{}, store: rt.refStore})
	if err != nil {
		b.Fatal(err)
	}
	defer producer.Close()
	relay, err := instantiateCore(relayCode, InstantiateOptions{store: rt.refStore})
	if err != nil {
		b.Fatal(err)
	}
	defer relay.Close()
	ref, err := producer.Invoke("get")
	if err != nil || len(ref) != 1 {
		b.Fatalf("producer get = %v, %v", ref, err)
	}
	if got, callErr := relay.Invoke("call", ref[0]); callErr != nil || len(got) != 1 || AsI32(got[0]) != 7 {
		b.Fatalf("warm dynamic call = %v, %v", got, callErr)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, callErr := relay.Invoke("call", ref[0]); callErr != nil || len(got) != 1 || AsI32(got[0]) != 7 {
			b.Fatalf("dynamic call = %v, %v", got, callErr)
		}
	}
}

func TestScalarCrossInstanceRelayWaitsForProducerGCInvocationLease(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	producerCode, err := Compile(cfg, gcAllocatingScalarProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	defer producerCode.Close()
	relayCode, err := Compile(cfg, scalarCrossInstanceRelayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayCode.Close()

	store := newReferenceStore(false)
	defer store.closeRuntime()
	gcCfg := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	producer, err := instantiateCore(producerCode, InstantiateOptions{GC: gcCfg, store: store})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("allocate")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := instantiateCore(relayCode, InstantiateOptions{store: store, Imports: Imports{"env.allocate": export}})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()

	type result struct {
		values []uint64
		err    error
	}
	for _, name := range []string{"direct", "wrapped"} {
		lease := producer.lockGCInvocation(newInvocationID())
		done := make(chan result, 1)
		go func(export string) {
			out, callErr := relay.Invoke(export)
			done <- result{values: out, err: callErr}
		}(name)
		select {
		case result := <-done:
			lease.unlock()
			t.Fatalf("relay %q completed while producer GC domain was leased: %v, %v", name, result.values, result.err)
		case <-time.After(50 * time.Millisecond):
		}
		lease.unlock()
		select {
		case result := <-done:
			if result.err != nil || len(result.values) != 1 || AsI32(result.values[0]) != 7 {
				t.Fatalf("relay %q after lease release = %v, %v; want [7], nil", name, result.values, result.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("relay %q did not resume after producer GC lease release", name)
		}
	}
}

func TestScalarCrossInstanceRelaySuspendsAllProducerGCDomainsForHostCollection(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	firstCode, err := Compile(cfg, gcAllocatingHostScalarProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	secondCode, err := Compile(cfg, gcAllocatingScalarProducerModule())
	if err != nil {
		firstCode.Close()
		t.Fatal(err)
	}
	relayCode, err := Compile(cfg, scalarTwoProducerRelayModule())
	if err != nil {
		firstCode.Close()
		secondCode.Close()
		t.Fatal(err)
	}
	store := newReferenceStore(false)
	var second *Instance
	first, err := instantiateCore(firstCode, InstantiateOptions{
		GC:    GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
		store: store,
		Imports: Imports{"env.host": HostFunc(func(module HostModule, _ []uint64, results []uint64) {
			if collectErr := second.CollectGC(); collectErr != nil {
				panic(HostTrap{Err: collectErr})
			}
			out, callErr := second.InvokeFromHost(context.Background(), module, "allocate")
			if callErr != nil || len(out) != 1 {
				panic(HostTrap{Err: callErr})
			}
			results[0] = out[0]
		})},
	})
	if err != nil {
		store.closeRuntime()
		firstCode.Close()
		secondCode.Close()
		relayCode.Close()
		t.Fatal(err)
	}
	second, err = instantiateCore(secondCode, InstantiateOptions{
		GC:    GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true},
		store: store,
	})
	if err != nil {
		first.Close()
		store.closeRuntime()
		firstCode.Close()
		secondCode.Close()
		relayCode.Close()
		t.Fatal(err)
	}
	if first.gcInvocationDomain() == nil || second.gcInvocationDomain() == nil || first.gcInvocationDomain() == second.gcInvocationDomain() {
		t.Fatal("test producers did not receive distinct Runtime GC domains")
	}
	firstExport, err := first.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	secondExport, err := second.ExportedFunc("allocate")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := instantiateCore(relayCode, InstantiateOptions{store: store, Imports: Imports{"env.first": firstExport, "env.second": secondExport}})
	if err != nil {
		t.Fatal(err)
	}
	if got := relay.gcInvocationDomains().len(); got != 2 {
		t.Fatalf("relay invocation domains = %d, want 2", got)
	}

	type result struct {
		values []uint64
		err    error
	}
	done := make(chan result, 1)
	go func() {
		out, callErr := relay.Invoke("run")
		done <- result{values: out, err: callErr}
	}()
	select {
	case result := <-done:
		if result.err != nil || len(result.values) != 1 || AsI32(result.values[0]) != 7 {
			t.Fatalf("two-domain relay host collection/re-entry = %v, %v; want [7], nil", result.values, result.err)
		}
	case <-time.After(5 * time.Second):
		// Cleanup would wait behind the deadlocked invocation and obscure the
		// bounded diagnostic on an implementation that suspends only the callee.
		t.Fatal("two-domain relay host collection/re-entry deadlocked")
	}
	if err := relay.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	store.closeRuntime()
	if err := firstCode.Close(); err != nil {
		t.Fatal(err)
	}
	if err := secondCode.Close(); err != nil {
		t.Fatal(err)
	}
	if err := relayCode.Close(); err != nil {
		t.Fatal(err)
	}
}
