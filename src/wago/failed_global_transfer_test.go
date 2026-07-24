package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func funcrefTokenProducerModule(value int32) []byte {
	body := append([]byte{0x41}, wasmtest.SLEB32(value)...)
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(body),
			wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
		)),
	)
}

func mutableFuncrefGlobalImport(module, name string) []byte {
	entry := append(wasmtest.Name(module), wasmtest.Name(name)...)
	return append(entry, 0x03, 0x70, 0x01)
}

func initErrorWithImportedFunctionAndGlobalModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			portableFuncImportEntry("producer", "target", 0),
			mutableFuncrefGlobalImport("env", "global"),
		)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x00})),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, 0))), // OOB: table size is zero
	)
}

func startTrapStoresLocalFuncrefGlobalModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(mutableFuncrefGlobalImport("env", "global"))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(8, wasmtest.ULEB(1)),
		wasmtest.Section(9, wasmtest.Vec(tableTestDeclarativeElem(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(append(append([]byte{0x41}, wasmtest.SLEB32(66)...), 0x0b)),
			wasmtest.Code([]byte{0xd2, 0x00, 0x24, 0x00, 0x00, 0x0b}),
		)),
	)
}

func funcrefGlobalCallerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(mutableFuncrefGlobalImport("env", "global"))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, 0x23, 0x00, 0x26, 0x00, // table.set 0, global value
			0x41, 0x00, 0x11, 0x00, 0x00, 0x0b,
		}))),
	)
}

func funcrefGlobalScannerModule() []byte {
	return wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(mutableFuncrefGlobalImport("env", "global"))))
}

func retainedGlobalRoot(t *testing.T, global *Global) *Instance {
	t.Helper()
	global.owner.mu.Lock()
	defer global.owner.mu.Unlock()
	if len(global.owner.retained) != 1 {
		t.Fatalf("retained global roots = %d, want 1", len(global.owner.retained))
	}
	for root := range global.owner.retained {
		return root
	}
	return nil
}

func scanGlobalAfterOverwrite(t *testing.T, rt *Runtime, global *Global) {
	t.Helper()
	code := MustCompile(funcrefGlobalScannerModule())
	defer code.Close()
	in, err := Instantiate(code, InstantiateOptions{Imports: Imports{"env.global": global}, store: rt.refStore})
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFailedInstantiationTransfersImportedGlobalRoots(t *testing.T) {
	t.Run("initialization error", func(t *testing.T) {
		rt := NewRuntime()
		producerMod, err := rt.Compile(funcrefTokenProducerModule(55))
		if err != nil {
			t.Fatal(err)
		}
		producer, err := rt.Instantiate(context.Background(), producerMod)
		if err != nil {
			t.Fatal(err)
		}
		values, err := producer.Call(context.Background(), "get")
		if err != nil || len(values) != 1 {
			t.Fatalf("producer get = %v, %v", values, err)
		}
		global, err := rt.NewFuncRefGlobal(values[0].FuncRef(), true)
		if err != nil {
			t.Fatal(err)
		}
		export, _ := producer.ExportedFunc("target")
		failedCode := MustCompile(initErrorWithImportedFunctionAndGlobalModule())
		defer failedCode.Close()
		if in, err := Instantiate(failedCode, InstantiateOptions{Imports: Imports{"producer.target": export, "env.global": global}, store: rt.refStore}); err == nil || in != nil {
			t.Fatalf("initialization error instantiate = %p, %v", in, err)
		}
		root := retainedGlobalRoot(t, global)
		if root.resourcesClosed {
			t.Fatal("failed initialization root was physically released")
		}

		callerMod, _ := rt.Compile(funcrefGlobalCallerModule())
		caller, err := rt.Instantiate(context.Background(), callerMod, WithImports(Imports{"env.global": global}))
		if err != nil {
			t.Fatal(err)
		}
		got, err := caller.Call(context.Background(), "call")
		if err != nil || len(got) != 1 || got[0].I32() != 55 {
			t.Fatalf("call retained init-error reference = %v, %v; want 55", got, err)
		}
		_ = caller.Close()
		_ = global.SetValue(ValueFuncRef(NullFuncRef()))
		scanGlobalAfterOverwrite(t, rt, global)
		global.owner.mu.Lock()
		retained := len(global.owner.retained)
		global.owner.mu.Unlock()
		if retained != 0 {
			t.Fatalf("overwriting init-error global left %d retained container root(s)", retained)
		}
		_ = global.Close()
		_ = producer.Close()
		_ = rt.Close()
		if !root.resourcesClosed {
			t.Fatal("producer remained live after container and token roots were released")
		}
	})

	t.Run("start trap", func(t *testing.T) {
		rt := NewRuntime()
		global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
		if err != nil {
			t.Fatal(err)
		}
		failedCode := MustCompile(startTrapStoresLocalFuncrefGlobalModule())
		defer failedCode.Close()
		if in, err := Instantiate(failedCode, InstantiateOptions{Imports: Imports{"env.global": global}, store: rt.refStore}); err == nil || in != nil {
			t.Fatalf("start-trap instantiate = %p, %v", in, err)
		}
		root := retainedGlobalRoot(t, global)
		callerMod, _ := rt.Compile(funcrefGlobalCallerModule())
		caller, err := rt.Instantiate(context.Background(), callerMod, WithImports(Imports{"env.global": global}))
		if err != nil {
			t.Fatal(err)
		}
		got, err := caller.Call(context.Background(), "call")
		if err != nil || len(got) != 1 || got[0].I32() != 66 {
			t.Fatalf("call retained start-trap reference = %v, %v; want 66", got, err)
		}
		_ = caller.Close()
		_ = global.SetValue(ValueFuncRef(NullFuncRef()))
		scanGlobalAfterOverwrite(t, rt, global)
		if !root.resourcesClosed {
			t.Fatal("overwriting start-trap global reference did not release root")
		}
		_ = global.Close()
		_ = rt.Close()
	})
}
