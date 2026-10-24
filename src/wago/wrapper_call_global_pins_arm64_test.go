//go:build (linux || darwin) && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func wrapperCallGlobalPinModule(call []byte) []byte {
	params := make([]wasm.ValType, 8)
	for i := range params {
		params[i] = wasm.I32
	}
	// Static loop weighting makes the global and local register assignments hot.
	// Updating all parameters afterward leaves their pinned copies dirty at call.
	body := []byte{0x03, 0x40, 0x03, 0x40, 0x03, 0x40, 0x23, 0, 0x1a}
	for i := range params {
		body = append(body, 0x20, byte(i), 0x1a)
	}
	body = append(body, 0x0b, 0x0b, 0x0b)
	for i := range params {
		body = append(body, 0x20, byte(i), 0x41, 1, 0x6a, 0x21, byte(i))
	}
	body = append(body, call...)
	body = append(body, 0x23, 0)
	for i := range params {
		body = append(body, 0x20, byte(i), 0x6a)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 1})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x0b}))),
	)
}

func TestARM64WrapperCallPreservesDirtyLocalsWithModuleGlobals(t *testing.T) {
	for _, tc := range []struct {
		name string
		call []byte
	}{
		{"direct", []byte{0x10, 1}},
		{"indirect", []byte{0x41, 0, 0x11, 1, 0}},
		{"ref", []byte{0xd2, 1, 0x14, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := wrapperCallGlobalPinModule(tc.call)
			for _, regABI := range []bool{true, false} {
				for _, stackReg := range []bool{true, false} {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithFunctionWorkers(1).WithOptimization("reg-abi", regABI).WithOptimization("inline", false).WithOptimization("stack-reg", stackReg)
					cm, err := cfg.Compile(data)
					if err != nil {
						t.Fatal(err)
					}
					in, err := Instantiate(cm, InstantiateOptions{})
					if err != nil {
						cm.Close()
						t.Fatal(err)
					}
					got, err := in.Invoke("run", 1, 2, 3, 4, 5, 6, 7, 8)
					in.Close()
					cm.Close()
					if err != nil || len(got) != 1 || got[0] != 45 {
						t.Errorf("reg-abi=%v stack-reg=%v: got %v, %v; want [45]", regABI, stackReg, got, err)
					}
				}
			}
		})
	}
}
