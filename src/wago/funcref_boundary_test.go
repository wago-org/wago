package wago

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestPublicFuncrefIngressRejectsForgedNonNullBeforeNativeExecution(t *testing.T) {
	tests := []struct {
		name string
		call func(*Instance, uint64) ([]uint64, error)
	}{
		{
			name: "Invoke",
			call: func(in *Instance, forged uint64) ([]uint64, error) {
				return in.Invoke("sink", forged)
			},
		},
		{
			name: "Call",
			call: func(in *Instance, forged uint64) ([]uint64, error) {
				out, err := in.Call(context.Background(), "sink", ValueOf(ValFuncRef, forged))
				if out != nil {
					return []uint64{1}, err
				}
				return nil, err
			},
		},
		{
			name: "invokeLocal",
			call: func(in *Instance, forged uint64) ([]uint64, error) {
				return in.invokeLocal(0, []uint64{forged})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := instantiateFuncrefBoundaryTestModule(t, funcrefIngressBoundaryModule())
			defer in.Close()
			if len(in.funcRefDescs) < 2*coreruntime.TableEntryBytes {
				t.Fatalf("funcref descriptor arena = %d bytes, want at least two entries", len(in.funcRefDescs))
			}
			forged := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[coreruntime.TableEntryBytes])))

			out, err := tc.call(in, forged)
			if err == nil || !strings.Contains(err.Error(), "invalid funcref token") {
				t.Fatalf("forged funcref call = %v, %v; want public-boundary rejection", out, err)
			}
			if out != nil {
				t.Fatalf("forged funcref call returned %v, want nil", out)
			}
			if marker, markerErr := in.Global("marker"); markerErr != nil || AsI32(marker) != 0 {
				t.Fatalf("marker after rejected call = %v, %v; want 0 (native body not entered)", marker, markerErr)
			}
		})
	}
}

func TestPublicFuncrefEgressReturnsStableOpaqueToken(t *testing.T) {
	in := instantiateFuncrefBoundaryTestModule(t, funcrefEgressBoundaryModule())
	defer in.Close()

	first, err := in.Invoke("get")
	if err != nil || len(first) != 1 || first[0] == 0 {
		t.Fatalf("Invoke get = %v, %v; want one non-null token", first, err)
	}
	token := first[0]
	if len(in.funcRefDescs) < 2*coreruntime.TableEntryBytes {
		t.Fatalf("funcref descriptor arena = %d bytes, want at least two entries", len(in.funcRefDescs))
	}
	descriptor := uint64(uintptr(unsafe.Pointer(&in.funcRefDescs[coreruntime.TableEntryBytes])))
	code := *(*uint64)(unsafe.Pointer(&in.funcRefDescs[coreruntime.TableEntryBytes]))
	if token == descriptor || token == code || token == uint64(in.base) {
		t.Fatalf("public token %#x aliases descriptor/code mapping %#x/%#x/%#x", token, descriptor, code, in.base)
	}

	second, err := in.Invoke("get")
	if err != nil || len(second) != 1 || second[0] != token {
		t.Fatalf("second Invoke get = %v, %v; want stable token %#x", second, err, token)
	}
	typed, err := in.Call(context.Background(), "get")
	if err != nil || len(typed) != 1 || typed[0].Type() != ValFuncRef || typed[0].Bits() != token {
		t.Fatalf("Call get = %v, %v; want typed token %#x", typed, err, token)
	}
	local, err := in.invokeLocal(1, nil)
	if err != nil || len(local) != 1 || local[0] != token {
		t.Fatalf("invokeLocal get = %v, %v; want token %#x", local, err, token)
	}
}

