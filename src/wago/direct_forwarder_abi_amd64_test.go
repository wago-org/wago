//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestDirectForwarderTargetABI(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types []wasm.ValType
		bits  []uint64
	}{
		{"i32", []wasm.ValType{wasm.I32}, []uint64{0xabcdef12}},
		{"i64", []wasm.ValType{wasm.I64}, []uint64{0xabcdef1234567890}},
		{"f32", []wasm.ValType{wasm.F32}, []uint64{0x3fc00000}},
		{"f64", []wasm.ValType{wasm.F64}, []uint64{0x4004000000000000}},
		{"v128", []wasm.ValType{wasm.V128}, []uint64{0x0123456789abcdef, 0xfedcba9876543210}},
		{"pair", []wasm.ValType{wasm.I32, wasm.I64}, []uint64{0xabcdef12, 0xabcdef1234567890}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var args []byte
			for i := range tc.types {
				args = append(args, 0x20, byte(i))
			}
			leaf := append(append([]byte(nil), args...), 0x0b)
			wrapper := append(append([]byte(nil), args...), 0x10, 0, 0x0b)
			caller := append(append([]byte(nil), args...), 0x10, 1, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(tc.types, tc.types), wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1}, []byte{1}, []byte{2})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(leaf), wasmtest.Code(wrapper), wasmtest.Code(caller), wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}))),
			)
			for _, regABI := range []bool{false, true} {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("reg-abi", regABI), data)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if status := c.GCNativeRootAdmission(); !status.Required || !status.Exact {
					t.Fatalf("ABI fixture requires exact roots: %+v", status)
				}
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("run", tc.bits...)
				in.Close()
				if err != nil || !reflect.DeepEqual(got, tc.bits) {
					t.Fatalf("regABI=%t result=%v,%v want%v", regABI, got, err, tc.bits)
				}
			}
		})
	}
}
