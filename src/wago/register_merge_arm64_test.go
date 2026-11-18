//go:build arm64 && (linux || darwin) && !tinygo && !wago_target_tinygo && !wago_standalone

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestRegisterOnlyMergeNative(t *testing.T) {
	forms := []struct {
		name      string
		arm       []byte
		multiply  bool
		otherwise bool
	}{
		{"if-else", []byte{0x20, 3, 0x42, 7, 0x7e, 0x21, 3, 0x05, 0x20, 3, 0x10, 0, 0x21, 3}, true, true},
		{"implicit-store-edge", []byte{0x20, 0, 0x10, 0, 0x1a}, false, false},
		{"implicit-register-edge", []byte{0x20, 3, 0x42, 7, 0x7e, 0x21, 3}, true, false},
	}
	for _, form := range forms {
		t.Run(form.name, func(t *testing.T) {
			body := []byte{0x20, 0, 0x20, 1, 0x7c, 0x21, 3, 0x20, 2, 0x04, 0x40}
			body = append(body, form.arm...)
			body = append(body, 0x0b, 0x20, 3, 0x20, 0, 0x85, 0x0b)
			imp := append(append(wasmtest.Name("env"), wasmtest.Name("identity")...), 0, 1)
			module := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64, wasm.I32, wasm.I64}, []wasm.ValType{wasm.I64}), wasmtest.FuncType([]wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I64}))),
				wasmtest.Section(2, wasmtest.Vec(imp)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			c, err := Compile(nil, module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			calls := 0
			host := HostCallFunc(func(call HostCall) { calls++; call.ResultSlots()[0] = call.ParamSlots()[0] })
			in, err := Instantiate(c, InstantiateOptions{Imports: NewImports().Function("env", "identity", host)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, a := range []uint64{0, 1, 17, ^uint64(0), 1 << 63, 0x123456789abcdef0} {
				for _, b := range []uint64{0, 9, ^uint64(0), 1 << 63} {
					for _, cond := range []uint64{0, 1, 0xffffffff} {
						want := a + b
						if form.multiply && cond != 0 {
							want *= 7
						}
						before := calls
						got, err := in.Invoke("run", a, b, cond, 0)
						if err != nil || len(got) != 1 || got[0] != want^a {
							t.Fatalf("a=%x b=%x cond=%x got=%x want=%x err=%v", a, b, cond, got, want^a, err)
						}
						wantCalls := 0
						if form.otherwise && cond == 0 || form.name == "implicit-store-edge" && cond != 0 {
							wantCalls = 1
						}
						if calls-before != wantCalls {
							t.Fatal("wrong conditional call effects")
						}
					}
				}
			}
		})
	}
}
