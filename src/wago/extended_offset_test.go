package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func localGlobalDataOffsetModule() []byte {
	data := wasmtest.Vec(
		[]byte{0x00, 0x23, 0x00, 0x41, 0x02, 0x6a, 0x0b, 0x01, 'a'}, // g0 + 2 = 4
		[]byte{0x00, 0x23, 0x00, 0x41, 0x01, 0x6b, 0x0b, 0x01, 's'}, // g0 - 1 = 1
		[]byte{0x00, 0x23, 0x00, 0x41, 0x03, 0x6c, 0x0b, 0x01, 'm'}, // g0 * 3 = 6
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x02, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("load", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x2d, 0x00, 0x00, 0x0b}))),
		wasmtest.Section(11, data),
	)
}

func localGlobalElementOffsetModule() []byte {
	// Active element flags=0, offset global.get 0, one function index 0.
	elem := []byte{0x00, 0x23, 0x00, 0x0b, 0x01, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x02})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 0x01, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(9, wasmtest.Vec(elem)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0x11, 0x00, 0x00, 0x0b}),
		)),
	)
}

func TestCompiledOffsetExpressionScopeValidation(t *testing.T) {
	defs := []GlobalDef{
		{Type: ValI32},
		{Type: ValI32, Mutable: true},
		{Type: ValI64},
	}
	offsetScope := constExprGlobalScope{context: constExprDataOffset, limit: len(defs)}
	for _, tc := range []struct {
		name string
		expr []byte
		want string
	}{
		{name: "mutable", expr: []byte{0x23, 0x01, 0x0b}, want: "mutable"},
		{name: "wrong type", expr: []byte{0x23, 0x02, 0x0b}, want: "type mismatch"},
		{name: "out of range", expr: []byte{0x23, 0x03, 0x0b}, want: "unavailable"},
		{name: "trailing instruction", expr: []byte{0x23, 0x00, 0x0b, 0x01}, want: "trailing"},
		{name: "missing end", expr: []byte{0x23, 0x00}, want: "missing end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCompiledScalarConstExpr(tc.expr, ValI32, defs, offsetScope)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation error = %v, want %q", err, tc.want)
			}
		})
	}
	if err := validateCompiledScalarConstExpr([]byte{0x23, 0x00, 0x0b}, ValI32, defs, constExprGlobalScope{context: constExprGlobalInitializer, limit: 0}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("forward global initializer error = %v", err)
	}
}

func TestActiveOffsetsUseLocalImmutableGlobals(t *testing.T) {
	t.Run("data add sub mul and codec", func(t *testing.T) {
		compiled, err := Compile(nil, localGlobalDataOffsetModule())
		if err != nil {
			t.Fatal(err)
		}
		blob, err := compiled.MarshalBinary()
		compiled.Close()
		if err != nil {
			t.Fatal(err)
		}
		compiled, err = Load(blob)
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := Instantiate(compiled, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		for offset, want := range map[int32]int32{1: 's', 4: 'a', 6: 'm'} {
			got, err := in.Invoke("load", I32(offset))
			if err != nil || AsI32(got[0]) != want {
				t.Fatalf("load(%d) = %v, %v; want %d", offset, got, err, want)
			}
		}
	})

	t.Run("element", func(t *testing.T) {
		compiled, err := Compile(nil, localGlobalElementOffsetModule())
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := Instantiate(compiled, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		got, err := in.Invoke("call")
		if err != nil || AsI32(got[0]) != 42 {
			t.Fatalf("call() = %v, %v; want 42", got, err)
		}
	})
}
