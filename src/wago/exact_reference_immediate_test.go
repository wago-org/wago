package wago

import (
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func exactReferenceBodyModule(body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func exactReferenceGlobalModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(6, wasmtest.Vec([]byte{
			0x63, 0x62, 0x00, // (ref null (exact 0))
			0x00,                   // immutable
			0xd0, 0x62, 0x00, 0x0b, // ref.null (exact 0); end
		})),
	)
}

func TestCompileExactReferenceImmediates(t *testing.T) {
	if !supportsCompleteCore3Backend(runtime.GOOS, runtime.GOARCH) {
		t.Skip("Core 3 execution backend is unavailable")
	}
	if runtime.GOARCH == "riscv64" {
		t.Skip("RISC-V exact-reference execution products require dedicated module shapes")
	}
	cases := []struct {
		name   string
		module []byte
	}{
		{
			name: "body ref.null",
			module: exactReferenceBodyModule([]byte{
				0xd0, 0x62, 0x00, // ref.null (exact 0)
				0x1a,       // drop
				0x41, 0x07, // i32.const 7
				0x0b,
			}),
		},
		{
			name: "block result",
			module: exactReferenceBodyModule([]byte{
				0x02, 0x63, 0x62, 0x00, // block (result (ref null (exact 0)))
				0xd0, 0x62, 0x00, // ref.null (exact 0)
				0x0b,
				0x1a,       // drop
				0x41, 0x07, // i32.const 7
				0x0b,
			}),
		},
		{
			name: "typed select",
			module: exactReferenceBodyModule([]byte{
				0xd0, 0x62, 0x00, // ref.null (exact 0)
				0xd0, 0x62, 0x00, // ref.null (exact 0)
				0x41, 0x00, // i32.const 0
				0x1c, 0x01, 0x63, 0x62, 0x00, // select (result (ref null (exact 0)))
				0x1a,       // drop
				0x41, 0x07, // i32.const 7
				0x0b,
			}),
		},
		{name: "global type and initializer", module: exactReferenceGlobalModule()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), tc.module)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			defer compiled.Close()
			if !compiled.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
				t.Fatalf("required features = %s, want typed-function-references", compiled.requiredFeatures)
			}
		})
	}
}