func TestRuntimeFuncrefTokenRoundTripsAndRetainsProducer(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	producerMod, err := rt.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	consumerMod, err := rt.Compile(funcrefCallableConsumerModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerMod)
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()
	relayMod, err := rt.Compile(nullableFuncrefModule())
	if err != nil {
		t.Fatalf("Compile relay: %v", err)
	}
	relay, err := rt.Instantiate(context.Background(), relayMod)
	if err != nil {
		t.Fatalf("Instantiate relay: %v", err)
	}
	defer relay.Close()

	out, err := producer.Call(context.Background(), "get")
	if err != nil || len(out) != 1 || out[0].FuncRef().IsNull() {
		t.Fatalf("producer get = %v, %v; want non-null funcref", out, err)
	}
	token := out[0]
	if got, err := relay.Call(context.Background(), "id", token); err != nil || len(got) != 1 || got[0].Bits() != token.Bits() {
		t.Fatalf("same-runtime relay = %v, %v; want stable token %#x", got, err, token.Bits())
	}
	if got, err := consumer.Call(context.Background(), "call", token); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("consumer call before producer close = %v, %v; want 42", got, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close runtime with live instances: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close producer: %v", err)
	}
	if got, err := consumer.Call(context.Background(), "call", token); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("consumer call after producer close = %v, %v; want retained 42", got, err)
	}
}

func TestRuntimeImportedFuncrefUsesProducerIdentityAndLifetime(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	producerMod, err := rt.Compile(funcrefImportedProducerModule())
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	target, err := producer.ExportedFunc("target")
	if err != nil {
		t.Fatalf("Export target: %v", err)
	}
	importerMod, err := rt.Compile(funcrefImportedRefFuncModule())
	if err != nil {
		t.Fatalf("Compile importer: %v", err)
	}
	importer, err := rt.Instantiate(context.Background(), importerMod, WithImports(Imports{"env.target": target}))
	if err != nil {
		t.Fatalf("Instantiate importer: %v", err)
	}
	defer importer.Close()
	consumerMod, err := rt.Compile(funcrefCallableConsumerModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerMod)
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()

	imported, err := importer.Invoke("get")
	if err != nil || len(imported) != 1 || imported[0] == 0 {
		t.Fatalf("importer get = %v, %v; want one non-null token", imported, err)
	}
	local, err := producer.Invoke("get")
	if err != nil || len(local) != 1 || local[0] != imported[0] {
		t.Fatalf("producer get = %v, %v; want imported identity %#x", local, err, imported[0])
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("Close producer: %v", err)
	}
	if got, err := importer.invokeLocal(0, nil); err != nil || len(got) != 1 || got[0] != imported[0] {
		t.Fatalf("imported invokeLocal after producer close = %v, %v; want token %#x", got, err, imported[0])
	}
	if got, err := consumer.Invoke("call", imported[0]); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("consumer call after producer close = %v, %v; want 42", got, err)
	}
}

func TestRuntimeImportedFuncrefRejectsForeignOrCorruptCanonicalDescriptor(t *testing.T) {
	t.Run("cross-runtime", func(t *testing.T) {
		producerRT := NewRuntime()
		defer producerRT.Close()
		producerMod, err := producerRT.Compile(funcrefImportedProducerModule())
		if err != nil {
			t.Fatalf("Compile producer: %v", err)
		}
		producer, err := producerRT.Instantiate(context.Background(), producerMod)
		if err != nil {
			t.Fatalf("Instantiate producer: %v", err)
		}
		defer producer.Close()
		target, err := producer.ExportedFunc("target")
		if err != nil {
			t.Fatalf("Export target: %v", err)
		}

		importerRT := NewRuntime()
		defer importerRT.Close()
		importerMod, err := importerRT.Compile(funcrefImportedRefFuncModule())
		if err != nil {
			t.Fatalf("Compile importer: %v", err)
		}
		importer, err := importerRT.Instantiate(context.Background(), importerMod, WithImports(Imports{"env.target": target}))
		if err != nil {
			t.Fatalf("Instantiate importer: %v", err)
		}
		defer importer.Close()

		got, err := importer.Invoke("get")
		if err == nil || !strings.Contains(err.Error(), "invalid funcref result") || got != nil {
			t.Fatalf("cross-runtime imported get = %v, %v; want fail-closed result", got, err)
		}
		if len(importerRT.refStore.byToken) != 0 || len(importerRT.refStore.byDescriptor) != 0 {
			t.Fatal("cross-runtime rejection issued a public token")
		}
	})

	t.Run("corrupt-ref-slot", func(t *testing.T) {
		rt := NewRuntime()
		defer rt.Close()
		producerMod, err := rt.Compile(funcrefImportedProducerModule())
		if err != nil {
			t.Fatalf("Compile producer: %v", err)
		}
		producer, err := rt.Instantiate(context.Background(), producerMod)
		if err != nil {
			t.Fatalf("Instantiate producer: %v", err)
		}
		defer producer.Close()
		target, err := producer.ExportedFunc("target")
		if err != nil {
			t.Fatalf("Export target: %v", err)
		}
		importerMod, err := rt.Compile(funcrefImportedRefFuncModule())
		if err != nil {
			t.Fatalf("Compile importer: %v", err)
		}
		importer, err := rt.Instantiate(context.Background(), importerMod, WithImports(Imports{"env.target": target}))
		if err != nil {
			t.Fatalf("Instantiate importer: %v", err)
		}
		defer importer.Close()

		importedOff := coreruntime.TableEntryBytes
		canonical := binary.LittleEndian.Uint64(importer.funcRefDescs[importedOff+coreruntime.TableEntryRefSlotOffset:])
		binary.LittleEndian.PutUint64(importer.funcRefDescs[importedOff+coreruntime.TableEntryRefSlotOffset:], canonical+8)
		got, err := importer.Invoke("get")
		if err == nil || !strings.Contains(err.Error(), "invalid funcref result") || got != nil {
			t.Fatalf("corrupt imported get = %v, %v; want fail-closed result", got, err)
		}
		if len(rt.refStore.byToken) != 0 || len(rt.refStore.byDescriptor) != 0 {
			t.Fatal("corrupt canonical descriptor issued a public token")
		}
	})
}

