//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64InternalCollectorDeclarationSourceAndArtifact(t *testing.T) {
	for _, kind := range []byte{0x5e, 0x5f} {
		t.Run(fmt.Sprintf("kind%x", kind), func(t *testing.T) {
			heap := []byte{kind, 0x7f, 0}
			if kind == 0x5f {
				heap = []byte{kind, 1, 0x7f, 0}
			}
			source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(heap, []byte{0x60, 1, 0x64, 0, 0})), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x1a, 0x0b}))))
			c, err := Compile(rv64RootedScalarConfig(), source)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			for _, code := range []*Compiled{c} {
				view := code.executionView()
				if !view.needsExactNativeGCRoots() || !rv64FreshRootedGC(view) || !view.hasCollectorDeclarationRoots() {
					t.Fatal("internal collector declaration lost its exact root/ABI proof")
				}
				in, err := Instantiate(code)
				if err != nil {
					t.Fatal(err)
				}
				in.Close()
			}
		})
	}
}
