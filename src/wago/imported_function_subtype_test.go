//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestImportedFunctionPreservesDynamicSubtype(t *testing.T) {
	types := []byte{2, 0x50, 0, 0x60, 1, 0x7f, 1, 0x7f, 0x4f, 1, 0, 0x60, 1, 0x7f, 1, 0x7f}
	provider := wasmtest.Module(
		wasmtest.Section(1, types), wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	im := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0, 0)
	consumer := wasmtest.Module(
		wasmtest.Section(1, types), wasmtest.Section(2, wasmtest.Vec(im)),
		wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(4, []byte{1, 0x70, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("sub", 0, 1), wasmtest.ExportEntry("super", 0, 2))),
		wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x11, 1, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x11, 0, 0, 0x0b}))),
	)
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			compile := func(source []byte) *Compiled {
				t.Helper()
				c, err := artifactTestConfig(artifact).Compile(source)
				if err != nil {
					t.Fatal(err)
				}
				closeCompiled := c.Close
				t.Cleanup(func() { _ = closeCompiled() })
				if artifact {
					blob, err := c.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					c, err = LoadTrustedArtifact(blob)
					if err != nil {
						t.Fatal(err)
					}
					closeLoaded := c.Close
					t.Cleanup(func() { _ = closeLoaded() })
				}
				return c
			}
			p, c := compile(provider), compile(consumer)
			owner, err := Instantiate(p)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			export, err := owner.ExportedFunc("f")
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, NewImports().Function("env", "f", export))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for _, name := range []string{"sub", "super"} {
				got, err := in.Invoke(name, 99)
				if err != nil || len(got) != 1 || got[0] != 99 {
					t.Fatal(name, got, err)
				}
			}
		})
	}
}
