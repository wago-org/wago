//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestLargeOffsetBoundsTrap(t *testing.T) {
	for _, off := range []uint32{0x7fffffff, 0x80000000, 0xffffffff} {
		t.Run(fmt.Sprint(off), func(t *testing.T) {
			body := []byte{0x20, 0, 0x2d, 0, 0, 0x20, 0, 0x2d, 0}
			body = append(body, wasmtest.ULEB(off)...)
			body = append(body, 0x0b)
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("check", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			)
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithDeferBoundsChecks(true), data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			_, err = in.Invoke("check", I32(0))
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
				t.Fatalf("check(0) = %v, want memory bounds trap", err)
			}
		})
	}
}
