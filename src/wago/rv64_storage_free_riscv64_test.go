//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64StorageFreeCallableAfterDependencyTransfer(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			a := rv64NativeTokenInstantiate(t, rt, rv64NativeTailCompile(t, "storage-free-transfer-provider", prefinalizerTableMemorySource(false), artifact), nil)
			defer a.Close()
			memory, err := a.ExportedMemory("memory")
			if err != nil {
				t.Fatal(err)
			}
			table, err := a.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			b := rv64NativeTokenInstantiate(t, rt, rv64NativeTailCompile(t, "storage-free-transfer-callee", frozenPrefinalizerSource(t), artifact), NewImports().Memory("env", "memory", memory).Table("env", "table", table))
			defer b.Close()
			target, err := b.ExportedFunc("safe")
			if err != nil {
				t.Fatal(err)
			}
			imp := append(append(wasmtest.Name("env"), wasmtest.Name("safe")...), 0, 0)
			source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
			caller := rv64NativeTokenInstantiate(t, rt, rv64NativeTailCompile(t, "storage-free-transfer-caller", source, artifact), NewImports().Function("env", "safe", target))
			defer caller.Close()
			a.Close()
			b.Close()
			if !a.resourcesClosed || b.resourcesClosed {
				t.Fatal("storage-free transfer did not release only unused storage")
			}
			got, err := caller.Invoke("run")
			if err != nil || len(got) != 1 || got[0] != 17 {
				t.Fatalf("retained storage-free callable=%v,%v", got, err)
			}
		})
	}
}

func TestRV64StorageFreeBodyProof(t *testing.T) {
	source := frozenPrefinalizerSource(t)
	for _, tc := range []struct {
		name string
		body []byte
		want bool
	}{
		{"constant", []byte{0x41, 17, 0x0b}, true},
		{"numeric", []byte{0x41, 17, 0x41, 1, 0x6a, 0x0b}, true},
		{"memory", []byte{0x41, 0, 0x28, 2, 0, 0x0b}, false},
		{"table", []byte{0x41, 0, 0x25, 0, 0x1a, 0x41, 17, 0x0b}, false},
		{"global", []byte{0x23, 0, 0x1a, 0x41, 17, 0x0b}, false},
		{"local call", []byte{0x10, 0, 0x0b}, false},
		{"indirect call", []byte{0x41, 0, 0x11, 0, 0, 0x0b}, false},
		{"reference", []byte{0xd2, 0, 0x1a, 0x41, 17, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(tc.body))))
			m, err := wasm.DecodeModule(input)
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{GCConstExpr: true, ExtendedConstGlobals: true, MultiMemory: true}, 2, wasm.ValidationLimits{}); err != nil {
				t.Fatal(err)
			}
			if got := rv64StorageFreeModule(m); got != tc.want {
				t.Fatalf("proof=%t; want %t", got, tc.want)
			}
		})
	}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("callback")...), 0, 0)
	callbackSource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
	m, err := wasm.DecodeModule(callbackSource)
	if err != nil {
		t.Fatal(err)
	}
	if rv64StorageFreeModule(m) {
		t.Fatal("host import received storage-free proof")
	}
	for _, tc := range []struct {
		name     string
		sections [][]byte
	}{
		{"GC initializer", [][]byte{
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 0, 1))),
			wasmtest.Section(3, []byte{1, 1}),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x64, 0, 0, 0x41, 1, 0xfb, 0, 0, 0x0b})),
		}},
		{"imported GC global", [][]byte{
			wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})),
			wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("root")...), 3, 0x6e, 0))),
			wasmtest.Section(3, []byte{1, 0}),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sections := append(tc.sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 17, 0x0b}))))
			m, err := wasm.DecodeModule(wasmtest.Module(sections...))
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{GCConstExpr: true, ExtendedConstGlobals: true}, 2, wasm.ValidationLimits{}); err != nil {
				t.Fatal(err)
			}
			if rv64StorageFreeModule(m) {
				t.Fatal("collector state received storage-free proof")
			}
		})
	}
	typ := reflect.TypeOf(railshotTailCompiledState{})
	var fields []reflect.StructField
	for i := 0; i < typ.NumField(); i++ {
		if field := typ.Field(i); field.Name != "storageFreeBodies" {
			fields = append(fields, field)
		}
	}
	if got, want := unsafe.Sizeof(railshotTailCompiledState{}), reflect.StructOf(fields).Size(); got != want {
		t.Fatalf("RV cold-state size=%d; before proof=%d", got, want)
	}
	t.Logf("RV cold-state size=%dB; proof adds 0B", typ.Size())
}
