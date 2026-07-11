package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestCompiledSignatureReportsImportedFunctionReexport(t *testing.T) {
	c, err := Compile(nil, importedFunctionReexportModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer c.Close()

	params, results, err := c.Signature("forward")
	if err != nil {
		t.Fatalf("Signature forward: %v", err)
	}
	if len(params) != 1 || params[0] != ValI32 || len(results) != 1 || results[0] != ValI32 {
		t.Fatalf("Signature forward = (%v) -> (%v), want (i32) -> (i32)", params, results)
	}
}

func TestImportedFunctionReexportForwardsInvokeCallTrapAndState(t *testing.T) {
	rt, producer, relay := instantiateImportedFunctionReexport(t)
	defer closeImportedFunctionReexport(t, rt, producer, relay)

	got, err := relay.Invoke("forward", I32(7))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 7 {
		t.Fatalf("Invoke forward(7) = %v, %v; want 7", got, err)
	}
	state, err := producer.Invoke("state")
	if err != nil || len(state) != 1 || AsI32(state[0]) != 7 {
		t.Fatalf("producer state after forwarding = %v, %v; want 7", state, err)
	}

	typed, err := relay.Call(context.Background(), "forward", ValueI32(9))
	if err != nil || len(typed) != 1 || typed[0].Type() != ValI32 || typed[0].I32() != 9 {
		t.Fatalf("Call forward(9) = %v, %v; want i32(9)", typed, err)
	}

	if out, err := relay.Invoke("forward", I32(-1)); err == nil || out != nil {
		t.Fatalf("Invoke forward(-1) = %v, %v; want forwarded trap", out, err)
	}
	state, err = producer.Invoke("state")
	if err != nil || len(state) != 1 || AsI32(state[0]) != -1 {
		t.Fatalf("producer state after forwarded trap = %v, %v; want -1", state, err)
	}
}

func TestImportedFunctionReexportCanLinkAgain(t *testing.T) {
	rt, producer, relay := instantiateImportedFunctionReexport(t)
	defer closeImportedFunctionReexport(t, rt, producer, relay)

	forward, err := relay.ExportedFunc("forward")
	if err != nil {
		t.Fatalf("ExportedFunc forward: %v", err)
	}
	consumerMod, err := rt.Compile(importedFunctionReexportModule())
	if err != nil {
		t.Fatalf("Compile consumer: %v", err)
	}
	consumer, err := rt.Instantiate(nil, consumerMod, WithImports(Imports{"env.step": forward}))
	if err != nil {
		t.Fatalf("Instantiate consumer: %v", err)
	}
	defer consumer.Close()

	got, err := consumer.Invoke("forward", I32(11))
	if err != nil || len(got) != 1 || AsI32(got[0]) != 11 {
		t.Fatalf("consumer forward(11) = %v, %v; want 11", got, err)
	}
}

func TestHostImportedFunctionReexportStaysFailClosed(t *testing.T) {
	c, err := Compile(nil, importedFunctionReexportModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{
		"env.step": HostFunc(func(_ HostModule, params, results []uint64) { results[0] = params[0] }),
	}})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()

	if _, err := in.ExportedFunc("forward"); err == nil || !strings.Contains(err.Error(), "imported function") {
		t.Fatalf("ExportedFunc host reexport error = %v, want explicit fail-closed rejection", err)
	}
}

func instantiateImportedFunctionReexport(t testing.TB) (*Runtime, *Instance, *Instance) {
	t.Helper()
	rt := NewRuntime()
	producerMod, err := rt.Compile(reexportProducerModule())
	if err != nil {
		t.Fatalf("Compile producer: %v", err)
	}
	producer, err := rt.Instantiate(nil, producerMod)
	if err != nil {
		t.Fatalf("Instantiate producer: %v", err)
	}
	step, err := producer.ExportedFunc("step")
	if err != nil {
		t.Fatalf("Export producer step: %v", err)
	}
	relayMod, err := rt.Compile(importedFunctionReexportModule())
	if err != nil {
		t.Fatalf("Compile relay: %v", err)
	}
	relay, err := rt.Instantiate(nil, relayMod, WithImports(Imports{"env.step": step}))
	if err != nil {
		t.Fatalf("Instantiate relay: %v", err)
	}
	return rt, producer, relay
}

func closeImportedFunctionReexport(t testing.TB, rt *Runtime, producer, relay *Instance) {
	t.Helper()
	if err := relay.Close(); err != nil {
		t.Errorf("close relay: %v", err)
	}
	if err := producer.Close(); err != nil {
		t.Errorf("close producer after relay: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Errorf("close runtime: %v", err)
	}
}

func importedFunctionReexportModule() []byte {
	imp := append(wasmtest.Name("env"), wasmtest.Name("step")...)
	imp = append(imp, 0x00, 0x00) // function import, type 0
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("forward", 0, 0))),
	)
}

func reexportProducerModule() []byte {
	stepBody := []byte{0x20, 0x00, 0x24, 0x00, 0x20, 0x00, 0x41}
	stepBody = append(stepBody, wasmtest.SLEB32(-1)...)
	stepBody = append(stepBody,
		0x46,       // i32.eq
		0x04, 0x40, // if (no result)
		0x00,       // unreachable
		0x0b,       // end if
		0x23, 0x00, // global.get 0
		0x0b, // end function
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("step", 0, 0),
			wasmtest.ExportEntry("state", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(stepBody),
			wasmtest.Code([]byte{0x23, 0x00, 0x0b}),
		)),
	)
}
