//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicPrimitiveNullAndTableGrowth(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, kind := range []string{"extern", "noextern", "global-nofunc", "global-indexed", "host-reference", "table-growth"} {
			for _, artifact := range []bool{false} {
				t.Run(fmt.Sprintf("%s/vector%t/artifact%t", kind, vector, artifact), func(t *testing.T) {
					types := [][]byte{{0x60, 0, 1, 0x7f}}
					if vector {
						types = append(types, []byte{0x60, 1, 0x7b, 0})
					}
					sections := [][]byte{}
					body, want := []byte{0xd0, 0x6f, 0xd1, 0x0b}, uint64(1)
					function := byte(0)
					imports := NewImports()
					switch kind {
					case "noextern":
						body[1] = 0x72
					case "global-nofunc", "global-indexed":
						heap := byte(0x73)
						if kind == "global-indexed" {
							heap = 0
						}
						sections = append(sections, wasmtest.Section(6, []byte{1, 0x70, 0, 0xd0, heap, 0x0b}))
						body = []byte{0x23, 0, 0xd1, 0x0b}
					case "host-reference":
						types = append(types, []byte{0x60, 0, 1, 0x6f})
						im := append(append(wasmtest.Name("env"), wasmtest.Name("value")...), 0, byte(len(types)-1))
						sections = append(sections, wasmtest.Section(2, wasmtest.Vec(im)))
						imports.Function("env", "value", HostCallFunc(func(call HostCall) { call.SetExternRef(0, ExternRef{}) }))
						body, function = []byte{0x10, 0, 0xd1, 0x0b}, 1
					case "table-growth":
						table := append([]byte{1, 0x70, 0}, wasmtest.ULEB(600)...)
						sections = append(sections, wasmtest.Section(4, table))
						body = []byte{0xd0, 0x70, 0x41, 1, 0xfc, 15, 0, 0x0b}
						want = 600
					}
					// Put declarations in binary section order.
					ordered := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
					for _, section := range sections {
						if section[0] == 2 {
							ordered = append(ordered, section)
						}
					}
					ordered = append(ordered, wasmtest.Section(3, []byte{1, 0}))
					for _, section := range sections {
						if section[0] != 2 {
							ordered = append(ordered, section)
						}
					}
					ordered = append(ordered, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, uint32(function)))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
					c, err := NewRuntimeConfig().Compile(wasmtest.Module(ordered...))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()

					in, err := Instantiate(c, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					got, err := in.Invoke("run")
					if err != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatal(got, err)
					}
					if in.gc != nil || c.needsNativeGCABI() {
						t.Fatal("primitive module allocated a collector")
					}
				})
			}
		}
	}
}
