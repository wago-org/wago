//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"errors"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func eagerTrapOrderModule(loadFirst bool) []byte {
	prefix := []byte{
		0x20, 0x00, // local.get 0
		0x20, 0x01, // local.get 1
		0x6d, // i32.div_s
	}
	if loadFirst {
		prefix = []byte{0x20, 0x00, 0x28, 0x02, 0x00} // local.get 0; i32.load
	}
	body := func(later ...byte) []byte {
		code := append([]byte(nil), prefix...)
		code = append(code, later...)
		return append(code, 0x0b)
	}
	bodies := [][]byte{
		body(0x20, 0x02, 0xaa, 0x1a),                         // i32.trunc_f64_s; drop
		body(0x41, 0x80, 0x80, 0x04, 0x28, 0x02, 0x00, 0x1a), // i32.load 65536; drop
		body(0x41, 0x01, 0x25, 0x00, 0x1a),                   // table.get 0 1; drop
		body(0x41, 0x01, 0xd0, 0x70, 0x26, 0x00),             // table.set 0 1 (ref.null func)
		body(0xd0, 0x6f, 0xd4, 0x1a),                         // ref.as_non_null (ref.null extern); drop
		body(0xd0, 0x6c, 0xfb, 0x1d, 0x1a),                   // i31.get_s (ref.null i31); drop
		body(0xd0, 0x6e, 0xfb, 0x16, 0x6c, 0x1a),             // ref.cast i31 (ref.null any); drop
	}
	functions := make([][]byte, len(bodies))
	codes := make([][]byte, len(bodies))
	exports := make([][]byte, len(bodies))
	names := []string{"trunc", "load", "table_get", "table_set", "non_null", "i31_get", "i31_cast"}
	for i := range bodies {
		functions[i] = wasmtest.ULEB(0)
		codes[i] = wasmtest.Code(bodies[i])
		exports[i] = wasmtest.ExportEntry(names[i], 0, uint32(i))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I32, wasm.F64},
			[]wasm.ValType{wasm.I32},
		))),
		wasmtest.Section(3, wasmtest.Vec(functions...)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func TestEarlierDeferredTrapWinsOverEagerTrap(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeaturesV3) {
		t.Skip("requires Core v3 features")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), eagerTrapOrderModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	for _, name := range []string{"trunc", "load", "table_get", "table_set", "non_null", "i31_get", "i31_cast"} {
		t.Run(name, func(t *testing.T) {
			_, err := instance.Invoke(name, 1, 0, math.Float64bits(math.NaN()))
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapDivZero {
				t.Fatalf("Invoke = %v; want %v", err, TrapDivZero)
			}
		})
	}
}

func TestEarlierDeferredLoadWinsOverEagerTrap(t *testing.T) {
	if !SupportedFeatures().IsEnabled(CoreFeaturesV3) {
		t.Skip("requires Core v3 features")
	}
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if GuardPageSupported() {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, mode := range modes {
		t.Run(mode.String(), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(mode), eagerTrapOrderModule(true))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			for _, name := range []string{"trunc", "table_get", "table_set", "non_null", "i31_get", "i31_cast"} {
				t.Run(name, func(t *testing.T) {
					_, err := instance.Invoke(name, 65536, 0, math.Float64bits(math.NaN()))
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
						t.Fatalf("Invoke = %v; want %v", err, TrapLinMemOutOfBounds)
					}
				})
			}
		})
	}
}
