//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64ExternalTableNullInitializer(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, count := range []int{1, 2} {
			for _, artifact := range []bool{false} {
				t.Run(fmt.Sprintf("vector%t/tables%d/artifact%t", vector, count, artifact), func(t *testing.T) {
					tables := []byte{byte(count)}
					for i := 0; i < count; i++ {
						tables = append(tables, 0x40, 0, 0x6f, 0, 2, 0xd0, 0x6f, 0x0b)
					}
					body := []byte{0x41, 0, 0x25, byte(count - 1), 0xd1, 0x0b}
					if vector {
						body = append(append([]byte{0xfd, 0x0c}, make([]byte, 16)...), append([]byte{0x1a}, body...)...)
					}
					source := wasmtest.Module(wasmtest.Section(1, []byte{1, 0x60, 0, 1, 0x7f}), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, tables), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("test", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
					cfg := rv64RootedScalarConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureTypedFunctionReferences | CoreFeatureGC)
					c, err := cfg.Compile(source)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()

					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					got, err := in.Invoke("test")
					if err != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal(got, err)
					}
					if in.gc != nil || c.needsNativeGCABI() {
						t.Fatal("external null initializer allocated a collector")
					}
				})
			}
		}
	}
}
