//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func stagedTypedTailCompile(t testing.TB, module []byte) *Compiled {
	t.Helper()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.TypedTailCalls = true
	compiled, err := compileWithFrontendFeatures(cfg, module, features)
	if err != nil {
		t.Fatalf("staged typed-tail compile: %v", err)
	}
	t.Cleanup(func() { _ = compiled.Close() })
	return compiled
}

func typedCrossTailProducerModule() []byte {
	declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	body := []byte{
		0x20, 0x00, 0x45, // local.get 0; i32.eqz
		0x04, 0x7f, // if (result i32)
		0x41, 0x07, // i32.const 7
		0x05,                         // else
		0x20, 0x00, 0x41, 0x01, 0x6b, // n - 1
		0xd2, 0x00, // ref.func 0
		0x15, 0x00, // return_call_ref type 0
		0x0b, // end if
		0x0b, // end function
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func typedCrossTailConsumerModule() []byte {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc))
	importEntry = append(importEntry, wasmtest.ULEB(1)...)
	declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 1),
			wasmtest.ExportEntry("nested", 0, 2),
			wasmtest.ExportEntry("null", 0, 3),
		)),
		wasmtest.Section(9, wasmtest.Vec(declared)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0xd2, 0x00, 0x15, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x41, 0x05, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd0, 0x01, 0x15, 0x01, 0x0b}),
		)),
	)
}

func instantiateTypedCrossTail(t testing.TB) (*Instance, *Instance) {
	t.Helper()
	producerCompiled := stagedTypedTailCompile(t, typedCrossTailProducerModule())
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate typed-tail producer: %v", err)
	}
	export, err := producer.ExportedFunc("f")
	if err != nil {
		producer.Close()
		t.Fatalf("export typed-tail producer: %v", err)
	}
	consumerCompiled := stagedTypedTailCompile(t, typedCrossTailConsumerModule())
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.f": export}})
	if err != nil {
		producer.Close()
		t.Fatalf("instantiate typed-tail consumer: %v", err)
	}
	return producer, consumer
}

func TestStagedTypedCrossInstanceReturnCallRefRootTransfer(t *testing.T) {
	if _, err := Compile(nil, typedCrossTailConsumerModule()); err == nil || !strings.Contains(err.Error(), "typed") {
		t.Fatalf("public typed-tail compile error = %v, want fail-closed feature rejection", err)
	}
	producer, consumer := instantiateTypedCrossTail(t)

	if _, err := consumer.Invoke("run", I32(1)); err == nil || !strings.Contains(err.Error(), "unsupported context switch") {
		t.Fatalf("cross-instance typed-tail context error = %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("logical producer close: %v", err)
	}
	producer.lifeMu.Lock()
	producerReleased := producer.resourcesClosed
	producer.lifeMu.Unlock()
	if producerReleased {
		t.Fatal("cross-tail consumer did not retain producer resources")
	}
	if _, err := consumer.Invoke("run", I32(1)); err == nil || !strings.Contains(err.Error(), "unsupported context switch") {
		t.Fatalf("cross-tail error after producer logical close = %v", err)
	}

	if _, err := consumer.Invoke("nested", I32(1)); err == nil || !strings.Contains(err.Error(), "unsupported context switch") {
		t.Fatalf("nested cross-tail context error = %v", err)
	}
	if _, err := consumer.Invoke("null", I32(1)); err == nil || !strings.Contains(err.Error(), "indirect call out of bounds") {
		t.Fatalf("null cross-tail error = %v", err)
	}

	desc := consumer.funcRefDescs[runtime.FuncRefDescBytes : 2*runtime.FuncRefDescBytes]
	oldKey := binary.LittleEndian.Uint64(desc[runtime.TableEntrySigKeyOffset:])
	binary.LittleEndian.PutUint64(desc[runtime.TableEntrySigKeyOffset:], oldKey+1)
	if _, err := consumer.Invoke("run", I32(1)); err == nil || !strings.Contains(err.Error(), "wrong signature") {
		t.Fatalf("wrong-key cross-tail error = %v", err)
	}
	binary.LittleEndian.PutUint64(desc[runtime.TableEntrySigKeyOffset:], oldKey)
	if _, err := consumer.Invoke("run", I32(1)); err == nil || !strings.Contains(err.Error(), "unsupported context switch") {
		t.Fatalf("cross tail did not recover to fail-closed context trap: %v", err)
	}

	rt := NewRuntime()
	host, err := rt.NewHostFuncRef(HostFunc(func(_ HostModule, args, results []uint64) {
		results[0] = args[0] + 1
	}), FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
	if err != nil {
		t.Fatalf("create host funcref: %v", err)
	}
	hostConsumerCompiled := stagedTypedTailCompile(t, typedCrossTailConsumerModule())
	hostConsumer, err := instantiateCore(hostConsumerCompiled, InstantiateOptions{Imports: Imports{"env.f": host}, store: rt.refStore})
	if err != nil {
		t.Fatalf("instantiate host typed-tail consumer: %v", err)
	}
	if _, err := hostConsumer.Invoke("run", I32(1)); err == nil || !strings.Contains(err.Error(), "unsupported context switch") {
		t.Fatalf("host cross-tail context error = %v", err)
	}
	if err := hostConsumer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}

	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	producer.lifeMu.Lock()
	producerReleased = producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !producerReleased {
		t.Fatal("producer resources remained retained after cross-tail consumer close")
	}
}
