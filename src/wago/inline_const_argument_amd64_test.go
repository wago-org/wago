//go:build linux && amd64 && !tinygo

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func inlineConstArgumentModule(body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body), wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x6a, 0x0b}))),
	)
}

func TestInlineConstantArgumentTrapOrder(t *testing.T) {
	for _, left := range []bool{false, true} {
		body := []byte{0x41, 1, 0x20, 0, 0x6e}
		if left {
			body = append(body, 0x41, 7)
		}
		body = append(body, 0x20, 1, 0x41, 0x7f, 0x6d)
		if !left {
			body = append(body, 0x41, 7)
		}
		body = append(body, 0x10, 1, 0x6a, 0x0b)
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), inlineConstArgumentModule(body))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			divisor uint64
			trap    TrapCode
		}{{0, TrapDivZero}, {1, TrapDivOverflow}} {
			_, err := in.Invoke("run", tc.divisor, 0x80000000)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != tc.trap {
				t.Fatalf("left%v divisor%d: %v; want%v", left, tc.divisor, err, tc.trap)
			}
		}
		got, err := in.Invoke("run", 1, 5)
		if err != nil || len(got) != 1 || got[0] != 3 {
			t.Fatalf("left%v: %v,%v; want3", left, got, err)
		}
		in.Close()
		c.Close()
	}
}