func TestRuntimeFuncrefTokenRejectsForgedAndCrossRuntimeBeforeExecution(t *testing.T) {
	producerRT := NewRuntime()
	defer producerRT.Close()
	producerMod, err := producerRT.Compile(funcrefCallableProducerModule())
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	producer, err := producerRT.Instantiate(context.Background(), producerMod)
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	defer producer.Close()
	out, err := producer.Invoke("get")
	if err != nil || len(out) != 1 || out[0] == 0 {
		t.Fatalf("producer get = %v, %v; want non-null token", out, err)
	}
	token := out[0]

	consumerRT := NewRuntime()
	defer consumerRT.Close()
	consumerMod, err := consumerRT.Compile(funcrefCallableConsumerModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	consumer, err := consumerRT.Instantiate(context.Background(), consumerMod)
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()

	for name, candidate := range map[string]uint64{
		"cross-runtime": token,
		"forged":        token ^ 0xa5a5a5a5a5a5a5a5,
	} {
		t.Run(name, func(t *testing.T) {
			if candidate == 0 || candidate == token && name == "forged" {
				t.Fatalf("bad test token %#x", candidate)
			}
			got, err := consumer.Invoke("call", candidate)
			if err == nil || !strings.Contains(err.Error(), "invalid funcref token") {
				t.Fatalf("consumer call(%#x) = %v, %v; want invalid-token rejection", candidate, got, err)
			}
			if marker, markerErr := consumer.Global("marker"); markerErr != nil || AsI32(marker) != 0 {
				t.Fatalf("marker after rejected call = %v, %v; want 0", marker, markerErr)
			}
		})
	}
}

func TestStandaloneFuncrefStoreIsLazy(t *testing.T) {
	scalar := instantiateFuncrefBoundaryTestModule(t, benchAddOneModule())
	defer scalar.Close()
	if _, err := scalar.Invoke("f", I32(1)); err != nil {
		t.Fatalf("scalar Invoke: %v", err)
	}
	if scalar.refStore != nil {
		t.Fatal("scalar standalone instance allocated a reference store")
	}

	producer := instantiateFuncrefBoundaryTestModule(t, funcrefCallableProducerModule())
	defer producer.Close()
	if producer.refStore != nil {
		t.Fatal("funcref producer allocated a private store before non-null egress")
	}
	if _, err := producer.Invoke("get"); err != nil {
		t.Fatalf("producer get: %v", err)
	}
	if producer.refStore == nil || !producer.refStore.private {
		t.Fatal("non-null egress did not create a private reference store")
	}
}

func TestStandaloneFuncrefTokenUsesPrivateStore(t *testing.T) {
	producer := instantiateFuncrefBoundaryTestModule(t, funcrefCallableProducerModule())
	defer producer.Close()
	consumer := instantiateFuncrefBoundaryTestModule(t, funcrefCallableConsumerModule())
	defer consumer.Close()

	out, err := producer.Invoke("get")
	if err != nil || len(out) != 1 || out[0] == 0 {
		t.Fatalf("producer get = %v, %v; want non-null token", out, err)
	}
	token := out[0]
	if got, err := producer.Invoke("call", token); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("same private-store call = %v, %v; want 42", got, err)
	}
	if got, err := consumer.Invoke("call", token); err == nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatalf("different private-store call = %v, %v; want rejection", got, err)
	}
}

