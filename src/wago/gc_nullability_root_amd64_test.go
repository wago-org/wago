//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcFuncrefNullabilityRootModule(op string) []byte {
	allocate := []byte{0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a} // struct.new 0; drop
	var body []byte
	switch op {
	case "ref.as_non_null":
		body = []byte{0x23, 0x00, 0xd4} // global.get 0; ref.as_non_null
		body = append(body, allocate...)
		body = append(body, 0xd1, 0x0b) // ref.is_null; end
	case "br_on_null":
		body = []byte{0x02, 0x40, 0x23, 0x00, 0xd5, 0x00} // block; global.get 0; br_on_null 0
		body = append(body, allocate...)
		body = append(body, 0xd1, 0x0f, 0x0b, 0x41, 0x01, 0x0b) // ref.is_null; return; end; i32.const 1; end
	case "br_on_non_null":
		body = []byte{0x02, 0x64, 0x70, 0x23, 0x00, 0xd6, 0x00} // block (result (ref func)); global.get 0; br_on_non_null 0
		body = append(body, 0x41, 0x01, 0x0f, 0x0b)             // null fallthrough returns 1; end
		body = append(body, allocate...)
		body = append(body, 0xd1, 0x0b) // ref.is_null; end
	default:
		panic("unknown nullability operation")
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01}, // (struct (field (mut i32)))
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x70, 0x00, 0xd2, 0x00, 0x0b})), // funcref global = ref.func 0
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x03, 0x00, 0x01, 0x00})), // declare ref.func 0
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code(body))),
	)
}

func TestGCFuncrefNullabilityDoesNotCreateCollectorRoot(t *testing.T) {
	for _, op := range []string{"ref.as_non_null", "br_on_null", "br_on_non_null"} {
		t.Run(op, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcFuncrefNullabilityRootModule(op))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
				Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16,
				TinyCollectEveryAlloc: true, VerifyAfterCollect: true,
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("run = %v, %v; want [0]", got, err)
			}
		})
	}
}

func gcStructNullabilityRootModule(op string) []byte {
	allocate := []byte{0xfb, 0x01, 0x00, 0x1a}   // struct.new_default 0; drop
	body := []byte{0x41, 0x2a, 0xfb, 0x00, 0x00} // struct.new(42)
	switch op {
	case "ref.as_non_null":
		body = append(body, 0xd4) // ref.as_non_null
	case "br_on_null":
		body = append([]byte{0x02, 0x40}, body...) // block; struct.new(42)
		body = append(body, 0xd5, 0x00)            // br_on_null 0
		body = append(body, allocate...)
		body = append(body, 0xfb, 0x02, 0x00, 0x00, 0x0f, 0x0b, 0x41, 0x00, 0x0b) // struct.get; return; end; fallback 0; end
		return gcStructNullabilityModule(body)
	case "br_on_non_null":
		body = append([]byte{0x02, 0x64, 0x00}, body...) // block (result (ref 0)); struct.new(42)
		body = append(body, 0xd6, 0x00)                  // br_on_non_null 0
		body = append(body, 0x41, 0x00, 0x0f, 0x0b)      // null fallthrough returns 0; end
	default:
		panic("unknown nullability operation")
	}
	body = append(body, allocate...)
	body = append(body, 0xfb, 0x02, 0x00, 0x00, 0x0b) // struct.get 0 0; end
	return gcStructNullabilityModule(body)
}

func gcStructNullabilityModule(body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01}, // (struct (field (mut i32)))
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCNullabilityPreservesCollectorRoot(t *testing.T) {
	for _, op := range []string{"ref.as_non_null", "br_on_null", "br_on_non_null"} {
		t.Run(op, func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcStructNullabilityRootModule(op))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
				Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16,
				TinyCollectEveryAlloc: true, VerifyAfterCollect: true,
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("run = %v, %v; want [42]", got, err)
			}
		})
	}
}
