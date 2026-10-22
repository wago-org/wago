//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled && !wago_guardpage

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64ExternTableInternalHelperCapacity(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t/artifact%t", vector, artifact), func(t *testing.T) {
				body := func(code []byte) []byte {
					if vector {
						code = append(append([]byte{0xfd, 12}, make([]byte, 16)...), append([]byte{0x1a}, code...)...)
					}
					return wasmtest.Code(code)
				}
				source := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
					wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2}, []byte{0}, []byte{2})),
					wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 1, 2, 4})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 0), wasmtest.ExportEntry("empty", 0, 1), wasmtest.ExportEntry("clear", 0, 2), wasmtest.ExportEntry("fill", 0, 3), wasmtest.ExportEntry("copy", 0, 4))),
					wasmtest.Section(10, wasmtest.Vec(
						body([]byte{0x41, 1, 0x20, 0, 0x26, 0, 0x0b}),
						body([]byte{0x41, 1, 0x25, 0, 0xd1, 0x0b}),
						body([]byte{0x41, 1, 0xd0, 0x6f, 0x26, 0, 0x0b}),
						body([]byte{0x41, 0, 0x20, 0, 0x41, 2, 0xfc, 17, 0, 0x0b}),
						body([]byte{0x41, 1, 0x41, 0, 0x41, 1, 0xfc, 14, 0, 0, 0x0b}))),
				)
				c, err := NewRuntimeConfig().Compile(source)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				if artifact {
					c = publicArtifactRoundTrip(t, c)
					defer c.Close()
				}
				rt := NewRuntime()
				defer rt.Close()
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				in, err := rt.Instantiate(context.Background(), mod)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if in.c.maxParamSlots < 5 {
					t.Fatal("internal table helper arguments do not fit")
				}
				ref, err := rt.NewExternRef("table value")
				if err != nil {
					t.Fatal(err)
				}
				invoke := func(name string, args ...uint64) {
					t.Helper()
					if _, err := in.Invoke(name, args...); err != nil {
						t.Fatal(name, err)
					}
				}
				empty := func(want uint64) {
					t.Helper()
					got, err := in.Invoke("empty")
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("empty=%v,%v want [%d]", got, err, want)
					}
				}
				empty(1)
				invoke("set", ref.token)
				empty(0)
				invoke("clear")
				empty(1)
				invoke("fill", ref.token)
				invoke("clear")
				empty(1)
				invoke("copy")
				empty(0)
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				empty(0)
				invoke("fill", 0)
				empty(1)
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				if !rt.ReleaseExternRef(ref) {
					t.Fatal("cleared table kept its extern reference")
				}
			})
		}
	}
}