func TestInvokeCacheTracksFuncrefBoundaryChecks(t *testing.T) {
	scalar := instantiateFuncrefBoundaryTestModule(t, benchAddOneModule())
	defer scalar.Close()
	if _, err := scalar.Invoke("f", I32(1)); err != nil {
		t.Fatalf("Invoke scalar: %v", err)
	}
	scalarCache := scalar.findInvokeCache("f")
	if scalarCache == nil || scalarCache.hasFuncRefParams || scalarCache.hasFuncRefResults {
		t.Fatalf("scalar invoke cache = %#v, want no funcref boundary checks", scalarCache)
	}

	reference := instantiateFuncrefBoundaryTestModule(t, nullableFuncrefModule())
	defer reference.Close()
	if _, err := reference.Invoke("id", 0); err != nil {
		t.Fatalf("Invoke id(null): %v", err)
	}
	referenceCache := reference.findInvokeCache("id")
	if referenceCache == nil || !referenceCache.hasFuncRefParams || !referenceCache.hasFuncRefResults {
		t.Fatalf("funcref invoke cache = %#v, want parameter and result boundary checks", referenceCache)
	}
}

func TestPublicFuncrefBoundaryContinuesToAcceptNull(t *testing.T) {
	in := instantiateFuncrefBoundaryTestModule(t, funcrefIngressBoundaryModule())
	defer in.Close()

	if out, err := in.Invoke("sink", 0); err != nil || len(out) != 0 {
		t.Fatalf("Invoke sink(null) = %v, %v; want success", out, err)
	}
	if marker, err := in.Global("marker"); err != nil || AsI32(marker) != 1 {
		t.Fatalf("marker after sink(null) = %v, %v; want 1", marker, err)
	}
}

func instantiateFuncrefBoundaryTestModule(t *testing.T, mod []byte) *Instance {
	t.Helper()
	c, err := Compile(nil, mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	in, err := Instantiate(c)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	return in
}

func funcrefIngressBoundaryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})), // funcref table min=1 max=1
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("sink", 0, 0),
			wasmtest.ExportEntry("marker", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x01, // i32.const 1
			0x24, 0x00, // global.set 0
			0x41, 0x00, // i32.const 0
			0x20, 0x00, // local.get 0
			0x26, 0x00, // table.set 0
			0x0b,
		}))),
	)
}

func funcrefCallableProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})), // funcref table min=1 max=1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("call", 0, 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))), // declarative func 0
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}), // target() -> 42
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}), // ref.func 0
			wasmtest.Code([]byte{0x41, 0x00, 0x20, 0x00, 0x26, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
}

func funcrefImportedProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x00, 0x00})), // funcref table min=0 max=0
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))), // declarative func 0
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
		)),
	)
}

func funcrefImportedRefFuncModule() []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("target")...)
	imp = append(imp, 0x00, 0x00) // function import, type 0
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x00, 0x00})), // funcref table min=0 max=0
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))), // declare imported func 0
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xd2, 0x00, 0x0b}))),
	)
}

func funcrefCallableConsumerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x01, 0x01})), // funcref table min=1 max=1
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call", 0, 0),
			wasmtest.ExportEntry("marker", 3, 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x01, 0x24, 0x00, // marker = 1 (must not run for rejected tokens)
			0x41, 0x00, 0x20, 0x00, 0x26, 0x00, // table.set 0
			0x41, 0x00, 0x11, 0x00, 0x00, // call_indirect type 0 table 0
			0x0b,
		}))),
	)
}

func funcrefEgressBoundaryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x01, 0x00, 0x00})), // funcref table min=0 max=0
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...))), // declarative func 0
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}), // ref.func 0
		)),
	)
}
