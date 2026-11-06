//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcDomainHostRootModule() []byte {
	callback := append(append(wasmtest.Name("env"), wasmtest.Name("callback")...), 0, 1)
	body := []byte{
		1, 1, 0x63, 0, // one local of nullable struct-reference type zero
		0x20, 0, 0xfb, 0, 0, 0x21, 1, // struct.new 0; local.set 1
		0x10, 0, // call callback
		0x20, 1, 0xfb, 2, 0, 0, // local.get 1; struct.get 0 0
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 1, 0x7f, 0},
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(callback)),
		wasmtest.Section(3, []byte{1, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCDomainHostRootsAcrossReentry(t *testing.T) {
	for _, shift := range []uint8{0, 6} {
		t.Run(fmt.Sprintf("region=%d", shift), func(t *testing.T) {
			gcDomainHostRootsAcrossReentryPolicy(t, shift)
		})
	}
}
func gcDomainHostRootsAcrossReentryPolicy(t *testing.T, shift uint8) {
	runtime := NewRuntime()
	defer runtime.Close()
	compiled, err := Compile(gcDomainHostRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	module, err := runtime.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	config := GCConfig{NurseryBytes: 1024, SurvivorBytes: 1024, NurseryRegionShift: shift, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	callee, err := runtime.Instantiate(
		context.Background(), module,
		WithGC(config),
		WithImports(NewImports().Function("env", "callback", CallerHostCallFunc(func(Caller, HostCall) {}))),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer callee.Close()
	var nested []uint64
	caller, err := runtime.Instantiate(
		context.Background(), module,
		WithGC(config),
		WithImports(NewImports().Function("env", "callback", CallerHostCallFunc(func(host Caller, _ HostCall) {
			var invokeErr error
			nested, invokeErr = callee.InvokeFromHost(context.Background(), host, "run", 99)
			if invokeErr != nil {
				panic(invokeErr)
			}
		}))),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	got, err := caller.Invoke("run", 42)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(nested, []uint64{99}) || !slices.Equal(got, []uint64{42}) {
		t.Fatal("collector lost a parked same-domain frame root", nested, got)
	}
	stats := caller.gc.Stats()
	if stats.MinorCollections+stats.FullCollections == 0 {
		t.Fatal("no collection", stats)
	}
	if actual := reflect.ValueOf(caller.gc).Elem().FieldByName("cfg").FieldByName("NurseryRegionShift").Uint(); actual != uint64(shift) {
		t.Fatal("wrong actual mature-region policy", actual)
	}
	if state := caller.existingPublicGCState(); state != nil && state.hostActivationCount != 0 {
		t.Fatal("leaked host activation")
	}

}
